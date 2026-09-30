package layout

import (
	"testing"
	"unicode/utf8"
)

func TestDeadKeysBelongToTheLayoutNotTheSymbol(t *testing.T) {
	cases := []struct {
		id    ID
		usage uint32
		shift bool
		want  Accent
		ok    bool
	}{
		{DE, 0x2e, false, Acute, true},
		{DE, 0x2e, true, Grave, true},
		{DE, 0x35, false, Circumflex, true},
		// Shift+circumflex key is the degree sign, a plain symbol.
		{DE, 0x35, true, 0, false},
		{DE, 0x08, false, 0, false},
		// US has no dead keys, and 0x2e there is a plain '='.
		{US, 0x2e, false, 0, false},
		{US, 0x35, false, 0, false},
		{"", 0x2e, false, 0, false},
		{"fr", 0x2e, false, 0, false},
		// A usage that does not fit a boot-protocol byte must not wrap into one.
		{DE, 0x12e, false, 0, false},
	}
	for _, tc := range cases {
		got, ok := DeadKey(tc.id, tc.usage, tc.shift)
		if ok != tc.ok || got != tc.want {
			t.Errorf("DeadKey(%q, %#x, shift=%v) = (%q,%v), want (%q,%v)",
				tc.id, tc.usage, tc.shift, got, ok, tc.want, tc.ok)
		}
	}
	// The kernel types the German circumflex key as a literal '^', which is why
	// the stage must decide by usage; pin that so the two tables stay aligned.
	if r, ok := Translate(DE, 0x35, false); !ok || r != '^' {
		t.Fatalf("Translate(DE, 0x35) = (%q,%v), want the literal '^' the kernel emits", r, ok)
	}
}

func TestComposeResolvesAccentPlusBase(t *testing.T) {
	cases := []struct {
		a    Accent
		base rune
		want rune
		ok   bool
	}{
		{Acute, 'e', 'é', true},
		{Acute, 'E', 'É', true},
		{Acute, 'a', 'á', true},
		{Acute, 'y', 'ý', true},
		{Grave, 'e', 'è', true},
		{Grave, 'U', 'Ù', true},
		{Circumflex, 'o', 'ô', true},
		{Circumflex, 'I', 'Î', true},
		// The bare accent, typed on purpose.
		{Acute, ' ', '´', true},
		{Acute, '´', '´', true},
		{Grave, ' ', '`', true},
		{Circumflex, ' ', '^', true},
		{Circumflex, '^', '^', true},
		// Strays compose to nothing; the caller types both characters.
		{Acute, 'x', 0, false},
		{Acute, 'z', 0, false},
		{Grave, 'y', 0, false},
		{Circumflex, '1', 0, false},
		{Acute, 'é', 0, false},
		{Accent('~'), 'a', 0, false},
	}
	for _, tc := range cases {
		got, ok := Compose(tc.a, tc.base)
		if ok != tc.ok || got != tc.want {
			t.Errorf("Compose(%q, %q) = (%q,%v), want (%q,%v)", tc.a, tc.base, got, ok, tc.want, tc.ok)
		}
	}
}

func TestComposeRowsAreWellFormed(t *testing.T) {
	seen := map[[2]rune]bool{}
	for _, row := range composeRows {
		if n, m := len(row.base), utf8.RuneCountInString(row.out); n != m {
			t.Fatalf("%q row has %d bases but %d results", row.accent, n, m)
		}
		i := 0
		for _, out := range row.out {
			base := rune(row.base[i])
			i++
			if base >= 0x80 || !(base >= 'a' && base <= 'z' || base >= 'A' && base <= 'Z') {
				t.Errorf("%q base %q is not an ASCII letter", row.accent, base)
			}
			if out < 0xc0 || out > 0xff {
				t.Errorf("%q+%q composes to %q, want a precomposed Latin-1 letter", row.accent, base, out)
			}
			key := [2]rune{rune(row.accent), base}
			if seen[key] {
				t.Errorf("duplicate compose entry %q+%q", row.accent, base)
			}
			seen[key] = true
		}
	}
	// Every accent a layout can stage has a row, or its keys would only ever
	// produce literals.
	hasRow := func(a Accent) bool {
		for _, row := range composeRows {
			if row.accent == a {
				return true
			}
		}
		return false
	}
	for _, tab := range tables {
		for _, d := range tab.dead {
			if !hasRow(d.accent) {
				t.Errorf("%s stages %q but the compose table has no row for it", tab.id, d.accent)
			}
		}
	}
}

func TestAccentMarkIsPaintableInTheEightByEightFace(t *testing.T) {
	for _, a := range []Accent{Acute, Grave, Circumflex} {
		if m := a.Mark(); m < 0x20 || m > 0x7e {
			t.Errorf("Mark(%q) = %q, outside the face's ASCII coverage", a, m)
		}
	}
	if Acute.Literal() != '´' || Grave.Literal() != '`' || Circumflex.Literal() != '^' {
		t.Fatal("Literal must be the spacing accent itself")
	}
	names := map[Accent]string{Acute: "acute", Grave: "grave", Circumflex: "circumflex", Accent('~'): "unknown"}
	for a, want := range names {
		if got := a.Name(); got != want {
			t.Errorf("Name(%q) = %q, want %q", a, got, want)
		}
	}
}
