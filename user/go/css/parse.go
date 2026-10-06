package css

import (
	"unicode/utf8"
	"virelai/webstyle"
)

// Stylesheet owns validated rules and bounded parse diagnostics. Its internals
// are private so downstream code cannot inject unvalidated declarations.
type Stylesheet struct {
	rules     []rule
	ruleCount int
	bytes     int
	diags     []webstyle.Diagnostic
}

type rule struct {
	selectors  []selector
	decls      []declaration
	sourceRule int
}

type parser struct {
	lex   lexer
	t     token
	diags diagnostics
}

func newParser(src string) *parser {
	p := &parser{}
	p.lex = lexer{src: src, line: 1, col: 1, diags: &p.diags}
	if !utf8.ValidString(src) {
		p.diags.add(webstyle.DiagnosticInvalid, 1, 1, "invalid-token UTF-8 source")
	}
	p.next()
	return p
}
func (p *parser) next() { p.t = p.lex.next() }
func (p *parser) warn(kind webstyle.DiagnosticKind, t token, text string) {
	p.diags.add(kind, t.line, t.col, text)
}

// collect stops only at an unquoted, unnested delimiter. Even past the nesting
// cap, it tracks balance without retaining input, so skipped blocks cannot leak.
func (p *parser) collect(stops string) ([]token, bool) {
	var out []token
	var stack []string
	depth := 0
	bad := false
	for p.t.kind != tEOF {
		t := p.t
		if depth == 0 && contains(stops, t.text) && t.kind == tPunct {
			break
		}
		if t.kind == tPunct {
			switch t.text {
			case "(", "[", "{":
				depth++
				if depth <= webstyle.MaxCSSNesting {
					stack = append(stack, t.text)
				} else if !bad {
					p.warn(webstyle.DiagnosticLimit, t, "css-nesting-limit")
					bad = true
				}
			case ")", "]", "}":
				if depth == 0 {
					bad = true
				} else {
					if depth <= webstyle.MaxCSSNesting {
						open := stack[len(stack)-1]
						if open+t.text != "()" && open+t.text != "[]" && open+t.text != "{}" {
							bad = true
						}
						stack = stack[:len(stack)-1]
					}
					depth--
				}
			}
		}
		if t.kind == tBad {
			bad = true
		}
		if !bad {
			out = append(out, t)
		}
		p.next()
	}
	return trim(out), bad || depth != 0
}

func contains(set, s string) bool {
	if len(s) != 1 {
		return false
	}
	for i := 0; i < len(set); i++ {
		if set[i] == s[0] {
			return true
		}
	}
	return false
}

func (p *parser) atRule() {
	at := p.t
	p.next()
	name := p.t.text
	p.warn(webstyle.DiagnosticUnsupported, at, "unsupported-at-rule @"+name)
	p.collect(";{")
	if p.t.punct("{") {
		p.next()
		p.collect("}")
	}
	if p.t.kind != tEOF {
		p.next()
	}
}

func (p *parser) declarations(block bool) []declaration {
	var out []declaration
	count, limited := 0, false
	for p.t.kind != tEOF && (!block || !p.t.punct("}")) {
		if p.t.kind == tSpace || p.t.punct(";") {
			p.next()
			continue
		}
		if p.t.punct("@") {
			p.atRule()
			continue
		}
		start := p.t
		ts, bad := p.collect(";}")
		if !block && p.t.punct("}") {
			bad = true
		}
		count++
		if count > webstyle.MaxDeclarationsPerRule {
			if !limited {
				p.warn(webstyle.DiagnosticLimit, start, "css-declaration-limit")
				limited = true
			}
		} else if bad || len(ts) < 3 || ts[0].kind != tIdent {
			p.warn(webstyle.DiagnosticInvalid, start, "invalid-declaration")
		} else {
			i := 1
			for i < len(ts) && ts[i].kind == tSpace {
				i++
			}
			if i >= len(ts) || !ts[i].punct(":") {
				p.warn(webstyle.DiagnosticInvalid, start, "invalid-declaration "+ts[0].text)
			} else {
				val := trim(ts[i+1:])
				important := false
				end := len(val) - 1
				if end >= 0 && val[end].kind == tIdent && lower(val[end].text) == "important" {
					j := end - 1
					for j >= 0 && val[j].kind == tSpace {
						j--
					}
					if j >= 0 && val[j].punct("!") {
						important = true
						val = trim(val[:j])
					}
				}
				decls := p.property(lower(ts[0].text), val, start)
				for i := range decls {
					decls[i].important = important
				}
				out = append(out, decls...)
			}
		}
		if p.t.punct(";") || !block && p.t.punct("}") {
			p.next()
		}
	}
	return out
}

// Parse parses one complete author source. Over-budget sources are omitted at
// the source boundary, not truncated halfway through a declaration.
func Parse(src []byte) (*Stylesheet, []webstyle.Diagnostic) {
	s := &Stylesheet{bytes: len(src)}
	if len(src) > webstyle.MaxCSSBytes {
		s.diags = []webstyle.Diagnostic{{Kind: webstyle.DiagnosticLimit, Text: "css-byte-limit"}}
		return s, append([]webstyle.Diagnostic(nil), s.diags...)
	}
	p := newParser(string(src))
	rules := 0
	for p.t.kind != tEOF {
		if p.t.kind == tSpace || p.t.punct(";") {
			p.next()
			continue
		}
		// CSS 2.1 permits CDO/CDC wrappers at the stylesheet top level.
		if p.t.punct("<") && p.lex.pos+3 <= len(p.lex.src) && p.lex.src[p.lex.pos:p.lex.pos+3] == "!--" {
			for i := 0; i < 3; i++ {
				p.lex.advance()
			}
			p.next()
			continue
		}
		if p.t.kind == tIdent && p.t.text == "--" && p.lex.pos < len(p.lex.src) && p.lex.src[p.lex.pos] == '>' {
			p.lex.advance()
			p.next()
			continue
		}
		if p.t.punct("@") {
			p.atRule()
			continue
		}
		start := p.t
		ts, bad := p.collect("{;}")
		if !p.t.punct("{") {
			p.warn(webstyle.DiagnosticInvalid, start, "invalid-rule")
			if p.t.kind != tEOF {
				p.next()
			}
			continue
		}
		rules++
		if rules > webstyle.MaxCSSRules {
			p.warn(webstyle.DiagnosticLimit, start, "css-rule-limit")
			break
		}
		sels := p.selectors(ts, start)
		p.next()
		if bad || len(sels) == 0 {
			if bad {
				p.warn(webstyle.DiagnosticInvalid, start, "invalid-rule")
			}
			p.collect("}")
		} else {
			decls := p.declarations(true)
			s.rules = append(s.rules, rule{selectors: sels, decls: decls, sourceRule: rules})
		}
		if p.t.punct("}") {
			p.next()
		}
	}
	s.diags = p.diags.list
	s.ruleCount = rules
	if s.ruleCount > webstyle.MaxCSSRules {
		s.ruleCount = webstyle.MaxCSSRules
	}
	return s, append([]webstyle.Diagnostic(nil), s.diags...)
}
