package pdf

// Views contain physical byte spans, never a retained AST or Go pointers.
// Container validation uses an explicit 32-frame POD stack. Semantic reads
// rescan validated spans and charge every examination and record operation.
type kind uint8

const (
	kBad kind = iota
	kNull
	kBool
	kNumber
	kName
	kString
	kArray
	kDict
	kRef
	kWord
)

type value struct {
	start, end int32
	n          int64 // exact millionths for decimals; object number for a reference
	count      uint16
	k          kind
	integer    bool
	big        bool
	f          float64
}
type parseFrame struct {
	keys      [256]int32
	count     uint16
	dict, key bool
}
type cursor struct {
	e        *engine
	pos, end int
}

func (e *engine) byteAt(pos int) byte {
	if e.f.Code != OK {
		return 0
	}
	if pos < 0 || int64(pos) >= e.src.Length() {
		e.set(Malformed)
		return 0
	}
	if pos < e.winStart || pos >= e.winStart+e.winN {
		start := pos / len(e.window) * len(e.window)
		n := min(len(e.window), int(e.src.Length())-start)
		got, c := e.src.ReadAt(e.window[:n], int64(start), e.l)
		e.set(c)
		if got != n {
			e.set(ReadFailed)
		}
		e.winStart = start
		e.winN = got
	}
	if !e.charge(Examine, 1) {
		return 0
	}
	return e.window[pos-e.winStart]
}
func (c *cursor) peek() byte {
	if c.pos >= c.end {
		return 0
	}
	return c.e.byteAt(c.pos)
}
func (c *cursor) get() byte {
	b := c.peek()
	if c.pos < c.end {
		c.pos++
	}
	return b
}
func white(b byte) bool { return b == 0 || b == 9 || b == 10 || b == 12 || b == 13 || b == 32 }
func delimiter(b byte) bool {
	return white(b) || b == '(' || b == ')' || b == '<' || b == '>' || b == '[' || b == ']' || b == '/' || b == '%' || b == 0
}
func (c *cursor) space() {
	for c.pos < c.end && c.e.f.Code == OK {
		b := c.peek()
		if white(b) {
			c.pos++
			continue
		}
		if b != '%' {
			return
		}
		for c.pos < c.end && c.e.f.Code == OK {
			if b = c.get(); b == 10 || b == 13 {
				break
			}
		}
	}
}
func (c *cursor) word(s string) bool {
	c.space()
	for i := 0; i < len(s); i++ {
		if c.pos >= c.end || c.get() != s[i] {
			return false
		}
	}
	return c.pos == c.end || delimiter(c.peek())
}
func hex(b byte) int {
	if b >= '0' && b <= '9' {
		return int(b - '0')
	}
	if b >= 'a' && b <= 'f' {
		return int(b-'a') + 10
	}
	if b >= 'A' && b <= 'F' {
		return int(b-'A') + 10
	}
	return -1
}
func (c *cursor) name(out *[64]byte) int {
	if c.get() != '/' {
		c.e.set(Malformed)
		return 0
	}
	n := 0
	for c.pos < c.end && !delimiter(c.peek()) && c.e.f.Code == OK {
		b := c.get()
		if b == '#' {
			if c.pos+2 > c.end {
				c.e.set(Malformed)
				break
			}
			a, d := hex(c.get()), hex(c.get())
			if a < 0 || d < 0 {
				c.e.set(Malformed)
				break
			}
			b = byte(a*16 + d)
		}
		if b == 0 || b > 127 {
			c.e.set(Malformed)
			break
		}
		if n == 64 {
			c.e.set(TokenLimit)
			break
		}
		c.e.charge(Copy, 1)
		out[n] = b
		n++
	}
	if n == 0 {
		c.e.set(Malformed)
	}
	return n
}
func nameEqual(a *[64]byte, n int, s string) bool {
	if n != len(s) {
		return false
	}
	for i := 0; i < n; i++ {
		if a[i] != s[i] {
			return false
		}
	}
	return true
}
func (e *engine) named(v value, s string) bool {
	if v.k != kName {
		return false
	}
	c := cursor{e, int(v.start), int(v.end)}
	var a [64]byte
	return nameEqual(&a, c.name(&a), s)
}
func (c *cursor) number() value {
	v := value{start: int32(c.pos), k: kNumber, integer: true}
	sign := int64(1)
	if c.peek() == '-' || c.peek() == '+' {
		if c.get() == '-' {
			sign = -1
		}
	}
	var whole, fraction int64
	magnitude := 0.0
	scale := 1.0
	digits, places := 0, 0
	dot := false
	for c.pos < c.end && c.e.f.Code == OK {
		b := c.peek()
		if b == '.' && !dot {
			dot = true
			v.integer = false
			c.pos++
			continue
		}
		if b < '0' || b > '9' {
			break
		}
		c.pos++
		digits++
		magnitude = magnitude*10 + float64(b-'0')
		if c.pos-int(v.start) > 32 {
			c.e.set(TokenLimit)
			break
		}
		if dot {
			places++
			scale *= 10
			if places > 6 {
				c.e.set(Malformed)
				break
			}
			fraction = fraction*10 + int64(b-'0')
		} else {
			if whole > (int64(1<<63-1)/1_000_000-int64(b-'0'))/10 {
				v.big = true
			} else if !v.big {
				whole = whole*10 + int64(b-'0')
			}
		}
	}
	if digits == 0 || c.pos-int(v.start) > 32 || c.pos < c.end && !delimiter(c.peek()) {
		c.e.set(Malformed)
	}
	for places < 6 {
		fraction *= 10
		places++
	}
	if whole > (int64(1<<63-1)-fraction)/1_000_000 {
		v.big = true
	} else {
		v.n = sign * (whole*1_000_000 + fraction)
	}
	v.f = float64(sign) * magnitude / scale
	v.end = int32(c.pos)
	return v
}

func (c *cursor) stringBytes(dst []byte) int {
	first := c.get()
	n := 0
	put := func(b byte) {
		if n >= 4096 {
			c.e.set(TokenLimit)
			return
		}
		if dst != nil {
			c.e.charge(Copy, 1)
			dst[n] = b
		}
		n++
	}
	if first == '<' {
		hi := -1
		for c.pos < c.end && c.e.f.Code == OK {
			b := c.get()
			if b == '>' {
				if hi >= 0 {
					put(byte(hi * 16))
				}
				return n
			}
			if white(b) {
				continue
			}
			d := hex(b)
			if d < 0 {
				c.e.set(Malformed)
				return n
			}
			if hi < 0 {
				hi = d
			} else {
				put(byte(hi*16 + d))
				hi = -1
			}
		}
	} else if first == '(' {
		depth := 1
		for c.pos < c.end && c.e.f.Code == OK {
			b := c.get()
			switch b {
			case '(':
				if depth == 32 {
					c.e.set(DepthLimit)
					return n
				}
				c.e.charge(Stack, 1)
				depth++
				put(b)
			case ')':
				c.e.charge(Stack, 1)
				depth--
				if depth == 0 {
					return n
				}
				put(b)
			case '\\':
				if c.pos == c.end {
					c.e.set(Malformed)
					return n
				}
				b = c.get()
				switch b {
				case 'n':
					b = 10
				case 'r':
					b = 13
				case 't':
					b = 9
				case 'b':
					b = 8
				case 'f':
					b = 12
				case 10:
					continue
				case 13:
					if c.pos < c.end && c.peek() == 10 {
						c.pos++
					}
					continue
				default:
					if b >= '0' && b <= '7' {
						x := int(b - '0')
						for k := 0; k < 2 && c.pos < c.end; k++ {
							d := c.peek()
							if d < '0' || d > '7' {
								break
							}
							c.pos++
							x = x*8 + int(d-'0')
						}
						b = byte(x)
					}
				}
				put(b)
			case 13:
				if c.pos < c.end && c.peek() == 10 {
					c.pos++
				}
				put(10)
			default:
				put(b)
			}
		}
	}
	c.e.set(Malformed)
	return n
}
func (c *cursor) atom() value {
	c.space()
	v := value{start: int32(c.pos)}
	if c.pos >= c.end {
		c.e.set(Malformed)
		return v
	}
	c.e.charge(Token, 1)
	switch b := c.peek(); b {
	case '/':
		var name [64]byte
		v.count = uint16(c.name(&name))
		v.k = kName
	case '(':
		v.count = uint16(c.stringBytes(nil))
		v.k = kString
	case '<':
		c.pos++
		if c.pos < c.end && c.peek() == '<' {
			c.pos++
			v.k = kDict
		} else {
			c.pos--
			v.count = uint16(c.stringBytes(nil))
			v.k = kString
		}
	case '[':
		c.pos++
		v.k = kArray
	default:
		if b == '+' || b == '-' || b == '.' || b >= '0' && b <= '9' {
			v = c.number()
			// A reference has two unsigned integer tokens and R. Speculative
			// lookahead is charged; it never consumes on a non-reference.
			if v.integer && v.n >= 0 && c.e.f.Code == OK {
				saved := c.pos
				c.space()
				if c.peek() >= '0' && c.peek() <= '9' {
					g := c.number()
					c.space()
					if c.peek() == 'R' {
						c.pos++
						if c.pos < c.end && !delimiter(c.peek()) {
							c.e.set(Malformed)
						}
						if !g.integer || g.n != 0 {
							c.e.set(UnsupportedStructure)
						}
						v.k = kRef
						v.n /= 1_000_000
						if v.big {
							c.e.set(Malformed)
						}
						if c.e.size > 0 {
							c.e.ref(v)
						}
					} else {
						c.pos = saved
					}
				} else {
					c.pos = saved
				}
			}
		} else {
			var word [32]byte
			n := 0
			for c.pos < c.end && !delimiter(c.peek()) && c.e.f.Code == OK {
				if n == 32 {
					c.e.set(TokenLimit)
					break
				}
				word[n] = c.get()
				n++
			}
			switch string(word[:n]) {
			case "null":
				v.k = kNull
			case "true":
				v.k = kBool
				v.n = 1
			case "false":
				v.k = kBool
			default:
				c.e.set(Malformed)
			}
		}
	}
	v.end = int32(c.pos)
	return v
}
func (c *cursor) value() value {
	v := c.atom()
	if v.k != kArray && v.k != kDict {
		if c.e.f.Code != OK && c.e.f.Offset < 0 {
			c.e.f.Offset = c.pos
		}
		return v
	}
	e := c.e
	depth := 1
	e.charge(Stack, 1)
	e.parse[0].count = 0
	e.parse[0].dict = v.k == kDict
	e.parse[0].key = v.k == kDict
	for depth > 0 && e.f.Code == OK {
		c.space()
		f := &e.parse[depth-1]
		end := c.peek() == ']' && !f.dict || c.peek() == '>' && f.dict
		if end {
			c.pos++
			if f.dict && (c.get() != '>' || !f.key) {
				e.set(Malformed)
				break
			}
			if depth == 1 {
				v.count = f.count
			}
			e.charge(Stack, 1)
			depth--
			continue
		}
		if f.dict && f.key {
			if c.peek() != '/' {
				e.set(Malformed)
				break
			}
			if f.count == 256 {
				e.set(ContainerLimit)
				break
			}
			pos := c.pos
			var key [64]byte
			n := c.name(&key)
			for i := 0; i < int(f.count) && e.f.Code == OK; i++ {
				prior := cursor{e, int(f.keys[i]), c.end}
				var other [64]byte
				m := prior.name(&other)
				e.charge(Record, 1)
				if n == m && key == other {
					e.set(Malformed)
				}
			}
			e.charge(Record, 1)
			f.keys[f.count] = int32(pos)
			f.count++
			f.key = false
			continue
		}
		if !f.dict {
			if f.count == 512 {
				e.set(ContainerLimit)
				break
			}
			f.count++
		}
		a := c.atom()
		if a.k == kBad {
			e.set(Malformed)
			break
		}
		if f.dict {
			f.key = true
		}
		if a.k == kDict || a.k == kArray {
			if depth == 32 {
				e.set(DepthLimit)
				break
			}
			e.charge(Stack, 1)
			e.parse[depth].count = 0
			e.parse[depth].dict = a.k == kDict
			e.parse[depth].key = a.k == kDict
			depth++
		}
	}
	v.end = int32(c.pos)
	if e.f.Code != OK && e.f.Offset < 0 {
		e.f.Offset = c.pos
	}
	return v
}

type pair struct {
	key value
	val value
}
type iterator struct {
	c         cursor
	dict      bool
	remaining int
}

func (e *engine) iter(v value) iterator {
	if v.k != kDict && v.k != kArray {
		e.set(Malformed)
	}
	n := 1
	if v.k == kDict {
		n = 2
	}
	return iterator{cursor{e, int(v.start) + n, int(v.end) - n}, v.k == kDict, int(v.count)}
}
func (i *iterator) next() pair {
	if i.remaining == 0 {
		return pair{}
	}
	i.remaining--
	p := pair{}
	if i.dict {
		p.key = i.c.atom()
	}
	p.val = i.c.value()
	return p
}
func (e *engine) get(d value, key string) value {
	i := e.iter(d)
	for i.remaining > 0 && e.f.Code == OK {
		p := i.next()
		if e.named(p.key, key) {
			return p.val
		}
	}
	return value{}
}
func (e *engine) number(v value) float64 {
	if v.k != kNumber {
		e.set(Malformed)
	}
	if v.big {
		return v.f
	}
	return float64(v.n) / 1_000_000
}
func (e *engine) integer(v value) int {
	if v.big && v.k == kNumber && v.integer {
		if v.f <= -9223372036854775808.0 || v.f >= 9223372036854775808.0 {
			e.set(Malformed)
			return 0
		}
		return int(v.f)
	}
	if v.k != kNumber || !v.integer || v.n%1_000_000 != 0 {
		e.set(Malformed)
		return 0
	}
	return int(v.n / 1_000_000)
}
func (e *engine) ref(v value) int {
	e.charge(Reference, 1)
	if v.k != kRef || v.n <= 0 || v.n >= int64(e.size) {
		e.set(Malformed)
		return 0
	}
	return int(v.n)
}
func (e *engine) objectValue(n int) (value, int, int, bool) {
	if n <= 0 || n >= e.size {
		e.set(Malformed)
		return value{}, 0, 0, false
	}
	e.object = n
	e.charge(Object, 1)
	c := cursor{e, int(e.x[n].off), int(e.x[n].end)}
	id := c.atom()
	gen := c.atom()
	if e.integer(id) != n || e.integer(gen) != 0 || !c.word("obj") {
		e.set(Malformed)
	}
	v := c.value()
	c.space()
	stream := false
	start, length := 0, 0
	if c.peek() == 's' {
		stream = true
		if v.k != kDict || !c.word("stream") {
			e.set(Malformed)
		}
		length = e.integer(e.get(v, "Length"))
		if length < 0 {
			e.set(Malformed)
		}
		if c.get() == 13 {
			if c.peek() == 10 {
				c.pos++
			}
		} else if e.byteAt(c.pos-1) != 10 {
			e.set(Malformed)
		}
		start = c.pos
		if length < 0 || length > c.end-start {
			e.set(Malformed)
			return v, 0, 0, stream
		}
		c.pos += length
		if c.peek() == 13 {
			c.pos++
			if c.peek() == 10 {
				c.pos++
			}
		} else if c.peek() == 10 {
			c.pos++
		}
		if !c.word("endstream") {
			e.set(Malformed)
		}
	}
	if !c.word("endobj") {
		e.set(Malformed)
	}
	c.space()
	if c.pos != c.end {
		e.set(Malformed)
	}
	if e.f.Code != OK && e.f.Offset < 0 {
		e.f.Offset = c.pos
	}
	return v, start, length, stream
}
func (e *engine) resolve(v value) value {
	// Container indirections are legal, but references cannot cycle or hide a
	// deeper traversal. No Go recursion or success-shaped missing defaults.
	var active [32]int
	for d := 0; v.k == kRef && e.f.Code == OK; d++ {
		if d == 32 {
			e.set(DepthLimit)
			break
		}
		n := e.ref(v)
		for i := 0; i < d; i++ {
			if active[i] == n {
				e.set(Malformed)
			}
		}
		active[d] = n
		e.charge(Stack, 1)
		if n == 0 {
			break
		}
		e.x[n].role |= roleValue
		var stream bool
		v, _, _, stream = e.objectValue(n)
		if stream {
			e.set(Malformed)
		}
	}
	return v
}
func (e *engine) allowed(d value, keys string, otherwise Code) {
	// Resource-bearing keys take precedence over unknown keys anywhere in
	// this dictionary, irrespective of physical key ordering.
	pre := e.iter(d)
	for pre.remaining > 0 && e.f.Code == OK {
		p := pre.next()
		for _, key := range [...]string{"F", "FFilter", "FDecodeParms", "URI", "FS", "EF", "RF", "URL"} {
			if e.named(p.key, key) {
				e.set(ExternalResource)
				return
			}
		}
		if e.named(p.key, "Encrypt") {
			e.set(Encrypted)
			return
		}
	}
	i := e.iter(d)
	for i.remaining > 0 && e.f.Code == OK {
		p := i.next()
		var a [64]byte
		c := cursor{e, int(p.key.start), int(p.key.end)}
		n := c.name(&a)
		if nameEqual(&a, n, "F") || nameEqual(&a, n, "FFilter") || nameEqual(&a, n, "FDecodeParms") ||
			nameEqual(&a, n, "URI") || nameEqual(&a, n, "FS") {
			e.set(ExternalResource)
			return
		}
		if nameEqual(&a, n, "Encrypt") {
			e.set(Encrypted)
			return
		}
		yes := false
		for start := 0; start < len(keys); {
			end := start
			for end < len(keys) && keys[end] != ' ' {
				end++
			}
			if nameEqual(&a, n, keys[start:end]) {
				yes = true
				break
			}
			start = end + 1
		}
		if !yes {
			code := otherwise
			switch string(a[:n]) {
			case "Prev", "XRefStm", "Linearized":
				code = UnsupportedStructure
			case "Rotate", "UserUnit", "BleedBox", "TrimBox", "ArtBox":
				code = UnsupportedPageGeometry
			}
			e.set(code)
		}
	}
}
