// Command flintd is FLINTD.ELF — the in-guest FLINT-SYNC/1 read daemon
// (M214a, issue #3918). It listens on its slot-80 port, frames one NDJSON
// request line at a time, and serves the read half of docs/flint-server-spec.md
// §2 from the shared virelai/flintstore core: hello, list, heads, open, read.
//
// Change lines cross the wire as raw JSONL bytes verbatim; the daemon never
// re-serializes a Change it serves, so canonical bytes and signatures survive
// transport untouched. Sessions are serialized with one request in flight.
// This milestone is read-only: create/append and auth refuse honestly by name
// until the write card lands.
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
	}
	consoleFn("flintd: starting store=" + root + " port=" + fmt.Sprint(cfg.port))

	ln, lerr := vi.SocketListen(cfg.port)
	if lerr != nil {
		consoleFn("flintd: listen failed port=" + fmt.Sprint(cfg.port))
		exitFn(1)
		return
	}
	consoleFn("flintd: listening port=" + fmt.Sprint(cfg.port))

	for {
		child, aerr := ln.AcceptConn()
		if aerr != nil {
			break
		}
		consoleFn("flintd: accepted")
		serveSession(child, root, consoleFn)
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
	store string
	port  uint16
}

// parseArgs accepts --store=<dir> and --port=<n>. No other flag is served by
// this milestone: advertising lands with the sync card and auth with the write
// card, so both refuse instead of silently doing nothing.
func parseArgs(args []string) (config, error) {
	cfg := config{store: flintstore.DefaultStoreRoot, port: defaultPort}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--store" || a == "--port":
			if i+1 >= len(args) {
				return cfg, flintstore.Refuse("usage", "missing flag value")
			}
			i++
			if a == "--store" {
				cfg.store = args[i]
			} else {
				p, ok := parsePort(args[i])
				if !ok {
					return cfg, flintstore.Refuse("usage", "invalid port")
				}
				cfg.port = p
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
		case a == "--advertise" || hasPrefix(a, "--advertise="):
			return cfg, flintstore.Refuse("usage", "advertise not served by this daemon")
		case a == "--auth-key" || hasPrefix(a, "--auth-key="):
			return cfg, flintstore.Refuse("usage", "auth not served by this daemon")
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
