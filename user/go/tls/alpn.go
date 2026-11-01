// The ALPN surface (RFC 7301, M212a #3872): DialALPN/HandshakeALPN are
// Dial/Handshake plus a caller-offered ProtocolNameList carried in the
// ClientHello's application_layer_protocol_negotiation extension; the
// negotiated pick is readable from TLSConn.ALPN ("" when nothing was
// negotiated). Everything lives here so the handshake files keep their
// shape — the extension emission itself is in client.go beside the other
// extensions.

package tls

import (
	"errors"

	"virelai/vi"
)

var (
	// ErrALPNList is a bad caller list — empty entries, oversized names,
	// or too many protocols. Checked before any dial.
	ErrALPNList = errors.New("tls: ALPN protocol list invalid")
	// ErrALPNProtocol is the peer's violation: it answered a protocol the
	// client never offered (or answered ALPN unsolicited).
	ErrALPNProtocol = errors.New("tls: ALPN negotiated an unoffered protocol")
	// errExtension is a malformed extension body in a handshake message.
	errExtension = errors.New("tls: malformed extension")
)

// alpnMaxProtocols bounds the caller's offered list — nobody negotiates
// from a longer menu, and a runaway list fails before it reaches the wire.
const alpnMaxProtocols = 8

// checkALPN validates an offered protocol list: every name is 1..255
// bytes (the ProtocolName u8 length's bound) and the list is bounded.
func checkALPN(protocols []string) error {
	if len(protocols) > alpnMaxProtocols {
		return ErrALPNList
	}
	for _, p := range protocols {
		if len(p) == 0 || len(p) > 255 {
			return ErrALPNList
		}
	}
	return nil
}

// DialALPN is Dial plus an ALPN ProtocolNameList (RFC 7301): protocols are
// offered in preference order ("h2" before "http/1.1" for the h2-first
// desk), and the server's pick is readable from ALPN(). An empty list is
// Dial verbatim — no extension is sent. A malformed list fails before any
// dial.
func DialALPN(addr string, port uint16, serverName string, protocols []string) (*TLSConn, error) {
	if err := checkALPN(protocols); err != nil {
		return nil, err
	}
	conn, err := vi.Dial(addr, port)
	if err != nil {
		return nil, err
	}
	if serverName == "" {
		serverName = addr
	}
	c, err := HandshakeALPN(conn, serverName, 0, protocols)
	if err != nil {
		return nil, err
	}
	// The same response-phase bound Dial arms (M97f F4, #2107).
	c.SetDeadline(vi.Nanos() + DefaultResponseNs)
	return c, nil
}

// HandshakeALPN is Handshake plus an offered ALPN list — the connected
// socket, the deadline semantics, and every failure path are identical;
// the ClientHello additionally carries the ALPN extension.
func HandshakeALPN(conn *vi.Conn, serverName string, deadline int64, protocols []string) (*TLSConn, error) {
	if conn == nil {
		return nil, errTransport
	}
	if err := checkALPN(protocols); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if serverName == "" {
		_ = conn.Close()
		return nil, errBadServerName
	}
	t := newVITransport(conn)
	t.st.deadlineAt = handshakeDeadline(deadline, vi.Nanos())
	c := &TLSConn{conn: conn, t: t}
	c.cl = newClient(t, serverName, vi.Time(), viRandom, true)
	c.cl.alpn = protocols
	c.sess = &c.cl.session
	if err := t.st.checkDeadline(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := c.cl.handshake(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	t.st.deadlineAt = 0
	return c, nil
}

// ALPN reports the protocol the server picked ("" when nothing was
// negotiated — either no offer went out or the peer answered none).
// Server-side (accepted) connections negotiate nothing and read "".
func (c *TLSConn) ALPN() string {
	if c == nil || c.cl == nil {
		return ""
	}
	return c.cl.alpnNegotiated
}
