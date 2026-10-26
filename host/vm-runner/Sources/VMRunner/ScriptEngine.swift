// Script/pointer engine: --script forwarding, key/chord
// injection, pointer scripting, and the forward-once driver.

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


// Claim 6684: scripted-input mode. Waits until the guest has reached the
// configured serial marker (the takeover terminal state by default), then
// forwards the script file's bytes exactly once into the serial attachment.
// The guest supplied its
// virtio RX buffer pre-exit, so nothing is lost while we wait; the settle
// delay only avoids racing the shell's very first poll.
func startScriptInput() {
    guard let scriptPath else { return }
    forwardScriptOnce(path: scriptPath, after: scriptAfter, label: "script", settle: 0.5)
}

// Claim 4613: the SECOND scripted phase. The primary --script is forwarded
// in ONE burst, so a command that must land AFTER a background program
// exits and is reaped (the long-lived gate re-execs USER.BIN into the
// freed pool slot) cannot be in the same burst. --script2 is forwarded
// once, after its own serial marker (the first USER.BIN's reap line),
// using the identical settle-then-forward machinery.
func startScript2Input() {
    guard let path = script2Path, let after = script2After else { return }
    forwardScriptOnce(path: path, after: after, label: "script2", settle: script2Delay)
}

// Card 3c (claim 7786): the THIRD scripted phase. The kill gate needs a
// post-reap snapshot — the `procs | pages | exec USER.BIN | procs` read
// that proves the killed process's pages returned and its slot was
// re-used — which must land AFTER the kill's reap line, so it cannot be
// in the one-burst primary script (claim 6684) or the claim-4613 second
// phase. --script3 is forwarded once after its own serial marker using
// the same settle-then-forward machinery.
func startScript3Input() {
    guard let path = script3Path, let after = script3After else { return }
    forwardScriptOnce(path: path, after: after, label: "script3", settle: script3Delay)
}

// Marker-watch deadline policy for the scripted-input injectors
// (--input-key, --input-string): M73z (#1638) + #2703 — a gate may anchor
// input on a marker deep into a long run (boot 04's `gotabwm: alt-tab
// id=4` lands ~200 s in; its `gotabwm: tab close id=` lands ~160 s in),
// and a marker can never arrive after --timeout, so the session timeout
// is the honest ceiling. The 40 s floor keeps short-timeout sessions on
// the original window.
func inputMarkerWaitSeconds(sessionTimeout: TimeInterval) -> TimeInterval {
    max(40, sessionTimeout)
}

// Milestone seven card I2 (claim 4116): synthesize ONE host key event into
// the VZVirtualMachineView after the marker appears. VZ has no programmatic
// keyboard-injection API — VZUSBKeyboardConfiguration is driven only by a
// VZVirtualMachineView forwarding host key events — so the runner builds an
// NSEvent keyDown and dispatches it straight into the view (the host keycode
// maps to the guest HID usage inside VZ). This is the MINIMAL seam the I2
// gate needs to produce one deterministic HID report; the full scripted
// key-sequence surface that types into Road Pops is I3.
func startKeyInject() {
    guard let keyCode = inputKeyCode else { return }
    let q = DispatchQueue(label: "virelaios.keyinject")
    q.async {
        let marker = inputKeyAfter ?? "usb: enumerated"
        // M73z (#1638): timeout-aware deadline, mirroring claim 0142's
        // script-wait fix — a gate may anchor this key on a marker deep
        // into a long run (boot 04's `gotabwm: alt-tab id=4` lands ~200 s
        // in), and a marker can never arrive after --timeout.
        let waitSeconds = inputMarkerWaitSeconds(sessionTimeout: timeout)
        let waitDeadline = Date().addingTimeInterval(waitSeconds)
        var sent = false
        while Date() < waitDeadline {
            if let text = try? String(contentsOf: serialURL, encoding: .utf8),
               text.contains(marker) {
                Thread.sleep(forTimeInterval: 0.5) // let the armed ring settle
                #if SPIKE
                if viaVirtioEnabled {
                    // Claim 9588: ride the custom-virtio INPUT queue — no
                    // view, no NSEvent, no window (headless-safe). KeyDown
                    // only, the same single-report shape as the synthesized
                    // seam.
                    guard #available(macOS 27.0, *) else {
                        fail("--via-virtio requires macOS 27 (VZCustomVirtioDevice).")
                    }
                    guard let ch = CustomVirtioSpike.charFromMacCode(keyCode),
                          let (usage, shift) = CustomVirtioSpike.hidUsage(for: ch) else {
                        FileHandle.standardError.write(Data("ERROR: --via-virtio --input-key: no HID mapping for macOS keycode \(keyCode)\n".utf8))
                        return
                    }
                    let mods: UInt8 = shift ? CustomVirtioSpike.hidModShift : 0
                    FileHandle.standardOutput.write(Data("KEY-INJECT: keyCode \(keyCode) keyDown rides the custom-virtio INPUT queue after \"\(marker)\"\n".utf8))
                    CustomVirtioSpike.injectStrokes(
                        [CustomVirtioSpike.HidStroke(mods: mods, usage: usage, isDown: true)],
                        sequenceTag: "key-inject"
                    )
                    sent = true
                    break
                }
                #endif
                DispatchQueue.main.async {
                    guard let view = machineView else {
                        FileHandle.standardError.write(Data("ERROR: --input-key needs --display/--screenshot (no VZVirtualMachineView)\n".utf8))
                        return
                    }
                    let windowNumber = view.window?.windowNumber ?? 0
                    let now = ProcessInfo.processInfo.systemUptime
                    // KeyDown only (no keyUp): a single deterministic
                    // "key pressed" HID report, no up/down timing race.
                    if let down = NSEvent.keyEvent(with: .keyDown, location: .zero, modifierFlags: [], timestamp: now, windowNumber: windowNumber, context: nil, characters: "a", charactersIgnoringModifiers: "a", isARepeat: false, keyCode: keyCode) {
                        view.keyDown(with: down)
                    }
                    FileHandle.standardOutput.write(Data("KEY-INJECT: keyCode \(keyCode) keyDown dispatched to the VZVirtualMachineView after \"\(marker)\"\n".utf8))
                    // Claim 8844: report the delivery-relevant window state so
                    // a silent VZ drop (not-key window) is diagnosed here.
                    reportKeyboardKeyState(view, label: "KEY-INJECT")
                }
                sent = true
                break
            }
            Thread.sleep(forTimeInterval: 0.1)
        }
        if !sent {
            FileHandle.standardError.write(Data("ERROR: guest did not emit key-inject marker '\(marker)' within \(Int(waitSeconds))s; key not injected\n".utf8))
        }
    }
}

// Milestone seven card I3 (claim 6050): map an ASCII character to its macOS
// virtual keycode + whether shift is required. Only the usable subset the
// guest keymap accepts is covered; anything else returns nil (the caller
// fails honestly rather than inventing a keystroke). Enter is `\n`.
func macKey(for ch: Character) -> (UInt16, Bool)? {
    switch ch {
    case "a": return (0x00, false); case "b": return (0x0B, false)
    case "c": return (0x08, false); case "d": return (0x02, false)
    case "e": return (0x0E, false); case "f": return (0x03, false)
    case "g": return (0x05, false); case "h": return (0x04, false)
    case "i": return (0x22, false); case "j": return (0x26, false)
    case "k": return (0x28, false); case "l": return (0x25, false)
    case "m": return (0x2E, false); case "n": return (0x2D, false)
    case "o": return (0x1F, false); case "p": return (0x23, false)
    case "q": return (0x0C, false); case "r": return (0x0F, false)
    case "s": return (0x01, false); case "t": return (0x11, false)
    case "u": return (0x20, false); case "v": return (0x09, false)
    case "w": return (0x0D, false); case "x": return (0x07, false)
    case "y": return (0x10, false); case "z": return (0x06, false)
    case "A": return (0x00, true);  case "B": return (0x0B, true)
    case "C": return (0x08, true);  case "D": return (0x02, true)
    case "E": return (0x0E, true);  case "F": return (0x03, true)
    case "G": return (0x05, true);  case "H": return (0x04, true)
    case "I": return (0x22, true);  case "J": return (0x26, true)
    case "K": return (0x28, true);  case "L": return (0x25, true)
    case "M": return (0x2E, true);  case "N": return (0x2D, true)
    case "O": return (0x1F, true);  case "P": return (0x23, true)
    case "Q": return (0x0C, true);  case "R": return (0x0F, true)
    case "S": return (0x01, true);  case "T": return (0x11, true)
    case "U": return (0x20, true);  case "V": return (0x09, true)
    case "W": return (0x0D, true);  case "X": return (0x07, true)
    case "Y": return (0x10, true);  case "Z": return (0x06, true)
    case "0": return (0x1D, false); case "1": return (0x12, false)
    case "2": return (0x13, false); case "3": return (0x14, false)
    case "4": return (0x15, false); case "5": return (0x17, false)
    case "6": return (0x16, false); case "7": return (0x1A, false)
    case "8": return (0x1C, false); case "9": return (0x19, false)
    case " ": return (0x31, false)
    case "_": return (0x1B, true)
    case ".": return (0x2F, false); case ",": return (0x2B, false)
    case "/": return (0x2C, false); case "?": return (0x2C, true)
    case "-": return (0x1B, false)
    case "=": return (0x18, false); case "[": return (0x21, false)
    case "]": return (0x1E, false); case "\\": return (0x2A, false)
    case ";": return (0x29, false); case "'": return (0x27, false)
    case "`": return (0x32, false)
    case "\n": return (0x24, false) // Enter / Return
    default: return nil
    }
}

// Milestone eight card U2 (claim 1809): map one `--input-chords` token to a
// (keyCode, modifiers, characters) triple. A single printable char uses
// macKey (shift for uppercase); the named chords map to the macOS virtual
// keycodes for the nav cluster (function-key characters so VZ translates the
// keyCode to the guest HID usage), and ctrl-x maps to the letter's keycode
// with the .control modifier. nil = unknown chord (the caller fails honestly).
func macChord(_ token: String) -> (UInt16, NSEvent.ModifierFlags, String)? {
    switch token {
    case "return": return (0x24, [], "\r")
    case "space": return (0x31, [], " ")
    case "comma": return (0x2B, [], ",") // K12/K16: the CSV token separator cannot carry a literal comma
    case "ctrl-comma": return (0x2B, [.control], ",")
    case "ctrl-space": return (0x31, [.control], " ") // M37 DQ1: the God Menu summon chord
    case "tab": return (0x30, [], "\t")
    case "alt-tab": return (0x30, [.option], "\t") // WMS6 Gate A: Option+Tab
    case "alt-shift-tab": return (0x30, [.option, .shift], "\t")
    case "ctrl-tab": return (0x30, [.control], "\t") // M63r #1424
    case "ctrl-shift-tab": return (0x30, [.control, .shift], "\t")
    // M80k (#1727): the terminal's full-RIS chord (macOS path; 'r' = 0x0F).
    case "ctrl-shift-alt-r": return (0x0F, [.control, .shift, .option], "r")
    // M137a/c (#2771/#2773, ADR 0007 R4/R5): the seat's pane-focus arrows
    // and space-switch paging. The ctrl-/ctrl-shift- prefix patterns are
    // letters only, so the nav-cluster chords are named outright, same gap
    // ctrl-shift-alt-r fills.
    case "ctrl-alt-up": return (0x7E, [.control, .option], "\u{F700}")
    case "ctrl-alt-down": return (0x7D, [.control, .option], "\u{F701}")
    case "ctrl-alt-left": return (0x7B, [.control, .option], "\u{F702}")
    case "ctrl-alt-right": return (0x7C, [.control, .option], "\u{F703}")
    case "ctrl-alt-pageup": return (0x74, [.control, .option], "\u{F72C}")
    case "ctrl-alt-pagedown": return (0x79, [.control, .option], "\u{F72D}")
    case "up": return (0x7E, [], "\u{F700}")
    case "down": return (0x7D, [], "\u{F701}")
    case "left": return (0x7B, [], "\u{F702}")
    case "right": return (0x7C, [], "\u{F703}")
    case "home": return (0x73, [], "\u{F729}")
    case "end": return (0x77, [], "\u{F72B}")
    case "delete": return (0x75, [], "\u{F728}")
    case "backspace": return (0x33, [], "\u{7F}") // M68a (#1449): macOS keycode 0x33 = Delete/Backspace; 0x7F is the byte a terminal expects
    case "pageup": return (0x74, [], "\u{F72C}")
    case "pagedown": return (0x79, [], "\u{F72D}")
    case "escape": return (0x35, [], "\u{1B}")
    case "insert": return (0x72, [], "\u{F729}")
    case "f1": return (0x7A, [], "\u{F704}")
    case "f2": return (0x78, [], "\u{F705}")
    case "f3": return (0x63, [], "\u{F706}")
    case "f4": return (0x76, [], "\u{F707}")
    case "f5": return (0x60, [], "\u{F708}")
    case "f6": return (0x61, [], "\u{F709}")
    case "f7": return (0x62, [], "\u{F70A}")
    case "f8": return (0x64, [], "\u{F70B}")
    case "f9": return (0x6D, [], "\u{F70C}")
    case "f10": return (0x67, [], "\u{F70D}")
    case "f11": return (0x6F, [], "\u{F70E}")
    case "f12": return (0x6A, [], "\u{F70F}")
    default:
        if token.hasPrefix("ctrl-shift-"), token.count == 12 {
            let letter = token[token.index(token.startIndex, offsetBy: 11)]
            if let (code, _) = macKey(for: letter) {
                return (code, [.control, .shift], String(letter))
            }
        }
        if token.hasPrefix("ctrl-"), token.count == 6 {
            let letter = token[token.index(token.startIndex, offsetBy: 5)]
            if let (code, _) = macKey(for: letter) {
                return (code, .control, String(letter))
            }
        }
        if token.count == 1, let (code, shift) = macKey(for: token[token.startIndex]) {
            return (code, shift ? .shift : [], token)
        }
        return nil
    }
}

// Milestone eight card U2 (claim 1809): type the `--input-chords` sequence
// into the VZVirtualMachineView once the marker appears — keyDown + keyUp
// per chord, mirroring the --input-string timing (one report per Road Pops
// present cadence). This is how arrows/Ctrl chords reach the I3 keymap on
// real hardware.
func startChordInject(csv inputCsv: String?, after inputAfter: String?) {
    guard let csv = inputCsv else { return }
    let q = DispatchQueue(label: "virelaios.chords")
    q.async {
        let marker = inputAfter ?? "userspace: el0=1"
        // #990 (claim #997): the marker deadline was a hardcoded 60 s — the
        // WM2 gate's choreography (loop-ok + 20 s settle) lands chord-go at
        // ~88 s, so the chords were NEVER typed ("guest did not emit
        // chord-seq marker ... within 60s") and every WM boot failed its
        // checks with key_fan=0. Key the watcher to the run deadline like
        // every other marker watcher.
        let waitDeadline = deadline
        var sent = false
        while Date() < waitDeadline {
            if let log = try? String(contentsOf: serialURL, encoding: .utf8), log.contains(marker) {
                Thread.sleep(forTimeInterval: 1.0) // let the armed ring settle
                #if SPIKE
                if viaVirtioEnabled && !chordsViaView {
                    // Claim 9588: chords ride the custom-virtio INPUT queue —
                    // keyDown + keyUp per token as HID reports, no view, no
                    // window, headless-safe. (--chords-view forces the view
                    // path below even when this queue is attached.)
                    guard #available(macOS 27.0, *) else {
                        fail("--via-virtio requires macOS 27 (VZCustomVirtioDevice).")
                    }
                    var strokes: [CustomVirtioSpike.HidStroke] = []
                    for token in csv.split(separator: ",").map(String.init) {
                        guard let (mods, usage) = CustomVirtioSpike.hidChord(token) else {
                            FileHandle.standardError.write(Data("ERROR: --via-virtio --input-chords: no HID mapping for chord '\(token)'\n".utf8))
                            FileHandle.standardOutput.write(Data("CHORD-SEQ: aborted (unknown chord) ok=false\n".utf8))
                            return
                        }
                        strokes.append(CustomVirtioSpike.HidStroke(mods: mods, usage: usage, isDown: true))
                        strokes.append(CustomVirtioSpike.HidStroke(mods: 0, usage: 0, isDown: false))
                    }
                    FileHandle.standardOutput.write(Data("CHORD-SEQ: typed \(csv.debugDescription) over the custom-virtio INPUT queue after \"\(marker)\" transport=cv-input\n".utf8))
                    CustomVirtioSpike.injectStrokes(strokes, sequenceTag: "chord-seq")
                    sent = true
                    break
                }
                #endif
                guard let view = machineView else {
                    FileHandle.standardError.write(Data("ERROR: --input-chords needs --display/--screenshot (no VZVirtualMachineView)\n".utf8))
                    return
                }
                let windowNumber = view.window?.windowNumber ?? 0
                var events: [(type: NSEvent.EventType, code: UInt16, mods: NSEvent.ModifierFlags, chars: String)] = []
                var allOk = true
                for token in csv.split(separator: ",").map(String.init) {
                    guard let (code, mods, chars) = macChord(token) else {
                        FileHandle.standardError.write(Data("ERROR: --input-chords: no mapping for chord '\(token)'\n".utf8))
                        allOk = false
                        break
                    }
                    events.append((.keyDown, code, mods, chars))
                    events.append((.keyUp, code, mods, chars))
                }
                if allOk {
                    // Chain the events: each keyDown/keyUp is scheduled only
                    // AFTER the previous one fires, so the 3 s gaps are real
                    // wall-clock gaps. Scheduling every event up front with
                    // increasing asyncAfter deadlines lets a busy main queue
                    // (VM display updates) fire several in a burst, which
                    // drops/reorders reports at the guest's single-pending-
                    // report interrupt-IN endpoint (observed claim-time).
                    var remaining = events
                    func fireNext(after delay: Double, first: Bool) {
                        if remaining.isEmpty {
                            FileHandle.standardOutput.write(Data("CHORD-SEQ: typed \(csv.debugDescription) into the VZVirtualMachineView after \"\(marker)\" ok=true\n".utf8))
                            return
                        }
                        if first, let view = machineView {
                            // Claim 8844: same delivery-state report as
                            // KEY-SEQ, before the first chord lands.
                            reportKeyboardKeyState(view, label: "CHORD-SEQ")
                        }
                        let evt = remaining.removeFirst()
                        DispatchQueue.main.asyncAfter(deadline: .now() + delay) {
                            let t = ProcessInfo.processInfo.systemUptime
                            if let e = NSEvent.keyEvent(with: evt.type, location: .zero, modifierFlags: evt.mods, timestamp: t, windowNumber: windowNumber, context: nil, characters: evt.chars, charactersIgnoringModifiers: evt.chars, isARepeat: false, keyCode: evt.code) {
                                if evt.type == .keyDown {
                                    view.keyDown(with: e)
                                } else {
                                    view.keyUp(with: e)
                                }
                            }
                            fireNext(after: inputChordsDelay, first: false)
                        }
                    }
                    fireNext(after: 0.0, first: true)
                } else {
                    FileHandle.standardOutput.write(Data("CHORD-SEQ: aborted (unknown chord) ok=false\n".utf8))
                }
                sent = true
                break
            }
            Thread.sleep(forTimeInterval: 0.1)
        }
        if !sent {
            FileHandle.standardError.write(Data("ERROR: guest did not emit chord-seq marker '\(marker)' before the run deadline; chords not typed\n".utf8))
        }
    }
}

func startPointerInject() {
    guard let script = pointerScript else { return }
    if pointerRequestTrust && !AXIsProcessTrusted() {
        // Prompt the system to grant Accessibility to the responsible
        // process (the terminal). Returns immediately; the user grants in
        // System Settings and re-runs. The cg route checks trust per post
        // and reports PTR-TRUST honestly either way.
        let opts = [kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary
        let nowTrusted = AXIsProcessTrustedWithOptions(opts)
        FileHandle.standardOutput.write(Data("PTR-TRUST: requested accessibility prompt, now-trusted=\(nowTrusted ? 1 : 0)\n".utf8))
    }
    let q = DispatchQueue(label: "virelaios.ptrseq")
    q.async {
        let marker = pointerAfter ?? "tasks user-el0 reaped"
        let waitDeadline = deadline // #990: was a hardcoded 120 s (same late-marker class)
        var sent = false
        while Date() < waitDeadline {
            if let log = try? String(contentsOf: serialURL, encoding: .utf8), log.contains(marker) {
                Thread.sleep(forTimeInterval: 0.5)
                guard let view = machineView else {
                    FileHandle.standardError.write(Data("ERROR: --pointer needs --display/--screenshot (no VZVirtualMachineView)\n".utf8))
                    return
                }
                let windowNumber = view.window?.windowNumber ?? 0
                // Resolve every step up front; a malformed step aborts
                // before any event fires (fail honestly, invent nothing).
                var steps: [(x: CGFloat, y: CGFloat, click: Bool)] = []
                for part in script.split(separator: ";") {
                    let fields = part.split(separator: ",", omittingEmptySubsequences: false).map { String($0).trimmingCharacters(in: .whitespaces) }
                    guard fields.count >= 2, let gx = Double(fields[0]), let gy = Double(fields[1]), gx >= 0, gy >= 0, gx <= 1280, gy <= 720 else {
                        FileHandle.standardError.write(Data("ERROR: --pointer step '\(part)' is not <x>,<y>[,c] with 0<=x<=1280, 0<=y<=720\n".utf8))
                        steps = []
                        break
                    }
                    let click = fields.count >= 3 && (fields[2] == "c" || fields[2] == "1")
                    steps.append((CGFloat(gx), CGFloat(gy), click))
                }
                if !steps.isEmpty {
                    let vh = view.bounds.height
                    // A real mouse always ENTERS the window before moving —
                    // synthesized moves alone were observed not to wake VZ's
                    // pointer tracking. Lead with a mouseEntered over the
                    // view (claim-time experiment, card U4).
                    DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
                        let t = ProcessInfo.processInfo.systemUptime
                        let loc = NSPoint(x: vh * 0 + 8, y: vh - 8)
                        if let e = NSEvent.enterExitEvent(with: .mouseEntered, location: loc, modifierFlags: [], timestamp: t, windowNumber: windowNumber, context: nil, eventNumber: 1, trackingNumber: 1, userData: nil) {
                            if let w = view.window {
                                w.sendEvent(e)
                            } else {
                                view.mouseEntered(with: e)
                            }
                            FileHandle.standardOutput.write(Data("PTR-EVT entered\n".utf8))
                        }
                    }
                    var delay: Double = 3.0
                    for st in steps {
                        let s = st
                        // Guest pixels are top-left origin; AppKit view
                        // coordinates are bottom-left — flip Y.
                        let loc = NSPoint(x: s.x, y: vh - s.y)
                        DispatchQueue.main.asyncAfter(deadline: .now() + delay) {
                            let t = ProcessInfo.processInfo.systemUptime
                            if let e = NSEvent.mouseEvent(with: .mouseMoved, location: loc, modifierFlags: [], timestamp: t, windowNumber: windowNumber, context: nil, eventNumber: 0, clickCount: 0, pressure: 0) {
                                deliverPointerEvent(view, e)
                                FileHandle.standardOutput.write(Data("PTR-EVT move \(Int(s.x)),\(Int(s.y))\n".utf8))
                            }
                        }
                        delay += 3.0
                        if s.click {
                            for (type, name) in [(NSEvent.EventType.leftMouseDown, "down"), (NSEvent.EventType.leftMouseUp, "up")] {
                                let ty = type
                                let nm = name
                                DispatchQueue.main.asyncAfter(deadline: .now() + delay) {
                                    let t = ProcessInfo.processInfo.systemUptime
                                    if let e = NSEvent.mouseEvent(with: ty, location: loc, modifierFlags: [], timestamp: t, windowNumber: windowNumber, context: nil, eventNumber: 0, clickCount: 1, pressure: ty == .leftMouseDown ? 1.0 : 0.0) {
                                        deliverPointerEvent(view, e)
                                        FileHandle.standardOutput.write(Data("PTR-EVT click \(nm) \(Int(s.x)),\(Int(s.y))\n".utf8))
                                    }
                                }
                                delay += 3.0
                            }
                        }
                    }
                    FileHandle.standardOutput.write(Data("PTR-SEQ: \(steps.count) pointer steps scheduled after \"\(marker)\" ok=true\n".utf8))
                } else {
                    FileHandle.standardOutput.write(Data("PTR-SEQ: aborted (malformed step) ok=false\n".utf8))
                }
                sent = true
                break
            }
            Thread.sleep(forTimeInterval: 0.1)
        }
        if !sent {
            FileHandle.standardError.write(Data("ERROR: guest did not emit pointer marker '\(marker)' within 120s; pointer steps not sent\n".utf8))
        }
    }
}

// Claim 9367: the --pointer-virtio sequence — pointer injection over the
// custom-virtio INPUT queue (kind-2 absolute-pointer messages), HEADLESS:
// no view, no window, no NSEvent/CGEvent anywhere. Same step grammar and
// guest-pixel coordinate space as --pointer ("x,y[,c]" steps, 0<=x<=1280,
// 0<=y<=720); each pixel coord is converted to the HID absolute 0..32767
// logical space the guest's map_pointer_axis expects. The sequence is
// scheduled once `--pointer-virtio-after <marker>` appears in the serial
// log (deterministic choreography, not a sleep).
func startPointerVirtioInject() {
    guard let script = pointerVirtioScript else { return }
    let q = DispatchQueue(label: "virelaios.ptrcv")
    q.async {
        let marker = pointerVirtioAfter ?? "winloop: present ok"
        let waitDeadline = deadline // #990: was a hardcoded 120 s (same late-marker class)
        var sent = false
        while Date() < waitDeadline {
            if let log = try? String(contentsOf: serialURL, encoding: .utf8), log.contains(marker) {
                Thread.sleep(forTimeInterval: 0.5)
                #if SPIKE
                guard #available(macOS 27.0, *) else {
                    fail("--pointer-virtio requires macOS 27 (VZCustomVirtioDevice).")
                }
                // Resolve every step up front; a malformed step aborts
                // before any message fires (fail honestly, invent nothing).
                var steps: [CustomVirtioSpike.PtrStep] = []
                for part in script.split(separator: ";") {
                    let fields = part.split(separator: ",", omittingEmptySubsequences: false).map { String($0).trimmingCharacters(in: .whitespaces) }
                    guard fields.count >= 2, let gx = Double(fields[0]), let gy = Double(fields[1]), gx >= 0, gy >= 0, gx <= 1280, gy <= 720 else {
                        FileHandle.standardError.write(Data("ERROR: --pointer-virtio step '\(part)' is not <x>,<y>[,c|d|u|rc] with 0<=x<=1280, 0<=y<=720\n".utf8))
                        steps = []
                        break
                    }
                    // Grammar: bare = move (buttons 0); `c`/`1` = click
                    // (down+up at one point, the claim 9367 shape); `d`/`2`
                    // = press-and-hold (down); `u`/`3` = release (up);
                    // `rc` = right-click (buttons bit1 down+up, M124d's
                    // context-menu leg). The WMS5 drag choreography is
                    // `<title>,d ; x1,y1 ; x2,y2 ; ...,u` — held moves
                    // between down and up.
                    let mode: CustomVirtioSpike.PtrStep.Mode
                    if fields.count >= 3 && (fields[2] == "d" || fields[2] == "2") {
                        mode = .down
                    } else if fields.count >= 3 && (fields[2] == "u" || fields[2] == "3") {
                        mode = .up
                    } else if fields.count >= 3 && (fields[2] == "c" || fields[2] == "1") {
                        mode = .click
                    } else if fields.count >= 3 && fields[2] == "rc" {
                        mode = .rightClick
                    } else if fields.count >= 3 && fields[2] == "mc" {
                        mode = .middleClick
                    } else {
                        mode = .move
                    }
                    // Guest pixels -> HID absolute logical (the inverse of
                    // driving_award.map_pointer_axis on the 1280x720 fb).
                    let lx = UInt16(min(32767, Int((gx * 32768.0 / 1280.0).rounded())))
                    let ly = UInt16(min(32767, Int((gy * 32768.0 / 720.0).rounded())))
                    steps.append(CustomVirtioSpike.PtrStep(x: lx, y: ly, mode: mode))
                }
                if !steps.isEmpty {
                    FileHandle.standardOutput.write(Data("PTR-CV-SEQ: \(steps.count) pointer steps scheduled after \"\(marker)\" transport=cv-input\n".utf8))
                    CustomVirtioSpike.injectPointerSteps(steps, sequenceTag: "ptr-seq")
                } else {
                    FileHandle.standardOutput.write(Data("PTR-CV-SEQ: aborted (malformed step) ok=false\n".utf8))
                }
                sent = true
                break
                #else
                fail("--pointer-virtio requires the SPIKE build (zig build spike-virtio).")
                #endif
            }
            Thread.sleep(forTimeInterval: 0.05)
        }
        if !sent {
            FileHandle.standardError.write(Data("ERROR: guest did not emit pointer-virtio marker '\(marker)' within 120s; pointer steps not sent\n".utf8))
        }
    }
}

// Milestone seven card I3 (claim 6050): type the `--input-string` text into
// the VZVirtualMachineView once the marker appears — keyDown + keyUp per
// char (shift for uppercase, `\n` = Enter). VZ has no programmatic keyboard
// API, so each keystroke is a synthesized NSEvent dispatched to the view.
func startKeyStringInject() {
    guard let text = inputString else { return }
    let q = DispatchQueue(label: "virelaios.keyseq")
    q.async {
        let marker = inputStringAfter ?? "virelai> "
        // #2703: timeout-aware deadline, the same M73z (#1638) fix
        // startKeyInject already carries — a gate may anchor the string on a
        // marker deep into a long run (go-dogfood boot 04's
        // `gotabwm: tab close id=` lands ~160 s in, after the demo's
        // auto-close and the state-ask drain on silent apps), and a marker
        // can never arrive after --timeout. The 40 s floor keeps every
        // short-timeout session's window unchanged.
        let waitSeconds = inputMarkerWaitSeconds(sessionTimeout: timeout)
        let waitDeadline = Date().addingTimeInterval(waitSeconds)
        var sent = false
        while Date() < waitDeadline {
            if let log = try? String(contentsOf: serialURL, encoding: .utf8), log.contains(marker) {
                Thread.sleep(forTimeInterval: 0.5) // let the armed ring settle
                #if SPIKE
                if viaVirtioEnabled {
                    // Claim 9588: type through the custom-virtio INPUT queue
                    // — keyDown + keyUp per char as HID reports, no view, no
                    // window, headless-safe.
                    guard #available(macOS 27.0, *) else {
                        fail("--via-virtio requires macOS 27 (VZCustomVirtioDevice).")
                    }
                    var strokes: [CustomVirtioSpike.HidStroke] = []
                    for ch in text {
                        guard let (usage, shift) = CustomVirtioSpike.hidUsage(for: ch) else {
                            FileHandle.standardError.write(Data("ERROR: --via-virtio --input-string: no HID usage for '\(ch)'\n".utf8))
                            FileHandle.standardOutput.write(Data("KEY-SEQ: aborted (no HID usage) ok=false\n".utf8))
                            return
                        }
                        let mods: UInt8 = shift ? CustomVirtioSpike.hidModShift : 0
                        strokes.append(CustomVirtioSpike.HidStroke(mods: mods, usage: usage, isDown: true))
                        strokes.append(CustomVirtioSpike.HidStroke(mods: 0, usage: 0, isDown: false))
                    }
                    FileHandle.standardOutput.write(Data("KEY-SEQ: typed \(text.debugDescription) over the custom-virtio INPUT queue after \"\(marker)\" transport=cv-input\n".utf8))
                    CustomVirtioSpike.injectStrokes(strokes, sequenceTag: "key-seq")
                    sent = true
                    break
                }
                #endif
                guard let view = machineView else {
                    FileHandle.standardError.write(Data("ERROR: --input-string needs --display/--screenshot (no VZVirtualMachineView)\n".utf8))
                    return
                }
                let windowNumber = view.window?.windowNumber ?? 0
                var allOk = true
                // Resolve every char up front (so a missing keycode aborts
                // before any event fires), then schedule the keyDown/keyUp
                // pairs on the MAIN queue with strictly increasing delays.
                // Each event is delivered via the view on the main thread;
                // the timing lives in asyncAfter, not in a background
                // Thread.sleep, so the run loop pumps normally between
                // events and no event is coalesced or dropped. The 2 s
                // spacing is deliberate: VZ's keyboard delivers reports at
                // roughly one per full-frame Road Pops present, so typing
                // faster drops reports (observed claim-time).
                var events: [(type: NSEvent.EventType, code: UInt16, mods: NSEvent.ModifierFlags, chars: String)] = []
                for ch in text {
                    guard let (code, shift) = macKey(for: ch) else {
                        FileHandle.standardError.write(Data("ERROR: --input-string: no macOS keycode for '\(ch)'\n".utf8))
                        allOk = false
                        break
                    }
                    let mods: NSEvent.ModifierFlags = shift ? .shift : []
                    let chars = String(ch)
                    events.append((.keyDown, code, mods, chars))
                    events.append((.keyUp, code, mods, chars))
                }
                if allOk {
                    var delay: Double = 0.0
                    for (i, ev) in events.enumerated() {
                        let evt = ev
                        DispatchQueue.main.asyncAfter(deadline: .now() + delay) {
                            if i == 0, let view = machineView {
                                // Claim 8844: report the delivery-relevant
                                // window state BEFORE the first keystroke
                                // lands (main thread) — a passing run shows
                                // key=true; a walled run shows key=false
                                // immediately, instead of a silent events=0.
                                reportKeyboardKeyState(view, label: "KEY-SEQ")
                            }
                            let t = ProcessInfo.processInfo.systemUptime
                            // VZ maps keyDown/keyUp by keyCode; keep the
                            // characters on both so the pair is symmetric.
                            let chars = evt.chars
                            if let e = NSEvent.keyEvent(with: evt.type, location: .zero, modifierFlags: evt.mods, timestamp: t, windowNumber: windowNumber, context: nil, characters: chars, charactersIgnoringModifiers: chars, isARepeat: false, keyCode: evt.code) {
                                if evt.type == .keyDown {
                                    view.keyDown(with: e)
                                } else {
                                    view.keyUp(with: e)
                                }
                            }
                        }
                        delay += 2.0
                    }
                    FileHandle.standardOutput.write(Data("KEY-SEQ: typed \(text.debugDescription) into the VZVirtualMachineView after \"\(marker)\" ok=true\n".utf8))
                } else {
                    FileHandle.standardOutput.write(Data("KEY-SEQ: aborted (missing keycode) ok=false\n".utf8))
                }
                sent = true
                break
            }
            Thread.sleep(forTimeInterval: 0.1)
        }
        if !sent {
            FileHandle.standardError.write(Data("ERROR: guest did not emit key-seq marker '\(marker)' within \(Int(waitSeconds))s; string not typed\n".utf8))
        }
    }
}

/// Forward `path` into the serial attachment exactly once, after `after`
/// appears in the serial log (default: the kernel terminal state). Shared
/// by the primary script (claim 6684) and the second phase (claim 4613).
///
/// Issue #843 (Shape A): every forward step is logged distinctly so a
/// script-phase delivery stall self-diagnoses at the host — "marker seen"
/// is logged SEPARATELY from "bytes written", and a post-write pipe
/// residual check distinguishes "VZ never consumed the bytes" (residual
/// bytes still queued in the serial input pipe) from "bytes left the pipe"
/// (delivered to VZ/guest — a guest RX or shell-processing stall).
func forwardScriptOnce(path: String, after: String?, label: String, settle: Double) {
    let q = DispatchQueue(label: "virelaios.\(label)")
    q.async {
        let scriptData: Data
        do {
            scriptData = try Data(contentsOf: URL(fileURLWithPath: path))
        } catch {
            FileHandle.standardError.write(Data("ERROR: could not read script file '\(path)': \(error)\n".utf8))
            exit(1)
        }
        let marker = after ?? "kernel terminal state"
        // Card N9 (claim 9489): a configured settle (--script2-delay /
        // --script3-delay) is a wall-clock wait the gate WANTS, so the
        // marker-wait extends with it — the marker of a later phase
        // legitimately appears only after the earlier phases' delays have
        // elapsed. The default 40 s is unchanged for every existing gate.
        //
        // Card U2 (claim 0142): the bound also extends to the session
        // timeout. A phase-2 marker can legitimately appear a long way in
        // when phase 1 is slow — the U2 gate's `u2done` lands only after ~24
        // synthesized keystrokes at 3 s each — and a marker can never arrive
        // after the VM is gone, so `--timeout` is the honest ceiling. This
        // only ever widens the wait, and only for a session that asked for a
        // longer timeout than the old fixed floor.
        let waitSeconds = max(max(40, settle + 60), timeout)
        let waitDeadline = Date().addingTimeInterval(waitSeconds)
        let started = Date()
        FileHandle.standardOutput.write(Data("\(label): waiting for marker \"\(marker)\" (script \(scriptData.count) bytes, settle \(settle)s, deadline \(Int(waitSeconds))s)\n".utf8))
        var sent = false
        var readFailures = 0
        while Date() < waitDeadline {
            if let text = try? String(contentsOf: serialURL, encoding: .utf8) {
                if text.contains(marker) {
                    FileHandle.standardOutput.write(Data("\(label): marker \"\(marker)\" seen at t=\(String(format: "%.1f", Date().timeIntervalSince(started)))s (serial log \(text.count) bytes) — settle \(settle)s, then forward\n".utf8))
                    Thread.sleep(forTimeInterval: settle)
                    let writeStart = Date()
                    do { try consoleInputPipe.fileHandleForWriting.write(contentsOf: scriptData) }
                    catch {
                        FileHandle.standardError.write(Data("ERROR: could not forward script to the guest serial attachment: \(error)\n".utf8))
                    }
                    let writeSecs = Date().timeIntervalSince(writeStart)
                    FileHandle.standardOutput.write(Data("\(label): forwarded \(scriptData.count) bytes to the serial attachment (write took \(String(format: "%.3f", writeSecs))s)\n".utf8))
                    if writeSecs > 1.0 {
                        FileHandle.standardError.write(Data("WARNING: \(label): serial-input write blocked \(String(format: "%.3f", writeSecs))s — the pipe was backed up (VZ not draining the serial attachment fast enough)\n".utf8))
                    }
                    // Issue #843 Shape A: after a grace period, check whether
                    // the forwarded bytes are STILL queued in the serial input
                    // pipe (unread by VZ's serial attachment). poll() on the
                    // READ end: POLLIN set = data pending = VZ never consumed
                    // it — the host→guest delivery itself stalled. POLLIN
                    // clear = the bytes left the pipe and any stall is in VZ's
                    // delivery or the guest's RX/shell path, not this write.
                    Thread.sleep(forTimeInterval: 2.0)
                    var pfd = pollfd(fd: consoleInputPipe.fileHandleForReading.fileDescriptor, events: Int16(POLLIN), revents: 0)
                    let pr = poll(&pfd, 1, 0)
                    if pr > 0 && (pfd.revents & Int16(POLLIN)) != 0 {
                        FileHandle.standardError.write(Data("WARNING: \(label): \(scriptData.count) forwarded bytes STILL queued in the serial input pipe 2s after the write — the VZ serial attachment did not consume them (host→guest RX stall)\n".utf8))
                    } else {
                        FileHandle.standardOutput.write(Data("\(label): serial input pipe drained (bytes left the host pipe — any stall is VZ delivery or guest RX)\n".utf8))
                    }
                    sent = true
                    break
                }
            } else {
                readFailures += 1
            }
            Thread.sleep(forTimeInterval: 0.5)
        }
        if readFailures > 0 {
            FileHandle.standardError.write(Data("WARNING: \(label): \(readFailures) serial-log read(s) failed while polling for the marker\n".utf8))
        }
        if !sent {
            FileHandle.standardError.write(Data("ERROR: guest did not emit \(label)-after marker '\(marker)' within \(Int(waitSeconds))s; script input not sent\n".utf8))
        }
    }
}
