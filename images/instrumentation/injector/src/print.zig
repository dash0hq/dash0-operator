// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const env = @import("env.zig");
const types = @import("types.zig");

const testing = std.testing;

const dash0_injector_debug_env_var_name = "DASH0_INJECTOR_DEBUG";
const log_prefix = "[Dash0 injector] ";

var is_debug = false;

/// Initializes the is_debug flag based on the environment variable DASH0_INJECTOR_DEBUG.
pub fn initDebugFlag(env_vars: [](types.NullTerminatedString)) void {
    is_debug = env.isTrue(env_vars, dash0_injector_debug_env_var_name);
}

test "initDebugFlag: not set, empty environmnet" {
    const original_value = is_debug;
    defer is_debug = original_value;

    const env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(env_vars);

    initDebugFlag(env_vars);
    try testing.expect(!isDebug());
}

test "initDebugFlag: DASH0_INJECTOR_DEBUG not set" {
    const original_value = is_debug;
    defer is_debug = original_value;

    const env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 3);
    defer std.heap.page_allocator.free(env_vars);
    env_vars[0] = "AAA=bbb";
    env_vars[1] = "CCC=ddd";
    env_vars[2] = "EEE=fff";

    initDebugFlag(env_vars);
    try testing.expect(!isDebug());
}

test "initDebugFlag: false" {
    const original_value = is_debug;
    defer is_debug = original_value;

    const env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 4);
    defer std.heap.page_allocator.free(env_vars);
    env_vars[0] = "AAA=bbb";
    env_vars[1] = "DASH0_INJECTOR_DEBUG=false";
    env_vars[2] = "CCC=ddd";
    env_vars[3] = "EEE=fff";

    initDebugFlag(env_vars);
    try testing.expect(!isDebug());
}

test "initDebugFlag: arbitrary string" {
    const original_value = is_debug;
    defer is_debug = original_value;

    const env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 4);
    defer std.heap.page_allocator.free(env_vars);
    env_vars[0] = "AAA=bbb";
    env_vars[1] = "DASH0_INJECTOR_DEBUG=whatever";
    env_vars[2] = "CCC=ddd";
    env_vars[3] = "EEE=fff";

    initDebugFlag(env_vars);
    try testing.expect(!isDebug());
}

test "initDebugFlag: empty string" {
    const original_value = is_debug;
    defer is_debug = original_value;

    const env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 4);
    defer std.heap.page_allocator.free(env_vars);
    env_vars[0] = "AAA=bbb";
    env_vars[1] = "DASH0_INJECTOR_DEBUG=";
    env_vars[2] = "CCC=ddd";
    env_vars[3] = "EEE=fff";

    initDebugFlag(env_vars);
    try testing.expect(!isDebug());
}

test "initDebugFlag: true, only env var" {
    const original_value = is_debug;
    defer is_debug = original_value;

    const env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(env_vars);
    env_vars[0] = "DASH0_INJECTOR_DEBUG=true";

    initDebugFlag(env_vars);
    try testing.expect(isDebug());
}

test "initDebugFlag: true, other env vars present" {
    const original_value = is_debug;
    defer is_debug = original_value;

    const env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 4);
    defer std.heap.page_allocator.free(env_vars);
    env_vars[0] = "AAA=bbb";
    env_vars[1] = "DASH0_INJECTOR_DEBUG=true";
    env_vars[2] = "CCC=ddd";
    env_vars[3] = "EEE=fff";

    initDebugFlag(env_vars);
    try testing.expect(isDebug());
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
