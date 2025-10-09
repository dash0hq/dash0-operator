// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const builtin = @import("builtin");
const std = @import("std");

const alloc = @import("allocator.zig");
const libc = @import("libc.zig");
const print = @import("print.zig");
const test_util = @import("test_util.zig");
const types = @import("types.zig");

const testing = std.testing;

pub const DotnetValues = struct {
    coreclr_enable_profiling: types.NullTerminatedString,
    coreclr_profiler: types.NullTerminatedString,
    coreclr_profiler_path: types.NullTerminatedString,
    additional_deps: types.NullTerminatedString,
    shared_store: types.NullTerminatedString,
    startup_hooks: types.NullTerminatedString,
    otel_auto_home: types.NullTerminatedString,
};

pub const CachedDotnetValues = struct {
    values: ?DotnetValues,
    done: bool,
};

const DotnetError = error{
    UnknownLibCFlavor,
    UnsupportedCpuArchitecture,
    OutOfMemory,
};

pub const coreclr_enable_profiling_env_var_name = "CORECLR_ENABLE_PROFILING";
pub const coreclr_profiler_env_var_name = "CORECLR_PROFILER";
pub const coreclr_profiler_path_env_var_name = "CORECLR_PROFILER_PATH";
pub const dotnet_additional_deps_env_var_name = "DOTNET_ADDITIONAL_DEPS";
pub const dotnet_shared_store_env_var_name = "DOTNET_SHARED_STORE";
pub const dotnet_startup_hooks_env_var_name = "DOTNET_STARTUP_HOOKS";
pub const otel_dotnet_auto_home_env_var_name = "OTEL_DOTNET_AUTO_HOME";

const dotnet_path_prefix = "/__dash0__/instrumentation/dotnet";

var cached_dotnet_values = CachedDotnetValues{
    .values = null,
    .done = false,
};
var libc_flavor: ?types.LibCFlavor = null;

pub fn setLibcFlavor(lf: types.LibCFlavor) void {
    libc_flavor = lf;
}

pub fn getDotnetValues() ?DotnetValues {
    if (libc_flavor == null) {
        print.printMessage("invariant violated: libc flavor has not been set prior to calling getDotnetValues().", .{});
        return null;
    }
    if (libc_flavor == types.LibCFlavor.UNKNOWN) {
        print.printMessage("Cannot determine libc flavor", .{});
        return null;
    }

    if (cached_dotnet_values.done) {
        return cached_dotnet_values.values;
    }

    if (libc_flavor) |libc_f| {
        const values = determineDotnetValues(libc_f, builtin.cpu.arch) catch |err| {
            print.printMessage("Cannot determine .NET environment variables: {}", .{err});
            cached_dotnet_values = .{
                .values = null,
                // do not try to determine the .NET values again
                .done = true,
            };
            return null;
        };

        const paths_to_check = [_]types.NullTerminatedString{
            values.coreclr_profiler_path,
            values.additional_deps,
            values.otel_auto_home,
            values.shared_store,
            values.startup_hooks,
        };
        for (paths_to_check) |p| {
            std.fs.cwd().access(std.mem.span(p), .{}) catch |err| {
                print.printMessage("Skipping injection of injecting the .NET OpenTelemetry instrumentation because of an issue accessing {s}: {}", .{ p, err });
                cached_dotnet_values = .{
                    .values = null,
                    // do not try to determine the .NET values again
                    .done = true,
                };
                return null;
            };
        }

        cached_dotnet_values = .{
            .values = values,
            .done = true,
        };
        return values;
    }

    unreachable;
}

test "getDotnetValues: should return null value if the libc flavor has not been set" {
    _resetState();
    defer _resetState();

    libc_flavor = null;
    const dotnet_values = getDotnetValues();
    try test_util.expectWithMessage(dotnet_values == null, "dotnet_values == null");
}

test "getDotnetValues: should return null value if the profiler path cannot be accessed" {
    _resetState();
    defer _resetState();

    libc_flavor = .GNU;
    const dotnet_values = getDotnetValues();
    try test_util.expectWithMessage(dotnet_values == null, "dotnet_values == null");
}

fn determineDotnetValues(libc_f: types.LibCFlavor, architecture: std.Target.Cpu.Arch) DotnetError!DotnetValues {
    const libc_flavor_prefix =
        switch (libc_f) {
            .GNU => "glibc",
            .MUSL => "musl",
            else => return error.UnknownLibCFlavor,
        };
    const platform =
        switch (libc_f) {
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

fn _resetState() void {
    cached_dotnet_values = CachedDotnetValues{
        .values = null,
        .done = false,
    };
    libc_flavor = null;
}
