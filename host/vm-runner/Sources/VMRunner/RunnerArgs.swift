// Command-line surface: every parsed flag's storage.
// Set by the parse loop in main.swift; read by the lane files.

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


let arguments = CommandLine.arguments

let diskImagePath = arguments.count > 1 ? arguments[1] : "artifacts/disk.img"

var serialLogPath = "artifacts/vm-serial.log"

var screenshotPath: String?

// Card G6 set_visible follow-on (claim 0487): `--screenshot-after <marker>`
// captures the framebuffer ONCE when the marker appears in the serial log
// (deterministic, marker-driven — the fixed 5/10/15 s captures cannot
// guarantee a capture lands inside an EL0 hide/show window). OFF by default;
// a no-op without `--screen` (validated at parse time).
var screenshotAfter: String?

var screenshotAfterCaptured = false

// M137d (#2774): seconds to wait after the --screenshot-after marker
// before grabbing the framebuffer. The marker prints when the guest flips
// the state; the composited frame lags by a frame or two, so an instant
// capture can land mid-transition (observed: the space-2 grab still held
// space-1 pixels). Zero preserves the historical capture-on-marker.
var screenshotAfterDelay: Double = 0

// M81f (#1766): `--host-write <relpath>` + `--host-write-content <text>` +
// `--host-write-after <marker>` — one marker-anchored macOS-side share
// mutation: when <marker> appears in the serial log, write <text> (+ newline)
// into <relpath> under the --cvc-file share dir. Test CHOREOGRAPHY for the
// change-feed gates: the guest must observe the mutation through the same
// stateless file channel it always reads through, so the write happens
// outside the guest, exactly like a real host-side edit. Three flags, not a
// joined `marker:path:text` value — guest markers carry colons (`gofiles:
// watch armed`), so no combined format can delimit them (observed: the
// colon-split first gate run wrote the wrong path). Requires --cvc-file.
var hostWritePath: String?

var hostWriteContent: String?

var hostWriteAfter: String?

var hostWriteFired = false

// Milestone six card G1 (claim 6053): `--display` attaches the virtio-gpu
// device and shows the VM window for the whole session (the machine boots
// to a screen). OFF by default — without the flag config.graphicsDevices
// stays [] exactly as before, so every existing gate stays byte-identical.
// `--screenshot <path>` remains the evidence capture (the two combine:
// `--display --screenshot`).
var displayMode = false

// M128c (#2607): `var`, not `let` — a Finder-launched session arrives
// bare and this flag is set when the open-documents event lands below.
var hostSessionMode = CommandLine.arguments.contains("--host-session")

var hostApplication: HostApplication?

// Milestone seven card I1 (claim 4272; premise corrected by claim 3868):
// `--input` attaches the keyboard + pointing devices
// (VZUSBKeyboardConfiguration +
// VZUSBScreenCoordinatePointingDeviceConfiguration). OFF by default —
// without the flag config.keyboards/pointingDevices stay [] exactly as
// before, so every existing gate stays byte-identical.
//
// CLAIM-TIME OBSERVATION (2026-08-13): these configs do NOT present a
// virtio-input device (DID 0x1052). VZ exposes them as an Apple XHCI USB
// host controller — PCI VID=0x106b DID=0x1a06 CLS=0x0c0330, two MMIO
// BARs (0x50001000 + 0x50000000) — with the keyboard/pointer as USB HID
// devices behind it. So screen-side input needs a USB XHCI + HID stack,
// not a virtio-input transport. See claim 3868 (historical — in git history).
var inputMode = false

// Milestone fifteen card A1 (claim 6140): `--sound` attaches the
// virtio-snd device (VZVirtioSoundDeviceConfiguration with one output
// stream + a VZHostAudioOutputStreamSink). OFF by default — without the
// flag config.soundDevices stays [] exactly as before, so every existing
// gate stays byte-identical. A1 is the TRANSPORT: the guest discovers the
// device, negotiates, and arms the control queue; the PCM playback path
// (the audible beep) is card A2.
var soundMode = false

// M183b (issue #3172): `--balloon` attaches the traditional memory
// balloon device (VZVirtioTraditionalMemoryBalloonDeviceConfiguration —
// public VZ API since macOS 11, one per VM). OFF by default — without
// the flag config.memoryBalloonDevices stays [] exactly as before, so
// every existing gate stays byte-identical.
var balloonEnabled = false

// M183b: one armed `--balloon-target-after <marker>:<MiB>` instance —
// when <marker> appears in the serial stream the runner writes <MiB>
// MiB to the device's targetVirtualMachineMemorySize (the public VZ
// property: shrinking it below memorySize asks the guest to inflate,
// restoring it asks the guest to deflate). Repeatable; each fires once.
// The serial marker is CHOREOGRAPHY (the host-side trigger the spec leg
// asserts against); the memory transfer itself travels the balloon
// device's inflate/deflate queues. Implies --balloon.
struct BalloonTargetTrigger {
    let marker: String
    let targetMiB: UInt64
    var fired = false
}

var balloonTargetTriggers: [BalloonTargetTrigger] = []

// M97g (#2084): --no-entropy detaches the virtio-entropy device so a gate
// can prove the guest's slot-72 fail-closed behavior while unseeded.
var noEntropy = false

// M43 card U6 (issue #1037, claim #1040): `--usb-msd <disk.img>` attaches a
// USB mass storage device (VZUSBMassStorageDeviceConfiguration wrapping a
// VZDiskImageStorageDeviceAttachment) on an EXPLICIT VZXHCIControllerConfiguration
// (macOS 15+ API) — the first third USB device class the runner can present.
// OFF by default — without the flag config.usbControllers stays [] exactly as
// before (the M7 keyboard/pointer attachments ride their own config surface
// and are untouched), so every existing gate stays byte-identical. The U1
// bulk probe is the first consumer: the guest enumerates the MSD as a third
// XHCI device (bulk EP1 OUT/IN pair) and `usb bulk` reads the engine state.
var usbMsdPath: String? = nil

// M100b (issue #2307, ADR 0050 D1/D2): `--data-disk <img>` attaches a SECOND
// virtio-blk function as the guest's VLFS data disk — the raw image is
// attached as a kernel block device and handed to
// VZDiskBlockDeviceStorageDeviceAttachment (never stacked; the throwaway
// overlay covers the boot disk only) named "virelai-vlfs" via
// blockDeviceIdentifier, synchronizationMode .full so a guest
// VIRTIO_BLK_T_FLUSH is a host-side full sync. A missing file is created
// as a zero-filled 64 MiB raw image — formatting is mkfs.vlfs's job,
// never the runner's. Off by default: without the flag
// config.storageDevices keeps its single boot element.
var dataDiskPath: String? = nil

/// Issue #3112: the kernel block device (`/dev/diskN`) the data-disk
/// image is attached through, and our open handle on its raw node —
/// held for the VM's lifetime so VZ's block-device attachment always
/// has a live device to write.
var dataDiskBlockDev: String? = nil

var dataDiskBlockFH: FileHandle? = nil

// M115c (issue #2462): `--profile <path>` boots a persistent machine from a
// `.virelaivm` bundle instead of the flag-by-flag throwaway pieces. The
// bundle's manifest members fill --vars/--overlay-base/--data-disk/
// --cvc-file and its config.json fills cpus/ram/device defaults — every
// explicit flag wins over the profile member it names. `--profile-init`
// only mints/validates the bundle and exits (session.sh's pre-seed step).
var profilePath: String?

var profileInitOnly = false

// M115d (#2463): `--resume <path>` boots the profile's saved machine
// state instead of the boot image — the continuation of a ctl `suspend`.
var resumeFromPath: String?

// M115f (#2465): `--apply-update` verifies the guest-staged signed set in
// <share>/UPDATE/ (UPDATE.MF + UPDATE.SIG over it) and swaps it in as the
// boot base image, retaining the old base at <base>.prev and discarding
// a profile's persistent overlay.asif. `--rollback-update` swaps .prev
// back. `--update-key <64-hex>` pins the Ed25519 public key the manifest
// must verify under; absent it, a profile's config.json updatePubkey
// applies.
var applyUpdate = false

var rollbackUpdate = false

var updateKeyHex: String?

var memoryMiB = 512

// M129a (#2610): like --cpus, an explicit --memory-mib wins over a profile's
// config.json memoryMiB; without it the bundle's stored value applies.
var memoryMiBExplicit = false

// Milestone seven card I2 (claim 4116): the minimal synthesized-key seam.
// `--input-key <mac-keycode>` posts one keyDown (no keyUp) into the
// VZVirtualMachineView after `--input-key-after <marker>` (default: the
// guest's `usb: enumerated` line), producing ONE deterministic HID report.
// VZ has no programmatic keyboard API — VZUSBKeyboardConfiguration is driven
// only by a view forwarding host key events — so the runner dispatches a
// synthesized NSEvent. The full scripted key-sequence surface is I3.
var inputKeyCode: UInt16?

var inputKeyAfter: String?

// Milestone seven card I3 (claim 6050): the scripted key-SEQUENCE surface.
// `--input-string <ascii>` types the literal string (keyDown + keyUp per
// char, shift for uppercase, `\n` = Enter) into the VZVirtualMachineView
// after `--input-string-after <marker>` (default: the shell's first
// `virelai> ` prompt — typing before the idle loop starts drops keystrokes,
// the interrupt-IN ring buffers one report). The marker watch runs to
// max(40 s, --timeout) — a deep marker needs a session timeout that
// covers it (#2703). This types a real command into Road Pops.
var inputString: String?

var inputStringAfter: String?

// Milestone eight card U2 (claim 1809): the scripted CHORD surface for the
// line-editor live gate. `--input-chords <csv>` types a comma-separated list
// of keystrokes — a printable char, or a named chord (return/up/down/left/
// right/home/end/delete/tab, ctrl-a..ctrl-z, ctrl-space, ctrl-comma) — keyDown + keyUp per chord
// after `--input-chords-after <marker>` (default: the boot self-test line),
// so arrows and Ctrl chords reach the I3 keymap over a real VZ keyboard.
//
// DELIVERY TRANSPORT (read this before writing a gate): the chord sequence
// rides the custom-virtio INPUT queue whenever via-virtio is on — implied by
// --via-virtio, --cvc-file, --cvc-snap, --snapshot-after, and --pointer-virtio
// (the deepest flag implies the shape below it). The cv-input transport needs
// no view/window (headless-safe) but paces at a FIXED 0.25 s per stroke and
// IGNORES --input-chords-delay. The VZVirtualMachineView path (default when
// via-virtio is off) honors --input-chords-delay, so gates that need slow,
// deterministic spacing for cross-process handoffs (launch-then-focus arcs
// like the desktop → FILE.BIN composition) must pass --chords-view to force
// the view path even when --cvc-file (etc.) armed the cv queue. Without it,
// a 0.5 s gap between two Returns is not enough for a freshly exec'd app to
// take focus (observed live 2026-09-01: the second Return re-launched
// FILE.BIN from the desktop menu).
var inputChords: String?

var inputChordsAfter: String?

// A SECOND gated chord batch (live-term-depth run 03's coalescing fix):
// the hygiene chords need two batches so the RIS can wait on the
// cleared-state snapshot's completion receipt (cvspike: snap n=2). A
// single batch drains the whole sequence in one poll_input pass whenever
// the guest stalls between polls, and a request anchored on `tty: clear`
// then ships the post-RIS frame — the cleared state is gone by the time
// the request can even exist.
var inputChords2: String?

var inputChords2After: String?

// Milestone-eight audit follow-up (issue #117): the keyDown/keyUp spacing
// for --input-chords. Default 3.0 s keeps every existing gate byte-identical;
// the input-depth gate lowers it to ~0.3 s to stress the guest's
// multi-TRB interrupt-IN depth. Only meaningful with --input-chords.
// NOTE: only the VZ-view path honors this; the cv-input transport (via-virtio
// armed) paces at a fixed 0.25 s per stroke regardless (see the delivery-
// transport note in the --input-chords doc above).
var inputChordsDelay: Double = 3.0

// M34 HF5 (issue #739): force --input-chords through the VZVirtualMachineView
// (NSEvents, honors --input-chords-delay) even when the custom-virtio INPUT
// queue is attached. See the delivery-transport note in the --input-chords
// doc above — the trigger is any flag that arms via-virtio (most commonly
// --cvc-file), which switches chords to the cv-input transport paced at
// 0.25 s per stroke and ignoring --input-chords-delay. That is too fast for
// a launch-then-focus handoff (observed live: the desktop composition gate's
// second Return hit the desktop before FILE.BIN's window took focus). The
// view path restores the slow, deterministic pacing for display-equipped
// gates. Requires --display (there is no view to post NSEvents into
// otherwise). Only affects --input-chords, not --input-string.
var chordsViaView = false

// Milestone eight cards U4/U5 (claims 4993/0935): the pointer-synthesis
// seam — "--pointer <x>,<y>[,c][;x2,y2[,c]...]" synthesizes one
// NSEvent.mouseEvent per step (mouseMoved; + mouseDown/Up when the click
// flag is set) into the VZVirtualMachineView after --pointer-after's
// marker, mirroring the I3 keyboard seam (VZ has no programmatic pointer
// API either). Coordinates are GUEST pixels (y from the top); the view's
// bottom-left origin is flipped here. 3 s per step (the report-cadence
// lesson; the chord interval does not apply — pointer reports ride the
// same single-TRB arming).
var pointerScript: String?

var pointerAfter: String?

var pointerVirtioScript: String?

var pointerVirtioAfter: String?

// The pointer delivery route: "window" (sendEvent into the key window —
// observed NOT to reach VZ's pointer translation), "app" (NSApp.postEvent
// into the application queue), or "cg" (real CGEventPost at the HID tap —
// requires Accessibility permission for the terminal). Probes pick the
// route; the gate pins the observed-working one.
var pointerRoute: String = "window"

// Card U4 CG follow-on (claim 3692): `--pointer-request-trust` prompts the
// system to grant Accessibility to the responsible process (the terminal)
// via AXIsProcessTrustedWithOptions. OFF by default — the default VM and
// every existing gate stay byte-identical. The cg route checks trust
// first and reports `PTR-TRUST: untrusted` instead of silently dropping
// the post (the claim-4993 observation).
var pointerRequestTrust = false

let vzRestore = arguments.contains("--vz-restore")

// M70g G3 (#1459): restore beyond same-process. `--vz-restore-save <dir>`
// boots, stores the RAM marker, saves the machine state + the ASIF overlay
// + the marker into <dir> and EXITS; `--vz-restore-load <dir>` builds the
// equivalent configuration in a NEW process, restores from <dir> instead of
// booting, and must recover the marker by a fresh serial query.
var vzRestoreSaveDir: String?

var vzRestoreLoadDir: String?

// ADR 0059 D4 (M131b #2639): on --vz-restore-save, the boot-settle marker
// a checkpoint base boot waits for before pause/save/stop.
var vzRestoreSettle: String?

var vzRestoreWitness: String?

var vzRestoreVerifyCommand: String?

var vzRestoreVerifyOutput: String?

var virtioFSShareDir: String?

// M115a (#2460): Apple's standard VSOCK device (`VZVirtioSocketDevice`) —
// the probe for a restore-safe guest↔host channel. `--vsock` attaches the
// device and arms a host listener on `--vsock-port` (default 1208); under
// `--vz-restore` the card measures whether the device survives
// save/restore and a post-restore guest connect is still accepted.
var vsockEnabled = false

var vsockPort: UInt32 = 1208

var vsockPortSet = false

// M115b (#2461, ADR 0056): the virelai-ctl/1 control channel on the same
// device. `--vsock-ctl` arms a second listener on `--vsock-ctl-port`
// (default 1209) that serves the HMAC-challenged suspend/snapshot wire;
// the credential is the share's `vsock-ctl` SECRETS.TXT row (uid_system).
var vsockCtlEnabled = false

var vsockCtlPort: UInt32 = VSCtl.defaultPort

var vsockCtlPortSet = false

var cpuCount = 2

var cpusExplicit = false

var timeout: TimeInterval = 30

var timeoutExplicit = false

var expectLine = "firmware has agreed to cooperate"

var terminalMarker: String?

var consoleMode = false

// #1066 Stage 0 (issue #1066): a host-side remote console. `--console-tcp
// [host:]port` listens on host:port and bridges the socket to the SAME guest
// serial console the `--console` plumbing uses (client bytes -> the serial
// input attachment; guest output -> every connected client). It turns the
// console session into something you can `nc localhost 2222` (or reach from
// another machine with `0.0.0.0:<port>`). Plaintext, single client at a
// time — the trusted-LAN MVP before any guest crypto/SSH.
var consoleTCPPort: UInt16?

var consoleTCPBind: String = "127.0.0.1"

// M50 TS4 (#1138, ADR 0024 D7): an optional bridge HMAC secret. When set
// (`[host:]port:secret`), the bridge sends a fresh
// `VIRELAIOS-AUTH/1 hmac-sha256 <hex-challenge>` line and requires
// `hex(HMAC-SHA256(secret, domain || 0x00 || challenge))` as the client's
// first line; on mismatch it writes `console-tcp: auth failed` and closes.
// M70g G2 (#1459): `--console-tcp-secret-file <path>` reads the secret's
// first line from a file instead, so it never sits in the process list.
var consoleTCPSecret: String?

var consoleTCPSecretFromFile: String?

var debugInput = false

var markerDumpPath: String?

var nvramConsolePath: String?

var scriptPath: String?

var scriptAfter: String?

var scriptExpect: String?

// Claim 4912: after --script-expect first matches, hold the VM this many
// seconds (default 1.5 — one 1 Hz guest timer tick plus delivery/tee
// margin) before teardown, so the kernel's async exit-report tail (the
// `tasks/procs ... exited status=N` reap lines, drained from the shell
// idle loop on the timer tick) reaches the serial log. 0 restores the
// legacy stop-on-first-match behavior.
var scriptExpectTail: Double = 1.5

// Claim 4613: a second scripted phase. The primary --script is forwarded
// in ONE burst (claim 6684), so a scripted command that must land AFTER a
// background program exits and is reaped (the long-lived gate's re-exec
// into the freed pool slot) cannot be in the same burst: --script2 is
// forwarded once after its own serial marker (the reap line) instead.
var script2Path: String?

var script2After: String?

// Card N9 (claim 9489): the claim-6684 settle before forwarding script2
// / script3 becomes configurable (default 0.5 — every existing gate is
// unchanged), so a lease-lifecycle gate can wait past T1/T2/expiry
// deterministically.
var script2Delay: Double = 0.5

var script3Delay: Double = 0.5

// Card 3c (claim 7786): a THIRD scripted phase. The primary --script is
// forwarded in ONE burst (claim 6684) and --script2 handles the next
// phase (claim 4613); --script3 covers the post-reap snapshot that must
// land after a background process is killed AND reaped (the kill gate's
// procs/pages/re-exec read) — forwarded once after its own serial marker
// via the identical machinery.
var script3Path: String?

var script3After: String?

var customVirtioEnabled = false

// Claim 3141 (issue #523 item 3): `--cvc-echo` runs the HOST-initiated push
// echo over a third virtqueue — implies --custom-virtio. The host app
// enqueues the request into the guest's pre-armed receive buffer, the guest
// replies on the same queue, and the delegate verifies byte-exactly.
var cvcEchoEnabled = false

// Claim 9588 (issue #523 item 3): `--via-virtio` injects keys through the
// custom-virtio INPUT queue (queue 3) as HID-shaped 16-byte messages — no
// NSEvent synthesis, no view, no window. Implies the full four-queue device
// (custom virtio + cvc-echo + input).
var viaVirtioEnabled = false

// Claim 0680 (issue #523 item 3 capstone): `--cvc-snap` adds the FIFTH
// virtqueue — the framebuffer-snapshot channel. The guest streams its
// composed scanout as tagged binary messages over queue 4; the runner
// reassembles them into a raw BGRX file, replacing ScreenCaptureKit
// scraping (and its Screen Recording TCC dependency) for gates that opt in.
// Implies --via-virtio (five contiguous queues).
var cvcSnapEnabled = false

// M34 HF1+HF2 (issues #735/#736): `--cvc-file <host-dir>` attaches the
// SIXTH virtqueue — the host file channel. The guest userland filesystem
// is a macOS folder served over queue 5 with plain FileManager calls
// (wire format in Sources/VFWire/VFWire.swift). The deepest flag implies
// the full five-queue shape below it (unchanged rule).
var cvcFileShareDir: String?

var cvcFileEnabled: Bool { cvcFileShareDir != nil }

// M108b (#2325, ADR 0051): the host-integration lanes are signed VHI1
// record files under <share>/HOSTINT/ — modifiers on --cvc-file, so the
// share is the transport. `--host-clipboard` arms the clipboard lane both
// directions (seat-emitted CLIP_OUT records are mirrored to the
// pasteboard; pasteboard changeCount bumps become CLIP_IN records the
// seat applies). OFF by default: no lane flag means no HOSTINT/ is ever
// created and no pasteboard is ever read (D7 byte-silence).
// M139a (#2779): the five host-integration endpoints are per-SESSION
// state on the primary VMSession — these names keep their hundreds of
// single-VM call sites but the storage lives on the session object the
// registry owns (a secondary session's endpoints are its own fields).
var hostClipboardFlag = false

// `--host-pasteboard <name>` retargets the lane to NSPasteboard(name:) —
// the gate/test hook so a run never clobbers the operator's .general
// pasteboard (ADR 0051 D3's pasteboard-identity rule).
var hostPasteboardName: String?

// `--host-clip-in <text>` + `--host-clip-in-after <marker>`: the
// marker-timed test hook that performs a real pasteboard write once the
// marker appears — the changeCount path then carries it in like any host
// copy (the --host-write-after precedent).
var hostClipInText: String?

var hostClipInAfter: String?

var hostClipInFired = false

// M124c: a typed inject — file bytes + a negotiated mime.
var hostClipInFile: String?

var hostClipInMime: String?

var hostClipboard: HostClipboard? {
    get { primarySession.clipboard }
    set { primarySession.clipboard = newValue }
}

// M108c (#2326, ADR 0051 D4): `--host-drop` arms the file-drop lane — the
// display view registers .fileURL, dropped files land in <share>/INBOX/
// (≤64 MiB/file, ≤16/gesture) and each gets a signed DROP notice the seat
// toasts. `--host-drop-file`/`--host-drop-after` are the marker-timed test
// hook: feed a host path through the SAME handler the NSView drop calls
// (repeatable — a multi-file gesture is one call).
var hostDropFlag = false

var hostDropFiles: [String] = []

var hostDropAfter: String?

var hostDropFired = false

var hostDrop: HostDrop? {
    get { primarySession.drop }
    set { primarySession.drop = newValue }
}

// M108d (#2328, ADR 0051 D5): `--host-notify` arms the notify-out lane —
// seat toasts arrive as signed NOTIFY_OUT records and post as macOS
// banners under the session bundle (bare runner: `no_bundle` drops).
var hostNotifyFlag = false

var hostNotify: HostNotify? {
    get { primarySession.notify }
    set { primarySession.notify = newValue }
}

// M127d (#2602, ADR 0051 V2-D5): `--host-outbox` arms the export lane —
// the seat signs OUTBOX records for REQ-marked payloads under
// <share>/OUTBOX/ and the runner moves them to `--host-outbox-dir`
// (default ~/Downloads; the flag is the gate redirect — a class-B run
// never touches a real Downloads). No display precondition: exports are
// not a window gesture.
var hostOutboxFlag = false

var hostOutboxDir: String?

var hostOutbox: HostOutbox? {
    get { primarySession.outbox }
    set { primarySession.outbox = newValue }
}

// M186d (#3181, ADR 0051 V2-D8): `--host-speech` arms the speech lane —
// SAY intent markers under <share>/SAY/ become signed SPEAK records the
// runner verifies and hands to NSSpeechSynthesizer (queued,
// interruptible, never blocking the VM). No display precondition.
var hostSpeechFlag = false

var hostSpeech: HostSpeech? {
    get { primarySession.speech }
    set { primarySession.speech = newValue }
}

// M202c (#3386, ADR 0051): `--host-print` arms the print lane — PREQ
// intent markers under <share>/PRINT/ become signed PRINT records the
// runner verifies and hands to NSPrintPanel (bundled — the human's click
// is the per-job consent) or moves to `--host-print-dir` (bare runner:
// the consent-free degrade, receipt `saved` — a saved file, never a
// claimed print).
var hostPrintFlag = false

var hostPrintDir: String?

var hostPrint: HostPrint? {
    get { primarySession.printLane }
    set { primarySession.printLane = newValue }
}

// M114a (#2454, index #2447): `--host-cmd-after <marker>:<verb>` — the
// headless mirror of the HostCommand bus every menu item dispatches on
// (index thesis 1: AppKit is never driven by synthesized clicks; the bus
// is the testable seam). Repeatable; each armed trigger fires once on
// its serial marker and the dispatch emits one `host-ui:` receipt.
var hostCmdTriggers: [HostCommandTrigger] = []

// M114b (#2455): `--host-capture-keys` arms the machine view's
// capturesSystemKeys at startup — by dispatching the capture-keys verb
// on the bus once setupDisplayWindow has built the view, so the receipt
// is identical to ⌥⌘K / the toolbar pill / a --host-cmd-after trigger.
var hostCaptureKeys = false

// M114d (#2457, index #2447): the inspector drawer's model — ONE
// instance for the run. The serial polls feed it the growing log; the
// `inspector`/`send-serial` verbs act on it; a session window's drawer
// renders the same object, so a headless --host-cmd-after run exercises
// the identical seam the open drawer does.
let inspector = HostInspector()

// `--host-serial-send <line>`: arms the drawer's input line headlessly —
// the send-serial verb (a --host-cmd-after trigger, or the drawer's own
// Send) writes it into the console pipe exactly as the field would.
var hostSerialSend: String?

// The shared lane session (ADR 0051 D2): ONE credential, ONE epoch, ONE
// ARMED record naming every live lane — the seat adopts (epoch, mask), so
// per-lane epochs would refuse each other stale_epoch forever.
var hostIntSession: HostIntSession? {
    get { primarySession.hostInt }
    set { primarySession.hostInt = newValue }
}

// Claim 0680: `--cvc-console-file <path>` captures every queue-1 guest log
// line to a structured file (the structured console). When the file is set,
// the host also answers the guest's "cvconsole-ready" line with a kind-3
// control message arming the guest's console tee, so kernel console output
// rides queue 1 alongside serial. Requires --custom-virtio (queue 1).
var cvcConsoleFilePath: String?

// Claim 0680: `--snapshot-after <marker>` fires one kind-4 snapshot request
// when <marker> appears in the serial stream (repeatable; each instance
// writes its own numbered .raw). `--snapshot-out <base>` overrides the
// default output base (<screen path>-snap).
struct SnapshotTrigger {
    let marker: String
    let outPath: String
    var fired = false
}

var snapshotAfterMarkers: [String] = []

var snapshotOutBase: String?

// Issue #1788: a kind-4 snapshot is a 3.5 MiB round trip, so a trigger
// marker printed microseconds before the --script-expect match leaves the
// stream mid-flight when the teardown would otherwise fire. Hold the VM
// this long (bounded) while a stream is in flight. 0 = stop immediately.
var snapshotDrainTimeout: Double = 20

var snapshotTriggers: [SnapshotTrigger] = []

// M102d3 (issue #2168): the opt-in private 3D render device. Zero-value
// options mean the VM configuration is byte-identical to before; the
// VMGPU3D leaf owns the parsing vocabulary, the named rejections and the
// device/backend assembly.
var gpu3dOptions = GPU3DRunnerConfiguration.Options()

// Issue #523 item 2 (claim 6637): run isolation.
// `--overlay-base <path>`: open <path> READ-ONLY as the base of a
// DiskImageKit stacked image and append a fresh ASIF overlay layer (created
// in a private temp dir, removed at exit). Every run boots a pristine disk
// by construction; guest writes land in the overlay and are discarded. The
// positional <disk-image> argument is ignored in this mode.
// `--vars <path>`: per-run EFI variable store (default artifacts/efi-vars.bin,
// kept for back-compat with every existing invocation).
var overlayBasePath: String?

var varsOverridePath: String?

// M113b (#2418, ADR 0054 Part I): the opt-in verified-boot flag family.
// `--secure-boot` builds a FRESH per-run variable store, enables UEFI
// Secure Boot with a custom Platform Key (`--secure-boot-pk`, a DER
// X.509 certificate), and enrolls the db as SHA-256 entries — either the
// PE/COFF Authenticode digest of each `--secure-boot-db-efi <PE>` image
// (the convention the firmware enforces — the M113a probe pinned it) or
// a raw 64-hex digest from `--secure-boot-db-hash`. Flags OFF is
// byte-identical: the store is created/opened exactly as before and the
// boot default does not move.
var secureBootEnabled = false

var secureBootPKPath: String?

var secureBootDbEfiPaths: [String] = []

var secureBootDbHashes: [String] = []

var idx = arguments.count > 1 && arguments[1].hasPrefix("--") ? 1 : 2

var documentProfileURL: URL? = nil

var activeProfile: VMProfile? = nil

var resumeStateURL: URL? = nil

// Console sessions run until the VM stops or the user ends the session,
// unless an explicit --timeout was requested. Script mode is a non-
// interactive variant of the duplex console plumbing (claim 6684).
let consoleTimeout: TimeInterval = (consoleMode && !timeoutExplicit) ? 0 : timeout

let scriptMode = scriptPath != nil

// Issue #523 item 2 (claim 6637): when --overlay-base is used, the throwaway
// ASIF overlay layer lives in this directory; it is removed at exit so
// concurrent runs never share (or leave behind) writable state. Set
// VIRELAI_KEEP_OVERLAY=1 to keep it for post-mortem.
var overlayCleanupPath: String?

let osVersion = ProcessInfo.processInfo.operatingSystemVersion

let vzWarmLeg = vzRestore && vzRestoreLoadDir != nil && scriptPath != nil

let overlayMode = overlayBasePath != nil

let diskURL = URL(fileURLWithPath: diskImagePath)

let artifactsDir = URL(fileURLWithPath: "artifacts")
