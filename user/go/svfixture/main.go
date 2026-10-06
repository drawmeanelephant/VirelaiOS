// svfixture is one ELF, staged under distinct process names for the monitor.
// Only the external gate kills children; the supervisor never calls Stop.
package main

import (
	"strconv"

	"virelai/supervise"
	"virelai/vi"
)

const (
	restartLabel  = "SVFIX-RESTART"
	neverLabel    = "SVFIX-NEVER"
	receipt5Path  = "/host/SVFIX5.TXT"
	fixtureBudget = int64(180e9)
)

func fail(message string) {
	vi.ConsoleLine("svfixture: FAIL " + message)
	vi.Exit(1)
}

func main() {
	args := vi.Args()
	if childMode(args) {
		vi.ConsoleLine("svfixture: child alive")
		for {
			vi.Sleep(1)
		}
	}
	p := supervise.Policy{Restart: supervise.OnFailure, BackoffBaseS: 2,
		BackoffCapS: 8, MaxRestarts: 5, Window: 1000}
	s, err := supervise.New([]supervise.Service{
		{Name: restartLabel, Binary: "SVFIXCH.ELF", Args: []string{"child"}, Policy: p},
		{Name: neverLabel, Binary: "SVFIXNV.ELF", Args: []string{"child"},
			Policy: supervise.Policy{Restart: supervise.Never}},
	}, supervise.GuestHooks())
	if err != nil {
		fail(err.Error())
		return
	}
	if err := s.Start(restartLabel); err != nil {
		fail(err.Error())
		return
	}
	deadline := vi.Nanos() + fixtureBudget
	readyStarts := uint64(0)
	fifthSaved, neverStarted, neverReady := false, false, false
	neverDoneAt := int64(0)
	for vi.Nanos() < deadline {
		if err := s.Tick(); err != nil {
			fail(err.Error())
			return
		}
		r, _ := s.Snapshot(restartLabel)
		if !fifthSaved && r.State == supervise.Backoff && r.Restart == 5 {
			body, rc := vi.ReadFileAll(vi.CrashReceiptPath(restartLabel), 1024)
			if rc < 0 || vi.WriteFileSafe(receipt5Path, body) < 0 {
				fail("fifth receipt copy")
				return
			}
			fifthSaved = true
			vi.ConsoleLine("svfixture: fifth receipt saved")
		}
		if r.State == supervise.Running && r.Observed && r.Starts != readyStarts {
			readyStarts = r.Starts
			vi.ConsoleLine("svfixture: ready mode=restart n=" + strconv.FormatUint(r.Starts, 10))
		}
		if r.State == supervise.Failed && !neverStarted {
			if r.Starts != 6 || r.Restart != 5 || !fifthSaved {
				fail("restart accounting")
				return
			}
			if err := s.Start(neverLabel); err != nil {
				fail(err.Error())
				return
			}
			neverStarted = true
		}
		n, _ := s.Snapshot(neverLabel)
		if n.State == supervise.Running && n.Observed && !neverReady {
			neverReady = true
			vi.ConsoleLine("svfixture: ready mode=never n=1")
		}
		if n.State == supervise.Exited {
			if n.Status != supervise.StatusKilled || n.Starts != 1 {
				fail("never accounting")
				return
			}
			if neverDoneAt == 0 {
				neverDoneAt = vi.Nanos()
			}
			// Observe beyond the largest retry delay, so a broken never
			// policy cannot pass merely because its timer has not fired.
			if vi.Nanos()-neverDoneAt >= 12e9 {
				vi.ConsoleLine("svfixture: complete")
				return
			}
		}
		vi.Sleep(1)
	}
	fail("deadline")
}

func childMode(args []string) bool {
	return len(args) == 2 && args[1] == "child"
}
