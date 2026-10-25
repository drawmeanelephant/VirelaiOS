import Foundation
import XCTest
@testable import VMAppKit
@testable import VMRunner

/// M202c (#3386, ADR 0051): the print lane, host half. A real
/// `HostIntSession.arm` on a temp share gives the lane dir, the epoch
/// and the key; the printer seam is mocked so a class-A run never
/// touches AppKit's print stack. Under XCTest `Bundle.main` is not an
/// app bundle — every job here takes the consent-free degrade: the
/// payload moves to destDir and the receipt says `saved`.
final class HostPrintTests: XCTestCase {
    private let key = String(repeating: "k", count: 64)

    private final class MockPrinter: HostPrinting {
        var presented: [URL] = []
        var outcome: HostPrintOutcome = .printed
        func present(_ url: URL, completion: @escaping (HostPrintOutcome) -> Void) {
            presented.append(url)
            completion(outcome)
        }
    }

    private func makeSession() throws -> (URL, URL, HostIntSession) {
        let dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("hostprint-tests-\(UUID().uuidString)", isDirectory: true)
        let dest = FileManager.default.temporaryDirectory
            .appendingPathComponent("hostprint-dest-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        try FileManager.default.createDirectory(at: dest, withIntermediateDirectories: true)
        addTeardownBlock {
            try? FileManager.default.removeItem(at: dir)
            try? FileManager.default.removeItem(at: dest)
        }
        try "#v1\nhost-int\t1000\t\(key)\tGOTABWM.ELF\n".write(
            to: dir.appendingPathComponent("SECRETS.TXT"), atomically: true, encoding: .utf8)
        let session = try XCTUnwrap(HostIntSession.arm(shareDir: dir, mask: HostInt.lanePrint, requested: ["print"]))
        return (dir, dest, session)
    }

    /// The DROP-shape payload: [name_len u8][name][size u64le].
    private func payload(_ name: String, _ size: UInt64) -> Data {
        var p = Data([UInt8(name.utf8.count)])
        p.append(Data(name.utf8))
        var s = size
        for _ in 0..<8 { p.append(UInt8(s & 0xff)); s >>= 8 }
        return p
    }

    /// Plant one signed PRINT the way the seat emits it.
    private func plantPrint(_ session: HostIntSession, seq: UInt32, payload: Data) throws {
        let rec = try XCTUnwrap(HostInt.encode(tag: .print, flags: 0, seq: seq,
                                               epoch: session.epoch, payload: payload,
                                               key: session.key))
        try rec.write(to: session.laneDir.appendingPathComponent(
            HostInt.recordName(tag: .print, seq: seq)))
    }

    private func stageJob(_ session: HostIntSession, name: String, _ bytes: Data) throws {
        try bytes.write(to: session.shareDir.appendingPathComponent("PRINT")
            .appendingPathComponent(name))
    }

    func testStartCreatesPrintDir() throws {
        let (dir, dest, session) = try makeSession()
        let lane = HostPrint(session: session, destDir: dest, printer: MockPrinter())
        XCTAssertTrue(lane.start())
        var isDir: ObjCBool = false
        XCTAssertTrue(FileManager.default.fileExists(
            atPath: dir.appendingPathComponent("PRINT").path, isDirectory: &isDir))
        XCTAssertTrue(isDir.boolValue)
    }

    /// The bare-runner posture (bundled=false): the consent-free degrade
    /// moves the payload to destDir — `saved`, never `printed`.
    func testVerifiedJobLandsInDest() throws {
        let (dir, dest, session) = try makeSession()
        let lane = HostPrint(session: session, destDir: dest, printer: MockPrinter())
        lane.bundled = false
        XCTAssertTrue(lane.start())
        let pdf = Data("%PDF-1.7 fake body".utf8)
        try stageJob(session, name: "REPORT.PDF", pdf)
        try plantPrint(session, seq: 1, payload: payload("REPORT.PDF", UInt64(pdf.count)))
        lane.tick()
        let landed = dest.appendingPathComponent("REPORT.PDF")
        XCTAssertEqual(try? Data(contentsOf: landed), pdf) // the PDF bytes arrive host-side
        // The staged payload left the share — consumed, not copied.
        XCTAssertFalse(FileManager.default.fileExists(
            atPath: dir.appendingPathComponent("PRINT/REPORT.PDF").path))
    }

    /// The bundled posture (default under XCTest): a verified job is
    /// presented to the print panel seam and the staged file is consumed
    /// when the outcome answers — the human's click is the consent.
    func testBundledJobPresentsPanel() throws {
        let (dir, dest, session) = try makeSession()
        let printer = MockPrinter()
        let lane = HostPrint(session: session, destDir: dest, printer: printer)
        lane.bundled = true
        XCTAssertTrue(lane.start())
        let pdf = Data("%PDF-1.7 fake body".utf8)
        try stageJob(session, name: "REPORT.PDF", pdf)
        try plantPrint(session, seq: 1, payload: payload("REPORT.PDF", UInt64(pdf.count)))
        lane.tick()
        XCTAssertEqual(printer.presented.map(\.lastPathComponent), ["REPORT.PDF"])
        XCTAssertFalse(FileManager.default.fileExists(
            atPath: dir.appendingPathComponent("PRINT/REPORT.PDF").path))
        XCTAssertTrue(try FileManager.default.contentsOfDirectory(atPath: dest.path).isEmpty)
    }

    func testAbsentPayloadIsGone() throws {
        let (_, dest, session) = try makeSession()
        let lane = HostPrint(session: session, destDir: dest, printer: MockPrinter())
        XCTAssertTrue(lane.start())
        try plantPrint(session, seq: 1, payload: payload("ABSENT.PDF", 5))
        lane.tick()
        XCTAssertTrue(try FileManager.default.contentsOfDirectory(atPath: dest.path).isEmpty)
    }

    func testNonPDFPayloadIsUnsupportedKind() throws {
        let (dir, dest, session) = try makeSession()
        let lane = HostPrint(session: session, destDir: dest, printer: MockPrinter())
        XCTAssertTrue(lane.start())
        try stageJob(session, name: "NOTAPDF.PDF", Data("plain text".utf8))
        try plantPrint(session, seq: 1, payload: payload("NOTAPDF.PDF", 10))
        lane.tick()
        XCTAssertTrue(try FileManager.default.contentsOfDirectory(atPath: dest.path).isEmpty)
        // Refused jobs stay staged (visible, never attributed to a print).
        XCTAssertTrue(FileManager.default.fileExists(
            atPath: dir.appendingPathComponent("PRINT/NOTAPDF.PDF").path))
    }

    func testNonPDFNameIsNameRejected() throws {
        let (_, dest, session) = try makeSession()
        let lane = HostPrint(session: session, destDir: dest, printer: MockPrinter())
        XCTAssertTrue(lane.start())
        try plantPrint(session, seq: 1, payload: payload("JOB.TXT", 5))
        lane.tick()
        XCTAssertTrue(try FileManager.default.contentsOfDirectory(atPath: dest.path).isEmpty)
    }

    func testForgedRecordIsBadMAC() throws {
        let (_, dest, session) = try makeSession()
        let lane = HostPrint(session: session, destDir: dest, printer: MockPrinter())
        XCTAssertTrue(lane.start())
        // A record signed under a foreign key fails verify → bad_mac,
        // deleted — never reaches the payload stage.
        let rec = try XCTUnwrap(HostInt.encode(tag: .print, flags: 0, seq: 1,
                                               epoch: session.epoch,
                                               payload: payload("X.PDF", 1),
                                               key: Data(repeating: 0x5A, count: 32)))
        try rec.write(to: session.laneDir.appendingPathComponent(
            HostInt.recordName(tag: .print, seq: 1)))
        lane.tick()
        XCTAssertTrue(try FileManager.default.contentsOfDirectory(atPath: dest.path).isEmpty)
    }

    func testForeignEpochIsStale() throws {
        let (_, dest, session) = try makeSession()
        let lane = HostPrint(session: session, destDir: dest, printer: MockPrinter())
        XCTAssertTrue(lane.start())
        let rec = try XCTUnwrap(HostInt.encode(tag: .print, flags: 0, seq: 1,
                                               epoch: session.epoch ^ 0xFF,
                                               payload: payload("X.PDF", 1),
                                               key: session.key))
        try rec.write(to: session.laneDir.appendingPathComponent(
            HostInt.recordName(tag: .print, seq: 1)))
        lane.tick()
        XCTAssertTrue(try FileManager.default.contentsOfDirectory(atPath: dest.path).isEmpty)
    }

    func testOversizePayloadRefused() throws {
        let (dir, dest, session) = try makeSession()
        let lane = HostPrint(session: session, destDir: dest, printer: MockPrinter())
        XCTAssertTrue(lane.start())
        var big = Data("%PDF-".utf8)
        big.append(Data(count: Int(HostPrint.fileMax))) // > fileMax total
        try stageJob(session, name: "BIG.PDF", big)
        try plantPrint(session, seq: 1, payload: payload("BIG.PDF", UInt64(big.count)))
        lane.tick()
        XCTAssertTrue(try FileManager.default.contentsOfDirectory(atPath: dest.path).isEmpty)
        XCTAssertTrue(FileManager.default.fileExists(
            atPath: dir.appendingPathComponent("PRINT/BIG.PDF").path))
    }
}
