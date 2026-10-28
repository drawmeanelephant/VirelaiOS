package main

// compose.go — M208b (issue #3823): the window's compose pane — To,
// Subject, Body — submitting through the exact send path the CLI's
// `send` verb runs (runSend), so the markers and refusals it emits are
// the same vocabulary ('gomail: mail', 'gomail: rcpt', 'gomail: sent
// bytes=', the named refusals). 'c' opens the pane, Tab/Enter walk the
// fields (Enter in Body is a newline — the pane's only multi-line
// field), printable bytes append, Backspace trims a rune, Ctrl-S
// submits, Esc cancels. Field bounds are named constants — the pane is
// a drafts-free one-shot, no persistence: out of scope by the card.
//
// The model is host-testable: composeKey mutates fields and reports the
// action; the draw pass only reads. sendFn is the wire seam — runSend
// on the guest, a stub in tests.

import "strings"

const (
	compFieldMax = 120      // To / Subject byte bound
	compBodyMax  = 4 * 1024 // Body byte bound
)

const (
	compFieldTo = iota // field ring: To -> Subject -> Body
	compFieldSubject
	compFieldBody
	compFieldCount
)

// composeModel is the pane's state — the field texts and the focused
// one. live says the pane owns the keyboard.
type composeModel struct {
	live    bool
	to      []byte
	subject []byte
	body    []byte
	field   int
}

// compAct is what a key did — the view repaints on any of them.
type compAct int

const (
	compNone compAct = iota
	compEdit
	compCancel
	compSend
)

// fieldText returns the focused field's buffer.
func (c *composeModel) buf() *[]byte {
	switch c.field {
	case compFieldTo:
		return &c.to
	case compFieldSubject:
		return &c.subject
	}
	return &c.body
}

func (c *composeModel) fieldBound() int {
	if c.field == compFieldBody {
		return compBodyMax
	}
	return compFieldMax
}

// recipients decodes the To field into runSend's --to list: commas
// separate, whitespace trims, empties drop — a bare "alice" is one
// rcpt, "alice, carol" two.
func (c *composeModel) recipients() []string {
	var out []string
	for _, t := range strings.Split(string(c.to), ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// composeKey routes one EvKeyDown while the pane lives. It swallows
// every printable byte and the editing cluster; nothing leaks to the
// mail panes' letter bindings (a typed 'q' is a 'q', not quit).
func (c *composeModel) composeKey(arg0, arg1 uint32, ctrl bool) compAct {
	if ctrl {
		// Ctrl-S submits; every other ctrl chord is the pane's to ignore.
		if byte(arg1) == 's' || byte(arg1) == 'S' || arg0 == 0x16 {
			return compSend
		}
		return compNone
	}
	switch arg0 {
	case 0x29: // Esc — cancel back to the mail panes.
		c.live = false
		return compCancel
	case 0x2b: // Tab — next field, always.
		c.field = (c.field + 1) % compFieldCount
		return compEdit
	case 0x28: // Enter — newline in Body, next field above it.
		if c.field == compFieldBody {
			return c.put('\n')
		}
		c.field = (c.field + 1) % compFieldCount
		return compEdit
	case 0x2a: // Backspace — one rune off the focused field.
		b := c.buf()
		if n := len(*b); n > 0 {
			cut := n - 1
			for cut > 0 && (*b)[cut]&0xc0 == 0x80 {
				cut--
			}
			*b = (*b)[:cut]
		}
		return compEdit
	case 0x4f: // Right — next field (the pane's only horizontal).
		c.field = (c.field + 1) % compFieldCount
		return compEdit
	case 0x50: // Left — previous field.
		c.field = (c.field + compFieldCount - 1) % compFieldCount
		return compEdit
	}
	if arg1 >= 0x20 && arg1 < 0x7f {
		return c.put(byte(arg1))
	}
	return compNone
}

// put appends one byte inside the field bound — a full field refuses
// silently (the bound is the pane's, the text stays whole).
func (c *composeModel) put(ch byte) compAct {
	b := c.buf()
	if len(*b) >= c.fieldBound() {
		return compEdit
	}
	*b = append(*b, ch)
	return compEdit
}

// composeLines renders the pane's rows for the draw pass: a title, the
// two single-line fields, the body text unwrapped, and the key hint.
// The focused field's row carries the '>' marker.
func (c *composeModel) composeLines() []string {
	out := []string{"Compose"}
	out = append(out, fieldRow("To", string(c.to), c.field == compFieldTo))
	out = append(out, fieldRow("Subject", string(c.subject), c.field == compFieldSubject))
	out = append(out, fieldRow("Body", "", c.field == compFieldBody))
	for _, l := range strings.Split(string(c.body), "\n") {
		out = append(out, "  "+l)
	}
	out = append(out, "")
	out = append(out, "tab/enter next · ctrl-s send · esc cancel")
	return out
}

func fieldRow(name, val string, focus bool) string {
	mark := " "
	if focus {
		mark = ">"
	}
	return mark + " " + name + ": " + val
}
