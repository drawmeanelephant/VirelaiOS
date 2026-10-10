// Package webrender is VirelaiOS's own HTML rendering library, written in Go
// for the in-guest browser (WEB.ELF). It parses a practical HTML subset,
// resolves a single compiled-in UA style table (no CSS cascade), lays out
// block + inline content, and paints through a caller-supplied Surface.
//
// Nothing here wraps an existing engine: the parser, the style table, the
// layout rules and the paint/span emission are this project's own code. The
// design follows the in-guest Zig renderer's rules (ADR 0028): no JavaScript,
// no cascade, unknown elements degrade to their text, static caps everywhere.
package webrender

import "strings"

// URL is a parsed HTTP(S) URL.
type URL struct {
	Scheme string // "http" or "https"
	Host   string
	Port   uint16
	Path   string
	IPv4   [4]byte
	IsIP   bool
}

// IsHTTPURL reports whether s starts with http:// (case-insensitive).
func IsHTTPURL(s string) bool { return len(s) >= 7 && strings.EqualFold(s[:7], "http://") }

// IsHTTPSURL reports whether s starts with https:// (case-insensitive).
func IsHTTPSURL(s string) bool { return len(s) >= 8 && strings.EqualFold(s[:8], "https://") }

// IsExternalURL reports whether s is an absolute URL the browser cannot load
// from the local file channel.
func IsExternalURL(s string) bool {
	low := strings.ToLower(s)
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "mailto:")
}

// ParseHTTPURL parses "http://host[:port][/path]". Port defaults to 80.
func ParseHTTPURL(raw string) (URL, bool) {
	if !IsHTTPURL(raw) {
		return URL{}, false
	}
	return parseHostPortPath(raw[7:], "http", 80)
}

// ParseURL parses http:// or https://. HTTPS defaults to port 443.
func ParseURL(raw string) (URL, bool) {
	if IsHTTPSURL(raw) {
		return parseHostPortPath(raw[8:], "https", 443)
	}
	return ParseHTTPURL(raw)
}

func parseHostPortPath(rest, scheme string, defPort uint16) (URL, bool) {
	if rest == "" {
		return URL{}, false
	}
	hostEnd := strings.IndexAny(rest, "/:")
	if hostEnd < 0 {
		hostEnd = len(rest)
	}
	if hostEnd == 0 {
		return URL{}, false
	}
	u := URL{Scheme: scheme, Port: defPort}
	u.Host = rest[:hostEnd]
	pathAt := hostEnd
	if hostEnd < len(rest) && rest[hostEnd] == ':' {
		i := hostEnd + 1
		var n uint32
		digits := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			n = n*10 + uint32(rest[i]-'0')
			digits++
			if n > 65535 || digits > 5 {
				return URL{}, false
			}
			i++
		}
		if digits == 0 {
			return URL{}, false
		}
		u.Port = uint16(n)
		pathAt = i
	}
	if pathAt >= len(rest) {
		u.Path = "/"
	} else {
		u.Path = rest[pathAt:]
	}
	if u.Path == "" {
		u.Path = "/"
	}
	if ip, ok := ParseIPv4(u.Host); ok {
		u.IPv4 = ip
		u.IsIP = true
	}
	return u, true
}

// ParseIPv4 parses a dotted quad. Rejects anything that is not exactly four
// 0..255 decimal octets.
func ParseIPv4(text string) ([4]byte, bool) {
	var out [4]byte
	part := 0
	cur := 0
	digits := 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c >= '0' && c <= '9':
			cur = cur*10 + int(c-'0')
			if cur > 255 {
				return out, false
			}
			digits++
			if digits > 3 {
				return out, false
			}
		case c == '.':
			if digits == 0 || part >= 3 {
				return out, false
			}
			out[part] = byte(cur)
			part++
			cur = 0
			digits = 0
		default:
			return out, false
		}
	}
	if digits == 0 || part != 3 {
		return out, false
	}
	out[3] = byte(cur)
	return out, true
}

// IPv4ToU32 packs an IPv4 address the way sys_tcp_connect expects it
// (ip[0]<<24 | ip[1]<<16 | ip[2]<<8 | ip[3] — see user/src/lib/html/url.zig).
func IPv4ToU32(ip [4]byte) uint32 {
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

// DirName returns the directory part of a document path ("/host/PAGE.HTML"
// -> "/host"); a bare name yields ".".
func DirName(path string) string {
	if path == "" {
		return "."
	}
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return "."
	}
	if i == 0 {
		return "/"
	}
	return path[:i]
}

// ResolveHref resolves href against the directory of basePath. Fragments are
// stripped; external URLs pass through unchanged; a leading '/' is rooted at
// "/". Returns ("", false) when the href is empty after fragment stripping.
func ResolveHref(basePath, href string) (string, bool) {
	target := href
	if h := strings.IndexByte(target, '#'); h >= 0 {
		target = target[:h]
	}
	if target == "" {
		return "", false
	}
	if IsExternalURL(target) {
		return target, true
	}
	if target[0] == '/' {
		return target, true
	}
	dir := DirName(basePath)
	rest := target
	for strings.HasPrefix(rest, "./") {
		rest = rest[2:]
	}
	for strings.HasPrefix(rest, "../") {
		dir = DirName(dir)
		rest = rest[3:]
	}
	if rest == "" {
		return dir, true
	}
	if dir == "." {
		return rest, true
	}
	if strings.HasSuffix(dir, "/") {
		return dir + rest, true
	}
	return dir + "/" + rest, true
}

// FormatGetRequest builds the HTTP/1.0 request the browser sends (the same
// shape the Zig DOC.BIN fetch path uses, so the host test responder and the
// gate's --net-tcp-respond see a byte-identical request line).
func FormatGetRequest(host, path string) string {
	p := path
	if p == "" {
		p = "/"
	}
	return "GET " + p + " HTTP/1.0\r\nHost: " + host + "\r\nUser-Agent: VirelaiOS/1.0\r\n\r\n"
}

// HTTPStatus extracts the status code from an HTTP/1.x response head.
// Returns 0 when the head does not start with a valid status line.
func HTTPStatus(head string) int {
	nl := strings.IndexByte(head, '\n')
	line := head
	if nl >= 0 {
		line = head[:nl]
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "HTTP/") {
		return 0
	}
	sp := strings.IndexByte(line, ' ')
	if sp < 0 {
		return 0
	}
	rest := strings.TrimSpace(line[sp+1:])
	code := 0
	digits := 0
	for i := 0; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			break
		}
		code = code*10 + int(rest[i]-'0')
		digits++
	}
	if digits != 3 {
		return 0
	}
	return code
}

// SplitHTTPResponse splits a raw HTTP/1.x response into head and body,
// handling the optional Content-Length framing. The in-guest responder
// closes the connection after the body, so a missing length is tolerated.
func SplitHTTPResponse(raw []byte) (head string, body []byte, ok bool) {
	i := indexHeaderEnd(raw)
	if i < 0 {
		return "", nil, false
	}
	head = string(raw[:i])
	body = raw[i:]
	// The header terminator itself is \r\n\r\n (or \n\n); indexHeaderEnd
	// returns the offset of the body.
	return head, body, true
}

func indexHeaderEnd(raw []byte) int {
	for i := 0; i+1 < len(raw); i++ {
		if raw[i] == '\n' && raw[i+1] == '\n' {
			return i + 2
		}
		if i+3 < len(raw) && raw[i] == '\r' && raw[i+1] == '\n' && raw[i+2] == '\r' && raw[i+3] == '\n' {
			return i + 4
		}
	}
	return -1
}

// LocationHeader extracts the Location header value from a response head
// ("" when absent). The comparison is case-insensitive on the field name.
func LocationHeader(head string) string {
	lines := strings.Split(head, "\n")
	for _, ln := range lines[1:] {
		ln = strings.TrimRight(ln, "\r")
		i := strings.IndexByte(ln, ':')
		if i <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(ln[:i]), "location") {
			return strings.TrimSpace(ln[i+1:])
		}
	}
	return ""
}

// ResolveRedirect resolves a Location value against the URL that produced it.
// A Location may be absolute (http(s)://...), root-relative (/x), or relative
// (x, ../x). Returns ok=false when the result is not an http(s) URL we can use.
func ResolveRedirect(from URL, location string) (URL, bool) {
	if location == "" {
		return URL{}, false
	}
	if u, ok := ParseURL(location); ok {
		return u, true
	}
	if IsHTTPURL(location) {
		return ParseHTTPURL(location)
	}
	if strings.HasPrefix(location, "//") {
		// Protocol-relative: keep the scheme we already have.
		if from.Scheme == "https" {
			return ParseURL("https:" + location)
		}
		return ParseHTTPURL("http:" + location)
	}
	scheme := from.Scheme
	if scheme == "" {
		scheme = "http"
	}
	base := scheme + "://" + from.Host
	if from.Path != "" && from.Path != "/" {
		if i := strings.LastIndexByte(from.Path, '/'); i >= 0 {
			base += from.Path[:i+1]
		} else {
			base += "/"
		}
	} else {
		base += "/"
	}
	resolved, ok := ResolveHrefForRedirect(base, location)
	if !ok {
		return URL{}, false
	}
	return ParseURL(resolved)
}

// ResolveHrefForRedirect is ResolveHref specialised for absolute http bases.
func ResolveHrefForRedirect(base, href string) (string, bool) {
	if strings.HasPrefix(href, "/") {
		if u, ok := ParseURL(base); ok {
			return u.Scheme + "://" + u.Host + href, true
		}
		return "", false
	}
	return ResolveHref(base, href)
}

// RedirectStatus reports whether a status code is a redirect we follow.
func RedirectStatus(code int) bool { return code >= 300 && code < 400 }

// Cookie is one parsed cookie row from a Set-Cookie response header.
type Cookie struct {
	Name   string
	Value  string
	Domain string // as sent ("" when the header had none)
	Path   string
	Flags  string // raw attribute tail, kept for the ledger only
}

// M97f F1+F2 (#2105): the bounded cookie subset ADR 0028 D §7 declares.
// Individual fields are size-capped, and any remote byte that could shift a
// tab-separated ledger row (tab, newline, carriage return, other controls)
// refuses the whole cookie — the parser never normalizes hostile bytes into
// something else.
const (
	cookieSpecMax  = 1024
	cookieNameMax  = 64
	cookieValueMax = 256
	cookieScopeMax = 128
)

// cookieFieldOK reports whether s is safe as one stored field: printable
// ASCII only. A tab would shift the ledger column layout; a CR/LF would
// inject a whole row.
func cookieFieldOK(s string) bool {
	for i := 0; i < len(s); i++ {
		if b := s[i]; b < 0x20 || b == 0x7f {
			return false
		}
	}
	return true
}

// SetCookieHeaders parses every Set-Cookie header in a response head. Only the
// name=value pair and the Domain/Path attributes are interpreted (there is no
// JS to read the rest); the remaining attributes are kept verbatim in Flags so
// the ledger stays inspectable and lossless.
func SetCookieHeaders(head string) []Cookie {
	var out []Cookie
	for _, ln := range strings.Split(head, "\n") {
		ln = strings.TrimRight(ln, "\r")
		i := strings.IndexByte(ln, ':')
		if i <= 0 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(ln[:i]), "set-cookie") {
			continue
		}
		spec := strings.TrimSpace(ln[i+1:])
		if spec == "" || len(spec) > cookieSpecMax {
			continue
		}
		parts := strings.Split(spec, ";")
		nv := strings.TrimSpace(parts[0])
		eq := strings.IndexByte(nv, '=')
		if eq <= 0 {
			continue
		}
		c := Cookie{Name: strings.TrimSpace(nv[:eq]), Value: strings.TrimSpace(nv[eq+1:])}
		var tail []string
		for _, attr := range parts[1:] {
			a := strings.TrimSpace(attr)
			if a == "" {
				continue
			}
			low := strings.ToLower(a)
			switch {
			case strings.HasPrefix(low, "domain="):
				c.Domain = strings.TrimSpace(a[len("domain="):])
			case strings.HasPrefix(low, "path="):
				c.Path = strings.TrimSpace(a[len("path="):])
			default:
				tail = append(tail, a)
			}
		}
		c.Flags = strings.Join(tail, "; ")
		if len(c.Name) > cookieNameMax || len(c.Value) > cookieValueMax ||
			len(c.Domain) > cookieScopeMax || len(c.Path) > cookieScopeMax ||
			len(c.Flags) > cookieScopeMax ||
			!cookieFieldOK(c.Name) || !cookieFieldOK(c.Value) ||
			!cookieFieldOK(c.Domain) || !cookieFieldOK(c.Path) || !cookieFieldOK(c.Flags) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// AcceptCookies applies the bounded accept policy of ADR 0028 D §7 to parsed
// Set-Cookie rows for a response from host: the Domain attribute must name
// the request host exactly (one leading dot is ignored) — a suffix, parent
// domain, or IP suffix is refused outright, never broadened or stored. A
// missing Domain stores host-only. Every returned cookie carries the request
// host in Domain, so the ledger can only ever hold origin-exact rows, and a
// non-"/" Path normalizes to "/" rather than shifting scope.
func AcceptCookies(cs []Cookie, host string) []Cookie {
	var out []Cookie
	for _, c := range cs {
		if c.Name == "" || host == "" {
			continue
		}
		if c.Domain != "" && !strings.EqualFold(strings.TrimPrefix(c.Domain, "."), host) {
			continue
		}
		c.Domain = host
		if !strings.HasPrefix(c.Path, "/") {
			c.Path = "/"
		}
		out = append(out, c)
	}
	return out
}

// CookieHeader builds a request Cookie header for host+path from stored rows
// ("name=value" fields, as written to the ledger). Host matching is EXACT:
// the stored domain must equal the request host (one leading dot ignored) —
// there is no suffix matching, so a row can never ride a request to a
// different origin, and a scope-blanked row matches nothing. An empty Path
// matches everything.
func CookieHeader(rows []string, host, path string) string {
	var pairs []string
	for _, row := range rows {
		f := strings.Split(row, "\t")
		if len(f) < 5 {
			continue
		}
		name, value, domain, cpath := f[1], f[2], f[3], f[4]
		if name == "" || domain == "" {
			continue
		}
		if !strings.EqualFold(host, strings.TrimPrefix(domain, ".")) {
			continue
		}
		if !cookieFieldOK(name) || !cookieFieldOK(value) {
			continue
		}
		if cpath != "" && !strings.HasPrefix(path, cpath) {
			continue
		}
		pairs = append(pairs, name+"="+value)
	}
	return strings.Join(pairs, "; ")
}

// FormatGetRequestWithCookies is FormatGetRequest plus the store's Cookie
// header (empty cookie == identical to FormatGetRequest).
func FormatGetRequestWithCookies(host, path, cookie string) string {
	base := FormatGetRequest(host, path)
	if cookie == "" {
		return base
	}
	// Insert before the terminating blank line.
	head := strings.TrimSuffix(base, "\r\n\r\n")
	return head + "\r\nCookie: " + cookie + "\r\n\r\n"
}
