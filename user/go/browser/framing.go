package main

import (
	"strings"

	"virelai/webrender"
	"virelai/webstyle"
)

const maxHTTPHeaders = 16384
const maxHTTPLine = 4096
const maxHTTPFields = 128
const maxPageRequests = 25

type responseFrame struct {
	head, err string
	offset    int
	need      int // -1 means EOF framing
	complete  bool
}

func parseResponseFrame(raw []byte) responseFrame {
	head, _, complete := webrender.SplitHTTPResponse(raw)
	if !complete {
		if len(raw) > maxHTTPHeaders {
			return responseFrame{err: "http-header-limit", complete: true}
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if len(strings.TrimRight(line, "\r")) > maxHTTPLine {
				return responseFrame{err: "http-header-limit", complete: true}
			}
		}
		return responseFrame{}
	}
	f := responseFrame{head: head, offset: len(head), need: -1, complete: true}
	if len(head) > maxHTTPHeaders {
		f.err = "http-header-limit"
		return f
	}
	lines := strings.Split(strings.TrimRight(head, "\r\n"), "\n")
	if len(lines) == 0 || len(lines[0]) > maxHTTPLine ||
		!(strings.HasPrefix(lines[0], "HTTP/1.0 ") || strings.HasPrefix(lines[0], "HTTP/1.1 ")) ||
		webrender.HTTPStatus(head) < 100 || !validStatusLine(strings.TrimSuffix(lines[0], "\r")) {
		f.err = "http-framing-invalid"
		return f
	}
	if len(lines)-1 > maxHTTPFields {
		f.err = "http-header-limit"
		return f
	}
	for _, rawLine := range lines[1:] {
		line := strings.TrimSuffix(rawLine, "\r")
		i := strings.IndexByte(line, ':')
		if i <= 0 || len(line) > maxHTTPLine || strings.ContainsAny(line, "\x00\r") ||
			!validFieldName(line[:i]) {
			f.err = "http-framing-invalid"
			return f
		}
		name, value := strings.ToLower(line[:i]), strings.TrimSpace(line[i+1:])
		switch name {
		case "transfer-encoding":
			f.err = "http-framing-unsupported"
			return f
		case "content-encoding":
			if !strings.EqualFold(value, "identity") {
				f.err = "http-framing-unsupported"
				return f
			}
		case "content-length":
			if f.need >= 0 || value == "" {
				f.err = "http-framing-invalid"
				return f
			}
			n := 0
			for _, c := range value {
				if c < '0' || c > '9' || n > (int(^uint(0)>>1)-int(c-'0'))/10 {
					f.err = "http-framing-invalid"
					return f
				}
				n = n*10 + int(c-'0')
			}
			f.need = n
		}
	}
	return f
}

func validStatusLine(line string) bool {
	if len(line) < 12 {
		return false
	}
	for _, c := range line {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return line[9] >= '0' && line[9] <= '9' && line[10] >= '0' && line[10] <= '9' &&
		line[11] >= '0' && line[11] <= '9' && (len(line) == 12 || line[12] == ' ')
}

func validFieldName(name string) bool {
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return name != ""
}

func formatPageRequest(u webrender.URL, cookie string) string {
	if strings.ContainsAny(cookie, "\x00\r\n") || len(cookie)+8 > maxHTTPLine {
		cookie = "" // an edited ledger cannot inject a request header
	}
	head := strings.TrimSuffix(webrender.FormatGetRequestWithCookies(urlAuthority(u), u.Path, cookie), "\r\n")
	return head + "Connection: close\r\nAccept-Encoding: identity\r\n\r\n"
}

func responseBody(f responseFrame, raw []byte, capn int, eof bool) ([]byte, bool, string) {
	if f.err != "" {
		return nil, false, f.err
	}
	if !f.complete {
		if eof {
			return nil, false, "http-premature-eof"
		}
		return nil, false, ""
	}
	body := raw[f.offset:]
	if f.need >= 0 {
		if f.need > capn && len(body) >= capn {
			return body[:capn], true, ""
		}
		if len(body) >= f.need {
			return body[:f.need], false, ""
		}
		if eof {
			return nil, false, "http-premature-eof"
		}
		return nil, false, ""
	}
	if len(body) > capn {
		return body[:capn], true, ""
	}
	if eof {
		return body, false, ""
	}
	return nil, false, ""
}

// responseNeed retains the pre-existing host-test framing contract. Production
// fetchers use the structured parser to preserve full named errors.
func responseNeed(raw []byte) (int, bool) {
	f := parseResponseFrame(raw)
	if !f.complete {
		return 0, false
	}
	if f.err != "" || f.need > webstyle.MaxHTMLBytes {
		return -2, true
	}
	return f.need, true
}
