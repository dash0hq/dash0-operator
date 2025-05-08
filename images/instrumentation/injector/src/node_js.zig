// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

const alloc = @import("./allocator.zig");
const print = @import("./print.zig");
const types = @import("./types.zig");

const otel_nodejs_module = "/__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry";
const node_options_addition = "--require " ++ otel_nodejs_module;

pub fn getModifiedNodeOptionsValue(name: [:0]const u8, original_value: ?[:0]const u8) ?types.NullTerminatedString {
    // Check the existence of the Node module: requiring or importing a module
    // that does not exist or cannot be opened will crash the Node.js process
    // with an 'ERR_MODULE_NOT_FOUND' error.
    std.fs.cwd().access(otel_nodejs_module, .{}) catch |err| {
        print.printError("Skipping injection of OTel Node.js module in 'NODE_OPTIONS' because of an issue accessing the Node.js module at {s}: {}", .{ otel_nodejs_module, err });
        return null;
    };

    if (original_value) |val| {
        // If NODE_OPTIONS is already set, prefix our --require to the original value.
        // Note: We must never free the return_buffer, or we may cause a USE_AFTER_FREE memory corruption in the
        // parent process.
        const return_buffer = std.fmt.allocPrintZ(alloc.allocator, "{s} {s}", .{ node_options_addition, val }) catch |err| {
            print.printError("Cannot allocate memory to manipulate the value of '{s}': {}", .{ name, err });
            return null;
        };
        return return_buffer.ptr;
    }

    return node_options_addition[0..].ptr;
}
