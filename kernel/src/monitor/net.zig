//! Monitor commands: the `net` verb tree — virtio-net transport, IPv4, ARP, ICMP, UDP, DHCP, DNS — plus `netsend` and `vsock`.
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
const csprng = mon.csprng;
const virtio_net = mon.virtio_net;
const virtio_vsock = mon.virtio_vsock;
const dns = mon.dns;
const events = mon.events;
const vsock_ctl = @import("../vsock_ctl.zig");
const net_tcp = @import("net_tcp.zig");
const cmd_net_tcp = net_tcp.cmd_net_tcp;
const net6 = @import("net6.zig");
const cmd_net_ip6 = net6.cmd_net_ip6;
const cmd_net_ndp = net6.cmd_net_ndp;
const cmd_net_ping6 = net6.cmd_net_ping6;
const cmd_net_udp6 = net6.cmd_net_udp6;
/// `vsock [<port>]` (M115a, issue #2460) — the guest→host connect half of
/// the `VZVirtioSocketDevice` save/restore probe. With no argument it
/// re-prints the bare arm line — the spec's whole-line witness, identical
/// pre-save and post-restore since it carries no device state. `vsock
/// <port>` issues one stream OP_REQUEST to VMADDR_CID_HOST:<port> on the
/// tx queue and reports the rx-ring answer: `connected` (OP_RESPONSE — the
/// host's VZVirtioSocketListener accepted), `refused` (OP_RST — no
/// listener on that port), or `timeout` (no answer inside the poll budget
/// — the degraded verdict a broken restore would produce). Absent/unarmed
/// device reports honestly.
pub fn cmd_vsock(m: *Monitor, args: []const []const u8) ExecError {
    // M115b (#2461, ADR 0056): `vsock ctl [badauth|replay|dup] <verb>
    // [name]` runs one virelai-ctl/1 session against the runner's
    // --vsock-ctl-port listener — connect, HMAC-challenge auth, request
    // line, reply verdict, shutdown. The exercise modes exist for the
    // gate's auth negatives.
    if (args.len > 0 and std.mem.eql(u8, args[0], "ctl")) {
        vsock_ctl.run(m.console, args[1..]);
        return .none;
    }
    if (!virtio_vsock.vs_ready) {
        m.console.print_line("vsock: absent (no virtio-vsock device armed)");
        return .none;
    }
    m.console.print_line("vsock: ready did=0x1053");
    if (args.len == 0) {
        m.console.puts("vsock: dev=");
        m.console.print_hex(virtio_vsock.vs_dev);
        m.console.puts(" cid=");
        m.console.print_hex(virtio_vsock.vs_guest_cid);
        m.console.puts("\n");
        return .none;
    }
    const parsed = parseInt(args[0]) catch {
        err_prefix(m);
        m.console.puts("invalid port: ");
        m.console.puts(args[0]);
        m.console.puts("\n");
        return .invalid_argument;
    };
    if (parsed == 0 or parsed > 0xffff_fffe) {
        err_prefix(m);
        m.console.puts("port out of range: ");
        m.console.puts(args[0]);
        m.console.puts("\n");
        return .invalid_argument;
    }
    const port: u32 = @intCast(parsed);
    m.console.puts("vsock: connect port=");
    m.console.print_u64(port);
    m.console.puts("\n");
    switch (virtio_vsock.connect(port)) {
        .connected => {
            m.console.puts("vsock: connected port=");
            m.console.print_u64(port);
            m.console.puts(" sport=");
            m.console.print_u64(virtio_vsock.vs_last_src_port);
            m.console.puts("\n");
        },
        .refused => {
            m.console.puts("vsock: refused port=");
            m.console.print_u64(port);
            m.console.puts(" (peer RST)\n");
        },
        .timeout => {
            m.console.puts("vsock: timeout port=");
            m.console.print_u64(port);
            m.console.puts(" (no reply inside the poll budget)\n");
        },
    }
    return .none;
}

// ---------------------------------------------------------------------------
// Net commands (milestone five card N1 — claim 1373)
// ---------------------------------------------------------------------------

/// `net` — report the virtio-net transport: the OBSERVED device DID +
/// class + bus slot, the MAC (with its source: the VIRTIO_NET_F_MAC
/// feature read or the fixed BSS fallback), the negotiated feature bits
/// (low/high), the two queues (q0 RX / q1 TX) with their sizes, the
/// device status + post-exit re-arm state, the TX counters, and (card
/// N2, claim 6076) the RX counters (frames received into the FIFO,
/// filtered/dropped, FIFO overflow + occupancy) + the rx-obs record
/// (last device-written length + first 16 bytes — the claim-time header
/// question). Grep-able and deterministic (the mbox/procs observability
/// shape). Honest when the device is absent: the default runner attaches
/// no network device. `net recv` is the card-N2 subcommand: drain the RX
/// used ring and print the received frame(s) byte-exact.
pub fn cmd_net(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len > 0) {
        if (std.mem.eql(u8, args[0], "recv")) return cmd_net_recv(m, args[1..]);
        if (std.mem.eql(u8, args[0], "ip")) return cmd_net_ip(m, args[1..]);
        if (std.mem.eql(u8, args[0], "arp")) return cmd_net_arp(m, args[1..]);
        if (std.mem.eql(u8, args[0], "ping")) return cmd_net_ping(m, args[1..]);
        if (std.mem.eql(u8, args[0], "udp")) return cmd_net_udp(m, args[1..]);
        if (std.mem.eql(u8, args[0], "dhcp")) return cmd_net_dhcp(m, args[1..]);
        if (std.mem.eql(u8, args[0], "tcp")) return cmd_net_tcp(m, args[1..]);
        if (std.mem.eql(u8, args[0], "dns")) return cmd_net_dns(m, args[1..]);
        if (std.mem.eql(u8, args[0], "ip6")) return cmd_net_ip6(m, args[1..]);
        if (std.mem.eql(u8, args[0], "ndp")) return cmd_net_ndp(m, args[1..]);
        if (std.mem.eql(u8, args[0], "ping6")) return cmd_net_ping6(m, args[1..]);
        if (std.mem.eql(u8, args[0], "udp6")) return cmd_net_udp6(m, args[1..]);
        if (std.mem.eql(u8, args[0], "status")) return cmd_net_status(m, args[1..]);
        if (std.mem.eql(u8, args[0], "route")) return cmd_net_route(m, args[1..]);
        if (std.mem.eql(u8, args[0], "log")) return cmd_net_log(m, args[1..]);
        print_usage(m, lookup("net").?);
        return .usage;
    }
    if (!virtio_net.net_ready) {
        m.console.puts("net: no virtio-net device (");
        m.console.puts(if (virtio_net.net_fail.len > 0) virtio_net.net_fail else "DID 0x1041 not found on bus 0");
        m.console.puts(")\n");
        // The offered features as observed (the honest claim-time record
        // when negotiation fails).
        m.console.puts("net: device-features=");
        m.console.print_hex(virtio_net.net_dev_feats_lo);
        m.console.puts("/");
        m.console.print_hex(virtio_net.net_dev_feats_hi);
        m.console.puts("\n");
        // The honest claim-time record when negotiation fails: the status
        // the device returned after the FEATURES_OK write (0x03 = it
        // rejected the accepted features; 0x00 = the status writes never
        // landed), what we actually accepted, and where the regions
        // resolved (so a BAR/capability walk mistake is visible).
        m.console.puts("net: status=");
        m.console.print_hex(virtio_net.net_status_last);
        m.console.puts(" accepted=");
        m.console.print_hex(virtio_net.net_dev.feats_lo);
        m.console.puts("/");
        m.console.print_hex(virtio_net.net_dev.feats_hi);
        m.console.puts("\n");
        m.console.puts("net: common=");
        m.console.print_hex(virtio_net.net_common);
        m.console.puts(" notify=");
        m.console.print_hex(virtio_net.net_notify);
        m.console.puts(" devcfg=");
        m.console.print_hex(virtio_net.net_devcfg);
        m.console.puts(" bar0=");
        m.console.print_hex(virtio_net.net_bar0);
        m.console.puts("\n");
        return .none;
    }
    // print_hex already emits "0x" + full 16 hex digits (the mbox/procs
    // observability shape) — no manual prefix.
    m.console.puts("net: did=");
    m.console.print_hex(virtio_net.net_did);
    m.console.puts(" class=");
    m.console.print_hex(virtio_net.net_class);
    m.console.puts(" dev=");
    m.console.print_u64(@intCast(virtio_net.net_dev_no));
    m.console.puts("\n");
    m.console.puts("net: mac=");
    m.console.puts(&virtio_net.net_mac_text);
    m.console.puts(" source=");
    m.console.puts(switch (virtio_net.net_mac_source) {
        .feature => "feature",
        .fallback => "fallback",
    });
    m.console.puts("\n");
    // The raw device-config MAC bytes captured pre-exit (the claim-1373
    // record: whether the host-set address is exposed at config offset 0
    // even without the MAC feature).
    if (virtio_net.net_devcfg_mac_seen) {
        m.console.puts("net: cfgmac=");
        var ci: usize = 0;
        while (ci < 6) : (ci += 1) {
            if (ci > 0) m.console.puts(":");
            const b = virtio_net.net_devcfg_mac_obs[ci];
            const hex = "0123456789abcdef";
            var two: [2]u8 = .{ hex[b >> 4], hex[b & 0xf] };
            m.console.puts(&two);
        }
        m.console.puts("\n");
    }
    m.console.puts("net: feat=");
    m.console.print_hex(virtio_net.net_dev.feats_lo);
    m.console.puts("/");
    m.console.print_hex(virtio_net.net_dev.feats_hi);
    m.console.puts(" q0=rx:size=");
    m.console.print_u64(@intCast(virtio_net.queue_size));
    m.console.puts(" q1=tx:size=");
    m.console.print_u64(@intCast(virtio_net.queue_size));
    m.console.puts("\n");
    // M198a (#3349): the device's OFFERED feature bits on the success
    // path too (the failure branch already prints them) — the
    // multicast-probe evidence for whether VIRTIO_NET_F_CTRL_VQ
    // (bit 17) / CTRL_MAC_ADDR (bit 23) programming is even possible.
    m.console.puts("net: offered=");
    m.console.print_hex(virtio_net.net_dev_feats_lo);
    m.console.puts("/");
    m.console.print_hex(virtio_net.net_dev_feats_hi);
    m.console.puts("\n");
    m.console.puts("net: status=");
    m.console.print_hex(virtio_net.net_status_last);
    m.console.puts(" rearm=");
    m.console.print_u64(if (virtio_net.net_rearmed) 1 else 0);
    m.console.puts(" tx=frames=");
    m.console.print_u64(virtio_net.net_dev.tx_frames);
    m.console.puts(",bytes=");
    m.console.print_u64(virtio_net.net_dev.tx_bytes);
    m.console.puts("\n");
    // Card N2 (claim 6076): RX counters + the rx-obs record. The drain is
    // idempotent (also run from the shell idle loop) so the report is
    // current when the frame has already landed.
    virtio_net.net_rx_drain();
    m.console.puts("net: rx=frames=");
    m.console.print_u64(virtio_net.rx_frames);
    m.console.puts(",bytes=");
    m.console.print_u64(virtio_net.rx_bytes);
    m.console.puts(",filtered=");
    m.console.print_u64(virtio_net.rx_filtered);
    m.console.puts(",overflow=");
    m.console.print_u64(virtio_net.rx_overflow);
    m.console.puts(",fifo=");
    m.console.print_u64(@intCast(virtio_net.fifo_occupancy()));
    // M198a (#3349): multicast-delivery evidence for the probe leg —
    // accepted group-dst frames, the mDNS subset, and multicast the
    // device delivered but the filter refused (delivery proven even
    // when nothing admitted it).
    m.console.puts(",mcast=");
    m.console.print_u64(virtio_net.rx_mcast);
    m.console.puts(",mdns=");
    m.console.print_u64(virtio_net.rx_mdns);
    m.console.puts(",mcast-filt=");
    m.console.print_u64(virtio_net.rx_mcast_filtered);
    m.console.puts("\n");
    // The raw ring state (the claim-time record): the guest's avail.idx
    // vs the device's used.idx tell whether a delivery ever completed.
    m.console.puts("net: rx-rings avail=");
    m.console.print_u64(@intCast(virtio_net.net_dev.rx_avail.idx));
    m.console.puts(" used=");
    m.console.print_u64(@intCast(virtio_net.net_dev.rx_used.idx));
    m.console.puts(" armed=");
    m.console.print_u64(if (virtio_net.rx_armed) 1 else 0);
    m.console.puts("\n");
    // The claim-time observation record: the last device-written length +
    // the first 16 bytes pin the RX-header question (12 zero bytes + dst
    // MAC = header present; dst MAC at 0 = none). Recorded even when the
    // filter dropped the frame, so a drop is distinguishable from a
    // failed delivery.
    m.console.puts("net: rx-obs len=");
    m.console.print_u64(virtio_net.rx_last_len);
    m.console.puts(" first16=");
    var oi: usize = 0;
    while (oi < 16) : (oi += 1) {
        if (oi > 0) m.console.puts(" ");
        const b = virtio_net.rx_first16[oi];
        const hex = "0123456789abcdef";
        var two: [2]u8 = .{ hex[b >> 4], hex[b & 0xf] };
        m.console.puts(&two);
    }
    m.console.puts("\n");
    // Card N3 (claim 7293): the ARP layer — our static IP (0.0.0.0 when
    // unset — DHCP is a later card, honest bound) + the counters
    // (requests sent, replies answered, replies learned, dropped,
    // reply TX failures). Grep-able and deterministic.
    m.console.puts("net: ip=");
    var ipbuf: [15]u8 = undefined;
    const ipn = virtio_net.arp.format_ip(virtio_net.arp.own_ip, &ipbuf);
    m.console.puts(ipbuf[0..ipn]);
    m.console.puts(" arp=req=");
    m.console.print_u64(virtio_net.arp.requests_sent);
    m.console.puts(",repl=");
    m.console.print_u64(virtio_net.arp.replies_sent);
    m.console.puts(",learn=");
    m.console.print_u64(virtio_net.arp.replies_learned);
    m.console.puts(",drop=");
    m.console.print_u64(virtio_net.arp.dropped);
    m.console.puts(",fail=");
    m.console.print_u64(virtio_net.arp.reply_tx_fail);
    m.console.puts("\n");
    // Card N4 (claim 0148): the IPv4/ICMP layer — echo requests sent
    // (`net ping`), echo replies answered, pongs observed (an echo
    // reply's id/seq echoed back — the ping proof), drops by cause, and
    // the last observed sequence. Grep-able and deterministic.
    m.console.puts(" icmp=req=");
    m.console.print_u64(virtio_net.ipv4.requests_sent);
    m.console.puts(",repl=");
    m.console.print_u64(virtio_net.ipv4.replies_sent);
    m.console.puts(",pong=");
    m.console.print_u64(virtio_net.ipv4.pongs_observed);
    m.console.puts(",drop=");
    m.console.print_u64(virtio_net.ipv4.dropped_short + virtio_net.ipv4.dropped_frag + virtio_net.ipv4.dropped_checksum + virtio_net.ipv4.dropped_proto + virtio_net.ipv4.dropped_other);
    m.console.puts(",fail=");
    m.console.print_u64(virtio_net.ipv4.reply_tx_fail);
    m.console.puts(",seq=");
    m.console.print_u64(virtio_net.ipv4.last_seq);
    m.console.puts("\n");
    // Card N5 (claim 8552): the UDP layer — datagrams received (into a
    // listener's buffer, incl. loopback), sent (device TX + loopback),
    // loopbacked (the no-device path), and dropped (the sum of the four
    // counted causes). Grep-able and deterministic.
    m.console.puts(" udp=rx=");
    m.console.print_u64(virtio_net.udp.received);
    m.console.puts(",tx=");
    m.console.print_u64(virtio_net.udp.sent);
    m.console.puts(",loop=");
    m.console.print_u64(virtio_net.udp.loopbacked);
    m.console.puts(",drop=");
    m.console.print_u64(virtio_net.udp.dropped_badsum + virtio_net.udp.dropped_closed + virtio_net.udp.dropped_len + virtio_net.udp.dropped_bcast);
    m.console.puts("\n");
    // Card N8 (claim 0351) + card N9 (claim 9489): the DHCP client — the
    // state (bound/unbound/renewing/rebinding), the lease
    // (ip/mask/gw/server/lease — zeros when unbound), the handshake
    // counters (discover sent, offer recv, request sent, ack recv, nack,
    // timeout, malformed), and the lease-lifecycle counters (renewing
    // REQUESTs sent, rebinding REQUESTs sent, renewals ACKed, expiries
    // enforced). Grep-able and deterministic. The lifecycle counters
    // APPEND at the end — the N8 gate's substring assertions stay green.
    m.console.puts(" dhcp=");
    m.console.puts(switch (virtio_net.dhcp.state) {
        .idle => "idle",
        .selecting => "selecting",
        .requesting => "requesting",
        .bound => "bound",
        .renewing => "renewing",
        .rebinding => "rebinding",
    });
    m.console.puts(",ip=");
    var dibuf: [15]u8 = undefined;
    const din = virtio_net.arp.format_ip(virtio_net.dhcp.lease_ip, &dibuf);
    m.console.puts(dibuf[0..din]);
    m.console.puts(",mask=");
    const dmn = virtio_net.arp.format_ip(virtio_net.dhcp.lease_mask, &dibuf);
    m.console.puts(dibuf[0..dmn]);
    m.console.puts(",gw=");
    const dgn = virtio_net.arp.format_ip(virtio_net.dhcp.lease_gw, &dibuf);
    m.console.puts(dibuf[0..dgn]);
    m.console.puts(",server=");
    const dsn = virtio_net.arp.format_ip(virtio_net.dhcp.lease_server, &dibuf);
    m.console.puts(dibuf[0..dsn]);
    m.console.puts(",lease=");
    m.console.print_u64(virtio_net.dhcp.lease_time);
    m.console.puts(",discover=");
    m.console.print_u64(virtio_net.dhcp.discover_sent);
    m.console.puts(",offer=");
    m.console.print_u64(virtio_net.dhcp.offer_recv);
    m.console.puts(",request=");
    m.console.print_u64(virtio_net.dhcp.request_sent);
    m.console.puts(",ack=");
    m.console.print_u64(virtio_net.dhcp.ack_recv);
    m.console.puts(",nack=");
    m.console.print_u64(virtio_net.dhcp.nack_recv);
    m.console.puts(",timeout=");
    m.console.print_u64(virtio_net.dhcp.timed_out);
    m.console.puts(",mal=");
    m.console.print_u64(virtio_net.dhcp.dropped_malformed);
    // Card N9 (claim 9489): the lease lifecycle counters (append-only).
    m.console.puts(",renew=");
    m.console.print_u64(virtio_net.dhcp.renew_sent);
    m.console.puts(",rebind=");
    m.console.print_u64(virtio_net.dhcp.rebind_sent);
    m.console.puts(",renewed=");
    m.console.print_u64(virtio_net.dhcp.renewed);
    m.console.puts(",expired=");
    m.console.print_u64(virtio_net.dhcp.expired);
    m.console.puts("\n");
    // Card N10 (claim 7026): the bounded TCP client — the state
    // (idle/syn_sent/established/fin_sent/closed), the peer (zeros when
    // unbound), and the counters (SYNs sent, SYN-ACKs recv, ACKs sent,
    // data sent/recv, FINs sent, FIN-ACKs recv, RSTs sent/recv, the
    // connect-timeout refusals, and the drop sum — badsum + malformed).
    // Grep-able and deterministic.
    m.console.puts(" tcp=");
    m.console.puts(switch (virtio_net.tcp.state) {
        .idle => "idle",
        .listen => "listen",
        .syn_sent => "syn_sent",
        .syn_received => "syn_received",
        .established => "established",
        .fin_sent => "fin_sent",
        .closed => "closed",
    });
    m.console.puts(",peer=");
    var tibuf: [15]u8 = undefined;
    const tin = virtio_net.arp.format_ip(virtio_net.tcp.peer_ip, &tibuf);
    m.console.puts(tibuf[0..tin]);
    m.console.puts(":");
    m.console.print_u64(virtio_net.tcp.peer_port);
    m.console.puts(",syn=");
    m.console.print_u64(virtio_net.tcp.syn_sent);
    m.console.puts(",synack=");
    m.console.print_u64(virtio_net.tcp.synack_recv);
    m.console.puts(",ack=");
    m.console.print_u64(virtio_net.tcp.ack_sent);
    m.console.puts(",data_s=");
    m.console.print_u64(virtio_net.tcp.data_sent);
    m.console.puts(",data_r=");
    m.console.print_u64(virtio_net.tcp.data_recv);
    m.console.puts(",fin=");
    m.console.print_u64(virtio_net.tcp.fin_sent);
    m.console.puts(",finack=");
    m.console.print_u64(virtio_net.tcp.finack_recv);
    m.console.puts(",rst_s=");
    m.console.print_u64(virtio_net.tcp.rst_sent);
    m.console.puts(",rst_r=");
    m.console.print_u64(virtio_net.tcp.rst_recv);
    m.console.puts(",timedout=");
    m.console.print_u64(virtio_net.tcp.timed_out);
    m.console.puts(",mal=");
    m.console.print_u64(virtio_net.tcp.dropped_badsum + virtio_net.tcp.dropped_malformed);
    // Card N11 (claim 5357): the retransmission counters APPENDED at the
    // end (the N9 append-never-change convention — the N10 gate's
    // substring assertions stay green). retx = segment retransmissions,
    // abort = the retransmission-bound aborts.
    m.console.puts(",retx=");
    m.console.print_u64(virtio_net.tcp.retransmitted);
    m.console.puts(",abort=");
    m.console.print_u64(virtio_net.tcp.retx_aborted);
    m.console.puts("\n");
    // Milestone 12 Card N2 (claim 7566): DNS resolver counters
    m.console.puts(" dns=");
    m.console.puts(switch (dns.state) {
        .idle => "idle",
        .query_sent => "query_sent",
        .resolved => "resolved",
        .failed => "failed",
    });
    m.console.puts(",q=");
    m.console.print_u64(dns.queries_sent);
    m.console.puts(",r=");
    m.console.print_u64(dns.responses_recv);
    m.console.puts(",err=");
    m.console.print_u64(dns.responses_err);
    m.console.puts(",timeout=");
    m.console.print_u64(dns.timed_out);
    m.console.puts("\n");
    // M117b (#2474, ADR 0057 D13): the v6 layer — the address set, the
    // router + on-link records, the NDP and ICMPv6 counters. Appended
    // under the standing append-only convention — existing substring
    // asserts stay green.
    virtio_net.ndp.now_ticks = timer.ticks;
    net_ndp_autonomous(m); // promote/conflict transitions print even here
    m.console.puts(" v6=ll=");
    print_ip6_or_dash(m, virtio_net.ipv6.link_local, virtio_net.ipv6.link_local_state != .none);
    m.console.puts(",g6=");
    print_ip6_or_dash(m, virtio_net.ipv6.global, virtio_net.ipv6.global_state != .none);
    m.console.puts(",rt=");
    print_ip6_or_dash(m, virtio_net.ipv6.router_addr, virtio_net.ipv6.router_valid);
    m.console.puts(",onl=");
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
    m.console.puts(" ndp=rs=");
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
    m.console.puts(" icmp6=req=");
    m.console.print_u64(virtio_net.ipv6.requests_sent);
    m.console.puts(",repl=");
    m.console.print_u64(virtio_net.ipv6.replies_sent);
    m.console.puts(",pong=");
    m.console.print_u64(virtio_net.ipv6.pongs_observed);
    m.console.puts(",err=");
    m.console.print_u64(virtio_net.ipv6.icmp6_err);
    m.console.puts(",drop=");
    m.console.print_u64(virtio_net.ipv6.dropped_short + virtio_net.ipv6.dropped_len + virtio_net.ipv6.dropped_nexthdr + virtio_net.ipv6.dropped_dst + virtio_net.ipv6.dropped_checksum + virtio_net.ipv6.dropped_proto + virtio_net.ipv6.dropped_type);
    m.console.puts(",fail=");
    m.console.print_u64(virtio_net.ipv6.reply_tx_fail);
    m.console.puts(",seq=");
    m.console.print_u64(virtio_net.ipv6.last_seq);
    m.console.puts("\n");
    // M117c (#2475): the v6 UDP row — family-tagged counters so a
    // `udp6=` fragment names the family's own traffic (the v4 ` udp=`
    // row above stays byte-identical for a guest that never sees 0x86dd).
    m.console.puts(" udp6=rx=");
    m.console.print_u64(virtio_net.udp.received6);
    m.console.puts(",tx=");
    m.console.print_u64(virtio_net.udp.sent6);
    m.console.puts(",drop=");
    m.console.print_u64(virtio_net.udp.dropped6_len + virtio_net.udp.dropped6_badsum + virtio_net.udp.dropped6_closed + virtio_net.udp.dropped6_dst);
    m.console.puts("\n");
    return .none;
}

/// Print a v6 address or "-" (unset). The 39-byte buffer covers the
/// longest uncompressed form.
pub fn print_ip6_or_dash(m: *Monitor, addr: virtio_net.ipv6.Addr, set: bool) void {
    if (!set) {
        m.console.puts("-");
        return;
    }
    var buf: [39]u8 = undefined;
    const n = virtio_net.ipv6.format_ip6(addr, &buf);
    m.console.puts(buf[0..n]);
}

/// M117b (#2474, ADR 0057 D4/D5): the autonomous NDP transitions —
/// drive the tick-seam poll (the net_dhcp_autonomous shape) and print
/// the DAD promotes + conflicts as they happen, one line per event —
/// the live gate's deterministic stage markers.
pub fn net_ndp_autonomous(m: *Monitor) void {
    const ev = virtio_net.net_ndp_poll();
    var buf: [39]u8 = undefined;
    if (ev.promoted_ll) {
        const n = virtio_net.ipv6.format_ip6(virtio_net.ipv6.link_local, &buf);
        m.console.puts("net: v6 ll=");
        m.console.puts(buf[0..n]);
        m.console.puts(" valid\n");
    }
    if (ev.promoted_g6) {
        const n = virtio_net.ipv6.format_ip6(virtio_net.ipv6.global, &buf);
        m.console.puts("net: v6 g6=");
        m.console.puts(buf[0..n]);
        m.console.puts(" valid\n");
    }
    if (ev.rs_sent) {
        // One bounded line per RS — the live gate's "the train is done"
        // stage marker ('net: v6 rs=3').
        m.console.puts("net: v6 rs=");
        m.console.print_u64(virtio_net.ndp.rs_sent);
        m.console.puts("\n");
    }
    if (virtio_net.ndp.last_conflict) |c| {
        const n = virtio_net.ipv6.format_ip6(c, &buf);
        m.console.puts("net: v6 dad-conflict ");
        m.console.puts(buf[0..n]);
        m.console.puts("\n");
        virtio_net.ndp.last_conflict = null;
    }
}

/// `net dhcp` — card N8 (claim 0351) + card N9 (claim 9489): drive the
/// bounded RFC 2131 client ONE STEP per invocation. INIT (idle): build
/// + transmit the DISCOVER (a CSPRNG transaction id), print it;
/// SELECTING: wait for the OFFER (already processed by a drain if it
/// landed); REQUESTING: transmit the built REQUEST exactly once, then
/// wait for the ACK; BOUND (N9): check the lease timer each invocation
/// — expiry -> release the address (INIT), T2 -> REBINDING (broadcast
/// REQUEST), T1 -> RENEWING (unicast REQUEST to the server), else print
/// the lease (the ACK's drain processing already set `arp.own_ip` — THE
/// one copy). The RX drain processes OFFER/ACK/NAK replies on port 68 (the
/// udp dispatch), so the handshake advances between invocations; each
/// command drains first (the claim-6076 polled-drain contract). Bounded
/// retry: `max_attempts` DISCOVERs without an OFFER -> an honest refuse
/// (the `timeout` counter). Deterministic, monitor-driven, no interrupts.
/// Issue #119 (audit follow-up 3): the autonomous DHCP lease lifecycle —
/// called from the shell idle loop each iteration (AFTER the RX drain,
/// so a pending renewal ACK has restarted the lease clock first):
/// advances T1/T2/expiry WITHOUT a human typing `net dhcp`, printing the
/// SAME transition lines `net dhcp` prints (identical text — the N9
/// gates' regexes/counters/captures are the evidence). Silent on the
/// no-ARP renew path (the client stays BOUND per RFC 2131 §4.4.5;
/// typing `net dhcp` surfaces the diagnostic) and on TX failures (the
/// next `net dhcp` retries — no per-second spam). The re-DISCOVER after
/// expiry stays command-triggered.
pub fn net_dhcp_autonomous(m: *Monitor) void {
    if (!virtio_net.net_ready) return;
    const p = virtio_net.net_dhcp_poll();
    switch (p.step) {
        .none, .renew_no_arp => {},
        .expired => {
            m.console.puts("net dhcp: lease expired (elapsed=");
            m.console.print_u64(p.elapsed);
            m.console.puts(" >= lease=");
            m.console.print_u64(p.lease);
            m.console.puts(") — address released, re-DISCOVER with `net dhcp`\n");
        },
        .rebinding => {
            if (!p.tx_ok) {
                err_prefix(m);
                m.console.print_line("REBINDING TX failed (transport unready)");
                return;
            }
            m.console.puts("net dhcp: rebinding (T2, elapsed=");
            m.console.print_u64(p.elapsed);
            m.console.puts(") request sent (");
            m.console.print_u64(@intCast(p.out_len));
            m.console.puts(" bytes)\n");
        },
        .renewing => {
            if (!p.tx_ok) {
                err_prefix(m);
                m.console.print_line("RENEWING TX failed (transport unready)");
                return;
            }
            m.console.puts("net dhcp: renewing (T1, elapsed=");
            m.console.print_u64(p.elapsed);
            m.console.puts(") request sent to the server (");
            m.console.print_u64(@intCast(p.out_len));
            m.console.puts(" bytes)\n");
        },
    }
}

fn cmd_net_dhcp(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 0) {
        print_usage(m, lookup("net").?);
        return .usage;
    }
    if (!virtio_net.net_ready) {
        err_prefix(m);
        m.console.puts("no virtio-net device (");
        m.console.puts(if (virtio_net.net_fail.len > 0) virtio_net.net_fail else "DID 0x1041 not found on bus 0");
        m.console.puts(")\n");
        return .none;
    }
    // Card N9 (claim 9489): stamp the lease clock BEFORE the drain — a
    // pending renewal ACK processed below restarts the lease from THIS
    // instant (the shell idle loop keeps it current between commands).
    virtio_net.dhcp.now_ticks = timer.ticks;
    // Deliver any pending OFFER/ACK before deciding (polled-drain
    // contract — the frame may have landed while the shell idled).
    virtio_net.net_rx_drain();
    switch (virtio_net.dhcp.state) {
        .idle => {
            if (virtio_net.dhcp.attempts >= virtio_net.dhcp.max_attempts) {
                virtio_net.dhcp.timed_out += 1;
                err_prefix(m);
                m.console.puts("refused (no OFFER after ");
                m.console.print_u64(@intCast(virtio_net.dhcp.max_attempts));
                m.console.puts(" DISCOVER attempts)\n");
                return .none;
            }
            // A fresh transaction id from the seeded CSPRNG (claim 2665).
            const xid: u32 = @truncate(csprng.random_u64());
            virtio_net.dhcp.start(&virtio_net.net_mac, xid);
            var out_len: usize = 0;
            switch (virtio_net.net_dhcp_send(virtio_net.dhcp.msg[0..virtio_net.dhcp.msg_len], &out_len)) {
                .ok => {
                    virtio_net.dhcp.discover_sent += 1;
                    virtio_net.dhcp.attempts += 1;
                    m.console.puts("net dhcp: discover sent xid=");
                    m.console.print_hex_min(xid);
                    m.console.puts(" (");
                    m.console.print_u64(@intCast(out_len));
                    m.console.puts(" bytes)\n");
                },
                else => {
                    virtio_net.dhcp.state = .idle; // the DISCOVER never went out
                    err_prefix(m);
                    m.console.print_line("DISCOVER TX failed (transport unready)");
                },
            }
            return .none;
        },
        .selecting => {
            m.console.puts("net dhcp: waiting for OFFER (xid=");
            m.console.print_hex_min(virtio_net.dhcp.xid);
            m.console.puts(")\n");
            return .none;
        },
        .requesting => {
            if (!virtio_net.dhcp.request_transmitted) {
                // The OFFER was accepted (the drain built the REQUEST into
                // dhcp.msg) — transmit it exactly once.
                var out_len: usize = 0;
                switch (virtio_net.net_dhcp_send(virtio_net.dhcp.msg[0..virtio_net.dhcp.msg_len], &out_len)) {
                    .ok => {
                        virtio_net.dhcp.request_transmitted = true;
                        virtio_net.dhcp.request_sent += 1;
                        m.console.puts("net dhcp: request sent xid=");
                        m.console.print_hex_min(virtio_net.dhcp.xid);
                        m.console.puts(" (");
                        m.console.print_u64(@intCast(out_len));
                        m.console.puts(" bytes)\n");
                    },
                    else => {
                        err_prefix(m);
                        m.console.print_line("REQUEST TX failed (transport unready)");
                    },
                }
            } else {
                m.console.puts("net dhcp: waiting for ACK (xid=");
                m.console.print_hex_min(virtio_net.dhcp.xid);
                m.console.puts(")\n");
            }
            return .none;
        },
        .renewing, .rebinding => {
            // Card N9 (claim 9489): the renewal REQUEST went out (or the
            // initial transmit failed — retry once). A pending renewal
            // never outlives its lease: if the ACK has not landed and the
            // lease expired meanwhile, release the address honestly.
            const lease = virtio_net.dhcp.lease_time;
            if (lease > 0 and virtio_net.dhcp.elapsed() >= lease) {
                virtio_net.dhcp.expire();
                m.console.puts("net dhcp: lease expired while waiting for the renewal ACK (elapsed=");
                m.console.print_u64(virtio_net.dhcp.elapsed());
                m.console.puts(" >= lease=");
                m.console.print_u64(lease);
                m.console.puts(") — address released\n");
                return .none;
            }
            if (!virtio_net.dhcp.request_transmitted) {
                var out_len: usize = 0;
                const r = if (virtio_net.dhcp.state == .renewing) blk: {
                    const srv_mac = virtio_net.arp.lookup(virtio_net.dhcp.lease_server) orelse {
                        err_prefix(m);
                        m.console.print_line("renewing needs the server MAC (net arp <server> first)");
                        break :blk .no_peer;
                    };
                    break :blk virtio_net.net_dhcp_send_unicast(virtio_net.dhcp.lease_server, srv_mac, virtio_net.dhcp.msg[0..virtio_net.dhcp.msg_len], &out_len);
                } else virtio_net.net_dhcp_send_bound(virtio_net.dhcp.msg[0..virtio_net.dhcp.msg_len], &out_len);
                if (r == .ok) {
                    virtio_net.dhcp.request_transmitted = true;
                    if (virtio_net.dhcp.state == .renewing) virtio_net.dhcp.renew_sent += 1 else virtio_net.dhcp.rebind_sent += 1;
                    m.console.puts("net dhcp: ");
                    m.console.puts(if (virtio_net.dhcp.state == .renewing) "renewing" else "rebinding");
                    m.console.puts(" request sent (");
                    m.console.print_u64(@intCast(out_len));
                    m.console.puts(" bytes)\n");
                } else {
                    m.console.puts("net dhcp: ");
                    m.console.puts(if (virtio_net.dhcp.state == .renewing) "RENEWING" else "REBINDING");
                    err_prefix(m);
                    m.console.print_line(" TX failed (transport unready)");
                }
            } else {
                m.console.puts("net dhcp: waiting for the renewal ACK (xid=");
                m.console.print_hex_min(virtio_net.dhcp.xid);
                m.console.puts(")\n");
            }
            return .none;
        },
        .bound => {
            // Card N9 (claim 9489): the lease lifecycle (RFC 2131
            // §4.4.5). Each invocation checks the elapsed lease time and
            // advances ONE step: expiry -> release the address (INIT);
            // T2 -> REBINDING (broadcast REQUEST); T1 -> RENEWING
            // (unicast REQUEST to the server — its MAC must be resolved;
            // the seam resolves nothing). The transition + transmit
            // happen in the SAME invocation (the polled-drain contract).
            const lease = virtio_net.dhcp.lease_time;
            const el = virtio_net.dhcp.elapsed();
            if (lease > 0 and el >= lease) {
                virtio_net.dhcp.expire();
                m.console.puts("net dhcp: lease expired (elapsed=");
                m.console.print_u64(el);
                m.console.puts(" >= lease=");
                m.console.print_u64(lease);
                m.console.puts(") — address released, re-DISCOVER with `net dhcp`\n");
                return .none;
            }
            if (lease > 0 and el >= virtio_net.dhcp.t2(lease)) {
                // REBINDING: the broadcast REQUEST (any server on the
                // link can answer — the lease is almost over).
                virtio_net.dhcp.enter_rebinding();
                var out_len: usize = 0;
                switch (virtio_net.net_dhcp_send_bound(virtio_net.dhcp.msg[0..virtio_net.dhcp.msg_len], &out_len)) {
                    .ok => {
                        virtio_net.dhcp.request_transmitted = true;
                        virtio_net.dhcp.rebind_sent += 1;
                        m.console.puts("net dhcp: rebinding (T2, elapsed=");
                        m.console.print_u64(el);
                        m.console.puts(") request sent (");
                        m.console.print_u64(@intCast(out_len));
                        m.console.puts(" bytes)\n");
                    },
                    else => {
                        err_prefix(m);
                        m.console.print_line("REBINDING TX failed (transport unready)");
                    },
                }
                return .none;
            }
            if (lease > 0 and el >= virtio_net.dhcp.t1(lease)) {
                // RENEWING: the unicast REQUEST to the server (RFC 2131
                // §4.4.5). The server MAC must be in the ARP table — the
                // guest resolves it with `net arp <server>` (the host's
                // --net-arp-respond answers); without it the client
                // honestly stays BOUND until T2 (RFC-compliant
                // degradation, never faked).
                const srv_mac = virtio_net.arp.lookup(virtio_net.dhcp.lease_server) orelse {
                    err_prefix(m);
                    m.console.puts("renewing needs the server MAC (net arp ");
                    var srvbuf: [15]u8 = undefined;
                    const sn = virtio_net.arp.format_ip(virtio_net.dhcp.lease_server, &srvbuf);
                    m.console.puts(srvbuf[0..sn]);
                    m.console.puts(" first) — the client stays BOUND until T2\n");
                    return .none;
                };
                virtio_net.dhcp.enter_renewing();
                var out_len: usize = 0;
                switch (virtio_net.net_dhcp_send_unicast(virtio_net.dhcp.lease_server, srv_mac, virtio_net.dhcp.msg[0..virtio_net.dhcp.msg_len], &out_len)) {
                    .ok => {
                        virtio_net.dhcp.request_transmitted = true;
                        virtio_net.dhcp.renew_sent += 1;
                        m.console.puts("net dhcp: renewing (T1, elapsed=");
                        m.console.print_u64(el);
                        m.console.puts(") request sent to the server (");
                        m.console.print_u64(@intCast(out_len));
                        m.console.puts(" bytes)\n");
                    },
                    else => {
                        err_prefix(m);
                        m.console.print_line("RENEWING TX failed (transport unready)");
                    },
                }
                return .none;
            }
            // The lease is still young — the existing bound print
            // (unchanged; the N8 gate asserts it byte-exact).
            m.console.puts("net: dhcp bound ip=");
            var ibuf: [15]u8 = undefined;
            const in = virtio_net.arp.format_ip(virtio_net.dhcp.lease_ip, &ibuf);
            m.console.puts(ibuf[0..in]);
            m.console.puts(" mask=");
            const mn = virtio_net.arp.format_ip(virtio_net.dhcp.lease_mask, &ibuf);
            m.console.puts(ibuf[0..mn]);
            m.console.puts(" gw=");
            const gn = virtio_net.arp.format_ip(virtio_net.dhcp.lease_gw, &ibuf);
            m.console.puts(ibuf[0..gn]);
            m.console.puts(" server=");
            const sn = virtio_net.arp.format_ip(virtio_net.dhcp.lease_server, &ibuf);
            m.console.puts(ibuf[0..sn]);
            m.console.puts(" lease=");
            m.console.print_u64(virtio_net.dhcp.lease_time);
            m.console.puts("\n");
            return .none;
        },
    }
}

fn cmd_net_dns(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len < 1 or args.len > 2) {
        print_usage(m, lookup("net").?);
        return .usage;
    }
    const hostname = args[0];
    var server_ip: [4]u8 = .{ 10, 0, 0, 2 }; // default host gateway
    if (args.len == 2) {
        server_ip = virtio_net.arp.parse_ip(args[1]) orelse {
            err_prefix(m);
            m.console.puts("invalid server address: ");
            m.console.puts(args[1]);
            m.console.puts("\n");
            return .invalid_argument;
        };
    } else {
        if (virtio_net.dhcp.state == .bound and (virtio_net.dhcp.lease_server[0] != 0 or virtio_net.dhcp.lease_server[1] != 0 or virtio_net.dhcp.lease_server[2] != 0 or virtio_net.dhcp.lease_server[3] != 0)) {
            server_ip = virtio_net.dhcp.lease_server;
        }
    }

    if (!virtio_net.net_ready) {
        err_prefix(m);
        m.console.print_line("no virtio-net device");
        return .none;
    }
    if (!virtio_net.arp.ip_set()) {
        err_prefix(m);
        m.console.print_line("no IP set (net ip <a.b.c.d> first)");
        return .none;
    }

    if (dns.resolve(hostname, server_ip)) |ip| {
        m.console.puts("net dns: ");
        m.console.puts(hostname);
        m.console.puts(" -> ");
        var ipbuf: [15]u8 = undefined;
        const in = virtio_net.arp.format_ip(ip, &ipbuf);
        m.console.puts(ipbuf[0..in]);
        m.console.puts("\n");
    } else |err| {
        err_prefix(m);
        m.console.puts("net dns error: ");
        m.console.puts(@errorName(err));
        m.console.puts(" for '");
        m.console.puts(hostname);
        m.console.puts("'\n");
    }
    return .none;
}

/// M26 N8 (issue #435): `net status` prints a concise one-line summary:
/// IP, Gateway, DNS server, DHCP state, and link connectivity.
fn cmd_net_status(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    var ipbuf: [15]u8 = undefined;
    var gwbuf: [15]u8 = undefined;
    var dnsbuf: [15]u8 = undefined;

    const ip_s: []const u8 = if (virtio_net.arp.ip_set())
        ipbuf[0..virtio_net.arp.format_ip(virtio_net.arp.own_ip, &ipbuf)]
    else
        "0.0.0.0";

    const gw: [4]u8 = if (virtio_net.dhcp.state == .bound and (virtio_net.dhcp.lease_gw[0] != 0 or virtio_net.dhcp.lease_gw[1] != 0 or virtio_net.dhcp.lease_gw[2] != 0 or virtio_net.dhcp.lease_gw[3] != 0))
        virtio_net.dhcp.lease_gw
    else if (virtio_net.arp.ip_set())
        .{ 10, 0, 0, 2 }
    else
        .{ 0, 0, 0, 0 };
    const gw_s: []const u8 = gwbuf[0..virtio_net.arp.format_ip(gw, &gwbuf)];

    const dns_srv: [4]u8 = if (virtio_net.dhcp.state == .bound and (virtio_net.dhcp.lease_server[0] != 0 or virtio_net.dhcp.lease_server[1] != 0 or virtio_net.dhcp.lease_server[2] != 0 or virtio_net.dhcp.lease_server[3] != 0))
        virtio_net.dhcp.lease_server
    else if (virtio_net.arp.ip_set())
        .{ 10, 0, 0, 2 }
    else
        .{ 0, 0, 0, 0 };
    const dns_s: []const u8 = dnsbuf[0..virtio_net.arp.format_ip(dns_srv, &dnsbuf)];

    const dhcp_str = switch (virtio_net.dhcp.state) {
        .idle => "idle",
        .selecting => "selecting",
        .requesting => "requesting",
        .bound => "bound",
        .renewing => "renewing",
        .rebinding => "rebinding",
    };

    const link_status = if (!virtio_net.net_ready)
        "disconnected"
    else if (virtio_net.arp.ip_set())
        "connected"
    else
        "idle";

    m.console.puts("net status: IP: ");
    m.console.puts(ip_s);
    m.console.puts(" Gateway: ");
    m.console.puts(gw_s);
    m.console.puts(" DNS: ");
    m.console.puts(dns_s);
    m.console.puts(" DHCP: ");
    m.console.puts(dhcp_str);
    m.console.puts(" (");
    m.console.puts(link_status);
    m.console.puts(")\n");
    return .none;
}

/// M26 N16 (issue #443): `net route` displays the active IPv4 routing table.
fn cmd_net_route(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    var gwbuf: [15]u8 = undefined;
    const gw: [4]u8 = if (virtio_net.dhcp.state == .bound and (virtio_net.dhcp.lease_gw[0] != 0 or virtio_net.dhcp.lease_gw[1] != 0 or virtio_net.dhcp.lease_gw[2] != 0 or virtio_net.dhcp.lease_gw[3] != 0))
        virtio_net.dhcp.lease_gw
    else if (virtio_net.arp.ip_set())
        .{ 10, 0, 0, 2 }
    else
        .{ 0, 0, 0, 0 };
    const gw_s: []const u8 = gwbuf[0..virtio_net.arp.format_ip(gw, &gwbuf)];

    m.console.puts("net route: table\n");
    m.console.puts("Destination     Gateway         Interface       Metric\n");
    m.console.puts("0.0.0.0/0       ");
    m.console.puts(gw_s);
    var pad: usize = gw_s.len;
    while (pad < 16) : (pad += 1) m.console.putc(' ');
    m.console.puts("eth0            1\n");
    return .none;
}

/// M26 N15 (issue #442): `net log` displays the in-memory ring buffer of network events.
fn cmd_net_log(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    const count = virtio_net.net_log.get_count();
    m.console.puts("net log: entries=");
    m.console.print_u64(@intCast(count));
    m.console.puts("\n");

    var i: usize = 0;
    while (i < count) : (i += 1) {
        if (virtio_net.net_log.get_entry(i)) |entry| {
            m.console.puts("  [");
            m.console.print_u64(@intCast(i));
            m.console.puts("] ");
            m.console.puts(entry.getText());
            m.console.puts("\n");
        }
    }
    return .none;
}

/// `net recv` — the card-N2 receive path: drain the RX used ring (polled
/// — the frame may have landed while the shell idled), then print every
/// frame currently in the bounded FIFO byte-exact (hex, the
/// netsend-style report) and consume it. The frames are the RAW
/// device-written buffer bytes (the 12-byte virtio_net_hdr headroom
/// included — the claim-time header question is pinned by `rx-obs`, not
/// stripped blindly). Honest when empty: a filtered frame shows as `net
/// recv: no frames` + the `net` report's filtered= counter.
fn cmd_net_recv(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    if (!virtio_net.net_ready) {
        m.console.puts("net recv: no virtio-net device (");
        m.console.puts(if (virtio_net.net_fail.len > 0) virtio_net.net_fail else "DID 0x1041 not found on bus 0");
        m.console.puts(")\n");
        return .none;
    }
    virtio_net.net_rx_drain();
    const count = virtio_net.fifo_occupancy();
    m.console.puts("net recv: frames=");
    m.console.print_u64(@intCast(count));
    m.console.puts("\n");
    if (count == 0) {
        m.console.puts("net recv: no frames (filtered or nothing injected)\n");
        return .none;
    }
    var idx: usize = 0;
    while (virtio_net.fifo_peek()) |frame| {
        m.console.puts("net recv: [");
        m.console.print_u64(@intCast(idx));
        m.console.puts("] len=");
        m.console.print_u64(@intCast(frame.len));
        m.console.puts("\n");
        m.console.puts("net recv: ");
        var bi: usize = 0;
        while (bi < frame.len) : (bi += 1) {
            if (bi > 0) m.console.puts(" ");
            const b = frame[bi];
            const hex = "0123456789abcdef";
            var two: [2]u8 = .{ hex[b >> 4], hex[b & 0xf] };
            m.console.puts(&two);
        }
        m.console.puts("\n");
        virtio_net.fifo_pop_advance();
        idx += 1;
    }
    return .none;
}

/// `net ip <a.b.c.d>` — card N3 (claim 7293): set our static IPv4
/// address (fixed BSS, no heap — DHCP is a later card). The echo line
/// `net ip: ip=<a.b.c.d>` is the live gate's injection trigger (the
/// runner's `--net-inject-after` marker — deterministic, not a sleep).
fn cmd_net_ip(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 1) {
        print_usage(m, lookup("net").?);
        return .usage;
    }
    const ip = virtio_net.arp.parse_ip(args[0]) orelse {
        err_prefix(m);
        m.console.puts("invalid address: ");
        m.console.puts(args[0]);
        m.console.puts("\n");
        return .invalid_argument;
    };
    virtio_net.arp.own_ip = ip;
    virtio_net.net_log.log_fmt("IP: assigned {d}.{d}.{d}.{d}", .{ ip[0], ip[1], ip[2], ip[3] });
    m.console.puts("net ip: ip=");
    var ipbuf: [15]u8 = undefined;
    const n = virtio_net.arp.format_ip(ip, &ipbuf);
    m.console.puts(ipbuf[0..n]);
    m.console.puts("\n");
    return .none;
}

/// `net arp [<a.b.c.d>]` — card N3 (claim 7293). With no argument:
/// drain the RX ring (idempotent — a reply may have landed while the
/// shell idled) and print the bounded table + the counters. With an
/// argument: resolve — a table hit prints the peer, a miss transmits an
/// ARP request (the reply is learned asynchronously by the RX drain) and
/// reports it; refused honestly when no static IP is set or the
/// transport is unready.
fn cmd_net_arp(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) {
        if (!virtio_net.net_ready) {
            m.console.print_line("net arp: no virtio-net device");
            return .none;
        }
        virtio_net.net_rx_drain();
        var count: usize = 0;
        for (&virtio_net.arp.table) |*e| {
            if (!e.valid) continue;
            count += 1;
        }
        m.console.puts("net arp: entries=");
        m.console.print_u64(@intCast(count));
        m.console.puts("\n");
        var ipbuf: [15]u8 = undefined;
        var macbuf: [17]u8 = undefined;
        for (&virtio_net.arp.table) |*e| {
            if (!e.valid) continue;
            const in = virtio_net.arp.format_ip(e.ip, &ipbuf);
            m.console.puts("net arp: ");
            m.console.puts(ipbuf[0..in]);
            m.console.puts(" -> ");
            virtio_net.format_mac(&e.mac, &macbuf);
            m.console.puts(&macbuf);
            m.console.puts("\n");
        }
        m.console.puts("net arp: req=");
        m.console.print_u64(virtio_net.arp.requests_sent);
        m.console.puts(",repl=");
        m.console.print_u64(virtio_net.arp.replies_sent);
        m.console.puts(",learn=");
        m.console.print_u64(virtio_net.arp.replies_learned);
        m.console.puts(",drop=");
        m.console.print_u64(virtio_net.arp.dropped);
        m.console.puts(",fail=");
        m.console.print_u64(virtio_net.arp.reply_tx_fail);
        m.console.puts("\n");
        return .none;
    }
    if (args.len != 1) {
        print_usage(m, lookup("net").?);
        return .usage;
    }
    const ip = virtio_net.arp.parse_ip(args[0]) orelse {
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
    virtio_net.net_rx_drain();
    if (virtio_net.arp.lookup(ip)) |mac| {
        m.console.puts("net arp: ");
        var ipbuf: [15]u8 = undefined;
        const in = virtio_net.arp.format_ip(ip, &ipbuf);
        m.console.puts(ipbuf[0..in]);
        m.console.puts(" is at ");
        var macbuf: [17]u8 = undefined;
        virtio_net.format_mac(&mac, &macbuf);
        m.console.puts(&macbuf);
        m.console.puts("\n");
        return .none;
    }
    var frame_len: usize = 0;
    switch (virtio_net.net_arp_request(ip, &frame_len)) {
        .ok => {
            virtio_net.arp.requests_sent += 1;
            virtio_net.net_log.log_fmt("ARP: request {d}.{d}.{d}.{d}", .{ ip[0], ip[1], ip[2], ip[3] });
            m.console.puts("net arp: request for ");
            var ipbuf: [15]u8 = undefined;
            const in = virtio_net.arp.format_ip(ip, &ipbuf);
            m.console.puts(ipbuf[0..in]);
            m.console.puts(" sent (");
            m.console.print_u64(@intCast(frame_len));
            m.console.puts(" bytes)\n");
        },
        .not_ready => {
            err_prefix(m);
            m.console.print_line("no IP set (net ip <a.b.c.d> first) or transport unready");
        },
        .timeout => {
            err_prefix(m);
            m.console.print_line("request TX timeout (device did not complete within the poll budget)");
        },
        .no_peer => unreachable,
    }
    return .none;
}

/// `net ping <a.b.c.d>` — card N4 (claim 0148): transmit an ICMP echo
/// request to a peer already in the ARP table (the table is drained
/// first — a reply may have landed while the shell idled). The peer's
/// MAC must be known (`net arp <ip>` resolves it first — an echo needs a
/// unicast dst; refused honestly otherwise). The reply is observed
/// asynchronously by the RX drain (`pongs_observed` + the `net` report's
/// `seq=`).
fn cmd_net_ping(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 1) {
        print_usage(m, lookup("net").?);
        return .usage;
    }
    const ip = virtio_net.arp.parse_ip(args[0]) orelse {
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
    virtio_net.net_rx_drain();
    var frame_len: usize = 0;
    switch (virtio_net.net_ping_request(ip, &frame_len)) {
        .ok => {
            virtio_net.ipv4.requests_sent += 1;
            virtio_net.ipv4.ping_seq +%= 1;
            m.console.puts("net ping: echo request to ");
            var ipbuf: [15]u8 = undefined;
            const in = virtio_net.arp.format_ip(ip, &ipbuf);
            m.console.puts(ipbuf[0..in]);
            m.console.puts(" sent (");
            m.console.print_u64(@intCast(frame_len));
            m.console.puts(" bytes)\n");
        },
        .not_ready => {
            err_prefix(m);
            m.console.print_line("no IP set (net ip <a.b.c.d> first) or transport unready");
        },
        .timeout => {
            err_prefix(m);
            m.console.print_line("echo request TX timeout (device did not complete within the poll budget)");
        },
        .no_peer => {
            err_prefix(m);
            m.console.print_line("peer not in ARP table (net arp <a.b.c.d> first)");
        },
    }
    return .none;
}

/// `net udp [...]` — card N5 (claim 8552). No argument: the bounded
/// listen table + the counters. `listen <port>` / `close <port>`: add /
/// remove a listener (a full or duplicate table is refused honestly).
/// `send <ip> <port> <len>`: transmit ONE UDP datagram to the peer (peer
/// MAC from the ARP table; `.no_peer` refused — `net arp <ip>` resolves
/// first) from the fixed source port 7000, the byte-index payload (≤ 64,
/// honest truncation); a send to OUR OWN IP takes the LOOPBACK path (no
/// device round trip). `recv [<port>]`: drain the RX ring (a datagram
/// may have landed while the shell idled) and print + consume the
/// datagram(s) in the listener's bounded buffer byte-exact (hex, the
/// net-recv style).
fn cmd_net_udp(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) {
        var count: usize = 0;
        for (&virtio_net.udp.listen) |e| {
            if (e.valid) count += 1;
        }
        m.console.puts("net udp: entries=");
        m.console.print_u64(@intCast(count));
        m.console.puts("\n");
        for (&virtio_net.udp.listen) |e| {
            if (!e.valid) continue;
            m.console.puts("net udp: port=");
            m.console.print_u64(e.port);
            m.console.puts("\n");
        }
        m.console.puts("net udp: rx=");
        m.console.print_u64(virtio_net.udp.received);
        m.console.puts(",tx=");
        m.console.print_u64(virtio_net.udp.sent);
        m.console.puts(",loop=");
        m.console.print_u64(virtio_net.udp.loopbacked);
        m.console.puts(",drop=");
        m.console.print_u64(virtio_net.udp.dropped_badsum + virtio_net.udp.dropped_closed + virtio_net.udp.dropped_len + virtio_net.udp.dropped_bcast);
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
        if (!virtio_net.udp.listen_port(port)) {
            err_prefix(m);
            m.console.print_line("listen failed (table full or duplicate)");
            return .none;
        }
        m.console.puts("net udp: listening on ");
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
        if (!virtio_net.udp.close_port(port)) {
            err_prefix(m);
            m.console.puts("not listening on ");
            m.console.print_u64(port);
            m.console.puts("\n");
            return .none;
        }
        m.console.puts("net udp: closed ");
        m.console.print_u64(port);
        m.console.puts("\n");
        return .none;
    }
    if (std.mem.eql(u8, args[0], "send")) {
        if (args.len != 4) {
            print_usage(m, lookup("net").?);
            return .usage;
        }
        const ip = virtio_net.arp.parse_ip(args[1]) orelse {
            err_prefix(m);
            m.console.puts("invalid address: ");
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
        // The deterministic payload — bytes 01 02 03 04… (byte i + 1,
        // bounded ≤ 64) — the live gate's byte-exact fixtures pin it.
        var payload: [virtio_net.udp.payload_max]u8 = undefined;
        var pi: usize = 0;
        while (pi < len) : (pi += 1) payload[pi] = @as(u8, @truncate(pi)) +% 1;
        var frame_len: usize = 0;
        switch (virtio_net.net_udp_send(ip, port, payload[0..@intCast(len)], &frame_len)) {
            .ok => {
                m.console.puts("net udp: sent ");
                m.console.print_u64(len);
                m.console.puts(" bytes to ");
                var ipbuf: [15]u8 = undefined;
                const in = virtio_net.arp.format_ip(ip, &ipbuf);
                m.console.puts(ipbuf[0..in]);
                m.console.puts(":");
                m.console.print_u64(port);
                m.console.puts(" (");
                m.console.print_u64(@intCast(frame_len));
                m.console.puts(" bytes)\n");
            },
            .not_ready => {
                err_prefix(m);
                m.console.print_line("no IP set (net ip <a.b.c.d> first) or transport unready");
            },
            .timeout => {
                err_prefix(m);
                m.console.print_line("datagram TX timeout (device did not complete within the poll budget)");
            },
            .no_peer => {
                err_prefix(m);
                m.console.print_line("peer not in ARP table (net arp <a.b.c.d> first)");
            },
        }
        return .none;
    }
    if (std.mem.eql(u8, args[0], "recv")) {
        if (args.len > 2) {
            print_usage(m, lookup("net").?);
            return .usage;
        }
        // A datagram may have landed while the shell idled — drain first.
        virtio_net.net_rx_drain();
        if (args.len == 2) {
            const port = parse_port(args[1]) orelse {
                m.console.puts("net udp: invalid port: ");
                m.console.puts(args[1]);
                m.console.puts("\n");
                return .invalid_argument;
            };
            _ = cmd_net_udp_recv_port(m, port);
            return .none;
        }
        var total: usize = 0;
        for (&virtio_net.udp.listen) |e| {
            if (!e.valid) continue;
            total += cmd_net_udp_recv_port(m, e.port);
        }
        m.console.puts("net udp recv: total=");
        m.console.print_u64(@intCast(total));
        m.console.puts("\n");
        return .none;
    }
    print_usage(m, lookup("net").?);
    return .usage;
}

/// Drain + print the datagrams buffered for ONE listener, byte-exact
/// (hex, the net-recv style). Returns how many were printed.
fn cmd_net_udp_recv_port(m: *Monitor, port: u16) usize {
    var count: usize = 0;
    while (virtio_net.udp.pop(port)) |d| {
        if (count == 0) {
            m.console.puts("net udp recv: port=");
            m.console.print_u64(port);
            m.console.puts("\n");
        }
        m.console.puts("net udp recv: [");
        m.console.print_u64(@intCast(count));
        m.console.puts("] len=");
        m.console.print_u64(@intCast(d.len));
        m.console.puts("\n");
        m.console.puts("net udp recv: ");
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
        m.console.puts("net udp recv: no datagrams for port ");
        m.console.print_u64(port);
        m.console.puts("\n");
    }
    return count;
}

/// Parse a decimal (or 0x-prefixed hex) port in 0..65535, the `net udp`
/// subcommands' bound.
pub fn parse_port(text: []const u8) ?u16 {
    const v = parseInt(text) catch return null;
    if (v > 0xffff) return null;
    return @intCast(v);
}

/// `netsend <bytes>` — build the N1 known frame in the fixed staging
/// buffer (broadcast dst, own MAC src, ethertype 0x0800, a deterministic
/// byte-index payload), submit it on the TX queue, drain the used ring
/// (polled), and report exact byte counts. Over-limit requests truncate
/// honestly at the 1500-byte payload bound (the reply names the drop);
/// an absent/unarmed transport is refused honestly. This is the live
/// gate's byte-exact TX proof: the host's file-handle attachment captures
/// exactly the frame bytes reported here.
pub fn cmd_netsend(m: *Monitor, args: []const []const u8) ExecError {
    const requested = parseInt(args[0]) catch {
        err_prefix(m);
        m.console.puts("invalid byte count: ");
        m.console.puts(args[0]);
        m.console.puts("\n");
        return .invalid_argument;
    };
    if (!virtio_net.net_ready) {
        err_prefix(m);
        m.console.print_line("transport not ready (no virtio-net device)");
        return .none;
    }
    m.console.puts("netsend: n=");
    m.console.print_u64(requested);
    if (requested > virtio_net.payload_max) {
        m.console.puts(" truncated to ");
        m.console.print_u64(virtio_net.payload_max);
        m.console.puts(" (payload bound)");
    }
    m.console.puts("\n");
    var frame_len: usize = 0;
    switch (virtio_net.net_send_frame(requested, &frame_len)) {
        .ok => {
            m.console.puts("netsend: tx ok frames=");
            m.console.print_u64(virtio_net.net_dev.tx_frames);
            m.console.puts(" bytes=");
            m.console.print_u64(@intCast(frame_len));
            m.console.puts("\n");
            m.console.puts("netsend: sent ");
            m.console.print_u64(@intCast(frame_len));
            m.console.puts(" bytes\n");
        },
        .not_ready => {
            err_prefix(m);
            m.console.print_line("transport not ready (no virtio-net device)");
        },
        .timeout => {
            err_prefix(m);
            m.console.print_line("tx timeout (device did not complete within the poll budget)");
        },
        .no_peer => unreachable,
    }
    return .none;
}
