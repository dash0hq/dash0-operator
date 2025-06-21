// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

// All files with unit tests need to be referenced here:
pub const dotnet = @import("dotnet.zig");
pub const env = @import("env.zig");
pub const injector = @import("injector.zig");
pub const jvm = @import("jvm.zig");
pub const node_js = @import("node_js.zig");
pub const print = @import("print.zig");
pub const res_attrs = @import("resource_attributes.zig");
pub const types = @import("types.zig");
// note: do not add root.zig here, as this would trigger init_array/initEnviron.

test {
    @import("std").testing.refAllDecls(@This());
}
