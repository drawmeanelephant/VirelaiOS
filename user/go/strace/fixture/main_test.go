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
