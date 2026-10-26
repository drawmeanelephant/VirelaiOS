//! Scrollback search overlay for `Screen` (split from terminal.zig, #3753):
//! the `/` prompt, rune-aware matching, hit highlighting and the stashed row.

const std = @import("std");
const cell_mod = @import("cell.zig");
const screen_mod = @import("screen.zig");
const Cell = cell_mod.Cell;
const CellStyle = cell_mod.CellStyle;
const Point = cell_mod.Point;
const default_cell_style = cell_mod.default_cell_style;
const empty_cell = cell_mod.empty_cell;
const empty_rgb = cell_mod.empty_rgb;
const Screen = screen_mod.Screen;
const SearchKey = Screen.SearchKey;
const grid_cols = screen_mod.grid_cols;
const grid_lines = screen_mod.grid_lines;
const histOf = screen_mod.histOf;
const history_lines = screen_mod.history_lines;

pub fn searchActive(self: *const Screen) bool {
    return self.search_active;
}

/// Open the bar: NORMAL screen only (alt returns false and changes
/// nothing — pinned). The user's selection is saved (search owns
/// `sel_*` for the current-match highlight until exit), and the view
/// lifts off the tail so live output never rewrites the overlay row
/// (single-line screens cannot lift — documented edge).
pub fn searchOpen(self: *Screen) bool {
    if (self.alt_active) return false;
    if (self.search_active) return true;
    self.saved_sel_anchor = self.sel_anchor;
    self.saved_sel_cursor = self.sel_cursor;
    self.clearSelection();
    self.search_active = true;
    self.search_len = 0;
    self.search_count = 0;
    self.search_has_cur = false;
    if (self.view == 0 and self.lineCount() > 1) self.view = 1;
    self.searchDrawPrompt(); // positions + saves the overlay row too
    return true;
}

/// Feed one key. Returns the match count to klog (`null` = nothing
/// changed — nav, esc, empty backspace; the `tty: copy`-style klog
/// mirror in input.zig fires only on a pattern change, and the empty
/// pattern is a no-op).
pub fn searchFeed(self: *Screen, key: SearchKey) ?usize {
    if (!self.search_active) return null;
    switch (key) {
        .esc => {
            self.searchExit();
            return null;
        },
        .enter => {
            if (self.search_count > 0) self.searchStep(1);
            return null;
        },
        .prev => {
            if (self.search_count > 0) self.searchStep(-1);
            return null;
        },
        .backspace => {
            if (self.search_len == 0) return null;
            self.search_len -= 1;
            self.searchRecompute();
            self.searchDrawPrompt();
            return if (self.search_len > 0) self.search_count else null;
        },
        .ch => |c| {
            if (self.search_len >= self.search_pat.len) return null;
            self.search_pat[self.search_len] = c;
            self.search_len += 1;
            self.searchRecompute();
            self.searchDrawPrompt();
            return self.search_count;
        },
    }
}

/// Exit (Esc): the overlay row and the user's selection come back,
/// state clears — and the VIEW STAYS where the match put it (the
/// card's "exits and restores the view at the match").
pub fn searchExit(self: *Screen) void {
    if (!self.search_active) return;
    self.searchRestoreOverlay();
    self.sel_anchor = self.saved_sel_anchor;
    self.sel_cursor = self.saved_sel_cursor;
    self.searchAbort();
}

/// Abort without writing anything back (clearScreen): the row content
/// is about to vanish, so restoring it later would resurrect stale
/// pre-clear bytes.
pub fn searchAbort(self: *Screen) void {
    self.search_active = false;
    self.search_len = 0;
    self.search_count = 0;
    self.search_has_cur = false;
    self.ov_valid = false;
    self.saved_sel_anchor = null;
    self.saved_sel_cursor = null;
}

// Row-local literal matcher: ASCII-case-insensitive, everything else
// exact. Rune-aware by construction — cells hold runes, the pattern
// holds runes, an accented match compares codepoint to codepoint.
// Matches NEVER cross row boundaries (v1, pinned): each row is its
// own haystack.
pub fn foldAscii(c: u21) u21 {
    return if (c >= 'A' and c <= 'Z') c + 32 else c;
}

pub fn matchAt(row_cells: []const Cell, row_len: usize, col: usize, pat: []const u21) bool {
    if (col + pat.len > row_len) return false;
    var j: usize = 0;
    while (j < pat.len) : (j += 1) {
        if (foldAscii(row_cells[col + j].base) != foldAscii(pat[j])) return false;
    }
    return true;
}

/// M73k: the unified row the prompt bar currently covers — from the
/// STASH (absolute across ring rotation), not from `view`: during a
/// scan the bar still sits where the last draw left it.
pub fn stashedRow(self: *const Screen) ?usize {
    if (!self.ov_valid) return null;
    if (self.ov_abs < self.hist_dropped) return null;
    return @intCast(self.ov_abs - self.hist_dropped);
}

/// Scan-time row view: the covered row yields its STASHED original
/// content — the live cells hold prompt chrome, which is neither the
/// user's data nor searchable (the count must see the real row under
/// the bar, and must never self-match the pattern the bar draws).
pub fn scanCells(self: *const Screen, row: usize) ?[]const Cell {
    if (self.stashedRow()) |u| {
        if (row == u) return self.ov_cells[0..];
    }
    return self.uCells(row);
}

pub fn scanLen(self: *const Screen, row: usize) ?usize {
    if (self.stashedRow()) |u| {
        if (row == u) return self.ov_lens;
    }
    return self.uLen(row);
}

/// Rescan the whole space: count every row-local occurrence, park the
/// current match on the FIRST (top-anchored incremental search), move
/// the view so it sits right above the prompt bar, and expose it via
/// the selection range so the existing inversion paint highlights it.
pub fn searchRecompute(self: *Screen) void {
    self.search_count = 0;
    self.search_has_cur = false;
    self.clearSelection();
    if (self.search_len == 0) return; // empty pattern = no-op
    const pat = self.search_pat[0..self.search_len];
    const total = self.lineCount();
    var row: usize = 0;
    var first: ?Point = null;
    while (row < total) : (row += 1) {
        const row_cells = self.scanCells(row) orelse continue;
        const row_len = self.scanLen(row) orelse continue;
        var c: usize = 0;
        while (c + pat.len <= row_len) : (c += 1) {
            if (matchAt(row_cells, row_len, c, pat)) {
                self.search_count += 1;
                if (first == null) first = .{ .line = row, .col = c };
                c += pat.len - 1; // non-overlapping occurrences
            }
        }
    }
    if (first) |f| {
        self.search_row = f.line;
        self.search_col = f.col;
        self.search_has_cur = true;
        self.searchHighlight();
        self.searchScrollTo(f.line);
    }
}

/// Next/prev with wrap-around — a linear walk over candidate start
/// positions (rows never contribute cross-row candidates).
pub fn searchStep(self: *Screen, dir: i32) void {
    if (!self.search_has_cur or self.search_len == 0) return;
    const pat = self.search_pat[0..self.search_len];
    const total = self.lineCount();
    if (total == 0) return;
    var row: usize = self.search_row;
    var col: usize = self.search_col;
    var guard: usize = 0;
    const guard_max = (history_lines + grid_lines) * grid_cols;
    while (guard < guard_max) : (guard += 1) {
        if (dir > 0) {
            col += 1;
            const rl = self.scanLen(row) orelse continue;
            if (col + pat.len > rl) {
                row += 1;
                if (row >= total) row = 0;
                col = 0;
            }
        } else {
            if (col == 0) {
                row = if (row == 0) total - 1 else row - 1;
                const rl = self.scanLen(row) orelse continue;
                col = if (rl >= pat.len) rl - pat.len else 0;
            } else {
                col -= 1;
            }
        }
        const row_cells = self.scanCells(row) orelse continue;
        const row_len = self.scanLen(row) orelse continue;
        if (col + pat.len > row_len) continue;
        if (matchAt(row_cells, row_len, col, pat)) {
            self.search_row = row;
            self.search_col = col;
            self.searchHighlight();
            self.searchScrollTo(row);
            return;
        }
    }
}

/// The current match IS the selection range while search is up —
/// `inSelection` treats the end column as inclusive, so the cursor
/// parks on the last cell of the match.
pub fn searchHighlight(self: *Screen) void {
    if (!self.search_has_cur) return;
    self.sel_anchor = .{ .line = self.search_row, .col = self.search_col };
    self.sel_cursor = .{
        .line = self.search_row,
        .col = self.search_col + self.search_len - 1,
    };
}

/// Park the match two rows above the window bottom — the row directly
/// above the prompt bar — without knowing the window's height (the
/// painter derives `first = lineCount - view - rows` from view alone).
pub fn searchScrollTo(self: *Screen, row: usize) void {
    const total = self.lineCount();
    var v: i64 = @as(i64, @intCast(total)) - @as(i64, @intCast(row)) - 2;
    if (v < 0) v = 0;
    const max_v: i64 = if (total > 0) @as(i64, @intCast(total - 1)) else 0;
    if (v > max_v) v = max_v;
    self.view = @intCast(v);
}

/// The bar's row: one above the window bottom (absolute-indexed via
/// `hist_dropped` so ring rotation and grid scroll cannot lose it).
pub fn searchOverlayRow(self: *const Screen) usize {
    const total = self.lineCount();
    const v = @min(self.view, total - 1);
    return total - v - 1;
}

/// Restore whatever row the bar currently covers, then save the row
/// about to be covered — so as the view jumps between matches the
/// saved content FOLLOWS the bar (no prompt ghosts, no lost rows).
pub fn searchPositionOverlay(self: *Screen) void {
    self.searchRestoreOverlay();
    const u = self.searchOverlayRow();
    const row_cells = self.uCells(u) orelse return;
    const row_styles = self.uStyles(u) orelse return;
    @memcpy(self.ov_cells[0..], row_cells[0..]);
    @memcpy(self.ov_styles[0..], row_styles[0..]);
    self.ov_lens = self.uLen(u) orelse 0;
    if (!self.alt_active and u >= self.hist_count and (u - self.hist_count) < self.used) {
        const gr = u - self.hist_count;
        @memcpy(self.ov_fg[0..], self.fg_rgb[gr][0..]);
        @memcpy(self.ov_bg[0..], self.bg_rgb[gr][0..]);
    } else {
        @memset(&self.ov_fg, empty_rgb);
        @memset(&self.ov_bg, empty_rgb);
    }
    self.ov_abs = u + self.hist_dropped;
    self.ov_valid = true;
}

pub fn searchRestoreOverlay(self: *Screen) void {
    if (!self.ov_valid) return;
    self.ov_valid = false;
    if (self.ov_abs < self.hist_dropped) return; // the line was evicted
    const u: usize = @intCast(self.ov_abs - self.hist_dropped);
    if (self.alt_active) {
        if (u >= self.used) return;
        @memcpy(self.cells[u][0..], self.ov_cells[0..]);
        @memcpy(self.styles[u][0..], self.ov_styles[0..]);
        @memcpy(self.fg_rgb[u][0..], self.ov_fg[0..]);
        @memcpy(self.bg_rgb[u][0..], self.ov_bg[0..]);
        self.lens[u] = self.ov_lens;
        return;
    }
    if (u < self.hist_count) {
        const bank = histOf(self) orelse return;
        const slot = (self.hist_start + u) % history_lines;
        @memcpy(bank.cells[slot][0..], self.ov_cells[0..]);
        @memcpy(bank.styles[slot][0..], self.ov_styles[0..]);
        bank.lens[slot] = self.ov_lens;
        return;
    }
    const gr = u - self.hist_count;
    if (gr >= self.used) return;
    @memcpy(self.cells[gr][0..], self.ov_cells[0..]);
    @memcpy(self.styles[gr][0..], self.ov_styles[0..]);
    @memcpy(self.fg_rgb[gr][0..], self.ov_fg[0..]);
    @memcpy(self.bg_rgb[gr][0..], self.ov_bg[0..]);
    self.lens[gr] = self.ov_lens;
}

/// Draw the prompt bar into the overlay row: `> pattern  (N)` or
/// `> pattern  no match` (the hint line — never an error), the whole
/// row reversed so the EXISTING inversion paint renders the bar with
/// zero painter changes.
pub fn searchDrawPrompt(self: *Screen) void {
    if (self.alt_active) return;
    self.searchPositionOverlay();
    const u = self.searchOverlayRow();
    var tgt_cells: *[grid_cols]Cell = undefined;
    var tgt_styles: *[grid_cols]CellStyle = undefined;
    var tgt_len: *usize = undefined;
    if (u < self.hist_count) {
        const bank = histOf(self) orelse return;
        const slot = (self.hist_start + u) % history_lines;
        tgt_cells = &bank.cells[slot];
        tgt_styles = &bank.styles[slot];
        tgt_len = &bank.lens[slot];
    } else {
        const gr = u - self.hist_count;
        if (gr >= self.used) return;
        tgt_cells = &self.cells[gr];
        tgt_styles = &self.styles[gr];
        tgt_len = &self.lens[gr];
    }
    const cols_n = self.cols;
    var x: usize = 0;
    const put = struct {
        fn run(cells: *[grid_cols]Cell, pos: *usize, cols_n_: usize, ch: u21) void {
            if (pos.* < cols_n_) {
                cells[pos.*] = .{ .base = ch, .mark = 0, .cont = 0 };
                pos.* += 1;
            }
        }
    };
    put.run(tgt_cells, &x, cols_n, '>');
    put.run(tgt_cells, &x, cols_n, ' ');
    for (self.search_pat[0..self.search_len]) |ch| put.run(tgt_cells, &x, cols_n, ch);
    if (self.search_len > 0) {
        if (self.search_count == 0) {
            const hint = "  no match";
            for (hint) |ch| put.run(tgt_cells, &x, cols_n, ch);
        } else {
            var num: [8]u8 = undefined;
            const s = std.fmt.bufPrint(&num, "  ({d})", .{self.search_count}) catch "";
            for (s) |ch| put.run(tgt_cells, &x, cols_n, ch);
        }
    }
    while (x < cols_n) put.run(tgt_cells, &x, cols_n, ' ');
    for (0..cols_n) |c| {
        tgt_styles.*[c] = default_cell_style;
        tgt_styles.*[c].reverse = true;
    }
    for (cols_n..grid_cols) |c| {
        tgt_cells.*[c] = empty_cell;
        tgt_styles.*[c] = default_cell_style;
    }
    tgt_len.* = cols_n;
}

test "terminal: search table — literal, case, rows, wrap, empty, alt (#1637)" {
    var s = Screen{};
    s.feed("top a\nb bottom\nthe Needle in HAY\nneedle rows\nnone here\n");
    // Alt screen: the chord refuses — search/history are normal-screen only.
    s.feed("\x1b[?1049h");
    try std.testing.expect(!s.searchOpen());
    try std.testing.expect(!s.searchActive());
    s.feed("\x1b[?1049l");
    // Open: empty pattern is a no-op (count 0, backspace at 0 = null).
    try std.testing.expect(s.searchOpen());
    try std.testing.expect(s.searchActive());
    try std.testing.expectEqual(@as(usize, 0), s.search_count);
    try std.testing.expect(s.searchFeed(.backspace) == null);
    // Literal + ASCII-case-insensitive: "needle" hits Needle and needle
    // ("none here" does not), first match parked, exposed as selection.
    var n: ?usize = null;
    for ("needle") |ch| n = s.searchFeed(.{ .ch = ch });
    try std.testing.expectEqual(@as(usize, 2), n.?);
    // First match is row 2 ("the Needle in HAY"), col 4; row 3
    // ("needle rows") is the second — rows 0/1 carry no match.
    try std.testing.expectEqual(@as(usize, 2), s.search_row);
    try std.testing.expectEqual(@as(usize, 4), s.search_col);
    try std.testing.expect(s.inSelection(2, 4));
    try std.testing.expect(s.inSelection(2, 9)); // end col inclusive
    try std.testing.expect(!s.inSelection(2, 10));
    // Enter/prev walk with wrap-around (the bar's Shift+Enter sibling).
    s.searchStep(1);
    try std.testing.expectEqual(@as(usize, 3), s.search_row); // "needle rows"
    try std.testing.expectEqual(@as(usize, 0), s.search_col);
    s.searchStep(1);
    try std.testing.expectEqual(@as(usize, 2), s.search_row); // wrapped
    try std.testing.expectEqual(@as(usize, 4), s.search_col);
    s.searchStep(-1);
    try std.testing.expectEqual(@as(usize, 3), s.search_row); // wrapped back
    // Matches do NOT cross row boundaries (v1): row 0 ends 'a', row 1
    // starts 'b' — the pair "ab" exists only ACROSS the break.
    var i: usize = 0;
    while (i < 6) : (i += 1) _ = s.searchFeed(.backspace);
    try std.testing.expect(s.searchFeed(.backspace) == null); // empty now
    _ = s.searchFeed(.{ .ch = 'a' });
    try std.testing.expectEqual(@as(usize, 0), s.searchFeed(.{ .ch = 'b' }).?);
    try std.testing.expect(s.searchActive()); // no-match = hint, not error
    try std.testing.expect(!s.search_has_cur); // nothing to highlight
    _ = s.searchFeed(.esc);
    try std.testing.expect(!s.searchActive());
    // Alt switch with a LIVE search: the prompt must not ghost into the
    // stashed primary grid (search closes cleanly before the swap).
    try std.testing.expect(s.searchOpen());
    _ = s.searchFeed(.{ .ch = 'a' });
    try std.testing.expect(s.searchActive());
    s.feed("\x1b[?1049h");
    try std.testing.expect(!s.searchActive());
    s.feed("\x1b[?1049l");
    try std.testing.expect(!s.searchActive());
    try std.testing.expectEqualStrings("top a", s.line(0));
    try std.testing.expectEqualStrings("needle rows", s.line(3));
}

test "terminal: search overlay + user selection restore exactly (#1637)" {
    var s = Screen{};
    s.feed("row one\nrow two\nrow three\n");
    s.beginSelection(1, 0);
    s.extendSelection(1, 4);
    try std.testing.expect(s.searchOpen()); // lifts view 0 -> 1
    try std.testing.expectEqual(@as(usize, 1), s.viewOffset());
    // The bar sits on the overlay row, drawn reversed through the grid.
    const bar_u = s.searchOverlayRow();
    try std.testing.expect(s.styleAt(bar_u, 0).reverse);
    _ = s.searchFeed(.{ .ch = 'x' }); // no match here — bar shows hint
    _ = s.searchFeed(.esc);
    // The covered row's original content came back byte-for-byte… (the
    // bar sits at total-view-1 = row 2 = "row three", proven by the
    // reversed styleAt assert above).
    try std.testing.expectEqualStrings("row three", s.line(bar_u));
    // …the user's selection is restored…
    try std.testing.expect(s.hasSelection());
    try std.testing.expect(s.sel_anchor.?.line == 1 and s.sel_anchor.?.col == 0);
    try std.testing.expect(s.sel_cursor.?.line == 1 and s.sel_cursor.?.col == 4);
    // …and the view stays where the search left it ("at the match").
    try std.testing.expectEqual(@as(usize, 1), s.viewOffset());
}

test "terminal: search is rune-aware — accented match, non-ASCII exact (#1637)" {
    var s = Screen{};
    s.feed("caf\u{e9} latte\n");
    try std.testing.expect(s.searchOpen());
    try std.testing.expectEqual(@as(usize, 1), s.searchFeed(.{ .ch = '\u{e9}' }).?);
    try std.testing.expectEqual(@as(usize, 0), s.search_row);
    try std.testing.expectEqual(@as(usize, 3), s.search_col);
    _ = s.searchFeed(.esc);
    // Case policy pin: folding is ASCII-only — \u{c9} does NOT match \u{e9}.
    try std.testing.expect(s.searchOpen());
    try std.testing.expectEqual(@as(usize, 0), s.searchFeed(.{ .ch = '\u{c9}' }).?);
    _ = s.searchFeed(.esc);
}
