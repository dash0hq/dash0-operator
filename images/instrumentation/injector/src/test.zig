// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const builtin = @import("builtin");
const std = @import("std");

// All files with unit tests need to be referenced here:
pub const dotnet = @import("dotnet.zig");
pub const env = @import("env.zig");
pub const environ_api = @import("environ_api.zig");
pub const environ_init = @import("environ_init.zig");
pub const jvm = @import("jvm.zig");
pub const node_js = @import("node_js.zig");
pub const print = @import("print.zig");
pub const res_attrs = @import("resource_attributes.zig");
pub const types = @import("types.zig");
// note: do not add root.zig here, as this would trigger init_array/initEnviron.

const testing = std.testing;

test {
    if (builtin.os.tag != .linux) {
        std.debug.print("ERROR: The injector Zig unit tests can only be executed on Linux, found OS {any} " ++
            "instead. Use images/instrumentation/start-injector-dev-container.sh and run the unit tests from " ++
            "within the injector dev container.\n", .{builtin.os.tag});
        std.process.exit(1);
    }

    testing.refAllDecls(@This());
}
