// choose.go — #3776: the bare launch's face. `exec GOSOUND.ELF` with no
// file used to park on a dead-end "no file" window while the record/meter
// faces sat behind argv-only verbs — undiscoverable without knowing the
// verbs. The chooser is the sibling convention (GOREC's windowed view,
// GOSNAP's runWindowed): a bare launch is a person clicking the catalog
// row, so the window offers the app's modes — 'o' the shared FilePicker
// for a file to play (the opens=audio contract's windowed half), 'r' a
// fixed clip through the record run, 'm' the bounded meter, 'q' quit.
//
// The argv verbs are untouched: the arms land on recordRun/meterRun, the
// same windowed runs recordMain/meterMain drive after parsing.
package main

import (
	"virelai/appkit"
	"virelai/vi"
)

const (
	// chooseSecs/chooseRecPath are the fixed clip the chooser's record arm
	// takes — GOREC's viewSeconds/viewPath convention: short enough the
	// window is only briefly busy, published crash-safe per record. The
	// CLI verb keeps its caller-supplied seconds and path.
	chooseSecs    = 5
	chooseRecPath = "/host/GOSOUND/CLIP.WAV"
)

// chooserSel is the mode the bare-launch face armed — the pick the event
// loop returns to main on, which dispatches the shared run. selNone means
// the face is still up.
type chooserSel int

const (
	selNone chooserSel = iota
	selRecord
	selMeter
)

// openPick raises the M124b FilePicker in open mode — the windowed half of
// the app's opens=audio contract: the person picks the file a bare launch
// was never handed. GOVIEW's clamp applies: the plate assumes a roomy
// canvas, so it is fitted inside the window the app actually got.
func (a *app) openPick() {
	a.pick = appkit.NewFilePicker(appkit.PickOpen, "")
	a.pick.Title = "Open audio"
	if a.ta != nil {
		p := &a.pick
		p.R.W = min(p.R.W, int(a.ta.W))
		p.R.H = min(p.R.H, int(a.ta.H))
		p.R.X = max(min(p.R.X, int(a.ta.W)-p.R.W), 0)
		p.R.Y = max(min(p.R.Y, int(a.ta.H)-p.R.H), 0)
	}
	vi.ConsoleLine(markerPick + "open")
}

// pickerDone applies the picker's result: "cancel" gets the cancel receipt
// and the chooser stays; a path is logged, then probed exactly like an
// argv file — the same verdict markers, the same states, so a refused pick
// shows the refused face rather than lying about the file.
func (a *app) pickerDone() {
	res := a.pick.Result
	if res == "cancel" {
		vi.ConsoleLine(markerPick + "cancel")
		return
	}
	vi.ConsoleLine(markerPick + "open " + res)
	a.path, a.name = res, basename(res)
	a.st = a.probe(res)
}
