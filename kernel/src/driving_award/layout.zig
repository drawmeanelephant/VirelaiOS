//! Window layout policy: tiling, minimize/dock/taskbar, maximize, fullscreen, always-on-top, modal/transient, keyboard move/resize.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const dock_w = da.dock_w;
const find_user_window = da.find_user_window;
const focus = da.focus;
const mark_dirty = da.mark_dirty;
const raise = da.raise;
const reflow = da.reflow;
const taskbar_h = da.taskbar_h;
const tile_master_pct = da.tile_master_pct;
const user_close = da.user_close;
const user_open = da.user_open;
const user_window_id_base = da.user_window_id_base;
const user_window_slot = da.user_window_slot;
const user_windows_max = da.user_windows_max;
const virtio_gpu = da.virtio_gpu;
const wm_mirror = da.wm_mirror;

// M32 WMS8 Gate 6 (issue #628): the kernel's drag-snap applied primitives
// (snap_zone_for_point / snap_zone_bounds / snap_window / snap_restore /
// snap_is_snapped / snap_current_zone + the per-window snap state) are
// DELETED — the WM owns snap-on-drop via its own mirror + the SHARED
// wnd_core.snap_zone_for_point / wnd_core.snap_zone_bounds rules (which
// stay). The kernel shim no longer snaps from the pointer.
// Tiling (M21 W1/W2) is a SEPARATE geometry policy the WM also serves via
// SET_STATE; it is NOT affected and its primitives remain below.

// ---------------------------------------------------------------------------
// M21 W1/W2 — Tiling mode
// ---------------------------------------------------------------------------

/// Usable area for tiled windows (avoids dock + taskbar).
pub const tile_x_start = dock_w;
pub const tile_y_start: u32 = 0;

/// M21 W1: toggle tiling for the focused window. When tiling is activated,
/// the focused window becomes master (or stack if a master already exists).
/// Max 2 tiled windows per workspace; third window reverts to floating.
pub fn toggle_tiling() void {
    _ = toggle_tiling_for(da.focused_id);
}

/// #3759 (slot-65 LAYOUT, cmd 17): the id-parameterized form the WM's
/// decision channel drives — the WM, not the kernel, names WHICH window
/// toggles; the kernel applies the SAME chain the shim's focused-window
/// toggle runs (parity by construction). Returns false for an id that
/// cannot tile (non-user window).
pub fn toggle_tiling_for(fid: u8) bool {
    if (fid < user_window_id_base or fid >= user_window_id_base + user_windows_max) return false;
    if (find_user_window(fid) == null) return false;
    // If already tiled, detach this window (back to floating).
    if (da.tile_master_id) |mid| {
        if (mid == fid) {
            da.tile_master_id = null;
            if (da.tile_stack_id) |sid| {
                // Stack becomes the only tiled window — promote to master.
                da.tile_master_id = sid;
                da.tile_stack_id = null;
            }
            da.tile_mode = da.tile_master_id != null;
            apply_tile_layout();
            return true;
        }
    }
    if (da.tile_stack_id) |sid| {
        if (sid == fid) {
            da.tile_stack_id = null;
            da.tile_mode = da.tile_master_id != null;
            apply_tile_layout();
            return true;
        }
    }
    // Not yet tiled — add this window.
    if (da.tile_master_id == null) {
        da.tile_master_id = fid;
    } else if (da.tile_stack_id == null) {
        da.tile_stack_id = fid;
    } else {
        // Both slots occupied — detach the oldest (master) and shift.
        da.tile_master_id = da.tile_stack_id;
        da.tile_stack_id = fid;
    }
    da.tile_mode = true;
    apply_tile_layout();
    return true;
}

/// M21 W2: swap which window is master and which is detail. Returns false
/// when tiling is off (nothing to swap — the slot-65 LAYOUT channel maps
/// that to an honest EINVAL).
pub fn swap_master() bool {
    if (!da.tile_mode) return false;
    if (da.tile_master_id) |mid| {
        if (da.tile_stack_id) |sid| {
            da.tile_master_id = sid;
            da.tile_stack_id = mid;
        }
    }
    da.tile_master_side = !da.tile_master_side;
    apply_tile_layout();
    return true;
}

/// Apply the current tiling layout: recalculate window rects for all
/// tiled windows. Master gets 2/3 width, detail gets 1/3.
pub fn apply_tile_layout() void {
    if (!da.tile_mode) return;
    const usable_w = virtio_gpu.fb_width - tile_x_start;
    const usable_h: u32 = virtio_gpu.fb_height - taskbar_h;
    const master_w: u32 = usable_w * tile_master_pct / 1000;
    const detail_w: u32 = usable_w - master_w;
    // Master window.
    if (da.tile_master_id) |mid| {
        if (find_user_window(mid)) |w| {
            const nx = if (da.tile_master_side) tile_x_start else tile_x_start + detail_w;
            // WM1: buffer follows the tile size; on pool exhaustion the
            // window keeps its old size at the new position.
            _ = reflow(w, nx, tile_y_start, master_w, usable_h);
            w.dirty = true;
            _ = mark_dirty(0); // terminal repaint behind old rect
        }
    }
    // Detail (stack) window.
    if (da.tile_stack_id) |sid| {
        if (find_user_window(sid)) |w| {
            const nx = if (da.tile_master_side) tile_x_start + master_w else tile_x_start;
            _ = reflow(w, nx, tile_y_start, detail_w, usable_h);
            w.dirty = true;
            _ = mark_dirty(0);
        }
    }
}

/// M21 W3: minimize the focused window. Saves its current rect for
/// restore. Returns false for unknown id or non-user window.
pub fn minimize_window(id: u8) bool {
    const s = user_window_slot(id) orelse return false;
    const w = find_user_window(id) orelse return false;
    // Save current rect for restore.
    da.minimize_prev_x[s] = w.x;
    da.minimize_prev_y[s] = w.y;
    da.minimize_prev_w[s] = w.w;
    da.minimize_prev_h[s] = w.h;
    da.minimize_prev_valid[s] = true;
    // Mark as minimized and hide.
    w.minimized = true;
    w.visible = false;
    w.dirty = true;
    _ = mark_dirty(0); // reveal whatever sat under
    // WM3 (issue #707 card 3): the WM's mirror must follow a minimize —
    // it is a visibility change (the WMS5 rule) and the WM's taskbar
    // restore bit is derived from the hidden mirror. No-op in shim mode.
    wm_mirror(id);
    // Fall focus back to terminal or next window.
    if (da.focused_id == id) {
        da.focused_id = 0;
    }
    return true;
}

/// M21 W3: restore a minimized window from dock click. Returns false
/// if the window is not minimized or unknown.
pub fn restore_from_dock(id: u8) bool {
    const s = user_window_slot(id) orelse return false;
    const w = find_user_window(id) orelse return false;
    if (!w.minimized) return false;
    // Restore saved rect (same size the buffer already holds — WM1
    // reflow is a position-only fast path here).
    if (da.minimize_prev_valid[s]) {
        if (!reflow(w, da.minimize_prev_x[s], da.minimize_prev_y[s], da.minimize_prev_w[s], da.minimize_prev_h[s])) return false;
        da.minimize_prev_valid[s] = false;
    }
    w.minimized = false;
    w.visible = true;
    w.dirty = true;
    _ = focus(id);
    _ = raise(id);
    return true;
}

/// M32 WMS6 Gate D (issue #626): the dock-icon click ACTION — the exact chain
/// the shim's dock-click handler runs (restore the first minimized user
/// window, else focus + raise a user window, else open one), applied by the
/// registered WM via DOCK (cmd 9). Clamped: the bar has exactly 5 icons
/// (c n t b s), so an out-of-range index is refused (EINVAL). The action
/// itself is byte-identical to the shim's, so a WM decision and a shim click
/// are identical actions.
pub fn dock_icon_click(idx: u32) bool {
    if (idx >= 5) return false; // the bar has 5 icons
    var restored = false;
    var k: usize = 0;
    while (k < da.win_count) : (k += 1) {
        if (da.windows[k].kind == .user and da.windows[k].minimized) {
            _ = restore_from_dock(da.windows[k].id);
            restored = true;
            break;
        }
    }
    if (!restored) {
        var has_user = false;
        var kk: usize = 0;
        while (kk < da.win_count) : (kk += 1) {
            if (da.windows[kk].kind == .user) {
                has_user = true;
                break;
            }
        }
        if (!has_user) {
            _ = user_open(64, 64, 512, 384, 99);
        } else {
            var kkk: usize = 0;
            while (kkk < da.win_count) : (kkk += 1) {
                if (da.windows[kkk].kind == .user) {
                    _ = focus(da.windows[kkk].id);
                    _ = raise(da.windows[kkk].id);
                    break;
                }
            }
        }
    }
    _ = mark_dirty(0);
    return true;
}

/// WM3 (issue #707 card 3): the taskbar ENTRY enumeration snapshot — the
/// id-ascending current-workspace filter the `.taskbar` render walks.
/// The render iterates THIS (single source); `dui taskbar` reports it so
/// a live gate can grep the exact entries the bar draws.
pub const TaskbarEntry = struct { id: u8, focused: bool, minimized: bool };

pub fn taskbar_entries(buf: *[user_windows_max]TaskbarEntry) usize {
    var n: usize = 0;
    var id_i: usize = 0;
    while (id_i < user_windows_max) : (id_i += 1) {
        const ww = find_user_window(user_window_id_base + @as(u8, @intCast(id_i))) orelse continue;
        if (ww.workspace != da.current_workspace) continue;
        buf[n] = .{
            .id = ww.id,
            .focused = da.focused_id == ww.id,
            .minimized = ww.minimized and !ww.visible,
        };
        n += 1;
    }
    return n;
}

/// WM3 (issue #707 card 3): the taskbar-entry click ACTION — restore a
/// minimized entry, else focus + raise a visible one — applied by the
/// registered WM via TASKBAR (slot-65 cmd 12). The WM — not the kernel —
/// decides WHICH entry the click hit (it hit-tests the shared wnd_core
/// entry rects); the kernel clamps (the id must name a live user window)
/// and applies the SAME chain a shim click would run, so a WM decision
/// and a click on the same window are identical actions. Mirrors the
/// window back (the WM's mirror must see the restored/focused state).
pub fn taskbar_click(id: u8) bool {
    const win = find_user_window(id) orelse return false;
    if (win.minimized and !win.visible) {
        if (!restore_from_dock(id)) return false;
    } else {
        _ = focus(id);
        _ = raise(id);
    }
    _ = mark_dirty(0);
    wm_mirror(id);
    return true;
}

// ---------------------------------------------------------------------------
// M21 W6 — Maximize / restore
// ---------------------------------------------------------------------------

/// M21 W6: toggle maximize on the focused window. Maximized windows
/// fill the workspace area (screen minus taskbar and dock). Title bar
/// stays visible. Saves the pre-maximize rect for restore.
pub fn toggle_maximize(id: u8) bool {
    const s = user_window_slot(id) orelse return false;
    const w = find_user_window(id) orelse return false;
    if (w.maximized) {
        // Restore from maximize (WM1: buffer follows back down).
        if (da.pre_max_valid[s]) {
            if (!reflow(w, da.pre_max_x[s], da.pre_max_y[s], da.pre_max_w[s], da.pre_max_h[s])) return false;
            da.pre_max_valid[s] = false;
        }
        w.maximized = false;
    } else {
        // Save current rect and maximize.
        da.pre_max_x[s] = w.x;
        da.pre_max_y[s] = w.y;
        da.pre_max_w[s] = w.w;
        da.pre_max_h[s] = w.h;
        da.pre_max_valid[s] = true;
        // WM1: the pool buffer grows to the workspace area (content
        // overlap preserved); on pool exhaustion the window stays put.
        if (!reflow(w, dock_w, 0, virtio_gpu.fb_width - dock_w, virtio_gpu.fb_height - taskbar_h)) return false;
        w.maximized = true;
        // W6 edge case: tiled wins — clear tile state.
        if (da.tile_master_id) |mid| {
            if (mid == id) {
                da.tile_master_id = null;
                if (da.tile_stack_id) |sid| {
                    da.tile_master_id = sid;
                    da.tile_stack_id = null;
                }
                da.tile_mode = da.tile_master_id != null;
            }
        }
        if (da.tile_stack_id) |sid| {
            if (sid == id) da.tile_stack_id = null;
        }
    }
    w.dirty = true;
    _ = mark_dirty(0);
    return true;
}

// ---------------------------------------------------------------------------
// M21 W7 — Fullscreen mode
// ---------------------------------------------------------------------------

/// M21 W7: toggle fullscreen on the focused window. Fullscreen fills
/// the ENTIRE framebuffer (1280x720), no title bar, no border, taskbar
/// and dock hidden. Only the fullscreen window paints.
pub fn toggle_fullscreen(id: u8) bool {
    const w = find_user_window(id) orelse return false;
    if (da.fullscreen_active and da.fullscreen_window_id == id) {
        // Exit fullscreen — restore saved rect (WM1: buffer follows).
        const s = user_window_slot(id) orelse return false;
        if (da.pre_max_valid[s]) {
            if (!reflow(w, da.pre_max_x[s], da.pre_max_y[s], da.pre_max_w[s], da.pre_max_h[s])) return false;
            da.pre_max_valid[s] = false;
        }
        da.fullscreen_active = false;
        da.fullscreen_window_id = null;
        // Show taskbar and dock again.
        var i: usize = 0;
        while (i < da.win_count) : (i += 1) {
            if (da.windows[i].kind == .taskbar or da.windows[i].kind == .dock) {
                da.windows[i].visible = true;
                da.windows[i].dirty = true;
            }
        }
    } else {
        // Enter fullscreen — save current rect and fill screen.
        const s = user_window_slot(id) orelse return false;
        da.pre_max_x[s] = w.x;
        da.pre_max_y[s] = w.y;
        da.pre_max_w[s] = w.w;
        da.pre_max_h[s] = w.h;
        da.pre_max_valid[s] = true;
        // WM1: the pool buffer grows to the full framebuffer (content
        // overlap preserved); on pool exhaustion the window stays put.
        if (!reflow(w, 0, 0, virtio_gpu.fb_width, virtio_gpu.fb_height)) return false;
        da.fullscreen_active = true;
        da.fullscreen_window_id = id;
        // Hide taskbar and dock.
        var i: usize = 0;
        while (i < da.win_count) : (i += 1) {
            if (da.windows[i].kind == .taskbar or da.windows[i].kind == .dock) {
                da.windows[i].visible = false;
            }
        }
    }
    w.dirty = true;
    _ = mark_dirty(0);
    return true;
}

// ---------------------------------------------------------------------------
// M21 W8 — Always-on-top
// ---------------------------------------------------------------------------

/// M21 W8: toggle always-on-top on a user window. Always-on-top windows
/// render above all normal windows in the z-order.
pub fn toggle_always_on_top(id: u8) bool {
    const w = find_user_window(id) orelse return false;
    w.always_on_top = !w.always_on_top;
    w.dirty = true;
    _ = mark_dirty(0);
    return true;
}

// ---------------------------------------------------------------------------
// M21 W12 — Window title updates
// ---------------------------------------------------------------------------

/// M21 W12: set the dynamic title of a user window. Copies up to 63 bytes
/// (one byte reserved for NUL terminator in monitor output) into the
/// window's title buffer. The `title` slice is then pointed at the buffer
/// so taskbar, alt-tab, and monitor listings show the custom title.
/// Returns false for unknown window id.
pub fn set_window_title(id: u8, new_title: []const u8) bool {
    const w = find_user_window(id) orelse return false;
    const len = @min(new_title.len, 63);
    @memcpy(w.title_buf[0..len], new_title[0..len]);
    w.title_buf[len] = 0; // NUL-terminate for monitor safety
    w.title_len = @intCast(len);
    w.title = w.title_buf[0..len];
    w.dirty = true;
    _ = mark_dirty(0);
    return true;
}

// ---------------------------------------------------------------------------
// M21 W10 — Keyboard window movement
// ---------------------------------------------------------------------------

/// M21 W10: move the focused window by (dx, dy) pixels. Used for
/// Alt+arrow keyboard movement (16px normal, 1px with Shift).
pub fn move_window_keyboard(id: u8, dx: i32, dy: i32) bool {
    const w = find_user_window(id) orelse return false;
    const new_x: i32 = @as(i32, @intCast(w.x)) + dx;
    const new_y: i32 = @as(i32, @intCast(w.y)) + dy;
    const clamped_x: u32 = if (new_x < 0) 0 else @intCast(@min(@as(u32, @intCast(new_x)), virtio_gpu.fb_width - w.w));
    const clamped_y: u32 = if (new_y < 0) 0 else @intCast(@min(@as(u32, @intCast(new_y)), virtio_gpu.fb_height - w.h));
    w.x = clamped_x;
    w.y = clamped_y;
    w.dirty = true;
    _ = mark_dirty(0);
    return true;
}

/// M21 W10: resize the focused window by (dw, dh) pixels. Used for
/// Alt+Ctrl+arrow keyboard resizing (16px normal).
pub fn resize_window_keyboard(id: u8, dw: i32, dh: i32) bool {
    const w = find_user_window(id) orelse return false;
    const min_w: u32 = 128;
    const min_h: u32 = 64;
    const max_w: u32 = if (virtio_gpu.fb_width > w.x) virtio_gpu.fb_width - w.x else min_w;
    const max_h: u32 = if (virtio_gpu.fb_height > taskbar_h + w.y) virtio_gpu.fb_height - taskbar_h - w.y else min_h;
    const new_w: i32 = @as(i32, @intCast(w.w)) + dw;
    const new_h: i32 = @as(i32, @intCast(w.h)) + dh;
    const clamped_w: u32 = if (new_w < @as(i32, @intCast(min_w))) min_w else @min(@as(u32, @intCast(new_w)), max_w);
    const clamped_h: u32 = if (new_h < @as(i32, @intCast(min_h))) min_h else @min(@as(u32, @intCast(new_h)), max_h);
    // WM1: the pool buffer follows (content overlap preserved); on pool
    // exhaustion the window keeps its old size.
    if (!reflow(w, w.x, w.y, clamped_w, clamped_h)) return false;
    w.dirty = true;
    _ = mark_dirty(0);
    return true;
}

// ---------------------------------------------------------------------------
// M21 W15 — Modal windows
// ---------------------------------------------------------------------------

/// M21 W15: set a window as modal. Modal windows block input to windows
/// below them. Clicks outside the modal are ignored (except to dismiss).
pub fn set_modal(id: u8, modal: bool) bool {
    const w = find_user_window(id) orelse return false;
    w.modal = modal;
    if (modal) {
        // Raise the modal above all other windows.
        _ = raise(id);
        _ = focus(id);
    }
    return true;
}

/// M21 W15: check if a modal window is currently open.
pub fn modal_active() bool {
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].modal and da.windows[i].visible) return true;
    }
    return false;
}

/// M21 W15: get the topmost modal window id, or null.
pub fn topmost_modal_id() ?u8 {
    var i: usize = da.win_count;
    while (i > 0) {
        i -= 1;
        if (da.windows[i].modal and da.windows[i].visible) return da.windows[i].id;
    }
    return null;
}

// ---------------------------------------------------------------------------
// M21 W16 — Transient window behavior
// ---------------------------------------------------------------------------

/// M21 W16: set a window as transient with a timeout. Transient windows
/// are short-lived popups that auto-close when the timeout expires.
pub fn set_transient(id: u8, timeout: u32) bool {
    const w = find_user_window(id) orelse return false;
    w.transient = true;
    w.transient_timeout = timeout;
    return true;
}

/// M21 W16: advance transient timeouts. Called once per composite.
/// Returns the id of any window that expired (and was closed).
pub fn transient_advance_tick() ?u8 {
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].transient and da.windows[i].transient_timeout > 0) {
            da.windows[i].transient_timeout -|= 1;
            if (da.windows[i].transient_timeout == 0) {
                const id = da.windows[i].id;
                _ = user_close(id);
                return id;
            }
        }
    }
    return null;
}

/// M21 W16: dismiss all transient windows (e.g., on click-outside).
pub fn dismiss_transients() void {
    // Iterate backwards to handle removals safely.
    var i: usize = da.win_count;
    while (i > 0) {
        i -= 1;
        if (da.windows[i].transient) {
            _ = user_close(da.windows[i].id);
        }
    }
}

/// #3759 (slot-65 LAYOUT, cmd 17): the seat-teardown half of the channel —
/// called from `wm_server.release_wm_captures` so a dead seat cannot
/// strand a layout modality the resumed shim no longer decides. A seat-
/// entered fullscreen keeps the taskbar/dock hidden forever (the shim's
/// F11 chord is drained; only `dui fullscreen` could undo it), and a
/// seat-declared modal wedges the shim's modal click gate. Minimize /
/// maximize / always-on-top / transient stay: they are per-window state
/// with live shim consumers (taskbar_click restores a minimized window;
/// the composite tick still expires transients).
pub fn release_seat_layout() void {
    if (da.fullscreen_active) {
        var exited = false;
        if (da.fullscreen_window_id) |fid| {
            if (find_user_window(fid) != null) {
                _ = toggle_fullscreen(fid);
                exited = true;
            }
        }
        if (!exited) {
            // The fullscreen window is gone — drop the mode outright and
            // re-show the chrome it hid.
            da.fullscreen_active = false;
            da.fullscreen_window_id = null;
            var i: usize = 0;
            while (i < da.win_count) : (i += 1) {
                if (da.windows[i].kind == .taskbar or da.windows[i].kind == .dock) {
                    da.windows[i].visible = true;
                    da.windows[i].dirty = true;
                }
            }
        }
    }
    // A seat-declared modal would block every shim click outside it —
    // the resumed shim reads the flag but owns no clear path.
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        da.windows[i].modal = false;
    }
}
