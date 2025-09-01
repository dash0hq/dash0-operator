const std = @import("std");

const print = @import("print.zig");

// Need to implement and export this to prevent an unwanted
// dependency on the `getauxval` symbol from LibC, which will
// not be fulfilled when linking to a process that does not
// include LibC itself.
pub export fn getauxval(auxv_type: u32) callconv(.C) usize {
    var auxv_file = std.fs.openFileAbsolute("/proc/self/auxv", .{}) catch |err| {
        print.printError("Failed to open /proc/self/auxv: {}", .{err});
        return 0;
    };
    defer auxv_file.close();

    var auxv_reader = auxv_file.reader();

    while (true) {
        const auxv_symbol = auxv_reader.readStruct(std.elf.Elf64_auxv_t) catch |err| {
            if (err == error.EndOfStream) {
                break;
            }

            print.printError("Failed to read from /proc/self/auxv: {}", .{err});
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
