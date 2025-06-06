// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const dash0_injector_debug_env_var_name = "DASH0_INJECTOR_DEBUG";
const log_prefix = "[Dash0 injector] ";

// TODO currently hard coded to true, needs to be fixed with reading the DASH0_INJECTOR_DEBUG variable from the original
// environment read from /proc/self/environ.
var is_debug = true;

/// Initializes the is_debug flag based on the environment variable DASH0_INJECTOR_DEBUG.
pub fn initDebugFlag() void {
    // if (std.posix.getenv(dash0_injector_debug_env_var_name)) |is_debug_raw| {
    //     is_debug = std.ascii.eqlIgnoreCase("true", is_debug_raw);
    // }
}

pub fn isDebug() bool {
    return is_debug;
}

pub fn printDebug(comptime fmt: []const u8, args: anytype) void {
    if (is_debug) {
        std.debug.print(log_prefix ++ fmt ++ "\n", args);
    }
}

pub fn printError(comptime fmt: []const u8, args: anytype) void {
    std.debug.print(log_prefix ++ fmt ++ "\n", args);
}

pub fn printMessage(comptime fmt: []const u8, args: anytype) void {
    std.debug.print(log_prefix ++ fmt ++ "\n", args);
}
