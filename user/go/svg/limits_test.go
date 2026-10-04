package svg

import (
	"math"
	"strings"
	"testing"
	"unsafe"
	"virelai/vector"
)

func TestDocumentCapacityBoundaries(t *testing.T) {
	for _, tc := range []struct {
		body string
		want vector.Code
	}{
		{strings.Repeat("<g/>", 4095), vector.OK},
		{strings.Repeat("<g/>", 4096), vector.NodeLimit},
		{strings.Repeat(`<path d=""/>`, 1024), vector.OK},
		{strings.Repeat(`<path d=""/>`, 1025), vector.SceneLimit},
		{`<path d="` + strings.Repeat("M0 0Z ", 1024) + `"/>`, vector.OK},
		{`<path d="` + strings.Repeat("M0 0Z ", 1025) + `"/>`, vector.ContourLimit},
		{`<path d="M0 0 ` + strings.Repeat("L0 0 ", 8190) + `Z"/>`, vector.OK},
		{`<path d="M0 0 ` + strings.Repeat("L0 0 ", 8191) + `Z"/>`, vector.CommandLimit},
		{`<g transform="` + strings.Repeat("translate(0) ", 16) + `"/>`, vector.OK},
		{`<g transform="` + strings.Repeat("translate(0) ", 17) + `"/>`, vector.TokenLimit},
		{strings.Repeat(`<g transform="`+strings.Repeat("translate(0) ", 16)+`"/>`, 16), vector.OK},
		{strings.Repeat(`<g transform="`+strings.Repeat("translate(0) ", 16)+`"/>`, 16) + `<g transform="scale(1)"/>`, vector.TokenLimit},
		{`<g id="` + strings.Repeat("a", 64) + `"/>`, vector.OK},
		{`<g id="` + strings.Repeat("a", 65) + `"/>`, vector.TokenLimit},
	} {
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(wrap(tc.body)), storage(), &b)
		if f.Code != tc.want {
			t.Fatalf("size %d: got %+v want %d", len(tc.body), f, tc.want)
		}
	}
	for _, n := range []int{1_048_576, 1_048_577} {
		src := wrap("")
		src += strings.Repeat(" ", n-len(src))
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(src), storage(), &b)
		want := vector.OK
		if n > 1_048_576 {
			want = vector.SourceLimit
		}
		if f.Code != want {
			t.Fatal(n, f)
		}
	}
}
func TestAttributeAndStorageCapacities(t *testing.T) {
	// The closed allowlist makes 16 distinct supported attributes on one
	// element and 32768 attributes before the other caps unreachable. Test
	// the XML scanner's exact capacity directly, without inventing support.
	for _, n := range []int{16, 17} {
		src := ""
		for i := 0; i < n; i++ {
			src += " a" + string(rune('A'+i)) + `="x"`
		}
		src += "?>"
		b := vector.Budget{Max: vector.MaxWork}
		p := parser{src: []byte(src), b: &b}
		p.attributes(true, "")
		want := vector.OK
		if n == 17 {
			want = vector.AttributeLimit
		}
		if p.f.Code != want {
			t.Fatal(n, p.f)
		}
	}
	b := vector.Budget{Max: vector.MaxWork}
	p := parser{src: []byte(` a="x" b="y"?>`), attrs: 32767, b: &b}
	p.attributes(true, "")
	if p.f.Code != vector.AttributeLimit || p.attrs != 32768 {
		t.Fatal(p.attrs, p.f)
	}
	for _, slots := range []int{0, 4, 5} {
		out := vector.Storage{Commands: make([]vector.Command, slots), Paints: make([]vector.Paint, 1)}
		b = vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(wrap(`<rect width="1" height="1"/>`)), out, &b)
		want := vector.OK
		if slots < 5 {
			want = vector.SceneLimit
		}
		if f.Code != want {
			t.Fatal(slots, f)
		}
	}
	size := int(unsafe.Sizeof(vector.Command{}))
	out := vector.Storage{Commands: make([]vector.Command, vector.MaxScene/size+1)}
	b = vector.Budget{Max: vector.MaxWork}
	_, _, f := Parse([]byte(wrap("")), out, &b)
	if f.Code != vector.SceneLimit || b.Used != 0 {
		t.Fatal(f, b)
	}
}
func TestNumericAndXMLBoundaries(t *testing.T) {
	for _, tc := range []struct {
		v    string
		want vector.Code
	}{
		{"32768", vector.OK}, {"32768.00000001", vector.CoordinateLimit},
		{"-32768", vector.OK}, {"-32768.00000001", vector.CoordinateLimit},
		{strings.Repeat("0", 32), vector.OK}, {strings.Repeat("0", 33), vector.TokenLimit},
		{"1e-999", vector.OK}, {"0e999", vector.OK}, {"1e999", vector.CoordinateLimit},
		{"NaN", vector.Malformed}, {"Inf", vector.Malformed}, {"1e", vector.Malformed},
		{".", vector.Malformed}, {"1,2", vector.UnsupportedFeature},
	} {
		b := vector.Budget{Max: vector.MaxWork}
		p := parser{src: []byte(tc.v), b: &b}
		p.scalar(span{0, len(tc.v)}, false)
		if p.f.Code != tc.want {
			t.Fatal(tc.v, p.f)
		}
	}
	for _, src := range []string{
		wrap(`<g id="` + string([]byte{0xff}) + `"/>`), wrap(`<title>&#xFFFE;</title>`),
		wrap(`<g id="&#x110000;"/>`), wrap(`<g id="a<b"/>`), wrap(`<!-- bad -- comment -->`),
		wrap(`<g unknown="x" id="&bad;"/>`), wrap(`<path d="M1" stroke="red"/>`),
	} {
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(src), storage(), &b)
		want := vector.Malformed
		if strings.Contains(src, "unknown") {
			want = vector.UnsupportedFeature
		}
		if f.Code != want {
			t.Fatal(src, f)
		}
	}
	for _, src := range []string{`<svg width="1024" height="1024"/>`, `<svg width="1025" height="1"/>`,
		`<svg width="1.5" height="1"/>`, `<svg width="0" height="1"/>`} {
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(src), storage(), &b)
		want := vector.CanvasLimit
		if strings.Contains(src, `height="1024"`) {
			want = vector.OK
		}
		if f.Code != want {
			t.Fatal(src, f)
		}
	}
}
func TestExactParserWorkCategories(t *testing.T) {
	b := vector.Budget{Max: vector.MaxWork}
	p := parser{src: []byte(`1.5`), b: &b}
	if p.scalar(span{0, 3}, false) != 1.5 || p.counts != ([4]uint64{12, 0, 0, 0}) {
		t.Fatal(p.counts)
	}
	b = vector.Budget{Max: vector.MaxWork}
	p = parser{src: []byte(`&#49;`), b: &b}
	r := p.reader(span{0, 5})
	if v, ok := r.next(); !ok || v != '1' || p.counts != ([4]uint64{8, 0, 0, 0}) {
		t.Fatal(v, ok, p.counts)
	}
	b = vector.Budget{Max: vector.MaxWork}
	p = parser{out: storage(), b: &b}
	p.command(frame{transform: identity}, vector.Move, vector.Point{X: 1, Y: 2})
	if p.counts != ([4]uint64{0, 1, 1, 0}) {
		t.Fatal(p.counts)
	}
	b = vector.Budget{Max: vector.MaxWork}
	p = parser{out: storage(), b: &b}
	p.arc(frame{transform: identity}, 0, 0, 1, 1, 0, math.Pi/2)
	if p.counts != ([4]uint64{0, 4, 8, 7}) {
		t.Fatal(p.counts)
	}
}
func TestBudgetCumulativeAndEveryCut(t *testing.T) {
	src := []byte(wrap(`<path d="M1 2Q3 4 5 6Z"/><circle r="2"/>`))
	out := storage()
	b := vector.Budget{Max: vector.MaxWork}
	_, _, f := Parse(src, out, &b)
	if f.Code != vector.OK {
		t.Fatal(f)
	}
	work := b.Used
	b.Max = work*2 - 1
	_, _, f = Parse(src, out, &b)
	if f.Code != vector.WorkLimit || b.Used < work {
		t.Fatal(f, b)
	}
	for n := 0; n < len(src); n++ {
		b = vector.Budget{Max: vector.MaxWork}
		_, _, f = Parse(src[:n], out, &b)
		if f.Code == vector.OK {
			t.Fatalf("truncated success at %d", n)
		}
	}
}
