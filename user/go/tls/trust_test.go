package tls

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestCompiledTrustInputs(t *testing.T) {
	source, err := os.ReadFile("ROOTS-SOURCES.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "MPL-2.0") ||
		!strings.Contains(string(source), "Approve the 15-root set, excluding R2") {
		t.Fatal("missing source license or owner ruling")
	}
	total := 0
	seen := map[string]bool{}
	for _, root := range vendoredRoots {
		h := sha256.Sum256(root.der)
		if hex.EncodeToString(h[:]) != root.sha256 || seen[root.sha256] {
			t.Fatalf("bad/duplicate DER hash: %s", root.name)
		}
		seen[root.sha256] = true
		total += len(root.der)
		if !strings.Contains(string(source), root.sha256) {
			t.Fatalf("provenance missing: %s", root.name)
		}
		if !gateRoots && (strings.Contains(root.name, "AutoClaw") || root.name == "GTS Root R2") {
			t.Fatalf("forbidden production anchor: %s", root.name)
		}
	}
	if total > 65536 || len(defaultStore.roots) != len(vendoredRoots) || trustStoreMax != 64 {
		t.Fatal("root capacity/input policy changed")
	}
	if TrustVersion() != defaultStore.version {
		t.Fatal("reported trust set differs from actual store")
	}
}

func TestDefaultStoreFixtureDecision(t *testing.T) {
	leaf := certVectorByName("leaf-ec")
	got := validateChain(fixtureDER(t, "leaf-ec"), [][]byte{fixtureDER(t, "inter")},
		defaultStore, []byte("leaf.example.com"), leaf.notBefore+100)
	want := resultNoPathToRoot
	if gateRoots {
		want = resultValid
	}
	if got != want {
		t.Fatalf("fixture verdict=%s gate=%v want=%s", got, gateRoots, want)
	}
}

func TestTrustStoreCapacityUnchanged(t *testing.T) {
	s := newTrustStore("explicit-host-test")
	for i := 0; i < trustStoreMax; i++ {
		if err := s.addRoot(fixtureDER(t, "root")); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.addRoot(fixtureDER(t, "root")); err == nil {
		t.Fatal("root capacity overflow accepted")
	}
}
