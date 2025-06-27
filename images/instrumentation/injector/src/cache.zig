// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const types = @import("types.zig");

/// A type for a single cached modified environment variable.
pub const CachedEnvVarValue = struct {
    done: bool,
    value: ?types.NullTerminatedString,
};

/// A type for a collection of modified environment variables for .NET instrumentation.
pub const CachedDotnetEnvVarUpdates = struct {
    done: bool,
    values: ?types.DotnetEnvVarUpdates,
};

/// A type for a global cache of already-calculated values for modified environment variables.
pub const InjectorCache = struct {
    java_tool_options: CachedEnvVarValue,
    node_options: CachedEnvVarValue,
    otel_resource_attributes: CachedEnvVarValue,
    experimental_dotnet_injection_enabled: ?bool,
    dotnet_env_var_updates: CachedDotnetEnvVarUpdates,
    libc_flavor: ?types.LibCFlavor,
};

/// Global pointers to already-calculated values to avoid multiple allocations on repeated lookups.
pub var injector_cache: InjectorCache = emptyInjectorCache();

/// Reset an empty modification cache with all fields set to null and done set to false.
pub fn emptyInjectorCache() InjectorCache {
    return InjectorCache{
        .java_tool_options = CachedEnvVarValue{ .value = null, .done = false },
        .node_options = CachedEnvVarValue{ .value = null, .done = false },
        .otel_resource_attributes = CachedEnvVarValue{ .value = null, .done = false },
        .experimental_dotnet_injection_enabled = null,
        .dotnet_env_var_updates = CachedDotnetEnvVarUpdates{ .values = null, .done = false },
        .libc_flavor = null,
    };
}
