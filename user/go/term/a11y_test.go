package main

import (
	"testing"

	"virelai/appkit"
)

// The M197d resting outline is the line editor's verdict; a modal
// session moves 'f' to the overlay and leaves the editor listed.
func TestSessionOutlineFocus(t *testing.T) {
	nodes := sessionOutline()
	if len(nodes) != 2 || nodes[0].Role != appkit.RoleWindow ||
		nodes[0].Name != "Term" {
		t.Fatalf("resting outline: %+v", nodes)
	}
	if nodes[1].Role != appkit.RoleTextField || nodes[1].Name != "command line" ||
		nodes[1].State&appkit.StateFocused == 0 {
		t.Fatalf("editor node: %+v", nodes[1])
	}

	modal := modalOutline(appkit.RoleTextField, "find")
	if len(modal) != 3 || modal[2].State&appkit.StateFocused == 0 ||
		modal[1].State&appkit.StateFocused != 0 {
		t.Fatalf("modal outline: %+v", modal)
	}
}

func TestMenuOutlineBarFocus(t *testing.T) {
	bar := buildTermBar(new(menuAct))
	bar.Activate(0)

	nodes := menuOutline(&bar)
	if nodes[0].Role != appkit.RoleWindow {
		t.Fatalf("window node: %+v", nodes[0])
	}
	var barNode, editor *appkit.Node
	focused := 0
	for i := range nodes {
		if nodes[i].State&appkit.StateFocused != 0 {
			focused++
		}
		switch nodes[i].Role {
		case appkit.RoleMenuBar:
			barNode = &nodes[i]
		case appkit.RoleTextField:
			editor = &nodes[i]
		}
	}
	if barNode == nil || barNode.State&appkit.StateExpanded == 0 {
		t.Fatalf("engaged bar: %+v", nodes)
	}
	if editor == nil || editor.State&appkit.StateFocused != 0 {
		t.Fatalf("the bar owns the keys — the editor must not be focused: %+v", editor)
	}
	if focused == 0 {
		t.Fatalf("no focused node in the menu outline: %+v", nodes)
	}
}
