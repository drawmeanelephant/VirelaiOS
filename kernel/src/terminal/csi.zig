//! CSI dispatch and rendition for `Screen` (split from terminal.zig, #3753):
//! the parameter engine, SGR including `38;5`/`38;2`, DECSET modes, and the
//! bounded DSR/CPR/DA replies.

const std = @import("std");
const cell_mod = @import("cell.zig");
const screen_mod = @import("screen.zig");
const Rgb = cell_mod.Rgb;
const colour_slot = cell_mod.colour_slot;
const default_cell_style = cell_mod.default_cell_style;
const default_colour = cell_mod.default_colour;
const rgb_colour = cell_mod.rgb_colour;
const styleBackground = cell_mod.styleBackground;
const styleBold = cell_mod.styleBold;
const styleDim = cell_mod.styleDim;
const styleForeground = cell_mod.styleForeground;
const styleItalic = cell_mod.styleItalic;
const styleReverse = cell_mod.styleReverse;
const styleUnderline = cell_mod.styleUnderline;
const Screen = screen_mod.Screen;
const grid_lines = screen_mod.grid_lines;
const reply_max = screen_mod.reply_max;

pub fn setForeground(self: *Screen, colour: colour_slot) void {
    self.style.fg = colour;
}

pub fn setBackground(self: *Screen, colour: colour_slot) void {
    self.style.bg = colour;
}

pub fn setBold(self: *Screen, on: bool) void {
    self.style.bold = on;
}

// M73h: the remaining rendition flags (SGR 2/3/4/7 and their resets).
pub fn setDim(self: *Screen, on: bool) void {
    self.style.dim = on;
}

pub fn setItalic(self: *Screen, on: bool) void {
    self.style.italic = on;
}

pub fn setUnderline(self: *Screen, on: bool) void {
    self.style.underline = on;
}

pub fn setReverse(self: *Screen, on: bool) void {
    self.style.reverse = on;
}

pub fn resetCsi(self: *Screen) void {
    self.csi_params = @as([self.csi_params.len]u16, @splat(@as(u16, 0)));
    self.csi_count = 1;
    self.csi_marker = 0;
    self.csi_malformed = false;
    self.csi_started = false;
    self.csi_intermediate = 0;
}

pub fn csiParam(self: *const Screen, param_index: usize, fallback: u16) u16 {
    if (param_index >= self.csi_count) return fallback;
    const value = self.csi_params[param_index];
    return if (value == 0) fallback else value;
}

pub fn applySgr(self: *Screen) void {
    var i: usize = 0;
    while (i < self.csi_count) : (i += 1) {
        const param = self.csi_params[i];
        switch (param) {
            0 => self.style = default_cell_style,
            1 => self.setBold(true),
            2 => self.setDim(true),
            3 => self.setItalic(true),
            4 => self.setUnderline(true),
            7 => self.setReverse(true),
            22 => {
                self.setBold(false);
                self.setDim(false);
            },
            23 => self.setItalic(false),
            24 => self.setUnderline(false),
            27 => self.setReverse(false),
            30...37 => self.setForeground(@intCast(param - 30)),
            39 => self.setForeground(default_colour),
            40...47 => self.setBackground(@intCast(param - 40)),
            49 => self.setBackground(default_colour),
            90...97 => self.setForeground(@intCast(param - 90 + 8)),
            100...107 => self.setBackground(@intCast(param - 100 + 8)),
            // M73h extended selectors: each consumes its own trailing
            // params so `1;38;5;196;48;2;1;2;3m` walks correctly.
            38 => self.applyExtendedColour(&i, true),
            48 => self.applyExtendedColour(&i, false),
            else => {}, // unknown — consumed, rendition unchanged (pinned)
        }
    }
}

/// M73h (#1634): the `38`/`48` extended colour selector — `5;n` (xterm
/// 256) or `2;r;g;b` (truecolour). `i` points at the selector; its
/// arguments are consumed past it. Invalid, out-of-range, or
/// truncated forms leave the slot unchanged — never a
/// half-interpreted colour; a truncated tail drops the remaining
/// params (they cannot be trusted as standalone SGRs). The colon (ITU)
/// sub-parameter form has no parse arm: ':' lands in the intermediate
/// skip, the digits collapse into one unknown param, and the selector
/// is ignored (pinned by the corpus).
pub fn applyExtendedColour(self: *Screen, i: *usize, fg: bool) void {
    const kind_i = i.* + 1;
    if (kind_i >= self.csi_count) return; // bare `38`/`48` — ignore
    i.* = kind_i;
    const kind = self.csi_params[kind_i];
    if (kind == 5) {
        const n_i = kind_i + 1;
        if (n_i >= self.csi_count) return; // truncated `38;5` — ignore
        i.* = n_i;
        const n = self.csi_params[n_i];
        if (n <= 255) {
            if (fg) self.setForeground(@intCast(n)) else self.setBackground(@intCast(n));
        } // out of range: slot unchanged
    } else if (kind == 2) {
        const b_i = kind_i + 3;
        if (b_i >= self.csi_count) {
            i.* = self.csi_count - 1; // truncated rgb: drop the tail
            return;
        }
        i.* = b_i;
        const r = self.csi_params[kind_i + 1];
        const g = self.csi_params[kind_i + 2];
        const b = self.csi_params[b_i];
        if (r <= 255 and g <= 255 and b <= 255) {
            const rgb = Rgb{ .r = @intCast(r), .g = @intCast(g), .b = @intCast(b) };
            if (fg) {
                self.style.fg = rgb_colour;
                self.fg_rgb_cur = rgb;
            } else {
                self.style.bg = rgb_colour;
                self.bg_rgb_cur = rgb;
            }
        } // out of range: slot unchanged (mode NOT set)
    } // any other kind: consumed and ignored
}

pub fn dispatchCsi(self: *Screen, final: u8) void {
    const p0 = self.csiParam(0, 0);
    // M182a (#3150): a private marker out of position malforms the
    // sequence — it was consumed to this final byte and nothing acts
    // on it: no arms, no replies.
    if (self.csi_malformed) return;
    if (self.csi_marker == 0 and final == 'p' and self.csi_intermediate == '!' and
        self.csi_count == 1 and p0 == 0)
    {
        self.softReset();
        return;
    }
    // M182a: '<' '=' '>' mark private sequences this parser has no
    // arms for — swallowed whole and silent. DA2 (`CSI > c`) gets no
    // reply (D17): the grid's VT100-with-AVO DA1 identity below is
    // the answer to plain `CSI c` only.
    if (self.csi_marker != 0 and self.csi_marker != '?') return;
    // M182a: with any intermediate byte present (':' included — the
    // pinned ITU sub-parameter skip) every plain and private arm is
    // disarmed; DECSTR's exact check above is the lone exception.
    if (self.csi_intermediate != 0) return;
    // M80f (#1723): replies are output-parser side effects. Keep them
    // bounded and ordered with other synthesised input; the session pump
    // drains this FIFO after every grid feed.
    if (self.csi_marker == 0) {
        if (final == 'n' and p0 == 5 and self.csi_count == 1) self.appendReply("\x1b[0n");
        if (final == 'n' and p0 == 6 and self.csi_count == 1) self.appendReply(self.cursorReply());
        if (final == 'c' and p0 == 0 and self.csi_count == 1) {
            // VT100 with AVO: the grid is not xterm, and this is the
            // oldest honest DA answer accepted by terminal clients.
            self.appendReply("\x1b[?1;2c");
        }
    }
    if (self.csi_marker == '?') {
        if (p0 == 6) {
            if (final == 'h') self.setOriginMode(true);
            if (final == 'l') self.setOriginMode(false);
        }
        if (p0 == 7) {
            if (final == 'h' or final == 'l') {
                self.autowrap = final == 'h';
                self.cancelPendingWrap();
            }
        }
        if (p0 == 47 or p0 == 1049) {
            if (final == 'h') self.setAlternate(true);
            if (final == 'l') self.setAlternate(false);
        }
        if (p0 == 25) {
            if (final == 'h') self.cursor_visible = true;
            if (final == 'l') self.cursor_visible = false;
        }
        // M73e (#1629): bracketed-paste mode — tracked, never painted.
        if (p0 == 2004) {
            self.bracketed_paste = (final == 'h');
        }
        // M73i (#1635): mouse tracking modes — consumed, never painted.
        if (p0 == 1000) self.mouse_1000 = (final == 'h');
        if (p0 == 1002) self.mouse_1002 = (final == 'h');
        if (p0 == 1003) self.mouse_1003 = (final == 'h');
        if (p0 == 1006) self.mouse_1006 = (final == 'h');
        return;
    }
    switch (final) {
        'm' => {
            self.cancelPendingWrap();
            self.applySgr();
        },
        'H', 'f' => self.moveCursor(self.csiParam(0, 1), self.csiParam(1, 1)),
        'J' => self.eraseDisplay(p0),
        'K' => self.eraseLine(p0),
        // M80a (#1712): cursor motion finals. Only param 0 is read (a
        // second param is ignored, like xterm) and the rendition is
        // never touched. Relative moves clamp at the grid edges and
        // never grow `used`; E/F are CNL/CPL — column 0, NOT "down/up
        // and keep the column"; G is the absolute column, d the
        // absolute row (grown like CUP via moveCursor, column kept).
        'A' => self.cursorUp(self.csiParam(0, 1)),
        'B' => self.cursorDown(self.csiParam(0, 1)),
        'C' => {
            self.cancelPendingWrap();
            self.col = @min(self.col + @as(usize, self.csiParam(0, 1)), self.cols - 1);
        },
        'D' => {
            self.cancelPendingWrap();
            self.col = self.col -| @as(usize, self.csiParam(0, 1));
        },
        'E' => {
            self.cursorDown(self.csiParam(0, 1));
            self.col = 0;
        },
        'F' => {
            self.cursorUp(self.csiParam(0, 1));
            self.col = 0;
        },
        'G' => {
            self.cancelPendingWrap();
            self.col = @min(@as(usize, self.csiParam(0, 1) - 1), self.cols - 1);
        },
        'd' => self.moveCursor(self.csiParam(0, 1), @intCast(self.col + 1)),
        'r' => self.setScrollRegion(self.csiParam(0, 1), self.csiParam(1, @intCast(grid_lines))),
        'b' => self.repeatLast(self.csiParam(0, 1)),
        // M80b (#1713): insert/delete/erase finals. @/P/X act at the
        // cursor on its row (a pending wrap stands on the last
        // column); L/M insert/delete whole rows and take the cursor
        // to the left margin (xterm's VT102-compatible IL/DL); S/T
        // scroll the grid and never touch the cursor. Only param 0
        // is read — except T, where a five-parameter form is
        // XTHIMOUSE (highlight tracking), not SD: unsupported here,
        // so consumed and never painted (the M80 rule).
        '@' => self.insertChars(self.csiParam(0, 1)),
        'P' => self.deleteChars(self.csiParam(0, 1)),
        'X' => self.eraseChars(self.csiParam(0, 1)),
        'L' => self.insertLines(self.csiParam(0, 1)),
        'M' => self.deleteLines(self.csiParam(0, 1)),
        'S' => self.scrollUp(self.csiParam(0, 1)),
        'T' => {
            if (self.csi_count == 1) self.scrollDown(self.csiParam(0, 1));
        },
        // M80d (#1721): tab-stop control. TBC 0 clears the stop under
        // the cursor; TBC 3 clears all. CHT/CBT move by a stop count
        // (default 1). Other TBC modes are consumed and ignored.
        'g' => switch (p0) {
            0 => self.clearTabStop(self.col),
            3 => self.activeTabStops().* = .{ 0, 0 },
            else => {},
        },
        'I' => self.moveTabStops(self.csiParam(0, 1), true),
        'Z' => self.moveTabStops(self.csiParam(0, 1), false),
        else => {},
    }
}

pub fn cursorReply(self: *const Screen) []const u8 {
    // This buffer is module scratch, like line(): one consumer at a time,
    // consumed by appendReply before dispatchCsi returns.
    cursor_reply_scratch[0] = 0x1b;
    cursor_reply_scratch[1] = '[';
    var n: usize = writeDecimal(cursor_reply_scratch[2..], self.cur + 1);
    cursor_reply_scratch[n + 2] = ';';
    n += 1;
    const report_col = if (self.col >= self.cols) self.cols else self.col + 1;
    n += 2 + writeDecimal(cursor_reply_scratch[n + 2 ..], report_col);
    cursor_reply_scratch[n] = 'R';
    return cursor_reply_scratch[0 .. n + 1];
}

pub fn writeDecimal(buf: []u8, value: usize) usize {
    var n: usize = 0;
    var digits: [20]u8 = undefined;
    var v = value;
    while (v > 0) : (v /= 10) {
        digits[n] = @intCast('0' + v % 10);
        n += 1;
    }
    var i: usize = 0;
    while (i < n) : (i += 1) buf[i] = digits[n - i - 1];
    return n;
}

pub fn appendReply(self: *Screen, bytes: []const u8) void {
    const room = reply_max - self.pending_reply_len;
    const n = @min(room, bytes.len);
    @memcpy(self.pending_reply[self.pending_reply_len..][0..n], bytes[0..n]);
    self.pending_reply_len += n;
}

/// M80f (#1723): output-parser reply scratch, consumed immediately by appendReply.
var cursor_reply_scratch: [reply_max]u8 = undefined;

// M182a (#3150): '<' '=' '>' '?' (0x3c-0x3f) are ECMA-48 private parameter
// markers. '?' is the only marker with arms here; the others swallow the
// sequence whole — no SGR, no DA1 for `CSI > c` (D17).
test "terminal: private markers other than ? swallow the sequence, no SGR" {
    var s = Screen{};
    s.feed("\x1b[>4;1mX");
    try std.testing.expectEqualStrings("X", s.line(0));
    const style = s.styleAt(0, 0);
    try std.testing.expectEqual(default_cell_style, style);
    try std.testing.expect(!styleBold(style));
    try std.testing.expect(!styleUnderline(style));
}

// M73e (#1629): DECSET 2004 state, the bracketed wrap, and the queue
// contract a full clipboard paste needs.
test "terminal: DECSET/DECRST 2004 tracks bracketed paste mode" {
    var s = Screen{};
    try std.testing.expect(!s.bracketed_paste);
    s.feed("ok\x1b[?2004h");
    try std.testing.expect(s.bracketed_paste);
    try std.testing.expectEqualStrings("ok", s.line(0));
    s.feed("\x1b[?2004l");
    try std.testing.expect(!s.bracketed_paste);
    // Mode bits survive an alternate-screen round trip (like DECTCEM).
    s.feed("\x1b[?2004h\x1b[?1049h\x1b[?1049l");
    try std.testing.expect(s.bracketed_paste);
}

// ---------------------------------------------------------------------------
// M73h (#1634, ADR 0020 Amendment E): rendition depth — xterm 256,
// truecolour side arrays, the new flags, and their resets. The 16-colour
// behaviour tests above must stay green UNCHANGED.
// ---------------------------------------------------------------------------
test "terminal: SGR 38;5/48;5 store xterm 256 indices" {
    var s = Screen{};
    s.feed("\x1b[38;5;196mA\x1b[48;5;21mB");
    try std.testing.expectEqual(@as(?u8, 196), styleForeground(s.styleAt(0, 0)));
    try std.testing.expectEqual(@as(?u8, 21), styleBackground(s.styleAt(0, 1)));
    try std.testing.expectEqual(@as(?u8, 196), styleForeground(s.styleAt(0, 1)));
    // Index 16 must be a REAL colour, not the old sentinel: the u9 slot
    // moved the default to 256 exactly so this cannot read as "default".
    s.feed("\x1b[38;5;16mX");
    try std.testing.expectEqual(@as(?u8, 16), styleForeground(s.styleAt(0, 2)));
    // A palette cell stores no RGB (the rgbAt gate is the slot).
    try std.testing.expectEqual(@as(?Rgb, null), s.rgbAt(0, 2).fg);
}

test "terminal: CSI SGR and CUP preserve per-cell attributes" {
    var s = Screen{};
    // A Bubble-Tea-shaped paint burst: clear, position, set SGR, paint, reset.
    s.feed("\x1b[2J\x1b[3;4H\x1b[31mR\x1b[1;44;97mB\x1b[0mN");
    try std.testing.expectEqualStrings("   RBN", s.line(2));
    try std.testing.expectEqual(@as(usize, 2), s.cursorLine());
    try std.testing.expectEqual(@as(usize, 6), s.cursorCol());

    const red = s.styleAt(2, 3);
    try std.testing.expectEqual(@as(?u8, 1), styleForeground(red));
    try std.testing.expectEqual(@as(?u8, null), styleBackground(red));
    try std.testing.expect(!styleBold(red));

    const bright = s.styleAt(2, 4);
    try std.testing.expectEqual(@as(?u8, 15), styleForeground(bright));
    try std.testing.expectEqual(@as(?u8, 4), styleBackground(bright));
    try std.testing.expect(styleBold(bright));

    const reset = s.styleAt(2, 5);
    try std.testing.expectEqual(@as(?u8, null), styleForeground(reset));
    try std.testing.expectEqual(@as(?u8, null), styleBackground(reset));
    try std.testing.expect(!styleBold(reset));
}

test "terminal: DSR, CPR, and DA synthesize bounded input replies" {
    var s = Screen{};
    s.feed("\x1b[2;3H\x1b[5n\x1b[6n\x1b[c");
    try std.testing.expectEqual(@as(usize, 17), s.pending_reply_len);
    try std.testing.expectEqualStrings("\x1b[0n\x1b[2;3R\x1b[?1;2c", s.pending_reply[0..s.pending_reply_len]);
}

test "terminal: DSR private and malformed forms stay silent" {
    var s = Screen{};
    s.feed("\x1b[?6n\x1b[6;99n\x1b[2n");
    try std.testing.expectEqual(@as(usize, 0), s.pending_reply_len);
}

test "terminal: unsupported CSI is swallowed rather than painted" {
    var s = Screen{};
    s.feed("before\x1b[999zafter");
    try std.testing.expectEqualStrings("beforeafter", s.line(0));
}

test "terminal: private-marker queries DA2/= / < stay silent" {
    var s = Screen{};
    s.feed("\x1b[>c\x1b[=c\x1b[<u");
    try std.testing.expectEqual(@as(usize, 0), s.pending_reply_len);
    try std.testing.expectEqualStrings("", s.line(0));
}

test "terminal: a late or second private marker is malformed — nothing acts" {
    var s = Screen{};
    // A marker after the first parameter byte: swallowed, modes unchanged.
    s.feed("\x1b[?25l\x1b[1?h");
    try std.testing.expect(!s.cursor_visible);
    // A second marker: `CSI ? ? 25 h` must not re-show the cursor.
    s.feed("\x1b[??25h");
    try std.testing.expect(!s.cursor_visible);
    // The marker itself still works: `?` switches screens.
    s.feed("\x1b[?1049h");
    try std.testing.expect(s.alt_active);
}

test "terminal: an intermediate byte disarms plain CSI, DECSTR excepted" {
    var s = Screen{};
    // `CSI ! 5 n` is not DSR: the intermediate disarms the reply arm.
    s.feed("\x1b[!5n");
    try std.testing.expectEqual(@as(usize, 0), s.pending_reply_len);
    // `CSI ! m` is not SGR: consumed, rendition untouched.
    s.feed("\x1b[!4mX");
    try std.testing.expectEqualStrings("X", s.line(0));
    try std.testing.expect(!styleUnderline(s.styleAt(0, 0)));
    // DECSTR keeps its exact check and still soft-resets.
    s.feed("\x1b[?25l\x1b[!p");
    try std.testing.expect(s.cursor_visible);
}

test "terminal: SGR 38;2/48;2 stores truecolour in the side arrays" {
    var s = Screen{};
    s.feed("\x1b[38;2;255;128;71;48;2;17;34;51mX");
    try std.testing.expectEqual(rgb_colour, s.styleAt(0, 0).fg);
    try std.testing.expectEqual(rgb_colour, s.styleAt(0, 0).bg);
    // The accessor contract: truecolour reads as "no palette index" —
    // paint callers check the slot first and fall through to rgbAt.
    try std.testing.expectEqual(@as(?u8, null), styleForeground(s.styleAt(0, 0)));
    const fg = s.rgbAt(0, 0).fg.?;
    const bg = s.rgbAt(0, 0).bg.?;
    try std.testing.expectEqual(@as(u8, 255), fg.r);
    try std.testing.expectEqual(@as(u8, 128), fg.g);
    try std.testing.expectEqual(@as(u8, 71), fg.b);
    try std.testing.expectEqual(@as(u8, 17), bg.r);
    try std.testing.expectEqual(@as(u8, 34), bg.g);
    try std.testing.expectEqual(@as(u8, 51), bg.b);
    // `39` returns the slot to default (rgb no longer applies).
    s.feed("\x1b[39mY");
    try std.testing.expectEqual(default_colour, s.styleAt(0, 1).fg);
    try std.testing.expectEqual(@as(?Rgb, null), s.rgbAt(0, 1).fg);
    // Out-of-range components never become a colour: the slot keeps its
    // PREVIOUS value (still the rgb marker from the earlier 48;2) and the
    // stored channels are untouched.
    s.feed("\x1b[48;2;300;0;0mZ");
    try std.testing.expectEqual(rgb_colour, s.styleAt(0, 2).bg);
    const bg2 = s.rgbAt(0, 2).bg.?;
    try std.testing.expectEqual(@as(u8, 17), bg2.r);
    try std.testing.expectEqual(@as(u8, 34), bg2.g);
    try std.testing.expectEqual(@as(u8, 51), bg2.b);
}

test "terminal: truecolour rides the alternate round trip" {
    var s = Screen{};
    s.feed("\x1b[38;2;10;20;30mA"); // primary: rgb A
    s.feed("\x1b[?1049h");
    s.feed("\x1b[38;2;40;50;60mB"); // alternate: rgb B
    s.feed("\x1b[?1049l");
    // The style word swapped back to the primary; if the rgb side arrays
    // had NOT swapped with it, rgbAt here would report B's channels.
    try std.testing.expectEqual(@as(u21, 'A'), s.cellAt(0, 0).base);
    const fg = s.rgbAt(0, 0).fg.?;
    try std.testing.expectEqual(@as(u8, 10), fg.r);
    try std.testing.expectEqual(@as(u8, 20), fg.g);
    try std.testing.expectEqual(@as(u8, 30), fg.b);
    // The state mirror swapped too: the next write on the primary keeps
    // rgb A even though the last 38;2 seen was B's.
    s.feed("C");
    try std.testing.expectEqual(@as(u8, 30), s.rgbAt(0, 1).fg.?.b);
}

test "terminal: extended selectors consume their params; invalid forms are ignored" {
    var s = Screen{};
    // Index walk: flags AFTER a full selector still apply — this is the
    // CURRENT rendition (no glyph written yet, so styleAt would be blank).
    s.feed("\x1b[1;38;5;196;4m");
    try std.testing.expectEqual(true, styleBold(s.style));
    try std.testing.expectEqual(@as(?u8, 196), styleForeground(s.style));
    try std.testing.expectEqual(true, styleUnderline(s.style));
    // Out-of-range index: rendition unchanged.
    s.feed("\x1b[38;5;999m");
    try std.testing.expectEqual(@as(?u8, 196), styleForeground(s.style)); // rendition untouched
    // Truncated rgb: the tail is DROPPED, never re-read as standalone
    // SGRs (31 would have turned the foreground red).
    s.feed("\x1b[38;2;31mX");
    try std.testing.expectEqual(@as(?u8, 196), styleForeground(s.styleAt(0, 0)));
    try std.testing.expectEqual(@as(u21, 'X'), s.cellAt(0, 0).base);
    // A bare selector and an unknown kind are consumed, not painted.
    s.feed("\x1b[38m\x1b[48;7mY");
    try std.testing.expectEqual(@as(?u8, 196), styleForeground(s.styleAt(0, 1)));
    try std.testing.expectEqual(@as(?u8, null), styleBackground(s.styleAt(0, 1)));
    // The colon (ITU) sub-parameter form has no arm: digits collapse into
    // one unknown param (3859) and everything stays unchanged.
    s.feed("\x1b[38:5:9mZ");
    try std.testing.expectEqual(@as(?u8, 196), styleForeground(s.styleAt(0, 2)));
    try std.testing.expectEqual(@as(u21, 'Z'), s.cellAt(0, 2).base);
}

test "terminal: rendition flags 2/3/4/7 and their resets" {
    var s = Screen{};
    s.feed("\x1b[2;3;4;7mA");
    try std.testing.expectEqual(true, styleDim(s.styleAt(0, 0)));
    try std.testing.expectEqual(true, styleItalic(s.styleAt(0, 0)));
    try std.testing.expectEqual(true, styleUnderline(s.styleAt(0, 0)));
    try std.testing.expectEqual(true, styleReverse(s.styleAt(0, 0)));
    try std.testing.expectEqual(false, styleBold(s.styleAt(0, 0)));
    s.feed("\x1b[22;23;24;27mB");
    try std.testing.expectEqual(false, styleDim(s.styleAt(0, 1)));
    try std.testing.expectEqual(false, styleItalic(s.styleAt(0, 1)));
    try std.testing.expectEqual(false, styleUnderline(s.styleAt(0, 1)));
    try std.testing.expectEqual(false, styleReverse(s.styleAt(0, 1)));
    // SGR 0 clears EVERYTHING — flags included, byte-exact default.
    // (A and B above already consumed columns 0 and 1, so A' lands at 2
    // and the reset cell B' at 3.)
    s.feed("\x1b[1;4;31mA\x1b[0mB");
    try std.testing.expectEqual(true, styleUnderline(s.styleAt(0, 2)));
    try std.testing.expectEqual(default_cell_style, s.styleAt(0, 3));
    try std.testing.expectEqual(false, styleBold(s.style));
    try std.testing.expectEqual(false, styleUnderline(s.style));
    try std.testing.expectEqual(@as(?u8, null), styleForeground(s.style));
}

test "terminal: a 12-param SGR line fits the widened csi_params" {
    // Before M73h this overflowed [8]: params 9.. dropped, so 48;5;9 was
    // lost. 12 params now land in full.
    var s = Screen{};
    s.feed("\x1b[1;4;7;3;38;2;1;2;3;48;5;9mX");
    try std.testing.expectEqual(true, styleBold(s.styleAt(0, 0)));
    try std.testing.expectEqual(true, styleUnderline(s.styleAt(0, 0)));
    try std.testing.expectEqual(true, styleReverse(s.styleAt(0, 0)));
    try std.testing.expectEqual(true, styleItalic(s.styleAt(0, 0)));
    const fg = s.rgbAt(0, 0).fg.?;
    try std.testing.expectEqual(@as(u8, 1), fg.r);
    try std.testing.expectEqual(@as(u8, 2), fg.g);
    try std.testing.expectEqual(@as(u8, 3), fg.b);
    try std.testing.expectEqual(@as(?u8, 9), styleBackground(s.styleAt(0, 0)));
}
