package agentverbs

// The card's pin: every verb's declared cap and both refusal strings —
// unknown_verb for a name the table does not carry, cap_required:<cap>
// for a grant the bitmask does not hold. The refusals are the exact
// strings AGENTD's wire emitted before the lift (M205a).

import (
	"strings"
	"testing"

	"virelai/vi"
)

var allCaps = CapProcess | CapFile | CapWindow | CapInput | CapTerm

// wantTable is the verb set in advertisement order with each verb's
// grant word — the D3 table pinned independently of the code under
// test.
var wantTable = []struct {
	verb string
	word string
	bit  Caps
}{
	{"proc.list", "process", CapProcess},
	{"proc.get", "process", CapProcess},
	{"fs.read", "file", CapFile},
	{"fs.write", "file", CapFile},
	{"ui.windows", "window", CapWindow},
	{"ui.screenshot", "window", CapWindow},
	{"ui.input", "input", CapInput},
	{"ui.geometry", "window", CapWindow},
	{"gosh.run", "term", CapTerm},
}

func TestVerbCapTable(t *testing.T) {
	d := New(Config{})

	// unknown_verb: a name the table does not carry refuses by name
	// regardless of the grant set — none held and all held alike.
	for _, caps := range []Caps{0, allCaps} {
		if _, code := d.Dispatch(&Session{Caps: caps}, &Request{Verb: "no.such"}); code != "unknown_verb" {
			t.Fatalf("caps %v: unknown verb code = %q want unknown_verb", caps, code)
		}
	}

	for _, e := range wantTable {
		// The verb's declared cap is the table row's.
		if verbCap[e.verb] != e.bit {
			t.Fatalf("%s cap = %d want %d", e.verb, verbCap[e.verb], e.bit)
		}
		if CapWord(e.word) != e.bit {
			t.Fatalf("CapWord(%q) = %d want %d", e.word, CapWord(e.word), e.bit)
		}
		// Missing cap refuses cap_required:<word> — before any
		// argument check (the request here has no fields at all), and
		// even with every OTHER cap held.
		if _, code := d.Dispatch(&Session{Caps: 0}, &Request{Verb: e.verb}); code != "cap_required:"+e.word {
			t.Fatalf("%s no caps: code = %q want cap_required:%s", e.verb, code, e.word)
		}
		if _, code := d.Dispatch(&Session{Caps: allCaps &^ e.bit, AllowExec: true},
			&Request{Verb: e.verb}); code != "cap_required:"+e.word {
			t.Fatalf("%s other caps: code = %q want cap_required:%s", e.verb, code, e.word)
		}
	}

	// ui.geometry's second gate: the verb takes `window`, and move/sash
	// take `input` inside the verb — a window-only session queries but
	// refuses the drive ops by name before a seam runs.
	req := &Request{Verb: "ui.geometry", Fields: map[string]Field{
		"win":   {Kind: FInt, Num: 0},
		"op":    {Kind: FStr, Str: "move"},
		"space": {Kind: FInt, Num: 1},
	}}
	if _, code := d.Dispatch(&Session{Caps: CapWindow}, req); code != "cap_required:input" {
		t.Fatalf("window-only move code = %q want cap_required:input", code)
	}
}

func TestAdvertisedOrder(t *testing.T) {
	got := Advertised(allCaps, true)
	if len(got) != len(wantTable) {
		t.Fatalf("Advertised = %v", got)
	}
	for i, e := range wantTable {
		if got[i] != e.verb {
			t.Fatalf("Advertised[%d] = %q want %q", i, got[i], e.verb)
		}
	}
	// The exec kill switch drops gosh.run alone.
	got = Advertised(allCaps, false)
	for _, v := range got {
		if v == "gosh.run" {
			t.Fatal("gosh.run advertised with exec off")
		}
	}
	if len(got) != len(wantTable)-1 {
		t.Fatalf("Advertised no-exec = %v", got)
	}
	// A cap absent drops exactly its verbs.
	got = Advertised(CapFile, true)
	if len(got) != 2 || got[0] != "fs.read" || got[1] != "fs.write" {
		t.Fatalf("file-only Advertised = %v", got)
	}
}

// One green dispatch through a fake seam proves the wiring: the cap
// gate passes, the verb runs, and the result renders verbatim.
func TestProcListGreen(t *testing.T) {
	d := New(Config{Seams: Seams{
		Procs: func(dst []vi.ProcRow) (int, int64) {
			var r vi.ProcRow
			r.PID = 9
			r.State = vi.ProcRunning
			copy(r.NameBuf[:], "AGENTD.ELF")
			dst[0] = r
			return 1, 1
		},
	}})
	res, code := d.Dispatch(&Session{Caps: CapProcess}, &Request{Verb: "proc.list"})
	want := `{"procs":[{"pid":9,"name":"AGENTD.ELF","state":"running","exit_status":null}]}`
	if code != "" || res != want {
		t.Fatalf("proc.list = %q code %q want %q", res, code, want)
	}
}

// fs.read honours Config.Sandbox + Config.Reserved: the path the seam
// sees is the configured root, and the reserved first segment refuses.
func TestFSReadSandbox(t *testing.T) {
	var gotPath string
	d := New(Config{
		Sandbox:  "/host/AGENT",
		Reserved: "AUDIT",
		Seams: Seams{
			ReadFile: func(path string, max int) ([]byte, int64) {
				gotPath = path
				return []byte("x"), 1
			},
		},
	})
	req := &Request{Verb: "fs.read", Fields: map[string]Field{
		"path": {Kind: FStr, Str: "SEED.TXT"},
	}}
	res, code := d.Dispatch(&Session{Caps: CapFile}, req)
	if code != "" || !strings.Contains(res, `"path":"SEED.TXT"`) {
		t.Fatalf("fs.read = %q code %q", res, code)
	}
	if gotPath != "/host/AGENT/SEED.TXT" {
		t.Fatalf("seam path = %q", gotPath)
	}
	req.Fields["path"] = Field{Kind: FStr, Str: "audit/x"}
	if _, code = d.Dispatch(&Session{Caps: CapFile}, req); code != "path_rejected" {
		t.Fatalf("reserved segment code = %q", code)
	}
}
