// Issue #2349 — pins the `:relay` responder's upstream plumbing. Runs
// WITHOUT a VM (VMTcp is pure Swift/Foundation; the "upstream" is a real
// loopback socket and guest-bound frames are captured through the
// injected `onFrame` sink).
//
// The two bugs this locks out, both observed live and worked around by
// M101a (#2313) and M107d (#2321):
//   * inbound starvation past ~1460 B: a stale (retransmitted) guest ack
//     permanently retreated the pump's equality gate, and the pump ran
//     one ≤192 B chunk per ACK at multi-second guest RTTs;
//   * retransmitted guest segments re-written to the upstream socket —
//     flintd saw every request twice.
import XCTest
@testable import VMTcp

final class NetTcpRelayTests: XCTestCase {
    let hostMAC: [UInt8] = [0x02, 0, 0, 0, 0, 0x02]
    let guestMAC: [UInt8] = [0x02, 0, 0, 0, 0, 0x01]
    let hostIP: [UInt8] = [10, 0, 0, 2]
    let guestIP: [UInt8] = [10, 0, 0, 2] // direction is encoded in the frame; contents are opaque to the relay
    let hostPort: UInt16 = 4748
    let guestPort: UInt16 = 8000
    let serverIsn: UInt32 = 0x12345678
    let guestIsn: UInt32 = 0x54321098

    private var frames: [[UInt8]] = []
    private var framesLock = NSLock()
    private var relayLogs: [String] = []
    private var logsLock = NSLock()

    private func makeRelay() -> NetTcpRelay {
        frames = []
        relayLogs = []
        let relay = NetTcpRelay()
        relay.hostMAC = hostMAC
        relay.hostPort = hostPort
        relay.serverIsn = serverIsn
        relay.onFrame = { frame in
            self.framesLock.lock()
            self.frames.append(frame)
            self.framesLock.unlock()
        }
        relay.onLog = { line in
            self.logsLock.lock()
            self.relayLogs.append(line)
            self.logsLock.unlock()
        }
        return relay
    }

    private var frameCount: Int {
        framesLock.lock()
        defer { framesLock.unlock() }
        return frames.count
    }

    private func frame(at i: Int) -> [UInt8] {
        framesLock.lock()
        defer { framesLock.unlock() }
        return frames[i]
    }

    private func logged(matching needle: String) -> Bool {
        logsLock.lock()
        defer { logsLock.unlock() }
        return relayLogs.contains { $0.contains(needle) }
    }

    /// A guest SYN carrying an MSS option (data offset 6) — the shape the
    /// guest kernel originates (kernel/src/tcp.zig `mss_option`).
    private func syn(mss: UInt16? = 1460, wnd: UInt16 = 1460) -> [UInt8] {
        var f = [UInt8]()
        _ = buildTcpFrameExplicit(&f, hostMAC, guestMAC, guestIP, hostIP, guestPort, hostPort, guestIsn, 0, 0x02, [])
        guard let mss else { return f }
        // Room for the 4-byte MSS option; bump the data offset to 6.
        f[46] = 0x60
        f[48] = UInt8(wnd >> 8)
        f[49] = UInt8(wnd & 0xff)
        f += [2, 4, UInt8(mss >> 8), UInt8(mss & 0xff)]
        return f
    }

    /// A guest data/ack segment (doff 5, ACK flag always on).
    private func seg(seq: UInt32, ack: UInt32, wnd: UInt16 = 1460, flags: UInt8 = 0x10, payload: [UInt8] = []) -> [UInt8] {
        var f = [UInt8]()
        let n = buildTcpFrameExplicit(&f, hostMAC, guestMAC, guestIP, hostIP, guestPort, hostPort, seq, ack, flags, payload)
        f[48] = UInt8(wnd >> 8)
        f[49] = UInt8(wnd & 0xff)
        return Array(f[0..<n])
    }

    /// Payload of an emitted guest-bound frame (the relay never sends
    /// options — data offset is 5).
    private func payload(of frame: [UInt8]) -> [UInt8] {
        XCTAssertEqual(frame[46] >> 4, 5)
        return Array(frame[54...])
    }

    private func waitFor(_ seconds: Double = 5.0, _ cond: () -> Bool) -> Bool {
        let deadline = Date().addingTimeInterval(seconds)
        while Date() < deadline {
            if cond() { return true }
            Thread.sleep(forTimeInterval: 0.005)
        }
        return cond()
    }

    private struct UpstreamTimeout: Error {}

    /// A real loopback listener on an ephemeral port; starts the relay
    /// against it and returns the accepted upstream fd.
    private func upstream(_ relay: NetTcpRelay) throws -> Int32 {
        let listener = socket(AF_INET, SOCK_STREAM, 0)
        XCTAssertGreaterThanOrEqual(listener, 0)
        var addr = sockaddr_in()
        addr.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = 0
        addr.sin_addr = in_addr(s_addr: INADDR_LOOPBACK.bigEndian)
        let bound = withUnsafePointer(to: &addr) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                Darwin.bind(listener, sa, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        XCTAssertEqual(bound, 0)
        XCTAssertEqual(Darwin.listen(listener, 1), 0)
        var len = socklen_t(MemoryLayout<sockaddr_in>.size)
        var actual = sockaddr_in()
        withUnsafeMutablePointer(to: &actual) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                _ = getsockname(listener, sa, &len)
            }
        }
        relay.start("127.0.0.1", UInt16(bigEndian: actual.sin_port))
        // A listener goes readable once a handshake completes — poll
        // before accept() so a starved dial fails at the bound instead
        // of parking the test inside the syscall (#2507).
        var pfd = pollfd(fd: listener, events: Int16(POLLIN), revents: 0)
        guard poll(&pfd, 1, 5000) == 1, (pfd.revents & Int16(POLLIN)) != 0 else {
            close(listener)
            throw UpstreamTimeout()
        }
        let up = accept(listener, nil, nil)
        XCTAssertGreaterThanOrEqual(up, 0)
        close(listener)
        // The socket is accepted but the reader thread may not have
        // cleared `connecting` yet — guest payload in that window is
        // dropped as .dialing (#2377) and this harness never
        // retransmits, so the request would never reach `up`. The
        // connect log posts after the flag clears; waiting for it
        // makes the forwarding path deterministic under a saturated
        // host.
        XCTAssertTrue(waitFor { self.logged(matching: "connected") })
        return up
    }

    private func writeUpstream(_ fd: Int32, _ bytes: [UInt8]) {
        let n = bytes.withUnsafeBufferPointer { p in write(fd, p.baseAddress, p.count) }
        XCTAssertEqual(n, bytes.count)
    }

    /// Read exactly `count` bytes from a blocking fd, giving up at the
    /// deadline. Every read() is gated on poll() for the time
    /// remaining, so zero arriving bytes cannot park the test inside
    /// the syscall — the old bare read() once held the suite ~30
    /// minutes when a starved relay never wrote (#2507).
    private func readUpstream(_ fd: Int32, _ count: Int, timeout: TimeInterval = 5) -> [UInt8] {
        var out = [UInt8]()
        var buf = [UInt8](repeating: 0, count: max(1, count))
        let deadline = Date().addingTimeInterval(timeout)
        while out.count < count {
            let ms = Int32((deadline.timeIntervalSinceNow * 1000).rounded(.up))
            guard ms > 0 else { break }
            var pfd = pollfd(fd: fd, events: Int16(POLLIN), revents: 0)
            let rc = poll(&pfd, 1, ms)
            if rc < 0 && errno == EINTR { continue }
            guard rc > 0 else { break }
            // Any revents — POLLIN, HUP, ERR — makes read() return at
            // once; only a silent socket blocks, and poll already
            // ruled that out.
            let n = read(fd, &buf, count - out.count)
            guard n > 0 else { break }
            out += buf[0..<n]
        }
        return out
    }

    private func noUpstreamBytes(_ fd: Int32) -> Bool {
        var pfd = pollfd(fd: fd, events: Int16(POLLIN), revents: 0)
        return poll(&pfd, 1, 50) == 0
    }

    // #2349 bug 1, part A — the inbound cliff. A 4000-byte upstream
    // response must reach the guest as MSS-bounded, window-paced,
    // in-order segments: nothing before the handshake ACK, ≤1460 per
    // segment, and never more than the advertised window in flight.
    func testLargeResponseFlowsMSSPacedAndWindowBounded() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(mss: 1460, wnd: 1460), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }

        let response = (0..<4000).map { UInt8($0 % 251) }
        writeUpstream(up, response)
        // Nothing may go out before the guest ACKs the SYN-ACK — the
        // guest's syn_sent state would drop it unrecoverably.
        XCTAssertFalse(waitFor(0.3) { self.frameCount > 0 })

        // Window 0 first: even an acked SYN-ACK opens nothing.
        XCTAssertFalse(relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 0), 54))
        _ = relay.pump()
        XCTAssertEqual(frameCount, 0)

        // The real window update opens 1460 bytes: one MSS segment goes.
        XCTAssertFalse(relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 1460), 54))
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 1 })
        XCTAssertEqual(payload(of: frame(at: 0)), Array(response[0..<1460]))
        XCTAssertEqual(tcpSeq(frame(at: 0)), serverIsn + 1)
        XCTAssertEqual(tcpAck(frame(at: 0)), guestIsn + 1)

        // Each ACK refills the window; the whole response lands verbatim.
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1 + 1460, wnd: 1460), 54)
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 2 })
        XCTAssertEqual(payload(of: frame(at: 1)), Array(response[1460..<2920]))
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1 + 2920, wnd: 1460), 54)
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 3 })
        XCTAssertEqual(payload(of: frame(at: 2)), Array(response[2920..<4000]))
    }

    // #2349 bug 1, part B — the observed deadlock: a retransmitted guest
    // segment's stale ack must not retreat the pump's ack state. This is
    // the sequence that starved flintd's >MTU read response.
    func testStaleRetransmittedAckCannotStallThePump() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }

        // The guest's request is delivered upstream once.
        let request = Array("{\"op\":\"read\"}".utf8)
        let req = seg(seq: guestIsn + 1, ack: serverIsn + 1, payload: request)
        XCTAssertEqual(relay.guestDataSegment(req, req.count), .forwarded(fresh: request.count))
        XCTAssertEqual(readUpstream(up, request.count), request)

        let response = (0..<3000).map { UInt8($0 % 253) }
        writeUpstream(up, response)
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1 + UInt32(request.count), ack: serverIsn + 1, wnd: 1460), 54)
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 1 }) // one window's worth

        // The guest's retransmitted request lands — its ack field is the
        // STALE value. Under the old code this overwrote the pump's
        // expected-ack forever and the guest never saw another byte; now
        // it is deduplicated AND moves no pump state.
        XCTAssertEqual(relay.guestDataSegment(req, req.count), .duplicate)
        _ = relay.pump()
        XCTAssertTrue(noUpstreamBytes(up))

        // The guest's next real ACK still releases the next window —
        // proof the stale pair was ignored.
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1 + UInt32(request.count), ack: serverIsn + 1 + 1460, wnd: 1460), 54)
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 2 })
        XCTAssertEqual(payload(of: frame(at: 1)), Array(response[1460..<2920]))
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1 + UInt32(request.count), ack: serverIsn + 1 + 2920, wnd: 1460), 54)
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 3 })
        XCTAssertEqual(payload(of: frame(at: 2)), Array(response[2920..<3000]))
    }

    // #2507 — the readUpstream deadline is real: a live upstream that
    // delivers fewer bytes than asked (then silence) returns at the
    // bound instead of parking inside read() — the failure mode that
    // once held the suite ~30 minutes.
    func testReadUpstreamDeadlineBoundsASilentUpstream() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }

        let t0 = Date()
        // Zero bytes ever: the forced-timeout leg.
        XCTAssertEqual(readUpstream(up, 16, timeout: 0.5), [])
        XCTAssertLessThan(Date().timeIntervalSince(t0), 4)

        // Two bytes arrive then silence: the partial leg returns what
        // came at the bound rather than waiting on the rest forever.
        let req = seg(seq: guestIsn + 1, ack: serverIsn + 1, payload: [0x52, 0x53])
        XCTAssertEqual(relay.guestDataSegment(req, req.count), .forwarded(fresh: 2))
        XCTAssertEqual(readUpstream(up, 16, timeout: 0.5), [0x52, 0x53])
    }

    // #2349 bug 2 — retransmit dedup on the guest->upstream direction.
    func testRetransmittedGuestSegmentWritesUpstreamOnce() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1), 54)

        let request = Array("{\"op\":\"append\",\"changes\":[]}".utf8)
        let seg1 = seg(seq: guestIsn + 1, ack: serverIsn + 1, payload: request)
        XCTAssertEqual(relay.guestDataSegment(seg1, seg1.count), .forwarded(fresh: request.count))
        XCTAssertEqual(readUpstream(up, request.count), request)

        // The byte-identical retransmit must write nothing more.
        XCTAssertEqual(relay.guestDataSegment(seg1, seg1.count), .duplicate)
        XCTAssertTrue(noUpstreamBytes(up))
        XCTAssertEqual(relay.currentSeqAck().ack, guestIsn + 1 + UInt32(request.count))
        XCTAssertTrue(logged(matching: "deduplicated"))

        // Partial overlap: a retransmit covering 100 already-delivered
        // bytes + 50 fresh ones forwards only the fresh suffix.
        let covered = [UInt8](repeating: 0x58, count: 150)
        let coveredSeq = guestIsn + 1 + UInt32(request.count) - 100
        let coveredSeg = seg(seq: coveredSeq, ack: serverIsn + 1, payload: covered)
        XCTAssertEqual(relay.guestDataSegment(coveredSeg, coveredSeg.count), .forwarded(fresh: 50))
        XCTAssertEqual(readUpstream(up, 50), Array(covered[100..<150]))
    }

    // An ahead-of-bound segment is dropped named — the relay cannot
    // reorder the upstream byte stream, and the honest ack (the in-order
    // bound, not seq+len) is what makes the guest retransmit the gap.
    func testOutOfOrderGuestSegmentIsDroppedNotForwarded() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1), 54)

        let ahead = seg(seq: guestIsn + 501, ack: serverIsn + 1, payload: [UInt8](repeating: 0x7a, count: 60))
        XCTAssertEqual(relay.guestDataSegment(ahead, ahead.count), .gap)
        XCTAssertTrue(noUpstreamBytes(up))
        XCTAssertEqual(relay.currentSeqAck().ack, guestIsn + 1) // did not overstate
        XCTAssertTrue(logged(matching: "ahead of the in-order bound"))
    }

    // #2366 — a segment whose seq sits exactly 2^31 from the in-order
    // bound computes rel == Int32.min either way; negating that used to
    // SIGTRAP the whole runner. Both directions are .gap, and the legs
    // one byte inside each edge keep their old meaning — ahead stays
    // .gap, behind stays a wholly-covered retransmit (.duplicate).
    func testSeqAtInt32MinOffsetIsAGapNotATrap() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1), 54)
        let bound = guestIsn &+ 1 // rcvNxt after begin

        // Exactly 2^31 ahead of the bound: rel == Int32.min — the old trap.
        let ahead31 = seg(seq: bound &+ 0x8000_0000, ack: serverIsn + 1, payload: [UInt8](repeating: 0x61, count: 10))
        XCTAssertEqual(relay.guestDataSegment(ahead31, ahead31.count), .gap)

        // Exactly 2^31 behind is the same wrapped delta — also .gap.
        let behind31 = seg(seq: bound &- 0x8000_0000, ack: serverIsn + 1, payload: [UInt8](repeating: 0x61, count: 10))
        XCTAssertEqual(relay.guestDataSegment(behind31, behind31.count), .gap)

        // One byte inside each edge: 2^31-1 ahead is still a gap;
        // 2^31-1 behind is a retransmit the dedup path already covers.
        let aheadEdge = seg(seq: bound &+ 0x7FFF_FFFF, ack: serverIsn + 1, payload: [UInt8](repeating: 0x61, count: 10))
        XCTAssertEqual(relay.guestDataSegment(aheadEdge, aheadEdge.count), .gap)
        let behindEdge = seg(seq: bound &- 0x7FFF_FFFF, ack: serverIsn + 1, payload: [UInt8](repeating: 0x61, count: 10))
        XCTAssertEqual(relay.guestDataSegment(behindEdge, behindEdge.count), .duplicate)

        // None of it went upstream, and the in-order bound never moved.
        XCTAssertTrue(noUpstreamBytes(up))
        XCTAssertEqual(relay.currentSeqAck().ack, bound)
        XCTAssertTrue(logged(matching: "ahead of the in-order bound"))
    }

    // Upstream EOF propagates as FIN once the buffered bytes drain.
    func testUpstreamCloseDeliversBufferedBytesThenFin() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        let response = (0..<2000).map { UInt8($0 % 241) }
        writeUpstream(up, response)
        close(up) // upstream is done: EOF

        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 4096), 54)
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 3 }) // 1460 + 540 + FIN
        XCTAssertEqual(payload(of: frame(at: 0)), Array(response[0..<1460]))
        XCTAssertEqual(payload(of: frame(at: 1)), Array(response[1460..<2000]))
        XCTAssertEqual(tcpFlags(frame(at: 2)) & 0x11, 0x11) // FIN+ACK
        // The FIN is emitted once.
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1 + 2001, wnd: 4096), 54)
        _ = relay.pump()
        Thread.sleep(forTimeInterval: 0.1)
        XCTAssertEqual(frameCount, 3)
    }

    // Named refusal: guest data for a relay whose upstream never
    // connected is an RST, not a silent sink.
    func testGuestDataWithNoUpstreamIsReset() {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let data = seg(seq: guestIsn + 1, ack: serverIsn + 1, payload: [UInt8](repeating: 0x62, count: 10))
        XCTAssertEqual(relay.guestDataSegment(data, data.count), .dead)
        XCTAssertEqual(frameCount, 1)
        XCTAssertEqual(tcpFlags(frame(at: 0)) & 0x14, 0x14) // RST+ACK
        XCTAssertTrue(relay.isDead)
    }

    // M179c: a duplicate-ACK storm is no longer an instant verdict —
    // every trio is a fast retransmit, and the stream dies only when the
    // shared attempts budget (retryLimit bursts) is spent. The named
    // RST is the same observable reset the old stallLimit emitted.
    func testUnacknowledgedInFlightBytesEventuallyReset() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }
        writeUpstream(up, [UInt8](repeating: 0x63, count: 100))
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 1460), 54)
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 1 })

        // retryLimit bursts, each taking three non-advancing ACKs; the
        // RST lands when the attempts budget is spent, i.e. one trio
        // past the last allowed resend.
        var killed = false
        for i in 1...NetTcpRelay.retryLimit * 3 + 3 {
            killed = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 0), 54)
            if killed { break }
            if i % 3 == 0 { XCTAssertTrue(logged(matching: "three duplicate ACKs")) }
        }
        XCTAssertTrue(killed)
        XCTAssertTrue(logged(matching: "retransmission attempts"))
        XCTAssertEqual(tcpFlags(frame(at: frameCount - 1)) & 0x14, 0x14)
    }

    // #2369 — death is a teardown, not a leak: the stall-RST path shuts
    // the upstream fd down in the same call, so the accepted server
    // side reads EOF instead of holding a live connection that only an
    // unrelated SYN purge would ever reap.
    func testDeadRelayClosesTheUpstream() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }
        writeUpstream(up, [UInt8](repeating: 0x63, count: 100))
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 1460), 54)
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 1 })

        var killed = false
        for _ in 0..<NetTcpRelay.retryLimit * 3 + 3 {
            killed = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 0), 54)
            if killed { break }
        }
        XCTAssertTrue(killed)
        XCTAssertTrue(relay.isDead)

        // The fd was shut the moment the RST went out — the server side
        // (which the relay never wrote to, so POLLIN can only be the
        // close) reads EOF within the bound.
        XCTAssertTrue(waitFor {
            var pfd = pollfd(fd: up, events: Int16(POLLIN), revents: 0)
            guard poll(&pfd, 1, 50) > 0 else { return false }
            var one: [UInt8] = [0]
            return read(up, &one, 1) == 0
        })
    }

    // #2368 — the tx buffer is bounded: with the guest's window shut,
    // the reader parks at the high-water mark and the backpressure
    // lands on the upstream socket's own TCP window instead of host
    // RAM. Opening the guest's window drains below the low-water mark
    // and reads (and upstream writes) resume.
    func testTxHighWaterBackpressuresUpstream() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(mss: 1460, wnd: 0), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }
        // Handshake ACK, window still shut: nothing may drain.
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 0), 54)
        _ = relay.pump()
        XCTAssertEqual(frameCount, 0)

        // Non-blocking upstream writes feed the staging buffer until
        // it sits at the cap. The buffer bound is asserted on
        // pendingUpstreamBytes itself — the kernel's send-buffer size
        // is autotuned and proves nothing on its own.
        let fl = fcntl(up, F_GETFL, 0)
        XCTAssertEqual(fcntl(up, F_SETFL, fl | O_NONBLOCK), 0)
        let chunk = [UInt8](repeating: 0x41, count: 8192)
        var total = 0
        XCTAssertTrue(waitFor(15) {
            let n = chunk.withUnsafeBufferPointer { Darwin.write(up, $0.baseAddress, $0.count) }
            if n > 0 { total += n }
            return relay.pendingUpstreamBytes >= NetTcpRelay.txHighWater
        })
        XCTAssertGreaterThanOrEqual(total, NetTcpRelay.txHighWater)
        // Bounded: the cap plus at most one in-flight read — never the
        // unbounded growth of #2368. The reader names it on the log.
        XCTAssertLessThan(relay.pendingUpstreamBytes, NetTcpRelay.txHighWater + 8192)
        XCTAssertTrue(waitFor { self.logged(matching: "high-water") })

        // And it stays there: nothing drains with the window shut, so
        // a parked reader takes no more bytes from the socket.
        let parked = relay.pendingUpstreamBytes
        Thread.sleep(forTimeInterval: 0.3)
        XCTAssertEqual(relay.pendingUpstreamBytes, parked)

        // The window opens: each ACK'd window drains a chunk of the
        // buffer; once pending drops below the low-water mark the
        // reader un-parks and upstream bytes flow again. The guest
        // ACKs exactly send-next each leg (currentSeqAck's seq).
        var resumed = false
        for _ in 0..<12 {
            _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: relay.currentSeqAck().seq, wnd: 0xFFFF), 54)
            _ = relay.pump()
            if relay.pendingUpstreamBytes < NetTcpRelay.txLowWater {
                let drained = relay.pendingUpstreamBytes
                _ = chunk.withUnsafeBufferPointer { Darwin.write(up, $0.baseAddress, $0.count) }
                resumed = waitFor(2, { relay.pendingUpstreamBytes > drained })
                break
            }
        }
        XCTAssertTrue(resumed)
        XCTAssertGreaterThan(frameCount, 0)
    }

    // #2377 — an ACK for bytes never sent earns no credit: send credit
    // stays bounded by the advertised window, not the ack's altitude.
    // Unclamped, ~2^30 ahead would have released the whole buffered
    // response in one go.
    func testAckBeyondSendNextEarnsNoCredit() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }
        let response = (0..<4000).map { UInt8($0 % 251) }
        writeUpstream(up, response)

        // The handshake ACK claims ~2^30 bytes past send-next. Clamped,
        // the advertised window still bounds: one 1460-byte segment.
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn &+ 1 &+ 0x4000_0000, wnd: 1460), 54)
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 1 })
        XCTAssertEqual(payload(of: frame(at: 0)), Array(response[0..<1460]))
        _ = relay.pump()
        Thread.sleep(forTimeInterval: 0.1)
        XCTAssertEqual(frameCount, 1)
        XCTAssertTrue(logged(matching: "past send-next"))
    }

    // #2377 — connect failure is an immediate named RST, not a dead
    // relay the guest only discovers on its next data segment (and
    // server-speaks-first protocols would never get that far).
    func testConnectFailureEmitsReset() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        defer { relay.shutdown() }
        // A port with nothing listening: bind, learn it, let it go.
        let listener = socket(AF_INET, SOCK_STREAM, 0)
        XCTAssertGreaterThanOrEqual(listener, 0)
        var addr = sockaddr_in()
        addr.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = 0
        addr.sin_addr = in_addr(s_addr: INADDR_LOOPBACK.bigEndian)
        let bound = withUnsafePointer(to: &addr) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                Darwin.bind(listener, sa, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        XCTAssertEqual(bound, 0)
        var len = socklen_t(MemoryLayout<sockaddr_in>.size)
        var actual = sockaddr_in()
        withUnsafeMutablePointer(to: &actual) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                _ = getsockname(listener, sa, &len)
            }
        }
        close(listener)

        relay.start("127.0.0.1", UInt16(bigEndian: actual.sin_port))
        // ECONNREFUSED lands on the reader thread → RST to the guest.
        XCTAssertTrue(waitFor { self.frameCount == 1 })
        XCTAssertEqual(tcpFlags(frame(at: 0)) & 0x14, 0x14)
        XCTAssertTrue(relay.isDead)
        XCTAssertTrue(logged(matching: "connect"))
    }

    // #2377 — start() must not run the blocking connect() on the
    // capture thread: it returns at once and the dial happens on the
    // reader. Guest payload mid-dial is dropped — never written to a
    // half-dialed socket and never RST.
    func testStartDoesNotDialOnTheCallingThread() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        defer { relay.shutdown() }
        // A blackholed upstream — a blocking connect would park the
        // caller for the OS timeout (~75 s); async it returns at once.
        let t0 = Date()
        relay.start("10.255.255.1", 65000)
        XCTAssertLessThan(Date().timeIntervalSince(t0), 5.0)

        let data = seg(seq: guestIsn + 1, ack: serverIsn + 1, payload: [UInt8](repeating: 0x64, count: 8))
        let delivery = relay.guestDataSegment(data, data.count)
        // The dial is usually still in flight here (.dialing); on a
        // host with no route the connect may already have failed
        // (.dead). Both prove nothing was written to a half-dialed
        // socket.
        XCTAssertTrue(delivery == .dialing || delivery == .dead)
        if delivery == .dialing { XCTAssertEqual(frameCount, 0) }
    }

    // --- M179c (#3152): retention + retransmission + injected loss --------

    // Retention: an emitted segment's bytes are kept until the guest's
    // ack covers them — the RTO timer resends the same seq/payload when
    // the ack never comes.
    func testUnackedBytesAreRetransmittedOnRTO() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }
        let body = [UInt8](repeating: 0x65, count: 100)
        writeUpstream(up, body)
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 1460), 54)
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 1 })
        XCTAssertEqual(payload(of: frame(at: 0)), body)

        // No ack: the floor RTO (~200 ms) resends the same bytes at the
        // same seq — proof the relay held them.
        XCTAssertTrue(waitFor(3) { self.frameCount >= 2 })
        XCTAssertEqual(payload(of: frame(at: 1)), body)
        XCTAssertEqual(tcpSeq(frame(at: 1)), tcpSeq(frame(at: 0)))
        XCTAssertTrue(logged(matching: "retransmitted 100 bytes"))
        XCTAssertTrue(logged(matching: "RTO"))

        // The ack then arrives: the flight disarms — no further resends.
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1 + 100, wnd: 1460), 54)
        let settled = frameCount
        Thread.sleep(forTimeInterval: 0.5)
        XCTAssertEqual(frameCount, settled)
        XCTAssertEqual(relay.pendingUpstreamBytes, 0)
    }

    // Three duplicate ACKs trigger a fast retransmit without waiting
    // for the RTO.
    func testThreeDuplicateAcksTriggerFastRetransmit() throws {
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }
        let body = (0..<2000).map { UInt8($0 % 233) }
        writeUpstream(up, body)
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 4096), 54)
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 2 }) // 1460 + 540

        // The guest's bound acks only the first segment (1460) — the
        // first moves `peerAck` forward, then three that advance
        // nothing resend from the oldest unacked byte.
        for _ in 0..<4 {
            _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1 + 1460, wnd: 4096), 54)
        }
        XCTAssertEqual(frameCount, 3)
        XCTAssertEqual(tcpSeq(frame(at: 2)), serverIsn + 1 + 1460)
        XCTAssertEqual(payload(of: frame(at: 2)), Array(body[1460..<2000]))
        XCTAssertTrue(logged(matching: "three duplicate ACKs"))
    }

    // Reordering after a dropped segment delivers in order: emits
    // 1,2,3 with #3 lost (rx:3), the guest dup-acks at the gap, the
    // resend fills it — the sequence stream stays complete and
    // in-order past the hole.
    func testDroppedSegmentIsRefilledInOrder() throws {
        setenv("VIRELAI_NET_DROP", "rx:3", 1)
        defer { unsetenv("VIRELAI_NET_DROP") }
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }
        let body = (0..<4000).map { UInt8($0 % 241) }
        writeUpstream(up, body)
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 8192), 54)
        _ = relay.pump()
        // 4000 bytes at wnd 8192: emits 1460 + 1460 + 1080 — the third
        // emit is segment #3 and is dropped.
        XCTAssertTrue(waitFor { self.frameCount == 2 })
        XCTAssertEqual(payload(of: frame(at: 0)), Array(body[0..<1460]))
        XCTAssertEqual(payload(of: frame(at: 1)), Array(body[1460..<2920]))
        XCTAssertTrue(logged(matching: "dropped guest-bound segment #3"))

        // The guest covers 2920 and keeps acking the gap — a fast
        // retransmit lands the tail segment (emit #4 is not a 3rd).
        for _ in 0..<4 {
            _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1 + 2920, wnd: 8192), 54)
        }
        XCTAssertTrue(waitFor { self.frameCount >= 3 })
        XCTAssertEqual(tcpSeq(frame(at: 2)), serverIsn + 1 + 2920)
        XCTAssertEqual(payload(of: frame(at: 2)), Array(body[2920..<4000]))

        // The guest's cumulative ack then covers the whole body.
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1 + 4000, wnd: 8192), 54)
        XCTAssertEqual(relay.pendingUpstreamBytes, 0)
    }

    // The tx direction: every n-th guest->host segment never arrives —
    // the relay leaves template, ack state and the in-order bound
    // untouched, so the guest's own retransmit lands the payload once.
    func testInjectedTxDropIsInvisibleToTheStream() throws {
        setenv("VIRELAI_NET_DROP", "tx:2", 1)
        defer { unsetenv("VIRELAI_NET_DROP") }
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1), 54)

        let req1 = seg(seq: guestIsn + 1, ack: serverIsn + 1, payload: [UInt8](repeating: 0x71, count: 10))
        XCTAssertEqual(relay.guestDataSegment(req1, req1.count), .forwarded(fresh: 10))
        XCTAssertEqual(readUpstream(up, 10), [UInt8](repeating: 0x71, count: 10))
        // Segment #2 (every 2nd) is dropped before it can move anything.
        let req2 = seg(seq: guestIsn + 11, ack: serverIsn + 1, payload: [UInt8](repeating: 0x72, count: 10))
        XCTAssertEqual(relay.guestDataSegment(req2, req2.count), .dropped)
        XCTAssertTrue(noUpstreamBytes(up))
        XCTAssertEqual(relay.currentSeqAck().ack, guestIsn + 11)
        XCTAssertTrue(logged(matching: "dropped guest->host segment #2"))
        // The guest's retransmit (in-order, count #3 — seen survives) lands.
        XCTAssertEqual(relay.guestDataSegment(req2, req2.count), .forwarded(fresh: 10))
        XCTAssertEqual(readUpstream(up, 10), [UInt8](repeating: 0x72, count: 10))
    }

    // The drop switch's arithmetic: only the n-th emit in the armed
    // direction is discarded; the other direction and non-armed
    // segments are untouched; a malformed value arms nothing.
    func testDropSwitchArithmetic() throws {
        setenv("VIRELAI_NET_DROP", "rx:3,tx:9", 1)
        defer { unsetenv("VIRELAI_NET_DROP") }
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }
        writeUpstream(up, [UInt8](repeating: 0x66, count: 300))
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 1460), 54)
        // First emit is segment #1 — survives.
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 1 })
        XCTAssertTrue(logged(matching: "armed (rx:3,tx:9)"))

        // Malformed input never arms.
        setenv("VIRELAI_NET_DROP", "xyzzy", 1)
        let clean = makeRelay()
        clean.begin(synFrame: syn(), 58)
        let up2 = try upstream(clean)
        defer { close(up2); clean.shutdown() }
        writeUpstream(up2, [UInt8](repeating: 0x67, count: 10))
        _ = clean.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 1460), 54)
        _ = clean.pump()
        XCTAssertTrue(waitFor { self.frameCount == 2 })
        XCTAssertTrue(logged(matching: "ignoring malformed VIRELAI_NET_DROP"))
        unsetenv("VIRELAI_NET_DROP")
    }

    // With the switch absent the stream is byte-identical to today:
    // an unacked segment is not retransmitted while the guest acks on
    // schedule, and nothing logs a drop.
    func testNoDropsWhenSwitchUnset() throws {
        unsetenv("VIRELAI_NET_DROP")
        let relay = makeRelay()
        relay.begin(synFrame: syn(), 58)
        let up = try upstream(relay)
        defer { close(up); relay.shutdown() }
        writeUpstream(up, [UInt8](repeating: 0x68, count: 100))
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1, wnd: 1460), 54)
        _ = relay.pump()
        XCTAssertTrue(waitFor { self.frameCount == 1 })
        _ = relay.guestAckSegment(seg(seq: guestIsn + 1, ack: serverIsn + 1 + 100, wnd: 1460), 54)
        Thread.sleep(forTimeInterval: 0.3)
        XCTAssertEqual(frameCount, 1)
        XCTAssertFalse(logged(matching: "VIRELAI_NET_DROP"))
    }

    // The gate stages the rule as a `net-drop-<tag>` FILE, and a text
    // file ends in a newline — the parser must tolerate it or the file
    // channel can never arm (observed live: 'rx:5\n' off the staged
    // file logged "ignoring malformed VIRELAI_NET_DROP" and the loss
    // legs ran clean).
    func testNetDropSpecToleratesFileTrailingNewline() throws {
        let rules = try XCTUnwrap(parseNetDropSpec("rx:5\n"))
        XCTAssertEqual(rules.count, 1)
        XCTAssertEqual(rules[0].dir, .rx)
        XCTAssertEqual(rules[0].n, 5)
    }
}
