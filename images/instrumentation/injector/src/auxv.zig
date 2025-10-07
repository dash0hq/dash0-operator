const std = @import("std");

const print = @import("print.zig");

// Need to implement and export this symbol to prevent an unwanted dependency on the `getauxval` symbol from libc, which
// will not be fulfilled when linking to a process that does not include libc itself. It is safe to export this, since
// we do not export any _global_ symbols, only local symbols, and in particular, getauxval is only exported locally.
// Executables requiring getauxval will bind to libc's getauxval, not the symbol exported here.
pub export fn getauxval(auxv_type: u32) callconv(.C) usize {
    var auxv_file = std.fs.openFileAbsolute("/proc/self/auxv", .{}) catch |err| {
        print.printMessage("Failed to open /proc/self/auxv: {}", .{err});
        return 0;
    };
    defer auxv_file.close();

    var auxv_reader = auxv_file.reader();

    while (true) {
        const auxv_symbol = auxv_reader.readStruct(std.elf.Elf64_auxv_t) catch |err| {
            if (err == error.EndOfStream) {
                break;
            }

            print.printMessage("Failed to read from /proc/self/auxv: {}", .{err});
            return 0;
        };

        if (auxv_symbol.a_type == auxv_type) {
            return auxv_symbol.a_un.a_val;
        } else if (auxv_symbol.a_type == std.elf.AT_NULL) {
            break;
        }
    }

    return 0;
}
