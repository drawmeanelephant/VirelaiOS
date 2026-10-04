package svg

import (
	"math"
	"virelai/vector"
)

func allowed(kind, name string) bool {
	switch kind {
	case "svg":
		return name == "width" || name == "height" || name == "viewBox" || name == "preserveAspectRatio" || name == "xmlns"
	case "path":
		return name == "d"
	case "rect":
		return name == "x" || name == "y" || name == "width" || name == "height" || name == "rx" || name == "ry"
	case "circle":
		return name == "cx" || name == "cy" || name == "r"
	case "ellipse":
		return name == "cx" || name == "cy" || name == "rx" || name == "ry"
	case "polygon":
		return name == "points"
	}
	return false
}
func (p *parser) attrName(s span) string {
	for _, name := range [...]string{"id", "fill", "fill-rule", "fill-opacity", "transform", "stroke",
		"width", "height", "viewBox", "preserveAspectRatio", "xmlns", "d",
		"x", "y", "rx", "ry", "cx", "cy", "r", "points"} {
		if p.equal(s, name) {
			return name
		}
	}
	return ""
}
func (p *parser) resource(a attr) bool {
	if p.equal(a.name, "id") {
		return false // Opaque metadata, never a resource name or URI.
	}
	for _, name := range [...]string{"href", "xlink:href", "src", "srcset", "resource", "url", "xml:base"} {
		if p.equal(a.name, name) {
			return true
		}
	}
	l := p.lex(a.value)
	l.spaces()
	// Refuse URI-valued content even on an otherwise unknown attribute.
	var prefix [8]byte
	n := 0
	for l.ok && n < len(prefix) {
		if l.c > 127 {
			break
		}
		if !p.take(byteWork, 1) {
			break
		}
		prefix[n], n = byte(l.c), n+1
		l.advance()
	}
	s := string(prefix[:n])
	return n >= 4 && s[:4] == "url(" || n >= 5 && (s[:5] == "http:" || s[:5] == "data:" || s[:5] == "file:") || n >= 6 && s[:6] == "https:"
}
func (p *parser) color(s span) (uint32, bool) {
	for _, c := range [...]struct {
		name string
		rgb  uint32
	}{{"none", 0}, {"black", 0}, {"white", 0xffffff}, {"red", 0xff0000}, {"green", 0x008000}, {"blue", 0x0000ff}} {
		if p.value(s, c.name) {
			return 0xff000000 | c.rgb, c.name == "none"
		}
	}
	l := p.lex(s)
	if !l.ok || l.c != '#' {
		p.fail(vector.UnsupportedFeature, s.lo)
		return 0, false
	}
	l.advance()
	var digits [6]uint32
	n := 0
	for l.ok && p.f.Code == vector.OK {
		if l.c > 127 || hex(byte(l.c)) < 0 || n == 6 {
			p.fail(vector.UnsupportedFeature, s.lo)
			break
		}
		if !p.take(byteWork, 1) {
			break
		}
		digits[n], n = uint32(hex(byte(l.c))), n+1
		l.advance()
	}
	var v uint32
	if n == 3 {
		for i := 0; i < 3; i++ {
			v = v<<8 | digits[i]*17
		}
	} else if n == 6 {
		for i := 0; i < 6; i++ {
			v = v<<4 | digits[i]
		}
	} else {
		p.fail(vector.UnsupportedFeature, s.lo)
	}
	return 0xff000000 | v, false
}
func (p *parser) configure(f *frame, attrs []attr) {
	kind := kindName(f.kind)
	local := identity
	var width, height float64
	haveW, haveH, haveVB, noneAspect := false, false, false, false
	var vb [4]float64
	for _, a := range attrs {
		name := p.attrName(a.name)
		if name != "xmlns" && p.resource(a) {
			p.fail(vector.ExternalResource, a.name.lo)
			return
		}
		if kind == "title" || kind == "desc" {
			if name != "id" {
				p.fail(vector.UnsupportedFeature, a.name.lo)
				return
			}
		}
		switch name {
		case "id":
			p.text(a.value, 64)
		case "fill":
			f.color, f.none = p.color(a.value)
		case "fill-opacity":
			v := p.scalar(a.value, false)
			if v < 0 || v > 1 {
				p.fail(vector.Malformed, a.value.lo)
			}
			f.alpha = uint8(math.Floor(v*255 + .5))
		case "fill-rule":
			if p.value(a.value, "evenodd") {
				f.rule = vector.EvenOdd
			} else if p.value(a.value, "nonzero") {
				f.rule = vector.NonZero
			} else {
				p.fail(vector.UnsupportedFeature, a.value.lo)
			}
		case "stroke":
			if !p.value(a.value, "none") {
				p.fail(vector.UnsupportedFeature, a.value.lo)
			}
		case "transform":
			local = p.transform(a.value)
		default:
			if !allowed(kind, name) {
				p.fail(vector.UnsupportedFeature, a.name.lo)
			} else if kind == "svg" {
				switch name {
				case "width", "height":
					v := p.scalar(a.value, true)
					if v <= 0 || v > 1024 || v != math.Trunc(v) {
						p.fail(vector.CanvasLimit, a.value.lo)
					}
					if name == "width" {
						width, haveW = v, true
					} else {
						height, haveH = v, true
					}
				case "viewBox":
					if p.numbers(a.value, vb[:]) != 4 || vb[2] <= 0 || vb[3] <= 0 {
						p.fail(vector.Malformed, a.value.lo)
					}
					haveVB = true
				case "preserveAspectRatio":
					if p.value(a.value, "none") {
						noneAspect = true
					} else if !p.value(a.value, "xMidYMid meet") {
						p.fail(vector.UnsupportedFeature, a.value.lo)
					}
				case "xmlns":
					if !p.value(a.value, "http://www.w3.org/2000/svg") {
						p.fail(vector.UnsupportedFeature, a.value.lo)
					}
				}
			}
		}
		if p.f.Code != vector.OK {
			return
		}
	}
	if kind == "svg" {
		if !haveW || !haveH {
			p.fail(vector.Malformed, f.name.lo)
			return
		}
		p.canvas = Canvas{int(width), int(height)}
		if !haveVB {
			vb = [4]float64{0, 0, width, height}
		}
		sx, sy := width/vb[2], height/vb[3]
		x, y := 0.0, 0.0
		if !noneAspect {
			sx = math.Min(sx, sy)
			sy = sx
			x, y = (width-vb[2]*sx)/2, (height-vb[3]*sy)/2
		}
		p.viewport = vector.Affine{A: sx, D: sy, E: x - vb[0]*sx, F: y - vb[1]*sy}
		if !matrixOK(p.viewport) {
			p.fail(vector.CoordinateLimit, f.name.lo)
		}
	}
	if !p.take(pointWork, 1) {
		return
	}
	f.mapping = multiply(f.mapping, local)
	if !matrixOK(f.mapping) {
		p.fail(vector.CoordinateLimit, f.name.lo)
	}
	if !p.take(pointWork, 1) {
		return
	}
	f.transform = multiply(p.viewport, f.mapping)
	if !matrixOK(f.transform) {
		p.fail(vector.CoordinateLimit, f.name.lo)
	}
}

// Validate attribute syntax as soon as its closing quote is read. This
// prevents a later bad attribute from hiding an earlier unsupported token.
// Geometry is emitted only after the complete inherited mapping is known.
func (p *parser) validateAttribute(kind string, a attr) {
	if a.name.hi-a.name.lo >= 5 {
		isFont := true
		for i := 0; i < 5; i++ {
			if p.at(a.name.lo+i) != "font-"[i] {
				isFont = false
				break
			}
		}
		if isFont {
			p.fail(vector.UnsupportedText, a.name.lo)
			return
		}
	}
	name := p.attrName(a.name)
	if name != "xmlns" && p.resource(a) {
		p.fail(vector.ExternalResource, a.name.lo)
		return
	}
	if (kind == "title" || kind == "desc") && name != "id" {
		p.fail(vector.UnsupportedFeature, a.name.lo)
		return
	}
	switch name {
	case "id":
		p.text(a.value, 64)
	case "fill":
		p.color(a.value)
	case "fill-rule":
		if !p.value(a.value, "nonzero") && !p.value(a.value, "evenodd") {
			p.fail(vector.UnsupportedFeature, a.value.lo)
		}
	case "fill-opacity":
		v := p.scalar(a.value, false)
		if v < 0 || v > 1 {
			p.fail(vector.Malformed, a.value.lo)
		}
	case "stroke":
		if !p.value(a.value, "none") {
			p.fail(vector.UnsupportedFeature, a.value.lo)
		}
	case "transform":
		before := p.transforms
		p.transform(a.value)
		p.transforms = before
	default:
		if !allowed(kind, name) {
			p.fail(vector.UnsupportedFeature, a.name.lo)
			return
		}
		switch name {
		case "d", "points":
			nc, contours := p.nc, p.contours
			p.preview = true
			if name == "d" {
				p.path(frame{transform: identity}, a.value)
			} else {
				p.polygon(frame{transform: identity}, a.value)
			}
			p.preview = false
			p.nc, p.contours = nc, contours
		case "viewBox":
			var vb [4]float64
			if p.numbers(a.value, vb[:]) != 4 || vb[2] <= 0 || vb[3] <= 0 {
				p.fail(vector.Malformed, a.value.lo)
			}
		case "preserveAspectRatio":
			if !p.value(a.value, "none") && !p.value(a.value, "xMidYMid meet") {
				p.fail(vector.UnsupportedFeature, a.value.lo)
			}
		case "xmlns":
			if !p.value(a.value, "http://www.w3.org/2000/svg") {
				p.fail(vector.UnsupportedFeature, a.value.lo)
			}
		default:
			v := p.scalar(a.value, true)
			if kind == "svg" && (v <= 0 || v > 1024 || v != math.Trunc(v)) {
				p.fail(vector.CanvasLimit, a.value.lo)
			} else if (name == "width" || name == "height" || name == "r" || name == "rx" || name == "ry") && v < 0 {
				p.fail(vector.Malformed, a.value.lo)
			}
		}
	}
}

func (p *parser) find(attrs []attr, name string) (span, bool) {
	for _, a := range attrs {
		if p.equal(a.name, name) {
			return a.value, true
		}
	}
	return span{}, false
}
func (p *parser) length(attrs []attr, name string, required, positive bool) (float64, bool) {
	s, found := p.find(attrs, name)
	if !found {
		if required {
			p.fail(vector.Malformed, p.pos)
		}
		return 0, false
	}
	v := p.scalar(s, true)
	if positive && v < 0 {
		p.fail(vector.Malformed, s.lo)
	}
	return v, true
}
