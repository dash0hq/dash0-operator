// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const print = @import("print.zig");
const test_util = @import("test_util.zig");

const testing = std.testing;

// Note on having tests embedded in the actual source files versus having them in a separate *_test.zig file: Proper
// pure unit tests are usually directly in the source file of the production function they are testing. More invasive
// tests that need to change the environment variables (for example) should go in a separate file, so we never run the
// risk of even compiling the test mechanism to modify the environment.

test "initLogLevel: DASH0_INJECTOR_DEBUG and DASH0_INJECTOR_LOG_LEVEL are not set" {
    const original_environ = try test_util.clearStdCEnviron();
    defer test_util.reset(original_environ);
    print.initLogLevel();
    try testing.expectEqual(.Error, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_DEBUG=false, no DASH0_INJECTOR_LOG_LEVEL" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_INJECTOR_DEBUG=false"});
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Error, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_DEBUG is an arbitrary string, no DASH0_INJECTOR_LOG_LEVEL" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_INJECTOR_DEBUG=whatever"});
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Error, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_DEBUG=true, no DASH0_INJECTOR_LOG_LEVEL" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_INJECTOR_DEBUG=true"});
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Debug, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=debug, no DASH0_INJECTOR_DEBUG" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_INJECTOR_LOG_LEVEL=debug"});
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Debug, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=info, no DASH0_INJECTOR_DEBUG" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_INJECTOR_LOG_LEVEL=info"});
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Info, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=warn, no DASH0_INJECTOR_DEBUG" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_INJECTOR_LOG_LEVEL=warn"});
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Warn, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=error, no DASH0_INJECTOR_DEBUG" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_INJECTOR_LOG_LEVEL=error"});
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Error, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=none, no DASH0_INJECTOR_DEBUG" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_INJECTOR_LOG_LEVEL=none"});
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.None, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL is an arbitrary string, no DASH0_INJECTOR_DEBUG" {
    const original_environ = try test_util.setStdCEnviron(&[1][]const u8{"DASH0_INJECTOR_LOG_LEVEL=whatever"});
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Error, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=debug, DASH0_INJECTOR_DEBUG=true" {
    const original_environ = try test_util.setStdCEnviron(&[2][]const u8{ "DASH0_INJECTOR_LOG_LEVEL=debug", "DASH0_INJECTOR_DEBUG=true" });
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Debug, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=error, DASH0_INJECTOR_DEBUG=true" {
    const original_environ = try test_util.setStdCEnviron(&[2][]const u8{ "DASH0_INJECTOR_LOG_LEVEL=error", "DASH0_INJECTOR_DEBUG=true" });
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Debug, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=none, DASH0_INJECTOR_DEBUG=true" {
    const original_environ = try test_util.setStdCEnviron(&[2][]const u8{ "DASH0_INJECTOR_LOG_LEVEL=none", "DASH0_INJECTOR_DEBUG=true" });
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Debug, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=debug, DASH0_INJECTOR_DEBUG=false" {
    const original_environ = try test_util.setStdCEnviron(&[2][]const u8{ "DASH0_INJECTOR_LOG_LEVEL=debug", "DASH0_INJECTOR_DEBUG=false" });
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Debug, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=info, DASH0_INJECTOR_DEBUG=false" {
    const original_environ = try test_util.setStdCEnviron(&[2][]const u8{ "DASH0_INJECTOR_LOG_LEVEL=info", "DASH0_INJECTOR_DEBUG=false" });
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Info, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=warn, DASH0_INJECTOR_DEBUG=false" {
    const original_environ = try test_util.setStdCEnviron(&[2][]const u8{ "DASH0_INJECTOR_LOG_LEVEL=warn", "DASH0_INJECTOR_DEBUG=false" });
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Warn, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=error, DASH0_INJECTOR_DEBUG=false" {
    const original_environ = try test_util.setStdCEnviron(&[2][]const u8{ "DASH0_INJECTOR_LOG_LEVEL=error", "DASH0_INJECTOR_DEBUG=false" });
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.Error, print.getLogLevel());
}

test "initLogLevel: DASH0_INJECTOR_LOG_LEVEL=none, DASH0_INJECTOR_DEBUG=false" {
    const original_environ = try test_util.setStdCEnviron(&[2][]const u8{ "DASH0_INJECTOR_LOG_LEVEL=none", "DASH0_INJECTOR_DEBUG=false" });
    defer test_util.reset(original_environ);

    print.initLogLevel();
    try testing.expectEqual(.None, print.getLogLevel());
}
