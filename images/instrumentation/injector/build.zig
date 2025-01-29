const std = @import("std");

pub const InjectorBuildError = error{UnsupportedArchitecturError};

// Although this function looks imperative, note that its job is to
// declaratively construct a build graph that will be executed by an external
// runner.

pub fn build(b: *std.Build) !void {
    const optimize = b.standardOptimizeOption(.{});

    var targetCpuArch = std.Target.Cpu.Arch.aarch64;
    var targetCpuModel = std.Target.Cpu.Model.generic(std.Target.Cpu.Arch.aarch64);

    if (b.option([]const u8, "cpu-arch", "The system architecture to compile the injector for; valid options are 'amd64' and 'arch64' (default)")) |val| {
        if (std.mem.eql(u8, "arm64", val)) {
            // Nothing to do
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

    const lib = b.addSharedLibrary(.{
        .name = "injector",
        // In this case the main source file is merely a path, however, in more
        // complicated build scripts, this could be a generated file.
        .root_source_file = b.path("src/dash0_injector.zig"),
        .target = target,
        .optimize = optimize,
        .link_libc = false,
        .pic = true,
        .strip = false,
    });
    lib.setVersionScript(b.path("src/dash0_injector.exports.map"));

    b.getInstallStep().dependOn(&b.addInstallArtifact(lib, .{ .dest_dir = .{ .override = .{ .custom = "." } } }).step);

    var copy_injector_to_bin = b.step("copy_file", "Copy injector file");
    copy_injector_to_bin.makeFn = copyInjectorFile;
    copy_injector_to_bin.dependOn(b.getInstallStep());

    b.default_step = copy_injector_to_bin;
}

fn copyInjectorFile(step: *std.Build.Step, _: std.Progress.Node) anyerror!void {
    const source_path = step.owner.pathFromRoot("./zig-out/libinjector.so");
    const dest_path = step.owner.pathFromRoot("./bin/dash0_injector.so");

    try std.fs.copyFileAbsolute(source_path, dest_path, .{});
}
