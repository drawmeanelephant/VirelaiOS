const std = @import("std");
const syscall = @import("syscall");
const memstat = syscall.memstat;
const helpers = @import("helpers");

test "memstat: frozen receipt extension and ENOSYS handler" {
    try std.testing.expectEqual(@as(usize, 240), @sizeOf(memstat.Record));
    try std.testing.expectEqual(@as(usize, 24), @offsetOf(memstat.Record, "peak_pages"));
    try std.testing.expectEqual(@as(usize, 64), @offsetOf(memstat.Record, "live_pages"));
    try std.testing.expectEqual(@as(usize, 112), @offsetOf(memstat.Record, "region_sizes"));
    try std.testing.expectEqual(syscall.process.max_mmap_regions, memstat.max_regions);
    var frame = helpers.task.fresh_frame();
    try std.testing.expectEqual(syscall.error_result(.enosys), memstat.handle(.{ 0, 0, 0, 0, 0, 0 }, &frame));
}
