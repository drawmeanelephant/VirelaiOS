//! B7 native acceptance fixture, not a shipped app. No libc or capture.
const std = @import("std");
pub const panic = std.debug.FullPanic(panicImpl);
const flag: u64 = @as(u64, 1) << 63;
const Params = extern struct { format: u8 = 19, rate: u8 = 7, channels: u8 = 2, reserved: u8 = 0, version: u32 = 1 };
const Status = extern struct {
    generation: u64,
    accepted: u64,
    submitted: u64,
    completed: u64,
    canceled: u64,
    state: u32,
    reason: u32,
    outstanding: u32,
    slots: u32,
    starts: u32,
    stops: u32,
    releases: u32,
    resets: u32,
};
export var pcm_probe_state: u64 = 1;
var samples: [4096]u8 = @splat(0);
var frame_offset: u32 = 0;

fn call(number: u64, args: [6]u64) i64 {
    var result: i64 = undefined;
    asm volatile ("svc #0"
        : [result] "={x0}" (result),
        : [number] "{x8}" (number),
          [a0] "{x0}" (args[0]),
          [a1] "{x1}" (args[1]),
          [a2] "{x2}" (args[2]),
          [a3] "{x3}" (args[3]),
          [a4] "{x4}" (args[4]),
          [a5] "{x5}" (args[5]),
        : .{ .memory = true });
    return result;
}
fn say(bytes: []const u8) void {
    var offset: usize = 0;
    while (offset < bytes.len) {
        const n = call(1, .{ 1, @intFromPtr(bytes.ptr) + offset, bytes.len - offset, 0, 0, 0 });
        if (n <= 0) exit(72);
        offset += @intCast(n);
    }
}
fn exit(code: u64) noreturn {
    _ = call(3, .{ code, 0, 0, 0, 0, 0 });
    unreachable;
}
fn panicImpl(_: []const u8, _: ?usize) noreturn {
    say("b7: PANIC\n");
    exit(71);
}
fn require(ok: bool) void {
    if (!ok) {
        say("b7: FAIL\n");
        exit(70);
    }
}
fn audio(op: u64, generation: u64, pointer: u64, length: usize) i64 {
    return call(43, .{ pointer, flag | length, op, generation, 0, 0 });
}
fn counter() u64 {
    return asm volatile ("mrs %[v], cntpct_el0"
        : [v] "=r" (-> u64),
    );
}
fn budget() u64 {
    const freq = asm volatile ("mrs %[v], cntfrq_el0"
        : [v] "=r" (-> u64),
    );
    return counter() + 10 * freq;
}
fn open() u64 {
    var params = Params{};
    const until = budget();
    while (counter() < until) {
        const n = audio(1, 0, @intFromPtr(&params), @sizeOf(Params));
        if (n == -11) continue; // bounded deferred owner-death teardown
        require(n > 0);
        return @intCast(n);
    }
    require(false);
    unreachable;
}
fn status(generation: u64) Status {
    var result: Status = undefined;
    require(audio(6, generation, @intFromPtr(&result), @sizeOf(Status)) == 0);
    require(result.slots == 8 and result.outstanding <= 32768);
    return result;
}
fn closed(generation: u64) Status {
    const until = budget();
    while (counter() < until) {
        const result = status(generation);
        if (result.state == 0) return result;
        require(result.state != 10); // quarantined
    }
    require(false);
    unreachable;
}
fn synth() void {
    for (0..512) |i| {
        const t: f64 = @floatFromInt(frame_offset + i);
        const sample: f32 = @floatCast(@sin(2 * std.math.pi * 440 * t / 48000) * 0.1);
        const bits: u32 = @bitCast(sample);
        std.mem.writeInt(u32, samples[i * 8 ..][0..4], bits, .little);
        std.mem.writeInt(u32, samples[i * 8 + 4 ..][0..4], bits, .little);
    }
    frame_offset += 512;
}
fn submit(generation: u64) void {
    synth();
    const until = budget();
    while (counter() < until) {
        const n = audio(2, generation, @intFromPtr(&samples), samples.len);
        if (n == -11) continue;
        if (n != 4096) {
            var buf: [128]u8 = undefined;
            const s = status(generation);
            say(std.fmt.bufPrint(&buf, "b7: submit refused={d} state={d} reason={d}\n", .{ n, s.state, s.reason }) catch unreachable);
        }
        require(n == 4096);
        return;
    }
    require(false);
}
fn prefill(generation: u64) void {
    for (0..8) |_| submit(generation);
}
fn start(generation: u64) void {
    require(audio(3, generation, 0, 0) == 0);
}

export fn _start(argc: u64, argv: u64) callconv(.c) noreturn {
    pcm_probe_state += 1;
    if (argc > 0) {
        const block: [*]const u8 = @ptrFromInt(argv);
        if (std.mem.startsWith(u8, block[0..256], "death")) {
            const generation = open();
            prefill(generation);
            start(generation);
            say("b7: owner exits with queued PCM\n");
            exit(0);
        }
    }
    var params = Params{};
    const initial = audio(1, 0, @intFromPtr(&params), @sizeOf(Params));
    if (initial == -9) {
        require(call(43, .{ @intFromPtr(&samples), 8, 0, 0, 0, 0 }) == -9);
        say("b7: NO AUDIO DEVICE (ENXIO), playback refused\n");
        say("b7: done\n");
        exit(0);
    }
    require(initial > 0);
    const generation: u64 = @intCast(initial);
    require(audio(3, generation, 0, 0) == -1); // no prefill
    require(audio(2, generation, 0xffff_ffff_ffff_f000, 4096) == -3);
    require(status(generation).accepted == 0); // failed copy rolled back
    require(audio(2, generation, @intFromPtr(&samples), 7) == -1);
    require(audio(2, generation, @intFromPtr(&samples), 4097) == -8);
    require(audio(2, generation + 1, @intFromPtr(&samples), 8) == -1);
    prefill(generation);
    require(audio(2, generation, @intFromPtr(&samples), 4096) == -11);
    require(status(generation).accepted == 32768);
    say("b7: bounds=8x4096 backpressure=EAGAIN accepted=32768 copy-rollback=0\n");
    start(generation);
    for (0..24) |_| submit(generation);
    require(audio(4, generation, 0, 0) == 0);
    const drained = closed(generation);
    require(drained.accepted == 131072 and drained.submitted == 131072 and drained.completed == 131072);
    require(drained.canceled == 0 and drained.reason == 0 and drained.outstanding == 0);
    require(drained.starts == 1 and drained.stops == 1 and drained.releases == 1 and drained.resets == 0);
    say("b7: continuous accepted=131072 submitted=131072 completed=131072 starts=1 stops=1 releases=1 resets=0\n");

    const underrun = open();
    prefill(underrun);
    start(underrun);
    const until = budget();
    while (counter() < until and status(underrun).state != 9) {}
    require(status(underrun).state == 9 and status(underrun).reason == 1);
    require(audio(2, underrun, @intFromPtr(&samples), 4096) == -1);
    require(audio(5, underrun, 0, 0) == 0);
    _ = closed(underrun);
    say("b7: underrun=XRUN sticky=1 explicit-abort=ok\n");

    const aborted = open();
    prefill(aborted);
    require(audio(5, aborted, 0, 0) == 0);
    const canceled = closed(aborted);
    require(canceled.canceled == 32768 and canceled.completed == 0 and canceled.reason == 3);
    say("b7: abort canceled=32768 outstanding=0\n");

    const name = "PCM.BIN";
    var arg: [256]u8 = @splat(0);
    @memcpy(arg[0..5], "death");
    const pid = call(28, .{ @intFromPtr(name.ptr), name.len, @intFromPtr(&arg), 1, 0, 0 });
    require(pid >= 0);
    require(call(8, .{ @intCast(pid), 0, 0, 0, 0, 0 }) == 0);
    const reopened = open();
    require(reopened > aborted);
    require(audio(5, reopened, 0, 0) == 0);
    _ = closed(reopened);
    say("b7: owner-death teardown reopened=ok\n");
    say("b7: output only, no capture or low-latency claim\n");
    say("b7: done\n");
    exit(0);
}
