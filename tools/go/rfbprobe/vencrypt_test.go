package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testCertFixture mints a self-signed Ed25519 leaf with the 10.0.0.1 IP
// SAN the probe pins, and writes its DER to a temp file — the same shape
// the gate's generated TLS.CRT carries.
func testCertFixture(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "virelai-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{netip.MustParseAddr("10.0.0.1").AsSlice()},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "TLS.CRT")
	if err := os.WriteFile(path, der, 0o600); err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv, Leaf: cert}, path
}

// vencryptServer drives a scripted guest over a net.Pipe end: the
// plaintext VeNCrypt 0.2 exchange, a real TLS 1.3 handshake, then
// accept/refuse on the scripted credential verdict, and a real
// ServerInit + raw patch when accepted.
func vencryptServer(t *testing.T, c net.Conn, cert tls.Certificate, expectUser, expectPass string, accept bool) {
	t.Helper()
	defer c.Close()
	mustRead := func(n int) []byte {
		t.Helper()
		b := make([]byte, n)
		if _, err := io.ReadFull(c, b); err != nil {
			t.Errorf("server read %d: %v", n, err)
		}
		return b
	}
	mustWrite := func(b []byte) {
		t.Helper()
		if _, err := c.Write(b); err != nil {
			t.Errorf("server write %x: %v", b, err)
		}
	}
	mustWrite([]byte("RFB 003.008\n"))
	if got := string(mustRead(12)); got != "RFB 003.008\n" {
		t.Errorf("client banner %q", got)
	}
	mustWrite([]byte{1, secVeNCrypt})
	if got := mustRead(1); got[0] != secVeNCrypt {
		t.Errorf("client security select %x", got)
	}
	mustWrite([]byte{0, 2})
	if got := mustRead(2); !bytes.Equal(got, []byte{0, 2}) {
		t.Errorf("client vencrypt version %x", got)
	}
	var subs [6]byte
	subs[1] = 1
	binary.BigEndian.PutUint32(subs[2:], vencryptSubX509)
	mustWrite(subs[:])
	if got := mustRead(4); binary.BigEndian.Uint32(got) != vencryptSubX509 {
		t.Errorf("client subtype pick %x", got)
	}
	tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{cert}, MaxVersion: tls.VersionTLS13})
	if err := tc.Handshake(); err != nil {
		t.Errorf("server TLS handshake: %v", err)
		return
	}
	mustReadTLS := func(n int) []byte {
		t.Helper()
		b := make([]byte, n)
		if _, err := io.ReadFull(tc, b); err != nil {
			t.Errorf("server TLS read %d: %v", n, err)
		}
		return b
	}
	ulen := binary.BigEndian.Uint32(mustReadTLS(4))
	u := string(mustReadTLS(int(ulen)))
	plen := binary.BigEndian.Uint32(mustReadTLS(4))
	p := string(mustReadTLS(int(plen)))
	if u != expectUser || p != expectPass {
		t.Errorf("credentials %q/%q, want %q/%q", u, p, expectUser, expectPass)
	}
	if !accept {
		reason := "rfb: X509Plain credential refused"
		refusal := make([]byte, 8+len(reason))
		binary.BigEndian.PutUint32(refusal[0:], 1)
		binary.BigEndian.PutUint32(refusal[4:], uint32(len(reason)))
		copy(refusal[8:], reason)
		if _, err := tc.Write(refusal); err != nil {
			t.Errorf("refusal write: %v", err)
		}
		tc.Close()
		return
	}
	if _, err := tc.Write([]byte{0, 0, 0, 0}); err != nil {
		t.Errorf("SecurityResult write: %v", err)
		return
	}
	init := make([]byte, 1)
	if _, err := tc.Read(init); err != nil || init[0] != 1 {
		t.Errorf("ClientInit: %v %x", err, init)
		return
	}
	var head [24]byte
	binary.BigEndian.PutUint16(head[0:2], frameW)
	binary.BigEndian.PutUint16(head[2:4], frameH)
	head[4], head[5], head[6], head[7] = 32, 24, 0, 1
	head[14], head[15], head[16] = 16, 8, 0
	binary.BigEndian.PutUint32(head[20:24], uint32(len("VirelaiOS")))
	if _, err := tc.Write(append(head[:], "VirelaiOS"...)); err != nil {
		t.Errorf("ServerInit write: %v", err)
		return
	}
	// SetEncodings + the patch request arrive first; the pointer pair
	// follows once the probe has its pixels.
	want := []byte{2, 0, 0, 1, 0, 0, 0, 0, 3, 0, 0, patchX, 0, patchY, 0, patchW, 0, patchH}
	if got := mustReadTLS(len(want)); !bytes.Equal(got, want) {
		t.Errorf("client messages: got=%x want=%x", got, want)
		return
	}
	upd := []byte{0, 0, 0, 1}
	upd = append(upd, make([]byte, 12)...)
	binary.BigEndian.PutUint16(upd[4:6], patchX)
	binary.BigEndian.PutUint16(upd[6:8], patchY)
	binary.BigEndian.PutUint16(upd[8:10], patchW)
	binary.BigEndian.PutUint16(upd[10:12], patchH)
	for i := 0; i < patchW*patchH; i++ {
		upd = append(upd, 0x48, 0x37, 0x2d, 0)
	}
	if _, err := tc.Write(upd); err != nil {
		t.Errorf("frame write: %v", err)
	}
	press := append(pointer(1, dismissX, dismissY), pointer(0, dismissX, dismissY)...)
	if got := mustReadTLS(len(press)); !bytes.Equal(got, press) {
		t.Errorf("pointer pair: got=%x want=%x", got, press)
	}
	tc.Close()
}

func TestVencryptFullPath(t *testing.T) {
	cert, certPath := testCertFixture(t)
	srv, cli := net.Pipe()
	go vencryptServer(t, srv, cert, "vncuser", "RFB-CRED-01", true)
	if err := probeVencrypt(cli, cli, certPath, "vncuser", "RFB-CRED-01", "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
}

func TestVencryptCredentialRefused(t *testing.T) {
	cert, certPath := testCertFixture(t)
	srv, cli := net.Pipe()
	go vencryptServer(t, srv, cert, "vncuser", "RFB-CRED-WRONG", false)
	if err := probeVencrypt(cli, cli, certPath, "vncuser", "RFB-CRED-WRONG", "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
}

// The security-offer read must consume the count byte and the type list
// exactly — a regression once read count+first-type together and then
// waited on a byte the seat never sends, deadlocking the lane.
func TestVencryptOfferBoundary(t *testing.T) {
	srv, cli := net.Pipe()
	go func() {
		defer srv.Close()
		io.WriteString(srv, "RFB 003.008\n")
		io.ReadFull(srv, make([]byte, 12))
		srv.Write([]byte{1, secVeNCrypt})
	}()
	cert, certPath := testCertFixture(t)
	_ = cert
	err := probeVencrypt(cli, cli, certPath, "u", "p", "10.0.0.1")
	if err == nil || strings.Contains(err.Error(), "security type") {
		t.Fatalf("expected progress past the security offer, got %v", err)
	}
}

func TestVencryptRejectsNonVencryptOffer(t *testing.T) {
	srv, cli := net.Pipe()
	go func() {
		defer srv.Close()
		io.WriteString(srv, "RFB 003.008\n")
		io.ReadFull(srv, make([]byte, 12))
		srv.Write([]byte{1, 2}) // VNC auth, not VeNCrypt
	}()
	err := probeVencrypt(cli, cli, "unused", "u", "p", "10.0.0.1")
	want := fmt.Sprintf("%v", []byte{2})
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("expected non-VeNCrypt offer refusal naming %s, got %v", want, err)
	}
}
