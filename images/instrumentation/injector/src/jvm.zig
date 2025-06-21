// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const cache = @import("cache.zig");
const env = @import("env.zig");
const print = @import("print.zig");
const res_attrs = @import("resource_attributes.zig");
const test_util = @import("test_util.zig");
const types = @import("types.zig");

const testing = std.testing;

pub const java_tool_options_env_var_name = "JAVA_TOOL_OPTIONS";
pub const otel_java_agent_path = "/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar";
const javaagent_flag_value = "-javaagent:" ++ otel_java_agent_path;
const injection_happened_msg = "injecting the Java OpenTelemetry agent";

pub fn checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue(env_vars: [](types.NullTerminatedString)) ?types.EnvVarUpdate {
    const original_value_and_index_optional = env.getEnvVar(env_vars, java_tool_options_env_var_name);

    // Check the existence of the Jar file: by passing a `-javaagent` to a jar file that does not exist or cannot be
    // opened will crash the JVM.
    std.fs.cwd().access(otel_java_agent_path, .{}) catch |err| {
        print.printError("Skipping injection of OTel Java agent in 'JAVA_TOOL_OPTIONS' because of an issue accessing the Jar file at {s}: {}", .{ otel_java_agent_path, err });
        if (original_value_and_index_optional) |original_value_and_index| {
            cache.injector_cache.java_tool_options =
                cache.CachedEnvVarModification{
                    .value = original_value_and_index.value,
                    .done = true,
                };
            return types.EnvVarUpdate{
                .value = original_value_and_index.value,
                .replace = true,
                .index = original_value_and_index.index,
            };
        }
        cache.injector_cache.java_tool_options =
            cache.CachedEnvVarModification{
                .value = null,
                .done = true,
            };
        return null;
    };

    return getModifiedJavaToolOptionsValue(
        original_value_and_index_optional,
        res_attrs.getResourceAttributes(env_vars),
    );
}

test "checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue: should return null value if the Java agent cannot be accessed" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 2);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "AAA=bbb";
    original_env_vars[1] = "CCC=ddd";
    const env_var_update_optional = checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue(original_env_vars);
    try testing.expect(env_var_update_optional == null);

    try testing.expectEqual(true, cache.injector_cache.java_tool_options.done);
    try testing.expect(cache.injector_cache.java_tool_options.value == null);
}

test "checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue: should return the original value if the Java agent cannot be accessed" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 3);
    defer std.heap.page_allocator.free(original_env_vars);
    original_env_vars[0] = "AAA=bbb";
    original_env_vars[1] = "JAVA_TOOL_OPTIONS=original value";
    original_env_vars[2] = "CCC=ddd";
    const env_var_update = checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue(original_env_vars).?;
    try testing.expectEqualStrings(
        "original value",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(1, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("original value", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

test "checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue: should return -javaagent if original value is unset and the Java agent can be accessed" {
    try test_util.createDummyDirectory("/__dash0__/instrumentation/jvm/");
    _ = try std.fs.createFileAbsolute(otel_java_agent_path, .{});
    defer {
        test_util.deleteDash0DummyDirectory();
    }

    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_env_vars = try std.heap.page_allocator.alloc(types.NullTerminatedString, 0);
    defer std.heap.page_allocator.free(original_env_vars);
    const env_var_update = checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue(original_env_vars).?;
    try testing.expectEqualStrings(
        "-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(false, env_var_update.replace);
    try testing.expectEqual(0, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

/// Returns the modified value for JAVA_TOOL_OPTIONS, including the -javaagent flag; based on the original value of
/// JAVA_TOOL_OPTIONS and the provided resource attributes that should be added.
///
/// Do no deallocate the return value, or we may cause a USE_AFTER_FREE memory corruption in the parent process.
///
/// getModifiedJavaToolOptionsValue will free the new_resource_attributes_optional parameter if it is not null
fn getModifiedJavaToolOptionsValue(
    original_value_and_index_optional: ?types.EnvVarValueAndIndex,
    new_resource_attributes_optional: ?[]u8,
) ?types.EnvVarUpdate {
    // For auto-instrumentation, we inject the -javaagent flag into the JAVA_TOOL_OPTIONS environment variable. In
    // addition, we use JAVA_TOOL_OPTIONS to supply addtional resource attributes. The Java runtime does not look up the
    // OTEL_RESOURCE_ATTRIBUTES environment variable using getenv(), instead it parses the environment block
    // /proc/<pid>/environ directly. We cannot hook into this mechanism to introduce additional resource attributes.
    // Instead, we add them together with the -javaagent flag as the -Dotel.resource.attributes Java system property to
    // JAVA_TOOL_OPTIONS. If the -Dotel.resource.attributes system property already exists in the original
    // JAVA_TOOL_OPTIONS value, we need to merge the two list of key-value pairs. If -Dotel.resource.attributes is
    // supplied via other means (for example via the command line), the value from the command line will override the
    // value we add here to JAVA_TOOL_OPTIONS, which can be verified as follows:
    //     % JAVA_TOOL_OPTIONS="-Dprop=B" jshell -R -Dprop=A
    //     Picked up JAVA_TOOL_OPTIONS: -Dprop=B
    //     jshell> System.getProperty("prop")
    //     $1 ==> "A"
    if (original_value_and_index_optional) |original_value_and_index| {
        const original_value: []const u8 = std.mem.span(original_value_and_index.value);
        // If JAVA_TOOL_OPTIONS is already set, append our values.
        if (new_resource_attributes_optional) |new_resource_attributes| {
            defer std.heap.page_allocator.free(new_resource_attributes);

            if (std.mem.indexOf(u8, original_value, "-Dotel.resource.attributes=")) |startIdx| {
                // JAVA_TOOL_OPTIONS already contains -Dotel.resource.attribute, we need to merge the existing with the new key-value list.
                const actualStartIdx = startIdx + 27;
                // assume the list of key-value pairs is terminated by a space of the end of the string
                var originalKvPairs: []const u8 = "";
                var remainingJavaToolOptions: []const u8 = "";
                if (std.mem.indexOfPos(u8, original_value, actualStartIdx, " ")) |endIdx| {
                    originalKvPairs = original_value[actualStartIdx..endIdx];
                    remainingJavaToolOptions = original_value[endIdx..];
                } else {
                    originalKvPairs = original_value[actualStartIdx..];
                    remainingJavaToolOptions = "";
                }

                const mergedKvPairs = std.fmt.allocPrintZ(std.heap.page_allocator, "{s},{s}", .{
                    originalKvPairs,
                    new_resource_attributes,
                }) catch |err| {
                    print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ java_tool_options_env_var_name, err });
                    cache.injector_cache.java_tool_options =
                        cache.CachedEnvVarModification{
                            .value = original_value_and_index.value,
                            .done = true,
                        };
                    return types.EnvVarUpdate{
                        .value = original_value_and_index.value,
                        .replace = true,
                        .index = original_value_and_index.index,
                    };
                };
                defer std.heap.page_allocator.free(mergedKvPairs);
                const return_buffer =
                    std.fmt.allocPrintZ(std.heap.page_allocator, "{s}-Dotel.resource.attributes={s}{s} {s}", .{
                        original_value[0..startIdx],
                        mergedKvPairs,
                        remainingJavaToolOptions,
                        javaagent_flag_value,
                    }) catch |err| {
                        print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ java_tool_options_env_var_name, err });
                        cache.injector_cache.java_tool_options =
                            cache.CachedEnvVarModification{
                                .value = original_value_and_index.value,
                                .done = true,
                            };
                        return types.EnvVarUpdate{
                            .value = original_value_and_index.value,
                            .replace = true,
                            .index = original_value_and_index.index,
                        };
                    };
                print.printMessage(injection_happened_msg, .{});
                print.printMessage(res_attrs.modification_happened_msg, .{java_tool_options_env_var_name});
                cache.injector_cache.java_tool_options =
                    cache.CachedEnvVarModification{
                        .value = return_buffer.ptr,
                        .done = true,
                    };
                return types.EnvVarUpdate{
                    .value = return_buffer.ptr,
                    .replace = true,
                    .index = original_value_and_index.index,
                };
            } // end of `if (std.mem.indexOf(u8, original_value, "-Dotel.resource.attributes=")) |startIdx| {`

            // JAVA_TOOL_OPTIONS is set but does not already contain -Dotel.resource.attributes, and new resource
            // attributes have been provided. Add -javaagent and -Dotel.resource.attributes.
            const return_buffer =
                std.fmt.allocPrintZ(std.heap.page_allocator, "{s} {s} -Dotel.resource.attributes={s}", .{
                    original_value,
                    javaagent_flag_value,
                    new_resource_attributes,
                }) catch |err| {
                    print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ java_tool_options_env_var_name, err });
                    cache.injector_cache.java_tool_options =
                        cache.CachedEnvVarModification{
                            .value = original_value_and_index.value,
                            .done = true,
                        };
                    return types.EnvVarUpdate{
                        .value = original_value_and_index.value,
                        .replace = true,
                        .index = original_value_and_index.index,
                    };
                };
            print.printMessage(res_attrs.modification_happened_msg, .{java_tool_options_env_var_name});
            print.printMessage(injection_happened_msg, .{});
            cache.injector_cache.java_tool_options =
                cache.CachedEnvVarModification{
                    .value = return_buffer.ptr,
                    .done = true,
                };
            return types.EnvVarUpdate{
                .value = return_buffer.ptr,
                .replace = true,
                .index = original_value_and_index.index,
            };
        } else { // this else belongs to `if (new_resource_attributes_optional) |new_resource_attributes| {`
            // JAVA_TOOL_OPTIONS is set, but no new resource attributes have been provided.
            const return_buffer =
                std.fmt.allocPrintZ(std.heap.page_allocator, "{s} {s}", .{
                    original_value,
                    javaagent_flag_value,
                }) catch |err| {
                    print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ java_tool_options_env_var_name, err });
                    cache.injector_cache.java_tool_options =
                        cache.CachedEnvVarModification{
                            .value = original_value_and_index.value,
                            .done = true,
                        };
                    return types.EnvVarUpdate{
                        .value = original_value_and_index.value,
                        .replace = true,
                        .index = original_value_and_index.index,
                    };
                };
            print.printMessage(injection_happened_msg, .{});
            cache.injector_cache.java_tool_options =
                cache.CachedEnvVarModification{
                    .value = return_buffer.ptr,
                    .done = true,
                };
            return types.EnvVarUpdate{
                .value = return_buffer.ptr,
                .replace = true,
                .index = original_value_and_index.index,
            };
        }
    } else {
        // JAVA_TOOL_OPTIONS is not set, but new resource attributes have been provided.
        if (new_resource_attributes_optional) |new_resource_attributes| {
            defer std.heap.page_allocator.free(new_resource_attributes);
            const return_buffer = std.fmt.allocPrintZ(std.heap.page_allocator, "{s} -Dotel.resource.attributes={s}", .{
                javaagent_flag_value,
                new_resource_attributes,
            }) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ java_tool_options_env_var_name, err });
                cache.injector_cache.java_tool_options =
                    cache.CachedEnvVarModification{
                        .value = null,
                        .done = true,
                    };
                return null;
            };
            print.printMessage(res_attrs.modification_happened_msg, .{java_tool_options_env_var_name});
            print.printMessage(injection_happened_msg, .{});
            cache.injector_cache.java_tool_options =
                cache.CachedEnvVarModification{
                    .value = return_buffer.ptr,
                    .done = true,
                };
            return types.EnvVarUpdate{
                .value = return_buffer.ptr,
                .replace = false,
                .index = 0,
            };
        } else {
            // JAVA_TOOL_OPTIONS is not set, and no new resource attributes have been provided. Simply return the -javaagent flag.
            print.printMessage(injection_happened_msg, .{});
            cache.injector_cache.java_tool_options =
                cache.CachedEnvVarModification{
                    .value = javaagent_flag_value[0..].ptr,
                    .done = true,
                };
            return types.EnvVarUpdate{
                .value = javaagent_flag_value[0..].ptr,
                .replace = false,
                .index = 0,
            };
        }
    }
}

test "getModifiedJavaToolOptionsValue: should return -javaagent if original value is unset and no resource attributes are provided" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const env_var_update = getModifiedJavaToolOptionsValue(null, null).?;
    try testing.expectEqualStrings(
        "-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(false, env_var_update.replace);
    try testing.expectEqual(0, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

test "getModifiedJavaToolOptionsValue: should return -javaagent and -Dotel.resource.attributes if original value is unset and resource attributes are provided" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const resource_attributes: []u8 = try std.heap.page_allocator.alloc(u8, 15);
    var fbs = std.io.fixedBufferStream(resource_attributes);
    _ = try fbs.writer().write("aaa=bbb,ccc=ddd");
    const env_var_update = getModifiedJavaToolOptionsValue(
        null,
        resource_attributes,
    ).?;
    try testing.expectEqualStrings(
        "-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dotel.resource.attributes=aaa=bbb,ccc=ddd",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(false, env_var_update.replace);
    try testing.expectEqual(0, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dotel.resource.attributes=aaa=bbb,ccc=ddd", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

test "getModifiedJavaToolOptionsValue: should append -javaagent if original value exists and no resource attributes are provided" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_value = types.EnvVarValueAndIndex{
        .value = "-Dsome.property=value"[0.. :0],
        .index = 3,
    };
    const env_var_update = getModifiedJavaToolOptionsValue(
        original_value,
        null,
    ).?;
    try testing.expectEqualStrings(
        "-Dsome.property=value -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(3, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("-Dsome.property=value -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

test "getModifiedJavaToolOptionsValue: should append -javaagent if original value exists and has -Dotel.resource.attributes but no new resource attributes are provided, " {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_value = types.EnvVarValueAndIndex{
        .value = "-Dsome.property=value -Dotel.resource.attributes=www=xxx,yyy=zzz"[0.. :0],
        .index = 3,
    };
    const env_var_update = getModifiedJavaToolOptionsValue(
        original_value,
        null,
    ).?;
    try testing.expectEqualStrings(
        "-Dsome.property=value -Dotel.resource.attributes=www=xxx,yyy=zzz -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(3, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("-Dsome.property=value -Dotel.resource.attributes=www=xxx,yyy=zzz -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

test "getModifiedJavaToolOptionsValue: should append -javaagent and -Dotel.resource.attributes if original value exists and resource attributes are provided" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_value = types.EnvVarValueAndIndex{
        .value = "-Dsome.property=value"[0.. :0],
        .index = 3,
    };
    const resource_attributes: []u8 = try std.heap.page_allocator.alloc(u8, 15);
    var fbs = std.io.fixedBufferStream(resource_attributes);
    _ = try fbs.writer().write("aaa=bbb,ccc=ddd");
    const env_var_update = getModifiedJavaToolOptionsValue(
        original_value,
        resource_attributes,
    ).?;
    try testing.expectEqualStrings(
        "-Dsome.property=value -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dotel.resource.attributes=aaa=bbb,ccc=ddd",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(3, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("-Dsome.property=value -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dotel.resource.attributes=aaa=bbb,ccc=ddd", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

test "getModifiedJavaToolOptionsValue: should merge new and existing -Dotel.resource.attributes (only property)" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_value = types.EnvVarValueAndIndex{
        .value = "-Dotel.resource.attributes=eee=fff,ggg=hhh"[0.. :0],
        .index = 3,
    };
    const resource_attributes: []u8 = try std.heap.page_allocator.alloc(u8, 15);
    var fbs = std.io.fixedBufferStream(resource_attributes);
    _ = try fbs.writer().write("aaa=bbb,ccc=ddd");
    const env_var_update = getModifiedJavaToolOptionsValue(
        original_value,
        resource_attributes,
    ).?;
    try testing.expectEqualStrings(
        "-Dotel.resource.attributes=eee=fff,ggg=hhh,aaa=bbb,ccc=ddd -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(3, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("-Dotel.resource.attributes=eee=fff,ggg=hhh,aaa=bbb,ccc=ddd -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

test "getModifiedJavaToolOptionsValue: should merge new and existing -Dotel.resource.attributes (at the start)" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_value = types.EnvVarValueAndIndex{
        .value = "-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh"[0.. :0],
        .index = 3,
    };
    const resource_attributes: []u8 = try std.heap.page_allocator.alloc(u8, 15);
    var fbs = std.io.fixedBufferStream(resource_attributes);
    _ = try fbs.writer().write("aaa=bbb,ccc=ddd");
    const env_var_update = getModifiedJavaToolOptionsValue(
        original_value,
        resource_attributes,
    ).?;
    try testing.expectEqualStrings(
        "-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh,aaa=bbb,ccc=ddd -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(3, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh,aaa=bbb,ccc=ddd -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

test "getModifiedJavaToolOptionsValue: should merge new and existing -Dotel.resource.attributes (in the middle)" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_value = types.EnvVarValueAndIndex{
        .value = "-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh -Dproperty2=value"[0.. :0],
        .index = 3,
    };
    const resource_attributes: []u8 = try std.heap.page_allocator.alloc(u8, 15);
    var fbs = std.io.fixedBufferStream(resource_attributes);
    _ = try fbs.writer().write("aaa=bbb,ccc=ddd");
    const env_var_update = getModifiedJavaToolOptionsValue(
        original_value,
        resource_attributes,
    ).?;
    try testing.expectEqualStrings(
        "-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh,aaa=bbb,ccc=ddd -Dproperty2=value -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(3, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh,aaa=bbb,ccc=ddd -Dproperty2=value -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}

test "getModifiedJavaToolOptionsValue: should merge new and existing -Dotel.resource.attributes (at the end)" {
    cache.injector_cache = cache.emptyInjectorCache();
    defer cache.injector_cache = cache.emptyInjectorCache();

    const original_value = types.EnvVarValueAndIndex{
        .value = "-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh"[0.. :0],
        .index = 3,
    };
    const resource_attributes: []u8 = try std.heap.page_allocator.alloc(u8, 15);
    var fbs = std.io.fixedBufferStream(resource_attributes);
    _ = try fbs.writer().write("aaa=bbb,ccc=ddd");
    const env_var_update = getModifiedJavaToolOptionsValue(
        original_value,
        resource_attributes,
    ).?;
    try testing.expectEqualStrings(
        "-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh,aaa=bbb,ccc=ddd -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(env_var_update.value),
    );
    try testing.expectEqual(true, env_var_update.replace);
    try testing.expectEqual(3, env_var_update.index);

    try testing.expectEqual(true, cache.injector_cache.java_tool_options.done);
    try testing.expectEqualStrings("-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh,aaa=bbb,ccc=ddd -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar", std.mem.span(cache.injector_cache.java_tool_options.value.?));
}
