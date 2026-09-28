package main

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"virelai/rfb"
	"virelai/vi"
)

// RFB is opt-in, one viewer per invocation, on the hermetic --net test attachment only.
// None authenticates nobody. There is no LAN/remote deployment mode here:
// ADR 0037 D6 requires an authenticated transport for that. The kernel TCP
// machine owns only one connection system-wide while this listener is open.
const (
	rfbPort       = 5900
	rfbInputLimit = 16
)

const (
	markerRFBListen  = "gotabwm: rfb listen 5900 (None; hermetic only)"
	markerRFBReady   = "gotabwm: rfb ready"
	markerRFBFrame   = "gotabwm: rfb frame"
	markerRFBKey     = "gotabwm: rfb key usage="
	markerRFBPointer = "gotabwm: rfb pointer x="
	markerRFBRelease = "gotabwm: rfb release x="
	markerRFBDrop    = "gotabwm: rfb drop "
	markerRFBDone    = "gotabwm: rfb done"
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

func rfbRequested(args []string) bool {
	return len(args) == 2 && args[1] == "--rfb-hermetic"
}

func startRFB(args []string, scan []byte) *rfbServer {
	if !rfbRequested(args) {
		return nil
	}
	if len(scan) != vi.ScanoutFbBytes {
		vi.ConsoleLine("gotabwm: rfb scanout refused")
		return nil
	}
	conn, err := vi.Listen(rfbPort)
	if err != nil {
		vi.ConsoleLine("gotabwm: rfb listen refused " + err.Error())
		return nil
	}
	s := &rfbServer{scan: scan, input: make(chan remoteInput, rfbInputLimit)}
	vi.ConsoleLine(markerRFBListen)
	go s.serve(conn)
	return s
}

// One bounded connection and one bounded frame cache. A failed negotiation,
// malformed message, peer death, or stalled read closes the socket. No
// reconnect loop silently re-exposes None after the explicit invocation.
func (s *rfbServer) serve(conn *vi.Conn) {
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
		errors.Is(err, vi.ErrPeerGone), errors.Is(err, vi.ErrConnClosed):
		return rfbDropPeer
	case errors.Is(err, errRFBSendStalled):
		return rfbDropStalled
	case errors.Is(err, errRFBSocket):
		return rfbDropSocket
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
// sending the next <=192 B chunk. The stall budget bounds each unACKed
// segment, not a whole update. It is wall clock, not polls: one worker
// tick can take seconds while the seat is idle. A raw full frame needs
// ~19,200 such ACKs and is not a practical interactive mode on this
// hermetic transport.
type rfbStream struct {
	conn    rfbSocket
	ready   func() (int64, int64)
	idle    func()
	stallNs int64
	// Viewer bytes taken while a send waited on its ACK, owed to Read.
	held  [2 * vi.TCPPayloadMax]byte
	heldN int
}

func newRFBStream(conn *vi.Conn) *rfbStream {
	return &rfbStream{conn: conn, ready: vi.TCPReady, idle: func() { vi.Sleep(1) },
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
			if vi.Nanos() >= deadline {
				return total, errRFBSendStalled
			}
			s.idle()
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
