//! WM2 mission-control overview: the hotkey-summoned window grid and the dock preview buffer.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const draw_string = da.draw_string;
const fill_rect = da.fill_rect;
const find_user_window = da.find_user_window;
const focus = da.focus;
const focus_ring = da.focus_ring;
const geom = da.geom;
const kbuf_ptr = da.kbuf_ptr;
const mark_dirty = da.mark_dirty;
const preview_h = da.preview_h;
const preview_w = da.preview_w;
const raise = da.raise;
const switch_workspace = da.switch_workspace;
const taskbar_entry_active = da.taskbar_entry_active;
const user_move_to_workspace = da.user_move_to_workspace;
const user_surface = da.user_surface;
const user_window_id_base = da.user_window_id_base;
const user_windows_max = da.user_windows_max;
const virtio_gpu = da.virtio_gpu;
const workspace_max = da.workspace_max;
const workspace_visible = da.workspace_visible;
const surface_covers = @import("user.zig").surface_covers;

pub fn overview_is_open() bool {
    return da.overview_open;
}

/// Live card count (0 when closed).
pub fn overview_window_count() usize {
    if (!da.overview_open) return 0;
    return da.overview_n;
}

/// Snapshot the current workspace's user windows (visible ones on the
/// current workspace) and open the grid. The snapshot sorts ids ASCENDING
/// so card index i names the same window on both sides of the seam: the WM
/// rebuilds the identical order from its mirrors (slot order == id order)
/// using only mirrored fields (valid/visible/workspace), so the card-index
/// mapping cannot drift. Returns the card count (0 with the grid still open
/// but empty — honest, not a refusal).
pub fn overview_enter() usize {
    var cnt: usize = 0;
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        const w = &da.windows[i];
        if (!w.visible) continue;
        if (w.kind != .user) continue;
        if (!workspace_visible(w)) continue;
        if (cnt < user_windows_max) {
            da.overview_ids[cnt] = w.id;
            cnt += 1;
        }
    }
    // Ascending-id insertion sort (<= 8 entries — the shared card order).
    var a: usize = 1;
    while (a < cnt) : (a += 1) {
        const key = da.overview_ids[a];
        var b: usize = a;
        while (b > 0 and da.overview_ids[b - 1] > key) : (b -= 1) {
            da.overview_ids[b] = da.overview_ids[b - 1];
        }
        da.overview_ids[b] = key;
    }
    da.overview_n = cnt;
    da.overview_open = true;
    _ = mark_dirty(0);
    return cnt;
}

/// Close the grid without touching focus.
pub fn overview_exit() void {
    if (!da.overview_open) return;
    da.overview_open = false;
    da.overview_n = 0;
    _ = mark_dirty(0);
}

/// The WM's click decision: card `id` gains focus+raise and the grid
/// closes. Returns false when closed or `id` is not a live card (the
/// handler maps that to EINVAL).
pub fn overview_focus(id: u8) bool {
    if (!da.overview_open) return false;
    var k: usize = 0;
    while (k < da.overview_n) : (k += 1) {
        if (da.overview_ids[k] == id) {
            da.overview_open = false;
            da.overview_n = 0;
            _ = focus(id);
            _ = raise(id);
            _ = mark_dirty(0);
            return true;
        }
    }
    return false;
}

/// The WM's drag decision: card `id` moves to workspace `ws` and the grid
/// closes onto that workspace. Returns false when closed, `id` is not a
/// live card, or `ws` is out of range.
pub fn overview_move(id: u8, ws: u8) bool {
    if (!da.overview_open) return false;
    if (ws >= workspace_max) return false;
    var k: usize = 0;
    while (k < da.overview_n) : (k += 1) {
        if (da.overview_ids[k] == id) {
            da.overview_open = false;
            da.overview_n = 0;
            _ = user_move_to_workspace(id, ws);
            switch_workspace(ws);
            _ = mark_dirty(0);
            return true;
        }
    }
    return false;
}

/// #990 (claim #997): WHY overview_move would refuse (id, ws) — the same
/// predicate the move runs, as a name for the serial. Pure.
pub fn overview_move_refusal(id: u8, ws: u8) []const u8 {
    if (!da.overview_open) return "closed";
    if (ws >= workspace_max) return "ws-range";
    var k: usize = 0;
    while (k < da.overview_n) : (k += 1) {
        if (da.overview_ids[k] == id) return "none";
    }
    return "not-a-card";
}

/// WM2: nearest-neighbor scale blit — a source image (a user window's own
/// back-buffer or shared surface, never the scanout, so source and
/// destination cannot overlap) scaled into a destination rect. Pure BSS
/// math, no allocation. Zero-size rects are honest no-ops.
pub fn blit_scaled(
    dst: [*]u8,
    dst_stride: usize,
    src: [*]const u8,
    src_w: usize,
    src_h: usize,
    src_stride: usize,
    dx: usize,
    dy: usize,
    dw: usize,
    dh: usize,
) void {
    if (dw == 0 or dh == 0 or src_w == 0 or src_h == 0) return;
    var y: usize = 0;
    while (y < dh) : (y += 1) {
        const src_y = y * src_h / dh;
        var x: usize = 0;
        while (x < dw) : (x += 1) {
            const src_x = x * src_w / dw;
            const s = src + src_y * src_stride + src_x * 4;
            const d = dst + (dy + y) * dst_stride + (dx + x) * 4;
            d[0] = s[0];
            d[1] = s[1];
            d[2] = s[2];
            d[3] = 0xff;
        }
    }
}

/// WM2: paint the overview grid (called from the overlay section when
/// `overview_open`). Dim backdrop + one card per snapshot window (title
/// band + live scaled content) + the workspace strip with one button per
/// workspace. Card/strip rects are the shared geom rules — the same ones
/// the WM hit-tests.
pub fn paint_overview(fb: [*]u8, stride: usize, wspan: usize, hspan: usize) void {
    if (!da.overview_open) return;
    const sw: u32 = @intCast(@min(wspan, 0xffff));
    const sh: u32 = @intCast(@min(hspan, 0xffff));
    // Dim backdrop over the whole scanout.
    fill_rect(fb, stride, 0, 0, wspan, hspan, 0x0f0f1a);
    const g = geom.overview_grid(da.overview_n, sw, sh, geom.taskbar_h, geom.dock_w);
    draw_string(fb, stride, @as(usize, g.x0), 0, "Overview - click focuses, drag to a workspace, Esc exits", 0x94a3b8);
    var idx: usize = 0;
    while (idx < da.overview_n) : (idx += 1) {
        const id = da.overview_ids[idx];
        const card = geom.overview_card_rect(g, idx);
        const cx: usize = card.x;
        const cy: usize = card.y;
        const cw: usize = card.w;
        const ch: usize = card.h;
        // Card surface + border (accent when focused).
        fill_rect(fb, stride, cx, cy, cw, ch, 0x1e293b);
        const border = if (id == da.focused_id) focus_ring() else 0x64748b;
        fill_rect(fb, stride, cx, cy, cw, 2, border);
        fill_rect(fb, stride, cx, cy + ch - 2, cw, 2, border);
        fill_rect(fb, stride, cx, cy, 2, ch, border);
        fill_rect(fb, stride, cx + cw - 2, cy, 2, ch, border);
        // Title band: first bytes of the window title.
        if (find_user_window(id)) |win| {
            const max_chars = if (cw > 8) (cw - 8) / 8 else 0;
            const n = @min(win.title.len, max_chars);
            if (n > 0) draw_string(fb, stride, cx + 4, cy + 4, win.title[0..n], 0xffffff);
            // Live content scaled into the card below the title band.
            const thumb_y = cy + @as(usize, geom.title_bar_h);
            const thumb_h = ch -| (@as(usize, geom.title_bar_h) + 2);
            const thumb_w = cw -| 4;
            if (thumb_w > 0 and thumb_h > 0) {
                var src: ?[*]const u8 = null;
                var src_w: usize = 0;
                var src_h: usize = 0;
                var src_stride: usize = 0;
                if (user_surface(id)) |sf| {
                    // #2111: read the surface only while the rect fits it.
                    if (surface_covers(sf.page_count, win.w, win.h)) {
                        src = @as([*]const u8, @ptrFromInt(sf.pa_base));
                        src_w = win.w;
                        src_h = win.h;
                        src_stride = @as(usize, win.w) * 4;
                    }
                }
                if (src == null and win.kbuf_pa != 0) {
                    src = kbuf_ptr(win);
                    src_w = win.w;
                    src_h = win.h;
                    src_stride = @as(usize, win.w) * 4;
                }
                if (src) |s| blit_scaled(fb, stride, s, src_w, src_h, src_stride, cx + 2, thumb_y, thumb_w, thumb_h);
            }
        }
    }
    // Workspace strip: one button per workspace, current highlighted.
    var ws: u8 = 0;
    while (ws < geom.overview_ws_count) : (ws += 1) {
        const b = geom.overview_ws_button_rect(ws, sw, geom.dock_w, g.strip_y);
        const bg = if (ws == da.current_workspace) taskbar_entry_active() else 0x0f172a;
        fill_rect(fb, stride, @as(usize, b.x), @as(usize, b.y), @as(usize, b.w), @as(usize, b.h), bg);
        var label: [4]u8 = .{ 'W', 'S', ' ', '0' + ws };
        draw_string(fb, stride, @as(usize, b.x) + 6, @as(usize, b.y) + 10, &label, 0xffffff);
    }
}

/// M27 G3: scale a window's framebuffer content into the preview buffer
/// using nearest-neighbor sampling. For user windows, reads the live
/// bytes — the shared surface when migrated (WM1 follows the SB3
/// surface-first convention; the old code read the frozen `user_bufs`
/// copy, which a migrated window never fills), else the pool back-buffer
/// at the window's own w×h (WM1: no more 512×424 full-buffer scale, so a
/// small window's thumbnail shows its content, not zero padding); for
/// the terminal, reads from the scanout framebuffer.
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
