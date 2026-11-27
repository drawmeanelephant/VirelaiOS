package main

import (
	"bytes"
	"strings"
	"testing"
)

const renameBaseGo = `package calc

func X() int {
	return 1
}

func Y() int {
	return 2
}
`

const renameRenamedGo = `package calc

func Z() int {
	return 1
}

func Y() int {
	return 2
}
`

func writeRenameFixture(t *testing.T, h *testHarness) {
	t.Helper()
	if err := h.fs.WriteFileSafe("/host/SRCA/calc.go", []byte(renameBaseGo)); err != nil {
		t.Fatal(err)
	}
	if rc, logs := h.runCmd("init", "--name=a", "--src=/host/SRCA"); rc != 0 {
		t.Fatalf("init: %v", logs)
	}
	if rc, logs := h.runCmd("add", "/host/SRCA"); rc != 0 {
		t.Fatalf("add: %v", logs)
	}
	if rc, logs := h.runCmd("commit", "-m", "base"); rc != 0 {
		t.Fatalf("commit: %v", logs)
	}
}

func atomByName(t *testing.T, h *testHarness, ws, name string) StoredAtom {
	t.Helper()
	atoms, err := LoadWorkspaceAtoms(h.fs, h.root, ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range atoms {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("atom %s missing in %+v", name, atoms)
	return StoredAtom{}
}

func TestRenameStagesCommitAndCheckoutRoundTrip(t *testing.T) {
	h := newTestHarness(t)
	writeRenameFixture(t, h)
	before := atomByName(t, h, "a", "calc.X")

	if err := h.fs.WriteFileSafe("/host/SRCA/calc.go", []byte(renameRenamedGo)); err != nil {
		t.Fatal(err)
	}
	rc, logs := h.runCmd("rename", "calc.X", "calc.Z")
	if rc != 0 || len(logs) != 1 || logs[0] != "flint: rename ok calc.X calc.Z" {
		t.Fatalf("rename: rc=%d logs=%v", rc, logs)
	}

	stage, err := LoadWorkspaceStage(h.fs, h.root, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(stage) != 1 || stage[0].Type != "rename" || stage[0].AtomID != before.ID || stage[0].NewName != "calc.Z" || stage[0].OldHash != before.Hash {
		t.Fatalf("stage after rename: %+v want one rename op for %s", stage, before.ID)
	}
	after := atomByName(t, h, "a", "calc.Z")
	if after.ID != before.ID || after.Hash != before.Hash || after.SigHash != before.SigHash {
		t.Fatalf("rename must preserve id/hash: before %+v after %+v", before, after)
	}

	rc, logs = h.runCmd("status")
	if rc != 0 || len(logs) < 2 || !strings.Contains(logs[0], " rename=1") || logs[1] != "flint: stage rename calc.Z" {
		t.Fatalf("status must name the rename op: rc=%d logs=%v", rc, logs)
	}
	rc, logs = h.runCmd("diff")
	if rc != 0 || len(logs) < 2 || !strings.Contains(logs[0], " rename=1") || logs[1] != "flint: diff rename calc.go calc.Z" {
		t.Fatalf("diff must name the rename op: rc=%d logs=%v", rc, logs)
	}

	if rc, logs := h.runCmd("commit", "-m", "rename X to Z"); rc != 0 {
		t.Fatalf("commit rename: %v", logs)
	}
	rc, logs = h.runCmd("log")
	if rc != 0 || len(logs) < 2 || logs[1] != "flint: change-op rename calc.go calc.Z" {
		t.Fatalf("log must name the rename op: rc=%d logs=%v", rc, logs)
	}
	rc, logs = h.runCmd("checkout", "--stream=main", "--dir=/host/OUT")
	if rc != 0 || len(logs) != 1 || logs[0] != "flint: checkout ok stream=main files=1" {
		t.Fatalf("checkout rename: rc=%d logs=%v", rc, logs)
	}
	got, err := h.fs.ReadFile("/host/OUT/calc.go", MaxFileReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte(renameRenamedGo)) {
		t.Fatalf("checkout did not round-trip renamed source:\n%s", got)
	}

	// Removing only the file blob forces canonical synthesis from per-Atom
	// bodies. That path must find the name-aware renamed body rather than
	// the pre-rename body that shares the same canonical hash.
	atoms, err := LoadWorkspaceAtoms(h.fs, h.root, "a")
	if err != nil {
		t.Fatal(err)
	}
	key, err := fileBlobKey("calc.go", atoms)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.fs.Remove(blobPath(h.root, key)); err != nil {
		t.Fatal(err)
	}
	if err := h.fs.Remove("/host/OUT/calc.go"); err != nil {
		t.Fatal(err)
	}
	rc, logs = h.runCmd("checkout", "--stream=main", "--dir=/host/OUT")
	if rc != 0 {
		t.Fatalf("canonical checkout after rename: rc=%d logs=%v", rc, logs)
	}
	got, err = h.fs.ReadFile("/host/OUT/calc.go", MaxFileReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("func Z() int")) || bytes.Contains(got, []byte("func X() int")) {
		t.Fatalf("canonical synthesis used the wrong renamed body:\n%s", got)
	}
	headAtoms := replayHeadAtoms(t, h, "main")
	found := false
	for _, a := range headAtoms {
		if a.ID == before.ID && a.Name == "calc.Z" {
			found = true
		}
	}
	if !found {
		t.Fatalf("replayed head did not preserve renamed atom identity: %+v", headAtoms)
	}
}

func TestRenameWithBodyEditStagesRefactorPair(t *testing.T) {
	h := newTestHarness(t)
	writeRenameFixture(t, h)
	before := atomByName(t, h, "a", "calc.X")
	refactor := strings.Replace(renameRenamedGo, "return 1", "return 10", 1)
	if err := h.fs.WriteFileSafe("/host/SRCA/calc.go", []byte(refactor)); err != nil {
		t.Fatal(err)
	}

	rc, logs := h.runCmd("rename", "calc.X", "calc.Z")
	if rc != 0 || len(logs) != 1 || logs[0] != "flint: rename ok calc.X calc.Z" {
		t.Fatalf("rename refactor: rc=%d logs=%v", rc, logs)
	}
	stage, err := LoadWorkspaceStage(h.fs, h.root, "a")
	if err != nil {
		t.Fatal(err)
	}
	var ren, mod *Op
	for i := range stage {
		switch stage[i].Type {
		case "rename":
			ren = &stage[i]
		case "modify":
			mod = &stage[i]
		}
	}
	if ren == nil || mod == nil || len(stage) != 2 {
		t.Fatalf("rename+modify must stage a refactor pair, got %+v", stage)
	}
	if ren.AtomID != before.ID || mod.AtomID != before.ID || ren.OldHash != before.Hash || mod.OldHash != before.Hash || mod.NewHash == before.Hash {
		t.Fatalf("bad refactor pair before=%+v rename=%+v modify=%+v", before, *ren, *mod)
	}
}

func TestMvStagesCommitAndCheckoutRoundTrip(t *testing.T) {
	h := newTestHarness(t)
	writeRenameFixture(t, h)
	before := atomByName(t, h, "a", "calc.Y")

	calcWithoutY := `package calc

func X() int {
	return 1
}
`
	utilWithY := `package calc

func Y() int {
	return 2
}
`
	if err := h.fs.WriteFileSafe("/host/SRCA/calc.go", []byte(calcWithoutY)); err != nil {
		t.Fatal(err)
	}
	if err := h.fs.WriteFileSafe("/host/SRCA/util.go", []byte(utilWithY)); err != nil {
		t.Fatal(err)
	}
	rc, logs := h.runCmd("mv", "calc.Y", "util.go")
	if rc != 0 || len(logs) != 1 || logs[0] != "flint: mv ok calc.Y util.go" {
		t.Fatalf("mv: rc=%d logs=%v", rc, logs)
	}

	stage, err := LoadWorkspaceStage(h.fs, h.root, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(stage) != 1 || stage[0].Type != "move" || stage[0].AtomID != before.ID || stage[0].NewSpan == nil || stage[0].NewSpan.File != "util.go" || stage[0].OldHash != before.Hash {
		t.Fatalf("stage after mv: %+v want one move op for %s", stage, before.ID)
	}
	after := atomByName(t, h, "a", "calc.Y")
	if after.ID != before.ID || after.Span.File != "util.go" || after.Hash != before.Hash {
		t.Fatalf("move must preserve id/hash and retarget span: before %+v after %+v", before, after)
	}
	rc, logs = h.runCmd("status")
	if rc != 0 || len(logs) < 2 || !strings.Contains(logs[0], " move=1") || logs[1] != "flint: stage move calc.Y" {
		t.Fatalf("status must name the move op: rc=%d logs=%v", rc, logs)
	}
	rc, logs = h.runCmd("diff")
	if rc != 0 || len(logs) < 2 || !strings.Contains(logs[0], " move=1") || logs[1] != "flint: diff move util.go calc.Y" {
		t.Fatalf("diff must name the move op: rc=%d logs=%v", rc, logs)
	}

	if rc, logs := h.runCmd("commit", "-m", "move Y"); rc != 0 {
		t.Fatalf("commit move: %v", logs)
	}
	rc, logs = h.runCmd("checkout", "--stream=main", "--dir=/host/OUT")
	if rc != 0 || len(logs) != 1 || logs[0] != "flint: checkout ok stream=main files=2" {
		t.Fatalf("checkout move: rc=%d logs=%v", rc, logs)
	}
	gotCalc, _ := h.fs.ReadFile("/host/OUT/calc.go", MaxFileReadBytes)
	gotUtil, _ := h.fs.ReadFile("/host/OUT/util.go", MaxFileReadBytes)
	if !bytes.Equal(gotCalc, []byte(calcWithoutY)) || !bytes.Equal(gotUtil, []byte(utilWithY)) {
		t.Fatalf("checkout did not round-trip moved source:\ncalc:\n%s\nutil:\n%s", gotCalc, gotUtil)
	}
}

func TestRenameRefusesMissingTargetAndDuplicateName(t *testing.T) {
	h := newTestHarness(t)
	writeRenameFixture(t, h)

	// The working tree has not been edited, so the explicit target is absent.
	rc, logs := h.runCmd("rename", "calc.X", "calc.Z")
	if rc != 1 || len(logs) != 1 || logs[0] != "flint: refuse atom_missing calc.Z" {
		t.Fatalf("expected atom_missing calc.Z, got rc=%d logs=%v", rc, logs)
	}

	renamedToY := strings.Replace(renameBaseGo, "func X()", "func Y()", 1)
	if err := h.fs.WriteFileSafe("/host/SRCA/calc.go", []byte(renamedToY)); err != nil {
		t.Fatal(err)
	}
	rc, logs = h.runCmd("rename", "calc.X", "calc.Y")
	if rc != 1 || len(logs) != 1 || !strings.HasPrefix(logs[0], "flint: refuse duplicate_decl calc.go fn calc.Y") {
		t.Fatalf("expected duplicate_decl, got rc=%d logs=%v", rc, logs)
	}
	if h.fs.Exists(workspaceStagePath(h.root, "a")) {
		t.Fatal("refused rename must not write STAGE.jsonl")
	}
}

func TestFoldStageOpsComposesMoveAndRename(t *testing.T) {
	id := "0199c82c-c000-7001-81b0-61d041f02110"
	h1 := strings.Repeat("11", 32)
	sp1 := &Span{File: "a.go", Start: 1, End: 2}
	sp2 := &Span{File: "b.go", Start: 3, End: 4}

	out := FoldStageOps([]Op{{Type: "rename", AtomID: id, NewName: "pkg.A", OldHash: h1}},
		[]Op{{Type: "rename", AtomID: id, NewName: "pkg.B", OldHash: h1}}, nil)
	if len(out) != 1 || out[0].Type != "rename" || out[0].NewName != "pkg.B" || out[0].OldHash != h1 {
		t.Fatalf("rename+rename fold: %+v", out)
	}

	out = FoldStageOps([]Op{{Type: "move", AtomID: id, NewSpan: sp1, OldHash: h1}},
		[]Op{{Type: "move", AtomID: id, NewSpan: sp2, OldHash: h1}}, nil)
	if len(out) != 1 || out[0].Type != "move" || out[0].NewSpan == nil || out[0].NewSpan.File != "b.go" || out[0].OldHash != h1 {
		t.Fatalf("move+move fold: %+v", out)
	}

	out = FoldStageOps([]Op{{Type: "add", AtomID: id, Kind: "fn", NewName: "pkg.A", NewSpan: sp1, NewHash: h1}},
		[]Op{
			{Type: "rename", AtomID: id, NewName: "pkg.B", OldHash: h1},
			{Type: "move", AtomID: id, NewSpan: sp2, OldHash: h1},
		}, nil)
	if len(out) != 1 || out[0].Type != "add" || out[0].NewName != "pkg.B" || out[0].NewSpan == nil || out[0].NewSpan.File != "b.go" {
		t.Fatalf("add+rename+move must remain one add op: %+v", out)
	}
}

func TestCheckoutReadsLegacyM215aBlobKeys(t *testing.T) {
	h := newTestHarness(t)
	writeRenameFixture(t, h)

	atoms, err := LoadWorkspaceAtoms(h.fs, h.root, "a")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range atoms {
		newKey, err := atomBodyKey(a)
		if err != nil {
			t.Fatal(err)
		}
		body, err := h.fs.ReadFile(blobPath(h.root, newKey), MaxFileReadBytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.fs.WriteFileSafe(blobPath(h.root, a.Hash), body); err != nil {
			t.Fatal(err)
		}
		if err := h.fs.Remove(blobPath(h.root, newKey)); err != nil {
			t.Fatal(err)
		}
	}
	newFileKey, err := fileBlobKey("calc.go", atoms)
	if err != nil {
		t.Fatal(err)
	}
	legacyFileKey, err := fileBlobKeyV1("calc.go", atoms)
	if err != nil {
		t.Fatal(err)
	}
	fileBody, err := h.fs.ReadFile(blobPath(h.root, newFileKey), MaxFileReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.fs.WriteFileSafe(blobPath(h.root, legacyFileKey), fileBody); err != nil {
		t.Fatal(err)
	}
	if err := h.fs.Remove(blobPath(h.root, newFileKey)); err != nil {
		t.Fatal(err)
	}

	rc, logs := h.runCmd("checkout", "--stream=main", "--dir=/host/LEGACY")
	if rc != 0 || len(logs) != 1 || logs[0] != "flint: checkout ok stream=main files=1" {
		t.Fatalf("legacy-key checkout: rc=%d logs=%v", rc, logs)
	}
	got, err := h.fs.ReadFile("/host/LEGACY/calc.go", MaxFileReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte(renameBaseGo)) {
		t.Fatalf("legacy-key checkout did not round-trip:\n%s", got)
	}

	// Remove the v1 file body too: canonical synthesis must fall back to the
	// legacy hash-only Atom body keys as well.
	if err := h.fs.Remove(blobPath(h.root, legacyFileKey)); err != nil {
		t.Fatal(err)
	}
	if err := h.fs.Remove("/host/LEGACY/calc.go"); err != nil {
		t.Fatal(err)
	}
	rc, logs = h.runCmd("checkout", "--stream=main", "--dir=/host/LEGACY")
	if rc != 0 {
		t.Fatalf("legacy atom-key canonical checkout: rc=%d logs=%v", rc, logs)
	}
	got, err = h.fs.ReadFile("/host/LEGACY/calc.go", MaxFileReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("func X() int")) || !bytes.Contains(got, []byte("func Y() int")) {
		t.Fatalf("legacy atom-key synthesis missing bodies:\n%s", got)
	}
}

func TestMvAcrossGoPackagesStagesMoveRenamePair(t *testing.T) {
	h := newTestHarness(t)
	writeRenameFixture(t, h)
	before := atomByName(t, h, "a", "calc.Y")

	calcWithoutY := `package calc

func X() int {
	return 1
}
`
	utilOtherY := `package other

func Y() int {
	return 2
}
`
	if err := h.fs.WriteFileSafe("/host/SRCA/calc.go", []byte(calcWithoutY)); err != nil {
		t.Fatal(err)
	}
	if err := h.fs.WriteFileSafe("/host/SRCA/util.go", []byte(utilOtherY)); err != nil {
		t.Fatal(err)
	}
	rc, logs := h.runCmd("mv", "calc.Y", "util.go")
	if rc != 0 || len(logs) != 1 || logs[0] != "flint: mv ok calc.Y util.go" {
		t.Fatalf("cross-package mv: rc=%d logs=%v", rc, logs)
	}
	stage, err := LoadWorkspaceStage(h.fs, h.root, "a")
	if err != nil {
		t.Fatal(err)
	}
	var mv, ren *Op
	for i := range stage {
		switch stage[i].Type {
		case "move":
			mv = &stage[i]
		case "rename":
			ren = &stage[i]
		}
	}
	if len(stage) != 2 || mv == nil || ren == nil || mv.AtomID != before.ID || ren.AtomID != before.ID || ren.NewName != "other.Y" {
		t.Fatalf("cross-package move must stage move+rename on one id: %+v", stage)
	}
	after := atomByName(t, h, "a", "other.Y")
	if after.ID != before.ID || after.Hash != before.Hash || after.Span.File != "util.go" {
		t.Fatalf("cross-package move identity: before %+v after %+v", before, after)
	}
}

func setupRenameMergeFork(t *testing.T, h *testHarness) {
	t.Helper()
	for _, dir := range []string{"/host/SRCA", "/host/SRCB"} {
		if err := h.fs.WriteFileSafe(dir+"/calc.go", []byte(renameBaseGo)); err != nil {
			t.Fatal(err)
		}
	}
	if rc, logs := h.runCmd("init", "--name=a", "--src=/host/SRCA"); rc != 0 {
		t.Fatalf("init a: %v", logs)
	}
	if rc, logs := h.runCmd("add", "/host/SRCA"); rc != 0 {
		t.Fatalf("add a: %v", logs)
	}
	if rc, logs := h.runCmd("commit", "-m", "base"); rc != 0 {
		t.Fatalf("commit base: %v", logs)
	}
	if rc, logs := h.runCmd("fork", "main", "feature"); rc != 0 {
		t.Fatalf("fork: %v", logs)
	}
	if rc, logs := h.runCmd("init", "--name=b", "--src=/host/SRCB", "--stream=feature"); rc != 0 {
		t.Fatalf("init b: %v", logs)
	}
}

func commitFeatureRename(t *testing.T, h *testHarness, newFunc, newName string) {
	t.Helper()
	src := strings.Replace(renameBaseGo, "func X()", "func "+newFunc+"()", 1)
	if err := h.fs.WriteFileSafe("/host/SRCB/calc.go", []byte(src)); err != nil {
		t.Fatal(err)
	}
	if rc, logs := h.runCmd("rename", "--ws=b", "calc.X", newName); rc != 0 {
		t.Fatalf("feature rename: %v", logs)
	}
	if rc, logs := h.runCmd("commit", "--ws=b", "-m", "feature rename"); rc != 0 {
		t.Fatalf("feature commit: %v", logs)
	}
}

func TestRenameMergeConflictsEndToEnd(t *testing.T) {
	t.Run("modify_vs_rename", func(t *testing.T) {
		h := newTestHarness(t)
		setupRenameMergeFork(t, h)
		commitFeatureRename(t, h, "Z", "calc.Z")

		mainSrc := strings.Replace(renameBaseGo, "return 1", "return 10", 1)
		if err := h.fs.WriteFileSafe("/host/SRCA/calc.go", []byte(mainSrc)); err != nil {
			t.Fatal(err)
		}
		if rc, logs := h.runCmd("add", "/host/SRCA"); rc != 0 {
			t.Fatalf("main add: %v", logs)
		}
		if rc, logs := h.runCmd("commit", "-m", "main modify"); rc != 0 {
			t.Fatalf("main commit: %v", logs)
		}
		rc, logs := h.runCmd("merge", "feature")
		want := []string{
			"flint: merge conflict atom=calc.X type=modify_vs_rename",
			"flint: merge conflicts=1",
		}
		if rc != 2 || strings.Join(logs, "\n") != strings.Join(want, "\n") {
			t.Fatalf("modify_vs_rename merge: rc=%d logs=%v", rc, logs)
		}
	})

	t.Run("concurrent_rename", func(t *testing.T) {
		h := newTestHarness(t)
		setupRenameMergeFork(t, h)
		commitFeatureRename(t, h, "Z", "calc.Z")

		mainSrc := strings.Replace(renameBaseGo, "func X()", "func W()", 1)
		if err := h.fs.WriteFileSafe("/host/SRCA/calc.go", []byte(mainSrc)); err != nil {
			t.Fatal(err)
		}
		if rc, logs := h.runCmd("rename", "--ws=a", "calc.X", "calc.W"); rc != 0 {
			t.Fatalf("main rename: %v", logs)
		}
		if rc, logs := h.runCmd("commit", "--ws=a", "-m", "main rename"); rc != 0 {
			t.Fatalf("main commit: %v", logs)
		}
		rc, logs := h.runCmd("merge", "feature")
		want := []string{
			"flint: merge conflict atom=calc.X type=concurrent_rename",
			"flint: merge conflicts=1",
		}
		if rc != 2 || strings.Join(logs, "\n") != strings.Join(want, "\n") {
			t.Fatalf("concurrent_rename merge: rc=%d logs=%v", rc, logs)
		}
	})
}
