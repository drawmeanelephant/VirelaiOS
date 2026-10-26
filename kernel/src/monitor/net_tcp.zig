//! Monitor commands: the `net tcp` bounded-client verbs (connect/send/recv/close/reset).
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
const net = @import("net.zig");
const parse_port = net.parse_port;
/// `net tcp` — card N10 (claim 7026): drive the bounded RFC 793 CLIENT
/// ONE STEP per invocation. `connect <addr> <port>` starts the
/// three-way handshake (build + transmit the SYN — the peer's MAC must
/// be in the ARP table: `net arp <addr>` resolves it first; an own-IP
/// connect is REFUSED — the bounded client is outward-only, no TCP
/// loopback); the bare `net tcp` drives: SYN_SENT — the bounded connect
/// timeout (30 s of guest ticks, the card-N9 timer pattern) refuses
/// honestly (`timed_out`), ESTABLISHED — transmit the pending ACK the
/// drain built (the polled-drain contract) then print the established
/// state, FIN_SENT — wait for the FIN-ACK, CLOSED — transmit the final
/// ACK after the FIN-ACK (or return to IDLE after a reset); `send <len>`
/// transmits a data segment (the deterministic payload 01 02 03…, ack =
/// rcv_nxt); `recv` prints the bounded RX buffer (ONE segment); `close`
/// transmits the FIN (ESTABLISHED -> FIN_SENT); `reset` transmits a RST
/// (the client's abort). The RX drain processes the peer's segments on
/// our port 8000 (the ipv4 protocol-6 dispatch -> tcp), so the
/// connection advances between invocations; each command drains first
/// (the claim-6076 polled-drain contract). Deterministic,
/// monitor-driven, no interrupts.
pub fn cmd_net_tcp(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) return cmd_net_tcp_drive(m);
    if (std.mem.eql(u8, args[0], "connect")) return cmd_net_tcp_connect(m, args[1..]);
    if (std.mem.eql(u8, args[0], "send")) return cmd_net_tcp_send(m, args[1..]);
    if (std.mem.eql(u8, args[0], "recv")) return cmd_net_tcp_recv(m, args[1..]);
    if (std.mem.eql(u8, args[0], "close")) return cmd_net_tcp_close(m, args[1..]);
    if (std.mem.eql(u8, args[0], "reset")) return cmd_net_tcp_reset(m, args[1..]);
    print_usage(m, lookup("net").?);
    return .usage;
}

/// Print the TCP connection's peer as `a.b.c.d:port`.
fn print_tcp_peer(m: *Monitor) void {
    var tibuf: [15]u8 = undefined;
    const tin = virtio_net.arp.format_ip(virtio_net.tcp.peer_ip, &tibuf);
    m.console.puts(tibuf[0..tin]);
    m.console.puts(":");
    m.console.print_u64(virtio_net.tcp.peer_port);
}

/// The bare `net tcp` drive (one step per invocation — see cmd_net_tcp).
fn cmd_net_tcp_drive(m: *Monitor) ExecError {
    if (!virtio_net.net_ready) {
        m.console.print_line("net tcp: no virtio-net device");
        return .none;
    }
    // Card N10: stamp the connect clock BEFORE the drain — a pending
    // SYN-ACK processed below starts the connection from THIS instant
    // (the shell idle loop keeps it current between commands).
    virtio_net.tcp.now_ticks = timer.ticks;
    // Deliver any pending segment before deciding (polled-drain
    // contract — the frame may have landed while the shell idled).
    virtio_net.net_rx_drain();
    switch (virtio_net.tcp.state) {
        .idle => {
            m.console.print_line("net tcp: no connection (net tcp connect <addr> <port>)");
            return .none;
        },
        .listen => {
            m.console.puts("net tcp: listening on port ");
            m.console.print_u64(virtio_net.tcp.listen_port);
            m.console.puts("\n");
            return .none;
        },
        .syn_received => {
            if (virtio_net.tcp.ack_pending) {
                var out_len: usize = 0;
                if (virtio_net.net_tcp_send(virtio_net.tcp.msg[0..virtio_net.tcp.msg_len], &out_len) == .ok) {
                    virtio_net.tcp.ack_pending = false;
                    virtio_net.tcp.ack_sent += 1;
                }
            }
            m.console.puts("net tcp: syn_received (peer=");
            print_tcp_peer(m);
            m.console.puts(")\n");
            return .none;
        },
        .syn_sent => {
            if (virtio_net.tcp.connect_timed_out()) {
                virtio_net.tcp.abort_timeout();
                err_prefix(m);
                m.console.puts("connect refused (no SYN-ACK after ");
                m.console.print_u64(@intCast(virtio_net.tcp.connect_timeout));
                m.console.puts("s) — run 'net tcp connect <addr> <port>' to retry\n");
                return .none;
            }
            m.console.puts("net tcp: waiting for SYN-ACK (peer=");
            print_tcp_peer(m);
            m.console.puts(")\n");
            return .none;
        },
        .established => {
            if (virtio_net.tcp.ack_pending) {
                // The drain built an ACK (the handshake, a data echo, or
                // a server FIN) — transmit it exactly once.
                var out_len: usize = 0;
                switch (virtio_net.net_tcp_send(virtio_net.tcp.msg[0..virtio_net.tcp.msg_len], &out_len)) {
                    .ok => {
                        virtio_net.tcp.ack_pending = false;
                        virtio_net.tcp.ack_sent += 1;
                        m.console.puts("net tcp: ack sent (ack=");
                        m.console.print_hex_min(virtio_net.tcp.rcv_nxt);
                        m.console.puts(", ");
                        m.console.print_u64(@intCast(out_len));
                        m.console.puts(" bytes)\n");
                    },
                    else => {
                        err_prefix(m);
                        m.console.print_line("ACK TX failed (transport unready)");
                    },
                }
            }
            m.console.puts("net tcp: established (peer=");
            print_tcp_peer(m);
            m.console.puts(")\n");
            return .none;
        },
        .fin_sent => {
            // The FIN-ACK, when it lands, is processed by the drain into
            // CLOSED with the final ACK built — the CLOSED branch sends
            // it. While still FIN_SENT, just wait.
            m.console.puts("net tcp: waiting for the FIN-ACK (peer=");
            print_tcp_peer(m);
            m.console.puts(")\n");
            return .none;
        },
        .closed => {
            if (virtio_net.tcp.ack_pending) {
                // The drain processed the FIN-ACK — transmit the final
                // ACK (the close completes).
                var out_len: usize = 0;
                switch (virtio_net.net_tcp_send(virtio_net.tcp.msg[0..virtio_net.tcp.msg_len], &out_len)) {
                    .ok => {
                        virtio_net.tcp.ack_pending = false;
                        virtio_net.tcp.ack_sent += 1;
                        m.console.puts("net tcp: final ack sent (ack=");
                        m.console.print_hex_min(virtio_net.tcp.rcv_nxt);
                        m.console.puts(", ");
                        m.console.print_u64(@intCast(out_len));
                        m.console.puts(" bytes)\n");
                    },
                    else => {
                        err_prefix(m);
                        m.console.print_line("final ACK TX failed (transport unready)");
                    },
                }
                virtio_net.tcp.state = .idle; // the close completed — a fresh connect is possible
                m.console.print_line("net tcp: connection closed");
                return .none;
            }
            // A clean close or a reset already handled — back to IDLE.
            m.console.print_line("net tcp: connection closed — idle again");
            virtio_net.tcp.state = .idle;
            return .none;
        },
    }
}

/// `net tcp connect <addr> <port>` — start the three-way handshake.
fn cmd_net_tcp_connect(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 2) {
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
    const port = parse_port(args[1]) orelse {
        err_prefix(m);
        m.console.puts("invalid port: ");
        m.console.puts(args[1]);
        m.console.puts("\n");
        return .invalid_argument;
    };
    if (!virtio_net.net_ready) {
        err_prefix(m);
        m.console.print_line("no virtio-net device");
        return .none;
    }
    if (virtio_net.tcp.state != .idle) {
        err_prefix(m);
        m.console.print_line("a connection is already in progress (net tcp reset to abort)");
        return .none;
    }
    if (!virtio_net.arp.ip_set()) {
        err_prefix(m);
        m.console.print_line("no IP set (net ip <a.b.c.d> first)");
        return .none;
    }
    if (std.mem.eql(u8, &ip, &virtio_net.arp.own_ip)) {
        err_prefix(m);
        m.console.print_line("own-IP connect refused (no TCP loopback — the bounded client is outward-only)");
        return .none;
    }
    // Drain first (the claim-6076 contract — the ARP reply for `net arp
    // <ip>` may have landed while the shell idled; the peer's MAC must
    // be in the table: the seam resolves nothing).
    virtio_net.net_rx_drain();
    const peer_mac = virtio_net.arp.lookup(ip) orelse {
        err_prefix(m);
        m.console.puts("peer not in ARP table (net arp ");
        var ipbuf: [15]u8 = undefined;
        const in = virtio_net.arp.format_ip(ip, &ipbuf);
        m.console.puts(ipbuf[0..in]);
        m.console.puts(" first)\n");
        return .none;
    };
    // A fresh ISN from the seeded CSPRNG (claim 2665) — real TCP
    // randomizes ISNs; the fixtures assert the seq/ack chain, not values.
    const isn: u32 = @truncate(csprng.random_u64());
    virtio_net.tcp.start(ip, port, isn, peer_mac);
    var out_len: usize = 0;
    switch (virtio_net.net_tcp_send(virtio_net.tcp.msg[0..virtio_net.tcp.msg_len], &out_len)) {
        .ok => {
            virtio_net.tcp.syn_sent += 1;
            virtio_net.tcp.advance_snd(1); // the SYN consumes one sequence number
            virtio_net.tcp.record_pending(); // card N11: the unacked SYN is retransmittable
            m.console.puts("net tcp: syn sent (peer=");
            print_tcp_peer(m);
            m.console.puts(", seq=");
            m.console.print_hex_min(isn);
            m.console.puts(", ");
            m.console.print_u64(@intCast(out_len));
            m.console.puts(" bytes)\n");
        },
        else => {
            virtio_net.tcp.state = .idle; // the SYN never went out
            err_prefix(m);
            m.console.print_line("SYN TX failed (transport unready)");
        },
    }
    return .none;
}

/// `net tcp send <len>` — transmit a data segment (1..payload_max bytes
/// of the deterministic pattern 01 02 03…, ack = rcv_nxt).
fn cmd_net_tcp_send(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 1) {
        print_usage(m, lookup("net").?);
        return .usage;
    }
    const len = parseInt(args[0]) catch {
        err_prefix(m);
        m.console.puts("invalid length: ");
        m.console.puts(args[0]);
        m.console.puts("\n");
        return .invalid_argument;
    };
    if (len < 1 or len > virtio_net.tcp.payload_max) {
        err_prefix(m);
        m.console.puts("length must be between 1 and ");
        m.console.print_u64(virtio_net.tcp.payload_max);
        m.console.puts("\n");
        return .invalid_argument;
    }
    if (virtio_net.tcp.state != .established) {
        err_prefix(m);
        m.console.print_line("not established (net tcp connect <addr> <port> first)");
        return .none;
    }
    // The deterministic payload — bytes 01 02 03 04… (byte i + 1,
    // bounded ≤ 64) — the live gate's byte-exact fixtures pin it.
    var payload: [virtio_net.tcp.payload_max]u8 = undefined;
    var pi: usize = 0;
    while (pi < len) : (pi += 1) payload[pi] = @as(u8, @truncate(pi)) +% 1;
    const seq = virtio_net.tcp.snd_una;
    virtio_net.tcp.build_data_msg(payload[0..@intCast(len)]);
    var out_len: usize = 0;
    switch (virtio_net.net_tcp_send(virtio_net.tcp.msg[0..virtio_net.tcp.msg_len], &out_len)) {
        .ok => {
            virtio_net.tcp.data_sent += 1;
            virtio_net.tcp.advance_snd(@intCast(len));
            virtio_net.tcp.record_pending(); // card N11: the unacked data is retransmittable
            m.console.puts("net tcp: data sent (seq=");
            m.console.print_hex_min(seq);
            m.console.puts(", ");
            m.console.print_u64(@intCast(len));
            m.console.puts(" bytes)\n");
        },
        else => {
            err_prefix(m);
            m.console.print_line("DATA TX failed (transport unready)");
        },
    }
    return .none;
}

/// `net tcp recv` — print the bounded RX buffer (ONE segment — the
/// honest bound, no reassembly) byte-exact (hex, the net udp recv
/// style) and consume it.
fn cmd_net_tcp_recv(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 0) {
        print_usage(m, lookup("net").?);
        return .usage;
    }
    if (virtio_net.tcp.state != .established) {
        err_prefix(m);
        m.console.print_line("not established (net tcp connect <addr> <port> first)");
        return .none;
    }
    // A segment may have landed while the shell idled — drain first.
    virtio_net.net_rx_drain();
    if (!virtio_net.tcp.rx_pending) {
        m.console.print_line("net tcp recv: no data");
        return .none;
    }
    const p = virtio_net.tcp.take_rx();
    m.console.puts("net tcp recv: ");
    var bi: usize = 0;
    while (bi < p.len) : (bi += 1) {
        if (bi > 0) m.console.puts(" ");
        const b = p[bi];
        const hex = "0123456789abcdef";
        var two: [2]u8 = .{ hex[b >> 4], hex[b & 0xf] };
        m.console.puts(&two);
    }
    m.console.puts("\n");
    return .none;
}

/// `net tcp close` — the client-driven close: transmit the FIN
/// (ESTABLISHED -> FIN_SENT; the FIN-ACK is processed by the drain, the
/// final ACK by the next `net tcp`).
fn cmd_net_tcp_close(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 0) {
        print_usage(m, lookup("net").?);
        return .usage;
    }
    if (virtio_net.tcp.state != .established) {
        err_prefix(m);
        m.console.print_line("not established (net tcp connect <addr> <port> first)");
        return .none;
    }
    const seq = virtio_net.tcp.snd_una;
    virtio_net.tcp.build_fin_msg();
    var out_len: usize = 0;
    switch (virtio_net.net_tcp_send(virtio_net.tcp.msg[0..virtio_net.tcp.msg_len], &out_len)) {
        .ok => {
            virtio_net.tcp.fin_sent += 1;
            virtio_net.tcp.advance_snd(1); // the FIN consumes one sequence number
            virtio_net.tcp.record_pending(); // card N11: the unacked FIN is retransmittable
            virtio_net.tcp.state = .fin_sent;
            m.console.puts("net tcp: fin sent (seq=");
            m.console.print_hex_min(seq);
            m.console.puts(", ");
            m.console.print_u64(@intCast(out_len));
            m.console.puts(" bytes)\n");
        },
        else => {
            err_prefix(m);
            m.console.print_line("FIN TX failed (transport unready)");
        },
    }
    return .none;
}

/// `net tcp reset` — the client's abort: transmit a RST (the connection
/// dies; the next `net tcp` returns to IDLE).
fn cmd_net_tcp_reset(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 0) {
        print_usage(m, lookup("net").?);
        return .usage;
    }
    if (virtio_net.tcp.state == .idle) {
        err_prefix(m);
        m.console.print_line("no connection to reset");
        return .none;
    }
    const seq = virtio_net.tcp.snd_una;
    virtio_net.tcp.build_rst_msg();
    var out_len: usize = 0;
    switch (virtio_net.net_tcp_send(virtio_net.tcp.msg[0..virtio_net.tcp.msg_len], &out_len)) {
        .ok => {
            virtio_net.tcp.rst_sent += 1;
            virtio_net.tcp.clear_pending(); // card N11: the connection died — the timer stops
            virtio_net.tcp.state = .closed; // the connection died
            m.console.puts("net tcp: reset sent (seq=");
            m.console.print_hex_min(seq);
            m.console.puts(", ");
            m.console.print_u64(@intCast(out_len));
            m.console.puts(" bytes)\n");
        },
        else => {
            err_prefix(m);
            m.console.print_line("RST TX failed (transport unready)");
        },
    }
    return .none;
}
