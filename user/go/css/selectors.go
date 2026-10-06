package css

import (
	"strings"
	"virelai/webrender"
	"virelai/webstyle"
)

type specificity struct{ inline, id, class, tag int }

func (a specificity) less(b specificity) bool {
	if a.inline != b.inline {
		return a.inline < b.inline
	}
	if a.id != b.id {
		return a.id < b.id
	}
	if a.class != b.class {
		return a.class < b.class
	}
	return a.tag < b.tag
}

type compound struct {
	tag     string
	ids     []string
	classes []string
}
type selector struct {
	parts []compound
	spec  specificity
}

func (p *parser) selectors(ts []token, start token) []selector {
	var out []selector
	for len(ts) > 0 {
		i := 0
		for i < len(ts) && !ts[i].punct(",") {
			i++
		}
		group := trim(ts[:i])
		if len(out) >= webstyle.MaxSelectorsPerRule {
			p.warn(webstyle.DiagnosticLimit, start, "css-selector-limit")
			return nil
		}
		var s selector
		for len(group) > 0 {
			if len(s.parts) >= webstyle.MaxSelectorParts {
				p.warn(webstyle.DiagnosticLimit, start, "css-selector-limit descendant parts")
				return nil
			}
			var c compound
			j := 0
			if group[0].kind == tIdent {
				c.tag = lower(group[0].text)
				s.spec.tag++
				j++
			} else if group[0].punct("*") {
				j++
			}
			for j < len(group) && group[j].kind != tSpace {
				switch {
				case group[j].kind == tHash:
					c.ids = append(c.ids, group[j].text)
					s.spec.id++
					j++
				case group[j].punct(".") && j+1 < len(group) && group[j+1].kind == tIdent:
					c.classes = append(c.classes, group[j+1].text)
					s.spec.class++
					j += 2
				default:
					construct := group[j].text
					if group[j].punct(":") {
						k := j + 1
						if k < len(group) && group[k].punct(":") {
							construct += ":"
							k++
						}
						if k < len(group) && group[k].kind == tIdent {
							construct += group[k].text
						}
					}
					p.warn(webstyle.DiagnosticUnsupported, group[j], "unsupported-selector "+construct)
					return nil // one excluded member poisons the complete grouped rule
				}
			}
			if j == 0 {
				p.warn(webstyle.DiagnosticUnsupported, start, "unsupported-selector")
				return nil
			}
			s.parts = append(s.parts, c)
			group = trim(group[j:])
		}
		if len(s.parts) == 0 {
			p.warn(webstyle.DiagnosticInvalid, start, "invalid-selector empty group")
			return nil
		}
		out = append(out, s)
		if i == len(ts) {
			break
		}
		ts = trim(ts[i+1:])
		if len(ts) == 0 {
			p.warn(webstyle.DiagnosticInvalid, start, "invalid-selector trailing comma")
			return nil
		}
	}
	return out
}

func (c compound) matches(n *webrender.Node) bool {
	if n == nil || n.Kind != webrender.KindElement || c.tag != "" && c.tag != lower(n.Tag) {
		return false
	}
	for _, id := range c.ids {
		if n.Attr("id") != id {
			return false
		}
	}
	for _, class := range c.classes {
		found := false
		for _, actual := range strings.FieldsFunc(n.Attr("class"), func(r rune) bool {
			return r < 128 && space(byte(r))
		}) {
			if actual == class {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

const maxMatchingWork = 8000000

func (s selector) matches(path []*webrender.Node, work *int) (bool, bool) {
	if len(path) == 0 || len(s.parts) == 0 {
		return false, false
	}
	part, ancestor := len(s.parts)-1, len(path)-1
	for part >= 0 && ancestor >= 0 {
		if work != nil {
			if *work >= maxMatchingWork {
				return false, true
			}
			*work++
		}
		if s.parts[part].matches(path[ancestor]) {
			part--
		} else if ancestor == len(path)-1 {
			return false, false
		}
		ancestor--
	}
	return part < 0, false
}
