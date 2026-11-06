package main

import (
	"errors"
	"testing"

	"virelai/h2"
)

// h2Script is the scripted byte stream the codec drives in host tests —
// the same shape h2's own tests use, local because test helpers don't
// cross packages.
type h2Script struct {
	in  []byte
	out []byte
}

func (s *h2Script) Read(p []byte) (int, error) {
	if len(s.in) == 0 {
		return 0, errors.New("script: eof")
	}
	n := copy(p, s.in)
	s.in = s.in[n:]
	return n, nil
}

func (s *h2Script) Write(p []byte) (int, error) {
	s.out = append(s.out, p...)
	return len(p), nil
}

func h2Frame(typ, flags byte, sid uint32, payload []byte) []byte {
	return append([]byte{
		byte(len(payload) >> 16), byte(len(payload) >> 8), byte(len(payload)),
		typ, flags, 0, 0, byte(sid >> 8), byte(sid)}, payload...)
}

// h2Transcript scripts a server SETTINGS plus one :status 200 HEADERS +
// END_STREAM DATA per stream id in order.
func h2Transcript(exchanges map[uint32]string, order []uint32) []byte {
	in := h2Frame(0x4, 0, 0, nil)
	for _, sid := range order {
		in = append(in, h2Frame(0x1, 0x4, sid, []byte{0x88})...)
		in = append(in, h2Frame(0x0, 0x1, sid, []byte(exchanges[sid]))...)
	}
	return in
}

// Sequential exchanges on one pooled h2 connection ride streams 1 and 3
// and come back reusable — the keep-alive shape the gate marker pins.
func TestExchangeH2StreamReuse(t *testing.T) {
	s := &h2Script{in: h2Transcript(map[uint32]string{1: "page", 3: "css"}, []uint32{1, 3})}
	p := &pooledConn{origin: "https://10.0.0.2:8443", h2c: h2.NewConn(s)}
	a := &app{}
	for i, tc := range []struct {
		raw, want string
	}{
		{"https://10.0.0.2:8443/", "page"},
		{"https://10.0.0.2:8443/a.css", "css"},
	} {
		u, ok := parseBrowserURL(tc.raw)
		if !ok {
			t.Fatalf("url %q", tc.raw)
		}
		head, body, _, reusable, kind := a.exchangeH2(p, u, 1024, true)
		if kind != "" {
			t.Fatalf("exchange %d kind %q", i, kind)
		}
		if !reusable {
			t.Fatalf("exchange %d not reusable", i)
		}
		if string(body) != tc.want {
			t.Fatalf("exchange %d body %q", i, body)
		}
		if got := h2StatusOf(head); got != 200 {
			t.Fatalf("exchange %d status %d", i, got)
		}
	}
	if p.h2c.NextStreamID() != 5 {
		t.Fatalf("next stream id %d", p.h2c.NextStreamID())
	}
	// Two request HEADERS frames left the client, on streams 1 and 3 —
	// walked sequentially after the 24-byte client preface.
	seen := map[uint32]bool{}
	for i := 24; i+9 <= len(s.out); {
		n := int(s.out[i])<<16 | int(s.out[i+1])<<8 | int(s.out[i+2])
		if s.out[i+3] == 0x1 {
			sid := uint32(s.out[i+5]&0x7f)<<24 | uint32(s.out[i+6])<<16 |
				uint32(s.out[i+7])<<8 | uint32(s.out[i+8])
			seen[sid] = true
		}
		i += 9 + n
	}
	if !seen[1] || !seen[3] || len(seen) != 2 {
		t.Fatalf("request streams %v", seen)
	}
}

func h2StatusOf(head string) int {
	if len(head) < 10 {
		return 0
	}
	n := 0
	for i := 7; i < len(head) && head[i] >= '0' && head[i] <= '9'; i++ {
		n = n*10 + int(head[i]-'0')
	}
	return n
}

// A PUSH_PROMISE on the negotiated connection names its refusal — the
// kind is h2-refused, never a silent h1 resend.
func TestExchangeH2PushRefused(t *testing.T) {
	pp := h2Frame(0x5, 0x4, 1, []byte{0, 0, 0, 2, 0x88})
	in := append(h2Frame(0x4, 0, 0, nil), pp...)
	p := &pooledConn{h2c: h2.NewConn(&h2Script{in: in})}
	u, ok := parseBrowserURL("https://10.0.0.2:8443/")
	if !ok {
		t.Fatal("url")
	}
	_, _, _, reusable, kind := (&app{}).exchangeH2(p, u, 1024, true)
	if kind != "h2-refused" || reusable {
		t.Fatalf("kind %q reusable %v", kind, reusable)
	}
}
