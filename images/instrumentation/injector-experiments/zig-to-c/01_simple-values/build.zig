// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

pub fn build(b: *std.Build) !void {
    const optimize = std.builtin.OptimizeMode.ReleaseSafe;

    var target_cpu_arch = std.Target.Cpu.Arch.aarch64;
    var target_cpu_model = std.Target.Cpu.Model.generic(std.Target.Cpu.Arch.aarch64);

    if (b.option([]const u8, "cpu-arch", "The system architecture to compile the library for; valid options are 'amd64' and 'arm64' (default)")) |val| {
        if (std.mem.eql(u8, "arm64", val)) {
            // Already the default
        } else if (std.mem.eql(u8, "amd64", val)) {
            target_cpu_arch = std.Target.Cpu.Arch.x86_64;
            target_cpu_model = std.Target.Cpu.Model.generic(std.Target.Cpu.Arch.x86_64);
        } else {
            return error.UnsupportedArchitecturError;
        }
    }

    const target = b.resolveTargetQuery(.{
        .cpu_arch = target_cpu_arch,
        // Skip cpu model detection because the automatic detection for transpiling fails in build
        .cpu_model = .{ .explicit = target_cpu_model },
        .os_tag = .linux,
    });

    const lib_mod = b.createModule(.{
        .root_source_file = b.path("root.zig"),
        .target = target,
        .optimize = optimize,
        .link_libc = false,
        .pic = true,
        .strip = false,
    });

    // Create a dynamically linked library based on the module created above.
    // This creates a `std.Build.Step.Compile`, which is the build step responsible
    // for actually invoking the compiler.
    const lib = b.addLibrary(.{
        .linkage = .dynamic,
        .name = "symbols",
        .root_module = lib_mod,
    });

    b.getInstallStep().dependOn(&b.addInstallArtifact(lib, .{ .dest_dir = .{ .override = .{ .custom = "." } } }).step);

    var copy_library_to_bin = b.step("copy_file", "Copy library file");
    copy_library_to_bin.makeFn = copyLibraryFile;

    // make the copy step depend on the install step, which then makes it transitively depend on the compile step
    copy_library_to_bin.dependOn(b.getInstallStep());

    // Make copying the shared library binary to its final location the default step. This wil also implictly
    // trigger building the library as a dependent build step.
    b.default_step = copy_library_to_bin;
}

fn copyLibraryFile(step: *std.Build.Step, _: std.Build.Step.MakeOptions) anyerror!void {
    const source_path = step.owner.pathFromRoot("./zig-out/libsymbols.so");
    const dest_path = step.owner.pathFromRoot("./libsymbols.so");
    try std.fs.copyFileAbsolute(source_path, dest_path, .{});
}
