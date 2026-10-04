package pdf

import "virelai/vector"

const (
	roleValue = 1 << iota
	roleCatalog
	rolePages
	rolePage
	roleContent
	roleFont
	roleGlyph
	roleImage
	roleInfo
)

type pageDesc struct {
	resources, contents value
	box                 [4]int64
	object              uint32
	width, height       uint16
}
type treeFrame struct {
	node, parent    int32
	kids            value
	children        [256]uint16
	at, leaves      uint16
	box, crop       [4]int64
	hasBox, hasCrop bool
	resources       value
}

func (e *engine) run(index int) {
	e.structure()
	if e.f.Code != OK {
		return
	}
	// The trailer view was retained by structure in sentinel offsets.
	c := cursor{e, int(e.x[0].off), int(e.x[0].end)}
	t := c.value()
	e.allowed(t, "Size Root Info ID", UnsupportedFeature)
	root := e.ref(e.get(t, "Root"))
	if e.f.Code != OK {
		return
	}
	e.graph(root)
	if info := e.get(t, "Info"); info.k != kBad {
		e.graph(e.ref(info))
	}
	if e.f.Code != OK {
		return
	}
	e.mark(root, roleCatalog)
	catalog, _, _, stream := e.objectValue(root)
	if stream {
		e.set(Malformed)
	}
	e.allowed(catalog, "Type Pages", UnsupportedFeature)
	if !e.named(e.get(catalog, "Type"), "Catalog") {
		e.set(Malformed)
	}
	e.pageTree(e.ref(e.get(catalog, "Pages")))
	if info := e.get(t, "Info"); info.k != kBad {
		e.info(e.ref(info))
	}
	if id := e.get(t, "ID"); id.k != kBad {
		if id.k != kArray || id.count != 2 {
			e.set(Malformed)
		} else {
			i := e.iter(id)
			for i.remaining > 0 {
				v := i.next().val
				if v.k != kString {
					e.set(Malformed)
				} else if v.count > 32 {
					e.set(TokenLimit)
				} else {
					e.ignoreMetadata(v.count)
				}
			}
		}
	}
	if index < 0 || index >= int(e.stats.Pages) {
		e.set(PageRange)
		return
	}
	// Whole-document validation visits pages in tree order, even when the
	// caller requests page zero. Resources and unused glyphs/images are closed.
	for n := 0; n < int(e.stats.Pages) && e.f.Code == OK; n++ {
		e.page = n
		e.renderPage(e.pages[n], false)
	}
	for n := 1; n < e.size && e.f.Code == OK; n++ {
		if e.x[n].role == 0 {
			e.object = n
			e.set(UnsupportedStructure)
		}
	}
	if e.f.Code != OK {
		return
	}
	e.page = index
	e.renderPage(e.pages[index], true)
}
func (e *engine) mark(n int, role uint16) {
	if n <= 0 || n >= e.size {
		e.set(Malformed)
		return
	}
	e.charge(Record, 1)
	if e.x[n].role != 0 && e.x[n].role&role == 0 {
		e.set(Malformed)
		return
	}
	e.x[n].role |= role
}
func (e *engine) unsigned(c *cursor) uint64 {
	c.space()
	var n uint64
	digits := 0
	for c.pos < c.end && e.f.Code == OK {
		b := c.peek()
		if b < '0' || b > '9' {
			break
		}
		c.pos++
		digits++
		if digits > 32 {
			e.set(TokenLimit)
			break
		}
		if n > (^uint64(0)-uint64(b-'0'))/10 {
			e.set(Malformed)
			break
		}
		n = n*10 + uint64(b-'0')
	}
	if digits == 0 {
		e.set(Malformed)
	}
	return n
}
func (e *engine) structure() {
	if e.src.Length() < 20 {
		e.set(Malformed)
		return
	}
	for i, b := range "%PDF-1." {
		if e.byteAt(i) != byte(b) {
			e.set(UnsupportedStructure)
			return
		}
	}
	if b := e.byteAt(7); b != '3' && b != '4' {
		e.set(UnsupportedStructure)
		return
	}
	if b := e.byteAt(8); b != 10 && b != 13 {
		e.set(Malformed)
		return
	}
	// Locate only the final startxref, in the bounded suffix; never repair-scan
	// object data. EOF and startxref spelling/offset must agree exactly.
	end := int(e.src.Length())
	for end > 0 && white(e.byteAt(end-1)) && e.f.Code == OK {
		end--
	}
	if end < 5 {
		e.set(Malformed)
		return
	}
	for i, b := range "%%EOF" {
		if e.byteAt(end-5+i) != byte(b) {
			e.set(Malformed)
		}
	}
	pos := end - 5
	for pos > 0 && white(e.byteAt(pos-1)) && e.f.Code == OK {
		pos--
	}
	numEnd := pos
	for pos > 0 && e.byteAt(pos-1) >= '0' && e.byteAt(pos-1) <= '9' && e.f.Code == OK {
		pos--
	}
	numStart := pos
	for pos > 0 && white(e.byteAt(pos-1)) && e.f.Code == OK {
		pos--
	}
	if pos < 9 || numStart == numEnd {
		e.set(Malformed)
		return
	}
	for i, b := range "startxref" {
		if e.byteAt(pos-9+i) != byte(b) {
			e.set(Malformed)
		}
	}
	for p := pos - 10; p >= 0; p-- {
		b := e.byteAt(p)
		if b == 10 || b == 13 {
			break
		}
		if b != 32 && b != 9 {
			e.set(Malformed)
			return
		}
	}
	q := cursor{e, numStart, numEnd}
	offset := e.unsigned(&q)
	if offset >= uint64(pos-9) || offset > MaxSource {
		e.set(Malformed)
		return
	}
	e.xoff = int(offset)
	q = cursor{e, e.xoff, pos - 9}
	if !q.word("xref") {
		e.set(UnsupportedStructure)
		return
	}
	zero, n := e.unsigned(&q), e.unsigned(&q)
	if zero != 0 || n < 1 {
		e.set(UnsupportedStructure)
		return
	}
	if n > MaxObjects+1 {
		e.set(ObjectLimit)
		return
	}
	e.size = int(n)
	e.stats.Objects = uint32(n - 1)
	if !e.charge(Zero, n*16) {
		return
	}
	for i := 0; i < e.size; i++ {
		e.x[i] = xref{}
	}
	e.xrefLine(&q)
	for i := 0; i < e.size && e.f.Code == OK; i++ {
		off := e.xrefDigits(&q, 10)
		if q.get() != 32 {
			e.set(Malformed)
		}
		gen := e.xrefDigits(&q, 5)
		if q.get() != 32 {
			e.set(Malformed)
		}
		state := q.get()
		if i == 0 {
			if off != 0 || gen != 65535 || state != 'f' {
				e.set(UnsupportedStructure)
			}
		} else {
			if gen != 0 || state != 'n' {
				e.set(UnsupportedStructure)
			}
			if off >= uint64(e.xoff) || off < 8 || i > 1 && off <= uint64(e.x[i-1].off) {
				e.set(Malformed)
			}
			e.x[i].off = uint32(off)
		}
		e.xrefLine(&q)
	}
	if !q.word("trailer") {
		e.set(UnsupportedStructure)
		return
	}
	start := q.pos
	t := q.value()
	finish := q.pos
	q.space()
	if q.pos != q.end {
		e.set(Malformed)
	}
	e.x[0].off = uint32(start)
	e.x[0].end = uint32(finish)
	e.allowed(t, "Size Root Info ID", UnsupportedFeature)
	if e.integer(e.get(t, "Size")) != e.size {
		e.set(Malformed)
	}
	header := cursor{e, 8, int(e.xoff)}
	header.space()
	if e.size > 1 && header.pos != int(e.x[1].off) || e.size == 1 && header.pos != e.xoff {
		e.set(Malformed)
	}
	for i := 1; i < e.size && e.f.Code == OK; i++ {
		end := e.xoff
		if i+1 < e.size {
			end = int(e.x[i+1].off)
		}
		e.x[i].end = uint32(end)
		e.object = i
		v, _, _, stream := e.objectValue(i)
		// Objects are validated physically before any semantic graph traversal.
		if v.k == kDict {
			if e.get(v, "Encrypt").k != kBad {
				e.set(Encrypted)
			}
			if e.get(v, "Linearized").k != kBad {
				e.set(UnsupportedStructure)
			}
			ty := e.get(v, "Type")
			if e.named(ty, "ObjStm") || e.named(ty, "XRef") {
				e.set(UnsupportedStructure)
			}
			switch {
			case e.named(ty, "Catalog"):
				e.allowed(v, "Type Pages", UnsupportedFeature)
			case e.named(ty, "Pages"):
				e.allowed(v, "Type Kids Count Parent MediaBox CropBox Rotate Resources", UnsupportedFeature)
			case e.named(ty, "Page"):
				e.allowed(v, "Type Parent MediaBox CropBox Rotate Resources Contents", UnsupportedFeature)
			case e.named(ty, "Font"):
				e.allowed(v, "Type Subtype FontBBox FontMatrix CharProcs Encoding FirstChar LastChar Widths Resources Name", UnsupportedFont)
				if !e.named(e.get(v, "Subtype"), "Type3") {
					e.set(UnsupportedFont)
				}
			case e.named(ty, "XObject"):
				if !e.named(e.get(v, "Subtype"), "Image") {
					e.set(UnsupportedFeature)
				}
				e.allowed(v, "Type Subtype Width Height BitsPerComponent ColorSpace Decode Interpolate ImageMask Length Filter DecodeParms", UnsupportedImage)
			}
			if stream {
				e.allowedStream(v)
			}
		}
	}
	e.object = -1
}
func (e *engine) xrefDigits(c *cursor, n int) uint64 {
	var value uint64
	for i := 0; i < n && e.f.Code == OK; i++ {
		b := c.get()
		if b < '0' || b > '9' {
			e.set(Malformed)
			return 0
		}
		value = value*10 + uint64(b-'0')
	}
	return value
}
func (e *engine) xrefLine(c *cursor) {
	for c.pos < c.end && (c.peek() == 32 || c.peek() == 9) && e.f.Code == OK {
		c.pos++
	}
	b := c.get()
	if b == 13 {
		if c.peek() == 10 {
			c.pos++
		}
	} else if b != 10 {
		e.set(Malformed)
	}
}
func (e *engine) allowedStream(v value) {
	// Validate filter syntax/checksums during the assigned role visit, but
	// external stream keys must win even on an orphan or unused stream.
	for _, key := range [...]string{"F", "FFilter", "FDecodeParms"} {
		if e.get(v, key).k != kBad {
			e.set(ExternalResource)
		}
	}
}
func (e *engine) box(v value) [4]int64 {
	var out [4]int64
	if v.k != kArray || v.count != 4 {
		e.set(Malformed)
		return out
	}
	i := e.iter(v)
	for n := 0; n < 4; n++ {
		a := i.next().val
		if a.k != kNumber {
			e.set(Malformed)
		}
		if a.big {
			e.set(CoordinateLimit)
		}
		out[n] = a.n
		if !finite(float64(a.n) / 1e6) {
			e.set(CoordinateLimit)
		}
	}
	if out[0] >= out[2] || out[1] >= out[3] {
		e.set(Malformed)
	}
	return out
}
func (e *engine) pageTree(root int) {
	var stack [32]treeFrame
	depth := 0
	node, parent := root, 0
	inherit := treeFrame{}
	for e.f.Code == OK {
		for j := 0; j < depth; j++ {
			if stack[j].node == int32(node) {
				e.set(Malformed)
			}
		}
		if depth == 32 {
			e.set(DepthLimit)
			return
		}
		e.charge(Stack, 1)
		v, _, _, stream := e.objectValue(node)
		if stream {
			e.set(Malformed)
		}
		ty := e.get(v, "Type")
		leaf := e.named(ty, "Page")
		if !leaf && !e.named(ty, "Pages") {
			e.set(Malformed)
		}
		if parent == 0 && leaf {
			e.set(Malformed)
		}
		if parent == 0 {
			if e.get(v, "Parent").k != kBad {
				e.set(Malformed)
			}
		} else if e.ref(e.get(v, "Parent")) != parent {
			e.set(Malformed)
		}
		// A child has exactly one owner, not just no active-path cycle.
		if node > 0 && e.x[node].role != 0 {
			e.set(Malformed)
		}
		if leaf {
			e.mark(node, rolePage)
			e.allowed(v, "Type Parent MediaBox CropBox Rotate Resources Contents", UnsupportedFeature)
		} else {
			e.mark(node, rolePages)
			e.allowed(v, "Type Kids Count Parent MediaBox CropBox Rotate Resources", UnsupportedFeature)
		}
		f := inherit
		f.node = int32(node)
		f.parent = int32(parent)
		f.at = 0
		f.leaves = 0
		if b := e.get(v, "MediaBox"); b.k != kBad {
			f.box = e.box(b)
			f.hasBox = true
		}
		if b := e.get(v, "CropBox"); b.k != kBad {
			f.crop = e.box(b)
			f.hasCrop = true
		}
		if r := e.get(v, "Rotate"); r.k != kBad && e.integer(r) != 0 {
			e.set(UnsupportedPageGeometry)
		}
		if r := e.get(v, "Resources"); r.k != kBad {
			f.resources = e.resolve(r)
			if f.resources.k != kDict {
				e.set(Malformed)
			}
			// Definitions replaced by a descendant remain declared, unused
			// resources. Validate them without a pixel-producing target.
			e.validatePage(pageDesc{resources: f.resources, width: 1, height: 1})
		}
		if leaf {
			if !f.hasBox {
				e.set(Malformed)
				return
			}
			box := f.box
			if f.hasCrop {
				box = [4]int64{max(box[0], f.crop[0]), max(box[1], f.crop[1]), min(box[2], f.crop[2]), min(box[3], f.crop[3])}
			}
			if box[0] >= box[2] || box[1] >= box[3] {
				e.set(Malformed)
				return
			}
			w, h := (box[2]-box[0])*4, (box[3]-box[1])*4
			if w%3_000_000 != 0 || h%3_000_000 != 0 {
				e.set(UnsupportedPageGeometry)
				return
			}
			w /= 3_000_000
			h /= 3_000_000
			if w > 1024 || h > 1536 {
				e.set(CanvasLimit)
				return
			}
			if e.stats.Pages == 256 {
				e.set(PageLimit)
				return
			}
			e.charge(Record, 1)
			e.pages[e.stats.Pages] = pageDesc{f.resources, e.get(v, "Contents"), box, uint32(node), uint16(w), uint16(h)}
			e.stats.Pages++
			if depth > 0 {
				stack[depth-1].leaves++
			}
		} else {
			f.kids = e.resolve(e.get(v, "Kids"))
			if f.kids.k != kArray || f.kids.count == 0 {
				e.set(Malformed)
				return
			}
			if f.kids.count > 256 {
				e.set(ContainerLimit)
				return
			}
			i := e.iter(f.kids)
			for j := uint16(0); j < f.kids.count && e.f.Code == OK; j++ {
				e.charge(Record, 1)
				f.children[j] = uint16(e.ref(i.next().val))
			}
			stack[depth] = f
			depth++
		}
		for depth > 0 && e.f.Code == OK {
			top := &stack[depth-1]
			if top.at < intCount(top.kids) {
				e.charge(Reference, 1)
				node = int(top.children[top.at])
				top.at++
				parent = int(top.node)
				inherit = *top
				break
			}
			v, _, _, _ := e.objectValue(int(top.node))
			if e.integer(e.get(v, "Count")) != int(top.leaves) {
				e.set(Malformed)
			}
			leaves := top.leaves
			depth--
			e.charge(Stack, 1)
			if depth > 0 {
				stack[depth-1].leaves += leaves
			}
		}
		if depth == 0 {
			return
		}
	}
}
func intCount(v value) uint16 { return v.count }
func (e *engine) info(n int) {
	e.mark(n, roleInfo)
	v, _, _, s := e.objectValue(n)
	if s {
		e.set(Malformed)
	}
	e.allowed(v, "Title Author Subject Keywords Creator Producer CreationDate ModDate Trapped", UnsupportedFeature)
	i := e.iter(v)
	for i.remaining > 0 && e.f.Code == OK {
		p := i.next()
		if e.named(p.key, "Trapped") {
			if !e.named(p.val, "True") && !e.named(p.val, "False") && !e.named(p.val, "Unknown") {
				e.set(Malformed)
			}
			if e.f.Code == OK {
				e.ignoreMetadata(p.val.count)
			}
		} else {
			if p.val.k != kString {
				e.set(Malformed)
			}
			if e.f.Code == OK {
				e.ignoreMetadata(p.val.count)
			}
		}
	}
}
func (e *engine) resources(v value, key string) value {
	if v.k == kBad {
		return value{}
	}
	e.allowed(v, "Font XObject", UnsupportedFeature)
	d := e.get(v, key)
	if d.k == kBad {
		return d
	}
	d = e.resolve(d)
	if d.k != kDict {
		e.set(Malformed)
		return d
	}
	if d.count > 64 {
		e.set(ContainerLimit)
	}
	return d
}
func (e *engine) resource(v value, key string, name *[64]byte, n int) int {
	d := e.resources(v, key)
	if d.k == kBad {
		e.set(MissingResource)
		return 0
	}
	i := e.iter(d)
	for i.remaining > 0 && e.f.Code == OK {
		p := i.next()
		c := cursor{e, int(p.key.start), int(p.key.end)}
		var other [64]byte
		m := c.name(&other)
		if n == m && *name == other {
			return e.ref(p.val)
		}
	}
	e.set(MissingResource)
	return 0
}
func (e *engine) validatePage(p pageDesc) {
	e.initPage(p)
	// Mark every resource, not just the names used by content operators.
	fonts := e.resources(p.resources, "Font")
	if fonts.k != kBad {
		var seen [16]int
		distinct := 0
		i := e.iter(fonts)
		for i.remaining > 0 && e.f.Code == OK {
			n := e.ref(i.next().val)
			known := false
			for j := 0; j < distinct; j++ {
				e.charge(Reference, 1)
				if seen[j] == n {
					known = true
				}
			}
			if known {
				continue
			}
			if distinct == 16 {
				e.set(FontLimit)
				return
			}
			seen[distinct] = n
			distinct++
			e.mark(n, roleFont)
			e.validateFont(n)
		}
	}
	images := e.resources(p.resources, "XObject")
	if images.k != kBad {
		i := e.iter(images)
		for i.remaining > 0 && e.f.Code == OK {
			n := e.ref(i.next().val)
			e.mark(n, roleImage)
			e.image(n, identity, vector.Rect{}, false, 1)
		}
	}
}
func (e *engine) contents(v value, out *[16]int) int {
	if v.k == kBad || v.k == kNull {
		return 0
	}
	n := 1
	if v.k == kArray {
		n = int(v.count)
		if n > 16 {
			e.set(ContainerLimit)
			return 0
		}
		i := e.iter(v)
		for j := 0; j < n; j++ {
			out[j] = e.ref(i.next().val)
		}
	} else {
		out[0] = e.ref(v)
	}
	for j := 0; j < n && e.f.Code == OK; j++ {
		e.mark(out[j], roleContent)
		d, _, _, s := e.objectValue(out[j])
		if !s {
			e.set(Malformed)
		}
		e.allowed(d, "Length Filter DecodeParms", UnsupportedFeature)
	}
	return n
}
