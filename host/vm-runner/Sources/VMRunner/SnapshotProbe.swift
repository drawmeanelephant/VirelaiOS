// Snapshot/probe helpers: marker-ladder + NVRAM console reads,
// kind-4 snapshot drain, and the suspend/snapshot ctl handles.

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


// M120c (#2560): the Machine-menu persistence verbs. The model resolves
// the profile/share/base/key from argv itself; the pieces only the
// runner owns are the VZ drivers and the live posture reads — the same
// SuspendCtl/SnapshotCtl pair the vsock-ctl wire serves, armed here for
// EVERY run so a scripted --host-cmd-after dispatches the identical
// seam the session menu does. The suspend driver's accept tail stops
// the run exactly like the guest-initiated suspend; the snapshot
// driver's resume-after-reply is a no-op on a refused save.
var machineSaveUnsupported: String? = nil

let machineSuspendCtl = SuspendCtl(vm: runner.vm, queue: runner.queue,
                                 savedStateDir: activeProfile?.savedStateURL,
                                 unsupported: machineSaveUnsupported)

let machineSnapshotCtl = SnapshotCtl(
    vm: runner.vm, queue: runner.queue,
    shareDir: (cvcFileShareDir ?? virtioFSShareDir)
        .map { URL(fileURLWithPath: $0, isDirectory: true) },
    unsupported: machineSaveUnsupported)

// Issue #1788: hold the VM before the teardown while an armed kind-4
// snapshot stream is still in flight, then run `done`.
//
// A kind-4 snapshot is a 3.5 MiB round trip (claim 0680: 113 chunks back
// over queue 4 after one request over queue 3), and a `--snapshot-after`
// marker can be printed microseconds before the `--script-expect` match
// that ends the run — live-web boot 13 prints `web-ink-20s` and then
// `web-ink-done` back to back, so ONE poll tick fires the second request
// and records the match. The old path then stopped the device one
// `--script-expect-tail` (default 1.5 s) later, mid-transfer, and the gate
// failed on a .raw the guest was still streaming. Other specs arm a
// snapshot on the very marker they expect (live-wm4-paint A/B,
// live-win-hig), so the honest fix belongs here, not in one spec.
//
// Bounded by --snapshot-drain-timeout; a stream that never lands (dead
// guest) costs that many seconds once and says so loudly.
func drainSnapshotsBeforeStop(_ context: String, then done: @escaping () -> Void) {
    guard snapshotDrainTimeout > 0, !snapshotTriggers.isEmpty else { done(); return }
    let started = Date()
    let deadline = started.addingTimeInterval(snapshotDrainTimeout)
    var announced = false
    func step() {
        // Nothing can land once the device is gone or faulted.
        if runner.vm.state == .stopped || runner.vm.state == .error { done(); return }
        if !snapshotStreamInFlight() {
            if announced {
                FileHandle.standardOutput.write(Data("CVC-SNAP-DRAIN: in-flight stream landed in \(String(format: "%.1f", Date().timeIntervalSince(started)))s — stopping the device (\(context))\n".utf8))
            }
            done()
            return
        }
        if !announced {
            announced = true
            FileHandle.standardOutput.write(Data("CVC-SNAP-DRAIN: kind-4 stream still in flight at \(context) — holding the VM up to \(snapshotDrainTimeout)s so the frame lands\n".utf8))
        }
        guard Date() < deadline else {
            FileHandle.standardError.write(Data("CVC-SNAP-DRAIN: TIMEOUT after \(snapshotDrainTimeout)s with a kind-4 stream still in flight — its .raw will be missing or short (\(context))\n".utf8))
            done()
            return
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) { step() }
    }
    step()
}

// Issue #1788: is a kind-4 scanout stream in flight right now? The read
// hops onto the device's serial queue — the same discipline every callback
// uses — so the (pendingSnapPath, snap) pair is read consistently, and a
// fire already dispatched onto that queue has necessarily run (FIFO), so a
// request enqueued microseconds earlier is never missed.
func snapshotStreamInFlight() -> Bool {
    #if SPIKE
    guard #available(macOS 27.0, *) else { return false }
    return CustomVirtioSpike.snapshotStreamInFlight()
    #else
    return false
    #endif
}

// ---------------------------------------------------------------------------
// ADR 0004 D4 fixed-memory-marker fallback (working form, claim 0009): the
// kernel writes each takeover stage as the EFI non-volatile variable
// `VirelaiM2` (VendorGuid M2M2_VIRELAIOS-M). EFI runtime services survive
// ExitBootServices on VZ, so after the run the host reads the variable store
// (artifacts/efi-vars.bin) and sees the ordered ladder of stages the kernel
// reached. A missing later stage names the crash window: a ladder ending at
// M2_MAPD! (identity map built, pre-install) with no M2_MMUP! means the MMU
// switch itself faulted — observed on every VZ run (claim 0009).
// ---------------------------------------------------------------------------
// The kernel's stage words, little-endian as stored by SetVariable. The LE
// byte strings are distinctive ASCII (e.g. "YRTNE_2M" for M2_ENTRY), so a
// plain byte scan of the store finds every instance in file order; the store
// is append-per-write, so file order == write order and the LAST instance is
// the kernel's final stage.
let markerNeedles: [(name: String, leBytes: [UInt8])] = [
    ("M2_TABLE", [0x45, 0x4c, 0x42, 0x41, 0x54, 0x5f, 0x32, 0x4d]), // ELBAT_2M
    ("M2_SERIA", [0x41, 0x49, 0x52, 0x45, 0x53, 0x5f, 0x32, 0x4d]), // AIRES_2M
    ("M2_ENTRY", [0x59, 0x52, 0x54, 0x4e, 0x45, 0x5f, 0x32, 0x4d]), // YRTNE_2M
    ("M2_CMAP!", [0x21, 0x50, 0x41, 0x4d, 0x43, 0x5f, 0x32, 0x4d]), // !PAMC_2M
    ("M2_MAPD!", [0x21, 0x44, 0x50, 0x41, 0x4d, 0x5f, 0x32, 0x4d]), // !DPAM_2M
    ("M2_PREX!", [0x21, 0x58, 0x45, 0x52, 0x50, 0x5f, 0x32, 0x4d]), // !XERP_2M
    ("M2_EXIT!", [0x21, 0x54, 0x49, 0x58, 0x45, 0x5f, 0x32, 0x4d]), // !TIXE_2M
    ("M2_MMUP!", [0x21, 0x50, 0x55, 0x4d, 0x4d, 0x5f, 0x32, 0x4d]), // !PUMM_2M
    ("M2_READY", [0x59, 0x44, 0x41, 0x45, 0x52, 0x5f, 0x32, 0x4d]), // YDAER_2M
    ("M2_RAW!", [0x21, 0x57, 0x41, 0x52, 0x5f, 0x32, 0x4d, 0x00]), // !WAR_2M\0 (claim 0013 probe stage; 7-char word, u64-padded with 0x00)
    ("M2_TXOK!", [0x21, 0x4b, 0x4f, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !KOXT_2M (claim 0013: first TX returned)
    ("M2_TXST!", [0x21, 0x54, 0x53, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !TSXT_2M (virtio flush entered, desc/avail posted)
    ("M2_TXNT!", [0x21, 0x54, 0x4e, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !TNXT_2M (notify write issued)
    ("M2_TXPL!", [0x21, 0x4c, 0x50, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !LPXT_2M (used-ring poll finished)
    ("M2_PEXT!", [0x21, 0x54, 0x58, 0x45, 0x50, 0x5f, 0x32, 0x4d]), // !TXEP_2M (claim 0017: pre-exit TX experiment entered)
    ("M2_PEXD!", [0x21, 0x44, 0x58, 0x45, 0x50, 0x5f, 0x32, 0x4d]), // !DXEP_2M (claim 0017: pre-exit TX experiment flush returned)
    ("M2_TXFL!", [0x21, 0x4c, 0x46, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !LFXT_2M (claim 0018: entered virtio flush)
    ("M2_TXDA!", [0x21, 0x41, 0x44, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !ADXT_2M (desc/avail prepared)
    ("M2_TXCC!", [0x21, 0x43, 0x43, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !CCXT_2M (DMA cache clean completed)
    ("M2_TXBR!", [0x21, 0x52, 0x42, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !RBXT_2M (before first post-exit BAR/common-cfg read)
    ("M2_TXAR!", [0x21, 0x52, 0x41, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !RAXT_2M (after that read)
    ("M2_TXBN!", [0x21, 0x4e, 0x42, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !NBXT_2M (before queue notify MMIO write)
    ("M2_TXAN!", [0x21, 0x4e, 0x41, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !NAXT_2M (after notify)
    ("M2_TXUP!", [0x21, 0x50, 0x55, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !PUXT_2M (entered used-ring poll)
    ("M2_TXUC!", [0x21, 0x43, 0x55, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !CUXT_2M (device changed used.idx)
    ("M2_TXFR!", [0x21, 0x52, 0x46, 0x58, 0x54, 0x5f, 0x32, 0x4d]), // !RFXT_2M (flush returned)
    ("M2_TRA1!", [0x21, 0x31, 0x41, 0x52, 0x54, 0x5f, 0x32, 0x4d]), // !1ART_2M (claim 0020 phase A: pre-EBS TX experiment entered)
    ("M2_TRA2!", [0x21, 0x32, 0x41, 0x52, 0x54, 0x5f, 0x32, 0x4d]), // !2ART_2M (phase A: flush returned)
    ("M2_TRAU!", [0x21, 0x55, 0x41, 0x52, 0x54, 0x5f, 0x32, 0x4d]), // !UART_2M (phase A: used.idx advanced)
    ("M2_TRB1!", [0x21, 0x31, 0x42, 0x52, 0x54, 0x5f, 0x32, 0x4d]), // !1BRT_2M (phase B: post-EBS/pre-MMU experiment entered)
    ("M2_TRB2!", [0x21, 0x32, 0x42, 0x52, 0x54, 0x5f, 0x32, 0x4d]), // !2BRT_2M (phase B: flush returned)
    ("M2_TRBU!", [0x21, 0x55, 0x42, 0x52, 0x54, 0x5f, 0x32, 0x4d]), // !UBRT_2M (phase B: used.idx advanced)
    ("M2_TRC1!", [0x21, 0x31, 0x43, 0x52, 0x54, 0x5f, 0x32, 0x4d]), // !1CRT_2M (phase C: post-MMU experiment entered)
    ("M2_TRC2!", [0x21, 0x32, 0x43, 0x52, 0x54, 0x5f, 0x32, 0x4d]), // !2CRT_2M (phase C: flush returned)
    ("M2_TRCU!", [0x21, 0x55, 0x43, 0x52, 0x54, 0x5f, 0x32, 0x4d]), // !UCRT_2M (phase C: used.idx advanced)
    ("M2_TRD1!", [0x21, 0x31, 0x44, 0x52, 0x54, 0x5f, 0x32, 0x4d]), // !1DRT_2M (phase D: final-location experiment entered)
    ("M2_TRD2!", [0x21, 0x32, 0x44, 0x52, 0x54, 0x5f, 0x32, 0x4d]), // !2DRT_2M (phase D: flush returned)
    ("M2_TRDU!", [0x21, 0x55, 0x44, 0x52, 0x54, 0x5f, 0x32, 0x4d]), // !UDRT_2M (phase D: used.idx advanced)
    ("M2_TRNX!", [0x21, 0x58, 0x4e, 0x52, 0x54, 0x5f, 0x32, 0x4d]), // !XNR T_2M (claim 0020: experiment skipped — transport not armed)
    ("M2_WP_00", [0x30, 0x30, 0x5f, 0x50, 0x57, 0x5f, 0x32, 0x4d]), // 00_PW_2M (claim 7896 walk-probe battery start)
    ("M2_WP_01", [0x31, 0x30, 0x5f, 0x50, 0x57, 0x5f, 0x32, 0x4d]), // 10_PW_2M (claim 7896: P1 kernel-BSS read returned)
    ("M2_WP_02", [0x32, 0x30, 0x5f, 0x50, 0x57, 0x5f, 0x32, 0x4d]), // 20_PW_2M (claim 7896: P2 ram-hi read returned)
    ("M2_WP_03", [0x33, 0x30, 0x5f, 0x50, 0x57, 0x5f, 0x32, 0x4d]), // 30_PW_2M (claim 7896: P3 ram-mid read returned)
    ("M2_WP_04", [0x34, 0x30, 0x5f, 0x50, 0x57, 0x5f, 0x32, 0x4d]), // 40_PW_2M (claim 7896: P4 ram-lo read returned)
    ("M2_WP_05", [0x35, 0x30, 0x5f, 0x50, 0x57, 0x5f, 0x32, 0x4d]), // 50_PW_2M (claim 7896: P5 virtio-BAR read returned — battery complete)
]

/// Read the EFI variable store and return the marker ladder (name, store
/// offset), in file order. Byte-scan only — no struct-layout assumption
/// beyond the value being present: the marker strings are distinctive 8-byte
/// ASCII sequences, and every hit is checked against the needle table.
func readMarkerLadder(from storeURL: URL) -> [(name: String, offset: Int)] {
    guard let data = try? Data(contentsOf: storeURL) else { return [] }
    let bytes = [UInt8](data)
    var hits: [(name: String, offset: Int)] = []
    var i = 0
    while i + 8 <= bytes.count {
        for (name, needle) in markerNeedles {
            var match = true
            var j = 0
            while j < 8 {
                if bytes[i + j] != needle[j] { match = false; break }
                j += 1
            }
            if match { hits.append((name, i)) }
        }
        i += 1
    }
    hits.sort { $0.offset < $1.offset }
    return hits
}

// ---------------------------------------------------------------------------
// Claim 0015: NVRAM console reconstruction. The kernel persists console
// bytes as chunked EFI variables VirelaiC0..N (runtime SetVariable — the
// proven post-exit-safe channel on VZ), each value prefixed with the
// in-band marker "VIRELAIC <4-digit-index>:" inside the value bytes. A
// plain byte scan of the store finds every chunk in file order (the store
// is append-per-write), exactly like the marker ladder — no struct-layout
// parsing. Payloads are concatenated after validating the indices are
// sequential from 0 (a gap means a SetVariable call was dropped; reported
// honestly).
// ---------------------------------------------------------------------------
struct NvramConsoleResult {
    var text: String = ""
    var chunks: Int = 0
    var complete: Bool = true
    var missing: Int? = nil
}

func readNvramConsole(from storeURL: URL) -> NvramConsoleResult {
    var result = NvramConsoleResult()
    guard let data = try? Data(contentsOf: storeURL) else { return result }
    let bytes = [UInt8](data)
    let prefix = Array("VIRELAIC ".utf8) // 9 bytes; +4 digits + ":" = marker at i+13
    let endMarker = Array("VIRELAIC-END".utf8) // 12 bytes; closes every chunk value
    guard prefix.count == 9, endMarker.count == 12 else { return result }

    // Find every chunk start marker, in file order.
    var starts: [(index: Int, pos: Int)] = []
    var i = 0
    while i + 14 <= bytes.count {
        if bytes[i..<(i + 9)].elementsEqual(prefix) {
            var ok = true
            var index = 0
            for k in 9..<13 {
                let c = bytes[i + k]
                guard c >= 0x30, c <= 0x39 else { ok = false; break }
                index = index * 10 + Int(c - 0x30)
            }
            if ok, bytes[i + 13] == 0x3a { // ":"
                starts.append((index, i))
                i += 14
                continue
            }
        }
        i += 1
    }

    // Payload of each chunk = bytes between its start marker and the first
    // VIRELAIC-END after it. The end marker is written atomically with the
    // value by the kernel, so this delimits payloads exactly without parsing
    // the store's structure (variable headers / GUIDs / other variables sit
    // between chunks). Validate the index sequence 0..n-1: a dropped
    // SetVariable shows as a gap and is reported honestly.
    var expected = 0
    var out = Data()
    var sawUnterminated = false
    for (n, (index, pos)) in starts.enumerated() {
        if index != expected {
            result.complete = false
            result.missing = expected
        }
        expected = index + 1
        let payloadStart = pos + 14
        let searchStart = (n + 1 < starts.count) ? min(payloadStart, starts[n + 1].pos) : payloadStart
        var end = -1
        var j = searchStart
        while j + 12 <= bytes.count {
            if bytes[j..<(j + 12)].elementsEqual(endMarker) {
                end = j
                break
            }
            j += 1
        }
        if end < 0 {
            sawUnterminated = true
            continue // no end marker: skip the chunk rather than swallow the store tail
        }
        out.append(contentsOf: bytes[payloadStart..<end])
    }
    result.chunks = starts.count
    if sawUnterminated {
        result.complete = false
        if result.missing == nil { result.missing = expected }
    }
    if result.chunks == 0 { return result }

    result.text = String(decoding: out, as: UTF8.self)
    return result
}

func writeNvramConsole(to path: String, result: NvramConsoleResult) {
    var lines: [String] = []
    lines.append("VIRELAIOS nvram console — claim 0015 (post-exit console bytes via the NVRAM variable channel)")
    lines.append("date=\(ISO8601DateFormatter().string(from: Date()))")
    lines.append("store=\(varsURL.path)")
    lines.append("chunks=\(result.chunks) complete=\(result.complete)\(result.missing.map { " missing=\($0)" } ?? "") bytes=\(result.text.count)")
    lines.append("NOTE: these bytes rode the NVRAM variable channel (runtime SetVariable), not the virtio serial pipe.")
    lines.append("")
    lines.append("----- reconstructed console stream -----")
    lines.append(result.text)
    lines.append("----------------------------------------")
    let text = lines.joined(separator: "\n") + "\n"
    do {
        try text.write(toFile: path, atomically: true, encoding: .utf8)
        print("nvram console output saved to \(path) (\(result.chunks) chunk(s), \(result.text.count) bytes)")
    } catch {
        FileHandle.standardError.write(Data("ERROR: could not write nvram console output to \(path): \(error)\n".utf8))
    }
}

func writeMarkerDump(to path: String, ladder: [(name: String, offset: Int)]) {
    var lines: [String] = []
    lines.append("VIRELAIOS marker dump — ADR 0004 D4 fixed-memory-marker fallback (NVRAM ladder)")
    lines.append("date=\(ISO8601DateFormatter().string(from: Date()))")
    lines.append("store=\(varsURL.path)")
    lines.append("")
    lines.append("marker ladder (file order == write order):")
    if ladder.isEmpty {
        lines.append("  (none — no M2_* marker instance in the store)")
    } else {
        for (name, offset) in ladder {
            lines.append("  \(name) @0x\(String(format: "%x", offset))")
        }
    }
    let text = lines.joined(separator: "\n") + "\n"
    do {
        try text.write(toFile: path, atomically: true, encoding: .utf8)
        print("marker dump saved to \(path) (\(ladder.count) marker instance(s))")
    } catch {
        FileHandle.standardError.write(Data("ERROR: could not write marker dump to \(path): \(error)\n".utf8))
    }
}
