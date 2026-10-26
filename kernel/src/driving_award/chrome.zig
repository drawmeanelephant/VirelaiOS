//! Window chrome: theme colors, the SET_WINDOW chrome-descriptor seam, tab-group facts, and draw_chrome.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const Window = da.Window;
const arm = da.arm;
const blit_rect = da.blit_rect;
const clock_accent_rgb = da.clock_accent_rgb;
const draw_glyph = da.draw_glyph;
const draw_glyph_16 = da.draw_glyph_16;
const draw_string = da.draw_string;
const draw_string_16 = da.draw_string_16;
const fade_half_frames = da.fade_half_frames;
const fill_rect = da.fill_rect;
const find_user_window = da.find_user_window;
const fmt_decimal = da.fmt_decimal;
const focus = da.focus;
const focus_ring_w = da.focus_ring_w;
const geom = da.geom;
const kbuf_ptr = da.kbuf_ptr;
const max_windows = da.max_windows;
const notif_center_w = da.notif_center_w;
const notify_entry = da.notify_entry;
const paint_overview = da.paint_overview;
const paint_scene = da.paint_scene;
const render_preview = da.render_preview;
const settings = da.settings;
const std = da.std;
const user_bind_surface = da.user_bind_surface;
const user_close = da.user_close;
const user_open = da.user_open;
const user_title_fg_rgb = da.user_title_fg_rgb;
const user_title_h = da.user_title_h;
const virtio_gpu = da.virtio_gpu;
const wm_apply_rect = da.wm_apply_rect;
const workspace_visible = da.workspace_visible;

/// Theme color for the clock title bar background.
pub fn clock_title_bg() u32 {
    return switch (da.theme_id) {
        1 => 0xf8fafc, // light: near-white title bar
        2 => 0xb58900, // amber: warm gold
        else => 0xb58900, // dark: original amber
    };
}

/// Theme color for the clock body background.
pub fn clock_bg() u32 {
    return switch (da.theme_id) {
        1 => 0xf1f5f9, // light: light gray
        2 => 0x0a1a2e, // amber: dark navy (original)
        else => 0x0a1a2e, // dark: original
    };
}

/// M20-U9: window border width — 2px, visible on every theme.
pub const chrome_border_w: usize = 2;

/// Theme color for the user window border (darker than the title bar).
pub fn user_border() u32 {
    return switch (da.theme_id) {
        1 => 0x94a3b8, // light: slate
        2 => 0x0c1826, // amber: near-black navy (original)
        else => 0x0c1826, // dark: original
    };
}

/// M21 W9: muted border color for unfocused windows (visible but not
/// attention-grabbing). The focused window gets an accent border instead.
pub fn user_border_unfocused() u32 {
    return switch (da.theme_id) {
        1 => 0x9ca3af, // light: muted slate
        2 => 0x44403c, // amber: warm stone
        else => 0x475569, // dark: muted blue-gray
    };
}

/// M37 DQ4 (issue #838): compositor drop-shadow offset (px) — right +
/// bottom bands outside the window rect. Mirrors `user/src/lib/ui.zig`
/// (`shadow_off`); pinned equal by the dq4 shadow-parity host test.
/// Gated by `settings set shadow on` (default off) so every pre-DQ4
/// pixel gate stays byte-identical.
pub const chrome_shadow_off: usize = 4;

/// M37 DQ4: theme color for the drop-shadow bands.
pub fn shadow_color() u32 {
    return switch (da.theme_id) {
        1 => 0x94a3b8, // light: slate (visible on light wallpaper)
        2 => 0x000000, // amber: black
        else => 0x000000, // dark: black
    };
}
/// M20-U9 layout helper: where the centered title text starts and how
/// many bytes of it to draw, given a window width and label length.
/// Leaves room for the minimize+close buttons on the right; labels too
/// wide for the remaining span truncate with a trailing "...".
pub fn chrome_title_layout(win_w: usize, label_len: usize) geom.TitleLayout {
    // Single source: the shared wnd_core layout rule.
    return geom.chrome_title_layout(win_w, label_len);
}

/// Theme color for the user window title bar.
pub fn user_title_bg() u32 {
    return switch (da.theme_id) {
        1 => 0xe2e8f0, // light: light surface
        2 => 0x1a2b3c, // amber: dark blue (original)
        else => 0x1a2b3c, // dark: original
    };
}

/// M21 W9: theme color for the focus ring — accent per theme.
/// Dark: blue, Light: deeper blue, Amber: amber.
pub fn focus_ring() u32 {
    return switch (da.theme_id) {
        1 => 0x1d4ed8, // light: blue ring (distinct on white)
        2 => 0xf59e0b, // amber: warm amber
        3 => da.custom_accent, // custom: the user's accent (#1662)
        else => 0x3b82f6, // dark: blue accent
    };
}

/// Theme color for the taskbar background.
pub fn taskbar_bg() u32 {
    return switch (da.theme_id) {
        1 => 0xe2e8f0, // light: light surface
        2 => 0x0f172a, // amber: dark (original)
        else => 0x0f172a, // dark: original
    };
}

/// Theme color for the taskbar entry highlight.
pub fn taskbar_entry_active() u32 {
    return switch (da.theme_id) {
        1 => 0x2563eb, // light: blue
        2 => 0xff8800, // amber: orange
        3 => da.custom_accent, // custom: the user's accent (#1662)
        else => 0x3b82f6, // dark: blue (original)
    };
}

/// Theme color for the taskbar entry (dimmed).
pub fn taskbar_entry_dimmed() u32 {
    return switch (da.theme_id) {
        1 => 0xcbd5e1, // light: light border
        2 => 0x1e293b, // amber: dark (original)
        else => 0x1e293b, // dark: original
    };
}

/// WM3 (issue #707 card 3): theme color for a MINIMIZED entry (darker than
/// the plain dimmed fill — the entry is a restore target, not a task).
pub fn taskbar_entry_minimized() u32 {
    return switch (da.theme_id) {
        1 => 0xb0bccd, // light: dimmer border
        2 => 0x0b1220, // amber: darker than dimmed
        else => 0x0b1220, // dark: darker than dimmed
    };
}

/// WM3: the focused entry's active-indicator underline ink (white —
/// visible on the active fill in every theme).
pub const taskbar_indicator_rgb: u32 = 0xffffff;

/// WM3: the minimized entry's indicator dot ink (the restore affordance).
pub const taskbar_min_dot_rgb: u32 = 0x94a3b8;

/// WM3: the minimized entry's title ink (muted gray).
pub const taskbar_min_title_rgb: u32 = 0x94a3b8;

/// Theme color for the desktop wallpaper top.
pub fn wallpaper_top() u32 {
    return switch (da.theme_id) {
        1 => 0xf1f5f9, // light: very light
        2 => 0x1a1a2e, // amber: dark (original)
        else => 0x1a1a2e, // dark: original
    };
}

/// Theme color for the desktop wallpaper bottom.
pub fn wallpaper_bot() u32 {
    return switch (da.theme_id) {
        1 => 0xe2e8f0, // light: light surface
        2 => 0x0a0a14, // amber: darker (original)
        else => 0x0a0a14, // dark: original
    };
}

// ---------------------------------------------------------------------------
// M32 WMS4 (issue #624): the SET_WINDOW chrome-descriptor seam. The WM
// server (userland) is the chrome-policy owner; the kernel stores the
// descriptors it submits and blits whatever they dictate. No WM chrome is
// present in the default VM — the shim's own rules (the "default
// descriptor" below) render byte-identically, so every pre-WMS4 gate is
// untouched.
// ---------------------------------------------------------------------------

/// Store a chrome descriptor for `id`, or the broadcast policy when `id`
/// is `geom.chrome_window_all`. Returns false for an unknown per-window
/// id or invalid descriptor (including a zero-kind broadcast). The syscall
/// validates the wire before this store applies the same pure rule.
/// Drop every WM chrome decision (the broadcast policy + per-window
/// overrides). Called on WM teardown (unregister) so the shim fallback
/// restores its own chrome rules — a dead WM must not leave its look
/// painted on the desktop.
pub fn clear_wm_chrome() void {
    da.wm_chrome_policy = null;
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        da.windows[i].chrome_valid = false;
    }
    // M32 WMS6 Gate E (issue #626): the WM's tray widget content dies with
    // it — the shim fallback re-derives clock/theme/clipboard from its own
    // state (a dead WM's declared values must not stay painted).
    da.wm_tray_clock_set = false;
    da.wm_tray_theme_set = false;
    da.wm_tray_clip_set = false;
}

pub fn set_window_chrome(id_in: u64, desc: geom.ChromeDesc) bool {
    if (!geom.chrome_valid_for_window(id_in, desc)) return false;
    if (id_in == geom.chrome_window_all) {
        // WM4 (issue #707 card 4): a policy change can flip the effective
        // rest alpha of windows already painted under the old policy —
        // snapshot the old effective alphas, swap the policy, and mark
        // every user window whose alpha changed dirty so the next
        // composite re-blits its client (chrome repaints anyway).
        var before: [max_windows]u32 = undefined;
        var i: usize = 0;
        while (i < da.win_count) : (i += 1) before[i] = wm_rest_alpha(da.windows[i].id);
        da.wm_chrome_policy = desc;
        i = 0;
        while (i < da.win_count) : (i += 1) {
            if (da.windows[i].kind != .user) continue;
            if (before[i] != wm_rest_alpha(da.windows[i].id)) da.windows[i].dirty = true;
        }
        return true;
    }
    if (id_in > 0xff) return false;
    const id: u8 = @intCast(id_in);
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].id != id) continue;
        da.windows[i].chrome = desc;
        da.windows[i].chrome_valid = true;
        return true;
    }
    return false;
}

/// The broadcast policy's chrome kind (0 when no WM chrome is set) — the
/// `wm` observability row.
pub fn wm_chrome_policy_kind() u32 {
    return if (da.wm_chrome_policy) |p| p.kind else 0;
}

/// The last chrome kind stored for window `id` (its override, else the
/// policy, else 0 = shim rules / unknown window) — the "last chrome kind
/// per window" observability.
pub fn wm_chrome_kind(id: u8) u32 {
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].id != id) continue;
        if (da.windows[i].chrome_valid) return da.windows[i].chrome.kind;
        return if (da.wm_chrome_policy) |p| p.kind else 0;
    }
    return 0; // no such window
}

/// One observability row for the `wm` report: a user window's id, its
/// effective last chrome kind, and its effective WM4 rest alpha.
pub const ChromeRow = struct { id: u8, kind: u32, rest_alpha: u32 };

// ---------------------------------------------------------------------------
// M37 DQ2 (issue #840) — tab-group facts for the strip paint. The WM
// decides grouping via validated ATTACH_TAB/DETACH_TAB; the kernel mirrors
// the facts here (parent id per window) so draw_chrome can paint the
// strip. Unknown ids are no-ops (the syscall layer validated already).
// ---------------------------------------------------------------------------

/// Record a validated attach (child → parent). Pure registry write.
pub fn note_tab_attach(child_id: u8, parent_id: u8) void {
    if (find_user_window(child_id)) |cm| cm.tab_parent = parent_id;
}

/// Record a validated detach (child → standalone). Pure registry write.
pub fn note_tab_detach(child_id: u8) void {
    if (find_user_window(child_id)) |cm| cm.tab_parent = 0;
}

/// The recorded parent of a window (0 = standalone / unknown). Pure.
pub fn tab_parent_of(id: u8) u8 {
    if (find_user_window(id)) |w| return w.tab_parent;
    return 0;
}

/// Count windows attached to `parent_id`. Pure over the registry.
pub fn tab_group_count(parent_id: u8) usize {
    var n: usize = 0;
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].kind != .user) continue;
        if (da.windows[i].tab_parent == parent_id) n += 1;
    }
    return n;
}

/// Fill `out` with the ids attached to `parent_id`, in registry order.
/// Returns the count written. Pure over the registry.
pub fn tab_group_members(parent_id: u8, out: []u8) usize {
    var n: usize = 0;
    var i: usize = 0;
    while (i < da.win_count and n < out.len) : (i += 1) {
        if (da.windows[i].kind != .user) continue;
        if (da.windows[i].tab_parent != parent_id) continue;
        out[n] = da.windows[i].id;
        n += 1;
    }
    return n;
}

/// Fill `rows` with one ChromeRow per user window in registry order.
/// Returns the count written. Pure over the registry.
pub fn wm_chrome_rows(rows: *[max_windows]ChromeRow) usize {
    var n: usize = 0;
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        const w = &da.windows[i];
        if (w.kind != .user) continue;
        if (n >= rows.len) break;
        rows[n] = .{ .id = w.id, .kind = wm_chrome_kind(w.id), .rest_alpha = wm_rest_alpha(w.id) };
        n += 1;
    }
    return n;
}

/// WM4 (issue #707 card 4): the effective REST alpha for window `id` —
/// its chrome override's rest_alpha, else the broadcast policy's, else
/// 256 (opaque; no WM / no policy = byte-identical v1 behavior).
/// Normalized through wnd_core (0 = none → 256). Pure over the registry.
pub fn wm_rest_alpha(id: u8) u32 {
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].id != id) continue;
        if (da.windows[i].chrome_valid) return geom.chrome_rest_alpha_normalized(da.windows[i].chrome);
        if (da.wm_chrome_policy) |p| return geom.chrome_rest_alpha_normalized(p);
        return 256;
    }
    return 256; // no such window
}

// ---------------------------------------------------------------------------
// M37 DQ2 (issue #840) — tab-strip paint. Called from draw_chrome for a
// container window with attached tabs when its chrome enables
// chrome_tab_bar. Trough + cells from the shared tab_item_rect geometry
// (the SAME rule DQ3 hit-tests, so pixels and clicks cannot drift).
// Titles from window title_buf with the title-bar "dui{id}" fallback;
// active = focused member (fallback: visible member); hover = cursor
// geometry (no policy). All fills clamped to the scanout (put_px has no
// bounds check); text/glyphs guarded per-cell.
// ---------------------------------------------------------------------------

/// Scanout-clamped fill (put_px writes blind — every strip fill routes here).
pub fn strip_fill(fb: [*]u8, stride: usize, x: u32, y: u32, w: u32, h: u32, rgb: u32, wspan: usize, hspan: usize) void {
    if (w == 0 or h == 0) return;
    if (x >= wspan or y >= hspan) return;
    const cw: usize = @min(@as(usize, w), wspan - x);
    const chh: usize = @min(@as(usize, h), hspan - y);
    fill_rect(fb, stride, x, y, cw, chh, rgb);
}

/// Guarded 8x16 text draw (drops glyphs that would leave the scanout).
pub fn strip_text(fb: [*]u8, stride: usize, x: u32, y: u32, s: []const u8, rgb: u32, wspan: usize, hspan: usize) void {
    if (y + 16 > hspan) return;
    for (s, 0..) |c, i| {
        const gx = x + @as(u32, @intCast(i)) * 8;
        if (gx + 8 > wspan) break;
        draw_glyph_16(fb, stride, gx, y, c, rgb);
    }
}

/// Guarded 8x8 glyph draw.
pub fn strip_glyph(fb: [*]u8, stride: usize, x: u32, y: u32, c: u8, rgb: u32, wspan: usize, hspan: usize) void {
    if (x + 8 > wspan or y + 8 > hspan) return;
    draw_glyph(fb, stride, x, y, c, rgb);
}

pub fn paint_tab_strip(fb: [*]u8, stride: usize, w: *const Window, ch: *const geom.ChromeDesc, wspan: usize, hspan: usize) void {
    // Collect the group: container first, then attached children in order.
    var members: [max_windows]u8 = undefined;
    var mcount: usize = 0;
    members[0] = w.id;
    mcount = 1;
    var mbuf: [max_windows]u8 = undefined;
    const cn = tab_group_members(w.id, &mbuf);
    var ci: usize = 0;
    while (ci < cn and mcount < members.len) : (ci += 1) {
        members[mcount] = mbuf[ci];
        mcount += 1;
    }
    if (mcount < 2) return; // no attached tabs — nothing to paint

    const strip = geom.tab_strip_rect(w.x, w.y, w.w);
    strip_fill(fb, stride, strip.x, strip.y, strip.w, strip.h, ch.border_unfocus_rgb, wspan, hspan);

    // Active member: the focused one; fallback: the first visible one.
    var active_id: u8 = 0xff;
    var mi: usize = 0;
    while (mi < mcount) : (mi += 1) {
        if (members[mi] == da.focused_id) {
            active_id = members[mi];
            break;
        }
    }
    if (active_id == 0xff) {
        mi = 0;
        while (mi < mcount) : (mi += 1) {
            if (find_user_window(members[mi])) |mw| {
                if (mw.visible) {
                    active_id = members[mi];
                    break;
                }
            }
        }
    }

    mi = 0;
    while (mi < mcount) : (mi += 1) {
        const id = members[mi];
        const cell = geom.tab_item_rect(strip.x, strip.y, strip.w, mi, mcount);
        if (cell.w == 0) continue;
        const is_active = id == active_id;
        // Cell interior leaves a 1px trough divider on its left (the
        // shared rect still tiles fully — DQ3 hit-tests the full cell).
        if (cell.w > 1) strip_fill(fb, stride, cell.x + 1, cell.y, cell.w - 1, cell.h, ch.title_bg_rgb, wspan, hspan);
        if (is_active) {
            // Active underline: 2px accent at the cell bottom.
            if (cell.h >= 2) strip_fill(fb, stride, cell.x, cell.y + cell.h - 2, cell.w, 2, ch.ring_rgb, wspan, hspan);
        } else if (da.cursor_shown and geom.tab_rect_contains(cell, da.cursor_x, da.cursor_y)) {
            // Hover outline: 1px border on the cell edges.
            strip_fill(fb, stride, cell.x, cell.y, cell.w, 1, ch.border_rgb, wspan, hspan);
            if (cell.h >= 1) strip_fill(fb, stride, cell.x, cell.y + cell.h - 1, cell.w, 1, ch.border_rgb, wspan, hspan);
            strip_fill(fb, stride, cell.x, cell.y, 1, cell.h, ch.border_rgb, wspan, hspan);
            if (cell.w >= 1) strip_fill(fb, stride, cell.x + cell.w - 1, cell.y, 1, cell.h, ch.border_rgb, wspan, hspan);
        }
        // Title: window title_buf, else the title-bar "dui{id}" fallback.
        var tb: [24]u8 = undefined;
        var n: usize = 0;
        if (find_user_window(id)) |mw| {
            if (mw.title_len > 0) {
                n = @min(@as(usize, mw.title_len), tb.len);
                @memcpy(tb[0..n], mw.title_buf[0..n]);
            }
        }
        if (n == 0) {
            @memcpy(tb[0..3], "dui");
            n = 3;
            const idstr = fmt_decimal(tb[n..], id);
            n += idstr.len;
        }
        const lay = geom.tab_title_layout(@as(usize, cell.w), n);
        const tx = cell.x + @as(u32, @intCast(lay.x_off));
        const ty = cell.y + 3;
        if (!lay.truncated) {
            strip_text(fb, stride, tx, ty, tb[0..lay.draw_len], ch.title_fg_rgb, wspan, hspan);
        } else {
            var tt: [24]u8 = undefined;
            @memcpy(tt[0..lay.draw_len], tb[0..lay.draw_len]);
            @memcpy(tt[lay.draw_len..][0..3], "...");
            strip_text(fb, stride, tx, ty, tt[0 .. lay.draw_len + 3], ch.title_fg_rgb, wspan, hspan);
        }
        // Per-tab close glyph (geometry only — DQ3 wires the click).
        const cb = geom.tab_close_rect(cell);
        strip_glyph(fb, stride, cb.x + 2, cb.y + 2, 'x', ch.close_rgb, wspan, hspan);
    }
}

/// The chrome decision for a window: its per-window override, else the
/// WM's broadcast policy, else the shim's own rules expressed as a
/// descriptor (the dark-theme defaults — byte-identical to the pre-WMS4
/// paint). The shim is thus just a default descriptor; parity is by
/// construction.
pub fn effective_chrome(w: *const Window) geom.ChromeDesc {
    if (w.chrome_valid) return w.chrome;
    if (da.wm_chrome_policy) |p| return p;
    return .{
        .kind = geom.chrome_kind_all,
        .flags = geom.chrome_flag_focus_accent,
        .border_rgb = user_border(),
        .border_unfocus_rgb = user_border_unfocused(),
        .title_bg_rgb = user_title_bg(),
        .title_fg_rgb = user_title_fg_rgb,
        .ring_rgb = focus_ring(),
        .close_rgb = 0xef4444,
        .min_rgb = 0x94a3b8,
        .pin_rgb = 0x38bdf8,
    };
}

/// Arc4 #239 / WM4 (issue #707 card 4): the blend alpha a window's CLIENT
/// area is painted at this frame (256 = fully opaque). Shared by `paint`
/// (the blit) and `chrome_occluded` — only an OPAQUE cover can hide a
/// window's chrome. Pure.
pub fn client_alpha(w: *const Window) u16 {
    if (w.fade_phase == 1) {
        if (w.fade_tick < fade_half_frames) return 64; // 25%
        if (w.fade_tick < fade_half_frames * 2) return 128; // 50%
        return 256; // fully opaque (fade complete)
    }
    const rest = wm_rest_alpha(w.id);
    if (w.id != da.focused_id and rest < 256) return @intCast(rest);
    return 256;
}

/// M69g (#1558): whether a user window is COMPLETELY covered by a higher
/// opaque user window. The compositor blits window CONTENT in z-order, so a
/// full-viewport client really does hide every window under it — but the
/// chrome pass runs AFTER the whole stack, so without this check a hidden
/// window paints its title bar and border straight over the covering
/// client's content. GOTABWM's own 96x64 client-death probe window did
/// exactly that over GOSH's tab, hiding the shell's first terminal line (the
/// only line a fresh prompt has). Chrome that cannot be seen is not drawn.
/// Pure over the registry.
fn chrome_occluded(i: usize) bool {
    const w = &da.windows[i];
    if (w.kind != .user) return false;
    var j = i + 1;
    while (j < da.win_count) : (j += 1) {
        const o = &da.windows[j];
        if (o.kind != .user or !o.visible) continue;
        if (!workspace_visible(o)) continue;
        if (client_alpha(o) < 256) continue; // a translucent cover lets chrome through
        if (o.x > w.x or o.y > w.y) continue;
        if (@as(u64, o.x) + o.w < @as(u64, w.x) + w.w) continue;
        if (@as(u64, o.y) + o.h < @as(u64, w.y) + w.h) continue;
        return true;
    }
    return false;
}

/// Card U5/U4: the chrome pass, drawn on the framebuffer AFTER the window
/// paints and BEFORE the transfer — user title bars, the focus ring on the
/// focused window, and the pointer cursor. Chrome never touches a window's
/// back-buffer (user buffers stay EL0-owned).
///
/// M32 WMS4: the per-window chrome decision (elements + colors) comes from
/// the WM's descriptor (`effective_chrome`) — the kernel blits what the
/// descriptor dictates; it no longer decides chrome. The element gating
/// that is DATA, not policy (focus, always-on-top, workspace, terminal)
/// stays kernel-side exactly as before.
pub fn draw_chrome() void {
    const fb: [*]u8 = @ptrCast(&virtio_gpu.gpu_fb);
    const stride = virtio_gpu.fb_width * 4;
    const wspan = virtio_gpu.fb_width;
    const hspan = virtio_gpu.fb_height;
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        const w = &da.windows[i];
        if (w.kind != .user or !w.visible) continue;
        // Arc4 #241: skip chrome for windows not in the current workspace.
        if (!workspace_visible(w)) continue;
        // M69g (#1558): a fully covered window is not visible, so its chrome
        // must not be painted over the covering window's content.
        if (chrome_occluded(i)) continue;
        // M32 WMS4: the chrome LOOK (elements + colors) is the WM's
        // descriptor; the shim's own rules are just the default descriptor.
        const ch = effective_chrome(w);
        // Kind zero disables every chrome element, including shadow bands.
        if (ch.kind == 0) continue;
        const focus_accent = (ch.flags & geom.chrome_flag_focus_accent) != 0;
        // M20-U9: 2px border around the whole window first (paint order:
        // background → border → title bar → buttons → title → content).
        if (ch.kind & geom.chrome_border != 0) {
            const b = if (focus_accent and w.id == da.focused_id) ch.border_rgb else ch.border_unfocus_rgb;
            const bw = chrome_border_w;
            const ww: usize = if (w.w > wspan) wspan else w.w;
            const wh: usize = if (w.h > hspan) hspan else w.h;
            fill_rect(fb, stride, w.x, w.y, ww, bw, b); // top
            if (wh > bw) fill_rect(fb, stride, w.x, w.y + wh - bw, ww, bw, b); // bottom
            fill_rect(fb, stride, w.x, w.y, bw, wh, b); // left
            if (ww > bw) fill_rect(fb, stride, w.x + ww - bw, w.y, bw, wh, b); // right
        }
        // M37 DQ4: drop-shadow bands OUTSIDE the window rect (right +
        // bottom + corner), drawn back-to-front per window so a front
        // window covers the shadow of the one beneath. Flag-gated
        // (`settings set shadow on`, default off): put_px does no
        // clipping, so every band is clamped to the scanout here.
        if (settings.get_shadow()) {
            const sc = shadow_color();
            const so = chrome_shadow_off;
            const ww: usize = if (w.w > wspan) wspan else w.w;
            const wh: usize = if (w.h > hspan) hspan else w.h;
            const zx0 = w.x + ww;
            const zy0 = w.y + so;
            const zy1 = @min(w.y + wh, hspan);
            if (zx0 < wspan and zy1 > zy0) {
                const zx1 = @min(zx0 + so, wspan);
                fill_rect(fb, stride, zx0, zy0, zx1 - zx0, zy1 - zy0, sc); // right
            }
            const bx0 = w.x + so;
            const by0 = w.y + wh;
            if (by0 < hspan and bx0 < wspan) {
                const bx1 = @min(bx0 + ww -| so, wspan);
                const by1 = @min(by0 + so, hspan);
                if (bx1 > bx0) fill_rect(fb, stride, bx0, by0, bx1 - bx0, by1 - by0, sc); // bottom
                if (zx0 < wspan and by1 > by0) {
                    const cx1 = @min(zx0 + so, wspan);
                    fill_rect(fb, stride, zx0, by0, cx1 - zx0, by1 - by0, sc); // corner
                }
            }
        }
        // Title bar: "dui<id> pid=<pid>" (the owning pid when known),
        // CENTERED with "..." truncation when it does not fit (M20-U9). The
        // label is DATA (kernel-owned); its colors are the WM's.
        if (ch.kind & geom.chrome_title != 0) {
            fill_rect(fb, stride, w.x, w.y, w.w, user_title_h, ch.title_bg_rgb);
            var tb: [24]u8 = undefined;
            var n: usize = 0;
            const label = "dui";
            @memcpy(tb[0..3], label);
            n = 3;
            const idstr = fmt_decimal(tb[n..], w.id);
            n += idstr.len;
            if (w.owner) |pid| {
                const ps = " pid=";
                @memcpy(tb[n..][0..ps.len], ps);
                n += ps.len;
                const pids = fmt_decimal(tb[n..], pid);
                n += pids.len;
            }
            const lay = chrome_title_layout(w.w, n);
            if (!lay.truncated) {
                draw_string_16(fb, stride, w.x + lay.x_off, w.y, tb[0..lay.draw_len], ch.title_fg_rgb);
            } else {
                var tt: [24]u8 = undefined;
                @memcpy(tt[0..lay.draw_len], tb[0..lay.draw_len]);
                @memcpy(tt[lay.draw_len..][0..3], "...");
                draw_string_16(fb, stride, w.x + lay.x_off, w.y, tt[0 .. lay.draw_len + 3], ch.title_fg_rgb);
            }
        }
        // Step 6: close button ("×" — red glyph at top-right of title bar).
        if (ch.kind & geom.chrome_close != 0) {
            draw_glyph(fb, stride, w.x + w.w - 14, w.y + 4, 'x', ch.close_rgb);
        }
        // Step 7: minimize button ("—" — muted glyph left of close).
        if (ch.kind & geom.chrome_minimize != 0) {
            draw_glyph(fb, stride, w.x + w.w - 26, w.y + 4, '-', ch.min_rgb);
        }
        // M21 W8: pin indicator for always-on-top windows ("*" — cyan glyph left of minimize).
        if (ch.kind & geom.chrome_pin != 0 and w.always_on_top and w.w >= 50) {
            draw_glyph(fb, stride, w.x + w.w - 38, w.y + 4, '*', ch.pin_rgb);
        }
        // M37 DQ2 (issue #840): tab strip on containers with attached tabs.
        // Containers only (children paint no strip of their own); hidden
        // groups never reach here (invisible windows skip above).
        if (ch.kind & geom.chrome_tab_bar != 0 and w.tab_parent == 0 and tab_group_count(w.id) > 0) {
            paint_tab_strip(fb, stride, w, &ch, wspan, hspan);
        }
    }
    // The focus ring: on the focused window's rect (D4 — focus is always
    // visible). 0xff = none (no ring). The full-screen TERMINAL never
    // carries it (issue #164): it is the always-focused background, and a
    // ring around it paints a white strip over the first/last text row
    // and column — the live-glyphs tripwire decoded cell 0 of every line
    // as unknown. The clock and user windows keep the ring.
    if (da.focused_id != 0xff) {
        var idx: usize = 0;
        while (idx < da.win_count) : (idx += 1) {
            if (da.windows[idx].id != da.focused_id) continue;
            const w = &da.windows[idx];
            if (w.kind == .terminal) break;
            // Arc4 #241: don't draw focus ring on off-workspace windows.
            if (!workspace_visible(w)) break;
            // M32 WMS4: the ring is drawn only when the WM's descriptor
            // enables it, in the WM's ring color (shim: always, accent).
            if (effective_chrome(w).kind & geom.chrome_ring == 0) break;
            const rx: usize = w.x;
            const ry: usize = w.y;
            const rw: usize = if (w.w > wspan) wspan else w.w;
            const rh: usize = if (w.h > hspan) hspan else w.h;
            const ring_rgb = effective_chrome(w).ring_rgb;
            fill_rect(fb, stride, rx, ry, rw, focus_ring_w, ring_rgb);
            fill_rect(fb, stride, rx, ry + rh - focus_ring_w, rw, focus_ring_w, ring_rgb);
            fill_rect(fb, stride, rx, ry, focus_ring_w, rh, ring_rgb);
            fill_rect(fb, stride, rx + rw - focus_ring_w, ry, focus_ring_w, rh, ring_rgb);
            break;
        }
    }
    // M15 C2 (Alt+Tab overlay, #225): centered window previews while Alt held.
    // Topmost below notification (D7 ordering), above user chrome. Dim the
    // backdrop by filling a translucent dark rect over the whole scanout, then
    // a centered list of user-window rows with highlight on `overlay_selected`.
    if (da.overlay_active and da.overlay_count > 0) {
        // Dim backdrop — 50% dark (opaque dark gray is the honest preview for the
        // host's ScreenCaptureKit composite; true alpha would be compositor-pricey).
        fill_rect(fb, stride, 0, 0, wspan, hspan, 0x0f0f1a);
        const ov_w: u32 = 440;
        const row_h: u32 = 28;
        const header_h: u32 = 24;
        const pad: u32 = 8;
        const ov_h: u32 = header_h + pad * 2 + @as(u32, @intCast(da.overlay_count)) * row_h + pad;
        const ov_x: u32 = if (wspan > ov_w) (wspan - ov_w) / 2 else 0;
        const ov_y: u32 = if (hspan > ov_h) (hspan - ov_h) / 2 else 0;
        // Overlay surface + border.
        fill_rect(fb, stride, ov_x, ov_y, ov_w, ov_h, 0x1e293b);
        // 2-px border in accent.
        fill_rect(fb, stride, ov_x, ov_y, ov_w, 2, clock_accent_rgb);
        fill_rect(fb, stride, ov_x, ov_y + ov_h - 2, ov_w, 2, clock_accent_rgb);
        fill_rect(fb, stride, ov_x, ov_y, 2, ov_h, clock_accent_rgb);
        fill_rect(fb, stride, ov_x + ov_w - 2, ov_y, 2, ov_h, clock_accent_rgb);
        // M21 W4: show workspace name in the overlay header.
        draw_string(fb, stride, ov_x + pad, ov_y + pad, "Alt+Tab  —  Switch window", 0xffffff);
        var ws_label_buf: [8]u8 = undefined;
        ws_label_buf[0] = ' ';
        ws_label_buf[1] = ' ';
        ws_label_buf[2] = 'W';
        ws_label_buf[3] = 'S';
        ws_label_buf[4] = ' ';
        ws_label_buf[5] = '0' + da.current_workspace;
        draw_string(fb, stride, ov_x + pad + 200, ov_y + pad, ws_label_buf[0..6], 0xf59e0b);
        draw_string(fb, stride, ov_x + pad, ov_y + pad + 12, "(Tab / Shift+Tab cycles, release Alt)", 0x94a3b8);
        var idx: usize = 0;
        while (idx < da.overlay_count) : (idx += 1) {
            const id = da.overlay_ids[idx];
            const row_y = ov_y + header_h + pad + @as(u32, @intCast(idx)) * row_h;
            const is_sel = idx == da.overlay_selected;
            const bg = if (is_sel) taskbar_entry_active() else 0x0f172a;
            fill_rect(fb, stride, ov_x + pad, row_y, ov_w - pad * 2, row_h - 2, bg);
            // Outline highlight on selected.
            if (is_sel) {
                fill_rect(fb, stride, ov_x + pad, row_y, ov_w - pad * 2, 1, 0xffffff);
                fill_rect(fb, stride, ov_x + pad, row_y + row_h - 3, ov_w - pad * 2, 1, 0xffffff);
            }
            var win_title: []const u8 = "user";
            var win_owner: ?usize = null;
            var k: usize = 0;
            while (k < da.win_count) : (k += 1) {
                if (da.windows[k].id == id and da.windows[k].kind == .user) {
                    win_title = da.windows[k].title;
                    win_owner = da.windows[k].owner;
                    break;
                }
            }
            var tbuf: [32]u8 = undefined;
            var tn: usize = 0;
            @memcpy(tbuf[0..4], "dui ");
            tn = 4;
            const id_s = fmt_decimal(tbuf[tn..], id);
            tn += id_s.len;
            if (win_owner) |pid| {
                const pfx = " pid=";
                @memcpy(tbuf[tn..][0..pfx.len], pfx);
                tn += pfx.len;
                const pid_s = fmt_decimal(tbuf[tn..], pid);
                tn += pid_s.len;
            }
            draw_string(fb, stride, ov_x + pad + 6, row_y + 10, tbuf[0..tn], if (is_sel) 0xffffff else 0xd8dee9);
            draw_string(fb, stride, ov_x + ov_w - pad - 80, row_y + 10, win_title, 0x94a3b8);
            // M27 G3: render scaled preview of the window's content.
            render_preview(id);
            const pv_x = ov_x + ov_w - pad - 68;
            const pv_y = row_y + 4;
            const pv_w: u32 = 64;
            const pv_h: u32 = row_h - 8;
            blit_rect(fb, stride, @ptrCast(&da.preview_buf), pv_w * 4, pv_x, pv_y, pv_w, pv_h);
            // Border around preview.
            fill_rect(fb, stride, pv_x, pv_y, pv_w, 1, 0x64748b);
            fill_rect(fb, stride, pv_x, pv_y + pv_h - 1, pv_w, 1, 0x64748b);
            fill_rect(fb, stride, pv_x, pv_y, 1, pv_h, 0x64748b);
            fill_rect(fb, stride, pv_x + pv_w - 1, pv_y, 1, pv_h, 0x64748b);
        }
    }
    // WM2 mission-control overview (issue #707 card 2): the grid paints
    // above the alt-tab overlay (both cannot be open at once in practice —
    // the WM exits overview before other chords — but a deterministic order
    // keeps the scanout honest if they ever coincide).
    if (da.overview_open) paint_overview(fb, stride, wspan, hspan);
    // M32 WMS8 Gate 6 (issue #628): the kernel snap-preview render (dragging
    // near an edge highlighted the zone) is DELETED — the WM owns snap-on-
    // drop; with a WM registered pointer_tick never reaches this drag path.
    // Arc4 #242: unsaved-changes confirmation dialog — centered modal.
    if (da.unsaved_dialog_open) {
        const dlg_w: u32 = 200;
        const dlg_h: u32 = 100;
        const dlg_x: u32 = if (wspan > dlg_w) (wspan - dlg_w) / 2 else 0;
        const dlg_y: u32 = if (hspan > dlg_h) (hspan - dlg_h) / 2 else 0;
        // Dim backdrop.
        fill_rect(fb, stride, 0, 0, wspan, hspan, 0x0f0f1a);
        // Dialog surface.
        fill_rect(fb, stride, dlg_x, dlg_y, dlg_w, dlg_h, 0x1e293b);
        // Border.
        fill_rect(fb, stride, dlg_x, dlg_y, dlg_w, 2, 0xf59e0b);
        fill_rect(fb, stride, dlg_x, dlg_y + dlg_h - 2, dlg_w, 2, 0xf59e0b);
        fill_rect(fb, stride, dlg_x, dlg_y, 2, dlg_h, 0xf59e0b);
        fill_rect(fb, stride, dlg_x + dlg_w - 2, dlg_y, 2, dlg_h, 0xf59e0b);
        // Message.
        draw_string(fb, stride, dlg_x + 10, dlg_y + 10, "Save before closing?", 0xffffff);
        // Buttons: Save (green), Don't Save (red), Cancel (gray).
        fill_rect(fb, stride, dlg_x + 20, dlg_y + dlg_h - 30, 60, 20, 0x10b981);
        draw_string(fb, stride, dlg_x + 26, dlg_y + dlg_h - 24, "Save", 0xffffff);
        fill_rect(fb, stride, dlg_x + 90, dlg_y + dlg_h - 30, 60, 20, 0xef4444);
        draw_string(fb, stride, dlg_x + 96, dlg_y + dlg_h - 24, "Don't", 0xffffff);
        fill_rect(fb, stride, dlg_x + 160, dlg_y + dlg_h - 30, 30, 20, 0x64748b);
        draw_string(fb, stride, dlg_x + 163, dlg_y + dlg_h - 24, "No", 0xffffff);
    }
    // Arc4 #240: notification toasts — top-right, stacked newest-top.
    // 300×40 px per toast, 8×8 font, colored left border.
    if (da.notify_count > 0) {
        const toast_w: u32 = 300;
        const toast_h: u32 = 40;
        const toast_x: u32 = if (wspan > toast_w) wspan - toast_w - 8 else 0;
        var ti: usize = 0;
        while (ti < da.notify_count) : (ti += 1) {
            if (notify_entry(ti)) |entry| {
                const toast_y: u32 = 8 + @as(u32, @intCast(ti)) * (toast_h + 4);
                if (toast_y + toast_h > hspan) break;
                // Background.
                fill_rect(fb, stride, toast_x, toast_y, toast_w, toast_h, 0x1e293b);
                // Colored left border: blue=info, amber=warn, red=error.
                const border_color: u32 = switch (entry.level) {
                    1 => 0xf59e0b, // amber warning
                    2 => 0xef4444, // red error
                    else => 0x3b82f6, // blue info
                };
                fill_rect(fb, stride, toast_x, toast_y, 4, toast_h, border_color);
                // Text — up to 36 chars (300-8 px / 8 px per glyph).
                const max_chars = 36;
                const len = @min(entry.text.len, max_chars);
                if (len > 0) {
                    draw_string(fb, stride, toast_x + 10, toast_y + 6, entry.text[0..len], 0xffffff);
                }
            }
        }
    }
    // M21 W5: notification center panel — right-side pull-out when open.
    if (da.notif_center_open) {
        const panel_x: u32 = if (wspan > notif_center_w) wspan - notif_center_w else 0;
        const panel_y: u32 = 40;
        const row_h: u32 = 36;
        const pad: u32 = 8;
        const header_h: u32 = 24;
        const display_count = @min(da.notify_count, 8);
        const panel_h: u32 = header_h + pad * 2 + @as(u32, @intCast(display_count)) * row_h + pad + 28;
        // Dim backdrop.
        fill_rect(fb, stride, 0, 0, wspan, hspan, 0x0f0f1a);
        // Panel surface.
        fill_rect(fb, stride, panel_x, panel_y, notif_center_w, panel_h, 0x1e293b);
        // Border.
        fill_rect(fb, stride, panel_x, panel_y, notif_center_w, 2, 0x3b82f6);
        fill_rect(fb, stride, panel_x, panel_y + panel_h - 2, notif_center_w, 2, 0x3b82f6);
        fill_rect(fb, stride, panel_x, panel_y, 2, panel_h, 0x3b82f6);
        fill_rect(fb, stride, panel_x + notif_center_w - 2, panel_y, 2, panel_h, 0x3b82f6);
        // Header.
        draw_string(fb, stride, panel_x + pad, panel_y + pad, "Notifications", 0xffffff);
        // Notification rows.
        var ni: usize = 0;
        while (ni < display_count) : (ni += 1) {
            if (notify_entry(ni)) |entry| {
                const row_y = panel_y + header_h + pad + @as(u32, @intCast(ni)) * row_h;
                const bg: u32 = if (ni == 0) 0x0f172a else 0x1e293b;
                fill_rect(fb, stride, panel_x + pad, row_y, notif_center_w - pad * 2, row_h - 2, bg);
                // Colored left border.
                const border_color: u32 = switch (entry.level) {
                    1 => 0xf59e0b,
                    2 => 0xef4444,
                    else => 0x3b82f6,
                };
                fill_rect(fb, stride, panel_x + pad, row_y, 4, row_h - 2, border_color);
                // Text — up to 34 chars.
                const max_chars = 34;
                const len = @min(entry.text.len, max_chars);
                if (len > 0) {
                    draw_string(fb, stride, panel_x + pad + 10, row_y + 6, entry.text[0..len], 0xffffff);
                }
            }
        }
        // "Clear all" button.
        const btn_y = panel_y + header_h + pad * 2 + @as(u32, @intCast(display_count)) * row_h + pad;
        fill_rect(fb, stride, panel_x + pad, btn_y, notif_center_w - pad * 2, 20, 0xef4444);
        draw_string(fb, stride, panel_x + pad + 8, btn_y + 6, "Clear all", 0xffffff);
    }
    // M27 G2: about dialog — centered modal.
    if (da.about_dialog_open) {
        const dlg_w: u32 = 280;
        const dlg_h: u32 = 160;
        const dlg_x: u32 = if (wspan > dlg_w) (wspan - dlg_w) / 2 else 0;
        const dlg_y: u32 = if (hspan > dlg_h) (hspan - dlg_h) / 2 else 0;
        // Dim backdrop.
        fill_rect(fb, stride, 0, 0, wspan, hspan, 0x0f0f1a);
        // Dialog surface.
        fill_rect(fb, stride, dlg_x, dlg_y, dlg_w, dlg_h, 0x1e293b);
        // Border.
        fill_rect(fb, stride, dlg_x, dlg_y, dlg_w, 2, 0x3b82f6);
        fill_rect(fb, stride, dlg_x, dlg_y + dlg_h - 2, dlg_w, 2, 0x3b82f6);
        fill_rect(fb, stride, dlg_x, dlg_y, 2, dlg_h, 0x3b82f6);
        fill_rect(fb, stride, dlg_x + dlg_w - 2, dlg_y, 2, dlg_h, 0x3b82f6);
        // Title.
        draw_string(fb, stride, dlg_x + 10, dlg_y + 10, "VirelaiOS", clock_accent_rgb);
        // Version.
        draw_string(fb, stride, dlg_x + 10, dlg_y + 26, "v0.1  (M21+M27)", 0x94a3b8);
        // Info lines.
        draw_string(fb, stride, dlg_x + 10, dlg_y + 42, "AArch64 on Apple Virtualization", 0xd8dee9);
        draw_string(fb, stride, dlg_x + 10, dlg_y + 54, ".framework", 0xd8dee9);
        draw_string(fb, stride, dlg_x + 10, dlg_y + 70, "Built with Zig 0.17", 0x94a3b8);
        draw_string(fb, stride, dlg_x + 10, dlg_y + 86, "Window Manager: Driving Award", 0x94a3b8);
        // Close button.
        fill_rect(fb, stride, dlg_x + dlg_w - 16, dlg_y + 4, 8, 8, 0xef4444);
        draw_glyph(fb, stride, dlg_x + dlg_w - 14, dlg_y + 4, 'x', 0xffffff);
    }
    // M27 G6: tooltip — small text box below cursor on hover.
    if (da.tooltip_visible and da.tooltip_text_len > 0) {
        const tw: u32 = @as(u32, da.tooltip_text_len) * 8 + 8;
        const th: u32 = 14;
        var tx: u32 = da.cursor_x + 4;
        var ty: u32 = da.cursor_y + 12;
        // Clamp to scanout.
        if (tx + tw > wspan) tx = if (wspan > tw) wspan - tw else 0;
        if (ty + th > hspan) ty = if (hspan > th) hspan - th else 0;
        fill_rect(fb, stride, tx, ty, tw, th, 0x1e293b);
        fill_rect(fb, stride, tx, ty, tw, 1, 0x3b82f6);
        draw_string(fb, stride, tx + 4, ty + 3, da.tooltip_text[0..da.tooltip_text_len], 0xffffff);
    }
}

test "hosted chrome: full viewport replaces the native frame before the first present" {
    arm();
    clear_wm_chrome();
    da.wm_owns_user_layer = true;
    defer da.wm_owns_user_layer = false;
    const id = switch (user_open(32, 32, 512, 384, 7)) {
        .opened => |opened| opened,
        else => return error.TestUnexpectedResult,
    };
    virtio_gpu.fill_framebuffer(0x182026);
    try std.testing.expectEqual(virtio_gpu.CmdResult.ok, paint_scene());
    const old_border = (415 * virtio_gpu.fb_width + 300) * 4;
    try std.testing.expectEqual(@as(u8, 0xf6), virtio_gpu.gpu_fb[old_border]);
    var desc = geom.chrome_parity_policy();
    desc.kind = 0;
    try std.testing.expect(set_window_chrome(id, desc));
    try std.testing.expect(wm_apply_rect(id, 0, 0, virtio_gpu.fb_width, virtio_gpu.fb_height));
    const win = find_user_window(id).?;
    // No tick or present has occurred since the handoff. Neither the old
    // body/title/frame nor a new screen-edge ring may survive it.
    draw_chrome();
    try std.testing.expectEqualSlices(u8, kbuf_ptr(win)[0..virtio_gpu.fb_size], &virtio_gpu.gpu_fb);
    try std.testing.expectEqual(@as(u16, 256), client_alpha(win));
    // A later split remains hosted; focus changes cannot reintroduce chrome.
    try std.testing.expect(wm_apply_rect(id, 0, 0, 640, 720));
    virtio_gpu.fill_framebuffer(0x182026);
    draw_chrome();
    try std.testing.expectEqual(@as(u8, 0x26), virtio_gpu.gpu_fb[0]);
    da.focused_id = 0xff;
    draw_chrome();
    try std.testing.expectEqual(@as(u8, 0x26), virtio_gpu.gpu_fb[0]);
}

test "hosted chrome: migrated clients skip frame title and focus ring" {
    arm();
    clear_wm_chrome();
    da.wm_owns_user_layer = true;
    defer da.wm_owns_user_layer = false;
    const id = switch (user_open(32, 32, 512, 384, 7)) {
        .opened => |opened| opened,
        else => return error.TestUnexpectedResult,
    };
    try std.testing.expect(user_bind_surface(id, .{ .handle = 1, .pa_base = 0x1000_0000, .page_count = 192 }));
    var desc = geom.chrome_parity_policy();
    desc.kind = 0;
    try std.testing.expect(set_window_chrome(id, desc));
    virtio_gpu.fill_framebuffer(0x182026);
    try std.testing.expectEqual(virtio_gpu.CmdResult.ok, paint_scene());
    const at = (32 * virtio_gpu.fb_width + 32) * 4;
    try std.testing.expectEqualSlices(u8, &.{ 0x26, 0x20, 0x18, 0xff }, virtio_gpu.gpu_fb[at..][0..4]);
}

test "hosted chrome: no-chrome descriptor also suppresses shadows" {
    arm();
    clear_wm_chrome();
    const shadow = settings.get_shadow();
    defer {
        _ = settings.set("shadow", if (shadow) "on" else "off");
    }
    _ = settings.set("shadow", "on");
    const id = switch (user_open(32, 32, 512, 384, 7)) {
        .opened => |opened| opened,
        else => return error.TestUnexpectedResult,
    };
    var desc = geom.chrome_parity_policy();
    desc.kind = 0;
    try std.testing.expect(set_window_chrome(id, desc));
    virtio_gpu.fill_framebuffer(0x182026);
    draw_chrome();
    for (0..virtio_gpu.fb_size / 4) |i| {
        try std.testing.expectEqualSlices(u8, &.{ 0x26, 0x20, 0x18, 0xff }, virtio_gpu.gpu_fb[i * 4 ..][0..4]);
    }
}

test "hosted chrome: explicit seat chrome and the unbound shim retain their look" {
    arm();
    clear_wm_chrome();
    da.wm_owns_user_layer = true;
    defer da.wm_owns_user_layer = false;
    const id = switch (user_open(32, 32, 512, 384, 7)) {
        .opened => |opened| opened,
        else => return error.TestUnexpectedResult,
    };
    try std.testing.expect(wm_apply_rect(id, 0, 0, virtio_gpu.fb_width, virtio_gpu.fb_height));
    const menu = switch (user_open(224, 692, 96, 20, 1)) {
        .opened => |opened| opened,
        else => return error.TestUnexpectedResult,
    };
    var desc = effective_chrome(find_user_window(menu).?);
    desc.kind = geom.chrome_border;
    desc.border_rgb = 0x123456;
    try std.testing.expect(set_window_chrome(menu, desc));
    virtio_gpu.fill_framebuffer(0x182026);
    draw_chrome();
    const at = (692 * virtio_gpu.fb_width + 224) * 4;
    try std.testing.expectEqualSlices(u8, &.{ 0x56, 0x34, 0x12, 0xff }, virtio_gpu.gpu_fb[at..][0..4]);
    try std.testing.expect(user_close(menu));
    try std.testing.expect(focus(id));
    // The descriptor remains authoritative even for a full-viewport client.
    try std.testing.expect(set_window_chrome(id, desc));
    virtio_gpu.fill_framebuffer(0x182026);
    draw_chrome();
    try std.testing.expectEqual(@as(u8, 0x56), virtio_gpu.gpu_fb[0]);
    clear_wm_chrome();
    da.wm_owns_user_layer = false;
    virtio_gpu.fill_framebuffer(0x182026);
    draw_chrome();
    try std.testing.expectEqual(@as(u8, 0xf6), virtio_gpu.gpu_fb[0]);
}

test "hosted chrome: zero broadcast is refused without changing existing policy" {
    arm();
    clear_wm_chrome();
    const policy = geom.chrome_parity_policy();
    try std.testing.expect(set_window_chrome(geom.chrome_window_all, policy));
    var none = policy;
    none.kind = 0;
    try std.testing.expect(!set_window_chrome(geom.chrome_window_all, none));
    try std.testing.expectEqual(policy.kind, da.wm_chrome_policy.?.kind);
    clear_wm_chrome();
}
