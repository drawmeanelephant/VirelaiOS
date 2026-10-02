//! Independent oracle: unchanged pinned compileBundle, not the guest adapter.
const std = @import("std");
const boris = @import("boris");
const fixture = @import("fixture.zig");
const core = @import("core.zig");

pub fn main(init: std.process.Init) !void {
    var buffer: [4096]u8 = undefined;
    var output = std.Io.File.stdout().writerStreaming(init.io, &buffer);
    if (init.minimal.args.vector.len == 2 and std.mem.eql(u8, std.mem.span(init.minimal.args.vector[1]), "sources")) {
        try output.interface.print("boris-closure-probe 1 {d}\n", .{fixture.files.len});
        for (fixture.files) |file| {
            try output.interface.print("{d} 6 {d}\n", .{ file.path.len, file.bytes.len });
            try output.interface.writeAll(file.path);
            try output.interface.writeAll("source");
            try output.interface.writeAll(file.bytes);
        }
        try output.interface.flush();
        return;
    }
    var result = try boris.compileBundle(init.io, init.gpa, &fixture.files, .{ .html = true, .evidence = true });
    defer result.deinit();
    if (!result.ok()) return error.CompilationFailed;
    try core.write(&output.interface, &result);
    try output.interface.flush();
}
