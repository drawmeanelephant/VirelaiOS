//! Cursor and mode ops for `Screen` (split from terminal.zig, #3753): motion,
//! DECSTBM/DECOM, DECSC/DECRC, charsets (SO/SI, DEC special) and tab stops.

const std = @import("std");
const cell_mod = @import("cell.zig");
const screen_mod = @import("screen.zig");
const Charset = cell_mod.Charset;
const Rgb = cell_mod.Rgb;
const default_cell_style = cell_mod.default_cell_style;
const empty_rgb = cell_mod.empty_rgb;
const rgb_colour = cell_mod.rgb_colour;
const Screen = screen_mod.Screen;
const grid_cols = screen_mod.grid_cols;
const grid_lines = screen_mod.grid_lines;

pub fn clearSavedCursor(self: *Screen) void {
    self.saved_cur = 0;
    self.saved_col = 0;
    self.saved_style = default_cell_style;
    self.saved_fg_rgb = empty_rgb;
    self.saved_bg_rgb = empty_rgb;
    self.saved_charset_g0 = .ascii;
    self.saved_charset_g1 = .ascii;
    self.saved_charset_active = 0;
    self.saved_pending_wrap = false;
    self.saved_valid = false;
}

pub fn saveCursor(self: *Screen) void {
    self.saved_cur = self.cur;
    self.saved_col = self.col;
    self.saved_style = self.style;
    self.saved_fg_rgb = self.fg_rgb_cur;
    self.saved_bg_rgb = self.bg_rgb_cur;
    self.saved_charset_g0 = self.charset_g0;
    self.saved_charset_g1 = self.charset_g1;
    self.saved_charset_active = self.charset_active;
    self.saved_pending_wrap = self.pending_wrap;
    self.saved_valid = true;
}

pub fn restoreCursor(self: *Screen) void {
    if (!self.saved_valid) return;
    self.cur = @min(self.saved_cur, grid_lines - 1);
    // `cols` is a pending-wrap position when equal to the width, so do
    // not clamp to cols-1 here; resize is the only operation that may
    // make a saved column too wide.
    self.col = @min(self.saved_col, self.cols);
    self.style = self.saved_style;
    self.fg_rgb_cur = self.saved_fg_rgb;
    self.bg_rgb_cur = self.saved_bg_rgb;
    self.charset_g0 = self.saved_charset_g0;
    self.charset_g1 = self.saved_charset_g1;
    self.charset_active = self.saved_charset_active;
    self.pending_wrap = self.saved_pending_wrap;
    if (!self.pending_wrap and self.col >= self.cols) self.col = self.cols - 1;
}

pub fn setCharset(self: *Screen, slot: u8, final: u8) void {
    const selected: Charset = switch (final) {
        '0' => .line_drawing,
        'B' => .ascii,
        else => return,
    };
    switch (slot) {
        0 => self.charset_g0 = selected,
        1 => self.charset_g1 = selected,
        else => {},
    }
}

/// DEC Special Graphics translation for the bytes 0x5f..0x7e. The
/// complete table is small and keeps the grid's existing Unicode-cell
/// and font path; unsupported code points stay literal ASCII.
pub fn translateCharset(self: *const Screen, b: u8) u21 {
    const selected = if (self.charset_active == 1) self.charset_g1 else self.charset_g0;
    if (selected != .line_drawing or b < 0x5f or b > 0x7e) return b;
    return switch (b) {
        0x5f => 0x00a0,
        0x60 => 0x25c6,
        0x61 => 0x2592,
        0x62 => 0x2409,
        0x63 => 0x240c,
        0x64 => 0x240d,
        0x65 => 0x240a,
        0x66 => 0x00b0,
        0x67 => 0x00b1,
        0x68 => 0x2424,
        0x69 => 0x240b,
        0x6a => 0x2518,
        0x6b => 0x2510,
        0x6c => 0x250c,
        0x6d => 0x2514,
        0x6e => 0x253c,
        0x6f => 0x23ba,
        0x70 => 0x23bb,
        0x71 => 0x2500,
        0x72 => 0x23bc,
        0x73 => 0x23bd,
        0x74 => 0x251c,
        0x75 => 0x2524,
        0x76 => 0x2534,
        0x77 => 0x252c,
        0x78 => 0x2502,
        0x79 => 0x2264,
        0x7a => 0x2265,
        0x7b => 0x03c0,
        0x7c => 0x2260,
        0x7d => 0x00a3,
        0x7e => 0x00b7,
        else => b,
    };
}

pub fn keypadApplication(self: *const Screen) bool {
    return self.keypad_application;
}

pub fn activeTabStops(self: *Screen) *[2]u64 {
    return if (self.alt_active) &self.alt_tab_stops else &self.tab_stops;
}

pub fn setTabStop(self: *Screen, col: usize) void {
    if (col >= self.cols) return;
    const word = col / 64;
    self.activeTabStops()[word] |= @as(u64, 1) << @intCast(col % 64);
}

pub fn clearTabStop(self: *Screen, col: usize) void {
    if (col >= self.cols) return;
    const word = col / 64;
    self.activeTabStops()[word] &= ~(@as(u64, 1) << @intCast(col % 64));
}

pub fn hasTabStop(self: *const Screen, col: usize) bool {
    if (col >= self.cols) return false;
    const word = col / 64;
    return (self.activeTabStopsConst()[word] & (@as(u64, 1) << @intCast(col % 64))) != 0;
}

pub fn activeTabStopsConst(self: *const Screen) *const [2]u64 {
    return if (self.alt_active) &self.alt_tab_stops else &self.tab_stops;
}

/// Move by `count` tab stops, clamped to the effective width. TAB and
/// CHT search forward; CBT searches backward. Reaching either edge is
/// clamped (never a row wrap), and a pending right-margin wrap is
/// cancelled to the last visible column first.
pub fn moveTabStops(self: *Screen, count: u16, forward: bool) void {
    self.cancelPendingWrap();
    var remaining: usize = count;
    while (remaining > 0) : (remaining -= 1) {
        if (forward) {
            var col = self.col + 1;
            while (col < self.cols and !self.hasTabStop(col)) : (col += 1) {}
            self.col = @min(col, self.cols - 1);
            if (self.col == self.cols - 1 and !self.hasTabStop(self.col)) break;
        } else {
            var col = self.col;
            while (col > 0) {
                col -= 1;
                if (self.hasTabStop(col)) break;
            }
            self.col = col;
            if (col == 0 and !self.hasTabStop(0)) break;
        }
    }
}

/// xterm resize policy: stops beyond the new effective width are
/// discarded and do not return if the window later widens. Both screen
/// tables are masked because either may become active after the resize.
pub fn maskTabStops(self: *Screen) void {
    if (self.cols >= grid_cols) return;
    const low_mask: u64 = if (self.cols >= 64)
        std.math.maxInt(u64)
    else
        (@as(u64, 1) << @intCast(self.cols)) - 1;
    self.tab_stops[0] &= low_mask;
    self.tab_stops[1] = 0;
    self.alt_tab_stops[0] &= low_mask;
    self.alt_tab_stops[1] = 0;
}

/// M80a: cursor up/down (CUU/CUD, and CNL/CPL ride these) — clamp at
/// the grid edges (A/F at row 0, B/E at the last grid row) and never
/// grow `used`. The subtract saturates, so row 0 cannot underflow.
pub fn cursorUp(self: *Screen, n: u16) void {
    self.cancelPendingWrap();
    self.cur = self.cur -| @as(usize, n);
}

pub fn cursorDown(self: *Screen, n: u16) void {
    self.cancelPendingWrap();
    self.cur = @min(self.cur + @as(usize, n), grid_lines - 1);
}

/// M80c: the cursor's home row follows DECOM. With DECOM reset,
/// CUP/VPA address the full screen; with DECOM set, row 1 is the
/// scrolling region's top margin.
pub fn cursorHomeRow(self: *const Screen) usize {
    return if (self.origin_mode) self.scroll_top else 0;
}

pub fn homeCursor(self: *Screen) void {
    self.moveCursor(@intCast(self.cursorHomeRow() + 1), 1);
}

/// M80c: DECSTBM is one-based inclusive at the CSI boundary. Invalid
/// regions (including top == bottom) are refused without disturbing
/// the old margins or cursor.
pub fn setScrollRegion(self: *Screen, top_one: u16, bottom_one: u16) void {
    if (top_one == 0 or bottom_one == 0) return;
    const top: usize = @intCast(top_one - 1);
    const bottom: usize = @intCast(bottom_one - 1);
    if (top >= bottom or bottom >= grid_lines) return;
    self.scroll_top = top;
    self.scroll_bottom = bottom;
    self.pending_wrap = false;
    self.homeCursor();
}

/// M80c: DECOM changes the coordinate system, so changing it homes
/// the cursor just like a real DEC terminal.
pub fn setOriginMode(self: *Screen, enabled: bool) void {
    self.origin_mode = enabled;
    self.pending_wrap = false;
    self.homeCursor();
}

pub fn moveCursor(self: *Screen, row_one_based: u16, col_one_based: u16) void {
    self.pending_wrap = false;
    const requested_row: usize = @intCast(row_one_based - 1);
    const row = if (self.origin_mode)
        self.scroll_top + @min(requested_row, self.scroll_bottom - self.scroll_top)
    else
        @min(requested_row, grid_lines - 1);
    const column = @min(@as(usize, col_one_based - 1), self.cols - 1);
    while (self.used <= row) {
        self.clearLine(self.used);
        self.used += 1;
        self.noteTailGrowth(); // M73k: growth under a pinned view
    }
    self.cur = row;
    self.col = column;
}

test "terminal: DECSC/DECRC preserves truecolor rendition" {
    var s = Screen{};
    s.feed("\x1b[38;2;1;2;3mA\x1b7\x1b[0m\x1b[38;2;4;5;6mB\x1b8C");
    try std.testing.expectEqualStrings("AC", s.line(0));
    const style = s.styleAt(0, 1);
    try std.testing.expectEqual(rgb_colour, style.fg);
    const rgb = s.rgbAt(0, 1);
    try std.testing.expectEqual(@as(?Rgb, Rgb{ .r = 1, .g = 2, .b = 3 }), rgb.fg);
}

test "terminal: DECSC save slot survives an alternate-screen round trip" {
    var s = Screen{};
    s.feed("A\x1b7\x1b[?1049hB\x1b7\x1b[?1049l\x1b8C");
    try std.testing.expectEqualStrings("AC", s.line(0));
    try std.testing.expectEqual(@as(usize, 2), s.cursorCol());
}
