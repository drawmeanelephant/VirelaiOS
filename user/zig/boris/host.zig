//! Independent oracle: unchanged pinned compileBundle, not the guest adapter.
const std = @import("std");
const boris = @import("boris");
const fixture = @import("fixture.zig");
const core = @import("core.zig");

pub fn main(init: std.process.Init) !void {
    var result = try boris.compileBundle(init.io, init.gpa, &fixture.files, .{ .html = true, .evidence = true });
    defer result.deinit();
    if (!result.ok()) return error.CompilationFailed;
    var buffer: [4096]u8 = undefined;
    var output = std.Io.File.stdout().writerStreaming(init.io, &buffer);
    try core.write(&output.interface, &result);
    try output.interface.flush();
}
