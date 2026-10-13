package parser

import (
	"strings"

	"virelai/sshlib"
)

// Hash domains (ADR 0049 §3, byte-exact).
const (
	atomDomain = "FLINT-ATOM/1\x00"
	sigDomain  = "FLINT-SIG/1\x00"
)

// ctok is one canonical token: absolute byte offsets into the source and
// the token's canonical text (identifier/literal text, or the operator's
// own string for punctuation).
type ctok struct {
	start, end int
	text       string
}

// canonize builds the §3 canonical token stream for a declaration: every
// token in [toks], comments already dropped by the tokenizer, joined by a
// single space, with the token at blankPos replaced by "_". An implicit
// iota repeat appends " iota=<index>" (iota >= 0 only). Tokens whose
// start falls in [sigLo, sigHi) also form the signature stream — the
// §3 rules in caller terms: fn/method/test cut at the body '{',
// containers and types take the whole declaration, const takes tokens
// after the declared name (type + value), var takes tokens before '='
// (qualifiers + type). The two SHA-256 sums are (hash, sig_hash).
func canonize(kind string, toks []ctok, blankPos, sigLo, sigHi, iota int) ([32]byte, [32]byte, string, string) {
	all, sig := canonStreams(toks, blankPos, sigLo, sigHi, iota)
	sh := sshlib.SHA256Sum(hashInput(atomDomain, kind, all))
	ss := sshlib.SHA256Sum(hashInput(sigDomain, kind, sig))
	return sh, ss, all, sig
}

// canonStreams joins the §3 streams: all tokens, and the [sigLo, sigHi)
// signature subrange, single-space separated, declared name blanked.
func canonStreams(toks []ctok, blankPos, sigLo, sigHi, iota int) (string, string) {
	var all, sig strings.Builder
	for _, t := range toks {
		text := t.text
		if t.start == blankPos {
			text = "_"
		}
		if all.Len() > 0 {
			all.WriteByte(' ')
		}
		all.WriteString(text)
		if t.start >= sigLo && t.start < sigHi {
			if sig.Len() > 0 {
				sig.WriteByte(' ')
			}
			sig.WriteString(text)
		}
	}
	if iota >= 0 {
		if all.Len() > 0 {
			all.WriteByte(' ')
		}
		all.WriteString("iota=")
		all.WriteString(itoa(iota))
		if sig.Len() > 0 {
			sig.WriteByte(' ')
		}
		sig.WriteString("iota=")
		sig.WriteString(itoa(iota))
	}
	return all.String(), sig.String()
}

func hashInput(domain, kind, body string) []byte {
	b := make([]byte, 0, len(domain)+len(kind)+1+len(body))
	b = append(b, domain...)
	b = append(b, kind...)
	b = append(b, 0)
	b = append(b, body...)
	return b
}
