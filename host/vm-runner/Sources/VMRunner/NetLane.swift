// virtio-net lane: capture/parse state, packet responders
// (ARP/ICMP/UDP/DNS/DHCP/SNTP/TCP v4+v6), inject + connect drivers,
// vmnet/bridged/NAT plumbing.

import AppKit
import ApplicationServices
import CryptoKit
import Darwin
// DiskImageKit ships in the macOS 27 SDK only (#523 item 2). The class-A
// CI job builds against the older macos-latest SDK, so the import and the
// --overlay-base implementation are compiled in ONLY when the SDK has the
// framework; elsewhere the flag fails honestly at runtime.
#if canImport(DiskImageKit)
import DiskImageKit
#endif
import Foundation
import ScreenCaptureKit
import Security
import Virtualization
// M117d (#2476, ADR 0057 D11): vmnet.framework backs --net-vmnet. The
// vmnet_network_* symbols are macOS 26+, so they exist wherever this
// runner's deployment floor applies; the module-level guard keeps an
// older-SDK toolchain compiling — the flag then refuses by name.
#if canImport(vmnet)
import vmnet
#endif
import VFWire
import VMAppKit
import VMDiskImage
import VMGPU3D
import VMPostmortem
import VMProfile
import VMTcp
import VSCtl
import VSNTP
import VSSH


// Milestone five card N1 (claim 1373): `--net <capture-file>` attaches the
// virtio-net device; the guest's TX frames are captured byte-exactly to the
// file. nil = default (no network device attached, config.networkDevices
// stays [] — the default VM is unchanged).
var netCapturePath: String?

// Milestone five card N2 (claim 6076): `--net-inject <file>` writes the
// file's bytes into the runner's end of the SAME datagram socketpair VZ
// delivers guest-bound frames from, exactly ONCE, when the trigger marker
// appears in the serial log (default: the guest's `net: rx-armed` line —
// the RX buffer is guaranteed supplied; deterministic, not a sleep). nil =
// no injection (the default VM is unchanged).
var netInjectPath: String?

var netInjectAfter: String?

// Milestone five card N3 (claim 7293): `--net-arp-respond <host-ip>`
// answers the guest's ARP requests from the HOST side — a tiny
// deterministic host-side ARP responder inside the capture thread: when a
// captured datagram is an ARP request (ethertype 0x0806, op 1, htype 1,
// ptype 0x0800, hlen 6, plen 4), the synthesized reply (host MAC
// 02:00:00:00:00:02 at the given IP — the same fixed address as the
// guest's fallback_mac) is written into the SAME attachment socket end
// `--net-inject` writes (VZ reads fds[0], so the guest receives it).
// Driven by the guest's actual request bytes, not a sleep. nil = the
// guest's ARP requests go unanswered (the default VM is unchanged).
var netArpRespondHostIP: [UInt8]?

// Milestone five card N4 (claim 0148): `--net-icmp-respond <host-ip>`
// answers the guest's ICMP ECHO REQUESTS from the HOST side — a tiny
// deterministic host-side ICMP responder inside the capture thread: when
// a captured datagram is an ICMP echo request for the given IP (ethertype
// 0x0800, protocol 1, type 8, dst IP match, non-fragment), the
// synthesized echo reply (type 0, id/seq/payload echoed byte-exact, both
// checksums recomputed) is written into the SAME attachment socket end
// the ARP responder writes (VZ reads fds[0], so the guest receives it).
// Driven by the guest's actual request bytes, not a sleep. nil = the
// guest's echo requests go unanswered (the default VM is unchanged).
var netIcmpRespondHostIP: [UInt8]?

// M117b (issue #2474, ADR 0057 D10): the three v6 hermetic responders —
// request-driven synthesizers inside the capture thread, the
// --net-arp-respond shape. `--net-ra-respond <v6-prefix>/64` answers a
// guest Router Solicitation with a Router Advertisement from the router
// link-local fe80::ff:fe00:2 (the EUI-64 derivation of the fixed host
// MAC, deterministic + gate-assertable) carrying the argument prefix as
// an A+L /64 PIO. `--net-ndp-respond <v6-addr>` (repeatable) answers a
// guest Neighbor Solicitation whose target is a listed address with a
// Neighbor Advertisement (S=1 O=1, TLLA 02:00:00:00:00:02); when
// --net-ra-respond is set the router link-local is implicitly listed —
// the guest's router-resolution NS is answered without a second flag.
// An NS for an unlisted target goes unanswered (the refusal leg).
// `--net-icmp6-respond <v6-addr>` answers an ICMPv6 echo request for the
// address byte-exact (id/seq/payload, the --net-icmp-respond mirror).
// All write into the SAME attachment socket end (VZ reads fds[0], so
// the guest receives it). nil flag = unanswered = the default VM
// unchanged.
var netRaRespondPrefix: [UInt8]? // the advertised /64 (16 bytes, IID bits zero)

var netNdpRespondAddrs: [[UInt8]] = []

var netIcmp6RespondAddr: [UInt8]?

// M117c (#2475, ADR 0057): the v6 transport responders — the N5 UDP echo
// and the N10 TCP handshake+echo shapes on the 40-byte fixed header
// (ethertype 0x86dd, next-header 17 / 6). Both take `[v6-addr]:port` —
// the same bracketed literal shape `vi.Dial` speaks — so an address's
// own colons never confuse the parse.
var netUdp6RespondAddr: [UInt8]?

var netUdp6RespondPort: UInt16?

var netTcp6RespondAddr: [UInt8]?

var netTcp6RespondPort: UInt16?

// The hermetic router's link-local — the modified-EUI-64 derivation of
// the fixed host MAC 02:00:00:00:00:02 (ADR 0057 D10).
let netRouterLinkLocal: [UInt8] = [0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xfe, 0, 0, 0x02]

// Milestone five card N5 (claim 8552): `--net-udp-respond <host-ip>:<host-port>`
// answers the guest's UDP datagrams from the HOST side — a tiny
// deterministic host-side UDP responder inside the capture thread: when
// a captured datagram is a UDP datagram for the given ip:port (ethertype
// 0x0800, version 4/IHL 5, non-fragment, protocol 17, dst IP + dst port
// match), the synthesized reply (FROM host-ip:host-port TO the sender's
// ip:src-port, the SAME payload byte-exact, both checksums recomputed)
// is written into the SAME attachment socket end (VZ reads fds[0], so the
// guest receives it). Driven by the guest's actual datagram bytes, not a
// sleep. nil = the guest's datagrams go unanswered (the default VM is
// unchanged).
var netUdpRespondHostIP: [UInt8]?

var netUdpRespondHostPort: UInt16?

// Milestone twelve card N2 (claim 7566): `--net-dns-respond <host-ip>[:<port>]`
// answers the guest's DNS queries from the HOST side — a tiny deterministic
// DNS resolver inside the capture thread.
var netDnsRespondHostIP: [UInt8]?

var netDnsRespondHostPort: UInt16 = 53

// M99i (issue #2187): the responder's depth knobs, all gated on
// --net-dns-respond. `--net-dns-answer <ipv4>` (repeatable) pins the A
// records every A reply carries (empty = the legacy qname table, one
// answer). `--net-dns-answer6 <32 hex digits>` (repeatable) pins the
// AAAA records every AAAA reply carries (empty = one fixed 2001:db8::53).
// `--net-dns-ttl <sec>` is the TTL written into every record (default
// 300). The hostile knobs inject noise BEFORE the real reply so the
// guest's rejection path is exercised, not just asserted:
// `--net-dns-spoof` sends the same reply forged from source IP 10.0.0.9,
// `--net-dns-wrong-question` sends a reply whose echoed question section
// is corrupted, and `--net-dns-truncate` answers every query with TC=1
// and ANCOUNT=0 (the guest must refuse — no TCP fallback exists).
var netDnsAnswers: [[UInt8]] = []

var netDnsAnswers6: [[UInt8]] = []

var netDnsTTL: UInt32 = 300

var netDnsSpoof = false

var netDnsWrongQuestion = false

var netDnsTruncate = false

// M83b (#1775): `--net-sntp-respond <host-ip>[:<port>]` answers the guest's
// SNTP queries with host wall-clock time + `--net-sntp-skew` seconds.
var netSntpRespondHostIP: [UInt8]?

var netSntpRespondHostPort: UInt16 = 123

var netSntpSkew: Int64?

// Milestone five card N8 (claim 0351): `--net-dhcp-respond <lease-ip>`
// answers the guest's DHCP handshake from the HOST side — a tiny
// deterministic host-side DHCP server inside the capture thread: when a
// captured datagram is a DHCPDISCOVER (ethertype 0x0800, protocol 17,
// src port 68 -> dst port 67, dst broadcast), the synthesized OFFER
// (BOOTREPLY, the guest's xid ECHOED, yiaddr = <lease-ip>, mask
// 255.255.255.0, gateway 10.0.0.1, server id = <lease-ip>, lease 3600)
// is written into the SAME attachment socket end the other responders
// write (VZ reads fds[0], so the guest receives it); on the guest's
// DHCPREQUEST (message type 3) the ACK (message type 5, the SAME fixed
// lease) is written. Driven by the guest's actual handshake bytes, not a
// sleep. nil = the guest's DHCP messages go unanswered (the default VM
// is unchanged). The fixed lease is gate-assertable — the `net dhcp`
// bound report must show ip=<lease-ip> mask=255.255.255.0 gw=10.0.0.1
// server=<lease-ip> lease=<lease-secs>.
var netDhcpRespondLeaseIP: [UInt8]?

// Card N9 (claim 9489): the OFFER/ACK lease option 51, in seconds
// (default 3600 — backward compatible; the `:N` suffix sets it, so the
// lease lifecycle is testable in seconds).
var netDhcpRespondLeaseSecs: UInt32 = 3600

// Audit follow-up 3 (issue #119): `--net-dhcp-respond-norenew` refuses
// the guest's UNICAST renewal REQUESTs (mtype 3 with a unicast dst MAC
// — the RENEWING REQUEST from RFC 2131 §4.4.5), so the client's T1
// renewal fails and it must ESCALATE to REBINDING at T2 (the broadcast
// REQUEST, which this knob still answers — it only refuses the unicast
// renew).
var netDhcpRespondNoRenew = false

// Audit follow-up 3 (issue #119): `--net-dhcp-respond-norebind` refuses
// the guest's BROADCAST renewal REQUESTs (mtype 3, broadcast dst, with
// ciaddr != 0 — the REBINDING REQUEST; the INITIAL REQUEST carries
// ciaddr == 0 and is still answered, so the bind + a post-expiry
// re-DISCOVER recovery keep working). With both refusal knobs (or with
// norebind + no resolved server ARP) the client's renewals all fail
// and the lease runs out — the autonomous-expiry evidence.
var netDhcpRespondNoRebind = false

// Milestone five card N10 (claim 7026): `--net-tcp-respond
// <host-ip>:<host-port>` answers the guest's bounded TCP client from the
// HOST side — a tiny deterministic TCP server inside the capture thread:
// the guest's SYN (src 8000 -> host-ip:host-port, protocol 6) is
// answered with a SYN-ACK (the FIXED gate-assertable server ISN
// 0x12345678, ack = the guest's ISN+1); the handshake ACK is observed;
// a data segment is answered with an ACK + the SAME payload byte-exact
// (the echo); the FIN is answered with a FIN-ACK; the final ACK is
// observed. Driven by the guest's actual segment bytes, not a sleep.
// nil = the guest's TCP segments go unanswered (the default VM is
// unchanged). The fixed ISN is gate-assertable — the live gate's python
// walk pins the full seq/ack chain.
var netTcpRespondHostIP: [UInt8]?

var netTcpRespondHostPort: UInt16?

// Each --net-tcp-respond flag is one target the responder answers; a
// --net-tcp-respond-relay names the upstream of the target declared just
// before it (one boot can relay IMAP and SMTP to two fixture servers).
// The mode suffix stays runner-wide.
struct NetTcpRespondTarget {
    let ip: [UInt8]
    let port: UInt16
    var relayPort: UInt16?
    var relayHost: String?
}

var netTcpRespondTargets: [NetTcpRespondTarget] = []

// Card N11 (claim 5357): the optional `:handshake` responder mode —
// answer the SYN with a SYN-ACK, then go SILENT on data/FIN (a
// deterministic data black hole for the gate's retransmission-bound
// run). Default (no suffix) = the full N10 responder.
var netTcpRespondHandshakeOnly = false

// M99h (#2186): the optional `:options` responder mode — the same
// deterministic endpoint, but its SYN-ACK carries a real peer's option
// list (MSS 1460 + SACK-permitted + WS + NOP + a pinned TS, data offset
// 10) and its data/FIN segments carry NOP+NOP+TS (data offset 8). The
// guest must parse-and-skip the options and honor the advertised MSS —
// the shape a real NAT peer puts on the wire.
var netTcpRespondOptionsMode = false

// The pinned option lists (pre-padded to the 4-byte bound). SYN-ACK:
// MSS(4) + SACK-perm(2) + WS=0(3) + NOP(1) + TS(10) = 20 bytes.
// Data/FIN: NOP + NOP + TS(10) = 12 bytes.
let netTcpOptionsSynAck: [UInt8] = [0x02, 0x04, 0x05, 0xb4, 0x04, 0x02, 0x03, 0x03, 0x00, 0x01,
                                  0x08, 0x0a, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00]

let netTcpOptionsData: [UInt8] = [0x01, 0x01, 0x08, 0x0a, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00]

// M51 SSH1 (#1168): the optional `:packet` responder mode — after the
// handshake, page a caller-supplied byte payload out as ≤chunk-byte TCP
// segments, one per guest ACK. This is the deterministic peer the
// `live-ssh-packet` gate needs: the guest's kernel RX is one bounded slot
// with no reassembly, so the host MUST pace (blast several segments and the
// guest drops all but the first; the host has no retransmission).
var netTcpRespondPacketMode = false

var netTcpRespondPacketPayload: [UInt8] = []

var netTcpRespondPacketChunk: Int = 192

// M51 SSH5 (#1172): the optional `:ssh` responder mode — a runner-hosted
// minimal SSH-2 server (VSSH) behind the same deterministic TCP responder:
// curve25519-sha256 KEX with a pinned ssh-ed25519 host key, pinned
// publickey ed25519 userauth, one session channel, one fixed exec. The
// guest's `SSH.BIN` is the client. TX is paced exactly like `:packet`
// (one ≤192-byte segment per guest ACK) because the guest kernel RX is a
// single bounded slot with no reassembly. The pinned fixtures default to
// RFC 8032 §7.1 TEST 1/TEST 2; the flags allow the negative runs to pin
// different keys.
var netTcpRespondSSHMode = false

var netTcpRespondSSHHostKey: [UInt8] = SSHFixtures.hex(SSHFixtures.hostKeySeedHex) ?? []

var netTcpRespondSSHUserKey: [UInt8] = SSHFixtures.hex(SSHFixtures.userPublicKeyHex) ?? []

var netTcpRespondSSHTamperMAC = false

var netTcpRespondSSHMarker = SSHFixtures.defaultMarker

var netTcpRespondSSHExitStatus: UInt32 = 0

// M51 real-OpenSSH interop (#1209, goal #1066, ADR 0025 D8): the optional
// `:relay` responder mode — the same deterministic TCP endpoint, but the
// byte stream is proxied byte-for-byte to a REAL upstream TCP server
// (typically a local OpenSSH `sshd`). The runner performs NO SSH/crypto:
// it only carries bytes, so the guest's SSH.BIN negotiates with real
// OpenSSH. TX is paced exactly like `:ssh` (one ≤192-byte segment per
// guest ACK) because the guest kernel RX is a single bounded slot with
// no reassembly. OFF by default: every existing gate is byte-identical.
var netTcpRespondRelayMode = false

var netTcpRespondRelayHost = "127.0.0.1"

var netTcpRespondRelayPort: UInt16?

// The responder's per-connection state, keyed by the guest's 4-tuple
// (src IP:src port — M99f #2184). The FIXED server ISN (gate-assertable);
// a new SYN on an existing key resets that connection's state. Each
// connection carries its own server sequence, packet-mode index, SSH
// server instance + paced TX, and relay upstream — one guest's socket
// table can hold several live connections and every one is answered.
let netTcpSrvIsn: UInt32 = 0x12345678

final class NetTcpSrvConn {
    var srvNxt: UInt32 = netTcpSrvIsn &+ 1 // after the SYN
    var packetIndex = 0
    var sshServer: SSHServer?
    var sshTx: [UInt8] = []
    var sshTxOffset = 0
    // #2349: the `:relay` upstream state machine (dedup + window/MSS
    // pacing) lives in VMTcp so `swift test` drives it without a VM.
    let relay = NetTcpRelay()
    var closed = false
}

var netTcpSrvConns: [String: NetTcpSrvConn] = [:]

/// Bounded table: 16 live server-side connections; a SYN over the cap is
/// refused with an RST (the same honesty the guest's table applies).
let netTcpSrvConnLimit = 16

func netTcpConnKey(_ guestIP: [UInt8], _ guestPort: UInt16, _ hostPort: UInt16) -> String {
    "\(guestIP[0]).\(guestIP[1]).\(guestIP[2]).\(guestIP[3]):\(guestPort)>\(hostPort)"
}

// M117c (#2475): the v6 connection key — the v6 address rendered
// compactly so it shares the bounded `netTcpSrvConns` table without ever
// colliding with a dotted-quad key.
func netTcp6ConnKey(_ guestAddr: [UInt8], _ guestPort: UInt16) -> String {
    "v6:" + v6Text(guestAddr) + ":\(guestPort)"
}

/// The v6 connection a guest segment belongs to — the v4
/// `netTcpSrvConn(for:)` shape on the 40-byte header (src at 22..38,
/// ports at 54..58).
func netTcp6SrvConn(for buf: [UInt8]) -> NetTcpSrvConn {
    let key = netTcp6ConnKey([UInt8](buf[22..<38]), (UInt16(buf[54]) << 8) | UInt16(buf[55]))
    if let conn = netTcpSrvConns[key] { return conn }
    let conn = NetTcpSrvConn()
    netTcpSrvConns[key] = conn
    return conn
}

/// The connection a guest segment belongs to — lazily created so every
/// segment reaches the per-connection state machine (byte-identical with
/// today's always-on scalar for the single-connection case).
func netTcpSrvConn(for buf: [UInt8]) -> NetTcpSrvConn {
    let key = netTcpConnKey([UInt8](buf[26..<30]), (UInt16(buf[34]) << 8) | UInt16(buf[35]), (UInt16(buf[36]) << 8) | UInt16(buf[37]))
    if let conn = netTcpSrvConns[key] { return conn }
    let conn = NetTcpSrvConn()
    netTcpSrvConns[key] = conn
    return conn
}

// M168f (#2962): append channel-stream bytes to the ssh-session capture.
// The file is created on first write; each later call appends.
func netTcpAppendFile(_ path: String, _ bytes: [UInt8]) {
    let url = URL(fileURLWithPath: path)
    if let handle = try? FileHandle(forWritingTo: url) {
        try? handle.seekToEnd()
        try? handle.write(contentsOf: bytes)
        try? handle.close()
    } else {
        try? Data(bytes).write(to: url)
    }
}

/// Bounded admission for a fresh SYN: drop closed entries first, then
/// refuse when the live table is full (the RST is the caller's to build).
func netTcpSrvAdmit(_ key: String) -> Bool {
    netTcpSrvConns = netTcpSrvConns.filter { !$0.value.closed }
    if netTcpSrvConns[key] == nil, netTcpSrvConns.count >= netTcpSrvConnLimit { return false }
    return true
}

// SH7 (#1083, ADR 0020 Amendment B): `--net-tcp-connect
// <guest-ip>:<port>[:<payload-file>]` — the host initiates an INBOUND TCP
// connection TO a guest listener (the reverse of the N10 responder): SYN,
// handshake, the payload, the guest shell's reply, then a FIN. Frames ride
// the same `--net` attachment socketpair. The guest server's ISN is the
// fixed `kernel/src/tcp.zig` constant 0x54321098; the client ISN/source
// port are fixed for gate assertability.
// M99f (#2184): each `--net-tcp-connect` flag is ONE host client with its
// own state machine, keyed by the guest listener port; its source port is
// `netTcpCliPort` + index so two clients to the same guest port still
// have distinct 5-tuples. `netTcpConnectGuestIP`/`netTcpConnectPort`/
// `netTcpConnectPayload` remain the FIRST target's mirrors for the
// validation/print paths — a single flag behaves byte-identically.
var netTcpConnectGuestIP: [UInt8]?

var netTcpConnectPort: UInt16?

var netTcpConnectPayload: [UInt8] = []

var netTcpConnectAfter: String?

var netTcpConnectCloseAfter: String?

// M50 TS4 (#1138, ADR 0024 D6): the delegated-auth client. With a secret
// set the client waits for the guest's `VIRELAIOS-AUTH/1` challenge, answers
// with hex(HMAC-SHA256(secret, domain || 0x00 || challenge)), then sends the
// payload. `netTcpConnectMac` is the replay path: send that pre-recorded MAC
// hex line instead of computing one (a captured handshake replayed against a
// fresh challenge must fail).
var netTcpConnectSecret: String?

var netTcpConnectMac: String?

// M70g G1 (#1491): `--net-tcp-connect …:ssh` — the runner-hosted SSH-2
// client that dials the in-guest GOSSHD.ELF. TX is paced one ≤192-byte
// segment per guest ACK (the guest kernel RX is a single slot).
var netTcpConnectSSHMode = false

var netTcpConnectSSHUserSeed: [UInt8] = SSHFixtures.hex(SSHFixtures.userKeySeedHex) ?? []

var netTcpConnectSSHHostPin: [UInt8] = SSHFixtures.hex(SSHFixtures.hostPublicKeyHex) ?? []

var netTcpConnectSSHUser = "alice"

var netTcpConnectSSHExec = "echo VIRELAI-SSH-SERVER-OK-ARGV2"

// M168f (#2962): the scripted pty session. `netTcpConnectSSHPty` switches
// the channel request to pty-req + shell; `netTcpConnectSSHKeys` is the
// parsed line script each client's SSHClient walks against the decrypted
// channel stream; `netTcpConnectSSHSessionFile` is where the CHANNEL_DATA
// bytes land (the gate's per-run ssh-session-<tag>.bin).
var netTcpConnectSSHPty: (cols: UInt32, rows: UInt32)?

var netTcpConnectSSHKeys: SSHKeyScript?

var netTcpConnectSSHSessionFile: String?

// The hermetic RFB gate's byte-stream adapter. Unlike the payload-file
// client this pipes a local probe's stdin/stdout through the existing
// host->guest TCP emulation. It never binds a host port or uses --net-nat.
// M99f: it attaches to the FIRST connect target's client.
var netTcpConnectStreamExec: String?

var netTcpConnectStreamArgs: [String] = []

// #3391: `--net-tcp-connect-resyn <n>` — a non-stream, non-SSH client
// re-SYNs on each NEW sighting of the after-marker while attempts
// remain. Against a relistening daemon (agentd prints its listen marker
// per session) the marker itself is the sequential-session clock: every
// sighting admits exactly one fresh connection.
var netTcpConnectResyn = 0

// A second viewer's SYN, sent from the next source port while the first
// holds the guest's one connection. The guest's answer (or silence) is the
// over-cap receipt.
var netTcpConnectIntruder = false

var nativeSocketProbeMode: String?

var nativeSocketProbe: NativeSocketProbe?

let netTcpIntruderIsn: UInt32 = 0x0badf00d

let netTcpStreamLock = NSLock()

let netTcpStreamMaxTx = 16 * 1024

// Lifetime byte bound, not a queue: one raw 1280x720x4 RFB frame plus
// handshake fits; the old 64 KiB cut off even a compressed first frame.
let netTcpStreamMaxRx = 4 * 1024 * 1024

let netTcpCliIsn: UInt32 = 0x10203040

let netTcpCliPort: UInt16 = 12345

let netTcpCliSrcIP: [UInt8] = [10, 0, 0, 2]

// M206a (#3813): a var so --mac can move the guest's MAC off the
// pinned default — the host-initiated client's frames must carry the
// address the guest actually owns.
var netTcpGuestMAC: [UInt8] = [0x02, 0x00, 0x00, 0x00, 0x00, 0x01]

let netTcpGuestSrvIsn: UInt32 = 0x54321098

/// One host-initiated connection's state machine (0 idle, 1 synSent,
/// 2 established, 3 closed, 4 finSent, 5 awaitingChallenge) plus the
/// SSH/stream/intruder per-connection state — M99f (#2184).
final class NetTcpClient {
    let guestIP: [UInt8]
    let guestPort: UInt16
    let cliPort: UInt16
    var payload: [UInt8]
    var sshMode: Bool
    var state: UInt8 = 0
    var seq: UInt32 = 0
    var ack: UInt32 = 0
    var recvText = ""
    var sshClient: SSHClient?
    var sshTx: [UInt8] = []
    var sshTxOffset = 0
    // M168f (#2962): how much of `sshClient.stdout` has already landed in
    // the --net-tcp-connect-ssh-session-file capture.
    var sshSessionWritten = 0
    // M99f (#2184): the guest's advertised window, refreshed on every
    // inbound segment. The guest kernel reports 0 while its one-slot RX
    // buffer holds an unconsumed segment, and the guest emits a
    // window-update ACK when its read drains the slot — so holding a
    // chunk back while peerWindow == 0 loses nothing.
    var peerWindow: UInt32 = 0
    // False until the guest Recv's the first SSH chunk and emits bytes.
    // The kernel ACKs a segment as soon as it lands in the one-slot RX
    // buffer (tcp.zig syn_received / established), so a bare ACK or
    // SYN-ACK retransmit must NOT release the next ≤192-byte chunk — that
    // overflow is dropped and never ACKed. Guest data is the signal.
    var sshGuestDataSeen = false
    var streamProcess: Process?
    var streamInput: FileHandle?
    var streamTx: [UInt8] = []
    var streamInFlight = false
    var streamDone = false
    var streamEOF = false // the probe exited 0: FIN once its bytes are ACKed
    var streamRxBytes = 0
    var intruderSent = false
    var intruderAnswered = false
    /// `--net-tcp-connect-after-skip <n>` (#3391): the client's SYN fires
    /// only once the after-marker's occurrence count passes n — several
    /// clients can take turns on one relisten marker, e.g. a spam client
    /// on every sighting and an ender client on the last.
    var afterSkip = 0
    /// `--net-tcp-connect-stream` attaches the probe to the FIRST declared
    /// client only (M99f: later clients never take the stream branch).
    var isStreamClient: Bool {
        netTcpConnectStreamExec != nil && netTcpClients.first === self
    }

    init(guestIP: [UInt8], guestPort: UInt16, cliPort: UInt16, payload: [UInt8], sshMode: Bool) {
        self.guestIP = guestIP
        self.guestPort = guestPort
        self.cliPort = cliPort
        self.payload = payload
        self.sshMode = sshMode
    }
}

var netTcpClients: [NetTcpClient] = []

// Milestone five card N7 (claim 4678): `--net-nat` attaches one
// VZVirtioNetworkDeviceConfiguration with a VZNATNetworkDeviceAttachment
// instead of the file-handle attachment — the host is the guest's router
// and performs NAT for accesses to outside networks. Boolean, OFF by
// default: without the flag config.networkDevices stays [] (the default
// VM is unchanged). Mutually exclusive with `--net` (one network device
// per guest for now).
var netNatEnabled = false

// M117d (issue #2476, ADR 0057 D11): the opt-in real-LAN attachments.
// `--net-bridged <iface>` attaches a VZBridgedNetworkDeviceAttachment on
// the named VZBridgedNetworkInterface; `--net-vmnet` attaches a
// VZVmnetNetworkDeviceAttachment on a shared-mode vmnet logical network
// created up front (vmnet_network_create). Both are off by default,
// mutually exclusive with --net/--net-nat/each other, and every missing
// piece refuses by name — the absent interface, the absent
// com.apple.vm.networking entitlement in this signature, or the
// non-success vmnet_return_t the daemon returns when it lacks
// com.apple.developer.networking.vmnet/root. netBridgedIface and
// netVmnetNetwork hold the validated objects between flag checks and the
// attachment assembly below.
var netBridgedInterface: String?

var netVmnetEnabled = false

var netVmnetSubnet: (ip: [UInt8], prefixLen: Int)?

var netBridgedIface: VZBridgedNetworkInterface?

var netVmnetSubnetText: String?

#if canImport(vmnet)
var netVmnetNetwork: vmnet_network_ref?
#endif

// Milestone five card N1 (claim 1373): the virtio-net device, attached only
// under `--net <capture-file>`. VZFileHandleNetworkDeviceAttachment transmits
// raw data-link frames over ONE connected datagram socket: VZ holds one end
// (every guest-transmitted frame arrives as a datagram; host->guest frames
// would be written into the same socket — that is card N2's RX direction),
// and the runner reads the other end and appends each datagram to the
// capture file byte-exactly. The guest MAC is FIXED on the host config
// (VZMACAddress "02:00:00:00:00:01", locally administered) so the guest's
// VIRTIO_NET_F_MAC read is deterministic and gate-assertable. Without the
// flag the config is exactly as before: networkDevices = [] — every existing
// gate stays byte-identical.
var netCaptureHandle: FileHandle? // capture file (append)

var netCaptureSocket: FileHandle? // the socket end VZ transmits to

var netCaptureReadSocket: FileHandle? // the runner's read end

var netCaptureStop = false

var netCaptureThread: Thread?

var netCaptureDone = DispatchSemaphore(value: 0)

let netSntpResponder: SNTPResponder? = netSntpRespondHostIP.map {
    SNTPResponder(hostIP: $0, hostPort: netSntpRespondHostPort, skew: netSntpSkew ?? 0,
                  clock: { Date().timeIntervalSince1970 })
}

#if canImport(vmnet)
func vmnetStatusName(_ status: vmnet_return_t) -> String {
    switch status.rawValue {
    case 1000: return "VMNET_SUCCESS"
    case 1001: return "VMNET_FAILURE"
    case 1002: return "VMNET_MEM_FAILURE"
    case 1003: return "VMNET_INVALID_ARGUMENT"
    case 1004: return "VMNET_SETUP_INCOMPLETE"
    case 1005: return "VMNET_INVALID_ACCESS"
    case 1006: return "VMNET_PACKET_TOO_BIG"
    case 1007: return "VMNET_BUFFER_EXHAUSTED"
    case 1008: return "VMNET_TOO_MANY_PACKETS"
    case 1009: return "VMNET_SHARING_SERVICE_BUSY"
    case 1010: return "VMNET_NOT_AUTHORIZED"
    default: return "vmnet_return_t(\(status.rawValue))"
    }
}
#endif

#if canImport(vmnet)
func ipv4Text(_ addr: in_addr) -> String {
    let b = UInt32(bigEndian: addr.s_addr)
    return "\(b >> 24 & 0xff).\(b >> 16 & 0xff).\(b >> 8 & 0xff).\(b & 0xff)"
}
#endif

#if canImport(vmnet)
func prefixLen(_ mask: in_addr) -> Int {
    Int((UInt32(bigEndian: mask.s_addr)).nonzeroBitCount)
}
#endif

/// Answer one `VIRELAIOS-AUTH/1 <scheme> <hex-challenge>` line with
/// `hex(HMAC-SHA256(secret, "<domain>" || 0x00 || challenge)) + "\n"` (the
/// TS4 acceptance primitive). `netTcpConnectMac` sends the pre-recorded hex
/// line instead (the captured-handshake replay path). Returns nil on a
/// malformed line or an unsupported scheme (Ed25519 host signing is the
/// recorded TS4 stretch) — the caller then closes honestly.
func netTcpAuthAnswer(challenge payload: [UInt8]) -> [UInt8]? {
    guard let newline = payload.firstIndex(of: 0x0a) else { return nil }
    var line = payload[0..<newline]
    if line.last == 0x0d { line = line.dropLast() }
    let parts = String(decoding: line, as: UTF8.self).split(separator: " ")
    guard parts.count == 3, parts[0] == "VIRELAIOS-AUTH/1" else { return nil }
    guard let challengeBytes = hexDecode(String(parts[2])) else { return nil }
    if let mac = netTcpConnectMac {
        return Array((mac + "\n").utf8)
    }
    guard parts[1] == "hmac-sha256", let secret = netTcpConnectSecret else { return nil }
    var message = Data("VIRELAIOS-AUTH/1 hmac-sha256".utf8)
    message.append(0)
    message.append(contentsOf: challengeBytes)
    let key = SymmetricKey(data: Data(secret.utf8))
    let mac = HMAC<SHA256>.authenticationCode(for: message, using: key)
    return Array((hexString(Array(mac)) + "\n").utf8)
}

// Claim 6076 (milestone five card N2): the host→guest RX injection. Waits
// until the trigger marker appears in the serial log (default: the guest's
// `net: rx-armed` line — the queue-0 RX buffer is guaranteed supplied and
// kicked, so the datagram cannot race an unarmed ring), then writes the
// --net-inject file's bytes into the runner's OTHER end of the attachment
// socketpair EXACTLY ONCE. Direction (observed in the claim-6076 probe):
// the attachment socket is fds[0], the reader's end is fds[1] — VZ READS
// fds[0] for host→guest packets and WRITES fds[0] for guest→host packets,
// so on a socketpair a datagram written to fds[1] is exactly what VZ's
// read on fds[0] consumes (a write to fds[0] instead would be captured by
// the runner's own reader and never reach the guest — the probe's capture
// held the injected bytes). The reader thread reads fds[1] for datagrams
// written to fds[0] (guest TX), so the two directions never race. The
// injected bytes are the raw Ethernet frame the guest's net recv must
// print byte-exact. Deterministic — a serial trigger, not a sleep.
// Requires --net (validated at parse time); a no-op without it.
func startNetInject() {
    guard let path = netInjectPath, let socket = netCaptureReadSocket else { return }
    let q = DispatchQueue(label: "virelaios.netinject")
    q.async {
        let injectData: Data
        do {
            injectData = try Data(contentsOf: URL(fileURLWithPath: path))
        } catch {
            FileHandle.standardError.write(Data("ERROR: could not read net-inject file '\(path)': \(error)\n".utf8))
            exit(1)
        }
        let marker = netInjectAfter ?? "net: rx-armed"
        let waitDeadline = Date().addingTimeInterval(40)
        var sent = false
        // Poll every 20 ms (claim 7293): a scripted gate that injects at a
        // MID-SCRIPT marker (the guest's `net ip: ip=...` echo, not a
        // boot-time marker) must not race the commands that follow the
        // marker — the guest executes the script burst in tens of ms, so a
        // 0.5 s poll would land the datagram AFTER the observation
        // commands already ran.
        while Date() < waitDeadline {
            if let text = try? String(contentsOf: serialURL, encoding: .utf8),
               text.contains(marker) {
                // One datagram per write (SOCK_DGRAM); the injected bytes
                // are the raw Ethernet frame the guest must receive.
                do {
                    try socket.write(contentsOf: injectData)
                    FileHandle.standardOutput.write(Data("NET-INJECT: sent \(injectData.count) byte(s) into the attachment socketpair (VZ reads fds[0]) after \"\(marker)\"\n".utf8))
                } catch {
                    FileHandle.standardError.write(Data("ERROR: net-inject write failed: \(error)\n".utf8))
                }
                sent = true
                break
            }
            Thread.sleep(forTimeInterval: 0.02)
        }
        if !sent {
            FileHandle.standardError.write(Data("ERROR: guest did not emit net-inject marker '\(marker)' within 40s; injection not sent\n".utf8))
        }
    }
}

// SH7 (#1083, ADR 0020 Amendment B): the host-initiated inbound TCP client
// kicker — wait for the guest shell's listening marker, then write the SYN
// into the attachment socketpair. The capture thread drives the rest
// (SYN-ACK -> ACK+payload -> data -> FIN). Deterministic — a serial
// trigger, not a sleep. Requires --net (validated at parse time).
// M99f (#2184): one SYN-watcher per declared client — several
// `--net-tcp-connect` flags each wait for the marker and fire their own
// SYN on their own source port.
func startNetTcpConnect() {
    guard let socket = netCaptureReadSocket else { return }
    for client in netTcpClients {
        let q = DispatchQueue(label: "virelaios.nettcpconnect.\(client.cliPort)")
        q.async {
            let marker = netTcpConnectAfter ?? "sh: remote"
            // #3391: a resyn client treats each NEW sighting of the marker
            // as a fresh SYN slot — against a relisten-on-session-end
            // daemon the marker IS the sequential-session clock. Stream
            // and SSH clients never resyn. afterSkip skips the first n
            // sightings so clients can take turns on the one marker.
            let resyn = (client.isStreamClient || client.sshMode) ? 0 : netTcpConnectResyn
            let waitDeadline = Date().addingTimeInterval(resyn > 0 ? 1800 : 60)
            var sent = false
            var attempts = 1 + resyn
            var seen = 0
            while Date() < waitDeadline {
                if let text = try? String(contentsOf: serialURL, encoding: .utf8) {
                    let occ = text.components(separatedBy: marker).count - 1
                    if occ > seen {
                        seen = occ
                        if occ > client.afterSkip && attempts > 0 &&
                            (client.state == 0 || client.state == 3) {
                            attempts -= 1
                            client.seq = netTcpCliIsn
                            client.ack = 0
                            client.sshGuestDataSeen = false
                            client.state = 1
                            var syn = [UInt8](repeating: 0, count: 4096)
                            let len = buildTcpFrameExplicit(&syn, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, client.guestIP, client.cliPort, client.guestPort, netTcpCliIsn, 0, 0x02, [])
                            do {
                                try socket.write(contentsOf: Data(syn[0..<len]))
                                let ipText = client.guestIP.map(String.init).joined(separator: ".")
                                FileHandle.standardOutput.write(Data("NET-TCP-CONNECT: sent SYN to \(ipText):\(client.guestPort) (seq 0x\(hex32(netTcpCliIsn))) after \"\(marker)\" sighting \(occ)\n".utf8))
                            } catch {
                                FileHandle.standardError.write(Data("ERROR: net-tcp-connect SYN write failed: \(error)\n".utf8))
                            }
                            sent = true
                        }
                    }
                }
                if sent && (resyn == 0 || attempts == 0) { break }
                Thread.sleep(forTimeInterval: 0.02)
            }
            if !sent {
                FileHandle.standardError.write(Data("ERROR: guest did not emit net-tcp-connect marker '\(marker)' within the wait window; connection not attempted\n".utf8))
            }
        }
    }
}

func startNativeSocketProbe() {
    guard let probe = nativeSocketProbe, let socket = netCaptureReadSocket else { return }
    DispatchQueue(label: "virelaios.native-preview").async {
        while Date() < deadline {
            if let text = try? String(contentsOf: serialURL, encoding: .utf8),
               text.contains("native-socket: listening two-client limit") {
                probe.start(socket: socket)
                return
            }
            Thread.sleep(forTimeInterval: 0.02)
        }
        print("NATIVE-PROBE: FAIL listener marker missing")
    }
}

// Card N3 (claim 7293): is the datagram an ARP request (Ethernet II
// ethertype 0x0806, ARP htype 1 / ptype 0x0800 / hlen 6 / plen 4, op 1)?
// The guest's request frames are raw Ethernet — dst ff*6 (broadcast),
// src own MAC, ethertype at bytes 12-13, the ARP payload at 14..42.
func isArpRequest(_ buf: [UInt8], _ n: Int) -> Bool {
    guard n >= 42 else { return false }
    guard buf[12] == 0x08 && buf[13] == 0x06 else { return false }
    guard buf[14] == 0x00 && buf[15] == 0x01 else { return false } // htype ethernet
    guard buf[16] == 0x08 && buf[17] == 0x00 else { return false } // ptype IPv4
    guard buf[18] == 0x06 && buf[19] == 0x04 else { return false } // hlen/plen
    guard buf[20] == 0x00 && buf[21] == 0x01 else { return false } // op request
    return true
}

// Card N3 (claim 7293): synthesize the 42-byte ARP REPLY to the request in
// `req` (the guest's bytes): dst = the requester's MAC, src = host MAC,
// ethertype 0x0806, op 2, sha/spa = host MAC/IP, tha/tpa = the requester's
// MAC/IP (copied from the request's sha/spa — the standard answer shape).
func buildArpReply(_ reply: inout [UInt8], _ req: [UInt8], _ n: Int, _ hostMAC: [UInt8], _ hostIP: [UInt8]) {
    _ = n
    reply[0...5] = req[22...27] // dst = the requester's MAC (its sha)
    reply[6...11] = hostMAC[0...5] // src
    reply[12] = 0x08
    reply[13] = 0x06
    reply[14] = 0x00
    reply[15] = 0x01
    reply[16] = 0x08
    reply[17] = 0x00
    reply[18] = 0x06
    reply[19] = 0x04
    reply[20] = 0x00
    reply[21] = 0x02 // op reply
    reply[22...27] = hostMAC[0...5] // sha
    reply[28...31] = hostIP[0...3] // spa
    reply[32...37] = req[22...27] // tha = the requester's MAC
    reply[38...41] = req[28...31] // tpa = the requester's IP
}

// Card N4 (claim 0148): is the datagram an ICMP ECHO REQUEST for our
// address? Ethernet II ethertype 0x0800, version 4 / IHL 5 (0x45),
// protocol ICMP (1), dst IP == `hostIP`, ICMP type 8. The guest's
// `net ping` frames are raw Ethernet — header at 14..34, ICMP at 34.
func isIcmpEchoRequest(_ buf: [UInt8], _ n: Int, _ hostIP: [UInt8]) -> Bool {
    guard n >= 46 else { return false }
    guard buf[12] == 0x08 && buf[13] == 0x00 else { return false } // ethertype IPv4
    guard buf[14] == 0x45 else { return false } // version 4, IHL 5
    guard (buf[20] & 0x1f) == 0 && buf[21] == 0 else { return false } // NOT a fragment (the ICMP header would not be at 34)
    guard buf[23] == 0x01 else { return false } // protocol ICMP
    guard buf[30] == hostIP[0] && buf[31] == hostIP[1] && buf[32] == hostIP[2] && buf[33] == hostIP[3] else { return false }
    guard buf[34] == 0x08 else { return false } // ICMP echo request
    return true
}

// Card N5 (claim 8552): the UDP checksum over the IPv4 pseudo-header
// (src IP, dst IP, zero, protocol 17, UDP length) + the datagram (RFC
// 768 §2 / RFC 1071; the checksum field must be zero during the build).
@Sendable func udpChecksum(_ srcIP: [UInt8], _ dstIP: [UInt8], _ datagram: [UInt8], _ udpLen: UInt16) -> UInt16 {
    var words: [UInt16] = []
    func push(_ hi: UInt8, _ lo: UInt8) {
        words.append((UInt16(hi) << 8) | UInt16(lo))
    }
    push(srcIP[0], srcIP[1])
    push(srcIP[2], srcIP[3])
    push(dstIP[0], dstIP[1])
    push(dstIP[2], dstIP[3])
    words.append(UInt16(17)) // zero + protocol UDP
    words.append(udpLen)
    var i = 0
    while i + 1 < datagram.count {
        push(datagram[i], datagram[i + 1])
        i += 2
    }
    if i < datagram.count { push(datagram[i], 0) }
    var sum: UInt32 = 0
    for w in words { sum += UInt32(w) }
    while sum >> 16 != 0 { sum = (sum & 0xffff) + (sum >> 16) }
    return UInt16(~sum & 0xffff)
}

// M117b (#2474, ADR 0057 D10): parse a text IPv6 address — any
// inet_pton-legal form ("::" compression, mixed case). The runner is a
// host process; Darwin's inet_pton is the honest parser (no hand-rolled
// hex walk).
@Sendable func parseIPv6(_ text: String) -> [UInt8]? {
    var addr = in6_addr()
    return text.withCString { cstr in
        guard inet_pton(AF_INET6, cstr, &addr) == 1 else { return nil }
        return withUnsafeBytes(of: &addr) { Array($0) }
    }
}

// M117b: the canonical text form of a v6 address (inet_ntop — the same
// compressed shape the guest's formatter emits for these values).
@Sendable func v6Text(_ a: [UInt8]) -> String {
    var addr = in6_addr()
    withUnsafeMutableBytes(of: &addr) { $0.copyBytes(from: a) }
    var buf = [CChar](repeating: 0, count: Int(INET6_ADDRSTRLEN))
    inet_ntop(AF_INET6, &addr, &buf, socklen_t(INET6_ADDRSTRLEN))
    return String(cString: buf)
}

// M117b: the ICMPv6 pseudo-header checksum — 16-byte src + dst, the
// 32-bit upper-layer length, 3 zero bytes + next-header — over `msg`
// (RFC 8200 §8.1; the checksum field must be zero during the build).
@Sendable func icmp6Checksum(_ srcIP: [UInt8], _ dstIP: [UInt8], _ msg: [UInt8], _ nextHeader: UInt8) -> UInt16 {
    var sum: UInt32 = 0
    var i = 0
    while i < 16 {
        sum += (UInt32(srcIP[i]) << 8) | UInt32(srcIP[i + 1])
        sum += (UInt32(dstIP[i]) << 8) | UInt32(dstIP[i + 1])
        i += 2
    }
    sum += UInt32(msg.count >> 16) + UInt32(msg.count & 0xffff)
    sum += UInt32(nextHeader)
    i = 0
    while i + 1 < msg.count {
        sum += (UInt32(msg[i]) << 8) | UInt32(msg[i + 1])
        i += 2
    }
    if i < msg.count { sum += UInt32(msg[i]) << 8 }
    while sum >> 16 != 0 { sum = (sum & 0xffff) + (sum >> 16) }
    return UInt16(~sum & 0xffff)
}

// M117b: is the datagram an IPv6 frame carrying ICMPv6 of the given
// type? Ethernet II ethertype 0x86dd, version 6, next-header 58 (the
// guest sends terminal next-headers only), the ICMPv6 type byte at 54.
@Sendable func isIcmp6Type(_ buf: [UInt8], _ n: Int, _ type: UInt8) -> Bool {
    guard n >= 56 else { return false }
    guard buf[12] == 0x86 && buf[13] == 0xdd else { return false }
    guard buf[14] >> 4 == 0x6 else { return false }
    guard buf[20] == 58 else { return false }
    return buf[54] == type
}

@Sendable func isRouterSolicitation(_ buf: [UInt8], _ n: Int) -> Bool {
    return isIcmp6Type(buf, n, 133)
}

@Sendable func isNeighborSolicitation(_ buf: [UInt8], _ n: Int) -> Bool {
    return isIcmp6Type(buf, n, 135)
}

// M117b: the NS's target when it names an answerable address — an
// explicit --net-ndp-respond entry, or the router link-local implicitly
// listed by --net-ra-respond (the guest's router-resolution NS is
// answered without a second flag). nil = unanswered (the refusal leg).
@Sendable func ndpRespondTarget(_ buf: [UInt8], _ n: Int) -> [UInt8]? {
    guard n >= 78 else { return nil }
    let target = [UInt8](buf[62..<78])
    for a in netNdpRespondAddrs where a == target { return target }
    if netRaRespondPrefix != nil, target == netRouterLinkLocal { return target }
    return nil
}

// M117b: is the datagram an ICMPv6 echo request for `addr`?
@Sendable func isIcmp6EchoRequest(_ buf: [UInt8], _ n: Int, _ addr: [UInt8]) -> Bool {
    guard isIcmp6Type(buf, n, 128) else { return false }
    return [UInt8](buf[38..<54]) == addr
}

// M117b: synthesize the ROUTER ADVERTISEMENT to a guest RS: dst
// all-nodes (33:33:00:00:00:01 / ff02::1 — the guest's D2 filter admits
// it), src the router link-local, hop 255, cur-hop 64, M|O 0, router
// lifetime 1800, reachable/retrans 0, ONE Prefix Information option
// carrying the argument /64 with A+L and finite recorded lifetimes
// (valid 86400, preferred 14400). 102 bytes total.
@Sendable func buildRouterAdvertisement(_ reply: inout [UInt8], _ hostMAC: [UInt8], _ prefix: [UInt8]) {
    reply = [UInt8](repeating: 0, count: 102)
    reply[0...5] = [0x33, 0x33, 0x00, 0x00, 0x00, 0x01] // all-nodes dst
    reply[6...11] = hostMAC[0...5] // src
    reply[12] = 0x86
    reply[13] = 0xdd
    reply[14] = 0x60 // version 6
    reply[18] = 0
    reply[19] = 48 // payload_len: 16-byte RA fixed part + 32-byte PIO
    reply[20] = 58 // ICMPv6
    reply[21] = 255 // hop limit — the NDP standing check
    reply[22...37] = netRouterLinkLocal[0...15] // src = the router link-local
    reply[38] = 0xff
    reply[39] = 0x02
    reply[53] = 0x01 // dst = ff02::1 (all-nodes)
    let src = netRouterLinkLocal
    var dst = [UInt8](repeating: 0, count: 16)
    dst[0] = 0xff
    dst[1] = 0x02
    dst[15] = 0x01
    // ICMPv6 RA: type 134, code 0, checksum, cur-hop 64, flags 0,
    // lifetime 1800, reachable 0, retrans 0.
    reply[54] = 134
    reply[58] = 64
    reply[60] = 0x07
    reply[61] = 0x08 // router lifetime 1800
    // PIO at 70: type 3, len 4, prefix_len 64, flags A|L (0xc0),
    // valid 86400, preferred 14400, reserved 0, prefix.
    reply[70] = 3
    reply[71] = 4
    reply[72] = 64
    reply[73] = 0xc0
    reply[74] = 0
    reply[75] = 1
    reply[76] = 0x51
    reply[77] = 0x80 // valid lifetime 86400
    reply[78] = 0
    reply[79] = 0
    reply[80] = 0x46
    reply[81] = 0x50 // preferred lifetime 14400
    reply[86...101] = prefix[0...15]
    let chk = icmp6Checksum(src, dst, [UInt8](reply[54..<102]), 58)
    reply[56] = UInt8(chk >> 8)
    reply[57] = UInt8(chk & 0xff)
}

// M117b: synthesize the NEIGHBOR ADVERTISEMENT to the guest's NS in
// `req`: dst = the request's observed Ethernet source (all-nodes when
// its v6 src was :: — the DAD answer), src = the solicited `target`,
// flags S|O, target echoed, TLLA = the host MAC, hop 255. 86 bytes.
@Sendable func buildNeighborAdvertisement(_ reply: inout [UInt8], _ req: [UInt8], _ n: Int, _ hostMAC: [UInt8], _ target: [UInt8]) {
    reply = [UInt8](repeating: 0, count: 86)
    let reqSrc = [UInt8](req[22..<38])
    let unspecified = reqSrc == [UInt8](repeating: 0, count: 16)
    if unspecified {
        reply[0...5] = [0x33, 0x33, 0x00, 0x00, 0x00, 0x01] // all-nodes
    } else {
        reply[0...5] = req[6...11] // dst = the observed src MAC
    }
    reply[6...11] = hostMAC[0...5] // src
    reply[12] = 0x86
    reply[13] = 0xdd
    reply[14] = 0x60
    reply[18] = 0
    reply[19] = 32 // payload_len: flags(4) + target(16) + TLLA(8) + hdr(4)
    reply[20] = 58
    reply[21] = 255
    reply[22...37] = target[0...15] // src = the solicited target
    if unspecified {
        reply[38] = 0xff
        reply[39] = 0x02
        reply[53] = 0x01 // dst = ff02::1
    } else {
        reply[38...53] = reqSrc[0...15] // dst = the requester's address
    }
    reply[54] = 136 // NA
    reply[58] = 0x60 // flags S|O
    reply[62...77] = target[0...15] // target echoed
    reply[78] = 2 // TLLA option type
    reply[79] = 1 // length 1
    reply[80...85] = hostMAC[0...5]
    let dst = [UInt8](reply[38..<54])
    let chk = icmp6Checksum(target, dst, [UInt8](reply[54..<86]), 58)
    reply[56] = UInt8(chk >> 8)
    reply[57] = UInt8(chk & 0xff)
}

// M117b: synthesize the ICMPv6 ECHO REPLY to the echo request in `req`
// — Ethernet dst/src swapped, v6 src/dst swapped (the reply's src is
// the address it was sent to), hop 64, type 129, id/seq/payload echoed
// byte-exact, the pseudo-header checksum recomputed.
@Sendable func buildIcmp6EchoReply(_ reply: inout [UInt8], _ req: [UInt8], _ n: Int, _ hostMAC: [UInt8]) {
    reply = [UInt8](repeating: 0, count: n)
    reply[0...5] = req[6...11] // dst = the requester's MAC
    reply[6...11] = hostMAC[0...5] // src
    reply[12] = 0x86
    reply[13] = 0xdd
    reply[14] = 0x60
    reply[18...19] = req[18...19] // payload_len (unchanged; TC/flow stays 0)
    reply[20] = 58
    reply[21] = 64 // hop limit — echo traffic, not NDP
    reply[22...37] = req[38...53] // src = the addressed address
    reply[38...53] = req[22...37] // dst = the requester's address
    reply[54] = 129 // echo reply
    if n > 58 {
        reply[58...n - 1] = req[58...n - 1] // id + seq + payload echoed
    }
    let src = [UInt8](reply[22..<38])
    let dst = [UInt8](reply[38..<54])
    let chk = icmp6Checksum(src, dst, [UInt8](reply[54..<n]), 58)
    reply[56] = UInt8(chk >> 8)
    reply[57] = UInt8(chk & 0xff)
}

// Card N5 (claim 8552): is the datagram a UDP datagram addressed to
// `hostIP:hostPort`? Ethernet II ethertype 0x0800, version 4 / IHL 5,
// NOT a fragment, protocol UDP (17), dst IP match, dst port match.
func isUdpDatagram(_ buf: [UInt8], _ n: Int, _ hostIP: [UInt8], _ hostPort: UInt16) -> Bool {
    guard n >= 46 else { return false }
    guard buf[12] == 0x08 && buf[13] == 0x00 else { return false } // ethertype IPv4
    guard buf[14] == 0x45 else { return false } // version 4, IHL 5
    guard (buf[20] & 0x1f) == 0 && buf[21] == 0 else { return false } // NOT a fragment
    guard buf[23] == 17 else { return false } // protocol UDP
    guard buf[30] == hostIP[0] && buf[31] == hostIP[1] && buf[32] == hostIP[2] && buf[33] == hostIP[3] else { return false }
    let dstPort = (UInt16(buf[36]) << 8) | UInt16(buf[37])
    return dstPort == hostPort
}

// Card N5 (claim 8552): synthesize the UDP REPLY to the datagram in `req`
// (the guest's bytes) into `reply` (same length): Ethernet dst/src
// swapped, ethertype 0x0800, IPv4 src/dst swapped (identification
// ECHOED — deterministic), TTL 64, protocol 17, UDP src port = the host
// port, dst port = the sender's src port, the payload ECHOED byte-exact;
// the IPv4 header checksum + the UDP pseudo-header checksum recomputed.
func buildUdpReply(_ reply: inout [UInt8], _ req: [UInt8], _ n: Int, _ hostMAC: [UInt8], _ hostIP: [UInt8], _ hostPort: UInt16) {
    reply = [UInt8](repeating: 0, count: n)
    reply[0...5] = req[6...11] // dst = the sender's MAC
    reply[6...11] = hostMAC[0...5] // src
    reply[12] = 0x08
    reply[13] = 0x00 // ethertype IPv4
    reply[14] = 0x45 // version 4, IHL 5
    reply[16...17] = req[16...17] // total length (unchanged)
    reply[18...19] = req[18...19] // identification ECHOED
    reply[22] = 64 // TTL
    reply[23] = 17 // protocol UDP
    reply[26...29] = hostIP[0...3] // src = our address
    reply[30...33] = req[26...29] // dst = the sender's address
    let hdrChk = ipChecksum(reply, 14, 34)
    reply[24] = UInt8(hdrChk >> 8)
    reply[25] = UInt8(hdrChk & 0xff)
    // UDP: src = the host port, dst = the sender's src port, length
    // echoed, the payload echoed byte-exact; checksum recomputed.
    reply[34] = UInt8(hostPort >> 8)
    reply[35] = UInt8(hostPort & 0xff)
    reply[36] = req[34] // the sender's src port
    reply[37] = req[35]
    reply[38...39] = req[38...39] // UDP length (unchanged)
    let udpLen = (UInt16(req[38]) << 8) | UInt16(req[39])
    let payloadStart = 42
    if n > payloadStart {
        reply[payloadStart...n - 1] = req[payloadStart...n - 1] // payload echoed
    }
    let senderIP = [UInt8](req[26...29])
    var datagram = [UInt8](reply[34..<n])
    datagram[6] = 0
    datagram[7] = 0 // zero the checksum field during the computation
    let udpChk = udpChecksum(hostIP, senderIP, datagram, udpLen)
    reply[40] = UInt8(udpChk >> 8)
    reply[41] = UInt8(udpChk & 0xff)
}

// M117c (#2475): is the datagram a UDP/IPv6 datagram addressed to
// `addr:port`? Ethernet II ethertype 0x86dd, version 6, next-header 17,
// the 16-byte dst address match, the dst port match. UDP over v6 is
// at frame 54 (14-byte Ethernet + the 40-byte fixed header).
@Sendable func isUdp6Datagram(_ buf: [UInt8], _ n: Int, _ addr: [UInt8], _ port: UInt16) -> Bool {
    guard n >= 62 else { return false }
    guard buf[12] == 0x86 && buf[13] == 0xdd else { return false } // ethertype IPv6
    guard buf[14] >> 4 == 0x6 else { return false } // version 6
    guard buf[20] == 17 else { return false } // next header UDP
    guard [UInt8](buf[38..<54]) == addr else { return false } // dst address
    let dstPort = (UInt16(buf[56]) << 8) | UInt16(buf[57])
    return dstPort == port
}

// M117c (#2475): synthesize the UDP/IPv6 REPLY to the datagram in `req`
// (the guest's bytes) into `reply` (same length): Ethernet dst/src
// swapped, ethertype 0x86dd, v6 src/dst swapped, payload_len echoed,
// hop 64, UDP src port = the host port, dst port = the sender's src
// port, the payload ECHOED byte-exact; the UDP pseudo-header checksum
// recomputed over the v6 addresses (icmp6Checksum is the generic
// 16-byte pseudo-header — nh 17 here).
@Sendable func buildUdp6Reply(_ reply: inout [UInt8], _ req: [UInt8], _ n: Int, _ hostMAC: [UInt8], _ addr: [UInt8], _ port: UInt16) {
    reply = [UInt8](repeating: 0, count: n)
    reply[0...5] = req[6...11] // dst = the sender's MAC
    reply[6...11] = hostMAC[0...5] // src
    reply[12] = 0x86
    reply[13] = 0xdd // ethertype IPv6
    reply[14] = 0x60 // version 6
    reply[18...19] = req[18...19] // payload_len (unchanged)
    reply[20] = 17 // next header UDP
    reply[21] = 64 // hop limit
    reply[22...37] = addr[0...15] // src = the addressed address
    reply[38...53] = req[22...37] // dst = the sender's address
    reply[54] = UInt8(port >> 8)
    reply[55] = UInt8(port & 0xff)
    reply[56] = req[54] // the sender's src port
    reply[57] = req[55]
    reply[58...59] = req[58...59] // UDP length (unchanged)
    if n > 62 {
        reply[62...n - 1] = req[62...n - 1] // payload echoed
    }
    let dst = [UInt8](reply[38..<54])
    var datagram = [UInt8](reply[54..<n])
    datagram[6] = 0
    datagram[7] = 0 // zero the checksum field during the computation
    let udpChk = icmp6Checksum(addr, dst, datagram, 17)
    reply[60] = UInt8(udpChk >> 8)
    reply[61] = UInt8(udpChk & 0xff)
}

// M99i (issue #2187): hex helper for --net-dns-answer6.
func parseHexBytes(_ hex: String) -> [UInt8]? {
    guard hex.count % 2 == 0 else { return nil }
    var out: [UInt8] = []
    out.reserveCapacity(hex.count / 2)
    var i = hex.startIndex
    while i < hex.endIndex {
        let j = hex.index(i, offsetBy: 2)
        guard let b = UInt8(hex[i..<j], radix: 16) else { return nil }
        out.append(b)
        i = j
    }
    return out
}

// Milestone twelve card N2 (claim 7566): DNS packet matching & response synthesis
func isDnsQuery(_ buf: [UInt8], _ n: Int, _ hostIP: [UInt8], _ hostPort: UInt16) -> Bool {
    guard n >= 42 + 12 + 5 else { return false }
    guard buf[12] == 0x08 && buf[13] == 0x00 else { return false } // IPv4
    guard buf[14] == 0x45 else { return false } // IHL 5
    guard (buf[20] & 0x1f) == 0 && buf[21] == 0 else { return false } // not a fragment
    guard buf[23] == 17 else { return false } // UDP
    guard buf[30] == hostIP[0] && buf[31] == hostIP[1] && buf[32] == hostIP[2] && buf[33] == hostIP[3] else { return false }
    let dstPort = (UInt16(buf[36]) << 8) | UInt16(buf[37])
    guard dstPort == hostPort else { return false }
    let flags = (UInt16(buf[44]) << 8) | UInt16(buf[45])
    return (flags & 0x8000) == 0 // QR = 0 (query)
}

func extractDnsQName(_ buf: [UInt8], _ n: Int) -> String {
    var off = 42 + 12
    var labels: [String] = []
    while off < n {
        let len = Int(buf[off])
        if len == 0 { break }
        if (len & 0xC0) != 0 { break }
        off += 1
        if off + len > n { break }
        let s = String(decoding: buf[off..<off+len], as: UTF8.self)
        labels.append(s)
        off += len
    }
    return labels.joined(separator: ".")
}

// M99i (issue #2187): the QTYPE the request's question section asks for
// (offset 42+12 qname, then QTYPE/QCLASS).
func dnsQueryType(_ req: [UInt8], _ n: Int) -> UInt16 {
    var off = 42 + 12
    while off < n {
        let len = Int(req[off])
        if len == 0 { off += 1; break }
        if (len & 0xC0) != 0 { off += 2; break }
        off += 1 + len
    }
    guard off + 2 <= n else { return 1 }
    return (UInt16(req[off]) << 8) | UInt16(req[off + 1])
}

// M99i: the answer set for one query. A queries take the configured A
// records (or the legacy qname table, one answer — the pre-M99i
// behavior); AAAA queries take the configured AAAA records (or one fixed
// 2001:db8::53).
func dnsAnswersFor(_ qtype: UInt16, _ qname: String) -> [(rtype: UInt16, rdata: [UInt8])] {
    if qtype == 28 {
        if !netDnsAnswers6.isEmpty {
            return netDnsAnswers6.map { (28, $0) }
        }
        return [(28, [0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x53])]
    }
    if !netDnsAnswers.isEmpty {
        return netDnsAnswers.map { (1, $0) }
    }
    var resolvedIP: [UInt8] = [93, 184, 216, 34]
    if qname == "myhost.local" || qname == "gateway.local" {
        resolvedIP = [10, 0, 0, 2]
    } else if qname == "virelai.local" {
        resolvedIP = [10, 0, 0, 1]
    }
    return [(1, resolvedIP)]
}

// M99i: builds the full Ethernet reply. `answers` is the record set,
// `ttl` is written into each, `truncate` answers TC=1/ANCOUNT=0,
// `forgeSrcIP` replaces the packet's source IPv4 (the spoof knob), and
// `corruptQuestion` flips the echoed question's first label byte so the
// guest's byte-exact question check rejects it.
func buildDnsReply(_ reply: inout [UInt8], _ req: [UInt8], _ n: Int, _ hostMAC: [UInt8], _ hostIP: [UInt8], _ hostPort: UInt16, answers: [(rtype: UInt16, rdata: [UInt8])], ttl: UInt32, truncate: Bool, forgeSrcIP: [UInt8]? = nil, corruptQuestion: Bool = false) -> Int {
    var off = 42 + 12
    while off < n {
        let len = Int(req[off])
        if len == 0 { off += 1; break }
        if (len & 0xC0) != 0 { off += 2; break }
        off += 1 + len
    }
    off += 4
    if off > n { return 0 }
    let questionLen = off - (42 + 12)

    let ancount = truncate ? 0 : answers.count
    var answersLen = 0
    for a in answers { answersLen += 12 + a.rdata.count }

    var dnsPayload = [UInt8](repeating: 0, count: 12 + questionLen + answersLen)
    dnsPayload[0] = req[42]
    dnsPayload[1] = req[43]
    dnsPayload[2] = truncate ? 0x83 : 0x81
    dnsPayload[3] = 0x80
    dnsPayload[4] = 0x00
    dnsPayload[5] = 0x01
    dnsPayload[6] = UInt8(ancount >> 8)
    dnsPayload[7] = UInt8(ancount & 0xff)
    dnsPayload[8] = 0x00
    dnsPayload[9] = 0x00
    dnsPayload[10] = 0x00
    dnsPayload[11] = 0x00

    dnsPayload[12..<(12 + questionLen)] = req[(42 + 12)..<off]
    if corruptQuestion, questionLen > 2 {
        // Flip the case of the first label's first byte — DNS name
        // comparison is case-insensitive on paper, but the guest's
        // byte-exact echo check must refuse it.
        dnsPayload[13] ^= 0x20
    }

    var aoff = 12 + questionLen
    for a in answers {
        dnsPayload[aoff + 0] = 0xC0
        dnsPayload[aoff + 1] = 0x0C
        dnsPayload[aoff + 2] = UInt8(a.rtype >> 8)
        dnsPayload[aoff + 3] = UInt8(a.rtype & 0xff)
        dnsPayload[aoff + 4] = 0x00
        dnsPayload[aoff + 5] = 0x01
        dnsPayload[aoff + 6] = UInt8(ttl >> 24)
        dnsPayload[aoff + 7] = UInt8((ttl >> 16) & 0xff)
        dnsPayload[aoff + 8] = UInt8((ttl >> 8) & 0xff)
        dnsPayload[aoff + 9] = UInt8(ttl & 0xff)
        dnsPayload[aoff + 10] = UInt8(a.rdata.count >> 8)
        dnsPayload[aoff + 11] = UInt8(a.rdata.count & 0xff)
        for (i, b) in a.rdata.enumerated() {
            dnsPayload[aoff + 12 + i] = b
        }
        aoff += 12 + a.rdata.count
    }

    let srcIP = forgeSrcIP ?? hostIP
    let totalLen = 42 + dnsPayload.count
    reply = [UInt8](repeating: 0, count: totalLen)
    reply[0...5] = req[6...11]
    reply[6...11] = hostMAC[0...5]
    reply[12] = 0x08
    reply[13] = 0x00
    reply[14] = 0x45
    let ipTotalLen = UInt16(20 + 8 + dnsPayload.count)
    reply[16] = UInt8(ipTotalLen >> 8)
    reply[17] = UInt8(ipTotalLen & 0xff)
    reply[18...19] = req[18...19]
    reply[22] = 64
    reply[23] = 17
    reply[26...29] = srcIP[0...3]
    reply[30...33] = req[26...29]
    let hdrChk = ipChecksum(reply, 14, 34)
    reply[24] = UInt8(hdrChk >> 8)
    reply[25] = UInt8(hdrChk & 0xff)

    reply[34] = UInt8(hostPort >> 8)
    reply[35] = UInt8(hostPort & 0xff)
    reply[36] = req[34]
    reply[37] = req[35]
    let udpLen = UInt16(8 + dnsPayload.count)
    reply[38] = UInt8(udpLen >> 8)
    reply[39] = UInt8(udpLen & 0xff)
    reply[42..<totalLen] = dnsPayload[0..<dnsPayload.count]

    let senderIP = [UInt8](req[26...29])
    var datagram = [UInt8](reply[34..<totalLen])
    datagram[6] = 0
    datagram[7] = 0
    let udpChk = udpChecksum(srcIP, senderIP, datagram, udpLen)
    reply[40] = UInt8(udpChk >> 8)
    reply[41] = UInt8(udpChk & 0xff)

    return totalLen
}

// Card N8 (claim 0351): is the datagram a DHCP client message (Ethernet
// II ethertype 0x0800, version 4 / IHL 5, NOT a fragment, protocol UDP,
// src port 68 -> dst port 67)? The guest's DISCOVER/REQUEST frames are
// raw Ethernet — header at 14..34, UDP at 34..42, the DHCP message at 42.
func isDhcpDatagram(_ buf: [UInt8], _ n: Int) -> Bool {
    guard n >= 46 else { return false }
    guard buf[12] == 0x08 && buf[13] == 0x00 else { return false } // ethertype IPv4
    guard buf[14] == 0x45 else { return false } // version 4, IHL 5
    guard (buf[20] & 0x1f) == 0 && buf[21] == 0 else { return false } // NOT a fragment
    guard buf[23] == 17 else { return false } // protocol UDP
    let srcPort = (UInt16(buf[34]) << 8) | UInt16(buf[35])
    let dstPort = (UInt16(buf[36]) << 8) | UInt16(buf[37])
    return srcPort == 68 && dstPort == 67
}

// Card N8 (claim 0351): the DHCP message type (option 53) of a client
// message, or nil when absent/malformed. Options start at frame 282 (42 +
// the 236-byte BOOTP header + the 4-byte magic cookie).
func dhcpMessageType(_ buf: [UInt8], _ n: Int) -> UInt8? {
    guard n >= 284 else { return nil }
    var i = 282
    while i + 2 <= n {
        let code = buf[i]
        if code == 255 { break }
        if code == 0 { i += 1; continue }
        let len = Int(buf[i + 1])
        if i + 2 + len > n { break }
        if code == 53 && len == 1 { return buf[i + 2] }
        i += 2 + len
    }
    return nil
}

// Card N8 (claim 0351): synthesize the DHCP OFFER (mtype 1 -> 2) or ACK
// (mtype 3 -> 5) to the client message in `req` into `reply` (a fresh
// broadcast frame): Ethernet dst ff*6 / src host MAC, ethertype 0x0800,
// IPv4 src = the lease IP / dst 255.255.255.255 (the client's broadcast
// flag), protocol 17, then the BOOTREPLY (op 2, the guest's xid ECHOED,
// yiaddr = the lease IP, the client's chaddr echoed) with the FIXED
// gate-assertable lease options: 53 (type), 1 (mask 255.255.255.0), 3
// (gateway 10.0.0.1), 54 (server id = the lease IP), 51 (lease 3600),
// 255. Both checksums recomputed (RFC 1071). Returns the frame length
// (310 bytes — the reply message is 268 bytes).
func buildDhcpReply(_ reply: inout [UInt8], _ req: [UInt8], _ n: Int, _ hostMAC: [UInt8], _ leaseIP: [UInt8], _ leaseSecs: UInt32, _ mtype: UInt8) -> Int {
    _ = n
    // The reply message: 236 header + 4 cookie + 53,1,type(3) +
    // 1,4,mask(6) + 3,4,gw(6) + 54,4,server(6) + 51,4,lease(6) + 255(1)
    // = 268 bytes; the frame = 42 + 268 = 310.
    let msgLen = 268
    let frameLen = 42 + msgLen // 310
    reply = [UInt8](repeating: 0, count: frameLen)
    // Ethernet: dst broadcast, src host MAC.
    reply[0...5] = [UInt8](repeating: 0xff, count: 6)[0...5]
    reply[6...11] = hostMAC[0...5]
    reply[12] = 0x08
    reply[13] = 0x00 // ethertype IPv4
    // IPv4: src = the lease IP, dst = 255.255.255.255, proto 17.
    reply[14] = 0x45 // version 4, IHL 5
    reply[16] = UInt8((20 + 8 + msgLen) >> 8)
    reply[17] = UInt8((20 + 8 + msgLen) & 0xff) // total length 296
    reply[22] = 64 // TTL
    reply[23] = 17 // protocol UDP
    reply[26...29] = leaseIP[0...3] // src
    reply[30...33] = [UInt8](repeating: 0xff, count: 4)[0...3] // dst broadcast
    let hdrChk = ipChecksum(reply, 14, 34)
    reply[24] = UInt8(hdrChk >> 8)
    reply[25] = UInt8(hdrChk & 0xff)
    // UDP: src 67, dst 68, length 8 + msgLen.
    reply[34] = 0x00
    reply[35] = 67 // src port 67
    reply[36] = 0x00
    reply[37] = 68 // dst port 68
    reply[38] = UInt8((8 + msgLen) >> 8)
    reply[39] = UInt8((8 + msgLen) & 0xff) // UDP length 276
    // The DHCP message at 42: op BOOTREPLY, htype/hlen, the guest's xid
    // echoed, yiaddr = the lease IP, the client's chaddr echoed.
    reply[42] = 2 // BOOTREPLY
    reply[43] = 1 // htype ethernet
    reply[44] = 6 // hlen
    reply[46...49] = req[46...49] // xid echoed byte-exact
    reply[58...61] = leaseIP[0...3] // yiaddr
    reply[70...85] = req[70...85] // chaddr echoed (the client's MAC)
    // The magic cookie + the fixed lease options.
    reply[278] = 0x63
    reply[279] = 0x82
    reply[280] = 0x53
    reply[281] = 0x63
    var o = 282
    // The reply's message type is the SERVER's answer to the client's
    // message (option 53 must be 2 OFFER for a DISCOVER, 5 ACK for a
    // REQUEST — never the echoed client type).
    let replyType: UInt8 = mtype == 1 ? 2 : 5
    reply[o] = 53; reply[o + 1] = 1; reply[o + 2] = replyType; o += 3 // message type
    reply[o] = 1; reply[o + 1] = 4; reply[o + 2] = 255; reply[o + 3] = 255; reply[o + 4] = 255; reply[o + 5] = 0; o += 6 // subnet mask
    reply[o] = 3; reply[o + 1] = 4; reply[o + 2] = 10; reply[o + 3] = 0; reply[o + 4] = 0; reply[o + 5] = 1; o += 6 // gateway
    reply[o] = 54; reply[o + 1] = 4; reply[o + 2...o + 5] = leaseIP[0...3]; o += 6 // server id
    reply[o] = 51; reply[o + 1] = 4; reply[o + 2] = UInt8(leaseSecs >> 24); reply[o + 3] = UInt8((leaseSecs >> 16) & 0xff); reply[o + 4] = UInt8((leaseSecs >> 8) & 0xff); reply[o + 5] = UInt8(leaseSecs & 0xff); o += 6 // lease option 51
    reply[o] = 255; o += 1 // end
    // o must be exactly frameLen (282 + 28 = 310).
    assert(o == frameLen)
    // The UDP checksum over the pseudo-header (lease IP -> broadcast).
    var datagram = [UInt8](reply[34..<frameLen])
    datagram[6] = 0
    datagram[7] = 0 // zero the checksum field during the computation
    let udpChk = udpChecksum(leaseIP, [UInt8](repeating: 0xff, count: 4), datagram, UInt16(8 + msgLen))
    reply[40] = UInt8(udpChk >> 8)
    reply[41] = UInt8(udpChk & 0xff)
    return frameLen
}

// M51 SSH1 (#1168): pace one payload chunk out in `--net-tcp-respond :packet`
// mode. Called once per accepted guest segment; `guestSeq`/`guestPayload` are
// the incoming segment's seq and payload length (so our ACK covers it). Sends
// at most one ≤chunk segment per call — the guest's one-slot RX cannot take
// two back-to-back, and the host has no retransmission. Returns true when a
// chunk was sent.
func netTcpSendPacketChunk(_ conn: NetTcpSrvConn, _ reply: inout [UInt8], _ buf: [UInt8], _ n: Int, _ hostMAC: [UInt8], _ hostPort: UInt16, _ guestSeq: UInt32, _ guestPayload: Int) -> Bool {
    guard netTcpRespondPacketMode else { return false }
    let total = netTcpRespondPacketPayload.count
    let off = conn.packetIndex * netTcpRespondPacketChunk
    guard off < total else { return false }
    let end = min(off + netTcpRespondPacketChunk, total)
    let chunk = Array(netTcpRespondPacketPayload[off..<end])
    let chunkCount = (total + netTcpRespondPacketChunk - 1) / netTcpRespondPacketChunk
    let replyLen = buildTcpReply(&reply, buf, n, hostMAC, hostPort, conn.srvNxt, guestSeq &+ UInt32(guestPayload), 0x10, chunk)
    try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
    conn.srvNxt = conn.srvNxt &+ UInt32(chunk.count)
    conn.packetIndex += 1
    print("NET-TCP: packet mode sent chunk \(conn.packetIndex)/\(chunkCount) (\(chunk.count) bytes, seq 0x\(hex32(conn.srvNxt &- UInt32(chunk.count))))")
    return true
}

// M51 SSH5 (#1172): pace one ≤192-byte segment of the SSH server's output
// stream out in `--net-tcp-respond :ssh` mode. Called once per accepted
// guest segment; `guestSeq`/`guestPayload` are the incoming segment's seq
// and payload length (so our ACK covers it). Same contract as
// `netTcpSendPacketChunk`: the guest kernel RX is a single bounded slot
// with no reassembly and the host has no retransmission, so at most one
// segment goes out per guest ACK. Returns true when a chunk was sent.
func netTcpRespondSSHSendChunk(_ conn: NetTcpSrvConn, _ reply: inout [UInt8], _ buf: [UInt8], _ n: Int, _ hostMAC: [UInt8], _ hostPort: UInt16, _ guestSeq: UInt32, _ guestPayload: Int) -> Bool {
    guard netTcpRespondSSHMode else { return false }
    let total = conn.sshTx.count
    let off = conn.sshTxOffset
    guard off < total else { return false }
    let end = min(off + netTcpRespondPacketChunk, total)
    let chunk = Array(conn.sshTx[off..<end])
    let replyLen = buildTcpReply(&reply, buf, n, hostMAC, hostPort, conn.srvNxt, guestSeq &+ UInt32(guestPayload), 0x10, chunk)
    try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
    conn.srvNxt = conn.srvNxt &+ UInt32(chunk.count)
    conn.sshTxOffset = end
    print("NET-TCP-SSH: sent \(chunk.count) bytes (\(end)/\(total), seq 0x\(hex32(conn.srvNxt &- UInt32(chunk.count))))")
    if end >= total {
        conn.sshTx.removeAll(keepingCapacity: true)
        conn.sshTxOffset = 0
    }
    return true
}

// M99f (#2184): the host-initiated client state machine, one instance per
// `--net-tcp-connect` flag. Everything that was a scalar global is a field
// on the client, so two live clients never share sequence/ACK/stream
// state — a segment's 5-tuple picks its client before `drive` runs.
extension NetTcpClient {
    /// One guest segment for THIS client: the handshake / payload / SSH /
    /// stream / close transitions that used to live in the capture loop's
    /// scalar switch.
    func drive(_ buf: [UInt8], _ n: Int) {
        let flags = tcpFlags(buf)
        let seq = tcpSeq(buf)
        if n > 49 { peerWindow = UInt32(tcpWindow(buf)) }
        var payload = [UInt8]()
        if n > 54 { payload = [UInt8](buf[54..<n]) }
        let isSynAck = (flags & 0x02) != 0 && (flags & 0x10) != 0
        let isFin = (flags & 0x01) != 0
        let isRst = (flags & 0x04) != 0
        var reply = [UInt8](repeating: 0, count: 4096)
        let guestIP = self.guestIP
        let guestPort = self.guestPort
        let cliPort = self.cliPort
        switch state {
        case 1:
            // SYN sent — expect the guest's SYN-ACK.
            if isSynAck {
                ack = seq &+ 1
                self.seq = netTcpCliIsn &+ 1
                if isStreamClient {
                    let replyLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x10, [])
                    try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
                    state = 2
                    startStream()
                    print("NET-TCP-STREAM: handshake complete; probe launched")
                } else if sshMode {
                    let cfg = SSHClient.Config(
                        userKeySeed: netTcpConnectSSHUserSeed,
                        hostPublicKey: netTcpConnectSSHHostPin,
                        user: netTcpConnectSSHUser,
                        execCommand: netTcpConnectSSHExec,
                        pty: netTcpConnectSSHPty,
                        keyScript: netTcpConnectSSHKeys
                    )
                    let client = SSHClient(config: cfg)
                    client.logSink = { line in print("SSH-CLI: \(line)") }
                    sshClient = client
                    sshTx = client.start()
                    sshTxOffset = 0
                    sshGuestDataSeen = false
                    state = 2
                    if !sshSendChunk() {
                        let replyLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x10, [])
                        try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
                    }
                    print("NET-TCP-CONNECT: handshake complete (server ISN 0x\(hex32(seq)), ack 0x\(hex32(ack))); SSH client started")
                } else {
                // M50 TS4 (#1138, ADR 0024 D6): in auth mode the
                // guest mints + sends its challenge once the
                // connection is ESTABLISHED, so the handshake ACK
                // must be bare; the payload follows the MAC.
                let authPending = netTcpConnectSecret != nil || netTcpConnectMac != nil
                let firstPayload = authPending ? [UInt8]() : self.payload
                let replyLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x10, firstPayload)
                try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
                self.seq = self.seq &+ UInt32(firstPayload.count)
                state = authPending ? 5 : 2
                if authPending {
                    print("NET-TCP-CONNECT: handshake complete (server ISN 0x\(hex32(seq)), ack 0x\(hex32(ack))); awaiting the VIRELAIOS-AUTH/1 challenge")
                } else {
                    print("NET-TCP-CONNECT: handshake complete (server ISN 0x\(hex32(seq)), ack 0x\(hex32(ack))); sent \(self.payload.count)-byte payload")
                }
                }
            } else if isRst {
                state = 3
                print("NET-TCP-CONNECT: the guest refused the connection (RST)")
            }
        case 5:
            // M50 TS4: awaiting the guest's `VIRELAIOS-AUTH/1` line.
            if isRst {
                state = 3
                print("NET-TCP-CONNECT: the guest reset the connection (RST)")
            } else if isFin {
                ack = seq &+ 1
                let replyLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x10, [])
                try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
                state = 3
                print("NET-TCP-CONNECT: the guest sent FIN; ACKed and closed")
            } else if !payload.isEmpty {
                var text = ""
                for b in payload { text.append((b >= 0x20 && b < 0x7f) ? Character(UnicodeScalar(b)) : ".") }
                recvText.append(text)
                print("NET-TCP-CONNECT: received \(payload.count) byte(s): \(text)")
                ack = seq &+ UInt32(payload.count)
                if let answer = netTcpAuthAnswer(challenge: payload) {
                    let combined = answer + self.payload
                    let replyLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x10, combined)
                    try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
                    self.seq = self.seq &+ UInt32(combined.count)
                    state = 2
                    print("NET-TCP-CONNECT: answered the challenge (\(answer.count) bytes) with the \(netTcpConnectMac != nil ? "recorded replay MAC" : "computed HMAC"); sent \(self.payload.count)-byte payload")
                } else {
                    let replyLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x10, [])
                    try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
                    state = 3
                    print("NET-TCP-CONNECT: could not answer the challenge (malformed / no HMAC support); closing")
                }
            }
        case 2:
            // ESTABLISHED — the guest shell's data / a FIN.
            if isRst {
                if isStreamClient { try? streamInput?.close() }
                state = 3
                print("NET-TCP-CONNECT: the guest reset the connection (RST)")
            } else if isFin {
                if isStreamClient { try? streamInput?.close() }
                ack = seq &+ 1
                let replyLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x10, [])
                try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
                state = 3
                print("NET-TCP-CONNECT: the guest sent FIN; ACKed and closed")
            } else if isStreamClient {
                netTcpStreamLock.lock()
                if !payload.isEmpty {
                    if netTcpConnectIntruder && !intruderSent {
                        // The guest is ESTABLISHED and serving: now a
                        // second viewer knocks from the next port.
                        intruderSent = true
                        let synLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort &+ 1, guestPort, netTcpIntruderIsn, 0, 0x02, [])
                        try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<synLen]))
                        print("NET-TCP-INTRUDER: sent a second SYN from port \(cliPort &+ 1) while the first viewer is connected")
                    }
                    let fresh = streamAccept(seq, payload)
                    if !fresh.isEmpty {
                        streamRxBytes += fresh.count
                        if streamRxBytes <= netTcpStreamMaxRx {
                            // nil once the probe exited: a dead viewer reads nothing.
                            try? streamInput?.write(contentsOf: Data(fresh))
                        } else {
                            streamDone = true
                            print("NET-TCP-STREAM: inbound byte bound exceeded")
                        }
                    }
                }
                if (flags & 0x10) != 0 && tcpAck(buf) == self.seq {
                    streamInFlight = false
                }
                if !payload.isEmpty || isSynAck {
                    let replyLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x10, [])
                    try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
                }
                streamPump()
                netTcpStreamLock.unlock()
            } else if sshMode {
                if (flags & 0x02) != 0 {
                    // SYN-ACK retransmit after ESTABLISHED. Re-ACK
                    // only — another SSH chunk would overflow RX.
                    let replyLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x10, [])
                    try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
                    print("NET-TCP-CONNECT-SSH: re-ACK SYN-ACK retransmit")
                } else {
                    var fresh: [UInt8] = []
                    if !payload.isEmpty {
                        fresh = sshAccept(seq, payload)
                        if !fresh.isEmpty {
                            sshGuestDataSeen = true
                            if let client = sshClient {
                                sshTx.append(contentsOf: client.feed(fresh))
                                // M168f (#2962): spill CHANNEL_DATA into
                                // the session capture as it lands.
                                if let path = netTcpConnectSSHSessionFile,
                                   client.stdout.count > sshSessionWritten {
                                    netTcpAppendFile(
                                        path,
                                        Array(client.stdout[sshSessionWritten...])
                                    )
                                    sshSessionWritten = client.stdout.count
                                }
                            }
                        }
                    }
                    // Pace further chunks only after the guest has
                    // Recv'd (it then emits SSH bytes). Bare ACKs
                    // of the handshake chunk land while RX is full.
                    if sshGuestDataSeen {
                        if !sshSendChunk() {
                            if !payload.isEmpty {
                                let replyLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x10, [])
                                try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
                            }
                        }
                    } else if !payload.isEmpty {
                        let replyLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x10, [])
                        try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
                    }
                    if let client = sshClient, client.isClosed, sshTx.isEmpty {
                        let finLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x11, [])
                        try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<finLen]))
                        self.seq = self.seq &+ 1
                        state = 4
                        print("NET-TCP-CONNECT-SSH: session done; sent FIN")
                    }
                }
            } else if !payload.isEmpty {
                ack = seq &+ UInt32(payload.count)
                var text = ""
                for b in payload { text.append((b >= 0x20 && b < 0x7f) ? Character(UnicodeScalar(b)) : ".") }
                recvText.append(text)
                print("NET-TCP-CONNECT: received \(payload.count) byte(s): \(text)")
                let replyLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x10, [])
                try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
                // Close once the reply shows the command ran (the
                // configure marker) or, absent one, after the first
                // data segment.
                let shouldClose = netTcpConnectCloseAfter.map { recvText.contains($0) } ?? true
                if shouldClose {
                    let finLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, self.seq, ack, 0x11, [])
                    try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<finLen]))
                    self.seq = self.seq &+ 1
                    state = 4
                    print("NET-TCP-CONNECT: sent FIN (closing the session)")
                }
            }
        case 4:
            // FIN sent — expect the guest's ACK.
            if (flags & 0x10) != 0 {
                state = 3
                print("NET-TCP-CONNECT: close acknowledged (session done)")
            }
        default:
            break
        }
    }

    // Advance ack over in-order guest payload and return the new bytes.
    // Retransmits and out-of-order segments are dropped: feeding them
    // into SSHClient concatenates a second copy of the identification
    // line onto a partial KEXINIT and the client fails closed (observed
    // on the first live-ssh-server run as "malformed KEXINIT").
    func sshAccept(_ seq: UInt32, _ payload: [UInt8]) -> [UInt8] {
        if payload.isEmpty { return [] }
        let delta = Int32(bitPattern: seq &- ack)
        if delta > 0 {
            print("NET-TCP-CONNECT-SSH: dropped out-of-order seq 0x\(hex32(seq)) want 0x\(hex32(ack))")
            return []
        }
        let skip = Int(ack &- seq)
        if skip >= payload.count {
            print("NET-TCP-CONNECT-SSH: ignored retransmit seq 0x\(hex32(seq)) (acked through 0x\(hex32(ack)))")
            return []
        }
        let fresh = skip == 0 ? payload : Array(payload[skip...])
        ack = ack &+ UInt32(fresh.count)
        print("NET-TCP-CONNECT-SSH: received \(fresh.count) byte(s)")
        return fresh
    }

    // M70g G1 (#1491): pace one ≤192-byte segment of the SSH client's
    // output toward the guest. Same contract as
    // netTcpRespondSSHSendChunk: the guest kernel RX is a single bounded
    // slot with no reassembly.
    func sshSendChunk() -> Bool {
        guard sshMode, peerWindow != 0 else { return false }
        let total = sshTx.count
        let off = sshTxOffset
        guard off < total else { return false }
        let end = min(off + netTcpRespondPacketChunk, total)
        let chunk = Array(sshTx[off..<end])
        var reply = [UInt8](repeating: 0, count: 4096)
        let replyLen = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0x00, 0x00, 0x00, 0x00, 0x02], netTcpCliSrcIP, guestIP, cliPort, guestPort, seq, ack, 0x10, chunk)
        try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<replyLen]))
        seq = seq &+ UInt32(chunk.count)
        sshTxOffset = end
        print("NET-TCP-CONNECT-SSH: sent \(chunk.count) bytes (\(end)/\(total), seq 0x\(hex32(seq &- UInt32(chunk.count))))")
        if end >= total {
            sshTx.removeAll(keepingCapacity: true)
            sshTxOffset = 0
        }
        return true
    }

    // Runs only after a guest SYN-ACK, on the hermetic test path. Child
    // stdout is binary RFB, never printed as text; stderr is diagnostic
    // only. One reader drains stdout, and the network capture thread
    // writes stdin.
    func startStream() {
        guard let path = netTcpConnectStreamExec else { return }
        let proc = Process()
        let stdin = Pipe(), stdout = Pipe()
        proc.executableURL = URL(fileURLWithPath: path)
        proc.arguments = netTcpConnectStreamArgs
        proc.standardInput = stdin
        proc.standardOutput = stdout
        proc.standardError = FileHandle.standardError
        // A viewer that dies mid-update leaves guest bytes in flight;
        // writing them to its closed pipe must be an error, not a SIGPIPE
        // to the runner.
        _ = fcntl(stdin.fileHandleForWriting.fileDescriptor, F_SETNOSIGPIPE, 1)
        do {
            try proc.run()
        } catch {
            streamDone = true
            print("NET-TCP-STREAM: probe launch failed: \(error)")
            return
        }
        streamProcess = proc
        streamInput = stdin.fileHandleForWriting
        DispatchQueue.global().async { [weak self] in
            var buf = [UInt8](repeating: 0, count: 192)
            while true {
                // FileHandle.readData(ofLength:) waits for the FULL
                // requested length on a pipe. RFB's 12-byte banner reply
                // would not reach the guest until EOF. POSIX read returns
                // each available chunk.
                let n = Darwin.read(stdout.fileHandleForReading.fileDescriptor, &buf, buf.count)
                if n < 0 && errno == EINTR { continue }
                if n <= 0 { break }
                guard let self else { break }
                let bytes = Array(buf[..<n])
                netTcpStreamLock.lock()
                if self.streamTx.count + bytes.count > netTcpStreamMaxTx {
                    self.streamDone = true
                    netTcpStreamLock.unlock()
                    print("NET-TCP-STREAM: outbound byte bound exceeded")
                    break
                }
                self.streamTx.append(contentsOf: bytes)
                self.streamPump()
                netTcpStreamLock.unlock()
            }
            proc.waitUntilExit()
            let status = proc.terminationStatus
            print("NET-TCP-STREAM: probe exited status=\(status)")
            if let self {
                if self.intruderSent && !self.intruderAnswered {
                    print("NET-TCP-INTRUDER: no answer to the second SYN (dropped without RST)")
                }
                // The viewer's exit is its close: a clean exit is a FIN
                // after its last byte is ACKed, any other exit is a peer
                // that vanished (RST).
                netTcpStreamLock.lock()
                try? self.streamInput?.close()
                self.streamInput = nil
                if status == 0 && !self.streamDone {
                    self.streamEOF = true
                    self.streamPump()
                } else {
                    self.streamDone = true
                    self.streamReset()
                }
                netTcpStreamLock.unlock()
            }
        }
    }

    // Called under streamLock. Only an ESTABLISHED stream is reset: once
    // the guest has closed first there is nothing left to tear down.
    func streamReset() {
        guard state == 2 else { return }
        var rst: [UInt8] = []
        let len = buildTcpFrameExplicit(&rst, netTcpGuestMAC, [0x02, 0, 0, 0, 0, 0x02],
            netTcpCliSrcIP, guestIP, cliPort, guestPort, seq, ack, 0x14, [])
        try? netCaptureReadSocket?.write(contentsOf: Data(rst[0..<len]))
        state = 3
        print("NET-TCP-STREAM: the viewer died; sent RST")
    }

    // In-order, retransmit-safe byte delivery to the probe. The kernel
    // only has one RX segment slot, so the reverse path likewise sends
    // one <=192-byte chunk and waits for its ACK before advancing.
    // Called under streamLock.
    func streamAccept(_ seq: UInt32, _ payload: [UInt8]) -> [UInt8] {
        let delta = Int32(bitPattern: seq &- ack)
        if delta > 0 { return [] }
        let skip = Int(ack &- seq)
        if skip >= payload.count { return [] }
        let fresh = Array(payload[skip...])
        ack = ack &+ UInt32(fresh.count)
        return fresh
    }

    func streamPump() {
        if streamEOF && streamTx.isEmpty && !streamInFlight && !streamDone {
            streamDone = true
            guard state == 2 else { return }
            var fin: [UInt8] = []
            let len = buildTcpFrameExplicit(&fin, netTcpGuestMAC, [0x02, 0, 0, 0, 0, 0x02],
                netTcpCliSrcIP, guestIP, cliPort, guestPort, seq, ack, 0x11, [])
            try? netCaptureReadSocket?.write(contentsOf: Data(fin[0..<len]))
            seq = seq &+ 1
            state = 4
            print("NET-TCP-STREAM: the viewer closed; sent FIN")
            return
        }
        guard !streamInFlight && !streamTx.isEmpty && !streamDone && peerWindow != 0 else { return }
        let count = min(192, streamTx.count)
        let chunk = Array(streamTx.prefix(count))
        streamTx.removeFirst(count)
        var reply: [UInt8] = []
        let len = buildTcpFrameExplicit(&reply, netTcpGuestMAC, [0x02, 0, 0, 0, 0, 0x02],
            netTcpCliSrcIP, guestIP, cliPort, guestPort, seq, ack, 0x10, chunk)
        do {
            try? netCaptureReadSocket?.write(contentsOf: Data(reply[0..<len]))
            seq = seq &+ UInt32(count)
            streamInFlight = true
        } catch {
            streamDone = true
            print("NET-TCP-STREAM: transmit failed: \(error)")
        }
    }
}

// Card N4 (claim 0148): synthesize the ICMP ECHO REPLY to the echo
// request in `req` (the guest's bytes) into `reply` (same length):
// Ethernet dst/src swapped, ethertype 0x0800, IPv4 src/dst swapped, the
// identification ECHOED (deterministic — the gate asserts it), TTL 64,
// protocol 1, ICMP type 0 with the id/seq/payload echoed byte-exact;
// both checksums recomputed (RFC 1071).
func buildIcmpEchoReply(_ reply: inout [UInt8], _ req: [UInt8], _ n: Int, _ hostMAC: [UInt8], _ hostIP: [UInt8]) {
    reply = [UInt8](repeating: 0, count: n)
    reply[0...5] = req[6...11] // dst = the requester's MAC
    reply[6...11] = hostMAC[0...5] // src
    reply[12] = 0x08
    reply[13] = 0x00 // ethertype IPv4
    reply[14] = 0x45 // version 4, IHL 5
    reply[16...17] = req[16...17] // total length (unchanged)
    reply[18...19] = req[18...19] // identification ECHOED
    reply[22] = 64 // TTL
    reply[23] = 0x01 // protocol ICMP
    reply[26...29] = hostIP[0...3] // src = our address
    reply[30...33] = req[26...29] // dst = the requester's address
    let hdrChk = ipChecksum(reply, 14, 34)
    reply[24] = UInt8(hdrChk >> 8)
    reply[25] = UInt8(hdrChk & 0xff)
    reply[34] = 0x00 // ICMP echo reply
    reply[35] = 0x00 // code
    reply[38...n - 1] = req[38...n - 1] // id + seq + payload echoed
    let icmpChk = ipChecksum(reply, 34, n)
    reply[36] = UInt8(icmpChk >> 8)
    reply[37] = UInt8(icmpChk & 0xff)
}
