package vi

import (
	"sort"
	"strconv"
	"strings"
)

// Per-app logs are deliberately small share files so the owner can inspect
// them after an app exits. They are diagnostic records, not an isolation or
// authorization mechanism (ADR 0024).
const (
	AppLogDir       = "/host/APPLOG"
	CrashReceiptDir = "/host/CRASH"
	AppLogMaxLines  = 32
	AppLogMaxLine   = 256
	appLogMaxBytes  = AppLogMaxLines * (AppLogMaxLine + 1)
	appLogMaxApps   = MaxDirEntries
	crashLogLines   = 8
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
// value is the underlying file error (zero on success).
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

// ReadLog reads one app's ring. A missing log is returned as ENOENT.
func ReadLog(app string) ([]byte, int64) {
	path := AppLogPath(app)
	if path == "" {
		return nil, -ErrEINVAL
	}
	return ReadFileAll(path, appLogMaxBytes)
}

// AppLogNames lists the app labels with log rings, sorted for deterministic
// display. The directory ABI is bounded to MaxDirEntries, so rings are
// intentionally bounded to the same number of distinct app files.
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
// including only its last eight log lines. Receipts record failures; they do
// not enforce policy or change the ADR 0024 isolation boundary.
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
