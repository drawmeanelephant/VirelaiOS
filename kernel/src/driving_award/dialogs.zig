//! Kernel-side modal dialogs: the unsaved-changes confirm, tooltips, and the About box.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const events = da.events;
const find_user_window = da.find_user_window;
const focus = da.focus;
const mark_dirty = da.mark_dirty;
const user_close = da.user_close;
const virtio_gpu = da.virtio_gpu;
const wm_mirror = da.wm_mirror;

/// Arc4 #242: open the unsaved-changes dialog for a window.
pub fn unsaved_dialog_show(target_id: u8) void {
    da.unsaved_dialog_open = true;
    da.unsaved_dialog_target = target_id;
}

/// Arc4 #242: check if the unsaved dialog is open.
pub fn unsaved_dialog_is_open() bool {
    return da.unsaved_dialog_open;
}

/// Arc4 #242: the Save button action — post WIN_UNSAVED arg0=0 (save) to
/// the target app (the app saves; the window stays open). Extracted from
/// the click hit-test so the WM's cmd-11 DIALOG save applies the SAME code.
pub fn unsaved_dialog_save() void {
    const tid = da.unsaved_dialog_target;
    da.unsaved_dialog_open = false;
    if (find_user_window(tid)) |w| {
        if (w.owner) |pid| {
            events.push(pid, .{
                .kind = events.WIN_UNSAVED,
                .flags = 0,
                .seq = 0,
                .arg0 = 0, // save
                .arg1 = 0,
            });
        }
    }
}

/// Arc4 #242: the Don't Save button action — close the target window.
/// Extracted from the click hit-test (parity by construction).
pub fn unsaved_dialog_dont_save() void {
    const tid = da.unsaved_dialog_target;
    da.unsaved_dialog_open = false;
    _ = user_close(tid);
}

/// Arc4 #242: the Cancel button action — dismiss the dialog.
pub fn unsaved_dialog_cancel() void {
    da.unsaved_dialog_open = false;
}

/// Arc4 #242: handle a click inside the unsaved dialog. The button rects
/// are the SINGLE source the shared `wnd_core.unsaved_dialog_choice_at`
/// rule mirrors (both sides agree — parity by construction).
pub fn unsaved_dialog_click(x: u32, y: u32) enum { save, dont_save, cancel, none } {
    if (!da.unsaved_dialog_open) return .none;
    // Dialog rect: centered, 200×100.
    const dlg_w: u32 = 200;
    const dlg_h: u32 = 100;
    const dlg_x: u32 = if (virtio_gpu.fb_width > dlg_w) (virtio_gpu.fb_width - dlg_w) / 2 else 0;
    const dlg_y: u32 = if (virtio_gpu.fb_height > dlg_h) (virtio_gpu.fb_height - dlg_h) / 2 else 0;
    // Save button: left, 60×20 at bottom of dialog.
    if (x >= dlg_x + 20 and x < dlg_x + 80 and y >= dlg_y + dlg_h - 30 and y < dlg_y + dlg_h - 10) {
        unsaved_dialog_save();
        return .save;
    }
    // Don't Save button: middle.
    if (x >= dlg_x + 90 and x < dlg_x + 150 and y >= dlg_y + dlg_h - 30 and y < dlg_y + dlg_h - 10) {
        unsaved_dialog_dont_save();
        return .dont_save;
    }
    // Cancel button: right — the painted 30px button (review fix, claim
    // 7639: the old 60px rect overran the 200px dialog).
    if (x >= dlg_x + 160 and x < dlg_x + 190 and y >= dlg_y + dlg_h - 30 and y < dlg_y + dlg_h - 10) {
        unsaved_dialog_cancel();
        return .cancel;
    }
    return .none;
}

/// Arc4 #242: set/clear the unsaved flag on a user window. M32 WMS8 Gate 4
/// (issue #628): also fans a kind-20 mirror so the registered WM learns the
/// dirty state as it changes (the WM's unsaved-dialog decision input).
pub fn user_set_unsaved(id: u8, flag: bool) bool {
    const w = find_user_window(id) orelse return false;
    // Review fix (claim 7639): no-op when the flag is unchanged — don't
    // fan a redundant mirror.
    if (w.unsaved == flag) return true;
    w.unsaved = flag;
    wm_mirror(id);
    return true;
}

// ---------------------------------------------------------------------------
// M27 G6 — Tooltip system
// ---------------------------------------------------------------------------

/// M27 G6 / M32 WMS8 Gate 1 (claim 4270): tooltip surface functions — the
/// kernel clamps + places + blits; the dwell/hover decision is the WM's.
/// M32 WMS6 Gate C (issue #626) / M32 WMS8 Gate 1 (claim 4270): clear the
/// tooltip — the WM owns the WHEN/HIDE decision; its TOOLTIP (cmd 8) clear
/// reaches through here. No kernel hover timer remains.
pub fn tooltip_clear() void {
    da.tooltip_visible = false;
    da.tooltip_text_len = 0;
}

/// M32 WMS6 Gate C (issue #626): set + show the tooltip IMMEDIATELY — the
/// decision channel for the registered WM (TOOLTIP cmd 8). The WM owns the
/// dwell policy by choosing WHEN to show; the kernel's old 10-tick hover timer
/// and its dwell decision died with WMS8 Gate 1. The kernel still clamps +
/// places + blits the box.
pub fn tooltip_show_now() void {
    if (da.tooltip_text_len == 0) return;
    da.tooltip_visible = true;
    _ = mark_dirty(0);
}

/// M32 WMS6 Gate C (issue #626): set + show the tooltip at the kernel's own
/// cursor (the `cursor_x/cursor_y` the box renders below) — the one call the
/// registered WM's TOOLTIP show reaches through (clamped 32-byte bound).
pub fn tooltip_show(text: []const u8) void {
    // Place at the kernel's cursor; the WM decided WHEN + WHAT. The clamp is
    // 32 bytes (the frozen WM_RPC bound) — WMS8 keeps only the surface.
    da.tooltip_x = da.cursor_x;
    da.tooltip_y = da.cursor_y;
    const len = @min(text.len, 32);
    @memcpy(da.tooltip_text[0..len], text[0..len]);
    da.tooltip_text_len = @intCast(len);
    da.tooltip_visible = true;
    _ = mark_dirty(0);
}

// ---------------------------------------------------------------------------
// M27 G2 — About dialog
// ---------------------------------------------------------------------------

pub fn about_dialog_open_dialog() void {
    if (!da.about_dialog_open) {
        da.previous_focus = da.focused_id;
        da.about_dialog_open = true;
        _ = mark_dirty(0);
    }
}

pub fn about_dialog_close() void {
    if (da.about_dialog_open) {
        da.about_dialog_open = false;
        if (da.previous_focus) |pf| {
            _ = focus(pf);
            da.previous_focus = null;
        }
        _ = mark_dirty(0);
    }
}

/// M27 G2: toggle the about dialog.
pub fn about_dialog_toggle() void {
    if (!da.about_dialog_open) {
        about_dialog_open_dialog();
    } else {
        about_dialog_close();
    }
}
