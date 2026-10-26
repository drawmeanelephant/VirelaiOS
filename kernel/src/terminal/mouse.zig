//! Mouse reporting for terminals (split from terminal.zig, #3753): the
//! ?1000/1002/1003 mode policy and legacy/SGR report encoding.

const std = @import("std");
const registry = @import("../terminal.zig");
const screen_mod = @import("screen.zig");
const session_mod = @import("session.zig");
const release = registry.release;
const Screen = screen_mod.Screen;
const grid_cols = screen_mod.grid_cols;
const grid_lines = screen_mod.grid_lines;
const Terminal = session_mod.Terminal;

// ---------------------------------------------------------------------------
// M73i (#1635): mouse tracking — pure mode policy and report encoding.
// ---------------------------------------------------------------------------
/// What happened to the pointer. `wheel` is encoded press-style (xterm
/// numbers wheel buttons 64/65 and sends no release).
pub const MouseKind = enum { press, release, drag, motion, wheel };

/// The truth table for "does this mode report this event class":
/// edges and wheel need any tracking mode on, button-motion needs `?1002`
/// or `?1003`, free motion needs `?1003`. Single source of truth — the
/// pointer path, the PageUp wheel chord, and the tests all read it.
pub fn mouseReports(m1000: bool, m1002: bool, m1003: bool, kind: MouseKind) bool {
    return switch (kind) {
        .press, .release, .wheel => m1000 or m1002 or m1003,
        .drag => m1002 or m1003,
        .motion => m1003,
    };
}

/// Decimal digits of `v` into `out`, returning the count (max 5).
fn decDigits(v: usize, out: []u8) usize {
    if (v == 0) {
        if (out.len < 1) return 0;
        out[0] = '0';
        return 1;
    }
    var tmp: [5]u8 = undefined;
    var n: usize = 0;
    var x = v;
    while (x > 0) : (x /= 10) {
        tmp[n] = @intCast('0' + (x % 10));
        n += 1;
    }
    var i: usize = 0;
    while (i < n and i < out.len) : (i += 1) out[i] = tmp[n - 1 - i];
    return i;
}

/// M73i (#1635): encode one mouse report. `col`/`row` are 1-based cell
/// coordinates, clamped to `cols` x `grid_lines`; `button` is 0/1/2 (left/
/// middle/right) or the wheel direction 0=up/1=down for `.wheel`.
///
/// SGR (`?1006`): `ESC [ < b ; x ; y M|m` — release is `b+32` with a
/// lowercase final. Legacy (xterm's three-byte `CSI b x y M`): Cb+32 as the
/// button byte, coordinates 1-based + 32, release Cb = 3, motion Cb = 35,
/// wheel Cb = 64/65. The legacy byte cannot overflow by construction:
/// `y <= grid_lines(128)` -> byte `<= 160`; `x <= grid_cols(80)` -> `<= 112`.
/// Returns 0 when `out` has no room.
pub fn encodeMouse(
    sgr: bool,
    kind: MouseKind,
    button: u8,
    col: u16,
    row: u16,
    cols: u16,
    out: []u8,
) usize {
    const c: u16 = @min(@max(col, 1), cols);
    const r: u16 = @min(@max(row, 1), @as(u16, grid_lines));
    if (sgr) {
        const b: u8 = switch (kind) {
            .press => button,
            .release => 32 + button,
            .drag => 32 + button,
            .motion => 35,
            .wheel => 64 + button,
        };
        if (out.len < 17) return 0;
        out[0] = 0x1b;
        out[1] = '[';
        out[2] = '<';
        var i: usize = 3;
        i += decDigits(b, out[i..]);
        out[i] = ';';
        i += 1;
        i += decDigits(c, out[i..]);
        out[i] = ';';
        i += 1;
        i += decDigits(r, out[i..]);
        out[i] = if (kind == .release) 'm' else 'M';
        i += 1;
        return i;
    }
    const cb: u8 = switch (kind) {
        .press => button,
        .release => 3,
        .drag => 32 + button,
        .motion => 35,
        .wheel => 64 + button,
    };
    if (out.len < 6) return 0;
    out[0] = 0x1b;
    out[1] = '[';
    out[2] = 32 + cb;
    out[3] = @intCast(32 + c);
    out[4] = @intCast(32 + r);
    out[5] = 'M';
    return 6;
}
test "terminal: mouse DECSET modes track independently, default off (#1635)" {
    var s = Screen{};
    try std.testing.expect(!s.mouse_1000);
    try std.testing.expect(!s.mouse_1002);
    try std.testing.expect(!s.mouse_1003);
    try std.testing.expect(!s.mouse_1006);
    s.feed("\x1b[?1000h\x1b[?1006h");
    try std.testing.expect(s.mouse_1000);
    try std.testing.expect(s.mouse_1006);
    try std.testing.expect(!s.mouse_1002);
    try std.testing.expect(!s.mouse_1003);
    s.feed("\x1b[?1000l\x1b[?1003h");
    try std.testing.expect(!s.mouse_1000);
    try std.testing.expect(s.mouse_1003);
    // Like DECTCEM/bracketed paste the mode survives an alt-screen swap.
    s.feed("\x1b[?1049h");
    try std.testing.expect(s.mouse_1003);
    s.feed("\x1b[?1049l");
    try std.testing.expect(s.mouse_1003);
}

test "terminal: mouseReports truth table (#1635)" {
    // No mode on: nothing reports.
    try std.testing.expect(!mouseReports(false, false, false, .press));
    try std.testing.expect(!mouseReports(false, false, false, .wheel));
    // ?1000: edges and wheel only.
    try std.testing.expect(mouseReports(true, false, false, .press));
    try std.testing.expect(mouseReports(true, false, false, .release));
    try std.testing.expect(mouseReports(true, false, false, .wheel));
    try std.testing.expect(!mouseReports(true, false, false, .drag));
    try std.testing.expect(!mouseReports(true, false, false, .motion));
    // ?1002: adds button-motion, not free motion.
    try std.testing.expect(mouseReports(false, true, false, .drag));
    try std.testing.expect(!mouseReports(false, true, false, .motion));
    // ?1003: everything.
    for ([_]MouseKind{ .press, .release, .drag, .motion, .wheel }) |k| {
        try std.testing.expect(mouseReports(false, false, true, k));
    }
}

test "terminal: encodeMouse SGR form (#1635)" {
    var out: [32]u8 = undefined;
    const n = encodeMouse(true, .press, 0, 3, 2, 80, &out);
    try std.testing.expectEqualStrings("\x1b[<0;3;2M", out[0..n]);
    const rel = encodeMouse(true, .release, 0, 3, 2, 80, &out);
    try std.testing.expectEqualStrings("\x1b[<32;3;2m", out[0..rel]);
    const drag = encodeMouse(true, .drag, 0, 3, 2, 80, &out);
    try std.testing.expectEqualStrings("\x1b[<32;3;2M", out[0..drag]);
    const mot = encodeMouse(true, .motion, 0, 3, 2, 80, &out);
    try std.testing.expectEqualStrings("\x1b[<35;3;2M", out[0..mot]);
    const up = encodeMouse(true, .wheel, 0, 3, 2, 80, &out);
    try std.testing.expectEqualStrings("\x1b[<64;3;2M", out[0..up]);
    const down = encodeMouse(true, .wheel, 1, 3, 2, 80, &out);
    try std.testing.expectEqualStrings("\x1b[<65;3;2M", out[0..down]);
}

test "terminal: encodeMouse legacy form and its byte bound (#1635)" {
    var out: [32]u8 = undefined;
    // press: Cb = button -> byte 32; x = 3 -> 35; y = 2 -> 34.
    const n = encodeMouse(false, .press, 0, 3, 2, 80, &out);
    try std.testing.expectEqual(@as(usize, 6), n);
    try std.testing.expectEqual(@as(u8, 0x1b), out[0]);
    try std.testing.expectEqual(@as(u8, '['), out[1]);
    try std.testing.expectEqual(@as(u8, 32), out[2]);
    try std.testing.expectEqual(@as(u8, 35), out[3]);
    try std.testing.expectEqual(@as(u8, 34), out[4]);
    try std.testing.expectEqual(@as(u8, 'M'), out[5]);
    // release: legacy Cb = 3 -> byte 35.
    const rel = encodeMouse(false, .release, 0, 3, 2, 80, &out);
    try std.testing.expectEqual(@as(u8, 35), out[2]);
    try std.testing.expectEqual(@as(usize, 6), rel);
    // Worst case: the bottom-right cell still fits a byte (<= 160 / <= 112).
    const edge = encodeMouse(false, .press, 2, grid_cols, grid_lines, grid_cols, &out);
    try std.testing.expectEqual(@as(usize, 6), edge);
    try std.testing.expectEqual(@as(u8, 34), out[2]); // Cb = right = 2
    try std.testing.expect(out[3] <= 112);
    try std.testing.expect(out[4] <= 160);
    // The SGR clamp: row 0 -> 1, col 0 -> 1, row past the grid -> grid_lines.
    const c1 = encodeMouse(true, .press, 0, 0, 0, 80, &out);
    try std.testing.expectEqualStrings("\x1b[<0;1;1M", out[0..c1]);
    const c2 = encodeMouse(true, .press, 0, 5, 999, 80, &out);
    try std.testing.expectEqualStrings("\x1b[<0;5;128M", out[0..c2]);
    // No room: 0 bytes, never a partial report.
    var tiny: [5]u8 = undefined;
    try std.testing.expectEqual(@as(usize, 0), encodeMouse(true, .press, 0, 3, 2, 80, &tiny));
    try std.testing.expectEqual(@as(usize, 0), encodeMouse(false, .press, 0, 3, 2, 80, &tiny));
}

test "terminal: motion-class reports coalesce, edges keep order (#1635)" {
    var t = Terminal{};
    _ = t.stageMouseMotion("\x1b[<35;5;5M");
    _ = t.stageMouseMotion("\x1b[<35;6;6M"); // newest wins; queue untouched
    try std.testing.expectEqual(@as(usize, 0), t.pendingInput());
    _ = t.pushMouse("\x1b[<0;7;7M"); // the edge flushes the staged motion first
    var buf: [64]u8 = undefined;
    const n = t.readInput(&buf);
    try std.testing.expectEqualStrings("\x1b[<35;6;6M\x1b[<0;7;7M", buf[0..n]);
    // Flush-on-read: a staged motion surfaces on the next drain alone.
    _ = t.stageMouseMotion("X");
    try std.testing.expectEqual(@as(usize, 1), t.readInput(&buf));
    try std.testing.expectEqualStrings("X", buf[0..1]);
}
