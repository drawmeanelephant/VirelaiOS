package main

// account_test.go — the settings-file codec + the consent rule, pinned
// on the pure decode half (accountApply has no vi dependency), plus the
// M196b store-resolution half through the storeGetFn test seam.

import "testing"

func TestAccountApply(t *testing.T) {
	var c Config
	if v := c.accountApply([]byte(
		"# gate fixture account\n" +
			"host=10.0.0.2\n" +
			"user=gate\n" +
			"from=gate@fixture.local\n" +
			"sni=leaf.example.com\n" +
			"tls=on\n" +
			"imap_port=1993\n" +
			"max_headers=8\n")); v != acctOK {
		t.Fatal("decode refused a good file")
	}
	if c.Host != "10.0.0.2" || c.User != "gate" || c.Pass != "" ||
		c.From != "gate@fixture.local" || c.ServerName != "leaf.example.com" ||
		!c.TLS || c.AllowPlain || c.IMAPPort != 1993 || c.MaxHeaders != 8 {
		t.Fatalf("decoded %+v", c)
	}
	// Transport-default ports.
	if c.smtpPort() != 465 {
		t.Fatalf("smtpPort %d", c.smtpPort())
	}
	c2 := Config{}
	if c2.imapPort() != 143 || c2.smtpPort() != 587 {
		t.Fatalf("plain defaults %d %d", c2.imapPort(), c2.smtpPort())
	}
	c2.TLS = true
	if c2.imapPort() != 993 {
		t.Fatalf("tls default %d", c2.imapPort())
	}
}

func TestAccountRefusals(t *testing.T) {
	for _, bad := range []string{
		"host",              // no '='
		"bogus=1",           // unknown key
		"tls=maybe",         // bad bool
		"imap_port=abc",     // bad number
		"imap_port=70000",   // over port bound
		"max_headers=0",     // under bound
		"max_headers=99999", // over bound
	} {
		var c Config
		if v := c.accountApply([]byte(bad)); v != acctBad {
			t.Fatalf("accepted %q", bad)
		}
	}
	// The consent rule: plain=on is the stored answer; absent = no consent.
	var c Config
	if v := c.accountApply([]byte("plain=on")); v != acctOK || !c.AllowPlain {
		t.Fatal("plain=on consent not stored")
	}
	var c2 Config
	if v := c2.accountApply([]byte("host=x")); v != acctOK || c2.AllowPlain {
		t.Fatal("absent plain granted consent")
	}
}

// M196b: a plaintext pass= row is its own named refusal — never parsed
// into cfg.Pass, alone or buried inside an otherwise-good file.
func TestAccountPlainPassRefused(t *testing.T) {
	for _, body := range []string{
		"pass=gate-pass",
		"host=10.0.0.2\nuser=gate\npass=gate-pass\nfrom=g@f.local\n",
		"host=10.0.0.2\npass = gate-pass\n", // padded key still names it
	} {
		var c Config
		if v := c.accountApply([]byte(body)); v != acctPlainPass {
			t.Fatalf("pass= row verdict %d for %q", v, body)
		}
		if c.Pass != "" {
			t.Fatalf("pass= value landed in cfg: %q", c.Pass)
		}
	}
}

// M196b: resolvePass reads mail-<user> out of the store through the
// test seam — found fills Pass, absent names secret_not_found, an
// empty entry names secret_malformed.
func TestResolvePass(t *testing.T) {
	old := storeGetFn
	defer func() { storeGetFn = old }()

	var gotName string
	storeGetFn = func(name string, out []byte) int {
		gotName = name
		return copy(out, "gate-pass")
	}
	c := Config{User: "gate"}
	if err := c.resolvePass(); err != nil {
		t.Fatalf("resolvePass: %v", err)
	}
	if gotName != "mail-gate" {
		t.Fatalf("looked up %q, want mail-gate", gotName)
	}
	if c.Pass != "gate-pass" {
		t.Fatalf("pass %q", c.Pass)
	}

	storeGetFn = func(string, []byte) int { return -1 }
	c = Config{User: "ghost"}
	err := c.resolvePass()
	me, ok := err.(mailError)
	if !ok || me.s != "gomail: refuse secret_not_found mail-ghost" {
		t.Fatalf("unprovisioned: %v", err)
	}
	if c.Pass != "" {
		t.Fatal("refusal still set a password")
	}

	storeGetFn = func(string, []byte) int { return 0 }
	c = Config{User: "gate"}
	err = c.resolvePass()
	me, ok = err.(mailError)
	if !ok || me.s != "gomail: refuse secret_malformed mail-gate" {
		t.Fatalf("empty entry: %v", err)
	}
}
