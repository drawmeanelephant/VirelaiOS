package css

import (
	"strings"
	"testing"
	"unicode/utf8"
	"virelai/webrender"
	"virelai/webstyle"
)

func hasDiagnostic(ds []webstyle.Diagnostic, prefix string) bool {
	for _, d := range ds {
		if strings.HasPrefix(d.Text, prefix) {
			return true
		}
	}
	return false
}

func checkDiagnostics(t testing.TB, ds []webstyle.Diagnostic) {
	t.Helper()
	if len(ds) > webstyle.MaxDiagnostics {
		t.Fatal("diagnostic count exceeded", len(ds))
	}
	for i, d := range ds {
		if len(d.Text) > webstyle.MaxDiagnosticTextBytes || !utf8.ValidString(d.Text) {
			t.Fatal("diagnostic text exceeded or invalid", d)
		}
		if d.Text == "diagnostics-truncated" && i != len(ds)-1 {
			t.Fatal("summary not terminal", ds)
		}
	}
}

func TestParserBoundsAtAndBeyond(t *testing.T) {
	// Whole sources, not arbitrary byte prefixes, are admitted.
	base := "span{color:green}"
	exact := base + "/*" + strings.Repeat("x", webstyle.MaxCSSBytes-len(base)-4) + "*/"
	if s, ds := Parse([]byte(exact)); len(ds) != 0 || len(s.rules) != 1 {
		t.Fatal("exact byte cap", ds)
	}
	if s, ds := Parse([]byte(exact + " ")); len(s.rules) != 0 || !hasDiagnostic(ds, "css-byte-limit") {
		t.Fatal("byte cap+1", ds)
	}
	rules := strings.Repeat("span{color:green}", webstyle.MaxCSSRules)
	if s, ds := Parse([]byte(rules)); len(s.rules) != webstyle.MaxCSSRules || len(ds) != 0 {
		t.Fatal("exact rule cap", ds)
	}
	if s, ds := Parse([]byte(rules + "span{color:red}")); len(s.rules) != webstyle.MaxCSSRules || !hasDiagnostic(ds, "css-rule-limit") {
		t.Fatal("rule cap+1", ds)
	}
	for _, cap := range []int{webstyle.MaxSelectorsPerRule, webstyle.MaxSelectorsPerRule + 1} {
		src := strings.TrimSuffix(strings.Repeat("span,", cap), ",") + "{color:red}"
		s, ds := Parse([]byte(src))
		if cap == webstyle.MaxSelectorsPerRule {
			if len(ds) != 0 || len(s.rules) != 1 || len(s.rules[0].selectors) != cap {
				t.Fatal("exact selector cap", ds)
			}
		} else if len(s.rules) != 0 || !hasDiagnostic(ds, "css-selector-limit") {
			t.Fatal("selector cap+1", ds)
		}
	}
	for _, count := range []int{webstyle.MaxSelectorParts, webstyle.MaxSelectorParts + 1} {
		s, ds := Parse([]byte(strings.Repeat("span ", count) + "{color:red}"))
		if count == webstyle.MaxSelectorParts {
			if len(ds) != 0 || len(s.rules) != 1 || len(s.rules[0].selectors[0].parts) != count {
				t.Fatal("exact descendant cap", ds)
			}
		} else if len(s.rules) != 0 || !hasDiagnostic(ds, "css-selector-limit") {
			t.Fatal("descendant cap+1", ds)
		}
	}
	decls := strings.Repeat("color:green;", webstyle.MaxDeclarationsPerRule)
	s, ds := Parse([]byte("span{" + decls + "color:red}"))
	if len(s.rules) != 1 || len(s.rules[0].decls) != webstyle.MaxDeclarationsPerRule || !hasDiagnostic(ds, "css-declaration-limit") {
		t.Fatal("declaration cap+1", ds)
	}
	s, ds = Parse([]byte("span{" + decls + "}"))
	if len(ds) != 0 || len(s.rules[0].decls) != webstyle.MaxDeclarationsPerRule {
		t.Fatal("exact declaration cap", ds)
	}
	for _, count := range []int{webstyle.MaxCSSTokenBytes, webstyle.MaxCSSTokenBytes + 1} {
		s, ds := Parse([]byte("." + strings.Repeat("x", count) + "{color:red} span{color:green}"))
		if count == webstyle.MaxCSSTokenBytes {
			if len(ds) != 0 || len(s.rules) != 2 {
				t.Fatal("exact token cap", ds)
			}
		} else if len(s.rules) != 1 || !hasDiagnostic(ds, "css-token-limit") {
			t.Fatal("token cap+1 recovery", ds)
		}
	}
	for _, count := range []int{webstyle.MaxCSSNesting, webstyle.MaxCSSNesting + 1} {
		src := "@unknown{" + strings.Repeat("{", count) + "span{color:red}" + strings.Repeat("}", count) + "}span{color:green}"
		s, ds := Parse([]byte(src))
		if len(s.rules) != 1 || count > webstyle.MaxCSSNesting && !hasDiagnostic(ds, "css-nesting-limit") {
			t.Fatal("nesting recovery", ds)
		}
	}
}

func TestPageBoundsAggregateSheetsInlineRulesAndDiagnostics(t *testing.T) {
	doc := webrender.ParseHTML([]byte(`<span id="target" style="color:blue">text</span>`))
	green, _ := Parse([]byte("span{color:green}"))
	red, _ := Parse([]byte("span{color:red!important}"))
	sheets := []*Stylesheet{green, green, green, green, green, green, green, green, red}
	s, ds := Cascade(doc, sheets)
	if !hasDiagnostic(ds, "css-sheet-limit") || s.ForNode(find(doc, "target")).Color != rgba(0xff0000ff) {
		t.Fatal("aggregate sheet bound", ds)
	}
	base := "span{color:green}"
	first, _ := Parse([]byte(base + "/*" + strings.Repeat("x", webstyle.MaxCSSBytes-len(base)-4) + "*/"))
	s, ds = Cascade(doc, []*Stylesheet{first, red})
	if !hasDiagnostic(ds, "css-byte-limit") || s.ForNode(find(doc, "target")).Color != rgba(0xff008000) {
		t.Fatal("aggregate byte / inline bound", first.bytes, ds)
	}
	many, _ := Parse([]byte(strings.Repeat("span{color:green}", webstyle.MaxCSSRules)))
	s, ds = Cascade(doc, []*Stylesheet{many, red})
	if !hasDiagnostic(ds, "css-rule-limit") || s.ForNode(find(doc, "target")).Color != rgba(0xff0000ff) {
		t.Fatal("aggregate rules", ds)
	}
	invalid, _ := Parse([]byte(strings.Repeat("span:hover{color:red}", webstyle.MaxCSSRules-1) + "span{color:green}"))
	s, ds = Cascade(doc, []*Stylesheet{invalid, red})
	if !hasDiagnostic(ds, "diagnostics-truncated") || s.ForNode(find(doc, "target")).Color != rgba(0xff0000ff) {
		t.Fatal("excluded rules still count toward cap", ds)
	}
	checkDiagnostics(t, ds)
	noisy, _ := Parse([]byte(strings.Repeat("span{unsupported:1}", 70)))
	_, ds = Cascade(doc, []*Stylesheet{noisy, noisy})
	if len(ds) != webstyle.MaxDiagnostics || ds[len(ds)-1].Text != "diagnostics-truncated" {
		t.Fatal("page-wide diagnostic cap", ds)
	}
	p := newParser(`font-family:"` + strings.Repeat("é", 1000) + `"`)
	p.declarations(false)
	checkDiagnostics(t, p.diags.list)
}

func TestMatchingWorkLimitPreservesCompletedStyles(t *testing.T) {
	root := &webrender.Node{Tag: "document"}
	for i := 0; i < 2100; i++ {
		root.Children = append(root.Children, &webrender.Node{Tag: "span"})
	}
	doc := &webrender.Document{Root: root}
	sheet, _ := Parse([]byte(strings.Repeat("span{color:red}", webstyle.MaxCSSRules)))
	s, ds := Cascade(doc, []*Stylesheet{sheet})
	if !hasDiagnostic(ds, "css-work-limit") || len(s.values) != 2101 {
		t.Fatal("work limit", len(s.values), ds)
	}
	if s.ForNode(root.Children[0]).Color != rgba(0xffff0000) ||
		s.ForNode(root.Children[len(root.Children)-1]).Color == rgba(0xffff0000) {
		t.Fatal("work limit did not preserve completed / stop new author work")
	}
	work := maxMatchingWork
	if _, exhausted := (selector{parts: []compound{{}}}).matches([]*webrender.Node{root}, &work); !exhausted || work != maxMatchingWork {
		t.Fatal("comparison cap exceeded", work)
	}
}

func TestNilMalformedAndDOMBounds(t *testing.T) {
	for _, doc := range []*webrender.Document{nil, {}} {
		s, ds := Cascade(doc, nil)
		if len(s.values) != 0 || !hasDiagnostic(ds, "invalid-input") {
			t.Fatal("nil document", ds)
		}
	}
	var nilStyles *Styles
	if nilStyles.ForNode(nil) != initialStyle() {
		t.Fatal("nil callback receiver")
	}
	root := &webrender.Node{Tag: "document"}
	root.Children = []*webrender.Node{nil, root}
	_, ds := Cascade(&webrender.Document{Root: root}, []*Stylesheet{nil})
	if len(ds) != 3 {
		t.Fatal("invalid DOM/sheet", ds)
	}
	root = &webrender.Node{Tag: "document"}
	leaf := root
	for i := 0; i < webstyle.MaxDepth+1; i++ {
		child := &webrender.Node{Tag: "span"}
		leaf.Children = []*webrender.Node{child}
		leaf = child
	}
	_, ds = Cascade(&webrender.Document{Root: root}, nil)
	if !hasDiagnostic(ds, "css-depth-limit") {
		t.Fatal("depth bound", ds)
	}
	root = &webrender.Node{Tag: "document"}
	for i := 0; i < webstyle.MaxNodes; i++ {
		root.Children = append(root.Children, &webrender.Node{Tag: "span"})
	}
	s, ds := Cascade(&webrender.Document{Root: root}, nil)
	if len(s.values) != webstyle.MaxNodes || !hasDiagnostic(ds, "css-node-limit") {
		t.Fatal("node bound", ds)
	}
}

func FuzzCSS(f *testing.F) {
	for _, seed := range []string{
		"", "span{color:green}", `@media print{span{display:none}}span{float:right;color:red}`,
		`span:hover,#target{display:none}`, `span{font-family:"Fira\20 Code";border:2px solid rgb(1,2,3)}`,
		"span{color:\xff}", "span{font-family:\"bad\n; color:green}", "span{width:calc(1px + 1%)}",
		strings.Repeat("span{float:right}", 130),
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		sheet, ds := Parse(src)
		checkDiagnostics(t, ds)
		if len(sheet.rules) > webstyle.MaxCSSRules || sheet.ruleCount > webstyle.MaxCSSRules {
			t.Fatal("rule cap")
		}
		for _, rule := range sheet.rules {
			if len(rule.selectors) > webstyle.MaxSelectorsPerRule || len(rule.decls) > webstyle.MaxDeclarationsPerRule*12 {
				t.Fatal("selector/declaration cap")
			}
			for _, sel := range rule.selectors {
				if len(sel.parts) > webstyle.MaxSelectorParts {
					t.Fatal("descendant cap")
				}
			}
		}
		p := newParser(string(src))
		// Inputs outside the source cap are rejected by Parse; do not bypass that
		// protection when directly exercising inline declaration recovery.
		if len(src) <= webstyle.MaxCSSBytes {
			p.declarations(false)
			checkDiagnostics(t, p.diags.list)
		}
		doc := webrender.ParseHTML([]byte(`<main><span id="target" class="x">text</span></main>`))
		styles, ds := Cascade(doc, []*Stylesheet{sheet})
		checkDiagnostics(t, ds)
		for _, style := range styles.values {
			if style.FontSize.Value < 8 || style.FontSize.Value > 48 || style.Color.Kind != webstyle.ColorRGBA ||
				style.LineHeight.Value < 1 || style.LineHeight.Value > 128 {
				t.Fatal("unresolved or unbounded computed value", style)
			}
		}
		// Independently parsed excluded constructs never change any random
		// source's computed result, even when that source is malformed.
		excluded, _ := Parse([]byte(`*{float:right;position:absolute;grid-template-columns:1fr}
		*:hover{display:none}*::before{content:"hidden"}@media screen{*{display:none}}
		@import "x";@font-face{font-family:X;src:url(x)}`))
		mixed, ds := Cascade(doc, []*Stylesheet{sheet, excluded})
		checkDiagnostics(t, ds)
		// If adding a source crosses the page byte cap, it is still excluded.
		for node, style := range styles.values {
			if mixed.ForNode(node) != style {
				t.Fatal("unsupported construct changed random style")
			}
		}
	})
}
