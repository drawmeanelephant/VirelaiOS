package pdf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Model the native source, not random access: one forward position, 2048-byte
// reads/discards, and close/reopen from zero on a backwards window miss.
// The only retained source cache is Render's arena window.
type forwardSource struct {
	data     []byte
	position int
	opened   bool
	chunk    int
}

func (s *forwardSource) Length() int64 { return int64(len(s.data)) }
func (s *forwardSource) close(l *Ledger) Code {
	if !s.opened {
		return OK
	}
	s.opened = false
	return l.Close()
}
func (s *forwardSource) ReadAt(dst []byte, off int64, l *Ledger) (int, Code) {
	if off < 0 || off > int64(len(s.data)) || int64(len(dst)) > int64(len(s.data))-off {
		return 0, ReadFailed
	}
	if s.opened && int(off) < s.position {
		if c := s.close(l); c != OK {
			return 0, c
		}
		if c := l.Rewind(); c != OK {
			return 0, c
		}
	}
	if !s.opened {
		if c := l.Open(); c != OK {
			return 0, c
		}
		s.position, s.opened = 0, true
	}
	for s.position < int(off) {
		n := min(s.chunk, int(off)-s.position)
		if c := l.Read(uint64(n)); c != OK {
			return 0, c
		}
		s.position += n
	}
	total := 0
	for total < len(dst) {
		n := min(s.chunk, len(dst)-total)
		if c := l.Read(uint64(n)); c != OK {
			return total, c
		}
		copy(dst[total:total+n], s.data[s.position:s.position+n])
		s.position += n
		total += n
	}
	return total, OK
}

func timeWorkload(t *testing.T) []byte {
	t.Helper()
	image := zipData(t, bytes.Repeat([]byte{17, 34, 51}, 4), 9)
	return document(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 768 1086] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> /XObject << /I1 7 0 R >> >> >>",
		stream([]byte(".5 g 0 0 768 1086 re f BT /F1 24 Tf 1 0 0 1 96 96 Tm (A) Tj ET q 96 0 0 96 0 0 cm /I1 Do Q"), ""),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0] /CharProcs << /A 6 0 R >> /Encoding << /Differences [65 /A] >> /FirstChar 65 /LastChar 65 /Widths [1] >>",
		stream([]byte("1 0 0 0 1 1 d1 0 0 1 1 re f"), ""),
		stream(image, "/Type /XObject /Subtype /Image /Width 2 /Height 2 /BitsPerComponent 8 /ColorSpace /DeviceRGB /Filter /FlateDecode"))
}

func TestTimeWorkloadBudgetsAndPixels(t *testing.T) {
	src := forwardSource{data: timeWorkload(t), chunk: 2048}
	l := Ledger{Max: MaxWork}
	p, st, f := Render(&src, 0, make([]byte, ArenaBytes), &l)
	if f.Code != OK || st.Work > MaxWork || st.VectorWork > 64_000_000 {
		t.Fatal(f, st)
	}
	if c := src.close(&l); c != OK || l.Handles != 0 || l.Decoders != 0 {
		t.Fatal(c, l)
	}
	if p.Width != 1024 || p.Height != 1448 {
		t.Fatal(p.Width, p.Height)
	}
	for y := 0; y < p.Height; y++ {
		for x := 0; x < p.Width; x++ {
			want := uint32(0xff808080)
			if x < 128 && y >= 1320 {
				want = 0xff112233
			}
			if p.Pix[y*p.Width+x] != want {
				t.Fatalf("%d,%d: %08x want %08x", x, y, p.Pix[y*p.Width+x], want)
			}
		}
	}
	t.Logf("PDF work=%d vector work=%d rewinds=%d", st.Work, st.VectorWork, st.Rewinds)
}

func capacityObjects(extra bool) []byte {
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", ""}
	var fonts strings.Builder
	for i := 0; i < 16; i++ {
		font := len(objects)
		fmt.Fprintf(&fonts, "/F%d %d 0 R ", i, font+1)
		objects = append(objects, "")
		var procs, widths strings.Builder
		count := 256
		if i == 15 {
			count = 237
		}
		for j := 0; j < count; j++ {
			fmt.Fprintf(&procs, "/G%d %d 0 R ", j, len(objects)+1)
			objects = append(objects, stream([]byte("0 0 0 0 0 0 d1"), ""))
		}
		for j := 0; j < 256; j++ {
			fmt.Fprintf(&widths, "%d 0 R ", len(objects)+1)
			objects = append(objects, "0")
		}
		objects[font] = "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 0 0] /FontMatrix [1 0 0 1 0 0] /CharProcs << " + procs.String() +
			">> /Encoding << /Differences [] >> /FirstChar 0 /LastChar 255 /Widths [" + widths.String() + "] >>"
	}
	objects[2] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Resources << /Font << " + fonts.String() + ">> >> >>"
	if extra {
		objects = append(objects, "null")
	}
	return document(objects...)
}

func TestObjectCapacityThroughForwardWindow(t *testing.T) {
	arena := make([]byte, ArenaBytes)
	for _, extra := range []bool{false, true} {
		data := capacityObjects(extra)
		for _, chunk := range []int{2048, 137} {
			src := forwardSource{data: data, chunk: chunk}
			l := Ledger{Max: MaxWork}
			p, st, f := Render(&src, 0, arena, &l)
			_ = src.close(&l)
			if extra {
				if f.Code != ObjectLimit || p.Pix != nil {
					t.Fatal(f)
				}
				continue
			}
			if f.Code != OK || st.Objects != MaxObjects || st.Rewinds > MaxRewinds-1 ||
				l.Handles != 0 || l.Decoders != 0 {
				t.Fatal(f, st, l.Handles, l.Decoders)
			}
			for _, pixel := range p.Pix {
				if pixel != 0xffffffff {
					t.Fatal("blank capacity page")
				}
			}
			// Reserve/charge the native publication recheck's extra rewind.
			if c := l.Rewind(); c != OK {
				t.Fatal(c)
			}
			t.Logf("chunk=%d work=%d vector=%d reads=%d rewinds+recheck=%d", chunk, st.Work, st.VectorWork, st.Reads, l.Rewinds)
		}
	}
}

func TestWindowOverlapAndSkippedBytes(t *testing.T) {
	for _, data := range [][]byte{
		bytes.Repeat([]byte("x"), 65536*3),
		append(bytes.Repeat([]byte("x"), 65536*3), 'y'),
	} {
		src := forwardSource{data: data, chunk: 137}
		e := syntaxEngine(data)
		e.src = &src
		for _, pos := range []int{0, 65535, 65536, 65535, 98304, 98303, len(data) - 1} {
			if got := e.byteAt(pos); got != data[pos] || e.f.Code != OK {
				t.Fatal(pos, got, e.f)
			}
		}
		if e.l.Rewinds != 0 || e.l.Counts[Copy] <= e.l.ReadBytes {
			t.Fatal("overlap copy not charged or source reopened", e.l)
		}
		_ = src.close(e.l)
	}
	// A forward skip larger than the window must not form an invalid overlap.
	src := forwardSource{data: bytes.Repeat([]byte("x"), 300000), chunk: 2048}
	e := syntaxEngine(src.data)
	e.src = &src
	e.byteAt(0)
	e.byteAt(250000)
	e.byteAt(0)
	if e.f.Code != OK || e.l.Rewinds != 1 {
		t.Fatal(e.f, e.l)
	}
	_ = src.close(e.l)
}

func indirectWidths(width string) []byte {
	return document(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		stream([]byte("BT /F1 6 Tf 1 0 0 1 3 3 Tm (AA) Tj ET"), ""),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0] /CharProcs << /A 6 0 R >> /Encoding << /Differences [65 /A] >> /FirstChar 65 /LastChar 65 /Widths [7 0 R] >>",
		stream([]byte("1 0 0 0 1 1 d1 0 0 1 1 re f"), ""),
		"8 0 R", width)
}

func TestIndirectWidthsAndFingerprintCollisions(t *testing.T) {
	src := indirectWidths("1")
	p, _, _ := rendered(t, src)
	for y := 52; y < 60; y++ {
		for x := 4; x < 20; x++ {
			if p.Pix[y*64+x] != 0xff000000 {
				t.Fatal("indirect advance", x, y)
			}
		}
	}
	refusal(t, indirectWidths("-1"), Malformed)
	refusal(t, indirectWidths("null"), Malformed)
	refusal(t, indirectWidths("32768.000001"), CoordinateLimit)
	arena := make([]byte, ArenaBytes)
	s := memorySource{data: src}
	l := Ledger{}
	if n := testing.AllocsPerRun(100, func() {
		l = Ledger{Max: MaxWork}
		if _, _, f := Render(&s, 0, arena, &l); f.Code != OK {
			panic(f.Code)
		}
	}); n != 0 {
		t.Fatal("indirect widths allocate", n)
	}
	// Independently find a fingerprint collision; different decoded names
	// remain legal, while alternate spellings of the same name still fail.
	seen := make(map[uint32]string)
	var a, b string
	for i := 0; b == ""; i++ {
		name := fmt.Sprintf("K%d", i)
		h := uint32(2166136261)
		for _, c := range []byte(name) {
			h = (h ^ uint32(c)) * 16777619
		}
		h <<= 22
		if prior, ok := seen[h]; ok {
			a, b = prior, name
		} else {
			seen[h] = name
		}
	}
	if _, f := parseSyntax("<< /" + a + " 0 /" + b + " 1 >>"); f.Code != OK {
		t.Fatal("hash collision rejected distinct keys", f)
	}
	if _, f := parseSyntax("<< /A 0 /#41 1 >>"); f.Code != Malformed {
		t.Fatal("decoded duplicate escaped key", f)
	}
}

func TestAcceptanceFixVectors(t *testing.T) {
	dir := "../../../tests/fixtures/pdf/engine"
	cases := map[string][]byte{"time-fill-text-image.pdf": timeWorkload(t), "indirect-widths.pdf": indirectWidths("1")}
	if os.Getenv("PDF_AUTHOR_VECTORS") == "1" {
		for name, data := range cases {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name := range cases {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		rendered(t, data)
	}
}
