package vi

import (
	"hash/fnv"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestCrashStackSiblingAndBound(t *testing.T) {
	if got := CrashStackPath("GOSELF.ELF"); got != "/host/CRASH/GOSELF.ELF.STK" {
		t.Fatalf("sibling=%q", got)
	}
	for _, bad := range []string{"", ".", "..", "../APP", "a/b", strings.Repeat("a", 29)} {
		if CrashStackPath(bad) != "" {
			t.Fatalf("accepted %q", bad)
		}
	}
	receipt := []byte("app=APP.ELF\noutcome=panic: test\nlast-log:\n")
	hash := fnv.New64a()
	_, _ = hash.Write(receipt)
	for _, full := range []bool{false, true} {
		body := captureCrashStack(receipt, 123, func(buf []byte, all bool) int {
			if all {
				t.Fatal("captured unrelated goroutines")
			}
			if full {
				for i := range buf {
					buf[i] = 'x'
				}
				return len(buf)
			}
			return copy(buf, "goroutine 1 [running]:\nmain.crash()\n\tfixture.go:1 +0x4\n")
		})
		want := "VCRASH1 receipt=" + strconv.FormatUint(hash.Sum64(), 16) +
			" nanos=123 truncated=" + strconv.FormatBool(full) + "\n"
		if !strings.HasPrefix(string(body), want) || len(body) > CrashStackMaxBytes {
			t.Fatalf("header/bound len=%d prefix=%q", len(body), body[:len(want)])
		}
	}
	body := captureCrashStack(receipt, 124, runtime.Stack)
	if !strings.Contains(string(body), "vi.TestCrashStackSiblingAndBound") {
		t.Fatalf("real current-goroutine stack missing: %s", body)
	}
}

func TestCrashGuardNormalReturn(t *testing.T) {
	installHook(t, func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		t.Fatalf("normal guard made syscall %d", num)
		return 0
	})
	func() { defer CrashGuard("APP.ELF") }()
}

func TestCrashGuardFrozenReceiptSiblingAndExit(t *testing.T) {
	k := newLeaseKernel(123)
	k.dirs[CrashReceiptDir] = true
	k.seed(AppLogPath("APP.ELF"), []byte("before panic\n"))
	base := k.hook(t)
	exit := &struct{}{}
	status := -1
	installHook(t, func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		switch num {
		case SlotExit:
			status = int(a0)
			panic(exit) // the real syscall cannot return
		case SlotFileOpen:
			k.nextFd = 1
			for k.handles[k.nextFd] != nil {
				k.nextFd++
			}
		case SlotFileSync:
			return 0
		case SlotFileRename:
			from, to := hookStr(a0, a1), hookStr(a2, a3)
			if _, exists := k.files[to]; exists {
				return ErrFileExists
			}
			k.files[to] = k.files[from]
			delete(k.files, from)
			return 0
		}
		return base(num, a0, a1, a2, a3)
	})
	func() {
		defer func() {
			if got := recover(); got != exit {
				t.Fatalf("guard did not exit: %v", got)
			}
		}()
		func() {
			defer CrashGuard("APP.ELF")
			panic("fixture")
		}()
	}()
	receipt := k.files[CrashReceiptPath("APP.ELF")]
	want := "app=APP.ELF\noutcome=panic: fixture\nlast-log:\nbefore panic\n"
	if string(receipt) != want || status != 2 {
		t.Fatalf("receipt=%q status=%d", receipt, status)
	}
	stack := k.files[CrashStackPath("APP.ELF")]
	if len(stack) > CrashStackMaxBytes || !strings.Contains(string(stack), "vi.CrashGuard") ||
		!strings.Contains(string(stack), "vi.TestCrashGuardFrozenReceiptSiblingAndExit") {
		t.Fatalf("guard stack len=%d body=%s", len(stack), stack)
	}
	for name := range k.files {
		if strings.HasSuffix(name, "~") {
			t.Fatalf("unpublished temp %s", name)
		}
	}
}

func TestCrashGuardStillExitsWhenReceiptWriteFails(t *testing.T) {
	exit := &struct{}{}
	status := -1
	installHook(t, func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		switch num {
		case SlotExit:
			status = int(a0)
			panic(exit)
		case SlotWrite:
			return int64(a2)
		default:
			t.Fatalf("invalid app must not enter the file path: slot=%d", num)
			return 0
		}
	})
	func() {
		defer func() {
			if recover() != exit {
				t.Fatal("guard swallowed failure without exit")
			}
		}()
		func() {
			defer CrashGuard("../invalid")
			panic("fixture")
		}()
	}()
	if status != 2 {
		t.Fatalf("failed diagnostic write exit=%d", status)
	}
}

func TestLevelLogFormatAndPlainParse(t *testing.T) {
	raw, rc := levelLogRow(nil, 'W', "net", "connected\nagain\rnow")
	if rc != 0 || raw != "1 W net connected again now" {
		t.Fatalf("row=%q rc=%d", raw, rc)
	}
	line := ParseLogLine(raw)
	if !line.Sequenced || line.Seq != 1 || line.Level != 'W' ||
		line.Tag != "net" || line.Message != "connected again now" || line.Raw != raw {
		t.Fatalf("parsed=%+v", line)
	}
	for _, raw := range []string{"started", "panic: fixture", "", "0 I net bad",
		"01 I net bad", "18446744073709551616 I net bad", "1 X net bad", "1 I"} {
		line := ParseLogLine(raw)
		if line.Sequenced || line.Level != 'I' || line.Tag != "" || line.Message != raw {
			t.Fatalf("legacy %q parsed=%+v", raw, line)
		}
	}
	raw, rc = levelLogRow(nil, 'I', "", "empty tag")
	if rc != 0 || raw != "1 I  empty tag" || ParseLogLine(raw).Tag != "" ||
		!ParseLogLine(raw).Sequenced {
		t.Fatalf("empty tag row=%q rc=%d", raw, rc)
	}
}

func TestLevelLogSequenceAcrossTrimAndPlainRows(t *testing.T) {
	var ring []byte
	for i := 1; i <= AppLogMaxLines*3; i++ {
		raw, rc := levelLogRow(ring, 'D', "ui", "row")
		if rc != 0 || ParseLogLine(raw).Seq != uint64(i) {
			t.Fatalf("seq %d row=%q rc=%d", i, raw, rc)
		}
		ring = trimLogRing(append(ring, []byte(raw+"\n")...), AppLogMaxLines, appLogMaxBytes)
	}
	rows := strings.Split(strings.TrimSuffix(string(ring), "\n"), "\n")
	if len(rows) != AppLogMaxLines || ParseLogLine(rows[0]).Seq != 65 {
		t.Fatalf("ring=%q", ring)
	}
	ring = append(ring, []byte("legacy\n")...)
	raw, rc := levelLogRow(ring, 'E', "net", "after plain")
	if rc != 0 || ParseLogLine(raw).Seq != 97 {
		t.Fatalf("continued=%q rc=%d", raw, rc)
	}
}

func TestLevelLogCapAndValidation(t *testing.T) {
	for _, tag := range []string{"", strings.Repeat("t", 32)} {
		raw, rc := levelLogRow([]byte("999 I net previous\n"), 'E', tag, strings.Repeat("x", 400))
		if rc != 0 || len(raw) != AppLogMaxLine || ParseLogLine(raw).Seq != 1000 {
			t.Fatalf("len=%d row=%q rc=%d", len(raw), raw, rc)
		}
	}
	for _, tag := range []string{"has space", "new\nline", strings.Repeat("x", 33), "é"} {
		if validLogTag(tag) || LogLevel("APP", 'I', tag, "message") != -ErrEINVAL {
			t.Fatalf("accepted tag %q", tag)
		}
	}
	if LogLevel("APP", 'X', "net", "message") != -ErrEINVAL {
		t.Fatal("accepted invalid level")
	}
	if _, rc := levelLogRow([]byte(strconv.FormatUint(^uint64(0), 10)+" I net last\n"),
		'I', "", "wrap"); rc != -ErrEINVAL {
		t.Fatalf("sequence wrap rc=%d", rc)
	}
}

func TestPlainLogAndReceiptBytesUnchanged(t *testing.T) {
	if got := oneLogLine("a\nb\rc"); got != "a b c" {
		t.Fatalf("normalization=%q", got)
	}
	if got := oneLogLine(strings.Repeat("x", 400)); len(got) != 256 {
		t.Fatalf("plain cap=%d", len(got))
	}
	var ring []byte
	for i := 0; i < 36; i++ {
		ring = trimLogRing(append(ring, []byte("line-"+twoDigits(i)+"\n")...),
			AppLogMaxLines, appLogMaxBytes)
	}
	var want string
	for i := 28; i < 36; i++ {
		want += "line-" + twoDigits(i) + "\n"
	}
	if got := string(lastLogLines(ring, crashLogLines)); got != want {
		t.Fatalf("last-log=%q want=%q", got, want)
	}
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
