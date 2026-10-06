const std = @import("std");
const syscall = @import("syscall");
const sampler = syscall.sampler;
const helpers = @import("helpers");

test "sampler: frozen layout and non-process refusal" {
    try std.testing.expectEqual(@as(usize, 176), @sizeOf(sampler.Record));
    try std.testing.expectEqual(@as(usize, 72), @sizeOf(sampler.Config));
    try std.testing.expectEqual(@as(usize, 24), @sizeOf(sampler.ReadHeader));
    try std.testing.expectEqual(@as(usize, 32), @offsetOf(sampler.Record, "pc"));
    try std.testing.expectEqual(@as(usize, 48), @offsetOf(sampler.Record, "frames"));
    try std.testing.expectEqual(@as(usize, 16), sampler.frame_depth);
    try std.testing.expectEqual(@as(usize, 100), sampler.rate_hz);
    try std.testing.expectEqual(@as(u64, 1_000_000_000), syscall.timer.period_ns);
    var frame = helpers.task.fresh_frame();
    syscall.process.init();
    try std.testing.expectEqual(syscall.error_result(.einval), sampler.handle(.{ 0, 0, 0, 0, 0, 0 }, &frame));
}

var frames: [20][16]u8 = undefined;
var reads: usize = 0;
var fault_at: ?u64 = null;

fn read_frame(address: u64, bytes: *[16]u8) bool {
    reads += 1;
    if (fault_at == address or address < 0x1000 or address >= 0x1000 + frames.len * 16) return false;
    bytes.* = frames[(address - 0x1000) / 16];
    return true;
}

fn chain() void {
    reads = 0;
    fault_at = null;
    for (&frames, 0..) |*frame, i| {
        std.mem.writeInt(u64, frame[0..8], 0x1000 + (i + 1) * 16, .little);
        std.mem.writeInt(u64, frame[8..16], 0x4000 + i * 4, .little);
    }
}

test "sampler: frame walk caps reads and zeroes unused wire frames" {
    chain();
    var record = std.mem.zeroes(sampler.Record);
    sampler.walk(0x1000, 0x8888, read_frame, &record);
    try std.testing.expectEqual(@as(u32, 16), record.depth);
    try std.testing.expectEqual(@as(usize, 16), reads);
    try std.testing.expectEqual(@as(u64, 0x4000), record.frames[0]);
    try std.testing.expectEqual(@as(u64, 0x403c), record.frames[15]);
}

test "sampler: hostile FP, copy fault, cycle and non-increasing chain stop" {
    for ([_]u64{ 0, 0x1001, std.math.maxInt(u64) - 15, 0xffff_0000 }) |fp| {
        chain();
        var record = std.mem.zeroes(sampler.Record);
        sampler.walk(fp, 0, read_frame, &record);
        try std.testing.expectEqual(@as(u32, 0), record.depth);
        try std.testing.expect(reads <= 1);
    }
    chain();
    fault_at = 0x1010;
    var record = std.mem.zeroes(sampler.Record);
    sampler.walk(0x1000, 0, read_frame, &record);
    try std.testing.expectEqual(@as(u32, 1), record.depth);
    try std.testing.expectEqual(@as(u64, 0), record.frames[1]);
    chain();
    std.mem.writeInt(u64, frames[0][0..8], 0x1000, .little);
    record = std.mem.zeroes(sampler.Record);
    sampler.walk(0x1000, 0, read_frame, &record);
    try std.testing.expectEqual(@as(usize, 1), reads);
    try std.testing.expectEqual(@as(u32, 1), record.depth);
}

test "sampler: interrupted PC is never repeated in caller frames" {
    chain();
    fault_at = 0x1020;
    var record = std.mem.zeroes(sampler.Record);
    sampler.walk(0x1000, 0x4000, read_frame, &record);
    try std.testing.expectEqual(@as(u32, 1), record.depth);
    try std.testing.expectEqual(@as(u64, 0x4004), record.frames[0]);
}

test "sampler: exact overwrite loss, uid sidecar and staged batch sequence" {
    var records: [sampler.ring_records]sampler.Record = undefined;
    var uids: [sampler.ring_records]u32 = undefined;
    var ring = sampler.Ring{ .records = &records, .uids = &uids };
    for (0..sampler.ring_records + 23) |i| {
        var record = std.mem.zeroes(sampler.Record);
        record.pc = i;
        ring.push(record, 42);
    }
    try std.testing.expectEqual(@as(usize, sampler.ring_records), ring.count());
    try std.testing.expectEqual(@as(u64, 23), ring.dropped);
    try std.testing.expectEqual(@as(u64, 23), records[ring.first % sampler.ring_records].pc);
    try std.testing.expectEqual(@as(u32, 42), uids[ring.first % sampler.ring_records]);
    const first = ring.first;
    for (0..32) |_| ring.push(std.mem.zeroes(sampler.Record), 99);
    const held = ring.first;
    ring.consume(first, 16);
    try std.testing.expectEqual(held, ring.first);
    ring.dropped = std.math.maxInt(u64);
    ring.push(std.mem.zeroes(sampler.Record), 99);
    try std.testing.expectEqual(std.math.maxInt(u64), ring.dropped);
}

test "sampler: config and capture uid privilege survive numeric pid reuse" {
    const process = syscall.process;
    process.init();
    const same = process.create_as("same", .{}, .{}, .{}, .{ .uid = 42 }).?;
    const foreign = process.create_as("foreign", .{}, .{}, .{}, .{ .uid = 99 }).?;
    var config = std.mem.zeroes(sampler.Config);
    config.version = 1;
    config.pid_count = 1;
    config.pids[0] = same;
    try std.testing.expectEqual(@as(i64, 0), sampler.validate_config(config, .{ .uid = 42 }));
    config.pids[0] = foreign;
    try std.testing.expectEqual(@as(i64, -7), sampler.validate_config(config, .{ .uid = 42 }));
    try std.testing.expectEqual(@as(i64, 0), sampler.validate_config(config, .{ .uid = 42, .caps = process.cap_proc_admin }));
    try std.testing.expect(!sampler.may_observe(.{ .uid = 42 }, 99));
    config.pid_count = 2;
    config.pids[1] = foreign;
    try std.testing.expectEqual(@as(i64, -1), sampler.validate_config(config, .{ .uid = 99 }));
    config.pid_count = 1;
    try std.testing.expectEqual(@as(i64, -1), sampler.validate_config(config, .{ .uid = 99 }));
    var records: [sampler.ring_records]sampler.Record = undefined;
    var uids: [sampler.ring_records]u32 = undefined;
    var buffered = sampler.Ring{ .records = &records, .uids = &uids };
    var captured = std.mem.zeroes(sampler.Record);
    captured.pid = foreign;
    buffered.push(captured, 99);
    for (2..process.max_processes) |_| {
        _ = process.create_as("fill", .{}, .{}, .{}, .{ .uid = 42 }).?;
    }
    try std.testing.expect(process.bind(foreign, 3));
    _ = process.on_task_exit(3, 0);
    const reused = process.create_as("reused", .{}, .{}, .{}, .{ .uid = 42 }).?;
    try std.testing.expectEqual(foreign, reused);
    try std.testing.expect(sampler.may_observe(.{ .uid = 42 }, process.principal(reused).?.uid));
    try std.testing.expect(!sampler.may_observe(.{ .uid = 42 }, uids[buffered.first % sampler.ring_records]));
}

test "sampler: virtual deadline skips missed periods without a catch-up IRQ flood" {
    const timer = syscall.timer;
    try std.testing.expectEqual(@as(u64, 110), timer.profile_next_deadline(100, 100, 10));
    try std.testing.expectEqual(@as(u64, 140), timer.profile_next_deadline(100, 137, 10));
    try std.testing.expectEqual(@as(u64, 100), timer.profile_next_deadline(100, 90, 10));
    try std.testing.expectEqual(@as(u64, 0), timer.profile_next_deadline(100, 137, 0));
}
