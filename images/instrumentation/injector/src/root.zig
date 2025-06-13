// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const injector = @import("injector.zig");

// This is how you can currently do something like `__attribute__((constructor))` in Zig, that is, register a function
// that runs before main(): Export a const named init_array which lists the functions you want to run. This will then
// be added to the .init_array ELF section of the binary. (See SHT_INIT_ARRAY in
// https://refspecs.linuxfoundation.org/LSB_5.0.0/LSB-Core-generic/LSB-Core-generic/sections.html.)
//
// Note: Destructor, i.e. things that run after main() are registered similarly via `fini_array`, but they do not run
// on panic or unhandled signals (so not chance for getting a cheap abnormal process detection mechanism here).
//
// There is discussion to provide a more explicit mechanism for init functions in the future, but not much traction so
// far. See https://github.com/ziglang/zig/issues/23574 and https://github.com/ziglang/zig/issues/20382.
export const init_array: [1]*const fn () callconv(.C) void linksection(".init_array") = .{&initEnviron};

// TODO c_char might be better than u8 here?
var __environ_internal: [*c]const [*c]const u8 = undefined;

comptime {
    @export(&__environ_internal, .{ .name = "__environ", .linkage = .strong });
    @export(&__environ_internal, .{ .name = "_environ", .linkage = .strong });
    @export(&__environ_internal, .{ .name = "environ", .linkage = .strong });
}

fn initEnviron() callconv(.C) void {
    const pid = std.os.linux.getpid();
    std.debug.print("[Dash0 injector] {d} initEnviron() start\n", .{pid});
    __environ_internal, _ = injector._initEnviron("/proc/self/environ") catch @panic("[Dash0 injector] initEnviron failed");
    std.debug.print("[Dash0 injector] {d} initEnviron() done\n", .{pid});
}
