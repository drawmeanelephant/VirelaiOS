package main

// receipt_test.go — the M205d (#3811) pins: the lane arm's named
// refusals before consent, the receipt's shape + MAC under assist-hmac,
// one receipt per resolved outcome (allow, deny, cap-refused), the
// auditFileMax rotation, and the dead-lane fail-closed path.
// Kept in its own file per the card split (act_test.go is card c's).

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"virelai/agentverbs"
	"virelai/shlib"
	"virelai/vi"
)

// assistTestKey is the lane key the test seam serves — the same bytes
// receipt verification recomputes the MAC under.
var assistTestKey = []byte("b5b5b5b5b5b5b5b5b5b5b5b5b5b5b5b5")

func assistTestKeyFn(name string, out []byte) int {
	if name != assistSecret {
		return -1
	}
	return copy(out, assistTestKey)
}

func assistTestRandFn(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(0x10 + i)
	}
	return len(p), nil
}

func assistTestTimeFn() int64 { return 1761234567 }

// The package default is an ARMED lane: every act-loop test in the
// package then exercises the real arm path (receipts land in the fake
// host's file map, which they never assert on). Tests of the
// fail-closed legs override per test and restore these fakes.
func init() {
	assistKeyFn = assistTestKeyFn
	assistRandFn = assistTestRandFn
	assistTimeFn = assistTestTimeFn
}

// swapAssistSeams installs the three lane seams for one test,
// restoring the armed defaults after.
func swapAssistSeams(t *testing.T, keyFn func(string, []byte) int,
	randFn func([]byte) (int, error), timeFn func() int64) {
	t.Helper()
	prevK, prevR, prevT := assistKeyFn, assistRandFn, assistTimeFn
	assistKeyFn, assistRandFn, assistTimeFn = keyFn, randFn, timeFn
	assist = assistLane{}
	t.Cleanup(func() {
		assistKeyFn, assistRandFn, assistTimeFn = prevK, prevR, prevT
		assist = assistLane{}
	})
}

// armAssistLane resets the lane to a fresh armed boot under the test
// key and restores the defaults after.
func armAssistLane(t *testing.T) {
	t.Helper()
	swapAssistSeams(t, assistTestKeyFn, assistTestRandFn, assistTestTimeFn)
}

// assistFiles names the lane's receipt files in the fake share.
func assistFiles(h *fakeHost) map[string][]byte {
	out := map[string][]byte{}
	pfx := "/host/ASSIST/AUDIT/"
	for p, b := range h.files {
		if strings.HasPrefix(p, pfx) {
			out[p[len(pfx):]] = b
		}
	}
	return out
}

// verifyAssistReceipt checks a lane file's shape — two lines, the JSON
// receipt then its HMAC-SHA256 under assistTestKey — and returns the
// receipt line for field assertions (the guest reader is GOTOP's
// scanFlat; the host test asserts the fields in place).
func verifyAssistReceipt(t *testing.T, body []byte) string {
	t.Helper()
	parts := strings.Split(string(body), "\n")
	if len(parts) != 3 || parts[2] != "" {
		t.Fatalf("receipt lines = %d\n%s", len(parts), body)
	}
	mac := shlib.HMACSHA256(assistTestKey, []byte(parts[0]))
	if parts[1] != hexStr(mac[:]) {
		t.Fatalf("MAC mismatch:\n%s", body)
	}
	return parts[0]
}

// The allow leg end to end: emit → call → cap passes → consent →
// proc.list runs — and exactly one new RCPT file lands in the assist
// lane, MAC'd under assist-hmac, naming the conv and GOASSIST.ELF.
func TestAssistAllowWritesReceipt(t *testing.T) {
	h := newFakeHost()
	armAssistLane(t)
	swapSeams(t, agentverbs.Seams{Procs: fakeProcs})
	v := stubView(t, h, "@emit @call proc.list")

	sendEmit(v, "hi")
	consentKey(v, 0) // focus Yes, Enter
	if !h.hasConsole(markerActOK + "proc.list") {
		t.Fatalf("allow path missing: %v", h.console)
	}
	files := assistFiles(h)
	if len(files) != 1 {
		t.Fatalf("lane files = %d want 1", len(files))
	}
	var body []byte
	for name, b := range files {
		if name != "RCPT-10111213-00000001" {
			t.Fatalf("receipt name = %q (the epoch draw's hex8)", name)
		}
		body = b
	}
	line := verifyAssistReceipt(t, body)
	for _, field := range []string{
		`"seq":1`, `"epoch":"10111213"`, `"agent":"C0001"`,
		`"caller":"GOASSIST.ELF"`, `"verb":"proc.list"`,
		`"args":{}`, `"ts":1761234567`, `"result":"ok"`,
	} {
		if !strings.Contains(line, field) {
			t.Fatalf("receipt missing %s\n%s", field, line)
		}
	}
	if !h.hasConsole(markerRcpt + "RCPT-10111213-00000001") {
		t.Fatalf("rcpt marker missing: %v", h.console)
	}
}

// The fail-closed leg: no assist-hmac row is served, so the act refuses
// no_receipt_key BEFORE the consent dialog — nothing runs, no dialog,
// no receipt.
func TestAssistNoKeyRefusesBeforeConsent(t *testing.T) {
	h := newFakeHost()
	swapAssistSeams(t, func(string, []byte) int { return -1 },
		assistTestRandFn, assistTestTimeFn)
	dispatched := false
	swapSeams(t, agentverbs.Seams{
		Procs: func(dst []vi.ProcRow) (int, int64) { dispatched = true; return 0, 0 },
	})
	v := stubView(t, h, "@emit @call proc.list")

	sendEmit(v, "hi")
	if v.act != nil || v.dlg != nil {
		t.Fatalf("a dialog stood without a key: act=%v dlg=%v", v.act, v.dlg)
	}
	if !h.hasConsole(markerCallRefused + "proc.list " + refuseNoKey) {
		t.Fatalf("refusal marker missing: %v", h.console)
	}
	if dispatched {
		t.Fatal("the verb ran without a receipt key")
	}
	last := v.cur.turns[len(v.cur.turns)-1]
	if last.role != roleResult || last.arg != "refuse:"+refuseNoKey {
		t.Fatalf("result = %+v", last)
	}
	if n := len(assistFiles(h)); n != 0 {
		t.Fatalf("a receipt landed without a key (%d files)", n)
	}
}

// Deny receipts the declined outcome: the lane holds one file whose
// result names the human's answer — the ask was real, the run was not.
func TestAssistDenyReceiptsDeclined(t *testing.T) {
	h := newFakeHost()
	armAssistLane(t)
	swapSeams(t, agentverbs.Seams{Procs: fakeProcs})
	v := stubView(t, h, "@emit @call proc.list")

	sendEmit(v, "hi")
	consentKey(v, v.dlg.Focus) // Enter on the deny default
	files := assistFiles(h)
	if len(files) != 1 {
		t.Fatalf("lane files = %d want 1", len(files))
	}
	for _, b := range files {
		if line := verifyAssistReceipt(t, b); !strings.Contains(line, `"result":"declined"`) {
			t.Fatalf("deny receipt = %s", line)
		}
	}
}

// A cap-gated call receipts its named refusal when the lane can write —
// the act never reaches consent but the lane still says what was asked.
func TestAssistCapRefusedReceipts(t *testing.T) {
	h := newFakeHost()
	armAssistLane(t)
	swapSeams(t, agentverbs.Seams{})
	v := stubView(t, h, "@emit @call gosh.run cmd=ls")

	sendEmit(v, "hi")
	if v.act != nil || v.dlg != nil {
		t.Fatal("a dialog stood for a refused cap")
	}
	files := assistFiles(h)
	if len(files) != 1 {
		t.Fatalf("lane files = %d want 1", len(files))
	}
	for _, b := range files {
		line := verifyAssistReceipt(t, b)
		if !strings.Contains(line, `"verb":"gosh.run"`) ||
			!strings.Contains(line, `"result":"cap_required:term"`) {
			t.Fatalf("cap receipt = %s", line)
		}
		if !strings.Contains(line, `"args":{"cmd":"ls"}`) {
			t.Fatalf("args object missing:\n%s", line)
		}
	}
}

// deadLaneHost fails every publish — the lane's writes, nothing else
// the test needs (the conv save's own error path is already exercised).
type deadLaneHost struct{ *fakeHost }

func (d deadLaneHost) WriteShareFile(p string, b []byte) error {
	if strings.Contains(p, "/AUDIT/") {
		return errors.New("publish -13")
	}
	return d.fakeHost.WriteShareFile(p, b)
}

// A refused receipt write marks the lane dead: the next act refuses
// audit_full by name before consent — nothing runs unreceipted.
func TestAssistDeadLaneRefusesAuditFull(t *testing.T) {
	h := newFakeHost()
	armAssistLane(t)
	dispatched := false
	swapSeams(t, agentverbs.Seams{
		Procs: func(dst []vi.ProcRow) (int, int64) { dispatched = true; return 0, 0 },
	})
	v := stubView(t, h, "@emit @call proc.list")
	v.h = deadLaneHost{h}

	sendEmit(v, "hi")
	if v.act == nil || v.dlg == nil {
		t.Fatalf("no consent dialog stood: %v", h.console)
	}
	consentKey(v, 0) // allow: the dispatch runs, the receipt write dies
	if !h.hasConsole(markerActOK+"proc.list") || !h.hasConsole(markerAuditFull) {
		t.Fatalf("dead lane markers missing: %v", h.console)
	}
	if !dispatched {
		t.Fatal("the allow act did not dispatch")
	}
	// The next act's lane gate refuses by name — fail closed.
	v.cur.turns = append(v.cur.turns, turn{role: roleModel, text: "@call proc.list"})
	v.actPass()
	if !h.hasConsole(markerCallRefused + "proc.list " + refuseLane) {
		t.Fatalf("audit_full refusal missing: %v", h.console)
	}
	if v.act != nil {
		t.Fatal("an act parked behind consent on a dead lane")
	}
}

// The auditFileMax bound: at the cap the writer drops the lexically
// smallest name — earlier-epoch files first, a fresh epoch's own
// evidence never the victim.
func TestAssistLaneRotation(t *testing.T) {
	h := newFakeHost()
	armAssistLane(t)
	swapSeams(t, agentverbs.Seams{Procs: fakeProcs})
	// 64 older-epoch receipts already in the lane: the next emit must
	// evict the smallest name and stay at the bound.
	for i := 0; i < assistFileMax; i++ {
		n := "RCPT-00000001-" + pad8(i+1)
		h.files["/host/ASSIST/AUDIT/"+n] = []byte("x\ny\n")
	}
	v := stubView(t, h, "@emit @call proc.list")
	sendEmit(v, "hi")
	consentKey(v, 0)

	files := assistFiles(h)
	if len(files) != assistFileMax {
		t.Fatalf("lane files = %d want bound %d", len(files), assistFileMax)
	}
	if _, ok := files["RCPT-00000001-00000001"]; ok {
		t.Fatal("the smallest earlier-epoch name survived the bound")
	}
	if _, ok := files["RCPT-10111213-00000001"]; !ok {
		t.Fatal("the fresh receipt was the rotation's victim")
	}
	if !h.hasConsole(markerAuditDrop + "RCPT-00000001-00000001") {
		t.Fatalf("drop marker missing: %v", h.console)
	}
}

// mkdir refusal at arm is the same dead lane — the act refuses
// audit_full before the dialog (MkdirShare's only failure mode).
func TestAssistArmMkdirRefuses(t *testing.T) {
	h := newFakeHost()
	armAssistLane(t)
	swapSeams(t, agentverbs.Seams{})
	v := stubView(t, h, "@emit @call proc.list")
	v.h = mkdirDeadHost{h}

	sendEmit(v, "hi")
	if v.act != nil || v.dlg != nil {
		t.Fatal("a dialog stood on a dead lane")
	}
	if !h.hasConsole(markerCallRefused + "proc.list " + refuseLane) {
		t.Fatalf("audit_full refusal missing: %v", h.console)
	}
}

type mkdirDeadHost struct{ *fakeHost }

func (d mkdirDeadHost) MkdirShare(p string) error {
	return errors.New("mkdir -13")
}

func pad8(i int) string {
	s := "0000000" + strconv.FormatInt(int64(i), 16)
	return s[len(s)-8:]
}
