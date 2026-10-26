//! Dirty-rect damage tracking: mark_dirty/mark_damage and the repaint plan inputs.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const events = da.events;
const find_user_window = da.find_user_window;
const font_metrics = da.font_metrics;
const terminal = da.terminal;
const user_window_id_base = da.user_window_id_base;
const user_windows_max = da.user_windows_max;

// ---------------------------------------------------------------------------
// Dirty tracking
// ---------------------------------------------------------------------------

/// Mark a window dirty (by id) so the next composite repaints it. Returns
/// false for an unknown id.
pub fn mark_dirty(id: u8) bool {
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].id == id) {
            da.windows[i].dirty = true;
            return true;
        }
    }
    return false;
}

/// M33 SB4 (claim 2382): union-rect a partial damage region into a window's
/// tracked damage rect, marking it dirty. When called while `damaged` is
/// already true, the tracked rect EXPANDS to the bounding box of the new
/// rect (damage is monotonic until composite consumes it). Returns false for
/// an unknown id. The rect is CLAMPED to the window's own bounds so a caller
/// can never push damage outside the repaintable surface.
pub fn mark_damage(id: u8, x: u32, y: u32, w: u32, h: u32) bool {
    if (w == 0 or h == 0) return false;
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].id != id) continue;
        const win = &da.windows[i];
        // Clamp to the window (local coords; caller passes in-window rects).
        const cx = x;
        const cy = y;
        var cw = w;
        var ch = h;
        if (x >= win.w or y >= win.h) return true; // out-of-window -> ignore
        if (x + w > win.w) cw = win.w - x;
        if (y + h > win.h) ch = win.h - y;
        const ex = cx + cw;
        const ey = cy + ch;
        if (win.damaged) {
            const ox = win.dx;
            const oy = win.dy;
            const oex = ox + win.dw;
            const oey = oy + win.dh;
            win.dx = @min(ox, cx);
            win.dy = @min(oy, cy);
            win.dw = @max(oex, ex) - win.dx;
            win.dh = @max(oey, ey) - win.dy;
        } else {
            win.dx = cx;
            win.dy = cy;
            win.dw = cw;
            win.dh = ch;
            win.damaged = true;
        }
        win.dirty = true;
        // card #3034 (ADR 0007 X4): user-surface damage is the seat's
        // backpressure edge — the hook decides whether a damage tick is
        // owed (at most one pending); null = no WM = no-op.
        if (win.kind == .user) {
            if (da.wm_damage_hook) |hook| hook();
        }
        return true;
    }
    return false;
}

/// M33 SB4: the per-surface damage rect for a user window, or null when it
/// has no rect-granular (partial) damage pending (whole-window dirty only).
pub const DamageRect = struct {
    x: u32,
    y: u32,
    w: u32,
    h: u32,
};
pub fn user_damage(id: u8) ?DamageRect {
    const win = find_user_window(id) orelse return null;
    if (!win.damaged) return null;
    return .{ .x = win.dx, .y = win.dy, .w = win.dw, .h = win.dh };
}

/// M33 SB4: a per-surface dirty bitmask for the COMPOSITE_TICK (kind-18)
/// damage payload — bit i is set when user window (i + user_window_id_base)
/// has pending damage this tick. The registered WM consumes this to know
/// WHICH surfaces changed (the rects come via `user_damage`); SB5's compose-N
/// repaints only those.
pub fn user_damage_mask() u32 {
    var mask: u32 = 0;
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        // Only `.user` surfaces carry the bitmask — the fixed layers (terminal
        // 0, wallpaper 254, taskbar 255, dock 253) are compositor-owned, not
        // user surfaces, and their ids would overflow the mask shift.
        if (da.windows[i].kind != .user) continue;
        const idx = da.windows[i].id - user_window_id_base;
        if (idx >= user_windows_max) continue;
        if (da.windows[i].dirty) {
            mask |= (@as(u32, 1) << @intCast(idx));
        }
    }
    return mask;
}

/// Mark the terminal (window 0) dirty — the Road Pops tee's write path.
pub fn mark_terminal_dirty() void {
    if (da.win_count > 0) da.windows[0].dirty = true;
}

/// M73m (#1662): a palette write recolours the WHOLE desktop — chrome,
/// tray, wallpaper and every window-bound terminal all resolve theme state
/// at paint time — so mark every window dirty. The next composite
/// re-resolves each colour: live apply, no reboot, no re-exec.
pub fn mark_palette_dirty() void {
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) da.windows[i].dirty = true;
}

/// M80i (#1725): the grid half of the `font_size` apply chain. Moves the
/// zoom ladder (font_metrics.set_size), re-flows every bound grid to the
/// new cell IMMEDIATELY (the next composite's syncWindowCols would get
/// there anyway; doing it here makes the change atomic with the setting
/// and runs before the repaint the caller triggers), and tells each
/// bound TUI its canvas changed in CELL terms — a font change is a
/// winsize change for a cell-addressed app — with the SAME payload
/// shape `user_resize` pushes (the pixel rect, arg0=w arg1=h). The cell
/// mapping the app derives from it (tabapp.CellGrid, the charm overlay's
/// cellsForRect) mirrors font_metrics at that instant.
pub fn apply_grid_font_size(s: font_metrics.Size) void {
    font_metrics.set_size(s);
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        const w = &da.windows[i];
        if (w.kind != .user) continue;
        if (terminal.screenForWindow(w.id) == null) continue;
        terminal.syncWindowCols(w.id, w.w);
        if (w.owner) |owner| {
            events.push(owner, .{
                .kind = events.WIN_RESIZE,
                .flags = 0,
                .seq = 0,
                .arg0 = w.w,
                .arg1 = w.h,
            });
        }
        w.dirty = true;
    }
}
