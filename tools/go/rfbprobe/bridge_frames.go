package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

// The bridge is the guest's RFB viewer. It can advertise hextile even when
// its authenticated downstream viewer only accepts Raw, then decode the
// guest's compressed update on the host. Only one 16-row band (at most
// 1280x16x4 pixels) is buffered; no image encoder or DES enters the guest.
type bridgeFramePlan struct {
	mu        sync.Mutex
	bpp       int
	translate bool
	started   bool
	ready     chan bool
}

func newBridgeFramePlan(bpp int) *bridgeFramePlan {
	return &bridgeFramePlan{bpp: bpp, ready: make(chan bool, 1)}
}

func (p *bridgeFramePlan) setBPP(bpp int) {
	p.mu.Lock()
	p.bpp = bpp
	p.mu.Unlock()
}

func (p *bridgeFramePlan) pixelBPP() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bpp
}

func (p *bridgeFramePlan) wantTranslation(noViewerCompression bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started {
		p.translate = noViewerCompression
	}
	return p.translate
}

// Do not start reading a server update until the viewer's SetEncodings and
// first request have been forwarded. EOF also releases a waiting pump.
func (p *bridgeFramePlan) start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started {
		p.started = true
		p.ready <- p.translate
		close(p.ready)
	}
}

func (p *bridgeFramePlan) mode() bool { return <-p.ready }

func forwardTranscodedFrames(guest io.Reader, viewer io.Writer, plan *bridgeFramePlan) (int64, error) {
	var forwarded int64
	for {
		var header [4]byte
		if _, err := io.ReadFull(guest, header[:]); err != nil {
			return forwarded, err
		}
		if header[0] != 0 || header[1] != 0 {
			return forwarded, fmt.Errorf("unsupported guest server message %d", header[0])
		}
		count := int(binary.BigEndian.Uint16(header[2:]))
		if count > 80*45 {
			return forwarded, fmt.Errorf("guest rectangle count %d exceeds frame bound", count)
		}
		if err := write(viewer, header[:]); err != nil {
			return forwarded, err
		}
		forwarded += 4
		for range count {
			var rect [12]byte
			if _, err := io.ReadFull(guest, rect[:]); err != nil {
				return forwarded, err
			}
			x, y := int(binary.BigEndian.Uint16(rect[0:])), int(binary.BigEndian.Uint16(rect[2:]))
			w, h := int(binary.BigEndian.Uint16(rect[4:])), int(binary.BigEndian.Uint16(rect[6:]))
			if w == 0 || h == 0 || x+w > frameW || y+h > frameH {
				return forwarded, fmt.Errorf("guest rectangle out of bounds")
			}
			bpp := plan.pixelBPP()
			if bpp != 1 && bpp != 2 && bpp != 4 {
				return forwarded, fmt.Errorf("guest pixel format refused")
			}
			switch int32(binary.BigEndian.Uint32(rect[8:])) {
			case 5: // guest hextile, downstream Raw
				binary.BigEndian.PutUint32(rect[8:], 0)
				if err := write(viewer, rect[:]); err != nil {
					return forwarded, err
				}
				forwarded += 12
				// Hextile tiles arrive in 16-row bands. Forward each
				// decoded Raw band immediately, before the next slow
				// guest TCP segment, so the native viewer sees progress.
				raw := make([]byte, w*min(16, h)*bpp)
				decoder := hextileDecoder{}
				for row := 0; row < h; row += 16 {
					band := raw[:w*min(16, h-row)*bpp]
					if err := decoder.decodeRows(guest, band, w, min(16, h-row), bpp); err != nil {
						return forwarded, err
					}
					if err := write(viewer, band); err != nil {
						return forwarded, err
					}
					forwarded += int64(len(band))
				}
			case -223: // DesktopSize, only when the guest accepted the viewer's advertisement
				if err := write(viewer, rect[:]); err != nil {
					return forwarded, err
				}
				forwarded += 12
			case -239: // RichCursor (pixels followed by one bit per mask pixel)
				if w > 64 || h > 64 {
					return forwarded, fmt.Errorf("guest cursor exceeds bound")
				}
				body := make([]byte, w*h*bpp+((w+7)/8)*h)
				if _, err := io.ReadFull(guest, body); err != nil {
					return forwarded, err
				}
				if err := write(viewer, rect[:]); err != nil {
					return forwarded, err
				}
				if err := write(viewer, body); err != nil {
					return forwarded + 12, err
				}
				forwarded += int64(12 + len(body))
			default:
				return forwarded, fmt.Errorf("unsupported guest encoding %d", int32(binary.BigEndian.Uint32(rect[8:])))
			}
		}
	}
}

// Background and foreground persist across tile rows by the RFB protocol.
// The current guest explicitly supplies the background for every tile.
type hextileDecoder struct {
	bg, fg         [4]byte
	haveBG, haveFG bool
}

// decodeRows consumes one 16-row band (or the rectangle's final short
// band) and emits only its Raw pixels. Hextile subrectangles are tile-local.
func (d *hextileDecoder) decodeRows(src io.Reader, dst []byte, width, height, bpp int) error {
	var scratch [16 * 16 * 4]byte
	for ty := 0; ty < height; ty += 16 {
		for tx := 0; tx < width; tx += 16 {
			tw, th := min(16, width-tx), min(16, height-ty)
			var flag [1]byte
			if _, err := io.ReadFull(src, flag[:]); err != nil {
				return err
			}
			if flag[0]&^byte(0x1f) != 0 {
				return fmt.Errorf("unsupported hextile flags")
			}
			if flag[0]&1 != 0 {
				if flag[0] != 1 {
					return fmt.Errorf("invalid raw hextile flags")
				}
				if _, err := io.ReadFull(src, scratch[:tw*th*bpp]); err != nil {
					return err
				}
				for row := 0; row < th; row++ {
					copy(dst[((ty+row)*width+tx)*bpp:], scratch[row*tw*bpp:(row+1)*tw*bpp])
				}
				continue
			}
			if flag[0]&2 != 0 {
				if _, err := io.ReadFull(src, d.bg[:bpp]); err != nil {
					return err
				}
				d.haveBG = true
			}
			if !d.haveBG {
				return fmt.Errorf("hextile background missing")
			}
			if flag[0]&4 != 0 {
				if _, err := io.ReadFull(src, d.fg[:bpp]); err != nil {
					return err
				}
				d.haveFG = true
			}
			for row := 0; row < th; row++ {
				for col := 0; col < tw; col++ {
					copy(dst[((ty+row)*width+tx+col)*bpp:], d.bg[:bpp])
				}
			}
			if flag[0]&8 == 0 {
				if flag[0]&16 != 0 {
					return fmt.Errorf("hextile colored flag without subrects")
				}
				continue
			}
			var num [1]byte
			if _, err := io.ReadFull(src, num[:]); err != nil {
				return err
			}
			if flag[0]&16 == 0 && !d.haveFG {
				return fmt.Errorf("hextile foreground missing")
			}
			for range int(num[0]) {
				if flag[0]&16 != 0 {
					if _, err := io.ReadFull(src, d.fg[:bpp]); err != nil {
						return err
					}
				}
				var shape [2]byte
				if _, err := io.ReadFull(src, shape[:]); err != nil {
					return err
				}
				sx, sy := int(shape[0]>>4), int(shape[0]&15)
				sw, sh := int(shape[1]>>4)+1, int(shape[1]&15)+1
				if sx+sw > tw || sy+sh > th {
					return fmt.Errorf("hextile subrectangle out of bounds")
				}
				for row := sy; row < sy+sh; row++ {
					for col := sx; col < sx+sw; col++ {
						copy(dst[((ty+row)*width+tx+col)*bpp:], d.fg[:bpp])
					}
				}
			}
		}
	}
	return nil
}

// Kept as a small standalone decoder seam for malformed-tile tests.
func decodeHextile(src io.Reader, dst []byte, width, height, bpp int) error {
	if width <= 0 || height <= 0 || len(dst) != width*height*bpp {
		return fmt.Errorf("invalid hextile image bound")
	}
	d := hextileDecoder{}
	for row := 0; row < height; row += 16 {
		strip := dst[row*width*bpp : (row+min(16, height-row))*width*bpp]
		if err := d.decodeRows(src, strip, width, min(16, height-row), bpp); err != nil {
			return err
		}
	}
	return nil
}
