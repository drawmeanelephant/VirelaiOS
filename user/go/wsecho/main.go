// Command wsecho is the M212c (issue #3874) hermetic leg driver: a
// WebSocket client over virelai/ws that a gate spec runs against the
// runner's :relay'd fixture. It exists so the echo leg can hold a real
// guest binary — the codec's own tests run on the host; this is the on-
// target proof that the same bytes cross the virtio net.
//
//	exec WSECHO.ELF <host> <port> [--path=/x] [--max-ms N]
//	exec WSECHO.ELF <host> <port> --refuse
//
// Default mode runs the echo script: text echo, binary echo, client
// ping/pong, the fixture's fragmented-text + server-ping half, then the
// close handshake. --refuse dials once per poisoned fixture conn and
// names each refusal the codec returns.
//
// Markers (wsecho: prefix, grep-able):
//
//	wsecho: dial <host> <port>          wsecho: handshake ok
//	wsecho: text ok                     wsecho: binary ok
//	wsecho: frag ok                     wsecho: ping srv
//	wsecho: pong ok                     wsecho: close ok
//	wsecho: done                        wsecho: refuse <name>
//	wsecho: refuse-miss want=<w> got=<g> wsecho: error <err>
package main

import (
	"bytes"
	"strconv"

	"virelai/vi"
	"virelai/vsys"
	"virelai/ws"
)

const (
	appName = "WSECHO.ELF"

	readBudgetNs = int64(30 * 1000 * 1000 * 1000) // per-dial bound
)

var argvPad [2048]byte // see fetch/main.go — sbrk/argv tail slack

func main() {
	args := vi.Args()
	if len(args) > 0 && (args[0] == appName || args[0] == "WSECHO" || args[0] == "wsecho") {
		args = args[1:]
	}
	if len(args) < 2 {
		vi.ConsoleLine("usage: WSECHO.ELF <host> <port> [--path=/x] [--refuse]")
		vi.Exit(1)
	}
	host := args[0]
	port64, perr := strconv.Atoi(args[1])
	if perr != nil || port64 <= 0 || port64 > 65535 {
		vi.ConsoleLine("wsecho: error bad-port")
		vi.Exit(1)
	}
	port := uint16(port64)
	path := "/"
	refuse := false
	for _, a := range args[2:] {
		switch {
		case a == "--refuse":
			refuse = true
		case len(a) > 7 && a[:7] == "--path=":
			path = a[7:]
		default:
			vi.ConsoleLine("wsecho: error bad-flag")
			vi.Exit(1)
		}
	}
	if refuse {
		vi.Exit(refuseRun(host, port))
	}
	vi.Exit(echoRun(host, port, path))
}

// dialOnce opens the transport and runs the handshake. The refused
// handshakes surface as ws's named Errors.
func dialOnce(host string, port uint16, path string) (*vi.Conn, *ws.Conn, error) {
	vi.ConsoleLine("wsecho: dial " + host + " " + vsys.Itoa64(int64(port)))
	tc, err := vi.Dial(host, port)
	if err != nil {
		return nil, nil, err
	}
	tc.SetRecvDeadline(readBudgetNs)
	c, err := ws.Dial(viPipe{tc}, ws.Options{
		Host: host + ":" + vsys.Itoa64(int64(port)),
		Path: path,
		Rand: vi.Random,
	})
	if err != nil {
		_ = tc.Close()
		return nil, nil, err
	}
	vi.ConsoleLine("wsecho: handshake ok")
	return tc, c, nil
}

// viPipe adapts vi.Conn's Send/Recv onto ws.Pipe.
type viPipe struct{ c *vi.Conn }

func (p viPipe) Read(b []byte) (int, error)  { return p.c.Recv(b) }
func (p viPipe) Write(b []byte) (int, error) { return p.c.Send(b) }

// echoRun is the leg's script: everything the codec promises in one
// session, order-tolerant (the fixture's unsolicited half interleaves).
func echoRun(host string, port uint16, path string) int {
	tc, c, err := dialOnce(host, port, path)
	if err != nil {
		vi.ConsoleLine("wsecho: error " + err.Error())
		return 1
	}
	defer tc.Close()

	textMsg := "wsecho-text-7"
	binMsg := []byte{0xde, 0xad, 0xbe, 0xef}
	if err := c.Send(ws.OpText, []byte(textMsg)); err != nil {
		return failErr("send-text", err)
	}
	if err := c.Send(ws.OpBinary, binMsg); err != nil {
		return failErr("send-binary", err)
	}
	if err := c.SendPing([]byte("wsecho-ping")); err != nil {
		return failErr("send-ping", err)
	}

	// The exchange set — the peer's close is NOT here: the fixture
	// answers a client close, it never opens one, so the read loop ends
	// on the five expected messages and the close handshake runs after.
	want := map[string]bool{
		"text": false, // our text echoed
		"bin":  false, // our binary echoed
		"frag": false, // the fixture's fragmented text reassembled
		"srvp": false, // the fixture's ping (already auto-ponged)
		"pong": false, // the answer to our ping
	}
	for i := 0; i < 16 && !allSeen(want); i++ {
		op, pl, err := c.ReadMessage()
		if err != nil {
			vi.ConsoleLine("wsecho: error " + err.Error())
			return 1
		}
		switch op {
		case ws.OpText:
			s := string(pl)
			if s == textMsg {
				want["text"] = true
				vi.ConsoleLine("wsecho: text ok")
			} else if s == "wsecho-frag-head-tail" {
				want["frag"] = true
				vi.ConsoleLine("wsecho: frag ok")
			}
		case ws.OpBinary:
			if bytes.Equal(pl, binMsg) {
				want["bin"] = true
				vi.ConsoleLine("wsecho: binary ok")
			}
		case ws.OpPing:
			want["srvp"] = true // ReadMessage already sent the pong
			vi.ConsoleLine("wsecho: ping srv")
		case ws.OpPong:
			if string(pl) == "wsecho-ping" {
				want["pong"] = true
				vi.ConsoleLine("wsecho: pong ok")
			}
		case ws.OpClose:
			vi.ConsoleLine("wsecho: close ok")
		}
	}
	for name, seen := range want {
		if !seen {
			vi.ConsoleLine("wsecho: error missing " + name)
			return 1
		}
	}
	// The close handshake: our close out, the peer's answer back.
	if err := c.Close(1000); err != nil {
		return failErr("send-close", err)
	}
	for i := 0; i < 8; i++ {
		op, _, err := c.ReadMessage()
		if err == ws.ErrClosed {
			break
		}
		if err != nil {
			vi.ConsoleLine("wsecho: error " + err.Error())
			return 1
		}
		if op == ws.OpClose {
			vi.ConsoleLine("wsecho: close ok")
			break
		}
	}
	if !c.PeerClosed() {
		vi.ConsoleLine("wsecho: error missing close")
		return 1
	}
	vi.ConsoleLine("wsecho: done")
	return 0
}

func allSeen(want map[string]bool) bool {
	for _, seen := range want {
		if !seen {
			return false
		}
	}
	return true
}

func failErr(what string, err error) int {
	vi.ConsoleLine("wsecho: error " + what + " " + err.Error())
	return 1
}

// refuseRun dials once per poisoned fixture connection — the fixture
// counts accepts and serves conn-indexed poison: bad opcode, a masked
// server frame, a declared-oversize frame, a bad Accept checksum, an
// unnegotiated extension offer. Each dial must fail (or the first read
// must refuse) with the named Error.
func refuseRun(host string, port uint16) int {
	wants := []string{
		string(ws.RefuseOpcode),
		string(ws.RefuseServerMasked),
		string(ws.RefuseFrameLarge),
		string(ws.RefuseAccept),
		string(ws.RefuseExtension),
	}
	for _, want := range wants {
		tc, c, err := dialOnce(host, port, "/")
		got := ""
		switch {
		case err != nil:
			got = err.Error()
		default:
			_, _, err = c.ReadMessage()
			if err != nil {
				got = err.Error()
			}
		}
		if tc != nil {
			_ = tc.Close()
		}
		if got != want {
			vi.ConsoleLine("wsecho: refuse-miss want=" + want + " got=" + got)
			return 1
		}
		vi.ConsoleLine("wsecho: refuse " + want)
	}
	vi.ConsoleLine("wsecho: refuse done")
	return 0
}
