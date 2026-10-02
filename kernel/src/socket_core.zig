//! B6's owner-approved injected core, NOT the live TCP/syscall implementation.
//! One listener and two charged children; numeric IPv4, passive open only.
//! A caller serializes access and supplies monotonic nanoseconds and fresh ISNs.
//! No locks, allocation, scheduler calls, DNS, threading or SDK resource grants.
//!
//! receive/poll/flush perform bounded work. Call poll(now) before flushing and
//! periodically even without RX/app traffic. flush calls a bounded, non-reentrant
//! transport callback once; true means accepted by that transport, not delivered.
//! Borrowed bytes are valid only during the callback. Failed TX preserves the slot.
//! Clean FIN drains queued input before EOF; reset/timeout discard input.
//! Terminal children (including failed half-opens, obtainable through accept)
//! stay charged until close/death. close is immediate local reclamation, not a
//! promise that FIN/RST reached a peer; shutdown initiates bounded graceful FIN.
//! The single independently addressed refusal slot drops/counts overflow.
//! Unsupported option-bearing TCP and fragmented IPv4 are counted and dropped.
const std = @import("std");
pub const wire = @import("tcp.zig");

pub const child_limit = 2;
pub const core_limit = 16 * 1024;
pub const deadline_ns: u64 = 30_000_000_000;
pub const rto_ns: u64 = 3_000_000_000;
pub const retry_limit: u8 = 10;
pub const Endpoint = struct { ip: [4]u8, port: u16 };
pub const Handle = struct { value: u64 };
pub const Error = error{
    InvalidArgument,
    InvalidHandle,
    AccessDenied,
    Capacity,
    WouldBlock,
    PeerReset,
    TimedOut,
    Closed,
    Unsupported,
    ClockWentBackwards,
};
pub const State = enum { half_open, established, closing, terminal };
pub const Outcome = enum { none, eof, reset, timeout, closed };
pub const Ready = struct {
    accept: bool = false,
    read: bool = false,
    write: bool = false,
    terminal: bool = false,
};
pub const Outbound = struct {
    local: Endpoint,
    remote: Endpoint,
    remote_mac: [6]u8,
    bytes: []const u8,
};
pub const Transmit = *const fn (?*anyopaque, Outbound) bool;
pub const Flush = enum { empty, sent, refused };
const Listener = struct { handle: Handle, owner: u64, local: Endpoint };
pub const Connection = struct {
    handle: Handle,
    owner: u64,
    local: Endpoint,
    remote: Endpoint,
    remote_mac: [6]u8,
    state: State = .half_open,
    outcome: Outcome = .none,
    accepted: bool = false,
    peer_fin: bool = false,
    peer_window: u16,
    snd_una: u32,
    snd_nxt: u32,
    rcv_nxt: u32,
    phase_deadline: u64,
    read_deadline: u64 = 0,
    write_deadline: u64 = 0,
    rx: [wire.payload_max]u8 = undefined,
    rx_len: usize = 0,
    tx: [wire.segment_max]u8 = undefined,
    tx_len: usize = 0,
    tx_due: bool = false,
    tx_started: bool = false,
    last_tx: u64 = 0,
    retries: u8 = 0,
    control: [wire.tcp_hdr_len]u8 = undefined,
    control_pending: bool = false,
};
const Refusal = struct {
    local: Endpoint,
    remote: Endpoint,
    remote_mac: [6]u8,
    bytes: [wire.tcp_hdr_len]u8,
};
pub const Counters = struct {
    malformed: u64 = 0,
    foreign: u64 = 0,
    duplicates: u64 = 0,
    rx_full: u64 = 0,
    capacity_refused: u64 = 0,
    refusal_dropped: u64 = 0,
    tx_refused: u64 = 0,
    retransmitted: u64 = 0,
    timed_out: u64 = 0,
};

pub const Core = struct {
    listener: ?Listener = null,
    children: [child_limit]?Connection = @splat(null),
    refusal: ?Refusal = null,
    next_generation: u64 = 1,
    now: u64 = 0,
    tx_cursor: usize = 0,
    counters: Counters = .{},

    fn clock(self: *Core, now: u64) Error!void {
        if (now < self.now) return error.ClockWentBackwards;
        self.now = now;
    }
    fn token(self: *Core, slot: u64) Error!Handle {
        if (self.next_generation > std.math.maxInt(u64) >> 2) return error.Capacity;
        const handle: Handle = .{ .value = (self.next_generation << 2) | slot };
        self.next_generation += 1;
        return handle;
    }
    fn listenerFor(self: *Core, owner: u64, handle: Handle) Error!*Listener {
        const listener = if (self.listener) |*l| l else return error.InvalidHandle;
        if (listener.handle.value != handle.value) return error.InvalidHandle;
        if (listener.owner != owner) return error.AccessDenied;
        return listener;
    }
    fn childFor(self: *Core, owner: u64, handle: Handle) Error!*Connection {
        const slot = handle.value & 3;
        if (slot == 0 or slot > child_limit) return error.InvalidHandle;
        const child = if (self.children[slot - 1]) |*c| c else return error.InvalidHandle;
        if (child.handle.value != handle.value) return error.InvalidHandle;
        if (child.owner != owner) return error.AccessDenied;
        return child;
    }
    pub fn listen(self: *Core, owner: u64, local: Endpoint, now: u64) Error!Handle {
        if (!validIp(local.ip) or local.port == 0) return error.InvalidArgument;
        if (self.listener != null) return error.Capacity;
        try self.clock(now);
        const handle = try self.token(0);
        self.listener = .{ .handle = handle, .owner = owner, .local = local };
        return handle;
    }
    pub fn accept(self: *Core, owner: u64, handle: Handle) Error!Handle {
        _ = try self.listenerFor(owner, handle);
        for (&self.children) |*entry| if (entry.*) |*c| {
            if (!c.accepted and c.state != .half_open) {
                c.accepted = true;
                return c.handle;
            }
        };
        return error.WouldBlock;
    }
    pub fn ready(self: *Core, owner: u64, handle: Handle) Error!Ready {
        if ((handle.value & 3) == 0) {
            _ = try self.listenerFor(owner, handle);
            for (self.children) |entry| if (entry) |c| {
                if (!c.accepted and c.state != .half_open) return .{ .accept = true };
            };
            return .{};
        }
        const c = try self.childFor(owner, handle);
        return .{
            .read = c.rx_len != 0 or c.peer_fin or c.state == .terminal,
            .write = c.state == .established and c.tx_len == 0 and !c.peer_fin and c.peer_window != 0,
            .terminal = c.state == .terminal,
        };
    }
    pub fn status(self: *Core, owner: u64, handle: Handle) Error!Outcome {
        return (try self.childFor(owner, handle)).outcome;
    }
    pub fn liveChildren(self: *const Core) usize {
        var count: usize = 0;
        for (self.children) |c| if (c != null) {
            count += 1;
        };
        return count;
    }
    pub fn send(self: *Core, owner: u64, handle: Handle, bytes: []const u8, now: u64) Error!usize {
        const c = try self.childFor(owner, handle);
        try self.poll(now);
        try usable(c);
        if (bytes.len == 0) return 0;
        if (c.state != .established or c.tx_len != 0 or c.peer_window == 0) return error.WouldBlock;
        const count = @min(bytes.len, @min(wire.payload_max, c.peer_window));
        queue(c, wire.flag_ack, bytes[0..count], now);
        c.write_deadline = deadline(now);
        return count;
    }
    pub fn read(self: *Core, owner: u64, handle: Handle, output: []u8, now: u64) Error!usize {
        const c = try self.childFor(owner, handle);
        try self.poll(now);
        // A zero-length buffer is a validated no-op, not an EOF indication.
        if (output.len == 0) return 0;
        if (c.rx_len != 0) {
            const count = @min(output.len, c.rx_len);
            @memcpy(output[0..count], c.rx[0..count]);
            std.mem.copyForwards(u8, c.rx[0 .. c.rx_len - count], c.rx[count..c.rx_len]);
            c.rx_len -= count;
            c.read_deadline = deadline(now);
            if (c.state != .terminal) ack(c);
            return count;
        }
        if (c.peer_fin or c.outcome == .eof) return 0;
        try usable(c);
        return error.WouldBlock;
    }
    /// Graceful close is an operation, not an in-handler wait. close releases.
    pub fn shutdown(self: *Core, owner: u64, handle: Handle, now: u64) Error!void {
        const c = try self.childFor(owner, handle);
        try self.poll(now);
        try usable(c);
        if (c.state != .established or c.tx_len != 0) return error.WouldBlock;
        queue(c, wire.flag_fin | wire.flag_ack, &.{}, now);
        c.state = .closing;
        c.phase_deadline = deadline(now);
    }
    pub fn close(self: *Core, owner: u64, handle: Handle) Error!void {
        if ((handle.value & 3) == 0) {
            _ = try self.listenerFor(owner, handle);
            self.children = @splat(null);
            self.listener = null;
            self.refusal = null;
        } else {
            _ = try self.childFor(owner, handle);
            self.children[(handle.value & 3) - 1] = null;
        }
    }
    pub fn closeOwner(self: *Core, owner: u64) void {
        if (self.listener) |l| {
            if (l.owner == owner) {
                self.listener = null;
                self.refusal = null;
            }
        }
        for (&self.children) |*entry| if (entry.*) |c| {
            if (c.owner == owner) entry.* = null;
        };
    }
    pub fn connect(_: *Core, _: Endpoint) Error!Handle {
        return error.Unsupported;
    }
    pub fn lookup(_: *Core, _: []const u8) Error![4]u8 {
        return error.Unsupported;
    }

    /// Complete Ethernet/IPv4 frames, not the old singleton's RX globals.
    pub fn receive(self: *Core, frame: []const u8, now: u64, fresh_isn: u32) Error!void {
        try self.poll(now);
        const l = self.listener orelse return;
        const p = parse(frame) orelse {
            self.counters.malformed +%= 1;
            return;
        };
        if (!same(p.local, l.local)) {
            self.counters.foreign +%= 1;
            return;
        }
        for (&self.children) |*entry| if (entry.*) |*c| {
            if (same(c.local, p.local) and same(c.remote, p.remote)) {
                self.received(c, p);
                return;
            }
        };
        if (p.flags != wire.flag_syn or p.payload.len != 0) {
            self.counters.foreign +%= 1;
            return;
        }
        for (&self.children, 0..) |*entry, i| if (entry.* == null) {
            const handle = self.token(i + 1) catch {
                self.refuse(p);
                return;
            };
            entry.* = .{
                .handle = handle,
                .owner = l.owner,
                .local = l.local,
                .remote = p.remote,
                .remote_mac = p.mac,
                .peer_window = p.window,
                .snd_una = fresh_isn,
                .snd_nxt = fresh_isn,
                .rcv_nxt = p.seq +% 1,
                .phase_deadline = deadline(now),
            };
            queue(&entry.*.?, wire.flag_syn | wire.flag_ack, &.{}, now);
            return;
        };
        self.refuse(p);
    }
    fn refuse(self: *Core, p: Parsed) void {
        self.counters.capacity_refused +%= 1;
        if (self.refusal != null) {
            self.counters.refusal_dropped +%= 1;
            return;
        }
        var refusal: Refusal = .{ .local = p.local, .remote = p.remote, .remote_mac = p.mac, .bytes = undefined };
        _ = wire.build_segment(&refusal.bytes, p.local.ip, p.remote.ip, p.local.port, p.remote.port, 0, p.seq +% 1, wire.flag_rst | wire.flag_ack, &.{});
        self.refusal = refusal;
    }
    fn received(self: *Core, c: *Connection, p: Parsed) void {
        if (c.state == .terminal) {
            if (p.flags == wire.flag_syn and p.payload.len == 0) self.refuse(p);
            return;
        }
        if (p.flags == wire.flag_syn and c.state == .half_open and p.seq +% 1 == c.rcv_nxt and p.payload.len == 0) {
            // Reuse the original SYN-ACK; never allocate another child/ISN.
            c.tx_due = true;
            return;
        }
        if (p.seq != c.rcv_nxt) {
            self.counters.duplicates +%= 1;
            if (c.state != .half_open) ack(c);
            return;
        }
        if ((p.flags & wire.flag_rst) != 0) {
            finish(c, .reset);
            return;
        }
        if ((p.flags & wire.flag_syn) != 0 or
            (c.state == .half_open and (p.flags & wire.flag_fin) != 0))
        {
            self.counters.malformed +%= 1;
            return;
        }
        const sent_limit = if (c.tx_started) c.snd_nxt else c.snd_una;
        if ((p.flags & wire.flag_ack) == 0 or !ackInRange(p.ack, c.snd_una, sent_limit)) {
            self.counters.malformed +%= 1;
            return;
        }
        c.peer_window = p.window;
        if (c.state == .half_open) {
            if (!c.tx_started or p.ack != c.snd_nxt) return;
            clearTx(c);
            c.snd_una = p.ack;
            c.state = .established;
            c.read_deadline = deadline(self.now);
        } else if (c.tx_started and p.ack == c.snd_nxt) {
            clearTx(c);
            c.snd_una = p.ack;
            if (c.state == .closing and c.peer_fin) {
                finish(c, .eof);
                // finish preserves queued clean input; final ACK still goes.
                ack(c);
                return;
            }
        }
        if (p.payload.len != 0) {
            if (c.peer_fin or c.rx_len != 0 or c.state == .closing) {
                self.counters.rx_full +%= 1;
                ack(c);
                return;
            }
            @memcpy(c.rx[0..p.payload.len], p.payload);
            c.rx_len = p.payload.len;
            c.rcv_nxt +%= @intCast(p.payload.len);
            ack(c);
        }
        if ((p.flags & wire.flag_fin) != 0) {
            c.rcv_nxt +%= 1;
            c.peer_fin = true;
            ack(c);
            if (c.tx_len == 0) {
                if (c.state == .closing) {
                    finish(c, .eof);
                    ack(c);
                } else {
                    queue(c, wire.flag_fin | wire.flag_ack, &.{}, self.now);
                    c.state = .closing;
                    c.phase_deadline = deadline(self.now);
                }
            }
        }
    }
    /// Deadlines win at equality, before a same-time retransmission.
    pub fn poll(self: *Core, now: u64) Error!void {
        try self.clock(now);
        for (&self.children) |*entry| if (entry.*) |*c| {
            if (c.state == .terminal) continue;
            const expired = if (c.state == .half_open or c.state == .closing)
                now >= c.phase_deadline
            else
                now >= c.read_deadline or (c.tx_len != 0 and now >= c.write_deadline);
            if (expired) {
                finish(c, .timeout);
                self.counters.timed_out +%= 1;
                continue;
            }
            if (c.tx_len != 0 and c.tx_started and !c.tx_due and now - c.last_tx >= rto_ns) {
                if (c.retries >= retry_limit) {
                    finish(c, .timeout);
                    self.counters.timed_out +%= 1;
                } else c.tx_due = true;
            }
            // A peer FIN while our data was pending: queue FIN after its ACK.
            if (c.peer_fin and c.state == .established and c.tx_len == 0) {
                queue(c, wire.flag_fin | wire.flag_ack, &.{}, now);
                c.state = .closing;
                c.phase_deadline = deadline(now);
            }
        };
    }
    /// Fair round-robin across two children and refusal, one callback per call.
    pub fn flush(self: *Core, context: ?*anyopaque, transmit: Transmit) Flush {
        for (0..child_limit + 1) |_| {
            const slot = self.tx_cursor;
            self.tx_cursor = (slot + 1) % (child_limit + 1);
            if (slot == child_limit) {
                if (self.refusal) |r| {
                    if (!transmit(context, .{ .local = r.local, .remote = r.remote, .remote_mac = r.remote_mac, .bytes = &r.bytes })) {
                        self.counters.tx_refused +%= 1;
                        return .refused;
                    }
                    self.refusal = null;
                    return .sent;
                }
            } else if (self.children[slot]) |*c| {
                const control = c.control_pending;
                if (!control and !c.tx_due) continue;
                if (!control and c.tx_started and c.retries >= retry_limit) {
                    // Repeated SYNs may request early retries, but cannot bypass
                    // the same bound or overflow the u8 counter.
                    finish(c, .timeout);
                    self.counters.timed_out +%= 1;
                    continue;
                }
                const bytes = if (control) &c.control else c.tx[0..c.tx_len];
                if (!transmit(context, .{ .local = c.local, .remote = c.remote, .remote_mac = c.remote_mac, .bytes = bytes })) {
                    self.counters.tx_refused +%= 1;
                    return .refused;
                }
                if (control) {
                    c.control_pending = false;
                } else {
                    if (c.tx_started) {
                        c.retries += 1;
                        self.counters.retransmitted +%= 1;
                    }
                    c.tx_due = false;
                    c.tx_started = true;
                    c.last_tx = self.now;
                }
                return .sent;
            }
        }
        return .empty;
    }
};

comptime {
    if (@sizeOf(Core) + wire.frame_max > core_limit)
        @compileError("SocketCoreBudget: core plus one owned frame exceeds 16 KiB");
}
fn deadline(now: u64) u64 {
    return now +| deadline_ns;
}
fn validIp(ip: [4]u8) bool {
    return ip[0] != 0 and ip[0] != 127 and ip[0] < 224;
}
fn same(a: Endpoint, b: Endpoint) bool {
    return a.port == b.port and std.mem.eql(u8, &a.ip, &b.ip);
}
fn ackInRange(value: u32, first: u32, last: u32) bool {
    return value -% first <= last -% first;
}
fn usable(c: *const Connection) Error!void {
    switch (c.outcome) {
        .reset => return error.PeerReset,
        .timeout => return error.TimedOut,
        .eof, .closed => return error.Closed,
        .none => {},
    }
    if (c.peer_fin) return error.Closed;
}
fn clearTx(c: *Connection) void {
    c.tx_len = 0;
    c.tx_due = false;
    c.tx_started = false;
    c.retries = 0;
    c.write_deadline = 0;
}
fn finish(c: *Connection, outcome: Outcome) void {
    c.state = .terminal;
    c.outcome = outcome;
    clearTx(c);
    c.control_pending = false;
    c.phase_deadline = 0;
    c.read_deadline = 0;
    if (outcome != .eof) {
        c.rx_len = 0;
        c.peer_fin = false;
    }
}
fn build(c: *const Connection, buf: []u8, seq: u32, flags: u8, payload: []const u8) usize {
    const len = wire.build_segment(buf, c.local.ip, c.remote.ip, c.local.port, c.remote.port, seq, c.rcv_nxt, flags, payload);
    // One segment, not 4096 bytes of reassembly space. A partial read stays shut.
    std.mem.writeInt(u16, buf[14..16], if (c.rx_len == 0) wire.payload_max else 0, .big);
    @memset(buf[16..18], 0);
    std.mem.writeInt(u16, buf[16..18], wire.checksum_tcp(c.local.ip, c.remote.ip, buf[0..len]), .big);
    return len;
}
fn queue(c: *Connection, flags: u8, payload: []const u8, now: u64) void {
    c.tx_len = build(c, &c.tx, c.snd_nxt, flags, payload);
    c.snd_nxt +%= @intCast(payload.len + @as(usize, if ((flags & (wire.flag_syn | wire.flag_fin)) != 0) 1 else 0));
    c.tx_due = true;
    c.tx_started = false;
    c.retries = 0;
    c.last_tx = now;
}
fn ack(c: *Connection) void {
    _ = build(c, &c.control, if (c.tx_started) c.snd_nxt else c.snd_una, wire.flag_ack, &.{});
    c.control_pending = true;
}
const Parsed = struct {
    local: Endpoint,
    remote: Endpoint,
    mac: [6]u8,
    seq: u32,
    ack: u32,
    flags: u8,
    window: u16,
    payload: []const u8,
};
fn parse(frame: []const u8) ?Parsed {
    if (frame.len < wire.frame_min or frame[12] != 8 or frame[13] != 0 or frame[14] != 0x45 or frame[23] != 6) return null;
    const ip = frame[14..34];
    if (wire.fold(wire.sum_words(ip)) != 0 or std.mem.readInt(u16, ip[6..8], .big) & 0xbfff != 0) return null;
    const length = std.mem.readInt(u16, ip[2..4], .big);
    if (length < 40 or length > 1500 or length + @as(usize, 14) > frame.len) return null;
    const seg = frame[34 .. 14 + @as(usize, length)];
    if (seg[12] != 0x50 or seg[13] & ~@as(u8, 0x1f) != 0 or (seg[13] & (wire.flag_syn | wire.flag_fin)) == (wire.flag_syn | wire.flag_fin)) return null;
    if (seg[13] & wire.flag_rst != 0 and (seg.len != wire.tcp_hdr_len or seg[13] & (wire.flag_syn | wire.flag_fin) != 0)) return null;
    const remote: [4]u8 = ip[12..16].*;
    const local: [4]u8 = ip[16..20].*;
    if (!validIp(remote) or wire.checksum_tcp(remote, local, seg) != 0) return null;
    const src_port = std.mem.readInt(u16, seg[0..2], .big);
    const dst_port = std.mem.readInt(u16, seg[2..4], .big);
    if (src_port == 0 or dst_port == 0) return null;
    return .{
        .local = .{ .ip = local, .port = dst_port },
        .remote = .{ .ip = remote, .port = src_port },
        .mac = frame[6..12].*,
        .seq = std.mem.readInt(u32, seg[4..8], .big),
        .ack = std.mem.readInt(u32, seg[8..12], .big),
        .flags = seg[13],
        .window = std.mem.readInt(u16, seg[14..16], .big),
        .payload = seg[20..],
    };
}
