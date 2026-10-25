// a11y.go — M197d (#3416): the terminal publishes its #a11y v1 outline.
//
// The M177b seam (user/go/appkit/a11y.go, the GODEMO precedent) stages the
// window's control tree at /host/A11Y.<win> so GOA11Y reads the focused
// window's structure. Four surfaces own this app's keyboard across its
// sessions: the shell's line editor at rest, the F10 menubar while it
// stands, the '?' chord-hint overlay, and the ctrl+shift+g find bar —
// so the outline is the resting textfield plus whichever modal surface
// currently holds the focus verdict. D2 keeps contents out: names are
// the surface labels, never command lines or the find pattern.
package main

import (
	"virelai/appkit"
	"virelai/tabapp"
)

// a11yLast is the last staged body — the GODEMO dedup: the sessions
// republish on every state change they drive, and identical bytes are
// not restaged.
var a11yLast string

// a11yPublish stages nodes at the window's rendezvous when the encoded
// outline differs from the last one the share accepted.
func a11yPublish(ta *tabapp.TabApp, nodes []appkit.Node) {
	enc := string(appkit.EncodeOutline(nodes))
	if enc == a11yLast {
		return
	}
	if appkit.PublishOutline(uint32(ta.Win), nodes) {
		a11yLast = enc
	}
}

// a11yUnpublish drops the staged outline on a clean exit — dead data to
// a reader either way (kind 16 only reads the focused window's file),
// but a graceful shutdown leaves the share honest.
func a11yUnpublish(ta *tabapp.TabApp) {
	_ = appkit.UnpublishOutline(uint32(ta.Win))
	a11yLast = ""
}

// sessionOutline is the resting shape: the window, and the line editor
// that owns the keys.
func sessionOutline() []appkit.Node {
	return []appkit.Node{
		{Role: appkit.RoleWindow, Name: appTitle},
		{Depth: 1, Role: appkit.RoleTextField,
			State: appkit.StateFocused, Name: "command line"},
	}
}

// modalOutline is the resting shape plus one focused node naming the
// overlay that took the keyboard — the menu session uses the bar's own
// A11yTree instead since its subtree is the structure.
func modalOutline(role appkit.Role, name string) []appkit.Node {
	return []appkit.Node{
		{Role: appkit.RoleWindow, Name: appTitle},
		{Depth: 1, Role: appkit.RoleTextField, Name: "command line"},
		{Depth: 1, Role: role, State: appkit.StateFocused, Name: name},
	}
}

// menuOutline is the menubar session's shape: the bar's own subtree —
// titles, and the open menu's items — ahead of the resting editor, with
// the focus verdict inside the bar.
func menuOutline(bar *appkit.MenuBar) []appkit.Node {
	nodes := []appkit.Node{{Role: appkit.RoleWindow, Name: appTitle}}
	nodes = append(nodes, bar.A11yTree(1, true)...)
	nodes = append(nodes, appkit.Node{Depth: 1, Role: appkit.RoleTextField,
		Name: "command line"})
	return nodes
}
