const builtin = @import("builtin");
const std = @import("std");
const testing = std.testing;

const auxv = @import("auxv.zig");
const elf = @import("elf.zig");
const print = @import("print.zig");
const test_util = @import("test_util.zig");
const types = @import("types.zig");

const proc_self_exe_path: []const u8 = "/proc/self/exe";

pub const LibCLibrary = struct {
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

const UnknownLibC = LibCLibrary{
    .flavor = types.LibCFlavor.UNKNOWN,
    .name = "",
};

const AuxiliaryPointers = struct {
    base: usize,
    phdr: usize,
};

// Look up whic LibC is used (if any), and the pointers to key libc facilities we need.
// This is performed in three steps:
// 1. Inspect the Elf metadata of the program's executable ("/proc/self/exe"), using
//    the DT_NEEDED symbols for the libraries that must be linked.
// 2. Look up pointer to the `dlsym` function in the libc loaded by the program, as
//    springboard for the next look ups. We use a simplified version of the Elf support
//    in Zig's std (`dynamic_library`) because we do not want to have to support the
//    infinite corner-cases of the various LibC flavors.
// 3. Use the loaded LibC's `dlsym` function to look up the symbols we need.
pub fn getLibc() !types.LibC {
    const libc_library = try doGetLibCLibrary(proc_self_exe_path);

    var libc_start_memory_range: usize = 0;
    var libc_end_memory_range: usize = 0;

    switch (libc_library.flavor) {
        types.LibCFlavor.MUSL => {
            const at_base = auxv.getauxval(std.elf.AT_BASE);
            if (at_base == 0) {
                print.printError("Cannot find AT_BASE in /proc/self/auxv", .{});
                return error.CannotFindAtBase;
            }

            const memory_range = try getMuslMemoryRange(at_base);
            libc_start_memory_range = memory_range.start;
            libc_end_memory_range = memory_range.end;
        },
        types.LibCFlavor.GNU => {
            const memory_range = try getGlibcMemoryRange(libc_library);
            libc_start_memory_range = memory_range.start;
            libc_end_memory_range = memory_range.end;
        },
        else => return error.UnknownLibCFlavor,
    }

    const linker = elf.ElfDynLib.open(libc_start_memory_range, libc_end_memory_range) catch |err| {
        print.printError("cannot open libc mapped range {x}-{x} as Elf library: {}", .{ libc_start_memory_range, libc_end_memory_range, err });
        return error.CannotOpenLibc;
    };

    const dlsym_fn = linker.lookup(types.DlSymFn, "dlsym") orelse return error.CannotFindDlSymSymbol;

    // Look up the symbols we need from the current program (handler = null)
    const maybe_setenv_fn = dlsym_fn(null, "setenv");
    const maybe_environ_ptr = dlsym_fn(null, "__environ");

    const setenv_fn_ptr: types.SetenvFnPtr = @ptrCast(@alignCast(maybe_setenv_fn orelse return error.CannotFindSetenvSymbol));
    const environ_ptr: types.EnvironPtr = @ptrCast(@alignCast(maybe_environ_ptr orelse return error.CannotFindEnvironSymbol));

    return .{
        .flavor = libc_library.flavor,
        .name = libc_library.name,
        .environ_ptr = environ_ptr,
        .setenv_fn_ptr = setenv_fn_ptr,
    };
}

const MemoryRange = struct {
    start: usize,
    end: usize,
};

fn getGlibcMemoryRange(libc_library: LibCLibrary) !MemoryRange {
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

        // TODO Ensure we get the memory range starting with the lowest number, as that is
        // The one with the program header, but it is not guaranteed to appear first in the
        // `/proc/self/maps` content
        // const permissions = slices.next() orelse return error.PermissionsNotFoundInMaps;
        // if (!std.mem.eql(u8, permissions, "r-xp")) {
        //     // We need the executable memory range
        //     continue;
        // }

        if (!std.mem.endsWith(u8, slices.rest(), libc_library.name)) {
            continue;
        }

        if (std.mem.indexOf(u8, memory_range, "-")) |range_separator_index| {
            const start_memory_range = try std.fmt.parseInt(usize, memory_range[0..range_separator_index], 16);
            const end_memory_range = try std.fmt.parseInt(usize, memory_range[range_separator_index + 1 ..], 16);

            return .{
                .start = start_memory_range,
                .end = end_memory_range,
            };
        }
    }

    return error.CannotFindLibcMemoryRange;
}

fn getMuslMemoryRange(at_base: usize) !MemoryRange {
    // MuslC bundles the linker and the libc itself in the same .so and
    // it gets mapped in the same memory region. We can find where the
    // linker is, and so also the LibC, we can look up the AT_BASE location
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

        if (std.mem.indexOf(u8, memory_range, "-")) |range_separator_index| {
            const start_memory_range = try std.fmt.parseInt(usize, memory_range[0..range_separator_index], 16);

            if (start_memory_range == at_base) {
                return .{
                    .start = at_base,
                    .end = try std.fmt.parseInt(usize, memory_range[range_separator_index + 1 ..], 16),
                };
            }
        }
    }

    return error.CannotFindLibcMemoryRange;
}

pub fn getLibCFlavor() types.LibCFlavor {
    const libC = doGetLibCLibrary(proc_self_exe_path) catch |err| {
        print.printError("Cannot determine LibC flavor from ELF metadata of \"{s}\": {}", .{ proc_self_exe_path, err });
        return UnknownLibC.flavor;
    };

    return libC.flavor;
}

pub fn getLibCLibrary() !LibCLibrary {
    return doGetLibCLibrary(proc_self_exe_path);
}

// Inspect the Elf metadata of the program's executable ("/proc/self/exe"), using
// the DT_NEEDED symbols for the libraries that must be linked. We use the executable's
// file instead of its in-memory mapping to avoid annoyances with looking up the
// in-memory location of the Elf header (it is never in memory at location 0 is the
// virtual memory space of the program, is is usually offset by 40 bytes).
// TODO: Rewrite this to use in-memory, finding u=out the Elf header location using auxv?
//       If that would work, we could make this logic allocation-free.
fn doGetLibCLibrary(self_exe_path: []const u8) !LibCLibrary {
    const self_exe_file = std.fs.openFileAbsolute(self_exe_path, .{ .mode = .read_only }) catch |err| {
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
    try self_exe_file.seekTo(dynamic_symbols_table_offset);
    const dynamic_symbol_count = dynamic_symbols_table_size / @sizeOf(std.elf.Elf64_Dyn);
    const dynamic_symbols = try std.heap.page_allocator.alloc(std.elf.Elf64_Dyn, dynamic_symbol_count);
    defer std.heap.page_allocator.free(dynamic_symbols);
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
                    const lib_name_own = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}", .{lib_name}) catch |err| {
                        print.printError("Failed to allocate memory for libc name: {}", .{err});
                        return error.CannotAllocateMemory;
                    };
                    return LibCLibrary{ .flavor = types.LibCFlavor.MUSL, .name = lib_name_own };
                }

                if (std.mem.indexOf(u8, lib_name, "libc.so.6")) |_| {
                    const lib_name_own = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}", .{lib_name}) catch |err| {
                        print.printError("Failed to allocate memory for libc name: {}", .{err});
                        return error.CannotAllocateMemory;
                    };
                    return LibCLibrary{ .flavor = types.LibCFlavor.GNU, .name = lib_name_own };
                }
            }
        }
    }

    return UnknownLibC;
}

test "doGetLibCLibrary: should return libc flavor unknown when file does not exist" {
    const lib_c = try doGetLibCLibrary("/does/not/exist");
    try testing.expectEqual(lib_c.flavor, .UNKNOWN);
}

test "doGetLibCLibrary: should return libc flavor unknown when file is not an ELF binary" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/not-an-elf-binary" });
    defer allocator.free(absolute_path_to_binary);
    const lib_c = try doGetLibCLibrary(absolute_path_to_binary);
    try testing.expectEqual(lib_c.flavor, .UNKNOWN);
}

test "doGetLibCLibrary: should identify musl libc flavor (arm64)" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/dotnet-app-arm64-musl" });
    defer allocator.free(absolute_path_to_binary);
    const lib_c = try doGetLibCLibrary(absolute_path_to_binary);
    try testing.expectEqual(lib_c.flavor, .MUSL);
}

test "doGetLibCLibrary: should identify musl libc flavor (x86_64)" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/dotnet-app-x86_64-musl" });
    defer allocator.free(absolute_path_to_binary);
    const lib_c = try doGetLibCLibrary(absolute_path_to_binary);
    try testing.expectEqual(lib_c.flavor, .MUSL);
}

test "doGetLibCLibrary: should identify glibc libc flavor (arm64)" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/dotnet-app-arm64-glibc" });
    defer allocator.free(absolute_path_to_binary);
    const lib_c = try doGetLibCLibrary(absolute_path_to_binary);
    try testing.expectEqual(lib_c.flavor, .GNU);
}

test "doGetLibCLibrary: should identify glibc libc flavor (x86_64)" {
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_binary = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/dotnet-app-x86_64-glibc" });
    defer allocator.free(absolute_path_to_binary);
    const lib_c = try doGetLibCLibrary(absolute_path_to_binary);
    try testing.expectEqual(lib_c.flavor, .GNU);
}
