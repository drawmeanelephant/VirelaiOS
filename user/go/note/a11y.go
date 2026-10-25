// a11y.go — M197d (#3416): the notepad publishes its #a11y v1 outline.
//
// The M177b seam (user/go/appkit/a11y.go, the GODEMO precedent) stages the
// window's control tree at /host/A11Y.<win> so GOA11Y can read the focused
// window's structure. This app has exactly two surfaces that ever own the
// keyboard — the document and the docked find bar — so the outline is two
// textfield nodes under the window, and the focus verdict is the bar's
// dock state: docked, the find field carries 'f' and the document does
// not; closed, the document does. D2 keeps contents out: names are the
// surface labels and the window's document name, never buffer or pattern
// text.
package main

import "virelai/appkit"

// outline assembles the window's node list in read order — the document
// surface first, the find field last while it is docked — with exactly
// one node carrying the focus verdict.
func (a *app) outline() []appkit.Node {
	doc := appkit.Node{Depth: 1, Role: appkit.RoleTextField, Name: "document"}
	if !a.find.Open {
		doc.State |= appkit.StateFocused
	}
	nodes := []appkit.Node{
		{Role: appkit.RoleWindow, Name: tabTitleForPath(a.path)},
		doc,
	}
	if a.find.Open {
		nodes = append(nodes, appkit.Node{Depth: 1, Role: appkit.RoleTextField,
			State: appkit.StateFocused, Name: "find"})
	}
	return nodes
}
