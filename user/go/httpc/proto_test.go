package httpc

import (
	"bytes"
	"strings"
	"testing"

	"virelai/h2"
	"virelai/webrender"
)

// The ALPN pick decides the wire engine: h2 explicit, http/1.1 or no
// extension stays h1, anything else fails closed rather than guessing.
func TestSelect(t *testing.T) {
	for _, tc := range []struct {
		alpn  string
		proto Proto
		ok    bool
	}{
		{"h2", ProtoH2, true},
		{"http/1.1", ProtoH1, true},
		{"", ProtoH1, true},
		{"h3", ProtoH1, false},
		{"http/2", ProtoH1, false},
	} {
		proto, ok := Select(tc.alpn)
		if proto != tc.proto || ok != tc.ok {
			t.Fatalf("Select(%q) = %v,%v want %v,%v", tc.alpn, proto, ok, tc.proto, tc.ok)
		}
	}
}

// A completed exchange renders the synthesized head the consumers'
// grammar reads — status, location, cookies — and hands the body back.
func TestH2ResultHead(t *testing.T) {
	resp := &h2.Response{
		Status: 200,
		Headers: []h2.Field{
			{Name: ":status", Value: "200"},
			{Name: "location", Value: "/next"},
			{Name: "set-cookie", Value: "a=b"},
			{Name: "content-length", Value: "5"},
		},
		Body: []byte("hello"),
	}
	head, body, trunc, err := H2Result(resp, 1024)
	if err != "" {
		t.Fatalf("err %q", err)
	}
	if trunc || string(body) != "hello" {
		t.Fatalf("body %q trunc %v", body, trunc)
	}
	if webrender.HTTPStatus(head) != 200 {
		t.Fatalf("HTTPStatus(%q)", head)
	}
	if webrender.LocationHeader(head) != "/next" {
		t.Fatalf("LocationHeader(%q)", head)
	}
	if !strings.Contains(head, "set-cookie: a=b") {
		t.Fatalf("head %q missing cookie line", head)
	}
	if strings.Contains(head, ":status") {
		t.Fatalf("pseudo-field leaked into head %q", head)
	}
}

// A gzip content-coding decodes through the shared Decode path.
func TestH2ResultGzip(t *testing.T) {
	resp := &h2.Response{
		Status: 200,
		Headers: []h2.Field{{Name: "content-encoding", Value: "gzip"}},
		Body:   gzipBytes(t, []byte("zipped")),
	}
	_, body, _, err := H2Result(resp, 1024)
	if err != "" {
		t.Fatalf("err %q", err)
	}
	if string(body) != "zipped" {
		t.Fatalf("body %q", body)
	}
}

// Every malformed shape the h1 grammar refuses is refused by the same
// name on the h2 path.
func TestH2ResultRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields []h2.Field
		body   string
		err    string
	}{
		{"uppercase", []h2.Field{{Name: "Content-Length", Value: "1"}}, "x", FramingInvalid},
		{"crlf value", []h2.Field{{Name: "x-a", Value: "1\r\nInjected: y"}}, "x", FramingInvalid},
		{"hop conn", []h2.Field{{Name: "connection", Value: "close"}}, "x", FramingInvalid},
		{"hop te", []h2.Field{{Name: "transfer-encoding", Value: "chunked"}}, "x", FramingInvalid},
		{"te non-trailers", []h2.Field{{Name: "te", Value: "gzip"}}, "x", FramingUnsupported},
		{"ce other", []h2.Field{{Name: "content-encoding", Value: "br"}}, "x", FramingUnsupported},
		{"dup ce", []h2.Field{{Name: "content-encoding", Value: "identity"}, {Name: "content-encoding", Value: "gzip"}}, "x", FramingInvalid},
		{"dup cl", []h2.Field{{Name: "content-length", Value: "1"}, {Name: "content-length", Value: "1"}}, "x", FramingInvalid},
		{"cl mismatch", []h2.Field{{Name: "content-length", Value: "9"}}, "x", FramingInvalid},
		{"cl junk", []h2.Field{{Name: "content-length", Value: "1x"}}, "x", FramingInvalid},
	} {
		resp := &h2.Response{Status: 200, Headers: tc.fields, Body: []byte(tc.body)}
		_, _, _, err := H2Result(resp, 1024)
		if err != tc.err {
			t.Fatalf("%s: err %q want %q", tc.name, err, tc.err)
		}
	}
	// "te: trailers" is the one legal TE spelling.
	resp := &h2.Response{Status: 200, Headers: []h2.Field{{Name: "te", Value: "trailers"}}, Body: []byte("x")}
	if _, _, _, err := H2Result(resp, 1024); err != "" {
		t.Fatalf("te trailers err %q", err)
	}
	// The decoded cap clips a plain body, names the gzip bomb.
	resp = &h2.Response{Status: 200, Body: bytes.Repeat([]byte{'x'}, 32)}
	if _, body, trunc, err := H2Result(resp, 8); err != "" || !trunc || len(body) != 8 {
		t.Fatalf("cap: body %d trunc %v err %q", len(body), trunc, err)
	}
}
