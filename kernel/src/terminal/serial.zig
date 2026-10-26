//! The serial front-end plumbing (split from terminal.zig, #3753): the
//! runtime console hookup and the console<->terminal pumps.

const std = @import("std");
const console = @import("../console.zig");
const klog = @import("../klog.zig");
const registry = @import("../terminal.zig");
const session_mod = @import("session.zig");
const create = registry.create;
const get = registry.get;
const Terminal = session_mod.Terminal;

// ---------------------------------------------------------------------------
// The serial front-end pump (ADR 0020 D2/D4). The kernel console is a
// front-end like any other: input bytes it reads are pushed into the
// attached terminal, and the terminal's output ring is drained back to it.
// The pump is driven from the syscall path (a process reading/writing its
// `/dev/tty`), so no idle-loop integration is needed — the console's RX
// FIFO buffers keys until the next read. Boot default is unchanged because
// nothing is attached until a process asks (ADR 0020 D4).
// ---------------------------------------------------------------------------
/// The kernel console used as the `.serial` front-end, set once at boot.
pub var runtime_console: ?console.Console = null;

pub fn setRuntimeConsole(con: console.Console) void {
    runtime_console = con;
}

/// The terminal currently attached to the serial console front-end, if any.
pub fn attachedSerial() ?*Terminal {
    for (&registry.terminals) |*t| {
        if (t.in_use and t.attached and t.front_end == .serial) return t;
    }
    return null;
}

/// Drain the console's pending input into the serial-attached terminal.
/// Pure w.r.t. the terminal object (the console is the side effect). Returns
/// bytes moved. A no-op with no serial-attached terminal.
pub fn pumpInput(con: console.Console) usize {
    const t = attachedSerial() orelse return 0;
    var total: usize = 0;
    var buf: [64]u8 = undefined;
    var n: usize = 0;
    while (con.readByte()) |b| {
        buf[n] = b;
        n += 1;
        if (n == buf.len) {
            total += t.pushInput(buf[0..n]);
            n = 0;
        }
    }
    if (n > 0) total += t.pushInput(buf[0..n]);
    return total;
}

/// Drain the serial-attached terminal's output ring to the console. A no-op
/// with no serial-attached terminal. Returns bytes moved.
pub fn pumpOutput(con: console.Console) usize {
    const t = attachedSerial() orelse return 0;
    var total: usize = 0;
    var buf: [128]u8 = undefined;
    while (true) {
        const n = t.readOut(&buf);
        if (n == 0) break;
        con.write(buf[0..n]);
        total += n;
    }
    if (total > 0) con.flush();
    return total;
}

/// Pump the runtime console (no-op before `setRuntimeConsole` / with no
/// serial-attached terminal). Used by the `/dev/tty` read path.
pub fn pumpRuntimeInput() usize {
    const con = runtime_console orelse return 0;
    return pumpInput(con);
}

/// Pump the runtime console out. Used by the `/dev/tty` write path.
pub fn pumpRuntimeOutput() usize {
    const con = runtime_console orelse return 0;
    return pumpOutput(con);
}

/// Return `t`'s unread input to the runtime console's RX — the sys_tty_attach
/// selector-0 half of `pumpRuntimeInput` (bytes pumped in come back out at
/// the handover). A no-op before `setRuntimeConsole`.
pub fn pushBackRuntimeInput(t: *Terminal) usize {
    const con = runtime_console orelse {
        klog.line("pushback: no runtime console\n");
        return 0;
    };
    const moved = t.pushBackInput(con);
    var dbuf: [48]u8 = undefined;
    klog.line(std.fmt.bufPrint(&dbuf, "pushback: moved {d} fe={s}\n", .{ moved, @tagName(t.front_end) }) catch "pushback: moved ?\n");
    return moved;
}

test "terminal: serial pump round-trips console input/output through an attached terminal" {
    for (&registry.terminals) |*t| t.reset();
    var mock = console.MockConsole(256){};
    const con = mock.console();

    // No attachment: the pump is a no-op (boot default unchanged).
    mock.feed("abc");
    try std.testing.expectEqual(@as(usize, 0), pumpInput(con));
    try std.testing.expectEqual(@as(usize, 0), pumpOutput(con));

    // Attach a terminal to the serial front-end.
    const h = create(5) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attach(.serial));
    try std.testing.expect(attachedSerial() != null and attachedSerial().? == t);

    // Console RX flows into the terminal's input queue.
    try std.testing.expectEqual(@as(usize, 3), pumpInput(con));
    var in: [8]u8 = undefined;
    try std.testing.expectEqual(@as(usize, 3), t.readInput(&in));
    try std.testing.expectEqualStrings("abc", in[0..3]);

    // Terminal output flows out to the console (front-end drain).
    _ = t.write("hello");
    try std.testing.expectEqual(@as(usize, 5), pumpOutput(con));
    try std.testing.expectEqualStrings("hello", mock.contents());

    // Detach stops the pump entirely.
    t.detach();
    try std.testing.expect(attachedSerial() == null);
    mock.feed("x");
    try std.testing.expectEqual(@as(usize, 0), pumpInput(con));
    try std.testing.expectEqual(@as(usize, 0), pumpOutput(con));
    for (&registry.terminals) |*tt| tt.reset();
}

test "terminal: serial detach returns unread input to the console in order" {
    for (&registry.terminals) |*t| t.reset();
    var mock = console.MockConsole(256){};
    const con = mock.console();

    const h = create(5) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attach(.serial));

    // The owner pumps a burst but consumes only its head — the M120f
    // handover shape: `monitor\r` read, the probe tail still queued.
    mock.feed("monitor\recho tail\r");
    try std.testing.expectEqual(@as(usize, 18), pumpInput(con));
    var head: [8]u8 = undefined;
    try std.testing.expectEqual(@as(usize, 8), t.readInput(&head));
    try std.testing.expectEqualStrings("monitor\r", head[0..8]);

    // The detach handover: the unread tail requeues on the console RX,
    // oldest first, ahead of newer host input that arrives afterwards.
    try std.testing.expectEqual(@as(usize, 10), t.pushBackInput(con));
    t.detach();
    mock.feed("late");
    var back: [16]u8 = undefined;
    var got: usize = 0;
    while (con.readByte()) |b| {
        back[got] = b;
        got += 1;
    }
    try std.testing.expectEqualStrings("echo tail\rlate", back[0..got]);
    try std.testing.expectEqual(@as(usize, 0), t.pendingInput());
    for (&registry.terminals) |*tt| tt.reset();
}
