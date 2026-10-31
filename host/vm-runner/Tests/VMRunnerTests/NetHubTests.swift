// M206a (issue #3813, ADR 0057 amendment): pins the --net-hub learning
// switch (learn, unicast forward, broadcast + multicast flood, drop to
// self, the bounded MAC table), the hub DHCP lease pool (one lease per
// MAC, bounded, never reissuing a held lease), and the --mac parser's
// named refusals. Everything runs WITHOUT a VM — NetHubSwitch /
// NetHubDhcp / netMacParse / netHubRecvFatal are pure state; only the
// burst-regression test binds real sockets (under /tmp, self-cleaning).
import Darwin
import XCTest

@testable import VMRunner

final class NetHubTests: XCTestCase {

    private func frame(dst: [UInt8], src: [UInt8], tag: UInt8 = 0) -> [UInt8] {
        // A minimal Ethernet II frame: dst(6) + src(6) + ethertype(2) +
        // a 32-byte payload — enough for the switch's 14-byte floor.
        return dst + src + [0x08, 0x00] + [UInt8](repeating: tag, count: 32)
    }

    private let macA: [UInt8] = [0x02, 0, 0, 0, 0, 0x0a]
    private let macB: [UInt8] = [0x02, 0, 0, 0, 0, 0x0b]
    private let macC: [UInt8] = [0x02, 0, 0, 0, 0, 0x0c]
    private let bcast: [UInt8] = [0xff, 0xff, 0xff, 0xff, 0xff, 0xff]
    private let mdns4: [UInt8] = [0x01, 0x00, 0x5e, 0x00, 0x00, 0xfb] // mDNS v4
    private let mdns6: [UInt8] = [0x33, 0x33, 0x00, 0x00, 0x00, 0xfb] // mDNS v6

    // learn: a src MAC maps to the port that sent the frame; a unicast
    // for it forwards to exactly that port.
    func testLearnAndUnicastForward() {
        let sw = NetHubSwitch()
        _ = sw.ingress(frame(dst: bcast, src: macA), from: "/h.cli-1")
        _ = sw.ingress(frame(dst: bcast, src: macB), from: "/h.cli-2")
        // C's frame for A's learned MAC goes to A's port only.
        let dests = sw.ingress(frame(dst: macA, src: macC), from: "/h.cli-3")
        XCTAssertEqual(dests, ["/h.cli-1"])
    }

    // A unicast to an unlearned MAC floods to every port except the
    // sender (the learning-switch default).
    func testUnknownUnicastFloods() {
        let sw = NetHubSwitch()
        _ = sw.ingress(frame(dst: bcast, src: macA), from: "/h.cli-1")
        _ = sw.ingress(frame(dst: bcast, src: macB), from: "/h.cli-2")
        let dests = sw.ingress(frame(dst: [0x02, 0, 0, 0, 0, 0x77], src: macA), from: "/h.cli-1")
        XCTAssertEqual(dests, ["/h.cli-2"])
    }

    // Broadcast floods to every port except the sender.
    func testBroadcastFloods() {
        let sw = NetHubSwitch()
        _ = sw.ingress(frame(dst: bcast, src: macA), from: "/h.cli-1")
        _ = sw.ingress(frame(dst: bcast, src: macB), from: "/h.cli-2")
        _ = sw.ingress(frame(dst: bcast, src: macC), from: "/h.cli-3")
        let dests = sw.ingress(frame(dst: bcast, src: macB), from: "/h.cli-2")
        XCTAssertEqual(Set(dests), Set(["/h.cli-1", "/h.cli-3"]))
    }

    // The two mDNS group MACs the card names flood the same way —
    // dst[0] bit0 is the whole rule, no special-casing.
    func testMulticastFloods() {
        let sw = NetHubSwitch()
        _ = sw.ingress(frame(dst: bcast, src: macA), from: "/h.cli-1")
        _ = sw.ingress(frame(dst: bcast, src: macB), from: "/h.cli-2")
        XCTAssertEqual(sw.ingress(frame(dst: mdns4, src: macA), from: "/h.cli-1"), ["/h.cli-2"])
        XCTAssertEqual(sw.ingress(frame(dst: mdns6, src: macA), from: "/h.cli-1"), ["/h.cli-2"])
    }

    // Drop to self: a unicast resolving to the sender's own port is not
    // returned to it.
    func testDropToSelf() {
        let sw = NetHubSwitch()
        _ = sw.ingress(frame(dst: bcast, src: macA), from: "/h.cli-1")
        _ = sw.ingress(frame(dst: bcast, src: macB), from: "/h.cli-2")
        // B sends to A's MAC — learned on /h.cli-1 — fine. But a frame
        // FROM A's port TO A's own MAC must not loop back.
        XCTAssertEqual(sw.ingress(frame(dst: macB, src: macA), from: "/h.cli-1"), ["/h.cli-2"])
        XCTAssertEqual(sw.ingress(frame(dst: macA, src: macA), from: "/h.cli-1"), [])
    }

    // A frame under the 14-byte Ethernet floor is dropped, not learned.
    func testShortFrameDropped() {
        let sw = NetHubSwitch()
        XCTAssertEqual(sw.ingress(Array(frame(dst: macA, src: macB)[0..<10]), from: "/h.cli-1"), [])
        XCTAssertTrue(sw.ports.isEmpty)
    }

    // The bounded table evicts the oldest learned MAC at the cap.
    func testMacTableBound() {
        let sw = NetHubSwitch()
        for i in 0..<NetHubSwitch.tableLimit {
            var src = macA
            src[5] = UInt8(i)
            _ = sw.ingress(frame(dst: bcast, src: src), from: "/h.cli-\(i)")
        }
        // One more — the first entry evicts.
        _ = sw.ingress(frame(dst: bcast, src: [0x02, 0, 0, 0, 0, 0xfe]), from: "/h.cli-new")
        // macA (index 0) is gone: a unicast for it now floods instead of
        // forwarding to /h.cli-0.
        var src = macA
        src[5] = UInt8(NetHubSwitch.tableLimit - 1)
        let dests = sw.ingress(frame(dst: macA, src: src), from: "/h.cli-\(NetHubSwitch.tableLimit - 1)")
        XCTAssertFalse(dests.contains("/h.cli-0"))
    }

    // A departed port leaves the segment: no more floods to it, and its
    // MAC re-learns on the port the same guest rejoins from.
    func testForgetDepartedPort() {
        let sw = NetHubSwitch()
        _ = sw.ingress(frame(dst: bcast, src: macA), from: "/h.cli-1")
        _ = sw.ingress(frame(dst: bcast, src: macB), from: "/h.cli-2")
        sw.forget("/h.cli-1")
        XCTAssertEqual(sw.ports, ["/h.cli-2"])
        XCTAssertEqual(sw.ingress(frame(dst: bcast, src: macB), from: "/h.cli-2"), [])
        _ = sw.ingress(frame(dst: bcast, src: macA), from: "/h.cli-3")
        XCTAssertEqual(sw.ingress(frame(dst: macA, src: macB), from: "/h.cli-2"), ["/h.cli-3"])
    }

    // The DHCP pool hands one lease per MAC, distinct per requester,
    // and a re-ask returns the same lease.
    func testDhcpLeasePerMac() {
        let dhcp = NetHubDhcp()
        let k1 = NetHubSwitch.key(macA[0...5])
        let k2 = NetHubSwitch.key(macB[0...5])
        XCTAssertEqual(dhcp.lease(for: k1), [10, 0, 0, 16])
        XCTAssertEqual(dhcp.lease(for: k2), [10, 0, 0, 17])
        XCTAssertEqual(dhcp.lease(for: k1), [10, 0, 0, 16]) // stable
    }

    // The bounded pool evicts the oldest lease at the cap and never
    // reissues a still-held tail.
    func testDhcpPoolBound() {
        let dhcp = NetHubDhcp()
        var keys: [UInt64] = []
        for i in 0..<NetHubDhcp.poolLimit {
            let k = UInt64(i) << 16
            keys.append(k)
            _ = dhcp.lease(for: k)
        }
        let held = (0..<NetHubDhcp.poolLimit).map { dhcp.lease(for: keys[$0]) }
        // Every held lease is distinct and inside the pool.
        XCTAssertEqual(Set(held).count, NetHubDhcp.poolLimit)
        for ip in held {
            XCTAssertTrue(ip[3] >= NetHubDhcp.poolFirst)
            XCTAssertTrue(Int(ip[3]) < Int(NetHubDhcp.poolFirst) + NetHubDhcp.poolLimit)
        }
    }

    // The wedge regression at socket level (real bound sockets in /tmp —
    // the rest of this suite is pure state): a bound hub socket must
    // absorb a burst far past the 4096-byte default SO_RCVBUF (~2
    // datagrams of 1400 B before the sender's sendto fails ENOBUFS). 48
    // frames of 1200 B sent while the owner is not draining must all
    // queue; the pre-fix socket refused the burst at sendto, which is how
    // hub-transiting legs lost frames silently mid-run.
    func testHubSocketAbsorbsBurst() throws {
        let dir = "/tmp/nethub-test-\(getpid())"
        try? FileManager.default.removeItem(atPath: dir)
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(atPath: dir) }
        let owner = try NetHub.attach(path: dir + "/h.sock")
        defer { owner.unlinkBoundPaths(); close(owner.fd) }
        // The queue the socket actually got: far past the 4 KiB default.
        var rcv: Int32 = 0
        var rcvLen = socklen_t(MemoryLayout<Int32>.size)
        XCTAssertEqual(getsockopt(owner.fd, SOL_SOCKET, SO_RCVBUF, &rcv, &rcvLen), 0)
        XCTAssertGreaterThan(rcv, 64 * 1024, "SO_RCVBUF must be deepened, got \(rcv)")
        // A bound sender (macOS wants a name on unix datagram sockets).
        let sender = socket(AF_UNIX, SOCK_DGRAM, 0)
        XCTAssertGreaterThanOrEqual(sender, 0)
        defer { close(sender); unlink(dir + "/s.sock") }
        guard var senderAddr = NetHub.unixAddr(dir + "/s.sock") else { return XCTFail("sender path") }
        let bindRC = withUnsafePointer(to: &senderAddr) { p in
            p.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.bind(sender, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        XCTAssertEqual(bindRC, 0)
        var addr = owner.hubAddr
        let frame = [UInt8](repeating: 0x5a, count: 1200)
        var sent = 0
        for _ in 0..<48 {
            let rc = withUnsafePointer(to: &addr) { p in
                p.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                    sendto(sender, frame, frame.count, 0, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
                }
            }
            if rc >= 0 { sent += 1 }
        }
        XCTAssertEqual(sent, 48, "the hub socket must queue the whole burst (old default failed at ~2)")
        var buf = [UInt8](repeating: 0, count: 2048)
        var got = 0
        while recv(owner.fd, &buf, buf.count, 0) > 0 { got += 1 }
        XCTAssertEqual(got, 48, "every queued datagram must be deliverable, not dropped")
    }

    // The host->guest userspace queue (the socketpair-side half of the
    // wedge fix — the pair keeps its 4096-byte kernel buffer because
    // deepening SO_RCVBUF broke VZ delivery): sends past the kernel
    // queue must hold in userspace and flush in order, not drop.
    func testGuestQueueHoldsBurstInOrder() throws {
        var fds: [Int32] = [0, 0]
        XCTAssertEqual(socketpair(AF_UNIX, SOCK_DGRAM, 0, &fds), 0)
        let saved = netCaptureReadSocket
        netCaptureReadSocket = FileHandle(fileDescriptor: fds[1], closeOnDealloc: true)
        defer { netCaptureReadSocket = saved; close(fds[0]) }
        let q = NetGuestQueue()
        // 24 frames of 1000 B: the pair admits ~3-4, the rest queue.
        for i in 0..<24 { q.send([UInt8](repeating: UInt8(i), count: 1000)) }
        var buf = [UInt8](repeating: 0, count: 2048)
        var got = [Int]()
        // Interleave reads with flushes — the queue must cough up every
        // held frame as space frees.
        for _ in 0..<64 {
            var tv = timeval(tv_sec: 0, tv_usec: 10_000)
            setsockopt(fds[0], SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
            let n = recv(fds[0], &buf, buf.count, 0)
            if n > 0 { got.append(Int(buf[0])) } else { q.flush() }
            if got.count == 24 { break }
        }
        XCTAssertEqual(got, Array(0..<24), "every queued frame must arrive, in send order")
    }

    // The rx loop's break verdict (the mid-run-wedge fix): only a dead fd
    // stops the loop — every other errno is transient and must be ridden,
    // because a silently exited rx thread takes a whole relay direction
    // down while the rest of the runner keeps working.
    func testRecvFatalVerdict() {
        XCTAssertTrue(netHubRecvFatal(EBADF))
        XCTAssertTrue(netHubRecvFatal(ENOTSOCK))
        for e in [EAGAIN, EINTR, ECONNREFUSED, ENOBUFS, ENOENT, ETIMEDOUT, EMSGSIZE, ENOMEM] {
            XCTAssertFalse(netHubRecvFatal(e), "errno \(e) must be ridden, not fatal")
        }
    }

    // --mac parse: the locally-administered-unicast acceptance plus each
    // named refusal.
    func testMacParse() {
        guard case .ok(let mac) = netMacParse("02:00:00:00:00:0a") else {
            return XCTFail("02:00:00:00:00:0a must parse")
        }
        XCTAssertEqual(mac, [0x02, 0, 0, 0, 0, 0x0a])
        guard case .ok = netMacParse("fe:dc:ba:98:76:54") else {
            return XCTFail("uppercase/any local unicast must parse")
        }
        XCTAssertEqual(netEffectiveMAC(), netDefaultMAC) // no override -> fixed default
    }

    func testMacParseRefusals() {
        // Malformed: wrong count, bad octet, long octet.
        if case .malformed = netMacParse("02:00:00:00:00") {} else { XCTFail("5 octets must refuse malformed") }
        if case .malformed = netMacParse("02:00:00:00:00:0a:0b") {} else { XCTFail("7 octets must refuse malformed") }
        if case .malformed = netMacParse("02:00:00:00:00:0g") {} else { XCTFail("non-hex must refuse malformed") }
        if case .malformed = netMacParse("02:00:00:00:00:00a") {} else { XCTFail("3-digit octet must refuse malformed") }
        if case .malformed = netMacParse("") {} else { XCTFail("empty must refuse malformed") }
        // Multicast: bit0 of octet 0 set — broadcast and group MACs.
        if case .multicast = netMacParse("ff:ff:ff:ff:ff:ff") {} else { XCTFail("broadcast must refuse multicast") }
        if case .multicast = netMacParse("01:00:5e:00:00:fb") {} else { XCTFail("mDNS group must refuse multicast") }
        if case .multicast = netMacParse("33:33:00:00:00:fb") {} else { XCTFail("v6 group must refuse multicast") }
        // Universally administered: u/l bit clear.
        if case .notLocal = netMacParse("00:00:00:00:00:01") {} else { XCTFail("UAA must refuse notLocal") }
        if case .notLocal = netMacParse("3c:22:fb:00:11:22") {} else { XCTFail("an Apple OUI must refuse notLocal") }
    }
}
