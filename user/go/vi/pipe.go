// The kernel pipe rows (M19 P1, ADR 0007 slots 56/57): the bounded
// per-process conduit GOSH's `|` uses to hand a left command's capture to
// the right command's stdin, exactly as SH.BIN's engine does at EL0. Since
// M97g #2081 each process holds its OWN 4 KiB pipe slot (kernel/src/pipe.zig
// el0 table) — a read/write never touches another process's staged bytes,
// and a full table refuses -ENOSPC. A read is NON-BLOCKING (0 when empty)
// and a write that does not fit is refused -ENOSPC — the sequential shell
// never hits either edge, but the wrappers surface them honestly anyway.
// On the host every call degrades to -ENOSYS.
package vi

import "unsafe"

// PipeCapacity is the kernel's single pipe buffer size (kernel/src/pipe.zig
// pipe_capacity): the most bytes a pipeline hand-off can carry in one go.
const PipeCapacity = 4096

// PipeWrite copies b into the kernel pipe (slot 57), returning the bytes
// stored. A write larger than the 4 KiB pipe capacity is refused with
// EINVAL before any byte moves; a write that does not fit the remaining
// room is refused with ENOSPC (the kernel drops nothing).
func PipeWrite(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if len(b) > PipeCapacity {
		return 0, errno(ErrEINVAL)
	}
	r := svc2(SlotPipeWrite, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	if r < 0 {
		return 0, errno(-r)
	}
	return int(r), nil
}

// PipeRead drains up to len(buf) unread bytes out of the kernel pipe
// (slot 56). It returns (0, nil) when the pipe is empty — the read never
// blocks — and the byte count otherwise.
func PipeRead(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	r := svc2(SlotPipeRead, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r < 0 {
		return 0, errno(-r)
	}
	return int(r), nil
}
