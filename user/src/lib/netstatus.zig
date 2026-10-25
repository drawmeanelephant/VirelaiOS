//! N13/N14 (march-m26): network preflight for userland net apps.
//!
//! FETCH.BIN (retired with user/src/fetch.zig in M78b, #1683) used to call
//! `check()` (one `sys_net_stats` snapshot, slot 62 — the M26 N2 seam, zero
//! new kernel surface) before its first network operation; its successor,
//! GOFETCH.ELF's cleartext mode, calls the Go twin below instead. The pure
//! classifier in this file turns a snapshot into a diagnosis so the app
//! exits FAST with a human message instead of burning its bounded timeout /
//! printing generic send failures. PING.BIN was the other caller until M71n
//! (#1573) retired it: its Go successor GOPING.ELF reads the same snapshot
//! through the Go twin, user/go/vi/netdiag.go, which pins the SAME message
//! shapes this file renders.
//!
//! Honest bounds (mirror of the claim notes):
//!   - Device-absence and IP-unset are INDISTINGUISHABLE from EL0: slot 62
//!     has no link flag and `net_mac` falls back to a nonzero constant, so
//!     `own_ip == 0.0.0.0` is the only offline signal. One message covers
//!     both and suggests both remedies (`net ip` / `net dhcp`).
//!   - No-route: IP set but the destination absent from the ARP table.
//!     The kernel returns generic EINVAL on an unresolved peer; the
//!     classification comes from the snapshot (arp_count/arp_ips), not a
//!     new errno.
//!   - Snapshot-refused (kernel < slot 62 or EFAULT): reported as
//!     `.unknown` — the caller keeps its legacy behavior rather than
//!     guessing. Never fabricate a diagnosis.
//!
//! All logic is pure and host-tested; only `check()` touches the syscall.

/// Product version — the `ver M.m.p` the component-registry lib row
/// shows and the drift check resolves (docs/component-registry.md).
/// The registry's bump rule says when it moves.
pub const version = "1.0.0";

const std = @import("std");
const netstats = @import("netstats.zig");
const ui = @import("ui.zig");

pub const Diagnosis = enum {
    /// own_ip unset — no device, or device present but unconfigured.
    offline_no_ip,
    /// own_ip set, destination not in the ARP table.
    no_route,
    /// own_ip set and destination resolvable.
    ready,
    /// snapshot refused — caller keeps legacy behavior.
    unknown,
};

pub const Verdict = struct {
    diagnosis: Diagnosis,
    /// A copy of the snapshot when diagnosis != .unknown (zeroes otherwise).
    snap: netstats.NetStats = .{},
};

/// Classify one snapshot against a destination IP. Pure.
pub fn classify(snap: netstats.NetStats, dest: [4]u8) Diagnosis {
    const unset = [4]u8{ 0, 0, 0, 0 };
    if (std.mem.eql(u8, &snap.own_ip, &unset)) return .offline_no_ip;
    if (std.mem.eql(u8, &dest, &snap.own_ip)) return .ready; // loopback-ish: no ARP needed
    var i: usize = 0;
    while (i < snap.arp_count and i < 4) : (i += 1) {
        if (std.mem.eql(u8, &snap.arp_ips[i], &dest)) return .ready;
    }
    return .no_route;
}

/// Fetch one snapshot and classify. The ONLY impure function.
pub fn check(dest: [4]u8) Verdict {
    var snap: netstats.NetStats = .{};
    if (!netstats.read_stats(&snap)) return .{ .diagnosis = .unknown };
    return .{ .diagnosis = classify(snap, dest), .snap = snap };
}

/// Render the N14 user-facing one-liner for a diagnosis into `out`.
/// Prefix is "ping" or "fetch" (the program's own name). Pure.
pub fn format_message(out: []u8, prog: []const u8, d: Diagnosis, dest: [4]u8) []const u8 {
    var ip_txt: [16]u8 = undefined;
    const ip_s = std.fmt.bufPrint(&ip_txt, "{d}.{d}.{d}.{d}", .{ dest[0], dest[1], dest[2], dest[3] }) catch "?";
    return switch (d) {
        .offline_no_ip => std.fmt.bufPrint(out, "{s}: offline — no IP address (set one: net ip <a.b.c.d> or net dhcp)\n", .{prog}) catch out[0..0],
        .no_route => std.fmt.bufPrint(out, "{s}: no route to {s} (resolve first: net arp <a.b.c.d>)\n", .{ prog, ip_s }) catch out[0..0],
        // Callers never print .ready/.unknown through this; give an honest
        // non-empty string anyway so a future caller cannot print garbage.
        .ready => std.fmt.bufPrint(out, "{s}: network ready\n", .{prog}) catch out[0..0],
        .unknown => std.fmt.bufPrint(out, "{s}: network status unavailable\n", .{prog}) catch out[0..0],
    };
}

// ---------------------------------------------------------------------------
// Host tests — pure classifier + message formatting
// ---------------------------------------------------------------------------

const unset_ip = [4]u8{ 0, 0, 0, 0 };

fn snap_with(own_ip: [4]u8, arp: []const [4]u8) netstats.NetStats {
    var s: netstats.NetStats = .{};
    s.own_ip = own_ip;
    const n = @min(arp.len, 4);
    var i: usize = 0;
    while (i < n) : (i += 1) {
        s.arp_ips[i] = arp[i];
        s.arp_macs[i] = .{ 2, 0, 0, 0, 0, @intCast(i + 1) };
    }
    s.arp_count = @intCast(n);
    return s;
}

test "netstatus: offline when own_ip unset (device-absent and unconfigured alike)" {
    const d = classify(snap_with(unset_ip, &.{}), .{ 10, 0, 0, 2 });
    try std.testing.expectEqual(Diagnosis.offline_no_ip, d);
    // A populated ARP table with own_ip unset is not observable in practice
    // (ARP answers require an own IP), but if it ever happens the honest
    // answer is still offline: no source address means no sends.
    const d2 = classify(snap_with(unset_ip, &.{.{ 10, 0, 0, 2 }}), .{ 10, 0, 0, 2 });
    try std.testing.expectEqual(Diagnosis.offline_no_ip, d2);
}

test "netstatus: ready when destination is in the ARP table" {
    const s = snap_with(.{ 10, 0, 0, 1 }, &.{ .{ 10, 0, 0, 2 }, .{ 192, 168, 64, 1 } });
    try std.testing.expectEqual(Diagnosis.ready, classify(s, .{ 10, 0, 0, 2 }));
    try std.testing.expectEqual(Diagnosis.ready, classify(s, .{ 192, 168, 64, 1 }));
    try std.testing.expectEqual(Diagnosis.no_route, classify(s, .{ 10, 0, 0, 99 }));
}

test "netstatus: no route when IP set but table lacks the destination" {
    const s = snap_with(.{ 10, 0, 0, 1 }, &.{});
    try std.testing.expectEqual(Diagnosis.no_route, classify(s, .{ 10, 0, 0, 2 }));
}

test "netstatus: own-IP destination needs no ARP entry (UDP loopback precedent)" {
    const s = snap_with(.{ 10, 0, 0, 1 }, &.{});
    try std.testing.expectEqual(Diagnosis.ready, classify(s, .{ 10, 0, 0, 1 }));
}

test "netstatus: arp_count above 4 never reads past the packed slots" {
    var s = snap_with(.{ 10, 0, 0, 1 }, &.{});
    s.arp_count = 200; // hostile/corrupt snapshot
    try std.testing.expectEqual(Diagnosis.no_route, classify(s, .{ 1, 2, 3, 4 }));
}

test "netstatus: N14 message shapes are stable for the gate" {
    var buf: [128]u8 = undefined;
    const off = format_message(&buf, "ping", .offline_no_ip, .{ 10, 0, 0, 2 });
    try std.testing.expectEqualStrings("ping: offline — no IP address (set one: net ip <a.b.c.d> or net dhcp)\n", off);
    var buf2: [128]u8 = undefined;
    const route = format_message(&buf2, "fetch", .no_route, .{ 10, 0, 0, 2 });
    try std.testing.expectEqualStrings("fetch: no route to 10.0.0.2 (resolve first: net arp <a.b.c.d>)\n", route);
    var buf3: [128]u8 = undefined;
    const via = format_message(&buf3, "ping", .no_route, .{ 192, 168, 64, 1 });
    try std.testing.expectEqualStrings("ping: no route to 192.168.64.1 (resolve first: net arp <a.b.c.d>)\n", via);
}

test "netstatus: format_message never returns a longer slice than out" {
    var tiny: [8]u8 = undefined;
    const s = format_message(&tiny, "ping", .offline_no_ip, .{ 10, 0, 0, 2 });
    try std.testing.expect(s.len <= tiny.len);
}

// Host stub: read_stats on host stubs to rc=0 != net_stats_bytes, so check()
// must report .unknown there — pinned so the host/live split stays honest.
test "netstatus: check() reports unknown when the snapshot is refused" {
    // On the host build the syscall stub returns 0; read_stats returns false.
    const v = check(.{ 10, 0, 0, 2 });
    try std.testing.expectEqual(Diagnosis.unknown, v.diagnosis);
}
