//! The `Terminal` byte session (split from terminal.zig, #3753): the
//! output ring, input queue, exclusive front-end attach, mouse staging and
//! net-auth state — pure bytes, no presentation.

const std = @import("std");
const console = @import("../console.zig");
const tcp = @import("../tcp.zig");
const net_mod = @import("net.zig");
const registry = @import("../terminal.zig");
const NetAuthScheme = net_mod.NetAuthScheme;
const net_auth_line_max = net_mod.net_auth_line_max;
const net_challenge_len = net_mod.net_challenge_len;

/// Output ring capacity (bytes the owner has written, awaiting a front-end).
pub const out_capacity: usize = 4096;

/// Input queue capacity (bytes a front-end has pushed, awaiting the owner).
pub const in_capacity: usize = 1024;

/// The consumer that renders output and supplies input. Exclusive per
/// terminal: one front-end at a time (ADR 0020 D2).
pub const FrontEnd = enum(u8) {
    none = 0,
    /// The raw virtio serial console.
    serial = 1,
    /// A TABWM desktop terminal window (TERM.BIN).
    window = 2,
    /// A network session (remote console / SSH channel).
    net = 3,
};

/// A terminal session. All methods are pure; the struct is `extern`-free and
/// host-testable as a value.
pub const Terminal = struct {
    out: [out_capacity]u8 = @as([out_capacity]u8, @splat(@as(u8, 0))),
    out_start: usize = 0,
    out_len: usize = 0,
    out_dropped: u64 = 0,

    in: [in_capacity]u8 = @as([in_capacity]u8, @splat(@as(u8, 0))),
    in_start: usize = 0,
    in_len: usize = 0,
    /// M73i (#1635): the single newest motion-class report, held out of the
    /// queue so a `?1003` motion storm coalesces to one line per read. Edge
    /// events flush it first (chronological order preserved) and a read
    /// drains it first. 32 B covers the longest SGR report.
    motion_pending: [32]u8 = undefined,
    motion_len: usize = 0,
    in_dropped: u64 = 0,

    attached: bool = false,
    front_end: FrontEnd = .none,
    owner_pid: ?usize = null,
    in_use: bool = false,
    /// #1082 (ADR 0020 Amendment A): the `.user` window this terminal is a
    /// front-end for, when `front_end == .window`. Null otherwise.
    window_id: ?u8 = null,
    /// #1083 (ADR 0020 Amendment B): the TCP listen port this terminal is a
    /// front-end for, when `front_end == .net`. 0 otherwise.
    net_port: u16 = 0,
    /// M50 TS4 (#1138, ADR 0024 D6): the delegated net-auth state. When
    /// `net_auth_on`, the pump mints a fresh 32-byte challenge once the
    /// connection is ESTABLISHED, frames it as
    /// `VIRELAIOS-AUTH/1 <scheme> <hex>\n`, buffers the client's one-line
    /// reply, and gates every post-challenge byte on the attached process's
    /// verdict. `net_authed` is the byte gate; `net_reply_ready` means a
    /// well-formed reply awaits the verdict; `net_auth_ticks` is the
    /// challenge clock (10 s deadline). All cleared on detach/reset.
    net_auth_on: bool = false,
    net_auth_scheme: NetAuthScheme = .open,
    net_authed: bool = true, // no auth => open (SH7 behavior)
    net_challenge: [net_challenge_len]u8 = @as([net_challenge_len]u8, @splat(@as(u8, 0))),
    net_challenge_sent: bool = false,
    net_auth_ticks: u64 = 0,
    net_reply: [net_auth_line_max]u8 = @as([net_auth_line_max]u8, @splat(@as(u8, 0))),
    net_reply_len: usize = 0,
    net_reply_ready: bool = false,
    /// Bytes pipelined after the reply line (the client's first command):
    /// held until the verdict accepts, then delivered — never before.
    net_post: [tcp.payload_max]u8 = @as([tcp.payload_max]u8, @splat(@as(u8, 0))),
    net_post_len: usize = 0,
    net_verdict: ?bool = null,
    net_allow_ip: [4]u8 = .{ 0, 0, 0, 0 },
    net_allow_on: bool = false,

    /// Append owner output to the ring. Always accepts every byte; when the
    /// ring is full it drops the oldest byte and counts it. Returns bytes
    /// accepted (== `bytes.len`).
    pub fn write(self: *Terminal, bytes: []const u8) usize {
        for (bytes) |b| {
            if (self.out_len == out_capacity) {
                self.out_start = (self.out_start + 1) % out_capacity;
                self.out_len -= 1;
                self.out_dropped += 1;
            }
            self.out[(self.out_start + self.out_len) % out_capacity] = b;
            self.out_len += 1;
        }
        return bytes.len;
    }

    /// Drain up to `buf.len` output bytes, oldest first. Returns the count.
    pub fn readOut(self: *Terminal, buf: []u8) usize {
        const n = @min(buf.len, self.out_len);
        for (0..n) |i| buf[i] = self.out[(self.out_start + i) % out_capacity];
        self.out_start = (self.out_start + n) % out_capacity;
        self.out_len -= n;
        return n;
    }

    /// Bytes waiting for a front-end to drain.
    pub fn pendingOut(self: *const Terminal) usize {
        return self.out_len;
    }

    /// Push front-end input. When the queue is full the NEWEST byte is
    /// dropped and counted. Returns bytes accepted.
    pub fn pushInput(self: *Terminal, bytes: []const u8) usize {
        var accepted: usize = 0;
        for (bytes) |b| {
            if (self.in_len == in_capacity) {
                self.in_dropped += 1;
                continue;
            }
            self.in[(self.in_start + self.in_len) % in_capacity] = b;
            self.in_len += 1;
            accepted += 1;
        }
        return accepted;
    }

    /// Drain up to `buf.len` input bytes, oldest first. Returns the count.
    pub fn readInput(self: *Terminal, buf: []u8) usize {
        self.flushMotion();
        const n = @min(buf.len, self.in_len);
        for (0..n) |i| buf[i] = self.in[(self.in_start + i) % in_capacity];
        self.in_start = (self.in_start + n) % in_capacity;
        self.in_len -= n;
        return n;
    }

    /// Input bytes waiting for the owner.
    pub fn pendingInput(self: *const Terminal) usize {
        return self.in_len;
    }

    /// M73i (#1635): push an EDGE-class mouse report (press/release/wheel).
    /// Any staged motion flushes first, so the queue stays chronological.
    pub fn pushMouse(self: *Terminal, bytes: []const u8) usize {
        self.flushMotion();
        return self.pushInput(bytes);
    }

    /// M73i (#1635): stage the newest MOTION-class report (drag or free
    /// motion) — a later stage overwrites it, so a flood never touches the
    /// queue more than once per read.
    pub fn stageMouseMotion(self: *Terminal, bytes: []const u8) usize {
        const n = @min(bytes.len, self.motion_pending.len);
        @memcpy(self.motion_pending[0..n], bytes[0..n]);
        self.motion_len = n;
        return n;
    }

    fn flushMotion(self: *Terminal) void {
        if (self.motion_len == 0) return;
        _ = self.pushInput(self.motion_pending[0..self.motion_len]);
        self.motion_len = 0;
    }

    /// Attach a front-end. Exclusive: fails when one is already attached or
    /// `fe` is `.none`. Idempotent re-attach by the SAME front-end succeeds.
    pub fn attach(self: *Terminal, fe: FrontEnd) bool {
        if (fe == .none) return false;
        if (self.attached) return self.front_end == fe;
        self.attached = true;
        self.front_end = fe;
        return true;
    }

    /// Return the unread `in` bytes to a console's RX path, oldest first —
    /// the serial-detach handover: input the owner pumped but never consumed
    /// belongs to the front-end this detach hands the console back to, not
    /// the dying session. Stops at the first byte the transport refuses.
    /// Returns bytes moved.
    pub fn pushBackInput(self: *Terminal, con: console.Console) usize {
        var moved: usize = 0;
        var b: [1]u8 = undefined;
        while (self.readInput(&b) == 1) {
            if (!con.pushByte(b[0])) break;
            moved += 1;
        }
        return moved;
    }

    /// Detach whatever front-end is attached (no-op when none).
    pub fn detach(self: *Terminal) void {
        self.attached = false;
        self.front_end = .none;
        self.window_id = null;
        self.net_port = 0;
        self.clearNetAuth();
    }

    /// M50 TS4: drop all net-auth state (challenge, reply, post bytes,
    /// verdict, auth flag, allowlist). Called on detach and before a fresh
    /// bind. Wipes the challenge and reply buffers (key-material hygiene).
    pub fn clearNetAuth(self: *Terminal) void {
        self.net_auth_on = false;
        self.net_auth_scheme = .open;
        self.net_authed = true;
        self.net_challenge_sent = false;
        self.net_auth_ticks = 0;
        self.net_reply_len = 0;
        self.net_reply_ready = false;
        self.net_post_len = 0;
        self.net_verdict = null;
        self.net_allow_on = false;
        self.net_allow_ip = .{ 0, 0, 0, 0 };
        @memset(&self.net_challenge, 0);
        @memset(&self.net_reply, 0);
        @memset(&self.net_post, 0);
    }

    /// #1082 (ADR 0020 Amendment A): attach this terminal to a `.user`
    /// window as its front-end. Exclusive per terminal (D2) and per window
    /// (A6): fails when another front-end is attached, or when another
    /// terminal already binds `window_id`. Idempotent for the same window.
    pub fn attachWindow(self: *Terminal, window_id: u8) bool {
        if (self.attached) {
            if (self.front_end == .window and self.window_id == window_id) return true;
            return false;
        }
        for (&registry.terminals) |*o| {
            if (@intFromPtr(o) == @intFromPtr(self)) continue;
            if (o.in_use and o.attached and o.front_end == .window and o.window_id == window_id) return false;
        }
        self.attached = true;
        self.front_end = .window;
        self.window_id = window_id;
        return true;
    }

    /// #1083 (ADR 0020 Amendment B): attach this terminal to a TCP listener
    /// on `port`. Exclusive per terminal (D2) and — because the TCP seam is
    /// a single connection at a time (B2) — at most one terminal may hold
    /// the net front-end. Idempotent for the same port. The caller must have
    /// entered LISTEN first.
    pub fn attachNet(self: *Terminal, port: u16) bool {
        if (self.attached) {
            if (self.front_end == .net and self.net_port == port) return true;
            return false;
        }
        for (&registry.terminals) |*o| {
            if (@intFromPtr(o) == @intFromPtr(self)) continue;
            if (o.in_use and o.attached and o.front_end == .net) return false;
        }
        self.attached = true;
        self.front_end = .net;
        self.net_port = port;
        return true;
    }

    /// M50 TS4 (#1138, ADR 0024 D6): attach the net front-end with the
    /// delegated challenge-response gate. `scheme` is what the pump frames
    /// in `VIRELAIOS-AUTH/1`; `.open` reproduces M46's accept-immediately
    /// behavior (the explicit insecure mode). A source-IP `allow_ip` is
    /// optional and unchanged from ADR 0022 D4.
    pub fn attachNetAuth(self: *Terminal, port: u16, scheme: NetAuthScheme, allow_ip: ?[4]u8) bool {
        if (!self.attachNet(port)) return false;
        self.clearNetAuth();
        if (scheme != .open) {
            self.net_auth_on = true;
            self.net_auth_scheme = scheme;
            self.net_authed = false; // the handshake must complete first
        }
        if (allow_ip) |ip| {
            self.net_allow_ip = ip;
            self.net_allow_on = true;
        }
        return true;
    }

    pub fn isAttached(self: *const Terminal) bool {
        return self.attached;
    }

    /// Drop all buffered bytes/counters and detach. Keeps `in_use`/owner.
    pub fn flush(self: *Terminal) void {
        self.out_start = 0;
        self.out_len = 0;
        self.out_dropped = 0;
        self.in_start = 0;
        self.in_len = 0;
        self.in_dropped = 0;
    }

    /// Full reset to a free slot (used by the registry on release/tests).
    pub fn reset(self: *Terminal) void {
        self.* = .{};
    }
};

// ---------------------------------------------------------------------------
// Tests (pure; no hardware)
// ---------------------------------------------------------------------------
test "terminal: output round-trips FIFO from owner to front-end" {
    var t = Terminal{};
    try std.testing.expectEqual(@as(usize, 5), t.write("hello"));
    try std.testing.expectEqual(@as(usize, 5), t.pendingOut());
    var buf: [8]u8 = undefined;
    try std.testing.expectEqual(@as(usize, 5), t.readOut(&buf));
    try std.testing.expectEqualStrings("hello", buf[0..5]);
    try std.testing.expectEqual(@as(usize, 0), t.pendingOut());
    // Draining an empty ring is a no-op.
    try std.testing.expectEqual(@as(usize, 0), t.readOut(&buf));
}

test "terminal: output ring wraps and preserves order across fill/drain cycles" {
    var t = Terminal{};
    var big: [out_capacity]u8 = undefined;
    for (&big, 0..) |*b, i| b.* = @truncate(i);
    try std.testing.expectEqual(out_capacity, t.write(&big));
    // Drain the first half, refill, then read everything back in order.
    var half: [out_capacity]u8 = undefined;
    try std.testing.expectEqual(out_capacity / 2, t.readOut(half[0 .. out_capacity / 2]));
    try std.testing.expectEqualStrings(big[0 .. out_capacity / 2], half[0 .. out_capacity / 2]);
    const more = "XYZ";
    _ = t.write(more);
    var rest: [out_capacity + 4]u8 = undefined;
    const n = t.readOut(&rest);
    try std.testing.expectEqual(out_capacity / 2 + more.len, n);
    // The tail of the original fill, then the appended bytes.
    try std.testing.expectEqualSlices(u8, big[out_capacity / 2 ..], rest[0 .. out_capacity / 2]);
    try std.testing.expectEqualStrings(more, rest[out_capacity / 2 .. n]);
}

test "terminal: output overflow drops the OLDEST byte and counts it" {
    // Serial/net still drop-oldest (M73f-1 non-goal). The window path's
    // polarity lives in "a window write larger than the ring cannot drop".
    var t = Terminal{};
    var big: [out_capacity + 10]u8 = undefined;
    for (&big, 0..) |*b, i| b.* = @truncate(i);
    _ = t.write(&big);
    try std.testing.expectEqual(@as(u64, 10), t.out_dropped);
    // The surviving window is the LAST out_capacity bytes.
    var buf: [out_capacity]u8 = undefined;
    try std.testing.expectEqual(out_capacity, t.readOut(&buf));
    try std.testing.expectEqualSlices(u8, big[10..], &buf);
}

test "terminal: input round-trips FIFO from front-end to owner" {
    var t = Terminal{};
    try std.testing.expectEqual(@as(usize, 3), t.pushInput("abc"));
    try std.testing.expectEqual(@as(usize, 3), t.pendingInput());
    var buf: [8]u8 = undefined;
    try std.testing.expectEqual(@as(usize, 3), t.readInput(&buf));
    try std.testing.expectEqualStrings("abc", buf[0..3]);
    try std.testing.expectEqual(@as(usize, 0), t.pendingInput());
}

test "terminal: input overflow drops the NEWEST byte (never evicts unread keys)" {
    var t = Terminal{};
    var big: [in_capacity]u8 = undefined;
    for (&big, 0..) |*b, i| b.* = @truncate(i);
    try std.testing.expectEqual(in_capacity, t.pushInput(&big));
    // One over capacity: the new byte is refused, the queue is untouched.
    try std.testing.expectEqual(@as(usize, 0), t.pushInput("Z"));
    try std.testing.expectEqual(@as(u64, 1), t.in_dropped);
    try std.testing.expectEqual(in_capacity, t.pendingInput());
    var buf: [in_capacity]u8 = undefined;
    try std.testing.expectEqual(in_capacity, t.readInput(&buf));
    try std.testing.expectEqualSlices(u8, &big, &buf);
}

test "terminal: front-end attach is exclusive and detach re-opens it" {
    var t = Terminal{};
    try std.testing.expect(!t.isAttached());
    try std.testing.expect(t.attach(.serial));
    try std.testing.expect(t.isAttached());
    try std.testing.expectEqual(FrontEnd.serial, t.front_end);
    // A second, different front-end is refused while attached.
    try std.testing.expect(!t.attach(.window));
    try std.testing.expectEqual(FrontEnd.serial, t.front_end);
    // Re-attach by the same front-end is idempotent.
    try std.testing.expect(t.attach(.serial));
    // `.none` is never a valid attach.
    t.detach();
    try std.testing.expect(!t.isAttached());
    try std.testing.expect(!t.attach(.none));
    // Now the window can take it.
    try std.testing.expect(t.attach(.window));
}

test "terminal: flush clears buffers and counters without detaching the owner" {
    var t = Terminal{};
    _ = t.write("out");
    _ = t.pushInput("in");
    t.attached = true;
    t.front_end = .serial;
    t.flush();
    try std.testing.expectEqual(@as(usize, 0), t.pendingOut());
    try std.testing.expectEqual(@as(usize, 0), t.pendingInput());
    try std.testing.expectEqual(@as(u64, 0), t.out_dropped);
    try std.testing.expectEqual(@as(u64, 0), t.in_dropped);
    try std.testing.expect(t.isAttached());
}
