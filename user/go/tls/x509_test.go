// X.509 parse, identity matching, and chain validation pins, all against the
// generated fixture set (OpenSSL-built, expected fields read back out of
// `openssl x509 -text` by the emitter that produced the vectors).

package tls

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func fixtureDER(t *testing.T, name string) []byte {
	t.Helper()
	return hexDecode(certVectorByName(name).der)
}

func TestX509ParsesEveryFixture(t *testing.T) {
	for _, v := range certVectors {
		c, err := parseCert(hexDecode(v.der))
		if err != nil {
			t.Fatalf("%s: parse failed: %v", v.name, err)
		}
		if c.version != v.version {
			t.Fatalf("%s: version %d != %d", v.name, c.version, v.version)
		}
		if string(c.subjectCN) != v.subjectCN {
			t.Fatalf("%s: subject CN %q != %q", v.name, c.subjectCN, v.subjectCN)
		}
		if string(c.issuerCN) != v.issuerCN {
			t.Fatalf("%s: issuer CN %q != %q", v.name, c.issuerCN, v.issuerCN)
		}
		if c.notBefore != v.notBefore || c.notAfter != v.notAfter {
			t.Fatalf("%s: validity mismatch", v.name)
		}
		if c.isCA != v.isCA || c.hasSAN != v.hasSAN {
			t.Fatalf("%s: CA/SAN flags mismatch", v.name)
		}
		dnsCount := 0
		for i := 0; i < c.sanLen; i++ {
			if !c.san[i].isIP {
				dnsCount++
			}
		}
		if dnsCount != v.sanDNSLen {
			t.Fatalf("%s: SAN dns count %d != %d", v.name, dnsCount, v.sanDNSLen)
		}
		if len(c.unknownCritical) != v.unknownCriticalLen {
			t.Fatalf("%s: unknown-critical count mismatch", v.name)
		}
		if !bytes.Equal(c.serial, hexDecode(v.serial)) {
			t.Fatalf("%s: serial mismatch", v.name)
		}
		kind := ""
		switch c.key.kind {
		case keyRSA:
			kind = "rsa"
		case keyEC:
			kind = "ec"
		case keyEd25519:
			kind = "ed25519"
		}
		if kind != v.keyKind {
			t.Fatalf("%s: key kind %q != %q", v.name, kind, v.keyKind)
		}
	}
}

func TestX509LeafFields(t *testing.T) {
	// leaf-ec: digitalSignature only, EKU serverAuth, SAN leaf+wildcard.
	c, err := parseCert(fixtureDER(t, "leaf-ec"))
	if err != nil {
		t.Fatal(err)
	}
	if c.sanLen != 2 || string(c.san[0].dns) != "leaf.example.com" || string(c.san[1].dns) != "*.wild.example.com" {
		t.Fatalf("leaf-ec SANs wrong: %v", c.san[:c.sanLen])
	}
	if !c.hasKeyUsage || c.keyUsage != kuDigitalSignature {
		t.Fatalf("leaf-ec keyUsage wrong")
	}
	if !c.hasEKU || !c.ekuServerAuth {
		t.Fatalf("leaf-ec EKU wrong")
	}
	if c.isCA {
		t.Fatal("leaf-ec must not be a CA")
	}

	// inter: CA with pathlen 0 and keyCertSign.
	inter, err := parseCert(fixtureDER(t, "inter"))
	if err != nil {
		t.Fatal(err)
	}
	if !inter.isCA || inter.pathLen == nil || *inter.pathLen != 0 {
		t.Fatalf("inter CA/pathlen wrong")
	}
	if inter.keyUsage&kuKeyCertSign == 0 {
		t.Fatalf("inter keyCertSign missing")
	}

	// leaf-rsa: modulus >= 2048-bit, exponent 65537.
	rsa, err := parseCert(fixtureDER(t, "leaf-rsa"))
	if err != nil {
		t.Fatal(err)
	}
	if rsa.key.kind != keyRSA || len(rsa.key.rsaModulus) < 256 {
		t.Fatalf("leaf-rsa key wrong")
	}
	if !bytes.Equal(rsa.key.rsaExponent, []byte{0x01, 0x00, 0x01}) {
		t.Fatalf("leaf-rsa exponent wrong")
	}
}

func TestX509MalformedRefused(t *testing.T) {
	der := fixtureDER(t, "leaf-ec")
	cuts := []int{1, 2, 3, 4, 8, 16, 32, 64, len(der) / 2, len(der) - 3, len(der) - 2, len(der) - 1}
	for _, cut := range cuts {
		if _, err := parseCert(der[:cut]); err == nil {
			t.Fatalf("truncation at %d accepted", cut)
		}
	}
	bad := append([]byte{}, der...)
	bad[0] = 0x31 // SET instead of SEQUENCE
	if _, err := parseCert(bad); err == nil {
		t.Fatal("wrong top-level tag accepted")
	}
	ext := make([]byte, len(der)+1)
	copy(ext, der)
	ext[len(der)] = 0x00 // trailing garbage
	if _, err := parseCert(ext); err == nil {
		t.Fatal("trailing garbage accepted")
	}
	indef := append([]byte{}, der...)
	indef[1] = 0x80 // DER forbids indefinite length
	if _, err := parseCert(indef); err == nil {
		t.Fatal("indefinite length accepted")
	}
}

func TestIdentityHostnameTable(t *testing.T) {
	cases := []struct {
		pattern, host string
		matches       bool
	}{
		{"example.com", "example.com", true},
		{"example.com", "EXAMPLE.COM", true},
		{"example.com", "www.example.com", false},
		{"example.com", "example.com.evil.test", false},
		{"example.com", "xample.com", false},
		{"*.example.com", "www.example.com", true},
		{"*.example.com", "Www.Example.Com", true},
		{"*.example.com", "example.com", false},
		{"*.example.com", "a.b.example.com", false},
		{"*.example.com", ".example.com", false},
		{"*.example.com", "wwwexample.com", false},
		{"www.*.com", "www.example.com", false},
		{"www.ex*.com", "www.example.com", false},
		{"*.com", "example.com", false},
		{"*", "example.com", false},
		{"**.example.com", "a.example.com", false},
	}
	for _, c := range cases {
		if got := matchDNS([]byte(c.pattern), []byte(c.host)); got != c.matches {
			t.Fatalf("matchDNS(%q, %q) = %v, want %v", c.pattern, c.host, got, c.matches)
		}
	}
}

func TestIdentityIPLiterals(t *testing.T) {
	ip, ok := parseIPLiteral([]byte("127.0.0.1"))
	if !ok || ip[10] != 0xff || ip[12] != 127 {
		t.Fatalf("127.0.0.1 parse wrong: %v %v", ip, ok)
	}
	if _, ok := parseIPLiteral([]byte("2001:db8::1")); !ok {
		t.Fatal("2001:db8::1 refused")
	}
	if _, ok := parseIPLiteral([]byte("::ffff:192.168.0.1")); !ok {
		t.Fatal("v4-mapped refused")
	}
	for _, bad := range []string{"example.com", "256.0.0.1", "1.2.3", "1.2.3.4.5", "01.2.3.4", "2001:db8::1::2"} {
		if _, ok := parseIPLiteral([]byte(bad)); ok {
			t.Fatalf("%q accepted as a literal", bad)
		}
	}
}

func TestIdentityAgainstFixtures(t *testing.T) {
	leaf, _ := parseCert(fixtureDER(t, "leaf-ec"))
	if v := verifyIdentity(leaf, []byte("leaf.example.com")); v != verdictMatchDNS {
		t.Fatalf("exact match verdict %v", v)
	}
	if v := verifyIdentity(leaf, []byte("LEAF.EXAMPLE.COM")); v != verdictMatchDNS {
		t.Fatalf("case-insensitive verdict %v", v)
	}
	if v := verifyIdentity(leaf, []byte("anything.wild.example.com")); v != verdictMatchWildcard {
		t.Fatalf("wildcard verdict %v", v)
	}
	if v := verifyIdentity(leaf, []byte("wild.example.com")); v != verdictNoMatch {
		t.Fatalf("apex-under-wildcard verdict %v", v)
	}
	// SAN present: the CN must NOT rescue an unmatched host.
	if v := verifyIdentity(leaf, []byte("other.example.com")); v != verdictNoMatch {
		t.Fatalf("CN rescue verdict %v", v)
	}
	if verifyIdentity(leaf, []byte("bad host.example.com")).ok() {
		t.Fatal("malformed host accepted")
	}
	if verifyIdentity(leaf, []byte("trailing.example.com.")).ok() {
		t.Fatal("trailing dot accepted")
	}

	// leaf-ip: iPAddress SANs match only the literal.
	ipLeaf, _ := parseCert(fixtureDER(t, "leaf-ip"))
	if v := verifyIdentity(ipLeaf, []byte("127.0.0.1")); v != verdictMatchIP {
		t.Fatalf("ip match verdict %v", v)
	}
	if v := verifyIdentity(ipLeaf, []byte("ip.example.com")); v != verdictNoMatch {
		t.Fatalf("dns-against-ip-san verdict %v", v)
	}

	// leaf-nosan: the CN fallback is reported but refused by ok().
	nosan, _ := parseCert(fixtureDER(t, "leaf-nosan"))
	if v := verifyIdentity(nosan, []byte("cnonly.example.com")); v != verdictMatchCNFallback {
		t.Fatalf("cn fallback verdict %v", v)
	}
	if verifyIdentity(nosan, []byte("cnonly.example.com")).ok() {
		t.Fatal("cn fallback must not be ok() by default")
	}
}

func TestValidateChain(t *testing.T) {
	root := fixtureDER(t, "root")
	inter := fixtureDER(t, "inter")
	leafEC := certVectorByName("leaf-ec")
	store := newTrustStore("test")
	if err := store.addRoot(root); err != nil {
		t.Fatal(err)
	}
	now := leafEC.notBefore + 100

	if res := validateChain(fixtureDER(t, "leaf-ec"), [][]byte{inter}, store, []byte("leaf.example.com"), now); res != resultValid {
		t.Fatalf("leaf-ec chain: %v", res)
	}
	if res := validateChain(fixtureDER(t, "leaf-rsa"), [][]byte{inter}, store, []byte("rsa.example.com"), now); res != resultValid {
		t.Fatalf("leaf-rsa chain: %v", res)
	}
	// Wildcard SAN resolves.
	if res := validateChain(fixtureDER(t, "leaf-ec"), [][]byte{inter}, store, []byte("a.wild.example.com"), now); res != resultValid {
		t.Fatalf("wildcard chain: %v", res)
	}
	// The served chain may include the root; unordered is fine.
	if res := validateChain(fixtureDER(t, "leaf-ec"), [][]byte{inter, root}, store, []byte("leaf.example.com"), now); res != resultValid {
		t.Fatalf("chain-with-root: %v", res)
	}
}

func TestValidateNegatives(t *testing.T) {
	root := fixtureDER(t, "root")
	inter := fixtureDER(t, "inter")
	leafEC := certVectorByName("leaf-ec")
	store := newTrustStore("test")
	_ = store.addRoot(root)
	valid := leafEC.notBefore + 100

	if res := validateChain(fixtureDER(t, "leaf-ec"), [][]byte{inter}, store, []byte("wrong.example.com"), valid); res != resultHostnameMismatch {
		t.Fatalf("wrong host: %v", res)
	}
	if res := validateChain(fixtureDER(t, "leaf-ec"), [][]byte{inter}, store, []byte("leaf.example.com"), leafEC.notBefore-1000); res != resultNotYetValid {
		t.Fatalf("not yet valid: %v", res)
	}
	if res := validateChain(fixtureDER(t, "leaf-ec"), [][]byte{inter}, store, []byte("leaf.example.com"), leafEC.notAfter+1000); res != resultExpired {
		t.Fatalf("expired: %v", res)
	}
	// Missing intermediate: no path to the root.
	if res := validateChain(fixtureDER(t, "leaf-ec"), nil, store, []byte("leaf.example.com"), valid); res != resultNoPathToRoot {
		t.Fatalf("no path: %v", res)
	}
	// A self-issued certificate cannot be its own trust anchor.
	empty := newTrustStore("empty")
	if res := validateChain(root, [][]byte{root}, empty, []byte("anything.example.com"), valid); res != resultNoPathToRoot {
		t.Fatalf("self-issued anchor: %v", res)
	}
	// Unknown critical extension fails the chain.
	if res := validateChain(fixtureDER(t, "leaf-unknowncrit"), [][]byte{inter}, store, []byte("unknown.example.com"), valid); res != resultUnknownCriticalExtension {
		t.Fatalf("unknown critical: %v", res)
	}
	// A non-CA cannot be injected as a root.
	if err := store.addRoot(fixtureDER(t, "leaf-ec")); err == nil {
		t.Fatal("non-CA root accepted")
	}
	// Removing the root breaks the chain.
	rootDER := fixtureDER(t, "root")
	if !store.removeRoot(rootDER) {
		t.Fatal("removeRoot failed")
	}
	if res := validateChain(fixtureDER(t, "leaf-ec"), [][]byte{inter}, store, []byte("leaf.example.com"), valid); res != resultNoPathToRoot {
		t.Fatalf("after root removal: %v", res)
	}
}

func TestValidateNameConstraints(t *testing.T) {
	// ca-nc permits .example.com and excludes .evil.example.com.
	store := newTrustStore("nc")
	_ = store.addRoot(fixtureDER(t, "root"))
	_ = store.addRoot(fixtureDER(t, "ca-nc"))
	nc := certVectorByName("leaf-nc")
	now := nc.notBefore + 100

	if res := validateChain(fixtureDER(t, "leaf-nc"), [][]byte{fixtureDER(t, "ca-nc")}, store, []byte("nc.example.com"), now); res != resultValid {
		t.Fatalf("nc leaf: %v", res)
	}
	if res := validateChain(fixtureDER(t, "leaf-ncevil"), [][]byte{fixtureDER(t, "ca-nc")}, store, []byte("evil.example.com"), now); res != resultNameConstraintViolation {
		t.Fatalf("excluded subtree: %v", res)
	}
	if res := validateChain(fixtureDER(t, "leaf-ncother"), [][]byte{fixtureDER(t, "ca-nc")}, store, []byte("other.com"), now); res != resultNameConstraintViolation {
		t.Fatalf("outside permitted subtree: %v", res)
	}
}

// Only the separately tagged gate build trusts the fixture root.
func TestVendoredRootMatchesBuildPolicy(t *testing.T) {
	want := fixtureDER(t, "root")
	if string(defaultStore.version) != vendoredRootVersion {
		t.Fatal("store version mismatch")
	}
	issuer := defaultStore.findIssuer(mustIssuer(t, "inter"))
	if gateRoots {
		if len(vendoredRoots) != 1 || !bytes.Equal(vendoredRoots[0].der, want) || issuer == nil {
			t.Fatal("gate store must contain only the exact fixture root")
		}
	} else if len(vendoredRoots) != 15 || issuer != nil {
		t.Fatal("production store must contain fifteen roots and reject the fixture")
	}
}

// mustIssuer returns the fixture's raw issuer Name — the Name a trust anchor
// issuing this fixture must carry as its subject.
func mustIssuer(t *testing.T, name string) []byte {
	t.Helper()
	c, err := parseCert(fixtureDER(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return c.issuerRaw
}

var _ = hex.EncodeToString
