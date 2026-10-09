package fsys

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"virelai/vi"
)

// fakeKernel records every slot number the real vi wrappers emit through
// the injected SVC gateway — the host-side equivalent of the gate's strace
// assert. It answers the file slots with a fake handle (7) and otherwise
// -ENOSYS.
type fakeKernel struct {
	slots   []int
	renameR int64 // what slot 35 returns
}

func (k *fakeKernel) hook(num uintptr, a0, a1, a2, a3 uintptr) int64 {
	k.slots = append(k.slots, int(num))
	switch num {
	case vi.SlotFileOpen:
		return 7
	case vi.SlotFileWrite:
		return int64(a2) // accept the whole buffer
	case vi.SlotFileRead:
		return 0
	case vi.SlotFileRename:
		return k.renameR
	}
	return 0
}

func (k *fakeKernel) install(t *testing.T) {
	t.Helper()
	prev := vi.SetSyscallHookForTest(k.hook)
	t.Cleanup(func() { vi.SetSyscallHookForTest(prev) })
}

func (k *fakeKernel) reset() { k.slots = nil }

// TestCreateWriteSyncCloseSlots is the slot-number evidence for the write
// path: exactly 23, 25×2 (3000 bytes crosses the 2048 chunk bound), 77, 26.
// Revert guard: a File.Write switched to os.File.Write, or Sync to a
// no-op, loses its slot and goes red.
func TestCreateWriteSyncCloseSlots(t *testing.T) {
	k := &fakeKernel{}
	k.install(t)

	f, err := Create("/host/G/file")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Write(make([]byte, 3000)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	want := []int{23, 25, 25, 77, 26}
	if len(k.slots) != len(want) {
		t.Fatalf("slots %v, want %v", k.slots, want)
	}
	for i := range want {
		if k.slots[i] != want[i] {
			t.Fatalf("slots %v, want %v", k.slots, want)
		}
	}
}

// TestReadFileSlots: read path is open+read+close — slots 23, 24, 26.
func TestReadFileSlots(t *testing.T) {
	k := &fakeKernel{}
	k.install(t)
	if _, err := ReadFile("/host/G/file", 1<<20); err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := []int{23, 24, 26}
	if len(k.slots) != len(want) {
		t.Fatalf("slots %v, want %v", k.slots, want)
	}
	for i := range want {
		if k.slots[i] != want[i] {
			t.Fatalf("slots %v, want %v", k.slots, want)
		}
	}
}

// TestRenameSlotsAndOverwriteRefusal: slot 35 on success, and the kernel's
// EEXIST answer surfaces as fs.ErrExist — the named refusal of POSIX
// rename-over. Revert to os.Rename loses slot 35 → red.
func TestRenameSlotsAndOverwriteRefusal(t *testing.T) {
	k := &fakeKernel{}
	k.install(t)

	if err := Rename("/host/G/a", "/host/G/b"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if len(k.slots) != 1 || k.slots[0] != 35 {
		t.Fatalf("slots %v, want [35]", k.slots)
	}

	k.reset()
	k.renameR = vi.ErrFileExists
	err := Rename("/host/G/a", "/host/G/b")
	if err == nil || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("Rename over live target: err %v, want fs.ErrExist", err)
	}
	if !strings.Contains(err.Error(), "overwrite") {
		t.Fatalf("refusal %q does not name the overwrite rule", err)
	}
}

// TestReplaceSlots: delete-then-rename — slots 34 then 35.
func TestReplaceSlots(t *testing.T) {
	k := &fakeKernel{}
	k.install(t)
	if err := Replace("/host/G/tmp", "/host/G/a"); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	want := []int{34, 35}
	if len(k.slots) != len(want) {
		t.Fatalf("slots %v, want %v", k.slots, want)
	}
	for i := range want {
		if k.slots[i] != want[i] {
			t.Fatalf("slots %v, want %v", k.slots, want)
		}
	}
}

// TestReadDirSlot: one listing window is slot 27.
func TestReadDirSlot(t *testing.T) {
	k := &fakeKernel{}
	k.install(t)
	ents, err := ReadDir("/host/G")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(ents) != 0 {
		t.Fatalf("entries %v, want empty (fake kernel returns 0)", ents)
	}
	if len(k.slots) != 1 || k.slots[0] != 27 {
		t.Fatalf("slots %v, want [27]", k.slots)
	}
}

// TestPathBoundsRefused: a path over the kernel depth bound fails BEFORE
// any slot burns — the hook must record nothing.
func TestPathBoundsRefused(t *testing.T) {
	k := &fakeKernel{}
	k.install(t)
	deep := "/" + strings.Repeat("d/", 9) + "x"
	if _, err := Open(deep); err == nil {
		t.Fatal("Open of depth-9 path succeeded — kernel bound is 8")
	}
	long := "/host/" + strings.Repeat("n", 300)
	if _, err := Open(long); err == nil {
		t.Fatal("Open of 300-byte component succeeded — kernel bound is 255")
	}
	if len(k.slots) != 0 {
		t.Fatalf("refused paths burned slots %v", k.slots)
	}
}

// TestKernelRefusalSurfaces: a slot-23 ENOENT becomes fs.ErrNotExist.
func TestKernelRefusalSurfaces(t *testing.T) {
	prev := vi.SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		return -vi.ErrENOENT
	})
	defer vi.SetSyscallHookForTest(prev)
	_, err := Open("/host/G/nope")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err %v, want fs.ErrNotExist", err)
	}
}
