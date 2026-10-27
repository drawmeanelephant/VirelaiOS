//! WM2 mission-control overview: the applied primitives behind slot-65
//! OVERVIEW (cmd 21), plus the M27 G3 dock-preview painter.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.
//!
//! #3759 (second item): the grid's RENDER + BOOKKEEPING drained to the seat.
//! The platform already makes the overview seat-mandatory — cmd 21 refuses
//! ENOSYS without a registered seat and no shim chord reaches enter — so the
//! kernel needed no fallback: the seat snapshots and hit-tests its own card
//! list (`overview_card_list`, the same membership/order rule the kernel's
//! snapshot used) and paints the grid into the scanout it already owns under
//! `wm_owns_user_layer` (the shared wnd_core geometry, same dim/cards/strip).
//! What remains here are the kernel FACTS the seat's decisions apply through:
//! focus+raise and move-to-workspace + workspace switch, gated on live-window
//! facts rather than a kernel copy of the card list (the seat decides which
//! card — membership is its bookkeeping, not a kernel safety gate).

const da = @import("../driving_award.zig");
const find_user_window = da.find_user_window;
const focus = da.focus;
const mark_dirty = da.mark_dirty;
const raise = da.raise;
const switch_workspace = da.switch_workspace;
const user_move_to_workspace = da.user_move_to_workspace;
const kbuf_ptr = da.kbuf_ptr;
const preview_h = da.preview_h;
const preview_w = da.preview_w;
const user_surface = da.user_surface;
const user_window_id_base = da.user_window_id_base;
const user_windows_max = da.user_windows_max;
const virtio_gpu = da.virtio_gpu;
const workspace_max = da.workspace_max;
const workspace_visible = da.workspace_visible;
const surface_covers = @import("user.zig").surface_covers;

/// The seat's enter decision: the kernel stores no snapshot — the seat owns
/// the card list — so the return is the LIVE eligible-card count (visible
/// user windows on the current workspace, capped at `user_windows_max`), the
/// same membership rule the old kernel snapshot filtered on. The seat prints
/// it in its `wnd: overview-enter n=N` marker. 0 is honest, not a refusal.
pub fn overview_enter() usize {
    var cnt: usize = 0;
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        const w = &da.windows[i];
        if (!w.visible) continue;
        if (w.kind != .user) continue;
        if (!workspace_visible(w)) continue;
        if (cnt < user_windows_max) cnt += 1;
    }
    return cnt;
}

/// The seat's click decision: window `id` gains focus+raise. Returns false
/// when `id` is not a live user window (the handler maps that to EINVAL) —
/// card membership is the seat's own bookkeeping, not a kernel gate.
pub fn overview_focus(id: u8) bool {
    if (find_user_window(id) == null) return false;
    _ = focus(id);
    _ = raise(id);
    _ = mark_dirty(0);
    return true;
}

/// The seat's drag decision: window `id` moves to workspace `ws` and the
/// desktop switches there. Returns false when `id` is not a live user
/// window or `ws` is out of range.
pub fn overview_move(id: u8, ws: u8) bool {
    if (ws >= workspace_max) return false;
    if (find_user_window(id) == null) return false;
    _ = user_move_to_workspace(id, ws);
    switch_workspace(ws);
    _ = mark_dirty(0);
    return true;
}

/// #990 (claim #997): WHY overview_move would refuse (id, ws) — the same
/// predicate the move runs, as a name for the serial. Pure.
pub fn overview_move_refusal(id: u8, ws: u8) []const u8 {
    if (ws >= workspace_max) return "ws-range";
    if (find_user_window(id) == null) return "no-window";
    return "none";
}

/// M27 G3: scale a window's framebuffer content into the preview buffer
/// using nearest-neighbor sampling. For user windows, reads the live
/// bytes — the shared surface when migrated (WM1 follows the SB3
/// surface-first convention; the old code read the frozen `user_bufs`
/// copy, which a migrated window never fills), else the pool back-buffer
/// at the window's own w×h (WM1: no more 512×424 full-buffer scale, so a
/// small window's thumbnail shows its content, not zero padding); for
/// the terminal, reads from the scanout framebuffer.
///
/// #3759: this stays kernel-side — it feeds the kernel's own dock-hover
/// overlay in chrome.zig and strides physical pages (surface pa_base /
/// pool kbuf) the seat cannot map.
pub fn render_preview(id: u8) void {
    @memset(&da.preview_buf, 0);
    // Find the window and its source buffer.
    var src: [*]const u8 = undefined;
    var src_w: u32 = 0;
    var src_h: u32 = 0;
    var src_stride: usize = 0;
    if (id >= user_window_id_base) {
        const win = find_user_window(id) orelse return;
        if (win.kbuf_pa == 0 and user_surface(id) == null) return;
        // #2111: read the surface only while the rect still fits its pages;
        // a desync falls back to the pool back-buffer (always exactly w×h).
        if (user_surface(id)) |sf| {
            src = if (surface_covers(sf.page_count, win.w, win.h))
                @as([*]const u8, @ptrFromInt(sf.pa_base))
            else
                kbuf_ptr(win);
        } else {
            src = kbuf_ptr(win);
        }
        src_w = win.w;
        src_h = win.h;
        src_stride = @as(usize, win.w) * 4;
    } else if (id == 0) {
        // Terminal — read from scanout framebuffer.
        src = @ptrCast(&virtio_gpu.gpu_fb);
        src_w = virtio_gpu.fb_width;
        src_h = virtio_gpu.fb_height;
        src_stride = virtio_gpu.fb_width * 4;
    } else {
        return; // clock/deprecated — no preview
    }
    // Nearest-neighbor scale to preview_w × preview_h.
    var py: u32 = 0;
    while (py < preview_h) : (py += 1) {
        const src_y = py * src_h / preview_h;
        var px: u32 = 0;
        while (px < preview_w) : (px += 1) {
            const src_x = px * src_w / preview_w;
            const src_off = src_y * src_stride + src_x * 4;
            const dst_off = (py * preview_w + px) * 4;
            da.preview_buf[dst_off] = src[src_off];
            da.preview_buf[dst_off + 1] = src[src_off + 1];
            da.preview_buf[dst_off + 2] = src[src_off + 2];
            da.preview_buf[dst_off + 3] = 0xff;
        }
    }
}
