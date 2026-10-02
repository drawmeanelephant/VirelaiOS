//! C4 headless finite synth. Go still owns the seat and the boot default.
const std = @import("std");
const sdk = @import("runtime.zig");
const port = @import("fart/port.zig");
const pcm = @import("fart/pcm.zig");
pub const virelai = sdk.platform;
pub const os = sdk.os;
pub const std_options = sdk.std_options;
pub const std_options_debug_io = sdk.std_options_debug_io;
pub const std_options_FilePermissions = sdk.std_options_FilePermissions;
pub const std_options_cwd = sdk.std_options_cwd;
pub const panic = std.debug.FullPanic(sdk.panic);

comptime {
    @import("entry.zig").exportEntry(run);
}

fn stream(fd: usize, bytes: []const u8) !void {
    var done: usize = 0;
    while (done < bytes.len) {
        const chunk = bytes[done..][0..@min(256, bytes.len - done)];
        const n = sdk.native.call(1, .{ fd, @intFromPtr(chunk.ptr), chunk.len, 0, 0, 0 });
        if (n <= 0 or n > chunk.len) return error.StreamWriteFailed;
        done += @intCast(n);
    }
}

fn run(args: *const sdk.startup.Startup) !void {
    app(args) catch |err| {
        // A3's standard Io tokens remain unbound after B1. Use B1's native
        // stderr here, not the SDK's pre-B1 console diagnostic fallback.
        stream(2, "fart-synth: ") catch sdk.native.exit(70);
        stream(2, @errorName(err)) catch sdk.native.exit(70);
        stream(2, "\n") catch sdk.native.exit(70);
        sdk.finish(70);
    };
}

fn app(args: *const sdk.startup.Startup) !void {
    if (args.argc == 2 and std.mem.eql(u8, args.args[1], "--help")) {
        try stream(1, "FARTSYN.BIN (--phrase TEXT | --seed U64) (--wav PATH | --stdout | --play)\n");
        return;
    }
    if (args.argc < 4 or args.argc > 5) return error.Usage;
    const input: port.Input = if (std.mem.eql(u8, args.args[1], "--phrase"))
        .{ .phrase = args.args[2] }
    else if (std.mem.eql(u8, args.args[1], "--seed"))
        .{ .seed = std.fmt.parseInt(u64, args.args[2], 10) catch return error.InvalidSeed }
    else
        return error.UnsupportedOperation;
    const output = args.args[3];
    const file_mode = std.mem.eql(u8, output, "--wav");
    const stdout_mode = std.mem.eql(u8, output, "--stdout");
    const play_mode = std.mem.eql(u8, output, "--play");
    if (!file_mode and !stdout_mode and !play_mode) return error.UnsupportedOperation;
    if (args.argc != (if (file_mode) @as(usize, 5) else 4)) return error.Usage;
    try sdk.initialize(port.arena_bytes);
    const a = std.heap.page_allocator;
    const wav = try port.render(a, input);
    defer a.free(wav);
    if (file_mode) {
        // Render and check every resource/output bound before truncating a file.
        const file = try std.Io.Dir.cwd().createFile(sdk.io, args.args[4], .{});
        try sdk.io_helpers.writeBounded(file, sdk.io, wav, port.max_output);
        try file.sync(sdk.io);
        file.close(sdk.io);
    } else if (stdout_mode) {
        try stream(1, wav);
    } else {
        const info = try pcm.query(sdk.native.call);
        const raw = try pcm.convert(a, wav[44..], info);
        defer a.free(raw);
        var digest: [32]u8 = undefined;
        std.crypto.hash.sha2.Sha256.hash(raw, &digest, .{});
        const hex = std.fmt.bytesToHex(digest, .lower);
        var report: [160]u8 = undefined;
        try stream(2, try std.fmt.bufPrint(&report, "fart-synth: pcm fmt={d} rate={d} ch={d} bytes={d} sha256={s}\n", .{
            info.format, info.rate, info.channels, raw.len, hex,
        }));
        try pcm.submit(sdk.native.call, raw);
    }
    var report: [160]u8 = undefined;
    try stream(2, try std.fmt.bufPrint(&report, "fart-synth: wav={d} arena_peak={d} stack_high_water={d} files={d}\n", .{
        wav.len, sdk.currentArena().peak(), sdk.stackHighWater(), @intFromBool(file_mode),
    }));
}
