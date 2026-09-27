// The M81d (#1764) advisory write lease: the "two writers, one file"
// convention for /host. GOEDIT and GOFILES can both reach the same file and
// last writer wins silently; a lease makes "who owns this right now" a
// question with an answer. It is ADVISORY by contract (the card's non-goal
// names mandatory locking as ADR 0024 territory): the helpers refuse a
// second writer and hand back -ErrEAGAIN, and every cooperating caller
// treats that as "ask the user / back off", never as a kernel guarantee.
//
// The record lives at /host/LEASES/<fnv1a64(path) in 16 lowercase hex
// digits> — a hash-keyed name, not a path suffix, because the kernel's
// max_path_len is 64 (file_table.zig) and WriteFileSafe's own history is
// the proof that even a one-byte suffix cannot publish deep paths. The
// lookup is a direct open of the hashed name, so the 16-row DirList window
// never gates how many leases exist, and an orphan from a crash is simply
// a file another writer takes over by the rules below.
//
// The record is versioned text (the VTRASH1 convention):
//
//	VLEASE1
//	pid=<holder pid, 0 when unknown>
//	ts=<wall epoch seconds, 0 when the boot had no epoch>
//	token=<16 hex digits, the holder's 8-byte CSPRNG identity>
//	path=<the leased file, informational>
//
// pid is best-effort because there is NO self-pid syscall: the holder's pid
// is whatever an INTACT sys_procs scan matches for the caller's own binary
// name, and 0 when that scan is ambiguous (two running rows with the same
// name — exactly the two-editors case this package serves — or unreadable).
// The token is the identity that matters; it is what makes "is this lease
// mine" answerable without a getpid seam, and it never guesses.
//
// Live/stale rules (classify): a lease is LIVE while its stamp is fresh (or
// there is no usable clock) and its pid, when known, still runs; it is STALE
// — takeover allowed — when the stamp is LeaseExpirySeconds old, when its
// pid is knowably dead (a crashed holder is recovered immediately instead
// of at expiry), or when the record is absent or corrupt. A failed procs
// scan never proves death: a lease is not stolen on the strength of a scan
// that could not read the registry (the M56d flake). A no-epoch boot
// (sys_time is -ENOSYS, #1058) has no comparable stamps, so pid liveness is
// the only signal and an unknown pid FAILS CLOSED — refused, not clobbered.
package vi

import (
	"encoding/hex"
	"strconv"
	"strings"
)

// The lease's shared locations and bounds. LeaseDir is created on demand
// beside TrashDir/RecentDir (M81a) — the third shared root the vi layer owns.
const (
	// LeaseDir is where the hash-keyed lease records live.
	LeaseDir = "/host/LEASES"
	// LeaseExpirySeconds is how old another writer's stamp must be before
	// the lease may be taken over. The holder re-stamps at every save, and
	// its own token bypasses expiry, so this bounds only a SILENT holder:
	// short enough that a crashed editor does not lock a file for long,
	// long enough to span a user's think between edits.
	LeaseExpirySeconds int64 = 30
	// leaseRecordMax caps one record read; a real record is ~100 bytes.
	leaseRecordMax = 256
	// leaseTokenBytes is the CSPRNG holder identity's width.
	leaseTokenBytes = 8
	// leaseHeader is the record's version line (the VTRASH1 convention).
	leaseHeader = "VLEASE1\n"
)

// FileLease is one held advisory write lease. The zero value is not useful;
// AcquireFileLease returns a ready one. Target/LeasePath/Token/PID are
// readable so a receipt can say who holds what.
type FileLease struct {
	Target    string  // the leased file
	LeasePath string  // the hash-keyed record on the share
	Token     [8]byte // this holder's identity
	PID       uint32  // best-effort holder pid (0 = unknown)
}

// leaseState is what classify decided about one on-share record.
type leaseState uint8

const (
	leaseFree leaseState = iota // absent/corrupt/expired/dead holder: take over
	leaseMine                   // the record's token is ours
	leaseLive                   // someone else holds it live: refuse
)

// LeasePathFor returns the hash-keyed lease record path for path, or ""
// when path is not a share file path (the same rule every /host writer
// here obeys — validShareFilePath).
func LeasePathFor(path string) string {
	if !validShareFilePath(path) {
		return ""
	}
	return LeaseDir + "/" + leaseHash(path)
}

// leaseHash renders fnv1a64(path) as 16 lowercase hex digits, big-endian
// byte order. The gate fixtures re-implement this on the host, so the
// rendering is part of the on-share contract and must not drift.
func leaseHash(path string) string {
	const (
		offset64 = uint64(0xcbf29ce484222325)
		prime64  = uint64(0x100000001b3)
	)
	h := offset64
	for i := 0; i < len(path); i++ {
		h ^= uint64(path[i])
		h *= prime64
	}
	var b [8]byte
	for i := 0; i < 8; i++ {
		b[i] = byte(h >> (56 - 8*i))
	}
	return hex.EncodeToString(b[:])
}

// AcquireFileLease takes the advisory write lease for path on behalf of a
// process whose binary is selfName. Returns (lease, 0) when the lease is
// held — freshly acquired, or taken over because the standing record was
// stale by the rules in the package comment — and (nil, rc < 0) when it is
// refused: -ErrEAGAIN when another writer holds it live, or the kernel code
// of the step that failed (an invalid path is -ErrEINVAL).
func AcquireFileLease(path, selfName string) (*FileLease, int64) {
	lp := LeasePathFor(path)
	if lp == "" {
		return nil, -ErrEINVAL
	}
	if rc := ensureFileDir(LeaseDir); rc < 0 {
		return nil, rc
	}
	var token [8]byte
	if n, err := Random(token[:]); err != nil || n != len(token) {
		if err != nil {
			if ec, ok := err.(errno); ok {
				return nil, -int64(ec)
			}
			return nil, -ErrENOSYS
		}
		return nil, -ErrEAGAIN
	}
	pid := selfPID(selfName)
	now := Time()
	rec, ok := readLeaseRecord(lp)
	if classify(rec, ok, nil, now) == leaseLive {
		return nil, -ErrEAGAIN
	}
	if rc := writeLeaseRecord(lp, pid, now, token, path); rc < 0 {
		return nil, rc
	}
	return &FileLease{Target: path, LeasePath: lp, Token: token, PID: pid}, 0
}

// Refresh re-stamps the held lease (the long-session save path: the
// holder's own token bypasses expiry, so a session longer than
// LeaseExpirySeconds still renews instead of fighting itself). When the
// record was taken over meanwhile the refusal is -ErrEAGAIN; when it is
// gone, corrupt or expired the holder simply re-stakes its claim — the
// advisory convention is last acquirer wins, and a counter-record nobody
// can parse is not a claim.
func (l *FileLease) Refresh() int64 {
	if l == nil {
		return -ErrEINVAL
	}
	now := Time()
	rec, ok := readLeaseRecord(l.LeasePath)
	if state := classify(rec, ok, &l.Token, now); state == leaseLive {
		return -ErrEAGAIN
	}
	return writeLeaseRecord(l.LeasePath, l.PID, now, l.Token, l.Target)
}

// Release removes the lease — but only while the record still carries this
// holder's token: a lease taken over meanwhile belongs to the new holder,
// and deleting it would hand this process a silent second claim. A record
// that is gone or unreadable is already released (0).
func (l *FileLease) Release() int64 {
	if l == nil {
		return -ErrEINVAL
	}
	rec, ok := readLeaseRecord(l.LeasePath)
	if !ok {
		return 0
	}
	if rec.token != l.Token {
		return -ErrEAGAIN
	}
	return FileDelete(l.LeasePath)
}

// CheckFileLease reports whether path stands under a live lease another
// writer holds: 0 = free (no record, or stale by the classify rules), and
// -ErrEAGAIN = refused. It is the read-only half of the convention — the
// mutating op a cooperating app calls BEFORE it clobbers a path it does
// not hold a token for. An invalid path is -ErrEINVAL.
func CheckFileLease(path string) int64 {
	lp := LeasePathFor(path)
	if lp == "" {
		return -ErrEINVAL
	}
	rec, ok := readLeaseRecord(lp)
	if !ok {
		return 0
	}
	if classify(rec, ok, nil, Time()) == leaseLive {
		return -ErrEAGAIN
	}
	return 0
}

// leaseRecord is one parsed record.
type leaseRecord struct {
	pid   uint32
	ts    int64
	token [leaseTokenBytes]byte
	path  string
}

// readLeaseRecord loads and parses the record at leasePath. ok=false covers
// every "no usable claim" shape — absent, unreadable, or corrupt — which
// the classify rules treat as free. A record longer than leaseRecordMax is
// corrupt (no honest writer produces one).
func readLeaseRecord(leasePath string) (leaseRecord, bool) {
	b, rc := ReadFileAll(leasePath, leaseRecordMax)
	if rc < 0 || len(b) >= leaseRecordMax {
		return leaseRecord{}, false
	}
	return parseLeaseRecord(b)
}

// parseLeaseRecord parses the VLEASE1 grammar strictly: header line, then
// pid=/ts=/token=/path= rows in order, each \n-terminated. Any deviation is
// corrupt — fail closed to free (takeover), never to a half-guessed claim.
func parseLeaseRecord(b []byte) (leaseRecord, bool) {
	rows := strings.Split(string(b), "\n")
	if len(rows) != 6 || rows[5] != "" || rows[0] != "VLEASE1" {
		return leaseRecord{}, false
	}
	var rec leaseRecord
	for _, row := range rows[1:5] {
		key, val, found := strings.Cut(row, "=")
		if !found {
			return leaseRecord{}, false
		}
		switch key {
		case "pid":
			v, err := strconv.ParseUint(val, 10, 32)
			if err != nil {
				return leaseRecord{}, false
			}
			rec.pid = uint32(v)
		case "ts":
			v, err := strconv.ParseInt(val, 10, 64)
			if err != nil || v < 0 {
				return leaseRecord{}, false
			}
			rec.ts = v
		case "token":
			tb, err := hex.DecodeString(val)
			if err != nil || len(tb) != leaseTokenBytes {
				return leaseRecord{}, false
			}
			copy(rec.token[:], tb)
		case "path":
			if val == "" || strings.ContainsAny(val, "\x00\n") {
				return leaseRecord{}, false
			}
			rec.path = val
		default:
			return leaseRecord{}, false
		}
	}
	return rec, true
}

// writeLeaseRecord publishes one record. The HF create-open does NOT
// truncate an existing file (observed: VFWire serveOpen opens for updating
// at the old size), so the rewrite truncates to 0 first — a shorter new
// record must not leave the old one's tail behind, or every later parse
// reads corrupt.
func writeLeaseRecord(leasePath string, pid uint32, now int64, token [leaseTokenBytes]byte, path string) int64 {
	ts := now
	if ts < 0 {
		// No epoch (sys_time is -ENOSYS, #1058): record no stamp rather
		// than a negative one — the classify rules read ts=0 as "no clock".
		ts = 0
	}
	var b strings.Builder
	b.WriteString(leaseHeader)
	b.WriteString("pid=" + strconv.FormatUint(uint64(pid), 10) + "\n")
	b.WriteString("ts=" + strconv.FormatInt(ts, 10) + "\n")
	b.WriteString("token=" + hex.EncodeToString(token[:]) + "\n")
	b.WriteString("path=" + path + "\n")
	h, rc := FileOpen(leasePath, ModeWrite|ModeCreate)
	if rc < 0 {
		return rc
	}
	if rc = FileTruncate(uint32(h), 0); rc < 0 {
		FileClose(uint32(h))
		return rc
	}
	if _, wr := FileWriteAll(uint32(h), []byte(b.String())); wr < 0 {
		FileClose(uint32(h))
		return wr
	}
	FileClose(uint32(h))
	return 0
}

// classify applies the live/stale rules to one record against this
// holder's token (nil for a read-only check) and the current clock.
// See the package comment for the rules and their fail-closed edges.
func classify(rec leaseRecord, ok bool, token *[leaseTokenBytes]byte, now int64) leaseState {
	if !ok {
		return leaseFree
	}
	if token != nil && rec.token == *token {
		return leaseMine
	}
	if now >= 0 && rec.ts > 0 {
		if now-rec.ts >= LeaseExpirySeconds {
			return leaseFree
		}
		if rec.pid != 0 && pidKnowablyDead(rec.pid) {
			return leaseFree
		}
		return leaseLive
	}
	// No usable clock (no-epoch boot, or the record carries ts=0): pid
	// liveness is the only signal, and an unknown pid fails closed.
	if rec.pid != 0 && pidKnowablyDead(rec.pid) {
		return leaseFree
	}
	return leaseLive
}

// selfPID is the holder's own pid, found the only way the surface allows:
// an INTACT sys_procs scan matching the caller's binary name. Ambiguity is
// honest zero — two RUNNING rows with the same name (the two-editors case)
// cannot be told apart, and a row whose name read back zeroed (the M56d
// flake) must not be matched to anything.
func selfPID(selfName string) uint32 {
	if selfName == "" {
		return 0
	}
	var rows [64]ProcRow
	n, rc := Procs(rows[:])
	if rc < 0 || n <= 0 {
		return 0
	}
	found := uint32(0)
	for i := 0; i < n; i++ {
		if rows[i].State != ProcRunning {
			continue
		}
		name := rows[i].Name()
		if name == "" {
			continue
		}
		if name == selfName {
			if found != 0 {
				return 0
			}
			found = uint32(rows[i].PID)
		}
	}
	return found
}

// pidKnowablyDead reports whether an INTACT procs scan proves pid is not
// running right now. A failed scan returns false — the lease then lives by
// its stamp alone instead of being stolen on a read that never happened.
func pidKnowablyDead(pid uint32) bool {
	if pid == 0 {
		return false
	}
	var rows [64]ProcRow
	n, rc := Procs(rows[:])
	if rc < 0 {
		return false
	}
	for i := 0; i < n; i++ {
		if uint32(rows[i].PID) == pid && rows[i].State == ProcRunning {
			return false
		}
	}
	return true
}
