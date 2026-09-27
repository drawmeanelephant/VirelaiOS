// M81d (#1764): the GOFILES half of the advisory write lease. The act ops
// refuse a path another writer holds live; these tests drive that through
// the hook seam with a minimal fake kernel (the file rows the lease check
// reads, plus the console rows the host stub would otherwise answer).
package main

import (
	"testing"
	"unsafe"

	"virelai/vi"
)

func leaseHookStr(a0, a1 uintptr) string {
	if a0 == 0 || a1 == 0 {
		return ""
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(a0)), a1))
}

// installLeaseStub seeds one live lease record and serves just enough of
// the file ABI for vi.CheckFileLease to find it (read returns the record
// once, then EOF — the cursor the real share keeps per handle).
func installLeaseStub(t *testing.T, now int64, records map[string][]byte) {
	t.Helper()
	readDone := false
	prev := vi.SetSyscallHookForTest(func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		switch num {
		case vi.SlotTime:
			return now
		case vi.SlotFileOpen:
			path := leaseHookStr(a0, a1)
			if _, ok := records[path]; !ok {
				return -vi.ErrENOENT
			}
			readDone = false
			return 1
		case vi.SlotFileRead:
			if readDone {
				return 0
			}
			readDone = true
			path := ""
			for p := range records {
				path = p // handle 1 is the only open; one record per test
				break
			}
			buf := unsafe.Slice((*byte)(unsafe.Pointer(a1)), a2)
			n := copy(buf, records[path])
			return int64(n)
		case vi.SlotFileClose:
			return 0
		}
		t.Fatalf("leaseStub: unexpected slot %d", num)
		return 0
	})
	t.Cleanup(func() { vi.SetSyscallHookForTest(prev) })
}

func TestLeaseBlocksRefusesALiveLease(t *testing.T) {
	const target = "/host/FM/SEED.TXT"
	live := "VLEASE1\npid=0\nts=1000\ntoken=0102030405060708\npath=" + target + "\n"
	installLeaseStub(t, 1000, map[string][]byte{vi.LeasePathFor(target): []byte(live)})

	if !leaseBlocks(target) {
		t.Fatal("a live lease must block the act op")
	}
	if _, rc := deleteEntry("/host/FM", "SEED.TXT"); rc != -vi.ErrEAGAIN {
		t.Fatalf("deleteEntry under a live lease = %d, want -ErrEAGAIN", rc)
	}
	if rc := renameEntry("/host/FM", "SEED.TXT", "OTHER.TXT"); rc != -vi.ErrEAGAIN {
		t.Fatalf("renameEntry under a live lease = %d, want -ErrEAGAIN", rc)
	}
	if refusalStatus("delete", -vi.ErrEAGAIN) != "delete refused: lease held" {
		t.Fatalf("refusal status = %q", refusalStatus("delete", -vi.ErrEAGAIN))
	}
}

func TestLeaseFreePathAllowsTheAct(t *testing.T) {
	const target = "/host/FM/SEED.TXT"
	// No record at all: the ordinary one-syscall check, and the act runs.
	installLeaseStub(t, 1000, map[string][]byte{})
	if leaseBlocks(target) {
		t.Fatal("an unleasded path must not block")
	}
	// A stale record is free by the takeover rule, not a refusal.
	stale := "VLEASE1\npid=31\nts=900\ntoken=0102030405060708\npath=" + target + "\n"
	installLeaseStub(t, 1000, map[string][]byte{vi.LeasePathFor(target): []byte(stale)})
	if leaseBlocks(target) {
		t.Fatal("a stale lease must not block")
	}
}
