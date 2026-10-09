package proc

import (
	"strings"
	"testing"
	"unsafe"

	"virelai/gsport/abi"
	"virelai/vi"
)

// fakeKernel records slot numbers and answers sys_procs (7) with a row for
// the requested pid when exited is set, mirroring the kernel's 40-byte
// snapshot row (u64 pid, u64 state, u64 exit_status, name[16]).
type fakeKernel struct {
	slots  []int
	pid    uint64
	state  uint64
	status uint64
	rows   int // rows to report for slot 7
}

func (k *fakeKernel) hook(num uintptr, a0, a1, a2, a3 uintptr) int64 {
	k.slots = append(k.slots, int(num))
	switch num {
	case vi.SlotExec:
		return int64(k.pid)
	case vi.SlotKill:
		return 0
	case vi.SlotSleep:
		return 0
	case vi.SlotProcs:
		if k.rows == 0 {
			return 0
		}
		row := unsafe.Slice((*byte)(unsafe.Pointer(a0)), vi.ProcRowSize)
		putU64(row[0:], k.pid)
		putU64(row[8:], k.state)
		putU64(row[16:], k.status)
		return int64(k.rows)
	}
	return 0
}

func putU64(b []byte, v uint64) {
	for i := 0; i < 8; i++ {
		b[i] = byte(v >> (8 * i))
	}
}

func (k *fakeKernel) install(t *testing.T) {
	t.Helper()
	prev := vi.SetSyscallHookForTest(k.hook)
	t.Cleanup(func() { vi.SetSyscallHookForTest(prev) })
}

// TestSpawnSlot: Spawn must reach the kernel through abi.Exec — the
// slot-28 entry. vi.Exec itself calls syscall4 directly (it predates the
// svcN hook seam), so the host proof swaps the abi table entry for a
// recorder — that is what the injectable table is for. Revert to
// os/exec.Command.Run never touches abi.Exec → red, and the guard refuses
// the os/exec import besides.
func TestSpawnSlot(t *testing.T) {
	var got []string
	prev := abi.Exec
	abi.Exec = func(name string, args ...string) (int64, error) {
		got = append(got, name)
		return 42, nil
	}
	defer func() { abi.Exec = prev }()

	pid, err := Spawn("HELLO.ELF", "arg1")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if pid != 42 {
		t.Fatalf("pid %d, want 42", pid)
	}
	if len(got) != 1 || got[0] != "HELLO.ELF" {
		t.Fatalf("abi.Exec calls %v, want [HELLO.ELF]", got)
	}
}

// TestSpawnBoundsRefused: argv past the kernel block fails before abi.Exec
// is touched.
func TestSpawnBoundsRefused(t *testing.T) {
	called := 0
	prev := abi.Exec
	abi.Exec = func(name string, args ...string) (int64, error) {
		called++
		return 1, nil
	}
	defer func() { abi.Exec = prev }()

	if _, err := Spawn(strings.Repeat("n", abi.ExecArgMax+1)); err == nil {
		t.Fatal("over-long name accepted")
	}
	args := make([]string, abi.ExecMaxArgs+1)
	for i := range args {
		args[i] = "x"
	}
	if _, err := Spawn("HELLO.ELF", args...); err == nil {
		t.Fatal("9 args accepted — kernel bound is 8")
	}
	if _, err := Spawn("HELLO.ELF", strings.Repeat("a", abi.ExecArgMax+1)); err == nil {
		t.Fatal("over-long arg accepted")
	}
	if called != 0 {
		t.Fatalf("refused spawns reached abi.Exec %d times", called)
	}
}

// TestProbeStatusMapping: slot 7's row states map to explicit Status —
// a revert that returns raw kernel codes or os.ProcessState fails.
func TestProbeStatusMapping(t *testing.T) {
	k := &fakeKernel{pid: 9, state: vi.ProcRunning, rows: 1}
	k.install(t)
	st, _ := Probe(9)
	if st != StatusRunning {
		t.Fatalf("running row -> %v, want StatusRunning", st)
	}
	k.state = vi.ProcExited
	k.status = 137
	st, code := Probe(9)
	if st != StatusExited || code != 137 {
		t.Fatalf("exited row -> (%v,%d), want (StatusExited,137)", st, code)
	}
	k.rows = 0
	st, _ = Probe(9)
	if st != StatusAbsent {
		t.Fatalf("absent pid -> %v, want StatusAbsent", st)
	}
	if got, _ := Probe(-1); got != StatusAbsent {
		t.Fatalf("pid<=0 -> %v, want StatusAbsent", got)
	}
}

// TestWaitAndKillSlots: Wait polls the registry (7), Kill is 29.
func TestWaitAndKillSlots(t *testing.T) {
	k := &fakeKernel{pid: 5, state: vi.ProcExited, status: 42, rows: 1}
	k.install(t)
	st, err := Wait(5)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if st != 42 {
		t.Fatalf("status %d, want 42", st)
	}
	if err := Kill(5); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	want := []int{7, 29}
	if len(k.slots) != len(want) {
		t.Fatalf("slots %v, want %v", k.slots, want)
	}
	for i := range want {
		if k.slots[i] != want[i] {
			t.Fatalf("slots %v, want %v", k.slots, want)
		}
	}
}

func TestKillBadPid(t *testing.T) {
	k := &fakeKernel{}
	k.install(t)
	if err := Kill(0); err == nil {
		t.Fatal("Kill(0) accepted")
	}
	if len(k.slots) != 0 {
		t.Fatalf("refused kill burned slots %v", k.slots)
	}
}
