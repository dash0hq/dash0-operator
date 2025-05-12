// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const alloc = @import("allocator.zig");
const print = @import("print.zig");
const res_attrs = @import("resource_attributes.zig");
const types = @import("types.zig");

const testing = std.testing;

pub const java_tool_options_env_var_name = "JAVA_TOOL_OPTIONS";
const otel_java_agent_path = "/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar";
const javaagent_flag_value = "-javaagent:" ++ otel_java_agent_path;

pub fn checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue(original_value_optional: ?[:0]const u8) ?types.NullTerminatedString {
    // Check the existence of the Jar file: by passing a `-javaagent` to a
    // jar file that does not exist or cannot be opened will crash the JVM
    std.fs.cwd().access(otel_java_agent_path, .{}) catch |err| {
        print.printError("Skipping injection of OTel Java agent in 'JAVA_TOOL_OPTIONS' because of an issue accessing the Jar file at {s}: {}", .{ otel_java_agent_path, err });
        if (original_value_optional) |original_value| {
            return original_value;
        }
        return null;
    };

    const resource_attributes_optional: ?[]u8 = res_attrs.getResourceAttributes();
    return getModifiedJavaToolOptionsValue(
        original_value_optional,
        resource_attributes_optional,
    );
}

test "checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue: should return null value if the Java agent cannot be accessed" {
    const modifiedJavaToolOptions = checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue(null);
    try testing.expect(modifiedJavaToolOptions == null);
}

test "checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue: should return the original value if the Java agent cannot be accessed" {
    const modifiedJavaToolOptions = checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue("original value");
    try testing.expectEqualStrings(
        "original value",
        std.mem.span(modifiedJavaToolOptions orelse "-"),
    );
}

/// Returns the modified value for JAVA_TOOL_OPTIONS, including the -javaagent flag; based on the original value of
/// JAVA_TOOL_OPTIONS and the provided resource attributes that should be added.
///
/// Do no deallocate the return value, or we may cause a USE_AFTER_FREE memory corruption in the parent process.
///
/// getModifiedJavaToolOptionsValue will free the new_resource_attributes_optional parameter if it is not null
fn getModifiedJavaToolOptionsValue(
    original_value_optional: ?[:0]const u8,
    new_resource_attributes_optional: ?[]u8,
) ?types.NullTerminatedString {
    // For auto-instrumentation, we inject the -javaagent flag into the JAVA_TOOL_OPTIONS environment variable. In
    // addition, we use JAVA_TOOL_OPTIONS to supply addtional resource attributes. The Java runtime does not look up the
    // OTEL_RESOURCE_ATTRIBUTES environment variable using getenv(), instead it parses the environment block
    // /proc/<pid>/environ directly. We cannot hook into mechanism to introduce additional resource attributes. Instead, we
    // add them together with the -javaagent flag as the -Dotel.resource.attributes Java system property to
    // JAVA_TOOL_OPTIONS. If the -Dotel.resource.attributes system property already exists in the original
    // JAVA_TOOL_OPTIONS value, we need to merge the two list of key-value pairs. If -Dotel.resource.attributes is
    // supplied via other means (for example via the command line), the value from the command line will override the
    // value we add here to JAVA_TOOL_OPTIONS, which can be verified as follows:
    //     % JAVA_TOOL_OPTIONS="-Dprop=B" jshell -R -Dprop=A
    //     Picked up JAVA_TOOL_OPTIONS: -Dprop=B
    //     jshell> System.getProperty("prop")
    //     $1 ==> "A"
    if (original_value_optional) |original_value| {
        // If JAVA_TOOL_OPTIONS is already set, append our values.
        if (new_resource_attributes_optional) |new_resource_attributes| {
            defer alloc.page_allocator.free(new_resource_attributes);

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

                const mergedKvPairs = std.fmt.allocPrintZ(alloc.page_allocator, "{s},{s}", .{
                    originalKvPairs,
                    new_resource_attributes,
                }) catch |err| {
                    print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ java_tool_options_env_var_name, err });
                    return original_value;
                };
                defer alloc.page_allocator.free(mergedKvPairs);
                const return_buffer =
                    std.fmt.allocPrintZ(alloc.page_allocator, "{s}-Dotel.resource.attributes={s}{s} {s}", .{
                        original_value[0..startIdx],
                        mergedKvPairs,
                        remainingJavaToolOptions,
                        javaagent_flag_value,
                    }) catch |err| {
                        print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ java_tool_options_env_var_name, err });
                        return original_value;
                    };
                return return_buffer.ptr;
            }

            // JAVA_TOOL_OPTIONS is set but does not contain does not already contain -Dotel.resource.attributes, and
            // new resource attributes have been provided. Add -javaagent and -Dotel.resource.attributes.
            const return_buffer =
                std.fmt.allocPrintZ(alloc.page_allocator, "{s} {s} -Dotel.resource.attributes={s}", .{
                    original_value,
                    javaagent_flag_value,
                    new_resource_attributes,
                }) catch |err| {
                    print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ java_tool_options_env_var_name, err });
                    return original_value;
                };
            return return_buffer.ptr;
        } else {
            // JAVA_TOOL_OPTIONS is set, but no new resource attributes have been provided.
            const return_buffer =
                std.fmt.allocPrintZ(alloc.page_allocator, "{s} {s}", .{
                    original_value,
                    javaagent_flag_value,
                }) catch |err| {
                    print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ java_tool_options_env_var_name, err });
                    return original_value;
                };
            return return_buffer.ptr;
        }
    } else {
        // JAVA_TOOL_OPTIONS is not set, but new resource attributes have been provided.
        if (new_resource_attributes_optional) |new_resource_attributes| {
            defer alloc.page_allocator.free(new_resource_attributes);
            const return_buffer = std.fmt.allocPrintZ(alloc.page_allocator, "{s} -Dotel.resource.attributes={s}", .{
                javaagent_flag_value,
                new_resource_attributes,
            }) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ java_tool_options_env_var_name, err });
                return null;
            };
            return return_buffer.ptr;
        } else {
            // JAVA_TOOL_OPTIONS is not set, and no new resource attributes have been provided. Simply return the -javaagent flag.
            return javaagent_flag_value[0..].ptr;
        }
    }
}

test "getModifiedJavaToolOptionsValue: should return -javaagent if original value is unset and no resource attributes are provided" {
    const modifiedJavaToolOptions = getModifiedJavaToolOptionsValue(
        null,
        null,
    );
    try testing.expectEqualStrings(
        "-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(modifiedJavaToolOptions orelse "-"),
    );
}

test "getModifiedJavaToolOptionsValue: should return -javaagent and -Dotel.resource.attributes if original value is unset and resource attributes are provided" {
    const resource_attributes: []u8 = try alloc.page_allocator.alloc(u8, 15);
    var fbs = std.io.fixedBufferStream(resource_attributes);
    _ = try fbs.writer().write("aaa=bbb,ccc=ddd");
    const modifiedJavaToolOptions = getModifiedJavaToolOptionsValue(
        null,
        resource_attributes,
    );
    try testing.expectEqualStrings(
        "-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dotel.resource.attributes=aaa=bbb,ccc=ddd",
        std.mem.span(modifiedJavaToolOptions orelse "-"),
    );
}

test "getModifiedJavaToolOptionsValue: should append -javaagent if original value exists and no resource attributes are provided" {
    const original_value: [:0]const u8 = "-Dsome.property=value"[0.. :0];
    const modifiedJavaToolOptions = getModifiedJavaToolOptionsValue(
        original_value,
        null,
    );
    try testing.expectEqualStrings(
        "-Dsome.property=value -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(modifiedJavaToolOptions orelse "-"),
    );
}

test "getModifiedJavaToolOptionsValue: should append -javaagent if original value exists and has -Dotel.resource.attributes but no new resource attributes are provided, " {
    const original_value: [:0]const u8 = "-Dsome.property=value -Dotel.resource.attributes=www=xxx,yyy=zzz"[0.. :0];
    const modifiedJavaToolOptions = getModifiedJavaToolOptionsValue(
        original_value,
        null,
    );
    try testing.expectEqualStrings(
        "-Dsome.property=value -Dotel.resource.attributes=www=xxx,yyy=zzz -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(modifiedJavaToolOptions orelse "-"),
    );
}

test "getModifiedJavaToolOptionsValue: should append -javaagent and -Dotel.resource.attributes if original value exists and resource attributes are provided" {
    const original_value: [:0]const u8 = "-Dsome.property=value"[0.. :0];
    const resource_attributes: []u8 = try alloc.page_allocator.alloc(u8, 15);
    var fbs = std.io.fixedBufferStream(resource_attributes);
    _ = try fbs.writer().write("aaa=bbb,ccc=ddd");
    const modifiedJavaToolOptions = getModifiedJavaToolOptionsValue(
        original_value,
        resource_attributes,
    );
    try testing.expectEqualStrings(
        "-Dsome.property=value -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dotel.resource.attributes=aaa=bbb,ccc=ddd",
        std.mem.span(modifiedJavaToolOptions orelse "-"),
    );
}

test "getModifiedJavaToolOptionsValue: should merge new and existing -Dotel.resource.attributes (only property)" {
    const original_value: [:0]const u8 = "-Dotel.resource.attributes=eee=fff,ggg=hhh"[0.. :0];
    const resource_attributes: []u8 = try alloc.page_allocator.alloc(u8, 15);
    var fbs = std.io.fixedBufferStream(resource_attributes);
    _ = try fbs.writer().write("aaa=bbb,ccc=ddd");
    const modifiedJavaToolOptions = getModifiedJavaToolOptionsValue(
        original_value,
        resource_attributes,
    );
    try testing.expectEqualStrings(
        "-Dotel.resource.attributes=eee=fff,ggg=hhh,aaa=bbb,ccc=ddd -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(modifiedJavaToolOptions orelse "-"),
    );
}

test "getModifiedJavaToolOptionsValue: should merge new and existing -Dotel.resource.attributes (at the start)" {
    const original_value: [:0]const u8 = "-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh"[0.. :0];
    const resource_attributes: []u8 = try alloc.page_allocator.alloc(u8, 15);
    var fbs = std.io.fixedBufferStream(resource_attributes);
    _ = try fbs.writer().write("aaa=bbb,ccc=ddd");
    const modifiedJavaToolOptions = getModifiedJavaToolOptionsValue(
        original_value,
        resource_attributes,
    );
    try testing.expectEqualStrings(
        "-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh,aaa=bbb,ccc=ddd -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(modifiedJavaToolOptions orelse "-"),
    );
}

test "getModifiedJavaToolOptionsValue: should merge new and existing -Dotel.resource.attributes (in the middle)" {
    const original_value: [:0]const u8 = "-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh -Dproperty2=value"[0.. :0];
    const resource_attributes: []u8 = try alloc.page_allocator.alloc(u8, 15);
    var fbs = std.io.fixedBufferStream(resource_attributes);
    _ = try fbs.writer().write("aaa=bbb,ccc=ddd");
    const modifiedJavaToolOptions = getModifiedJavaToolOptionsValue(
        original_value,
        resource_attributes,
    );
    try testing.expectEqualStrings(
        "-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh,aaa=bbb,ccc=ddd -Dproperty2=value -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(modifiedJavaToolOptions orelse "-"),
    );
}

test "getModifiedJavaToolOptionsValue: should merge new and existing -Dotel.resource.attributes (at the end)" {
    const original_value: [:0]const u8 = "-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh"[0.. :0];
    const resource_attributes: []u8 = try alloc.page_allocator.alloc(u8, 15);
    var fbs = std.io.fixedBufferStream(resource_attributes);
    _ = try fbs.writer().write("aaa=bbb,ccc=ddd");
    const modifiedJavaToolOptions = getModifiedJavaToolOptionsValue(
        original_value,
        resource_attributes,
    );
    try testing.expectEqualStrings(
        "-Dproperty1=value -Dotel.resource.attributes=eee=fff,ggg=hhh,aaa=bbb,ccc=ddd -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(modifiedJavaToolOptions orelse "-"),
    );
}
