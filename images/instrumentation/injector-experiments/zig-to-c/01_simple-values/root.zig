// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

export const init_array: [1]*const fn () callconv(.C) void linksection(".init_array") = .{&init};

export var my_int: c_int = 0;
export var my_char: u8 = 'a';
export var my_string: [*:0]const u8 = "aaa";
export var my_nt_kv_str: [*:0]const u8 = "aaa=111\x00bbb=222\x00";

fn init() callconv(.C) void {
    std.debug.print("root.zig#initEnviron START\n", .{});
    _init() catch @panic("_init failed");
}

fn _init() !void {
    my_int = 1;
    my_char = 'b';
    my_string = "bbb";
    my_nt_kv_str = "ccc=333\x00ddd=444\x00";
}

export fn change_values() callconv(.C) void {
    std.debug.print("root.zig#change_values\n", .{});
    my_int = 2;
    my_char = 'c';
    my_string = "ccc";
    my_nt_kv_str = "eee=555\x00fff=666\x00";
}
