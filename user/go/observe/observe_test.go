package observe

import (
	"testing"

	"virelai/vi"
)

func TestIsSaveMatchesPublishRename(t *testing.T) {
	var r vi.TraceRecord
	r.Number = 35
	r.StringMask = 0b0101 // arg0 old path, arg2 new path
	old := "/host/EDIT/OBSERVE.TXT~"
	newPath := "/host/EDIT/OBSERVE.TXT"
	copy(r.Strings[0][:], old)
	copy(r.Strings[2][:], newPath)
	r.StringLengths[0] = uint16(len(old))
	r.StringLengths[2] = uint16(len(newPath))
	if !isSave(r, newPath) {
		t.Fatal("rename onto the watched path is a save")
	}
	if isSave(r, old) {
		t.Fatal("rename is not a save of the temp sibling")
	}
	r.Flags = vi.TraceRedacted
	if isSave(r, newPath) {
		t.Fatal("a redacted record is never a save")
	}
}

func TestIsSaveIgnoresOtherSlots(t *testing.T) {
	var r vi.TraceRecord
	r.Number = 23
	if isSave(r, "/host/EDIT/OBSERVE.TXT") {
		t.Fatal("an open is not a save")
	}
}

func TestFixedWorkIsDeterministic(t *testing.T) {
	if fixedWork(1) != fixedWork(1) {
		t.Fatal("fixed work diverged")
	}
	if fixedWork(1) == 0 {
		t.Fatal("fixed work produced a zero checksum")
	}
}

func TestRunRejectsBadArgv(t *testing.T) {
	for _, args := range [][]string{
		{"observe"},
		{"observe", "-p"},
		{"observe", "overhead", "extra"},
		{"observe", "-p", "GOEDIT.ELF", "-bogus", "x"},
	} {
		if err := Run(args); err == nil {
			t.Fatalf("argv %v unexpectedly accepted", args)
		}
	}
}
