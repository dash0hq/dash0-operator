// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const types = @import("types.zig");

/// Get the value of an environment variable from the provided env_vars list, which is a list of null-terminated
/// strings. Returns an the value of the environment variable as an optional, and the index of the environment variable;
/// the index is only valid if the environment variable was found (i.e. the optional is not null).
pub fn getEnvVar(env_vars: [](types.NullTerminatedString), name: []const u8) ?types.EnvVarValueAndIndex {
	for (env_vars, 0..) |env_var, idx| {
		const env_var_slice: []const u8 = std.mem.span(env_var);
		if (std.mem.indexOf(u8, env_var_slice, "=")) |equals_char_idx| {
			if (std.mem.eql(u8, name, env_var[0..equals_char_idx])) {
				if (std.mem.len(env_var) == equals_char_idx + 1) {
					return null;
				}
				return types.EnvVarValueAndIndex{ .value = env_var[equals_char_idx + 1 ..], .index = idx };
			}
		}
	}

	return null;
}