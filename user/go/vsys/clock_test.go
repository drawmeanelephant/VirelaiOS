package vsys

import "testing"

// fakeClock injects a deterministic monotonic clock (the nowFn seam).
func fakeClock(t *testing.T, fn func() int64) {
	t.Helper()
	prev := nowFn
	nowFn = fn
	t.Cleanup(func() { nowFn = prev })
}

func TestTicksToNanos(t *testing.T) {
	cases := []struct {
		name  string
		ticks uint64
		freq  uint64
		want  int64
	}{
		{"zero ticks", 0, 24_000_000, 0},
		{"one second at VZ 24MHz", 24_000_000, 24_000_000, 1_000_000_000},
		{"half second at VZ 24MHz", 12_000_000, 24_000_000, 500_000_000},
		{"one and a half seconds", 36_000_000, 24_000_000, 1_500_000_000},
		{"one tick at VZ 24MHz", 1, 24_000_000, 41}, // 41.67ns truncated
		{"unprogrammed counter is an honest zero", 12345, 0, 0},
		{"ns-granular counter passes through", 999, 1_000_000_000, 999},
		{"faster than ns counter passes through", 999, 2_000_000_000, 999},
	}
	for _, tc := range cases {
		if got := ticksToNanos(tc.ticks, tc.freq); got != tc.want {
			t.Errorf("%s: ticksToNanos(%d,%d) = %d, want %d", tc.name, tc.ticks, tc.freq, got, tc.want)
		}
	}
}

func TestNanotimeUsesInjectedClock(t *testing.T) {
	fakeClock(t, func() int64 { return 7_000_000_000 })
	if got := Nanotime(); got != 7_000_000_000 {
		t.Fatalf("Nanotime() = %d, want the injected 7e9", got)
	}
}

func TestNowReadsExistingEpochSlot(t *testing.T) {
	prev := syscallFn
	t.Cleanup(func() { syscallFn = prev })
	syscallFn = func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		if num != 66 || a0 != 0 || a1 != 0 || a2 != 0 || a3 != 0 {
			t.Fatalf("Now syscall = (%d,%d,%d,%d,%d), want slot 66 with no args", num, a0, a1, a2, a3)
		}
		return 1_789_043_696
	}
	if got := Now(); got != 1_789_043_696 {
		t.Fatalf("Now() = %d", got)
	}
	syscallFn = func(num uintptr, a0, a1, a2, a3 uintptr) int64 { return -ErrENOSYS }
	if got := Now(); got != -ErrENOSYS {
		t.Fatalf("Now() without firmware epoch = %d, want -ENOSYS", got)
	}
}

func TestSetReadDeadline_AbsoluteAndClearable(t *testing.T) {
	resetConn(t)
	fakeClock(t, func() int64 { return 100_000_000 })
	fakeKern(t, func(num uintptr, a0, a1, a2, a3 uintptr) int64 { return 0 })
	c, err := Dial("10.0.2.2", 80)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	c.SetReadDeadline(2_000_000_000)
	if c.deadlineAt != 2_100_000_000 {
		t.Fatalf("deadlineAt = %d, want now+2s = 2100000000", c.deadlineAt)
	}
	// Non-positive clears the deadline back to the default budget.
	c.SetReadDeadline(0)
	if c.deadlineAt != 0 {
		t.Fatalf("deadlineAt after SetReadDeadline(0) = %d, want 0", c.deadlineAt)
	}
	c.SetReadDeadline(-1)
	if c.deadlineAt != 0 {
		t.Fatalf("deadlineAt after SetReadDeadline(-1) = %d, want 0", c.deadlineAt)
	}
}
