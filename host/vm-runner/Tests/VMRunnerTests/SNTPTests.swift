// M83b (#1775): pins the host SNTP responder's wire behaviour. Runs WITHOUT
// a VM (VSNTP imports no Virtualization).
import XCTest

@testable import VSNTP

final class SNTPTests: XCTestCase {
    let hostIP: [UInt8] = [10, 0, 0, 1]
    let guestIP: [UInt8] = [10, 0, 0, 2]
    let unix = 1_789_043_696.0
    let transmit: [UInt8] = [0xE9, 0x12, 0x34, 0x56, 0x80, 0x00, 0x00, 0x01]

    func responder(clock: Double? = nil, skew: Int64 = 0, port: UInt16 = 123) -> SNTPResponder {
        let c = clock ?? unix
        return SNTPResponder(hostIP: hostIP, hostPort: port, skew: skew, clock: { c })
    }

    func request(vn: UInt8 = 4, mode: UInt8 = 3, poll: UInt8 = 6, dstIP: [UInt8]? = nil,
                 dstPort: UInt16 = 123, payloadLen: Int = 48, proto: UInt8 = 17,
                 ihl: UInt8 = 5, fragOff: UInt8 = 0, mf: Bool = false) -> [UInt8] {
        var p = [UInt8](repeating: 0, count: payloadLen)
        if payloadLen > 0 { p[0] = (vn << 3) | mode }
        if payloadLen > 2 { p[2] = poll }
        if payloadLen >= 48 { p[40...47] = transmit[0...7] }
        var f = [UInt8](repeating: 0, count: 42 + payloadLen)
        f[0...5] = [0x02, 0, 0, 0, 0, 0x02]
        f[6...11] = [0x02, 0, 0, 0, 0, 0x01]
        f[12] = 0x08
        f[14] = 0x40 | ihl
        let total = 28 + payloadLen
        f[16] = UInt8(total >> 8)
        f[17] = UInt8(total & 0xff)
        f[18] = 0x12
        f[19] = 0x34
        f[20] = (mf ? 0x20 : 0) | fragOff
        f[22] = 64
        f[23] = proto
        f[26...29] = guestIP[0...3]
        f[30...33] = (dstIP ?? hostIP)[0...3]
        f[34] = 0xC0
        f[35] = 0x01
        f[36] = UInt8(dstPort >> 8)
        f[37] = UInt8(dstPort & 0xff)
        f[38] = UInt8((8 + payloadLen) >> 8)
        f[39] = UInt8((8 + payloadLen) & 0xff)
        f[42...] = p[0...]
        return f
    }

    func ans(_ r: SNTPResponder, _ f: [UInt8]) -> SNTPResponder.Answer? { r.answer(f, f.count) }

    func testNtpTimestampConversion() {
        XCTAssertEqual(SNTPResponder.ntpTimestamp(unix: unix), [0xEE, 0x4D, 0x22, 0x70, 0, 0, 0, 0])
        XCTAssertEqual(0xEE4D2270, 3_998_032_496)
        XCTAssertEqual(SNTPResponder.ntpTimestamp(unix: unix + 0.5), [0xEE, 0x4D, 0x22, 0x70, 0x80, 0, 0, 0])
        XCTAssertEqual(SNTPResponder.ntpTimestamp(unix: 0), [0x83, 0xAA, 0x7E, 0x80, 0, 0, 0, 0])
    }

    func testPinnedReplyPayload() {
        let a = ans(responder(), request())!
        let expect: [UInt8] = [
            0x24, 0x02, 0x06, 0xEC, 0, 0, 0, 0, 0, 0, 0, 0, 0x56, 0x49, 0x52, 0x45,
            0xEE, 0x4D, 0x22, 0x70, 0, 0, 0, 0,
            0xE9, 0x12, 0x34, 0x56, 0x80, 0x00, 0x00, 0x01,
            0xEE, 0x4D, 0x22, 0x70, 0, 0, 0, 0,
            0xEE, 0x4D, 0x22, 0x70, 0, 0, 0, 0,
        ]
        XCTAssertEqual(Array(a.frame[42...]), expect)
        XCTAssertEqual(a.originate, transmit)
        XCTAssertEqual(a.servedUnix, 1_789_043_696)
    }

    func testSkewApplied() {
        let plus = ans(responder(skew: 3600), request())!
        XCTAssertEqual(Array(plus.frame[74..<78]), SNTPResponder.ntpTimestamp(unix: unix + 3600)[0..<4].map { $0 })
        XCTAssertEqual(plus.servedUnix, 1_789_043_696 + 3600)
        let minus = ans(responder(skew: -30), request())!
        XCTAssertEqual(Array(minus.frame[74..<78]), SNTPResponder.ntpTimestamp(unix: unix - 30)[0..<4].map { $0 })
        XCTAssertEqual(minus.servedUnix, 1_789_043_696 - 30)
    }

    func testVersionEchoed() {
        for vn: UInt8 in 1...4 {
            let a = ans(responder(), request(vn: vn))!
            XCTAssertEqual(a.frame[42], (vn << 3) | 4)
        }
    }

    func testFractionalClockOrdersT2BeforeT3() {
        let a = ans(responder(clock: unix + 0.5), request())!
        XCTAssertEqual(Array(a.frame[42 + 16..<42 + 24]), SNTPResponder.ntpTimestamp(unix: unix))
        XCTAssertEqual(Array(a.frame[42 + 32..<42 + 40]), SNTPResponder.ntpTimestamp(unix: unix + 0.5))
    }

    func testIgnored() {
        let r = responder()
        XCTAssertNil(ans(r, request(dstIP: [10, 0, 0, 9])))
        XCTAssertNil(ans(r, request(dstPort: 124)))
        XCTAssertNil(ans(r, request(payloadLen: 47)))
        XCTAssertNil(ans(r, request(payloadLen: 0)))
        XCTAssertNil(ans(r, request(mode: 4)))
        XCTAssertNil(ans(r, request(mode: 1)))
        XCTAssertNil(ans(r, request(vn: 0)))
        XCTAssertNil(ans(r, request(vn: 5)))
        XCTAssertNil(ans(r, request(proto: 6)))
        XCTAssertNil(ans(r, request(fragOff: 1)))
        XCTAssertNil(ans(r, request(mf: true)))
        XCTAssertNil(ans(r, request(ihl: 6)))
        XCTAssertNil(r.answer(request(), 40))
        XCTAssertNotNil(ans(r, request(payloadLen: 60)))
    }

    func testCustomPort() {
        XCTAssertNil(ans(responder(port: 5123), request()))
        XCTAssertNotNil(ans(responder(port: 5123), request(dstPort: 5123)))
    }

    func testFrameAddressingAndChecksums() {
        let f = ans(responder(), request())!.frame
        XCTAssertEqual(f.count, 90)
        XCTAssertEqual(Array(f[0...5]), [0x02, 0, 0, 0, 0, 0x01])
        XCTAssertEqual(Array(f[6...11]), [0x02, 0, 0, 0, 0, 0x02])
        XCTAssertEqual(Array(f[26...29]), hostIP)
        XCTAssertEqual(Array(f[30...33]), guestIP)
        XCTAssertEqual(Array(f[34...35]), [0, 123])
        XCTAssertEqual(Array(f[36...37]), [0xC0, 0x01])
        XCTAssertEqual(Array(f[38...39]), [0, 56])
        XCTAssertEqual(Array(f[16...17]), [0, 76])

        func fold(_ words: [UInt8]) -> UInt32 {
            var s: UInt32 = 0
            var i = 0
            while i < words.count {
                s += UInt32(words[i]) << 8 | UInt32(i + 1 < words.count ? words[i + 1] : 0)
                i += 2
            }
            while s >> 16 != 0 { s = (s & 0xffff) + (s >> 16) }
            return s
        }
        XCTAssertEqual(fold(Array(f[14..<34])), 0xffff)
        let pseudo = Array(f[26..<34]) + [0, 17, 0, 56] + Array(f[34...])
        XCTAssertEqual(fold(pseudo), 0xffff)
    }
}
