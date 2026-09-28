package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// The guest picks the first supported image encoding a viewer advertises.
// Screen Sharing can list Raw before hextile/RRE; that would send a 3.7 MB
// first frame through the runner's paced 192-byte TCP segments. Reorder only
// encodings the viewer itself advertised, and leave every other message
// byte-for-byte intact. No pixel format or input bytes are logged.
func forwardViewerMessages(viewer io.Reader, guest io.Writer) (int64, error) {
	var forwarded int64
	for {
		var kind [1]byte
		if _, err := io.ReadFull(viewer, kind[:]); err != nil {
			if err == io.EOF {
				return forwarded, nil
			}
			return forwarded, err
		}
		switch kind[0] {
		case 0: // SetPixelFormat: 3 padding + 16 format bytes
			if err := forwardFixed(viewer, guest, kind[0], 19); err != nil {
				return forwarded, err
			}
			forwarded += 20
		case 2: // SetEncodings: 1 padding + count:u16 + count * i32
			var header [3]byte
			if _, err := io.ReadFull(viewer, header[:]); err != nil {
				return forwarded, err
			}
			count := int(binary.BigEndian.Uint16(header[1:]))
			if count > 256 {
				// The guest's existing bound will refuse this message.
				if err := write(guest, append(kind[:], header[:]...)); err != nil {
					return forwarded, err
				}
				n, err := io.Copy(guest, viewer)
				return forwarded + 4 + n, err
			}
			codes := make([]byte, count*4)
			if _, err := io.ReadFull(viewer, codes); err != nil {
				return forwarded, err
			}
			best := -1
			rre := -1
			for i := 0; i < count; i++ {
				switch int32(binary.BigEndian.Uint32(codes[4*i:])) {
				case 5: // hextile
					best = i
				case 2: // RRE
					rre = i
				}
			}
			if best < 0 {
				best = rre
			}
			if best >= 0 {
				code := append([]byte(nil), codes[best*4:best*4+4]...)
				copy(codes[4:best*4+4], codes[:best*4])
				copy(codes[:4], code)
				fmt.Fprintf(os.Stderr, "RFBPROBE: bridge preferred viewer-advertised encoding %d (count=%d)\n",
					binary.BigEndian.Uint32(code), count)
			} else {
				fmt.Fprintf(os.Stderr, "RFBPROBE: bridge viewer advertised no hextile/RRE (count=%d)\n", count)
			}
			if err := write(guest, append(append(kind[:], header[:]...), codes...)); err != nil {
				return forwarded, err
			}
			forwarded += int64(4 + len(codes))
		case 3: // FramebufferUpdateRequest
			if err := forwardFixed(viewer, guest, kind[0], 9); err != nil {
				return forwarded, err
			}
			forwarded += 10
		case 4: // KeyEvent
			if err := forwardFixed(viewer, guest, kind[0], 7); err != nil {
				return forwarded, err
			}
			forwarded += 8
		case 5: // PointerEvent
			if err := forwardFixed(viewer, guest, kind[0], 5); err != nil {
				return forwarded, err
			}
			forwarded += 6
		default:
			// Preserve the guest's own malformed-message refusal behavior.
			if err := write(guest, kind[:]); err != nil {
				return forwarded, err
			}
			n, err := io.Copy(guest, viewer)
			return forwarded + 1 + n, err
		}
	}
}

func forwardFixed(viewer io.Reader, guest io.Writer, kind byte, rest int) error {
	buf := make([]byte, 1+rest)
	buf[0] = kind
	if _, err := io.ReadFull(viewer, buf[1:]); err != nil {
		return err
	}
	return write(guest, buf)
}
