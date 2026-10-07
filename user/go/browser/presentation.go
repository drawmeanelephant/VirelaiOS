package main

import (
	"virelai/ttf"
	"virelai/vi"
	"virelai/webrender"
	"virelai/webstyle"
)

type presentation struct{ X, Y, W, H int }

func documentPresentation(w, h int) presentation {
	aw, ah := w, h-kernelBand-32-16
	if aw <= 0 || ah <= 0 {
		return presentation{}
	}
	pw := min(webstyle.ViewportWidth, min(aw, ah*webstyle.ViewportWidth/webstyle.ViewportHeight))
	ph := pw * webstyle.ViewportHeight / webstyle.ViewportWidth
	if pw <= 0 || ph <= 0 {
		return presentation{}
	}
	return presentation{(aw - pw) / 2, kernelBand + 32 + (ah-ph)/2, pw, ph}
}

func (p presentation) inverse(x, y, scroll int) (int, int, bool) {
	if p.W <= 0 || p.H <= 0 || !inRect(x, y, p.X, p.Y, p.W, p.H) {
		return 0, 0, false
	}
	return (2*(x-p.X) + 1) * webstyle.ViewportWidth / (2 * p.W),
		(2*(y-p.Y)+1)*webstyle.ViewportHeight/(2*p.H) + scroll, true
}

func (a *app) canvasSize() (int, int) {
	if !a.canvasConfigured {
		return winW, winH
	}
	w, h := a.canvasW, a.canvasH
	return w, h
}

func (a *app) bindCanvas(w, h int) {
	a.canvasConfigured = true
	a.canvasW, a.canvasH = w, h
	if w <= 0 || h <= 0 || w > vi.ScanoutWidth || h > vi.ScanoutHeight {
		a.canvasW, a.canvasH = 0, 0
		a.status = "viewport-too-small"
		return
	}
	if documentPresentation(w, h).W == 0 {
		a.status = "viewport-too-small"
	} else if a.status == "viewport-too-small" {
		a.status = ""
	}
	// Native AA is blended once in the bounded CSS frame. Publish sampled
	// RGB spans through the existing window fill batch, which follows resize
	// without leaking/rebinding a one-surface-per-window shared mapping.
}

func (a *app) nativeSurface() virender {
	if a.nativePix == nil {
		a.nativePix = make([]uint32, webstyle.ViewportWidth*webstyle.ViewportHeight)
	}
	return virender{pix: a.nativePix, w: webstyle.ViewportWidth, h: webstyle.ViewportHeight}
}

func (a *app) sampleFrame(dst virender, p presentation) {
	if p.W <= 0 || p.H <= 0 {
		return
	}
	for y := 0; y < p.H; y++ {
		sy := (2*y + 1) * webstyle.ViewportHeight / (2 * p.H)
		for x := 0; x < p.W; {
			sx := (2*x + 1) * webstyle.ViewportWidth / (2 * p.W)
			color := a.nativePix[sy*webstyle.ViewportWidth+sx] & 0xffffff
			end := x + 1
			for end < p.W {
				sx = (2*end + 1) * webstyle.ViewportWidth / (2 * p.W)
				if a.nativePix[sy*webstyle.ViewportWidth+sx]&0xffffff != color {
					break
				}
				end++
			}
			dst.Fill(p.X+x, p.Y+y, end-x, 1, color)
			x = end
		}
	}
}

func (v virender) BlitMask(x, y int, mask *ttf.Mask, rgb uint32) {
	if v.pix != nil {
		ttf.Blend(v.pix, v.w, v.w, v.h, x, y, mask, rgb)
		return
	}
	webrender.BlitMaskSpans(fillOnly{v}, webrender.Clip{W: v.w, H: v.h}, x, y, mask, rgb)
}

type fillOnly struct{ v virender }

func (s fillOnly) Fill(x, y, w, h int, rgb uint32) { s.v.Fill(x, y, w, h, rgb) }
