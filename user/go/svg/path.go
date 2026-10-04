package svg

import "virelai/vector"

func (p *parser) path(f frame, s span) {
	l := p.lex(s)
	var cur, start, control vector.Point
	var cmd, family rune
	open, closed, newCommand := false, false, false
	for l.ok && p.f.Code == vector.OK {
		spaced := l.spaces()
		if !l.ok {
			break
		}
		newCommand = false
		if l.c >= 'A' && l.c <= 'Z' || l.c >= 'a' && l.c <= 'z' {
			cmd = l.c
			l.advance()
			newCommand = true
		}
		upper := cmd
		relative := upper >= 'a' && upper <= 'z'
		if relative {
			upper -= 'a' - 'A'
		}
		if upper == 'A' {
			p.fail(vector.UnsupportedFeature, l.r.i-1)
			break
		}
		if upper != 'M' && upper != 'L' && upper != 'H' && upper != 'V' &&
			upper != 'C' && upper != 'S' && upper != 'Q' && upper != 'T' && upper != 'Z' {
			p.fail(vector.UnsupportedFeature, l.r.i-1)
			break
		}
		if !open && upper != 'M' {
			p.fail(vector.Malformed, l.r.i-1)
			break
		}
		if upper == 'Z' {
			if !newCommand {
				p.fail(vector.Malformed, l.r.i-1)
				break
			}
			p.command(f, vector.Close)
			cur, closed, family, cmd = start, true, 0, 0
			continue
		}
		n := 2
		switch upper {
		case 'H', 'V':
			n = 1
		case 'Q', 'S':
			n = 4
		case 'C':
			n = 6
		}
		var args [6]float64
		for i := 0; i < n && p.f.Code == vector.OK; i++ {
			if !l.separator(i == 0 && (newCommand || spaced), true) || !numStart(l.c) {
				p.fail(vector.Malformed, l.r.i-1)
				break
			}
			args[i] = l.number()
		}
		if p.f.Code != vector.OK {
			break
		}
		var q [3]vector.Point
		for i := 0; i < n/2; i++ {
			q[i] = vector.Point{X: args[i*2], Y: args[i*2+1]}
			if relative {
				q[i].X += cur.X
				q[i].Y += cur.Y
			}
		}
		reflected := cur
		if family == 'C' && upper == 'S' || family == 'Q' && upper == 'T' {
			reflected = vector.Point{X: 2*cur.X - control.X, Y: 2*cur.Y - control.Y}
		}
		nextFamily := rune(0)
		switch upper {
		case 'M':
			if open && !closed {
				p.command(f, vector.Close)
			}
			p.command(f, vector.Move, q[0])
			cur, start, open, closed = q[0], q[0], true, false
			cmd = 'L'
			if relative {
				cmd = 'l'
			}
		case 'H':
			x := args[0]
			if relative {
				x += cur.X
			}
			q[0] = vector.Point{X: x, Y: cur.Y}
			p.command(f, vector.Line, q[0])
			cur, closed = q[0], false
		case 'V':
			y := args[0]
			if relative {
				y += cur.Y
			}
			q[0] = vector.Point{X: cur.X, Y: y}
			p.command(f, vector.Line, q[0])
			cur, closed = q[0], false
		case 'L':
			p.command(f, vector.Line, q[0])
			cur, closed = q[0], false
		case 'Q':
			p.command(f, vector.Quad, q[0], q[1])
			control, cur, closed, nextFamily = q[0], q[1], false, 'Q'
		case 'T':
			p.command(f, vector.Quad, reflected, q[0])
			control, cur, closed, nextFamily = reflected, q[0], false, 'Q'
		case 'C':
			p.command(f, vector.Cubic, q[0], q[1], q[2])
			control, cur, closed, nextFamily = q[1], q[2], false, 'C'
		case 'S':
			p.command(f, vector.Cubic, reflected, q[0], q[1])
			control, cur, closed, nextFamily = q[0], q[1], false, 'C'
		}
		family = nextFamily
	}
	if open && !closed {
		p.command(f, vector.Close)
	}
}
