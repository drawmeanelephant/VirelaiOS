package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestInputWireShapes(t *testing.T) {
	if got := key(true, 0xffe3); !bytes.Equal(got, []byte{4, 1, 0, 0, 0, 0, 0xff, 0xe3}) {
		t.Fatalf("Ctrl_L: %x", got)
	}
	if got := pointer(1, 100, 600); !bytes.Equal(got, []byte{5, 1, 0, 100, 2, 88}) {
		t.Fatalf("pointer: %x", got)
	}
}

func TestProbeRejectsWrongPixel(t *testing.T) {
	wire := fixture()
	// Same shape and handshake, but one pixel from the empty desktop.
	p := len(wire) - patchW*patchH*4
	copy(wire[p:p+4], []byte{0x26, 0x20, 0x18, 0})
	var out bytes.Buffer
	if err := probe(bytes.NewReader(wire), &out); err == nil || !strings.Contains(err.Error(), "Calc button pixel") {
		t.Fatalf("expected pixel refusal, got %v", err)
	}
}

func TestProbeNegotiatesAndRequestsRealPixels(t *testing.T) {
	var out bytes.Buffer
	if err := probe(bytes.NewReader(fixture()), &out); err != nil {
		t.Fatal(err)
	}
	b := out.Bytes()
	if !bytes.HasPrefix(b, []byte("RFB 003.008\n")) ||
		!bytes.Contains(b, []byte{3, 0, 0, patchX, 0, patchY, 0, patchW, 0, patchH}) ||
		!bytes.HasSuffix(b, pointer(0, 100, 600)) {
		t.Fatalf("client did not request pixels and send input: %x", b)
	}
}

func TestSecurityRefusalRequiresFailedResultThenClose(t *testing.T) {
	refusal := append([]byte("RFB 003.008\n"), 1, 1, 0, 0, 0, 1, 0, 0, 0, 6)
	refusal = append(refusal, "denied"...)
	var out bytes.Buffer
	if err := probeSecurityRefused(bytes.NewReader(refusal), &out); err != nil {
		t.Fatal(err)
	}
	b := out.Bytes()
	if !bytes.HasPrefix(b, []byte("RFB 003.008\n\x02")) || !bytes.HasSuffix(b, pointer(1, contentX, contentY)) {
		t.Fatalf("client did not pick VncAuth then keep talking: %x", b)
	}
	if err := probeSecurityRefused(bytes.NewReader(append(refusal, 0)), &out); err == nil ||
		!strings.Contains(err.Error(), "seat sent 00 after the refusal") {
		t.Fatalf("a byte after the refusal must fail, got %v", err)
	}
	accepted := append([]byte("RFB 003.008\n"), 1, 1, 0, 0, 0, 0, 0, 0, 0, 0)
	if err := probeSecurityRefused(bytes.NewReader(accepted), &out); err == nil ||
		!strings.Contains(err.Error(), "want failed") {
		t.Fatalf("an accepted VncAuth must fail, got %v", err)
	}
}

func TestHostileModesSendTheirBytesAndWantAHangUp(t *testing.T) {
	settle = 0
	for _, tc := range []struct {
		mode string
		run  func(io.Reader, io.Writer) error
		want []byte
	}{
		{"malformed", probeMalformed, []byte{2, 0, 0xff, 0xff, 0, 0, 0, 0}},
		{"stall", probeStall, []byte{3, 0, 0}},
	} {
		var out bytes.Buffer
		if err := tc.run(bytes.NewReader(handshake()), &out); err != nil {
			t.Fatalf("%s: %v", tc.mode, err)
		}
		if !bytes.HasSuffix(out.Bytes(), tc.want) {
			t.Fatalf("%s: wire ends %x, want %x", tc.mode, out.Bytes(), tc.want)
		}
	}
	var out bytes.Buffer
	if err := probeMalformed(bytes.NewReader(handshake()), &out); err != nil ||
		!bytes.Contains(out.Bytes(), pointer(1, contentX, contentY)) {
		t.Fatalf("malformed mode must hold a content press first: %v %x", err, out.Bytes())
	}
}

func TestExpectCloseFailsWhenTheSeatStaysOpen(t *testing.T) {
	old := closeWait
	closeWait = 50 * time.Millisecond
	defer func() { closeWait = old }()
	r, w := io.Pipe()
	defer w.Close()
	if err := expectClose(r, "while idle"); err == nil || !strings.Contains(err.Error(), "kept the connection open") {
		t.Fatalf("expected a timeout, got %v", err)
	}
}

func TestDeathModesDieOnPurpose(t *testing.T) {
	settle = 0
	var died errDied
	var out bytes.Buffer
	if err := probeDieFocus(bytes.NewReader(handshake()), &out); !errors.As(err, &died) ||
		!bytes.HasSuffix(out.Bytes(), key(true, 'c')) {
		t.Fatalf("die-focus: %v %x", err, out.Bytes())
	}
	wire := append(handshake(), 0, 0, 0, 1, 0, 0, 0, 0, 0x05, 0, 0x02, 0xd0, 0, 0, 0, 0)
	wire = append(wire, make([]byte, 2048)...)
	out.Reset()
	if err := probeDieDrag(bytes.NewReader(wire), &out); !errors.As(err, &died) {
		t.Fatalf("die-drag: %v", err)
	}
	if !bytes.Contains(out.Bytes(), concat(pointer(1, contentX, contentY), pointer(1, contentX+40, contentY+30))) {
		t.Fatalf("die-drag did not drag with the button held: %x", out.Bytes())
	}
	if err := probeDieDrag(bytes.NewReader(handshake()), &out); err == nil || errors.As(err, &died) {
		t.Fatalf("die-drag without an update must fail, not die: %v", err)
	}
}

func TestBridgeIsLoopbackOnlyAndPipesOneViewer(t *testing.T) {
	if err := runBridge("0.0.0.0:0", strings.NewReader(""), io.Discard); err == nil ||
		!strings.Contains(err.Error(), "not loopback") {
		t.Fatalf("wildcard bridge must be refused, got %v", err)
	}
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	seatR, seatW := io.Pipe()
	defer seatW.Close()
	var toGuest bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- bridgeOne(ln, 5*time.Second, seatR, &toGuest) }()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = seatW.Write([]byte("RFB 003.008\n")) }()
	banner := make([]byte, 12)
	if _, err := io.ReadFull(c, banner); err != nil || string(banner) != "RFB 003.008\n" {
		t.Fatalf("viewer banner %q %v", banner, err)
	}
	_, _ = c.Write([]byte("RFB 003.003\n"))
	_ = c.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if toGuest.String() != "RFB 003.003\n" {
		t.Fatalf("guest got %q", toGuest.String())
	}
	if _, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err == nil {
		t.Fatal("bridge accepted a second viewer")
	}
}

// handshake is a server's None negotiation through ServerInit, then EOF.
func handshake() []byte {
	b := fixture()
	return b[:len(b)-16-patchW*patchH*4]
}

func fixture() []byte {
	b := []byte("RFB 003.008\n")
	b = append(b, 1, 1, 0, 0, 0, 0) // None and SecurityResult=0
	init := make([]byte, 24)
	binary.BigEndian.PutUint16(init[:2], frameW)
	binary.BigEndian.PutUint16(init[2:4], frameH)
	init[4], init[5], init[7] = 32, 24, 1
	init[14], init[15] = 16, 8
	binary.BigEndian.PutUint32(init[20:24], 9)
	b = append(b, init...)
	b = append(b, "VirelaiOS"...)
	rect := make([]byte, 16)
	binary.BigEndian.PutUint16(rect[2:4], 1)
	binary.BigEndian.PutUint16(rect[4:6], patchX)
	binary.BigEndian.PutUint16(rect[6:8], patchY)
	binary.BigEndian.PutUint16(rect[8:10], patchW)
	binary.BigEndian.PutUint16(rect[10:12], patchH)
	b = append(b, rect...)
	for range patchW * patchH {
		b = append(b, 0x48, 0x37, 0x2d, 0)
	}
	return b
}
