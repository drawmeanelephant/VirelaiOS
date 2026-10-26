//! System-tray helpers: clock text, theme letter, clipboard indicator (pure, host-testable).
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const clipboard = da.clipboard;
const mark_dirty = da.mark_dirty;
const taskbar_entry_active = da.taskbar_entry_active;
const tray_h = da.tray_h;
const tray_w = da.tray_w;
const tray_x = da.tray_x;
const tray_y = da.tray_y;

// ---------------------------------------------------------------------------
// Arc2 W3 — system tray helpers (pure, host-testable, isolated for merge)
// ---------------------------------------------------------------------------

/// Tray rect — right 80px of 20px taskbar at y=700. Pure — host-testable.
pub fn tray_rect() struct { x: u32, y: u32, w: u32, h: u32 } {
    return .{ .x = tray_x, .y = tray_y, .w = tray_w, .h = tray_h };
}

/// Format HH:MM from tick (seconds since boot, 1 Hz timer). Zero-padded,
/// 24h wrap. Pure — host-testable. Writes into buf[0..5] and returns slice.
pub fn format_hhmm(buf: []u8, ticks: u64) []const u8 {
    if (buf.len < 5) return "";
    const total_minutes = (ticks / 60) % (24 * 60);
    const hh = total_minutes / 60;
    const mm = total_minutes % 60;
    buf[0] = @as(u8, @intCast('0' + hh / 10));
    buf[1] = @as(u8, @intCast('0' + hh % 10));
    buf[2] = ':';
    buf[3] = @as(u8, @intCast('0' + mm / 10));
    buf[4] = @as(u8, @intCast('0' + mm % 10));
    return buf[0..5];
}

/// Theme letter D/L/A per theme_id. Pure — host-testable.
pub fn theme_letter() u8 {
    return switch (da.theme_id) {
        1 => 'L',
        2 => 'A',
        else => 'D',
    };
}

/// Theme accent for tray letter — taskbar active entry color per theme.
pub fn tray_theme_accent() u32 {
    return taskbar_entry_active();
}

/// Clipboard filled when current_len !=0. Pure wrapper — host-testable.
pub fn tray_clipboard_filled() bool {
    return clipboard.current_len() != 0;
}

/// M32 WMS6 Gate E (issue #626): apply the registered WM's TRAY decision —
/// the clock string, theme letter, and clipboard indicator (each optional;
/// a null field leaves the current value untouched). The kernel clamps to
/// the frozen bounds (5-char clock, one of D/L/A, clip bool) and marks the
/// taskbar (window 255) dirty so composite repaints it.
pub fn tray_set(clock: ?[]const u8, theme: ?u8, clip: ?bool) void {
    if (clock) |c| {
        var i: usize = 0;
        while (i < 5 and i < c.len) : (i += 1) da.wm_tray_clock_text[i] = c[i];
        da.wm_tray_clock_set = true;
    }
    if (theme) |t| {
        da.wm_tray_theme = t;
        da.wm_tray_theme_set = true;
    }
    if (clip) |v| {
        da.wm_tray_clip = v;
        da.wm_tray_clip_set = true;
    }
    _ = mark_dirty(255); // taskbar
}

/// Current tray tick (for tests).
pub fn tray_current_tick() u64 {
    return da.tray_tick;
}
pub fn tray_has_clock() bool {
    return da.tray_has_tick;
}
