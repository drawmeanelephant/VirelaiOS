package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func viewerEncodings(codes ...int32) []byte {
	b := []byte{2, 0, byte(len(codes) >> 8), byte(len(codes))}
	for _, code := range codes {
		b = binary.BigEndian.AppendUint32(b, uint32(code))
	}
	return b
}

func TestBridgePrefersAdvertisedCompression(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []int32
		want []int32
	}{
		{"hextile ahead of Raw and RRE", []int32{0, -223, 2, 5}, []int32{5, 0, -223, 2}},
		{"RRE if hextile absent", []int32{0, 2}, []int32{2, 0}},
		{"Raw stays Raw", []int32{0, -223}, []int32{0, -223}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			format := append([]byte{0, 0, 0, 0}, make([]byte, 16)...)
			request := []byte{3, 0, 0, 80, 0, 150, 0, 16, 0, 16}
			key := key(true, 'x')
			pointer := pointer(1, 100, 600)
			input := concat(format, viewerEncodings(tc.in...), request, key, pointer)
			var guest bytes.Buffer
			n, err := forwardViewerMessages(bytes.NewReader(input), &guest)
			if err != nil || n != int64(len(input)) {
				t.Fatalf("forward = %d, %v", n, err)
			}
			want := concat(format, viewerEncodings(tc.want...), request, key, pointer)
			if !bytes.Equal(guest.Bytes(), want) {
				t.Fatalf("guest messages %x, want %x", guest.Bytes(), want)
			}
		})
	}
}

func TestBridgeForwardsMalformedCountToGuest(t *testing.T) {
	input := []byte{2, 0, 0xff, 0xff, 0, 0, 0, 0}
	var guest bytes.Buffer
	n, err := forwardViewerMessages(bytes.NewReader(input), &guest)
	if err != nil || n != int64(len(input)) || !bytes.Equal(guest.Bytes(), input) {
		t.Fatalf("malformed guest refusal bytes = %x, n=%d err=%v", guest.Bytes(), n, err)
	}
}

func TestBridgeRequestsGuestHextileForRawOnlyViewer(t *testing.T) {
	plan := newBridgeFramePlan(4)
	format := append([]byte{0, 0, 0, 0}, make([]byte, 16)...)
	format[4] = 16
	request := []byte{3, 0, 0, 80, 0, 150, 0, 16, 0, 16}
	input := concat(format, viewerEncodings(0, -223, -239), request)
	var guest bytes.Buffer
	n, err := forwardViewerMessagesWithPlan(bytes.NewReader(input), &guest, plan)
	want := concat(format, viewerEncodings(5, 0, -223, -239), request)
	if err != nil || n != int64(len(want)) || !bytes.Equal(guest.Bytes(), want) {
		t.Fatalf("guest messages = %x, n=%d err=%v, want %x", guest.Bytes(), n, err, want)
	}
	if !plan.mode() || plan.pixelBPP() != 2 {
		t.Fatal("bridge did not enable translation at the viewer's pixel depth")
	}
}

func TestBridgeHextileToRawBoundsAndPixels(t *testing.T) {
	for _, bpp := range []int{1, 2, 4} {
		t.Run(string(rune('0'+bpp)), func(t *testing.T) {
			w, h := 20, 17
			bg := bytes.Repeat([]byte{0x12}, bpp)
			fg := bytes.Repeat([]byte{0xa5}, bpp)
			var body bytes.Buffer
			// Four tiles. The first has a colored two-by-two subrectangle,
			// the second is a raw edge tile; the last two are solid.
			body.WriteByte(2 | 4 | 8)
			body.Write(bg)
			body.Write(fg)
			body.Write([]byte{1, 0x11, 0x11})
			body.WriteByte(1)
			body.Write(bytes.Repeat([]byte{0x55}, 4*16*bpp))
			body.WriteByte(2)
			body.Write(bg)
			body.WriteByte(2)
			body.Write(bg)

			guest := []byte{0, 0, 0, 1}
			rect := make([]byte, 12)
			binary.BigEndian.PutUint16(rect[4:], uint16(w))
			binary.BigEndian.PutUint16(rect[6:], uint16(h))
			binary.BigEndian.PutUint32(rect[8:], 5)
			guest = append(append(guest, rect...), body.Bytes()...)
			plan := newBridgeFramePlan(bpp)
			var viewer bytes.Buffer
			n, err := forwardTranscodedFrames(bytes.NewReader(guest), &viewer, plan)
			if err != io.EOF || n != int64(4+12+w*h*bpp) || viewer.Len() != int(n) {
				t.Fatalf("translated %d bytes, %v, viewer=%d", n, err, viewer.Len())
			}
			got := viewer.Bytes()
			if binary.BigEndian.Uint32(got[12:16]) != 0 {
				t.Fatalf("viewer was sent guest-only encoding: %x", got[12:16])
			}
			pixels := got[16:]
			for _, pixel := range []struct {
				x, y int
				want []byte
			}{
				{0, 0, bg}, {1, 1, fg}, {2, 2, fg}, {3, 3, bg},
				{16, 0, bytes.Repeat([]byte{0x55}, bpp)}, {19, 15, bytes.Repeat([]byte{0x55}, bpp)},
				{0, 16, bg}, {19, 16, bg},
			} {
				i := (pixel.y*w + pixel.x) * bpp
				if !bytes.Equal(pixels[i:i+bpp], pixel.want) {
					t.Fatalf("pixel %d,%d=%x want %x", pixel.x, pixel.y, pixels[i:i+bpp], pixel.want)
				}
			}
		})
	}
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"missing background", []byte{0}},
		{"colored flag without subrects", []byte{2 | 16, 1, 2, 3, 4}},
		{"subrect beyond edge", []byte{2 | 4 | 8, 1, 2, 3, 4, 5, 6, 7, 8, 1, 0xff, 0xff}},
		{"truncated tile", []byte{1, 0}},
		{"reserved flag", []byte{0x20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := decodeHextile(bytes.NewReader(tc.body), make([]byte, 4), 1, 1, 4); err == nil {
				t.Fatal("malformed tile accepted")
			}
		})
	}
}

func TestBridgeStreamsFirstRawBandBeforeGuestFrameCompletes(t *testing.T) {
	guestR, guestW := io.Pipe()
	viewerR, viewerW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := forwardTranscodedFrames(guestR, viewerW, newBridgeFramePlan(4))
		done <- err
	}()
	go func() {
		first := []byte{0, 0, 0, 1, // update
			0, 0, 0, 0, 0, 16, 0, 32, 0, 0, 0, 5, // rect
			2, 1, 2, 3, 4} // first tile is solid
		_, _ = guestW.Write(first)
	}()
	band := make([]byte, 4+12+16*16*4)
	received := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(viewerR, band)
		received <- err
	}()
	select {
	case err := <-received:
		if err != nil || binary.BigEndian.Uint32(band[12:16]) != 0 ||
			!bytes.Equal(band[16:20], []byte{1, 2, 3, 4}) {
			t.Fatalf("first Raw band before second tile: %x, %v", band[:20], err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge held the first Raw band until the whole guest frame")
	}
	go func() {
		_, _ = guestW.Write([]byte{2, 5, 6, 7, 8})
		_ = guestW.Close()
	}()
	second := readTestBytes(t, viewerR, 16*16*4)
	if !bytes.Equal(second[:4], []byte{5, 6, 7, 8}) {
		t.Fatalf("second band pixel = %x", second[:4])
	}
	if err := <-done; err != io.EOF {
		t.Fatalf("frame pump exit = %v", err)
	}
}

func TestBridgeRawOnlyViewerGetsDecodedFrame(t *testing.T) {
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	guestStream := append(handshake(), []byte{
		0, 0, 0, 1, // one FramebufferUpdate rectangle
		0, 80, 0, 150, 0, 16, 0, 16, // 16x16 at the test patch
		0, 0, 0, 5, // Hextile
		2, 0x37, 0x2d, 0x48, 0, // one solid-color tile
	}...)
	var guestOut bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- bridgeOne(ln, 5*time.Second, "password", bytes.NewReader(guestStream), &guestOut) }()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	authenticateTestViewer(t, c, "RFB 003.003\n", "password")
	_, _ = c.Write([]byte{1})
	readTestBytes(t, c, 24+9) // ServerInit
	_, _ = c.Write(concat(viewerEncodings(0, -223), []byte{3, 0, 0, 80, 0, 150, 0, 16, 0, 16}))
	update := readTestBytes(t, c, 16+16*16*4)
	if binary.BigEndian.Uint32(update[12:16]) != 0 {
		t.Fatalf("viewer saw non-Raw encoding %x", update[12:16])
	}
	for i := 16; i < len(update); i += 4 {
		if !bytes.Equal(update[i:i+4], []byte{0x37, 0x2d, 0x48, 0}) {
			t.Fatalf("pixel %d = %x", i, update[i:i+4])
		}
	}
	_ = c.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
