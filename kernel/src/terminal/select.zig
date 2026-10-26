//! Mouse selection for `Screen` (split from terminal.zig, #3753): the
//! anchor/cursor pair, point clamping, and the UTF-8 copy-out.

const std = @import("std");
const cell_mod = @import("cell.zig");
const screen_mod = @import("screen.zig");
const Point = cell_mod.Point;
const encodeCell = cell_mod.encodeCell;
const Screen = screen_mod.Screen;

// -- M49 SD5 (#1132): selection + copy ----------------------------------
pub fn clampPoint(self: *const Screen, row: usize, col: usize) Point {
    return .{
        .line = @min(row, self.used - 1),
        .col = @min(col, self.cols),
    };
}

pub fn beginSelection(self: *Screen, row: usize, col: usize) void {
    const p = self.clampPoint(row, col);
    self.sel_anchor = p;
    self.sel_cursor = p;
}

pub fn extendSelection(self: *Screen, row: usize, col: usize) void {
    if (self.sel_anchor == null) return;
    self.sel_cursor = self.clampPoint(row, col);
}

pub fn clearSelection(self: *Screen) void {
    self.sel_anchor = null;
    self.sel_cursor = null;
}

pub fn hasSelection(self: *const Screen) bool {
    return self.sel_anchor != null and self.sel_cursor != null;
}

/// True when the cell (row, col) lies inside the current selection
/// (used by the renderer to highlight it). Empty selections match
/// nothing.
pub fn inSelection(self: *const Screen, row: usize, col: usize) bool {
    const a = self.sel_anchor orelse return false;
    const b = self.sel_cursor orelse return false;
    const start = if (a.line < b.line or (a.line == b.line and a.col <= b.col)) a else b;
    const end = if (start.line == a.line and start.col == a.col) b else a;
    if (row < start.line or row > end.line) return false;
    if (row == start.line and col < start.col) return false;
    if (row == end.line and col > end.col) return false;
    return true;
}

/// Copy the selected region into `dst` as UTF-8 (lines joined by `\n`,
/// endpoints inclusive; the region is clamped to the line lengths).
/// A wide glyph copies once even when only one of its two cells is in
/// the selection, and a combining overlay rides its base. Returns the
/// byte count; 0 when there is no selection. Whole runes only — a rune
/// that does not fit the remaining room ends the copy.
pub fn copySelection(self: *const Screen, dst: []u8) usize {
    const a = self.sel_anchor orelse return 0;
    const b = self.sel_cursor orelse return 0;
    const start = if (a.line < b.line or (a.line == b.line and a.col <= b.col)) a else b;
    const end = if (start.line == a.line and start.col == a.col) b else a;
    var out: usize = 0;
    var row = start.line;
    while (row <= end.line) : (row += 1) {
        const len = if (row < self.used) self.lens[row] else 0;
        const from = if (row == start.line) @min(start.col, len) else 0;
        const to = if (row == end.line) @min(end.col, len) else len;
        var c = from;
        while (c < to) : (c += 1) {
            const cell = self.cells[row][c];
            if (cell.cont != 0) {
                // The selection starts inside a wide glyph: emit the
                // whole glyph once — its base lies just outside.
                if (c == from and c > 0) {
                    out = encodeCell(dst, out, self.cells[row][c - 1]) orelse break;
                }
                continue; // never a second copy of an emitted glyph
            }
            out = encodeCell(dst, out, cell) orelse break;
        }
        if (row < end.line and out < dst.len) {
            dst[out] = '\n';
            out += 1;
        }
    }
    return out;
}

test "terminal: selection copies across lines and normalizes direction" {
    var s = Screen{};
    s.feed("alpha\nbeta\ngamma");
    var buf: [64]u8 = undefined;

    // Forward selection from (0,2) to (2,3): "pha\nbeta\ngam".
    s.beginSelection(0, 2);
    s.extendSelection(2, 3);
    const n = s.copySelection(&buf);
    try std.testing.expectEqualStrings("pha\nbeta\ngam", buf[0..n]);

    // Backwards selection (2,3) -> (0,2) is the same region.
    s.beginSelection(2, 3);
    s.extendSelection(0, 2);
    const n2 = s.copySelection(&buf);
    try std.testing.expectEqualStrings("pha\nbeta\ngam", buf[0..n2]);

    // A single-point selection merges to the cell.
    s.beginSelection(1, 1);
    s.extendSelection(1, 3);
    const n3 = s.copySelection(&buf);
    try std.testing.expectEqualStrings("et", buf[0..n3]);

    // In-range query.
    s.beginSelection(0, 0);
    s.extendSelection(0, 2);
    try std.testing.expect(s.inSelection(0, 1));
    try std.testing.expect(!s.inSelection(0, 3));
    try std.testing.expect(!s.inSelection(1, 0));

    s.clearSelection();
    try std.testing.expectEqual(@as(usize, 0), s.copySelection(&buf));
    try std.testing.expect(!s.hasSelection());
}

test "terminal: selection coordinates clamp to the grid" {
    var s = Screen{};
    s.feed("x");
    var buf: [16]u8 = undefined;
    // An out-of-range line clamps to the last row; the region then covers it.
    s.beginSelection(999, 0);
    s.extendSelection(0, 1);
    const n = s.copySelection(&buf);
    try std.testing.expectEqualStrings("x", buf[0..n]);
    // An out-of-range column clamps to the grid width (the copy clamps to
    // the stored line length).
    s.beginSelection(0, 999);
    s.extendSelection(0, 0);
    const n2 = s.copySelection(&buf);
    try std.testing.expectEqualStrings("x", buf[0..n2]);
}

test "terminal: selection copies UTF-8 runes once, overlays included" {
    var s = Screen{};
    s.feed("a\xe4\xbd\xa0" ++ "b"); // [0]=a [1]=wide base [2]=cont [3]=b
    var buf: [64]u8 = undefined;
    s.beginSelection(0, 0);
    s.extendSelection(0, 4);
    const n = s.copySelection(&buf);
    try std.testing.expectEqualStrings("a\xe4\xbd\xa0" ++ "b", buf[0..n]);
    // Selection starts on the continuation cell: whole glyph, once.
    s.beginSelection(0, 2);
    s.extendSelection(0, 3);
    const n2 = s.copySelection(&buf);
    try std.testing.expectEqualStrings("\xe4\xbd\xa0", buf[0..n2]);
    // Selection covers only the base: one copy, no continuation bytes.
    s.beginSelection(0, 1);
    s.extendSelection(0, 2);
    const n3 = s.copySelection(&buf);
    try std.testing.expectEqualStrings("\xe4\xbd\xa0", buf[0..n3]);
    // A combining overlay rides its base as real UTF-8.
    var s2 = Screen{};
    s2.feed("e\xcc\x81");
    s2.beginSelection(0, 0);
    s2.extendSelection(0, 1);
    const m = s2.copySelection(&buf);
    try std.testing.expectEqualStrings("e\xcc\x81", buf[0..m]);
}
