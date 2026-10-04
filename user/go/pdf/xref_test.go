package pdf

import (
	"bytes"
	"testing"
)

func TestClassicTableAndEOFSpellings(t *testing.T) {
	src := simple("", "")
	at := bytes.Index(src, []byte("xref\n"))
	for _, ending := range []string{"\n", "\r", "\r\n"} {
		changed := append(bytes.Clone(src[:at]), bytes.ReplaceAll(src[at:], []byte("\n"), []byte(ending))...)
		changed[8] = '\r'
		changed = bytes.Replace(changed, []byte("%PDF-1.4"), []byte("%PDF-1.3"), 1)
		rendered(t, changed)
	}
	for _, test := range []struct {
		from, to string
		code     Code
	}{
		{"0000000015 00000 n", "0000000016 00000 n", Malformed},
		{"0000000015 00000 n", "000000001 00000 n", Malformed},
		{"0000000015 00000 n", "0000000015 00001 n", UnsupportedStructure},
		{"0000000015 00000 n", "0000000015 00000 f", UnsupportedStructure},
		{"0 5\n", "1 5\n", UnsupportedStructure},
		{"0000000000 65535 f", "0000000000 00000 f", UnsupportedStructure},
		{"startxref\n", "% startxref\n", Malformed},
		{"startxref\n", "startxrefx\n", Malformed},
		{"%%EOF\n", "%%EOF\njunk", Malformed},
	} {
		refusal(t, bytes.Replace(src, []byte(test.from), []byte(test.to), 1), test.code)
	}
}
func TestGraphCyclesAndSharedDepth(t *testing.T) {
	refusal(t, document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 .75 .75] /Resources 4 0 R >>", "<< /Font 4 0 R >>"), Malformed)
	// First encounter a shared dictionary shallowly, then reach it through
	// a 30-object chain. Black-node memoization must retain subtree height.
	// Object 4 must be a terminal subtree, so keep the chain disjoint:
	objects := []string{"[2 0 R 3 0 R]", "4 0 R", "5 0 R", "[33 0 R]"}
	for n := 5; n <= 32; n++ {
		v := stringNumber(n+1) + " 0 R"
		if n == 32 {
			v = "2 0 R"
		}
		objects = append(objects, v)
	}
	objects = append(objects, "null")
	e := syntaxEngine(document(objects...))
	e.x = make([]xref, 8193)
	e.structure()
	e.graph(1)
	if e.f.Code != DepthLimit {
		t.Fatal(e.f)
	}
}
func stringNumber(n int) string {
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte(n%10) + '0'
		n /= 10
	}
	return string(b[i:])
}
func TestLargeFiniteNumbersAndCheckedIntegerOverflow(t *testing.T) {
	v, f := parseSyntax("100000000000000000000.25")
	if f.Code != OK || !v.big {
		t.Fatal(v, f)
	}
	src := document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100000000000000000000.25 48] >>")
	refusal(t, src, CoordinateLimit)
	v, f = parseSyntax("100000000000000000000")
	if f.Code != OK {
		t.Fatal(f)
	}
	e := syntaxEngine(nil)
	e.integer(v)
	if e.f.Code != Malformed {
		t.Fatal(e.f)
	}
}
