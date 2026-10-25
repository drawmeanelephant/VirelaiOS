package main

import (
	"testing"

	"virelai/appkit"
)

// The M197d outline is the menubar's subtree, the document, and the
// modal's own node — with the verdict on exactly one surface.
func TestOutlineModeFocus(t *testing.T) {
	e := &editor{doc: doc{path: "/host/notes.txt"}}
	e.buildMenus()

	nodes := e.outline()
	if nodes[0].Role != appkit.RoleWindow || nodes[0].Name != "notes.txt" {
		t.Fatalf("window node: %+v", nodes[0])
	}
	var doc *appkit.Node
	for i := range nodes {
		if nodes[i].Role == appkit.RoleTextField && nodes[i].Name == "document" {
			doc = &nodes[i]
		}
	}
	if doc == nil || doc.State&appkit.StateFocused == 0 {
		t.Fatalf("document is the resting focus: %+v", nodes)
	}

	e.mode = modeReplace
	nodes = e.outline()
	var findF, repF *appkit.Node
	for i := range nodes {
		if nodes[i].Role == appkit.RoleTextField && nodes[i].Name == "find" {
			findF = &nodes[i]
		}
		if nodes[i].Role == appkit.RoleTextField && nodes[i].Name == "replace" {
			repF = &nodes[i]
		}
	}
	if findF == nil || repF == nil {
		t.Fatalf("replace mode: %+v", nodes)
	}
	if findF.State&appkit.StateFocused == 0 || repF.State&appkit.StateFocused != 0 {
		t.Fatalf("the pattern field is focused first: %+v", nodes)
	}
	e.bar2At = true
	nodes = e.outline()
	for i := range nodes {
		if nodes[i].Role == appkit.RoleTextField && nodes[i].Name == "replace" &&
			nodes[i].State&appkit.StateFocused == 0 {
			t.Fatalf("bar2At moves the verdict: %+v", nodes)
		}
	}
}

func TestOutlineMenubarStealsFocus(t *testing.T) {
	e := &editor{doc: doc{path: "/host/notes.txt"}}
	e.buildMenus()
	e.mbar.Active = true

	nodes := e.outline()
	for _, n := range nodes {
		if n.Role == appkit.RoleTextField && n.State&appkit.StateFocused != 0 {
			t.Fatalf("the bar owns the keys — no field may be focused: %+v", nodes)
		}
	}
	var bar *appkit.Node
	for i := range nodes {
		if nodes[i].Role == appkit.RoleMenuBar {
			bar = &nodes[i]
		}
	}
	if bar == nil || bar.State&appkit.StateExpanded == 0 {
		t.Fatalf("engaged bar: %+v", nodes)
	}
}

// The wire form a reader prints: the assembled list round-trips.
func TestOutlineDecodes(t *testing.T) {
	e := &editor{doc: doc{path: "/host/notes.txt"}}
	e.buildMenus()
	if _, ok := appkit.DecodeOutline(appkit.EncodeOutline(e.outline())); !ok {
		t.Fatal("published outline does not decode")
	}
}
