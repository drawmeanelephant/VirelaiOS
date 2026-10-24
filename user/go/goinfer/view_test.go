package main

// view_test.go — the M122c (#2572) host half of GOINFER's windowed view:
// the mirrored console, the one-session-per-process bound, and the
// window-refused fallback are all pinned against the fake host.

import (
	"strings"
	"testing"

	"virelai/gpu3d"
	"virelai/infer"
	"virelai/tabapp"
	"virelai/vi"
)

func (v *inferView) hasLine(sub string) bool {
	for _, l := range v.lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// A bare launch on a window-less host names the journeys and exits
// nonzero — the honest headless answer, not a silent death. The dispatch
// pin proves the bare argv reaches the view path at all.
func TestViewFallbackWithoutWindow(t *testing.T) {
	var lines []string
	old := viewConsole
	viewConsole = func(s string) { lines = append(lines, s) }
	defer func() { viewConsole = old }()
	if rc := dispatch(nil); rc == 0 {
		t.Fatal("dispatch(nil) on a window-less host must not report success")
	}
	var refused, done bool
	for _, l := range lines {
		if strings.Contains(l, "window refused") {
			refused = true
		}
		if l == "goinfer: done" {
			done = true
		}
	}
	if !refused || !done {
		t.Fatalf("lines=%v, want window-refused then done", lines)
	}
}

// The Stub button drives the real journey and the window's log carries
// every console line the fake host received.
func TestViewStubMirrorsConsole(t *testing.T) {
	h := newFakeHost()
	v := newInferView(&tabapp.TabApp{W: viewW, H: viewH}, h)
	v.journey("stub")
	if !h.hasConsole("goinfer: stub consistent K=5") {
		t.Fatalf("console: %v", h.console)
	}
	if !v.hasLine("goinfer: stub consistent K=5") || !v.hasLine("goinfer OK") {
		t.Fatalf("view log lacks the journey's own lines: %v", v.lines)
	}
	if !v.h.opened {
		t.Fatal("the stub run took the session; opened must be set")
	}
}

// The one-session bound is shown on the window, not left to the kernel's
// refusal: a second journey press after a session-opening run says spent
// and runs nothing.
func TestViewSessionSpentIsNamed(t *testing.T) {
	h := newFakeHost()
	v := newInferView(&tabapp.TabApp{W: viewW, H: viewH}, h)
	v.journey("stub")
	if !v.hasLine("session spent") {
		t.Fatalf("spent note missing: %v", v.lines)
	}
	before := len(h.devs)
	v.journey("stub")
	if len(h.devs) != before {
		t.Fatal("a second journey opened another session")
	}
	if got := v.lines[len(v.lines)-1]; !strings.Contains(got, "session spent") {
		t.Fatalf("last log line %q, want the spent refusal", got)
	}
}

// A weights-absent model press never reaches OpenComp, so the session is
// NOT spent — a later stub press still runs.
func TestViewAbsentModelKeepsSession(t *testing.T) {
	h := newFakeHost()
	v := newInferView(&tabapp.TabApp{W: viewW, H: viewH}, h)
	v.journey("model")
	if v.h.opened {
		t.Fatal("absent weights must not consume the session")
	}
	if !v.hasLine("goinfer: weights absent") {
		t.Fatalf("view log lacks the absent line: %v", v.lines)
	}
	v.journey("stub")
	if !v.hasLine("goinfer: stub consistent K=5") {
		t.Fatalf("stub after absent-model did not run: %v", v.lines)
	}
}

// A device-less journey names itself unsupported in the window and the
// session stays unspent (the open was refused before a device existed).
func TestViewUnsupportedIsShown(t *testing.T) {
	h := newFakeHost()
	h.openErr = gpu3d.ErrComputeNoDevice
	v := newInferView(&tabapp.TabApp{W: viewW, H: viewH}, h)
	v.journey("stub")
	if !v.hasLine("goinfer: unsupported") {
		t.Fatalf("view log lacks unsupported: %v", v.lines)
	}
	if v.h.opened {
		t.Fatal("a refused open must not mark the session spent")
	}
}

// The log is bounded.
func TestViewLogBounded(t *testing.T) {
	v := newInferView(&tabapp.TabApp{W: viewW, H: viewH}, newFakeHost())
	for i := 0; i < viewLogMax*3; i++ {
		v.say("x")
	}
	if len(v.lines) != viewLogMax {
		t.Fatalf("log n=%d, want %d", len(v.lines), viewLogMax)
	}
}

// --- M195e (#3310): the appver-1 state envelope -------------------------

// A stub journey's armed state and the whole log tail encode and decode
// back identically — the blob the seat stores is the blob the app wrote.
func TestStateRoundTrip(t *testing.T) {
	h := newFakeHost()
	v := newInferView(&tabapp.TabApp{W: viewW, H: viewH}, h)
	v.journey("stub")
	st, ok := decodeState(v.encodeState())
	if !ok {
		t.Fatal("a blob the app just wrote refused to decode")
	}
	if st.journey != "stub" || st.path != "" {
		t.Fatalf("journey/path = %q/%q, want stub/''", st.journey, st.path)
	}
	if len(st.tail) != len(v.lines) {
		t.Fatalf("tail n=%d, want %d", len(st.tail), len(v.lines))
	}
	for i := range st.tail {
		if st.tail[i] != v.lines[i] {
			t.Fatalf("tail[%d] drifted: %q vs %q", i, st.tail[i], v.lines[i])
		}
	}
}

// The apply path: a stored envelope replays its tail on the reopened
// view and re-arms the journey — but runs NOTHING (no compute session
// opens, the press stays the person's). A foreign appver and a torn
// blob refuse by name; no blob is a silent fresh start.
func TestApplyRestored(t *testing.T) {
	src := newInferView(&tabapp.TabApp{W: viewW, H: viewH}, newFakeHost())
	src.journey("stub")
	st, ok := decodeState(src.encodeState())
	if !ok {
		t.Fatal("the app's own blob would not decode")
	}

	// The round trip: tail replayed, armed journey carried, nothing ran.
	h := newFakeHost()
	v := newInferView(&tabapp.TabApp{W: viewW, H: viewH,
		Restored: true, RestoredVersion: stateAppVer}, h)
	line := v.applyRestored(st, ok)
	if line != "goinfer: state restored journey=stub tail="+vi.Itoa64(int64(len(st.tail))) {
		t.Fatalf("restored marker = %q", line)
	}
	if len(h.devs) != 0 {
		t.Fatal("a restore opened a compute session — journeys stay the person's click")
	}
	if !v.hasLine("goinfer: stub consistent K=5") {
		t.Fatalf("restored tail lacks the run lines: %v", v.lines)
	}
	if v.armed != "stub" {
		t.Fatalf("armed = %q, want stub", v.armed)
	}

	// No stored envelope: silent, nothing applied.
	v = newInferView(&tabapp.TabApp{W: viewW, H: viewH}, newFakeHost())
	if line := v.applyRestored(inferState{}, false); line != "" {
		t.Fatalf("fresh-start apply = %q, want silence", line)
	}

	// A foreign appver refuses even though the blob decoded.
	v = newInferView(&tabapp.TabApp{W: viewW, H: viewH,
		Restored: true, RestoredVersion: 9}, newFakeHost())
	if line := v.applyRestored(st, true); line != "goinfer: state refused ver=9" {
		t.Fatalf("refused-ver marker = %q", line)
	}

	// A torn blob refuses whole — nothing applied.
	v = newInferView(&tabapp.TabApp{W: viewW, H: viewH,
		Restored: true, RestoredVersion: stateAppVer}, newFakeHost())
	if line := v.applyRestored(inferState{}, false); line != "goinfer: state refused bad" {
		t.Fatalf("refused-bad marker = %q", line)
	}
	if v.armed != "" || len(v.lines) != 3 {
		t.Fatalf("a refused blob touched the view: armed=%q lines=%v", v.armed, v.lines)
	}
}

// A restored weights path is what the next model press runs — the armed
// journey and its path carry forward, still nobody's auto-run.
func TestRestoredModelPathRearms(t *testing.T) {
	src := newInferView(&tabapp.TabApp{W: viewW, H: viewH}, newFakeHost())
	src.modelPath = "/host/GOINFER/ALT.VIW"
	src.journey("model") // absent weights: armed, path armed with it
	st, ok := decodeState(src.encodeState())
	if !ok || st.journey != "model" || st.path != "/host/GOINFER/ALT.VIW" {
		t.Fatalf("decode: ok=%v st=%+v", ok, st)
	}

	h := newFakeHost()
	h.files["/host/GOINFER/ALT.VIW"] = infer.SynthStub()
	v := newInferView(&tabapp.TabApp{W: viewW, H: viewH,
		Restored: true, RestoredVersion: stateAppVer}, h)
	line := v.applyRestored(st, ok)
	if line != "goinfer: state restored journey=model path=/host/GOINFER/ALT.VIW tail="+vi.Itoa64(int64(len(st.tail))) {
		t.Fatalf("restored marker = %q", line)
	}
	if len(h.devs) != 0 {
		t.Fatal("a restore opened a compute session")
	}
	// The person's next 'm' press runs the restored path, not the card
	// default — absent on the default, present on ALT.VIW.
	v.journey("model")
	if !h.hasConsole("goinfer: model answer") {
		t.Fatalf("model run at restored path did not land: %v", h.console)
	}
}

// runView wires the seat hooks end to end: a stored blob handed to
// Config.RestoreState inside the (stubbed) Init surfaces as the restored
// marker on serial once the view exists, and nothing auto-runs.
func TestRunViewAppliesStoredEnvelope(t *testing.T) {
	src := newInferView(&tabapp.TabApp{W: viewW, H: viewH}, newFakeHost())
	src.journey("stub")
	blob := src.encodeState()

	var lines []string
	oldCon, oldOpen := viewConsole, openViewWindow
	viewConsole = func(s string) { lines = append(lines, s) }
	openViewWindow = func(cfg tabapp.Config) *tabapp.TabApp {
		ta := &tabapp.TabApp{Win: 9, W: viewW, H: viewH, OpenOK: true}
		if cfg.RestoreState != nil {
			ta.Restored = true
			ta.RestoredVersion = cfg.StateVersion
			cfg.RestoreState(blob)
		}
		return ta
	}
	defer func() { viewConsole, openViewWindow = oldCon, oldOpen }()
	if rc := dispatch(nil); rc != 0 {
		t.Fatalf("dispatch(nil) rc=%d", rc)
	}
	var restored bool
	for _, l := range lines {
		if strings.HasPrefix(l, "goinfer: state restored journey=stub") {
			restored = true
		}
	}
	if !restored {
		t.Fatalf("no restored marker: %v", lines)
	}
}

// The whole bare-launch path, pinned: when the window opens the view
// reaches its event loop (PollEventRaw's ENOSYS exits it on the host) —
// `goinfer: view open id=` is the marker a seat leg can assert.
func TestViewRunsWhenWindowOpens(t *testing.T) {
	var lines []string
	oldCon, oldOpen := viewConsole, openViewWindow
	viewConsole = func(s string) { lines = append(lines, s) }
	openViewWindow = func(tabapp.Config) *tabapp.TabApp {
		return &tabapp.TabApp{Win: 9, W: viewW, H: viewH, OpenOK: true}
	}
	defer func() { viewConsole, openViewWindow = oldCon, oldOpen }()
	if rc := dispatch(nil); rc != 0 {
		t.Fatalf("dispatch(nil) with a window rc=%d, want 0", rc)
	}
	var opened, done bool
	for _, l := range lines {
		if l == "goinfer: view open id=9" {
			opened = true
		}
		if l == "goinfer: done" {
			done = true
		}
	}
	if !opened || !done {
		t.Fatalf("lines=%v, want view-open then done", lines)
	}
}
