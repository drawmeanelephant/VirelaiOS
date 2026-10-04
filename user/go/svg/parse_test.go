package svg

import (
	"os"
	"strings"
	"testing"
	"virelai/vector"
)

func storage() vector.Storage {
	return vector.Storage{Commands: make([]vector.Command, vector.MaxCommands), Paints: make([]vector.Paint, vector.MaxPaints)}
}
func parse(t *testing.T, src string) (vector.Scene, Canvas) {
	t.Helper()
	b := vector.Budget{Max: vector.MaxWork}
	s, c, f := Parse([]byte(src), storage(), &b)
	if f.Code != vector.OK {
		t.Fatalf("%+v for %s", f, src)
	}
	return s, c
}
func wrap(body string) string { return `<svg width="64" height="64">` + body + `</svg>` }

func TestElementsDefaultsAndColors(t *testing.T) {
	for _, body := range []string{
		`<path d=""/>`, `<polygon points=""/>`, `<polygon points="1 2"/>`,
		`<path d="M1 2 L4 5 H6 V7 Q1 2 3 4 T5 6 C1 2 3 4 5 6 S7 8 9 10 Z"/>`,
		`<path d="m1 2 3 4 l1-2 h3 v4 q1 2 3 4 t5 6 c1 2 3 4 5 6 s7 8 9 10 z"/>`,
		`<rect width="3px" height="4" x="1" y="2"/>`,
		`<rect width="3" height="4" rx="8"/>`, `<rect width="3" height="4" ry="8"/>`,
		`<circle r="4"/>`, `<ellipse rx="5" ry="3"/>`,
		`<g><title id="metadata">A &amp; B &#x1f600;</title><desc>ignored</desc><rect width="0" height="5"/></g>`,
		`<path d="M0 0z L2 2 4 0"/>`,
	} {
		parse(t, wrap(body))
	}
	for _, tc := range []struct {
		name string
		rgb  uint32
	}{{"black", 0xff000000}, {"white", 0xffffffff}, {"red", 0xffff0000},
		{"green", 0xff008000}, {"blue", 0xff0000ff}, {"#aBc", 0xffaabbcc}, {"#AbCdEf", 0xffabcdef}, {"none", 0}} {
		s, _ := parse(t, wrap(`<rect width="1" height="1" fill="`+tc.name+`"/>`))
		if s.Paints[0].Color != tc.rgb {
			t.Fatalf("%s: %x", tc.name, s.Paints[0].Color)
		}
	}
	s, c := parse(t, "\xef\xbb\xbf"+`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><svg xmlns="http://www.w3.org/2000/svg" width="64px" height="64"><!-- comment --></svg>`)
	if len(s.Paints) != 0 || c.Width != 64 {
		t.Fatal(s, c)
	}
}
func TestInheritanceViewportAndReflection(t *testing.T) {
	s, _ := parse(t, wrap(`<g fill="red" fill-opacity=".5" transform="translate(2 3) scale(2)"><path fill-opacity="1" d="M1 1 Q2 3 4 5 T6 7 L8 8 T9 9 C1 2 3 4 5 6 S7 8 9 10"/></g>`))
	p := s.Paints[0]
	if p.Color != 0xffff0000 || p.Transform != (vector.Affine{A: 2, D: 2, E: 2, F: 3}) {
		t.Fatal(p)
	}
	if s.Commands[2].P[0] != (vector.Point{X: 6, Y: 7}) || s.Commands[4].P[0] != (vector.Point{X: 8, Y: 8}) ||
		s.Commands[6].P[0] != (vector.Point{X: 7, Y: 8}) {
		t.Fatal(s.Commands)
	}
	for _, mode := range []string{"none", "xMidYMid meet"} {
		s, _ := parse(t, `<svg width="64" height="32" viewBox="10 20 16 16" preserveAspectRatio="`+mode+`"><path d="M10 20L26 36"/></svg>`)
		a := s.Paints[0].Transform
		want := vector.Affine{A: 4, D: 2, E: -40, F: -40}
		if mode != "none" {
			want = vector.Affine{A: 2, D: 2, E: -4, F: -40}
		}
		if a != want {
			t.Fatal(mode, a)
		}
	}
}
func TestRefusals(t *testing.T) {
	for _, tc := range []struct {
		body string
		code vector.Code
	}{
		{`<text/>`, vector.UnsupportedText}, {`<tspan/>`, vector.UnsupportedText},
		{`<image/>`, vector.ExternalResource}, {`<foreignObject/>`, vector.ExternalResource}, {`<a/>`, vector.ExternalResource},
		{`<rect width="1" height="1" href="x"/>`, vector.ExternalResource},
		{`<path d="M0 0 A1 1 0 0 0 2 2"/>`, vector.UnsupportedFeature},
		{`<rect width="1" height="1" stroke="red"/>`, vector.UnsupportedFeature},
		{`<g opacity=".5"/>`, vector.UnsupportedFeature}, {`<path d="L1 2"/>`, vector.Malformed},
		{`<path d="M1 2 3"/>`, vector.Malformed}, {`<path d="M1,,2"/>`, vector.Malformed},
		{`<polygon points="1 2,"/>`, vector.Malformed}, {`<circle r="-1"/>`, vector.Malformed},
		{`<circle r="32769"/>`, vector.CoordinateLimit}, {`<circle r="1e999"/>`, vector.CoordinateLimit},
		{`<g id="&bad;"/>`, vector.Malformed}, {`<g id="&#0;"/>`, vector.Malformed},
		{`<g id="&#xD800;"/>`, vector.Malformed}, {`<g id="&#1114112;"/>`, vector.Malformed},
		{`<g id="a" id="b"/>`, vector.Malformed}, {`<g></path>`, vector.Malformed},
		{`<g transform="scale(257)"/>`, vector.CoordinateLimit},
		{`<g transform="rotate(3)"/>`, vector.UnsupportedFeature},
		{`<g transform="translate(1,)"/>`, vector.Malformed},
		{`<rect width="1%" height="1"/>`, vector.UnsupportedFeature},
		{`<title><path d=""/></title>`, vector.UnsupportedFeature},
	} {
		for _, hidden := range []bool{false, true} {
			body := tc.body
			if hidden {
				body = `<g fill="none" fill-opacity="0">` + body + `</g>`
			}
			b := vector.Budget{Max: vector.MaxWork}
			s, c, f := Parse([]byte(wrap(body)), storage(), &b)
			if f.Code != tc.code || s.Commands != nil || c != (Canvas{}) || f.Offset < 0 {
				t.Fatalf("%s: %+v", body, f)
			}
		}
	}
	for _, name := range []string{"line", "polyline", "defs", "use", "symbol", "svg", "clipPath", "mask", "filter", "animate", "set", "script", "style", "unknown", "p:g"} {
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(wrap("<"+name+"/>")), storage(), &b)
		if f.Code != vector.UnsupportedFeature {
			t.Fatal(name, f)
		}
	}
	for _, src := range []string{`<!DOCTYPE svg><svg/>`, `<?other?><svg/>`, wrap(`<![CDATA[a]]>`)} {
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(src), storage(), &b)
		if f.Code != vector.UnsupportedXML {
			t.Fatal(src, f)
		}
	}
}
func TestAllocationAndRecovery(t *testing.T) {
	out := storage()
	src := []byte(wrap(`<g fill="#aBc" transform="translate(1,2) scale(2)"><circle r="3"/><path d="M1 2Q3 4 5 6T7 8"/></g>`))
	b := vector.Budget{Max: vector.MaxWork}
	if n := testing.AllocsPerRun(100, func() {
		b.Used = 0
		_, _, f := Parse(src, out, &b)
		if f.Code != vector.OK {
			panic(f.Error())
		}
	}); n != 0 {
		t.Fatal(n)
	}
	bad := []byte(wrap(`<path d="M1"/>`))
	for i := 0; i < 100; i++ {
		b.Used = 0
		_, _, f := Parse(bad, out, &b)
		if f.Code != vector.Malformed {
			t.Fatal(f)
		}
		b.Used = 0
		_, _, f = Parse(src, out, &b)
		if f.Code != vector.OK {
			t.Fatal(f)
		}
	}
}
func TestAdmissionAndDepthLimits(t *testing.T) {
	for _, tc := range []struct {
		src  string
		code vector.Code
	}{
		{strings.Repeat(" ", 1_048_577), vector.SourceLimit},
		{wrap(strings.Repeat("<g>", 32) + strings.Repeat("</g>", 32)), vector.DepthLimit},
		{wrap(`<g id="` + strings.Repeat("a", 65) + `"/>`), vector.TokenLimit},
		{wrap(`<path d="M` + strings.Repeat("0", 33) + ` 1"/>`), vector.TokenLimit},
	} {
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(tc.src), storage(), &b)
		if f.Code != tc.code {
			t.Fatal(tc.code, f)
		}
	}
	parse(t, wrap(strings.Repeat("<g>", 31)+strings.Repeat("</g>", 31)))
}
func TestPinnedAnalyticFixture(t *testing.T) {
	src, err := os.ReadFile("../../../tests/fixtures/svg/engine/alpha.svg")
	if err != nil {
		t.Fatal(err)
	}
	b := vector.Budget{Max: vector.MaxWork}
	s, c, f := Parse(src, storage(), &b)
	if f.Code != vector.OK {
		t.Fatal(f)
	}
	dst := vector.Target{Pix: make([]uint32, c.Width*c.Height), Width: c.Width, Height: c.Height, Stride: c.Width}
	_, f = vector.Rasterize(s, dst, vector.Workspace{Bytes: make([]byte, vector.WorkspaceSize)}, &b)
	if f.Code != vector.OK {
		t.Fatal(f)
	}
	// Hand-derived source-over: alpha 128 then alpha 128 => alpha 192;
	// red and blue weights are 128*127 and 128*255 respectively.
	if dst.Pix[0] != 0x80ff0000 || dst.Pix[1] != 0xc05500aa || dst.Pix[2] != 0x800000ff || dst.Pix[3] != 0 {
		t.Fatalf("%x", dst.Pix)
	}
}
