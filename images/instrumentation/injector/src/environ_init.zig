// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const builtin = @import("builtin");
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

var cached_original_env_vars: [](types.NullTerminatedString) = undefined;

pub fn _initEnviron(proc_self_environ_path: []const u8) ![*c]const [*c]const u8 {
    const original_env_vars = try readProcSelfEnvironFile(proc_self_environ_path);
    cached_original_env_vars = original_env_vars;
    print.initDebugFlag(original_env_vars);
    if (print.isDebug()) {
        const pid = std.os.linux.getpid();
        const exe = std.fs.selfExePathAlloc(std.heap.page_allocator) catch "?";
        print.printDebug("initalizing __environ, _environ and environ (pid: {d}, executable: {s})\n", .{ pid, exe });
    }
    const modified_env_vars = try applyModifications(original_env_vars);
    return try renderEnvVarsToExport(modified_env_vars);
}

test "_initEnviron: /proc/self/environ does not exist" {
    const error_union = _initEnviron("/does/not/exist"); // catch {
    try testing.expectError(std.fs.File.OpenError.FileNotFound, error_union);
}

test "_initEnviron: empty /proc/self/environ file" {
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

    const __environ_internal = try _initEnviron(absolute_path);
    try testing.expectEqual(1, std.mem.len(__environ_internal));
    try testing.expectEqualStrings("__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true", std.mem.span(__environ_internal[0]));
    try testing.expectEqual(null, __environ_internal[1]);
}

test "_initEnviron: no modifications" {
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

    const __environ_internal = try _initEnviron(absolute_path);

    try testing.expectEqual(4, std.mem.len(__environ_internal));
    try testing.expectEqualStrings("VAR1=value1", std.mem.span(__environ_internal[0]));
    try testing.expectEqualStrings("VAR2=value2", std.mem.span(__environ_internal[1]));
    try testing.expectEqualStrings("VAR3=value3", std.mem.span(__environ_internal[2]));
    try testing.expectEqualStrings("__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true", std.mem.span(__environ_internal[3]));
}

test "_initEnviron: append OTEL_RESOURCE_ATTRIBUTES" {
    const filename = "unit_test_proc_self_environ";
    const proc_self_environ_file = try std.fs.cwd().createFile(filename, .{});
    const content = "VAR1=value1\x00DASH0_NAMESPACE_NAME=namespace\x00DASH0_POD_NAME=pod\x00DASH0_POD_UID=uid\x00DASH0_CONTAINER_NAME=container\x00DASH0_SERVICE_NAME=service\x00DASH0_SERVICE_VERSION=version\x00DASH0_SERVICE_NAMESPACE=servicenamespace\x00DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd\x00VAR2=value2\x00";
    _ = try proc_self_environ_file.write(content);
    defer {
        proc_self_environ_file.close();
        std.fs.cwd().deleteFile(filename) catch |err| {
            std.debug.print("Failed to delete file {s}: {}\n", .{ filename, err });
        };
    }
    const absolute_path = try std.fs.cwd().realpathAlloc(std.heap.page_allocator, filename);
    defer std.heap.page_allocator.free(absolute_path);

    const __environ_internal = try _initEnviron(absolute_path);

    try testing.expectEqual(12, std.mem.len(__environ_internal));
    try testing.expectEqualStrings("VAR1=value1", std.mem.span(__environ_internal[0]));
    try testing.expectEqualStrings("DASH0_NAMESPACE_NAME=namespace", std.mem.span(__environ_internal[1]));
    try testing.expectEqualStrings("DASH0_POD_NAME=pod", std.mem.span(__environ_internal[2]));
    try testing.expectEqualStrings("DASH0_POD_UID=uid", std.mem.span(__environ_internal[3]));
    try testing.expectEqualStrings("DASH0_CONTAINER_NAME=container", std.mem.span(__environ_internal[4]));
    try testing.expectEqualStrings("DASH0_SERVICE_NAME=service", std.mem.span(__environ_internal[5]));
    try testing.expectEqualStrings("DASH0_SERVICE_VERSION=version", std.mem.span(__environ_internal[6]));
    try testing.expectEqualStrings("DASH0_SERVICE_NAMESPACE=servicenamespace", std.mem.span(__environ_internal[7]));
    try testing.expectEqualStrings("DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd", std.mem.span(__environ_internal[8]));
    try testing.expectEqualStrings("VAR2=value2", std.mem.span(__environ_internal[9]));
    try testing.expectEqualStrings("__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true", std.mem.span(__environ_internal[10]));
    try testing.expectEqualStrings(
        "OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd",
        std.mem.span(__environ_internal[11]),
    );
}

test "_initEnviron: replace OTEL_RESOURCE_ATTRIBUTES" {
    const filename = "unit_test_proc_self_environ";
    const proc_self_environ_file = try std.fs.cwd().createFile(filename, .{});
    const content = "VAR1=value1\x00DASH0_NAMESPACE_NAME=namespace\x00DASH0_POD_NAME=pod\x00DASH0_POD_UID=uid\x00DASH0_CONTAINER_NAME=container\x00OTEL_RESOURCE_ATTRIBUTES=www=xxx,yyy=zzz\x00DASH0_SERVICE_NAME=service\x00DASH0_SERVICE_VERSION=version\x00DASH0_SERVICE_NAMESPACE=servicenamespace\x00DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd\x00VAR2=value2\x00";
    _ = try proc_self_environ_file.write(content);
    defer {
        proc_self_environ_file.close();
        std.fs.cwd().deleteFile(filename) catch |err| {
            std.debug.print("Failed to delete file {s}: {}\n", .{ filename, err });
        };
    }
    const absolute_path = try std.fs.cwd().realpathAlloc(std.heap.page_allocator, filename);
    defer std.heap.page_allocator.free(absolute_path);

    const __environ_internal = try _initEnviron(absolute_path);

    try testing.expectEqual(12, std.mem.len(__environ_internal));
    try testing.expectEqualStrings("VAR1=value1", std.mem.span(__environ_internal[0]));
    try testing.expectEqualStrings("DASH0_NAMESPACE_NAME=namespace", std.mem.span(__environ_internal[1]));
    try testing.expectEqualStrings("DASH0_POD_NAME=pod", std.mem.span(__environ_internal[2]));
    try testing.expectEqualStrings("DASH0_POD_UID=uid", std.mem.span(__environ_internal[3]));
    try testing.expectEqualStrings("DASH0_CONTAINER_NAME=container", std.mem.span(__environ_internal[4]));
    try testing.expectEqualStrings(
        "OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd,www=xxx,yyy=zzz",
        std.mem.span(__environ_internal[5]),
    );
    try testing.expectEqualStrings("DASH0_SERVICE_NAME=service", std.mem.span(__environ_internal[6]));
    try testing.expectEqualStrings("DASH0_SERVICE_VERSION=version", std.mem.span(__environ_internal[7]));
    try testing.expectEqualStrings("DASH0_SERVICE_NAMESPACE=servicenamespace", std.mem.span(__environ_internal[8]));
    try testing.expectEqualStrings("DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd", std.mem.span(__environ_internal[9]));
    try testing.expectEqualStrings("VAR2=value2", std.mem.span(__environ_internal[10]));
    try testing.expectEqualStrings("__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true", std.mem.span(__environ_internal[11]));
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
    // TODO shouldn't we do `defer std.heap.page_allocator.free(environ_buffer_original);` here?

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
    try testing.expectEqualStrings("VAR1=value1", std.mem.span(original_env_vars[0]));
    try testing.expectEqualStrings("VAR2=value2", std.mem.span(original_env_vars[1]));
    try testing.expectEqualStrings("VAR3=value3", std.mem.span(original_env_vars[2]));
}

fn readProcSelfEnvironBuffer(environ_buffer_original: []const u8) ![](types.NullTerminatedString) {
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

test "readProcSelfEnvironBuffer: empty buffer" {
    const env_vars = try readProcSelfEnvironBuffer("\x00");
    try testing.expectEqual(0, env_vars.len);
}

test "readProcSelfEnvironBuffer: read environment variables" {
    const env_vars = try readProcSelfEnvironBuffer("VAR1=value1\x00VAR2=value2\x00VAR3=value3\x00");
    try testing.expectEqual(3, env_vars.len);
    try testing.expectEqualStrings("VAR1=value1", std.mem.span(env_vars[0]));
    try testing.expectEqualStrings("VAR2=value2", std.mem.span(env_vars[1]));
    try testing.expectEqualStrings("VAR3=value3", std.mem.span(env_vars[2]));
}

/// Applies all modifications to the environment variables.
fn applyModifications(original_env_vars: [](types.NullTerminatedString)) ![](types.NullTerminatedString) {
    // Maintenance note: The environment variables modified by environ_init.applyModifications must be kept in sync with
    // the environment variables modified by the environ_api.modifyEnvVar.

    if (env.isTrue(original_env_vars, types.injector_has_applied_modifications_env_var_name)) {
        // The parent process has already been instrumented, and it then started a child process (which is the process
        // we are currently in), also, the child process has inherited the environment from the parent process. This
        // means all our modifications have already been applied, we must not apply them again.
        return original_env_vars;
    }

    // +1 for __DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS
    var number_of_env_vars_after_modifications: usize = original_env_vars.len + 1;

    const java_tool_options_update_optional =
        jvm.checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue(original_env_vars);
    if (java_tool_options_update_optional) |java_tool_options_update| {
        if (!java_tool_options_update.replace) {
            number_of_env_vars_after_modifications += 1;
        }
    }

    const original_node_options_optional = env.getEnvVar(original_env_vars, node_js.node_options_env_var_name);
    const node_options_update_optional =
        node_js.checkNodeJsOTelSdkDistributionAndGetModifiedNodeOptionsValue(original_node_options_optional);
    if (node_options_update_optional) |node_options_update| {
        if (!node_options_update.replace) {
            number_of_env_vars_after_modifications += 1;
        }
    }

    const otel_resource_attributes_update_optional =
        res_attrs.getModifiedOtelResourceAttributesValue(original_env_vars);
    if (otel_resource_attributes_update_optional) |otel_resource_attributes_update| {
        if (!otel_resource_attributes_update.replace) {
            number_of_env_vars_after_modifications += 1;
        }
    }

    var dotnet_value_optional: ?types.DotnetEnvVarUpdates = null;
    if (dotnet.isEnabled(original_env_vars)) {
        if (dotnet.getDotnetEnvVarUpdates()) |dotnet_env_var_updates| {
            dotnet_value_optional = dotnet_env_var_updates;
            number_of_env_vars_after_modifications += 7;
        }
    }

    const modified_env_vars = try std.heap.page_allocator.alloc(
        types.NullTerminatedString,
        number_of_env_vars_after_modifications,
    );

    // copy all original environment variables over to the new slice of modified environment variables
    for (original_env_vars, 0..) |original_env_var, i| {
        modified_env_vars[i] = original_env_var;
    }

    var index_for_appending_env_vars: usize = original_env_vars.len;

    // Set a marker environment variable to avoid applying the modifications multiple times, in case the current child
    // spawns a child process which inherits the environment from this process.
    modified_env_vars[index_for_appending_env_vars] = types.injector_has_applied_modifications_env_var_name ++ "=true";
    index_for_appending_env_vars += 1;

    // apply the actual modifications
    if (!applyEnvVarUpdate(
        modified_env_vars,
        jvm.java_tool_options_env_var_name,
        java_tool_options_update_optional,
        &index_for_appending_env_vars,
    )) {
        return modified_env_vars;
    }
    if (!applyEnvVarUpdate(
        modified_env_vars,
        node_js.node_options_env_var_name,
        node_options_update_optional,
        &index_for_appending_env_vars,
    )) {
        return modified_env_vars;
    }
    if (!applyEnvVarUpdate(
        modified_env_vars,
        res_attrs.otel_resource_attributes_env_var_name,
        otel_resource_attributes_update_optional,
        &index_for_appending_env_vars,
    )) {
        return modified_env_vars;
    }

    if (dotnet.isEnabled(original_env_vars)) {
        if (dotnet_value_optional) |dotnet_env_var_updates| {
            if (!applyDotnetEnvVarModifications(
                modified_env_vars,
                dotnet_env_var_updates,
                &index_for_appending_env_vars,
            )) {
                return modified_env_vars;
            }
        }
    }

    return modified_env_vars;
}

/// Applies one specific environment variable update to the modified environment variables slice.
/// Client code is expected to stop trying to modify environment variables if the function returns false (this indicates
/// that a memory allocation failed).
fn applyEnvVarUpdate(
    modified_env_vars: [](types.NullTerminatedString),
    env_var_name: []const u8,
    env_var_update_optional: ?types.EnvVarUpdate,
    index_for_appending_env_vars: *usize,
) bool {
    if (env_var_update_optional) |env_var_update| {
        const key_value_pair =
            std.fmt.allocPrintZ(
                std.heap.page_allocator,
                "{s}={s}",
                .{ env_var_name, env_var_update.value },
            ) catch |err| {
                print.printError(
                    "Cannot allocate memory to manipulate the value of '{s}': {}",
                    .{ env_var_name, err },
                );
                return false;
            };
        if (!env_var_update.replace) {
            modified_env_vars[index_for_appending_env_vars.*] = key_value_pair;
            index_for_appending_env_vars.* += 1;
        } else {
            modified_env_vars[env_var_update.index] = key_value_pair;
        }
    }
    return true;
}

fn applyDotnetEnvVarModifications(
    modified_env_vars: [](types.NullTerminatedString),
    dotnet_env_var_updates: types.DotnetEnvVarUpdates,
    index_for_appending_env_vars: *usize,
) bool {
    if (!applyEnvVarUpdate(
        modified_env_vars,
        dotnet.coreclr_enable_profiling_env_var_name,
        dotnet_env_var_updates.coreclr_enable_profiling,
        index_for_appending_env_vars,
    )) {
        return false;
    }
    if (!applyEnvVarUpdate(
        modified_env_vars,
        dotnet.coreclr_profiler_env_var_name,
        dotnet_env_var_updates.coreclr_profiler,
        index_for_appending_env_vars,
    )) {
        return false;
    }
    if (!applyEnvVarUpdate(
        modified_env_vars,
        dotnet.coreclr_profiler_path_env_var_name,
        dotnet_env_var_updates.coreclr_profiler_path,
        index_for_appending_env_vars,
    )) {
        return false;
    }
    if (!applyEnvVarUpdate(
        modified_env_vars,
        dotnet.dotnet_additional_deps_env_var_name,
        dotnet_env_var_updates.dotnet_additional_deps,
        index_for_appending_env_vars,
    )) {
        return false;
    }
    if (!applyEnvVarUpdate(
        modified_env_vars,
        dotnet.dotnet_shared_store_env_var_name,
        dotnet_env_var_updates.dotnet_shared_store,
        index_for_appending_env_vars,
    )) {
        return false;
    }
    if (!applyEnvVarUpdate(
        modified_env_vars,
        dotnet.dotnet_startup_hooks_env_var_name,
        dotnet_env_var_updates.dotnet_startup_hooks,
        index_for_appending_env_vars,
    )) {
        return false;
    }
    if (!applyEnvVarUpdate(
        modified_env_vars,
        dotnet.otel_auto_home_env_var_name,
        dotnet_env_var_updates.otel_auto_home,
        index_for_appending_env_vars,
    )) {
        return false;
    }
    return true;
}

test "applyModifications: no changes" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 3);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "VAR1=value1";
    original_env_vars[1] = "VAR2=value2";
    original_env_vars[2] = "VAR3=value3";

    const modified_env_vars = try applyModifications(original_env_vars);
    try testing.expectEqual(4, modified_env_vars.len);
    try testing.expectEqualStrings("VAR1=value1", std.mem.span(modified_env_vars[0]));
    try testing.expectEqualStrings("VAR2=value2", std.mem.span(modified_env_vars[1]));
    try testing.expectEqualStrings("VAR3=value3", std.mem.span(modified_env_vars[2]));
    try testing.expectEqualStrings("__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true", std.mem.span(modified_env_vars[3]));
}

test "applyModifications: append JAVA_TOOL_OPTIONS" {
    try test_util.createDummyJavaAgent();
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

    const modified_env_vars = try applyModifications(original_env_vars);
    try testing.expectEqual(5, modified_env_vars.len);
    try testing.expectEqualStrings("VAR1=value1", std.mem.span(modified_env_vars[0]));
    try testing.expectEqualStrings("VAR2=value2", std.mem.span(modified_env_vars[1]));
    try testing.expectEqualStrings("VAR3=value3", std.mem.span(modified_env_vars[2]));
    try testing.expectEqualStrings("__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true", std.mem.span(modified_env_vars[3]));
    try testing.expectEqualStrings("JAVA_TOOL_OPTIONS=-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", std.mem.span(modified_env_vars[4]));
}

test "applyModifications: append NODE_OPTIONS" {
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

    const modified_env_vars = try applyModifications(original_env_vars);
    try testing.expectEqual(5, modified_env_vars.len);
    try testing.expectEqualStrings("VAR1=value1", std.mem.span(modified_env_vars[0]));
    try testing.expectEqualStrings("VAR2=value2", std.mem.span(modified_env_vars[1]));
    try testing.expectEqualStrings("VAR3=value3", std.mem.span(modified_env_vars[2]));
    try testing.expectEqualStrings("__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true", std.mem.span(modified_env_vars[3]));
    try testing.expectEqualStrings("NODE_OPTIONS=--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry", std.mem.span(modified_env_vars[4]));
}

test "applyModifications: compose OTEL_RESOURCE_ATTRIBUTES, OTEL_RESOURCE_ATTRIBUTES not present, source env vars present, other env vars are present" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

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
    try testing.expectEqual(19, modified_env_vars.len);
    try testing.expectEqualStrings("VAR1=value1", std.mem.span(modified_env_vars[0]));
    try testing.expectEqualStrings("DASH0_NAMESPACE_NAME=namespace", std.mem.span(modified_env_vars[1]));
    try testing.expectEqualStrings("VAR2=value2", std.mem.span(modified_env_vars[2]));
    try testing.expectEqualStrings("DASH0_POD_NAME=pod", std.mem.span(modified_env_vars[3]));
    try testing.expectEqualStrings("VAR3=value3", std.mem.span(modified_env_vars[4]));
    try testing.expectEqualStrings("DASH0_POD_UID=uid", std.mem.span(modified_env_vars[5]));
    try testing.expectEqualStrings("VAR4=value4", std.mem.span(modified_env_vars[6]));
    try testing.expectEqualStrings("DASH0_CONTAINER_NAME=container", std.mem.span(modified_env_vars[7]));
    try testing.expectEqualStrings("VAR5=value5", std.mem.span(modified_env_vars[8]));
    try testing.expectEqualStrings("DASH0_SERVICE_NAME=service", std.mem.span(modified_env_vars[9]));
    try testing.expectEqualStrings("VAR6=value6", std.mem.span(modified_env_vars[10]));
    try testing.expectEqualStrings("DASH0_SERVICE_VERSION=version", std.mem.span(modified_env_vars[11]));
    try testing.expectEqualStrings("VAR7=value7", std.mem.span(modified_env_vars[12]));
    try testing.expectEqualStrings("DASH0_SERVICE_NAMESPACE=servicenamespace", std.mem.span(modified_env_vars[13]));
    try testing.expectEqualStrings("VAR8=value8", std.mem.span(modified_env_vars[14]));
    try testing.expectEqualStrings("DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd", std.mem.span(modified_env_vars[15]));
    try testing.expectEqualStrings("VAR9=value9", std.mem.span(modified_env_vars[16]));
    try testing.expectEqualStrings("__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true", std.mem.span(modified_env_vars[17]));
    try testing.expectEqualStrings(
        "OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd",
        std.mem.span(modified_env_vars[18]),
    );
}

test "applyModifications: compose OTEL_RESOURCE_ATTRIBUTES, OTEL_RESOURCE_ATTRIBUTES present, source env vars present" {
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

    const modified_env_vars = try applyModifications(original_env_vars);
    try testing.expectEqual(10, modified_env_vars.len);
    try testing.expectEqualStrings("DASH0_NAMESPACE_NAME=namespace", std.mem.span(modified_env_vars[0]));
    try testing.expectEqualStrings("DASH0_POD_NAME=pod", std.mem.span(modified_env_vars[1]));
    try testing.expectEqualStrings("DASH0_POD_UID=uid", std.mem.span(modified_env_vars[2]));
    try testing.expectEqualStrings("DASH0_CONTAINER_NAME=container", std.mem.span(modified_env_vars[3]));
    try testing.expectEqualStrings(
        "OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd,key1=value1,key2=value2",
        std.mem.span(modified_env_vars[4]),
    );
    try testing.expectEqualStrings("DASH0_SERVICE_NAME=service", std.mem.span(modified_env_vars[5]));
    try testing.expectEqualStrings("DASH0_SERVICE_VERSION=version", std.mem.span(modified_env_vars[6]));
    try testing.expectEqualStrings("DASH0_SERVICE_NAMESPACE=servicenamespace", std.mem.span(modified_env_vars[7]));
    try testing.expectEqualStrings("DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd", std.mem.span(modified_env_vars[8]));
    try testing.expectEqualStrings("__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true", std.mem.span(modified_env_vars[9]));
}

test "applyModifications: compose OTEL_RESOURCE_ATTRIBUTES, OTEL_RESOURCE_ATTRIBUTES present but empty, source env vars present" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 5);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_NAMESPACE_NAME=namespace";
    original_env_vars[1] = "DASH0_POD_NAME=pod";
    original_env_vars[2] = "OTEL_RESOURCE_ATTRIBUTES=";
    original_env_vars[3] = "DASH0_POD_UID=uid";
    original_env_vars[4] = "DASH0_CONTAINER_NAME=container";

    const modified_env_vars = try applyModifications(original_env_vars);
    try testing.expectEqual(6, modified_env_vars.len);
    try testing.expectEqualStrings("DASH0_NAMESPACE_NAME=namespace", std.mem.span(modified_env_vars[0]));
    try testing.expectEqualStrings("DASH0_POD_NAME=pod", std.mem.span(modified_env_vars[1]));
    try testing.expectEqualStrings(
        "OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container",
        std.mem.span(modified_env_vars[2]),
    );
    try testing.expectEqualStrings("DASH0_POD_UID=uid", std.mem.span(modified_env_vars[3]));
    try testing.expectEqualStrings("DASH0_CONTAINER_NAME=container", std.mem.span(modified_env_vars[4]));
    try testing.expectEqualStrings("__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true", std.mem.span(modified_env_vars[5]));
}

test "applyModifications: append OTEL_RESOURCE_ATTRIBUTES, JAVA_TOOL_OPTIONS, and .NET environment variables" {
    try test_util.createAllDummyInstrumentations();
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    // set a dummy value for the libc flavor just for the test, will be reverted after the test via
    // `defer cache.injector_cache = cache.emptyInjectorCache()`
    cache.injector_cache.libc_flavor = types.LibCFlavor.GNU_LIBC;

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 9);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "DASH0_NAMESPACE_NAME=namespace";
    original_env_vars[1] = "DASH0_POD_NAME=pod";
    original_env_vars[2] = "DASH0_POD_UID=uid";
    original_env_vars[3] = "DASH0_CONTAINER_NAME=container";
    original_env_vars[4] = "DASH0_SERVICE_NAME=service";
    original_env_vars[5] = "DASH0_SERVICE_VERSION=version";
    original_env_vars[6] = "DASH0_SERVICE_NAMESPACE=servicenamespace";
    original_env_vars[7] = "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd";
    original_env_vars[8] = "DASH0_EXPERIMENTAL_DOTNET_INJECTION=true";

    const modified_env_vars = try applyModifications(original_env_vars);
    try testing.expectEqual(20, modified_env_vars.len);
    try testing.expectEqualStrings("DASH0_NAMESPACE_NAME=namespace", std.mem.span(modified_env_vars[0]));
    try testing.expectEqualStrings("DASH0_POD_NAME=pod", std.mem.span(modified_env_vars[1]));
    try testing.expectEqualStrings("DASH0_POD_UID=uid", std.mem.span(modified_env_vars[2]));
    try testing.expectEqualStrings("DASH0_CONTAINER_NAME=container", std.mem.span(modified_env_vars[3]));
    try testing.expectEqualStrings("DASH0_SERVICE_NAME=service", std.mem.span(modified_env_vars[4]));
    try testing.expectEqualStrings("DASH0_SERVICE_VERSION=version", std.mem.span(modified_env_vars[5]));
    try testing.expectEqualStrings("DASH0_SERVICE_NAMESPACE=servicenamespace", std.mem.span(modified_env_vars[6]));
    try testing.expectEqualStrings("DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd", std.mem.span(modified_env_vars[7]));
    try testing.expectEqualStrings("DASH0_EXPERIMENTAL_DOTNET_INJECTION=true", std.mem.span(modified_env_vars[8]));
    try testing.expectEqualStrings("__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true", std.mem.span(modified_env_vars[9]));
    try testing.expectEqualStrings(
        "JAVA_TOOL_OPTIONS=-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dotel.resource.attributes=k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd",
        std.mem.span(modified_env_vars[10]),
    );
    try testing.expectEqualStrings(
        "NODE_OPTIONS=--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry",
        std.mem.span(modified_env_vars[11]),
    );
    try testing.expectEqualStrings(
        "OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd",
        std.mem.span(modified_env_vars[12]),
    );
    try testing.expectEqualStrings(
        "CORECLR_ENABLE_PROFILING=1",
        std.mem.span(modified_env_vars[13]),
    );
    try testing.expectEqualStrings(
        "CORECLR_PROFILER={918728DD-259F-4A6A-AC2B-B85E1B658318}",
        std.mem.span(modified_env_vars[14]),
    );
    const expected_profiler_path = comptime "CORECLR_PROFILER_PATH=/__dash0__/instrumentation/dotnet/glibc/" ++
        test_util.getDotnetPlatformForTest() ++
        "/OpenTelemetry.AutoInstrumentation.Native.so";
    try testing.expectEqualStrings(
        expected_profiler_path,
        std.mem.span(modified_env_vars[15]),
    );
    try testing.expectEqualStrings(
        "DOTNET_ADDITIONAL_DEPS=/__dash0__/instrumentation/dotnet/glibc/AdditionalDeps",
        std.mem.span(modified_env_vars[16]),
    );
    try testing.expectEqualStrings(
        "DOTNET_SHARED_STORE=/__dash0__/instrumentation/dotnet/glibc/store",
        std.mem.span(modified_env_vars[17]),
    );
    try testing.expectEqualStrings(
        "DOTNET_STARTUP_HOOKS=/__dash0__/instrumentation/dotnet/glibc/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll",
        std.mem.span(modified_env_vars[18]),
    );
    try testing.expectEqualStrings(
        "OTEL_DOTNET_AUTO_HOME=/__dash0__/instrumentation/dotnet/glibc",
        std.mem.span(modified_env_vars[19]),
    );
}

test "applyModifications: replace JAVA_TOOL_OPTIONS, NODE_OPTIONS, and OTEL_RESOURCE_ATTRIBUTES" {
    try test_util.createDummyJavaAgent();
    _ = try std.fs.createFileAbsolute(jvm.otel_java_agent_path, .{});
    try test_util.createDummyNodeJsDistribution();
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 11);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "NODE_OPTIONS=--abort-on-uncaught-exception";
    original_env_vars[1] = "DASH0_NAMESPACE_NAME=namespace";
    original_env_vars[2] = "JAVA_TOOL_OPTIONS=-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh";
    original_env_vars[3] = "DASH0_POD_NAME=pod";
    original_env_vars[4] = "DASH0_POD_UID=uid";
    original_env_vars[5] = "DASH0_CONTAINER_NAME=container";
    original_env_vars[6] = "OTEL_RESOURCE_ATTRIBUTES=key1=value1,key2=value2";
    original_env_vars[7] = "DASH0_SERVICE_NAME=service";
    original_env_vars[8] = "DASH0_SERVICE_VERSION=version";
    original_env_vars[9] = "DASH0_SERVICE_NAMESPACE=servicenamespace";
    original_env_vars[10] = "DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd";

    const modified_env_vars = try applyModifications(original_env_vars);
    try testing.expectEqual(12, modified_env_vars.len);
    try testing.expectEqualStrings("NODE_OPTIONS=--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry --abort-on-uncaught-exception", std.mem.span(modified_env_vars[0]));
    try testing.expectEqualStrings("DASH0_NAMESPACE_NAME=namespace", std.mem.span(modified_env_vars[1]));
    try testing.expectEqualStrings(
        "JAVA_TOOL_OPTIONS=-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh,k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(modified_env_vars[2]),
    );
    try testing.expectEqualStrings("DASH0_POD_NAME=pod", std.mem.span(modified_env_vars[3]));
    try testing.expectEqualStrings("DASH0_POD_UID=uid", std.mem.span(modified_env_vars[4]));
    try testing.expectEqualStrings("DASH0_CONTAINER_NAME=container", std.mem.span(modified_env_vars[5]));
    try testing.expectEqualStrings(
        "OTEL_RESOURCE_ATTRIBUTES=k8s.namespace.name=namespace,k8s.pod.name=pod,k8s.pod.uid=uid,k8s.container.name=container,service.name=service,service.version=version,service.namespace=servicenamespace,aaa=bbb,ccc=ddd,key1=value1,key2=value2",
        std.mem.span(modified_env_vars[6]),
    );
    try testing.expectEqualStrings("DASH0_SERVICE_NAME=service", std.mem.span(modified_env_vars[7]));
    try testing.expectEqualStrings("DASH0_SERVICE_VERSION=version", std.mem.span(modified_env_vars[8]));
    try testing.expectEqualStrings("DASH0_SERVICE_NAMESPACE=servicenamespace", std.mem.span(modified_env_vars[9]));
    try testing.expectEqualStrings("DASH0_RESOURCE_ATTRIBUTES=aaa=bbb,ccc=ddd", std.mem.span(modified_env_vars[10]));
    try testing.expectEqualStrings("__DASH0_INJECTOR_HAS_APPLIED_MODIFICATIONS=true", std.mem.span(modified_env_vars[11]));
}

fn renderEnvVarsToExport(env_vars: [](types.NullTerminatedString)) ![*c]const [*c]const u8 {
    if (env_vars.len == 0) {
        return @as([1][*c]const u8, .{null})[0..].ptr;
    }

    const exported = try std.heap.page_allocator.allocSentinel(
        [*c]const u8,
        // +1 for the final null pointer
        env_vars.len + 1,
        null,
    );

    for (env_vars, 0..) |env_var, i| {
        exported[i] = @ptrCast(env_var);
    }

    // We need to append a final null pointer to the exported array of pointers. This is the signal for the routines
    // in glibc/musl/etc. that read from the exported __environ symbol that the list of environment variables has been
    // read completely.
    //
    // Implementation note: Appending a null _character_ to the incoming env_vars: [](types.NullTerminatedString) is not
    // an adequate substitute - the null character is a single byte with a value of zero, which is different from the
    // NULL pointer, which is a pointer type, i.e. 8 bytes for 64 bit architectures.
    exported[env_vars.len] = null;

    return @ptrCast(exported);
}

pub fn getCachedOriginalEnvVars() [](types.NullTerminatedString) {
    return cached_original_env_vars;
}
