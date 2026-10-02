//! Independent host oracle: directly calls the unchanged upstream synth,
//! not the guest policy. No upstream host main/UI/audio/command imports.
const std = @import("std");
const synth = @import("upstream/synth.zig");
pub fn main(init: std.process.Init) !void {
    var args = init.minimal.args.iterate();
    _ = args.next();
    const kind = args.next() orelse return error.Usage;
    const input = args.next() orelse return error.Usage;
    const path = args.next() orelse return error.Usage;
    const wav = if (std.mem.eql(u8, kind, "phrase"))
        try synth.renderPhraseWav(init.gpa, input)
    else
        try synth.renderShuziWav(init.gpa, try std.fmt.parseInt(u64, input, 10));
    defer init.gpa.free(wav);
    const file = try std.Io.Dir.cwd().createFile(init.io, path, .{});
    defer file.close(init.io);
    try file.writeStreamingAll(init.io, wav);
}
