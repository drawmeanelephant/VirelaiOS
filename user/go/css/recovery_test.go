package css

import (
	"testing"
	"virelai/webstyle"
)

func TestCSS21RecoveryExamples(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         uint32
	}{
		{"unknown-property", `span{color:red; rotation:70minutes; color:green}`, 0xff008000},
		{"unknown-value", `span{color:green; color:10}`, 0xff008000},
		{"invalid-declaration", `span{color:green; color}`, 0xff008000},
		{"invalid-combined-declaration", `span{color:green; color:red color:blue}`, 0xff008000},
		{"missing-colon", `span{color:green; color red; background-color:blue}`, 0xff008000},
		{"unclosed-string-newline", "span{font-family:\"broken\n; color:green}", 0xff008000},
		{"invalid-rule", `span, ? {color:red} span{color:green}`, 0xff008000},
		{"unknown-at-block", `@three-dee{span{color:red}@nested{x{color:blue}}}span{color:green}`, 0xff008000},
		{"unknown-at-statement", `@unknown "semicolon; not a delimiter";span{color:green}`, 0xff008000},
		{"balanced-invalid-declaration", `span{color:green; invalid:fun(";",{a:b;}); color:red}`, 0xffff0000},
		{"strings-braces", `@unknown "{semicolon;}";span{color:green}`, 0xff008000},
		{"comment-delimiters", `span{color:green;/* ; } @media */color:red}`, 0xffff0000},
		{"comment-not-descendant", `sp/**/an{color:red} span{color:green}`, 0xff008000},
		{"eof-closes-rule", `span{color:green`, 0xff008000},
		{"cdo-cdc", `<!-- span{color:green} -->`, 0xff008000},
		{"escape-important", `span{color:green!\69mportant; color:red}`, 0xff008000},
		{"string-close-brace", `span{font-family:"}";color:green}`, 0xff008000},
		{"string-open-paren", `span{font-family:"(";color:green}`, 0xff008000},
		{"string-comma", `span{font-family:",";color:green}`, 0xff008000},
		{"invalid-function-delimiters", `span{color:green;color:rgb(1 "," 2 "," 3 ")")}`, 0xff008000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, s, _ := compute(t, `<span id="target">text</span>`, tc.source)
			if got := s.ForNode(find(doc, "target")).Color; got != rgba(tc.want) {
				t.Fatal(tc.source, got, rgba(tc.want))
			}
		})
	}
}

func TestTokenEscapesStringsCommentsAndLocations(t *testing.T) {
	p := newParser("\r\n/*ignored*/\n\\63 olor:rgb(1,2,3); font-family:\"Fira\\20 Code\"")
	ds := p.declarations(false)
	if len(p.diags.list) != 0 || len(ds) != 2 || ds[0].value.color != rgba(0xff010203) ||
		ds[1].value.enum != uint8(webstyle.FontMono) {
		t.Fatal(ds, p.diags.list)
	}
	_, diags := Parse([]byte("\nspan{\n  float:right;\n}"))
	if len(diags) != 1 || diags[0].Line != 3 || diags[0].Col != 3 {
		t.Fatal("byte locations", diags)
	}
	for _, input := range []string{"span{color:\xff}", "span{color:\x00}", `span{color:"unterminated}`} {
		_, diags := Parse([]byte(input))
		if len(diags) == 0 {
			t.Fatal("malformed input not diagnosed", input)
		}
	}
}
