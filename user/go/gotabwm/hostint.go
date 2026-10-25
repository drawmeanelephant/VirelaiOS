// GOTABWM.ELF — M108b (#2325, ADR 0051): the seat-side end of the host
// integration lanes. The wire is signed VHI1 records under the share's
// /host/HOSTINT/ directory (the existing --cvc-file channel — no kernel
// surface moves). The seat is the ONLY lawful guest endpoint: slots 38/39
// already admit the registered seat (M97g's clipboard_authorized), and
// poll-on-tick is the notification channel — no new event kind exists or
// is needed.
//
// This file is the engine wiring: credential probe, file-op seams, the
// per-tick drive. The lane policy itself lives in hostclip.go (M108b); the
// drop and notify-out lanes (M108c/d) attach to the same engine.
package main

import (
	"virelai/hostint"
	"virelai/shlib"
	"virelai/vi"
)

// hostintMarkerPrefix is the seat's lane log prefix. The engine's own
// markers (armed, refuse) are built from it; lane markers hang off it too.
const hostintMarkerPrefix = "gotabwm: hostint"

// hostintEngine is the seat's live lane engine, or nil when the host-int
// credential is absent: ADR 0051 D7's byte-silence rule is "no row → the
// seat never even polls the lane directory".
var hostintEngine *hostint.Engine

// The IO seams, as vars so host tests drive the whole lane in memory. The
// production values wire the engine to the real file channel and console.
var (
	hostintSecretGet = shlib.StoreGet
	// The lane dir rides the --cvc-file channel (queue 5): vi.ListDirAt
	// pages the v2 open/page/close protocol the kernel serves there (the
	// dirV2List fallback — the same shape OUTBOX/SAY/PRINT already use).
	// The 16-row sys_dir_list window was a pinning surface (≥16 foreign
	// names starved every record) AND the 31-byte row's st_path_limit
	// whole-listing kill — the snapshot serves 255-byte names up to the
	// 256-entry bound, so a hostile name degrades to a foreign row the
	// engine evicts, never a dead listing (#3387). ENOENT — a missing
	// dir or a channel that is not there — is the flags-off steady
	// state: silent. Any other failure reports absent=false and Poll
	// names io_error once.
	hostintListDir = func() ([]vi.DirItem, bool) {
		items, err := vi.ListDirAt("/HOSTINT")
		if err != nil {
			return nil, err.Error() == "ENOENT"
		}
		// A successful-but-empty snapshot comes back nil — the (nil,
		// false) pair is the channel-failure form, so an empty dir must
		// be normalized or it would read as io_error.
		if items == nil {
			items = []vi.DirItem{}
		}
		return items, false
	}
	hostintReadFile = func(name string) ([]byte, int64) {
		// RecordMax+1 so an oversize lane file is visibly oversize to
		// Decode (a clean bad_record) rather than indistinguishably
		// truncated at the cap.
		return vi.ReadFileAll(hostint.HostDir+"/"+name, hostint.RecordMax+1)
	}
	hostintWriteFile = func(name string, b []byte) int64 {
		return vi.WriteFileSafe(hostint.HostDir+"/"+name, b)
	}
	hostintDeleteFile = func(name string) int64 {
		return vi.FileDelete(hostint.HostDir + "/" + name)
	}
	hostintLog = vi.ConsoleLine
)

// hostintSeatInit probes the host-int SECRETS.TXT row once, at seat
// startup. Absent or unreadable is silent — the engine simply never
// exists, which is the whole flags-off posture. The row is served only to
// this image (the ADR 0024 D8 app binding: uid_user + GOTABWM.ELF named
// against a uid_system-owned OWNERS.TXT row), so holding it is already the
// operator's statement that THIS seat may run lanes.
func hostintSeatInit() {
	var key [vi.SecretValMax]byte
	n := hostintSecretGet(hostint.SecretName, key[:])
	if n <= 0 {
		return
	}
	hostintEngine = hostint.NewEngine(key[:n], hostint.EngineIO{
		List:   hostintListDir,
		Read:   hostintReadFile,
		Write:  hostintWriteFile,
		Delete: hostintDeleteFile,
		// Indirection, not the value: armRefusalLaneWatch re-binds
		// hostintLog AFTER this init, and a captured function value would
		// bypass the watch — the engine's own `refuse lane=` lines would
		// print but never dispatch the seat's refusal toast (observed
		// go-wm-seat run 18: refuse logged, EACCES never raised).
		Log: func(s string) { hostintLog(s) },
	}, hostintMarkerPrefix)
	hostintEngine.Consume = hostintConsume
	hostClipBuf = make([]byte, clipKernelMax)
}

// The v2 hook vars (ADR 0051 V2-D7 — the shared seam): the dependent
// cards bind them from their own files, and the nil default is exactly
// the v1 behavior — an unbound NOTIFY_FOCUS consume refuses lane_disabled
// and the outbox drive is a no-op.
var (
	// hostfocusConsume is M127c's NOTIFY_FOCUS route (hostfocus.go binds
	// it): a verified click-through record asking the seat to focus a tab.
	hostfocusConsume func(rec hostint.Record) string
	// outboxTickFn is M127d's per-tick OUTBOX drive (hostoutbox.go binds
	// it): the REQ-marker scan that turns app intents into signed emits.
	outboxTickFn func()
	// speechTickFn is M186d's per-tick SAY drive (hostspeech.go binds
	// it): the same intent-marker scan for the speech lane.
	speechTickFn func()
	// printTickFn is M202c's per-tick PRINT drive (hostprint.go binds
	// it): the same intent-marker scan for the print lane.
	printTickFn func()
)

// hostintConsume routes seat-inbound records to their lane — M108b owns
// CLIP_IN (hostclip.go), M108c owns DROP (hostdrop.go), M127c owns
// NOTIFY_FOCUS (hostfocus.go, bound through hostfocusConsume). A tag with
// no tenant is refused lane_disabled.
func hostintConsume(tag uint8, rec hostint.Record) string {
	switch tag {
	case hostint.TagClipIn:
		return hostclipApply(rec)
	case hostint.TagClipInT:
		return hostclipApplyT(rec)
	case hostint.TagDrop:
		return hostdropApply(rec)
	case hostint.TagNotifyFocus:
		if hostfocusConsume != nil {
			return hostfocusConsume(rec)
		}
	}
	return hostint.RefuseLaneDisabled
}

// hostintSeatTick is the per-composite-tick drive: consume what the lane
// directory holds (adoption first), then produce what changed (the
// clipboard lane's CLIP_OUT, M108b), what the strip staged (the notify-out
// lane's NOTIFY_OUT, M108d), what the OUTBOX markers request (M127d),
// what the SAY markers request (M186d) and what the PREQ markers request
// (M202c).
// ~1 Hz, bounded file ops.
func hostintSeatTick() {
	if hostintEngine == nil {
		return
	}
	hostintEngine.Poll()
	hostclipPoll()
	hostnotifyPoll()
	if outboxTickFn != nil {
		outboxTickFn()
	}
	if speechTickFn != nil {
		speechTickFn()
	}
	if printTickFn != nil {
		printTickFn()
	}
}

// laneEvictBound mirrors the engine's own evictPerTick — the per-tick
// bound on the foreign-name sweep one lane scan performs.
const laneEvictBound = 8

// laneSweep deletes up to laneEvictBound entries keep() refuses — the
// foreign names a lane dir can never carry lawfully (#3387, audit R5
// F1). Directories and '~'-suffixed names are always kept: a dir
// outlives file delete, and a temp is a live publish whose deletion
// would tear the rename. Returns the count so the caller names one
// refusal per sweep — the names themselves are attacker-controlled text
// and never reach the log.
func laneSweep(items []vi.DirItem, keep func(it vi.DirItem) bool, del func(string) int64) int {
	n := 0
	for _, it := range items {
		if n >= laneEvictBound {
			break
		}
		if it.IsDir || keep(it) || (len(it.Name) > 0 && it.Name[len(it.Name)-1] == '~') {
			continue
		}
		_ = del(it.Name)
		n++
	}
	return n
}
