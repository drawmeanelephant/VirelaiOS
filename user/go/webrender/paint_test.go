package webrender

import "testing"

func scaledRampPixels(w, h int) []uint32 {
	pix := make([]uint32, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pix[y*w+x] = 0xff000000 | uint32(x*40)<<16 | uint32(y*40)<<8 | 7
		}
	}
	return pix
}

func TestDrawImageAreaFiltersNonIntegerDownscale(t *testing.T) {
	img := &Image{Width: 5, Height: 5, Pix: scaledRampPixels(5, 5)}
	f := newFB(2, 2, ColorPageBg)
	drawImage(f, Clip{W: 2, H: 2}, 0, 0, Item{W: 2, H: 2, Img: img})
	want := [2][2]uint32{
		{0x00202007, 0x00802007},
		{0x00208007, 0x00808007},
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			if got := f.at(x, y); got != want[y][x] {
				t.Fatalf("painted pixel (%d,%d) = %08x, want exact footprint mean %08x", x, y, got, want[y][x])
			}
		}
	}
}

func TestDrawImagePremultipliedAlphaEdgeStaysBright(t *testing.T) {
	img := &Image{Width: 2, Height: 2, Pix: []uint32{
		0xffffffff, 0x00000000,
		0xffffffff, 0x00000000,
	}}
	f := newFB(1, 1, 0)
	drawImage(&maskFB{f}, Clip{W: 1, H: 1}, 0, 0, Item{W: 1, H: 1, Img: img})
	got := f.at(0, 0)
	for shift := uint(0); shift <= 16; shift += 8 {
		if channel := got >> shift & 0xff; channel < 0xa0 {
			t.Fatalf("transparent black darkened the composited edge channel shift %d to %02x in %08x", shift, channel, got)
		}
	}
}

func TestDrawImagePNGAlphaUsesMaskWhenAvailable(t *testing.T) {
	img := &Image{Width: 2, Height: 2, png: true, Pix: []uint32{
		0xffffffff, 0x00000000,
		0xffffffff, 0x00000000,
	}}
	f := newFB(1, 1, 0)
	drawImage(&maskFB{f}, Clip{W: 1, H: 1}, 0, 0, Item{W: 1, H: 1, Img: img})
	got := f.at(0, 0)
	for shift := uint(0); shift <= 16; shift += 8 {
		if channel := got >> shift & 0xff; channel < 0xa0 {
			t.Fatalf("legacy PNG compatibility darkened alpha-capable edge channel shift %d to %02x in %08x", shift, channel, got)
		}
	}
}
