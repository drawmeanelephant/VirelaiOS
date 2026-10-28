// The agent control channel's verb glue (M205a, card #3808): the verb
// table, the capability check and the per-verb handlers live in
// virelai/agentverbs — this file keeps the daemon's substitutable
// syscall seams, the session context, and the adapters that lift a
// wire request into the library's types. Behaviour on the channel is
// unchanged: Dispatch returns the same result and refusal values the
// daemon emitted before the lift.
package main

import (
	"virelai/agentverbs"
	"virelai/shlib"
	"virelai/vi"
)

// Syscall seams: production wires the real vi/shlib calls; host tests
// substitute fakes (the same seam discipline gotabwm and vi's own tests
// use). They are package vars so a test can swap one without touching
// the others. The dispatcher below delegates through them, so a fake
// lands in the library's seam table at call time.
var (
	procsFn         = func(dst []vi.ProcRow) (int, int64) { return vi.Procs(dst) }
	readFileFn      = vi.ReadFileAll
	writeFileFn     = vi.WriteFileSafe
	deleteFileFn    = vi.FileDelete
	dirListFn       = vi.DirList
	execFn          = vi.Exec
	waitFn          = vi.WaitBudget
	killFn          = vi.Kill
	probeFn         = vi.Probe
	seatPidFn       = vi.WmctlSeatPid
	winOpenFn       = vi.WinOpen
	winHideFn       = vi.WinSetVisible
	wmWindowsFn     = vi.WmWindows
	scanoutFn       = vi.ScanoutRead
	agentInputKeyFn = vi.WmAgentInputKey
	agentInputPtrFn = vi.WmAgentInputPtr
	agentGeomFn     = vi.WmAgentGeometry
	secretGetFn     = shlib.StoreGet
	principalFn     = vi.Principal
	randomFn        = vi.Random
	argsFn          = vi.Args
	listenFn        = vi.Listen
	consoleFn       = vi.ConsoleLine
	sleepFn         = vi.Sleep
	exitFn          = vi.Exit
)

const (
	// agentPort is the fixed v1 listener (D1).
	agentPort = 4747
	// appName is this image's proc-table name — also the self name the
	// WM_RPC peer scan resolves.
	appName = "AGENTD.ELF"
	// secretName is the app-bound HMAC key row (D2).
	secretName = "agent-hmac"
	// sandboxDir is the host-share root fs.* may reach (D3). auditDir is
	// the receipt lane staged beside it (ADR 0052 D1 posture check 5) —
	// unreachable through fs.* (the AUDIT name is reserved, paths.go).
	sandboxDir = "/host/AGENT"
	auditDir   = "/host/AGENT/AUDIT"
	// outPath holds one gosh.run's captured stdout, off the fs.* surface on
	// purpose (D3): it lives inside the audit lane, the sandbox's reserved
	// first segment — agent-writable (M173d: the daemon and its children are
	// uid_agent, and only the agent-owned roots take a write) yet never
	// reachable through fs.*.
	outPath = "/host/AGENT/AUDIT/AGENTD.OUT"
	goshBin = "GOSH.ELF"
)

// Session budgets (D2/D4).
const (
	authBudgetNs = int64(10) * 1e9
	idleBudgetNs = int64(60) * 1e9
	dampingTicks = 1 // 1 s brute-force damping after an auth failure (D2)
)

// The verb bounds the channel code and the host suites still name,
// re-exported from the library so the value lives in exactly one place.
const (
	outMax         = agentverbs.OutMax
	streamChunk    = agentverbs.StreamChunk
	cmdMax         = agentverbs.CmdMax
	timeoutMaxMs   = agentverbs.TimeoutMaxMs
	inputEventsMax = agentverbs.InputEventsMax
	thumbW         = agentverbs.ThumbW
	thumbH         = agentverbs.ThumbH
	fullShotRows   = agentverbs.FullShotRows
	fullShotMinNs  = agentverbs.FullShotMinNs
)

// sessionCtx is what one authenticated connection resolved: the claimed
// agent id, the caps its keyring row declared (ADR 0052 D3 — resolved
// once at auth, never re-read mid-session), the daemon's --allow-exec
// kill switch, the stream writer bounded-output verbs emit chunk lines
// through, and the next instant a full-resolution ui.screenshot may run
// (the per-session rate bound, D3 amendment).
type sessionCtx struct {
	agentID        string
	caps           map[string]bool
	allowExec      bool
	emit           func(chunk []byte)
	nextFullShotNs int64
}

// verbLib is the daemon's dispatcher: the library configured with
// AGENTD's sandbox root, its audit-lane capture path, the reserved AUDIT
// segment, its shell image, its proc-table name and its serial marker
// prefix. Every seam delegates to the package vars above, so a host
// test's fake lands inside the library at call time.
var verbLib = agentverbs.New(agentverbs.Config{
	Sandbox:  sandboxDir,
	OutPath:  outPath,
	Reserved: "AUDIT",
	GoshBin:  goshBin,
	Self:     appName,
	Mark:     "agentd",
	Seams: agentverbs.Seams{
		Procs:      func(dst []vi.ProcRow) (int, int64) { return procsFn(dst) },
		ReadFile:   func(p string, max int) ([]byte, int64) { return readFileFn(p, max) },
		WriteFile:  func(p string, b []byte) int64 { return writeFileFn(p, b) },
		DeleteFile: func(p string) int64 { return deleteFileFn(p) },
		DirList:    func(p string, buf []vi.DirEntry) (int, int64) { return dirListFn(p, buf) },
		Exec:       func(name string, args ...string) (int64, error) { return execFn(name, args...) },
		Wait:       func(pid, budgetNs int64) (int64, error) { return waitFn(pid, budgetNs) },
		Kill:       func(pid uint64) int64 { return killFn(pid) },
		Probe:      func(pid int64) (int64, int) { return probeFn(pid) },
		SeatPid:    func() int64 { return seatPidFn() },
		WinOpen:    func(x, y, w, h uint32) (int, int64) { return winOpenFn(x, y, w, h) },
		WinHide:    func(id int, visible bool) int64 { return winHideFn(id, visible) },
		WmWindows:  func(id uint32, self string) ([]vi.WmWindowRow, error) { return wmWindowsFn(id, self) },
		Scanout:    func(x, y, w, h uint32, buf []byte) int64 { return scanoutFn(x, y, w, h, buf) },
		AgentInputKey: func(id uint32, mods, usage uint8, self string) (vi.WmRpc, bool) {
			return agentInputKeyFn(id, mods, usage, self)
		},
		AgentInputPtr: func(id uint32, buttons uint8, x, y uint16, self string) (vi.WmRpc, bool) {
			return agentInputPtrFn(id, buttons, x, y, self)
		},
		AgentGeometry: func(id uint32, op uint8, target uint32, arg uint8, self string) (vi.WmRpc, bool) {
			return agentGeomFn(id, op, target, arg, self)
		},
		Nanos: func() int64 { return nanosFn() },
		Yield: func() { yieldFn() },
		Sleep: func(t uint64) { sleepFn(t) },
		Log:   func(s string) { consoleFn(s) },
	},
})

// capBits resolves a session's grant words to the library's bitmask;
// words outside the grant set resolve to nothing, as the old map
// lookups answered.
func capBits(caps map[string]bool) agentverbs.Caps {
	var c agentverbs.Caps
	for w := range caps {
		c |= agentverbs.CapWord(w)
	}
	return c
}

// libSess lifts the session context into the library's Session. The
// audit intent hook keeps the D4 pre-spawn receipt in the daemon's own
// lane — it is the one piece of gosh.run that is audit, not verb.
func libSess(sess *sessionCtx, req *request) *agentverbs.Session {
	return &agentverbs.Session{
		Caps:           capBits(sess.caps),
		AllowExec:      sess.allowExec,
		Emit:           sess.emit,
		NextFullShotNs: sess.nextFullShotNs,
		AuditIntent: func() {
			lane.emit(sess.agentID, "gosh.run", auditArgs(req, "", "intent"), "ok")
		},
	}
}

// libReq lifts a validated wire request into the library's projection:
// the verb name plus the argument fields outside {id, verb}.
func libReq(req *request) *agentverbs.Request {
	out := &agentverbs.Request{
		Verb:   req.verb,
		Fields: make(map[string]agentverbs.Field, len(req.fields)),
	}
	for k, f := range req.fields {
		if k == "id" || k == "verb" {
			continue
		}
		nf := agentverbs.Field{Str: f.str, Num: f.num}
		switch f.kind {
		case fStr:
			nf.Kind = agentverbs.FStr
		case fInt:
			nf.Kind = agentverbs.FInt
		case fBool:
			nf.Kind = agentverbs.FBool
		case fArr:
			nf.Kind = agentverbs.FArr
			nf.Arr = libVals(f.arr)
		}
		out.Fields[k] = nf
	}
	return out
}

// libVal lifts one parse-tree node for an array argument (ui.input's
// events[]); the tree the flat grammar admits is mirrored node for node.
func libVal(v jval) agentverbs.Val {
	out := agentverbs.Val{Str: v.str}
	switch v.kind {
	case jObj:
		out.Kind = agentverbs.KObj
		out.Membs = make([]agentverbs.Member, len(v.membs))
		for i, m := range v.membs {
			out.Membs[i] = agentverbs.Member{Key: m.key, Val: libVal(m.val)}
		}
	case jArr:
		out.Kind = agentverbs.KArr
		out.Items = libVals(v.items)
	case jStr:
		out.Kind = agentverbs.KStr
	case jNum:
		out.Kind = agentverbs.KNum
	case jTrue:
		out.Kind = agentverbs.KTrue
	case jFalse:
		out.Kind = agentverbs.KFalse
	default:
		out.Kind = agentverbs.KNull
	}
	return out
}

func libVals(in []jval) []agentverbs.Val {
	out := make([]agentverbs.Val, len(in))
	for i, v := range in {
		out[i] = libVal(v)
	}
	return out
}

// advertisedVerbs is the session's resolved grant set: a verb appears
// iff its cap is on the row (D2), and gosh.run also only when the
// daemon's exec flag is on.
func advertisedVerbs(sess *sessionCtx) []string {
	return agentverbs.Advertised(capBits(sess.caps), sess.allowExec)
}

// dispatch validates a request's argument set against its verb and runs
// it — the library owns the cap gate and every refusal name. It returns
// (resultJSON, "") on success or ("", code) on refusal; the caller owns
// the response envelope and the req/refuse markers. The screenshot rate
// bound is per-session state, so the library's copy writes back here.
func dispatch(req *request, sess *sessionCtx) (string, string) {
	s := libSess(sess, req)
	res, code := verbLib.Dispatch(s, libReq(req))
	sess.nextFullShotNs = s.NextFullShotNs
	return res, code
}

// The per-verb entry points the host suite pins, forwarders onto the
// dispatcher — the tests exercise the same verbs through the same
// seams, just below the dispatch level.
func verbProcList() (string, string) {
	return verbLib.ProcList()
}

func verbProcGet(req *request) (string, string) {
	return verbLib.ProcGet(libReq(req))
}

func verbFSRead(req *request) (string, string) {
	return verbLib.FSRead(libReq(req))
}

func verbFSWrite(req *request) (string, string) {
	return verbLib.FSWrite(libReq(req))
}

func verbUIWindows() (string, string) {
	return verbLib.UIWindows()
}

func verbUIScreenshot(req *request, sess *sessionCtx) (string, string) {
	s := libSess(sess, req)
	res, code := verbLib.UIScreenshot(libReq(req), s)
	sess.nextFullShotNs = s.NextFullShotNs
	return res, code
}

func verbUIInput(req *request) (string, string) {
	return verbLib.UIInput(libReq(req))
}

func verbUIGeometry(req *request, sess *sessionCtx) (string, string) {
	return verbLib.UIGeometry(libReq(req), libSess(sess, req))
}

func verbGoshRun(req *request, sess *sessionCtx) (string, string) {
	return verbLib.GoshRun(libReq(req), libSess(sess, req))
}

// dirExists is the startup /host/AGENT probe (posture check 3).
func dirExists(path string) bool {
	var ents [vi.MaxDirEntries]vi.DirEntry
	_, r := dirListFn(path, ents[:])
	return r >= 0
}

// foldEq is ASCII case-insensitive equality — NAME matching follows the
// kernel's case-insensitive rule.
func foldEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'a' && ca <= 'z' {
			ca -= 32
		}
		if cb >= 'a' && cb <= 'z' {
			cb -= 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// Base64 (RFC 4648, standard alphabet, strict decode): the
// implementation lives in the library — the wire's stream chunks and
// the audit lane's payload digest encode and decode with it.
func b64Encode(b []byte) string { return agentverbs.B64Encode(b) }

func b64Decode(s string) ([]byte, bool) { return agentverbs.B64Decode(s) }
