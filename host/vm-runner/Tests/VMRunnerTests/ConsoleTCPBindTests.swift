import Darwin
import Foundation
import XCTest

@testable import VMRunner

// #3910: the --console-tcp socket setup must return a verdict the run can
// fail on — a held port comes back as ("bind", EADDRINUSE) so
// startTCPConsoleBridge exits non-zero instead of booting listener-less.
final class ConsoleTCPBindTests: XCTestCase {
    /// Hold 127.0.0.1 on a kernel-assigned port; returns (listener fd, port).
    private func holdLoopbackPort() throws -> (Int32, UInt16) {
        let s = socket(AF_INET, SOCK_STREAM, 0)
        guard s >= 0 else { throw NSError(domain: NSPOSIXErrorDomain, code: Int(errno)) }
        var reuse: Int32 = 1
        _ = setsockopt(s, SOL_SOCKET, SO_REUSEADDR, &reuse, socklen_t(MemoryLayout<Int32>.size))
        var addr = sockaddr_in()
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = 0
        addr.sin_addr.s_addr = inet_addr("127.0.0.1")
        let brc = withUnsafePointer(to: &addr) { p in
            p.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.bind(s, $0, socklen_t(MemoryLayout<sockaddr_in>.size)) }
        }
        XCTAssertEqual(brc, 0)
        XCTAssertEqual(listen(s, 1), 0)
        var bound = sockaddr_in()
        var len = socklen_t(MemoryLayout<sockaddr_in>.size)
        let grc = withUnsafeMutablePointer(to: &bound) { p in
            p.withMemoryRebound(to: sockaddr.self, capacity: 1) { getsockname(s, $0, &len) }
        }
        XCTAssertEqual(grc, 0)
        return (s, UInt16(bigEndian: bound.sin_port))
    }

    func testHeldPortFailsBindWithEADDRINUSE() throws {
        // BSD semantics: SO_REUSEADDR does not let a second socket bind a
        // live listener's addr:port — that is exactly the squatter case.
        let (stub, port) = try holdLoopbackPort()
        defer { close(stub) }
        let r = consoleTCPBindListen(port: port, bindHost: "127.0.0.1")
        XCTAssertEqual(r.fd, -1)
        XCTAssertEqual(r.stage, "bind")
        XCTAssertEqual(r.err, EADDRINUSE)
    }

    func testFreePortReturnsListeningSocket() throws {
        let r = consoleTCPBindListen(port: 0, bindHost: "127.0.0.1")
        XCTAssertGreaterThanOrEqual(r.fd, 0)
        defer { close(r.fd) }
        var bound = sockaddr_in()
        var len = socklen_t(MemoryLayout<sockaddr_in>.size)
        _ = withUnsafeMutablePointer(to: &bound) { p in
            p.withMemoryRebound(to: sockaddr.self, capacity: 1) { getsockname(r.fd, $0, &len) }
        }
        let c = socket(AF_INET, SOCK_STREAM, 0)
        XCTAssertGreaterThanOrEqual(c, 0)
        defer { close(c) }
        let crc = withUnsafePointer(to: &bound) { p in
            p.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.connect(c, $0, socklen_t(MemoryLayout<sockaddr_in>.size)) }
        }
        XCTAssertEqual(crc, 0)
    }
}
