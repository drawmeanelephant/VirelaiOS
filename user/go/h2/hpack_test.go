// HPACK pins: the RFC 7541 appendix-C request examples (plain and
// Huffman), dynamic-table indexing across decodes, the size-update
// bound, and the refusal grammar (past-the-end index, truncated block,
// bad Huffman tail).

package h2

import (
	"errors"
	"testing"
)

func decodeHex(t *testing.T, hex string) []byte {
	var b []byte
	for i := 0; i+1 < len(hex); i += 2 {
		var v byte
		for _, c := range []byte{hex[i], hex[i+1]} {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= c - '0'
			case c >= 'a' && c <= 'f':
				v |= c - 'a' + 10
			default:
				t.Fatalf("bad hex %q", hex)
			}
		}
		b = append(b, v)
	}
	return b
}

func fieldEq(f Field, name, value string) bool { return f.Name == name && f.Value == value }

// RFC 7541 §C.3.1 — first request, no Huffman.
func TestRFC7541C31(t *testing.T) {
	d := &hpackDecoder{}
	fields, err := d.decode(decodeHex(t, "828684410f7777772e6578616d706c652e636f6d"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Field{
		{Name: ":method", Value: "GET"},
		{Name: ":scheme", Value: "http"},
		{Name: ":path", Value: "/"},
		{Name: ":authority", Value: "www.example.com"},
	}
	if len(fields) != len(want) {
		t.Fatalf("fields %v", fields)
	}
	for i := range want {
		if fields[i] != want[i] {
			t.Fatalf("field %d: %+v", i, fields[i])
		}
	}
	// The literal with indexing must have entered the dynamic table:
	// C.3.2's first byte (0xbe) indexes :authority from dyn.
	fields, err = d.decode(decodeHex(t, "828684be58086e6f2d6361636865"))
	if err != nil {
		t.Fatal(err)
	}
	if !fieldEq(fields[3], ":authority", "www.example.com") ||
		!fieldEq(fields[4], "cache-control", "no-cache") {
		t.Fatalf("fields %v", fields)
	}
}

// RFC 7541 §C.4.1 — same request with Huffman strings.
func TestRFC7541C41Huffman(t *testing.T) {
	d := &hpackDecoder{}
	fields, err := d.decode(decodeHex(t, "828684418cf1e3c2e5f23a6ba0ab90f4ff"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 || !fieldEq(fields[3], ":authority", "www.example.com") {
		t.Fatalf("fields %v", fields)
	}
	// C.4.2: :path is /index.html here; dyn carries the authority.
	fields, err = d.decode(decodeHex(t, "828685be5886a8eb10649cbf"))
	if err != nil {
		t.Fatal(err)
	}
	if !fieldEq(fields[2], ":path", "/index.html") || !fieldEq(fields[3], ":authority", "www.example.com") ||
		!fieldEq(fields[4], "cache-control", "no-cache") {
		t.Fatalf("fields %v", fields)
	}
}

// RFC 7541 §C.6.3 — full response header example, Huffman throughout.
func TestRFC7541C63Response(t *testing.T) {
	d := &hpackDecoder{maxSize: 256} // the example runs a 256-byte table
	fields, err := d.decode(decodeHex(t,
		"488264025885aec3771a4b6196d07abe941054d444a8200595040b8166e082a62d1bff"+
			"6e919d29ad171863c78f0b97c8e9ae82ae43d3"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Field{
		{Name: ":status", Value: "302"},
		{Name: "cache-control", Value: "private"},
		{Name: "date", Value: "Mon, 21 Oct 2013 20:13:21 GMT"},
		{Name: "location", Value: "https://www.example.com"},
	}
	if len(fields) != len(want) {
		t.Fatalf("fields %v", fields)
	}
	for i := range want {
		if fields[i] != want[i] {
			t.Fatalf("field %d: %+v want %+v", i, fields[i], want[i])
		}
	}
}

func TestDynamicTableSizeUpdateBound(t *testing.T) {
	d := &hpackDecoder{}
	// 0x20 + value: a size update to 0 is legal.
	if _, err := d.decode([]byte{0x20}); err != nil {
		t.Fatal(err)
	}
	if d.maxSize != 0 {
		t.Fatalf("maxSize %d", d.maxSize)
	}
	// Past the advertised cap refuses (0x3f 0xe2 0x1f = 4097 > 4096).
	if _, err := d.decode([]byte{0x3f, 0xe2, 0x1f}); !errors.Is(err, ErrHeaderBlock) {
		t.Fatalf("err %v", err)
	}
}

func TestIndexPastEndRefused(t *testing.T) {
	d := &hpackDecoder{}
	if _, err := d.decode([]byte{0xff, 0x00}); !errors.Is(err, ErrHeaderBlock) {
		t.Fatalf("err %v", err)
	}
	if _, err := d.decode([]byte{0x80}); !errors.Is(err, ErrHeaderBlock) {
		t.Fatalf("index 0 err %v", err)
	}
}

func TestTruncatedStringRefused(t *testing.T) {
	d := &hpackDecoder{}
	// Literal w/o indexing, name "x", then a value len that overruns.
	if _, err := d.decode([]byte{0x00, 0x01, 'x', 0x05, 'a'}); !errors.Is(err, ErrHeaderBlock) {
		t.Fatalf("err %v", err)
	}
}

func TestHuffmanBadTailRefused(t *testing.T) {
	// A single 0x00 byte is symbol '0' at 5 bits + a 3-bit 000 tail —
	// non-EOS padding.
	if _, err := huffmanDecode([]byte{0x00}); err == nil {
		t.Fatal("expected refusal")
	}
	// EOS appearing in the data stream (30 one-bits) must refuse.
	if _, err := huffmanDecode([]byte{0xff, 0xff, 0xff, 0xff}); err == nil {
		t.Fatal("expected EOS refusal")
	}
}
