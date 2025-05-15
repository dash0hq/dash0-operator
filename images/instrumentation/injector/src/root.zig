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

// TODO switch to local var again
var environ_idx: usize = 0;

// TODO maybe initialize the initial dummy __environ in a less stupid way :)
// TODO actually, use heap memory here.
// TODO init with a small dummy value here, overwrite in _initEnviron
var environ_buffer: [131072]u8 = [_]u8{0} ** 131072;
const environ_buffer_ptr: *[131072]u8 = &environ_buffer;

// Export __environ as a strong symbol, to override the glibc/musl symbol.
export var __environ: [*]u8 = environ_buffer_ptr;
// export var __my_environ: [*]u8 = environ_buffer_ptr;

// Also declare (and thus override) the aliases _environ and environ, see e.g.
// https://sourceware.org/git/?p=glibc.git;a=blob;f=posix/environ.c
// export var _environ: [*]u8 = __environ;
// export var environ: [*]u8 = __environ;

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
    std.debug.print("initEnviron() START\n", .{});
    _initEnviron() catch @panic("[Dash0 injector] initEnviron failed");
    std.debug.print("initEnviron() END\n", .{});
}

fn _initEnviron() !void {
    // std.debug.print("root.zig#_initEnviron() START\n", .{});
    // std.debug.print("root.zig#_initEnviron: XXX my_exported pointer: {x}\n", .{@intFromPtr(my_exported)});

    const proc_self_environ_path = "/proc/self/environ";
    const proc_self_environ_file = try std.fs.openFileAbsolute(proc_self_environ_path, .{
        .mode = std.fs.File.OpenMode.read_only,
        .lock = std.fs.File.Lock.none,
    });
    defer proc_self_environ_file.close();
    // std.debug.print("root.zig#_initEnviron() file open done\n", .{});

    // For now, we allocate heap memory for reading /proc/self/environ. We might want to optimize this later.
    // StackFallbackAllocator might be a good convenient option.
    var buf_reader = std.io.bufferedReader(proc_self_environ_file.reader());
    var in_stream = buf_reader.reader();
    // std.debug.print("root.zig#_initEnviron() init in_stream done\n", .{});

    // We have not seen OTEL_RESOURCE_ATTRIBUTES /proc/self/environ, add it now.
    for (res_attrs.otel_resource_attributes_env_var_name) |c| {
        environ_buffer[environ_idx] = c;
        environ_idx += 1;
    }
    environ_buffer[environ_idx] = '=';
    environ_idx += 1;
    for ("k8s.namespace.name=my-namespace,k8s.pod.name=my-pod,k8s.pod.uid=275ecb36-5aa8-4c2a-9c47-d8bb681b9aff,k8s.container.name=test-app") |c| {
        environ_buffer[environ_idx] = c;
        environ_idx += 1;
    }
    environ_buffer[environ_idx] = 0;
    environ_idx += 1;
    // std.debug.print("root.zig#_initEnviron() OTEL_RESOURCE_ATTRIBUTES has been added\n", .{});

    var init_arena = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer init_arena.deinit();
    const init_allocator = init_arena.allocator();
    // std.debug.print("root.zig#_initEnviron() init init_allocator done\n", .{});
    // TODO max_size? We currently set std.math.maxInt(usize) which basically means "unlimited", we might want to set
    // an (arbitrary but reasonable) limit here?
    // TODO switch to local var again var environ_idx: usize = 0;
    while (try in_stream.readUntilDelimiterOrEofAlloc(init_allocator, 0, std.math.maxInt(usize))) |environ_entry| {
        // std.debug.print("root.zig#_initEnviron: read environ entry: {s}\n", .{environ_entry});
        for (environ_entry) |c| {
            environ_buffer[environ_idx] = c;
            environ_idx += 1;
        }
        // add null terminator for this environ entry
        environ_buffer[environ_idx] = 0;
        environ_idx += 1;
    }
    // std.debug.print("root.zig#_initEnviron() /proc/self/environ has been read once\n", .{});

    // Add final null terminator to signify the end of __environ.
    environ_buffer[environ_idx] = 0;
    environ_idx += 1;

    std.debug.print("root.zig#_initEnviron: XXX __environ outer pointer: {x}\n", .{@intFromPtr(__environ)});
    std.debug.print("root.zig#_initEnviron: XXX environ_buffer: {s}\n", .{ environ_buffer[0..environ_idx] });
    // std.debug.print("root.zig#_initEnviron: XXX __my_environ outer pointer: {x}\n", .{@intFromPtr(__my_environ)});
    // std.debug.print("root.zig#_initEnviron: XXX __my_environ inner pointer: {x}\n", .{@intFromPtr(my_environ_buffer_ptr)});
    // std.debug.print("root.zig#_initEnviron: XXX my_environ_buffer: {s}\n", .{ my_environ_buffer[0..environ_idx] });
    // std.debug.print("root.zig#_initEnviron() END\n", .{});
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
