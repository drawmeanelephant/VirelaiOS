package main

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"virelai/rfb"
	"virelai/vi"
)

// RFB is opt-in, one viewer, on the hermetic --net test attachment only.
// None authenticates nobody. There is no LAN/remote deployment mode here:
// ADR 0037 D6 requires an authenticated transport for that. The kernel TCP
// machine owns only one connection system-wide while this listener is open.
const (
	rfbPort       = 5900
	rfbInputLimit = 16
	rfbWaitTicks  = 30
)

const (
	markerRFBListen  = "gotabwm: rfb listen 5900 (None; hermetic only)"
	markerRFBReady   = "gotabwm: rfb ready"
	markerRFBFrame   = "gotabwm: rfb frame"
	markerRFBKey     = "gotabwm: rfb key usage="
	markerRFBPointer = "gotabwm: rfb pointer x="
	markerRFBDone    = "gotabwm: rfb done"
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
	defer func() {
		_ = conn.Close()
		// Never enqueue teardown into the finite client queue: an input
		// flood could fill it and lose the one release that unwinds a drag.
		s.closed.Store(true)
		vi.ConsoleLine(markerRFBDone)
	}()
	if err := conn.Accept(); err != nil {
		vi.ConsoleLine("gotabwm: rfb accept refused " + err.Error())
		return
	}
	stream := &rfbStream{conn: conn}
	session, err := rfb.NewSession(stream, stream, vi.ScanoutWidth, vi.ScanoutHeight, "VirelaiOS")
	if err != nil {
		return
	}
	if _, err = session.Handshake(); err != nil {
		vi.ConsoleLine("gotabwm: rfb handshake refused")
		return
	}
	vi.ConsoleLine(markerRFBReady)

	// Exactly one immutable snapshot (3,686,400 B), allocated only after a
	// viewer requests pixels. The codec owns at most one sent-frame cache
	// and one known-pixel map; no frame backlog or unbounded client queue.
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
					return
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
		mask, rc := vi.TCPReady()
		if rc < 0 {
			return
		}
		if mask&1 == 0 {
			vi.Sleep(1)
			continue
		}
		msg, err := session.ReadMessage()
		if err != nil {
			return
		}
		switch msg.Kind {
		case rfb.UpdateRequest:
			if pending != nil {
				return // one outstanding request, no unbounded work queue
			}
			if frame == nil {
				frame = make([]byte, vi.ScanoutFbBytes)
			}
			req := msg.Request
			pending = &req
		case rfb.KeyInput:
			if e, ok := mods.key(msg.Key); ok && !s.enqueue(remoteInput{event: e, kind: rfb.KeyInput}) {
				return
			}
		case rfb.PointerInput:
			if msg.Pointer.Buttons&^uint8(0x07) != 0 {
				return // only the seat's left/right/middle button bits
			}
			buttons := rfbButtonsToSeat(msg.Pointer.Buttons)
			e := vi.Event{
				Kind: vi.EvWmPointer, Flags: uint16(buttons),
				Arg0: uint32(msg.Pointer.X) | uint32(msg.Pointer.Y)<<16,
			}
			if !s.enqueue(remoteInput{event: e, kind: rfb.PointerInput}) {
				return
			}
		}
	}
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
	if s.lastButtons != 0 && prevPtrButtons != 0 {
		consumeSeatEvent(vi.Event{Kind: vi.EvWmPointer, Arg0: s.lastPoint})
	}
	return true // drop the final snapshot/cache when the one viewer leaves
}

// RFB button 2 means middle and 3 means right; the seat's HID bit 1
// means right and bit 2 means middle. Left is bit 0 on both wires.
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

// The codec uses io.Reader/io.Writer; vi.Conn intentionally does not claim
// POSIX net.Conn semantics. TCP's one pending segment must be ACKed before
// sending the next <=192 B chunk. A deadline bounds each stalled segment.
type rfbStream struct{ conn *vi.Conn }

func (s *rfbStream) Read(p []byte) (int, error) {
	n, err := s.conn.Recv(p)
	if err == vi.ErrPeerGone {
		return 0, io.EOF
	}
	return n, err
}

func (s *rfbStream) Write(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		for polls := 0; ; polls++ {
			mask, rc := vi.TCPReady()
			if rc < 0 {
				return total, fmt.Errorf("rfb: TCP readiness %d", rc)
			}
			if mask&2 != 0 {
				break
			}
			if polls >= rfbWaitTicks {
				return total, fmt.Errorf("rfb: TCP send stalled")
			}
			vi.Sleep(1)
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
