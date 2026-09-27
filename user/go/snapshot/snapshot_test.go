// M81g (#1767) — the bundle codec's own tests.
//
// The card's promise is a bundle that is WHOLE or nothing, so most of this
// file is the refusal table: for every shape a bundle can be wrong in, the
// test pins the reason, because a gate that asserts "the restore refused"
// wants to pin WHICH doubt fired. The two properties that are easy to get
// wrong and expensive to get wrong — the length framing and the path-traversal
// refusal — get their own named tests rather than a line in a table.
package snapshot

import (
	"bytes"
	"strings"
	"testing"
)

// A body that is not text at all: the session strip is BINARY, and a codec
// that could only carry printable bytes could not carry the state the card is
// about. It also contains a byte sequence that LOOKS like an entry header, so
// the framing test below has teeth.
var binaryBody = []byte{
	0x00, 0xff, 0x0a, 0x0a, 's', 'e', 't', 't', 'i', 'n', 'g', 's', ' ', '9', 0x0a,
	'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 'x', 0x7f, 0x80, 0x1b,
}

func TestRoundTripIsByteExactIncludingBinary(t *testing.T) {
	in := []Entry{
		{Name: EntrySettings, Body: []byte("#v2\ntheme=dark\n")},
		{Name: EntrySession, Body: binaryBody},
		{Name: DocsPrefix + "NOTE.TXT", Body: []byte("hello\n")},
		{Name: DocsPrefix + "empty.txt", Body: nil},
	}
	raw, ok := Build(in)
	if !ok {
		t.Fatal("Build refused a legal bundle")
	}
	b, ok, reason := Parse(raw)
	if !ok {
		t.Fatalf("Parse refused a bundle Build wrote: %s", reason)
	}
	if len(b.Entries) != len(in) {
		t.Fatalf("entries = %d want %d", len(b.Entries), len(in))
	}
	for i := range in {
		if b.Entries[i].Name != in[i].Name {
			t.Fatalf("entry %d name = %q want %q", i, b.Entries[i].Name, in[i].Name)
		}
		if !bytes.Equal(b.Entries[i].Body, in[i].Body) {
			t.Fatalf("entry %d body = %v want %v", i, b.Entries[i].Body, in[i].Body)
		}
	}
	// A nil body must come back as an empty body, not as absent: a
	// zero-length document is a document, and restoring "absent" would
	// quietly turn an empty file into a missing one.
	got, found := b.Get(DocsPrefix + "empty.txt")
	if !found || len(got) != 0 {
		t.Fatalf("empty body entry: found=%v len=%d", found, len(got))
	}
}

// The framing property, on its own: a body may contain any bytes, including a
// complete fake entry header, because a parser consumes exactly the declared
// count and NEVER re-interprets a body byte as structure. If this breaks, a
// document that happens to contain the header text would be split into two
// entries and the bundle would restore the wrong file.
func TestBodyContainingAnEntryHeaderIsNotRescanned(t *testing.T) {
	forged := []byte("#vb1\nsession 5\nHELLO")
	raw, ok := Build([]Entry{
		{Name: EntrySession, Body: forged},
		{Name: EntrySettings, Body: []byte("s")},
	})
	if !ok {
		t.Fatal("Build refused")
	}
	b, ok, reason := Parse(raw)
	if !ok {
		t.Fatalf("Parse refused: %s", reason)
	}
	if len(b.Entries) != 2 {
		t.Fatalf("entries = %d, want 2 — the body was rescanned for structure", len(b.Entries))
	}
	if !bytes.Equal(b.Entries[0].Body, forged) {
		t.Fatalf("body = %v want %v", b.Entries[0].Body, forged)
	}
	if b.Entries[1].Name != EntrySettings {
		t.Fatalf("second entry = %q want %q", b.Entries[1].Name, EntrySettings)
	}
}

// Path traversal is the security-shaped refusal, so it is tested by name: the
// docs namespace is mapped onto a real directory by the consumer, and a bundle
// that can name `..` (or a nested path, or a leading dot) could place a file
// outside it.
func TestNamesCannotEscapeTheDocsDirectory(t *testing.T) {
	// Full entry names, as they appear in a bundle.
	badNames := []string{
		"..", ".", ".hidden", "docs/..", "docs/../../evil", "docs/a/b",
		"a/b", "with space", "with\ttab", "with\nnewline", "nul\x00byte",
		strings.Repeat("n", MaxNameLen+1), "", "docs", "other", "DOCS/a",
	}
	for _, name := range badNames {
		if KnownEntry(name) {
			t.Errorf("KnownEntry(%q) = true, want false", name)
		}
		if _, ok := Build([]Entry{{Name: name, Body: []byte("x")}}); ok {
			t.Errorf("Build accepted the entry name %q", name)
		}
		// And the PARSER must refuse it too, not just the builder: a
		// bundle is untrusted input at restore time.
		forged := string(Header(1)) + name + " 1\nx"
		if _, ok, _ := Parse([]byte(forged)); ok {
			t.Errorf("Parse accepted the entry name %q", name)
		}
	}
	// Bare document SEGMENTS, which is what a share listing hands a caller
	// and therefore what the exported ValidName is for.
	badSegments := []string{"..", ".", ".hidden", "a/b", "with space", ""}
	for _, s := range badSegments {
		if ValidName(s) {
			t.Errorf("ValidName(%q) = true, want false", s)
		}
	}
	good := []string{EntrySettings, EntrySession, DocsPrefix + "a",
		DocsPrefix + "NOTE.TXT", DocsPrefix + "a-b_c.d"}
	for _, name := range good {
		if !KnownEntry(name) {
			t.Errorf("KnownEntry(%q) = false, want true", name)
		}
	}
	goodSegments := []string{"a", "NOTE.TXT", "note.txt", "a-b_c.d", "0", "x9"}
	for _, s := range goodSegments {
		if !ValidName(s) {
			t.Errorf("ValidName(%q) = false, want true", s)
		}
	}
}

// An entry name this version does not know is a refusal, not a skip: that is
// the settings header gate's rule (a newer schema is refused whole), and it
// is what keeps a restore from silently dropping state a newer producer wrote.
func TestUnknownEntryNameIsRefusedWhole(t *testing.T) {
	raw := string(Header(1)) + "somethingelse 2\nhi"
	b, ok, reason := Parse([]byte(raw))
	if ok {
		t.Fatal("Parse accepted an entry name this version cannot map")
	}
	if reason != ReasonEntryName {
		t.Fatalf("reason = %q want %q", reason, ReasonEntryName)
	}
	if len(b.Entries) != 0 {
		t.Fatal("a refused bundle must carry no entries at all")
	}
}

func TestRefusalReasons(t *testing.T) {
	// Every `in` below is a BUNDLE BODY, so each one but the header cases
	// carries the real header: a refusal table that fed Parse a bare entry
	// line would only ever prove ReasonHeader over and over.
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"not a bundle", "hello\n", ReasonHeader},
		{"older header", "#vb0 1\nsettings 1\nx", ReasonHeader},
		{"newer header", "#vb2 1\nsettings 1\nx", ReasonHeader},
		{"header alone", "#vb1", ReasonHeader},
		{"no count", "#vb1\nsettings 1\nx", ReasonHeader},
		{"header plus nothing", string(Header(1)), ReasonEmpty},
		{"zero entries declared", "#vb1 0\n", ReasonCount},
		{"count over the cap", "#vb1 99\n", ReasonCount},
		{"non-digit count", "#vb1 x\nsettings 1\nx", ReasonCount},
		{"no separator", string(Header(1)) + "settings1\nx", ReasonEntryHeader},
		{"name only", string(Header(1)) + "settings\n", ReasonEntryHeader},
		{"two separators", string(Header(1)) + "settings 1 2\nx", ReasonEntryHeader},
		{"negative length", string(Header(1)) + "settings -1\n", ReasonEntryHeader},
		{"plus length", string(Header(1)) + "settings +1\nx", ReasonEntryHeader},
		{"non-digit length", string(Header(1)) + "settings 1x\nx", ReasonEntryHeader},
		{"space in length", string(Header(1)) + "settings  1\nx", ReasonEntryHeader},
		{"huge length field", string(Header(1)) + "settings 1234567890\nx", ReasonEntryHeader},
		{"over MaxBody", string(Header(1)) + "settings " + itoa(MaxBody+1) + "\n", ReasonEntrySize},
		{"body short", string(Header(1)) + "settings 8\nabc", ReasonTruncated},
		{"body absent", string(Header(1)) + "settings 4\n", ReasonTruncated},
		{"duplicate name", string(Header(2)) + "settings 1\nxsettings 1\ny", ReasonDuplicate},
		// Bytes after the last body are read as the next entry header, and
		// they are not one. Refusing here is the same all-or-nothing
		// refusal; the reason names what was actually wrong.
		{"junk after the last body", string(Header(1)) + "settings 1\nxJUNK", ReasonEntryHeader},
		{"unknown name", string(Header(1)) + "nope 1\nx", ReasonEntryName},
		{"docs entry with a bad segment", string(Header(1)) + "docs/.. 1\nx", ReasonEntryName},
		{"docs namespace alone", string(Header(1)) + "docs/ 1\nx", ReasonEntryName},
		// The count is the whole point of the header: a container cut at an
		// entry BOUNDARY is otherwise a well-formed smaller bundle.
		{"two declared, one present", string(Header(2)) + "settings 1\nx", ReasonEntryCount},
		{"one declared, two present", string(Header(1)) + "settings 1\nxsession 1\ny", ReasonEntryCount},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ok, reason := Parse([]byte(c.in))
			if ok {
				t.Fatalf("Parse accepted %q, want refusal %s", c.in, c.want)
			}
			if reason != c.want {
				t.Fatalf("reason = %q want %q", reason, c.want)
			}
		})
	}
}

// A bundle claiming more entries than the container allows is refused rather
// than partially honoured, and the cap must be exactly MaxEntries — one more
// is a refusal, so the boundary is tested on both sides.
func TestEntryCountCap(t *testing.T) {
	mk := func(n int) []Entry {
		out := make([]Entry, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, Entry{Name: DocsPrefix + "f" + itoa(i), Body: []byte("x")})
		}
		return out
	}
	if _, ok := Build(mk(MaxEntries + 1)); ok {
		t.Fatalf("Build accepted %d entries, want a refusal past %d", MaxEntries+1, MaxEntries)
	}
	raw, ok := Build(mk(MaxEntries))
	if !ok {
		t.Fatalf("Build refused exactly %d entries", MaxEntries)
	}
	if b, ok, reason := Parse(raw); !ok || len(b.Entries) != MaxEntries {
		t.Fatalf("Parse of a full bundle: ok=%v reason=%s n=%d", ok, reason, len(b.Entries))
	}
	// A count past the cap is refused by the header itself.
	if _, ok, reason := Parse(append(Header(MaxEntries+1), DocsPrefix+"f0 1\nx"...)); ok ||
		reason != ReasonCount {
		t.Fatalf("a count past the cap: ok=%v reason=%q want %q", ok, reason, ReasonCount)
	}
	// And a LEGAL count with one entry too many is refused by the body.
	forged := string(Header(MaxEntries))
	for i := 0; i <= MaxEntries; i++ {
		forged += DocsPrefix + "f" + itoa(i) + " 1\nx"
	}
	if _, ok, reason := Parse([]byte(forged)); ok || reason != ReasonTooMany {
		t.Fatalf("oversized bundle: ok=%v reason=%q want %q", ok, reason, ReasonTooMany)
	}
}

func TestBuildRefusals(t *testing.T) {
	if _, ok := Build(nil); ok {
		t.Fatal("Build accepted an empty bundle")
	}
	if _, ok := Build([]Entry{{Name: EntrySettings, Body: make([]byte, MaxBody+1)}}); ok {
		t.Fatal("Build accepted an oversized body")
	}
	dup := []Entry{{Name: EntrySettings, Body: []byte("a")}, {Name: EntrySettings, Body: []byte("b")}}
	if _, ok := Build(dup); ok {
		t.Fatal("Build accepted a duplicate name instead of failing the capture")
	}
	// A name this version cannot map must fail the CAPTURE, so the person
	// finds out when they press the chord rather than on a later boot.
	if _, ok := Build([]Entry{{Name: "settings.txt", Body: []byte("a")}}); ok {
		t.Fatal("Build accepted an entry name Parse would refuse")
	}
}

func TestGetDocsAndCount(t *testing.T) {
	raw, _ := Build([]Entry{
		{Name: EntrySettings, Body: []byte("s")},
		{Name: EntrySession, Body: []byte("e")},
		{Name: DocsPrefix + "B.TXT", Body: []byte("b")},
		{Name: DocsPrefix + "A.TXT", Body: []byte("a")},
	})
	b, ok, _ := Parse(raw)
	if !ok {
		t.Fatal("Parse refused")
	}
	if got, found := b.Get(EntrySettings); !found || string(got) != "s" {
		t.Fatalf("Get(settings) = %q found=%v", got, found)
	}
	if _, found := b.Get("docs/NOPE.TXT"); found {
		t.Fatal("Get reported a document the bundle does not carry")
	}
	docs := b.Docs()
	if len(docs) != 2 {
		t.Fatalf("Docs() = %d entries, want 2", len(docs))
	}
	// The names come back BARE, and in file order: the consumer joins them
	// onto its own directory and must never take a prefix from the bundle.
	if docs[0].Name != "B.TXT" || docs[1].Name != "A.TXT" {
		t.Fatalf("Docs names = %q,%q want B.TXT,A.TXT (file order, bare)", docs[0].Name, docs[1].Name)
	}
	if b.DocsCount() != 2 {
		t.Fatalf("DocsCount = %d want 2", b.DocsCount())
	}
}

func TestKnownEntry(t *testing.T) {
	good := []string{EntrySettings, EntrySession, DocsPrefix + "a", DocsPrefix + "NOTE.TXT"}
	bad := []string{"", "docs", "docs/", "DOCS/a", "Settings", DocsPrefix + "..", "other"}
	for _, n := range good {
		if !KnownEntry(n) {
			t.Errorf("KnownEntry(%q) = false, want true", n)
		}
	}
	for _, n := range bad {
		if KnownEntry(n) {
			t.Errorf("KnownEntry(%q) = true, want false", n)
		}
	}
}

// The zero-length body is the case a naive `body == nil means absent` reading
// gets wrong, and a bundle is the one place it matters: a captured document
// that was empty must restore as an empty file.
func TestEmptyBodySurvivesTheRoundTrip(t *testing.T) {
	raw, ok := Build([]Entry{{Name: EntrySession, Body: []byte{}}})
	if !ok {
		t.Fatal("Build refused an empty body")
	}
	b, ok, reason := Parse(raw)
	if !ok {
		t.Fatalf("Parse refused: %s", reason)
	}
	body, found := b.Get(EntrySession)
	if !found {
		t.Fatal("an empty body is still an entry")
	}
	if len(body) != 0 {
		t.Fatalf("body = %v want empty", body)
	}
	// A hand-forged zero length must parse as an empty body, not as a
	// refusal: `settings 0` is a legal, if useless, entry.
	b, ok, reason = Parse([]byte(string(Header(1)) + "settings 0\n"))
	if !ok {
		t.Fatalf("a zero-length entry was refused: %s", reason)
	}
	if body, found := b.Get(EntrySettings); !found || len(body) != 0 {
		t.Fatalf("zero-length entry: found=%v len=%d", found, len(body))
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

// The property the header's entry count exists for: a container cut short at
// ANY point is a refusal. Without the count, a cut at an entry BOUNDARY would
// parse as a well-formed smaller bundle — and a restore would then succeed on
// half the state and report success, which is the one failure mode a
// snapshot format must not have.
func TestNoTruncatedBundleParses(t *testing.T) {
	raw, ok := Build([]Entry{
		{Name: EntrySettings, Body: []byte("#v2\ntheme=dark\n")},
		{Name: EntrySession, Body: binaryBody},
		{Name: DocsPrefix + "NOTE.TXT", Body: []byte("note\n")},
	})
	if !ok {
		t.Fatal("Build refused")
	}
	for n := 0; n < len(raw); n++ {
		if b, ok, _ := Parse(raw[:n]); ok {
			t.Fatalf("a bundle truncated to %d of %d bytes parsed clean (%d entries)",
				n, len(raw), len(b.Entries))
		}
	}
	// One byte PAST the end is a refusal as well, not a silent accept of the
	// prefix.
	if _, ok, _ := Parse(append(append([]byte(nil), raw...), 'x')); ok {
		t.Fatal("a bundle with one trailing byte parsed clean")
	}
}
