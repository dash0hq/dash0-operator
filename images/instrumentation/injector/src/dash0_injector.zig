const builtin = @import("builtin");
const std = @import("std");
const assert = std.debug.assert;

const null_terminated_string = [*:0]const u8;

const log_prefix = "Dash0 injector: ";

const otel_java_agent_path = "/__dash0__/instrumentation/jvm/opentelemetry-javaagent.jar";
const java_tool_options_addition = "-javaagent:" ++ otel_java_agent_path;

const otel_nodejs_module = "/__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry";
const node_options_addition = "--require " ++ otel_nodejs_module;

const dotnet_path_prefix = "/__dash0__/instrumentation/dotnet";

// We need to use a rather "innocent" type here, the actual type involves
// optionals that cannot be used in global declarations.
extern var __environ: [*]u8;

// We need to allocate memory only to manipulate and return the few environment
// variables we want to modify. Unmodified values are returned as pointers to
// the original `__environ` memory. We pre-allocate an obscene 128Kb for it.
var allocator_buffer: [131072:0]u8 = undefined;
var fba = std.heap.FixedBufferAllocator.init(&allocator_buffer);
const allocator: std.mem.Allocator = fba.allocator();

// Ensure we process requests synchtonously. LibC is *not* threadsafe
// with respect to the environment, but chances are some apps will try
// to look up env vars in parallel
const _env_mutex = std.Thread.Mutex{};

var is_debug = false;

// Keep global pointers to already-calculated values to avoid multiple allocations
// on repeated lookups.
var modified_java_tool_options_value_calculated = false;
var modified_java_tool_options_value: ?null_terminated_string = null;
var modified_node_options_value_calculated = false;
var modified_node_options_value: ?null_terminated_string = null;
var modified_otel_resource_attributes_value_calculated = false;
var modified_otel_resource_attributes_value: ?null_terminated_string = null;

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

    if (is_debug) {
        if (res) |value| {
            printDebug("{s} = '{s}'", .{ name, value });
        } else {
            printDebug("{s} = null", .{name});
        }
    }

    return res;
}

fn getEnvValue(name: [:0]const u8) ?null_terminated_string {
    const original_value = std.posix.getenv(name);

    if (std.mem.eql(u8, name, "OTEL_RESOURCE_ATTRIBUTES")) {
        if (!modified_otel_resource_attributes_value_calculated) {
            modified_otel_resource_attributes_value = getModifiedOtelResourceAttributesValue(name, original_value);
            modified_otel_resource_attributes_value_calculated = true;
        }

        if (modified_otel_resource_attributes_value) |updated_value| {
            return updated_value;
        }
    } else if (std.mem.eql(u8, name, "JAVA_TOOL_OPTIONS")) {
        if (!modified_java_tool_options_value_calculated) {
            modified_java_tool_options_value = getModifiedJavaToolOptionsValue(name, original_value);
            modified_java_tool_options_value_calculated = true;
        }

        if (modified_java_tool_options_value) |updated_value| {
            return updated_value;
        }
    } else if (std.mem.eql(u8, name, "NODE_OPTIONS")) {
        if (!modified_node_options_value_calculated) {
            modified_node_options_value = getModifiedNodeOptionsValue(name, original_value);
            modified_node_options_value_calculated = true;
        }

        if (modified_node_options_value) |updated_value| {
            return updated_value;
        }
    } else if (std.mem.eql(u8, name, "CORECLR_ENABLE_PROFILING")) {
        if (getDotNetValues()) |v| {
            return v.coreclr_enable_profiling;
        }
    } else if (std.mem.eql(u8, name, "CORECLR_PROFILER")) {
        if (getDotNetValues()) |v| {
            return v.coreclr_profiler;
        }
    } else if (std.mem.eql(u8, name, "CORECLR_PROFILER_PATH")) {
        if (getDotNetValues()) |v| {
            return v.coreclr_profiler_path;
        }
    } else if (std.mem.eql(u8, name, "DOTNET_ADDITIONAL_DEPS")) {
        if (getDotNetValues()) |v| {
            return v.additional_deps;
        }
    } else if (std.mem.eql(u8, name, "DOTNET_SHARED_STORE")) {
        if (getDotNetValues()) |v| {
            return v.shared_store;
        }
    } else if (std.mem.eql(u8, name, "DOTNET_STARTUP_HOOKS")) {
        if (getDotNetValues()) |v| {
            return v.startup_hooks;
        }
    } else if (std.mem.eql(u8, name, "OTEL_DOTNET_AUTO_HOME")) {
        if (getDotNetValues()) |v| {
            return v.otel_auto_home;
        }
    }

    // Do not reallocate the original value; instead, return pointer to the original value
    if (original_value) |val| {
        return val.ptr;
    }

    return null;
}

fn getModifiedOtelResourceAttributesValue(name: [:0]const u8, original_value: ?[:0]const u8) ?null_terminated_string {
    if (getResourceAttributes()) |resource_attributes| {
        defer allocator.free(resource_attributes);

        if (original_value) |val| {
            // Prefix our resource attributes to those already existent

            // Note: We can *never* deallocate this, or we may cause a USE_AFTER_FREE
            // memory corruption in the parent process. The Libcs can do it too, but
            // they apparently YOLO it.
            const return_buffer = std.fmt.allocPrintZ(allocator, "{s},{s}", .{ resource_attributes, val }) catch |err| {
                printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ name, err });
                return null;
            };

            return return_buffer.ptr;
        } else {
            // Note: We can *never* deallocate this, or we may cause a USE_AFTER_FREE
            // memory corruption in the parent process. The Libcs can do it too, but
            // they apparently YOLO it.
            const return_buffer = std.fmt.allocPrintZ(allocator, "{s}", .{resource_attributes}) catch |err| {
                printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ name, err });
                return null;
            };

            return return_buffer.ptr;
        }
    } else {
        // No resource attributes to add. Return a pointer to the current value,
        // or null if there is no current value.
        if (original_value) |val| {
            // Note: We can *never* deallocate this, or we may cause a USE_AFTER_FREE
            // memory corruption in the parent process. The Libcs can do it too, but
            // they apparently YOLO it.
            const return_buffer = std.fmt.allocPrintZ(allocator, "{s}", .{val}) catch |err| {
                printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ name, err });
                return null;
            };

            return return_buffer.ptr;
        }
    }

    return null;
}

const DotNetValues = struct {
    coreclr_enable_profiling: null_terminated_string,
    coreclr_profiler: null_terminated_string,
    coreclr_profiler_path: null_terminated_string,
    additional_deps: null_terminated_string,
    shared_store: null_terminated_string,
    startup_hooks: null_terminated_string,
    otel_auto_home: null_terminated_string,
};

var dotnet_values: ?DotNetValues = null;
var libcFlavor: ?LibCFlavor = null;

pub const DotNetError = error{
    LibCFlavorUnknown,
};

fn getDotNetValues() ?DotNetValues {
    if (dotnet_values) |val| {
        return val;
    }

    if (libcFlavor == null) {
        libcFlavor = getLibCFlavor();
    }

    if (libcFlavor == LibCFlavor.UNKNOWN) {
        printError("Cannot determine LibC flavor", .{});
        return null;
    }

    if (libcFlavor) |flavor| {
        return doDotNetValues(flavor) catch |err| {
            printError("Cannot determine .NET environment variables: {}", .{err});
            return null;
        };
    }

    unreachable;
}

fn doDotNetValues(flavor: LibCFlavor) !DotNetValues {
    const libc_flavor_prefix = if (flavor == LibCFlavor.GNU_LIBC) "glibc" else "muslc";
    const platform = if (flavor == LibCFlavor.GNU_LIBC) (if (builtin.cpu.arch == .aarch64) "linux-arm64" else "linux-x64") else (if (builtin.cpu.arch == .aarch64) "linux-musl-arm64" else "linux-musl-x64");

    const coreclr_profiler_path = try std.fmt.allocPrintZ(allocator, "{s}/{s}/{s}/OpenTelemetry.AutoInstrumentation.Native.so", .{
        dotnet_path_prefix, libc_flavor_prefix, platform,
    });

    const additional_deps = try std.fmt.allocPrintZ(allocator, "{s}/{s}/AdditionalDeps", .{
        dotnet_path_prefix, libc_flavor_prefix,
    });

    const otel_auto_home = try std.fmt.allocPrintZ(allocator, "{s}/{s}", .{ dotnet_path_prefix, libc_flavor_prefix });

    const shared_store = try std.fmt.allocPrintZ(allocator, "{s}/{s}/store", .{
        dotnet_path_prefix, libc_flavor_prefix,
    });

    const startup_hooks = try std.fmt.allocPrintZ(allocator, "{s}/{s}/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll", .{
        dotnet_path_prefix, libc_flavor_prefix,
    });

    return .{
        .coreclr_enable_profiling = "1",
        .coreclr_profiler = "{918728DD-259F-4A6A-AC2B-B85E1B658318}",
        .coreclr_profiler_path = coreclr_profiler_path,
        .additional_deps = additional_deps,
        .otel_auto_home = otel_auto_home,
        .shared_store = shared_store,
        .startup_hooks = startup_hooks,
    };
}

fn getModifiedJavaToolOptionsValue(name: [:0]const u8, original_value: ?[:0]const u8) ?null_terminated_string {
    // Check the existence of the Jar file: by passing a `-javaagent` to a
    // jar file that does not exist or cannot be opened will crash the JVM
    std.fs.cwd().access(otel_java_agent_path, .{}) catch |err| {
        printError("Skipping injection of OTel Java Agent in 'JAVA_TOOL_OPTIONS' because of an issue accessing the Jar file at {s}: {}", .{ otel_java_agent_path, err });
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

            // If JAVA_TOOL_OPTIONS is already set, append our --javaagent after it.

            // Note: We can *never* deallocate this, or we may cause a USE_AFTER_FREE
            // memory corruption in the parent process. The Libcs can do it too, but
            // they apparently YOLO it.
            const return_buffer = std.fmt.allocPrintZ(allocator, "{s} {s} -Dotel.resource.attributes={s}", .{ val, java_tool_options_addition, resource_attributes }) catch |err| {
                printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ name, err });
                return null;
            };

            return return_buffer.ptr;
        } else {
            // If JAVA_TOOL_OPTIONS is already set, append our --javaagent after it.

            // Note: We can *never* deallocate this, or we may cause a USE_AFTER_FREE
            // memory corruption in the parent process. The Libcs can do it too, but
            // they apparently YOLO it.
            const return_buffer = std.fmt.allocPrintZ(allocator, "{s} {s}", .{ java_tool_options_addition, val }) catch |err| {
                printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ name, err });
                return null;
            };

            return return_buffer.ptr;
        }
    }

    return java_tool_options_addition[0..].ptr;
}

fn getModifiedNodeOptionsValue(name: [:0]const u8, original_value: ?[:0]const u8) ?null_terminated_string {
    // Check the existence of the Node module: requiring or importing a module
    // that does not exist or cannot be opened will crash the Node.js process
    // with an 'ERR_MODULE_NOT_FOUND' error.
    std.fs.cwd().access(otel_nodejs_module, .{}) catch |err| {
        printError("Skipping injection of OTel Node.js module in 'NODE_OPTIONS' because of an issue accessing the Node.js module at {s}: {}", .{ otel_nodejs_module, err });
        return null;
    };

    if (original_value) |val| {
        // If NODE_OPTIONS is already set, prefix our --require to the original value.

        // Note: We can *never* deallocate this, or we may cause a USE_AFTER_FREE
        // memory corruption in the parent process. The Libcs can do it too, but
        // they apparently YOLO it.
        const return_buffer = std.fmt.allocPrintZ(allocator, "{s} {s}", .{ node_options_addition, val }) catch |err| {
            printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ name, err });
            return null;
        };

        return return_buffer.ptr;
    }

    return node_options_addition[0..].ptr;
}

// If `resource_attributes_key` is not null, we append a `{key}={value}`
// otherwise just `{value}`
const EnvToResourceAttributeMapping = struct {
    environement_variable_name: []const u8,
    resource_attributes_key: ?[]const u8,
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
    .resource_attributes_key = null,
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

                if (mapping.resource_attributes_key) |attribute_key| {
                    final_len += std.fmt.count("{s}={s}", .{ attribute_key, value });
                } else {
                    final_len += value.len;
                }
            }
        }
    }

    if (final_len < 1) {
        return null;
    }

    const resource_attributes = allocator.alloc(u8, final_len) catch |err| {
        printError("Cannot allocate memory to prepare the resource attributes (len: {d}): {}", .{ final_len, err });
        return null;
    };

    var fbs = std.io.fixedBufferStream(resource_attributes);

    var is_first_token = true;
    for (mappings) |mapping| {
        const env_var_name = mapping.environement_variable_name;
        if (std.posix.getenv(env_var_name)) |value| {
            if (value.len > 0) {
                if (is_first_token) {
                    is_first_token = false;
                } else {
                    std.fmt.format(fbs.writer(), ",", .{}) catch |err| {
                        printError("Cannot append ',' delimiter to resource attributes: {}", .{err});
                        return null;
                    };
                }

                if (mapping.resource_attributes_key) |attribute_key| {
                    std.fmt.format(fbs.writer(), "{s}={s}", .{ attribute_key, value }) catch |err| {
                        printError("Cannot append '{s}={s}' from env var '{s}' to resource attributes: {}", .{ attribute_key, value, env_var_name, err });
                        return null;
                    };
                } else {
                    std.fmt.format(fbs.writer(), "{s}", .{value}) catch |err| {
                        printError("Cannot append '{s}' from env var '{s}' to resource attributes: {}", .{ value, env_var_name, err });
                        return null;
                    };
                }
            }
        }
    }

    // Returns a slice
    return resource_attributes;
}

const LibCFlavor = enum { UNKNOWN, GNU_LIBC, MULSC };

pub const LibCFlavorError = error{
    ElfNot64Bit,
    ElfDynamicStringTableNotFound,
    ElfDynamicSymbolTableNotFound,
    ElfStringsTableNotFound,
};

fn getLibCFlavor() LibCFlavor {
    return doGetLibCFlavor() catch |err| {
        printError("Cannot determine LibC flavor from ELF metadata of '/proc/self/exe': {}", .{err});
        return LibCFlavor.UNKNOWN;
    };
}

fn doGetLibCFlavor() !LibCFlavor {
    const self_exe_file = std.fs.openFileAbsolute("/proc/self/exe", .{ .mode = .read_only }) catch |err| {
        printError("Cannot open '/proc/self/exe': {}", .{err});
        return LibCFlavor.UNKNOWN;
    };
    defer self_exe_file.close();

    const elf_header = std.elf.Header.read(self_exe_file) catch |err| {
        printError("Cannot read ELF header from '/proc/self/exe': {}", .{err});
        return LibCFlavor.UNKNOWN;
    };

    if (!elf_header.is_64) {
        printError("ELF header from '/proc/self/exe' seems not to be the one of a 64-bit binary", .{});
        return error.ElfNot64Bit;
    }

    var sections_header_iterator = elf_header.section_header_iterator(self_exe_file);

    var dynamic_symbols_table_offset: u64 = 0;
    var dynamic_symbols_table_size: u64 = 0;

    while (try sections_header_iterator.next()) |section_header| {
        switch (section_header.sh_type) {
            std.elf.SHT_DYNAMIC => {
                dynamic_symbols_table_offset = section_header.sh_offset;
                dynamic_symbols_table_size = section_header.sh_size;
            },
            else => {
                // Ignore this section
            },
        }
    }

    if (dynamic_symbols_table_offset == 0) {
        printError("No dynamic section found in ELF metadata from '/proc/self/exe'", .{});
        return error.ElfDynamicSymbolTableNotFound;
    }

    // Look for DT_NEEDED entries in the Dynamic table, they state which libraries were
    // used at compilation step. Examples:
    //
    // Java + GNU LibC
    //
    // $ readelf -Wd /usr/bin/java
    // Dynamic section at offset 0xfd28 contains 30 entries:
    //   Tag        Type                         Name/Value
    //  0x0000000000000001 (NEEDED)             Shared library: [libz.so.1]
    //  0x0000000000000001 (NEEDED)             Shared library: [libjli.so]
    //  0x0000000000000001 (NEEDED)             Shared library: [libc.so.6]
    //
    // Java + muslc
    //
    // $ readelf -Wd /usr/bin/java
    // Dynamic section at offset 0xfd18 contains 33 entries:
    //   Tag        Type                         Name/Value
    //  0x0000000000000001 (NEEDED)             Shared library: [libjli.so]
    //  0x0000000000000001 (NEEDED)             Shared library: [libc.musl-aarch64.so.1]

    // Read dynamic section
    // Read dynamic section
    try self_exe_file.seekTo(dynamic_symbols_table_offset);
    const dynamic_symbol_count = dynamic_symbols_table_size / @sizeOf(std.elf.Elf64_Dyn);
    const dynamic_symbols = try allocator.alloc(std.elf.Elf64_Dyn, dynamic_symbol_count);
    defer allocator.free(dynamic_symbols);
    _ = try self_exe_file.read(std.mem.sliceAsBytes(dynamic_symbols));

    // Find string table address (DT_STRTAB)
    var strtab_addr: u64 = 0;
    for (dynamic_symbols) |dyn| {
        if (dyn.d_tag == std.elf.DT_STRTAB) {
            strtab_addr = dyn.d_val;
            break;
        }
    }
    if (strtab_addr == 0) {
        printError("No string table found", .{});
        return error.ElfStringsTableNotFound;
    }

    sections_header_iterator.index = 0;
    var string_table_offset: u64 = 0;
    while (try sections_header_iterator.next()) |shdr| {
        if (shdr.sh_type == std.elf.SHT_STRTAB and shdr.sh_addr == strtab_addr) {
            string_table_offset = shdr.sh_offset;
            break;
        }
    }

    if (string_table_offset == 0) {
        // Fallback: Use program headers if section headers don’t map it
        try self_exe_file.seekTo(elf_header.phoff);
        const phdrs = try std.heap.page_allocator.alloc(std.elf.Elf64_Phdr, elf_header.phnum);
        defer std.heap.page_allocator.free(phdrs);
        _ = try self_exe_file.read(std.mem.sliceAsBytes(phdrs));
        for (phdrs) |phdr| {
            if (phdr.p_type == std.elf.PT_LOAD and phdr.p_vaddr <= strtab_addr and strtab_addr < phdr.p_vaddr + phdr.p_filesz) {
                string_table_offset = phdr.p_offset + (strtab_addr - phdr.p_vaddr);
                break;
            }
        }
        if (string_table_offset == 0) {
            printError("Could not map string table address", .{});
            return error.ElfStringsTableNotFound;
        }
    }

    for (dynamic_symbols) |dynamic_symbol| {
        if (dynamic_symbol.d_tag == std.elf.DT_NULL) {
            break;
        }

        if (dynamic_symbol.d_tag == std.elf.DT_NEEDED) {
            const string_offset = string_table_offset + dynamic_symbol.d_val;
            try self_exe_file.seekTo(string_offset);

            // Read null-terminated string (up to 256 bytes max for simplicity)
            var buffer: [256]u8 = undefined;
            const bytes_read = try self_exe_file.read(&buffer);
            const lib_name = buffer[0..bytes_read];

            if (std.mem.indexOf(u8, lib_name, "musl")) |_| {
                return LibCFlavor.MULSC;
            }

            if (std.mem.indexOf(u8, lib_name, "libc.so.6")) |_| {
                return LibCFlavor.GNU_LIBC;
            }
        }
    }

    return LibCFlavor.UNKNOWN;
}

fn printDebug(comptime fmt: []const u8, args: anytype) void {
    if (is_debug) {
        std.debug.print(log_prefix ++ fmt ++ "\n", args);
    }
}

fn printError(comptime fmt: []const u8, args: anytype) void {
    std.debug.print(log_prefix ++ fmt ++ "\n", args);
}

// TODO Tests
//
// Lookup non-set variable returns null
// Lookup non-modified variable returns original value
// Stress-test with additions to env via setenv until reallocation occurs
// OTEL_RESOURCE_ATTRIBUTES append to existing value
// OTEL_RESOURCE_ATTRIBUTES without pre-existing value
// JAVA_TOOL_OPTIONS without Jar file at expected location
// JAVA_TOOL_OPTIONS with Jar file at expected location but cannot read due to file permissions
// JAVA_TOOL_OPTIONS happy path
// NODE_OPTIONS without module at expected location
// NODE_OPTIONS with module at expected location but cannot read due to file permissions
// NODE_OPTIONS happy path
