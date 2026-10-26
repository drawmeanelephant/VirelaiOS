//! Resize reflow for `Screen` (split from terminal.zig, #3753): re-wrap the
//! grid and the history ring to a new column count, pairs never split.

const std = @import("std");
const text = @import("../text.zig");
const cell_mod = @import("cell.zig");
const registry = @import("../terminal.zig");
const screen_mod = @import("screen.zig");
const window_mod = @import("window.zig");
const Cell = cell_mod.Cell;
const CellStyle = cell_mod.CellStyle;
const Rgb = cell_mod.Rgb;
const empty_rgb = cell_mod.empty_rgb;
const create = registry.create;
const get = registry.get;
const HistoryBank = screen_mod.HistoryBank;
const Screen = screen_mod.Screen;
const grid_cols = screen_mod.grid_cols;
const grid_lines = screen_mod.grid_lines;
const histOf = screen_mod.histOf;
const history_lines = screen_mod.history_lines;
const screenForWindow = window_mod.screenForWindow;

/// The reflow snapshot (deliverable 3): one at a time, paint-path only.
var history_temp: HistoryBank = undefined;

// -- M49 SD5 (#1132): resize reflow -------------------------------------
/// The number of grid rows the cells of one logical line occupy at
/// `cols` columns — placement-aware (M73a-1): wide pairs never split,
/// so a pair that meets the last free column wraps early (at least one
/// row, even for an empty line).
pub fn wrappedRows(cells_in: []const Cell, cols: usize) usize {
    if (cells_in.len == 0) return 1;
    var rows: usize = 1;
    var col: usize = 0;
    for (cells_in) |cell| {
        if (cell.cont != 0) continue;
        if (cell.img != 0) {
            // M85b (#1814): an image cell never wraps — past the new
            // right margin it is dropped (`reflow` and `reflowHistory`
            // apply the same rule, so the row counts agree).
            if (col < cols) col += 1;
            continue;
        }
        const w: usize = if (text.char_width(cell.base) >= 2) 2 else 1;
        if (col + w > cols) {
            rows += 1;
            col = w;
        } else {
            col += w;
        }
    }
    return rows;
}

/// Reflow the stored lines to `new_cols` columns. The buffer is fixed;
/// when the wrapped result would overflow `grid_lines`, whole oldest
/// lines are dropped (the same policy as output scrolling). The cursor
/// follows the last kept line. Selection is cleared (its coordinates
/// were for the old layout).
pub fn reflow(self: *Screen, new_cols: usize) void {
    const c = @max(@as(usize, 8), @min(new_cols, grid_cols));
    if (c == self.cols) return;
    // M73k: close any live search FIRST — its overlay row must be
    // restored as the OLD content before rows re-lay out, and match
    // coordinates cannot survive a re-wrap (resize closes the bar).
    self.searchExit();

    // The oldest line that still fits, so the reflow drops from the top
    // exactly like new output would.
    var kept: usize = 0;
    var first: usize = self.used;
    while (first > 0) {
        const k = wrappedRows(self.cells[first - 1][0..self.lens[first - 1]], c);
        if (kept + k > grid_lines) break;
        kept += k;
        first -= 1;
    }

    // Snapshot the kept lines (module BSS scratch: the rune-cell grid
    // is ~80 KiB, too much for IRQ/Task stacks).
    var count: usize = 0;
    var i: usize = first;
    while (i < self.used) : (i += 1) {
        const len = @min(self.lens[i], grid_cols);
        @memcpy(reflow_lines[count][0..len], self.cells[i][0..len]);
        @memcpy(reflow_styles[count][0..len], self.styles[i][0..len]);
        @memcpy(reflow_fg_rgb[count][0..len], self.fg_rgb[i][0..len]);
        @memcpy(reflow_bg_rgb[count][0..len], self.bg_rgb[i][0..len]);
        reflow_lens[count] = len;
        count += 1;
    }
    var line_count: usize = 0;
    while (line_count < grid_lines) : (line_count += 1) self.clearLine(line_count);
    self.used = 1;
    self.cur = 0;
    self.col = 0;
    self.esc_state = 0;
    self.cols = c;
    self.view = 0;
    self.clearSelection();

    // Re-feed the kept logical lines at the new width.
    var n: usize = 0;
    while (n < count) : (n += 1) {
        var cell: usize = 0;
        while (cell < reflow_lens[n]) : (cell += 1) {
            const src = reflow_lines[n][cell];
            if (src.cont != 0) continue; // its base re-creates the pair
            // M85b (#1814): an image cell never wraps; at the new
            // right margin it is dropped (the `wrappedRows` rule).
            if (src.img != 0 and self.col >= self.cols) continue;
            const width = text.char_width(src.base);
            self.putRune(src.base, src.mark, reflow_styles[n][cell]);
            if (width > 0) {
                // putRune left the cursor past the glyph it just wrote
                // (after a wrap that is column `width`); M73h: carry the
                // cell's truecolour channels to the same landing spot.
                const landed = self.col - width;
                self.fg_rgb[self.cur][landed] = reflow_fg_rgb[n][cell];
                self.bg_rgb[self.cur][landed] = reflow_bg_rgb[n][cell];
                if (width == 2) {
                    self.fg_rgb[self.cur][landed + 1] = reflow_fg_rgb[n][cell];
                    self.bg_rgb[self.cur][landed + 1] = reflow_bg_rgb[n][cell];
                }
                // M85b: the tile rides with its blank rune.
                if (src.img != 0) self.cells[self.cur][landed] = src;
            }
        }
        if (n + 1 < count) self.newline();
    }
    self.reflowHistory(c);
}

/// M73k (#1637, deliverable 3): history re-wraps WITH the grid — the
/// same per-row `wrappedRows` policy and the same drop-oldest bound —
/// so grid and history can never disagree after a drag. A logical-
/// store/view-wrap alternative would break the paint walk's 1:1 row
/// mapping (that constraint is why this policy is THE policy). The
/// kept rows bounce through the temp bank because an EXPANDING
/// re-wrap cannot be done in place: on one ring it would overwrite
/// rows it has not read yet.
pub fn reflowHistory(self: *Screen, new_cols: usize) void {
    if (self.hist_count == 0) return;
    const bank = histOf(self) orelse {
        self.hist_count = 0;
        return;
    };
    // Pass 1 (read-only): newest-first, keep the longest suffix whose
    // re-wrapped row count fits the ring — drop-oldest, like the grid.
    var out_rows: usize = 0;
    var hfirst: usize = self.hist_count;
    while (hfirst > 0) {
        const idx = hfirst - 1;
        const slot = (self.hist_start + idx) % history_lines;
        const k = wrappedRows(bank.cells[slot][0..bank.lens[slot]], new_cols);
        if (out_rows + k > history_lines) break;
        out_rows += k;
        hfirst -= 1;
    }
    // Pass 2: snapshot the kept rows (forward) into the temp bank.
    var m: usize = 0;
    var i = hfirst;
    while (i < self.hist_count) : (i += 1) {
        const slot = (self.hist_start + i) % history_lines;
        const len = bank.lens[slot];
        @memcpy(history_temp.cells[m][0..len], bank.cells[slot][0..len]);
        @memcpy(history_temp.styles[m][0..len], bank.styles[slot][0..len]);
        history_temp.lens[m] = len;
        m += 1;
    }
    // Pass 3: rebuild the ring at the new width. Direct cell
    // placement (no decode): wide pairs stay together, a wrap that
    // would split one lands early — the same rule as `wrappedRows`.
    self.hist_count = 0;
    self.hist_start = 0;
    self.hist_dropped += hfirst;
    var t: usize = 0;
    while (t < m) : (t += 1) {
        const len = history_temp.lens[t];
        var oc: usize = 0; // output column on the current row
        var ci: usize = 0;
        while (ci < len) : (ci += 1) {
            const src = history_temp.cells[t][ci];
            if (src.cont != 0) continue; // its base already placed the pair
            if (src.img != 0 and oc >= new_cols) continue; // M85b: never wraps
            const w: usize = if (text.char_width(src.base) >= 2) 2 else 1;
            if (oc + w > new_cols and oc > 0) {
                self.closeHistRow(bank, oc);
                oc = 0;
            }
            const slot = self.hist_count; // hist_start = 0 during rebuild
            bank.cells[slot][oc] = src;
            bank.styles[slot][oc] = history_temp.styles[t][ci];
            if (w == 2 and ci + 1 < len) {
                bank.cells[slot][oc + 1] = history_temp.cells[t][ci + 1];
                bank.styles[slot][oc + 1] = history_temp.styles[t][ci + 1];
            }
            oc += w;
        }
        self.closeHistRow(bank, oc); // every input row ends one output row
    }
}

/// Finish the ring row currently being rebuilt (`hist_count` grows
/// only when a row is complete, so a partial row is never visible).
pub fn closeHistRow(self: *Screen, bank: *HistoryBank, len: usize) void {
    bank.lens[self.hist_count] = len;
    self.hist_count += 1;
}

/// Set the effective column count, reflowing when it changes. Returns
/// the effective value.
pub fn setCols(self: *Screen, new_cols: usize) usize {
    const c = @max(@as(usize, 8), @min(new_cols, grid_cols));
    if (c != self.cols) self.reflow(c);
    self.maskTabStops();
    return self.cols;
}

/// M49 SD5: reflow scratch (module BSS — the grid is too large for the task
/// stacks). One reflow at a time (the paint/idle path), documented bound.
var reflow_lines: [grid_lines][grid_cols]Cell = undefined;

var reflow_styles: [grid_lines][grid_cols]CellStyle = undefined;

/// M73h: reflow scratch for the truecolour side arrays (module BSS).
var reflow_fg_rgb: [grid_lines][grid_cols]Rgb = undefined;

var reflow_bg_rgb: [grid_lines][grid_cols]Rgb = undefined;

var reflow_lens: [grid_lines]usize = undefined;

// ---------------------------------------------------------------------------
// M49 SD5 (#1132): reflow, scrollback view, selection/copy tests
// ---------------------------------------------------------------------------
test "terminal: setCols reflows long lines to the narrower width" {
    var s = Screen{};
    s.feed("abcdefghij\nxy");
    try std.testing.expectEqual(@as(usize, 2), s.lineCount());
    _ = s.setCols(4); // clamped to the 8-column floor
    try std.testing.expectEqual(@as(usize, 8), s.cols);
    try std.testing.expectEqual(@as(usize, 3), s.lineCount());
    try std.testing.expectEqualStrings("abcdefgh", s.line(0));
    try std.testing.expectEqualStrings("ij", s.line(1));
    try std.testing.expectEqualStrings("xy", s.line(2));
    // The cursor follows the last line.
    try std.testing.expectEqual(@as(usize, 2), s.cursorLine());
    try std.testing.expectEqual(@as(usize, 2), s.cursorCol());
    // New output now wraps at the new width.
    s.feed("z12345");
    try std.testing.expectEqual(@as(usize, 3), s.lineCount());
    try std.testing.expectEqualStrings("xyz12345", s.line(2));
    s.feed("9");
    try std.testing.expectEqual(@as(usize, 4), s.lineCount());
    try std.testing.expectEqualStrings("9", s.line(3));
}

test "terminal: reflow overflow drops whole oldest lines" {
    var s = Screen{};
    var i: usize = 0;
    while (i < grid_lines) : (i += 1) s.feed("0123456789\n");
    try std.testing.expectEqual(@as(usize, grid_lines), s.lineCount());
    // Each 10-byte line wraps to 2 rows at 8 columns: only half the content
    // lines fit, and the old trailing empty line survives (127 rows).
    _ = s.setCols(8);
    try std.testing.expectEqual(@as(usize, 8), s.cols);
    try std.testing.expectEqual(@as(usize, 127), s.lineCount());
    // The newest lines survived; the oldest are gone.
    try std.testing.expectEqualStrings("01234567", s.line(0));
    try std.testing.expectEqualStrings("89", s.line(1));
    try std.testing.expectEqualStrings("01234567", s.line(124));
    try std.testing.expectEqualStrings("89", s.line(125));
}

test "terminal: setCols to the same value is a no-op; growth keeps lines" {
    var s = Screen{};
    s.feed("hello\nworld");
    _ = s.setCols(40);
    try std.testing.expectEqual(@as(usize, 40), s.cols);
    try std.testing.expectEqualStrings("hello", s.line(0));
    try std.testing.expectEqualStrings("world", s.line(1));
    _ = s.setCols(80);
    try std.testing.expectEqual(@as(usize, 2), s.lineCount());
    try std.testing.expectEqualStrings("world", s.line(1));
}

test "terminal: reflow re-wraps history with the grid, never disagreeing (#1637)" {
    for (&registry.terminals) |*t| t.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));
    const s = screenForWindow(7).?;
    // Long rows fill the grid and spill a batch into history.
    var i: usize = 0;
    while (i < 150) : (i += 1) {
        var b: [96]u8 = undefined;
        @memset(&b, 'x');
        const row = std.fmt.bufPrint(&b, "row{d:0>3}" ++ &@as([72:0]u8, @splat("x"[0])), .{i}) catch unreachable;
        s.feed(row);
        s.feed("\n");
    }
    try std.testing.expect(s.hist_count > 0);
    // Shrink 80 -> 40: EVERY row, history and grid alike, must re-lay to
    // <= 40 cells — that is the whole point of the shared policy.
    _ = s.setCols(40);
    try std.testing.expectEqual(@as(usize, 40), s.cols);
    try std.testing.expect(s.lineCount() <= history_lines + grid_lines);
    i = 0;
    while (i < s.lineCount()) : (i += 1) {
        const n = s.uLen(i) orelse continue;
        try std.testing.expect(n <= 40);
    }
    // Back up: still consistent, still bounded.
    _ = s.setCols(80);
    try std.testing.expect(s.lineCount() <= history_lines + grid_lines);
    i = 0;
    while (i < s.lineCount()) : (i += 1) {
        const n = s.uLen(i) orelse continue;
        try std.testing.expect(n <= 80);
    }
}

test "terminal: reflow keeps wide pairs intact at the new width" {
    var s = Screen{};
    s.feed("abcdefghi\xe4\xbd\xa0"); // 9 narrow + wide pair
    _ = s.setCols(8);
    try std.testing.expectEqual(@as(usize, 8), s.cols);
    try std.testing.expectEqual(@as(usize, 2), s.lineCount());
    try std.testing.expectEqualStrings("abcdefgh", s.line(0));
    try std.testing.expectEqual(@as(u21, 'i'), s.cellAt(1, 0).base);
    try std.testing.expectEqual(@as(u21, 0x4F60), s.cellAt(1, 1).base);
    try std.testing.expectEqual(@as(u1, 1), s.cellAt(1, 2).cont);
    try std.testing.expectEqual(@as(usize, 3), s.line(1).len);
    // Pair invariant across the whole grid: every continuation has a wide
    // base immediately to its left.
    var row: usize = 0;
    while (row < s.used) : (row += 1) {
        var c: usize = 0;
        while (c < grid_cols) : (c += 1) {
            const cell = s.cellAt(row, c);
            if (cell.cont == 0) continue;
            try std.testing.expect(c > 0);
            try std.testing.expect(text.char_width(s.cellAt(row, c - 1).base) >= 2);
        }
    }
}

test "terminal: truecolour rides a resize reflow" {
    var s = Screen{};
    s.feed("\x1b[38;2;9;8;7m");
    s.feed("aaaaaaaaaaaaaaaaaaaaaaaaaa"); // 26 cells, all rgb-marked
    _ = s.setCols(8); // reflow: rows of 8 — most cells change row
    try std.testing.expectEqual(@as(u21, 'a'), s.cellAt(1, 1).base);
    const fg = s.rgbAt(1, 1).fg.?; // landed mid-row after the re-feed
    try std.testing.expectEqual(@as(u8, 9), fg.r);
    try std.testing.expectEqual(@as(u8, 8), fg.g);
    try std.testing.expectEqual(@as(u8, 7), fg.b);
    // Erasing zeroes the side arrays, not just the slot.
    s.feed("\x1b[1;1H\x1b[2K");
    try std.testing.expectEqual(empty_rgb, s.fg_rgb[0][0]);
    try std.testing.expectEqual(@as(?Rgb, null), s.rgbAt(0, 0).fg);
}
