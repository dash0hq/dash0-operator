// SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const std = @import("std");

pub const InjectorBuildError = error{UnsupportedArchitecturError};

// Although this function looks imperative, note that its job is to
// declaratively construct a build graph that will be executed by an external
// runner.
pub fn build(b: *std.Build) !void {
    // Standard optimization options allow the person running `zig build` to select
    // between Debug, ReleaseSafe, ReleaseFast, and ReleaseSmall. Here we do not
    // set a preferred release mode, allowing the user to decide how to optimize.
    const optimize = b.standardOptimizeOption(.{});

    var targetCpuArch = std.Target.Cpu.Arch.aarch64;
    var targetCpuModel = std.Target.Cpu.Model.generic(std.Target.Cpu.Arch.aarch64);

    if (b.option([]const u8, "cpu-arch", "The system architecture to compile the injector for; valid options are 'amd64' and 'arm64' (default)")) |val| {
        if (std.mem.eql(u8, "arm64", val)) {
            // Already the default
        } else if (std.mem.eql(u8, "amd64", val)) {
            targetCpuArch = std.Target.Cpu.Arch.x86_64;
            targetCpuModel = std.Target.Cpu.Model.generic(std.Target.Cpu.Arch.x86_64);
        } else {
            return error.UnsupportedArchitecturError;
        }
    }

    const target = b.resolveTargetQuery(.{
        .cpu_arch = targetCpuArch,
        // Skip cpu model detection because the automatic detection for transpiling fails in build
        .cpu_model = .{ .explicit = targetCpuModel },
        .os_tag = .linux,
    });

    // Creates a "module", which represents a collection of source files alongside
    // some compilation options, such as optimization mode and linked system libraries.
    // Every executable or library we compile will be based on one or more modules.
    const lib_mod = b.createModule(.{
        // `root_source_file` is the Zig "entry point" of the module. If a module
        // only contains e.g. external object files, you can make this `null`.
        // In this case the main source file is merely a path, however, in more
        // complicated build scripts, this could be a generated file.
        .root_source_file = b.path("src/root.zig"),
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
        .name = "injector",
        .root_module = lib_mod,
    });
    lib.setVersionScript(b.path("src/dash0_injector.exports.map"));

    b.getInstallStep().dependOn(&b.addInstallArtifact(lib, .{ .dest_dir = .{ .override = .{ .custom = "." } } }).step);

    var copy_injector_to_bin = b.step("copy_file", "Copy injector file");
    copy_injector_to_bin.makeFn = copyInjectorFile;

    // make the copy step depend in the install step, which then makes it transitively depend on the compile step
    copy_injector_to_bin.dependOn(b.getInstallStep());

    // Make copying the injector shared library binary to its final location the default step. This wil also implictly
    // trigger building the library as a dependent build step.
    b.default_step = copy_injector_to_bin;

    // TESTING
    const testTarget = b.standardTargetOptions(.{});
    const test_mod = b.createModule(.{
        .root_source_file = b.path("src/test.zig"),
        .target = testTarget,
        .optimize = optimize,
        // in contrast to the production module which uses link_libc = false, we deliberately link libc here, to satisfy
        // the "extern var __environ: [*]u8;" dependency.
        .link_libc = true,
        .pic = true,
        .strip = false,
    });
    const unit_tests = b.addTest(.{
        .root_module = test_mod,
    });
    const run_unit_tests = b.addRunArtifact(unit_tests);
    const test_step = b.step("test", "Run unit tests");
    test_step.dependOn(&run_unit_tests.step);
}

fn copyInjectorFile(step: *std.Build.Step, _: std.Build.Step.MakeOptions) anyerror!void {
    const source_path = step.owner.pathFromRoot("./zig-out/libinjector.so");
    const dest_path = step.owner.pathFromRoot("./dash0_injector.so");
    try std.fs.copyFileAbsolute(source_path, dest_path, .{});
}
