// Command flintd is FLINTD.ELF — the in-guest FLINT-SYNC/1 daemon
// (M214a/b, issues #3918/#3919). It listens on its slot-80 port, frames one
// NDJSON request line at a time, and serves docs/flint-server-spec.md §2 from
// the shared virelai/flintstore core: hello/list/heads/open/read, and the
// validated write half create/append with the optional --auth-key HMAC
// challenge and --allow-author admission gate.
//
// Change lines cross the wire as raw JSONL bytes verbatim; the daemon never
// re-serializes a Change it serves or accepts, so canonical bytes and
// signatures survive transport untouched. Sessions are serialized with one
// request in flight. The daemon holds no signing key and writes no workspace
// or key state: it verifies signatures, it never signs.
//
// --advertise NAME publishes the listener as "NAME._flint._tcp.local" over
// the shared bounded mDNS advertiser (M214c, #3920): startup burst, query
// responder and TTL=0 goodbye on one :5353 owner. HTTPD and FLINTD stage
// their lifecycles, so a guest never carries two owners.
//
// The daemon is a supervised service (M214d, #3921): the kernel's bounded
// accept wait expiring is a routine idle interval and the listener keeps
// serving. --idle-exit=<seconds> is the optional bounded CLI-phase
// lifetime (default zero = persistent); a terminal listener error exits
// non-zero so INIT's on-failure policy owns recovery.
package main

import (
	"errors"
	"fmt"

	"virelai/flintstore"
	"virelai/mdns"
	"virelai/vi"
)

const (
	appName      = "FLINTD.ELF"
	defaultPort  = 4748
	protoVersion = "FLINT-SYNC/1"
	serverName   = "flintd"
	maxLineBytes = flintstore.MaxLineBytes
	nsPerSecond  = int64(1000 * 1000 * 1000)
	maxIdleExitS = int64(3600)
)

var consoleFn = vi.ConsoleLine
var exitFn = vi.Exit

// nanosFn is the daemon's monotonic clock; host tests substitute it.
var nanosFn = vi.Nanos

func main() {
	cfg, err := parseArgs(cliArgs(vi.Args()))
	if err != nil {
		consoleFn("flintd: refuse " + refusalName(err))
		exitFn(2)
		return
	}
	root := cfg.store
	if root == "" {
		root = flintstore.DefaultStoreRoot
	}
	if !storeFS.Exists(flintstore.StreamsDir(root)) {
		consoleFn("flintd: refuse store_missing")
		exitFn(1)
		return
	}
	if cfg.authKeyPath != "" {
		key, kerr := loadAuthKeyFile(storeFS, cfg.authKeyPath)
		if kerr != nil {
			consoleFn("flintd: auth-key " + refusalDetail(kerr))
			consoleFn("flintd: refuse secret_malformed")
			exitFn(1)
			return
		}
		cfg.authKey = key
	}
	if cfg.allowAuthorPath != "" {
		authors, aerr := loadAllowAuthorFile(storeFS, cfg.allowAuthorPath)
		if aerr != nil {
			consoleFn("flintd: allow-author " + refusalDetail(aerr))
			consoleFn("flintd: refuse secret_malformed")
			exitFn(1)
			return
		}
		cfg.allowAuthors = authors
	}
	// The advertisement is validated before either listener binds: a bad
	// label, a denied grant or a NAT-shaped boot refuses by name and exits
	// instead of running unadvertised.
	var adv *mdns.Advertiser
	if cfg.advertise != "" {
		adv = newAdv(cfg.advertise, cfg.port, guestAdvSeam())
		if adv == nil {
			exitFn(1)
			return
		}
	}

	consoleFn("flintd: starting store=" + root + " port=" + fmt.Sprint(cfg.port))

	ln, lerr := vi.SocketListen(cfg.port)
	if lerr != nil {
		consoleFn("flintd: listen failed port=" + fmt.Sprint(cfg.port))
		exitFn(1)
		return
	}
	consoleFn("flintd: listening port=" + fmt.Sprint(cfg.port))

	// One :5353 owner, staged after HTTPD's lifecycle in the hub journey.
	if adv != nil && !adv.Start() {
		_ = ln.Close()
		exitFn(1)
		return
	}

	sessCfg := sessionConfig{root: root, authKey: cfg.authKey, allowAuthors: cfg.allowAuthors}
	// A supervised service is long-lived: the kernel's bounded accept wait
	// expiring (ETIMEDOUT) is a routine idle interval, not a reason to
	// exit. Only a configured --idle-exit phase bound, or a terminal
	// listener error, ends the loop. Sessions stay one at a time and each
	// keeps its own bounded recv/IO budget.
	code := 0
	lastActive := nanosFn()
	for {
		child, aerr := ln.AcceptConn()
		if aerr != nil {
			retry, idleExit := acceptPlan(aerr, cfg.idleExitS, nanosFn(), lastActive)
			if retry {
				continue
			}
			if idleExit {
				consoleFn("flintd: idle exit seconds=" + fmt.Sprint(cfg.idleExitS))
			} else {
				consoleFn("flintd: accept failed")
				code = 1
			}
			break
		}
		lastActive = nanosFn()
		consoleFn("flintd: accepted")
		serveSessionConfig(child, sessCfg, consoleFn)
		_ = child.Close()
		consoleFn("flintd: session close")
		lastActive = nanosFn()
	}
	_ = ln.Close()
	if adv != nil {
		adv.Stop()
	}
	consoleFn("flintd: bye")
	exitFn(code)
}

// acceptPlan classifies one failed AcceptConn. A bounded wait expiry is
// retried; the optional --idle-exit CLI phase setting ends the process
// cleanly once no connection has been served for that many seconds. Any
// other listener error is terminal (exit non-zero so a supervisor's
// on-failure policy can act instead of silently losing the listener).
func acceptPlan(err error, idleExitS int64, nowNs, lastActiveNs int64) (retry, idleExit bool) {
	if !errors.Is(err, vi.ErrConnTimeout) {
		return false, false
	}
	if idleExitS > 0 && nowNs-lastActiveNs >= idleExitS*nsPerSecond {
		return false, true
	}
	return true, false
}

// cliArgs drops the leading argv[0] spelling the runner/kernel may pass.
func cliArgs(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	first := raw[0]
	if len(first) >= 4 && (first[len(first)-4:] == ".ELF" || first == appName || first == "FLINTD") {
		return raw[1:]
	}
	return raw
}

type config struct {
	store           string
	port            uint16
	advertise       string
	authKeyPath     string
	allowAuthorPath string
	// idleExitS is the optional bounded CLI-phase lifetime: exit cleanly
	// after this many seconds without a served connection. Zero (the
	// default) is the persistent supervised-service behavior.
	idleExitS int64
	authKey   *[32]byte

	allowAuthors map[string]bool
}

// parseArgs accepts --store=<dir>, --port=<n>, --advertise NAME,
// --auth-key=<file>, --allow-author=<file> and --idle-exit=<seconds>.
// The advertisement name is validated before either listener binds.
func parseArgs(args []string) (config, error) {
	cfg := config{store: flintstore.DefaultStoreRoot, port: defaultPort}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--store" || a == "--port" || a == "--advertise" ||
			a == "--auth-key" || a == "--allow-author" || a == "--idle-exit":
			if i+1 >= len(args) {
				return cfg, flintstore.Refuse("usage", "missing flag value")
			}
			i++
			switch a {
			case "--store":
				cfg.store = args[i]
			case "--port":
				p, ok := parsePort(args[i])
				if !ok {
					return cfg, flintstore.Refuse("usage", "invalid port")
				}
				cfg.port = p
			case "--advertise":
				if args[i] == "" {
					return cfg, flintstore.Refuse("usage", "empty advertise name")
				}
				cfg.advertise = args[i]
			case "--auth-key":
				cfg.authKeyPath = args[i]
			case "--allow-author":
				cfg.allowAuthorPath = args[i]
			case "--idle-exit":
				s, ok := parseIdleExit(args[i])
				if !ok {
					return cfg, flintstore.Refuse("usage", "invalid idle-exit")
				}
				cfg.idleExitS = s
			}
		case hasPrefix(a, "--store="):
			cfg.store = a[len("--store="):]
			if cfg.store == "" {
				return cfg, flintstore.Refuse("usage", "empty store")
			}
		case hasPrefix(a, "--port="):
			p, ok := parsePort(a[len("--port="):])
			if !ok {
				return cfg, flintstore.Refuse("usage", "invalid port")
			}
			cfg.port = p
		case hasPrefix(a, "--auth-key="):
			cfg.authKeyPath = a[len("--auth-key="):]
			if cfg.authKeyPath == "" {
				return cfg, flintstore.Refuse("usage", "empty auth key path")
			}
		case hasPrefix(a, "--allow-author="):
			cfg.allowAuthorPath = a[len("--allow-author="):]
			if cfg.allowAuthorPath == "" {
				return cfg, flintstore.Refuse("usage", "empty allow-author path")
			}
		case hasPrefix(a, "--advertise="):
			cfg.advertise = a[len("--advertise="):]
			if cfg.advertise == "" {
				return cfg, flintstore.Refuse("usage", "empty advertise name")
			}
		case hasPrefix(a, "--idle-exit="):
			s, ok := parseIdleExit(a[len("--idle-exit="):])
			if !ok {
				return cfg, flintstore.Refuse("usage", "invalid idle-exit")
			}
			cfg.idleExitS = s
		default:
			return cfg, flintstore.Refuse("usage", "unknown flag")
		}
	}
	return cfg, nil
}

// parseIdleExit bounds the optional CLI-phase lifetime. The default is
// the persistent supervised service; a phase script chooses 1..3600 s.
func parseIdleExit(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	var n int64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
		if n > maxIdleExitS {
			return 0, false
		}
	}
	if n < 1 {
		return 0, false
	}
	return n, true
}

func parsePort(s string) (uint16, bool) {
	if s == "" {
		return 0, false
	}
	var n uint32
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + uint32(c-'0')
		if n > 65535 {
			return 0, false
		}
	}
	if n == 0 {
		return 0, false
	}
	return uint16(n), true
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func refusalName(err error) string {
	if name := flintstore.RefusalName(err); name != "" {
		return name
	}
	return "usage"
}

// refusalDetail is the diagnostic half of a refusal: the file read's path
// and errno when there is one, never file content. The line precedes the
// pinned `flintd: refuse <name>` so the serial distinguishes a denied read
// from a malformed file without weakening the contract.
func refusalDetail(err error) string {
	if re, ok := err.(*flintstore.RefuseError); ok && re.Detail != "" {
		return re.Detail
	}
	return "malformed"
}
