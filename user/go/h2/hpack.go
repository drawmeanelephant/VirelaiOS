// HPACK (RFC 7541), bounded for the desk's fetch path. Decode is full:
// indexed fields, the three literal forms, dynamic-table size updates,
// and Huffman strings — the dynamic table honors the peer's updates up to
// our advertised dynTableMax. Encode is deliberately narrow: indexed
// static entries plus literal-without-indexing fields with uncompressed
// strings — legal HPACK that never touches the dynamic table, so the
// request head is deterministic.
//
// Bounds: maxHeaderFields fields per block, maxHeaderText summed name+value
// bytes, and a Huffman code longer than 30 bits or a non-EOS pad is a
// decode refusal — never a misparse.

package h2

// staticTable is RFC 7541 Appendix A, index 1..61.
var staticTable = [...]Field{
	{Name: ":authority"},
	{Name: ":method", Value: "GET"},
	{Name: ":method", Value: "POST"},
	{Name: ":path", Value: "/"},
	{Name: ":path", Value: "/index.html"},
	{Name: ":scheme", Value: "http"},
	{Name: ":scheme", Value: "https"},
	{Name: ":status", Value: "200"},
	{Name: ":status", Value: "204"},
	{Name: ":status", Value: "206"},
	{Name: ":status", Value: "304"},
	{Name: ":status", Value: "400"},
	{Name: ":status", Value: "404"},
	{Name: ":status", Value: "500"},
	{Name: "accept-charset"},
	{Name: "accept-encoding", Value: "gzip, deflate"},
	{Name: "accept-language"},
	{Name: "accept-ranges"},
	{Name: "accept"},
	{Name: "access-control-allow-origin"},
	{Name: "age"},
	{Name: "allow"},
	{Name: "authorization"},
	{Name: "cache-control"},
	{Name: "content-disposition"},
	{Name: "content-encoding"},
	{Name: "content-language"},
	{Name: "content-length"},
	{Name: "content-location"},
	{Name: "content-range"},
	{Name: "content-type"},
	{Name: "cookie"},
	{Name: "date"},
	{Name: "etag"},
	{Name: "expect"},
	{Name: "expires"},
	{Name: "from"},
	{Name: "host"},
	{Name: "if-match"},
	{Name: "if-modified-since"},
	{Name: "if-none-match"},
	{Name: "if-range"},
	{Name: "if-unmodified-since"},
	{Name: "last-modified"},
	{Name: "link"},
	{Name: "location"},
	{Name: "max-forwards"},
	{Name: "proxy-authenticate"},
	{Name: "proxy-authorization"},
	{Name: "range"},
	{Name: "referer"},
	{Name: "refresh"},
	{Name: "retry-after"},
	{Name: "server"},
	{Name: "set-cookie"},
	{Name: "strict-transport-security"},
	{Name: "transfer-encoding"},
	{Name: "user-agent"},
	{Name: "vary"},
	{Name: "via"},
	{Name: "www-authenticate"},
}

const (
	// maxHeaderFields bounds the decoded field count per header block.
	maxHeaderFields = 64
	// maxHeaderText bounds summed name+value bytes per header block.
	maxHeaderText = 8192
	// dynEntryMax bounds dynamic-table entry count alongside the byte cap.
	dynEntryMax = 256
	// dynEntryOverhead is the RFC 7541 §4.1 per-entry accounting (32).
	dynEntryOverhead = 32
)

// hpackDecoder is one connection's decode state: the dynamic table —
// newest entry first — and its byte accounting.
type hpackDecoder struct {
	dyn     []Field
	dynSize int
	maxSize int // current bound; ≤ dynTableMax, set by size updates
}

// decode runs one complete header block. A malformed integer, an index
// outside static+dynamic, a bound breach, or a bad Huffman string is the
// named ErrHeaderBlock refusal.
func (d *hpackDecoder) decode(block []byte) ([]Field, error) {
	if d.maxSize == 0 {
		d.maxSize = dynTableMax
	}
	var fields []Field
	text := 0
	pos := 0
	for pos < len(block) {
		if len(fields) >= maxHeaderFields {
			return nil, ErrHeaderBlock
		}
		b := block[pos]
		pos++
		var f Field
		switch {
		case b&0x80 != 0: // indexed field
			idx, err := hpackInt(b, block, &pos, 7)
			if err != nil {
				return nil, err
			}
			g, ok := d.lookup(int(idx))
			if !ok {
				return nil, ErrHeaderBlock
			}
			f = g
		case b&0xc0 == 0x40: // literal with incremental indexing
			idx, err := hpackInt(b, block, &pos, 6)
			if err != nil {
				return nil, err
			}
			name, err := d.fieldName(block, &pos, int(idx))
			if err != nil {
				return nil, err
			}
			value, err := hpackString(block, &pos)
			if err != nil {
				return nil, err
			}
			f = Field{Name: name, Value: value}
			d.insert(f)
		case b&0xe0 == 0x20: // dynamic table size update
			sz, err := hpackInt(b, block, &pos, 5)
			if err != nil {
				return nil, err
			}
			if sz > dynTableMax {
				return nil, ErrHeaderBlock
			}
			d.maxSize = int(sz)
			d.evict()
		default: // literal without indexing (0x00) / never indexed (0x10)
			idx, err := hpackInt(b, block, &pos, 4)
			if err != nil {
				return nil, err
			}
			name, err := d.fieldName(block, &pos, int(idx))
			if err != nil {
				return nil, err
			}
			value, err := hpackString(block, &pos)
			if err != nil {
				return nil, err
			}
			f = Field{Name: name, Value: value}
		}
		text += len(f.Name) + len(f.Value)
		if text > maxHeaderText {
			return nil, ErrHeaderBlock
		}
		fields = append(fields, f)
	}
	return fields, nil
}

// lookup resolves a 1-based index: 1..61 static, 62.. dynamic (dyn[0]
// newest). Index 0 or a past-the-end dynamic index fails.
func (d *hpackDecoder) lookup(idx int) (Field, bool) {
	if idx <= 0 {
		return Field{}, false
	}
	if idx <= len(staticTable) {
		return staticTable[idx-1], true
	}
	i := idx - len(staticTable) - 1
	if i >= len(d.dyn) {
		return Field{}, false
	}
	return d.dyn[i], true
}

// fieldName resolves a literal's name: an index reuses the table name
// (its value is discarded); index 0 means a literal name follows.
func (d *hpackDecoder) fieldName(block []byte, pos *int, idx int) (string, error) {
	if idx == 0 {
		return hpackString(block, pos)
	}
	f, ok := d.lookup(idx)
	if !ok {
		return "", ErrHeaderBlock
	}
	return f.Name, nil
}

// insert adds a field at the head of the dynamic table, evicting the tail
// to the byte bound; an entry larger than the bound empties the table and
// is not inserted (RFC 7541 §4.4).
func (d *hpackDecoder) insert(f Field) {
	entry := len(f.Name) + len(f.Value) + dynEntryOverhead
	if entry > d.maxSize {
		d.dyn = nil
		d.dynSize = 0
		return
	}
	d.dyn = append([]Field{f}, d.dyn...)
	d.dynSize += entry
	d.evict()
}

func (d *hpackDecoder) evict() {
	for d.dynSize > d.maxSize || len(d.dyn) > dynEntryMax {
		last := d.dyn[len(d.dyn)-1]
		d.dyn = d.dyn[:len(d.dyn)-1]
		d.dynSize -= len(last.Name) + len(last.Value) + dynEntryOverhead
	}
}

// hpackInt decodes an N-bit-prefix integer (RFC 7541 §5.1): first is the
// byte the caller already consumed carrying the representation bits; the
// prefix's low N bits hold the value or the continuation marker. A
// continuation longer than five octets is refused — nothing legitimately
// indexes past 2^35 in a bounded table.
func hpackInt(first byte, block []byte, pos *int, prefix uint) (uint64, error) {
	mask := uint64(1<<prefix) - 1
	v := uint64(first) & mask
	if v < mask {
		return v, nil
	}
	m := uint(0)
	for i := 0; i < 5; i++ {
		if *pos >= len(block) {
			return 0, ErrHeaderBlock
		}
		c := block[*pos]
		*pos += 1
		v += uint64(c&0x7f) << m
		m += 7
		if c&0x80 == 0 {
			return v, nil
		}
	}
	return 0, ErrHeaderBlock
}

// hpackString decodes a string literal: H bit + 7-bit-prefix length +
// bytes, Huffman-decoded when H is set (RFC 7541 §5.2). A length past the
// block's end refuses rather than misaligning the rest of the block.
func hpackString(block []byte, pos *int) (string, error) {
	if *pos >= len(block) {
		return "", ErrHeaderBlock
	}
	first := block[*pos]
	*pos += 1
	n, err := hpackInt(first, block, pos, 7)
	if err != nil {
		return "", err
	}
	if n > uint64(len(block)-*pos) {
		return "", ErrHeaderBlock
	}
	raw := block[*pos : *pos+int(n)]
	*pos += int(n)
	if first&0x80 == 0 {
		return string(raw), nil
	}
	dec, err := huffmanDecode(raw)
	if err != nil {
		return "", ErrHeaderBlock
	}
	return string(dec), nil
}

// encodeRequest renders the one request header shape the fetch path
// sends: indexed static entries where the table carries an exact match,
// literal-without-indexing for :authority and :path — no dynamic-table
// writes, no Huffman, so the block is byte-deterministic. extra fields
// (the consumers' h1 parity headers) ride as literal-without-indexing
// literal names; a name that is not a lowercase token or a value carrying
// a control byte is refused by name — the h2 grammar forbids uppercase
// names outright (RFC 9113 §8.2.1) and the h1 hygiene rules still apply.
func encodeRequest(authority, path string, extra []Field) ([]byte, error) {
	dst := appendHpackIndex(nil, 7, 0x80, 2) // :method GET
	dst = appendHpackIndex(dst, 7, 0x80, 7)  // :scheme https
	// :authority — literal, indexed name (static index 1).
	dst = appendHpackIndex(dst, 4, 0x00, 1)
	dst = appendHpackString(dst, authority)
	// :path — literal, indexed name (static index 4).
	dst = appendHpackIndex(dst, 4, 0x00, 4)
	dst = appendHpackString(dst, path)
	for _, f := range extra {
		if !validFieldName(f.Name) || !validFieldValue(f.Value) {
			return nil, ErrHeaderBlock
		}
		dst = appendHpackIndex(dst, 4, 0x00, 0) // literal, new name
		dst = appendHpackString(dst, f.Name)
		dst = appendHpackString(dst, f.Value)
	}
	return dst, nil
}

// validFieldName is the h2 request-field bound: a nonempty lowercase
// token (RFC 9113 §8.2.1 fields are lowercase by definition).
func validFieldName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
			c == '!' || c == '#' || c == '$' || c == '%' || c == '&' ||
			c == '\'' || c == '*' || c == '+' || c == '-' || c == '.' ||
			c == '^' || c == '_' || c == '`' || c == '|' || c == '~') {
			return false
		}
	}
	return true
}

// validFieldValue refuses the bytes that could not ride an HTTP/1.1
// field — NUL, CR, LF — so the two engines keep one hygiene bar.
func validFieldValue(value string) bool {
	for _, c := range value {
		if c == 0 || c == '\r' || c == '\n' {
			return false
		}
	}
	return true
}

// appendHpackInt appends an N-bit-prefix integer whose first byte already
// carries rep bits.
func appendHpackIndex(dst []byte, prefix uint, rep byte, v uint64) []byte {
	mask := uint64(1<<prefix) - 1
	if v < mask {
		return append(dst, rep|byte(v))
	}
	dst = append(dst, rep|byte(mask))
	v -= mask
	for v >= 128 {
		dst = append(dst, byte(v&0x7f)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

// appendHpackString appends a string literal without Huffman coding.
func appendHpackString(dst []byte, s string) []byte {
	dst = appendHpackIndex(dst, 7, 0x00, uint64(len(s)))
	return append(dst, s...)
}
