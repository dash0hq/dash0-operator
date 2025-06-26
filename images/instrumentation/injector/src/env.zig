// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const test_util = @import("test_util.zig");
const types = @import("types.zig");

const testing = std.testing;
const expectWithMessage = test_util.expectWithMessage;

/// Get the value of an environment variable from the provided env_vars list, which is a list of null-terminated
/// strings following the pattern VARIABLE_NAME=value. Returns an the value of the environment variable, and the index
/// where it was found in the provided slice. Returns null if the environment variable is not found. Note that the
/// returned value might be the empty string (if the environment variable was set like this "VARIABLE_NAME=").
pub fn getEnvVar(env_vars: [](types.NullTerminatedString), name: []const u8) ?types.EnvVarValueAndIndex {
    for (env_vars, 0..) |env_var, idx| {
        const env_var_slice: []const u8 = std.mem.span(env_var);
        // split at the = character
        if (std.mem.indexOf(u8, env_var_slice, "=")) |equals_char_idx| {
            if (std.mem.eql(u8, name, env_var[0..equals_char_idx])) {
                // This is the environment variable we are looking for.
                // Note: We deliberately do not check whether the enviroment variable is the empty string here, that is
                // client code has to be able to deal with the case that EnvVarValueAndIndex.value is the empty
                // null-terminated string.
                return types.EnvVarValueAndIndex{ .value = env_var[equals_char_idx + 1 ..], .index = idx };
            }
        }
    }
    return null;
}

test "getEnvVar: empty list" {
    const env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(env_vars);
    if (getEnvVar(env_vars, "ENV_VAR")) |_| {
        return error.TestUnexpectedResult;
    }
}

test "getEnvVar: env var not found" {
    const env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 5);
    defer std.heap.page_allocator.free(env_vars);
    env_vars[0] = "ENV_VAR_1=value1";
    env_vars[1] = "ENV_VAR_2=value2";
    env_vars[2] = "ENV_VAR_3=value3";
    env_vars[3] = "ENV_VAR_4=value4";
    env_vars[4] = "ENV_VAR_5=value5";
    try expectWithMessage(getEnvVar(env_vars, "ENV_VAR_42") == null, "etEnvVar(env_vars, \"ENV_VAR_42\") == null");
}

test "getEnvVar: env var found at the beginning" {
    const env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 5);
    defer std.heap.page_allocator.free(env_vars);
    env_vars[0] = "ENV_VAR_1=value1";
    env_vars[1] = "ENV_VAR_2=value2";
    env_vars[2] = "ENV_VAR_3=value3";
    env_vars[3] = "ENV_VAR_4=value4";
    env_vars[4] = "ENV_VAR_5=value5";
    const value_and_index = getEnvVar(env_vars, "ENV_VAR_1").?;
    try testing.expectEqualStrings("value1", std.mem.span(value_and_index.value));
    try testing.expectEqual(0, value_and_index.index);
}

test "getEnvVar: env var found in the middle" {
    const env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 5);
    defer std.heap.page_allocator.free(env_vars);
    env_vars[0] = "ENV_VAR_1=value1";
    env_vars[1] = "ENV_VAR_2=value2";
    env_vars[2] = "ENV_VAR_3=value3";
    env_vars[3] = "ENV_VAR_4=value4";
    env_vars[4] = "ENV_VAR_5=value5";
    const value_and_index = getEnvVar(env_vars, "ENV_VAR_3").?;
    try testing.expectEqualStrings("value3", std.mem.span(value_and_index.value));
    try testing.expectEqual(2, value_and_index.index);
}

test "getEnvVar: env var found at the end" {
    const env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 5);
    defer std.heap.page_allocator.free(env_vars);
    env_vars[0] = "ENV_VAR_1=value1";
    env_vars[1] = "ENV_VAR_2=value2";
    env_vars[2] = "ENV_VAR_3=value3";
    env_vars[3] = "ENV_VAR_4=value4";
    env_vars[4] = "ENV_VAR_5=value5";
    const value_and_index = getEnvVar(env_vars, "ENV_VAR_5").?;
    try testing.expectEqualStrings("value5", std.mem.span(value_and_index.value));
    try testing.expectEqual(4, value_and_index.index);
}

test "getEnvVar: env var value contains additional = characters" {
    const env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 5);
    defer std.heap.page_allocator.free(env_vars);
    env_vars[0] = "ENV_VAR_1=value1";
    env_vars[1] = "ENV_VAR_2=value2";
    env_vars[2] = "ENV_VAR_3=aa=bb,cc=dd,ee=ff";
    env_vars[3] = "ENV_VAR_4=value4";
    env_vars[4] = "ENV_VAR_5=value5";
    const value_and_index = getEnvVar(env_vars, "ENV_VAR_3").?;
    try testing.expectEqualStrings("aa=bb,cc=dd,ee=ff", std.mem.span(value_and_index.value));
    try testing.expectEqual(2, value_and_index.index);
}

pub fn isTrue(env_vars: [](types.NullTerminatedString), name: []const u8) bool {
    if (getEnvVar(env_vars, name)) |value_and_index| {
        return std.ascii.eqlIgnoreCase(std.mem.span(value_and_index.value), "true");
    }
    return false;
}
