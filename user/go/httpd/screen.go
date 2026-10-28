// screen.go — M209b (issue #3826, index #3824): /admin/screen.png — a PNG
// snapshot of the current framebuffer, behind M209a's TLS + credential
// checks (routeAdmin reaches it only after all three pass).
//
// Every request reads the frame fresh through slot 88 (vi.ScanoutRead —
// the bounded read-only copy agentd's ui.screenshot rides), in band rects
// sized to stay inside ScanoutReadMax, expands B8G8R8X8 to the 0xAARRGGBB
// word layout, and encodes once with webrender.EncodePNG. No pixels are
// cached between requests: a live frame is the point.
//
// The errno map: an unarmed framebuffer (ENXIO — a boot with no virtio-gpu
// scanout) is 503 with the errno named; any other read refusal is a named
// 500. Both statuses register into server.go's statusText from here —
// server.go is outside this card's Touches.
package main

import (
	"virelai/vi"
	"virelai/webrender"
)

func init() {
	statusText[500] = "Internal Server Error"
	statusText[503] = "Service Unavailable"
}

// screenRead is the slot-88 seam; host tests stub it (the host build's
// ScanoutRead answers -ENOSYS).
var screenRead = vi.ScanoutRead

// screenBandRows is the largest band height one ScanoutRead call serves
// inside ScanoutReadMax: 12 rows of 1280*4 = 61,440 bytes; 13 would not
// fit. agentd's fullShotRows makes the same arithmetic, and 720 divides
// it evenly.
const screenBandRows = vi.ScanoutReadMax / (vi.ScanoutWidth * vi.ScanoutBPP)

// screenRect is one ScanoutRead call's rectangle.
type screenRect struct{ x, y, w, h uint32 }

// planScreenChunks returns the band list covering the full frame: every
// rect is full-width and screenBandRows tall except possibly the last.
func planScreenChunks() []screenRect {
	var out []screenRect
	for y := uint32(0); y < vi.ScanoutHeight; y += screenBandRows {
		h := uint32(screenBandRows)
		if rem := vi.ScanoutHeight - y; rem < h {
			h = rem
		}
		out = append(out, screenRect{0, y, vi.ScanoutWidth, h})
	}
	return out
}

// captureScreen reads the full frame once: each band's B8G8R8X8 bytes
// expand into the matching rows of a 0xAARRGGBB word slice (the layout
// webrender.EncodePNG and every surface share). A refused read stops the
// capture — the errno is the response's name.
func captureScreen() ([]uint32, int64) {
	pix := make([]uint32, vi.ScanoutWidth*vi.ScanoutHeight)
	band := make([]byte, screenBandRows*vi.ScanoutWidth*vi.ScanoutBPP)
	for _, r := range planScreenChunks() {
		need := int(r.w*r.h) * vi.ScanoutBPP
		rc := screenRead(r.x, r.y, r.w, r.h, band[:need])
		if rc < 0 {
			return nil, rc
		}
		// The slot's contract is exactly `need` bytes or a negative
		// errno — a short positive count is EIO, never partial pixels.
		if rc != int64(need) {
			return nil, -vi.ErrEIO
		}
		dst := pix[r.y*vi.ScanoutWidth : (r.y+r.h)*vi.ScanoutWidth]
		for i := 0; i < int(r.w*r.h); i++ {
			o := i * vi.ScanoutBPP
			dst[i] = 0xff000000 |
				uint32(band[o+2])<<16 | uint32(band[o+1])<<8 | uint32(band[o])
		}
	}
	return pix, 0
}

// adminScreenPNG answers GET /admin/screen.png: one fresh capture, one
// encode, one image/png response.
func adminScreenPNG() response {
	pix, rc := captureScreen()
	if rc < 0 {
		return screenErr(rc)
	}
	png := webrender.EncodePNG(pix, vi.ScanoutWidth, vi.ScanoutHeight)
	if png == nil {
		return response{Status: 500, ContentType: "text/plain; charset=utf-8",
			Body: []byte("screen encode failed\n")}
	}
	return response{Status: 200, ContentType: "image/png", Body: png}
}

// screenErr names the read's refusal: ENXIO (no armed framebuffer) is 503;
// every other errno is a named 500 — "unknown is a value" applies to the
// wire, not just the tables.
func screenErr(rc int64) response {
	name := vi.ErrnoName(rc)
	if name == "" {
		name = "refused"
	}
	if rc == -vi.ErrENXIO {
		return response{Status: 503, ContentType: "text/plain; charset=utf-8",
			Body: []byte("screen unavailable (" + name + ")\n")}
	}
	return response{Status: 500, ContentType: "text/plain; charset=utf-8",
		Body: []byte("screen read failed (" + name + ")\n")}
}
