package svg

import (
	"math"
	"virelai/vector"
)

func (p *parser) command(f frame, verb vector.Verb, pts ...vector.Point) {
	if p.f.Code != vector.OK {
		return
	}
	if p.nc == vector.MaxCommands {
		p.fail(vector.CommandLimit, p.pos)
		return
	}
	if !p.preview && p.nc == len(p.out.Commands) {
		p.fail(vector.SceneLimit, p.pos)
		return
	}
	c := vector.Command{Verb: verb}
	for i, q := range pts {
		if !bound(q.X, 32768) || !bound(q.Y, 32768) {
			p.fail(vector.CoordinateLimit, p.pos)
			return
		}
		if !p.take(pointWork, 1) {
			return
		}
		t := f.transform
		x, y := t.A*q.X+t.C*q.Y+t.E, t.B*q.X+t.D*q.Y+t.F
		if !bound(x, 32768) || !bound(y, 32768) {
			p.fail(vector.CoordinateLimit, p.pos)
			return
		}
		c.P[i] = q
	}
	if verb == vector.Move {
		if p.contours == vector.MaxContours {
			p.fail(vector.ContourLimit, p.pos)
			return
		}
		p.contours++
	}
	if !p.take(recordWork, 1) {
		return
	}
	if !p.preview {
		p.out.Commands[p.nc] = c
	}
	p.nc++
}
func (p *parser) shape(f frame, attrs []attr) {
	if p.np == vector.MaxPaints || p.np == len(p.out.Paints) {
		p.fail(vector.SceneLimit, p.pos)
		return
	}
	first := p.nc
	target := false
	switch kindName(f.kind) {
	case "path":
		s, ok := p.find(attrs, "d")
		if !ok {
			p.fail(vector.Malformed, f.name.lo)
		} else {
			p.path(f, s)
		}
	case "polygon":
		s, ok := p.find(attrs, "points")
		if !ok {
			p.fail(vector.Malformed, f.name.lo)
		} else {
			p.polygon(f, s)
		}
	case "rect":
		x, _ := p.length(attrs, "x", false, false)
		y, _ := p.length(attrs, "y", false, false)
		w, _ := p.length(attrs, "width", true, true)
		h, _ := p.length(attrs, "height", true, true)
		rx, hx := p.length(attrs, "rx", false, true)
		ry, hy := p.length(attrs, "ry", false, true)
		if hx && !hy {
			ry = rx
		} else if hy && !hx {
			rx = ry
		}
		rx, ry = math.Min(rx, w/2), math.Min(ry, h/2)
		if w != 0 && h != 0 {
			if rx == 0 || ry == 0 {
				p.command(f, vector.Move, vector.Point{X: x, Y: y})
				p.command(f, vector.Line, vector.Point{X: x + w, Y: y})
				p.command(f, vector.Line, vector.Point{X: x + w, Y: y + h})
				p.command(f, vector.Line, vector.Point{X: x, Y: y + h})
				p.command(f, vector.Close)
			} else {
				target = true
				p.targetCommand(f, vector.Move, vector.Point{X: x + rx, Y: y})
				p.targetCommand(f, vector.Line, vector.Point{X: x + w - rx, Y: y})
				p.arc(f, x+w-rx, y+ry, rx, ry, -math.Pi/2, 0)
				p.targetCommand(f, vector.Line, vector.Point{X: x + w, Y: y + h - ry})
				p.arc(f, x+w-rx, y+h-ry, rx, ry, 0, math.Pi/2)
				p.targetCommand(f, vector.Line, vector.Point{X: x + rx, Y: y + h})
				p.arc(f, x+rx, y+h-ry, rx, ry, math.Pi/2, math.Pi)
				p.targetCommand(f, vector.Line, vector.Point{X: x, Y: y + ry})
				p.arc(f, x+rx, y+ry, rx, ry, math.Pi, 3*math.Pi/2)
				p.command(frame{transform: identity}, vector.Close)
			}
		}
	case "circle", "ellipse":
		x, _ := p.length(attrs, "cx", false, false)
		y, _ := p.length(attrs, "cy", false, false)
		var rx, ry float64
		if kindName(f.kind) == "circle" {
			rx, _ = p.length(attrs, "r", true, true)
			ry = rx
		} else {
			rx, _ = p.length(attrs, "rx", true, true)
			ry, _ = p.length(attrs, "ry", true, true)
		}
		if rx != 0 && ry != 0 {
			target = true
			p.targetCommand(f, vector.Move, vector.Point{X: x + rx, Y: y})
			for i := 0; i < 4; i++ {
				p.arc(f, x, y, rx, ry, float64(i)*math.Pi/2, float64(i+1)*math.Pi/2)
			}
			p.command(frame{transform: identity}, vector.Close)
		}
	}
	if p.f.Code != vector.OK {
		return
	}
	if !p.take(recordWork, 1) {
		return
	}
	color := uint32(f.alpha)<<24 | f.color&0xffffff
	if f.none {
		color = 0
	}
	t := f.transform
	if target {
		t = identity
	}
	p.out.Paints[p.np] = vector.Paint{First: uint32(first), Count: uint32(p.nc - first), Transform: t,
		Clip: vector.Rect{X1: p.canvas.Width, Y1: p.canvas.Height}, Color: color, Rule: f.rule}
	p.np++
}

func (p *parser) targetCommand(f frame, v vector.Verb, q vector.Point) {
	if !bound(q.X, 32768) || !bound(q.Y, 32768) {
		p.fail(vector.CoordinateLimit, p.pos)
		return
	}
	if !p.take(pointWork, 1) {
		return
	}
	a := f.transform
	q = vector.Point{X: a.A*q.X + a.C*q.Y + a.E, Y: a.B*q.X + a.D*q.Y + a.F}
	p.command(frame{transform: identity}, v, q)
}

// Linear interpolation error <= sup ||ellipse”|| * delta² / 8.
// The affine-image Frobenius norm is a conservative bound for every angle,
// including shear, reflection and singular transforms. No cubic surrogate.
func (p *parser) arc(f frame, cx, cy, rx, ry, start, end float64) {
	a := f.transform
	norm := math.Hypot(math.Hypot(a.A*rx, a.B*rx), math.Hypot(a.C*ry, a.D*ry))
	n := 1
	for depth := 0; ; depth++ {
		if !p.take(arcWork, 1) {
			return
		}
		delta := (end - start) / float64(n)
		if norm*delta*delta/8 <= 1.0/16 {
			break
		}
		if depth == 12 {
			p.fail(vector.CurveLimit, p.pos)
			return
		}
		n *= 2
	}
	for i := 1; i <= n && p.f.Code == vector.OK; i++ {
		if !p.take(arcWork, 1) {
			return
		}
		if p.arcs == vector.MaxEdges {
			p.fail(vector.SegmentLimit, p.pos)
			return
		}
		angle := start + (end-start)*float64(i)/float64(n)
		s, c := math.Sincos(angle)
		p.targetCommand(f, vector.Line, vector.Point{X: cx + rx*c, Y: cy + ry*s})
		p.arcs++
	}
}

func (p *parser) polygon(f frame, s span) {
	l := p.lex(s)
	count := 0
	for l.ok && p.f.Code == vector.OK {
		if !l.separator(count == 0, false) {
			break
		}
		x := l.number()
		if !l.separator(false, false) {
			p.fail(vector.Malformed, s.lo)
			break
		}
		y := l.number()
		v := vector.Line
		if count == 0 {
			v = vector.Move
		}
		p.command(f, v, vector.Point{X: x, Y: y})
		count++
	}
	if count > 0 {
		p.command(f, vector.Close)
	}
	// Fewer than three points are geometrically empty, but their coordinates
	// and commands are still validated and charged.
}
