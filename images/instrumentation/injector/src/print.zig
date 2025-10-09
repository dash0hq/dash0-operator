// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const dash0_injector_debug_env_var_name = "DASH0_INJECTOR_DEBUG";
const dash0_injector_quiet_env_var_name = "DASH0_INJECTOR_QUIET";
const log_prefix = "[Dash0 injector] ";

var is_debug = false;
var is_quiet = false;

/// Initializes the is_debug and the is_quiet flag based on the environment variables DASH0_INJECTOR_DEBUG and
/// DASH0_INJECTOR_QUIET.
pub fn initFlags() void {
    if (std.posix.getenv(dash0_injector_debug_env_var_name)) |is_debug_raw| {
        is_debug = std.ascii.eqlIgnoreCase("true", is_debug_raw);
    }
    if (std.posix.getenv(dash0_injector_quiet_env_var_name)) |is_quiet_raw| {
        is_quiet = std.ascii.eqlIgnoreCase("true", is_quiet_raw);
    }
}

pub fn isDebug() bool {
    return is_debug;
}

pub fn isQuiet() bool {
    return is_quiet;
}

pub fn printDebug(comptime fmt: []const u8, args: anytype) void {
    if (is_debug) {
        _printMessage(fmt, args);
    }
}

pub fn printMessage(comptime fmt: []const u8, args: anytype) void {
    if (!is_quiet or is_debug) { // DASH0_INJECTOR_DEBUG overrides DASH0_INJECTOR_QUIET
        _printMessage(fmt, args);
    }
}

fn _printMessage(comptime fmt: []const u8, args: anytype) void {
    std.debug.print(log_prefix ++ fmt ++ "\n", args);
}

pub fn resetFlags() void {
    is_debug = false;
    is_quiet = false;
}
