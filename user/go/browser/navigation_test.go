package main

import (
	"testing"

	"virelai/vi"
	"virelai/webrender"
)

func TestCrossDocumentFragmentIsAppliedAfterLayout(t *testing.T) {
	a := &app{hist: newHistory(), text: webrender.Bitmap{}, target: "/host/NEXT.HTML#deep%20section"}
	a.loadBody([]byte(`<p style="height:800px">top</p><h2 id="deep section">Destination</h2><p style="height:800px">tail</p>`), a.target)
	if a.scroll <= 0 || a.scroll > webrenderScrollMax(a) {
		t.Fatalf("cross-document fragment not applied: scroll=%d", a.scroll)
	}
	old := a.scroll
	a.applyFragment("/host/NEXT.HTML#missing")
	if a.scroll != old {
		t.Fatal("an absent anchor reset scroll")
	}
}

func TestReplacementNavigationDoesNotInheritHistoryTraversal(t *testing.T) {
	a := &app{hist: newHistory(), text: webrender.Bitmap{}, historyMove: true}
	for _, name := range []string{"A", "B", "C"} {
		a.hist.push(entry{Target: name})
	}
	_, _ = a.hist.back()
	a.navigate("ftp://D", "url-entry") // a defined refusal still records a fresh visit
	if a.historyMove || a.hist.canForward() {
		t.Fatal("replacement navigation kept the interrupted traversal tail")
	}
}

func TestBoundedReadsServiceCancellationAndPreserveOtherInput(t *testing.T) {
	a := &app{}
	key := vi.Event{Kind: vi.EvKeyDown, Arg0: 0x04, Arg1: 'a'}
	a.queueOrCancel(key)
	if a.pageCancelled || len(a.pendingEvents) != 1 || a.pendingEvents[0] != key {
		t.Fatal("ordinary input lost while reading a resource")
	}
	a.queueOrCancel(vi.Event{Kind: vi.EvKeyDown, Arg0: keyEscape})
	if !a.cancelBetweenReads() {
		t.Fatal("Escape was not serviced between bounded reads")
	}
	u, _ := parseBrowserURL("http://10.0.0.2/")
	if _, kind := a.connectPage(u); kind != "cancelled" || a.requests != 0 {
		t.Fatal("cancelled resource issued another connect")
	}
	b := &app{}
	b.queueOrCancel(vi.Event{Kind: vi.EvWinClose})
	if !b.quit || !b.pageCancelled {
		t.Fatal("window close remained deferred")
	}
}
