// Package svg parses the closed SVG subset in ADR 0041. No XML DOM, source
// strings, retained source references, or allocation fallback are used.
package svg

import (
	"math"
	"unicode/utf8"
	"unsafe"
	"virelai/vector"
)

type Canvas struct{ Width, Height int }
type span struct{ lo, hi int }
type attr struct{ name, value span }
type frame struct {
	name      span
	kind      uint8
	transform vector.Affine
	mapping   vector.Affine // inherited local mapping, before viewport scale
	color     uint32
	alpha     uint8
	none      bool
	rule      vector.Rule
}
type parser struct {
	src                        []byte
	out                        vector.Storage
	b                          *vector.Budget
	pos, nc, np, nodes, attrs  int
	contours, transforms, arcs int
	depth                      int
	stack                      [32]frame
	canvas                     Canvas
	viewport                   vector.Affine
	f                          vector.Failure
	counts                     [4]uint64
	preview                    bool
}

const (
	byteWork = iota
	recordWork
	pointWork
	arcWork
)

func (p *parser) fail(c vector.Code, offset int) {
	if p.f.Code == vector.OK {
		p.f = vector.Failure{Code: c, Offset: offset, Paint: -1, Command: -1}
	}
}
func (p *parser) take(kind int, n uint64) bool {
	if p.f.Code != vector.OK {
		return false
	}
	if n > p.b.Max-p.b.Used {
		p.fail(vector.WorkLimit, p.pos)
		return false
	}
	p.b.Used += n
	p.counts[kind] += n
	return true
}
func (p *parser) at(i int) byte {
	if i >= len(p.src) || !p.take(byteWork, 1) {
		return 0
	}
	return p.src[i]
}
func white(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
func (p *parser) ws() bool {
	start := p.pos
	for p.pos < len(p.src) && white(p.at(p.pos)) && p.f.Code == vector.OK {
		p.pos++
	}
	return p.pos > start
}
func (p *parser) literal(s string) bool {
	for i := 0; i < len(s); i++ {
		if p.at(p.pos+i) != s[i] {
			return false
		}
	}
	return true
}
func (p *parser) equal(a span, s string) bool {
	if a.hi-a.lo != len(s) {
		return false
	}
	for i := range s {
		if p.at(a.lo+i) != s[i] {
			return false
		}
	}
	return true
}
func legal(r rune) bool {
	return r == 9 || r == 10 || r == 13 || r >= 0x20 && r <= 0xd7ff || r >= 0xe000 && r <= 0xfffd || r >= 0x10000 && r <= 0x10ffff
}
func nameStart(r rune) bool {
	return r == ':' || r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' ||
		r >= 0xc0 && r <= 0xd6 || r >= 0xd8 && r <= 0xf6 || r >= 0xf8 && r <= 0x2ff ||
		r >= 0x370 && r <= 0x37d || r >= 0x37f && r <= 0x1fff || r >= 0x200c && r <= 0x200d ||
		r >= 0x2070 && r <= 0x218f || r >= 0x2c00 && r <= 0x2fef || r >= 0x3001 && r <= 0xd7ff ||
		r >= 0xf900 && r <= 0xfdcf || r >= 0xfdf0 && r <= 0xfffd || r >= 0x10000 && r <= 0xeffff
}
func nameChar(r rune) bool {
	return nameStart(r) || r == '-' || r == '.' || r >= '0' && r <= '9' || r == 0xb7 || r >= 0x300 && r <= 0x36f || r >= 0x203f && r <= 0x2040
}
func (p *parser) runeAt(i, end int) (rune, int) {
	if i >= end {
		return 0, 0
	}
	c := p.at(i)
	if c < 128 {
		if !legal(rune(c)) {
			p.fail(vector.Malformed, i)
		}
		return rune(c), 1
	}
	n := 1
	for n < 4 && i+n < end && p.at(i+n)&0xc0 == 0x80 {
		n++
	}
	// Charge the decoder's byte examination too.
	if !p.take(byteWork, uint64(n)) {
		return 0, n
	}
	r, size := utf8.DecodeRune(p.src[i : i+n])
	if size != n || r == utf8.RuneError && n == 1 || !legal(r) {
		p.fail(vector.Malformed, i)
	}
	return r, n
}
func (p *parser) name() span {
	s := span{lo: p.pos}
	first := true
	for p.pos < len(p.src) && p.f.Code == vector.OK {
		c := p.at(p.pos)
		if c < 128 && !(nameChar(rune(c))) {
			break
		}
		r, n := p.runeAt(p.pos, len(p.src))
		if first && !nameStart(r) || !nameChar(r) {
			p.fail(vector.Malformed, p.pos)
			break
		}
		first = false
		p.pos += n
		if p.pos-s.lo > 64 {
			p.fail(vector.TokenLimit, s.lo)
		}
	}
	s.hi = p.pos
	if first {
		p.fail(vector.Malformed, p.pos)
	}
	return s
}

// reader decodes XML character references exactly once, without a decoded
// attribute copy. All rescans, numeric/entity handling and UTF-8 bytes count.
type reader struct {
	p *parser
	s span
	i int
}

func (p *parser) reader(s span) reader { return reader{p, s, s.lo} }
func (r *reader) next() (rune, bool) {
	if r.i >= r.s.hi || r.p.f.Code != vector.OK {
		return 0, false
	}
	p, start := r.p, r.i
	if p.at(r.i) != '&' {
		v, n := p.runeAt(r.i, r.s.hi)
		r.i += n
		return v, p.f.Code == vector.OK
	}
	r.i++
	if r.i < r.s.hi && p.at(r.i) == '#' {
		r.i++
		base := uint32(10)
		if r.i < r.s.hi && p.at(r.i) == 'x' {
			base = 16
			r.i++
		}
		v, digits := uint32(0), 0
		for r.i < r.s.hi && p.f.Code == vector.OK {
			c := p.at(r.i)
			r.i++
			if c == ';' {
				if digits > 0 && legal(rune(v)) {
					return rune(v), true
				}
				break
			}
			if !p.take(byteWork, 1) {
				return 0, false
			}
			d := hex(c)
			if d < 0 || uint32(d) >= base || v > (0x10ffff-uint32(d))/base {
				break
			}
			v = v*base + uint32(d)
			digits++
		}
		p.fail(vector.Malformed, start)
		return 0, false
	}
	var buf [5]byte
	n := 0
	for r.i < r.s.hi && p.f.Code == vector.OK {
		c := p.at(r.i)
		r.i++
		if c == ';' {
			if n == 0 {
				break
			}
			if !p.take(byteWork, uint64(n)) {
				return 0, false
			}
			switch string(buf[:n]) {
			case "amp":
				return '&', true
			case "lt":
				return '<', true
			case "gt":
				return '>', true
			case "quot":
				return '"', true
			case "apos":
				return '\'', true
			}
			break
		}
		if n == len(buf) {
			break
		}
		if !p.take(byteWork, 1) {
			return 0, false
		}
		buf[n], n = c, n+1
	}
	p.fail(vector.Malformed, start)
	return 0, false
}
func hex(c byte) int {
	if c >= '0' && c <= '9' {
		return int(c - '0')
	}
	if c >= 'a' && c <= 'f' {
		return int(c-'a') + 10
	}
	if c >= 'A' && c <= 'F' {
		return int(c-'A') + 10
	}
	return -1
}
func (p *parser) value(s span, want string) bool {
	r := p.reader(s)
	for _, c := range want {
		v, ok := r.next()
		if !ok || v != c {
			return false
		}
	}
	_, more := r.next()
	return !more && p.f.Code == vector.OK
}
func (p *parser) text(s span, limit int) {
	r := p.reader(s)
	n := 0
	for {
		v, ok := r.next()
		if !ok {
			break
		}
		n += utf8.RuneLen(v)
		if limit > 0 && n > limit {
			p.fail(vector.TokenLimit, s.lo)
			break
		}
	}
}

func Parse(src []byte, out vector.Storage, b *vector.Budget) (vector.Scene, Canvas, vector.Failure) {
	p := parser{src: src, out: out, b: b, f: vector.Failure{Offset: -1, Paint: -1, Command: -1}}
	if len(src) > 1_048_576 {
		p.fail(vector.SourceLimit, 1_048_576)
	} else if b == nil || b.Used > b.Max || b.Max > vector.MaxWork {
		p.fail(vector.WorkLimit, -1)
	} else if uint64(len(out.Commands))*uint64(unsafe.Sizeof(vector.Command{}))+uint64(len(out.Paints))*uint64(unsafe.Sizeof(vector.Paint{})) > vector.MaxScene {
		p.fail(vector.SceneLimit, -1)
	} else {
		p.document()
	}
	if p.f.Code != vector.OK {
		return vector.Scene{}, Canvas{}, p.f
	}
	return vector.Scene{Commands: out.Commands[:p.nc], Paints: out.Paints[:p.np]}, p.canvas, p.f
}

func (p *parser) comment() {
	p.pos += 4
	for p.pos < len(p.src) && p.f.Code == vector.OK {
		if p.literal("--") {
			if !p.literal("-->") {
				p.fail(vector.Malformed, p.pos)
			} else {
				p.pos += 3
			}
			return
		}
		_, n := p.runeAt(p.pos, len(p.src))
		p.pos += n
	}
	p.fail(vector.Malformed, p.pos)
}
func (p *parser) attributes(endPI bool, kind string) ([16]attr, int, bool) {
	var a [16]attr
	n := 0
	for p.f.Code == vector.OK {
		separated := p.ws()
		if endPI && p.literal("?>") {
			p.pos += 2
			return a, n, false
		}
		if !endPI && p.literal("/>") {
			p.pos += 2
			return a, n, true
		}
		if !endPI && p.at(p.pos) == '>' {
			p.pos++
			return a, n, false
		}
		if !separated {
			p.fail(vector.Malformed, p.pos)
			break
		}
		if n == 16 || p.attrs == 32768 {
			p.fail(vector.AttributeLimit, p.pos)
			break
		}
		name := p.name()
		for i := 0; i < n; i++ {
			if p.same(name, a[i].name) {
				p.fail(vector.Malformed, name.lo)
			}
		}
		p.ws()
		if p.at(p.pos) != '=' {
			p.fail(vector.Malformed, p.pos)
			break
		}
		p.pos++
		p.ws()
		q := p.at(p.pos)
		if q != '"' && q != '\'' {
			p.fail(vector.Malformed, p.pos)
			break
		}
		p.pos++
		value := span{lo: p.pos}
		for p.pos < len(p.src) && p.f.Code == vector.OK && p.at(p.pos) != q {
			if p.at(p.pos) == '<' {
				p.fail(vector.Malformed, p.pos)
				break
			}
			_, size := p.runeAt(p.pos, len(p.src))
			p.pos += size
		}
		value.hi = p.pos
		if p.at(p.pos) != q {
			p.fail(vector.Malformed, p.pos)
			break
		}
		p.pos++
		p.text(value, 0)
		if !endPI {
			p.validateAttribute(kind, attr{name, value})
		}
		if !p.take(recordWork, 1) {
			break
		}
		a[n], n, p.attrs = attr{name, value}, n+1, p.attrs+1
	}
	return a, n, false
}
func (p *parser) same(a, b span) bool {
	if a.hi-a.lo != b.hi-b.lo {
		return false
	}
	for i := 0; i < a.hi-a.lo; i++ {
		if p.at(a.lo+i) != p.at(b.lo+i) {
			return false
		}
	}
	return true
}
func (p *parser) declaration() {
	p.pos += 5
	a, n, _ := p.attributes(true, "")
	if n < 1 || n > 3 || !p.equal(a[0].name, "version") || !p.value(a[0].value, "1.0") {
		p.fail(vector.UnsupportedXML, 0)
		return
	}
	i := 1
	if i < n && p.equal(a[i].name, "encoding") {
		if !p.value(a[i].value, "UTF-8") {
			p.fail(vector.UnsupportedXML, a[i].value.lo)
		}
		i++
	}
	if i < n && p.equal(a[i].name, "standalone") {
		if !p.value(a[i].value, "yes") && !p.value(a[i].value, "no") {
			p.fail(vector.UnsupportedXML, a[i].value.lo)
		}
		i++
	}
	if i != n {
		p.fail(vector.UnsupportedXML, a[i].name.lo)
	}
}

func (p *parser) document() {
	if p.literal("\xef\xbb\xbf") {
		p.pos += 3
	}
	if p.literal("<?xml") {
		p.declaration()
	}
	root, finished := false, false
	for p.pos < len(p.src) && p.f.Code == vector.OK {
		if p.literal("<!--") {
			p.comment()
			continue
		}
		if p.literal("<!") || p.literal("<?") {
			p.fail(vector.UnsupportedXML, p.pos)
			break
		}
		if p.at(p.pos) != '<' {
			start := p.pos
			for p.pos < len(p.src) && p.at(p.pos) != '<' && p.f.Code == vector.OK {
				if p.literal("]]>") {
					p.fail(vector.Malformed, p.pos)
					break
				}
				p.pos++
			}
			s := span{start, p.pos}
			metadata := p.depth > 0 && (kindName(p.stack[p.depth-1].kind) == "title" || kindName(p.stack[p.depth-1].kind) == "desc")
			if metadata {
				p.text(s, 0)
			} else {
				for i := s.lo; i < s.hi && p.f.Code == vector.OK; i++ {
					if !white(p.at(i)) {
						p.fail(vector.Malformed, i)
					}
				}
			}
			continue
		}
		p.pos++
		if p.at(p.pos) == '/' {
			p.pos++
			n := p.name()
			p.ws()
			if p.at(p.pos) != '>' || p.depth == 0 || !p.same(n, p.stack[p.depth-1].name) {
				p.fail(vector.Malformed, n.lo)
				break
			}
			p.pos++
			p.depth--
			if p.depth == 0 {
				finished = true
			}
			continue
		}
		n := p.name()
		if finished {
			p.fail(vector.Malformed, n.lo)
			break
		}
		if p.depth == 32 {
			p.fail(vector.DepthLimit, n.lo)
			break
		}
		if p.nodes == 4096 {
			p.fail(vector.NodeLimit, n.lo)
			break
		}
		p.nodes++
		if !p.take(recordWork, 1) {
			break
		}
		kind := p.element(n)
		if p.depth == 0 {
			if root || kind != "svg" {
				p.fail(vector.Malformed, n.lo)
				break
			}
			root = true
		} else {
			parent := kindName(p.stack[p.depth-1].kind)
			if kind == "svg" || parent != "svg" && parent != "g" {
				p.fail(vector.UnsupportedFeature, n.lo)
				break
			}
		}
		a, count, empty := p.attributes(false, kind)
		f := frame{name: n, kind: kindCode(kind), transform: identity, mapping: identity, color: 0xff000000, alpha: 255}
		if p.depth > 0 {
			if !p.take(recordWork, 1) {
				break
			}
			f = p.stack[p.depth-1]
			f.name, f.kind = n, kindCode(kind)
		}
		p.configure(&f, a[:count])
		if p.f.Code != vector.OK {
			break
		}
		if kind != "svg" && kind != "g" && kind != "title" && kind != "desc" {
			p.shape(f, a[:count])
		}
		if !empty {
			if !p.take(recordWork, 1) {
				break
			}
			p.stack[p.depth] = f
			p.depth++
		} else if p.depth == 0 {
			finished = true
		}
	}
	if !root || !finished || p.depth != 0 {
		p.fail(vector.Malformed, p.pos)
	}
}

var elementNames = [...]string{"", "svg", "g", "path", "rect", "circle", "ellipse", "polygon", "title", "desc"}

func kindCode(name string) uint8 {
	for i, s := range elementNames {
		if name == s {
			return uint8(i)
		}
	}
	return 0
}
func kindName(kind uint8) string { return elementNames[kind] }
func (p *parser) element(n span) string {
	for _, s := range [...]string{"svg", "g", "path", "rect", "circle", "ellipse", "polygon", "title", "desc"} {
		if p.equal(n, s) {
			return s
		}
	}
	for _, s := range [...]string{"text", "tspan", "textPath", "font", "font-face", "font-face-src", "font-face-uri", "glyph", "missing-glyph"} {
		if p.equal(n, s) {
			p.fail(vector.UnsupportedText, n.lo)
			return ""
		}
	}
	for _, s := range [...]string{"image", "foreignObject", "a"} {
		if p.equal(n, s) {
			p.fail(vector.ExternalResource, n.lo)
			return ""
		}
	}
	p.fail(vector.UnsupportedFeature, n.lo)
	return ""
}

var identity = vector.Affine{A: 1, D: 1}

func multiply(a, b vector.Affine) vector.Affine {
	return vector.Affine{A: a.A*b.A + a.C*b.B, B: a.B*b.A + a.D*b.B,
		C: a.A*b.C + a.C*b.D, D: a.B*b.C + a.D*b.D,
		E: a.A*b.E + a.C*b.F + a.E, F: a.B*b.E + a.D*b.F + a.F}
}
func bound(v, cap float64) bool { return !math.IsNaN(v) && math.Abs(v) <= cap }
func matrixOK(a vector.Affine) bool {
	return bound(a.A, 256) && bound(a.B, 256) && bound(a.C, 256) && bound(a.D, 256) && bound(a.E, 32768) && bound(a.F, 32768)
}
