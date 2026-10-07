package main

import (
	"strconv"
	"strings"
	"testing"

	"virelai/vi"
)

func TestFixtureRows(t *testing.T) {
	for i, want := range []struct {
		level    byte
		tag, msg string
	}{{'D', "net", "line=000001"}, {'I', "ui", "line=000002"},
		{'W', "net", "line=000003"}, {'E', "ui", "line=000004"},
		{'D', "net", "line=000005"}} {
		level, tag, msg := fixtureRow(i + 1)
		if level != want.level || tag != want.tag || msg != want.msg {
			t.Fatalf("row %d: %c %s %s", i+1, level, tag, msg)
		}
	}
}

func seededRing() []byte {
	var ring strings.Builder
	for i := 1; i <= vi.AppLogMaxLines; i++ {
		level, tag, msg := fixtureRow(i)
		ring.WriteString(strconv.Itoa(100000+i) + " " + string(level) + " " + tag + " " + msg + "\n")
	}
	return []byte(ring.String())
}

func TestBurstPublishesOnlyCompleteSameSizeSnapshot(t *testing.T) {
	old := seededRing()
	reads, writes := 0, 0
	rc := publishBurst("A", 1, 64, func(app string) ([]byte, int64) {
		reads++
		if app != "A" {
			t.Fatalf("read app=%q", app)
		}
		return old, 0
	}, func(path string, body []byte) int64 {
		writes++
		if path != vi.AppLogPath("A") || len(body) != len(old) {
			t.Fatalf("snapshot path=%q bytes=%d want=%d", path, len(body), len(old))
		}
		rows := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
		if len(rows) != vi.AppLogMaxLines {
			t.Fatalf("ring rows=%d", len(rows))
		}
		for i, raw := range rows {
			row := vi.ParseLogLine(raw)
			level, tag, message := fixtureRow(i + 33)
			if row.Seq != uint64(100065+i) || row.Level != level ||
				row.Tag != tag || row.Message != message {
				t.Fatalf("partial/wrong snapshot row=%+v", row)
			}
		}
		return 0
	})
	if rc != 0 || reads != 1 || writes != 1 {
		t.Fatalf("rc=%d reads=%d publications=%d", rc, reads, writes)
	}
}

func TestBurstPartialGroupAndFailurePropagation(t *testing.T) {
	rc := publishBurst("B", 5, 6, func(string) ([]byte, int64) {
		return nil, vi.ErrFileNotFound
	}, func(_ string, body []byte) int64 {
		if string(body) != "1 D net line=000005\n2 I ui line=000006\n" {
			t.Fatalf("partial final group=%q", body)
		}
		return -vi.ErrENOSPC
	})
	if rc != -vi.ErrENOSPC {
		t.Fatalf("publish error=%d", rc)
	}
	written := false
	writer := func(string, []byte) int64 { written = true; return 0 }
	for _, read := range []func(string) ([]byte, int64){
		func(string) ([]byte, int64) { return nil, -vi.ErrEBADF },
		func(string) ([]byte, int64) { return []byte("18446744073709551615 I net full\n"), 0 },
	} {
		if rc := publishBurst("A", 1, 64, read, writer); rc >= 0 {
			t.Fatalf("read/wrap refusal rc=%d", rc)
		}
	}
	if written {
		t.Fatal("failed burst published an intermediate ring")
	}
}

func TestBurstBounds(t *testing.T) {
	read := func(string) ([]byte, int64) {
		return []byte(strings.Repeat("x", vi.AppLogMaxLines*(vi.AppLogMaxLine+1))), 0
	}
	write := func(_ string, body []byte) int64 {
		if len(body) > vi.AppLogMaxLines*(vi.AppLogMaxLine+1) {
			t.Fatalf("unbounded snapshot: %d", len(body))
		}
		return 0
	}
	if rc := publishBurst("A", 1, 1024, read, write); rc != 0 {
		t.Fatalf("maximum group rc=%d", rc)
	}
	if rc := publishBurst("A", 1, 2, read, write); rc != 0 {
		t.Fatalf("oversized old row rc=%d", rc)
	}
	for _, test := range []struct {
		app         string
		first, last int
	}{{"../A", 1, 64}, {"A", 0, 64}, {"A", 2, 1}, {"A", 1, 1025}} {
		if rc := publishBurst(test.app, test.first, test.last, read, write); rc != -vi.ErrEINVAL {
			t.Fatalf("accepted invalid burst %+v rc=%d", test, rc)
		}
	}
}
