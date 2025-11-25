// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const dash0_injector_debug_env_var_name = "DASH0_INJECTOR_DEBUG";
const dash0_injector_log_level_env_var_name = "DASH0_INJECTOR_LOG_LEVEL";
const log_prefix = "[Dash0 injector] ";

const LogLevel = enum(u8) {
    Debug = 0,
    Info = 1,
    Warn = 2,
    Error = 3,
    None = 4,
};
var log_level: LogLevel = .Error;

/// Initializes the log levelbased on the environment variables DASH0_INJECTOR_LOG_LEVEL or DASH0_INJECTOR_DEBUG.
pub fn initLogLevel() void {
    if (std.posix.getenv(dash0_injector_debug_env_var_name)) |is_debug_raw| {
        if (std.ascii.eqlIgnoreCase("true", is_debug_raw)) {
            log_level = .Debug;
            // DASH0_INJECTOR_DEBUG=true overrides DASH0_INJECTOR_LOG_LEVEL
            return;
        }
    }
    if (std.posix.getenv(dash0_injector_log_level_env_var_name)) |log_level_raw| {
        if (std.ascii.eqlIgnoreCase("debug", log_level_raw)) {
            log_level = .Debug;
        } else if (std.ascii.eqlIgnoreCase("info", log_level_raw)) {
            log_level = .Info;
        } else if (std.ascii.eqlIgnoreCase("warn", log_level_raw)) {
            log_level = .Warn;
        } else if (std.ascii.eqlIgnoreCase("error", log_level_raw)) {
            log_level = .Error;
        } else if (std.ascii.eqlIgnoreCase("none", log_level_raw)) {
            log_level = .None;
        }
    }
}

pub fn resetLogLevel() void {
    log_level = .Error;
}

pub fn getLogLevel() LogLevel {
    return log_level;
}

pub fn printDebug(comptime fmt: []const u8, args: anytype) void {
    if (log_level == .Debug) {
        _printMessage(fmt, args);
    }
}

pub fn printInfo(comptime fmt: []const u8, args: anytype) void {
    if (@intFromEnum(log_level) <= @intFromEnum(LogLevel.Info)) {
        _printMessage(fmt, args);
    }
}

pub fn printWarn(comptime fmt: []const u8, args: anytype) void {
    if (@intFromEnum(log_level) <= @intFromEnum(LogLevel.Warn)) {
        _printMessage(fmt, args);
    }
}

pub fn printError(comptime fmt: []const u8, args: anytype) void {
    if (@intFromEnum(log_level) <= @intFromEnum(LogLevel.Error)) {
        _printMessage(fmt, args);
    }
}

fn _printMessage(comptime fmt: []const u8, args: anytype) void {
    std.debug.print(log_prefix ++ fmt ++ "\n", args);
}
