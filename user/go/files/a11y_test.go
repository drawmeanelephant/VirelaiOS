package main

import (
	"testing"

	"virelai/appkit"
)

// The M197d outline is the listing's selection as the focus, the
// menubar's own subtree ahead of it, and one node per modal — names are
// the focused item's label, never listing contents.
func TestOutlineListingFocus(t *testing.T) {
	m := testModel(entry("KNOWN.TXT", false), entry("SUB", true))

	nodes := m.outline()
	if nodes[0].Role != appkit.RoleWindow || nodes[0].Name != "Files" {
		t.Fatalf("window node: %+v", nodes[0])
	}
	// buildMenus is not armed in a testModel — the bar's subtree is the
	// bare bar node.
	var tree, item *appkit.Node
	for i := range nodes {
		switch nodes[i].Role {
		case appkit.RoleTree:
			tree = &nodes[i]
		case appkit.RoleTreeItem:
			item = &nodes[i]
		}
	}
	if tree == nil || tree.Name != "listing" {
		t.Fatalf("listing tree: %+v", nodes)
	}
	if item == nil || item.Name != "SUB" ||
		item.State&appkit.StateFocused == 0 {
		t.Fatalf("selected item is the focus: %+v", item)
	}
}

func TestOutlineModalFocus(t *testing.T) {
	m := testModel(entry("KNOWN.TXT", false))
	m.mode = modeRename

	nodes := m.outline()
	var item, modal *appkit.Node
	for i := range nodes {
		switch nodes[i].Role {
		case appkit.RoleTreeItem:
			item = &nodes[i]
		case appkit.RoleTextField:
			modal = &nodes[i]
		}
	}
	if item == nil || item.State&appkit.StateFocused != 0 {
		t.Fatalf("a modal owns the keys — the item must not be focused: %+v", item)
	}
	if modal == nil || modal.Name != "rename" ||
		modal.State&appkit.StateFocused == 0 {
		t.Fatalf("rename node: %+v", modal)
	}
}

func TestOutlineContextMenu(t *testing.T) {
	m := testModel(entry("KNOWN.TXT", false))
	m.mode = modeContext
	m.ctx = ctxMenu{name: "KNOWN.TXT", sel: 1}
	m.ctx.apps = []ctxApp{{label: "Text Editor"}, {label: "Web"}}
	m.ctx.sub, m.ctx.subSel = true, 0

	nodes := m.outline()
	var menu, sub *appkit.Node
	depths := 0
	for i := range nodes {
		if nodes[i].Role == appkit.RoleMenu && nodes[i].Name == "context" {
			menu = &nodes[i]
		}
		if nodes[i].Depth == 3 {
			depths++
			if nodes[i].State&appkit.StateFocused != 0 {
				sub = &nodes[i]
			}
		}
	}
	if menu == nil || menu.State&appkit.StateExpanded == 0 {
		t.Fatalf("context menu: %+v", nodes)
	}
	if depths != 2 || sub == nil || sub.Name != "Text Editor" {
		t.Fatalf("submenu rows: %+v", nodes)
	}
}

// The wire form a reader prints: the assembled list round-trips.
func TestOutlineDecodes(t *testing.T) {
	m := testModel(entry("KNOWN.TXT", false))
	if _, ok := appkit.DecodeOutline(appkit.EncodeOutline(m.outline())); !ok {
		t.Fatal("published outline does not decode")
	}
}
