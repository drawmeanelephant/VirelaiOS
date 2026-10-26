//! Grid editing ops for `Screen` (split from terminal.zig, #3753): insert/
//! delete/erase of chars and lines, ED/EL, and the primary/alternate swap.

const std = @import("std");
const cell_mod = @import("cell.zig");
const screen_mod = @import("screen.zig");
const Cell = cell_mod.Cell;
const CellStyle = cell_mod.CellStyle;
const Charset = cell_mod.Charset;
const Rgb = cell_mod.Rgb;
const default_cell_style = cell_mod.default_cell_style;
const empty_cell = cell_mod.empty_cell;
const empty_rgb = cell_mod.empty_rgb;
const Screen = screen_mod.Screen;
const default_tab_stops = screen_mod.default_tab_stops;
const grid_cols = screen_mod.grid_cols;
const grid_lines = screen_mod.grid_lines;

pub fn eraseLine(self: *Screen, mode: u16) void {
    self.cancelPendingWrap();
    var start: usize = switch (mode) {
        1 => 0,
        2 => 0,
        else => @min(self.col, self.cols),
    };
    var end: usize = switch (mode) {
        1 => @min(self.col + 1, self.cols),
        else => self.cols,
    };
    // M73a-1: never erase half a wide pair — extend the range over any
    // pair edge the requested range would split.
    if (start > 0 and self.cells[self.cur][start].cont != 0) start -= 1;
    if (end < grid_cols and self.cells[self.cur][end].cont != 0) end += 1;
    var c = start;
    while (c < end) : (c += 1) {
        self.cells[self.cur][c] = empty_cell;
        self.styles[self.cur][c] = default_cell_style;
        self.fg_rgb[self.cur][c] = empty_rgb;
        self.bg_rgb[self.cur][c] = empty_rgb;
    }
    if (mode == 2) {
        self.lens[self.cur] = 0;
    } else if (mode == 0 and start < self.lens[self.cur]) {
        self.lens[self.cur] = start;
    }
}

pub fn eraseDisplay(self: *Screen, mode: u16) void {
    switch (mode) {
        1 => {
            var row: usize = 0;
            while (row < self.cur) : (row += 1) self.clearLine(row);
            self.eraseLine(1);
        },
        2 => self.clearScreen(),
        // M80b (#1713): ED 3 is "Erase Saved Lines" (xterm ctlseqs
        // #411) — it was lumped with ED 2 here and unpinned.
        3 => self.clearScrollback(),
        else => {
            self.eraseLine(0);
            var row = self.cur + 1;
            while (row < self.used) : (row += 1) self.clearLine(row);
        },
    }
}

/// M80b (#1713): blank one cell to the erase default — the grid
/// erases to the presentation default (eraseLine's rule), never to
/// the current rendition (the grid is not BCE).
pub fn blankCell(self: *Screen, row: usize, col: usize) void {
    self.cells[row][col] = empty_cell;
    self.styles[row][col] = default_cell_style;
    self.fg_rgb[row][col] = empty_rgb;
    self.bg_rgb[row][col] = empty_rgb;
}

/// M80b: move one grid row wholesale (cells, styles, truecolour side
/// arrays, length). Wide pairs never straddle rows, so a row copy
/// cannot tear one.
pub fn copyRow(self: *Screen, from: usize, to: usize) void {
    self.cells[to] = self.cells[from];
    self.styles[to] = self.styles[from];
    self.lens[to] = self.lens[from];
    self.fg_rgb[to] = self.fg_rgb[from];
    self.bg_rgb[to] = self.bg_rgb[from];
}

/// M80b: CSI @ (ICH) — insert n blanks at the cursor and slide the
/// tail right; cells pushed past the right margin are dropped. The
/// slide never splits a wide pair (the insert point steps back to
/// the pair's base) and a pair cut by the right margin is dropped
/// WHOLE (its surviving base is blanked). Moved cells carry their
/// own rendition; the blanks are the erase default. The cursor stays
/// put — at a pending wrap it stands on the last column (BS parity)
/// — and an edit below the used tail is a no-op (M80a's rule: a row
/// materialises on write, never on edit).
pub fn insertChars(self: *Screen, n: u16) void {
    self.cancelPendingWrap();
    if (self.cur >= self.used) return;
    const w = self.cols;
    var start = self.col;
    if (start > 0 and self.cells[self.cur][start].cont != 0) start -= 1;
    const count: usize = @min(@as(usize, n), w);
    const drop: usize = w - count;
    const cut_pair = count > 0 and drop > start and self.cells[self.cur][drop].cont != 0;
    if (start + count < w) {
        var i: usize = w;
        while (i > start + count) {
            i -= 1;
            self.cells[self.cur][i] = self.cells[self.cur][i - count];
            self.styles[self.cur][i] = self.styles[self.cur][i - count];
            self.fg_rgb[self.cur][i] = self.fg_rgb[self.cur][i - count];
            self.bg_rgb[self.cur][i] = self.bg_rgb[self.cur][i - count];
        }
    }
    var j: usize = start;
    while (j < start + count and j < w) : (j += 1) self.blankCell(self.cur, j);
    if (cut_pair) self.blankCell(self.cur, w - 1);
    if (start < self.lens[self.cur]) {
        self.lens[self.cur] = @min(self.lens[self.cur] + count, w);
    }
}

/// M80b: CSI P (DCH) — delete n cells at the cursor and pull the
/// tail left; the right end fills with erase-default blanks. A
/// delete range that would split a wide pair extends over the pair
/// edge (eraseLine's rule) — the pair goes WHOLE. The cursor stays
/// put (pending wrap as above), and a delete below the tail is a
/// no-op.
pub fn deleteChars(self: *Screen, n: u16) void {
    self.cancelPendingWrap();
    if (self.cur >= self.used) return;
    const w = self.cols;
    var start = self.col;
    if (start > 0 and self.cells[self.cur][start].cont != 0) start -= 1;
    var end = @min(self.col + @as(usize, n), w);
    if (end < w and self.cells[self.cur][end].cont != 0) end += 1;
    const m = end - start;
    {
        var i: usize = start;
        while (i + m < w) : (i += 1) {
            self.cells[self.cur][i] = self.cells[self.cur][i + m];
            self.styles[self.cur][i] = self.styles[self.cur][i + m];
            self.fg_rgb[self.cur][i] = self.fg_rgb[self.cur][i + m];
            self.bg_rgb[self.cur][i] = self.bg_rgb[self.cur][i + m];
        }
    }
    var j: usize = w - m;
    while (j < w) : (j += 1) self.blankCell(self.cur, j);
    if (start < self.lens[self.cur]) {
        self.lens[self.cur] = @max(start, self.lens[self.cur] -| m);
    }
}

/// M80b: CSI X (ECH) — erase n cells at the cursor IN PLACE (no
/// slide) to the erase default; the range extends over wide-pair
/// edges like eraseLine, and the length trims when the erase reaches
/// the row end. The cursor stays put (pending wrap as above).
pub fn eraseChars(self: *Screen, n: u16) void {
    self.cancelPendingWrap();
    if (self.cur >= self.used) return;
    const w = self.cols;
    var start = self.col;
    var end = @min(self.col + @as(usize, n), w);
    if (start > 0 and self.cells[self.cur][start].cont != 0) start -= 1;
    if (end < w and self.cells[self.cur][end].cont != 0) end += 1;
    var i: usize = start;
    while (i < end) : (i += 1) self.blankCell(self.cur, i);
    if (start < self.lens[self.cur] and end >= self.lens[self.cur]) {
        self.lens[self.cur] = start;
    }
}

/// M80c: IL/DL and the explicit scroll commands operate inside the
/// active DECSTBM region. Rows outside it are stable, so a partial
/// region never turns into a full-grid redraw.
pub fn insertLines(self: *Screen, n: u16) void {
    if (self.cur < self.scroll_top or self.cur > self.scroll_bottom) return;
    if (self.cur >= self.used) return;
    self.cancelPendingWrap();
    self.prepareRowSlide();
    const before = self.lineCount();
    const cnt = @min(@as(usize, n), self.scroll_bottom - self.cur + 1);
    var i = self.scroll_bottom + 1;
    while (i > self.cur + cnt) {
        i -= 1;
        self.copyRow(i - cnt, i);
    }
    var row = self.cur;
    while (row < self.cur + cnt) : (row += 1) self.clearLine(row);
    self.col = 0;
    if (self.used <= self.scroll_bottom + 1) {
        self.used = @min(self.scroll_bottom + 1, self.used + cnt);
    }
    if (self.used <= self.cur) self.used = self.cur + 1;
    self.noteLineCountChange(before);
}

/// M80c: delete lines only within the active scrolling region.
pub fn deleteLines(self: *Screen, n: u16) void {
    if (self.cur < self.scroll_top or self.cur > self.scroll_bottom) return;
    if (self.cur >= self.used) return;
    self.cancelPendingWrap();
    self.prepareRowSlide();
    const before = self.lineCount();
    const m = @min(@as(usize, n), self.scroll_bottom - self.cur + 1);
    var i = self.cur;
    while (i + m <= self.scroll_bottom) : (i += 1) self.copyRow(i + m, i);
    var row = (self.scroll_bottom + 1) - m;
    while (row <= self.scroll_bottom) : (row += 1) self.clearLine(row);
    self.col = 0;
    if (self.used <= self.scroll_bottom + 1) {
        self.used = @max(1, @max(self.cur, self.used -| m));
    }
    self.noteLineCountChange(before);
}

pub fn swapAlternate(self: *Screen) void {
    var row: usize = 0;
    while (row < grid_lines) : (row += 1) {
        std.mem.swap([grid_cols]Cell, &self.cells[row], &self.alt_cells[row]);
        std.mem.swap([grid_cols]CellStyle, &self.styles[row], &self.alt_styles[row]);
    }
    std.mem.swap([grid_lines]usize, &self.lens, &self.alt_lens);
    std.mem.swap(usize, &self.used, &self.alt_used);
    std.mem.swap(usize, &self.cur, &self.alt_cur);
    std.mem.swap(usize, &self.col, &self.alt_col);
    std.mem.swap(usize, &self.view, &self.alt_view);
    std.mem.swap(CellStyle, &self.style, &self.alt_style);
    std.mem.swap(usize, &self.saved_cur, &self.alt_saved_cur);
    std.mem.swap(usize, &self.saved_col, &self.alt_saved_col);
    std.mem.swap(CellStyle, &self.saved_style, &self.alt_saved_style);
    std.mem.swap(bool, &self.saved_pending_wrap, &self.alt_saved_pending_wrap);
    std.mem.swap(Rgb, &self.saved_fg_rgb, &self.alt_saved_fg_rgb);
    std.mem.swap(Rgb, &self.saved_bg_rgb, &self.alt_saved_bg_rgb);
    std.mem.swap(Charset, &self.charset_g0, &self.alt_charset_g0);
    std.mem.swap(Charset, &self.charset_g1, &self.alt_charset_g1);
    std.mem.swap(u8, &self.charset_active, &self.alt_charset_active);
    std.mem.swap(bool, &self.saved_valid, &self.alt_saved_valid);
    std.mem.swap(usize, &self.scroll_top, &self.alt_scroll_top);
    std.mem.swap(usize, &self.scroll_bottom, &self.alt_scroll_bottom);
    std.mem.swap(bool, &self.origin_mode, &self.alt_origin_mode);
    std.mem.swap(bool, &self.pending_wrap, &self.alt_pending_wrap);
    std.mem.swap([grid_lines][grid_cols]Rgb, &self.fg_rgb, &self.alt_fg_rgb);
    std.mem.swap([grid_lines][grid_cols]Rgb, &self.bg_rgb, &self.alt_bg_rgb);
    std.mem.swap(Rgb, &self.fg_rgb_cur, &self.alt_fg_rgb_cur);
    std.mem.swap(Rgb, &self.bg_rgb_cur, &self.alt_bg_rgb_cur);
}

pub fn setAlternate(self: *Screen, enabled: bool) void {
    if (self.alt_active == enabled) return;
    // M73k: close a live search BEFORE the swap — its prompt row
    // lives in the primary grid, and an abort after the swap would
    // stash the prompt and ghost it on exit (pinned by test).
    if (self.search_active) self.searchExit();
    self.swapAlternate();
    self.alt_active = enabled;
    self.clearSelection();
    if (enabled) {
        // Unlike grid cells, tab tables are selected by alt_active and
        // do not need a physical swap: reset the alternate table when
        // it is armed while leaving the primary table untouched.
        self.alt_tab_stops = default_tab_stops;
        self.clearScreen();
        self.style = default_cell_style;
        self.fg_rgb_cur = empty_rgb;
        self.bg_rgb_cur = empty_rgb;
        self.clearSavedCursor();
        self.charset_g0 = .ascii;
        self.charset_g1 = .ascii;
        self.charset_active = 0;
        // DECSTBM/DECOM are per-screen. The newly armed alternate
        // starts with the full-screen defaults; the primary state is
        // already in the alternate storage after the swap above.
        self.scroll_top = 0;
        self.scroll_bottom = grid_lines - 1;
        self.origin_mode = false;
        self.pending_wrap = false;
    }
}

test "terminal: CSI EL and ED erase only their declared regions" {
    var s = Screen{};
    s.feed("abcdef\x1b[1;4H\x1b[K");
    try std.testing.expectEqualStrings("abc", s.line(0));

    s.feed("\x1b[2Jkeep");
    try std.testing.expectEqual(@as(usize, 1), s.lineCount());
    try std.testing.expectEqualStrings("keep", s.line(0));

    s.feed("\x1b[1;3H\x1b[1J");
    try std.testing.expectEqualStrings("   p", s.line(0));
}

test "terminal: alternate screen restores the primary grid and DECTCEM hides the cursor" {
    var s = Screen{};
    s.feed("primary");
    s.feed("\x1b[?1049halt\x1b[?25l");
    try std.testing.expect(s.alt_active);
    try std.testing.expectEqualStrings("alt", s.line(0));
    try std.testing.expect(!s.cursor_visible);

    s.feed("\x1b[?1049l");
    try std.testing.expect(!s.alt_active);
    try std.testing.expectEqualStrings("primary", s.line(0));
    s.feed("\x1b[?25h");
    try std.testing.expect(s.cursor_visible);
}

test "terminal: HTS sets the current stop and the alternate screen restores it (#1721)" {
    var s = Screen{};
    s.feed("\x1b[1;13H\x1bH");
    try std.testing.expect(s.hasTabStop(12));
    s.feed("\x1b[1;10H\tZ");
    try std.testing.expectEqualStrings("            Z", s.line(0));
    s.feed("\x1b[?1049h");
    try std.testing.expect(!s.hasTabStop(12));
    s.feed("\x1b[?1049l");
    try std.testing.expect(s.hasTabStop(12));
}

test "terminal: the alternate screen swap carries rune cells verbatim" {
    var s = Screen{};
    s.feed("e\xcc\x81");
    s.feed("\x1b[?1049h");
    s.feed("\xe4\xbd\xa0");
    try std.testing.expectEqual(@as(u21, 0x4F60), s.cellAt(0, 0).base);
    s.feed("\x1b[?1049l");
    try std.testing.expectEqual(@as(u21, 'e'), s.cellAt(0, 0).base);
    try std.testing.expectEqual(@as(u21, 0x301), s.cellAt(0, 0).mark);
    try std.testing.expectEqual(@as(usize, 1), s.cursorCol());
}
