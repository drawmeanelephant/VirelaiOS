package vsys

// User space has a monotonic clock (GOOS=virelai phase 2.1) and the boot
// firmware's wall-clock epoch (ADR 0007 slot 66).
//
// CNTPCT_EL0 / CNTFRQ_EL0 are EL0-readable: the kernel arms
// CNTKCTL_EL1.EL0PCTEN|EL0VCTEN in timer.allow_el0_counter() (see
// kernel/src/timer.zig, and ADR 0007's "so EL0 processes can read time
// without a syscall slot"). Before this file, no Go package used that
// capability, so every deadline in the Go surface was expressed as a
// SCHEDULER-TICK budget (1 tick ~= 1 s) — the bound the phase-2 handoff
// left open ("user space has no clock source in this runtime yet").
//
// Nanotime closes that bound for the Go surface: deadlines become
// wall-clock values, so a budget shorter than one tick is not silently
// rounded up, and a stalled tick cannot stretch a deadline.

// nowFn is the clock source. It is a variable so host tests can inject a
// deterministic clock — the same injection seam as syscallFn.
var nowFn = platformNano

// SlotTime is the existing ADR 0007 wall-clock slot (#1058), not an M83a
// addition. vi.Time and the Go runtime port use the same ABI number.
const SlotTime uintptr = 66

// Nanotime returns the guest's monotonic clock in nanoseconds since boot.
//
// It reads the architectural counter directly (no syscall, no fd, no
// alloc); only the multiply/divide is Go. On a non-virelai build it
// returns 0 (the host has no VirelaiOS counter) — inject nowFn in tests.
func Nanotime() int64 { return nowFn() }

// Now returns Unix wall-clock seconds from the kernel's existing sys_time
// slot. -ENOSYS means the firmware supplied no usable epoch: callers must
// not turn Nanotime into a calendar date. The host gateway returns -ENOSYS
// too, and tests can inject the syscallFn seam.
func Now() int64 { return syscallFn(SlotTime, 0, 0, 0, 0) }

// ticksToNanos converts a raw CNTPCT_EL0 reading to nanoseconds using
// CNTFRQ_EL0. Split into whole seconds plus a remainder so no intermediate
// product leaves 64 bits for the counter frequencies VZ programs
// (24 MHz nominal, and anything up to 1 GHz where the counter already
// counts nanoseconds). Pure — exercised on the host by clock_test.go.
func ticksToNanos(ticks, freq uint64) int64 {
	if freq == 0 {
		return 0 // an unprogrammed counter is an honest zero, not a guess
	}
	if freq > 1_000_000_000 {
		return int64(ticks) // faster than ns: the counter is the clock
	}
	sec := ticks / freq
	rem := ticks % freq
	return int64(sec)*1_000_000_000 + int64(rem)*1_000_000_000/int64(freq)
}
