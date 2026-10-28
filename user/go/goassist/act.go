// act.go — the M205c act loop (#3810): when an assistant turn carries a
// call, GOASSIST checks the verb's cap against its own grants, asks
// consent in a dialog, and on allow runs the verb in-process through
// virelai/agentverbs — the same dispatcher AGENTD's channel serves —
// writing the v3 call/consent/result records (#3809) as it goes.
//
// The call's fixed textual form is one line in the assistant turn:
//
//	@call <verb> [key=value ...]
//
// at most one per turn. A malformed call is a named refusal in the
// transcript, never a guess; a well-formed call the cap gate refuses is
// recorded with its named refusal and no dialog. The consent dialog
// keeps the safe default (focus on deny): allow is the explicit second
// gesture. Ordering is the card's own — cap check first, consent
// second, dispatch last.
//
// The pinned refusals: gosh.run lands cap_required:term (the term word
// is outside GOASSIST's caps= ceiling), and ui.input lands
// cap_required:input — `input` is an agent-keyring word with no caps=
// spelling at all (kernel grant.zig word_count), so the seat's
// agent_input_denied is unreachable in-process and the refusal lands
// one layer earlier, still a named refusal with no dialog.
package main

import (
	"strings"

	"virelai/agentverbs"
	"virelai/appkit"
	"virelai/vi"
	"virelai/widgets"
)

const (
	// callMark is the fixed textual form: a line in the assistant turn
	// beginning `@call ` names the verb; `key=value` tokens after it are
	// the flat-grammar args (the same shape the @@call record's body
	// stores).
	callMark = "@call "
	// emitMark is the deterministic driver (#3807 thesis 5): a preset's
	// @@system turn carrying a line `@emit <text>` makes the STUB engine
	// emit the line's tail verbatim as its answer — the preset emits
	// the fixed call, so the gate legs never depend on a model's text.
	// The staged model is never intercepted.
	emitMark = "@emit "

	// refuseDeclined is the named refusal a deny lands — the user's own
	// answer, recorded so a reload shows the act was declined, not run.
	refuseDeclined = "declined"
	// refuseBadCall names a malformed call line: the refusal is named in
	// the transcript but carries no @@call record — a verb outside the
	// codec grammar could never survive the file's own strict decode.
	refuseBadCall = "bad_call"
)

// Serial markers for the gate legs (the go-assist spec asserts on them;
// they print only AFTER the step they name actually ran).
const (
	markerEmit        = "goassist: emit"
	markerCall        = "goassist: call "
	markerCallBad     = "goassist: call malformed"
	markerCallRefused = "goassist: call refused "
	markerConsentAsk  = "goassist: consent ask "
	markerConsent     = "goassist: consent "
	markerActOK       = "goassist: act ok "
	markerActRefused  = "goassist: act refused "
)

// assistCapWords are GOASSIST's own grants as the manifest declares
// them: caps=file,window plus the floor CapsFloorWords pins
// (memory,process,debug). The dispatcher's cap gate runs against these;
// a narrower GRANTS.TXT row can only take words away, and the kernel
// still enforces that row underneath — the syscall stays the backstop
// the named refusal rides on.
var assistCapWords = []string{"file", "window", "memory", "process", "debug"}

// assistCaps resolves GOASSIST's grant words to the library's bitmask;
// words outside the agent vocabulary (memory, debug) resolve to nothing.
func assistCaps() agentverbs.Caps {
	var c agentverbs.Caps
	for _, w := range assistCapWords {
		c |= agentverbs.CapWord(w)
	}
	return c
}

// verbNeed resolves one verb's required cap by asking the library's own
// advertised set: with exactly one cap bit lit, Advertised lists the
// verbs gated behind it, so the cap GOASSIST's gate checks IS the
// library's table — never a second copy to drift. known=false is
// Dispatch's unknown_verb.
func verbNeed(verb string) (need agentverbs.Caps, known bool) {
	for _, bit := range []agentverbs.Caps{
		agentverbs.CapProcess, agentverbs.CapFile,
		agentverbs.CapWindow, agentverbs.CapInput, agentverbs.CapTerm,
	} {
		for _, v := range agentverbs.Advertised(bit, true) {
			if v == verb {
				return bit, true
			}
		}
	}
	return 0, false
}

// capWord renders a cap bit's grant word for the cap_required:<word>
// refusal — CapWord inverted over the vocabulary, so the refusal names
// the same word an agent keyring row would declare.
func capWord(c agentverbs.Caps) string {
	for _, w := range []string{"process", "file", "window", "input", "term"} {
		if agentverbs.CapWord(w) == c {
			return w
		}
	}
	return ""
}

// capGate is the pre-consent half of the act loop: the verb's cap
// against GOASSIST's own bitmask. "" lets the call through to the
// dialog; any answer is Dispatch's own refusal code and nothing runs.
func capGate(verb string, caps agentverbs.Caps) string {
	need, known := verbNeed(verb)
	if !known {
		return "unknown_verb"
	}
	if caps&need == 0 {
		return "cap_required:" + capWord(need)
	}
	return ""
}

// actCall is one extracted call: the verb, the raw args text (what the
// @@call record's body stores), and the flat-grammar projection the
// dispatcher consumes.
type actCall struct {
	verb   string
	args   string
	fields map[string]agentverbs.Field
}

// pendingAct is the call parked behind the consent dialog: the records
// it needs once the answer lands. The dialog itself carries no act
// state — one act is ever open at a time because generation is
// synchronous and the dialog is modal.
type pendingAct struct {
	verb   string
	fields map[string]agentverbs.Field
}

// extractCall scans an assistant turn's text for the call line.
// (nil, true) is no call; (nil, false) is a malformed call — the named
// refusal path; a found call is parsed whole: the verb must fit the
// codec's @@call grammar (a record that can't carry it would fail the
// file's own decode) and every arg token must be name=value.
func extractCall(text string) (c *actCall, ok bool) {
	for _, ln := range strings.Split(text, "\n") {
		if ln != strings.TrimSuffix(callMark, " ") && !strings.HasPrefix(ln, callMark) {
			continue
		}
		if c != nil {
			return nil, false // a second call line: malformed, one per turn
		}
		rest := strings.TrimSpace(strings.TrimPrefix(ln, "@call"))
		verb, argText, _ := strings.Cut(rest, " ")
		if !validVerb(verb) {
			return nil, false
		}
		fields, argOK := parseCallArgs(argText)
		if !argOK {
			return nil, false
		}
		c = &actCall{verb: verb, args: argText, fields: fields}
	}
	return c, true
}

// parseCallArgs lifts the flat key=value tail into the dispatcher's
// field projection: `true`/`false` are booleans, unsigned decimal
// integers within the flat grammar's bound are ints, and everything
// else is a string. A token that isn't name=value refuses the call.
func parseCallArgs(s string) (map[string]agentverbs.Field, bool) {
	f := map[string]agentverbs.Field{}
	if s == "" {
		return f, true
	}
	for _, tok := range strings.Split(s, " ") {
		k, v, ok := strings.Cut(tok, "=")
		if !ok || !validArgName(k) || v == "" {
			return nil, false
		}
		switch {
		case v == "true" || v == "false":
			f[k] = agentverbs.Field{Kind: agentverbs.FBool, Str: v}
		default:
			if n, isInt := flatUint(v); isInt {
				f[k] = agentverbs.Field{Kind: agentverbs.FInt, Num: n}
			} else {
				f[k] = agentverbs.Field{Kind: agentverbs.FStr, Str: v}
			}
		}
	}
	return f, true
}

// validArgName bounds an arg key: [a-z_][a-z0-9_]* — the wire field
// names the verbs already declare (pid, path, data_b64, win, events,
// op, space, percent, cmd, timeout_ms, scale).
func validArgName(s string) bool {
	if len(s) < 1 || len(s) > convVerbMax {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || c >= 'a' && c <= 'z' || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// flatUint parses an unsigned decimal within the flat grammar's int
// bound (2^31-1) — the same shape the wire reader admits.
func flatUint(s string) (int64, bool) {
	if len(s) == 0 || len(s) > 10 {
		return 0, false
	}
	var n int64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
		if n > 2147483647 {
			return 0, false
		}
	}
	return n, true
}

// emittedAnswer is the deterministic driver: in the stub persona only,
// a system turn carrying an `@emit <text>` line makes the emitted tail
// the answer — the preset emits the fixed call and no decode runs at
// all, so the leg never spends a compute session it does not need.
func (v *view) emittedAnswer() (string, bool) {
	if !v.eng.stub || v.cur == nil || len(v.cur.turns) == 0 {
		return "", false
	}
	sys := v.cur.turns[0]
	if sys.role != roleSystem {
		return "", false
	}
	for _, ln := range strings.Split(sys.text, "\n") {
		if rest, ok := strings.CutPrefix(ln, emitMark); ok && rest != "" {
			return rest, true
		}
	}
	return "", false
}

// actPass is the act loop's front half, run once a complete assistant
// turn exists: extract the call, gate its cap against GOASSIST's own
// grants, and either record the named refusal (no dialog — the codec's
// legal call-without-consent shape) or raise the consent dialog with
// the call parked.
func (v *view) actPass() {
	if v.cur == nil || len(v.cur.turns) == 0 {
		return
	}
	last := v.cur.turns[len(v.cur.turns)-1]
	if last.role != roleModel {
		return
	}
	call, ok := extractCall(last.text)
	if !ok {
		v.h.ConsoleLine(markerCallBad)
		v.note("(call refused: " + refuseBadCall + ")")
		return
	}
	if call == nil {
		return
	}
	v.h.ConsoleLine(markerCall + call.verb)
	v.cur.turns = append(v.cur.turns,
		turn{role: roleCall, arg: call.verb, text: call.args})
	if code := capGate(call.verb, assistCaps()); code != "" {
		// A refused call receipts its named refusal when the lane can
		// write — the arm is what lets it; a missing key just leaves
		// the refusal unreceipted (nothing was going to run anyway).
		v.assistLaneArm()
		v.actReceipt(call.verb, call.fields, code)
		v.h.ConsoleLine(markerCallRefused + call.verb + " " + code)
		v.cur.turns = append(v.cur.turns,
			turn{role: roleResult, arg: "refuse:" + code})
		return
	}
	// The M205d receipt lane is the pre-consent half of the gate: an
	// act that cannot be receipted refuses by name before the dialog —
	// no key row, no entropy, or a dead lane (fail closed, audit_full).
	if code := v.assistLaneArm(); code != "" {
		v.h.ConsoleLine(markerCallRefused + call.verb + " " + code)
		v.cur.turns = append(v.cur.turns,
			turn{role: roleResult, arg: "refuse:" + code})
		return
	}
	v.act = &pendingAct{verb: call.verb, fields: call.fields}
	v.h.ConsoleLine(markerConsentAsk + call.verb)
	v.askConsent(call)
}

// askConsent raises the allow/deny dialog naming the verb and its
// args. NewConfirm's default focus is deny — the safe answer is the
// one a reflexive Enter lands.
func (v *view) askConsent(c *actCall) {
	msg := "Run " + c.verb
	if c.args != "" {
		msg += " " + c.args
	}
	if len(msg) > 96 {
		msg = msg[:96]
	}
	d := appkit.NewConfirm("Allow act?", msg+"?")
	d.R = widgets.Rect{
		X: (int(v.ta.W) - d.R.W) / 2, Y: (int(v.ta.H) - d.R.H) / 2,
		W: d.R.W, H: d.R.H,
	}
	v.dlg = &d
	v.status = "consent: " + c.verb + " — Enter denies; Tab then Enter allows"
}

// consentResolve lands the dialog's answer: deny records the named
// refusal and runs nothing; allow records the consent and dispatches
// the verb in-process under GOASSIST's own grants, then records the
// result or the dispatch's named refusal. Either way the act records
// commit crash-safe and the gate gets its committed anchor.
func (v *view) consentResolve(allow bool) {
	a := v.act
	v.act = nil
	if !allow {
		v.h.ConsoleLine(markerConsent + "deny " + a.verb)
		v.actReceipt(a.verb, a.fields, refuseDeclined)
		v.cur.turns = append(v.cur.turns,
			turn{role: roleConsent, arg: "deny"},
			turn{role: roleResult, arg: "refuse:" + refuseDeclined})
	} else {
		v.h.ConsoleLine(markerConsent + "allow " + a.verb)
		v.cur.turns = append(v.cur.turns,
			turn{role: roleConsent, arg: "allow"})
		out, code := v.actDispatch(a)
		if code != "" {
			v.actReceipt(a.verb, a.fields, code)
			v.h.ConsoleLine(markerActRefused + a.verb + " " + code)
			v.cur.turns = append(v.cur.turns,
				turn{role: roleResult, arg: "refuse:" + code})
		} else {
			v.actReceipt(a.verb, a.fields, "ok")
			v.h.ConsoleLine(markerActOK + a.verb)
			v.cur.turns = append(v.cur.turns,
				turn{role: roleResult, arg: "ok", text: cutResultBody(out)})
		}
	}
	v.finishSend()
}

// actDispatch runs the call through GOASSIST's in-process dispatcher:
// the library configured for this app's identity — the fs.* sandbox is
// the conversation store (GOASSIST's data root), Self names
// GOASSIST.ELF so the WM_RPC peer scan resolves it (and the seat can
// refuse it by name), and the session is GOASSIST's own cap bitmask
// with the exec kill switch off. The bounded-output verbs' streamed
// chunks append under the result JSON so the record holds everything
// the verb produced.
func (v *view) actDispatch(a *pendingAct) (string, string) {
	if v.verbs == nil {
		v.verbs = agentverbs.New(actConfig(v.dir))
	}
	var streamed []byte
	sess := &agentverbs.Session{
		Caps: assistCaps(),
		Emit: func(b []byte) { streamed = append(streamed, b...) },
	}
	res, code := v.verbs.Dispatch(sess,
		&agentverbs.Request{Verb: a.verb, Fields: a.fields})
	if len(streamed) > 0 {
		res += "\n" + string(streamed)
	}
	return res, code
}

// actConfig is the dispatcher's GOASSIST identity. OutPath sits inside
// the sandbox (GOASSIST has no reserved audit lane — receipts are card
// d's), and gosh.run never reaches it anyway: the term cap is outside
// GOASSIST's grants, so the spawn path is cap-gated before it runs.
func actConfig(sandbox string) agentverbs.Config {
	return agentverbs.Config{
		Seams:   actSeams(),
		Sandbox: sandbox,
		OutPath: sandbox + "/GOASSIST.OUT",
		GoshBin: "GOSH.ELF",
		Self:    appName,
		Mark:    "goassist",
	}
}

// actSeams wires the production syscall surface — the same vi calls
// AGENTD delegates to. It is a var so host tests substitute fakes the
// same way the daemon's own suite does.
var actSeams = func() agentverbs.Seams {
	return agentverbs.Seams{
		Procs:      vi.Procs,
		ReadFile:   vi.ReadFileAll,
		WriteFile:  vi.WriteFileSafe,
		DeleteFile: vi.FileDelete,
		DirList:    vi.DirList,
		Exec:       vi.Exec,
		Wait:       vi.WaitBudget,
		Kill:       vi.Kill,
		Probe:      vi.Probe,
		SeatPid:    vi.WmctlSeatPid,
		WinOpen:    vi.WinOpen,
		WinHide:    vi.WinSetVisible,
		WmWindows:  vi.WmWindows,
		Scanout:    vi.ScanoutRead,
		AgentInputKey: func(id uint32, mods, usage uint8, self string) (vi.WmRpc, bool) {
			return vi.WmAgentInputKey(id, mods, usage, self)
		},
		AgentInputPtr: func(id uint32, buttons uint8, x, y uint16, self string) (vi.WmRpc, bool) {
			return vi.WmAgentInputPtr(id, buttons, x, y, self)
		},
		AgentGeometry: func(id uint32, op uint8, target uint32, arg uint8, self string) (vi.WmRpc, bool) {
			return vi.WmAgentGeometry(id, op, target, arg, self)
		},
		Nanos: vi.Nanos,
		Yield: vi.Yield,
		Sleep: vi.Sleep,
		Log:   vi.ConsoleLine,
	}
}
