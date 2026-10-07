package heap

import (
	"encoding/binary"
	"testing"
	"time"

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

func TestPublicationGapRetriesWithoutAcceptingPartialSeries(t *testing.T) {
	valid := []byte(testSample(1).Row() + "\n")
	for _, tc := range []struct {
		name      string
		bodies    [][]byte
		results   []int64
		wantRows  int
		wantError bool
		wantCalls int
	}{
		{"empty EOF during rename", [][]byte{nil, valid}, []int64{0, int64(len(valid))}, 1, false, 2},
		{"partial row during rename", [][]byte{valid[:5], valid}, []int64{5, int64(len(valid))}, 1, false, 2},
		{"missing then published", [][]byte{nil, valid}, []int64{vi.ErrFileNotFound, int64(len(valid))}, 1, false, 2},
		{"persistent corrupt file", [][]byte{nil, nil, nil}, []int64{0, 0, 0}, 0, true, 3},
		{"no publisher", [][]byte{nil, nil, nil}, []int64{vi.ErrFileNotFound, vi.ErrFileNotFound, vi.ErrFileNotFound}, 0, false, 3},
		{"permission failure", [][]byte{nil}, []int64{-vi.ErrEACCES}, 0, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, sleeps := 0, 0
			rows, err := readSeries("/host/HEAP/HEAPFIX.ELF.TXT", func(path string, max int) ([]byte, int64) {
				if path != "/host/HEAP/HEAPFIX.ELF.TXT" || max != MaxBytes+1 {
					t.Fatal("wrong bounded read")
				}
				body, result := tc.bodies[calls], tc.results[calls]
				calls++
				return body, result
			}, func(duration time.Duration) {
				if duration != time.Second {
					t.Fatal("retry must sleep")
				}
				sleeps++
			})
			if len(rows) != tc.wantRows || (err != nil) != tc.wantError ||
				calls != tc.wantCalls || sleeps != calls-1 {
				t.Fatalf("rows=%d err=%v calls=%d sleeps=%d", len(rows), err, calls, sleeps)
			}
		})
	}
}
