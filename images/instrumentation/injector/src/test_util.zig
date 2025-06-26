// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const builtin = @import("builtin");
const std = @import("std");

const testing = std.testing;

/// An extended version of std.testing.expect that prints a message in case of failure. Useful for tests that have
/// multiple expects; this version should generally be used in favor of the original std.testing.expect function.
pub fn expectWithMessage(
    ok: bool,
    comptime message: []const u8,
) !void {
    if (!ok) {
        print("\n====== assertion failed: =========\n", .{});
        print(message, .{});
        print("\n==================================\n", .{});
        return error.TestUnexpectedResult;
    }
}

// Copied from Zig's std.testing module.
fn print(comptime fmt: []const u8, args: anytype) void {
    if (@inComptime()) {
        @compileError(std.fmt.comptimePrint(fmt, args));
    } else if (testing.backend_can_print) {
        std.debug.print(fmt, args);
    }
}

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

/// Creates a file at the given path.
pub fn createDummyFile(path: []const u8) !void {
    _ = std.fs.createFileAbsolute(path, .{}) catch |err| {
        print("\n====== setup failed: =============\n", .{});
        print("ERROR: std.fs.createFileAbsolute({s}) failed in test: {}\n", .{ path, err });
        return err;
    };
}

/// Creates a directory at the given path.
pub fn createDummyDirectory(path: []const u8) !void {
    const root_dir = std.fs.openDirAbsolute("/", .{}) catch |err| {
        print("\n====== setup failed: =============\n", .{});
        print("ERROR: std.fs.openDirAbsolute(\"/\") failed in test: {}\n", .{err});
        return err;
    };
    root_dir.makePath(path) catch |err| {
        print("\n====== setup failed: =============\n", .{});
        print("ERROR: root_dir.makePath({s}) failed in test: {}\n", .{ path, err });
        return err;
    };
}

/// The equivalent of `rm -rf /__dash0__`.
pub fn deleteDash0DummyDirectory() void {
    const root_dir_or_error = std.fs.openDirAbsolute("/", .{});
    if (root_dir_or_error) |root_dir| {
        root_dir.deleteTree("__dash0__") catch |err| {
            print("\n====== cleanup failed: ===========\n", .{});
            print("root_dir.deleteTree({s}) failed: {}\n", .{ "__dash0__", err });
        };
    } else |err| {
        print("\n====== cleanup failed: ===========\n", .{});
        print("std.fs.openDirAbsolute({s})) failed: {}\n", .{ "/", err });
    }
}
