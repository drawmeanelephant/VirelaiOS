package prof

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"virelai/vi"
)

func FindTarget(selector string) (vi.ProcRow, error) {
	pid, numericErr := strconv.ParseUint(selector, 10, 64)
	var rows [16]vi.ProcRow
	n, raw := vi.Procs(rows[:])
	if raw < 0 {
		return vi.ProcRow{}, fmt.Errorf("prof: process snapshot %d", raw)
	}
	var found *vi.ProcRow
	for i := 0; i < n; i++ {
		row := &rows[i]
		if row.State != vi.ProcRunning {
			continue
		}
		if (numericErr == nil && row.PID == pid) ||
			(numericErr != nil && strings.EqualFold(selector, row.Name())) {
			if found != nil {
				return vi.ProcRow{}, fmt.Errorf("prof: ambiguous target %q, use pid", selector)
			}
			found = row
		}
	}
	if found == nil {
		return vi.ProcRow{}, fmt.Errorf("prof: target %q is not running", selector)
	}
	return *found, nil
}

func LoadSymbols(name string) (*Symbols, error) {
	if !strings.HasPrefix(name, "/host/") {
		name = "/host/" + name
	}
	handle, raw := vi.FileOpen(name, vi.ModeRead)
	if raw < 0 {
		return nil, fmt.Errorf("prof: open target native result %d", raw)
	}
	defer vi.FileClose(uint32(handle))
	// The stdlib port's positional shadow stops at 16 MiB. The guest compiler
	// is larger, so stream the exact ELF through the native cursor into one
	// bounded buffer, then give debug/elf an ordinary memory ReaderAt.
	const maxImageBytes = 32 << 20 // the kernel's exec_image_max
	body := make([]byte, maxImageBytes+1)
	used := 0
	for used < len(body) {
		n, result := vi.FileRead(uint32(handle), body[used:])
		if result < 0 {
			return nil, fmt.Errorf("prof: target read native result %d", result)
		}
		if n == 0 {
			break
		}
		if n < 0 || n > len(body)-used {
			return nil, fmt.Errorf("prof: invalid target read length")
		}
		used += n
	}
	if used > maxImageBytes {
		return nil, fmt.Errorf("prof: ELF exceeds loader file bound")
	}
	return ReadSymbols(bytes.NewReader(body[:used]))
}

func running(pid uint64) bool {
	var rows [16]vi.ProcRow
	n, raw := vi.Procs(rows[:])
	if raw < 0 {
		return false
	}
	for _, row := range rows[:n] {
		if row.PID == pid {
			return row.State == vi.ProcRunning
		}
	}
	return false
}

// Capture drains once per physical scheduler tick, never a busy poll.
// Each drain is bounded by the 1,024-record reservation.
func Capture(pid uint64, seconds int, symbols *Symbols) (*Report, error) {
	if seconds < 1 || seconds > 3600 {
		return nil, fmt.Errorf("prof: duration must be 1..3600 seconds")
	}
	token, err := vi.ProfileStart([]uint64{pid})
	if err != nil {
		return nil, err
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = vi.ProfileStop(token)
		}
	}()
	report := NewReport()
	drain := func() error {
		for batch := 0; batch < vi.ProfileRingRecords/16; batch++ {
			records, dropped, err := vi.ProfileSamples(token)
			if err != nil {
				return err
			}
			report.Dropped = dropped
			for _, record := range records {
				report.Add(symbols, record)
			}
			if len(records) < 16 {
				return nil
			}
		}
		return nil
	}
	start := vi.Nanos()
	for vi.Nanos()-start < int64(seconds)*1_000_000_000 && running(pid) {
		if err := drain(); err != nil {
			return nil, err
		}
		vi.Sleep(1)
	}
	if err := vi.ProfileStop(token); err != nil {
		return nil, err
	}
	stopped = true
	if err := drain(); err != nil {
		return nil, err
	}
	return report, nil
}

func Save(name string, report *Report, console io.Writer) error {
	name = filepath.Base(name)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("prof: invalid output name")
	}
	if err := os.MkdirAll("/host/PROF", 0755); err != nil {
		return err
	}
	for _, output := range []struct {
		suffix string
		write  func(io.Writer) error
	}{{".folded", report.WriteFolded}, {".top", report.WriteTop}} {
		file, err := os.Create("/host/PROF/" + name + output.suffix)
		if err != nil {
			return err
		}
		err = output.write(file)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	fmt.Fprintf(console, "prof: samples=%d symbolized_pct=%.2f dropped=%d\n",
		report.Samples, report.Percent(), report.Dropped)
	return report.WriteTop(console)
}
