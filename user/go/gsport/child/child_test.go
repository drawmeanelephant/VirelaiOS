package child

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"virelai/supervise"
)

// fakeTable is the injected process registry: pids spawn running, flip to
// exited with a recorded status, and kills arm the 137 answer the kernel
// gives on the next poll.
type fakeTable struct {
	now      int64
	nextPID  int64
	execErr  error
	rows     map[int64]*supervise.Process
	killed   []uint64
	receipts []string
	serial   []string
	logs     map[string][]byte
}

func newFake() *fakeTable {
	return &fakeTable{now: 1_000_000_000, nextPID: 100, rows: map[int64]*supervise.Process{},
		logs: map[string][]byte{}}
}

func (f *fakeTable) hooks() Hooks {
	return Hooks{
		Sup: supervise.Hooks{
			Now: func() int64 { return f.now },
			Exec: func(name string, args ...string) (int64, error) {
				if f.execErr != nil {
					return 0, f.execErr
				}
				f.nextPID++
				f.rows[f.nextPID] = &supervise.Process{PID: f.nextPID, Running: true}
				return f.nextPID, nil
			},
			Table: func() ([]supervise.Process, error) {
				out := make([]supervise.Process, 0, len(f.rows))
				for _, r := range f.rows {
					out = append(out, *r)
				}
				return out, nil
			},
			Kill: func(pid uint64) int64 {
				f.killed = append(f.killed, pid)
				if r, ok := f.rows[int64(pid)]; ok {
					r.Running, r.Exited, r.Status = false, true, supervise.StatusKilled
					return 0
				}
				return -3 // ESRCH-shaped refusal
			},
			Random: func(b []byte) (int, error) {
				for i := range b {
					b[i] = 0
				}
				return len(b), nil
			},
			Receipt: func(app, outcome string) int64 {
				f.receipts = append(f.receipts, app+"|"+outcome)
				return 0
			},
			Serial: func(s string) { f.serial = append(f.serial, s) },
		},
		ReadLog: func(label string) ([]byte, error) { return f.logs[label], nil },
	}
}

func (f *fakeTable) exit(pid int64, status int64) {
	f.rows[pid].Running, f.rows[pid].Exited, f.rows[pid].Status = false, true, status
}

func (f *fakeTable) lastReceipt() string {
	if len(f.receipts) == 0 {
		return ""
	}
	return f.receipts[len(f.receipts)-1]
}

var neverSpec = Spec{Name: "DEMO", Args: []string{"DEMO.ELF"}}

// advance runs ticks until the child leaves non-terminal states or the
// budget is spent — the poll loop the guest overlay runs.
func advance(t *testing.T, f *fakeTable, m *Manager, name string, steps int) Snapshot {
	t.Helper()
	for i := 0; i < steps; i++ {
		if err := m.Tick(); err != nil {
			t.Fatalf("tick: %v", err)
		}
		snap, ok := m.Snapshot(name)
		if !ok {
			t.Fatalf("snapshot %s missing", name)
		}
		switch snap.State {
		case supervise.Exited, supervise.Stopped, supervise.Failed:
			return snap
		}
		f.now += 100_000_000 // keep the clock moving for window math
	}
	snap, _ := m.Snapshot(name)
	return snap
}

func TestCleanExit(t *testing.T) {
	f := newFake()
	m, err := New(f.hooks())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(neverSpec); err != nil {
		t.Fatal(err)
	}
	if err := m.Tick(); err != nil {
		t.Fatal(err)
	}
	snap, _ := m.Snapshot("DEMO")
	if snap.State != supervise.Running || snap.PID == 0 {
		t.Fatalf("want running with pid, got %+v", snap)
	}
	f.exit(snap.PID, 0)
	snap = advance(t, f, m, "DEMO", 3)
	if snap.State != supervise.Exited || snap.Status != 0 {
		t.Fatalf("clean exit -> %+v", snap)
	}
	if len(f.receipts) != 0 {
		t.Fatalf("clean exit wrote receipts: %v", f.receipts)
	}
}

// Each non-zero exit status is a failure with a receipt under never:
// restart=0/0 per ADR 0042 §7.
func TestFailureStatuses(t *testing.T) {
	for _, status := range []int64{2, 137, 139} {
		f := newFake()
		m, _ := New(f.hooks())
		spec := Spec{Name: fmt.Sprintf("S%d", status), Args: []string{"X.ELF"}}
		if err := m.Start(spec); err != nil {
			t.Fatal(err)
		}
		m.Tick()
		snap, _ := m.Snapshot(spec.Name)
		f.exit(snap.PID, status)
		snap = advance(t, f, m, spec.Name, 3)
		if snap.State != supervise.Exited || snap.Status != status {
			t.Fatalf("status %d -> %+v", status, snap)
		}
		want := fmt.Sprintf("%s|restart=0/0 status=%d backoff_s=0", spec.Name, status)
		if got := f.lastReceipt(); got != want {
			t.Fatalf("status %d receipt %q, want %q", status, got, want)
		}
	}
}

// A supervisor stop is never a failure: kill arms, the 137 lands, the
// state folds to stopped with no receipt.
func TestSupervisorStop(t *testing.T) {
	f := newFake()
	m, _ := New(f.hooks())
	if err := m.Start(neverSpec); err != nil {
		t.Fatal(err)
	}
	m.Tick()
	snap, _ := m.Snapshot("DEMO")
	pid := snap.PID
	if err := m.Stop("DEMO"); err != nil {
		t.Fatal(err)
	}
	if len(f.killed) != 1 || f.killed[0] != uint64(pid) {
		t.Fatalf("kill arms %v, want [%d]", f.killed, pid)
	}
	snap = advance(t, f, m, "DEMO", 3)
	if snap.State != supervise.Stopped || snap.Status != supervise.StatusKilled {
		t.Fatalf("stop -> %+v, want stopped/137", snap)
	}
	if len(f.receipts) != 0 {
		t.Fatalf("supervisor stop wrote receipts: %v", f.receipts)
	}
}

// Never started: exec refusal is a failure with a receipt, not a hang.
func TestNeverStarted(t *testing.T) {
	f := newFake()
	f.execErr = errors.New("spawn: ENOSPC")
	m, _ := New(f.hooks())
	if err := m.Start(neverSpec); err != nil {
		t.Fatal(err)
	}
	snap := advance(t, f, m, "DEMO", 3)
	if snap.State != supervise.Exited || snap.Status != supervise.StatusUnknown {
		t.Fatalf("failed spawn -> %+v", snap)
	}
	if got := f.lastReceipt(); !strings.Contains(got, "status=-1") {
		t.Fatalf("spawn-failure receipt %q, want status=-1", got)
	}
}

// Refusals by name happen before any supervisor exists.
func TestRefusals(t *testing.T) {
	f := newFake()
	m, _ := New(f.hooks())
	if err := m.Start(Spec{Name: "bad label!", Args: []string{"X.ELF"}}); err == nil {
		t.Fatal("invalid label accepted")
	}
	if err := m.Start(Spec{Name: "DEMO"}); err == nil {
		t.Fatal("empty argv accepted")
	}
	if len(f.rows) != 0 || len(f.receipts) != 0 {
		t.Fatalf("refusals touched the kernel: %+v %v", f.rows, f.receipts)
	}
	if m.Known("DEMO") {
		t.Fatal("refused spec left a supervisor behind")
	}
	// A second live start on the same label is refused.
	if err := m.Start(neverSpec); err != nil {
		t.Fatal(err)
	}
	m.Tick()
	if err := m.Start(neverSpec); err == nil {
		t.Fatal("second live start accepted")
	}
	// A changed spec on a live label is refused by name, not adopted.
	changed := Spec{Name: "DEMO", Args: []string{"OTHER.ELF"}}
	if err := m.Start(changed); err == nil {
		t.Fatal("spec change on live label accepted")
	}
	// Over-bound argv refuses through supervise's own validation.
	bad := Spec{Name: "BIG", Args: append([]string{"X.ELF"}, make([]string, 9)...)}
	for i := range bad.Args {
		bad.Args[i] = "a"
	}
	if err := m.Start(bad); err == nil {
		t.Fatal("9-arg spec accepted")
	}
}

// Crash loop: an on-failure spec restarts to MaxRestarts inside the
// window, reaches failed, and a subsequent Start is refused by name.
func TestCrashLoopGiveUp(t *testing.T) {
	f := newFake()
	m, _ := New(f.hooks())
	pol := supervise.Policy{Restart: supervise.OnFailure, BackoffBaseS: 1,
		BackoffCapS: 4, MaxRestarts: 5, Window: 1000}
	spec := Spec{Name: "LOOP", Args: []string{"L.ELF", "exit", "9"}, Policy: pol}
	if err := m.Start(spec); err != nil {
		t.Fatal(err)
	}
	var snap Snapshot
	for i := 0; i < 40; i++ {
		m.Tick()
		snap, _ = m.Snapshot("LOOP")
		if snap.State == supervise.Running {
			f.exit(snap.PID, 9)
		}
		f.now += 2_000_000_000 // 2 s per step: the 1 s backoff expires each loop
		if snap.State == supervise.Failed {
			break
		}
	}
	if snap.State != supervise.Failed {
		t.Fatalf("crash loop -> %+v, want failed", snap)
	}
	if snap.Starts != 6 || snap.Restart != 5 {
		t.Fatalf("starts=%d restart=%d, want 6/5", snap.Starts, snap.Restart)
	}
	if err := m.Start(spec); !errors.Is(err, ErrFailed) {
		t.Fatalf("post-give-up start err=%v, want ErrFailed", err)
	}
	// Every crash wrote a receipt; the last shows the spent budget.
	if len(f.receipts) != 6 || f.lastReceipt() != "LOOP|restart=5/5 status=9 backoff_s=0" {
		t.Fatalf("receipts %v", f.receipts)
	}
}

// The bounded capture: last-N lines, truncation flagged at the ring bound.
func TestLogTail(t *testing.T) {
	f := newFake()
	m, _ := New(f.hooks())
	f.logs["DEMO"] = []byte("one\ntwo\nthree\n")
	lines, trunc, err := m.LogTail("DEMO", 32)
	if err != nil || len(lines) != 3 || trunc {
		t.Fatalf("tail %v trunc=%v err=%v", lines, trunc, err)
	}
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "line-%02d\n", i)
	}
	f.logs["DEMO"] = []byte(b.String())
	lines, trunc, err = m.LogTail("DEMO", 32)
	if err != nil || len(lines) != 32 || !trunc {
		t.Fatalf("full ring -> %d lines trunc=%v err=%v", len(lines), trunc, err)
	}
	if lines[0] != "line-08" || lines[31] != "line-39" {
		t.Fatalf("tail window wrong: %q..%q", lines[0], lines[31])
	}
	lines, trunc, err = m.LogTail("GONE", 32)
	if err != nil || len(lines) != 0 || trunc {
		t.Fatalf("absent ring -> %v %v %v", lines, trunc, err)
	}
}

// Spawn rides gsport/proc: the exec bound refusal is named before the
// supervise layer ever sees a service.
func TestSpawnViaProcBounds(t *testing.T) {
	f := newFake()
	m, _ := New(f.hooks())
	long := Spec{Name: "LONG", Args: []string{"X.ELF", strings.Repeat("a", 256)}}
	if err := m.Start(long); err == nil {
		t.Fatal("256-byte arg accepted")
	}
}
