package webrender

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"virelai/webstyle"
)

func px(n int) webstyle.Length { return webstyle.Length{Kind: webstyle.LengthPx, Value: int32(n)} }
func percent(n int) webstyle.Length {
	return webstyle.Length{Kind: webstyle.LengthPercent, Value: int32(n)}
}
func edges(n int) webstyle.Edges {
	return webstyle.Edges{Top: px(n), Right: px(n), Bottom: px(n), Left: px(n)}
}
func solid(n int) webstyle.Border {
	return webstyle.Border{Width: px(n), Style: webstyle.BorderSolid}
}

func blockStyle(n *Node) webstyle.ComputedStyle {
	s := webstyle.ComputedStyle{Display: webstyle.DisplayBlock}
	if n.Kind == KindText || n.Tag == "span" || n.Tag == "a" || n.Tag == "br" {
		s.Display = webstyle.DisplayInline
	}
	if n.Tag == "head" {
		s.Display = webstyle.DisplayNone
	}
	return s
}

func exactLayout(t *testing.T, src string, style func(*Node) webstyle.ComputedStyle) (*BoxTree, *Layout) {
	t.Helper()
	tree, ds := BuildBoxTree(ParseHTML([]byte(src)), style)
	if len(ds) != 0 {
		t.Fatalf("build diagnostics: %+v", ds)
	}
	l, ds := LayoutBoxes(tree, webstyle.Viewport{Width: 1280, Height: 720}, metricText{})
	if len(ds) != 0 {
		t.Fatalf("layout diagnostics: %+v", ds)
	}
	if l.BoxTree != tree || l.Width != 1280 {
		t.Fatal("layout lost its tree or CSS viewport")
	}
	return tree, l
}

// This injected metric honors CSS font/line sizes before M93e adds that
// capability to the production text engines.
type metricText struct{ Bitmap }

func (metricText) Measure(text string, s Style) int {
	if s.FontPx > 0 {
		return len([]rune(text)) * s.FontPx
	}
	return Bitmap{}.Measure(text, s)
}
func (metricText) LineHeight(s Style) int {
	if s.LineHeightPx > 0 {
		return s.LineHeightPx
	}
	return Bitmap{}.LineHeight(s)
}

func boxID(t *testing.T, tree *BoxTree, id string) *Box {
	t.Helper()
	for _, b := range tree.Boxes {
		if b.Node != nil && b.Node.Attr("id") == id && !b.Anonymous {
			return b
		}
	}
	t.Fatalf("missing box %q", id)
	return nil
}

func TestBoxTreeStructure(t *testing.T) {
	tree, ds := BuildBoxTree(ParseHTML([]byte(`<div id="p">one<span>two<div id="b">block</div>three</span>four<div id="gone">hidden</div></div>`)), func(n *Node) webstyle.ComputedStyle {
		s := blockStyle(n)
		if n.Attr("id") == "gone" {
			s.Display = webstyle.DisplayNone
		}
		return s
	})
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	p := boxID(t, tree, "p")
	if len(p.Children) != 3 || !p.Children[0].Anonymous || p.Children[1].Node.Attr("id") != "b" || !p.Children[2].Anonymous {
		t.Fatalf("inline/block split: %+v", p.Children)
	}
	for _, b := range tree.Boxes {
		if b.Node != nil && b.Node.Attr("id") == "gone" {
			t.Fatal("display:none subtree retained")
		}
	}
	if tree.Root == nil || len(tree.Boxes) < 8 {
		t.Fatal("anonymous boxes not counted")
	}
}

func TestBoxCompatibilityAndStyleCopies(t *testing.T) {
	doc := ParseHTML([]byte(`<p>text <strong>bold</strong></p><noscript>fallback</noscript><script>never</script>`))
	l := LayoutDocument(doc, 470, metricText{})
	if l.BoxTree == nil || l.Width != 470 || !strings.Contains(layoutText(l), "fallback") || strings.Contains(layoutText(l), "never") {
		t.Fatal(l)
	}
	for _, it := range l.Items {
		if it.Kind == ItemText && (it.FontPx != 0 || it.LineHeightPx != 0) {
			t.Fatal("legacy size seam changed", it)
		}
	}
	if !strings.Contains(TextContent(doc.Root), "fallback") {
		t.Fatal("fallback vanished in text extraction")
	}
	st := webstyle.ComputedStyle{Display: webstyle.DisplayBlock, Width: px(25)}
	tree, ds := BuildBoxTree(ParseHTML([]byte(`<div id="a"></div>`)), func(*Node) webstyle.ComputedStyle { return st })
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	st.Width = px(100)
	if boxID(t, tree, "a").Style.Width != px(25) {
		t.Fatal("box style is not an owned copy")
	}
}

func TestReferenceBoxRects(t *testing.T) {
	for _, name := range []string{"block", "collapse", "flex"} {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile("../../../tests/fixtures/web/reference/boxes-" + name + ".html")
			if err != nil {
				t.Fatal(err)
			}
			tree, _ := exactLayout(t, string(src), referenceStyle(name))
			f, err := os.Open(filepath.Join("testdata", "boxes", name+".boxes"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				line := sc.Text()
				if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
					continue
				}
				var id, area string
				var want BoxRect
				if _, err := fmt.Sscanf(line, "%s %s %d %d %d %d", &id, &area, &want.X, &want.Y, &want.W, &want.H); err != nil {
					t.Fatal(err)
				}
				b := boxID(t, tree, id)
				got := map[string]BoxRect{"content": b.Content, "padding": b.Padding, "border": b.Border, "margin": b.Margin}[area]
				if got != want {
					t.Errorf("%s %s = %+v, want %+v", id, area, got, want)
				}
			}
			if err := sc.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Independent, hand-constructed styles match the declarations in the three
// arithmetic pages. Layout never imports the CSS parser, even in its tests.
func referenceStyle(page string) func(*Node) webstyle.ComputedStyle {
	return func(n *Node) webstyle.ComputedStyle {
		s := blockStyle(n)
		id, class := n.Attr("id"), n.Attr("class")
		switch page {
		case "block":
			switch id {
			case "outer":
				s.Width, s.Height, s.Margin, s.Padding = px(200), px(40), edges(10), edges(8)
				s.Border = webstyle.Borders{Top: solid(2), Right: solid(2), Bottom: solid(2), Left: solid(2)}
			case "next":
				s.Width, s.Height, s.Margin.Top = px(100), px(20), px(6)
			}
		case "collapse":
			if n.Tag == "body" {
				s.Padding = edges(10)
			}
			if n.Tag == "div" {
				s.Width = px(100)
			}
			switch id {
			case "a":
				s.Height, s.Margin.Bottom = px(20), px(20)
			case "b":
				s.Height, s.Margin.Top, s.Margin.Bottom = px(10), px(30), px(10)
			case "c":
				s.Height, s.Margin.Top = px(10), px(-5)
			case "parent":
				s.Margin.Top = px(20)
			case "child":
				s.Height, s.Margin.Top = px(20), px(30)
			}
		case "flex":
			switch class {
			case "row":
				s.Display, s.Width, s.Height = webstyle.DisplayFlex, px(300), px(40)
				s.RowGap, s.ColumnGap = px(10), px(10)
			case "shrink":
				s.Display, s.Width, s.Height = webstyle.DisplayFlex, px(180), px(20)
			case "wrap":
				s.Display, s.Width, s.Height = webstyle.DisplayFlex, px(130), px(50)
				s.FlexWrap, s.AlignItems = webstyle.FlexWrapLines, webstyle.ItemsStart
				s.RowGap, s.ColumnGap = px(10), px(10)
			case "column":
				s.Display, s.Width, s.Height = webstyle.DisplayFlex, px(40), px(100)
				s.FlexDirection, s.RowGap, s.ColumnGap = webstyle.FlexColumn, px(10), px(10)
			}
			if len(id) == 2 {
				switch id[0] {
				case 'r':
					s.FlexBasis = px(50)
					if id == "r1" {
						s.FlexGrow = webstyle.FlexFactor{Set: true, Value: 1}
					}
					if id == "r2" {
						s.FlexGrow = webstyle.FlexFactor{Set: true, Value: 2}
					}
				case 's':
					s.FlexBasis = px(60)
					if id == "s1" {
						s.FlexBasis = px(120)
					}
				case 'w':
					s.FlexBasis, s.Height = px(60), px(20)
				case 'c':
					s.FlexBasis, s.FlexGrow = px(20), webstyle.FlexFactor{Set: true, Value: 1}
				}
			}
		}
		return s
	}
}

func TestBoxWidthConstraints(t *testing.T) {
	tree, _ := exactLayout(t, `<div id="a"></div><div id="b"></div>`, func(n *Node) webstyle.ComputedStyle {
		s := blockStyle(n)
		switch n.Attr("id") {
		case "a":
			s.Width, s.Height = percent(50), px(20)
			s.Margin.Left, s.Margin.Right = webstyle.Length{Kind: webstyle.LengthAuto}, webstyle.Length{Kind: webstyle.LengthAuto}
			s.Padding = edges(10)
		case "b":
			s.Width, s.MinWidth, s.MaxWidth, s.Height = px(10), px(100), px(40), px(10)
		}
		return s
	})
	if got := boxID(t, tree, "a").Content; got != (BoxRect{X: 320, Y: 10, W: 640, H: 20}) {
		t.Fatal(got)
	}
	if got := boxID(t, tree, "b").Content; got != (BoxRect{Y: 40, W: 100, H: 10}) {
		t.Fatal(got)
	}
}

func TestBoxParentBottomAndEmptyCollapse(t *testing.T) {
	tree, _ := exactLayout(t, `<div id="p"><div id="a"></div><div id="e"></div></div><div id="b"></div>`, func(n *Node) webstyle.ComputedStyle {
		s := blockStyle(n)
		s.Width = px(100)
		switch n.Attr("id") {
		case "p":
			s.Margin.Top, s.Margin.Bottom = px(5), px(12)
		case "a":
			s.Height, s.Margin.Top, s.Margin.Bottom = px(20), px(10), px(30)
		case "e":
			s.Margin.Top, s.Margin.Bottom = px(-4), px(25)
		case "b":
			s.Height, s.Margin.Top = px(10), px(20)
		}
		return s
	})
	if got := boxID(t, tree, "p").Content; got != (BoxRect{Y: 10, W: 100, H: 20}) {
		t.Fatal(got)
	}
	if got := boxID(t, tree, "b").Content; got != (BoxRect{Y: 56, W: 100, H: 10}) {
		t.Fatal(got)
	}
}

func TestFlexFreezeAndNested(t *testing.T) {
	tree, _ := exactLayout(t, `<div id="row"><div id="a"></div><div id="b"><div id="inner"></div></div></div>`, func(n *Node) webstyle.ComputedStyle {
		s := blockStyle(n)
		switch n.Attr("id") {
		case "row":
			s.Display, s.Width, s.Height = webstyle.DisplayFlex, px(200), px(40)
		case "a":
			s.FlexBasis, s.MinWidth = px(160), px(140)
		case "b":
			s.Display, s.FlexBasis, s.Height = webstyle.DisplayFlex, px(160), px(40)
		case "inner":
			s.FlexGrow = webstyle.FlexFactor{Set: true, Value: 1}
		}
		return s
	})
	for id, want := range map[string]BoxRect{"a": {W: 140, H: 40}, "b": {X: 140, W: 60, H: 40}, "inner": {X: 140, W: 60, H: 40}} {
		if got := boxID(t, tree, id).Content; got != want {
			t.Errorf("%s: %+v != %+v", id, got, want)
		}
	}
}

func TestFlexJustifyAlignAndAutoMargins(t *testing.T) {
	for _, justify := range []webstyle.JustifyContent{webstyle.JustifyStart, webstyle.JustifyEnd, webstyle.JustifyCenter, webstyle.JustifySpaceBetween, webstyle.JustifySpaceAround, webstyle.JustifySpaceEvenly} {
		t.Run(fmt.Sprint(justify), func(t *testing.T) {
			tree, _ := exactLayout(t, `<div id="row"><div id="a"></div><div id="b"></div></div>`, func(n *Node) webstyle.ComputedStyle {
				s := blockStyle(n)
				if n.Attr("id") == "row" {
					s.Display, s.Width, s.Height, s.JustifyContent, s.AlignItems = webstyle.DisplayFlex, px(101), px(40), justify, webstyle.ItemsCenter
				} else if n.Attr("id") != "" {
					s.Width, s.Height = px(20), px(10)
				}
				return s
			})
			want := [][2]int{{0, 20}, {61, 81}, {30, 50}, {0, 81}, {15, 65}, {20, 60}}[justify]
			for i, id := range []string{"a", "b"} {
				if got := boxID(t, tree, id).Content; got != (BoxRect{X: want[i], Y: 15, W: 20, H: 10}) {
					t.Fatal(got, want)
				}
			}
		})
	}
	tree, _ := exactLayout(t, `<div id="row"><div id="a"></div><div id="b"></div></div>`, func(n *Node) webstyle.ComputedStyle {
		s := blockStyle(n)
		if n.Attr("id") == "row" {
			s.Display, s.Width, s.Height = webstyle.DisplayFlex, px(101), px(40)
		} else if n.Attr("id") != "" {
			s.Width, s.Height = px(20), px(10)
			s.Margin.Left = webstyle.Length{Kind: webstyle.LengthAuto}
		}
		return s
	})
	if got := boxID(t, tree, "a").Content; got != (BoxRect{X: 31, W: 20, H: 10}) {
		t.Fatal(got)
	}
	if got := boxID(t, tree, "b").Content; got != (BoxRect{X: 81, W: 20, H: 10}) {
		t.Fatal(got)
	}
}

func TestFlexCrossStretchRelayoutAndDeepAutoBasis(t *testing.T) {
	tree, _ := exactLayout(t, `<div id="row"><div id="col"><div id="a"></div><div id="b"></div></div></div>`, func(n *Node) webstyle.ComputedStyle {
		s := blockStyle(n)
		switch n.Attr("id") {
		case "row":
			s.Display, s.Width, s.Height = webstyle.DisplayFlex, px(80), px(100)
		case "col":
			s.Display, s.FlexDirection, s.FlexGrow = webstyle.DisplayFlex, webstyle.FlexColumn, webstyle.FlexFactor{Set: true, Value: 1}
		case "a", "b":
			s.FlexBasis, s.FlexGrow = px(10), webstyle.FlexFactor{Set: true, Value: 1}
		}
		return s
	})
	if got := boxID(t, tree, "a").Content; got != (BoxRect{W: 80, H: 50}) {
		t.Fatal(got)
	}
	if got := boxID(t, tree, "b").Content; got != (BoxRect{Y: 50, W: 80, H: 50}) {
		t.Fatal(got)
	}
	src := strings.Repeat("<div>", 32) + "text" + strings.Repeat("</div>", 32)
	_, l := exactLayout(t, src, func(n *Node) webstyle.ComputedStyle {
		s := blockStyle(n)
		if n.Tag == "div" {
			s.Display, s.FlexDirection = webstyle.DisplayFlex, webstyle.FlexColumn
		}
		return s
	})
	if !strings.Contains(layoutText(l), "text") {
		t.Fatal("deep flex lost text")
	}
}

func TestBoxCollapseBarriersAndPercentEdges(t *testing.T) {
	tree, _ := exactLayout(t, `<div id="p"><div id="a"></div></div><div id="next"></div>`, func(n *Node) webstyle.ComputedStyle {
		s := blockStyle(n)
		switch n.Attr("id") {
		case "p":
			s.Width, s.Padding.Top, s.Border.Bottom = px(100), percent(1), solid(2)
		case "a":
			s.Height, s.Margin.Top, s.Margin.Bottom = px(20), px(30), px(10)
		case "next":
			s.Height, s.Margin.Top = px(10), px(5)
		}
		return s
	})
	// Vertical padding percent resolves against 1280: floor(12.8)=12.
	// Padding prevents first-child collapse; bottom border contains the 10px.
	if got := boxID(t, tree, "a").Content; got != (BoxRect{Y: 42, W: 100, H: 20}) {
		t.Fatal(got)
	}
	if got := boxID(t, tree, "next").Content.Y; got != 79 {
		t.Fatal(got)
	}
}

func TestBoxInlineReplacedAndListMarkers(t *testing.T) {
	_, l := exactLayout(t, `<div><a href="next">a<img alt="x" width="40" height="20">b</a><input value="name"></div><ol><li>first</li><li>second</li></ol>`, func(n *Node) webstyle.ComputedStyle {
		s := blockStyle(n)
		if n.Tag == "img" || n.Tag == "input" {
			s.Display = webstyle.DisplayInline
		}
		if n.Tag == "li" {
			s.Display = webstyle.DisplayListItem
		}
		return s
	})
	images := 0
	for _, it := range l.Items {
		if it.Kind == ItemImage {
			images++
			if it.W != 40 || it.H != 20 {
				t.Fatal(it)
			}
		}
	}
	if images != 1 || !strings.Contains(layoutText(l), "name") || !strings.Contains(layoutText(l), "1.") || !strings.Contains(layoutText(l), "2.") {
		t.Fatal(l.Items)
	}
	if len(l.Links) < 3 {
		t.Fatal("image link hit region missing", l.Links)
	}
}

func TestBoxTableCaptionAndEqualColumns(t *testing.T) {
	tree, _ := exactLayout(t, `<table id="table"><tbody><tr id="row"><td id="a">a</td><td id="b">b</td></tr></tbody><caption id="cap">caption</caption></table>`, func(n *Node) webstyle.ComputedStyle {
		s := blockStyle(n)
		switch n.Tag {
		case "table":
			s.Display, s.Width = webstyle.DisplayTable, px(101)
		case "tbody":
			s.Display = webstyle.DisplayTableRowGroup
		case "tr":
			s.Display = webstyle.DisplayTableRow
		case "td":
			s.Display = webstyle.DisplayTableCell
		case "caption":
			s.Display = webstyle.DisplayTableCaption
		}
		return s
	})
	if got := boxID(t, tree, "cap").Content; got != (BoxRect{W: 101, H: 18}) {
		t.Fatal(got)
	}
	if got := boxID(t, tree, "a").Content; got != (BoxRect{Y: 18, W: 51, H: 18}) {
		t.Fatal(got)
	}
	if got := boxID(t, tree, "b").Content; got != (BoxRect{X: 51, Y: 18, W: 50, H: 18}) {
		t.Fatal(got)
	}
}

func TestBoxInlineFormatting(t *testing.T) {
	for _, mode := range []webstyle.WhiteSpace{webstyle.WhiteSpaceNormal, webstyle.WhiteSpacePre, webstyle.WhiteSpacePreWrap} {
		tree, l := exactLayout(t, "<div id=\"p\">ab   cd\nxy</div>", func(n *Node) webstyle.ComputedStyle {
			s := blockStyle(n)
			s.WhiteSpace, s.FontSize, s.LineHeight = mode, px(10), px(16)
			if n.Attr("id") == "p" {
				s.Width = px(50)
			}
			return s
		})
		wantH := []int{32, 32, 48}[mode]
		if h := boxID(t, tree, "p").Content.H; h != wantH {
			t.Errorf("mode %d: height %d want %d", mode, h, wantH)
		}
		for _, it := range l.Items {
			if it.Kind == ItemText && (it.FontPx != 10 || it.LineHeightPx != 16) {
				t.Fatal(it)
			}
		}
	}
}

func TestBoxInlineEdgesAndAlignment(t *testing.T) {
	for _, align := range []webstyle.TextAlign{webstyle.AlignLeft, webstyle.AlignCenter, webstyle.AlignRight} {
		tree, l := exactLayout(t, `<div id="p"><span id="s">ab</span>cd</div>`, func(n *Node) webstyle.ComputedStyle {
			s := blockStyle(n)
			s.FontSize, s.LineHeight, s.TextAlign = px(10), px(16), align
			if n.Attr("id") == "p" {
				s.Width = px(100)
			}
			if n.Attr("id") == "s" {
				s.Width, s.Height = px(5), px(99) // dimensions do not size inline boxes
				s.Padding.Left, s.Padding.Right = px(3), px(4)
				s.Border.Left, s.Border.Right = solid(1), solid(2)
				s.Margin.Left, s.Margin.Right = px(2), px(1)
			}
			return s
		})
		shift := []int{0, 23, 47}[align] // line width 20+20+13=53
		if got := boxID(t, tree, "s").Border; got != (BoxRect{X: 2 + shift, W: 30, H: 16}) {
			t.Fatal(got)
		}
		if len(l.Items) != 2 || l.Items[0].X != 6+shift || l.Items[1].X != 33+shift {
			t.Fatal(l.Items)
		}
	}
}

func TestFlexConstraintsAndNoMarginCollapse(t *testing.T) {
	tree, _ := exactLayout(t, `<div id="r"><div id="a"></div><div id="b"></div><div id="c"></div></div>`, func(n *Node) webstyle.ComputedStyle {
		s := blockStyle(n)
		switch n.Attr("id") {
		case "r":
			s.Display, s.Width, s.Height, s.FlexDirection = webstyle.DisplayFlex, px(40), px(120), webstyle.FlexColumn
		case "a", "b", "c":
			s.FlexBasis, s.FlexGrow = px(10), webstyle.FlexFactor{Set: true, Value: 1}
			s.Margin.Top, s.Margin.Bottom = px(5), px(5)
			if n.Attr("id") == "a" {
				s.MaxHeight = px(20)
			}
		}
		return s
	})
	// 90px available after six 5px margins. a freezes at 20; b/c get 35.
	for id, want := range map[string]BoxRect{"a": {Y: 5, W: 40, H: 20}, "b": {Y: 35, W: 40, H: 35}, "c": {Y: 80, W: 40, H: 35}} {
		if got := boxID(t, tree, id).Content; got != want {
			t.Fatal(id, got, want)
		}
	}
	for _, height := range []webstyle.Length{{}, px(100)} {
		tree, _ := exactLayout(t, `<div id="r"><div id="a"></div></div>`, func(n *Node) webstyle.ComputedStyle {
			s := blockStyle(n)
			if n.Attr("id") == "r" {
				s.Display, s.Width, s.Height = webstyle.DisplayFlex, px(40), height
				s.MinHeight, s.MaxHeight = px(50), px(50)
			} else if n.Attr("id") == "a" {
				s.FlexGrow = webstyle.FlexFactor{Set: true, Value: 1}
			}
			return s
		})
		if got := boxID(t, tree, "a").Content; got != (BoxRect{W: 40, H: 50}) {
			t.Fatal(got)
		}
	}
}

func TestBoxInvalidAndLimits(t *testing.T) {
	if _, ds := BuildBoxTree(nil, blockStyle); len(ds) == 0 || !strings.HasPrefix(ds[0].Text, "invalid-input") {
		t.Fatal(ds)
	}
	if _, ds := BuildBoxTree(ParseHTML(nil), nil); len(ds) == 0 {
		t.Fatal("nil style accepted")
	}
	if _, ds := LayoutBoxes(nil, webstyle.Viewport{}, nil); len(ds) == 0 {
		t.Fatal("nil tree accepted")
	}
	root := &Node{Tag: "document"}
	for i := 0; i < webstyle.MaxBoxes+10; i++ {
		root.Children = append(root.Children, &Node{Tag: "div"})
	}
	tree, ds := BuildBoxTree(&Document{Root: root}, blockStyle)
	if !tree.Truncated || len(tree.Boxes) > webstyle.MaxBoxes || len(ds) == 0 {
		t.Fatal(len(tree.Boxes), ds)
	}
	l, ds := LayoutBoxes(tree, webstyle.Viewport{Width: 1280, Height: 720}, nil)
	if !l.Truncated || len(ds) == 0 || !strings.Contains(layoutText(l), "truncated") {
		t.Fatal("invisible limit", ds)
	}
}

func TestBoxCoordinatePaintAndDepthLimits(t *testing.T) {
	for _, kind := range []string{"depth", "coordinate", "paint"} {
		t.Run(kind, func(t *testing.T) {
			root := &Node{Tag: "document"}
			style := blockStyle
			switch kind {
			case "depth":
				n := root
				for i := 0; i < webstyle.MaxDepth+10; i++ {
					c := &Node{Tag: "div"}
					n.Children = []*Node{c}
					n = c
				}
			case "coordinate":
				for i := 0; i < 40; i++ {
					root.Children = append(root.Children, &Node{Tag: "div"})
				}
				style = func(n *Node) webstyle.ComputedStyle {
					s := blockStyle(n)
					if n.Tag == "div" {
						s.Height = px(32768)
					}
					return s
				}
			case "paint":
				root.Children = []*Node{{Tag: "a", Attrs: []Attr{{Name: "href", Value: "next"}}, Children: []*Node{{Kind: KindText, Text: strings.Repeat("x ", webstyle.MaxPaintItems)}}}}
			}
			tree, buildDS := BuildBoxTree(&Document{Root: root}, style)
			l, ds := LayoutBoxes(tree, webstyle.Viewport{Width: 1280, Height: 720}, metricText{})
			if !l.Truncated || len(l.Items) > webstyle.MaxPaintItems || len(buildDS)+len(ds) == 0 || !strings.Contains(layoutText(l), "truncated") {
				t.Fatal("invisible/unbounded", len(l.Items), ds)
			}
		})
	}
}

func BenchmarkLargestReferenceLayout(b *testing.B) {
	files, err := filepath.Glob("../../../tests/fixtures/web/reference/*.html")
	if err != nil {
		b.Fatal(err)
	}
	var largest []byte
	var name string
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			b.Fatal(err)
		}
		if len(src) > len(largest) {
			largest, name = src, f
		}
	}
	doc := ParseHTML(largest)
	style := compatibilityStyles(doc)
	if strings.HasSuffix(name, "boxes-flex.html") {
		style = referenceStyle("flex")
	}
	tree, ds := BuildBoxTree(doc, style)
	if len(ds) != 0 {
		b.Fatal(ds)
	}
	b.Logf("largest reference: %s", name)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l, ds := LayoutBoxes(tree, webstyle.Viewport{Width: 1280, Height: 720}, nil)
		if len(ds) != 0 || l.Truncated {
			b.Fatal(ds)
		}
	}
	b.ReportMetric(float64(len(largest)), "source-bytes")
	b.ReportMetric(float64(len(tree.Boxes)), "boxes")
}

func BenchmarkLargestReferenceLayoutPaint(b *testing.B) {
	src, err := os.ReadFile("../../../tests/fixtures/web/reference/boxes-flex.html")
	if err != nil {
		b.Fatal(err)
	}
	tree, ds := BuildBoxTree(ParseHTML(src), referenceStyle("flex"))
	if len(ds) != 0 {
		b.Fatal(ds)
	}
	frame := newFB(1280, 720, ColorPageBg)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l, ds := LayoutBoxes(tree, webstyle.Viewport{Width: 1280, Height: 720}, metricText{})
		if len(ds) != 0 {
			b.Fatal(ds)
		}
		Paint(l, frame, 0, 0, 1280, 720, 0)
	}
	b.ReportMetric(float64(len(tree.Boxes)), "boxes")
}
