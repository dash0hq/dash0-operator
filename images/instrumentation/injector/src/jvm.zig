// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const alloc = @import("allocator.zig");
const print = @import("print.zig");
const res_attrs = @import("resource_attributes.zig");
const test_util = @import("test_util.zig");
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
        print.printMessage("Skipping injection of OTel Java agent in 'JAVA_TOOL_OPTIONS' because of an issue accessing the Jar file at {s}: {}", .{ otel_java_agent_path, err });
        return null;
    };

    return getModifiedJavaToolOptionsValue(
        original_value_optional,
    );
}

test "checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue: should return null if the Java agent cannot be accessed (not set)" {
    const modifiedJavaToolOptions = checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue(null);
    try test_util.expectWithMessage(modifiedJavaToolOptions == null, "modifiedJavaToolOptions == null");
}

test "checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue: should return null if the Java agent cannot be accessed (original value present)" {
    const modifiedJavaToolOptions = checkOTelJavaAgentJarAndGetModifiedJavaToolOptionsValue("original value");
    try test_util.expectWithMessage(modifiedJavaToolOptions == null, "modifiedJavaToolOptions == null");
}

/// Returns the modified value for JAVA_TOOL_OPTIONS, including the -javaagent flag; based on the original value of
/// JAVA_TOOL_OPTIONS.
///
/// Do no deallocate the return value, or we may cause a USE_AFTER_FREE memory corruption in the parent process.
fn getModifiedJavaToolOptionsValue(original_java_tool_options_env_var_value_optional: ?[:0]const u8) ?types.NullTerminatedString {
    // For auto-instrumentation, we inject the -javaagent flag into the JAVA_TOOL_OPTIONS environment variable.
    if (original_java_tool_options_env_var_value_optional) |original_java_tool_options_env_var_value| {
        if (std.mem.indexOf(u8, original_java_tool_options_env_var_value, javaagent_flag_value)) |_| {
            // If our "-javaagent ..." flag is already present in JAVA_TOOL_OPTIONS, do nothing. This is particularly
            // important to avoid double injection, for example if we are injecting into a container which has a shell
            // executable as its entry point (into which we inject env var modifications), and then this shell starts
            // the JVM executable as a child process, which inherits the environment from the already injected shell.
            return null;
        }

        // If JAVA_TOOL_OPTIONS is already set, prepend the "-javaagent ..." flag to the original value.
        // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
        // parent process.
        const return_buffer =
            std.fmt.allocPrintZ(alloc.page_allocator, "{s} {s}", .{
                original_java_tool_options_env_var_value,
                javaagent_flag_value,
            }) catch |err| {
                print.printMessage("Cannot allocate memory to manipulate the value of '{s}': {}", .{ java_tool_options_env_var_name, err });
                return null;
            };
        print.printDebug(injection_happened_msg, .{});
        return return_buffer.ptr;
    } else {
        // JAVA_TOOL_OPTIONS is not set, simply return the -javaagent flag.
        print.printDebug(injection_happened_msg, .{});
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

test "getModifiedJavaToolOptionsValue: should do nothing if our -javaagent is already present" {
    const original_value: [:0]const u8 = "-Dsome.property=value -javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar -Dsome.other.property=value"[0.. :0];
    const modifiedJavaToolOptions = getModifiedJavaToolOptionsValue(original_value);
    try test_util.expectWithMessage(modifiedJavaToolOptions == null, "modifiedJavaToolOptions == null");
}
