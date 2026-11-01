// ALPN (RFC 7301, M212a #3872) pins: the ClientHello carries the
// application_layer_protocol_negotiation extension only when the caller
// offered a list; the EncryptedExtensions pick lands on the conn (and a
// pick outside the offered list fails the handshake); the interop legs
// against the host stdlib server live at the bottom.

package tls

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"net"
	"testing"
	"time"
)

func TestClientHelloALPNShape(t *testing.T) {
	var random, sessionID, pub [32]byte
	c := &client{host: "example.com", alpn: []string{"h2", "http/1.1"}}
	ch := c.buildClientHello(random, sessionID, pub)

	r := newWireReader(ch[4:])
	_, _ = r.u16()
	_, _ = r.take(32)
	_, _ = r.vec8()
	_, _ = r.vec16()
	_, _ = r.vec8()
	exts, err := r.vec16()
	if err != nil {
		t.Fatal(err)
	}
	er := newWireReader(exts)
	var alpnBody []byte
	for !er.atEnd() {
		at, _ := er.u16()
		body, _ := er.vec16()
		if at == extALPN {
			if alpnBody != nil {
				t.Fatal("ALPN extension duplicated")
			}
			alpnBody = body
		}
	}
	if alpnBody == nil {
		t.Fatal("ALPN extension missing")
	}
	ar := newWireReader(alpnBody)
	list, err := ar.vec16()
	if err != nil || !ar.atEnd() {
		t.Fatal("ProtocolNameList malformed")
	}
	lr := newWireReader(list)
	var names []string
	for !lr.atEnd() {
		n, err := lr.vec8()
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, string(n))
	}
	if len(names) != 2 || names[0] != "h2" || names[1] != "http/1.1" {
		t.Fatalf("ALPN names %v", names)
	}
}

func TestClientHelloNoALPNOfferedSendsNoExtension(t *testing.T) {
	var empty [32]byte
	c := &client{host: "example.com"}
	ch := c.buildClientHello(empty, empty, empty)
	r := newWireReader(ch[4:])
	_, _ = r.u16()
	_, _ = r.take(32)
	_, _ = r.vec8()
	_, _ = r.vec16()
	_, _ = r.vec8()
	exts, err := r.vec16()
	if err != nil {
		t.Fatal(err)
	}
	er := newWireReader(exts)
	for !er.atEnd() {
		at, _ := er.u16()
		_, _ = er.vec16()
		if at == extALPN {
			t.Fatal("ALPN extension present without an offer")
		}
	}
}

// alpnEE builds an EncryptedExtensions body carrying one ALPN pick.
func alpnEE(t *testing.T, pick string) []byte {
	var w byteWriter
	outer := w.reserve16()
	w.u16(extALPN)
	at := w.reserve16()
	inner := w.reserve16()
	w.u8(byte(len(pick)))
	w.raw([]byte(pick))
	w.patch16(inner)
	w.patch16(at)
	w.patch16(outer)
	return w.buf[:w.len]
}

func TestParseEncryptedExtensionsALPN(t *testing.T) {
	got, err := parseEncryptedExtensions(alpnEE(t, "h2"))
	if err != nil || got != "h2" {
		t.Fatalf("got %q err %v", got, err)
	}
	// An EE with no extensions negotiates nothing.
	empty := []byte{0, 0}
	got, err = parseEncryptedExtensions(empty)
	if err != nil || got != "" {
		t.Fatalf("empty EE got %q err %v", got, err)
	}
}

func TestALPNNegotiationState(t *testing.T) {
	c := &client{state: stateReceivedServerHello, alpn: []string{"h2", "http/1.1"}}
	body := alpnEE(t, "h2")
	msg := append([]byte{hsEncryptedExtensions, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}, body...)
	if err := c.processMessage(hsEncryptedExtensions, msg, [32]byte{}); err != nil {
		t.Fatal(err)
	}
	if c.alpnNegotiated != "h2" {
		t.Fatalf("negotiated %q", c.alpnNegotiated)
	}
	if c.state != stateReceivedEncryptedExtensions {
		t.Fatalf("state %v", c.state)
	}
}

func TestALPNUnofferedPickFails(t *testing.T) {
	c := &client{state: stateReceivedServerHello, alpn: []string{"h2"}}
	body := alpnEE(t, "h3")
	msg := append([]byte{hsEncryptedExtensions, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}, body...)
	err := c.processMessage(hsEncryptedExtensions, msg, [32]byte{})
	if !errors.Is(err, ErrALPNProtocol) {
		t.Fatalf("err %v, want ErrALPNProtocol", err)
	}
	// Unsolicited ALPN (no offer at all) is the same violation.
	c2 := &client{state: stateReceivedServerHello}
	if err := c2.processMessage(hsEncryptedExtensions, msg, [32]byte{}); !errors.Is(err, ErrALPNProtocol) {
		t.Fatalf("unsolicited err %v, want ErrALPNProtocol", err)
	}
}

func TestCheckALPNBounds(t *testing.T) {
	if err := checkALPN(nil); err != nil {
		t.Fatal(err)
	}
	if err := checkALPN([]string{"h2", "http/1.1"}); err != nil {
		t.Fatal(err)
	}
	if err := checkALPN([]string{""}); !errors.Is(err, ErrALPNList) {
		t.Fatalf("empty name err %v", err)
	}
	if err := checkALPN([]string{string(bytes.Repeat([]byte{'x'}, 256))}); !errors.Is(err, ErrALPNList) {
		t.Fatalf("oversized name err %v", err)
	}
	over := make([]string, alpnMaxProtocols+1)
	for i := range over {
		over[i] = "p"
	}
	if err := checkALPN(over); !errors.Is(err, ErrALPNList) {
		t.Fatalf("over-long list err %v", err)
	}
}

func TestInteropALPN(t *testing.T) {
	caDER, cert := buildTestChain(t, "interop.example.com")
	store := newTrustStore("interop")
	if err := store.addRoot(caDER); err != nil {
		t.Fatal(err)
	}
	addr, done := startTLSServer(t, cert, func(cfg *tls.Config) {
		cfg.NextProtos = []string{"h2"}
	})
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	nt := &netTransport{conn: conn}
	cl := newClient(nt, "interop.example.com", time.Now().Unix(), func(p []byte) error {
		_, err := rand.Read(p)
		return err
	}, true)
	cl.store = store
	cl.alpn = []string{"h2", "http/1.1"}
	if err := cl.handshake(); err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	if got := (&TLSConn{cl: cl}).ALPN(); got != "h2" {
		t.Fatalf("ALPN() = %q, want h2", got)
	}
	_ = cl.close()
	<-done
}

func TestInteropALPNAbsent(t *testing.T) {
	caDER, cert := buildTestChain(t, "interop.example.com")
	store := newTrustStore("interop")
	_ = store.addRoot(caDER)
	// Server carries no NextProtos: no ALPN extension is answered and the
	// connection still completes — the negotiated protocol reads "".
	addr, done := startTLSServer(t, cert, nil)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	nt := &netTransport{conn: conn}
	cl := newClient(nt, "interop.example.com", time.Now().Unix(), func(p []byte) error {
		_, err := rand.Read(p)
		return err
	}, true)
	cl.store = store
	cl.alpn = []string{"h2", "http/1.1"}
	if err := cl.handshake(); err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	if got := (&TLSConn{cl: cl}).ALPN(); got != "" {
		t.Fatalf("ALPN() = %q, want empty", got)
	}
	_ = cl.close()
	<-done
}
