package speech

import (
	"strings"
	"testing"
)

// fakeShare is the staging gesture's in-memory SAY/ dir: files the
// WriteSafe seam accepts, plus the armed toggle IsDir reads.
type fakeShare struct {
	files map[string][]byte
	armed bool
	io    IO
}

func newFakeShare() *fakeShare {
	f := &fakeShare{files: map[string][]byte{}, armed: true}
	f.io = IO{
		WriteSafe: func(path string, b []byte) int64 {
			name := path[len(Dir)+1:]
			cp := make([]byte, len(b))
			copy(cp, b)
			f.files[name] = cp
			return 0
		},
		IsDir: func(path string) bool { return f.armed },
		Rand:  func(p []byte) (int, error) { p[0] = 0xAB; return 4, nil },
	}
	return f
}

func (f *fakeShare) markers() []string {
	var out []string
	for name := range f.files {
		if IsSayName(name) {
			out = append(out, name)
		}
	}
	return out
}

func TestSayNameGrammar(t *testing.T) {
	if got := SayName(0x000000ab); got != "SAY-000000ab" {
		t.Fatalf("SayName = %q", got)
	}
	for _, name := range []string{"SAY-000000ab", "SAY-FFFFFFFF", "SAY-deadbeef"} {
		if !IsSayName(name) {
			t.Fatalf("%q must parse as a SAY marker", name)
		}
	}
	for _, name := range []string{
		"SAY-", "SAY-123", "SAY-0000000g", "REQ-00000001", "SAY-00000001~",
		"xsay-00000001", "",
	} {
		if IsSayName(name) {
			t.Fatalf("%q must not parse as a SAY marker", name)
		}
	}
}

func TestSayStagesMarker(t *testing.T) {
	f := newFakeShare()
	if r := f.io.Say("hello"); r != "" {
		t.Fatalf("Say refused %q", r)
	}
	marks := f.markers()
	if len(marks) != 1 || marks[0] != "SAY-000000ab" {
		t.Fatalf("markers = %v", marks)
	}
	if got := string(f.files["SAY-000000ab"]); got != "hello" {
		t.Fatalf("marker content = %q", got)
	}
}

func TestSayRefusals(t *testing.T) {
	f := newFakeShare()
	if r := f.io.Say(""); r != RefuseNotText {
		t.Fatalf("empty Say = %q", r)
	}
	if r := f.io.Say("\xff\xfe"); r != RefuseNotText {
		t.Fatalf("bad-utf8 Say = %q", r)
	}
	if r := f.io.Say(strings.Repeat("x", TextMax+1)); r != RefuseTooLarge {
		t.Fatalf("over-cap Say = %q", r)
	}
	f.armed = false
	if r := f.io.Say("hi"); r != RefuseLaneDisabled {
		t.Fatalf("unarmed Say = %q", r)
	}
	f.armed = true
	f.io.WriteSafe = func(string, []byte) int64 { return -5 }
	if r := f.io.Say("hi"); r != RefuseIO {
		t.Fatalf("io-failed Say = %q", r)
	}
	if len(f.markers()) != 0 {
		t.Fatal("a refused Say staged a marker")
	}
}

func TestClamp(t *testing.T) {
	if got := Clamp("short"); got != "short" {
		t.Fatalf("Clamp short = %q", got)
	}
	long := strings.Repeat("x", TextMax) + "é" // one rune past the bound
	got := Clamp(long)
	if len(got) != TextMax {
		t.Fatalf("Clamp cut at %d bytes, want %d", len(got), TextMax)
	}
	// The boundary must not split the rune — the cut lands before it.
	if got[len(got)-1] != 'x' {
		t.Fatal("Clamp split a multi-byte rune")
	}
}
