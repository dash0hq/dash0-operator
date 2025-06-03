// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const dotnet = @import("dotnet.zig");
const jvm = @import("jvm.zig");
const node_js = @import("node_js.zig");
const print = @import("print.zig");
const types = @import("types.zig");

const assert = std.debug.assert;
const expect = std.testing.expect;

var environ_buffer: []types.NullTerminatedString = &.{};
const empty_env_var: [*:0]const u8 = "\x00";

export var __environ: [*c]const [*c]const u8 = @as([1][*c]const u8, .{null})[0..].ptr;
export var _environ: [*c]const [*c]const u8 = @as([1][*c]const u8, .{null})[0..].ptr;
export var environ: [*c]const [*c]const u8 = @as([1][*c]const u8, .{null})[0..].ptr;

// Ensure we process requests synchronously. LibC is *not* threadsafe
// with respect to the environment, but chances are some apps will try
// to look up env vars in parallel
const _env_mutex = std.Thread.Mutex{};

// Keep global pointers to already-calculated values to avoid multiple allocations
// on repeated lookups.
var modified_java_tool_options_value_calculated = false;
var modified_java_tool_options_value: ?types.NullTerminatedString = null;
var modified_node_options_value_calculated = false;
var modified_node_options_value: ?types.NullTerminatedString = null;
var modified_otel_resource_attributes_value_calculated = false;
var modified_otel_resource_attributes_value: ?types.NullTerminatedString = null;

// This is how you can currently do something like `__attribute__((constructor))` in Zig, that is, register a function
// that runs before main(): Export a const named init_array which lists the functions you want to run. This will then
// be added to the .init_array ELF section of the binary. (See SHT_INIT_ARRAY in
// https://refspecs.linuxfoundation.org/LSB_5.0.0/LSB-Core-generic/LSB-Core-generic/sections.html.)
//
// Note: Destructor, i.e. things that run after main() are registered similarly via `fini_array`, but they do not run
// on panic or unhandled signals (so not chance for getting a cheap abnormal process detection mechanism here).
//
// There is discussion to provide a more explicit mechanism for init functions in the future, but not much traction so
// far. See https://github.com/ziglang/zig/issues/23574 and https://github.com/ziglang/zig/issues/20382.
export const init_array: [1]*const fn () callconv(.C) void linksection(".init_array") = .{&initEnviron};

const otel_resource_attributes_env_var_name: []const u8 = "OTEL_RESOURCE_ATTRIBUTES";
const empty_otel_resource_attributes_env_var: [*:0]const u8 = otel_resource_attributes_env_var_name ++ "=\x00";

fn initEnviron() callconv(.C) void {
    _initEnviron() catch @panic("[Dash0 injector] initEnviron failed");
}

// This function MUST be idempotent, as parent processes may pass the environment to child processes
fn _initEnviron() !void {
    const proc_self_environ_path = "/proc/self/environ";
    const proc_self_environ_file = try std.fs.openFileAbsolute(proc_self_environ_path, .{
        .mode = std.fs.File.OpenMode.read_only,
        .lock = std.fs.File.Lock.none,
    });
    defer proc_self_environ_file.close();

    // IMPORTANT! /proc/selv/environ skips the final \x00 terminator
    // TODO Fix max size
    const environ_buffer_original = try proc_self_environ_file.readToEndAlloc(std.heap.page_allocator, std.math.maxInt(usize));

    var env_vars = std.ArrayList(types.NullTerminatedString).init(std.heap.page_allocator);
    defer env_vars.deinit();

    var index: usize = 0;
    var otel_resource_attributes_env_var_found = false;
    var otel_resource_attributes_env_var_index: usize = 0;

    if (environ_buffer_original.len > 0) {
        for (environ_buffer_original, 0..) |c, i| {
            if (c == 0) {
                // We have a null terminator, so we need to create a slice from the start of the string to the null terminator
                // and append it to the result, provided the string is not empty.
                if (i > index) {
                    const env_var: [*:0]const u8 = environ_buffer_original[index..i :0];

                    if (std.mem.indexOf(c_char, environ_buffer_original, "OTEL_RESOURCE_ATTRIBUTES=")) |j| {
                        if (j == 0) {
                            otel_resource_attributes_env_var_found = true;
                            otel_resource_attributes_env_var_index = j;
                        }
                    }

                    try env_vars.append(env_var);
                } else {
                    break; // Empty string, we can stop processing
                }
                index = i + 1;
            }
        }
    }

    // Ensure there is the OTEL_RESOURCE_ATTRIBUTES env var, even if empty
    if (!otel_resource_attributes_env_var_found) {
        try env_vars.append(empty_otel_resource_attributes_env_var[0..]);
        otel_resource_attributes_env_var_index = env_vars.items.len - 1;
    }

    try env_vars.append(empty_env_var);

    const env_var_slices = try env_vars.toOwnedSlice();
    const env_var_count = env_var_slices.len;

    environ_buffer = try std.heap.page_allocator.alloc(types.NullTerminatedString, env_var_count);

    for (env_var_slices, 0..) |env_var, i| {
        // We copy the env var slice to the environ_buffer, so that we can modify it later.
        // Note: We do not need to copy the final null terminator, as it is already there.
        environ_buffer[i] = env_var;
    }

    if (getModifiedOtelResourceAttributesValue()) |resource_attributes| {
        environ_buffer[otel_resource_attributes_env_var_index] = resource_attributes;
    }

    __environ = @ptrCast(environ_buffer);
    _environ = __environ;
    environ = __environ;

    std.debug.print("[Dash0 injector] Initialized environment\n", .{});
}

pub fn getEnvVar(name: []const u8) ?types.NullTerminatedString {
    for (environ_buffer[0..]) |env_var| {
        const env_var_slice: []const u8 = std.mem.span(env_var);
        if (std.mem.indexOf(u8, env_var_slice, "=")) |j| {
            if (std.mem.eql(u8, name, env_var[0..j])) {
                if (std.mem.len(env_var) == j + 1) {
                    return null;
                }

                return env_var[j + 1 ..];
            }
        }
    }

    return null;
}

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
pub fn getModifiedOtelResourceAttributesValue() ?types.NullTerminatedString {
    if (modified_otel_resource_attributes_value_calculated) {
        std.debug.print("OTEL_RESOURCE_ATTRIBUTES already modified\n", .{});
        // We have already calculated the value, so we can return it.
        return modified_otel_resource_attributes_value;
    }

    std.debug.print("OTEL_RESOURCE_ATTRIBUTES not modified yet\n", .{});

    const original_value_optional = getEnvVar("OTEL_RESOURCE_ATTRIBUTES");
    if (getResourceAttributes()) |resource_attributes| {
        defer std.heap.page_allocator.free(resource_attributes);

        if (original_value_optional) |original_value| {
            // Prepend our resource attributes to the already existing key-value pairs.
            // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
            // parent process.
            const return_buffer = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}={s},{s}", .{ otel_resource_attributes_env_var_name, resource_attributes, original_value }) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ otel_resource_attributes_env_var_name, err });
                return original_value;
            };

            std.debug.print("OTEL_RESOURCE_ATTRIBUTES updated to '{s}\n", .{return_buffer});

            modified_otel_resource_attributes_value = return_buffer.ptr;
            modified_otel_resource_attributes_value_calculated = true;

            return modified_otel_resource_attributes_value;
        } else {
            // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
            // parent process.
            const return_buffer = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}={s}", .{ otel_resource_attributes_env_var_name, resource_attributes }) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ otel_resource_attributes_env_var_name, err });
                return null;
            };

            modified_otel_resource_attributes_value = return_buffer.ptr;
            modified_otel_resource_attributes_value_calculated = true;

            return modified_otel_resource_attributes_value;
        }
    } else {
        // No resource attributes to add. Return a pointer to the current value, or null if there is no current value.
        if (original_value_optional) |original_value| {
            // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
            // parent process.
            const return_buffer = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}={s}", .{ otel_resource_attributes_env_var_name, original_value }) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ otel_resource_attributes_env_var_name, err });
                return original_value;
            };

            modified_otel_resource_attributes_value = return_buffer.ptr;
            modified_otel_resource_attributes_value_calculated = true;

            return modified_otel_resource_attributes_value;
        } else {
            // There is no original value, and also nothing to add, return null.
            modified_otel_resource_attributes_value_calculated = true;
            return null;
        }
    }
}

/// Maps the DASH0_* environment variables that are set by the operator (workload_modifier#addEnvironmentVariables) to a
/// string that can be used for the value of OTEL_RESOURCE_ATTRIBUTES.
///
/// Important: The caller must free the returned []u8 array, if a non-null value is returned.
fn getResourceAttributes() ?[]u8 {
    var final_len: usize = 0;

    for (mappings) |mapping| {
        if (getEnvVar(mapping.environement_variable_name)) |value| {
            if (std.mem.len(value) > 0) {
                if (final_len > 0) {
                    final_len += 1; // ","
                }

                if (mapping.resource_attributes_key) |attribute_key| {
                    final_len += std.fmt.count("{s}={s}", .{ attribute_key, value });
                } else {
                    final_len += std.mem.len(value);
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
        const env_var_name = mapping.environement_variable_name;
        if (getEnvVar(env_var_name)) |value| {
            if (std.mem.len(value) > 0) {
                if (is_first_token) {
                    is_first_token = false;
                } else {
                    std.fmt.format(fbs.writer(), ",", .{}) catch |err| {
                        print.printError("Cannot append ',' delimiter to resource attributes: {}", .{err});
                        return null;
                    };
                }

                if (mapping.resource_attributes_key) |attribute_key| {
                    std.fmt.format(fbs.writer(), "{s}={s}", .{ attribute_key, value }) catch |err| {
                        print.printError("Cannot append '{s}={s}' from env var '{s}' to resource attributes: {}", .{ attribute_key, value, env_var_name, err });
                        return null;
                    };
                } else {
                    std.fmt.format(fbs.writer(), "{s}", .{value}) catch |err| {
                        print.printError("Cannot append '{s}' from env var '{s}' to resource attributes: {}", .{ value, env_var_name, err });
                        return null;
                    };
                }
            }
        }
    }

    return resource_attributes;
}
