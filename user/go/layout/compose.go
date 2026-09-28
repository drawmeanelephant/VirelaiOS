package layout

// Accent is a pending dead-key accent, spelled as the spacing character it
// falls back to when nothing composes with it.
type Accent rune

const (
	Acute      Accent = '\u00b4' // ´
	Grave      Accent = '`'
	Circumflex Accent = '^'
)

// Literal is the character a stray accent resolves to: the accent itself.
func (a Accent) Literal() rune { return rune(a) }

// Name is the accent's plain-ASCII name, for console markers.
func (a Accent) Name() string {
	switch a {
	case Acute:
		return "acute"
	case Grave:
		return "grave"
	case Circumflex:
		return "circumflex"
	}
	return "unknown"
}

// Mark is the accent as the 8x8 face can paint it. The face has no glyph for
// the spacing acute (it draws U+00B4 as '?'), so a painter showing a pending
// accent uses this instead of Literal. Only the acute differs.
func (a Accent) Mark() rune {
	if a == Acute {
		return '\''
	}
	return rune(a)
}

// DeadKey reports the accent a physical key stages under a layout. It is
// decided by usage and shift alone, never by the symbol the kernel translated,
// because a layout's dead key can still carry a literal (the German circumflex
// key arrives as '^'). A layout with no dead keys, or an unknown one, reports
// false for every key.
func DeadKey(id ID, usage uint32, shift bool) (Accent, bool) {
	t := lookup(id)
	if t == nil || usage > 0xff {
		return 0, false
	}
	for _, d := range t.dead {
		if uint32(d.usage) == usage && d.shifted == shift {
			return d.accent, true
		}
	}
	return 0, false
}

// composeRow lists one accent's results: base[i] composes to out's i-th rune.
// Every result is one precomposed codepoint; sequences that need a base plus
// a combining mark are out of scope.
type composeRow struct {
	accent Accent
	base   string
	out    string
}

var composeRows = [...]composeRow{
	{Acute, "aeiouyAEIOUY", "áéíóúýÁÉÍÓÚÝ"},
	{Grave, "aeiouAEIOU", "àèìòùÀÈÌÒÙ"},
	{Circumflex, "aeiouAEIOU", "âêîôûÂÊÎÔÛ"},
}

// Compose resolves a pending accent followed by a base character. Space and
// the accent's own spacing character both give the bare accent, the usual way
// to type one on purpose. The second result is false for every other pair, and
// the caller then types the accent and the base as two literal characters.
func Compose(a Accent, base rune) (rune, bool) {
	if base == ' ' || base == rune(a) {
		return rune(a), true
	}
	for _, row := range composeRows {
		if row.accent != a {
			continue
		}
		i := 0
		for _, out := range row.out {
			if rune(row.base[i]) == base {
				return out, true
			}
			i++
		}
	}
	return 0, false
}
