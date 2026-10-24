import Foundation
import XCTest
@testable import VMAppKit
@testable import VMRunner

/// M186d (#3181, ADR 0051 V2-D8): the speech lane, host half. The whole
/// lane is files — a real `HostIntSession.arm` on a temp share gives the
/// lane dir, the epoch and the key, so decode → synth hand-off runs
/// class-A with the synth mocked (the seam is the contract's end).
final class HostSpeechTests: XCTestCase {
    private let key = String(repeating: "k", count: 64)

    /// The mocked synth: every enqueue lands here, never near AppKit's
    /// speech stack.
    private final class MockSynth: HostSpeechSynthesizing {
        var spoken: [String] = []
        var interrupts = 0
        func enqueue(_ text: String) { spoken.append(text) }
        func interrupt() { interrupts += 1 }
    }

    private func makeSession(mask: UInt8 = HostInt.laneSpeech) throws -> (URL, HostIntSession) {
        let dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("hostspeech-tests-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        addTeardownBlock { try? FileManager.default.removeItem(at: dir) }
        try "#v1\nhost-int\t1000\t\(key)\tGOTABWM.ELF\n".write(
            to: dir.appendingPathComponent("SECRETS.TXT"), atomically: true, encoding: .utf8)
        let session = try XCTUnwrap(HostIntSession.arm(shareDir: dir, mask: mask, requested: ["speech"]))
        return (dir, session)
    }

    /// Plant one signed SPEAK the way the seat emits it.
    private func plantSpeak(_ session: HostIntSession, seq: UInt32, payload: Data) throws {
        let rec = try XCTUnwrap(HostInt.encode(tag: .speak, flags: 0, seq: seq,
                                               epoch: session.epoch, payload: payload,
                                               key: session.key))
        try rec.write(to: session.laneDir.appendingPathComponent(
            HostInt.recordName(tag: .speak, seq: seq)))
    }

    private func speakFiles(_ session: HostIntSession) -> [String] {
        (HostInt.records(laneDir: session.laneDir, tag: .speak) ?? []).map(\.0)
    }

    func testStartCreatesSayDir() throws {
        let (dir, session) = try makeSession()
        let lane = HostSpeech(session: session, synth: MockSynth())
        XCTAssertTrue(lane.start())
        var isDir: ObjCBool = false
        XCTAssertTrue(FileManager.default.fileExists(
            atPath: dir.appendingPathComponent("SAY").path, isDirectory: &isDir))
        XCTAssertTrue(isDir.boolValue)
    }

    func testConsumeHandsTextToSynth() throws {
        let (_, session) = try makeSession()
        let synth = MockSynth()
        let lane = HostSpeech(session: session, synth: synth)
        try plantSpeak(session, seq: 1, payload: Data("hello".utf8))
        lane.tick()
        XCTAssertEqual(synth.spoken, ["hello"])
        XCTAssertTrue(speakFiles(session).isEmpty) // consumed, not just read
    }

    func testGrammarEmptySpeakIsBadRecord() throws {
        // The wire grammar: plen 0 SPEAK fails encode AND decode.
        let (_, session) = try makeSession()
        XCTAssertNil(HostInt.encode(tag: .speak, flags: 0, seq: 1,
                                    epoch: session.epoch, payload: Data(), key: session.key))
        var raw = try XCTUnwrap(HostInt.encode(tag: .speak, flags: 0, seq: 1,
                                               epoch: session.epoch, payload: Data("x".utf8), key: session.key))
        // Hand-mutate plen to zero and trim the payload byte: decode
        // must still refuse (bad_record), never reach the synth.
        raw[16] = 0
        raw[17] = 0
        raw = raw.subdata(in: 0..<raw.count - 1)
        XCTAssertNil(HostInt.decode(raw))
    }

    func testRefusalsAreNamedAndDeleted() throws {
        let (dir, session) = try makeSession()
        let synth = MockSynth()
        let lane = HostSpeech(session: session, synth: synth)
        // Forged record: bad_mac — deleted, never spoken.
        let forged = try XCTUnwrap(HostInt.encode(tag: .speak, flags: 0, seq: 1,
                                                  epoch: session.epoch, payload: Data("forged".utf8),
                                                  key: Data("wrong-key".utf8)))
        try forged.write(to: session.laneDir.appendingPathComponent("SPEAK-00000001"))
        // Wrong tag under a SPEAK name: bad_record.
        let clipShape = try XCTUnwrap(HostInt.encode(tag: .clipOut, flags: 0, seq: 7,
                                                     epoch: session.epoch, payload: Data("x".utf8),
                                                     key: session.key))
        try clipShape.write(to: session.laneDir.appendingPathComponent("SPEAK-00000007"))
        // Non-UTF-8 payload: payload_not_text.
        try plantSpeak(session, seq: 3, payload: Data([0xff, 0xfe]))
        // Foreign epoch: stale_epoch.
        let stale = try XCTUnwrap(HostInt.encode(tag: .speak, flags: 0, seq: 4,
                                                 epoch: session.epoch &+ 1, payload: Data("old".utf8),
                                                 key: session.key))
        try stale.write(to: session.laneDir.appendingPathComponent("SPEAK-00000004"))
        lane.tick()
        XCTAssertTrue(synth.spoken.isEmpty)
        XCTAssertTrue(speakFiles(session).isEmpty)
        _ = dir
    }

    func testStaleSeqIsRefused() throws {
        let (_, session) = try makeSession()
        let synth = MockSynth()
        let lane = HostSpeech(session: session, synth: synth)
        try plantSpeak(session, seq: 1, payload: Data("one".utf8))
        lane.tick()
        XCTAssertEqual(synth.spoken, ["one"])
        // Replay seq 1 under a different text: stale_seq, deleted.
        try plantSpeak(session, seq: 1, payload: Data("replay".utf8))
        lane.tick()
        XCTAssertEqual(synth.spoken, ["one"])
        XCTAssertTrue(speakFiles(session).isEmpty)
    }

    func testPerTickBoundQueuesTheTail() throws {
        let (_, session) = try makeSession()
        let synth = MockSynth()
        let lane = HostSpeech(session: session, synth: synth)
        try plantSpeak(session, seq: 1, payload: Data("one".utf8))
        try plantSpeak(session, seq: 2, payload: Data("two".utf8))
        lane.tick()
        XCTAssertEqual(synth.spoken, ["one"])
        XCTAssertEqual(speakFiles(session).count, 1) // the tail waits
        lane.tick()
        XCTAssertEqual(synth.spoken, ["one", "two"])
        XCTAssertTrue(speakFiles(session).isEmpty)
    }

    func testSuspendedLaneRefusesLaneDisabled() throws {
        let (_, session) = try makeSession()
        let synth = MockSynth()
        let lane = HostSpeech(session: session, synth: synth)
        XCTAssertTrue(session.setSuspended(HostInt.laneSpeech, true))
        try plantSpeak(session, seq: 1, payload: Data("shh".utf8))
        lane.tick()
        XCTAssertTrue(synth.spoken.isEmpty)
        XCTAssertTrue(speakFiles(session).isEmpty) // refused records are deleted
    }
}
