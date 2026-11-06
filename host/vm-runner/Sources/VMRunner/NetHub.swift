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
// Queue depth (the mid-run wedge fixed here): every hop on the relay is a
// UNIX datagram socket whose default SO_RCVBUF is 4096 bytes on macOS
// 27.2 — measured: ~2-3 datagrams of ~1400 B fit before the sender's
// send fails ENOBUFS. Bursts that outrun a momentarily-slow consumer
// (VZ's lazy ring drain, a guest busy in an app) therefore dropped
// silently at every hop; one-shot exchanges (ICMP, an ARP request)
// eventually slip through but multi-round-trip establishment (an ARP
// reply inside the guest's ~0.5 s learn window, a TCP three-way
// handshake) collapses under sustained load — the observed wedge,
// where replies to pings still landed but SYNs produced no inbound
// activity. The bound hub sockets ask for netHubRcvBufBytes (1 MiB ≈
// 700+ datagrams); the LAST hop — the attachment socketpair VZ reads —
// takes a bounded userspace queue instead (NetGuestQueue), because
// deepening that pair's SO_RCVBUF visibly broke VZ delivery (observed:
// a guest received exactly one frame, then none). Backpressure becomes
// queueing delay everywhere, never silent loss.
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

    /// A port whose socket is gone (its runner exited — sendto names it
    /// ENOENT/ECONNREFUSED): stop flooding to it and drop every MAC
    /// learned against it, so a guest that rejoins with the same MAC on
    /// a new port re-learns instead of resolving to the dead one.
    func forget(_ port: String) {
        ports.removeAll { $0 == port }
        let gone = macToPort.filter { $0.value == port }.map(\.key)
        for k in gone { macToPort.removeValue(forKey: k) }
        learnOrder.removeAll { gone.contains($0) }
    }
}

/// The receive queue every hub-side socket asks for: 1 MiB ≈ 700+
/// datagrams of 1400 B (measured on macOS 27.2 — the 4096-byte default
/// holds ~2-3). A peer that stops draining entirely still reaches
/// ENOBUFS at the bound; the queue's job is to absorb bursts, not to be
/// infinite.
let netHubRcvBufBytes: Int32 = 1 << 20

/// The deep host->guest queue in front of the VZ attachment socketpair
/// (fds[1] writes deliver to fds[0], which VZ reads into the guest's rx
/// ring). The pair's kernel queue is the 4096-byte default (~2-3
/// datagrams) and VZ's ring drain lags bursts by design — the wedge was
/// the writes into it failing ENOBUFS and being swallowed. Deepening
/// the pair's SO_RCVBUF is NOT an option — observed on the live gate:
/// with a 1 MiB SO_RCVBUF set, a guest received exactly one frame and
/// never another, so VZ's attachment misbehaves on a deepened buffer.
/// The deep queue therefore lives here in userspace: up to `limit`
/// frames ride until the pair admits them, every writer flushes first,
/// MSG_DONTWAIT keeps each send a drop rather than a parked relay
/// thread, and overflow is a counted drop — never a silent one.
///
/// #3863 — the delivery LAG behind the same wedge class. The bisected
/// deep-buffer failure (one frame, then none) only makes sense if VZ's
/// attachment read is readiness-edge driven: a datagram that lands
/// while the pair is still non-empty produces no NEW edge, so a writer
/// that keeps the pair occupied starves the very read it needs. With a
/// 1 MiB buffer the first burst filled it and the socket never returned
/// to empty — no further edges, no further reads. The shallow pair hits
/// the same trap under sustained load: back-to-back sends leave it
/// permanently non-empty, so host->guest delivery degrades to whatever
/// other cadence pokes VZ (the observed multi-second-to-minute spikes
/// and the "one request late" ARP staleness). The queue therefore paces
/// the last hop: ONE datagram is written per fully-empty pair state
/// (SO_NREAD == 0), so every frame is a fresh empty->nonempty edge and
/// delivery self-clocks at VZ's real service rate. A dedicated drainer
/// polls the empty state while backlog exists (the 25 ms rx-loop wake
/// alone would cap throughput at ~40 fps); a socket that cannot be
/// sampled (-1) keeps the legacy send-until-ENOBUFS pacing.
final class NetGuestQueue {
    static let limit = 512 // ~0.75 MiB worst case of queued frames
    /// Above this pending depth, new broadcast/multicast frames are
    /// shed instead of queued — unicast gets the whole budget.
    static let mcastShed = 128
    /// How long the drainer sleeps between empty-state polls while a
    /// backlog waits on VZ. Sub-millisecond so the delivered rate is
    /// bounded by VZ's service rate, not the poll cadence.
    static let drainPollSeconds = 0.0005
    /// #3863: the write-side pace — after a datagram lands in the pair,
    /// hold the NEXT write this long. VZ provably reads every datagram
    /// (SO_NREAD clears each) yet drops them before any DMA on wedged
    /// boots; if its model reads greedily into a small staging depth
    /// faster than it can post to the ring, the gap between reads is
    /// the only thing that bounds the staging pressure — the pair
    /// still edges once per frame, the experiment just widens the
    /// space between the edges.
    static let drainPaceSeconds = 0.002

    /// One queued frame and when it arrived — the enqueue->pair-send
    /// delay is the measurement that names WHERE the host->guest lag
    /// sits (#3863): a frame sent promptly that lands late blames the
    /// pair (VZ's read), a frame sent late blames this queue's flush.
    private struct Queued {
        let frame: [UInt8]
        let enq: TimeInterval
    }

    private var frames: [Queued] = []
    private var head = 0
    private var drops = 0
    private var mcastSheds = 0
    private let lock = NSLock()
    private let work = NSCondition()
    private var drainerStarted = false
    // #3863 diagnostics (bounded logs): sent/slowSent count deliveries,
    // maxDelay is the worst enqueue->send wait observed, stallTicks the
    // flush passes that moved nothing.
    private var sent = 0
    private var slowSent = 0
    private var maxDelay: TimeInterval = 0
    private var stallTicks = 0
    /// #3863: when a non-empty queue first finds the pair unread — the
    /// open of one stall episode. The stall report carries the age, and
    /// the drain that finally moves logs the whole window, so the gate
    /// evidence names VZ's un-serviced bound instead of a guess.
    private var stallSince: TimeInterval? = nil

    /// #3895: the frame written into a sampleable pair but not yet
    /// proven consumed — VZ's read clears SO_NREAD back to 0 and the
    /// next flush sample credits it (with its bytes, so the detector
    /// can match a delivered reply to its solicitation). An
    /// unsampleable pair (pairUnread -1) can never prove consumption,
    /// so nothing is credited there.
    private var inflight: [UInt8]?
    /// The dead-RX detector — armed by the runner under --net, nil in
    /// the queue-less/inject shapes (a nil detector just never fires).
    /// Touched only under `lock`.
    var deadRx: NetDeadRx?

    /// The first pending datagram's length on VZ's end of the attachment
    /// socketpair — the pair's occupancy (SO_NREAD; 0 = empty, the only
    /// state the pacing sends into). -1 = the pair can't be sampled
    /// (--net-inject and the tests run queue-less) and pacing falls back
    /// to send-until-ENOBUFS.
    private func pairUnread() -> Int {
        guard let vfd = netCaptureSocket?.fileDescriptor else { return -1 }
        var n: Int32 = 0
        var len = socklen_t(MemoryLayout<Int32>.size)
        if getsockopt(vfd, SOL_SOCKET, SO_NREAD, &n, &len) != 0 { return -1 }
        return Int(n)
    }

    /// Append + flush. Every host->guest writer rides the one queue —
    /// hub deliveries and the hermetic responders — so ordering holds.
    /// Under pressure the queue sheds FLOOD traffic first (dst[0]
    /// bit0 — broadcast/multicast is best-effort on a real switch, and
    /// storm control is the same trade): a congested guest's shallow
    /// rx fifo must be spent on the unicasts its protocols need.
    func send(_ frame: [UInt8]) {
        lock.lock()
        defer { lock.unlock() }
        flushLocked()
        let pending = frames.count - head
        if pending >= Self.limit {
            noteDrop()
            return
        }
        if pending >= Self.mcastShed, frame.count > 0, frame[0] & 0x01 != 0 {
            mcastSheds += 1
            if mcastSheds <= 4 || mcastSheds % 256 == 0 {
                FileHandle.standardError.write(Data("NET-HUB: flood frame shed under pressure — \(mcastSheds) sheds, \(pending) queued\n".utf8))
            }
            return
        }
        frames.append(Queued(frame: frame, enq: Date().timeIntervalSince1970))
        flushLocked()
        work.lock()
        work.signal()
        work.unlock()
        kickDrainer()
    }

    /// Queued-but-unsent depth — the test seam for the #3863 pacing
    /// (the wire can't distinguish "1 in flight" from "N in flight" —
    /// SO_NREAD reports only the first pending datagram's length).
    func pendingCount() -> Int {
        lock.lock()
        defer { lock.unlock() }
        return frames.count - head
    }

    /// Attempt to move the queue toward the pair now. Called on each
    /// rx-loop wake and each capture-loop read so the backlog never
    /// waits solely on the drainer's poll cadence.
    func flush() {
        lock.lock()
        flushLocked()
        lock.unlock()
    }

    /// #3895: feed the dead-RX detector one guest-TX datagram — the
    /// capture loop's per-datagram call.
    func observeGuestTx(_ frame: [UInt8]) {
        lock.lock()
        deadRx?.noteGuestTx(frame, now: Date().timeIntervalSince1970)
        lock.unlock()
    }

    /// The drainer thread: while the queue is non-empty, poll VZ's end
    /// of the pair at the sub-ms cadence and write the next frame the
    /// instant the pair returns to empty — the edge-per-frame pacing.
    /// Sleeps on `work` when the queue is empty (the wait is timed: a
    /// signal that raced the wake self-heals in one interval, and the
    /// send() that raced it already flushed the frame itself). Started
    /// lazily on the first queued frame; lives for the process — the
    /// queue is a process-lifetime singleton.
    private func kickDrainer() {
        if drainerStarted { return }
        drainerStarted = true
        let t = Thread {
            while true {
                self.work.lock()
                _ = self.work.wait(until: Date().addingTimeInterval(0.05))
                self.work.unlock()
                while true {
                    self.lock.lock()
                    if self.head == self.frames.count {
                        self.lock.unlock()
                        break
                    }
                    let unread = self.pairUnread()
                    self.lock.unlock()
                    if unread > 0 {
                        // VZ hasn't emptied the pair — poll, not block:
                        // SO_NREAD has no waitable peer (the pair's
                        // POLLOUT fires on ANY room, not on empty).
                        Thread.sleep(forTimeInterval: Self.drainPollSeconds)
                        continue
                    }
                    self.flush()
                    if unread < 0 {
                        // Unsampleable pair: the flush already ran the
                        // legacy send-until-ENOBUFS drain — yield before
                        // re-checking instead of spinning on sends.
                        Thread.sleep(forTimeInterval: Self.drainPollSeconds)
                    } else {
                        // A write just landed (pair was empty) — pace
                        // the next one: VZ reads greedily, and its
                        // staging needs a beat to post the frame to the
                        // ring before the next datagram arrives.
                        Thread.sleep(forTimeInterval: Self.drainPaceSeconds)
                    }
                }
            }
        }
        t.name = "net-guest-queue-drainer"
        t.start()
    }

    private func flushLocked() {
        let now = Date().timeIntervalSince1970
        defer { deadRx?.tick(now) }
        guard let wfd = netCaptureReadSocket?.fileDescriptor else {
            frames.removeAll()
            head = 0
            return
        }
        var sentThisPass = false
        // #3895: an empty pair with writes outstanding means VZ drained
        // them — the consumed deliveries the dead-RX detector counts.
        // Outside the loop so an EMPTY queue still credits the last
        // write's drain on the next wake instead of waiting for the
        // next queued frame.
        if pairUnread() == 0, let f = inflight {
            deadRx?.noteConsumed(1, frame: f, now: now)
            inflight = nil
        }
        while head < frames.count {
            // #3863: write ONLY into a fully-empty pair — each datagram
            // is then a fresh empty->nonempty readiness edge for VZ's
            // read. -1 (unsampleable) falls back to the legacy
            // send-until-ENOBUFS pacing.
            let unread = pairUnread()
            if unread == 0, let f = inflight {
                deadRx?.noteConsumed(1, frame: f, now: now)
                inflight = nil
            }
            if unread > 0 { break }
            let q = frames[head]
            if netRxDeadArmed {
                // #3895 fault seam: swallow the frame at the last hop
                // but count it sent AND consumed-without-completion —
                // the observed wedge signature (VZ reads every
                // datagram, nothing completes). frame: nil — a reply
                // the guest provably never received is no answer.
                head += 1
                sent += 1
                sentThisPass = true
                deadRx?.noteConsumed(1, frame: nil, now: now)
                continue
            }
            let n = q.frame.withUnsafeBufferPointer { bp in
                Darwin.send(wfd, bp.baseAddress, bp.count, MSG_DONTWAIT | MSG_NOSIGNAL)
            }
            if n >= 0 {
                head += 1
                sent += 1
                sentThisPass = true
                if unread == 0 { inflight = q.frame }
                // #3863: name every ARP datagram written into the pair —
                // the guest's used-ring trace (RXC/RXA in `net log`)
                // correlates seq -> completion, so a datagram VZ read
                // (unread cleared) that never completes is observed lost
                // inside the pair->ring hop rather than guessed.
                if q.frame.count >= 42, q.frame[12] == 0x08, q.frame[13] == 0x06 {
                    let op = Int(q.frame[20]) << 8 | Int(q.frame[21])
                    let spa = "\(q.frame[28]).\(q.frame[29]).\(q.frame[30]).\(q.frame[31])"
                    let tpa = "\(q.frame[38]).\(q.frame[39]).\(q.frame[40]).\(q.frame[41])"
                    let dst = q.frame[0..<6].map { String(format: "%02x", $0) }.joined(separator: ":")
                    FileHandle.standardError.write(Data("NET-GQ-TX: n=\(sent) len=\(q.frame.count) arp op=\(op) spa=\(spa) tpa=\(tpa) dst=\(dst)\n".utf8))
                }
                let delay = Date().timeIntervalSince1970 - q.enq
                if delay > maxDelay { maxDelay = delay }
                if delay > 0.1 { slowSent += 1 }
                if delay > 0.5 {
                    FileHandle.standardError.write(Data("NET-GQ: frame sent after \(String(format: "%.2f", delay))s queued (pair unread=\(pairUnread())B, pending=\(frames.count - head), slow=\(slowSent))\n".utf8))
                }
                if unread == 0 { break } // one frame per empty state; -1 keeps draining
                continue
            }
            let e = errno
            if e == EINTR { continue }
            if e == EAGAIN || e == ENOBUFS { break } // peer full — stay queued
            head += 1 // dead fd etc. — this frame is undeliverable
            noteDrop()
        }
        // A non-empty queue the pair refuses is the stall record —
        // pairUnread names VZ's own backlog beside it.
        let pending = frames.count - head
        if pending > 0 && !sentThisPass {
            if stallSince == nil { stallSince = Date().timeIntervalSince1970 }
            stallTicks += 1
            let stalled = Date().timeIntervalSince1970 - stallSince!
            if stallTicks <= 4 || stallTicks % 64 == 0 {
                FileHandle.standardError.write(Data("NET-GQ: \(pending) queued, pair unread=\(pairUnread())B, sent=\(sent) (slow=\(slowSent), max=\(String(format: "%.2f", maxDelay))s) drops=\(drops) stalled=\(String(format: "%.1f", stalled))s\n".utf8))
            }
        }
        if head == frames.count {
            // The stall cleared — name the whole window (VZ's un-serviced
            // bound) once, bounded: only episodes over half a second.
            if let since = stallSince {
                let window = Date().timeIntervalSince1970 - since
                if window > 0.5 {
                    FileHandle.standardError.write(Data("NET-GQ: pair serviced after \(String(format: "%.1f", window))s unread (stallTicks=\(stallTicks))\n".utf8))
                }
            }
            stallSince = nil
            frames.removeAll()
            head = 0
        }
    }

    private func noteDrop() {
        drops += 1
        if drops <= 4 || drops % 256 == 0 {
            FileHandle.standardError.write(Data("NET-HUB: host->guest frame dropped (queue full) — \(drops) drops total\n".utf8))
        }
    }
}

/// #3895: the bounded dead-RX detector — the host-visible signature of
/// the VZ attachment's pair->ring dead state #3880 root-caused: the
/// host writes replies into the attachment socketpair and VZ READS
/// every one (a consumed delivery — SO_NREAD clears back to 0), yet the
/// guest re-sends the identical solicitation because nothing ever
/// completes to its RX ring. Pure state, no sockets — NetHubTests
/// drives it without a VM.
///
/// The signals, all host-observable:
///   * a SOLICITATION — a request-shaped guest-TX frame whose re-send
///     is the honest "reply never landed" evidence: an ARP request, an
///     ICMPv6 neighbor solicitation, a DHCP client datagram, or a TCP
///     SYN without ACK. Unsolicited periodic frames (mDNS announces,
///     RAs) are not solicitations — their identical repeats prove
///     nothing and are never counted;
///   * a CONSUMED DELIVERY — NetGuestQueue wrote a host->guest frame
///     into the pair and later sampled the pair EMPTY again (VZ read
///     it). An unsampleable pair (SO_NREAD failing) can never prove
///     consumption, so the detector cannot fire there;
///   * the WINDOW — repeat sightings count only inside `windowSeconds`.
///
/// The verdict fires when ONE solicitation reaches `repeatBound`
/// re-sends EACH preceded by at least one consumed delivery: N
/// provably-delivered replies producing zero RX completions is the dead
/// state — not loss before VZ. The false-positive bound is exactly that
/// requirement: honest retransmits follow replies that were NOT
/// consumed (still queued, shed under pressure, dropped at the bound),
/// so a healthy link can only reach the bound when VZ demonstrably
/// swallowed N replies in a row while the solicitation stayed open.
///
/// Recovery is bounded and named at every step: on the verdict the
/// runner writes `net rxreset` into the guest console (the guest runs
/// the proven transport_init + re-arm — #3880 observed VZ re-observing
/// queue setup on a healthy device) at most `recoverMax` times spaced
/// `recoverWindowSeconds` apart; a post-attempt solicitation that gets
/// a consumed reply and then stays quiet `quietSeconds` is a COMPLETED
/// exchange — delivery resumed, the `NET-RECOVER: rx delivery restored`
/// marker. A window that produces no completed exchange retries or
/// ends at the named refusal — never a silent reset loop.
final class NetDeadRx {
    struct Limits {
        var windowSeconds: TimeInterval = 20
        var repeatBound = 3 // re-sends (the 4th identical sighting fires)
        var recoverMax = 2
        var recoverWindowSeconds: TimeInterval = 12
        var quietSeconds: TimeInterval = 5
        var tableLimit = 64 // bounded solicitation memory
    }

    enum Phase {
        case watching
        case recovering(attempt: Int, since: TimeInterval)
        case refused
    }

    let limits: Limits
    /// The recovery dispatch — returns false when the guest console
    /// channel is not attached or the write fails; the detector then
    /// names the refusal instead of pretending a command was delivered.
    var onRequestReinit: (() -> Bool)?
    /// Where the named markers go — runner stdout by default so gate
    /// legs assert them with output-contains; tests inject a capture.
    var mark: (String) -> Void = { print($0) }

    private(set) var phase: Phase = .watching
    private(set) var attempts = 0 // re-init requests dispatched (telemetry)

    private struct Entry {
        var first: TimeInterval
        var last: TimeInterval
        var repeats: Int // consumed-backed re-sends inside the window
        var consumedAtLast: Int // consumedTotal at the last sighting
        /// A reply MATCHING this solicitation was consumed by VZ — the
        /// stamp is the delivery, and a last-sighting at-or-before it
        /// followed by quiet is the completed exchange (the guest
        /// provably stopped asking because the answer landed). A
        /// consumed reply that does NOT match can never set this, so
        /// retry-exhaustion quiet alone can never mark delivery
        /// restored.
        var answeredAt: TimeInterval?
    }

    private var consumedTotal = 0
    private var solicits: [[UInt8]: Entry] = [:]

    init(limits: Limits = .init()) { self.limits = limits }

    /// Is `f` a request-shaped frame whose identical re-send proves an
    /// unanswered solicitation? ARP request, ICMPv6 NS, DHCP client
    /// datagram, TCP SYN-without-ACK — bounded, and deliberately blind
    /// to everything else (a periodic mDNS announce can never
    /// false-fire, however often it repeats).
    static func isSolicitation(_ f: [UInt8]) -> Bool {
        guard f.count >= 42 else { return false }
        let et = Int(f[12]) << 8 | Int(f[13])
        switch et {
        case 0x0806: // ARP: op 1 (request) only
            return f[20] == 0x00 && f[21] == 0x01
        case 0x86dd: // IPv6: next-header ICMPv6, type 135 (NS)
            guard f.count >= 14 + 40 + 4 else { return false }
            return f[14 + 6] == 58 && f[14 + 40] == 135
        case 0x0800: // IPv4
            let ihl = Int(f[14] & 0x0f) * 4
            guard ihl >= 20, f.count >= 14 + ihl + 4 else { return false }
            switch f[14 + 9] {
            case 17: // UDP: DHCP client port pair 68 -> 67
                let sp = Int(f[14 + ihl]) << 8 | Int(f[14 + ihl + 1])
                let dp = Int(f[14 + ihl + 2]) << 8 | Int(f[14 + ihl + 3])
                return sp == 68 && dp == 67
            case 6: // TCP: SYN set, ACK clear (a connect solicitation)
                guard f.count >= 14 + ihl + 14 else { return false }
                let flags = f[14 + ihl + 13]
                return flags & 0x12 == 0x02
            default:
                return false
            }
        default:
            return false
        }
    }

    /// The queue's consumed-delivery feed — n frames written into the
    /// pair were just sampled drained (VZ read them). `frame` is the
    /// delivered reply when the credit carries its bytes (the real
    /// pair drain); the fault seam passes nil — a consumed frame the
    /// guest provably never received must never mark a solicitation
    /// answered.
    func noteConsumed(_ n: Int, frame: [UInt8]?, now: TimeInterval) {
        consumedTotal += n
        guard let frame else { return }
        for (key, var e) in solicits {
            if Self.replyAnswers(reply: frame, solicitation: key) {
                e.answeredAt = now
                solicits[key] = e
            }
        }
    }

    /// Does the delivered `reply` answer `solicitation`? Matched per
    /// family: ARP reply (op 2) whose spa is the request's tpa and tha
    /// its sha; NDP NA (type 136) with the NS's target; DHCP server
    /// datagram (67->68) with the client's xid; TCP SYN-ACK (0x12) on
    /// the reversed tuple acknowledging ISN+1.
    static func replyAnswers(reply f: [UInt8], solicitation s: [UInt8]) -> Bool {
        guard f.count >= 42, s.count >= 42 else { return false }
        let ret = Int(f[12]) << 8 | Int(f[13])
        let set = Int(s[12]) << 8 | Int(s[13])
        if set == 0x0806, ret == 0x0806 {
            // ARP: reply spa(28-31) == request tpa(38-41), tha(32-37) ==
            // request sha(22-27).
            return f[20] == 0 && f[21] == 2 &&
                f[28..<32].elementsEqual(s[38..<42]) &&
                f[32..<38].elementsEqual(s[22..<28])
        }
        if set == 0x86dd, ret == 0x86dd {
            guard f.count >= 78, s.count >= 78 else { return false }
            return f[20] == 58 && f[54] == 136 &&
                f[62..<78].elementsEqual(s[62..<78])
        }
        guard set == 0x0800, ret == 0x0800 else { return false }
        let sihl = Int(s[14] & 0x0f) * 4, rihl = Int(f[14] & 0x0f) * 4
        guard sihl >= 20, rihl >= 20 else { return false }
        if s[14 + 9] == 17, f[14 + 9] == 17 {
            // DHCP OFFER/ACK 67->68 echoing the client's xid (BOOTP
            // bytes 4-7 of the UDP payload).
            guard f.count >= 14 + rihl + 8 + 8, s.count >= 14 + sihl + 8 + 8 else { return false }
            let sdp = Int(f[14 + rihl + 2]) << 8 | Int(f[14 + rihl + 3])
            let ddp = Int(f[14 + rihl]) << 8 | Int(f[14 + rihl + 1])
            guard sdp == 68, ddp == 67 else { return false }
            return f[(14 + rihl + 8 + 4)..<(14 + rihl + 8 + 8)]
                .elementsEqual(s[(14 + sihl + 8 + 4)..<(14 + sihl + 8 + 8)])
        }
        if s[14 + 9] == 6, f[14 + 9] == 6 {
            // SYN-ACK: flags 0x12, reversed ports, ack == SYN seq + 1.
            guard f.count >= 14 + rihl + 20, s.count >= 14 + sihl + 20 else { return false }
            guard f[14 + rihl + 13] & 0x12 == 0x12 else { return false }
            let ssp = Int(s[14 + sihl]) << 8 | Int(s[14 + sihl + 1])
            let sdp = Int(s[14 + sihl + 2]) << 8 | Int(s[14 + sihl + 3])
            let rsp = Int(f[14 + rihl]) << 8 | Int(f[14 + rihl + 1])
            let rdp = Int(f[14 + rihl + 2]) << 8 | Int(f[14 + rihl + 3])
            guard rsp == sdp, rdp == ssp else { return false }
            var sseq: UInt32 = 0, rack: UInt32 = 0
            for i in 0..<4 {
                sseq = sseq << 8 | UInt32(s[14 + sihl + 4 + i])
                rack = rack << 8 | UInt32(f[14 + rihl + 8 + i])
            }
            return rack == sseq &+ 1
        }
        return false
    }

    /// One guest-TX datagram (the capture loop's feed). Only
    /// solicitation sightings mutate state.
    func noteGuestTx(_ frame: [UInt8], now: TimeInterval) {
        guard Self.isSolicitation(frame) else { return }
        if var e = solicits[frame], now - e.last <= limits.windowSeconds {
            // A re-send inside the window: it counts only when at least
            // one delivery was consumed since the LAST sighting — a
            // re-send behind an unconsumed reply is honest loss before
            // VZ, not dead-rx.
            if consumedTotal > e.consumedAtLast { e.repeats += 1 }
            e.last = now
            e.consumedAtLast = consumedTotal
            solicits[frame] = e
            if case .watching = phase, e.repeats >= limits.repeatBound {
                dispatchAttempt(1, now,
                    "a solicitation re-sent \(e.repeats)x behind consumed replies inside \(fmt(limits.windowSeconds))s — TX flows, zero RX completions")
            }
            return
        }
        // First sighting — or the window expired and this is a fresh one.
        if solicits[frame] == nil, solicits.count >= limits.tableLimit {
            evictOldest()
        }
        solicits[frame] = Entry(first: now, last: now, repeats: 0, consumedAtLast: consumedTotal)
    }

    /// Drive the post-attempt window — called on every queue flush (the
    /// rx-loop and capture-loop wakes) so recovery/refusal timing never
    /// waits on traffic.
    func tick(_ now: TimeInterval) {
        guard case .recovering(let n, let since) = phase else { return }
        // A post-attempt solicitation whose MATCHING reply was consumed,
        // then quiet `quietSeconds`, is a completed exchange — the
        // delivery-resumed evidence. A straggler re-send in flight when
        // its answer lands bumps `last` past `answeredAt` but does not
        // un-answer it; a reply that dies inside VZ keeps the re-sends
        // coming (last stays fresh), and retry-exhaustion quiet alone
        // has no answeredAt — neither can false-restore.
        for (_, e) in solicits where e.answeredAt != nil && now - e.last >= limits.quietSeconds {
            mark("NET-RECOVER: rx delivery restored after re-init attempt \(n) (a solicitation's reply completed, quiet \(fmt(limits.quietSeconds))s)")
            phase = .watching
            solicits.removeAll()
            return
        }
        if now - since >= limits.recoverWindowSeconds {
            if n < limits.recoverMax {
                dispatchAttempt(n + 1, now,
                    "attempt \(n) produced no completed exchange inside \(fmt(limits.recoverWindowSeconds))s")
            } else {
                mark("NET-RECOVER: refusing further attachment re-inits — \(n) bounded attempts left RX delivery dead")
                phase = .refused
            }
        }
    }

    private func dispatchAttempt(_ n: Int, _ now: TimeInterval, _ reason: String) {
        attempts += 1
        mark("NET-RECOVER: dead-rx — \(reason); requesting guest transport re-init (attempt \(n)/\(limits.recoverMax))")
        solicits.removeAll() // post-attempt sightings start a fresh epoch
        guard let req = onRequestReinit, req() else {
            mark("NET-RECOVER: refusing — no guest console channel, or the re-init write failed; the dead-rx signature stands")
            phase = .refused
            return
        }
        phase = .recovering(attempt: n, since: now)
    }

    private func evictOldest() {
        if let oldest = solicits.min(by: { $0.value.first < $1.value.first }) {
            solicits.removeValue(forKey: oldest.key)
        }
    }

    private func fmt(_ t: TimeInterval) -> String {
        String(format: "%.0f", t)
    }
}

/// #3895: the recovery dispatch — write `net rxreset` into the guest
/// console (the same host->guest serial path --script forwards
/// through). Returns false when no console input is attached (plain
/// evidence runs) or the write fails — the detector names that refusal
/// instead of pretending a command was delivered.
func netRecoverRequestReinit() -> Bool {
    if netRxDeadMode == .untilReset && netRxDeadArmed {
        // The fault seam's recovery half: disarm at the dispatch so the
        // leg's post-attempt deliveries reach VZ again — the injected
        // wedge ends exactly where a real one would.
        netRxDeadArmed = false
        FileHandle.standardError.write(Data("NET-RECOVER-FAULT: dead-rx fault cleared on the re-init dispatch\n".utf8))
    }
    guard netConsoleInputAttached else { return false }
    do {
        try consoleInputPipe.fileHandleForWriting.write(contentsOf: Data("net rxreset\n".utf8))
        return true
    } catch {
        FileHandle.standardError.write(Data("NET-RECOVER: re-init write to the guest console failed: \(error)\n".utf8))
        return false
    }
}

let netGuestQueue = NetGuestQueue()

/// The rx loop's break verdict — one case per errno worth stopping for.
/// Everything that is not "the fd is gone" is transient on this socket:
/// the loop must ride it (and count it), because an rx thread that exits
/// silently takes a whole relay direction with it while every other path
/// keeps working — the wedge's exact asymmetry.
func netHubRecvFatal(_ e: Int32) -> Bool {
    e == EBADF || e == ENOTSOCK
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
    /// hub. The bound socket asks for the deep receive queue
    /// (netHubRcvBufBytes) and keeps a short receive timeout so the
    /// RX thread exits cleanly on `stop` and re-flushes the host->guest
    /// queue every wake — #3849 tightened it 250 ms -> 25 ms: a queued
    /// frame must wait at most one wake on the pair making room, and the
    /// guest's ARP learn window is only ~2 s, so 250 ms of queueing delay
    /// per hop was eating the whole budget on a loaded host.
    static func attach(path: String) throws -> NetHub {
        guard let hubAddr = unixAddr(path) else { throw NetHubError.pathTooLong }
        let fd = Darwin.socket(AF_UNIX, SOCK_DGRAM, 0)
        guard fd >= 0 else { throw NetHubError.socketFailed(errno) }

        func finishOwner() throws -> NetHub {
            var tv = timeval(tv_sec: 0, tv_usec: 25_000)
            setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
            var rcvbuf = netHubRcvBufBytes
            setsockopt(fd, SOL_SOCKET, SO_RCVBUF, &rcvbuf, socklen_t(MemoryLayout<Int32>.size))
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
        var tv = timeval(tv_sec: 0, tv_usec: 25_000)
        setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
        var rcvbuf = netHubRcvBufBytes
        setsockopt(fd, SOL_SOCKET, SO_RCVBUF, &rcvbuf, socklen_t(MemoryLayout<Int32>.size))
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
    /// the attachment socketpair (VZ reads fds[0]). The socket is
    /// blocking with a 25 ms receive timeout (attach) — the kernel
    /// wakes the read on arrival and the timeout lets `stop` exit the
    /// thread cleanly. The loop exits only on stop or a dead fd
    /// (netHubRecvFatal); its exit is logged, and unexpected errnos are
    /// counted and logged, because an rx thread that dies silently
    /// takes a whole relay direction with it.
    func startRx() {
        rxThread = Thread {
            var buf = [UInt8](repeating: 0, count: 4096)
            var sender = sockaddr_storage()
            var senderLen = socklen_t(MemoryLayout<sockaddr_storage>.size)
            var transientErrs = 0
            while !self.stop {
                // Every wake — a datagram or the 25 ms timeout — also
                // drains the host->guest queue as the pair makes room.
                netGuestQueue.flush()
                let n = withUnsafeMutablePointer(to: &sender) { sp in
                    sp.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                        recvfrom(self.fd, &buf, buf.count, 0, sa, &senderLen)
                    }
                }
                if n < 0 {
                    let e = errno
                    if e == EAGAIN || e == EINTR { continue }
                    if netHubRecvFatal(e) { break }
                    transientErrs += 1
                    if transientErrs <= 4 || transientErrs % 256 == 0 {
                        FileHandle.standardError.write(Data("NET-HUB: recvfrom on \(self.boundPath) returned errno \(e) (\(transientErrs) transient) — continuing\n".utf8))
                    }
                    continue
                }
                if n == 0 { continue }
                let frame = Array(buf[0..<n])
                if self.role == .client {
                    self.deliver(Self.selfPort, frame)
                } else if let port = Self.senderPath(sender) {
                    self.ingress(frame, from: port)
                }
            }
            FileHandle.standardError.write(Data("NET-HUB: rx loop on \(self.boundPath) exited (stop=\(self.stop))\n".utf8))
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
    /// the NetGuestQueue feeding the attachment socketpair — the deep
    /// queue is userspace because deepening the pair's SO_RCVBUF broke
    /// VZ delivery) or a client's bound socket. A failed sendto (ENOBUFS
    /// while the peer's queue is full) is a drop — counted and logged
    /// bounded, never silently swallowed like the wedge hid behind. A
    /// client path whose runner is gone leaves the segment instead: it
    /// is not a loss, and flooding to it forever made it look like one.
    private var sendDrops = 0

    private func noteDrop(_ port: String, _ e: Int32) {
        sendDrops += 1
        if sendDrops <= 4 || sendDrops % 256 == 0 {
            let dst = port == Self.selfPort ? "the local guest" : port
            FileHandle.standardError.write(Data("NET-HUB: delivery to \(dst) dropped (errno \(e)) — \(sendDrops) drops total\n".utf8))
        }
    }

    func deliver(_ port: String, _ frame: [UInt8]) {
        if port == Self.selfPort {
            netGuestQueue.send(frame)
            return
        }
        guard var addr = Self.unixAddr(port) else { return }
        let rc = withUnsafePointer(to: &addr) { p in
            p.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                sendto(fd, frame, frame.count, 0, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        if rc < 0 {
            let e = errno
            if e == ENOENT || e == ECONNREFUSED {
                sw.forget(port)
                FileHandle.standardError.write(Data("NET-HUB: client \(port) is gone (errno \(e)) — removed from the segment\n".utf8))
                return
            }
            noteDrop(port, e)
        }
    }
}

var netHubPath: String?

var netHub: NetHub?
