// WEB.ELF — the VirelaiOS browser (Go, EL0).
//
// The app shell (window, chrome, navigation, history, HTTP fetch) is Go, and
// the page renderer is the project's own Go library (virelai/webrender):
// parse -> UA style table -> block/inline layout -> span paint. There is no
// JavaScript and no CSS cascade by design; the UI says so out loud.
//
// Usage:  exec WEB.ELF /host/PAGE.HTML     (file channel)
//
//	exec WEB.ELF http://10.0.0.2/     (TCP fetch; IP literals only)
//	exec WEB.ELF https://10.0.0.2:24533/  (in-process TLS; fixture SNI)
//	exec WEB.ELF @/host/RSS.LINK      (first line of that file is the page)
//
// vi.Exec allows 255 bytes per argument. Callers can split longer URLs
// across argv slots (joined with no separator) or pass @path to a file whose
// first line is the URL.
package main

import (
	"strings"

	"virelai/tls"
	"virelai/vi"
	"virelai/webrender"
)

// Window geometry matches the in-guest Zig renderer's window (DOC.BIN) so the
// same scanout probes apply.
const (
	winX = 40
	winY = 28
	winW = 512
	winH = 384

	// The name the WM peer lookup resolves this process by, and the tab title
	// the rail shows. M69a (#1528).
	appName  = "WEB.ELF"
	appTitle = "Web"
)

// Chrome geometry (all inside the app's own surface).
//
// The kernel paints its OWN window title band over the top 16 rows of every
// user window (observed live: see the live-web pixel probes), so the app's
// chrome starts below it — otherwise the title and the JS indicator would be
// hidden under the kernel band.
const (
	kernelBand = 16
	titleY     = kernelBand
	titleH     = 16
	urlRowY    = titleY + titleH // 32
	urlRowH    = 18
	contentY   = urlRowY + urlRowH // 50
	statusH    = 12
	contentX   = 8
	contentW   = winW - 2*contentX
	contentH   = winH - contentY - statusH

	backX, backY, chipW, chipH = 6, urlRowY + 2, 14, 14
	fwdX                       = 22
	reloadX                    = 38
	urlX                       = 56
	urlY, urlH                 = urlRowY + 2, 14
	urlW                       = winW - urlX - 8
)

// Serial markers. These exact bytes are the live gate's grep targets and are
// pinned by a host test (see history_test.go).
const (
	markerOpen      = "web: open id="
	markerParse     = "web: parse nodes="
	markerLayout    = "web: layout blocks="
	markerURL       = "web: url "
	markerNav       = "web: nav "
	markerNavigated = "web: navigated"
	markerError     = "web: error "
	markerPaint     = "web: paint items="
	markerPollErr   = "web: poll err="
	markerLoop      = "web: poll n="
	markerEvent     = "web: ev "
	markerRepaint   = "web: repaint items="
	markerReady     = "web: ready"
	markerNavReady  = "web: nav-ready"
	markerFetch     = "web: fetch "
	markerRedirect  = "web: redirect n="
	markerStores    = "web: stores "
	markerCache     = "web: cache store "
	markerOffline   = "web: offline "
	markerBookmark  = "web: bookmark "
	markerCleared   = "web: cleared "
	markerDownload  = "web: download "
	markerBudget    = "web: budget "
	markerOver      = "web: budget over "
	markerSettled   = "web: settled"
	markerQuit      = "web: quit"
	// The typography probes. They carry measurable facts (engine name,
	// per-glyph advances, line heights) rather than adjectives, so a silent
	// regression to the 8x8 bitmap fails the class-B gate instead of only
	// looking slightly wrong in a screenshot.
	markerFonts = "web: fonts "
	markerText  = "web: text "
	// M69a (#1528): go-dogfood.spec's ordered marker -- printed once, on the
	// first frame of a LAID-OUT page reaching the scanout (a.lay != nil), so it
	// means "this page rendered", not "the app started".
	markerDogfoodPage = "dogfood: page"
)

// Load bounds. The response read is bounded three ways so a bad network
// cannot hang the app: a byte cap, a wall-clock deadline, and the redirect
// hop cap.
const (
	maxRedirects   = 5
	readDeadlineMs = 30000

	// fixtureSNI is the AutoClaw test-leaf name the runner TLS responder
	// serves. IP-literal https uses this SNI; production roots are a later
	// card. Public-internet hostnames stay a dns refuse.
	fixtureSNI = "leaf.example.com"
)

// HistoryPersistence is where visits are appended (inspectable text, one
// "unix-seconds target" row per visit, after a schema line).
const (
	historyPath   = "/host/WEB-HISTORY.TXT"
	historySchema = "# virelai-web-history v1 (unix-seconds<TAB>target)"
)

// HID usage codes the OS delivers in EvKeyDown.Arg0.
const (
	keyQ        = 0x14
	keyR        = 0x15
	keyEscape   = 0x29
	keyBacksp   = 0x2a
	keyRight    = 0x4f
	keyLeft     = 0x50
	keyDown     = 0x51
	keyUp       = 0x52
	keyPageUp   = 0x4b
	keyPageDown = 0x4e
	keyHome     = 0x4a
	keyEnd      = 0x4d
	keyF5       = 0x3e
	keyX        = 0x1b
	keyB        = 0x05
	keyC        = 0x06
	keyD        = 0x07
	keyK        = 0x0e
	keyS        = 0x16
)

type app struct {
	win    int
	filler vi.Filler
	// text is the engine the page is measured and painted with: real TrueType
	// when the faces load, the 8x8 bitmap when they do not.
	text webrender.TextEngine

	hist   *history
	target string
	title  string

	doc *webrender.Document
	lay *webrender.Layout

	scroll     int
	hover      string
	status     string
	errKind    string
	errMsg     string
	dirty      bool
	quit       bool
	painted    int
	navCount   int
	polls      int
	events     int
	lastFills  int
	logPaint   bool
	loggedPoll bool
	// dogfoodPage is the once-only latch for markerDogfoodPage (M69a #1528).
	dogfoodPage bool

	// In-flight HTTP load. The loop steps the socket instead of blocking
	// inside navigate(), so Stop/cancel and window-close stay live during a
	// load (a browser must not freeze on a slow peer).
	settled  bool
	loading  bool
	lastBody []byte

	// Budget instrumentation. The numbers are printed with the first frame
	// and asserted by the gate; the budgets themselves are stated below.
	tStart   int64 // main() entry
	tNav0    int64 // navigate() entered
	tBody    int64 // body ready (or the error decided)
	tParse   int64
	tLayout  int64
	tPaint   int64
	tSettled int64 // first frame published
	over     string
	loadFrom string
	loadURL  webrender.URL
	loadBuf  []byte
	loadHops int
	loadSeen map[string]bool
	loadEnd  int64 // monotonic deadline (vi.Nanos ns) for the in-flight load
	// loadFramed/loadNeed cache the response framing once the header block
	// arrives: loadNeed >= 0 is the Content-Length body size, -1 means
	// close-delimited (the peer's FIN ends the body), -2 means unsupported
	// framing (chunked, duplicate/bogus Content-Length, over the byte cap).
	loadFramed bool
	loadNeed   int
	chunk      [1024]byte
}

// virender adapts the kernel fill batcher to the renderer's Surface.
type virender struct {
	f   *vi.Filler
	win int
}

func (v virender) Fill(x, y, w, h int, rgb uint32) {
	if w <= 0 || h <= 0 || x < 0 || y < 0 {
		return
	}
	v.f.Rect(v.win, uint32(x), uint32(y), uint32(w), uint32(h), rgb)
}

// Budgets for this machine (Apple silicon host, VZ, software raster, one
// vCPU pair). They are deliberately loose: they exist to catch a structural
// regression (an accidental O(n^2), a fetch on the render path), not to
// micro-benchmark.
const (
	// budgetStartupMs is the cold-start cost the BROWSER owns for a page that
	// is already on the share: main() entry to navigate(), i.e. argv + window +
	// the face loads. It is NOT "cold start to a published frame" — that is
	// settle-ms, which reuses this same bar below. A network page also waits on
	// the peer; that wait is reported separately (wait-ms) and never counted as
	// browser cost.
	//
	// M69d #1531 loads four faces (~1.5 MiB) through the kernel's 2048-byte
	// sys_file_read cap (~750 calls). Measured on the reference host after
	// #1586: startup=3025ms (fonts=3022ms of it, every other phase 0-1ms) and
	// 3029-3054ms across the fourteen live-web boots — i.e. this bar is a
	// FONT-LOAD bar. #1586 removed 7013ms of WM-seat retry that used to sit in
	// this window on every boot with no seat (the shim path), which is what held
	// the number at ~10048ms and printed a budget-over line in every WEB boot
	// (13 of the 14; boot 12 launches GOFETCH) while only three boots asserted
	// its absence. The same four faces read 3027ms before that retry existed.
	// 4000ms stays the loose bar for four faces: a structural regression (faces
	// read twice, a fetch on the render path) still trips it, with ~1s of
	// headroom over the measured cost. parse/layout/paint budgets are unchanged.
	budgetStartupMs = 4000
	budgetRenderMs  = 400 // parse + layout + paint of a page
	budgetParseMs   = 120
	budgetLayoutMs  = 120
	budgetPaintMs   = 250
)

// argvTarget joins exec arguments into the page to open. args[0] is the
// program name. Later slots are concatenated because vi.Exec caps each at
// 255 bytes. A target that starts with '@' is a path; the first line of that
// file is the page (urlFromHandoff).
func argvTarget(args []string) (target string, fromFile bool) {
	if len(args) < 2 {
		return "", false
	}
	joined := strings.Join(args[1:], "")
	if strings.HasPrefix(joined, "@") && len(joined) > 1 {
		return joined[1:], true
	}
	return joined, false
}

// urlFromHandoff is the first line of an @path file, trimmed.
func urlFromHandoff(body []byte) string {
	s := string(body)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func main() {
	t0 := vi.Nanos()
	args := vi.Args()
	target, fromFile := argvTarget(args)
	if fromFile {
		b, rc := vi.ReadFileAll(target, 8192)
		if rc < 0 || len(b) == 0 {
			target = ""
		} else {
			target = urlFromHandoff(b)
		}
	}

	id, rc := vi.WinOpen(winX, winY, winW, winH)
	if rc < 0 || id < 0 {
		vi.ConsoleLine("web: error window")
		vi.Exit(2)
	}
	a := &app{win: id, hist: newHistory(), tStart: t0}
	engine, uiState, monoState := loadTextEngine()
	a.text = engine
	vi.ConsoleLine(markerFonts + engine.Name() + " ui=" + uiState + " mono=" + monoState)
	vi.ConsoleLine(markerText + textProbeString(engine))
	vi.ConsoleLine(markerOpen + itoa(id))
	// M69a (#1528): the dogfood beat's browser TAB. Best-effort ATTACH (kind 5)
	// rather than declare_fullscreen (kind 8): attach lands the page on the
	// seat's strip WITHOUT proposing the full viewport, so the page keeps the
	// 512x384 geometry the live-web gates pin while the seat stops blanking the
	// scanout (it paints the blank desktop only while its strip is empty, and
	// that fill sits ABOVE user windows). With no seat -- the shell shim, WND
	// desktop, or no WM at all -- nothing answers and this app is unchanged.
	// The client allocates the request sequence; the browser sends exactly this
	// one WM request, so it does not share a request id with another operation.
	vi.WmMailRequest(vi.WmRpcKindAttachTab, uint32(id), 0, 0, 0, 0, appTitle, appName)
	// The store inventory is read from disk at boot: it is how the gate sees
	// that a previous run's rows persisted.
	vi.ConsoleLine(a.storeSummary())

	if target == "" {
		a.showStartSurface()
	} else {
		a.navigate(target, "")
	}
	a.settleIfNeeded()
	a.loop()
}

// settleRepaint presents the first frame a second time after the scheduler has
// had a tick to pace the composite. Observed live: a single present
// immediately after a long blocking fetch could leave the scanout on the
// pre-paint buffer, while the fill accounting showed every rect accepted.
func (a *app) settleRepaint() {
	vi.Sleep(30)
	a.render()
	vi.ConsoleLine(markerRepaint + itoa(itemsOf(a)) + " fills=" + itoa(a.lastFills))
	// The gate snapshots on `web: repaint` and *exits* on `web: ready`; the
	// two are deliberately different lines so the framebuffer stream has
	// time to complete before the run ends (observed live: a shared marker
	// let the runner shut down mid-stream and the snapshot never landed).
	vi.Sleep(5)
	vi.ConsoleLine(markerReady)
}

// settleIfNeeded publishes the first frame exactly once, and only once the
// window has real content. With a stepped load the socket can still be in
// flight when the window opens, so `web: settled` must keep meaning "the page
// (or its error page) is on screen" rather than "the window exists".
func (a *app) settleIfNeeded() {
	if a.settled || a.loading {
		return
	}
	if a.lay == nil && a.errKind == "" {
		return
	}
	a.render()
	a.tSettled = vi.Nanos() // absolute; reportBudget takes the deltas
	vi.ConsoleLine(markerSettled)
	a.reportBudget()
	a.settleRepaint()
	a.settled = true
}

// reportBudget prints the measured first-frame cost and flags any stage that
// exceeded its stated budget. The gate asserts the line exists and that no
// `web: budget over` line appears, so a regression fails the run.
func (a *app) reportBudget() {
	ms := func(ns int64) string { return itoa(int(ns / 1000000)) }
	wait := a.tBody - a.tNav0
	startup := a.tNav0 - a.tStart
	render := a.tParse + a.tLayout + a.tPaint
	settle := a.tSettled - a.tStart
	// A missing or non-monotonic timestamp means the instrumentation is
	// broken: say so out loud instead of clamping to a flattering zero. The
	// gate asserts `web: budget over` is absent, so this fails the run.
	switch {
	case a.tNav0 == 0 || a.tBody == 0 || a.tSettled == 0:
		a.over = "invalid"
		vi.ConsoleLine(markerOver + "invalid missing-timestamp")
	case wait < 0 || startup < 0 || settle < 0 || render < 0:
		a.over = "invalid"
		vi.ConsoleLine(markerOver + "invalid non-monotonic")
	}
	vi.ConsoleLine(markerBudget +
		"startup-ms=" + ms(startup) +
		" wait-ms=" + ms(wait) +
		" render-ms=" + ms(render) +
		" settle-ms=" + ms(settle) +
		" parse-ms=" + ms(a.tParse) +
		" layout-ms=" + ms(a.tLayout) +
		" paint-ms=" + ms(a.tPaint))

	checks := []struct {
		name  string
		ms    int
		limit int
	}{
		{"startup", int(startup / 1000000), budgetStartupMs},
		{"render", int(render / 1000000), budgetRenderMs},
		{"parse", int(a.tParse / 1000000), budgetParseMs},
		{"layout", int(a.tLayout / 1000000), budgetLayoutMs},
		{"paint", int(a.tPaint / 1000000), budgetPaintMs},
	}
	// settle is only the browser's to own when nothing had to be fetched.
	if wait == 0 {
		checks = append(checks, struct {
			name  string
			ms    int
			limit int
		}{"settle", int(settle / 1000000), budgetStartupMs})
	}
	for _, st := range checks {
		if st.ms > st.limit {
			a.over = st.name
			vi.ConsoleLine(markerOver + st.name + " " + itoa(st.ms) + "ms")
		}
	}
}

func (a *app) loop() {
	for !a.quit {
		if a.loading {
			a.loadStep()
		}
		a.polls++
		if a.polls%250 == 0 {
			vi.ConsoleLine(markerLoop + itoa(a.polls) + " ev=" + itoa(a.events) + " loading=" + boolStr(a.loading))
		}
		ev, raw, ok := vi.PollEventRaw()
		if !ok {
			if raw < 0 && !a.loggedPoll {
				// A negative poll is a kernel refusal (not an empty queue):
				// surface it instead of spinning silently.
				vi.ConsoleLine(markerPollErr + itoa64(raw))
				a.loggedPoll = true
			}
			// Poll at ~10 Hz: the browser does not need finer input
			// latency, and quiet tasks leave the shell's idle loop (which
			// paces the composite and services the scanout snapshot) room
			// to run on this single-user machine.
			vi.Sleep(10)
			continue
		}
		a.events++
		if a.events <= 8 {
			vi.ConsoleLine(markerEvent + "kind=" + itoa(int(ev.Kind)) + " a0=" + itoa(int(ev.Arg0)) + " a1=" + itoa(int(ev.Arg1)))
		}
		switch ev.Kind {
		case vi.EvWinClose:
			a.quit = true
		case vi.EvKeyDown:
			a.key(ev.Arg0, ev.Flags)
		case vi.EvMouseDown:
			a.click(int(ev.Arg0), int(ev.Arg1))
		case vi.EvMouseMove:
			a.move(int(ev.Arg0), int(ev.Arg1))
		}
		if a.dirty {
			a.render()
			a.dirty = false
		}
	}
	vi.ConsoleLine(markerQuit)
	vi.WinClose(a.win)
	vi.Exit(0)
}

// --- navigation -----------------------------------------------------------

// showStartSurface renders the "nothing loaded" page (still a real page: the
// same pipeline, no special-casing of the viewport).
func (a *app) showStartSurface() {
	body := []byte("<h1>VirelaiOS Browser</h1><p>No target. Pass a path or an HTTP URL on the command line:</p>" +
		"<pre>exec WEB.ELF /host/PAGE.HTML\nexec WEB.ELF http://10.0.0.2/</pre>" +
		"<p>This browser has no JavaScript and no CSS cascade: pages render with a fixed built-in style table.</p>")
	a.target = ""
	a.title = "Start"
	a.loadBody(body, "/")
}

func (a *app) navigate(target, from string) {
	a.tNav0 = vi.Nanos()
	resolved, kind := resolveInput(target)
	switch kind {
	case "empty":
		a.finishError("url", target, from)
		return
	case "unsupported":
		a.finishError("scheme", resolved, from)
		return
	}
	a.target = resolved
	a.scroll = 0
	a.loadFrom = from
	a.loadHops = 0
	a.loadSeen = nil
	vi.ConsoleLine(markerURL + resolved)

	switch kind := classifyTarget(resolved); kind {
	case "https":
		u, ok := webrender.ParseURL(resolved)
		if !ok || !u.IsIP {
			a.finishError("url", resolved, from)
			return
		}
		a.startHTTPS(u)
	case "dns":
		// No resolver in this slice: refuse rather than hang or guess.
		a.finishError("dns", resolved, from)
	case "url":
		a.finishError("url", resolved, from)
	case "http":
		u, _ := webrender.ParseHTTPURL(resolved)
		a.startHTTP(u)
	default:
		body, rc := vi.ReadFileAll(resolved, vi.MaxFileBytes)
		switch {
		case rc < 0:
			a.finishError("file", resolved, from)
		case len(body) == 0:
			a.finishError("empty", resolved, from)
		default:
			a.loadBody(body, resolved)
			a.afterLoad(from)
		}
	}
}

// classifyTarget decides what a resolved target is, and it is deliberately a
// pure function: the "never send an https request in the clear" decision is
// the one thing here that must be unit-testable without a socket.
//
//	kinds: "https" (TLS fetch, IP literal), "dns" (hostname), "url" (malformed),
//	       "http" (cleartext fetch), "file" (file channel)
func classifyTarget(resolved string) string {
	low := strings.ToLower(resolved)
	switch {
	case strings.HasPrefix(low, "https://"):
		u, ok := webrender.ParseURL(resolved)
		if !ok {
			return "url"
		}
		if !u.IsIP {
			return "dns"
		}
		return "https"
	case strings.HasPrefix(low, "http://"):
		u, ok := webrender.ParseHTTPURL(resolved)
		if !ok {
			return "url"
		}
		if !u.IsIP {
			return "dns"
		}
		return "http"
	}
	return "file"
}

// sendAll writes b to the socket in payload-bounded chunks, so a request
// larger than one syscall send (192 B) can never be silently truncated.
func sendAll(b []byte) bool {
	for len(b) > 0 {
		n, rc := vi.TCPSend(b)
		if rc < 0 || n <= 0 {
			return false
		}
		b = b[n:]
	}
	return true
}

// startHTTP connects, sends the GET, and arms the stepped read.
func (a *app) startHTTP(u webrender.URL) {
	if rc := vi.TCPConnect(u.IPv4, u.Port); rc < 0 {
		a.offlineOr("tcp")
		return
	}
	req := webrender.FormatGetRequestWithCookies(u.Host, u.Path, a.cookieHeaderFor(u.Host, u.Path))
	if !sendAll([]byte(req)) {
		vi.TCPClose()
		a.offlineOr("tcp")
		return
	}
	a.loadURL = u
	a.loadBuf = a.loadBuf[:0]
	a.loadFramed = false
	a.loadNeed = 0
	a.loading = true
	a.loadEnd = vi.Nanos() + readDeadlineMs*1_000_000
	vi.ConsoleLine(markerFetch + u.Host + u.Path)
}

// startHTTPS dials in-process (tls.Dial over vi.Dial), sends the GET, and
// reads the response on this goroutine. Single-goroutine fetch: no M65
// follow-up. Concurrent fetch would need one — vi.Conn is one-owner and the
// kernel allows one TCP socket per process.
func (a *app) startHTTPS(u webrender.URL) {
	sni := fixtureSNI
	if !u.IsIP {
		sni = u.Host
	}
	conn, err := tls.Dial(u.Host, u.Port, sni)
	if err != nil {
		a.finishError("tls", a.target, a.loadFrom)
		return
	}
	req := webrender.FormatGetRequestWithCookies(sni, u.Path, a.cookieHeaderFor(u.Host, u.Path))
	if _, err := conn.Write([]byte(req)); err != nil {
		_ = conn.Close()
		a.offlineOr("tls")
		return
	}
	vi.ConsoleLine(markerFetch + u.Host + u.Path)
	buf, err := readTLSConn(conn, vi.MaxFileBytes)
	_ = conn.Close()
	if err != nil && len(buf) == 0 {
		a.offlineOr("tls")
		return
	}
	a.loadURL = u
	a.loadBuf = buf
	a.loading = false
	a.finishResponse(true)
}

func readTLSConn(c *tls.TLSConn, capn int) ([]byte, error) {
	tmp := make([]byte, 16384)
	var out []byte
	for len(out) < capn {
		n, err := c.Read(tmp)
		if n > 0 {
			out = append(out, tmp[:n]...)
		}
		if err != nil || n == 0 {
			if len(out) > 0 {
				return out, nil
			}
			return out, err
		}
	}
	return out, nil
}

// responseNeed returns the response framing of a buffer:
//
//	>= 0  the body is exactly n bytes (Content-Length)
//	-1    close-delimited: no Content-Length, the peer's FIN ends the body
//	-2    unsupported or invalid framing (chunked transfer coding, a
//	      duplicate or non-numeric Content-Length, or a declared body
//	      over vi.MaxFileBytes)
//
// ok is false while the header block has not fully arrived (keep loading).
func responseNeed(raw []byte) (need int, ok bool) {
	head, _, complete := webrender.SplitHTTPResponse(raw)
	if !complete {
		return 0, false
	}
	need = -1
	for _, line := range strings.Split(head, "\n")[1:] {
		i := strings.IndexByte(line, ':')
		if i < 0 {
			continue
		}
		name := strings.TrimSpace(line[:i])
		value := strings.TrimSpace(line[i+1:])
		if strings.EqualFold(name, "Transfer-Encoding") {
			return -2, true // this browser does not decode transfer codings
		}
		if !strings.EqualFold(name, "Content-Length") {
			continue
		}
		if need >= 0 || value == "" {
			return -2, true // duplicate or empty length
		}
		n := 0
		for _, c := range value {
			if c < '0' || c > '9' {
				return -2, true
			}
			if n > (vi.MaxFileBytes-int(c-'0'))/10 {
				return -2, true // the declared body cannot fit the cap
			}
			n = n*10 + int(c-'0')
		}
		need = n
	}
	return need, true
}

// bodyLen is the response body bytes buffered so far (0 before the header
// block completes).
func (a *app) bodyLen() int {
	_, body, ok := webrender.SplitHTTPResponse(a.loadBuf)
	if !ok {
		return 0
	}
	return len(body)
}

// frame parses (once) and caches the response framing.
func (a *app) frame() {
	if a.loadFramed {
		return
	}
	if need, ok := responseNeed(a.loadBuf); ok {
		a.loadFramed, a.loadNeed = true, need
	}
}

// endFail closes the socket and raises a load error.
func (a *app) endFail(kind string) {
	vi.TCPClose()
	a.loading = false
	a.finishError(kind, a.target, a.loadFrom)
}

// endOffline closes the socket and takes the offline fallback.
func (a *app) endOffline(kind string) {
	vi.TCPClose()
	a.loading = false
	a.offlineOr(kind)
}

// loadStep advances an in-flight load by one bounded read.
//
// The body ends by its declared Content-Length, by the peer's FIN (a
// close-delimited body: readiness bit 0 with a drained recv), by the byte
// cap, or by the wall-clock deadline. An empty recv alone is NOT completion —
// the kernel returns 0 while the next segment may still be a poll away — so
// only an explicit framing or the FIN ends the read. The deadline is
// consulted only when a step makes no progress: it bounds waiting, never
// discards queued bytes.
func (a *app) loadStep() {
	mask, rc := vi.TCPReady()
	if rc < 0 {
		a.endOffline("tcp")
		return
	}
	n, rc := vi.TCPRecv(a.chunk[:])
	if rc < 0 {
		a.endOffline("tcp")
		return
	}
	if n > 0 {
		a.loadBuf = append(a.loadBuf, a.chunk[:n]...)
		a.loadEnd = vi.Nanos() + readDeadlineMs*1_000_000
	} else if mask&1 == 0 {
		// No bytes and no FIN: the read is idle, so the deadline applies.
		if vi.Nanos() >= a.loadEnd {
			a.endOffline("timeout")
		}
		return
	}
	a.frame()
	switch {
	case a.loadFramed && a.loadNeed == -2:
		a.endFail("truncated")
	case a.loadFramed && a.loadNeed >= 0:
		if a.bodyLen() >= a.loadNeed {
			a.completeLoad()
			return
		}
		if n == 0 { // the peer closed before the declared body arrived
			a.endFail("truncated")
		}
	case n == 0:
		if len(a.loadBuf) == 0 {
			// The peer closed without answering: not an empty page, and not
			// a truncated one either — take the offline fallback if there is one.
			a.endOffline("tcp")
			return
		}
		a.completeLoad() // close-delimited: the FIN ended the body
	default:
		if len(a.loadBuf) >= vi.MaxFileBytes {
			a.completeLoad()
		}
	}
}

// completeLoad turns a finished response into a page, a redirect step, or an
// error page.
func (a *app) completeLoad() {
	vi.TCPClose()
	a.finishResponse(false)
}

func (a *app) finishResponse(viaTLS bool) {
	a.loading = false
	head, body, ok := webrender.SplitHTTPResponse(a.loadBuf)
	if !ok {
		a.finishError("truncated", a.target, a.loadFrom)
		return
	}
	code := webrender.HTTPStatus(head)
	if webrender.RedirectStatus(code) {
		next, ok := webrender.ResolveRedirect(a.loadURL, webrender.LocationHeader(head))
		if !ok || next.Host == "" {
			a.finishError("redirect", a.target, a.loadFrom)
			return
		}
		key := next.Host + next.Path
		if a.loadSeen == nil {
			a.loadSeen = map[string]bool{}
		}
		if a.loadSeen[key] || a.loadHops >= maxRedirects {
			a.finishError("redirect-loop", a.target, a.loadFrom)
			return
		}
		a.loadSeen[key] = true
		a.loadHops++
		a.target = formatNavURL(next)
		vi.ConsoleLine(markerRedirect + itoa(a.loadHops) + " " + a.target)
		if !next.IsIP {
			a.finishError("dns", a.target, a.loadFrom)
			return
		}
		if next.Scheme == "https" || (viaTLS && next.Scheme != "http") {
			a.startHTTPS(next)
			return
		}
		a.startHTTP(next)
		return
	}
	if code != 200 {
		a.status = "HTTP " + itoa(code)
		a.finishError("http", a.target, a.loadFrom)
		return
	}
	if len(body) == 0 {
		a.finishError("empty", a.target, a.loadFrom)
		return
	}
	a.loadBody(body, a.target)
	if n := a.persistCookies(head); n > 0 {
		vi.ConsoleLine(markerStores + "cookies+" + itoa(n))
	}
	a.cacheStore(a.target, body)
	a.afterLoad(a.loadFrom)
}

func formatNavURL(u webrender.URL) string {
	scheme := u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	def := uint16(80)
	if scheme == "https" {
		def = 443
	}
	if u.Port != 0 && u.Port != def {
		return scheme + "://" + u.Host + ":" + itoa(int(u.Port)) + u.Path
	}
	return scheme + "://" + u.Host + u.Path
}

// cancelLoad stops an in-flight load (Stop / Escape / X).
func (a *app) cancelLoad() {
	if !a.loading {
		a.status = "stopped"
		a.dirty = true
		return
	}
	vi.TCPClose()
	a.loading = false
	a.finishError("cancelled", a.target, a.loadFrom)
}

// offlineOr falls back to the stored copy of a page when the network cannot
// deliver it, and is explicit about it: an offline copy is never passed off as
// a fresh fetch.
func (a *app) offlineOr(kind string) {
	if kind == "tcp" || kind == "timeout" {
		if body, ok := a.cacheLookup(a.target); ok {
			a.loadBody(body, a.target)
			a.status = "offline copy (network unavailable)"
			vi.ConsoleLine(markerOffline + a.target)
			a.afterLoad(a.loadFrom)
			return
		}
	}
	a.finishError(kind, a.target, a.loadFrom)
}

// afterLoad records the visit and announces the navigation once the frame is
// up (see announceNavigation).
func (a *app) afterLoad(from string) {
	a.hist.push(entry{Target: a.target, Title: a.title})
	a.persistHistory(a.target)
	if from != "" {
		a.announceNavigation()
	} else {
		a.settleIfNeeded()
	}
}

// finishError renders the error page for a failed load and records the visit.
func (a *app) finishError(kind, target, from string) {
	if a.loading {
		a.loading = false
	}
	if a.tBody == 0 {
		a.tBody = vi.Nanos()
	}
	a.setError(kind, target)
	a.hist.push(entry{Target: target, Title: a.title})
	a.persistHistory(target)
	if from != "" {
		a.announceNavigation()
	} else {
		a.settleIfNeeded()
	}
}

// loadBody runs the renderer pipeline and emits the parse/layout markers.
func (a *app) loadBody(body []byte, target string) {
	a.errKind, a.errMsg = "", ""
	a.lastBody = body
	a.tBody = vi.Nanos()
	t0 := vi.Nanos()
	a.doc = webrender.ParseHTML(body)
	t1 := vi.Nanos()
	// The engine this page is measured with is stored on the Layout, so Paint
	// draws it with identical metrics; a.resolveImage lets <img> decode without
	// layout ever opening a file itself (ADR 0028 D1/D3).
	a.lay = webrender.LayoutDocument(a.doc, contentW, a.text, a.resolveImage)
	t2 := vi.Nanos()
	a.tParse = t1 - t0
	a.tLayout = t2 - t1
	a.scroll = 0
	vi.ConsoleLine(markerParse + itoa(a.doc.Nodes) + " text=" + itoa(a.doc.TextBytes) + " truncated=" + boolStr(a.doc.Truncated))
	vi.ConsoleLine(markerLayout + itoa(a.lay.Blocks) + " lines=" + itoa(a.lay.Lines) + " h=" + itoa(a.lay.Height))
	a.title = pageTitle(a.doc, target)
	a.status = ""
	a.logPaint = true
	a.dirty = true
}

func (a *app) setError(kind, target string) {
	a.errKind = kind
	a.errMsg = errorMessage(kind, target)
	a.lay = nil
	a.doc = nil
	a.scroll = 0
	a.title = "Error"
	a.logPaint = true
	vi.ConsoleLine(markerError + kind)
	a.dirty = true
}

// --- events ---------------------------------------------------------------

func (a *app) key(usage uint32, flags uint16) {
	alt := flags&vi.ModAlt != 0
	ctrl := flags&vi.ModCtrl != 0
	page := contentH - 16
	if page < 16 {
		page = 16
	}
	switch usage {
	case keyQ:
		a.quit = true
	case keyEscape, keyX:
		a.cancelLoad()
	case keyB:
		if ok, added := a.bookmarkToggle(); ok {
			if added {
				vi.ConsoleLine(markerBookmark + "added")
			} else {
				vi.ConsoleLine(markerBookmark + "removed")
			}
			a.status = "bookmark"
			a.dirty = true
		}
	case keyC:
		vi.ConsoleLine(markerCleared + "cookies " + itoa(a.clearCookies()))
		a.status = "cookies cleared"
		a.dirty = true
	case keyD:
		vi.ConsoleLine(markerCleared + "history-entry " + boolStr(a.historyDeleteNewest()))
		a.status = "history entry deleted"
		a.dirty = true
	case keyK:
		vi.ConsoleLine(markerCleared + "cache " + itoa(a.clearCache()))
		a.status = "cache cleared"
		a.dirty = true
	case keyS:
		if a.saveDownload() {
			a.status = "saved to the share"
			a.dirty = true
		}
	case keyR, keyF5:
		a.reload()
	case keyUp:
		a.scrollBy(-16)
	case keyDown:
		a.scrollBy(16)
	case keyPageUp:
		a.scrollBy(-page)
	case keyPageDown:
		a.scrollBy(page)
	case keyHome:
		a.scroll = 0
		a.dirty = true
	case keyEnd:
		a.scroll = webrender.ScrollMax(a.lay, contentH)
		a.dirty = true
	case keyBacksp, keyLeft:
		if usage == keyLeft && !alt {
			return
		}
		a.goBack()
	case keyRight:
		if !alt && !ctrl {
			return
		}
		a.goForward()
	}
}

func (a *app) click(x, y int) {
	switch {
	case inRect(x, y, backX, backY, chipW, chipH):
		a.goBack()
		return
	case inRect(x, y, fwdX, backY, chipW, chipH):
		a.goForward()
		return
	case inRect(x, y, reloadX, backY, chipW, chipH):
		a.reload()
		return
	}
	if y < contentY || y >= contentY+contentH || a.lay == nil {
		return
	}
	cx := x - contentX
	cy := y - contentY + a.scroll
	if target := webrender.HitTest(a.lay, cx, cy); target != "" {
		from := a.target
		resolved, kind := resolveInput(relativeTo(from, target))
		if kind == "empty" {
			return
		}
		a.navigate(resolved, from)
		if from != "" {
			a.announceNavigation()
		}
	}
}

func (a *app) move(x, y int) {
	if a.lay == nil || y < contentY {
		if a.hover != "" {
			a.hover = ""
			a.dirty = true
		}
		return
	}
	cx := x - contentX
	cy := y - contentY + a.scroll
	if target := webrender.HitTest(a.lay, cx, cy); target != a.hover {
		a.hover = target
		a.dirty = true
	}
}

func (a *app) goBack() {
	if e, ok := a.hist.back(); ok {
		a.navigate(e.Target, "")
		a.hist.replace(e)
		a.announceNavigation()
	}
}

func (a *app) goForward() {
	if e, ok := a.hist.forward(); ok {
		a.navigate(e.Target, "")
		a.hist.replace(e)
		a.announceNavigation()
	}
}

// announceNavigation paints the newly loaded page FIRST and only then prints
// the navigation markers, so `web: navigated` means "the new page is on
// screen" — the gate snapshots on that line. (Observed live: printing the
// markers inside navigate() let the snapshot capture the previous page.)
func (a *app) announceNavigation() {
	// Present, let the shell's idle path composite, then present again — the
	// same shape as settleRepaint, and for the same observed reason: a single
	// present immediately after a blocking fetch can be composited late, so a
	// marker printed right after it would still describe the previous frame.
	a.render()
	vi.Sleep(10)
	a.render()
	a.dirty = false
	a.navCount++
	vi.ConsoleLine(markerNav + a.target)
	vi.ConsoleLine(markerNavigated)
	// A navigation gets its own settle marker: the gate snapshots on
	// `web: navigated` and exits on this line, so the framebuffer stream is
	// never cut off by the run ending (same reason as markerReady).
	vi.Sleep(5)
	vi.ConsoleLine(markerNavReady)
}

func (a *app) reload() {
	if a.target == "" {
		a.showStartSurface()
		return
	}
	a.status = "reloading"
	a.navigate(a.target, "")
}

func (a *app) scrollBy(d int) {
	if a.lay == nil {
		return
	}
	a.scroll += d
	if a.scroll < 0 {
		a.scroll = 0
	}
	if max := webrender.ScrollMax(a.lay, contentH); a.scroll > max {
		a.scroll = max
	}
	a.dirty = true
}

// --- painting -------------------------------------------------------------

func (a *app) render() {
	f := &a.filler
	vs := virender{f: f, win: a.win}
	clip := webrender.Clip{X: 0, Y: 0, W: winW, H: winH}

	// Title band.
	f.Rect(a.win, 0, titleY, winW, titleH, webrender.ColorChromeBg)
	webrender.DrawText(vs, 6, titleY+4, fit("WEB.ELF  "+a.title, (winW-72)/8), 1, false, webrender.ColorChromeInk, clip)
	webrender.DrawText(vs, winW-58, titleY+4, "JS: off", 1, false, webrender.ColorError, clip)

	// URL row.
	f.Rect(a.win, 0, urlRowY, winW, urlRowH, webrender.ColorChromeBg)
	chevBack := webrender.ColorMuted
	if a.hist.canBack() {
		chevBack = webrender.ColorChromeInk
	}
	chevFwd := webrender.ColorMuted
	if a.hist.canForward() {
		chevFwd = webrender.ColorChromeInk
	}
	webrender.DrawText(vs, backX+3, backY+3, "<", 1, false, chevBack, clip)
	webrender.DrawText(vs, fwdX+3, backY+3, ">", 1, false, chevFwd, clip)
	webrender.DrawText(vs, reloadX+3, backY+3, "R", 1, false, webrender.ColorChromeInk, clip)

	f.Rect(a.win, urlX, urlY, urlW, urlH, webrender.ColorSurface)
	f.Rect(a.win, urlX, urlY, urlW, 1, webrender.ColorRule)
	shown := a.target
	if shown == "" {
		shown = "(no target)"
	}
	webrender.DrawText(vs, urlX+4, urlY+3, fit(shown, (urlW-8)/8), 1, false, webrender.ColorChromeInk, clip)

	// Content.
	f.Rect(a.win, 0, contentY, winW, contentH, webrender.ColorPageBg)
	if a.lay != nil {
		webrender.Paint(a.lay, vs, contentX, contentY, contentW, contentH, a.scroll)
		a.painted = len(a.lay.Items)
	} else {
		a.paintErrorPage(vs)
	}

	// Status line (drawn last so nothing can cover it).
	f.Rect(a.win, 0, winH-statusH, winW, statusH, webrender.ColorChromeBg)
	webrender.DrawText(vs, 6, winH-statusH+2, fit(a.statusText(), winW/8-2), 1, false, webrender.ColorMuted, clip)

	t0 := vi.Nanos()
	processed := f.Flush()
	a.lastFills = processed
	vi.WinPresent(a.win)
	a.tPaint = vi.Nanos() - t0
	// M69a (#1528): the page is on the wire. Once only, and only for a real
	// laid-out page (the error page paints with a.lay == nil).
	if !a.dogfoodPage && a.lay != nil {
		a.dogfoodPage = true
		vi.ConsoleLine(markerDogfoodPage)
	}
	if a.logPaint {
		vi.ConsoleLine(markerPaint + itoa(itemsOf(a)) + " fills=" + itoa(processed))
		a.logPaint = false
	}
}

func itemsOf(a *app) int {
	if a.lay == nil {
		return 0
	}
	return len(a.lay.Items)
}

func (a *app) statusText() string {
	if a.hover != "" {
		return "link: " + a.hover
	}
	if a.status != "" {
		return a.status
	}
	if a.lay == nil {
		return "error"
	}
	max := webrender.ScrollMax(a.lay, contentH)
	return "nodes=" + itoa(nodeCount(a.doc)) + " blocks=" + itoa(a.lay.Blocks) +
		" lines=" + itoa(a.lay.Lines) + " items=" + itoa(len(a.lay.Items)) +
		" scroll=" + itoa(a.scroll) + "/" + itoa(max)
}

func (a *app) paintErrorPage(vs virender) {
	clip := webrender.Clip{X: contentX, Y: contentY, W: contentW, H: contentH}
	y := contentY + 12
	webrender.DrawText(vs, contentX, y, "This page could not be loaded", 2, true, webrender.ColorText, clip)
	y += 26
	webrender.DrawText(vs, contentX, y, fit(a.errMsg, contentW/8), 1, false, webrender.ColorError, clip)
	y += 18
	webrender.DrawText(vs, contentX, y, fit("target: "+a.describeTarget(), contentW/8), 1, false, webrender.ColorMuted, clip)
	y += 18
	webrender.DrawText(vs, contentX, y, "kind: "+a.errKind, 1, false, webrender.ColorMuted, clip)
	y += 24
	webrender.DrawText(vs, contentX, y, "R reloads  < back  > forward  Esc stop  Q quit", 1, false, webrender.ColorMuted, clip)
}

func (a *app) describeTarget() string {
	if a.target == "" {
		return "(none)"
	}
	return a.target
}

func (a *app) persistHistory(target string) {
	if !vi.FileExists(historyPath) {
		vi.FileAppend(historyPath, []byte(historySchema+"\n"))
	}
	line := itoa64(vi.Time()) + "\t" + target + "\n"
	vi.FileAppend(historyPath, []byte(line))
}

// --- pure helpers (host-tested) -------------------------------------------

// resolveInput classifies a command-line target or address-bar entry.
func resolveInput(in string) (string, string) {
	s := strings.TrimSpace(in)
	if s == "" {
		return "", "empty"
	}
	// A scheme prefix is recognised before anything else: "javascript:...",
	// "file:///...", "mailto:..." must be refused as schemes, never rewritten
	// into a file-channel path (which is where a bare-name rule would send
	// them).
	if i := strings.IndexByte(s, ':'); i > 0 && !strings.ContainsAny(s[:i], "/?#:") && isSchemeAlpha(s[:i]) {
		switch strings.ToLower(s[:i]) {
		case "http", "https":
			return s, "http"
		}
		return s, "unsupported"
	}
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "http://"), strings.HasPrefix(low, "https://"):
		// Scheme-shaped: hand it on unresolved so classifyTarget can pick
		// https (TLS) vs dns vs url rather than the classifier never
		// seeing it.
		return s, "http"
	case strings.Contains(low, "://"):
		return s, "unsupported"
	case strings.HasPrefix(s, "/"):
		return s, "file"
	}
	return "/host/" + s, "file"
}

// isSchemeAlpha reports whether every byte is an RFC 3986 scheme character.
func isSchemeAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return s != ""
}

// relativeTo resolves an href found on a page against the page's own target.
func relativeTo(base, href string) string {
	low := strings.ToLower(href)
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
		return href
	}
	if strings.HasPrefix(strings.ToLower(base), "https://") {
		if strings.HasPrefix(href, "/") {
			if u, ok := webrender.ParseURL(base); ok {
				return "https://" + u.Host + href
			}
		}
		if resolved, ok := webrender.ResolveHref(base, href); ok {
			return resolved
		}
		return href
	}
	if strings.HasPrefix(strings.ToLower(base), "http://") {
		if strings.HasPrefix(href, "/") {
			if u, ok := webrender.ParseHTTPURL(base); ok {
				return "http://" + u.Host + href
			}
		}
		if resolved, ok := webrender.ResolveHref(base, href); ok {
			return resolved
		}
		return href
	}
	if base == "" {
		return href
	}
	if resolved, ok := webrender.ResolveHref(base, href); ok {
		return resolved
	}
	return href
}

func pageTitle(doc *webrender.Document, target string) string {
	if doc != nil {
		for _, it := range doc.Root.Children {
			if t := firstHeading(it); t != "" {
				return t
			}
		}
	}
	if i := strings.LastIndexByte(target, '/'); i >= 0 && i+1 < len(target) {
		return target[i+1:]
	}
	return target
}

func firstHeading(n *webrender.Node) string {
	if n.Kind == webrender.KindElement && (n.Tag == "h1" || n.Tag == "h2" || n.Tag == "title") {
		if t := strings.TrimSpace(webrender.TextContent(n)); t != "" {
			return t
		}
	}
	for _, c := range n.Children {
		if t := firstHeading(c); t != "" {
			return t
		}
	}
	return ""
}

func nodeCount(doc *webrender.Document) int {
	if doc == nil {
		return 0
	}
	return doc.Nodes
}

// fit truncates s so it fits in cols 8-pixel columns, marking the cut.
func fit(s string, cols int) string {
	if cols < 1 {
		cols = 1
	}
	s = webrender.UpperASCII(s)
	if len(s) <= cols {
		return s
	}
	if cols <= 2 {
		return s[:cols]
	}
	return s[:cols-2] + ".."
}

func inRect(x, y, rx, ry, rw, rh int) bool {
	return x >= rx && x < rx+rw && y >= ry && y < ry+rh
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func errorMessage(kind, target string) string {
	switch kind {
	case "file":
		return "No such file on the share (expected " + target + ")."
	case "dns":
		return "This slice resolves IP literals only; use http://10.0.0.2/ style URLs."
	case "tcp":
		return "TCP connect, send, or receive failed."
	case "timeout":
		return "The server did not answer in time."
	case "truncated":
		return "The response ended before its headers were complete."
	case "empty":
		return "The target produced zero bytes."
	case "http":
		return "The server answered with a non-200 status."
	case "scheme":
		return "Only http://, https://, and the local file channel are supported."
	case "https":
		return "https:// refused: hostname is not an IP literal. Nothing was sent in the clear."
	case "tls":
		return "TLS handshake failed (fail closed). Nothing was sent in the clear."
	case "cancelled":
		return "Load stopped before the server answered."
	case "redirect":
		return "The server sent a redirect we could not resolve."
	case "redirect-loop":
		return "Too many redirects (or a redirect loop); stopped after " + itoa(maxRedirects) + " hops."
	}
	return "Unrecognised target."
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [24]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [24]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
