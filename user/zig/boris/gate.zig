//! Acceptance-only B1 stream launcher. Its output files are harness captures,
//! never Boris staging/publication. Wait every child before advancing.
const std = @import("std");
const native = @import("native");
const startup = @import("startup");
pub const panic = std.debug.FullPanic(panicImpl);
export var gate_data: u64 = 1;
var manifest: [16384]u8 = undefined;
const Request = extern struct { version: u32 = 1, reserved: u32 = 0, sources: [3]u64 };
fn print(bytes: []const u8) void {
    var at: usize = 0;
    while (at < bytes.len) {
        const n = native.call(1, .{ 1, @intFromPtr(bytes.ptr) + at, bytes.len - at, 0, 0, 0 });
        require(n > 0 and n <= bytes.len - at);
        at += @intCast(n);
    }
}
fn panicImpl(_: []const u8, _: ?usize) noreturn {
    print("boris-gate: FAIL\n");
    native.exit(70);
}
fn require(ok: bool) void {
    if (!ok) @panic("gate");
}
fn open(path: []const u8, flags: u64) u64 {
    const n = native.call(23, .{ @intFromPtr(path.ptr), path.len, flags, 0, 0, 0 });
    require(n >= 0);
    return @intCast(n);
}
export fn _start(argc: usize, argv: usize) callconv(.c) noreturn {
    gate_data +%= 1;
    const pointer: [*]const u8 = @ptrFromInt(argv);
    const launch = startup.Startup.parse(argc, pointer[0..startup.block_bytes]) catch @panic("startup");
    require(launch.argc == 2);
    const batch = std.fmt.parseInt(usize, launch.args[1], 10) catch @panic("batch");
    const fd = open("/host/BORIS.CASES", 1);
    var length: usize = 0;
    while (true) {
        require(length < manifest.len);
        const n = native.call(24, .{ fd, @intFromPtr(&manifest) + length, @min(2048, manifest.len - length), 0, 0, 0 });
        require(n >= 0 and n <= @min(2048, manifest.len - length));
        if (n == 0) break;
        length += @intCast(n);
    }
    require(native.call(26, .{ fd, 0, 0, 0, 0, 0 }) == 0);
    var lines = std.mem.tokenizeScalar(u8, manifest[0..length], '\n');
    var index: usize = 0;
    var count: usize = 0;
    while (lines.next()) |line| {
        if (index < batch * 10) {
            index += 1;
            continue;
        }
        if (count == 10) break;
        var columns = std.mem.splitScalar(u8, line, '\t');
        const id = columns.next() orelse @panic("id");
        const binary = columns.next() orelse @panic("binary");
        const status = std.fmt.parseInt(u8, columns.next() orelse @panic("status"), 10) catch @panic("status");
        var args: [8][]const u8 = undefined;
        args[0] = binary;
        var n: usize = 1;
        while (columns.next()) |arg| {
            require(n < args.len);
            args[n] = arg;
            n += 1;
        }
        var block: [startup.block_bytes]u8 = undefined;
        startup.pack(args[0..n], &.{}, &block) catch @panic("pack");
        var out_buffer: [64]u8 = undefined;
        var err_buffer: [64]u8 = undefined;
        const out = std.fmt.bufPrint(&out_buffer, "/host/{s}.out", .{id}) catch @panic("path");
        const err = std.fmt.bufPrint(&err_buffer, "/host/{s}.err", .{id}) catch @panic("path");
        const request = Request{ .sources = .{ std.math.maxInt(u64) - 1, open(out, 6), open(err, 6) } };
        const child = native.call(28, .{
            @intFromPtr(binary.ptr),       binary.len,            @intFromPtr(&block) + 256,
            (@as(u64, 1) << 63) | (n - 1), @intFromPtr(&request), @sizeOf(Request),
        });
        require(child >= 0);
        if (native.call(8, .{ @intCast(child), 0, 0, 0, 0, 0 }) != status) {
            var report: [96]u8 = undefined;
            print(std.fmt.bufPrint(&report, "boris-gate: unexpected status {s}\n", .{id}) catch @panic("report"));
            const error_fd = open(err, 1);
            var diagnostic: [2048]u8 = undefined;
            const bytes = native.call(24, .{ error_fd, @intFromPtr(&diagnostic), diagnostic.len, 0, 0, 0 });
            if (bytes > 0 and bytes <= diagnostic.len) print(diagnostic[0..@intCast(bytes)]);
            _ = native.call(26, .{ error_fd, 0, 0, 0, 0, 0 });
            @panic("status");
        }
        var report: [96]u8 = undefined;
        print(std.fmt.bufPrint(&report, "boris-gate: case {s}\n", .{id}) catch @panic("report"));
        count += 1;
    }
    require(count != 0);
    var report: [64]u8 = undefined;
    print(std.fmt.bufPrint(&report, "boris-gate: done cases={d}\n", .{count}) catch @panic("report"));
    native.exit(0);
}
