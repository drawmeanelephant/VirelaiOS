//! Focus, z-order, hit-testing, workspaces, and the Alt-Tab switcher overlay.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const Window = da.Window;
const events = da.events;
const find_user_window = da.find_user_window;
const find_user_window_index = da.find_user_window_index;
const geom = da.geom;
const mark_dirty = da.mark_dirty;
const max_windows = da.max_windows;
const wm_mirror = da.wm_mirror;
const wm_rest_alpha = da.wm_rest_alpha;
const workspace_max = da.workspace_max;

/// Arc4 #241: switch to a workspace (0..2). Marks all windows dirty
/// so the compositor repaints the new visibility set.
pub fn switch_workspace(ws: u8) void {
    if (ws >= workspace_max) return;
    da.current_workspace = ws;
    // Mark all windows dirty so the compositor repaints.
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        da.windows[i].dirty = true;
    }
    _ = mark_dirty(0);
    _ = mark_dirty(1);
}

/// M21 W4: cycle to the next workspace (wrapping). Marks all windows
/// dirty so the compositor repaints the new visibility set.
pub fn cycle_workspace() void {
    const next = (da.current_workspace + 1) % workspace_max;
    switch_workspace(next);
}

/// Arc4 #241: check if a window is visible in the current workspace.
pub fn workspace_visible(w: *const Window) bool {
    // Delegated to the shared wnd_core rule (single source with the WM
    // server — the drift guard). Fixed layers are always visible.
    return geom.workspace_visible(to_geom(w), da.current_workspace);
}

// ---------------------------------------------------------------------------
// Z-order, focus, hit-testing (pure — host-testable)
// ---------------------------------------------------------------------------

/// The index of the focused window, or null when none.
pub fn focused_index() ?usize {
    if (!da.armed_global) return null;
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].id == da.focused_id) return i;
    }
    return null;
}

/// The focused window's id, or 0xff when none.
pub fn focused_window_id() u8 {
    return da.focused_id;
}

/// True when the focused window is the terminal (the I3 keyboard sink).
pub fn terminal_focused() bool {
    const fi = focused_index() orelse return false;
    return da.windows[fi].kind == .terminal;
}

/// The focused window, or null when none.
pub fn focused_window() ?*const Window {
    const fi = focused_index() orelse return null;
    return &da.windows[fi];
}

/// The owning process ID of the focused user window, or null if terminal/clock/none.
pub fn focused_owner() ?usize {
    const fi = focused_index() orelse return null;
    if (da.windows[fi].kind == .user) return da.windows[fi].owner;
    return null;
}

/// Focus a window by id. Returns false for an unknown id.
/// Card E4 (claim 0293): emits WIN_BLUR to previous focused user window
/// and WIN_FOCUS to newly focused user window.
pub fn focus(id: u8) bool {
    if (!da.armed_global) return false;
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].id == id) {
            const old_id = da.focused_id;
            // Card U5 (ADR 0008 D4): focus is ALWAYS VISIBLE — a focus
            // change must repaint so the ring moves.
            if (da.focused_id != id) {
                // Deliver WIN_BLUR to previously focused user window owner
                if (find_user_window(old_id)) |old_win| {
                    if (old_win.owner) |old_owner| {
                        events.push(old_owner, .{
                            .kind = events.WIN_BLUR,
                            .flags = 0,
                            .seq = 0,
                            .arg0 = old_id,
                            .arg1 = id,
                        });
                    }
                }
                // Deliver WIN_FOCUS to newly focused user window owner
                if (da.windows[i].kind == .user and da.windows[i].owner != null) {
                    events.push(da.windows[i].owner.?, .{
                        .kind = events.WIN_FOCUS,
                        .flags = 0,
                        .seq = 0,
                        .arg0 = id,
                        .arg1 = old_id,
                    });
                }
                // WM4 (issue #707 card 4): a focus change flips the
                // rest-alpha state of BOTH windows (the focused one is
                // exempt, the unfocused one blends). When either side
                // carries a rest policy, mark both dirty so the next
                // composite re-blits their CLIENTS at the new alpha (the
                // chrome repaints every composite anyway). No policy = 256
                // = no churn (the v1 behavior is untouched).
                if (wm_rest_alpha(old_id) < 256 or wm_rest_alpha(id) < 256) {
                    if (find_user_window(old_id)) |ow| ow.dirty = true;
                    if (find_user_window(id)) |nw| nw.dirty = true;
                }
                _ = mark_dirty(0);
            }
            // Step 7 (Issue #210): auto-show a hidden window on focus.
            if (!da.windows[i].visible) {
                da.windows[i].visible = true;
                da.windows[i].dirty = true;
                _ = mark_dirty(0);
            }
            da.focused_id = id;
            // M32 WMS5: mirror BOTH edges under the new holder — the old
            // window's cleared bit and the new window's set bit. Mirroring
            // the old id only after focused_id moved is what makes the
            // cleared bit honest; stamping it earlier reported the stale
            // pre-change bit (#3126 — a `dui focus` steal left every blur
            // mirror claiming focused=1).
            if (old_id != id) {
                if (find_user_window(old_id)) |old_win| wm_mirror(old_win.id);
            }
            wm_mirror(id);
            return true;
        }
    }
    return false;
}

/// Raise a window to the top of the z-order (the end of the array). Focus
/// is unchanged (tracked by id). Returns false for an unknown id.
pub fn raise(id: u8) bool {
    if (!da.armed_global) return false;
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].id == id) {
            const moved = da.windows[i];
            var j = i;
            while (j + 1 < da.win_count) : (j += 1) da.windows[j] = da.windows[j + 1];
            da.windows[da.win_count - 1] = moved;
            // Re-blit the raised window (z-order changed).
            da.windows[da.win_count - 1].dirty = true;
            return true;
        }
    }
    return false;
}

/// Adapter: the pure wnd_core geometry view of a kernel Window. The ONLY
/// rule logic lives in wnd_core (the drift guard); this is pure glue.
pub fn to_geom(w: *const Window) geom.Geom {
    return .{
        .id = w.id,
        .kind = @fromBackingInt(@intCast(@backingInt(w.kind))),
        .x = w.x,
        .y = w.y,
        .w = w.w,
        .h = w.h,
        .visible = w.visible,
        .workspace = w.workspace,
    };
}

/// The topmost visible window containing (x, y), or null when none does.
pub fn hit_test(x: u32, y: u32) ?u8 {
    // Delegated to the shared wnd_core hit-test rule (single source with
    // the WM server). The registry order IS the z-order (0 = bottom).
    var gbuf: [max_windows]geom.Geom = undefined;
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) gbuf[i] = to_geom(&da.windows[i]);
    return geom.hit_test(gbuf[0..da.win_count], da.current_workspace, x, y);
}

/// Focus the topmost window at (x, y). Returns false when nothing is there.
pub fn focus_at(x: u32, y: u32) bool {
    const id = hit_test(x, y) orelse return false;
    return focus(id);
}

/// Card U5 (claim 0935, ADR 0008 D4): cycle focus to the next visible
/// window in z-order (wrapping). Returns the newly focused id, or null
/// when no window is cyclable. A focus change repaints the whole scene
/// (mark_dirty(0) — the compositor repaints everything above the lowest
/// dirty window, so the ring moves correctly under any overlay).
pub fn cycle_focus() ?u8 {
    if (da.win_count == 0) return null;
    const start = focused_index() orelse 0;
    var i: usize = 1;
    while (i <= da.win_count) : (i += 1) {
        const idx = (start + i) % da.win_count;
        if (!da.windows[idx].visible) continue;
        // Wallpaper, taskbar and dock are not cyclable.
        if (da.windows[idx].kind == .wallpaper or da.windows[idx].kind == .taskbar or da.windows[idx].kind == .dock) continue;
        const target_id = da.windows[idx].id;
        _ = focus(target_id);
        return da.focused_id;
    }
    return null;
}

/// M15 C2 (Alt+Tab overlay, #225): hold-Alt cycling UI — BSS snapshot + highlight.
/// Pure overlay: Alt held + first Tab captures user-window ids, Tab/Shift+Tab cycles
/// `overlay_selected`, Alt release commits the highlight. No new syscall/kind.
pub fn alt_tab_is_active() bool {
    return da.overlay_active;
}

pub fn alt_tab_selected_id() ?u8 {
    if (!da.overlay_active or da.overlay_count == 0) return null;
    return da.overlay_ids[da.overlay_selected];
}

pub fn alt_tab_count() usize {
    return da.overlay_count;
}

/// Snapshot user windows and activate the overlay. Returns true when an overlay
/// with ≥2 windows is now visible (1 window → no overlay, honest no-op).
pub fn alt_tab_activate() bool {
    if (da.overlay_active) return false;
    var ids: [max_windows]u8 = undefined;
    var cnt: usize = 0;
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        const w = &da.windows[i];
        if (!w.visible) continue;
        if (w.kind != .user) continue;
        // M21 W3: skip minimized windows in Alt+Tab.
        if (w.minimized) continue;
        // M21 W4: only show windows on the current workspace.
        if (!workspace_visible(w)) continue;
        if (cnt < max_windows) {
            ids[cnt] = w.id;
            cnt += 1;
        }
    }
    if (cnt <= 1) return false;
    var sel: usize = 0;
    var found: ?usize = null;
    var k: usize = 0;
    while (k < cnt) : (k += 1) {
        if (ids[k] == da.focused_id) {
            found = k;
            break;
        }
    }
    if (found) |idx| sel = (idx + 1) % cnt else sel = 0;
    var j: usize = 0;
    while (j < cnt) : (j += 1) da.overlay_ids[j] = ids[j];
    da.overlay_count = cnt;
    da.overlay_selected = sel;
    da.overlay_active = true;
    _ = mark_dirty(0);
    return true;
}

/// Cycle the highlight — Shift inverts (reverse).
pub fn alt_tab_cycle(shift: bool) void {
    if (!da.overlay_active or da.overlay_count == 0) return;
    if (shift) {
        if (da.overlay_selected == 0) da.overlay_selected = da.overlay_count - 1 else da.overlay_selected -= 1;
    } else {
        da.overlay_selected = (da.overlay_selected + 1) % da.overlay_count;
    }
    _ = mark_dirty(0);
}

/// Commit the highlighted window (focus+raise) and dismiss the overlay.
pub fn alt_tab_commit() ?u8 {
    if (!da.overlay_active or da.overlay_count == 0) return null;
    const id = da.overlay_ids[da.overlay_selected];
    da.overlay_active = false;
    da.overlay_count = 0;
    _ = focus(id);
    _ = raise(id);
    _ = mark_dirty(0);
    return id;
}

/// Dismiss without committing (right-click / Escape).
pub fn alt_tab_dismiss() void {
    if (!da.overlay_active) return;
    da.overlay_active = false;
    da.overlay_count = 0;
    _ = mark_dirty(0);
}

/// M32 WMS6 Gate A (issue #626): the WM, not the kernel, picks which window
/// Alt+Tab switches to. The kernel keeps the overlay SNAPSHOT + blit (WMS8
/// deletes it) but exposes focus-by-id so the WM's decision drives it. The
/// WM proposes the highlighted id; the kernel clamps (id must name a live
/// alt-tab window: visible, non-minimized, on the current workspace — the
/// M21 W3/W4 rules) and repaints. Returns false when `id` is not a valid
/// alt-tab target (the handler maps that to EINVAL).
pub fn alt_tab_overlay_focus(id: u8) bool {
    // Build the snapshot if the overlay isn't active (the same rules
    // alt_tab_activate uses — visible, non-minimized, current workspace).
    if (!da.overlay_active) {
        var ids: [max_windows]u8 = undefined;
        var cnt: usize = 0;
        var i: usize = 0;
        while (i < da.win_count) : (i += 1) {
            const w = &da.windows[i];
            if (!w.visible) continue;
            if (w.kind != .user) continue;
            if (w.minimized) continue;
            if (!workspace_visible(w)) continue;
            if (cnt < max_windows) {
                ids[cnt] = w.id;
                cnt += 1;
            }
        }
        if (cnt <= 1) return false; // not enough windows to Alt+Tab
        var j: usize = 0;
        while (j < cnt) : (j += 1) da.overlay_ids[j] = ids[j];
        da.overlay_count = cnt;
        da.overlay_active = true;
    }
    // Find the WM's proposed id in the snapshot and make it the highlight.
    var k: usize = 0;
    while (k < da.overlay_count) : (k += 1) {
        if (da.overlay_ids[k] == id) {
            da.overlay_selected = k;
            _ = mark_dirty(0);
            return true;
        }
    }
    return false; // id not a live alt-tab window
}

/// WMS6 Gate A: commit the WM's chosen id directly (focus + raise + dismiss),
/// the same final action `alt_tab_commit` performs (by value, not by snapshot
/// index). Returns false when `id` does not name a live user window (the
/// handler maps that to EINVAL).
pub fn alt_tab_wm_commit(id: u8) bool {
    if (find_user_window_index(id) == null) return false;
    da.overlay_active = false;
    da.overlay_count = 0;
    _ = focus(id);
    _ = raise(id);
    _ = mark_dirty(0);
    return true;
}
