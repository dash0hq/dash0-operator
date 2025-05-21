// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const dotnet = @import("dotnet.zig");
const jvm = @import("jvm.zig");
const node_js = @import("node_js.zig");
const print = @import("print.zig");
const res_attrs = @import("resource_attributes.zig");
const types = @import("types.zig");

const assert = std.debug.assert;
const expect = std.testing.expect;

var __environ: *[][*:0]c_char = undefined;

comptime {
    @export(&__environ, .{ .name = "__environ", .linkage = .strong });
    @export(&__environ, .{ .name = "_environ", .linkage = .strong });
    @export(&__environ, .{ .name = "environ", .linkage = .strong });
}

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

fn initEnviron() callconv(.C) void {
    _initEnviron() catch @panic("[Dash0 injector] initEnviron failed");
}

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
    defer std.heap.page_allocator.free(environ_buffer_original);

    var otel_resource_attributes_env_var: []c_char = &.{};
    var otel_resource_attributes_index: usize = 0;

    if (std.mem.indexOf(c_char, environ_buffer_original, "OTEL_RESOURCE_ATTRIBUTES=")) |start_index| {
        if (start_index == 0 or
            // Excess of caution in case there is something out there with a 'POTEL_RESOURCE_ATTRIBUTES' :-)
            (start_index > 0 and environ_buffer_original[start_index - 1] == '\x00'))
        {
            if (std.mem.indexOfPos(c_char, environ_buffer_original, start_index, "\x00")) |end_index| {
                otel_resource_attributes_env_var = environ_buffer_original[start_index..end_index];
                otel_resource_attributes_index = start_index;
            }
        }
    }

    // TODO Get actual values from the environment
    // TODO Avoid duplication of values already set
    const additional_values = "k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app";

    var environ_buffer: [][*:0]u8 = undefined;
    if (otel_resource_attributes_env_var.len > 0) {
        // Insert the additional values after the existing OTEL_RESOURCE_ATTRIBUTES
        environ_buffer = try allocate_env(std.heap.page_allocator, "{s},{s}\x00{s}\x00", .{
            environ_buffer_original[0 .. otel_resource_attributes_index + otel_resource_attributes_env_var.len],
            additional_values,
            environ_buffer_original[otel_resource_attributes_index + 1 + otel_resource_attributes_env_var.len ..],
        });
    } else {
        // Append OTEL_RESOURCE_ATTRIBUTES
        environ_buffer = try allocate_env(std.heap.page_allocator, "{s}\x00{s}={s}\x00\x00", .{
            environ_buffer_original[0 .. environ_buffer_original.len - 1], // Skip the last \x00
            "OTEL_RESOURCE_ATTRIBUTES",
            additional_values,
        });
    }

    std.os.environ = environ_buffer;

    std.debug.print("root.zig#initEnviron: OTEL_RESOURCE_ATTRIBUTES => '{?s}'\n", .{std.posix.getenv("OTEL_RESOURCE_ATTRIBUTES")});

    __environ = &environ_buffer;
}

fn allocate_env(allocator: std.mem.Allocator, comptime fmt: []const u8, args: anytype) ![][*:0]u8 {
    const buf = try std.fmt.allocPrint(allocator, fmt, args);

    var result = std.ArrayList([*:0]u8).init(allocator);
    // We do not need to call `result.deinit()` because we are going to return the result as an owned slice,
    // which will owned in terms of memory allocation by the caller of this function.

    var index: usize = 0;
    for (buf, 0..) |c, i| {
        if (c == 0) {
            // We have a null terminator, so we need to create a slice from the start of the string to the null terminator
            // and append it to the result, provided the string is not empty.
            if (i > index) {
                try result.append(buf[index..i :0]);
            } else {
                break; // Empty string, we can stop processing
            }
            index = i + 1;
        }
    }

    // Append an empty string at the end to terminate the list
    const empty_string: [*:0]u8 = "";
    try result.append(empty_string);

    return try result.toOwnedSlice();
}

// fn getenv(name_z: types.NullTerminatedString) ?types.NullTerminatedString {
//     const name = std.mem.sliceTo(name_z, 0);
//
//     std.debug.print("root.zig#getenv: XXX __environ outer pointer: {x}\n", .{@intFromPtr(__environ)});
//     // std.debug.print("root.zig#getenv: XXX __environ inner pointer: {x}\n", .{@intFromPtr(__environ.ptr)});
//     // std.debug.print("root.zig#getenv: XXX environ_buffer: {s}\n", .{__environ[0..environ_idx]});
//
//     // TODO if we do not modify stuff because we do not override putenv and friends, we might get rid of the mutex.
//     // Need to change type from `const` to be able to lock
//     var env_mutex = _env_mutex;
//     env_mutex.lock();
//     defer env_mutex.unlock();
//
//     // Dynamic libs do not get the std.os.environ initialized, see https://github.com/ziglang/zig/issues/4524, so we
//     // back fill it. This logic is based on parsing of envp on zig's start. We re-bind the environment every time, as we
//     // cannot ensure it did not change since the previous invocation. Libc implementations can re-allocate the
//     // environment (http://github.com/lattera/glibc/blob/master/stdlib/setenv.c;
//     // https://git.musl-libc.org/cgit/musl/tree/src/env/setenv.c) if the backing memory location is outgrown by apps
//     // modifying the environment via setenv or putenv.
//     const environment_optional: [*:null]?[*:0]u8 = @ptrCast(@alignCast(__environ));
//     var environment_count: usize = 0;
//     if (@intFromPtr(__environ) != 0) { // __environ can be a null pointer, e.g. directly after clearenv()
//         while (environment_optional[environment_count]) |_| : (environment_count += 1) {}
//     }
//     std.os.environ = @as([*][*:0]u8, @ptrCast(environment_optional))[0..environment_count];
//
//     // Technically, a process could change the value of `DASH0_INJECTOR_DEBUG` after it started (mostly when we debug
//     // stuff in REPL) so we look up the value every time.
//     print.initDebugFlag();
//     print.printDebug("getenv({s}) called", .{name});
//
//     const res = getEnvValue(name);
//
//     if (res) |value| {
//         print.printDebug("getenv({s}) -> '{s}'", .{ name, value });
//     } else {
//         print.printDebug("getenv({s}) -> null", .{name});
//     }
//
//     return res;
// }

fn getEnvValue(name: [:0]const u8) ?types.NullTerminatedString {
    const original_value = std.posix.getenv(name);

    // if (std.mem.eql(
    //     u8,
    //     name,
    //     res_attrs.otel_resource_attributes_env_var_name,
    // )) {
    //     if (!modified_otel_resource_attributes_value_calculated) {
    //         modified_otel_resource_attributes_value = res_attrs.getModifiedOtelResourceAttributesValue(original_value);
    //         modified_otel_resource_attributes_value_calculated = true;
    //     }
    //     if (modified_otel_resource_attributes_value) |updated_value| {
    //         return updated_value;
    //     }
    // } else
    if (std.mem.eql(u8, name, jvm.java_tool_options_env_var_name)) {
        if (!modified_java_tool_options_value_calculated) {
            modified_java_tool_options_value = jvm.checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue(original_value);
            modified_java_tool_options_value_calculated = true;
        }
        if (modified_java_tool_options_value) |updated_value| {
            return updated_value;
        }
    } else if (std.mem.eql(u8, name, node_js.node_options_env_var_name)) {
        if (!modified_node_options_value_calculated) {
            modified_node_options_value =
                node_js.checkNodeJsOTelSdkDistributionAndGetModifiedNodeOptionsValue(original_value);
            modified_node_options_value_calculated = true;
        }
        if (modified_node_options_value) |updated_value| {
            return updated_value;
        }
    } else if (dotnet.isEnabled()) {
        if (std.mem.eql(u8, name, "CORECLR_ENABLE_PROFILING")) {
            if (dotnet.getDotnetValues()) |v| {
                return v.coreclr_enable_profiling;
            }
        } else if (std.mem.eql(u8, name, "CORECLR_PROFILER")) {
            if (dotnet.getDotnetValues()) |v| {
                return v.coreclr_profiler;
            }
        } else if (std.mem.eql(u8, name, "CORECLR_PROFILER_PATH")) {
            if (dotnet.getDotnetValues()) |v| {
                return v.coreclr_profiler_path;
            }
        } else if (std.mem.eql(u8, name, "DOTNET_ADDITIONAL_DEPS")) {
            if (dotnet.getDotnetValues()) |v| {
                return v.additional_deps;
            }
        } else if (std.mem.eql(u8, name, "DOTNET_SHARED_STORE")) {
            if (dotnet.getDotnetValues()) |v| {
                return v.shared_store;
            }
        } else if (std.mem.eql(u8, name, "DOTNET_STARTUP_HOOKS")) {
            if (dotnet.getDotnetValues()) |v| {
                return v.startup_hooks;
            }
        } else if (std.mem.eql(u8, name, "OTEL_DOTNET_AUTO_HOME")) {
            if (dotnet.getDotnetValues()) |v| {
                return v.otel_auto_home;
            }
        }
    }

    // The requested environment variable is not one that we want to modify, hence we just return the original value by
    // returning a pointer to it.
    if (original_value) |val| {
        return val.ptr;
    }

    // The requested environment variable is not one that we want to modify, and it does not exist. Return null.
    return null;
}
