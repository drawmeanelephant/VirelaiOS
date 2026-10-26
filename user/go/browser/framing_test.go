package main

import (
	"strings"
	"testing"

	"virelai/vi"
	"virelai/webrender"
)

func TestRequestAuthorityAndIdentityFraming(t *testing.T) {
	u, ok := parseBrowserURL("https://Example.COM:8443/a?x=1")
	if !ok {
		t.Fatal("URL rejected")
	}
	got := formatPageRequest(u, "a=b")
	want := "GET /a?x=1 HTTP/1.1\r\nHost: example.com:8443\r\nUser-Agent: VirelaiOS/1.0\r\nCookie: a=b\r\nConnection: keep-alive\r\nAccept-Encoding: gzip\r\n\r\n"
	if got != want {
		t.Fatalf("request=%q want=%q", got, want)
	}
	if strings.Contains(formatPageRequest(u, "x\r\nInjected: yes"), "Injected") {
		t.Fatal("cookie ledger injected a header")
	}
}

func TestBoundedRecordedHTTPFrames(t *testing.T) {
	for _, tc := range []struct {
		name, response, error string
	}{
		{"length", "HTTP/1.0 200 OK\r\nContent-Length: 3\r\n\r\nabc", ""},
		{"EOF", "HTTP/1.1 200 OK\n\nabc", ""},
		{"chunked", "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n", "chunked-malformed"},
		{"gzip", "HTTP/1.0 200 OK\r\nContent-Encoding: gzip\r\n\r\n", "gzip-malformed"},
		{"te nonchunked", "HTTP/1.1 200 OK\r\nTransfer-Encoding: deflate\r\n\r\n", "http-framing-unsupported"},
		{"ce br", "HTTP/1.0 200 OK\r\nContent-Encoding: br\r\n\r\n", "http-framing-unsupported"},
		{"te+cl", "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nContent-Length: 3\r\n\r\n", "http-framing-invalid"},
		{"length conflict", "HTTP/1.0 200 OK\r\nContent-Length: 3\r\nContent-Length: 4\r\n\r\n", "http-framing-invalid"},
		{"bogus length", "HTTP/1.0 200 OK\r\nContent-Length: -1\r\n\r\n", "http-framing-invalid"},
		{"short", "HTTP/1.0 200 OK\r\nContent-Length: 4\r\n\r\nabc", "http-premature-eof"},
		{"fold", "HTTP/1.0 200 OK\r\n folded\r\n\r\n", "http-framing-invalid"},
		{"bad field token", "HTTP/1.0 200 OK\r\nX(bad): y\r\n\r\n", "http-framing-invalid"},
		{"bad status token", "HTTP/1.0 200oops\r\n\r\n", "http-framing-invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := parseResponseFrame([]byte(tc.response))
			_, _, err := responseBody(f, []byte(tc.response), 100, true)
			if err != tc.error {
				t.Fatalf("frame error=%q want=%q", err, tc.error)
			}
		})
	}
	for _, raw := range []string{
		"HTTP/1.0 200 OK\r\nX: " + strings.Repeat("x", maxHTTPLine) + "\r\n\r\n",
		"HTTP/1.0 200 OK\r\n" + strings.Repeat("X: y\r\n", maxHTTPFields+1) + "\r\n",
		strings.Repeat("x", maxHTTPHeaders+1),
	} {
		if f := parseResponseFrame([]byte(raw)); f.err == "" {
			t.Fatal("header bound accepted")
		}
	}
}

// The fetch drivers feed one appended buffer per read; the card's fix is
// that an n-byte head arriving in 1-byte drips costs one pass total. The
// pre-fix shape re-ran the terminator scan plus a whole-buffer line split
// per read — work ~ n^2 with the peer choosing the read count (#3684).
func TestHeadScanIsLinearUnderByteDrips(t *testing.T) {
	// A head near 12 KiB built from a few near-limit lines: the field count
	// stays under maxHTTPFields and every line under maxHTTPLine.
	head := "HTTP/1.1 200 OK\r\n"
	for i := 0; i < 3; i++ {
		head += "X-Field: " + strings.Repeat("y", 3800) + "\r\n"
	}
	head += "Content-Length: 4\r\n\r\n"
	want := parseResponseFrame([]byte(head + "body"))
	if !want.complete || want.err != "" {
		t.Fatalf("fixture head must parse: complete=%v err=%q", want.complete, want.err)
	}

	var s headScan
	var raw []byte
	var f responseFrame
	for i := 0; i < len(head); i++ {
		raw = append(raw, head[i])
		f = s.feed(raw)
	}
	if !f.complete || f.err != "" {
		t.Fatalf("dripped head: complete=%v err=%q", f.complete, f.err)
	}
	if f.offset != len(head) || f.need != 4 {
		t.Fatalf("dripped frame offset=%d need=%d, want %d/4", f.offset, f.need, len(head))
	}
	// Resume semantics: each feed scans the new byte plus at most the
	// three byte positions a split \r\n\r\n could still start at, and the
	// line bound sees each byte once — work <= 2n + 3·reads <= 5n. The
	// pre-fix rescan examined ~2n per read: ~n^2 for n drips.
	if s.work > 5*len(head) {
		t.Fatalf("head scan work=%d for %d dripped bytes — not linear", s.work, len(head))
	}
	// The drip verdict matches the one-shot verdict on the same bytes.
	if f.head != want.head || f.parsed != want.parsed {
		t.Fatal("dripped and one-shot frames disagree")
	}

	// A refused head drips to the same named refusal.
	var s2 headScan
	raw = nil
	bad := "HTTP/1.1 200 OK\r\nX: " + strings.Repeat("x", maxHTTPLine+1) + "\r\n\r\n"
	f = responseFrame{}
	for i := 0; i < len(bad) && !f.complete; i++ {
		raw = append(raw, bad[i])
		f = s2.feed(raw)
	}
	if f.err != "http-header-limit" {
		t.Fatalf("over-limit dripped line: err=%q want http-header-limit", f.err)
	}
}

// Every framing fixture dripped a byte at a time lands on the same
// accept/refuse verdict as the one-shot parse — and on acceptance the
// same frame. (A refusal's name can legitimately differ by delivery:
// the incremental line bound trips before a terminator that a whole
// buffer would have parsed through to a grammar refusal.)
func TestHeadScanDripsMatchOneShot(t *testing.T) {
	for _, tc := range []string{
		"HTTP/1.0 200 OK\r\nContent-Length: 3\r\n\r\nabc",
		"HTTP/1.1 200 OK\n\nabc",
		"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n",
		"HTTP/1.0 200 OK\r\nContent-Encoding: gzip\r\n\r\n",
		"HTTP/1.1 200 OK\r\nTransfer-Encoding: deflate\r\n\r\n",
		"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nContent-Length: 3\r\n\r\n",
		"HTTP/1.0 200 OK\r\nContent-Length: -1\r\n\r\n",
		"HTTP/1.0 200 OK\r\n folded\r\n\r\n",
		"HTTP/1.0 200oops\r\n\r\n",
		"HTTP/1.0 200 OK\r\nX: " + strings.Repeat("x", maxHTTPLine) + "\r\n\r\n",
		strings.Repeat("x", maxHTTPHeaders+1),
		"HTTP/1.0 200 OK\r\nContent-Len", // never completes
	} {
		var s headScan
		var raw []byte
		var f responseFrame
		for i := 0; i < len(tc) && !f.complete; i++ {
			raw = append(raw, tc[i])
			f = s.feed(raw)
		}
		want := parseResponseFrame([]byte(tc))
		if f.complete != want.complete || (f.err == "") != (want.err == "") {
			t.Fatalf("%q: dripped complete=%v err=%q, one-shot complete=%v err=%q",
				tc[:min(20, len(tc))], f.complete, f.err, want.complete, want.err)
		}
		if f.err == "" && f.complete &&
			(f.head != want.head || f.offset != want.offset || f.need != want.need || f.parsed != want.parsed) {
			t.Fatalf("%q: dripped and one-shot frames disagree", tc[:min(20, len(tc))])
		}
	}
}

func TestBrowserURLAndRedirectPolicy(t *testing.T) {
	for _, raw := range []string{"https://u@h/", "https://[::1]/", "http://h:0/", "http://h/\r\nX: y", "http://h/a b"} {
		if _, ok := parseBrowserURL(raw); ok {
			t.Fatalf("unsafe URL accepted: %q", raw)
		}
	}
	from, _ := parseBrowserURL("https://h:8443/a/b?old=1")
	for _, tc := range []struct{ ref, want string }{
		{"/c", "https://h:8443/c"},
		{"../c", "https://h:8443/c"},
		{"?q=x", "https://h:8443/a/b?q=x"},
		{"#x", "https://h:8443/a/b?old=1#x"},
		{"//other/x", "https://other/x"},
	} {
		if got := resolveReference(formatNavURL(from), tc.ref); got != tc.want {
			t.Fatalf("ref=%q got=%q want=%q", tc.ref, got, tc.want)
		}
	}
	if _, kind := redirectTarget(from, "http://h/"); kind != "redirect-downgrade" {
		t.Fatalf("downgrade=%q", kind)
	}
	if _, kind := redirectTarget(from, "javascript:run"); kind != "redirect-location" {
		t.Fatalf("unsupported redirect=%q", kind)
	}
	if isPageRedirect(304) || !isPageRedirect(308) {
		t.Fatal("redirect status subset changed")
	}
}

func TestTotalDeadlineDoesNotResetWithProgress(t *testing.T) {
	f := &fakeTCP{ready: []int64{3}, recv: []int64{1}}
	defer f.install()()
	a := &app{hist: newHistory(), loading: true, pageEnd: vi.Nanos() - 1,
		loadEnd: vi.Nanos() + 30_000_000_000, target: "http://10.0.0.2/"}
	a.loadStep()
	if a.loading || a.errKind != "timeout" || f.ci != 0 {
		t.Fatalf("expired whole page read more bytes: loading=%v error=%q reads=%d", a.loading, a.errKind, f.ci)
	}
}

func TestBrowserConnectUsesExplicitFiveSecondBudget(t *testing.T) {
	f := &fakeTCP{}
	defer f.install()()
	var portWord uintptr
	prev := vi.SyscallHookForTest()
	vi.SetSyscallHookForTest(func(slot, x0, x1, x2, x3 uintptr) int64 {
		// DialTimeout rides the slot-80 handle ABI: x0 is the op, and a
		// connect carries ip in x1 and port|timeoutMs<<16 in x2.
		if slot == vi.SlotSocket && x0 == vi.SockOpConnect {
			portWord = x2
			return 0x11 // a table handle
		}
		return prev(slot, x0, x1, x2, x3)
	})
	defer vi.SetSyscallHookForTest(prev)
	a := &app{}
	u, _ := parseBrowserURL("http://10.0.0.2/")
	c, kind := a.connectPage(u)
	if kind != "" {
		t.Fatal(kind)
	}
	defer c.Close()
	ms := portWord >> 16
	if portWord&0xffff != 80 || ms == 0 || ms > 5000 {
		t.Fatalf("connect word=%#x timeout=%dms", portWord, ms)
	}
}

func TestMainResponseUsesOnlyDeclaredBodyLength(t *testing.T) {
	body := "<p>safe</p>"
	a := &app{hist: newHistory(), text: webrender.Bitmap{}, target: "http://10.0.0.2/",
		tStart: vi.Nanos(), tNav0: vi.Nanos(),
		loadBuf: []byte("HTTP/1.0 200 OK\r\nContent-Length: " + itoa(len(body)) +
			"\r\n\r\n" + body + "<p>undeclared tail</p>")}
	frame := parseResponseFrame(a.loadBuf)
	decoded, _, failure := responseBody(frame, a.loadBuf, 256*1024, true)
	if failure != "" {
		t.Fatalf("responseBody: %q", failure)
	}
	a.finishResponse(false, frame.head, decoded, false)
	if string(a.lastBody) != body {
		t.Fatalf("undeclared bytes reached the page: %q", a.lastBody)
	}
}
