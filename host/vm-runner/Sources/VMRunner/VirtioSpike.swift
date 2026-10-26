// macOS 27 custom-virtio spike (-DSPIKE only).

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


// ---------------------------------------------------------------------------
// macOS 27 spike (capability-audit step 3): one default-off custom virtio
// device, so the guest's PCI discovery can observe it on a real VZ boot.
//
// Compiled only with -DSPIKE (the `zig build spike-virtio` step): the CI
// toolchain's macOS 26 SDK does not declare VZCustomVirtioDevice at all, so
// this whole section must be absent from the base class-A build.
//
// Device identity (guest-facing): virtio-pci VID 0x1af4, DID = 0x1040 | 0x42
// = 0x1082 — distinct from the console's DID 0x1043 (claim 0013) and blk's
// 0x1042 (claim 6420). PCI class 0x00 / subclass 0x00, one virtqueue.
//
// SDK reality (Xcode 27 beta 4, macOS 27.0): the framework exposes NO
// host-triggered guest-interrupt API — the WWDC26 "trigger an interrupt on
// the device" claim has no public symbol; the word "interrupt" appears in
// exactly one framework header (a POSIX EINTR param). The only host->guest
// signaling is the framework-internal used-buffer notification when queue
// elements are returned via VZVirtioQueueElement.returnToQueue. This spike
// is discovery + transport evidence only; the DRIVER_OK / queue-notification
// logs below give the audit its step-4 hooks for free.
// ---------------------------------------------------------------------------

#if SPIKE

@available(macOS 27.0, *)
enum CustomVirtioSpike {
    /// Virtio device ID; PCI DID presented to the guest = 0x1040 | deviceID.
    static let deviceID: UInt16 = 0x42
    static let pciClass: UInt8 = 0x00
    static let pciSubclass: UInt8 = 0x00
    // Two queues (claims 4374/4837): queue 0 = the exchange/transport
    // queue, queue 1 = the guest log transport. The claim-3141 push echo
    // (--cvc-echo) adds queue 2 — the queue COUNT is the capability signal;
    // the guest driver probes its size through the common config. The
    // claim-9588 input channel (--via-virtio) adds queue 3, and the
    // claim-0680 snapshot channel (--cvc-snap) adds queue 4 (virtqueues are
    // contiguous, so each deeper flag implies the full shape below it).
    static let queueCount: UInt16 = cvcFileEnabled ? 6 : (cvcSnapEnabled ? 5 : (viaVirtioEnabled ? 4 : (cvcEchoEnabled ? 3 : 2)))

    // ---- Claim 9588 INPUT channel (queue 3): HID-shaped input messages.
    // Wire format is normative in docs/hardware-contract.md:
    //   [kind u8][flags u8][len u16le][12-byte payload], fixed 16 bytes;
    //   kind 1 = raw 8-byte HID keyboard boot report [mods, 0, k0..k5];
    //   kind 2 = raw 5-byte absolute-pointer report [buttons, x_lo, x_hi,
    //   y_lo, y_hi] (claim 9367), HID absolute 0..32767 coords.
    static let inputMsgLen = 16
    static let inputKindKeyboard: UInt8 = 1
    static let inputKindPointer: UInt8 = 2
    static let inputKindConsoleCtrl: UInt8 = 3
    static let inputKindSnapshotReq: UInt8 = 4
    static let inputKeyboardReportLen = 8
    static let inputPointerReportLen = 5
    // HID boot-protocol modifier bits (the guest's hid_modifiers_to_flags
    // reads exactly these: bit0 LCtrl, bit1 LShift, bit2 LAlt, bit3 LCmd).
    static let hidModCtrl: UInt8 = 0x01
    static let hidModShift: UInt8 = 0x02
    static let hidModAlt: UInt8 = 0x04 // USB HID keyboard modifier bit 2 = LAlt

    /// One injected key report: (modifier byte, usage ID, down or up).
    struct HidStroke {
        let mods: UInt8
        let usage: UInt8
        let isDown: Bool
        var label: String { "\(isDown ? "down" : "up") usage=0x\(String(usage, radix: 16)) mods=0x\(String(mods, radix: 16))" }
    }

    /// Claim 9367: one injected pointer report — absolute logical coords
    /// (HID 0..32767 convention; the guest maps them onto the framebuffer)
    /// plus the button byte (bit0 = button 1, bit1 = button 2/right).
    /// M32 WMS5 (issue #625): the step grammar gains `d` (down, held) and
    /// `u` (up) so a DRAG can be expressed: move to the title bar, down,
    /// move while held, up — the established `c` click still expands to
    /// down+up at one point (zero regression for the claim 9367 gates).
    /// M124d (#2583): `rc` adds the right-button click for GOFILES'
    /// context-menu leg (the guest already maps buttons bit1 to SGR
    /// button 2 — only the injection grammar lacked a way to ask for it).
    /// M124e (#2584): `mc` adds the middle-button click (buttons bit2,
    /// 0x04) — the primary-selection paste gesture's injection.
    struct PtrStep {
        enum Mode {
            case move, click, down, up, rightClick, middleClick
        }

        let x: UInt16
        let y: UInt16
        let mode: Mode
        var label: String { "mode=\(mode) x=\(x) y=\(y)" }
    }

    /// Map one character of the `macKey` vocabulary to its HID usage ID +
    /// shift requirement (the guest keymap's inverse — a–z, 0–9, punctuation,
    /// space, `\n` = Enter). nil = outside the vocabulary (fail honestly).
    static func hidUsage(for ch: Character) -> (usage: UInt8, shift: Bool)? {
        switch ch {
        case "a": return (0x04, false); case "b": return (0x05, false)
        case "c": return (0x06, false); case "d": return (0x07, false)
        case "e": return (0x08, false); case "f": return (0x09, false)
        case "g": return (0x0A, false); case "h": return (0x0B, false)
        case "i": return (0x0C, false); case "j": return (0x0D, false)
        case "k": return (0x0E, false); case "l": return (0x0F, false)
        case "m": return (0x10, false); case "n": return (0x11, false)
        case "o": return (0x12, false); case "p": return (0x13, false)
        case "q": return (0x14, false); case "r": return (0x15, false)
        case "s": return (0x16, false); case "t": return (0x17, false)
        case "u": return (0x18, false); case "v": return (0x19, false)
        case "w": return (0x1A, false); case "x": return (0x1B, false)
        case "y": return (0x1C, false); case "z": return (0x1D, false)
        case "A": return (0x04, true);  case "B": return (0x05, true)
        case "C": return (0x06, true);  case "D": return (0x07, true)
        case "E": return (0x08, true);  case "F": return (0x09, true)
        case "G": return (0x0A, true);  case "H": return (0x0B, true)
        case "I": return (0x0C, true);  case "J": return (0x0D, true)
        case "K": return (0x0E, true);  case "L": return (0x0F, true)
        case "M": return (0x10, true);  case "N": return (0x11, true)
        case "O": return (0x12, true);  case "P": return (0x13, true)
        case "Q": return (0x14, true);  case "R": return (0x15, true)
        case "S": return (0x16, true);  case "T": return (0x17, true)
        case "U": return (0x18, true);  case "V": return (0x19, true)
        case "W": return (0x1A, true);  case "X": return (0x1B, true)
        case "Y": return (0x1C, true);  case "Z": return (0x1D, true)
        case "1": return (0x1E, false); case "2": return (0x1F, false)
        case "3": return (0x20, false); case "4": return (0x21, false)
        case "5": return (0x22, false); case "6": return (0x23, false)
        case "7": return (0x24, false); case "8": return (0x25, false)
        case "9": return (0x26, false); case "0": return (0x27, false)
        case "\n", "\r": return (0x28, false) // Enter / Return
        case " ": return (0x2C, false)		case "_": return (0x2D, true)
		case "+": return (0x2E, true)
		case ":": return (0x33, true)
		case "-": return (0x2D, false); case "=": return (0x2E, false)
        case "[": return (0x2F, false); case "]": return (0x30, false)
        case "\\": return (0x31, false)
        case ";": return (0x33, false); case "'": return (0x34, false)
        case "`": return (0x35, false)
        case ",": return (0x36, false); case ".": return (0x37, false)
        case "/": return (0x38, false); case "?": return (0x38, true)
        default: return nil
        }
    }

    /// Map one `macChord` token to (mods, usage). Same accepted vocabulary:
    /// named nav tokens + ctrl-a..ctrl-z + single printable chars. The nav
    /// usages are the guest keymap's editing cluster (arrows CSI x, Home/
    /// End/PgUp/PgDn; `delete` = forward-delete 0x4C → the guest's CSI 3~,
    /// matching the macOS-0x75 semantics macChord carries).
    static func hidChord(_ token: String) -> (mods: UInt8, usage: UInt8)? {        switch token {
        case "return": return (0, 0x28)
        case "space": return (0, 0x2C)
        case "comma": return (0, 0x36) // K12/K16: the CSV token separator cannot carry a literal comma
        case "ctrl-comma": return (hidModCtrl, 0x36)
        case "ctrl-space": return (hidModCtrl, 0x2C) // M37 DQ1: the God Menu summon chord
        case "tab": return (0, 0x2B)
        // WMS6 Gate A (issue #626): a real Alt+Tab chord over the HID channel
        // (LAlt modifier + Tab usage) — the WM's alt-tab policy hook.
        case "alt-tab": return (hidModAlt, 0x2B)
        case "alt-shift-tab": return (hidModAlt | hidModShift, 0x2B)
        // M63r (#1424): Ctrl+Tab / Ctrl+Shift+Tab were missing — the
        // ctrl-<letter> / ctrl-shift-<letter> patterns are letters only.
        case "ctrl-tab": return (hidModCtrl, 0x2B)
        case "ctrl-shift-tab": return (hidModCtrl | hidModShift, 0x2B)
        case "escape": return (0, 0x29)
        case "insert": return (0, 0x70)
        // M124e (#2584): the seat's primary-paste chord — the canonical
        // X11 spelling, free at every scope (ctrl+shift+v is split-cycle
        // at seat scope and tty clipboard paste at kernel-terminal scope).
        case "shift-insert": return (hidModShift, 0x70)
        case "f1": return (0, 0x3A)
        case "f2": return (0, 0x3B)
        case "f3": return (0, 0x3C)
        case "f4": return (0, 0x3D)
        case "f5": return (0, 0x3F)
        case "f6": return (0, 0x40)
        case "f7": return (0, 0x41)
        case "f8": return (0, 0x42)
        case "f9": return (0, 0x42)
        // M137d (#2774): f10/f11 were off-by-one (0x44/0x46) — USB HID
        // usages run F1=0x3A .. F12=0x45, so F10 is 0x43 (what the guest
        // keymap and appkit's Named() decode) and F11 is 0x44.
        case "f10": return (0, 0x43)
        case "f11": return (0, 0x44)
        case "f12": return (0, 0x45)
        case "up": return (0, 0x52)
        case "down": return (0, 0x51)
        case "left": return (0, 0x50)
        case "right": return (0, 0x4F)
        case "home": return (0, 0x4A)
        case "end": return (0, 0x4D)
        case "delete": return (0, 0x4C)
        // M68a (#1449): `backspace` was missing from BOTH chord tables, so
        // any spec that edits a typed line (type, delete a char, type over
        // it) failed the whole sequence with "no HID mapping for chord
        // 'backspace'" and injected NOTHING — the go-sh gate then sat at its
        // prompt for its full 240 s deadline. Usage 0x2A is the guest
        // keymap's Backspace (kernel/src/input.zig maps it to byte 0x08),
        // so this behaves exactly like the physical key.
        case "backspace": return (0, 0x2A)
        case "pageup": return (0, 0x4B)
        case "pagedown": return (0, 0x4E)
        // M121b (#2565): the seat's tab-move chords — the ctrl-shift-<x>
        // pattern is letters only, so the editing-cluster pair is named.
        case "ctrl-shift-pageup": return (hidModCtrl | hidModShift, 0x4B)
        case "ctrl-shift-pagedown": return (hidModCtrl | hidModShift, 0x4E)
        // M137a/c (#2771/#2773, ADR 0007 R4/R5): pane-focus arrows and
        // space-switch paging — named outright for the same reason.
        case "ctrl-alt-up": return (hidModCtrl | hidModAlt, 0x52)
        case "ctrl-alt-down": return (hidModCtrl | hidModAlt, 0x51)
        case "ctrl-alt-left": return (hidModCtrl | hidModAlt, 0x50)
        case "ctrl-alt-right": return (hidModCtrl | hidModAlt, 0x4F)
        case "ctrl-alt-pageup": return (hidModCtrl | hidModAlt, 0x4B)
        case "ctrl-alt-pagedown": return (hidModCtrl | hidModAlt, 0x4E)
        // WM2 mission-control overview (Self-hosting Lane 1, issue #707
        // card 2, port of the #971 exploration): the overview hotkey —
        // LCtrl modifier + USB HID F12 usage (0x45). The guest kind-21
        // stream carries raw HID usages, so WND.BIN matches 0x45 for the
        // overview chord.
        case "ctrl-f12": return (hidModCtrl, 0x45)
        // M80k (#1727): the terminal's full-RIS chord. A THIRD modifier
        // is not expressible in the ctrl-/ctrl-shift- letter patterns
        // (they are letters only), so the triple-modifier chord is named
        // outright — usage 0x15 = 'r', the same key as ctrl-shift-r's
        // soft reset, so the guest can tell them apart by the alt bit.
        case "ctrl-shift-alt-r": return (hidModCtrl | hidModShift | hidModAlt, 0x15)
        default: break
        }
        if token.hasPrefix("ctrl-shift-"), token.count == 12 {
            let letter = token[token.index(token.startIndex, offsetBy: 11)]
            if let (usage, shift) = hidUsage(for: letter), !shift {
                return (hidModCtrl | hidModShift, usage)
            }
            return nil
        }
        if token.hasPrefix("ctrl-"), token.count == 6 {
            let letter = token[token.index(token.startIndex, offsetBy: 5)]
            if let (usage, shift) = hidUsage(for: letter), !shift {
                return (hidModCtrl, usage)
            }
            return nil
        }
        if token.count == 1, let (usage, shift) = hidUsage(for: token[token.startIndex]) {
            return (shift ? hidModShift : 0, usage)
        }
        return nil
    }

    /// Reverse of the free function macKey(for:): macOS virtual keycode →
    /// character, so `--input-key`'s numeric keycode can ride the HID
    /// channel (mirrors macKey's pairs exactly; nil = unmapped, loud fail).
    static func charFromMacCode(_ code: UInt16) -> Character? {
        switch code {
        case 0x00: return "a"; case 0x0B: return "b"; case 0x08: return "c"
        case 0x02: return "d"; case 0x0E: return "e"; case 0x03: return "f"
        case 0x05: return "g"; case 0x04: return "h"; case 0x22: return "i"
        case 0x26: return "j"; case 0x28: return "k"; case 0x25: return "l"
        case 0x2E: return "m"; case 0x2D: return "n"; case 0x1F: return "o"
        case 0x23: return "p"; case 0x0C: return "q"; case 0x0F: return "r"
        case 0x01: return "s"; case 0x11: return "t"; case 0x20: return "u"
        case 0x09: return "v"; case 0x0D: return "w"; case 0x07: return "x"
        case 0x10: return "y"; case 0x06: return "z"
        case 0x12: return "1"; case 0x13: return "2"; case 0x14: return "3"
        case 0x15: return "4"; case 0x17: return "5"; case 0x16: return "6"
        case 0x1A: return "7"; case 0x1C: return "8"; case 0x19: return "9"
        case 0x1D: return "0"
        case 0x31: return " "; case 0x24: return "\n"
        case 0x2F: return "."; case 0x2B: return ","; case 0x2C: return "/"
        case 0x1B: return "-"; case 0x18: return "="; case 0x21: return "["
        case 0x1E: return "]"; case 0x2A: return "\\"; case 0x29: return ";"
        case 0x27: return "'"; case 0x32: return "`"
        default: return nil
        }
    }

    /// Build the fixed 16-byte input message for one stroke (contract shape:
    /// [kind][flags=0][len LE][payload]; kind 1 payload = the raw report).
    static func inputMessage(mods: UInt8, usage: UInt8, isDown: Bool) -> Data {
        var msg = Data(repeating: 0, count: inputMsgLen)
        msg[0] = inputKindKeyboard
        msg[2] = UInt8(inputKeyboardReportLen & 0xff)
        if isDown {
            msg[4] = mods       // byte 0 of the HID boot report: modifiers
            msg[6] = usage      // byte 2: first keycode slot (byte 1 reserved)
        }
        // keyUp = the all-zero report — the guest's held-set transition
        // logic emits KEY_UP exactly like a physical release.
        return msg
    }

    /// Claim 9367: build the fixed 16-byte kind-2 pointer message — payload
    /// [buttons, x_lo, x_hi, y_lo, y_hi], the exact shape the guest's
    /// decode_pointer_report reads from an XHCI pointer report.
    static func inputPointerMessage(x: UInt16, y: UInt16, buttons: UInt8) -> Data {
        var msg = Data(repeating: 0, count: inputMsgLen)
        msg[0] = inputKindPointer
        msg[2] = UInt8(inputPointerReportLen & 0xff)
        msg[4] = buttons
        msg[5] = UInt8(x & 0xff)
        msg[6] = UInt8((x >> 8) & 0xff)
        msg[7] = UInt8(y & 0xff)
        msg[8] = UInt8((y >> 8) & 0xff)
        return msg
    }

    /// Enqueue one pre-built input message into a pre-armed receive buffer
    /// on queue 3 and return it (used ring advance + SPI — the only
    /// host→guest data path). Runs ON the device delegate queue so element
    /// access stays single-threaded with the callbacks. Returns false when
    /// the pool is momentarily empty (the caller retries — ordering
    /// preserved); shape or write failures report loudly on stderr.
    @discardableResult
    static func enqueueMessageNow(_ msg: Data, label: String) -> Bool {
        guard let device, let queue = device.queue(at: 3) else {
            FileHandle.standardError.write(Data("ERROR: CUSTOM-VIRTIO-INPUT: queue 3 unavailable (device \(device == nil ? "nil" : "present"))\n".utf8))
            return true // unrecoverable: do not retry-spin the sequence
        }
        guard let element = queue.nextElement() else {
            return false // pool empty: retry THIS message, later ones wait
        }
        do {
            try element.write(msg)
        } catch {
            FileHandle.standardError.write(Data("ERROR: CUSTOM-VIRTIO-INPUT: write FAILED (\(label)): \(error)\n".utf8))
            element.returnToQueue()
            return true
        }
        element.returnToQueue()
        print("CUSTOM-VIRTIO-INPUT: enqueued \(label) (\(msg.count)-byte message)")
        return true
    }

    /// Keyboard convenience: build + enqueue one stroke's message.
    static func enqueueInputNow(_ stroke: HidStroke, sequenceTag: String) -> Bool {
        let msg = inputMessage(mods: stroke.mods, usage: stroke.usage, isDown: stroke.isDown)
        return enqueueMessageNow(msg, label: "\(sequenceTag) \(stroke.label)")
    }

    /// Deliver a stroke sequence in STRICT order on the device queue: the
    /// next stroke is scheduled only after the previous one was accepted,
    /// so a pool-empty retry delays the rest of the sequence instead of
    /// reordering it (observed live, claim 9588: a fixed-schedule burst hit
    /// a momentarily empty pool and typed 'inpu⏎t' — the guest executed two
    /// garbled commands). 0.25 s pacing; exhaustion fails LOUDLY, never
    /// drops silently.
    static func injectStrokes(_ strokes: [HidStroke], sequenceTag: String) {
        deviceQueue.async {
            deliverStrokes(strokes, sequenceTag: sequenceTag, index: 0, attempts: 0)
        }
    }

    private static func deliverStrokes(_ strokes: [HidStroke], sequenceTag: String, index: Int, attempts: Int) {
        if index >= strokes.count {
            print("CUSTOM-VIRTIO-INPUT: sequence complete tag=\(sequenceTag) n=\(strokes.count) ok=true")
            return
        }
        let stroke = strokes[index]
        if enqueueInputNow(stroke, sequenceTag: sequenceTag) {
            deviceQueue.asyncAfter(deadline: .now() + 0.25) {
                deliverStrokes(strokes, sequenceTag: sequenceTag, index: index + 1, attempts: 0)
            }
        } else if attempts < 40 {
            deviceQueue.asyncAfter(deadline: .now() + 0.25) {
                deliverStrokes(strokes, sequenceTag: sequenceTag, index: index, attempts: attempts + 1)
            }
        } else {
            FileHandle.standardError.write(Data("ERROR: CUSTOM-VIRTIO-INPUT: rx pool empty after 40 retries (\(sequenceTag) \(stroke.label)) — sequence ABORTED at \(index)/\(strokes.count)\n".utf8))
        }
    }

    /// Claim 9367: deliver a POINTER step sequence in STRICT order on the
    /// device queue — same shape as the stroke ladder (0.25 s pacing, pool-
    /// empty retry delays the rest instead of reordering, exhaustion fails
    /// LOUDLY). Each move is one kind-2 message; a click step emits
    /// down then up at the same coords so the guest's button edge logic
    /// sees exactly what a physical click produces.
    /// Claim 9367: deliver a POINTER step sequence in STRICT order on the
    /// device queue — same shape as the stroke ladder (pool-empty retry
    /// delays the rest instead of reordering, exhaustion fails LOUDLY).
    /// Each move is one kind-2 message; a click step emits down then up at
    /// the same coords so the guest's button edge logic sees exactly what a
    /// physical click produces. PACING: pointer presses are edge-detected
    /// by driving_award.pointer_tick, which runs once per shell-idle pass —
    /// at the Road Pops present cadence (~1.5–2 s, observed claim-time),
    /// NOT per message. Spacing below one idle pass collapses press+release
    /// into a single tick and the click never fires (observed live, this
    /// claim), so pointer sequences pace at `pacingSeconds` (default 2.5 s,
    /// the same order as the synthesized chord paths' 2–3 s).
    static func injectPointerSteps(_ steps: [PtrStep], sequenceTag: String, pacingSeconds: Double = 2.5) {
        deviceQueue.async {
            var msgs: [(Data, String)] = []
            var held = false
            for s in steps {
                switch s.mode {
                case .move:
                    // A move between down and up carries the held button
                    // (the WMS5 drag choreography); a leading move is a bare
                    // hover (buttons 0) exactly like the claim 9367 gates.
                    let buttons: UInt8 = held ? 0x01 : 0
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: buttons), "\(sequenceTag) move buttons=0x\(String(buttons, radix: 16)) x=\(s.x) y=\(s.y)"))
                case .click:
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: 0), "\(sequenceTag) move buttons=0x0 x=\(s.x) y=\(s.y)"))
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: 0x01), "\(sequenceTag) down buttons=0x1 x=\(s.x) y=\(s.y)"))
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: 0), "\(sequenceTag) up buttons=0x0 x=\(s.x) y=\(s.y)"))
                case .rightClick:
                    // Same click choreography, button byte bit1: the
                    // guest's right-button edge logic sees a physical
                    // right-click (driving_award maps it to SGR button 2).
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: 0), "\(sequenceTag) move buttons=0x0 x=\(s.x) y=\(s.y)"))
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: 0x02), "\(sequenceTag) down buttons=0x2 x=\(s.x) y=\(s.y)"))
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: 0), "\(sequenceTag) up buttons=0x0 x=\(s.x) y=\(s.y)"))
                case .middleClick:
                    // M124e: same click choreography, button byte bit2 —
                    // the guest sees a physical middle-click (the seat's
                    // hidBtnMiddle edge), which is the primary-paste
                    // gesture when the target's manifest row opts in.
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: 0), "\(sequenceTag) move buttons=0x0 x=\(s.x) y=\(s.y)"))
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: 0x04), "\(sequenceTag) down buttons=0x4 x=\(s.x) y=\(s.y)"))
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: 0), "\(sequenceTag) up buttons=0x0 x=\(s.x) y=\(s.y)"))
                case .down:
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: 0), "\(sequenceTag) move buttons=0x0 x=\(s.x) y=\(s.y)"))
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: 0x01), "\(sequenceTag) down buttons=0x1 x=\(s.x) y=\(s.y)"))
                    held = true
                case .up:
                    // Land at the target WHILE held (the guest issues its
                    // final SET_WINDOW there), then release — a physical
                    // mouse's up lands at the drag's final position.
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: 0x01), "\(sequenceTag) move buttons=0x1 x=\(s.x) y=\(s.y)"))
                    msgs.append((inputPointerMessage(x: s.x, y: s.y, buttons: 0), "\(sequenceTag) up buttons=0x0 x=\(s.x) y=\(s.y)"))
                    held = false
                }
            }
            deliverMessages(msgs, sequenceTag: sequenceTag, index: 0, attempts: 0, pacingSeconds: pacingSeconds)
        }
    }

    private static func deliverMessages(_ msgs: [(Data, String)], sequenceTag: String, index: Int, attempts: Int, pacingSeconds: Double = 0.25) {
        if index >= msgs.count {
            print("CUSTOM-VIRTIO-INPUT: sequence complete tag=\(sequenceTag) n=\(msgs.count) ok=true")
            return
        }
        let (msg, label) = msgs[index]
        if enqueueMessageNow(msg, label: label) {
            deviceQueue.asyncAfter(deadline: .now() + pacingSeconds) {
                deliverMessages(msgs, sequenceTag: sequenceTag, index: index + 1, attempts: 0, pacingSeconds: pacingSeconds)
            }
        } else if attempts < 40 {
            deviceQueue.asyncAfter(deadline: .now() + pacingSeconds) {
                deliverMessages(msgs, sequenceTag: sequenceTag, index: index, attempts: attempts + 1, pacingSeconds: pacingSeconds)
            }
        } else {
            FileHandle.standardError.write(Data("ERROR: CUSTOM-VIRTIO-INPUT: rx pool empty after 40 retries (\(label)) — sequence ABORTED at \(index)/\(msgs.count)\n".utf8))
        }
    }

    // ---- Claim 0680 control plane (queue 3 carries kinds 3/4 alongside
    // the input kinds; queue 4 streams the framebuffer back) ----

    /// Build a fixed 16-byte control message ([kind][flags=0][len LE][payload]).
    static func controlMessage(_ kind: UInt8, payload: [UInt8]) -> Data {
        var msg = Data(repeating: 0, count: inputMsgLen)
        msg[0] = kind
        msg[2] = UInt8(payload.count & 0xff)
        for (i, b) in payload.enumerated() { msg[4 + i] = b }
        return msg
    }

    /// Snapshot wire tags + sizes (normative in docs/hardware-contract.md).
    static let snapTagHeader: UInt8 = 0x01
    static let snapTagChunk: UInt8 = 0x02
    static let snapTagDone: UInt8 = 0x03

    /// One in-flight snapshot assembly (touched only on the deviceQueue —
    /// the same serial discipline as every other element access).
    struct SnapAssembly {
        var width = 0
        var height = 0
        var bpp = 0
        var total = 0
        var chunkLen = 0
        var expectedChunks = 0
        var receivedChunks = 0
        var data = Data()
        var outPath = ""
    }
    static var snap: SnapAssembly?
    /// The output path of the snapshot request most recently sent — set by
    /// fireSnapshotsIfMarker, consumed by the stream's header message.
    static var pendingSnapPath: String?
    /// Fired-but-parked request labels, FIFO (the run-03 coalescing fix —
    /// deviceQueue only). A trigger that matches while an earlier request
    /// is still armed-but-unstreamed waits here instead of overwriting
    /// pendingSnapPath or coalescing guest-side into one bool-armed
    /// stream. pumpSnapshotRequests releases the head when the stream's
    /// header claims the slot.
    static var parkedSnapPaths: [String] = []

    /// Park one fired snapshot request and try to send. deviceQueue only.
    static func parkSnapshotRequest(_ outPath: String) {
        parkedSnapPaths.append(outPath)
        if pendingSnapPath != nil {
            // The visible half of the serialization: this request would
            // have overwritten pendingSnapPath (or coalesced guest-side)
            // before the fix.
            print("CVC-SNAP-REQ: \(outPath) parked — an earlier request is still armed-but-unstreamed")
        }
        pumpSnapshotRequests()
    }

    /// Send the next parked request iff the armed slot is free — i.e. the
    /// previous request's stream header already consumed pendingSnapPath.
    /// At most one request is ever armed-but-unstreamed, so the guest's
    /// bool snap_pending can never see two undrained requests. Called on
    /// park, on the header's slot release, and on a send failure (which
    /// frees the slot and tries the next parked request).
    static func pumpSnapshotRequests() {
        guard pendingSnapPath == nil, !parkedSnapPaths.isEmpty else { return }
        let outPath = parkedSnapPaths.removeFirst()
        if !parkedSnapPaths.isEmpty {
            print("CVC-SNAP-REQ: sent \(outPath); \(parkedSnapPaths.count) request(s) still parked behind it")
        }
        pendingSnapPath = outPath
        if !enqueueMessageNow(
            controlMessage(inputKindSnapshotReq, payload: []),
            label: "snap-req → \(outPath)"
        ) {
            // The request never left the host: clear the pending path so
            // the drain does not wait out its whole window for a stream
            // that was never armed, and give the next parked request its
            // turn.
            pendingSnapPath = nil
            FileHandle.standardError.write(Data("ERROR: CVC-SNAP-REQ: queue 3 pool empty at request time (\(outPath))\n".utf8))
            pumpSnapshotRequests()
        }
    }

    /// Issue #1788: is a kind-4 scanout stream in flight right now? Read
    /// with a sync hop onto deviceQueue, so the pair is observed the same
    /// serial way the callbacks mutate it. A PARKED request counts too:
    /// it is committed to send, so stopping the device inside its wait
    /// would lose a frame the gate expects (the drain's contract).
    static func snapshotStreamInFlight() -> Bool {
        deviceQueue.sync { snap != nil || pendingSnapPath != nil || !parkedSnapPaths.isEmpty }
    }

    // ---- Claim 3141 host-push echo state (all touched only on the
    // device's serial deviceQueue) ----
    // The byte-exact request the host app writes into the guest's pre-armed
    // receive buffer, and the phase machine:
    //   0 idle            — the rx buffer is armed, waiting for the guest's
    //                       "cvc-push-armed" log line to trigger the push
    //   1 awaiting reply  — the request was enqueued + returned
    //   2 done            — the reply was observed and verified (or failed)
    static let pushRequest = Data("CVC-PING-0x42".utf8)
    /// Byte-exact success line constants (the gate greps these verbatim).
    static let pushRequestText = "CVC-PING-0x42"
    static var pushPhase = 0

    // The provider holds the configuration delegate *weakly* and the created
    // VZCustomVirtioDevice holds the device delegate *weakly* too, so both
    // delegates must be kept alive for the VM's whole lifetime or the device
    // silently goes deaf. Static stored properties live forever.
    static let configDelegate = CustomVirtioSpikeConfigDelegate()
    static let deviceDelegate = CustomVirtioSpikeDeviceDelegate()

    /// THE device delegate queue, created here so the injection paths can
    /// schedule their element access onto the SAME serial queue the
    /// callbacks run on (all queue-element access single-threaded).
    static let deviceQueue = DispatchQueue(label: "virelaios.customvirtio")

    /// The live device (captured at didCreateDevice; strong — lives as long
    /// as the VM). The input-injection paths dereference it on deviceQueue.
    static var device: VZCustomVirtioDevice?

    static func attach(to config: VZVirtualMachineConfiguration) -> String {
        let provider = VZCustomVirtioDeviceDelegateProvider(
            deviceQueue: deviceQueue,
            delegate: configDelegate
        )

        let deviceConfig = VZCustomVirtioDeviceConfiguration()
        deviceConfig.deviceID = deviceID
        deviceConfig.pciClassID = pciClass
        deviceConfig.pciSubclassID = pciSubclass
        deviceConfig.virtioQueueCount = queueCount
        deviceConfig.provider = provider

        config.customVirtioDevices = [deviceConfig]

        let did = 0x1040 + Int(deviceID)  // virtio transitional PCI DID = 0x1040 + device_id (add, not OR)
        if cvcFileEnabled {
            return String(
                format: "  custom virtio: ENABLED — VID 0x1af4 DID 0x%04x (virtio deviceID 0x%02x), class 0x%02x/0x%02x, %d queue(s) incl. push-echo, INPUT, SNAPSHOT, and the M34 FILE channel (queue 5) serving \"%@\" (--cvc-file)",
                did, Int(deviceID), Int(pciClass), Int(pciSubclass), Int(queueCount), cvcFileShareDir ?? "<none>"
            )
        }
        if cvcSnapEnabled {
            return String(
                format: "  custom virtio: ENABLED — VID 0x1af4 DID 0x%04x (virtio deviceID 0x%02x), class 0x%02x/0x%02x, %d queue(s) incl. the claim-3141 push-echo queue, the claim-9588 INPUT queue, and the claim-0680 SNAPSHOT queue (--cvc-snap)",
                did, Int(deviceID), Int(pciClass), Int(pciSubclass), Int(queueCount)
            )
        }
        if viaVirtioEnabled {
            return String(
                format: "  custom virtio: ENABLED — VID 0x1af4 DID 0x%04x (virtio deviceID 0x%02x), class 0x%02x/0x%02x, %d queue(s) incl. the claim-3141 push-echo queue and the claim-9588 INPUT queue (--via-virtio)",
                did, Int(deviceID), Int(pciClass), Int(pciSubclass), Int(queueCount)
            )
        }
        if cvcEchoEnabled {
            return String(
                format: "  custom virtio: ENABLED — VID 0x1af4 DID 0x%04x (virtio deviceID 0x%02x), class 0x%02x/0x%02x, %d queue(s) incl. the claim-3141 push-echo queue (--cvc-echo)",
                did, Int(deviceID), Int(pciClass), Int(pciSubclass), Int(queueCount)
            )
        }
        return String(
            format: "  custom virtio: ENABLED — VID 0x1af4 DID 0x%04x (virtio deviceID 0x%02x), class 0x%02x/0x%02x, %d queue(s); guest PCI discovery evidence (spike)",
            did, Int(deviceID), Int(pciClass), Int(pciSubclass), Int(queueCount)
        )
    }
}

/// Receives the created device and wires the device delegate. Called on the
/// VM's serial queue when VZVirtualMachine is created.
@available(macOS 27.0, *)
final class CustomVirtioSpikeConfigDelegate: NSObject, VZCustomVirtioDeviceConfigurationDelegate {
    func customVirtioConfiguration(_ deviceConfiguration: VZCustomVirtioDeviceConfiguration, didCreateDevice device: VZCustomVirtioDevice) {
        print("CUSTOM-VIRTIO: device created (didCreateDevice)")
        device.delegate = CustomVirtioSpike.deviceDelegate
        // Claim 9588: capture the live device so the input-injection paths
        // can reach its queues (they touch it on CustomVirtioSpike.deviceQueue
        // only — the same serial queue every callback runs on).
        CustomVirtioSpike.device = device
    }
}

/// Device lifecycle + guest-driver evidence. Every method here is optional in
/// the protocol; only the ones the spike needs are implemented.
///
/// Audit step 4/5 (claim 0828): on a queue notification the delegate
/// dequeues every available element (the guest's known-payload descriptor),
/// logs the exact bytes, and returns the element via `returnToQueue` — the
/// framework then advances the used ring AND asserts the device's
/// interrupt (the framework-internal used-buffer notification, the only
/// host→guest signaling the macOS 27 SDK exposes). The callback runs on the
/// provider's deviceQueue (serial), so element access is single-threaded.
@available(macOS 27.0, *)
final class CustomVirtioSpikeDeviceDelegate: NSObject, VZCustomVirtioDeviceDelegate {
    func customVirtioDeviceDidAcceptDriverOk(_ device: VZCustomVirtioDevice) {
        print("CUSTOM-VIRTIO: guest set DRIVER_OK — negotiation complete, queues ready")
    }

    func customVirtioDevice(_ device: VZCustomVirtioDevice, didReceiveNotificationFor queue: VZVirtioQueue) {
        print("CUSTOM-VIRTIO: guest notified queue \(queue.queueIndex) (size \(queue.queueSize))")
        if queue.queueIndex == 2 && cvcEchoEnabled {
            // Claim 3141 push-echo queue: phase-dependent handling — never
            // drain it blindly (the armed rx buffer must survive until the
            // host chooses to enqueue into it).
            handlePushNotification(device: device, queue: queue)
            return
        }
        if queue.queueIndex == 3 && viaVirtioEnabled {
            // Claim 9588 input queue: notifications here are the guest's
            // arm-time kick and per-batch replenish kicks. The rx buffers
            // are the GUEST's — never dequeue them; each log line is the
            // host-side evidence that the pool is cycling.
            print("CUSTOM-VIRTIO-INPUT: queue 3 notified (guest rx pool cycling)")
            return
        }
        if queue.queueIndex == 4 && cvcSnapEnabled {
            // Claim 0680 snapshot queue: the guest streams header/chunk/
            // done messages as device-read payloads; each element is
            // acknowledged ("OK:<n>") and returned so the guest's next
            // submit can proceed.
            handleSnapshotNotification(queue: queue)
            return
        }
        if queue.queueIndex == 5 && cvcFileEnabled {
            // M34 HF1+HF2 (issues #735/#736): the host file channel — the
            // guest sends one framed request per element; the host serves
            // it with FileManager calls rooted at the share dir (stateless:
            // no handles, nothing held between requests). VF_PROBE is the
            // transport-only 32 KiB device-write spike.
            handleFileNotification(queue: queue)
            return
        }
        // Drain every available element (many may be in flight — claim
        // 4374's concurrency): queue 0 exchanges get the payload echoed
        // back verbatim; queue 1 log lines are printed to stdout and
        // answered with ACK:<len> (claim 4837). Then return each element
        // so the used ring advances (its length reflects writtenByteCount)
        // and the device IRQ asserts.
        while let element = queue.nextElement() {
            process(element: element, device: device, queueIndex: Int(queue.queueIndex))
        }
    }

    /// Claim 3141: queue-2 notifications. Phase 0 = the arm-time kick (the
    /// guest pre-armed its receive buffer) — log and leave the buffer in
    /// place. Phase 1 = the guest's reply arrived after our push — verify
    /// byte-exactly, write the ack, return.
    private func handlePushNotification(device: VZCustomVirtioDevice, queue: VZVirtioQueue) {
        switch CustomVirtioSpike.pushPhase {
        case 0:
            print("CUSTOM-VIRTIO-PUSH: queue 2 notified with the rx buffer armed (idle — waiting for the guest's armed signal)")
        case 1:
            while let element = queue.nextElement() {
                var bytes: [UInt8] = []
                for buffer in element.readBuffers() {
                    bytes.append(contentsOf: [UInt8](buffer))
                }
                let expected = [UInt8](CustomVirtioSpike.pushRequest)
                if bytes == expected {
                    // Byte-exact reply observed: acknowledge with OK:<len>
                    // so the guest can verify the full loop closed.
                    let ackText = "OK:\(bytes.count)"
                    do {
                        try element.write(Data(ackText.utf8))
                        print("CUSTOM-VIRTIO-PUSH: reply verified rsp=\"\(CustomVirtioSpike.pushRequestText)\" (byte-exact), wrote ack=\"\(ackText)\"")
                    } catch {
                        print("CUSTOM-VIRTIO-PUSH: ack write FAILED: \(error)")
                    }
                } else {
                    print("CUSTOM-VIRTIO-PUSH: reply MISMATCH (\(bytes.count) byte(s), expected \(expected.count)): hex=[\(hexSummary(bytes))]")
                }
                element.returnToQueue()
                CustomVirtioSpike.pushPhase = 2
            }
        default:
            print("CUSTOM-VIRTIO-PUSH: unexpected queue 2 notification in phase \(CustomVirtioSpike.pushPhase)")
        }
    }

    /// Claim 3141: the host app ENQUEUES the request. Called on observing
    /// the guest's "cvc-push-armed" log line (event-driven — no timing
    /// dance): dequeue the pre-armed receive buffer, write the request into
    /// it, return it. The framework then advances the used ring and asserts
    /// the device interrupt — the only host→guest data path the SDK exposes.
    private func enqueuePushRequest(device: VZCustomVirtioDevice) {
        guard CustomVirtioSpike.pushPhase == 0 else {
            print("CUSTOM-VIRTIO-PUSH: push requested in phase \(CustomVirtioSpike.pushPhase) (skipped)")
            return
        }
        guard let queue = device.queue(at: 2) else {
            print("CUSTOM-VIRTIO-PUSH: queueAtIndex(2) unavailable")
            return
        }
        guard let element = queue.nextElement() else {
            print("CUSTOM-VIRTIO-PUSH: no pre-armed rx buffer available (nextElement nil)")
            return
        }
        guard element.readBuffersByteCount == 0, element.writeBuffersByteCount >= CustomVirtioSpike.pushRequest.count else {
            print("CUSTOM-VIRTIO-PUSH: unexpected rx shape (read \(element.readBuffersByteCount), write \(element.writeBuffersByteCount))")
            element.returnToQueue()
            return
        }
        do {
            try element.write(CustomVirtioSpike.pushRequest)
        } catch {
            print("CUSTOM-VIRTIO-PUSH: request write FAILED: \(error)")
            element.returnToQueue()
            return
        }
        element.returnToQueue()
        CustomVirtioSpike.pushPhase = 1
        print("CUSTOM-VIRTIO-PUSH: host enqueued req=\"\(CustomVirtioSpike.pushRequestText)\" (\(CustomVirtioSpike.pushRequest.count) byte(s)) into the pre-armed rx buffer")
    }

    /// Claim 0680: queue-4 notifications — reassemble the guest's tagged
    /// snapshot stream (header → chunks → done) into a raw BGRX file.
    /// Strict-order assembly: the guest submits sequentially and waits for
    /// each OK, so chunk seq must equal the received count. Any mismatch
    /// reports loudly, drops the assembly, and still acknowledges the
    /// element (the guest counts its own failure; neither side wedges).
    private func handleSnapshotNotification(queue: VZVirtioQueue) {
        while let element = queue.nextElement() {
            var bytes: [UInt8] = []
            for buffer in element.readBuffers() {
                bytes.append(contentsOf: [UInt8](buffer))
            }
            if let err = consumeSnapshotMessage(bytes) {
                print("CVC-SNAPSHOT: ERROR \(err) (\(bytes.count)-byte message dropped, assembly aborted)")
                CustomVirtioSpike.snap = nil
            }
            do {
                try element.write(Data("OK:\(bytes.count)".utf8))
            } catch {
                print("CVC-SNAPSHOT: ack write FAILED: \(error)")
            }
            element.returnToQueue()
        }
    }

    /// Consume one tagged snapshot message; nil = accepted, String = loud
    /// error. Mirrors the guest's little-endian framing exactly.
    private func consumeSnapshotMessage(_ bytes: [UInt8]) -> String? {
        guard bytes.count >= 1 else { return "empty message" }
        switch bytes[0] {
        case CustomVirtioSpike.snapTagHeader:
            guard bytes.count >= 24 else { return "short header" }
            guard bytes[1...4].elementsEqual("SNAP".utf8) else { return "bad header magic" }
            guard bytes[5] == 1 else { return "unsupported header version \(bytes[5])" }
            var a = CustomVirtioSpike.SnapAssembly()
            func le32(_ off: Int) -> Int {
                Int(bytes[off]) | (Int(bytes[off + 1]) << 8) | (Int(bytes[off + 2]) << 16) | (Int(bytes[off + 3]) << 24)
            }
            a.bpp = Int(bytes[6])
            a.width = le32(8)
            a.height = le32(12)
            a.total = le32(16)
            a.chunkLen = le32(20)
            guard a.width > 0, a.height > 0, a.bpp == 4 else { return "nonsensical geometry w=\(a.width) h=\(a.height) bpp=\(a.bpp)" }
            guard a.total > 0, a.chunkLen > 0 else { return "nonsensical sizes total=\(a.total) chunk=\(a.chunkLen)" }
            a.expectedChunks = (a.total + a.chunkLen - 1) / a.chunkLen
            a.data.reserveCapacity(a.total)
            // The out path rides the trigger that requested THIS stream;
            // keep it across the header reset by re-reading the pending
            // trigger assignment made in fireSnapshotsIfMarker.
            if let pending = CustomVirtioSpike.pendingSnapPath { a.outPath = pending }
            CustomVirtioSpike.pendingSnapPath = nil
            // The armed slot just released: a request parked behind this
            // stream may go out now (its own stream follows this one on
            // the guest's next drain — never merged into it).
            CustomVirtioSpike.pumpSnapshotRequests()
            CustomVirtioSpike.snap = a
            print("CVC-SNAPSHOT: header w=\(a.width) h=\(a.height) bpp=\(a.bpp) total=\(a.total) chunk=\(a.chunkLen) chunks=\(a.expectedChunks) → \(a.outPath)")
            return nil
        case CustomVirtioSpike.snapTagChunk:
            guard var a = CustomVirtioSpike.snap else { return "chunk before header" }
            guard bytes.count >= 12 else { return "short chunk envelope" }
            let seq = Int(bytes[2]) | (Int(bytes[3]) << 8)
            let len = Int(bytes[4]) | (Int(bytes[5]) << 8)
            let cksum = UInt16(Int(bytes[6]) | (Int(bytes[7]) << 8))
            guard seq == a.receivedChunks else { return "chunk seq \(seq) out of order (expected \(a.receivedChunks))" }
            guard len == bytes.count - 12, len > 0 else { return "chunk len field \(len) != payload \(bytes.count - 12)" }
            guard rfc1071(Array(bytes[12...])) == cksum else { return "chunk seq \(seq) cksum mismatch" }
            a.data.append(contentsOf: bytes[12...])
            a.receivedChunks += 1
            CustomVirtioSpike.snap = a
            if a.receivedChunks % 32 == 0 {
                print("CVC-SNAPSHOT: chunk \(a.receivedChunks)/\(a.expectedChunks)")
            }
            return nil
        case CustomVirtioSpike.snapTagDone:
            guard let a = CustomVirtioSpike.snap else { return "done before header" }
            guard bytes.count >= 10 else { return "short done message" }
            let chunks = Int(bytes[2]) | (Int(bytes[3]) << 8)
            let total = Int(bytes[4]) | (Int(bytes[5]) << 8) | (Int(bytes[6]) << 16) | (Int(bytes[7]) << 24)
            let cksum = UInt16(Int(bytes[8]) | (Int(bytes[9]) << 8))
            guard chunks == a.expectedChunks, chunks == a.receivedChunks else {
                return "done claims \(chunks) chunks (expected \(a.expectedChunks), received \(a.receivedChunks))"
            }
            guard total == a.data.count else { return "done claims \(total) bytes (received \(a.data.count))" }
            let frameCksum = rfc1071([UInt8](a.data))
            guard frameCksum == cksum else {
                return String(format: "frame cksum mismatch got=0x%04x want=0x%04x", frameCksum, cksum)
            }
            let out = Data(a.data)
            let url = URL(fileURLWithPath: a.outPath)
            do {
                try out.write(to: url)
            } catch {
                return "raw write to \(a.outPath) FAILED: \(error)"
            }
            print(String(format: "CVC-SNAPSHOT: done chunks=%d bytes=%d cksum=0x%04x path=%@", chunks, total, cksum, a.outPath))
            CustomVirtioSpike.snap = nil
            return nil
        default:
            return "unknown tag 0x\(String(bytes[0], radix: 16))"
        }
    }

    /// RFC 1071 one's-complement Internet checksum over big-endian words —
    /// byte-for-byte the same function as the guest's ipv4.checksum /
    /// virtio_custom.checksum1071, so the host verifies what the guest sent.
    /// The accumulator is UInt64: a whole-frame checksum over the 3.5 MiB
    /// scanout overflows UInt32 before the fold (observed live as a Swift
    /// "arithmetic overflow" trap, claim 0680).
    private func rfc1071(_ b: [UInt8]) -> UInt16 {
        var sum: UInt64 = 0
        var i = 0
        while i + 1 < b.count {
            sum += (UInt64(b[i]) << 8) | UInt64(b[i + 1])
            i += 2
        }
        if i < b.count { sum += UInt64(b[i]) << 8 }
        while (sum >> 16) != 0 { sum = (sum & 0xffff) + (sum >> 16) }
        return ~UInt16(sum & 0xffff)
    }

    private func process(element: VZVirtioQueueElement, device: VZCustomVirtioDevice, queueIndex: Int) {
        // Reassemble the guest's device-read spans (claim 9492: a
        // >4 KiB payload arrives as several readBuffers()).
        var bytes: [UInt8] = []
        for buffer in element.readBuffers() {
            bytes.append(contentsOf: [UInt8](buffer))
        }
        if queueIndex == 1 {
            // Guest log transport (claim 4837): print the line verbatim to
            // the runner stdout and write ACK:<len> back into the element's
            // write buffers — the guest verifies the ack.
            let line = String(bytes: bytes, encoding: .utf8) ?? "<non-utf8>"
            print("CUSTOM-VIRTIO-LOG: \(line)")
            // Claim 0680: structured console — raw bytes into the file, no
            // injected newlines, so the capture stays byte-faithful.
            if let h = cvcConsoleFileHandle {
                do { try h.write(Data(bytes)) } catch {
                    print("CUSTOM-VIRTIO-CONSOLE: file write FAILED: \(error)")
                }
            }
            if line == "cvconsole-ready" && cvcConsoleFileHandle != nil {
                // Claim 0680: the guest's queue-3 pool is live — arm the
                // console tee (kind-3, bit0=1) NOW on this serial
                // deviceQueue, event-driven like the claim-3141 push.
                _ = CustomVirtioSpike.enqueueMessageNow(
                    CustomVirtioSpike.controlMessage(CustomVirtioSpike.inputKindConsoleCtrl, payload: [0x01]),
                    label: "console-arm enable=1"
                )
            }
            if line == "cvc-push-armed" && cvcEchoEnabled {
                // Claim 3141: the guest pre-armed its push rx buffer — the
                // HOST now chooses the moment and enqueues the request.
                // Inline on this serial deviceQueue, so ordering with the
                // queue-2 reply notification is deterministic.
                enqueuePushRequest(device: device)
            }
            let ack = Data("ACK:\(bytes.count)".utf8)
            do {
                try element.write(ack)
                print("CUSTOM-VIRTIO: log ack written (\(element.writtenByteCount) byte(s) into \(element.writeBuffersByteCount) byte(s) of write buffers)")
            } catch {
                print("CUSTOM-VIRTIO: log ack write FAILED: \(error)")
            }
        } else {
            // Queue-0 exchange: echo the exact reassembled payload back
            // (claim 0828's bidirectional flow, now length-agnostic). The
            // hex summary is bounded so a 12,340-byte payload does not
            // flood the runner log; the byte count + the guest's
            // byte-for-byte echo comparison carry the assertion.
            print("CUSTOM-VIRTIO: dequeued \(bytes.count) byte(s) (read \(element.readBuffersByteCount)): hex=[\(hexSummary(bytes))] ascii=\"\(printableAscii(bytes))\"")
            if element.writeBuffersByteCount >= bytes.count {
                do {
                    try element.write(Data(bytes))
                    print("CUSTOM-VIRTIO: echoed \(element.writtenByteCount) byte(s) into \(element.writeBuffersByteCount) byte(s) of write buffers")
                } catch {
                    print("CUSTOM-VIRTIO: reply write FAILED: \(error)")
                }
            } else {
                print("CUSTOM-VIRTIO: reply write skipped (write buffers \(element.writeBuffersByteCount) < \(bytes.count))")
            }
        }
        element.returnToQueue()
        print("CUSTOM-VIRTIO: returned element to queue \(queueIndex) — used ring advanced, device interrupt asserted")
    }

    /// Bounded hex rendering: full hex up to 64 bytes, otherwise the first
    /// and last 16 bytes + a 32-bit running sum (the claim-9492 big
    /// payload is 12,340 bytes of non-printable pattern).
    private func hexSummary(_ bytes: [UInt8]) -> String {
        let hex = { (slice: ArraySlice<UInt8>) in
            slice.map { String(format: "%02x", $0) }.joined(separator: " ")
        }
        if bytes.count <= 64 {
            return hex(bytes[0...])
        }
        var sum: UInt32 = 0
        for b in bytes { sum = sum &+ UInt32(b) }
        return "\(hex(bytes[0..<16]))..\(hex(bytes[(bytes.count - 16)...])) sum=0x\(String(format: "%08x", sum))"
    }

    /// The payload's ASCII form, or <binary> when any byte is non-printable
    /// (the big-payload pattern is binary by design).
    private func printableAscii(_ bytes: [UInt8]) -> String {
        guard bytes.allSatisfy({ $0 >= 0x20 && $0 <= 0x7e }) else { return "<binary>" }
        return String(bytes: bytes, encoding: .utf8) ?? "<non-utf8>"
    }

    func customVirtioDeviceWillReset(_ device: VZCustomVirtioDevice) {
        print("CUSTOM-VIRTIO: device reset")
    }

    func customVirtioDeviceWillStop(_ device: VZCustomVirtioDevice) {
        print("CUSTOM-VIRTIO: device stopped")
    }
}

#endif

