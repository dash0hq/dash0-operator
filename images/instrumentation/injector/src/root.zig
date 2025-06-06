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
    const pid = std.os.linux.getpid();
    std.debug.print("[Dash0 injector] {d} initEnviron() start\n", .{pid});
    _initEnviron() catch @panic("[Dash0 injector] initEnviron failed");
    std.debug.print("[Dash0 injector] {d} initEnviron() done\n", .{pid});
}

// TODO add unit tests for _initEnviron.

// TODO This function must be idempotent, as parent processes may pass the environment to child processes, compounding
// our modification with each nested child process start. Or add a marker env var.
fn _initEnviron() !void {
    var env_vars, const otel_resource_attributes_env_var_found, const otel_resource_attributes_env_var_index = try readProcSelfEnviron();
    defer env_vars.deinit();
    try applyModifications(env_vars, otel_resource_attributes_env_var_found, otel_resource_attributes_env_var_index);
}

fn readProcSelfEnviron() !struct { *std.ArrayList(types.NullTerminatedString), bool, usize } {
    const proc_self_environ_path = "/proc/self/environ";
    const proc_self_environ_file = try std.fs.openFileAbsolute(proc_self_environ_path, .{
        .mode = std.fs.File.OpenMode.read_only,
        .lock = std.fs.File.Lock.none,
    });
    defer proc_self_environ_file.close();

    // IMPORTANT! /proc/self/environ skips the final \x00 terminator
    // TODO Fix max size
    // root@9fc29beca780:/proc/self# hexdump -C environ
    // 00000000  50 41 54 48 3d 2f 75 73  72 2f 6c 6f 63 61 6c 2f  |PATH=/usr/local/|
    // 00000010  73 62 69 6e 3a 2f 75 73  72 2f 6c 6f 63 61 6c 2f  |sbin:/usr/local/|
    // 00000020  62 69 6e 3a 2f 75 73 72  2f 73 62 69 6e 3a 2f 75  |bin:/usr/sbin:/u|
    // 00000030  73 72 2f 62 69 6e 3a 2f  73 62 69 6e 3a 2f 62 69  |sr/bin:/sbin:/bi|
    // 00000040  6e 3a 2f 6f 70 74 2f 7a  69 67 00 48 4f 53 54 4e  |n:/opt/zig.HOSTN|
    // 00000050  41 4d 45 3d 39 66 63 32  39 62 65 63 61 37 38 30  |AME=9fc29beca780|
    // 00000060  00 54 45 52 4d 3d 78 74  65 72 6d 00 4c 41 4e 47  |.TERM=xterm.LANG|
    // 00000070  3d 65 6e 5f 55 53 2e 75  74 66 38 00 48 4f 4d 45  |=en_US.utf8.HOME|
    // 00000080  3d 2f 72 6f 6f 74 00                              |=/root.|
    // 00000087
    // root@9fc29beca780:/proc/self#
    const environ_buffer_original = try proc_self_environ_file.readToEndAlloc(std.heap.page_allocator, std.math.maxInt(usize));
    // TODO ??? defer std.heap.page_allocator.free(environ_buffer_original);

    // TODO use for unit tests for reading original env vars
    // --{ 80, 65, 84, 72, 61, 47, 117, 115, 114, 47, 108, 111, 99, 97, 108, 47, 111, 112, 101, 110, 106, 100, 107, 45, 50, 52, 47, 98, 105, 110, 58, 47, 117, 115, 114, 47, 108, 111, 99, 97, 108, 47, 115, 98, 105, 110, 58, 47, 117, 115, 114, 47, 108, 111, 99, 97, 108, 47, 98, 105, 110, 58, 47, 117, 115, 114, 47, 115, 98, 105, 110, 58, 47, 117, 115, 114, 47, 98, 105, 110, 58, 47, 115, 98, 105, 110, 58, 47, 98, 105, 110, 0, 72, 79, 83, 84, 78, 65, 77, 69, 61, 56, 99, 55, 53, 99, 100, 52, 49, 100, 50, 97, 53, 0, 68, 65, 83, 72, 48, 95, 78, 65, 77, 69, 83, 80, 65, 67, 69, 95, 78, 65, 77, 69, 61, 110, 97, 109, 101, 115, 112, 97, 99, 101, 0, 68, 65, 83, 72, 48, 95, 80, 79, 68, 95, 85, 73, 68, 61, 112, 111, 100, 95, 117, 105, 100, 0, 68, 65, 83, 72, 48, 95, 80, 79, 68, 95, 78, 65, 77, 69, 61, 112, 111, 100, 95, 110, 97, 109, 101, 0, 68, 65, 83, 72, 48, 95, 67, 79, 78, 84, 65, 73, 78, 69, 82, 95, 78, 65, 77, 69, 61, 99, 111, 110, 116, 97, 105, 110, 101, 114, 95, 110, 97, 109, 101, 0, 79, 84, 69, 76, 95, 76, 79, 71, 83, 95, 69, 88, 80, 79, 82, 84, 69, 82, 61, 110, 111, 110, 101, 0, 79, 84, 69, 76, 95, 77, 69, 84, 82, 73, 67, 83, 95, 69, 88, 80, 79, 82, 84, 69, 82, 61, 110, 111, 110, 101, 0, 79, 84, 69, 76, 95, 84, 82, 65, 67, 69, 83, 95, 69, 88, 80, 79, 82, 84, 69, 82, 61, 110, 111, 110, 101, 0, 74, 65, 86, 65, 95, 72, 79, 77, 69, 61, 47, 117, 115, 114, 47, 108, 111, 99, 97, 108, 47, 111, 112, 101, 110, 106, 100, 107, 45, 50, 52, 0, 76, 65, 78, 71, 61, 67, 46, 85, 84, 70, 45, 56, 0, 74, 65, 86, 65, 95, 86, 69, 82, 83, 73, 79, 78, 61, 50, 52, 0, 76, 68, 95, 80, 82, 69, 76, 79, 65, 68, 61, 47, 95, 95, 100, 97, 115, 104, 48, 95, 95, 47, 100, 97, 115, 104, 48, 95, 105, 110, 106, 101, 99, 116, 111, 114, 46, 115, 111, 0, 72, 79, 77, 69, 61, 47, 114, 111, 111, 116, 0 }---
    // std.debug.print("\n\n---{d}---\n\n", .{environ_buffer_original});
    // ---PATH=/usr/local/openjdk-24/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/binHOSTNAME=8c75cd41d2a5DASH0_NAMESPACE_NAME=namespaceDASH0_POD_UID=pod_uidDASH0_POD_NAME=pod_nameDASH0_CONTAINER_NAME=container_nameOTEL_LOGS_EXPORTER=noneOTEL_METRICS_EXPORTER=noneOTEL_TRACES_EXPORTER=noneJAVA_HOME=/usr/local/openjdk-24LANG=C.UTF-8JAVA_VERSION=24LD_PRELOAD=/__dash0__/dash0_injector.soHOME=/root---
    // std.debug.print("\n\n---{s}---\n\n", .{environ_buffer_original});

    var env_vars = std.ArrayList(types.NullTerminatedString).init(std.heap.page_allocator);

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

                    if (std.mem.indexOf(u8, environ_buffer_original, "OTEL_RESOURCE_ATTRIBUTES=")) |j| {
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

    return .{ &env_vars, otel_resource_attributes_env_var_found, otel_resource_attributes_env_var_index };
}

fn applyModifications(env_vars: *std.ArrayList(types.NullTerminatedString), otel_resource_attributes_env_var_found: bool, otel_resource_attributes_env_var_index: usize) !void {
    if (!otel_resource_attributes_env_var_found) {
        std.debug.print("[Dash0 injector] potentially appending OTEL_RESOURCE_ATTRIBUTES as the last env var\n", .{});
        if (getModifiedOtelResourceAttributesValue(env_vars)) |resource_attributes| {
            std.debug.print("[Dash0 injector] getModifiedOtelResourceAttributesValue has returned values: {s}\n", .{resource_attributes});
            try env_vars.append(resource_attributes[0..]);
        } else {
            std.debug.print("[Dash0 injector] getModifiedOtelResourceAttributesValue has not returned any values, not appending OTEL_RESOURCE_ATTRIBUTES\n", .{});
        }
    } else {
        std.debug.print("[Dash0 injector] OTEL_RESOURCE_ATTRIBUTES exists, potentially overwriting current value\n", .{});
        if (getModifiedOtelResourceAttributesValue(env_vars)) |resource_attributes| {
            std.debug.print("[Dash0 injector] getModifiedOtelResourceAttributesValue has returned values: {s}\n", .{resource_attributes});
            env_vars.items[otel_resource_attributes_env_var_index] = resource_attributes[0..];
        } else {
            std.debug.print("[Dash0 injector] getModifiedOtelResourceAttributesValue has not returned any values, not overwriting OTEL_RESOURCE_ATTRIBUTES\n", .{});
        }
    }

    // TODO this is nonsense? Should be terminated by a null pointer, not by a null character.
    try env_vars.append(empty_env_var);

    const env_var_slices = try env_vars.toOwnedSlice();
    const env_var_count = env_var_slices.len;

    environ_buffer = try std.heap.page_allocator.alloc(types.NullTerminatedString, env_var_count);

    // TODO make sure the last pointer in environ_buffer is the NULL pointer
    for (env_var_slices, 0..) |env_var, i| {
        // We copy the env var slice to the environ_buffer, so that we can modify it later.
        // Note: We do not need to copy the final null terminator, as it is already there.
        environ_buffer[i] = env_var;
    }

    __environ = @ptrCast(environ_buffer);
    _environ = __environ;
    environ = __environ;

    std.debug.print("[Dash0 injector] {d} _initEnviron() done\n", .{std.os.linux.getpid()});
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
pub fn getModifiedOtelResourceAttributesValue(env_vars: *std.ArrayList(types.NullTerminatedString)) ?types.NullTerminatedString {
    if (modified_otel_resource_attributes_value_calculated) {
        std.debug.print("getModifiedOtelResourceAttributesValue: OTEL_RESOURCE_ATTRIBUTES already modified\n", .{});
        // We have already calculated the value, so we can return it.
        return modified_otel_resource_attributes_value;
    }

    std.debug.print("[Dash0 injector] getModifiedOtelResourceAttributesValue: OTEL_RESOURCE_ATTRIBUTES not modified yet\n", .{});

    const original_value_optional, _ = getEnvVar(env_vars, "OTEL_RESOURCE_ATTRIBUTES");
    const resource_attributes_optional = getResourceAttributes(env_vars);
    if (original_value_optional) |original_value| {
        if (resource_attributes_optional) |resource_attributes| {
            defer std.heap.page_allocator.free(resource_attributes);

            std.debug.print("getModifiedOtelResourceAttributesValue: original value: {s}\n", .{original_value});

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

            std.debug.print("getModifiedOtelResourceAttributesValue: both original value and new values to add are present\n", .{});
            return modified_otel_resource_attributes_value;
        } else {
            std.debug.print("getModifiedOtelResourceAttributesValue: original value: {s}\n", .{original_value});

            // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
            // parent process.
            const return_buffer = std.fmt.allocPrintZ(std.heap.page_allocator, "{s}={s}", .{ otel_resource_attributes_env_var_name, original_value }) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ otel_resource_attributes_env_var_name, err });
                return original_value;
            };

            modified_otel_resource_attributes_value = return_buffer.ptr;
            modified_otel_resource_attributes_value_calculated = true;

            std.debug.print("getModifiedOtelResourceAttributesValue: original value, nothing to add\n", .{});
            return modified_otel_resource_attributes_value;
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

            std.debug.print("getModifiedOtelResourceAttributesValue: no original value, but new values to add are present, returning {any}\n", .{modified_otel_resource_attributes_value});
            return modified_otel_resource_attributes_value;
        } else {
            // There is no original value, and also nothing to add, return null.
            modified_otel_resource_attributes_value_calculated = true;
            std.debug.print("getModifiedOtelResourceAttributesValue: no original, nothing to add\n", .{});
            return null;
        }
    }
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
