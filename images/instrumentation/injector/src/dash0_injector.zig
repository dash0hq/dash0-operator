const std = @import("std");

const null_terminated_string = [*:0]const u8;

const dash0_log_prefix = "Dash0 injector: ";

const dash0_java_tool_options_addition = "-javaagent:/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar";
const dash0_node_options_addition = "--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry";

// In Linux 2.6.13+ there is the rlimit_resource, but we cannot access it at compile time
// and assume it is the same for whatever Linux the injector will run on.
// Instead, we assume the 512KB limit.
var buffer: [524288:0]u8 = undefined;
var fba = std.heap.FixedBufferAllocator.init(&buffer);
const allocator: std.mem.Allocator = fba.allocator();

const _envMutex = std.Thread.Mutex{};

export fn getenv(nameZ: null_terminated_string) ?null_terminated_string {
    const name = std.mem.sliceTo(nameZ, 0);

    if (std.mem.eql(u8, "PWD", name)) {
        return null;
    }

    // Some runtimes like Node.js look uop variables in a multithreaded fashion
    var envMutex = _envMutex;
    envMutex.lock();
    defer envMutex.unlock();

    const envMap = getEnvMap() catch |err| {
        std.debug.print(dash0_log_prefix ++ "Cannot retrieve process environment, returning null for environment variable '{s}': {}\n", .{ name, err });
        return null;
    };

    const res = getEnvValue(envMap, name);
    if (res) |val| {
        // std.debug.print(dash0_log_prefix ++ "{s}={s}\n", .{ name, val });
        std.debug.print("{s}={s}\n", .{ name, val });
        // } else {
        //     std.debug.print(dash0_log_prefix ++ "{s} => null\n", .{name});
    }
    return res;
}

fn getEnvValue(envMap: std.process.EnvMap, name: [:0]const u8) ?null_terminated_string {
    const value = envMap.get(name);

    if (std.mem.eql(u8, name, "OTEL_RESOURCE_ATTRIBUTES")) {
        const resource_attributes = getResourceAttributes(envMap) catch |err| {
            std.debug.print(dash0_log_prefix ++ "Cannot look up resource attributes from environment to calculate the value of '{s}': {}\n", .{ name, err });
            return null;
        };

        if (resource_attributes.len < 1) {
            // No resource attributes to add. Return a pointer to the current value,
            // or null if there is no current value.
            if (value) |val| {
                const return_buffer = std.fmt.allocPrintZ(allocator, "{s}", .{val}) catch |err| {
                    std.debug.print(dash0_log_prefix ++ "Cannot allocate memory to manipulate the value of '{s}': {}\n", .{ name, err });
                    return null;
                };

                return return_buffer.ptr;
            } else {
                return null;
            }
        }

        if (value) |val| {
            const return_buffer = std.fmt.allocPrintZ(allocator, "{s},{s}", .{ resource_attributes, val }) catch |err| {
                std.debug.print(dash0_log_prefix ++ "Cannot allocate memory to manipulate the value of '{s}': {}\n", .{ name, err });
                return null;
            };

            return return_buffer.ptr;
        }

        const return_buffer = std.fmt.allocPrintZ(allocator, "{s}", .{resource_attributes}) catch |err| {
            std.debug.print(dash0_log_prefix ++ "Cannot allocate memory to manipulate the value of '{s}': {}\n", .{ name, err });
            return null;
        };

        return return_buffer.ptr;
    } else if (std.mem.eql(u8, name, "JAVA_TOOL_OPTIONS")) {
        // The Java runtime does not look up the OTEL_RESOURCE_ATTRIBUTES env var
        // using getenv(), but rather by parsing the environment block
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
        if (value) |val| {
            const resourceAttributes = getResourceAttributes(envMap) catch |err| {
                std.debug.print(dash0_log_prefix ++ "Cannot retrieve resource attributes: {}\n", .{err});
                return "";
            };

            if (resourceAttributes.len > 0) {
                const return_buffer = std.fmt.allocPrintZ(allocator, "{s} {s} -Dotel.resource.attributes={s}", .{ val, dash0_java_tool_options_addition, resourceAttributes }) catch |err| {
                    std.debug.print(dash0_log_prefix ++ "Cannot allocate memory to manipulate the value of '{s}': {}\n", .{ name, err });
                    return null;
                };

                return return_buffer.ptr;
            } else {
                // If JAVA_TOOL_OPTIONS were present, append the existing JAVA_TOOL_OPTIONS after our --javaagent.
                const return_buffer = std.fmt.allocPrintZ(allocator, "{s} {s}", .{ dash0_java_tool_options_addition, val }) catch |err| {
                    std.debug.print(dash0_log_prefix ++ "Cannot allocate memory to manipulate the value of '{s}': {}\n", .{ name, err });
                    return null;
                };

                return return_buffer.ptr;
            }
        } else {
            return dash0_java_tool_options_addition[0..].ptr;
        }
    } else if (std.mem.eql(u8, name, "NODE_OPTIONS")) {
        if (value) |val| {
            // If NODE_OPTIONS were present, append the existing NODE_OPTIONS after our --require.
            const return_buffer = std.fmt.allocPrintZ(allocator, "{s} {s}", .{ dash0_node_options_addition, val }) catch |err| {
                std.debug.print(dash0_log_prefix ++ "Cannot allocate memory to manipulate the value of '{s}': {}\n", .{ name, err });
                return null;
            };

            return return_buffer.ptr;
        } else {
            return dash0_node_options_addition[0..].ptr;
        }
    }

    if (value) |val| {
        if (val.len > 0) {
            const return_buffer = std.fmt.allocPrintZ(allocator, "{s}", .{val}) catch |err| {
                std.debug.print(dash0_log_prefix ++ "Cannot allocate memory to return the unmodified value '{s}' of '{s}': {}\n", .{ val, name, err });
                return null;
            };

            return return_buffer.ptr;
        }
    }

    return null;
}

const EnvToResourceAttributeMapping = struct {
    environement_variable_name: []const u8,
    resource_attributes_key: []const u8,
};

const mappings: [4]EnvToResourceAttributeMapping = .{ EnvToResourceAttributeMapping{
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
} };

fn getResourceAttributes(envMap: std.process.EnvMap) ![]u8 {
    var final_len: usize = 0;

    for (mappings) |mapping| {
        if (envMap.get(mapping.environement_variable_name)) |value| {
            if (final_len > 0) {
                final_len += 1; // ";"
            }

            final_len += mapping.resource_attributes_key.len;
            final_len += 1; // "="
            final_len += value.len;
        }
    }

    var resourceAttributes = try allocator.alloc(u8, final_len);
    // Do not defer, we need to return this memory location outside of the injector

    var i: usize = 0;
    for (mappings) |mapping| {
        if (i > 0) {
            resourceAttributes[i] = ',';
            i += 1;
        }

        if (envMap.get(mapping.environement_variable_name)) |value| {
            for (mapping.resource_attributes_key) |char| {
                resourceAttributes[i] = char;
                i += 1;
            }

            resourceAttributes[i] = '=';
            i += 1;

            for (value) |char| {
                resourceAttributes[i] = char;
                i += 1;
            }
        }
    }

    // Return slice, buffer has deferred free
    return resourceAttributes;
}

const EnvironmentMapCreationError = error{
    CannotOpenProcessEnviron,
    CannotReadProcessEnviron,
};

var _environ: ?std.process.EnvMap = null;

fn getEnvMap() !std.process.EnvMap {
    if (_environ) |envMap| {
        return envMap;
    }

    var file = try std.fs.cwd().openFile("/proc/self/environ", .{ .mode = .read_only });
    defer file.close();

    const in_stream = file.reader();
    const max_size = std.math.maxInt(u32); // TODO Find better limit
    const environ = try in_stream.readAllAlloc(allocator, max_size);

    std.debug.print(dash0_log_prefix ++ "Environ: {s}\n", .{environ});

    var res = std.process.EnvMap.init(allocator);

    var i: usize = 0;

    while (environ.len > i) {
        const env_var: []u8 = std.mem.sliceTo(environ[i..], 0);

        if (std.mem.indexOf(u8, env_var, "=")) |index_first_equals| {
            const key = env_var[0..index_first_equals];
            const value = env_var[index_first_equals + 1 ..];

            res.put(key, value) catch |err| {
                // TODO Can we add args to the context?
                return err;
            };

            i += env_var.len + 1;
        } else {
            break;
        }
    }

    _environ = res;
    return res;
}
