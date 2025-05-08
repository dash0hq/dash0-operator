// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

// We need to allocate memory only to manipulate and return the few environment
// variables we want to modify. Unmodified values are returned as pointers to
// the original `__environ` memory. We pre-allocate an obscene 128Kb for it.
// TODO Make it PageAllocator based
var allocator_buffer: [131072:0]u8 = undefined;
var fba = std.heap.FixedBufferAllocator.init(&allocator_buffer);
pub const allocator: std.mem.Allocator = fba.allocator();
