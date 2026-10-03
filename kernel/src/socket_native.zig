//! B6 native service. NET-domain callers serialize this singleton.
//! One passive listener, two children (including half-open/terminal), one
//! separately owned DNS transaction. Legacy slots 30–33/76 stay independent.
//! Handles are generation-tagged, process-owned, never file descriptors.
//! All operations probe; EL0 sleeps between probes. No scheduler entry here.
pub const core = @import("socket_core.zig");
const std = @import("std");
const timer = @import("timer.zig");
const builtin = @import("builtin");
const udp = @import("udp.zig");
pub var sockets: core.Core = .{};
pub const dns_port: u16 = 7001;
pub const dns_max = 64;
pub const Dns = struct {
    owner: u64,
    handle: core.Handle,
    server: [4]u8,
    id: u16,
    deadline: u64,
    reply: [dns_max]u8 = undefined,
    len: usize = 0,
    failed: bool = false,
    timed_out: bool = false,
};
pub var dns: ?Dns = null;
pub var test_now: u64 = 0;

pub fn now() u64 {
    if (builtin.is_test) return test_now;
    if (timer.freq == 0) return timer.ticks *| timer.period_ns;
    return @intCast(@min(std.math.maxInt(u64), @as(u128, timer.cntpct()) * timer.period_ns / timer.freq));
}
pub fn poll() void {
    sockets.poll(now()) catch unreachable;
    if (dns) |*d| {
        if (d.len == 0 and !d.failed and now() >= d.deadline) d.timed_out = true;
    }
}
pub fn closeOwner(owner: u64) void {
    sockets.closeOwner(owner);
    if (dns) |d| if (d.owner == owner) {
        _ = udp.close_port(dns_port);
        dns = null;
    };
}
pub fn close(owner: u64, handle: core.Handle) core.Error!void {
    if (handle.value & 3 != 3) return sockets.close(owner, handle);
    _ = try dnsFor(owner, handle);
    _ = udp.close_port(dns_port);
    dns = null;
}
pub fn beginDns(owner: u64, server: [4]u8, query: []const u8) core.Error!core.Handle {
    if (server[0] == 0 or server[0] == 127 or server[0] >= 224 or query.len < 17 or query.len > dns_max)
        return error.InvalidArgument;
    if (dns != null or sockets.next_generation > std.math.maxInt(i64) >> 2) return error.Capacity;
    // Pin one legacy listen-table seat so nobody can bind it concurrently.
    // A pre-existing bind is a capacity refusal, never silently intercepted.
    if (!udp.listen_port(dns_port)) return error.Capacity;
    const handle: core.Handle = .{ .value = sockets.next_generation << 2 | 3 };
    sockets.next_generation += 1;
    dns = .{ .owner = owner, .handle = handle, .server = server, .id = std.mem.readInt(u16, query[0..2], .big), .deadline = now() +| core.deadline_ns };
    return handle;
}
fn dnsFor(owner: u64, handle: core.Handle) core.Error!*Dns {
    const d = if (dns) |*d| d else return error.InvalidHandle;
    if (handle.value != d.handle.value) return error.InvalidHandle;
    if (owner != d.owner) return error.AccessDenied;
    return d;
}
pub fn readDns(owner: u64, handle: core.Handle, output: []u8) core.Error!usize {
    const d = try dnsFor(owner, handle);
    if (d.timed_out) return error.TimedOut;
    if (d.failed) return error.InvalidArgument;
    if (d.len == 0) return error.WouldBlock;
    if (output.len < d.len) return error.InvalidArgument;
    @memcpy(output[0..d.len], d.reply[0..d.len]);
    return d.len; // level-triggered until explicit close, never silently consumed
}
/// Called only after IPv4 checksum/destination/fragment validation.
pub fn receiveDns(frame: []const u8) bool {
    if (frame.len < 42 or frame[23] != 17 or std.mem.readInt(u16, frame[36..38], .big) != dns_port) return false;
    const d = if (dns) |*d| d else return false;
    if (d.timed_out or d.len != 0 or d.failed) return true;
    const total = std.mem.readInt(u16, frame[16..18], .big);
    const length = std.mem.readInt(u16, frame[38..40], .big);
    if (total < 28 or @as(usize, total) + 14 > frame.len or length < 20 or length != total - 20 or
        !std.mem.eql(u8, frame[26..30], &d.server) or std.mem.readInt(u16, frame[34..36], .big) != 53) return true;
    const segment = frame[34 .. 14 + @as(usize, total)];
    if (std.mem.readInt(u16, segment[6..8], .big) != 0 and
        udp.checksum_udp(d.server, frame[30..34].*, segment) != 0) return true;
    const reply = segment[8..];
    if (std.mem.readInt(u16, reply[0..2], .big) != d.id or reply[2] & 0x80 == 0) return true;
    if (reply.len > dns_max) {
        d.failed = true;
    } else {
        @memcpy(d.reply[0..reply.len], reply);
        d.len = reply.len;
    }
    return true;
}
pub fn ready(owner: u64, handle: core.Handle) core.Error!u64 {
    if (handle.value & 3 == 3) {
        const d = try dnsFor(owner, handle);
        return if (d.len != 0 or d.failed or d.timed_out) 1 else 0;
    }
    const r = try sockets.ready(owner, handle);
    return @as(u64, @intFromBool(r.read)) | (@as(u64, @intFromBool(r.write)) << 1) |
        (@as(u64, @intFromBool(r.accept)) << 2) | (@as(u64, @intFromBool(r.terminal)) << 3);
}
pub fn peer(owner: u64, handle: core.Handle) core.Error!u64 {
    _ = try sockets.status(owner, handle);
    const c = sockets.children[(handle.value & 3) - 1].?;
    return ipWord(c.remote.ip) << 16 | c.remote.port;
}
pub fn ownedCount(owner: u64) u64 {
    var n: u64 = 0;
    if (sockets.listener) |l| if (l.owner == owner) {
        n += 1;
    };
    for (sockets.children) |c| if (c) |child| if (child.owner == owner) {
        n += 1;
    };
    if (dns) |d| if (d.owner == owner) {
        n += 1;
    };
    return n;
}
pub fn ipWord(ip: [4]u8) u64 {
    return @as(u64, ip[0]) << 24 | @as(u64, ip[1]) << 16 | @as(u64, ip[2]) << 8 | ip[3];
}
pub fn ipBytes(value: u64) [4]u8 {
    return .{ @truncate(value >> 24), @truncate(value >> 16), @truncate(value >> 8), @truncate(value) };
}
