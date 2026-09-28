// GOHTTPD.ELF — the Go replacement for the Zig HTTPD.BIN (M71l / #1571).
//
// This file is deliberately ABI-free: parsing, routing and formatting live here
// so they run in host tests. The guest wiring (listen, accept, recv, send) is
// in main.go. Nothing here imports virelai/vi.
package main

import (
	"bytes"
	"strconv"
	"strings"
)

// maxRequestBytes bounds the request line + headers buffered before the
// connection is refused. The kernel's TCP seam does not reassemble; this
// application limit stays independent of the larger single-segment bound.
const maxRequestBytes = 1024

// sharePath is the one file this server publishes, and shareTarget is the only
// target that reaches it. The path is pinned rather than derived from the
// request, so no user-supplied string ever reaches the filesystem: the Zig
// server sanitised traversal, a fixed path cannot traverse at all.
const (
	sharePath   = "/host/HTTPD.TXT"
	shareTarget = "/file"
)

type request struct {
	Method string
	Target string
}

type response struct {
	Status      int
	ContentType string
	Body        []byte
}

var statusText = map[int]string{
	200: "OK",
	404: "Not Found",
	405: "Method Not Allowed",
}

func indexBody() []byte {
	return []byte("<!doctype html>\n<html><head><title>VirelaiOS</title></head>\n" +
		"<body><h1>VirelaiOS</h1>\n" +
		"<p><span class=\"badge\">ONLINE &bull; PORT 8080</span></p>\n" +
		"<p><a href=\"/file\">/file</a> &middot; <a href=\"/healthz\">/healthz</a></p>\n" +
		"</body></html>\n")
}

func healthBody() []byte { return []byte("{\"status\":\"ok\",\"port\":8080}\n") }

func notFound() response {
	return response{Status: 404, ContentType: "text/plain; charset=utf-8", Body: []byte("not found\n")}
}

// hasHeaderEnd reports whether the request headers are complete (CRLF or bare
// LF form). An unterminated buffer is incomplete, not malformed.
func hasHeaderEnd(raw []byte) bool {
	return bytes.Contains(raw, []byte("\r\n\r\n")) || bytes.Contains(raw, []byte("\n\n"))
}

// parseRequest reads the first line: METHOD SP TARGET SP HTTP/1.x. It returns
// false for anything that is not a well-formed, absolute-path HTTP/1.x request.
func parseRequest(raw []byte) (request, bool) {
	if !hasHeaderEnd(raw) {
		return request{}, false
	}
	line := raw
	if i := bytes.IndexByte(raw, '\n'); i >= 0 {
		line = raw[:i]
	}
	if n := len(line); n > 0 && line[n-1] == '\r' {
		line = line[:n-1]
	}
	sp1 := bytes.IndexByte(line, ' ')
	if sp1 <= 0 {
		return request{}, false
	}
	rest := line[sp1+1:]
	sp2 := bytes.IndexByte(rest, ' ')
	if sp2 <= 0 {
		return request{}, false
	}
	method := string(line[:sp1])
	target := string(rest[:sp2])
	version := string(rest[sp2+1:])
	if !strings.HasPrefix(version, "HTTP/1.") {
		return request{}, false
	}
	if !strings.HasPrefix(target, "/") {
		return request{}, false
	}
	return request{Method: method, Target: target}, true
}

// route maps a parsed request to a response. pinned is the pinned share file's
// bytes and pinnedOK says whether it was readable at startup.
func route(req request, pinned []byte, pinnedOK bool) response {
	if req.Method != "GET" && req.Method != "HEAD" {
		return response{Status: 405, ContentType: "text/plain; charset=utf-8", Body: []byte("method not allowed\n")}
	}
	switch req.Target {
	case "/":
		return response{Status: 200, ContentType: "text/html; charset=utf-8", Body: indexBody()}
	case "/healthz":
		return response{Status: 200, ContentType: "application/json", Body: healthBody()}
	case shareTarget:
		if !pinnedOK {
			return notFound()
		}
		return response{Status: 200, ContentType: mimeForPath(sharePath), Body: pinned}
	default:
		return notFound()
	}
}

func mimeForPath(path string) string {
	i := strings.LastIndexByte(path, '.')
	if i < 0 {
		return "application/octet-stream"
	}
	switch strings.ToLower(path[i:]) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".qoi":
		return "image/qoi"
	default:
		return "application/octet-stream"
	}
}

// formatResponse serialises a response. Content-Length always describes the
// entity, so a HEAD reply carries the length a GET would have produced.
func formatResponse(r response, headOnly bool) []byte {
	text, ok := statusText[r.Status]
	if !ok {
		text = "Status"
	}
	var b []byte
	b = append(b, "HTTP/1.1 "...)
	b = append(b, strconv.Itoa(r.Status)...)
	b = append(b, ' ')
	b = append(b, text...)
	b = append(b, "\r\nServer: VirelaiOS\r\nContent-Type: "...)
	b = append(b, r.ContentType...)
	b = append(b, "\r\nContent-Length: "...)
	b = append(b, strconv.Itoa(len(r.Body))...)
	b = append(b, "\r\nConnection: close\r\n\r\n"...)
	if !headOnly {
		b = append(b, r.Body...)
	}
	return b
}
