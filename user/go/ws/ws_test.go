package ws

// Host tests for the M212c codec: the handshake checksum and head
// grammar are pure, the frame layer runs over a scripted Pipe (the
// httpc test discipline — the whole refusal grammar without a socket).

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// scriptPipe is a canned server: rx is what Read returns (chunked at
// step boundaries), tx collects what the client wrote.
type scriptPipe struct {
	rx    []byte
	tx    bytes.Buffer
	steps []int // optional chunk boundaries for rx drip-feeding
}

func (p *scriptPipe) Read(b []byte) (int, error) {
	if len(p.rx) == 0 {
		return 0, nil
	}
	n := len(b)
	if len(p.steps) > 0 {
		n = p.steps[0]
		p.steps = p.steps[1:]
		if n > len(b) {
			n = len(b)
		}
	}
	if n > len(p.rx) {
		n = len(p.rx)
	}
	copy(b, p.rx[:n])
	p.rx = p.rx[n:]
	return n, nil
}

func (p *scriptPipe) Write(b []byte) (int, error) {
	return p.tx.Write(b)
}

// eofPipe ends reads with io.EOF-class error after rx drains.
type eofPipe struct {
	scriptPipe
	err error
}

func (p *eofPipe) Read(b []byte) (int, error) {
	n, _ := p.scriptPipe.Read(b)
	if n == 0 {
		return 0, p.err
	}
	return n, nil
}

// ioEOF is the stand-in end-of-stream for the pure tests.
var errEOF = errorString("EOF")

type errorString string

func (e errorString) Error() string { return string(e) }

func fixedRand(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(0xA0 + i%13)
	}
	return len(p), nil
}

func testOpts() Options {
	var o Options
	o.Host = "gate.example:80"
	o.Path = "/irc"
	for i := range o.Key {
		o.Key[i] = byte(i + 1) // fixed nonce — the head builder needs it
	}
	o.Rand = fixedRand
	o.MaxMessage = 1024
	o.MaxFrame = 1024
	return o
}

// serverHead renders the 101 answer a correct peer gives testOpts' key.
func serverHead(o Options) string {
	return "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + Accept(base64.StdEncoding.EncodeToString(o.Key[:])) + "\r\n" +
		"\r\n"
}

// sframe builds an unmasked server frame.
func sframe(fin bool, op byte, payload []byte) []byte {
	b := []byte{op}
	if fin {
		b[0] |= 0x80
	}
	n := len(payload)
	switch {
	case n < 126:
		b = append(b, byte(n))
	case n <= 0xFFFF:
		b = append(b, 126, byte(n>>8), byte(n))
	default:
		b = append(b, 127, 0, 0, 0, 0, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	return append(b, payload...)
}

// smasked builds a MASKED server frame (the refusal case).
func smasked(op byte, payload []byte) []byte {
	b := []byte{0x80 | op}
	n := len(payload)
	switch {
	case n < 126:
		b = append(b, 0x80|byte(n))
	case n <= 0xFFFF:
		b = append(b, 0x80|126, byte(n>>8), byte(n))
	default:
		b = append(b, 0x80|127, 0, 0, 0, 0, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	b = append(b, 1, 2, 3, 4)
	for i, c := range payload {
		b = append(b, c^[]byte{1, 2, 3, 4}[i%4])
	}
	return b
}

func dial(t *testing.T, o Options, after string) (*Conn, *scriptPipe) {
	t.Helper()
	p := &scriptPipe{rx: []byte(serverHead(o) + after)}
	c, err := Dial(p, o)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return c, p
}

func TestHandshakeRequestShape(t *testing.T) {
	o := testOpts()
	p := &scriptPipe{rx: []byte(serverHead(o))}
	c, err := Dial(p, o)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if c == nil {
		t.Fatal("nil conn")
	}
	req := p.tx.String()
	for _, want := range []string{
		"GET /irc HTTP/1.1\r\n",
		"Host: gate.example:80\r\n",
		"Upgrade: websocket\r\n",
		"Connection: Upgrade\r\n",
		"Sec-WebSocket-Version: 13\r\n",
		"Sec-WebSocket-Key: ",
	} {
		if !strings.Contains(req, want) {
			t.Fatalf("request missing %q:\n%s", want, req)
		}
	}
	if strings.Contains(req, "Sec-WebSocket-Extensions") {
		t.Fatal("client offered an extension — the card refuses them")
	}
}

func TestHandshakeRefusals(t *testing.T) {
	o := testOpts()
	cases := []struct {
		name string
		head string
		want string
	}{
		{"status403", strings.Replace(serverHead(o), "101 Switching Protocols", "403 Forbidden", 1), string(RefuseStatus)},
		{"badaccept", strings.Replace(serverHead(o), Accept(base64.StdEncoding.EncodeToString(o.Key[:])), "AAAA", 1), string(RefuseAccept)},
		{"noupgrade", strings.Replace(serverHead(o), "Upgrade: websocket\r\n", "", 1), string(RefuseUpgradeHdr)},
		{"extension", strings.Replace(serverHead(o), "\r\n\r\n", "\r\nSec-WebSocket-Extensions: permessage-deflate\r\n\r\n", 1), string(RefuseExtension)},
		{"subprotocol", strings.Replace(serverHead(o), "\r\n\r\n", "\r\nSec-WebSocket-Protocol: irc.unoffered\r\n\r\n", 1), string(RefuseSubprotocol)},
	}
	for _, tc := range cases {
		p := &scriptPipe{rx: []byte(tc.head)}
		if _, err := Dial(p, o); err == nil || err.Error() != tc.want {
			t.Fatalf("%s: got %v want %s", tc.name, err, tc.want)
		}
	}
}

func TestSubprotocolAccepted(t *testing.T) {
	o := testOpts()
	o.Subprotocol = "irc"
	head := strings.Replace(serverHead(o), "\r\n\r\n", "\r\nSec-WebSocket-Protocol: irc\r\n\r\n", 1)
	p := &scriptPipe{rx: []byte(head)}
	if _, err := Dial(p, o); err != nil {
		t.Fatalf("Dial with matching subprotocol: %v", err)
	}
	if !strings.Contains(p.tx.String(), "Sec-WebSocket-Protocol: irc") {
		t.Fatal("request did not offer the token")
	}
}

func TestTextRoundTrip(t *testing.T) {
	c, p := dial(t, testOpts(), "")
	if err := c.Send(OpText, []byte("hello")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	frame := p.tx.Bytes()[len(p.tx.Bytes())-11:]
	if frame[0] != 0x81 || frame[1]&0x80 == 0 {
		t.Fatalf("client frame not fin+text or unmasked: %x", frame[:2])
	}
	if frame[1]&0x7F != 5 {
		t.Fatalf("len byte = %d", frame[1])
	}
}

func TestMaskedSendDecodes(t *testing.T) {
	c, p := dial(t, testOpts(), "")
	payload := []byte("mask-check")
	if err := c.Send(OpBinary, payload); err != nil {
		t.Fatalf("Send: %v", err)
	}
	f := p.tx.Bytes()
	f = f[len(f)-(2+4+len(payload)):]
	key := f[2:6]
	for i := 0; i < len(payload); i++ {
		if f[6+i]^key[i%4] != payload[i] {
			t.Fatalf("payload byte %d mis-masked", i)
		}
	}
}

func TestReadText(t *testing.T) {
	c, _ := dial(t, testOpts(), string(sframe(true, OpText, []byte("from-server"))))
	op, pl, err := c.ReadMessage()
	if err != nil || op != OpText || string(pl) != "from-server" {
		t.Fatalf("got op=%d pl=%q err=%v", op, pl, err)
	}
}

func TestFragmentReassembly(t *testing.T) {
	rx := string(sframe(false, OpText, []byte("frag-"))) +
		string(sframe(false, OpContinuation, []byte("ment-"))) +
		string(sframe(true, OpContinuation, []byte("tail")))
	c, _ := dial(t, testOpts(), rx)
	op, pl, err := c.ReadMessage()
	if err != nil || op != OpText || string(pl) != "frag-ment-tail" {
		t.Fatalf("got op=%d pl=%q err=%v", op, pl, err)
	}
}

func TestPingAutoPong(t *testing.T) {
	c, p := dial(t, testOpts(), string(sframe(true, OpPing, []byte("pg"))))
	op, pl, err := c.ReadMessage()
	if err != nil || op != OpPing || string(pl) != "pg" {
		t.Fatalf("got op=%d pl=%q err=%v", op, pl, err)
	}
	sent := p.tx.Bytes()
	last := sent[len(sent)-8:]
	if last[0] != 0x8A || last[1]&0x80 == 0 {
		t.Fatalf("no masked pong on the wire: %x", last[:2])
	}
	if last[1]&0x7F != 2 {
		t.Fatalf("pong len = %d", last[1])
	}
}

func TestCloseHandshake(t *testing.T) {
	body := []byte{0x03, 0xE8} // 1000
	c, p := dial(t, testOpts(), string(sframe(true, OpClose, body)))
	op, pl, err := c.ReadMessage()
	if err != nil || op != OpClose {
		t.Fatalf("got op=%d err=%v", op, err)
	}
	if !bytes.Equal(pl, body) || !c.PeerClosed() {
		t.Fatalf("close body %x PeerClosed=%v", pl, c.PeerClosed())
	}
	sent := p.tx.Bytes()
	last := sent[len(sent)-8:]
	if last[0] != 0x88 {
		t.Fatalf("no close answer on the wire: %x", last[:2])
	}
	if _, _, err := c.ReadMessage(); err != ErrClosed {
		t.Fatalf("post-close read = %v, want ErrClosed", err)
	}
	if err := c.Close(1000); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}
}

func TestClientInitiatedClose(t *testing.T) {
	c, p := dial(t, testOpts(), "")
	if err := c.Close(1000); err != nil {
		t.Fatalf("Close: %v", err)
	}
	sent := p.tx.Bytes()
	last := sent[len(sent)-8:]
	if last[0] != 0x88 || last[1]&0x80 == 0 {
		t.Fatalf("client close not masked fin: %x", last[:2])
	}
}

func TestRefusalGrammar(t *testing.T) {
	big := make([]byte, 70000)
	o := testOpts()
	cases := []struct {
		name string
		wire string
		want string
	}{
		{"masked server", string(smasked(OpText, []byte("x"))), string(RefuseServerMasked)},
		{"rsv set", string(sframe(true, OpText|0x40, []byte("x"))), string(RefuseRSV)},
		{"bad opcode", string(sframe(true, 0x3, []byte("x"))), string(RefuseOpcode)},
		{"stray cont", string(sframe(true, OpContinuation, []byte("x"))), string(RefuseStrayCont)},
		{"frag control", string(sframe(false, OpPing, []byte("x"))), string(RefuseControlFrag)},
		{"oversize frame", string(sframe(true, OpText, big)), string(RefuseFrameLarge)},
		{"declared oversize", "\x81\x7e\xff\xff", string(RefuseFrameLarge)},
		{"control oversize", string(sframe(true, OpPing, make([]byte, 126))), string(RefuseControlLarge)},
		{"bad close len", string(sframe(true, OpClose, []byte{7})), string(RefuseCloseBad)},
		{"bad close code", string(sframe(true, OpClose, []byte{0x03, 0xED})), string(RefuseCloseBad)}, // 1005
		{"interleaved", string(sframe(false, OpText, []byte("a"))) + string(sframe(true, OpText, []byte("b"))), string(RefuseFragmentMixed)},
	}
	for _, tc := range cases {
		p := &scriptPipe{rx: []byte(serverHead(o) + tc.wire)}
		c, err := Dial(p, o)
		if err != nil {
			t.Fatalf("%s: Dial: %v", tc.name, err)
		}
		_, _, err = c.ReadMessage()
		if err == nil || err.Error() != tc.want {
			t.Fatalf("%s: got %v want %s", tc.name, err, tc.want)
		}
	}
}

func TestMessageTooLarge(t *testing.T) {
	o := testOpts() // MaxMessage=1024, MaxFrame=1024
	half := make([]byte, 800)
	rx := string(sframe(false, OpBinary, half)) + string(sframe(false, OpContinuation, half))
	p := &scriptPipe{rx: []byte(serverHead(o) + rx)}
	c, err := Dial(p, o)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_, _, err = c.ReadMessage()
	if err == nil || err.Error() != string(RefuseMessageLarge) {
		t.Fatalf("got %v want %s", err, RefuseMessageLarge)
	}
}

func TestTruncatedFrame(t *testing.T) {
	o := testOpts()
	p := &eofPipe{scriptPipe: scriptPipe{rx: []byte(serverHead(o) + "\x82\x7e\x01\x00" + "short")}, err: errEOF}
	c, err := Dial(p, o)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_, _, err = c.ReadMessage()
	if err == nil || err != errEOF {
		// The transport's own error surfaces — no invented refusal.
		t.Fatalf("got %v want the transport's EOF", err)
	}
}

func TestReadDeadlinePassthrough(t *testing.T) {
	// A transport error mid-read passes through untouched — goirc's
	// liveness machine matches ETIMEDOUT by name.
	o := testOpts()
	p := &eofPipe{scriptPipe: scriptPipe{rx: []byte(serverHead(o))}, err: errorString("ETIMEDOUT")}
	c, err := Dial(p, o)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_, _, err = c.ReadMessage()
	if err == nil || err.Error() != "ETIMEDOUT" {
		t.Fatalf("got %v want ETIMEDOUT", err)
	}
}

func TestHeadLimit(t *testing.T) {
	o := testOpts()
	bloat := strings.Repeat("X-Pad: "+strings.Repeat("a", 900)+"\r\n", 12)
	p := &scriptPipe{rx: []byte("HTTP/1.1 101 Switching Protocols\r\n" + bloat)}
	if _, err := Dial(p, o); err == nil || err.Error() != string(RefuseHead) {
		t.Fatalf("got %v want %s", err, RefuseHead)
	}
}
