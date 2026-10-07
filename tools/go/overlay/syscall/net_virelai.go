// The socket-shaped syscall surface, for GOOS=virelai.
//
// internal/poll's FD type is one type for files AND sockets: its Accept,
// Recvfrom, Sendto, Recvmsg, SendmsgN and Shutdown methods compile on every
// unix-shaped GOOS whether or not the platform has sockets. This GOOS has
// none — the kernel's slot table (ADR 0007) has no socket, bind, listen or
// connect — so the types exist for the compiler and every operation refuses.
//
// The types are the POSIX shapes on purpose: poll hands their field
// addresses to nothing (it cannot, since the calls refuse), but net-shaped
// code and the guest SDK read Addr/Port, and a member layout that disagreed
// with what a caller expects would be a silent lie rather than a loud one.
package syscall

// Iovec is the scatter/gather element internal/poll caches per descriptor.
type Iovec struct {
	Base *byte
	Len  uint64
}

func (iov *Iovec) SetLen(length int) { iov.Len = uint64(length) }

// SockaddrInet4/6 are storage for addresses this GOOS cannot produce.
type SockaddrInet4 struct {
	Port int
	Addr [4]byte
}

func (sa *SockaddrInet4) sockaddr() {}

type SockaddrInet6 struct {
	Port   int
	ZoneId uint32
	Addr   [16]byte
}

func (sa *SockaddrInet6) sockaddr() {}

// SockaddrUnix is storage for unix-domain addresses this GOOS cannot
// produce either. net's unixsock plumbing names the type whether or not a
// unix socket can exist, so the shape exists and the ops refuse.
type SockaddrUnix struct {
	Name string
}

func (sa *SockaddrUnix) sockaddr() {}

// The address-family, socket-type and protocol constants net's *_posix.go
// plumbing names (issue #2029). No slot consumes them — a socket() call
// refuses before any of these are delivered to a kernel that does not have
// the concept — but they must exist as constants for the port to compile,
// and a caller that spells AF_INET6 must get back the refusal its family
// asked for, not a missing symbol.
const (
	AF_UNSPEC = iota
	AF_UNIX
	AF_INET
	AF_INET6
)

const (
	SOCK_STREAM = 1 + iota
	SOCK_DGRAM
	SOCK_RAW
	SOCK_SEQPACKET
)

const (
	IPPROTO_IP   = 0
	IPPROTO_TCP  = 6
	IPPROTO_UDP  = 0x11
	IPPROTO_IPV6 = 0x29
)

const (
	IPV6_V6ONLY = 1
	SO_ERROR    = 2
)

// SOMAXCONN is the backlog bound sock_stub.go's maxListenerBacklog reports.
// The number is advisory on every GOOS; here it is also unreachable, since
// listen() refuses before a backlog is ever requested.
const SOMAXCONN = 0x80

// Bind and SetsockoptInt are compile-time leaves of net's capability probe
// (ipsock_posix.go's probe()): it calls sysSocket first, which refuses, so
// neither is ever reached at run time — but both are named in its body and
// must exist for the package to compile. Same story for Socket, Connect,
// Listen and GetsockoptInt: nothing that runs selects them (hook_unix.go's
// test-hook assignments are not part of this port), so they exist as the
// same refusal the rest of this file gives.
func Socket(domain, stype, proto int) (fd int, err error)     { return -1, ENOSYS }
func Bind(fd int, sa Sockaddr) error                          { return ENOSYS }
func Connect(fd int, sa Sockaddr) error                       { return ENOSYS }
func Listen(fd int, backlog int) error                        { return ENOSYS }
func GetsockoptInt(fd, level, opt int) (value int, err error) { return -1, ENOSYS }
func SetsockoptInt(fd, level, opt int, value int) error       { return ENOSYS }

// The socket operations. Each is ENOSYS rather than a zero result: a caller
// that reads a byte count of 0 believes it hit EOF, and there is no socket to
// hit EOF on.
func Accept4(fd int, flags int) (nfd int, sa Sockaddr, err error) { return -1, nil, ENOSYS }
func Shutdown(fd int, how int) error                              { return ENOSYS }

func Recvfrom(fd int, p []byte, flags int) (n int, from Sockaddr, err error) {
	return 0, nil, ENOSYS
}

func Sendto(fd int, p []byte, flags int, to Sockaddr) error { return ENOSYS }

func Recvmsg(fd int, p, oob []byte, flags int) (n, oobn, recvflags int, from Sockaddr, err error) {
	return 0, 0, 0, nil, ENOSYS
}

func SendmsgN(fd int, p, oob []byte, to Sockaddr, flags int) (n int, err error) {
	return 0, ENOSYS
}

// The Inet4/Inet6 specializations are the LINKNAME TARGETS of
// internal/syscall/unix/net.go, which `internal/poll` imports and which
// apply.sh deliberately selects for this GOOS (its FD type names
// unix.RecvfromInet4 and friends whether or not a socket can exist). Stock
// declares these eight in syscall/syscall_unix.go, a file tagged `unix` —
// which this GOOS never selects, so without them the link fails with
// "relocation target syscall.recvfromInet4 not defined". They are named here
// to match those linknames exactly, unexported and with the stock signatures,
// because a linkname is matched by NAME and shape, not by package boundary.
//
// Each returns ENOSYS exactly as its generic sibling above does, and for the
// same reason: stock's versions differ only in that they decode the peer
// address out of a RawSockaddr buffer after a successful call, and there is
// no call here to succeed. Returning a zero n would read as EOF on a socket
// that cannot exist, so the refusal is the honest answer. `from` and `to`
// are left untouched for the same reason — writing them would report a peer
// that was never contacted.
func recvfromInet4(fd int, p []byte, flags int, from *SockaddrInet4) (n int, err error) {
	return 0, ENOSYS
}

func recvfromInet6(fd int, p []byte, flags int, from *SockaddrInet6) (n int, err error) {
	return 0, ENOSYS
}

func recvmsgInet4(fd int, p, oob []byte, flags int, from *SockaddrInet4) (n, oobn int, recvflags int, err error) {
	return 0, 0, 0, ENOSYS
}

func recvmsgInet6(fd int, p, oob []byte, flags int, from *SockaddrInet6) (n, oobn int, recvflags int, err error) {
	return 0, 0, 0, ENOSYS
}

func sendmsgNInet4(fd int, p, oob []byte, to *SockaddrInet4, flags int) (n int, err error) {
	return 0, ENOSYS
}

func sendmsgNInet6(fd int, p, oob []byte, to *SockaddrInet6, flags int) (n int, err error) {
	return 0, ENOSYS
}

func sendtoInet4(fd int, p []byte, flags int, to *SockaddrInet4) (err error) {
	return ENOSYS
}

func sendtoInet6(fd int, p []byte, flags int, to *SockaddrInet6) (err error) {
	return ENOSYS
}

// Pread lives in fs_virelai.go: the port answers positional reads from the
// shadow it keeps for each real handle (M70c-S1T, issue #1543), because the
// toolchain's readers and writers both need a position and the kernel's file
// channel is cursor-only.
//
// Pwrite stays refused. Staging a positional WRITE would need a handle the
// port can rewind and rewrite, which only exists for a handle it opened
// itself for writing; a caller that hands the port an inherited fd and asks
// for an offset write is refused rather than guessed at. Nothing in the
// toolchain path does that (cmd/link writes through bio.Writer's Seek, which
// fs_virelai.go's Seek implements).
func Pwrite(fd int, p []byte, offset int64) (n int, err error) { return 0, ENOSYS }

// No socket seam exists, so the connection-shaped errors cannot be produced
// here. They are defined because std compares against them (net, internal/poll
// and os's error classification) and a missing constant would read as a
// compile error rather than as "this GOOS has no sockets", which is the fact.
const (
	EADDRINUSE      Errno = 26
	EADDRNOTAVAIL   Errno = 27
	EAFNOSUPPORT    Errno = 28
	ECONNABORTED    Errno = 29
	ECONNREFUSED    Errno = 30
	ECONNRESET      Errno = 31
	EHOSTUNREACH    Errno = 32
	EINPROGRESS     Errno = 33
	EISCONN         Errno = 34
	ENETDOWN        Errno = 35
	ENETUNREACH     Errno = 36
	ENOTCONN        Errno = 37
	EOPNOTSUPP      Errno = 38
	ENOTSUP         Errno = 38 // the same refusal, spelled as os does
	EPROTONOSUPPORT Errno = 39
)
