package heap

import (
	"bytes"
	"strings"
	"testing"

	"virelai/vi"
)

func testSample(seq uint64) Sample {
	return Sample{PID: 2, Session: 99, Seq: seq, LiveBytes: 1024, Objects: 4,
		Mallocs: seq + 4, Frees: seq, NumGC: seq}
}

func TestRingNewestRowsAndCanonicalFormat(t *testing.T) {
	var ring Ring
	for seq := uint64(1); seq <= MaxRows+9; seq++ {
		if err := ring.Append(testSample(seq)); err != nil {
			t.Fatal(err)
		}
	}
	body := ring.Bytes()
	if len(body) > MaxBytes || bytes.Count(body, []byte("\n")) != MaxRows {
		t.Fatalf("ring bytes=%d rows=%d", len(body), bytes.Count(body, []byte("\n")))
	}
	series, err := ParseSeries(body)
	if err != nil || len(series) != MaxRows || series[0].Seq != 10 || series[MaxRows-1].Seq != MaxRows+9 {
		t.Fatalf("series=%v err=%v", series, err)
	}
	want := "H1 2 99 1 1024 4 5 1 1"
	if got := testSample(1).Row(); got != want {
		t.Fatalf("row=%q want=%q", got, want)
	}
	before := ring.Bytes()
	if err := ring.Append(testSample(99)); err == nil || !bytes.Equal(before, ring.Bytes()) {
		t.Fatal("invalid append changed ring")
	}
}

func TestMalformedRowsAndPartialSeriesFailClosed(t *testing.T) {
	for _, row := range []string{
		"", "H2 2 99 1 1024 4 5 1 1", "H1 2 99 0 1024 4 5 1 1",
		"H1 2 0 1 1024 4 5 1 1", "H1 2 99 01 1024 4 5 1 1",
		"H1 2 99 1 1024 4 1 5 1", "H1 2 99 1 1024 5 5 1 1",
		strings.Repeat("9", MaxRowBytes+1), "H1 2 99 1 1024 4 5 1 1 extra",
	} {
		if _, err := ParseRow(row); err == nil {
			t.Fatalf("accepted %q", row)
		}
	}
	for _, body := range []string{"", testSample(1).Row(),
		testSample(1).Row() + "\n" + testSample(3).Row() + "\n"} {
		if _, err := ParseSeries([]byte(body)); err == nil {
			t.Fatalf("accepted partial/discontinuous series %q", body)
		}
	}
	for _, app := range []string{"", "..", "../leak", "a/b", "a\nb", strings.Repeat("a", 29)} {
		if Path(app) != "" {
			t.Fatalf("accepted path %q", app)
		}
	}
	if Path("GOEDIT.ELF") != "/host/HEAP/GOEDIT.ELF.TXT" {
		t.Fatal("wrong publisher path")
	}
}

func makeSeries(live, pages []uint64) []Joined {
	series := make([]Joined, len(live))
	for i := range series {
		sample := testSample(uint64(i + 1))
		sample.LiveBytes = live[i]
		series[i] = Joined{Sample: sample, Kernel: vi.MemstatRecord{
			PID: sample.PID, LivePages: pages[i], PeakPages: pages[i]}}
	}
	return series
}

func TestOnlySustainedLeakFlags(t *testing.T) {
	for _, tc := range []struct {
		name        string
		live, pages []uint64
		want        bool
	}{
		{"steady", []uint64{100, 100, 100, 100, 100}, []uint64{10, 10, 10, 10, 10}, false},
		{"sawtooth", []uint64{100, 900000, 100, 900000, 100, 900000}, []uint64{10, 20, 20, 20, 20, 20}, false},
		{"leak", []uint64{100, 524388, 1048676, 1572964, 2097252}, []uint64{10, 138, 266, 394, 522}, true},
		{"one-off spike", []uint64{100, 900000, 900000, 900000, 900000}, []uint64{10, 230, 230, 230, 230}, false},
		{"tiny rises", []uint64{100, 110, 120, 130, 140}, []uint64{10, 11, 12, 13, 14}, false},
		{"Go only", []uint64{100, 600000, 1200000, 1800000}, []uint64{10, 10, 10, 10}, false},
		{"kernel only", []uint64{100, 100, 100, 100}, []uint64{10, 200, 400, 600}, false},
		{"too short", []uint64{100, 600000, 1200000}, []uint64{10, 200, 400}, false},
		// Pinned joined values observed in the first M94d VZ fixture pair.
		{"recorded leak", []uint64{56000, 580848, 1105160, 1629496, 2153784, 2678168, 3202456},
			[]uint64{305, 491, 651, 806, 957, 1103, 1275}, true},
		{"recorded clean", []uint64{56016, 56552, 56552, 56552, 56552, 56568, 56568},
			[]uint64{305, 362, 394, 421, 444, 461, 471}, false},
		{"recorded editor", []uint64{68328, 91984, 83248, 65776, 65776},
			[]uint64{199, 273, 322, 360, 391}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Suspected(makeSeries(tc.live, tc.pages)); got != tc.want {
				t.Fatalf("suspected=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestTrendResetsOnIdentitySequenceGCOrKernelDiscontinuity(t *testing.T) {
	for _, mutate := range []func(*Joined){
		func(row *Joined) { row.PID++ },
		func(row *Joined) { row.Session++ },
		func(row *Joined) { row.Seq++ },
		func(row *Joined) { row.NumGC = 1 },
		func(row *Joined) { row.Kernel.PID++ },
		func(row *Joined) { row.Kernel.Flags = vi.MemstatExited },
		func(row *Joined) { row.Kernel.LivePages = 0 },
	} {
		series := makeSeries([]uint64{100, 600000, 1200000, 1800000}, []uint64{10, 200, 400, 600})
		mutate(&series[2])
		if Suspected(series) {
			t.Fatal("discontinuous series flagged")
		}
	}
}
