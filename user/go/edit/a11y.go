// a11y.go — M197d (#3416): the editor publishes its #a11y v1 outline.
//
// The M177b seam (user/go/appkit/a11y.go, the GODEMO precedent) stages the
// window's control tree at /host/A11Y.<win> so GOA11Y reads the focused
// window's structure. The outline is the menubar's own subtree ahead of
// the document surface, plus the modal the mode raised: the find/goto/
// replace bars name their fields, the rarer surfaces one named node
// each. The focus verdict follows the mode — the menubar's subtree
// while it is engaged, the modal's node while it stands, the document
// otherwise. D2 keeps contents out: names are the surface labels and the
// document's base name, never buffer, pattern, or query text.
package main

import "virelai/appkit"

// a11yField is a textfield node's entry: the surface's label plus the
// focus verdict when it owns the keys.
func a11yField(name string, focused bool) appkit.Node {
	n := appkit.Node{Depth: 1, Role: appkit.RoleTextField, Name: name}
	if focused {
		n.State |= appkit.StateFocused
	}
	return n
}

// outline assembles the window's node list in read order — menubar,
// document, then whichever modal the mode raised — with exactly one
// node carrying the focus verdict.
func (e *editor) outline() []appkit.Node {
	nodes := []appkit.Node{{Role: appkit.RoleWindow, Name: baseOf(e.path)}}
	nodes = append(nodes, e.mbar.A11yTree(1, e.mbar.Active)...)
	nodes = append(nodes,
		a11yField("document", e.mode == modeEdit && !e.mbar.Active))
	focus := !e.mbar.Active
	switch e.mode {
	case modeFind:
		nodes = append(nodes, a11yField("find", focus))
	case modeGoto:
		nodes = append(nodes, a11yField("goto line", focus))
	case modeReplace:
		nodes = append(nodes,
			a11yField("find", focus && !e.bar2At),
			a11yField("replace", focus && e.bar2At))
	case modeDiags:
		nodes = append(nodes, a11yField("diagnostics", focus))
	default:
		// The rarer surfaces — picker, dialog, hints, quick-open — own
		// the keys without a field worth naming: one node marks the
		// modal so the document is not left looking focused-less.
		if e.mode != modeEdit {
			nodes = append(nodes, a11yField("modal", focus))
		}
	}
	return nodes
}

// publishOutline stages the window's outline at the rendezvous when the
// encoded bytes moved — never per frame, and a no-op pass costs a
// compare, never a write.
func (e *editor) publishOutline() {
	if e.ta == nil {
		return
	}
	nodes := e.outline()
	enc := string(appkit.EncodeOutline(nodes))
	if enc == e.a11yLast {
		return
	}
	if appkit.PublishOutline(uint32(e.ta.Win), nodes) {
		e.a11yLast = enc
	}
}

// unpublishOutline drops the staged outline on a clean exit — dead data
// to a reader either way (kind 16 only reads the focused window's file),
// but a graceful close leaves the share honest.
func (e *editor) unpublishOutline() {
	if e.ta == nil {
		return
	}
	_ = appkit.UnpublishOutline(uint32(e.ta.Win))
	e.a11yLast = ""
}
