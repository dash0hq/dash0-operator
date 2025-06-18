// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const types = @import("types.zig");

/// A type for a single cached modified environment variable.
pub const CachedModification = struct {
	done: bool,
	value: ?types.NullTerminatedString,
};

/// A type for a global cache of already-calculated values for modified environment variables.
pub const ModificationCache = struct {
	java_tool_options: CachedModification,
	node_options: CachedModification,
	otel_resource_attributes: CachedModification,
};

/// Global pointers to already-calculated values to avoid multiple allocations on repeated lookups.
pub var modification_cache: ModificationCache = emptyModificationCache();

/// Reset an empty modification cache with all fields set to null and done set to false.
pub fn emptyModificationCache() ModificationCache {
	return ModificationCache{
		.java_tool_options = CachedModification{ .value = null, .done = false },
		.node_options = CachedModification{ .value = null, .done = false },
		.otel_resource_attributes = CachedModification{ .value = null, .done = false },
	};
}
