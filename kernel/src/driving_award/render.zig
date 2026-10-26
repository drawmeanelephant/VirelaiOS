//! Framebuffer draw primitives and the per-window content painters (clock, terminal, user buffers).
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const TaskbarEntry = da.TaskbarEntry;
const Window = da.Window;
const clock_accent_rgb = da.clock_accent_rgb;
const clock_bg = da.clock_bg;
const clock_border_rgb = da.clock_border_rgb;
const clock_fg_rgb = da.clock_fg_rgb;
const clock_h = da.clock_h;
const clock_title_bg = da.clock_title_bg;
const clock_title_fg_rgb = da.clock_title_fg_rgb;
const clock_w = da.clock_w;
const dock_bg_rgb = da.dock_bg_rgb;
const dock_icon_active_rgb = da.dock_icon_active_rgb;
const dock_icon_bg_rgb = da.dock_icon_bg_rgb;
const fbtext = da.fbtext;
const find_user_window = da.find_user_window;
const focus = da.focus;
const font = da.font;
const font_metrics = da.font_metrics;
const font_unicode = da.font_unicode;
const format_hhmm = da.format_hhmm;
const geom = da.geom;
const kbuf_ptr = da.kbuf_ptr;
const taskbar_bg = da.taskbar_bg;
const taskbar_entries = da.taskbar_entries;
const taskbar_entry_active = da.taskbar_entry_active;
const taskbar_entry_dimmed = da.taskbar_entry_dimmed;
const taskbar_entry_minimized = da.taskbar_entry_minimized;
const taskbar_h = da.taskbar_h;
const taskbar_indicator_rgb = da.taskbar_indicator_rgb;
const taskbar_min_dot_rgb = da.taskbar_min_dot_rgb;
const taskbar_min_title_rgb = da.taskbar_min_title_rgb;
const taskbar_y = da.taskbar_y;
const terminal = da.terminal;
const theme_letter = da.theme_letter;
const tray_clipboard_filled = da.tray_clipboard_filled;
const tray_theme_accent = da.tray_theme_accent;
const tray_w = da.tray_w;
const tray_x = da.tray_x;
const tray_y = da.tray_y;
const user_surface = da.user_surface;
const user_windows_max = da.user_windows_max;
const virtio_gpu = da.virtio_gpu;
const wallpaper_bot = da.wallpaper_bot;
const wallpaper_top = da.wallpaper_top;
const workspace_max = da.workspace_max;
const client_alpha = @import("chrome.zig").client_alpha;
const surface_covers = @import("user.zig").surface_covers;

// ---------------------------------------------------------------------------
// Pixel helpers (pure — host-testable)
// ---------------------------------------------------------------------------

pub fn put_px(buf: [*]u8, stride: usize, x: usize, y: usize, rgb: u32) void {
    const off = y * stride + x * 4;
    buf[off] = @truncate(rgb & 0xff); // B
    buf[off + 1] = @truncate((rgb >> 8) & 0xff); // G
    buf[off + 2] = @truncate((rgb >> 16) & 0xff); // R
    buf[off + 3] = 0xff; // X — opaque (the claim-6053 lesson)
}

pub fn fill_rect(buf: [*]u8, stride: usize, x0: usize, y0: usize, w: usize, h: usize, rgb: u32) void {
    var y: usize = 0;
    while (y < h) : (y += 1) {
        var x: usize = 0;
        while (x < w) : (x += 1) put_px(buf, stride, x0 + x, y0 + y, rgb);
    }
}

/// Draw one 8x8 glyph at (x0, y0) in the given 0xRRGGBB color. Non-printable
/// bytes are skipped (no invented pixels).
pub fn draw_glyph(buf: [*]u8, stride: usize, x0: usize, y0: usize, c: u8, rgb: u32) void {
    if (c < 0x20 or c > 0x7e) return;
    const glyph = font.glyphs[c - 0x20];
    var gy: usize = 0;
    while (gy < 8) : (gy += 1) {
        const bits = glyph[gy];
        var gx: usize = 0;
        while (gx < 8) : (gx += 1) {
            if (font.row_pixel(bits, gx)) put_px(buf, stride, x0 + gx, y0 + gy, rgb);
        }
    }
}

pub fn draw_string(buf: [*]u8, stride: usize, x0: usize, y0: usize, s: []const u8, rgb: u32) void {
    for (s, 0..) |c, i| draw_glyph(buf, stride, x0 + i * 8, y0, c, rgb);
}

/// M73a-2 (#1631): resolve one terminal rune cell to its 8x8 rows — the
/// shared M20 lookup (`text.glyph_for` → `font_unicode`), the pinned
/// fallback (unmapped or ill-formed art is U+FFFD, never a skip), and
/// part 1's overlay policy OR'd onto the base.
pub fn rune_cell_rows(cell: terminal.Cell) [8]u8 {
    const base = fbtext.glyph_for(cell.base) orelse &font_unicode.fffd_glyph;
    var rows = base.*;
    if (cell.mark != 0) {
        if (font_unicode.combining_overlay(cell.mark)) |ov| {
            for (0..8) |i| rows[i] |= ov[i];
        }
    }
    return rows;
}

/// M73l (#1661): paint one cell at (x0, y0). ASCII with no overlay
/// paints from the FiraCode atlas — the face IS the cell now (8×16,
/// 4-bit alpha over the cell fill); everything else keeps the composed
/// 8×8 rune art, stretched ×1/×2 into the taller cell (no coverage
/// regression). A wide base still spans both cells of its pair —
/// `wide` is the caller's clipped pair check — and a continuation cell
/// draws nothing (the base owns the pair: no double-draw, no shift).
pub fn draw_cell_glyph(buf: [*]u8, stride: usize, x0: usize, y0: usize, cell: terminal.Cell, rgb: u32, wide: bool, italic: bool) void {
    if (cell.cont != 0) return;
    // M73l: the atlas path — M72b's byte-identical font8x8 fast path
    // retires with the 8×8 cell (ADR 0020 Amendment F).
    if (cell.mark == 0 and cell.base >= 0x20 and cell.base <= 0x7e) {
        draw_atlas_glyph(buf, stride, x0, y0, cell.base, rgb, italic);
        return;
    }
    // Fallback: composed rows (rune/overlay path), nearest-neighbour
    // stretched over the WHOLE tall cell (M80i #1725: the zoom ladder's
    // cells are 13/16/21 tall — no longer a multiple of 8 — so every
    // output row samples its source row instead of a fixed ×1/×2
    // repeat). At cell_h 8/16 the mapping is byte-identical to the old
    // stretch; at 13/21 it fills the cell instead of stranding the
    // bottom rows empty.
    const rows = rune_cell_rows(cell);
    const ch: usize = font_metrics.cell_h;
    var dy: usize = 0;
    while (dy < ch) : (dy += 1) {
        const bits = rows[dy * 8 / ch];
        // M73h (Amendment E): fake italic — top rows shift right up
        // to 2px, bottom none, same ≤2px rendition over the taller
        // cell. A pixel that would leave the cell is DROPPED (no
        // column bleed: the pixel gates pin x extents).
        const xoff: usize = if (italic) italic_shear(dy) else 0;
        var gx: usize = 0;
        while (gx < 8) : (gx += 1) {
            if (font.row_pixel(bits, gx)) {
                if (gx + xoff < font_metrics.cell_w) {
                    put_px(buf, stride, x0 + gx + xoff, y0 + dy, rgb);
                    if (wide) put_px(buf, stride, x0 + gx + xoff + font_metrics.cell_w, y0 + dy, rgb);
                }
            }
        }
    }
}

/// M73l: the ≤2px top shear, linear over the cell's height — dy 0 (cell
/// top) shifts 2px right, dy cell_h-1 (bottom) shifts none.
fn italic_shear(dy: usize) usize {
    return ((font_metrics.cell_h - 1 - dy) * 2) / (font_metrics.cell_h - 1);
}

/// M73l: one atlas glyph, alpha-blended over the cell fill. Placement
/// is baked into the fixture (baseline at `ascent` from the top), so
/// this walks rows 0..cell_h and high/low nibbles per byte. Pixels the
/// shear would push past the cell edge are dropped (M73h x-extent pin).
/// M80i (#1725): the blob and its row/glyph strides come from the
/// ACTIVE cell (font_metrics), so the painter follows the zoom ladder
/// — each size's atlas is rasterized at its own pixel size.
fn draw_atlas_glyph(buf: [*]u8, stride: usize, x0: usize, y0: usize, cp: u21, rgb: u32, italic: bool) void {
    const blob = font_metrics.active().blob;
    const off: usize = @as(usize, cp - font_metrics.first_cp) * font_metrics.glyph_bytes;
    const row_stride = font_metrics.row_bytes; // cell_w/2 rounded up: 4-bit pixels
    var dy: usize = 0;
    while (dy < font_metrics.cell_h) : (dy += 1) {
        const xoff: usize = if (italic) italic_shear(dy) else 0;
        const row = off + dy * row_stride;
        var gx: usize = 0;
        while (gx < font_metrics.cell_w) : (gx += 1) {
            const byte = blob[row + gx / 2];
            const lvl: u8 = if (@rem(gx, 2) == 0) byte >> 4 else byte & 0x0f;
            if (lvl == 0) continue;
            if (gx + xoff >= font_metrics.cell_w) continue;
            put_alpha_px(buf, stride, x0 + gx + xoff, y0 + dy, rgb, lvl * 17);
        }
    }
}

/// M73l: blend one atlas pixel (alpha 0..255) over what the cell fill
/// left. Level 15 lands exactly on fg (255/255 — no rounding slop for
/// the golden pins), level 0 is never called.
fn put_alpha_px(buf: [*]u8, stride: usize, x: usize, y: usize, rgb: u32, a: u16) void {
    const off = y * stride + x * 4;
    const inv: u16 = 255 - a;
    const s_b: u16 = @truncate(rgb & 0xff);
    const s_g: u16 = @truncate((rgb >> 8) & 0xff);
    const s_r: u16 = @truncate((rgb >> 16) & 0xff);
    buf[off] = @intCast((@as(u16, buf[off]) * inv + s_b * a) / 255);
    buf[off + 1] = @intCast((@as(u16, buf[off + 1]) * inv + s_g * a) / 255);
    buf[off + 2] = @intCast((@as(u16, buf[off + 2]) * inv + s_r * a) / 255);
    buf[off + 3] = 0xff;
}

/// Step 6 (Issue #206): draw one 8×16 glyph at (x0, y0). Uses the 2×-stretched
/// glyph table for titles and headings. Non-printable bytes are skipped.
pub fn draw_glyph_16(buf: [*]u8, stride: usize, x0: usize, y0: usize, c: u8, rgb: u32) void {
    if (c < 0x20 or c > 0x7e) return;
    const glyph = font.glyphs_16[c - 0x20];
    var gy: usize = 0;
    while (gy < 16) : (gy += 1) {
        const bits = glyph[gy];
        var gx: usize = 0;
        while (gx < 8) : (gx += 1) {
            if (font.row_pixel_16(bits, gx)) put_px(buf, stride, x0 + gx, y0 + gy, rgb);
        }
    }
}

/// Draw a string using the 8×16 font. Each glyph advances 8px horizontally.
pub fn draw_string_16(buf: [*]u8, stride: usize, x0: usize, y0: usize, s: []const u8, rgb: u32) void {
    for (s, 0..) |c, i| draw_glyph_16(buf, stride, x0 + i * 8, y0, c, rgb);
}

/// Minimal unsigned-decimal formatting (no heap, no failure modes) — the
/// clock's dynamic text. Returns the filled slice.
pub fn fmt_decimal(buf: []u8, v: u64) []const u8 {
    if (buf.len == 0) return "";
    if (v == 0) {
        buf[0] = '0';
        return buf[0..1];
    }
    var tmp: [20]u8 = undefined;
    var n: usize = 0;
    var x = v;
    while (x > 0 and n < tmp.len) : (n += 1) {
        tmp[n] = @as(u8, @intCast(x % 10)) + '0';
        x /= 10;
    }
    const out = @min(n, buf.len);
    var i: usize = 0;
    while (i < out) : (i += 1) buf[i] = tmp[n - 1 - i];
    return buf[0..out];
}

/// Copy a B8G8R8X8 rect from `src` (stride `src_stride` bytes) into `dst`
/// (stride `dst_stride` bytes) at (dx, dy). Pure — host-testable.
pub fn blit_rect(
    dst: [*]u8,
    dst_stride: usize,
    src: [*]const u8,
    src_stride: usize,
    dx: usize,
    dy: usize,
    w: usize,
    h: usize,
) void {
    var y: usize = 0;
    while (y < h) : (y += 1) {
        const drow = dst + (dy + y) * dst_stride + dx * 4;
        const srow = src + y * src_stride;
        @memcpy(drow[0 .. w * 4], srow[0 .. w * 4]);
    }
}

/// Arc4 #239: alpha-blended blit. Blends `src` over `dst` with the
/// given alpha (0..256). 256 = fully opaque (same as blit_rect),
/// 64 = 25%, 128 = 50%. Per-pixel: dst = dst × (1 - a/256) + src × (a/256).
/// Pure BSS math, no allocation.
pub fn blit_rect_alpha(
    dst: [*]u8,
    dst_stride: usize,
    src: [*]const u8,
    src_stride: usize,
    dx: usize,
    dy: usize,
    w: usize,
    h: usize,
    alpha: u16,
) void {
    if (alpha >= 256) {
        blit_rect(dst, dst_stride, src, src_stride, dx, dy, w, h);
        return;
    }
    const inv_alpha: u16 = 256 - alpha;
    var y: usize = 0;
    while (y < h) : (y += 1) {
        const drow = dst + (dy + y) * dst_stride + dx * 4;
        const srow = src + y * src_stride;
        var x: usize = 0;
        while (x < w * 4) : (x += 1) {
            const d: u16 = drow[x];
            const s: u16 = srow[x];
            drow[x] = @intCast((d * inv_alpha + s * alpha) >> 8);
        }
    }
}

// ---------------------------------------------------------------------------
// Window painting
// ---------------------------------------------------------------------------

pub fn fb_canvas() fbtext.Canvas {
    return .{
        .base = @ptrCast(&virtio_gpu.gpu_fb),
        .width = virtio_gpu.fb_width,
        .height = virtio_gpu.fb_height,
        .stride = virtio_gpu.fb_width * 4,
    };
}

/// Render the clock window's content into a buffer. `focused` selects the
/// focus-status line. Pure — host-testable with an injectable buffer.
pub fn render_clock_content(buf: [*]u8, stride: usize, w: usize, h: usize, ticks: u64, focused: bool) void {
    // Background + border (2 px), then the amber title bar.
    fill_rect(buf, stride, 0, 0, w, h, clock_bg());
    fill_rect(buf, stride, 0, 0, w, 2, clock_border_rgb);
    fill_rect(buf, stride, 0, h - 2, w, 2, clock_border_rgb);
    fill_rect(buf, stride, 0, 0, 2, h, clock_border_rgb);
    fill_rect(buf, stride, w - 2, 0, 2, h, clock_border_rgb);
    fill_rect(buf, stride, 2, 2, w - 4, 16, clock_title_bg());

    draw_string(buf, stride, 8, 6, "clock", clock_title_fg_rgb);

    var nb: [24]u8 = undefined;
    // Body lines (8 px grid, origin 8 px inside the border).
    draw_string_16(buf, stride, 8, 26, "DRIVING AWARD", clock_accent_rgb);
    draw_string(buf, stride, 8, 42, "ticks=", clock_fg_rgb);
    draw_string(buf, stride, 8 + 6 * 8, 42, fmt_decimal(&nb, ticks), clock_fg_rgb);
    draw_string(buf, stride, 8, 58, if (focused) "focus=clock" else "focus=roadpops", clock_fg_rgb);
    draw_string(buf, stride, 8, 74, "windows=2", clock_fg_rgb);
}

/// #1082 (ADR 0020 Amendment A, A4): render a window-bound terminal's
/// presentation grid into its back-buffer with the kernel glyph raster. The
/// bytes were already drained into the grid (file_table's write path); this
/// turns the grid into the window's pixels, and the compositor blits it on
/// its cadence (the deferred-present discipline).
const ansi_palette = [_]u32{
    0x000000, 0xcd3131, 0x0dbc79, 0xe5e510,
    0x2472c8, 0xbc3fbc, 0x11a8cd, 0xe5e5e5,
    0x666666, 0xf14c4c, 0x23d18b, 0xf5f543,
    0x3b8eea, 0xd670d6, 0x29b8db, 0xffffff,
};

/// M73h (#1634): one palette index → RGB. 0..=15 keep the paint-local
/// `ansi_palette` (pixel parity for old sequences); 16..=255 come from
/// the canonical xterm table in terminal.zig.
fn terminalColour(index: u8) u32 {
    if (index < ansi_palette.len) return ansi_palette[index];
    return rgbToU32(terminal.xterm256Rgb(index));
}

fn rgbToU32(c: terminal.Rgb) u32 {
    return (@as(u32, c.r) << 16) | (@as(u32, c.g) << 8) | c.b;
}

/// M73h (#1634, dim SGR 2): reduce each channel to 2/3 intensity.
fn dimRgb(c: u32) u32 {
    const r = ((c >> 16) & 0xff) * 2 / 3;
    const g = ((c >> 8) & 0xff) * 2 / 3;
    const b = (c & 0xff) * 2 / 3;
    return (r << 16) | (g << 8) | b;
}

/// M73h (#1634, deliverable 3): what the terminal's DEFAULT colour slots
/// (256) resolve to — the desktop theme, the same `theme_id` source the
/// chrome accessors read (#207). Dark (the boot theme) is BYTE-IDENTICAL
/// to the pre-M73h hardcoded `fbtext.fg_rgb`/`fbtext.bg_rgb`, so every
/// existing pixel gate stays valid; pinned by driving_award_test.
pub fn terminalThemeDefaults() struct { fg: u32, bg: u32 } {
    return switch (da.theme_id) {
        1 => .{ .fg = 0x353b45, .bg = 0xf5f6f8 }, // light: slate ink on paper
        2 => .{ .fg = 0xffd27f, .bg = 0x0a1a2e }, // amber: warm text on navy
        // M73m (#1662): the user's own colours. The slot semantics are
        // unchanged (Amendment E: presentation default, resolved at paint,
        // never a stored per-cell RGB) — only what "default" resolves TO.
        3 => .{ .fg = da.custom_fg, .bg = da.custom_bg }, // custom == the store's palette
        else => .{ .fg = fbtext.fg_rgb, .bg = fbtext.bg_rgb }, // dark == legacy
    };
}

/// M73h: what one cell paints as — both channels resolved to RGB
/// (truecolour side arrays > palette/xterm > theme default), then
/// reverse swaps them (xterm semantics: the FLAG is stored, the swap
/// happens here), then dim scales the foreground. `italic`/`underline`
/// pass through for the draw path.
pub const Rendition = struct { fg: u32, bg: u32, italic: bool, underline: bool };

pub fn terminalRendition(style: terminal.CellStyle, fg_side: ?terminal.Rgb, bg_side: ?terminal.Rgb) Rendition {
    const theme = terminalThemeDefaults();
    var fg: u32 = undefined;
    if (style.fg == terminal.rgb_colour) {
        fg = rgbToU32(fg_side orelse .{});
    } else if (terminal.styleForeground(style)) |colour| {
        fg = terminalColour(colour);
        if (terminal.styleBold(style) and colour < 8) fg = terminalColour(colour + 8);
    } else if (terminal.styleBold(style) and da.theme_id == 0) {
        // The historical default is terminal-green rather than ANSI
        // colour 7. Give `SGR 1` on that default a visible bright-green
        // rendition too (dark theme ONLY — byte parity with the pre-M73h
        // gates; light/amber/custom keep their theme foreground, so a
        // custom palette is never overridden by the legacy green).
        fg = ansi_palette[10];
    } else {
        fg = theme.fg;
    }
    var bg: u32 = if (style.bg == terminal.rgb_colour)
        rgbToU32(bg_side orelse .{})
    else if (terminal.styleBackground(style)) |colour|
        terminalColour(colour)
    else
        theme.bg;
    if (terminal.styleReverse(style)) {
        const tmp = fg;
        fg = bg;
        bg = tmp;
    }
    if (terminal.styleDim(style)) fg = dimRgb(fg);
    return .{ .fg = fg, .bg = bg, .italic = style.italic, .underline = style.underline };
}

/// M85b (#1814): one image cell — its tile's SET pixels over the cell
/// (unset sixel pixels are transparent), scaled nearest-neighbour from
/// the geometry the image was placed at to the active cell, so a font
/// zoom keeps the image covering exactly its cells.
fn paint_image_tile(dst: [*]u8, stride: usize, x0: usize, y0: usize, tile: terminal.term_image.Tile) void {
    const cw: usize = font_metrics.cell_w;
    const ch: usize = font_metrics.cell_h;
    var dy: usize = 0;
    while (dy < ch) : (dy += 1) {
        var dx: usize = 0;
        while (dx < cw) : (dx += 1) {
            if (tile.sample(dx, dy, cw, ch)) |rgb| put_px(dst, stride, x0 + dx, y0 + dy, rgb);
        }
    }
}

pub fn render_terminal_screen(dst: [*]u8, w: *const Window, scr: *const terminal.Screen) void {
    const wu: usize = @intCast(w.w);
    const hu: usize = @intCast(w.h);
    const stride = wu * 4;
    fill_rect(dst, stride, 0, 0, wu, hu, fbtext.bg_rgb);
    const cols = @min(scr.columns(), wu / font_metrics.cell_w);
    // The compositor draws the title bar over the top `title_bar_h` rows, so
    // the grid's first line starts below it (client-area origin).
    const y0: usize = @intCast(geom.title_bar_h);
    const rows = if (hu > y0) (hu - y0) / font_metrics.cell_h else 0;
    if (cols == 0 or rows == 0) return;
    const total = scr.lineCount();
    // M49 SD5 (#1132): the scrollback view shifts the first visible line
    // back by `view` lines (0 = follow the tail).
    const view = scr.viewOffset();
    const bottom = if (total > view) total - view else 0;
    const first = if (bottom > rows) bottom - rows else 0;
    var r: usize = 0;
    while (r < rows) : (r += 1) {
        const ri = first + r;
        // M73a-2 (#1631): `line()` is the length gate (lens, in cells) and
        // the byte source for the ASCII fast path; the real content comes
        // from `cellAt` — runes, overlays, and wide pairs included.
        const line = scr.line(ri);
        const ry = y0 + r * font_metrics.cell_h;
        var c: usize = 0;
        while (c < cols) : (c += 1) {
            const cell = scr.cellAt(ri, c);
            const side = scr.rgbAt(ri, c);
            const colours = terminalRendition(scr.styleAt(ri, c), side.fg, side.bg);
            const x = c * font_metrics.cell_w;
            const wide = fbtext.char_width(cell.base) >= 2 and c + 1 < cols;
            // M49 SD5: selected cells invert (fg on bg).
            if (scr.inSelection(ri, c)) {
                fill_rect(dst, stride, x, ry, font_metrics.cell_w, font_metrics.cell_h, colours.fg);
                if (c < line.len) draw_cell_glyph(dst, stride, x, ry, cell, colours.bg, wide, colours.italic);
            } else if (cell.cont != 0) {
                // The wide base already filled this cell's half of the pair
                // and stroked its glyph across both cells; a continuation
                // paints nothing of its own.
            } else if (colours.bg != fbtext.bg_rgb or c < line.len) {
                // A wide base fills (and strokes) its whole pair in one pass.
                const span: usize = if (wide) font_metrics.wide_cell_w else font_metrics.cell_w;
                fill_rect(dst, stride, x, ry, span, font_metrics.cell_h, colours.bg);
                if (c < line.len) draw_cell_glyph(dst, stride, x, ry, cell, colours.fg, wide, colours.italic);
            }
            // M85b (#1814): an image cell composites its tile over the
            // fill it just got (image cells carry the erase default, so
            // that is the theme background — or the selection's ink).
            if (cell.img != 0) {
                if (scr.imageTile(ri, c)) |tile| paint_image_tile(dst, stride, x, ry, tile);
            }
            // M73h: underline strokes the cell's bottom row (the base owns
            // its whole pair — a continuation skips so the line draws
            // once, wide or not).
            if (colours.underline and cell.cont == 0) {
                const u_span: usize = if (wide) font_metrics.wide_cell_w else font_metrics.cell_w;
                fill_rect(dst, stride, x, ry + font_metrics.underline_row, u_span, 1, colours.fg);
            }
        }
    }
    // The block cursor: invert the cell the shell's line editor is at —
    // only while following the tail (a scrolled-back view has no cursor).
    // M73k (#1637): unified index — with history, the grid-relative
    // cursorLine would sit `hist_count` rows above its painted position.
    const cl = scr.cursorLineUnified();
    if (scr.cursor_visible and view == 0 and cl >= first and cl - first < rows) {
        const cc = scr.cursorCol();
        if (cc < cols) {
            const cy = y0 + (cl - first) * font_metrics.cell_h;
            const c_side = scr.rgbAt(cl, cc);
            const colours = terminalRendition(scr.styleAt(cl, cc), c_side.fg, c_side.bg);
            fill_rect(dst, stride, cc * font_metrics.cell_w, cy, font_metrics.cell_w, font_metrics.cell_h, colours.fg);
            const line = scr.line(cl);
            const cell = scr.cellAt(cl, cc);
            const wide = fbtext.char_width(cell.base) >= 2 and cc + 1 < cols;
            // M80i (#1725): was `cc * 8` — the one geometry literal
            // M73l missed. At the zoom sizes the cursor glyph landed a
            // row-width away from its cell fill.
            if (cc < line.len) draw_cell_glyph(dst, stride, cc * font_metrics.cell_w, cy, cell, colours.bg, wide, colours.italic);
        }
    }
}

/// Paint one window into the framebuffer. The terminal renders the shared
/// text layer straight into the scanout framebuffer; the clock renders its
/// back-buffer and blits it over the terminal.
pub fn paint(w: *Window) void {
    switch (w.kind) {
        .terminal => fbtext.render(fb_canvas()),
        .clock => {
            render_clock_content(
                @ptrCast(&da.clock_buf),
                clock_w * 4,
                clock_w,
                clock_h,
                if (da.clock_has_tick) da.clock_shown_tick else 0,
                da.focused_id == w.id,
            );
            blit_rect(
                @ptrCast(&virtio_gpu.gpu_fb),
                virtio_gpu.fb_width * 4,
                @ptrCast(&da.clock_buf),
                clock_w * 4,
                w.x,
                w.y,
                w.w,
                w.h,
            );
        },
        .user => {
            // M33 SB3 (claim 9361): a SURFACE-BACKED window's rendering lives
            // in its shared-anonymous pages (mapped into the owner's root and
            // mirrored RO into the registered WM), NOT the kernel pool
            // buffer. The kernel root identity-maps the low physical space, so
            // composite() blits directly from the surface's OWN pages — the
            // on-scanout bytes are the app's plain stores, byte-identical to
            // what the old fill path produced (parity). An unmigrated window
            // blits from its pool back-buffer (WM1), sized exactly win.w×h.
            const surface = user_surface(w.id);
            // #2111: blit from the surface only while the rect still fits
            // inside its pages; a desync falls back to the pool back-buffer
            // (always exactly w×h) rather than reading past the region.
            const src_ptr: [*]const u8 = if (surface) |sf|
                if (surface_covers(sf.page_count, w.w, w.h))
                    @as([*]const u8, @ptrFromInt(sf.pa_base))
                else
                    kbuf_ptr(w)
            else
                kbuf_ptr(w);
            // #1082 (A4): a WINDOW-bound terminal repaints its whole grid
            // into the back-buffer from the kernel's presentation state; the
            // blit below then shows it. The full-grid render means the whole
            // window is repainted, so the partial-damage path is skipped.
            // M49 SD5 (#1132): sync the grid to the window's width first so
            // a resize reflows before this frame paints.
            terminal.syncWindowCols(w.id, w.w);
            const term_screen = terminal.screenOf(w.id);
            if (term_screen) |scr| {
                render_terminal_screen(@constCast(src_ptr), w, scr);
            }
            const src_stride: usize = @as(usize, w.w) * 4;
            // The source is the window's own back-buffer (surface and pool
            // buffer are both sized exactly win.w × win.h); clamp SOURCE
            // dims for the legacy rect overflow (M21 tiling) defensively.
            const bw = w.w;
            const bh = w.h;
            // M33 SB4 (claim 2382): rect-granular repaint — when this window
            // carries a partial damage rect, blit ONLY that region (from its
            // back-buffer origin to the scanout origin), not the whole window.
            // Whole-window ops (move/focus/fade) leave `damaged` false -> full
            // blit, so the pre-SB4 path is unchanged.
            var src_off: usize = 0;
            var dest_x: u32 = w.x;
            var dest_y: u32 = w.y;
            var sw = bw;
            var sh = bh;
            if (w.damaged and term_screen == null) {
                const sx = @min(w.dx, bw);
                const sy = @min(w.dy, bh);
                sw = @min(w.dw, bw - @min(w.dx, bw));
                sh = @min(w.dh, bh - @min(w.dy, bh));
                dest_x = w.x + sx;
                dest_y = w.y + sy;
                src_off = @as(usize, sy) * src_stride + @as(usize, sx) * 4;
            }
            const src_ptr2: [*]const u8 = src_ptr + src_off;
            // Arc4 #239: during fade-in, blend with alpha over the
            // background (wallpaper/terminal already composited below).
            // WM4 (issue #707 card 4): at REST — fade complete — an
            // UNFOCUSED window with a rest-alpha policy blends its CLIENT
            // area at that alpha (the chrome is drawn opaque afterwards,
            // so the WMS4 chrome pixel parity is untouched). No policy /
            // no WM / focused = 256, byte-identical to v1.
            const alpha: u16 = client_alpha(w);
            if (alpha < 256) {
                blit_rect_alpha(
                    @ptrCast(&virtio_gpu.gpu_fb),
                    virtio_gpu.fb_width * 4,
                    src_ptr2,
                    src_stride,
                    dest_x,
                    dest_y,
                    sw,
                    sh,
                    alpha,
                );
            } else {
                blit_rect(
                    @ptrCast(&virtio_gpu.gpu_fb),
                    virtio_gpu.fb_width * 4,
                    src_ptr2,
                    src_stride,
                    dest_x,
                    dest_y,
                    sw,
                    sh,
                );
            }
        },
        .wallpaper => {
            // Step 9: vertical gradient from wallpaper_top() to wallpaper_bot().
            const fb: [*]u8 = @ptrCast(&virtio_gpu.gpu_fb);
            const stride = virtio_gpu.fb_width * 4;
            var y: u32 = 0;
            while (y < virtio_gpu.fb_height) : (y += 1) {
                const t = y * 256 / virtio_gpu.fb_height;
                const r = ((wallpaper_top() >> 16) & 0xff) * (256 - t) / 256 + ((wallpaper_bot() >> 16) & 0xff) * t / 256;
                const g = ((wallpaper_top() >> 8) & 0xff) * (256 - t) / 256 + ((wallpaper_bot() >> 8) & 0xff) * t / 256;
                const b = (wallpaper_top() & 0xff) * (256 - t) / 256 + (wallpaper_bot() & 0xff) * t / 256;
                const row_color: u32 = (@as(u32, @intCast(r)) << 16) | (@as(u32, @intCast(g)) << 8) | @as(u32, @intCast(b));
                fill_rect(fb, stride, 0, y, virtio_gpu.fb_width, 1, row_color);
            }
        },
        .taskbar => {
            // Step 8: dark background bar spanning full width.
            const fb: [*]u8 = @ptrCast(&virtio_gpu.gpu_fb);
            const stride = virtio_gpu.fb_width * 4;
            fill_rect(fb, stride, 0, taskbar_y, virtio_gpu.fb_width, taskbar_h, taskbar_bg());
            // Arc4 #241: workspace switcher [1][2][3] on the left side.
            // WM3: the cell geometry is the shared wnd_core rule.
            var ws_idx: u8 = 0;
            while (ws_idx < workspace_max) : (ws_idx += 1) {
                const ws_x = geom.taskbar_ws_x0 + @as(u32, ws_idx) * geom.taskbar_ws_stride;
                const is_cur = ws_idx == da.current_workspace;
                const ws_bg = if (is_cur) taskbar_entry_active() else taskbar_entry_dimmed();
                fill_rect(fb, stride, ws_x, taskbar_y + 2, geom.taskbar_ws_cell, taskbar_h - 4, ws_bg);
                const label: [1]u8 = .{'1' + ws_idx};
                draw_string(fb, stride, ws_x + 6, taskbar_y + 6, label[0..1], 0xffffff);
            }
            // WM3 (issue #707 card 3): entries for the CURRENT workspace
            // only, enumerated ID-ASCENDING by the shared wnd_core rule —
            // the same enumeration the registered WM hit-tests with (the
            // drift guard: kernel render and WM hit-test cannot disagree).
            // The focused entry carries the active fill + the 2px
            // underline indicator; a minimized entry carries the darker
            // fill + restore dot (the taskbar is the restore target).
            var tb: [user_windows_max]TaskbarEntry = undefined;
            const tn = taskbar_entries(&tb);
            var ei: u32 = 0;
            while (ei < tn) : (ei += 1) {
                const r = geom.taskbar_entry_rect(ei, workspace_max, virtio_gpu.fb_width, virtio_gpu.fb_height, tray_w) orelse break;
                const is_focused = tb[ei].focused;
                const is_min = tb[ei].minimized;
                const ww = find_user_window(tb[ei].id).?;
                const entry_bg = if (is_focused) taskbar_entry_active() else if (is_min) taskbar_entry_minimized() else taskbar_entry_dimmed();
                fill_rect(fb, stride, r.x, r.y, r.w, r.h, entry_bg);
                const entry_title = geom.taskbar_title_slice(ww.title);
                const title_ink: u32 = if (is_min) taskbar_min_title_rgb else 0xffffff;
                draw_string(fb, stride, r.x + geom.taskbar_entry_pad, taskbar_y + 6, entry_title, title_ink);
                if (is_focused) {
                    // Active indicator: a 2px underline across the entry.
                    fill_rect(fb, stride, r.x + geom.taskbar_indicator_inset, r.y + r.h - geom.taskbar_indicator_h, r.w - 2 * geom.taskbar_indicator_inset, geom.taskbar_indicator_h, taskbar_indicator_rgb);
                } else if (is_min) {
                    // Restore affordance: a 4x4 dot in the entry's tail.
                    fill_rect(fb, stride, r.x + r.w - 9, r.y + 5, 4, 4, taskbar_min_dot_rgb);
                }
            }
            // Arc2 W3: tray in right 80px — HH:MM, D/L/A, clipboard rect.
            // HH:MM from tray_tick (tick-derived, 1 Hz). M32 WMS6 Gate E:
            // when the registered WM has declared a field (its `_set` flag),
            // draw ITS value; the shim fallback (no WM) stays derived.
            var tbuf: [5]u8 = undefined;
            const hhmm: []const u8 = if (da.wm_tray_clock_set)
                da.wm_tray_clock_text[0..]
            else
                format_hhmm(&tbuf, if (da.tray_has_tick) da.tray_tick else 0);
            draw_string(fb, stride, tray_x + 4, tray_y + 6, hhmm, 0xffffff);
            // Theme letter in accent
            var tletter: [1]u8 = .{if (da.wm_tray_theme_set) da.wm_tray_theme else theme_letter()};
            draw_string(fb, stride, tray_x + 48, tray_y + 6, tletter[0..1], tray_theme_accent());
            // Clipboard indicator: filled when has content, outline when empty.
            const clip_x = tray_x + 64;
            const clip_y = tray_y + 6;
            const clip_w: usize = 10;
            const clip_h: usize = 8;
            if (if (da.wm_tray_clip_set) da.wm_tray_clip else tray_clipboard_filled()) {
                fill_rect(fb, stride, clip_x, clip_y, clip_w, clip_h, 0xffffff);
            } else {
                fill_rect(fb, stride, clip_x, clip_y, clip_w, 1, 0xffffff);
                fill_rect(fb, stride, clip_x, clip_y + clip_h - 1, clip_w, 1, 0xffffff);
                fill_rect(fb, stride, clip_x, clip_y, 1, clip_h, 0xffffff);
                fill_rect(fb, stride, clip_x + clip_w - 1, clip_y, 1, clip_h, 0xffffff);
            }
        },
        .dock => {
            // M15 C4: 24 px left dock, vertical icon bar (hardcoded dock apps).
            const fb: [*]u8 = @ptrCast(&virtio_gpu.gpu_fb);
            const stride = virtio_gpu.fb_width * 4;
            fill_rect(fb, stride, w.x, w.y, w.w, w.h, dock_bg_rgb);
            // Icons for dock=true apps (first 5 from image/apps.txt).
            const dock_icons = [_]u8{ 'c', 'n', 't', 'b', 's' };
            // M21 W3: count minimized windows to show restore indicators.
            var minimized_count: usize = 0;
            {
                var mi: usize = 0;
                while (mi < da.win_count) : (mi += 1) {
                    if (da.windows[mi].kind == .user and da.windows[mi].minimized) minimized_count += 1;
                }
            }
            var idx: usize = 0;
            while (idx < dock_icons.len) : (idx += 1) {
                const iy = w.y + 8 + @as(u32, @intCast(idx)) * 32;
                if (iy + 24 > w.y + w.h) break;
                // M21 W3: highlight first icon if there are minimized windows.
                const bg = if (idx == 0 and minimized_count > 0)
                    0xf59e0b // amber — indicates minimized windows to restore
                else if (idx == 0 and da.focused_id == 2)
                    dock_icon_active_rgb
                else
                    dock_icon_bg_rgb;
                fill_rect(fb, stride, w.x + 2, iy, 20, 20, bg);
                draw_glyph(fb, stride, w.x + 8, iy + 6, dock_icons[idx], 0xffffff);
                // M21 W3: small dot indicator below the icon for each minimized window.
                if (idx == 0 and minimized_count > 0) {
                    fill_rect(fb, stride, w.x + 8, iy + 22, 2, 2, 0xf59e0b);
                }
            }
        },
    }
}

/// The repaint plan: the index of the LOWEST dirty visible window, or null
/// when the scene is clean. Pure — host-testable.
pub fn repaint_start() ?usize {
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].visible and da.windows[i].dirty) return i;
    }
    return null;
}
