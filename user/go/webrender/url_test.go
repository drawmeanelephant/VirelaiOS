package webrender

import (
	"strings"
	"testing"
)

func TestParseHTTPURL(t *testing.T) {
	cases := []struct {
		raw    string
		ok     bool
		host   string
		port   uint16
		path   string
		isIP   bool
		ipWord uint32
	}{
		{"http://10.0.0.2/", true, "10.0.0.2", 80, "/", true, 0x0A000002},
		{"http://10.0.0.2:80/x.html", true, "10.0.0.2", 80, "/x.html", true, 0x0A000002},
		{"http://10.0.0.2:8080", true, "10.0.0.2", 8080, "/", true, 0x0A000002},
		{"http://example.com/a/b", true, "example.com", 80, "/a/b", false, 0},
		{"HTTP://Example.COM/", true, "Example.COM", 80, "/", false, 0},
		{"https://example.com/", false, "", 0, "", false, 0},
		{"http://", false, "", 0, "", false, 0},
		{"http://h:99999/", false, "", 0, "", false, 0},
		{"", false, "", 0, "", false, 0},
	}
	for _, c := range cases {
		u, ok := ParseHTTPURL(c.raw)
		if ok != c.ok {
			t.Fatalf("%q: ok=%v want %v", c.raw, ok, c.ok)
		}
		if !ok {
			continue
		}
		if u.Host != c.host || u.Port != c.port || u.Path != c.path || u.IsIP != c.isIP {
			t.Fatalf("%q: got %+v", c.raw, u)
		}
		if c.isIP && IPv4ToU32(u.IPv4) != c.ipWord {
			t.Fatalf("%q: ip word %#x want %#x", c.raw, IPv4ToU32(u.IPv4), c.ipWord)
		}
	}
}

func TestParseURLHTTPS(t *testing.T) {
	u, ok := ParseURL("https://10.0.0.2:24533/")
	if !ok || u.Scheme != "https" || u.Host != "10.0.0.2" || u.Port != 24533 || u.Path != "/" || !u.IsIP {
		t.Fatalf("got %+v ok=%v", u, ok)
	}
	def, ok := ParseURL("https://10.0.0.2/")
	if !ok || def.Port != 443 || def.Scheme != "https" {
		t.Fatalf("default https port = %+v ok=%v", def, ok)
	}
	if _, ok := ParseURL("https://"); ok {
		t.Fatal("empty https host accepted")
	}
	http, ok := ParseURL("http://10.0.0.2/")
	if !ok || http.Scheme != "http" || http.Port != 80 {
		t.Fatalf("http via ParseURL = %+v ok=%v", http, ok)
	}
}

func TestParseIPv4Rejects(t *testing.T) {
	bad := []string{"", "1", "1.2.3", "1.2.3.4.5", "256.0.0.1", "1.2.3.999", "1.2.3.a", " 1.2.3.4", "1..2.3"}
	for _, s := range bad {
		if _, ok := ParseIPv4(s); ok {
			t.Fatalf("ParseIPv4(%q) accepted", s)
		}
	}
	if ip, ok := ParseIPv4("255.255.255.255"); !ok || IPv4ToU32(ip) != 0xFFFFFFFF {
		t.Fatalf("255.255.255.255 failed: %v", ip)
	}
}

func TestResolveHref(t *testing.T) {
	cases := []struct {
		base, href, want string
		ok               bool
	}{
		{"/host/PAGE.HTML", "NEXT.HTML", "/host/NEXT.HTML", true},
		{"/host/PAGE.HTML", "./NEXT.HTML", "/host/NEXT.HTML", true},
		{"/host/PAGE.HTML", "../UP.HTML", "/UP.HTML", true},
		{"/host/PAGE.HTML", "/abs/X.HTML", "/abs/X.HTML", true},
		{"/host/PAGE.HTML", "http://10.0.0.2/next", "http://10.0.0.2/next", true},
		{"/host/PAGE.HTML", "#frag", "", false},
		{"/host/PAGE.HTML", "NEXT.HTML#frag", "/host/NEXT.HTML", true},
	}
	for _, c := range cases {
		got, ok := ResolveHref(c.base, c.href)
		if ok != c.ok || got != c.want {
			t.Fatalf("ResolveHref(%q,%q) = %q,%v want %q,%v", c.base, c.href, got, ok, c.want, c.ok)
		}
	}
}

func TestHTTPStatusAndSplit(t *testing.T) {
	if s := HTTPStatus("HTTP/1.0 200 OK\r\n"); s != 200 {
		t.Fatalf("status=%d", s)
	}
	if s := HTTPStatus("HTTP/1.1 404 Not Found\n"); s != 404 {
		t.Fatalf("status=%d", s)
	}
	if s := HTTPStatus("garbage"); s != 0 {
		t.Fatalf("status=%d", s)
	}
	head, body, ok := SplitHTTPResponse([]byte("HTTP/1.0 200 OK\r\n\r\nhello"))
	if !ok || head != "HTTP/1.0 200 OK\r\n\r\n" || string(body) != "hello" {
		t.Fatalf("split: %q %q %v", head, body, ok)
	}
	if _, _, ok := SplitHTTPResponse([]byte("HTTP/1.0 200 OK\r\n")); ok {
		t.Fatal("truncated head accepted")
	}
}

func TestFormatGetRequest(t *testing.T) {
	got := FormatGetRequest("10.0.0.2", "/index.html")
	want := "GET /index.html HTTP/1.0\r\nHost: 10.0.0.2\r\nUser-Agent: VirelaiOS/1.0\r\n\r\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
	if got := FormatGetRequest("h", ""); got[:4] != "GET " || got[4:5] != "/" {
		t.Fatalf("empty path: %q", got)
	}
}

func TestSetCookieHeaders(t *testing.T) {
	head := "HTTP/1.0 200 OK\r\nSet-Cookie: sid=abc; Path=/; HttpOnly; Secure\r\nSet-Cookie: theme=dark; Domain=.example.com\r\nset-cookie: bare=1\r\n\r\n"
	cs := SetCookieHeaders(head)
	if len(cs) != 3 {
		t.Fatalf("parsed %d cookies want 3: %+v", len(cs), cs)
	}
	if cs[0].Name != "sid" || cs[0].Value != "abc" || cs[0].Path != "/" || cs[0].Flags != "HttpOnly; Secure" {
		t.Fatalf("cookie 0 = %+v", cs[0])
	}
	if cs[1].Domain != ".example.com" {
		t.Fatalf("cookie 1 = %+v", cs[1])
	}
	if cs[2].Name != "bare" || cs[2].Value != "1" {
		t.Fatalf("cookie 2 = %+v", cs[2])
	}
	if got := SetCookieHeaders("HTTP/1.0 200 OK\r\nContent-Length: 3\r\n\r\n"); len(got) != 0 {
		t.Fatalf("cookie-less head parsed %d cookies", len(got))
	}
	// A malformed row must be skipped, not crash.
	if got := SetCookieHeaders("HTTP/1.0 200 OK\r\nSet-Cookie: novalue\r\n\r\n"); len(got) != 0 {
		t.Fatalf("malformed Set-Cookie parsed %d cookies", len(got))
	}
}

func TestCookieHeaderMatching(t *testing.T) {
	rows := []string{
		"1\tsid\tabc\texample.com\t/\tHttpOnly", // host-only, path /
		"2\tid\t7\t.example.com\t/\t",           // domain suffix row reads host-only
		"3\tscoped\tx\texample.com\t/app\t",     // path /app
		"4\tother\t9\t.other.test\t/\t",         // unrelated domain
		"5\ttoss\t1\t\t/\t",                     // scope-blanked row: inert
		"6\twide\ta\tcom\t/\t",                  // broad suffix: refused
		"broken",                                // malformed row ignored
	}
	got := CookieHeader(rows, "example.com", "/app/page")
	for _, want := range []string{"sid=abc", "id=7", "scoped=x"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %q", want, got)
		}
	}
	for _, bad := range []string{"other=9", "toss=1", "wide=a"} {
		if strings.Contains(got, bad) {
			t.Fatalf("%s leaked: %q", bad, got)
		}
	}
	if got := CookieHeader(rows, "example.com", "/other"); strings.Contains(got, "scoped=x") {
		t.Fatalf("path-scoped cookie leaked: %q", got)
	}
	// Host-only means host-only: a row stored for example.com does not ride
	// requests to a subdomain or a sibling.
	if got := CookieHeader(rows, "sub.example.com", "/"); got != "" {
		t.Fatalf("subdomain received parent cookie: %q", got)
	}
	if got := CookieHeader(rows, "bank.example", "/"); got != "" {
		t.Fatalf("cross-origin cookie tossed: %q", got)
	}
	if got := CookieHeader(nil, "example.com", "/"); got != "" {
		t.Fatalf("empty store produced %q", got)
	}
}

// M97f F1+F2 (#2105): the bounded accept policy. A Domain attribute must
// name the request host exactly; suffixes, parent domains and IP suffixes
// are refused outright, and a missing Domain stores host-only.
func TestAcceptCookiesPolicy(t *testing.T) {
	head := "HTTP/1.0 200 OK\r\n" +
		"Set-Cookie: broad=1; Domain=com\r\n" +
		"Set-Cookie: ip=2; Domain=0.0.2\r\n" +
		"Set-Cookie: parent=3; Domain=example.com\r\n" +
		"Set-Cookie: exact=4; Domain=shop.com; Path=/app\r\n" +
		"Set-Cookie: dotted=6; Domain=.shop.com\r\n" +
		"Set-Cookie: plain=5\r\n\r\n"
	cs := AcceptCookies(SetCookieHeaders(head), "shop.com")
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
		if c.Domain != "shop.com" {
			t.Fatalf("accepted cookie %s stored domain %q want shop.com", c.Name, c.Domain)
		}
	}
	want := []string{"exact", "dotted", "plain"}
	if len(names) != len(want) {
		t.Fatalf("accepted %v want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("accepted %v want %v", names, want)
		}
	}
	// A foreign host accepts only the Domain-less cookie — and that one
	// binds to ITS host, never to shop.com.
	cs = AcceptCookies(SetCookieHeaders(head), "other.test")
	if len(cs) != 1 || cs[0].Name != "plain" || cs[0].Domain != "other.test" {
		t.Fatalf("foreign host accepted %+v", cs)
	}
	// A non-"/" path attribute normalizes to "/" rather than shifting scope.
	cs = AcceptCookies(SetCookieHeaders("HTTP/1.0 200 OK\r\nSet-Cookie: p=1; Path=relative\r\n\r\n"), "h.test")
	if len(cs) != 1 || cs[0].Path != "/" {
		t.Fatalf("relative path stored %+v", cs)
	}
}

// M97f F1+F2 (#2105): remote bytes that would shift a ledger field (a tab
// inside name/value/domain/path) refuse the cookie at the parser — they are
// never normalized into something else.
func TestSetCookieHeadersRefusesFieldInjection(t *testing.T) {
	head := "HTTP/1.0 200 OK\r\n" +
		"Set-Cookie: evil\t=1\r\n" +
		"Set-Cookie: v=a\tb\r\n" +
		"Set-Cookie: d=1; Domain=x\ty.test\r\n" +
		"Set-Cookie: ok=1\r\n\r\n"
	cs := SetCookieHeaders(head)
	// The edge-tab name trims clean ("evil\t" -> "evil"); the interior tabs
	// in value and domain are the real field-shift vectors and are refused.
	if len(cs) != 2 || cs[0].Name != "evil" || cs[1].Name != "ok" {
		t.Fatalf("field-injection cookies parsed %+v", cs)
	}
	for _, c := range cs {
		for _, f := range []string{c.Name, c.Value, c.Domain, c.Path, c.Flags} {
			if strings.ContainsAny(f, "\t\r\n") {
				t.Fatalf("parsed field %q kept a control byte", f)
			}
		}
	}
}

func TestFormatGetRequestWithCookies(t *testing.T) {
	plain := FormatGetRequest("10.0.0.2", "/")
	if got := FormatGetRequestWithCookies("10.0.0.2", "/", ""); got != plain {
		t.Fatalf("cookie-less request changed: %q", got)
	}
	got := FormatGetRequestWithCookies("10.0.0.2", "/x", "sid=abc; id=7")
	if !strings.Contains(got, "Cookie: sid=abc; id=7\r\n") {
		t.Fatalf("missing cookie header: %q", got)
	}
	if !strings.HasSuffix(got, "\r\n\r\n") {
		t.Fatalf("request must end with a blank line: %q", got)
	}
	if strings.Index(got, "Cookie:") > strings.Index(got, "\r\n\r\n") {
		t.Fatal("cookie header after the terminator")
	}
}
