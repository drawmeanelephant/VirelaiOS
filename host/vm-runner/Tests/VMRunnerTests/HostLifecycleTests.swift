import XCTest
import VMAppKit

final class HostLifecycleTests: XCTestCase {
    func testSuccessfulLaunchCloseAndRelaunchAreIndependent() {
        for _ in 0..<2 {
            let lifecycle = HostLifecycle()
            XCTAssertEqual(lifecycle.state, .starting)
            lifecycle.didStart()
            XCTAssertEqual(lifecycle.state, .running)
            XCTAssertTrue(lifecycle.requestStop())
            XCTAssertEqual(lifecycle.state, .stopping)
            XCTAssertFalse(lifecycle.requestStop(code: 130))
            XCTAssertEqual(lifecycle.exitCode, 0)
            lifecycle.didStop()
            XCTAssertEqual(lifecycle.state, .stopped)
            XCTAssertFalse(lifecycle.requestStop())
        }
    }

    func testCloseDuringStartupWinsOverLateCompletion() {
        let lifecycle = HostLifecycle()
        XCTAssertTrue(lifecycle.requestStop())
        lifecycle.didStart()
        lifecycle.fail("late start error")
        XCTAssertEqual(lifecycle.state, .stopping)
        XCTAssertEqual(lifecycle.exitCode, 0)
    }

    func testStartAndRuntimeFailuresRemainVisibleUntilQuit() {
        for started in [false, true] {
            let lifecycle = HostLifecycle()
            if started { lifecycle.didStart() }
            lifecycle.fail("fixture failure")
            XCTAssertEqual(lifecycle.state, .failed("fixture failure"))
            lifecycle.didStart()
            XCTAssertEqual(lifecycle.state, .failed("fixture failure"))
            XCTAssertTrue(lifecycle.requestStop())
            XCTAssertEqual(lifecycle.exitCode, 1)
        }
    }

    func testSignalExitCodeAndSingleStop() {
        let lifecycle = HostLifecycle()
        lifecycle.didStart()
        XCTAssertTrue(lifecycle.requestStop(code: 143))
        XCTAssertFalse(lifecycle.requestStop())
        XCTAssertEqual(lifecycle.exitCode, 143)
    }
}
