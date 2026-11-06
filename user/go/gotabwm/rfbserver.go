package main

import (
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"virelai/rfb"
	"virelai/tls"
	"virelai/vi"
)

// RFB is opt-in, one viewer per invocation, on the hermetic --net test attachment only.
// None authenticates nobody. There is no LAN/remote deployment mode here:
// ADR 0037 D6 requires an authenticated transport for that. The kernel TCP
// machine owns only one connection system-wide while this listener is open.
// The "--rfb-vencrypt" lane terminates VeNCrypt 0.2 / X509Plain on the same
// attachment: the M135 TLS 1.3 server half authenticates the seat's Ed25519
// certificate, then a staged credential inside the channel authenticates the
// viewer. X509Vnc is refused — guest-side DES is rejected (ADR 0037 D6), so
// Plain-over-TLS is the smallest honest X509 subtype.
const (
	rfbPort       = 5900
	rfbInputLimit = 16
)

const (
	rfbSecVeNCrypt       = 19
	vencryptSubX509Plain = 262
	vencryptCredMax      = 1024
)

const (
	markerRFBListen    = "gotabwm: rfb listen 5900 (None; hermetic only)"
	markerRFBListenTLS = "gotabwm: rfb listen 5900 (VeNCrypt X509Plain; hermetic only)"
	markerRFBReady     = "gotabwm: rfb ready"
	markerRFBFrame     = "gotabwm: rfb frame"
	markerRFBKey       = "gotabwm: rfb key usage="
	markerRFBPointer   = "gotabwm: rfb pointer x="
	markerRFBRelease   = "gotabwm: rfb release x="
	markerRFBDrop      = "gotabwm: rfb drop "
	markerRFBDone      = "gotabwm: rfb done"
)

// Drop reasons name why the one session ended, so a gate can tell the
// refusal it provoked from an unrelated failure.
const (
	rfbDropPeer      = "peer"      // FIN/RST: the viewer or its host went away
	rfbDropMalformed = "malformed" // the codec refused a client message
	rfbDropTimeout   = "timeout"   // a started read never completed
	rfbDropStalled   = "stalled"   // the viewer stopped acknowledging sends
	rfbDropFlood     = "flood"     // input arrived faster than the seat drains it
	rfbDropSocket    = "socket"    // the kernel no longer reports this socket
	rfbDropPanic     = "panic"     // a codec bug on hostile input, contained
	rfbDropIO        = "io"
)

var (
	errRFBSendStalled = errors.New("rfb: TCP send stalled")
	errRFBSocket      = errors.New("rfb: TCP readiness refused")
	errRFBInputFlood  = errors.New("rfb: input during send overflowed")
)

type remoteInput struct {
	event vi.Event
	kind  rfb.MessageKind
}

type rfbServer struct {
	// Main holds frameMu across the existing paint/present, the worker holds
	// it only while taking a snapshot. No socket I/O runs under it.
	frameMu     sync.Mutex
	scan        []byte
	presented   uint64
	input       chan remoteInput
	closed      atomic.Bool
	lastPoint   uint32 // seat goroutine only; release at this point on death
	lastButtons uint8
}

// rfbConfig is the parsed RFB opt-in: the plain None lane, or the VeNCrypt
// lane with its staged credential paths (share files the run sets up).
type rfbConfig struct {
	vencrypt           bool
	certPath, seedPath string
	credPath           string
}

// rfbRequested parses the operator's opt-in. Exactly "--rfb-hermetic" is
// the None lane; "--rfb-vencrypt CERT SEED CRED" is the TLS lane. Neither
// is ever implied — no argument, no listener.
func rfbRequested(args []string) *rfbConfig {
	switch {
	case len(args) == 2 && args[1] == "--rfb-hermetic":
		return &rfbConfig{}
	case len(args) == 5 && args[1] == "--rfb-vencrypt":
		return &rfbConfig{vencrypt: true, certPath: args[2], seedPath: args[3], credPath: args[4]}
	}
	return nil
}

func startRFB(args []string, scan []byte) *rfbServer {
	cfg := rfbRequested(args)
	if cfg == nil {
		return nil
	}
	if len(scan) != vi.ScanoutFbBytes {
		vi.ConsoleLine("gotabwm: rfb scanout refused")
		return nil
	}
	var conn *vi.Conn
	var err error
	if cfg.vencrypt {
		// The TLS lane rides the per-handle socket ABI: the legacy
		// listener's slot-76 probe does not drain the virtio RX ring, and
		// tls.Server is defined on a SocketListen child.
		conn, err = vi.SocketListen(rfbPort)
	} else {
		conn, err = vi.Listen(rfbPort)
	}
	if err != nil {
		vi.ConsoleLine("gotabwm: rfb listen refused " + err.Error())
		return nil
	}
	s := &rfbServer{scan: scan, input: make(chan remoteInput, rfbInputLimit)}
	if cfg.vencrypt {
		vi.ConsoleLine(markerRFBListenTLS)
	} else {
		vi.ConsoleLine(markerRFBListen)
	}
	go s.serve(conn, cfg)
	return s
}

// One bounded connection and one bounded frame cache. A failed negotiation,
// malformed message, peer death, or stalled read closes the socket. No
// reconnect loop silently re-exposes None after the explicit invocation.
func (s *rfbServer) serve(conn *vi.Conn, cfg *rfbConfig) {
	reason := rfbDropIO
	defer func() {
		_ = conn.Close()
		vi.ConsoleLine(markerRFBDrop + reason)
		vi.ConsoleLine(markerRFBDone)
		// Never enqueue teardown into the finite client queue: an input
		// flood could fill it and lose the one release that unwinds a drag.
		s.closed.Store(true)
	}()
	// The worker shares the seat's process: a codec bug on hostile bytes
	// must end this session, not the compositor.
	defer func() {
		if recover() != nil {
			reason = rfbDropPanic
		}
	}()
	if cfg.vencrypt {
		child, err := conn.AcceptConn()
		if err != nil {
			vi.ConsoleLine("gotabwm: rfb accept refused " + err.Error())
			reason = "accept"
			return
		}
		tc, err := vencryptServe(child, cfg)
		if err != nil {
			_ = child.Close()
			reason = "handshake " + rfbDropReason(err)
			vi.ConsoleLine("gotabwm: rfb handshake refused " + err.Error())
			return
		}
		defer func() { _ = tc.Close() }()
		session, err := rfb.NewSession(tc, tc, vi.ScanoutWidth, vi.ScanoutHeight, "VirelaiOS")
		if err != nil {
			return
		}
		if _, err = session.HandshakeEstablished(); err != nil {
			reason = "handshake " + rfbDropReason(err)
			vi.ConsoleLine("gotabwm: rfb handshake refused")
			return
		}
		vi.ConsoleLine(markerRFBReady)
		reason = s.run(session, tlsReady(tc, child), func() { vi.Sleep(1) })
		return
	}
	if err := conn.Accept(); err != nil {
		vi.ConsoleLine("gotabwm: rfb accept refused " + err.Error())
		reason = "accept"
		return
	}
	stream := newRFBStream(conn)
	session, err := rfb.NewSession(stream, stream, vi.ScanoutWidth, vi.ScanoutHeight, "VirelaiOS")
	if err != nil {
		return
	}
	if _, err = session.Handshake(); err != nil {
		reason = "handshake " + rfbDropReason(err)
		vi.ConsoleLine("gotabwm: rfb handshake refused")
		return
	}
	vi.ConsoleLine(markerRFBReady)
	reason = s.run(session, stream.poll, stream.idle)
}

// tlsReady is the TLS session loop's readiness probe on the accepted
// handle: socket-readable, or decrypted plaintext the record layer already
// holds (a record can carry more RFB messages than one ReadMessage
// consumes).
func tlsReady(tc *tls.TLSConn, conn *vi.Conn) func() (int64, int64) {
	return func() (int64, int64) {
		mask, err := conn.ReadyMask()
		if err != nil {
			return 0, -1
		}
		if tc.Buffered() > 0 {
			mask |= 1
		}
		return mask, 0
	}
}

// vencryptStream adapts the accepted child to the pre-TLS negotiation:
// sends pace on the one-pending-segment rule AND drain the RX ring — a
// pending segment's ACK (and any early viewer bytes) only leaves the ring
// when drained, so two back-to-back writes without a drain wedge. The
// drain here is the handle child's own recv, deadline-armed into a poll.
func vencryptStream(child *vi.Conn) *rfbStream {
	return &rfbStream{
		conn: child,
		ready: func() (int64, int64) {
			mask, err := child.ReadyMask()
			if err != nil {
				return 0, -1
			}
			return mask, 0
		},
		idle:     func() { vi.Sleep(1) },
		sendIdle: vi.Yield,
		drain: func(p []byte) (int, int64) {
			child.SetRecvDeadline(1)
			defer child.SetRecvDeadline(0)
			n, err := child.Recv(p)
			switch {
			case err == nil:
				return n, 0
			case err.Error() == "ETIMEDOUT":
				return 0, 0
			default:
				return n, -1
			}
		},
		stallNs: vi.DefaultRecvBudgetNs,
	}
}

// vencryptServe runs the VeNCrypt security phase end to end: the plaintext
// offer/version/subtype exchange (vencryptHello), then the M135 TLS 1.3
// server half on the same socket, then the X509Plain credential check and
// SecurityResult inside the established channel. The codec resumes at
// ClientInit (HandshakeEstablished) on the returned connection.
func vencryptServe(conn *vi.Conn, cfg *rfbConfig) (*tls.TLSConn, error) {
	tlsCfg, cred, err := loadVeNCrypt(cfg)
	if err != nil {
		return nil, err
	}
	ns := vencryptStream(conn)
	if err := vencryptHello(ns, ns); err != nil {
		return nil, err
	}
	if ns.heldN != 0 {
		// A viewer that pipelined past the subtype selection would strand
		// drained bytes the TLS layer still expects: refuse, don't lose.
		return nil, fmt.Errorf("%w: bytes pipelined past the subtype", rfb.ErrProtocol)
	}
	tc, err := tls.Server(conn, *tlsCfg)
	if err != nil {
		return nil, err
	}
	if err := x509plainCheck(tc, tc, cred); err != nil {
		_ = tc.Close()
		return nil, err
	}
	return tc, nil
}

// loadVeNCrypt reads the staged TLS.CRT (DER), TLS.SEED (raw 32-byte
// Ed25519 seed), and RFB.CRED ("user\npassword\n") share files.
func loadVeNCrypt(cfg *rfbConfig) (*tls.ServerConfig, [2][]byte, error) {
	var cred [2][]byte
	certDER, rc := vi.ReadFileAll(cfg.certPath, 4096)
	seedBytes, rc2 := vi.ReadFileAll(cfg.seedPath, 64)
	if rc < 0 || rc2 < 0 || len(seedBytes) != 32 {
		return nil, cred, errors.New("rfb: vencrypt credential load")
	}
	raw, rc3 := vi.ReadFileAll(cfg.credPath, vencryptCredMax)
	if rc3 < 0 {
		return nil, cred, errors.New("rfb: vencrypt credential load")
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 || len(lines[0]) == 0 || len(lines[1]) == 0 ||
		len(lines[0]) > 255 || len(lines[1]) > 255 {
		return nil, cred, errors.New("rfb: vencrypt credential shape")
	}
	cred[0], cred[1] = []byte(lines[0]), []byte(lines[1])
	var seed [32]byte
	copy(seed[:], seedBytes)
	return &tls.ServerConfig{Chain: [][]byte{certDER}, Ed25519Seed: seed}, cred, nil
}

// vencryptHello is the plaintext half of VeNCrypt 0.2 (rfbproto §7.2.4):
// the {19} security offer, the version exchange, and the X509Plain subtype
// offer. Each stage that has a refusal spelling writes it; a bad subtype
// selection ends the connection the way the spec leaves it — closed.
// This lane is RFB 3.8 only: 3.3 cannot select a security type and 3.7
// gains nothing here.
func vencryptHello(r io.Reader, w io.Writer) error {
	if err := rfbWriteAll(w, []byte("RFB 003.008\n")); err != nil {
		return fmt.Errorf("vencrypt banner write: %w", err)
	}
	var banner [12]byte
	if _, err := io.ReadFull(r, banner[:]); err != nil {
		return fmt.Errorf("vencrypt banner read: %w", err)
	}
	if string(banner[:]) != "RFB 003.008\n" {
		return fmt.Errorf("%w: vencrypt requires RFB 3.8", rfb.ErrProtocol)
	}
	if err := rfbWriteAll(w, []byte{1, rfbSecVeNCrypt}); err != nil {
		return fmt.Errorf("vencrypt offer write: %w", err)
	}
	var sel [1]byte
	if _, err := io.ReadFull(r, sel[:]); err != nil {
		return fmt.Errorf("vencrypt select read: %w", err)
	}
	if sel[0] != rfbSecVeNCrypt {
		return rfbSecurityFail(w, "rfb: VeNCrypt lane offers type 19 only")
	}
	if err := rfbWriteAll(w, []byte{0, 2}); err != nil { // VeNCrypt 0.2
		return fmt.Errorf("vencrypt version write: %w", err)
	}
	var version [2]byte
	if _, err := io.ReadFull(r, version[:]); err != nil {
		return fmt.Errorf("vencrypt version read: %w", err)
	}
	if version != [2]byte{0, 2} {
		_ = rfbWriteAll(w, []byte{0xff}) // version refusal, then close
		return fmt.Errorf("%w: vencrypt version", rfb.ErrProtocol)
	}
	var offer [6]byte
	offer[0] = 0 // version accepted
	offer[1] = 1 // one subtype
	binary.BigEndian.PutUint32(offer[2:], vencryptSubX509Plain)
	if err := rfbWriteAll(w, offer[:]); err != nil {
		return fmt.Errorf("vencrypt subtype write: %w", err)
	}
	var subtype [4]byte
	if _, err := io.ReadFull(r, subtype[:]); err != nil {
		return fmt.Errorf("vencrypt subtype read: %w", err)
	}
	if binary.BigEndian.Uint32(subtype[:]) != vencryptSubX509Plain {
		return fmt.Errorf("%w: vencrypt subtype", rfb.ErrProtocol)
	}
	return nil
}

// rfbSecurityFail writes SecurityResult=failed + u32-length reason and
// returns the refusal as an error — the same spelling the codec's own
// security refusal uses.
func rfbSecurityFail(w io.Writer, why string) error {
	reply := make([]byte, 8+len(why))
	binary.BigEndian.PutUint32(reply[:4], 1)
	binary.BigEndian.PutUint32(reply[4:8], uint32(len(why)))
	copy(reply[8:], why)
	_ = rfbWriteAll(w, reply)
	return fmt.Errorf("%w: %s", rfb.ErrProtocol, why)
}

// x509plainCheck answers the X509Plain credential exchange inside the
// established channel: u32 user-length, user, u32 password-length,
// password, then SecurityResult. The compare is constant-time; the
// credential is the hermetic fixture, never logged.
func x509plainCheck(r io.Reader, w io.Writer, cred [2][]byte) error {
	readField := func() ([]byte, error) {
		var n [4]byte
		if _, err := io.ReadFull(r, n[:]); err != nil {
			return nil, err
		}
		l := binary.BigEndian.Uint32(n[:])
		if l == 0 || l > 255 {
			return nil, fmt.Errorf("%w: vencrypt credential field", rfb.ErrProtocol)
		}
		b := make([]byte, l)
		if _, err := io.ReadFull(r, b); err != nil {
			return nil, err
		}
		return b, nil
	}
	refuse := func(err error) error {
		_ = rfbSecurityFail(w, "rfb: X509Plain credential refused")
		return err
	}
	user, err := readField()
	if err != nil {
		return refuse(err)
	}
	pass, err := readField()
	if err != nil {
		return refuse(err)
	}
	if subtle.ConstantTimeCompare(user, cred[0]) == 1 &&
		subtle.ConstantTimeCompare(pass, cred[1]) == 1 {
		return rfbWriteAll(w, []byte{0, 0, 0, 0})
	}
	return rfbSecurityFail(w, "rfb: X509Plain credential refused")
}

func rfbWriteAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

// run is the post-handshake session loop. It returns the drop reason. The
// readiness probe and idle wait are the kernel's in the guest and a byte
// stream's in host tests, so hostile-input tests drive this exact loop.
func (s *rfbServer) run(session *rfb.Session, ready func() (int64, int64), idle func()) string {
	// One immutable snapshot (3,686,400 B), allocated only after a viewer
	// requests pixels. The codec also owns a sent-frame cache and known-pixel
	// map; full updates transiently allocate an encoded body and packet copy.
	// All are framebuffer-bounded, with no frame backlog or client queue growth.
	var frame []byte
	var lastPresent uint64
	var pending *rfb.Request // exactly one outstanding update, no frame queue
	var mods rfbModifiers
	for {
		if pending != nil {
			s.frameMu.Lock()
			present := s.presented
			fresh := present != lastPresent
			if fresh {
				copy(frame, s.scan)
				lastPresent = present
			}
			s.frameMu.Unlock()
			if present != 0 && (fresh || !pending.Incremental) {
				// A full request sends the latest presented frame. An
				// incremental request is tested once per *new* present.
				sent, updateErr := session.FramebufferUpdate(*pending, frame)
				if updateErr != nil {
					return rfbDropReason(updateErr)
				}
				if sent {
					vi.ConsoleLine(markerRFBFrame)
					pending = nil
				}
			}
		}
		// Poll instead of blocking in ReadMessage between messages: a
		// pending incremental request must not starve keyboard/pointer
		// input when the screen is unchanged. The kernel tick is the
		// frame clock; a quiet client costs one sleeping worker.
		mask, rc := ready()
		if rc < 0 {
			return rfbDropSocket
		}
		if mask&1 == 0 {
			idle()
			continue
		}
		msg, err := session.ReadMessage()
		if err != nil {
			return rfbDropReason(err)
		}
		switch msg.Kind {
		case rfb.UpdateRequest:
			req := msg.Request
			if pending != nil {
				// A viewer may ask again before an unchanged screen has
				// answered. Merging keeps one bounded request, no queue.
				req = mergeRFBRequests(*pending, req)
			}
			if frame == nil {
				frame = make([]byte, vi.ScanoutFbBytes)
			}
			pending = &req
		case rfb.KeyInput:
			if e, ok := mods.key(msg.Key); ok && !s.enqueue(remoteInput{event: e, kind: rfb.KeyInput}) {
				return rfbDropFlood
			}
		case rfb.PointerInput:
			// ReadMessage already rejects coordinates outside the 1280x720
			// session. Ignore wheel/extra button bits, which have no seat
			// representation, without disconnecting an otherwise valid viewer.
			buttons := rfbButtonsToSeat(msg.Pointer.Buttons)
			e := vi.Event{
				Kind: vi.EvWmPointer, Flags: uint16(buttons),
				Arg0: uint32(msg.Pointer.X) | uint32(msg.Pointer.Y)<<16,
			}
			if !s.enqueue(remoteInput{event: e, kind: rfb.PointerInput}) {
				return rfbDropFlood
			}
		}
	}
}

// mergeRFBRequests covers both rectangles, which ReadMessage has already
// bounded to the frame. Any full request makes the merge a full one.
func mergeRFBRequests(a, b rfb.Request) rfb.Request {
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.Width, b.X+b.Width), max(a.Y+a.Height, b.Y+b.Height)
	return rfb.Request{
		Incremental: a.Incremental && b.Incremental,
		Rectangle:   rfb.Rectangle{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0},
	}
}

func rfbDropReason(err error) string {
	switch {
	case errors.Is(err, rfb.ErrProtocol), errors.Is(err, rfb.ErrUnsupported):
		return rfbDropMalformed
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, vi.ErrPeerGone), errors.Is(err, vi.ErrConnClosed),
		tls.IsPeerClosed(err):
		return rfbDropPeer
	case errors.Is(err, errRFBSendStalled):
		return rfbDropStalled
	case errors.Is(err, errRFBInputFlood):
		return rfbDropFlood
	case errors.Is(err, errRFBSocket):
		return rfbDropSocket
	case tls.IsTimeout(err):
		return rfbDropTimeout
	case err != nil && err.Error() == "ETIMEDOUT": // vi keeps its errno type private
		return rfbDropTimeout
	}
	return rfbDropIO
}

func (s *rfbServer) enqueue(e remoteInput) bool {
	select {
	case s.input <- e:
		return true
	default:
		// An input flood is a failed session, never an unbounded backlog.
		return false
	}
}

// Input is consumed only on the seat goroutine. No socket worker can race
// tabs, launcher state, the hardware event drain, or content forwarding.
func (s *rfbServer) drainInput() bool {
	for i := 0; i < rfbInputLimit; i++ {
		select {
		case item := <-s.input:
			consumeSeatEvent(item.event) // exactly the kind-19/21 seat path
			if item.kind == rfb.KeyInput {
				vi.ConsoleLine(markerRFBKey + vi.Itoa64(int64(item.event.Arg0)))
			} else {
				s.lastPoint = item.event.Arg0
				s.lastButtons = uint8(item.event.Flags)
				x, y := item.event.Arg0&0xffff, item.event.Arg0>>16
				vi.ConsoleLine(markerRFBPointer + vi.Itoa64(int64(x)) +
					" y=" + vi.Itoa64(int64(y)) +
					" buttons=" + vi.Itoa64(int64(item.event.Flags)))
			}
		default:
			return s.finishInput()
		}
	}
	return s.finishInput()
}

func (s *rfbServer) finishInput() bool {
	if !s.closed.Load() || len(s.input) != 0 {
		return false
	}
	// A dead viewer's held button is released where it was last seen, as
	// HID removal does: the seat and the content app both see the up edge.
	if s.lastButtons != 0 && prevPtrButtons != 0 {
		consumeSeatEvent(vi.Event{Kind: vi.EvWmPointer, Arg0: s.lastPoint})
		vi.ConsoleLine(markerRFBRelease + vi.Itoa64(int64(s.lastPoint&0xffff)) +
			" y=" + vi.Itoa64(int64(s.lastPoint>>16)))
	}
	return true // drop the final snapshot/cache when the one viewer leaves
}

// RFB button 2 means middle and 3 means right; the seat's HID bit 1
// means right and bit 2 means middle. Left is bit 0 on both wires.
// Higher RFB button bits (wheel and extra buttons) have no seat mapping.
func rfbButtonsToSeat(b uint8) uint8 {
	return b&1 | (b&2)<<1 | (b&4)>>1
}

type rfbModifiers struct {
	shift, ctrl, alt, cmd uint8
	held                  [256]bool
}

// WM_KEY reports *down edges* with the current modifier flags. Key releases
// only update the held-modifier state, like the hardware's boot report.
func (m *rfbModifiers) key(k rfb.KeyEvent) (vi.Event, bool) {
	if !k.Known {
		return vi.Event{}, false
	}
	if k.Down && m.held[k.Usage] {
		return vi.Event{}, false // the seat gets down edges, not repeats
	}
	m.held[k.Usage] = k.Down
	var held *uint8
	switch k.Usage {
	case 0xe1, 0xe5:
		held = &m.shift
	case 0xe0, 0xe4:
		held = &m.ctrl
	case 0xe2, 0xe6:
		held = &m.alt
	case 0xe3, 0xe7:
		held = &m.cmd
	}
	if held != nil {
		bit := uint8(1)
		if k.Usage&0x04 != 0 {
			bit = 2
		}
		if k.Down {
			*held |= bit
		} else {
			*held &^= bit
		}
	}
	if !k.Down || held != nil {
		return vi.Event{}, false
	}
	var flags uint16
	if m.shift != 0 {
		flags |= vi.ModShift
	}
	if m.ctrl != 0 {
		flags |= vi.ModCtrl
	}
	if m.alt != 0 {
		flags |= vi.ModAlt
	}
	if m.cmd != 0 {
		flags |= vi.ModCmd
	}
	return vi.Event{Kind: vi.EvWmKey, Flags: flags, Arg0: uint32(k.Usage)}, true
}

// rfbSocket is the part of *vi.Conn the stream uses, so host tests can
// script the viewer's side of a send that waits on its ACK.
type rfbSocket interface {
	Recv(p []byte) (int, error)
	Send(p []byte) (int, error)
	SetRecvDeadline(ns int64)
}

// The codec uses io.Reader/io.Writer; vi.Conn intentionally does not claim
// POSIX net.Conn semantics. TCP's one pending segment must be ACKed before
// sending the next <=1460 B chunk. The stall budget bounds each unACKed
// segment, not a whole update. Slot 76 probes readiness but does NOT drain
// incoming ACKs; slot 32 does. While a send waits, drain via TCPRecv and
// buffer any viewer input for Read. The quiet session loop still sleeps.
// A raw full frame needs ~2,525 ACKs, so compression remains necessary.
type rfbStream struct {
	conn     rfbSocket
	ready    func() (int64, int64)
	idle     func()
	sendIdle func()
	drain    func([]byte) (int, int64)
	stallNs  int64
	// Viewer bytes taken while a send waited on its ACK, owed to Read.
	held  [2 * vi.TCPPayloadMax]byte
	heldN int
}

func newRFBStream(conn *vi.Conn) *rfbStream {
	return &rfbStream{conn: conn, ready: vi.TCPReady, idle: func() { vi.Sleep(1) },
		sendIdle: vi.Yield, drain: vi.TCPRecv,
		stallNs: vi.DefaultRecvBudgetNs}
}

// poll is the session loop's readiness probe: held bytes are readable even
// though the kernel's queue is already empty.
func (s *rfbStream) poll() (int64, int64) {
	mask, rc := s.ready()
	if rc == 0 && s.heldN > 0 {
		mask |= 1
	}
	return mask, rc
}

func (s *rfbStream) Read(p []byte) (int, error) {
	if s.heldN > 0 {
		n := copy(p, s.held[:s.heldN])
		s.heldN = copy(s.held[:], s.held[n:s.heldN])
		return n, nil
	}
	n, err := s.conn.Recv(p)
	if err == vi.ErrPeerGone {
		return 0, io.EOF
	}
	return n, err
}

func (s *rfbStream) Write(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		deadline := vi.Nanos() + s.stallNs
		for {
			mask, rc := s.ready()
			if rc < 0 {
				return total, errRFBSocket
			}
			if mask&2 != 0 {
				break
			}
			if mask&1 != 0 {
				if err := s.takeReadable(); err != nil {
					return total, err
				}
			}
			if s.drain != nil {
				// TCPRecv drains ACK-only frames as well as viewer input.
				// Input taken here is owed to the session's next Read.
				buf := s.held[s.heldN:]
				if len(buf) == 0 {
					var scratch [1]byte
					buf = scratch[:]
				}
				n, rc := s.drain(buf)
				if rc < 0 {
					return total, errRFBSocket
				}
				if s.heldN == len(s.held) {
					if n > 0 {
						return total, errRFBInputFlood
					}
				} else {
					s.heldN += n
				}
				mask, rc = s.ready()
				if rc < 0 {
					return total, errRFBSocket
				}
				if mask&2 != 0 {
					break
				}
			}
			if vi.Nanos() >= deadline {
				return total, errRFBSendStalled
			}
			s.sendIdle()
		}
		end := total + vi.TCPPayloadMax
		if end > len(p) {
			end = len(p)
		}
		n, err := s.conn.Send(p[total:end])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// takeReadable settles a socket that is readable while a send waits: the
// viewer sent input (held for Read), or its RST/FIN arrived (the empty read
// is ErrPeerGone). A dead viewer must not cost the whole stall budget. Its
// held bytes die with it, as a pulled HID device's last report does.
func (s *rfbStream) takeReadable() error {
	s.conn.SetRecvDeadline(1)
	defer s.conn.SetRecvDeadline(0)
	// The kernel queues at most one segment, so the second read settles it.
	for i := 0; i < 2 && s.heldN+vi.TCPPayloadMax <= len(s.held); i++ {
		n, err := s.conn.Recv(s.held[s.heldN : s.heldN+vi.TCPPayloadMax])
		s.heldN += n
		if err != nil {
			if rfbDropReason(err) == rfbDropTimeout {
				return nil // nothing more queued: still just waiting on the ACK
			}
			return err
		}
	}
	return nil
}
