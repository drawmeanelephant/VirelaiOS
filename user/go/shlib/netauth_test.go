package shlib

import (
	"bytes"
	"testing"

	"virelai/vi"
)

type testSeam struct {
	challenge  [32]byte
	reply      []byte
	verdict    *bool
	challengeN int
	keyName    string
	keyValue   []byte
}

func (s *testSeam) challengeFn(out []byte) int64 {
	s.challengeN++
	copy(out, s.challenge[:])
	return 32
}
func (s *testSeam) responseFn(out []byte) int64 {
	if len(s.reply) == 0 {
		return 0
	}
	copy(out, s.reply)
	return int64(len(s.reply))
}
func (s *testSeam) verdictFn(accept bool) int64 {
	v := accept
	s.verdict = &v
	return 0
}
func (s *testSeam) getFn(name string, out []byte) int {
	if name != s.keyName {
		return -1
	}
	n := copy(out, s.keyValue)
	return n
}

func sequentialChallenge(first byte) [32]byte {
	var c [32]byte
	for i := range c {
		c[i] = first + byte(i)
	}
	return c
}

func TestNetAuthHMACPinnedVectorAcceptsAndWipes(t *testing.T) {
	s := &testSeam{
		challenge: sequentialChallenge(0),
		reply:     []byte("65bcb791094a86de218b38905cb939c6554ae9eb98238c48cd6835a8883f3ecb"),
		keyName:   keyHMAC,
		keyValue:  []byte("s3cret"),
	}
	a := &NetAuth{
		scheme:      vi.NetSchemeHMAC,
		challengeFn: s.challengeFn,
		responseFn:  s.responseFn,
		verdictFn:   s.verdictFn,
		getFn:       s.getFn,
	}
	a.Step()
	if s.verdict == nil || !*s.verdict {
		t.Fatalf("verdict = %v want accept", s.verdict)
	}
	if !a.done {
		t.Fatal("auth not done")
	}
	if !bytes.Equal(a.key[:], make([]byte, len(a.key))) {
		t.Fatal("key not wiped")
	}
	if !bytes.Equal(a.challenge[:], make([]byte, 32)) {
		t.Fatal("challenge not wiped")
	}
	if a.keyLen != 0 {
		t.Fatalf("keyLen = %d want 0", a.keyLen)
	}
}

func TestNetAuthWrongMACAndMissingKeyReject(t *testing.T) {
	wrong := []byte("00bcb791094a86de218b38905cb939c6554ae9eb98238c48cd6835a8883f3ecb")
	s := &testSeam{
		challenge: sequentialChallenge(0),
		reply:     wrong,
		keyName:   keyHMAC,
		keyValue:  []byte("s3cret"),
	}
	a := &NetAuth{scheme: vi.NetSchemeHMAC, challengeFn: s.challengeFn, responseFn: s.responseFn, verdictFn: s.verdictFn, getFn: s.getFn}
	a.Step()
	if s.verdict == nil || *s.verdict {
		t.Fatalf("wrong MAC verdict = %v want reject", s.verdict)
	}
	s2 := &testSeam{
		challenge: sequentialChallenge(0),
		reply:     wrong,
		keyName:   "other-key",
		keyValue:  []byte("s3cret"),
	}
	a2 := &NetAuth{scheme: vi.NetSchemeHMAC, challengeFn: s2.challengeFn, responseFn: s2.responseFn, verdictFn: s2.verdictFn, getFn: s2.getFn}
	a2.Step()
	if s2.verdict == nil || *s2.verdict {
		t.Fatalf("missing key verdict = %v want reject", s2.verdict)
	}
}

func TestNetAuthCapturedHandshakeDoesNotReplay(t *testing.T) {
	s := &testSeam{
		challenge: sequentialChallenge(7), // different challenge, same MAC
		reply:     []byte("65bcb791094a86de218b38905cb939c6554ae9eb98238c48cd6835a8883f3ecb"),
		keyName:   keyHMAC,
		keyValue:  []byte("s3cret"),
	}
	a := &NetAuth{scheme: vi.NetSchemeHMAC, challengeFn: s.challengeFn, responseFn: s.responseFn, verdictFn: s.verdictFn, getFn: s.getFn}
	a.Step()
	if s.verdict == nil || *s.verdict {
		t.Fatalf("stale MAC verdict = %v want reject", s.verdict)
	}
}

func TestSelectSchemePrefersHMAC(t *testing.T) {
	hmacOnly := func(name string, out []byte) int {
		if name == keyHMAC {
			out[0] = 'k'
			return 1
		}
		return -1
	}
	edOnly := func(name string, out []byte) int {
		if name == keyEd25519 {
			out[0] = 'k'
			return 1
		}
		return -1
	}
	none := func(name string, out []byte) int { return -1 }
	if sch, ok := SelectScheme(hmacOnly); !ok || sch != vi.NetSchemeHMAC {
		t.Fatalf("hmac = %d ok=%v", sch, ok)
	}
	if sch, ok := SelectScheme(edOnly); !ok || sch != vi.NetSchemeEd25519 {
		t.Fatalf("ed25519 = %d ok=%v", sch, ok)
	}
	if _, ok := SelectScheme(none); ok {
		t.Fatal("empty store selected a scheme")
	}
}

func TestNetAuthOpenNeverTouchesSeams(t *testing.T) {
	s := &testSeam{challenge: sequentialChallenge(0)}
	a := &NetAuth{scheme: vi.NetSchemeOpen, challengeFn: s.challengeFn, responseFn: s.responseFn, verdictFn: s.verdictFn, getFn: s.getFn}
	a.Step()
	if s.verdict != nil || a.done || s.challengeN != 0 {
		t.Fatalf("open touched the seam: verdict=%v done=%v reads=%d", s.verdict, a.done, s.challengeN)
	}
}

func TestNetAuthNilStepIsSafe(t *testing.T) {
	var a *NetAuth
	a.Step()
}

// TestStoreAskMarshalsOwnUidAndLine pins the M196a (#3351) wire: the
// staged line is name<TAB><the slot-68 uid><TAB>value[<TAB>apps], uid
// taken from the principal seam, never caller-supplied.
func TestStoreAskMarshalsOwnUidAndLine(t *testing.T) {
	p, a := storePrincipal, storeSecretAsk
	defer func() { storePrincipal, storeSecretAsk = p, a }()
	var gotLine string
	storePrincipal = func() (uint32, uint32, bool) { return 1000, 0, true }
	storeSecretAsk = func(line string) int64 { gotLine = line; return 3 }
	if id := StoreAsk("mail-x", "pw", "GOMAIL.ELF"); id != 3 {
		t.Fatalf("id=%d want 3", id)
	}
	if gotLine != "mail-x\t1000\tpw\tGOMAIL.ELF" {
		t.Fatalf("line %q", gotLine)
	}
	if id := StoreAsk("k", "v", ""); id != 3 {
		t.Fatalf("id=%d want 3", id)
	}
	if gotLine != "k\t1000\tv" {
		t.Fatalf("unbound line %q", gotLine)
	}
	// No principal (a host build) is EINVAL, not a uid-0 line.
	storePrincipal = func() (uint32, uint32, bool) { return 0, 0, false }
	if rc := StoreAsk("k", "v", ""); rc != -vi.ErrEINVAL {
		t.Fatalf("no-principal rc=%d want EINVAL", rc)
	}
}
