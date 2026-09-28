package layout

import "testing"

// press builds the Input the kernel would deliver for a physical key under a
// layout: usage plus the symbol its table translates it to (0 when the key has
// none, which is how a dead key arrives).
func press(id ID, usage uint32, shift bool) Input {
	in := Input{Usage: usage, Shift: shift}
	if usage <= 0xff {
		if r, ok := Translate(id, uint8(usage), shift); ok {
			in.Sym = r
		}
	}
	return in
}

const (
	keyE     = 0x08
	keyX     = 0x1b
	keyAcute = 0x2e // DE: dead acute, Shift dead grave
	keyCirc  = 0x35 // DE: dead circumflex
	keyEnter = 0x28
	keyEsc   = 0x29
	keyBksp  = 0x2a
	keyTab   = 0x2b
	keySpace = 0x2c
	keyLeft  = 0x50
)

func want(t *testing.T, step Step, pass bool, commit string) {
	t.Helper()
	if step.Pass != pass || string(step.Commit) != commit {
		t.Fatalf("step = {commit %q pass %v}, want {commit %q pass %v}", string(step.Commit), step.Pass, commit, pass)
	}
}

func TestDeadAcuteThenBaseComposes(t *testing.T) {
	s := NewStage(DE)
	want(t, s.Feed(press(DE, keyAcute, false)), false, "")
	if a, ok := s.Pending(); !ok || a != Acute {
		t.Fatalf("after the dead key Pending = (%q,%v), want the acute", a, ok)
	}
	if s.Preedit() != "'" {
		t.Fatalf("Preedit = %q, want the paintable stand-in for the acute", s.Preedit())
	}
	want(t, s.Feed(press(DE, keyE, false)), false, "é")
	if _, ok := s.Pending(); ok || s.Preedit() != "" {
		t.Fatal("a composed character must clear the pending state")
	}
	// Shift gives the capital, and Shift on the dead key is a different accent.
	s.Feed(press(DE, keyAcute, false))
	want(t, s.Feed(press(DE, keyE, true)), false, "É")
	s.Feed(press(DE, keyAcute, true))
	if a, _ := s.Pending(); a != Grave {
		t.Fatalf("Shift+acute key stages %q, want the grave", a)
	}
	want(t, s.Feed(press(DE, keyE, false)), false, "è")
}

func TestCircumflexIsDeadEvenThoughTheKernelTypesALiteral(t *testing.T) {
	in := press(DE, keyCirc, false)
	if in.Sym != '^' {
		t.Fatalf("test premise: the kernel delivers %q for this key", in.Sym)
	}
	s := NewStage(DE)
	want(t, s.Feed(in), false, "")
	if a, ok := s.Pending(); !ok || a != Circumflex {
		t.Fatalf("Pending = (%q,%v), want the circumflex", a, ok)
	}
	want(t, s.Feed(press(DE, 0x12, false)), false, "ô")
	// Shift is the degree sign, which stays an ordinary symbol.
	want(t, s.Feed(press(DE, keyCirc, true)), true, "")
}

func TestStrayCombinationResolvesToLiteralCharacters(t *testing.T) {
	s := NewStage(DE)
	s.Feed(press(DE, keyAcute, false))
	want(t, s.Feed(press(DE, keyX, false)), false, "´x")
	if _, ok := s.Pending(); ok {
		t.Fatal("a stray must not leave the accent staged")
	}
	s.Feed(press(DE, keyAcute, true))
	want(t, s.Feed(press(DE, keyX, true)), false, "`X")
	s.Feed(press(DE, keyCirc, false))
	want(t, s.Feed(press(DE, 0x1e, false)), false, "^1")
	// The next key after a resolved stray is an ordinary key again.
	want(t, s.Feed(press(DE, keyX, false)), true, "")
}

func TestBareAccentOnPurpose(t *testing.T) {
	s := NewStage(DE)
	s.Feed(press(DE, keyAcute, false))
	want(t, s.Feed(press(DE, keySpace, false)), false, "´")
	// The same dead key twice is also the bare accent, once.
	s.Feed(press(DE, keyAcute, false))
	want(t, s.Feed(press(DE, keyAcute, false)), false, "´")
	if _, ok := s.Pending(); ok {
		t.Fatal("dead key twice must leave nothing staged")
	}
}

func TestDifferentDeadKeyCommitsTheFirstAndStagesTheSecond(t *testing.T) {
	s := NewStage(DE)
	s.Feed(press(DE, keyAcute, false))
	want(t, s.Feed(press(DE, keyAcute, true)), false, "´")
	if a, ok := s.Pending(); !ok || a != Grave {
		t.Fatalf("Pending = (%q,%v), want the grave still staged", a, ok)
	}
	want(t, s.Feed(press(DE, keyE, false)), false, "è")
}

func TestBackspaceDropsTheStagedAccent(t *testing.T) {
	s := NewStage(DE)
	s.Feed(press(DE, keyAcute, false))
	want(t, s.Feed(press(DE, keyBksp, false)), false, "")
	if _, ok := s.Pending(); ok {
		t.Fatal("Backspace must drop the staged accent")
	}
	// A source with no usage spells Backspace as its codepoint.
	s.Feed(press(DE, keyAcute, false))
	want(t, s.Feed(Input{Sym: 0x08}), false, "")
	s.Feed(press(DE, keyAcute, false))
	want(t, s.Feed(Input{Sym: 0x7f}), false, "")
	// With nothing staged Backspace is not the stage's business.
	want(t, s.Feed(press(DE, keyBksp, false)), true, "")
}

func TestNonTextKeysCommitTheLiteralAndStillDoTheirJob(t *testing.T) {
	keys := []struct {
		name string
		in   Input
	}{
		{"enter", press(DE, keyEnter, false)},
		{"tab", press(DE, keyTab, false)},
		{"escape", press(DE, keyEsc, false)},
		{"left arrow", press(DE, keyLeft, false)},
		{"ctrl+s", Input{Usage: 0x16, Chord: true, Sym: 0x13}},
		{"alt+e", Input{Usage: keyE, Chord: true, Sym: 'e'}},
		{"unmapped sym", Input{Usage: 0x39}},
		{"surrogate", Input{Usage: 0x04, Sym: 0xd800}},
	}
	for _, k := range keys {
		s := NewStage(DE)
		s.Feed(press(DE, keyAcute, false))
		step := s.Feed(k.in)
		if !step.Pass || string(step.Commit) != "´" {
			t.Errorf("%s: step = {commit %q pass %v}, want the literal accent then the key", k.name, string(step.Commit), step.Pass)
		}
		if _, ok := s.Pending(); ok {
			t.Errorf("%s: accent still staged", k.name)
		}
	}
}

func TestLayoutsWithoutDeadKeysNeverStage(t *testing.T) {
	for _, s := range []Stage{NewStage(US), {}, NewStage("fr")} {
		// Under US the same physical key is '='; it must reach the text.
		step := s.Feed(press(US, keyAcute, false))
		want(t, step, true, "")
		if _, ok := s.Pending(); ok {
			t.Fatalf("layout %q staged a key", s.Layout())
		}
	}
}

func TestFlushCancelAndSetLayout(t *testing.T) {
	s := NewStage(DE)
	if got := s.Flush(); got != nil {
		t.Fatalf("Flush with nothing staged = %q", string(got))
	}
	s.Feed(press(DE, keyAcute, false))
	if got := s.Flush(); string(got) != "´" {
		t.Fatalf("Flush = %q, want the literal accent", string(got))
	}
	if _, ok := s.Pending(); ok {
		t.Fatal("Flush must clear the state")
	}
	s.Feed(press(DE, keyAcute, false))
	s.Cancel()
	if _, ok := s.Pending(); ok {
		t.Fatal("Cancel must clear the state")
	}
	s.Feed(press(DE, keyAcute, false))
	s.SetLayout(US)
	if _, ok := s.Pending(); ok || s.Layout() != US {
		t.Fatal("SetLayout must drop a sequence that belongs to the old layout")
	}
}

func TestFeedDoesNotAllocate(t *testing.T) {
	s := NewStage(DE)
	acute, e := press(DE, keyAcute, false), press(DE, keyE, false)
	if n := testing.AllocsPerRun(100, func() {
		s.Feed(acute)
		s.Feed(e)
	}); n != 0 {
		t.Fatalf("a dead-key sequence allocated %v times per run", n)
	}
}
