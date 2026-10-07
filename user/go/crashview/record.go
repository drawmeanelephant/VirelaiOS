package main

import (
	"hash/fnv"
	"strconv"
	"strings"

	"virelai/vi"
)

const (
	receiptLimit = 4096
	kernelLimit  = 1024
	recordLimit  = 32
	frameLimit   = 128
)

type entryKind byte

const (
	unknownEntry entryKind = iota
	receiptEntry
	stackEntry
	kernelEntry
)

type frame struct{ function, source string }

type record struct {
	app, kind, outcome, path string
	frames                   []frame
	log, serial              string
	pid, tick                uint64
	generation               int64
	truncated                bool
}

func classify(name string) (entryKind, string) {
	dot := strings.LastIndexByte(name, '.')
	if dot < 1 {
		return unknownEntry, ""
	}
	base, ext := name[:dot], strings.ToUpper(name[dot:])
	// Writers freeze .TXT for app receipts and .txt for tombstones. Preserve
	// that distinction for an app label such as "1-APP.ELF".
	if name[dot:] == ".txt" {
		if dash := strings.IndexByte(base, '-'); dash > 0 {
			pid, err := strconv.ParseUint(base[:dash], 10, 64)
			app := base[dash+1:]
			if err == nil && pid > 0 && vi.CrashReceiptPath(app) != "" {
				return kernelEntry, app
			}
		}
	}
	if vi.CrashReceiptPath(base) == "" {
		return unknownEntry, ""
	}
	switch ext {
	case ".TXT":
		return receiptEntry, base
	case ".STK":
		return stackEntry, base
	}
	return unknownEntry, ""
}

func hashBytes(body []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(body)
	return h.Sum64()
}

func fingerprint(body, stack []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(body)
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(stack)
	return h.Sum64()
}

func parseRecord(name string, body, sidecar []byte) (record, bool) {
	kind, app := classify(name)
	r := record{app: app}
	switch kind {
	case receiptEntry:
		if len(body) > receiptLimit {
			return r, false
		}
		head, tail, ok := strings.Cut(string(body), "\nlast-log:\n")
		if !ok {
			return r, false
		}
		fields := strings.Split(head, "\n")
		if len(fields) != 2 || fields[0] != "app="+app ||
			!strings.HasPrefix(fields[1], "outcome=") {
			return r, false
		}
		r.outcome = strings.TrimPrefix(fields[1], "outcome=")
		r.log = tail
		r.kind = "exit"
		if strings.HasPrefix(r.outcome, "panic: ") {
			r.kind = "go"
			r.frames, r.generation, r.truncated = parseStack(body, sidecar)
		}
		return r, true
	case kernelEntry:
		if len(body) > kernelLimit || !strings.HasPrefix(string(body),
			"VirelaiOS Crash Tombstone\n========================\n") {
			return r, false
		}
		head := string(body)
		if before, serial, found := strings.Cut(head, "\n--- Last Serial Output ---\n"); found {
			head = before
			r.serial, _, _ = strings.Cut(serial, "\n--- End Serial Output ---")
		}
		fields := map[string]string{}
		for _, line := range strings.Split(head, "\n")[2:] {
			key, value, ok := strings.Cut(line, ": ")
			if ok {
				if _, duplicate := fields[key]; duplicate {
					return r, false
				}
				fields[key] = value
			}
		}
		if fields["Process"] != app {
			return r, false
		}
		pid, err := strconv.ParseUint(fields["PID"], 10, 64)
		if err != nil || pid == 0 || !strings.HasPrefix(name, strconv.FormatUint(pid, 10)+"-") {
			return r, false
		}
		status, err := strconv.ParseUint(fields["Exit Status"], 10, 64)
		if err != nil {
			return r, false
		}
		r.tick, err = strconv.ParseUint(fields["Tick"], 10, 64)
		if err != nil {
			return r, false
		}
		r.pid, r.kind = pid, "kernel"
		r.outcome = "exit=" + strconv.FormatUint(status, 10)
		if address := fields["Fault Address"]; address != "" {
			r.outcome += " fault=" + address
		}
		if symbol := fields["Symbol"]; symbol != "" {
			r.frames = []frame{{function: symbol}} // exactly one recorded PC
		}
		r.truncated = len(body) == kernelLimit
		return r, true
	}
	return r, false
}

func parseStack(receipt, body []byte) ([]frame, int64, bool) {
	if len(body) > vi.CrashStackMaxBytes {
		return nil, 0, false
	}
	header, stack, ok := strings.Cut(string(body), "\n")
	parts := strings.Fields(header)
	if !ok || len(parts) != 4 || parts[0] != "VCRASH1" ||
		parts[1] != "receipt="+strconv.FormatUint(hashBytes(receipt), 16) ||
		!strings.HasPrefix(parts[2], "nanos=") ||
		(parts[3] != "truncated=true" && parts[3] != "truncated=false") {
		return nil, 0, false
	}
	nanos, err := strconv.ParseInt(strings.TrimPrefix(parts[2], "nanos="), 10, 64)
	if err != nil || nanos <= 0 || !strings.HasPrefix(stack, "goroutine ") {
		return nil, 0, false
	}
	var frames []frame
	lines := strings.Split(stack, "\n")
	for i := 1; i+2 < len(lines) && len(frames) < frameLimit; i++ {
		fn, source := lines[i], lines[i+1]
		if fn == "" || strings.HasPrefix(fn, "created by ") ||
			strings.HasPrefix(fn, "goroutine ") || strings.HasPrefix(fn, "\t") ||
			!strings.HasPrefix(source, "\t") ||
			(!strings.Contains(source, ".go:") && !strings.Contains(source, ".s:")) {
			continue
		}
		frames = append(frames, frame{fn, strings.TrimSpace(source)})
		i++
	}
	return frames, nanos, parts[3] == "truncated=true"
}

func (r record) stackNote() string {
	if r.kind == "exit" {
		return "stack: none (exit status only)"
	}
	if r.kind == "kernel" && len(r.frames) != 0 {
		return "stack: one recorded PC (not an unwound stack)"
	}
	if len(r.frames) == 0 {
		return "stack: unavailable"
	}
	if r.truncated {
		return "stack: truncated"
	}
	return "stack: panicking goroutine"
}

type viewer struct {
	seen     map[string]uint64
	records  []record
	selected int
}

func newViewer() *viewer { return &viewer{seen: make(map[string]uint64)} }

func (v *viewer) observe(path, name string, body, stack []byte) (record, bool) {
	r, ok := parseRecord(name, body, stack)
	if !ok {
		return r, false
	}
	key := path
	hash := fingerprint(body, stack)
	if old, exists := v.seen[key]; exists && old == hash {
		return r, false
	}
	r.path = path
	v.seen[key] = hash
	for i := range v.records {
		if v.records[i].path == path {
			v.records = append(v.records[:i], v.records[i+1:]...)
			break
		}
	}
	v.records = append([]record{r}, v.records...)
	if len(v.records) > recordLimit {
		old := v.records[recordLimit]
		delete(v.seen, old.path)
		v.records = v.records[:recordLimit]
	}
	v.selected = 0 // newest observed receipt first
	return r, true
}
