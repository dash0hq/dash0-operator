// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

export const init_array: [1]*const fn () callconv(.C) void linksection(".init_array") = .{&init};

var __environ: [*c]const [*c]const c_char = undefined;

comptime {
    @export(&__environ, .{ .name = "__environ", .linkage = .strong });
    @export(&__environ, .{ .name = "_environ", .linkage = .strong });
    @export(&__environ, .{ .name = "environ", .linkage = .strong });
}

fn init() callconv(.C) void {
    std.debug.print("root.zig#initEnviron START\n", .{});
    _init() catch @panic("_init failed");
}

fn _init() !void {
    std.debug.print("root.zig#_init __environ address (before change): {any}\n", .{__environ});

    // Note: Adding a final null value is crucial, otherwise C will iterate past the end of the list. The actual length
    // of the list that Zig knows about is lost in transfer, C will only react to null terminators to keep individual
    // strings apart and to the final double null terminator to know when the list ends.
    var env_vars: std.ArrayList([]c_char) = std.ArrayList([]c_char).init(std.heap.page_allocator);
    const env_var: [*:0]c_char = try std.fmt.allocPrintZ(std.heap.page_allocator, "VAR4=VALUE4", .{});
    try env_vars.append(env_var[0..]);
    const env_var_slices = try env_vars.toOwnedSliceSentinel("\x00");
    const env_var_len = env_var_slices.len;
    var env_slice = try std.heap.page_allocator.alloc([*c]const c_char, env_var_len + 1);
    for (0..env_var_len) |i| {
        env_slice[i] = env_var_slices[i];
    }

    // const envVar2: [*c]const c_char = "VAR4=VALUE4";
    // __environ = @as([2][*c]const c_char, .{  envVar[0..], null })[0..].ptr;
    __environ = @ptrCast(env_slice);

    std.debug.print("root.zig#_init __environ address (after change):: {any}\n", .{__environ});
}

export fn change_values() callconv(.C) void {
    std.debug.print("root.zig#change_values\n", .{});

    std.debug.print("root.zig#change_values __environ address (before change): {any}\n", .{__environ});
    __environ = @as([4][*c]const c_char, .{ "VAR5=VALUE5", "VAR6=VALUE6", "VAR7=VALUE7", null })[0..].ptr;
    std.debug.print("root.zig#change_values __environ address (after change): {any}\n", .{__environ});
}
