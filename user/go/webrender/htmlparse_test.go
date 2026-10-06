package webrender

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Like TestDumpItems, this is an opt-in review aid. It never rewrites a golden.
func TestParseBrokenGoldenPreview(t *testing.T) {
	dir := os.Getenv("WEBRENDER_BROKEN_PREVIEW")
	if dir == "" {
		t.Skip("set WEBRENDER_BROKEN_PREVIEW to an artifact directory")
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("preview directory must be absolute")
	}
	before := mustReadTestdata(t, "testdata/golden/broken.png")
	_, f := renderFixture(t, "broken.html", 512, 384, 0)
	after := f.pngBytes(t)
	old, err := png.Decode(bytes.NewReader(before))
	if err != nil {
		t.Fatal(err)
	}
	bounds := old.Bounds()
	if bounds.Dx() != f.w || bounds.Dy() != f.h {
		t.Fatal("preview size changed")
	}
	diff := 0
	minX, minY, maxX, maxY := f.w, f.h, -1, -1
	for y := 0; y < f.h; y++ {
		for x := 0; x < f.w; x++ {
			r, g, b, _ := old.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			if f.px[y*f.w+x] == (r>>8)<<16|(g>>8)<<8|(b>>8) {
				continue
			}
			diff++
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"broken.before.png": before, "broken.after.png": after} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s (%d bytes)", path, len(data))
	}
	t.Logf("pixel-diff=%d bounds=(%d,%d)-(%d,%d)", diff, minX, minY, maxX, maxY)
}

// Parser changes must preserve the frozen compatibility renders. Compare the
// encoded bytes too, without creating review PNGs beside another lane's files.
func TestParseCompatibilityGoldens(t *testing.T) {
	for _, name := range []string{"simple", "broken", "corpus-httpbin-forms", "corpus-w3-styleguide", "corpus-w3-png"} {
		t.Run(name, func(t *testing.T) {
			var f *fb
			if strings.HasPrefix(name, "corpus-") {
				_, f = renderCorpus(t, strings.TrimPrefix(name, "corpus-")+".html", 512, 384)
			} else {
				_, f = renderFixture(t, name+".html", 512, 384, 0)
			}
			want := mustReadTestdata(t, "testdata/golden/"+name+".png")
			if !bytes.Equal(f.pngBytes(t), want) {
				t.Fatal("render is not byte-identical to pinned PNG")
			}
		})
	}
}

func TestParseRawTextScript(t *testing.T) {
	checkTextElement(t, `<script>if(a<b){}</script><p>after</p>`, "script", "if(a<b){}")
}

func TestParseRawTextStyle(t *testing.T) {
	checkTextElement(t, `<style>p>a{color:red}</style><p>after</p>`, "style", "p>a{color:red}")
	// '>' alone already survives the old tokenizer. '<' distinguishes the
	// required raw-text state from ordinary entity-decoded markup.
	checkTextElement(t, `<style>p>a{content:"<b>&amp;"}</style>`, "style", `p>a{content:"<b>&amp;"}`)
}

func TestParseRCDATATitle(t *testing.T) {
	checkTextElement(t, `<title>x<y</title><p>after</p>`, "title", "x<y")
}

func checkTextElement(t *testing.T, src, tag, want string) {
	t.Helper()
	n := findTag(ParseHTML([]byte(src)).Root, tag)
	if n == nil || len(n.Children) != 1 || n.Children[0].Kind != KindText || n.Children[0].Text != want {
		t.Fatalf("%s must contain one text node %q; got %+v", tag, want, n)
	}
}

func TestParseTextStates(t *testing.T) {
	for _, tc := range []struct {
		name, src, tag, want string
	}{
		{"script-entities", `<script>&amp;<b></script>`, "script", "&amp;<b>"},
		{"style-end-case", `<STYLE>x</StYlE ><p>after</p>`, "style", "x"},
		{"title-entities", `<title>&amp;<b>x</b></TITLE>`, "title", "&<b>x</b>"},
		{"textarea", `<textarea>&lt;<b>x</b></TeXtArEa>`, "textarea", "<<b>x</b>"},
		{"end-boundary", `<script>x</scripted>y</script><p>after</p>`, "script", "x</scripted>y"},
		{"end-slash", `<script>x</script/><p>after</p>`, "script", "x"},
		{"end-attributes", `<style>x</style data-x=">"><p>after</p>`, "style", "x"},
		{"unfinished-end", `<script>x</script`, "script", "x</script"},
		{"eof", `<style>p<b>&amp;`, "style", "p<b>&amp;"},
		{"self-closing-nonvoid", `<script/>x<b></script>`, "script", "x<b>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkTextElement(t, tc.src, tc.tag, tc.want)
			doc := ParseHTML([]byte(tc.src))
			if strings.Contains(tc.src, "<p>after") {
				p := findTag(doc.Root, "p")
				if p == nil || TextContent(p) != "after" {
					t.Fatal("matching end tag did not return to markup")
				}
			}
		})
	}
}

func TestParseImpliedDocument(t *testing.T) {
	for _, src := range []string{
		`<title>T</title><p>body`,
		`<head><title>T</title><p>body`,
		`<html lang=en><body id=page><p>body</body></html>`,
		`plain text`,
		``,
		`<html><head></head><body>body</body></html><body id=later>after`,
		`<html><html><head><head><title>T</title></head><body><body>body`,
	} {
		t.Run(src, func(t *testing.T) {
			doc := ParseHTML([]byte(src))
			html, head, body := findTag(doc.Root, "html"), findTag(doc.Root, "head"), findTag(doc.Root, "body")
			if html == nil || head == nil || body == nil {
				t.Fatal("missing implied document elements")
			}
			if countTag(doc.Root, "html") != 1 || countTag(doc.Root, "head") != 1 || countTag(doc.Root, "body") != 1 {
				t.Fatal("duplicate document elements")
			}
			if len(html.Children) < 2 || html.Children[0] != head || !hasChild(html, body) {
				t.Fatal("head/body are not ordered siblings under html")
			}
			if title := findTag(doc.Root, "title"); title != nil && !hasChild(head, title) {
				t.Fatal("metadata escaped head")
			}
			if p := findTag(doc.Root, "p"); p != nil && !hasChild(body, p) {
				t.Fatal("body content escaped body")
			}
			if strings.Contains(src, "lang=en") && (html.Attr("lang") != "en" || body.Attr("id") != "page") {
				t.Fatal("explicit document attributes lost")
			}
		})
	}
}

func TestParseImplicitCloses(t *testing.T) {
	for _, tc := range []struct {
		name, src, parent, first, second string
	}{
		{"li-inline", `<ul><li><b>one<li>two</ul>`, "ul", "li", "li"},
		{"dt-dd-inline", `<dl><dt><b>one<dd>two</dl>`, "dl", "dt", "dd"},
		{"dd-dt", `<dl><dd>one<dt>two</dl>`, "dl", "dd", "dt"},
		{"cells", `<table><tr><td><b>one<th>two</table>`, "tr", "td", "th"},
		{"rows", `<table><tr><td>one<tr><td>two</table>`, "tbody", "tr", "tr"},
		{"table-sections", `<table><thead><tr><th>one<tbody><tr><td>two<tfoot><tr><td>three</table>`, "table", "thead", "tbody"},
		{"tfoot-tbody", `<table><tfoot><tr><td>one<tbody><tr><td>two</table>`, "table", "tfoot", "tbody"},
		{"option", `<select><option><b>one<option>two</select>`, "select", "option", "option"},
		{"optgroup", `<select><optgroup label=a><option>one<optgroup label=b><option>two</select>`, "select", "optgroup", "optgroup"},
		{"p-inline", `<body><p><em>one<div>two</div>`, "body", "p", "div"},
		{"p-p", `<p>one<p>two`, "body", "p", "p"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := findTag(ParseHTML([]byte(tc.src)).Root, tc.parent)
			if parent == nil || len(parent.Children) < 2 || parent.Children[0].Tag != tc.first || parent.Children[1].Tag != tc.second {
				t.Fatalf("expected sibling %s/%s under %s; got %+v", tc.first, tc.second, tc.parent, parent)
			}
		})
	}
	for _, tag := range []string{"address", "article", "aside", "blockquote", "details", "dialog", "div", "dl", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hgroup", "hr", "main", "menu", "nav", "ol", "pre", "search", "section", "summary", "table", "ul"} {
		t.Run("p-"+tag, func(t *testing.T) {
			body := findTag(ParseHTML([]byte("<p><b>one<"+tag+">two")).Root, "body")
			if body == nil || len(body.Children) < 2 || body.Children[0].Tag != "p" || body.Children[1].Tag != tag {
				t.Fatalf("%s did not close p through inline descendants", tag)
			}
		})
	}
}

func TestParseImpliedTableElements(t *testing.T) {
	for _, src := range []string{
		`<table><tr><td>one</table>`,
		`<table><td>one</table>`,
		`<table><tbody><td>one</table>`,
	} {
		doc := ParseHTML([]byte(src))
		table, section, row, cell := findTag(doc.Root, "table"), findTag(doc.Root, "tbody"), findTag(doc.Root, "tr"), findTag(doc.Root, "td")
		if table == nil || section == nil || row == nil || cell == nil || !hasChild(table, section) || !hasChild(section, row) || !hasChild(row, cell) {
			t.Fatalf("missing table > tbody > tr > td for %q", src)
		}
	}
}

func TestParseScopeBoundaries(t *testing.T) {
	for _, src := range []string{
		`<ul><li>outer<ul><li>inner<li>next</ul>end</ul>`,
		`<dl><dd>outer<dl><dt>inner<dd>next</dl>end</dl>`,
		`<table><tr><td>outer<table><tr><td>inner<tr><td>next</table>end</table>`,
	} {
		doc := ParseHTML([]byte(src))
		if !strings.Contains(TextContent(doc.Root), "outer") || !strings.Contains(TextContent(doc.Root), "end") {
			t.Fatalf("scope boundary lost content: %q", src)
		}
		outer := findTag(doc.Root, "li")
		nested := "ul"
		if strings.HasPrefix(src, "<dl>") {
			outer, nested = findTag(doc.Root, "dd"), "dl"
		} else if strings.HasPrefix(src, "<table>") {
			outer, nested = findTag(doc.Root, "td"), "table"
		}
		if outer == nil || !hasChildTag(outer, nested) || !strings.HasSuffix(TextContent(outer), "end") {
			t.Fatalf("nested %s closed its outer container", nested)
		}
	}
}

func TestParseUnsupportedContent(t *testing.T) {
	for _, tag := range []string{"noscript", "frame", "frameset", "object", "embed", "applet"} {
		t.Run(tag, func(t *testing.T) {
			doc := ParseHTML([]byte("<" + tag + ">fallback <b>text</b></" + tag + ">"))
			if got := TextContent(doc.Root); got != "fallback text" {
				t.Fatalf("fallback = %q", got)
			}
			layout := LayoutDocument(doc, 512, nil)
			var text strings.Builder
			for _, item := range layout.Items {
				if item.Kind == ItemText {
					text.WriteString(item.Text)
				}
			}
			if !strings.Contains(text.String(), "fallback") || !strings.Contains(text.String(), "text") {
				t.Fatal("fallback did not reach the painter")
			}
		})
	}
}

func TestParseCorpus(t *testing.T) {
	for _, dir := range []string{"testdata/corpus", "../../../tests/fixtures/web/reference"} {
		t.Run(dir, func(t *testing.T) {
			paths, err := filepath.Glob(filepath.Join(dir, "*.html"))
			if err != nil {
				t.Fatal(err)
			}
			if len(paths) == 0 {
				t.Fatal("empty corpus")
			}
			for _, path := range paths {
				t.Run(filepath.Base(path), func(t *testing.T) {
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					doc := ParseHTML(data)
					checkHTMLInvariants(t, data, doc)
					if doc.Truncated {
						t.Fatal("reference/corpus page unexpectedly truncated")
					}
				})
			}
		})
	}
}

func TestParseStructure(t *testing.T) {
	doc := ParseHTML([]byte(`<html><body><h1>Title</h1><p>Hello <a href="next.html">link</a> world</p></body></html>`))
	if doc.Root == nil {
		t.Fatal("no root")
	}
	h1 := findTag(doc.Root, "h1")
	if h1 == nil || TextContent(h1) != "Title" {
		t.Fatalf("h1 = %v", h1)
	}
	a := findTag(doc.Root, "a")
	if a == nil || a.Attr("href") != "next.html" {
		t.Fatalf("a = %v", a)
	}
	if got := TextContent(doc.Root); !strings.Contains(got, "Hello link world") {
		t.Fatalf("text = %q", got)
	}
}

func TestParseVoidAndSkip(t *testing.T) {
	doc := ParseHTML([]byte(`<p>a<br>b</p><script>var x = 1;</script><style>p{}</style><!-- c -->`))
	if strings.Contains(TextContent(doc.Root), "var x") {
		t.Fatal("script text leaked into content")
	}
	if strings.Contains(TextContent(doc.Root), "p{}") {
		t.Fatal("style text leaked into content")
	}
	if !strings.Contains(TextContent(doc.Root), "a b") {
		t.Fatalf("text = %q", TextContent(doc.Root))
	}
}

func TestParseMalformedNeverDropsText(t *testing.T) {
	inputs := []string{
		"<p>ok <<<< <em>unclosed",
		"plain text with < and > and &",
		"<div><p>a<p>b</div>",
		"<ul><li>one<li>two</ul>",
		"<td>orphan cell",
		"<not a tag at all <b>bold",
		strings.Repeat("<div>", 400) + "deep",
		"<p>trunc",
	}
	for _, in := range inputs {
		doc := ParseHTML([]byte(in))
		got := TextContent(doc.Root)
		if !strings.Contains(got, "ok") && !strings.Contains(got, "plain") && !strings.Contains(got, "a") &&
			!strings.Contains(got, "one") && !strings.Contains(got, "orphan") && !strings.Contains(got, "bold") &&
			!strings.Contains(got, "deep") && !strings.Contains(got, "trunc") {
			t.Fatalf("input %q lost all text: %q", in, got)
		}
	}
}

func TestParseCapsAreVisibleNotFatal(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < MaxNodes+500; i++ {
		sb.WriteString("<p>x</p>")
	}
	doc := ParseHTML([]byte(sb.String()))
	if !doc.Truncated {
		t.Fatal("expected Truncated for oversized input")
	}
	if doc.Nodes > MaxNodes {
		t.Fatalf("node cap exceeded: %d", doc.Nodes)
	}
	checkHTMLInvariants(t, []byte(sb.String()), doc)
}

func TestParseCapBoundaries(t *testing.T) {
	for _, src := range []string{
		strings.Repeat("<div>", MaxDepth+10) + "<script><b>hidden</script>visible",
		"<p title='" + strings.Repeat("x", MaxAttrLen+1) + "'>x",
		"<p " + strings.Repeat("x=1 ", MaxAttrCount+1) + ">x",
		strings.Repeat("<br>", MaxNodes-4) + "<script>hidden</script>after",
		strings.Repeat("<", 10000),
	} {
		doc := ParseHTML([]byte(src))
		checkHTMLInvariants(t, []byte(src), doc)
		if strings.Contains(src, "hidden") && strings.Contains(TextContent(doc.Root), "hidden") {
			t.Fatal("depth/node cap leaked script text")
		}
		if !strings.HasPrefix(src, "<<") && !doc.Truncated {
			t.Fatal("cap overflow not reported")
		}
	}
}

func TestParseTextContentBoundIncludesBreaks(t *testing.T) {
	for _, src := range []string{"<br>", "<br><br>", "<p>one<br>two</p>"} {
		checkHTMLInvariants(t, []byte(src), ParseHTML([]byte(src)))
	}
}

func TestDecodeEntities(t *testing.T) {
	got := DecodeEntities("a&amp;b &lt;tag&gt; &quot;q&quot; &#39;s&#39; &#x41; &nbsp; &unknown;")
	want := "a&b <tag> \"q\" 's' A \u00a0 &unknown;"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func FuzzHTML(f *testing.F) {
	seeds := []string{
		`<html><body><p>hi</p></body></html>`,
		`<<<>>>&&&;;;`,
		`<a href="x">y</a>`,
		`<table><tr><td>1</td></tr></table>`,
		`<pre>  spaced  </pre>`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		doc := ParseHTML(data)
		checkHTMLInvariants(t, data, doc)
	})
}

func checkHTMLInvariants(t *testing.T, src []byte, doc *Document) {
	t.Helper()
	if doc == nil || doc.Root == nil {
		t.Fatal("nil document")
	}
	if doc.Nodes > MaxNodes {
		t.Fatalf("node cap exceeded: %d", doc.Nodes)
	}
	// The public tree has child edges only. Build an independent parent map
	// to check ownership, cycles and every parent chain, not just stack depth.
	parents := map[*Node]*Node{doc.Root: nil}
	nodes, textBytes, breaks := 0, 0, 0
	var walk func(*Node, int)
	walk = func(n *Node, depth int) {
		if depth > MaxDepth {
			t.Fatalf("depth cap exceeded: %d", depth)
		}
		if len(n.Attrs) > MaxAttrCount {
			t.Fatalf("attribute count cap exceeded: %d", len(n.Attrs))
		}
		for _, a := range n.Attrs {
			if len(a.Value) > MaxAttrLen {
				t.Fatalf("attribute length cap exceeded: %d", len(a.Value))
			}
		}
		if n.Kind == KindText {
			textBytes += len(n.Text)
			if len(n.Children) != 0 {
				t.Fatal("text node has children")
			}
		}
		if n.Kind == KindElement && n.Tag == "br" {
			breaks++
		}
		for _, c := range n.Children {
			if c == nil {
				t.Fatal("nil child")
			}
			if _, seen := parents[c]; seen {
				t.Fatal("node has multiple parents or a cycle")
			}
			if textElement(n.Tag) && c.Kind != KindText {
				t.Fatalf("%s contains an element child", n.Tag)
			}
			parents[c] = n
			nodes++
			walk(c, depth+1)
		}
	}
	walk(doc.Root, 0)
	for n := range parents {
		depth := 0
		for parent := parents[n]; parent != nil; parent = parents[parent] {
			depth++
			if depth > MaxDepth {
				t.Fatal("parent chain does not terminate within depth cap")
			}
		}
	}
	if nodes != doc.Nodes || textBytes != doc.TextBytes {
		t.Fatalf("accounting: nodes=%d/%d text=%d/%d", nodes, doc.Nodes, textBytes, doc.TextBytes)
	}
	// rawText emits a newline per br without keeping it in TextBytes.
	// Include that independently counted allowance; the total is still
	// bounded by the source bytes, not an arbitrary fuzz-only ceiling.
	readable := len(TextContent(doc.Root))
	if textBytes+breaks > len(src) || readable > textBytes+breaks {
		t.Fatalf("text bound exceeded: input=%d kept=%d breaks=%d readable=%d", len(src), textBytes, breaks, readable)
	}
}

func textElement(tag string) bool {
	return tag == "script" || tag == "style" || tag == "title" || tag == "textarea"
}

func hasChild(n, child *Node) bool {
	for _, c := range n.Children {
		if c == child {
			return true
		}
	}
	return false
}

func hasChildTag(n *Node, tag string) bool {
	for _, c := range n.Children {
		if c.Tag == tag {
			return true
		}
	}
	return false
}

func countTag(n *Node, tag string) int {
	count := 0
	if n.Tag == tag {
		count++
	}
	for _, c := range n.Children {
		count += countTag(c, tag)
	}
	return count
}

func findTag(n *Node, tag string) *Node {
	if n.Kind == KindElement && n.Tag == tag {
		return n
	}
	for _, c := range n.Children {
		if got := findTag(c, tag); got != nil {
			return got
		}
	}
	return nil
}
