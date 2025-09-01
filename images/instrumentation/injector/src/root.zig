// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const builtin = @import("builtin");
const std = @import("std");

const dotnet = @import("dotnet.zig");
const libc = @import("libc.zig");
const jvm = @import("jvm.zig");
const node_js = @import("node_js.zig");
const print = @import("print.zig");
const res_attrs = @import("resource_attributes.zig");
const types = @import("types.zig");

const assert = std.debug.assert;
const expect = std.testing.expect;

const init_section_name = switch (builtin.target.os.tag) {
    .linux => ".init_array",
    .macos => "__DATA,__mod_init_func", // needed to run tests locally on macOS
    else => {
        error.OsNotSupported;
    },
};

export const init_array: [1]*const fn () callconv(.C) void linksection(init_section_name) = .{&initEnviron};

// Keep global pointers to already-calculated values to avoid multiple allocations
// on repeated lookups.
var modified_java_tool_options_value_calculated = false;
var modified_java_tool_options_value: ?types.NullTerminatedString = null;
var modified_node_options_value_calculated = false;
var modified_node_options_value: ?types.NullTerminatedString = null;

var environ_ptr: ?types.EnvironPtr = null;
var setenv_fn_ptr: ?types.SetenvFnPtr = null;

const InjectorError = error{
    CannotFindEnvironSymbol,
};

fn initEnviron() callconv(.C) void {
    const libc_library = libc.getLibc() catch |err| {
        if (err == error.UnknownLibCFlavor) {
            print.printDebug("no LibC found: {}", .{err});
        } else {
            print.printError("failed to identify libc: {}", .{err});
        }
        return;
    };

    print.printDebug("identified {s} LibC loaded from {s}", .{ switch (libc_library.flavor) {
        types.LibCFlavor.GNU => "GNU",
        types.LibCFlavor.MUSL => "musl",
        else => "unknown",
    }, libc_library.name });

    environ_ptr = libc_library.environ_ptr;

    updateStdOsEnviron() catch |err| {
        print.printError("cannot initiailize OTEL_RESOURCE_ATTRIBUTES: {}", .{err});
        return;
    };

    const maybe_modified_resource_attributes = res_attrs.getModifiedOtelResourceAttributesValue(std.posix.getenv(res_attrs.otel_resource_attributes_env_var_name)) catch |err| {
        print.printError("cannot calculate modified OTEL_RESOURCE_ATTRIBUTES: {}", .{err});
        return;
    };

    if (maybe_modified_resource_attributes) |modified_resource_attributes| {
        const setenv_res = libc_library.setenv_fn_ptr(res_attrs.otel_resource_attributes_env_var_name, modified_resource_attributes, true);
        if (setenv_res != 0) {
            print.printError("failed to set modified value for '{s}' to '{s}': errno={}", .{ res_attrs.otel_resource_attributes_env_var_name, modified_resource_attributes, setenv_res });
        }
    }
}

fn updateStdOsEnviron() !void {
    // Dynamic libs do not get the std.os.environ initialized, see https://github.com/ziglang/zig/issues/4524, so we
    // back fill it. This logic is based on parsing of envp on zig's start. We re-bind the environment every time, as
    // we cannot ensure it did not change since the previous invocation. Libc implementations can re-allocate the
    // environment (http://github.com/lattera/glibc/blob/master/stdlib/setenv.c;
    // https://git.musl-libc.org/cgit/musl/tree/src/env/setenv.c) if the backing memory location is outgrown by apps
    // modifying the environment via setenv or putenv.
    if (environ_ptr) |environment_ptr| {
        const env_array = environment_ptr.*;
        var env_var_count: usize = 0;
        while (env_array[env_var_count] != null) : (env_var_count += 1) {}

        std.os.environ = @ptrCast(@constCast(env_array[0..env_var_count]));
    } else {
        return error.CannotFindEnvironSymbol;
    }
}

export fn getenv(name_z: types.NullTerminatedString) ?types.NullTerminatedString {
    const name = std.mem.sliceTo(name_z, 0);

    print.initDebugFlag();
    print.printDebug("getenv('{s}') called", .{name});

    updateStdOsEnviron() catch |err| {
        print.printDebug("getenv('{s}') -> null; could not update std.os.environ: {}; ", .{ name, err });
        return null;
    };

    // Technically, a process could change the value of `DASH0_INJECTOR_DEBUG` after it started (mostly when we debug
    // stuff in REPL) so we look up the value every time.

    const res = getEnvValue(name);

    if (res) |value| {
        print.printDebug("getenv('{s}') -> '{s}'", .{ name, value });
    } else {
        print.printDebug("getenv('{s}') -> null", .{name});
    }

    return res;
}

fn getEnvValue(name: [:0]const u8) ?types.NullTerminatedString {
    const original_value = std.posix.getenv(name);

    if (std.mem.eql(u8, name, jvm.java_tool_options_env_var_name)) {
        if (!modified_java_tool_options_value_calculated) {
            modified_java_tool_options_value =
                jvm.checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue(original_value);
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
