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
const testing = std.testing;

// Keep global pointers to already-calculated values to avoid multiple allocations
// on repeated lookups.
// TODO make this into a cached struct with a reset function which we can use in tests
var modified_java_tool_options_value_calculated = false;
var modified_java_tool_options_value: ?types.NullTerminatedString = null;
var modified_node_options_value_calculated = false;
var modified_node_options_value: ?types.NullTerminatedString = null;
var modified_otel_resource_attributes_value_calculated = false;
var modified_otel_resource_attributes_value: ?types.NullTerminatedString = null;

const otel_resource_attributes_env_var_name: []const u8 = "OTEL_RESOURCE_ATTRIBUTES";

// VERBOSE=true SUPPRESS_SKIPPED=true RUNTIMES=c,jvm TEST_CASES=otel-resource-attributes-unset,existing-env-var-return-unmodified ./watch-tests-within-container.sh

// TODO
// ====
// - fix unit tests
// - add unit tests for _initEnviron.
// - move otel resource attributes stuff back to resource_attributes.zig
// - get __DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS going, add tests for child process
// - revisit __DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS vs idempotency (maybe later)
// - remove all std.debug.print calls
// - move OTEL_RESOURCE_ATTRIBUTES back to resource_attributes.zig
// - enable all other env var modifications again (NODE_OPTIONS, JAVA_TOOL_OPTIONS, etc.)
// - clean up JAVA_TOOL_OPTIONS, we probably still need the -javaagent there, but not the otel resource attributes
// - add Python test for OTEL_RESOURCE_ATTRIBUTES
// - repair injector integration tests
// - enable NO_ENVIRON tests
// - add instrumentation and injector tests that also change the environment via setenv, putenv, and also directly
//   importing __environ, _environ, and environ and writing to that.
// - more extensive instrumentation tests for .NET, verifying OTEL_RESOURCE_ATTRIBUTES, and the various env vars that
//   activate tracing.

const EnvVarValueAndIndex = struct {
    /// The value of the environment variable. If the environment variable is not found, no EnvVarValueAndIndex should
    /// be returned at all, that is, functions that return EnvVarValueAndIndex always return an optional
    /// ?EnvVarValueAndIndex.
    value: types.NullTerminatedString,
    /// The index of the environment variable in the list of environment variables list.
    index: usize,
};

const EnvVarUpdate = struct {
    /// The value of the environment variable to set.
    value: types.NullTerminatedString,
    /// The index of the environment variable in the original environment variables list. If this is true, the required
    /// action is to replace the environment variable at that index with the new value. If this is false, the required
    /// action is to append the new value to the end of the environment variables list.
    replace: bool,
    /// The index of the environment variable in the original environment variables list. This value is only valid if
    /// replace is true, otherwise it must be ignored.
    index: usize,
};

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

// TODO This function must be idempotent, as parent processes may pass the environment to child processes, compounding
// our modification with each nested child process start. Or add a marker env var.
pub fn _initEnviron(proc_self_environ_path: []const u8) ![*c]const [*c]const u8 {
    const original_env_vars = try readProcSelfEnvironFile(proc_self_environ_path);
    const modified_env_vars = try applyModifications(original_env_vars);
    // TODO can we free the two slices?
    return try renderEnvVarsToExport(modified_env_vars);
}

fn readProcSelfEnvironFile(proc_self_environ_path: []const u8) ![](types.NullTerminatedString) {
    const proc_self_environ_file = std.fs.openFileAbsolute(proc_self_environ_path, .{
        .mode = std.fs.File.OpenMode.read_only,
        .lock = std.fs.File.Lock.none,
    }) catch |err| {
        print.printError("Cannot open file {s}: {}\n", .{ proc_self_environ_path, err });
        return err;
    };
    defer proc_self_environ_file.close();

    const environ_buffer_original = try proc_self_environ_file.readToEndAlloc(std.heap.page_allocator, std.math.maxInt(usize));
    // TODO shouldn't we defer std.heap.page_allocator.free(environ_buffer_original);

    return readProcSelfEnvironBuffer(environ_buffer_original);
}

test "readProcSelfEnvironFile: read empty /proc/self/environ file" {
    const filename = "unit_test_proc_self_environ";
    const proc_self_environ_file = try std.fs.cwd().createFile(filename, .{});
    defer {
        proc_self_environ_file.close();
        std.fs.cwd().deleteFile(filename) catch |err| {
            std.debug.print("Failed to delete file {s}: {}\n", .{ filename, err });
        };
    }
    const absolute_path = try std.fs.cwd().realpathAlloc(std.heap.page_allocator, filename);
    defer std.heap.page_allocator.free(absolute_path);

    const original_env_vars = try readProcSelfEnvironFile(absolute_path);

    try testing.expectEqual(0, original_env_vars.len);
}

test "readProcSelfEnvironFile: read environment variables" {
    const filename = "unit_test_proc_self_environ";
    const proc_self_environ_file = try std.fs.cwd().createFile(filename, .{});
    const content = "VAR1=value1\x00VAR2=value2\x00VAR3=value3\x00";
    _ = try proc_self_environ_file.write(content);
    defer {
        proc_self_environ_file.close();
        std.fs.cwd().deleteFile(filename) catch |err| {
            std.debug.print("Failed to delete file {s}: {}\n", .{ filename, err });
        };
    }
    const absolute_path = try std.fs.cwd().realpathAlloc(std.heap.page_allocator, filename);
    defer std.heap.page_allocator.free(absolute_path);

    const original_env_vars = try readProcSelfEnvironFile(absolute_path);

    try testing.expectEqual(3, original_env_vars.len);
    try testing.expect(std.mem.eql(u8, "VAR1=value1", std.mem.span(original_env_vars[0])));
    try testing.expect(std.mem.eql(u8, "VAR2=value2", std.mem.span(original_env_vars[1])));
    try testing.expect(std.mem.eql(u8, "VAR3=value3", std.mem.span(original_env_vars[2])));
}

// note: unit tests for readProcSelfEnvironFile segfault if this function is not inlined.
inline fn readProcSelfEnvironBuffer(environ_buffer_original: []const u8) ![](types.NullTerminatedString) {
    var env_vars = std.ArrayList(types.NullTerminatedString).init(std.heap.page_allocator);
    var index: usize = 0;
    if (environ_buffer_original.len > 0) {
        for (environ_buffer_original, 0..) |c, i| {
            if (c == 0) {
                // We have a null terminator, so we need to create a slice from the start of the string to the null terminator
                // and append it to the result, provided the string is not empty.
                if (i > index) {
                    const env_var: [*:0]const u8 = environ_buffer_original[index..i :0];
                    try env_vars.append(env_var);
                } else {
                    break; // Empty string, we can stop processing
                }
                index = i + 1;
            }
        }
    }

    return env_vars.toOwnedSlice();
}

test "readProcSelfEnvironBuffer: read environment variables" {
    const env_vars = try readProcSelfEnvironBuffer("VAR1=value1\x00VAR2=value2\x00VAR3=value3\x00");
    try testing.expectEqual(3, env_vars.len);
    try testing.expect(std.mem.eql(u8, "VAR1=value1", std.mem.span(env_vars[0])));
    try testing.expect(std.mem.eql(u8, "VAR2=value2", std.mem.span(env_vars[1])));
    try testing.expect(std.mem.eql(u8, "VAR3=value3", std.mem.span(env_vars[2])));
}

fn applyModifications(original_env_vars: [](types.NullTerminatedString)) ![](types.NullTerminatedString) {
    var number_of_env_vars_after_modifications: usize = original_env_vars.len;
    const otel_resource_attributes_update_optional = getModifiedOtelResourceAttributesValue(original_env_vars);
    if (otel_resource_attributes_update_optional) |otel_resource_attributes_update| {
        if (!otel_resource_attributes_update.replace) {
            number_of_env_vars_after_modifications += 1;
        }
    }

    const modified_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, number_of_env_vars_after_modifications);
    for (original_env_vars, 0..) |original_env_var, i| {
        modified_env_vars[i] = original_env_var;
    }
    if (otel_resource_attributes_update_optional) |otel_resource_attributes_update| {
        if (!otel_resource_attributes_update.replace) {
            modified_env_vars[original_env_vars.len] = otel_resource_attributes_update.value;
        } else {
            modified_env_vars[otel_resource_attributes_update.index] = otel_resource_attributes_update.value;
        }
    }

    return modified_env_vars;
}

test "applyModifications: no changes" {
    modified_java_tool_options_value_calculated = false;
    modified_java_tool_options_value = null;
    modified_node_options_value_calculated = false;
    modified_node_options_value = null;
    modified_otel_resource_attributes_value_calculated = false;
    modified_otel_resource_attributes_value = null;

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 3);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "VAR1=value1";
    original_env_vars[1] = "VAR2=value2";
    original_env_vars[2] = "VAR3=value3";

    const modified_environment = try applyModifications(original_env_vars);
    try testing.expectEqual(3, modified_environment.len);
    try testing.expect(std.mem.eql(u8, "VAR1=value1", std.mem.span(modified_environment[0])));
    try testing.expect(std.mem.eql(u8, "VAR2=value2", std.mem.span(modified_environment[1])));
    try testing.expect(std.mem.eql(u8, "VAR3=value3", std.mem.span(modified_environment[2])));
}

test "applyModifications: compose OTEL_RESOURCE_ATTRIBUTES, OTEL_RESOURCE_ATTRIBUTES not present, source env vars present, other env vars are present" {
    modified_java_tool_options_value_calculated = false;
    modified_java_tool_options_value = null;
    modified_node_options_value_calculated = false;
    modified_node_options_value = null;
    modified_otel_resource_attributes_value_calculated = false;
    modified_otel_resource_attributes_value = null;

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 17);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "VAR1=value1";
    original_env_vars[1] = "DASH0_NAMESPACE_NAME=namespace";
    original_env_vars[2] = "VAR2=value2";
    original_env_vars[3] = "DASH0_POD_NAME=pod";
    original_env_vars[4] = "VAR3=value3";
    original_env_vars[5] = "DASH0_POD_UID=uid";
    original_env_vars[6] = "VAR4=value4";
    original_env_vars[7] = "DASH0_CONTAINER_NAME=container";
    original_env_vars[8] = "VAR5=value5";
    original_env_vars[9] = "DASH0_SERVICE_NAME=service";
    original_env_vars[10] = "VAR6=value6";
    original_env_vars[11] = "DASH0_SERVICE_VERSION=version";
    original_env_vars[12] = "VAR7=value7";
    original_env_vars[13] = "DASH0_SERVICE_NAMESPACE=servicenamespace";
    original_env_vars[14] = "VAR8=value8";
    original_env_vars[15] = "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd";
    original_env_vars[16] = "VAR9=value9";

    const modified_env_vars = try applyModifications(original_env_vars);
    try testing.expectEqual(18, modified_env_vars.len);
    try testing.expect(std.mem.eql(u8, "VAR1=value1", std.mem.span(modified_env_vars[0])));
    try testing.expect(std.mem.eql(u8, "DASH0_NAMESPACE_NAME=namespace", std.mem.span(modified_env_vars[1])));
    try testing.expect(std.mem.eql(u8, "VAR2=value2", std.mem.span(modified_env_vars[2])));
    try testing.expect(std.mem.eql(u8, "DASH0_POD_NAME=pod", std.mem.span(modified_env_vars[3])));
    try testing.expect(std.mem.eql(u8, "VAR3=value3", std.mem.span(modified_env_vars[4])));
    try testing.expect(std.mem.eql(u8, "DASH0_POD_UID=uid", std.mem.span(modified_env_vars[5])));
    try testing.expect(std.mem.eql(u8, "VAR4=value4", std.mem.span(modified_env_vars[6])));
    try testing.expect(std.mem.eql(u8, "DASH0_CONTAINER_NAME=container", std.mem.span(modified_env_vars[7])));
    try testing.expect(std.mem.eql(u8, "VAR5=value5", std.mem.span(modified_env_vars[8])));
    try testing.expect(std.mem.eql(u8, "DASH0_SERVICE_NAME=service", std.mem.span(modified_env_vars[9])));
    try testing.expect(std.mem.eql(u8, "VAR6=value6", std.mem.span(modified_env_vars[10])));
    try testing.expect(std.mem.eql(u8, "DASH0_SERVICE_VERSION=version", std.mem.span(modified_env_vars[11])));
    try testing.expect(std.mem.eql(u8, "VAR7=value7", std.mem.span(modified_env_vars[12])));
    try testing.expect(std.mem.eql(u8, "DASH0_SERVICE_NAMESPACE=servicenamespace", std.mem.span(modified_env_vars[13])));
    try testing.expect(std.mem.eql(u8, "VAR8=value8", std.mem.span(modified_env_vars[14])));
    try testing.expect(std.mem.eql(u8, "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd", std.mem.span(modified_env_vars[15])));
    try testing.expect(std.mem.eql(u8, "VAR9=value9", std.mem.span(modified_env_vars[16])));
    try testing.expect(std.mem.eql(u8, "OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd", std.mem.span(modified_env_vars[17])));
}


test "applyModifications: compose OTEL_RESOURCE_ATTRIBUTES, OTEL_RESOURCE_ATTRIBUTES present, source env vars present" {
    modified_java_tool_options_value_calculated = false;
    modified_java_tool_options_value = null;
    modified_node_options_value_calculated = false;
    modified_node_options_value = null;
    modified_otel_resource_attributes_value_calculated = false;
    modified_otel_resource_attributes_value = null;

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

    const modified_env_vars = try applyModifications(original_env_vars);
    try testing.expectEqual(9, modified_env_vars.len);
    try testing.expect(std.mem.eql(u8, "DASH0_NAMESPACE_NAME=namespace", std.mem.span(modified_env_vars[0])));
    try testing.expect(std.mem.eql(u8, "DASH0_POD_NAME=pod", std.mem.span(modified_env_vars[1])));
    try testing.expect(std.mem.eql(u8, "DASH0_POD_UID=uid", std.mem.span(modified_env_vars[2])));
    try testing.expect(std.mem.eql(u8, "DASH0_CONTAINER_NAME=container", std.mem.span(modified_env_vars[3])));
    try testing.expect(std.mem.eql(u8, "OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd,key1=value1,key2=value2", std.mem.span(modified_env_vars[4])));
    try testing.expect(std.mem.eql(u8, "DASH0_SERVICE_NAME=service", std.mem.span(modified_env_vars[5])));
    try testing.expect(std.mem.eql(u8, "DASH0_SERVICE_VERSION=version", std.mem.span(modified_env_vars[6])));
    try testing.expect(std.mem.eql(u8, "DASH0_SERVICE_NAMESPACE=servicenamespace", std.mem.span(modified_env_vars[7])));
    try testing.expect(std.mem.eql(u8, "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd", std.mem.span(modified_env_vars[8])));
}

/// Derive the modified value for OTEL_RESOURCE_ATTRIBUTES based on the original value, and on other resource attributes
/// provided via the DASH0_* environment variables set by the operator (workload_modifier#addEnvironmentVariables).
pub fn getModifiedOtelResourceAttributesValue(env_vars: [](types.NullTerminatedString)) ?EnvVarUpdate {
    const original_value_optional = getEnvVar(env_vars, "OTEL_RESOURCE_ATTRIBUTES");
    const resource_attributes_optional = getResourceAttributes(env_vars);
    if (original_value_optional) |original_value_and_index| {
        const original_value = original_value_and_index.value;
        const original_index = original_value_and_index.index;
        if (resource_attributes_optional) |resource_attributes| {
            defer std.heap.page_allocator.free(resource_attributes);

            std.debug.print("getModifiedOtelResourceAttributesValue: original value: {s}\n", .{original_value});

            // Prepend our resource attributes to the already existing key-value pairs.
            // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
            // parent process.
            const return_buffer = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}={s},{s}", .{ otel_resource_attributes_env_var_name, resource_attributes, original_value }) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ otel_resource_attributes_env_var_name, err });
                return EnvVarUpdate{ .value = original_value, .replace = true, .index = original_index };
            };

            std.debug.print("OTEL_RESOURCE_ATTRIBUTES updated to '{s}\n", .{return_buffer});

            modified_otel_resource_attributes_value = return_buffer.ptr;
            modified_otel_resource_attributes_value_calculated = true;

            std.debug.print("getModifiedOtelResourceAttributesValue: both original value and new values to add are present, returning {s}\n", .{return_buffer.ptr});
            return EnvVarUpdate{ .value = return_buffer.ptr, .replace = true, .index = original_index };
        } else {
            std.debug.print("getModifiedOtelResourceAttributesValue: original value: {s}\n", .{original_value});

            // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
            // parent process.
            const return_buffer = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}={s}", .{ otel_resource_attributes_env_var_name, original_value }) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ otel_resource_attributes_env_var_name, err });
                return EnvVarUpdate{ .value = original_value, .replace = true, .index = original_index };
            };

            modified_otel_resource_attributes_value = return_buffer.ptr;
            modified_otel_resource_attributes_value_calculated = true;

            std.debug.print("getModifiedOtelResourceAttributesValue: original value, nothing to add, returning {s}\n", .{return_buffer.ptr});
            return EnvVarUpdate{ .value = return_buffer.ptr, .replace = true, .index = original_index };
        }
    } else {
        if (resource_attributes_optional) |resource_attributes| {
            defer std.heap.page_allocator.free(resource_attributes);

            // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
            // process.
            const return_buffer = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}={s}", .{ otel_resource_attributes_env_var_name, resource_attributes }) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ otel_resource_attributes_env_var_name, err });
                return null;
            };

            modified_otel_resource_attributes_value = return_buffer.ptr;
            modified_otel_resource_attributes_value_calculated = true;

            std.debug.print("getModifiedOtelResourceAttributesValue: no original value, but new values to add are present, returning {s}\n", .{return_buffer.ptr});
            return EnvVarUpdate{ .value = return_buffer.ptr, .replace = false, .index = 0 };
        } else {
            // There is no original value, and also nothing to add, return null.
            modified_otel_resource_attributes_value_calculated = true;
            std.debug.print("getModifiedOtelResourceAttributesValue: no original, nothing to add, returning null\n", .{});
            return null;
        }
    }
}

/// Maps the DASH0_* environment variables that are set by the operator (workload_modifier#addEnvironmentVariables) to a
/// string that can be used for the value of OTEL_RESOURCE_ATTRIBUTES.
///
/// Important: The caller must free the returned []u8 array, if a non-null value is returned.
fn getResourceAttributes(env_vars: [](types.NullTerminatedString)) ?[]u8 {
    var final_len: usize = 0;

    for (mappings) |mapping| {
        const original_value_and_index_optional = getEnvVar(env_vars, mapping.environement_variable_name);
        if (original_value_and_index_optional) |original_value_and_index| {
            const value = original_value_and_index.value;
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
    // TODO why do we iterate twice over mappings?
    for (mappings) |mapping| {
        const env_var_name = mapping.environement_variable_name;
        const original_value_and_index_optional = getEnvVar(env_vars, env_var_name);
        if (original_value_and_index_optional) |original_value_and_index| {
            const value = original_value_and_index.value;
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

    std.debug.print("getResourceAttributes: returning {s}\n", .{resource_attributes});
    return resource_attributes;
}

/// Get the value of an environment variable from the provided env_vars list, which is a list of null-terminated
/// strings. Returns an the value of the environment variable as an optional, and the index of the environment variable;
/// the index is only valid if the environment variable was found (i.e. the optional is not null).
pub fn getEnvVar(env_vars: [](types.NullTerminatedString), name: []const u8) ?EnvVarValueAndIndex {
    for (env_vars, 0..) |env_var, idx| {
        const env_var_slice: []const u8 = std.mem.span(env_var);
        if (std.mem.indexOf(u8, env_var_slice, "=")) |equals_char_idx| {
            if (std.mem.eql(u8, name, env_var[0..equals_char_idx])) {
                if (std.mem.len(env_var) == equals_char_idx + 1) {
                    return null;
                }
                return EnvVarValueAndIndex{ .value = env_var[equals_char_idx + 1 ..], .index = idx };
            }
        }
    }

    return null;
}

fn renderEnvVarsToExport(env_vars: [](types.NullTerminatedString)) ![*c]const [*c]const u8 {
    return @ptrCast(env_vars);
}
