//! Grid cell vocabulary for the terminal screens (split from terminal.zig, #3753):
//! the rune `Cell`, rendition `CellStyle`/`Rgb`, charsets, the xterm-256 palette,
//! and the UTF-8 cell encoders used by the readers, selection copy and search.

const std = @import("std");

/// A bounded character grid with scrollback for one window-bound terminal.
/// Bytes fed from the output ring are laid out (CR/LF/BS/TAB, a minimal CSI
/// clear/home), wrapping at `cols` and scrolling one line at a time. Bytes
/// >= 0x80 are UTF-8-decoded into **rune cells** (M73a-1 #1625): wide
/// pairs, combining overlays, U+FFFD for ill-formed input. A window resize
/// reflows the stored lines to the new column count (M49 SD5 #1132) —
/// placement-aware, so a pair never splits. Pure: fixed arrays, no
/// allocation, host-testable.
pub const Point = struct { line: usize, col: usize };

/// M73a-1 (#1625, ADR 0020 Amendment D): one grid cell as presentation
/// state. `base` is the rune anchored here (U+0000..U+10FFFF), `mark` an
/// optional combining overlay on that rune (0 = none), and `cont` marks the
/// right half of a double-width pair — its `base` is always 0 and the glyph
/// lives in the cell to the left. Packed (60 bits, still 8 bytes) so the
/// 80x128 grids and the reflow snapshot stay contiguous.
///
/// M85b (#1814): `img` != 0 makes this an IMAGE cell — it shows tile
/// (`tx`, `ty`) of image buffer `img - 1` in the screen's image bank
/// (`term_image`). An image cell's rune is a blank, so selection, search
/// and `line()` read it as a space; it is written only by sixel
/// placement, and any rune write, erase or clear replaces it like text.
/// The fields ride in bits the 43-bit layout already paid for, so no
/// grid, history bank or snapshot grows.
pub const Cell = packed struct {
    base: u21 = ' ',
    mark: u21 = 0,
    cont: u1 = 0,
    img: u3 = 0,
    tx: u7 = 0,
    ty: u7 = 0,
};

comptime {
    std.debug.assert(@sizeOf(Cell) == 8);
}
/// The erased/initial cell: a blank, unmarked, glyph-owning cell.
pub const empty_cell: Cell = .{};

fn utf8Len(cp: u21) usize {
    if (cp < 0x80) return 1;
    if (cp < 0x800) return 2;
    if (cp < 0x10000) return 3;
    return 4;
}

/// Encode one rune at `dst[out..]`; null when the whole rune does not fit
/// (selection copy never writes a truncated UTF-8 sequence).
fn encodeOne(dst: []u8, out: usize, cp: u21) ?usize {
    const n = utf8Len(cp);
    if (out + n > dst.len) return null;
    if (n == 1) {
        dst[out] = @intCast(cp);
        return out + 1;
    } else if (n == 2) {
        dst[out] = 0xc0 | @as(u8, @intCast(cp >> 6));
        dst[out + 1] = 0x80 | @as(u8, @intCast(cp & 0x3f));
        return out + 2;
    } else if (n == 3) {
        dst[out] = 0xe0 | @as(u8, @intCast(cp >> 12));
        dst[out + 1] = 0x80 | @as(u8, @intCast((cp >> 6) & 0x3f));
        dst[out + 2] = 0x80 | @as(u8, @intCast(cp & 0x3f));
        return out + 3;
    }
    dst[out] = 0xf0 | @as(u8, @intCast(cp >> 18));
    dst[out + 1] = 0x80 | @as(u8, @intCast((cp >> 12) & 0x3f));
    dst[out + 2] = 0x80 | @as(u8, @intCast((cp >> 6) & 0x3f));
    dst[out + 3] = 0x80 | @as(u8, @intCast(cp & 0x3f));
    return out + 4;
}

/// Encode a cell's base then its overlay as UTF-8, all-or-nothing.
pub fn encodeCell(dst: []u8, out: usize, cell: Cell) ?usize {
    const total = utf8Len(cell.base) + (if (cell.mark != 0) utf8Len(cell.mark) else 0);
    if (out + total > dst.len) return null;
    var i = encodeOne(dst, out, cell.base).?;
    if (cell.mark != 0) i = encodeOne(dst, i, cell.mark).?;
    return i;
}

/// A terminal cell's rendition — foreground/background colour slot plus
/// attributes (M73h #1634, ADR 0020 Amendment E). This SUPERSEDES the
/// M72b/Amendment-C 16-colour freeze: a compact u16 could not hold the
/// selectors Charm apps actually emit (`38;5;n`, `38;2;r;g;b`, underline,
/// italic, reverse, dim).
///
/// `fg`/`bg` are 9-bit colour slots:
///   0..=255  an xterm 256-colour index (`38;5;n`/`48;5;n`; 0..=15 also
///            covers classic SGR 30-37/40-47/90-97/100-107);
///   256      the presentation default — resolved at PAINT time from the
///            desktop theme (#207 `theme_id`), never a stored RGB;
///   257      truecolour: this cell's RGB rides in the side arrays
///            (`fg_rgb`/`bg_rgb`) — one u32 cannot carry two 24-bit
///            channels, so RGB swaps and reflows beside the style word
///            (presentation state; ADR 0020 D1 holds, no new syscall).
///
/// `flags`: bold (SGR 1/22), dim (2/22), italic (3/23), underline (4/24),
/// reverse (7/27 — resolved at paint by swapping the channels). Nine pad
/// bits are unused: blink/overline/strike are M73h non-goals (storage
/// would be free, paint is not).
///
/// Old sequences stay pixel-identical: SGR 30-37/40-47/90-97 still store
/// 0..=15 and the paint side keeps `ansi_palette` for exactly those.
pub const CellStyle = packed struct(u32) {
    fg: colour_slot = default_colour,
    bg: colour_slot = default_colour,
    bold: bool = false,
    dim: bool = false,
    italic: bool = false,
    underline: bool = false,
    reverse: bool = false,
    _pad: u9 = 0,
};

/// One colour slot in a `CellStyle`.
pub const colour_slot = u9;

/// The presentation-default slot (paint resolves it from the theme).
/// Was 16 in the frozen u16 layout; u9 widens the space so index 16 is a
/// real xterm colour (`38;5;16`) instead of the sentinel.
pub const default_colour: colour_slot = 256;

/// The truecolour marker: the cell's RGB rides in the side arrays.
pub const rgb_colour: colour_slot = 257;

/// A 24-bit colour as stored beside a truecolour cell (3 packed bytes).
pub const Rgb = struct { r: u8 = 0, g: u8 = 0, b: u8 = 0 };

/// The erased/initial RGB side value (matches `empty_cell`).
pub const empty_rgb: Rgb = .{};

pub const default_cell_style: CellStyle = .{};

/// M80e (#1722): the bounded character-set vocabulary needed by the
/// classic DEC line-drawing designators. The parser stores the designation;
/// `putRune` translates only ASCII input while the selected set is active.
pub const Charset = enum(u8) {
    ascii = 0,
    line_drawing = 1,
};

/// The palette index of a cell's foreground, or null for default AND for
/// truecolour cells (callers wanting RGB use `Screen.rgbAt`).
pub fn styleForeground(style: CellStyle) ?u8 {
    if (style.fg == default_colour or style.fg == rgb_colour) return null;
    return @intCast(style.fg);
}

/// The palette index of a cell's background, or null for default AND for
/// truecolour cells (callers wanting RGB use `Screen.rgbAt`).
pub fn styleBackground(style: CellStyle) ?u8 {
    if (style.bg == default_colour or style.bg == rgb_colour) return null;
    return @intCast(style.bg);
}

pub fn styleBold(style: CellStyle) bool {
    return style.bold;
}

pub fn styleDim(style: CellStyle) bool {
    return style.dim;
}

pub fn styleItalic(style: CellStyle) bool {
    return style.italic;
}

pub fn styleUnderline(style: CellStyle) bool {
    return style.underline;
}

pub fn styleReverse(style: CellStyle) bool {
    return style.reverse;
}

/// M73h (#1634): the canonical xterm 256-colour table, total (no traps).
/// 0..=15 are the canonical xterm ANSI colours — the PAINT side keeps its
/// own `ansi_palette` for those (pixel parity for old sequences) and only
/// calls this for 16..=255; 16..=231 is the 6×6×6 cube (levels 0, 95,
/// 135, 175, 215, 255), 232..=255 the 24-step grayscale ramp
/// (8 + 10·i). Spot-checked class-A.
pub fn xterm256Rgb(index: u8) Rgb {
    const ansi = [_]Rgb{
        .{ .r = 0x00, .g = 0x00, .b = 0x00 }, .{ .r = 0xcd, .g = 0x00, .b = 0x00 },
        .{ .r = 0x00, .g = 0xcd, .b = 0x00 }, .{ .r = 0xcd, .g = 0xcd, .b = 0x00 },
        .{ .r = 0x00, .g = 0x00, .b = 0xee }, .{ .r = 0xcd, .g = 0x00, .b = 0xcd },
        .{ .r = 0x00, .g = 0xcd, .b = 0xcd }, .{ .r = 0xe5, .g = 0xe5, .b = 0xe5 },
        .{ .r = 0x7f, .g = 0x7f, .b = 0x7f }, .{ .r = 0xff, .g = 0x00, .b = 0x00 },
        .{ .r = 0x00, .g = 0xff, .b = 0x00 }, .{ .r = 0xff, .g = 0xff, .b = 0x00 },
        .{ .r = 0x5c, .g = 0x5c, .b = 0xff }, .{ .r = 0xff, .g = 0x00, .b = 0xff },
        .{ .r = 0x00, .g = 0xff, .b = 0xff }, .{ .r = 0xff, .g = 0xff, .b = 0xff },
    };
    if (index < 16) return ansi[index];
    if (index >= 232) {
        const level: u8 = 8 + 10 * (index - 232);
        return .{ .r = level, .g = level, .b = level };
    }
    const levels = [_]u8{ 0, 95, 135, 175, 215, 255 };
    const n = index - 16;
    return .{
        .r = levels[n / 36],
        .g = levels[(n / 6) % 6],
        .b = levels[n % 6],
    };
}

test "terminal: xterm256Rgb spot-checks (cube + grayscale + ansi)" {
    // Cube: index 16 = (0,0,0), 22 = level(1,0,0) = (0,95,0),
    // 52 = level(1) on red = (95,0,0), 102 = (135,135,135), 231 = white.
    try std.testing.expectEqual(Rgb{ .r = 0, .g = 0, .b = 0 }, xterm256Rgb(16));
    try std.testing.expectEqual(Rgb{ .r = 0, .g = 95, .b = 0 }, xterm256Rgb(22));
    try std.testing.expectEqual(Rgb{ .r = 95, .g = 0, .b = 0 }, xterm256Rgb(52));
    try std.testing.expectEqual(Rgb{ .r = 135, .g = 135, .b = 135 }, xterm256Rgb(102));
    try std.testing.expectEqual(Rgb{ .r = 255, .g = 255, .b = 255 }, xterm256Rgb(231));
    // Grayscale ramp: 232 = 8, 255 = 8 + 10·23 = 238.
    try std.testing.expectEqual(Rgb{ .r = 8, .g = 8, .b = 8 }, xterm256Rgb(232));
    try std.testing.expectEqual(Rgb{ .r = 238, .g = 238, .b = 238 }, xterm256Rgb(255));
    // 0..=15 are total (paint keeps its ansi_palette for these).
    try std.testing.expectEqual(Rgb{ .r = 0xe5, .g = 0xe5, .b = 0xe5 }, xterm256Rgb(7));
    try std.testing.expectEqual(Rgb{ .r = 0x00, .g = 0xff, .b = 0xff }, xterm256Rgb(14));
}
