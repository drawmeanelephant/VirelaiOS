// pinned-sha256: 854148798ff12df6291189e4788f693524ddea1c3bbad375cb85317e2aff5be9
//
// Overlay replacement for the pinned platform.go. The pin's DialIPC
// switches unix:// and tcp:// — host transports that do not exist on the
// guest (no unix sockets, no loopback; net.Pipe plumbing is the only
// "socket" a virelai process can hold). Under GOOS=virelai the endpoint
// scheme is mem://, served by gsport/ipc.
//
// This file is identical to the pinned source except for the DialIPC
// scheme set; keep the package doc verbatim so the replace stays
// reviewable.

// Package platform isolates the host-specific integration points of the
// environment: the small set of things the standard library does not
// already abstract, such as the local IPC listener mechanism. Everything
// here must stay thin; if a file grows, the concern probably belongs in
// the subsystem it serves.
package platform

import (
	"fmt"
	"net"
	"strings"

	"virelai/gsport/ipc"
)

// DialIPC connects to a local environment IPC endpoint. On the guest the
// only endpoint scheme is mem://<name>, produced by ListenIPC in
// ipc_virelai.go. unix:// and tcp:// are refused BY NAME here — never
// allowed to reach a socket call that would answer ENOSYS without saying
// which assumption was wrong.
func DialIPC(endpoint string) (net.Conn, error) {
	switch {
	case strings.HasPrefix(endpoint, "unix://"), strings.HasPrefix(endpoint, "tcp://"):
		return nil, fmt.Errorf("platform: endpoint %q names a host transport virelai does not have (no unix sockets, no TCP loopback); want %s://<name>", endpoint, ipc.Scheme)
	case strings.HasPrefix(endpoint, ipc.Scheme+"://"):
		return ipc.Dial(endpoint)
	default:
		return nil, fmt.Errorf("platform: unknown endpoint scheme %q", endpoint)
	}
}
