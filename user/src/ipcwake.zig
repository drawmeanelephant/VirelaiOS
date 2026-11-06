//! census (M188e, #3190): IPCWAKE.BIN — gate-referenced: live-ipc-wake.
//! VirelaiOS ESP user program — IPCWAKE.BIN (issue #3901).
//!
//! The blocked-consumer half of the IPC wake proof. Before this card a
//! `sys_ipc_send` into a process blocked in `sys_wait_event` did NOT wake
//! it — `events.on_event_pushed` covered only the event FIFO, so a
//! mailbox consumer parked until its next real event (the Go seat's ~1 s
//! COMPOSITE_TICK; measured ~1 s per missed WM_RPC in #3898). Now every
//! delivered message also queues a kind-23 IPC_MAIL event, which wakes
//! the blocked consumer on the next scheduler pass.
//!
//! This program makes the bound observable: it arms a 12-tick app timer —
//! the only OTHER event a headless process can ever receive — then
//! blocks in `sys_wait_event`. A kernel WITHOUT the wake can only ever
//! return the TIMER event (~12 s in): the program reports
//! `ipcwake: tick-waited` and exits nonzero. With the wake, the first
//! IPC_MAIL push returns immediately, the program drains the mailbox with
//! `sys_ipc_recv`, echoes each message as `ipcwake: mail <n> <bytes>`,
//! prints `ipcwake: ok`, and keeps draining as a permanent occupant (the
//! gate's `mbox` row then carries the live sent/recv counters). The mail
//! wake arriving strictly before the armed deadline IS the sub-tick
//! bound (no wall-clock needed); a TIMER event that lands AFTER the
//! first mail is expected and reported as `ipcwake: timer`.
//!
//! Invocation: `exec IPCWAKE.BIN` — the gate exec's it first so the
//! sender (COUNTER.BIN) targets the deterministic pid 1.

const ui = @import("lib/ui.zig");

/// Pinned markers (the live gate's grep targets; host-tested so they
/// cannot drift from the payload).
pub const armed_marker: []const u8 = "ipcwake: armed";
pub const mail_marker: []const u8 = "ipcwake: mail ";
pub const ok_marker: []const u8 = "ipcwake: ok";
pub const timer_marker: []const u8 = "ipcwake: timer";
pub const tick_waited_marker: []const u8 = "ipcwake: tick-waited";
pub const waitfail_marker: []const u8 = "ipcwake: waitfail";

/// The arm bound in scheduler ticks. Long enough that the sender's first
/// burst (COUNTER.BIN sends on its 6th yield iteration — ~4 s observed)
/// never races the deadline, short enough that a wake-less kernel fails
/// the leg in seconds instead of hanging to the gate timeout.
const arm_ticks: u64 = 12;

/// Bound on non-mail, non-timer events consumed before the first mail —
/// a flood of foreign events should still fail honestly, never spin.
const max_stray_wakes: usize = 8;

/// Segmented (DSK3) image, same shape as TIMER.BIN: the writable tail
/// needs a real .data word — `export` keeps this one resident.
export var argv_segment_pad: u64 = 1;

fn append_str(buf: []u8, pos: usize, src: []const u8) usize {
    @memcpy(buf[pos .. pos + src.len], src);
    return pos + src.len;
}

fn fmt_u64(buf: []u8, value: u64) []const u8 {
    var v = value;
    var i: usize = buf.len;
    if (v == 0) {
        i -= 1;
        buf[i] = '0';
        return buf[i..];
    }
    while (v > 0) : (v /= 10) {
        i -= 1;
        buf[i] = @intCast('0' + (v % 10));
    }
    return buf[i..];
}

fn line(parts: []const []const u8) void {
    var buf: [128]u8 = undefined;
    var pos: usize = 0;
    for (parts) |p| {
        pos = append_str(&buf, pos, p);
    }
    buf[pos] = '\n';
    ui.write_console(buf[0 .. pos + 1]);
}

/// Drain the mailbox and echo each message — the same full-drain pass a
/// real consumer makes per wake (the seat's serviceRPC drains all 32).
fn drain_mail() void {
    var raw: [64]u8 = undefined;
    while (true) {
        const got = ui.syscall2(ui.sys_ipc_recv_num, @intFromPtr(&raw), raw.len);
        if (got <= 0) return;
        var msg = raw[0..@intCast(got)];
        // Trim a trailing newline so the echo stays a one-line marker.
        while (msg.len > 0 and msg[msg.len - 1] == '\n') msg = msg[0 .. msg.len - 1];
        var nbuf: [16]u8 = undefined;
        line(&.{ mail_marker, "<", fmt_u64(&nbuf, @intCast(msg.len)), "> ", msg });
    }
}

export fn _start(argc: usize, argv: ?[*]const [256]u8) callconv(.c) noreturn {
    argv_segment_pad = argc; // keep the writable segment live
    _ = argv;

    if (ui.timer_set(arm_ticks) < 0) {
        line(&.{"ipcwake: arm-fail"});
        ui.exit_process(1);
    }
    line(&.{armed_marker});

    var mailed = false;
    var strays: usize = 0;
    while (true) {
        var ev: ui.Event = undefined;
        const n = ui.wait_event(&ev);
        if (n <= 0) {
            var nbuf: [16]u8 = undefined;
            line(&.{ waitfail_marker, " rc=", fmt_u64(&nbuf, @bitCast(n)) });
            ui.exit_process(2);
        }
        switch (ev.kind) {
            ui.EVENT_TIMER => {
                if (!mailed) {
                    // The 4-tick deadline fired before ANY mail: on a
                    // wake-less kernel this is the only event the process
                    // can ever see — the leg's failure signature.
                    line(&.{tick_waited_marker});
                    ui.exit_process(3);
                }
                line(&.{timer_marker});
            },
            ui.IPC_MAIL => {
                drain_mail();
                if (!mailed) {
                    mailed = true;
                    line(&.{ok_marker});
                }
            },
            else => {
                if (mailed) continue;
                strays += 1;
                if (strays >= max_stray_wakes) {
                    line(&.{"ipcwake: no-mail"});
                    ui.exit_process(4);
                }
                var kbuf: [16]u8 = undefined;
                line(&.{ "ipcwake: kind ", fmt_u64(&kbuf, ev.kind) });
            },
        }
    }
}

test "ipcwake: the marker shapes are pinned (live-gate grep targets)" {
    const std = @import("std");
    try std.testing.expectEqualStrings("ipcwake: armed", armed_marker);
    try std.testing.expectEqualStrings("ipcwake: mail ", mail_marker);
    try std.testing.expectEqualStrings("ipcwake: ok", ok_marker);
    try std.testing.expectEqualStrings("ipcwake: timer", timer_marker);
    try std.testing.expectEqualStrings("ipcwake: tick-waited", tick_waited_marker);
    try std.testing.expectEqualStrings("ipcwake: waitfail", waitfail_marker);
}
