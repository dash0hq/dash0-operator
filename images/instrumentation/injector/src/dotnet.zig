// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const builtin = @import("builtin");
const std = @import("std");

const alloc = @import("allocator.zig");
const print = @import("print.zig");
const types = @import("types.zig");

pub const DotNetValues = struct {
    coreclr_enable_profiling: types.NullTerminatedString,
    coreclr_profiler: types.NullTerminatedString,
    coreclr_profiler_path: types.NullTerminatedString,
    additional_deps: types.NullTerminatedString,
    shared_store: types.NullTerminatedString,
    startup_hooks: types.NullTerminatedString,
    otel_auto_home: types.NullTerminatedString,
};

const LibCFlavor = enum { UNKNOWN, GNU_LIBC, MULSC };

const dotnet_path_prefix = "/__dash0__/instrumentation/dotnet";
var experimental_dotnet_injection_enabled: ?bool = null;
// TODO we never write to cached_dotnet_values
var cached_dotnet_values: ?DotNetValues = null;
var cached_libc_flavor: ?LibCFlavor = null;

const injection_happened_msg = "injecting the .NET OpenTelemetry instrumentation";

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

pub fn getDotNetValues() ?DotNetValues {
    if (cached_dotnet_values) |val| {
        return val;
    }

    if (cached_libc_flavor == null) {
        cached_libc_flavor = getLibCFlavor();
    }

    if (cached_libc_flavor == LibCFlavor.UNKNOWN) {
        print.printError("Cannot determine LibC flavor", .{});
        return null;
    }

    if (cached_libc_flavor) |flavor| {
        return determineDotNetValues(flavor) catch |err| {
            print.printError("Cannot determine .NET environment variables: {}", .{err});
            return null;
        };
    }

    unreachable;
}

fn determineDotNetValues(flavor: LibCFlavor) !DotNetValues {
    const libc_flavor_prefix = if (flavor == LibCFlavor.GNU_LIBC) "glibc" else "musl";
    const platform =
        if (flavor == LibCFlavor.GNU_LIBC)
            (if (builtin.cpu.arch == .aarch64) "linux-arm64" else "linux-x64")
        else
            (if (builtin.cpu.arch == .aarch64) "linux-musl-arm64" else "linux-musl-x64");

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

    print.printMessage(injection_happened_msg, .{});
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

fn getLibCFlavor() LibCFlavor {
    return doGetLibCFlavor() catch |err| {
        print.printError("Cannot determine LibC flavor from ELF metadata of '/proc/self/exe': {}", .{err});
        return LibCFlavor.UNKNOWN;
    };
}

fn doGetLibCFlavor() !LibCFlavor {
    const self_exe_file = std.fs.openFileAbsolute("/proc/self/exe", .{ .mode = .read_only }) catch |err| {
        print.printError("Cannot open '/proc/self/exe': {}", .{err});
        return LibCFlavor.UNKNOWN;
    };
    defer self_exe_file.close();

    const elf_header = std.elf.Header.read(self_exe_file) catch |err| {
        print.printError("Cannot read ELF header from '/proc/self/exe': {}", .{err});
        return LibCFlavor.UNKNOWN;
    };

    if (!elf_header.is_64) {
        print.printError("ELF header from '/proc/self/exe' seems not to be the one of a 64-bit binary", .{});
        return error.ElfNot64Bit;
    }

    var sections_header_iterator = elf_header.section_header_iterator(self_exe_file);

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
        print.printError("No dynamic section found in ELF metadata from '/proc/self/exe'", .{});
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
    try self_exe_file.seekTo(dynamic_symbols_table_offset);
    const dynamic_symbol_count = dynamic_symbols_table_size / @sizeOf(std.elf.Elf64_Dyn);
    const dynamic_symbols = try alloc.page_allocator.alloc(std.elf.Elf64_Dyn, dynamic_symbol_count);
    defer alloc.page_allocator.free(dynamic_symbols);
    _ = try self_exe_file.read(std.mem.sliceAsBytes(dynamic_symbols));

    // Find string table address (DT_STRTAB)
    var strtab_addr: u64 = 0;
    for (dynamic_symbols) |dyn| {
        if (dyn.d_tag == std.elf.DT_STRTAB) {
            strtab_addr = dyn.d_val;
            break;
        }
    }
    if (strtab_addr == 0) {
        print.printError("No string table found", .{});
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
        try self_exe_file.seekTo(elf_header.phoff);
        const phdrs = try std.heap.page_allocator.alloc(std.elf.Elf64_Phdr, elf_header.phnum);
        defer std.heap.page_allocator.free(phdrs);
        _ = try self_exe_file.read(std.mem.sliceAsBytes(phdrs));
        for (phdrs) |phdr| {
            if (phdr.p_type == std.elf.PT_LOAD and phdr.p_vaddr <= strtab_addr and strtab_addr < phdr.p_vaddr + phdr.p_filesz) {
                string_table_offset = phdr.p_offset + (strtab_addr - phdr.p_vaddr);
                break;
            }
        }
        if (string_table_offset == 0) {
            print.printError("Could not map string table address", .{});
            return error.ElfStringsTableNotFound;
        }
    }

    for (dynamic_symbols) |dynamic_symbol| {
        if (dynamic_symbol.d_tag == std.elf.DT_NULL) {
            break;
        }

        if (dynamic_symbol.d_tag == std.elf.DT_NEEDED) {
            const string_offset = string_table_offset + dynamic_symbol.d_val;
            try self_exe_file.seekTo(string_offset);

            // Read null-terminated string (up to 256 bytes max for simplicity)
            var buffer: [256]u8 = undefined;
            const bytes_read = try self_exe_file.read(&buffer);
            const lib_name = buffer[0..bytes_read];

            if (std.mem.indexOf(u8, lib_name, "musl")) |_| {
                return LibCFlavor.MULSC;
            }

            if (std.mem.indexOf(u8, lib_name, "libc.so.6")) |_| {
                return LibCFlavor.GNU_LIBC;
            }
        }
    }

    return LibCFlavor.UNKNOWN;
}
