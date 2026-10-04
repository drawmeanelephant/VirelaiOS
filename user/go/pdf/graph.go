package pdf

// Graph validation is independent of rendering roles. Parent is the one
// non-traversed back-link; pageTree checks it exactly. Gray/black xref marks
// prevent both cycles and unbounded repeated descent through shared objects.
type graphFrame struct {
	node, next int
	refs       [512]uint16
	count      uint16
	height     uint16
}

func (e *engine) graph(root int) {
	var frames [32]graphFrame
	n := root
	depth := 0
	for e.f.Code == OK {
		if depth == 32 {
			e.set(DepthLimit)
			return
		}
		if n <= 0 || n >= e.size {
			e.set(Malformed)
			return
		}
		e.charge(Stack, 1)
		e.x[n].active = 1
		v, _, _, _ := e.objectValue(n)
		top := &frames[depth]
		top.node = n
		top.next = 0
		top.height = 1
		top.count = e.references(v, &top.refs)
		depth++
		for depth > 0 && e.f.Code == OK {
			top := &frames[depth-1]
			if top.next == int(top.count) {
				e.charge(Stack, 1)
				e.x[top.node].active = 2
				e.x[top.node].spare = top.height
				height := top.height
				depth--
				if depth > 0 {
					frames[depth-1].height = max(frames[depth-1].height, height+1)
				}
				continue
			}
			e.charge(Reference, 1)
			child := int(top.refs[top.next])
			top.next++
			if e.x[child].active == 1 {
				e.set(Malformed)
				return
			}
			if e.x[child].active == 2 {
				if depth+int(e.x[child].spare) > 32 {
					e.set(DepthLimit)
					return
				}
				top.height = max(top.height, e.x[child].spare+1)
				continue
			}
			n = child
			break
		}
		if depth == 0 {
			return
		}
	}
}
func (e *engine) references(v value, out *[512]uint16) uint16 {
	parentBacklink := false
	if v.k == kDict {
		ty := e.get(v, "Type")
		parentBacklink = e.named(ty, "Page") || e.named(ty, "Pages")
	}
	c := cursor{e, int(v.start), int(v.end)}
	var dict, key [32]bool
	depth := 0
	found := uint16(0)
	for c.pos < c.end && e.f.Code == OK {
		c.space()
		b := c.peek()
		if b == ']' || b == '>' {
			c.pos++
			if b == '>' {
				c.pos++
			}
			depth--
			e.charge(Stack, 1)
			continue
		}
		if depth > 0 && dict[depth-1] && key[depth-1] {
			k := c.atom()
			key[depth-1] = false
			if depth == 1 && parentBacklink && e.named(k, "Parent") {
				c.value()
				key[depth-1] = true
			}
			continue
		}
		a := c.atom()
		if depth > 0 && dict[depth-1] {
			key[depth-1] = true
		}
		if a.k == kRef {
			n := e.ref(a)
			if found == 512 {
				e.set(Malformed)
				return found
			}
			if !e.charge(Record, 1) {
				return found
			}
			out[found] = uint16(n)
			found++
		}
		if a.k == kDict || a.k == kArray {
			if depth == 32 {
				e.set(DepthLimit)
				return found
			}
			e.charge(Stack, 1)
			dict[depth] = a.k == kDict
			key[depth] = dict[depth]
			depth++
		}
	}
	return found
}
