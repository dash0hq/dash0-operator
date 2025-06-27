// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

pub const NullTerminatedString = [*:0]const u8;

pub const EnvVarValueAndIndex = struct {
    /// The value of the environment variable. If the environment variable is not found, no EnvVarValueAndIndex should
    /// be returned at all, that is, functions that return EnvVarValueAndIndex always return an optional
    /// ?EnvVarValueAndIndex.
    value: NullTerminatedString,
    /// The index of the environment variable in the list of environment variables list.
    index: usize,
};

pub const EnvVarUpdate = struct {
    /// The value of the environment variable to set. Not that this is not the full environment variable key-value pair,
    /// that is, to put this into __environ, the name of the environment variable and the = character must still be
    /// prepended.
    /// If no update (no replace, no append) for an environment variable can be created, no EnvVarUpdate should be
    /// returned at all, that is, functions that return EnvVarValueAndIndex always return an optional
    /// ?EnvVarValueAndIndex.
    value: NullTerminatedString,
    /// If this is true, the required action is to _replace_ the environment variable at the index specified via the
    /// index property with the new value. If this is false, the required action is to append the new value to the end
    /// of the environment variables list, the index property must be ignored in that case.
    replace: bool,
    /// The index of the environment variable in the original environment variables list. This value is only valid if
    /// replace is true, otherwise it must be ignored.
    index: usize,
};

pub const LibCFlavor = enum { UNKNOWN, GNU_LIBC, MUSL };

pub const DotnetEnvVarUpdates = struct {
    coreclr_enable_profiling: EnvVarUpdate,
    coreclr_profiler: EnvVarUpdate,
    coreclr_profiler_path: EnvVarUpdate,
    dotnet_additional_deps: EnvVarUpdate,
    dotnet_shared_store: EnvVarUpdate,
    dotnet_startup_hooks: EnvVarUpdate,
    otel_auto_home: EnvVarUpdate,
};

pub const injector_has_applied_modifications_env_var_name = "__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS";
