//! The presentation `Screen` (split from terminal.zig, #3753): the bounded rune
//! grid with scrollback, the escape/UTF-8 parser core (`putByte`/`feed`), the
//! public read surface, and thin forwarders to the ops modules in this
//! directory (osc/image/scroll/edit/cursor/csi/reflow/select/search).

const std = @import("std");
const clipboard = @import("../clipboard.zig");
const text = @import("../text.zig");
const term_image = @import("../term_image.zig");
const cell_mod = @import("cell.zig");
const csi_mod = @import("csi.zig");
const cursor_mod = @import("cursor.zig");
const edit_mod = @import("edit.zig");
const image_mod = @import("image.zig");
const osc_mod = @import("osc.zig");
const reflow_mod = @import("reflow.zig");
const registry = @import("../terminal.zig");
const scroll_mod = @import("scroll.zig");
const search_mod = @import("search.zig");
const select_mod = @import("select.zig");
const Cell = cell_mod.Cell;
const CellStyle = cell_mod.CellStyle;
const Charset = cell_mod.Charset;
const Point = cell_mod.Point;
const Rgb = cell_mod.Rgb;
const colour_slot = cell_mod.colour_slot;
const default_cell_style = cell_mod.default_cell_style;
const empty_cell = cell_mod.empty_cell;
const empty_rgb = cell_mod.empty_rgb;
const rgb_colour = cell_mod.rgb_colour;
const max_terminals = registry.max_terminals;

/// Bounded output-parser reply staging. DSR/CPR and DA replies are small;
/// keeping this on the pure Screen lets the session drain it through the
/// terminal's existing input FIFO without coupling presentation to Terminal.
pub const reply_max: usize = 32;

/// M80g (#1715): the OSC string bound — one `ESC ] … ST/BEL` payload.
/// Sized to carry a full OSC 52 copy (base64 of the 512 B clipboard is
/// 684 bytes + the `c;` selector) with room for the Ps digits; a longer
/// OSC is dropped whole at its terminator — consumed, never painted,
/// never partially applied.
pub const osc_max: usize = 768;

/// M80g: the stored window-title bound. Delivery sinks truncate further:
/// the window title buffer takes 63 bytes + NUL, the seat's kind-11
/// frame the frozen 24-byte `wm_rpc_title_max` field.
pub const title_max: usize = 64;

/// #1082 (ADR 0020 Amendment A): the window front-end's presentation grid.
/// The terminal OBJECT stays a pure byte session (D1); this bounded
/// character grid + scrollback is presentation state rendered by the kernel
/// into the bound `.user` window (A4). 8x8 cells, the kernel glyph raster.
pub const grid_cols: usize = 80;

pub const grid_lines: usize = 128;

/// Default horizontal tab stops at columns 8, 16, ..., 72. Column 0 is
/// deliberately absent, matching xterm and the terminal's historical
/// `(col + 8) & ~7` TAB rule.
pub const default_tab_stops: [2]u64 = .{ 0x0101010101010100, 0x0000000000000101 };

/// M73k (#1637): scrollback depth beyond the live grid. MEASURED against
/// `tools/verify-bss-budget.sh`, not guessed: one history row is
/// cells 80x8 + styles 80x4 + lens = 968 B (M73h widened CellStyle to
/// u32 — that is what pushes the card's 512-line band down), and five
/// banks are needed: one per terminal (`max_terminals`) plus ONE
/// full-size reflow temp — an EXPANDING re-wrap cannot happen in place
/// (it would write ahead of its own reads on a single ring), so reflow
/// snapshots exactly like the grid's own `reflow_lines` path:
///   5 * 256 * 968 = 1,239,040 B  ->  budget PASS (~195 KiB headroom).
/// 256 = 2x the live screen depth per window, 1024 lines across the
/// fleet. Rows carry the M73a-1 `Cell` repr + rendition; the RGB side
/// arrays do NOT ride along (+480 B/row would blow the budget) —
/// truecolour cells are coerced to the presentation default on push
/// (stated honestly in the PR).
pub const history_lines: usize = 256;

/// One screen's history ring contents. Module BSS for the same reason as
/// `reflow_lines`: far too large for a Screen's .data object or a stack.
pub const HistoryBank = struct {
    cells: [history_lines][grid_cols]Cell,
    styles: [history_lines][grid_cols]CellStyle,
    lens: [history_lines]usize,
};

/// Per-terminal history banks, linked to `screens[i]` by pointer identity.
/// A Screen outside the registry (a local test value) has no bank and
/// simply never accumulates history — existing tests stay byte-identical.
var history_bank: [max_terminals]HistoryBank = undefined;

pub fn histOf(s: *const Screen) ?*HistoryBank {
    for (&registry.screens, 0..) |*sc, i| {
        if (sc == s) return &history_bank[i];
    }
    return null;
}

/// M73a-1 (#1625): `line()`'s ASCII projection scratch (module BSS). One
/// consumer at a time — the paint path reads a line and moves on.
var line_scratch: [grid_cols]u8 = undefined;

// ---------------------------------------------------------------------------
// M73a-1 (#1625): rune cells — UTF-8 decode, wide pairs, overlays, UTF-8 copy
// ---------------------------------------------------------------------------
test "terminal: grid stores ASCII as a single cell and projects it unchanged" {
    var s = Screen{};
    s.feed("A");
    const cell = s.cellAt(0, 0);
    try std.testing.expectEqual(@as(u21, 'A'), cell.base);
    try std.testing.expectEqual(@as(u21, 0), cell.mark);
    try std.testing.expectEqual(@as(u1, 0), cell.cont);
    try std.testing.expectEqualStrings("A", s.line(0));
}

pub const Screen = struct {
    cells: [grid_lines][grid_cols]Cell = @as([grid_lines][grid_cols]Cell, @splat(@as([grid_cols]Cell, @as([grid_cols]Cell, @splat(@as(Cell, empty_cell)))))),
    styles: [grid_lines][grid_cols]CellStyle = @as([grid_lines][grid_cols]CellStyle, @splat(@as([grid_cols]CellStyle, @as([grid_cols]CellStyle, @splat(@as(CellStyle, default_cell_style)))))),
    lens: [grid_lines]usize = @as([grid_lines]usize, @splat(@as(usize, 0))),
    /// Number of lines in use (>= 1); grows to `grid_lines` then scrolls.
    used: usize = 1,
    /// The cursor's line (0..used-1) and column. `col == cols` is the
    /// VT-correct pending-wrap position; `pending_wrap` distinguishes it
    /// from a cursor deliberately parked by a motion.
    cur: usize = 0,
    col: usize = 0,
    /// M80c (#1714): DECSTBM and DECOM belong to the screen, not the
    /// terminal object. Rows are zero-based internally; the CSI form is
    /// one-based and inclusive. The alternate screen has its own margins.
    scroll_top: usize = 0,
    scroll_bottom: usize = grid_lines - 1,
    origin_mode: bool = false,
    pending_wrap: bool = false,
    alt_scroll_top: usize = 0,
    alt_scroll_bottom: usize = grid_lines - 1,
    alt_origin_mode: bool = false,
    alt_pending_wrap: bool = false,
    /// M80c: DECAWM is shared across primary and alternate screens, like
    /// bracketed paste and the mouse modes. Only the DECSTBM/DECOM state
    /// and the cursor's pending-wrap bit round-trip with each screen.
    autowrap: bool = true,
    /// The alternate screen is a second bounded grid, not an allocation.
    /// Entering DECSET 47/1049 swaps the primary into this storage and clears
    /// the active grid; DECRST swaps it back unchanged.
    alt_cells: [grid_lines][grid_cols]Cell = @as([grid_lines][grid_cols]Cell, @splat(@as([grid_cols]Cell, @as([grid_cols]Cell, @splat(@as(Cell, empty_cell)))))),
    alt_styles: [grid_lines][grid_cols]CellStyle = @as([grid_lines][grid_cols]CellStyle, @splat(@as([grid_cols]CellStyle, @as([grid_cols]CellStyle, @splat(@as(CellStyle, default_cell_style)))))),
    alt_lens: [grid_lines]usize = @as([grid_lines]usize, @splat(@as(usize, 0))),
    alt_used: usize = 1,
    alt_cur: usize = 0,
    alt_col: usize = 0,
    alt_view: usize = 0,
    alt_style: CellStyle = default_cell_style,
    alt_active: bool = false,
    /// M80e (#1722): one DECSC/DECRC slot per screen. The active slot is
    /// swapped with the alternate screen; ESC 7 saves cursor, rendition,
    /// and character-set state, while ESC 8 restores it.
    saved_cur: usize = 0,
    saved_col: usize = 0,
    saved_style: CellStyle = default_cell_style,
    saved_fg_rgb: Rgb = empty_rgb,
    saved_bg_rgb: Rgb = empty_rgb,
    saved_charset_g0: Charset = .ascii,
    saved_charset_g1: Charset = .ascii,
    saved_charset_active: u8 = 0,
    saved_pending_wrap: bool = false,
    saved_valid: bool = false,
    alt_saved_cur: usize = 0,
    alt_saved_col: usize = 0,
    alt_saved_style: CellStyle = default_cell_style,
    alt_saved_fg_rgb: Rgb = empty_rgb,
    alt_saved_bg_rgb: Rgb = empty_rgb,
    alt_saved_charset_g0: Charset = .ascii,
    alt_saved_charset_g1: Charset = .ascii,
    alt_saved_charset_active: u8 = 0,
    alt_saved_pending_wrap: bool = false,
    alt_saved_valid: bool = false,
    /// M80e: the active G0/G1 character sets. G0 is selected with ESC (,
    /// G1 with ESC ), and SO/SI switch the active set without painting.
    charset_g0: Charset = .ascii,
    charset_g1: Charset = .ascii,
    charset_active: u8 = 0,
    alt_charset_g0: Charset = .ascii,
    alt_charset_g1: Charset = .ascii,
    alt_charset_active: u8 = 0,
    /// M80e: DECKPAM/DECKPNM is application-visible state only; the grid
    /// never invents keypad glyphs. It is shared across primary/alternate.
    keypad_application: bool = false,
    /// M73h (#1634): truecolour channels per cell, primary and alternate.
    /// Written by `putRune` from the current rendition state and read ONLY
    /// for cells whose slot marks `rgb_colour`; swapped and reflowed with
    /// their style arrays.
    fg_rgb: [grid_lines][grid_cols]Rgb = @as([grid_lines][grid_cols]Rgb, @splat(@as([grid_cols]Rgb, @as([grid_cols]Rgb, @splat(@as(Rgb, empty_rgb)))))),
    bg_rgb: [grid_lines][grid_cols]Rgb = @as([grid_lines][grid_cols]Rgb, @splat(@as([grid_cols]Rgb, @as([grid_cols]Rgb, @splat(@as(Rgb, empty_rgb)))))),
    alt_fg_rgb: [grid_lines][grid_cols]Rgb = @as([grid_lines][grid_cols]Rgb, @splat(@as([grid_cols]Rgb, @as([grid_cols]Rgb, @splat(@as(Rgb, empty_rgb)))))),
    alt_bg_rgb: [grid_lines][grid_cols]Rgb = @as([grid_lines][grid_cols]Rgb, @splat(@as([grid_cols]Rgb, @as([grid_cols]Rgb, @splat(@as(Rgb, empty_rgb)))))),
    /// The current truecolour state (applies to FUTURE writes), mirrored
    /// across the alternate swap exactly like `style`/`alt_style`.
    fg_rgb_cur: Rgb = empty_rgb,
    bg_rgb_cur: Rgb = empty_rgb,
    alt_fg_rgb_cur: Rgb = empty_rgb,
    alt_bg_rgb_cur: Rgb = empty_rgb,
    /// M80d (#1721): horizontal tab stops, one bit per grid column. The
    /// defaults match the historical fixed TAB rule: columns 8, 16, ...;
    /// column 0 is not a stop unless HTS explicitly adds it. Stop tables
    /// belong to the screen, so an alternate-screen round trip restores the
    /// primary table unchanged.
    tab_stops: [2]u64 = default_tab_stops,
    alt_tab_stops: [2]u64 = default_tab_stops,
    /// Parser state: 0 normal, 1 ESC, 2 CSI, 3 ESC character-set final,
    /// 4 OSC string, 5 OSC string after its ESC (ST pending), 6 ESC string
    /// (DCS/APC/SOS/PM), 7 ESC string after its ESC (ST pending).
    esc_state: u8 = 0,
    esc_charset_slot: u8 = 0,
    csi_params: [16]u16 = @as([16]u16, @splat(@as(u16, 0))),
    csi_count: usize = 0,
    /// M182a (#3150): the ECMA-48 private-parameter marker — '<' '=' '>'
    /// '?' (0x3c-0x3f), recorded only as the FIRST byte of the parameter
    /// section; 0 = none. A marker anywhere later, or a second marker,
    /// sets `csi_malformed`: the sequence is consumed to its final byte
    /// and nothing acts on it. `csi_started` is the first-byte test —
    /// true once any parameter/intermediate byte has been consumed.
    csi_marker: u8 = 0,
    csi_malformed: bool = false,
    csi_started: bool = false,
    csi_intermediate: u8 = 0,
    /// M80g (#1715): the in-flight OSC string and the effects it queued.
    /// Stream state like the CSI parser's — not swapped with the alternate
    /// screen. The EFFECTS (title, clipboard copy) are delivered by the
    /// window pump, the M80f reply-drain pattern: a corpus Screen drives
    /// the parser with no side effects at all.
    osc_buf: [osc_max]u8 = undefined,
    osc_len: usize = 0,
    osc_overflow: bool = false,
    /// M80g: the window title the stream asked for (Ps 0/2), bounded and
    /// NUL-safe; `title_dirty` marks a change the pump still owes the
    /// window buffer and (when a seat is registered) its kind-11 seam.
    title: [title_max]u8 = undefined,
    title_len: usize = 0,
    title_dirty: bool = false,
    /// M80g: OSC 52's decoded copy queued for the clipboard; a `?` read
    /// query is refused (privacy) and never lands here.
    clip_data: [clipboard.capacity]u8 = undefined,
    clip_len: usize = 0,
    clip_dirty: bool = false,
    /// M85a (#1813): image-sequence intake — DCS `q` (sixel) and the
    /// "consumed, never painted" rule for every other ESC string (APC —
    /// kitty's channel — SOS, PM, non-`q` DCS). Stream state like the OSC
    /// parser's: never swapped with the alternate screen.
    /// The string introducer that opened the in-flight string: 'P' (DCS),
    /// '_' (APC), 'X' (SOS), '^' (PM).
    dcs_intro: u8 = 0,
    /// 0 = parameter string (before the DCS final), 1 = sixel payload
    /// (after `q`), 2 = drain-only (non-`q` final or non-DCS introducer).
    dcs_phase: u8 = 0,
    /// The three DCS Ps values (`P1;P2;P3`); params beyond three are
    /// consumed, never parsed. M85b parses but does not honour them (see
    /// term_image: square pixels, unset pixels transparent).
    sixel_p: [3]u16 = @as([3]u16, @splat(@as(u16, 0))),
    sixel_pcount: usize = 0,
    /// M85b (#1814): the payload is DECODED as it streams (M85a's 4 KiB
    /// capture buffer is gone — it bounded bytes, when the real bound is
    /// pixels, and cost 16 KiB of kernel image across the registry).
    sixel: term_image.Decoder = .{},
    /// Sticky until the next string begins: the image broke a bound (or
    /// lost its buffer) and was refused WHOLE — nothing placed.
    sixel_overflow: bool = false,
    /// The cursor at the string's start — where the image is placed.
    sixel_origin_line: usize = 0,
    sixel_origin_col: usize = 0,
    /// M85b: placement order for the bank's oldest-first eviction, and
    /// the counted outcomes (the overflow policy is explicit and counted):
    /// images placed, refused whole, and evicted by a newer one.
    img_seq: u32 = 0,
    img_placed: u32 = 0,
    img_refused: u32 = 0,
    img_evicted: u32 = 0,
    /// M80a (#1712): REP (CSI b)'s "last printed rune" — the placement is
    /// repeated VERBATIM (rune, overlay mark and rendition). Stream state
    /// like the CSI parser's own: not swapped with the alternate screen,
    /// not reset by cursor motion; 0 = nothing printed yet (the C0
    /// controls and NUL never reach putRune).
    last_cp: u21 = 0,
    last_mark: u21 = 0,
    last_style: CellStyle = default_cell_style,
    /// M80f (#1723): replies generated while parsing owner output. This is
    /// presentation-side state, drained by the session pump into the bound
    /// terminal's input FIFO; Screen never owns a Terminal pointer.
    pending_reply: [reply_max]u8 = undefined,
    pending_reply_len: usize = 0,
    /// M73a-1 (#1625): the in-flight UTF-8 sequence, if any. `utf_need` is
    /// the continuation bytes still expected (0 = idle), `utf_acc` the
    /// partial codepoint, `utf_len` the total sequence length (for the
    /// overlong/surrogate/range check at completion).
    utf_need: u8 = 0,
    utf_acc: u32 = 0,
    utf_len: u8 = 0,
    /// Current SGR rendition. It is presentation state, never terminal-object
    /// bytes, so serial and network front-ends remain byte-for-byte unchanged.
    style: CellStyle = default_cell_style,
    /// DECTCEM (`CSI ? 25 h/l`) controls only the painted block cursor.
    /// Unlike grid content and rendition, this terminal-mode bit is shared
    /// across primary and alternate screens: changing cursor visibility while
    /// an alternate screen is active remains in effect after it is restored.
    cursor_visible: bool = true,
    /// M73e (#1629): DECSET/DECRST `CSI ? 2004 h/l` — bracketed paste.
    /// A paste pushed while this is set is wrapped in `\e[200~ … \e[201~`
    /// so the receiving editor keeps paste content literal. Like DECTCEM
    /// it is a terminal-mode bit: shared across primary/alternate screens.
    bracketed_paste: bool = false,
    /// M73i (#1635): mouse tracking modes — DECSET/DECRST `CSI ? 1000/1002/
    /// 1003/1006 h/l`, tracked like bracketed_paste and never painted.
    /// xterm resolution: `?1000` press/release edges, `?1002` adds button-
    /// motion drag, `?1003` adds all motion (superset), `?1006` selects the
    /// SGR encoding over the legacy `CSI b x y M` bytes. Shared across the
    /// primary/alternate screens.
    mouse_1000: bool = false,
    mouse_1002: bool = false,
    mouse_1003: bool = false,
    mouse_1006: bool = false,
    /// M49 SD5 (#1132): the effective column count (8..grid_cols). A window
    /// resize reflows the grid to the new client width.
    cols: usize = grid_cols,
    /// M49 SD5: scrollback view offset — 0 follows the tail, N shows N
    /// lines further back. New output snaps the view back to the tail.
    view: usize = 0,
    /// M49 SD5: the selection endpoints (absolute grid rows), if any.
    sel_anchor: ?Point = null,
    sel_cursor: ?Point = null,
    /// M73k (#1637): history ring position for THIS screen — `hist_count`
    /// surviving rows (0..history_lines), `hist_start` the oldest slot,
    /// `hist_dropped` the total ever evicted. `u + hist_dropped` is a
    /// line's STABLE absolute identity (unified indices shift when the
    /// ring rotates; absolutes do not).
    hist_count: usize = 0,
    hist_start: usize = 0,
    hist_dropped: u64 = 0,
    /// M73k: kernel-side search — presentation state like selection,
    /// never in the byte pipe (D1 holds). Normal screen only. Literal,
    /// row-local, rune-aware, ASCII-case-insensitive (pinned by test).
    search_active: bool = false,
    search_pat: [32]u21 = @as([32]u21, @splat(@as(u21, 0))),
    search_len: usize = 0,
    search_count: usize = 0,
    search_row: usize = 0, // unified row of the current match
    search_col: usize = 0, // start column of the current match
    search_has_cur: bool = false,
    /// The user's selection, saved while search owns `sel_*` for the
    /// current-match highlight; restored on exit.
    saved_sel_anchor: ?Point = null,
    saved_sel_cursor: ?Point = null,
    /// The row under the prompt bar, saved/restored through ABSOLUTE
    /// identity (survives ring rotation and grid scroll; an evicted row
    /// has nothing left to restore, so the restore skips it).
    ov_cells: [grid_cols]Cell = @as([grid_cols]Cell, @splat(@as(Cell, empty_cell))),
    ov_styles: [grid_cols]CellStyle = @as([grid_cols]CellStyle, @splat(@as(CellStyle, default_cell_style))),
    ov_fg: [grid_cols]Rgb = @as([grid_cols]Rgb, @splat(@as(Rgb, empty_rgb))),
    ov_bg: [grid_cols]Rgb = @as([grid_cols]Rgb, @splat(@as(Rgb, empty_rgb))),
    ov_lens: usize = 0,
    ov_abs: u64 = 0,
    ov_valid: bool = false,

    pub fn reset(self: *Screen) void {
        self.* = .{};
    }

    /// M80e: soft reset (CSI ! p) resets parser-visible modes and rendition
    /// but deliberately leaves the grid, scrollback, and cursor untouched.
    /// M80k (#1727) also reaches this from the Ctrl+Shift+R chord — the
    /// escape path and the chord must not drift apart, so the chord calls
    /// the same seam the parser does.
    pub fn softReset(self: *Screen) void {
        // DECSTR clears the deferred right-margin state too; otherwise
        // col == cols would survive with pending_wrap false and the next
        // print would index one cell past the effective width.
        self.cancelPendingWrap();
        self.style = default_cell_style;
        self.fg_rgb_cur = empty_rgb;
        self.bg_rgb_cur = empty_rgb;
        self.last_cp = 0;
        self.last_mark = 0;
        self.last_style = default_cell_style;
        self.cursor_visible = true;
        self.bracketed_paste = false;
        self.mouse_1000 = false;
        self.mouse_1002 = false;
        self.mouse_1003 = false;
        self.mouse_1006 = false;
        self.charset_g0 = .ascii;
        self.charset_g1 = .ascii;
        self.charset_active = 0;
        self.keypad_application = false;
        self.scroll_top = 0;
        self.scroll_bottom = grid_lines - 1;
        self.origin_mode = false;
        self.pending_wrap = false;
        self.autowrap = true;
    }

    /// M80e: RIS is a full grid reset. The effective window width belongs to
    /// the front-end, not the escape sequence, so preserve it across the
    /// state reset and re-mask the default tab table to that width. The
    /// M80g OSC queue clears with everything else — a dropped queue, never
    /// a delivered blank title. M80k (#1727) reaches it from the
    /// Ctrl+Shift+Alt+R chord (the full RIS) through this same seam.
    pub fn hardReset(self: *Screen) void {
        const cols = self.cols;
        self.reset();
        self.cols = cols;
        self.maskTabStops();
    }

    /// M80c: a cursor/control motion cancels the deferred right-margin
    /// wrap. `col == cols` is a logical position only while pending_wrap
    /// is set; motions leave the visible cursor on the last column.
    pub fn cancelPendingWrap(self: *Screen) void {
        self.pending_wrap = false;
        if (self.col >= self.cols) self.col = self.cols - 1;
    }

    /// Move to the next row for a soft wrap. This is deliberately not
    /// `newline`: it clears the destination row and does not perform a
    /// carriage return until the caller has established the new column.
    pub fn wrapForPrint(self: *Screen) void {
        self.pending_wrap = false;
        if (self.cur < self.scroll_bottom) {
            self.cur += 1;
            const grew = self.cur >= self.used;
            if (grew) self.used = self.cur + 1;
            self.clearLine(self.cur);
            if (grew) self.noteTailGrowth();
        } else if (self.cur == self.scroll_bottom) {
            self.scrollRegionUp(1);
            if (self.used <= self.cur) self.used = self.cur + 1;
        } else if (self.cur + 1 < grid_lines) {
            self.cur += 1;
            const grew = self.cur >= self.used;
            if (grew) self.used = self.cur + 1;
            self.clearLine(self.cur);
            if (grew) self.noteTailGrowth();
        } else {
            self.scrollFullUp(1);
        }
        self.col = 0;
    }

    /// M80a: REP (CSI b) — repeat the last printed rune `n` more times.
    /// Routed through putRune, never a direct cell write, so a pending
    /// wrap fires on the first repeat exactly like a real print; a repeat
    /// re-places the stored mark and rendition verbatim. No prior print
    /// is a no-op.
    pub fn repeatLast(self: *Screen, n: u16) void {
        if (self.last_cp == 0) return;
        const count: usize = n;
        var i: usize = 0;
        while (i < count) : (i += 1) self.putRune(self.last_cp, self.last_mark, self.last_style);
    }

    /// M73a-1 (#1625): place one rune. Width comes from `text.char_width`:
    /// a double-width rune takes base + continuation cells and never splits
    /// across a wrap; a zero-width rune overlays its mark onto the base
    /// behind the cursor (stepping over a continuation cell), with no base
    /// behind it pinned to U+FFFD, and ignorable zero-width runes dropped
    /// without a cell. `mark` is only non-zero for the reflow re-feed,
    /// which restores a stored overlay verbatim. A write that would split
    /// a wide pair repairs the pair first.
    pub fn putRune(self: *Screen, cp: u21, mark: u21, style: CellStyle) void {
        const width: usize = text.char_width(cp);
        if (width == 0) {
            if (text.is_zero_width_ignorable(cp)) return; // no cell, cursor unchanged
            self.materializeForWrite();
            if (self.col == 0) {
                self.putRune(0xFFFD, 0, style); // no base behind: pin to U+FFFD
                return;
            }
            var base_col = self.col - 1;
            if (base_col > 0 and self.cells[self.cur][base_col].cont != 0) base_col -= 1;
            if (self.cells[self.cur][base_col].cont != 0) {
                // A continuation at column 0 would be corrupt; be honest.
                self.putRune(0xFFFD, 0, style);
                return;
            }
            if (self.cells[self.cur][base_col].img != 0) {
                // M85b (#1814): an image cell is no base — the no-base rule.
                self.putRune(0xFFFD, 0, style);
                return;
            }
            self.cells[self.cur][base_col].mark = cp; // one overlay slot: last wins
            return; // M73k: writes never touch the view (pin-while-scrolled)
        }
        self.materializeForWrite();
        if (self.pending_wrap) {
            self.wrapForPrint();
        } else if (width == 2 and self.col + 2 > self.cols) {
            if (self.autowrap) {
                self.wrapForPrint();
            } else {
                // A double-width rune cannot be split when autowrap is
                // disabled. Keep the cursor total and make the refusal
                // visible as one ordinary replacement cell.
                self.putRune(0xFFFD, 0, style);
                return;
            }
        }
        // Pair repair: a continuation cell we overwrite loses its base, and
        // a continuation cell just past the written range belonged to a
        // base we are replacing. Clear the orphan rather than leave a
        // torn half-pair for the painter.
        if (self.col > 0 and self.cells[self.cur][self.col].cont != 0) {
            self.cells[self.cur][self.col - 1] = empty_cell;
            self.styles[self.cur][self.col - 1] = default_cell_style;
        }
        if (self.col + width < grid_cols and self.cells[self.cur][self.col + width].cont != 0) {
            self.cells[self.cur][self.col + width] = empty_cell;
            self.styles[self.cur][self.col + width] = default_cell_style;
        }
        self.cells[self.cur][self.col] = .{ .base = cp, .mark = mark, .cont = 0 };
        self.styles[self.cur][self.col] = style;
        self.fg_rgb[self.cur][self.col] = self.fg_rgb_cur;
        self.bg_rgb[self.cur][self.col] = self.bg_rgb_cur;
        self.last_cp = cp; // M80a: REP's "last printed rune" (+ mark/style)
        self.last_mark = mark;
        self.last_style = style;
        if (width == 2) {
            self.cells[self.cur][self.col + 1] = empty_cell;
            self.cells[self.cur][self.col + 1].cont = 1;
            self.styles[self.cur][self.col + 1] = style;
            self.fg_rgb[self.cur][self.col + 1] = self.fg_rgb_cur;
            self.bg_rgb[self.cur][self.col + 1] = self.bg_rgb_cur;
        }
        const end = self.col + width;
        if (end > self.lens[self.cur]) self.lens[self.cur] = end;
        if (end == self.cols) {
            self.col = if (self.autowrap) self.cols else self.cols - 1;
            self.pending_wrap = self.autowrap;
        } else {
            self.col = end;
            self.pending_wrap = false;
        }
    }

    /// Feed one output byte. CSI is intentionally bounded to the sequences
    /// a window TUI needs; unsupported sequences are consumed, never painted.
    /// Bytes >= 0x80 are UTF-8-decoded into the grid (M73a-1 #1625); an
    /// ill-formed sequence becomes U+FFFD, never a raw byte.
    pub fn putByte(self: *Screen, b: u8) void {
        // M73a-1: a pending UTF-8 sequence continues here (b is a
        // continuation byte) or fails: one U+FFFD for the truncated
        // sequence, then this byte is processed fresh as if idle (an ESC
        // after a half-sequence still starts an escape).
        if (self.utf_need > 0) {
            if ((b & 0xc0) == 0x80) {
                self.utfContinue(b);
                return;
            }
            self.utf_need = 0;
            self.utf_len = 0;
            self.putRune(0xFFFD, 0, self.style);
        }
        switch (self.esc_state) {
            0 => {},
            1 => {
                self.esc_state = 0;
                switch (b) {
                    '[' => {
                        self.esc_state = 2;
                        self.resetCsi();
                    },
                    ']' => {
                        // M80g (#1715): OSC — a string until BEL or ST.
                        self.esc_state = 4;
                        self.osc_len = 0;
                        self.osc_overflow = false;
                    },
                    '7' => self.saveCursor(),
                    '8' => self.restoreCursor(),
                    'D' => self.index(),
                    'M' => self.reverseIndex(),
                    'E' => self.nextLine(),
                    'c' => self.hardReset(),
                    '=' => self.keypad_application = true,
                    '>' => self.keypad_application = false,
                    '(' => {
                        self.esc_state = 3;
                        self.esc_charset_slot = 0;
                    },
                    ')' => {
                        self.esc_state = 3;
                        self.esc_charset_slot = 1;
                    },
                    'H' => {
                        // M80d: HTS sets a stop at the current column.
                        self.setTabStop(self.col);
                    },
                    'P', '_', 'X', '^' => {
                        // M85a (#1813): an ESC string — DCS (sixel's
                        // channel), APC (kitty's), SOS, PM. All are
                        // consumed through ST, never painted; only DCS `q`
                        // captures payload.
                        self.dcsBegin(b);
                    },
                    else => {},
                }
                return;
            },
            2 => {
                if (b >= '0' and b <= '9') {
                    self.csi_started = true;
                    const last = self.csi_count - 1;
                    self.csi_params[last] = self.csi_params[last] *% 10 +% (b - '0');
                    return;
                }
                if (b == ';') {
                    self.csi_started = true;
                    if (self.csi_count < self.csi_params.len) self.csi_count += 1;
                    return;
                }
                if (b >= 0x3c and b <= 0x3f) {
                    // M182a (#3150): '<' '=' '>' '?' are ECMA-48 private
                    // parameter markers, legal only as the FIRST byte of
                    // the parameter section. Anywhere later — or as a
                    // second marker — the sequence is malformed: consumed
                    // to the final byte, nothing acts on it.
                    if (self.csi_started) {
                        self.csi_malformed = true;
                    } else {
                        self.csi_marker = b;
                    }
                    self.csi_started = true;
                    return;
                }
                if (b >= 0x20 and b <= 0x3f) {
                    self.csi_started = true;
                    if (self.csi_intermediate == 0) self.csi_intermediate = b;
                    return;
                }
                self.esc_state = 0;
                self.dispatchCsi(b);
                return;
            },
            3 => {
                self.esc_state = 0;
                self.setCharset(self.esc_charset_slot, b);
                return;
            },
            4 => {
                // M80g: OSC collection. BEL terminates, ESC starts the
                // two-byte ST, CAN/SUB abort. Past the bound the string
                // keeps consuming until its terminator and is then dropped
                // WHOLE — bounded memory, never partially applied.
                if (b == 0x07) {
                    self.esc_state = 0;
                    self.dispatchOsc();
                    return;
                }
                if (b == 0x1b) {
                    self.esc_state = 5;
                    return;
                }
                if (b == 0x18 or b == 0x1a) {
                    self.oscAbort();
                    return;
                }
                if (self.osc_len < osc_max) {
                    self.osc_buf[self.osc_len] = b;
                    self.osc_len += 1;
                } else {
                    self.osc_overflow = true;
                }
                return;
            },
            5 => {
                // M80g: the byte after an ESC inside an OSC. `\\` completes
                // the ST and dispatches; anything else aborts the string
                // and is processed as an escape final (ESC x), xterm-style.
                if (b == '\\') {
                    self.esc_state = 0;
                    self.dispatchOsc();
                    return;
                }
                self.oscAbort();
                self.esc_state = 1;
                self.putByte(b);
                return;
            },
            6 => {
                // M85a (#1813): ESC-string collection (DCS/APC/SOS/PM).
                // Consumed through ST, never painted; CAN/SUB aborts and
                // the partial capture is dropped whole.
                if (b == 0x1b) {
                    self.esc_state = 7;
                    return;
                }
                if (b == 0x18 or b == 0x1a) {
                    self.dcsAbort();
                    return;
                }
                self.dcsByte(b);
                return;
            },
            7 => {
                // M85a: the byte after an ESC inside a string — the OSC
                // state-5 rule, mirrored: `\\` completes the ST, anything
                // else aborts the string and runs as an escape final.
                if (b == '\\') {
                    self.dcsFinish();
                    return;
                }
                self.dcsAbort();
                self.esc_state = 1;
                self.putByte(b);
                return;
            },
            else => self.esc_state = 0,
        }
        switch (b) {
            0x1b => self.esc_state = 1,
            0x0e => self.charset_active = 1, // SO: invoke G1
            0x0f => self.charset_active = 0, // SI: invoke G0
            '\n' => self.newline(),
            '\r' => {
                self.cancelPendingWrap();
                self.col = 0;
            },
            0x08 => {
                self.cancelPendingWrap();
                if (self.col > 0) self.col -= 1;
            },
            '\t' => self.moveTabStops(1, true),
            0x07 => {}, // bell — silent
            else => {
                if (b < 0x20 or b == 0x7f) return;
                if (b < 0x80) {
                    self.putRune(self.translateCharset(b), 0, self.style);
                } else {
                    self.utfStart(b);
                }
            },
        }
    }

    /// M73a-1: start a UTF-8 sequence — C2..F4 are well-formed leads; a
    /// stray continuation byte or an invalid lead (C0/C1, F5..FF) is one
    /// U+FFFD.
    pub fn utfStart(self: *Screen, b: u8) void {
        if (b < 0xc2 or b > 0xf4) {
            self.putRune(0xFFFD, 0, self.style);
            return;
        }
        self.utf_need = if (b < 0xe0) @as(u8, 1) else if (b < 0xf0) 2 else 3;
        self.utf_acc = if (b < 0xe0) b & 0x1f else if (b < 0xf0) b & 0x0f else b & 0x07;
        self.utf_len = self.utf_need + 1;
    }

    /// M73a-1: accumulate a continuation byte; at the last byte validate
    /// the whole sequence (overlong, surrogate, and above U+10FFFF each
    /// fail as one U+FFFD for the sequence, not one per byte).
    pub fn utfContinue(self: *Screen, b: u8) void {
        self.utf_acc = (self.utf_acc << 6) | @as(u32, b & 0x3f);
        self.utf_need -= 1;
        if (self.utf_need > 0) return;
        const cp = self.utf_acc;
        const ok = switch (self.utf_len) {
            2 => cp >= 0x80,
            3 => cp >= 0x800 and !(cp >= 0xd800 and cp <= 0xdfff),
            4 => cp >= 0x10000 and cp <= 0x10ffff,
            else => false,
        };
        self.utf_len = 0;
        const rune: u21 = if (ok) @intCast(cp) else 0xFFFD;
        self.putRune(rune, 0, self.style);
    }

    pub fn feed(self: *Screen, bytes: []const u8) void {
        for (bytes) |b| self.putByte(b);
    }

    /// M73k (#1637): the total surviving lines as ONE space — history
    /// first (oldest at 0), then the grid. The painter's existing
    /// `lineCount - view - rows` walk and `terminalHitAt`'s mirror both
    /// index this, so history paints and hit-tests with zero walk edits.
    /// Alt-screen: its own bounded world — history never shows there.
    pub fn lineCount(self: *const Screen) usize {
        if (self.alt_active) return self.used;
        return self.hist_count + self.used;
    }

    /// The unified row the cursor sits on — the PAINT-side index (the
    /// corpus, wheel reports and search keep the grid-relative `cursorLine`).
    pub fn cursorLineUnified(self: *const Screen) usize {
        return if (self.alt_active) self.cur else self.hist_count + self.cur;
    }

    /// The ASCII projection of line `i` (empty for an out-of-range line):
    /// base runes U+0000..U+007F as themselves; continuation cells and any
    /// non-ASCII rune project to one 0x00 byte (the renderer already skips
    /// <0x20 and >0x7E, so rune cells draw nothing until M73a-2's painter
    /// reads `cellAt`). The length is still the cell count (`lens`), so
    /// column indices line up. Module scratch: consume the result before
    /// the next call.
    pub fn line(self: *const Screen, i: usize) []const u8 {
        const row_cells = self.uCells(i) orelse return &.{};
        const n = @min(self.uLen(i) orelse 0, grid_cols);
        for (0..n) |c| {
            const cell = row_cells[c];
            line_scratch[c] = if (cell.cont != 0 or cell.base >= 0x80) 0 else @intCast(cell.base);
        }
        return line_scratch[0..n];
    }

    /// M73k: resolve unified row `i` to its cells / length / rendition
    /// (null = past the end). History rows come from this screen's bank,
    /// grid rows shift by `hist_count`, the alt screen never has history.
    pub fn uCells(self: *const Screen, i: usize) ?[]const Cell {
        if (self.alt_active) {
            if (i >= self.used) return null;
            return &self.cells[i];
        }
        if (i < self.hist_count) {
            const bank = histOf(self) orelse return null;
            return &bank.cells[(self.hist_start + i) % history_lines];
        }
        const gr = i - self.hist_count;
        if (gr >= self.used) return null;
        return &self.cells[gr];
    }

    pub fn uLen(self: *const Screen, i: usize) ?usize {
        if (self.alt_active) {
            if (i >= self.used) return null;
            return self.lens[i];
        }
        if (i < self.hist_count) {
            const bank = histOf(self) orelse return null;
            return bank.lens[(self.hist_start + i) % history_lines];
        }
        const gr = i - self.hist_count;
        if (gr >= self.used) return null;
        return self.lens[gr];
    }

    pub fn uStyles(self: *const Screen, i: usize) ?[]const CellStyle {
        if (self.alt_active) {
            if (i >= self.used) return null;
            return &self.styles[i];
        }
        if (i < self.hist_count) {
            const bank = histOf(self) orelse return null;
            return &bank.styles[(self.hist_start + i) % history_lines];
        }
        const gr = i - self.hist_count;
        if (gr >= self.used) return null;
        return &self.styles[gr];
    }

    /// The real cell at (line, col) — M73a-1's presentation truth for the
    /// painter, tests, and anyone who needs runes rather than bytes.
    pub fn cellAt(self: *const Screen, line_index: usize, col_index: usize) Cell {
        if (col_index >= grid_cols) return empty_cell;
        const row_cells = self.uCells(line_index) orelse return empty_cell;
        return row_cells[col_index];
    }

    pub fn cursorLine(self: *const Screen) usize {
        return self.cur;
    }

    pub fn cursorCol(self: *const Screen) usize {
        return self.col;
    }

    pub fn columns(self: *const Screen) usize {
        return self.cols;
    }

    pub fn styleAt(self: *const Screen, line_index: usize, col_index: usize) CellStyle {
        if (col_index >= self.cols) return default_cell_style;
        const row_styles = self.uStyles(line_index) orelse return default_cell_style;
        return row_styles[col_index];
    }

    /// M73h (#1634): the truecolour channels for a cell — null unless that
    /// cell's slot marks `rgb_colour` (palette/default cells store no RGB).
    pub fn rgbAt(self: *const Screen, line_index: usize, col_index: usize) struct { fg: ?Rgb, bg: ?Rgb } {
        // M73k: history rows store no RGB side arrays (push coerces those
        // slots to the presentation default) — nulls are truthful there.
        if (!self.alt_active and line_index < self.hist_count) return .{ .fg = null, .bg = null };
        const gr = if (self.alt_active) line_index else line_index - self.hist_count;
        if (gr >= self.used or col_index >= self.cols) return .{ .fg = null, .bg = null };
        const st = self.styles[gr][col_index];
        return .{
            .fg = if (st.fg == rgb_colour) self.fg_rgb[gr][col_index] else null,
            .bg = if (st.bg == rgb_colour) self.bg_rgb[gr][col_index] else null,
        };
    }

    // -- M49 SD5 (#1132): scrollback view -----------------------------------
    /// Move the scrollback view by `delta` lines (positive = older). Clamped
    /// to the stored range; 0 follows the tail.
    pub fn scrollBy(self: *Screen, delta: i32) void {
        // M73k: the stored range is now grid+history as one space.
        const total = self.lineCount();
        const max_view: i64 = if (total > 0) @intCast(total - 1) else 0;
        var v: i64 = @as(i64, @intCast(self.view)) + delta;
        if (v < 0) v = 0;
        if (v > max_view) v = max_view;
        self.view = @intCast(v);
        // M73k: the prompt bar follows the window (its row is bottom-1).
        if (self.search_active) self.searchDrawPrompt();
    }

    /// Snap the view back to the tail. M73k: new output no longer does
    /// this implicitly — pin-while-scrolled; Shift+End still snaps.
    pub fn scrollReset(self: *Screen) void {
        self.view = 0;
        if (self.search_active) self.searchDrawPrompt(); // M73k: bar follows
    }

    pub fn viewOffset(self: *const Screen) usize {
        return self.view;
    }

    /// M80k (#1727): the surviving scrollback rows for this screen. Zero
    /// on the alternate screen — the ring belongs to the primary grid
    /// waiting underneath (M73k) — so the Ctrl+Shift+K chord's marker
    /// counts what ACTUALLY went rather than what the user asked for.
    pub fn historyCount(self: *const Screen) usize {
        if (histOf(self)) |_| return self.hist_count;
        return 0;
    }

    // -- M73k (#1637): kernel-side search over grid + history ------------
    /// The abstract keys the search bar consumes. input.zig maps HID keys
    /// to these; tests construct them directly — which is also how a
    /// RUNE enters the pattern (HID keys are ASCII; cells hold runes).
    pub const SearchKey = union(enum) { ch: u21, backspace, enter, prev, esc };

    // ------------------------------------------------------------------
    // Ops live in the sibling modules of kernel/src/terminal/ (#3753);
    // these forwarders keep the `Screen` call surface byte-identical.
    // ------------------------------------------------------------------

    pub fn oscAbort(self: *Screen) void {
        return osc_mod.oscAbort(self);
    }

    pub fn dispatchOsc(self: *Screen) void {
        return osc_mod.dispatchOsc(self);
    }

    pub fn setTitle(self: *Screen, payload: []const u8) void {
        return osc_mod.setTitle(self, payload);
    }

    pub fn osc52(self: *Screen, payload: []const u8) void {
        return osc_mod.osc52(self, payload);
    }

    pub fn dcsBegin(self: *Screen, intro: u8) void {
        return osc_mod.dcsBegin(self, intro);
    }

    pub fn dcsByte(self: *Screen, b: u8) void {
        return osc_mod.dcsByte(self, b);
    }

    pub fn dcsFinish(self: *Screen) void {
        return osc_mod.dcsFinish(self);
    }

    pub fn dcsAbort(self: *Screen) void {
        return osc_mod.dcsAbort(self);
    }

    pub fn imageRefs(self: *const Screen) u8 {
        return image_mod.imageRefs(self);
    }

    pub fn imageScrub(self: *Screen, buf: u3) void {
        return image_mod.imageScrub(self, buf);
    }

    pub fn imageBankClaim(self: *Screen) ?*term_image.Bank {
        return image_mod.imageBankClaim(self);
    }

    pub fn imageBegin(self: *Screen) void {
        return image_mod.imageBegin(self);
    }

    pub fn imageCommit(self: *Screen) void {
        return image_mod.imageCommit(self);
    }

    pub fn imageRefused(self: *Screen) void {
        return image_mod.imageRefused(self);
    }

    pub fn imagePlace(self: *Screen, id: u3, img: *const term_image.Image) void {
        return image_mod.imagePlace(self, id, img);
    }

    pub fn imageTile(self: *const Screen, line_index: usize, col_index: usize) ?term_image.Tile {
        return image_mod.imageTile(self, line_index, col_index);
    }

    pub fn clearSavedCursor(self: *Screen) void {
        return cursor_mod.clearSavedCursor(self);
    }

    pub fn saveCursor(self: *Screen) void {
        return cursor_mod.saveCursor(self);
    }

    pub fn restoreCursor(self: *Screen) void {
        return cursor_mod.restoreCursor(self);
    }

    pub fn setCharset(self: *Screen, slot: u8, final: u8) void {
        return cursor_mod.setCharset(self, slot, final);
    }

    pub fn translateCharset(self: *const Screen, b: u8) u21 {
        return cursor_mod.translateCharset(self, b);
    }

    pub fn index(self: *Screen) void {
        return scroll_mod.index(self);
    }

    pub fn reverseIndex(self: *Screen) void {
        return scroll_mod.reverseIndex(self);
    }

    pub fn nextLine(self: *Screen) void {
        return scroll_mod.nextLine(self);
    }

    pub fn keypadApplication(self: *const Screen) bool {
        return cursor_mod.keypadApplication(self);
    }

    pub fn activeTabStops(self: *Screen) *[2]u64 {
        return cursor_mod.activeTabStops(self);
    }

    pub fn setTabStop(self: *Screen, col: usize) void {
        return cursor_mod.setTabStop(self, col);
    }

    pub fn clearTabStop(self: *Screen, col: usize) void {
        return cursor_mod.clearTabStop(self, col);
    }

    pub fn hasTabStop(self: *const Screen, col: usize) bool {
        return cursor_mod.hasTabStop(self, col);
    }

    pub fn activeTabStopsConst(self: *const Screen) *const [2]u64 {
        return cursor_mod.activeTabStopsConst(self);
    }

    pub fn moveTabStops(self: *Screen, count: u16, forward: bool) void {
        return cursor_mod.moveTabStops(self, count, forward);
    }

    pub fn maskTabStops(self: *Screen) void {
        return cursor_mod.maskTabStops(self);
    }

    pub fn clearLine(self: *Screen, i: usize) void {
        return scroll_mod.clearLine(self, i);
    }

    pub fn newline(self: *Screen) void {
        return scroll_mod.newline(self);
    }

    pub fn scrollFullUp(self: *Screen, n: usize) void {
        return scroll_mod.scrollFullUp(self, n);
    }

    pub fn noteTailGrowth(self: *Screen) void {
        return scroll_mod.noteTailGrowth(self);
    }

    pub fn pushHistory(self: *Screen) void {
        return scroll_mod.pushHistory(self);
    }

    pub fn clearScreen(self: *Screen) void {
        return scroll_mod.clearScreen(self);
    }

    pub fn materializeForWrite(self: *Screen) void {
        return scroll_mod.materializeForWrite(self);
    }

    pub fn cursorUp(self: *Screen, n: u16) void {
        return cursor_mod.cursorUp(self, n);
    }

    pub fn cursorDown(self: *Screen, n: u16) void {
        return cursor_mod.cursorDown(self, n);
    }

    pub fn cursorHomeRow(self: *const Screen) usize {
        return cursor_mod.cursorHomeRow(self);
    }

    pub fn homeCursor(self: *Screen) void {
        return cursor_mod.homeCursor(self);
    }

    pub fn setScrollRegion(self: *Screen, top_one: u16, bottom_one: u16) void {
        return cursor_mod.setScrollRegion(self, top_one, bottom_one);
    }

    pub fn setOriginMode(self: *Screen, enabled: bool) void {
        return cursor_mod.setOriginMode(self, enabled);
    }

    pub fn scrollRegionUp(self: *Screen, n: usize) void {
        return scroll_mod.scrollRegionUp(self, n);
    }

    pub fn scrollRegionDown(self: *Screen, n: usize) void {
        return scroll_mod.scrollRegionDown(self, n);
    }

    pub fn setForeground(self: *Screen, colour: colour_slot) void {
        return csi_mod.setForeground(self, colour);
    }

    pub fn setBackground(self: *Screen, colour: colour_slot) void {
        return csi_mod.setBackground(self, colour);
    }

    pub fn setBold(self: *Screen, on: bool) void {
        return csi_mod.setBold(self, on);
    }

    pub fn setDim(self: *Screen, on: bool) void {
        return csi_mod.setDim(self, on);
    }

    pub fn setItalic(self: *Screen, on: bool) void {
        return csi_mod.setItalic(self, on);
    }

    pub fn setUnderline(self: *Screen, on: bool) void {
        return csi_mod.setUnderline(self, on);
    }

    pub fn setReverse(self: *Screen, on: bool) void {
        return csi_mod.setReverse(self, on);
    }

    pub fn resetCsi(self: *Screen) void {
        return csi_mod.resetCsi(self);
    }

    pub fn csiParam(self: *const Screen, param_index: usize, fallback: u16) u16 {
        return csi_mod.csiParam(self, param_index, fallback);
    }

    pub fn eraseLine(self: *Screen, mode: u16) void {
        return edit_mod.eraseLine(self, mode);
    }

    pub fn eraseDisplay(self: *Screen, mode: u16) void {
        return edit_mod.eraseDisplay(self, mode);
    }

    pub fn blankCell(self: *Screen, row: usize, col: usize) void {
        return edit_mod.blankCell(self, row, col);
    }

    pub fn copyRow(self: *Screen, from: usize, to: usize) void {
        return edit_mod.copyRow(self, from, to);
    }

    pub fn clearScrollback(self: *Screen) void {
        return scroll_mod.clearScrollback(self);
    }

    pub fn prepareRowSlide(self: *Screen) void {
        return scroll_mod.prepareRowSlide(self);
    }

    pub fn noteLineCountChange(self: *Screen, before: usize) void {
        return scroll_mod.noteLineCountChange(self, before);
    }

    pub fn insertChars(self: *Screen, n: u16) void {
        return edit_mod.insertChars(self, n);
    }

    pub fn deleteChars(self: *Screen, n: u16) void {
        return edit_mod.deleteChars(self, n);
    }

    pub fn eraseChars(self: *Screen, n: u16) void {
        return edit_mod.eraseChars(self, n);
    }

    pub fn insertLines(self: *Screen, n: u16) void {
        return edit_mod.insertLines(self, n);
    }

    pub fn deleteLines(self: *Screen, n: u16) void {
        return edit_mod.deleteLines(self, n);
    }

    pub fn scrollUp(self: *Screen, n: u16) void {
        return scroll_mod.scrollUp(self, n);
    }

    pub fn scrollDown(self: *Screen, n: u16) void {
        return scroll_mod.scrollDown(self, n);
    }

    pub fn moveCursor(self: *Screen, row_one_based: u16, col_one_based: u16) void {
        return cursor_mod.moveCursor(self, row_one_based, col_one_based);
    }

    pub fn swapAlternate(self: *Screen) void {
        return edit_mod.swapAlternate(self);
    }

    pub fn setAlternate(self: *Screen, enabled: bool) void {
        return edit_mod.setAlternate(self, enabled);
    }

    pub fn applySgr(self: *Screen) void {
        return csi_mod.applySgr(self);
    }

    pub fn applyExtendedColour(self: *Screen, i: *usize, fg: bool) void {
        return csi_mod.applyExtendedColour(self, i, fg);
    }

    pub fn dispatchCsi(self: *Screen, final: u8) void {
        return csi_mod.dispatchCsi(self, final);
    }

    pub fn cursorReply(self: *const Screen) []const u8 {
        return csi_mod.cursorReply(self);
    }

    pub fn appendReply(self: *Screen, bytes: []const u8) void {
        return csi_mod.appendReply(self, bytes);
    }

    pub fn reflow(self: *Screen, new_cols: usize) void {
        return reflow_mod.reflow(self, new_cols);
    }

    pub fn reflowHistory(self: *Screen, new_cols: usize) void {
        return reflow_mod.reflowHistory(self, new_cols);
    }

    pub fn closeHistRow(self: *Screen, bank: *HistoryBank, len: usize) void {
        return reflow_mod.closeHistRow(self, bank, len);
    }

    pub fn setCols(self: *Screen, new_cols: usize) usize {
        return reflow_mod.setCols(self, new_cols);
    }

    pub fn clampPoint(self: *const Screen, row: usize, col: usize) Point {
        return select_mod.clampPoint(self, row, col);
    }

    pub fn beginSelection(self: *Screen, row: usize, col: usize) void {
        return select_mod.beginSelection(self, row, col);
    }

    pub fn extendSelection(self: *Screen, row: usize, col: usize) void {
        return select_mod.extendSelection(self, row, col);
    }

    pub fn clearSelection(self: *Screen) void {
        return select_mod.clearSelection(self);
    }

    pub fn hasSelection(self: *const Screen) bool {
        return select_mod.hasSelection(self);
    }

    pub fn inSelection(self: *const Screen, row: usize, col: usize) bool {
        return select_mod.inSelection(self, row, col);
    }

    pub fn copySelection(self: *const Screen, dst: []u8) usize {
        return select_mod.copySelection(self, dst);
    }

    pub fn searchActive(self: *const Screen) bool {
        return search_mod.searchActive(self);
    }

    pub fn searchOpen(self: *Screen) bool {
        return search_mod.searchOpen(self);
    }

    pub fn searchFeed(self: *Screen, key: SearchKey) ?usize {
        return search_mod.searchFeed(self, key);
    }

    pub fn searchExit(self: *Screen) void {
        return search_mod.searchExit(self);
    }

    pub fn searchAbort(self: *Screen) void {
        return search_mod.searchAbort(self);
    }

    pub fn stashedRow(self: *const Screen) ?usize {
        return search_mod.stashedRow(self);
    }

    pub fn scanCells(self: *const Screen, row: usize) ?[]const Cell {
        return search_mod.scanCells(self, row);
    }

    pub fn scanLen(self: *const Screen, row: usize) ?usize {
        return search_mod.scanLen(self, row);
    }

    pub fn searchRecompute(self: *Screen) void {
        return search_mod.searchRecompute(self);
    }

    pub fn searchStep(self: *Screen, dir: i32) void {
        return search_mod.searchStep(self, dir);
    }

    pub fn searchHighlight(self: *Screen) void {
        return search_mod.searchHighlight(self);
    }

    pub fn searchScrollTo(self: *Screen, row: usize) void {
        return search_mod.searchScrollTo(self, row);
    }

    pub fn searchOverlayRow(self: *const Screen) usize {
        return search_mod.searchOverlayRow(self);
    }

    pub fn searchPositionOverlay(self: *Screen) void {
        return search_mod.searchPositionOverlay(self);
    }

    pub fn searchRestoreOverlay(self: *Screen) void {
        return search_mod.searchRestoreOverlay(self);
    }

    pub fn searchDrawPrompt(self: *Screen) void {
        return search_mod.searchDrawPrompt(self);
    }
};

test "terminal: screen lays out CR/LF/BS/TAB and wraps at the column bound" {
    var s = Screen{};
    s.feed("abc\r\nx");
    try std.testing.expectEqual(@as(usize, 2), s.lineCount());
    try std.testing.expectEqualStrings("abc", s.line(0));
    try std.testing.expectEqualStrings("x", s.line(1));
    try std.testing.expectEqual(@as(usize, 1), s.cursorCol());
    // Backspace erases the cursor's column (no glyph invented).
    s.feed("\x08y");
    try std.testing.expectEqualStrings("y", s.line(1));
    // TAB advances to the next multiple of 8.
    s.feed("\tz");
    try std.testing.expectEqual(@as(usize, 9), s.cursorCol());
    try std.testing.expectEqualStrings("y       z", s.line(1));
    // A line longer than the grid wraps instead of overrunning.
    var long: [grid_cols + 3]u8 = undefined;
    @memset(&long, 'a');
    s.feed(&long);
    try std.testing.expectEqual(@as(usize, 3), s.lineCount());
    try std.testing.expectEqual(@as(usize, grid_cols), s.line(1).len);
    try std.testing.expectEqual(@as(usize, 12), s.line(2).len);
}

test "terminal: 2-, 3-, and 4-byte UTF-8 decodes to one rune cell" {
    var s = Screen{};
    s.feed("\xc3\xa9"); // U+00E9 e-acute (2-byte, narrow)
    try std.testing.expectEqual(@as(u21, 0xE9), s.cellAt(0, 0).base);
    try std.testing.expectEqual(@as(usize, 1), s.cursorCol());
    s.feed("\xe2\x82\xac"); // U+20AC euro (3-byte, narrow)
    try std.testing.expectEqual(@as(u21, 0x20AC), s.cellAt(0, 1).base);
    try std.testing.expectEqual(@as(usize, 2), s.cursorCol());
    s.feed("\xf0\x9f\x98\x80"); // U+1F600 grin (4-byte, wide)
    try std.testing.expectEqual(@as(u21, 0x1F600), s.cellAt(0, 2).base);
    try std.testing.expectEqual(@as(u1, 1), s.cellAt(0, 3).cont);
    try std.testing.expectEqual(@as(usize, 4), s.cursorCol());
    try std.testing.expectEqual(@as(usize, 4), s.line(0).len); // lens counts cells
    // The ASCII projection skips rune cells; M73a-2's painter reads cellAt.
    try std.testing.expectEqualStrings("\x00\x00\x00\x00", s.line(0));
}

test "terminal: ill-formed UTF-8 is one U+FFFD per bad sequence, never a raw byte" {
    var s = Screen{};
    s.feed("\x80"); // stray continuation
    try std.testing.expectEqual(@as(u21, 0xFFFD), s.cellAt(0, 0).base);
    s.feed("\xc0\x80"); // C0 is not a lead: two bad bytes, two FFFDs
    try std.testing.expectEqual(@as(u21, 0xFFFD), s.cellAt(0, 1).base);
    try std.testing.expectEqual(@as(u21, 0xFFFD), s.cellAt(0, 2).base);
    s.feed("\xff"); // invalid lead
    try std.testing.expectEqual(@as(u21, 0xFFFD), s.cellAt(0, 3).base);
    // Truncated tail: one FFFD, then the offending byte reprocessed fresh.
    s.feed("\xc3");
    s.feed("x");
    try std.testing.expectEqual(@as(u21, 0xFFFD), s.cellAt(0, 4).base);
    try std.testing.expectEqual(@as(u21, 'x'), s.cellAt(0, 5).base);
    s.feed("\xe0\x80\x80"); // overlong NUL fails as one sequence
    try std.testing.expectEqual(@as(u21, 0xFFFD), s.cellAt(0, 6).base);
    try std.testing.expectEqual(@as(usize, 7), s.cursorCol());
    s.feed("\xed\xa0\x80"); // UTF-16 surrogate
    try std.testing.expectEqual(@as(u21, 0xFFFD), s.cellAt(0, 7).base);
    s.feed("\xf4\x90\x80\x80"); // above U+10FFFF
    try std.testing.expectEqual(@as(u21, 0xFFFD), s.cellAt(0, 8).base);
    // A well-formed sequence still decodes after all that.
    s.feed("\xc3\xa9");
    try std.testing.expectEqual(@as(u21, 0xE9), s.cellAt(0, 9).base);
}

test "terminal: a wide pair occupies two cells and never splits across a wrap" {
    var s = Screen{};
    s.feed("\xe4\xbd\xa0"); // U+4F60 (wide)
    try std.testing.expectEqual(@as(u21, 0x4F60), s.cellAt(0, 0).base);
    try std.testing.expectEqual(@as(u1, 1), s.cellAt(0, 1).cont);
    try std.testing.expectEqual(@as(usize, 2), s.cursorCol());
    try std.testing.expectEqual(@as(usize, 2), s.line(0).len);
    // Park the cursor in the last column; a wide glyph wraps, never splits.
    var i: usize = 0;
    while (i < 77) : (i += 1) s.feed("a");
    try std.testing.expectEqual(@as(usize, 79), s.cursorCol());
    s.feed("\xe4\xbd\xa0");
    try std.testing.expectEqual(@as(usize, 1), s.cursorLine());
    try std.testing.expectEqual(@as(usize, 2), s.cursorCol());
    try std.testing.expectEqual(@as(u21, 0x4F60), s.cellAt(1, 0).base);
    try std.testing.expectEqual(@as(u1, 1), s.cellAt(1, 1).cont);
    // The last cell of line 0 was never half-written.
    try std.testing.expectEqual(@as(u1, 0), s.cellAt(0, 79).cont);
}

test "terminal: overwriting or erasing a pair edge repairs the pair" {
    var s = Screen{};
    s.feed("\xe4\xbd\xa0a"); // [0]=wide base [1]=cont [2]=a
    s.feed("\x1b[1;2H"); // cursor onto the continuation cell
    s.feed("x"); // narrow overwrite of the right half clears the base
    try std.testing.expectEqual(@as(u21, 'x'), s.cellAt(0, 1).base);
    try std.testing.expectEqual(@as(u21, ' '), s.cellAt(0, 0).base);
    try std.testing.expectEqual(@as(u1, 0), s.cellAt(0, 0).cont);
    try std.testing.expectEqual(@as(u21, 'a'), s.cellAt(0, 2).base);

    var s2 = Screen{};
    s2.feed("\xe4\xbd\xa0a");
    s2.feed("\r");
    s2.feed("y"); // overwriting the base clears its orphaned continuation
    try std.testing.expectEqual(@as(u21, 'y'), s2.cellAt(0, 0).base);
    try std.testing.expectEqual(@as(u21, ' '), s2.cellAt(0, 1).base);
    try std.testing.expectEqual(@as(u1, 0), s2.cellAt(0, 1).cont);

    // EL0 from the middle of a pair extends over the whole pair.
    var s3 = Screen{};
    s3.feed("\xe4\xbd\xa0a");
    s3.feed("\x1b[1;2H\x1b[0K");
    try std.testing.expectEqual(@as(u21, ' '), s3.cellAt(0, 0).base);
    try std.testing.expectEqual(@as(u21, ' '), s3.cellAt(0, 1).base);
    try std.testing.expectEqual(@as(u1, 0), s3.cellAt(0, 0).cont);
}

test "terminal: combining marks overlay the base behind the cursor" {
    var s = Screen{};
    s.feed("e\xcc\x81"); // e + U+0301 acute
    try std.testing.expectEqual(@as(u21, 'e'), s.cellAt(0, 0).base);
    try std.testing.expectEqual(@as(u21, 0x301), s.cellAt(0, 0).mark);
    try std.testing.expectEqual(@as(usize, 1), s.cursorCol());
    try std.testing.expectEqual(@as(usize, 1), s.line(0).len);
    // One overlay slot per cell: a second mark is last-wins.
    s.feed("\xcc\x80"); // U+0300
    try std.testing.expectEqual(@as(u21, 0x300), s.cellAt(0, 0).mark);
    try std.testing.expectEqual(@as(usize, 1), s.cursorCol());
    // A mark after a wide glyph rides the wide base (step over the cont).
    s.feed("\xe4\xbd\xa0\xcd\x82"); // 你 + U+0342
    try std.testing.expectEqual(@as(u21, 0x4F60), s.cellAt(0, 1).base);
    try std.testing.expectEqual(@as(u21, 0x342), s.cellAt(0, 1).mark);
    try std.testing.expectEqual(@as(u1, 1), s.cellAt(0, 2).cont);
    try std.testing.expectEqual(@as(usize, 3), s.cursorCol());
    // A mark with no base behind it pins to U+FFFD — never a bare mark cell.
    var s2 = Screen{};
    s2.feed("\xcc\x81");
    try std.testing.expectEqual(@as(u21, 0xFFFD), s2.cellAt(0, 0).base);
    try std.testing.expectEqual(@as(usize, 1), s2.cursorCol());
    // Ignorable zero-width runes are dropped: no cell, cursor unchanged.
    var s3 = Screen{};
    s3.feed("a\xe2\x80\x8bb"); // a + ZWSP + b
    try std.testing.expectEqual(@as(usize, 2), s3.cursorCol());
    try std.testing.expectEqualStrings("ab", s3.line(0));
}
