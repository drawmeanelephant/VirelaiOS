package css

import (
	"strings"
	"unicode/utf8"
	"virelai/webstyle"
)

type diagnostics struct{ list []webstyle.Diagnostic }

func (d *diagnostics) add(kind webstyle.DiagnosticKind, line, col uint32, text string) {
	if len(d.list) >= webstyle.MaxDiagnostics {
		return
	}
	if len(d.list) == webstyle.MaxDiagnostics-1 {
		d.list = append(d.list, webstyle.Diagnostic{Kind: webstyle.DiagnosticLimit, Text: "diagnostics-truncated"})
		return
	}
	if len(text) > webstyle.MaxDiagnosticTextBytes {
		text = text[:webstyle.MaxDiagnosticTextBytes]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	d.list = append(d.list, webstyle.Diagnostic{Kind: kind, Line: line, Col: col, Text: text})
}

func (d *diagnostics) merge(list []webstyle.Diagnostic) {
	for _, diag := range list {
		d.add(diag.Kind, diag.Line, diag.Col, diag.Text)
	}
}

const (
	tEOF byte = iota
	tSpace
	tIdent
	tString
	tNumber
	tDimension
	tPercent
	tHash
	tBad
	tPunct
)

type token struct {
	kind      byte
	text      string
	line, col uint32
}

func (t token) punct(s string) bool { return t.kind == tPunct && t.text == s }

type lexer struct {
	src       string
	pos       int
	line, col uint32
	diags     *diagnostics
}

func space(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }
func digit(c byte) bool { return c >= '0' && c <= '9' }
func nameStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 128
}
func nameChar(c byte) bool { return nameStart(c) || digit(c) || c == '-' }
func hex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}
func lower(s string) string {
	// Only ASCII is case-insensitive in the frozen subset.
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

func (l *lexer) advance() {
	c := l.src[l.pos]
	l.pos++
	if c == '\r' {
		if l.pos < len(l.src) && l.src[l.pos] == '\n' {
			l.pos++
		}
		l.line++
		l.col = 1
	} else if c == '\n' || c == '\f' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
}

func (l *lexer) escaped() bool {
	return l.pos+1 < len(l.src) && l.src[l.pos] == '\\' &&
		l.src[l.pos+1] != '\n' && l.src[l.pos+1] != '\r' && l.src[l.pos+1] != '\f'
}

func (l *lexer) escape(b *strings.Builder) {
	l.advance() // backslash
	value, count := 0, 0
	for l.pos < len(l.src) && count < 6 && hex(l.src[l.pos]) >= 0 {
		value = value*16 + hex(l.src[l.pos])
		count++
		l.advance()
	}
	if count > 0 {
		if l.pos < len(l.src) && space(l.src[l.pos]) {
			l.advance()
		}
		if value == 0 || value > utf8.MaxRune || value >= 0xd800 && value <= 0xdfff {
			value = utf8.RuneError
		}
		b.WriteRune(rune(value))
	} else if l.pos < len(l.src) {
		r, size := utf8.DecodeRuneInString(l.src[l.pos:])
		b.WriteRune(r)
		for i := 0; i < size; i++ {
			l.advance()
		}
	}
}

func (l *lexer) name() string {
	var b strings.Builder
	start := l.pos
	for l.pos < len(l.src) {
		if nameChar(l.src[l.pos]) {
			if l.pos-start <= webstyle.MaxCSSTokenBytes {
				b.WriteByte(l.src[l.pos])
			}
			l.advance()
		} else if l.escaped() {
			if l.pos-start <= webstyle.MaxCSSTokenBytes {
				l.escape(&b)
			} else {
				var discard strings.Builder
				l.escape(&discard)
			}
		} else {
			break
		}
	}
	return b.String()
}

func (l *lexer) identStart() bool {
	if l.pos >= len(l.src) {
		return false
	}
	c := l.src[l.pos]
	if nameStart(c) || l.escaped() {
		return true
	}
	if c == '-' && l.pos+1 < len(l.src) {
		n := l.src[l.pos+1]
		return nameStart(n) || n == '-' || n == '\\' && l.pos+2 < len(l.src) && !space(l.src[l.pos+2])
	}
	return false
}

func (l *lexer) numberStart() bool {
	i := l.pos
	if i < len(l.src) && (l.src[i] == '+' || l.src[i] == '-') {
		i++
	}
	return i < len(l.src) && (digit(l.src[i]) ||
		l.src[i] == '.' && i+1 < len(l.src) && digit(l.src[i+1]))
}

func (l *lexer) next() token {
	for l.pos < len(l.src) && strings.HasPrefix(l.src[l.pos:], "/*") {
		l.advance()
		l.advance()
		for l.pos < len(l.src) && !strings.HasPrefix(l.src[l.pos:], "*/") {
			l.advance()
		}
		if l.pos < len(l.src) {
			l.advance()
			l.advance()
		}
	}
	t := token{line: l.line, col: l.col}
	if l.pos >= len(l.src) {
		return t
	}
	start := l.pos
	c := l.src[l.pos]
	switch {
	case space(c):
		t.kind, t.text = tSpace, " "
		for l.pos < len(l.src) && space(l.src[l.pos]) {
			l.advance()
		}
		return t
	case c == '"' || c == '\'':
		t.kind = tString
		l.advance()
		var b strings.Builder
		closed := false
		for l.pos < len(l.src) {
			c = l.src[l.pos]
			if c == l.src[start] {
				l.advance()
				closed = true
				break
			}
			if c == '\n' || c == '\r' || c == '\f' {
				break // bad-string ends before the newline, allowing recovery
			}
			if c == '\\' {
				if l.pos+1 < len(l.src) && space(l.src[l.pos+1]) && l.src[l.pos+1] != ' ' && l.src[l.pos+1] != '\t' {
					l.advance()
					l.advance()
				} else if l.escaped() {
					if l.pos-start <= webstyle.MaxCSSTokenBytes {
						l.escape(&b)
					} else {
						var discard strings.Builder
						l.escape(&discard)
					}
				} else {
					l.advance()
				}
			} else {
				if l.pos-start <= webstyle.MaxCSSTokenBytes {
					b.WriteByte(c)
				}
				l.advance()
			}
		}
		t.text = b.String()
		if !closed {
			t.kind = tBad
		}
	case l.numberStart():
		t.kind = tNumber
		if c == '+' || c == '-' {
			l.advance()
		}
		for l.pos < len(l.src) && digit(l.src[l.pos]) {
			l.advance()
		}
		if l.pos+1 < len(l.src) && l.src[l.pos] == '.' && digit(l.src[l.pos+1]) {
			l.advance()
			for l.pos < len(l.src) && digit(l.src[l.pos]) {
				l.advance()
			}
		}
		end := l.pos
		if l.identStart() {
			t.kind = tDimension
			unit := l.name()
			if end-start <= webstyle.MaxCSSTokenBytes {
				t.text = l.src[start:end] + lower(unit)
			}
		} else if l.pos < len(l.src) && l.src[l.pos] == '%' {
			t.kind = tPercent
			l.advance()
			if l.pos-start <= webstyle.MaxCSSTokenBytes {
				t.text = l.src[start:l.pos]
			}
		} else if end-start <= webstyle.MaxCSSTokenBytes {
			t.text = l.src[start:end]
		}
	case c == '#':
		l.advance()
		t.kind, t.text = tHash, l.name()
		if t.text == "" {
			t.kind, t.text = tPunct, "#"
		}
	case l.identStart():
		t.kind, t.text = tIdent, l.name()
	default:
		t.kind, t.text = tPunct, string(c)
		l.advance()
	}
	if l.pos-start > webstyle.MaxCSSTokenBytes {
		l.diags.add(webstyle.DiagnosticLimit, t.line, t.col, "css-token-limit")
		t.kind, t.text = tBad, ""
	} else if !utf8.ValidString(t.text) || strings.IndexByte(t.text, 0) >= 0 {
		l.diags.add(webstyle.DiagnosticInvalid, t.line, t.col, "invalid-token UTF-8")
		t.kind, t.text = tBad, ""
	}
	return t
}

func trim(ts []token) []token {
	for len(ts) > 0 && ts[0].kind == tSpace {
		ts = ts[1:]
	}
	for len(ts) > 0 && ts[len(ts)-1].kind == tSpace {
		ts = ts[:len(ts)-1]
	}
	return ts
}

func compact(ts []token) []token {
	out := make([]token, 0, len(ts))
	for _, t := range ts {
		if t.kind != tSpace {
			out = append(out, t)
		}
	}
	return out
}
