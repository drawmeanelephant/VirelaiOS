// M206a (issue #3813, ADR 0057 amendment): pins the --net-hub learning
// switch (learn, unicast forward, broadcast + multicast flood, drop to
// self, the bounded MAC table), the hub DHCP lease pool (one lease per
// MAC, bounded, never reissuing a held lease), and the --mac parser's
// named refusals. Runs WITHOUT a VM or a bound socket — NetHubSwitch /
// NetHubDhcp / netMacParse are pure state.
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
