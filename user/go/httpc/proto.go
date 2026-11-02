package httpc

import (
	"strconv"
	"strings"

	"virelai/h2"
)

// Proto is the negotiated wire engine — the ALPN pick, verbatim.
type Proto int

const (
	// ProtoH1 is the HTTP/1.1 wire engine this package has always been.
	ProtoH1 Proto = iota
	// ProtoH2 is the M212a HTTP/2 codec — selected only by an explicit
	// ALPN "h2" pick, never assumed.
	ProtoH2
)

// Select maps a negotiated ALPN name to the wire engine (M212b, #3873):
// "h2" picks the codec, "http/1.1" or "" (the peer answered no extension)
// picks the h1 engine unchanged. Any other pick cannot legitimately
// arrive — the TLS layer refuses an unoffered protocol by name — and is
// reported ok=false rather than silently downgraded: a caller that sees
// an unknown negotiated protocol must refuse, not guess.
func Select(alpn string) (Proto, bool) {
	switch alpn {
	case "h2":
		return ProtoH2, true
	case "http/1.1", "":
		return ProtoH1, true
	}
	return ProtoH1, false
}

// H2Result turns one completed h2 exchange into the consumer's response
// shape: the head text the status/location/cookie grammar reads (a
// synthesized "HTTP/2 <status>" head with the fields re-lined), and the
// body under the shared content-coding cap — gzip decodes through the
// same Decode path, so the refusal grammar cannot diverge between the
// two engines.
//
// The h2 wire still carries the HTTP semantics: a field that could not
// be an HTTP/1.x field (uppercase name, control bytes), a hop-by-hop
// field the h2 grammar forbids (RFC 9113 §8.2.2), a second coding
// declaration, or a content-length that disagrees with the DATA sum is
// refused by the same names the h1 head parser uses.
func H2Result(resp *h2.Response, capn int) (head string, body []byte, truncated bool, err string) {
	var h Head
	h.Status, h.Length = resp.Status, -1
	h.NoBody = resp.Status >= 100 && resp.Status < 200 || resp.Status == 204 || resp.Status == 304
	var b strings.Builder
	// "HTTP/2 <status>" — HTTPStatus reads the "HTTP/" prefix and three
	// digits; consumers never branch on the version token itself.
	b.WriteString("HTTP/2 ")
	b.WriteString(strconv.Itoa(resp.Status))
	b.WriteString("\r\n")
	clSeen, ceSeen := false, false
	cl := -1
	for _, f := range resp.Headers {
		if f.Name == "" || f.Name[0] == ':' {
			continue // pseudo-fields are codec state, not head lines
		}
		if !validFieldName(f.Name) || hasUpper(f.Name) || badFieldValue(f.Value) {
			return "", nil, false, FramingInvalid
		}
		switch f.Name {
		case "connection", "keep-alive", "proxy-connection", "transfer-encoding", "upgrade":
			return "", nil, false, FramingInvalid
		case "te":
			if !strings.EqualFold(f.Value, "trailers") {
				return "", nil, false, FramingUnsupported
			}
			continue
		case "content-encoding":
			if ceSeen {
				return "", nil, false, FramingInvalid
			}
			ceSeen = true
			switch {
			case strings.EqualFold(f.Value, "identity"):
			case strings.EqualFold(f.Value, "gzip"):
				h.Gzip = true
			default:
				return "", nil, false, FramingUnsupported
			}
		case "content-length":
			if clSeen || f.Value == "" {
				return "", nil, false, FramingInvalid
			}
			clSeen = true
			n, cerr := strconv.Atoi(f.Value)
			if cerr != nil || n < 0 {
				return "", nil, false, FramingInvalid
			}
			cl = n
		}
		b.WriteString(f.Name)
		b.WriteString(": ")
		b.WriteString(f.Value)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	if clSeen && !h.NoBody && cl != len(resp.Body) {
		return "", nil, false, FramingInvalid
	}
	body, truncated, err = Decode(h, resp.Body, capn)
	if err != "" {
		return "", nil, false, err
	}
	return b.String(), body, truncated, ""
}

// hasUpper reports whether a field name contains an uppercase letter —
// h2 fields are lowercase by definition (RFC 9113 §8.2.1), so one is a
// framing violation, not a style nit.
func hasUpper(name string) bool {
	for _, c := range name {
		if c >= 'A' && c <= 'Z' {
			return true
		}
	}
	return false
}

// badFieldValue refuses the control bytes that could forge a head line
// in the synthesized head text.
func badFieldValue(value string) bool {
	return strings.ContainsAny(value, "\x00\r\n")
}
