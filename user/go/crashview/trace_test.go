package main

import (
	"encoding/binary"
	"strings"
	"testing"
	"unsafe"

	"virelai/vi"
)

func TestReopenUsesAtomicAPIInSameProcess(t *testing.T) {
	var operations []uint64
	prev := vi.SetSyscallHookForTest(func(slot uintptr, a0, a1, a2, a3 uintptr) int64 {
		if slot == vi.SlotWrite {
			return int64(a2)
		}
		if slot != vi.SlotTrace {
			t.Fatalf("unexpected syscall %d (ordinary exec/separate tracer forbidden)", slot)
		}
		op := uint64(a0)
		operations = append(operations, op)
		buf := unsafe.Slice((*byte)(unsafe.Pointer(a2)), a3)
		switch op {
		case vi.TraceArm:
			if len(buf) != 88 || binary.LittleEndian.Uint32(buf[4:]) != 0 {
				t.Fatal("session must be armed empty before spawning")
			}
			mask := binary.LittleEndian.Uint64(buf[72:])
			for _, included := range []uint64{1, 3, 23, 24, 25, 26} {
				if mask&(1<<included) == 0 {
					t.Fatalf("missing useful trace slot %d", included)
				}
			}
			return 7
		case vi.TraceArmExec:
			if a1 != 7 || len(buf) != 32 || binary.LittleEndian.Uint64(buf[24:]) != 0 {
				t.Fatal("wrong token/exec request; receipts have no argv")
			}
			ptr := binary.LittleEndian.Uint64(buf)
			n := binary.LittleEndian.Uint64(buf[8:])
			path := string(unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), n))
			if path != "CRASHFIX.ELF" {
				t.Fatalf("wrong recorded basename: %q", path)
			}
			return 12
		default:
			t.Fatalf("unexpected trace operation %d", op)
		}
		return 0
	})
	defer vi.SetSyscallHookForTest(prev)
	a := &application{view: newViewer()}
	a.view.records = []record{{app: "CRASHFIX.ELF", kind: "go"}}
	a.reopen()
	a.reopen() // one active child/session, even if the action is repeated
	if len(operations) != 2 || operations[0] != vi.TraceArm ||
		operations[1] != vi.TraceArmExec || a.tracePID != 12 || a.trace == nil ||
		!strings.Contains(a.status, "arguments unavailable") {
		t.Fatalf("reopen ops=%v state=%+v", operations, a)
	}
}
