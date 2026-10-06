package main

import (
	"strconv"
	"strings"

	"virelai/vi"
)

const viewMaxRows = vi.MaxDirEntries * vi.AppLogMaxLines

type filter struct {
	apps  []string
	level byte
	tag   string
	grep  string
}

func levelRank(level byte) int {
	return strings.IndexByte("DIWE", level)
}

func (f filter) follows(app string) bool {
	if len(f.apps) == 0 {
		return true
	}
	for _, name := range f.apps {
		if name == app {
			return true
		}
	}
	return false
}

func (f filter) matches(e entry) bool {
	return f.follows(e.app) && (e.lost != 0 ||
		(levelRank(e.line.Level) >= levelRank(f.level) &&
			(f.tag == "" || f.tag == e.line.Tag) &&
			strings.Contains(e.line.Message, f.grep)))
}

type entry struct {
	app  string
	line vi.LogLine
	lost uint64
}

func (e entry) text() string {
	if e.lost != 0 {
		return "gap app=" + e.app + " lost=" + strconv.FormatUint(e.lost, 10)
	}
	return e.app + " " + e.line.Raw
}

type cursor struct {
	body string
	seq  uint64
}

type viewer struct {
	filter  filter
	cursors map[string]*cursor
	rows    []entry
}

func newViewer(f filter) *viewer {
	return &viewer{filter: f, cursors: make(map[string]*cursor)}
}

func logRows(body string) []string {
	if body == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(body, "\n"), "\n")
}

// overlap finds the old suffix still retained as the new prefix. Plain rows
// have no sequence, so only this content overlap can distinguish new rows.
func overlap(old, next []string) int {
	n := len(old)
	if n > len(next) {
		n = len(next)
	}
	for ; n > 0; n-- {
		equal := true
		for i := 0; i < n; i++ {
			if old[len(old)-n+i] != next[i] {
				equal = false
				break
			}
		}
		if equal {
			return n
		}
	}
	return 0
}

// observe preserves first-observed arrival order. A poll visits apps in stable
// name order, the tie-break for rows whose cross-app write time is unknowable.
// Sequence gaps are detected BEFORE filters, so excluded rows are not losses.
func (v *viewer) observe(app string, body []byte) []entry {
	if !v.filter.follows(app) {
		return nil
	}
	c := v.cursors[app]
	if c == nil {
		if len(v.cursors) >= vi.MaxDirEntries {
			return nil
		}
		c = &cursor{}
		v.cursors[app] = c
	}
	next := string(body)
	if next == c.body {
		return nil
	}
	rows := logRows(next)
	start := overlap(logRows(c.body), rows)
	var added []entry
	for i, raw := range rows {
		line := vi.ParseLogLine(raw)
		if line.Sequenced {
			if line.Seq <= c.seq {
				continue
			}
			if line.Seq-c.seq > 1 {
				added = append(added, entry{app: app, lost: line.Seq - c.seq - 1})
			}
			c.seq = line.Seq
		} else if i < start {
			continue
		}
		added = append(added, entry{app: app, line: line})
	}
	c.body = next
	v.rows = append(v.rows, added...)
	if len(v.rows) > viewMaxRows {
		v.rows = append([]entry(nil), v.rows[len(v.rows)-viewMaxRows:]...)
	}
	var visible []entry
	for _, e := range added {
		if v.filter.matches(e) {
			visible = append(visible, e)
		}
	}
	return visible
}

func (v *viewer) visible() []entry {
	var rows []entry
	for _, e := range v.rows {
		if v.filter.matches(e) {
			rows = append(rows, e)
		}
	}
	return rows
}

// exportBodies is the filtered, bounded view grouped by source app. It keeps
// each app's gaps beside its rows and includes the same app column as serial.
func (v *viewer) exportBodies() map[string][]byte {
	bodies := make(map[string][]byte)
	for _, e := range v.visible() {
		bodies[e.app] = append(bodies[e.app], []byte(e.text()+"\n")...)
	}
	return bodies
}

const exportDir = "/host/LOGEXPORT"

// exportView creates new numbered files, never overwriting a prior export.
// The explicit I/O seams let host tests check paths, filtering and refusal.
func (v *viewer) exportView(exists func(string) (bool, int64),
	write func(string, []byte) int64) ([]string, int64) {
	bodies := v.exportBodies()
	var paths []string
	// Stable export order, matching ring discovery.
	for _, app := range sortedApps(bodies) {
		for k := 1; ; k++ {
			path := exportDir + "/" + app + "-" + strconv.Itoa(k) + ".TXT"
			// HF components are 31 bytes, including the suffix.
			if len(app+"-"+strconv.Itoa(k)+".TXT") > 31 {
				return paths, -vi.ErrENOSPC
			}
			present, rc := exists(path)
			if rc < 0 {
				return paths, rc
			}
			if present {
				continue
			}
			if rc := write(path, bodies[app]); rc < 0 {
				return paths, rc
			}
			paths = append(paths, path)
			break
		}
	}
	return paths, 0
}
