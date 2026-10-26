//! OSC/DCS string handling for `Screen` (split from terminal.zig, #3753):
//! window titles, the OSC 52 clipboard copy, sixel DCS framing, and the strict
//! base64 decode a refused copy needs.

const std = @import("std");
const clipboard = @import("../clipboard.zig");
const image_mod = @import("image.zig");
const registry = @import("../terminal.zig");
const screen_mod = @import("screen.zig");
const window_mod = @import("window.zig");
const imageBankOf = image_mod.imageBankOf;
const create = registry.create;
const get = registry.get;
const Screen = screen_mod.Screen;
const title_max = screen_mod.title_max;
const screenForWindow = window_mod.screenForWindow;
const writeWindow = window_mod.writeWindow;

/// M80g (#1715): drop the in-flight OSC string (abort, or after a
/// dispatch consumed it).
pub fn oscAbort(self: *Screen) void {
    self.esc_state = 0;
    self.osc_len = 0;
    self.osc_overflow = false;
}

/// M80g: apply one COMPLETE OSC string — leading digits are Ps, `;`
/// separates them from the payload. Malformed forms, over-bound
/// strings and unknown Ps are consumed and dropped ("consumed, never
/// painted"); Ps 0/2 and 52 queue their effect for the pump. The grid
/// is never touched from here.
pub fn dispatchOsc(self: *Screen) void {
    const len = self.osc_len;
    const overflow = self.osc_overflow;
    self.osc_len = 0;
    self.osc_overflow = false;
    if (overflow) return;
    const raw = self.osc_buf[0..len];
    var i: usize = 0;
    while (i < len and raw[i] >= '0' and raw[i] <= '9') : (i += 1) {}
    if (i == 0 or i == len or raw[i] != ';') return;
    var num: usize = 0;
    for (raw[0..i]) |d| num = num *% 10 +% (d - '0');
    const payload = raw[i + 1 ..];
    switch (num) {
        0, 2 => self.setTitle(payload),
        52 => self.osc52(payload),
        else => {},
    }
}

/// M80g: queue a window title (Ps 0/2). Empty is refused — a tab
/// keeps its name (the seat refuses empty kind-11 titles too); a
/// longer title truncates honestly at `title_max`.
pub fn setTitle(self: *Screen, payload: []const u8) void {
    if (payload.len == 0) return;
    const n = @min(payload.len, title_max);
    @memcpy(self.title[0..n], payload[0..n]);
    self.title_len = n;
    self.title_dirty = true;
}

/// M80g: OSC 52 — `c;<base64>` queues a copy into the shared clipboard
/// (the pump bridges it). `c` is the only target; any other selector
/// is dropped. A `?` read query is refused honestly — privacy: no
/// reply is generated and nothing is copied. Malformed base64 refuses
/// WHOLE (decode to scratch, commit only on success); a valid but
/// over-long result truncates at the clipboard bound.
pub fn osc52(self: *Screen, payload: []const u8) void {
    const sep = std.mem.indexOfScalar(u8, payload, ';') orelse return;
    if (!std.mem.eql(u8, payload[0..sep], "c")) return;
    const data = payload[sep + 1 ..];
    if (std.mem.eql(u8, data, "?")) return;
    var scratch: [clipboard.capacity]u8 = undefined;
    const n = base64Decode(data, &scratch) orelse return;
    @memcpy(self.clip_data[0..n], scratch[0..n]);
    self.clip_len = n;
    self.clip_dirty = true;
}

/// M85a (#1813): open an ESC string — DCS `P` (sixel's channel), APC
/// `_` (kitty's), SOS `X`, PM `^`. All four are consumed through ST;
/// only a DCS whose final is `q` (sixel) decodes, and M85b (#1814)
/// places the image at the cursor recorded here.
pub fn dcsBegin(self: *Screen, intro: u8) void {
    self.esc_state = 6;
    self.dcs_intro = intro;
    self.dcs_phase = 0;
    self.sixel_p = @as([3]u16, @splat(@as(u16, 0)));
    self.sixel_pcount = 0;
    self.sixel_overflow = false;
    self.sixel_origin_line = self.cur;
    self.sixel_origin_col = self.col;
}

/// M85a: one byte of an ESC string. Phase 0 collects the DCS parameter
/// string — digits and `;` into at most three Ps values, everything
/// else consumed — until the first final byte (0x40–0x7E): `q` after a
/// DCS starts the sixel decode (M85b), any other final or introducer
/// opens a pure drain. C0 bytes inside a string are consumed as
/// payload: never executed, never painted.
pub fn dcsByte(self: *Screen, b: u8) void {
    switch (self.dcs_phase) {
        0 => {
            if (b >= '0' and b <= '9') {
                if (self.sixel_pcount < self.sixel_p.len) {
                    self.sixel_p[self.sixel_pcount] = self.sixel_p[self.sixel_pcount] *% 10 +% (b - '0');
                }
                return;
            }
            if (b == ';') {
                // Params beyond three are consumed, never parsed: the
                // selector saturates at 3 (= exhausted) and the digit
                // guard then drops their digits.
                self.sixel_pcount = @min(self.sixel_pcount + 1, self.sixel_p.len);
                return;
            }
            if (b >= 0x40 and b <= 0x7e) {
                self.dcs_phase = if (self.dcs_intro == 'P' and b == 'q') 1 else 2;
                if (self.dcs_phase == 1) self.imageBegin();
            }
            return;
        },
        1 => {
            const bank = imageBankOf(self) orelse return self.sixel.refuse();
            self.sixel.feed(&bank.img[self.sixel.stage], b);
        },
        else => {}, // phase 2: pure drain
    }
}

/// M85a: ST completed the string. M85b: a sixel string commits its
/// image — placed, refused whole (`sixel_overflow`, sticky until the
/// next string begins), or nothing at all for an empty one. Every
/// other string was consumed and is simply over ("never painted").
pub fn dcsFinish(self: *Screen) void {
    self.esc_state = 0;
    if (self.dcs_phase == 1) self.imageCommit();
}

/// M85a: drop the in-flight string whole (CAN/SUB, or an ESC that did
/// not complete ST). A half-decoded image only ever touched its
/// staging buffer, so dropping it costs nothing on screen.
pub fn dcsAbort(self: *Screen) void {
    self.esc_state = 0;
}

/// M80g: strict RFC 4648 base64 decode with padding. Returns null on any
/// malformed input (alphabet byte outside the table, padding before the
/// tail, data after padding, length not a multiple of 4) so a refused
/// OSC 52 copy never partially lands — `dst` may hold a partial write
/// when a LATER group fails, so callers commit only on non-null (the
/// osc52 scratch). A valid but over-long result truncates honestly at
/// `dst.len` (the clipboard bound, the ipc/udp truncation pattern).
fn base64Decode(src: []const u8, dst: []u8) ?usize {
    if (src.len % 4 != 0) return null;
    var out: usize = 0;
    var g: usize = 0;
    while (g < src.len) : (g += 4) {
        const last = (g + 4) == src.len;
        var v: [4]u8 = @as([4]u8, @splat(@as(u8, 0)));
        var pads: u8 = 0;
        for (0..4) |k| {
            const c = src[g + k];
            if (c == '=') {
                // '=' only in the final group, only the last two slots;
                // once it starts it must run to the end of the string.
                if (!last or k < 2) return null;
                if (k == 2 and src[g + 3] != '=') return null;
                pads += 1;
                continue;
            }
            if (pads > 0) return null;
            v[k] = base64Value(c) orelse return null;
        }
        const triple: u32 = (@as(u32, v[0]) << 18) | (@as(u32, v[1]) << 12) |
            (@as(u32, v[2]) << 6) | @as(u32, v[3]);
        const shifts = [_]u5{ 16, 8, 0 };
        const emit: usize = 3 - pads;
        var k: usize = 0;
        while (k < emit) : (k += 1) {
            if (out < dst.len) dst[out] = @truncate(triple >> shifts[k]);
            out += 1;
        }
    }
    return @min(out, dst.len);
}

/// M80g: one base64 sextet, strict alphabet (no URL-safe variants).
fn base64Value(c: u8) ?u8 {
    return switch (c) {
        'A'...'Z' => c - 'A',
        'a'...'z' => c - 'a' + 26,
        '0'...'9' => c - '0' + 52,
        '+' => 62,
        '/' => 63,
        else => null,
    };
}

// ---------------------------------------------------------------------------
// M80g (#1715) — OSC delivery: window/seat titles, the clipboard bridge
// ---------------------------------------------------------------------------
test "terminal: OSC titles queue on the Screen and the pump delivers them once (#1715)" {
    for (&registry.terminals) |*tm| tm.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const tm = get(h).?;
    try std.testing.expect(tm.attachWindow(7));
    const s = screenForWindow(7).?;
    _ = writeWindow(h, "\x1b]0;desk\x07\x1b]2;edit\x1b\\");
    // Last writer wins; the pump already handed the queue to the delivery
    // seam (the dirty flag is spent) and the store keeps the title.
    try std.testing.expectEqualStrings("edit", s.title[0..s.title_len]);
    try std.testing.expect(!s.title_dirty);
    // A title past the bound truncates honestly at title_max.
    const long: [70]u8 = @as([70]u8, @splat(@as(u8, 't')));
    _ = writeWindow(h, "\x1b]2;");
    _ = writeWindow(h, &long);
    _ = writeWindow(h, "\x07");
    try std.testing.expectEqual(@as(usize, title_max), s.title_len);
    try std.testing.expectEqualStrings(long[0..title_max], s.title[0..s.title_len]);
    // An empty title is refused: the stored title stays the last one.
    _ = writeWindow(h, "\x1b]0;\x07");
    try std.testing.expectEqualStrings(long[0..title_max], s.title[0..s.title_len]);
    // RIS clears the stored queue (nothing is delivered as a blank title).
    _ = writeWindow(h, "\x1bc");
    try std.testing.expectEqual(@as(usize, 0), s.title_len);
    try std.testing.expect(!s.title_dirty);
}

test "terminal: OSC 52 copies into the kernel clipboard through the pump (#1715)" {
    clipboard.init();
    for (&registry.terminals) |*tm| tm.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const tm = get(h).?;
    try std.testing.expect(tm.attachWindow(7));
    const s = screenForWindow(7).?;
    _ = writeWindow(h, "\x1b]52;c;aGVsbG8=\x07");
    var buf: [16]u8 = undefined;
    const n = clipboard.get(&buf);
    try std.testing.expectEqualStrings("hello", buf[0..n]);
    try std.testing.expect(!s.clip_dirty);
    try std.testing.expectEqual(@as(u64, 1), clipboard.sets());
    clipboard.init();
}

test "terminal: an OSC 52 read query is refused: no reply, no copy (#1715)" {
    clipboard.init();
    _ = clipboard.set("kept");
    for (&registry.terminals) |*tm| tm.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const tm = get(h).?;
    try std.testing.expect(tm.attachWindow(7));
    const s = screenForWindow(7).?;
    _ = writeWindow(h, "\x1b]52;c;?\x07");
    var buf: [16]u8 = undefined;
    const n = clipboard.get(&buf);
    try std.testing.expectEqualStrings("kept", buf[0..n]);
    try std.testing.expectEqual(@as(usize, 0), s.pending_reply_len);
    try std.testing.expect(!s.clip_dirty);
    try std.testing.expectEqual(@as(usize, 0), s.clip_len);
    clipboard.init();
}

test "terminal: malformed OSC 52 base64 is refused whole (#1715)" {
    clipboard.init();
    _ = clipboard.set("kept");
    for (&registry.terminals) |*tm| tm.reset();
    for (&registry.screens) |*sc| sc.reset();
    const h = create(7) orelse return error.TestUnexpectedResult;
    const tm = get(h).?;
    try std.testing.expect(tm.attachWindow(7));
    const s = screenForWindow(7).?;
    // Valid first group, invalid byte later: nothing lands, ever.
    _ = writeWindow(h, "\x1b]52;c;aGVsaG!=\x07");
    var buf: [16]u8 = undefined;
    const n = clipboard.get(&buf);
    try std.testing.expectEqualStrings("kept", buf[0..n]);
    try std.testing.expect(!s.clip_dirty);
    try std.testing.expectEqual(@as(usize, 0), s.clip_len);
    clipboard.init();
}

test "terminal: base64Decode is strict, padded, and truncates at the bound (#1715)" {
    var dst: [512]u8 = undefined;
    var n = base64Decode("aGVsbG8=", &dst) orelse return error.TestUnexpectedResult;
    try std.testing.expectEqualStrings("hello", dst[0..n]);
    n = base64Decode("", &dst) orelse return error.TestUnexpectedResult;
    try std.testing.expectEqual(@as(usize, 0), n);
    n = base64Decode("aH==", &dst) orelse return error.TestUnexpectedResult; // double pad
    try std.testing.expectEqualStrings("h", dst[0..n]);
    // Unpadded, padding before the tail, padding in a non-final group:
    // each refuses WHOLE (null), never a partial commit.
    try std.testing.expect(base64Decode("aGVsbG8", &dst) == null);
    try std.testing.expect(base64Decode("a=Gs", &dst) == null);
    try std.testing.expect(base64Decode("aGVs=bG8", &dst) == null);
    // A valid over-long result truncates honestly at dst.len.
    var wide: [800]u8 = undefined;
    var i: usize = 0;
    while (i < 200) : (i += 1) @memcpy(wide[i * 4 ..][0..4], "eHh4"); // "xxx", no padding
    n = base64Decode(wide[0..800], &dst) orelse return error.TestUnexpectedResult;
    try std.testing.expectEqual(@as(usize, 512), n);
    try std.testing.expectEqual(@as(u8, 'x'), dst[511]);
}
