// VeNCrypt verifier (M213c): the probe grown a TLS mode. It speaks RFB
// 3.8 security type 19 (VeNCrypt 0.2), picks subtype X509Plain (262),
// then handshakes the seat's in-tree TLS 1.3 server half with the host
// Go crypto/tls client — the same stack family the bridge already uses,
// BSD-licensed. The trust anchor is the staged fixture certificate, so
// the X509 subtype's chain verification is real: RootCAs + ServerName,
// not a skip-verify. Inside the channel it sends the Plain credential,
// completes ClientInit/ServerInit, reads a real frame, and routes one
// pointer event — auth + frame is the whole deliverable.
//
// Third-party reachability answer recorded for the card: macOS Screen
// Sharing offers nothing VeNCrypt-shaped (M84e's type-2 bridge exists
// for that reason), and the only VeNCrypt-capable viewers (TigerVNC and
// family) are GPL — barred as a host tool or test reference. This probe
// is the reachable verifier.

package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

const (
	secVeNCrypt       = 19
	vencryptSubX509   = 262 // X509Plain
	vencryptFieldMax  = 255
	defaultVerifyName = "10.0.0.1" // the hermetic address and fixture IP SAN
)

// streamConn adapts the probe's stdin/stdout byte stream to net.Conn for
// crypto/tls. Deadlines are no-ops: the runner's stream and the seat's
// own budgets own timing, and the gate asserts on its prints.
type streamConn struct {
	r io.Reader
	w io.Writer
}

type streamAddr string

func (a streamAddr) Network() string { return "rfb-stream" }
func (a streamAddr) String() string  { return string(a) }

func (c streamConn) Read(p []byte) (int, error)       { return c.r.Read(p) }
func (c streamConn) Write(p []byte) (int, error)      { return c.w.Write(p) }
func (c streamConn) Close() error                     { return nil }
func (c streamConn) LocalAddr() net.Addr              { return streamAddr("viewer") }
func (c streamConn) RemoteAddr() net.Addr             { return streamAddr("guest") }
func (c streamConn) SetDeadline(time.Time) error      { return nil }
func (c streamConn) SetReadDeadline(time.Time) error  { return nil }
func (c streamConn) SetWriteDeadline(time.Time) error { return nil }

func probeVencrypt(r io.Reader, w io.Writer, certPath, user, pass, serverName string) error {
	if certPath == "" {
		return fmt.Errorf("-mode=vencrypt requires -vencrypt-cert")
	}
	if err := readBanner(r); err != nil {
		return err
	}
	if err := write(w, []byte("RFB 003.008\n")); err != nil {
		return err
	}
	// Security offer: the VeNCrypt lane must offer type 19, and nothing
	// else is acceptable — an offer of None alongside would be reported.
	var offer [32]byte
	if _, err := io.ReadFull(r, offer[:1]); err != nil {
		return fmt.Errorf("security types: %w", err)
	}
	n := int(offer[0])
	if n == 0 || n > len(offer)-1 {
		return fmt.Errorf("security type count %d", n)
	}
	types := offer[1 : 1+n]
	if _, err := io.ReadFull(r, types); err != nil {
		return fmt.Errorf("security types: %w", err)
	}
	if n != 1 || types[0] != secVeNCrypt {
		return fmt.Errorf("security types %v, want exactly VeNCrypt (19)", types)
	}
	fmt.Fprintln(os.Stderr, "RFBPROBE: seat offers VeNCrypt (19) only")
	if err := write(w, []byte{secVeNCrypt}); err != nil {
		return err
	}
	// VeNCrypt 0.2: server version, client version, ack, subtype list.
	var version [2]byte
	if _, err := io.ReadFull(r, version[:]); err != nil {
		return fmt.Errorf("vencrypt version: %w", err)
	}
	if version != [2]byte{0, 2} {
		return fmt.Errorf("vencrypt version %v, want 0.2", version)
	}
	if err := write(w, []byte{0, 2}); err != nil {
		return err
	}
	var ack [1]byte
	if _, err := io.ReadFull(r, ack[:]); err != nil {
		return fmt.Errorf("vencrypt version ack: %w", err)
	}
	if ack[0] != 0 {
		return fmt.Errorf("vencrypt version refused (ack %d)", ack[0])
	}
	var count [1]byte
	if _, err := io.ReadFull(r, count[:]); err != nil {
		return fmt.Errorf("vencrypt subtypes: %w", err)
	}
	subs := make([]byte, 4*int(count[0]))
	if _, err := io.ReadFull(r, subs); err != nil {
		return fmt.Errorf("vencrypt subtypes: %w", err)
	}
	offered := false
	for i := 0; i+4 <= len(subs); i += 4 {
		if binary.BigEndian.Uint32(subs[i:]) == vencryptSubX509 {
			offered = true
		}
	}
	if !offered {
		return fmt.Errorf("vencrypt subtypes %x, want X509Plain (262)", subs)
	}
	fmt.Fprintln(os.Stderr, "RFBPROBE: VeNCrypt 0.2, picking subtype X509Plain (262)")
	var pick [4]byte
	binary.BigEndian.PutUint32(pick[:], vencryptSubX509)
	if err := write(w, pick[:]); err != nil {
		return err
	}

	// TLS 1.3 with the fixture leaf as the only trust anchor: the chain
	// and the Ed25519 CertificateVerify are verified for real.
	der, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("vencrypt cert fixture: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return fmt.Errorf("vencrypt cert fixture parse: %w", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	tc := tls.Client(streamConn{r: r, w: w}, &tls.Config{
		MinVersion:       tls.VersionTLS13,
		MaxVersion:       tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.X25519},
		RootCAs:          pool,
		ServerName:       serverName,
	})
	if err := tc.Handshake(); err != nil {
		return fmt.Errorf("vencrypt TLS handshake: %w", err)
	}
	st := tc.ConnectionState()
	fmt.Fprintf(os.Stderr, "RFBPROBE: TLS 1.3 %s x25519; peer cert %q verified against fixture\n",
		tls.CipherSuiteName(st.CipherSuite), leaf.Subject.CommonName)

	// X509Plain inside the channel: u32 user-length + user, u32
	// password-length + password, then SecurityResult.
	cred := make([]byte, 8+len(user)+len(pass))
	if len(user) > vencryptFieldMax || len(pass) > vencryptFieldMax {
		return fmt.Errorf("vencrypt credential fields bounded at %d", vencryptFieldMax)
	}
	binary.BigEndian.PutUint32(cred[0:4], uint32(len(user)))
	copy(cred[4:], user)
	binary.BigEndian.PutUint32(cred[4+len(user):], uint32(len(pass)))
	copy(cred[8+len(user):], pass)
	if err := write(tc, cred); err != nil {
		return err
	}
	var result [4]byte
	if _, err := io.ReadFull(tc, result[:]); err != nil {
		return fmt.Errorf("vencrypt SecurityResult: %w", err)
	}
	if binary.BigEndian.Uint32(result[:]) != 0 {
		var rlen [4]byte
		if _, err := io.ReadFull(tc, rlen[:]); err != nil {
			return fmt.Errorf("vencrypt refusal reason: %w", err)
		}
		reason := make([]byte, binary.BigEndian.Uint32(rlen[:]))
		if _, err := io.ReadFull(tc, reason); err != nil {
			return fmt.Errorf("vencrypt refusal reason: %w", err)
		}
		fmt.Fprintf(os.Stderr, "RFBPROBE: X509Plain refused: %s\n", reason)
		return expectClose(tc, "after the credential refusal")
	}
	fmt.Fprintln(os.Stderr, "RFBPROBE: X509Plain auth accepted")

	// Established session: ClientInit, ServerInit, a real frame, and one
	// routed pointer press — the card's auth+frame bar end to end.
	if err := write(tc, []byte{1}); err != nil {
		return err
	}
	name, err := readServerInit(tc)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "RFBPROBE: RFB 3.8 X509Plain %dx%d %s\n", frameW, frameH, name)
	// Same composite settle as the pixels leg: 'gocalc: present' is the
	// app's marker; the seat paints it on its own frame clock.
	time.Sleep(settle)
	if err := requestCalcPatch(tc, tc); err != nil {
		return err
	}
	if err := write(tc, concat(pointer(1, dismissX, dismissY), pointer(0, dismissX, dismissY))); err != nil {
		return err
	}
	time.Sleep(settle)
	fmt.Fprintln(os.Stderr, "RFBPROBE: VeNCrypt auth + frame + input complete")
	return nil
}
