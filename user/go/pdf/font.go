package pdf

import (
	"unsafe"
	"virelai/vector"
)

func contentStateBytes() int { return int(unsafe.Sizeof(contentState{})) }

type fontDesc struct {
	box                [4]float64
	matrix             vector.Affine
	procs, differences value
	widths             [256]float64
	first, last        int
}

func (e *engine) numbers(v value, n int, out []float64) {
	v = e.resolve(v)
	if v.k != kArray || int(v.count) != n {
		e.set(Malformed)
		return
	}
	i := e.iter(v)
	for j := 0; j < n; j++ {
		out[j] = e.number(i.next().val)
		if !finite(out[j]) {
			e.set(CoordinateLimit)
		}
	}
}
func (e *engine) font(n int) fontDesc {
	var f fontDesc
	v, _, _, s := e.objectValue(n)
	if s {
		e.set(UnsupportedFont)
	}
	e.allowed(v, "Type Subtype FontBBox FontMatrix CharProcs Encoding FirstChar LastChar Widths Resources Name", UnsupportedFont)
	if !e.named(e.get(v, "Type"), "Font") || !e.named(e.get(v, "Subtype"), "Type3") {
		e.set(UnsupportedFont)
	}
	e.numbers(e.get(v, "FontBBox"), 4, f.box[:])
	if f.box[0] > f.box[2] || f.box[1] > f.box[3] {
		e.set(Malformed)
	}
	var a [6]float64
	e.numbers(e.get(v, "FontMatrix"), 6, a[:])
	f.matrix = vector.Affine{A: a[0], B: a[1], C: a[2], D: a[3], E: a[4], F: a[5]}
	e.affine(f.matrix)
	f.first = e.integer(e.get(v, "FirstChar"))
	f.last = e.integer(e.get(v, "LastChar"))
	if f.first < 0 || f.last > 255 || f.first > f.last {
		e.set(Malformed)
	}
	widths := e.resolve(e.get(v, "Widths"))
	if widths.k != kArray || int(widths.count) != f.last-f.first+1 {
		e.set(Malformed)
	}
	enc := e.resolve(e.get(v, "Encoding"))
	e.allowed(enc, "Type Differences", UnsupportedFont)
	if ty := e.get(enc, "Type"); ty.k != kBad && !e.named(ty, "Encoding") {
		e.set(Malformed)
	}
	f.differences = e.resolve(e.get(enc, "Differences"))
	if f.differences.k != kArray {
		e.set(Malformed)
	}
	f.procs = e.resolve(e.get(v, "CharProcs"))
	if f.procs.k != kDict {
		e.set(Malformed)
	}
	if f.procs.count > 256 {
		e.set(FontLimit)
	}
	if r := e.get(v, "Resources"); r.k != kBad {
		r = e.resolve(r)
		if r.k != kDict || r.count != 0 {
			e.set(UnsupportedGlyph)
		}
	}
	if name := e.get(v, "Name"); name.k != kBad {
		if name.k != kName {
			e.set(Malformed)
		} else if e.x[n].generation&1 == 0 {
			e.ignoreMetadata(name.count)
			e.x[n].generation |= 1
		}
	}
	// Collect the bounded array before following indirect scalar widths.
	// Interleaving those reads with the font iterator would reopen a native
	// forward source for every entry when the values lie beyond its window.
	if widths.k == kArray && e.f.Code == OK {
		var values [256]value
		i := e.iter(widths)
		for j := 0; j < int(widths.count) && e.f.Code == OK; j++ {
			if !e.charge(Record, 1) {
				return f
			}
			values[j] = i.next().val
		}
		for j := 0; j < int(widths.count) && e.f.Code == OK; j++ {
			w := e.number(e.resolve(values[j]))
			if w < 0 {
				e.set(Malformed)
			}
			if !finite(w) {
				e.set(CoordinateLimit)
			}
			if e.charge(Record, 1) {
				f.widths[j] = w
			}
		}
	}
	return f
}
func (e *engine) proc(f fontDesc, name value) int {
	if name.k != kName {
		e.set(Malformed)
		return 0
	}
	c := cursor{e, int(name.start), int(name.end)}
	var want [64]byte
	n := c.name(&want)
	i := e.iter(f.procs)
	for i.remaining > 0 && e.f.Code == OK {
		p := i.next()
		c = cursor{e, int(p.key.start), int(p.key.end)}
		var other [64]byte
		m := c.name(&other)
		if m == n && other == want {
			return e.ref(p.val)
		}
	}
	e.set(MissingGlyph)
	return 0
}
func (e *engine) encoding(f fontDesc, code int) value {
	i := e.iter(f.differences)
	at := -1
	var out value
	for i.remaining > 0 && e.f.Code == OK {
		v := i.next().val
		if v.k == kNumber {
			at = e.integer(v)
		} else if v.k == kName {
			if at == code {
				out = v
			}
			at++
		} else {
			e.set(Malformed)
		}
	}
	return out
}
func (e *engine) width(f fontDesc, code int) float64 {
	if code < f.first || code > f.last {
		e.set(MissingGlyph)
		return 0
	}
	e.charge(Record, 1)
	return f.widths[code-f.first]
}
func (e *engine) validateFont(n int) {
	f := e.font(n)
	if e.f.Code != OK {
		return
	}
	var assigned [256]bool
	i := e.iter(f.differences)
	at := -1
	needsName := false
	for i.remaining > 0 && e.f.Code == OK {
		v := i.next().val
		if v.k == kNumber {
			if needsName {
				e.set(Malformed)
			}
			at = e.integer(v)
			if at < 0 || at > 255 {
				e.set(Malformed)
			}
			needsName = true
		} else if v.k == kName {
			if at < 0 || at > 255 || assigned[at] {
				e.set(Malformed)
				break
			}
			e.charge(Record, 1)
			assigned[at] = true
			at++
			needsName = false
			e.proc(f, v)
		} else {
			e.set(Malformed)
		}
	}
	if needsName {
		e.set(Malformed)
	}
	i = e.iter(f.procs)
	for i.remaining > 0 && e.f.Code == OK {
		p := i.next()
		proc := e.ref(p.val)
		e.mark(proc, roleGlyph)
		wx := e.glyph(proc, f, identity, false)
		e.flush()
		// Every width mapped to this glyph must agree, not just used codes.
		for code := f.first; code <= f.last && e.f.Code == OK; code++ {
			name := e.encoding(f, code)
			if name.k != kBad && e.proc(f, name) == proc && e.width(f, code) != wx {
				e.set(Malformed)
			}
		}
	}
}
func (e *engine) glyph(n int, f fontDesc, mapping vector.Affine, paint bool) float64 {
	s := e.state
	if !e.charge(Copy, uint64(contentStateBytes())) {
		return 0
	}
	backup := *s
	base := int(backup.depth) + int(backup.outerDepth) + 1
	if base > 16 {
		e.set(GraphicsDepthLimit)
		return 0
	}
	e.charge(Glyph, 1)
	e.charge(Stack, 1)
	if !e.charge(Zero, uint64(contentStateBytes())) {
		return 0
	}
	// The isolated program shares batches/cumulative counters, never page
	// paths, operands, text state, clip state or graphics frames.
	*s = contentState{
		g:     graphics{ctm: identity, clip: backup.g.clip, color: backup.g.color, tz: 1},
		glyph: true, outerDepth: uint8(base), fontBox: f.box, mapPage: mapping,
		pathStart: backup.pathStart + backup.pathN, batchC: backup.batchC, batchP: backup.batchP, page: backup.page,
	}
	if !paint {
		// Unused outlines are still passed through vector's validation. A
		// zero clip avoids pixel work, without bypassing geometry expansion.
		s.g.clip = vector.Rect{}
	}
	d, _, _, stream := e.objectValue(n)
	if !stream {
		e.set(Malformed)
	}
	e.allowed(d, "Length Filter DecodeParms", UnsupportedGlyph)
	p := program{e: e, n: 1, slot: 1}
	p.objects[0] = n
	e.program(&p)
	wx := s.wx
	backup.batchC = s.batchC
	backup.batchP = s.batchP
	e.charge(Copy, uint64(contentStateBytes()))
	*s = backup
	return wx
}
func (e *engine) nextLine() {
	s := e.state
	s.lm = e.multiply(s.lm, vector.Affine{A: 1, D: 1, F: -s.g.leading})
	s.tm = s.lm
}
func (e *engine) show(start, count uint16) {
	s := e.state
	if s.g.font <= 0 {
		e.set(MissingResource)
		return
	}
	f := e.font(int(s.g.font))
	for i := int(start); i < int(start)+int(count) && e.f.Code == OK; i++ {
		e.charge(Examine, 1)
		code := int(s.chars[i])
		name := e.encoding(f, code)
		if name.k == kBad {
			e.set(MissingGlyph)
			return
		}
		n := e.proc(f, name)
		w := e.width(f, code)
		tr := vector.Affine{A: s.g.size * s.g.tz, D: s.g.size, F: s.g.rise}
		mapping := e.multiply(s.mapPage, e.multiply(s.g.ctm, e.multiply(s.tm, e.multiply(tr, f.matrix))))
		wx := e.glyph(n, f, mapping, true)
		if wx != w {
			e.set(Malformed)
		}
		delta := (w*f.matrix.A*s.g.size + s.g.tc) * s.g.tz
		if code == 32 {
			delta += s.g.tw * s.g.tz
		}
		if !finite(delta) {
			e.set(CoordinateLimit)
		}
		s.tm = e.multiply(s.tm, vector.Affine{A: 1, D: 1, E: delta})
	}
}
func (e *engine) textOperator(op string) {
	s := e.state
	if !s.bt && op != "Tf" {
		e.set(MalformedContent)
		return
	}
	switch op {
	case "Tf":
		if !e.arity(2, "n#") {
			return
		}
		if s.ops[1].num < 0 || s.ops[1].num > 256 {
			e.set(UnsupportedText)
			return
		}
		s.g.font = int32(e.resource(s.page.resources, "Font", &s.ops[0].name, int(s.ops[0].count)))
		s.g.size = s.ops[1].num
	case "Tm":
		if !e.arity(6, "######") {
			return
		}
		a := vector.Affine{A: s.ops[0].num, B: s.ops[1].num, C: s.ops[2].num, D: s.ops[3].num, E: s.ops[4].num, F: s.ops[5].num}
		e.affine(a)
		s.tm = a
		s.lm = a
	case "Td", "TD":
		if !e.arity(2, "##") {
			return
		}
		if op == "TD" {
			s.g.leading = -s.ops[1].num
		}
		s.lm = e.multiply(s.lm, vector.Affine{A: 1, D: 1, E: s.ops[0].num, F: s.ops[1].num})
		s.tm = s.lm
	case "T*":
		if !e.arity(0, "") {
			return
		}
		e.nextLine()
	case "Tj", "'":
		if !e.arity(1, "s") {
			return
		}
		if op == "'" {
			e.nextLine()
		}
		e.show(s.ops[0].start, s.ops[0].count)
	case "\"":
		if !e.arity(3, "##s") {
			return
		}
		if !finite(s.ops[0].num) || !finite(s.ops[1].num) {
			e.set(CoordinateLimit)
			return
		}
		s.g.tw = s.ops[0].num
		s.g.tc = s.ops[1].num
		e.nextLine()
		e.show(s.ops[2].start, s.ops[2].count)
	case "TJ":
		if !e.arity(1, "a") {
			return
		}
		for j := 0; j < int(s.itemN) && e.f.Code == OK; j++ {
			e.charge(Operand, 1)
			it := s.items[j]
			if it.text {
				e.show(it.start, it.count)
			} else {
				delta := -it.num / 1000 * s.g.size * s.g.tz
				if !finite(it.num) || !finite(delta) {
					e.set(CoordinateLimit)
				}
				s.tm = e.multiply(s.tm, vector.Affine{A: 1, D: 1, E: delta})
			}
		}
	}
}
