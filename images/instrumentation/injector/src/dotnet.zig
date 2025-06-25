// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const builtin = @import("builtin");
const std = @import("std");

const cache = @import("cache.zig");
const env = @import("env.zig");
const print = @import("print.zig");
const test_util = @import("test_util.zig");
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

const DotnetError = error{
    UnknownLibCFlavor,
    UnsupportedCpuArchitecture,
    OutOfMemory,
};

const dash0_experimental_dotnet_injection_env_var_name = "DASH0_EXPERIMENTAL_DOTNET_INJECTION";

pub const coreclr_enable_profiling_env_var_name = "CORECLR_ENABLE_PROFILING";
pub const coreclr_profiler_env_var_name = "CORECLR_PROFILER";
pub const coreclr_profiler_path_env_var_name = "CORECLR_PROFILER_PATH";
pub const dotnet_additional_deps_env_var_name = "DOTNET_ADDITIONAL_DEPS";
pub const dotnet_shared_store_env_var_name = "DOTNET_SHARED_STORE";
pub const dotnet_startup_hooks_env_var_name = "DOTNET_STARTUP_HOOKS";
pub const otel_auto_home_env_var_name = "OTEL_DOTNET_AUTO_HOME";

pub const dotnet_path_prefix = "/__dash0__/instrumentation/dotnet";

const injection_happened_msg = "injecting the .NET OpenTelemetry instrumentation";
var injection_happened_msg_has_been_printed = false;

fn initIsEnabled(env_vars: [](types.NullTerminatedString)) void {
    if (cache.injector_cache.experimental_dotnet_injection_enabled == null) {
        cache.injector_cache.experimental_dotnet_injection_enabled =
            env.isTrue(env_vars, dash0_experimental_dotnet_injection_env_var_name);
    }
}

pub fn isEnabled(env_vars: [](types.NullTerminatedString)) bool {
    if (cache.injector_cache.experimental_dotnet_injection_enabled == null) {
        initIsEnabled(env_vars);
    }
    return cache.injector_cache.experimental_dotnet_injection_enabled orelse false;
}

pub fn getDotnetEnvVarUpdates() ?types.DotnetEnvVarUpdates {
    if (cache.injector_cache.dotnet_env_var_updates.done) {
        return cache.injector_cache.dotnet_env_var_updates.values;
    }

    if (cache.injector_cache.libc_flavor == null) {
        cache.injector_cache.libc_flavor = getLibCFlavor();
    }

    if (cache.injector_cache.libc_flavor == types.LibCFlavor.UNKNOWN) {
        print.printError("Cannot determine LibC flavor", .{});
        cache.injector_cache.dotnet_env_var_updates = cache.CachedDotnetEnvVarUpdates{
            .values = null,
            .done = true,
        };
        return null;
    }

    if (cache.injector_cache.libc_flavor) |flavor| {
        const dotnet_env_var_updates = determineDotnetValues(flavor, builtin.cpu.arch) catch |err| {
            print.printError("Cannot determine .NET environment variables: {}", .{err});
            cache.injector_cache.dotnet_env_var_updates = cache.CachedDotnetEnvVarUpdates{
                .values = null,
                .done = true,
            };
            return null;
        };

        const paths_to_check = [_]types.NullTerminatedString{
            dotnet_env_var_updates.coreclr_profiler_path.value,
            dotnet_env_var_updates.dotnet_additional_deps.value,
            dotnet_env_var_updates.otel_auto_home.value,
            dotnet_env_var_updates.dotnet_shared_store.value,
            dotnet_env_var_updates.dotnet_startup_hooks.value,
        };
        for (paths_to_check) |p| {
            std.fs.cwd().access(std.mem.span(p), .{}) catch |err| {
                print.printError("Skipping injection of injecting the .NET OpenTelemetry instrumentation because of an issue accessing {s}: {}", .{ p, err });
                cache.injector_cache.dotnet_env_var_updates = cache.CachedDotnetEnvVarUpdates{
                    .values = null,
                    .done = true,
                };
                return null;
            };
        }

        cache.injector_cache.dotnet_env_var_updates = cache.CachedDotnetEnvVarUpdates{
            .values = dotnet_env_var_updates,
            .done = true,
        };
        return dotnet_env_var_updates;
    }

    unreachable;
}

test "getDotnetValues: should return null value if the profiler path cannot be accessed" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const dotnet_env_var_updates_optional = getDotnetEnvVarUpdates();
    try testing.expect(dotnet_env_var_updates_optional == null);
}

test "getDotnetValues: should cache and return values if the profiler path can be accessed" {
    try test_util.createDummyDotnetInstrumentation();
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    // set a dummy value for the libc flavor just for the test, will be reverted after the test via
    // `defer cache.injector_cache = cache.emptyInjectorCache()`
    cache.injector_cache.libc_flavor = types.LibCFlavor.GNU_LIBC;

    const dotnet_env_var_updates_optional = getDotnetEnvVarUpdates();
    try testing.expect(dotnet_env_var_updates_optional != null);
    try testing.expectEqualStrings(
        "1",
        std.mem.span(dotnet_env_var_updates_optional.?.coreclr_enable_profiling.value),
    );
    try testing.expectEqualStrings(
        "{918728DD-259F-4A6A-AC2B-B85E1B658318}",
        std.mem.span(dotnet_env_var_updates_optional.?.coreclr_profiler.value),
    );
    try testing.expectStringStartsWith(
        std.mem.span(dotnet_env_var_updates_optional.?.coreclr_profiler_path.value),
        "/__dash0__/instrumentation/dotnet/glibc/linux-",
    );
    try testing.expectStringEndsWith(
        std.mem.span(dotnet_env_var_updates_optional.?.coreclr_profiler_path.value),
        "OpenTelemetry.AutoInstrumentation.Native.so",
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/AdditionalDeps",
        std.mem.span(dotnet_env_var_updates_optional.?.dotnet_additional_deps.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/store",
        std.mem.span(dotnet_env_var_updates_optional.?.dotnet_shared_store.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll",
        std.mem.span(dotnet_env_var_updates_optional.?.dotnet_startup_hooks.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc",
        std.mem.span(dotnet_env_var_updates_optional.?.otel_auto_home.value),
    );

    try testing.expect(cache.injector_cache.dotnet_env_var_updates.done);
    try testing.expectEqualStrings("1", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.coreclr_enable_profiling.value));
    try testing.expectEqualStrings("{918728DD-259F-4A6A-AC2B-B85E1B658318}", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.coreclr_profiler.value));
    try testing.expectStringStartsWith(
        std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.coreclr_profiler_path.value),
        "/__dash0__/instrumentation/dotnet/glibc/linux-",
    );
    try testing.expectStringEndsWith(
        std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.coreclr_profiler_path.value),
        "OpenTelemetry.AutoInstrumentation.Native.so",
    );
    try testing.expectEqualStrings("/__dash0__/instrumentation/dotnet/glibc/AdditionalDeps", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.dotnet_additional_deps.value));
    try testing.expectEqualStrings("/__dash0__/instrumentation/dotnet/glibc/store", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.dotnet_shared_store.value));
    try testing.expectEqualStrings("/__dash0__/instrumentation/dotnet/glibc/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.dotnet_startup_hooks.value));
    try testing.expectEqualStrings("/__dash0__/instrumentation/dotnet/glibc", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.otel_auto_home.value));
}

fn getLibCFlavor() types.LibCFlavor {
    const proc_self_exe_path = "/proc/self/exe";
    return doGetLibCFlavor(proc_self_exe_path) catch |err| {
        print.printError("Cannot determine LibC flavor from ELF metadata of \"{s}\": {}", .{ proc_self_exe_path, err });
        return types.LibCFlavor.UNKNOWN;
    };
}

fn doGetLibCFlavor(proc_self_exe_path: []const u8) !types.LibCFlavor {
    const proc_self_exe_file = std.fs.openFileAbsolute(proc_self_exe_path, .{ .mode = .read_only }) catch |err| {
        print.printError("Cannot open \"{s}\": {}", .{ proc_self_exe_path, err });
        return types.LibCFlavor.UNKNOWN;
    };
    defer proc_self_exe_file.close();

    const elf_header = std.elf.Header.read(proc_self_exe_file) catch |err| {
        print.printError("Cannot read ELF header from  \"{s}\": {}", .{ proc_self_exe_path, err });
        return types.LibCFlavor.UNKNOWN;
    };

    if (!elf_header.is_64) {
        print.printError("ELF header from \"{s}\" seems to not be from a  64 bit binary", .{proc_self_exe_path});
        return error.ElfNot64Bit;
    }

    var sections_header_iterator = elf_header.section_header_iterator(proc_self_exe_file);

    var dynamic_symbols_table_offset: u64 = 0;
    var dynamic_symbols_table_size: u64 = 0;

    while (try sections_header_iterator.next()) |section_header| {
        switch (section_header.sh_type) {
            std.elf.SHT_DYNAMIC => {
                dynamic_symbols_table_offset = section_header.sh_offset;
                dynamic_symbols_table_size = section_header.sh_size;
            },
            else => {
                // Ignore this section
            },
        }
    }

    if (dynamic_symbols_table_offset == 0) {
        print.printError("No dynamic section found in ELF metadata when inspecting \"{s}\"", .{proc_self_exe_path});
        return error.ElfDynamicSymbolTableNotFound;
    }

    // Look for DT_NEEDED entries in the Dynamic table, they state which libraries were
    // used at compilation step. Examples:
    //
    // Java + GNU LibC
    //
    // $ readelf -Wd /usr/bin/java
    // Dynamic section at offset 0xfd28 contains 30 entries:
    //   Tag        Type                         Name/Value
    //  0x0000000000000001 (NEEDED)             Shared library: [libz.so.1]
    //  0x0000000000000001 (NEEDED)             Shared library: [libjli.so]
    //  0x0000000000000001 (NEEDED)             Shared library: [libc.so.6]
    //
    // Java + musl
    //
    // $ readelf -Wd /usr/bin/java
    // Dynamic section at offset 0xfd18 contains 33 entries:
    //   Tag        Type                         Name/Value
    //  0x0000000000000001 (NEEDED)             Shared library: [libjli.so]
    //  0x0000000000000001 (NEEDED)             Shared library: [libc.musl-aarch64.so.1]

    // Read dynamic section
    // Read dynamic section
    try proc_self_exe_file.seekTo(dynamic_symbols_table_offset);
    const dynamic_symbol_count = dynamic_symbols_table_size / @sizeOf(std.elf.Elf64_Dyn);
    const dynamic_symbols = try std.heap.page_allocator.alloc(std.elf.Elf64_Dyn, dynamic_symbol_count);
    defer std.heap.page_allocator.free(dynamic_symbols);
    _ = try proc_self_exe_file.read(std.mem.sliceAsBytes(dynamic_symbols));

    // Find string table address (DT_STRTAB)
    var strtab_addr: u64 = 0;
    for (dynamic_symbols) |dyn| {
        if (dyn.d_tag == std.elf.DT_STRTAB) {
            strtab_addr = dyn.d_val;
            break;
        }
    }
    if (strtab_addr == 0) {
        print.printError("No string table found when inspecting ELF binary \"{s}\"", .{proc_self_exe_path});
        return error.ElfStringsTableNotFound;
    }

    sections_header_iterator.index = 0;
    var string_table_offset: u64 = 0;
    while (try sections_header_iterator.next()) |shdr| {
        if (shdr.sh_type == std.elf.SHT_STRTAB and shdr.sh_addr == strtab_addr) {
            string_table_offset = shdr.sh_offset;
            break;
        }
    }

    if (string_table_offset == 0) {
        // Fallback: Use program headers if section headers don’t map it
        try proc_self_exe_file.seekTo(elf_header.phoff);
        const phdrs = try std.heap.page_allocator.alloc(std.elf.Elf64_Phdr, elf_header.phnum);
        defer std.heap.page_allocator.free(phdrs);
        _ = try proc_self_exe_file.read(std.mem.sliceAsBytes(phdrs));
        for (phdrs) |phdr| {
            if (phdr.p_type == std.elf.PT_LOAD and phdr.p_vaddr <= strtab_addr and strtab_addr < phdr.p_vaddr + phdr.p_filesz) {
                string_table_offset = phdr.p_offset + (strtab_addr - phdr.p_vaddr);
                break;
            }
        }
        if (string_table_offset == 0) {
            print.printError("Could not map string table address when inspecting ELF binary \"{s}\"", .{proc_self_exe_path});
            return error.ElfStringsTableNotFound;
        }
    }

    for (dynamic_symbols) |dynamic_symbol| {
        if (dynamic_symbol.d_tag == std.elf.DT_NULL) {
            break;
        }

        if (dynamic_symbol.d_tag == std.elf.DT_NEEDED) {
            const string_offset = string_table_offset + dynamic_symbol.d_val;
            try proc_self_exe_file.seekTo(string_offset);

            // Read null-terminated string (up to 256 bytes max for simplicity)
            var buffer: [256]u8 = undefined;
            const bytes_read = try proc_self_exe_file.read(&buffer);
            const lib_name = buffer[0..bytes_read];

            if (std.mem.indexOf(u8, lib_name, "musl")) |_| {
                print.printDebug("Identified libc flavor \"musl\" from inspecting \"{s}\"", .{proc_self_exe_path});
                return types.LibCFlavor.MUSL;
            }

            if (std.mem.indexOf(u8, lib_name, "libc.so.6")) |_| {
                print.printDebug("Identified libc flavor \"glibc\" from inspecting \"{s}\"", .{proc_self_exe_path});
                return types.LibCFlavor.GNU_LIBC;
            }
        }
    }

    print.printDebug("No libc flavor could be identified from inspecting \"{s}\"", .{proc_self_exe_path});
    return types.LibCFlavor.UNKNOWN;
}

test "doGetLibCFlavor: should return libc flavor unknown when file does not exist" {
    const libc_flavor = try doGetLibCFlavor("/does/not/exist");
    try testing.expectEqual(libc_flavor, .UNKNOWN);
}

test "doGetLibCFlavor: should return libc flavor unknown when file is not an ELF binary" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/not-an-elf-binary" });
    defer allocator.free(absolute_path_to_binary);
    const libc_flavor = try doGetLibCFlavor(absolute_path_to_binary);
    try testing.expectEqual(libc_flavor, .UNKNOWN);
}

test "doGetLibCFlavor: should identify musl libc flavor (arm64)" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/dotnet-app-arm64-musl" });
    defer allocator.free(absolute_path_to_binary);
    const libc_flavor = try doGetLibCFlavor(absolute_path_to_binary);
    try testing.expectEqual(libc_flavor, .MUSL);
}

test "doGetLibCFlavor: should identify musl libc flavor (x86_64)" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/dotnet-app-x86_64-musl" });
    defer allocator.free(absolute_path_to_binary);
    const libc_flavor = try doGetLibCFlavor(absolute_path_to_binary);
    try testing.expectEqual(libc_flavor, .MUSL);
}

test "doGetLibCFlavor: should identify glibc libc flavor (arm64)" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/dotnet-app-arm64-glibc" });
    defer allocator.free(absolute_path_to_binary);
    const libc_flavor = try doGetLibCFlavor(absolute_path_to_binary);
    try testing.expectEqual(libc_flavor, .GNU_LIBC);
}

test "doGetLibCFlavor: should identify glibc libc flavor (x86_64)" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/dotnet-app-x86_64-glibc" });
    defer allocator.free(absolute_path_to_binary);
    const libc_flavor = try doGetLibCFlavor(absolute_path_to_binary);
    try testing.expectEqual(libc_flavor, .GNU_LIBC);
}

fn determineDotnetValues(
    libc_flavor: types.LibCFlavor,
    architecture: std.Target.Cpu.Arch,
) DotnetError!types.DotnetEnvVarUpdates {
    const libc_flavor_prefix =
        switch (libc_flavor) {
            .GNU_LIBC => "glibc",
            .MUSL => "musl",
            else => return error.UnknownLibCFlavor,
        };
    const platform =
        switch (libc_flavor) {
            .GNU_LIBC => switch (architecture) {
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

    const coreclr_profiler_path = try std.fmt.allocPrintZ(std.heap.page_allocator, "{s}/{s}/{s}/OpenTelemetry.AutoInstrumentation.Native.so", .{
        dotnet_path_prefix, libc_flavor_prefix, platform,
    });
    const dotnet_additional_deps = try std.fmt.allocPrintZ(std.heap.page_allocator, "{s}/{s}/AdditionalDeps", .{
        dotnet_path_prefix, libc_flavor_prefix,
    });
    const dotnet_shared_store = try std.fmt.allocPrintZ(std.heap.page_allocator, "{s}/{s}/store", .{
        dotnet_path_prefix, libc_flavor_prefix,
    });
    const dotnet_startup_hooks = try std.fmt.allocPrintZ(std.heap.page_allocator, "{s}/{s}/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll", .{
        dotnet_path_prefix, libc_flavor_prefix,
    });
    const otel_auto_home = try std.fmt.allocPrintZ(std.heap.page_allocator, "{s}/{s}", .{ dotnet_path_prefix, libc_flavor_prefix });

    if (!injection_happened_msg_has_been_printed) {
        print.printMessage(injection_happened_msg, .{});
        injection_happened_msg_has_been_printed = true;
    }
    return .{
        //
        .coreclr_enable_profiling = types.EnvVarUpdate{
            .value = "1",
            .replace = false,
            .index = 0,
        },
        .coreclr_profiler = types.EnvVarUpdate{
            .value = "{918728DD-259F-4A6A-AC2B-B85E1B658318}",
            .replace = false,
            .index = 0,
        },
        .coreclr_profiler_path = types.EnvVarUpdate{
            .value = coreclr_profiler_path,
            .replace = false,
            .index = 0,
        },
        .dotnet_additional_deps = types.EnvVarUpdate{
            .value = dotnet_additional_deps,
            .replace = false,
            .index = 0,
        },
        .dotnet_shared_store = types.EnvVarUpdate{
            .value = dotnet_shared_store,
            .replace = false,
            .index = 0,
        },
        .dotnet_startup_hooks = types.EnvVarUpdate{
            .value = dotnet_startup_hooks,
            .replace = false,
            .index = 0,
        },
        .otel_auto_home = types.EnvVarUpdate{
            .value = otel_auto_home,
            .replace = false,
            .index = 0,
        },
    };
}

test "determineDotnetValues: should return error for unsupported CPU architecture" {
    try testing.expectError(error.UnsupportedCpuArchitecture, determineDotnetValues(.GNU_LIBC, .powerpc64le));
}

test "determineDotnetValues: should return error for unknown libc flavor" {
    try testing.expectError(error.UnknownLibCFlavor, determineDotnetValues(.UNKNOWN, .x86_64));
}

test "determineDotnetValues: should return values for glibc/x86_64" {
    const dotnet_env_var_updates = try determineDotnetValues(.GNU_LIBC, .x86_64);
    try testing.expectEqualStrings(
        "1",
        std.mem.span(dotnet_env_var_updates.coreclr_enable_profiling.value),
    );
    try testing.expectEqualStrings(
        "{918728DD-259F-4A6A-AC2B-B85E1B658318}",
        std.mem.span(dotnet_env_var_updates.coreclr_profiler.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/linux-x64/OpenTelemetry.AutoInstrumentation.Native.so",
        std.mem.span(dotnet_env_var_updates.coreclr_profiler_path.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/AdditionalDeps",
        std.mem.span(dotnet_env_var_updates.dotnet_additional_deps.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/store",
        std.mem.span(dotnet_env_var_updates.dotnet_shared_store.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll",
        std.mem.span(dotnet_env_var_updates.dotnet_startup_hooks.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc",
        std.mem.span(dotnet_env_var_updates.otel_auto_home.value),
    );
}

test "determineDotnetValues: should return values for glibc/arm64" {
    const dotnet_env_var_updates = try determineDotnetValues(.GNU_LIBC, .aarch64);
    try testing.expectEqualStrings(
        "1",
        std.mem.span(dotnet_env_var_updates.coreclr_enable_profiling.value),
    );
    try testing.expectEqualStrings(
        "{918728DD-259F-4A6A-AC2B-B85E1B658318}",
        std.mem.span(dotnet_env_var_updates.coreclr_profiler.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/linux-arm64/OpenTelemetry.AutoInstrumentation.Native.so",
        std.mem.span(dotnet_env_var_updates.coreclr_profiler_path.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/AdditionalDeps",
        std.mem.span(dotnet_env_var_updates.dotnet_additional_deps.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/store",
        std.mem.span(dotnet_env_var_updates.dotnet_shared_store.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll",
        std.mem.span(dotnet_env_var_updates.dotnet_startup_hooks.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc",
        std.mem.span(dotnet_env_var_updates.otel_auto_home.value),
    );
}

test "determineDotnetValues: should return values for musl/x86_64" {
    const dotnet_env_var_updates = try determineDotnetValues(.MUSL, .x86_64);
    try testing.expectEqualStrings(
        "1",
        std.mem.span(dotnet_env_var_updates.coreclr_enable_profiling.value),
    );
    try testing.expectEqualStrings(
        "{918728DD-259F-4A6A-AC2B-B85E1B658318}",
        std.mem.span(dotnet_env_var_updates.coreclr_profiler.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/linux-musl-x64/OpenTelemetry.AutoInstrumentation.Native.so",
        std.mem.span(dotnet_env_var_updates.coreclr_profiler_path.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/AdditionalDeps",
        std.mem.span(dotnet_env_var_updates.dotnet_additional_deps.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/store",
        std.mem.span(dotnet_env_var_updates.dotnet_shared_store.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll",
        std.mem.span(dotnet_env_var_updates.dotnet_startup_hooks.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl",
        std.mem.span(dotnet_env_var_updates.otel_auto_home.value),
    );
}

test "determineDotnetValues: should return values for musl/arm64" {
    const dotnet_env_var_updates = try determineDotnetValues(.MUSL, .aarch64);
    try testing.expectEqualStrings(
        "1",
        std.mem.span(dotnet_env_var_updates.coreclr_enable_profiling.value),
    );
    try testing.expectEqualStrings(
        "{918728DD-259F-4A6A-AC2B-B85E1B658318}",
        std.mem.span(dotnet_env_var_updates.coreclr_profiler.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/linux-musl-arm64/OpenTelemetry.AutoInstrumentation.Native.so",
        std.mem.span(dotnet_env_var_updates.coreclr_profiler_path.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/AdditionalDeps",
        std.mem.span(dotnet_env_var_updates.dotnet_additional_deps.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/store",
        std.mem.span(dotnet_env_var_updates.dotnet_shared_store.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll",
        std.mem.span(dotnet_env_var_updates.dotnet_startup_hooks.value),
    );
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/musl",
        std.mem.span(dotnet_env_var_updates.otel_auto_home.value),
    );
}
