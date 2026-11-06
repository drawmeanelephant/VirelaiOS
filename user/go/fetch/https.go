package main

import (
	"virelai/h2"
	"virelai/httpc"
	"virelai/tls"
	"virelai/vi"
)

// The windowed TLS consumer is a one-shot fetch: it speaks the same
// HTTP/1.1 request shape as the cleartext path (M135b), reads the
// response through the shared engine (Content-Length, chunked, gzip),
// and closes when the body is complete — a keep-alive-armed request is
// still honest here because the framing is real, not EOF-assumed.
// M212a (#3872) adds the --http2 mode: ALPN offers h2,http/1.1 and the
// negotiated pick selects the h2 codec or stays on the h1 engine.

func httpsDial(addr string, port uint16, sni string, http2 bool) (*tls.TLSConn, error) {
	if !http2 {
		return tls.Dial(addr, port, sni)
	}
	return tls.DialALPN(addr, port, sni, []string{"h2", "http/1.1"})
}

type tlsSource struct{ c *tls.TLSConn }

func (s tlsSource) Read(p []byte) (int, error) { return s.c.Read(p) }

func httpsGetOn(c *tls.TLSConn, sni, path string) ([]byte, error) {
	host := sni
	if host == "" {
		host = "localhost"
	}
	req := httpc.GetRequest(host, path, "")
	if _, err := c.Write([]byte(req)); err != nil {
		return nil, err
	}
	_, _, body, _, _, rerr := httpc.ReadResponse(tlsSource{c}, fetchPolicy)
	if rerr != "" {
		return nil, errStr(rerr)
	}
	return body, nil
}

// h2GetOn runs the M212a GET exchange over the negotiated h2 stream:
// :authority is the SNI host (plus a non-default port, the Host-header
// spelling), and the body bound matches the h1 policy's cap.
func h2GetOn(c *tls.TLSConn, sni string, port uint16, path string) ([]byte, error) {
	authority := sni
	if authority == "" {
		authority = "localhost"
	}
	if port != DefaultHTTPSPort {
		authority += ":" + portString(port)
	}
	hc := h2.NewConn(c)
	hc.MaxBody = vi.MaxFileBytes
	resp, err := hc.Get(authority, path)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
