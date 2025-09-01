// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const alloc = @import("allocator.zig");
const print = @import("print.zig");
const types = @import("types.zig");

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

pub const otel_resource_attributes_env_var_name = "OTEL_RESOURCE_ATTRIBUTES";

const dash0_resource_attributes_env_name = "DASH0_RESOURCE_ATTRIBUTES";

/// A list of mappings from environment variables to resource attributes.
const mappings: [7]EnvToResourceAttributeMapping =
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
    };

/// Derive the modified value for OTEL_RESOURCE_ATTRIBUTES based on the original value, and on other resource attributes
/// provided via the DASH0_* environment variables set by the operator (workload_modifier#addEnvironmentVariables).
pub fn getModifiedOtelResourceAttributesValue(maybe_original_value: ?[:0]const u8) !?types.NullTerminatedString {
    var res = try std.ArrayList(u8).initCapacity(alloc.page_allocator, std.heap.pageSize());
    const writer = res.writer();

    if (maybe_original_value) |original_value| {
        try writer.writeAll(original_value);
    }

    var has_modified_value = false;

    // DASH0_RESOURCE_ATTRIBUTES should not be overwritten by built-in mappings, so we process it first
    if (std.posix.getenv(dash0_resource_attributes_env_name)) |dash0_resource_attributes_value| {
        var resource_attributes = std.mem.splitAny(u8, dash0_resource_attributes_value, ",");
        while (true) {
            const resource_attribute = resource_attributes.next() orelse break;

            var resource_attribute_split = std.mem.splitAny(u8, resource_attribute, "=");
            const resource_attribute_key = resource_attribute_split.first();
            const resource_attribute_value = resource_attribute_split.rest();

            var already_set_attribute_value: ?[]const u8 = null;
            var already_set_attributes = std.mem.splitAny(u8, res.items, ",");

            while (true) {
                // If next() returns null, we are done
                const already_set_attribute: []const u8 = already_set_attributes.next() orelse break;

                var already_set_attribute_split = std.mem.splitAny(u8, already_set_attribute, "=");
                const already_set_attribute_key = already_set_attribute_split.first();

                if (std.mem.eql(u8, already_set_attribute_key, resource_attribute_key)) {
                    already_set_attribute_value = already_set_attribute_split.rest();
                    break;
                }
            }

            if (already_set_attribute_value) |value| {
                print.printDebug("OTEL_RESOURCE_ATTRIBUTES already sets '{s}={s}'; skipping appending '{s}={s}' from DASH0_RESOURCE_ATTRIBUTES", .{ resource_attribute_key, value, resource_attribute_key, resource_attribute_value });
                continue;
            }

            if (res.items.len > 0) {
                try writer.writeAll(",");
            }

            try writer.writeAll(resource_attribute_key);
            try writer.writeAll("=");
            try writer.writeAll(resource_attribute_value);

            has_modified_value = true;
        }
    }

    for (mappings) |mapping| {
        const resource_attribute_key = mapping.resource_attributes_key orelse continue;

        // If no value is set in the process environment for the current mapping, skip
        const value_to_be_set = std.posix.getenv(mapping.environement_variable_name) orelse continue;

        var already_set_attribute_value: ?[]const u8 = null;
        var already_set_attributes = std.mem.splitAny(u8, res.items, ",");

        while (true) {
            // If next() returns null, we are done
            const next_attribute: []const u8 = already_set_attributes.next() orelse break;

            var split = std.mem.splitAny(u8, next_attribute, "=");
            const next_attribute_key = split.first();

            if (std.mem.eql(u8, next_attribute_key, resource_attribute_key)) {
                already_set_attribute_value = split.rest();
                break;
            }
        }

        if (already_set_attribute_value) |value| {
            print.printDebug("OTEL_RESOURCE_ATTRIBUTES already sets '{s}={s}'; skipping appending '{s}={s}' from {s}", .{ resource_attribute_key, value, resource_attribute_key, value_to_be_set, mapping.environement_variable_name });
            continue;
        }

        if (res.items.len > 0) {
            try writer.writeAll(",");
        }

        try writer.writeAll(resource_attribute_key);
        try writer.writeAll("=");
        try writer.writeAll(value_to_be_set);

        has_modified_value = true;
    }

    if (!has_modified_value) {
        res.deinit();
        return null;
    }

    try writer.writeAll("\x00");

    const owned = try res.toOwnedSlice();
    const result: types.NullTerminatedString = @ptrCast(owned.ptr);

    return result;
}
