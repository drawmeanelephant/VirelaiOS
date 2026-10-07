// The trust store: capacity-bounded, caller-owned anchors (ADR 0029 D5). The
// guest instantiates the small vendored set; roots are ordinary DER and the
// store reports a version string for troubleshooting.

package tls

import "virelai/vi"

const trustStoreMax = 64

type rootAnchor struct {
	name, sha256 string
	der          []byte
}

// trustStore holds up to trustStoreMax anchors. findIssuer matches on the
// exact raw DER of the issuer Name.
type trustStore struct {
	roots   []*cert // parsed views of the anchor DERs
	raws    [][]byte
	version string
}

func newTrustStore(version string) *trustStore {
	return &trustStore{version: version}
}

// addRoot injects an anchor. The DER must be a self-signed CA with
// basicConstraints cA=TRUE; anything else is refused at injection so a bad
// anchor cannot sit silently in the store.
func (s *trustStore) addRoot(derBytes []byte) error {
	if len(s.roots) >= trustStoreMax {
		return errNoTrustAnchor
	}
	c, err := parseCert(derBytes)
	if err != nil {
		return errNoTrustAnchor
	}
	if !c.isCA || !c.hasBasicConstraints {
		return errNoTrustAnchor // a non-CA root is refused
	}
	cp := make([]byte, len(derBytes))
	copy(cp, derBytes)
	s.raws = append(s.raws, cp)
	s.roots = append(s.roots, c)
	return nil
}

// removeRoot removes an anchor by DER identity.
func (s *trustStore) removeRoot(derBytes []byte) bool {
	for i, raw := range s.raws {
		if string(raw) == string(derBytes) {
			s.raws = append(s.raws[:i], s.raws[i+1:]...)
			s.roots = append(s.roots[:i], s.roots[i+1:]...)
			return true
		}
	}
	return false
}

// findIssuer returns the anchor whose subject matches the given raw issuer
// Name, or nil.
func (s *trustStore) findIssuer(issuerRaw []byte) *cert {
	for _, c := range s.roots {
		if string(c.subjectRaw) == string(issuerRaw) {
			return c
		}
	}
	return nil
}

// defaultStore is the process-wide guest store.
var defaultStore = newTrustStore(vendoredRootVersion)

func init() {
	for _, anchor := range vendoredRoots {
		if err := defaultStore.addRoot(anchor.der); err != nil {
			panic("tls: vendored root rejected: " + anchor.name + ": " + err.Error())
		}
	}
	vi.ConsoleLine("tls: roots " + vendoredRootVersion)
}

// TrustVersion identifies the compiled trust set. It cannot change at runtime.
func TrustVersion() string { return vendoredRootVersion }
