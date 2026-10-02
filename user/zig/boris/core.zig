//! Use Boris's existing serial memory seam, never substitute a renderer.
const std = @import("std");
const boris = @import("boris");
const policy = @import("policy.zig");

pub fn compile(io: std.Io, allocator: std.mem.Allocator, files: []const boris.SourceFile) !boris.Compilation {
    try policy.inputs(files);
    // compileBundle calls compileHtmlToSink, whose serial contract rejects
    // jobs != 1. compileHtmlSiteMulti (and its Thread.spawn branch) is NOT
    // this entry point. Evidence is the offline target-local chain.
    var result = try boris.compileBundle(io, allocator, files, .{ .html = true, .evidence = true });
    errdefer result.deinit();
    if (!result.ok()) return error.CompilationFailed;
    try policy.outputs(result.artifacts.items());
    return result;
}

/// Length-framed compiler evidence, not a publication format or a site commit.
pub fn write(writer: *std.Io.Writer, result: *const boris.Compilation) !void {
    try writer.print("boris-closure-probe 1 {d}\n", .{result.artifacts.records.items.len});
    for (result.artifacts.records.items) |record| {
        try writer.print("{d} {d} {d}\n", .{ record.path.len, record.media_type.len, record.bytes.len });
        try writer.writeAll(record.path);
        try writer.writeAll(record.media_type);
        try writer.writeAll(record.bytes);
    }
}
