// Host tests for the M81d advisory write lease. The fake kernel below
// models the OBSERVED share semantics the lease stands on — including the
// one that makes the rewrite's truncate load-bearing: an HF create-open
// does NOT truncate an existing file (VFWire serveOpen opens for updating
// at the old size), so a shorter record must truncate to 0 explicitly or
// every later parse reads the old record's tail as corruption.
package vi

import (
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

// leaseKernel is an in-memory kernel for the file/procs/time/random rows
// the lease helpers drive. Dirs are tracked separately so the MODE_DIR
// create's EEXIST row lands honestly.
type leaseKernel struct {
	files   map[string][]byte
	dirs    map[string]bool
	handles map[uint32]*leaseHandle
	nextFd  uint32
	now     int64
	rows    []ProcRow
	rand    byte
}

type leaseHandle struct {
	path   string
	cursor int
	read   bool
}

func newLeaseKernel(now int64) *leaseKernel {
	return &leaseKernel{
		files:   map[string][]byte{},
		dirs:    map[string]bool{},
		handles: map[uint32]*leaseHandle{},
		nextFd:  1,
		now:     now,
	}
}

// withProc adds one RUNNING row (name, pid) to the registry.
func (k *leaseKernel) withProc(pid uint64, name string) *leaseKernel {
	var r ProcRow
	r.PID, r.State = pid, ProcRunning
	copy(r.NameBuf[:], name)
	k.rows = append(k.rows, r)
	return k
}

func (k *leaseKernel) seed(path string, b []byte) { k.files[path] = append([]byte(nil), b...) }

func (k *leaseKernel) hook(t *testing.T) func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
	t.Helper()
	return func(num uintptr, a0, a1, a2, a3 uintptr) int64 {
		switch num {
		case SlotTime:
			return k.now
		case SlotGetRandom:
			buf := unsafe.Slice((*byte)(unsafe.Pointer(a0)), a1)
			for i := range buf {
				k.rand++
				buf[i] = k.rand
			}
			return int64(a1)
		case SlotProcs:
			buf := unsafe.Slice((*byte)(unsafe.Pointer(a0)), a1)
			n := len(buf) / ProcRowSize
			if n > len(k.rows) {
				n = len(k.rows)
			}
			for i := 0; i < n; i++ {
				off := i * ProcRowSize
				putU64(buf[off:], k.rows[i].PID)
				putU64(buf[off+8:], k.rows[i].State)
				putU64(buf[off+16:], k.rows[i].ExitStatus)
				copy(buf[off+24:off+24+ProcNameBytes], k.rows[i].NameBuf[:])
			}
			return int64(n)
		case SlotFileOpen:
			path := hookStr(a0, a1)
			flags := uint32(a2)
			if flags&ModeDir != 0 {
				if k.dirs[path] {
					return -9 // the file-domain EEXIST row
				}
				k.dirs[path] = true
				return 9
			}
			if k.nextFd > 8 {
				return -ErrENOSPC
			}
			if _, ok := k.files[path]; !ok {
				if flags&ModeCreate == 0 {
					return -ErrENOENT
				}
				k.files[path] = nil // created EMPTY: no truncate-on-create
			}
			fd := k.nextFd
			k.nextFd++
			k.handles[fd] = &leaseHandle{path: path, read: flags&ModeWrite == 0}
			return int64(fd)
		case SlotFileRead:
			h := k.handles[uint32(a0)]
			if h == nil || !h.read {
				return -ErrEBADF
			}
			buf := unsafe.Slice((*byte)(unsafe.Pointer(a1)), a2)
			body := k.files[h.path]
			n := copy(buf, body[h.cursor:])
			h.cursor += n
			return int64(n)
		case SlotFileWrite:
			h := k.handles[uint32(a0)]
			if h == nil || h.read {
				return -ErrEBADF
			}
			src := unsafe.Slice((*byte)(unsafe.Pointer(a1)), a2)
			body := k.files[h.path]
			if h.cursor+len(src) > len(body) {
				grown := make([]byte, h.cursor+len(src))
				copy(grown, body)
				body = grown
			}
			copy(body[h.cursor:], src)
			k.files[h.path] = body
			h.cursor += len(src)
			return int64(a2)
		case SlotFileTruncate:
			h := k.handles[uint32(a0)]
			if h == nil || h.read {
				return -ErrEBADF
			}
			size := int(a1)
			if size > len(k.files[h.path]) {
				k.files[h.path] = append(k.files[h.path], make([]byte, size-len(k.files[h.path]))...)
			} else {
				k.files[h.path] = k.files[h.path][:size]
			}
			if h.cursor > size {
				h.cursor = size
			}
			return 0
		case SlotFileClose:
			delete(k.handles, uint32(a0))
			return 0
		case SlotFileDelete:
			path := hookStr(a0, a1)
			if _, ok := k.files[path]; !ok {
				return -ErrENOENT
			}
			delete(k.files, path)
			return 0
		}
		t.Fatalf("leaseKernel: unexpected slot %d", num)
		return 0
	}
}

// withLeaseKernel installs the fake for one test (the installHook cleanup
// pattern) and returns it for seeding and assertions.
func withLeaseKernel(t *testing.T, now int64) *leaseKernel {
	t.Helper()
	k := newLeaseKernel(now)
	prev := SetSyscallHookForTest(k.hook(t))
	t.Cleanup(func() { SetSyscallHookForTest(prev) })
	return k
}

// forgeRecord hand-writes one VLEASE1 record the way a foreign writer
// would — the format is an on-share convention, not a private type.
func forgeRecord(pid uint32, ts int64, tokenHex, path string) []byte {
	var b strings.Builder
	b.WriteString(leaseHeader)
	b.WriteString("pid=" + strconv.FormatUint(uint64(pid), 10) + "\n")
	b.WriteString("ts=" + strconv.FormatInt(ts, 10) + "\n")
	b.WriteString("token=" + tokenHex + "\n")
	b.WriteString("path=" + path + "\n")
	return []byte(b.String())
}

func TestLeaseHashIsStableHex(t *testing.T) {
	a := LeasePathFor("/host/EDIT/SEED.TXT")
	b := LeasePathFor("/host/EDIT/OTHER.TXT")
	if len(a) != len(LeaseDir)+17 || !strings.HasPrefix(a, LeaseDir+"/") {
		t.Fatalf("LeasePathFor shape = %q", a)
	}
	if a == b {
		t.Fatal("distinct paths hashed to one record")
	}
	if LeasePathFor("/etc/passwd") != "" || LeasePathFor("") != "" {
		t.Fatal("non-share paths must not lease")
	}
	if again := LeasePathFor("/host/EDIT/SEED.TXT"); again != a {
		t.Fatal("hash not stable")
	}
}

func TestAcquireThenRefuseSecondWriter(t *testing.T) {
	k := withLeaseKernel(t, 1000).withProc(7, "GOEDIT.ELF").withProc(9, "GOSH.ELF")
	k.seed("/host/EDIT/SEED.TXT", []byte("seed-line\n"))

	l1, rc := AcquireFileLease("/host/EDIT/SEED.TXT", "GOEDIT.ELF")
	if rc != 0 || l1 == nil || l1.PID != 7 {
		t.Fatalf("first acquire = %d, want 0 with pid=7 (rc=%d)", rc, rc)
	}
	if _, ok := k.dirs[LeaseDir]; !ok {
		t.Fatal("LeaseDir not created")
	}
	// A different writer (fresh token) against a fresh stamp and a live
	// pid is refused with the one EAGAIN row the convention defines.
	if _, rc := AcquireFileLease("/host/EDIT/SEED.TXT", "GOFILES.ELF"); rc != -ErrEAGAIN {
		t.Fatalf("second acquire = %d, want -ErrEAGAIN", rc)
	}
	if rc := CheckFileLease("/host/EDIT/SEED.TXT"); rc != -ErrEAGAIN {
		t.Fatalf("check under live lease = %d, want -ErrEAGAIN", rc)
	}
}

func TestAcquireTakesOverExpiredAndDeadHolders(t *testing.T) {
	const target = "/host/EDIT/SEED.TXT"
	// Expired stamp, holder long gone: takeover at the first acquire.
	k := withLeaseKernel(t, 100000).withProc(7, "GOEDIT.ELF")
	k.seed(LeasePathFor(target), forgeRecord(31, 1000, "0102030405060708", target))
	l, rc := AcquireFileLease(target, "GOEDIT.ELF")
	if rc != 0 || l == nil {
		t.Fatalf("takeover of expired = %d, want 0", rc)
	}

	// Fresh stamp but a knowably-dead holder: the crash-recovery rule
	// recovers immediately instead of waiting out the expiry.
	k2 := withLeaseKernel(t, 1000).withProc(7, "GOEDIT.ELF")
	k2.seed(LeasePathFor(target), forgeRecord(31, 1000, "0102030405060708", target))
	if _, rc := AcquireFileLease(target, "GOEDIT.ELF"); rc != 0 {
		t.Fatalf("takeover of dead holder = %d, want 0", rc)
	}

	// The contrast: a live pid with a fresh stamp is refused.
	k3 := withLeaseKernel(t, 1000).withProc(7, "GOEDIT.ELF").withProc(31, "GOEDIT.ELF")
	k3.seed(LeasePathFor(target), forgeRecord(31, 1000, "0102030405060708", target))
	if _, rc := AcquireFileLease(target, "GOFILES.ELF"); rc != -ErrEAGAIN {
		t.Fatalf("live pid fresh stamp = %d, want -ErrEAGAIN", rc)
	}
}

func TestAcquireFailsClosedWithoutAClock(t *testing.T) {
	const target = "/host/EDIT/SEED.TXT"
	// No epoch: sys_time is -ENOSYS, the record carries ts=0, and pid
	// liveness is the only signal. Unknown pid (0) must REFUSE, not clobber.
	k := withLeaseKernel(t, -ErrENOSYS).withProc(7, "GOEDIT.ELF")
	k.seed(LeasePathFor(target), forgeRecord(0, 0, "0102030405060708", target))
	if _, rc := AcquireFileLease(target, "GOEDIT.ELF"); rc != -ErrEAGAIN {
		t.Fatalf("no-clock unknown pid = %d, want -ErrEAGAIN", rc)
	}
	// A dead pid is still recoverable without a clock.
	k2 := withLeaseKernel(t, -ErrENOSYS).withProc(7, "GOEDIT.ELF")
	k2.seed(LeasePathFor(target), forgeRecord(31, 0, "0102030405060708", target))
	if _, rc := AcquireFileLease(target, "GOEDIT.ELF"); rc != 0 {
		t.Fatalf("no-clock dead pid = %d, want 0", rc)
	}
}

func TestAcquireRejectsBadPaths(t *testing.T) {
	withLeaseKernel(t, 1000)
	for _, p := range []string{"", "/etc/passwd", "/host/", "/host/a/b/../c"} {
		if _, rc := AcquireFileLease(p, "X.ELF"); rc != -ErrEINVAL {
			t.Fatalf("acquire(%q) = %d, want -ErrEINVAL", p, rc)
		}
		if rc := CheckFileLease(p); rc != -ErrEINVAL {
			t.Fatalf("check(%q) = %d, want -ErrEINVAL", p, rc)
		}
	}
}

func TestLeaseRewriteTruncatesTheOldTail(t *testing.T) {
	const target = "/host/EDIT/SEED.TXT"
	k := withLeaseKernel(t, 100000).withProc(7, "GOEDIT.ELF")
	// A record with a garbage tail — the shape an unclean takeover would
	// leave if the rewrite did not truncate. Corrupt means free, and the
	// takeover's rewrite must leave a record that parses CLEANLY (the
	// fake's create-open does not truncate, so only the explicit
	// FileTruncate(0) can produce this).
	k.seed(LeasePathFor(target), append(forgeRecord(31, 1000, "0102030405060708", target),
		strings.Repeat("X", 40)...))
	l, rc := AcquireFileLease(target, "GOEDIT.ELF")
	if rc != 0 || l == nil {
		t.Fatalf("takeover of corrupt = %d, want 0", rc)
	}
	if _, ok := readLeaseRecord(LeasePathFor(target)); !ok {
		t.Fatal("rewritten record does not parse — the truncate did not happen")
	}
}

func TestRefreshBypassesExpiryForTheHolder(t *testing.T) {
	const target = "/host/EDIT/SEED.TXT"
	k := withLeaseKernel(t, 1000).withProc(7, "GOEDIT.ELF")
	l, rc := AcquireFileLease(target, "GOEDIT.ELF")
	if rc != 0 {
		t.Fatalf("acquire = %d", rc)
	}
	k.now += LeaseExpirySeconds + 5000 // a session far longer than the expiry
	if rc := l.Refresh(); rc != 0 {
		t.Fatalf("holder refresh after expiry = %d, want 0", rc)
	}
	rec, ok := readLeaseRecord(l.LeasePath)
	if !ok || rec.ts != k.now {
		t.Fatalf("refresh stamp = %d %v, want ts=%d", rec.ts, ok, k.now)
	}
}

func TestRefreshRefusesAfterATakeover(t *testing.T) {
	const target = "/host/EDIT/SEED.TXT"
	k := withLeaseKernel(t, 1000).withProc(7, "GOEDIT.ELF").withProc(9, "GOFILES.ELF")
	l1, rc := AcquireFileLease(target, "GOEDIT.ELF")
	if rc != 0 {
		t.Fatalf("acquire = %d", rc)
	}
	k.now += LeaseExpirySeconds * 3 // l1's stamp expires; B may take over
	l2, rc := AcquireFileLease(target, "GOFILES.ELF")
	if rc != 0 || l2 == nil {
		t.Fatalf("B takeover = %d, want 0", rc)
	}
	if rc := l1.Refresh(); rc != -ErrEAGAIN {
		t.Fatalf("A refresh under B = %d, want -ErrEAGAIN", rc)
	}
	if rc := l1.Release(); rc != -ErrEAGAIN {
		t.Fatalf("A release of B's lease = %d, want -ErrEAGAIN", rc)
	}
	if rc := l2.Release(); rc != 0 {
		t.Fatalf("B release = %d, want 0", rc)
	}
	if rc := CheckFileLease(target); rc != 0 {
		t.Fatalf("check after release = %d, want 0", rc)
	}
}

func TestReleaseRules(t *testing.T) {
	const target = "/host/EDIT/SEED.TXT"
	k := withLeaseKernel(t, 1000).withProc(7, "GOEDIT.ELF")
	l, _ := AcquireFileLease(target, "GOEDIT.ELF")
	if rc := l.Release(); rc != 0 {
		t.Fatalf("release = %d, want 0", rc)
	}
	if _, ok := k.files[l.LeasePath]; ok {
		t.Fatal("record survived its release")
	}
	if rc := l.Release(); rc != 0 {
		t.Fatalf("double release = %d, want 0 (nothing to release)", rc)
	}
}

func TestSelfPIDIsHonestUnderAmbiguity(t *testing.T) {
	const target = "/host/EDIT/SEED.TXT"
	// Two running rows with the same name: the scan cannot know which one
	// is us, and the lease records pid=0 rather than a guess.
	_ = withLeaseKernel(t, 1000).withProc(7, "GOEDIT.ELF").withProc(8, "GOEDIT.ELF")
	l, rc := AcquireFileLease(target, "GOEDIT.ELF")
	if rc != 0 || l == nil || l.PID != 0 {
		t.Fatalf("ambiguous acquire = %d, want 0 with pid=0", rc)
	}
	// A zeroed name row (the M56d flake) matches nothing.
	_ = withLeaseKernel(t, 1000).withProc(7, "")
	if l2, rc := AcquireFileLease(target, "GOEDIT.ELF"); rc != 0 || l2 == nil || l2.PID != 0 {
		t.Fatalf("zeroed-name acquire = %d, want 0 with pid=0", rc)
	}
	// One unambiguous row lands the pid in the record.
	_ = withLeaseKernel(t, 1000).withProc(12, "NOTE.ELF")
	l3, rc := AcquireFileLease(target, "NOTE.ELF")
	if rc != 0 || l3 == nil || l3.PID != 12 {
		t.Fatalf("unambiguous acquire = %d, want 0 with pid=12", rc)
	}
}

func TestParseLeaseRecordStrictness(t *testing.T) {
	good := forgeRecord(7, 123, "0102030405060708", "/host/A.TXT")
	if rec, ok := parseLeaseRecord(good); !ok || rec.pid != 7 || rec.ts != 123 || rec.path != "/host/A.TXT" {
		t.Fatalf("good record = %+v %v", rec, ok)
	}
	bad := map[string][]byte{
		"no header":      []byte("LEASE1\npid=7\nts=123\ntoken=0102030405060708\npath=/host/A.TXT\n"),
		"short":          []byte("VLEASE1\npid=7\n"),
		"no trailing nl": []byte("VLEASE1\npid=7\nts=123\ntoken=0102030405060708\npath=/host/A.TXT"),
		"bad token":      []byte("VLEASE1\npid=7\nts=123\ntoken=zz\npath=/host/A.TXT\n"),
		"bad ts":         []byte("VLEASE1\npid=7\nts=x\ntoken=0102030405060708\npath=/host/A.TXT\n"),
		"neg ts":         []byte("VLEASE1\npid=7\nts=-1\ntoken=0102030405060708\npath=/host/A.TXT\n"),
		"bad pid":        []byte("VLEASE1\npid=-7\nts=123\ntoken=0102030405060708\npath=/host/A.TXT\n"),
		"extra row":      []byte("VLEASE1\npid=7\nts=123\ntoken=0102030405060708\npath=/host/A.TXT\nextra=1\n"),
		"bad key":        []byte("VLEASE1\npid=7\nts=123\nnope=1\npath=/host/A.TXT\n"),
		"empty path":     []byte("VLEASE1\npid=7\nts=123\ntoken=0102030405060708\npath=\n"),
	}
	for name, b := range bad {
		if _, ok := parseLeaseRecord(b); ok {
			t.Fatalf("%s parsed as a valid record", name)
		}
	}
}

func TestTokenBypassesExpiryInClassify(t *testing.T) {
	rec, ok := parseLeaseRecord(forgeRecord(0, 10, "0102030405060708", "/host/A.TXT"))
	if !ok {
		t.Fatal("fixture record must parse")
	}
	var token [leaseTokenBytes]byte
	copy(token[:], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	if classify(rec, ok, &token, 10+LeaseExpirySeconds) != leaseMine {
		t.Fatal("the holder's own expired record is not live-against-itself")
	}
	if classify(rec, ok, nil, 10) != leaseLive {
		t.Fatal("a fresh foreign record must classify live")
	}
	if classify(rec, ok, nil, 10+LeaseExpirySeconds) != leaseFree {
		t.Fatal("an expired foreign record must classify free")
	}
}
