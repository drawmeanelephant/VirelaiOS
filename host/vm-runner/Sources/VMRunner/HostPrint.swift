import AppKit
import Foundation
import PDFKit

// MARK: - HostPrint — the M202c print lane, host half (ADR 0051)
//
// `--host-print` arms the lane: guest apps stage a PDF payload plus a
// PREQ-<8hex> intent marker under <share>/PRINT/ (the runner creates the
// dir at arm — the guest SDK has no mkdir, the OUTBOX posture), the seat
// signs each as a PRINT record (the DROP payload shape — [name_len u8]
// [name][size u64le] — naming the staged job), and this lane drains them
// on the shared 250 ms poll.
//
// What a verified record earns (the card's measured consent story):
//
//   - A bundled session shows NSPrintPanel for the job — the human's
//     Print/Cancel click is the per-job consent ON TOP of the flag's
//     per-boot consent. The lane never runs a print operation without
//     the panel: a job that reaches a printer without a person asking
//     is the dishonest path, so none exists.
//   - A bare runner has no panel to show — every class-B gate — so the
//     consent-free degrade is the honest one: the payload moves to
//     --host-print-dir (default ~/Downloads) and the receipt says
//     `saved`, never `printed` — a file the operator can print, not a
//     claim that paper moved.
//
// The rules (the outbox discipline, mirrored):
//   - What the host trusts is the signed record — the PREQ is unsigned
//     intent. Per verified record: the named payload must exist (gone),
//     be regular (unsupported_kind), fit ≤ 4 MiB at consume time
//     (payload_too_large — the live fstat rules, not the record's
//     attest-time size), and BE a PDF: the %PDF- magic is re-checked
//     host-side, so a lane file that is not a document is
//     unsupported_kind, never a panel of garbage. The name is
//     re-validated against the D4 charset + the lane's .pdf rule — a
//     verified record can still carry a name that would walk off the
//     destination, and name_rejected is the answer.
//   - ≤ 16 jobs per epoch (quota_exceeded — the OUTBOX cap's shape at
//     a paper lane's quorum) and ≤ 1 verified record consumed per tick.

/// What the panel did with the job — the completion seam's vocabulary.
enum HostPrintOutcome {
    case printed    // the human confirmed: a real print op ran
    case cancelled  // the human declined the panel — an outcome, not an error
    case failed     // the panel/job path itself could not run
}

/// The printer seam — the verify → present hand-off's host-side end.
protocol HostPrinting: AnyObject {
    /// Present the job's print UI; must return immediately (the VM's
    /// 250 ms drain must never block on a modal panel). The completion
    /// fires on the main queue with the job's outcome.
    func present(_ url: URL, completion: @escaping (HostPrintOutcome) -> Void)
}

/// Production: PDFKit's print operation under a modal NSPrintPanel.
/// runModal(for:delegate:didRun:contextInfo:) is the non-blocking modal
/// run — the panel is up for however long the human takes while the VM
/// keeps its 250 ms polls; the staged payload stays on the share until
/// didRun answers, so a slow confirm never prints a torn file.
final class HostNSPrint: NSObject, HostPrinting {
    /// Ops in flight, keyed by identity — the operation must outlive the
    /// call (runModal returns at once), so the table retains it until
    /// didRun.
    private var pending: [ObjectIdentifier: (NSPrintOperation, (HostPrintOutcome) -> Void)] = [:]

    func present(_ url: URL, completion: @escaping (HostPrintOutcome) -> Void) {
        guard let doc = PDFDocument(url: url),
              let op = doc.printOperation(for: NSPrintInfo.shared,
                                          scalingMode: .pageScaleToFit,
                                          autoRotate: true) else {
            completion(.failed)
            return
        }
        op.showsPrintPanel = true
        guard let win = NSApp.keyWindow ?? NSApp.mainWindow else {
            // Bundled but no window up — the panel has nowhere to hang.
            completion(.failed)
            return
        }
        pending[ObjectIdentifier(op)] = (op, completion)
        op.runModal(for: win, delegate: self,
                    didRun: #selector(didRun(_:success:contextInfo:)), contextInfo: nil)
    }

    @objc private func didRun(_ op: NSPrintOperation, success: Bool,
                              contextInfo: UnsafeMutableRawPointer?) {
        let entry = pending.removeValue(forKey: ObjectIdentifier(op))
        entry?.1(success ? .printed : .cancelled)
    }
}

final class HostPrint: NSObject {
    /// Per-job bound at consume time — the tree's own PDF bound (the
    /// pdfwrite output cap and the reader's MaxSource are both 4 MiB),
    /// so a job past it is nothing an in-tree producer made.
    static let fileMax: Int64 = 4 * 1024 * 1024
    /// Per-epoch job cap — the outbox quota's shape at a paper lane's
    /// quorum.
    static let epochMax = 16
    /// Consume bound per 250 ms tick: ONE verified record — the notify
    /// drain's cadence every g→h lane holds.
    static let perTickMax = 1
    /// The guest-visible staging dir inside the share — the runner owns
    /// it at arm like OUTBOX/ and SAY/ (the SDK has no mkdir).
    static let printDirName = "PRINT"

    let session: HostIntSession
    private var laneDir: URL { session.laneDir }
    private var printDir: URL {
        session.shareDir.appendingPathComponent(Self.printDirName, isDirectory: true)
    }
    /// The degrade landing — --host-print-dir, else ~/Downloads.
    let destDir: URL
    private var lastSeq: UInt32 = 0      // last consumed PRINT seq
    private var jobs = 0                 // per-epoch job count
    private var ioLogged = Set<String>() // one io_error per filename
    /// Internal, not private: a test binds a mock — the print UI is the
    /// seam this file's contract ends at.
    var printer: HostPrinting
    /// Panels exist only under an app bundle — the bare gate runner
    /// takes the consent-free degrade (a saved file), the posture the
    /// header names. Internal, not private: XCTest itself runs inside a
    /// bundle, so tests set it to drive both paths.
    var bundled = Bundle.main.bundleIdentifier != nil

    init(session: HostIntSession, destDir: URL, printer: HostPrinting? = nil) {
        self.session = session
        self.destDir = destDir
        self.printer = printer ?? HostNSPrint()
        super.init()
    }

    /// Arm the lane: create <share>/PRINT/ — the runner owns the staging
    /// dirs, the same posture AGENT/, OUTBOX/ and SAY/ take.
    @discardableResult
    func start() -> Bool {
        do {
            try FileManager.default.createDirectory(at: printDir, withIntermediateDirectories: true)
        } catch {
            HostInt.logRefuse(lane: "print", reason: .ioError)
            return false
        }
        return true
    }

    /// The 250 ms poll step (hostIntTick): drain PRINT records in seq
    /// order — the same verify-then-consume shape the outbox lane runs.
    /// Refused records are deleted as met; only `perTickMax` verified
    /// records are consumed per tick — anything beyond stays on disk and
    /// drains at the lane's own rate.
    ///
    /// While the session's print bit is suspended the lane is a hard
    /// off — every record for it is refused `lane_disabled` and deleted.
    func tick() {
        if session.isSuspended(HostInt.lanePrint) {
            guard let pending = HostInt.records(laneDir: laneDir, tag: .print) else { return }
            for (name, _) in pending {
                HostInt.logRefuse(lane: "print", reason: .laneDisabled)
                remove(laneDir.appendingPathComponent(name))
            }
            return
        }
        guard let files = HostInt.records(laneDir: laneDir, tag: .print) else { return }
        var consumed = 0
        for (name, seq) in files {
            let url = laneDir.appendingPathComponent(name)
            guard let raw = try? Data(contentsOf: url) else {
                if ioLogged.insert(name).inserted {
                    HostInt.logRefuse(lane: "print", reason: .ioError)
                }
                continue
            }
            ioLogged.remove(name)
            guard let rec = HostInt.decode(raw), rec.tag == .print else {
                HostInt.logRefuse(lane: "print", reason: .badRecord)
                remove(url)
                continue
            }
            guard HostInt.verify(rec, key: session.key) else {
                HostInt.logRefuse(lane: "print", reason: .badMAC)
                remove(url)
                continue
            }
            guard rec.epoch == session.epoch else {
                HostInt.logRefuse(lane: "print", reason: .staleEpoch)
                remove(url)
                continue
            }
            guard seq > lastSeq else {
                HostInt.logRefuse(lane: "print", reason: .staleSeq)
                remove(url)
                continue
            }
            // The per-tick bound: a verified record past it is left for
            // the next tick — consume is the expensive act (the panel or
            // the landing), not the refusal sweeps above.
            guard consumed < Self.perTickMax else { break }
            lastSeq = seq
            remove(url)
            job(rec, seq: seq)
            consumed += 1
        }
    }

    /// One verified PRINT record → one panel or one landing. The payload
    /// shape is the DROP shape: [name_len u8][name][size u64le].
    private func job(_ rec: HostInt.Record, seq: UInt32) {
        let p = rec.payload
        guard p.count >= 10, Int(p[0]) > 0, p.count == 1 + Int(p[0]) + 8 else {
            HostInt.logRefuse(lane: "print", reason: .badRecord)
            return
        }
        let n = Int(p[0])
        guard let name = String(data: p.subdata(in: 1..<1 + n), encoding: .utf8),
              HostOutbox.validName(name),
              name.lowercased().hasSuffix(".pdf") else {
            HostInt.logRefuse(lane: "print", reason: .nameRejected)
            return
        }
        let src = printDir.appendingPathComponent(name)
        var st = stat()
        guard stat(src.path, &st) == 0 else {
            HostInt.logRefuse(lane: "print", reason: .gone)
            return
        }
        guard st.st_mode & S_IFMT == S_IFREG else {
            HostInt.logRefuse(lane: "print", reason: .unsupportedKind)
            return
        }
        guard st.st_size <= Self.fileMax else {
            HostInt.logRefuse(lane: "print", reason: .payloadTooLarge)
            return
        }
        // The lane carries print jobs — the payload must BE a PDF. The
        // magic re-check is the host's half of the app-side IsPDF pin.
        guard let head = try? Data(contentsOf: src, options: .mappedIfSafe),
              head.count >= 5, head.prefix(5) == Data("%PDF-".utf8) else {
            HostInt.logRefuse(lane: "print", reason: .unsupportedKind)
            return
        }
        guard jobs < Self.epochMax else {
            HostInt.logRefuse(lane: "print", reason: .quotaExceeded)
            return
        }
        jobs += 1
        if bundled {
            FileHandle.standardOutput.write(Data(
                "host-print: panel seq=\(seq) name=\(name)\n".utf8))
            printer.present(src) { [weak self] outcome in
                // Consume completes here: the staged job served its run,
                // whatever the human answered.
                self?.remove(src)
                let word = outcome == .printed ? "printed" : outcome == .cancelled ? "cancelled" : "error"
                FileHandle.standardOutput.write(Data(
                    "host-print: \(word) seq=\(seq) name=\(name)\n".utf8))
            }
            return
        }
        // The consent-free degrade, named honestly: a bare runner cannot
        // show a panel, so the job lands as a file the operator can
        // print — `saved`, never `printed`.
        guard let dst = HostOutbox.landingURL(in: destDir, name) else {
            HostInt.logRefuse(lane: "print", reason: .nameRejected)
            return
        }
        do {
            try FileManager.default.moveItem(at: src, to: dst)
        } catch {
            HostInt.logRefuse(lane: "print", reason: .ioError)
            return
        }
        FileHandle.standardOutput.write(Data(
            "host-print: saved seq=\(seq) name=\(dst.lastPathComponent) size=\(st.st_size)\n".utf8))
    }

    private func remove(_ url: URL) {
        try? FileManager.default.removeItem(at: url)
    }
}
