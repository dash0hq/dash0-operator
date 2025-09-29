const builtin = @import("builtin");
const std = @import("std");
const testing = std.testing;

const auxv = @import("auxv.zig");
const elf = @import("elf.zig");
const print = @import("print.zig");
const test_util = @import("test_util.zig");
const types = @import("types.zig");

const proc_self_exe_path: []const u8 = "/proc/self/exe";
const readable_executable_private = "r-xp";
const readable_private = "r--p";
const dlsym_function_name = "dlsym";
const setenv_function_name = "setenv";
const environ_symbol_name = "__environ";

const LibCNameAndFlavor = struct {
    flavor: types.LibCFlavor,
    name: []const u8,
};

const LibCError = error{
    CannotAllocateMemory,
    CannotFindAtBase,
    CannotFindElfDynamicSymbolTableOffset,
    CannotFindElfDynamicSymbolTableSize,
    CannotFindDlSymSymbol,
    CannotFindEnvironSymbol,
    CannotFindLibcMemoryRange,
    CannotFindSetenvSymbol,
    CannotOpenLibc,
    PermissionsNotFoundInMaps,
    UnknownLibCFlavor,
};

const UnknownLibC = LibCNameAndFlavor{
    .flavor = types.LibCFlavor.UNKNOWN,
    .name = "",
};

const DlsymLookupResult = struct {
    found: bool,
    libc_info: types.LibCInfo,
};

const AuxiliaryPointers = struct {
    base: usize,
    phdr: usize,
};

/// Look up which libc flavor (glibc vs. musl) is used (if any), and the memory addresses of key libc facilities we need
/// (i.e. __environ, setenv).
///
/// This is performed in three steps:
/// 1. Inspect the Elf metadata of the program's executable ("/proc/self/exe"), using the DT_NEEDED symbols for the
///    libraries that must be linked.
/// 2. Look up pointer to the `dlsym` function in the libc loaded by the program, as springboard for the next look ups.
///    We use a simplified version of the Elf support in Zig's std (`dynamic_library`) because we do not want to have to
///    support the infinite number of corner cases of the various libc flavors and versions.
/// 3. Use the loaded libc's `dlsym` function to look up the symbols we need (setenv, __environ).
pub fn getLibCInfo() !types.LibCInfo {
    const libc_name_and_flavor = try getLibCNameAndFlavor(proc_self_exe_path);
    return getLibCMemoryLocations(libc_name_and_flavor);
}

///  Inspect the Elf metadata of the program's executable ("/proc/self/exe"), using the DT_NEEDED symbols for the
///  libraries that must be linked. We use the executable's file instead of its in-memory mapping to avoid annoyances
///  with looking up the in-memory location of the Elf header (it is never in memory at location 0 is the virtual memory
///  space of the program, is is usually offset by 40 bytes).
// TODO MM: Rewrite this to use in-memory, finding u=out the Elf header location using auxv? If that would work, we
// could make this logic allocation-free.
fn getLibCNameAndFlavor(self_exe_path: []const u8) !LibCNameAndFlavor {
    const self_exe_file =
        std.fs.openFileAbsolute(self_exe_path, .{ .mode = .read_only }) catch |err| {
            print.printError("Cannot open \"{s}\": {}", .{ self_exe_path, err });
            return UnknownLibC;
        };
    defer self_exe_file.close();

    const elf_header = std.elf.Header.read(self_exe_file) catch |err| {
        print.printError("Cannot read ELF header from  \"{s}\": {}", .{ self_exe_path, err });
        return UnknownLibC;
    };

    if (!elf_header.is_64) {
        print.printError("ELF header from \"{s}\" seems to not be from a 64 bit binary", .{self_exe_path});
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
        print.printError("No dynamic section found in ELF metadata when inspecting \"{s}\"", .{self_exe_path});
        return error.ElfDynamicSymbolTableNotFound;
    }

    // Look for DT_NEEDED entries in the dynamic table, they state which libraries were used when the binary has been
    // compiled. Some Examples:
    //
    // JVM with GNU libc
    // -----------------
    // $ readelf -Wd /usr/bin/java
    // Dynamic section at offset 0xfd28 contains 30 entries:
    //   Tag        Type                         Name/Value
    //  0x0000000000000001 (NEEDED)             Shared library: [libz.so.1]
    //  0x0000000000000001 (NEEDED)             Shared library: [libjli.so]
    //  0x0000000000000001 (NEEDED)             Shared library: [libc.so.6]
    //
    // JVM with musl libc
    // ------------------
    // $ readelf -Wd /usr/bin/java
    // Dynamic section at offset 0xfd18 contains 33 entries:
    //   Tag        Type                         Name/Value
    //  0x0000000000000001 (NEEDED)             Shared library: [libjli.so]
    //  0x0000000000000001 (NEEDED)             Shared library: [libc.musl-aarch64.so.1]

    // read dynamic section
    try self_exe_file.seekTo(dynamic_symbols_table_offset);
    const dynamic_symbol_count = dynamic_symbols_table_size / @sizeOf(std.elf.Elf64_Dyn);
    const dynamic_symbols = try std.heap.page_allocator.alloc(std.elf.Elf64_Dyn, dynamic_symbol_count);
    defer std.heap.page_allocator.free(dynamic_symbols);
    _ = try self_exe_file.read(std.mem.sliceAsBytes(dynamic_symbols));

    // find string table address (DT_STRTAB)
    var strtab_addr: u64 = 0;
    for (dynamic_symbols) |dyn| {
        if (dyn.d_tag == std.elf.DT_STRTAB) {
            strtab_addr = dyn.d_val;
            break;
        }
    }
    if (strtab_addr == 0) {
        print.printError("No string table found when inspecting ELF binary \"{s}\"", .{self_exe_path});
        return error.ElfStringsTableNotFound;
    }

    sections_header_iterator.index = 0;
    var string_table_offset: u64 = 0;
    var string_table_size: u64 = 0;
    while (try sections_header_iterator.next()) |shdr| {
        if (shdr.sh_type == std.elf.SHT_STRTAB and shdr.sh_addr == strtab_addr) {
            string_table_offset = shdr.sh_offset;
            string_table_size = shdr.sh_size;
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
            print.printError("Could not map string table address when inspecting ELF binary \"{s}\"", .{self_exe_path});
            return error.ElfStringsTableNotFound;
        }
    }

    for (dynamic_symbols) |dynamic_symbol| {
        if (dynamic_symbol.d_tag == std.elf.DT_NULL) {
            // End of the dynamic symbols
            break;
        }

        if (dynamic_symbol.d_tag == std.elf.DT_NEEDED) {
            const string_offset = string_table_offset + dynamic_symbol.d_val;
            try self_exe_file.seekTo(string_offset);

            const maybe_lib_name = try self_exe_file.reader().readUntilDelimiterOrEofAlloc(std.heap.page_allocator, '\x00', 256);
            if (maybe_lib_name) |lib_name| {
                if (std.mem.indexOf(u8, lib_name, "musl")) |_| {
                    // lib_name exists on the stack, we need to allocate a string with the same content on the heap
                    const lib_name_owned = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}", .{lib_name}) catch |err| {
                        print.printError("Failed to allocate memory for libc name: {}", .{err});
                        return error.CannotAllocateMemory;
                    };
                    return LibCNameAndFlavor{ .flavor = types.LibCFlavor.MUSL, .name = lib_name_owned };
                }

                if (std.mem.indexOf(u8, lib_name, "libc.so.6")) |_| {
                    print.printMessage("found a libc at {s}", .{lib_name});
                    // lib_name exists on the stack, we need to allocate a string with the same content on the heap
                    const lib_name_owned = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}", .{lib_name}) catch |err| {
                        print.printError("Failed to allocate memory for libc name: {}", .{err});
                        return error.CannotAllocateMemory;
                    };
                    return LibCNameAndFlavor{ .flavor = types.LibCFlavor.GNU, .name = lib_name_owned };
                }
            }
        }
    }

    return UnknownLibC;
}

test "getLibCNameAndFlavor: should return libc flavor unknown when file does not exist" {
    const lib_c = try getLibCNameAndFlavor("/does/not/exist");
    try testing.expectEqual(lib_c.flavor, .UNKNOWN);
}

test "getLibCNameAndFlavor: should return libc flavor unknown when file is not an ELF binary" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/not-an-elf-binary" });
    defer allocator.free(absolute_path_to_binary);
    const lib_c = try getLibCNameAndFlavor(absolute_path_to_binary);
    try testing.expectEqual(lib_c.flavor, .UNKNOWN);
}

test "getLibCNameAndFlavor: should identify musl libc flavor (arm64)" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/dotnet-app-arm64-musl" });
    defer allocator.free(absolute_path_to_binary);
    const lib_c = try getLibCNameAndFlavor(absolute_path_to_binary);
    try testing.expectEqual(lib_c.flavor, .MUSL);
}

test "getLibCNameAndFlavor: should identify musl libc flavor (x86_64)" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/dotnet-app-x86_64-musl" });
    defer allocator.free(absolute_path_to_binary);
    const lib_c = try getLibCNameAndFlavor(absolute_path_to_binary);
    try testing.expectEqual(lib_c.flavor, .MUSL);
}

test "getLibCNameAndFlavor: should identify glibc libc flavor (arm64)" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/dotnet-app-arm64-glibc" });
    defer allocator.free(absolute_path_to_binary);
    const lib_c = try getLibCNameAndFlavor(absolute_path_to_binary);
    try testing.expectEqual(lib_c.flavor, .GNU);
}

test "getLibCNameAndFlavor: should identify glibc libc flavor (x86_64)" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/dotnet-app-x86_64-glibc" });
    defer allocator.free(absolute_path_to_binary);
    const lib_c = try getLibCNameAndFlavor(absolute_path_to_binary);
    try testing.expectEqual(lib_c.flavor, .GNU);
}

fn getLibCMemoryLocations(libc_name_and_flavor: LibCNameAndFlavor) !types.LibCInfo {
    switch (libc_name_and_flavor.flavor) {
        types.LibCFlavor.GNU => {
            return findGlibcMemoryRangeAndLookupMemoryLocations(libc_name_and_flavor);
        },
        types.LibCFlavor.MUSL => {
            const at_base = auxv.getauxval(std.elf.AT_BASE);
            if (at_base == 0) {
                print.printError("Cannot find AT_BASE in /proc/self/auxv", .{});
                return error.CannotFindAtBase;
            }

            return findMuslMemoryRangeAndLookupMemoryLocations(libc_name_and_flavor, at_base);
        },
        else => return error.UnknownLibCFlavor,
    }
}

fn findGlibcMemoryRangeAndLookupMemoryLocations(libc_name_and_flavor: LibCNameAndFlavor) !types.LibCInfo {
    var maps_file = try std.fs.openFileAbsolute("/proc/self/maps", .{});
    defer maps_file.close();

    // Find the end of the memory range of the linker using /proc/self/maps
    var buf_reader = std.io.bufferedReader(maps_file.reader());
    var in_stream = buf_reader.reader();
    var buf: [1024]u8 = undefined;

    // On a lot of modern distributions, the name returned by getLibCNameAndFlavor (e.g. "libc.so.6"), and it will
    // appear verbatim in /proc/self/maps. But on other (older) distributions (Debian Bullseye for example), libc.so.6
    // is a symbolic link to the actual file, i.e. a link to libc-2.31.so or similar; and /proc/self/maps has no entry
    // for "libc.so.6", only one for libc-2.31.so. The linker has resolved the symbolic link libc.so.6 by finding that
    // file system entry in its standard libary search paths before /proc/self/maps is provided. To avoid having to
    // reimplement the library search path logic of the linker, we will first try to find a /proc/self/maps entry for
    // the exact name (e.g. libc.so.6) and look for dlsym in the associcated memory range. If that fails, we will try
    // to find dlsym in all memory ranges referenced by and /proc/self/maps entry that has the correct permissions.
    //
    // First pass/fast path: look for an entry in /proc/self/maps that matches the libc name.
    print.printMessage("FIRST PASS OVER /proc/self/maps, looking for library name {s}", .{libc_name_and_flavor.name});
    while (try in_stream.readUntilDelimiterOrEof(&buf, '\n')) |line| {
        print.printMessage("{s}", .{line});
        // Parse the address range (e.g., "55b3e9c1a000-55b3e9e1a000 ...")
        // address           perms offset  dev   inode   pathname
        // aaaac5560000-aaaaca1fd000 r-xp 00000000 00:11e 8682241 /usr/local/bin/node
        var slices = std.mem.splitAny(u8, line, " ");
        const memory_range = slices.first();

        const permissions = slices.next() orelse return error.PermissionsNotFoundInMaps;
        if (!memoryRangeHasMatchingPermissions(permissions)) {
            continue;
        }

        if (!std.mem.endsWith(u8, slices.rest(), libc_name_and_flavor.name)) {
            continue;
        }

        print.printMessage("We found a match! -- {s}", .{line});
        if (std.mem.indexOf(u8, memory_range, "-")) |range_separator_index| {
            const start_memory_range = try std.fmt.parseInt(usize, memory_range[0..range_separator_index], 16);
            const end_memory_range = try std.fmt.parseInt(usize, memory_range[range_separator_index + 1 ..], 16);
            if (tryToFindSymbolsInMemoryRange(
                libc_name_and_flavor,
                start_memory_range,
                end_memory_range,
            )) |libc_info| {
                print.printMessage("dlsym lookup via {s} succeeded (permissions: {s})", .{ libc_name_and_flavor.name, permissions });
                return libc_info;
            } else |err| {
                print.printMessage("dlsym lookup via {s} failed (permissions: {s}): {}", .{ libc_name_and_flavor.name, permissions, err });
                continue;
            }
        }
    }

    // TODO add the second pass for musl as well!
    // Second pass: try the dlsym lookup for all /proc/self/maps memory rnages with matching permissions.
    try maps_file.seekTo(0);
    buf_reader = std.io.bufferedReader(maps_file.reader());
    in_stream = buf_reader.reader();
    print.printMessage("SECOND PASS OVER /proc/self/maps", .{});
    while (try in_stream.readUntilDelimiterOrEof(&buf, '\n')) |line| {
        // Parse the address range (e.g., "55b3e9c1a000-55b3e9e1a000 ...")
        // address           perms offset  dev   inode   pathname
        // aaaac5560000-aaaaca1fd000 r-xp 00000000 00:11e 8682241 /usr/local/bin/node
        var slices = std.mem.splitAny(u8, line, " ");
        const memory_range = slices.first();

        // TODO the following check should be correct, but it breaks the instrumentation tests for Debian bullsey base
        // images, where the second-pass fallback is used.
        const permissions = slices.next() orelse return error.PermissionsNotFoundInMaps;
        if (!memoryRangeHasMatchingPermissions(permissions)) {
            continue;
        }

        if (std.mem.indexOf(u8, memory_range, "-")) |range_separator_index| {
            const start_memory_range_hex = memory_range[0..range_separator_index];
            const end_memory_range_hex = memory_range[range_separator_index + 1 ..];
            print.printMessage("attempting dlsym lookup for {s}-{s}", .{ start_memory_range_hex, end_memory_range_hex });
            const start_memory_range = try std.fmt.parseInt(usize, memory_range[0..range_separator_index], 16);
            const end_memory_range = try std.fmt.parseInt(usize, memory_range[range_separator_index + 1 ..], 16);
            if (tryToFindSymbolsInMemoryRange(
                libc_name_and_flavor,
                start_memory_range,
                end_memory_range,
            )) |libc_info| {
                print.printMessage(
                    "dlsym lookup for /proc/self/maps entry \"{s}\" succeeded for memory range {s}-{s} (permissions {s})",
                    .{
                        slices.rest(),
                        start_memory_range_hex,
                        end_memory_range_hex,
                        permissions,
                    },
                );
                return libc_info;
            } else |err| {
                print.printMessage("dlsym lookup for /proc/self/maps entry \"{s}\" failed: {}", .{ slices.rest(), err });
                continue;
            }
        }
    }

    return error.CannotFindLibcMemoryRange;
}

fn findMuslMemoryRangeAndLookupMemoryLocations(libc_name_and_flavor: LibCNameAndFlavor, at_base: usize) !types.LibCInfo {
    // MuslC bundles the linker and the libc itself in the same .so and
    // it gets mapped in the same memory region. We can find where the
    // linker is, and so also the libc, we can look up the AT_BASE location
    // in /proc/self/auxv.
    var maps_file = try std.fs.openFileAbsolute("/proc/self/maps", .{});
    defer maps_file.close();

    // Find the end of the memory range of the linker using /proc/self/maps
    var buf_reader = std.io.bufferedReader(maps_file.reader());
    var in_stream = buf_reader.reader();
    var buf: [1024]u8 = undefined;

    while (try in_stream.readUntilDelimiterOrEof(&buf, '\n')) |line| {
        // Parse the address range (e.g., "55b3e9c1a000-55b3e9e1a000 ...")
        // address           perms offset  dev   inode   pathname
        // aaaac5560000-aaaaca1fd000 r-xp 00000000 00:11e 8682241 /usr/local/bin/node
        var slices = std.mem.splitAny(u8, line, " ");
        const memory_range = slices.first();

        const permissions = slices.next() orelse return error.PermissionsNotFoundInMaps;
        if (!memoryRangeHasMatchingPermissions(permissions)) {
            continue;
        }

        if (std.mem.indexOf(u8, memory_range, "-")) |range_separator_index| {
            const start_memory_range =
                try std.fmt.parseInt(usize, memory_range[0..range_separator_index], 16);
            if (start_memory_range == at_base) {
                const memory_range_end =
                    try std.fmt.parseInt(usize, memory_range[range_separator_index + 1 ..], 16);
                return tryToFindSymbolsInMemoryRange(libc_name_and_flavor, at_base, memory_range_end);
            }
        }
    }

    return error.CannotFindLibcMemoryRange;
}

fn memoryRangeHasMatchingPermissions(permissions: []const u8) bool {
    // Intuitively, one might thing that looking for dlsym in /proc/self/maps memory ranges with permission flags r-xp
    // (readable, not writable, executable & private i.e., copy-on-write) would be enough. But in some scenarios, the
    // memory range that actually contains dlsym has "r--p" instead. Two known cases:
    // - Node.js on x86_64/glibc with base image node:22.15.0-bookworm-slim (might only happen when running via
    //   emulation, i.e. running x86_64 on an Apple Silicon Macs or other arm64 system).
    // - JVM & Node.js on Debian Bullseye (glibc), in particular when iterating over all /proc/self/maps entries in the
    //   second pass, because the entry is not named "libc.so.6" but something like "libc-2.31.so".
    //
    // Either way, we allow /proc/self/maps entries with both "r-xp" and "r--p" permissions to be inspected for dlsym.
    return std.mem.eql(u8, permissions, readable_executable_private) or
        std.mem.eql(u8, permissions, readable_private);
}

fn tryToFindSymbolsInMemoryRange(
    libc_name_and_flavor: LibCNameAndFlavor,
    start: usize,
    end: usize,
) !types.LibCInfo {
    const linker = elf.ElfDynLib.open(start, end) catch |err| {
        print.printError("cannot open libc mapped range {x}-{x} as Elf library: {}", .{ start, end, err });
        return error.CannotOpenLibc;
    };

    const dlsym_fn =
        linker.lookup(types.DlSymFn, dlsym_function_name) orelse return error.CannotFindDlSymSymbol;

    // look up the symbols we need from the current program (handle = null)
    const maybe_setenv_fn = dlsym_fn(null, setenv_function_name);
    const maybe_environ_ptr = dlsym_fn(null, environ_symbol_name);

    const setenv_fn_ptr: types.SetenvFnPtr =
        @ptrCast(@alignCast(maybe_setenv_fn orelse return error.CannotFindSetenvSymbol));
    const environ_ptr: types.EnvironPtr =
        @ptrCast(@alignCast(maybe_environ_ptr orelse return error.CannotFindEnvironSymbol));

    return .{
        .flavor = libc_name_and_flavor.flavor,
        .name = libc_name_and_flavor.name,
        .environ_ptr = environ_ptr,
        .setenv_fn_ptr = setenv_fn_ptr,
    };
}
