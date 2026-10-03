import AppKit
import XCTest
import VMAppKit

/// Hidden UI seams only: no VM, input posts, activation or class-C claim.
final class HostApplicationTests: XCTestCase {
    private final class GuestView: NSView {
        override var acceptsFirstResponder: Bool { true }
    }

    func testStartupRunningFocusAndFailurePresentation() throws {
        let host = HostApplication(serialLog: "/fixture/serial.log", presentWindow: false)
        let view = GuestView()
        host.attach(view)
        XCTAssertEqual(host.window.title, "VirelaiOS — Starting VM")
        XCTAssertTrue(view.isHidden)
        let status = try XCTUnwrap(host.window.contentView?.subviews.compactMap { $0 as? NSTextField }.first)
        XCTAssertTrue(status.stringValue.contains("/fixture/serial.log"))
        XCTAssertFalse(status.isHidden)
        host.didStart()
        XCTAssertEqual(host.window.title, "VirelaiOS")
        XCTAssertFalse(view.isHidden)
        XCTAssertTrue(status.isHidden)
        XCTAssertTrue(host.window.firstResponder === view)
        host.window.makeFirstResponder(nil)
        host.windowDidBecomeKey(Notification(name: NSWindow.didBecomeKeyNotification))
        XCTAssertTrue(host.window.firstResponder === view)
        host.showFailure("fixture VZ refusal")
        XCTAssertEqual(host.window.title, "VirelaiOS — VM failed")
        XCTAssertTrue(view.isHidden)
        XCTAssertFalse(host.window.firstResponder === view)
        XCTAssertFalse(status.isHidden)
        XCTAssertTrue(status.stringValue.contains("fixture VZ refusal"))
        XCTAssertTrue(status.stringValue.contains("/fixture/serial.log"))
    }

    func testCloseQuitAndLateStartShareOneStopAndKeepWindowUntilCleanup() {
        let host = HostApplication(serialLog: "/fixture/serial.log", presentWindow: false)
        var stops: [String] = []
        host.onStop = { reason, code in
            stops.append(reason)
            XCTAssertEqual(code, 0)
        }
        XCTAssertFalse(host.windowShouldClose(host.window))
        XCTAssertEqual(host.window.title, "VirelaiOS — Stopping VM")
        XCTAssertEqual(host.applicationShouldTerminate(NSApplication.shared), .terminateLater)
        host.didStart()
        XCTAssertEqual(host.window.title, "VirelaiOS — Stopping VM")
        XCTAssertEqual(stops, ["window-close"])
    }

    func testQuitAfterFailureReturnsFailureStatus() {
        let host = HostApplication(serialLog: "/fixture/serial.log", presentWindow: false)
        var stops: [Int32] = []
        host.onStop = { _, code in stops.append(code) }
        host.showFailure("fixture failure")
        XCTAssertEqual(host.applicationShouldTerminate(NSApplication.shared), .terminateLater)
        host.stop("signal", code: 143)
        XCTAssertEqual(stops, [1])
    }
}
