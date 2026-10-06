package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMeasurementStatistics(t *testing.T) {
	values := []int64{103, 101, 200, 104, 100, 99, 102}
	if got := median(values); got != 102 {
		t.Fatalf("median=%d", got)
	}
	minimum, maximum := bounds(values)
	if minimum != 99 || maximum != 200 {
		t.Fatalf("range=%d..%d", minimum, maximum)
	}
	if values[0] != 103 {
		t.Fatal("statistics reordered the measurements")
	}
	if measurementPairs < 7 {
		t.Fatal("owner requires at least seven pairs")
	}
}

// Synthetic serial tests validate the gate logic, not hardware overhead.
func TestOverheadAssertionRejectsUnresolvedAndBrokenSets(t *testing.T) {
	spec, err := os.ReadFile("../../../tools/gate/specs/live-observe.spec")
	if err != nil {
		t.Fatal(err)
	}
	start := "vgate_assert overhead python <<'PY'\n"
	_, script, ok := strings.Cut(string(spec), start)
	if !ok {
		t.Fatal("overhead assertion missing")
	}
	script, _, ok = strings.Cut(script, "\nPY\n")
	if !ok {
		t.Fatal("overhead assertion terminator missing")
	}
	tests := []struct {
		name     string
		on       int64
		outlier  bool
		grouped  bool
		wantPass bool
	}{
		{"resolved-below", 100500000, false, false, true},
		{"spin-budget-red", 110000000, false, false, false},
		{"unresolved-spread", 100500000, true, false, false},
		{"old-grouped-order", 100500000, false, true, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var serial strings.Builder
			write := func(mode string, pair int) {
				ns := int64(100000000)
				if mode == "on" {
					ns = test.on
					if test.outlier && pair == 7 {
						ns = 103000000
					}
				}
				fmt.Fprintf(&serial, "prof: work mode=%s run=%d ns=%d checksum=1234\n", mode, pair, ns)
			}
			if test.grouped {
				for _, mode := range []string{"off", "on"} {
					for pair := 1; pair <= 7; pair++ {
						write(mode, pair)
					}
				}
			} else {
				for pair := 1; pair <= 7; pair++ {
					write("off", pair)
					write("on", pair)
				}
			}
			fmt.Fprintf(&serial, "prof: samples=700 dropped=0 off_median_ns=100000000 on_median_ns=%d\n", test.on)
			maximum := test.on
			if test.outlier {
				maximum = 103000000
			}
			fmt.Fprintf(&serial, "prof: spread off_min_ns=100000000 off_max_ns=100000000 on_min_ns=%d on_max_ns=%d pairs=7\n", test.on, maximum)
			for core := 0; core <= 1; core++ {
				fmt.Fprintf(&serial, "prof: timer core=%d irq=1000 poll=0 elapsed_cntpct=240000000 freq=24000000 physical_ticks=10\n", core)
			}
			path := filepath.Join(t.TempDir(), "synthetic-serial.log")
			if err := os.WriteFile(path, []byte(serial.String()), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("python3", "-c", script)
			cmd.Env = append(os.Environ(), "VG_SER="+path)
			out, err := cmd.CombinedOutput()
			if (err == nil) != test.wantPass {
				t.Fatalf("pass=%v, want %v: %v\n%s", err == nil, test.wantPass, err, out)
			}
			if test.outlier && !strings.Contains(string(out), "UNRESOLVED") {
				t.Fatalf("spread not labeled unresolved:\n%s", out)
			}
		})
	}
}
