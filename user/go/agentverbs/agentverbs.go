// Package agentverbs is the agent verb library lifted out of AGENTD
// (M205a, card #3808): the fixed verb table, the capability check and
// the per-verb handlers, callable in-process by any daemon that serves
// the agent verb set. AGENTD is the first caller — the JSON-Lines wire,
// the Ed25519 auth, the keyring and the audit lane stay with the
// daemon; what moved is everything between "a validated request
// arrived" and "the result or the refusal code is in hand".
//
// The split is by lifetime. The Dispatcher holds the caller's identity
// — its fs.* sandbox root, its audit-lane capture path, its image name
// for the WM_RPC peer scan, its serial marker prefix — plus every
// syscall seam, so host tests inject fakes exactly the way the daemon's
// own suite does. A Session is what one authenticated caller resolved:
// the capability bitmask, the exec kill switch, the stream-chunk sink
// and the screenshot rate bound. Dispatch runs one request against the
// pair and returns (resultJSON, "") on success or ("", code) on
// refusal — the caller owns the response envelope.
package agentverbs

import "virelai/vi"

// Caps is a principal's capability bitmask — the resolved form of the
// grant words a keyring row declares. The dispatcher tests bits, never
// grant words; the word set the kernel exposes through vi.Principal is
// a different mask entirely (ADR 0024).
type Caps uint32

const (
	CapProcess Caps = 1 << iota
	CapFile
	CapWindow
	CapInput
	CapTerm
)

// CapWord resolves a grant word to its bit. An unknown word resolves
// to 0 — it simply never matches, the same answer a missing map key
// gave the daemon's table.
func CapWord(word string) Caps {
	switch word {
	case "process":
		return CapProcess
	case "file":
		return CapFile
	case "window":
		return CapWindow
	case "input":
		return CapInput
	case "term":
		return CapTerm
	}
	return 0
}

// capName renders one bit's grant word for the cap_required:<cap>
// refusal — the named refusal is the word, not the bit.
func capName(c Caps) string {
	switch c {
	case CapProcess:
		return "process"
	case CapFile:
		return "file"
	case CapWindow:
		return "window"
	case CapInput:
		return "input"
	case CapTerm:
		return "term"
	}
	return ""
}

// verbEnt is one table row: the verb name and the cap it requires.
type verbEnt struct {
	name string
	cap  Caps
}

// table is the fixed verb->cap set (ADR 0052 D3), in the order the
// ready line advertises: proc verbs take `process`, fs takes `file`,
// ui.windows/ui.screenshot take `window`, ui.input takes `input` — a
// DISTINCT grant, because an observer that may only read the desktop
// must not get to drive it (M139b) — and gosh.run takes `term` (AND-ed
// with the exec kill switch inside the verb — the daemon flag is the
// operator's switch, the cap is the per-agent grant). ui.geometry
// (card #2985) splits the same rule BY OP: the verb gate is `window`
// so a read-only observer can query, and the verb itself refuses
// move/sash cap_required:input before touching the seat — they drive
// the desktop, so they take the drive grant.
var table = []verbEnt{
	{"proc.list", CapProcess},
	{"proc.get", CapProcess},
	{"fs.read", CapFile},
	{"fs.write", CapFile},
	{"ui.windows", CapWindow},
	{"ui.screenshot", CapWindow},
	{"ui.input", CapInput},
	{"ui.geometry", CapWindow},
	{"gosh.run", CapTerm},
}

var verbCap = func() map[string]Caps {
	m := make(map[string]Caps, len(table))
	for _, e := range table {
		m[e.name] = e.cap
	}
	return m
}()

// Advertised is a session's resolved grant set: a verb appears iff its
// cap is on the bitmask (D2), and gosh.run also only when the caller's
// exec flag is on.
func Advertised(caps Caps, allowExec bool) []string {
	var out []string
	for _, e := range table {
		if caps&e.cap == 0 {
			continue
		}
		if e.name == "gosh.run" && !allowExec {
			continue
		}
		out = append(out, e.name)
	}
	return out
}

// Seams is the syscall surface the verbs reach through. Production
// wires the real vi/shlib calls; host tests substitute fakes — the
// same seam discipline the daemon's own suite uses.
type Seams struct {
	Procs         func(dst []vi.ProcRow) (int, int64)
	ReadFile      func(path string, max int) ([]byte, int64)
	WriteFile     func(path string, b []byte) int64
	DeleteFile    func(path string) int64
	DirList       func(path string, buf []vi.DirEntry) (int, int64)
	Exec          func(name string, args ...string) (int64, error)
	Wait          func(pid int64, budgetNs int64) (int64, error)
	Kill          func(pid uint64) int64
	Probe         func(pid int64) (int64, int)
	SeatPid       func() int64
	WinOpen       func(x, y, w, h uint32) (int, int64)
	WinHide       func(id int, visible bool) int64
	WmWindows     func(winID uint32, self string) ([]vi.WmWindowRow, error)
	Scanout       func(x, y, w, h uint32, buf []byte) int64
	AgentInputKey func(winID uint32, mods, usage uint8, self string) (vi.WmRpc, bool)
	AgentInputPtr func(winID uint32, buttons uint8, x, y uint16, self string) (vi.WmRpc, bool)
	AgentGeometry func(winID uint32, op uint8, target uint32, arg uint8, self string) (vi.WmRpc, bool)
	Nanos         func() int64
	Yield         func()
	Sleep         func(ticks uint64)
	Log           func(s string)
}

// Config is one daemon's dispatch identity: the fs.* sandbox root (the
// verbs only ever reach beneath it), the gosh.run capture path inside
// the audit lane, the first path segment the sandbox grammar reserves,
// the shell image gosh.run spawns, the image name the WM_RPC peer scan
// resolves, and the serial marker prefix the spawn/reap lines print.
// AGENTD configures "/host/AGENT", "/host/AGENT/AUDIT/AGENTD.OUT",
// "AUDIT", "GOSH.ELF", "AGENTD.ELF" and "agentd".
type Config struct {
	Seams    Seams
	Sandbox  string // the fs.* root, e.g. "/host/AGENT"
	OutPath  string // the gosh.run capture file inside the audit lane
	Reserved string // the fs.* reserved first segment, "" reserves none
	GoshBin  string // the shell image gosh.run spawns
	Self     string // this image's proc-table name (WM_RPC peer scan)
	Mark     string // the serial marker prefix for spawn/reap lines
}

// Dispatcher is one configured verb endpoint: the config plus the
// lazily bound kernel window the seat-facing verbs share (M97g-F2).
// One per daemon; dispatch serially — the verbs hold no other state.
type Dispatcher struct {
	cfg         Config
	prefix      string // Sandbox with its trailing separator
	agentWindow int
}

// New builds a Dispatcher; the bind window starts unopened.
func New(cfg Config) *Dispatcher {
	return &Dispatcher{cfg: cfg, prefix: cfg.Sandbox + "/", agentWindow: -1}
}

// ResetWindow drops the lazily bound kernel window so the next
// seat-facing verb re-binds — for tests that swap the window seams.
func (d *Dispatcher) ResetWindow() {
	d.agentWindow = -1
}

func (d *Dispatcher) log(s string) {
	if d.cfg.Seams.Log != nil {
		d.cfg.Seams.Log(s)
	}
}

// Session is what one authenticated caller resolved for the dispatch
// loop: the cap bitmask, the exec kill switch, the stream-chunk sink
// the bounded-output verbs emit ahead of their result, the
// ui.screenshot scale=1 rate bound (the caller persists it across the
// session's requests), and the pre-spawn receipt hook gosh.run fires —
// AGENTD wires that to its audit lane; a caller without a lane leaves
// it nil.
type Session struct {
	Caps           Caps
	AllowExec      bool
	Emit           func(chunk []byte)
	NextFullShotNs int64
	AuditIntent    func()
}

// Dispatch validates a request's argument set against its verb and
// runs it. The cap gate runs before any argument check: a missing cap
// is the named refusal cap_required:<cap> — refuse before touching
// anything, and nothing under the cap runs for a principal that was
// never granted it (D3). It returns (resultJSON, "") on success or
// ("", code) on refusal; the caller owns the response envelope and the
// req/refuse markers.
func (d *Dispatcher) Dispatch(s *Session, req *Request) (string, string) {
	cap, known := verbCap[req.Verb]
	if !known {
		return "", "unknown_verb"
	}
	if s.Caps&cap == 0 {
		return "", "cap_required:" + capName(cap)
	}
	switch req.Verb {
	case "proc.list":
		if !req.hasArgs() {
			return "", "bad_request"
		}
		return d.ProcList()
	case "proc.get":
		if !req.hasArgs("pid") {
			return "", "bad_request"
		}
		return d.ProcGet(req)
	case "fs.read":
		if !req.hasArgs("path") {
			return "", "bad_request"
		}
		return d.FSRead(req)
	case "fs.write":
		if !req.hasArgs("path", "data_b64") {
			return "", "bad_request"
		}
		return d.FSWrite(req)
	case "ui.windows":
		if !req.hasArgs() {
			return "", "bad_request"
		}
		return d.UIWindows()
	case "ui.screenshot":
		if !req.hasArgs("scale") {
			return "", "bad_request"
		}
		return d.UIScreenshot(req, s)
	case "ui.input":
		if !req.hasArgs("win", "events") {
			return "", "bad_request"
		}
		return d.UIInput(req)
	case "ui.geometry":
		if !req.hasArgs("win", "op", "space", "percent") {
			return "", "bad_request"
		}
		return d.UIGeometry(req, s)
	case "gosh.run":
		if !req.hasArgs("cmd", "timeout_ms") {
			return "", "bad_request"
		}
		return d.GoshRun(req, s)
	}
	return "", "unknown_verb"
}

// --- shared helpers -----------------------------------------------------------

// ioErr is D4's catch-all: any other negative kernel result is io_error
// and the message carries vi.ErrnoName so the client sees which errno
// it was.
func ioErr(r int64) (string, string) {
	name := vi.ErrnoName(r)
	if name == "" {
		name = "EIO"
	}
	return "", "io_error:" + name
}

// fileErr maps a file-ABI result onto the D4 code table. EINVAL on a
// read-only open is how a directory answers, so it is disambiguated
// with a directory scan before it degrades to io_error.
func (d *Dispatcher) fileErr(r int64, name string) (string, string) {
	switch vi.ErrnoName(r) {
	case "ENOENT":
		return "", "not_found"
	case "EACCES":
		return "", "access_denied"
	case "EINVAL":
		if d.isDir(name) {
			return "", "not_a_file"
		}
	}
	return ioErr(r)
}

// isDir reports whether a sandbox path resolves to a directory: the
// parent is listed and the last segment matched. The kernel matches
// names case-insensitively (ADR 0024 D4); so does this.
func (d *Dispatcher) isDir(path string) bool {
	parent, last := parentOf(path)
	dir := d.cfg.Sandbox
	if parent != "" {
		dir = d.prefix + parent
	}
	var ents [vi.MaxDirEntries]vi.DirEntry
	n, r := d.cfg.Seams.DirList(dir, ents[:])
	if r < 0 {
		return false
	}
	for i := 0; i < n; i++ {
		if ents[i].Dir() && foldEq(ents[i].NameString(), last) {
			return true
		}
	}
	return false
}

func jbool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// emitChunks streams out through s.Emit in StreamChunk pieces — the
// shared tail of every bounded-output verb.
func emitChunks(s *Session, out []byte) {
	for len(out) > 0 {
		n := StreamChunk
		if len(out) < n {
			n = len(out)
		}
		if s.Emit != nil {
			s.Emit(out[:n])
		}
		out = out[n:]
	}
}

// Bound constants from D3/D4. The values the channel code and the
// host suites pin are exported; the rest stay internal.
const (
	ReadMax = 4096  // fs.read / fs.write body ceiling
	OutMax  = 16384 // gosh.run captured-stdout ceiling (streamed)
	// StreamChunk bounds one streamed response line's payload: b64 is
	// 4/3 plus the envelope, so 4608 raw bytes stay well inside the
	// 8192-byte session line bound.
	StreamChunk  = 4608
	CmdMax       = 192 // gosh.run cmd byte ceiling
	TimeoutMaxMs = 30000
	timeoutDefMs = 10000
	maxProcRows  = 64 // D3: at most 64 rows, kernel order
	// reapProbeTries bounds the probe polls after Kill on a timed-out
	// child.
	reapProbeTries = 8
	// spawnRetryNs bounds the ENOSPC yield loop in gosh.run — comfortably
	// past an idle-reaper pass over one child's task slots.
	spawnRetryNs = int64(2) * 1e9
	// InputEventsMax bounds one ui.input events[] batch (D3).
	InputEventsMax = 64
)
