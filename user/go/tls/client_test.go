// Client pins: the ClientHello's wire shape, ServerHello / Certificate /
// CertificateVerify parsing against the RFC 8448 vectors, the Finished MAC,
// the record layer's decrypt path, and the state machine's gate. The full
// end-to-end handshake against a live server lives in interop_test.go.

package tls

import (
	"bytes"
	"testing"
)

func TestClientHelloShape(t *testing.T) {
	var random, sessionID, pub [32]byte
	for i := range random {
		random[i] = byte(i)
		sessionID[i] = byte(i + 1)
		pub[i] = byte(i + 2)
	}
	c := &client{host: "example.com"}
	ch := c.buildClientHello(random, sessionID, pub)

	if ch[0] != hsClientHello {
		t.Fatal("wrong handshake type")
	}
	bodyLen := int(ch[1])<<16 | int(ch[2])<<8 | int(ch[3])
	if bodyLen != len(ch)-4 {
		t.Fatalf("handshake length %d != %d", bodyLen, len(ch)-4)
	}

	r := newWireReader(ch[4:])
	if v, _ := r.u16(); v != versionTLS12 {
		t.Fatal("legacy_version wrong")
	}
	if _, err := r.take(32); err != nil { // random
		t.Fatal(err)
	}
	sid, _ := r.vec8()
	if len(sid) != 32 || !bytes.Equal(sid, sessionID[:]) {
		t.Fatal("session id wrong")
	}
	suites, _ := r.vec16()
	if !bytes.Equal(suites, []byte{0x13, 0x01}) {
		t.Fatal("cipher suites wrong")
	}
	if comp, _ := r.vec8(); len(comp) != 1 || comp[0] != 0 {
		t.Fatal("compression methods wrong")
	}
	exts, _ := r.vec16()
	er := newWireReader(exts)
	found := map[uint16]bool{}
	for !er.atEnd() {
		at, _ := er.u16()
		body, _ := er.vec16()
		found[at] = true
		switch at {
		case extServerName:
			nr := newWireReader(body)
			list, _ := nr.vec16()
			sr := newWireReader(list)
			if typ, _ := sr.u8(); typ != 0 {
				t.Fatal("SNI type wrong")
			}
			name, _ := sr.vec16()
			if string(name) != "example.com" {
				t.Fatalf("SNI %q", name)
			}
		case extSupportedVersions:
			vr := newWireReader(body)
			if n, _ := vr.u8(); n != 2 {
				t.Fatal("supported_versions list wrong")
			}
			if v, _ := vr.u16(); v != versionTLS13 {
				t.Fatal("supported version wrong")
			}
		case extKeyShare:
			kr := newWireReader(body)
			// KeyShareClientHello = u16 length || KeyShareEntry.
			if n, _ := kr.u16(); n != 36 {
				t.Fatalf("client_shares length %d", n)
			}
			if g, _ := kr.u16(); g != groupX25519 {
				t.Fatal("key share group wrong")
			}
			key, _ := kr.vec16()
			if !bytes.Equal(key, pub[:]) {
				t.Fatal("key share value wrong")
			}
		case extSignatureAlgorithms:
			sr := newWireReader(body)
			list, _ := sr.vec16()
			if len(list)%2 != 0 || len(list) < 2*len(offeredSigAlgs) {
				t.Fatal("sig algs list wrong")
			}
		}
	}
	for _, want := range []uint16{extServerName, extSupportedVersions, extSupportedGroups, extSignatureAlgorithms, extKeyShare, extPSKKeyExchangeModes} {
		if !found[want] {
			t.Fatalf("extension %d missing", want)
		}
	}
}

func TestLiteralIPOmitsSNIWithoutChangingVerificationName(t *testing.T) {
	for _, host := range []string{"10.0.0.2", "127.0.0.1", "::1"} {
		c := &client{host: host, verify: true}
		var empty [32]byte
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
			kind, err := er.u16()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := er.vec16(); err != nil {
				t.Fatal(err)
			}
			if kind == extServerName {
				t.Fatalf("IP literal emitted SNI: %s", host)
			}
		}
		if c.host != host || !c.verify {
			t.Fatal("IP identity verification changed")
		}
	}
}

type recordInput struct{ data []byte }

func (r *recordInput) read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	if n == 0 {
		return 0, errStreamClosed
	}
	return n, nil
}
func (r *recordInput) write([]byte) (int, error) { return 0, errTransport }
func (r *recordInput) close() error              { return nil }

func TestAuthenticatedCloseNotifyIsEOF(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
		tamper  bool
		eof     bool
	}{
		{"close", []byte{1, 0}, false, true},
		{"fatal", []byte{2, 40}, false, false},
		{"malformed", []byte{0}, false, false},
		{"tampered", []byte{1, 0}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cipher, _ := newAES128(make([]byte, 16))
			out := &loopbackTransport{}
			w := &client{t: out, state: stateConnected, writeKeysReady: true, writeCipher: cipher}
			if err := w.sendRecord(tc.payload, ctAlert); err != nil {
				t.Fatal(err)
			}
			if tc.tamper {
				out.written[6] ^= 1
			}
			r := &client{t: &recordInput{data: out.written}, state: stateConnected,
				readEncrypted: true, readCipher: cipher}
			_, err := r.read(make([]byte, 1))
			if (err == errStreamClosed) != tc.eof || err == nil {
				t.Fatalf("alert err=%v wantEOF=%v", err, tc.eof)
			}
		})
	}
}

func TestServerHelloRFC8448(t *testing.T) {
	shBytes := mustHex(t, rfc8448Flight[1].bytes) // ServerHello, header included
	suite, peer, ok, err := parseServerHello(shBytes[4:])
	if err != nil {
		t.Fatal(err)
	}
	if suite != suiteOffered {
		t.Fatalf("suite %x", suite)
	}
	if !ok {
		t.Fatal("key share missing")
	}
	if !bytes.Equal(peer[:], mustHex(t, rfc8448["server_public_key"])) {
		t.Fatal("peer key mismatch")
	}
}

func TestServerFinishedRFC8448(t *testing.T) {
	// Finished = HMAC(finished_key, Transcript-Hash(CH..CertificateVerify)).
	fk := mustHex(t, rfc8448["server_finished_key"])
	th := transcriptHash(rfc8448Flight[:5])
	mac := hmacSha256(fk, th[:])
	fin := mustHex(t, rfc8448Flight[5].bytes) // Finished message
	if fin[0] != hsFinished {
		t.Fatal("finished type wrong")
	}
	if !bytes.Equal(fin[4:], mac[:]) {
		t.Fatalf("server Finished mismatch")
	}
}

func TestRecordLayerRFC8448(t *testing.T) {
	// The RFC's client application_data record decrypts with the published
	// application traffic key and IV at sequence 0.
	key := [16]byte(mustHex(t, rfc8448["client_app_write_key"]))
	iv := [12]byte(mustHex(t, rfc8448["client_app_write_iv"]))
	rec := mustHex(t, rfc8448["client_app_record"])
	wantPT := mustHex(t, rfc8448["client_app_payload"])

	c := &client{readKeys: trafficKeys{key: key, iv: iv}, readEncrypted: true}
	c.readCipher, _ = newAES128(key[:])
	var inner byte
	pt, err := c.openRecord(rec, &inner)
	if err != nil {
		t.Fatal(err)
	}
	if inner != ctApplicationData {
		t.Fatalf("inner type %d", inner)
	}
	if !bytes.Equal(pt, wantPT) {
		t.Fatal("payload mismatch")
	}

	// A flipped ciphertext byte fails closed.
	bad := append([]byte{}, rec...)
	bad[6] ^= 0x40
	if _, err := c.openRecord(bad, &inner); err == nil {
		t.Fatal("tampered record accepted")
	}

	// A wrong sequence number changes the nonce: fails closed.
	c.readSeq = 1
	if _, err := c.openRecord(rec, &inner); err == nil {
		t.Fatal("wrong-seq record accepted")
	}
}

func TestRecordNonceLayout(t *testing.T) {
	iv := [12]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	n0 := recordNonce(&iv, 0)
	if n0 != iv {
		t.Fatal("seq 0 must be the bare IV")
	}
	n1 := recordNonce(&iv, 1)
	if n1[11] != iv[11]^1 {
		t.Fatal("seq must XOR the low byte")
	}
	nbig := recordNonce(&iv, 0x0102030405060708)
	if nbig[4] != iv[4]^0x01 || nbig[11] != iv[11]^0x08 {
		t.Fatal("seq XOR layout wrong")
	}
}

func TestCaptureCertificateFromRFC8448(t *testing.T) {
	cert := mustHex(t, rfc8448Flight[3].bytes) // Certificate message
	c := &client{}
	if err := c.captureCertificate(cert[4:]); err != nil {
		t.Fatal(err)
	}
	if c.leafDER == nil || len(c.leafDER) == 0 {
		t.Fatal("leaf not captured")
	}
	// The RFC 8448 leaf is 1024-bit RSA — below the 2048-bit floor the
	// parser enforces (the same floor x509.zig pins). The DER is captured
	// intact, and the parse must fail CLOSED with the specific algorithm
	// error, never accept a weak key.
	if len(c.leafDER) == 0 {
		t.Fatal("leaf not captured")
	}
	if _, err := parseCert(c.leafDER); err != errBadAlgorithm {
		t.Fatalf("leaf parse: %v, want errBadAlgorithm", err)
	}
}

func TestStateMachineGate(t *testing.T) {
	for s := stateIdle; s <= stateClosed; s++ {
		want := s == stateConnected
		if s.allowsApplicationData() != want {
			t.Fatalf("state %d allowsApplicationData = %v", s, !want)
		}
	}
}

func TestClientWriteRecordRoundTrip(t *testing.T) {
	// A sealed record through sendRecord opens again with the same keys.
	key := [16]byte(mustHex(t, rfc8448["client_app_write_key"]))
	iv := [12]byte(mustHex(t, rfc8448["client_app_write_iv"]))
	rw := &loopbackTransport{}
	c := &client{
		t:              rw,
		writeCipher:    mustAES(t, key[:]),
		writeKeys:      trafficKeys{key: key, iv: iv},
		writeKeysReady: true,
		state:          stateConnected,
	}
	payload := []byte("GET / HTTP/1.0\r\n\r\n")
	if err := c.sendRecord(payload, ctApplicationData); err != nil {
		t.Fatal(err)
	}
	if len(rw.written) < recordHeaderLen+17 {
		t.Fatal("record too short")
	}
	c2 := &client{readKeys: trafficKeys{key: key, iv: iv}, readEncrypted: true, readCipher: mustAES(t, key[:])}
	var inner byte
	pt, err := c2.openRecord(rw.written, &inner)
	if err != nil {
		t.Fatal(err)
	}
	if inner != ctApplicationData || !bytes.Equal(pt, payload) {
		t.Fatal("round-trip mismatch")
	}
}

type loopbackTransport struct {
	written []byte
}

func (l *loopbackTransport) read(p []byte) (int, error) { return 0, errTransport }
func (l *loopbackTransport) write(p []byte) (int, error) {
	l.written = append(l.written, p...)
	return len(p), nil
}
func (l *loopbackTransport) close() error { return nil }

func mustAES(t *testing.T, key []byte) *aes128 {
	t.Helper()
	a, err := newAES128(key)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
