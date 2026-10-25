// a11y.go — M197d (#3416): the calculator publishes its #a11y v1 outline.
//
// The M177b seam (user/go/appkit/a11y.go, the GODEMO precedent) stages the
// window's control tree at /host/A11Y.<win> so GOA11Y reads the focused
// window's structure. The calc's controls are the display line and the
// pad: the entry textfield carries the focus verdict because typed keys
// land on the engine, and every live cell of the active pad is a button
// node labelled by its own glyph — the decimal pad's twenty, the
// programmer pad's thirty-four. D2 keeps the contents out: the display's
// name is its label, never the expression or the result.
package main

import "virelai/appkit"

// outline assembles the window's node list: the display — the surface the
// keyboard actually edits — then the pad's labelled cells row-major, the
// same order the pointer scans them. A mode change rewrites the pad, so
// the deduped publish re-describes it.
func (a *app) outline() []appkit.Node {
	nodes := []appkit.Node{
		{Role: appkit.RoleWindow, Name: appTitle},
		{Depth: 1, Role: appkit.RoleTextField,
			State: appkit.StateFocused, Name: "display"},
	}
	rows, cols := padDims(a.eng.prog)
	for i := 0; i < rows*cols; i++ {
		if label := padAt(a.eng.prog, i); label != "" {
			nodes = append(nodes, appkit.Node{Depth: 1,
				Role: appkit.RoleButton, Name: label})
		}
	}
	return nodes
}

// publishOutline stages the window's outline at the rendezvous when the
// encoded bytes moved — a mode toggle republishes, a digit press costs a
// compare, never a write.
func (a *app) publishOutline() {
	if a.ta == nil {
		return
	}
	nodes := a.outline()
	enc := string(appkit.EncodeOutline(nodes))
	if enc == a.a11yLast {
		return
	}
	if appkit.PublishOutline(uint32(a.ta.Win), nodes) {
		a.a11yLast = enc
	}
}

// unpublishOutline drops the staged outline on a clean exit — dead data
// to a reader either way (kind 16 only reads the focused window's file),
// but a graceful close leaves the share honest.
func (a *app) unpublishOutline() {
	if a.ta == nil {
		return
	}
	_ = appkit.UnpublishOutline(uint32(a.ta.Win))
	a.a11yLast = ""
}
