// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const builtin = @import("builtin");
const std = @import("std");

pub const test_allocator: std.mem.Allocator = std.heap.page_allocator;

pub fn createAllDummyInstrumentations() !void {
    try createDummyJavaAgent();
    try createDummyNodeJsDistribution();
    try createDummyDotnetInstrumentation();
}

pub fn createDummyJavaAgent() !void {
    try createDummyDirectory("/__dash0__/instrumentation/jvm/");
    _ = try std.fs.createFileAbsolute("/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", .{});
}

pub fn createDummyNodeJsDistribution() !void {
    try createDummyDirectory("/__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry");
}

pub fn createDummyDotnetInstrumentation() !void {
    const platform = comptime getDotnetPlatformForTest();
    const dotnetDir = "/__dash0__/instrumentation/dotnet/glibc";
    try createDummyDirectory(dotnetDir ++ "/net");
    const dotnetPlatformDir = dotnetDir ++ "/" ++ platform;
    try createDummyDirectory(dotnetPlatformDir);
    _ = try std.fs.createFileAbsolute(dotnetPlatformDir ++ "/OpenTelemetry.AutoInstrumentation.Native.so", .{});
    _ = try std.fs.createFileAbsolute(dotnetDir ++ "/AdditionalDeps", .{});
    _ = try std.fs.createFileAbsolute(dotnetDir ++ "/store", .{});
    _ = try std.fs.createFileAbsolute(dotnetDir ++ "/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll", .{});
}

pub fn getDotnetPlatformForTest() []const u8 {
    return switch (builtin.cpu.arch) {
        .x86_64 => "linux-x64",
        .aarch64 => "linux-arm64",
        else => return error.TestUnexpectedResult,
    };
}

/// Creates a directory at the given path.
pub fn createDummyDirectory(path: []const u8) !void {
    const root_dir = try std.fs.openDirAbsolute("/", .{});
    try root_dir.makePath(path);
}

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
