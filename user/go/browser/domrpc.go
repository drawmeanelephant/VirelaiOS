// M211a (#3867): WEB is ADR 0039's one bounded JS consumer. A page carrying
// <script> gets a QJS.BIN --dom-rpc worker exec'd with the JSON-lines
// channel bound to its inherited streams: the staged ops spool becomes the
// child's fd-0 and a reply spool its fd-1, while fd-2 keeps the console so
// worker diagnostics stay on serial. The wire is the whole capability —
// file, net and window handles never cross it. The page paints first and
// the worker state machine runs off the event loop, so a slow or absent
// worker can never hold up a frame.

package main

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"unsafe"

	"virelai/vi"
	"virelai/vsys"
	"virelai/webrender"
)

const (
	domRpcBin       = "QJS.BIN"
	domRpcOpsPath   = "/host/WEBRPC.OPS"
	domRpcReplyPath = "/host/WEBRPC.OUT"
	// domRpcAnsPath (M211b #3868) is the parent→worker half of a DOM
	// request: WEB publishes each answer atomically here, the worker
	// re-opens it per poll (the share clamps reads to open-time size).
	domRpcAnsPath   = "/host/WEBRPC.ANS"
	// domRpcSrcMax is one eval op's source bound; domRpcOpsMax the staged
	// ops stream's bound — it mirrors domrpc.ops_max in the worker so the
	// wire can never carry an op the protocol will not accept.
	domRpcSrcMax    = 262144
	domRpcOpsMax    = 1_114_112
	domRpcReplyMax  = 262144
	domRpcMaxScripts = 32
	// domRpcBudgetNs is the worker's kill deadline. M211b: a DOM request
	// round trip rides share-latency reads on both ends (~1s/ask worst
	// case); a 64-op script can legitimately need ~70s inside the wire's
	// own budgets. Two minutes stays bounded and covers the envelope.
	domRpcBudgetNs  = int64(120) * 1_000_000_000
	// domRpcVersion is the wire handshake (domrpc.version worker-side).
	// v3 admits the write ops and the timer pair: `timer.set` grants an
	// id synchronously, `timer.next` parks until an app-timer fire makes
	// an id due (M211c #3869).
	domRpcVersion   = 3
	// domValueMax mirrors domrpc.value_max — one answered value's clip.
	domValueMax     = 2048
	// The v3 bounds mirror domrpc.script_mut_max / timer_pending_max /
	// timer_delay_max_ms: mutations per script refuse `script_budget_mut`,
	// granted-but-unfired timers refuse `script_budget_timers`, an
	// over-bound delay refuses `dom_op_bounds`. The delay bound stays
	// under the worker's 10s rendezvous wait so a parked `timer.next`
	// answer always lands inside the wire's own patience.
	domRpcMaxMut      = 16
	domRpcMaxTimers   = 8
	domRpcTimerMaxMs  = 5000
	// markerDomNamesClip bounds how much of a console.log line reaches
	// the serial marker — the op's payload bound is the wire's, not the
	// console's.
	domConsoleClip  = 240
)

// The exec stream-request ABI (kernel/src/file_table.zig StreamRequest):
// bit 63 in argc plus a 32-byte request whose sources[i] bind the child's
// fd-i. A file source MOVES out of the parent's handle table on a
// successful spawn, so each handle is opened fresh per worker and never
// closed by us afterward — on a refused spawn the plan is cancelled and the
// handles are still ours to close. The plain slot-28 form carries argv only.
type streamRequest struct {
	version  uint32
	reserved uint32
	sources  [3]uint64
}

const (
	streamInherit   = ^uint64(0)
	slotExec        = 28
	execStreamFlag  = uintptr(1) << 63
	streamRequestSz = 32
)

//go:linkname execsvc6 virelai/vi.syscall6
func execsvc6(num uintptr, a0, a1, a2, a3, a4, a5 uintptr) int64

func strPtr(s string) uintptr {
	return uintptr(unsafe.Pointer(unsafe.StringData(s)))
}

// scriptWorker is the one in-flight worker: pid from the stream-bound exec,
// ops the number of op lines staged (one hello + one eval per shipped
// script), reps the hello/eval reply lines seen, off the reply-spool drain
// cursor (mid-run reads re-open the file so appended bytes stay visible),
// domOps the DOM requests served, deadline the kill point. M211c adds the
// write/timer half: mut the served writes in the current per-script
// window, timers the granted-but-unfired set, due the fired ids awaiting
// a `timer.next` ask, waitSeq/waitParked the parked ask itself, nextTimer
// the id counter. A worker is done when the process exits AND every op
// got its reply — anything short is "worker gone".
type scriptWorker struct {
	pid        int64
	ops        int
	reps       int
	off        int
	domOps     int
	lastDrain  int64
	deadline   int64
	mut        int
	timers     []scriptTimer
	due        []int
	waitSeq    int64
	waitParked bool
	nextTimer  int
}

// scriptTimer is one granted setTimeout: the id the worker's JS table
// keys on and the deadline the app timer posts EvTimer at.
type scriptTimer struct {
	id  int
	due int64
}

// collectScripts walks the loaded document in document order and returns
// the sources to eval: inline script text, or the fetched body for src=.
// A refused or over-bound source is skipped with a named marker, never
// silently shipped as an empty eval.
func (a *app) collectScripts() [][]byte {
	var srcs [][]byte
	var walk func(*webrender.Node)
	walk = func(n *webrender.Node) {
		if a.pageCancelled || a.quit || len(srcs) >= domRpcMaxScripts {
			return
		}
		if n.Kind == webrender.KindElement && n.Tag == "script" {
			if src := strings.TrimSpace(n.Attr("src")); src != "" {
				data, kind := a.fetchResource(relativeTo(a.target, src), domRpcSrcMax)
				if kind != "" {
					vi.ConsoleLine(markerScript + "skip " + kind)
				} else if len(data) > 0 {
					srcs = append(srcs, data)
				}
			} else {
				var b strings.Builder
				for _, c := range n.Children {
					if c.Kind == webrender.KindText {
						b.WriteString(c.Text)
					}
				}
				s := b.String()
				switch {
				case strings.TrimSpace(s) == "":
				case len(s) > domRpcSrcMax:
					vi.ConsoleLine(markerScript + "skip src-limit")
				default:
					srcs = append(srcs, []byte(s))
				}
			}
			return // a script node's children are raw text, never elements
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(a.doc.Root)
	return srcs
}

type domRpcOp struct {
	Op      string `json:"op"`
	Version int    `json:"version,omitempty"`
	Src     string `json:"src,omitempty"`
}

// stageDomRpcOps writes the ops spool: a hello handshake then one eval op
// per shipped script. The op list clips at the wire bound with a named
// marker rather than carry a line the worker's parser must refuse.
func (a *app) stageDomRpcOps(srcs [][]byte) (int, bool) {
	var buf bytes.Buffer
	hello, _ := json.Marshal(domRpcOp{Op: "hello", Version: domRpcVersion})
	buf.Write(hello)
	buf.WriteByte('\n')
	shipped := 0
	for _, s := range srcs {
		line, _ := json.Marshal(domRpcOp{Op: "eval", Src: string(s)})
		if buf.Len()+len(line)+1 > domRpcOpsMax {
			vi.ConsoleLine(markerScript + "skip ops-limit")
			break
		}
		buf.Write(line)
		buf.WriteByte('\n')
		shipped++
	}
	// The ops spool publishes atomically — a torn write would hand the
	// worker a clipped JSON-lines stream it cannot tell from a complete one.
	if vi.WriteFileSafe(domRpcOpsPath, buf.Bytes()) < 0 {
		return 0, false
	}
	return 1 + shipped, true
}

// startScripts runs at afterLoad's end — the page's ready markers already
// painted, so nothing here can hold up the frame. A page without script
// nodes costs nothing; a scripted page gets the worker.
func (a *app) startScripts() {
	if a.doc == nil || a.rpc != nil {
		return
	}
	srcs := a.collectScripts()
	if len(srcs) == 0 {
		return
	}
	vi.ConsoleLine(markerScript + "n=" + itoa(len(srcs)))
	ops, ok := a.stageDomRpcOps(srcs)
	if !ok {
		vi.ConsoleLine(markerScript + "stage-fail")
		return
	}
	hOps, rc := vi.FileOpen(domRpcOpsPath, vi.ModeRead)
	if rc < 0 {
		vi.ConsoleLine(markerScript + "stage-fail")
		return
	}
	// The reply spool is the child's sacrificial write target — the `~` name
	// is the codebase's scratch-file marker (it must exist empty pre-exec so
	// the worker's fd-1 writes land from offset 0; stale replies from an
	// earlier page can never masquerade as this one's).
	hReply, rrc := vi.FileOpen(domRpcReplyPath+"~", vi.ModeWrite|vi.ModeCreate)
	if rrc < 0 {
		vi.FileClose(uint32(hOps))
		vi.ConsoleLine(markerScript + "stage-fail")
		return
	}
	// M211b: arm the answer rendezvous before exec — the seq-0 sentinel
	// overwrites any previous page's last answer, so a fresh worker's
	// seq-1 request can never match a stale line.
	if vi.WriteFileSafe(domRpcAnsPath, []byte("{\"seq\":0}\n")) < 0 {
		vi.FileClose(uint32(hOps))
		vi.FileClose(uint32(hReply))
		vi.ConsoleLine(markerScript + "stage-fail")
		return
	}
	var argv [8 * 256]byte
	copy(argv[:], "--dom-rpc")
	req := streamRequest{version: 1,
		sources: [3]uint64{uint64(hOps), uint64(hReply), streamInherit}}
	pid := execsvc6(slotExec, strPtr(domRpcBin), uintptr(len(domRpcBin)),
		uintptr(unsafe.Pointer(&argv[0])), uintptr(1)|execStreamFlag,
		uintptr(unsafe.Pointer(&req)), streamRequestSz)
	if pid < 0 {
		// Refused spawn: the plan is cancelled, both handles are still ours.
		vi.FileClose(uint32(hOps))
		vi.FileClose(uint32(hReply))
		vi.ConsoleLine(markerScript + "worker gone")
		return
	}
	a.rpc = &scriptWorker{pid: pid, ops: ops,
		deadline: vi.Nanos() + domRpcBudgetNs}
	vi.ConsoleLine(markerScript + "worker pid=" + itoa64(pid))
}

// The reply stream's line vocabulary: hello/eval replies carry ok/error/
// version (and M211b's per-eval DOM op count); a DOM request line from
// the worker carries seq plus the generic a/b args — seq is what tells a
// request apart from a reply.
type domRpcReply struct {
	Op      string `json:"op"`
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Version int    `json:"version"`
	Seq     *int64 `json:"seq"`
	A       string `json:"a"`
	B       string `json:"b"`
	Ops     int    `json:"ops"`
}

// noteWireLine routes one appended reply-spool line: a seq line is a DOM
// request to answer mid-run, anything else is a reply to a staged op and
// counts toward "every op got its reply".
func (a *app) noteWireLine(w *scriptWorker, line []byte) {
	var r domRpcReply
	if err := json.Unmarshal(line, &r); err != nil {
		vi.ConsoleLine(markerScript + "reply malformed")
		return
	}
	if r.Seq != nil {
		a.serveDom(w, r.Op, *r.Seq, r.A, r.B)
		return
	}
	// M211c: a `fire` line is a timer callback's receipt, not a staged
	// op's reply — it never counts toward "every op got its reply". Its
	// arrival also closes a per-script mutation window, same as an eval.
	if r.Op == "fire" {
		w.mut = 0
		if r.OK {
			vi.ConsoleLine(markerScript + "fire id=" + r.A + " rc=0 ops=" + itoa(r.Ops))
		} else {
			vi.ConsoleLine(markerScript + "fire id=" + r.A + " error=" + r.Error)
		}
		return
	}
	w.reps++
	switch r.Op {
	case "hello":
		if r.OK {
			vi.ConsoleLine(markerScript + "hello version=" + itoa(r.Version))
		} else {
			vi.ConsoleLine(markerScript + "hello refused " + r.Error)
		}
	case "eval":
		// M211c: the reply closes the script — the mutation window resets
		// so the next eval (or fire) starts a fresh per-script budget.
		w.mut = 0
		if r.OK {
			vi.ConsoleLine(markerScript + "rc=0 ops=" + itoa(r.Ops))
		} else {
			vi.ConsoleLine(markerScript + "rc=70 error=" + r.Error)
		}
	default:
		vi.ConsoleLine(markerScript + "reply " + r.Op + " refused " + r.Error)
	}
}

// drainInterval paces mid-run servicing: a full ReadFileAll on the host
// share costs tens of ms, so draining on every 1ms pass pegs WEB at 100%
// CPU and starves the servicing itself (observed: ~1 op/sec storms). At
// ~20ms a request round trip still lands in a frame or two. The final
// drain after worker exit is unconditional — nothing waits there.
const drainInterval = int64(20) * 1_000_000

// serviceReplies drains the reply spool's appended lines at the poll
// cadence — while the worker runs this is what makes a DOM request a
// round trip rather than a deadlock; after it exits the same pass is the
// final drain. A partial trailing line waits for its newline rather than
// being parsed half-written.
func (a *app) serviceReplies(w *scriptWorker) {
	w.lastDrain = vi.Nanos()
	body, rc := vi.ReadFileAll(domRpcReplyPath+"~", domRpcReplyMax)
	if rc >= 0 && len(body) >= w.off {
		rest := body[w.off:]
		for {
			nl := bytes.IndexByte(rest, '\n')
			if nl < 0 {
				break
			}
			line := bytes.TrimSpace(rest[:nl])
			w.off += nl + 1
			rest = rest[nl+1:]
			if len(line) > 0 {
				a.noteWireLine(w, line)
			}
		}
	}
	// M211c: writes batch — one reflow + repaint per drain pass, never a
	// re-layout per op. `web: dom reflow` lands AFTER the present so a
	// gate snapshot-on-marker sees the mutated frame on the scanout.
	if a.mutated {
		a.mutated = false
		a.flushScriptMutations()
	}
}

// flushScriptMutations re-enters the existing layout→paint pipeline on
// the mutated document: relayout() is the same whole-tree reflow a zoom
// change drives — there is no second render path for scripted writes —
// then render() presents the new frame. An absent document or layout
// (an error page mid-load) just drops the flag.
func (a *app) flushScriptMutations() {
	if a.doc == nil || a.lay == nil {
		return
	}
	a.relayout()
	a.render()
	vi.ConsoleLine(markerDom + "reflow")
}

// domOpRefused names the op vocabulary the wire knows but v3 does not
// admit — traversal, doc.write, attribute/style writes, interval/raf
// timers, fetch. A name outside even that answers dom_op_unknown.
var domOpRefused = map[string]bool{
	"nodes.byTag": true, "nodes.byClass": true, "node.query": true,
	"doc.write": true, "node.setAttr": true,
	"timer.interval": true, "timer.raf": true, "net.fetch": true,
}

// domAnswer is one answer line for WEBRPC.ANS — value stays a pointer so
// an absent node or attribute publishes JSON null, not an empty string.
type domAnswer struct {
	Op      string  `json:"op"`
	Seq     int64   `json:"seq"`
	OK      bool    `json:"ok"`
	Value   *string `json:"value"`
	Error   string  `json:"error,omitempty"`
	Clipped bool    `json:"clipped,omitempty"`
}

// clipDomValue bounds one answered value at the wire's dom_value_max.
func clipDomValue(s string) (*string, bool) {
	if len(s) <= domValueMax {
		return &s, false
	}
	v := s[:domValueMax]
	return &v, true
}

// domClipMarker bounds a console.log line for the serial marker — one
// line, no embedded newlines, clipped at the console's own bound.
func domClipMarker(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)
	if len(s) > domConsoleClip {
		return s[:domConsoleClip]
	}
	return s
}

// serveDom answers one DOM request line against the page's own state —
// the ops read the retained node tree, never a copy: doc.title answers
// the <title> element, node.text/node.attr the id-addressed node
// (ByID is the seam the parse keeps), console.log prints its serial
// marker and acks. Every outcome also lands on the wire as an answer
// line — refused and unknown alike — and on serial as `web: dom`.
func (a *app) serveDom(w *scriptWorker, op string, seq int64, arg, barg string) {
	w.domOps++
	ans := domAnswer{Op: op, Seq: seq}
	outcome := "ok"
	switch op {
	case "doc.title":
		title := docTitle(a.doc)
		ans.OK, ans.Value = true, &title
	case "node.text":
		if n := a.doc.ByID(arg); n != nil {
			ans.OK = true
			ans.Value, ans.Clipped = clipDomValue(webrender.TextContent(n))
		} else {
			ans.OK = true
		}
	case "node.attr":
		if n := a.doc.ByID(arg); n != nil && n.HasAttr(barg) {
			ans.OK = true
			ans.Value, ans.Clipped = clipDomValue(n.Attr(barg))
		} else {
			ans.OK = true
		}
	case "console.log":
		vi.ConsoleLine(markerConsole + domClipMarker(arg))
		ans.OK = true
		empty := ""
		ans.Value = &empty
	// M211c (#3869): the write half. A served write mutates the retained
	// tree and sets the dirty flag — the batched reflow+repaint runs at
	// the end of the drain pass through the existing pipeline. An absent
	// target answers null (the read ops' contract), not a refusal, and
	// spends no mutation budget.
	case "node.setText":
		if w.mut >= domRpcMaxMut {
			outcome = "script_budget_mut"
			ans.Error = outcome
			break
		}
		if n := a.doc.ByID(arg); n != nil {
			n.SetText(barg)
			a.doc.InvalidateIndex()
			w.mut++
			a.mutated = true
			ans.OK = true
			empty := ""
			ans.Value = &empty
		} else {
			ans.OK = true
		}
	case "field.setValue":
		if w.mut >= domRpcMaxMut {
			outcome = "script_budget_mut"
			ans.Error = outcome
			break
		}
		// The value lands on the node's attribute — the same place the
		// form scan re-reads after the reflow's initForms, so a scripted
		// value survives exactly like a typed one.
		if n := a.doc.ByID(arg); n != nil && webrender.FormControl(n.Tag) {
			n.SetAttr("value", barg)
			w.mut++
			a.mutated = true
			ans.OK = true
			empty := ""
			ans.Value = &empty
		} else {
			ans.OK = true
		}
	// M211c: `timer.set` grants an id and arms the app's one kernel timer
	// at the earliest deadline; `timer.next` is the worker's drain ask —
	// it answers a fired id at once, parks when timers are pending but
	// none due, and answers null when nothing could ever fire.
	case "timer.set":
		ms, ok := timerDelayMs(arg)
		switch {
		case !ok:
			outcome = "dom_op_bounds"
			ans.Error = outcome
		case len(w.timers)+len(w.due) >= domRpcMaxTimers:
			outcome = "script_budget_timers"
			ans.Error = outcome
		default:
			w.nextTimer++
			w.timers = append(w.timers, scriptTimer{
				id:  w.nextTimer,
				due: vi.Nanos() + ms*1_000_000,
			})
			a.armScriptTimer()
			ans.OK = true
			id := itoa(w.nextTimer)
			ans.Value = &id
		}
	case "timer.next":
		if len(w.due) > 0 {
			id := w.due[0]
			w.due = w.due[1:]
			ans.OK = true
			v := itoa(id)
			ans.Value = &v
		} else if len(w.timers) == 0 {
			ans.OK = true // nothing pending — the drain ends on null
		} else {
			// Parked: the answer publishes from the EvTimer path when a
			// timer comes due, or never — the kill deadline bounds both.
			w.waitSeq, w.waitParked = seq, true
			vi.ConsoleLine(markerDom + op + " wait")
			return
		}
	default:
		if domOpRefused[op] {
			outcome = "dom_op_refused"
		} else {
			outcome = "dom_op_unknown"
		}
		ans.Error = outcome
	}
	if op != "console.log" {
		vi.ConsoleLine(markerDom + op + " " + outcome)
	}
	if line, err := json.Marshal(&ans); err == nil {
		vi.WriteFileSafe(domRpcAnsPath, append(line, '\n'))
	}
}

// timerDelayMs parses one `timer.set` delay argument: a bare non-negative
// millisecond count inside the wire's delay bound, nothing else.
func timerDelayMs(s string) (int64, bool) {
	ms, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	if err != nil || ms < 0 || ms > domRpcTimerMaxMs {
		return 0, false
	}
	return ms, true
}

// armScriptTimer points the process's ONE app timer (vsys.TimerSetNs,
// slot 95) at the earliest granted deadline — re-arming replaces the
// pending arm, so the queue of script timers rides the single kernel
// timer rather than needing one each. Nothing pending disarms it.
func (a *app) armScriptTimer() {
	w := a.rpc
	if w == nil || len(w.timers) == 0 {
		vsys.TimerCancel()
		return
	}
	earliest := w.timers[0].due
	for _, t := range w.timers[1:] {
		if t.due < earliest {
			earliest = t.due
		}
	}
	d := earliest - vi.Nanos()
	if d < 1_000_000 {
		d = 1_000_000
	}
	vsys.TimerSetNs(uint64(d))
}

// onScriptTimer is the EvTimer fan-out: due timers move to the due queue,
// a parked `timer.next` ask takes the first of them as its answer, and
// the app timer re-arms at whatever deadline remains. Fired ids sit in
// the queue until the worker's drain asks for them — the set is bounded
// by timer_pending_max either way.
func (a *app) onScriptTimer() {
	w := a.rpc
	if w == nil {
		return
	}
	now := vi.Nanos()
	keep := w.timers[:0]
	for _, t := range w.timers {
		if t.due <= now {
			w.due = append(w.due, t.id)
		} else {
			keep = append(keep, t)
		}
	}
	w.timers = keep
	if w.waitParked && len(w.due) > 0 {
		a.answerTimerWait(w, w.due[0])
		w.due = w.due[1:]
	}
	a.armScriptTimer()
}

// answerTimerWait publishes the parked `timer.next` answer — the same
// answer shape serveDom writes, delivered late off the timer event.
func (a *app) answerTimerWait(w *scriptWorker, id int) {
	w.waitParked = false
	v := itoa(id)
	ans := domAnswer{Op: "timer.next", Seq: w.waitSeq, OK: true, Value: &v}
	if line, err := json.Marshal(&ans); err == nil {
		vi.WriteFileSafe(domRpcAnsPath, append(line, '\n'))
	}
	vi.ConsoleLine(markerDom + "timer.next ok")
}

// pollScripts is the event-loop service: while the worker runs it
// services the reply spool on the poll cadence (M211b's DOM round trips),
// and on exit (or the kill deadline) drains it once more and names the
// teardown — `web: script done` when every op was answered and the
// worker left cleanly, `web: script worker gone` otherwise.
func (a *app) pollScripts() {
	w := a.rpc
	if w == nil {
		return
	}
	st, state := vi.Probe(w.pid)
	if state == vi.ProbeRunning {
		if vi.Nanos() < w.deadline {
			// M211b: a running worker can block on a DOM request — the
			// reply spool's appended lines are serviced on this cadence,
			// so an ask is a round trip, never a deadlock. The drain is
			// throttled: a share read per 1ms pass costs more than it
			// serves.
			if vi.Nanos()-w.lastDrain >= drainInterval {
				a.serviceReplies(w)
			}
			return
		}
		vi.Kill(uint64(w.pid)) // wedged: armed, exit lands on its own
	}
	a.rpc = nil
	vsys.TimerCancel() // the worker's granted timers retire with it
	a.serviceReplies(w)
	if state != vi.ProbeExited && state != vi.ProbeAbsent {
		vi.WaitBudget(w.pid, int64(2)*1_000_000_000) // let the armed kill land
	}
	if w.reps == w.ops && (state == vi.ProbeExited && st == 0 || state == vi.ProbeAbsent) {
		vi.ConsoleLine(markerScript + "done ops=" + itoa(w.domOps))
	} else {
		vi.ConsoleLine(markerScript + "worker gone")
	}
}

// closeScripts is the ordered-teardown half: a navigation or a quit retires
// the page's worker — kill it if live, let the armed exit land, then name
// what its replies showed. The app timer goes with the worker: a pending
// arm must never fire into the next page's queue.
func (a *app) closeScripts() {
	w := a.rpc
	vsys.TimerCancel()
	if w == nil {
		return
	}
	a.rpc = nil
	_, state := vi.Probe(w.pid)
	if state == vi.ProbeRunning {
		vi.Kill(uint64(w.pid))
		vi.WaitBudget(w.pid, int64(2)*1_000_000_000)
	}
	a.serviceReplies(w)
	vi.ConsoleLine(markerScript + "worker gone")
}
