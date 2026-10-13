package parser

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite MANIFEST.json and canon/ fixtures")

const corpusDir = "../testdata/corpus"

// manifestJSON is the pinned schema for testdata/corpus/MANIFEST.json:
// every corpus atom, sorted by (file, start), hashes lowercase hex.
type manifestJSON struct {
	Version int            `json:"version"`
	Files   int            `json:"files"`
	Atoms   []manifestAtom `json:"atoms"`
}

type manifestAtom struct {
	File    string `json:"file"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Start   uint32 `json:"start"`
	End     uint32 `json:"end"`
	Hash    string `json:"hash"`
	SigHash string `json:"sig_hash"`
}

// corpusFiles returns the sorted corpus-relative file list
// (go/*.go + zig/*.zig); refuse/ is not corpus input.
func corpusFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, sub := range []string{"go", "zig"} {
		ents, err := os.ReadDir(filepath.Join(corpusDir, sub))
		if err != nil {
			t.Fatalf("corpus: %v", err)
		}
		for _, e := range ents {
			files = append(files, sub+"/"+e.Name())
		}
	}
	sort.Strings(files)
	return files
}

func parseCorpus(t *testing.T) []Atom {
	t.Helper()
	var atoms []Atom
	for _, rel := range corpusFiles(t) {
		src, err := os.ReadFile(filepath.Join(corpusDir, rel))
		if err != nil {
			t.Fatalf("corpus: %v", err)
		}
		p, ok := ForPath(rel)
		if !ok {
			t.Fatalf("corpus: no parser for %s", rel)
		}
		as, err := p.Parse(rel, src)
		if err != nil {
			t.Fatalf("corpus: %s: %v", rel, err)
		}
		atoms = append(atoms, as...)
	}
	sort.Slice(atoms, func(i, j int) bool {
		if atoms[i].File != atoms[j].File {
			return atoms[i].File < atoms[j].File
		}
		return atoms[i].Start < atoms[j].Start
	})
	return atoms
}

// TestCorpus: all 100 corpus files parse to exactly MANIFEST.json.
func TestCorpus(t *testing.T) {
	files := corpusFiles(t)
	if len(files) != 100 {
		t.Fatalf("corpus: got %d files, want 100", len(files))
	}
	atoms := parseCorpus(t)

	// Kind coverage (card: every Go and Zig kind at least three times).
	wantGo := map[string]int{
		"fn": 3, "method": 3, "test": 3, "struct": 3, "interface": 3,
		"type": 3, "type_alias": 3, "const": 3, "var": 3,
	}
	wantZig := map[string]int{
		"fn": 3, "struct": 3, "enum": 3, "union": 3,
		"const": 3, "var": 3, "test": 3,
	}
	got := map[string]int{}
	for _, a := range atoms {
		got[strings.SplitN(a.File, "/", 2)[0]+"/"+a.Kind]++
	}
	for k, n := range wantGo {
		if got["go/"+k] < n {
			t.Errorf("kind coverage: go %s x%d, want >= %d", k, got["go/"+k], n)
		}
	}
	for k, n := range wantZig {
		if got["zig/"+k] < n {
			t.Errorf("kind coverage: zig %s x%d, want >= %d", k, got["zig/"+k], n)
		}
	}

	want := manifestJSON{Version: 1, Files: len(files)}
	for _, a := range atoms {
		want.Atoms = append(want.Atoms, manifestAtom{
			File: a.File, Kind: a.Kind, Name: a.Name,
			Start: a.Start, End: a.End,
			Hash:    hex.EncodeToString(a.Hash[:]),
			SigHash: hex.EncodeToString(a.SigHash[:]),
		})
	}
	if *update {
		writeManifest(t, want)
		writeCanon(t, atoms)
		return
	}
	raw, err := os.ReadFile(filepath.Join(corpusDir, "MANIFEST.json"))
	if err != nil {
		t.Fatalf("corpus: MANIFEST.json: %v (regenerate with -update)", err)
	}
	var man manifestJSON
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatalf("corpus: MANIFEST.json: %v", err)
	}
	if man.Files != want.Files || len(man.Atoms) != len(want.Atoms) {
		t.Fatalf("corpus: manifest files=%d atoms=%d, want files=%d atoms=%d",
			man.Files, len(man.Atoms), want.Files, len(want.Atoms))
	}
	for i := range want.Atoms {
		if man.Atoms[i] != want.Atoms[i] {
			t.Errorf("corpus: atom %d mismatch:\n got  %+v\n want %+v", i, man.Atoms[i], want.Atoms[i])
		}
	}
}

func writeManifest(t *testing.T, man manifestJSON) {
	t.Helper()
	out, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(filepath.Join(corpusDir, "MANIFEST.json"), out, 0644); err != nil {
		t.Fatal(err)
	}
}

// Pinned canon/ fixtures: byte-exact canonical streams for a spread of
// kinds across both languages.
var canonPins = [][2]string{
	{"go/sample.go", "sample.Add"},
	{"go/iota.go", "flags.Green"},
	{"go/methods.go", "shapes.Circle.Area"},
	{"zig/sample.zig", "sample.Point"},
	{"zig/sample.zig", "sample.test:add works"},
	{"zig/quals.zig", "quals.three"},
	{"zig/containers.zig", "containers.U"},
}

func canonName(file, name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, file+"__"+name) + ".txt"
}

func writeCanon(t *testing.T, atoms []Atom) {
	t.Helper()
	dir := "../testdata/canon"
	for _, pin := range canonPins {
		var found *Atom
		for i := range atoms {
			if atoms[i].File == pin[0] && atoms[i].Name == pin[1] {
				found = &atoms[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("canon pin %s %s: not an atom", pin[0], pin[1])
		}
		body := "# " + pin[0] + " " + found.Kind + " " + found.Name + "\n" +
			found.canon + "\n==SIG==\n" + found.sig + "\n"
		if err := os.WriteFile(filepath.Join(dir, canonName(pin[0], pin[1])), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCanon checks the pinned canonical streams byte-for-byte.
func TestCanon(t *testing.T) {
	if *update {
		t.Skip("canon/ is regenerated by TestCorpus -update")
	}
	atoms := parseCorpus(t)
	for _, pin := range canonPins {
		var found *Atom
		for i := range atoms {
			if atoms[i].File == pin[0] && atoms[i].Name == pin[1] {
				found = &atoms[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("canon pin %s %s: not an atom", pin[0], pin[1])
		}
		want := "# " + pin[0] + " " + found.Kind + " " + found.Name + "\n" +
			found.canon + "\n==SIG==\n" + found.sig + "\n"
		path := filepath.Join("../testdata/canon", canonName(pin[0], pin[1]))
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("canon: %v (regenerate with -update)", err)
		}
		if string(raw) != want {
			t.Errorf("canon %s mismatch:\n got %q\nwant %q", path, raw, want)
		}
	}
}

// TestPairs pins §3's consequences: rename-stable, whitespace-stable,
// body moves hash only, param/field moves both.
func TestPairs(t *testing.T) {
	atoms := parseCorpus(t)
	byName := map[string]Atom{}
	for _, a := range atoms {
		byName[a.File+"\x00"+a.Name] = a
	}
	pairs := []struct {
		aFile, aName, bFile, bName string
		hashEq, sigEq              bool
	}{
		{"go/pair_rename_a.go", "pairex.compute", "go/pair_rename_b.go", "pairex.computeRenamed", true, true},
		{"go/pair_body_a.go", "pairex.shrink", "go/pair_body_b.go", "pairex.shrink", false, true},
		{"go/pair_ws_a.go", "pairex.spacey", "go/pair_ws_b.go", "pairex.spacey", true, true},
		{"go/pair_param_a.go", "pairex.takes", "go/pair_param_b.go", "pairex.takes", false, false},
		{"go/pair_field_a.go", "pairex.Pair", "go/pair_field_b.go", "pairex.Pair", false, false},
		{"zig/pair_rename_a.zig", "pair_rename_a.compute", "zig/pair_rename_b.zig", "pair_rename_b.computeRenamed", true, true},
		{"zig/pair_body_a.zig", "pair_body_a.shrink", "zig/pair_body_b.zig", "pair_body_b.shrink", false, true},
		{"zig/pair_ws_a.zig", "pair_ws_a.spacey", "zig/pair_ws_b.zig", "pair_ws_b.spacey", true, true},
		{"zig/pair_param_a.zig", "pair_param_a.takes", "zig/pair_param_b.zig", "pair_param_b.takes", false, false},
		{"zig/pair_field_a.zig", "pair_field_a.Pair", "zig/pair_field_b.zig", "pair_field_b.Pair", false, false},
	}
	for _, p := range pairs {
		a, aok := byName[p.aFile+"\x00"+p.aName]
		b, bok := byName[p.bFile+"\x00"+p.bName]
		if !aok || !bok {
			t.Errorf("pair %s:%s vs %s:%s: atom missing", p.aFile, p.aName, p.bFile, p.bName)
			continue
		}
		if (a.Hash == b.Hash) != p.hashEq {
			t.Errorf("pair %s vs %s: hash eq=%v, want %v", p.aName, p.bName, a.Hash == b.Hash, p.hashEq)
		}
		if (a.SigHash == b.SigHash) != p.sigEq {
			t.Errorf("pair %s vs %s: sig_hash eq=%v, want %v", p.aName, p.bName, a.SigHash == b.SigHash, p.sigEq)
		}
	}
}

// TestRefusals: constructs the scanner will not guess at come back with
// the right ParseError codes, never atoms.
func TestRefusals(t *testing.T) {
	cases := []struct {
		file, code string
	}{
		{"refuse/anon_ret.zig", "parse_unsupported"},
		{"refuse/bad.go", "syntax"},
	}
	for _, c := range cases {
		src, err := os.ReadFile(filepath.Join(corpusDir, c.file))
		if err != nil {
			t.Fatalf("refuse: %v", err)
		}
		p, ok := ForPath(c.file)
		if !ok {
			t.Fatalf("refuse: no parser for %s", c.file)
		}
		atoms, err := p.Parse(c.file, src)
		if err == nil {
			t.Errorf("refuse %s: parsed %d atoms, want %s error", c.file, len(atoms), c.code)
			continue
		}
		pe, ok := err.(*ParseError)
		if !ok || pe.Code != c.code {
			t.Errorf("refuse %s: err %v, want ParseError code %s", c.file, err, c.code)
		}
	}
}

// TestZigOracle runs the host std.zig.Ast oracle over every corpus .zig
// file and requires the Go-native scanner to agree (kind/name/span) on
// every declaration. A missing zig binary fails, per the card.
func TestZigOracle(t *testing.T) {
	zig, err := exec.LookPath("zig")
	if err != nil {
		t.Fatal("TestZigOracle: zig binary not found (required, not skippable)")
	}
	files := corpusFiles(t)
	var zigFiles, oracleArgs []string
	for _, f := range files {
		if strings.HasPrefix(f, "zig/") {
			zigFiles = append(zigFiles, f)
			oracleArgs = append(oracleArgs, f)
		}
	}
	oracle := "../../../../../tools/flint/zigdecls.zig" // cwd=testdata/corpus
	args := append([]string{"run", oracle, "--"}, oracleArgs...)
	cmd := exec.Command(zig, args...)
	cmd.Dir = corpusDir // oracle prints file paths as given: zig/x.zig
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("TestZigOracle: oracle failed: %v\n%s", err, out)
	}
	gotSet := map[oracleTup]bool{}
	var got []oracleTup
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("TestZigOracle: bad oracle line %q", line)
		}
		tp := oracleTup{f[1], f[2], f[3], f[4]}
		if gotSet[tp] {
			t.Fatalf("TestZigOracle: duplicate oracle line %q", line)
		}
		gotSet[tp] = true
		got = append(got, tp)
	}
	// Same shape from the Go scanner over the same files.
	var want []oracleTup
	for _, rel := range zigFiles {
		src, err := os.ReadFile(filepath.Join(corpusDir, rel))
		if err != nil {
			t.Fatal(err)
		}
		p, _ := ForPath(rel)
		as, err := p.Parse(rel, src)
		if err != nil {
			t.Fatalf("TestZigOracle: %s: %v", rel, err)
		}
		for _, a := range as {
			want = append(want, oracleTup{a.Kind, a.Name, strconv.Itoa(int(a.Start)), strconv.Itoa(int(a.End))})
		}
	}
	sort.Slice(got, func(i, j int) bool { return tupLess(got[i], got[j]) })
	sort.Slice(want, func(i, j int) bool { return tupLess(want[i], want[j]) })
	if len(got) != len(want) {
		t.Fatalf("TestZigOracle: oracle %d decls, scanner %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("TestZigOracle: decl %d: oracle %+v != scanner %+v", i, got[i], want[i])
		}
	}
}

type oracleTup struct{ kind, name, start, end string }

func tupLess(a, b oracleTup) bool {
	if a.name != b.name {
		return a.name < b.name
	}
	return a.start < b.start
}
