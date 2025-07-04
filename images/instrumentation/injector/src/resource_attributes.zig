// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const env = @import("env.zig");
const cache = @import("cache.zig");
const print = @import("print.zig");
const test_util = @import("test_util.zig");
const types = @import("types.zig");

const testing = std.testing;

pub const otel_resource_attributes_env_var_name: []const u8 = "OTEL_RESOURCE_ATTRIBUTES";

pub const modification_happened_msg = "adding additional OpenTelemetry resources attributes via {s}";

/// A type for a rule to map an environment variable to a resource attribute. The result of applying these rules (via
/// getResourceAttributes) is a string of key-value pairs, where each pair is of the form key=value, and pairs are
/// separated by commas. If resource_attributes_key is not null, we append a key-value pair
/// (that is, ${resource_attributes_key}=${value of environment variable}). If resource_attributes_key is null, the
/// value of the enivronment variable is expected to already be a key-value pair (or a comma separated list of key-value
/// pairs), and the value of the enivronment variable is appended as is.
const EnvToResourceAttributeMapping = struct {
    environement_variable_name: []const u8,
    resource_attributes_key: ?[]const u8,
};

/// A list of mappings from environment variables to resource attributes.
const mappings: [8]EnvToResourceAttributeMapping =
    .{
        EnvToResourceAttributeMapping{
            .environement_variable_name = "DASH0_NAMESPACE_NAME",
            .resource_attributes_key = "k8s.namespace.name",
        },
        EnvToResourceAttributeMapping{
            .environement_variable_name = "DASH0_POD_NAME",
            .resource_attributes_key = "k8s.pod.name",
        },
        EnvToResourceAttributeMapping{
            .environement_variable_name = "DASH0_POD_UID",
            .resource_attributes_key = "k8s.pod.uid",
        },
        EnvToResourceAttributeMapping{
            .environement_variable_name = "DASH0_CONTAINER_NAME",
            .resource_attributes_key = "k8s.container.name",
        },
        EnvToResourceAttributeMapping{
            .environement_variable_name = "DASH0_SERVICE_NAME",
            .resource_attributes_key = "service.name",
        },
        EnvToResourceAttributeMapping{
            .environement_variable_name = "DASH0_SERVICE_VERSION",
            .resource_attributes_key = "service.version",
        },
        EnvToResourceAttributeMapping{
            .environement_variable_name = "DASH0_SERVICE_NAMESPACE",
            .resource_attributes_key = "service.namespace",
        },
        EnvToResourceAttributeMapping{
            .environement_variable_name = "DASH0_RESOURCE_ATTRIBUTES",
            .resource_attributes_key = null,
        },
    };

/// Derive the modified value for OTEL_RESOURCE_ATTRIBUTES based on the original value, and on other resource attributes
/// provided via the DASH0_* environment variables set by the operator (workload_modifier#addEnvironmentVariables).
pub fn getModifiedOtelResourceAttributesValue(env_vars: [](types.NullTerminatedString)) ?types.EnvVarUpdate {
    const original_value_and_index_optional = env.getEnvVar(env_vars, otel_resource_attributes_env_var_name);
    const resource_attributes_optional = getResourceAttributes(env_vars);
    if (original_value_and_index_optional) |original_value_and_index| {
        const original_value = original_value_and_index.value;
        const original_index = original_value_and_index.index;
        if (resource_attributes_optional) |resource_attributes| {
            defer std.heap.page_allocator.free(resource_attributes);
            if (std.mem.len(original_value) == 0) {
                // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
                // parent process.
                const return_buffer = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}", .{resource_attributes}) catch |err| {
                    print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ otel_resource_attributes_env_var_name, err });
                    return types.EnvVarUpdate{ .value = original_value, .replace = true, .index = original_index };
                };
                print.printMessage(modification_happened_msg, .{otel_resource_attributes_env_var_name});
                cache.injector_cache.otel_resource_attributes = cache.CachedEnvVarValue{ .value = return_buffer.ptr, .done = true };
                return types.EnvVarUpdate{ .value = return_buffer.ptr, .replace = true, .index = original_index };
            }

            // Prepend our resource attributes to the already existing key-value pairs.
            // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
            // parent process.
            const return_buffer = std.fmt.allocPrintZ(std.heap.page_allocator, "{s},{s}", .{ resource_attributes, original_value }) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ otel_resource_attributes_env_var_name, err });
                return types.EnvVarUpdate{ .value = original_value, .replace = true, .index = original_index };
            };
            print.printMessage(modification_happened_msg, .{otel_resource_attributes_env_var_name});
            cache.injector_cache.otel_resource_attributes = cache.CachedEnvVarValue{ .value = return_buffer.ptr, .done = true };
            return types.EnvVarUpdate{ .value = return_buffer.ptr, .replace = true, .index = original_index };
        } else {
            // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
            // parent process.
            const return_buffer = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}", .{original_value}) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ otel_resource_attributes_env_var_name, err });
                return types.EnvVarUpdate{ .value = original_value, .replace = true, .index = original_index };
            };
            cache.injector_cache.otel_resource_attributes = cache.CachedEnvVarValue{ .value = return_buffer.ptr, .done = true };
            return types.EnvVarUpdate{ .value = return_buffer.ptr, .replace = true, .index = original_index };
        }
    } else {
        if (resource_attributes_optional) |resource_attributes| {
            defer std.heap.page_allocator.free(resource_attributes);
            // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
            // instrumented process.
            const return_buffer = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}", .{resource_attributes}) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ otel_resource_attributes_env_var_name, err });
                return null;
            };
            print.printMessage(modification_happened_msg, .{otel_resource_attributes_env_var_name});
            cache.injector_cache.otel_resource_attributes = cache.CachedEnvVarValue{ .value = return_buffer.ptr, .done = true };
            return types.EnvVarUpdate{ .value = return_buffer.ptr, .replace = false, .index = 0 };
        } else {
            // There is no original value, and also nothing to add, return null.
            cache.injector_cache.otel_resource_attributes = cache.CachedEnvVarValue{ .value = null, .done = true };
            return null;
        }
    }
}

test "getModifiedOtelResourceAttributesValue: no original value, no new resource attributes" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 2);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "AAA=bbb";
    original_env_vars[1] = "CCC=ddd";
    const env_var_update_optional = getModifiedOtelResourceAttributesValue(original_env_vars);
    try test_util.expectWithMessage(env_var_update_optional == null, "env_var_update_optional == null");
    try testing.expectEqual(true, cache.injector_cache.otel_resource_attributes.done);
    try testing.expectEqual(null, cache.injector_cache.otel_resource_attributes.value);
}

test "getModifiedOtelResourceAttributesValue: original value is empty string, no new resource attributes" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 3);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "AAA=bbb";
    original_env_vars[1] = "OTEL_RESOURCE_ATTRIBUTES=";
    original_env_vars[2] = "CCC=ddd";
    const env_var_update = getModifiedOtelResourceAttributesValue(original_env_vars).?;
    try testing.expectEqual(0, env_var_update.value[0]);
    try testing.expectEqual(0, std.mem.len(env_var_update.value));
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(1, env_var_update.index);
    try testing.expectEqual(true, cache.injector_cache.otel_resource_attributes.done);
    try testing.expectEqualStrings("", std.mem.span(cache.injector_cache.otel_resource_attributes.value.?));
}

test "getModifiedOtelResourceAttributesValue: no original value, only new resource attributes" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 3);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_NAMESPACE_NAME=namespace";
    original_env_vars[1] = "DASH0_POD_NAME=pod";
    original_env_vars[2] = "DASH0_POD_UID=uid";
    const env_var_update = getModifiedOtelResourceAttributesValue(original_env_vars).?;
    try testing.expectEqualStrings("k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid", std.mem.span(env_var_update.value));
    try testing.expectEqual(false, env_var_update.replace);
    try testing.expectEqual(0, env_var_update.index);
    try testing.expectEqual(true, cache.injector_cache.otel_resource_attributes.done);
    try testing.expectEqualStrings("k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid", std.mem.span(cache.injector_cache.otel_resource_attributes.value.?));
}

test "getModifiedOtelResourceAttributesValue: original value is empty string, new resource attributes present" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 4);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "OTEL_RESOURCE_ATTRIBUTES=";
    original_env_vars[1] = "DASH0_NAMESPACE_NAME=namespace";
    original_env_vars[2] = "DASH0_POD_NAME=pod";
    original_env_vars[3] = "DASH0_POD_UID=uid";
    const env_var_update = getModifiedOtelResourceAttributesValue(original_env_vars).?;
    try testing.expectEqualStrings("k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid", std.mem.span(env_var_update.value));
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(0, env_var_update.index);
    try testing.expectEqual(true, cache.injector_cache.otel_resource_attributes.done);
    try testing.expectEqualStrings("k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid", std.mem.span(cache.injector_cache.otel_resource_attributes.value.?));
}

test "getModifiedOtelResourceAttributesValue: original value exists, no new resource attributes" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 3);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "AAA=bbb";
    original_env_vars[1] = "CCC=ddd";
    original_env_vars[2] = "OTEL_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd";
    const env_var_update = getModifiedOtelResourceAttributesValue(original_env_vars).?;
    try testing.expectEqualStrings("aaa=bbb,ccc=ddd", std.mem.span(env_var_update.value));
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(2, env_var_update.index);
    try testing.expectEqual(true, cache.injector_cache.otel_resource_attributes.done);
    try testing.expectEqualStrings("aaa=bbb,ccc=ddd", std.mem.span(cache.injector_cache.otel_resource_attributes.value.?));
}

test "getModifiedOtelResourceAttributesValue: original value and new resource attributes" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 4);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_NAMESPACE_NAME=namespace";
    original_env_vars[1] = "DASH0_POD_NAME=pod";
    original_env_vars[2] = "OTEL_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd";
    original_env_vars[3] = "DASH0_POD_UID=uid";
    const env_var_update = getModifiedOtelResourceAttributesValue(original_env_vars).?;
    try testing.expectEqualStrings("k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,aaa=bbb,ccc=ddd", std.mem.span(env_var_update.value));
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(2, env_var_update.index);
    try testing.expectEqual(true, cache.injector_cache.otel_resource_attributes.done);
    try testing.expectEqualStrings("k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,aaa=bbb,ccc=ddd", std.mem.span(cache.injector_cache.otel_resource_attributes.value.?));
}

/// Maps the DASH0_* environment variables that are set by the operator (workload_modifier#addEnvironmentVariables) to a
/// string that can be used for the value of OTEL_RESOURCE_ATTRIBUTES or -Dotel.resource.attributes (for adding to
/// JAVA_TOOL_OPTIONS for JVMs).
///
/// Important: The caller must free the returned []u8 array, if a non-null value is returned.
pub fn getResourceAttributes(env_vars: [](types.NullTerminatedString)) ?[]u8 {
    var final_len: usize = 0;
    for (mappings) |mapping| {
        const dash0_source_env_var_value_and_index_optional =
            env.getEnvVar(
                env_vars,
                mapping.environement_variable_name,
            );
        if (dash0_source_env_var_value_and_index_optional) |dash0_source_env_var_value_and_index| {
            const dash0_source_env_var_value = dash0_source_env_var_value_and_index.value;
            if (std.mem.len(dash0_source_env_var_value) > 0) {
                if (final_len > 0) {
                    final_len += 1; // ","
                }

                if (mapping.resource_attributes_key) |attribute_key| {
                    final_len += std.fmt.count("{s}={s}", .{ attribute_key, dash0_source_env_var_value });
                } else {
                    final_len += std.mem.len(dash0_source_env_var_value);
                }
            }
        }
    }

    if (final_len < 1) {
        return null;
    }

    const resource_attributes = std.heap.page_allocator.alloc(u8, final_len) catch |err| {
        print.printError("Cannot allocate memory to prepare the resource attributes (len: {d}): {}", .{ final_len, err });
        return null;
    };

    var fbs = std.io.fixedBufferStream(resource_attributes);

    var is_first_token = true;
    for (mappings) |mapping| {
        const dash0_source_env_var_name = mapping.environement_variable_name;
        const dash0_source_env_var_value_and_index_optional = env.getEnvVar(env_vars, dash0_source_env_var_name);
        if (dash0_source_env_var_value_and_index_optional) |dash0_source_env_var_value_and_index| {
            const dash0_source_env_var_value = dash0_source_env_var_value_and_index.value;
            if (std.mem.len(dash0_source_env_var_value) > 0) {
                if (is_first_token) {
                    is_first_token = false;
                } else {
                    std.fmt.format(fbs.writer(), ",", .{}) catch |err| {
                        print.printError("Cannot append ',' delimiter to resource attributes: {}", .{err});
                        return null;
                    };
                }

                if (mapping.resource_attributes_key) |attribute_key| {
                    std.fmt.format(fbs.writer(), "{s}={s}", .{ attribute_key, dash0_source_env_var_value }) catch |err| {
                        print.printError("Cannot append '{s}={s}' from env var '{s}' to resource attributes: {}", .{ attribute_key, dash0_source_env_var_value, dash0_source_env_var_name, err });
                        return null;
                    };
                } else {
                    std.fmt.format(fbs.writer(), "{s}", .{dash0_source_env_var_value}) catch |err| {
                        print.printError("Cannot append '{s}' from env var '{s}' to resource attributes: {}", .{ dash0_source_env_var_value, dash0_source_env_var_name, err });
                        return null;
                    };
                }
            }
        }
    }

    return resource_attributes;
}

test "getResourceAttributes: empty environment" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(original_env_vars);
    try test_util.expectWithMessage(getResourceAttributes(original_env_vars) == null, "getResourceAttributes(original_env_vars) == null");
}

test "getResourceAttributes: Kubernetes namespace only" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_NAMESPACE_NAME=namespace";
    const resource_attributes = getResourceAttributes(original_env_vars).?;
    defer std.heap.page_allocator.free(resource_attributes);
    try testing.expectEqualStrings("k8s.namespace.name=namespace", resource_attributes);
}

test "getResourceAttributes: Kubernetes pod name only" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_POD_NAME=pod";
    const resource_attributes = getResourceAttributes(original_env_vars).?;
    defer std.heap.page_allocator.free(resource_attributes);
    try testing.expectEqualStrings("k8s.pod.name=pod", resource_attributes);
}

test "getResourceAttributes: Kubernetes pod uid only" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_POD_UID=uid";
    const resource_attributes = getResourceAttributes(original_env_vars).?;
    defer std.heap.page_allocator.free(resource_attributes);
    try testing.expectEqualStrings("k8s.pod.uid=uid", resource_attributes);
}

test "getResourceAttributes: container name only" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_CONTAINER_NAME=container";
    const resource_attributes = getResourceAttributes(original_env_vars).?;
    defer std.heap.page_allocator.free(resource_attributes);
    try testing.expectEqualStrings("k8s.container.name=container", resource_attributes);
}

test "getResourceAttributes: service name only" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_SERVICE_NAME=service";
    const resource_attributes = getResourceAttributes(original_env_vars).?;
    defer std.heap.page_allocator.free(resource_attributes);
    try testing.expectEqualStrings("service.name=service", resource_attributes);
}

test "getResourceAttributes: service version only" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_SERVICE_VERSION=version";
    const resource_attributes = getResourceAttributes(original_env_vars).?;
    defer std.heap.page_allocator.free(resource_attributes);
    try testing.expectEqualStrings("service.version=version", resource_attributes);
}

test "getResourceAttributes: service namespaces only" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_SERVICE_NAMESPACE=servicenamespace";
    // original_env_vars[0] = "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd";
    const resource_attributes = getResourceAttributes(original_env_vars).?;
    defer std.heap.page_allocator.free(resource_attributes);
    try testing.expectEqualStrings("service.namespace=servicenamespace", resource_attributes);
}

test "getResourceAttributes: free-form resource attributes only" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 1);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd";
    const resource_attributes = getResourceAttributes(original_env_vars).?;
    defer std.heap.page_allocator.free(resource_attributes);
    try testing.expectEqualStrings("aaa=bbb,ccc=ddd", resource_attributes);
}

test "getResourceAttributes: everything is set" {
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
    const resource_attributes = getResourceAttributes(original_env_vars).?;
    defer std.heap.page_allocator.free(resource_attributes);
    try testing.expectEqualStrings(
        "k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd",
        resource_attributes,
    );
}
