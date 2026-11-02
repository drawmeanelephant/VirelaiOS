// Package ws is the bounded RFC 6455 WebSocket client codec — M212c
// (card #3874, index #3871). It owns the HTTP/1.1 Upgrade handshake
// over the httpc head grammar, the masked-client frame layer, control
// frames (ping/pong/close), continuation reassembly, and the close
// handshake. The guest consumers (GOIRC's --ws gateway transport,
// WSECHO's hermetic echo leg) hold a *Conn; the frame grammar is pure
// so the refusal names are host-tested without a socket.
//
// Bounds (each a named refusal, never a silent drop):
//
//   - the response head caps at MaxHead,
//   - one frame caps at MaxFrame,
//   - one reassembled message caps at MaxMessage,
//   - fragmentation is one message deep (no interleaved data frames).
//
// Out of scope by card: the server side, h2 extended-connect,
// compression extensions (a Sec-WebSocket-Extensions offer we did not
// negotiate fails the handshake), subprotocol negotiation beyond one
// named token, and cleartext upgrade off an existing h1 connection.
package ws

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"strings"

	"virelai/httpc"
)

// Error is a named refusal — the stable marker text the gate and the
// consumers grep for (the httpc refusal-string discipline).
type Error string

func (e Error) Error() string { return string(e) }

// The named refusal grammar. ws_message_too_large is the card's own
// spelling; the rest follow its underscore convention.
const (
	// Handshake.
	RefuseStatus      = Error("ws_upgrade_refused")          // not a 101
	RefuseHead        = Error("ws_head_limit")               // response head over MaxHead
	RefuseUpgradeHdr  = Error("ws_upgrade_header_bad")       // Upgrade/Connection tokens wrong
	RefuseAccept      = Error("ws_accept_bad")               // Sec-WebSocket-Accept mismatch
	RefuseExtension   = Error("ws_extension_unnegotiated")   // server offered an extension anyway
	RefuseSubprotocol = Error("ws_subprotocol_unnegotiated") // server named a token we did not send

	// Frame layer.
	RefuseFrameLarge    = Error("ws_frame_too_large")      // declared length over MaxFrame
	RefuseMessageLarge  = Error("ws_message_too_large")    // assembled message over MaxMessage
	RefuseServerMasked  = Error("ws_server_masked")        // server->client frame with MASK set
	RefuseRSV           = Error("ws_rsv_set")              // RSV1-3 set, no extension negotiated
	RefuseOpcode        = Error("ws_opcode_bad")           // reserved opcode (0x3-7, 0xB-F)
	RefuseControlFrag   = Error("ws_control_fragmented")   // control frame with fin=0
	RefuseControlLarge  = Error("ws_control_too_large")    // control payload over 125
	RefuseStrayCont     = Error("ws_continuation_stray")   // continuation with no message open
	RefuseFragmentMixed = Error("ws_fragment_interleaved") // data frame while a message is open
	RefuseTruncated     = Error("ws_frame_truncated")      // peer EOF inside a frame
	RefuseCloseBad      = Error("ws_close_bad")            // close body of 1 byte or a bad code
	RefuseKeyEntropy    = Error("ws_key_entropy")          // the nonce/mask source ran dry
)

// ErrClosed is the session-end error ReadMessage returns once the close
// handshake has run its course — the peer's close seen, our answer sent.
var ErrClosed = errors.New("ws: closed")

// Opcodes (RFC 6455 §5.2).
const (
	OpContinuation = 0x0
	OpText         = 0x1
	OpBinary       = 0x2
	OpClose        = 0x8
	OpPing         = 0x9
	OpPong         = 0xA
)

// Bounds.
const (
	DefaultMaxHead    = 8192   // response head
	DefaultMaxFrame   = 65536  // one frame's declared payload
	DefaultMaxMessage = 262144 // one reassembled message
	headroom          = 14     // worst-case frame header + mask key
)

// Pipe is the byte transport under the session — the shape vi.Conn's
// Send/Recv and tls.TLSConn's Read/Write adapt onto (GOIRC's WireConn).
// Read's error surface (deadline ETIMEDOUT, peer-gone) is passed through
// untouched: the consumers' liveness state machines match those names.
type Pipe interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
}

// Options is one dial's shape. Rand fills the handshake nonce and every
// frame's mask key; nil falls back to crypto/rand (the fork carries
// getrandom on virelai). The bounds zero-default to the package's.
type Options struct {
	Host        string   // authority for the Host header — caller renders host[:port]
	Path        string   // default "/"
	Subprotocol string   // one named token; empty sends no Sec-WebSocket-Protocol
	Origin      string   // optional Origin header
	Key         [16]byte // client nonce; a zero Key is filled from Rand
	Rand        func(p []byte) (int, error)

	MaxHead    int // response head bound
	MaxFrame   int // declared payload bound per frame
	MaxMessage int // assembled message bound
}

// acceptGUID is the RFC 6455 §1.3 magic concatenated onto the client key.
const acceptGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Accept computes the Sec-WebSocket-Accept value a correct server must
// return for a base64 nonce — the handshake's one checksum.
func Accept(keyB64 string) string {
	sum := sha1.Sum([]byte(keyB64 + acceptGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// Request renders the Upgrade request for o. Pure — the bytes a correct
// peer answers with a 101. o.Key must already hold the nonce (Dial fills
// it before calling).
func Request(o Options) []byte {
	path := o.Path
	if path == "" {
		path = "/"
	}
	var b strings.Builder
	b.WriteString("GET " + path + " HTTP/1.1\r\n")
	b.WriteString("Host: " + o.Host + "\r\n")
	b.WriteString("User-Agent: VirelaiOS/1.0\r\n")
	b.WriteString("Upgrade: websocket\r\n")
	b.WriteString("Connection: Upgrade\r\n")
	b.WriteString("Sec-WebSocket-Key: " + base64.StdEncoding.EncodeToString(o.Key[:]) + "\r\n")
	b.WriteString("Sec-WebSocket-Version: 13\r\n")
	if o.Subprotocol != "" {
		b.WriteString("Sec-WebSocket-Protocol: " + o.Subprotocol + "\r\n")
	}
	if o.Origin != "" {
		b.WriteString("Origin: " + o.Origin + "\r\n")
	}
	b.WriteString("\r\n")
	return []byte(b.String())
}

// hasToken reports whether a header's comma list carries token
// (case-insensitive) — the Upgrade/Connection grammar.
func hasToken(list, token string) bool {
	for _, part := range strings.Split(list, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// headerVal extracts one field's value from a parsed head block —
// whitespace-trimmed, name-matched case-insensitively.
func headerVal(head, name string) string {
	for _, line := range strings.Split(head, "\n") {
		line = strings.TrimRight(line, "\r")
		k := strings.IndexByte(line, ':')
		if k < 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(line[:k]), name) {
			return strings.TrimSpace(line[k+1:])
		}
	}
	return ""
}

// CheckResponse validates a complete 101 response head against the
// request: status, Upgrade/Connection tokens, the Accept checksum, and
// the two we-did-not-negotiate refusals. Returns "" or the named refusal.
func CheckResponse(head []byte, o Options) string {
	h, refusal := httpc.ParseHead(head, 1024, 32)
	if refusal != "" {
		return string(RefuseUpgradeHdr)
	}
	if h.Status != 101 {
		return string(RefuseStatus)
	}
	text := string(head)
	if !hasToken(headerVal(text, "Upgrade"), "websocket") ||
		!hasToken(headerVal(text, "Connection"), "upgrade") {
		return string(RefuseUpgradeHdr)
	}
	want := Accept(base64.StdEncoding.EncodeToString(o.Key[:]))
	if got := headerVal(text, "Sec-WebSocket-Accept"); got != want {
		return string(RefuseAccept)
	}
	if headerVal(text, "Sec-WebSocket-Extensions") != "" {
		return string(RefuseExtension)
	}
	if sp := headerVal(text, "Sec-WebSocket-Protocol"); sp != "" && sp != o.Subprotocol {
		return string(RefuseSubprotocol)
	}
	return ""
}

// Conn is one established session. The read side answers pings, echoes
// the peer's close, and reassembles continuations; the send side masks
// every frame — RFC 6455 §5.1's client rule, non-optional.
type Conn struct {
	p    Pipe
	rand func(p []byte) (int, error)

	maxFrame int
	maxMsg   int

	rbuf []byte // bytes read ahead of the parse cursor
	eof  bool   // the transport ended; drains rbuf then fails

	fragOp byte   // open message's opcode; 0 = none
	frag   []byte // open message's accumulated payload

	closeSent bool
	peerClose bool // peer's close frame seen (its payload in closeBody)
	closeBody []byte
}

// Dial performs the client handshake on p: nonce, request, bounded head
// read, CheckResponse. A refused handshake is a named Error; a dead
// transport is the transport's own error. Leftover head bytes seed the
// frame buffer — a server that pipelined its first frame loses nothing.
func Dial(p Pipe, o Options) (*Conn, error) {
	if o.MaxHead <= 0 {
		o.MaxHead = DefaultMaxHead
	}
	if o.MaxFrame <= 0 {
		o.MaxFrame = DefaultMaxFrame
	}
	if o.MaxMessage <= 0 {
		o.MaxMessage = DefaultMaxMessage
	}
	if o.Rand == nil {
		o.Rand = rand.Read
	}
	zero := [16]byte{}
	if o.Key == zero {
		if n, err := o.Rand(o.Key[:]); err != nil || n != len(o.Key) {
			return nil, RefuseKeyEntropy
		}
	}
	c := &Conn{p: p, rand: o.Rand, maxFrame: o.MaxFrame, maxMsg: o.MaxMessage}
	if err := c.writeAll(Request(o)); err != nil {
		return nil, err
	}
	head, err := c.readHead(o.MaxHead)
	if err != nil {
		return nil, err
	}
	if refusal := CheckResponse(head, o); refusal != "" {
		return nil, Error(refusal)
	}
	return c, nil
}

// readHead fills rbuf until the head terminator lands or MaxHead is
// exceeded. The head is consumed; the pipelined remainder stays.
func (c *Conn) readHead(max int) ([]byte, error) {
	var tmp [2048]byte
	for {
		if end := httpc.HeadEnd(c.rbuf); end >= 0 {
			head := c.rbuf[:end]
			c.rbuf = c.rbuf[end:]
			return head, nil
		}
		if len(c.rbuf) >= max {
			return nil, RefuseHead
		}
		n, err := c.p.Read(tmp[:])
		if n > 0 {
			c.rbuf = append(c.rbuf, tmp[:n]...)
		}
		if err != nil {
			return nil, err
		}
	}
}

func (c *Conn) writeAll(b []byte) error {
	for len(b) > 0 {
		n, err := c.p.Write(b)
		if n > 0 {
			b = b[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return RefuseTruncated
		}
	}
	return nil
}

// frameHead is one parsed frame header — the wire shape before payload
// bounds or masking rules apply.
type frameHead struct {
	fin     bool
	rsv     byte // RSV1-3 as the 0x70 mask's value
	op      byte
	masked  bool
	length  uint64
	maskKey [4]byte
	headLen int
}

// parseHead decodes one frame header. need>0 means buf is short: pull
// more wire. refusal names the first grammar violation.
func parseHead(buf []byte) (h frameHead, need int, refusal string) {
	if len(buf) < 2 {
		return h, 2 - len(buf), ""
	}
	h.fin = buf[0]&0x80 != 0
	h.rsv = buf[0] & 0x70
	h.op = buf[0] & 0x0F
	h.masked = buf[1]&0x80 != 0
	n := uint64(buf[1] & 0x7F)
	off := 2
	switch n {
	case 126:
		if len(buf) < off+2 {
			return h, off + 2 - len(buf), ""
		}
		n = uint64(buf[off])<<8 | uint64(buf[off+1])
		off += 2
	case 127:
		if len(buf) < off+8 {
			return h, off + 8 - len(buf), ""
		}
		n = 0
		for i := 0; i < 8; i++ {
			n = n<<8 | uint64(buf[off+i])
		}
		off += 8
	}
	if h.masked {
		if len(buf) < off+4 {
			return h, off + 4 - len(buf), ""
		}
		copy(h.maskKey[:], buf[off:off+4])
		off += 4
	}
	h.length, h.headLen = n, off
	return h, 0, ""
}

// checkHead applies the server-side grammar rules to a parsed header —
// every refusal RFC 6455 §5.x assigns the receiving client.
func (c *Conn) checkHead(h frameHead) string {
	if h.rsv != 0 {
		return string(RefuseRSV) // we negotiated no extension — any RSV is the peer's bug
	}
	if h.masked {
		return string(RefuseServerMasked)
	}
	switch h.op {
	case OpContinuation, OpText, OpBinary, OpClose, OpPing, OpPong:
	default:
		return string(RefuseOpcode)
	}
	if h.op >= 0x8 {
		if !h.fin {
			return string(RefuseControlFrag)
		}
		if h.length > 125 {
			return string(RefuseControlLarge)
		}
	}
	if h.length > uint64(c.maxFrame) {
		return string(RefuseFrameLarge)
	}
	if h.op == OpContinuation && c.fragOp == 0 {
		return string(RefuseStrayCont)
	}
	if (h.op == OpText || h.op == OpBinary) && c.fragOp != 0 {
		return string(RefuseFragmentMixed)
	}
	return ""
}

// fill pulls at least want more bytes into rbuf. A peer EOF inside a
// pending frame is RefuseTruncated; between frames it is the transport's
// own end error.
func (c *Conn) fill(want int) error {
	var tmp [8192]byte
	for want > 0 {
		n, err := c.p.Read(tmp[:])
		if n > 0 {
			c.rbuf = append(c.rbuf, tmp[:n]...)
			want -= n
		}
		if err != nil {
			return err
		}
		if n == 0 && want > 0 {
			return RefuseTruncated
		}
	}
	return nil
}

// frame reads the next complete frame: header, bounds, payload (masked
// server payloads would already be refused; client payloads never mask-
// decode — servers never send them).
func (c *Conn) frame() (h frameHead, payload []byte, err error) {
	for {
		var need int
		var refusal string
		h, need, refusal = parseHead(c.rbuf)
		if refusal != "" {
			return h, nil, Error(refusal)
		}
		if need == 0 {
			break
		}
		if err := c.fill(need); err != nil {
			return h, nil, err
		}
	}
	if refusal := c.checkHead(h); refusal != "" {
		return h, nil, Error(refusal)
	}
	if len(c.rbuf) < h.headLen+int(h.length) {
		if err := c.fill(h.headLen + int(h.length) - len(c.rbuf)); err != nil {
			return h, nil, err
		}
	}
	payload = c.rbuf[h.headLen : h.headLen+int(h.length)]
	c.rbuf = c.rbuf[h.headLen+int(h.length):]
	if h.masked {
		// Unreachable — checkHead refuses masked server frames — but the
		// unmask belongs to the parse, not a caller's hope.
		for i := range payload {
			payload[i] ^= h.maskKey[i%4]
		}
	}
	return h, payload, nil
}

// marshal builds one masked client frame. Control payloads are the
// caller's already-bounded bytes; Send refuses oversize before the wire.
func (c *Conn) marshal(fin bool, op byte, payload []byte) ([]byte, error) {
	head := []byte{op}
	if fin {
		head[0] |= 0x80
	}
	var ext [8]byte
	n := len(payload)
	switch {
	case n < 126:
		head = append(head, 0x80|byte(n))
	case n <= 0xFFFF:
		head = append(head, 0x80|126, byte(n>>8), byte(n))
	default:
		head = append(head, 0x80|127)
		for i := 7; i >= 0; i-- {
			ext[i] = byte(n >> (8 * uint(7-i)))
		}
		head = append(head, ext[:]...)
	}
	var mask [4]byte
	if c.rand != nil {
		if rn, err := c.rand(mask[:]); err != nil || rn != 4 {
			return nil, RefuseKeyEntropy
		}
	}
	head = append(head, mask[:]...)
	frame := make([]byte, 0, len(head)+n)
	frame = append(frame, head...)
	for i := 0; i < n; i++ {
		frame = append(frame, payload[i]^mask[i%4])
	}
	return frame, nil
}

// Send writes one whole message as a single masked frame of opcode op
// (OpText or OpBinary). Fragmented sends stay out of scope — the
// consumers' messages are bounded small.
func (c *Conn) Send(op byte, payload []byte) error {
	if op != OpText && op != OpBinary {
		return RefuseOpcode
	}
	if len(payload) > c.maxFrame {
		return RefuseFrameLarge
	}
	frame, err := c.marshal(true, op, payload)
	if err != nil {
		return err
	}
	return c.writeAll(frame)
}

// SendPing / SendPong write the control pair. Payloads cap at 125 — the
// RFC's control bound — refused by name rather than truncated.
func (c *Conn) SendPing(payload []byte) error { return c.sendControl(OpPing, payload) }
func (c *Conn) SendPong(payload []byte) error { return c.sendControl(OpPong, payload) }

func (c *Conn) sendControl(op byte, payload []byte) error {
	if len(payload) > 125 {
		return RefuseControlLarge
	}
	frame, err := c.marshal(true, op, payload)
	if err != nil {
		return err
	}
	return c.writeAll(frame)
}

// Close runs the client's half of the close handshake: a close frame
// carrying code (0 = no status). Idempotent — ReadMessage's auto-answer
// may already have sent it; the second call is a no-op.
func (c *Conn) Close(code uint16) error {
	if c.closeSent {
		return nil
	}
	c.closeSent = true
	var payload []byte
	if code != 0 {
		payload = []byte{byte(code >> 8), byte(code)}
	}
	frame, err := c.marshal(true, OpClose, payload)
	if err != nil {
		return err
	}
	return c.writeAll(frame)
}

// CloseBody is the peer's close payload once PeerClosed — the two-byte
// status when it sent one.
func (c *Conn) CloseBody() []byte { return c.closeBody }

// PeerClosed reports whether the peer's close frame has landed.
func (c *Conn) PeerClosed() bool { return c.peerClose }

// checkClose validates a close payload: empty, or a 2-byte code inside
// the registered ranges (RFC 6455 §7.4 — 1000-2999 plus the protocol's
// reserved set; the never-send codes are refused).
func checkClose(payload []byte) string {
	switch len(payload) {
	case 0:
		return ""
	case 1:
		return string(RefuseCloseBad)
	}
	code := uint16(payload[0])<<8 | uint16(payload[1])
	if code < 1000 || code >= 5000 {
		return string(RefuseCloseBad)
	}
	switch code {
	case 1004, 1005, 1006, 1015: // RFC §7.4.1's never-wire codes
		return string(RefuseCloseBad)
	}
	return ""
}

// ReadMessage returns the next complete message: op is OpText/OpBinary
// for data, OpPing/OpPong/OpClose for control (pings are answered before
// they return — the consumer sees them but never owes the pong). A peer
// close is auto-answered once and returned; every call after the close
// handshake completes reports ErrClosed. Fragmentation is reassembled
// under MaxMessage with the named refusals armed at the head parse.
func (c *Conn) ReadMessage() (op byte, payload []byte, err error) {
	if c.peerClose {
		return 0, nil, ErrClosed
	}
	for {
		h, pl, err := c.frame()
		if err != nil {
			return 0, nil, err
		}
		switch h.op {
		case OpPing:
			if err := c.SendPong(pl); err != nil {
				return 0, nil, err
			}
			return h.op, append([]byte(nil), pl...), nil
		case OpPong:
			return h.op, append([]byte(nil), pl...), nil
		case OpClose:
			if refusal := checkClose(pl); refusal != "" {
				return 0, nil, Error(refusal)
			}
			c.peerClose = true
			c.closeBody = append([]byte(nil), pl...)
			if !c.closeSent {
				// The RFC's answer echoes the peer's close body — sent
				// directly, not via Send (which gates on data opcodes).
				frame, ferr := c.marshal(true, OpClose, pl)
				if ferr != nil {
					return 0, nil, ferr
				}
				if werr := c.writeAll(frame); werr != nil {
					return 0, nil, werr
				}
				c.closeSent = true
			}
			return h.op, append([]byte(nil), pl...), nil
		case OpText, OpBinary:
			if !h.fin {
				c.fragOp = h.op
				c.frag = append([]byte(nil), pl...)
				continue
			}
			return h.op, append([]byte(nil), pl...), nil
		case OpContinuation:
			c.frag = append(c.frag, pl...)
			if len(c.frag) > c.maxMsg {
				return 0, nil, RefuseMessageLarge
			}
			if !h.fin {
				continue
			}
			op, payload = c.fragOp, c.frag
			c.fragOp, c.frag = 0, nil
			return op, payload, nil
		}
	}
}
