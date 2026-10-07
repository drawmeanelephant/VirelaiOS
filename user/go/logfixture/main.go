package main

import (
	"strconv"
	"strings"
	"time"

	"virelai/vi"
)

// LOGFIX label rate count publishes rate rows per second. Publish each burst
// once, so the same-size snapshot drill cannot expose an intermediate ring.
// Rate 1 still exercises the SDK's live, per-row publication path.
func main() {
	args := vi.Args()
	if len(args) != 4 {
		vi.ConsoleLine("usage: LOGFIX label rate count")
		vi.Exit(2)
	}
	app := args[1]
	rate, e1 := strconv.Atoi(args[2])
	count, e2 := strconv.Atoi(args[3])
	if vi.AppLogPath(app) == "" || e1 != nil || e2 != nil ||
		rate < 1 || rate > 1024 || count < 1 || count > 4096 {
		vi.ConsoleLine("logfix: invalid arguments")
		vi.Exit(2)
	}
	if rate > 1 {
		h, rc := vi.FileOpen(vi.AppLogDir, vi.ModeWrite|vi.ModeCreate|vi.ModeDir)
		if rc < 0 && rc != vi.ErrFileExists {
			vi.ConsoleLine("logfix: error app=" + app + " rc=" + vi.Itoa64(rc))
			vi.Exit(1)
		}
		if rc >= 0 {
			vi.FileClose(uint32(h))
		}
		names, rc := vi.AppLogNames()
		known := false
		for _, name := range names {
			known = known || name == app
		}
		if rc >= 0 && !known && len(names) >= vi.MaxDirEntries {
			rc = -vi.ErrENOSPC
		}
		if rc < 0 {
			vi.ConsoleLine("logfix: error app=" + app + " rc=" + vi.Itoa64(rc))
			vi.Exit(1)
		}
	}
	for first := 1; first <= count; first += rate {
		last := first + rate - 1
		if last > count {
			last = count
		}
		var rc int64
		if rate == 1 {
			level, tag, message := fixtureRow(first)
			rc = vi.LogLevel(app, level, tag, message)
		} else {
			rc = publishBurst(app, first, last, vi.ReadLog, vi.WriteFileSafe)
		}
		if rc < 0 {
			vi.ConsoleLine("logfix: error app=" + app + " rc=" + vi.Itoa64(rc))
			vi.Exit(1)
		}
		if first == 1 {
			vi.ConsoleLine("logfix: active app=" + app)
		}
		if last < count {
			time.Sleep(time.Second)
		}
	}
	vi.ConsoleLine("logfix: done app=" + app + " count=" + strconv.Itoa(count))
	time.Sleep(2 * time.Second)
}

func publishBurst(app string, first, last int, read func(string) ([]byte, int64), write func(string, []byte) int64) int64 {
	path := vi.AppLogPath(app)
	if path == "" || first < 1 || last < first || last-first >= 1024 {
		return -vi.ErrEINVAL
	}
	old, rc := read(app)
	if rc < 0 && rc != vi.ErrFileNotFound {
		return rc
	}
	rows := strings.Split(strings.TrimSuffix(string(old), "\n"), "\n")
	if len(rows) == 1 && rows[0] == "" {
		rows = nil
	}
	var seq uint64
	for _, raw := range rows {
		if line := vi.ParseLogLine(raw); line.Sequenced {
			seq = line.Seq
		}
	}
	if seq > ^uint64(0)-uint64(last-first+1) {
		return -vi.ErrEINVAL
	}
	for i := first; i <= last; i++ {
		level, tag, message := fixtureRow(i)
		seq++
		rows = append(rows, strconv.FormatUint(seq, 10)+" "+string(level)+" "+tag+" "+message)
	}
	if len(rows) > vi.AppLogMaxLines {
		rows = rows[len(rows)-vi.AppLogMaxLines:]
	}
	next := strings.Join(rows, "\n") + "\n"
	for len(next) > vi.AppLogMaxLines*(vi.AppLogMaxLine+1) {
		next = next[strings.IndexByte(next, '\n')+1:]
	}
	return write(path, []byte(next))
}

func fixtureRow(i int) (byte, string, string) {
	tag := "net"
	if i%2 == 0 {
		tag = "ui"
	}
	n := strconv.Itoa(i)
	for len(n) < 6 {
		n = "0" + n
	}
	return "DIWE"[(i-1)%4], tag, "line=" + n
}
