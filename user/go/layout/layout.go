// Package layout contains the data tables for the keyboard layouts the
// kernel's measured HID-to-symbol translation point supports. The kernel
// carries the same small tables in Zig; these are the Go-side fixtures and
// settings vocabulary, not a second input translation path.
package layout

// ID names a supported keyboard layout.
type ID string

const (
	US ID = "us"
	DE ID = "de"
)

type key struct {
	usage   uint8
	plain   rune
	shifted rune
}

type table struct {
	id            ID
	letters       string // one unshifted symbol per HID usage 0x04..0x1d
	shiftedDigits []rune // HID usages 0x1e..0x27
	punctuation   []key
}

var tables = [...]table{
	{
		id:            US,
		letters:       "abcdefghijklmnopqrstuvwxyz",
		shiftedDigits: []rune("!@#$%^&*()"),
		punctuation: []key{
			{0x2d, '-', '_'}, {0x2e, '=', '+'},
			{0x2f, '[', '{'}, {0x30, ']', '}'},
			{0x31, '\\', '|'}, {0x33, ';', ':'},
			{0x34, '\'', '"'}, {0x35, '`', '~'},
			{0x36, ',', '<'}, {0x37, '.', '>'},
			{0x38, '/', '?'},
		},
	},
	{
		id: DE,
		// German QWERTZ swaps the symbols on the physical Y and Z keys.
		letters:       "abcdefghijklmnopqrstuvwxzy",
		shiftedDigits: []rune("!\"§$%&/()="),
		punctuation: []key{
			{0x2d, 'ß', '?'}, {0x2f, 'ü', 'Ü'},
			{0x30, '+', '*'}, {0x31, '#', '\''},
			{0x33, 'ö', 'Ö'}, {0x34, 'ä', 'Ä'},
			{0x35, '^', '°'}, {0x36, ',', ';'},
			{0x37, '.', ':'}, {0x38, '-', '_'},
			// The extra ISO key immediately left of Z is not present on
			// US ANSI keyboards. On German keyboards it types angle brackets.
			{0x64, '<', '>'},
		},
	},
}

// Values returns a fresh copy of the setting vocabulary in display order.
func Values() []string {
	return []string{string(US), string(DE)}
}

// Parse recognizes a supported setting value.
func Parse(name string) (ID, bool) {
	for _, t := range tables {
		if name == string(t.id) {
			return t.id, true
		}
	}
	return "", false
}

// Translate maps one keyboard boot-protocol usage through a layout table.
// Return false when the usage has no printable symbol in that layout.
func Translate(id ID, usage uint8, shift bool) (rune, bool) {
	var t *table
	for i := range tables {
		if tables[i].id == id {
			t = &tables[i]
			break
		}
	}
	if t == nil {
		return 0, false
	}
	if usage >= 0x04 && usage <= 0x1d {
		r := rune(t.letters[usage-0x04])
		if shift && r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		return r, true
	}
	if usage >= 0x1e && usage <= 0x27 {
		i := usage - 0x1e
		if shift {
			return t.shiftedDigits[i], true
		}
		return rune("1234567890"[i]), true
	}
	for _, k := range t.punctuation {
		if k.usage == usage {
			if shift {
				return k.shifted, true
			}
			return k.plain, true
		}
	}
	switch usage {
	case 0x28:
		return '\n', true
	case 0x2a:
		return '\b', true
	case 0x2b:
		return '\t', true
	case 0x2c:
		return ' ', true
	default:
		return 0, false
	}
}
