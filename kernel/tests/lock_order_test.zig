//! Canonical service-domain lock-order probes (the #2041/#2043 follow-on:
//! service-domain locks are now held through their handlers, so a nested
//! acquisition that takes a LOWER-ranked domain under a higher hold inverts
//! the order and closes a two-core deadlock cycle — core A spins K->F/N/W/E
//! while core B holds that domain and spins for KERNEL via the observed
//! uaccess write-page resolution inside socket/file syscalls).
//!
//! A single-core host cannot observe that deadlock directly, so these tests
//! drive the REAL dispatchers — `syscall.dispatch` for `sys_thread` op 1
//! (thread exit) and `monitor.exec` for `time <cmd>` — through svclock's
//! order_witness: every acquisition records the held mask it saw plus the
//! domain bit it took. Bits are rank-encoded (file=1 .. kernel=16), so an
//! entry whose taken bit does not exceed every held bit IS an inversion:
//! a failing assertion here, a hang on the 2-vCPU guest.

const std = @import("std");
const monitor = @import("monitor");
const scheduler = monitor.scheduler;
const syscall = monitor.syscall;
const svclock = monitor.svclock;
const process = scheduler.process;

var observed: u5 = 0;
var calls: usize = 0;

fn held_mask() u5 {
    var bits: u5 = 0;
    if (svclock.file.held()) bits |= svclock.dom_bit(.file);
    if (svclock.net.held()) bits |= svclock.dom_bit(.net);
    if (svclock.win.held()) bits |= svclock.dom_bit(.win);
    if (svclock.ev.held()) bits |= svclock.dom_bit(.ev);
    if (svclock.kernel.held()) bits |= svclock.dom_bit(.kernel);
    return bits;
}

fn expect_unlocked() !void {
    try std.testing.expectEqual(@as(u5, 0), held_mask());
    inline for (.{ &svclock.file, &svclock.net, &svclock.win, &svclock.ev, &svclock.kernel }) |lock| {
        // Check the spinlock as well as its holder record.
        try std.testing.expect(lock.try_acquire());
        lock.release();
    }
}

/// Every recorded acquisition must take a bit ABOVE every bit already
/// held (bits are the canonical rank). Any other entry is a
/// held-while-acquiring inversion — the edge that completes the
/// deadlock cycle with the legal domain->KERNEL takes.
fn expect_canonical_order() !void {
    var i: usize = 0;
    while (i < svclock.order_witness.count()) : (i += 1) {
        const held = svclock.order_witness.held_at(i);
        const taken = svclock.order_witness.taken_at(i);
        try std.testing.expect(taken > held);
    }
}

fn discard(_: []const u8) void {}

fn observe_command(_: *monitor.Monitor, args: []const []const u8) monitor.ExecError {
    observed = held_mask();
    calls += 1;
    return if (args.len == 0) .none else .invalid_argument;
}

fn make_monitor(mock: anytype) monitor.Monitor {
    return monitor.Monitor.init(mock.console(), .{
        .handoff = std.mem.zeroes(monitor.handoff.HandoffV2),
        .map = monitor.memmap.MapView.init(&.{}, @sizeOf(monitor.memmap.MemoryDescriptor), 0),
        .console_name = "lockorder-probe",
    }, monitor.MachineControl.disabled());
}

test "order witness records acquisitions and flags a foreign inversion" {
    try expect_unlocked();
    defer svclock.release_set(held_mask());
    // Canonical take: two ascending entries, no violation.
    svclock.order_witness.reset();
    svclock.acquire_set(svclock.dom_bit(.file) | svclock.dom_bit(.kernel));
    try std.testing.expectEqual(@as(usize, 2), svclock.order_witness.count());
    try std.testing.expectEqual(@as(u5, 0), svclock.order_witness.held_at(0));
    try std.testing.expectEqual(svclock.dom_bit(.file), svclock.order_witness.taken_at(0));
    try std.testing.expectEqual(svclock.dom_bit(.file), svclock.order_witness.held_at(1));
    try std.testing.expectEqual(svclock.dom_bit(.kernel), svclock.order_witness.taken_at(1));
    try expect_canonical_order();
    svclock.release_set(svclock.dom_bit(.file) | svclock.dom_bit(.kernel));
    // A raw KERNEL hold then a FILE take is the inversion shape the
    // probes hunt: the witness must see taken <= held.
    svclock.kernel.acquire();
    svclock.order_witness.reset();
    svclock.file.acquire();
    try std.testing.expectEqual(@as(usize, 1), svclock.order_witness.count());
    try std.testing.expect(svclock.order_witness.taken_at(0) <= svclock.order_witness.held_at(0));
    svclock.release_set(svclock.dom_bit(.file) | svclock.dom_bit(.kernel));
    try expect_unlocked();
}

test "monitor `time <cmd>` takes the inner command's domain in canonical order" {
    try expect_unlocked();
    defer svclock.release_set(held_mask());
    var mock = monitor.console.MockConsole(512){};
    var mon = make_monitor(&mock);
    const ls = @constCast(monitor.lookup("ls").?);
    const saved = ls.*;
    defer ls.* = saved;
    ls.handler = observe_command;
    svclock.order_witness.reset();
    try std.testing.expectEqual(monitor.ExecError.none, monitor.exec(&mon, &.{ "time", "ls" }));
    try std.testing.expectEqual(@as(usize, 1), calls);
    // The inner handler still runs under the full domain set; only the
    // ACQUISITION ORDER changes (KERNEL may not precede FILE here).
    try std.testing.expectEqual(svclock.dom_bit(.file) | svclock.dom_bit(.kernel), observed);
    try expect_canonical_order();
    try expect_unlocked();
}

// #1336: N x 192 KiB kstack locals overflow a typical host test stack,
// so the dispatch probe borrows this BSS pool (scheduler_test.zig does).
var kstack_pool: [scheduler.max_tasks][scheduler.task_stack_size]u8 align(16) = undefined;

/// Boot a bound user task and yield until it is the current task, in the
/// shape of scheduler_test.zig's ExitKillCase.boot — `sys_thread` op 1
/// must exit the CALLING task, and the exit teardown only runs for a
/// process-bound caller.
fn boot_user_task() !usize {
    _ = scheduler.init();
    scheduler.current[1] = scheduler.idle_id;
    scheduler.current[2] = scheduler.idle_id;
    scheduler.current[3] = scheduler.idle_id;
    const tid = scheduler.spawn("user-exec", 0x3000, scheduler.spsr_el0t_irqs, &kstack_pool[0], 0, 0).?;
    const pid = process.create("LOCKORD.ELF", .{}, .{}, .{}).?;
    try std.testing.expect(process.bind(pid, tid));
    scheduler.start();
    for (0..scheduler.max_tasks * 2) |_| {
        if (scheduler.current_id() == tid) {
            scheduler.exceptions.resume_frame[0] = scheduler.tasks[tid].sp;
            scheduler.exceptions.resume_sp_el0[0] = scheduler.tasks[tid].sp_el0;
            return tid;
        }
        try std.testing.expect(scheduler.yield_current());
    }
    return error.TestUnexpectedResult;
}

test "sys_thread op 1 exit takes the teardown domains in canonical order" {
    syscall.init(discard);
    _ = try boot_user_task();
    try expect_unlocked();
    defer svclock.release_set(held_mask());
    svclock.order_witness.reset();
    var frame = std.mem.zeroes(syscall.exceptions.VectorFrame);
    // Op 1 = thread exit. The teardown takes every domain lock; the
    // KERNEL hold this dispatch took must not precede them.
    try std.testing.expectEqual(@as(u64, 0), syscall.dispatch(syscall.sys_thread, .{ 1, 7, 0, 0, 0, 0 }, &frame));
    try expect_canonical_order();
    try expect_unlocked();
}

test "exit_current outside a syscall hold takes every domain canonically" {
    _ = try boot_user_task();
    try expect_unlocked();
    defer svclock.release_set(held_mask());
    svclock.order_witness.reset();
    try std.testing.expect(scheduler.exit_current(0));
    try expect_canonical_order();
    try expect_unlocked();
}

test "a lower-domain hold may extend to KERNEL (the cycle's legal half)" {
    try expect_unlocked();
    defer svclock.release_set(held_mask());
    // The deadlock's partner edge — uaccess write-page resolution under a
    // socket/file hold taking KERNEL — is canonical and must KEEP working;
    // the fix removes K->lower inversions, it does not ban nesting.
    svclock.file.acquire();
    svclock.order_witness.reset();
    const taken = svclock.acquire_missing(svclock.dom_bit(.kernel));
    try std.testing.expectEqual(svclock.dom_bit(.kernel), taken);
    try expect_canonical_order();
    svclock.release_set(taken);
    svclock.file.release();
    try expect_unlocked();
}
