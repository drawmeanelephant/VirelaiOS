import Foundation
import XCTest
import VFWire
#if canImport(Darwin)
import Darwin
#endif

final class FileRenameTests: XCTestCase {
    private func temporaryShare() throws -> URL {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        addTeardownBlock { try FileManager.default.removeItem(at: root) }
        return root
    }

    private func write(_ root: URL, _ path: String, _ text: String) throws {
        try Data(text.utf8).write(to: root.appendingPathComponent(path))
    }

    private func read(_ root: URL, _ path: String) throws -> String {
        String(decoding: try Data(contentsOf: root.appendingPathComponent(path)), as: UTF8.self)
    }

    private func rename(_ root: URL, _ from: String, _ to: String, _ mode: VFWire.RenameMode) throws -> UInt8 {
        let payload = try XCTUnwrap(VFWire.buildRenamePayload(from: from, to: to))
        return VFWire.rename(root: root, payload: payload, mode: mode)
    }

    func testReplacementOpcodeAndStatusesAreAdditive() {
        XCTAssertEqual(VFWire.opRename, 0x09)
        XCTAssertEqual(VFWire.opDirOpen, 0x0d)
        XCTAssertEqual(VFWire.opDirPage, 0x0e)
        XCTAssertEqual(VFWire.opDirClose, 0x0f)
        XCTAssertEqual(VFWire.opReplace, 0x10)
        XCTAssertEqual(VFWire.stLimit, 7)
        XCTAssertEqual(VFWire.stPathLimit, 8)
        XCTAssertEqual(VFWire.stChanged, 9)
        XCTAssertEqual(VFWire.stAccess, 10)
        XCTAssertEqual(VFWire.stUnsupported, 11)
    }

    #if canImport(Darwin)
    func testTwoPublicationsReplaceExistingContent() throws {
        let root = try temporaryShare()
        try write(root, "output", "original")
        for text in ["first publication", "second publication"] {
            try write(root, "stage", text)
            XCTAssertEqual(try rename(root, "stage", "output", .replace), VFWire.stOk)
            XCTAssertEqual(try read(root, "output"), text)
            XCTAssertFalse(FileManager.default.fileExists(atPath: root.appendingPathComponent("stage").path))
        }
    }

    func testNoOverwriteRefusesAndKeepsBothFiles() throws {
        let root = try temporaryShare()
        try write(root, "output", "original")
        try write(root, "stage", "new")
        XCTAssertEqual(try rename(root, "stage", "output", .preserveExisting), VFWire.stExists)
        XCTAssertEqual(try read(root, "output"), "original")
        XCTAssertEqual(try read(root, "stage"), "new")
        XCTAssertEqual(try rename(root, "stage", "fresh", .preserveExisting), VFWire.stOk)
        XCTAssertEqual(try read(root, "fresh"), "new")
    }

    func testMissingSourceReportsErrorWithoutChangingPublication() throws {
        let root = try temporaryShare()
        try write(root, "output", "original")
        XCTAssertEqual(try rename(root, "missing", "output", .replace), VFWire.stNotFound)
        XCTAssertEqual(try read(root, "output"), "original")
    }

    func testNoOverwriteRefusesDanglingDestinationSymlink() throws {
        let root = try temporaryShare()
        try write(root, "stage", "new")
        let destination = root.appendingPathComponent("output")
        try FileManager.default.createSymbolicLink(atPath: destination.path, withDestinationPath: "missing")
        XCTAssertEqual(try rename(root, "stage", "output", .preserveExisting), VFWire.stExists)
        XCTAssertEqual(try FileManager.default.destinationOfSymbolicLink(atPath: destination.path), "missing")
        XCTAssertEqual(try read(root, "stage"), "new")
        XCTAssertEqual(try rename(root, "stage", "output", .replace), VFWire.stOk)
        XCTAssertEqual(try read(root, "output"), "new")
        XCTAssertFalse(FileManager.default.fileExists(atPath: root.appendingPathComponent("missing").path))
    }

    func testRejectedDirectoryReplacementPreservesBothEnds() throws {
        let root = try temporaryShare()
        try write(root, "output", "original")
        try FileManager.default.createDirectory(at: root.appendingPathComponent("stage"), withIntermediateDirectories: false)
        XCTAssertNotEqual(try rename(root, "stage", "output", .replace), VFWire.stOk)
        XCTAssertEqual(try read(root, "output"), "original")
        XCTAssertTrue(FileManager.default.fileExists(atPath: root.appendingPathComponent("stage").path))
    }

    func testPermissionDenialAtSourceAndDestinationPreservesPublication() throws {
        guard geteuid() != 0 else { throw XCTSkip("root bypasses host directory permission checks") }
        let root = try temporaryShare()
        let source = root.appendingPathComponent("source")
        let destination = root.appendingPathComponent("destination")
        try FileManager.default.createDirectory(at: source, withIntermediateDirectories: false)
        try FileManager.default.createDirectory(at: destination, withIntermediateDirectories: false)
        try write(root, "source/stage", "new")
        try write(root, "destination/output", "original")
        for denied in [source, destination] {
            XCTAssertEqual(chmod(denied.path, 0o500), 0)
            let status = try rename(root, "source/stage", "destination/output", .replace)
            let restored = chmod(denied.path, 0o700)
            XCTAssertEqual(restored, 0)
            XCTAssertEqual(status, VFWire.stAccess)
            XCTAssertEqual(try read(root, "source/stage"), "new")
            XCTAssertEqual(try read(root, "destination/output"), "original")
        }
    }

    func testMalformedAndEscapingPathsNeverMutatePublication() throws {
        let root = try temporaryShare()
        try write(root, "output", "original")
        try write(root, "stage", "new")
        for payload in [
            Array("stage\0output\0extra".utf8),
            Array("\0output".utf8),
            Array("stage\0".utf8),
            Array("../stage\0output".utf8),
            Array("stage\0../output".utf8),
            Array("stage\0.".utf8),
            Array("stage\0/absolute".utf8),
            [UInt8](repeating: 0xff, count: 10),
        ] {
            XCTAssertEqual(VFWire.rename(root: root, payload: payload, mode: .replace), VFWire.stHostError)
            XCTAssertEqual(try read(root, "stage"), "new")
            XCTAssertEqual(try read(root, "output"), "original")
        }
    }
    #endif
}
