// The flat-grammar request projection (ADR 0044 D1): the caller's
// wire reader parses and validates the line, then lifts it into a
// Request — the verb name plus the fields outside {id, verb}. The
// verbs re-validate every argument they consume.
package agentverbs

import (
	"strings"
	"unicode/utf8"
)

// Field kinds under the flat grammar (D1): a value may only be a
// string, an unsigned integer no larger than 2^31-1, a boolean, or an
// array of flat objects — the numeric order mirrors the wire reader's.
const (
	FStr = iota
	FInt
	FBool
	FArr
)

// Parsed-value kinds for array elements — the same order the wire
// reader's parse tree uses.
const (
	KObj = iota
	KArr
	KStr
	KNum
	KTrue
	KFalse
	KNull
)

// Field is one argument's flat projection: the grammar kind and its
// payload (Str for FStr, Num for FInt, Arr for FArr).
type Field struct {
	Kind int
	Str  string
	Num  int64
	Arr  []Val
}

// Member is one object member inside a Val.
type Member struct {
	Key string
	Val Val
}

// Val is one parse-tree node of an array argument (ui.input's
// events[]): kind plus the payload the kind carries — decoded Str for
// KStr, literal text for KNum/KTrue/KFalse/KNull, Membs for KObj,
// Items for KArr.
type Val struct {
	Kind  int
	Str   string
	Membs []Member
	Items []Val
}

// Request is one validated request's verb projection: the verb name
// and the flat argument fields. The caller (the wire reader) has
// already refused malformed lines and non-grammar values; Dispatch
// refuses the arg-level mistakes.
type Request struct {
	Verb   string
	Fields map[string]Field
}

// hasArgs reports whether every key outside {id, verb} is in the
// verb's allowed argument set — an unknown key is bad_request (D1).
func (r *Request) hasArgs(names ...string) bool {
	for k := range r.Fields {
		if k == "id" || k == "verb" {
			continue
		}
		ok := false
		for _, n := range names {
			if k == n {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// argInt returns an integer argument validated against [lo, hi].
// present is false when the key is absent; ok is false when the value
// is mistyped or out of range — the caller maps that to bad_request.
func (r *Request) argInt(name string, lo, hi int64) (v int64, present, ok bool) {
	f, ok := r.Fields[name]
	if !ok {
		return 0, false, true
	}
	if f.Kind != FInt || f.Num < lo || f.Num > hi {
		return 0, true, false
	}
	return f.Num, true, true
}

// argStr returns a string argument. present/ok mirror argInt.
func (r *Request) argStr(name string) (v string, present, ok bool) {
	f, ok := r.Fields[name]
	if !ok {
		return "", false, true
	}
	if f.Kind != FStr {
		return "", true, false
	}
	return f.Str, true, true
}

// argArray returns an array argument's raw elements. present/ok mirror
// argInt; ok is false when the value is not an array. Element shape is
// the verb's own check — the grammar only guarantees the parse tree.
func (r *Request) argArray(name string) (v []Val, present, ok bool) {
	f, ok := r.Fields[name]
	if !ok {
		return nil, false, true
	}
	if f.Kind != FArr {
		return nil, true, false
	}
	return f.Arr, true, true
}

// intMax is the flat grammar's integer bound (2^31 - 1).
const intMax = 2147483647

// flatInt reports whether text is an unsigned decimal integer within
// the 2^31-1 bound — no sign, no fraction, no exponent.
func flatInt(s string) (int64, bool) {
	if len(s) == 0 || len(s) > 10 {
		return 0, false
	}
	var n int64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
		if n > intMax {
			return 0, false
		}
	}
	return n, true
}

// hexDigits is the lowercase hex alphabet the escaped-string writer
// uses for control bytes.
const hexDigits = "0123456789abcdef"

// jwriteStr appends a JSON string literal: '"', '\' and control bytes
// are escaped, and invalid UTF-8 becomes U+FFFD (D1's text-field rule).
func jwriteStr(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for len(s) > 0 {
		c := s[0]
		switch {
		case c == '"' || c == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
			s = s[1:]
		case c == '\n':
			sb.WriteString(`\n`)
			s = s[1:]
		case c == '\r':
			sb.WriteString(`\r`)
			s = s[1:]
		case c == '\t':
			sb.WriteString(`\t`)
			s = s[1:]
		case c == '\b':
			sb.WriteString(`\b`)
			s = s[1:]
		case c == '\f':
			sb.WriteString(`\f`)
			s = s[1:]
		case c < 0x20:
			sb.WriteString("\\u00")
			sb.WriteByte(hexDigits[c>>4])
			sb.WriteByte(hexDigits[c&0xf])
			s = s[1:]
		case c < 0x80:
			sb.WriteByte(c)
			s = s[1:]
		default:
			r, n := utf8.DecodeRuneInString(s)
			if r == utf8.RuneError && n <= 1 {
				sb.WriteString("�")
				s = s[1:]
				continue
			}
			sb.WriteString(s[:n])
			s = s[n:]
		}
	}
	sb.WriteByte('"')
}
