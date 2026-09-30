// M83b (#1775): the host-side SNTP responder behind `--net-sntp-respond`.
// Pure Swift/Foundation, zero Virtualization imports, so the query
// recognition, NTP timestamp conversion and frame construction are pinned
// by `swift test` without booting a VM. The clock is injected so the
// served timestamps are deterministic under test.
import Foundation

public struct SNTPResponder {
    public static let ntpUnixOffset: Int64 = 2_208_988_800

    public let hostIP: [UInt8]
    public let hostPort: UInt16
    public let hostMAC: [UInt8]
    public let skew: Int64
    private let clock: () -> Double

    public init(hostIP: [UInt8], hostPort: UInt16 = 123,
                hostMAC: [UInt8] = [0x02, 0x00, 0x00, 0x00, 0x00, 0x02],
                skew: Int64 = 0, clock: @escaping () -> Double) {
        precondition(hostIP.count == 4 && hostMAC.count == 6)
        self.hostIP = hostIP
        self.hostPort = hostPort
        self.hostMAC = hostMAC
        self.skew = skew
        self.clock = clock
    }

    public struct Answer {
        public let frame: [UInt8]
        public let guestIP: [UInt8]
        public let guestPort: UInt16
        public let servedUnix: Int64
        public let originate: [UInt8]
    }

    /// Ethernet/IPv4 (IHL 5, non-fragment) UDP to hostIP:hostPort carrying
    /// a >= 48-byte client-mode (3) SNTP request with version 1...4.
    public func isQuery(_ buf: [UInt8], _ n: Int) -> Bool {
        guard n <= buf.count, n >= 42 + 48 else { return false }
        guard buf[12] == 0x08 && buf[13] == 0x00 else { return false }
        guard buf[14] == 0x45 else { return false }
        guard (buf[20] & 0x3f) == 0 && buf[21] == 0 else { return false } // MF or fragment offset
        guard buf[23] == 17 else { return false }
        guard Array(buf[30..<34]) == hostIP else { return false }
        guard (UInt16(buf[36]) << 8 | UInt16(buf[37])) == hostPort else { return false }
        let udpLen = Int(UInt16(buf[38]) << 8 | UInt16(buf[39]))
        guard udpLen >= 8 + 48, 34 + udpLen <= n else { return false }
        let b0 = buf[42]
        let vn = (b0 >> 3) & 0x07
        return (b0 & 0x07) == 3 && (1...4).contains(vn)
    }

    /// Served time (host wall clock + skew) as unix seconds.
    private func served() -> Double { clock() + Double(skew) }

    /// The 64-bit NTP timestamp (era 0: seconds wrap in 2036) for a unix time.
    public static func ntpTimestamp(unix: Double) -> [UInt8] {
        let whole = unix.rounded(.down)
        let secs = UInt32(truncatingIfNeeded: Int64(whole) + ntpUnixOffset)
        let frac = UInt32(min(max((unix - whole) * 4_294_967_296.0, 0), 4_294_967_295.0))
        return be32(secs) + be32(frac)
    }

    static func be32(_ v: UInt32) -> [UInt8] {
        [UInt8(v >> 24), UInt8((v >> 16) & 0xff), UInt8((v >> 8) & 0xff), UInt8(v & 0xff)]
    }

    /// RFC 4330 server packet for the request payload `req` (>= 48 bytes).
    public static func replyPayload(request req: [UInt8], receive t2: Double, transmit t3: Double) -> [UInt8] {
        var p = [UInt8](repeating: 0, count: 48)
        p[0] = (req[0] & 0x38) | 4 // LI 0, echoed VN, mode 4
        p[1] = 2
        p[2] = req[2]
        p[3] = 0xEC
        p[12...15] = [0x56, 0x49, 0x52, 0x45] // "VIRE"
        p[16...23] = ntpTimestamp(unix: t3.rounded(.down))[0...7]
        p[24...31] = req[40...47]
        p[32...39] = ntpTimestamp(unix: t2)[0...7]
        p[40...47] = ntpTimestamp(unix: t3)[0...7]
        return p
    }

    /// Build the complete Ethernet reply frame, or nil when `buf` is not a query.
    public func answer(_ buf: [UInt8], _ n: Int) -> Answer? {
        guard isQuery(buf, n) else { return nil }
        let t2 = served()
        let req = [UInt8](buf[42..<(42 + 48)])
        let t3 = max(served(), t2)
        let payload = SNTPResponder.replyPayload(request: req, receive: t2, transmit: t3)

        let totalLen = 42 + payload.count
        var r = [UInt8](repeating: 0, count: totalLen)
        r[0...5] = buf[6...11]
        r[6...11] = hostMAC[0...5]
        r[12] = 0x08
        r[13] = 0x00
        r[14] = 0x45
        let ipTotal = UInt16(20 + 8 + payload.count)
        r[16] = UInt8(ipTotal >> 8)
        r[17] = UInt8(ipTotal & 0xff)
        r[18...19] = buf[18...19]
        r[22] = 64
        r[23] = 17
        r[26...29] = hostIP[0...3]
        r[30...33] = buf[26...29]
        let ipChk = SNTPResponder.checksum(r, 14, 34)
        r[24] = UInt8(ipChk >> 8)
        r[25] = UInt8(ipChk & 0xff)

        r[34] = UInt8(hostPort >> 8)
        r[35] = UInt8(hostPort & 0xff)
        r[36...37] = buf[34...35]
        let udpLen = UInt16(8 + payload.count)
        r[38] = UInt8(udpLen >> 8)
        r[39] = UInt8(udpLen & 0xff)
        r[42..<totalLen] = payload[0..<payload.count]

        let guestIP = [UInt8](buf[26...29])
        // Pseudo-header + datagram folded through the same RFC 1071 sum.
        var pseudo = hostIP + guestIP + [0, 17, UInt8(udpLen >> 8), UInt8(udpLen & 0xff)]
        pseudo += r[34..<totalLen]
        var udpChk = SNTPResponder.checksum(pseudo, 0, pseudo.count)
        if udpChk == 0 { udpChk = 0xffff }
        r[40] = UInt8(udpChk >> 8)
        r[41] = UInt8(udpChk & 0xff)

        return Answer(frame: r, guestIP: guestIP,
                      guestPort: UInt16(buf[34]) << 8 | UInt16(buf[35]),
                      servedUnix: Int64(t3.rounded(.down)),
                      originate: [UInt8](req[40...47]))
    }

    /// RFC 1071 one's-complement sum over `bytes[start..<end]`.
    static func checksum(_ bytes: [UInt8], _ start: Int, _ end: Int) -> UInt16 {
        var sum: UInt32 = 0
        var i = start
        while i + 1 < end {
            sum += (UInt32(bytes[i]) << 8) | UInt32(bytes[i + 1])
            i += 2
        }
        if i < end { sum += UInt32(bytes[i]) << 8 }
        while sum >> 16 != 0 { sum = (sum & 0xffff) + (sum >> 16) }
        return UInt16(~sum & 0xffff)
    }
}
