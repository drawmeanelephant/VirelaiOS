package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReferenceRenderWorkInventory(t *testing.T) {
	files, err := filepath.Glob("../../../tests/fixtures/web/reference/*.html")
	if err != nil {
		t.Fatal(err)
	}
	largest, work := "", 0
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		a := &app{hist: newHistory(), text: browserFonts(t), target: "/host/REF.HTML"}
		a.loadBody(body, a.target)
		score := a.doc.Nodes + len(a.lay.BoxTree.Boxes) + len(a.lay.Items)
		t.Logf("%s bytes=%d nodes=%d boxes=%d items=%d work=%d",
			filepath.Base(path), len(body), a.doc.Nodes, len(a.lay.BoxTree.Boxes), len(a.lay.Items), score)
		if score > work {
			largest, work = filepath.Base(path), score
		}
	}
	t.Logf("largest retained render work: %s score=%d", largest, work)
}
