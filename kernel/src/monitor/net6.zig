//! Monitor commands: the IPv6 half of `net` (`ip6`/`ndp`/`ping6`/`udp6`).
//!
//! Split out of monitor.zig (#3755): declarations only — the
//! shared types, registry, dispatch, completion, and print/parse
//! helpers stay in the root and are aliased below so the moved
//! code is verbatim.

const std = @import("std");
const mon = @import("../monitor.zig");

const Monitor = mon.Monitor;
const ExecError = mon.ExecError;
const err_prefix = mon.err_prefix;
const print_usage = mon.print_usage;
const parseInt = mon.parseInt;
const lookup = mon.lookup;
const timer = mon.timer;
const virtio_net = mon.virtio_net;
const net = @import("net.zig");
const print_ip6_or_dash = net.print_ip6_or_dash;
const net_ndp_autonomous = net.net_ndp_autonomous;
const parse_port = net.parse_port;
/// `net ip6` — M117b (#2474, ADR 0057 D3/D13): report the v6 address
/// set — the link-local and the SLAAC global with their DAD states,
/// the router record, and the on-link prefix. Report-only: the monitor
/// never assigns v6 addresses (SLAAC forms them).
pub fn cmd_net_ip6(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 0) {
        print_usage(m, lookup("net").?);
        return .usage;
    }
    if (!virtio_net.net_ready) {
        m.console.print_line("net ip6: no virtio-net device");
        return .none;
    }
    virtio_net.ndp.now_ticks = timer.ticks;
    virtio_net.net_rx_drain();
    net_ndp_autonomous(m);
    m.console.puts("net ip6: ll=");
    print_ip6_or_dash(m, virtio_net.ipv6.link_local, virtio_net.ipv6.link_local_state != .none);
    m.console.puts(" ");
    m.console.puts(state_name(virtio_net.ipv6.link_local_state));
    m.console.puts(" g6=");
    print_ip6_or_dash(m, virtio_net.ipv6.global, virtio_net.ipv6.global_state != .none);
    m.console.puts(" ");
    m.console.puts(state_name(virtio_net.ipv6.global_state));
    m.console.puts("\n");
    m.console.puts("net ip6: rt=");
    print_ip6_or_dash(m, virtio_net.ipv6.router_addr, virtio_net.ipv6.router_valid);
    m.console.puts(" lt=");
    m.console.print_u64(virtio_net.ipv6.router_lifetime);
    m.console.puts(" onl=");
    if (virtio_net.ipv6.onlink_valid) {
        var pbuf: [39]u8 = undefined;
        const pn = virtio_net.ipv6.format_ip6(virtio_net.ipv6.onlink_prefix, &pbuf);
        m.console.puts(pbuf[0..pn]);
        m.console.puts("/");
        m.console.print_u64(virtio_net.ipv6.onlink_len);
    } else {
        m.console.puts("-");
    }
    m.console.puts("\n");
    return .none;
}

/// The DAD state tag printed beside each address.
fn state_name(s: virtio_net.ipv6.AddrState) []const u8 {
    return switch (s) {
        .none => "-",
        .tentative => "tentative",
        .valid => "valid",
        .conflicted => "conflicted",
    };
}

/// `net ndp [<v6-addr>]` — M117b (#2474, ADR 0057 D5). No argument:
/// drain the RX ring and print the bounded table + the counters (the
/// `net arp` shape). With an argument: resolve — a table hit prints
/// the peer, a miss transmits a Neighbor Solicitation for the NEXT HOP
/// (the literal address on-link, the router off-link; the NA is
/// learned asynchronously by the drain). Named refusals per D3/D9.
pub fn cmd_net_ndp(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) {
        if (!virtio_net.net_ready) {
            m.console.print_line("net ndp: no virtio-net device");
            return .none;
        }
        virtio_net.net_rx_drain();
        var count: usize = 0;
        for (&virtio_net.ndp.table) |*e| {
            if (e.valid) count += 1;
        }
        m.console.puts("net ndp: entries=");
        m.console.print_u64(@intCast(count));
        m.console.puts("\n");
        var abuf: [39]u8 = undefined;
        var macbuf: [17]u8 = undefined;
        for (&virtio_net.ndp.table) |*e| {
            if (!e.valid) continue;
            const an = virtio_net.ipv6.format_ip6(e.addr, &abuf);
            m.console.puts("net ndp: ");
            m.console.puts(abuf[0..an]);
            m.console.puts(" -> ");
            virtio_net.format_mac(&e.mac, &macbuf);
            m.console.puts(&macbuf);
            m.console.puts("\n");
        }
        m.console.puts("net ndp: rs=");
        m.console.print_u64(virtio_net.ndp.rs_sent);
        m.console.puts(",dad=");
        m.console.print_u64(virtio_net.ndp.dad_sent);
        m.console.puts(",dadc=");
        m.console.print_u64(virtio_net.ndp.dad_conflicts);
        m.console.puts(",ra=");
        m.console.print_u64(virtio_net.ndp.ra_accepted);
        m.console.puts(",for=");
        m.console.print_u64(virtio_net.ndp.ra_foreign);
        m.console.puts(",mal=");
        m.console.print_u64(virtio_net.ndp.ra_malformed);
        m.console.puts(",opt=");
        m.console.print_u64(virtio_net.ndp.opt_skipped);
        m.console.puts(",ns=");
        m.console.print_u64(virtio_net.ndp.ns_sent);
        m.console.puts(",na=");
        m.console.print_u64(virtio_net.ndp.na_sent);
        m.console.puts(",learn=");
        m.console.print_u64(virtio_net.ndp.learned);
        m.console.puts(",unsol=");
        m.console.print_u64(virtio_net.ndp.na_unsolicited);
        m.console.puts(",drop=");
        m.console.print_u64(virtio_net.ndp.ndp_drop + virtio_net.ndp.rs_dropped);
        m.console.puts(",redir=");
        m.console.print_u64(virtio_net.ndp.redirect_dropped);
        m.console.puts(",fail=");
        m.console.print_u64(virtio_net.ndp.reply_tx_fail);
        m.console.puts("\n");
        return .none;
    }
    if (args.len != 1) {
        print_usage(m, lookup("net").?);
        return .usage;
    }
    const addr = virtio_net.ipv6.parse_ip6(args[0]) orelse {
        err_prefix(m);
        m.console.puts("invalid address: ");
        m.console.puts(args[0]);
        m.console.puts("\n");
        return .invalid_argument;
    };
    if (!virtio_net.net_ready) {
        err_prefix(m);
        m.console.print_line("no virtio-net device");
        return .none;
    }
    virtio_net.ndp.now_ticks = timer.ticks;
    virtio_net.net_rx_drain();
    net_ndp_autonomous(m);
    var abuf: [39]u8 = undefined;
    const an = virtio_net.ipv6.format_ip6(addr, &abuf);
    const target = virtio_net.ipv6.next_hop(&addr);
    if (target) |t| {
        if (virtio_net.ndp.lookup(&t)) |mac| {
            m.console.puts("net ndp: ");
            m.console.puts(abuf[0..an]);
            m.console.puts(" is at ");
            var macbuf: [17]u8 = undefined;
            virtio_net.format_mac(&mac, &macbuf);
            m.console.puts(&macbuf);
            m.console.puts("\n");
            return .none;
        }
    }
    var frame_len: usize = 0;
    switch (virtio_net.net_ndp_solicit(addr, &frame_len)) {
        .ok => {
            virtio_net.ndp.ns_sent += 1;
            m.console.puts("net ndp: request for ");
            m.console.puts(abuf[0..an]);
            m.console.puts(" sent (");
            m.console.print_u64(@intCast(frame_len));
            m.console.puts(" bytes)\n");
        },
        .bad_dst6 => {
            err_prefix(m);
            m.console.print_line("bad_dst6 (multicast/unspecified/loopback destination)");
        },
        .no_src6 => {
            err_prefix(m);
            m.console.print_line("no_src6 (no valid link-local — net ip6)");
        },
        .no_route6 => {
            err_prefix(m);
            m.console.print_line("no_route6 (off-link destination and no router)");
        },
        .not_ready => {
            err_prefix(m);
            m.console.print_line("transport unready");
        },
        .timeout => {
            err_prefix(m);
            m.console.print_line("request TX timeout (device did not complete within the poll budget)");
        },
        .no_peer => unreachable, // solicitation goes out regardless of the table
    }
    return .none;
}

/// `net ping6 <v6-addr>` — M117b (#2474, ADR 0057 D6/D9): transmit an
/// ICMPv6 echo request — the Ethernet dst is the next hop's learned MAC
/// (the peer on-link, the router off-link), so the next hop must be in
/// the NDP table (`net ndp <addr>` resolves it first). The reply is
/// observed asynchronously by the RX drain (`pongs_observed` + the
/// `net` report's `seq=`). Named refusals per D3/D9.
pub fn cmd_net_ping6(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 1) {
        print_usage(m, lookup("net").?);
        return .usage;
    }
    const addr = virtio_net.ipv6.parse_ip6(args[0]) orelse {
        err_prefix(m);
        m.console.puts("invalid address: ");
        m.console.puts(args[0]);
        m.console.puts("\n");
        return .invalid_argument;
    };
    if (!virtio_net.net_ready) {
        err_prefix(m);
        m.console.print_line("no virtio-net device");
        return .none;
    }
    virtio_net.ndp.now_ticks = timer.ticks;
    virtio_net.net_rx_drain();
    net_ndp_autonomous(m);
    var frame_len: usize = 0;
    switch (virtio_net.net_ping6_request(addr, &frame_len)) {
        .ok => {
            virtio_net.ipv6.requests_sent += 1;
            virtio_net.ipv6.ping6_seq +%= 1;
            m.console.puts("net ping6: echo request to ");
            var abuf: [39]u8 = undefined;
            const an = virtio_net.ipv6.format_ip6(addr, &abuf);
            m.console.puts(abuf[0..an]);
            m.console.puts(" sent (");
            m.console.print_u64(@intCast(frame_len));
            m.console.puts(" bytes)\n");
        },
        .bad_dst6 => {
            err_prefix(m);
            m.console.print_line("bad_dst6 (multicast/unspecified/loopback destination)");
        },
        .no_route6 => {
            err_prefix(m);
            m.console.print_line("no_route6 (off-link destination and no router)");
        },
        .no_src6 => {
            err_prefix(m);
            m.console.print_line("no_src6 (no valid source address — net ip6)");
        },
        .no_peer => {
            err_prefix(m);
            m.console.print_line("peer not in NDP table (net ndp <addr> first)");
        },
        .not_ready => {
            err_prefix(m);
            m.console.print_line("transport unready");
        },
        .timeout => {
            err_prefix(m);
            m.console.print_line("echo request TX timeout (device did not complete within the poll budget)");
        },
    }
    return .none;
}

/// M117c (#2475, ADR 0057 D8): `net udp6` — the v6 half of `net udp`.
/// Same bounded listen table (family-tagged — a v6 bind never sees a
/// v4 datagram and vice versa) with a v6 literal on `send`. The refuse
/// names are Send6Result's (`bad_dst6`/`no_src6`/`no_route6`/`no_peer`).
pub fn cmd_net_udp6(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) {
        var count: usize = 0;
        for (&virtio_net.udp.listen) |e| {
            if (e.valid and e.v6) count += 1;
        }
        m.console.puts("net udp6: entries=");
        m.console.print_u64(@intCast(count));
        m.console.puts("\n");
        for (&virtio_net.udp.listen) |e| {
            if (!e.valid or !e.v6) continue;
            m.console.puts("net udp6: port=");
            m.console.print_u64(e.port);
            m.console.puts("\n");
        }
        m.console.puts("net udp6: rx=");
        m.console.print_u64(virtio_net.udp.received6);
        m.console.puts(",tx=");
        m.console.print_u64(virtio_net.udp.sent6);
        m.console.puts(",drop=");
        m.console.print_u64(virtio_net.udp.dropped6_len + virtio_net.udp.dropped6_badsum + virtio_net.udp.dropped6_closed + virtio_net.udp.dropped6_dst);
        m.console.puts("\n");
        return .none;
    }
    if (std.mem.eql(u8, args[0], "listen")) {
        if (args.len != 2) {
            print_usage(m, lookup("net").?);
            return .usage;
        }
        const port = parse_port(args[1]) orelse {
            err_prefix(m);
            m.console.puts("invalid port: ");
            m.console.puts(args[1]);
            m.console.puts("\n");
            return .invalid_argument;
        };
        if (!virtio_net.udp.listen_port6(port)) {
            err_prefix(m);
            m.console.print_line("listen failed (table full or duplicate)");
            return .none;
        }
        m.console.puts("net udp6: listening on ");
        m.console.print_u64(port);
        m.console.puts("\n");
        return .none;
    }
    if (std.mem.eql(u8, args[0], "close")) {
        if (args.len != 2) {
            print_usage(m, lookup("net").?);
            return .usage;
        }
        const port = parse_port(args[1]) orelse {
            err_prefix(m);
            m.console.puts("invalid port: ");
            m.console.puts(args[1]);
            m.console.puts("\n");
            return .invalid_argument;
        };
        if (!virtio_net.udp.close_port6(port)) {
            err_prefix(m);
            m.console.puts("not listening (v6) on ");
            m.console.print_u64(port);
            m.console.puts("\n");
            return .none;
        }
        m.console.puts("net udp6: closed ");
        m.console.print_u64(port);
        m.console.puts("\n");
        return .none;
    }
    if (std.mem.eql(u8, args[0], "send")) {
        if (args.len != 4) {
            print_usage(m, lookup("net").?);
            return .usage;
        }
        const addr = virtio_net.ipv6.parse_ip6(args[1]) orelse {
            err_prefix(m);
            m.console.puts("invalid v6 address: ");
            m.console.puts(args[1]);
            m.console.puts("\n");
            return .invalid_argument;
        };
        const port = parse_port(args[2]) orelse {
            err_prefix(m);
            m.console.puts("invalid port: ");
            m.console.puts(args[2]);
            m.console.puts("\n");
            return .invalid_argument;
        };
        const len = parseInt(args[3]) catch {
            err_prefix(m);
            m.console.puts("invalid length: ");
            m.console.puts(args[3]);
            m.console.puts("\n");
            return .invalid_argument;
        };
        if (len < 1 or len > virtio_net.udp.payload_max) {
            err_prefix(m);
            m.console.puts("length must be between 1 and ");
            m.console.print_u64(virtio_net.udp.payload_max);
            m.console.puts("\n");
            return .invalid_argument;
        }
        // The deterministic payload — bytes 01 02 03 04… (the v4 shape).
        var payload: [virtio_net.udp.payload_max]u8 = undefined;
        var pi: usize = 0;
        while (pi < len) : (pi += 1) payload[pi] = @as(u8, @truncate(pi)) +% 1;
        var frame_len: usize = 0;
        switch (virtio_net.net_udp6_send(addr, port, payload[0..@intCast(len)], &frame_len)) {
            .ok => {
                m.console.puts("net udp6: sent ");
                m.console.print_u64(len);
                m.console.puts(" bytes to ");
                var abuf: [39]u8 = undefined;
                const an = virtio_net.ipv6.format_ip6(addr, &abuf);
                m.console.puts(abuf[0..an]);
                m.console.puts(":");
                m.console.print_u64(port);
                m.console.puts(" (");
                m.console.print_u64(@intCast(frame_len));
                m.console.puts(" bytes)\n");
            },
            .not_ready => {
                err_prefix(m);
                m.console.print_line("transport not ready (no virtio-net device)");
            },
            .timeout => {
                err_prefix(m);
                m.console.print_line("datagram TX timeout (device did not complete within the poll budget)");
            },
            .no_peer => {
                err_prefix(m);
                m.console.print_line("next hop not in NDP table (net ndp <v6-addr> first)");
            },
            .no_src6 => {
                err_prefix(m);
                m.console.print_line("no_src6 (no valid v6 source for this dst — net ip6)");
            },
            .no_route6 => {
                err_prefix(m);
                m.console.print_line("no_route6 (off-link dst and no recorded router)");
            },
            .bad_dst6 => {
                err_prefix(m);
                m.console.print_line("bad_dst6 (multicast/unspecified/loopback/mapped destination)");
            },
        }
        return .none;
    }
    if (std.mem.eql(u8, args[0], "recv")) {
        if (args.len > 2) {
            print_usage(m, lookup("net").?);
            return .usage;
        }
        virtio_net.net_rx_drain();
        if (args.len == 2) {
            const port = parse_port(args[1]) orelse {
                m.console.puts("net udp6: invalid port: ");
                m.console.puts(args[1]);
                m.console.puts("\n");
                return .invalid_argument;
            };
            _ = cmd_net_udp6_recv_port(m, port);
            return .none;
        }
        var total: usize = 0;
        for (&virtio_net.udp.listen) |e| {
            if (!e.valid or !e.v6) continue;
            total += cmd_net_udp6_recv_port(m, e.port);
        }
        m.console.puts("net udp6 recv: total=");
        m.console.print_u64(@intCast(total));
        m.console.puts("\n");
        return .none;
    }
    print_usage(m, lookup("net").?);
    return .usage;
}

/// Drain + print the datagrams buffered for ONE v6 listener — the
/// `net udp6 recv` shape: the peer's v6 address is part of the record.
fn cmd_net_udp6_recv_port(m: *Monitor, port: u16) usize {
    var count: usize = 0;
    while (virtio_net.udp.pop6(port)) |d| {
        if (count == 0) {
            m.console.puts("net udp6 recv: port=");
            m.console.print_u64(port);
            m.console.puts("\n");
        }
        m.console.puts("net udp6 recv: [");
        m.console.print_u64(@intCast(count));
        m.console.puts("] len=");
        m.console.print_u64(@intCast(d.len));
        m.console.puts(" from=");
        var abuf: [39]u8 = undefined;
        const an = virtio_net.ipv6.format_ip6(d.src6, &abuf);
        m.console.puts(abuf[0..an]);
        m.console.puts("\n");
        m.console.puts("net udp6 recv: ");
        var bi: usize = 0;
        while (bi < d.len) : (bi += 1) {
            if (bi > 0) m.console.puts(" ");
            const b = d.bytes[bi];
            const hex = "0123456789abcdef";
            var two: [2]u8 = .{ hex[b >> 4], hex[b & 0xf] };
            m.console.puts(&two);
        }
        m.console.puts("\n");
        count += 1;
    }
    if (count == 0) {
        m.console.puts("net udp6 recv: no datagrams for port ");
        m.console.print_u64(port);
        m.console.puts("\n");
    }
    return count;
}
