package main

import (
	"os"
	"testing"

	"virelai/webrender"
)

// Retargeting cannot retain a physical-height threshold after 0.4x sampling.
// Report the observed host pixels before asking for a gate-policy ruling.
func TestPresentationTypographyProbeMeasurements(t *testing.T) {
	body, err := os.ReadFile("../../../tests/oliver-spike/expect.html")
	if err != nil {
		t.Fatal(err)
	}
	a := &app{hist: newHistory(), text: browserFonts(t), target: "/host/OLIVER.HTML"}
	a.loadBody(body, a.target)
	native := a.nativeSurface()
	native.Fill(0, 0, contentW, contentH, webrender.ColorPageBg)
	webrender.Paint(a.lay, native, 0, 0, contentW, contentH, 0)
	pix := make([]uint32, 512*288)
	a.sampleFrame(virender{pix: pix, w: 512, h: 288}, presentation{0, 0, 512, 288})
	first, last, minX, maxX := -1, -1, 512, -1
	for y := 0; y < 288; y++ {
		ink := false
		for x := 0; x < 512; x++ {
			p := pix[y*512+x]
			diff := false
			for _, shift := range []uint{0, 8, 16} {
				c, bg := int(p>>shift&255), int(webrender.ColorPageBg>>shift&255)
				if c-bg > 12 || bg-c > 12 {
					diff = true
				}
			}
			if diff {
				ink = true
				if first >= 0 || last < 0 {
					minX, maxX = min(minX, x), max(maxX, x)
				}
			}
		}
		if ink && first < 0 {
			first = y
		}
		if !ink && first >= 0 {
			last = y
			break
		}
	}
	t.Logf("approved sampler 512x288: first band=[%d,%d), height=%d, span=%d; existing gate requires height>=17, span>=200",
		first, last, last-first, maxX-minX+1)
	if first < 0 || last <= first {
		t.Fatal("no first text band")
	}
}
