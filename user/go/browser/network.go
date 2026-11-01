package main

import (
	"errors"
	"strings"
	"time"

	"virelai/h2"
	"virelai/httpc"
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
	if a.stageEnd(5_000_000_000) <= vi.Nanos() {
		return nil, "timeout"
	}
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

// pooledConn is the M135b keep-alive slot: at most one idle connection,
// bound to the origin that produced it (the kernel's per-process table is
// small and the legacy shim resolves to the only live conn, so holding a
// second connection would misroute the raw slots the loader drives).
// h2c is non-nil only when ALPN negotiated "h2" (M212b): sequential
// exchanges then ride streams on this one connection — the h2 analogue
// of h1 keep-alive reuse.
type pooledConn struct {
	origin string
	conn   *vi.Conn
	secure *tls.TLSConn
	h2c    *h2.Conn
}

// alpnOffer is the desk's protocol menu (M212b): h2 first, http/1.1 as
// the floor. A peer that answers nothing keeps the h1 engine.
var alpnOffer = []string{"h2", "http/1.1"}

func originKey(u webrender.URL) string {
	return u.Scheme + "://" + u.Host + ":" + itoa(int(u.Port))
}

// acquireConn returns a connection for u: the pooled idle one when the
// origin matches, else a fresh dial (plus TLS handshake for https). The
// pool can only ever hold one conn, so a non-matching request drops it
// before dialing — the legacy slot view must never see two live conns.
// Every call is one HTTP request for the page bound.
func (a *app) acquireConn(u webrender.URL) (*pooledConn, bool, string) {
	if a.cancelBetweenReads() {
		return nil, false, "cancelled"
	}
	if a.stageEnd(5_000_000_000) <= vi.Nanos() {
		return nil, false, "timeout"
	}
	if a.requests >= maxPageRequests {
		return nil, false, "network-request-limit"
	}
	a.requests++
	key := originKey(u)
	if a.idle != nil && a.idle.origin == key {
		p := a.idle
		a.idle = nil
		return p, true, ""
	}
	a.closePool()
	conn, kind := a.connectPage(u)
	if kind != "" {
		if u.Scheme == "https" && kind == "tcp" {
			kind = "tls"
		}
		return nil, false, kind
	}
	if u.Scheme != "https" {
		return &pooledConn{origin: key, conn: conn}, false, ""
	}
	secured, err := tls.HandshakeALPN(conn, u.Host, a.stageEnd(10_000_000_000), alpnOffer)
	if a.cancelBetweenReads() {
		if secured != nil {
			_ = secured.Close()
		}
		return nil, false, "cancelled"
	}
	if err != nil {
		return nil, false, tlsErrorKind(err) // Handshake owns failure cleanup
	}
	vi.ConsoleLine("web: tls " + u.Host + " " + tls.TrustVersion())
	// M212b: the ALPN pick selects the wire engine. An unknown pick
	// cannot arrive — the TLS layer refuses an unoffered protocol — but
	// fails closed rather than silently downgrading the negotiated name.
	proto, ok := httpc.Select(secured.ALPN())
	if !ok {
		_ = secured.Close()
		return nil, false, "tls"
	}
	shown := secured.ALPN()
	if shown == "" {
		shown = "none"
	}
	vi.ConsoleLine("web: alpn " + shown)
	p := &pooledConn{origin: key, secure: secured}
	if proto == httpc.ProtoH2 {
		p.h2c = h2.NewConn(secured)
	}
	return p, false, ""
}

func (a *app) dropConn(p *pooledConn) {
	if p == nil {
		return
	}
	if p.secure != nil {
		_ = p.secure.Close()
	} else if p.conn != nil {
		_ = p.conn.Close()
	}
}

// releaseConn parks a clean connection for reuse or closes it. A conn is
// parked only when the exchange ended on a declared boundary and the peer
// did not close; anything else is teardown, never a parked desync risk.
func (a *app) releaseConn(p *pooledConn, reusable bool) {
	if reusable {
		a.idle = p
		return
	}
	a.dropConn(p)
}

func (a *app) closePool() {
	a.dropConn(a.idle)
	a.idle = nil
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
// It acquires the origin's connection (pooled when keep-alive permits),
// sends the request, frames the response by its declared shape, and
// content-decodes the body. A stale pooled conn (the peer closed it while
// idle) earns exactly one fresh-dial retry — GET is idempotent.
func (a *app) fetchNetwork(u webrender.URL, capn int, mainPage bool) (string, []byte, bool, string) {
	for attempt := 0; ; attempt++ {
		p, reused, kind := a.acquireConn(u)
		if kind != "" {
			return "", nil, false, kind
		}
		head, body, truncated, rePool, kind := a.exchange(p, u, capn, mainPage)
		if kind == "" {
			a.releaseConn(p, rePool)
			return head, body, truncated, ""
		}
		a.dropConn(p)
		if !(reused && attempt == 0) {
			return "", nil, false, kind
		}
	}
}

func (a *app) exchange(p *pooledConn, u webrender.URL, capn int, mainPage bool) (string, []byte, bool, bool, string) {
	if p.h2c != nil {
		return a.exchangeH2(p, u, capn, mainPage)
	}
	request := []byte(formatPageRequest(u, a.cookieHeaderFor(u.Host, u.Path)))
	end := a.stageEnd(5_000_000_000)
	if p.secure != nil {
		p.secure.SetDeadline(end)
		if n, err := p.secure.Write(request); err != nil || n != len(request) {
			return "", nil, false, false, "tls"
		}
	} else {
		if n, err := p.conn.Send(request); err != nil || n != len(request) {
			return "", nil, false, false, "tcp"
		}
	}
	vi.ConsoleLine(markerFetch + u.Host + u.Path + " proto=http/1.1")
	raw := make([]byte, 0, min(capn+maxHTTPHeaders, 16384))
	var scan headScan
	var frame responseFrame
	var dec httpc.Chunked
	var decoded []byte
	var chunk [1024]byte
	for {
		if a.cancelBetweenReads() {
			return "", nil, false, false, "cancelled"
		}
		if vi.Nanos() >= end || vi.Nanos() >= a.pageEnd {
			vi.ConsoleLine("web: read-timeout buffered=" + itoa(len(raw)) +
				" header=" + boolStr(frame.complete) + " requests=" + itoa(a.requests))
			return "", nil, false, false, "timeout"
		}
		n, eof, failure := 0, false, ""
		if p.secure != nil {
			p.secure.SetDeadline(end)
			var err error
			n, err = p.secure.Read(chunk[:])
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
			return "", nil, false, false, "cancelled"
		}
		if failure != "" {
			if failure == "timeout" {
				vi.ConsoleLine("web: read-timeout buffered=" + itoa(len(raw)) +
					" header=" + boolStr(frame.complete) + " requests=" + itoa(a.requests))
			}
			return "", nil, false, false, failure
		}
		if n > 0 {
			if len(raw)+n > capn+maxHTTPHeaders+64*1024 {
				return "", nil, false, false, "http-body-limit"
			}
			raw = append(raw, chunk[:n]...)
		}
		if !frame.complete {
			frame = scan.feed(raw)
			if frame.complete && frame.err == "" {
				end = a.stageEnd(5_000_000_000)
			}
		} else if n > 0 {
			end = a.stageEnd(5_000_000_000)
		}
		done, decoded, truncated, ferr := a.frameDone(frame, raw, &dec, decoded, capn, eof)
		if ferr != "" {
			return "", nil, false, false, ferr
		}
		if done {
			body, trunc, derr := httpc.Decode(frame.parsed, decoded, capn)
			if derr != "" {
				return "", nil, false, false, derr
			}
			if trunc && !mainPage {
				return "", nil, false, false, "http-body-limit"
			}
			return frame.head, body, truncated || trunc, frame.parsed.KeepAlive() && !eof, ""
		}
		if eof {
			return "", nil, false, false, "http-premature-eof"
		}
		if n == 0 {
			vi.Sleep(1)
		}
	}
}

// frameDone is the incremental completeness check: NoBody is already
// done, chunked advances its decoder over new wire bytes, Content-Length
// compares counts, and close-delimited waits on the FIN. decoded carries
// the wire bytes that content-coding will read (dechunked form when
// chunked, the declared-length region otherwise).
func (a *app) frameDone(frame responseFrame, raw []byte, dec *httpc.Chunked, decoded []byte, capn int, eof bool) (bool, []byte, bool, string) {
	if !frame.complete {
		if eof && len(raw) == 0 {
			return false, nil, false, "http-premature-eof"
		}
		return false, nil, false, ""
	}
	if frame.err != "" {
		return false, nil, false, frame.err
	}
	wire := raw[frame.offset:]
	switch {
	case frame.parsed.NoBody:
		return true, nil, false, ""
	case frame.parsed.Chunked:
		dec.Feed(wire[dec.Fed:], &decoded, capn)
		if dec.Err != "" {
			return false, nil, false, dec.Err
		}
		if dec.Done {
			return true, decoded, dec.Over, ""
		}
		if eof {
			return false, nil, false, httpc.ChunkedMalformed
		}
		return false, nil, false, ""
	case frame.need >= 0:
		if len(wire) >= frame.need {
			return true, wire[:frame.need], false, ""
		}
		if eof {
			return false, nil, false, "http-premature-eof"
		}
		return false, nil, false, ""
	default:
		if eof {
			return true, wire, false, ""
		}
		return false, nil, false, ""
	}
}

// exchangeH2 is the M212b negotiated-h2 exchange: HEADERS+DATA on the
// next stream of the pooled connection, the same page/request budgets as
// the h1 path, the same content-decoded result shape through
// httpc.H2Result. A completed stream means the connection is reusable —
// the next fetch rides a fresh stream id, and the marker names both so
// the gate can see the reuse. Every codec refusal surfaces by its own
// name; nothing downgrades past the negotiated protocol.
func (a *app) exchangeH2(p *pooledConn, u webrender.URL, capn int, mainPage bool) (string, []byte, bool, bool, string) {
	if a.cancelBetweenReads() {
		return "", nil, false, false, "cancelled"
	}
	if p.secure != nil {
		p.secure.SetDeadline(a.stageEnd(5_000_000_000))
	}
	extra := []h2.Field{{Name: "accept-encoding", Value: "gzip"}}
	if cookie := a.cookieHeaderFor(u.Host, u.Path); cookie != "" &&
		len(cookie)+8 <= maxHTTPLine && !strings.ContainsAny(cookie, "\x00\r\n") {
		extra = append(extra, h2.Field{Name: "cookie", Value: cookie})
	}
	// The codec's per-response DATA bound is the h1 path's wire slack —
	// a gzip body decodes larger than its wire form.
	p.h2c.MaxBody = capn + 64*1024
	vi.ConsoleLine(markerFetch + u.Host + u.Path +
		" proto=h2 stream=" + itoa(int(p.h2c.NextStreamID())))
	resp, err := p.h2c.GetWith(urlAuthority(u), u.Path, extra)
	if a.cancelBetweenReads() {
		return "", nil, false, false, "cancelled"
	}
	if err != nil {
		kind := h2ErrorKind(err)
		if kind == "h2-refused" {
			vi.ConsoleLine("web: h2-refuse " + err.Error())
		}
		return "", nil, false, false, kind
	}
	head, body, truncated, rerr := httpc.H2Result(resp, capn)
	if rerr != "" {
		return "", nil, false, false, rerr
	}
	if truncated && !mainPage {
		return "", nil, false, false, "http-body-limit"
	}
	return head, body, truncated, true, ""
}

// h2ErrorKind maps an h2 exchange failure onto the consumer's refusal
// kinds: the transport's own timeout/close keep their h1 names, the
// codec's body bound shares the h1 name, and every "h2: ... refused" the
// codec names is one kind — the wire name rides the h2-refuse marker the
// line above prints.
func h2ErrorKind(err error) string {
	switch {
	case tls.IsTimeout(err):
		return "timeout"
	case tls.IsPeerClosed(err):
		return "http-premature-eof"
	case errors.Is(err, h2.ErrBodyTooLarge):
		return "http-body-limit"
	case strings.HasPrefix(err.Error(), "h2: "):
		return "h2-refused"
	}
	return "tls"
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
		head, body, _, kind := a.fetchNetwork(u, capn, false)
		if kind != "" {
			return nil, kind
		}
		code := webrender.HTTPStatus(head)
		if isPageRedirect(code) {
			if hops >= maxRedirects {
				return nil, "redirect-loop"
			}
			next, kind := redirectTarget(u, webrender.LocationHeader(head))
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
		return body, ""
	}
}
