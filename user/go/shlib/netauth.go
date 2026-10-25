// Delegated net-auth verifier (M50 TS4, ADR 0024 D6): the attached process
// reads the kernel's fresh challenge and the client's one-line reply through
// slot 71, computes HMAC-SHA256 over the domain-separated message, and
// votes. Credentials come from the TS5 store (slot 70) and NEVER from argv.
//
// The signed message is `"VIRELAIOS-AUTH/1 hmac-sha256" || 0x00 ||
// challenge[32]`. A captured handshake cannot replay against a fresh
// challenge. Ed25519 is selected when that is the only stored credential so
// the kernel frames the right scheme, but verify fails closed: there is no
// in-tree Go Ed25519, and this card does not add a new primitive.
package shlib

import "virelai/vi"

const (
	domainHMAC = "VIRELAIOS-AUTH/1 hmac-sha256"
	keyHMAC    = "net-hmac"
	keyEd25519 = "net-ed25519"
	hexDigits  = "0123456789abcdef"
)

type NetAuth struct {
	scheme        uint64
	challengeFn   func([]byte) int64
	responseFn    func([]byte) int64
	verdictFn     func(bool) int64
	getFn         func(name string, out []byte) int
	challenge     [vi.NetChallengeLen]byte
	challengeRead bool
	done          bool
	key           [vi.SecretValMax]byte
	keyLen        int
}

func SysNetAuth(scheme uint64) *NetAuth {
	return &NetAuth{
		scheme:      scheme,
		challengeFn: func(out []byte) int64 { return vi.TtyNetAuth(vi.NetAuthOpChallenge, out) },
		responseFn:  func(out []byte) int64 { return vi.TtyNetAuth(vi.NetAuthOpResponse, out) },
		verdictFn: func(accept bool) int64 {
			var b [1]byte
			if accept {
				b[0] = 1
			}
			return vi.TtyNetAuth(vi.NetAuthOpVerdict, b[:])
		},
		getFn: StoreGet,
	}
}

func StoreGet(name string, out []byte) int {
	var recs [vi.SecretEntriesMax]vi.SecretRecord
	n, r := vi.SecretList(recs[:])
	if r < 0 {
		return -1
	}
	for i := 0; i < n; i++ {
		if recs[i].KeyString() != name {
			continue
		}
		take := int(recs[i].ValLen)
		if take > len(out) {
			take = len(out)
		}
		copy(out, recs[i].Val[:take])
		return take
	}
	return -1
}

// M196a (issue #3351): the provisioning half beside StoreGet. StoreAsk
// stages `name<TAB><own uid><TAB>value[<TAB>apps]` for the seat's consent
// gesture — the kernel binds the caller's principal itself, so the helper
// reads it through slot 68 rather than trusting a caller-supplied uid.
// Returns the ask id (≥1) or the negative errno — EINVAL when the caller
// has no principal (a host build) or the line is malformed, ENOSPC at
// the kernel's ask cap. StoreAskStatus reads the staged ask's verdict:
// vi.SecretAskPending / Committed / Dropped, or ENOENT when the id is
// not the caller's — a terminal verdict is consumed on read (#3406), so
// a second read reports ENOENT too.
var (
	storePrincipal = vi.Principal
	storeSecretAsk = vi.SecretAsk
)

func StoreAsk(name, value, apps string) int64 {
	uid, _, ok := storePrincipal()
	if !ok {
		return -vi.ErrEINVAL
	}
	line := name + "\t" + vi.Itoa64(int64(uid)) + "\t" + value
	if apps != "" {
		line += "\t" + apps
	}
	return storeSecretAsk(line)
}

func StoreAskStatus(id int64) int64 {
	if id < 0 || id > 0x7fffffff {
		return -vi.ErrEINVAL
	}
	return vi.SecretAskStatus(uint32(id))
}

// SelectScheme prefers hmac-sha256, then ed25519, else none. Reads no
// secret beyond a length probe.
func SelectScheme(get func(name string, out []byte) int) (uint64, bool) {
	var probe [1]byte
	defer wipe(probe[:])
	if get(keyHMAC, probe[:]) >= 0 {
		return vi.NetSchemeHMAC, true
	}
	if get(keyEd25519, probe[:]) >= 0 {
		return vi.NetSchemeEd25519, true
	}
	return 0, false
}

func (a *NetAuth) Step() {
	if a == nil || a.done || a.scheme == vi.NetSchemeOpen {
		return
	}
	if !a.challengeRead {
		n := a.challengeFn(a.challenge[:])
		if n != int64(vi.NetChallengeLen) {
			return
		}
		a.challengeRead = true
	}
	var reply [vi.NetAuthLineMax]byte
	rn := a.responseFn(reply[:])
	if rn <= 0 {
		return
	}
	accept := a.verify(reply[:int(rn)])
	_ = a.verdictFn(accept)
	wipe(reply[:])
	wipe(a.challenge[:])
	wipe(a.key[:])
	a.keyLen = 0
	a.done = true
}

func (a *NetAuth) verify(reply []byte) bool {
	switch a.scheme {
	case vi.NetSchemeHMAC:
		if len(reply) != 64 {
			return false
		}
		klen := a.getFn(keyHMAC, a.key[:])
		if klen < 0 {
			return false
		}
		a.keyLen = klen
		var msg [len(domainHMAC) + 1 + vi.NetChallengeLen]byte
		copy(msg[:], domainHMAC)
		msg[len(domainHMAC)] = 0
		copy(msg[len(domainHMAC)+1:], a.challenge[:])
		mac := hmacSha256(a.key[:klen], msg[:])
		var hexb [64]byte
		hexLower(hexb[:], mac[:])
		ok := ctEq(hexb[:], reply)
		wipe(mac[:])
		wipe(hexb[:])
		return ok
	case vi.NetSchemeEd25519:
		// Fail closed: HMAC is the live path this card ports. Selecting
		// the scheme still makes the kernel frame `ed25519`, so a client
		// cannot sneak an open session past a store that only has this key.
		return false
	default:
		return true
	}
}

func hexLower(out, bytes []byte) {
	n := 0
	for _, b := range bytes {
		if n+2 > len(out) {
			return
		}
		out[n] = hexDigits[b>>4]
		out[n+1] = hexDigits[b&0xf]
		n += 2
	}
}

func ctEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
