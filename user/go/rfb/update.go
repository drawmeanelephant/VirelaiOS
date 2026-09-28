package rfb

import (
	"encoding/binary"
	"fmt"
)

func append16(b []byte, v uint16) []byte {
	return append(b, byte(v>>8), byte(v))
}

func append32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func appendRect(b []byte, r Rectangle, encoding int32) []byte {
	b = append16(b, r.X)
	b = append16(b, r.Y)
	b = append16(b, r.Width)
	b = append16(b, r.Height)
	return append32(b, uint32(encoding))
}

func (s *Session) pixel(b []byte, rgb uint32) []byte {
	p := s.Format
	red := (rgb >> 16) & 255
	green := (rgb >> 8) & 255
	blue := rgb & 255
	v := ((red*uint32(p.RedMax) + 127) / 255 << p.RedShift) |
		((green*uint32(p.GreenMax) + 127) / 255 << p.GreenShift) |
		((blue*uint32(p.BlueMax) + 127) / 255 << p.BlueShift)
	switch p.BitsPerPixel {
	case 8:
		return append(b, byte(v))
	case 16:
		if p.BigEndian == 1 {
			return append(b, byte(v>>8), byte(v))
		}
		return append(b, byte(v), byte(v>>8))
	default:
		if p.BigEndian == 1 {
			return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
		}
		return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
}

// rgbAt intentionally ignores the source X byte. B8G8R8X8 is a storage
// format, not a promise that the fourth byte carries wire-visible data.
func rgbAt(frame []byte, stride, x, y int) uint32 {
	i := (y*stride + x) * 4
	return uint32(frame[i]) | uint32(frame[i+1])<<8 | uint32(frame[i+2])<<16
}

func (s *Session) raw(frame []byte, r Rectangle) []byte {
	n := int(r.Width) * int(r.Height) * int(s.Format.BitsPerPixel/8)
	out := make([]byte, 0, n)
	for y := int(r.Y); y < int(r.Y+r.Height); y++ {
		for x := int(r.X); x < int(r.X+r.Width); x++ {
			out = s.pixel(out, rgbAt(frame, int(s.Width), x, y))
		}
	}
	return out
}

// rre uses horizontal runs, stopping as soon as raw would be cheaper. The
// run count and scratch storage cannot grow with unbounded malicious input:
// the framebuffer and the raw payload are both capped.
func (s *Session) rre(frame []byte, r Rectangle) ([]byte, bool) {
	bpp := int(s.Format.BitsPerPixel / 8)
	rawLen := int(r.Width) * int(r.Height) * bpp
	bg := rgbAt(frame, int(s.Width), int(r.X), int(r.Y))
	out := s.pixel(make([]byte, 4, 4+bpp), bg) // u32 count, background pixel
	if len(out) >= rawLen {
		return nil, false
	}
	var count uint32
	for y := 0; y < int(r.Height); y++ {
		for x := 0; x < int(r.Width); {
			color := rgbAt(frame, int(s.Width), int(r.X)+x, int(r.Y)+y)
			start := x
			for x < int(r.Width) && rgbAt(frame, int(s.Width), int(r.X)+x, int(r.Y)+y) == color {
				x++
			}
			if color == bg {
				continue
			}
			if len(out)+bpp+8 >= rawLen {
				return nil, false
			}
			out = s.pixel(out, color)
			out = append16(out, uint16(start))
			out = append16(out, uint16(y))
			out = append16(out, uint16(x-start))
			out = append16(out, 1)
			count++
		}
	}
	binary.BigEndian.PutUint32(out[:4], count)
	return out, true
}

// hextile emits one independently decodable tile at a time. Explicit
// backgrounds avoid relying on a prior tile's colors (especially after raw).
func (s *Session) hextile(frame []byte, r Rectangle) []byte {
	bpp := int(s.Format.BitsPerPixel / 8)
	out := make([]byte, 0, int(r.Width)*int(r.Height)*bpp+int(r.Height/16+1)*int(r.Width/16+1))
	for ty := int(r.Y); ty < int(r.Y+r.Height); ty += 16 {
		h := min(16, int(r.Y+r.Height)-ty)
		for tx := int(r.X); tx < int(r.X+r.Width); tx += 16 {
			w := min(16, int(r.X+r.Width)-tx)
			counts := make(map[uint32]int)
			var bg uint32
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					c := rgbAt(frame, int(s.Width), tx+x, ty+y)
					counts[c]++
					if counts[c] > counts[bg] {
						bg = c
					}
				}
			}
			// Bit 1: background specified. Bit 2: foreground specified
			// when the tile has precisely two colors. Bit 3: subrectangles.
			// Bit 4: each subrect has its own color when >2 colors.
			tile := s.pixel([]byte{2}, bg)
			twoColors := len(counts) == 2
			var fg uint32
			if twoColors {
				for c := range counts {
					if c != bg {
						fg = c
					}
				}
				tile[0] |= 4
				tile = s.pixel(tile, fg)
			}
			subrects := make([]byte, 0, w*h*(bpp+2))
			subcount := 0
			for y := 0; y < h; y++ {
				for x := 0; x < w; {
					c := rgbAt(frame, int(s.Width), tx+x, ty+y)
					start := x
					for x < w && rgbAt(frame, int(s.Width), tx+x, ty+y) == c {
						x++
					}
					if c == bg {
						continue
					}
					if !twoColors {
						subrects = s.pixel(subrects, c)
					}
					subrects = append(subrects, byte(start<<4|y), byte((x-start-1)<<4))
					subcount++
				}
			}
			if subcount > 0 && subcount <= 255 {
				tile[0] |= 8
				if !twoColors {
					tile[0] |= 16
				}
				tile = append(tile, byte(subcount))
				tile = append(tile, subrects...)
			}
			rawSize := 1 + w*h*bpp
			if subcount > 255 || len(tile) >= rawSize {
				out = append(out, 1) // raw tile, including after a colored one
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						out = s.pixel(out, rgbAt(frame, int(s.Width), tx+x, ty+y))
					}
				}
			} else {
				out = append(out, tile...)
			}
		}
	}
	return out
}

func (s *Session) changed(frame []byte, r Rectangle) bool {
	for y := int(r.Y); y < int(r.Y+r.Height); y++ {
		for x := int(r.X); x < int(r.X+r.Width); x++ {
			i := y*int(s.Width) + x
			j := i * 4
			if len(s.known) == 0 || s.known[i] == 0 ||
				frame[j] != s.last[j] || frame[j+1] != s.last[j+1] || frame[j+2] != s.last[j+2] {
				return true
			}
		}
	}
	return false
}

func (s *Session) markSent(frame []byte, r Rectangle) {
	if s.last == nil {
		s.last = make([]byte, len(frame))
		s.known = make([]byte, int(s.Width)*int(s.Height))
	}
	for y := int(r.Y); y < int(r.Y+r.Height); y++ {
		start := y*int(s.Width) + int(r.X)
		end := start + int(r.Width)
		copy(s.last[start*4:end*4], frame[start*4:end*4])
		for i := start; i < end; i++ {
			s.known[i] = 1
		}
	}
}

// FramebufferUpdate sends a full or incremental response to a validated
// request. The frame is the entire current B8G8R8X8 scanout. sent=false
// means no changed pixels, and no bytes written (the caller may wait for
// the next present). A failed write ends the session.
func (s *Session) FramebufferUpdate(req Request, frame []byte) (sent bool, err error) {
	if !s.ready || !s.validRect(req.Rectangle) ||
		len(frame) != int(s.Width)*int(s.Height)*4 {
		return false, fmt.Errorf("%w: frame or request", ErrProtocol)
	}
	rects := make([]Rectangle, 0, 64)
	if !req.Incremental {
		rects = append(rects, req.Rectangle)
	} else {
		for y := int(req.Y); y < int(req.Y+req.Height); y += 16 {
			for x := int(req.X); x < int(req.X+req.Width); x += 16 {
				r := Rectangle{
					X: uint16(x), Y: uint16(y),
					Width:  uint16(min(16, int(req.X+req.Width)-x)),
					Height: uint16(min(16, int(req.Y+req.Height)-y)),
				}
				if s.changed(frame, r) {
					rects = append(rects, r)
				}
			}
		}
	}
	if len(rects) == 0 {
		return false, nil
	}
	// 1280x720 produces at most 80*45 = 3600 dirty tiles.
	packet := []byte{0, 0, byte(len(rects) >> 8), byte(len(rects))}
	for _, r := range rects {
		encoding := s.preferred
		var body []byte
		switch encoding {
		case EncodingRRE:
			if compressed, ok := s.rre(frame, r); ok {
				body = compressed
			} else {
				encoding = EncodingRaw
				body = s.raw(frame, r)
			}
		case EncodingHextile:
			body = s.hextile(frame, r)
		default:
			body = s.raw(frame, r)
		}
		packet = appendRect(packet, r, encoding)
		packet = append(packet, body...)
	}
	if err := writeAll(s.w, packet); err != nil {
		s.ready = false
		return false, err
	}
	for _, r := range rects {
		s.markSent(frame, r)
	}
	return true, nil
}

// DesktopSize announces a new geometry only to a client that advertised it.
// The next update must carry a frame of the new size; sent state is cleared.
func (s *Session) DesktopSize(width, height uint16) error {
	if !s.ready || !s.desktopSize {
		return ErrUnsupported
	}
	if !validSize(width, height) {
		return fmt.Errorf("%w: DesktopSize", ErrProtocol)
	}
	packet := appendRect([]byte{0, 0, 0, 1}, Rectangle{Width: width, Height: height}, EncodingDesktopSize)
	if err := writeAll(s.w, packet); err != nil {
		s.ready = false
		return err
	}
	s.Width, s.Height = width, height
	s.resetCache()
	return nil
}

// Cursor carries B8G8R8X8 pixels and an MSB-first transparency mask.
// The hotspot must lie inside the cursor rectangle.
type Cursor struct {
	HotX, HotY, Width, Height uint16
	Pixels                    []byte
	Mask                      []byte
}

// RichCursor announces a cursor shape only if the client advertised -239.
func (s *Session) RichCursor(c Cursor) error {
	if !s.ready || !s.richCursor {
		return ErrUnsupported
	}
	if c.Width == 0 || c.Height == 0 || c.Width > MaxCursor || c.Height > MaxCursor ||
		c.HotX >= c.Width || c.HotY >= c.Height ||
		len(c.Pixels) != int(c.Width)*int(c.Height)*4 ||
		len(c.Mask) != int((c.Width+7)/8)*int(c.Height) {
		return fmt.Errorf("%w: RichCursor", ErrProtocol)
	}
	packet := appendRect([]byte{0, 0, 0, 1}, Rectangle{c.HotX, c.HotY, c.Width, c.Height}, EncodingRichCursor)
	for y := 0; y < int(c.Height); y++ {
		for x := 0; x < int(c.Width); x++ {
			packet = s.pixel(packet, rgbAt(c.Pixels, int(c.Width), x, y))
		}
	}
	packet = append(packet, c.Mask...)
	if err := writeAll(s.w, packet); err != nil {
		s.ready = false
		return err
	}
	return nil
}
