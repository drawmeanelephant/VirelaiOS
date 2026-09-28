package main

import (
	"bytes"
	"encoding/binary"
	"testing"
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
