// GOTABWM.ELF — M123a (issue #2575): kernel refusals get a face.
//
// There is no kernel→seat error channel (kinds 18-21 carry no errno and
// the card adds none), so "intercepting error markers" means the seat's
// OWN refusal sites — the places its event and tick paths already print
// a serial-only marker (or stay silent) when a vi/vsys call comes back
// negative. Each such site now also posts an actionable notify toast:
// the serial marker stays exactly as it was and the toast rides beside
// it, so a refusal stops being invisible on the scanout.
//
// The sites this file serves:
//
//   - exec arms inside consumeSeatEvent's handlers — the launcher row
//     click / Enter (execSelected), the palette's exec, the reopen and
//     duplicate chords, the session re-exec, and crashRestart —
//     `vi.Exec` answering ENOENT/EACCES/ENOSPC/...
//   - the clipboard lane (hostclip.go): a refused slot-38/39 call —
//     the seat is authorized today, but the refusal branch exists and
//     is what a future posture would hit
//   - the hostint engine's lane refusals (hostint.go): a signed record
//     refused for its lane — `lane=clip reason=consent_denied` IS a
//     clipboard denial the seat can see, and the lane mask revocation
//     a suspend produces IS a "denied" a class-B leg can drive for real
//
// Per-errno action (the toast's click and its center row run the same
// dispatch — a resolved report leaves both surfaces, like M125f's offer):
//
//   - EACCES          -> Open Settings: exec GOSET.ELF (the grants pane
//     is where the consent lives, ADR 0054)
//   - ENOENT/ENOSPC/
//     ETIMEDOUT       -> Retry: re-exec the named binary
//   - anything else   -> Copy Error: write the error text to the kernel
//     clipboard (the seat is clipboard-authorized by construction)
//
// A refusal storm produces ONE toast: the dedupe table keys (name, ctx)
// and absorbs repeats inside refusalDedupeTicks, printing a counted
// dedupe line — the drop-counter idiom, so an absorbed storm is still
// named on serial.
package main

import (
	"strings"

	"virelai/hostint"
	"virelai/vi"
)

const (
	// MarkerRefusal is printed AFTER the toast is queued, naming the
	// refusal's errno (or the lane's reason vocabulary), the site
	// context, and the action the click will run.
	MarkerRefusal = "gotabwm: refusal "
	// MarkerRefusalDedupe is printed when a repeat of an already-toasted
	// refusal is absorbed. `total=` is the CUMULATIVE count since boot —
	// the MarkerNotifyDrop idiom: a per-event 1 is useless, the running
	// figure says how big the storm got.
	MarkerRefusalDedupe = "gotabwm: refusal dedupe "
	// MarkerRefusalAct is printed when an action dispatch runs — Retry's
	// re-exec, the grant pane's open, or the clipboard write — carrying
	// the outcome: the pid/ok or the errno that refused the action too.
	MarkerRefusalAct = "gotabwm: refusal act "
	// MarkerNotice is printed AFTER a seat notice's toast is queued —
	// the action-free half of the refusal lane (M216c): a refused split,
	// a space at the cap, an empty reopen names its reason the same way
	// an errno refusal does, minus the Retry/Grant/Copy arm.
	MarkerNotice = "gotabwm: notice "

	// gosetBin is the settings panel the grant action opens (its grants
	// view is the ADR 0054 consent surface).
	gosetBin = "GOSET.ELF"

	// refusalDedupeTicks is the absorption window, in composite ticks.
	// It matches the toast's own lifetime (NotifyTicks): a repeat inside
	// the window is the SAME report, and a refusal that keeps firing
	// refreshes the window so a storm stays one toast.
	refusalDedupeTicks = NotifyTicks
	// refusalTableMax bounds the dedupe table — the seat's other tables'
	// discipline (bounded, oldest-evicted).
	refusalTableMax = 8
	// refusalTextMax bounds the toast text — the crashSnapToast width.
	refusalTextMax = 24
)

// The notice contexts (M216c) — each IS the toast text, and every one
// fits the refusalTextMax budget (the longest is 22 bytes).
const (
	noticeSplitOneTab = "split: one tab"
	noticeSplitPane   = "split: pane limit"
	noticeSplitDepth  = "split: depth limit"
	noticeSplitSmall  = "split: too small"
	noticeSplitNoTab  = "split: no tab left"
	noticeSpacesLimit = "spaces: limit 4"
	noticeReopenEmpty = "reopen: nothing closed"
)

// refusalSeen is the bounded dedupe table: (errno-name + context) → the
// seat tick that signature last posted at.
var (
	refusalSeen [refusalTableMax]struct {
		key  string
		tick uint64
	}
	refusalSeenN   int
	refusalDeduped int
)

// refusalActionFor is the errno → action map. EACCES is the one refusal
// the user can fix through the grants pane; the transient trio earns a
// Retry; everything else offers Copy Error so the text can leave the VM.
func refusalActionFor(name string) uint8 {
	switch name {
	case "EACCES":
		return notifyActionGrant
	case "ENOENT", "ENOSPC", "ETIMEDOUT":
		return notifyActionRetry
	default:
		return notifyActionCopy
	}
}

// refusalActionName renders the action kind for the refusal marker and
// the center row's action line.
func refusalActionName(action uint8) string {
	switch action {
	case notifyActionRetry:
		return "retry"
	case notifyActionGrant:
		return "grant"
	case notifyActionCopy:
		return "copy"
	case notifyActionCrashSnap:
		return "restore"
	case notifyActionDropOpen:
		return "drop-open"
	case notifyActionUnsavedReopen:
		return "reopen"
	}
	return "none"
}

// refusalText builds the bounded toast line: the errno/reason name FIRST,
// so truncation at the panel width can never eat the name of the refusal.
func refusalText(name, ctx string) string {
	t := name
	if ctx != "" {
		t += " " + ctx
	}
	if len(t) > refusalTextMax {
		t = t[:refusalTextMax]
	}
	return t
}

// refusalSeenHit records key's signature at seatTick and reports whether
// the press is a repeat inside the dedupe window — an absorbed repeat
// still names itself on serial through the counted dedupe line. False
// means the caller pushes its toast (a fresh signature or an expired
// window); the table insert/refresh already ran either way.
func refusalSeenHit(key, ctx string) bool {
	oldest := 0
	for i := 0; i < refusalSeenN; i++ {
		if refusalSeen[i].tick < refusalSeen[oldest].tick {
			oldest = i
		}
		if refusalSeen[i].key == key {
			if seatTick-refusalSeen[i].tick < refusalDedupeTicks {
				refusalSeen[i].tick = seatTick
				refusalDeduped++
				vi.ConsoleLine(MarkerRefusalDedupe + ctx +
					" total=" + vi.Itoa64(int64(refusalDeduped)))
				return true
			}
			refusalSeen[i].tick = seatTick
			return false
		}
	}
	if refusalSeenN < refusalTableMax {
		refusalSeen[refusalSeenN].key = key
		refusalSeen[refusalSeenN].tick = seatTick
		refusalSeenN++
	} else {
		refusalSeen[oldest].key = key
		refusalSeen[oldest].tick = seatTick
	}
	return false
}

// seatNotice is the refusal lane's action-free sibling (M216c): a seat
// refusal that names a LIMIT rather than a kernel errno — a refused
// split, a space at the cap, an empty reopen — still gets a face. The
// toast carries the context verbatim through notifyPush (no action arm:
// there is nothing to retry, grant or copy), the same dedupe table
// absorbs repeats, and the marker prints AFTER the toast is queued.
func seatNotice(ctx string) {
	if ctx == "" || refusalSeenHit("notice|"+ctx, ctx) {
		return
	}
	notifyPush(0, ctx, compositeClock)
	vi.ConsoleLine(MarkerNotice + ctx)
}

// seatRefusalName converts one refusal into an actionable toast (or absorbs
// it into the dedupe window). `name` is the errno name — or a lane reason
// — `ctx` the short site label ("exec MISSING.ELF", "clipboard",
// "lane-clip"), and `payload` the retry action's binary name.
func seatRefusalName(name, ctx, payload string) {
	if name == "" {
		return
	}
	action := refusalActionFor(name)
	if action == notifyActionCopy && payload == "" {
		payload = name + " " + ctx
	}
	if refusalSeenHit(name+"|"+ctx, ctx) {
		return
	}
	text := refusalText(name, ctx)
	notifyPushAction(0, text, compositeClock, action, payload, 0)
	vi.ConsoleLine(MarkerRefusal + name + " " + ctx +
		" act=" + refusalActionName(action))
}

// seatRefusal is seatRefusalName for a raw kernel result (rc < 0).
func seatRefusal(rc int64, ctx, payload string) {
	if rc >= 0 {
		return
	}
	seatRefusalName(vi.ErrnoName(rc), ctx, payload)
}

// seatRefusalErr is seatRefusalName for an errno-typed error (the
// execApp return shape).
func seatRefusalErr(err error, ctx, payload string) {
	// M216m (#3952): the window-ceiling guard raises its own notice.
	if err == nil || err == errWindowLimit {
		return
	}
	seatRefusalName(err.Error(), ctx, payload)
}

// armRefusalLaneWatch wraps hostintLog — the engine's marker sink, which
// the seat already owns — so every `refuse lane=<l> reason=<code>` line
// the engine emits ALSO reaches the toast path. The seat touches neither
// the engine nor the lane files: the marker's own format (pinned by
// hostint's tests) is the observation, parsed, not a new seam. Wired once
// from main after hostintSeatInit.
func armRefusalLaneWatch() {
	inner := hostintLog
	hostintLog = func(s string) {
		inner(s)
		if lane, reason, ok := laneRefusalLine(s); ok {
			seatLaneRefusal(lane, reason)
		}
	}
}

// laneRefusalLine parses the engine's one refusal line —
// `… refuse lane=<l> reason=<code>` — into its lane and reason. False for
// every other line the sink carries.
func laneRefusalLine(s string) (lane, reason string, ok bool) {
	i := strings.Index(s, " refuse lane=")
	if i < 0 {
		return "", "", false
	}
	rest := s[i+len(" refuse lane="):]
	j := strings.Index(rest, " reason=")
	if j < 0 {
		return "", "", false
	}
	return rest[:j], rest[j+len(" reason="):], true
}

// seatLaneRefusal is the lane watch's dispatch, scoped to the CLIPBOARD
// lane: a refused clip record IS the "clipboard denied" the card names.
// The drop lane's consent wall stays silent — an unconsented drop must
// not notify (TestHostdropConsentDeniedWithoutLaneBit pins that
// posture). Denial-class reasons — consent withheld, a revoked mask, a
// missing credential, an auth denial — map to the grant action like an
// EACCES; payload failures map to Copy Error; integrity/replay noise
// (bad_mac, stale_*, bad_record) and rate pressure (backpressured, gone)
// keep their serial-only posture: a forged record storm must not become
// a toast storm.
func seatLaneRefusal(lane, reason string) {
	if lane != "clip" {
		return
	}
	ctx := "lane-" + lane
	switch reason {
	case hostint.RefuseConsent, hostint.RefuseLaneDisabled,
		hostint.RefuseNoCredential, hostint.RefuseAuthDenied:
		seatRefusalName("EACCES", ctx, "")
	case hostint.RefuseIO, hostint.RefuseNotText, hostint.RefuseTooLarge,
		hostint.RefuseNameRejected, hostint.RefuseNoBundle,
		hostint.RefuseUnsupported, hostint.RefuseQuota:
		seatRefusalName(reason, ctx, reason)
	}
}

// runNotifyAction performs an action toast's side effect — the dispatch
// the toast's click and its center row's click SHARE, so an error report
// resolves the same way from either surface. size is the binding's
// attested payload size (#3388 — drop-open only; 0 on every other arm).
func runNotifyAction(action uint8, payload string, size uint64) {
	switch action {
	case notifyActionCrashSnap:
		crashSnapRestore(payload)
	case notifyActionRetry:
		refusalRetry(payload)
	case notifyActionGrant:
		refusalGrant()
	case notifyActionCopy:
		refusalCopy(payload)
	case notifyActionDropOpen:
		dropOpen(payload, size)
	case notifyActionUnsavedReopen:
		// M216e (#3944): the dirty-close toast's click IS the reopen —
		// applyReopen pops the LIFO the close just fed and re-execs the
		// bin with its state blob, printing its own markers.
		_ = applyReopen()
	}
}

// refusalRetry is the Retry arm: re-exec the binary the refusal names.
// The verdict rides the marker — a still-missing binary reports ENOENT
// again (through the normal refusal path on its next raise, not a loop).
func refusalRetry(bin string) {
	if bin == "" {
		vi.ConsoleLine(MarkerRefusalAct + "retry none")
		return
	}
	if _, err := execApp(bin); err != nil {
		vi.ConsoleLine(MarkerRefusalAct + "retry " + bin + " " + err.Error())
		return
	}
	vi.ConsoleLine(MarkerRefusalAct + "retry " + bin + " ok")
}

// refusalGrant is the Open Settings arm: exec the settings panel — its
// grants view is the consent surface an EACCES asks for.
func refusalGrant() {
	if _, err := execApp(gosetBin); err != nil {
		vi.ConsoleLine(MarkerRefusalAct + "grant " + gosetBin + " " + err.Error())
		return
	}
	vi.ConsoleLine(MarkerRefusalAct + "grant " + gosetBin + " ok")
}

// refusalCopy is the Copy Error arm: the kernel clipboard takes the
// refusal's text. The seat's own write CAN be refused in principle —
// the marker keeps the raw verdict instead of claiming a copy that
// never landed.
func refusalCopy(text string) {
	if text == "" {
		vi.ConsoleLine(MarkerRefusalAct + "copy none")
		return
	}
	n, rc := hostClipSet([]byte(text))
	if rc < 0 {
		vi.ConsoleLine(MarkerRefusalAct + "copy rc=" + vi.Itoa64(rc))
		return
	}
	vi.ConsoleLine(MarkerRefusalAct + "copy n=" + vi.Itoa64(int64(n)))
}
