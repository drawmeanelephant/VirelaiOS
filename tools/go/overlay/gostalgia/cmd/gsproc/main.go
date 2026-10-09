//go:build virelai

// gsproc is the M95e class-B fixture: it drives the pinned Gostalgia
// internal/process child API against real kernel EL0 processes (spawn via
// slot 28, liveness via slot 7, kill via slot 29) and prints one bounded
// "gsproc:" marker per observable fact. The gate owns the one external
// kill and reads the M82e CRASH receipts straight off the share.
//
// Sequence: clean exit + APPLOG capture, an idle child the gate kills
// (status 137), a BRK fault (status 139), and an on-failure crash loop
// that spends its restart budget (6 starts, 5 retries, give-up, refused
// restart). Exit is nonzero on any surprise — markers are evidence, not
// decoration.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"gostalgia/internal/events"
	"gostalgia/internal/process"

	"virelai/supervise"
)

func marker(format string, args ...any) {
	fmt.Printf("gsproc: "+format+"\n", args...)
}

func fatalf(format string, args ...any) {
	fmt.Printf("gsproc: FAIL "+format+"\n", args...)
	os.Exit(70)
}

func waitDone(p *process.Process, name string) process.Info {
	select {
	case <-p.Done():
		return p.Info()
	case <-time.After(120 * time.Second):
		fatalf("timeout waiting for %s", name)
	}
	return process.Info{}
}

func main() {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	procs := process.NewManager(events.NewBus(), log)
	children := procs.Children()
	if children == nil {
		fatalf("child adapter unavailable")
	}
	ctx := context.Background()

	live := func() map[int64]bool {
		rows, err := children.Table()
		if err != nil {
			fatalf("process table: %v", err)
		}
		live := map[int64]bool{}
		for _, r := range rows {
			if r.Running && !r.Exited {
				live[r.PID] = true
			}
		}
		return live
	}
	baseline := live()
	marker("ready baseline=%d", len(baseline))

	// Clean exit 0 plus bounded capture: the child wrote its own APPLOG
	// ring; the parent's LogTail read is the only output channel an EL0
	// child has.
	clean, err := procs.StartChild(ctx, process.Spec{
		Name: "GSCHK",
		Args: []string{"GSCHK.ELF", "log", "GSCHK", "4", "exit", "0"},
	})
	if err != nil {
		fatalf("start GSCHK: %v", err)
	}
	info := waitDone(clean, "GSCHK")
	marker("exit name=GSCHK state=%s code=%d", info.State, info.ExitCode)
	if info.State != process.StateStopped || info.ExitCode != 0 {
		fatalf("GSCHK expected stopped/0, got %s/%d", info.State, info.ExitCode)
	}
	lines, truncated, err := children.LogTail("GSCHK", 8)
	if err != nil {
		fatalf("applog GSCHK: %v", err)
	}
	for _, l := range lines {
		marker("applog GSCHK %s", l)
	}
	marker("capture name=GSCHK lines=%d truncated=%t", len(lines), truncated)

	// External kill: the gate kills GSKILL.ELF while it sleeps. A 137 that
	// is not supervisor-initiated is a failure, and supervise writes the
	// receipt.
	killed, err := procs.StartChild(ctx, process.Spec{
		Name: "GSKILL", Args: []string{"GSKILL.ELF", "sleep"},
	})
	if err != nil {
		fatalf("start GSKILL: %v", err)
	}
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		snap, _ := children.Snapshot("GSKILL")
		if snap.State == supervise.Running && snap.Observed {
			marker("running name=GSKILL pid=%d", snap.PID)
			break
		}
		if time.Now().After(deadline) {
			fatalf("GSKILL never reached running")
		}
	}
	info = waitDone(killed, "GSKILL")
	marker("exit name=GSKILL state=%s code=%d", info.State, info.ExitCode)
	if info.State != process.StateFailed || info.ExitCode != 137 {
		fatalf("GSKILL expected failed/137, got %s/%d", info.State, info.ExitCode)
	}

	// Unhandled fault: BRK is the only guest-visible instruction class the
	// kernel reaps as a fault rather than a deliverable trap — status 139.
	faulted, err := procs.StartChild(ctx, process.Spec{
		Name: "GSFAULT",
		Args: []string{"GSFAULT.ELF", "log", "GSFAULT", "2", "fault"},
	})
	if err != nil {
		fatalf("start GSFAULT: %v", err)
	}
	info = waitDone(faulted, "GSFAULT")
	marker("exit name=GSFAULT state=%s code=%d", info.State, info.ExitCode)
	if info.State != process.StateFailed || info.ExitCode != 139 {
		fatalf("GSFAULT expected failed/139, got %s/%d", info.State, info.ExitCode)
	}

	// Crash loop: on-failure with a five-retry budget inside the window —
	// six starts, five backoffs, then failed; the next Start is refused
	// and the registry returns to the baseline live count.
	loop := process.Spec{
		Name: "GSLOOP", Args: []string{"GSLOOP.ELF", "exit", "3"},
		Restart: supervise.Policy{Restart: supervise.OnFailure,
			BackoffBaseS: 1, BackoffCapS: 1, MaxRestarts: 5, Window: 30},
	}
	loopProc, err := procs.StartChild(ctx, loop)
	if err != nil {
		fatalf("start GSLOOP: %v", err)
	}
	info = waitDone(loopProc, "GSLOOP")
	snap, _ := children.Snapshot("GSLOOP")
	marker("loop name=GSLOOP state=%s starts=%d restarts=%d code=%d",
		info.State, snap.Starts, snap.Restart, info.ExitCode)
	if info.State != process.StateFailed || snap.Starts != 6 || snap.Restart != 5 {
		fatalf("GSLOOP accounting: state=%s starts=%d restarts=%d",
			info.State, snap.Starts, snap.Restart)
	}
	if _, err := procs.StartChild(ctx, loop); err != nil {
		marker("refused name=GSLOOP err=%q", err.Error())
	} else {
		fatalf("start after give-up was admitted")
	}

	// No leaked live slots: every pid live now must have been live at
	// baseline — a child can never impersonate a baseline row, while a
	// baseline row (e.g. a one-shot SMP task) may legitimately exit.
	leaked := 0
	for pid := range live() {
		if !baseline[pid] {
			leaked++
		}
	}
	marker("procs baseline=%d leaked=%d", len(baseline), leaked)
	if leaked != 0 {
		fatalf("process slots leaked baseline=%d leaked=%d", len(baseline), leaked)
	}
	marker("complete")
}
