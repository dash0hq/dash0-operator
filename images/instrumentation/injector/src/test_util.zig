// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// **Note: This file must only be imported from *_test.zig files, never from actual production code zig files.**

const std = @import("std");

pub const test_allocator: std.mem.Allocator = std.heap.page_allocator;

/// The equivalent of `rm -rf /__dash0__`.
pub fn deleteDash0DummyDirectory() void {
    const root_dir_or_error = std.fs.openDirAbsolute("/", .{});
    if (root_dir_or_error) |root_dir| {
        root_dir.deleteTree("__dash0__") catch |err| {
            std.debug.print("Failed to delete dummy distribution: {}\n", .{err});
        };
    } else |err| {
        std.debug.print("Failed to open dir for dummy distribution: {}\n", .{err});
    }
}

pub fn createDummyDirectory(path: []const u8) !void {
    const root_dir = try std.fs.openDirAbsolute("/", .{});
    try root_dir.makePath(path);
}
