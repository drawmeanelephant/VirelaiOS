package main

// act_test.go — the M205c (#3810) pins: the fixed call form's extraction
// and arg typing, the cap gate derived from the library's own table, the
// @emit preset driver, and the whole loop end to end through the view —
// allow dispatches in-process and writes the v3 records, deny records
// the named refusal, a refused cap never shows a dialog, and a reload
// decodes the records the leg wrote.
// Kept in its own file per the card split (conv_v3_test.go is card b's).

import (
	"strings"
	"testing"

	"virelai/agentverbs"
	"virelai/vi"
)

func TestExtractCall(t *testing.T) {
	// (verb, args) the scan should lift; "" is no call; "!" is malformed.
	cases := map[string]struct {
		text, verb, args string
	}{
		"no call":        {"just prose\nno markers here", "", ""},
		"call alone":     {"@call proc.list", "proc.list", ""},
		"call mid-prose": {"let me check\n@call proc.list\ndone", "proc.list", ""},
		"call args":      {"@call fs.read path=C0001.CHAT", "fs.read", "path=C0001.CHAT"},
		"call two args":  {"@call gosh.run cmd=ls timeout_ms=1000", "gosh.run", "cmd=ls timeout_ms=1000"},
		"trailing space": {"@call proc.list  ", "proc.list", ""},
		"not a call":     {"@callback foo\n@called x", "", ""},
		// Malformed: the named refusal, never a guess.
		"bare mark":   {"@call", "!", ""},
		"bad verb":    {"@call PROC.LIST", "!", ""},
		"two calls":   {"@call proc.list\n@call fs.read a=b", "!", ""},
		"arg no eq":   {"@call fs.read C0001.CHAT", "!", ""},
		"arg no name": {"@call fs.read =x", "!", ""},
		"empty value": {"@call fs.read path=", "!", ""},
	}
	for name, c := range cases {
		got, ok := extractCall(c.text)
		if c.verb == "!" {
			if ok {
				t.Fatalf("%s: malformed call accepted", name)
			}
			continue
		}
		if !ok {
			t.Fatalf("%s: valid call refused", name)
		}
		if c.verb == "" {
			if got != nil {
				t.Fatalf("%s: phantom call %+v", name, got)
			}
			continue
		}
		if got == nil || got.verb != c.verb || got.args != c.args {
			t.Fatalf("%s: got %+v want verb=%q args=%q", name, got, c.verb, c.args)
		}
	}
}

// The arg typing is the flat grammar's: booleans, bounded ints, strings.
func TestParseCallArgs(t *testing.T) {
	f, ok := parseCallArgs("pid=7 path=C0001.CHAT flag=true big=2147483648 neg=-1")
	if !ok {
		t.Fatal("valid args refused")
	}
	if f["pid"].Kind != agentverbs.FInt || f["pid"].Num != 7 {
		t.Fatalf("pid = %+v", f["pid"])
	}
	if f["path"].Kind != agentverbs.FStr || f["path"].Str != "C0001.CHAT" {
		t.Fatalf("path = %+v", f["path"])
	}
	if f["flag"].Kind != agentverbs.FBool {
		t.Fatalf("flag = %+v", f["flag"])
	}
	// Over the 2^31-1 bound or signed: strings, not ints — Dispatch's own
	// argInt then answers bad_request, the honest refusal.
	if f["big"].Kind != agentverbs.FStr || f["neg"].Kind != agentverbs.FStr {
		t.Fatalf("big/neg = %+v %+v", f["big"], f["neg"])
	}
	f, ok = parseCallArgs("")
	if !ok || len(f) != 0 {
		t.Fatalf("empty args = %+v %v", f, ok)
	}
}

// capGate is the library's own table under GOASSIST's grants — derived
// through Advertised, so the gate can never drift from Dispatch.
func TestCapGate(t *testing.T) {
	caps := assistCaps()
	for verb, want := range map[string]string{
		"proc.list":     "",
		"proc.get":      "",
		"fs.read":       "",
		"fs.write":      "",
		"ui.windows":    "",
		"ui.screenshot": "",
		"ui.geometry":   "",
		"gosh.run":      "cap_required:term",
		// The pinned honesty: `input` is an agent-keyring word with no
		// caps= spelling, so ui.input never reaches the seat in-process —
		// the refusal lands one layer earlier than agent_input_denied.
		"ui.input":   "cap_required:input",
		"bogus.verb": "unknown_verb",
	} {
		if got := capGate(verb, caps); got != want {
			t.Fatalf("capGate(%q) = %q want %q", verb, got, want)
		}
	}
}

// emittedAnswer drives only the stub persona and only off a system turn.
func TestEmittedAnswer(t *testing.T) {
	h := newFakeHost()
	v := newTestView(h, true)
	v.cur = &conversation{name: "C0001", turns: []turn{
		{role: roleSystem, text: "be terse\n@emit @call proc.list"},
	}}
	if a, ok := v.emittedAnswer(); !ok || a != "@call proc.list" {
		t.Fatalf("emit = %q %v", a, ok)
	}
	// The staged model is never intercepted.
	v.eng.stub = false
	if _, ok := v.emittedAnswer(); ok {
		t.Fatal("emit fired on the real persona")
	}
	v.eng.stub = true
	v.cur.turns[0].role = roleUser
	if _, ok := v.emittedAnswer(); ok {
		t.Fatal("emit fired without a system turn")
	}
}

// stubView is the act-loop fixture: a stub view with a live conversation
// carrying the emit preset, and the act seams swapped for fakes.
func stubView(t *testing.T, h *fakeHost, emit string) *view {
	t.Helper()
	v := newTestView(h, true)
	v.cur = &conversation{name: "C0001", turns: []turn{
		{role: roleSystem, text: emit},
	}}
	return v
}

// swapSeams installs a seams factory for one test.
func swapSeams(t *testing.T, s agentverbs.Seams) {
	t.Helper()
	old := actSeams
	actSeams = func() agentverbs.Seams { return s }
	t.Cleanup(func() { actSeams = old })
}

func fakeProcs(dst []vi.ProcRow) (int, int64) {
	var r vi.ProcRow
	r.PID = 9
	r.State = vi.ProcRunning
	copy(r.NameBuf[:], "GOTABWM.ELF")
	dst[0] = r
	return 1, 1
}

// consentKey answers the standing dialog: Focus picks the button,
// Enter chooses it through the real dialog machinery.
func consentKey(v *view, focus int) {
	v.dlg.Focus = focus
	if _, done := v.dlg.HandleKey(vi.Event{Kind: vi.EvKeyDown, Arg0: 0x28}); !done {
		panic("consent dialog did not resolve on Enter")
	}
	v.applyDialog()
}

// sendEmit drives a send into the emit-preset conversation and returns
// once the consent dialog (or refusal) stands.
func sendEmit(v *view, text string) {
	v.prompt.SetValue(text)
	v.send()
}

// The allow leg end to end: emit → call → cap passes → dialog on deny
// focus → allow → proc.list runs under GOASSIST's own grants and the v3
// records land crash-safe — and the saved file still strict-decodes.
func TestActAllowDispatches(t *testing.T) {
	h := newFakeHost()
	swapSeams(t, agentverbs.Seams{Procs: fakeProcs})
	v := stubView(t, h, "@emit @call proc.list")

	sendEmit(v, "hi")
	if v.act == nil || v.dlg == nil || !v.dlg.Open {
		t.Fatalf("no consent dialog: act=%v dlg=%v console=%v", v.act, v.dlg, h.console)
	}
	for _, want := range []string{markerEmit, markerCall + "proc.list",
		markerConsentAsk + "proc.list"} {
		if !h.hasConsole(want) {
			t.Fatalf("missing %q: %v", want, h.console)
		}
	}
	// The dialog's own default is the safe answer.
	if v.dlg.Focus != 1 || v.dlg.Buttons[v.dlg.Focus].Label != "No" {
		t.Fatalf("consent default focus = %d want deny", v.dlg.Focus)
	}
	consentKey(v, 0) // focus Yes, Enter — the explicit second gesture
	if !h.hasConsole(markerConsent+"allow proc.list") ||
		!h.hasConsole(markerActOK+"proc.list") {
		t.Fatalf("allow path missing: %v", h.console)
	}
	d, ok := decodeConv(h.files["/host/ASSIST/C0001.CHAT"])
	if !ok {
		t.Fatalf("saved conv refused decode:\n%s", h.files["/host/ASSIST/C0001.CHAT"])
	}
	var roles []string
	for _, tr := range d.turns {
		roles = append(roles, tr.role)
	}
	want := []string{"system", "user", "assistant", "call", "consent", "result"}
	if strings.Join(roles, ",") != strings.Join(want, ",") {
		t.Fatalf("turns = %v", roles)
	}
	res := d.turns[5]
	if res.arg != "ok" || !strings.Contains(res.text, "GOTABWM.ELF") {
		t.Fatalf("result = %+v", res)
	}
}

// The deny leg: Enter on the default focus records the named refusal
// and runs nothing — the result body is empty.
func TestActDenyRecordsRefusal(t *testing.T) {
	h := newFakeHost()
	dispatched := false
	swapSeams(t, agentverbs.Seams{
		Procs: func(dst []vi.ProcRow) (int, int64) { dispatched = true; return 0, 0 },
	})
	v := stubView(t, h, "@emit @call proc.list")

	sendEmit(v, "hi")
	if v.act == nil {
		t.Fatalf("no parked act: %v", h.console)
	}
	consentKey(v, v.dlg.Focus) // Enter on the deny default
	if !h.hasConsole(markerConsent+"deny proc.list") {
		t.Fatalf("deny marker missing: %v", h.console)
	}
	if dispatched {
		t.Fatal("a denied call dispatched")
	}
	d, ok := decodeConv(h.files["/host/ASSIST/C0001.CHAT"])
	if !ok {
		t.Fatal("saved conv refused decode")
	}
	last := d.turns[len(d.turns)-2:]
	if last[0].role != roleConsent || last[0].arg != "deny" ||
		last[1].role != roleResult || last[1].arg != "refuse:"+refuseDeclined ||
		last[1].text != "" {
		t.Fatalf("deny records = %+v", last)
	}
}

// A cap the grants do not hold is refused by name before any dialog —
// and no consent record lands, the codec's legal call-then-result shape.
func TestActCapRefusedNoDialog(t *testing.T) {
	h := newFakeHost()
	swapSeams(t, agentverbs.Seams{})
	cases := map[string]struct{ emit, verb, want string }{
		"gosh":    {"@emit @call gosh.run cmd=ls", "gosh.run", "cap_required:term"},
		"input":   {"@emit @call ui.input win=0 events=x", "ui.input", "cap_required:input"},
		"unknown": {"@emit @call bogus.verb", "bogus.verb", "unknown_verb"},
	}
	for name, c := range cases {
		h.console = nil
		v := stubView(t, h, c.emit)
		sendEmit(v, "hi")
		if v.act != nil || v.dlg != nil {
			t.Fatalf("%s: a dialog stood for a refused cap", name)
		}
		if !h.hasConsole(markerCallRefused + c.verb + " " + c.want) {
			t.Fatalf("%s: refusal marker missing: %v", name, h.console)
		}
		last := v.cur.turns[len(v.cur.turns)-1]
		if last.role != roleResult || last.arg != "refuse:"+c.want || last.text != "" {
			t.Fatalf("%s: result = %+v", name, last)
		}
		// And the record survived the save → strict decode on reload.
		if _, ok := decodeConv(h.files["/host/ASSIST/C0001.CHAT"]); !ok {
			t.Fatalf("%s: file refused decode", name)
		}
	}
}

// A malformed call is a named refusal in the transcript — never a
// guess, and never a @@call record the codec would refuse on reload.
func TestActMalformedCall(t *testing.T) {
	h := newFakeHost()
	swapSeams(t, agentverbs.Seams{})
	v := stubView(t, h, "@emit @call PROC.LIST")

	sendEmit(v, "hi")
	if !h.hasConsole(markerCallBad) {
		t.Fatalf("malformed marker missing: %v", h.console)
	}
	if v.act != nil || v.dlg != nil {
		t.Fatal("a malformed call raised a dialog")
	}
	last := v.cur.turns[len(v.cur.turns)-1]
	if last.role != roleMeta || !strings.Contains(last.text, refuseBadCall) {
		t.Fatalf("refusal note = %+v", last)
	}
	for _, tr := range v.cur.turns {
		if isActRole(tr.role) {
			t.Fatalf("a malformed call wrote an act record: %+v", tr)
		}
	}
}

// The second send in an acted conversation still emits — the reload's
// strict decode kept the system turn, the file-level proof the leg's
// boot leans on.
func TestActConvReloadKeepsDriver(t *testing.T) {
	h := newFakeHost()
	swapSeams(t, agentverbs.Seams{Procs: fakeProcs})
	v := stubView(t, h, "@emit @call proc.list")

	sendEmit(v, "hi")
	consentKey(v, 0)
	if !h.hasConsole(markerActOK + "proc.list") {
		t.Fatalf("first act did not land: %v", h.console)
	}
	// Reload the file exactly like a fresh boot's openConv.
	d, ok := decodeConv(h.files["/host/ASSIST/C0001.CHAT"])
	if !ok {
		t.Fatal("reload refused the v3 file")
	}
	v.cur = d
	if _, ok := v.emittedAnswer(); !ok {
		t.Fatal("the reloaded system turn lost its @emit driver")
	}
}

// The generated (non-emit) answer's call goes through the same loop —
// actPass runs on the real decode path too, not only the driver.
func TestActGeneratedAnswerCall(t *testing.T) {
	h := newFakeHost()
	swapSeams(t, agentverbs.Seams{})
	v := stubView(t, h, "be terse") // no @emit: the stub decodes for real
	// Plant the call in the assistant turn the generate produced, as the
	// model could — extraction doesn't care which side made it.
	v.cur.turns = append(v.cur.turns, turn{role: roleModel, text: "@call gosh.run cmd=ls"})
	v.actPass()
	last := v.cur.turns[len(v.cur.turns)-1]
	if last.role != roleResult || last.arg != "refuse:cap_required:term" {
		t.Fatalf("generated call result = %+v", last)
	}
}
