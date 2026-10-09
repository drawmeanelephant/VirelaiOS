// Package ipc is the gsport IPC adapter: an in-memory net.Listener and
// dialer under the mem:// endpoint scheme.
//
// Host assumption it replaces: Gostalgia's IPC transport is a unix domain
// socket under os.TempDir (platform/ipc_unix.go) or a loopback TCP
// listener (platform/ipc_windows.go). VirelaiOS has neither — no unix
// sockets, no TCP loopback, no listener table (kernel/src/tcp.zig).
// Gostalgia's only IPC peer on the guest is the shell inside the SAME
// process, so the honest transport is an in-process channel pair, not an
// emulation of a kernel object that does not exist.
//
// Slots: none. net.Pipe is channel plumbing that never reaches an SVC.
//
// Refuses by name: any endpoint that is not mem://<name>, dialling a name
// with no live listener, and a second listener on the same name.
package ipc

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
)

// Scheme is the endpoint scheme the adapter owns. Endpoints have the form
// mem://<name>; the name is opaque to the transport (the platform overlay
// derives it from the environment root).
const Scheme = "mem"

// Endpoint renders name as a mem:// endpoint URI.
func Endpoint(name string) string { return Scheme + "://" + name }

// Parse splits a mem:// endpoint into its name, refusing every other
// scheme by name — a mistyped unix:// or tcp:// must fail here, not reach
// a socket path that would report ENOSYS without saying why.
func Parse(endpoint string) (string, error) {
	name, ok := strings.CutPrefix(endpoint, Scheme+"://")
	if !ok {
		return "", fmt.Errorf("ipc: endpoint %q: no unix sockets or TCP loopback on virelai — want %s://<name>", endpoint, Scheme)
	}
	if name == "" || strings.ContainsAny(name, "/?#") {
		return "", fmt.Errorf("ipc: bad %s endpoint %q", Scheme, endpoint)
	}
	return name, nil
}

// backlog bounds unaccepted connections per listener.
const backlog = 16

// Addr is a mem:// socket address.
type Addr string

func (a Addr) Network() string { return Scheme }
func (a Addr) String() string  { return string(a) }

// Listener is an in-memory net.Listener: Accept returns connections a
// Dial handed over through net.Pipe.
type Listener struct {
	name    string
	pending chan net.Conn
	done    chan struct{}
	once    sync.Once
}

var (
	mu        sync.Mutex
	listeners = map[string]*Listener{}
)

// Listen registers a mem:// endpoint and returns its listener.
func Listen(endpoint string) (net.Listener, error) {
	name, err := Parse(endpoint)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := listeners[name]; dup {
		return nil, fmt.Errorf("ipc: %s in use", endpoint)
	}
	l := &Listener{
		name:    name,
		pending: make(chan net.Conn, backlog),
		done:    make(chan struct{}),
	}
	listeners[name] = l
	return l, nil
}

// Dial connects to a live mem:// listener, refusing an absent one by name.
func Dial(endpoint string) (net.Conn, error) {
	name, err := Parse(endpoint)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	l := listeners[name]
	mu.Unlock()
	if l == nil {
		return nil, fmt.Errorf("ipc: dial %s: no such listener", endpoint)
	}
	client, server := net.Pipe()
	select {
	case l.pending <- server:
		return &conn{Conn: client, local: Addr(name), remote: Addr(name)}, nil
	case <-l.done:
		client.Close()
		server.Close()
		return nil, fmt.Errorf("ipc: dial %s: listener closed", endpoint)
	}
}

func (l *Listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.pending:
		return &conn{Conn: c, local: Addr(l.name), remote: Addr(l.name)}, nil
	case <-l.done:
		return nil, errors.New("ipc: listener closed")
	}
}

func (l *Listener) Close() error {
	l.once.Do(func() {
		mu.Lock()
		delete(listeners, l.name)
		mu.Unlock()
		close(l.done)
	})
	return nil
}

func (l *Listener) Addr() net.Addr { return Addr(l.name) }

// conn stamps the pipe ends with the endpoint's address so callers that
// log conn.RemoteAddr() see the mem:// name rather than "pipe".
type conn struct {
	net.Conn
	local, remote Addr
}

func (c *conn) LocalAddr() net.Addr  { return c.local }
func (c *conn) RemoteAddr() net.Addr { return c.remote }
