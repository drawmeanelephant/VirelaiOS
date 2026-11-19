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
package main

import (
	"fmt"

	"virelai/flintstore"
	"virelai/vi"
)

const (
	appName      = "FLINTD.ELF"
	defaultPort  = 4748
	protoVersion = "FLINT-SYNC/1"
	serverName   = "flintd"
	maxLineBytes = flintstore.MaxLineBytes
)

var consoleFn = vi.ConsoleLine
var exitFn = vi.Exit

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
			consoleFn("flintd: refuse secret_malformed")
			exitFn(1)
			return
		}
		cfg.authKey = key
	}
	if cfg.allowAuthorPath != "" {
		authors, aerr := loadAllowAuthorFile(storeFS, cfg.allowAuthorPath)
		if aerr != nil {
			consoleFn("flintd: refuse secret_malformed")
			exitFn(1)
			return
		}
		cfg.allowAuthors = authors
	}
	consoleFn("flintd: starting store=" + root + " port=" + fmt.Sprint(cfg.port))

	ln, lerr := vi.SocketListen(cfg.port)
	if lerr != nil {
		consoleFn("flintd: listen failed port=" + fmt.Sprint(cfg.port))
		exitFn(1)
		return
	}
	consoleFn("flintd: listening port=" + fmt.Sprint(cfg.port))

	sessCfg := sessionConfig{root: root, authKey: cfg.authKey, allowAuthors: cfg.allowAuthors}
	for {
		child, aerr := ln.AcceptConn()
		if aerr != nil {
			break
		}
		consoleFn("flintd: accepted")
		serveSessionConfig(child, sessCfg, consoleFn)
		_ = child.Close()
		consoleFn("flintd: session close")
	}
	_ = ln.Close()
	consoleFn("flintd: bye")
	exitFn(0)
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
	authKeyPath     string
	allowAuthorPath string
	authKey         *[32]byte
	allowAuthors    map[string]bool
}

// parseArgs accepts --store=<dir>, --port=<n>, --auth-key=<file> and
// --allow-author=<file>. Advertising lands with the sync card; it refuses by
// name instead of silently doing nothing.
func parseArgs(args []string) (config, error) {
	cfg := config{store: flintstore.DefaultStoreRoot, port: defaultPort}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--store" || a == "--port" || a == "--auth-key" || a == "--allow-author":
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
			case "--auth-key":
				cfg.authKeyPath = args[i]
			case "--allow-author":
				cfg.allowAuthorPath = args[i]
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
		case a == "--advertise" || hasPrefix(a, "--advertise="):
			return cfg, flintstore.Refuse("usage", "advertise not served by this daemon")
		default:
			return cfg, flintstore.Refuse("usage", "unknown flag")
		}
	}
	return cfg, nil
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
