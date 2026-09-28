package main

import (
	"bytes"
	"errors"

	"virelai/webrender"
)

// The terminal's M85b image bank refuses anything beyond 96x64 pixels.
const maxWidth, maxHeight = 96, 64

var errImageBounds = errors.New("imgcat: image exceeds 96x64")
var errPalette = errors.New("imgcat: image exceeds 256 sixel colours")

// sixel encodes one decoded image with the M85a primary protocol. Registers
// are defined in RGB percentages, six rows per band; transparent pixels are
// left unset. The raster declaration fixes the extent even at transparent
// edges. Reject before emitting a DCS so a bad image cannot leave a partial
// graphic on the bound terminal.
func sixel(im *webrender.Image) ([]byte, error) {
	if im == nil || im.Width < 1 || im.Height < 1 ||
		im.Width > maxWidth || im.Height > maxHeight ||
		len(im.Pix) != im.Width*im.Height {
		return nil, errImageBounds
	}
	var colours []uint32
	indices := make([]int16, len(im.Pix))
	seen := make(map[uint32]int16)
	for i, px := range im.Pix {
		if px>>24 < 128 {
			indices[i] = -1
			continue
		}
		// Sixel's RGB controls are percentages, not 8-bit channels.
		rgb := quantize(px)
		id, ok := seen[rgb]
		if !ok {
			if len(colours) == 256 {
				return nil, errPalette
			}
			id = int16(len(colours))
			colours = append(colours, rgb)
			seen[rgb] = id
		}
		indices[i] = id
	}
	var out bytes.Buffer
	out.WriteString("\x1bPq\"1;1;")
	number(&out, im.Width)
	out.WriteByte(';')
	number(&out, im.Height)
	for y := 0; y < im.Height; y += 6 {
		if y > 0 {
			out.WriteByte('-')
		}
		for id, rgb := range colours {
			if id > 0 {
				out.WriteByte('$')
			}
			out.WriteByte('#')
			number(&out, id)
			out.WriteString(";2;")
			number(&out, int(rgb>>16&0xff))
			out.WriteByte(';')
			number(&out, int(rgb>>8&0xff))
			out.WriteByte(';')
			number(&out, int(rgb&0xff))
			for x := 0; x < im.Width; x++ {
				var bits byte
				for bit := 0; bit < 6 && y+bit < im.Height; bit++ {
					if indices[(y+bit)*im.Width+x] == int16(id) {
						bits |= 1 << bit
					}
				}
				out.WriteByte('?' + bits)
			}
		}
	}
	out.WriteString("\x1b\\")
	return out.Bytes(), nil
}

func quantize(px uint32) uint32 {
	channel := func(v uint32) uint32 { return (v*100 + 127) / 255 }
	return channel(px>>16&0xff)<<16 | channel(px>>8&0xff)<<8 | channel(px&0xff)
}

func number(b *bytes.Buffer, n int) {
	var digits [10]byte
	i := len(digits)
	for {
		i--
		digits[i] = '0' + byte(n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	b.Write(digits[i:])
}
