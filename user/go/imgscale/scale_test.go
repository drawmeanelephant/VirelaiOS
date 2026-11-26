package imgscale

import "testing"

func rampPixels(w, h int) []uint32 {
	pix := make([]uint32, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			pix[y*w+x] = 0xff000000 | uint32(x*40)<<16 | uint32(y*40)<<8 | 7
		}
	}
	return pix
}

func TestAreaFilterNonIntegerDownscaleMeans(t *testing.T) {
	s := New(5, 5, 2, 2)
	if !s.OK() || !s.Area() {
		t.Fatalf("5x5 -> 2x2 did not select the area filter")
	}
	pix := rampPixels(5, 5)
	want := [2][2]uint32{
		{0xff202007, 0xff802007},
		{0xff208007, 0xff808007},
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			if got := s.Pixel(pix, x, y); got != want[y][x] {
				t.Fatalf("area pixel (%d,%d) = %08x, want exact footprint mean %08x", x, y, got, want[y][x])
			}
		}
	}
}

func TestPremultipliedAlphaEdgesDoNotDarken(t *testing.T) {
	cases := []struct {
		name           string
		sw, sh, dw, dh int
		area           bool
	}{
		{"area", 2, 2, 1, 1, true},
		{"mixed-axis-bilinear", 2, 1, 1, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pix := make([]uint32, tc.sw*tc.sh)
			for y := 0; y < tc.sh; y++ {
				pix[y*tc.sw] = 0xffffffff
			}
			s := New(tc.sw, tc.sh, tc.dw, tc.dh)
			if s.Area() != tc.area {
				t.Fatalf("area selection = %v, want %v", s.Area(), tc.area)
			}
			got := s.Pixel(pix, 0, 0)
			if got>>24 < 127 || got>>24 > 129 {
				t.Fatalf("edge alpha = %02x in %08x, want about half", got>>24, got)
			}
			for shift := uint(0); shift <= 16; shift += 8 {
				if channel := got >> shift & 0xff; channel < 0xfe {
					t.Fatalf("transparent black darkened channel shift %d to %02x in %08x", shift, channel, got)
				}
			}
		})
	}
}

func TestOpaquePixelsIgnoreInertAlphaBytes(t *testing.T) {
	pix := []uint32{0x00102030, 0x00102030, 0x00102030, 0x00102030}
	s := New(2, 2, 1, 1)
	if got := s.PixelOpaque(pix, 0, 0); got != 0xff102030 {
		t.Fatalf("opaque RGB footprint = %08x, want 0xff102030", got)
	}
}
