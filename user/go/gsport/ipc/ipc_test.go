package ipc

import (
	"bufio"
	"fmt"
	"net"
	"testing"
)

var _ net.Listener = (*Listener)(nil)

// TestListenDialRoundtrip proves the mem:// endpoint answers a real
// net.Conn. Revert guard: a "fix" that switches Listen to net.Listen
// ("unix", ...) fails the Network()=="mem" assert even on a host where
// unix sockets exist — the scheme IS the refusal.
func TestListenDialRoundtrip(t *testing.T) {
	ep := Endpoint("gs-test-roundtrip")
	ln, err := Listen(ep)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	if ln.Addr().Network() != Scheme {
		t.Fatalf("listener network %q, want %q", ln.Addr().Network(), Scheme)
	}

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadString('\n')
		fmt.Fprintf(c, "echo:%s", line)
	}()

	conn, err := Dial(ep)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()
	fmt.Fprintln(conn, "hello")
	got, _ := bufio.NewReader(conn).ReadString('\n')
	if got != "echo:hello\n" {
		t.Fatalf("roundtrip got %q", got)
	}
	if conn.LocalAddr().Network() != Scheme {
		t.Fatalf("conn network %q, want %q", conn.LocalAddr().Network(), Scheme)
	}
}

// TestParseRefusesNonMem pins the refusal-by-name: unix:// and tcp:// must
// fail HERE, with a message naming the missing transports — never reach a
// socket call that reports ENOSYS.
func TestParseRefusesNonMem(t *testing.T) {
	for _, ep := range []string{
		"unix:///tmp/sock",
		"tcp://127.0.0.1:7000",
		"/tmp/sock",
		"mem://",
		"mem://a/b",
		"",
	} {
		if _, err := Parse(ep); err == nil {
			t.Errorf("Parse(%q) = nil error, want refusal", ep)
		}
		if _, err := Listen(ep); err == nil {
			t.Errorf("Listen(%q) = nil error, want refusal", ep)
		}
	}
}

func TestDialAbsentAndDoubleListen(t *testing.T) {
	if _, err := Dial(Endpoint("gs-test-absent")); err == nil {
		t.Fatal("Dial of absent listener succeeded")
	}
	ep := Endpoint("gs-test-double")
	ln, err := Listen(ep)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	if _, err := Listen(ep); err == nil {
		t.Fatal("second Listen on same name succeeded")
	}
	// After close the name frees: listen again works.
	if err := ln.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ln2, err := Listen(ep)
	if err != nil {
		t.Fatalf("Listen after Close: %v", err)
	}
	ln2.Close()
}

// TestDialClosedListener: a dial racing a closed listener refuses.
func TestDialClosedListener(t *testing.T) {
	ep := Endpoint("gs-test-closed")
	ln, _ := Listen(ep)
	ln.Close()
	if _, err := Dial(ep); err == nil {
		t.Fatal("Dial of closed listener succeeded")
	}
}
