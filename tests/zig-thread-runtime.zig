//! Real EL0 explicit TLS, join, contended futex and coarse-timeout witness.
const std = @import("std");
const threads = @import("threads");
const native = @import("native");
pub const panic = std.debug.FullPanic(fatal);
fn fatal(_: []const u8, _: ?usize) noreturn {
    _ = native.consoleChunk("zig-threads: FAIL\n");
    native.exit(70);
}
fn check(ok: bool) void {
    if (!ok) fatal("check", null);
}
const Storage = struct {
    stack: [128 * 1024]u8 align(16) = undefined,
    tls: [4096]u8 align(4096) = undefined,
};
var storage: [7]Storage = undefined;
var main_tls: [4096]u8 align(4096) = undefined;
const template = [_]u8{ 42, 0, 0, 0, 0, 0, 0, 0 };
var arrived: u32 = 0;
var release: u32 = 0;
var mutex: threads.Mutex = .{};
var count: usize = 0;
var results: [6]usize = @splat(0);

fn state() *[2]usize {
    return @ptrCast(@alignCast(threads.context() + 16));
}
fn child(arg: usize) u8 {
    check(state()[0] == 42 and state()[1] == 0);
    state().* = .{ 1000 + arg, 2000 + arg };
    _ = @atomicRmw(u32, &arrived, .Add, 1, .release);
    _ = threads.wake(&arrived, 6) catch fatal("wake", null);
    while (@atomicLoad(u32, &release, .acquire) == 0)
        _ = threads.wait(&release, 0, 0) catch fatal("wait", null);
    for (0..16) |_| {
        mutex.lock() catch fatal("lock", null);
        const before = count;
        _ = native.call(0, .{ 0, 0, 0, 0, 0, 0 }); // yield while holding lock
        count = before + 1;
        check(state()[0] == 1000 + arg and state()[1] == 2000 + arg);
        mutex.unlock() catch fatal("unlock", null);
    }
    results[arg] = state()[0] + state()[1];
    return 0;
}
fn counter() u64 {
    return asm volatile ("mrs %[v], cntpct_el0"
        : [v] "=r" (-> u64),
        :
        : .{ .memory = true });
}
fn frequency() u64 {
    return asm volatile ("mrs %[v], cntfrq_el0"
        : [v] "=r" (-> u64),
    );
}

export fn _start() callconv(.c) noreturn {
    threads.attach(&template, 16, 16, &main_tls) catch fatal("attach", null);
    state().* = .{ 77, 88 };
    var handles: [6]threads.Thread = undefined;
    for (&handles, 0..) |*handle, i|
        handle.* = threads.Thread.spawn(&storage[i].stack, &template, 16, 16, &storage[i].tls, child, i) catch fatal("spawn", null);
    if (threads.Thread.spawn(&storage[6].stack, &template, 16, 16, &storage[6].tls, child, 0)) |_| {
        fatal("capacity", null);
    } else |err| check(err == error.Capacity);
    while (true) {
        const n = @atomicLoad(u32, &arrived, .acquire);
        if (n == 6) break;
        _ = threads.wait(&arrived, n, 0) catch fatal("arrived", null);
    }
    @atomicStore(u32, &release, 1, .release);
    _ = threads.wake(&release, 6) catch fatal("release", null);
    for (&handles) |*handle| check((handle.join() catch fatal("join", null)) == 0);
    check(count == 96 and state()[0] == 77 and state()[1] == 88);
    for (results, 0..) |result, i| check(result == 3000 + 2 * i);
    if (handles[0].join()) |_| fatal("double join", null) else |err| check(err == error.InvalidJoin);
    _ = native.consoleChunk("zig-threads: independent=6 joined=6 count=96 capacity=refused\n");
    // Store+wake before seating: no waiter exists; the next compare refuses
    // instead of sleeping forever. Deterministic kernel race tests cover
    // the second compare after seating as well.
    var word: u32 = 1;
    check((threads.wake(&word, 1) catch fatal("wake-before", null)) == 0);
    check((threads.wait(&word, 0, 0) catch fatal("lost-wake", null)) == .changed);
    check(native.call(74, .{ 2, @intFromPtr(&word), 1, 1, 0, 0 }) == -1);
    if (threads.wait(&word, 1, 1)) |_| fatal("precision", null) else |err| check(err == error.UnsupportedPrecision);
    const start = counter();
    check((threads.wait(&word, 1, 1_000_000_000) catch fatal("timeout", null)) == .timed_out);
    check(counter() - start >= frequency());
    _ = native.consoleChunk("zig-threads: lost-wake=changed fine=refused coarse=not-early\n");
    _ = native.consoleChunk("zig-threads: done\n");
    native.exit(0);
}
