// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const types = @import("types.zig");

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
