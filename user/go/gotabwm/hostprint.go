// GOTABWM.ELF — M202c (#3386): the PRINT lane, seat half.
//
// The outbox shape pointed at paper. A guest app cannot sign (the
// host-int row is bound to this image), so intent and provenance split:
// the app (GOEDIT/WEB/GOREADER's print verbs, through virelai/printq)
// writes /host/PRINT/<name>.pdf and a PREQ-<8hex> marker carrying the
// basename verbatim, and this file is the signer. Once per lane tick,
// while the adopted mask carries lane_print, the scan takes ONE
// well-formed PREQ whose payload exists, emits the signed PRINT record
// (the DROP payload shape: [name_len u8][name][size u64le]) and deletes
// the marker. Refusals are named and the marker is still deleted — the
// payload is left in place: gone (payload absent), name_rejected (PREQ
// content breaks the D4+.pdf name rule), unsupported_kind (a directory),
// payload_too_large (> printq.JobMax).
//
// The PREQ is unsigned by design — it is intent, not provenance. What
// the host trusts is the signed record; what the seat signs is only a
// PREQ-marked payload, so an unmarked PDF sitting in PRINT/ is inert,
// never attributed, never printed.
package main

import (
	"virelai/hostint"
	"virelai/printq"
	"virelai/vi"
)

// MarkerHostintPrint is the seat's print emit receipt — the shape the
// runner's `host-print: saved`/`host-print: printed` markers answer on
// the far side.
const MarkerHostintPrint = "gotabwm: hostint print "

// The IO seams, as vars so host tests drive the whole lane in memory —
// the hostint fixture discipline. PRINT/ rides the same --cvc-file
// channel as the lane dir; the 16-row sys_dir_list window would hide
// every PREQ behind 16 payloads — vi.ListDirAt pages the v2 open/page/
// close protocol up to the snapshot bound. ENOENT — a missing dir — is
// the flags-off steady state: silent. Any other failure reports
// absent=false and the tick names io_error once.
var (
	hostprintListDir = func() ([]vi.DirItem, bool) {
		items, err := vi.ListDirAt("/PRINT")
		if err != nil {
			return nil, err.Error() == "ENOENT"
		}
		if items == nil {
			items = []vi.DirItem{} // an empty snapshot is a listing, not a failure
		}
		return items, false
	}
	// hostprintListFailed latches the listing-failure refusal so a
	// persistently-broken share dir names io_error once, not once a tick.
	hostprintListFailed = false
	// PREQ bodies are the basename verbatim — a file over the name bound
	// reads as oversize so the name check can still name the refusal.
	hostprintReadFile = func(name string) ([]byte, int64) {
		return vi.ReadFileAll(printq.Dir+"/"+name, printq.NameMax+1)
	}
	hostprintDeleteFile = func(name string) int64 {
		return vi.FileDelete(printq.Dir + "/" + name)
	}
)

func init() { printTickFn = hostprintTick }

// hostprintTick is the print lane's per-tick drive, bound through the
// seam's printTickFn hook. It polls only while the adopted mask carries
// lane_print — the D7 posture: flags-off (or revoked) is byte-silent.
// At most one PREQ outcome per tick — the emit-side bound every g→h
// lane already holds.
func hostprintTick() {
	eng := hostintEngine
	if eng == nil || !eng.Armed() || eng.LaneMask()&hostint.LanePrint == 0 {
		return
	}
	items, absent := hostprintListDir()
	if items == nil {
		if absent {
			return // PRINT/ never existed — silent, like the lane dir
		}
		if !hostprintListFailed {
			hostprintListFailed = true
			eng.RefuseLane(hostint.TagPrint, hostint.RefuseIO)
		}
		return
	}
	hostprintListFailed = false
	// Foreign-name eviction (#3387): an entry that is neither a PREQ
	// marker nor a lawful payload name can never be signed, so it
	// deletes on sight — one bounded sweep per tick, one refusal per
	// sweep. A name-rule-valid unmarked file stays (it may be a payload
	// whose PREQ is still in flight; inert either way).
	if laneSweep(items, func(it vi.DirItem) bool {
		return printq.IsReqName(it.Name) || printq.NameValid(it.Name)
	}, hostprintDeleteFile) > 0 {
		eng.RefuseLane(hostint.TagPrint, hostint.RefuseNameRejected)
	}
	// Name-ordered scan, mirroring the engine's own ordering. Payloads
	// are looked up in the same listing the PREQs came from; directories
	// join the map too so a dir payload is unsupported_kind, not gone.
	names := make([]string, 0, len(items))
	byName := make(map[string]vi.DirItem, len(items))
	for _, it := range items {
		byName[it.Name] = it
		if !it.IsDir && printq.IsReqName(it.Name) {
			names = append(names, it.Name)
		}
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	for _, req := range names {
		hostprintOne(eng, req, byName)
		return // ≤ 1 PREQ consumed per tick
	}
}

// hostprintOne resolves one PREQ marker: read the payload name it
// carries, check the payload's presence/kind/size against the same
// listing, and emit the signed record — or name the refusal and delete
// the marker.
func hostprintOne(eng *hostint.Engine, req string, byName map[string]vi.DirItem) {
	refuse := func(reason string) {
		eng.RefuseLane(hostint.TagPrint, reason)
		_ = hostprintDeleteFile(req)
	}
	raw, rc := hostprintReadFile(req)
	if rc < 0 {
		// A transient read error leaves the marker for the next tick —
		// the engine's unreadable-file posture, named once.
		eng.RefuseLane(hostint.TagPrint, hostint.RefuseIO)
		return
	}
	if !printq.NameValid(string(raw)) {
		refuse(hostint.RefuseNameRejected)
		return
	}
	name := string(raw)
	it, ok := byName[name]
	if !ok {
		refuse(hostint.RefuseGone)
		return
	}
	// The listing's kind view: a directory payload is unsupported_kind.
	// The size view is the seat's attest-time view — the host's live
	// fstat rules the job at consume.
	if it.IsDir {
		refuse(hostint.RefuseUnsupported)
		return
	}
	if it.Size > printq.JobMax {
		refuse(hostint.RefuseTooLarge)
		return
	}
	// The signed notice: the DROP shape, reversed — [name_len u8][name]
	// [size u64le].
	p := make([]byte, 0, 1+len(name)+8)
	p = append(p, byte(len(name)))
	p = append(p, name...)
	for i := 0; i < 8; i++ {
		p = append(p, byte(it.Size>>(8*i)))
	}
	seq, refusal := eng.Emit(hostint.TagPrint, 0, p)
	switch refusal {
	case "":
		_ = hostprintDeleteFile(req)
		hostintLog(MarkerHostintPrint + "seq=" + vi.Itoa64(int64(seq)) +
			" name=" + name + " size=" + vi.Itoa64(int64(it.Size)))
	case hostint.RefuseBackpressure:
		// The runner holds MaxOutstanding of ours — the PREQ waits, a
		// retry next tick rather than a dropped intent.
	default:
		refuse(refusal)
	}
}
