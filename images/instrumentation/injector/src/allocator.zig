// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

pub const page_allocator: std.mem.Allocator = std.heap.page_allocator;
