// Package proc is the gsport process adapter: spawn, poll/status, kill —
// the PRIMITIVES only; Gostalgia's process model (supervision, parentage)
// is M95e's and does not live here.
//
// Host assumption it replaces: os/exec (fork+exec, waitpid, kill signal)
// and the "child" process kind Gostalgia runs on POSIX. On VirelaiOS there
// is no fork; sys_exec loads a program from the host share into a fresh
// kernel process slot, sys_procs is the status registry, and sys_kill
// ARMS a kill the target observes at its next ring selection — status 137
// is the kernel's recorded answer, never a signal.
//
// Slots: 7 sys_procs (status/scan/wait polling), 28 sys_exec (spawn),
// 29 sys_kill (arm kill); waits pace on slot 4 sys_sleep.
//
// Refuses by name: argv beyond the kernel's exec block (vi.ExecMaxArgs
// args of abi.ExecArgMax bytes each — the kernel does not chop, it refuses),
// pid ≤ 0, and any notion of signalling — Kill is the only verb.
package proc

import (
	"fmt"

	"virelai/gsport/abi"
)

// Status is the explicit mapping of a sys_procs row state.
type Status int

const (
	StatusAbsent  Status = iota // pid not in the registry
	StatusRunning               // present, not yet exited
	StatusExited                // present and exited (ExitCode is the recorded status)
)

// Spawn loads name from the host share into a fresh process and returns
// its pid (slot 28). Argument bounds are the kernel's: abi.ExecMaxArgs
// arguments, abi.ExecArgMax bytes each, and the name carries the same
// limit — all refused here before the slot is burned.
func Spawn(name string, args ...string) (int64, error) {
	if name == "" {
		return 0, fmt.Errorf("spawn: empty program name")
	}
	if len(name) > abi.ExecArgMax {
		return 0, fmt.Errorf("spawn: name %q exceeds %d bytes", name, abi.ExecArgMax)
	}
	if len(args) > abi.ExecMaxArgs {
		return 0, fmt.Errorf("spawn: %d args exceeds the kernel's %d-arg block", len(args), abi.ExecMaxArgs)
	}
	for i, a := range args {
		if len(a) > abi.ExecArgMax {
			return 0, fmt.Errorf("spawn: arg %d exceeds %d bytes", i, abi.ExecArgMax)
		}
	}
	pid, err := abi.Exec(name, args...)
	if err != nil {
		return 0, fmt.Errorf("spawn %s: %w", name, err)
	}
	return pid, nil
}

// Probe reads the registry once for pid (slot 7) and maps the row to an
// explicit Status. The kernel's own codes are the vi.Probe* constants;
// callers see Status*, never raw row bits.
func Probe(pid int64) (Status, int64) {
	if pid <= 0 {
		return StatusAbsent, -1
	}
	st, state := abi.Probe(pid)
	switch state {
	case abi.ProbeExited:
		return StatusExited, st
	case abi.ProbeRunning:
		return StatusRunning, -1
	default:
		return StatusAbsent, -1
	}
}

// Wait blocks until pid exits and returns the recorded status, polling
// the registry between scheduler sleeps (slots 7+4). The seen-then-gone
// pid counts as status 0 (reaped and recycled) — the contract vi.Wait
// pins.
func Wait(pid int64) (int64, error) {
	st, err := abi.Wait(pid)
	if err != nil {
		return -1, fmt.Errorf("wait pid %d: %w", pid, err)
	}
	return st, nil
}

// Kill arms the kill of pid (slot 29). Success means ARMED, never gone:
// the target exits with the kernel's reserved status 137 at its next ring
// selection.
func Kill(pid int64) error {
	if pid <= 0 {
		return fmt.Errorf("kill pid %d: invalid pid", pid)
	}
	return abi.Check("kill", "", abi.Kill(uint64(pid)))
}
