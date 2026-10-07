package main

import (
	"strings"
	"time"

	"virelai/tls"
	"virelai/vi"
	"virelai/webrender"
	"virelai/webstyle"
)

func (a *app) stageEnd(budget int64) int64 {
	now := vi.Nanos()
	if a.pageEnd == 0 {
		a.pageEnd = now + readDeadlineMs*1_000_000
	}
	end := now + budget
	if a.pageEnd < end {
		end = a.pageEnd
	}
	return end
}

// Preserve ordinary input while a sequential resource read owns the event
// loop. Only cancellation is acted on here; deferred events keep their order.
func (a *app) cancelBetweenReads() bool {
	if a.pageCancelled || a.quit {
		return true
	}
	for n := 0; n < 64; n++ {
		ev, _, ok := vi.PollEventRaw()
		if !ok {
			break
		}
		a.queueOrCancel(ev)
	}
	return a.pageCancelled || a.quit
}

func (a *app) queueOrCancel(ev vi.Event) {
	switch {
	case ev.Kind == vi.EvWinClose:
		a.quit, a.pageCancelled = true, true
	case ev.Kind == vi.EvKeyDown && ev.Arg0 == keyEscape:
		a.pageCancelled = true
	case ev.Kind == vi.EvMouseDown &&
		inRect(int(ev.Arg0), int(ev.Arg1), reloadX, backY, chipW, chipH):
		a.pageCancelled = true
	default:
		if len(a.pendingEvents) < 2*maxURLBytes {
			a.pendingEvents = append(a.pendingEvents, ev)
		} else {
			a.pageCancelled = true
			a.diagnostic(webstyle.DiagnosticLimit, "input-event-limit")
		}
	}
}

func (a *app) connectPage(u webrender.URL) (*vi.Conn, string) {
	if a.cancelBetweenReads() {
		return nil, "cancelled"
	}
	if a.stageEnd(5_000_000_000) <= vi.Nanos() {
		return nil, "timeout"
	}
	if a.requests >= maxPageRequests {
		return nil, "network-request-limit"
	}
	a.requests++
	ip := u.IPv4
	if !u.IsIP {
		var err error
		dnsBudget := a.stageEnd(5_000_000_000) - vi.Nanos()
		if dnsBudget <= 0 {
			return nil, "timeout"
		}
		ip, err = vi.ResolveDNS(u.Host, vi.DefaultDNSServer, dnsBudget)
		if a.cancelBetweenReads() {
			return nil, "cancelled"
		}
		if err != nil {
			vi.ConsoleLine("web: dns-error " + err.Error())
			return nil, "dns"
		}
		vi.ConsoleLine("web: dns " + u.Host + " " + vi.FormatIPv4(ip))
	}
	budget := a.stageEnd(5_000_000_000) - vi.Nanos()
	if budget <= 0 {
		return nil, "timeout"
	}
	conn, err := vi.DialTimeout(vi.FormatIPv4(ip), u.Port, time.Duration(budget))
	if a.cancelBetweenReads() {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, "cancelled"
	}
	if err != nil {
		vi.ConsoleLine("web: connect-error " + err.Error())
		if strings.Contains(err.Error(), "ETIMEDOUT") {
			return nil, "timeout"
		}
		return nil, "tcp"
	}
	if vi.Nanos() >= a.pageEnd {
		_ = conn.Close()
		return nil, "timeout"
	}
	return conn, ""
}

func (a *app) closeLoad() {
	if a.loadConn != nil {
		_ = a.loadConn.Close()
		a.loadConn = nil
	} else {
		vi.TCPClose()
	}
}

func tlsErrorKind(err error) string {
	switch {
	case tls.IsHostnameMismatch(err):
		return "tls-hostname-mismatch"
	case tls.IsExpired(err):
		return "tls-expired"
	case tls.IsNoPathToRoot(err):
		return "tls-unknown-root"
	case tls.IsTimeout(err):
		return "timeout"
	}
	return "tls"
}

// fetchNetwork is sequential and shares the page request/deadline ledger.
// Only explicit HTTP identity framing can turn the returned bytes into a body.
func (a *app) fetchNetwork(u webrender.URL, capn int, mainPage bool) ([]byte, bool, string) {
	conn, kind := a.connectPage(u)
	if kind != "" {
		if u.Scheme == "https" && kind == "tcp" {
			kind = "tls"
		}
		return nil, false, kind
	}
	var secured *tls.TLSConn
	if u.Scheme == "https" {
		var err error
		secured, err = tls.Handshake(conn, u.Host, a.stageEnd(10_000_000_000))
		if a.cancelBetweenReads() {
			if secured != nil {
				_ = secured.Close()
			}
			return nil, false, "cancelled"
		}
		if err != nil {
			return nil, false, tlsErrorKind(err) // Handshake owns failure cleanup
		}
		defer secured.Close()
		vi.ConsoleLine("web: tls " + u.Host + " " + tls.TrustVersion())
	} else {
		defer conn.Close()
	}
	request := []byte(formatPageRequest(u, a.cookieHeaderFor(u.Host, u.Path)))
	end := a.stageEnd(5_000_000_000)
	if secured != nil {
		secured.SetDeadline(end)
		if n, err := secured.Write(request); err != nil || n != len(request) {
			return nil, false, "tls"
		}
	} else if !sendAll(request) {
		return nil, false, "tcp"
	}
	vi.ConsoleLine(markerFetch + u.Host + u.Path)
	raw := make([]byte, 0, min(capn+maxHTTPHeaders, 16384))
	var frame responseFrame
	var chunk [1024]byte
	for {
		if a.cancelBetweenReads() {
			return nil, false, "cancelled"
		}
		if vi.Nanos() >= end || vi.Nanos() >= a.pageEnd {
			vi.ConsoleLine("web: read-timeout buffered=" + itoa(len(raw)) +
				" header=" + boolStr(frame.complete) + " requests=" + itoa(a.requests))
			return nil, false, "timeout"
		}
		n, eof, failure := 0, false, ""
		if secured != nil {
			secured.SetDeadline(end)
			var err error
			n, err = secured.Read(chunk[:])
			if err != nil {
				if tls.IsPeerClosed(err) {
					eof = true
				} else {
					failure = tlsErrorKind(err)
				}
			}
		} else {
			got, rc := vi.TCPRecv(chunk[:])
			if rc < 0 {
				failure = "tcp"
			} else {
				n = got
				mask, ready := vi.TCPReady()
				eof = n == 0 && ready >= 0 && mask&1 != 0
			}
		}
		if a.cancelBetweenReads() {
			return nil, false, "cancelled"
		}
		if failure != "" {
			if failure == "timeout" {
				vi.ConsoleLine("web: read-timeout buffered=" + itoa(len(raw)) +
					" header=" + boolStr(frame.complete) + " requests=" + itoa(a.requests))
			}
			return nil, false, failure
		}
		if n > 0 {
			if len(raw)+n > capn+maxHTTPHeaders {
				return nil, false, "http-body-limit"
			}
			raw = append(raw, chunk[:n]...)
		}
		if !frame.complete {
			frame = parseResponseFrame(raw)
			if frame.complete && frame.err == "" {
				end = a.stageEnd(5_000_000_000)
			}
		} else if n > 0 {
			end = a.stageEnd(5_000_000_000)
		}
		body, truncated, failure := responseBody(frame, raw, capn, eof)
		if failure != "" {
			return nil, false, failure
		}
		complete := body != nil || frame.complete && frame.need == 0
		if complete {
			if truncated && !mainPage {
				return nil, false, "http-body-limit"
			}
			raw = raw[:frame.offset+len(body)] // never expose bytes beyond Content-Length
			return raw, truncated, ""
		}
		if eof {
			return nil, false, "http-premature-eof"
		}
		if n == 0 {
			vi.Sleep(1)
		}
	}
}

func (a *app) fetchResource(target string, capn int) ([]byte, string) {
	start := vi.Nanos()
	defer func() { a.resourceWait += vi.Nanos() - start }()
	if a.resourceForTest != nil {
		return a.resourceForTest(target, capn)
	}
	u, ok := parseBrowserURL(target)
	if !ok {
		resolved, kind := resolveInput(target)
		if kind != "file" {
			return nil, "scheme"
		}
		data := readWholeFile(resolved, capn+1)
		if len(data) == 0 {
			return nil, "file"
		}
		if len(data) > capn {
			return nil, "http-body-limit"
		}
		return data, ""
	}
	seen := map[string]bool{formatNavURL(u): true}
	for hops := 0; ; hops++ {
		raw, _, kind := a.fetchNetwork(u, capn, false)
		if kind != "" {
			return nil, kind
		}
		frame := parseResponseFrame(raw)
		code := webrender.HTTPStatus(frame.head)
		if isPageRedirect(code) {
			if hops >= maxRedirects {
				return nil, "redirect-loop"
			}
			next, kind := redirectTarget(u, webrender.LocationHeader(frame.head))
			if kind != "" {
				return nil, kind
			}
			if seen[formatNavURL(next)] {
				return nil, "redirect-loop"
			}
			seen[formatNavURL(next)] = true
			u = next
			continue
		}
		if code != 200 {
			return nil, "http"
		}
		return raw[frame.offset:], ""
	}
}
