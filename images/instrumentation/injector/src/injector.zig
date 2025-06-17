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

// TODO
// ====
// - create instrumentation tests that use an entrypoint directly instead of a CMD (which routes via shell parent process)
// - run tests in the container to make sure we test the mystery segfaults without child processes!, then try to remove
//   the dreaded function parameters and make it idempotent.
// - make applyModifications independent from the code that reads the original environment, instead, look up
//   OTEL_RESOURCE_ATTRIBUTES from the env_vars that have been read.
// - add unit tests for _initEnviron.
// - eliminate return values/fn params otel_resource_attributes_env_var_found and otel_resource_attributes_env_var_index
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
pub fn _initEnviron(proc_self_environ_path: []const u8) !struct { [*c]const [*c]const u8, usize } {
    var env_vars = try readProcSelfEnvironFile(proc_self_environ_path);
    defer env_vars.deinit();
    try applyModifications(env_vars);
    return try renderEnvVarsToExport(env_vars);
}

fn readProcSelfEnvironFile(proc_self_environ_path: []const u8) !*std.ArrayList(types.NullTerminatedString) {
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

    const env_vars, const otel_resource_attributes_env_var_found, const otel_resource_attributes_env_var_index =
        try readProcSelfEnvironFile(absolute_path);

    defer env_vars.deinit();
    try testing.expectEqual(0, env_vars.items.len);
    try testing.expect(!otel_resource_attributes_env_var_found);
    try testing.expectEqual(0, otel_resource_attributes_env_var_index);
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

    const env_vars, const otel_resource_attributes_env_var_found, const otel_resource_attributes_env_var_index =
        try readProcSelfEnvironFile(absolute_path);

    defer env_vars.deinit();
    try testing.expectEqual(3, env_vars.items.len);
    try testing.expect(std.mem.eql(u8, "VAR1=value1", std.mem.span(env_vars.items[0])));
    try testing.expect(std.mem.eql(u8, "VAR2=value2", std.mem.span(env_vars.items[1])));
    try testing.expect(std.mem.eql(u8, "VAR3=value3", std.mem.span(env_vars.items[2])));
    try testing.expect(!otel_resource_attributes_env_var_found);
    try testing.expectEqual(0, otel_resource_attributes_env_var_index);
}

// note: unit tests for readProcSelfEnvironFile segfault if this function is not inlined.
inline fn readProcSelfEnvironBuffer(environ_buffer_original: []const u8) !*std.ArrayList(types.NullTerminatedString) {
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

    return &env_vars;
}

test "readProcSelfEnvironBuffer: read environment variables" {
    const env_vars, const otel_resource_attributes_env_var_found, const otel_resource_attributes_env_var_index =
        try readProcSelfEnvironBuffer("VAR1=value1\x00VAR2=value2\x00VAR3=value3\x00");
    defer env_vars.deinit();

    try testing.expectEqual(3, env_vars.items.len);
    try testing.expect(std.mem.eql(u8, "VAR1=value1", std.mem.span(env_vars.items[0])));
    try testing.expect(std.mem.eql(u8, "VAR2=value2", std.mem.span(env_vars.items[1])));
    try testing.expect(std.mem.eql(u8, "VAR3=value3", std.mem.span(env_vars.items[2])));
    try testing.expect(!otel_resource_attributes_env_var_found);
    try testing.expectEqual(0, otel_resource_attributes_env_var_index);
}

fn applyModifications(_: *std.ArrayList(types.NullTerminatedString)) !void {
    // TODO implement
}

fn renderEnvVarsToExport(env_vars: *std.ArrayList(types.NullTerminatedString)) !struct { [*c]const [*c]const u8, usize } {
    // TODO enable -- unfortunately, calling getEnvVar here segfaults, while it works perfectly well when called from
    // getModifiedOtelResourceAttributesValue -- oh Zig, why are you so infuriating?
    //
    // const already_modified_optional, _ = getEnvVar(env_vars, "__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS");
    // if (already_modified_optional) |already_modified| {
    //     if (std.mem.eql(u8, std.mem.span(already_modified), "true")) {
    //         // When this process spawns a child process and passes on its own environment to that child process, it will
    //         // also pass on LD_PRELOAD, which means the injector will also run for the child process. We need to
    //         // actively prevent from applying any modifications in the child process, otherwise we would apply
    //         // modifications twice where we append to an environment variable (like OTEL_RESOURCE_ATTRIBUTES). That is,
    //         // we would end up with something like
    //         // OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name,k8s.namespace.name=namespace,k8s.pod.name=pod_name,k8s.pod.uid=pod_uid,k8s.container.name=container_name
    //         std.debug.print("[Dash0 injector] renderEnvVarsToExport(): already instrumented, skipping\n", .{});
    //         return;
    //     } else {
    //         std.debug.print("[Dash0 injector] renderEnvVarsToExport(): not yet instrumented, modifying environment\n", .{});
    //     }
    // } else {
    //     std.debug.print("[Dash0 injector] renderEnvVarsToExport(): not yet instrumented, modifying environment\n", .{});
    // }

    // TODO enable
    // try env_vars.append("__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true\x00"); // ? does it need a null byte

    // TODO this is nonsense? Should be terminated by a null pointer, not by a null character.
    // Do we need that at all?
    // try env_vars.append("\x00");

    const env_var_slices = try env_vars.toOwnedSlice();
    const env_var_count: usize = env_var_slices.len;

    var environ_buffer: []types.NullTerminatedString = try std.heap.page_allocator.alloc(types.NullTerminatedString, env_var_count);

    // TODO make sure the last pointer in environ_buffer is the NULL pointer
    for (env_var_slices, 0..) |env_var, i| {
        // We copy the env var slice to the environ_buffer, so that we can modify it later.
        // Note: We do not need to copy the final null terminator, as it is already there.
        environ_buffer[i] = env_var;
    }

    std.debug.print("[Dash0 injector] {d} _initEnviron() done\n", .{std.os.linux.getpid()});
    return .{ @ptrCast(environ_buffer), env_var_count };
}

// TODO these tests are testing the wrong function now
test "renderEnvVarsToExport: no changes" {
    modified_java_tool_options_value_calculated = false;
    modified_java_tool_options_value = null;
    modified_node_options_value_calculated = false;
    modified_node_options_value = null;
    modified_otel_resource_attributes_value_calculated = false;
    modified_otel_resource_attributes_value = null;

    var env_vars = std.ArrayList(types.NullTerminatedString).init(std.heap.page_allocator);
    defer env_vars.deinit();
    try env_vars.append("VAR1=value1");
    try env_vars.append("VAR2=value2");
    try env_vars.append("VAR3=value3");

    const modified_environment, const modified_environment_len = try renderEnvVarsToExport(&env_vars, false, 0);
    try testing.expectEqual(3, modified_environment_len);
    try testing.expect(std.mem.eql(u8, "VAR1=value1", std.mem.span(modified_environment[0])));
    try testing.expect(std.mem.eql(u8, "VAR2=value2", std.mem.span(modified_environment[1])));
    try testing.expect(std.mem.eql(u8, "VAR3=value3", std.mem.span(modified_environment[2])));
}

test "renderEnvVarsToExport: compose OTEL_RESOURCE_ATTRIBUTES, OTEL_RESOURCE_ATTRIBUTES not present, source env vars present, other env vars are present" {
    modified_java_tool_options_value_calculated = false;
    modified_java_tool_options_value = null;
    modified_node_options_value_calculated = false;
    modified_node_options_value = null;
    modified_otel_resource_attributes_value_calculated = false;
    modified_otel_resource_attributes_value = null;

    var env_vars = std.ArrayList(types.NullTerminatedString).init(std.heap.page_allocator);
    defer env_vars.deinit();
    try env_vars.append("VAR1=value1");
    try env_vars.append("DASH0_NAMESPACE_NAME=namespace");
    try env_vars.append("VAR2=value2");
    try env_vars.append("DASH0_POD_NAME=pod");
    try env_vars.append("VAR3=value3");
    try env_vars.append("DASH0_POD_UID=uid");
    try env_vars.append("VAR4=value4");
    try env_vars.append("DASH0_CONTAINER_NAME=container");
    try env_vars.append("VAR5=value5");
    try env_vars.append("DASH0_SERVICE_NAME=service");
    try env_vars.append("VAR6=value6");
    try env_vars.append("DASH0_SERVICE_VERSION=version");
    try env_vars.append("VAR7=value7");
    try env_vars.append("DASH0_SERVICE_NAMESPACE=servicenamespace");
    try env_vars.append("VAR8=value8");
    try env_vars.append("DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd");
    try env_vars.append("VAR9=value9");

    const modified_environment, const modified_environment_len = try renderEnvVarsToExport(&env_vars, false, 0);
    try testing.expectEqual(18, modified_environment_len);
    try testing.expect(std.mem.eql(u8, "VAR1=value1", std.mem.span(modified_environment[0])));
    try testing.expect(std.mem.eql(u8, "DASH0_NAMESPACE_NAME=namespace", std.mem.span(modified_environment[1])));
    try testing.expect(std.mem.eql(u8, "VAR2=value2", std.mem.span(modified_environment[2])));
    try testing.expect(std.mem.eql(u8, "DASH0_POD_NAME=pod", std.mem.span(modified_environment[3])));
    try testing.expect(std.mem.eql(u8, "VAR3=value3", std.mem.span(modified_environment[4])));
    try testing.expect(std.mem.eql(u8, "DASH0_POD_UID=uid", std.mem.span(modified_environment[5])));
    try testing.expect(std.mem.eql(u8, "VAR4=value4", std.mem.span(modified_environment[6])));
    try testing.expect(std.mem.eql(u8, "DASH0_CONTAINER_NAME=container", std.mem.span(modified_environment[7])));
    try testing.expect(std.mem.eql(u8, "VAR5=value5", std.mem.span(modified_environment[8])));
    try testing.expect(std.mem.eql(u8, "DASH0_SERVICE_NAME=service", std.mem.span(modified_environment[9])));
    try testing.expect(std.mem.eql(u8, "VAR6=value6", std.mem.span(modified_environment[10])));
    try testing.expect(std.mem.eql(u8, "DASH0_SERVICE_VERSION=version", std.mem.span(modified_environment[11])));
    try testing.expect(std.mem.eql(u8, "VAR7=value7", std.mem.span(modified_environment[12])));
    try testing.expect(std.mem.eql(u8, "DASH0_SERVICE_NAMESPACE=servicenamespace", std.mem.span(modified_environment[13])));
    try testing.expect(std.mem.eql(u8, "VAR8=value8", std.mem.span(modified_environment[14])));
    try testing.expect(std.mem.eql(u8, "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd", std.mem.span(modified_environment[15])));
    try testing.expect(std.mem.eql(u8, "VAR9=value9", std.mem.span(modified_environment[16])));
    try testing.expect(std.mem.eql(u8, "OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd", std.mem.span(modified_environment[17])));
}

test "renderEnvVarsToExport: compose OTEL_RESOURCE_ATTRIBUTES, OTEL_RESOURCE_ATTRIBUTES present, source env vars present" {
    modified_java_tool_options_value_calculated = false;
    modified_java_tool_options_value = null;
    modified_node_options_value_calculated = false;
    modified_node_options_value = null;
    modified_otel_resource_attributes_value_calculated = false;
    modified_otel_resource_attributes_value = null;

    var env_vars = std.ArrayList(types.NullTerminatedString).init(std.heap.page_allocator);
    defer env_vars.deinit();
    try env_vars.append("DASH0_NAMESPACE_NAME=namespace");
    try env_vars.append("DASH0_POD_NAME=pod");
    try env_vars.append("DASH0_POD_UID=uid");
    try env_vars.append("DASH0_CONTAINER_NAME=container");
    try env_vars.append("OTEL_RESOURCE_ATTRIBUTES=key1=value1,key2=value2");
    try env_vars.append("DASH0_SERVICE_NAME=service");
    try env_vars.append("DASH0_SERVICE_VERSION=version");
    try env_vars.append("DASH0_SERVICE_NAMESPACE=servicenamespace");
    try env_vars.append("DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd");

    const modified_environment, const modified_environment_len =
        try renderEnvVarsToExport(&env_vars, true, 4);
    try testing.expectEqual(9, modified_environment_len);
    try testing.expect(std.mem.eql(u8, "DASH0_NAMESPACE_NAME=namespace", std.mem.span(modified_environment[0])));
    try testing.expect(std.mem.eql(u8, "DASH0_POD_NAME=pod", std.mem.span(modified_environment[1])));
    try testing.expect(std.mem.eql(u8, "DASH0_POD_UID=uid", std.mem.span(modified_environment[2])));
    try testing.expect(std.mem.eql(u8, "DASH0_CONTAINER_NAME=container", std.mem.span(modified_environment[3])));
    try testing.expect(std.mem.eql(u8, "OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd,key1=value1,key2=value2", std.mem.span(modified_environment[4])));
    try testing.expect(std.mem.eql(u8, "DASH0_SERVICE_NAME=service", std.mem.span(modified_environment[5])));
    try testing.expect(std.mem.eql(u8, "DASH0_SERVICE_VERSION=version", std.mem.span(modified_environment[6])));
    try testing.expect(std.mem.eql(u8, "DASH0_SERVICE_NAMESPACE=servicenamespace", std.mem.span(modified_environment[7])));
    try testing.expect(std.mem.eql(u8, "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd", std.mem.span(modified_environment[8])));
}

/// Maps the DASH0_* environment variables that are set by the operator (workload_modifier#addEnvironmentVariables) to a
/// string that can be used for the value of OTEL_RESOURCE_ATTRIBUTES.
///
/// Important: The caller must free the returned []u8 array, if a non-null value is returned.
fn getResourceAttributes(env_vars: *std.ArrayList(types.NullTerminatedString)) ?[]u8 {
    var final_len: usize = 0;

    for (mappings) |mapping| {
        const original_value, _ = getEnvVar(env_vars, mapping.environement_variable_name);
        if (original_value) |value| {
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
        const original, _ = getEnvVar(env_vars, env_var_name);
        if (original) |value| {
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
pub fn getEnvVar(env_vars: *std.ArrayList(types.NullTerminatedString), name: []const u8) struct { ?types.NullTerminatedString, usize } {
    for (env_vars.items, 0..) |env_var, env_var_idx| {
        const env_var_slice: []const u8 = std.mem.span(env_var);
        if (std.mem.indexOf(u8, env_var_slice, "=")) |equals_char_idx| {
            if (std.mem.eql(u8, name, env_var[0..equals_char_idx])) {
                if (std.mem.len(env_var) == equals_char_idx + 1) {
                    return .{ null, 0 };
                }
                return .{ env_var[equals_char_idx + 1 ..], env_var_idx };
            }
        }
    }

    return .{ null, 0 };
}
