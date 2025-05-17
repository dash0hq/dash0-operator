// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

export const init_array: [1]*const fn () callconv(.C) void linksection(".init_array") = .{&init};

export var my_environ: [*c]const [*c]const u8 = @as([3][*c]const u8, .{ "VAR1=VALUE1", "VAR2=VALUE2", null })[0..].ptr;

fn init() callconv(.C) void {
    std.debug.print("root.zig#initEnviron START\n", .{});
    _init() catch @panic("_init failed");
}

fn _init() !void {
    std.debug.print("root.zig#_init my_environ address (before change): {any}\n", .{my_environ});

    // Note: Adding a final null value is crucial, otherwise C will iterate past the end of the list. The actual length
    // of the list that Zig knows about is lost in transfer, C will only react to null terminators to keep individual
    // strings apart and to the final double null terminator to know when the list ends.
    my_environ = @as([3][*c]const u8, .{ "VAR3=VALUE3", "VAR4=VALUE4", null })[0..].ptr;
    std.debug.print("root.zig#_init my_environ address (after change):: {any}\n", .{my_environ});
}

export fn change_values() callconv(.C) void {
    std.debug.print("root.zig#change_values\n", .{});

    std.debug.print("root.zig#change_values my_environ address (before change): {any}\n", .{my_environ});
    my_environ = @as([4][*c]const u8, .{ "VAR5=VALUE5", "VAR6=VALUE6", "VAR7=VALUE7", null })[0..].ptr;
    std.debug.print("root.zig#change_values my_environ address (after change): {any}\n", .{my_environ});
}
