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
const types = @import("types.zig");

const assert = std.debug.assert;
const expect = std.testing.expect;
const testing = std.testing;

// VERBOSE=true SUPPRESS_SKIPPED=true RUNTIMES=c,jvm TEST_CASES=otel-resource-attributes-unset,existing-env-var-return-unmodified ./watch-tests-within-container.sh

// TODO
// ====
// - add unit tests for _initEnviron.
// - move otel resource attributes stuff back to resource_attributes.zig
// - get __DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS going, add tests for child process
// - revisit __DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS vs idempotency (maybe later)
// - remove all std.debug.print calls (make them contigent on DASH0_INJECTOR_DEBUG being set).
// - move OTEL_RESOURCE_ATTRIBUTES back to resource_attributes.zig
// - add instrumentation test with an empty OTEL_RESOURCE_ATTRIBUTES env var, make sure it gets correctly replaced
//   (instead of appending a new entry).
// - enable all other env var modifications again (NODE_OPTIONS, JAVA_TOOL_OPTIONS, etc.)
// - clean up JAVA_TOOL_OPTIONS, we probably still need the -javaagent there, but not the otel resource attributes
// - add Python test for OTEL_RESOURCE_ATTRIBUTES
// - repair injector integration tests
// - enable NO_ENVIRON tests
// - check Node.js tests -- do we need to add getenv override again?
// - add tests that cached values are actually used (after adding back override for getenv)
// - add instrumentation and injector tests that also change the environment via setenv, putenv, and also directly
//   importing __environ, _environ, and environ and writing to that.
// - more extensive instrumentation tests for .NET, verifying OTEL_RESOURCE_ATTRIBUTES, and the various env vars that
//   activate tracing.

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
    const otel_resource_attributes_update_optional =
        res_attrs.getModifiedOtelResourceAttributesValue(original_env_vars);
    if (otel_resource_attributes_update_optional) |otel_resource_attributes_update| {
        if (!otel_resource_attributes_update.replace) {
            number_of_env_vars_after_modifications += 1;
        }
    }

    const modified_env_vars = try std.heap.page_allocator.alloc(
        types.NullTerminatedString, number_of_env_vars_after_modifications,
    );
    for (original_env_vars, 0..) |original_env_var, i| {
        modified_env_vars[i] = original_env_var;
    }
    if (otel_resource_attributes_update_optional) |otel_resource_attributes_update| {
        const key_value_pair =
            std.fmt.allocPrintZ(
                std.heap.page_allocator,
                "{s}={s}",
                .{ res_attrs.otel_resource_attributes_env_var_name, otel_resource_attributes_update.value },
            ) catch |err| {
                print.printError(
                    "Cannot allocate memory to manipulate the value of '{s}': {}",
                    .{ res_attrs.otel_resource_attributes_env_var_name, err },
                );
                return modified_env_vars;
            };
        if (!otel_resource_attributes_update.replace) {
            modified_env_vars[original_env_vars.len] = key_value_pair;
        } else {
            modified_env_vars[otel_resource_attributes_update.index] = key_value_pair;
        }
    }

    return modified_env_vars;
}

test "applyModifications: no changes" {
    cache.modification_cache = cache.emptyModificationCache();
    defer cache.modification_cache = cache.emptyModificationCache();

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
    cache.modification_cache = cache.emptyModificationCache();
    defer cache.modification_cache = cache.emptyModificationCache();

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
    cache.modification_cache = cache.emptyModificationCache();
    defer cache.modification_cache = cache.emptyModificationCache();

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

test "applyModifications: compose OTEL_RESOURCE_ATTRIBUTES, OTEL_RESOURCE_ATTRIBUTES present but empty, source env vars present" {
    cache.modification_cache = cache.emptyModificationCache();
    defer cache.modification_cache = cache.emptyModificationCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 5);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_NAMESPACE_NAME=namespace";
    original_env_vars[1] = "DASH0_POD_NAME=pod";
    original_env_vars[2] = "OTEL_RESOURCE_ATTRIBUTES=";
    original_env_vars[3] = "DASH0_POD_UID=uid";
    original_env_vars[4] = "DASH0_CONTAINER_NAME=container";

    const modified_env_vars = try applyModifications(original_env_vars);
    try testing.expectEqual(5, modified_env_vars.len);
    try testing.expect(std.mem.eql(u8, "DASH0_NAMESPACE_NAME=namespace", std.mem.span(modified_env_vars[0])));
    try testing.expect(std.mem.eql(u8, "DASH0_POD_NAME=pod", std.mem.span(modified_env_vars[1])));
    try testing.expect(std.mem.eql(u8, "OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container", std.mem.span(modified_env_vars[2])));
    try testing.expect(std.mem.eql(u8, "DASH0_POD_UID=uid", std.mem.span(modified_env_vars[3])));
    try testing.expect(std.mem.eql(u8, "DASH0_CONTAINER_NAME=container", std.mem.span(modified_env_vars[4])));
}

fn renderEnvVarsToExport(env_vars: [](types.NullTerminatedString)) ![*c]const [*c]const u8 {
    return @ptrCast(env_vars);
}
