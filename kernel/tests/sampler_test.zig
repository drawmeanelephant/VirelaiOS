const std = @import("std");
const syscall = @import("syscall");
const sampler = syscall.sampler;
const helpers = @import("helpers");

test "sampler: frozen layout and ENOSYS handler" {
    try std.testing.expectEqual(@as(usize, 176), @sizeOf(sampler.Record));
    try std.testing.expectEqual(@as(usize, 72), @sizeOf(sampler.Config));
    try std.testing.expectEqual(@as(usize, 24), @sizeOf(sampler.ReadHeader));
    try std.testing.expectEqual(@as(usize, 32), @offsetOf(sampler.Record, "pc"));
    try std.testing.expectEqual(@as(usize, 48), @offsetOf(sampler.Record, "frames"));
    try std.testing.expectEqual(@as(usize, 16), sampler.frame_depth);
    try std.testing.expectEqual(@as(usize, 100), sampler.rate_hz);
    try std.testing.expectEqual(@as(u64, 1_000_000_000), syscall.timer.period_ns);
    var frame = helpers.task.fresh_frame();
    try std.testing.expectEqual(syscall.error_result(.enosys), sampler.handle(.{ 0, 0, 0, 0, 0, 0 }, &frame));
}
