//! C1 gate launcher: sequential slot-28 spawn/wait with B1 file redirection.
//! Test-only, not another allowlisted application or a shell implementation.
const std = @import("std");
const native = @import("sdk").native;
const S = @import("oliver_streams.zig").Streams(native);
pub const panic = std.debug.FullPanic(panicImpl);
export var oliver_probe_state: u64 = 1;
const Request = extern struct { version: u32 = 1, reserved: u32 = 0, sources: [3]u64 };

fn panicImpl(_: []const u8, _: ?usize) noreturn {
    _ = S.writeAll(2, "oliver-probe: panic\n") catch {};
    native.exit(71);
}
fn require(ok: bool) void {
    if (!ok) {
        _ = S.writeAll(2, "oliver-probe: FAIL\n") catch {};
        native.exit(70);
    }
}
fn open(path: []const u8, mode: u64) u64 {
    const fd = native.call(23, .{ @intFromPtr(path.ptr), path.len, mode, 0, 0, 0 });
    require(fd >= 0);
    return @intCast(fd);
}
fn execute(line: []const u8) void {
    var fields = std.mem.splitScalar(u8, line, '\t');
    const tag = fields.next().?;
    const input = fields.next().?;
    const expected = std.fmt.parseInt(i64, fields.next().?, 10) catch unreachable;
    var argblock: [8 * 256]u8 = @splat(0);
    var count: usize = 0;
    while (fields.next()) |arg| {
        require(arg.len <= 256 and count < 8);
        @memcpy(argblock[count * 256 ..][0..arg.len], arg);
        count += 1;
    }
    var out_buf: [64]u8 = undefined;
    var err_buf: [64]u8 = undefined;
    const output = std.fmt.bufPrint(&out_buf, "/host/{s}.OUT", .{tag}) catch unreachable;
    const errors = std.fmt.bufPrint(&err_buf, "/host/{s}.ERR", .{tag}) catch unreachable;
    const req = Request{ .sources = .{ open(input, 1), open(output, 6), open(errors, 6) } };
    const name = "OLIVER.BIN";
    const pid = native.call(28, .{ @intFromPtr(name.ptr), name.len, @intFromPtr(&argblock), (@as(u64, 1) << 63) | count, @intFromPtr(&req), @sizeOf(Request) });
    if (expected < 0) {
        require(pid == expected);
        for (req.sources) |fd| require(native.call(26, .{ fd, 0, 0, 0, 0, 0 }) == 0);
    } else {
        if (pid < 0) {
            var buf: [128]u8 = undefined;
            const text = std.fmt.bufPrint(&buf, "oliver-probe: spawn {s} result={d}\n", .{ tag, pid }) catch unreachable;
            S.writeAll(2, text) catch unreachable;
            require(false);
        }
        const status = native.call(8, .{ @intCast(pid), 0, 0, 0, 0, 0 });
        if (status != expected) {
            var buf: [128]u8 = undefined;
            const text = std.fmt.bufPrint(&buf, "oliver-probe: wait {s} status={d} expected={d}\n", .{ tag, status, expected }) catch unreachable;
            S.writeAll(2, text) catch unreachable;
            require(false);
        }
        // wait establishes exit, not idle-reap completion. Let the reaper
        // return process/stack/arena records before the next reservation.
        _ = native.call(4, .{ 2, 0, 0, 0, 0, 0 });
        if (expected == 0) {
            const fd = open("/host/OLIVER.METRICS", 1);
            var receipt: [160]u8 = undefined;
            const bytes = S.readBounded(fd, &receipt) catch unreachable;
            S.close(fd) catch unreachable;
            var message: [256]u8 = undefined;
            const text = std.fmt.bufPrint(&message, "{s}: {s}", .{ tag, bytes }) catch unreachable;
            S.writeAll(1, text) catch unreachable;
        }
    }
    var message: [128]u8 = undefined;
    const text = std.fmt.bufPrint(&message, "oliver-probe: ok {s}\n", .{tag}) catch unreachable;
    S.writeAll(1, text) catch unreachable;
}
export fn _start(argc: usize, argv: ?[*]const [256]u8) callconv(.c) noreturn {
    oliver_probe_state +%= 1;
    require(argc == 2 and argv != null);
    const group = std.mem.sliceTo(&argv.?[1], 0);
    require(group.len == 1 and group[0] >= '1' and group[0] <= '5');
    var path: [32]u8 = undefined;
    const name = std.fmt.bufPrint(&path, "/host/OLIVER.CASES.{s}", .{group}) catch unreachable;
    const fd = open(name, 1);
    var buffer: [16384]u8 = undefined;
    const bytes = S.readBounded(fd, &buffer) catch unreachable;
    S.close(fd) catch unreachable;
    var lines = std.mem.splitScalar(u8, bytes, '\n');
    var count: usize = 0;
    while (lines.next()) |line| {
        if (line.len == 0) continue;
        execute(line);
        count += 1;
    }
    var msg: [96]u8 = undefined;
    const text = std.fmt.bufPrint(&msg, "oliver-probe: done cases={d}\n", .{count}) catch unreachable;
    S.writeAll(1, text) catch unreachable;
    native.exit(0);
}
