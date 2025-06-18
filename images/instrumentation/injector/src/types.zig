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
    /// The value of the environment variable to set.
    value: NullTerminatedString,
    /// The index of the environment variable in the original environment variables list. If this is true, the required
    /// action is to replace the environment variable at that index with the new value. If this is false, the required
    /// action is to append the new value to the end of the environment variables list.
    replace: bool,
    /// The index of the environment variable in the original environment variables list. This value is only valid if
    /// replace is true, otherwise it must be ignored.
    index: usize,
};
