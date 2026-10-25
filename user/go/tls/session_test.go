// Record-layer bound regression tests: the admit bound is the epoch's
// (TLSPlaintext ≤ 2^14, TLSCiphertext ≤ 2^14+256) but plainBuf is sized for
// plaintext, so an oversized record must be a named refusal, never a slice
// panic — the remote kill this file pins down.

package tls

import (
	"bytes"
	"testing"
)

// scriptTransport serves one scripted byte stream to read() and captures
// writes; reads past the script report a dead transport.
type scriptTransport struct {
	reads   []byte
	written []byte
}

func (s *scriptTransport) read(p []byte) (int, error) {
	if len(s.reads) == 0 {
		return 0, errTransport
	}
	n := copy(p, s.reads)
	s.reads = s.reads[n:]
	return n, nil
}

func (s *scriptTransport) write(p []byte) (int, error) {
	s.written = append(s.written, p...)
	return len(p), nil
}

func (s *scriptTransport) close() error { return nil }

func frameRecord(contentType byte, payload []byte) []byte {
	rec := make([]byte, recordHeaderLen+len(payload))
	rec[0] = contentType
	rec[1], rec[2] = 0x03, 0x03
	rec[3], rec[4] = byte(len(payload)>>8), byte(len(payload))
	copy(rec[recordHeaderLen:], payload)
	return rec
}

func TestReadRecordPlaintextBound(t *testing.T) {
	// Pre-keys epoch: TLSPlaintext.length is bounded by 2^14, not
	// maxCiphertext — a record declaring (16384, 16640] used to reach
	// copy(plainBuf[:l], …) and panic.
	for _, l := range []int{maxPlaintext + 1, maxCiphertext} {
		s := &session{t: &scriptTransport{reads: frameRecord(ctHandshake, make([]byte, l))}}
		if _, _, err := s.readRecord(); err != errRecordTooLarge {
			t.Fatalf("l=%d: %v, want errRecordTooLarge", l, err)
		}
	}

	// The boundary itself stays legal: a full 2^14 plaintext record reads.
	payload := bytes.Repeat([]byte{0xAB}, maxPlaintext)
	s := &session{t: &scriptTransport{reads: frameRecord(ctHandshake, payload)}}
	n, ct, err := s.readRecord()
	if err != nil {
		t.Fatal(err)
	}
	if n != maxPlaintext || ct != ctHandshake {
		t.Fatalf("boundary read = (%d, %d)", n, ct)
	}
	if !bytes.Equal(s.plainBuf[:n], payload) {
		t.Fatal("boundary payload mismatch")
	}
}

func TestReadRecordInnerPlaintextBound(t *testing.T) {
	// Post-handshake epoch: the ciphertext admit bound reaches 2^14+256,
	// so a conforming AEAD wrapper can still deliver >2^14 of inner
	// plaintext — illegal per RFC 8446 §5.2 and wider than plainBuf.
	key := [16]byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 1, 2, 3, 4, 5, 6}
	iv := [12]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	keys := trafficKeys{key: key, iv: iv}

	w := &session{
		t:              &loopbackTransport{},
		writeCipher:    mustAES(t, key[:]),
		writeKeys:      keys,
		writeKeysReady: true,
	}
	if err := w.sendRecord(make([]byte, maxPlaintext+1), ctApplicationData); err != nil {
		t.Fatal(err)
	}

	r := &session{
		t:             &scriptTransport{reads: w.t.(*loopbackTransport).written},
		readCipher:    mustAES(t, key[:]),
		readKeys:      keys,
		readEncrypted: true,
	}
	if _, _, err := r.readRecord(); err != errRecordTooLarge {
		t.Fatalf("oversized inner plaintext: %v, want errRecordTooLarge", err)
	}
}
