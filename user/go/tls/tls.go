// The guest-facing TLS surface: Dial connects over the M67a socket seam
// (vi.Dial — kernel TCP slots 30-33, DNS in front of the dial), completes
// the ADR 0029 TLS 1.3 handshake in-process, and hands back a Read/Write/
// Close connection. This is the card M67b's whole point: HTTPS without
// execing FETCHS.BIN — no spawn, no helper process, no argv caps.

package tls

import "virelai/vi"

const (
	// DefaultHandshakeNs is the total bound a caller-less handshake gets
	// (M97f F4, #2107): 30 s, the browser's declared-budget class. It caps
	// the whole record loop — a peer dribbling ChangeCipherSpec or
	// fragmenting its flight indefinitely hits it, where the legacy path
	// had only pumpOnce's per-pump 5 s (reset by every received byte).
	DefaultHandshakeNs = 30_000_000_000
	// DefaultResponseNs is the total bound Dial arms on the established
	// connection for the caller's request/response phase. A server that
	// completes the handshake then stalls the body is cut here.
	DefaultResponseNs = 30_000_000_000
)

// handshakeDeadline resolves the caller's absolute deadline: zero or
// negative means "no declared bound", which maps to the package default
// total deadline measured from now. A positive value is the caller's
// override and is honored verbatim.
func handshakeDeadline(d, now int64) int64 {
	if d > 0 {
		return d
	}
	return now + DefaultHandshakeNs
}

// viTransport bridges the kernel TCP seam to the client's byte-stream
// transport. It does not use vi.Conn.Recv/Send: Recv waits on slot 76
// without draining virtio, and Send does not drain between 192-byte
// segments. The stream adapter (stream.go, the FETCHS.BIN contract) is
// what makes a live handshake complete on this kernel.
type viTransport struct {
	conn *vi.Conn
	st   tcpStream
}

func newVITransport(conn *vi.Conn) *viTransport {
	t := &viTransport{conn: conn}
	t.st.recv = t.kernelRecv
	t.st.send = t.kernelSend
	t.st.peekEOF = t.kernelEOF
	t.st.buf = make([]byte, streamStashCap)
	return t
}

func (t *viTransport) kernelRecv(p []byte) (int, error) {
	n, rc := vi.TCPRecv(p)
	if rc < 0 {
		return 0, errTransport
	}
	return n, nil
}

func (t *viTransport) kernelSend(p []byte) (int, error) {
	n, rc := vi.TCPSend(p)
	if rc < 0 {
		return 0, errTransport
	}
	if n == 0 && len(p) > 0 {
		return 0, errTransport
	}
	return n, nil
}

func (t *viTransport) kernelEOF() bool {
	mask, rc := vi.TCPReady()
	return rc >= 0 && mask&1 != 0
}

func (t *viTransport) read(p []byte) (int, error) { return t.st.read(p) }

func (t *viTransport) write(p []byte) (int, error) {
	n, err := t.st.write(p)
	if err != nil {
		return n, err
	}
	if n != len(p) {
		return n, errTransport
	}
	return n, nil
}

func (t *viTransport) close() error { return t.conn.Close() }

// TLSConn is an established TLS 1.3 connection.
type TLSConn struct {
	cl   *client
	conn *vi.Conn
	t    *viTransport
}

// Dial connects to addr:port and handshakes. serverName is what goes into
// the SNI extension and what the certificate is verified against — it may
// differ from the dial address (the live gate dials 10.0.0.2 as
// leaf.example.com). An empty serverName verifies against the dial address.
// The validity clock is vi.Time and the entropy source is the kernel CSPRNG
// (slot 72), the same contract FETCHS.BIN had.
func Dial(addr string, port uint16, serverName string) (*TLSConn, error) {
	conn, err := vi.Dial(addr, port)
	if err != nil {
		return nil, err
	}
	if serverName == "" {
		serverName = addr
	}
	c, err := Handshake(conn, serverName, 0)
	if err != nil {
		return nil, err
	}
	// M97f F4 (#2107): the established connection keeps a total bound for
	// the caller's request/response phase — a peer that answers the
	// handshake then dribbles or stalls the body is cut at the declared
	// budget instead of parking the tool. Callers re-arm with SetDeadline.
	c.SetDeadline(vi.Nanos() + DefaultResponseNs)
	return c, nil
}

// Handshake takes ownership of a connected socket, including every failure
// path. serverName is always verified; literal IPs omit SNI, not validation.
// deadline is an absolute vi.Nanos instant bounding the WHOLE handshake —
// ClientHello through the server Finished — not any single record pump.
// A non-positive deadline selects DefaultHandshakeNs from now; there is no
// "unbounded" spelling left (M97f F4, #2107: a dribbling peer held the
// legacy path forever). On success the stream returns to its per-pump
// bounds — the handshake bound was the handshake's, and the caller arms
// the response phase through SetDeadline (Dial arms DefaultResponseNs).
// Browser callers resolve/connect separately, then pass the earlier of
// their handshake and whole-page deadlines.
func Handshake(conn *vi.Conn, serverName string, deadline int64) (*TLSConn, error) {
	if conn == nil {
		return nil, errTransport
	}
	if serverName == "" {
		_ = conn.Close()
		return nil, errBadServerName
	}
	t := newVITransport(conn)
	t.st.deadlineAt = handshakeDeadline(deadline, vi.Nanos())
	c := &TLSConn{conn: conn, t: t}
	c.cl = newClient(t, serverName, vi.Time(), viRandom, true)
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

// viRandom fills p from the kernel CSPRNG (slot 72 getrandom, 256 bytes per
// call — loop for more).
func viRandom(p []byte) error {
	for len(p) > 0 {
		take := len(p)
		if take > 256 {
			take = 256
		}
		n, err := vi.Random(p[:take])
		if err != nil {
			return err
		}
		if n <= 0 {
			return ErrEntropyFailed
		}
		p = p[n:]
	}
	return nil
}

// Write sends data as one or more AEAD records (chunked to the 2^14 record
// limit), advancing only by confirmed sends.
func (c *TLSConn) Write(p []byte) (int, error) {
	if c.t != nil {
		if err := c.t.st.checkDeadline(); err != nil {
			return 0, err
		}
	}
	sent := 0
	for sent < len(p) {
		take := len(p) - sent
		if take > maxPlaintext {
			take = maxPlaintext
		}
		if err := c.cl.write(p[sent : sent+take]); err != nil {
			return sent, err
		}
		sent += take
	}
	return sent, nil
}

// Read returns application data.
func (c *TLSConn) Read(p []byte) (int, error) {
	if c.t != nil {
		if err := c.t.st.checkDeadline(); err != nil {
			return 0, err
		}
	}
	return c.cl.read(p)
}

// SetDeadline bounds all subsequent stream reads/writes, including buffered
// plaintext and progress. It never changes certificate validity or trust.
func (c *TLSConn) SetDeadline(deadline int64) {
	if c != nil && c.t != nil {
		c.t.st.deadlineAt = deadline
	}
}

// IsTimeout distinguishes a stream deadline from certificate/record errors.
func IsTimeout(err error) bool { return err == errStreamTimeout }

// IsPeerClosed reports orderly TLS close_notify or a drained TCP FIN.
func IsPeerClosed(err error) bool { return err == errStreamClosed }

// Close sends close_notify and tears the TCP connection down.
func (c *TLSConn) Close() error { return c.cl.close() }

// LastValidation reports the chain-validation verdict for troubleshooting.
func (c *TLSConn) LastValidation() validationResult { return c.cl.lastValidation }
