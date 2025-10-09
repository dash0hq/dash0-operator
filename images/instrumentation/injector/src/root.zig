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

const empty_z_string = "\x00";

const init_section_name = switch (builtin.target.os.tag) {
    .linux => ".init_array",
    .macos => "__DATA,__mod_init_func", // needed to run tests locally on macOS
    else => {
        error.OsNotSupported;
    },
};

export const init_array: [1]*const fn () callconv(.C) void linksection(init_section_name) = .{&initEnviron};

var environ_ptr: ?types.EnvironPtr = null;

const InjectorError = error{
    CannotFindEnvironSymbol,
};

fn initEnviron() callconv(.C) void {
    const libc_info = libc.getLibCInfo() catch |err| {
        if (err == error.UnknownLibCFlavor) {
            print.printMessage("no libc found: {}", .{err});
        } else {
            print.printMessage("failed to identify libc: {}", .{err});
        }
        return;
    };
    dotnet.setLibcFlavor(libc_info.flavor);

    environ_ptr = libc_info.environ_ptr;

    updateStdOsEnviron() catch |err| {
        print.printMessage("initEnviron(): cannot update std.os.environ: {}; ", .{err});
        return;
    };
    print.initLogLevel();

    print.printDebug("identified {s} libc loaded from {s}", .{ switch (libc_info.flavor) {
        types.LibCFlavor.GNU => "GNU",
        types.LibCFlavor.MUSL => "musl",
        else => "unknown",
    }, libc_info.name });

    const maybe_modified_resource_attributes = res_attrs.getModifiedOtelResourceAttributesValue(std.posix.getenv(res_attrs.otel_resource_attributes_env_var_name)) catch |err| {
        print.printMessage("cannot calculate modified OTEL_RESOURCE_ATTRIBUTES: {}", .{err});
        return;
    };

    if (maybe_modified_resource_attributes) |modified_resource_attributes| {
        print.printDebug(
            "setting {s}={s}",
            .{ res_attrs.otel_resource_attributes_env_var_name, modified_resource_attributes },
        );
        const setenv_res =
            libc_info.setenv_fn_ptr(
                res_attrs.otel_resource_attributes_env_var_name,
                modified_resource_attributes,
                true,
            );
        if (setenv_res != 0) {
            print.printMessage("failed to set modified value for '{s}' to '{s}': errno={}", .{ res_attrs.otel_resource_attributes_env_var_name, modified_resource_attributes, setenv_res });
        }
    }

    modifyEnvironmentVariable(libc_info.setenv_fn_ptr, node_js.node_options_env_var_name);
    modifyEnvironmentVariable(libc_info.setenv_fn_ptr, jvm.java_tool_options_env_var_name);
    if (dotnet.isEnabled()) {
        modifyEnvironmentVariable(libc_info.setenv_fn_ptr, dotnet.coreclr_enable_profiling_env_var_name);
        modifyEnvironmentVariable(libc_info.setenv_fn_ptr, dotnet.coreclr_profiler_env_var_name);
        modifyEnvironmentVariable(libc_info.setenv_fn_ptr, dotnet.coreclr_profiler_path_env_var_name);
        modifyEnvironmentVariable(libc_info.setenv_fn_ptr, dotnet.dotnet_additional_deps_env_var_name);
        modifyEnvironmentVariable(libc_info.setenv_fn_ptr, dotnet.dotnet_shared_store_env_var_name);
        modifyEnvironmentVariable(libc_info.setenv_fn_ptr, dotnet.dotnet_startup_hooks_env_var_name);
        modifyEnvironmentVariable(libc_info.setenv_fn_ptr, dotnet.otel_dotnet_auto_home_env_var_name);
    }
    print.printMessage("environment injection finished", .{});
}

fn updateStdOsEnviron() !void {
    // Dynamic libs do not get the std.os.environ initialized, see https://github.com/ziglang/zig/issues/4524, so we
    // back fill it. This logic is based on parsing of envp on zig's start. We re-bind the environment every time, as
    // we cannot ensure it did not change since the previous invocation. libc implementations can re-allocate the
    // environment (http://github.com/lattera/glibc/blob/master/stdlib/setenv.c;
    // https://git.musl-libc.org/cgit/musl/tree/src/env/setenv.c) if the backing memory location is outgrown by apps
    // modifying the environment via setenv or putenv.
    if (environ_ptr) |environment_ptr| {
        const env_array = environment_ptr.*;
        var env_var_count: usize = 0;
        // Note: env_array will be empty in some cases, for example if the application calls clearenv. Accessing
        // env_array[0] as we do in the while loop below would segfault. Instead we initialize an empty environ slice.
        if (env_array == 0) {
            std.os.environ = &.{};
            return;
        }
        while (env_array[env_var_count] != null) : (env_var_count += 1) {}

        std.os.environ = @ptrCast(@constCast(env_array[0..env_var_count]));
    } else {
        return error.CannotFindEnvironSymbol;
    }
}

fn modifyEnvironmentVariable(setenv_fn_ptr: types.SetenvFnPtr, name: [:0]const u8) void {
    if (getEnvValue(name)) |value| {
        print.printDebug(
            "setting {s}={s}",
            .{ name, value },
        );
        const setenv_res = setenv_fn_ptr(name, value, true);
        if (setenv_res != 0) {
            print.printMessage(
                "failed to set modified value for '{s}' to '{s}': errno={}",
                .{ name, value, setenv_res },
            );
        }
    }
}

fn getEnvValue(name: [:0]const u8) ?types.NullTerminatedString {
    const original_value = std.posix.getenv(name);

    if (std.mem.eql(u8, name, jvm.java_tool_options_env_var_name)) {
        return jvm.checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue(original_value);
    } else if (std.mem.eql(u8, name, node_js.node_options_env_var_name)) {
        return node_js.checkNodeJsOTelSdkDistributionAndGetModifiedNodeOptionsValue(original_value);
    } else if (std.mem.eql(u8, name, dotnet.coreclr_enable_profiling_env_var_name)) {
        if (dotnet.getDotnetValues()) |v| {
            return v.coreclr_enable_profiling;
        }
    } else if (std.mem.eql(u8, name, dotnet.coreclr_profiler_env_var_name)) {
        if (dotnet.getDotnetValues()) |v| {
            return v.coreclr_profiler;
        }
    } else if (std.mem.eql(u8, name, dotnet.coreclr_profiler_path_env_var_name)) {
        if (dotnet.getDotnetValues()) |v| {
            return v.coreclr_profiler_path;
        }
    } else if (std.mem.eql(u8, name, dotnet.dotnet_additional_deps_env_var_name)) {
        if (dotnet.getDotnetValues()) |v| {
            return v.additional_deps;
        }
    } else if (std.mem.eql(u8, name, dotnet.dotnet_shared_store_env_var_name)) {
        if (dotnet.getDotnetValues()) |v| {
            return v.shared_store;
        }
    } else if (std.mem.eql(u8, name, dotnet.dotnet_startup_hooks_env_var_name)) {
        if (dotnet.getDotnetValues()) |v| {
            return v.startup_hooks;
        }
    } else if (std.mem.eql(u8, name, dotnet.otel_dotnet_auto_home_env_var_name)) {
        if (dotnet.getDotnetValues()) |v| {
            return v.otel_auto_home;
        }
    }

    return null;
}
