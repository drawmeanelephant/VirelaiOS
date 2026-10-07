package heap

import (
	"encoding/binary"
	"testing"

	"virelai/vi"
)

func TestResolvePIDZeroAndAmbiguousName(t *testing.T) {
	count := 1
	prior := vi.SetSyscallHookForTest(func(slot, pointer, size, _, _ uintptr) int64 {
		if slot != vi.SlotProcs {
			t.Fatalf("unexpected slot=%d", slot)
		}
		buf := hookBuffer(pointer, size)
		for i := 0; i < count; i++ {
			offset := i * vi.ProcRowSize
			binary.LittleEndian.PutUint64(buf[offset:], uint64(i))
			binary.LittleEndian.PutUint64(buf[offset+8:], vi.ProcRunning)
			copy(buf[offset+24:], "GOEDIT.ELF")
		}
		return int64(count)
	})
	defer vi.SetSyscallHookForTest(prior)
	if row, err := Resolve("0"); err != nil || row.PID != 0 || row.Name() != "GOEDIT.ELF" {
		t.Fatalf("pid zero row=%+v err=%v", row, err)
	}
	if row, err := Resolve("GOEDIT.ELF"); err != nil || row.PID != 0 {
		t.Fatalf("by name row=%+v err=%v", row, err)
	}
	count = 2
	if _, err := Resolve("GOEDIT.ELF"); err == nil {
		t.Fatal("accepted ambiguous live name")
	}
	if _, err := Resolve("missing"); err == nil {
		t.Fatal("accepted missing target")
	}
}
