package css

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"virelai/webrender"
)

// This independent deletion oracle covers the deliberately unsupported
// constructs in the authored reference corpus. It does not call the CSS parser
// to decide which bytes to delete.
func deleteExcluded(src string) string {
	for {
		at := strings.Index(src, "@")
		if at < 0 {
			break
		}
		end, depth := at, 0
		for ; end < len(src); end++ {
			switch src[end] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end++
					goto removed
				}
			case ';':
				if depth == 0 {
					end++
					goto removed
				}
			}
		}
	removed:
		src = src[:at] + src[end:]
	}
	src = regexp.MustCompile(`[^{}]*[:>\[+~][^{}]*\{[^{}]*\}`).ReplaceAllString(src, "")
	src = regexp.MustCompile(`(?i)(float|position|top|z-index|grid-template-columns|transform|animation|background-image|opacity|content)\s*:[^;{}]*;`).ReplaceAllString(src, "")
	return src
}

func assertEquivalent(t *testing.T, html, source string) {
	t.Helper()
	doc := webrender.ParseHTML([]byte(html))
	mixed, _ := Parse([]byte(source))
	clean, _ := Parse([]byte(deleteExcluded(source)))
	a, _ := Cascade(doc, []*Stylesheet{mixed})
	b, _ := Cascade(doc, []*Stylesheet{clean})
	var walk func(*webrender.Node)
	walk = func(n *webrender.Node) {
		if a.ForNode(n) != b.ForNode(n) {
			t.Errorf("unsupported CSS changed node %s: mixed=%+v deleted=%+v", n.Tag, a.ForNode(n), b.ForNode(n))
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(doc.Root)
}

func TestUnsupportedEquivalence(t *testing.T) {
	assertEquivalent(t, `<main><p class="x">Visible</p></main>`, `
		p { color:green; float:right; position:absolute; grid-template-columns:1fr; }
		p:hover { display:none; } p::before { content:"hidden"; }
		@media print { p { display:none; } }
		@import "never.css"; @font-face { font-family:Remote; src:url(never.ttf); }
	`)
}

func TestReferenceUnsupportedEquivalence(t *testing.T) {
	files, err := filepath.Glob("../../../tests/fixtures/web/reference/*.html")
	if err != nil || len(files) != 15 {
		t.Fatalf("reference inventory: %d, %v", len(files), err)
	}
	styleRE := regexp.MustCompile(`(?s)<style>(.*?)</style>`)
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			buf, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			html := string(buf)
			var sources []string
			for _, match := range styleRE.FindAllStringSubmatch(html, -1) {
				sources = append(sources, match[1])
			}
			// Extract before parsing: the pre-M93b HTML parser has no raw-text
			// state. CSS equivalence is independent of that other lane.
			assertEquivalent(t, styleRE.ReplaceAllString(html, ""), strings.Join(sources, "\n"))
		})
	}
}
