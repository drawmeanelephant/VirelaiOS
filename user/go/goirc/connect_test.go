package main

// #3779: the connect face's pure halves — the GOIRC.TXT codec and the
// prompt-field parsers. No vi dependency; the file seam stays in
// connect.go's load/write wrappers.

import "testing"

func TestConnectConfigRoundTrip(t *testing.T) {
	cfg := Config{
		Host:       "irc.example.com",
		Port:       6697,
		Nick:       "gate",
		TLS:        true,
		ServerName: "irc.example.com",
		AllowPlain: true,
	}
	got, ok := applyConnectConfig(cfg.connectBody())
	if !ok {
		t.Fatal("applyConnectConfig refused a clean body")
	}
	if got.Host != cfg.Host || got.Port != cfg.Port || got.Nick != cfg.Nick ||
		got.TLS != cfg.TLS || got.ServerName != cfg.ServerName ||
		got.AllowPlain != cfg.AllowPlain {
		t.Fatalf("round trip = %+v, want %+v", got, cfg)
	}
}

func TestConnectConfigRequiresHostAndNick(t *testing.T) {
	for _, body := range []string{
		"#v1\nnick=gate\n",               // no host
		"#v1\nhost=10.0.0.2\n",           // no nick
		"#v1\nhost=h\nnick=n\nbogus=1\n", // unknown key refuses whole
		"#v1\nhost=h\nnick=n\nport=x\n",  // malformed port
		"#v1\nhost=h\nnick=n\ntls=maybe\n",
		"",
	} {
		if _, ok := applyConnectConfig([]byte(body)); ok {
			t.Fatalf("applyConnectConfig(%q) parsed", body)
		}
	}
}

func TestConnectConfigDefaults(t *testing.T) {
	got, ok := applyConnectConfig([]byte("#v1\nhost=h\nnick=n\n"))
	if !ok {
		t.Fatal("minimal body refused")
	}
	if got.Port != 6667 || !got.Reconnect || got.TLS || got.AllowPlain {
		t.Fatalf("defaults = %+v", got)
	}
}

func TestParseServerArg(t *testing.T) {
	for _, c := range []struct {
		in   string
		host string
		port uint16
		ok   bool
	}{
		{"10.0.0.2", "10.0.0.2", 0, true},
		{"irc.example.com:6697", "irc.example.com", 6697, true},
		{"  spaced  ", "spaced", 0, true},
		{"", "", 0, false},
		{"a b", "", 0, false},
		{"host:", "", 0, false},
		{":6667", "", 0, false},
		{"host:abc", "", 0, false},
		{"host:0", "", 0, false},
		{"host:70000", "", 0, false},
	} {
		h, p, ok := parseServerArg(c.in)
		if ok != c.ok || h != c.host || p != c.port {
			t.Fatalf("parseServerArg(%q) = %q %d %v, want %q %d %v",
				c.in, h, p, ok, c.host, c.port, c.ok)
		}
	}
}

func TestParseNickArg(t *testing.T) {
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"gate", "gate", true},
		{"gate_-", "gate_-", true},
		{"gate extra", "gate", true},
		{"  ", "", false},
		{"", "", false},
		{"a,b", "", false},
	} {
		n, ok := parseNickArg(c.in)
		if ok != c.ok || n != c.want {
			t.Fatalf("parseNickArg(%q) = %q %v, want %q %v", c.in, n, ok, c.want, c.ok)
		}
	}
}
