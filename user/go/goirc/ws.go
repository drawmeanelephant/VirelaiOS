package main

// ws.go — M212c (issue #3874): the --ws gateway transport. The card's
// live consumer is IRC-over-WebSocket (the Kiwi/webchat gateway shape):
// the virelai/ws codec runs the Upgrade handshake over the existing
// TCP/TLS dial, then each IRC line rides one text message and each text
// or binary message decodes as one line — the framing real gateways
// (ircv3's websocket convention) use, not a byte tunnel.
//
// Markers: the dial line's scheme names the transport ('ws' or 'wss');
// ws's own named refusals surface through the session's drop line
// ('goirc: drop ws_message_too_large' and kin).

import (
	"strings"

	"virelai/vi"
	"virelai/vsys"
	"virelai/ws"
)

// wsLineConn adapts *ws.Conn onto the core's byte-pipe WireConn: the
// write side splits the stream on '\n' into one text message per IRC
// line (CRLF stripped — the gateway convention carries the line bare),
// the read side returns each received message's payload + '\n' into the
// splitter's grammar. ws's control frames are its own layer: pings are
// already ponged inside ws, closes end the session as the peer drop.
type wsLineConn struct {
	conn *ws.Conn
	wire WireConn // the transport under ws — deadline + close live here
	rbuf []byte
	wbuf []byte
}

// wsDial upgrades an already-connected transport to a ws session: the
// handshake runs on wire, then the message codec wraps it.
func wsDial(wire WireConn, cfg Config) (*wsLineConn, error) {
	path := cfg.WSPath
	if path == "" {
		path = "/"
	}
	conn, err := ws.Dial(wire, ws.Options{
		Host:        hostHeader(cfg),
		Path:        path,
		Subprotocol: "irc",
		Rand:        wsRand,
	})
	if err != nil {
		return nil, err
	}
	return &wsLineConn{conn: conn, wire: wire}, nil
}

// hostHeader renders the authority the Upgrade's Host header carries.
func hostHeader(cfg Config) string {
	return cfg.Host + ":" + vsys.Itoa64(int64(cfg.Port))
}

// wsRand is the nonce/mask entropy seam — vi.Random on the guest,
// swappable in tests.
var wsRand = vi.Random

func (w *wsLineConn) Read(b []byte) (int, error) {
	for len(w.rbuf) == 0 {
		op, payload, err := w.conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		switch op {
		case ws.OpText, ws.OpBinary:
			w.rbuf = append(payload, '\n')
		case ws.OpPing, ws.OpPong:
			continue // ws-layer traffic, invisible to the IRC line stream
		case ws.OpClose:
			return 0, ws.ErrClosed
		}
	}
	n := copy(b, w.rbuf)
	w.rbuf = w.rbuf[n:]
	return n, nil
}

// Write splits p on '\n': each complete line goes out as one text
// message; an unterminated tail waits in wbuf for the rest of the line.
func (w *wsLineConn) Write(p []byte) (int, error) {
	w.wbuf = append(w.wbuf, p...)
	for {
		i := strings.IndexByte(string(w.wbuf), '\n')
		if i < 0 {
			return len(p), nil
		}
		line := strings.TrimRight(string(w.wbuf[:i]), "\r")
		w.wbuf = w.wbuf[i+1:]
		if line == "" {
			continue
		}
		if err := w.conn.Send(ws.OpText, []byte(line)); err != nil {
			return 0, err
		}
	}
}

// SetReadDeadline forwards the core's absolute-nanos arming to the
// transport — the ws layer never swallows the ETIMEDOUT it produces.
func (w *wsLineConn) SetReadDeadline(at int64) { w.wire.SetReadDeadline(at) }

// Close runs the close handshake then releases the transport — a QUIT
// ends politely at both layers; a drop's Close is already cheap.
func (w *wsLineConn) Close() error {
	if w.conn != nil {
		_ = w.conn.Close(1000) // best-effort; the transport dies regardless
	}
	return w.wire.Close()
}
