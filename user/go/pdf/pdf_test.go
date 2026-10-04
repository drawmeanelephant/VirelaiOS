package pdf

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
	"virelai/vector"
)

type memorySource struct {
	data []byte
	size int64
	code Code
}

func (s *memorySource) Length() int64 {
	if s.size != 0 {
		return s.size
	}
	return int64(len(s.data))
}
func (s *memorySource) ReadAt(dst []byte, off int64, l *Ledger) (int, Code) {
	if s.code != OK {
		return 0, s.code
	}
	if off < 0 || off > int64(len(s.data)) {
		return 0, ReadFailed
	}
	n := min(len(dst), len(s.data)-int(off))
	if c := l.Read(uint64(n)); c != OK {
		return 0, c
	}
	copy(dst[:n], s.data[off:int(off)+n])
	return n, OK
}
func document(objects ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objects)+1)
	for i, o := range objects {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, off := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), start)
	return b.Bytes()
}
func stream(data []byte, keys string) string {
	return fmt.Sprintf("<< /Length %d %s >>\nstream\n%s\nendstream", len(data), keys, data)
}
func simple(content string, extra string) []byte {
	return document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Contents 4 0 R "+extra+" >>", stream([]byte(content), ""))
}
func rendered(t *testing.T, src []byte) (Page, Stats, *Ledger) {
	t.Helper()
	l := &Ledger{Max: MaxWork}
	a := make([]byte, ArenaBytes)
	p, st, f := Render(&memorySource{data: src}, 0, a, l)
	if f.Code != OK {
		t.Fatalf("render: %+v (%s)", f, f.Error())
	}
	return p, st, l
}
func refusal(t *testing.T, src []byte, want Code) {
	t.Helper()
	a := make([]byte, ArenaBytes)
	l := Ledger{Max: MaxWork}
	p, _, f := Render(&memorySource{data: src}, 0, a, &l)
	if f.Code != want || p.Pix != nil {
		t.Fatalf("want %s, got %+v page=%d", fail(want).Error(), f, len(p.Pix))
	}
	if l.Decoders != 0 {
		t.Fatalf("leaked decoder: %d", l.Decoders)
	}
}
func zipData(t *testing.T, data []byte, level int) []byte {
	t.Helper()
	var b bytes.Buffer
	w, err := zlib.NewWriterLevel(&b, level)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(data)
	w.Close()
	return b.Bytes()
}
func TestArenaPartitions(t *testing.T) {
	if unsafe.Sizeof(xref{}) != 16 {
		t.Fatal("xref layout")
	}
	n := 524288 - 32*int(unsafe.Sizeof(parseFrame{})) - contentStateBytes()
	if n < int(unsafe.Sizeof(vector.Command{}))*MaxCommands {
		t.Fatalf("state partition insufficient: %d", n)
	}
	if unsafe.Sizeof(decoder{}) > 65536 {
		t.Fatal("decoder exceeds frame bound")
	}
	if 2*unsafe.Sizeof(decoder{})+3072 > 262144 {
		t.Fatal("decode partition")
	}
}
func TestAnalyticFills(t *testing.T) {
	for _, op := range []string{"f", "F", "f*"} {
		p, st, l := rendered(t, simple("1 0 0 rg 0 0 24 48 re "+op+" 0 0 1 rg 24 0 24 48 re "+op, ""))
		if p.Width != 64 || p.Height != 64 || p.Stride != 64 {
			t.Fatal(p.Width, p.Height)
		}
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				want := uint32(0xffff0000)
				if x >= 32 {
					want = 0xff0000ff
				}
				if p.Pix[y*64+x] != want {
					t.Fatalf("%d,%d: %x", x, y, p.Pix[y*64+x])
				}
			}
		}
		if l.Counts[Background] != 16384 || st.Commands != 20 || st.Paints != 4 || st.Contours != 4 || st.Edges != 16 {
			t.Fatalf("%+v %+v", st, l.Counts)
		}
	}
}
func TestEmptyAndRecovery(t *testing.T) {
	good := simple("", "")
	bad := simple("0 0 m S", "")
	a := make([]byte, ArenaBytes)
	src := memorySource{}
	for i := 0; i < 100; i++ {
		for _, data := range [][]byte{good, bad, good} {
			src.data = data
			l := Ledger{Max: MaxWork}
			p, _, f := Render(&src, 0, a, &l)
			if bytes.Equal(data, bad) {
				if f.Code != UnsupportedStroke || p.Pix != nil {
					t.Fatal(f)
				}
			} else {
				if f.Code != OK {
					t.Fatal(f)
				}
				for _, c := range p.Pix {
					if c != 0xffffffff {
						t.Fatal("stale output")
					}
				}
			}
			if l.Decoders != 0 {
				t.Fatal("decoder retained")
			}
		}
	}
}
func TestOperatorsAndConstructionMapping(t *testing.T) {
	for _, content := range []string{
		"0 0 m 48 0 l 48 48 l 0 48 l f",
		"0 0 m 48 0 l 48 48 l 0 48 l h f",
		"q 0 0 48 48 re 1 0 0 1 100 100 cm Q f",
		"0 0 48 48 re q 0 0 0 0 0 0 cm Q f",
		"0 0 m 48 0 48 0 48 0 c 48 48 48 48 y 0 48 0 48 v h f",
	} {
		p, _, _ := rendered(t, simple(content, ""))
		if p.Pix[32*64+32] != 0xff000000 {
			t.Fatal(content)
		}
	}
	p, _, _ := rendered(t, simple("0 0 48 48 re 0 0 0 0 0 0 cm f", ""))
	if p.Pix[100] != 0xff000000 {
		t.Fatal("cm moved constructed rectangle")
	}
	for _, op := range []string{"f", "f*", "F"} {
		p, _, _ := rendered(t, simple("0 0 24 48 re W n 1 0 0 rg 0 0 48 48 re "+op, ""))
		if p.Pix[32] != 0xffffffff || p.Pix[31] != 0xffff0000 {
			t.Fatal("clip")
		}
	}
}
func TestFlateAndSplitContent(t *testing.T) {
	program := "1 0 0 rg 0 0 48 48 re f"
	for _, level := range []int{flate.NoCompression, flate.HuffmanOnly, flate.BestCompression} {
		data := zipData(t, []byte(program), level)
		src := document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Contents 4 0 R >>", stream(data, "/Filter [/FlateDecode] /DecodeParms [<< /Predictor 1 >>]"))
		p, _, l := rendered(t, src)
		if p.Pix[0] != 0xffff0000 || l.Expanded != uint64(len(program)*2) {
			t.Fatal(level, l.Expanded)
		}
		for cut := 0; cut < len(data); cut++ {
			src = document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Contents 4 0 R >>", stream(data[:cut], "/Filter /FlateDecode"))
			refusal(t, src, MalformedStream)
		}
	}
	for cut := 0; cut <= len(program); cut++ {
		src := document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Contents [4 0 R 5 0 R] >>", stream([]byte(program[:cut]), ""), stream([]byte(program[cut:]), ""))
		if cut > 0 && cut < len(program) && boundaryRegular(program[cut-1]) && boundaryRegular(program[cut]) {
			refusal(t, src, MalformedContent)
			continue
		}
		p, _, _ := rendered(t, src)
		for _, pixel := range p.Pix {
			if pixel != 0xffff0000 {
				t.Fatal(cut)
			}
		}
	}
}
func fontDocument(content, glyph string, extra string) []byte {
	return document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		stream([]byte(content), ""),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0] /CharProcs << /A 6 0 R >> /Encoding << /Differences [65 /A] >> /FirstChar 65 /LastChar 65 /Widths [1] "+extra+" >>",
		stream([]byte(glyph), ""))
}
func TestType3(t *testing.T) {
	glyph := "1 0 0 0 1 1 d1 0 0 1 1 re f"
	for _, show := range []string{"(A) Tj", "[(A) 0 (A)] TJ", "(A) '", "0 0 (A) \""} {
		p, _, l := rendered(t, fontDocument("BT /F1 24 Tf 1 0 0 1 0 0 Tm 0 TL "+show+" ET", glyph, ""))
		if p.Pix[48*64+8] != 0xff000000 {
			t.Fatal(show)
		}
		if l.Counts[Glyph] < 3 {
			t.Fatal(l.Counts)
		}
	}
	for _, content := range []string{
		"BT /F1 12 Tf 1 Tc 2 Tw 100 Tz 12 TL 0 Ts 0 Tr 0 12 Td (A) Tj 0 -12 TD (A) Tj T* ET",
		"BT /F1 0 Tf <41> Tj ET",
		"q BT /F1 12 Tf (\\101) Tj ET Q",
	} {
		rendered(t, fontDocument(content, glyph, "/Resources << >> /Name /Bounded"))
	}
	refusal(t, fontDocument("BT /F1 12 Tf (B) Tj ET", glyph, ""), MissingGlyph)
	refusal(t, fontDocument("", "1 0 0 0 1 1 d1 1 0 0 rg", ""), UnsupportedGlyph)
	refusal(t, fontDocument("", "1 0 0 0 1 1 d1 2 0 m n", ""), UnsupportedGlyph)
	refusal(t, fontDocument("", "2 0 0 0 1 1 d1", ""), Malformed)
}
func imageDocument(content string, data []byte, keys string) []byte {
	return document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Resources << /XObject << /I1 5 0 R >> >> /Contents 4 0 R >>",
		stream([]byte(content), ""), stream(data, "/Type /XObject /Subtype /Image "+keys))
}
func TestImagesAndOrder(t *testing.T) {
	data := []byte{255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 255}
	keys := "/Width 2 /Height 2 /BitsPerComponent 8 /ColorSpace /DeviceRGB /Decode [0 1 0 1 0 1] /Interpolate false /ImageMask false"
	for _, compressed := range []bool{false, true} {
		img := data
		k := keys
		if compressed {
			img = zipData(t, data, flate.BestCompression)
			k += " /Filter /FlateDecode"
		}
		p, _, _ := rendered(t, imageDocument("48 0 0 48 0 0 cm /I1 Do", img, k))
		for _, test := range []struct {
			x, y int
			c    uint32
		}{{1, 1, 0xffff0000}, {62, 1, 0xff00ff00}, {1, 62, 0xff0000ff}, {62, 62, 0xffffffff}} {
			if p.Pix[test.y*64+test.x] != test.c {
				t.Fatalf("image sample %+v got %x", test, p.Pix[test.y*64+test.x])
			}
		}
		p, _, _ = rendered(t, imageDocument("q 48 0 0 48 0 0 cm /I1 Do Q 0 g 0 0 48 24 re f", img, k))
		if p.Pix[1] != 0xffff0000 || p.Pix[63*64] != 0xff000000 {
			t.Fatal("image/fill order")
		}
	}
	p, _, _ := rendered(t, imageDocument("-48 0 0 -48 48 48 cm /I1 Do", data, keys))
	if p.Pix[0] != 0xffffffff || p.Pix[63*64+63] != 0xffff0000 {
		t.Fatal("reflection")
	}
	refusal(t, imageDocument("", data[:11], keys), MalformedStream)
	refusal(t, imageDocument("", append(data, 0), keys), MalformedStream)
	p, _, _ = rendered(t, imageDocument("48 0 0 48 0 0 cm /I1 Do", []byte{0, 255}, "/Width 1 /Height 2 /BitsPerComponent 8 /ColorSpace /DeviceGray"))
	if p.Pix[0] != 0xff000000 || p.Pix[63*64] != 0xffffffff {
		t.Fatal("gray")
	}
}
func TestClosedRefusals(t *testing.T) {
	for _, test := range []struct {
		content, extra string
		c              Code
	}{
		{"S", "", UnsupportedStroke}, {"1 w", "", UnsupportedStroke},
		{"0 0 m 48 48 l W n", "", UnsupportedClip},
		{"0 0 1 1 re W n", "", UnsupportedClip},
		{"BI", "", UnsupportedImage}, {"1 Tr", "", UnsupportedText},
		{"q", "", MalformedContent}, {"Q", "", MalformedContent},
		{"BT BT", "", MalformedContent}, {"BT 0 0 m ET", "", MalformedContent},
		{"1", "", MalformedContent}, {"0 0 m f 1 0 0 sc", "", UnsupportedFeature},
		{"", "/Annots []", UnsupportedFeature}, {"", "/UserUnit 1", UnsupportedPageGeometry},
		{"", "/Rotate 90", UnsupportedPageGeometry}, {"", "/URI (no)", ExternalResource},
		{"/Absent Do", "", MissingResource}, {"BT (A) Tj ET", "", MissingResource},
	} {
		t.Run(test.content+test.extra, func(t *testing.T) { refusal(t, simple(test.content, test.extra), test.c) })
	}
	for _, keys := range []string{"/Filter /DCTDecode", "/Filter [/FlateDecode /FlateDecode]", "/Filter /FlateDecode /DecodeParms << /Predictor 2 >>"} {
		src := document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Contents 4 0 R >>", stream(nil, keys))
		refusal(t, src, UnsupportedFilter)
	}
	refusal(t, append(simple("", ""), []byte("junk")...), Malformed)
	refusal(t, bytes.Replace(simple("", ""), []byte("%PDF-1.4"), []byte("%PDF-1.7"), 1), UnsupportedStructure)
	refusal(t, document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 2 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] >>"), Malformed)
	refusal(t, document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] >>", "null"), UnsupportedStructure)
}
func TestTruncations(t *testing.T) {
	src := simple("0 0 48 48 re f", "")
	a := make([]byte, ArenaBytes)
	s := memorySource{}
	for i := 0; i < len(bytes.TrimRight(src, "\r\n ")); i++ {
		s.data = src[:i]
		l := Ledger{Max: MaxWork}
		p, _, f := Render(&s, 0, a, &l)
		if f.Code == OK || p.Pix != nil {
			t.Fatal(i)
		}
	}
}
func TestNoHeapAllocations(t *testing.T) {
	for _, data := range [][]byte{
		simple("0 0 48 48 re f", ""),
		fontDocument("BT /F1 24 Tf (AAA) Tj ET", "1 0 0 0 1 1 d1 0 0 1 1 re f", ""),
		imageDocument("48 0 0 48 0 0 cm /I1 Do", zipData(t, []byte{255, 0, 0}, flate.BestCompression), "/Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceRGB /Filter /FlateDecode"),
	} {
		s := memorySource{data: data}
		a := make([]byte, ArenaBytes)
		l := Ledger{}
		var f Failure
		n := testing.AllocsPerRun(10, func() { l = Ledger{Max: MaxWork}; _, _, f = Render(&s, 0, a, &l) })
		if f.Code != OK || n != 0 {
			t.Fatalf("allocations=%g failure=%+v", n, f)
		}
	}
}
func TestAuthoredVectors(t *testing.T) {
	files, err := filepath.Glob("../../../tests/fixtures/pdf/engine/*.pdf")
	if err != nil || len(files) == 0 {
		t.Fatal("missing pinned vectors")
	}
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		rendered(t, data)
	}
}
func TestEmitVectors(t *testing.T) {
	if os.Getenv("PDF_AUTHOR_VECTORS") != "1" {
		return
	}
	dir := "../../../tests/fixtures/pdf/engine"
	os.MkdirAll(dir, 0755)
	cases := map[string][]byte{
		"filled.pdf": simple("1 0 0 rg 0 0 24 48 re f 0 0 1 rg 24 0 24 48 re f", ""),
		"type3.pdf":  fontDocument("BT /F1 24 Tf (A) Tj ET", "1 0 0 0 1 1 d1 0 0 1 1 re f", ""),
		"rgb.pdf":    imageDocument("48 0 0 48 0 0 cm /I1 Do", []byte{255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 255}, "/Width 2 /Height 2 /BitsPerComponent 8 /ColorSpace /DeviceRGB"),
		"cubic.pdf":  simple("0 0 m 0 48 48 48 48 0 c h f", ""),
		"empty.pdf":  simple("", ""),
	}
	for name, data := range cases {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
func TestGeometryLimits(t *testing.T) {
	for _, test := range []struct {
		box string
		c   Code
	}{{"0 0 768 1152", OK}, {"0 0 768.75 1152", CanvasLimit}, {"0 0 768 1152.75", CanvasLimit}, {"0 0 1 1", UnsupportedPageGeometry}, {"0 0 0 48", Malformed}, {"0 0 32768.000001 48", CoordinateLimit}} {
		src := document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox ["+test.box+"] >>")
		if test.c == OK {
			p, _, _ := rendered(t, src)
			if p.Height != 1536 || p.Width != 1024 || p.Pix[len(p.Pix)-1] != 0xffffffff {
				t.Fatal("maximum canvas")
			}
		} else {
			refusal(t, src, test.c)
		}
	}
	for _, n := range []int{16, 17} {
		src := simple(strings.Repeat("q ", n)+strings.Repeat("Q ", n), "")
		if n == 16 {
			rendered(t, src)
		} else {
			refusal(t, src, GraphicsDepthLimit)
		}
	}
}
