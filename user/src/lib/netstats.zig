//! Userland mirror of the kernel's `sys_net_stats` snapshot (slot 62).
//!
//! The struct below is a byte-for-byte mirror of `kernel/src/syscall.zig`'s
//! `NetStats` extern struct. Both sides pin the key `@offsetOf`s and the
//! struct size in host tests, so any drift fails loudly on the next test
//! run — the kernel test "sys_net_stats marshals a whole snapshot and
//! pins the layout" plus the size test at the bottom of this file.
//!
//! All integers little-endian; IPs are raw network-order bytes ([4]u8);
//! MACs raw bytes ([6]u8). Enum naturals are pinned in the kernel doc.

/// Product version — the `ver M.m.p` the component-registry lib row
/// shows and the drift check resolves (docs/component-registry.md).
/// The registry's bump rule says when it moves.
pub const version = "1.0.0";

const std = @import("std");
const ui = @import("ui.zig");

pub const NetStats = extern struct {
    // ---- interface ---------------------------------------------------------
    mac: [6]u8 = .{ 0, 0, 0, 0, 0, 0 },
    own_ip: [4]u8 = .{ 0, 0, 0, 0 },
    gateway: [4]u8 = .{ 0, 0, 0, 0 },
    // ---- dhcp ---------------------------------------------------------------
    dhcp_state: u8 = 0, // 0 idle,1 selecting,2 requesting,3 bound,4 renewing,5 rebinding
    lease_ip: [4]u8 = .{ 0, 0, 0, 0 },
    lease_mask: [4]u8 = .{ 0, 0, 0, 0 },
    lease_server: [4]u8 = .{ 0, 0, 0, 0 },
    lease_secs: u32 = 0,
    // ---- TCP ----------------------------------------------------------------
    tcp_state: u8 = 0, // 0 idle,1 syn_sent,2 established,3 fin_sent,4 closed
    tcp_peer_ip: [4]u8 = .{ 0, 0, 0, 0 },
    tcp_peer_port: u16 = 0,
    // ---- UDP ----------------------------------------------------------------
    udp_count: u8 = 0,
    udp_ports: [4]u16 = .{ 0, 0, 0, 0 },
    // ---- ARP ----------------------------------------------------------------
    arp_count: u8 = 0,
    arp_ips: [4][4]u8 = .{ .{ 0, 0, 0, 0 }, .{ 0, 0, 0, 0 }, .{ 0, 0, 0, 0 }, .{ 0, 0, 0, 0 } },
    arp_macs: [4][6]u8 = .{ .{ 0, 0, 0, 0, 0, 0 }, .{ 0, 0, 0, 0, 0, 0 }, .{ 0, 0, 0, 0, 0, 0 }, .{ 0, 0, 0, 0, 0, 0 } },
    // ---- counters -----------------------------------------------------------
    tx_frames: u64 = 0,
    tx_bytes: u64 = 0,
    rx_frames: u64 = 0,
    rx_bytes: u64 = 0,
    rx_filtered: u64 = 0,
    rx_overflow: u64 = 0,
    // tcp segment counters: syn_sent, synack_recv, ack_sent, data_sent,
    // data_recv, fin_sent, finack_recv, rst_sent
    tcp_segs: [8]u64 = .{ 0, 0, 0, 0, 0, 0, 0, 0 },
    // udp datagram counters: received, sent, loopbacked, dropped
    udp_dgrams: [4]u64 = .{ 0, 0, 0, 0 },
};

pub const net_stats_bytes: usize = @sizeOf(NetStats);

/// Fetch one snapshot. Returns true on success (snapshot fully copied);
/// false when the kernel refused (too-small buffer / EFAULT — the caller
/// keeps its previous view).
pub fn read_stats(snap: *NetStats) bool {
    const rc = ui.syscall2(ui.sys_net_stats_num, @intFromPtr(snap), net_stats_bytes);
    if (rc != net_stats_bytes) return false;
    return true;
}

// Human-readable helpers (pure — host-tested).

pub fn dhcp_state_name(state: u8) []const u8 {
    return switch (state) {
        1 => "selecting",
        2 => "requesting",
        3 => "bound",
        4 => "renewing",
        5 => "rebinding",
        else => "idle",
    };
}

pub fn tcp_state_name(state: u8) []const u8 {
    return switch (state) {
        1 => "SYN-SENT",
        2 => "ESTABLISHED",
        3 => "FIN-WAIT",
        4 => "CLOSED",
        else => "IDLE",
    };
}

test "netstats: struct size and key offsets match the kernel pins" {
    try std.testing.expectEqual(@as(usize, 6), @offsetOf(NetStats, "own_ip"));
    try std.testing.expectEqual(@as(usize, 10), @offsetOf(NetStats, "gateway"));
    try std.testing.expectEqual(@as(usize, 14), @offsetOf(NetStats, "dhcp_state"));
    try std.testing.expectEqual(@as(usize, 28), @offsetOf(NetStats, "lease_secs"));
    try std.testing.expectEqual(@as(usize, 32), @offsetOf(NetStats, "tcp_state"));
    try std.testing.expectEqual(@as(usize, 33), @offsetOf(NetStats, "tcp_peer_ip"));
    try std.testing.expectEqual(@as(usize, 38), @offsetOf(NetStats, "tcp_peer_port"));
    try std.testing.expectEqual(@as(usize, 40), @offsetOf(NetStats, "udp_count"));
    try std.testing.expect(net_stats_bytes <= 256);
}

test "netstats: state name helpers" {
    try std.testing.expectEqualStrings("bound", dhcp_state_name(3));
    try std.testing.expectEqualStrings("idle", dhcp_state_name(0));
    try std.testing.expectEqualStrings("ESTABLISHED", tcp_state_name(2));
    try std.testing.expectEqualStrings("CLOSED", tcp_state_name(4));
}
