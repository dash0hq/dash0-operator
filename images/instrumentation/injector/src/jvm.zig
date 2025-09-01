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
const injection_happened_msg = "injecting the Java OpenTelemetry agent";

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

    return getModifiedJavaToolOptionsValue(
        original_value_optional,
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
/// JAVA_TOOL_OPTIONS.
///
/// Do no deallocate the return value, or we may cause a USE_AFTER_FREE memory corruption in the parent process.
fn getModifiedJavaToolOptionsValue(original_java_tool_options_env_var_value_optional: ?[:0]const u8) ?types.NullTerminatedString {
    // For auto-instrumentation, we inject the -javaagent flag into the JAVA_TOOL_OPTIONS environment variable.
    if (original_java_tool_options_env_var_value_optional) |original_java_tool_options_env_var_value| {
        // JAVA_TOOL_OPTIONS is set, but no new resource attributes have been provided.
        const return_buffer =
            std.fmt.allocPrintZ(alloc.page_allocator, "{s} {s}", .{
                original_java_tool_options_env_var_value,
                javaagent_flag_value,
            }) catch |err| {
                print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ java_tool_options_env_var_name, err });
                return original_java_tool_options_env_var_value;
            };
        print.printMessage(injection_happened_msg, .{});
        return return_buffer.ptr;
    } else {
        // JAVA_TOOL_OPTIONS is not set, and no new resource attributes have been provided. Simply return the -javaagent flag.
        print.printMessage(injection_happened_msg, .{});
        return javaagent_flag_value[0..].ptr;
    }
}

test "getModifiedJavaToolOptionsValue: should return -javaagent if original value is unset" {
    const modifiedJavaToolOptions = getModifiedJavaToolOptionsValue(null);
    try testing.expectEqualStrings(
        "-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(modifiedJavaToolOptions orelse "-"),
    );
}

test "getModifiedJavaToolOptionsValue: should append -javaagent if original value exists" {
    const original_value: [:0]const u8 = "-Dsome.property=value"[0.. :0];
    const modifiedJavaToolOptions = getModifiedJavaToolOptionsValue(original_value);
    try testing.expectEqualStrings(
        "-Dsome.property=value -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar",
        std.mem.span(modifiedJavaToolOptions orelse "-"),
    );
}
