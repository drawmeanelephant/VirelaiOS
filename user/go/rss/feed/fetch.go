package feed

import (
	"errors"
	"strconv"
	"strings"

	"virelai/tls"
	"virelai/vi"
)

// Fetch-layer limits, all bounded on purpose: the guest is a modest 2-vCPU VM
// and vi.MaxFileBytes caps a single file load at 256 KiB.
const (
	// MaxBodyBytes bounds one feed document. Feeds larger than this are
	// truncated rather than allowed to grow the heap without limit.
	MaxBodyBytes = 256 * 1024
	// readChunk is the per-read buffer; TCPPayloadMax is 1460, so a larger
	// buffer simply amortises the calls without changing the wire shape.
	readChunk = 8192
	// maxRedirects bounds a redirect chain. Past this the peer is looping
	// or the document is not where the subscription said it was.
	maxRedirects = 5
)

// Errors the UI can name.
var (
	ErrNetwork = errors.New("feed: network error")
	ErrTLS     = errors.New("feed: TLS validation failed")
	ErrStatus  = errors.New("feed: HTTP status error")
	ErrTooBig  = errors.New("feed: response too large")
)

// HTTPError carries the status code so the UI can say "HTTP 404" precisely.
type HTTPError struct{ Status int }

func (e HTTPError) Error() string { return "feed: HTTP " + strconv.Itoa(e.Status) }

// stream is the common read/write shape of a plain and a TLS connection, so
// the request path below is written once.
type stream interface {
	Write(p []byte) (int, error)
	Read(p []byte) (int, error)
	Close() error
}

// connStream adapts vi.Conn's Send/Recv onto the io-style shape.
type connStream struct{ c *vi.Conn }

func (s connStream) Write(p []byte) (int, error) { return s.c.Send(p) }

func (s connStream) Read(p []byte) (int, error) {
	n, err := s.c.Recv(p)
	if err != nil {
		return n, err
	}
	return n, nil
}

func (s connStream) Close() error { return s.c.Close() }

// Dial seams. They are package variables so a host test can substitute a fake
// transport and exercise the request/response split without a guest.
var (
	dialPlain = func(host string, port uint16) (stream, error) {
		c, err := vi.Dial(host, port)
		if err != nil {
			return nil, err
		}
		return connStream{c: c}, nil
	}
	dialTLS = func(host string, port uint16, sni string) (stream, error) {
		return tls.Dial(host, port, sni)
	}
)

// Fetch classifies raw, retrieves it over the correct transport, and parses
// the body. It returns the raw document alongside the parsed feed so a caller
// (or a test) can compare parsed fields against the original bytes.
//
// Redirects (301, 302, 303, 307, 308) are followed, up to maxRedirects, and
// an https URL is never retried in the clear. The body is decoded when the
// response is chunked; Content-Length trims a delimited body.
func Fetch(raw string) (*Feed, []byte, error) {
	u := Classify(raw)
	if u.Scheme == SchemeInvalid {
		return nil, nil, errors.New("feed: unusable URL (need http:// or https://)")
	}
	seen := map[string]bool{}
	var last []byte
	for hop := 0; hop <= maxRedirects; hop++ {
		key := u.Raw
		if key == "" {
			key = formatURL(u)
		}
		if seen[key] {
			return nil, last, errors.New("feed: redirect loop")
		}
		seen[key] = true
		resp, err := get(u, MaxBodyBytes)
		if err != nil {
			return nil, nil, err
		}
		status, doc, err := splitResponse(resp)
		if err != nil {
			return nil, nil, err
		}
		head, _, _ := splitHead(resp)
		if redirectStatus(status) {
			if hop == maxRedirects {
				return nil, doc, errors.New("feed: too many redirects")
			}
			next, err := resolveRedirect(u, headerValue(head, "location"))
			if err != nil {
				return nil, doc, err
			}
			vi.ConsoleLine("feed: redirect " + formatURL(next))
			u = next
			last = doc
			continue
		}
		doc, err = frameBody(head, doc)
		if err != nil {
			return nil, nil, err
		}
		last = doc
		if status < 200 || status > 299 {
			return nil, doc, HTTPError{Status: status}
		}
		f, err := Parse(doc)
		if err != nil {
			return nil, doc, err
		}
		return f, doc, nil
	}
	return nil, last, errors.New("feed: too many redirects")
}

// get dials, sends one HTTP/1.1 GET, and reads the whole response up to max
// bytes. A TLS handshake failure is reported as ErrTLS, never retried in the
// clear.
func get(u URL, max int) ([]byte, error) {
	var (
		s   stream
		err error
	)
	if u.Scheme == SchemeHTTPS {
		s, err = dialTLS(u.Host, u.Port, u.Host)
		if err != nil {
			return nil, wrapTLSErr(err)
		}
	} else {
		s, err = dialPlain(u.Host, u.Port)
		if err != nil {
			return nil, wrapNetErr(err)
		}
	}
	defer s.Close()

	req := formatRequest(u)
	if _, err := s.Write([]byte(req)); err != nil {
		return nil, wrapNetErr(err)
	}
	return readAll(s, max)
}

// readAll drains s up to max bytes. Hitting the cap is not an error: it is a
// deliberate bound, and the caller parses what arrived.
func readAll(s stream, max int) ([]byte, error) {
	buf := make([]byte, readChunk)
	out := make([]byte, 0, readChunk)
	for len(out) < max {
		n, err := s.Read(buf)
		if n > 0 {
			room := max - len(out)
			if n > room {
				n = room
			}
			out = append(out, buf[:n]...)
		}
		if err != nil || n == 0 {
			if len(out) > 0 {
				return out, nil
			}
			return out, wrapNetErr(err)
		}
	}
	return out, nil
}

// formatRequest is the one HTTP/1.1 GET this client sends. Host carries the
// port when it is not the scheme default: a server on :18099 that keys off
// Host cannot see the port if it is omitted.
func formatRequest(u URL) string {
	return "GET " + u.Path + " HTTP/1.1\r\n" +
		"Host: " + hostHeader(u) + "\r\n" +
		"User-Agent: VirelaiRSS/1.0\r\n" +
		"Accept: application/rss+xml, application/atom+xml, application/xml, text/xml, */*\r\n" +
		"Connection: close\r\n\r\n"
}

func hostHeader(u URL) string {
	if u.Port == 0 || u.Port == u.DefaultPort() {
		return u.Host
	}
	return u.Host + ":" + strconv.Itoa(int(u.Port))
}

func formatURL(u URL) string {
	return string(u.Scheme) + "://" + hostHeader(u) + u.Path
}

// splitResponse separates status line and headers from the body. It is a pure
// helper so a host test can pin the split against a captured response without
// a socket. An empty body after the header block is legal.
func splitResponse(resp []byte) (int, []byte, error) {
	head, body, err := splitHead(resp)
	if err != nil {
		return 0, nil, err
	}
	code, err := statusCode(head)
	if err != nil {
		return 0, nil, err
	}
	return code, body, nil
}

func splitHead(resp []byte) (head string, body []byte, err error) {
	i := strings.Index(string(resp), "\r\n\r\n")
	sep := 4
	if i < 0 {
		i = strings.Index(string(resp), "\n\n")
		sep = 2
		if i < 0 {
			return "", nil, errors.New("feed: response has no header terminator")
		}
	}
	return string(resp[:i]), resp[i+sep:], nil
}

func statusCode(head string) (int, error) {
	line := head
	if nl := strings.IndexAny(head, "\r\n"); nl >= 0 {
		line = head[:nl]
	}
	parts := strings.SplitN(strings.TrimSpace(line), " ", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "HTTP/") {
		return 0, errors.New("feed: malformed status line")
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, errors.New("feed: malformed status line")
	}
	return code, nil
}

func headerValue(head, name string) string {
	for _, ln := range strings.Split(head, "\n") {
		ln = strings.TrimRight(ln, "\r")
		i := strings.IndexByte(ln, ':')
		if i <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(ln[:i]), name) {
			return strings.TrimSpace(ln[i+1:])
		}
	}
	return ""
}

func redirectStatus(code int) bool {
	switch code {
	case 301, 302, 303, 307, 308:
		return true
	}
	return false
}

// resolveRedirect turns a Location into the next request. An absolute URL is
// reclassified. A root-relative or relative Location stays on the same host
// and scheme. https never becomes http.
func resolveRedirect(from URL, location string) (URL, error) {
	loc := strings.TrimSpace(location)
	if loc == "" {
		return URL{}, errors.New("feed: redirect missing Location")
	}
	if strings.HasPrefix(loc, "//") {
		loc = string(from.Scheme) + ":" + loc
	}
	var next URL
	lower := strings.ToLower(loc)
	switch {
	case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
		next = Classify(loc)
	case strings.HasPrefix(loc, "/"):
		next = from
		next.Path = loc
		next.Raw = formatURL(next)
	default:
		dir := from.Path
		if q := strings.IndexAny(dir, "?#"); q >= 0 {
			dir = dir[:q]
		}
		if i := strings.LastIndex(dir, "/"); i >= 0 {
			dir = dir[:i+1]
		} else {
			dir = "/"
		}
		next = from
		next.Path = dir + loc
		next.Raw = formatURL(next)
	}
	if next.Scheme == SchemeInvalid {
		return URL{}, errors.New("feed: redirect target is not http(s)")
	}
	if from.Scheme == SchemeHTTPS && next.Scheme != SchemeHTTPS {
		return URL{}, errors.New("feed: refusing to follow an https redirect in the clear")
	}
	if next.Raw == "" {
		next.Raw = formatURL(next)
	}
	return next, nil
}

// frameBody applies Transfer-Encoding and Content-Length. Chunked wins when
// both are present. A short body is kept: the read is already bounded.
func frameBody(head string, body []byte) ([]byte, error) {
	if chunked(head) {
		return decodeChunked(body)
	}
	cl := headerValue(head, "content-length")
	if cl == "" {
		return body, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(cl))
	if err != nil || n < 0 {
		return nil, errors.New("feed: bad Content-Length")
	}
	if n > len(body) {
		return body, nil
	}
	return body[:n], nil
}

func chunked(head string) bool {
	return strings.Contains(strings.ToLower(headerValue(head, "transfer-encoding")), "chunked")
}

func decodeChunked(body []byte) ([]byte, error) {
	rest := body
	var out []byte
	for {
		line, next, ok := readLine(rest)
		if !ok {
			return nil, errors.New("feed: truncated chunk size")
		}
		rest = next
		if semi := strings.IndexByte(line, ';'); semi >= 0 {
			line = line[:semi]
		}
		line = strings.TrimSpace(line)
		n, err := strconv.ParseUint(line, 16, 32)
		if err != nil {
			return nil, errors.New("feed: bad chunk size")
		}
		if n == 0 {
			return out, nil
		}
		if uint64(len(rest)) < n {
			return nil, errors.New("feed: truncated chunk")
		}
		out = append(out, rest[:n]...)
		rest = rest[n:]
		_, next, ok = readLine(rest)
		if !ok {
			return nil, errors.New("feed: chunk missing CRLF")
		}
		rest = next
	}
}

func readLine(b []byte) (line string, rest []byte, ok bool) {
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			return strings.TrimRight(string(b[:i]), "\r"), b[i+1:], true
		}
	}
	return "", b, false
}

func wrapNetErr(err error) error {
	if err == nil {
		return nil
	}
	return errJoined(ErrNetwork, err)
}

func wrapTLSErr(err error) error {
	if err == nil {
		return nil
	}
	return errJoined(ErrTLS, err)
}

// errJoined keeps errors.Is working for both the sentinel and the cause while
// rendering as one readable line in the TUI.
type joined struct{ a, b error }

func (e joined) Error() string { return e.a.Error() + ": " + e.b.Error() }
func (e joined) Unwrap() []error {
	return []error{e.a, e.b}
}

func errJoined(a, b error) error { return joined{a: a, b: b} }
