// Package h2 is the bounded HTTP/2 client codec (M212a, issue #3872):
// client preface, SETTINGS exchange, HEADERS with a bounded HPACK dynamic
// table, DATA, the client-side stream state machine, and bounded flow
// control — enough for the desk's fetch path over an ALPN-negotiated TLS
// stream (RFC 9113; ALPN per RFC 7301, h2 over TLS per RFC 9113 §3.3).
//
// Everything outside the bound refuses by name:
//   - PUSH_PROMISE — we advertise SETTINGS_ENABLE_PUSH=0; a pushed frame
//     is ErrPushPromise, never decoded.
//   - h2c — the cleartext Upgrade dance is never attempted; callers hand
//     this codec a negotiated-h2 stream (ErrH2C names the refusal at the
//     fetch layer).
//   - CONTINUATION storms — a header block is bounded by maxHeaderBlock
//     bytes and maxContinuations frames; over either is ErrContinuation.
//   - oversized frames — an inbound frame above our advertised
//     SETTINGS_MAX_FRAME_SIZE (maxFrameLen) is ErrFrameTooLarge.
//
// The package is pure: it drives a byte Stream (a *tls.TLSConn in the
// guest; a scripted pipe in host tests) so the whole grammar is
// host-tested without a socket, the httpc discipline.
package h2

import "errors"

// Stream is the byte transport the codec drives — a TLS connection in the
// guest, an in-memory pipe in host tests.
type Stream interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
}

// The named refusal grammar. These errors are the stable refusal surface
// on consumer serial markers (gofetch: error h2 ...).
var (
	// ErrPushPromise: the peer sent PUSH_PROMISE even though this
	// connection advertises ENABLE_PUSH=0. Never decoded, always fatal.
	ErrPushPromise = errors.New("h2: push_promise refused")
	// ErrH2C: a caller asked for HTTP/2 over a cleartext (Upgrade) path.
	// The codec only runs on an ALPN-negotiated stream.
	ErrH2C = errors.New("h2: h2c refused")
	// ErrContinuation: a header block exceeded maxContinuations fragments
	// or maxHeaderBlock bytes — the storm bound.
	ErrContinuation = errors.New("h2: continuation_storm refused")
	// ErrFrameTooLarge: an inbound frame header declared more than our
	// advertised SETTINGS_MAX_FRAME_SIZE.
	ErrFrameTooLarge = errors.New("h2: frame_too_large refused")
	// ErrSettingsFirst: the peer's first frame was not its SETTINGS — the
	// server connection preface (RFC 9113 §3.5) is not negotiable.
	ErrSettingsFirst = errors.New("h2: settings_first refused")
	// ErrHeaderBlock: the HPACK block failed decode or a header bound —
	// a malformed block is refused, never misparsed into a response.
	ErrHeaderBlock = errors.New("h2: header_block refused")
	// ErrBodyTooLarge: the response body passed Conn.MaxBody.
	ErrBodyTooLarge = errors.New("h2: body_max refused")
	// ErrWindow: a flow-control bound — WINDOW_UPDATE overflow, zero
	// increment, or inbound DATA beyond the advertised window.
	ErrWindow = errors.New("h2: flow_window refused")
	// ErrProtocol: a wire violation that is not one of the named
	// refusals above (bad SETTINGS, a frame on an unknown stream, a
	// CONTINUATION outside a header block, ...).
	ErrProtocol = errors.New("h2: protocol error")
	// ErrTransport: the byte stream failed or closed mid-exchange.
	ErrTransport = errors.New("h2: transport failed")
)

// errCode renders an RST_STREAM/GOAWAY error into a named refusal — the
// RFC 9113 §7 registry name, or the numeric code when it is unknown.
func errCode(kind string, code uint32) error {
	name := codeName(code)
	if name == "" {
		name = "0x" + hex32(code)
	}
	return errors.New("h2: " + kind + " " + name + " refused")
}

func codeName(code uint32) string {
	switch code {
	case 0x0:
		return "no_error"
	case 0x1:
		return "protocol_error"
	case 0x2:
		return "internal_error"
	case 0x3:
		return "flow_control_error"
	case 0x4:
		return "settings_timeout"
	case 0x5:
		return "stream_closed"
	case 0x6:
		return "frame_size_error"
	case 0x7:
		return "refused_stream"
	case 0x8:
		return "cancel"
	case 0x9:
		return "compression_error"
	case 0xa:
		return "connect_error"
	case 0xb:
		return "enhance_your_calm"
	case 0xc:
		return "inadequate_security"
	case 0xd:
		return "http_1_1_required"
	}
	return ""
}

func hex32(v uint32) string {
	const digits = "0123456789abcdef"
	var b [8]byte
	for i := 7; i >= 0; i-- {
		b[i] = digits[v&0xf]
		v >>= 4
	}
	return string(b[:])
}

// Field is one decoded header field.
type Field struct {
	Name  string
	Value string
}

// Response is one completed exchange: the final (non-1xx) :status, the
// header fields that rode the final HEADERS frame, and the DATA body.
type Response struct {
	Status  int
	Headers []Field
	Body    []byte
}

// Conn is one HTTP/2 client connection over a negotiated stream. It is
// deliberately sequential: one outstanding request at a time (the desk's
// fetch path is one-shot; card b's reuse across streams still shares this
// codec, one stream after another).
type Conn struct {
	s Stream

	// MaxBody bounds one response's DATA bytes; zero takes maxBodyDefault.
	MaxBody int

	started bool
	nextID  uint32 // next client-initiated stream id (odd)
	cur     *stream

	// peer settings applied to what we send
	peerMaxFrame uint32
	initSendWin  int64

	// flow control (send side; receive side lives on the stream +
	// connRecv). connSend is the peer's connection-level send credit.
	connSend int64
	// connRecv is our connection-level inbound credit not yet returned to
	// the peer; WINDOW_UPDATE refills it at the halfway mark.
	connRecv int64

	goawayLast uint32
	goawayCode uint32
	goaway     bool

	dec hpackDecoder
}

const (
	// maxFrameLen is the SETTINGS_MAX_FRAME_SIZE floor — also the inbound
	// admit bound, so a peer cannot legitimately out-frame it.
	maxFrameLen = 16384
	// maxHeaderBlock bounds the fragmented header block (HEADERS plus
	// every CONTINUATION) and maxContinuations the fragment count.
	maxHeaderBlock   = 16384
	maxContinuations = 16
	// dynTableMax is the advertised SETTINGS_HEADER_TABLE_SIZE — the
	// bound the HPACK dynamic table may never outgrow.
	dynTableMax = 4096
	// windowInit is the default per-stream / connection inbound window.
	windowInit = 65535
	// windowMax is the RFC flow-control ceiling (2^31-1).
	windowMax = 1<<31 - 1
	// maxBodyDefault bounds a response body when MaxBody is unset.
	maxBodyDefault = 1 << 20
)

// NewConn wraps a negotiated-h2 byte stream (ALPN "h2" — callers check
// tls.TLSConn.ALPN before constructing one).
func NewConn(s Stream) *Conn {
	return &Conn{
		s:           s,
		MaxBody:     maxBodyDefault,
		nextID:      1,
		peerMaxFrame: maxFrameLen,
		initSendWin: windowInit,
		connSend:    windowInit,
		connRecv:    windowInit,
	}
}

// start writes the client connection preface and the bound-bearing
// SETTINGS, then requires the peer's first frame to be its SETTINGS and
// acknowledges it — the whole connection preface exchange of RFC 9113 §3.5.
func (c *Conn) start() error {
	if c.started {
		return nil
	}
	out := append([]byte(clientPreface), c.settingsFrame()...)
	if err := c.writeAll(out); err != nil {
		return err
	}
	typ, flags, sid, p, err := c.readFrame()
	if err != nil {
		return err
	}
	if typ != frameSETTINGS || flags&flagAck != 0 || sid != 0 {
		return ErrSettingsFirst
	}
	if err := c.applySettings(p); err != nil {
		return err
	}
	if err := c.writeAll(frameHeader(frameSETTINGS, flagAck, 0, 0)); err != nil {
		return err
	}
	c.started = true
	return nil
}

// settingsFrame renders our SETTINGS: ENABLE_PUSH=0 (push refused by
// contract), and the bounds the peer must honor — the HPACK table cap and
// the frame-size cap the read path enforces.
func (c *Conn) settingsFrame() []byte {
	p := make([]byte, 0, 18)
	p = appendSetting(p, settingsEnablePush, 0)
	p = appendSetting(p, settingsHeaderTable, dynTableMax)
	p = appendSetting(p, settingsMaxFrame, maxFrameLen)
	return append(frameHeader(frameSETTINGS, 0, 0, len(p)), p...)
}

// applySettings takes a peer SETTINGS payload (u16 id || u32 value pairs)
// and applies the entries that change what we may send. Bounds failures
// are ErrProtocol — the RFC's own connection-error spellings.
func (c *Conn) applySettings(p []byte) error {
	if len(p)%6 != 0 {
		return ErrProtocol
	}
	for i := 0; i+6 <= len(p); i += 6 {
		id := uint16(p[i])<<8 | uint16(p[i+1])
		v := uint32(p[i+2])<<24 | uint32(p[i+3])<<16 | uint32(p[i+4])<<8 | uint32(p[i+5])
		switch id {
		case settingsInitialWindow:
			if v > windowMax {
				return ErrProtocol
			}
			delta := int64(v) - c.initSendWin
			c.initSendWin = int64(v)
			if c.cur != nil {
				c.cur.sendWin += delta
			}
		case settingsMaxFrame:
			if v < maxFrameLen || v > 16777215 {
				return ErrProtocol
			}
			c.peerMaxFrame = v
		case settingsMaxConcurrent:
			// We run one outstanding stream regardless; record nothing.
		case settingsHeaderTable:
			// Bounds OUR encoder's dynamic table; we never index.
		case settingsEnablePush:
			// A server may not set this (RFC 9113 §6.5.2), and we never
			// push regardless — tolerate and ignore.
		}
	}
	return nil
}

// Get performs one GET exchange: request HEADERS with END_STREAM, then
// frames until the stream's END_STREAM closes it. Sequential — a second
// call reuses the connection on a fresh stream id.
func (c *Conn) Get(authority, path string) (*Response, error) {
	if err := c.start(); err != nil {
		return nil, err
	}
	if c.cur != nil && c.cur.state != stClosed {
		return nil, ErrProtocol // one outstanding stream at a time
	}
	if c.goaway {
		return nil, errCode("goaway", c.goawayCode)
	}
	if path == "" {
		path = "/"
	}
	st := &stream{id: c.nextID, state: stHalfClosedLocal, sendWin: c.initSendWin, recvWin: windowInit}
	c.nextID += 2
	c.cur = st

	block := encodeRequest(authority, path)
	hdr := frameHeader(frameHEADERS, flagEndHeaders|flagEndStream, st.id, len(block))
	if err := c.writeAll(append(hdr, block...)); err != nil {
		return nil, err
	}
	return c.pump(st)
}

// pump reads frames until the request stream closes, interleaving the
// connection-level bookkeeping (SETTINGS acks, PING replies, WINDOW_UPDATE
// accounting, GOAWAY) with the stream's response assembly.
func (c *Conn) pump(st *stream) (*Response, error) {
	for {
		typ, flags, sid, p, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch typ {
		case frameSETTINGS:
			if sid != 0 {
				return nil, ErrProtocol
			}
			if flags&flagAck != 0 {
				if len(p) != 0 {
					return nil, ErrProtocol
				}
				continue
			}
			if err := c.applySettings(p); err != nil {
				return nil, err
			}
			if err := c.writeAll(frameHeader(frameSETTINGS, flagAck, 0, 0)); err != nil {
				return nil, err
			}
		case framePING:
			if sid != 0 || len(p) != 8 {
				return nil, ErrProtocol
			}
			if flags&flagAck != 0 {
				continue
			}
			if err := c.writeAll(append(frameHeader(framePING, flagAck, 0, 8), p...)); err != nil {
				return nil, err
			}
		case frameGOAWAY:
			if sid != 0 || len(p) < 8 {
				return nil, ErrProtocol
			}
			c.goawayLast = u31(p[0:4])
			c.goawayCode = u32v(p[4:8])
			c.goaway = true
			if st.id > c.goawayLast {
				return nil, errCode("goaway", c.goawayCode)
			}
		case framePUSHPROMISE:
			return nil, ErrPushPromise
		case frameWINDOWUPDATE:
			if len(p) != 4 {
				return nil, ErrProtocol
			}
			inc := int64(u31(p[0:4]))
			if inc == 0 {
				return nil, ErrProtocol
			}
			if sid == 0 {
				c.connSend += inc
				if c.connSend > windowMax {
					return nil, ErrWindow
				}
			} else if sid == st.id && st.state != stClosed {
				st.sendWin += inc
				if st.sendWin > windowMax {
					return nil, ErrWindow
				}
			}
		case framePRIORITY:
			// Deprecated signal (RFC 9113 §6.3): parsed and dropped.
			continue
		case frameHEADERS, frameDATA, frameRSTSTREAM, frameCONTINUATION:
			if sid != st.id || sid == 0 {
				return nil, ErrProtocol
			}
			done, err := c.streamFrame(st, typ, flags, p)
			if err != nil {
				return nil, err
			}
			if done {
				return &Response{Status: st.status, Headers: st.fields, Body: st.body}, nil
			}
		default:
			// Unknown extension frames are skipped wholesale (RFC 9113 §5.5).
			continue
		}
	}
}

// streamFrame applies one frame to the request stream's state machine and
// reports whether the exchange completed.
func (c *Conn) streamFrame(st *stream, typ, flags byte, p []byte) (bool, error) {
	switch typ {
	case frameHEADERS:
		if st.state != stHalfClosedLocal {
			return false, ErrProtocol
		}
		block, err := c.headerFragment(p, flags)
		if err != nil {
			return false, err
		}
		if flags&flagEndHeaders == 0 {
			block, err = c.collectContinuations(st.id, block)
			if err != nil {
				return false, err
			}
		}
		fields, err := c.dec.decode(block)
		if err != nil {
			return false, err
		}
		status, interim := statusOf(fields)
		if interim {
			// 1xx: consumed and dropped — the final HEADERS is still coming.
		} else if !st.gotHead {
			st.status, st.fields, st.gotHead = status, fields, true
		}
		// A second non-interim HEADERS is trailers: decoded (the dynamic
		// table must see it) and dropped.
		if flags&flagEndStream != 0 {
			st.state = stClosed
			return true, nil
		}
		return false, nil
	case frameCONTINUATION:
		// Only legal inside a header block, which collectContinuations
		// drives — a bare one is a violation.
		return false, ErrProtocol
	case frameDATA:
		if st.state != stHalfClosedLocal {
			return false, ErrProtocol
		}
		data, err := dataPayload(p, flags)
		if err != nil {
			return false, err
		}
		// The whole frame payload — padding included — debits the window
		// (RFC 9113 §6.1); only the unpadded part is body.
		if err := c.consume(st, len(p)); err != nil {
			return false, err
		}
		if len(st.body)+len(data) > c.MaxBody {
			return false, ErrBodyTooLarge
		}
		st.body = append(st.body, data...)
		if flags&flagEndStream != 0 {
			st.state = stClosed
			return true, nil
		}
		return false, nil
	case frameRSTSTREAM:
		if len(p) != 4 {
			return false, ErrProtocol
		}
		st.state = stClosed
		return false, errCode("rst_stream", u32v(p))
	}
	return false, ErrProtocol
}

// collectContinuations assembles a fragmented header block: CONTINUATION
// frames on the same stream until END_HEADERS, bounded on count and total
// bytes — the storm bound.
func (c *Conn) collectContinuations(sid uint32, block []byte) ([]byte, error) {
	for i := 0; i < maxContinuations; i++ {
		typ, flags, csid, p, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		if typ != frameCONTINUATION || csid != sid {
			return nil, ErrProtocol
		}
		block = append(block, p...)
		if len(block) > maxHeaderBlock {
			return nil, ErrContinuation
		}
		if flags&flagEndHeaders != 0 {
			return block, nil
		}
	}
	return nil, ErrContinuation
}

// headerFragment strips a HEADERS payload's padding and priority prefix.
func (c *Conn) headerFragment(p []byte, flags byte) ([]byte, error) {
	off := 0
	if flags&flagPadded != 0 {
		if len(p) < 1 {
			return nil, ErrProtocol
		}
		off = 1
	}
	if flags&flagPriority != 0 {
		off += 5
	}
	if off > len(p) {
		return nil, ErrProtocol
	}
	end := len(p)
	if flags&flagPadded != 0 {
		end -= int(p[0])
	}
	if end < off {
		return nil, ErrProtocol
	}
	return p[off:end], nil
}

// dataPayload strips a DATA payload's padding.
func dataPayload(p []byte, flags byte) ([]byte, error) {
	if flags&flagPadded == 0 {
		return p, nil
	}
	if len(p) < 1 || int(p[0])+1 > len(p) {
		return nil, ErrProtocol
	}
	return p[1 : len(p)-int(p[0])], nil
}

// consume debits inbound DATA from the connection and stream windows and
// returns credit at the halfway mark — the bounded flow-control contract.
// DATA beyond the advertised window is a connection violation.
func (c *Conn) consume(st *stream, n int) error {
	c.connRecv -= int64(n)
	st.recvWin -= int64(n)
	if c.connRecv < 0 || st.recvWin < 0 {
		return ErrWindow
	}
	var upd []byte
	if c.connRecv <= windowInit/2 {
		inc := windowInit - c.connRecv
		c.connRecv = windowInit
		upd = append(upd, frameHeader(frameWINDOWUPDATE, 0, 0, 4)...)
		upd = appendU32(upd, uint32(inc))
	}
	if st.recvWin <= windowInit/2 {
		inc := windowInit - st.recvWin
		st.recvWin = windowInit
		upd = append(upd, frameHeader(frameWINDOWUPDATE, 0, st.id, 4)...)
		upd = appendU32(upd, uint32(inc))
	}
	if len(upd) > 0 {
		return c.writeAll(upd)
	}
	return nil
}

// statusOf extracts :status and reports whether the head is interim (1xx).
func statusOf(fields []Field) (status int, interim bool) {
	for _, f := range fields {
		if f.Name == ":status" {
			for _, ch := range f.Value {
				if ch < '0' || ch > '9' {
					return 0, false
				}
				status = status*10 + int(ch-'0')
			}
			return status, status >= 100 && status < 200
		}
	}
	return 0, false
}

// stream is one request's client-side state machine. A client stream:
// HEADERS+END_SENT leaves idle for halfClosedLocal (the remote may still
// send); its END_STREAM closes.
type stream struct {
	id      uint32
	state   int
	sendWin int64
	recvWin int64

	gotHead bool
	status  int
	fields  []Field
	body    []byte
}

const (
	stIdle = iota
	stOpen
	stHalfClosedLocal
	stHalfClosedRemote
	stClosed
)

func (c *Conn) writeAll(p []byte) error {
	for len(p) > 0 {
		n, err := c.s.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrTransport
		}
		p = p[n:]
	}
	return nil
}

// readFull fills p; the TLS record seam may return short reads.
func (c *Conn) readFull(p []byte) error {
	for off := 0; off < len(p); {
		n, err := c.s.Read(p[off:])
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrTransport
		}
		off += n
	}
	return nil
}

// readFrame reads one frame header and payload, applying the inbound
// admit bound.
func (c *Conn) readFrame() (typ, flags byte, sid uint32, payload []byte, err error) {
	var hdr [9]byte
	if err := c.readFull(hdr[:]); err != nil {
		return 0, 0, 0, nil, err
	}
	l := int(hdr[0])<<16 | int(hdr[1])<<8 | int(hdr[2])
	if l > maxFrameLen {
		return 0, 0, 0, nil, ErrFrameTooLarge
	}
	payload = make([]byte, l)
	if err := c.readFull(payload); err != nil {
		return 0, 0, 0, nil, err
	}
	return hdr[3], hdr[4], u31(hdr[5:9]), payload, nil
}

func u31(b []byte) uint32 {
	return uint32(b[0]&0x7f)<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func u32v(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func appendU32(dst []byte, v uint32) []byte {
	return append(dst, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
