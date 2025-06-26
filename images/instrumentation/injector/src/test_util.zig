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

pub fn expectStringStartAndEnd(actual: []const u8, prefix: []const u8, suffix: []const u8) !void {
    try testing.expectStringStartsWith(actual, prefix);
    try testing.expectStringEndsWith(actual, suffix);
}

pub fn expectStringContains(actual: []const u8, expected_needle: []const u8) !void {
    if (std.mem.indexOf(u8, actual, expected_needle) != null) {
        return;
    }
    print("\n====== expected to contain: =========\n", .{});
    printWithVisibleNewlines(expected_needle);
    print("\n========= full output: ==============\n", .{});
    printWithVisibleNewlines(actual);
    print("\n======================================\n", .{});

    return error.TestExpectedContains;
}

// Copied from Zig's std.testing module.
fn print(comptime fmt: []const u8, args: anytype) void {
    if (@inComptime()) {
        @compileError(std.fmt.comptimePrint(fmt, args));
    } else if (testing.backend_can_print) {
        std.debug.print(fmt, args);
    }
}

// Copied from Zig's std.testing module.
fn printWithVisibleNewlines(source: []const u8) void {
    var i: usize = 0;
    while (std.mem.indexOfScalar(u8, source[i..], '\n')) |nl| : (i += nl + 1) {
        printLine(source[i..][0..nl]);
    }
    print("{s}␃\n", .{source[i..]}); // End of Text symbol (ETX)
}

// Copied from Zig's std.testing module.
fn printLine(line: []const u8) void {
    if (line.len != 0) switch (line[line.len - 1]) {
        ' ', '\t' => return print("{s}⏎\n", .{line}), // Return symbol
        else => {},
    };
    print("{s}\n", .{line});
}

pub const DummyInstrumentationDirs = struct {
    dummy_instrumentation_parent: []const u8,
    dummy_instrumentation_base_dir: []const u8,
    dummy_instrumentation_dotnet_dir: []const u8,
    dummy_instrumentation_jvm_agent_path: []const u8,
    dummy_instrumentation_nodejs_dir: []const u8,
};

pub fn getDummyInstrumentationDirs() !DummyInstrumentationDirs {
    const cwd = std.fs.cwd().realpathAlloc(std.heap.page_allocator, ".") catch |err| {
        print("\n====== setup failed: =============\n", .{});
        print("ERROR: std.fs.cwd().realpathAlloc failed in test: {}\n", .{err});
        return err;
    };
    const dummy_instrumentation_dir = concat(cwd, "/__dash0__/instrumentation");
    return DummyInstrumentationDirs{
        .dummy_instrumentation_parent = cwd,
        .dummy_instrumentation_base_dir = dummy_instrumentation_dir,
        .dummy_instrumentation_dotnet_dir = concat(dummy_instrumentation_dir, "/dotnet"),
        .dummy_instrumentation_jvm_agent_path = concat(dummy_instrumentation_dir, "/jvm/opentelemetry-javaagent.jar"),
        .dummy_instrumentation_nodejs_dir = concat(dummy_instrumentation_dir, "/node.js/node_modules/@dash0hq/opentelemetry"),
    };
}

pub fn createAllDummyInstrumentations(dummy_instrumentation_dirs: DummyInstrumentationDirs) !void {
    try createDummyJavaAgent(dummy_instrumentation_dirs);
    try createDummyNodeJsDistribution(dummy_instrumentation_dirs);
    try createDummyDotnetInstrumentation(dummy_instrumentation_dirs);
}

pub fn createDummyJavaAgent(dummy_instrumentation_dirs: DummyInstrumentationDirs) !void {
    try createDummyDirectory(concat(dummy_instrumentation_dirs.dummy_instrumentation_base_dir, "/jvm"));
    try createDummyFile(concat(dummy_instrumentation_dirs.dummy_instrumentation_base_dir, "/jvm/opentelemetry-javaagent.jar"));
}

pub fn createDummyNodeJsDistribution(dummy_instrumentation_dirs: DummyInstrumentationDirs) !void {
    try createDummyDirectory(concat(dummy_instrumentation_dirs.dummy_instrumentation_nodejs_dir, "/node_modules/@dash0hq/opentelemetry"));
}

pub fn createDummyDotnetInstrumentation(dummy_instrumentation_dirs: DummyInstrumentationDirs) !void {
    const platform = comptime getDotnetPlatformForTest();
    const dotnetDir = concat(dummy_instrumentation_dirs.dummy_instrumentation_dotnet_dir, "/glibc");
    try createDummyDirectory(concat(dotnetDir, "/net"));
    const dotnetPlatformDir = concat3(dotnetDir, "/", platform);
    try createDummyDirectory(dotnetPlatformDir);
    try createDummyFile(concat(dotnetPlatformDir, "/OpenTelemetry.AutoInstrumentation.Native.so"));
    try createDummyFile(concat(dotnetDir, "/AdditionalDeps"));
    try createDummyFile(concat(dotnetDir, "/store"));
    try createDummyFile(concat(dotnetDir, "/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll"));
}

fn concat(a: []const u8, b: []const u8) []const u8 {
    return std.fmt.allocPrint(std.heap.page_allocator, "{s}{s}", .{ a, b }) catch |err| {
        print("\n====== failed: =============\n", .{});
        print("PANIC: concat({s}, {s}) failed in test: {}\n", .{ a, b, err });
        @panic("concat failed");
    };
}

fn concat3(a: []const u8, b: []const u8, c: []const u8) []const u8 {
    return std.fmt.allocPrint(std.heap.page_allocator, "{s}{s}{s}", .{ a, b, c }) catch |err| {
        print("\n====== failed: =============\n", .{});
        print("PANIC: concat3({s}, {s}, {s}) failed in test: {}\n", .{ a, b, c, err });
        @panic("concat failed");
    };
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
pub fn deleteDash0DummyDirectory(dummy_instrumentation_dirs: DummyInstrumentationDirs) void {
    const root_dir_or_error = std.fs.openDirAbsolute(dummy_instrumentation_dirs.dummy_instrumentation_parent, .{});
    if (root_dir_or_error) |root_dir| {
        root_dir.deleteTree("__dash0__") catch |err| {
            print("\n====== cleanup failed: ===========\n", .{});
            print("root_dir.deleteTree({s}) failed: {}\n", .{ "__dash0__", err });
        };
    } else |err| {
        print("\n====== cleanup failed: ===========\n", .{});
        print("std.fs.openDirAbsolute({s})) failed: {}\n", .{ dummy_instrumentation_dirs.dummy_instrumentation_parent, err });
    }
}
