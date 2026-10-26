//! The net front-end pump and delegated auth (split from terminal.zig,
//! #3753): `NetSeam` transport injection, SH7 byte pumping, and the M50 TS4
//! challenge/verdict gate (ADR 0024 D6).

const std = @import("std");
const klog = @import("../klog.zig");
const tcp = @import("../tcp.zig");
const virtio_net = @import("../virtio_net.zig");
const csprng = @import("../csprng.zig");
const timer = @import("../timer.zig");
const registry = @import("../terminal.zig");
const session_mod = @import("session.zig");
const create = registry.create;
const get = registry.get;
const release = registry.release;
const FrontEnd = session_mod.FrontEnd;
const Terminal = session_mod.Terminal;

/// M50 TS4 (#1138, ADR 0024 D6): the delegated net-auth protocol.
/// `net_challenge_len` is the fresh 32-byte server challenge minted from the
/// kernel CSPRNG on every accept; `net_auth_line_max` bounds the client's
/// one-line reply (64 hex chars for HMAC-SHA256, 128 for Ed25519 — 160 is
/// the ADR's fixed bound, reassembly-free). `net_auth_deadline` is the 1 Hz
/// seam clock's 10 s bound: no verdict by then is a failed connection,
/// never a bypass.
pub const net_challenge_len: usize = 32;

pub const net_auth_line_max: usize = 160;

pub const net_auth_deadline: u64 = 10;

/// The framing tag: `VIRELAIOS-AUTH/1 <scheme> <hex-challenge>\n`.
pub const net_auth_tag: []const u8 = "VIRELAIOS-AUTH/1";

/// M50 TS4: the scheme the pump frames and the verifier (the attached
/// process) must answer. `.open` is M46's accept-immediately posture,
/// reached only through the explicit insecure CLI mode (ADR 0024 D6).
pub const NetAuthScheme = enum(u8) {
    open = 0,
    hmac_sha256 = 1,
    ed25519 = 2,

    pub fn name(self: NetAuthScheme) []const u8 {
        return switch (self) {
            .open => "open",
            .hmac_sha256 => "hmac-sha256",
            .ed25519 => "ed25519",
        };
    }

    /// The expected hex reply length for the scheme (0 for `.open`).
    pub fn expectedReplyLen(self: NetAuthScheme) usize {
        return switch (self) {
            .open => 0,
            .hmac_sha256 => 64,
            .ed25519 => 128,
        };
    }
};

// ---------------------------------------------------------------------------
// The net front-end pump (SH7 #1083, ADR 0020 Amendment B; M46 RC3 #1111,
// ADR 0022; M50 TS4 #1138, ADR 0024 D6). The kernel's TCP seam is a single
// bounded connection; the pump moves bytes between the net-bound terminal
// and that connection. Incoming segments are drained, a pending ACK/SYN-ACK
// is flushed, the received payload is delivered through the delegated
// challenge-response gate (the process's verdict), and (on the owner's
// `/dev/tty` write) the terminal output ring is chunked into TCP data
// segments. A peer FIN / dead connection / exhausted SYN-ACK accept
// auto-detaches the terminal (B4, #1105). The pump is driven from the
// `/dev/tty` syscall path.
//
// The transport is an injectable seam (`NetSeam`) so the byte movement is
// host-testable without a live NIC (#1105): the default seam drives
// virtio-net + the 1 Hz timer; tests inject a capture seam.
// ---------------------------------------------------------------------------
/// The net transport seam: `rxDrain` pulls pending frames from the device,
/// `tx` transmits one built frame, `now` is the accept-timeout clock. It is a
/// COMPTIME type parameter, not a runtime function-pointer table — a static
/// initializer holding code addresses is exactly the unrelocated link-time
/// pointer hazard the M33 sweep guards against (issue #1042). A host test
/// injects its own type with the same three functions.
pub const NetSeam = struct {
    pub fn rxDrain() void {
        virtio_net.net_rx_drain();
    }
    pub fn tx(bytes: []const u8) bool {
        var out_len: usize = 0;
        return virtio_net.net_tcp_send(bytes, &out_len) == .ok;
    }
    pub fn now() u64 {
        return timer.ticks;
    }
};

/// The terminal currently attached to the net front-end, if any (at most
/// one — the TCP seam is a single connection at a time).
pub fn attachedNet() ?*Terminal {
    for (&registry.terminals) |*t| {
        if (t.in_use and t.attached and t.front_end == .net) return t;
    }
    return null;
}

/// M50 TS4 (#1138): the deterministic-challenge hook for pinned-vector host
/// tests. Production never sets it; `mintChallenge` uses the kernel CSPRNG.
pub var test_challenge: ?[net_challenge_len]u8 = null;

/// Mint a fresh 32-byte challenge (CSPRNG, or the test override).
fn mintChallenge(out: *[net_challenge_len]u8) void {
    if (test_challenge) |c| {
        out.* = c;
        return;
    }
    csprng.random_bytes(out);
}

const hex_digits = "0123456789abcdef";

/// Append the lowercase hex of `bytes` to `out`; returns the characters
/// written (a short `out` truncates — callers size it for the full form).
fn appendHex(out: []u8, bytes: []const u8) usize {
    var n: usize = 0;
    for (bytes) |b| {
        if (n + 2 > out.len) break;
        out[n] = hex_digits[b >> 4];
        out[n + 1] = hex_digits[b & 0xf];
        n += 2;
    }
    return n;
}

/// True when every byte is a hex digit (either case).
fn isHex(bytes: []const u8) bool {
    for (bytes) |b| {
        const ok = (b >= '0' and b <= '9') or (b >= 'a' and b <= 'f') or (b >= 'A' and b <= 'F');
        if (!ok) return false;
    }
    return true;
}

/// Transmit `auth failed`, end the session, and detach — the one failed-door
/// path shared by a wrong/malformed reply, a reject verdict, and the
/// deadline. Pre-auth bytes are never delivered by this path.
fn netAuthFailSeam(t: *Terminal, comptime seam: type) void {
    _ = netTx(seam, "auth failed\n");
    klog.line("tty net: auth failed\n");
    tcp.reset();
    t.detach();
}

/// Production-seam wrapper (used by the slot-71 verdict handler).
pub fn netAuthFail(t: *Terminal) void {
    netAuthFailSeam(t, NetSeam);
}

/// Send the `VIRELAIOS-AUTH/1 <scheme> <hex-challenge>\n` line once, when
/// the connection is ESTABLISHED. Stamps the 10 s deadline clock. Returns
/// whether the challenge was sent (or already sent).
pub fn netAuthSendChallengeSeam(t: *Terminal, comptime seam: type) bool {
    if (t.net_challenge_sent) return true;
    if (!t.net_auth_on or t.net_auth_scheme == .open) return false;
    mintChallenge(&t.net_challenge);
    var line: [tcp.payload_max]u8 = undefined;
    var n: usize = 0;
    @memcpy(line[n..][0..net_auth_tag.len], net_auth_tag);
    n += net_auth_tag.len;
    line[n] = ' ';
    n += 1;
    const sname = t.net_auth_scheme.name();
    @memcpy(line[n..][0..sname.len], sname);
    n += sname.len;
    line[n] = ' ';
    n += 1;
    n += appendHex(line[n..], &t.net_challenge);
    line[n] = '\n';
    n += 1;
    if (!netTx(seam, line[0..n])) return false;
    t.net_challenge_sent = true;
    t.net_auth_ticks = seam.now();
    return true;
}

/// Production-seam wrapper (the pump uses the injected seam directly).
pub fn netAuthSendChallenge(t: *Terminal) bool {
    return netAuthSendChallengeSeam(t, NetSeam);
}

/// Hold pipelined post-reply bytes until the verdict (bounded by
/// `tcp.payload_max`; overflow is an honest failed connection).
fn netAuthAppendPost(t: *Terminal, comptime seam: type, bytes: []const u8) usize {
    if (bytes.len == 0) return 0;
    if (t.net_post_len + bytes.len > t.net_post.len) {
        netAuthFailSeam(t, seam);
        return 0;
    }
    @memcpy(t.net_post[t.net_post_len..][0..bytes.len], bytes);
    t.net_post_len += bytes.len;
    return 0;
}

/// Buffer one received payload through the delegated auth gate (ADR 0024
/// D6). Pre-auth bytes go to the bounded reply line (or the pipelined-post
/// buffer) and NEVER to the terminal input queue. A complete reply line is
/// length- and hex-validated immediately; malformed/over-long is a failed
/// connection. A valid reply waits for the process's verdict. Once
/// `net_authed`, every byte flows to the input queue. Returns bytes
/// delivered to the terminal (0 while the handshake is pending).
fn netAuthConsume(t: *Terminal, comptime seam: type, bytes: []const u8) usize {
    const delivered: usize = 0;
    if (t.net_authed) {
        return t.pushInput(bytes);
    }
    // Already have a reply: only pipeline post bytes (the client may send
    // its first command without waiting for the accept marker).
    if (t.net_reply_ready) {
        _ = netAuthAppendPost(t, seam, bytes);
        return 0;
    }
    var i: usize = 0;
    while (i < bytes.len) {
        const b = bytes[i];
        i += 1;
        if (b == '\n' or b == '\r') {
            if (b == '\r' and i < bytes.len and bytes[i] == '\n') i += 1; // CRLF
            const want = t.net_auth_scheme.expectedReplyLen();
            if (t.net_reply_len != want or !isHex(t.net_reply[0..t.net_reply_len])) {
                netAuthFailSeam(t, seam);
                return delivered;
            }
            t.net_reply_ready = true;
            return delivered + netAuthAppendPost(t, seam, bytes[i..]);
        }
        if (t.net_reply_len < net_auth_line_max) {
            t.net_reply[t.net_reply_len] = b;
            t.net_reply_len += 1;
        } else {
            // Over-long line: reject honestly (bounded buffer).
            netAuthFailSeam(t, seam);
            return delivered;
        }
    }
    return delivered;
}

/// Apply the attached process's verdict for the buffered reply (slot 71
/// op = verdict). Accept opens the byte gate and delivers any pipelined
/// post-auth bytes; reject is the same failed connection as a malformed
/// line. Returns false when there is no reply awaiting a verdict.
pub fn netAuthVerdictSeam(t: *Terminal, accept: bool, comptime seam: type) bool {
    if (!t.net_auth_on or t.net_authed or !t.net_reply_ready or t.net_verdict != null) return false;
    t.net_verdict = accept;
    if (!accept) {
        netAuthFailSeam(t, seam);
        return true;
    }
    t.net_authed = true;
    t.net_reply_ready = false;
    const n = t.net_post_len;
    if (n > 0) {
        _ = t.pushInput(t.net_post[0..n]);
        t.net_post_len = 0;
    }
    // Key-material hygiene: the challenge and the reply (a MAC/signature)
    // are never needed again.
    t.net_reply_len = 0;
    @memset(&t.net_reply, 0);
    @memset(&t.net_challenge, 0);
    return true;
}

/// Production-seam wrapper (the slot-71 handler).
pub fn netAuthVerdict(t: *Terminal, accept: bool) bool {
    return netAuthVerdictSeam(t, accept, NetSeam);
}

/// Build + transmit one raw TCP data segment, advancing the send state.
/// Returns whether the transport accepted it.
fn netTx(comptime seam: type, bytes: []const u8) bool {
    tcp.build_data_msg(bytes);
    if (!seam.tx(tcp.msg[0..tcp.msg_len])) return false;
    tcp.data_sent += 1;
    tcp.advance_snd(bytes.len);
    tcp.record_pending();
    return true;
}

/// Pump TCP bytes into the net-attached terminal's input queue: drain the
/// device RX, flush any built ACK/SYN-ACK, deliver a received payload through
/// the auth gate, enforce the half-open accept timeout (#1105), and
/// auto-detach on a peer FIN / dead connection. Returns bytes delivered.
pub fn pumpNetInput() usize {
    return pumpNetInputSeam(NetSeam);
}

pub fn pumpNetInputSeam(comptime seam: type) usize {
    const t = attachedNet() orelse return 0;
    seam.rxDrain();
    tcp.now_ticks = seam.now(); // the #1105 accept-timeout clock
    if (tcp.ack_pending) {
        if (seam.tx(tcp.msg[0..tcp.msg_len])) {
            tcp.ack_pending = false;
            tcp.ack_sent += 1;
        }
    }
    var total: usize = 0;
    if (tcp.rx_pending) {
        total += netAuthConsume(t, seam, tcp.take_rx());
    }
    // #1105: a half-open accept (a SYN whose ACK never arrives) is ended after
    // a bounded timeout instead of stranding the terminal in `.syn_received`.
    // A dedicated accept clock (not the client RTO) so the front-end never
    // retransmits and only ever TXes from the owner's read/write path.
    if (tcp.state == .syn_received and
        tcp.now_ticks -| tcp.accept_ticks >= tcp.accept_timeout)
    {
        klog.line("tty net: accept timeout\n");
        tcp.reset();
        t.detach();
        return total;
    }
    // M50 TS4 (#1138, ADR 0024 D6): the delegated handshake. Once the
    // connection is ESTABLISHED, mint + send the fresh challenge (never
    // before establishment), then enforce the 10 s deadline: no process
    // verdict in time is the same failed connection as a bad reply.
    if (t.net_auth_on and !t.net_authed) {
        if (tcp.state == .established and !tcp.peer_fin) {
            _ = netAuthSendChallengeSeam(t, seam);
        }
        if (t.net_challenge_sent and
            tcp.now_ticks -| t.net_auth_ticks >= net_auth_deadline)
        {
            klog.line("tty net: auth deadline\n");
            netAuthFailSeam(t, seam);
            return total;
        }
    }
    // Once the connection is up, flush anything the shell wrote before the
    // handshake completed (the initial prompt).
    if (tcp.state == .established and !tcp.peer_fin) _ = pumpNetOutputSeam(NetCapture);
    if (tcp.peer_fin or tcp.state == .closed) {
        // The peer disconnected (or the connection died): end the session,
        // release the listener, and leave the terminal buffered/unattached.
        tcp.reset();
        t.detach();
        klog.line("tty net: detached\n");
    }
    return total;
}

/// Drain the net-attached terminal's output ring into TCP data segments
/// (chunked to the stack's `payload_max`). A no-op unless the connection is
/// ESTABLISHED and authenticated (output is never leaked before auth).
/// Returns bytes sent.
pub fn pumpNetOutput() usize {
    return pumpNetOutputSeam(NetSeam);
}

pub fn pumpNetOutputSeam(comptime seam: type) usize {
    const t = attachedNet() orelse return 0;
    if (!t.net_authed) return 0; // never leak output before auth
    if (tcp.state != .established) return 0;
    var total: usize = 0;
    var buf: [tcp.payload_max]u8 = undefined;
    while (true) {
        const n = t.readOut(&buf);
        if (n == 0) break;
        if (!netTx(seam, buf[0..n])) break;
        total += n;
    }
    return total;
}

// M46 RC3 (#1111) / #1105: the net pump is host-testable through an injected
// seam. These fixtures drive the auth gate and byte movement with no NIC.
const NetCapture = struct {
    var buf: [4096]u8 = undefined;
    var len: usize = 0;
    var clock: u64 = 0;
    fn clear() void {
        len = 0;
    }
    pub fn rxDrain() void {}
    pub fn tx(bytes: []const u8) bool {
        if (len + bytes.len > buf.len) return false;
        @memcpy(buf[len..][0..bytes.len], bytes);
        len += bytes.len;
        return true;
    }
    pub fn now() u64 {
        return clock;
    }
};

fn netTestSetRx(bytes: []const u8) void {
    @memcpy(tcp.rx_payload[0..bytes.len], bytes);
    tcp.rx_len = bytes.len;
    tcp.rx_pending = true;
}

fn netTestSent(needle: []const u8) bool {
    return std.mem.indexOf(u8, NetCapture.buf[0..NetCapture.len], needle) != null;
}

fn netTestEstablish(port: u16) void {
    tcp.reset();
    tcp.listen(port);
    tcp.state = .established;
}

test "terminal: net binding is exclusive and single-session (SH7 B2)" {
    for (&registry.terminals) |*tt| tt.reset();
    const a = create(1) orelse return error.TestUnexpectedResult;
    const b = create(2) orelse return error.TestUnexpectedResult;
    const ta = get(a).?;
    const tb = get(b).?;
    try std.testing.expect(ta.attachNet(2323));
    try std.testing.expectEqual(FrontEnd.net, ta.front_end);
    try std.testing.expectEqual(@as(u16, 2323), ta.net_port);
    try std.testing.expect(attachedNet() == ta);
    // A second terminal may not hold the net front-end (one TCP connection).
    try std.testing.expect(!tb.attachNet(4242));
    try std.testing.expect(attachedNet() == ta);
    // A net terminal may not take a window (front-end exclusive).
    try std.testing.expect(!ta.attachWindow(5));
    // Idempotent re-attach of the same port succeeds.
    try std.testing.expect(ta.attachNet(2323));
    // Detach frees the single net slot.
    ta.detach();
    try std.testing.expect(attachedNet() == null);
    try std.testing.expectEqual(@as(u16, 0), ta.net_port);
    try std.testing.expect(tb.attachNet(4242));
    for (&registry.terminals) |*tt| tt.reset();
}

test "terminal: net pump mints a fresh challenge and gates delivery on the verdict (TS4)" {
    for (&registry.terminals) |*tt| tt.reset();
    tcp.reset();
    defer tcp.reset();
    defer test_challenge = null;
    NetCapture.clock = 0;
    NetCapture.clear();

    // (1) The challenge is minted + framed on establishment: pre-auth output
    // is withheld and nothing reaches the shell.
    const h1 = create(3) orelse return error.TestUnexpectedResult;
    const t1 = get(h1).?;
    try std.testing.expect(t1.attachNetAuth(2323, .hmac_sha256, null));
    try std.testing.expect(!t1.net_authed);
    try std.testing.expect(!t1.net_challenge_sent);
    var fixed: [net_challenge_len]u8 = undefined;
    for (&fixed, 0..) |*b, i| b.* = @intCast(i);
    test_challenge = fixed;
    netTestEstablish(2323);
    _ = t1.write("prompt> ");
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(t1.net_challenge_sent);
    try std.testing.expect(netTestSent("VIRELAIOS-AUTH/1 hmac-sha256 "));
    // The exact 32-byte challenge hex (000102...1f) + newline.
    try std.testing.expect(netTestSent("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f\n"));
    try std.testing.expect(!netTestSent("prompt> ")); // output withheld pre-verdict
    try std.testing.expectEqual(@as(usize, 0), t1.pendingInput());

    // (2) A malformed (non-hex) reply is the failed connection: `auth failed`
    // + reset + detach, and NO byte reached the shell.
    NetCapture.clear();
    netTestSetRx("zz\n");
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(attachedNet() == null);
    try std.testing.expect(netTestSent("auth failed"));
    try std.testing.expectEqual(@as(usize, 0), t1.pendingInput());
    release(h1);

    // (3) A fresh accept mints a NEW challenge (the CSPRNG stream advances,
    // never a reused nonce): a captured handshake cannot answer it. No test
    // override here, so the mint path runs.
    test_challenge = null;
    NetCapture.clear();
    const h2 = create(4) orelse return error.TestUnexpectedResult;
    const t2 = get(h2).?;
    try std.testing.expect(t2.attachNetAuth(2323, .hmac_sha256, null));
    netTestEstablish(2323);
    _ = pumpNetInputSeam(NetCapture);
    var first: [net_challenge_len]u8 = undefined;
    @memcpy(&first, &t2.net_challenge);
    release(h2);
    tcp.reset();
    NetCapture.clear();
    const h2b = create(5) orelse return error.TestUnexpectedResult;
    const t2b = get(h2b).?;
    try std.testing.expect(t2b.attachNetAuth(2323, .hmac_sha256, null));
    netTestEstablish(2323);
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(!std.mem.eql(u8, &first, &t2b.net_challenge));
    release(h2b);

    // (4) A well-formed reply + an accept verdict delivers the pipelined
    // command and opens the output gate.
    NetCapture.clear();
    tcp.reset();
    const h3 = create(6) orelse return error.TestUnexpectedResult;
    const t3 = get(h3).?;
    try std.testing.expect(t3.attachNetAuth(2323, .hmac_sha256, null));
    netTestEstablish(2323);
    _ = t3.write("prompt> ");
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(!netTestSent("prompt> "));
    const mac_hex = "a0a1a2a3a4a5a6a7a8a9aaabacadaeaf" ++
        "b0b1b2b3b4b5b6b7b8b9babbbcbdbebf";
    netTestSetRx(mac_hex ++ "\r\nhelp\n");
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(t3.net_reply_ready); // awaiting the process verdict
    try std.testing.expect(!t3.net_authed);
    try std.testing.expectEqual(@as(usize, 0), t3.pendingInput());
    try std.testing.expectEqualStrings("help\n", t3.net_post[0..t3.net_post_len]);
    try std.testing.expect(netAuthVerdict(t3, true));
    var in: [32]u8 = undefined;
    const n = t3.readInput(&in);
    try std.testing.expectEqualStrings("help\n", in[0..n]);
    try std.testing.expectEqual(@as(usize, 0), t3.net_reply_len); // wiped
    try std.testing.expect(t3.net_authed);
    release(h3);

    // (5) A reject verdict is the same failed connection.
    NetCapture.clear();
    tcp.reset();
    const h4 = create(7) orelse return error.TestUnexpectedResult;
    const t4 = get(h4).?;
    try std.testing.expect(t4.attachNetAuth(2323, .hmac_sha256, null));
    netTestEstablish(2323);
    test_challenge = fixed;
    _ = pumpNetInputSeam(NetCapture);
    netTestSetRx(mac_hex ++ "\n");
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(t4.net_reply_ready);
    try std.testing.expect(netAuthVerdictSeam(t4, false, NetCapture));
    try std.testing.expect(attachedNet() == null);
    try std.testing.expect(netTestSent("auth failed"));
    try std.testing.expectEqual(@as(usize, 0), t4.pendingInput());

    for (&registry.terminals) |*tt| tt.reset();
    tcp.reset();
}

test "terminal: ED25519 net auth accepts the 128-hex reply (TS4 class-A framing)" {
    for (&registry.terminals) |*tt| tt.reset();
    tcp.reset();
    defer tcp.reset();
    defer test_challenge = null;
    NetCapture.clock = 0;
    NetCapture.clear();
    const h = create(3) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachNetAuth(2323, .ed25519, null));
    netTestEstablish(2323);
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(netTestSent("VIRELAIOS-AUTH/1 ed25519 "));
    // A 128-hex signature line (the kernel frames 64/128; it does not
    // verify — the process does).
    var sig_hex: [128]u8 = undefined;
    for (&sig_hex, 0..) |*b, i| b.* = hex_digits[i % 16];
    netTestSetRx(sig_hex ++ "\n");
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(t.net_reply_ready);
    try std.testing.expect(netAuthVerdict(t, true));
    try std.testing.expect(t.net_authed);
    // A 64-hex HMAC line is malformed for ed25519.
    t.detach();
    tcp.reset();
    NetCapture.clear();
    const h2 = create(4) orelse return error.TestUnexpectedResult;
    const t2 = get(h2).?;
    try std.testing.expect(t2.attachNetAuth(2323, .ed25519, null));
    netTestEstablish(2323);
    _ = pumpNetInputSeam(NetCapture);
    netTestSetRx("a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf\n");
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(attachedNet() == null);
    try std.testing.expect(netTestSent("auth failed"));
    for (&registry.terminals) |*tt| tt.reset();
    tcp.reset();
}

test "terminal: net auth deadline trips to a failed connection, never a bypass (TS4)" {
    for (&registry.terminals) |*tt| tt.reset();
    tcp.reset();
    defer tcp.reset();
    NetCapture.clear();
    const h = create(3) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachNetAuth(2323, .hmac_sha256, null));
    netTestEstablish(2323);
    NetCapture.clock = 0;
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(t.net_challenge_sent);
    // One tick short of the deadline: still waiting, not authed.
    NetCapture.clock = net_auth_deadline - 1;
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(attachedNet() != null);
    try std.testing.expect(!t.net_authed);
    // At the deadline: the failed connection — `auth failed`, reset, detach.
    NetCapture.clock = net_auth_deadline;
    NetCapture.clear();
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(attachedNet() == null);
    try std.testing.expect(netTestSent("auth failed"));
    try std.testing.expectEqual(@as(usize, 0), t.pendingInput());
    try std.testing.expectEqual(@as(u64, 0), tcp.listen_port);
    for (&registry.terminals) |*tt| tt.reset();
    tcp.reset();
}

test "terminal: the TS4 auth framing fits one bounded TCP segment (bounds audit)" {
    // The challenge line: tag + scheme + 2*32 hex + newline.
    const hmac_line = net_auth_tag.len + 1 + "hmac-sha256".len + 1 + 2 * net_challenge_len + 1;
    const ed_line = net_auth_tag.len + 1 + "ed25519".len + 1 + 2 * net_challenge_len + 1;
    try std.testing.expect(hmac_line <= tcp.payload_max);
    try std.testing.expect(ed_line <= tcp.payload_max);
    // The reply line: 128 hex chars (Ed25519) + newline fits the raised bound.
    try std.testing.expect(NetAuthScheme.hmac_sha256.expectedReplyLen() + 1 <= tcp.payload_max);
    try std.testing.expect(NetAuthScheme.ed25519.expectedReplyLen() + 1 <= tcp.payload_max);
    try std.testing.expect(net_auth_line_max >= NetAuthScheme.ed25519.expectedReplyLen());
    // M84e raises the bound to one full, unfragmented Ethernet MTU.
    try std.testing.expectEqual(@as(usize, 1460), tcp.payload_max);
    try std.testing.expectEqual(@as(usize, 1480), tcp.segment_max);
}

test "terminal: explicit open mode reproduces SH7 byte flow (TS4)" {
    for (&registry.terminals) |*tt| tt.reset();
    tcp.reset();
    defer tcp.reset();
    NetCapture.clear();
    const h = create(3) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachNetAuth(2323, .open, null));
    try std.testing.expect(t.net_authed);
    try std.testing.expect(!t.net_auth_on);
    netTestEstablish(2323);
    netTestSetRx("help\n");
    _ = pumpNetInputSeam(NetCapture);
    var in: [32]u8 = undefined;
    const n = t.readInput(&in);
    try std.testing.expectEqualStrings("help\n", in[0..n]);
    // No challenge was ever framed in open mode.
    try std.testing.expect(!netTestSent("VIRELAIOS-AUTH/1"));
    for (&registry.terminals) |*tt| tt.reset();
    tcp.reset();
}

test "terminal: net pump ends a half-open accept on its timeout (M46 #1105)" {
    for (&registry.terminals) |*tt| tt.reset();
    tcp.reset();
    defer tcp.reset();
    NetCapture.clear();
    const h = create(3) orelse return error.TestUnexpectedResult;
    const t = get(h).?;
    try std.testing.expect(t.attachNetAuth(2323, .open, null));
    // A SYN was accepted (state syn_received) with the accept clock stamped.
    tcp.listen(2323);
    tcp.state = .syn_received;
    tcp.accept_ticks = 0;
    NetCapture.clock = tcp.accept_timeout - 1;
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(attachedNet() != null); // still waiting for the ACK
    NetCapture.clock = tcp.accept_timeout;
    _ = pumpNetInputSeam(NetCapture);
    try std.testing.expect(attachedNet() == null); // timed out and detached
    try std.testing.expectEqual(@as(u64, 0), tcp.listen_port);
    for (&registry.terminals) |*tt| tt.reset();
    tcp.reset();
}
