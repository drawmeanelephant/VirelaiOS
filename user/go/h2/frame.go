// The frame layer (RFC 9113 §4.1): the 9-byte header — u24 length, u8
// type, u8 flags, u31 stream identifier — and the frame type / settings /
// flag constants the codec speaks.

package h2

const (
	frameDATA         = 0x0
	frameHEADERS      = 0x1
	framePRIORITY     = 0x2
	frameRSTSTREAM    = 0x3
	frameSETTINGS     = 0x4
	framePUSHPROMISE  = 0x5
	framePING         = 0x6
	frameGOAWAY       = 0x7
	frameWINDOWUPDATE = 0x8
	frameCONTINUATION = 0x9
)

// flag bits (meanings are per-type; flagEndStream and flagAck share 0x1).
const (
	flagEndStream  = 0x1
	flagAck        = 0x1
	flagEndHeaders = 0x4
	flagPadded     = 0x8
	flagPriority   = 0x20
)

// SETTINGS parameter identifiers (RFC 9113 §6.5.2).
const (
	settingsHeaderTable    = 0x1
	settingsEnablePush     = 0x2
	settingsMaxConcurrent  = 0x3
	settingsInitialWindow  = 0x4
	settingsMaxFrame       = 0x5
	settingsMaxHeaderList  = 0x6
)

// clientPreface is the 24-octet magic every client connection opens with.
const clientPreface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"

// frameHeader renders a 9-byte frame header for a payload of length n.
func frameHeader(typ, flags byte, sid uint32, n int) []byte {
	return []byte{
		byte(n >> 16), byte(n >> 8), byte(n),
		typ, flags,
		byte(sid >> 24), byte(sid >> 16), byte(sid >> 8), byte(sid),
	}
}

// appendSetting appends one SETTINGS parameter (u16 id || u32 value).
func appendSetting(p []byte, id uint16, v uint32) []byte {
	return append(p, byte(id>>8), byte(id), byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
