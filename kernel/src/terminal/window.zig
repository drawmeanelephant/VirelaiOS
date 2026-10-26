//! The window front-end plumbing (split from terminal.zig, #3753): bind a
//! terminal to a `.user` window, pump output into its `Screen`, clipboard
//! copy/paste, and the OSC side-effects (title frame, clipboard set).

const std = @import("std");
const clipboard = @import("../clipboard.zig");
const driving_award = @import("../driving_award.zig");
const wm_server = @import("../wm_server.zig");
const mailbox = @import("../mailbox.zig");
const wnd_core = @import("../wnd_core.zig");
const font_metrics = @import("../font_metrics.zig");
const registry = @import("../terminal.zig");
const screen_mod = @import("screen.zig");
const session_mod = @import("session.zig");
const create = registry.create;
const get = registry.get;
const handleOf = registry.handleOf;
const Screen = screen_mod.Screen;
const grid_cols = screen_mod.grid_cols;
const Terminal = session_mod.Terminal;
const out_capacity = session_mod.out_capacity;

/// #1082 (A5): the terminal bound to the `.user` window `window_id`, or
/// null when the window is not a terminal front-end. `input.zig` uses this
/// to encode focused-window keys into the bound terminal's input queue.
pub fn windowTerminal(window_id: u8) ?*Terminal {
    for (&registry.terminals) |*t| {
        if (t.in_use and t.attached and t.front_end == .window) {
            if (t.window_id) |wid| {
                if (wid == window_id) return t;
            }
        }
    }
    return null;
}

/// #1082 (A6): auto-detach any terminal bound to `window_id` (called by the
/// window close / owner-exit path). The terminal survives, buffered,
/// unattached; the serial console is NOT reclaimed.
pub fn detachWindow(window_id: u8) void {
    if (windowTerminal(window_id)) |t| t.detach();
}

/// True when any live terminal holds the window front-end (used by the
/// attach syscall to keep serial/window/net mutually exclusive, B2/A2).
pub fn anyWindowAttached() bool {
    for (&registry.terminals) |*t| {
        if (t.in_use and t.attached and t.front_end == .window) return true;
    }
    return false;
}

/// #1082 (A4): drain terminal `handle`'s output ring into its presentation
/// grid. Returns bytes moved; a no-op when the terminal has no window
/// binding. The caller marks the bound window damaged (deferred present).
pub fn pumpWindowOutput(handle: usize) usize {
    const t = get(handle) orelse return 0;
    if (t.window_id == null) return 0;
    var total: usize = 0;
    var buf: [128]u8 = undefined;
    while (true) {
        const n = t.readOut(&buf);
        if (n == 0) break;
        registry.screens[handle].feed(buf[0..n]);
        // M80f (#1723): output parsing may synthesize a DSR/CPR/DA answer.
        // It is delivered through the same bounded input FIFO as keys,
        // mouse reports, and paste bytes, preserving generation order.
        const screen = &registry.screens[handle];
        if (screen.pending_reply_len > 0) {
            _ = t.pushInput(screen.pending_reply[0..screen.pending_reply_len]);
            screen.pending_reply_len = 0;
        }
        total += n;
    }
    // M80g (#1715): parsing may also have queued OSC effects — the window
    // title (its buffer of record plus M79d's kind-11 set_title seam when
    // a seat is registered) and OSC 52 clipboard copies. Delivered once
    // the bytes are parsed, beside the M80f reply drain above.
    if (t.window_id) |wid| deliverScreenEffects(wid, &registry.screens[handle]);
    return total;
}

/// #1630 (M73f-1): append `bytes` to a WINDOW-bound terminal without dropping.
/// The ring is 4 KiB and a TUI frame can be larger; the window path drains
/// into the grid as it writes, in chunks that always fit, so `out_dropped`
/// stays 0. Serial/net keep the drop-oldest ring policy (non-goal). Returns
/// bytes accepted (`bytes.len` when the handle is window-bound, else 0).
pub fn writeWindow(handle: usize, bytes: []const u8) usize {
    const t = get(handle) orelse return 0;
    if (t.window_id == null) return 0;
    var off: usize = 0;
    while (off < bytes.len) {
        const room = out_capacity - t.pendingOut();
        if (room == 0) {
            // Defensive: a leftover full ring must drain before we write.
            // If it cannot, return the prefix accepted rather than spin.
            if (pumpWindowOutput(handle) == 0) return off;
            continue;
        }
        const take = @min(room, bytes.len - off);
        _ = t.write(bytes[off..][0..take]);
        _ = pumpWindowOutput(handle);
        off += take;
    }
    return bytes.len;
}

/// #1082 (A4): the presentation grid bound to `window_id`, or null when the
/// window is not a terminal front-end. `driving_award.paint` renders it.
pub fn screenOf(window_id: u8) ?*const Screen {
    const t = windowTerminal(window_id) orelse return null;
    const h = handleOf(t) orelse return null;
    return &registry.screens[h];
}

/// M49 SD5 (#1132): the mutable presentation grid bound to `window_id`.
pub fn screenForWindow(window_id: u8) ?*Screen {
    const t = windowTerminal(window_id) orelse return null;
    const h = handleOf(t) orelse return null;
    return &registry.screens[h];
}

/// M49 SD5 (#1132): the mutable presentation grid for an already-resolved
/// window-bound terminal.
pub fn screenForTerminal(t: *Terminal) ?*Screen {
    const h = handleOf(t) orelse return null;
    return &registry.screens[h];
}

/// M49 SD5 (#1132): sync the presentation grid to the window's current
/// client width, reflowing the stored lines when the column count changed.
/// Called by the compositor before rendering (never in IRQ context).
pub fn syncWindowCols(window_id: u8, pixel_w: u32) void {
    const s = screenForWindow(window_id) orelse return;
    // M73l (#1661): cols come from the face's advance, not a literal —
    // this is the winsize mirror every TUI (charm signals, tabapp) reads.
    _ = s.setCols(@intCast(pixel_w / font_metrics.cell_w));
}

/// M49 SD5 (#1132): copy the window terminal's selection into the shared
/// clipboard. Returns the bytes copied (0 when there is no selection).
pub fn copySelectionToClipboard(window_id: u8) usize {
    const s = screenForWindow(window_id) orelse return 0;
    var buf: [clipboard.capacity]u8 = undefined;
    const n = s.copySelection(&buf);
    if (n == 0) return 0;
    return clipboard.set(buf[0..n]);
}

/// M73e (#1629): Ctrl+Shift+V — push the clipboard into the bound
/// terminal's input queue, wrapped in `\e[200~ … \e[201~` when the app
/// enabled DECSET 2004 (the shell requests it at startup). Returns the
/// bytes accepted; `in_capacity` holds one full paste (512 B clipboard +
/// 12 B markers), and any overflow is the queue's drop-newest +
/// `in_dropped` — visible, never silent. Returns 0 when the window has no
/// terminal or the clipboard is empty.
pub fn pasteFromClipboard(window_id: u8) usize {
    const t = windowTerminal(window_id) orelse return 0;
    const s = screenForWindow(window_id) orelse return 0;
    var content: [clipboard.capacity]u8 = undefined;
    const n = clipboard.get(&content);
    if (n == 0) return 0;
    if (!s.bracketed_paste) return t.pushInput(content[0..n]);
    var buf: [clipboard.capacity + 12]u8 = undefined;
    @memcpy(buf[0..6], "\x1b[200~");
    @memcpy(buf[6..][0..n], content[0..n]);
    @memcpy(buf[6 + n ..][0..6], "\x1b[201~");
    return t.pushInput(buf[0 .. 6 + n + 6]);
}

// ---------------------------------------------------------------------------
// M80g (#1715) — OSC delivery: titles to the window and seat, copies to the
// clipboard. The parser queues on the pure Screen; this is the pump side,
// the only place the global effects happen.
// ---------------------------------------------------------------------------
/// M80g: the M79d seam frame — one WM_RPC kind-11 (set_title) for
/// `window_id`. Pure: the title truncates at the frozen 24-byte wire
/// field and the frame is zero-padded (ADR 0015's fixed layout, bounded
/// by `wm_rpc_max`).
fn titleFrame(window_id: u8, title: []const u8) wnd_core.WmRpc {
    var frame: wnd_core.WmRpc = std.mem.zeroes(wnd_core.WmRpc);
    frame.kind = wnd_core.wm_rpc_kind_set_title;
    frame.id = window_id;
    const n = @min(title.len, wnd_core.wm_rpc_title_max);
    @memcpy(frame.title[0..n], title[0..n]);
    return frame;
}

/// M80g: deliver what the parser queued while the pump drained output.
/// The title lands in the window title buffer ALWAYS — the machine's
/// title of record (the shim paints it; WMCTL's window-name resolves
/// labels from it, the no-seat fallback `vi.WinSetTitle` performs from
/// userland) — and, when a seat is registered, also rides M79d's kind-11
/// set_title so the rail's tab follows live. OSC 52 copies bridge into
/// the shared kernel clipboard through the existing setter.
fn deliverScreenEffects(window_id: u8, s: *Screen) void {
    if (s.title_dirty) {
        s.title_dirty = false;
        _ = driving_award.set_window_title(window_id, s.title[0..s.title_len]);
        if (wm_server.registered_pid()) |pid| {
            const frame = titleFrame(window_id, s.title[0..s.title_len]);
            _ = mailbox.send(pid, std.mem.asBytes(&frame));
        }
    }
    if (s.clip_dirty) {
        s.clip_dirty = false;
        _ = clipboard.set(s.clip_data[0..s.clip_len]);
    }
}

test "terminal: pasteFromClipboard wraps per mode and lands intact (#1629)" {
    for (&registry.terminals) |*t| t.reset();
    const payload = "LINE-00 pppp\nLINE-29 pppp";
    _ = clipboard.set(payload);
    const h = create(7) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));
    // Bracketed OFF: raw content, newline included.
    screenForWindow(7).?.bracketed_paste = false;
    try std.testing.expectEqual(payload.len, pasteFromClipboard(7));
    var buf: [96]u8 = undefined;
    try std.testing.expectEqual(payload.len, t.readInput(&buf));
    try std.testing.expectEqualStrings(payload, buf[0..payload.len]);
    // Bracketed ON: markers around the same bytes.
    screenForWindow(7).?.feed("\x1b[?2004h");
    _ = pasteFromClipboard(7);
    const m = t.readInput(&buf);
    const wrapped = "\x1b[200~" ++ payload ++ "\x1b[201~";
    try std.testing.expectEqual(wrapped.len, m);
    try std.testing.expectEqualStrings(wrapped, buf[0..m]);
}

test "terminal: a full-clipboard paste exceeds the old 256 B queue, dropping nothing (#1629)" {
    var t = Terminal{};
    var content: [clipboard.capacity]u8 = undefined;
    @memset(&content, 'x');
    var wrap: [clipboard.capacity + 12]u8 = undefined;
    @memcpy(wrap[0..6], "\x1b[200~");
    @memcpy(wrap[6..][0..clipboard.capacity], &content);
    @memcpy(wrap[6 + clipboard.capacity ..][0..6], "\x1b[201~");
    try std.testing.expectEqual(clipboard.capacity + 12, t.pushInput(&wrap));
    try std.testing.expectEqual(@as(u64, 0), t.in_dropped);
    try std.testing.expectEqual(clipboard.capacity + 12, t.pendingInput());
    var buf: [clipboard.capacity + 16]u8 = undefined;
    try std.testing.expectEqual(clipboard.capacity + 12, t.readInput(&buf));
    try std.testing.expectEqualStrings(&wrap, buf[0 .. clipboard.capacity + 12]);
}

test "terminal: window binding is exclusive per terminal and per window" {
    for (&registry.terminals) |*t| t.reset();
    const a = create(1) orelse return error.TestUnexpectedResult;
    const b = create(2) orelse return error.TestUnexpectedResult;
    const ta = get(a).?;
    const tb = get(b).?;
    try std.testing.expect(ta.attachWindow(2));
    try std.testing.expectEqual(@as(?u8, 2), ta.window_id);
    try std.testing.expect(windowTerminal(2) == ta);
    // Another terminal may not bind the same window.
    try std.testing.expect(!tb.attachWindow(2));
    // The same terminal may not bind a second window (front-end exclusive).
    try std.testing.expect(!ta.attachWindow(3));
    // Idempotent re-attach of the same window succeeds.
    try std.testing.expect(ta.attachWindow(2));
    // A different window on a free terminal succeeds.
    try std.testing.expect(tb.attachWindow(3));
    try std.testing.expect(windowTerminal(3) == tb);
    // Detach frees the binding for both lookups.
    detachWindow(2);
    try std.testing.expect(windowTerminal(2) == null);
    try std.testing.expect(!ta.isAttached());
    for (&registry.terminals) |*t| t.reset();
}

test "terminal: window pump drains the output ring into the grid and screenOf finds it" {
    for (&registry.terminals) |*t| t.reset();
    const h = create(3) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));
    _ = t.write("hi\nthere");
    try std.testing.expectEqual(@as(usize, 8), pumpWindowOutput(h));
    const scr = screenOf(7) orelse return error.TestUnexpectedResult;
    try std.testing.expectEqualStrings("hi", scr.line(0));
    try std.testing.expectEqualStrings("there", scr.line(1));
    // A second pump with an empty ring moves nothing (idempotent).
    try std.testing.expectEqual(@as(usize, 0), pumpWindowOutput(h));
    // An unbound handle pumps nothing.
    const h2 = create(4) orelse return error.TestUnexpectedResult;
    _ = get(h2).?.write("x");
    try std.testing.expectEqual(@as(usize, 0), pumpWindowOutput(h2));
    try std.testing.expect(screenOf(99) == null);
    for (&registry.terminals) |*tt| tt.reset();
    for (&registry.screens) |*ss| ss.reset();
}

test "terminal: window pump serializes output replies behind existing input" {
    for (&registry.terminals) |*tt| tt.reset();
    for (&registry.screens) |*ss| ss.reset();
    const h = create(3) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));
    try std.testing.expectEqual(@as(usize, 1), t.pushInput("k"));
    _ = t.write("\x1b[2;3H\x1b[6n");
    try std.testing.expectEqual(@as(usize, 10), pumpWindowOutput(h));
    var buf: [32]u8 = undefined;
    const n = t.readInput(&buf);
    try std.testing.expectEqual(@as(usize, 7), n);
    try std.testing.expectEqualStrings("k\x1b[2;3R", buf[0..n]);
    for (&registry.terminals) |*tt| tt.reset();
    for (&registry.screens) |*ss| ss.reset();
}

test "terminal: a window write larger than the ring cannot drop (M73f-1)" {
    for (&registry.terminals) |*tt| tt.reset();
    for (&registry.screens) |*ss| ss.reset();
    const h = create(3) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));

    // Distinctive head and tail around a payload bigger than the 4 KiB ring.
    const extra = 64;
    var big: [out_capacity + extra]u8 = undefined;
    @memcpy(big[0..4], "HEAD");
    for (big[4 .. big.len - 4], 0..) |*b, i| b.* = 'A' + @as(u8, @intCast(i % 26));
    @memcpy(big[big.len - 4 ..], "TAIL");

    try std.testing.expectEqual(big.len, writeWindow(h, &big));
    try std.testing.expectEqual(@as(u64, 0), t.out_dropped);
    try std.testing.expectEqual(@as(usize, 0), t.pendingOut());

    const scr = screenOf(7) orelse return error.TestUnexpectedResult;
    try std.testing.expectEqualStrings("HEAD", scr.line(0)[0..4]);
    const last_off = big.len - 4;
    const last_line = last_off / grid_cols;
    const last_col = last_off % grid_cols;
    try std.testing.expectEqual(@as(u21, 'T'), scr.cellAt(last_line, last_col).base);
    try std.testing.expectEqual(@as(u21, 'A'), scr.cellAt(last_line, last_col + 1).base);
    try std.testing.expectEqual(@as(u21, 'I'), scr.cellAt(last_line, last_col + 2).base);
    try std.testing.expectEqual(@as(u21, 'L'), scr.cellAt(last_line, last_col + 3).base);

    // An unbound handle does not consume (serial/net keep Terminal.write).
    const h2 = create(4) orelse return error.TestUnexpectedResult;
    try std.testing.expectEqual(@as(usize, 0), writeWindow(h2, "nope"));
    try std.testing.expectEqual(@as(usize, 0), get(h2).?.pendingOut());

    for (&registry.terminals) |*tt| tt.reset();
    for (&registry.screens) |*ss| ss.reset();
}

test "terminal: the kind-11 set_title frame is the frozen WM_RPC wire (#1715)" {
    const long = "0123456789abcdefghijklmnopqrst"; // 30 > the 24-byte field
    const frame = titleFrame(7, long);
    try std.testing.expect(@sizeOf(wnd_core.WmRpc) <= wnd_core.wm_rpc_max);
    try std.testing.expectEqual(wnd_core.wm_rpc_kind_set_title, frame.kind);
    try std.testing.expectEqual(@as(u8, 11), frame.kind); // mirrors vi.WmRpcKindSetTitle
    try std.testing.expectEqual(@as(u8, 7), frame.id);
    try std.testing.expectEqual(@as(u8, 0), frame.reply_to); // no reply expected
    try std.testing.expectEqualStrings(long[0..24], frame.title[0..wnd_core.wm_rpc_title_max]);
}

test "terminal: copySelectionToClipboard is a no-op without a window binding" {
    for (&registry.terminals) |*t| t.reset();
    try std.testing.expectEqual(@as(usize, 0), copySelectionToClipboard(42));
    for (&registry.terminals) |*t| t.reset();
}
