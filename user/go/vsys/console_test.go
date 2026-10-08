package vsys

import (
	"runtime"
	"strings"
	"testing"
)

func TestPrintKeepsStagingUntilWriteReturns(t *testing.T) {
	prev := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prev)

	const marker = "gonet: read failed closed err=vsys: kernel error 12\n"
	const heartbeat = "gonet: hb=3\n"
	started := make(chan struct{})
	done := make(chan struct{})
	var first, second string
	calls := 0
	fakeKern(t, func(num, fd, ptr, n, unused uintptr) int64 {
		calls++
		if calls == 1 {
			// Print has copied its bytes, but the kernel has not read them.
			// Let the heartbeat enter Print on the only available P. A
			// contending writer must yield back rather than spin here.
			go func() {
				close(started)
				Print(heartbeat)
				close(done)
			}()
			<-started
			runtime.Gosched()
			first = string(virConsoleStaging[:n])
		} else {
			second = string(virConsoleStaging[:n])
		}
		return int64(n)
	})

	Print(marker)
	<-done
	if first != marker {
		t.Errorf("first write corrupted: got %q, want %q", first, marker)
	}
	if second != heartbeat {
		t.Errorf("second write = %q, want %q", second, heartbeat)
	}
	if calls != 2 {
		t.Errorf("write calls = %d, want 2", calls)
	}
}

func TestPrintTruncationAndWriteError(t *testing.T) {
	var writes []string
	var slots, descriptors []uintptr
	fakeKern(t, func(num, fd, ptr, n, unused uintptr) int64 {
		slots = append(slots, num)
		descriptors = append(descriptors, fd)
		writes = append(writes, string(virConsoleStaging[:n]))
		return -ErrEFAULT
	})

	Print(strings.Repeat("x", 300))
	Println("after error")
	Print("")
	want := []string{strings.Repeat("x", 256), "after error\n", ""}
	if len(writes) != len(want) {
		t.Fatalf("write count = %d, want %d", len(writes), len(want))
	}
	for i := range want {
		if writes[i] != want[i] || slots[i] != SlotWrite || descriptors[i] != 1 {
			t.Errorf("write %d = (%d, %d, %q), want (%d, 1, %q)",
				i, slots[i], descriptors[i], writes[i], SlotWrite, want[i])
		}
	}
}
