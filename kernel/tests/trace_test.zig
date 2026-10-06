const std = @import("std");
const syscall = @import("syscall");
const trace = syscall.trace;
const helpers = @import("helpers");

var output: [512]u8 = undefined;
var output_len: usize = 0;

fn writer(bytes: []const u8) void {
    @memcpy(output[output_len..][0..bytes.len], bytes);
    output_len += bytes.len;
}

test "trace: frozen layout and ENOSYS handler" {
    try std.testing.expectEqual(@as(usize, 896), @sizeOf(trace.Record));
    try std.testing.expectEqual(@as(usize, 88), @sizeOf(trace.Config));
    try std.testing.expectEqual(@as(usize, 32), @sizeOf(trace.ExecRequest));
    try std.testing.expectEqual(@as(usize, 24), @sizeOf(trace.ReadHeader));
    try std.testing.expectEqual(@as(usize, 40), @offsetOf(trace.Record, "args"));
    try std.testing.expectEqual(@as(usize, 88), @offsetOf(trace.Record, "result"));
    try std.testing.expectEqual(@as(usize, 96), @offsetOf(trace.Record, "errno"));
    try std.testing.expectEqual(@as(usize, 128), @offsetOf(trace.Record, "strings"));
    var frame = helpers.task.fresh_frame();
    try std.testing.expectEqual(syscall.error_result(.enosys), trace.handle(.{ 0, 0, 0, 0, 0, 0 }, &frame));
}

test "trace: monitor compatibility output is byte-for-byte unchanged" {
    syscall.userspace.init();
    syscall.init(writer);
    _ = syscall.scheduler.init();
    _ = syscall.scheduler.register_worker(0x2000);
    _ = syscall.scheduler.register_user(0x3000, 0);
    syscall.scheduler.start();
    try std.testing.expect(syscall.scheduler.yield_current());
    syscall.strace_pid = 0;
    defer syscall.strace_pid = null;
    var frame = helpers.task.fresh_frame();
    output_len = 0;
    _ = syscall.dispatch(syscall.sys_ping, .{ 9, 10, 11, 12, 13, 14 }, &frame);
    try std.testing.expectEqualStrings("[strace 0] sys_ping(0x9, 0xa, 0xb) = 0x9\n", output[0..output_len]);
    const call = trace.before(syscall.sys_exit, .{ 42, 0, 0, 0, 0, 0 }, &frame);
    _ = call;
    try std.testing.expectEqualStrings(
        "[strace 0] sys_ping(0x9, 0xa, 0xb) = 0x9\n[strace 0] sys_exit(0x2a) = \xe2\x80\x94\n",
        output[0..output_len],
    );
    const old_len = output_len;
    for ([_]u64{ 70, 71 }) |slot| {
        const redacted_call = trace.before(slot, .{ 1, 2, 3, 4, 5, 6 }, &frame);
        trace.after(redacted_call, slot, .{ 1, 2, 3, 4, 5, 6 }, 7);
    }
    try std.testing.expectEqual(old_len, output_len);
    syscall.strace_pid = null;
    _ = syscall.dispatch(syscall.sys_ping, .{ 9, 0, 0, 0, 0, 0 }, &frame);
    try std.testing.expectEqual(old_len, output_len);
}
