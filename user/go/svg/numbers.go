package svg

import (
	"math"
	"virelai/vector"
)

type lexer struct {
	r  reader
	c  rune
	ok bool
}

func (p *parser) lex(s span) lexer {
	l := lexer{r: p.reader(s)}
	l.advance()
	return l
}
func (l *lexer) advance() { l.c, l.ok = l.r.next() }
func (l *lexer) spaces() bool {
	had := false
	for l.ok && l.c < 128 && white(byte(l.c)) {
		had = true
		l.advance()
	}
	return had
}
func digit(c rune) bool    { return c >= '0' && c <= '9' }
func numStart(c rune) bool { return digit(c) || c == '.' || c == '+' || c == '-' }

func (l *lexer) number() float64 {
	p, offset := l.r.p, l.r.i
	var token [32]byte
	n := 0
	push := func() {
		if n == len(token) {
			p.fail(vector.TokenLimit, offset)
			l.ok = false
			return
		}
		if !p.take(byteWork, 1) {
			l.ok = false
			return
		}
		token[n], n = byte(l.c), n+1
		l.advance()
	}
	if l.ok && (l.c == '+' || l.c == '-') {
		push()
	}
	digits := 0
	for l.ok && digit(l.c) {
		push()
		digits++
	}
	if l.ok && l.c == '.' {
		push()
		for l.ok && digit(l.c) {
			push()
			digits++
		}
	}
	if digits == 0 {
		p.fail(vector.Malformed, offset)
		return 0
	}
	if l.ok && (l.c == 'e' || l.c == 'E') {
		push()
		if l.ok && (l.c == '+' || l.c == '-') {
			push()
		}
		digits = 0
		for l.ok && digit(l.c) {
			push()
			digits++
		}
		if digits == 0 {
			p.fail(vector.Malformed, offset)
			return 0
		}
	}
	v, scale, exponent, k, neg := 0.0, 0, 0, 0, false
	if n > 0 && (token[0] == '-' || token[0] == '+') {
		neg, k = token[0] == '-', 1
	}
	fraction := false
	for k < n && token[k] != 'e' && token[k] != 'E' {
		if !p.take(byteWork, 1) {
			return 0
		}
		if token[k] == '.' {
			fraction = true
		} else {
			v = v*10 + float64(token[k]-'0')
			if fraction {
				scale++
			}
		}
		k++
	}
	if k < n {
		if !p.take(byteWork, 1) {
			return 0
		}
		k++
		eneg := false
		if k < n && (token[k] == '+' || token[k] == '-') {
			if !p.take(byteWork, 1) {
				return 0
			}
			eneg = token[k] == '-'
			k++
		}
		for ; k < n; k++ {
			if !p.take(byteWork, 1) {
				return 0
			}
			exponent = min(10000, exponent*10+int(token[k]-'0'))
		}
		if eneg {
			exponent = -exponent
		}
	}
	// Splitting a very small power prevents premature underflow when the
	// significand's magnitude can compensate for it.
	exp := exponent - scale
	if v != 0 {
		if exp < -308 {
			v = v * math.Pow10(-308) * math.Pow10(exp+308)
		} else {
			v *= math.Pow10(exp)
		}
	}
	if neg {
		v = -v
	}
	if !bound(v, 32768) {
		p.fail(vector.CoordinateLimit, offset)
	}
	return v
}

// separator validates one separator between numbers. Signs may delimit path
// numbers, but a comma must have a following number and may not be doubled.
func (l *lexer) separator(first, sign bool) bool {
	space := l.spaces()
	if l.ok && l.c == ',' {
		if first {
			l.r.p.fail(vector.Malformed, l.r.i-1)
			return false
		}
		l.advance()
		l.spaces()
		if !l.ok || !numStart(l.c) {
			l.r.p.fail(vector.Malformed, l.r.i-1)
			return false
		}
		return true
	}
	if !l.ok {
		return false
	}
	if !first && !space && !(sign && (l.c == '+' || l.c == '-')) {
		l.r.p.fail(vector.Malformed, l.r.i-1)
		return false
	}
	return true
}
func (p *parser) scalar(s span, length bool) float64 {
	l := p.lex(s)
	l.spaces()
	v := l.number()
	if length && l.ok && l.c == 'p' {
		l.advance()
		if !l.ok || l.c != 'x' {
			p.fail(vector.UnsupportedFeature, s.lo)
		} else {
			l.advance()
		}
	}
	l.spaces()
	if l.ok {
		p.fail(vector.UnsupportedFeature, s.lo)
	}
	return v
}
func (p *parser) numbers(s span, out []float64) int {
	l := p.lex(s)
	n := 0
	l.spaces()
	for l.ok && p.f.Code == vector.OK {
		if !l.separator(n == 0, false) {
			break
		}
		if n == len(out) {
			p.fail(vector.Malformed, s.lo)
			break
		}
		out[n], n = l.number(), n+1
	}
	return n
}
func (p *parser) transform(s span) vector.Affine {
	l := p.lex(s)
	a, count := identity, 0
	l.spaces()
	for l.ok && p.f.Code == vector.OK {
		if count == 16 || p.transforms == 256 {
			p.fail(vector.TokenLimit, l.r.i-1)
			break
		}
		var name [10]byte
		n := 0
		for l.ok && l.c >= 'a' && l.c <= 'z' {
			if n == len(name) {
				p.fail(vector.UnsupportedFeature, l.r.i-1)
				break
			}
			if !p.take(byteWork, 1) {
				break
			}
			name[n], n = byte(l.c), n+1
			l.advance()
		}
		l.spaces()
		if !l.ok || l.c != '(' {
			p.fail(vector.Malformed, l.r.i-1)
			break
		}
		l.advance()
		l.spaces()
		var args [6]float64
		nargs := 0
		for l.ok && l.c != ')' && p.f.Code == vector.OK {
			if !l.separator(nargs == 0, false) {
				break
			}
			if nargs == 6 {
				p.fail(vector.Malformed, l.r.i-1)
				break
			}
			args[nargs], nargs = l.number(), nargs+1
			// Permit whitespace before the closing parenthesis without
			// consuming separators between successive numbers.
			probe := l
			probe.spaces()
			if probe.ok && probe.c == ')' {
				l = probe
			}
		}
		if !l.ok || l.c != ')' {
			p.fail(vector.Malformed, s.lo)
			break
		}
		l.advance()
		t := identity
		if !p.take(byteWork, uint64(n)) {
			break
		}
		switch string(name[:n]) {
		case "matrix":
			if nargs != 6 {
				p.fail(vector.Malformed, s.lo)
			}
			t = vector.Affine{A: args[0], B: args[1], C: args[2], D: args[3], E: args[4], F: args[5]}
		case "translate":
			if nargs < 1 || nargs > 2 {
				p.fail(vector.Malformed, s.lo)
			}
			t.E, t.F = args[0], args[1]
		case "scale":
			if nargs < 1 || nargs > 2 {
				p.fail(vector.Malformed, s.lo)
			}
			if nargs == 1 {
				args[1] = args[0]
			}
			t.A, t.D = args[0], args[1]
		default:
			p.fail(vector.UnsupportedFeature, s.lo)
		}
		if !matrixOK(t) {
			p.fail(vector.CoordinateLimit, s.lo)
		}
		if !p.take(pointWork, 1) {
			break
		}
		a = multiply(a, t)
		if !matrixOK(a) {
			p.fail(vector.CoordinateLimit, s.lo)
		}
		count++
		p.transforms++
		spaced := l.spaces()
		if l.ok && l.c == ',' {
			l.advance()
			l.spaces()
			if !l.ok || l.c == ',' {
				p.fail(vector.Malformed, l.r.i-1)
			}
		} else if l.ok && !spaced {
			p.fail(vector.Malformed, l.r.i-1)
		}
	}
	if count == 0 {
		p.fail(vector.Malformed, s.lo)
	}
	return a
}
