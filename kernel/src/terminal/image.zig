//! Sixel image plumbing for `Screen` (split from terminal.zig, #3753): the
//! page-pool-backed image banks (host-test statics under `builtin.is_test`),
//! cell refs, placement, eviction and the scrub rules.

const std = @import("std");
const alloc = @import("../alloc.zig");
const font_metrics = @import("../font_metrics.zig");
const term_image = @import("../term_image.zig");
const builtin = @import("builtin");
const cell_mod = @import("cell.zig");
const registry = @import("../terminal.zig");
const screen_mod = @import("screen.zig");
const Cell = cell_mod.Cell;
const empty_cell = cell_mod.empty_cell;
const max_terminals = registry.max_terminals;
const Screen = screen_mod.Screen;
const grid_cols = screen_mod.grid_cols;
const grid_lines = screen_mod.grid_lines;
const histOf = screen_mod.histOf;
const history_lines = screen_mod.history_lines;

/// M85b (#1814): per-terminal image banks, linked to `screens[i]` by
/// pointer identity like the history banks — but carved from the page
/// pool on the terminal's first sixel, not module BSS: KERNEL.BIN carries
/// BSS as zero fill and has no room for pixels (term_image's header has
/// the measurement). A bank, once carved, is kept for the kernel's life.
var image_bank: [max_terminals]?*term_image.Bank = @as([max_terminals]?*term_image.Bank, @splat(@as(?*term_image.Bank, null)));

/// A Screen outside the registry (a corpus or test value) borrows this
/// ONE shared bank. It belongs to the last such Screen that began a sixel
/// decode; a Screen that lost it paints its image cells blank, and scrubs
/// them if it ever reclaims the bank, so it can never show another
/// Screen's pixels.
var image_loose: ?*term_image.Bank = null;

var image_loose_owner: ?*const Screen = null;

const image_bank_pages: u64 = (@sizeOf(term_image.Bank) + alloc.page_size - 1) / alloc.page_size;

/// Host tests cannot dereference pool addresses (driving_award's user
/// windows have the same rule), so their banks are static.
var test_image_bank: [max_terminals + 1]term_image.Bank = undefined;

var test_image_pool_empty: bool = false;

/// Bank `slot` (`max_terminals` = the loose bank), or null when the pool
/// has no run for it — the sixel is then refused whole.
fn imageBankAlloc(slot: usize) ?*term_image.Bank {
    if (builtin.is_test) return if (test_image_pool_empty) null else &test_image_bank[slot];
    const pa = alloc.alloc_pages(image_bank_pages) orelse return null;
    return @ptrFromInt(pa);
}

pub fn imageBankOf(s: *const Screen) ?*term_image.Bank {
    for (&registry.screens, 0..) |*sc, i| {
        if (sc == s) return image_bank[i];
    }
    if (image_loose_owner == s) return image_loose;
    return null;
}

fn imageBit(cell: Cell) u8 {
    return if (cell.img == 0) 0 else @as(u8, 1) << (cell.img - 1);
}

fn scrubCell(cell: *Cell, buf: u3) void {
    if (cell.img != 0 and (buf == 0 or cell.img == buf)) cell.* = empty_cell;
}

// -- M85b (#1814): image placement --------------------------------------
/// Bit i set = some cell this Screen can still show refers to image
/// buffer i: the grid, the swapped-out other screen, the history ring,
/// and the row stashed under the search prompt (it is restored later).
pub fn imageRefs(self: *const Screen) u8 {
    var mask: u8 = 0;
    for (&self.cells) |*row| {
        for (row) |cell| mask |= imageBit(cell);
    }
    for (&self.alt_cells) |*row| {
        for (row) |cell| mask |= imageBit(cell);
    }
    if (histOf(self)) |bank| {
        for (0..self.hist_count) |i| {
            for (&bank.cells[(self.hist_start + i) % history_lines]) |cell| mask |= imageBit(cell);
        }
    }
    if (self.ov_valid) {
        for (self.ov_cells) |cell| mask |= imageBit(cell);
    }
    return mask;
}

/// Blank every cell showing image buffer `buf - 1` (`buf` 0 = every
/// image), wherever `imageRefs` looks. Image cells carry the
/// erase-default rendition, so only the cell itself changes.
pub fn imageScrub(self: *Screen, buf: u3) void {
    for (&self.cells) |*row| {
        for (row) |*cell| scrubCell(cell, buf);
    }
    for (&self.alt_cells) |*row| {
        for (row) |*cell| scrubCell(cell, buf);
    }
    if (histOf(self)) |bank| {
        for (0..self.hist_count) |i| {
            for (&bank.cells[(self.hist_start + i) % history_lines]) |*cell| scrubCell(cell, buf);
        }
    }
    for (&self.ov_cells) |*cell| scrubCell(cell, buf);
}

/// This Screen's bank, carving it from the pool on first use. A loose
/// Screen (re)claims the shared bank; whatever it showed from an
/// earlier ownership may have been overwritten since.
pub fn imageBankClaim(self: *Screen) ?*term_image.Bank {
    for (&registry.screens, 0..) |*sc, i| {
        if (sc != self) continue;
        if (image_bank[i] == null) image_bank[i] = imageBankAlloc(i);
        return image_bank[i];
    }
    if (image_loose == null) image_loose = imageBankAlloc(max_terminals);
    const bank = image_loose orelse return null;
    if (image_loose_owner != self) {
        self.imageScrub(0);
        image_loose_owner = self;
    }
    return bank;
}

/// The `q` final: pick the staging buffer (one no cell refers to —
/// `buffers` = `slots` + 1 guarantees one) and start decoding into it.
/// No bank (pool exhausted) refuses the image at ST.
pub fn imageBegin(self: *Screen) void {
    const bank = self.imageBankClaim() orelse {
        self.sixel = .{};
        self.sixel.refuse();
        return;
    };
    const refs = self.imageRefs();
    var stage: u8 = 0;
    while (stage < term_image.buffers and refs & (@as(u8, 1) << @intCast(stage)) != 0) : (stage += 1) {}
    if (stage == term_image.buffers) {
        self.sixel = .{};
        self.sixel.refuse();
        return;
    }
    self.sixel.begin(&bank.img[stage], stage);
}

/// ST on a sixel string: stamp the image, make room (oldest-first
/// eviction down to `slots - 1` live images), then place it.
pub fn imageCommit(self: *Screen) void {
    const bank = imageBankOf(self) orelse {
        self.sixel.refuse();
        return self.imageRefused();
    };
    const stage = self.sixel.stage;
    const img = &bank.img[stage];
    if (!self.sixel.finish(img, font_metrics.cell_w, font_metrics.cell_h)) {
        if (self.sixel.refused) self.imageRefused();
        return;
    }
    var live = self.imageRefs();
    while (@popCount(live) >= term_image.slots) {
        var victim: u8 = 0;
        var oldest: u32 = 0;
        for (0..term_image.buffers) |i| {
            if (live & (@as(u8, 1) << @intCast(i)) == 0) continue;
            const age = self.img_seq -% bank.img[i].seq;
            if (age >= oldest) {
                oldest = age;
                victim = @intCast(i);
            }
        }
        self.imageScrub(@intCast(victim + 1));
        live &= ~(@as(u8, 1) << @intCast(victim));
        self.img_evicted +%= 1;
    }
    self.img_seq +%= 1;
    img.seq = self.img_seq;
    self.imagePlace(@intCast(stage + 1), img);
    self.img_placed +%= 1;
}

pub fn imageRefused(self: *Screen) void {
    self.sixel_overflow = true;
    self.img_refused +%= 1;
}

/// Cell-native placement (sixel scrolling on — the DECSDM default):
/// the image's top-left tile lands on the cursor cell and it covers
/// ceil(w / cell_w) x ceil(h / cell_h) cells, REPLACING whatever text
/// was there. Columns past the right margin are cropped (never
/// wrapped); each further tile row is an IND, so an image taller than
/// the room below scrolls the region like text would, rows already
/// placed moving up with it. The cursor ends at the left margin of the
/// line below the image (the xterm-family "next line" rule; the
/// right-of-image variant, xterm DECSET 8452, is not implemented).
pub fn imagePlace(self: *Screen, id: u3, img: *const term_image.Image) void {
    const ncols = (@as(usize, img.w) + img.cell_w - 1) / img.cell_w;
    const nrows = (@as(usize, img.h) + img.cell_h - 1) / img.cell_h;
    self.cancelPendingWrap();
    self.materializeForWrite();
    const col0 = self.col;
    const end = @min(col0 + ncols, self.cols);
    var ty: usize = 0;
    while (ty < nrows) : (ty += 1) {
        if (ty > 0) self.index();
        // Pair repair, putRune's rule: never leave half a wide pair.
        if (col0 > 0 and self.cells[self.cur][col0].cont != 0) self.blankCell(self.cur, col0 - 1);
        if (end < grid_cols and self.cells[self.cur][end].cont != 0) self.blankCell(self.cur, end);
        var c = col0;
        while (c < end) : (c += 1) {
            self.blankCell(self.cur, c);
            self.cells[self.cur][c] = .{ .img = id, .tx = @intCast(c - col0), .ty = @intCast(ty) };
        }
        if (end > self.lens[self.cur]) self.lens[self.cur] = end;
    }
    self.nextLine();
}

/// The image tile the cell at unified (line, col) shows, or null: a
/// text cell, or a loose Screen that lost the shared bank.
pub fn imageTile(self: *const Screen, line_index: usize, col_index: usize) ?term_image.Tile {
    const cell = self.cellAt(line_index, col_index);
    if (cell.img == 0) return null;
    const bank = imageBankOf(self) orelse return null;
    const img = &bank.img[cell.img - 1];
    return .{
        .img = img,
        .x0 = @as(usize, cell.tx) * img.cell_w,
        .y0 = @as(usize, cell.ty) * img.cell_h,
    };
}

// -- M85b (#1814): image layer --------------------------------------------
const test_red_cell = "\x1bPq\"1;1;8;16#1;2;100;0;0~\x1b\\";

test "terminal: image rows scroll into history with their tiles; eviction and ED 3 reach the ring (M85b #1814)" {
    for (&registry.screens) |*sc| sc.reset();
    const s = &registry.screens[0];
    s.feed(test_red_cell);
    while (s.hist_count == 0) s.feed("x\n");
    // The image row is the oldest history row now: its tile rode the
    // push (history stores the Cell repr whole) and still paints.
    try std.testing.expectEqual(@as(usize, 1), s.hist_count);
    try std.testing.expect(s.cellAt(0, 0).img != 0);
    const tile = s.imageTile(0, 0) orelse return error.TestExpectedImageCell;
    try std.testing.expectEqual(@as(?u32, 0xff0000), tile.sample(0, 0, 8, 16));
    // A second image evicts the first even though it lives in the ring.
    s.feed(test_red_cell);
    try std.testing.expectEqual(@as(u3, 0), s.cellAt(0, 0).img);
    try std.testing.expectEqual(@as(u32, 1), s.img_evicted);
    // Scroll the second image into the ring too, then ED 3: the ring —
    // and every reference in it — is gone, so a third image evicts
    // nothing.
    for (0..grid_lines) |_| s.feed("x\n");
    try std.testing.expect(s.imageRefs() != 0);
    s.feed("\x1b[3J");
    try std.testing.expectEqual(@as(u8, 0), s.imageRefs());
    s.feed(test_red_cell);
    try std.testing.expectEqual(@as(u32, 1), s.img_evicted);
    try std.testing.expectEqual(@as(u32, 3), s.img_placed);
    for (&registry.screens) |*sc| sc.reset();
}

test "terminal: a loose Screen that lost the shared image bank paints nothing and scrubs on reclaim (M85b #1814)" {
    var a = Screen{};
    var b = Screen{};
    a.feed(test_red_cell);
    try std.testing.expect(a.imageTile(0, 0) != null);
    b.feed(test_red_cell);
    // `a` still has its image cell, but the bank is b's now: no pixels.
    try std.testing.expect(a.cellAt(0, 0).img != 0);
    try std.testing.expect(a.imageTile(0, 0) == null);
    try std.testing.expect(b.imageTile(0, 0) != null);
    // Reclaiming scrubs a's stale cell before the new image lands.
    a.feed(test_red_cell);
    try std.testing.expectEqual(@as(u3, 0), a.cellAt(0, 0).img);
    try std.testing.expect(a.imageTile(1, 0) != null);
    try std.testing.expect(b.imageTile(0, 0) == null);
}

test "terminal: an empty page pool refuses the sixel whole, and the next one carves the bank (M85b #1814)" {
    for (&registry.screens) |*sc| sc.reset();
    const s = &registry.screens[1];
    const kept = image_bank[1];
    defer {
        image_bank[1] = kept;
        test_image_pool_empty = false;
        for (&registry.screens) |*sc| sc.reset();
    }
    image_bank[1] = null;
    test_image_pool_empty = true;
    s.feed(test_red_cell ++ "A");
    try std.testing.expectEqual(@as(u32, 1), s.img_refused);
    try std.testing.expectEqual(@as(u32, 0), s.img_placed);
    try std.testing.expect(s.sixel_overflow);
    // Nothing placed, cursor unmoved: the text after ST lands at 0,0.
    try std.testing.expectEqual(@as(u3, 0), s.cellAt(0, 0).img);
    try std.testing.expectEqual(@as(u21, 'A'), s.cellAt(0, 0).base);
    try std.testing.expect(image_bank[1] == null);
    test_image_pool_empty = false;
    s.feed("\r\n" ++ test_red_cell);
    try std.testing.expect(image_bank[1] != null);
    try std.testing.expectEqual(@as(u32, 1), s.img_placed);
    try std.testing.expect(!s.sixel_overflow);
    try std.testing.expect(s.imageTile(1, 0) != null);
}

test "terminal: the search overlay's stashed row keeps its image buffer referenced (M85b #1814)" {
    var s = Screen{};
    s.feed(test_red_cell);
    s.ov_cells[0] = s.cells[0][0];
    s.ov_valid = true;
    s.cells[0][0] = empty_cell;
    try std.testing.expect(s.imageRefs() != 0);
    s.imageScrub(0);
    try std.testing.expectEqual(@as(u3, 0), s.ov_cells[0].img);
}
