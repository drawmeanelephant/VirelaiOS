package vi

import "testing"

func TestTimerSetNsCallsSlot95AndPassesRefusalThrough(t *testing.T) {
	var gotNum, gotA0 uintptr
	calls := 0
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		gotNum, gotA0 = num, a0
		calls++
		return 0
	})
	t.Cleanup(func() { SetSyscallHookForTest(prev) })

	if rc := TimerSetNs(500_000_000); rc != 0 {
		t.Fatalf("TimerSetNs rc=%d want 0", rc)
	}
	if calls != 1 || gotNum != SlotTimerSetNs || gotA0 != 500_000_000 {
		t.Fatalf("hook saw calls=%d num=%d a0=%d, want slot %d with 500ms", calls, gotNum, gotA0, SlotTimerSetNs)
	}

	SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		gotNum = num
		return -ErrEACCES
	})
	if rc := TimerSetNs(500_000_000); rc != -ErrEACCES {
		t.Fatalf("refused TimerSetNs rc=%d want -EACCES (%d)", rc, -ErrEACCES)
	}
	if gotNum != SlotTimerSetNs {
		t.Fatalf("refused TimerSetNs fell back to slot %d; only -ENOSYS may fall back", gotNum)
	}
}

func TestTimerSetNsFallsBackToSlot40OnENOSYS(t *testing.T) {
	var slots []uintptr
	var arg uintptr
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		slots = append(slots, num)
		if num == SlotTimerSetNs {
			return -ErrENOSYS
		}
		arg = a0
		return 0
	})
	t.Cleanup(func() { SetSyscallHookForTest(prev) })

	if rc := TimerSetNs(500_000_000); rc != 0 {
		t.Fatalf("fallback TimerSetNs rc=%d want 0", rc)
	}
	if len(slots) != 2 || slots[0] != SlotTimerSetNs || slots[1] != SlotTimerSet || arg != 1 {
		t.Fatalf("fallback saw slots=%v arg=%d want [%d %d] arg=1", slots, arg, SlotTimerSetNs, SlotTimerSet)
	}
}

func TestTimerCancelCallsSlot41(t *testing.T) {
	var gotNum uintptr
	prev := SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		gotNum = num
		return 1
	})
	t.Cleanup(func() { SetSyscallHookForTest(prev) })

	if rc := TimerCancel(); rc != 1 || gotNum != SlotTimerCancel {
		t.Fatalf("TimerCancel rc=%d slot=%d want 1/%d", rc, gotNum, SlotTimerCancel)
	}
}
