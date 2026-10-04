package svg

import (
	"math"
	"strings"
	"testing"
	"virelai/vector"
)

func TestEllipseErrorBound(t *testing.T) {
	for _, a := range []vector.Affine{{A: 1, D: 1}, {A: 3, B: 2, C: -1, D: .5},
		{A: -2, D: 1}, {}, {A: 256, D: 256}} {
		b := vector.Budget{Max: vector.MaxWork}
		p := parser{out: storage(), b: &b}
		p.targetCommand(frame{transform: a}, vector.Move, vector.Point{X: 3, Y: 0})
		p.arc(frame{transform: a}, 0, 0, 3, 2, 0, math.Pi/2)
		if p.f.Code != vector.OK {
			t.Fatal(p.f)
		}
		n := p.nc - 1
		for i := 0; i < n; i++ {
			left := p.out.Commands[i].P[0]
			right := p.out.Commands[i+1].P[0]
			for k := 1; k < 8; k++ {
				u := float64(k) / 8
				angle := (float64(i) + u) / float64(n) * math.Pi / 2
				x, y := 3*math.Cos(angle), 2*math.Sin(angle)
				q := vector.Point{X: a.A*x + a.C*y + a.E, Y: a.B*x + a.D*y + a.F}
				linear := vector.Point{X: left.X*(1-u) + right.X*u, Y: left.Y*(1-u) + right.Y*u}
				if math.Hypot(q.X-linear.X, q.Y-linear.Y) > 1.0/16 {
					t.Fatal("analytic ellipse tolerance exceeded", a, i, k)
				}
			}
		}
	}
}
func TestAllPathAritiesAndSeparators(t *testing.T) {
	for _, cmd := range []string{"M", "m", "L", "l", "H", "h", "V", "v", "Q", "q", "T", "t", "C", "c", "S", "s"} {
		arity := 2
		switch strings.ToUpper(cmd) {
		case "H", "V":
			arity = 1
		case "Q", "S":
			arity = 4
		case "C":
			arity = 6
		}
		// Two complete repeated groups, with ordinary and sign-separated
		// numeric spelling, and every incomplete final group.
		full := "M0 0 " + cmd + strings.Repeat("1 ", arity*2)
		parse(t, wrap(`<path d="`+full+`"/>`))
		signs := "M0 0 " + cmd + strings.Repeat("+1", arity*2)
		parse(t, wrap(`<path d="`+signs+`"/>`))
		for n := 0; n < arity; n++ {
			b := vector.Budget{Max: vector.MaxWork}
			_, _, f := Parse([]byte(wrap(`<path d="M0 0 `+cmd+strings.Repeat("1 ", n)+`"/>`)), storage(), &b)
			if f.Code != vector.Malformed {
				t.Fatal(cmd, n, f)
			}
		}
	}
	for _, data := range []string{"M0,0,L1 1", "M0 0,", "M0 0 L1,1,", "M0 0 L1.2.3", "M0 0 Z1", "M0 0 L1e+ 2"} {
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(wrap(`<path d="`+data+`"/>`)), storage(), &b)
		if f.Code == vector.OK {
			t.Fatal("accepted bad separators", data)
		}
	}
	for _, data := range []string{"M1e-1,+.2L3,4", "M0 0Z Z", "M0 0ZM1 1", "M0 0 L-1-2", ""} {
		parse(t, wrap(`<path d="`+data+`"/>`))
	}
}
func TestAttributeExclusionFamiliesAndMetadata(t *testing.T) {
	for _, name := range []string{"stroke-width", "stroke-linecap", "stroke-linejoin", "stroke-dasharray", "marker-end", "vector-effect",
		"filter", "mask", "clip-path", "onload", "onclick", "style", "class", "opacity",
		"display", "visibility", "mix-blend-mode", "paint-order", "version", "xmlns:foo", "unknown"} {
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(wrap(`<g fill="none" `+name+`="x"/>`)), storage(), &b)
		if f.Code != vector.UnsupportedFeature {
			t.Fatal(name, f)
		}
	}
	for _, name := range []string{"font-family", "font-size", "font-style", "font-weight", "font-variant", "font-stretch"} {
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(wrap(`<g fill="none" `+name+`="x"/>`)), storage(), &b)
		if f.Code != vector.UnsupportedText {
			t.Fatal(name, f)
		}
	}
	for _, name := range []string{"font", "font-face", "font-face-src", "font-face-uri", "glyph", "missing-glyph"} {
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(wrap("<"+name+"/>")), storage(), &b)
		if f.Code != vector.UnsupportedText {
			t.Fatal(name, f)
		}
	}
	for _, data := range []string{`fill="url(#paint)"`, `unknown="data:x"`, `xlink:href="x"`, `src="file:x"`} {
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(wrap("<g "+data+"/>")), storage(), &b)
		if f.Code != vector.ExternalResource {
			t.Fatal(data, f)
		}
	}
	parse(t, wrap(`<title>&lt;&gt;&amp;&quot;&apos;&#00000000000000000000000000000065;</title>`))
	for _, data := range []string{`<title>]]></title>`, `<title>&#x;</title>`, `<title>&#;</title>`, `<title>&#X41;</title>`} {
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse([]byte(wrap(data)), storage(), &b)
		if f.Code != vector.Malformed {
			t.Fatal(data, f)
		}
	}
}
func TestRoundedRectRadiusCopyAndClamp(t *testing.T) {
	var before []vector.Command
	for _, attrs := range []string{`rx="99"`, `ry="99"`, `rx="4" ry="3"`} {
		s, _ := parse(t, wrap(`<rect x="1" y="2" width="8" height="6" `+attrs+`/>`))
		if before == nil {
			before = s.Commands
		} else {
			if len(before) != len(s.Commands) {
				t.Fatal("radius copy or clamp", len(before), len(s.Commands))
			}
			for i := range before {
				if before[i] != s.Commands[i] {
					t.Fatal("radius copy or clamp", i)
				}
			}
		}
		if s.Paints[0].Transform != identity {
			t.Fatal("ellipse expansion must use identity transform")
		}
	}
}
func TestLocalBudgetDoesNotEscape(t *testing.T) {
	src, out := []byte(wrap(`<path d="M1 2L3 4Z"/>`)), storage()
	if n := testing.AllocsPerRun(100, func() {
		b := vector.Budget{Max: vector.MaxWork}
		_, _, f := Parse(src, out, &b)
		if f.Code != vector.OK {
			panic(f.Error())
		}
	}); n != 0 {
		t.Fatal("caller-owned budget escaped", n)
	}
}

func TestViewportDoesNotHideLocalCompositionLimit(t *testing.T) {
	src := []byte(`<svg width="1" height="1" viewBox="0 0 32768 32768"><g transform="scale(256)"><g transform="scale(256)"/></g></svg>`)
	b := vector.Budget{Max: vector.MaxWork}
	_, _, f := Parse(src, storage(), &b)
	if f.Code != vector.CoordinateLimit {
		t.Fatal(f)
	}
}

func FuzzParse(f *testing.F) {
	for _, src := range []string{"\xef", wrap(""), wrap(`<path d="M0 0Q1 2 3 4"/>`), wrap(`<g id="&#49;"/>`), wrap(`<circle r="5"/>`)} {
		f.Add([]byte(src))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		if len(src) > 2048 {
			return
		}
		b := vector.Budget{Max: 100_000}
		s, c, fail := Parse(src, storage(), &b)
		if b.Used > b.Max || fail.Offset > len(src) || fail.Code != vector.OK && (s.Commands != nil || c != (Canvas{})) {
			t.Fatal(fail, b)
		}
	})
}
