// Run control: poll loops (plain/script/console/host-session),
// marker-triggered fires, stop/drain/finish, signal handlers.

import AppKit
import ApplicationServices
import CryptoKit
import Darwin
// DiskImageKit ships in the macOS 27 SDK only (#523 item 2). The class-A
// CI job builds against the older macos-latest SDK, so the import and the
// --overlay-base implementation are compiled in ONLY when the SDK has the
// framework; elsewhere the flag fails honestly at runtime.
#if canImport(DiskImageKit)
import DiskImageKit
#endif
import Foundation
import ScreenCaptureKit
import Security
import Virtualization
// M117d (#2476, ADR 0057 D11): vmnet.framework backs --net-vmnet. The
// vmnet_network_* symbols are macOS 26+, so they exist wherever this
// runner's deployment floor applies; the module-level guard keeps an
// older-SDK toolchain compiling — the flag then refuses by name.
#if canImport(vmnet)
import vmnet
#endif
import VFWire
import VMAppKit
import VMDiskImage
import VMGPU3D
import VMPostmortem
import VMProfile
import VMTcp
import VSCtl
import VSNTP
import VSSH


// Set when vm.start completes successfully; consolePoll only treats a
// .stopped/.error state as "session over" after the VM has actually started
// (a fresh VZVirtualMachine is .stopped until boot begins).
var vmDidStart = false

let startTime = Date()

let deadline = startTime.addingTimeInterval(timeout)

let consoleStart = Date()

let consoleDeadline = consoleStart.addingTimeInterval(consoleTimeout)

var lastText = ""

var screenshotSaved = false

var serialMatched = false

var terminalMatched = terminalMarker == nil

var evidenceSince: Date?

let terminalDwell: TimeInterval = 2

// Issue #1788: stop the device, then run the post-stop evidence reads and
// exit. Split out of finish() so the teardown drain can gate the stop
// itself without duplicating this body.
/// Issue #3112: after vm.stop, give the disk-image driver's queue a
/// beat to land — it drains asynchronously past the power drop, and a
/// post-kill fsck reading the file a beat too early would see the
/// mid-drain state. fsync on our raw-device handle pushes our side; the
/// short settle covers the driver's own queue.
func drainDataDiskDevice() {
    if let fh = dataDiskBlockFH { fsync(fh.fileDescriptor) }
    if let dev = dataDiskBlockDev {
        if let f = try? FileHandle(forUpdating: URL(fileURLWithPath: dev)) {
            fsync(f.fileDescriptor); try? f.close()
        }
        usleep(500_000)
    }
}

func stopDeviceAndExit(success: Bool, wantDump: Bool, wantNvram: Bool) {
    runner.queue.async {
        runner.vm.stop { _ in
            drainDataDiskDevice()
            // Claim 2188: the VM is stopped, so every guest byte VZ will
            // deliver is already in the pipe — let the tee flush it into the
            // log before the NVRAM reads and exit() below (which would
            // otherwise race the tee thread and cut the tail mid-write).
            drainTeeBeforeExit("finish(success:\(success))")
            // Exit code: the serial evidence `success` is the default; each
            // NVRAM-gated channel (marker ladder, nvram console) flips it to
            // true when its bytes are found. With no such flag the original
            // serial-gate semantics are unchanged.
            var finalSuccess = success
            if wantDump, let dumpPath = markerDumpPath {
                // ADR 0004 D4 fixed-memory-marker fallback (working form, claim
                // 0009): the kernel persists its takeover stage as the EFI
                // non-volatile variable `VirelaiM2` (runtime SetVariable survives
                // ExitBootServices on VZ — observed), and the host reads the
                // store after the VM stops. The memory-scan variant is impossible
                // on VZ: guest RAM is not mapped into the runner process
                // (observed — a full submap-aware walk finds no 256 MiB region
                // and every M2_* hit is the runner's own constant array). The
                // NVRAM ladder is the gate here; the exit code becomes 0 iff at
                // least one marker instance is present in the store.
                print("marker ladder: reading EFI variable store")
                let ladder = readMarkerLadder(from: varsURL)
                writeMarkerDump(to: dumpPath, ladder: ladder)
                if ladder.isEmpty {
                    print("MARKER-GATE: no M2_* marker in the EFI variable store (kernel died before its first marker write, or SetVariable failed)")
                } else {
                    finalSuccess = true
                    for (name, _) in ladder {
                        print("MARKER-GATE: \(name)")
                    }
                }
            }
            if wantNvram, let path = nvramConsolePath {
                print("nvram console: reconstructing console stream from EFI variable store")
                let result = readNvramConsole(from: varsURL)
                writeNvramConsole(to: path, result: result)
                if result.text.isEmpty {
                    print("NVRAM-CONSOLE: no console chunks in the EFI variable store (kernel wrote nothing, or SetVariable failed post-exit)")
                } else {
                    finalSuccess = true
                    print("NVRAM-CONSOLE: \(result.chunks) chunk(s) reconstructed\(result.complete ? "" : " (INCOMPLETE — missing chunk index \(result.missing!))"), \(result.text.count) bytes")
                }
            }
            // Milestone five card N1 (claim 1373): the VM is stopped, so the
            // net attachment is quiescent — stop the reader (closing the
            // runner's socket end unblocks its read), join it, and close the
            // capture file so the gate can assert the guest's frames
            // byte-exactly.
            if netCaptureSocket != nil {
                netCaptureStop = true
                try? netCaptureReadSocket?.close()
                _ = netCaptureDone.wait(timeout: .now() + 2)
                try? netCaptureHandle?.close()
            }
            for client in netTcpClients {
                if let probe = client.streamProcess, probe.isRunning {
                    probe.terminate() // VM stopped; do not leave a blocked test peer behind
                }
            }
            // M102d3 (#2168): the opt-in render device's lifecycle
            // counters (resets, refusals, draws, completions) — numbers
            // only, never token contents or guest bytes.
            if let gpu3dCountersReport {
                print("  \(gpu3dCountersReport())")
            }
            // Claim #1278: report the stop verdict on EVERY run, not only the
            // failing ones. The live gate asserts this line, so dropping the
            // `runner.vm.delegate = vmDelegate` wiring — the exact omission that
            // left #1261 with a bare `state=3` and no reason — now fails a gate
            // instead of silently discarding the reason again.
            print("vm-stop: state=\(runner.vm.state.rawValue)\(vmDelegate.verdict())")
            exit(finalSuccess ? 0 : 1)
        }
    }
}

// Issue #1788: every exit path goes through here. Stopping the device is
// what truncates an in-flight kind-4 stream, so the drain runs first.
func finish(success: Bool) {
    drainSnapshotsBeforeStop("finish(success:\(success))") {
        stopDeviceAndExit(success: success, wantDump: markerDumpPath != nil, wantNvram: nvramConsolePath != nil)
    }
}

func poll() {
    if let data = try? Data(contentsOf: serialURL), let text = String(data: data, encoding: .utf8) {
        if !text.isEmpty { lastText = text; feedInspector(text) }
        serialMatched = text.contains(expectLine)
        if let terminalMarker { terminalMatched = text.contains(terminalMarker) }
        if serialMatched && terminalMatched {
            if evidenceSince == nil { evidenceSince = Date() }
            if Date().timeIntervalSince(evidenceSince!) >= terminalDwell {
                if runner.vm.state != .running {
                    print("FAILURE: VM left the running state during terminal dwell (state=\(runner.vm.state.rawValue)).")
                    finish(success: false)
                    return
                }
                print("SUCCESS: requested serial evidence remained present during terminal dwell.")
                print("----- captured serial console -----")
                print(text)
                print("-----------------------------------")
                finish(success: true)
                return
            }
        } else {
            evidenceSince = nil
        }
    }

    captureScreenshotIfDue()

    // `--timeout 0` = no deadline (an interactive session runs until the
    // operator interrupts it); every existing invocation passes a positive
    // timeout, so this guard is behavior-preserving for the gate fleet.
    if timeout > 0 && Date() > deadline {
        if screenshotSaved && terminalMarker == nil {
            print("Timed out waiting for serial output, but a framebuffer screenshot was captured.")
            finish(success: true)
        } else {
            print("FAILURE: requested evidence not observed within \(Int(timeout))s (serial=\(serialMatched), terminal=\(terminalMatched)).")
            if !lastText.isEmpty { print("----- captured serial console (partial) -----\n\(lastText)\n---------------------------------------------") }
            finish(success: false)
        }
        return
    }
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { poll() }
}

// Milestone six card G1 (claim 6053): the 5/10/15 s screenshot capture,
// shared by the evidence poll and scriptPoll so a gated scripted run can
// capture the guest framebuffer.
func captureScreenshotIfDue() {
    if screenshotPath != nil {
        let elapsed = Date().timeIntervalSince(startTime)
        if let next = captureTimes.first(where: { $0 <= elapsed }) {
            captureTimes.removeAll { $0 == next }
            captureScreenshot(at: next)
        }
    }
}

// Card G6 set_visible follow-on (claim 0487): marker-driven capture. Once
// the `--screenshot-after` marker appears in the serial log, capture the
// framebuffer under the stable `-after` name (once only). Deterministic
// where the fixed 5/10/15 s captures are not: the marker appears exactly
// when an EL0 hide/show transition has landed.
func captureScreenshotIfMarker(_ text: String) {
    if let marker = screenshotAfter, !screenshotAfterCaptured, text.contains(marker) {
        screenshotAfterCaptured = true
        if screenshotAfterDelay > 0 {
            DispatchQueue.main.asyncAfter(deadline: .now() + screenshotAfterDelay) {
                captureScreenshotMarker("after")
            }
        } else {
            captureScreenshotMarker("after")
        }
    }
}

// M108b (#2325, ADR 0051): the lane session's 250 ms poll — drains
// CLIP_OUT records to the pasteboard, NOTIFY_OUT records to banners
// (M108d), and carries changeCount bumps out as CLIP_IN records. Runs on
// the main queue like every other runner poll, for the life of the
// process.
func hostIntTick() {
    hostClipboard?.tick()
    hostNotify?.tick()
    hostOutbox?.tick()
    hostSpeech?.tick()
    hostPrint?.tick()
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) { hostIntTick() }
}

// Marker-driven pasteboard write (`--host-clip-in` + `--host-clip-in-after`):
// when the marker appears in the serial stream the text is written to the
// lane's pasteboard — a REAL host change the ordinary changeCount poll then
// carries in, not a seam that skips it. Fires once; the marker line carries
// the byte count, never the text (never-logged contract).
func fireHostClipInIfMarker(_ text: String) {
    guard let marker = hostClipInAfter, !hostClipInFired,
          text.contains(marker) else { return }
    if let body = hostClipInText {
        hostClipInFired = true
        guard let lane = hostClipboard else { return }
        let cc = lane.inject(body)
        FileHandle.standardOutput.write(Data("host-clip: inject len=\(body.utf8.count) changeCount=\(cc) after \"\(marker)\"\n".utf8))
    } else if let path = hostClipInFile, let mime = hostClipInMime {
        hostClipInFired = true
        guard let lane = hostClipboard else { return }
        let cc = lane.injectTyped(path: path, mime: mime)
        FileHandle.standardOutput.write(Data("host-clip: inject mime=\(mime) changeCount=\(cc) after \"\(marker)\"\n".utf8))
    }
}

// M108c (#2326, ADR 0051 D6): marker-driven file drop (`--host-drop-file` +
// `--host-drop-after`). When the marker appears in the serial stream the
// listed paths go through HostDrop.drop — the SAME function the NSView's
// performDragOperation calls — so a class-B run exercises the real handler:
// sanitize, caps, temp+rename into INBOX/, then the signed DROP notices.
// Fires once; the marker names a file COUNT, never contents (never-logged).
func fireHostDropIfMarker(_ text: String) {
    guard let marker = hostDropAfter, !hostDropFired, !hostDropFiles.isEmpty,
          text.contains(marker) else { return }
    hostDropFired = true
    guard let lane = hostDrop else {
        hostIntRefuse("drop", .laneDisabled)
        return
    }
    FileHandle.standardOutput.write(Data("host-drop: inject files=\(hostDropFiles.count) after \"\(marker)\"\n".utf8))
    lane.drop(urls: hostDropFiles.map { URL(fileURLWithPath: $0) })
}

// M81f (#1766): marker-driven host-side share mutation. When the
// --host-write-after marker appears in the serial stream, the runner writes
// the --host-write-content body into --host-write under the share dir
// (macOS-side, FileManager). The serial marker is CHOREOGRAPHY (when to
// mutate); how the guest comes to observe the change through the file
// channel is the behavior under test. Fires once; a failed write is named
// on stdout, never silent.
func fireHostWriteIfMarker(_ text: String) {
    guard let marker = hostWriteAfter, !hostWriteFired, let relPath = hostWritePath,
          text.contains(marker) else { return }
    hostWriteFired = true
    guard let root = cvcFileShareDir else { return }
    let url = URL(fileURLWithPath: root).appendingPathComponent(relPath)
    let body = hostWriteContent ?? ""
    let payload = body.hasSuffix("\n") ? body : body + "\n"
    do {
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try payload.write(to: url, atomically: true, encoding: .utf8)
        FileHandle.standardOutput.write(Data("HOST-WRITE: wrote \(relPath) (\(payload.utf8.count) bytes) after \"\(marker)\"\n".utf8))
    } catch {
        FileHandle.standardOutput.write(Data("HOST-WRITE: FAILED for \(relPath): \(error)\n".utf8))
    }
}

// M114d (#2457): the inspector's feed — every poll hands it the same
// serial-log snapshot it just read (the model's offset ingest keeps only
// the new lines), plus a lane-directory seq scan when HostInt is armed:
// a record filename's seq counts whether or not the file survives to the
// next tick. Runs before fireHostCmdsIfMarker so a verb firing on a
// marker this tick already saw the line ingested — `saw=` in a receipt
// is then the model's truth, not a race.
func feedInspector(_ text: String) {
    inspector.ingest(serialText: text)
    if let laneDir = hostIntSession?.laneDir {
        for tag in [HostInt.Tag.clipIn, .clipOut, .notifyOut, .drop, .outbox, .notifyFocus, .print] {
            for (_, seq) in HostInt.records(laneDir: laneDir, tag: tag) ?? [] {
                inspector.noteHostIntSeq(seq)
            }
        }
    }
}

// The runner's own `host-int: refuse` sites report to the model too —
// lane-internal refusals reach stdout only (the model doc names that
// limit); these are the refusals main.swift itself emits.
func hostIntRefuse(_ lane: String, _ reason: HostInt.Refusal) {
    HostInt.logRefuse(lane: lane, reason: reason)
    inspector.noteHostMarker("host-int: refuse lane=\(lane) reason=\(reason.rawValue)")
}

// M120e (#2562): one postmortem for every poll's "VM ended" observation —
// the inspector's card + auto-open and the `host-inspector: postmortem`
// receipt are the same model state the session window's drawer shows
// (the index's identical-seam rule: headless modes get the model truth,
// the session gets the real pane). Idempotent — a still-dead VM's repeat
// observation re-notes nothing.
func noteVMEnd(_ state: VZVirtualMachine.State) {
    inspector.noteStop(PostmortemCard(
        ending: state == .error ? .crashed : .stopped,
        state: "\(state.rawValue)",
        reason: vmDelegate.stopReasons.reason ?? StopReasonRecorder.noReason,
        serialLines: inspector.ingestedCount))
}

// M183b (#3172): marker-driven balloon target writes
// (`--balloon-target-after <marker>:<MiB>`). When a trigger's marker
// appears in the serial stream the runner writes that many MiB to the
// device's targetVirtualMachineMemorySize — the public VZ property the
// header documents as the balloon request channel: a value below
// memorySize asks the guest to inflate, a return to memorySize asks it
// to deflate. The serial marker is CHOREOGRAPHY (when the host asks);
// the guest's own inflate/deflate markers are the evidence. Each
// trigger fires once; a missing device is named on stdout, never
// silent. The write itself is dispatched on the VM's queue (the
// SuspendCtl/restore precedent — VZ device mutation rides runner.queue).
func fireBalloonTargetsIfMarker(_ text: String) {
    for i in balloonTargetTriggers.indices {
        if balloonTargetTriggers[i].fired { continue }
        let trigger = balloonTargetTriggers[i]
        guard text.contains(trigger.marker) else { continue }
        balloonTargetTriggers[i].fired = true
        guard let balloon = runner.vm.memoryBalloonDevices.first
                as? VZVirtioTraditionalMemoryBalloonDevice else {
            FileHandle.standardOutput.write(Data("balloon: target write dropped — no VZVirtioTraditionalMemoryBalloonDevice (marker \"\(trigger.marker)\")\n".utf8))
            continue
        }
        let target = trigger.targetMiB * 1024 * 1024
        FileHandle.standardOutput.write(Data("balloon: target \(trigger.targetMiB)MiB after \"\(trigger.marker)\"\n".utf8))
        runner.queue.async {
            balloon.targetVirtualMachineMemorySize = target
        }
    }
}

// M114a (#2454, index #2447): `--host-cmd-after <marker>:<verb>` — the
// headless mirror of the HostCommand bus. When a trigger's marker appears
// in the serial stream the verb dispatches through the SAME bus the
// AppKit menu items use, so a class-B run exercises the real seam instead
// of a synthesized click (index thesis 1). Each trigger fires once; the
// dispatch itself writes the `host-ui:` receipt.
func fireHostCmdsIfMarker(_ text: String) {
    // M139a: the shared per-session fire — the primary's list and bus.
    // A `vm<k>:`-bound trigger fires only on session k's own scan; the
    // `new-window@<path>` operand is staged by the shared body.
    fireHostCmdTriggers(&hostCmdTriggers, on: commandBus, session: 1, text: text)
}

// Claim 0680 (issue #523 item 3 capstone): marker-driven snapshot requests.
// When a registered --snapshot-after marker appears in the serial stream,
// enqueue one kind-4 control message; the guest composites and streams the
// scanout back over queue 4, and the delegate writes the raw BGRX file.
// The serial marker is CHOREOGRAPHY (when to ask), not evidence — the
// pixels themselves travel the custom-virtio channel.
func fireSnapshotsIfMarker(_ text: String) {
    #if SPIKE
    for i in snapshotTriggers.indices {
        if snapshotTriggers[i].fired { continue }
        let trigger = snapshotTriggers[i]
        guard text.contains(trigger.marker) else { continue }
        snapshotTriggers[i].fired = true
        guard #available(macOS 27.0, *) else { continue }
        FileHandle.standardOutput.write(Data("CVC-SNAP-REQ: kind-4 request scheduled after \"\(trigger.marker)\" transport=cv-input → \(trigger.outPath)\n".utf8))
        CustomVirtioSpike.deviceQueue.async {
            // Issue #1788: claim pendingSnapPath on the device queue, where
            // the stream's header message consumes it — so the teardown drain
            // (which reads the pair with a sync hop onto this same serial
            // queue) cannot observe a fired trigger whose request has not
            // been sent yet, and cannot miss one that has.
            //
            // Run-03 coalescing fix: park the request instead of arming the
            // slot directly. pendingSnapPath is a SINGLE slot and the guest's
            // snap_pending is a bool: a second request sent while the first
            // is still armed-but-unstreamed overwrites the label, and two
            // requests drained by one guest poll_input merge into ONE stream
            // (the live-term-depth snap-03-* misfile, observed #1725-era and
            // again on #3121). At most one request may be armed at a time;
            // the parked FIFO releases on the stream's header, so every
            // request gets its own stream and its own out path.
            CustomVirtioSpike.parkSnapshotRequest(trigger.outPath)
        }
    }
    #endif
}

// Claim 6684: script-mode lifecycle. Polls the serial log for the expected
// transcript; success (exit 0) once it appears and the claim-4912 tail
// window has elapsed, failure on timeout or an early VM stop.
// Issue #990 (claim #997): three robustness fixes for the silent-death
// flake (the run ended rc=1 with NO verdict while the guest had completed
// the whole flow byte-clean):
//   1. LOSSY decode — `String(decoding:as:)` never returns nil, so a
//      transient/partial multi-byte sequence at a poll instant can no
//      longer stall matching (the old `String(data:encoding:.utf8)`+
//      `try?` pair silently skipped EVERY match check for that poll and,
//      if the tail byte was permanently dangling, forever).
//   2. UNBUFFERED verdicts — the SUCCESS/FAILURE lines go through
//      FileHandle.standardOutput (direct write), so a killed/exited
//      process can no longer lose the verdict to stdio buffering.
//   3. Poll heartbeat — an unbuffered stderr line every 30 s proves the
//      poll recursion is alive; its absence in evidence localizes the
//      death to a specific window instead of "no verdict at all".
var scriptPollLastBeat: Date = Date()

// M115d (#2463): set by SuspendCtl's accepted-suspend tail — the stop a
// suspended VM lands is the teardown the guest asked for, so the poll's
// early-stop failure branch must not outrace it.
var suspendStopInFlight = false

func scriptPoll(matchedAt: Date? = nil) {
    var matchedAt = matchedAt
    // An early VM stop ends the run: pass when the expected transcript was
    // already observed (the tail window simply cannot capture more output),
    // otherwise fail. A ctl `suspend` teardown is its own success path.
    if !suspendStopInFlight && vmDidStart && (runner.vm.state == .stopped || runner.vm.state == .error) {
        // M120e (#2562): the drawer notes the end in script mode too —
        // the postmortem card + auto-open is the same model state the
        // session window would show, and the receipt is the live leg's
        // "drawer opened" evidence.
        noteVMEnd(runner.vm.state)
        // Issue #1024: the state was sampled BEFORE this tick's log read —
        // a VM that stops in the gap after the previous read makes this
        // branch see "stopped" while the transcript already sits in the
        // file from that same gap. Re-observe the log before judging: the
        // old order failed fully-green runs (rc=1, all asserts green) when
        // the stop landed in the inter-poll window.
        let textAtStop = String(decoding: (try? Data(contentsOf: serialURL)) ?? Data(), as: UTF8.self)
        if matchedAt == nil, let expect = scriptExpect, textAtStop.contains(expect) {
            FileHandle.standardOutput.write(Data("SUCCESS: expected transcript '\(expect)' observed in the serial log (VM ended; log re-read at verdict time, issue #1024).\n".utf8))
            finish(success: true)
            return
        }
        if matchedAt != nil {
            finish(success: true)
        } else {
            // Claim #1278: name the reason, not just the state. `state=3` on
            // its own is what made #1261 unfalsifiable from the outside.
            FileHandle.standardError.write(Data("FAILURE: VM ended before the expected transcript appeared (state=\(runner.vm.state.rawValue)).\(vmDelegate.verdict())\n".utf8))
            finish(success: false)
        }
        return
    }
    // Lossy decode (fix 1): invalid bytes become U+FFFD, never nil.
    let text = String(decoding: (try? Data(contentsOf: serialURL)) ?? Data(), as: UTF8.self)
    if !text.isEmpty { lastText = text; feedInspector(text) }
    if matchedAt == nil, let expect = scriptExpect, text.contains(expect) {
        // Claim 4912: the kernel prints the exec reap/report lines
        // (`tasks` / `procs <name> exited status=N`) from the shell idle
        // loop, which only wakes on the 1 Hz EL1 timer tick — up to ~1 s
        // AFTER the program's own last marker lands in this log. Tearing
        // down on the first match (the pre-4912 behavior) stopped the VM
        // inside that window and dropped the tail, flaking the live
        // gates. Hold the VM through `scriptExpectTail` so the tail
        // reaches the log, then finish. 0 = legacy immediate stop.
        if scriptExpectTail > 0 {
            FileHandle.standardOutput.write(Data("script-expect: transcript '\(expect)' observed — holding the VM for the \(scriptExpectTail)s tail window so post-marker kernel output (reap lines) reaches the serial log\n".utf8))
            matchedAt = Date()
        } else {
            FileHandle.standardOutput.write(Data("SUCCESS: expected transcript '\(expect)' observed in the serial log.\n".utf8))
            finish(success: true)
            return
        }
    }
    if let m = matchedAt, Date().timeIntervalSince(m) >= scriptExpectTail {
        let expect = scriptExpect ?? "<none>"
        FileHandle.standardOutput.write(Data("SUCCESS: expected transcript '\(expect)' observed in the serial log (claim-4912 tail window \(scriptExpectTail)s elapsed; post-marker output captured).\n".utf8))
        finish(success: true)
        return
    }
    captureScreenshotIfDue()
    captureScreenshotIfMarker(lastText)
    fireSnapshotsIfMarker(lastText)
    fireHostWriteIfMarker(lastText)
    fireHostClipInIfMarker(lastText)
    fireHostDropIfMarker(lastText)
    fireHostCmdsIfMarker(lastText)
    fireBalloonTargetsIfMarker(lastText)
    // Fix 3: the liveness heartbeat — unbuffered stderr every 30 s.
    if Date().timeIntervalSince(scriptPollLastBeat) >= 30 {
        scriptPollLastBeat = Date()
        FileHandle.standardError.write(Data("scriptPoll: alive (log=\(text.count) bytes, expect=\(scriptExpect ?? "<none>"), matched=\(matchedAt != nil))\n".utf8))
    }
    // `--timeout 0` = no deadline (shared with the evidence path above).
    if timeout > 0 && Date() > deadline {
        if matchedAt != nil {
            // The transcript appeared but --timeout cut the tail window
            // short — the gate's evidence is already in the log; pass.
            FileHandle.standardOutput.write(Data("SUCCESS: expected transcript '\(scriptExpect ?? "<none>")' observed before the deadline (claim-4912 tail window cut short by --timeout).\n".utf8))
            finish(success: true)
            return
        }
        FileHandle.standardOutput.write(Data("FAILURE: expected transcript '\(scriptExpect ?? "<none>")' not observed within \(Int(timeout))s (log=\(text.count) bytes).\n".utf8))
        // Issue #1024: re-read the serial log NOW — the log we just measured
        // against was read at the TOP of this poll tick, and the VM has run
        // for another 0.5 s since. Under host load VZ virtual time dilates
        // (the 1 Hz kernel heartbeat can land arbitrarily late on the wall
        // clock), and a marker that appeared in that gap made this path
        // exit 1 on a run whose gate evidence was fully intact (observed:
        // live-win-move rc=1 with all 25 asserts + both snapshots green).
        // A stale-read failure verdict is an observation bug, not gate
        // evidence; the log is the record either way.
        let finalText = String(decoding: (try? Data(contentsOf: serialURL)) ?? Data(), as: UTF8.self)
        if scriptExpect != nil, finalText.contains(scriptExpect!) {
            FileHandle.standardOutput.write(Data("SUCCESS: expected transcript '\(scriptExpect!)' observed in the serial log (raced the deadline — log re-read at verdict time, issue #1024).\n".utf8))
            finish(success: true)
            return
        }
        finish(success: scriptExpect == nil)
        return
    }
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { scriptPoll(matchedAt: matchedAt) }
}

var signalSources: [DispatchSourceSignal] = []

// All host exit sources funnel through HostLifecycle on the main queue.
// VZ state/stop remain on the VM queue; a close during start waits for its
// completion instead of racing a second VZ operation.
func stopHostSession(_ reason: String, code: Int32) {
    print("host-session: stopping reason=\(reason) pid=\(getpid())")
    fflush(stdout)
    let stopDeadline = Date().addingTimeInterval(15)
    runner.queue.async {
        @Sendable func complete(_ exitCode: Int32) {
            drainTeeBeforeExit("host-session \(reason)")
            restoreTerminal()
            print("host-session: stopped pid=\(getpid()) state=\(runner.vm.state.rawValue) code=\(exitCode)\(vmDelegate.verdict())")
            fflush(stdout)
            exit(exitCode)
        }
        // Also bounds a stop() whose completion never arrives. This is a
        // failure exit, not evidence that VZ stopped successfully.
        runner.queue.asyncAfter(deadline: .now() + 15) {
            FileHandle.standardError.write(Data("host-session: VM stop timed out, terminating owning process\n".utf8))
            complete(1)
        }
        @Sendable func step() {
            if runner.vm.state == .stopped || runner.vm.state == .error {
                complete(code)
            } else if runner.vm.canStop {
                runner.vm.stop { error in
                    if let error {
                        FileHandle.standardError.write(Data("host-session: VM stop failed: \(error)\n".utf8))
                        complete(1)
                    } else {
                        complete(code)
                    }
                }
            } else if Date() < stopDeadline {
                runner.queue.asyncAfter(deadline: .now() + 0.05) { step() }
            } else {
                FileHandle.standardError.write(Data("host-session: VM stop timed out, terminating owning process\n".utf8))
                complete(1)
            }
        }
        step()
    }
}

func hostSessionPoll() {
    guard let hostApplication,
          hostApplication.lifecycle.state != .stopping else { return }
    // M114a: --host-cmd-after fires in session mode on the same serial-log
    // scan the scripted poll uses — the mirror of choosing the menu item.
    let sessionText = String(decoding: (try? Data(contentsOf: serialURL)) ?? Data(), as: UTF8.self)
    if !sessionText.isEmpty { lastText = sessionText; feedInspector(sessionText) }
    fireHostCmdsIfMarker(lastText)
    runner.queue.async {
        let state = runner.vm.state
        DispatchQueue.main.async {
            if vmDidStart && (state == .stopped || state == .error) {
                // M114d (#2457) + M120e (#2562): the in-window postmortem
                // — the drawer opens itself on the stop/crash card
                // (state + the VZ delegate's reason + how deep the
                // serial tail is), and the host-inspector: receipt is
                // the same line the headless end paths print.
                noteVMEnd(state)
                hostApplication.showFailure("VM ended (state=\(state.rawValue)).\(vmDelegate.verdict())")
            } else if timeout > 0 && Date() > deadline {
                hostApplication.stop("session-timeout")
                return
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { hostSessionPoll() }
        }
    }
}

// Claim 2188: single-teardown guard for the console exit paths. All of
// them run on the main queue (the poll recursion and the signal sources
// both dispatch there), so a plain flag serializes them: the first path
// wins, later ones (a second signal, or a signal racing the timeout)
// return without double-stopping the VM or double-exiting.
var consoleExitStarted = false

/// Claim 2188: stop the VM if it is still running (every guest byte VZ
/// will deliver then lands in the pipe), then let the tee flush the
/// remaining bytes into the serial log before the process exits.
func stopAndDrainForExit(_ context: String) {
    runner.queue.async {
        runner.vm.stop { _ in
            drainDataDiskDevice()
            drainTeeBeforeExit(context)
            exitWithTerminalRestore(consoleExitCode)
        }
    }
}

var consoleExitCode: Int32 = 0

func beginConsoleExit(_ context: String, code: Int32, restore: Bool = false) {
    guard !consoleExitStarted else { return }
    consoleExitStarted = true
    consoleExitCode = code
    if restore {
        restoreTerminal()
        FileHandle.standardError.write(Data("console: \(context) — terminal restored, draining guest output, exiting\n".utf8))
    } else {
        print("console: \(context) — ending session")
    }
    stopAndDrainForExit(context)
}

func installSignalHandlers() {
    for sig: Int32 in [SIGINT, SIGTERM, SIGHUP] {
        signal(sig, SIG_IGN) // suppress default termination; the source below handles it
        let src = DispatchSource.makeSignalSource(signal: sig, queue: .main)
        src.setEventHandler {
            if hostSessionMode {
                hostApplication?.stop("signal-\(sig)", code: 128 + sig)
                return
            }
            if consoleMode {
                // Claim 2188: stop the VM (closes the serial pipe) and wait for
                // the tee to flush, instead of the old fixed 0.4 s sleep that
                // could still race the tee thread's final write. `restore:` is
                // true — the terminal is raw while the console session runs.
                beginConsoleExit("caught signal \(sig)", code: 128 + sig, restore: true)
                return
            }
            // Scripted/plain headless runs have no console teardown, but a
            // default signal death skips atexit — leaking the throwaway ASIF
            // overlay dir ($TMPDIR/virelai-overlay-*). Run the same teardown
            // inline, then die BY the signal: a plain exit() would read to the
            // harness as a clean child exit, and a gate shell holding a
            // pending SIGINT discards it when the child exits normally — ^C
            // would stop aborting runs.
            restoreTerminal()
            if let p = overlayCleanupPath,
               ProcessInfo.processInfo.environment["VIRELAI_KEEP_OVERLAY"] == nil {
                try? FileManager.default.removeItem(atPath: p)
                overlayCleanupPath = nil
            }
            signal(sig, SIG_DFL)
            kill(getpid(), sig)
            exit(128 + sig) // unreachable unless the re-raise was blocked
        }
        src.resume()
        signalSources.append(src)
    }
}

func consolePoll() {
    // M114d (#2457): the drawer's tail feeds off the same serial log the
    // tee is writing — the model's offset ingest keeps only new lines.
    if let text = try? String(contentsOf: serialURL, encoding: .utf8), !text.isEmpty {
        feedInspector(text)
    }
    let state = runner.vm.state
    if vmDidStart && (state == .stopped || state == .error) {
        // M120e (#2562): the console-mode end notes the postmortem too —
        // same model, same receipt.
        noteVMEnd(state)
        // The VM already ended, so VZ closed the serial pipe — the tee is
        // draining on its own; wait for it instead of the fixed 0.5 s sleep.
        // Claim #1278: report the delegate's reason alongside the state on the
        // console-exit path too — this is the branch a passing-but-dead boot
        // takes, so it is the one that most often hid an error.
        beginConsoleExit("VM ended (state=\(state.rawValue)).\(vmDelegate.verdict())", code: 0)
        return
    }
    if consoleTimeout > 0, Date() > consoleDeadline {
        beginConsoleExit("session timed out after \(Int(consoleTimeout))s (VM state=\(state.rawValue))", code: 0)
        return
    }
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) { consolePoll() }
}

func exitWithTerminalRestore(_ code: Int32) -> Never {
    restoreTerminal()
    exit(code)
}
