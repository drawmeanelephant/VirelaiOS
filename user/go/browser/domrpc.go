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
	"strings"
	"unsafe"

	"virelai/vi"
	"virelai/webrender"
)

const (
	domRpcBin       = "QJS.BIN"
	domRpcOpsPath   = "/host/WEBRPC.OPS"
	domRpcReplyPath = "/host/WEBRPC.OUT"
	// domRpcSrcMax is one eval op's source bound; domRpcOpsMax the staged
	// ops stream's bound — it mirrors domrpc.ops_max in the worker so the
	// wire can never carry an op the protocol will not accept.
	domRpcSrcMax    = 262144
	domRpcOpsMax    = 1_114_112
	domRpcReplyMax  = 262144
	domRpcMaxScripts = 32
	domRpcBudgetNs  = int64(60) * 1_000_000_000
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
// script), deadline the kill point. A worker is done when the process exits
// AND every op got its reply — anything short is "worker gone".
type scriptWorker struct {
	pid      int64
	ops      int
	deadline int64
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
	hello, _ := json.Marshal(domRpcOp{Op: "hello", Version: 1})
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

type domRpcReply struct {
	Op      string `json:"op"`
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Version int    `json:"version"`
}

// noteScriptReply turns one reply line into its marker — `web: script rc=`
// is the eval round-trip line the card pins.
func (a *app) noteScriptReply(line []byte) {
	var r domRpcReply
	if err := json.Unmarshal(line, &r); err != nil {
		vi.ConsoleLine(markerScript + "reply malformed")
		return
	}
	switch r.Op {
	case "hello":
		if r.OK {
			vi.ConsoleLine(markerScript + "hello version=" + itoa(r.Version))
		} else {
			vi.ConsoleLine(markerScript + "hello refused " + r.Error)
		}
	case "eval":
		if r.OK {
			vi.ConsoleLine(markerScript + "rc=0")
		} else {
			vi.ConsoleLine(markerScript + "rc=70 error=" + r.Error)
		}
	default:
		vi.ConsoleLine(markerScript + "reply " + r.Op + " refused " + r.Error)
	}
}

// drainReplies reads the reply spool after the worker is gone and emits one
// marker per reply line. The count answers "did every op get its reply".
func (a *app) drainReplies() int {
	body, rc := vi.ReadFileAll(domRpcReplyPath+"~", domRpcReplyMax)
	if rc < 0 {
		return -1
	}
	reps := 0
	for _, line := range bytes.Split(body, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		reps++
		a.noteScriptReply(line)
	}
	return reps
}

// pollScripts is the event-loop service: it costs nothing while the worker
// runs, and on exit (or the kill deadline) drains the reply spool and names
// the teardown — `web: script done` when every op was answered and the
// worker left cleanly, `web: script worker gone` otherwise.
func (a *app) pollScripts() {
	w := a.rpc
	if w == nil {
		return
	}
	st, state := vi.Probe(w.pid)
	if state == vi.ProbeRunning {
		if vi.Nanos() < w.deadline {
			return
		}
		vi.Kill(uint64(w.pid)) // wedged: armed, exit lands on its own
	}
	a.rpc = nil
	reps := a.drainReplies()
	if state != vi.ProbeExited && state != vi.ProbeAbsent {
		vi.WaitBudget(w.pid, int64(2)*1_000_000_000) // let the armed kill land
	}
	if reps == w.ops && (state == vi.ProbeExited && st == 0 || state == vi.ProbeAbsent) {
		vi.ConsoleLine(markerScript + "done")
	} else {
		vi.ConsoleLine(markerScript + "worker gone")
	}
}

// closeScripts is the ordered-teardown half: a navigation or a quit retires
// the page's worker — kill it if live, let the armed exit land, then name
// what its replies showed.
func (a *app) closeScripts() {
	w := a.rpc
	if w == nil {
		return
	}
	a.rpc = nil
	_, state := vi.Probe(w.pid)
	if state == vi.ProbeRunning {
		vi.Kill(uint64(w.pid))
		vi.WaitBudget(w.pid, int64(2)*1_000_000_000)
	}
	a.drainReplies()
	vi.ConsoleLine(markerScript + "worker gone")
}
