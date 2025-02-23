const std = @import("std");

const null_terminated_string = [*:0]const u8;

const dash0_log_prefix = "Dash0 injector: ";

const otel_java_agent_path = "/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar";
const dash0_java_tool_options_addition = "-javaagent:" ++ otel_java_agent_path;

const otel_nodejs_module = "/__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry";
const dash0_node_options_addition = "--require " ++ otel_nodejs_module;

// We need to use a rather "innocent" type here, the actual type involves
// optionals that cannot be used in global declarations.
extern var __environ: [*]u8;

// We need to allocate memory only to manipulate and return the few environment
// variables we want to modify. Unmodified values are returned as pointers to
// the original `__environ` memory. We pre-allocate an obscene 128Kb for it.
var buffer: [131072:0]u8 = undefined;
var fba = std.heap.FixedBufferAllocator.init(&buffer);
const allocator: std.mem.Allocator = fba.allocator();

// Ensure we process requests synchtonously. LibC is *not* threadsafe
// with respect to the environment, but chances are some apps will try
// to look up env vars in parallel
const _env_mutex = std.Thread.Mutex{};

var is_debug = false;

// Keep global pointers to already-calculated values to avoid multiple allocations
// on repeated lookups.
var java_tool_options_value_calculated = false;
var java_tool_options_value: ?null_terminated_string = null;
var node_options_value_calculated = false;
var node_options_value: ?null_terminated_string = null;
var otel_resource_attributes_value_calculated = false;
var otel_resource_attributes_value: ?null_terminated_string = null;

export fn getenv(name_z: null_terminated_string) ?null_terminated_string {
    const name = std.mem.sliceTo(name_z, 0);

    // Need to change type from `const` to be able to lock
    var env_mutex = _env_mutex;
    env_mutex.lock();
    defer env_mutex.unlock();

    // Dynamic libs do not get the std.os.environ initialized,
    // see https://github.com/ziglang/zig/issues/4524.
    // So we back fill it.
    // This logic is based on parsing of envp on zig's start.
    //
    // We re-bind the environment every time, as we cannot
    // ensure it did not change since the previous invocation.
    // Libc implementations can re-allocate the environment
    // (http://github.com/lattera/glibc/blob/master/stdlib/setenv.c;
    // https://git.musl-libc.org/cgit/musl/tree/src/env/setenv.c)
    // if the backing memory location is outgrown by apps modifying
    // the environment via setenv or putenv.
    const environment_optional: [*:null]?[*:0]u8 = @ptrCast(@alignCast(__environ));
    var environment_count: usize = 0;
    while (environment_optional[environment_count]) |_| : (environment_count += 1) {}
    std.os.environ = @as([*][*:0]u8, @ptrCast(environment_optional))[0..environment_count];

    // Technically, a process could change the value of `DASH0_DEBUG`
    // after it started (mostly when we debug stuff in REPL) so we look up
    // the value every time.
    if (std.posix.getenv("DASH0_DEBUG")) |is_debug_raw| {
        is_debug = std.ascii.eqlIgnoreCase("true", is_debug_raw);
    }

    const res = getEnvValue(name);

    if (res) |value| {
        printDebug("{s} = '{s}'", .{ name, value });
    } else {
        printDebug("{s} = null", .{name});
    }

    return res;
}

fn getEnvValue(name: [:0]const u8) ?null_terminated_string {
    const original_value = std.posix.getenv(name);

    if (std.mem.eql(u8, name, "OTEL_RESOURCE_ATTRIBUTES")) {
        if (!otel_resource_attributes_value_calculated) {
            otel_resource_attributes_value = getOtelResourceAttributesValue(name, original_value);
            otel_resource_attributes_value_calculated = true;
        } else {
            std.debug.print("REUSED VALUE\n", .{});
        }

        if (otel_resource_attributes_value) |updated_value| {
            return updated_value;
        }
    } else if (std.mem.eql(u8, name, "JAVA_TOOL_OPTIONS")) {
        if (!java_tool_options_value_calculated) {
            java_tool_options_value = getJavaToolOptionsValue(name, original_value);
            java_tool_options_value_calculated = true;
        }

        if (java_tool_options_value) |updated_value| {
            return updated_value;
        }
    } else if (std.mem.eql(u8, name, "NODE_OPTIONS")) {
        if (!node_options_value_calculated) {
            node_options_value = getNodeOptionsValue(name, original_value);
            node_options_value_calculated = true;
        }

        if (node_options_value) |updated_value| {
            return updated_value;
        }
    }

    // Do not reallocate the original value; instead, return pointer to the original value
    if (original_value) |val| {
        return val.ptr;
    }

    return null;
}

fn getOtelResourceAttributesValue(name: [:0]const u8, original_value: ?[:0]const u8) ?null_terminated_string {
    if (getResourceAttributes()) |resource_attributes| {
        defer allocator.free(resource_attributes);

        if (original_value) |val| {
            // Prefix our resource attributes to those already existent

            // Note: We can *never* deallocate this, or we may cause a USE_AFTER_FREE
            // memory corruption in the parent process. The Libcs can do it too, but
            // they apparently YOLO it.
            const return_buffer = std.fmt.allocPrintZ(allocator, "{s},{s}", .{ resource_attributes, val }) catch |err| {
                printDebug("Cannot allocate memory to manipulate the value of '{s}': {}", .{ name, err });
                return null;
            };

            return return_buffer.ptr;
        } else {
            // Note: We can *never* deallocate this, or we may cause a USE_AFTER_FREE
            // memory corruption in the parent process. The Libcs can do it too, but
            // they apparently YOLO it.
            const return_buffer = std.fmt.allocPrintZ(allocator, "{s}", .{resource_attributes}) catch |err| {
                printDebug("Cannot allocate memory to manipulate the value of '{s}': {}", .{ name, err });
                return null;
            };

            return return_buffer.ptr;
        }
    }

    // No resource attributes to add. Return a pointer to the current value,
    // or null if there is no current value.
    if (original_value) |val| {
        // Note: We can *never* deallocate this, or we may cause a USE_AFTER_FREE
        // memory corruption in the parent process. The Libcs can do it too, but
        // they apparently YOLO it.
        const return_buffer = std.fmt.allocPrintZ(allocator, "{s}", .{val}) catch |err| {
            printDebug("Cannot allocate memory to manipulate the value of '{s}': {}", .{ name, err });
            return null;
        };

        return return_buffer.ptr;
    }

    return null;
}

fn getJavaToolOptionsValue(name: [:0]const u8, original_value: ?[:0]const u8) ?null_terminated_string {
    // Check the existence of the Jar file: by passing a `-javaagent` to a
    // jar file that does not exist or cannot be opened will crash the JVM
    std.fs.cwd().access(otel_java_agent_path, .{}) catch |err| {
        printDebug("Skipping injection of OTel Java Agent in 'JAVA_TOOL_OPTIONS', because of an issue accessing the Jar file at {s}: {}", .{ otel_java_agent_path, err });
        return null;
    };

    if (original_value) |val| {
        // The Java runtime does not look up the OTEL_RESOURCE_ATTRIBUTES env
        // var using getenv(), but rather by parsing the environment block
        // (/proc/env/<pid>) directly, which we cannot affect with the getenv
        // hook. So, instead, we append the resource attributes as the
        // -Dotel.resource.attributes Java system property.
        // If the -Dotel.resource.attributes system property is already set,
        // the user-defined property will take precedence:
        //
        // % JAVA_TOOL_OPTIONS="-Dprop=B" jshell -R -Dprop=A
        // Picked up JAVA_TOOL_OPTIONS: -Dprop=B
        // |  Welcome to JShell -- Version 17.0.12
        // |  For an introduction type: /help intro
        //
        // jshell> System.getProperty("prop")
        // $1 ==> "A"
        if (getResourceAttributes()) |resource_attributes| {
            defer allocator.free(resource_attributes);

            // Note: We can *never* deallocate this, or we may cause a USE_AFTER_FREE
            // memory corruption in the parent process. The Libcs can do it too, but
            // they apparently YOLO it.
            const return_buffer = std.fmt.allocPrintZ(allocator, "{s} {s} -Dotel.resource.attributes={s}", .{ val, dash0_java_tool_options_addition, resource_attributes }) catch |err| {
                printDebug("Cannot allocate memory to manipulate the value of '{s}': {}", .{ name, err });
                return null;
            };

            return return_buffer.ptr;
        }

        // If a JAVA_TOOL_OPTIONS is already present, append it after our --javaagent.

        // Note: We can *never* deallocate this, or we may cause a USE_AFTER_FREE
        // memory corruption in the parent process. The Libcs can do it too, but
        // they apparently YOLO it.
        const return_buffer = std.fmt.allocPrintZ(allocator, "{s} {s}", .{ dash0_java_tool_options_addition, val }) catch |err| {
            printDebug("Cannot allocate memory to manipulate the value of '{s}': {}", .{ name, err });
            return null;
        };

        return return_buffer.ptr;
    }

    return dash0_java_tool_options_addition[0..].ptr;
}

fn getNodeOptionsValue(name: [:0]const u8, original_value: ?[:0]const u8) ?null_terminated_string {
    // Check the existence of the Node module: requiring or importing a module
    // that does not exist or cannot be opened will crash the Node.js process
    // with an 'ERR_MODULE_NOT_FOUND' error.
    std.fs.cwd().access(otel_nodejs_module, .{}) catch |err| {
        printDebug("Skipping injection of OTel Node.js in 'NODE_OPTIONS', because of an issue accessing the Node.js module at {s}: {}", .{ otel_nodejs_module, err });
        return null;
    };

    if (original_value) |val| {
        // If NODE_OPTIONS were present, append it after our --require.

        // Note: We can *never* deallocate this, or we may cause a USE_AFTER_FREE
        // memory corruption in the parent process. The Libcs can do it too, but
        // they apparently YOLO it.
        const return_buffer = std.fmt.allocPrintZ(allocator, "{s} {s}", .{ dash0_node_options_addition, val }) catch |err| {
            printDebug("Cannot allocate memory to manipulate the value of '{s}': {}", .{ name, err });
            return null;
        };

        return return_buffer.ptr;
    }

    return dash0_node_options_addition[0..].ptr;
}

const EnvToResourceAttributeMapping = struct {
    environement_variable_name: []const u8,
    resource_attributes_key: []const u8,
};

const mappings: [8]EnvToResourceAttributeMapping = .{ EnvToResourceAttributeMapping{
    .environement_variable_name = "DASH0_SERVICE_NAME",
    .resource_attributes_key = "service.name",
}, EnvToResourceAttributeMapping{
    .environement_variable_name = "DASH0_SERVICE_NAMESPACE",
    .resource_attributes_key = "service.namespace",
}, EnvToResourceAttributeMapping{
    .environement_variable_name = "DASH0_SERVICE_VERSION",
    .resource_attributes_key = "service.version",
}, EnvToResourceAttributeMapping{
    .environement_variable_name = "DASH0_NAMESPACE_NAME",
    .resource_attributes_key = "k8s.namespace.name",
}, EnvToResourceAttributeMapping{
    .environement_variable_name = "DASH0_POD_NAME",
    .resource_attributes_key = "k8s.pod.name",
}, EnvToResourceAttributeMapping{
    .environement_variable_name = "DASH0_POD_UID",
    .resource_attributes_key = "k8s.pod.uid",
}, EnvToResourceAttributeMapping{
    .environement_variable_name = "DASH0_CONTAINER_NAME",
    .resource_attributes_key = "k8s.container.name",
}, EnvToResourceAttributeMapping{
    .environement_variable_name = "DASH0_RESOURCE_ATTRIBUTES",
    .resource_attributes_key = "",
} };

// Called must free the returned []u8 array, if a non-null value is returned.
fn getResourceAttributes() ?[]u8 {
    var final_len: usize = 0;

    for (mappings) |mapping| {
        if (std.posix.getenv(mapping.environement_variable_name)) |value| {
            if (value.len > 0) {
                if (final_len > 0) {
                    final_len += 1; // ","
                }

                if (mapping.resource_attributes_key.len > 0) {
                    final_len += std.fmt.count("{s}={s}", .{ mapping.resource_attributes_key, value });
                } else {
                    // DASH0_RESOURCE_ATTRIBUTES
                    final_len += value.len;
                }
            }
        }
    }

    if (final_len < 1) {
        return null;
    }

    const resource_attributes = allocator.alloc(u8, final_len) catch |err| {
        printDebug("Cannot allocate memory to prepare the resource attributes (len: {d}): {}", .{ final_len, err });
        return null;
    };

    var fbs = std.io.fixedBufferStream(resource_attributes);

    var isFirstToken = true;
    for (mappings) |mapping| {
        if (std.posix.getenv(mapping.environement_variable_name)) |value| {
            if (value.len > 0) {
                if (isFirstToken) {
                    isFirstToken = false;
                } else {
                    std.fmt.format(fbs.writer(), ",", .{}) catch |err| {
                        printDebug("Cannot append ',' delimiter to resource attributes: {}", .{err});
                        return null;
                    };
                }

                if (mapping.resource_attributes_key.len > 0) {
                    std.fmt.format(fbs.writer(), "{s}={s}", .{ mapping.resource_attributes_key, value }) catch |err| {
                        printDebug("Cannot append '{s}={s}' from env var '{s}' to resource attributes: {}", .{ mapping.resource_attributes_key, value, mapping.environement_variable_name, err });
                        return null;
                    };
                } else {
                    // DASH0_RESOURCE_ATTRIBUTES
                    std.fmt.format(fbs.writer(), "{s}", .{value}) catch |err| {
                        printDebug("Cannot append '{s}' from env var '{s}' to resource attributes: {}", .{ value, mapping.environement_variable_name, err });
                        return null;
                    };
                }
            }
        }
    }

    // Returns a slice
    return resource_attributes;
}

fn printDebug(comptime fmt: []const u8, args: anytype) void {
    if (is_debug) {
        std.debug.print(dash0_log_prefix ++ fmt ++ "\n", args);
    }
}
