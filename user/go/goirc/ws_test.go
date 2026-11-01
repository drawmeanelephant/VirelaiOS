package main

// Host tests for the M212c (#3874) ws gateway transport: wsLineConn's
// line↔message adapter over a scripted pipe — the codec itself is
// covered in virelai/ws's own tests; this is the consumer's half of the
// contract (one IRC line per text message, control frames invisible).

import (
	"encoding/base64"
	"strings"
	"testing"

	"virelai/ws"
)

// fakeWire is the WireConn under the test's ws.Conn — canned reads,
// captured writes.
type fakeWire struct {
	rx   []byte
	tx   strings.Builder
	dl   int64
	shut bool
}

func (f *fakeWire) Read(b []byte) (int, error) {
	if len(f.rx) == 0 {
		return 0, ircError("ETIMEDOUT")
	}
	n := copy(b, f.rx)
	f.rx = f.rx[n:]
	return n, nil
}

func (f *fakeWire) Write(b []byte) (int, error) {
	f.tx.Write(b)
	return len(b), nil
}

func (f *fakeWire) SetReadDeadline(at int64) { f.dl = at }
func (f *fakeWire) Close() error             { f.shut = true; return nil }

type wsFrame struct {
	op      byte
	payload string
}

// wsFrames decodes the masked client frames the adapter emitted.
func wsFrames(t *testing.T, raw []byte) []wsFrame {
	t.Helper()
	var out []wsFrame
	for len(raw) > 0 {
		if len(raw) < 2 {
			t.Fatalf("short frame head: %x", raw)
		}
		op := raw[0] & 0x0F
		n := int(raw[1] & 0x7F)
		off := 2
		switch n {
		case 126:
			n = int(raw[2])<<8 | int(raw[3])
			off = 4
		case 127:
			n = 0
			for i := 0; i < 8; i++ {
				n = n<<8 | int(raw[off+i])
			}
			off += 8
		}
		if raw[1]&0x80 == 0 {
			t.Fatal("client frame unmasked")
		}
		key := raw[off : off+4]
		off += 4
		payload := make([]byte, n)
		for i := 0; i < n; i++ {
			payload[i] = raw[off+i] ^ key[i%4]
		}
		out = append(out, wsFrame{op: op, payload: string(payload)})
		raw = raw[off+n:]
	}
	return out
}

// sframe is the unmasked server frame builder (same shape as ws's test).
func sframe(fin bool, op byte, payload []byte) []byte {
	b := []byte{op}
	if fin {
		b[0] |= 0x80
	}
	if len(payload) < 126 {
		b = append(b, byte(len(payload)))
	} else {
		b = append(b, 126, byte(len(payload)>>8), byte(len(payload)))
	}
	return append(b, payload...)
}

// wsNonce is the fixed client key wsLineForTest's Rand fills.
var wsNonce [16]byte

func init() {
	for i := range wsNonce {
		wsNonce[i] = 0x55
	}
}

// head101 answers the fixed-nonce handshake.
func head101() string {
	return "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + ws.Accept(base64.StdEncoding.EncodeToString(wsNonce[:])) + "\r\n\r\n"
}

// wsLineForTest runs the handshake then clears the capture so emitted
// frames are readable on their own.
func wsLineForTest(t *testing.T, w *fakeWire) *wsLineConn {
	t.Helper()
	c, err := ws.Dial(w, ws.Options{
		Host: "gw:443",
		Path: "/irc",
		Rand: func(p []byte) (int, error) {
			for i := range p {
				p[i] = 0x55
			}
			return len(p), nil
		},
	})
	if err != nil {
		t.Fatalf("ws.Dial: %v", err)
	}
	w.tx.Reset()
	return &wsLineConn{conn: c, wire: w}
}

func TestWSLineWriteSplitsLines(t *testing.T) {
	fw := &fakeWire{rx: []byte(head101())}
	w := wsLineForTest(t, fw)
	if _, err := w.Write([]byte("NICK gate\r\nUSER u 0 * :gate user\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := w.Write([]byte("JOIN #goirc\r\n")); err != nil {
		t.Fatalf("Write2: %v", err)
	}
	var lines []string
	for _, f := range wsFrames(t, []byte(fw.tx.String())) {
		if f.op != ws.OpText {
			t.Fatalf("non-text frame emitted: op=%d", f.op)
		}
		lines = append(lines, f.payload)
	}
	want := []string{"NICK gate", "USER u 0 * :gate user", "JOIN #goirc"}
	if len(lines) != len(want) {
		t.Fatalf("lines: %v", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d: %q want %q", i, lines[i], want[i])
		}
	}
}

func TestWSLineReadDecodesMessages(t *testing.T) {
	fw := &fakeWire{rx: []byte(head101() +
		string(sframe(true, ws.OpText, []byte(":srv 001 gate :Welcome"))) +
		string(sframe(true, ws.OpPing, []byte("x"))) +
		string(sframe(true, ws.OpText, []byte(":srv PONG srv :goirc"))))}
	w := wsLineForTest(t, fw)
	var got strings.Builder
	buf := make([]byte, 16)
	for !strings.HasSuffix(got.String(), ":goirc\n") {
		n, err := w.Read(buf)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		got.Write(buf[:n])
	}
	s := got.String()
	if !strings.Contains(s, ":srv 001 gate :Welcome\n") ||
		!strings.Contains(s, ":srv PONG srv :goirc\n") {
		t.Fatalf("decoded stream: %q", s)
	}
	// The ping earned a masked pong on the wire.
	ponged := false
	for _, f := range wsFrames(t, []byte(fw.tx.String())) {
		if f.op == ws.OpPong {
			ponged = true
		}
	}
	if !ponged {
		t.Fatal("server ping got no masked pong")
	}
}

func TestWSLineCloseEndsReads(t *testing.T) {
	fw := &fakeWire{rx: []byte(head101() +
		string(sframe(true, ws.OpClose, []byte{0x03, 0xE8})))}
	w := wsLineForTest(t, fw)
	if _, err := w.Read(make([]byte, 16)); err != ws.ErrClosed {
		t.Fatalf("Read after close = %v, want ws.ErrClosed", err)
	}
}
