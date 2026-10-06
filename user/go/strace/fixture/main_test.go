package main

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"virelai/vi"
)

func TestPairedWorkloadAlternatesEqualOffOnLoops(t *testing.T) {
	active := false
	var opens, closes, offCalls, onCalls int
	previous := vi.SetSyscallHookForTest(func(slot, op, token, ptr, size uintptr) int64 {
		switch slot {
		case vi.SlotTrace:
			switch op {
			case vi.TraceArm:
				if active || size != 88 {
					t.Fatal("arm did not follow off loop")
				}
				config := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), size)
				if binary.LittleEndian.Uint32(config[4:]) != 1 ||
					binary.LittleEndian.Uint64(config[8:]) != 7 ||
					binary.LittleEndian.Uint64(config[72:]) != uint64(1)<<60 {
					t.Fatal("wrong target/slot configuration")
				}
				opens++
				active = true
				return int64(opens)
			case vi.TraceDisarm:
				if !active || token != uintptr(opens) {
					t.Fatal("disarm did not follow on loop")
				}
				closes++
				active = false
				return 0
			default:
				t.Fatal("unexpected control op", op)
			}
		case vi.SlotPingPoll:
			if active {
				onCalls++
			} else {
				offCalls++
			}
		}
		return 0
	})
	defer vi.SetSyscallHookForTest(previous)
	off, on := paired(7, "test", 10000, []uint64{60})
	if off <= 0 || on <= 0 || active || opens != 7 || closes != 7 ||
		offCalls != 7*(1000+10000) || onCalls != offCalls {
		t.Fatalf("off=%d on=%d active=%v opens=%d closes=%d offCalls=%d onCalls=%d",
			off, on, active, opens, closes, offCalls, onCalls)
	}
}

func TestCaptureBatchUsesReadable128BytesAndRefusesOpen(t *testing.T) {
	var path [128]byte
	for i := range path {
		path[i] = 'x'
	}
	calls := 0
	previous := vi.SetSyscallHookForTest(func(slot, ptr, length, flags, unused uintptr) int64 {
		if slot != vi.SlotFileOpen || length != 128 || flags != 0 {
			t.Fatalf("wrong capture syscall shape slot=%d length=%d flags=%d", slot, length, flags)
		}
		if got := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), length); string(got) != string(path[:]) {
			t.Fatal("wrong capture bytes")
		}
		calls++
		return -vi.ErrEINVAL
	})
	defer vi.SetSyscallHookForTest(previous)
	var samples [20]uint64
	captureBatch(samples[:], uintptr(unsafe.Pointer(&path[0])))
	if calls != len(samples) {
		t.Fatal("workload count", calls)
	}
}

func TestCapturePercentilesRetainRawOrder(t *testing.T) {
	var samples [20]uint64
	var added [20]int64
	for i := range samples {
		samples[i] = uint64(20 - i)
		added[i] = int64(10 - i)
	}
	if p95Ticks(samples[:]) != 19 || p95AddedTicks(added[:]) != 9 || totalTicks(samples[:]) != 210 {
		t.Fatal("nearest-rank percentile or total")
	}
	if samples[0] != 20 || added[0] != 10 {
		t.Fatal("percentile calculation mutated raw sample order")
	}
}
