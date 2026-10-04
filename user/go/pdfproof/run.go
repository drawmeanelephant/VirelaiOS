package pdfproof

import (
	"runtime"
	"unsafe"
	"virelai/pdf"
	"virelai/vi"
)

type Renderer func(pdf.Source, int, []byte, *pdf.Ledger) (pdf.Page, pdf.Stats, pdf.Failure)

type caseRow struct {
	id, source, output string
	expected           pdf.Code
	page, repeats      int
}

type receipt struct {
	b []byte
	n int
	c pdf.Code
}

func (r *receipt) text(s string) {
	if r.c != pdf.OK {
		return
	}
	if len(s) > len(r.b)-r.n {
		r.c = pdf.ReceiptLimit
		return
	}
	r.n += copy(r.b[r.n:], s)
}
func (r *receipt) number(n uint64) {
	var b [20]byte
	i := len(b)
	for {
		i--
		b[i] = '0' + byte(n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	r.text(unsafe.String(&b[i], len(b)-i))
}

func integer(s string) (int, bool) {
	n := 0
	if len(s) == 0 || len(s) > 9 {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func parsePlan(b []byte, rows *[128]caseRow) (int, pdf.Code) {
	text := unsafe.String(unsafe.SliceData(b), len(b))
	count, start := 0, 0
	for start < len(text) {
		if count == len(rows) {
			return 0, pdf.ReceiptLimit
		}
		var fields [6]string
		f, begin, end := 0, start, start
		for end < len(text) && text[end] != '\n' {
			if text[end] == '\t' {
				if f >= 5 {
					return 0, pdf.InvalidBuffer
				}
				fields[f] = text[begin:end]
				f++
				begin = end + 1
			}
			end++
		}
		if f != 5 || end == len(text) {
			return 0, pdf.InvalidBuffer
		}
		fields[5] = text[begin:end]
		page, a := integer(fields[3])
		code, c := integer(fields[4])
		repeats, d := integer(fields[5])
		if !a || !c || !d || code > int(pdf.SourceDrift) || repeats < 1 || repeats > 21 || len(fields[0]) > 64 ||
			len(fields[1]) > 256 || len(fields[2]) > 256 {
			return 0, pdf.InvalidBuffer
		}
		rows[count] = caseRow{fields[0], fields[1], fields[2], pdf.Code(code), page, repeats}
		count++
		start = end + 1
	}
	if count == 0 {
		return 0, pdf.InvalidBuffer
	}
	return count, pdf.OK
}

func loadPlan(path string, b []byte, l *pdf.Ledger) (int, pdf.Code) {
	if c := l.Open(); c != pdf.OK {
		return 0, c
	}
	h, rc := vi.FileOpen(path, vi.ModeRead)
	if rc < 0 {
		_ = l.Close()
		return 0, pdf.ReadFailed
	}
	total, c := 0, pdf.OK
	for total < len(b) {
		take := min(chunk, len(b)-total)
		if c = l.Read(uint64(take)); c != pdf.OK {
			break
		}
		n, rc := vi.FileRead(uint32(h), b[total:total+take])
		if rc < 0 || n < 0 || n > take {
			c = pdf.ReadFailed
			break
		}
		if n == 0 {
			break
		}
		total += n
	}
	if total == len(b) {
		c = pdf.ReceiptLimit
	}
	vi.FileClose(uint32(h))
	if closed := l.Close(); c == pdf.OK {
		c = closed
	}
	return total, c
}

func now() uint64 { return uint64(vi.Nanos()) }

// Run is a no-UI suite driver. Expected refusals are test outcomes, not pages;
// an unexpected refusal, missing output or transport failure fails the suite.
// Every transaction, including a refusal, has a fresh cumulative ledger.
func Run(render Renderer) pdf.Code {
	args := vi.Args()
	if len(args) != 4 {
		return pdf.InvalidBuffer
	}
	rounds, ok := integer(args[3])
	if !ok || (rounds != 1 && rounds != 100) {
		return pdf.InvalidBuffer
	}
	arena, err := vi.MmapHint(0x80000000, pdf.ArenaBytes, vi.ProtRead|vi.ProtWrite,
		vi.MapAnonymous|vi.MapPrivate|vi.MapPopulate)
	if err != nil || len(arena) != pdf.ArenaBytes {
		return pdf.MemoryLimit
	}
	// Only the unassigned, charged arena margin is used for native I/O, the
	// bounded plan and receipts. Engine partitions and its source cache are
	// never borrowed for the adapter's state.
	plan := arena[pdf.ArenaBytes-65536 : pdf.ArenaBytes-49152]
	out := receipt{b: arena[pdf.ArenaBytes-49152 : pdf.ArenaBytes-32768]}
	stage := arena[pdf.ArenaBytes-chunk:]
	l := pdf.Ledger{Max: pdf.MaxWork}
	n, code := loadPlan(args[1], plan, &l)
	if code != pdf.OK {
		return code
	}
	var rows [128]caseRow
	count, code := parsePlan(plan[:n], &rows)
	if code != pdf.OK {
		return code
	}
	s := NewSource("", stage)
	// Warm GC is part of the runtime proof; it never removes charged backing.
	runtime.GC()
	for round := 0; round < rounds; round++ {
		for i := 0; i < count; i++ {
			row := rows[i]
			var maxNs, maxWork, maxReads, maxRewinds, maxExpanded, maxVector uint64
			for repeat := 0; repeat < row.repeats; repeat++ {
				l = pdf.Ledger{Max: pdf.MaxWork}
				if code = s.SetPath(row.source); code != pdf.OK {
					return code
				}
				code = s.Admit(&l)
				start := now()
				l.Now, l.Deadline = now, start+pdf.MaxNanos
				var p pdf.Page
				var stats pdf.Stats
				if code == pdf.OK {
					var failure pdf.Failure
					p, stats, failure = render(&s, row.page, arena, &l)
					code = failure.Code
					if code == pdf.OK {
						code = s.Recheck(&l)
					}
				}
				if closed := s.Close(&l); code == pdf.OK {
					code = closed
				}
				elapsed := now() - start
				if elapsed > pdf.MaxNanos {
					code = pdf.TimeLimit
				}
				if code != row.expected || l.Handles != 0 || l.Decoders != 0 {
					return codeOrInvalid(code)
				}
				// Output serialization is not timed, but is still charged.
				l.Now = nil
				// A row is at most 64 ID bytes + twelve 20-digit integers
				// + separators. Reserve formatting/copy work conservatively,
				// including the bounded footer, before constructing receipts.
				if c := l.Charge(pdf.Record, 512); c != pdf.OK {
					return c
				}
				if c := l.Charge(pdf.Copy, 512); c != pdf.OK {
					return c
				}
				if code == pdf.OK && repeat == 0 && round == rounds-1 && row.output != "-" {
					if code = Publish(row.output, p, stage, &l); code != pdf.OK {
						return code
					}
				}
				maxNs, maxWork = max(maxNs, elapsed), max(maxWork, l.Used)
				maxReads, maxRewinds = max(maxReads, l.ReadBytes), max(maxRewinds, l.Rewinds)
				maxExpanded, maxVector = max(maxExpanded, l.Expanded), max(maxVector, l.VectorBudget.Used)
				if stats.ArenaBytes != pdf.ArenaBytes && row.expected == pdf.OK {
					return pdf.MemoryLimit
				}
			}
			if round == rounds-1 {
				out.text(row.id)
				out.text("\t")
				out.number(uint64(row.expected))
				out.text("\t")
				out.number(uint64(row.repeats))
				out.text("\t")
				out.number(maxNs)
				out.text("\t")
				out.number(maxWork)
				out.text("\t")
				out.number(maxReads)
				out.text("\t")
				out.number(maxRewinds)
				out.text("\t")
				out.number(maxExpanded)
				out.text("\t")
				out.number(maxVector)
				out.text("\t2304\t1\t0\t0\n")
			}
		}
		runtime.GC()
		if rounds == 100 && round == 0 {
			vi.Console("pdf-proof: baseline\n")
			vi.Sleep(2)
		}
	}
	if out.c != pdf.OK {
		return out.c
	}
	out.text("cycles\t")
	out.number(uint64(rounds))
	out.text("\n")
	if out.c != pdf.OK {
		return out.c
	}
	l.Now = nil
	if code = WriteReceipt(args[2], out.b[:out.n], &l); code != pdf.OK {
		return code
	}
	runtime.KeepAlive(arena)
	// The only successful completion marker follows all writes, syncs and
	// closes. Kernel teardown (not Munmap) owns arena reclamation.
	vi.Console("pdf-proof: complete\n")
	return pdf.OK
}

func codeOrInvalid(code pdf.Code) pdf.Code {
	if code == pdf.OK {
		return pdf.InvalidBuffer
	}
	return code
}
