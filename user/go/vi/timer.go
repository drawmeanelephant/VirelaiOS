package vi

// TimerSet arms the calling process's single app timer (slot 40) for
// delayTicks scheduler ticks (1 s on VZ). Returns 0 on success or a
// negative errno. Re-arming replaces any pending timer.
func TimerSet(delayTicks uint64) int64 {
	return svc1(SlotTimerSet, uintptr(delayTicks))
}

// TimerCancel disarms the calling process's single app timer (slot 41).
// Returns 1 when a pending timer was cancelled, 0 when none was armed,
// or a negative errno.
func TimerCancel() int64 {
	return svc0(SlotTimerCancel)
}

// TimerSetNs arms the calling process's single app timer (slot 95,
// ADR 0060 D2) for ns nanoseconds. Returns 0 on success or a negative
// errno (for example -ErrEACCES when the process lacks the `timer` grant,
// or -ErrEINVAL for ns == 0 or ns > 3600 s). On a pre-card-#3036 kernel
// that returns -ErrENOSYS, falls back to slot 40 rounding up to whole ticks.
func TimerSetNs(ns uint64) int64 {
	r := svc1(SlotTimerSetNs, uintptr(ns))
	if r == -ErrENOSYS {
		return TimerSet((ns + 999_999_999) / 1_000_000_000)
	}
	return r
}
