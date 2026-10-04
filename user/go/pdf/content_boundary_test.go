package pdf

import (
	"bytes"
	"compress/flate"
	"fmt"
	"strings"
	"testing"
)

func boundaryDocument(t *testing.T, parts []string, compressed uint16) []byte {
	t.Helper()
	font := 4 + len(parts)
	var refs strings.Builder
	for i := range parts {
		fmt.Fprintf(&refs, "%d 0 R ", 4+i)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Resources << /Font << /F1 %d 0 R >> >> /Contents [%s] >>", font, refs.String()),
	}
	for i, part := range parts {
		data, keys := []byte(part), ""
		if compressed&(1<<i) != 0 {
			data = zipData(t, data, flate.BestCompression)
			keys = "/Filter /FlateDecode"
		}
		objects = append(objects, stream(data, keys))
	}
	// Parentheses, @ and A share an outline so all string spellings paint.
	objects = append(objects,
		fmt.Sprintf("<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0] /CharProcs << /A %d 0 R >> /Encoding << /Differences [40 /A /A 64 /A /A] >> /FirstChar 40 /LastChar 65 /Widths [%s] >>", font+1, strings.Repeat("1 ", 26)),
		stream([]byte("1 0 0 0 1 1 d1 0 0 1 1 re f"), ""))
	return document(objects...)
}

func boundaryRegular(b byte) bool {
	// Independent ISO 32000-1 §7.2.2 character classes, including braces.
	return !bytes.ContainsRune([]byte("\x00\t\n\f\r ()<>[]{}/%"), rune(b))
}

func boundaryOffset(t *testing.T, src []byte, object int) int {
	t.Helper()
	start := bytes.Index(src, []byte(fmt.Sprintf("\n%d 0 obj\n", object)))
	if start < 0 {
		t.Fatal("missing stream object", object)
	}
	offset := bytes.Index(src[start:], []byte("\nstream\n"))
	if offset < 0 {
		t.Fatal("missing stream data", object)
	}
	return start + offset + len("\nstream\n")
}

func boundaryRefusal(t *testing.T, src []byte, object int) {
	t.Helper()
	arena := make([]byte, ArenaBytes)
	for _, forward := range []bool{false, true} {
		var source Source = &memorySource{data: src}
		sequential := forwardSource{data: src, chunk: 137}
		if forward {
			source = &sequential
		}
		l := Ledger{Max: MaxWork}
		p, _, f := Render(source, 0, arena, &l)
		if f.Code != MalformedContent || f.Object != object || f.Page != 0 ||
			f.Offset != boundaryOffset(t, src, object) || p.Pix != nil {
			t.Fatalf("forward=%t: failure=%+v page=%d", forward, f, len(p.Pix))
		}
		if c := sequential.close(&l); c != OK || l.Handles != 0 || l.Decoders != 0 {
			t.Fatal("boundary cleanup", c, l.Handles, l.Decoders)
		}
		// Reuse the failed arena; neither a decoder nor page state survives.
		good := simple("", "")
		l = Ledger{Max: MaxWork}
		p, _, f = Render(&memorySource{data: good}, 0, arena, &l)
		if f.Code != OK || l.Decoders != 0 {
			t.Fatal("boundary recovery", f)
		}
		for _, pixel := range p.Pix {
			if pixel != 0xffffffff {
				t.Fatal("boundary recovery pixels")
			}
		}
	}
}

func TestContentsBoundaryCuts(t *testing.T) {
	for _, text := range []string{
		"(A)", "(\\101)", "(\\A)", "(\\\nA)", "(\\\r\nA)",
		"(A(A)A)", "(A\\)A)", "(A\\(A)", "<41>", "<4 1>", "<4>",
	} {
		t.Run(text, func(t *testing.T) {
			prefix := "q 1 0 0 rg 3 3 24 24 re BT /F1 3 Tf ["
			program := prefix + text + " 0 <41>] TJ ET q 0 1 0 rg Q f Q"
			uncut, _, _ := rendered(t, boundaryDocument(t, []string{program}, 0))
			for cut := 0; cut <= len(program); cut++ {
				t.Run(fmt.Sprint(cut), func(t *testing.T) {
					src := boundaryDocument(t, []string{program[:cut], program[cut:]}, 0)
					openString := cut > len(prefix) && cut < len(prefix)+len(text) ||
						cut > len(prefix)+len(text)+3 && cut < len(prefix)+len(text)+7
					regularPair := cut > 0 && cut < len(program) &&
						boundaryRegular(program[cut-1]) && boundaryRegular(program[cut])
					if openString || regularPair {
						boundaryRefusal(t, src, 5)
						return
					}
					p, _, _ := rendered(t, src)
					for i, pixel := range p.Pix {
						if pixel != uncut.Pix[i] {
							t.Fatalf("pixel %d: %08x want %08x", i, pixel, uncut.Pix[i])
						}
					}
				})
			}
		})
	}
}

func TestContentsBoundaryNeighborsAndLocations(t *testing.T) {
	for _, parts := range [][]string{
		{"1 0 0 r", "g 0 0 48 48 re f"},
		{"q Q", "q Q"},
		{"1", "0 g"},
		{"BT /F", "1 3 Tf (A) Tj ET"},
		{"BT /F1 3 Tf (", "A) Tj ET"},
		{"BT /F1 3 Tf (A\\", ") Tj ET"},
		{"BT /F1 3 Tf (A\\\r", "\nA) Tj ET"},
		{"BT /F1 3 Tf <", "41> Tj ET"},
		{"BT /F1 3 Tf <41 ", "> Tj ET"},
	} {
		for _, empty := range []bool{false, true} {
			later := 5
			streams := parts
			if empty {
				streams = []string{"", parts[0], "", "", parts[1], ""}
				later = 8
			}
			for _, mask := range []uint16{0, 0xffff, 0xaaaa, 0x5555} {
				t.Run(fmt.Sprintf("%q/empty=%t/flate=%x", parts, empty, mask), func(t *testing.T) {
					boundaryRefusal(t, boundaryDocument(t, streams, mask), later)
				})
			}
		}
	}
}

func TestContentsCommentsEndAtBoundary(t *testing.T) {
	for _, parts := range [][]string{
		{"1 0 0 rg % ignored", " 0 0 48 48 re f"},
		{"1 0 0 % ignored ", "rg 0 0 48 48 re f"},
		{"1 0 0 rg %", "0 0 48 48 re f"},
		{"1 0 0 rg % ignored ", "0 0 48 48 re f"},
	} {
		// Unlike concatenation, a boundary ends the comment. A newline
		// models that end independently; pending operands must survive it.
		want, _, _ := rendered(t, boundaryDocument(t, []string{parts[0] + "\n" + parts[1]}, 0))
		for _, mask := range []uint16{0, 0xffff, 0xaaaa, 0x5555} {
			p, _, _ := rendered(t, boundaryDocument(t, []string{"", parts[0], "", "", parts[1], ""}, mask))
			for i, pixel := range p.Pix {
				if pixel != want.Pix[i] || pixel != 0xffff0000 {
					t.Fatalf("comment boundary pixel %d: %08x", i, pixel)
				}
			}
		}
	}
	// A1.1's byte-pair refusal also applies at the end of a comment.
	boundaryRefusal(t, boundaryDocument(t, []string{"% ignored", "q Q"}, 0), 5)
}

func TestContentsCommentCuts(t *testing.T) {
	prefix := "1 0 0 rg 0 0 48 48 re "
	comment := "% 0 g 0 g 0 g"
	program := prefix + comment + "\nf"
	arena := make([]byte, ArenaBytes)
	for cut := 0; cut <= len(program); cut++ {
		t.Run(fmt.Sprint(cut), func(t *testing.T) {
			src := boundaryDocument(t, []string{program[:cut], program[cut:]}, 0)
			if cut > 0 && cut < len(program) && boundaryRegular(program[cut-1]) && boundaryRegular(program[cut]) {
				boundaryRefusal(t, src, 5)
				return
			}
			reference := program
			if cut > len(prefix) && cut <= len(prefix)+len(comment) {
				reference = program[:cut] + "\n" + program[cut:]
			}
			l := Ledger{Max: MaxWork}
			want, _, expected := Render(&memorySource{data: boundaryDocument(t, []string{reference}, 0)}, 0, arena, &l)
			p, _, got := Render(&memorySource{data: src}, 0, make([]byte, ArenaBytes), &Ledger{Max: MaxWork})
			if got.Code != expected.Code || len(p.Pix) != len(want.Pix) {
				t.Fatal("comment cut", got, expected)
			}
			for i, pixel := range p.Pix {
				if pixel != want.Pix[i] {
					t.Fatalf("comment cut pixel %d: %08x want %08x", i, pixel, want.Pix[i])
				}
			}
		})
	}
}

func TestContentsLegalBoundariesAndEmptyStreams(t *testing.T) {
	parts := []string{"", "q 1 0 ", "", "0 rg 3 3 24 24 re BT /", "F1 3 Tf [", "(A)", " 0 ", "<41>", "] TJ ET q 0 ", "", "1 0 rg Q f Q", ""}
	want, _, _ := rendered(t, boundaryDocument(t, []string{strings.Join(parts, "")}, 0))
	for _, mask := range []uint16{0, 0xffff, 0xaaaa, 0x5555} {
		p, _, _ := rendered(t, boundaryDocument(t, parts, mask))
		for i, pixel := range p.Pix {
			if pixel != want.Pix[i] {
				t.Fatalf("state boundary pixel %d: %08x want %08x", i, pixel, want.Pix[i])
			}
		}
		rendered(t, boundaryDocument(t, []string{"", "", ""}, mask))
		rendered(t, boundaryDocument(t, make([]string, 16), mask))
	}
	for b := 0; b <= 255; b++ {
		if regular(byte(b)) != boundaryRegular(byte(b)) {
			t.Fatal("ISO character class", b)
		}
	}
}

func TestContentsStringsStillOpenAtFinalEnd(t *testing.T) {
	for _, content := range []string{"(", "(A\\", "(A\\101", "(A(A)", "<", "<4", "<41 "} {
		for _, mask := range []uint16{0, 0xffff} {
			refusal(t, boundaryDocument(t, []string{content, "", ""}, mask), MalformedContent)
		}
	}
}
