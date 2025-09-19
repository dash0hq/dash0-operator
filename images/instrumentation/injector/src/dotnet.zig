// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const builtin = @import("builtin");
const std = @import("std");

const alloc = @import("allocator.zig");
const libc = @import("libc.zig");
const print = @import("print.zig");
const types = @import("types.zig");

const testing = std.testing;

// Note: The CLR bootstrapping code (implemented in C++) uses getenv, but when doing
// Environment.GetEnvironmentVariable from within a .NET application, it will apparently bypass getenv.
// That is, while we can inject the OTel SDK and activate tracing for a .NET application, overriding the the getenv
// function is probably not suitable for overriding
// environment variables that the OTel SDK looks up from within the CLR (like OTEL_DOTNET_AUTO_HOME,
// OTEL_RESOURCE_ATTRIBUTES, etc.).
//
// Here is an example for the lookup of DOTNET_SHARED_STORE, which happens at runtime startup, via getenv:
// https://github.com/dotnet/runtime/blob/v9.0.5/src/native/corehost/hostpolicy/shared_store.cpp#L16
// -> https://github.com/dotnet/runtime/blob/v9.0.5/src/native/corehost/hostmisc/pal.unix.cpp#L954.
//
// In contrast to that, the implementation of Environment.GetEnvironmentVariable reads the __environ into a dictionary
// and then uses that dictionary for all lookups, see here:
// https://github.com/dotnet/runtime/blob/v9.0.5/src/libraries/System.Private.CoreLib/src/System/Environment.cs#L66 ->
// - https://github.com/dotnet/runtime/blob/v9.0.5/src/libraries/System.Private.CoreLib/src/System/Environment.Variables.Unix.cs#L15-L32,
// - https://github.com/dotnet/runtime/blob/v9.0.5/src/libraries/System.Private.CoreLib/src/System/Environment.Variables.Unix.cs#L85-L91, and
// https://github.com/dotnet/runtime/blob/v9.0.5/src/libraries/System.Private.CoreLib/src/System/Environment.Variables.Unix.cs#L93-L166

pub const DotnetValues = struct {
    coreclr_enable_profiling: types.NullTerminatedString,
    coreclr_profiler: types.NullTerminatedString,
    coreclr_profiler_path: types.NullTerminatedString,
    additional_deps: types.NullTerminatedString,
    shared_store: types.NullTerminatedString,
    startup_hooks: types.NullTerminatedString,
    otel_auto_home: types.NullTerminatedString,
};

const DotnetError = error{
    UnknownLibCFlavor,
    UnsupportedCpuArchitecture,
    OutOfMemory,
};

const dotnet_path_prefix = "/__dash0__/instrumentation/dotnet";
var experimental_dotnet_injection_enabled: ?bool = null;

var cached_dotnet_values: ?DotnetValues = null;
var cached_libc_flavor: ?types.LibCFlavor = null;

const injection_happened_msg = "injecting the .NET OpenTelemetry instrumentation";
var injection_happened_msg_has_been_printed = false;

fn initIsEnabled() void {
    if (experimental_dotnet_injection_enabled == null) {
        if (std.posix.getenv("DASH0_EXPERIMENTAL_DOTNET_INJECTION")) |raw| {
            experimental_dotnet_injection_enabled = std.ascii.eqlIgnoreCase("true", raw);
        } else {
            experimental_dotnet_injection_enabled = false;
        }
    }
}

pub fn isEnabled() bool {
    if (experimental_dotnet_injection_enabled == null) {
        initIsEnabled();
    }
    return experimental_dotnet_injection_enabled orelse false;
}

pub fn getDotnetValues() ?DotnetValues {
    if (cached_dotnet_values) |val| {
        return val;
    }

    if (cached_libc_flavor == null) {
        const lib_c = libc.getLibCInfo() catch |err| {
            print.printError("Cannot get LibC information: {}", .{err});
            return null;
        };
        cached_libc_flavor = lib_c.flavor;
    }

    if (cached_libc_flavor == types.LibCFlavor.UNKNOWN) {
        print.printError("Cannot determine LibC flavor", .{});
        return null;
    }

    if (cached_libc_flavor) |flavor| {
        const dotnet_values = determineDotnetValues(flavor, builtin.cpu.arch) catch |err| {
            print.printError("Cannot determine .NET environment variables: {}", .{err});
            return null;
        };

        const paths_to_check = [_]types.NullTerminatedString{
            dotnet_values.coreclr_profiler_path,
            dotnet_values.additional_deps,
            dotnet_values.otel_auto_home,
            dotnet_values.shared_store,
            dotnet_values.startup_hooks,
        };
        for (paths_to_check) |p| {
            std.fs.cwd().access(std.mem.span(p), .{}) catch |err| {
                print.printError("Skipping injection of injecting the .NET OpenTelemetry instrumentation because of an issue accessing {s}: {}", .{ p, err });
                return null;
            };
        }

        cached_dotnet_values = dotnet_values;
        return cached_dotnet_values;
    }

    unreachable;
}

test "getDotnetValues: should return null value if the profiler path cannot be accessed" {
    const dotnet_values = getDotnetValues();
    try testing.expect(dotnet_values == null);
}

fn determineDotnetValues(libc_flavor: types.LibCFlavor, architecture: std.Target.Cpu.Arch) DotnetError!DotnetValues {
    const libc_flavor_prefix =
        switch (libc_flavor) {
            .GNU => "glibc",
            .MUSL => "musl",
            else => return error.UnknownLibCFlavor,
        };
    const platform =
        switch (libc_flavor) {
            .GNU => switch (architecture) {
                .x86_64 => "linux-x64",
                .aarch64 => "linux-arm64",
                else => return error.UnsupportedCpuArchitecture,
            },
            .MUSL => switch (architecture) {
                .x86_64 => "linux-musl-x64",
                .aarch64 => "linux-musl-arm64",
                else => return error.UnsupportedCpuArchitecture,
            },
            else => return error.UnknownLibCFlavor,
        };
    const coreclr_profiler_path = try std.fmt.allocPrintZ(alloc.page_allocator, "{s}/{s}/{s}/OpenTelemetry.AutoInstrumentation.Native.so", .{
        dotnet_path_prefix, libc_flavor_prefix, platform,
    });

    const additional_deps = try std.fmt.allocPrintZ(alloc.page_allocator, "{s}/{s}/AdditionalDeps", .{
        dotnet_path_prefix, libc_flavor_prefix,
    });

    const otel_auto_home = try std.fmt.allocPrintZ(alloc.page_allocator, "{s}/{s}", .{ dotnet_path_prefix, libc_flavor_prefix });

    const shared_store = try std.fmt.allocPrintZ(alloc.page_allocator, "{s}/{s}/store", .{
        dotnet_path_prefix, libc_flavor_prefix,
    });

    const startup_hooks = try std.fmt.allocPrintZ(alloc.page_allocator, "{s}/{s}/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll", .{
        dotnet_path_prefix, libc_flavor_prefix,
    });

    if (!injection_happened_msg_has_been_printed) {
        print.printMessage(injection_happened_msg, .{});
        injection_happened_msg_has_been_printed = true;
    }
    return .{
        .coreclr_enable_profiling = "1",
        .coreclr_profiler = "{918728DD-259F-4A6A-AC2B-B85E1B658318}",
        .coreclr_profiler_path = coreclr_profiler_path,
        .additional_deps = additional_deps,
        .otel_auto_home = otel_auto_home,
        .shared_store = shared_store,
        .startup_hooks = startup_hooks,
    };
}

test "determineDotnetValues: should return error for unsupported CPU architecture" {
    try testing.expectError(error.UnsupportedCpuArchitecture, determineDotnetValues(.GNU, .powerpc64le));
}

test "determineDotnetValues: should return error for unknown libc flavor" {
    try testing.expectError(error.UnknownLibCFlavor, determineDotnetValues(.UNKNOWN, .x86_64));
}

test "determineDotnetValues: should return values for glibc/x86_64" {
    const dotnet_values = try determineDotnetValues(.GNU, .x86_64);
    try testing.expectEqualStrings(
        "1",
        std.mem.span(dotnet_values.coreclr_enable_profiling),
    );
    try testing.expectEqualStrings(
        "{918728DD-259F-4A6A-AC2B-B85E1B658318}",
        std.mem.span(dotnet_values.coreclr_profiler),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/linux-x64/OpenTelemetry.AutoInstrumentation.Native.so",
        std.mem.span(dotnet_values.coreclr_profiler_path),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/AdditionalDeps",
        std.mem.span(dotnet_values.additional_deps),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc",
        std.mem.span(dotnet_values.otel_auto_home),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/store",
        std.mem.span(dotnet_values.shared_store),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll",
        std.mem.span(dotnet_values.startup_hooks),
    );
}

test "determineDotnetValues: should return values for glibc/arm64" {
    const dotnet_values = try determineDotnetValues(.GNU, .aarch64);
    try testing.expectEqualStrings(
        "1",
        std.mem.span(dotnet_values.coreclr_enable_profiling),
    );
    try testing.expectEqualStrings(
        "{918728DD-259F-4A6A-AC2B-B85E1B658318}",
        std.mem.span(dotnet_values.coreclr_profiler),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/linux-arm64/OpenTelemetry.AutoInstrumentation.Native.so",
        std.mem.span(dotnet_values.coreclr_profiler_path),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/AdditionalDeps",
        std.mem.span(dotnet_values.additional_deps),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc",
        std.mem.span(dotnet_values.otel_auto_home),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/store",
        std.mem.span(dotnet_values.shared_store),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll",
        std.mem.span(dotnet_values.startup_hooks),
    );
}

test "determineDotnetValues: should return values for musl/x86_64" {
    const dotnet_values = try determineDotnetValues(.MUSL, .x86_64);
    try testing.expectEqualStrings(
        "1",
        std.mem.span(dotnet_values.coreclr_enable_profiling),
    );
    try testing.expectEqualStrings(
        "{918728DD-259F-4A6A-AC2B-B85E1B658318}",
        std.mem.span(dotnet_values.coreclr_profiler),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/linux-musl-x64/OpenTelemetry.AutoInstrumentation.Native.so",
        std.mem.span(dotnet_values.coreclr_profiler_path),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/AdditionalDeps",
        std.mem.span(dotnet_values.additional_deps),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl",
        std.mem.span(dotnet_values.otel_auto_home),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/store",
        std.mem.span(dotnet_values.shared_store),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll",
        std.mem.span(dotnet_values.startup_hooks),
    );
}

test "determineDotnetValues: should return values for musl/arm64" {
    const dotnet_values = try determineDotnetValues(.MUSL, .aarch64);
    try testing.expectEqualStrings(
        "1",
        std.mem.span(dotnet_values.coreclr_enable_profiling),
    );
    try testing.expectEqualStrings(
        "{918728DD-259F-4A6A-AC2B-B85E1B658318}",
        std.mem.span(dotnet_values.coreclr_profiler),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/linux-musl-arm64/OpenTelemetry.AutoInstrumentation.Native.so",
        std.mem.span(dotnet_values.coreclr_profiler_path),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/AdditionalDeps",
        std.mem.span(dotnet_values.additional_deps),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl",
        std.mem.span(dotnet_values.otel_auto_home),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/store",
        std.mem.span(dotnet_values.shared_store),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll",
        std.mem.span(dotnet_values.startup_hooks),
    );
}
