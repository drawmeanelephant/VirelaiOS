package main

// view_test.go — the class-A half of M208a: the window's model over the
// map-backed share — pane loads, selection, the body render rules, pane
// navigation, and Fetch's status-line contract — no socket, no window.

import (
	"errors"
	"testing"
)

// seedViewCache loads the fake share the gate's fixture mirrors: two
// listed boxes plus an envelope row and a body under the second one.
func seedViewCache(f *fakeCacheHost) {
	f.put("BOXES", "INBOX\nLists\n")
	f.put("HDRS", "Lists\t7\tdana@fixture.local\tweekly digest\tWed\n")
	f.put("BODIES", "@Lists\t7\t1\ttext/plain\t18\nweekly digest body\n")
}

func newTestView(cfg Config) *mailView {
	return &mailView{cfg: cfg, fetchFn: func(Config, string, []int) error {
		return errors.New("fetchFn not stubbed")
	}}
}

func TestViewLoadReadsCache(t *testing.T) {
	f := swapCache(t)
	seedViewCache(f)

	v := newTestView(Config{})
	v.load()
	if len(v.boxes) != 2 || v.boxes[0] != "INBOX" || v.boxes[1] != "Lists" {
		t.Fatalf("boxes %v", v.boxes)
	}
	// No USE row: resolveBox's INBOX default wins the initial pick.
	if v.curBox() != "INBOX" {
		t.Fatalf("initial box %q", v.curBox())
	}
	if len(v.hdrs) != 0 {
		t.Fatalf("INBOX hdrs %v", v.hdrs)
	}
	// The remembered selection overrides the default on a fresh open.
	f.put("USE", "Lists\n")
	v2 := newTestView(Config{})
	v2.load()
	if v2.curBox() != "Lists" || len(v2.hdrs) != 1 || v2.hdrs[0].seq != 7 {
		t.Fatalf("use-restored box %q hdrs %v", v2.curBox(), v2.hdrs)
	}
	// A reload keeps the box the user is on — the USE row does not
	// steal the selection back.
	v2.boxSel = 0
	v2.load()
	if v2.curBox() != "INBOX" {
		t.Fatalf("reload stole box %q", v2.curBox())
	}
}

func TestViewBoxSelectListsHeaders(t *testing.T) {
	f := swapCache(t)
	seedViewCache(f)

	v := newTestView(Config{})
	v.load()
	v.pane = paneBoxes
	v.move(1) // INBOX -> Lists
	if v.curBox() != "Lists" {
		t.Fatalf("box %q", v.curBox())
	}
	if len(v.hdrs) != 1 || v.hdrs[0].from != "dana@fixture.local" ||
		v.hdrs[0].subject != "weekly digest" {
		t.Fatalf("hdrs %+v", v.hdrs)
	}
	// The picked header's body opened with it.
	if len(v.body) != 1 || v.body[0] != "weekly digest body" {
		t.Fatalf("body %v", v.body)
	}
}

func TestViewBodyRules(t *testing.T) {
	f := swapCache(t)
	f.put("BOXES", "B\n")
	f.put("HDRS", "B\t1\ta\tplain\tWed\nB\t2\tb\thtml\tWed\nB\t3\tc\tmissing\tWed\n")
	f.put("BODIES", "@B\t1\t1\ttext/plain\t9\nline one\n"+
		"@B\t2\t1\ttext/html\t13\n<p>markup</p>\n")

	v := newTestView(Config{})
	v.load()
	if len(v.hdrs) != 3 {
		t.Fatalf("hdrs %+v", v.hdrs)
	}
	if len(v.body) != 1 || v.body[0] != "line one" {
		t.Fatalf("plain body %v", v.body)
	}
	// The HTML-only body names itself instead of rendering markup.
	v.pane = paneHdrs
	v.move(1)
	if len(v.body) != 1 || v.body[0] != "text/html body — not rendered" {
		t.Fatalf("html body %v", v.body)
	}
	if v.status != "gomail: not-rendered text/html 2" {
		t.Fatalf("html status %q", v.status)
	}
	// An envelope with no cached body is a cache-miss, never invented.
	v.move(1)
	if len(v.body) != 1 || v.body[0] != "(no cached body — f fetches)" {
		t.Fatalf("missing body %v", v.body)
	}
	if v.status != "gomail: cache-miss B 3" {
		t.Fatalf("miss status %q", v.status)
	}
}

func TestViewNavClamps(t *testing.T) {
	f := swapCache(t)
	seedViewCache(f)

	v := newTestView(Config{})
	v.load()
	v.pane = paneBoxes
	v.move(-1) // clamps at 0
	if v.boxSel != 0 {
		t.Fatalf("clamp lo %d", v.boxSel)
	}
	v.move(5) // clamps at the last row
	if v.boxSel != 1 {
		t.Fatalf("clamp hi %d", v.boxSel)
	}
	// Pane cycling stays inside the ring.
	v.setPane(9)
	if v.pane != paneCount-1 {
		t.Fatalf("pane hi %d", v.pane)
	}
	v.setPane(-2)
	if v.pane != 0 {
		t.Fatalf("pane lo %d", v.pane)
	}
}

func TestViewFetchRunsPathAndNamesStatus(t *testing.T) {
	f := swapCache(t)
	seedViewCache(f)

	var gotBox string
	var gotSeqs []int
	v := newTestView(Config{Host: "10.0.0.2", User: "gate"})
	v.fetchFn = func(cfg Config, box string, seqs []int) error {
		gotBox, gotSeqs = box, seqs
		// The wire path's own markers ride say() — the tap catches them.
		say("gomail: select " + box + " exists=3")
		say("gomail: hdr 4 erin@fixture.local: fresh mail")
		cacheSaveHdrs(box, []Header{hdrRow(4, "erin@fixture.local", "fresh mail")})
		return nil
	}
	v.load()
	v.pane = paneBoxes
	v.move(1) // Lists, hdrSel=0 (seq 7)
	v.fetch()
	if gotBox != "Lists" || len(gotSeqs) != 1 || gotSeqs[0] != 7 {
		t.Fatalf("fetch target %q seqs %v", gotBox, gotSeqs)
	}
	// The status line names the last marker the path emitted — the
	// publish row, which the real fetch path also ends on.
	if v.status != "gomail: cache /host/MAIL/HDRS" {
		t.Fatalf("status %q", v.status)
	}
	// The panes repainted from the refreshed cache — the new row is in.
	if len(v.hdrs) != 1 || v.hdrs[0].subject != "fresh mail" {
		t.Fatalf("reloaded hdrs %+v", v.hdrs)
	}
}

func TestViewFetchRefusalIsAStatusNotAnExit(t *testing.T) {
	f := swapCache(t)
	seedViewCache(f)

	v := newTestView(Config{Host: "10.0.0.2", User: "gate"})
	v.fetchFn = func(Config, string, []int) error {
		say("gomail: refuse plaintext")
		return protoErr("refuse plaintext", "")
	}
	v.load()
	v.pane = paneBoxes
	v.move(1)
	v.fetch()
	if v.status != "gomail: refuse plaintext" {
		t.Fatalf("status %q", v.status)
	}
	// A refusal repaints nothing — the cached rows still hold.
	if len(v.hdrs) != 1 || v.hdrs[0].subject != "weekly digest" {
		t.Fatalf("hdrs after refusal %+v", v.hdrs)
	}
}

func TestViewFetchEdgeCases(t *testing.T) {
	f := swapCache(t)
	seedViewCache(f)

	// No account host — the same named line the CLI prints.
	v := newTestView(Config{})
	v.fetchFn = func(Config, string, []int) error {
		t.Fatal("fetchFn ran with no host")
		return nil
	}
	v.load()
	v.pane = paneBoxes
	v.move(1)
	v.fetch()
	if v.status != "gomail: account missing host" {
		t.Fatalf("host status %q", v.status)
	}

	// No boxes at all — a cache-miss by name, never a dial.
	f.files = map[string][]byte{}
	v.load()
	v.fetch()
	if v.status != "gomail: cache-miss boxes" {
		t.Fatalf("boxes status %q", v.status)
	}
}
