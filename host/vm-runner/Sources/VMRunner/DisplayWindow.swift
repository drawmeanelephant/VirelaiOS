// Display + window lane: the AppKit window and
// VZVirtualMachineView, screenshot capture paths, TraceView, and the
// pointer-event delivery the script engine synthesizes.

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


var machineView: VZVirtualMachineView?

var machineWindow: NSWindow?

var captureTimes: [TimeInterval] = [5, 10, 15]

// Milestone six card G1 (claim 6053): create the AppKit window + the
// VZVirtualMachineView the virtio-gpu scanout renders into, whenever the
// gpu device is attached (`--display` and/or `--screenshot`). Shared by
// the evidence path and script mode so a gated run can screenshot the
// guest framebuffer mid-script.
func setupDisplayWindow() {
    guard screenshotPath != nil || displayMode else { return }
    let app = NSApplication.shared
    // Screenshot/headless gates keep the historical accessory policy.
    // Human display+input sessions must finish launching as an AppKit app
    // and dispatch native events, not just run Foundation's VM timers.
    let wantPointer = pointerScript != nil
    let nativeInput = NativeInput.enabled(display: displayMode, input: inputMode)
    if hostSessionMode {
        // The bundled front door already finished launching.
    } else if wantPointer || nativeInput {
        app.setActivationPolicy(.regular)
        // A CLI process must finishLaunching before the window server
        // will grant it activation/key status (unbundled executables skip
        // the normal launch sequence). Policy before finishLaunching.
        app.finishLaunching()
    } else {
        app.setActivationPolicy(.accessory)
    }
    // M114b: the evidence window gets the same resizable + 16:9-locked
    // chrome as the session window, so the fullscreen/capture-keys verbs
    // performed on it are real, not just receipted.
    let window = hostApplication?.window ?? NSWindow(contentRect: NSRect(x: 0, y: 0, width: 640, height: 480), styleMask: [.titled, .resizable], backing: .buffered, defer: false)
    window.contentAspectRatio = NSSize(width: 16, height: 9)
    let view = TraceView(frame: NSRect(x: 0, y: 0, width: 1280, height: 720))
    view.trace = ["direct", "diag", "pid", "drag", "warp", "cg"].contains(pointerRoute)
    view.nativeInput = nativeInput
    view.virtualMachine = runner.vm
    window.setContentSize(NSSize(width: 1280, height: 720))
    if let hostApplication {
        hostApplication.attach(view)
    } else {
        window.contentView = view
    }
    if nativeInput && !hostSessionMode {
        window.title = "VirelaiOS"
        window.makeFirstResponder(view)
    }
    window.center()
    window.acceptsMouseMovedEvents = true
    // AppKit only dispatches mouseMoved to views inside a tracking area;
    // without one, moves posted to the window are dropped at the
    // dispatch layer (a real mouse gets moves via the implicit tracking
    // area of the window's first responder path, but synthesized posts
    // need the explicit area).
    let tracking = NSTrackingArea(
        rect: view.bounds,
        options: [.activeInKeyWindow, .mouseMoved, .mouseEnteredAndExited, .activeAlways],
        owner: view, userInfo: nil)
    view.addTrackingArea(tracking)
    // M108c (#2326, ADR 0051 D4): the drop target lives here, on the display
    // view itself — registered only while the lane is armed, so a flags-off
    // boot shows no affordance.
    if hostDrop != nil {
        view.registerForDraggedTypes([.fileURL])
        view.dropHandler = { urls in hostDrop?.drop(urls: urls) }
    }
    // VIRELAI_WINDOW_BG=1 keeps gate/test windows out of the user's face:
    // the view still exists (scanout, screenshots, drop lanes all work)
    // but the window is ordered behind everything and no activation is
    // attempted. Pointer-script and native-input runs may still want a
    // key window for synthesized key events — the knob is opt-in.
    let windowBackground = ProcessInfo.processInfo.environment["VIRELAI_WINDOW_BG"] != nil
    if windowBackground {
        window.orderBack(nil)
    } else {
        window.orderFrontRegardless()
        window.makeKeyAndOrderFront(nil)
    }
    if hostSessionMode {
        // HostApplication owns activation, focus and close/quit.
    } else if windowBackground {
        pmo("diag window-bg: unfocused run; synthesized key events may not land without key status")
    } else if wantPointer {
        window.level = .normal
        // macOS 14+: the deprecated ignoringOtherApps form is a no-op for
        // unbundled CLI processes; the modern no-arg activate() is the
        // one that can steal focus. Try both.
        NSApp.activate(ignoringOtherApps: true)
        NSApp.activate()
        NSRunningApplication.current.activate(options: [.activateAllWindows, .activateIgnoringOtherApps])
        // Re-assert after the run loop has a chance to complete the
        // activation (async on macOS), so the first synthesized event
        // lands on an already-key window.
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) {
            window.makeKeyAndOrderFront(nil)
            NSApp.activate(ignoringOtherApps: true)
            NSApp.activate()
            NSRunningApplication.current.activate(options: [.activateAllWindows, .activateIgnoringOtherApps])
            pmo("diag key-after-activate=\(window.isKeyWindow) main=\(window.isMainWindow) active=\(NSApp.isActive) visible=\(window.isVisible) onscreen=\(window.isOnActiveSpace)")
        }
    } else if nativeInput {
        app.activate()
    } else {
        app.activate(ignoringOtherApps: true)
    }
    machineView = view
    machineWindow = window
    // M139a: the router's window lookup covers the evidence window too —
    // a menu action lands on the session whose window is key.
    primarySession.view = view
    primarySession.window = window
    // M114b (#2455): --host-capture-keys rides the bus — one dispatch once
    // the view exists, so startup capture lands in the same host-ui:
    // receipt as ⌥⌘K / the toolbar pill / a --host-cmd-after trigger.
    if hostCaptureKeys, machineView?.capturesSystemKeys == false {
        commandBus.dispatch(.captureKeys)
    }
}

// Milestone six card G1 (claim 6053): the 5/10/15 s screenshot capture,
// shared by the evidence poll and scriptPoll so a gated scripted run can
// capture the guest framebuffer. The evidence now comes from
// ScreenCaptureKit — the runner's own window captured by ID and cropped
// to the content area, i.e. the composited pixels exactly as the operator
// sees them in `--display` (title bar excluded via the window's own frame
// geometry). The offscreen `cacheDisplay` render remains as the honest
// fallback when Screen Recording permission is unavailable (TCC not
// granted, headless CI, ...); the printed capture path says which one
// produced the PNG.
func captureScreenshot(at t: TimeInterval) {
    let path: String
    if let base = screenshotPath {
        let dot = (base as NSString).deletingPathExtension
        let ext = (base as NSString).pathExtension
        path = ext.isEmpty ? "\(dot)-\(Int(t))s" : "\(dot)-\(Int(t))s.\(ext)"
    } else {
        path = "artifacts/vm-screen-\(Int(t))s.png"
    }
    writeScreenshot(to: path)
}

// Card G6 set_visible follow-on (claim 0487): a MARKER-driven capture.
// When `--screenshot-after <marker>` is set, the framebuffer is captured
// ONCE the moment the marker appears in the serial log, under a stable
// `-<label>` name (deterministic — the fixed 5/10/15 s captures cannot
// guarantee a capture lands inside an EL0 hide/show window).
// The `-<label>` path naming shared by --screenshot-after and the bus's
// screenshot verb (M114a): <base>-<label>[.<ext>], or artifacts/ when no
// --screen base was given.
func screenshotPathForLabel(_ label: String) -> String {
    if let base = screenshotPath {
        let dot = (base as NSString).deletingPathExtension
        let ext = (base as NSString).pathExtension
        return ext.isEmpty ? "\(dot)-\(label)" : "\(dot)-\(label).\(ext)"
    }
    return "artifacts/vm-screen-\(label).png"
}

// M120e (#2562): the ⌘⇧5 verb's destination — `<share>/CAPTURES/` of the
// armed share or the active profile (the recorder's own destination), with
// the --screen basename + ext kept so a run's naming survives the reroute.
// A shareless run keeps the historical <base>-<label>[.<ext>] / artifacts/
// path — the card promises CAPTURES only where a share exists.
func screenshotPathForCapture(_ label: String) -> String {
    guard let dir = commandBus.captures.resolvedDir() else {
        return screenshotPathForLabel(label)
    }
    try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    let screen = (screenshotPath ?? "") as NSString
    let stem = (screen.deletingPathExtension as NSString).lastPathComponent
    let ext = screen.pathExtension
    let name = "\(stem.isEmpty ? "vm-screen" : stem)-\(label).\(ext.isEmpty ? "png" : ext)"
    return dir.appendingPathComponent(name).path
}

func captureScreenshotMarker(_ label: String) {
    writeScreenshot(to: screenshotPathForLabel(label))
}

// Shared capture body (ScreenCaptureKit first, cacheDisplay fallback):
// render the composited content area to a PNG and write it to `path`.
func writeScreenshot(to path: String) {
    var png: Data?
    if let img = screenCaptureKitScreenshot() {
        let rep = NSBitmapImageRep(cgImage: img)
        if let data = rep.representation(using: .png, properties: [:]) {
            png = data
            print("  capture path: ScreenCaptureKit (composited window, \(img.width)x\(img.height) px)")
        }
    }
    if png == nil, let view = machineView,
       let rep = view.bitmapImageRepForCachingDisplay(in: view.bounds) {
        view.cacheDisplay(in: view.bounds, to: rep)
        png = rep.representation(using: .png, properties: [:]) ?? png
        print("  capture path: cacheDisplay fallback (Screen Recording permission unavailable)")
    }
    guard let data = png else { return }
    do {
        try data.write(to: URL(fileURLWithPath: path))
        print("SUCCESS: framebuffer screenshot saved to \(path) (\(data.count) bytes).")
        screenshotSaved = true
    } catch { print("WARNING: could not write screenshot: \(error)") }
}

// ScreenCaptureKit path: capture OUR window by ID via a window content
// filter (SCShareableContent -> SCContentFilter(display:includingWindows:))
// and crop the title bar off, so the evidence is the composited content
// area exactly as the operator sees it in `--display`. No display-space
// coordinate math is involved — the filter captures the window and the
// title bar height comes from the window's own frame geometry. Screen
// Recording permission is required; the caller falls back to cacheDisplay
// when the capture fails.
func screenCaptureKitScreenshot() -> CGImage? {
    guard let window = machineWindow else { return nil }
    let windowID = CGWindowID(window.windowNumber)
    guard windowID > 0 else { return nil }

    let sem = DispatchSemaphore(value: 0)
    var scWindow: SCWindow?
    var display: SCDisplay?
    SCShareableContent.getExcludingDesktopWindows(false, onScreenWindowsOnly: true) { content, error in
        if let error {
            print("WARNING: SCShareableContent failed: \(error)")
        } else if let content {
            scWindow = content.windows.first { $0.windowID == windowID }
            display = content.displays.first
            if scWindow == nil {
                print("WARNING: window \(windowID) not in SCK shareable content (off-screen?)")
            }
        }
        sem.signal()
    }
    if sem.wait(timeout: .now() + 5) == .timedOut {
        print("WARNING: SCShareableContent timed out")
        return nil
    }
    guard let scWindow, let display else { return nil }

    let filter = SCContentFilter(display: display, including: [scWindow])
    let config = SCScreenshotConfiguration()
    config.showsCursor = false
    config.ignoreShadows = true

    var image: CGImage?
    let sem2 = DispatchSemaphore(value: 0)
    SCScreenshotManager.captureScreenshot(contentFilter: filter, configuration: config) { output, error in
        if let error {
            print("WARNING: ScreenCaptureKit capture failed: \(error)")
        } else {
            image = output?.sdrImage
        }
        sem2.signal()
    }
    if sem2.wait(timeout: .now() + 5) == .timedOut {
        print("WARNING: ScreenCaptureKit capture timed out")
        return nil
    }
    guard let full = image else { return nil }

    // The window capture includes the title bar (the frame above the
    // content view). Crop it off using the window's own geometry so the
    // evidence is exactly the view's content area (what cacheDisplay
    // produced before, so the gate pixel math is unchanged).
    let titleBar = window.frame.height - window.contentLayoutRect.height
    let scale = filter.pointPixelScale > 0 ? CGFloat(filter.pointPixelScale) : (window.screen?.backingScaleFactor ?? 2.0)
    let crop = CGRect(x: 0,
                      y: titleBar * scale,
                      width: window.contentLayoutRect.width * scale,
                      height: window.contentLayoutRect.height * scale)
    print("  sck debug: window=\(windowID) raw=\(full.width)x\(full.height) scale=\(scale) titleBar=\(titleBar) crop=\(NSStringFromRect(crop))")
    guard crop.width > 0, crop.height > 0,
          crop.minX >= 0, crop.minY >= 0,
          crop.maxX <= Double(full.width), crop.maxY <= Double(full.height),
          let cropped = full.cropping(to: crop) else {
        return full
    }
    return cropped
}

// Issue #179 follow-up (claim 8844): the activation wall (claim 4769) —
// VZ's synthesized-input translation is session-dependent (macOS 14+
// refuses programmatic focus-stealing while another app holds focus), so
// a dispatch into the view reports ok=true whether or not a single
// keystroke reaches the guest; a silent drop used to surface only as
// guest `events=0`. Report the delivery-relevant window state on every
// synthesized-keyboard path so a failing run is diagnosed at the host
// instead of guessed at. Observed 2026-08-18: a not-key window at the
// FIRST keystroke is consistent with BOTH events=0 (walled run) and a
// full events=6 delivery (the wall lifted mid-sequence) — so this report
// is evidence, not a prediction. Must be called on the main thread.
func reportKeyboardKeyState(_ view: VZVirtualMachineView, label: String) {
    let w = view.window
    let key = w?.isKeyWindow ?? false
    let main = w?.isMainWindow ?? false
    let active = NSApp.isActive
    if key {
        FileHandle.standardOutput.write(Data("\(label): window key=\(key) main=\(main) active=\(active) — key window, input should translate\n".utf8))
    } else {
        FileHandle.standardOutput.write(Data("\(label): window key=\(key) main=\(main) active=\(active) — the claim-4769 activation wall may be holding (VZ synthesized-keyboard delivery is session-dependent: the guest can report events=0 OR translate normally; observed both on 2026-08-18). If this run shows guest events=0, re-run when the machine is idle or after a fresh login; no code fix exists (VZ has no programmatic keyboard API).\n".utf8))
    }
}

// Milestone eight cards U4/U5 (claims 4993/0935): the pointer-synthesis
// seam. Once the marker appears, each "<x>,<y>[,c]" step synthesizes one
// mouseMoved NSEvent (plus a left mouseDown/mouseUp pair when the click
// flag 'c' is set) and dispatches it to the VZVirtualMachineView — VZ has
// no programmatic pointer API, exactly like the I3 keyboard seam. 3 s per
// step; the guest's pointer reports ride the same single-TRB interrupt-IN
// arming as the keyboard.
// Pointer-route probe (claim 4769): a VZVirtualMachineView subclass that
// logs which NSResponder mouse methods actually fire, so a probe run can
// tell "the event never reached the view" from "VZ dropped it internally".
final class TraceView: VZVirtualMachineView {
    var trace = false
    var nativeInput = false
    /// M108c (#2326, ADR 0051 D4): the file-drop surface. Set only when
    /// --host-drop armed the lane AND this view registered .fileURL — a
    /// flags-off view never advertises a drop affordance.
    var dropHandler: (([URL]) -> Void)?
    private var nativeKeyLogged = false
    private var nativeClickLogged = false

    private func reportNativeInput(_ kind: String) {
        // Bounded receipts, never key characters or document contents.
        print("input: host-view \(kind) key=\(window?.isKeyWindow ?? false) responder=\(window?.firstResponder === self) active=\(NSApp.isActive)")
        fflush(stdout)
    }

    override func keyDown(with event: NSEvent) {
        if nativeInput && !nativeKeyLogged {
            nativeKeyLogged = true
            reportNativeInput("keyDown")
        }
        super.keyDown(with: event)
    }

    // macOS click-through: the first click on an INACTIVE app's window is
    // swallowed to activate the app and never reaches the view. Synthesized
    // posts hit exactly this wall (the app can never become active from a
    // CLI process). Accepting the first mouse makes the click deliver
    // anyway, so the posted event reaches the VZ view's responder methods
    // regardless of activation state.
    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
    override func mouseMoved(with event: NSEvent) {
        if trace { pmo("TRACE mouseMoved inW=(\(event.locationInWindow))") }
        super.mouseMoved(with: event)
    }
    override func mouseDragged(with event: NSEvent) {
        if trace { pmo("TRACE mouseDragged inW=(\(event.locationInWindow))") }
        super.mouseDragged(with: event)
    }
    override func mouseDown(with event: NSEvent) {
        if nativeInput && !nativeClickLogged {
            nativeClickLogged = true
            reportNativeInput("mouseDown")
        }
        if trace { pmo("TRACE mouseDown inW=(\(event.locationInWindow)) key=\(event.window?.isKeyWindow ?? false)") }
        super.mouseDown(with: event)
    }
    override func mouseUp(with event: NSEvent) {
        if trace { pmo("TRACE mouseUp inW=(\(event.locationInWindow)) key=\(event.window?.isKeyWindow ?? false)") }
        super.mouseUp(with: event)
    }
    override func mouseEntered(with event: NSEvent) {
        if trace { pmo("TRACE mouseEntered") }
        super.mouseEntered(with: event)
    }

    // NSDraggingDestination: the drop lands ONLY while the lane armed this
    // surface — the copy cursor is the affordance, and the URLs go through
    // the one handler --host-drop-file also calls (ADR 0051 D4's "same
    // function" rule).
    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        guard dropHandler != nil else { return [] }
        return .copy
    }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        guard let handler = dropHandler else { return false }
        let urls = sender.draggingPasteboard.readObjects(
            forClasses: [NSURL.self],
            options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
        handler(urls)
        return true
    }
}

// M114b (#2455): the session window's capture-keys verb toggles the view
// through the HostKeyCapturing seam — VMAppKit stays Virtualization-free,
// so the retroactive conformance lives here where Virtualization is
// already imported. `capturesSystemKeys` is inherited from
// VZVirtualMachineView and satisfies the protocol as-is.
extension TraceView: HostKeyCapturing {}

func pmo(_ s: String) {
    FileHandle.standardOutput.write(Data("PTR-TRACE: \(s)\n".utf8))
}

/// Deliver one synthesized pointer NSEvent to the VZ view over the
/// configured route (see --pointer-route). The "cg" route re-posts a real
/// CGEvent at the HID tap in GLOBAL screen coordinates — the OS delivers
/// it to the key window exactly like a physical mouse. The "pid" route
/// posts the SAME CGEvent straight into OUR OWN application queue
/// (CGEventPostToPid needs no frontmost window — it is hit-tested against
/// our key window, i.e. the VZ window). The "direct" route calls the
/// NSResponder mouse methods VZ implements directly on the view — the
/// exact pattern the keyboard seam uses. The claim-4769 probe also added
/// "warp" (warp the real cursor over the view, then trusted HID-tap
/// post), "diag" (force key + first responder, then sendEvent), and
/// "drag" (moves as leftMouseDragged). All of them hit the same
/// activation wall: VZ only translates for its KEY window, and macOS 14+
/// refuses programmatic focus-stealing from a background process — see
/// claim 4769 (historical — in git history).
func deliverPointerEvent(_ view: VZVirtualMachineView, _ e: NSEvent) {
    switch pointerRoute {
    case "direct":
        switch e.type {
        case .leftMouseDown: view.mouseDown(with: e)
        case .leftMouseUp: view.mouseUp(with: e)
        default: view.mouseMoved(with: e)
        }
    case "pid":
        if let w = view.window {
            let glob = w.convertToScreen(NSRect(x: e.locationInWindow.x, y: e.locationInWindow.y, width: 1, height: 1)).origin
            guard let screen = NSScreen.main else { return }
            let cgPt = CGPoint(x: glob.x, y: screen.frame.maxY - glob.y)
            let src = CGEventSource(stateID: .hidSystemState)
            let type: CGEventType = e.type == .leftMouseDown ? .leftMouseDown : (e.type == .leftMouseUp ? .leftMouseUp : .mouseMoved)
            if let cg = CGEvent(mouseEventSource: src, mouseType: type, mouseCursorPosition: cgPt, mouseButton: .left) {
                pmo("pid-post \(type) at (\(Int(cgPt.x)),\(Int(cgPt.y)))")
                cg.postToPid(pid_t(getpid()))
            }
        }
    case "warp":
        // Physical-mouse precondition: the REAL cursor must be over the
        // view for hit-tested delivery. Warp it into the window's center
        // (no Accessibility needed for the warp), then post the event via
        // the HID tap (trust needed; checked per post). If VZ tracks the
        // global cursor location rather than the event field, this is the
        // missing piece.
        if let w = view.window {
            let center = w.convertToScreen(NSRect(x: w.contentView!.bounds.midX, y: w.contentView!.bounds.midY, width: 1, height: 1)).origin
            guard let screen = NSScreen.main else { return }
            CGWarpMouseCursorPosition(CGPoint(x: center.x, y: screen.frame.maxY - center.y))
            pmo("warped cursor to (\(Int(center.x)),\(Int(screen.frame.maxY - center.y)))")
            if CGPreflightPostEventAccess() {
                let local = e.locationInWindow
                let glob = w.convertToScreen(NSRect(x: local.x, y: local.y, width: 1, height: 1)).origin
                let cgPt = CGPoint(x: glob.x, y: screen.frame.maxY - glob.y)
                let src = CGEventSource(stateID: .hidSystemState)
                let type: CGEventType = e.type == .leftMouseDown ? .leftMouseDown : (e.type == .leftMouseUp ? .leftMouseUp : .mouseMoved)
                if let cg = CGEvent(mouseEventSource: src, mouseType: type, mouseCursorPosition: cgPt, mouseButton: .left) {
                    cg.post(tap: .cghidEventTap)
                }
            } else {
                pmo("warp skipped post (untrusted)")
            }
        }
    case "app":
        NSApp.postEvent(e, atStart: true)
    case "diag":
        // Route-minus-buried-context probe: force the VZ window to be key,
        // frontmost, and first responder (the one thing physical-mouse
        // users have and synthesized routes may lack), then sendEvent.
        if let w = view.window {
            w.makeKeyAndOrderFront(nil)
            w.makeFirstResponder(view)
            w.acceptsMouseMovedEvents = true
            NSApp.activate(ignoringOtherApps: true)
            pmo("diag key=\(w.isKeyWindow) main=\(w.isMainWindow) fr=\(w.firstResponder === view)")
            w.sendEvent(e)
        }
    case "drag":
        // VZ's pointer tracking may bind on mouseDragged (the path a real
        // drag takes) rather than mouseMoved. Convert moves into
        // leftMouseDragged events (button state down), delivered to the
        // window like a real drag sequence.
        if e.type == .mouseMoved {
            let loc = e.locationInWindow
            let t = ProcessInfo.processInfo.systemUptime
            if let drag = NSEvent.mouseEvent(with: .leftMouseDragged, location: loc, modifierFlags: [], timestamp: t, windowNumber: e.windowNumber, context: nil, eventNumber: 1, clickCount: 1, pressure: 1.0) {
                if let w = view.window {
                    w.sendEvent(drag)
                } else {
                    view.mouseDragged(with: drag)
                }
            }
        } else if let w = view.window {
            w.sendEvent(e)
        } else {
            view.mouseMoved(with: e)
        }
    case "cg":
        if let w = view.window {
            // Accessibility trust is the gating permission: without it the
            // HID-tap post is silently dropped (the claim-4993 observation).
            // Report the truth once so a gate can distinguish "untrusted"
            // from "trusted but no report".
            let trusted = CGPreflightPostEventAccess()
            if !trusted {
                FileHandle.standardOutput.write(Data("PTR-TRUST: untrusted (cg route needs Accessibility for the terminal) skipped-post\n".utf8))
                return
            }
            // Window-local (bottom-left) -> global AppKit -> CG (top-left).
            let local = e.locationInWindow
            let glob = w.convertToScreen(NSRect(x: local.x, y: local.y, width: 1, height: 1)).origin
            guard let screen = NSScreen.main else { return }
            let cgPt = CGPoint(x: glob.x, y: screen.frame.maxY - glob.y)
            let src = CGEventSource(stateID: .hidSystemState)
            let type: CGEventType = e.type == .leftMouseDown ? .leftMouseDown : (e.type == .leftMouseUp ? .leftMouseUp : .mouseMoved)
            let cg = CGEvent(mouseEventSource: src, mouseType: type, mouseCursorPosition: cgPt, mouseButton: .left)
            cg?.post(tap: .cghidEventTap)
            pmo("cg-post \(type) at (\(Int(cgPt.x)),\(Int(cgPt.y))) key=\(w.isKeyWindow) active=\(NSApp.isActive) front=\(NSWorkspace.shared.frontmostApplication?.localizedName ?? "nil") cursor=\(Int(NSEvent.mouseLocation.x)),\(Int(NSEvent.mouseLocation.y))")
        }
    default: // "window"
        if let w = view.window {
            w.sendEvent(e)
        } else {
            view.mouseMoved(with: e)
        }
    }
}
