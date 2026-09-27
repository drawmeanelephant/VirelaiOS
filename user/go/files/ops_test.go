package main

import (
	"strings"
	"testing"
)

// vi's errno magnitudes, aliased for the host tests (ADR 0007 D3): the host
// stub returns the negation just like the guest does.
const (
	viErrEINVAL = 1
	viErrENOSYS = 4
)

func TestBaseName(t *testing.T) {
	cases := map[string]string{
		"/host/FM/KNOWN.TXT": "KNOWN.TXT",
		"/host/FM/":          "FM",
		"/host/FM":           "FM",
		"/host":              "host",
		"":                   "",
	}
	for in, want := range cases {
		if got := baseName(in); got != want {
			t.Errorf("baseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidName(t *testing.T) {
	good := []string{"a", "KNOWN.TXT", "newname.txt", "with space", "UPPER"}
	for _, n := range good {
		if !validName(n) {
			t.Errorf("validName(%q) = false, want true", n)
		}
	}
	bad := []string{"", ".", "..", "a/b", "with\nctrl", "tab\there",
		strings.Repeat("x", maxNameLen+1)}
	for _, n := range bad {
		if validName(n) {
			t.Errorf("validName(%q) = true, want false", n)
		}
	}
}

// Validation must fail closed BEFORE any syscall: on the host that means
// -ErrEINVAL (not the host stub's -ErrENOSYS), which is how the test tells
// the two apart.
func TestRenameEntryValidatesFirst(t *testing.T) {
	if rc := renameEntry("/host/FM", "KNOWN.TXT", "a/b"); rc != -viErrEINVAL {
		t.Errorf("slash target rc=%d, want %d", rc, -viErrEINVAL)
	}
	if rc := renameEntry("/host/FM", "", "x"); rc != -viErrEINVAL {
		t.Errorf("empty source rc=%d, want %d", rc, -viErrEINVAL)
	}
	if rc := renameEntry("/host/FM", "KNOWN.TXT", "KNOWN.TXT"); rc != -viErrEINVAL {
		t.Errorf("identity rename rc=%d, want %d", rc, -viErrEINVAL)
	}
	// A well-formed rename reaches the syscall: ENOSYS on the host.
	if rc := renameEntry("/host/FM", "KNOWN.TXT", "newname.txt"); rc != -viErrENOSYS {
		t.Errorf("valid rename rc=%d, want %d", rc, -viErrENOSYS)
	}
}

func TestDeleteEntryValidatesFirst(t *testing.T) {
	if _, rc := deleteEntry("/host/FM", ".."); rc != -viErrEINVAL {
		t.Errorf("dotdot delete rc=%d, want %d", rc, -viErrEINVAL)
	}
	if _, rc := deleteEntry("/host/FM", "KNOWN.TXT"); rc != -viErrENOSYS {
		t.Errorf("valid delete rc=%d, want %d", rc, -viErrENOSYS)
	}
}

func TestPasteGuards(t *testing.T) {
	// An at-cap body is refused, never silently truncated.
	big := make([]byte, maxPasteBytes)
	if rc := pasteCopy("/host/FM", "/host/OTHER/X", big); rc != -viErrEINVAL {
		t.Errorf("at-cap paste rc=%d, want %d", rc, -viErrEINVAL)
	}
	// A normal body reaches WriteFileSafe (ENOSYS on the host).
	if rc := pasteCopy("/host/FM", "/host/OTHER/X", []byte("hi")); rc != -viErrENOSYS {
		t.Errorf("paste copy rc=%d, want %d", rc, -viErrENOSYS)
	}
	// Moving a file onto itself is EINVAL, not a syscall.
	if rc := pasteMove("/host/FM", "/host/FM/X"); rc != -viErrEINVAL {
		t.Errorf("self move rc=%d, want %d", rc, -viErrEINVAL)
	}
	if rc := pasteMove("/host/OTHER", "/host/FM/X"); rc != -viErrENOSYS {
		t.Errorf("cross-dir move rc=%d, want %d", rc, -viErrENOSYS)
	}
	// Reads fail honestly on the host.
	if _, rc := readCapped("/host/FM/KNOWN.TXT", maxPreviewBytes); rc != -viErrENOSYS {
		t.Errorf("readCapped rc=%d, want %d", rc, -viErrENOSYS)
	}
}

func TestSanitizePreviewNeutralizesEscapes(t *testing.T) {
	got := sanitizePreview([]byte("ab\x1b[31m\ncd\te\x00"))
	// Only ESC, NUL and the control byte are replaced (one '·' each); the
	// CSI body is printable ASCII and passes through.
	want := "ab·[31m\ncd e·"
	if got != want {
		t.Errorf("sanitizePreview = %q, want %q", got, want)
	}
}
