package vi

import (
	"fmt"
	"hash/fnv"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Per-app logs are deliberately small share files so the owner can inspect
// them after an app exits. The M97g (#2085) binding makes the label a
// security boundary: the kernel ties an APPLOG/CRASH label to the
// caller's own recorded image name (or to a registry row the caller
// spawned — the supervisor case), and every other label fails EACCES for
// a uid_user actor. Privileged actors keep the bypass (the operator's
// `-u0` viewer channel); ADR 0024 records the policy.
const (
	AppLogDir       = "/host/APPLOG"
	CrashReceiptDir = "/host/CRASH"
	// CrashStackMaxBytes bounds the entire additive .STK file, header included.
	CrashStackMaxBytes = 16 * 1024
	AppLogMaxLines     = 32
	AppLogMaxLine      = 256
	appLogMaxBytes     = AppLogMaxLines * (AppLogMaxLine + 1)
	appLogMaxApps      = MaxDirEntries
	crashLogLines      = 8
)

// appFileName validates an app label before it is used as a share path
// component. Labels are the app's basename, such as GOSH.ELF.
func appFileName(app string) bool {
	if len(app) == 0 || len(app) > 28 || app == "." || app == ".." {
		return false
	}
	for i := 0; i < len(app); i++ {
		c := app[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// AppLogPath returns the share path for an app's bounded log ring, or "" for
// an invalid app label.
func AppLogPath(app string) string {
	if !appFileName(app) {
		return ""
	}
	return AppLogDir + "/" + app + ".LOG"
}

// CrashReceiptPath returns the share path for an app's latest failure receipt,
// or "" for an invalid app label.
func CrashReceiptPath(app string) string {
	if !appFileName(app) {
		return ""
	}
	return CrashReceiptDir + "/" + app + ".TXT"
}

// CrashStackPath names the stack sibling without changing the receipt format.
func CrashStackPath(app string) string {
	if !appFileName(app) {
		return ""
	}
	return CrashReceiptDir + "/" + app + ".STK"
}

func captureCrashStack(receipt []byte, nanos int64, capture func([]byte, bool) int) []byte {
	stack := make([]byte, CrashStackMaxBytes-128)
	n := capture(stack, false) // only the panicking goroutine, not unrelated tasks
	truncated := n == len(stack)
	hash := fnv.New64a()
	_, _ = hash.Write(receipt)
	header := "VCRASH1 receipt=" + strconv.FormatUint(hash.Sum64(), 16) +
		" nanos=" + strconv.FormatInt(nanos, 10) +
		" truncated=" + strconv.FormatBool(truncated) + "\n"
	return append([]byte(header), stack[:n]...)
}

// CrashGuard must be deferred directly in the goroutine being guarded:
//
//	defer vi.CrashGuard("APP.ELF")
//
// A panic publishes the frozen receipt and a bounded current-goroutine stack
// in its sibling, then exits 2 even if either diagnostic write fails. Other
// goroutines need their own guard. A normal return does nothing.
func CrashGuard(app string) {
	if value := recover(); value != nil {
		rc := WriteCrashReceipt(app, "panic: "+fmt.Sprint(value))
		if rc >= 0 {
			receipt, readRC := ReadFileAll(CrashReceiptPath(app), 4096)
			rc = readRC
			if rc >= 0 {
				rc = WriteFileSafe(CrashStackPath(app),
					captureCrashStack(receipt, Nanos(), runtime.Stack))
			}
		}
		if rc < 0 {
			ConsoleLine("crashguard: diagnostic write failed rc=" + Itoa64(rc))
		}
		Exit(2)
	}
}

func ensureLogDir(path string) int64 {
	h, rc := FileOpen(path, ModeWrite|ModeCreate|ModeDir)
	if rc == ErrFileExists {
		return 0
	}
	if rc < 0 {
		return rc
	}
	FileClose(uint32(h))
	return 0
}

func oneLogLine(line string) string {
	line = strings.ReplaceAll(line, "\r", " ")
	line = strings.ReplaceAll(line, "\n", " ")
	if len(line) > AppLogMaxLine {
		line = line[:AppLogMaxLine]
	}
	return line
}

// trimLogRing retains the newest rows within both ring bounds.
func trimLogRing(body []byte, maxLines, maxBytes int) []byte {
	if len(body) == 0 {
		return nil
	}
	rows := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	start, used := len(rows), 0
	for start > 0 && len(rows)-start < maxLines {
		n := len(rows[start-1]) + 1
		if n > maxBytes-used {
			break
		}
		used += n
		start--
	}
	return []byte(strings.Join(rows[start:], "\n") + "\n")
}

// Log appends one line to app's bounded persistent ring. A message is
// normalized to one line and capped before it enters the ring. The returned
// value is the underlying file error (zero on success). `app` must bind to
// the caller's own image name (modulo a ".ELF" suffix) or to a process the
// caller spawned — a foreign label returns EACCES (#2085).
func Log(app, message string) int64 {
	return appendAppLog(app, 0, "", message)
}

// LogLine is a parsed log row. Legacy rows have Level='I', an empty Tag and
// Sequenced=false; Message and Raw preserve their original bytes.
type LogLine struct {
	Seq       uint64
	Sequenced bool
	Level     byte
	Tag       string
	Message   string
	Raw       string
}

// LogLevel appends "<seq> <D|I|W|E> <tag> <message>". Tags are printable ASCII
// tokens of at most 32 bytes (empty is allowed). The entire row, excluding LF,
// stays within AppLogMaxLine. One producer owns an app's read/modify/write ring,
// just as for Log; this is not an atomic cross-process append.
func LogLevel(app string, level byte, tag, message string) int64 {
	if !validLogLevel(level) || !validLogTag(tag) {
		return -ErrEINVAL
	}
	return appendAppLog(app, level, tag, message)
}

func validLogLevel(level byte) bool {
	return level == 'D' || level == 'I' || level == 'W' || level == 'E'
}

func validLogTag(tag string) bool {
	if len(tag) > 32 {
		return false
	}
	for i := range tag {
		if tag[i] < '!' || tag[i] > '~' {
			return false
		}
	}
	return true
}

// ParseLogLine recognizes the additive format without reinterpreting malformed
// or ordinary legacy rows. An empty tag is represented by two spaces.
func ParseLogLine(raw string) LogLine {
	plain := LogLine{Level: 'I', Message: raw, Raw: raw}
	fields := strings.SplitN(raw, " ", 4)
	if len(fields) != 4 || len(fields[1]) != 1 ||
		!validLogLevel(fields[1][0]) || !validLogTag(fields[2]) {
		return plain
	}
	seq, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || seq == 0 || strconv.FormatUint(seq, 10) != fields[0] {
		return plain
	}
	return LogLine{Seq: seq, Sequenced: true, Level: fields[1][0],
		Tag: fields[2], Message: fields[3], Raw: raw}
}

func levelLogRow(old []byte, level byte, tag, message string) (string, int64) {
	var seq uint64
	for _, raw := range strings.Split(string(old), "\n") {
		if line := ParseLogLine(raw); line.Sequenced {
			seq = line.Seq
		}
	}
	if seq == ^uint64(0) {
		return "", -ErrEINVAL // refuse wrap rather than repeat a sequence
	}
	prefix := strconv.FormatUint(seq+1, 10) + " " + string(level) + " " + tag + " "
	message = oneLogLine(message)
	if len(message) > AppLogMaxLine-len(prefix) {
		message = message[:AppLogMaxLine-len(prefix)]
	}
	return prefix + message, 0
}

func appendAppLog(app string, level byte, tag, message string) int64 {
	path := AppLogPath(app)
	if path == "" {
		return -ErrEINVAL
	}
	if rc := ensureLogDir(AppLogDir); rc < 0 {
		return rc
	}
	names, rc := AppLogNames()
	if rc < 0 {
		return rc
	}
	known := false
	for _, name := range names {
		if name == app {
			known = true
			break
		}
	}
	if !known && len(names) >= appLogMaxApps {
		return -ErrENOSPC
	}
	old, rc := ReadFileAll(path, appLogMaxBytes+1)
	if rc < 0 && rc != ErrFileNotFound {
		return rc
	}
	if len(old) > appLogMaxBytes {
		old = old[len(old)-appLogMaxBytes:]
	}
	line := oneLogLine(message)
	if level != 0 {
		var rc int64
		line, rc = levelLogRow(old, level, tag, message)
		if rc < 0 {
			return rc
		}
	}
	row := []byte(line + "\n")
	next := append(append([]byte(nil), old...), row...)
	next = trimLogRing(next, AppLogMaxLines, appLogMaxBytes)
	return WriteFileSafe(path, next)
}

// ReadLog reads one app's ring. A missing log is returned as ENOENT. A
// label the caller does not own returns EACCES — reads are bound, not open
// (#2085); a privileged caller keeps the view-everything channel.
func ReadLog(app string) ([]byte, int64) {
	path := AppLogPath(app)
	if path == "" {
		return nil, -ErrEINVAL
	}
	return ReadFileAll(path, appLogMaxBytes)
}

// AppLogNames lists the app labels with log rings, sorted for deterministic
// display. The directory ABI is bounded to MaxDirEntries, so rings are
// intentionally bounded to the same number of distinct app files. Listing
// stays open — names are enumerable; the ring CONTENTS are bound (#2085).
func AppLogNames() ([]string, int64) {
	var entries [appLogMaxApps]DirEntry
	n, rc := DirList(AppLogDir, entries[:])
	if rc < 0 {
		return nil, rc
	}
	names := make([]string, 0, n)
	for _, e := range entries[:n] {
		if e.Dir() {
			continue
		}
		file := e.NameString()
		if !strings.HasSuffix(file, ".LOG") {
			continue
		}
		app := strings.TrimSuffix(file, ".LOG")
		if appFileName(app) {
			names = append(names, app)
		}
	}
	sort.Strings(names)
	return names, 0
}

func lastLogLines(body []byte, count int) []byte {
	rows := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if len(rows) > count {
		rows = rows[len(rows)-count:]
	}
	if len(rows) == 1 && rows[0] == "" {
		return nil
	}
	return []byte(strings.Join(rows, "\n") + "\n")
}

// WriteCrashReceipt publishes the latest exit or panic record for app,
// including only its last eight log lines. The label binding applies to the
// receipt AND to the embedded log tail: a foreign label fails EACCES before
// any bytes move, so one app cannot forge another's receipt or smuggle its
// log lines (#2085).
func WriteCrashReceipt(app, outcome string) int64 {
	path := CrashReceiptPath(app)
	if path == "" {
		return -ErrEINVAL
	}
	if rc := ensureLogDir(CrashReceiptDir); rc < 0 {
		return rc
	}
	outcome = oneLogLine(outcome)
	log, rc := ReadLog(app)
	if rc < 0 && rc != ErrFileNotFound {
		return rc
	}
	var b strings.Builder
	b.WriteString("app=" + app + "\noutcome=" + outcome + "\nlast-log:\n")
	b.Write(lastLogLines(log, crashLogLines))
	return WriteFileSafe(path, []byte(b.String()))
}
