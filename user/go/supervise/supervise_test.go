package supervise

import (
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"
	"unsafe"

	"virelai/vi"
)

type fake struct {
	now       int64
	nextPID   int64
	execs     int
	rows      []Process
	receipts  []string
	lines     []string
	execErr   error
	tableErr  error
	killRC    int64
	receiptRC int64
	randomErr error
	kills     int
}

func (f *fake) hooks() Hooks {
	return Hooks{
		Now: func() int64 { return f.now },
		Exec: func(_ string, _ ...string) (int64, error) {
			f.execs++
			if f.execErr != nil {
				return 0, f.execErr
			}
			f.nextPID++
			f.rows = append(f.rows, Process{PID: f.nextPID, Running: true})
			return f.nextPID, nil
		},
		Table: func() ([]Process, error) { return f.rows, f.tableErr },
		Kill: func(_ uint64) int64 {
			f.kills++
			return f.killRC
		},
		Random: func(b []byte) (int, error) {
			binary.LittleEndian.PutUint64(b, 100) // zero jitter for base 2
			return len(b), f.randomErr
		},
		Receipt: func(label, outcome string) int64 {
			f.receipts = append(f.receipts, label+":"+outcome)
			return f.receiptRC
		},
		Serial: func(line string) { f.lines = append(f.lines, line) },
	}
}

func policy(restart string) Policy {
	if restart == Never {
		return Policy{Restart: Never}
	}
	return Policy{Restart: restart, BackoffBaseS: 2, BackoffCapS: 8, MaxRestarts: 5, Window: 1000}
}

func setup(t *testing.T, p Policy) (*Supervisor, *fake) {
	t.Helper()
	f := &fake{}
	s, err := New([]Service{{Name: "worker", Binary: "WORKER.ELF", Policy: p}}, f.hooks())
	if err != nil {
		t.Fatal(err)
	}
	return s, f
}

func tick(t *testing.T, s *Supervisor) {
	t.Helper()
	if err := s.Tick(); err != nil {
		t.Fatal(err)
	}
}

func view(t *testing.T, s *Supervisor) Snapshot {
	t.Helper()
	v, ok := s.Snapshot("worker")
	if !ok {
		t.Fatal("missing worker")
	}
	return v
}

func start(t *testing.T, s *Supervisor) {
	t.Helper()
	if err := s.Start("worker"); err != nil {
		t.Fatal(err)
	}
	tick(t, s)
	tick(t, s)
	if v := view(t, s); v.State != Running || !v.Observed {
		t.Fatalf("not observed running: %+v", v)
	}
}

func exit(t *testing.T, s *Supervisor, f *fake, status int64) {
	t.Helper()
	pid := view(t, s).PID
	found := false
	for i := range f.rows {
		if f.rows[i].PID == pid {
			f.rows[i].Exited, f.rows[i].Status = true, status
			found = true
		}
	}
	if !found {
		t.Fatal("pid not found")
	}
	tick(t, s)
}

func restart(t *testing.T, s *Supervisor, f *fake) {
	t.Helper()
	v := view(t, s)
	if v.State != Backoff {
		t.Fatalf("not backoff: %+v", v)
	}
	f.now = v.NextNS - 1
	tick(t, s)
	if got := view(t, s); got.State != Backoff {
		t.Fatalf("spawned early: %+v", got)
	}
	f.now++
	tick(t, s)
	tick(t, s)
}

func TestPolicyMatrix(t *testing.T) {
	for _, name := range []string{Never, Always, OnFailure} {
		for _, status := range []int64{0, 7, StatusKilled} {
			t.Run(name+"/status="+vi.Itoa64(status), func(t *testing.T) {
				s, f := setup(t, policy(name))
				start(t, s)
				exit(t, s, f, status)
				wantRetry := name == Always || (name == OnFailure && status != 0)
				wantState := Exited
				if wantRetry {
					wantState = Backoff
				}
				if got := view(t, s); got.State != wantState {
					t.Fatalf("state %s want %s", got.State, wantState)
				}
				wantReceipts := 0
				if status != 0 {
					wantReceipts = 1
				}
				if len(f.receipts) != wantReceipts {
					t.Fatalf("receipts %v", f.receipts)
				}
				if wantRetry {
					restart(t, s, f)
				} else {
					f.now += 2000 * second
					tick(t, s)
					if f.execs != 1 {
						t.Fatal("terminal child restarted")
					}
				}
			})
		}
		t.Run(name+"/never-started", func(t *testing.T) {
			s, f := setup(t, policy(name))
			for i := 0; i < 10; i++ {
				tick(t, s)
				f.now += second
			}
			if view(t, s).State != Stopped || f.execs != 0 || len(f.receipts) != 0 {
				t.Fatal("never-started child was launched or failed")
			}
		})
		t.Run(name+"/spawn-refused", func(t *testing.T) {
			s, f := setup(t, policy(name))
			f.execErr = errors.New("no capacity")
			_ = s.Start("worker")
			tick(t, s)
			want := Exited
			if name != Never {
				want = Backoff
			}
			if view(t, s).State != want || len(f.receipts) != 1 {
				t.Fatal(view(t, s), f.receipts)
			}
		})
	}
}

func TestSupervisorStopIsNotFailure(t *testing.T) {
	for _, name := range []string{Never, Always, OnFailure} {
		t.Run(name, func(t *testing.T) {
			s, f := setup(t, policy(name))
			start(t, s)
			if err := s.Stop("worker"); err != nil {
				t.Fatal(err)
			}
			if view(t, s).State != Stopping {
				t.Fatal("stop claimed exit before observation")
			}
			exit(t, s, f, StatusKilled)
			f.now += 10000 * second
			tick(t, s)
			if view(t, s).State != Stopped || len(f.receipts) != 0 || f.execs != 1 || f.kills != 1 {
				t.Fatal(view(t, s), f.receipts, f.execs, f.kills)
			}
		})
	}
	s, f := setup(t, policy(Always))
	start(t, s)
	f.killRC = -vi.ErrEACCES
	if s.Stop("worker") == nil || view(t, s).State != Running {
		t.Fatal("refused kill suppressed a failure")
	}
	exit(t, s, f, StatusKilled)
	if len(f.receipts) != 1 || view(t, s).State != Backoff {
		t.Fatal("external kill not counted")
	}
}

func TestBackoffScheduleAndGiveUp(t *testing.T) {
	s, f := setup(t, policy(OnFailure))
	start(t, s)
	for k, delay := range []uint64{2, 4, 8, 8, 8} {
		exit(t, s, f, StatusKilled)
		v := view(t, s)
		if v.Restart != uint32(k+1) || v.DelayS != delay {
			t.Fatalf("restart %d: %+v", k+1, v)
		}
		restart(t, s, f)
	}
	exit(t, s, f, StatusKilled)
	if v := view(t, s); v.State != Failed || v.Restart != 5 || v.DelayS != 0 {
		t.Fatal(v)
	}
	if f.execs != 6 || len(f.receipts) != 6 ||
		f.receipts[5] != "worker:restart=5/5 status=137 backoff_s=0" {
		t.Fatal(f.execs, f.receipts)
	}
	f.now += 2000 * second
	tick(t, s)
	if s.Start("worker") == nil || f.execs != 6 {
		t.Fatal("failed service resurrected")
	}
	if strings.Count(strings.Join(f.lines, "\n"), "svc: failed name=worker") != 1 {
		t.Fatal(f.lines)
	}
}

func TestRestartDeadlineIncludesReceiptAndSerialWork(t *testing.T) {
	s, f := setup(t, policy(OnFailure))
	receipt := s.hooks.Receipt
	s.hooks.Receipt = func(label, outcome string) int64 {
		rc := receipt(label, outcome)
		f.now += second
		return rc
	}
	serial := s.hooks.Serial
	s.hooks.Serial = func(line string) {
		serial(line)
		f.now += second / 10
	}
	start(t, s)
	exit(t, s, f, StatusKilled)
	handled := f.now
	if v := view(t, s); v.DelayS != 2 || v.NextNS != handled+2*second {
		t.Fatal("receipt/serial work shortened backoff", v, handled)
	}
	f.now = handled + 2*second - 1
	tick(t, s)
	if view(t, s).State != Backoff || f.execs != 1 {
		t.Fatal("respawn before measured delay")
	}
	f.now++
	tick(t, s)
	if view(t, s).State != Running || f.execs != 2 {
		t.Fatal("did not respawn at deadline")
	}
}

func TestStableRunResetsStreakAndRollingBudgetExpires(t *testing.T) {
	p := policy(OnFailure)
	p.Window = 10
	p.MaxRestarts = 1
	s, f := setup(t, p)
	start(t, s)
	exit(t, s, f, 7)
	restart(t, s, f)
	f.now += 10 * second
	tick(t, s) // actually observed alive for the stable window
	exit(t, s, f, 7)
	if v := view(t, s); v.State != Backoff || v.DelayS != 2 {
		t.Fatal("stability did not reset backoff", v)
	}
	restart(t, s, f)
	exit(t, s, f, 7)
	if view(t, s).State != Failed {
		t.Fatal("current-window retry did not consume the budget")
	}
}

func TestFailedRetrySpawnsConsumeBudget(t *testing.T) {
	p := policy(OnFailure)
	p.MaxRestarts = 2
	s, f := setup(t, p)
	f.execErr = errors.New("missing ELF")
	_ = s.Start("worker")
	tick(t, s)
	for i := 0; i < 2; i++ {
		f.now = view(t, s).NextNS
		tick(t, s)
	}
	if view(t, s).State != Failed || f.execs != 3 || len(f.receipts) != 3 {
		t.Fatal(view(t, s), f.execs, f.receipts)
	}
}

func TestTransientTableErrorAndGonePID(t *testing.T) {
	s, f := setup(t, policy(OnFailure))
	start(t, s)
	before := view(t, s)
	f.tableErr = errors.New("transient")
	if s.Tick() == nil || view(t, s) != before {
		t.Fatal("table error changed state")
	}
	f.tableErr, f.rows = nil, nil
	tick(t, s)
	if view(t, s).Status != StatusUnknown || len(f.receipts) != 1 || view(t, s).State != Backoff {
		t.Fatal("missing pid was treated as clean", view(t, s), f.receipts)
	}
}

func TestStopPendingStartAndBackoff(t *testing.T) {
	s, f := setup(t, policy(Always))
	_ = s.Start("worker")
	_ = s.Stop("worker")
	tick(t, s)
	if f.execs != 0 || f.kills != 0 {
		t.Fatal("pending start not cancelled")
	}
	start(t, s)
	exit(t, s, f, 7)
	_ = s.Stop("worker")
	f.now += 100 * second
	tick(t, s)
	if view(t, s).State != Stopped || f.execs != 1 || f.kills != 0 {
		t.Fatal("backoff not cancelled")
	}
}

func TestReloadPreservesRollingBudget(t *testing.T) {
	p := policy(OnFailure)
	p.MaxRestarts = 1
	s, f := setup(t, p)
	start(t, s)
	exit(t, s, f, 7)
	restart(t, s, f)
	_ = s.Stop("worker")
	exit(t, s, f, StatusKilled)
	start(t, s)
	if view(t, s).Restart != 1 {
		t.Fatal("stop/start cleared the receipt counter")
	}
	exit(t, s, f, 7)
	if view(t, s).State != Failed {
		t.Fatal("stop/start cleared the retry window")
	}
}

func TestReservedPIDIsNotReady(t *testing.T) {
	s, f := setup(t, policy(OnFailure))
	start(t, s)
	f.rows[0].Running = false
	tick(t, s)
	if view(t, s).State != Running || view(t, s).Observed {
		t.Fatal("reserved row counted as ready or dead")
	}
}

func TestStableWindowRequiresLiveObservation(t *testing.T) {
	p := policy(OnFailure)
	p.Window = 10
	s, f := setup(t, p)
	start(t, s)
	exit(t, s, f, 7)
	restart(t, s, f)
	f.now += 10 * second
	exit(t, s, f, 7) // no live observation at the stable boundary
	if view(t, s).DelayS != 4 {
		t.Fatal("exit observation invented a stable run")
	}
}

func TestEntropyAndReceiptErrorsFailClosed(t *testing.T) {
	for _, reason := range []string{"entropy", "receipt"} {
		t.Run(reason, func(t *testing.T) {
			s, f := setup(t, policy(OnFailure))
			start(t, s)
			if reason == "entropy" {
				f.randomErr = errors.New("unavailable")
			} else {
				f.receiptRC = -vi.ErrENOSPC
			}
			exit(t, s, f, 7)
			if view(t, s).State != Failed || len(f.receipts) != 1 ||
				!strings.Contains(f.lines[len(f.lines)-1], "reason="+reason) {
				t.Fatal(view(t, s), f.receipts, f.lines)
			}
		})
	}
}

func TestFlatServicesAndImmutableArgv(t *testing.T) {
	f := &fake{}
	args := []string{"original"}
	h := f.hooks()
	var got string
	exec := h.Exec
	h.Exec = func(binary string, argv ...string) (int64, error) {
		if binary == "A.ELF" {
			got = argv[0]
		}
		return exec(binary, argv...)
	}
	s, err := New([]Service{
		{Name: "a", Binary: "A.ELF", Args: args, Policy: policy(Never)},
		{Name: "b", Binary: "B.ELF", Policy: policy(Always)},
	}, h)
	if err != nil {
		t.Fatal(err)
	}
	args[0] = "changed"
	_ = s.Start("a")
	_ = s.Start("b")
	tick(t, s)
	a, _ := s.Snapshot("a")
	b, _ := s.Snapshot("b")
	if got != "original" || a.PID == b.PID || f.execs != 2 {
		t.Fatal(got, a, b)
	}
}

func TestValidation(t *testing.T) {
	for _, p := range []Policy{
		{}, {Restart: "sometimes"}, {Restart: Never, Window: 1},
		{Restart: Always, BackoffBaseS: 0, BackoffCapS: 1, MaxRestarts: 1, Window: 1},
		{Restart: Always, BackoffBaseS: 3, BackoffCapS: 2, MaxRestarts: 1, Window: 2},
		{Restart: Always, BackoffBaseS: 1, BackoffCapS: 3601, MaxRestarts: 1, Window: 4000},
		{Restart: Always, BackoffBaseS: 1, BackoffCapS: 1, MaxRestarts: 33, Window: 1},
		{Restart: Always, BackoffBaseS: 1, BackoffCapS: 2, MaxRestarts: 1, Window: 1},
		{Restart: Always, BackoffBaseS: 1, BackoffCapS: 1, MaxRestarts: 1, Window: 86401},
	} {
		if p.Validate() == nil {
			t.Fatal("accepted invalid policy", p)
		}
	}
	f := &fake{}
	for _, name := range []string{"", ".", "..", "bad/name", "space name", strings.Repeat("a", 29)} {
		if _, err := New([]Service{{Name: name, Binary: "A.ELF", Policy: policy(Never)}}, f.hooks()); err == nil {
			t.Fatal("accepted invalid label", name)
		}
	}
	service := Service{Name: "ok", Binary: "A.ELF", Policy: policy(Never)}
	if _, err := New([]Service{service, service}, f.hooks()); err == nil {
		t.Fatal("accepted duplicate")
	}
	service.Args = make([]string, vi.ExecMaxArgs+1)
	if _, err := New([]Service{service}, f.hooks()); err == nil {
		t.Fatal("accepted oversized argv")
	}
	service.Args = []string{strings.Repeat("x", vi.ExecArgMax+1)}
	if _, err := New([]Service{service}, f.hooks()); err == nil {
		t.Fatal("accepted long arg")
	}
	if _, err := New(nil, Hooks{}); err == nil {
		t.Fatal("accepted missing hooks")
	}
	s, _ := setup(t, policy(Never))
	if s.Start("missing") == nil || s.Stop("missing") == nil {
		t.Fatal("unknown name accepted")
	}
}

func TestJitterBoundsDistributionAndSaturation(t *testing.T) {
	p := policy(OnFailure)
	p.BackoffBaseS, p.BackoffCapS = 7, 100
	seen := map[uint64]bool{}
	var draw uint64
	random := func(b []byte) (int, error) {
		draw++
		binary.LittleEndian.PutUint64(b, draw+1000)
		return len(b), nil
	}
	for k, base := range []uint64{7, 14, 28, 56, 100, 100} {
		for i := 0; i < 100; i++ {
			d, err := Delay(p, uint32(k), random)
			if err != nil || d < base || d >= base+7 {
				t.Fatalf("k=%d delay=%d err=%v", k, d, err)
			}
			seen[d-base] = true
		}
	}
	if len(seen) != 7 {
		t.Fatal("jitter is constant or missing buckets", seen)
	}
	if d, err := Delay(p, ^uint32(0), random); err != nil || d < 100 || d >= 107 {
		t.Fatal("saturation failed", d, err)
	}
	tries := 0
	if _, err := Delay(p, 0, func(b []byte) (int, error) {
		tries++
		return len(b), nil // zero is in this bound's rejection interval
	}); err == nil || tries != 8 {
		t.Fatal("bad entropy source not bounded", err, tries)
	}
	if _, err := Delay(p, 0, func(b []byte) (int, error) { return 1, nil }); err == nil {
		t.Fatal("short entropy accepted")
	}
}

// The syscall hook exercises the real, unchanged vi.WriteCrashReceipt. It
// captures the safely-published temp's bytes, not a supervisor-owned formatter.
//
//go:nocheckptr
func hookBytes(address, size uintptr) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(address)), int(size))
}

func TestPinnedReceiptThroughGuestHook(t *testing.T) {
	var written []byte
	var renamed bool
	old := vi.SetSyscallHookForTest(func(slot uintptr, a0, a1, a2, a3 uintptr) int64 {
		switch slot {
		case vi.SlotFileOpen:
			if uint32(a2)&vi.ModeRead != 0 {
				return vi.ErrFileNotFound // no app log
			}
			if uint32(a2)&vi.ModeDir == 0 {
				written = nil
				renamed = false
			}
			return 1
		case vi.SlotFileWrite:
			written = append(written, hookBytes(a1, a2)...)
			return int64(a2)
		case vi.SlotFileDelete:
			return vi.ErrFileNotFound
		case vi.SlotFileRename:
			path := string(hookBytes(a2, a3))
			renamed = path == "/host/CRASH/worker.TXT"
			return 0
		case vi.SlotFileSync, vi.SlotFileClose:
			return 0
		default:
			t.Fatalf("unexpected syscall %d", slot)
			return -1
		}
	})
	defer vi.SetSyscallHookForTest(old)
	s, f := setup(t, policy(OnFailure))
	s.hooks.Receipt = GuestHooks().Receipt
	start(t, s)
	for i := 0; i < 5; i++ {
		exit(t, s, f, StatusKilled)
		restart(t, s, f)
	}
	exit(t, s, f, StatusKilled)
	want, err := os.ReadFile("crash-receipt.golden")
	if err != nil {
		t.Fatal(err)
	}
	if !renamed || string(written) != string(want) {
		t.Fatalf("receipt %q want %q (published=%v)", written, want, renamed)
	}
}
