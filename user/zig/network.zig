//! Native B6, not POSIX sockets. IPv4 passive preview: one listener and two
//! charged children system-wide. DNS: one owned A-only transaction, 64-byte
//! messages, 30 s budget, no search suffixes/cache/TCP fallback. Numeric listen
//! addresses never resolve names. Names enter only through resolve/netLookup.
//! Readiness is a level probe. Waits sleep one coarse scheduler tick at EL0.
const std = @import("std");
pub const slot: u64 = 80;
pub const budget_ns: u64 = 30_000_000_000;
pub const dns_max = 64;
pub const default_dns = [4]u8{ 10, 0, 0, 2 };
pub const Error = error{ InvalidArgument, InvalidHandle, BadPointer, AccessDenied, Capacity, NetworkDown, WouldBlock, TimedOut, PeerReset, Closed, Unsupported, ProtocolViolation, NameTooLong, NameNotFound, DnsFailed };

pub fn word(ip: [4]u8) u64 {
    return @as(u64, ip[0]) << 24 | @as(u64, ip[1]) << 16 | @as(u64, ip[2]) << 8 | ip[3];
}
pub fn result(value: i64) Error!u64 {
    if (value >= 0) return @intCast(value);
    return switch (value) {
        -1 => error.InvalidArgument,
        -2 => error.InvalidHandle,
        -3 => error.BadPointer,
        -4 => error.Unsupported,
        -5 => error.Capacity,
        -7 => error.AccessDenied,
        -9 => error.NetworkDown,
        -11 => error.WouldBlock,
        -12 => error.TimedOut,
        -13 => error.PeerReset,
        -14 => error.Closed,
        else => error.ProtocolViolation,
    };
}
pub fn query(name: []const u8, id: u16, output: *[dns_max]u8) Error![]const u8 {
    std.Io.net.HostName.validate(name) catch return error.InvalidArgument;
    const text = std.mem.trimEnd(u8, name, ".");
    if (text.len > dns_max - 18) return error.NameTooLong;
    @memset(output, 0);
    std.mem.writeInt(u16, output[0..2], id, .big);
    output[2] = 1; // recursion desired
    output[5] = 1;
    var labels = std.mem.splitScalar(u8, text, '.');
    var at: usize = 12;
    while (labels.next()) |label| {
        output[at] = @intCast(label.len);
        at += 1;
        for (label) |c| {
            output[at] = std.ascii.toLower(c);
            at += 1;
        }
    }
    output[at + 2] = 1; // terminating zero, QTYPE A, QCLASS IN
    output[at + 4] = 1;
    return output[0 .. at + 5];
}
pub fn parseReply(message: []const u8, request: []const u8) Error![4]u8 {
    if (message.len < 12 or message.len > dns_max or request.len < 17 or
        !std.mem.eql(u8, message[0..2], request[0..2])) return error.DnsFailed;
    const flags = std.mem.readInt(u16, message[2..4], .big);
    if (flags & 0x8000 == 0 or flags & 0x7a40 != 0) return error.DnsFailed;
    const not_found = flags & 15 == 3;
    if ((!not_found and flags & 15 != 0) or std.mem.readInt(u16, message[4..6], .big) != 1) return error.DnsFailed;
    var name_buffer: [std.Io.net.HostName.max_len]u8 = undefined;
    const expanded = std.Io.net.HostName.expand(message, 12, &name_buffer) catch return error.DnsFailed;
    var query_buffer: [std.Io.net.HostName.max_len]u8 = undefined;
    const original = std.Io.net.HostName.expand(request, 12, &query_buffer) catch return error.DnsFailed;
    if (!std.Io.net.HostName.eql(expanded[1], original[1])) return error.DnsFailed;
    var at = 12 + expanded[0];
    if (at + 4 > message.len or !std.mem.eql(u8, message[at .. at + 4], &.{ 0, 1, 0, 1 })) return error.DnsFailed;
    at += 4;
    var address: ?[4]u8 = null;
    const answers = std.mem.readInt(u16, message[6..8], .big);
    // Validate every claimed record/section, not just a convenient prefix.
    const records = @as(u32, answers) + std.mem.readInt(u16, message[8..10], .big) + std.mem.readInt(u16, message[10..12], .big);
    for (0..records) |i| {
        const owner = std.Io.net.HostName.expand(message, at, &name_buffer) catch return error.DnsFailed;
        at += owner[0];
        if (at + 10 > message.len) return error.DnsFailed;
        const kind = std.mem.readInt(u16, message[at..][0..2], .big);
        const class = std.mem.readInt(u16, message[at + 2 ..][0..2], .big);
        const len = std.mem.readInt(u16, message[at + 8 ..][0..2], .big);
        at += 10;
        if (len > message.len - at) return error.DnsFailed;
        if (i < answers and kind == 1 and class == 1 and len == 4 and
            std.Io.net.HostName.eql(owner[1], original[1]) and address == null)
            address = message[at..][0..4].*;
        at += len;
    }
    if (at != message.len) return error.DnsFailed;
    if (not_found) return error.NameNotFound;
    return address orelse error.DnsFailed; // CNAME-only, AAAA, absent A refuse
}

pub fn Native(comptime Driver: type) type {
    return struct {
        pub fn call(op: u64, h: u64, ptr: u64, len: u64) Error!u64 {
            return result(Driver.call(slot, .{ op, h, ptr, len, 0, 0 }));
        }
        pub fn now() u64 {
            if (@hasDecl(Driver, "networkNow")) return Driver.networkNow();
            const builtin = @import("builtin");
            if (builtin.cpu.arch != .aarch64 or builtin.os.tag != .freestanding)
                Driver.fatal("NetworkClockRequired");
            const ticks = asm volatile ("mrs %[v], cntvct_el0"
                : [v] "=r" (-> u64),
            );
            const freq = asm volatile ("mrs %[v], cntfrq_el0"
                : [v] "=r" (-> u64),
            );
            if (freq == 0) return 0;
            return @intCast(@min(std.math.maxInt(u64), @as(u128, ticks) * 1_000_000_000 / freq));
        }
        pub fn pause(deadline: u64) Error!void {
            if (now() >= deadline) return error.TimedOut;
            if (Driver.call(4, .{ 1, 0, 0, 0, 0, 0 }) != 0) {
                if (@hasDecl(Driver, "diagnostic")) Driver.diagnostic("Network:SleepRefused\n");
                return error.ProtocolViolation;
            }
        }
        pub fn close(handle: u64) Error!void {
            _ = try call(4, handle, 0, 0);
        }
        pub fn read(handle: u64, bytes: []u8) Error!usize {
            const n = try call(2, handle, @intFromPtr(bytes.ptr), bytes.len);
            if (n > bytes.len) return error.ProtocolViolation;
            return @intCast(n);
        }
        pub fn send(handle: u64, bytes: []const u8) Error!usize {
            const n = try call(3, handle, @intFromPtr(bytes.ptr), bytes.len);
            if (n == 0 and bytes.len != 0 or n > bytes.len) return error.ProtocolViolation;
            return @intCast(n);
        }
        pub fn wait(handle: u64, want: u64, deadline: u64) Error!u64 {
            if (want == 0 or want & ~@as(u64, 15) != 0) return error.InvalidArgument;
            for (0..31) |_| {
                const ready = try call(5, handle, 0, 0);
                if (ready & want != 0) return ready;
                try pause(deadline);
            }
            return error.TimedOut;
        }
        pub fn resolve(name: []const u8, server: [4]u8) Error![4]u8 {
            if (std.Io.net.Ip4Address.parse(name, 0)) |ip| return ip.bytes else |_| {}
            var request_buffer: [dns_max]u8 = undefined;
            var entropy: [2]u8 = undefined;
            // Validate before touching transport; overlong names never truncate.
            _ = try query(name, 0, &request_buffer);
            if (Driver.call(72, .{ @intFromPtr(&entropy), entropy.len, 0, 0, 0, 0 }) != entropy.len) return error.ProtocolViolation;
            const request = try query(name, std.mem.readInt(u16, &entropy, .little), &request_buffer);
            const handle = try call(8, word(server), @intFromPtr(request.ptr), request.len);
            defer close(handle) catch {
                if (@hasDecl(Driver, "fatal")) Driver.fatal("DnsCloseFailed") else Driver.exit(70);
            };
            const deadline = now() +| budget_ns;
            var reply: [dns_max]u8 = undefined;
            while (true) {
                _ = try wait(handle, 1, deadline);
                const n = call(9, handle, @intFromPtr(&reply), reply.len) catch |err| switch (err) {
                    error.WouldBlock => continue,
                    else => return err,
                };
                if (n > reply.len) return error.ProtocolViolation;
                return parseReply(reply[0..@intCast(n)], request);
            }
        }
    };
}
