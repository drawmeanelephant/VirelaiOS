package main

import (
	"testing"

	"virelai/appkit"
)

// The M197d outline is the two surfaces that own the keyboard: the
// document while the find bar is closed, the find field while it is
// docked — exactly one focused node either way, and names are labels,
// never contents.
func TestOutlineFindToggle(t *testing.T) {
	a := &app{buf: NewBuffer(), path: defaultPath, top: 1}
	a.find = appkit.NewFind()

	nodes := a.outline()
	if len(nodes) != 2 {
		t.Fatalf("closed outline: %d nodes want 2", len(nodes))
	}
	if nodes[0].Role != appkit.RoleWindow || nodes[0].Name != "notes.txt" {
		t.Fatalf("window node: %+v", nodes[0])
	}
	if nodes[1].Role != appkit.RoleTextField || nodes[1].Name != "document" ||
		nodes[1].State&appkit.StateFocused == 0 {
		t.Fatalf("document node: %+v", nodes[1])
	}

	a.find.Present()
	nodes = a.outline()
	if len(nodes) != 3 {
		t.Fatalf("docked outline: %d nodes want 3", len(nodes))
	}
	if nodes[1].State&appkit.StateFocused != 0 {
		t.Fatalf("document stays focused under the docked bar: %+v", nodes[1])
	}
	if nodes[2].Role != appkit.RoleTextField || nodes[2].Name != "find" ||
		nodes[2].State&appkit.StateFocused == 0 {
		t.Fatalf("find node: %+v", nodes[2])
	}

	// The whole list decodes — the wire form a reader prints.
	if _, ok := appkit.DecodeOutline(appkit.EncodeOutline(nodes)); !ok {
		t.Fatal("published outline does not decode")
	}
}
