package main

// M196c (#3380): --sasl names the account and the password resolves out
// of the slot-70 store's `irc-<acct>` row; the argv credential shape is
// the named refusal. The store seam is stubbed — these tests never touch
// the kernel.

import (
	"strings"
	"testing"
)

// stubSASLStore swaps saslStoreGet for a fixed map; a missing key returns
// -1 (StoreGet's not-found). seen records every name queried.
func stubSASLStore(t *testing.T, rows map[string]string, seen *[]string) {
	t.Helper()
	orig := saslStoreGet
	saslStoreGet = func(name string, out []byte) int {
		if seen != nil {
			*seen = append(*seen, name)
		}
		v, ok := rows[name]
		if !ok {
			return -1
		}
		return copy(out, v)
	}
	t.Cleanup(func() { saslStoreGet = orig })
}

func TestParseSASLNamesAccountOnly(t *testing.T) {
	for _, arg := range []string{"--sasl", "--sasl=gate"} {
		args := []string{"--host", "h", "--nick", "n"}
		if arg == "--sasl" {
			args = append(args, arg, "gate")
		} else {
			args = append(args, arg)
		}
		o, ok := parseArgs(args)
		if !ok {
			t.Fatalf("parseArgs(%v) failed", args)
		}
		if o.saslAcct != "gate" || o.saslArgv {
			t.Fatalf("parseArgs(%v): acct=%q argv=%v", args, o.saslAcct, o.saslArgv)
		}
		if o.cfg.SASL != nil {
			t.Fatalf("parseArgs(%v) resolved the store eagerly", args)
		}
	}
}

func TestParseSASLArgvIsRefused(t *testing.T) {
	for _, v := range []string{"gate:sasl-secret-7", "gate:", ":sasl-secret-7"} {
		o, ok := parseArgs([]string{"--host", "h", "--nick", "n", "--sasl", v})
		if !ok {
			t.Fatalf("parseArgs(--sasl %q) failed to parse", v)
		}
		if !o.saslArgv || o.saslAcct != "" || o.cfg.SASL != nil {
			t.Fatalf("--sasl %q: argv=%v acct=%q sasl=%v", v, o.saslArgv, o.saslAcct, o.cfg.SASL)
		}
	}
}

func TestParseSASLEmptyIsUsage(t *testing.T) {
	if _, ok := parseArgs([]string{"--host", "h", "--nick", "n", "--sasl="}); ok {
		t.Fatal("--sasl= parsed")
	}
	if _, ok := parseArgs([]string{"--host", "h", "--nick", "n", "--sasl"}); ok {
		t.Fatal("dangling --sasl parsed")
	}
}

func TestResolveSASLArgvPrintsTheNamedRefusal(t *testing.T) {
	o, ok := parseArgs([]string{"--host", "h", "--nick", "n", "--sasl", "gate:pw"})
	if !ok {
		t.Fatal("parse failed")
	}
	err := resolveSASL(&o)
	if err == nil || !strings.Contains(err.Error(), "goirc: refuse sasl-argv") {
		t.Fatalf("resolveSASL = %v, want the refuse sasl-argv marker", err)
	}
	if o.cfg.SASL != nil {
		t.Fatal("a refused argv credential armed SASL")
	}
}

func TestResolveSASLReadsTheStoreRow(t *testing.T) {
	var seen []string
	stubSASLStore(t, map[string]string{"irc-gate": "sasl-secret-7"}, &seen)
	o, ok := parseArgs([]string{"--host", "h", "--nick", "n", "--sasl", "gate"})
	if !ok {
		t.Fatal("parse failed")
	}
	if err := resolveSASL(&o); err != nil {
		t.Fatalf("resolveSASL = %v", err)
	}
	if len(seen) != 1 || seen[0] != "irc-gate" {
		t.Fatalf("store queried %v, want [irc-gate]", seen)
	}
	if o.cfg.SASL == nil || o.cfg.SASL.Account != "gate" || o.cfg.SASL.Password != "sasl-secret-7" {
		t.Fatalf("cfg.SASL = %+v", o.cfg.SASL)
	}
}

func TestResolveSASLMissingRowNamesIt(t *testing.T) {
	stubSASLStore(t, map[string]string{}, nil)
	o, ok := parseArgs([]string{"--host", "h", "--nick", "n", "--sasl", "gate"})
	if !ok {
		t.Fatal("parse failed")
	}
	err := resolveSASL(&o)
	if err == nil ||
		!strings.Contains(err.Error(), "secret_not_found") ||
		!strings.Contains(err.Error(), "irc-gate") {
		t.Fatalf("resolveSASL = %v, want the named secret_not_found row", err)
	}
	if o.cfg.SASL != nil {
		t.Fatal("an unprovisioned account armed SASL")
	}
}

func TestResolveSASLAbsentIsANoOp(t *testing.T) {
	stubSASLStore(t, map[string]string{}, nil)
	o, ok := parseArgs([]string{"--host", "h", "--nick", "n"})
	if !ok {
		t.Fatal("parse failed")
	}
	if err := resolveSASL(&o); err != nil || o.cfg.SASL != nil {
		t.Fatalf("resolveSASL = %v sasl=%v", err, o.cfg.SASL)
	}
}

// The loadSeed precedent: loadSASL must return the store bytes verbatim —
// a truncated or padded read breaks the wire payload the gate fixture
// verifies.
func TestLoadSASLCopiesExactly(t *testing.T) {
	stubSASLStore(t, map[string]string{"irc-a": "p"}, nil)
	s, err := loadSASL("a")
	if err != nil {
		t.Fatalf("loadSASL = %v", err)
	}
	if s.Account != "a" || s.Password != "p" {
		t.Fatalf("loadSASL = %+v", s)
	}
}
