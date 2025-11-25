// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const alloc = @import("allocator.zig");
const print = @import("print.zig");
const test_util = @import("test_util.zig");
const types = @import("types.zig");

const testing = std.testing;

pub const node_options_env_var_name = "NODE_OPTIONS";
const dash0_nodejs_otel_sdk_distribution = "/__dash0__/instrumentation/node.js/node_modules/@dash0/opentelemetry";
const require_dash0_nodejs_otel_sdk_distribution = "--require " ++ dash0_nodejs_otel_sdk_distribution;

pub fn checkNodeJsOTelSdkDistributionAndGetModifiedNodeOptionsValue(original_value_optional: ?[:0]const u8) ?types.NullTerminatedString {
    // Check the existence of the Node module: requiring or importing a module
    // that does not exist or cannot be opened will crash the Node.js process
    // with an 'ERR_MODULE_NOT_FOUND' error.
    std.fs.cwd().access(dash0_nodejs_otel_sdk_distribution, .{}) catch |err| {
        print.printError("Skipping injection of the Node.js OTel SDK distribution in '{s}' because of an issue accessing the Node.js module at {s}: {}", .{ node_options_env_var_name, dash0_nodejs_otel_sdk_distribution, err });
        return null;
    };
    return getModifiedNodeOptionsValue(original_value_optional);
}

test "checkNodeJsOTelDistributionAndGetModifiedNodeOptionsValue: should return null if the Node.js OTel SDK distribution cannot be accessed (no original value)" {
    const modifiedNodeOptionsValue = checkNodeJsOTelSdkDistributionAndGetModifiedNodeOptionsValue(null);
    try test_util.expectWithMessage(modifiedNodeOptionsValue == null, "modifiedNodeOptionsValue == null");
}

test "checkNodeJsOTelDistributionAndGetModifiedNodeOptionsValue: should return null if the Node.js OTel SDK distribution cannot be accessed (original value present)" {
    const modifiedNodeOptionsValue = checkNodeJsOTelSdkDistributionAndGetModifiedNodeOptionsValue("--abort-on-uncaught-exception"[0.. :0]);
    try test_util.expectWithMessage(modifiedNodeOptionsValue == null, "modifiedNodeOptionsValue == null");
}

fn getModifiedNodeOptionsValue(original_value_optional: ?[:0]const u8) ?types.NullTerminatedString {
    if (original_value_optional) |original_value| {
        if (std.mem.indexOf(u8, original_value, require_dash0_nodejs_otel_sdk_distribution)) |_| {
            // If the "--require ..." flag is already present in NODE_OPTIONS, do nothing. This is particularly
            // important to avoid double injection, for example if we are injecting into a container which has a shell
            // executable as its entry point (into which we inject env var modifications), and then this shell starts
            // the Node.js executable as a child process, which inherits the environment from the already injected
            // shell.
            return null;
        }

        // If NODE_OPTIONS is already set, prepend the "--require ..." flag to the original value.
        // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
        // parent process.
        const return_buffer = std.fmt.allocPrintZ(alloc.page_allocator, "{s} {s}", .{ require_dash0_nodejs_otel_sdk_distribution, original_value }) catch |err| {
            print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ node_options_env_var_name, err });
            return null;
        };
        return return_buffer.ptr;
    }

    // If NODE_OPTIONS is not set, simply return the "--require ..." flag.
    return require_dash0_nodejs_otel_sdk_distribution[0..].ptr;
}

test "getModifiedNodeOptionsValue: should return --require if original value is unset" {
    const modifiedNodeOptionsValue = getModifiedNodeOptionsValue(null);
    try testing.expectEqualStrings(
        "--require /__dash0__/instrumentation/node.js/node_modules/@dash0/opentelemetry",
        std.mem.span(modifiedNodeOptionsValue orelse "-"),
    );
}

test "getModifiedNodeOptionsValue: should prepend --require if original value exists" {
    const original_value: [:0]const u8 = "--abort-on-uncaught-exception"[0.. :0];
    const modified_node_options_value = getModifiedNodeOptionsValue(original_value);
    try testing.expectEqualStrings(
        "--require /__dash0__/instrumentation/node.js/node_modules/@dash0/opentelemetry --abort-on-uncaught-exception",
        std.mem.span(modified_node_options_value orelse "-"),
    );
}

test "getModifiedNodeOptionsValue: should do nothing if our --require is already present" {
    const modifiedNodeOptionsValue = getModifiedNodeOptionsValue("--abort-on-uncaught-exception --require /__dash0__/instrumentation/node.js/node_modules/@dash0/opentelemetry --something-else"[0.. :0]);
    try test_util.expectWithMessage(modifiedNodeOptionsValue == null, "modifiedNodeOptionsValue == null");
}
