package main

import (
	"os"
	"testing"

	"virelai/webrender"
)

// The live gate injects a pointer click at a fixed scanout point. This test
// pins the whole chain on the host: the fixture's first link must contain the
// point the gate clicks, expressed in the window-local coordinates the kernel
// actually delivers (the kernel publishes window-local x/y — see
// kernel/src/driving_award.zig pointer_tick).
func TestGateClickHitsTheFixtureLink(t *testing.T) {
	src, err := os.ReadFile("testdata/gate-page.html")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	a := &app{hist: newHistory(), text: browserFonts(t), target: "/host/PAGE.HTML"}
	a.loadBody(src, a.target)
	lay := a.lay
	if len(lay.Links) == 0 {
		t.Fatal("fixture has no links")
	}
	link := lay.Links[0]
	if link.Target != "NEXT.HTML" {
		t.Fatalf("first link target = %q", link.Target)
	}
	// Pixel-center inverse mapping must hit a word, not its inter-word gap.
	gateScanoutX, gateScanoutY := 42, 95
	localX := gateScanoutX - winX
	localY := gateScanoutY - winY
	// The chrome geometry maps a window-local point into content space.
	contentXPoint, contentYPoint, hit := documentPresentation(winW, winH).inverse(localX, localY, 0)
	if !hit {
		t.Fatal("gate point outside the declared presentation")
	}
	if target := webrender.HitTest(lay, contentXPoint, contentYPoint); target != "NEXT.HTML" {
		t.Fatalf("gate click (local %d,%d -> content %d,%d) missed the link; links=%v",
			localX, localY, contentXPoint, contentYPoint, lay.Links)
	}
	// And the link must sit above the fold, so the snapshot shows the page
	// that was clicked.
	if link.Y+link.H > contentH {
		t.Fatalf("link outside the viewport: %+v (contentH=%d)", link, contentH)
	}
}

// The chrome must not overlap the kernel's own window title band: the kernel
// paints over the top 16 rows of every user window, so anything the app draws
// there is invisible (observed live).
func TestChromeStartsBelowTheKernelBand(t *testing.T) {
	if titleY < kernelBand {
		t.Fatalf("title band starts at %d, inside the kernel's %d-row band", titleY, kernelBand)
	}
	if contentY >= winH-statusH {
		t.Fatalf("content box is empty: contentY=%d winH=%d statusH=%d", contentY, winH, statusH)
	}
	if backY < titleY+titleH {
		t.Fatalf("chips at y=%d overlap the title band", backY)
	}
	// The chips and the URL field must not collide horizontally.
	if urlX < reloadX+chipW {
		t.Fatalf("URL field starts at %d, overlapping the reload chip", urlX)
	}
}
