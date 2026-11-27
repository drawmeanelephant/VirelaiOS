import AppKit
import Foundation
import XCTest

@testable import VMAppKit
@testable import VMRunner

// M216k (#3950): class-A pins for the `--pointer-virtio` wheel step grammar,
// the kind-5 16-byte wire encoder, and the live AppKit scroll-wheel accumulator.
final class PointerVirtioWheelTests: XCTestCase {
    func testParsePointerAndWheelSteps() throws {
        let res = parsePointerVirtioSteps("640,360;640,360,c;100,20,d;200,40,u;50,50,rc;60,60,mc;640,360,w+3;640,360,w-2;640,360,w5;320,10,h+4;320,10,h-1")
        let steps = try res.get()
        XCTAssertEqual(steps.count, 11)
        XCTAssertEqual(steps[0].mode, .move)
        XCTAssertEqual(steps[1].mode, .click)
        XCTAssertEqual(steps[2].mode, .down)
        XCTAssertEqual(steps[3].mode, .up)
        XCTAssertEqual(steps[4].mode, .rightClick)
        XCTAssertEqual(steps[5].mode, .middleClick)
        XCTAssertEqual(steps[6], PointerVirtioStep(x: 16384, y: 16384, mode: .wheel(dx: 0, dy: 3)))
        XCTAssertEqual(steps[7], PointerVirtioStep(x: 16384, y: 16384, mode: .wheel(dx: 0, dy: -2)))
        XCTAssertEqual(steps[8], PointerVirtioStep(x: 16384, y: 16384, mode: .wheel(dx: 0, dy: 5)))
        XCTAssertEqual(steps[9].mode, .wheel(dx: 4, dy: 0))
        XCTAssertEqual(steps[10].mode, .wheel(dx: -1, dy: 0))
    }

    func testParseRejectsMalformedWheelAndUnknownModes() {
        let bad = [
            "",
            "640,360,w0",
            "640,360,h0",
            "640,360,w",
            "640,360,h",
            "640,360,w8192",
            "640,360,w-8192",
            "640,360,h9000",
            "640,360,wabc",
            "640,360,bogus",
            "640,360,c,extra",
            "1281,360,w+1",
            "640,721,w+1",
            "-1,100,w+1",
            "640,360,w+1;",
        ]
        for s in bad {
            switch parsePointerVirtioSteps(s) {
            case .success(let steps):
                XCTFail("expected '\(s)' to fail parsing, got \(steps)")
            case .failure:
                break
            }
        }
    }

    func testBuildVirtioWheelMessageWireLayout() {
        // x=0x4000 (16384), y=0x1234 (4660), dy=-3 (0xfffd), dx=+2 (0x0002)
        let msg = buildVirtioWheelMessage(x: 0x4000, y: 0x1234, dx: 2, dy: -3)
        XCTAssertEqual(msg.count, 16)
        XCTAssertEqual(Array(msg), [
            5, 0,       // kind=5, flags=0
            8, 0,       // len=8 LE
            0x00, 0x40, // x LE
            0x34, 0x12, // y LE
            0xfd, 0xff, // dy=-3 LE
            0x02, 0x00, // dx=+2 LE
            0, 0, 0, 0, // trailing zero padding
        ])
    }

    func testHostWheelAccumulatorPreciseAndNotchedAndMomentum() {
        var acc = HostWheelAccumulator()
        // Precise trackpad: negative scrollingDeltaY means scroll down (+dy).
        // Accumulate sub-line deltas until crossing 12 pt -> 1 line down.
        let r1 = acc.consume(deltaX: 0, deltaY: -5.0, hasPreciseDeltas: true, phase: .began, momentumPhase: [])
        XCTAssertEqual(r1.dx, 0)
        XCTAssertEqual(r1.dy, 0)
        let r2 = acc.consume(deltaX: 0, deltaY: -8.0, hasPreciseDeltas: true, phase: .changed, momentumPhase: [])
        XCTAssertEqual(r2.dx, 0)
        XCTAssertEqual(r2.dy, 1) // 13 pt -> 1 line, 1 pt remainder
        let r3 = acc.consume(deltaX: 0, deltaY: -11.0, hasPreciseDeltas: true, phase: .changed, momentumPhase: [])
        XCTAssertEqual(r3.dy, 1) // 1 + 11 = 12 pt -> 1 line, 0 remainder

        // Momentum phase is ignored (out of scope).
        let mom = acc.consume(deltaX: 0, deltaY: -48.0, hasPreciseDeltas: true, phase: [], momentumPhase: .changed)
        XCTAssertEqual(mom.dx, 0)
        XCTAssertEqual(mom.dy, 0)

        // Notched mouse wheel: already in lines; +deltaY means scroll up (-dy).
        let up = acc.consume(deltaX: 0, deltaY: 3.0, hasPreciseDeltas: false, phase: [], momentumPhase: [])
        XCTAssertEqual(up.dx, 0)
        XCTAssertEqual(up.dy, -3)

        // Horizontal: -deltaX means scroll right (+dx).
        let right = acc.consume(deltaX: -2.0, deltaY: 0, hasPreciseDeltas: false, phase: [], momentumPhase: [])
        XCTAssertEqual(right.dx, 2)
        XCTAssertEqual(right.dy, 0)
    }

    func testGuestWheelPointCoordinateConversion() {
        let bounds = NSRect(x: 0, y: 0, width: 1280, height: 720)
        // Non-flipped AppKit view: bottom-left (0, 0) is guest bottom-left (0, 32767).
        let bl = HostApplication.guestWheelPoint(viewPoint: NSPoint(x: 0, y: 0), viewBounds: bounds, isFlipped: false)
        XCTAssertEqual(bl?.x, 0)
        XCTAssertEqual(bl?.y, 32767)
        // Top-left in non-flipped view is (0, 719) -> guest (0, ~46).
        let mid = HostApplication.guestWheelPoint(viewPoint: NSPoint(x: 640, y: 360), viewBounds: bounds, isFlipped: false)
        XCTAssertEqual(mid?.x, 16384)
        XCTAssertEqual(mid?.y, 16384)
        // Outside view bounds -> nil.
        XCTAssertNil(HostApplication.guestWheelPoint(viewPoint: NSPoint(x: -1, y: 100), viewBounds: bounds, isFlipped: false))
        XCTAssertNil(HostApplication.guestWheelPoint(viewPoint: NSPoint(x: 1281, y: 100), viewBounds: bounds, isFlipped: false))
    }
}
