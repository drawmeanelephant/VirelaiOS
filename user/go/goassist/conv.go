// conv.go — GOASSIST's conversation model and its on-disk codec (M140b,
// #2785; v2 adds the title header and the system turn, #2920; v3 adds the
// act records, #3809). A conversation is an ordered list of turns; the
// file is its durable half, published crash-safe through
// vi.WriteFileSafe so a torn write is never a torn chat.
//
// The wire format is line-oriented so it stays readable with `vf cat`:
//
//	GOASSIST-CHAT v3
//	@@title <display title, ≤48 bytes>   (v2+, first line, optional)
//	@@system                             (v2+, optional, turn 0 only)
//	<prompt lines>
//	@@user
//	<text lines>
//	@@assistant
//	<text lines>
//	@@call <verb>                        (v3 only; body = the call's args)
//	<args lines>
//	@@consent allow|deny                 (v3 only; must follow a @@call)
//	@@result ok|refuse:<reason>          (v3 only; follows @@call or
//	<output lines>                        @@consent; body = verb output,
//	                                      cut at convOutMax + trunc mark)
//
// The three act records are the conversation's memory of a verb the
// assistant tried to run: the call it made, the user's answer, and what
// came back (or the named refusal). @@consent must immediately follow a
// @@call and @@result immediately follows the call or its consent — any
// other record closes the act, so a later @@consent or @@result with no
// open @@call refuses the file.
//
// A file stays v1 while it has no title and no preset — an untouched store
// is byte-identical to the M140b format, and a v1 file reads as a v2 one
// with an empty title. v3 is likewise write-on-demand: a conversation with
// no act records still encodes v1/v2, so files without acts stay
// byte-identical. Encode picks the version from the content; decode picks
// it from the magic line.
//
// A `@@`-prefixed line inside a body would split a turn, so bodies never
// carry one: encode prefixes such a line with a single space, decode
// strips it. Decode is strict — wrong magic, an unknown role, a body out
// of order, a stray or over-long title, a system turn that is not first,
// bounds, or trailing garbage refuse the WHOLE file (ok=false), never a
// half-loaded conversation.
package main

import (
	"strconv"
	"strings"
)

const (
	convMagicV1   = "GOASSIST-CHAT v1"
	convMagicV2   = "GOASSIST-CHAT v2"
	convMagicV3   = "GOASSIST-CHAT v3"
	convTitleMark = "@@title "
	convExt       = ".CHAT"
	convMark      = "@@"
	convMaxTurns  = 256
	convMaxBytes  = 256 * 1024
	convNameMax   = 24
	convTitleMax  = 48

	// Act bounds: the verb name rides the @@call line, its args the
	// body; a @@result body holds verb output cut at convOutMax — past
	// the bound the writer keeps the cut and ends the body with the
	// truncation mark.
	convVerbMax   = 24
	convArgsMax   = 1 << 10
	convOutMax    = 8 << 10
	convReasonMax = 48
	convTruncMark = "… output truncated"

	roleUser    = "user"
	roleModel   = "assistant"
	roleMeta    = "meta"
	roleSystem  = "system"
	roleCall    = "call"
	roleConsent = "consent"
	roleResult  = "result"
)

// turn is one side of the exchange. role is roleUser, roleModel, roleMeta,
// or — only as turn 0 — roleSystem carrying the preset's prompt. The v3
// act records (roleCall, roleConsent, roleResult) also carry arg, the
// marker line's parameter: the verb, the consent answer, or the result
// state.
type turn struct {
	role string
	arg  string
	text string
}

// isActRole reports whether role is one of the v3 act records.
func isActRole(role string) bool {
	return role == roleCall || role == roleConsent || role == roleResult
}

// conversation is the in-memory chat: a stable name (its file's stem), an
// optional display title, plus the turns, oldest first.
type conversation struct {
	name  string
	title string
	turns []turn
}

// encodeConv serializes c. A body line that begins with the turn marker
// is space-protected so decode never splits a turn mid-body.
func encodeConv(c *conversation) []byte {
	var sb strings.Builder
	ver := 1
	if c.title != "" {
		ver = 2
	}
	for _, t := range c.turns {
		switch {
		case isActRole(t.role):
			ver = 3
		case t.role == roleSystem && ver < 2:
			ver = 2
		}
	}
	switch ver {
	case 3:
		sb.WriteString(convMagicV3 + "\n")
	case 2:
		sb.WriteString(convMagicV2 + "\n")
	default:
		sb.WriteString(convMagicV1 + "\n")
	}
	if ver >= 2 && c.title != "" {
		sb.WriteString(convTitleMark + c.title + "\n")
	}
	for _, t := range c.turns {
		if isActRole(t.role) && t.arg != "" {
			sb.WriteString(convMark + t.role + " " + t.arg + "\n")
		} else {
			sb.WriteString(convMark + t.role + "\n")
		}
		for _, ln := range strings.Split(t.text, "\n") {
			if strings.HasPrefix(ln, convMark) || strings.HasPrefix(ln, " "+convMark) {
				sb.WriteByte(' ')
			}
			sb.WriteString(ln + "\n")
		}
	}
	return []byte(sb.String())
}

// decodeConv is encodeConv's strict inverse. Every bound is checked; a
// refusal is total.
func decodeConv(b []byte) (*conversation, bool) {
	if len(b) == 0 || len(b) > convMaxBytes {
		return nil, false
	}
	s := string(b)
	ver := 1
	switch {
	case strings.HasPrefix(s, convMagicV1+"\n"):
		s = s[len(convMagicV1)+1:]
	case strings.HasPrefix(s, convMagicV2+"\n"):
		ver = 2
		s = s[len(convMagicV2)+1:]
	case strings.HasPrefix(s, convMagicV3+"\n"):
		ver = 3
		s = s[len(convMagicV3)+1:]
	default:
		return nil, false
	}
	var c conversation
	if ver >= 2 && strings.HasPrefix(s, convTitleMark) {
		rest := s[len(convTitleMark):]
		end := strings.IndexByte(rest, '\n')
		if end < 0 {
			end = len(rest)
		}
		title := rest[:end]
		if !validTitle(title) {
			return nil, false
		}
		c.title = title
		s = rest[end:]
		if len(s) > 0 {
			s = s[1:]
		}
	}
	var cur *turn
	// act tracks the open act record: @@consent must immediately follow
	// a @@call and @@result the call or its consent. Any other record
	// closes the act — a consent or result with no call behind it is a
	// refusal, not a skip.
	const (
		actNone = iota
		actOpen
		actConsent
	)
	act := actNone
	for _, ln := range strings.Split(s, "\n") {
		if strings.HasPrefix(ln, convMark) {
			field := ln[len(convMark):]
			role, arg := field, ""
			if i := strings.IndexByte(field, ' '); i >= 0 {
				role, arg = field[:i], field[i+1:]
			}
			ok := false
			switch role {
			case roleUser, roleModel, roleMeta:
				ok = arg == ""
				act = actNone
			case roleSystem:
				ok = ver >= 2 && arg == "" && len(c.turns) == 0
				act = actNone
			case roleCall:
				ok = ver >= 3 && validVerb(arg)
				if ok {
					act = actOpen
				}
			case roleConsent:
				ok = ver >= 3 && act == actOpen &&
					(arg == "allow" || arg == "deny")
				if ok {
					act = actConsent
				}
			case roleResult:
				ok = ver >= 3 && act != actNone && validResultArg(arg)
				if ok {
					act = actNone
				}
			}
			if !ok {
				return nil, false
			}
			if len(c.turns) >= convMaxTurns {
				return nil, false
			}
			c.turns = append(c.turns, turn{role: role, arg: arg})
			cur = &c.turns[len(c.turns)-1]
			continue
		}
		if cur == nil {
			// Body text before the first marker, or the file's trailing
			// newline after the last turn — only the empty string is legal.
			if ln != "" {
				return nil, false
			}
			continue
		}
		if strings.HasPrefix(ln, " "+convMark) {
			ln = ln[1:]
		}
		if cur.text != "" {
			cur.text += "\n"
		}
		cur.text += ln
	}
	// Split's trailing "" after a final newline lands inside the last turn
	// as a phantom empty line; drop exactly that one.
	if cur != nil && strings.HasSuffix(cur.text, "\n") {
		cur.text = strings.TrimSuffix(cur.text, "\n")
	}
	// Act bodies carry their own bounds: call args stay small, a consent
	// is a bare record, and an over-bound result body is only legal when
	// the writer cut it under the truncation mark.
	for _, t := range c.turns {
		switch t.role {
		case roleCall:
			if len(t.text) > convArgsMax {
				return nil, false
			}
		case roleConsent:
			if t.text != "" {
				return nil, false
			}
		case roleResult:
			if !validResultBody(t.text) {
				return nil, false
			}
		}
	}
	// A turn-less file is a legitimate empty conversation — newConv saves
	// one the moment it allocates the name.
	return &c, true
}

// validVerb is the @@call parameter grammar: a dotted lowercase verb
// name (proc.list, fs.read, ui.windows, …), 1..24 bytes. The codec
// checks shape, not membership — which verbs exist is the dispatcher's
// question, and a file must not stop decoding because a verb went away.
func validVerb(s string) bool {
	if len(s) < 1 || len(s) > convVerbMax || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isWordByte(s[i]) && s[i] != '.' {
			return false
		}
	}
	return true
}

// validReason is the @@result refuse:<reason> grammar: the named
// refusal as a single token (unknown_verb, cap_required:file, denied).
func validReason(s string) bool {
	if len(s) < 1 || len(s) > convReasonMax {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isWordByte(s[i]) && s[i] != ':' && s[i] != '.' {
			return false
		}
	}
	return true
}

func isWordByte(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '_'
}

// validResultArg is the @@result parameter: ok, or refuse:<reason>.
func validResultArg(a string) bool {
	if a == "ok" {
		return true
	}
	return strings.HasPrefix(a, "refuse:") && validReason(a[len("refuse:"):])
}

// validResultBody bounds a @@result body: at or under convOutMax it is
// legal as-is; past the bound only the writer's cut is, ending on the
// truncation mark.
func validResultBody(t string) bool {
	if len(t) <= convOutMax {
		return true
	}
	return strings.HasSuffix(t, "\n"+convTruncMark) &&
		len(t)-len(convTruncMark)-1 <= convOutMax
}

// cutResultBody is the writer side of the @@result bound: verb output
// past convOutMax is stored cut, with the named truncation mark as the
// body's last line so the truncation is visible, not silent.
func cutResultBody(out string) string {
	if len(out) <= convOutMax {
		return out
	}
	return out[:convOutMax] + "\n" + convTruncMark
}

// validConvName is the C#### grammar the store generates: a file a user
// dropped beside them can be listed, but only generated names count when
// allocating the next one.
func validConvName(s string) bool {
	if len(s) < 2 || len(s) > convNameMax {
		return false
	}
	if s[0] != 'C' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// nextConvName allocates the lowest free C0001..C9999 over the names the
// store listed; "" means the space is exhausted (a named refusal upstream,
// not a wraparound).
func nextConvName(have []string) string {
	used := map[int]bool{}
	for _, n := range have {
		if !validConvName(n) || len(n) > 5 {
			continue
		}
		if v, err := strconv.Atoi(n[1:]); err == nil {
			used[v] = true
		}
	}
	for i := 1; i <= 9999; i++ {
		if !used[i] {
			return "C" + pad4(i)
		}
	}
	return ""
}

func pad4(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

// validTitle is the title bound: 1..48 bytes, single-line. Rename treats
// the empty string as "clear the title" before this is ever consulted.
func validTitle(t string) bool {
	return len(t) >= 1 && len(t) <= convTitleMax &&
		!strings.ContainsAny(t, "\r\n")
}

// convTitleOf is the listing's cheap header read: the title a v2/v3
// file's head carries, "" for v1, malformed, or untitled. It parses only
// the first two lines, so callers bound the read small.
func convTitleOf(b []byte) string {
	s := string(b)
	switch {
	case strings.HasPrefix(s, convMagicV2+"\n"):
		s = s[len(convMagicV2)+1:]
	case strings.HasPrefix(s, convMagicV3+"\n"):
		s = s[len(convMagicV3)+1:]
	default:
		return ""
	}
	if !strings.HasPrefix(s, convTitleMark) {
		return ""
	}
	rest := s[len(convTitleMark):]
	end := strings.IndexByte(rest, '\n')
	if end < 0 {
		end = len(rest)
	}
	t := rest[:end]
	if !validTitle(t) {
		return ""
	}
	return t
}

// displayTitle is the list/title-bar label: the stored title when one
// exists, "Conversation N" for a generated C#### name with none (the v1
// file's reading), else the raw stem for a file dropped beside them.
func displayTitle(name, title string) string {
	if title != "" {
		return title
	}
	if validConvName(name) {
		if v, err := strconv.Atoi(name[1:]); err == nil {
			return "Conversation " + strconv.Itoa(v)
		}
	}
	return name
}
