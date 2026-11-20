package main

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"virelai/flint/parser"
)

const checkoutGoSrc = `package calc

import "fmt"

// A doc comment that lives outside every Atom span.
const (
	A = iota
	B
	C = 5
)

var X, Y = 1, 2

func Add(a, b int) int {
	return a + b
}

func (p Point) Norm() int {
	return p.X * p.Y
}

type Point struct {
	X int
	Y int
}

func usesFmt() {
	fmt.Println(Add(X, Y))
}
`

const checkoutZigSrc = `const std = @import("std");

pub fn add(a: i32, b: i32) i32 {
    return a + b;
}

test "add works" {
    try std.testing.expectEqual(@as(i32, 3), add(1, 2));
}
`

func writeFile(t *testing.T, h *testHarness, path string, src []byte) {
	t.Helper()
	if err := h.fs.WriteFileSafe(path, src); err != nil {
		t.Fatal(err)
	}
}

// parseTree parses each named file under dir and returns the decls as
// StoredAtoms (id unset — identity here is content, not lineage).
func parseTree(t *testing.T, h *testHarness, dir string, rels []string) []StoredAtom {
	t.Helper()
	var out []StoredAtom
	for _, rel := range rels {
		src, err := h.fs.ReadFile(dir+"/"+rel, MaxFileReadBytes)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		p, ok := parser.ForPath(rel)
		if !ok {
			t.Fatalf("no parser for %s", rel)
		}
		decls, err := p.Parse(rel, src)
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		for _, d := range decls {
			out = append(out, StoredAtomFromParser("", d))
		}
	}
	return out
}

func replayHeadAtoms(t *testing.T, h *testHarness, streamName string) []StoredAtom {
	t.Helper()
	idx, err := LoadStoreIndex(h.fs, h.root)
	if err != nil {
		t.Fatal(err)
	}
	st := idx.StreamsByName[streamName]
	if st == nil {
		t.Fatalf("stream %s missing", streamName)
	}
	all, err := LoadAllStreamChanges(h.fs, h.root, idx)
	if err != nil {
		t.Fatal(err)
	}
	atoms, _, err := ReplayStreamAtoms(st.Head, all)
	if err != nil {
		t.Fatal(err)
	}
	return atoms
}

func sortAtomsForCompare(atoms []StoredAtom) {
	sort.Slice(atoms, func(i, j int) bool {
		if atoms[i].Span.File != atoms[j].Span.File {
			return atoms[i].Span.File < atoms[j].Span.File
		}
		return atoms[i].Span.Start < atoms[j].Span.Start
	})
}

// TestCheckoutRoundTrip proves the card's central property:
// parse -> commit -> checkout -> re-parse yields an identical Atom table,
// byte-exact source included (the verbatim file-body path).
func TestCheckoutRoundTrip(t *testing.T) {
	h := newTestHarness(t)

	writeFile(t, h, "/host/src/calc.go", []byte(checkoutGoSrc))
	writeFile(t, h, "/host/src/util.zig", []byte(checkoutZigSrc))
	if rc, logs := h.runCmd("init", "--name=w", "--src=/host/src"); rc != 0 {
		t.Fatalf("init: %v", logs)
	}
	if rc, logs := h.runCmd("add", "/host/src"); rc != 0 {
		t.Fatalf("add: %v", logs)
	}
	if rc, logs := h.runCmd("commit", "-m", "base"); rc != 0 {
		t.Fatalf("commit: %v", logs)
	}

	atomsBefore, err := LoadWorkspaceAtoms(h.fs, h.root, "w")
	if err != nil {
		t.Fatal(err)
	}
	if len(atomsBefore) == 0 {
		t.Fatal("expected atoms in the workspace table")
	}

	rc, logs := h.runCmd("checkout", "--stream=main", "--dir=/host/out")
	if rc != 0 || len(logs) != 1 || logs[0] != "flint: checkout ok stream=main files=2" {
		t.Fatalf("checkout: rc=%d logs=%v", rc, logs)
	}

	for _, rel := range []string{"calc.go", "util.zig"} {
		got, err := h.fs.ReadFile("/host/out/"+rel, MaxFileReadBytes)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		want, err := h.fs.ReadFile("/host/src/"+rel, MaxFileReadBytes)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: emitted bytes differ from committed source", rel)
		}
	}

	// Re-parse the emitted tree and require the identical Atom table,
	// spans included (the verbatim path reproduces original bytes).
	decls := parseTree(t, h, "/host/out", []string{"calc.go", "util.zig"})
	stored := replayHeadAtoms(t, h, "main")
	if len(decls) != len(stored) {
		t.Fatalf("re-parse atoms=%d want %d", len(decls), len(stored))
	}
	sortAtomsForCompare(decls)
	sortAtomsForCompare(stored)
	for i := range decls {
		d, s := decls[i], stored[i]
		if d.Kind != s.Kind || d.Name != s.Name || d.Hash != s.Hash ||
			d.SigHash != s.SigHash || d.Span != s.Span {
			t.Fatalf("atom[%d] mismatch:\nparsed: %+v\nstored: %+v", i, d, s)
		}
	}
}

// TestCheckoutSrcBehindGuard: a diverged destination file refuses instead
// of being clobbered.
func TestCheckoutSrcBehindGuard(t *testing.T) {
	h := newTestHarness(t)
	writeFile(t, h, "/host/src/calc.go", []byte(checkoutGoSrc))
	h.runCmd("init", "--name=w", "--src=/host/src")
	h.runCmd("add", "/host/src")
	h.runCmd("commit", "-m", "base")

	if rc, logs := h.runCmd("checkout", "--stream=main", "--dir=/host/out"); rc != 0 {
		t.Fatalf("first checkout: rc=%d logs=%v", rc, logs)
	}
	// Idempotent: a second identical checkout writes nothing and succeeds.
	if rc, logs := h.runCmd("checkout", "--stream=main", "--dir=/host/out"); rc != 0 {
		t.Fatalf("second checkout: rc=%d logs=%v", rc, logs)
	}

	diverged := []byte(strings.Replace(checkoutGoSrc, "return a + b", "return a * b", 1))
	writeFile(t, h, "/host/out/calc.go", diverged)
	rc, logs := h.runCmd("checkout", "--stream=main", "--dir=/host/out")
	if rc != 1 || len(logs) != 1 || logs[0] != "flint: refuse src_behind calc.go" {
		t.Fatalf("expected src_behind calc.go, got rc=%d logs=%v", rc, logs)
	}
	got, _ := h.fs.ReadFile("/host/out/calc.go", MaxFileReadBytes)
	if !bytes.Equal(got, diverged) {
		t.Fatal("src_behind refusal must not clobber the working copy")
	}
}

// TestCheckoutBodyMissing: a store whose blob lane is absent (every
// pre-content-store history) refuses body_missing by name rather than
// emitting empty files.
func TestCheckoutBodyMissing(t *testing.T) {
	h := newTestHarness(t)
	writeFile(t, h, "/host/src/calc.go", []byte(checkoutGoSrc))
	h.runCmd("init", "--name=w", "--src=/host/src")
	h.runCmd("add", "/host/src")
	h.runCmd("commit", "-m", "base")

	entries, err := h.fs.List(blobsDir(h.root))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected blobs to exist after add+commit")
	}
	for _, e := range entries {
		if err := h.fs.Remove(blobPath(h.root, e.Name)); err != nil {
			t.Fatal(err)
		}
	}

	rc, logs := h.runCmd("checkout", "--stream=main", "--dir=/host/out")
	if rc != 1 || len(logs) != 1 || !strings.HasPrefix(logs[0], "flint: refuse body_missing calc.go") {
		t.Fatalf("expected body_missing, got rc=%d logs=%v", rc, logs)
	}
	if h.fs.Exists("/host/out/calc.go") {
		t.Fatal("body_missing refusal must not write partial files")
	}
}

// TestCheckoutPinnedPreContentStream seeds a store from the pinned
// testdata/sig/good.jsonl Change (signed history written by a store that
// never had the blob lane) and proves checkout refuses by name rather than
// emitting anything. The vector's modify op references a never-added Atom,
// so the replayed table carries a spanless Atom and the named refusal is
// atom_malformed; the body_missing half is TestCheckoutBodyMissing.
func TestCheckoutPinnedPreContentStream(t *testing.T) {
	h := newTestHarness(t)
	goodRaw, err := os.ReadFile("testdata/sig/good.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var change Change
	if err := json.Unmarshal(bytes.TrimSpace(goodRaw), &change); err != nil {
		t.Fatal(err)
	}
	if err := h.fs.WriteFileSafe(streamLogPath(h.root, change.Stream), goodRaw); err != nil {
		t.Fatal(err)
	}
	rec := Stream{ID: change.Stream, Name: "main", Head: &change.ID, Created: change.TS}
	if err := WriteJSONSafe(h.fs, streamRecordPath(h.root, change.Stream), rec); err != nil {
		t.Fatal(err)
	}

	rc, logs := h.runCmd("checkout", "--stream=main", "--dir=/host/out")
	if rc != 1 || len(logs) != 1 || !strings.HasPrefix(logs[0], "flint: refuse atom_malformed") {
		t.Fatalf("expected atom_malformed on the pinned vector, got rc=%d logs=%v", rc, logs)
	}
}

// TestCheckoutCanonicalSynthesis exercises the fallback path: the
// per-Atom bodies exist but the file-version blob does not — the emitter
// synthesizes a canonical file whose re-parse reproduces the Atom table.
func TestCheckoutCanonicalSynthesis(t *testing.T) {
	h := newTestHarness(t)
	writeFile(t, h, "/host/src/calc.go", []byte(checkoutGoSrc))
	h.runCmd("init", "--name=w", "--src=/host/src")
	h.runCmd("add", "/host/src")
	h.runCmd("commit", "-m", "base")

	atoms, err := LoadWorkspaceAtoms(h.fs, h.root, "w")
	if err != nil {
		t.Fatal(err)
	}
	group := fileAtomGroup(atoms, "calc.go")
	sortStoredAtoms(group)
	key, err := fileBlobKey("calc.go", group)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.fs.Remove(blobPath(h.root, key)); err != nil {
		t.Fatal(err)
	}

	rc, logs := h.runCmd("checkout", "--stream=main", "--dir=/host/out")
	if rc != 0 || len(logs) != 1 || logs[0] != "flint: checkout ok stream=main files=1" {
		t.Fatalf("canonical checkout: rc=%d logs=%v", rc, logs)
	}
	got, err := h.fs.ReadFile("/host/out/calc.go", MaxFileReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := parser.ForPath("calc.go")
	decls, err := p.Parse("calc.go", got)
	if err != nil {
		t.Fatalf("synthesized file does not parse: %v", err)
	}
	if !sameAtomIdentities(decls, group) {
		t.Fatal("synthesized file re-parses to a different Atom set")
	}
}
