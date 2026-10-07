// Package heap joins opt-in post-GC Go samples with the kernel page view.
package heap

import (
	"fmt"
	"strconv"
	"strings"

	"virelai/vi"
)

const (
	Dir         = "/host/HEAP"
	MaxRows     = vi.AppLogMaxLines
	MaxRowBytes = vi.AppLogMaxLine
	MaxBytes    = MaxRows * (MaxRowBytes + 1)
	LeakRises   = 3
	LeakGrowth  = 512 * 1024
)

// Sample is self-reported Go memory, never a physical memory measurement.
// Session and PID prevent joining different producers' consecutive sequences.
type Sample struct {
	PID, Session, Seq, LiveBytes, Objects, Mallocs, Frees, NumGC uint64
}

func Path(app string) string {
	// Reuse the SDK's app-label validation, without touching its log writer.
	if vi.AppLogPath(app) == "" {
		return ""
	}
	return Dir + "/" + app + ".TXT"
}

func (sample Sample) Row() string {
	return fmt.Sprintf("H1 %d %d %d %d %d %d %d %d", sample.PID, sample.Session,
		sample.Seq, sample.LiveBytes, sample.Objects, sample.Mallocs, sample.Frees, sample.NumGC)
}

func ParseRow(row string) (Sample, error) {
	var sample Sample
	if len(row) > MaxRowBytes {
		return sample, fmt.Errorf("heap: oversized row")
	}
	fields := strings.Split(row, " ")
	if len(fields) != 9 || fields[0] != "H1" {
		return sample, fmt.Errorf("heap: invalid row")
	}
	values := []*uint64{&sample.PID, &sample.Session, &sample.Seq, &sample.LiveBytes,
		&sample.Objects, &sample.Mallocs, &sample.Frees, &sample.NumGC}
	for i, value := range values {
		n, err := strconv.ParseUint(fields[i+1], 10, 64)
		if err != nil || strconv.FormatUint(n, 10) != fields[i+1] {
			return Sample{}, fmt.Errorf("heap: invalid row integer")
		}
		*value = n
	}
	if sample.Seq == 0 || sample.Session == 0 || sample.Frees > sample.Mallocs ||
		sample.Objects != sample.Mallocs-sample.Frees {
		return Sample{}, fmt.Errorf("heap: inconsistent sample")
	}
	return sample, nil
}

// ParseSeries refuses partial publications rather than inventing a sample.
func ParseSeries(body []byte) ([]Sample, error) {
	if len(body) == 0 || len(body) > MaxBytes || body[len(body)-1] != '\n' {
		return nil, fmt.Errorf("heap: invalid series size or terminator")
	}
	rows := strings.Split(string(body[:len(body)-1]), "\n")
	if len(rows) > MaxRows {
		return nil, fmt.Errorf("heap: oversized series")
	}
	samples := make([]Sample, 0, len(rows))
	for _, row := range rows {
		sample, err := ParseRow(row)
		if err != nil {
			return nil, err
		}
		if len(samples) != 0 && !consecutive(samples[len(samples)-1], sample) {
			return nil, fmt.Errorf("heap: discontinuous series")
		}
		samples = append(samples, sample)
	}
	return samples, nil
}

func consecutive(before, after Sample) bool {
	return before.PID == after.PID && before.Session == after.Session &&
		before.Seq != ^uint64(0) && after.Seq == before.Seq+1 &&
		after.Mallocs >= before.Mallocs && after.Frees >= before.Frees && after.NumGC > before.NumGC
}

// Ring has the app log's row/byte bounds and retains only the newest rows.
// One publisher owns one app file; this is not a multi-process append channel.
type Ring struct {
	rows  [MaxRows]Sample
	start int
	count int
}

func (ring *Ring) Append(sample Sample) error {
	if _, err := ParseRow(sample.Row()); err != nil {
		return err
	}
	if ring.count != 0 && !consecutive(ring.rows[(ring.start+ring.count-1)%MaxRows], sample) {
		return fmt.Errorf("heap: discontinuous append")
	}
	if ring.count == MaxRows {
		ring.start = (ring.start + 1) % MaxRows
		ring.count--
	}
	ring.rows[(ring.start+ring.count)%MaxRows] = sample
	ring.count++
	return nil
}

func (ring *Ring) Bytes() []byte {
	var out strings.Builder
	for i := 0; i < ring.count; i++ {
		out.WriteString(ring.rows[(ring.start+i)%MaxRows].Row())
		out.WriteByte('\n')
	}
	return []byte(out.String())
}

type Joined struct {
	Sample
	Kernel vi.MemstatRecord
}

// Suspected requires a contiguous run of post-GC growth AND rising dynamic
// ownership. No HeapSys, GC target, static pages or receipt totals enter it.
func Suspected(series []Joined) bool {
	rises, start := 0, 0
	for i := 1; i < len(series); i++ {
		before, after := series[i-1], series[i]
		if !consecutive(before.Sample, after.Sample) ||
			after.LiveBytes <= before.LiveBytes ||
			after.Kernel.PID != after.PID || before.Kernel.PID != before.PID ||
			after.Kernel.Flags != 0 || before.Kernel.Flags != 0 ||
			after.Kernel.LivePages < before.Kernel.LivePages {
			rises, start = 0, i
			continue
		}
		rises++
		if rises >= LeakRises && after.LiveBytes-series[start].LiveBytes >= LeakGrowth &&
			after.Kernel.LivePages > series[start].Kernel.LivePages {
			return true
		}
	}
	return false
}
