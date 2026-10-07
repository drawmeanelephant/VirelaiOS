// The `net` platform layer for GOOS=virelai (issue #2029).
//
// This kernel has no socket slots (ADR 0007): no socket(2), bind, listen,
// connect or accept exists to call. The goal of this port is narrower than
// a real network stack: `net` must compile so that in-process IPC users —
// net.Pipe, and net.Conn/net.Listener implementations like Gostalgia's
// (M95b, #2010) — can use the package's interfaces, while every path that
// would need a kernel socket refuses with a named error.
//
// The js/wasip1 answer — widened net_fake.go/fd_fake.go tags — was
// evaluated and rejected: the fake fabric is a WORKING in-memory net, so
// net.Listen("tcp") and Dial("tcp", "127.0.0.1:...") would silently
// succeed. The card requires TCP loopback/listeners and unix sockets to
// refuse, so this port keeps the stock *_posix.go plumbing (address
// resolution, OpError wrapping, listener bookkeeping) and refuses at the
// leaf it funnels into: socket(). A Dial that reaches it reports
// "socket: function not implemented" through the stock OpError/AddrError
// wrapping, and a netFD can therefore never exist — every method below is
// a refusal reached only by a caller that conjured the fd itself.
//
// What does work: net.Pipe (pipe.go is untagged channel plumbing that
// never names netFD), the Addr/Conn/Listener interfaces and all address
// parsing. What refuses: dial/listen on every network, DNS lookups (the
// go resolver's dial refuses), interface enumeration, the hosts file and
// FileConn/FileListener/FilePacketConn.
package net

import (
	"context"
	"internal/poll"
	"os"
	"syscall"
	"time"
)

// netFD is the descriptor a stock socket would own. Its fields mirror the
// other ports' because the *_posix.go plumbing reads them; pfd is a real
// poll.FD because rawconn.go takes its address unconditionally. No field is
// ever populated — socket() refuses — so the pfd is always the zero value.
type netFD struct {
	pfd poll.FD

	// immutable until Close
	family      int
	sotype      int
	isConnected bool // handshake completed or use of association with peer
	net         string
	laddr       Addr
	raddr       Addr
}

// The two constructors. socket is the leaf every dial and listen flows
// through (internetSocket and unixSocket both end here); sysSocket is the
// leaf the capability probe() calls. Both refuse: there is no socket slot
// to fill the fd they would return.
func socket(ctx context.Context, net string, family, sotype, proto int, ipv6only bool, laddr, raddr sockaddr, ctrlCtxFn func(context.Context, string, string, syscall.RawConn) error) (*netFD, error) {
	return nil, os.NewSyscallError("socket", syscall.ENOSYS)
}

func sysSocket(family, sotype, proto int) (int, error) {
	return -1, os.NewSyscallError("sysSocket", syscall.ENOSYS)
}

// The netFD method set the stock plumbing names. A netFD cannot exist —
// socket() refuses — so each method is a refusal, not an operation. They
// return ENOSYS rather than delegating to pfd because a pfd whose Sysfd
// was never assigned is descriptor 0, and silently reading the guest's
// console as "the socket" would be a lie the refusal exists to prevent.
func (fd *netFD) accept() (*netFD, error) { return nil, os.NewSyscallError("accept", syscall.ENOSYS) }

func (fd *netFD) closeRead() error  { return os.NewSyscallError("closeRead", syscall.ENOSYS) }
func (fd *netFD) closeWrite() error { return os.NewSyscallError("closeWrite", syscall.ENOSYS) }

func (fd *netFD) Close() error                    { return os.NewSyscallError("close", syscall.ENOSYS) }
func (fd *netFD) Read(p []byte) (int, error)      { return 0, os.NewSyscallError("read", syscall.ENOSYS) }
func (fd *netFD) Write(p []byte) (int, error)     { return 0, os.NewSyscallError("write", syscall.ENOSYS) }
func (fd *netFD) SetDeadline(t time.Time) error   { return os.NewSyscallError("setDeadline", syscall.ENOSYS) }
func (fd *netFD) SetReadDeadline(t time.Time) error {
	return os.NewSyscallError("setDeadline", syscall.ENOSYS)
}
func (fd *netFD) SetWriteDeadline(t time.Time) error {
	return os.NewSyscallError("setDeadline", syscall.ENOSYS)
}
func (fd *netFD) dup() (*os.File, error) { return nil, os.NewSyscallError("dup", syscall.ENOSYS) }

func (fd *netFD) readFrom(p []byte) (n int, sa syscall.Sockaddr, err error) {
	return 0, nil, os.NewSyscallError("readFrom", syscall.ENOSYS)
}
func (fd *netFD) readFromInet4(p []byte, sa *syscall.SockaddrInet4) (int, error) {
	return 0, os.NewSyscallError("readFrom", syscall.ENOSYS)
}
func (fd *netFD) readFromInet6(p []byte, sa *syscall.SockaddrInet6) (int, error) {
	return 0, os.NewSyscallError("readFrom", syscall.ENOSYS)
}
func (fd *netFD) readMsg(p, oob []byte, flags int) (n, oobn, retflags int, sa syscall.Sockaddr, err error) {
	return 0, 0, 0, nil, os.NewSyscallError("readMsg", syscall.ENOSYS)
}
func (fd *netFD) readMsgInet4(p, oob []byte, flags int, sa *syscall.SockaddrInet4) (n, oobn, retflags int, err error) {
	return 0, 0, 0, os.NewSyscallError("readMsg", syscall.ENOSYS)
}
func (fd *netFD) readMsgInet6(p, oob []byte, flags int, sa *syscall.SockaddrInet6) (n, oobn, retflags int, err error) {
	return 0, 0, 0, os.NewSyscallError("readMsg", syscall.ENOSYS)
}
func (fd *netFD) writeTo(p []byte, sa syscall.Sockaddr) (int, error) {
	return 0, os.NewSyscallError("writeTo", syscall.ENOSYS)
}
func (fd *netFD) writeToInet4(p []byte, sa *syscall.SockaddrInet4) (int, error) {
	return 0, os.NewSyscallError("writeTo", syscall.ENOSYS)
}
func (fd *netFD) writeToInet6(p []byte, sa *syscall.SockaddrInet6) (int, error) {
	return 0, os.NewSyscallError("writeTo", syscall.ENOSYS)
}
func (fd *netFD) writeMsg(p, oob []byte, sa syscall.Sockaddr) (n, oobn int, err error) {
	return 0, 0, os.NewSyscallError("writeMsg", syscall.ENOSYS)
}
func (fd *netFD) writeMsgInet4(p, oob []byte, sa *syscall.SockaddrInet4) (n, oobn int, err error) {
	return 0, 0, os.NewSyscallError("writeMsg", syscall.ENOSYS)
}
func (fd *netFD) writeMsgInet6(p, oob []byte, sa *syscall.SockaddrInet6) (n, oobn int, err error) {
	return 0, 0, os.NewSyscallError("writeMsg", syscall.ENOSYS)
}

// The socket-option hooks tcpsock.go/tcpsock_unix.go/net.go name. There is
// no socket to hold an option, so each is ENOSYS — the same refusal the
// syscall layer gives (overlay/syscall/net_virelai.go), not ENOPROTOOPT,
// which would claim an option table exists and merely lacks this entry.
func setReadBuffer(fd *netFD, bytes int) error         { return syscall.ENOSYS }
func setWriteBuffer(fd *netFD, bytes int) error        { return syscall.ENOSYS }
func setLinger(fd *netFD, sec int) error               { return syscall.ENOSYS }
func setKeepAlive(fd *netFD, keepalive bool) error     { return syscall.ENOSYS }
func setKeepAliveIdle(fd *netFD, d time.Duration) error     { return syscall.ENOSYS }
func setKeepAliveInterval(fd *netFD, d time.Duration) error { return syscall.ENOSYS }
func setKeepAliveCount(fd *netFD, n int) error              { return syscall.ENOSYS }
func setNoDelay(fd *netFD, noDelay bool) error              { return syscall.ENOSYS }

// The multicast hooks udpsock_posix.go's listenMulticastUDP path names.
func setIPv4MulticastInterface(fd *netFD, ifi *Interface) error  { return syscall.ENOSYS }
func setIPv4MulticastLoopback(fd *netFD, v bool) error           { return syscall.ENOSYS }
func joinIPv4Group(fd *netFD, ifi *Interface, ip IP) error       { return syscall.ENOSYS }
func setIPv6MulticastInterface(fd *netFD, ifi *Interface) error  { return syscall.ENOSYS }
func setIPv6MulticastLoopback(fd *netFD, v bool) error           { return syscall.ENOSYS }
func joinIPv6Group(fd *netFD, ifi *Interface, ip IP) error       { return syscall.ENOSYS }

// FileConn/FileListener/FilePacketConn adapt an existing os.File into a
// socket. There is no socket-shaped file to adapt, so they refuse rather
// than wrapping the file's poll.FD in a netFD and lying about it.
func fileConn(f *os.File) (Conn, error)             { return nil, syscall.ENOSYS }
func fileListener(f *os.File) (Listener, error)     { return nil, syscall.ENOSYS }
func filePacketConn(f *os.File) (PacketConn, error) { return nil, syscall.ENOSYS }

// Interface enumeration. The guest DOES own a virtio-net device — the vi
// socket seam drives it — but nothing at the syscall layer can enumerate
// it for `net`, so the honest answer is the named refusal, not the empty
// table js/wasip1's interface_stub returns (an empty table reads as "no
// interfaces", which is false).
func interfaceTable(ifindex int) ([]Interface, error) {
	return nil, os.NewSyscallError("interfaceTable", syscall.ENOSYS)
}
func interfaceAddrTable(ifi *Interface) ([]Addr, error) {
	return nil, os.NewSyscallError("interfaceAddrTable", syscall.ENOSYS)
}
func interfaceMulticastAddrTable(ifi *Interface) ([]Addr, error) {
	return nil, os.NewSyscallError("interfaceMulticastAddrTable", syscall.ENOSYS)
}

// hostsFilePath is where hosts.go looks for a static table. The guest has
// no /etc (no filesystem layout convention at all — paths are a VFS seat's
// business), so the path is one that can never resolve; readHostsFile's
// open fails, the lookup falls through to the DNS path, and the DNS dial
// refuses. The named path keeps the host-file log lines truthful.
const hostsFilePath = "/etc/hosts"

// concurrentThreadsLimit bounds how many threads the Go resolver may block
// at once (net.go's acquireThread). Every lookup refuses inside socket(),
// so the bound is bookkeeping; a small value keeps a misbehaving caller
// from spawning lookup goroutines the refusal would retire anyway.
func concurrentThreadsLimit() int { return 4 }
