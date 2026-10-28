package main

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	"virelai/vi"
)

// screenSetup arms the admin section (TLS + the staged row, like an
// authed live leg) and installs the slot-88 stub for one test.
func screenSetup(t *testing.T, read func(x, y, w, h uint32, buf []byte) int64) {
	adminSetup(t, true, storeWith)
	old := screenRead
	t.Cleanup(func() { screenRead = old })
	screenRead = read
}

// synthFrame answers ScanoutRead with a deterministic B8G8R8X8 frame:
// pixel (x,y) carries B=x, G=y, R=x^y, X=x+y — every channel a different
// signal, so a swapped byte or an off-by-one row is observed.
func synthFrame() func(x, y, w, h uint32, buf []byte) int64 {
	return func(x, y, w, h uint32, buf []byte) int64 {
		need := int(w*h) * vi.ScanoutBPP
		if len(buf) != need {
			return -vi.ErrEINVAL
		}
		for row := uint32(0); row < h; row++ {
			for px := uint32(0); px < w; px++ {
				o := int(row*w+px) * vi.ScanoutBPP
				gx, gy := x+px, y+row
				buf[o] = byte(gx)
				buf[o+1] = byte(gy)
				buf[o+2] = byte(gx ^ gy)
				buf[o+3] = byte(gx + gy)
			}
		}
		return int64(need)
	}
}

// TestScreenChunkPlanCoversTheFrame checks the band plan over the real
// compile-time geometry: contiguous full-width bands, none over the
// slot's per-call bound, the last row landing exactly on the frame edge.
func TestScreenChunkPlanCoversTheFrame(t *testing.T) {
	rects := planScreenChunks()
	var y uint32
	for i, r := range rects {
		if r.x != 0 || r.w != vi.ScanoutWidth {
			t.Fatalf("rect %d is not full-width: %+v", i, r)
		}
		if r.y != y {
			t.Fatalf("rect %d starts at y=%d, want %d — gap or overlap", i, r.y, y)
		}
		if int64(r.w*r.h)*vi.ScanoutBPP > vi.ScanoutReadMax {
			t.Fatalf("rect %d asks %d bytes, over ScanoutReadMax",
				i, int64(r.w*r.h)*vi.ScanoutBPP)
		}
		y += r.h
	}
	if y != vi.ScanoutHeight {
		t.Fatalf("plan covers %d rows, want %d", y, vi.ScanoutHeight)
	}
	// The geometry is fixed: 12-row bands (61,440 bytes each — 13 rows
	// would not fit the 64 KiB bound), 720/12 = 60 reads for a frame.
	if len(rects) != 60 {
		t.Fatalf("plan has %d bands, want 60 for 1280x720", len(rects))
	}
}

// TestScreenPNGRoundTrip reads a synthetic frame through the real chunk
// plan, then decodes the response with image/png — the host-only
// reference webrender pins EncodePNG against (pngenc_test.go); the owned
// decoder's 256 KiB image cap cannot take a full frame.
func TestScreenPNGRoundTrip(t *testing.T) {
	screenSetup(t, synthFrame())
	got := route(adminReq("/admin/screen.png", goodAuth()), noShare(t))
	if got.Status != 200 || got.ContentType != "image/png" {
		t.Fatalf("status=%d type=%q", got.Status, got.ContentType)
	}
	img, err := png.Decode(bytes.NewReader(got.Body))
	if err != nil {
		t.Fatalf("response is not a PNG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != vi.ScanoutWidth || b.Dy() != vi.ScanoutHeight {
		t.Fatalf("decoded %dx%d, want %dx%d",
			b.Dx(), b.Dy(), vi.ScanoutWidth, vi.ScanoutHeight)
	}
	// Every pixel: B8G8R8X8 expanded to 0xAARRGGBB with alpha forced
	// opaque, so x^y lands in R, y in G, x in B.
	for py := 0; py < b.Dy(); py++ {
		for px := 0; px < b.Dx(); px++ {
			r, g, bl, a := img.At(px, py).RGBA()
			wr, wg, wb := uint32(byte(px^py)), uint32(byte(py)), uint32(byte(px))
			if a>>8 != 0xff || r>>8 != wr || g>>8 != wg || bl>>8 != wb {
				t.Fatalf("pixel (%d,%d) = a%02x r%02x g%02x b%02x, want ffr%02xg%02xb%02x",
					px, py, a>>8, r>>8, g>>8, bl>>8, wr, wg, wb)
			}
		}
	}
}

// TestScreenCapturesFreshEveryRequest pins "one capture per request": the
// second request reads the scanout again and sees the changed frame —
// pixels are never cached.
func TestScreenCapturesFreshEveryRequest(t *testing.T) {
	var calls, salt int
	screenSetup(t, func(x, y, w, h uint32, buf []byte) int64 {
		if y == 0 {
			calls++
		}
		rc := synthFrame()(x, y, w, h, buf)
		for i := 0; i+3 < len(buf); i += vi.ScanoutBPP {
			buf[i] += byte(salt)
		}
		return rc
	})
	first := route(adminReq("/admin/screen.png", goodAuth()), noShare(t))
	salt = 0x40
	second := route(adminReq("/admin/screen.png", goodAuth()), noShare(t))
	if calls != 2 {
		t.Fatalf("%d captures for 2 requests, want one each", calls)
	}
	if first.Status != 200 || second.Status != 200 {
		t.Fatalf("statuses %d, %d", first.Status, second.Status)
	}
	img1, err := png.Decode(bytes.NewReader(first.Body))
	if err != nil {
		t.Fatal(err)
	}
	img2, err := png.Decode(bytes.NewReader(second.Body))
	if err != nil {
		t.Fatal(err)
	}
	_, _, b1, _ := img1.At(0, 0).RGBA()
	_, _, b2, _ := img2.At(0, 0).RGBA()
	if b1 == b2 {
		t.Fatal("second response served the first capture's pixels")
	}
}

// TestScreenErrnoMap pins the refusal surface: ENXIO (no armed
// framebuffer) is 503 and every other read errno is a named 500; the
// body always names the kernel's errno.
func TestScreenErrnoMap(t *testing.T) {
	for _, c := range []struct {
		name   string
		rc     int64
		status int
		want   string
	}{
		{"unarmed", -vi.ErrENXIO, 503, "ENXIO"},
		{"bad buf", -vi.ErrEFAULT, 500, "EFAULT"},
		{"bad rect", -vi.ErrEINVAL, 500, "EINVAL"},
		{"unsupported", -vi.ErrENOSYS, 500, "ENOSYS"},
	} {
		t.Run(c.name, func(t *testing.T) {
			screenSetup(t, func(x, y, w, h uint32, buf []byte) int64 { return c.rc })
			got := route(adminReq("/admin/screen.png", goodAuth()), noShare(t))
			if got.Status != c.status {
				t.Fatalf("status=%d, want %d", got.Status, c.status)
			}
			if !strings.Contains(string(got.Body), c.want) {
				t.Fatalf("body %q missing %q", got.Body, c.want)
			}
		})
	}
}

// TestScreenRidesTheAdminLattice checks the endpoint sits behind the
// same three locks as the rest of the section: wrong credentials 401,
// no httpd-admin row 404, plaintext 403.
func TestScreenRidesTheAdminLattice(t *testing.T) {
	screenSetup(t, synthFrame())
	if got := route(adminReq("/admin/screen.png", ""), noShare(t)); got.Status != 401 {
		t.Fatalf("unauthed: status=%d, want 401", got.Status)
	}
	adminStoreGet = storeEmpty
	if got := route(adminReq("/admin/screen.png", goodAuth()), noShare(t)); got.Status != 404 {
		t.Fatalf("no row: status=%d, want 404", got.Status)
	}
	adminStoreGet = storeWith
	adminSecure = false
	if got := route(adminReq("/admin/screen.png", goodAuth()), noShare(t)); got.Status != 403 {
		t.Fatalf("plaintext: status=%d, want 403", got.Status)
	}
}

// TestScreenIndexEmbedsTheImg checks the index carries the card's <img>.
func TestScreenIndexEmbedsTheImg(t *testing.T) {
	screenSetup(t, synthFrame())
	got := route(adminReq("/admin/", goodAuth()), noShare(t))
	if !strings.Contains(string(got.Body), `<img src="/admin/screen.png"`) {
		t.Fatalf("index missing the screen embed: %.300q", got.Body)
	}
}
