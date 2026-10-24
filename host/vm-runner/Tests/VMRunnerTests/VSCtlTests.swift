// VSCtlTests — the virelai-ctl/1 session matrix (M115b, issue #2461, ADR
// 0056): greeting/auth/request/reply plus every named refusal — forged
// MAC, replayed auth line, replayed request id, unknown verb, bad NAME —
// driven VM-free against the pure VSCtl module.
import CryptoKit
import Foundation
import XCTest
@testable import VSCtl
@testable import VMRunner

final class VSCtlTests: XCTestCase {
    /// ADR 0056 D2's pinned vector: key = ASCII "a5" x 32, challenge =
    /// 00 01 .. 1f — the same bytes the Zig and Go tests pin.
    private let pinKey = Data(String(repeating: "a5", count: 32).utf8)
    private let pinChallenge = [UInt8](0..<32)
    private let pinMAC = "87b459ff70564dd354e1415d16618cc768bf5f2181124a2b495c73b01e274b6c"

    private func authLine(mac: String) -> String { "{\"auth\":\"\(mac)\"}" }

    private func macFor(key: Data, challenge: [UInt8]) -> String {
        var msg = Data(VSCtl.authDomain.utf8)
        msg.append(0)
        msg.append(contentsOf: challenge)
        let mac = HMAC<SHA256>.authenticationCode(for: msg, using: SymmetricKey(data: key))
        return mac.map { String(format: "%02x", $0) }.joined()
    }

    private func code(of data: Data) -> String? {
        guard let s = String(data: data, encoding: .utf8),
              let at = s.range(of: "\"code\":\"") else { return nil }
        let rest = s[at.upperBound...]
        guard let end = rest.firstIndex(of: "\"") else { return nil }
        return String(rest[..<end])
    }

    func testGreetingCarriesPinnedChallenge() {
        let s = CTLSession(key: pinKey, challenge: pinChallenge)
        let g = String(decoding: s.greetingLine(), as: UTF8.self)
        XCTAssertEqual(g, "{\"proto\":\"virelai-ctl/1\",\"auth\":\"hmac-sha256\",\"challenge\":\"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f\"}\n")
    }

    func testPinnedVectorAuthenticates() {
        var s = CTLSession(key: pinKey, challenge: pinChallenge)
        let (reply, fatal) = s.feed(line: authLine(mac: pinMAC))
        XCTAssertFalse(fatal)
        XCTAssertTrue(String(decoding: reply, as: UTF8.self).contains("\"ok\":true"))
        XCTAssertTrue(s.authedForMarker)
    }

    func testForgedMACIsAuthFailed() {
        var s = CTLSession(key: pinKey, challenge: pinChallenge)
        // Every-zero MAC is well-formed hex, wrong answer.
        let (reply, fatal) = s.feed(line: authLine(mac: String(repeating: "00", count: 32)))
        XCTAssertTrue(fatal)
        XCTAssertEqual(code(of: reply), "auth_failed")
        XCTAssertFalse(s.authedForMarker)
    }

    func testReplayedAuthDiesWithItsChallenge() {
        // Session A: capture the auth line that answered challenge A.
        var a = CTLSession(key: pinKey, challenge: pinChallenge)
        _ = a.feed(line: authLine(mac: pinMAC))
        // Session B (a fresh connection, a fresh challenge): replay the
        // captured line verbatim — it is a MAC against the wrong
        // challenge, so the refusal is auth_failed.
        var b = CTLSession(key: pinKey, challenge: [UInt8](32..<64))
        let (reply, fatal) = b.feed(line: authLine(mac: pinMAC))
        XCTAssertTrue(fatal)
        XCTAssertEqual(code(of: reply), "auth_failed")
    }

    func testRequestBeforeAuthIsAuthRequired() {
        var s = CTLSession(key: pinKey, challenge: pinChallenge)
        let (reply, fatal) = s.feed(line: "{\"id\":1,\"verb\":\"suspend\"}")
        XCTAssertTrue(fatal)
        XCTAssertEqual(code(of: reply), "auth_required")
    }

    func testMalformedFirstLineIsRefused() {
        var s = CTLSession(key: pinKey, challenge: pinChallenge)
        var r = s.feed(line: "not json")
        XCTAssertTrue(r.fatal)
        XCTAssertEqual(code(of: r.reply), "bad_json")
        s = CTLSession(key: pinKey, challenge: pinChallenge)
        r = s.feed(line: "{\"auth\":\"zz\"}")
        XCTAssertTrue(r.fatal)
        XCTAssertEqual(code(of: r.reply), "auth_failed")
    }

    private func authedSession() -> CTLSession {
        var s = CTLSession(key: pinKey, challenge: pinChallenge)
        _ = s.feed(line: authLine(mac: pinMAC))
        return s
    }

    func testVerbsRoundTripAsUnimplemented() {
        var s = authedSession()
        var r = s.feed(line: "{\"id\":1,\"verb\":\"suspend\"}")
        XCTAssertFalse(r.fatal)
        XCTAssertEqual(code(of: r.reply), "unimplemented")
        XCTAssertTrue(String(decoding: r.reply, as: UTF8.self).contains("\"id\":1"))
        r = s.feed(line: "{\"id\":2,\"verb\":\"snapshot\",\"name\":\"BEFORE-1\"}")
        XCTAssertEqual(code(of: r.reply), "unimplemented")
    }

    func testUnknownVerb() {
        var s = authedSession()
        let r = s.feed(line: "{\"id\":1,\"verb\":\"frobnicate\"}")
        XCTAssertFalse(r.fatal)
        XCTAssertEqual(code(of: r.reply), "unknown_verb")
    }

    func testReplayedRequestID() {
        var s = authedSession()
        _ = s.feed(line: "{\"id\":1,\"verb\":\"suspend\"}")
        let r = s.feed(line: "{\"id\":1,\"verb\":\"suspend\"}")
        XCTAssertEqual(code(of: r.reply), "replay")
        // A skipped id is the same refusal.
        let r2 = s.feed(line: "{\"id\":5,\"verb\":\"suspend\"}")
        XCTAssertEqual(code(of: r2.reply), "replay")
        // And the counter resumes where it should.
        let r3 = s.feed(line: "{\"id\":2,\"verb\":\"suspend\"}")
        XCTAssertEqual(code(of: r3.reply), "unimplemented")
    }

    func testNameGrammar() {
        var s = authedSession()
        for (i, name) in [".hidden", "..", "a/b", "x y", String(repeating: "n", count: 32)].enumerated() {
            let r = s.feed(line: "{\"id\":\(i + 1),\"verb\":\"snapshot\",\"name\":\"\(name)\"}")
            XCTAssertEqual(code(of: r.reply), "name_rejected", "name \(name)")
        }
        var s2 = authedSession()
        let r = s2.feed(line: "{\"id\":1,\"verb\":\"suspend\",\"name\":\"X\"}")
        XCTAssertEqual(code(of: r.reply), "bad_request")
        s2 = authedSession()
        let r2 = s2.feed(line: "{\"id\":1,\"verb\":\"snapshot\"}")
        XCTAssertEqual(code(of: r2.reply), "bad_request")
        s2 = authedSession()
        let r3 = s2.feed(line: "{\"id\":1,\"verb\":\"suspend\",\"extra\":1}")
        XCTAssertEqual(code(of: r3.reply), "bad_request")
    }

    // M115d (#2463): the armed suspend — an injected driver answers the
    // verb; `ok:true` names state=saved and sets acceptedSuspend for the
    // transport's post-reply tail.
    func testArmedSuspendAccepts() {
        var s = authedSession()
        var calls = 0
        s.suspendHandler = {
            calls += 1
            return .accept
        }
        let r = s.feed(line: "{\"id\":1,\"verb\":\"suspend\"}")
        XCTAssertFalse(r.fatal)
        XCTAssertEqual(calls, 1)
        XCTAssertTrue(s.acceptedSuspend)
        let line = String(decoding: r.reply, as: UTF8.self)
        XCTAssertTrue(line.contains("\"id\":1"))
        XCTAssertTrue(line.contains("\"ok\":true"))
        XCTAssertTrue(line.contains("\"verb\":\"suspend\""))
        XCTAssertTrue(line.contains("\"state\":\"saved\""))
    }

    func testArmedSuspendRefusalCodes() {
        for want in ["no_state", "unsupported", "save_failed", "busy"] {
            var s = authedSession()
            s.suspendHandler = { .refuse(want) }
            let r = s.feed(line: "{\"id\":1,\"verb\":\"suspend\"}")
            XCTAssertEqual(code(of: r.reply), want, want)
            XCTAssertFalse(s.acceptedSuspend)
            XCTAssertTrue(String(decoding: r.reply, as: UTF8.self)
                .contains(VSCtl.messages[want]!), want)
        }
    }

    func testArmedSuspendConsumesTheIdOnRefusal() {
        var s = authedSession()
        s.suspendHandler = { .refuse("no_state") }
        _ = s.feed(line: "{\"id\":1,\"verb\":\"suspend\"}")
        let r = s.feed(line: "{\"id\":1,\"verb\":\"suspend\"}")
        XCTAssertEqual(code(of: r.reply), "replay")
        let r2 = s.feed(line: "{\"id\":2,\"verb\":\"suspend\"}")
        XCTAssertEqual(code(of: r2.reply), "no_state")
    }

    func testBadRequestShapes() {
        let cases: [(String, String)] = [
            ("{\"id\":1}", "bad_request"),                       // verb absent
            ("{\"verb\":\"suspend\"}", "bad_request"),          // id absent
            ("{\"id\":1,\"verb\":3}", "bad_request"),           // verb not a string
            ("{\"id\":0,\"verb\":\"suspend\"}", "bad_request"), // id out of range
            ("{\"id\":1,\"verb\":\"suspend\"} trailing", "bad_request"), // object-shaped, grammar-broken
            ("[1,2]", "bad_json"),
            ("{\"id\":1,\"verb\":\"suspend\",\"id\":2}", "bad_request"), // duplicate key -> not an object
        ]
        for (line, want) in cases {
            var s = authedSession()
            let r = s.feed(line: line)
            XCTAssertEqual(code(of: r.reply), want, line)
        }
    }

    func testLoadKeyRow() throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let secrets = "#v1\nvsock-ctl\t0\t" + String(repeating: "a5", count: 32) + "\n"
        try secrets.write(to: dir.appendingPathComponent("SECRETS.TXT"), atomically: true, encoding: .utf8)
        XCTAssertEqual(VSCtl.loadKey(shareDir: dir), pinKey)
        // A row bound to uid_user does NOT answer a uid_system lookup.
        try "#v1\nvsock-ctl\t1000\tbeef\tVSCTL.ELF\n".write(
            to: dir.appendingPathComponent("SECRETS.TXT"), atomically: true, encoding: .utf8)
        XCTAssertNil(VSCtl.loadKey(shareDir: dir))
        // A non-#v1 file refuses whole.
        try ("vsock-ctl\t0\t" + String(repeating: "a5", count: 32)).write(
            to: dir.appendingPathComponent("SECRETS.TXT"), atomically: true, encoding: .utf8)
        XCTAssertNil(VSCtl.loadKey(shareDir: dir))
    }

    // M115e (#2464): the armed snapshot/restore — injected drivers answer
    // the verbs; `ok:true` carries the name and the saved/staged state.
    func testArmedSnapshotAccepts() {
        var s = authedSession()
        var calls: [String] = []
        s.snapshotHandler = { name in
            calls.append(name)
            return .accept
        }
        let r = s.feed(line: "{\"id\":1,\"verb\":\"snapshot\",\"name\":\"BEFORE-1\"}")
        XCTAssertFalse(r.fatal)
        XCTAssertEqual(calls, ["BEFORE-1"])
        XCTAssertTrue(s.answeredSnapshot)
        let line = String(decoding: r.reply, as: UTF8.self)
        XCTAssertTrue(line.contains("\"id\":1"))
        XCTAssertTrue(line.contains("\"ok\":true"))
        XCTAssertTrue(line.contains("\"verb\":\"snapshot\""))
        XCTAssertTrue(line.contains("\"name\":\"BEFORE-1\""))
        XCTAssertTrue(line.contains("\"state\":\"saved\""))
    }

    func testArmedSnapshotRefusalsStillAnswer() {
        // A refused save still counts as answered — the transport resumes
        // the paused machine after writing the refusal.
        for want in ["exists", "no_state", "unsupported", "save_failed", "busy", "store_full"] {
            var s = authedSession()
            s.snapshotHandler = { _ in .refuse(want) }
            let r = s.feed(line: "{\"id\":1,\"verb\":\"snapshot\",\"name\":\"S1\"}")
            XCTAssertEqual(code(of: r.reply), want, want)
            XCTAssertTrue(s.answeredSnapshot, want)
            XCTAssertTrue(String(decoding: r.reply, as: UTF8.self)
                .contains(VSCtl.messages[want]!), want)
        }
    }

    // #3369: a save admitted past the cap names the evicted snapshot in
    // the ok reply's result — the wire carries which store member paid
    // for the room.
    func testSnapshotAcceptRelaysAnEviction() {
        var s = authedSession()
        s.snapshotHandler = { _ in
            SuspendVerdict(accepted: true, code: "", evicted: "OLDEST")
        }
        let r = s.feed(line: "{\"id\":1,\"verb\":\"snapshot\",\"name\":\"NEW\"}")
        let line = String(decoding: r.reply, as: UTF8.self)
        XCTAssertTrue(line.contains("\"ok\":true"))
        XCTAssertTrue(line.contains("\"evicted\":\"OLDEST\""))
        XCTAssertTrue(s.answeredSnapshot)
    }

    // #3369: SnapshotCtl.admission — the cap policy evaluated against a
    // tempdir store. The named rule is newest-N auto-eviction with an
    // explicit pin (`<name>.pin` beside the .vzsave); CRASH<digits>
    // rides its own bounded namespace.
    private func snapStore(_ names: [String], pins: [String] = [],
                           mtimes: [String: Date] = [:]) throws -> URL {
        let dir = FileManager.default.temporaryDirectory
            .appendingPathComponent(UUID().uuidString)
        let snaps = dir.appendingPathComponent("SNAPSHOTS", isDirectory: true)
        try FileManager.default.createDirectory(at: snaps, withIntermediateDirectories: true)
        for (i, n) in names.enumerated() {
            let url = snaps.appendingPathComponent(n + ".vzsave")
            try Data([UInt8(i)]).write(to: url)
            // Distinct mtimes in list order unless overridden — the
            // eviction victim is the oldest unpinned name.
            let mtime = mtimes[n] ?? Date(timeIntervalSince1970: 1000 + Double(i))
            try FileManager.default.setAttributes([.modificationDate: mtime],
                                                  ofItemAtPath: url.path)
        }
        for p in pins {
            try Data("pin\n".utf8).write(to: snaps.appendingPathComponent(p + ".pin"))
        }
        return dir
    }

    func testAdmissionAllowsBelowTheCap() throws {
        let dir = try snapStore(["A", "B"])
        defer { try? FileManager.default.removeItem(at: dir) }
        XCTAssertEqual(SnapshotCtl.admission(shareDir: dir, name: "C"), .allow)
    }

    func testAdmissionTreatsAMissingStoreAsEmpty() throws {
        let dir = FileManager.default.temporaryDirectory
            .appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        // No SNAPSHOTS/ dir at all — the first save must admit.
        XCTAssertEqual(SnapshotCtl.admission(shareDir: dir, name: "A"), .allow)
    }

    func testAdmissionAtCapEvictsTheOldestUnpinned() throws {
        let dir = try snapStore(["N1", "N2", "N3", "N4", "N5", "N6", "N7", "N8"])
        defer { try? FileManager.default.removeItem(at: dir) }
        XCTAssertEqual(SnapshotCtl.admission(shareDir: dir, name: "NEW"),
                       .evict("N1"))
    }

    func testAdmissionSkipsPinnedSnapshots() throws {
        // N1 is oldest but pinned; N2 pays for the room instead.
        let dir = try snapStore(["N1", "N2", "N3", "N4", "N5", "N6", "N7", "N8"],
                                pins: ["N1"])
        defer { try? FileManager.default.removeItem(at: dir) }
        XCTAssertEqual(SnapshotCtl.admission(shareDir: dir, name: "NEW"),
                       .evict("N2"))
    }

    func testAdmissionRefusesWhenEverythingIsPinned() throws {
        let all = (1...SnapshotCtl.namedCap).map { "N\($0)" }
        let dir = try snapStore(all, pins: all)
        defer { try? FileManager.default.removeItem(at: dir) }
        XCTAssertEqual(SnapshotCtl.admission(shareDir: dir, name: "NEW"),
                       .refuse("store_full"))
    }

    func testAdmissionCrashNamesAreExemptAndSeparatelyBounded() throws {
        // A named store AT cap plus a full CRASH rotation: a CRASH*
        // save still admits — the rotation is exempt from the named
        // cap — and never appears as an eviction victim.
        var names = (1...SnapshotCtl.namedCap).map { "N\($0)" }
        names += (0...2).map { "CRASH\($0)" }
        let dir = try snapStore(names)
        defer { try? FileManager.default.removeItem(at: dir) }
        XCTAssertEqual(SnapshotCtl.admission(shareDir: dir, name: "CRASH7"),
                       .allow)
        // But the exemption is itself bounded: crashCap fresh CRASH
        // names refuse rather than growing the store through the seam.
        var crashNames: [String] = []
        for i in 0..<SnapshotCtl.crashCap { crashNames.append("CRASH\(i)") }
        let dir2 = try snapStore(crashNames)
        defer { try? FileManager.default.removeItem(at: dir2) }
        XCTAssertEqual(SnapshotCtl.admission(shareDir: dir2, name: "CRASH9"),
                       .refuse("store_full"))
    }

    func testAdmissionDoesNotCountCrashNamesTowardTheNamedCap() throws {
        // crashCap crash slots + only 2 named — far under the named
        // cap: CRASH* files are not named-store members.
        var names = (0..<SnapshotCtl.crashCap).map { "CRASH\($0)" }
        names += ["A", "B"]
        let dir = try snapStore(names)
        defer { try? FileManager.default.removeItem(at: dir) }
        XCTAssertEqual(SnapshotCtl.admission(shareDir: dir, name: "C"), .allow)
    }

    func testAdmissionCrashNameGrammar() {
        XCTAssertTrue(SnapshotCtl.isCrashName("CRASH0"))
        XCTAssertTrue(SnapshotCtl.isCrashName("CRASH12"))
        // The prefix alone is an ordinary name, as is a non-digit tail.
        XCTAssertFalse(SnapshotCtl.isCrashName("CRASH"))
        XCTAssertFalse(SnapshotCtl.isCrashName("CRASHX"))
        XCTAssertFalse(SnapshotCtl.isCrashName("CRASH1A"))
        XCTAssertFalse(SnapshotCtl.isCrashName("crash0"))
    }

    func testAdmissionStrayFilesDoNotCount() throws {
        let dir = try snapStore(["A", "B"])
        defer { try? FileManager.default.removeItem(at: dir) }
        let snaps = dir.appendingPathComponent("SNAPSHOTS", isDirectory: true)
        // An in-flight save's .part and an orphan .pin are not snapshots.
        try Data([0]).write(to: snaps.appendingPathComponent("WIP.vzsave.part"))
        try Data("pin\n".utf8).write(to: snaps.appendingPathComponent("ORPHAN.pin"))
        XCTAssertEqual(SnapshotCtl.admission(shareDir: dir, name: "C"), .allow)
    }

    func testArmedRestoreAccepts() {
        var s = authedSession()
        var calls: [String] = []
        s.restoreHandler = { name in
            calls.append(name)
            return .accept
        }
        let r = s.feed(line: "{\"id\":1,\"verb\":\"restore\",\"name\":\"BEFORE-1\"}")
        XCTAssertFalse(r.fatal)
        XCTAssertEqual(calls, ["BEFORE-1"])
        XCTAssertFalse(s.answeredSnapshot)
        XCTAssertFalse(s.acceptedSuspend)
        let line = String(decoding: r.reply, as: UTF8.self)
        XCTAssertTrue(line.contains("\"verb\":\"restore\""))
        XCTAssertTrue(line.contains("\"state\":\"staged\""))
    }

    func testArmedRestoreRefusals() {
        for want in ["not_found", "no_state", "io_error"] {
            var s = authedSession()
            s.restoreHandler = { _ in .refuse(want) }
            let r = s.feed(line: "{\"id\":1,\"verb\":\"restore\",\"name\":\"S1\"}")
            XCTAssertEqual(code(of: r.reply), want, want)
        }
    }

    func testRestoreRejectsABadNameLikeSnapshot() {
        var s = authedSession()
        s.restoreHandler = { _ in .accept }
        let r = s.feed(line: "{\"id\":1,\"verb\":\"restore\",\"name\":\"a/b\"}")
        XCTAssertEqual(code(of: r.reply), "name_rejected")
        s = authedSession()
        let r2 = s.feed(line: "{\"id\":1,\"verb\":\"restore\"}")
        XCTAssertEqual(code(of: r2.reply), "bad_request")
    }

    // M128d (#2608): the armed profile-info — the injected sampler's
    // fields land verbatim in the reply's result object.
    func testArmedProfileInfoAccepts() {
        var s = authedSession()
        var calls = 0
        s.profileInfoHandler = {
            calls += 1
            return ProfileSummary(profile: "work", base: "disk.img",
                                  rollback: true, snapshots: 3)
        }
        let r = s.feed(line: "{\"id\":1,\"verb\":\"profile-info\"}")
        XCTAssertFalse(r.fatal)
        XCTAssertEqual(calls, 1)
        XCTAssertFalse(s.acceptedSuspend)
        XCTAssertFalse(s.answeredSnapshot)
        let line = String(decoding: r.reply, as: UTF8.self)
        XCTAssertTrue(line.contains("\"id\":1"))
        XCTAssertTrue(line.contains("\"ok\":true"))
        XCTAssertTrue(line.contains("\"profile\":\"work\""))
        XCTAssertTrue(line.contains("\"base\":\"disk.img\""))
        XCTAssertTrue(line.contains("\"rollback\":true"))
        XCTAssertTrue(line.contains("\"snapshots\":3"))
    }

    func testProfileInfoUnimplementedWithoutHandler() {
        var s = authedSession()
        let r = s.feed(line: "{\"id\":1,\"verb\":\"profile-info\"}")
        XCTAssertEqual(code(of: r.reply), "unimplemented")
        XCTAssertFalse(r.fatal)
    }

    func testProfileInfoRejectsAName() {
        var s = authedSession()
        s.profileInfoHandler = { ProfileSummary(profile: "p", base: "b",
                                                rollback: false, snapshots: 0) }
        let r = s.feed(line: "{\"id\":1,\"verb\":\"profile-info\",\"name\":\"X\"}")
        XCTAssertEqual(code(of: r.reply), "bad_request")
    }

    func testProfileInfoEscapesStrings() {
        var s = authedSession()
        s.profileInfoHandler = {
            ProfileSummary(profile: "weird\"\\name", base: "b",
                           rollback: false, snapshots: 0)
        }
        let r = s.feed(line: "{\"id\":1,\"verb\":\"profile-info\"}")
        let line = String(decoding: r.reply, as: UTF8.self)
        XCTAssertTrue(line.contains("\"profile\":\"weird\\\"\\\\name\""))
    }

    func testReadyLineListsAllVerbs() {
        var s = CTLSession(key: pinKey, challenge: pinChallenge)
        let (reply, _) = s.feed(line: authLine(mac: pinMAC))
        let line = String(decoding: reply, as: UTF8.self)
        for verb in ["suspend", "snapshot", "restore", "profile-info"] {
            XCTAssertTrue(line.contains("\"\(verb)\""), verb)
        }
    }
}
