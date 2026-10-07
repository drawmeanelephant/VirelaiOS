// TLS 1.3 client — the 1-RTT handshake driver and the application record
// path (RFC 8446), the Go mirror of user/src/lib/tls/client.zig's contract:
// TLS_AES_128_GCM_SHA256 + x25519 only, full certificate verification against
// a configured trust store, no PSK / 0-RTT / client certificates, and
// HelloRetryRequest rejected (a missing key share surfaces as
// ErrUnsupportedGroup) rather than mishandled.
//
// The handshake is a state machine: only `connected` permits application
// data, and `connected` is reachable only after the server's Finished
// verifies. Pinned end-to-end against RFC 8448's published flight in
// client_test.go and against a live Go-stdlib TLS 1.3 server (interop.go).

package tls

// record layer bounds (RFC 8446 §5.2: length MUST NOT exceed 2^14 + 256).
const (
	recordHeaderLen  = 5
	maxPlaintext     = 16384
	maxCiphertext    = maxPlaintext + 256
	maxHandshakeMsg  = 16 * 1024
	maxIntermediates = 8
	maxCertDER       = 4096
	keyLen           = 16
	ivLen            = 12
)

// content types and handshake types (RFC 8446 §5.1, §4).
const (
	ctChangeCipherSpec = 20
	ctAlert            = 21
	ctHandshake        = 22
	ctApplicationData  = 23

	hsClientHello         = 1
	hsServerHello         = 2
	hsNewSessionTicket    = 4
	hsEncryptedExtensions = 8
	hsCertificate         = 11
	hsCertificateRequest  = 13
	hsCertificateVerify   = 15
	hsFinished            = 20
	hsKeyUpdate           = 24
)

// extension numbers and the offered suite / signature schemes. The offer
// lists exactly what verifyCertificateVerify can check — never more (an
// advertised scheme the client cannot verify is a downgrade-shaped lie).
const (
	extServerName          = 0
	extSupportedGroups     = 10
	extSignatureAlgorithms = 13
	extSupportedVersions   = 43
	extKeyShare            = 51
	extPSKKeyExchangeModes = 45

	groupX25519  = 0x001d
	suiteOffered = 0x1301 // TLS_AES_128_GCM_SHA256
	versionTLS12 = 0x0303
	versionTLS13 = 0x0304

	sigEcdsaSecp256r1Sha256 = 0x0403
	sigEcdsaSecp384r1Sha384 = 0x0503
	sigRsaPssRsaeSha256     = 0x0804
	sigRsaPssRsaeSha384     = 0x0805
	sigRsaPssRsaeSha512     = 0x0806
	sigRsaPkcs1Sha256       = 0x0401
	sigRsaPkcs1Sha384       = 0x0501
	sigRsaPkcs1Sha512       = 0x0601
)

// offeredSigAlgs is the ClientHello signature_algorithms list.
var offeredSigAlgs = []uint16{
	sigEcdsaSecp256r1Sha256,
	sigEcdsaSecp384r1Sha384,
	sigRsaPssRsaeSha256,
	sigRsaPssRsaeSha384,
	sigRsaPssRsaeSha512,
	sigRsaPkcs1Sha256,
	sigRsaPkcs1Sha384,
	sigRsaPkcs1Sha512,
}

// serverCVContext is the RFC 8446 §4.4.3 context string a server signs.
const serverCVContext = "TLS 1.3, server CertificateVerify"

// transport is the byte-stream the client drives (the vi.Conn seam in the
// guest; net.Conn in host tests).
type transport interface {
	// read returns at least one byte or an error; (0, nil) is EOF.
	read(p []byte) (int, error)
	// write writes all of p or returns an error.
	write(p []byte) (int, error)
	close() error
}

// tlsState is the handshake state machine's shape: application data is
// reachable only from stateConnected.
type tlsState int

const (
	stateIdle tlsState = iota
	stateSentClientHello
	stateReceivedServerHello
	stateReceivedEncryptedExtensions
	stateReceivedCertificate
	stateReceivedCertificateVerify
	stateReceivedFinished
	stateConnected
	stateFailed
	stateClosed
)

func (s tlsState) allowsApplicationData() bool { return s == stateConnected }

// trafficKeys is one direction's AEAD material.
type trafficKeys struct {
	key [keyLen]byte
	iv  [ivLen]byte
}

// recordNonce is RFC 8446 §5.3: iv XOR the 64-bit sequence number,
// right-aligned.
func recordNonce(iv *[12]byte, seq uint64) [12]byte {
	var n [12]byte
	copy(n[:], iv[:])
	v := uint64(n[4])<<56 | uint64(n[5])<<48 | uint64(n[6])<<40 | uint64(n[7])<<32 |
		uint64(n[8])<<24 | uint64(n[9])<<16 | uint64(n[10])<<8 | uint64(n[11])
	v ^= seq
	n[4] = byte(v >> 56)
	n[5] = byte(v >> 48)
	n[6] = byte(v >> 40)
	n[7] = byte(v >> 32)
	n[8] = byte(v >> 24)
	n[9] = byte(v >> 16)
	n[10] = byte(v >> 8)
	n[11] = byte(v)
	return n
}

// client is the TLS 1.3 client over a transport.
type client struct {
	t      transport
	host   string // SNI and hostname verification name
	now    int64
	rand   func(p []byte) error
	verify bool
	// store is the trust anchor set for chain validation; nil means the
	// vendored default (the guest's pinned root).
	store *trustStore

	state      tlsState
	transcript sha256Digest

	hsBuf [maxHandshakeMsg]byte
	hsLen int

	// plainBuf is the decrypted-record scratch; pending holds the unread
	// tail of a record whose application data exceeded the caller's buffer.
	plainBuf [maxPlaintext]byte
	pending  []byte

	// Separate AES contexts per direction: the round keys are key-specific,
	// and the client's and server's traffic keys differ.
	readCipher  *aes128
	writeCipher *aes128

	readKeys       trafficKeys
	writeKeys      trafficKeys
	readSeq        uint64
	writeSeq       uint64
	readEncrypted  bool
	writeKeysReady bool

	// handshake secrets kept for the application-key derivation
	handshakeSecret   [32]byte
	clientFinishedKey [32]byte
	serverFinishedKey [32]byte

	// captured chain (leaf copied out of hsBuf, which is compacted)
	leafDER           []byte
	intermediates     [][]byte
	pendingClientCert bool

	// negotiated
	lastValidation validationResult
}

func newClient(t transport, host string, now int64, rand func(p []byte) error, verify bool) *client {
	return &client{t: t, host: host, now: now, rand: rand, verify: verify}
}

// sendRecord frames one record. Before write keys exist the ClientHello goes
// out as a plaintext handshake record; after, the outer type is always
// application_data and the payload is the AEAD-protected inner record.
func (c *client) sendRecord(payload []byte, inner byte) error {
	if !c.writeKeysReady {
		buf := make([]byte, recordHeaderLen+len(payload))
		buf[0] = ctHandshake
		buf[1] = 0x03
		buf[2] = 0x03
		buf[3] = byte(len(payload) >> 8)
		buf[4] = byte(len(payload))
		copy(buf[recordHeaderLen:], payload)
		_, err := c.t.write(buf)
		return err
	}
	innerBuf := make([]byte, len(payload)+1)
	copy(innerBuf, payload)
	innerBuf[len(payload)] = inner

	ctLen := len(innerBuf) + 16
	out := make([]byte, recordHeaderLen+ctLen)
	out[0] = ctApplicationData
	out[1] = 0x03
	out[2] = 0x03
	out[3] = byte(ctLen >> 8)
	out[4] = byte(ctLen)
	nonce := recordNonce(&c.writeKeys.iv, c.writeSeq)
	// The 5-byte record header is the AAD (RFC 8446 §5.2).
	if err := gcmSeal(c.writeCipher, out[recordHeaderLen:], innerBuf, out[:recordHeaderLen], nonce[:]); err != nil {
		return err
	}
	c.writeSeq++
	_, werr := c.t.write(out)
	return werr
}

// readRecord reads and decrypts one record into c.plainBuf. A
// change_cipher_spec is consumed and reported as length 0.
func (c *client) readRecord() (int, byte, error) {
	var hdr [recordHeaderLen]byte
	if err := c.readAll(hdr[:]); err != nil {
		return 0, 0, err
	}
	ct := hdr[0]
	l := int(hdr[3])<<8 | int(hdr[4])
	if l > maxCiphertext {
		return 0, 0, errRecordTooLarge
	}
	rec := make([]byte, recordHeaderLen+l)
	copy(rec, hdr[:])
	if err := c.readAll(rec[recordHeaderLen:]); err != nil {
		return 0, 0, err
	}

	if ct == ctChangeCipherSpec {
		return 0, ctChangeCipherSpec, nil
	}
	if ct == ctAlert {
		if c.readEncrypted {
			// Alerts are AEAD-protected once keys are installed; an
			// unauthenticable alert is still an alert (fail closed).
			var innerType byte
			if _, err := c.openRecord(rec, &innerType); err != nil {
				return 0, 0, ErrAlertReceived
			}
			c.readSeq++
		}
		return 0, 0, ErrAlertReceived
	}
	if !c.readEncrypted {
		copy(c.plainBuf[:l], rec[recordHeaderLen:recordHeaderLen+l])
		return l, ct, nil
	}
	var innerType byte
	pt, err := c.openRecord(rec, &innerType)
	if err != nil {
		return 0, 0, ErrRecordDecryptFailed // AEAD failure: fail closed
	}
	c.readSeq++
	copy(c.plainBuf[:len(pt)], pt)
	return len(pt), innerType, nil
}

// openRecord decrypts an AEAD record: TLSInnerPlaintext = content || type ||
// zeros, the type is the last non-zero byte.
func (c *client) openRecord(rec []byte, innerType *byte) ([]byte, error) {
	if len(rec) < recordHeaderLen+1+16 {
		return nil, errTruncatedRecord
	}
	declared := int(rec[3])<<8 | int(rec[4])
	if declared != len(rec)-recordHeaderLen {
		return nil, errTruncatedRecord
	}
	if rec[0] != ctApplicationData {
		return nil, errInnerTypeBad
	}
	ctLen := declared - 16
	nonce := recordNonce(&c.readKeys.iv, c.readSeq)
	pt := make([]byte, ctLen)
	// The AEAD input is the full ciphertext||tag; the 5-byte header is the AAD.
	if err := gcmOpen(c.readCipher, pt, rec[recordHeaderLen:recordHeaderLen+declared], rec[:recordHeaderLen], nonce[:]); err != nil {
		return nil, err
	}
	end := ctLen
	for end > 0 && pt[end-1] == 0 {
		end--
	}
	if end == 0 {
		return nil, errInnerTypeMissing
	}
	t := pt[end-1]
	switch t {
	case ctHandshake, ctApplicationData, ctAlert, ctChangeCipherSpec:
	default:
		return nil, errInnerTypeBad
	}
	*innerType = t
	return pt[:end-1], nil
}

// readAll fills p (the transport may return short reads — the vi.Conn seam
// caps at one 192-byte kernel segment).
func (c *client) readAll(p []byte) error {
	off := 0
	for off < len(p) {
		n, err := c.t.read(p[off:])
		if err != nil {
			return err
		}
		if n == 0 {
			return errTransport
		}
		off += n
	}
	return nil
}

// handshake runs ClientHello through the client Finished.
func (c *client) handshake() error {
	var random, sessionID, priv [32]byte
	if err := c.rand(random[:]); err != nil {
		return err
	}
	if err := c.rand(sessionID[:]); err != nil {
		return err
	}
	if err := c.rand(priv[:]); err != nil {
		return err
	}
	pub, ok := x25519Base(priv)
	if !ok {
		return errKeyShareRejected
	}

	ch := c.buildClientHello(random, sessionID, pub)
	c.transcript = sha256Init()
	c.transcript.write(ch)
	if err := c.sendRecord(ch, ctHandshake); err != nil {
		return err
	}
	c.state = stateSentClientHello

	for {
		n, inner, err := c.readRecord()
		if err != nil {
			return err
		}
		if inner == ctChangeCipherSpec {
			continue
		}
		if inner == ctApplicationData || inner == ctHandshake {
			// Decrypted handshake bytes (application_data is the AEAD outer
			// type once keys are installed).
			if err := c.feedHandshake(c.plainBuf[:n], priv); err != nil {
				return err
			}
			if c.state == stateReceivedFinished {
				return c.finishHandshake()
			}
			continue
		}
		if inner == ctAlert {
			return ErrAlertReceived
		}
	}
}

func (c *client) buildClientHello(random, sessionID, x25519Pub [32]byte) []byte {
	var w byteWriter
	w.u8(hsClientHello)
	hdrLenAt := w.len
	w.u24(0)

	w.u16(versionTLS12)
	w.raw(random[:])
	w.u8(32) // legacy_session_id (middlebox compat)
	w.raw(sessionID[:])

	// cipher_suites
	at := w.reserve16()
	w.u16(suiteOffered)
	w.patch16(at)
	// legacy_compression_methods
	w.u8(1)
	w.u8(0)

	extAt := w.reserve16()

	// RFC 6066 prohibits literal IP addresses in host_name. Verification
	// still uses c.host, including exact iPAddress SAN checks.
	if _, literal := parseIPLiteral([]byte(c.host)); !literal {
		w.u16(extServerName)
		at := w.reserve16()
		inner := w.reserve16()
		w.u8(0) // host_name
		nameAt := w.reserve16()
		w.raw([]byte(c.host))
		w.patch16(nameAt)
		w.patch16(inner)
		w.patch16(at)
	}

	// supported_versions
	{
		w.u16(extSupportedVersions)
		at := w.reserve16()
		w.u8(2)
		w.u16(versionTLS13)
		w.patch16(at)
	}

	// supported_groups
	{
		w.u16(extSupportedGroups)
		at := w.reserve16()
		inner := w.reserve16()
		list := w.reserve16()
		w.u16(groupX25519)
		w.patch16(list)
		w.patch16(inner)
		w.patch16(at)
	}

	// signature_algorithms
	{
		w.u16(extSignatureAlgorithms)
		at := w.reserve16()
		inner := w.reserve16()
		list := w.reserve16()
		for _, s := range offeredSigAlgs {
			w.u16(s)
		}
		w.patch16(list)
		w.patch16(inner)
		w.patch16(at)
	}

	// key_share
	{
		w.u16(extKeyShare)
		at := w.reserve16()
		inner := w.reserve16()
		w.u16(groupX25519)
		w.u16(32)
		w.raw(x25519Pub[:])
		w.patch16(inner)
		w.patch16(at)
	}

	// psk_key_exchange_modes (present but no PSK offered)
	{
		w.u16(extPSKKeyExchangeModes)
		at := w.reserve16()
		w.u8(1)
		w.u8(1) // psk_dhe_ke
		w.patch16(at)
	}

	w.patch16(extAt)
	// Back-patch the handshake header length (hdrLenAt is the u24 field).
	bodyLen := w.len - hdrLenAt - 3
	w.buf[hdrLenAt] = byte(bodyLen >> 16)
	w.buf[hdrLenAt+1] = byte(bodyLen >> 8)
	w.buf[hdrLenAt+2] = byte(bodyLen)
	return w.buf[:w.len]
}

// byteWriter is the fixed-buffer message writer (back-patching lengths).
type byteWriter struct {
	buf []byte
	len int
}

func (w *byteWriter) ensure(n int) {
	for len(w.buf)-w.len < n {
		if w.buf == nil {
			w.buf = make([]byte, 256)
			continue
		}
		grown := make([]byte, len(w.buf)*2)
		copy(grown, w.buf)
		w.buf = grown
	}
}

func (w *byteWriter) u8(v byte) {
	w.ensure(1)
	w.buf[w.len] = v
	w.len++
}

func (w *byteWriter) u16(v uint16) {
	w.ensure(2)
	w.buf[w.len] = byte(v >> 8)
	w.buf[w.len+1] = byte(v)
	w.len += 2
}

func (w *byteWriter) u24(v uint32) {
	w.ensure(3)
	w.buf[w.len] = byte(v >> 16)
	w.buf[w.len+1] = byte(v >> 8)
	w.buf[w.len+2] = byte(v)
	w.len += 3
}

func (w *byteWriter) raw(p []byte) {
	w.ensure(len(p))
	copy(w.buf[w.len:], p)
	w.len += len(p)
}

func (w *byteWriter) reserve16() int {
	at := w.len
	w.u16(0)
	return at
}

func (w *byteWriter) patch16(at int) {
	n := w.len - at - 2
	w.buf[at] = byte(n >> 8)
	w.buf[at+1] = byte(n)
}

// feedHandshake appends decrypted bytes and processes every complete message.
func (c *client) feedHandshake(chunk []byte, priv [32]byte) error {
	if c.hsLen+len(chunk) > len(c.hsBuf) {
		return errHandshakeTooLarge
	}
	copy(c.hsBuf[c.hsLen:], chunk)
	c.hsLen += len(chunk)

	off := 0
	for c.hsLen-off >= 4 {
		msgType := c.hsBuf[off]
		total := 4 + (int(c.hsBuf[off+1])<<16 | int(c.hsBuf[off+2])<<8 | int(c.hsBuf[off+3]))
		if c.hsLen-off < total {
			break
		}
		msg := c.hsBuf[off : off+total]
		if err := c.processMessage(msgType, msg, priv); err != nil {
			return err
		}
		off += total
	}
	if off > 0 {
		copy(c.hsBuf[:], c.hsBuf[off:c.hsLen])
		c.hsLen -= off
	}
	return nil
}

func (c *client) processMessage(msgType byte, msg []byte, priv [32]byte) error {
	body := msg[4:]
	switch msgType {
	case hsServerHello:
		if c.state != stateSentClientHello {
			return ErrUnexpectedState
		}
		suite, peer, ok, err := parseServerHello(body)
		if err != nil {
			return err
		}
		if suite != suiteOffered {
			return ErrUnsupportedSuite
		}
		if !ok {
			return ErrUnsupportedGroup
		}
		c.transcript.write(msg)
		if err := c.deriveHandshakeKeys(peer, priv); err != nil {
			return err
		}
		c.state = stateReceivedServerHello
	case hsEncryptedExtensions:
		if c.state != stateReceivedServerHello {
			return ErrUnexpectedState
		}
		if err := parseEncryptedExtensions(body); err != nil {
			return err
		}
		c.transcript.write(msg)
		c.state = stateReceivedEncryptedExtensions
	case hsCertificate:
		if c.state != stateReceivedEncryptedExtensions {
			return ErrUnexpectedState
		}
		if err := c.captureCertificate(body); err != nil {
			return err
		}
		c.transcript.write(msg)
		c.state = stateReceivedCertificate
	case hsCertificateVerify:
		if c.state != stateReceivedCertificate {
			return ErrUnexpectedState
		}
		scheme, sig, err := parseCertificateVerify(body)
		if err != nil {
			return err
		}
		// The signature covers the transcript through Certificate, so hash
		// BEFORE adding this message.
		if !c.verifyCertificateVerify(scheme, sig) {
			return ErrCertificateVerify
		}
		c.transcript.write(msg)
		c.state = stateReceivedCertificateVerify
	case hsFinished:
		if c.state != stateReceivedCertificateVerify {
			return ErrUnexpectedState
		}
		if len(body) != 32 {
			return ErrFinishedMismatch
		}
		var th [32]byte
		th = c.transcript.sum()
		expect := hmacSha256(c.serverFinishedKey[:], th[:])
		var diff byte
		for i := 0; i < 32; i++ {
			diff |= expect[i] ^ body[i]
		}
		if diff != 0 {
			return ErrFinishedMismatch
		}
		c.transcript.write(msg)
		c.state = stateReceivedFinished
	case hsCertificateRequest:
		// We have no client certificate; the answer is an empty Certificate
		// before our Finished (RFC 8446 §4.4.2).
		c.transcript.write(msg)
		c.pendingClientCert = true
	case hsNewSessionTicket:
		// Legal to ignore for a client that did not offer a PSK.
		c.transcript.write(msg)
	default:
		return ErrUnexpectedMessage
	}
	return nil
}

// parseServerHello extracts the suite and the x25519 key share.
func parseServerHello(body []byte) (uint16, [32]byte, bool, error) {
	var peer [32]byte
	ok := false
	r := newWireReader(body)
	if _, err := r.u16(); err != nil { // legacy_version
		return 0, peer, false, err
	}
	if _, err := r.take(32); err != nil { // random
		return 0, peer, false, err
	}
	if _, err := r.vec8(); err != nil { // legacy_session_id_echo
		return 0, peer, false, err
	}
	suite, err := r.u16()
	if err != nil {
		return 0, peer, false, err
	}
	if _, err := r.u8(); err != nil { // legacy_compression_method
		return 0, peer, false, err
	}
	exts, err := r.vec16()
	if err != nil {
		return 0, peer, false, err
	}
	er := newWireReader(exts)
	for !er.atEnd() {
		at, err := er.u16()
		if err != nil {
			return 0, peer, false, err
		}
		b2, err := er.vec16()
		if err != nil {
			return 0, peer, false, err
		}
		if at == extKeyShare {
			kr := newWireReader(b2)
			g, err := kr.u16()
			if err != nil {
				return 0, peer, false, err
			}
			key, err := kr.vec16()
			if err != nil {
				return 0, peer, false, err
			}
			if g == groupX25519 && len(key) == 32 {
				copy(peer[:], key)
				ok = true
			}
		} else if at == extSupportedVersions {
			vr := newWireReader(b2)
			v, err := vr.u16()
			if err != nil {
				return 0, peer, false, err
			}
			if v != versionTLS13 {
				return 0, peer, false, errBadExtension
			}
		}
	}
	return suite, peer, ok, nil
}

func parseEncryptedExtensions(body []byte) error {
	r := newWireReader(body)
	exts, err := r.vec16()
	if err != nil {
		return err
	}
	er := newWireReader(exts)
	for !er.atEnd() {
		if _, err := er.u16(); err != nil {
			return err
		}
		if _, err := er.vec16(); err != nil {
			return err
		}
	}
	return nil
}

func parseCertificateVerify(body []byte) (uint16, []byte, error) {
	r := newWireReader(body)
	scheme, err := r.u16()
	if err != nil {
		return 0, nil, err
	}
	sig, err := r.vec16()
	if err != nil {
		return 0, nil, err
	}
	return scheme, sig, nil
}

// captureCertificate keeps the leaf DER (copied: hsBuf is compacted) and the
// intermediates.
func (c *client) captureCertificate(body []byte) error {
	r := newWireReader(body)
	if _, err := r.vec8(); err != nil { // certificate_request_context
		return err
	}
	list, err := r.vec24()
	if err != nil {
		return err
	}
	lr := newWireReader(list)
	idx := 0
	for !lr.atEnd() {
		certDER, err := lr.vec24()
		if err != nil {
			return err
		}
		if !lr.atEnd() {
			if _, err := lr.vec16(); err != nil { // per-entry extensions
				return err
			}
		}
		if idx == 0 {
			if len(certDER) > maxCertDER {
				return ErrCertificateParse
			}
			cp := make([]byte, len(certDER))
			copy(cp, certDER)
			c.leafDER = cp
		} else if len(c.intermediates) < maxIntermediates {
			if len(certDER) > maxCertDER {
				return ErrCertificateParse
			}
			cp := make([]byte, len(certDER))
			copy(cp, certDER)
			c.intermediates = append(c.intermediates, cp)
		}
		idx++
	}
	if c.leafDER == nil {
		return ErrCertificateParse
	}
	return nil
}

// verifyCertificateVerify checks the server's transcript signature.
func (c *client) verifyCertificateVerify(scheme uint16, sig []byte) bool {
	th := c.transcript.sum()
	// 64 spaces || context || 0x00 || transcript hash (RFC 8446 §4.4.3).
	blob := make([]byte, 0, 64+len(serverCVContext)+1+32)
	for i := 0; i < 64; i++ {
		blob = append(blob, 0x20)
	}
	blob = append(blob, serverCVContext...)
	blob = append(blob, 0x00)
	blob = append(blob, th[:]...)

	leaf, err := parseCert(c.leafDER)
	if err != nil {
		return false
	}
	switch scheme {
	case sigEcdsaSecp256r1Sha256:
		if leaf.key.kind != keyEC || leaf.key.curve != curveKindP256 {
			return false
		}
		return verifyLeafSig(leaf, sig, blob, hashSHA256, ecP256(), 32)
	case sigEcdsaSecp384r1Sha384:
		if leaf.key.kind != keyEC || leaf.key.curve != curveKindP384 {
			return false
		}
		return verifyLeafSig(leaf, sig, blob, hashSHA384, ecP384(), 48)
	case sigRsaPssRsaeSha256:
		if leaf.key.kind != keyRSA {
			return false
		}
		return verifyRsaPss(leaf.key.rsaModulus, leaf.key.rsaExponent, hashSHA256, blob, sig)
	case sigRsaPssRsaeSha384:
		if leaf.key.kind != keyRSA {
			return false
		}
		return verifyRsaPss(leaf.key.rsaModulus, leaf.key.rsaExponent, hashSHA384, blob, sig)
	case sigRsaPssRsaeSha512:
		if leaf.key.kind != keyRSA {
			return false
		}
		return verifyRsaPss(leaf.key.rsaModulus, leaf.key.rsaExponent, hashSHA512, blob, sig)
	case sigRsaPkcs1Sha256:
		if leaf.key.kind != keyRSA {
			return false
		}
		return verifyRsaPkcs1(leaf.key.rsaModulus, leaf.key.rsaExponent, hashSHA256, blob, sig)
	case sigRsaPkcs1Sha384:
		if leaf.key.kind != keyRSA {
			return false
		}
		return verifyRsaPkcs1(leaf.key.rsaModulus, leaf.key.rsaExponent, hashSHA384, blob, sig)
	case sigRsaPkcs1Sha512:
		if leaf.key.kind != keyRSA {
			return false
		}
		return verifyRsaPkcs1(leaf.key.rsaModulus, leaf.key.rsaExponent, hashSHA512, blob, sig)
	}
	return false
}

// verifyLeafSig verifies an ECDSA CertificateVerify signature (DER
// ECDSA-Sig-Value) over blob with the given hash and curve.
func verifyLeafSig(leaf *cert, sig, blob []byte, h hashAlg, curve *ecCurve, zlen int) bool {
	outer := newDERReader(sig)
	seq, err := outer.expect(derTagSequence)
	if err != nil || !outer.atEnd() {
		return false
	}
	sr := newDERReader(seq.content)
	rElem, err := sr.expect(derTagInteger)
	if err != nil {
		return false
	}
	sElem, err := sr.expect(derTagInteger)
	if err != nil {
		return false
	}
	r, err := derInteger(rElem)
	if err != nil {
		return false
	}
	s, err := derInteger(sElem)
	if err != nil {
		return false
	}
	point := leaf.key.point
	if len(point) < 1 || point[0] != 0x04 {
		return false
	}
	flen := (len(point) - 1) / 2
	digest := h.hashSum(blob)
	if len(digest) < zlen {
		return false
	}
	return curve.verifyECDSA(point[1:1+flen], point[1+flen:], r, s, digest[:zlen])
}

// deriveHandshakeKeys runs the RFC 8446 §7.1 schedule through the handshake
// secrets and installs the handshake traffic keys.
func (c *client) deriveHandshakeKeys(serverPub [32]byte, priv [32]byte) error {
	shared, ok := x25519(priv, serverPub)
	if !ok {
		return errKeyShareRejected
	}
	var zero [32]byte
	if string(shared[:]) == string(zero[:]) {
		return errKeyShareRejected
	}

	early := hkdfExtract(zero[:], zero[:])
	var emptyHash [32]byte
	emptyHash = sha256Sum(nil)
	derived := deriveSecret(early, "derived", emptyHash)

	hsSecret := hkdfExtract(derived[:], shared[:])
	c.handshakeSecret = hsSecret

	th := c.transcript.sum()
	cHS := deriveSecret(hsSecret, "c hs traffic", th)
	sHS := deriveSecret(hsSecret, "s hs traffic", th)

	c.writeKeys.key = trafficKey(cHS)
	c.writeKeys.iv = trafficIv(cHS)
	c.readKeys.key = trafficKey(sHS)
	c.readKeys.iv = trafficIv(sHS)
	c.clientFinishedKey = finishedKey(cHS)
	c.serverFinishedKey = finishedKey(sHS)

	c.writeKeysReady = true
	c.readEncrypted = true
	c.readSeq = 0
	c.writeSeq = 0
	// 16-byte keys; newAES128 cannot fail on them.
	c.writeCipher, _ = newAES128(c.writeKeys.key[:])
	c.readCipher, _ = newAES128(c.readKeys.key[:])
	return nil
}

// finishHandshake validates the chain, sends the client Finished, and
// switches to the application keys.
func (c *client) finishHandshake() error {
	if c.verify {
		store := c.store
		if store == nil {
			store = defaultStore
		}
		res := validateChain(c.leafDER, c.intermediates, store, []byte(c.host), c.now)
		c.lastValidation = res
		if res != resultValid {
			return chainValidationError{res: res}
		}
	}

	var zero [32]byte
	var emptyHash [32]byte
	emptyHash = sha256Sum(nil)
	derived := deriveSecret(c.handshakeSecret, "derived", emptyHash)
	master := hkdfExtract(derived[:], zero[:])

	thAP := c.transcript.sum()
	cAP := deriveSecret(master, "c ap traffic", thAP)
	sAP := deriveSecret(master, "s ap traffic", thAP)

	if c.pendingClientCert {
		emptyCert := []byte{0x0b, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00}
		if err := c.sendRecord(emptyCert, ctHandshake); err != nil {
			return err
		}
		c.transcript.write(emptyCert)
	}

	th := c.transcript.sum()
	expect := hmacSha256(c.clientFinishedKey[:], th[:])
	fin := make([]byte, 4+32)
	fin[0] = hsFinished
	fin[1] = 0
	fin[2] = 0
	fin[3] = 32
	copy(fin[4:], expect[:])
	if err := c.sendRecord(fin, ctHandshake); err != nil {
		return err
	}
	c.transcript.write(fin)

	c.writeKeys.key = trafficKey(cAP)
	c.writeKeys.iv = trafficIv(cAP)
	c.readKeys.key = trafficKey(sAP)
	c.readKeys.iv = trafficIv(sAP)
	writeCipher, err := newAES128(c.writeKeys.key[:])
	if err != nil {
		return err
	}
	readCipher, err := newAES128(c.readKeys.key[:])
	if err != nil {
		return err
	}
	c.writeCipher = writeCipher
	c.readCipher = readCipher
	c.readSeq = 0
	c.writeSeq = 0
	c.state = stateConnected
	return nil
}

// write sends application data (one AEAD record; callers chunk).
func (c *client) write(data []byte) error {
	if !c.state.allowsApplicationData() {
		return ErrNotConnected
	}
	if len(data) > maxPlaintext {
		return errRecordTooLarge
	}
	return c.sendRecord(data, ctApplicationData)
}

// read returns application data, absorbing post-handshake handshake messages
// (NewSessionTicket and friends) transparently. A record larger than out
// keeps its unread tail in c.pending for the next read.
func (c *client) read(out []byte) (int, error) {
	if !c.state.allowsApplicationData() {
		return 0, ErrNotConnected
	}
	for {
		if len(c.pending) > 0 {
			if len(out) == 0 {
				return 0, nil
			}
			take := len(c.pending)
			if take > len(out) {
				take = len(out)
			}
			copy(out, c.pending[:take])
			c.pending = c.pending[take:]
			if len(c.pending) == 0 {
				c.pending = nil
			}
			return take, nil
		}
		n, inner, err := c.readRecord()
		if err != nil {
			return 0, err
		}
		switch inner {
		case ctChangeCipherSpec:
			continue
		case ctApplicationData:
			if len(out) == 0 {
				return 0, nil
			}
			take := n
			if take > len(out) {
				take = len(out)
			}
			copy(out, c.plainBuf[:take])
			if take < n {
				rest := make([]byte, n-take)
				copy(rest, c.plainBuf[take:n])
				c.pending = rest
			}
			return take, nil
		case ctHandshake:
			continue
		case ctAlert:
			if n == 2 && c.plainBuf[1] == 0 {
				return 0, errStreamClosed // authenticated close_notify
			}
			return 0, ErrAlertReceived
		}
	}
}

// close sends a close_notify alert and closes the transport.
func (c *client) close() error {
	if c.state == stateConnected {
		_ = c.sendRecord([]byte{1, 0}, ctAlert) // close_notify
	}
	c.state = stateClosed
	return c.t.close()
}
