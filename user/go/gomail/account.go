package main

// account.go — the GOMAIL account settings file: a small key=value codec
// (the SETTINGS.TXT grammar, not the schema: an app file, not the kernel
// table). The card's "configure account via settings-file" half — GOVAULT
// exists but its passphrase-locked entries have no headless unlock path;
// a plain file is the mechanism that exists for a console tool.
//
// Keys: host, user, from, sni, imap_port, smtp_port, tls (on|off),
// plain (on|off — the stored plaintext-consent answer), max_headers.
// Argv overrides file values. A corrupt file is a named refusal, never a
// half-parsed account; a missing file is fine — argv must then carry the
// account whole.
//
// M196b (#3379): the credential is never a file row or an argv flag — a
// plaintext `pass=` line refuses the file by name and `--pass` refuses
// on argv; the password resolves out of the kernel secret store (slot 70)
// as `mail-<user>`, the FLINT loadSeed precedent.

import (
	"strings"

	"virelai/shlib"
	"virelai/sshlib"
	"virelai/vi"
)

// DefaultAccountPath is where the app looks when --account is absent.
const DefaultAccountPath = "/host/GOMAIL.TXT"

const (
	accountMaxBytes = 4096
	accountMaxRows  = 32
	accountMaxKey   = 16
	accountMaxVal   = 128
)

// Config is the resolved account — argv merged over the file.
type Config struct {
	Host string
	User string
	Pass string
	From string // envelope+From: address; empty falls back to User

	// TLS selects tls.Dial (993/465 by convention). Plaintext runs ONLY
	// under AllowPlain — the stored consent answer; dialOne refuses
	// otherwise.
	TLS        bool
	ServerName string // SNI/verify name; empty takes Host
	AllowPlain bool

	IMAPPort uint16 // 0 = transport default
	SMTPPort uint16 // 0 = transport default

	MaxHeaders int // header list bound; 0 = headerMaxDefault
}

const (
	headerMaxDefault = 20
	headerMaxBound   = 200
)

// imapPort resolves the port in force for the IMAP leg.
func (c Config) imapPort() uint16 {
	if c.IMAPPort != 0 {
		return c.IMAPPort
	}
	if c.TLS {
		return 993
	}
	return 143
}

// smtpPort resolves the port in force for the submit leg.
func (c Config) smtpPort() uint16 {
	if c.SMTPPort != 0 {
		return c.SMTPPort
	}
	if c.TLS {
		return 465
	}
	return 587
}

// fromAddr resolves the envelope sender.
func (c Config) fromAddr() string {
	if c.From != "" {
		return c.From
	}
	return c.User
}

// acctVerdict is accountApply's outcome: the file is clean, corrupt, or
// carries a plaintext pass= row — its own named refusal since M196b.
type acctVerdict int

const (
	acctOK acctVerdict = iota
	acctBad
	acctPlainPass
)

// accountApply decodes the file body into cfg. acctBad is the
// named-refusal verdict ('gomail: account bad'); acctPlainPass is the
// plaintext-credential refusal ('gomail: refuse pass').
func (cfg *Config) accountApply(body []byte) acctVerdict {
	rows := 0
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		rows++
		if rows > accountMaxRows {
			return acctBad
		}
		k := strings.IndexByte(line, '=')
		if k <= 0 || k > accountMaxKey {
			return acctBad
		}
		key, val := strings.TrimSpace(line[:k]), strings.TrimSpace(line[k+1:])
		if key == "" || len(val) > accountMaxVal {
			return acctBad
		}
		switch key {
		case "host":
			cfg.Host = val
		case "user":
			cfg.User = val
		case "pass":
			// M196b: plaintext credentials are refused, never parsed —
			// the store row mail-<user> is the only credential source.
			return acctPlainPass
		case "from":
			cfg.From = val
		case "sni":
			cfg.ServerName = val
		case "imap_port":
			n, ok := parseUint(val)
			if !ok || n <= 0 || n > 65535 {
				return acctBad
			}
			cfg.IMAPPort = uint16(n)
		case "smtp_port":
			n, ok := parseUint(val)
			if !ok || n <= 0 || n > 65535 {
				return acctBad
			}
			cfg.SMTPPort = uint16(n)
		case "tls":
			on, ok := onOff(val)
			if !ok {
				return acctBad
			}
			cfg.TLS = on
		case "plain":
			on, ok := onOff(val)
			if !ok {
				return acctBad
			}
			cfg.AllowPlain = on
		case "max_headers":
			n, ok := parseUint(val)
			if !ok || n <= 0 || n > headerMaxBound {
				return acctBad
			}
			cfg.MaxHeaders = int(n)
		default:
			// An unknown key is a named refusal, not a silent drop: a
			// mistyped 'pas=' would otherwise leave the account silently
			// unauthenticated.
			return acctBad
		}
	}
	return acctOK
}

// loadAccount reads the settings file into cfg. Missing file: no-op,
// acctOK — absent is not corrupt. Corrupt file or unreadable share:
// acctBad; a plaintext pass= row: acctPlainPass — the caller prints the
// named refusal and exits.
func (cfg *Config) loadAccount(path string) (loaded bool, v acctVerdict) {
	body, rc := vi.ReadFileAll(path, accountMaxBytes)
	if rc < 0 {
		return false, acctOK
	}
	if v := cfg.accountApply(body); v != acctOK {
		return false, v
	}
	return true, acctOK
}

// storeGetFn is the slot-70 secret lookup seam; tests replace it without
// touching the kernel store (the FLINT sign.go pattern).
var storeGetFn = shlib.StoreGet

// secretName is the store row the credential resolves from: mail-<user>.
// Entry names are loggable (vi.SecretRecord: names are the only part of
// an entry a caller may log); the value never prints.
func (c Config) secretName() string { return "mail-" + c.User }

// resolvePass fills cfg.Pass out of the kernel secret store — the ONLY
// credential source now that pass= and --pass refuse by name. The raw
// buffer is wiped before returning. An unprovisioned row is the named
// refusal secret_not_found; an empty entry is secret_malformed.
func (cfg *Config) resolvePass() error {
	name := cfg.secretName()
	var raw [vi.SecretValMax]byte
	defer sshlib.Wipe(raw[:])
	n := storeGetFn(name, raw[:])
	if n < 0 {
		return mailError{s: "gomail: refuse secret_not_found " + name}
	}
	if n == 0 {
		return mailError{s: "gomail: refuse secret_malformed " + name}
	}
	cfg.Pass = string(raw[:n])
	say("gomail: secret " + name)
	return nil
}

func onOff(v string) (bool, bool) {
	switch v {
	case "on", "yes", "true", "1":
		return true, true
	case "off", "no", "false", "0":
		return false, true
	}
	return false, false
}
