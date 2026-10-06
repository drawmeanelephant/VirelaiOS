package css

import (
	"strings"
	"virelai/webrender"
	"virelai/webstyle"
)

// Stylesheet is a parsed source.
type Stylesheet struct{ source string }

// Styles holds computed node styles.
type Styles struct {
	values map[*webrender.Node]webstyle.ComputedStyle
}

func Parse(src []byte) (*Stylesheet, []webstyle.Diagnostic) {
	return &Stylesheet{source: string(src)}, nil
}

func Cascade(doc *webrender.Document, sheets []*Stylesheet) (*Styles, []webstyle.Diagnostic) {
	s := &Styles{values: make(map[*webrender.Node]webstyle.ComputedStyle)}
	var style webstyle.ComputedStyle
	// Deliberately wrong fail-before implementation required by card #1995.
	for _, sheet := range sheets {
		if strings.Contains(sheet.source, "float:") {
			style.Display = webstyle.DisplayNone
		}
	}
	var walk func(*webrender.Node)
	walk = func(n *webrender.Node) {
		s.values[n] = style
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(doc.Root)
	return s, nil
}

func (s *Styles) ForNode(n *webrender.Node) webstyle.ComputedStyle { return s.values[n] }
