// a11y.go — M197d (#3416): the file manager publishes its #a11y v1 outline.
//
// The M177b seam (user/go/appkit/a11y.go, the GODEMO precedent) stages the
// window's control tree at /host/A11Y.<win> so GOA11Y reads the focused
// window's structure. The outline is the menubar's own subtree ahead of
// the listing — the surface this app exists for, where the selection IS
// the focus — plus one node per modal that can take the keyboard: the
// rename prompt, the delete confirm, the open-with list, the right-click
// menu with its submenu, the chord hints, Quick Look, the properties
// card, and the content search's prompt and hit list. D2 keeps contents
// out: names are the surface labels and the focused item's own name —
// the selected entry, the hovered menu row, the hit's relative path —
// never file contents, pattern text, or whole listings.
package main

import "virelai/appkit"

// a11yNode is one surface's entry at depth d.
func a11yNode(d int, role appkit.Role, name string, st appkit.State) appkit.Node {
	return appkit.Node{Depth: d, Role: role, Name: name, State: st}
}

// outline assembles the window's node list in read order — menubar,
// listing, then whichever modal the mode raised — with the focus verdict
// on the one surface that owns the keys.
func (m *model) outline() []appkit.Node {
	nodes := []appkit.Node{{Role: appkit.RoleWindow, Name: appTitle}}
	nodes = append(nodes, m.bar.A11yTree(1, m.bar.Active)...)
	normal := m.mode == modeNormal && !m.bar.Active
	tree := a11yNode(1, appkit.RoleTree, "listing", 0)
	if m.n == 0 && normal {
		// An empty listing still holds the keys — the tree itself is the
		// focused control.
		tree.State |= appkit.StateFocused
	}
	nodes = append(nodes, tree)
	if m.n > 0 && m.sel >= 0 && m.sel < m.n {
		item := a11yNode(2, appkit.RoleTreeItem,
			m.entries[m.sel].NameString(), appkit.StateSelected)
		if normal {
			item.State |= appkit.StateFocused
		}
		nodes = append(nodes, item)
	}
	modal := !m.bar.Active
	switch m.mode {
	case modeNormal:
	case modeRename:
		nodes = append(nodes, a11yNode(1, appkit.RoleTextField, "rename",
			a11yFocus(modal)))
	case modeConfirmDelete:
		nodes = append(nodes, a11yNode(1, appkit.RoleButton, "confirm delete",
			a11yFocus(modal)))
	case modeOpenWith:
		// The candidate list owns the digits — a menu of handlers with
		// the focus on the plate itself: digit selection has no cursor.
		nodes = append(nodes, a11yNode(1, appkit.RoleMenu, "open with",
			appkit.StateExpanded|a11yFocus(modal)))
		for _, h := range m.openCands {
			nodes = append(nodes, a11yNode(2, appkit.RoleMenuItem, h.Label, 0))
		}
	case modeContext:
		nodes = append(nodes, m.ctxA11y(modal)...)
	case modeHints:
		nodes = append(nodes, a11yNode(1, appkit.RoleTree, "chord hints",
			a11yFocus(modal)))
	case modeQuickLook:
		nodes = append(nodes, a11yNode(1, appkit.RoleTree, "quick look",
			a11yFocus(modal)))
	case modeProps:
		nodes = append(nodes, a11yNode(1, appkit.RoleTable, "properties",
			a11yFocus(modal)))
	case modeSearchInput:
		nodes = append(nodes, a11yNode(1, appkit.RoleTextField, "search",
			a11yFocus(modal)))
	case modeSearchHits:
		nodes = append(nodes, m.searchA11y(modal)...)
	}
	return nodes
}

// a11yFocus is the verdict bit when the surface owns the keys.
func a11yFocus(owns bool) appkit.State {
	if owns {
		return appkit.StateFocused
	}
	return 0
}

// ctxA11y is the right-click menu's subtree: the top-level plate, and the
// descended "Open With…" submenu's handler rows when it is open. The
// focus verdict is the deepest hovered row.
func (m *model) ctxA11y(modal bool) []appkit.Node {
	nodes := []appkit.Node{a11yNode(1, appkit.RoleMenu, "context",
		appkit.StateExpanded)}
	labels := m.ctxTopLabels()
	openWith := m.ctxOpenWithRow()
	for i, l := range labels {
		var st appkit.State
		if i == m.ctx.sel {
			st |= appkit.StateSelected
			if modal && !m.ctx.sub {
				st |= appkit.StateFocused
			}
		}
		nodes = append(nodes, a11yNode(2, appkit.RoleMenuItem, l, st))
		if !(m.ctx.sub && i == openWith) {
			continue
		}
		for j, h := range m.ctx.apps {
			var sst appkit.State
			if j == m.ctx.subSel {
				sst |= appkit.StateSelected
				if modal {
					sst |= appkit.StateFocused
				}
			}
			nodes = append(nodes, a11yNode(3, appkit.RoleMenuItem, h.label, sst))
		}
	}
	return nodes
}

// searchA11y is the hit frame's subtree: the results tree and the
// cursor's own row — the hit's path label, never the matched line's
// text (D2).
func (m *model) searchA11y(modal bool) []appkit.Node {
	tree := a11yNode(1, appkit.RoleTree, "search results", 0)
	nodes := []appkit.Node{tree}
	s := &m.search
	if len(s.hits) == 0 {
		if modal {
			nodes[0].State |= appkit.StateFocused
		}
		return nodes
	}
	if s.sel >= 0 && s.sel < len(s.hits) {
		item := a11yNode(2, appkit.RoleTreeItem, s.hits[s.sel].rel,
			appkit.StateSelected)
		if modal {
			item.State |= appkit.StateFocused
		}
		nodes = append(nodes, item)
	}
	return nodes
}

// publishOutline stages the window's outline at the rendezvous when the
// encoded bytes moved — the GODEMO dedup: main calls it once per loop
// pass and a quiet pass costs a compare, never a write.
func (m *model) publishOutline() {
	nodes := m.outline()
	enc := string(appkit.EncodeOutline(nodes))
	if enc == m.a11yLast {
		return
	}
	if appkit.PublishOutline(m.winID, nodes) {
		m.a11yLast = enc
	}
}
