import AppKit
import XCTest

import VMAppKit

final class NativeInputTests: XCTestCase {
    func testApplicationLoopActuallyDispatchesQueuedEvents() throws {
        let app = NSApplication.shared
        let policy = app.activationPolicy()
        app.setActivationPolicy(.prohibited) // No window, activation or VM.
        defer { app.setActivationPolicy(policy) }
        var deliveries = 0
        let monitor = try XCTUnwrap(NSEvent.addLocalMonitorForEvents(matching: .applicationDefined) { event in
            if event.data1 == 91 { deliveries += 1 }
            return event
        })
        defer { NSEvent.removeMonitor(monitor) }
        let event = try XCTUnwrap(NSEvent.otherEvent(
            with: .applicationDefined, location: .zero, modifierFlags: [],
            timestamp: 0, windowNumber: 0, context: nil, subtype: 0, data1: 91, data2: 0))
        let wake = try XCTUnwrap(NSEvent.otherEvent(
            with: .applicationDefined, location: .zero, modifierFlags: [],
            timestamp: 0, windowNumber: 0, context: nil, subtype: 0, data1: 0, data2: 0))
        DispatchQueue.main.async { app.postEvent(event, atStart: false) }
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.2) {
            app.stop(nil)
            app.postEvent(wake, atStart: false)
        }
        NativeInput.run(
            display: true, input: true,
            applicationLoop: { app.run() },
            headlessLoop: { RunLoop.main.run(until: Date().addingTimeInterval(0.3)) }
        )
        XCTAssertEqual(deliveries, 1)
    }

    func testNativeInputRequiresBothDisplayAndInput() {
        XCTAssertTrue(NativeInput.enabled(display: true, input: true))
        XCTAssertFalse(NativeInput.enabled(display: true, input: false))
        XCTAssertFalse(NativeInput.enabled(display: false, input: true))
        XCTAssertFalse(NativeInput.enabled(display: false, input: false))
    }

    func testInteractiveSessionDispatchesApplicationLoopOnce() {
        var calls: [String] = []
        NativeInput.run(
            display: true,
            input: true,
            applicationLoop: { calls.append("AppKit") },
            headlessLoop: { calls.append("Foundation") }
        )
        XCTAssertEqual(calls, ["AppKit"])
    }

    func testNoninteractiveModesPreserveFoundationLoop() {
        for (display, input) in [(true, false), (false, true), (false, false)] {
            var calls: [String] = []
            NativeInput.run(
                display: display,
                input: input,
                applicationLoop: { calls.append("AppKit") },
                headlessLoop: { calls.append("Foundation") }
            )
            XCTAssertEqual(calls, ["Foundation"])
        }
    }
}
