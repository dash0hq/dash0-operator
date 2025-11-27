// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const testing = std.testing;

const dash0_injector_log_level_env_var_name = "DASH0_INJECTOR_LOG_LEVEL";
const dash0_injector_log_level_environ_prefix = "DASH0_INJECTOR_LOG_LEVEL=";
const log_prefix = "[Dash0 injector] ";

const LogLevel = enum(u8) {
    Debug = 0,
    Info = 1,
    Warn = 2,
    Error = 3,
    None = 4,
};

var log_level: LogLevel = .Error;

const proc_self_environ_path = "/proc/self/environ";

/// Initializes the log level based on the environment variable DASH0_INJECTOR_LOG_LEVEL, by reading /proc/self/environ
/// line by line. When reading environment variables later in the injector's life cycle, we will use the pointer to the
/// __environ array after looking it up via libc.getLibCInfo(), but of course this pointer is not available during the
/// injector's initialization phase yet, and we need to know the log level _before_ running libc.getLibCInfo() (or any
/// other code that might want to print log messages).
pub fn initLogLevelFromProcSelfEnviron() !void {
    try initLogLevelFromEnvironFile(proc_self_environ_path);
}

// Note: initLogLevelFromEnvironFile is exposed as pub for testing purposes only.
pub fn initLogLevelFromEnvironFile(self_environ_path: []const u8) !void {
    var log_level_env_var_value: ?[]const u8 = null;

    var environ_file = try std.fs.openFileAbsolute(self_environ_path, .{});
    defer environ_file.close();
    var buf_reader = std.io.bufferedReader(environ_file.reader());
    var in_stream = buf_reader.reader();
    var buf: [256]u8 = undefined;
    while (true) {
        const l = in_stream.readUntilDelimiterOrEof(&buf, 0) catch {
            // Ignore lines that are too long for the buffer; advance the the read positon to the next delimiter to
            // avoid stream corruption.
            in_stream.skipUntilDelimiterOrEof(0) catch {};
            continue;
        };
        if (l) |line| {
            if (std.mem.startsWith(u8, line, dash0_injector_log_level_environ_prefix)) {
                log_level_env_var_value = line[dash0_injector_log_level_environ_prefix.len..line.len];
                break;
            }
        } else {
            // line is null, end of file has been reached
            break;
        }
    }

    if (log_level_env_var_value) |log_level_value| {
        if (std.ascii.eqlIgnoreCase("debug", log_level_value)) {
            log_level = .Debug;
        } else if (std.ascii.eqlIgnoreCase("info", log_level_value)) {
            log_level = .Info;
        } else if (std.ascii.eqlIgnoreCase("warn", log_level_value)) {
            log_level = .Warn;
        } else if (std.ascii.eqlIgnoreCase("error", log_level_value)) {
            log_level = .Error;
        } else if (std.ascii.eqlIgnoreCase("none", log_level_value)) {
            log_level = .None;
        } else {
            printError("unknown value for DASH0_INJECTOR_LOG_LEVEL: \"{s}\" -- valid log levels are \"debug\", \"info\", \"warn\", \"error\", \"none\".", .{log_level_value});
        }
    }
    printDebug("log level: {}", .{getLogLevel()});
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL is not set" {
    defer resetLogLevel();
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_environ_file = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/proc-self-environ-no-log-level" });
    defer allocator.free(absolute_path_to_environ_file);
    try initLogLevelFromEnvironFile(absolute_path_to_environ_file);
    // verify that the default log level is set
    try testing.expectEqual(.Error, getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=debug" {
    defer resetLogLevel();
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_environ_file = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/proc-self-environ-log-level-debug" });
    defer allocator.free(absolute_path_to_environ_file);
    try initLogLevelFromEnvironFile(absolute_path_to_environ_file);
    try testing.expectEqual(.Debug, getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=info" {
    defer resetLogLevel();
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_environ_file = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/proc-self-environ-log-level-info" });
    defer allocator.free(absolute_path_to_environ_file);
    try initLogLevelFromEnvironFile(absolute_path_to_environ_file);
    try testing.expectEqual(.Info, getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=warn" {
    defer resetLogLevel();
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_environ_file = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/proc-self-environ-log-level-warn" });
    defer allocator.free(absolute_path_to_environ_file);
    try initLogLevelFromEnvironFile(absolute_path_to_environ_file);
    try testing.expectEqual(.Warn, getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=error" {
    defer resetLogLevel();
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_environ_file = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/proc-self-environ-log-level-error" });
    defer allocator.free(absolute_path_to_environ_file);
    try initLogLevelFromEnvironFile(absolute_path_to_environ_file);
    try testing.expectEqual(.Error, getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=none" {
    defer resetLogLevel();
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_environ_file = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/proc-self-environ-log-level-none" });
    defer allocator.free(absolute_path_to_environ_file);
    try initLogLevelFromEnvironFile(absolute_path_to_environ_file);
    try testing.expectEqual(.None, getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=none with overly long environment variable" {
    defer resetLogLevel();
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_environ_file = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/proc-self-environ-log-level-none-overly-long-env-var" });
    defer allocator.free(absolute_path_to_environ_file);
    try initLogLevelFromEnvironFile(absolute_path_to_environ_file);
    try testing.expectEqual(.None, getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL is an arbitrary string" {
    defer resetLogLevel();
    const allocator = std.heap.page_allocator;
    const cwd_path = try std.fs.cwd().realpathAlloc(allocator, ".");
    defer allocator.free(cwd_path);
    const absolute_path_to_environ_file = try std.fs.path.resolve(allocator, &.{ cwd_path, "unit-test-assets/proc-self-environ-log-level-arbitrary-string" });
    defer allocator.free(absolute_path_to_environ_file);
    try initLogLevelFromEnvironFile(absolute_path_to_environ_file);
    try testing.expectEqual(.Error, getLogLevel());
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
