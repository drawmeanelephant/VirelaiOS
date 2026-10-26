//! Scrolling and history ops for `Screen` (split from terminal.zig, #3753):
//! LF/IND/RI, DECSTBM regions, SU/SD, the scrollback ring and viewport clamps.

const std = @import("std");
const cell_mod = @import("cell.zig");
const registry = @import("../terminal.zig");
const screen_mod = @import("screen.zig");
const window_mod = @import("window.zig");
const CellStyle = cell_mod.CellStyle;
const default_cell_style = cell_mod.default_cell_style;
const default_colour = cell_mod.default_colour;
const empty_cell = cell_mod.empty_cell;
const empty_rgb = cell_mod.empty_rgb;
const rgb_colour = cell_mod.rgb_colour;
const create = registry.create;
const get = registry.get;
const Screen = screen_mod.Screen;
const grid_cols = screen_mod.grid_cols;
const grid_lines = screen_mod.grid_lines;
const histOf = screen_mod.histOf;
const history_lines = screen_mod.history_lines;
const screenForWindow = window_mod.screenForWindow;

/// M80e: IND moves down without the carriage return performed by LF.
/// M80c: the active scroll region is honored at its bottom edge.
pub fn index(self: *Screen) void {
    self.pending_wrap = false;
    if (self.cur < self.scroll_bottom) {
        self.cur += 1;
        if (self.cur >= self.used) {
            self.clearLine(self.cur);
            self.used = self.cur + 1;
            self.noteTailGrowth();
        }
    } else if (self.cur == self.scroll_bottom) {
        self.scrollRegionUp(1);
        if (self.used <= self.cur) self.used = self.cur + 1;
    } else if (self.cur + 1 < grid_lines) {
        self.cur += 1;
        if (self.cur >= self.used) {
            self.clearLine(self.cur);
            self.used = self.cur + 1;
            self.noteTailGrowth();
        }
    } else {
        self.scrollFullUp(1);
    }
}

/// M80e: RI is the reverse of IND, including the top-edge scroll.
/// M80c: the active scroll region is honored at its top edge.
pub fn reverseIndex(self: *Screen) void {
    self.pending_wrap = false;
    if (self.cur > self.scroll_top) {
        self.cur -= 1;
    } else if (self.cur == self.scroll_top) {
        self.scrollRegionDown(1);
    } else if (self.cur > 0) {
        self.cur -= 1;
    }
}

pub fn nextLine(self: *Screen) void {
    self.col = 0;
    self.index();
}

pub fn clearLine(self: *Screen, i: usize) void {
    @memset(&self.cells[i], empty_cell);
    @memset(&self.styles[i], default_cell_style);
    @memset(&self.fg_rgb[i], empty_rgb);
    @memset(&self.bg_rgb[i], empty_rgb);
    self.lens[i] = 0;
}

/// M80c: move down one line for LF. The active DECSTBM region owns
/// the scroll when the cursor is at its bottom; outside the region
/// the full grid still scrolls. A top-edge eviction is the only path
/// into scrollback, and it happens only when the region starts at row
/// zero on the primary screen.
pub fn newline(self: *Screen) void {
    self.pending_wrap = false;
    if (self.cur < self.scroll_bottom) {
        self.cur += 1;
        if (self.cur >= self.used) {
            self.clearLine(self.cur);
            self.used = self.cur + 1;
            self.noteTailGrowth();
        }
    } else if (self.cur == self.scroll_bottom) {
        self.scrollRegionUp(1);
        if (self.used <= self.cur) self.used = self.cur + 1;
    } else if (self.cur + 1 < grid_lines) {
        self.cur += 1;
        if (self.cur >= self.used) {
            self.clearLine(self.cur);
            self.used = self.cur + 1;
            self.noteTailGrowth();
        }
    } else {
        self.scrollFullUp(1);
    }
    self.col = 0;
}

/// Scroll the full grid upward, retaining the historical M73k
/// policy. This is the fallback for a cursor below a non-full DECSTBM
/// region; region-internal scrolling uses `scrollRegionUp` instead.
pub fn scrollFullUp(self: *Screen, n: usize) void {
    const count = @min(n, self.used + history_lines);
    var i: usize = 0;
    while (i < count) : (i += 1) {
        self.pushHistory();
        var row: usize = 0;
        while (row + 1 < grid_lines) : (row += 1) self.copyRow(row + 1, row);
        self.clearLine(grid_lines - 1);
    }
    // A cursor at the physical bottom makes the full grid the active
    // line span even when it had only been motion-materialized.
    self.used = grid_lines;
    self.noteTailGrowth();
}

/// M73k: the tail grew (line pushed to history, or a new grid row
/// materialised). While the reader is scrolled back the window's
/// BOTTOM must stay put: view counts from the tail, so bumping it by
/// the growth keeps `lineCount - view` — and the absolute identity
/// behind every unified row — exactly where it was. At view 0
/// nothing changes (the tail follows, as ever).
pub fn noteTailGrowth(self: *Screen) void {
    if (self.view > 0) {
        self.view += 1;
        // Ring-full pushes do not grow lineCount: clamp so a pinned
        // view can never outrange the stored space (bottom >= 0).
        const max_view = self.lineCount() - 1;
        if (self.view > max_view) self.view = max_view;
    }
}

/// M73k: move the grid's evicted oldest row into this screen's ring.
/// Registry screens only (a local Screen has no bank and drops like
/// before — existing tests unchanged). RGB side arrays are coerced to
/// the presentation default: history does not store them (budget),
/// and a stale `rgb_colour` slot with no side data would paint black.
pub fn pushHistory(self: *Screen) void {
    if (self.alt_active) return; // history belongs to the normal screen
    const bank = histOf(self) orelse return;
    var styles_row: [grid_cols]CellStyle = undefined;
    for (0..grid_cols) |c| {
        var st = self.styles[0][c];
        if (st.fg == rgb_colour) st.fg = default_colour;
        if (st.bg == rgb_colour) st.bg = default_colour;
        styles_row[c] = st;
    }
    if (self.hist_count < history_lines) {
        const slot = (self.hist_start + self.hist_count) % history_lines;
        @memcpy(bank.cells[slot][0..], self.cells[0][0..]);
        @memcpy(bank.styles[slot][0..], styles_row[0..]);
        bank.lens[slot] = self.lens[0];
        self.hist_count += 1;
    } else {
        // Ring full: overwrite the oldest slot and rotate — the same
        // drop-oldest policy the grid itself has always used.
        @memcpy(bank.cells[self.hist_start][0..], self.cells[0][0..]);
        @memcpy(bank.styles[self.hist_start][0..], styles_row[0..]);
        bank.lens[self.hist_start] = self.lens[0];
        self.hist_start = (self.hist_start + 1) % history_lines;
        self.hist_dropped += 1;
    }
}

pub fn clearScreen(self: *Screen) void {
    // M73k: a screen clear takes the whole space — history included —
    // and any live search with it (its overlay row is about to vanish;
    // restoring it later would resurrect pre-clear content). The ALT
    // entry clear is the exception: it clears the freshly swapped-in
    // alt grid, while the history belongs to the primary screen
    // waiting underneath and must survive the round trip (pinned).
    self.searchAbort();
    if (!self.alt_active and histOf(self) != null) {
        self.hist_count = 0;
        self.hist_start = 0;
        self.hist_dropped = 0;
    }
    var i: usize = 0;
    while (i < grid_lines) : (i += 1) self.clearLine(i);
    self.used = 1;
    self.cur = 0;
    self.col = 0;
    self.pending_wrap = false;
    self.view = 0;
    self.clearSelection();
}

/// M80a (#1712): a relative motion (A-F) can park the cursor below the
/// used tail; a WRITE materialises the rows up to it — a written row is
/// in use. This is the only growth path for a relative motion's target
/// row (the absolute finals grow through moveCursor); erases and
/// motions alone leave the row unmaterialised.
pub fn materializeForWrite(self: *Screen) void {
    while (self.used <= self.cur) {
        self.clearLine(self.used);
        self.used += 1;
        self.noteTailGrowth(); // M73k: growth under a pinned view
    }
}

/// M80c: move one or more rows up inside DECSTBM. Rows outside the
/// region are never touched. Only a region whose top is row zero can
/// evict through the full-grid top, and therefore only that path can
/// feed the primary screen's history ring.
pub fn scrollRegionUp(self: *Screen, n: usize) void {
    const height = self.scroll_bottom - self.scroll_top + 1;
    const limit = if (self.scroll_top == 0) height + history_lines else height;
    const count = @min(n, limit);
    var i: usize = 0;
    while (i < count) : (i += 1) {
        if (self.scroll_top == 0) self.pushHistory();
        var row = self.scroll_top;
        while (row < self.scroll_bottom) : (row += 1) self.copyRow(row + 1, row);
        self.clearLine(self.scroll_bottom);
    }
    if (self.scroll_top == 0 and self.used <= self.scroll_bottom + 1) {
        self.used = @max(1, self.used -| count);
    }
}

/// M80c: move one or more rows down inside DECSTBM. This never reads
/// or writes history; a bottom-edge drop is ordinary grid loss.
pub fn scrollRegionDown(self: *Screen, n: usize) void {
    const height = self.scroll_bottom - self.scroll_top + 1;
    const count = @min(n, height);
    var i = self.scroll_bottom + 1;
    while (i > self.scroll_top + count) {
        i -= 1;
        self.copyRow(i - count, i);
    }
    var row = self.scroll_top;
    while (row < self.scroll_top + count) : (row += 1) self.clearLine(row);
    if (self.used <= self.cur) self.used = self.cur + 1;
    // A top-at-zero region is also the full-grid SD case: SD grows
    // the used tail until the region bottom. A region below zero is
    // internal and cannot grow the full-grid tail by itself.
    if (self.scroll_top == 0 and self.used <= self.scroll_bottom + 1) {
        self.used = @min(self.scroll_bottom + 1, self.used + count);
        if (self.used <= self.cur) self.used = self.cur + 1;
    }
}

/// M80b: ED 3 — "Erase Saved Lines" (xterm). The grid and the cursor
/// are untouched: the HISTORY goes (ring counters reset, the view
/// snaps back to the tail). The scan overlay is resolved BEFORE the
/// counters reset (searchExit — its restore is keyed by absolute
/// identity, which the reset would re-point at innocent rows): a
/// covered grid row gets its true content back (ED 3 never destroys
/// grid rows), a covered history row lands in the ring that dies on
/// the next line. Nothing can resurrect a cleared row afterwards —
/// the stash is gone with the scan. History belongs to the primary
/// screen (M73k), so ED 3 on the alternate screen is consumed and
/// changes nothing (pinned). M80k (#1727) reaches it from the
/// Ctrl+Shift+K chord — the hygiene escape for a grid the user wants
/// wiped, byte-for-byte the ED 3 an app could have sent.
pub fn clearScrollback(self: *Screen) void {
    if (self.alt_active) return;
    self.searchExit();
    self.hist_count = 0;
    self.hist_start = 0;
    self.hist_dropped = 0;
    self.view = 0;
}

/// M80b: IL/DL/SU/SD restructure rows. The scan overlay is resolved
/// FIRST (searchExit — the stash must land before the rows move, or
/// the restore would write pre-slide content into post-slide rows)
/// and the selection goes: its absolute endpoints cannot follow
/// content through a slide (clearScreen's precedent).
pub fn prepareRowSlide(self: *Screen) void {
    self.searchExit();
    self.clearSelection();
}

/// M80b: a row-structure change moves `used` — and with it
/// `lineCount()`. Under a pinned (scrolled-back) view the window's
/// BOTTOM must stay put (M73k's rule): view counts from the tail, so
/// it follows the line count's delta exactly — growth and shrinkage.
pub fn noteLineCountChange(self: *Screen, before: usize) void {
    if (self.view == 0) return;
    const after = self.lineCount();
    if (after >= before) {
        self.view += after - before;
        const max_view = self.lineCount() - 1;
        if (self.view > max_view) self.view = max_view;
    } else {
        self.view -|= (before - after);
    }
}

/// M80b/M80c: CSI S (SU) — scroll the active region upward. The
/// cursor is never touched, so a pending wrap survives explicit SU.
pub fn scrollUp(self: *Screen, n: u16) void {
    self.prepareRowSlide();
    const before = self.lineCount();
    self.scrollRegionUp(@intCast(n));
    self.noteLineCountChange(before);
}

/// M80b/M80c: CSI T (SD) — scroll the active region downward. The
/// cursor is never touched and history is never pulled from the ring.
pub fn scrollDown(self: *Screen, n: u16) void {
    self.prepareRowSlide();
    const before = self.lineCount();
    self.scrollRegionDown(@intCast(n));
    self.noteLineCountChange(before);
}

test "terminal: screen scrolls past the line bound and clears on CSI 2J" {
    var s = Screen{};
    var i: usize = 0;
    while (i < grid_lines + 5) : (i += 1) {
        s.feed("L\n");
    }
    // The buffer is full and the oldest lines were dropped.
    try std.testing.expectEqual(@as(usize, grid_lines), s.lineCount());
    // `ESC [ 2 J` clears the screen back to one empty line.
    s.feed("\x1b[2J\x1b[Hx");
    try std.testing.expectEqual(@as(usize, 1), s.lineCount());
    try std.testing.expectEqualStrings("x", s.line(0));
}

test "terminal: scrollback view clamps to the stored range" {
    var s = Screen{};
    s.feed("one\ntwo\nthree\n");
    try std.testing.expectEqual(@as(usize, 0), s.viewOffset());
    s.scrollBy(1);
    try std.testing.expectEqual(@as(usize, 1), s.viewOffset());
    s.scrollBy(100);
    try std.testing.expectEqual(@as(usize, s.lineCount() - 1), s.viewOffset());
    s.scrollBy(-1);
    try std.testing.expectEqual(@as(usize, s.lineCount() - 2), s.viewOffset());
    s.scrollBy(-100);
    try std.testing.expectEqual(@as(usize, 0), s.viewOffset());
    s.scrollBy(2);
    const bottom_before = s.lineCount() - s.viewOffset();
    // M73k (#1637): PIN-while-scrolled — new output must not yank the
    // reader back to the tail; the window's bottom stays put instead
    // (chosen over snap: a deep log or an active search breaks under
    // snap-on-output; Shift+End/scrollReset still snaps on demand).
    s.feed("four\n");
    try std.testing.expectEqual(bottom_before, s.lineCount() - s.viewOffset());
    try std.testing.expectEqual(@as(usize, 3), s.viewOffset());
    // At view 0 the tail follows new output exactly as before.
    s.scrollReset();
    s.feed("five\n");
    try std.testing.expectEqual(@as(usize, 0), s.viewOffset());
}

test "terminal: history ring keeps lines past the grid, drops oldest (#1637)" {
    for (&registry.terminals) |*t| t.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));
    const s = screenForWindow(7).?;
    var i: usize = 0;
    while (i < 140) : (i += 1) {
        var b: [16]u8 = undefined;
        const row = std.fmt.bufPrint(&b, "L{d:0>3}\n", .{i}) catch unreachable;
        s.feed(row);
    }
    // 140 LFs + the final unterminated row = 141 surviving lines:
    // 128 in the grid, the oldest 13 in the ring.
    try std.testing.expectEqual(@as(usize, 141), s.lineCount());
    try std.testing.expectEqual(@as(usize, 13), s.hist_count);
    // The oldest line is reachable BELOW the old 128-line limit.
    try std.testing.expectEqualStrings("L000", s.line(0));
    // …and the newest: every feed ends in \n, so L139 sits at lineCount-2
    // and index 140 is the cleared cursor row the last LF moved onto.
    try std.testing.expectEqualStrings("L139", s.line(139));
    try std.testing.expectEqualStrings("", s.line(140));
    // Scroll back to the very top through the unified space.
    s.scrollBy(100_000);
    try std.testing.expectEqual(@as(usize, 140), s.viewOffset());
    // Overflow: past history_lines the OLDEST drops (drop-oldest policy),
    // lineCount saturates at history_lines + grid_lines.
    i = 0;
    while (i < 300) : (i += 1) {
        var b: [16]u8 = undefined;
        const row = std.fmt.bufPrint(&b, "Z{d:0>3}\n", .{i}) catch unreachable;
        s.feed(row);
    }
    try std.testing.expectEqual(@as(usize, history_lines), s.hist_count);
    try std.testing.expect(s.hist_dropped > 0);
    try std.testing.expectEqual(
        @as(usize, history_lines + grid_lines),
        s.lineCount(),
    );
    try std.testing.expect(!(std.mem.eql(u8, s.line(0), "L000")));
}

test "terminal: the run-03 fill shape pins the ring at the fill's overshoot, not the viewport (#1757)" {
    // live-term-depth run 03 observed `tty: clear 24 lines` from a
    // "~300-row fill" and 24 == the window's visible rows, so the count
    // looked like a hidden viewport cap. It is arithmetic, not a cap:
    // gosh does not echo `source`d script lines, so FILL.SH (150 echo
    // outputs) plus the typed line is 151 newlines — the 128-row grid
    // absorbs 127 of them and exactly 24 rows scroll into the ring. The
    // match with rows_visible was numerology. This pin reproduces the
    // exact guest byte shape and holds both halves: the 24, and the ring
    // saturating at history_lines (never any viewport figure) when the
    // fill is deeper — M73k's promise to Shift+Home stands.
    for (&registry.terminals) |*t| t.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));
    const s = screenForWindow(7).?;
    // The editor paints prompt + typed line on row 0; submit emits CR LF.
    s.feed("gosh> source FILL.SH\r\n");
    // `source` runs 150 echoes; each output row ends in exactly one LF.
    var i: usize = 0;
    while (i < 150) : (i += 1) {
        var b: [16]u8 = undefined;
        const row = std.fmt.bufPrint(&b, "FILL-{d:0>3} pppp\n", .{i}) catch unreachable;
        s.feed(row);
    }
    // 151 newlines: 127 absorbed by the grid, 24 scrolled into the ring.
    try std.testing.expectEqual(@as(usize, 24), s.hist_count);
    try std.testing.expectEqual(@as(usize, grid_lines), s.used);
    try std.testing.expectEqual(@as(usize, 152), s.lineCount());
    // Ring contents: the typed row, then FILL-000..FILL-022.
    try std.testing.expectEqualStrings("gosh> source FILL.SH", s.line(0));
    try std.testing.expectEqualStrings("FILL-000 pppp", s.line(1));
    try std.testing.expectEqualStrings("FILL-022 pppp", s.line(23));
    // The grid tail: FILL-023 at the top, FILL-149 above the cursor row.
    try std.testing.expectEqualStrings("FILL-023 pppp", s.line(24));
    try std.testing.expectEqualStrings("FILL-149 pppp", s.line(150));
    try std.testing.expectEqualStrings("", s.line(151));
    // A deeper fill saturates the ring at history_lines, with the oldest
    // rows dropping — no viewport-shaped cap anywhere in the path.
    while (i < 600) : (i += 1) {
        var b: [16]u8 = undefined;
        const row = std.fmt.bufPrint(&b, "MORE-{d:0>3}\n", .{i}) catch unreachable;
        s.feed(row);
    }
    try std.testing.expectEqual(@as(usize, history_lines), s.hist_count);
    try std.testing.expect(s.hist_dropped > 0);
    for (&registry.terminals) |*tt| tt.reset();
    for (&registry.screens) |*ss| ss.reset();
}

test "terminal: history is normal-screen only; alt walks its own space (#1637)" {
    for (&registry.terminals) |*t| t.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));
    const s = screenForWindow(7).?;
    var i: usize = 0;
    while (i < 140) : (i += 1) {
        var b: [16]u8 = undefined;
        const row = std.fmt.bufPrint(&b, "N{d:0>3}\n", .{i}) catch unreachable;
        s.feed(row);
    }
    const hist_before = s.hist_count;
    try std.testing.expect(hist_before > 0);
    const lines_before = s.lineCount();
    // Enter the alternate screen: its world is the alt grid, no history.
    s.feed("\x1b[?1049h");
    try std.testing.expectEqual(@as(usize, 1), s.lineCount());
    try std.testing.expectEqual(@as(usize, 0), s.cursorLineUnified());
    // Fill the alt grid far past 128 — history must NOT grow there.
    i = 0;
    while (i < 200) : (i += 1) s.feed("alt\n");
    try std.testing.expectEqual(hist_before, s.hist_count);
    try std.testing.expectEqual(@as(usize, grid_lines), s.lineCount());
    // Leave: the primary space (with its history) comes back untouched.
    s.feed("\x1b[?1049l");
    try std.testing.expectEqual(lines_before, s.lineCount());
    try std.testing.expectEqual(hist_before, s.hist_count);
    try std.testing.expectEqualStrings("N000", s.line(0));
}

test "terminal: a pushed history row drops its RGB slots to default (#1637)" {
    for (&registry.terminals) |*t| t.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));
    const s = screenForWindow(7).?;
    s.feed("\x1b[48;2;10;20;30mCOLOURED\x1b[0m\n");
    // Grid cell carries the truecolour slot…
    const gr = s.hist_count; // unified index the row will land on after push
    s.feed("\n"); // fill 127 more rows to force the push
    var i: usize = 2;
    while (i < grid_lines) : (i += 1) s.feed(".\n");
    try std.testing.expect(s.hist_count > 0);
    // …the stored history row does not: slot coerced, rgbAt truthful.
    const st = s.styleAt(0, 0);
    try std.testing.expect(st.bg != rgb_colour);
    const side = s.rgbAt(0, 0);
    try std.testing.expect(side.bg == null);
    _ = gr;
}

test "terminal: scroll carries truecolour side arrays with the cells (#1637)" {
    for (&registry.terminals) |*t| t.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));
    const s = screenForWindow(7).?;
    s.feed("plain\n\x1b[48;2;10;20;30mTOP\x1b[0m\n");
    // Scroll exactly once (registry screen — a local Screen{} has no
    // history bank, so its ring never fills): the coloured row (grid
    // row 1) moves to grid row 0 = unified row hist_count (1), side
    // arrays in tow. Pre-M73k fix they stayed behind → default bg.
    while (s.hist_count == 0) s.feed("x\n");
    try std.testing.expectEqual(@as(usize, 1), s.hist_count);
    const side = s.rgbAt(1, 0);
    try std.testing.expectEqual(@as(u8, 10), side.bg.?.r);
    try std.testing.expectEqual(@as(u8, 20), side.bg.?.g);
    try std.testing.expectEqual(@as(u8, 30), side.bg.?.b);
}

test "terminal: ED 3 erases saved lines only; nothing resurrects them (#1713)" {
    for (&registry.terminals) |*t| t.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));
    const s = screenForWindow(7).?;
    var i: usize = 0;
    while (i < 140) : (i += 1) {
        var b: [16]u8 = undefined;
        const row = std.fmt.bufPrint(&b, "L{d:0>3}\n", .{i}) catch unreachable;
        s.feed(row);
    }
    try std.testing.expect(s.hist_count > 0);
    // A scan parks the prompt bar over a row and stashes that row's
    // content keyed by absolute identity.
    try std.testing.expect(s.searchOpen());
    _ = s.searchFeed(.{ .ch = 'L' });
    try std.testing.expect(s.searchActive());
    s.feed("\x1b[3J");
    // The scan resolved BEFORE the counters reset: its covered row keeps
    // its true content and the stash is gone, so no later restore can
    // re-point cleared-row bytes at innocent rows.
    try std.testing.expect(!s.searchActive());
    try std.testing.expectEqual(@as(usize, 0), s.hist_count);
    try std.testing.expectEqual(@as(usize, 0), s.hist_start);
    try std.testing.expectEqual(@as(usize, 0), s.hist_dropped);
    try std.testing.expectEqual(@as(usize, 0), s.viewOffset());
    try std.testing.expectEqual(@as(usize, grid_lines), s.lineCount());
    // The grid survives untouched: the newest row is still itself and no
    // bar chrome was left behind on any row.
    try std.testing.expectEqualStrings("L139", s.line(126));
    for (0..grid_lines) |r| {
        try std.testing.expect(!std.mem.startsWith(u8, s.line(r), "> "));
    }
}

test "terminal: ED 3 on the alternate screen leaves the primary scrollback alone (#1713)" {
    for (&registry.terminals) |*t| t.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));
    const s = screenForWindow(7).?;
    var i: usize = 0;
    while (i < 140) : (i += 1) {
        var b: [16]u8 = undefined;
        const row = std.fmt.bufPrint(&b, "N{d:0>3}\n", .{i}) catch unreachable;
        s.feed(row);
    }
    const hist_before = s.hist_count;
    try std.testing.expect(hist_before > 0);
    s.feed("\x1b[?1049h");
    s.feed("\x1b[3J"); // consumed on the alt screen — history lives below
    try std.testing.expectEqual(hist_before, s.hist_count);
    s.feed("\x1b[?1049l");
    try std.testing.expectEqual(hist_before, s.hist_count);
    try std.testing.expectEqualStrings("N000", s.line(0));
}

test "terminal: SU feeds history like an LF scroll; SD never pulls from it (#1713)" {
    for (&registry.terminals) |*t| t.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));
    const s = screenForWindow(7).?;
    s.feed("A\nB\nC");
    s.feed("\x1b[2S");
    // The two top rows left through the TOP edge — into the ring, same
    // as an LF scroll; the grid holds only "C".
    try std.testing.expectEqual(@as(usize, 2), s.hist_count);
    try std.testing.expectEqualStrings("A", s.line(0));
    try std.testing.expectEqualStrings("B", s.line(1));
    try std.testing.expectEqualStrings("C", s.line(2));
    // SD slides content down: a blank enters at the top, the bottom row
    // is dropped (no scrollback for the bottom edge), and the ring is
    // NOT consulted.
    s.feed("\x1b[T");
    try std.testing.expectEqual(@as(usize, 2), s.hist_count);
    try std.testing.expectEqualStrings("", s.line(2));
    try std.testing.expectEqualStrings("C", s.line(3));
}

test "terminal: IL/DL/SU/SD pin a scrolled-back view to its bottom (#1713)" {
    for (&registry.terminals) |*t| t.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachWindow(7));
    const s = screenForWindow(7).?;
    var i: usize = 0;
    while (i < 200) : (i += 1) {
        var b: [16]u8 = undefined;
        const row = std.fmt.bufPrint(&b, "V{d:0>3}\n", .{i}) catch unreachable;
        s.feed(row);
    }
    s.scrollBy(5);
    const bottom = s.lineCount() - s.viewOffset();
    const ops = [_][]const u8{
        "\x1b[2;1H\x1b[3L",
        "\x1b[3;1H\x1b[2M",
        "\x1b[2S",
        "\x1b[2T",
    };
    for (ops) |seq| {
        s.feed(seq);
        try std.testing.expectEqual(bottom, s.lineCount() - s.viewOffset());
    }
}

test "terminal: DECSTBM scopes LF IND RI and IL/DL to the active region (#1714)" {
    var s = Screen{};
    s.feed("a\r\nb\r\nc\r\nd\r\ne");
    s.feed("\x1b[2;4r\x1b[4;1H\nZ");
    try std.testing.expectEqualStrings("a", s.line(0));
    try std.testing.expectEqualStrings("c", s.line(1));
    try std.testing.expectEqualStrings("d", s.line(2));
    try std.testing.expectEqualStrings("Z", s.line(3));
    try std.testing.expectEqualStrings("e", s.line(4));

    s.reset();
    s.feed("a\r\nb\r\nc");
    s.feed("\x1b[2;3r\x1b[3;1H\x1bD");
    try std.testing.expectEqualStrings("a", s.line(0));
    try std.testing.expectEqualStrings("c", s.line(1));
    try std.testing.expectEqualStrings("", s.line(2));
    s.feed("\x1b[2;1H\x1bM");
    try std.testing.expectEqualStrings("a", s.line(0));
    try std.testing.expectEqualStrings("", s.line(1));
    try std.testing.expectEqualStrings("c", s.line(2));

    s.reset();
    s.feed("A\r\nB\r\nC\r\nD\r\nE");
    s.feed("\x1b[2;4r\x1b[3;1H\x1b[L");
    try std.testing.expectEqualStrings("A", s.line(0));
    try std.testing.expectEqualStrings("B", s.line(1));
    try std.testing.expectEqualStrings("", s.line(2));
    try std.testing.expectEqualStrings("C", s.line(3));
    try std.testing.expectEqualStrings("E", s.line(4));
    s.feed("\x1b[M");
    try std.testing.expectEqualStrings("B", s.line(1));
    try std.testing.expectEqualStrings("C", s.line(2));
    try std.testing.expectEqualStrings("", s.line(3));
    try std.testing.expectEqualStrings("E", s.line(4));
}

test "terminal: DECSTBM scopes SU and SD without inventing scrollback (#1714)" {
    var s = Screen{};
    s.feed("A\r\nB\r\nC\r\nD");
    s.feed("\x1b[2;4r\x1b[S");
    try std.testing.expectEqualStrings("A", s.line(0));
    try std.testing.expectEqualStrings("C", s.line(1));
    try std.testing.expectEqualStrings("D", s.line(2));
    try std.testing.expectEqualStrings("", s.line(3));
    s.feed("\x1b[T");
    try std.testing.expectEqualStrings("A", s.line(0));
    try std.testing.expectEqualStrings("", s.line(1));
    try std.testing.expectEqualStrings("C", s.line(2));
    try std.testing.expectEqualStrings("D", s.line(3));
}

test "terminal: DECSTBM and DECOM round-trip independently on alternate registry.screens (#1714)" {
    var s = Screen{};
    s.feed("\x1b[3;5r\x1b[?6h\x1b[?7l\x1b[?1049h");
    try std.testing.expectEqual(@as(usize, 0), s.scroll_top);
    try std.testing.expectEqual(@as(usize, grid_lines - 1), s.scroll_bottom);
    try std.testing.expect(!s.origin_mode);
    try std.testing.expect(!s.autowrap); // DECAWM is shared, not per-screen.
    s.feed("\x1b[2;4r\x1b[?6h");
    s.feed("\x1b[?1049l");
    try std.testing.expectEqual(@as(usize, 2), s.scroll_top);
    try std.testing.expectEqual(@as(usize, 4), s.scroll_bottom);
    try std.testing.expect(s.origin_mode);
    try std.testing.expect(!s.autowrap);
}

test "terminal: region-internal scrolling never feeds the primary history ring (#1714)" {
    for (&registry.terminals) |*tm| tm.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const tm = get(h).?;
    try std.testing.expect(tm.attachWindow(7));
    const s = screenForWindow(7).?;
    s.feed("A\r\nB\r\nC\r\nD\r\nE");
    try std.testing.expectEqual(@as(usize, 0), s.hist_count);
    s.feed("\x1b[2;4r\x1b[S");
    try std.testing.expectEqual(@as(usize, 0), s.hist_count);
    try std.testing.expectEqualStrings("C", s.line(1));
    try std.testing.expectEqualStrings("D", s.line(2));
}
