package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"virelai/webrender"
)

func TestPinnedQuadrants(t *testing.T) {
	data, err := os.ReadFile("testdata/quadrants.qoi")
	if err != nil {
		t.Fatal(err)
	}
	const fixtureSHA = "526e75a76e0315686ba9ae02252f49627cc24db6ea2ba2728957687cfa38cfed"
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != fixtureSHA {
		t.Fatalf("fixture SHA-256 = %s, want %s", got, fixtureSHA)
	}
	im, err := webrender.DecodeImage(data)
	if err != nil {
		t.Fatal(err)
	}
	if im.Width != 16 || im.Height != 32 {
		t.Fatalf("fixture is %dx%d, want 16x32", im.Width, im.Height)
	}
	for _, tc := range []struct {
		x, y int
		rgb  uint32
	}{
		{0, 0, 0xffff0000}, {15, 0, 0xff00ff00},
		{0, 31, 0xff0000ff}, {15, 31, 0xffffff00},
	} {
		if got := im.At(tc.x, tc.y); got != tc.rgb {
			t.Fatalf("pixel (%d,%d) = %08x, want %08x", tc.x, tc.y, got, tc.rgb)
		}
	}
	got, err := sixel(im)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(got, []byte("\x1bPq\"1;1;16;32")) ||
		!bytes.HasSuffix(got, []byte("\x1b\\")) ||
		!bytes.Contains(got, []byte("#0;2;100;0;0")) ||
		!bytes.Contains(got, []byte("#3;2;100;100;0")) {
		t.Fatalf("unexpected sixel envelope/palette: %q", got)
	}
}

func TestSixelRefusesBadImagesBeforeDCS(t *testing.T) {
	for _, im := range []*webrender.Image{
		nil, {Width: 97, Height: 1, Pix: make([]uint32, 97)},
		{Width: 1, Height: 65, Pix: make([]uint32, 65)},
		{Width: 2, Height: 2, Pix: make([]uint32, 3)},
	} {
		if out, err := sixel(im); err == nil || out != nil {
			t.Fatalf("invalid image emitted %q: %v", out, err)
		}
	}
	// 257 distinct RGB-percent colours cannot fit the kernel's 256 slots.
	pix := make([]uint32, 257)
	for i := range pix {
		pix[i] = 0xff000000 | uint32(i/101*3)<<16 | uint32(i%101*255/100)<<8
	}
	im := &webrender.Image{Width: 86, Height: 3, Pix: make([]uint32, 258)}
	copy(im.Pix, pix)
	if out, err := sixel(im); err != errPalette || out != nil {
		t.Fatalf("palette overflow emitted %q: %v", out, err)
	}
}

func TestSixelTransparentAndBands(t *testing.T) {
	im := &webrender.Image{Width: 2, Height: 7, Pix: []uint32{
		0xffff0000, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff0000ff, 0,
	}}
	got, err := sixel(im)
	if err != nil {
		t.Fatal(err)
	}
	// The colour loop is per band, so the first blue register is defined
	// even when that band contains no blue pixels.
	if !bytes.HasPrefix(got, []byte("\x1bPq\"1;1;2;7")) ||
		!bytes.Contains(got, []byte("-#0;2;100;0;0??$#1;2;0;0;100@?")) {
		t.Fatalf("band/transparent encoding: %q", got)
	}
}
