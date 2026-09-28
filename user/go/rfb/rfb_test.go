package rfb

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// The file hashes pin independently authored wire vectors. Comparing codec
// output to a file without checking its hash would let an accidental fixture
// rewrite silently redefine the protocol.
var vectors = map[string]string{
	"rfb-handshake-38.bin":        "4ad0b6444c429ac78aaf5da78e5e42cc6ce51d08475216f1230f3c01160e6c1d",
	"rfb-handshake-37.bin":        "0aa756ee38d55d460f41e9f9e40c383a2de2b338588c1f0da57f1d34d60218b9",
	"rfb-handshake-33.bin":        "2ea025afe336c6c5394c6c5364b61e7f58717260ddae95c8daf3d153c9be7d41",
	"rfb-refusal-security.bin":    "93dc1e2ec950f44628c56e090e604f655d9edab76e763e5fdfe0a445249205ee",
	"rfb-update-raw.bin":          "d9e07540b41b0b141ad6cfba7d7be73460a7525c3a702989634a2f4a9aef9901",
	"rfb-update-rre.bin":          "0261bdccefbe1c553ffa0f0c4f7106d576ce97aa57611616f8f8894fbae8c64d",
	"rfb-update-hextile.bin":      "245c357383cba9c532bfe0d4dc9d8ce955a35818ad18571c9d876d82b1c3ca37",
	"rfb-update-incremental.bin":  "5abd5026a2b42a255a3d89d391a893e806ea2e880b87150f6214de4736869de9",
	"rfb-update-resize.bin":       "e87c6019947a06ceddaa809e1aaf8fd925e91b5bdd9f40a1edc4e289b0d53e4b",
	"rfb-update-cursor.bin":       "9d438f313e8189af159532c9c2aeddb96011a4b9c993862436ba8b75a409972c",
	"rfb-malformed-encodings.bin": "d2f19a5141079961c7cf43d0f96a210ff1bb39b42bac40086d68d7145ce583ab",
	"rfb-malformed-format.bin":    "48b4b6a99d17f8972f2507babd47b8daed0a0080721269380afad49b3dd8b82e",
	"rfb-malformed-request.bin":   "02f1aca15c6282c016d0981a2d842c96c58636421ee806242eb161da8fbc1f52",
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "tests", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := sha256.Sum256(b)
	if want, ok := vectors[name]; !ok || want != fmtHash(got) {
		t.Fatalf("%s sha256 %x, want %s", name, got, want)
	}
	return b
}

func fmtHash(sum [32]byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 64)
	for i, v := range sum {
		out[2*i], out[2*i+1] = digits[v>>4], digits[v&15]
	}
	return string(out)
}

func session(t *testing.T, client []byte, width, height uint16) (*Session, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	s, err := NewSession(bytes.NewReader(client), &out, width, height, "Virelai")
	if err != nil {
		t.Fatal(err)
	}
	return s, &out
}

func ready(t *testing.T, messages []byte, width, height uint16) (*Session, *bytes.Buffer) {
	t.Helper()
	client := append([]byte("RFB 003.008\n\x01\x01"), messages...)
	s, out := session(t, client, width, height)
	if _, err := s.Handshake(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	return s, out
}

func checkFixture(t *testing.T, name string, got []byte) {
	t.Helper()
	if want := fixture(t, name); !bytes.Equal(got, want) {
		t.Fatalf("%s\n got %x\nwant %x", name, got, want)
	}
}

func TestHandshakeVectors(t *testing.T) {
	for _, tc := range []struct {
		name, version string
		selection     string
		shared        bool
	}{
		{"rfb-handshake-38.bin", "008", "\x01", true},
		{"rfb-handshake-37.bin", "007", "\x01", true},
		{"rfb-handshake-33.bin", "003", "", false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			shared := "\x00"
			if tc.shared {
				shared = "\x01"
			}
			s, out := session(t, []byte("RFB 003."+tc.version+"\n"+tc.selection+shared), 4, 2)
			got, err := s.Handshake()
			if err != nil || got != tc.shared {
				t.Fatalf("Handshake = %v, %v", got, err)
			}
			checkFixture(t, tc.name, out.Bytes())
			if _, err := s.Handshake(); !errors.Is(err, ErrProtocol) {
				t.Fatalf("repeated handshake: %v", err)
			}
		})
	}
}

func TestRefusalVector(t *testing.T) {
	s, out := session(t, []byte("RFB 003.008\n\x02"), 4, 2)
	if _, err := s.Handshake(); !errors.Is(err, ErrProtocol) {
		t.Fatalf("security type 2: %v", err)
	}
	checkFixture(t, "rfb-refusal-security.bin", out.Bytes())
}

func TestMalformedVectors(t *testing.T) {
	for _, name := range []string{
		"rfb-malformed-encodings.bin",
		"rfb-malformed-format.bin",
		"rfb-malformed-request.bin",
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := ready(t, fixture(t, name), 4, 2)
			if _, err := s.ReadMessage(); !errors.Is(err, ErrProtocol) {
				t.Fatalf("ReadMessage = %v, want protocol refusal", err)
			}
			if _, err := s.ReadMessage(); !errors.Is(err, ErrProtocol) {
				t.Fatalf("read after refusal = %v, want closed session", err)
			}
		})
	}
}

func testFrame() []byte {
	// Seven blue pixels and one green pixel; source X bytes must not leak.
	frame := bytes.Repeat([]byte{255, 0, 0, 0xa5}, 8)
	copy(frame[4:8], []byte{0, 255, 0, 0xbe})
	return frame
}

func encodings(codes ...int32) []byte {
	b := []byte{2, 0, byte(len(codes) >> 8), byte(len(codes))}
	for _, c := range codes {
		b = append32(b, uint32(c))
	}
	return b
}

func request(incremental bool, x, y, w, h uint16) []byte {
	b := []byte{3, 0}
	if incremental {
		b[1] = 1
	}
	b = append16(b, x)
	b = append16(b, y)
	b = append16(b, w)
	return append16(b, h)
}

func TestUpdateVectors(t *testing.T) {
	frame := testFrame()
	for _, tc := range []struct {
		code int32
		name string
	}{
		{EncodingRaw, "rfb-update-raw.bin"},
		{EncodingRRE, "rfb-update-rre.bin"},
		{EncodingHextile, "rfb-update-hextile.bin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, out := ready(t, append(encodings(tc.code), request(false, 0, 0, 4, 2)...), 4, 2)
			if _, err := s.ReadMessage(); err != nil {
				t.Fatal(err)
			}
			m, err := s.ReadMessage()
			if err != nil || m.Kind != UpdateRequest {
				t.Fatalf("ReadMessage = %+v, %v", m, err)
			}
			sent, err := s.FramebufferUpdate(m.Request, frame)
			if !sent || err != nil {
				t.Fatalf("FramebufferUpdate = %v, %v", sent, err)
			}
			checkFixture(t, tc.name, out.Bytes())
		})
	}
}

func TestIncrementalPixelsAndCache(t *testing.T) {
	frame := testFrame()
	s, out := ready(t, nil, 4, 2)
	full := Request{Rectangle: Rectangle{Width: 4, Height: 2}}
	if sent, err := s.FramebufferUpdate(full, frame); !sent || err != nil {
		t.Fatalf("full = %v, %v", sent, err)
	}
	out.Reset()
	partial := Request{Incremental: true, Rectangle: Rectangle{X: 1, Width: 3, Height: 2}}
	if sent, err := s.FramebufferUpdate(partial, frame); sent || err != nil || out.Len() != 0 {
		t.Fatalf("unchanged = %v, %v, %x", sent, err, out.Bytes())
	}
	// X is not color data.
	frame[7] ^= 0xff
	if sent, _ := s.FramebufferUpdate(partial, frame); sent {
		t.Fatal("source X changed the update")
	}
	// A change outside the requested area cannot make this request dirty.
	frame[0] = 0
	if sent, _ := s.FramebufferUpdate(partial, frame); sent {
		t.Fatal("change outside request leaked into update")
	}
	frame[6*4], frame[6*4+2] = 0, 255
	if sent, err := s.FramebufferUpdate(partial, frame); !sent || err != nil {
		t.Fatalf("incremental = %v, %v", sent, err)
	}
	checkFixture(t, "rfb-update-incremental.bin", out.Bytes())
	out.Reset()
	if sent, _ := s.FramebufferUpdate(partial, frame); sent {
		t.Fatal("sent the same rectangle twice")
	}
	if sent, err := s.FramebufferUpdate(Request{Incremental: true, Rectangle: Rectangle{Width: 1, Height: 1}}, frame); !sent || err != nil {
		t.Fatalf("outside change was marked sent: %v, %v", sent, err)
	}
}

func TestUnsentPixelsAndDirtyTileBound(t *testing.T) {
	s, out := ready(t, nil, 32, 16)
	frame := bytes.Repeat([]byte{0, 0, 0, 0}, 32*16)
	left := Request{Rectangle: Rectangle{Width: 16, Height: 16}}
	if sent, err := s.FramebufferUpdate(left, frame); !sent || err != nil {
		t.Fatal(err)
	}
	out.Reset()
	right := Request{Incremental: true, Rectangle: Rectangle{X: 16, Width: 16, Height: 16}}
	if sent, err := s.FramebufferUpdate(right, frame); !sent || err != nil {
		t.Fatalf("never-sent right half = %v, %v", sent, err)
	}
	if binary.BigEndian.Uint16(out.Bytes()[2:4]) != 1 ||
		binary.BigEndian.Uint16(out.Bytes()[4:6]) != 16 {
		t.Fatalf("dirty tile rectangle: %x", out.Bytes()[:16])
	}
}

func TestPixelFormatsAndMessages(t *testing.T) {
	p := PixelFormat{BitsPerPixel: 16, Depth: 16, TrueColor: 1,
		RedMax: 31, GreenMax: 63, BlueMax: 31, RedShift: 11, GreenShift: 5}
	pix := p.wire()
	setFormat := append([]byte{0, 0, 0, 0}, pix[:]...)
	messages := append(setFormat, encodings(123456, EncodingDesktopSize, EncodingRichCursor, EncodingHextile, EncodingRRE)...)
	messages = append(messages, request(true, 1, 0, 3, 2)...)
	messages = append(messages, 4, 1, 0, 0, 0, 0, 0, 'A')
	messages = append(messages, 4, 0, 0, 0, 0, 0, 0, 'A')
	messages = append(messages, 4, 1, 0, 0, 0x01, 0, 0, 0x01) // unknown keysym
	messages = append(messages, 5, 3, 0, 2, 0, 1)
	s, _ := ready(t, messages, 4, 2)
	if m, err := s.ReadMessage(); err != nil || m.Kind != SetPixelFormat || m.Format != p {
		t.Fatalf("pixel format = %+v, %v", m, err)
	}
	if m, err := s.ReadMessage(); err != nil || m.Kind != SetEncodings ||
		s.preferred != EncodingHextile || !s.desktopSize || !s.richCursor {
		t.Fatalf("encodings = %+v, %v", m, err)
	}
	if m, err := s.ReadMessage(); err != nil || !m.Request.Incremental ||
		m.Request.Rectangle != (Rectangle{1, 0, 3, 2}) {
		t.Fatalf("request = %+v, %v", m, err)
	}
	if m, err := s.ReadMessage(); err != nil || !m.Key.Down ||
		!m.Key.Known || m.Key.Usage != 0x04 {
		t.Fatalf("key down = %+v, %v", m, err)
	}
	if m, err := s.ReadMessage(); err != nil || m.Key.Down || !m.Key.Known {
		t.Fatalf("key up = %+v, %v", m, err)
	}
	if m, err := s.ReadMessage(); err != nil || m.Key.Known || m.Key.Usage != 0 {
		t.Fatalf("unknown key = %+v, %v", m, err)
	}
	if m, err := s.ReadMessage(); err != nil || m.Pointer != (PointerEvent{3, 2, 1}) {
		t.Fatalf("pointer = %+v, %v", m, err)
	}
	var out []byte
	out = s.pixel(out, 0xff0000)
	if !bytes.Equal(out, []byte{0, 0xf8}) {
		t.Fatalf("RGB565 little endian = %x", out)
	}
	p.BigEndian = 1
	s.Format = p
	if got := s.pixel(nil, 0xff0000); !bytes.Equal(got, []byte{0xf8, 0}) {
		t.Fatalf("RGB565 big endian = %x", got)
	}
}

func TestPseudoEncodingVectors(t *testing.T) {
	s, out := ready(t, encodings(EncodingDesktopSize, EncodingRichCursor), 4, 2)
	if err := s.DesktopSize(2, 2); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unadvertised size: %v", err)
	}
	if err := s.RichCursor(Cursor{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unadvertised cursor: %v", err)
	}
	if _, err := s.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if err := s.DesktopSize(2, 2); err != nil {
		t.Fatal(err)
	}
	checkFixture(t, "rfb-update-resize.bin", out.Bytes())
	if s.Width != 2 || s.Height != 2 {
		t.Fatal("size not updated")
	}
	out.Reset()
	cursor := Cursor{
		HotX: 1, Width: 2, Height: 1,
		Pixels: []byte{255, 0, 0, 0xa5, 0, 255, 0, 0xbe},
		Mask:   []byte{0xc0},
	}
	if err := s.RichCursor(cursor); err != nil {
		t.Fatal(err)
	}
	checkFixture(t, "rfb-update-cursor.bin", out.Bytes())
	if err := s.RichCursor(Cursor{Width: 65, Height: 1}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("oversize cursor: %v", err)
	}
	if err := s.DesktopSize(0, 2); !errors.Is(err, ErrProtocol) {
		t.Fatalf("zero width: %v", err)
	}
}

func TestEncodingsAndBounds(t *testing.T) {
	if _, err := NewSession(nil, &bytes.Buffer{}, 1, 1, ""); !errors.Is(err, ErrProtocol) {
		t.Fatal(err)
	}
	if _, err := NewSession(bytes.NewReader(nil), &bytes.Buffer{}, 1281, 1, ""); !errors.Is(err, ErrProtocol) {
		t.Fatal(err)
	}
	if _, err := NewSession(bytes.NewReader(nil), &bytes.Buffer{}, 1, 1, string(bytes.Repeat([]byte{'a'}, 256))); !errors.Is(err, ErrProtocol) {
		t.Fatal(err)
	}
	s, _ := ready(t, encodings(EncodingRRE, EncodingRaw), 4, 2)
	if _, err := s.ReadMessage(); err != nil || s.preferred != EncodingRRE {
		t.Fatalf("preferred encoding = %d, %v", s.preferred, err)
	}
	if _, err := s.FramebufferUpdate(Request{Rectangle: Rectangle{Width: 4, Height: 2}}, []byte{0}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("bad frame size: %v", err)
	}
	if _, err := s.FramebufferUpdate(Request{Rectangle: Rectangle{X: 3, Width: 2, Height: 1}}, testFrame()); !errors.Is(err, ErrProtocol) {
		t.Fatalf("bad rectangle: %v", err)
	}
	// SetEncodings is a replacement, not additive: a client withdrawing
	// a pseudo-encoding must stop receiving it immediately.
	s, _ = ready(t, append(encodings(EncodingDesktopSize, EncodingRichCursor), encodings(EncodingRaw)...), 4, 2)
	if _, err := s.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if s.desktopSize || s.richCursor || s.preferred != EncodingRaw {
		t.Fatal("stale pseudo-encoding advertisement")
	}
}

func TestMalformedAndShortStreams(t *testing.T) {
	for _, client := range [][]byte{
		[]byte("RFB 003.009\n"), []byte("RFB 004.008\n"),
		[]byte("RFB 003.008\n\x01\x02"),
		[]byte("RFB 003.008\n\x01\x03"),
	} {
		s, _ := session(t, client, 4, 2)
		if _, err := s.Handshake(); !errors.Is(err, ErrProtocol) {
			t.Fatalf("bad handshake %q: %v", client, err)
		}
	}
	for i := 0; i < 12; i++ {
		s, _ := session(t, []byte("RFB 003.008\n")[:i], 4, 2)
		if _, err := s.Handshake(); err == nil {
			t.Fatalf("accepted short banner of length %d", i)
		}
	}
	pf := DefaultPixelFormat.wire()
	messages := [][]byte{
		encodings(EncodingHextile),
		request(true, 0, 0, 4, 2),
		append([]byte{0, 0, 0, 0}, pf[:]...),
		{4, 1, 0, 0, 0, 0, 0, 'a'},
		{5, 0, 0, 0, 0, 0},
	}
	for _, msg := range messages {
		for i := 0; i < len(msg); i++ {
			s, _ := ready(t, msg[:i], 4, 2)
			if _, err := s.ReadMessage(); err == nil {
				t.Fatalf("accepted truncated %x at %d", msg, i)
			}
		}
	}
	for _, msg := range [][]byte{
		{6}, {3, 2, 0, 0, 0, 0, 0, 4, 0, 2},
		{4, 2, 0, 0, 0, 0, 0, 'a'}, {5, 0, 0, 4, 0, 0},
		{3, 1, 0, 0, 0, 0, 0, 0, 0, 2},
	} {
		s, _ := ready(t, msg, 4, 2)
		if _, err := s.ReadMessage(); !errors.Is(err, ErrProtocol) {
			t.Fatalf("accepted malformed %x: %v", msg, err)
		}
	}
}

type failingWriter struct{ n int }

func (w *failingWriter) Write(b []byte) (int, error) {
	if w.n == 0 {
		return 0, io.ErrClosedPipe
	}
	n := min(w.n, len(b))
	w.n -= n
	return n, nil
}

func TestPartialWritesDoNotCommitCache(t *testing.T) {
	s, _ := ready(t, nil, 4, 2)
	s.w = &failingWriter{n: 5}
	if _, err := s.FramebufferUpdate(Request{Rectangle: Rectangle{Width: 4, Height: 2}}, testFrame()); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("failed update: %v", err)
	}
	if s.last != nil || s.known != nil || s.ready {
		t.Fatal("partial update was committed or session remained usable")
	}
}

func TestKeysymToHID(t *testing.T) {
	for _, tc := range []struct {
		key   uint32
		usage uint8
	}{
		{'a', 0x04}, {'A', 0x04}, {'!', 0x1e}, {'0', 0x27},
		{0xff0d, 0x28}, {0xff51, 0x50}, {0xffe1, 0xe1},
		{0xffe3, 0xe0}, {0xffbe, 0x3a}, {0xffd5, 0x73},
		{0xffb0, 0x62},
	} {
		if usage, ok := KeysymToHID(tc.key); !ok || usage != tc.usage {
			t.Errorf("keysym %#x: usage %#x, known %v", tc.key, usage, ok)
		}
	}
	if _, ok := KeysymToHID(0x0101f600); ok {
		t.Fatal("unmapped Unicode keysym became a physical key")
	}
}

func TestRREFallbackAndHextileTiles(t *testing.T) {
	s, out := ready(t, encodings(EncodingRRE), 16, 16)
	if _, err := s.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 16*16*4)
	for i := 0; i < 16*16; i++ {
		frame[i*4] = byte(i)
	}
	if _, err := s.FramebufferUpdate(Request{Rectangle: Rectangle{Width: 16, Height: 16}}, frame); err != nil {
		t.Fatal(err)
	}
	if got := int32(binary.BigEndian.Uint32(out.Bytes()[12:16])); got != EncodingRaw {
		t.Fatalf("incompressible RRE used %d", got)
	}
	s, out = ready(t, encodings(EncodingHextile), 16, 16)
	if _, err := s.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FramebufferUpdate(Request{Rectangle: Rectangle{Width: 16, Height: 16}}, frame); err != nil {
		t.Fatal(err)
	}
	if out.Bytes()[16] != 1 {
		t.Fatalf("incompressible hextile did not use raw tile: %x", out.Bytes()[16])
	}
	// Three colors force hextile's colored-subrectangle bit.
	frame = bytes.Repeat([]byte{255, 0, 0, 0}, 16*16)
	frame[4], frame[6] = 0, 255
	frame[8], frame[9] = 0, 255
	out.Reset()
	if _, err := s.FramebufferUpdate(Request{Rectangle: Rectangle{Width: 16, Height: 16}}, frame); err != nil {
		t.Fatal(err)
	}
	if out.Bytes()[16] != 2|8|16 {
		t.Fatalf("colored hextile subrect: %x", out.Bytes()[16])
	}
}

func TestHextileTileEdgesAndByteOrder(t *testing.T) {
	s, out := ready(t, encodings(EncodingHextile), 18, 18)
	if _, err := s.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	frame := bytes.Repeat([]byte{255, 0, 0, 0}, 18*18)
	// The bottom-right partial tile is a different color. It must have
	// its own background even though the preceding three tiles are blue.
	for y := 16; y < 18; y++ {
		for x := 16; x < 18; x++ {
			copy(frame[(y*18+x)*4:], []byte{0, 255, 0, 0})
		}
	}
	if _, err := s.FramebufferUpdate(Request{Rectangle: Rectangle{Width: 18, Height: 18}}, frame); err != nil {
		t.Fatal(err)
	}
	want := appendRect([]byte{0, 0, 0, 1}, Rectangle{Width: 18, Height: 18}, EncodingHextile)
	blueTile := []byte{2, 255, 0, 0, 0}
	for range 3 {
		want = append(want, blueTile...)
	}
	want = append(want, 2, 0, 255, 0, 0)
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("partial hextile tile = %x, want %x", out.Bytes(), want)
	}

	// A valid 8-bit true-color format is also a client choice, not a
	// hard-coded assumption that every client can accept B,G,R,0.
	p := PixelFormat{BitsPerPixel: 8, Depth: 8, TrueColor: 1,
		RedMax: 7, GreenMax: 7, BlueMax: 3,
		RedShift: 5, GreenShift: 2, BlueShift: 0}
	if !p.valid() {
		t.Fatal("valid RGB332 refused")
	}
	s.Format = p
	if got := s.pixel(nil, 0xff8000); !bytes.Equal(got, []byte{0xf0}) {
		t.Fatalf("RGB332 red + half green = %x", got)
	}
}
