// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const cache = @import("cache.zig");
const dotnet = @import("dotnet.zig");
const env = @import("env.zig");
const jvm = @import("jvm.zig");
const node_js = @import("node_js.zig");
const print = @import("print.zig");
const res_attrs = @import("resource_attributes.zig");
const test_util = @import("test_util.zig");
const types = @import("types.zig");

const testing = std.testing;

pub fn _getenv(original_env_vars: [](types.NullTerminatedString), name_z: types.NullTerminatedString) ?types.NullTerminatedString {
    const name: []const u8 = std.mem.span(name_z);
    // print.printDebug("getenv({s})\n", .{name});
    const potentially_modified_value = modifyEnvVar(original_env_vars, name);
    // if (print.isDebug()) {
    //     if (potentially_modified_value) |value| {
    //         print.printDebug("getenv({s}) -> '{s}'", .{ name, value });
    //     } else {
    //         print.printDebug("getenv({s}) -> null", .{name});
    //     }
    // }
    return potentially_modified_value;
}

test "_getenv: no changes, no original value" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 3);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "VAR1=value1";
    original_env_vars[1] = "VAR2=value2";
    original_env_vars[2] = "VAR3=value3";

    const value_optional = _getenv(original_env_vars, "VAR4");
    try testing.expect(value_optional == null);
}

test "_getenv: no changes, original value is empty string" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 3);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "VAR1=value1";
    original_env_vars[1] = "VAR2=";
    original_env_vars[2] = "VAR3=value3";

    const value_optional = _getenv(original_env_vars, "VAR2");
    try testing.expectEqualStrings("", std.mem.span(value_optional.?));
}

test "_getenv: no changes, original value exists" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 3);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "VAR1=value1";
    original_env_vars[1] = "VAR2=value2";
    original_env_vars[2] = "VAR3=value3";

    const value_optional = _getenv(original_env_vars, "VAR2");
    try testing.expectEqualStrings("value2", std.mem.span(value_optional.?));
}

test "_getenv: add new NODE_OPTIONS value when not set" {
    try test_util.createDummyNodeJsDistribution();
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 3);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "VAR1=value1";
    original_env_vars[1] = "VAR2=value2";
    original_env_vars[2] = "VAR3=value3";

    const value_optional = _getenv(original_env_vars, node_js.node_options_env_var_name);
    try testing.expectEqualStrings("--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry", std.mem.span(value_optional.?));
    try testing.expect(cache.injector_cache.node_options.done);
    try testing.expectEqualStrings("--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry", std.mem.span(cache.injector_cache.node_options.value.?));
}

test "_getenv: prepend to existing NODE_OPTIONS value" {
    try test_util.createDummyNodeJsDistribution();
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 2);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "NODE_OPTIONS=--abort-on-uncaught-exception";
    original_env_vars[1] = "VAR1=value1";

    const value_optional = _getenv(
        original_env_vars,
        node_js.node_options_env_var_name,
    );
    try testing.expectEqualStrings(
        "--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --abort-on-uncaught-exception",
        std.mem.span(value_optional.?),
    );
    try testing.expect(cache.injector_cache.node_options.done);
    try testing.expectEqualStrings("--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --abort-on-uncaught-exception", std.mem.span(cache.injector_cache.node_options.value.?));
}

fn modifyEnvVar(original_env_vars: [](types.NullTerminatedString), name: []const u8) ?types.NullTerminatedString {
    // Maintenance note: The environment variables modified by environ_init.applyModifications must be kept in sync with
    // the environment variables modified by the environ_api.modifyEnvVar.

    const original_value_and_index_optional = env.getEnvVar(original_env_vars, name);
    const environ_init_modifications_have_been_applied_in_parent_process =
        env.isTrue(original_env_vars, types.injector_has_applied_modifications_env_var_name);

    if (environ_init_modifications_have_been_applied_in_parent_process) {
        if (original_value_and_index_optional) |original_value_and_index| {
            return original_value_and_index.value;
        }
        return null;
    }

    if (std.mem.eql(u8, name, jvm.java_tool_options_env_var_name)) {
        if (cache.injector_cache.java_tool_options.done) {
            return cache.injector_cache.java_tool_options.value;
        }
        if (jvm.checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue(original_env_vars)) |env_var_update| {
            return env_var_update.value;
        }
    } else if (std.mem.eql(u8, name, node_js.node_options_env_var_name)) {
        if (cache.injector_cache.node_options.done) {
            return cache.injector_cache.node_options.value;
        }
        if (node_js.checkNodeJsOTelSdkDistributionAndGetModifiedNodeOptionsValue(original_value_and_index_optional)) |env_var_update| {
            return env_var_update.value;
        }
    } else if (std.mem.eql(u8, name, res_attrs.otel_resource_attributes_env_var_name)) {
        if (cache.injector_cache.otel_resource_attributes.done) {
            return cache.injector_cache.otel_resource_attributes.value;
        }
        if (res_attrs.getModifiedOtelResourceAttributesValue(original_env_vars)) |env_var_update| {
            return env_var_update.value;
        }
    } else if (dotnet.isEnabled(original_env_vars)) {
        if (std.mem.eql(u8, name, dotnet.coreclr_enable_profiling_env_var_name)) {
            if (dotnet.getDotnetEnvVarUpdates()) |dotnet_env_var_updates| {
                return dotnet_env_var_updates.coreclr_enable_profiling.value;
            }
        } else if (std.mem.eql(u8, name, dotnet.coreclr_profiler_env_var_name)) {
            if (dotnet.getDotnetEnvVarUpdates()) |dotnet_env_var_updates| {
                return dotnet_env_var_updates.coreclr_profiler.value;
            }
        } else if (std.mem.eql(u8, name, dotnet.coreclr_profiler_path_env_var_name)) {
            if (dotnet.getDotnetEnvVarUpdates()) |dotnet_env_var_updates| {
                return dotnet_env_var_updates.coreclr_profiler_path.value;
            }
        } else if (std.mem.eql(u8, name, dotnet.dotnet_additional_deps_env_var_name)) {
            if (dotnet.getDotnetEnvVarUpdates()) |dotnet_env_var_updates| {
                return dotnet_env_var_updates.dotnet_additional_deps.value;
            }
        } else if (std.mem.eql(u8, name, dotnet.dotnet_shared_store_env_var_name)) {
            if (dotnet.getDotnetEnvVarUpdates()) |dotnet_env_var_updates| {
                return dotnet_env_var_updates.dotnet_shared_store.value;
            }
        } else if (std.mem.eql(u8, name, dotnet.dotnet_startup_hooks_env_var_name)) {
            if (dotnet.getDotnetEnvVarUpdates()) |dotnet_env_var_updates| {
                return dotnet_env_var_updates.dotnet_startup_hooks.value;
            }
        } else if (std.mem.eql(u8, name, dotnet.otel_auto_home_env_var_name)) {
            if (dotnet.getDotnetEnvVarUpdates()) |dotnet_env_var_updates| {
                return dotnet_env_var_updates.otel_auto_home.value;
            }
        }
    }

    // The requested environment variable is either not a variable that we we want to modify, or the preconditions for
    // modifying it are not met (i.e. the Node.js OTel SDK distribution is not available or similar). Hence we return
    // the original unmodified value.
    if (original_value_and_index_optional) |original_value_and_index| {
        return original_value_and_index.value;
    }

    // The requested environment variable is either not a variable that we we want to modify, or the preconditions for
    // modifying it are not met (i.e. the Node.js OTel SDK distribution is not available or similar). Additionally, the
    // environment variable is not set, i.e. it is absent in the original unmodified environment. Hence we return
    // null.
    return null;
}

test "modifyEnvVar: irrelevant environment variable, no original value" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(original_env_vars);

    const value_optional = modifyEnvVar(original_env_vars, "ENV_VAR");
    try testing.expect(value_optional == null);
}

test "modifyEnvVar: irrelevant environment variable, original value is empty string" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "ENV_VAR=";

    const value_optional = modifyEnvVar(original_env_vars, "ENV_VAR");
    try testing.expectEqualStrings("", std.mem.span(value_optional.?));
}

test "modifyEnvVar: irrelevant environment variable, original value exists" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "ENV_VAR=value";

    const value_optional = modifyEnvVar(original_env_vars, "ENV_VAR");
    try testing.expectEqualStrings("value", std.mem.span(value_optional.?));
}

test "modifyEnvVar: leave JAVA_TOOL_OPTIONS unchanged (null) when not set and Java agent is not available" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(original_env_vars);

    const value_optional = modifyEnvVar(original_env_vars, jvm.java_tool_options_env_var_name);
    try testing.expect(value_optional == null);
    try testing.expect(cache.injector_cache.java_tool_options.done);
    try testing.expect(cache.injector_cache.java_tool_options.value == null);
}

test "modifyEnvVar: add new JAVA_TOOL_OPTIONS when Java agent is available" {
    try test_util.createDummyJavaAgent();
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(original_env_vars);

    const value_optional = modifyEnvVar(original_env_vars, jvm.java_tool_options_env_var_name);
    try testing.expectEqualStrings("-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", std.mem.span(value_optional.?));
    try testing.expect(cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

test "modifyEnvVar: leave existing JAVA_TOOL_OPTIONS unchanged when Java agent is not available" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "JAVA_TOOL_OPTIONS=-Dsome.property=value";

    const value_optional = modifyEnvVar(original_env_vars, jvm.java_tool_options_env_var_name);
    try testing.expectEqualStrings("-Dsome.property=value", std.mem.span(value_optional.?));
    try testing.expect(cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("-Dsome.property=value", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

test "modifyEnvVar: append to existing JAVA_TOOL_OPTIONS when Java agent is available" {
    try test_util.createDummyJavaAgent();
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "JAVA_TOOL_OPTIONS=-Dsome.property=value";

    const value_optional = modifyEnvVar(original_env_vars, jvm.java_tool_options_env_var_name);
    try testing.expectEqualStrings("-Dsome.property=value -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", std.mem.span(value_optional.?));
    try testing.expect(cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("-Dsome.property=value -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

test "modifyEnvVar: use cached JAVA_TOOL_OPTIONS null value" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    cache.injector_cache.java_tool_options = cache.CachedEnvVarValue{
        .done = true,
        .value = null,
    };

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(original_env_vars);

    const value_optional = modifyEnvVar(original_env_vars, jvm.java_tool_options_env_var_name);
    try testing.expect(value_optional == null);
    try testing.expect(cache.injector_cache.java_tool_options.done);
    try testing.expect(cache.injector_cache.java_tool_options.value == null);
}

test "modifyEnvVar: use cached JAVA_TOOL_OPTIONS value" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    cache.injector_cache.java_tool_options = cache.CachedEnvVarValue{
        .done = true,
        .value = "cached JAVA_TOOL_OPTIONS value"[0.. :0],
    };

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(original_env_vars);

    const value_optional = modifyEnvVar(original_env_vars, jvm.java_tool_options_env_var_name);
    try testing.expectEqualStrings("cached JAVA_TOOL_OPTIONS value", std.mem.span(value_optional.?));
    try testing.expect(cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("cached JAVA_TOOL_OPTIONS value", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

test "modifyEnvVar: leave NODE_OPTIONS unchanged (null) when not set and Node.js OTel SDK distribution is not available" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "ENV_VAR=value";

    const value_optional = modifyEnvVar(original_env_vars, node_js.node_options_env_var_name);
    try testing.expect(value_optional == null);
    try testing.expect(cache.injector_cache.node_options.done);
    try testing.expect(cache.injector_cache.node_options.value == null);
}

test "modifyEnvVar: add new NODE_OPTIONS when Node.js OTel SDK distribution is available" {
    try test_util.createDummyNodeJsDistribution();
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(original_env_vars);

    const value_optional = modifyEnvVar(original_env_vars, node_js.node_options_env_var_name);
    try testing.expectEqualStrings("--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry", std.mem.span(value_optional.?));
    try testing.expect(cache.injector_cache.node_options.done);
    try testing.expectEqualStrings("--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry", std.mem.span(cache.injector_cache.node_options.value.?));
}

test "modifyEnvVar: leave existing NODE_OPTIONS unchanged when Node.js OTel SDK distribution is not available" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "NODE_OPTIONS=--abort-on-uncaught-exception";

    const value_optional = modifyEnvVar(original_env_vars, node_js.node_options_env_var_name);
    try testing.expectEqualStrings("--abort-on-uncaught-exception", std.mem.span(value_optional.?));
    try testing.expect(cache.injector_cache.node_options.done);
    try testing.expectEqualStrings("--abort-on-uncaught-exception", std.mem.span(cache.injector_cache.node_options.value.?));
}

test "modifyEnvVar: prepend to existing NODE_OPTIONS when Node.js OTel SDK distribution is available" {
    try test_util.createDummyNodeJsDistribution();
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "NODE_OPTIONS=--abort-on-uncaught-exception";

    const value_optional = modifyEnvVar(original_env_vars, node_js.node_options_env_var_name);
    try testing.expectEqualStrings("--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --abort-on-uncaught-exception", std.mem.span(value_optional.?));
    try testing.expect(cache.injector_cache.node_options.done);
    try testing.expectEqualStrings("--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --abort-on-uncaught-exception", std.mem.span(cache.injector_cache.node_options.value.?));
}

test "modifyEnvVar: use cached NODE_OPTIONS null value" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    cache.injector_cache.node_options = cache.CachedEnvVarValue{
        .done = true,
        .value = null,
    };

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(original_env_vars);

    const value_optional = modifyEnvVar(original_env_vars, node_js.node_options_env_var_name);
    try testing.expect(value_optional == null);
    try testing.expect(cache.injector_cache.node_options.done);
    try testing.expect(cache.injector_cache.node_options.value == null);
}

test "modifyEnvVar: use cached NODE_OPTIONS value" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    cache.injector_cache.node_options = cache.CachedEnvVarValue{
        .done = true,
        .value = "cached NODE_OPTIONS value"[0.. :0],
    };

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(original_env_vars);

    const value_optional = modifyEnvVar(original_env_vars, node_js.node_options_env_var_name);
    try testing.expectEqualStrings("cached NODE_OPTIONS value", std.mem.span(value_optional.?));
    try testing.expect(cache.injector_cache.node_options.done);
    try testing.expectEqualStrings("cached NODE_OPTIONS value", std.mem.span(cache.injector_cache.node_options.value.?));
}

test "modifyEnvVar: add new OTEL_RESOURCE_ATTRIBUTES from source env vars" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 8);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_NAMESPACE_NAME=namespace";
    original_env_vars[1] = "DASH0_POD_NAME=pod";
    original_env_vars[2] = "DASH0_POD_UID=uid";
    original_env_vars[3] = "DASH0_CONTAINER_NAME=container";
    original_env_vars[4] = "DASH0_SERVICE_NAME=service";
    original_env_vars[5] = "DASH0_SERVICE_VERSION=version";
    original_env_vars[6] = "DASH0_SERVICE_NAMESPACE=servicenamespace";
    original_env_vars[7] = "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd";

    const value_optional = modifyEnvVar(original_env_vars, res_attrs.otel_resource_attributes_env_var_name);
    try testing.expectEqualStrings(
        "k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd",
        std.mem.span(value_optional.?),
    );
    try testing.expect(cache.injector_cache.otel_resource_attributes.done);
    try testing.expectEqualStrings(
        "k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd",
        std.mem.span(cache.injector_cache.otel_resource_attributes.value.?),
    );
}

test "modifyEnvVar: compose OTEL_RESOURCE_ATTRIBUTES, OTEL_RESOURCE_ATTRIBUTES present but empty, source env vars present" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 5);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_NAMESPACE_NAME=namespace";
    original_env_vars[1] = "DASH0_POD_NAME=pod";
    original_env_vars[2] = "OTEL_RESOURCE_ATTRIBUTES=";
    original_env_vars[3] = "DASH0_POD_UID=uid";
    original_env_vars[4] = "DASH0_CONTAINER_NAME=container";

    const value_optional = modifyEnvVar(original_env_vars, res_attrs.otel_resource_attributes_env_var_name);
    try testing.expectEqualStrings(
        "k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container",
        std.mem.span(value_optional.?),
    );
    try testing.expect(cache.injector_cache.otel_resource_attributes.done);
    try testing.expectEqualStrings(
        "k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container",
        std.mem.span(cache.injector_cache.otel_resource_attributes.value.?),
    );
}

test "modifyEnvVar: append to existing OTEL_RESOURCE_ATTRIBUTES" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 9);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_NAMESPACE_NAME=namespace";
    original_env_vars[1] = "DASH0_POD_NAME=pod";
    original_env_vars[2] = "DASH0_POD_UID=uid";
    original_env_vars[3] = "DASH0_CONTAINER_NAME=container";
    original_env_vars[4] = "OTEL_RESOURCE_ATTRIBUTES=key1=value1,key2=value2";
    original_env_vars[5] = "DASH0_SERVICE_NAME=service";
    original_env_vars[6] = "DASH0_SERVICE_VERSION=version";
    original_env_vars[7] = "DASH0_SERVICE_NAMESPACE=servicenamespace";
    original_env_vars[8] = "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd";

    const value_optional = modifyEnvVar(original_env_vars, res_attrs.otel_resource_attributes_env_var_name);
    try testing.expectEqualStrings(
        "k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd,key1=value1,key2=value2",
        std.mem.span(value_optional.?),
    );
    try testing.expect(cache.injector_cache.otel_resource_attributes.done);
    try testing.expectEqualStrings(
        "k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd,key1=value1,key2=value2",
        std.mem.span(cache.injector_cache.otel_resource_attributes.value.?),
    );
}

test "modifyEnvVar: use cached OTEL_RESOURCE_ATTRIBUTES null value" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    cache.injector_cache.otel_resource_attributes = cache.CachedEnvVarValue{
        .done = true,
        .value = null,
    };

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(original_env_vars);

    const value_optional = modifyEnvVar(original_env_vars, res_attrs.otel_resource_attributes_env_var_name);
    try testing.expect(value_optional == null);
    try testing.expect(cache.injector_cache.otel_resource_attributes.done);
    try testing.expect(cache.injector_cache.otel_resource_attributes.value == null);
}

test "modifyEnvVar: use cached OTEL_RESOURCE_ATTRIBUTES value" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    cache.injector_cache.otel_resource_attributes = cache.CachedEnvVarValue{
        .done = true,
        .value = "cached OTEL_RESOURCE_ATTRIBUTES value"[0.. :0],
    };

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(original_env_vars);

    const value_optional = modifyEnvVar(original_env_vars, res_attrs.otel_resource_attributes_env_var_name);
    try testing.expectEqualStrings("cached OTEL_RESOURCE_ATTRIBUTES value", std.mem.span(value_optional.?));
    try testing.expect(cache.injector_cache.otel_resource_attributes.done);
    try testing.expectEqualStrings("cached OTEL_RESOURCE_ATTRIBUTES value", std.mem.span(cache.injector_cache.otel_resource_attributes.value.?));
}

test "modifyEnvVar: do not add .NET values if not enabled" {
    try test_util.createDummyDotnetInstrumentation();
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    // set a dummy value for the libc flavor just for the test, will be reverted after the test via
    // `defer cache.injector_cache = cache.emptyInjectorCache()`
    cache.injector_cache.libc_flavor = types.LibCFlavor.GNU_LIBC;

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(original_env_vars);

    var value_optional = modifyEnvVar(original_env_vars, dotnet.coreclr_enable_profiling_env_var_name);
    try testing.expect(value_optional == null);
    value_optional = modifyEnvVar(original_env_vars, dotnet.coreclr_profiler_env_var_name);
    try testing.expect(value_optional == null);
    value_optional = modifyEnvVar(original_env_vars, dotnet.coreclr_profiler_path_env_var_name);
    try testing.expect(value_optional == null);
    value_optional = modifyEnvVar(original_env_vars, dotnet.dotnet_additional_deps_env_var_name);
    try testing.expect(value_optional == null);
    value_optional = modifyEnvVar(original_env_vars, dotnet.dotnet_shared_store_env_var_name);
    try testing.expect(value_optional == null);
    value_optional = modifyEnvVar(original_env_vars, dotnet.dotnet_startup_hooks_env_var_name);
    try testing.expect(value_optional == null);
    value_optional = modifyEnvVar(original_env_vars, dotnet.otel_auto_home_env_var_name);
    try testing.expect(value_optional == null);
}

test "modifyEnvVar: add new .NET values if enabled" {
    try test_util.createDummyDotnetInstrumentation();
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    // set a dummy value for the libc flavor just for the test, will be reverted after the test via
    // `defer cache.injector_cache = cache.emptyInjectorCache()`
    cache.injector_cache.libc_flavor = types.LibCFlavor.GNU_LIBC;

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_EXPERIMENTAL_DOTNET_INJECTION=true";

    var value_optional = modifyEnvVar(original_env_vars, dotnet.coreclr_enable_profiling_env_var_name);
    try testing.expectEqualStrings("1", std.mem.span(value_optional.?));
    value_optional = modifyEnvVar(original_env_vars, dotnet.coreclr_profiler_env_var_name);
    try testing.expectEqualStrings("{918728DD-259F-4A6A-AC2B-B85E1B658318}", std.mem.span(value_optional.?));
    value_optional = modifyEnvVar(original_env_vars, dotnet.coreclr_profiler_path_env_var_name);
    const expected_profiler_path = comptime "/__dash0__/instrumentation/dotnet/glibc/" ++
        test_util.getDotnetPlatformForTest() ++
        "/OpenTelemetry.AutoInstrumentation.Native.so";
    try testing.expectEqualStrings(
        expected_profiler_path,
        std.mem.span(value_optional.?),
    );
    value_optional = modifyEnvVar(original_env_vars, dotnet.dotnet_additional_deps_env_var_name);
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/AdditionalDeps",
        std.mem.span(value_optional.?),
    );
    value_optional = modifyEnvVar(original_env_vars, dotnet.dotnet_shared_store_env_var_name);
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/store",
        std.mem.span(value_optional.?),
    );
    value_optional = modifyEnvVar(original_env_vars, dotnet.dotnet_startup_hooks_env_var_name);
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll",
        std.mem.span(value_optional.?),
    );
    value_optional = modifyEnvVar(original_env_vars, dotnet.otel_auto_home_env_var_name);
    try testing.expectEqualStrings(
        "/__dash0__/instrumentation/dotnet/glibc",
        std.mem.span(value_optional.?),
    );

    try testing.expect(cache.injector_cache.dotnet_env_var_updates.done);
    try testing.expectEqualStrings("1", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.coreclr_enable_profiling.value));
    try testing.expectEqualStrings("{918728DD-259F-4A6A-AC2B-B85E1B658318}", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.coreclr_profiler.value));
    try testing.expectEqualStrings(expected_profiler_path, std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.coreclr_profiler_path.value));
    try testing.expectEqualStrings("/__dash0__/instrumentation/dotnet/glibc/AdditionalDeps", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.dotnet_additional_deps.value));
    try testing.expectEqualStrings("/__dash0__/instrumentation/dotnet/glibc/store", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.dotnet_shared_store.value));
    try testing.expectEqualStrings("/__dash0__/instrumentation/dotnet/glibc/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.dotnet_startup_hooks.value));
    try testing.expectEqualStrings("/__dash0__/instrumentation/dotnet/glibc", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.otel_auto_home.value));
}

test "modifyEnvVar: use cached .NET null values" {
    try test_util.createDummyDotnetInstrumentation();
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    cache.injector_cache.dotnet_env_var_updates = cache.CachedDotnetEnvVarUpdates{
        .done = true,
        .values = null,
    };

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_EXPERIMENTAL_DOTNET_INJECTION=true";

    cache.injector_cache.libc_flavor = types.LibCFlavor.GNU_LIBC;

    var value_optional = modifyEnvVar(original_env_vars, dotnet.coreclr_enable_profiling_env_var_name);
    try testing.expect(value_optional == null);
    value_optional = modifyEnvVar(original_env_vars, dotnet.coreclr_profiler_env_var_name);
    try testing.expect(value_optional == null);
    value_optional = modifyEnvVar(original_env_vars, dotnet.coreclr_profiler_path_env_var_name);
    try testing.expect(value_optional == null);
    value_optional = modifyEnvVar(original_env_vars, dotnet.dotnet_additional_deps_env_var_name);
    try testing.expect(value_optional == null);
    value_optional = modifyEnvVar(original_env_vars, dotnet.dotnet_shared_store_env_var_name);
    try testing.expect(value_optional == null);
    value_optional = modifyEnvVar(original_env_vars, dotnet.dotnet_startup_hooks_env_var_name);
    try testing.expect(value_optional == null);
    value_optional = modifyEnvVar(original_env_vars, dotnet.otel_auto_home_env_var_name);
    try testing.expect(value_optional == null);

    try testing.expect(cache.injector_cache.dotnet_env_var_updates.done);
    try testing.expect(cache.injector_cache.dotnet_env_var_updates.values == null);
}

test "modifyEnvVar: use cached .NET values" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    cache.injector_cache.dotnet_env_var_updates = cache.CachedDotnetEnvVarUpdates{
        .done = true,
        .values = types.DotnetEnvVarUpdates{
            .coreclr_enable_profiling = types.EnvVarUpdate{
                .value = "cached CORECLR_ENABLE_PROFILING value",
                .replace = false,
                .index = 0,
            },
            .coreclr_profiler = types.EnvVarUpdate{
                .value = "cached CORECLR_PROFILER value",
                .replace = false,
                .index = 0,
            },
            .coreclr_profiler_path = types.EnvVarUpdate{
                .value = "cached CORECLR_PROFILER_PATH value",
                .replace = false,
                .index = 0,
            },
            .dotnet_additional_deps = types.EnvVarUpdate{
                .value = "cached DOTNET_ADDITIONAL_DEPS value",
                .replace = false,
                .index = 0,
            },
            .dotnet_shared_store = types.EnvVarUpdate{
                .value = "cached DOTNET_SHARED_STORE value",
                .replace = false,
                .index = 0,
            },
            .dotnet_startup_hooks = types.EnvVarUpdate{
                .value = "cached DOTNET_STARTUP_HOOKS value",
                .replace = false,
                .index = 0,
            },
            .otel_auto_home = types.EnvVarUpdate{
                .value = "cached OTEL_DOTNET_AUTO_HOME value",
                .replace = false,
                .index = 0,
            },
        },
    };

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_EXPERIMENTAL_DOTNET_INJECTION=true";

    var value_optional = modifyEnvVar(original_env_vars, dotnet.coreclr_enable_profiling_env_var_name);
    try testing.expectEqualStrings("cached CORECLR_ENABLE_PROFILING value", std.mem.span(value_optional.?));
    value_optional = modifyEnvVar(original_env_vars, dotnet.coreclr_profiler_env_var_name);
    try testing.expectEqualStrings("cached CORECLR_PROFILER value", std.mem.span(value_optional.?));
    value_optional = modifyEnvVar(original_env_vars, dotnet.coreclr_profiler_path_env_var_name);
    try testing.expectEqualStrings("cached CORECLR_PROFILER_PATH value", std.mem.span(value_optional.?));
    value_optional = modifyEnvVar(original_env_vars, dotnet.dotnet_additional_deps_env_var_name);
    try testing.expectEqualStrings("cached DOTNET_ADDITIONAL_DEPS value", std.mem.span(value_optional.?));
    value_optional = modifyEnvVar(original_env_vars, dotnet.dotnet_shared_store_env_var_name);
    try testing.expectEqualStrings("cached DOTNET_SHARED_STORE value", std.mem.span(value_optional.?));
    value_optional = modifyEnvVar(original_env_vars, dotnet.dotnet_startup_hooks_env_var_name);
    try testing.expectEqualStrings("cached DOTNET_STARTUP_HOOKS value", std.mem.span(value_optional.?));
    value_optional = modifyEnvVar(original_env_vars, dotnet.otel_auto_home_env_var_name);
    try testing.expectEqualStrings("cached OTEL_DOTNET_AUTO_HOME value", std.mem.span(value_optional.?));

    try testing.expect(cache.injector_cache.dotnet_env_var_updates.done);
    try testing.expectEqualStrings("cached CORECLR_ENABLE_PROFILING value", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.coreclr_enable_profiling.value));
    try testing.expectEqualStrings("cached CORECLR_PROFILER value", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.coreclr_profiler.value));
    try testing.expectEqualStrings("cached CORECLR_PROFILER_PATH value", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.coreclr_profiler_path.value));
    try testing.expectEqualStrings("cached DOTNET_ADDITIONAL_DEPS value", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.dotnet_additional_deps.value));
    try testing.expectEqualStrings("cached DOTNET_SHARED_STORE value", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.dotnet_shared_store.value));
    try testing.expectEqualStrings("cached DOTNET_STARTUP_HOOKS value", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.dotnet_startup_hooks.value));
    try testing.expectEqualStrings("cached OTEL_DOTNET_AUTO_HOME value", std.mem.span(cache.injector_cache.dotnet_env_var_updates.values.?.otel_auto_home.value));
}
