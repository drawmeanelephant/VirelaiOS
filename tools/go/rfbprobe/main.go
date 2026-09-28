// rfbprobe is a deliberately independent RFB 3.8 client for the hermetic
// live-rfb gate. Its stdin/stdout are a binary byte stream supplied by the
// runner's --net attachment, NOT a host TCP socket or routable endpoint.
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const (
	frameW = 1280
	frameH = 720
	patchX = 80
	patchY = 150
	patchW = 16
	patchH = 16
)

func main() {
	if err := probe(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "RFBPROBE: FAIL:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "RFBPROBE: pixel and input exchange complete")
}

func probe(r io.Reader, w io.Writer) error {
	var banner [12]byte
	if _, err := io.ReadFull(r, banner[:]); err != nil {
		return fmt.Errorf("server banner: %w", err)
	}
	if string(banner[:]) != "RFB 003.008\n" {
		return fmt.Errorf("server banner %q", banner)
	}
	if err := write(w, banner[:]); err != nil {
		return err
	}
	var security [2]byte
	if _, err := io.ReadFull(r, security[:]); err != nil {
		return fmt.Errorf("security types: %w", err)
	}
	if security != [2]byte{1, 1} {
		return fmt.Errorf("security types %v, want exactly None", security)
	}
	if err := write(w, []byte{1}); err != nil {
		return err
	}
	var result [4]byte
	if _, err := io.ReadFull(r, result[:]); err != nil {
		return fmt.Errorf("security result: %w", err)
	}
	if result != [4]byte{} {
		return fmt.Errorf("security result %x", result)
	}
	if err := write(w, []byte{1}); err != nil {
		return err
	}
	var init [24]byte
	if _, err := io.ReadFull(r, init[:]); err != nil {
		return fmt.Errorf("ServerInit: %w", err)
	}
	if binary.BigEndian.Uint16(init[0:2]) != frameW || binary.BigEndian.Uint16(init[2:4]) != frameH ||
		init[4] != 32 || init[5] != 24 || init[6] != 0 || init[7] != 1 ||
		init[14] != 16 || init[15] != 8 || init[16] != 0 {
		return fmt.Errorf("ServerInit dimensions or pixel format: %x", init[:20])
	}
	n := binary.BigEndian.Uint32(init[20:24])
	if n > 255 {
		return fmt.Errorf("server name length %d", n)
	}
	name := make([]byte, n)
	if _, err := io.ReadFull(r, name); err != nil {
		return fmt.Errorf("server name: %w", err)
	}
	fmt.Fprintf(os.Stderr, "RFBPROBE: RFB 3.8 None %dx%d %s\n", frameW, frameH, name)

	// Raw only, never a library call into the server codec. The probe asks
	// for a 16x16 region fully inside Calc's first idle button, where its
	// token is dark BtnIdle #2d3748. The empty desktop is #182026, so a
	// passing comparison cannot come from the seat's blank frame alone.
	if err := write(w, []byte{2, 0, 0, 1, 0, 0, 0, 0}); err != nil {
		return err
	}
	req := []byte{3, 0, 0, patchX, 0, patchY, 0, patchW, 0, patchH}
	if err := write(w, req); err != nil {
		return err
	}
	var header [16]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return fmt.Errorf("FramebufferUpdate: %w", err)
	}
	if header[0] != 0 || binary.BigEndian.Uint16(header[2:4]) != 1 ||
		binary.BigEndian.Uint16(header[4:6]) != patchX ||
		binary.BigEndian.Uint16(header[6:8]) != patchY ||
		binary.BigEndian.Uint16(header[8:10]) != patchW ||
		binary.BigEndian.Uint16(header[10:12]) != patchH ||
		binary.BigEndian.Uint32(header[12:16]) != 0 {
		return fmt.Errorf("raw rectangle header: %x", header)
	}
	pixels := make([]byte, patchW*patchH*4)
	if _, err := io.ReadFull(r, pixels); err != nil {
		return fmt.Errorf("raw pixels: %w", err)
	}
	const wantB, wantG, wantR = 0x48, 0x37, 0x2d
	for i := 0; i < len(pixels); i += 4 {
		if pixels[i] != wantB || pixels[i+1] != wantG || pixels[i+2] != wantR || pixels[i+3] != 0 {
			return fmt.Errorf("Calc button pixel (%d,%d) = %x want BGR0 48372d00",
				patchX+(i/4)%patchW, patchY+(i/4)/patchW, pixels[i:i+4])
		}
	}
	fmt.Fprintln(os.Stderr, "RFBPROBE: Calc BtnIdle 16x16 pixels = #2d3748")

	// Ctrl+Space opens the seat launcher. A left press OUTSIDE its panel
	// dismisses it. They traverse the same kind-21/19 path as local input.
	for _, event := range [][]byte{
		key(true, 0xffe3), key(true, ' '), key(false, ' '), key(false, 0xffe3),
		pointer(1, 100, 600), pointer(0, 100, 600),
	} {
		if err := write(w, event); err != nil {
			return err
		}
	}
	return nil
}

func key(down bool, sym uint32) []byte {
	b := []byte{4, 0, 0, 0, 0, 0, 0, 0}
	if down {
		b[1] = 1
	}
	binary.BigEndian.PutUint32(b[4:], sym)
	return b
}

func pointer(buttons uint8, x, y uint16) []byte {
	b := []byte{5, buttons, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(b[2:4], x)
	binary.BigEndian.PutUint16(b[4:6], y)
	return b
}

func write(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
