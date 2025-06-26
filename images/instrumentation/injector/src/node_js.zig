// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const cache = @import("cache.zig");
const print = @import("print.zig");
const test_util = @import("test_util.zig");
const types = @import("types.zig");

const testing = std.testing;
const expectWithMessage = test_util.expectWithMessage;

pub const node_options_env_var_name = "NODE_OPTIONS";
pub const dash0_nodejs_otel_sdk_distribution = "/__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry";
const require_dash0_nodejs_otel_sdk_distribution = "--require " ++ dash0_nodejs_otel_sdk_distribution;
const injection_happened_msg = "injecting the Dash0 Node.js OpenTelemetry distribution";

pub fn checkNodeJsOTelSdkDistributionAndGetModifiedNodeOptionsValue(original_value_and_index_optional: ?types.EnvVarValueAndIndex) ?types.EnvVarUpdate {
    // Check the existence of the Node module: requiring or importing a module
    // that does not exist or cannot be opened will crash the Node.js process
    // with an 'ERR_MODULE_NOT_FOUND' error.
    std.fs.cwd().access(dash0_nodejs_otel_sdk_distribution, .{}) catch |err| {
        print.printError("Skipping injection of the Node.js OTel SDK distribution in '{s}' because of an issue accessing the Node.js module at {s}: {}", .{ node_options_env_var_name, dash0_nodejs_otel_sdk_distribution, err });
        if (original_value_and_index_optional) |original_value_and_index| {
            cache.injector_cache.node_options =
                cache.CachedEnvVarValue{
                    .value = original_value_and_index.value,
                    .done = true,
                };
            return types.EnvVarUpdate{
                .value = original_value_and_index.value,
                .replace = true,
                .index = original_value_and_index.index,
            };
        }
        cache.injector_cache.node_options =
            cache.CachedEnvVarValue{
                .value = null,
                .done = true,
            };
        return null;
    };
    return getModifiedNodeOptionsValue(original_value_and_index_optional);
}

test "checkNodeJsOTelDistributionAndGetModifiedNodeOptionsValue: should return null if the Node.js OTel SDK distribution cannot be accessed" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const env_var_update_optional = checkNodeJsOTelSdkDistributionAndGetModifiedNodeOptionsValue(null);
    try expectWithMessage(env_var_update_optional == null, "env_var_update_optional == null");

    try testing.expectEqual(true, cache.injector_cache.node_options.done);
    try expectWithMessage(cache.injector_cache.node_options.value == null, "cache.injector_cache.node_options.value == null");
}

test "checkNodeJsOTelDistributionAndGetModifiedNodeOptionsValue: should return the original value if the Node.js OTel SDK distribution cannot be accessed" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const env_var_update = checkNodeJsOTelSdkDistributionAndGetModifiedNodeOptionsValue(types.EnvVarValueAndIndex{
        .value = "--abort-on-uncaught-exception"[0.. :0],
        .index = 3,
    }).?;
    try testing.expectEqualStrings(
        "--abort-on-uncaught-exception",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(3, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.node_options.done);
    try testing.expectEqualStrings("--abort-on-uncaught-exception", std.mem.span(cache.injector_cache.node_options.value.?));
}

test "checkNodeJsOTelDistributionAndGetModifiedNodeOptionsValue: should return --require if original value is unset and the Node.js OTel SDK distribution can be accessed" {
    try test_util.createDummyNodeJsDistribution();
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const env_var_update = checkNodeJsOTelSdkDistributionAndGetModifiedNodeOptionsValue(null).?;
    try testing.expectEqualStrings(
        "--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(false, env_var_update.replace);
    try testing.expectEqual(0, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.node_options.done);
    try testing.expectEqualStrings("--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry", std.mem.span(cache.injector_cache.node_options.value.?));
}

fn getModifiedNodeOptionsValue(original_value_and_index_optional: ?types.EnvVarValueAndIndex) ?types.EnvVarUpdate {
    if (original_value_and_index_optional) |original_value_and_index| {
        // If NODE_OPTIONS is already set, prepend our "--require ..." flag to the original value.
        // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
        // parent process.
        const return_buffer =
            std.fmt.allocPrintZ(
                std.heap.page_allocator,
                "{s} {s}",
                .{ require_dash0_nodejs_otel_sdk_distribution, original_value_and_index.value },
            ) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ node_options_env_var_name, err });
                return types.EnvVarUpdate{
                    .value = original_value_and_index.value,
                    .replace = true,
                    .index = original_value_and_index.index,
                };
            };
        print.printMessage(injection_happened_msg, .{});
        cache.injector_cache.node_options =
            cache.CachedEnvVarValue{ .value = return_buffer.ptr, .done = true };
        return types.EnvVarUpdate{
            .value = return_buffer.ptr,
            .replace = true,
            .index = original_value_and_index.index,
        };
    }

    // If NODE_OPTIONS is not set, simply return our "--require ..." flag.
    print.printMessage(injection_happened_msg, .{});
    cache.injector_cache.node_options =
        cache.CachedEnvVarValue{
            .value = require_dash0_nodejs_otel_sdk_distribution[0..].ptr,
            .done = true,
        };
    return types.EnvVarUpdate{
        .value = require_dash0_nodejs_otel_sdk_distribution[0..].ptr,
        .replace = false,
        .index = 0,
    };
}

test "getModifiedNodeOptionsValue: should return --require if original value is unset" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const env_var_update = getModifiedNodeOptionsValue(null).?;
    try testing.expectEqualStrings(
        "--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(false, env_var_update.replace);
    try testing.expectEqual(0, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.node_options.done);
    try testing.expectEqualStrings("--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry", std.mem.span(cache.injector_cache.node_options.value.?));
}

test "getModifiedNodeOptionsValue: should prepend --require if original value exists" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const env_var_update = getModifiedNodeOptionsValue(types.EnvVarValueAndIndex{
        .value = "--abort-on-uncaught-exception"[0.. :0],
        .index = 3,
    }).?;
    try testing.expectEqualStrings(
        "--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --abort-on-uncaught-exception",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(3, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.node_options.done);
    try testing.expectEqualStrings("--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --abort-on-uncaught-exception", std.mem.span(cache.injector_cache.node_options.value.?));
}
