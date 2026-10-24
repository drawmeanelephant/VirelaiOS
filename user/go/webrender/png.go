package webrender

import (
	"virelai/flate"
	"virelai/webstyle"
)

const maxPNGScanlines = 1_049_600
const maxPNGChunks = 256

type imageError string

func (e imageError) Error() string { return string(e) }

const (
	errPNGInvalid = imageError("image-invalid: png")
	errPNGLimit   = imageError("image-limit: png")
)

// DecodePNG implements ADR 0028 Amendment D: 8-bit PNG types 0/2/3/4/6,
// interlace methods 0 (none) and 1 (Adam7), straight RGBA8, no
// profile/gamma transformation. All allocation sizes are validated before
// allocating. The host and guest use this same decoder; image/png is only
// a host test reference.
func DecodePNG(data []byte) (*Image, error) {
	if len(data) > webstyle.MaxImageBytes {
		return nil, errPNGLimit
	}
	if len(data) < 8 || string(data[:8]) != string(pngMagic) {
		return nil, errPNGInvalid
	}
	var w, h, channels int
	var typ byte
	var interlaced bool
	var palette []byte
	var transparency []byte
	var compressed []byte
	header, seenPalette, seenTRNS, seenIDAT, endedIDAT := false, false, false, false, false
	pos, chunks := 8, 0
	for pos < len(data) {
		chunks++
		if chunks > maxPNGChunks {
			return nil, errPNGLimit
		}
		if len(data)-pos < 12 {
			return nil, errPNGInvalid
		}
		n := uint64(be32b(data[pos : pos+4]))
		if n > uint64(len(data)-pos-12) {
			return nil, errPNGInvalid
		}
		end := pos + 8 + int(n)
		name := string(data[pos+4 : pos+8])
		for _, c := range data[pos+4 : pos+8] {
			if c < 'A' || c > 'Z' && c < 'a' || c > 'z' {
				return nil, errPNGInvalid
			}
		}
		if data[pos+6]&0x20 != 0 || pngCRC(data[pos+4:end]) != be32b(data[end:end+4]) {
			return nil, errPNGInvalid
		}
		body := data[pos+8 : end]
		pos = end + 4
		if !header && name != "IHDR" {
			return nil, errPNGInvalid
		}
		if seenIDAT && name != "IDAT" {
			endedIDAT = true
		}
		switch name {
		case "IHDR":
			if header || len(body) != 13 {
				return nil, errPNGInvalid
			}
			header = true
			width, height := uint64(be32b(body[:4])), uint64(be32b(body[4:8]))
			if width == 0 || height == 0 {
				return nil, errPNGInvalid
			}
			if width > webstyle.MaxImageDimension || height > webstyle.MaxImageDimension ||
				width*height > webstyle.MaxImagePixels || width*height*4 > webstyle.MaxDecodedImageBytes {
				return nil, errPNGLimit
			}
			w, h, typ = int(width), int(height), body[9]
			switch typ {
			case 0, 3:
				channels = 1
			case 2:
				channels = 3
			case 4:
				channels = 2
			case 6:
				channels = 4
			default:
				return nil, errPNGInvalid
			}
			if body[10] != 0 || body[11] != 0 || body[12] > 1 {
				return nil, errPNGInvalid
			}
			if body[8] != 8 {
				return nil, ErrImageUnsupported
			}
			interlaced = body[12] == 1
		case "PLTE":
			if seenPalette || seenTRNS || seenIDAT || typ == 0 || typ == 4 ||
				len(body) == 0 || len(body)%3 != 0 || len(body) > 768 {
				return nil, errPNGInvalid
			}
			seenPalette, palette = true, body
		case "tRNS":
			if seenTRNS || seenIDAT {
				return nil, errPNGInvalid
			}
			switch typ {
			case 0:
				if len(body) != 2 || body[0] != 0 {
					return nil, errPNGInvalid
				}
			case 2:
				if len(body) != 6 || body[0] != 0 || body[2] != 0 || body[4] != 0 {
					return nil, errPNGInvalid
				}
			case 3:
				if !seenPalette || len(body) == 0 || len(body) > len(palette)/3 {
					return nil, errPNGInvalid
				}
			default:
				return nil, errPNGInvalid
			}
			seenTRNS, transparency = true, body
		case "IDAT":
			if endedIDAT || typ == 3 && !seenPalette {
				return nil, errPNGInvalid
			}
			seenIDAT = true
			compressed = append(compressed, body...)
		case "IEND":
			if len(body) != 0 || !seenIDAT || pos != len(data) {
				return nil, errPNGInvalid
			}
			return pngPixels(w, h, typ, channels, palette, transparency, compressed, interlaced)
		case "acTL", "fcTL", "fdAT":
			return nil, ErrImageUnsupported
		default:
			if name[0]&0x20 == 0 {
				return nil, ErrImageUnsupported
			}
		}
	}
	return nil, errPNGInvalid
}

// adam7 is the pass grid of PNG interlace method 1: xstart, ystart, xstep,
// ystep for each of the seven passes, in stream order.
var adam7 = [7][4]int{
	{0, 0, 8, 8},
	{4, 0, 8, 8},
	{0, 4, 4, 8},
	{2, 0, 4, 4},
	{0, 2, 2, 4},
	{1, 0, 2, 2},
	{0, 1, 1, 2},
}

// adam7Size returns a pass's pixel dimensions for a w×h image: (0,0) when
// the pass's grid misses the image entirely (small or narrow inputs), in
// which case the pass contributes no scanlines to the stream.
func adam7Size(w, h int, pass [4]int) (pw, ph int) {
	if w > pass[0] {
		pw = (w - pass[0] + pass[2] - 1) / pass[2]
	}
	if h > pass[1] {
		ph = (h - pass[1] + pass[3] - 1) / pass[3]
	}
	return pw, ph
}

func pngPixels(w, h int, typ byte, channels int, palette, transparency, compressed []byte, interlaced bool) (*Image, error) {
	// The decompressed stream is exact — one filter byte per scanline then
	// the pass's pixel row — so the same scanline budget bounds both
	// layouts; interlacing only adds filter bytes (≤7/4 of h more).
	n := (w*channels + 1) * h
	if interlaced {
		n = 0
		for _, pass := range adam7 {
			pw, ph := adam7Size(w, h, pass)
			if pw > 0 && ph > 0 {
				n += ph * (pw*channels + 1)
			}
		}
	}
	if n > maxPNGScanlines {
		return nil, errPNGLimit
	}
	rows := make([]byte, n)
	written, consumed, err := flate.InflateZlib(rows, compressed)
	if err != nil || written != n || consumed != len(compressed) {
		return nil, errPNGInvalid
	}
	out := &Image{Width: w, Height: h, Pix: make([]uint32, w*h), png: true}
	pos := 0
	// pass decodes ph scanlines of pw pixels each from rows[pos:] and scatters
	// them into out at (x0+x*dx, y0+y*dy). Filtering restarts per pass: prev
	// is the prior row of THIS pass only.
	pass := func(pw, ph, x0, y0, dx, dy int) error {
		var prev []byte
		for y := 0; y < ph; y++ {
			row, err := pngScanline(rows[pos:], pw*channels, channels, prev)
			if err != nil {
				return err
			}
			pos += pw*channels + 1
			for x := 0; x < pw; x++ {
				pix, err := pngPixel(row[x*channels:(x+1)*channels], typ, palette, transparency)
				if err != nil {
					return err
				}
				out.Pix[(y0+y*dy)*w+x0+x*dx] = pix
			}
			prev = row
		}
		return nil
	}
	if interlaced {
		for _, p := range adam7 {
			pw, ph := adam7Size(w, h, p)
			if pw == 0 || ph == 0 {
				continue
			}
			if err := pass(pw, ph, p[0], p[1], p[2], p[3]); err != nil {
				return nil, err
			}
		}
	} else if err := pass(w, h, 0, 0, 1, 1); err != nil {
		return nil, err
	}
	return out, nil
}

// pngScanline unfilters one row in place and returns it: raw[0] is the
// filter byte, raw[1:1+rowLen] the filtered bytes. bpp is the pixel stride
// (channels at 8-bit depth); prev is the previous unfiltered row of the same
// pass, nil on the first.
func pngScanline(raw []byte, rowLen, bpp int, prev []byte) ([]byte, error) {
	filter := raw[0]
	if filter > 4 {
		return nil, errPNGInvalid
	}
	row := raw[1 : 1+rowLen]
	for i := range row {
		var a, b, c byte
		if i >= bpp {
			a = row[i-bpp]
		}
		if prev != nil {
			b = prev[i]
			if i >= bpp {
				c = prev[i-bpp]
			}
		}
		switch filter {
		case 1:
			row[i] += a
		case 2:
			row[i] += b
		case 3:
			row[i] += byte((int(a) + int(b)) / 2)
		case 4:
			row[i] += pngPaeth(a, b, c)
		}
	}
	return row, nil
}

// pngPixel converts one decoded pixel to the straight RGBA8 word the tree
// expects, applying the palette and tRNS of types 0/2/3.
func pngPixel(p []byte, typ byte, palette, transparency []byte) (uint32, error) {
	r, g, b, a := p[0], p[0], p[0], byte(255)
	switch typ {
	case 0:
		if len(transparency) != 0 && r == transparency[1] {
			a = 0
		}
	case 2:
		g, b = p[1], p[2]
		if len(transparency) != 0 && r == transparency[1] && g == transparency[3] && b == transparency[5] {
			a = 0
		}
	case 3:
		index := int(p[0])
		if index >= len(palette)/3 {
			return 0, errPNGInvalid
		}
		r, g, b = palette[index*3], palette[index*3+1], palette[index*3+2]
		if index < len(transparency) {
			a = transparency[index]
		}
	case 4:
		a = p[1]
	case 6:
		g, b, a = p[1], p[2], p[3]
	}
	return uint32(a)<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b), nil
}

func pngPaeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa, pb, pc := absPNG(p-int(a)), absPNG(p-int(b)), absPNG(p-int(c))
	if pa <= pb && pa <= pc {
		return a
	}
	if pb <= pc {
		return b
	}
	return c
}

func absPNG(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func pngCRC(data []byte) uint32 {
	crc := uint32(0xffffffff)
	for _, b := range data {
		crc ^= uint32(b)
		for i := 0; i < 8; i++ {
			mask := uint32(0) - (crc & 1)
			crc = crc>>1 ^ 0xedb88320&mask
		}
	}
	return ^crc
}
