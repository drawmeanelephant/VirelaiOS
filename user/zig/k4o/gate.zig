//! Sequential acceptance launcher, not installed or exposed by the CLI.
const std = @import("std");
const native = @import("native");
const startup = @import("startup");
pub const panic = std.debug.FullPanic(panicImpl);
export var gate_data: u64 = 1;
var manifest: [64 * 1024]u8 = undefined;
const Request = extern struct { version: u32 = 1, reserved: u32 = 0, sources: [3]u64 };

fn panicImpl(_: []const u8, _: ?usize) noreturn {
    _ = native.call(1, .{ 2, @intFromPtr("k4o-gate: FAIL\n"), 15, 0, 0, 0 });
    native.exit(70);
}
fn require(ok: bool) void {
    if (!ok) @panic("gate");
}
fn open(path: []const u8, flags: u64) u64 {
    const result = native.call(23, .{ @intFromPtr(path.ptr), path.len, flags, 0, 0, 0 });
    require(result >= 0);
    return @intCast(result);
}
fn print(bytes: []const u8) void {
    var at: usize = 0;
    while (at < bytes.len) {
        const n = native.call(1, .{ 1, @intFromPtr(bytes.ptr) + at, bytes.len - at, 0, 0, 0 });
        require(n > 0 and n <= bytes.len - at);
        at += @intCast(n);
    }
}
export fn _start(entry_argc: usize, argv: usize) callconv(.c) noreturn {
    gate_data +%= 1;
    const pointer: [*]const u8 = @ptrFromInt(argv);
    const launch = startup.Startup.parse(entry_argc, pointer[0..startup.block_bytes]) catch @panic("startup");
    require(launch.argc == 2);
    const batch = std.fmt.parseInt(usize, launch.args[1], 10) catch @panic("batch");
    require(batch < 18);
    const fd = open("/host/K4O.CASES", 1);
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
    var count: usize = 0;
    var index: usize = 0;
    while (lines.next()) |line| {
        if (index < batch * 12) {
            index += 1;
            continue;
        }
        if (count == 12) break;
        var columns = std.mem.splitScalar(u8, line, '\t');
        const id = columns.next() orelse @panic("id");
        const binary = columns.next() orelse @panic("binary");
        const status = std.fmt.parseInt(u8, columns.next() orelse @panic("status"), 10) catch @panic("status");
        var args: [8][]const u8 = undefined;
        args[0] = binary;
        var argc: usize = 1;
        while (columns.next()) |arg| {
            require(argc < args.len);
            args[argc] = arg;
            argc += 1;
        }
        var block: [startup.block_bytes]u8 = undefined;
        startup.pack(args[0..argc], &.{}, &block) catch @panic("pack");
        var out_buffer: [64]u8 = undefined;
        var err_buffer: [64]u8 = undefined;
        const out = std.fmt.bufPrint(&out_buffer, "/host/{s}.out", .{id}) catch @panic("path");
        const err = std.fmt.bufPrint(&err_buffer, "/host/{s}.err", .{id}) catch @panic("path");
        const request = Request{ .sources = .{ std.math.maxInt(u64) - 1, open(out, 6), open(err, 6) } };
        const child = native.call(28, .{
            @intFromPtr(binary.ptr),          binary.len,            @intFromPtr(&block) + 256,
            (@as(u64, 1) << 63) | (argc - 1), @intFromPtr(&request), @sizeOf(Request),
        });
        if (child < 0) {
            var refusal: [128]u8 = undefined;
            print(std.fmt.bufPrint(&refusal, "k4o-gate: spawn refused case={s} native={d}\n", .{ id, child }) catch @panic("spawn report"));
            @panic("spawn");
        }
        const actual = native.call(8, .{ @intCast(child), 0, 0, 0, 0, 0 });
        if (actual != status) {
            print("k4o-gate: unexpected status case=");
            print(id);
            print("\n");
            const error_fd = open(err, 1);
            var diagnostic: [2048]u8 = undefined;
            const n = native.call(24, .{ error_fd, @intFromPtr(&diagnostic), diagnostic.len, 0, 0, 0 });
            if (n > 0 and n <= diagnostic.len) print(diagnostic[0..@intCast(n)]);
            _ = native.call(26, .{ error_fd, 0, 0, 0, 0, 0 });
            @panic("child status");
        }
        var case_report: [96]u8 = undefined;
        print(std.fmt.bufPrint(&case_report, "k4o-gate: case {s}\n", .{id}) catch @panic("case report"));
        count += 1;
    }
    // Native argv marshalling must refuse before a child is published.
    var block: [startup.block_bytes]u8 = @splat('x');
    const binary = "K4O.BIN";
    require(native.call(28, .{ @intFromPtr(binary.ptr), binary.len, @intFromPtr(&block), 1, 0, 0 }) == -1);
    @memset(&block, 0); // Valid empty slots: isolate count refusal from length.
    require(native.call(28, .{ @intFromPtr(binary.ptr), binary.len, @intFromPtr(&block), 8, 0, 0 }) == -1);
    print("k4o-gate: ArgumentLimit native length/count\n");
    var report: [64]u8 = undefined;
    print(std.fmt.bufPrint(&report, "k4o-gate: done cases={d}\n", .{count}) catch @panic("report"));
    native.exit(0);
}
