// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const alloc = @import("allocator.zig");
const print = @import("print.zig");
const types = @import("types.zig");

pub const modification_happened_msg = "adding additional OpenTelemetry resources attributes via {s}";

/// A type for a rule to map an environment variable to a resource attribute. The result of applying these rules (via
/// getModifiedOtelResourceAttributesValue) is a string of key-value pairs, where each pair is of the form key=value,
/// and pairs are separated by commas. If resource_attributes_key is not null, we append a key-value pair
/// (that is, ${resource_attributes_key}=${value of environment variable}). If resource_attributes_key is null, the
/// value of the enivronment variable is expected to already be a key-value pair (or a comma separated list of key-value
/// pairs), and the value of the enivronment variable is appended as is.
const EnvToResourceAttributeMapping = struct {
    environement_variable_name: []const u8,
    resource_attributes_key: []const u8,
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
    var result_list = try std.ArrayList(u8).initCapacity(alloc.page_allocator, std.heap.pageSize());
    const result_writer = result_list.writer();

    // Key-value pairs from the original environment variable value have the highest priority. We write them first, and
    // then later, when processing DASH0_RESOURCE_ATTRIBUTES or any of the mappings, we check for each key if we have
    // already written it earlier. This avoids duplicate keys and resolves conflicts between the original
    // OTEL_RESOURCE_ATTRIBUTES value and our modifications in favor of the original OTEL_RESOURCE_ATTRIBUTES value.
    if (maybe_original_value) |original_value| {
        if (std.mem.trim(u8, original_value, " \t\r\n").len > 0) {
            try result_writer.writeAll(original_value);
        }
    }

    var has_modified_value = false;

    // DASH0_RESOURCE_ATTRIBUTES should have priority over the built-in mappings, so we process
    // DASH0_RESOURCE_ATTRIBUTES first. Then later, when iterating over `mappings`, we will check whether a key has
    // already been written (and if so, skip it in the mappings-loop).
    if (std.posix.getenv(dash0_resource_attributes_env_name)) |dash0_resource_attributes_value| {
        // Split the value of DASH0_RESOURCE_ATTRIBUTES into individual key-value pairs.
        var all_key_value_pairs_from_dash0_resource_attributes = std.mem.splitAny(u8, dash0_resource_attributes_value, ",");
        while (true) {
            // Terminate the while (true) loop if the SplitIterator resource_attributes returns null.
            const key_value_pair = all_key_value_pairs_from_dash0_resource_attributes.next() orelse break;

            // Split the key-value pair into key and value.
            var key_value_pair_split = std.mem.splitAny(u8, key_value_pair, "=");
            const key = std.mem.trim(u8, key_value_pair_split.first(), " \t\r\n");
            if (key.len == 0) {
                // Skip empty keys (e.g. if there is something like ",," in  DASH0_RESOURCE_ATTRIBUTES)
                continue;
            }
            // deliberately not trimming the value, strictly speaking leading or trailing whitespace could be part of
            // the value
            const value = key_value_pair_split.rest();

            // Check if we have written this particular key already, either when copying over the original value of
            // OTEL_RESOURCE_ATTRIBUTES, or when processing the key-value pairs from DASH0_RESOURCE_ATTRIBUTES:
            var value_already_written: ?[]const u8 = null;
            var all_key_value_pairs_already_written = std.mem.splitAny(u8, result_list.items, ",");
            while (true) {
                // Terminate the inner while (true) loop for the already-written check if the SplitIterator
                // already_set_attributes returns null.
                const key_value_pair_already_written: []const u8 = all_key_value_pairs_already_written.next() orelse break;
                var key_value_pair_already_written_split = std.mem.splitAny(u8, key_value_pair_already_written, "=");
                const key_already_written = key_value_pair_already_written_split.first();

                if (std.mem.eql(u8, key_already_written, key)) {
                    // We have written this key already. Set value_already_written so we can log this and skip this
                    // key-value pair.
                    value_already_written = key_value_pair_already_written_split.rest();
                    break;
                }
            }

            if (value_already_written) |val| {
                print.printDebug("OTEL_RESOURCE_ATTRIBUTES already sets '{s}={s}'; skipping appending '{s}={s}' from DASH0_RESOURCE_ATTRIBUTES", .{ key, val, key, value });
                continue;
            }

            // We have not written this key before, let's write the key-value pair now:
            if (result_list.items.len > 0) {
                // write leading comma if we have already written key-value pairs
                try result_writer.writeAll(",");
            }

            // write key-value pair
            try result_writer.writeAll(key);
            try result_writer.writeAll("=");
            try result_writer.writeAll(value);

            has_modified_value = true;
        }
    }

    // Now iterate over the mappings from DASH0_* environment variables to resource attributes:
    for (mappings) |mapping| {
        const key = mapping.resource_attributes_key;

        // If no value is set in the process environment, i.e. there we are processing the mapping from DASH0_POD_NAME
        // to k8s.pod.name but DASH0_POD_NAME is not set, we skip this mapping.
        const value = std.posix.getenv(mapping.environement_variable_name) orelse continue;

        // Check if the original value contains the key already:
        var value_already_written: ?[]const u8 = null;
        var all_key_value_pairs_already_written = std.mem.splitAny(u8, result_list.items, ",");
        while (true) {
            // Terminate the inner while (true) loop for the already-written check if the SplitIterator
            // already_set_attributes returns null.
            const key_value_pair_already_written: []const u8 = all_key_value_pairs_already_written.next() orelse break;
            var key_value_pair_already_written_split = std.mem.splitAny(u8, key_value_pair_already_written, "=");
            const key_already_written = key_value_pair_already_written_split.first();

            if (std.mem.eql(u8, key_already_written, key)) {
                // We have written this key already. Set value_already_written so we can log this and skip this
                // key-value pair.
                value_already_written = key_value_pair_already_written_split.rest();
                break;
            }
        }
        if (value_already_written) |val| {
            print.printDebug("OTEL_RESOURCE_ATTRIBUTES already sets '{s}={s}'; skipping appending '{s}={s}' from {s}", .{ key, val, key, value, mapping.environement_variable_name });
            continue;
        }

        if (result_list.items.len > 0) {
            try result_writer.writeAll(",");
        }

        try result_writer.writeAll(key);
        try result_writer.writeAll("=");
        try result_writer.writeAll(value);

        has_modified_value = true;
    }

    if (!has_modified_value) {
        result_list.deinit();
        return null;
    }

    try result_writer.writeAll("\x00");

    const result_owned = try result_list.toOwnedSlice();
    const result_as_null_terminated_string: types.NullTerminatedString = @ptrCast(result_owned.ptr);

    return result_as_null_terminated_string;
}
