package main

// #3348: the app's half of the kind-31 region declare — the document
// band is the staged txt zone, and the chrome row and minimap column
// stay out of it. The declared geometry is pinned through the
// declareRegion seam, never a guest kernel.

import (
	"testing"

	"virelai/tabapp"
	"virelai/vi"
)

type regionCall struct {
	winID    uint32
	slot     uint8
	shape    uint64
	x, y     uint16
	w, h     uint16
	title    string
	selfName string
}

func regionFixture(t *testing.T) *[]regionCall {
	t.Helper()
	calls := &[]regionCall{}
	old := declareRegion
	declareRegion = func(winID uint32, slot uint8, shape uint64,
		x, y, w, h uint16, title, selfName string) bool {
		*calls = append(*calls, regionCall{winID, slot, shape, x, y, w, h, title, selfName})
		return true
	}
	t.Cleanup(func() { declareRegion = old })
	return calls
}

func TestDeclareRegionsCoversTheDocumentBand(t *testing.T) {
	calls := regionFixture(t)
	e := &editor{ta: &tabapp.TabApp{Win: 5, W: 1280, H: 720, Name: appName, TabAware: true}}
	e.declareRegions()
	if len(*calls) != 1 {
		t.Fatalf("declare calls = %d, want 1", len(*calls))
	}
	c := (*calls)[0]
	// y=chromeH..h — the document band, full width when no minimap runs.
	if c.winID != 5 || c.slot != 0 || c.shape != vi.CursorTxt ||
		c.x != 0 || c.y != chromeH || c.w != 1280 || c.h != 720-chromeH {
		t.Fatalf("declared = %+v", c)
	}
	if c.selfName != appName {
		t.Fatalf("selfName = %q", c.selfName)
	}
}

func TestDeclareRegionsStopsAtTheMinimap(t *testing.T) {
	calls := regionFixture(t)
	e := &editor{minimap: true,
		ta: &tabapp.TabApp{Win: 5, W: 1280, H: 720, Name: appName, TabAware: true}}
	e.declareRegions()
	if len(*calls) != 1 {
		t.Fatalf("declare calls = %d, want 1", len(*calls))
	}
	if c := (*calls)[0]; c.w != 1280-miniW {
		t.Fatalf("declared w = %d, want %d (the column keeps the arrow)", c.w, 1280-miniW)
	}
}

func TestDeclareRegionsSkipsUnhostedAndThinWindows(t *testing.T) {
	calls := regionFixture(t)
	// Before Init and under a Zig seat (TabAware false) nothing posts —
	// a refused declare would only feed the caller a lie.
	e := &editor{}
	e.declareRegions()
	e.ta = &tabapp.TabApp{Win: 5, W: 1280, H: 720}
	e.declareRegions()
	// A window with no document band has nothing to name.
	e.ta = &tabapp.TabApp{Win: 5, W: 40, H: chromeH, Name: appName, TabAware: true}
	e.declareRegions()
	if len(*calls) != 0 {
		t.Fatalf("declare calls = %d, want 0", len(*calls))
	}
}
