// Codec pins over a scripted byte stream: the full SETTINGS/HEADERS/DATA
// exchange, the request block's exact wire shape, and every named
// refusal (PUSH_PROMISE, non-SETTINGS preface, oversized frames,
// CONTINUATION storms, window overflow, RST_STREAM/GOAWAY codenames).
package h2

import (
	"bytes"
	"errors"
	"testing"
)

type scriptStream struct {
	in  []byte
	out []byte
}

func (s *scriptStream) Read(p []byte) (int, error) {
	if len(s.in) == 0 {
		return 0, errors.New("script: eof")
	}
	n := copy(p, s.in)
	s.in = s.in[n:]
	return n, nil
}

func (s *scriptStream) Write(p []byte) (int, error) {
	s.out = append(s.out, p...)
	return len(p), nil
}

func fr(typ, flags byte, sid uint32, payload []byte) []byte {
	return append(frameHeader(typ, flags, sid, len(payload)), payload...)
}

func settings() []byte { return fr(frameSETTINGS, 0, 0, nil) }

// okTranscript is the minimal honest h2 exchange: SETTINGS, then a
// :status 200 head and an END_STREAM DATA body for stream 1.
func okTranscript(body string) []byte {
	head := fr(frameHEADERS, flagEndHeaders, 1, []byte{0x88})
	data := fr(frameDATA, flagEndStream, 1, []byte(body))
	return append(append(settings(), head...), data...)
}

func TestGetRoundTrip(t *testing.T) {
	s := &scriptStream{in: okTranscript("go-fetch-h2-ok\n")}
	c := NewConn(s)
	resp, err := c.Get("leaf.example.com:24554", "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 {
		t.Fatalf("status %d", resp.Status)
	}
	if string(resp.Body) != "go-fetch-h2-ok\n" {
		t.Fatalf("body %q", resp.Body)
	}

	out := s.out
	if !bytes.HasPrefix(out, []byte(clientPreface)) {
		t.Fatal("no client preface")
	}
	out = out[len(clientPreface):]
	// Our SETTINGS (18 bytes: three entries), then the ACK.
	if out[3] != frameSETTINGS {
		t.Fatal("no client SETTINGS")
	}
	setLen := int(out[0])<<16 | int(out[1])<<8 | int(out[2])
	out = out[9+setLen:]
	if out[3] != frameSETTINGS || out[4]&flagAck == 0 {
		t.Fatal("no SETTINGS ack")
	}
	out = out[9:]
	// The request HEADERS: END_HEADERS|END_STREAM on stream 1, then the
	// deterministic block.
	if out[3] != frameHEADERS || out[4] != flagEndHeaders|flagEndStream {
		t.Fatalf("request flags %02x", out[4])
	}
	if u31(out[5:9]) != 1 {
		t.Fatal("request stream id")
	}
	want := []byte{0x82, 0x87, 0x01}
	want = append(want, byte(len("leaf.example.com:24554")))
	want = append(want, "leaf.example.com:24554"...)
	want = append(want, 0x04, 1, '/')
	if !bytes.Equal(out[9:9+len(want)], want) {
		t.Fatalf("request block %x", out[9:9+len(want)])
	}
}

func TestPushPromiseRefused(t *testing.T) {
	pp := fr(framePUSHPROMISE, flagEndHeaders, 1, append([]byte{0, 0, 0, 2}, 0x88))
	head := fr(frameHEADERS, flagEndHeaders, 1, []byte{0x88})
	in := append(append(settings(), pp...), head...)
	s := &scriptStream{in: in}
	_, err := NewConn(s).Get("a", "/")
	if !errors.Is(err, ErrPushPromise) {
		t.Fatalf("err %v, want ErrPushPromise", err)
	}
}

func TestNonSettingsPrefaceRefused(t *testing.T) {
	s := &scriptStream{in: fr(framePING, 0, 0, make([]byte, 8))}
	_, err := NewConn(s).Get("a", "/")
	if !errors.Is(err, ErrSettingsFirst) {
		t.Fatalf("err %v, want ErrSettingsFirst", err)
	}
}

func TestOversizedFrameRefused(t *testing.T) {
	s := &scriptStream{in: append(settings(), frameHeader(frameDATA, 0, 1, maxFrameLen+1)...)}
	_, err := NewConn(s).Get("a", "/")
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err %v, want ErrFrameTooLarge", err)
	}
}

func TestContinuationStormRefused(t *testing.T) {
	in := append(settings(), frameHeader(frameHEADERS, 0, 1, 1)...)
	in = append(in, 0x88)
	for i := 0; i < maxContinuations+1; i++ {
		in = append(in, fr(frameCONTINUATION, 0, 1, []byte{0x00})...)
	}
	s := &scriptStream{in: in}
	_, err := NewConn(s).Get("a", "/")
	if !errors.Is(err, ErrContinuation) {
		t.Fatalf("err %v, want ErrContinuation", err)
	}
}

// A header block under the fragment bound still assembles and decodes.
func TestContinuationAssembles(t *testing.T) {
	in := append(settings(), frameHeader(frameHEADERS, 0, 1, 1)...)
	in = append(in, 0x88)
	in = append(in, fr(frameCONTINUATION, flagEndHeaders, 1, []byte{0x0f, 0x0d, 0x02, '3', '1'})...) // content-length: "31"
	in = append(in, fr(frameDATA, flagEndStream, 1, []byte("x"))...)
	s := &scriptStream{in: in}
	resp, err := NewConn(s).Get("a", "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 || string(resp.Body) != "x" {
		t.Fatalf("resp %+v", resp)
	}
}

func TestRSTStreamNamed(t *testing.T) {
	code := []byte{0, 0, 0, 0x8} // CANCEL
	s := &scriptStream{in: append(settings(), fr(frameRSTSTREAM, 0, 1, code)...)}
	_, err := NewConn(s).Get("a", "/")
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("rst_stream cancel")) {
		t.Fatalf("err %v", err)
	}
}

func TestGoawayBlocksUnprocessed(t *testing.T) {
	g := fr(frameGOAWAY, 0, 0, []byte{0, 0, 0, 0, 0, 0, 0, 1}) // last=0, protocol_error
	s := &scriptStream{in: append(settings(), g...)}
	_, err := NewConn(s).Get("a", "/")
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("goaway protocol_error")) {
		t.Fatalf("err %v", err)
	}
}

func TestInterimThenFinal(t *testing.T) {
	// literal :status "103" — name index 8 (:status), value "103".
	h103 := []byte{0x08, 0x03, '1', '0', '3'}
	in := append(settings(), fr(frameHEADERS, flagEndHeaders, 1, h103)...)
	in = append(in, okTranscript("body")[len(settings()):]...)
	s := &scriptStream{in: in}
	resp, err := NewConn(s).Get("a", "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 || string(resp.Body) != "body" {
		t.Fatalf("resp %+v", resp)
	}
}

func TestPaddedData(t *testing.T) {
	payload := append([]byte{4}, []byte("data")...)
	payload = append(payload, 0, 0, 0, 0)
	in := append(settings(), fr(frameHEADERS, flagEndHeaders, 1, []byte{0x88})...)
	in = append(in, fr(frameDATA, flagPadded|flagEndStream, 1, payload)...)
	s := &scriptStream{in: in}
	resp, err := NewConn(s).Get("a", "/")
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "data" {
		t.Fatalf("body %q", resp.Body)
	}
}

// dataFrames splits a body into ≤ maxFrameLen DATA frames; the last
// carries END_STREAM.
func dataFrames(sid uint32, body []byte) []byte {
	var out []byte
	for len(body) > 0 {
		n := len(body)
		var flags byte
		if n > maxFrameLen {
			n = maxFrameLen
		} else {
			flags = flagEndStream
		}
		out = append(out, fr(frameDATA, flags, sid, body[:n])...)
		body = body[n:]
	}
	return out
}

// DATA past the half-window mark must return credit on both the
// connection and the stream.
func TestWindowUpdateEmitted(t *testing.T) {
	body := bytes.Repeat([]byte{'x'}, windowInit-8)
	in := append(settings(), fr(frameHEADERS, flagEndHeaders, 1, []byte{0x88})...)
	in = append(in, dataFrames(1, body)...)
	s := &scriptStream{in: in}
	resp, err := NewConn(s).Get("a", "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Body) != len(body) {
		t.Fatalf("body %d", len(resp.Body))
	}
	// Find the two WINDOW_UPDATE frames in the client's output.
	foundConn, foundStream := false, false
	for i := 0; i+13 <= len(s.out); i++ {
		if s.out[i+3] == frameWINDOWUPDATE {
			sid := u31(s.out[i+5 : i+9])
			if sid == 0 {
				foundConn = true
			}
			if sid == 1 {
				foundStream = true
			}
			i += 12
		}
	}
	if !foundConn || !foundStream {
		t.Fatal("missing WINDOW_UPDATE frames")
	}
}

func TestWindowOverflowRefused(t *testing.T) {
	// A WINDOW_UPDATE pushing the connection send window past 2^31-1 is
	// a connection error (RFC 9113 §6.9.1); a zero increment is too.
	in := append(settings(), fr(frameWINDOWUPDATE, 0, 0, []byte{0x7f, 0xff, 0xff, 0xff})...)
	s := &scriptStream{in: in}
	_, err := NewConn(s).Get("a", "/")
	if !errors.Is(err, ErrWindow) {
		t.Fatalf("err %v, want ErrWindow", err)
	}

	in = append(settings(), fr(frameWINDOWUPDATE, 0, 0, []byte{0, 0, 0, 0})...)
	s = &scriptStream{in: in}
	if _, err := NewConn(s).Get("a", "/"); !errors.Is(err, ErrProtocol) {
		t.Fatalf("zero increment err %v", err)
	}
}

func TestSequentialGets(t *testing.T) {
	in := append(settings(), okTranscript("one")[len(settings()):]...)
	in = append(in, fr(frameHEADERS, flagEndHeaders, 3, []byte{0x88})...)
	in = append(in, fr(frameDATA, flagEndStream, 3, []byte("two"))...)
	s := &scriptStream{in: in}
	c := NewConn(s)
	r1, err := c.Get("a", "/1")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := c.Get("a", "/2")
	if err != nil {
		t.Fatal(err)
	}
	if string(r1.Body) != "one" || string(r2.Body) != "two" {
		t.Fatal("bodies")
	}
}

func TestFrameOnForeignStreamRefused(t *testing.T) {
	in := append(settings(), fr(frameDATA, 0, 7, []byte("x"))...)
	s := &scriptStream{in: in}
	_, err := NewConn(s).Get("a", "/")
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("err %v", err)
	}
}
