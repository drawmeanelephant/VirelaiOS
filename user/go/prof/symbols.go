// Package prof captures and renders ADR 0043 samples without changing the
// target runtime. debug/elf is the existing Go standard library (BSD-3-Clause),
// chosen instead of duplicating its bounded section and symbol-table parsing.
package prof

import (
	"debug/elf"
	"fmt"
	"io"
	"sort"
	"strings"

	"virelai/vi"
)

type Symbol struct {
	Start, Size uint64
	Name        string
}

type Symbols struct{ Functions []Symbol }

func ReadSymbols(input io.ReaderAt) (*Symbols, error) {
	file, err := elf.NewFile(input)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if file.Class != elf.ELFCLASS64 || file.Data != elf.ELFDATA2LSB ||
		file.Machine != elf.EM_AARCH64 || file.Type != elf.ET_EXEC {
		return nil, fmt.Errorf("prof: require a little-endian AArch64 executable")
	}
	symbols, err := file.Symbols()
	if err != nil {
		return nil, fmt.Errorf("prof: target requires .symtab: %w", err)
	}
	result := &Symbols{}
	for _, symbol := range symbols {
		if elf.ST_TYPE(symbol.Info) == elf.STT_FUNC && symbol.Section != elf.SHN_UNDEF &&
			symbol.Size != 0 && symbol.Value <= ^uint64(0)-symbol.Size {
			result.Functions = append(result.Functions, Symbol{symbol.Value, symbol.Size, symbol.Name})
		}
	}
	sort.Slice(result.Functions, func(i, j int) bool {
		return result.Functions[i].Start < result.Functions[j].Start
	})
	if len(result.Functions) == 0 {
		return nil, fmt.Errorf("prof: no function symbols")
	}
	return result, nil
}

func (s *Symbols) Resolve(pc uint64) (string, bool) {
	i := sort.Search(len(s.Functions), func(i int) bool { return s.Functions[i].Start > pc }) - 1
	if i >= 0 {
		f := s.Functions[i]
		if pc-f.Start < f.Size {
			return escapeFrame(f.Name), true
		}
	}
	return fmt.Sprintf("0x%x", pc), false
}

// Folded-stack delimiters and line breaks must never be supplied by an ELF.
func escapeFrame(name string) string {
	return strings.NewReplacer(";", "_", "\r", "_", "\n", "_", " ", "_").Replace(name)
}

type Count struct {
	Name    string
	Samples uint64
}

type Report struct {
	Samples, Symbolized, Dropped uint64
	Folded                       map[string]uint64
	Flat                         map[string]uint64
}

func NewReport() *Report {
	return &Report{Folded: make(map[string]uint64), Flat: make(map[string]uint64)}
}

func (r *Report) Add(symbols *Symbols, sample vi.SampleRecord) {
	pc, known := symbols.Resolve(sample.PC)
	r.Samples++
	if known {
		r.Symbolized++
	}
	r.Flat[pc]++
	stack := make([]string, 0, 1+sample.Depth)
	depth := int(sample.Depth)
	if depth > vi.ProfileFrameDepth {
		depth = vi.ProfileFrameDepth
	}
	for i := depth - 1; i >= 0; i-- {
		address := sample.Frames[i]
		if address == 0 || address == sample.PC {
			continue
		}
		// A caller LR points just after the call instruction.
		name, _ := symbols.Resolve(address - 1)
		stack = append(stack, name)
	}
	stack = append(stack, pc)
	r.Folded[strings.Join(stack, ";")]++
}

func ranked(counts map[string]uint64) []Count {
	rows := make([]Count, 0, len(counts))
	for name, count := range counts {
		rows = append(rows, Count{name, count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Samples != rows[j].Samples {
			return rows[i].Samples > rows[j].Samples
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

func (r *Report) Top(n int) []Count {
	rows := ranked(r.Flat)
	if n < len(rows) {
		rows = rows[:n]
	}
	return rows
}

func (r *Report) Percent() float64 {
	if r.Samples == 0 {
		return 0
	}
	return 100 * float64(r.Symbolized) / float64(r.Samples)
}

func (r *Report) WriteFolded(out io.Writer) error {
	names := make([]string, 0, len(r.Folded))
	for name := range r.Folded {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := fmt.Fprintf(out, "%s %d\n", name, r.Folded[name]); err != nil {
			return err
		}
	}
	return nil
}

func (r *Report) WriteTop(out io.Writer) error {
	for i, row := range r.Top(10) {
		if _, err := fmt.Fprintf(out, "prof: top %d samples=%d %s\n", i+1, row.Samples, row.Name); err != nil {
			return err
		}
	}
	return nil
}
