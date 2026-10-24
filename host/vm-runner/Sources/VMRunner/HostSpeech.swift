import AppKit
import Foundation

// MARK: - HostSpeech — the M186d speech lane, host half (ADR 0051 V2-D8)
//
// `--host-speech` arms the lane: guest apps stage utterances as
// SAY-<8hex> intent markers under <share>/SAY/ (the runner creates the
// dir at arm — the guest SDK has no mkdir, the OUTBOX posture), the seat
// signs each as a SPEAK record (the marker's content IS the payload —
// bounded UTF-8, plen 1..512), and this lane drains them on the shared
// 250 ms poll into NSSpeechSynthesizer — queued, interruptible, never
// blocking the VM.
//
// The synth is a seam: production binds NSSpeechSynthesizer (its own
// utterance queue — startSpeaking returns at once, so the drain never
// waits on a voice), a test pins the decode → synth hand-off with a
// mock. Never-logged holds: the utterance text never reaches stdout —
// the receipt is seq + len + sha256[:8], the lane's standard shape.

/// The synth seam — the decode → speak hand-off's host-side end.
protocol HostSpeechSynthesizing: AnyObject {
    /// Queue one utterance; must return immediately (the VM's 250 ms
    /// drain must never block on the speech stack).
    func enqueue(_ text: String)
    /// Interrupt anything queued or in flight.
    func interrupt()
}

/// Production: NSSpeechSynthesizer's own utterance queue — interruptible
/// via stopSpeaking.
final class HostNSSpeech: HostSpeechSynthesizing {
    private let synth = NSSpeechSynthesizer()
    func enqueue(_ text: String) { synth.startSpeaking(text) }
    func interrupt() { synth.stopSpeaking() }
}

final class HostSpeech: NSObject {
    /// Consume bound per 250 ms tick: ONE verified record — the seat's
    /// own emit rate and the notify drain's cadence. The lane dir is
    /// guest-writable and the guest holds the lane key, so a hostile
    /// guest can drop thousands of validly-MAC'd records at once; the
    /// cap means that buys at most one utterance per poll. Verified
    /// records past the cap stay on disk for the next tick.
    static let perTickMax = 1
    /// The guest-visible staging dir inside the share — the runner owns
    /// it at arm like OUTBOX/ and INBOX/ (the SDK has no mkdir).
    static let sayDirName = "SAY"

    let session: HostIntSession
    private var laneDir: URL { session.laneDir }
    private var sayDir: URL {
        session.shareDir.appendingPathComponent(Self.sayDirName, isDirectory: true)
    }
    private var lastSeq: UInt32 = 0      // last consumed SPEAK seq
    private var ioLogged = Set<String>() // one io_error per filename
    /// Internal, not private: a test binds a mock — the synth itself is
    /// the seam this file's contract ends at.
    var synth: HostSpeechSynthesizing

    init(session: HostIntSession, synth: HostSpeechSynthesizing? = nil) {
        self.session = session
        self.synth = synth ?? HostNSSpeech()
        super.init()
    }

    /// Arm the lane: create <share>/SAY/ — the runner owns the staging
    /// dirs, the same posture AGENT/, INBOX/ and OUTBOX/ take.
    @discardableResult
    func start() -> Bool {
        do {
            try FileManager.default.createDirectory(at: sayDir, withIntermediateDirectories: true)
        } catch {
            HostInt.logRefuse(lane: "speech", reason: .ioError)
            return false
        }
        return true
    }

    /// The 250 ms poll step (hostIntTick): drain SPEAK records in seq
    /// order — the same verify-then-consume shape the notify lane runs.
    /// Refused records are deleted as met; only `perTickMax` verified
    /// records are consumed per tick — anything beyond stays on disk and
    /// drains at the lane's own rate.
    ///
    /// While the session's speech bit is suspended the lane is a hard
    /// off — every record for it is refused `lane_disabled` and deleted.
    func tick() {
        if session.isSuspended(HostInt.laneSpeech) {
            guard let pending = HostInt.records(laneDir: laneDir, tag: .speak) else { return }
            for (name, _) in pending {
                HostInt.logRefuse(lane: "speech", reason: .laneDisabled)
                remove(laneDir.appendingPathComponent(name))
            }
            return
        }
        guard let files = HostInt.records(laneDir: laneDir, tag: .speak) else { return }
        var consumed = 0
        for (name, seq) in files {
            let url = laneDir.appendingPathComponent(name)
            guard let raw = try? Data(contentsOf: url) else {
                if ioLogged.insert(name).inserted {
                    HostInt.logRefuse(lane: "speech", reason: .ioError)
                }
                continue
            }
            ioLogged.remove(name)
            guard let rec = HostInt.decode(raw), rec.tag == .speak else {
                HostInt.logRefuse(lane: "speech", reason: .badRecord)
                remove(url)
                continue
            }
            guard HostInt.verify(rec, key: session.key) else {
                HostInt.logRefuse(lane: "speech", reason: .badMAC)
                remove(url)
                continue
            }
            guard rec.epoch == session.epoch else {
                HostInt.logRefuse(lane: "speech", reason: .staleEpoch)
                remove(url)
                continue
            }
            guard seq > lastSeq else {
                HostInt.logRefuse(lane: "speech", reason: .staleSeq)
                remove(url)
                continue
            }
            guard let text = String(data: rec.payload, encoding: .utf8) else {
                HostInt.logRefuse(lane: "speech", reason: .payloadNotText)
                remove(url)
                continue
            }
            // The per-tick bound: a verified record past it is left for
            // the next tick — consume is the expensive act (the synth
            // hand-off), not the refusal sweeps above.
            guard consumed < Self.perTickMax else { break }
            lastSeq = seq
            remove(url)
            synth.enqueue(text)
            FileHandle.standardOutput.write(Data(
                "host-speech: speak seq=\(seq) sha256=\(HostInt.sha256_8(rec.payload)) len=\(rec.payload.count)\n".utf8))
            consumed += 1
        }
    }

    private func remove(_ url: URL) {
        try? FileManager.default.removeItem(at: url)
    }
}
