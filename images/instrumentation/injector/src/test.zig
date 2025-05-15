// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// All files with unit tests need to be referenced here:
pub const dotnet = @import("dotnet.zig");
pub const jvm = @import("jvm.zig");
pub const node_js = @import("node_js.zig");
pub const print = @import("print.zig");
pub const print_test = @import("print_test.zig");
pub const res_attrs = @import("resource_attributes.zig");
pub const res_attrs_test = @import("resource_attributes_test.zig");
pub const root = @import("root.zig");
pub const types = @import("types.zig");

test {
    @import("std").testing.refAllDecls(@This());
}
