// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const environ_api = @import("environ_api.zig");
const environ_init = @import("environ_init.zig");
const print = @import("print.zig");
const types = @import("types.zig");

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

var __environ_internal: [*c]const [*c]const u8 = undefined;

comptime {
    @export(&__environ_internal, .{ .name = "__environ", .linkage = .strong });
    @export(&__environ_internal, .{ .name = "_environ", .linkage = .strong });
    @export(&__environ_internal, .{ .name = "environ", .linkage = .strong });
    @export(&getenv, .{ .name = "getenv", .linkage = .strong });
}

fn initEnviron() callconv(.C) void {
    __environ_internal = environ_init._initEnviron("/proc/self/environ") catch @panic("[Dash0 injector] initEnviron failed");
    // For very verbose debugging, uncomment the following lines to print all exported environment variables:
    // const num_env_vars = std.mem.len(__environ_internal);
    // for (__environ_internal, 0..num_env_vars) |env_var, i| {
    //     std.debug.print("- initEnviron: {d} -> {s}\n", .{i, env_var});
    // }
    if (print.isDebug()) {
        // Note: print.isDebug is only initalized after injector._initEnviron, since it requires access to environment
        // variables, thus the startup message is not printed here but in injector._initEnviron after reading the
        // environment.
        const pid = std.os.linux.getpid();
        const exe = std.fs.selfExePathAlloc(std.heap.page_allocator) catch "?";
        print.printDebug("successfully initalized __environ, _environ and environ (pid: {d}, executable: {s})\n", .{ pid, exe });
    }
}

fn getenv(name: types.NullTerminatedString) callconv(.C) ?types.NullTerminatedString {
    return environ_api._getenv(environ_init.getCachedOriginalEnvVars(), name);
}
