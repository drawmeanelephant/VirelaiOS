package svg

import (
	"strings"
	"testing"
	"virelai/vector"
)

func TestExclusionClassification(t *testing.T) {
	// ADR 0041 §3.3, in row order. Memory/time admission belongs to the
	// adapter, not Parse; the limits row exercises Parse's own limit codes.
	type testCase struct {
		family, name, body string
		code               vector.Code
		root               string
		budget             uint64
	}
	var cases []testCase
	add := func(family, name, body string, code vector.Code) {
		cases = append(cases, testCase{family: family, name: name, body: body, code: code, budget: vector.MaxWork})
	}
	elements := func(family string, code vector.Code, names ...string) {
		for _, name := range names {
			add(family, "element/"+name, "<"+name+"/>", code)
		}
	}
	attributes := func(family string, code vector.Code, names ...string) {
		for _, name := range names {
			add(family, "attribute/"+name, "<g "+name+`="x"/>`, code)
		}
	}
	elements("text-refusal", vector.UnsupportedText, "text", "tspan", "textPath",
		"font", "font-face", "font-face-src", "font-face-uri", "font-face-name", "font-face-format",
		"glyph", "missing-glyph", "hkern", "vkern")
	attributes("text-refusal", vector.UnsupportedText, "font", "font-family", "font-size",
		"font-style", "font-weight", "font-variant", "font-stretch", "font-size-adjust")
	elements("stroke-refusal", vector.UnsupportedFeature, "line", "polyline", "marker")
	attributes("stroke-refusal", vector.UnsupportedFeature, "stroke-width", "stroke-linecap",
		"stroke-linejoin", "stroke-miterlimit", "stroke-dasharray", "stroke-dashoffset",
		"stroke-opacity", "marker-start", "marker-mid", "marker-end", "vector-effect")
	add("stroke-refusal", "stroke", `<rect width="1" height="1" stroke="red"/>`, vector.UnsupportedFeature)
	for _, cmd := range []string{"A", "a"} {
		add("syntax-subset", cmd, `<path d="M0 0 `+cmd+`1 1 0 0 0 2 2"/>`, vector.UnsupportedFeature)
	}
	for _, name := range []string{"rotate", "skewX", "skewY", "unknown", "Scale"} {
		add("syntax-subset", name, `<g transform="`+name+`(1)"/>`, vector.UnsupportedFeature)
	}
	for _, unit := range []string{"em", "ex", "EM", "EX", "%", "pt", "pc", "in", "cm", "mm"} {
		add("syntax-subset", unit, `<rect width="1`+unit+`" height="1"/>`, vector.UnsupportedFeature)
	}
	for _, color := range []string{"yellow", "rgb(1,2,3)", "#12345678"} {
		add("syntax-subset", color, `<g fill="`+color+`"/>`, vector.UnsupportedFeature)
	}
	for _, mode := range []string{"xMinYMin meet", "xMidYMid slice", "defer xMidYMid meet"} {
		cases = append(cases, testCase{family: "syntax-subset", name: mode, code: vector.UnsupportedFeature,
			root: ` preserveAspectRatio="` + mode + `"`, budget: vector.MaxWork})
	}
	elements("paint-refusal", vector.UnsupportedFeature, "linearGradient", "radialGradient", "stop",
		"pattern", "defs", "use", "symbol", "svg", "clipPath", "mask")
	elements("resource-refusal", vector.ExternalResource, "image", "foreignObject", "a")
	attributes("resource-refusal", vector.ExternalResource, "href", "xlink:href", "src", "srcset",
		"resource", "url", "xml:base")
	for _, value := range []string{"url(#paint)", "data:image/svg+xml,x", "file:x", "http://example.invalid/x", "https://example.invalid/x"} {
		add("resource-refusal", value, `<g fill="`+value+`"/>`, vector.ExternalResource)
	}
	elements("filter-refusal", vector.UnsupportedFeature, "filter", "feGaussianBlur", "feBlend", "feColorMatrix")
	attributes("filter-refusal", vector.UnsupportedFeature, "filter", "filterUnits", "primitiveUnits")
	elements("animation-refusal", vector.UnsupportedFeature, "animate", "animateTransform", "animateMotion", "animateColor", "set")
	add("animation-refusal", "CSS animation", `<g style="animation: spin 1s"/>`, vector.UnsupportedFeature)
	elements("script-refusal", vector.UnsupportedFeature, "script")
	attributes("script-refusal", vector.UnsupportedFeature, "onload", "onclick", "onunknown")
	elements("style-refusal", vector.UnsupportedFeature, "style")
	attributes("style-refusal", vector.UnsupportedFeature, "style", "class", "opacity", "display",
		"visibility", "mix-blend-mode", "paint-order")
	for _, value := range []string{"currentColor", "inherit"} {
		add("style-refusal", value, `<g fill="`+value+`"/>`, vector.UnsupportedFeature)
	}
	elements("unknown-refusal", vector.UnsupportedFeature, "unknown", "p:g")
	attributes("unknown-refusal", vector.UnsupportedFeature, "unknown", "p:fill", "xmlns:p")
	for _, attrs := range []string{` version="1.1"`, ` baseProfile="tiny"`, ` xmlns="urn:foreign"`} {
		cases = append(cases, testCase{family: "unknown-refusal", name: attrs, root: attrs,
			code: vector.UnsupportedFeature, budget: vector.MaxWork})
	}
	add("metadata", "bounded ignore", " \n<!-- comment --><g id=\"url(#opaque)\"/><title id=\"title\">A &amp; B</title><desc>&#49;</desc>\t", vector.OK)
	for _, xml := range []string{`<!DOCTYPE svg>`, `<!ENTITY x "y">`, `<!DOCTYPE svg SYSTEM "file:x">`,
		`<![CDATA[x]]>`, `<?other?>`, `<?xml version="1.0"?>`} {
		add("xml-subset", xml, xml, vector.UnsupportedXML)
	}
	for _, tc := range []struct{ name, body string }{
		{"XML", `<g id=x/>`}, {"UTF-8", "<title>\xff</title>"}, {"entity", `<g id="&bad;"/>`},
		{"duplicate", `<g id="a" id="b"/>`}, {"mismatch", `<g></path>`},
		{"path group", `<path d="M1 2 L3"/>`}, {"radius", `<circle r="-1"/>`},
		{"extent", `<rect width="-1" height="1"/>`}, {"NaN", `<circle r="NaN"/>`},
		{"Inf", `<circle r="Inf"/>`}, {"exponent", `<circle r="1e"/>`},
	} {
		add("malformed", tc.name, tc.body, vector.Malformed)
	}
	add("malformed", "numeric cap", `<circle r="32769"/>`, vector.CoordinateLimit)
	add("malformed", "overflow", `<circle r="1e999"/>`, vector.CoordinateLimit)
	for _, tc := range []struct {
		name, body string
		code       vector.Code
	}{
		{"source", strings.Repeat(" ", 1_048_577), vector.SourceLimit},
		{"depth", strings.Repeat("<g>", 32) + strings.Repeat("</g>", 32), vector.DepthLimit},
		{"nodes", strings.Repeat("<g/>", 4096), vector.NodeLimit},
		{"token", `<g id="` + strings.Repeat("x", 65) + `"/>`, vector.TokenLimit},
		{"commands", `<path d="M0 0 ` + strings.Repeat("L0 0 ", 8191) + `Z"/>`, vector.CommandLimit},
		{"contours", `<path d="` + strings.Repeat("M0 0Z ", 1025) + `"/>`, vector.ContourLimit},
		{"scene", strings.Repeat(`<path d=""/>`, 1025), vector.SceneLimit},
	} {
		add("limits", tc.name, tc.body, tc.code)
	}
	cases = append(cases, testCase{family: "limits", name: "work", code: vector.WorkLimit})

	out := storage()
	for _, tc := range cases {
		t.Run(tc.family+"/"+tc.name, func(t *testing.T) {
			for _, context := range []struct{ name, open, close string }{
				{"normal", "", ""},
				{"hidden", `<g fill="none">`, `</g>`},
				{"zero-alpha", `<g fill-opacity="0">`, `</g>`},
			} {
				t.Run(context.name, func(t *testing.T) {
					src := `<svg width="64" height="64"` + tc.root + `>` + context.open + tc.body + context.close + `</svg>`
					b := vector.Budget{Max: tc.budget}
					s, c, f := Parse([]byte(src), out, &b)
					if f.Code != tc.code {
						t.Fatalf("got %#v, want code %d", f, tc.code)
					}
					if f.Code != vector.OK && (s.Commands != nil || s.Paints != nil || c != (Canvas{}) ||
						f.Offset < 0 || f.Offset > len(src)) {
						t.Fatalf("invalid refusal result: %#v", f)
					}
				})
			}
		})
	}
}

func TestUnsupportedSyntaxOffsets(t *testing.T) {
	for _, tc := range []struct{ body, token string }{
		{`<g transform="skewX(1)"/>`, "skewX"},
		{`<g transform="skewY(1)"/>`, "skewY"},
		{`<g transform="translate(1) skewX(1)"/>`, "skewX"},
		{`<g transform="scale(1), skewY(1)"/>`, "skewY"},
		{`<rect width="1em" height="1"/>`, "1em"},
		{`<rect width=" 1em" height="1"/>`, "1em"},
		{`<rect width="&#49;em" height="1"/>`, "&#49;em"},
		{`<rect width="1&#101;m" height="1"/>`, "1&#101;m"},
		{`<rect width="1e&#109;" height="1"/>`, "1e&#109;"},
		{`<g transform="skew&#88;(1)"/>`, "skew&#88;"},
		{`<g transform="&#115;kewY(1)"/>`, "&#115;kewY"},
	} {
		for _, group := range []string{"", `<g fill="none">`, `<g fill-opacity="0">`} {
			body := group + tc.body
			if group != "" {
				body += "</g>"
			}
			src := wrap(body)
			b := vector.Budget{Max: vector.MaxWork}
			_, _, f := Parse([]byte(src), storage(), &b)
			if f.Code != vector.UnsupportedFeature || f.Offset != strings.Index(src, tc.token) {
				t.Fatalf("%s: got %#v, want code 2 at %d", src, f, strings.Index(src, tc.token))
			}
		}
	}
}

func TestScientificNotationClassification(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  float64
		code  vector.Code
	}{
		{"1e1", 10, vector.OK}, {"1E+1", 10, vector.OK}, {"1e-1", .1, vector.OK},
		{"1.25e1px", 12.5, vector.OK}, {"0e999", 0, vector.OK},
		{"1e", 0, vector.Malformed}, {"1e+", 0, vector.Malformed}, {"1e-", 0, vector.Malformed},
		{"1e+em", 0, vector.Malformed}, {"1e999", 0, vector.CoordinateLimit},
	} {
		b := vector.Budget{Max: vector.MaxWork}
		p := parser{src: []byte(tc.value), b: &b}
		got := p.scalar(span{0, len(tc.value)}, true)
		if p.f.Code != tc.code || tc.code == vector.OK && got != tc.want {
			t.Fatalf("%s: got %g, %#v; want %g, code %d", tc.value, got, p.f, tc.want, tc.code)
		}
	}
}
