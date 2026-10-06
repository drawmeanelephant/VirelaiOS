package webrender

import "strings"

// NodeKind distinguishes element and text nodes.
type NodeKind uint8

const (
	// KindElement is an element node (Tag is set).
	KindElement NodeKind = iota
	// KindText is a text node (Text is set).
	KindText
)

// Attr is one HTML attribute.
type Attr struct {
	Name  string
	Value string
}

// Node is a document node: either an element or a run of text.
type Node struct {
	Kind     NodeKind
	Tag      string // lowercase element name (KindElement)
	Text     string // decoded text (KindText)
	Attrs    []Attr
	Children []*Node
}

// Attr returns the value of the named attribute ("" when absent).
func (n *Node) Attr(name string) string {
	for i := range n.Attrs {
		if n.Attrs[i].Name == name {
			return n.Attrs[i].Value
		}
	}
	return ""
}

// HasAttr reports whether the attribute is present.
func (n *Node) HasAttr(name string) bool {
	for i := range n.Attrs {
		if n.Attrs[i].Name == name {
			return true
		}
	}
	return false
}

// Document is a parsed page. Root is a synthetic "document" element whose
// children are the top-level nodes.
type Document struct {
	Root      *Node
	Nodes     int  // total nodes kept
	TextBytes int  // decoded text bytes kept
	Truncated bool // a cap was hit: the remainder was dropped visibly
}

// Parser caps. Every cap is a project decision (ADR 0028 D6): a page that
// exceeds one renders a visible truncation notice rather than panicking,
// hanging, or silently cutting content.
const (
	MaxNodes     = 20000
	MaxDepth     = 256
	MaxAttrLen   = 4096
	MaxAttrCount = 64
)

// VoidElement reports whether tag never has a closing form.
func VoidElement(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link",
		"meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

// SkipSubtree reports whether the element's content is never rendered.
func SkipSubtree(tag string) bool {
	switch tag {
	case "script", "style", "head", "title", "meta", "link", "template":
		return true
	}
	return false
}

// BlockElement reports whether the element starts a new block box.
func BlockElement(tag string) bool {
	switch tag {
	case "html", "body", "div", "p", "h1", "h2", "h3", "h4", "h5", "h6",
		"ul", "ol", "li", "pre", "blockquote", "hr", "table", "thead", "tbody",
		"tr", "td", "th", "dl", "dt", "dd", "img", "section", "article", "header",
		"footer", "main", "nav", "aside", "figure", "figcaption", "form", "center",
		"fieldset", "legend", "textarea", "button", "select", "address":
		return true
	}
	return false
}

// FormControl reports whether tag is a static form control. These are
// replaced boxes (value/placeholder/label), never interactive — there is
// no form submission and no cascade (ADR 0028 D2).
func FormControl(tag string) bool {
	switch tag {
	case "input", "textarea", "select", "button":
		return true
	}
	return false
}

type parser struct {
	src   []byte
	pos   int
	doc   *Document
	stack []*Node
	html  *Node
	head  *Node
	body  *Node
}

// ParseHTML parses src into a Document. It never panics and never drops text:
// malformed markup is flattened into the enclosing block in document order.
func ParseHTML(src []byte) *Document {
	doc := &Document{Root: &Node{Kind: KindElement, Tag: "document"}}
	p := &parser{src: src, doc: doc}
	p.stack = []*Node{doc.Root}
	p.html = &Node{Kind: KindElement, Tag: "html"}
	p.push(p.html)
	p.head = &Node{Kind: KindElement, Tag: "head"}
	p.addChild(p.html, p.head)

	for p.pos < len(p.src) {
		if doc.Nodes >= MaxNodes {
			doc.Truncated = true
			break
		}
		if p.src[p.pos] == '<' {
			if !p.consumeMarkup() {
				p.appendStrayText()
			}
		} else {
			p.appendText()
		}
	}
	p.ensureBody()
	p.unwrapNoScript(doc.Root)
	return doc
}

func (p *parser) top() *Node { return p.stack[len(p.stack)-1] }

func (p *parser) addChild(parent, n *Node) bool {
	if p.doc.Nodes >= MaxNodes {
		p.doc.Truncated = true
		return false
	}
	parent.Children = append(parent.Children, n)
	p.doc.Nodes++
	if n.Kind == KindText {
		p.doc.TextBytes += len(n.Text)
	}
	return true
}

func (p *parser) push(n *Node) bool {
	if !p.addChild(p.top(), n) {
		return false
	}
	p.stack = append(p.stack, n)
	return true
}

func (p *parser) appendTextNode(s string) {
	if s == "" {
		return
	}
	if p.top() == p.html && (p.body != nil || strings.TrimSpace(s) != "") {
		p.ensureBody()
	}
	if p.top() == p.head && strings.TrimSpace(s) != "" {
		p.ensureBody()
	}
	p.addChild(p.top(), &Node{Kind: KindText, Text: s})
}

// appendText preserves the tokenizer's existing text-run boundaries.
func (p *parser) appendText() {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != '<' {
		p.pos++
	}
	p.appendTextNode(DecodeEntities(string(p.src[start:p.pos])))
}

// Batch adjacent stray '<' bytes, but retain their existing coalescing with
// the previous run. Copying once avoids quadratic work on '<<<<'.
func (p *parser) appendStrayText() {
	start := p.pos
	p.pos++
	for p.pos < len(p.src) && p.src[p.pos] == '<' {
		if p.pos+1 < len(p.src) {
			next := p.src[p.pos+1]
			if isNameStart(next) || next == '!' || next == '?' || next == '/' {
				break
			}
		}
		p.pos++
	}
	text := string(p.src[start:p.pos])
	kids := p.top().Children
	if len(kids) > 0 && kids[len(kids)-1].Kind == KindText {
		kids[len(kids)-1].Text += text
		p.doc.TextBytes += len(text)
	} else {
		p.appendTextNode(text)
	}
}

func (p *parser) ensureBody() {
	if p.body == nil {
		body := &Node{Kind: KindElement, Tag: "body"}
		if !p.addChild(p.html, body) {
			return
		}
		p.body = body
	}
	if len(p.stack) <= 2 || p.stack[2] == p.head {
		p.stack = []*Node{p.doc.Root, p.html, p.body}
	}
}

// Keep the original fallback descendants, but remove the noscript wrapper:
// the frozen UA style table still skips that tag. No style/layout change is
// needed, and script/style descendants remain inert.
func (p *parser) unwrapNoScript(n *Node) {
	var children []*Node
	for i, c := range n.Children {
		p.unwrapNoScript(c)
		if c.Tag == "noscript" {
			if children == nil {
				children = append(make([]*Node, 0, len(n.Children)), n.Children[:i]...)
			}
			children = append(children, c.Children...)
			p.doc.Nodes--
		} else if children != nil {
			children = append(children, c)
		}
	}
	if children != nil {
		n.Children = children
	}
}

func headElement(tag string) bool {
	switch tag {
	case "base", "link", "meta", "title", "style", "script", "template":
		return true
	}
	return false
}

func mergeAttrs(dst, src *Node) {
	for _, a := range src.Attrs {
		if !dst.HasAttr(a.Name) && len(dst.Attrs) < MaxAttrCount {
			dst.Attrs = append(dst.Attrs, a)
		}
	}
}

// consumeMarkup parses one '<...>' construct. Returns false when the byte
// after '<' cannot start markup (the caller then keeps it as text).
func (p *parser) consumeMarkup() bool {
	if p.pos+1 >= len(p.src) {
		return false
	}
	next := p.src[p.pos+1]
	switch {
	case next == '!':
		if p.pos+3 < len(p.src) && p.src[p.pos+2] == '-' && p.src[p.pos+3] == '-' {
			end := indexFrom(p.src, p.pos+4, "-->")
			if end < 0 {
				p.pos = len(p.src)
			} else {
				p.pos = end + 3
			}
			return true
		}
		// <!DOCTYPE ...> and friends: skip to '>'.
		end := indexByteFrom(p.src, p.pos+2, '>')
		if end < 0 {
			p.pos = len(p.src)
		} else {
			p.pos = end + 1
		}
		return true
	case next == '?':
		end := indexByteFrom(p.src, p.pos+2, '>')
		if end < 0 {
			p.pos = len(p.src)
		} else {
			p.pos = end + 1
		}
		return true
	case next == '/':
		return p.closeTag()
	case isNameStart(next):
		return p.openTag()
	}
	return false
}

func (p *parser) closeTag() bool {
	i := p.pos + 2
	nameStart := i
	for i < len(p.src) && isNameChar(p.src[i]) {
		i++
	}
	name := strings.ToLower(string(p.src[nameStart:i]))
	if name == "" {
		return false
	}
	end := indexByteFrom(p.src, i, '>')
	if end < 0 {
		p.pos = len(p.src)
		return true
	}
	p.pos = end + 1
	// The document skeleton is unique. Later content resumes in its body,
	// not beside html, and a stray head close cannot close body descendants.
	switch name {
	case "html", "body":
		p.ensureBody()
		p.stack = []*Node{p.doc.Root, p.html, p.body}
		return true
	case "head":
		if len(p.stack) > 2 && p.stack[2] == p.head {
			p.stack = p.stack[:2]
		}
		return true
	}
	// Pop to the nearest matching open element; unknown closes are ignored.
	for j := len(p.stack) - 1; j > 2; j-- {
		if p.stack[j].Tag == name {
			p.stack = p.stack[:j]
			return true
		}
	}
	return true
}

func (p *parser) openTag() bool {
	i := p.pos + 1
	nameStart := i
	for i < len(p.src) && isNameChar(p.src[i]) {
		i++
	}
	tag := strings.ToLower(string(p.src[nameStart:i]))
	if tag == "" {
		return false
	}
	node := &Node{Kind: KindElement, Tag: tag}

	// Attributes.
	attrCount := 0
	for i < len(p.src) {
		for i < len(p.src) && isSpace(p.src[i]) {
			i++
		}
		if i >= len(p.src) {
			break
		}
		if p.src[i] == '>' {
			i++
			break
		}
		if p.src[i] == '/' {
			i++
			if i < len(p.src) && p.src[i] == '>' {
				i++
			}
			break
		}
		ns := i
		for i < len(p.src) && isNameChar(p.src[i]) {
			i++
		}
		if i == ns {
			i++ // skip a junk byte so we always make progress
			continue
		}
		name := strings.ToLower(string(p.src[ns:i]))
		for i < len(p.src) && isSpace(p.src[i]) {
			i++
		}
		val := ""
		if i < len(p.src) && p.src[i] == '=' {
			i++
			for i < len(p.src) && isSpace(p.src[i]) {
				i++
			}
			if i < len(p.src) && (p.src[i] == '"' || p.src[i] == '\'') {
				q := p.src[i]
				i++
				vs := i
				for i < len(p.src) && p.src[i] != q {
					i++
				}
				val = DecodeEntities(string(p.src[vs:i]))
				if i < len(p.src) {
					i++
				}
			} else {
				vs := i
				for i < len(p.src) && !isSpace(p.src[i]) && p.src[i] != '>' {
					i++
				}
				val = DecodeEntities(string(p.src[vs:i]))
			}
		}
		if attrCount < MaxAttrCount && len(name) <= 64 {
			if len(val) > MaxAttrLen {
				val = val[:MaxAttrLen]
				p.doc.Truncated = true
			}
			node.Attrs = append(node.Attrs, Attr{Name: name, Value: val})
			attrCount++
		} else {
			p.doc.Truncated = true
		}
	}
	p.pos = i

	switch tag {
	case "html":
		mergeAttrs(p.html, node)
		return true
	case "head":
		if p.body == nil {
			mergeAttrs(p.head, node)
			p.stack = []*Node{p.doc.Root, p.html, p.head}
		}
		return true
	case "body":
		p.ensureBody()
		if p.body != nil {
			mergeAttrs(p.body, node)
		}
		return true
	}
	if p.body == nil && headElement(tag) {
		if p.top() == p.html {
			p.stack = append(p.stack, p.head)
		}
	} else {
		p.ensureBody()
	}

	p.implicitClose(tag)
	p.implyTableParents(tag)
	if !p.addChild(p.top(), node) {
		return true
	}
	atDepthCap := len(p.stack) >= MaxDepth
	if atDepthCap && !VoidElement(tag) {
		// Depth cap: the element's content still lands in the enclosing
		// block in document order (text is never dropped), and the
		// truncation is visible on the Document.
		p.doc.Truncated = true
	}
	if rawTextElement(tag) || tag == "title" || tag == "textarea" {
		// Text states consume their own end tag even at a cap. Otherwise
		// script bytes could become visible markup in the enclosing block.
		p.consumeTextElement(node, !atDepthCap)
		return true
	}
	if VoidElement(tag) || atDepthCap {
		return true
	}
	p.stack = append(p.stack, node)
	return true
}

func rawTextElement(tag string) bool { return tag == "script" || tag == "style" }

// Raw text/RCDATA recognize only an ASCII-case-insensitive, delimited end
// tag for the current element. A quoted '>' in a malformed end tag is not
// its terminator. Everything else, including '<' and fake end-tag prefixes,
// belongs to the one text node. EOF without a complete end tag is text too.
func (p *parser) consumeTextElement(n *Node, keep bool) {
	start, textEnd, end := p.pos, len(p.src), len(p.src)
	for i := start; i+2+len(n.Tag) <= len(p.src); i++ {
		if p.src[i] != '<' || p.src[i+1] != '/' {
			continue
		}
		j := i + 2
		match := true
		for k := range n.Tag {
			c := p.src[j+k]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != n.Tag[k] {
				match = false
				break
			}
		}
		j += len(n.Tag)
		if !match || j >= len(p.src) || (!isSpace(p.src[j]) && p.src[j] != '/' && p.src[j] != '>') {
			continue
		}
		quote := byte(0)
		for ; j < len(p.src); j++ {
			c := p.src[j]
			if quote != 0 {
				if c == quote {
					quote = 0
				}
			} else if c == '"' || c == '\'' {
				quote = c
			} else if c == '>' {
				textEnd, end = i, j+1
				break
			}
		}
		// If the candidate ends at EOF, no later complete end tag exists
		// outside its unterminated quote. Do not scan its suffix again.
		break
	}
	p.pos = end
	if keep && textEnd > start {
		text := string(p.src[start:textEnd])
		if !rawTextElement(n.Tag) {
			text = DecodeEntities(text)
		}
		p.addChild(n, &Node{Kind: KindText, Text: text})
	} else if !keep && textEnd > start {
		p.doc.Truncated = true
	}
}

// popInScope closes the nearest target through inline descendants, without
// crossing a nested list/table/select (or the document skeleton).
func (p *parser) popInScope(targets, barriers string) {
	for i := len(p.stack) - 1; i > 2; i-- {
		tag := "|" + p.stack[i].Tag + "|"
		if strings.Contains(targets, tag) {
			p.stack = p.stack[:i]
			return
		}
		if strings.Contains(barriers, tag) {
			return
		}
	}
}

func (p *parser) implicitClose(tag string) {
	switch tag {
	case "li":
		p.popInScope("|li|", "|ul|ol|")
	case "dt", "dd":
		p.popInScope("|dt|dd|", "|dl|")
	case "td", "th":
		p.popInScope("|td|th|", "|tr|table|")
	case "tr":
		p.popInScope("|tr|", "|table|thead|tbody|tfoot|")
	case "thead", "tbody", "tfoot":
		p.popInScope("|thead|tbody|tfoot|", "|table|")
	case "option":
		p.popInScope("|option|", "|select|optgroup|")
	case "optgroup":
		p.popInScope("|option|", "|select|optgroup|")
		p.popInScope("|optgroup|", "|select|")
	default:
		if BlockElement(tag) || strings.Contains("|details|dialog|hgroup|menu|search|summary|", "|"+tag+"|") {
			if p.top().Tag == "p" {
				p.stack = p.stack[:len(p.stack)-1]
			} else if tag != "img" && !FormControl(tag) {
				p.popInScope("|p|", "|table|td|th|select|")
			}
		}
	}
}

func (p *parser) implyTableParents(tag string) {
	if tag != "tr" && tag != "td" && tag != "th" {
		return
	}
	if p.top().Tag == "table" {
		p.pushImplied("tbody")
	}
	if (tag == "td" || tag == "th") && (p.top().Tag == "thead" || p.top().Tag == "tbody" || p.top().Tag == "tfoot") {
		p.pushImplied("tr")
	}
}

func (p *parser) pushImplied(tag string) {
	if len(p.stack) >= MaxDepth {
		p.doc.Truncated = true
		return
	}
	p.push(&Node{Kind: KindElement, Tag: tag})
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func isNameStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameChar(c byte) bool {
	return isNameStart(c) || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == ':'
}

func indexByteFrom(b []byte, from int, want byte) int {
	for i := from; i < len(b); i++ {
		if b[i] == want {
			return i
		}
	}
	return -1
}

func indexFrom(b []byte, from int, want string) int {
	if from < 0 || from > len(b) {
		return -1
	}
	rel := strings.Index(string(b[from:]), want)
	if rel < 0 {
		return -1
	}
	return from + rel
}

// DecodeEntities decodes the common HTML entities and numeric references.
// Unknown entities are kept verbatim so nothing is silently lost.
func DecodeEntities(s string) string {
	if !strings.ContainsRune(s, '&') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			b.WriteByte(s[i])
			continue
		}
		semi := -1
		for j := i + 1; j < len(s) && j < i+12; j++ {
			if s[j] == ';' {
				semi = j
				break
			}
			if s[j] == '&' || s[j] == '<' || s[j] == ' ' {
				break
			}
		}
		// Named entities may appear without their semicolon; try the
		// longest matching name at this position before giving up.
		if semi < 0 {
			best := ""
			for _, e := range namedEntities {
				bare := strings.TrimSuffix(e.name, ";")
				if strings.HasPrefix(s[i:], bare) && len(bare) > len(best) {
					best = bare
				}
			}
			if best == "" {
				b.WriteByte('&')
				continue
			}
			key := best
			if strings.HasPrefix(best, "#") {
				if r, ok := decodeNumericRef(best[1:]); ok {
					b.WriteRune(r)
					i += len(best) - 1
					continue
				}
			}
			b.WriteString(entityTable[key])
			i += len(best) - 1
			continue
		}
		name := s[i+1 : semi]
		if strings.HasPrefix(name, "#") {
			if r, ok := decodeNumericRef(name[1:]); ok {
				b.WriteRune(r)
				i = semi
				continue
			}
			b.WriteByte('&')
			continue
		}
		v, ok := entityTable[strings.ToLower(name)]
		if !ok {
			b.WriteByte('&')
			continue
		}
		b.WriteString(v)
		i = semi
	}
	return b.String()
}

func decodeNumericRef(body string) (rune, bool) {
	base := 10
	if strings.HasPrefix(body, "x") || strings.HasPrefix(body, "X") {
		base = 16
		body = body[1:]
	}
	if body == "" {
		return 0, false
	}
	var v int64
	for i := 0; i < len(body); i++ {
		var d int64
		c := body[i]
		switch {
		case c >= '0' && c <= '9':
			d = int64(c - '0')
		case base == 16 && c >= 'a' && c <= 'f':
			d = int64(c-'a') + 10
		case base == 16 && c >= 'A' && c <= 'F':
			d = int64(c-'A') + 10
		default:
			return 0, false
		}
		v = v*int64(base) + d
		if v > 0x10FFFF {
			return 0xFFFD, true
		}
	}
	if v == 0 {
		return 0xFFFD, true
	}
	return rune(v), true
}

type entity struct {
	name  string
	value string
}

var entityTable = map[string]string{
	"amp": "&", "lt": "<", "gt": ">", "quot": "\"", "apos": "'",
	"nbsp": "\u00a0", "copy": "\u00a9", "reg": "\u00ae", "hellip": "\u2026",
	"mdash": "\u2014", "ndash": "\u2013", "lsquo": "\u2018", "rsquo": "\u2019",
	"ldquo": "\u201c", "rdquo": "\u201d", "times": "\u00d7", "middot": "\u00b7",
	"bull": "\u2022", "deg": "\u00b0", "eacute": "\u00e9", "uuml": "\u00fc",
}

var namedEntities = []entity{
	{"&#39;", "'"}, {"&#34;", "\""},
	{"&amp;", "&"}, {"&lt;", "<"}, {"&gt;", ">"}, {"&quot;", "\""}, {"&apos;", "'"},
	{"&nbsp;", "\u00a0"}, {"&copy;", "\u00a9"}, {"&hellip;", "\u2026"},
	{"&mdash;", "\u2014"}, {"&ndash;", "\u2013"}, {"&middot;", "\u00b7"},
}
