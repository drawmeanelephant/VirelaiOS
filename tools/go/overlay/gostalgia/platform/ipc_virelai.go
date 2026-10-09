//go:build virelai

package platform

import (
	"crypto/sha1"
	"fmt"
	"net"

	"virelai/gsport/ipc"
)

// ListenIPC opens the environment's local IPC listener. The pinned hosts
// reach for a unix socket path under os.TempDir or a loopback TCP port —
// VirelaiOS has neither (no unix sockets, no TCP loopback, no listener
// table), and the environment's only IPC peer on the guest is the shell
// inside the same process. The endpoint is therefore a mem:// name served
// by gsport/ipc's in-process listener: same shape, no kernel slot burned.
//
// The name is derived from the environment root exactly like the pinned
// unix path (sha1 of root, first 5 bytes) so two environments never
// collide and the mapping stays debuggable.
func ListenIPC(root string) (net.Listener, string, error) {
	sum := sha1.Sum([]byte(root))
	endpoint := ipc.Endpoint(fmt.Sprintf("gostalgia-%x", sum[:5]))
	ln, err := ipc.Listen(endpoint)
	if err != nil {
		return nil, "", fmt.Errorf("platform: listen %s: %w", endpoint, err)
	}
	return ln, endpoint, nil
}
