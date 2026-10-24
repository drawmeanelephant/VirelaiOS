package main

// a11y_test.go — M197a (#3381): the seat outline producer's host-side
// pins. The grammar itself is appkit's; these tests pin the seat's
// state-to-node mapping — which surface produces which nodes, and who
// may carry 'f' — plus the publish dedup over the staged-file seam.

import (
	"testing"

	"virelai/appkit"
)

// saveA11yState preserves the globals the outline reads that
// saveSeatState does not already cover.
func saveA11yState() func() {
	unlock := saveSeatState()
	lk, il := lockActive, idleState
	nq, np := notifyQueue, notifyPainted
	rm, sa, cm := railMenuOpen, secretAskModal, clipMenuOpen
	last := seatA11yLast
	return func() {
		unlock()
		lockActive, idleState = lk, il
		notifyQueue, notifyPainted = nq, np
		railMenuOpen, secretAskModal, clipMenuOpen = rm, sa, cm
		seatA11yLast = last
	}
}

func TestSeatOutlineRailNamesLiveTabs(t *testing.T) {
	defer saveA11yState()()
	tabs = TabStrip{}
	tabs.OpenTab(3, "Calc")
	tabs.OpenTab(4, "Edit")
	out := seatA11yOutline()
	if len(out) != 4 {
		t.Fatalf("outline = %+v", out)
	}
	if out[0].Role != appkit.RoleSeat || out[0].Depth != 0 ||
		out[0].Name != "Desktop" {
		t.Fatalf("root = %+v", out[0])
	}
	if out[1].Role != appkit.RoleRail || out[1].Depth != 1 ||
		out[1].Name != "space 1" {
		t.Fatalf("rail = %+v", out[1])
	}
	if out[2].Role != appkit.RoleTab || out[2].Name != "Calc" ||
		out[2].State != appkit.StateFocused {
		t.Fatalf("focused cell = %+v", out[2])
	}
	if out[3].Role != appkit.RoleTab || out[3].Name != "Edit" ||
		out[3].State != 0 {
		t.Fatalf("idle cell = %+v", out[3])
	}
	// Focus moves name the cell, not a reorder.
	tabs.FocusTab(4)
	out = seatA11yOutline()
	if out[3].State != appkit.StateFocused || out[2].State != 0 {
		t.Fatalf("focus move = %+v %+v", out[2], out[3])
	}
}

func TestSeatOutlineLauncherOwnsTheKeys(t *testing.T) {
	defer saveA11yState()()
	tabs = TabStrip{}
	tabs.OpenTab(3, "Calc")
	launch = launcherState{
		open: true,
		catalog: []AppEntry{
			{Bin: "GOCALC.ELF", Label: "Calculator"},
			{Bin: "GOEDIT.ELF", Label: "Edit"},
		},
		filtered: []int{0, 1},
		reasons:  []string{"", "Binary not staged"},
		sel:      1,
	}
	out := seatA11yOutline()
	// seat, rail, tab, menu, field, 2 items = 7.
	if len(out) != 7 {
		t.Fatalf("outline = %+v", out)
	}
	// While the strip's sink holds the keys the rail cell is the
	// selection, not the focus — 's', never 'f'.
	if out[2].Role != appkit.RoleTab || out[2].State != appkit.StateSelected {
		t.Fatalf("rail cell under modal = %+v", out[2])
	}
	if out[3].Role != appkit.RoleMenu || out[3].Name != "Launcher" ||
		out[3].State != appkit.StateExpanded {
		t.Fatalf("menu = %+v", out[3])
	}
	if out[4].Role != appkit.RoleTextField ||
		out[4].State != appkit.StateFocused {
		t.Fatalf("filter field = %+v", out[4])
	}
	if out[5].Role != appkit.RoleMenuItem || out[5].Name != "Calculator" ||
		out[5].State != 0 {
		t.Fatalf("item 0 = %+v", out[5])
	}
	if out[6].Role != appkit.RoleMenuItem || out[6].Name != "Edit" ||
		out[6].State != appkit.StateSelected|appkit.StateDisabled {
		t.Fatalf("selected refused row = %+v", out[6])
	}
	// Exactly one focused node, and it is the field.
	focused := 0
	for _, n := range out {
		if n.State&appkit.StateFocused != 0 {
			focused++
		}
	}
	if focused != 1 {
		t.Fatalf("focused nodes = %d", focused)
	}
}

func TestSeatOutlinePaletteFace(t *testing.T) {
	defer saveA11yState()()
	launch = launcherState{
		open:    true,
		palette: true,
		results: []paletteEntry{
			{kind: palPane, label: "Zoom pane"},
			{kind: palTab, label: "Edit"},
		},
		filtered: []int{0, 1},
	}
	out := seatA11yOutline()
	if out[1].Role != appkit.RoleMenu || out[1].Name != "Palette" {
		t.Fatalf("palette menu = %+v", out[1])
	}
	if out[3].Name != "Zoom pane" || out[3].State != appkit.StateSelected {
		t.Fatalf("palette row 0 = %+v", out[3])
	}
	if out[4].Name != "Edit" || out[4].State != 0 {
		t.Fatalf("palette row 1 = %+v", out[4])
	}
}

func TestSeatOutlineEmptyAndErrorRows(t *testing.T) {
	defer saveA11yState()()
	launch = launcherState{open: true, filter: "zz"}
	out := seatA11yOutline()
	last := out[len(out)-1]
	if last.Role != appkit.RoleMenuItem ||
		last.State != appkit.StateDisabled ||
		last.Name != "No matching apps" {
		t.Fatalf("empty plate row = %+v", last)
	}
	launch.err = "APPS.TXT unavailable"
	out = seatA11yOutline()
	if out[len(out)-2].Name != "APPS.TXT unavailable" {
		t.Fatalf("err row = %+v", out[len(out)-2])
	}
}

func TestSeatOutlineToasts(t *testing.T) {
	defer saveA11yState()()
	notifyQueue = []notifyToast{
		{tabID: 3, text: "older"},
		{tabID: 4, text: "newer"},
	}
	out := seatA11yOutline()
	if len(out) != 3 {
		t.Fatalf("outline = %+v", out)
	}
	if out[1].Role != appkit.RoleToast || out[1].Name != "older" ||
		out[2].Name != "newer" {
		t.Fatalf("toasts = %+v %+v", out[1], out[2])
	}
}

func TestSeatOutlineCurtainCoversEverything(t *testing.T) {
	defer saveA11yState()()
	tabs = TabStrip{}
	tabs.OpenTab(3, "Calc")
	notifyQueue = []notifyToast{{tabID: 3, text: "secret"}}
	lockActive = true
	out := seatA11yOutline()
	// The veil replaces the ambient description: no rail cell, no toast
	// text may leak past it.
	if len(out) != 3 {
		t.Fatalf("locked outline = %+v", out)
	}
	if out[1].Role != appkit.RoleCurtain || out[1].Name != "Locked" {
		t.Fatalf("curtain = %+v", out[1])
	}
	if out[2].Role != appkit.RoleTextField ||
		out[2].State != appkit.StateFocused || out[2].Name != "passphrase" {
		t.Fatalf("passphrase field = %+v", out[2])
	}
	// The idle curtain is the same veil, minus the auth field.
	lockActive = false
	idleState = idleCurtain
	out = seatA11yOutline()
	if len(out) != 2 || out[1].Role != appkit.RoleCurtain {
		t.Fatalf("idle curtain outline = %+v", out)
	}
}

func TestSeatA11yPublishDedupAndSeam(t *testing.T) {
	defer saveA11yState()()
	var gotPath string
	var gotBody []byte
	writes := 0
	old := seatA11yWrite
	seatA11yWrite = func(path string, data []byte) int64 {
		gotPath = path
		gotBody = append([]byte(nil), data...)
		writes++
		return 0
	}
	defer func() { seatA11yWrite = old }()

	tabs = TabStrip{}
	tabs.OpenTab(3, "Calc")
	seatA11yPublish(false)
	if writes != 0 {
		t.Fatal("an unpresented frame must not publish")
	}
	seatA11yPublish(true)
	if writes != 1 || gotPath != appkit.A11ySeatPath {
		t.Fatalf("first publish: writes=%d path=%q", writes, gotPath)
	}
	seatA11yPublish(true)
	if writes != 1 {
		t.Fatal("identical chrome restaged the file")
	}
	nodes, ok := appkit.DecodeOutline(gotBody)
	if !ok {
		t.Fatal("published body is not the v1 grammar")
	}
	if len(nodes) != 3 || nodes[2].Name != "Calc" {
		t.Fatalf("decoded = %+v", nodes)
	}
	// A focus move is a real change and restages.
	tabs.OpenTab(4, "Edit")
	tabs.FocusTab(4)
	seatA11yPublish(true)
	if writes != 2 {
		t.Fatal("a changed outline did not republish")
	}
	if seatA11yNodes(gotBody) != 4 {
		t.Fatalf("seatA11yNodes = %d", seatA11yNodes(gotBody))
	}
}
