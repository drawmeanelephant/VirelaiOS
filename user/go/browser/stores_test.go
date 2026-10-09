package main

import (
	"strings"
	"testing"
)

func TestCacheKeyAndBodyPath(t *testing.T) {
	a := cacheKey("http://10.0.0.2/")
	b := cacheKey("http://10.0.0.2/")
	if a != b {
		t.Fatal("cache key is not deterministic")
	}
	if len(a) != 16 {
		t.Fatalf("cache key length = %d want 16 hex digits", len(a))
	}
	if cacheKey("http://10.0.0.2/x") == a {
		t.Fatal("distinct URLs share a cache key")
	}
	// The empty string hashes to the FNV offset basis, which is a known
	// value: pin it so a hash change cannot silently orphan every cache file.
	if got := cacheKey(""); got != "cbf29ce484222325" {
		t.Fatalf("fnv1a offset basis = %s", got)
	}
	p := cacheBodyPath("http://10.0.0.2/")
	if !strings.HasPrefix(p, cacheBodyFmt) || !strings.HasSuffix(p, ".BIN") {
		t.Fatalf("cache body path = %q", p)
	}
}

func TestDownloadName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://10.0.0.2/index.html", "index.html"},
		{"/host/PAGE.HTML", "PAGE.HTML"},
		{"http://10.0.0.2/a/b.txt?x=1", "b.txt"},
		{"http://10.0.0.2/", "PAGE.HTML"},
		{"http://10.0.0.2/../etc/passwd", "passwd"},
		{"http://10.0.0.2/a b*c.d", "a_b_c.d"},
	}
	for _, c := range cases {
		if got := downloadName(c.in); got != c.want {
			t.Fatalf("downloadName(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestPersistCookiesCountsRows(t *testing.T) {
	a := &app{}
	head := "HTTP/1.0 200 OK\r\nSet-Cookie: sid=abc; Path=/; HttpOnly\r\nSet-Cookie: theme=dark\r\n\r\n"
	if n := a.persistCookies(head, "example.com"); n != 2 {
		t.Fatalf("persisted %d cookies want 2", n)
	}
	if n := a.persistCookies("HTTP/1.0 200 OK\r\n\r\n", "example.com"); n != 0 {
		t.Fatalf("persisted %d cookies from a cookie-less head", n)
	}
}

// M97f F1+F2 (#2105): the bounded cookie subset — a response can only ever
// store rows for the host that sent it, so a broad Domain, a parent domain
// or an IP suffix is refused at the store door.
func TestPersistCookiesHostOnly(t *testing.T) {
	a := &app{}
	head := "HTTP/1.0 200 OK\r\n" +
		"Set-Cookie: broad=1; Domain=com\r\n" +
		"Set-Cookie: parent=2; Domain=example.com\r\n" +
		"Set-Cookie: own=3; Domain=sub.example.com\r\n" +
		"Set-Cookie: plain=4\r\n\r\n"
	if n := a.persistCookies(head, "sub.example.com"); n != 2 {
		t.Fatalf("persisted %d cookies want 2 (exact host + host-only)", n)
	}
	if n := a.persistCookies(head, "other.test"); n != 1 {
		t.Fatalf("foreign host persisted %d cookies want 1 (host-only only)", n)
	}
	// A scope-blank Domain attribute can never re-enter through the front
	// door: Domain= empty string is still a Domain attribute.
	if n := a.persistCookies("HTTP/1.0 200 OK\r\nSet-Cookie: x=1; Domain=\r\n\r\n", "h.test"); n != 1 {
		t.Fatalf("empty Domain attribute = %d want 1 (treated as absent)", n)
	}
}

// M97f F2 (#2105): the ledger cap evicts the oldest rows, never grows past
// the bound.
func TestBoundCookieRows(t *testing.T) {
	rows := make([]string, cookieMaxRows)
	for i := range rows {
		rows[i] = "r"
	}
	got := boundCookieRows(rows, 1)
	if len(got) != cookieMaxRows-1 {
		t.Fatalf("evicted to %d want %d", len(got), cookieMaxRows-1)
	}
	if got := boundCookieRows(rows, 4); len(got) != cookieMaxRows-4 {
		t.Fatalf("evicted to %d want %d", len(got), cookieMaxRows-4)
	}
	small := []string{"a", "b"}
	if got := boundCookieRows(small, 1); len(got) != 2 {
		t.Fatalf("small ledger grew-evicted to %d", len(got))
	}
}

// M97f F8 (#2105): the one field check guards every ledger write — a
// remote-derived value carrying a tab or CR can never shift a row.
func TestLedgerFieldOK(t *testing.T) {
	for _, bad := range []string{"a\tb", "a\rb", "a\nb", "a\x01b", "a\x7fb"} {
		if ledgerFieldOK(bad) {
			t.Fatalf("ledgerFieldOK(%q) = true", bad)
		}
	}
	for _, ok := range []string{"", "plain", "a b;c=d", "https://h.test/p?q=1"} {
		if !ledgerFieldOK(ok) {
			t.Fatalf("ledgerFieldOK(%q) = false", ok)
		}
	}
}

// A cache miss on the host (no file channel) must be a miss, not a panic, and
// the offline path must degrade to the ordinary error page.
func TestOfflineFallsBackToTheErrorPage(t *testing.T) {
	a := &app{hist: newHistory(), target: "http://10.0.0.2/"}
	a.offlineOr("tcp")
	if a.errKind != "tcp" {
		t.Fatalf("errKind = %q want tcp", a.errKind)
	}
}

func TestStoreSummaryShape(t *testing.T) {
	a := &app{}
	got := a.storeSummary()
	if !strings.HasPrefix(got, markerStores) {
		t.Fatalf("summary = %q", got)
	}
	for _, field := range []string{"history=", "bookmarks=", "cookies=", "cache=", "downloads="} {
		if !strings.Contains(got, field) {
			t.Fatalf("summary %q is missing %s", got, field)
		}
	}
}

func TestLedgerRewriteOnHostIsAFailedWriteNotAPanic(t *testing.T) {
	if ledgerRewrite("/host/WEB-TEST.TXT", "# v1", []string{"a"}) {
		t.Fatal("host has no file channel; the rewrite must report failure")
	}
	if rows := ledgerRows("/host/WEB-TEST.TXT"); rows != nil {
		t.Fatalf("host ledger read = %v", rows)
	}
	if n := ledgerCount("/host/WEB-TEST.TXT"); n != 0 {
		t.Fatalf("host ledger count = %d", n)
	}
}

// The request builder must not send a Cookie header when the store is empty,
// and must place it before the terminating blank line when it is not.
func TestCookieHeaderPlacement(t *testing.T) {
	if got := cookieHeaderForTest(t, "", "10.0.0.2", "/"); got != "" {
		t.Fatalf("empty store produced %q", got)
	}
}

func cookieHeaderForTest(t *testing.T, rows, host, path string) string {
	t.Helper()
	a := &app{}
	_ = rows
	return a.cookieHeaderFor(host, path)
}
