// M206a (issue #3813, ADR 0057 amendment): --net-hub <path> — a host-side
// L2 segment over ONE UNIX datagram socket, and --mac <addr>, the pinned
// guest-MAC override.
//
// The hub is a learning switch that lives inside the first runner to
// bind <path> (the claimant's call, pinned in the ADR): it learns
// src-MAC -> sender sockaddr, forwards known unicasts to one port, and
// floods broadcast + multicast (01:00:5e:* / 33:33:* included — dst[0]
// bit0) + unknown unicasts to every learned port except the sender. A
// later runner binds <path>.cli-<pid> and relays its guest's TX to the
// hub path; hub deliveries arrive on its own bound socket and are
// written into the SAME attachment socketpair the responders write
// (VZ reads fds[0], so the guest receives them). The --net capture file
// stays byte-exact: only guest TX lands in it — hub-injected frames
// travel the host->guest direction, never the capture direction.
//
// A stale socket file (a hub owner killed without unlinking) is named by
// connect() returning ECONNREFUSED — observed on macOS 27.2 — so attach
// unlinks and rebinds instead of silently joining a dead segment.
//
// Addressing on the segment (the ADR's pinned scheme): the hub owns
// DHCP. The owner answers every DHCP client message on the segment —
// its own guest's and clients' — with one lease per source MAC out of a
// bounded pool, reusing the --net-dhcp-respond reply builder.
// --net-dhcp-respond therefore refuses by name under --net-hub: two
// servers on one segment is the failure mode.

import Darwin
import Foundation

// M206a: --mac <addr> overrides the pinned guest MAC on any net
// attachment (--net/--net-hub/--net-nat/--net-bridged/--net-vmnet).
// Locally administered unicast only — a multicast or universally
// administered argument refuses by name at parse time. nil = the fixed
// default (every existing gate byte-identical).
var netMacOverride: [UInt8]?

let netDefaultMAC: [UInt8] = [0x02, 0x00, 0x00, 0x00, 0x00, 0x01]

func netEffectiveMAC() -> [UInt8] {
    netMacOverride ?? netDefaultMAC
}

func netMacText(_ mac: [UInt8]) -> String {
    mac.map { String(format: "%02x", $0) }.joined(separator: ":")
}

/// The --mac parse verdict — one case per named refusal, pinned by
/// NetHubTests. A well-formed address is accepted only when it is
/// locally administered (u/l bit set) AND unicast (i/g bit clear).
enum NetMacVerdict {
    case ok([UInt8])
    case malformed
    case multicast
    case notLocal
}

func netMacParse(_ text: String) -> NetMacVerdict {
    let halves = text.split(separator: ":", omittingEmptySubsequences: false)
    guard halves.count == 6 else { return .malformed }
    var mac = [UInt8]()
    for h in halves {
        guard h.count == 2, let b = UInt8(h, radix: 16) else { return .malformed }
        mac.append(b)
    }
    if mac[0] & 0x01 != 0 { return .multicast }
    if mac[0] & 0x02 == 0 { return .notLocal }
    return .ok(mac)
}

/// The learning-switch core — pure state over String port ids, no
/// sockets, so NetHubTests drives learn/forward/flood/drop-to-self
/// without a VM or a bound path. Port ids are bound socket paths;
/// NetHub.selfPort names the owning runner's own guest.
final class NetHubSwitch {
    /// Bounded table: 16 MAC->port entries, drop-oldest (the arp.zig
    /// shape — a bound before a promise).
    static let tableLimit = 16

    private var macToPort: [UInt64: String] = [:]
    private var learnOrder: [UInt64] = []
    private(set) var ports: [String] = [] // senders seen, first-seen order

    static func key(_ mac: ArraySlice<UInt8>) -> UInt64 {
        var k: UInt64 = 0
        for b in mac { k = (k << 8) | UInt64(b) }
        return k
    }

    /// One ingress frame from `port`: learn src->port, return the egress
    /// port set. A frame under 14 bytes is not Ethernet — dropped.
    /// Multicast/broadcast (dst[0] bit0 — 01:00:5e:00:00:fb and
    /// 33:33:00:00:00:fb ride this) and unknown unicast flood to every
    /// learned port except the sender; a unicast resolving to the
    /// sender's own port is the drop-to-self.
    func ingress(_ frame: [UInt8], from port: String) -> [String] {
        guard frame.count >= 14 else { return [] }
        if !ports.contains(port) { ports.append(port) }
        let src = Self.key(frame[6..<12])
        if macToPort[src] == nil {
            if learnOrder.count >= Self.tableLimit, let evict = learnOrder.first {
                macToPort.removeValue(forKey: evict)
                learnOrder.removeFirst()
            }
            learnOrder.append(src)
        }
        macToPort[src] = port
        if frame[0] & 0x01 != 0 { return ports.filter { $0 != port } }
        let dst = Self.key(frame[0..<6])
        if let p = macToPort[dst] { return p == port ? [] : [p] }
        return ports.filter { $0 != port }
    }
}

/// The hub's DHCP lease pool — one lease per source MAC out of
/// 10.0.0.16..10.0.0.31 (16 entries; the gateway stays at the
/// --net-dhcp-respond shape's 10.0.0.1, outside the pool). Bounded like
/// the MAC table: the oldest entry evicts at the cap; the smallest free
/// tail is always chosen so a still-held lease is never reissued.
final class NetHubDhcp {
    static let poolFirst: UInt8 = 16
    static let poolLimit = 16

    private var leases: [UInt64: UInt8] = [:] // mac key -> pool tail
    private var order: [UInt64] = []

    func lease(for mac: UInt64) -> [UInt8] {
        if let t = leases[mac] { return [10, 0, 0, t] }
        if order.count >= Self.poolLimit, let evict = order.first {
            leases.removeValue(forKey: evict)
            order.removeFirst()
        }
        let held = Set(leases.values)
        var tail = Self.poolFirst
        while held.contains(tail) { tail += 1 }
        leases[mac] = tail
        order.append(mac)
        return [10, 0, 0, tail]
    }
}

/// Attach-time failures — every one names itself in the runner's fail().
enum NetHubError: Error {
    case pathTooLong
    case socketFailed(Int32)
    case bindFailed(Int32)
    case pathOccupied
    case probeFailed(Int32)
    case rebindFailed(Int32)

    var message: String {
        switch self {
        case .pathTooLong:
            return "exceeds the 103-byte UNIX socket path limit"
        case .socketFailed(let e):
            return "socket(AF_UNIX, SOCK_DGRAM) failed (errno \(e))"
        case .bindFailed(let e):
            return "bind failed (errno \(e))"
        case .pathOccupied:
            return "exists and is not a socket (remove it or pick another path)"
        case .probeFailed(let e):
            return "liveness probe failed (errno \(e))"
        case .rebindFailed(let e):
            return "rebind after unlinking the stale socket failed (errno \(e))"
        }
    }
}

/// The socket half — one bound UNIX datagram socket in either role, a
/// recv thread, and the guest-TX relay. The switch + DHCP state lives
/// behind one lock: guest TX (capture thread) and client frames (the
/// hub's own RX thread) both run ingress.
final class NetHub {
    enum Role { case owner, client }

    /// The owning runner's own guest — a port id no client ever binds
    /// (client paths are always <path>.cli-<pid>).
    static let selfPort = "\u{0}self"

    /// The hub-side DHCP server's source MAC — the same fixed
    /// locally-administered host peer the hermetic responders answer
    /// from, so a lone --net-hub run looks like the --net-dhcp-respond
    /// shape it replaces.
    static let hostMAC: [UInt8] = [0x02, 0x00, 0x00, 0x00, 0x00, 0x02]

    let path: String
    let boundPath: String // == path for the owner, <path>.cli-<pid> for a client
    let role: Role
    let fd: Int32
    let hubAddr: sockaddr_un
    let sw = NetHubSwitch()
    let dhcp = NetHubDhcp()
    let lock = NSLock()
    var rxThread: Thread?
    var stop = false

    private init(path: String, boundPath: String, role: Role, fd: Int32, hubAddr: sockaddr_un) {
        self.path = path
        self.boundPath = boundPath
        self.role = role
        self.fd = fd
        self.hubAddr = hubAddr
    }

    /// sockaddr_un for `path`, or nil past sun_path's 103-byte budget.
    static func unixAddr(_ path: String) -> sockaddr_un? {
        let bytes = Array(path.utf8)
        guard bytes.count <= 103 else { return nil }
        var addr = sockaddr_un()
        addr.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
        addr.sun_family = sa_family_t(AF_UNIX)
        withUnsafeMutableBytes(of: &addr.sun_path) { raw in
            raw.copyBytes(from: bytes)
        }
        return addr
    }

    private static func bindFd(_ fd: Int32, _ addr: sockaddr_un) -> Int32 {
        var a = addr
        return withUnsafePointer(to: &a) { p in
            p.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                bind(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
    }

    /// First-runner-binds attach. Try to own <path>; on EADDRINUSE probe
    /// liveness with connect() — observed on macOS 27.2: a stale file
    /// refuses (ECONNREFUSED), a live hub accepts. Stale -> unlink and
    /// retake as owner; live -> bind <path>.cli-<pid> and relay to the
    /// hub. The bound socket gets a 250 ms receive timeout so `stop`
    /// exits the RX thread cleanly at teardown.
    static func attach(path: String) throws -> NetHub {
        guard let hubAddr = unixAddr(path) else { throw NetHubError.pathTooLong }
        let fd = Darwin.socket(AF_UNIX, SOCK_DGRAM, 0)
        guard fd >= 0 else { throw NetHubError.socketFailed(errno) }

        func finishOwner() throws -> NetHub {
            var tv = timeval(tv_sec: 0, tv_usec: 250_000)
            setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
            return NetHub(path: path, boundPath: path, role: .owner, fd: fd, hubAddr: hubAddr)
        }

        if bindFd(fd, hubAddr) == 0 { return try finishOwner() }
        guard errno == EADDRINUSE else { close(fd); throw NetHubError.bindFailed(errno) }

        // The name is taken. A regular file at <path> is the caller's
        // mistake, not a hub — refuse by name rather than unlink it.
        var st = stat()
        if lstat(path, &st) == 0, (st.st_mode & S_IFMT) != S_IFSOCK {
            close(fd)
            throw NetHubError.pathOccupied
        }
        // Live hub or stale file? connect() answers on macOS: it only
        // succeeds when a process holds the socket bound.
        let probe = Darwin.socket(AF_UNIX, SOCK_DGRAM, 0)
        var probeAddr = hubAddr
        let probeRC = withUnsafePointer(to: &probeAddr) { p in
            p.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                connect(probe, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        let probeErr = errno
        close(probe)
        if probeRC != 0 {
            if probeErr != ECONNREFUSED { close(fd); throw NetHubError.probeFailed(probeErr) }
            // Stale — nobody holds the bound inode. Retake the address.
            unlink(path)
            if bindFd(fd, hubAddr) == 0 { return try finishOwner() }
            // A concurrent runner won the race to rebind (EADDRINUSE) —
            // fall through to client mode against the live hub.
            guard errno == EADDRINUSE else { close(fd); throw NetHubError.rebindFailed(errno) }
        }

        let clientPath = "\(path).cli-\(getpid())"
        guard let clientAddr = unixAddr(clientPath) else { close(fd); throw NetHubError.pathTooLong }
        unlink(clientPath) // our own pid namespace — a leftover is stale
        guard bindFd(fd, clientAddr) == 0 else { close(fd); throw NetHubError.bindFailed(errno) }
        var tv = timeval(tv_sec: 0, tv_usec: 250_000)
        setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
        return NetHub(path: path, boundPath: clientPath, role: .client, fd: fd, hubAddr: hubAddr)
    }

    /// unlink the bound path(s) at exit — the stale-file detector in
    /// attach() is the belt, this is the suspenders. A kill -9 leaves
    /// the file; the next runner's connect() probe heals it.
    func unlinkBoundPaths() {
        unlink(boundPath)
    }

    /// The bound socket's recv loop. Owner role: each inbound datagram
    /// is a client's frame — run the switch. Client role: each inbound
    /// datagram is a hub delivery for the local guest — write it into
    /// the attachment socketpair (VZ reads fds[0]).
    func startRx() {
        rxThread = Thread {
            var buf = [UInt8](repeating: 0, count: 4096)
            var sender = sockaddr_storage()
            var senderLen = socklen_t(MemoryLayout<sockaddr_storage>.size)
            while !self.stop {
                let n = withUnsafeMutablePointer(to: &sender) { sp in
                    sp.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                        recvfrom(self.fd, &buf, buf.count, 0, sa, &senderLen)
                    }
                }
                if n < 0 && (errno == EAGAIN || errno == EINTR) { continue }
                if n <= 0 { break }
                let frame = Array(buf[0..<n])
                if self.role == .client {
                    self.deliver(Self.selfPort, frame)
                } else if let port = Self.senderPath(sender) {
                    self.ingress(frame, from: port)
                }
            }
        }
        rxThread?.start()
    }

    /// The sender's bound path out of a sockaddr_storage — the client
    /// port id the switch learns against.
    static func senderPath(_ ss: sockaddr_storage) -> String? {
        guard ss.ss_family == sa_family_t(AF_UNIX) else { return nil }
        var su = sockaddr_un()
        var copy = ss
        memcpy(&su, &copy, MemoryLayout<sockaddr_un>.size)
        return withUnsafeBytes(of: &su.sun_path) { raw in
            String(decoding: raw.prefix { $0 != 0 }, as: UTF8.self)
        }
    }

    /// One frame learned + forwarded + (for a DHCP client message)
    /// answered. Called under `lock` by both the capture thread (owner's
    /// own guest TX) and the hub RX thread (client frames).
    func ingress(_ frame: [UInt8], from port: String) {
        lock.lock()
        defer { lock.unlock() }
        for dest in sw.ingress(frame, from: port) {
            deliver(dest, frame)
        }
        // The hub owns DHCP on the segment (ADR-pinned addressing): one
        // lease per source MAC, the --net-dhcp-respond reply shape —
        // broadcast dst so the guest's MAC filter admits it, directed
        // back to the requester's port only.
        if isDhcpDatagram(frame, frame.count),
           let mtype = dhcpMessageType(frame, frame.count), mtype == 1 || mtype == 3 {
            var reply = [UInt8](repeating: 0, count: 4096)
            let leaseIP = dhcp.lease(for: NetHubSwitch.key(frame[6..<12]))
            let replyLen = buildDhcpReply(&reply, frame, frame.count, Self.hostMAC, leaseIP, 3600, mtype)
            deliver(port, Array(reply[0..<replyLen]))
            let srcText = port == Self.selfPort ? "the local guest" : port
            print("NET-HUB-DHCP: answered the \(mtype == 1 ? "DISCOVER" : "REQUEST") from \(srcText) with \(leaseIP[0]).\(leaseIP[1]).\(leaseIP[2]).\(leaseIP[3]) (lease 3600s)")
        }
    }

    /// Guest TX -> the segment (the capture thread's relay). Owner mode
    /// runs the frame through the switch in-process — the local guest is
    /// the selfPort. Client mode sendto()s the hub path; a dead hub
    /// names itself in errno (ECONNREFUSED — the same liveness signal
    /// attach() probes).
    func guestTx(_ frame: [UInt8]) {
        if role == .owner {
            ingress(frame, from: Self.selfPort)
            return
        }
        var addr = hubAddr
        let rc = withUnsafePointer(to: &addr) { p in
            p.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                sendto(fd, frame, frame.count, 0, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        if rc < 0 {
            FileHandle.standardError.write(Data("NET-HUB: relay to hub \(path) failed (errno \(errno)) — the owning runner is gone; restart it or remove \(path)\n".utf8))
        }
    }

    /// Deliver one frame to a port: the owning runner's own guest (into
    /// the attachment socketpair) or a client's bound socket.
    func deliver(_ port: String, _ frame: [UInt8]) {
        if port == Self.selfPort {
            try? netCaptureReadSocket?.write(contentsOf: Data(frame))
            return
        }
        guard var addr = Self.unixAddr(port) else { return }
        _ = withUnsafePointer(to: &addr) { p in
            p.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                sendto(fd, frame, frame.count, 0, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
    }
}

var netHubPath: String?

var netHub: NetHub?
