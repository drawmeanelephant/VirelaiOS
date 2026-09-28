// Package rfb implements the server-side RFB 3.8 wire protocol of RFC 6143.
// It consumes and produces bytes on an io.Reader/io.Writer pair. It does not
// listen, dial, authenticate a transport, compose a frame, or inject input.
// A seat can use it over the hermetic test connection now; an SSH channel can
// carry the same bytes later.
//
// # Trust boundary
//
// The wire offers exactly security type 1 (None). It has NO password and
// supplies NO authentication or confidentiality. ADR 0037 D6 restricts use
// to the trusted hermetic runner and a same-user, localhost-only test bridge.
// Never expose this codec as direct RFB on a routable network. Remote use is
// unsupported until an authenticated transport exists. Pointer and key
// messages carry the local seat's authority; the caller must enforce the
// transport/exposure boundary before forwarding them.
//
// # Connection
//
// Construct a Session with NewSession(reader, writer, width, height, name),
// then call Handshake once. The server writes "RFB 003.008\n" first. The
// client replies "RFB 003.008\n", "RFB 003.007\n", or "RFB 003.003\n".
// In 3.8/3.7 the server sends {1, 1} (one security type, None), reads
// the client's selection {1}, and sends a four-byte zero SecurityResult
// in 3.8 (3.7 omits it for None). In 3.3 the server sends the four-byte
// big-endian value 1 instead, with no selection/result. A bad 3.8
// selection gets SecurityResult 1 followed by a u32 length and ASCII
// reason, then the connection must close. The next client byte is
// ClientInit.shared (0 or 1); Handshake returns it. The server replies
// with ServerInit: width:u16, height:u16, pixel format:16 bytes,
// name length:u32, then name bytes. All multibyte wire fields are
// big-endian except pixel values requested little-endian by SetPixelFormat.
//
// The initial pixel format is 32 bpp / depth 24, little-endian true color,
// RGB maxima 255/255/255 and shifts 16/8/0. Thus each wire pixel is
// B,G,R,0 from the seat's B8G8R8X8 scanout; the X byte is never sent as
// source data. A client may request 8/16/32-bpp true color with valid
// nonoverlapping RGB bitfields and either byte order. Palette formats are
// refused. Changing the format invalidates the sent-frame cache.
//
// # Client messages
//
// After Handshake, ReadMessage reads exactly one message. Supported layouts:
//
//	0  SetPixelFormat       3 padding, 16-byte pixel format
//	2  SetEncodings         1 padding, count:u16, count * encoding:i32
//	3  FramebufferUpdateRequest incremental:u8, x/y/w/h:u16
//	4  KeyEvent             down:u8, 2 padding, keysym:u32
//	5  PointerEvent         buttons:u8, x/y:u16
//
// Unknown messages, unsupported formats, truncated reads, excessive encoding
// counts, and out-of-bounds update requests return errors. Unknown encoding
// numbers are ignored; raw (0) is implicit. The first supported image
// encoding in SetEncodings wins (raw 0, RRE 2, hextile 5). DesktopSize
// (-223) and RichCursor (-239) require explicit advertisement. KeyEvent
// carries a numeric X11 keysym; KeysymToHID maps it to a USB HID usage
// without depending on X11. Unknown keysyms remain visible with Known=false
// and must not be synthesized as usage zero.
//
// # Server updates
//
// Pass a request returned by ReadMessage and a full, row-major B,G,R,X
// frame to FramebufferUpdate. The server sends message 0, pad:u8,
// rectangle-count:u16, then rectangles (x/y/w/h:u16, encoding:i32,
// payload). Raw sends row-major pixels. RRE sends count:u32,
// background pixel, then count (pixel, x/y/w/h:u16 relative to the
// rectangle). Hextile visits 16x16 tiles in scan order, each with a
// subencoding byte: raw (bit 0) sends tile pixels; otherwise bit 1
// specifies a background pixel, bit 2 a foreground pixel, bit 3
// subrectangles, and bit 4 a pixel on every subrectangle. The subrect
// count is u8; each xy and (width-1,height-1) pair packs two four-bit
// coordinates. We always specify a background per non-raw tile.
// RRE falls back to raw when it would cost more bytes. An incremental
// request sends only dirty rectangles of at most 16x16 pixels within
// the requested area; if no pixel has changed it returns sent=false
// and writes nothing.
// Cache state advances only after a successful write. The first request
// considers every pixel unsent. Full requests always produce an update.
//
// DesktopSize sends one rectangle with encoding -223 and the new width
// and height, no payload; RichCursor sends one rectangle with encoding
// -239, x/y as the hotspot, size as the cursor dimensions, then row-major
// pixels followed by an MSB-first one-bit transparency mask with rows
// padded to whole bytes. Neither is sent without client advertisement.
// Bounds: width <= 1280, height <= 720, encoding count <= 256, cursor
// <= 64x64, name <= 255 bytes. A wire read/write error ends the session;
// a caller must not try to resynchronize a malformed or partially written
// byte stream. A rejected local argument writes nothing and leaves the
// session intact.
package rfb
