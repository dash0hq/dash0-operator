// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// All files with unit tests need to be referenced here:
pub const _ = @import("root.zig");

// Provide a C-style `char **environ` variable to the linker, to satisfy the
//   extern var __environ: [*]u8;
// declaration in `root.zig`.
var ___environ: [100]u8 = [_]u8{0} ** 100;
const ___environ_ptr: *[100]u8 = &___environ;
export var __environ: [*]u8 = ___environ_ptr;

test {
    @import("std").testing.refAllDecls(@This());
}
