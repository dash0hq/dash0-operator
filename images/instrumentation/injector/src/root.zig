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
const testing = std.testing;
const expect = testing.expect;

// We need to use a rather "innocent" type here, the actual type involves
// optionals that cannot be used in global declarations.
extern var __environ: [*]u8;

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

export fn getenv(name_z: types.NullTerminatedString) ?types.NullTerminatedString {
    const name = std.mem.sliceTo(name_z, 0);

    // Need to change type from `const` to be able to lock
    var env_mutex = _env_mutex;
    env_mutex.lock();
    defer env_mutex.unlock();

    // Dynamic libs do not get the std.os.environ initialized, see https://github.com/ziglang/zig/issues/4524, so we
    // back fill it. This logic is based on parsing of envp on zig's start. We re-bind the environment every time, as we
    // cannot ensure it did not change since the previous invocation. Libc implementations can re-allocate the
    // environment (http://github.com/lattera/glibc/blob/master/stdlib/setenv.c;
    // https://git.musl-libc.org/cgit/musl/tree/src/env/setenv.c) if the backing memory location is outgrown by apps
    // modifying the environment via setenv or putenv.
    const environment_optional: [*:null]?[*:0]u8 = @ptrCast(@alignCast(__environ));
    var environment_count: usize = 0;
    if (@intFromPtr(__environ) != 0) { // __environ can be a null pointer, e.g. directly after clearenv()
        while (environment_optional[environment_count]) |_| : (environment_count += 1) {}
    }
    std.os.environ = @as([*][*:0]u8, @ptrCast(environment_optional))[0..environment_count];

    // Technically, a process could change the value of `DASH0_INJECTOR_DEBUG` after it started (mostly when we debug
    // stuff in REPL) so we look up the value every time.
    print.initDebugFlag();
    print.printDebug("getenv({s}) called", .{name});

    const res = getEnvValue(name);

    if (res) |value| {
        print.printDebug("getenv({s}) -> '{s}'", .{ name, value });
    } else {
        print.printDebug("getenv({s}) -> null", .{name});
    }

    return res;
}

fn getEnvValue(name: [:0]const u8) ?types.NullTerminatedString {
    const original_value = std.posix.getenv(name);

    if (std.mem.eql(
        u8,
        name,
        res_attrs.otel_resource_attributes_env_var_name,
    )) {
        if (!modified_otel_resource_attributes_value_calculated) {
            modified_otel_resource_attributes_value = res_attrs.getModifiedOtelResourceAttributesValue(original_value);
            modified_otel_resource_attributes_value_calculated = true;
        }
        if (modified_otel_resource_attributes_value) |updated_value| {
            return updated_value;
        }
    } else if (std.mem.eql(u8, name, jvm.java_tool_options_env_var_name)) {
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
