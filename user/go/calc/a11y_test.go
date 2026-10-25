package main

import (
	"testing"

	"virelai/appkit"
)

// The M197d outline is the display plus the live pad's labelled cells —
// a mode toggle rewrites the button set, and 'f' stays on the surface
// the keyboard edits.
func TestOutlinePads(t *testing.T) {
	a := &app{}

	nodes := a.outline()
	if nodes[0].Role != appkit.RoleWindow || nodes[0].Name != "Calc" {
		t.Fatalf("window node: %+v", nodes[0])
	}
	if nodes[1].Role != appkit.RoleTextField || nodes[1].Name != "display" ||
		nodes[1].State&appkit.StateFocused == 0 {
		t.Fatalf("display node: %+v", nodes[1])
	}
	decButtons := 0
	for _, n := range nodes {
		if n.Role == appkit.RoleButton {
			decButtons++
			if n.Name == "" {
				t.Fatalf("unlabelled button: %+v", n)
			}
		}
	}
	if decButtons != 20 {
		t.Fatalf("decimal pad: %d buttons want 20", decButtons)
	}

	a.eng.RestoreMode(true, 10)
	nodes = a.outline()
	progButtons := 0
	for _, n := range nodes {
		if n.Role == appkit.RoleButton {
			progButtons++
		}
	}
	if progButtons != 34 {
		t.Fatalf("programmer pad: %d buttons want 34", progButtons)
	}

	if _, ok := appkit.DecodeOutline(appkit.EncodeOutline(nodes)); !ok {
		t.Fatal("published outline does not decode")
	}
}
