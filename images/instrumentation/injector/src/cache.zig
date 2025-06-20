// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const types = @import("types.zig");

/// A type for a single cached modified environment variable.
pub const CachedEnvVarModification = struct {
    done: bool,
    value: ?types.NullTerminatedString,
};

/// A type for a collection of modified environment variables for .NET instrumentation.
pub const CachedDotnetValues = struct {
    done: bool,
    values: ?types.DotnetValues,
};

/// A type for a global cache of already-calculated values for modified environment variables.
pub const InjectorCache = struct {
    java_tool_options: CachedEnvVarModification,
    node_options: CachedEnvVarModification,
    otel_resource_attributes: CachedEnvVarModification,
    experimental_dotnet_injection_enabled: ?bool,
    dotnet_values: CachedDotnetValues,
    libc_flavor: ?types.LibCFlavor,
};

/// Global pointers to already-calculated values to avoid multiple allocations on repeated lookups.
pub var injector_cache: InjectorCache = emptyInjectorCache();

/// Reset an empty modification cache with all fields set to null and done set to false.
pub fn emptyInjectorCache() InjectorCache {
    return InjectorCache{
        .java_tool_options = CachedEnvVarModification{ .value = null, .done = false },
        .node_options = CachedEnvVarModification{ .value = null, .done = false },
        .otel_resource_attributes = CachedEnvVarModification{ .value = null, .done = false },
        .experimental_dotnet_injection_enabled = null,
        .dotnet_values = CachedDotnetValues{ .values = null, .done = false },
        .libc_flavor = null,
    };
}
