//go:build virelai

package main

import (
	"runtime"
	"virelai/svgproof"
	"virelai/vector"
	"virelai/vi"
)

func run(a []byte, mode string) bool {
	v := svgproof.View(a)
	stage := a[svgproof.ScratchEnd : svgproof.ScratchEnd+2048]
	r := receipt{buf: a[svgproof.ScratchEnd+2048 : svgproof.StagingEnd]}
	r.text("schema\t1\narena\t8388608\t2048\t1\n")
	success := true
	switch mode {
	case "icons":
		for _, name := range [...]string{"rect", "ellipse", "polygon", "lines", "quadratic", "cubic", "nonzero", "evenodd", "affine", "group", "viewport", "alpha", "viewport-meet"} {
			if !renderSVG(v, a, name, 21, 100_000_000, stage, &r) {
				return false
			}
		}
	case "maximum":
		for _, name := range [...]string{"max-fill", "max-curves"} {
			if !renderSVG(v, a, name, 1, 5_000_000_000, stage, &r) {
				return false
			}
		}
	case "reuse":
		r.buf = r.buf[:320*1024-2048]
		success = negativeSuite(v, a, stage, &r)
	default:
		return false
	}
	closed := write(mode+".tsv", r.buf[:r.n], stage)
	return closed && success
}

func renderSVG(v svgproof.Buffers, a []byte, name string, samples int, ceiling int64, stage []byte, r *receipt) bool {
	n, ok := read(name+".svg", v.Source, stage)
	if !ok || n > len(v.Source) {
		vi.Console("svg-proof: source transport failed\n")
		return false
	}
	immutable := hash(v.Source[:n])
	vi.Console("svg-proof: render ")
	vi.Console(name)
	vi.Console("\n")
	var before, after runtime.MemStats
	w, h := 0, 0
	runtime.ReadMemStats(&before)
	for i := 0; i < samples; i++ {
		b := vector.Budget{Max: vector.MaxWork}
		start := vi.Nanos()
		s, c, stats, f := v.Render(n, 0, &b)
		elapsed := vi.Nanos() - start
		if f.Code != vector.OK || elapsed < 0 || elapsed > ceiling ||
			hash(v.Source[:n]) != immutable {
			vi.Console("svg-proof: render admission/budget failed\n")
			var bytes [160]byte
			d := receipt{buf: bytes[:]}
			d.text("svg-proof: failure code/ns\t")
			d.number(uint64(f.Code))
			d.field(uint64(elapsed))
			d.text("\n")
			vi.Console(string(d.buf[:d.n]))
			return false
		}
		if samples == 21 && (len(s.Commands) > 128 || len(s.Paints) > 16 || stats.Edges > 512 || n > 16384) {
			vi.Console("svg-proof: icon complexity failed\n")
			return false
		}
		w, h = c.Width, c.Height
		r.text("render\t")
		r.text(name)
		r.field(uint64(i))
		r.field(uint64(elapsed))
		r.field(b.Used)
		r.field(stats.Work)
		r.field(stats.Edges)
		r.field(stats.ScratchBytes)
		// An aggregate zero delta below proves every call allocated zero:
		// Mallocs is cumulative, not net live heap. Avoid a stop-the-world
		// ReadMemStats diagnostic between every pair of tiny icon calls.
		r.field(0)
		r.field(uint64(w))
		r.field(uint64(h))
		r.text("\n")
	}
	runtime.ReadMemStats(&after)
	r.text("allocations\t")
	r.text(name)
	r.field(after.Mallocs - before.Mallocs)
	r.field(uint64(samples))
	r.text("\n")
	if after.Mallocs != before.Mallocs {
		vi.Console("svg-proof: engine calls allocated\n")
		return false
	}
	ok = write(name+".bgra", a[svgproof.SourceEnd:svgproof.SourceEnd+w*h*4], stage)
	if !ok {
		vi.Console("svg-proof: bitmap transport failed\n")
	}
	return ok
}

func negativeSuite(v svgproof.Buffers, a, stage []byte, r *receipt) bool {
	// The negative index is bounded, resides in charged staging, and is not a
	// Go string copy of the source. Every row is consumed exactly once.
	index := a[svgproof.ScratchEnd+320*1024 : svgproof.StagingEnd]
	n, ok := read("negative.tsv", index, stage)
	if !ok || n > len(index) {
		return false
	}
	pos, rows := 0, 0
	success := true
	for pos < n {
		start := pos
		for pos < n && index[pos] != '\t' {
			pos++
		}
		if pos == n {
			return false
		}
		// Transport filename is at most eight bytes, outside timed calls.
		name := string(index[start:pos])
		vi.Console("svg-proof: negative ")
		vi.Console(name)
		vi.Console("\n")
		pos++
		want := 0
		for pos < n && index[pos] != '\t' {
			if index[pos] < '0' || index[pos] > '9' {
				return false
			}
			want = want*10 + int(index[pos]-'0')
			pos++
		}
		for pos < n && index[pos] != '\n' {
			pos++
		}
		pos++
		size, ok := read(name, v.Source, stage)
		if !ok {
			return false
		}
		for i := range v.Pixels {
			v.Pixels[i] = 0x71345678
		}
		b := vector.Budget{Max: vector.MaxWork}
		s, c, _, f := v.Render(size, 0, &b)
		if int(f.Code) != want {
			success = false
		}
		if want != 0 {
			if len(s.Commands) != 0 || len(s.Paints) != 0 || c.Width != 0 || c.Height != 0 {
				return false
			}
			for _, p := range v.Pixels {
				if p != 0x71345678 {
					return false
				}
			}
		}
		r.text("negative\t")
		r.text(name)
		r.field(uint64(want))
		r.field(uint64(f.Code))
		r.field(b.Used)
		r.text("\n")
		rows++
	}
	r.text("negative-count")
	r.field(uint64(rows))
	r.text("\n")
	// Explicit collector cycles observe retained-runtime behavior as well as
	// zero allocations in engine calls. Source transport is not timed.
	var before, after runtime.MemStats
	for cycle := 0; cycle < 100; cycle++ {
		vi.Console("svg-proof: reuse cycle\n")
		bad := `<svg width="64" height="64"><g fill-opacity="0"><text/></g></svg>`
		copy(v.Source, bad)
		if cycle%10 == 0 {
			runtime.ReadMemStats(&before)
		}
		b := vector.Budget{Max: vector.MaxWork}
		_, _, _, f := v.Render(len(bad), 0, &b)
		if f.Code != vector.UnsupportedText {
			return false
		}
		good := `<svg width="64" height="64"><rect width="64" height="64" fill="red"/></svg>`
		copy(v.Source, good)
		b = vector.Budget{Max: vector.MaxWork}
		start := vi.Nanos()
		_, _, _, f = v.Render(len(good), 0, &b)
		elapsed := vi.Nanos() - start
		if f.Code != vector.OK || v.Pixels[4095] != 0xffff0000 || elapsed < 0 || elapsed > 100_000_000 {
			return false
		}
		r.text("cycle")
		r.field(uint64(cycle))
		r.field(uint64(elapsed))
		r.field(b.Used)
		r.field(0)
		r.text("\n")
		if cycle%10 == 9 {
			runtime.ReadMemStats(&after)
			r.text("allocation-batch")
			r.field(uint64(cycle))
			r.field(after.Mallocs - before.Mallocs)
			r.text("\n")
			if after.Mallocs != before.Mallocs {
				return false
			}
			runtime.GC()
			r.text("gc")
			r.field(uint64(cycle))
			r.text("\n")
		}
	}
	return success
}
