package rfb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
)

const (
	MaxWidth     = 1280
	MaxHeight    = 720
	MaxFrameSize = MaxWidth * MaxHeight * 4
	MaxEncodings = 256
	MaxCursor    = 64

	EncodingRaw         int32 = 0
	EncodingRRE         int32 = 2
	EncodingHextile     int32 = 5
	EncodingDesktopSize int32 = -223
	EncodingRichCursor  int32 = -239
)

var (
	ErrProtocol    = errors.New("rfb: malformed or unsupported message")
	ErrUnsupported = errors.New("rfb: encoding not advertised")
)

// PixelFormat is the 16-byte RFB true-color format, not the scanout layout.
type PixelFormat struct {
	BitsPerPixel, Depth, BigEndian, TrueColor uint8
	RedMax, GreenMax, BlueMax                 uint16
	RedShift, GreenShift, BlueShift           uint8
}

var DefaultPixelFormat = PixelFormat{
	BitsPerPixel: 32, Depth: 24, TrueColor: 1,
	RedMax: 255, GreenMax: 255, BlueMax: 255,
	RedShift: 16, GreenShift: 8, BlueShift: 0,
}

func (p PixelFormat) valid() bool {
	if p.BitsPerPixel != 8 && p.BitsPerPixel != 16 && p.BitsPerPixel != 32 ||
		p.Depth == 0 || p.Depth > p.BitsPerPixel || p.BigEndian > 1 || p.TrueColor != 1 {
		return false
	}
	var used uint32
	significant := 0
	for _, c := range []struct {
		max   uint16
		shift uint8
	}{{p.RedMax, p.RedShift}, {p.GreenMax, p.GreenShift}, {p.BlueMax, p.BlueShift}} {
		n := bits.Len16(c.max)
		if n == 0 || uint32(c.max) != (uint32(1)<<n)-1 ||
			int(c.shift)+n > int(p.Depth) {
			return false
		}
		mask := uint32(c.max) << c.shift
		if used&mask != 0 {
			return false
		}
		used |= mask
		significant += n
	}
	return significant <= int(p.Depth)
}

func (p PixelFormat) wire() [16]byte {
	var b [16]byte
	b[0], b[1], b[2], b[3] = p.BitsPerPixel, p.Depth, p.BigEndian, p.TrueColor
	binary.BigEndian.PutUint16(b[4:6], p.RedMax)
	binary.BigEndian.PutUint16(b[6:8], p.GreenMax)
	binary.BigEndian.PutUint16(b[8:10], p.BlueMax)
	b[10], b[11], b[12] = p.RedShift, p.GreenShift, p.BlueShift
	return b
}

func parsePixelFormat(b []byte) (PixelFormat, error) {
	p := PixelFormat{
		BitsPerPixel: b[0], Depth: b[1], BigEndian: b[2], TrueColor: b[3],
		RedMax:   binary.BigEndian.Uint16(b[4:6]),
		GreenMax: binary.BigEndian.Uint16(b[6:8]),
		BlueMax:  binary.BigEndian.Uint16(b[8:10]),
		RedShift: b[10], GreenShift: b[11], BlueShift: b[12],
	}
	if !p.valid() {
		return PixelFormat{}, fmt.Errorf("%w: pixel format", ErrProtocol)
	}
	return p, nil
}

// Session owns protocol and sent-frame state, but not its underlying stream.
// Its methods must be called serially; a caller owns connection teardown.
type Session struct {
	r io.Reader
	w io.Writer

	Width, Height uint16
	Name          string
	Format        PixelFormat
	version       int
	ready         bool
	preferred     int32
	desktopSize   bool
	richCursor    bool
	last          []byte
	known         []byte // one byte per pixel; a partial request cannot mark other pixels sent
}

// NewSession bounds all server-controlled lengths before any wire I/O.
func NewSession(r io.Reader, w io.Writer, width, height uint16, name string) (*Session, error) {
	if r == nil || w == nil || !validSize(width, height) || len(name) > 255 {
		return nil, fmt.Errorf("%w: session dimensions, stream, or name", ErrProtocol)
	}
	return &Session{
		r: r, w: w, Width: width, Height: height, Name: name,
		Format: DefaultPixelFormat, preferred: EncodingRaw,
	}, nil
}

func validSize(w, h uint16) bool {
	return w != 0 && h != 0 && w <= MaxWidth && h <= MaxHeight
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n < 0 || n > len(b) {
			return io.ErrShortWrite
		}
		b = b[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

// Handshake negotiates version/None and ServerInit. It must be called once.
// Any error (including write failure) is terminal for this Session.
func (s *Session) Handshake() (shared bool, err error) {
	if s.version != 0 || s.ready {
		return false, fmt.Errorf("%w: repeated handshake", ErrProtocol)
	}
	s.version = -1 // even a partial handshake cannot be retried
	if err := writeAll(s.w, []byte("RFB 003.008\n")); err != nil {
		return false, err
	}
	var banner [12]byte
	if _, err := io.ReadFull(s.r, banner[:]); err != nil {
		return false, err
	}
	switch string(banner[:]) {
	case "RFB 003.008\n":
		s.version = 8
	case "RFB 003.007\n":
		s.version = 7
	case "RFB 003.003\n":
		s.version = 3
	default:
		return false, fmt.Errorf("%w: client version", ErrProtocol)
	}
	if s.version == 3 {
		var security [4]byte
		binary.BigEndian.PutUint32(security[:], 1) // None, without a selection
		if err := writeAll(s.w, security[:]); err != nil {
			return false, err
		}
	} else {
		if err := writeAll(s.w, []byte{1, 1}); err != nil {
			return false, err
		}
		var selection [1]byte
		if _, err := io.ReadFull(s.r, selection[:]); err != nil {
			return false, err
		}
		if selection[0] != 1 {
			if s.version == 8 {
				reason := []byte("rfb: security type refused")
				reply := make([]byte, 8+len(reason))
				binary.BigEndian.PutUint32(reply[:4], 1)
				binary.BigEndian.PutUint32(reply[4:8], uint32(len(reason)))
				copy(reply[8:], reason)
				if err := writeAll(s.w, reply); err != nil {
					return false, err
				}
			}
			return false, fmt.Errorf("%w: security type", ErrProtocol)
		}
		if s.version == 8 {
			if err := writeAll(s.w, []byte{0, 0, 0, 0}); err != nil {
				return false, err
			}
		}
	}
	var init [1]byte
	if _, err := io.ReadFull(s.r, init[:]); err != nil {
		return false, err
	}
	if init[0] > 1 {
		return false, fmt.Errorf("%w: ClientInit", ErrProtocol)
	}
	reply := make([]byte, 24+len(s.Name))
	binary.BigEndian.PutUint16(reply[:2], s.Width)
	binary.BigEndian.PutUint16(reply[2:4], s.Height)
	p := s.Format.wire()
	copy(reply[4:20], p[:])
	binary.BigEndian.PutUint32(reply[20:24], uint32(len(s.Name)))
	copy(reply[24:], s.Name)
	if err := writeAll(s.w, reply); err != nil {
		return false, err
	}
	s.ready = true
	return init[0] == 1, nil
}

// MessageKind is one of the five client messages this codec accepts.
type MessageKind uint8

const (
	SetPixelFormat MessageKind = 0
	SetEncodings   MessageKind = 2
	UpdateRequest  MessageKind = 3
	KeyInput       MessageKind = 4
	PointerInput   MessageKind = 5
)

// Rectangle is expressed in framebuffer pixels.
type Rectangle struct{ X, Y, Width, Height uint16 }

// Request is a validated FramebufferUpdateRequest.
type Request struct {
	Incremental bool
	Rectangle
}

type KeyEvent struct {
	Down   bool
	Keysym uint32
	Usage  uint8
	Known  bool
}

type PointerEvent struct {
	Buttons uint8
	X, Y    uint16
}

// Message carries only the field relevant to its Kind. Format and encoding
// changes are already applied to the Session when ReadMessage returns.
type Message struct {
	Kind    MessageKind
	Format  PixelFormat
	Request Request
	Key     KeyEvent
	Pointer PointerEvent
}

// ReadMessage reads one bounded client message. An error is terminal.
func (s *Session) ReadMessage() (m Message, err error) {
	if !s.ready {
		return Message{}, fmt.Errorf("%w: handshake required", ErrProtocol)
	}
	defer func() {
		if err != nil {
			s.ready = false
		}
	}()
	var kind [1]byte
	if _, err := io.ReadFull(s.r, kind[:]); err != nil {
		return Message{}, err
	}
	m = Message{Kind: MessageKind(kind[0])}
	switch m.Kind {
	case SetPixelFormat:
		var b [19]byte // 3 padding + 16 format
		if _, err := io.ReadFull(s.r, b[:]); err != nil {
			return Message{}, err
		}
		p, err := parsePixelFormat(b[3:])
		if err != nil {
			return Message{}, err
		}
		s.Format, m.Format = p, p
		s.resetCache()
	case SetEncodings:
		var b [3]byte
		if _, err := io.ReadFull(s.r, b[:]); err != nil {
			return Message{}, err
		}
		n := int(binary.BigEndian.Uint16(b[1:]))
		if n > MaxEncodings {
			return Message{}, fmt.Errorf("%w: encoding count", ErrProtocol)
		}
		codes := make([]byte, n*4)
		if _, err := io.ReadFull(s.r, codes); err != nil {
			return Message{}, err
		}
		s.preferred, s.desktopSize, s.richCursor = EncodingRaw, false, false
		chosen := false
		for i := 0; i < n; i++ {
			code := int32(binary.BigEndian.Uint32(codes[i*4:]))
			switch code {
			case EncodingDesktopSize:
				s.desktopSize = true
			case EncodingRichCursor:
				s.richCursor = true
			case EncodingRaw, EncodingRRE, EncodingHextile:
				if !chosen {
					s.preferred, chosen = code, true
				}
			}
		}
	case UpdateRequest:
		var b [9]byte
		if _, err := io.ReadFull(s.r, b[:]); err != nil {
			return Message{}, err
		}
		if b[0] > 1 {
			return Message{}, fmt.Errorf("%w: incremental flag", ErrProtocol)
		}
		r := Rectangle{
			X: binary.BigEndian.Uint16(b[1:3]), Y: binary.BigEndian.Uint16(b[3:5]),
			Width: binary.BigEndian.Uint16(b[5:7]), Height: binary.BigEndian.Uint16(b[7:9]),
		}
		if !s.validRect(r) {
			return Message{}, fmt.Errorf("%w: update rectangle", ErrProtocol)
		}
		m.Request = Request{Incremental: b[0] == 1, Rectangle: r}
	case KeyInput:
		var b [7]byte
		if _, err := io.ReadFull(s.r, b[:]); err != nil {
			return Message{}, err
		}
		if b[0] > 1 {
			return Message{}, fmt.Errorf("%w: key state", ErrProtocol)
		}
		m.Key.Down = b[0] == 1
		m.Key.Keysym = binary.BigEndian.Uint32(b[3:])
		m.Key.Usage, m.Key.Known = KeysymToHID(m.Key.Keysym)
	case PointerInput:
		var b [5]byte
		if _, err := io.ReadFull(s.r, b[:]); err != nil {
			return Message{}, err
		}
		m.Pointer = PointerEvent{
			Buttons: b[0], X: binary.BigEndian.Uint16(b[1:3]),
			Y: binary.BigEndian.Uint16(b[3:5]),
		}
		if m.Pointer.X >= s.Width || m.Pointer.Y >= s.Height {
			return Message{}, fmt.Errorf("%w: pointer position", ErrProtocol)
		}
	default:
		return Message{}, fmt.Errorf("%w: client message %d", ErrProtocol, kind[0])
	}
	return m, nil
}

func (s *Session) validRect(r Rectangle) bool {
	return r.Width != 0 && r.Height != 0 &&
		uint32(r.X)+uint32(r.Width) <= uint32(s.Width) &&
		uint32(r.Y)+uint32(r.Height) <= uint32(s.Height)
}

func (s *Session) resetCache() {
	s.last, s.known = nil, nil
}
