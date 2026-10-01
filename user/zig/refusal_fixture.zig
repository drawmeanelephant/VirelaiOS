//! Host subprocess proof of no-error-return APIs. No native syscalls are mocked
//! as successful: this driver must terminate before it can call one.
const std = @import("std");
const Backend = @import("io.zig").Backend(struct {
    pub fn instance() *anyopaque {
        @panic("missing test userdata");
    }
    pub fn diagnostic(bytes: []const u8) void {
        std.debug.print("{s}", .{bytes});
    }
    pub fn fatal(name: []const u8) noreturn {
        std.debug.print("{s}\n", .{name});
        std.process.exit(70);
    }
    pub fn call(number: u64, _: [6]u64) i64 {
        if (number == 72) return -1; // Entropy failure injection, never fallback bytes.
        std.process.exit(99); // A refusal must never reach the system.
    }
});

pub fn main(init: std.process.Init) void {
    var backend: Backend = .{};
    const io = backend.io();
    var args = init.minimal.args.iterate();
    _ = args.next();
    const name = args.next() orelse std.process.exit(2);
    const file: std.Io.File = .{ .handle = -2, .flags = .{ .nonblocking = false } };
    if (std.mem.eql(u8, name, "now")) {
        _ = std.Io.Clock.real.now(io);
    } else if (std.mem.eql(u8, name, "futexWait")) {
        const word: u32 = 0;
        io.vtable.futexWaitUncancelable(io.userdata, &word, 0);
    } else if (std.mem.eql(u8, name, "close")) {
        file.close(io);
    } else if (std.mem.eql(u8, name, "unlock")) {
        io.vtable.fileUnlock(io.userdata, file);
    } else if (std.mem.eql(u8, name, "tty")) {
        _ = file.isTty(io) catch std.process.exit(98);
    } else if (std.mem.eql(u8, name, "netClose")) {
        io.vtable.netClose(io.userdata, &.{0});
    } else if (std.mem.eql(u8, name, "random")) {
        var bytes: [8]u8 = undefined;
        io.random(&bytes);
    } else std.process.exit(2);
    std.process.exit(0); // Python asserts this is never reached.
}
