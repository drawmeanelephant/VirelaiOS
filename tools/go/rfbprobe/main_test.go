package main

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
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
