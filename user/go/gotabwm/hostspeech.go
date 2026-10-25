// GOTABWM.ELF — M186d (#3181, ADR 0051 V2-D8): the speech lane, seat
// half.
//
// A guest app cannot sign (the host-int row is bound to this image), so
// intent and provenance split — the OUTBOX REQ pattern: the app (GOSH
// `say`, GOA11Y -say) writes /host/SAY/SAY-<8hex> carrying the utterance
// verbatim, and this file is the signer. Once per lane tick, while the
// adopted mask carries lane_speech, the scan takes ONE well-formed SAY
// marker, emits the signed SPEAK record (the marker's content IS the
// payload — a bounded text needs no second file) and deletes the marker.
// Refusals are named and the marker is still deleted: payload_not_text
// (empty or invalid UTF-8), payload_too_large (> 512). A backpressured
// emit keeps the marker for the next tick — the intent waits rather
// than dropping.
//
// The SAY marker is unsigned by design — it is intent, not provenance.
// What the host trusts is the signed record; what the seat signs is
// only a SAY-marked utterance.
package main

import (
	"unicode/utf8"

	"virelai/hostint"
	"virelai/shlib"
	"virelai/speech"
	"virelai/vi"
)

// MarkerHostintSpeak is the seat's speak emit receipt — seq, len and
// sha256[:8], the same shape the other emit lanes' markers carry. The
// utterance itself never rides the marker (never-logged contract).
const MarkerHostintSpeak = "gotabwm: hostint speak "

// The IO seams, as vars so host tests drive the whole lane in memory —
// the hostint fixture discipline. SAY/ rides the same --cvc-file channel
// as the lane dir; the 16-row sys_dir_list window would hide every
// marker behind 16 utterances — vi.ListDirAt pages the v2 open/page/
// close protocol up to the snapshot bound. ENOENT — a missing dir — is
// the flags-off steady state: silent. Any other failure reports
// absent=false and the tick names io_error once.
var (
	hostspeechListDir = func() ([]vi.DirItem, bool) {
		items, err := vi.ListDirAt("/SAY")
		if err != nil {
			return nil, err.Error() == "ENOENT"
		}
		if items == nil {
			items = []vi.DirItem{} // an empty snapshot is a listing, not a failure
		}
		return items, false
	}
	// hostspeechListFailed latches the listing-failure refusal so a
	// persistently-broken share dir names io_error once, not once a tick.
	hostspeechListFailed = false
	// Marker bodies are the utterance verbatim — a marker over the text
	// bound reads as oversize so the bound check can still name the
	// refusal.
	hostspeechReadFile = func(name string) ([]byte, int64) {
		return vi.ReadFileAll(speech.Dir+"/"+name, speech.TextMax+1)
	}
	hostspeechDeleteFile = func(name string) int64 {
		return vi.FileDelete(speech.Dir + "/" + name)
	}
)

func init() { speechTickFn = hostspeechTick }

// hostspeechTick is the speech lane's per-tick drive, bound through the
// seam's speechTickFn hook. It polls only while the adopted mask
// carries lane_speech — the D7 posture: flags-off (or revoked) is
// byte-silent. At most one SAY outcome per tick — the emit-side bound
// every g→h lane already holds.
func hostspeechTick() {
	eng := hostintEngine
	if eng == nil || !eng.Armed() || eng.LaneMask()&hostint.LaneSpeech == 0 {
		return
	}
	items, absent := hostspeechListDir()
	if items == nil {
		if absent {
			return // SAY/ never existed — silent, like the lane dir
		}
		if !hostspeechListFailed {
			hostspeechListFailed = true
			eng.RefuseLane(hostint.TagSpeak, hostint.RefuseIO)
		}
		return
	}
	hostspeechListFailed = false
	// Foreign-name eviction (#3387): SAY/ holds markers only — there is
	// no payload file shape — so anything that is not a SAY-<8hex> marker
	// or a publish temp deletes on sight, one bounded sweep per tick.
	if laneSweep(items, func(it vi.DirItem) bool {
		return speech.IsSayName(it.Name)
	}, hostspeechDeleteFile) > 0 {
		eng.RefuseLane(hostint.TagSpeak, hostint.RefuseNameRejected)
	}
	// Name-ordered scan, mirroring the engine's own ordering.
	var names []string
	for _, it := range items {
		if !it.IsDir && speech.IsSayName(it.Name) {
			names = append(names, it.Name)
		}
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	for _, marker := range names {
		hostspeechOne(eng, marker)
		return // ≤ 1 SAY marker consumed per tick
	}
}

// hostspeechOne resolves one SAY marker: read the utterance it carries,
// check it against the record's own grammar (nonempty valid UTF-8, ≤
// TextMax), and emit the signed SPEAK record — or name the refusal and
// delete the marker.
func hostspeechOne(eng *hostint.Engine, marker string) {
	refuse := func(reason string) {
		eng.RefuseLane(hostint.TagSpeak, reason)
		_ = hostspeechDeleteFile(marker)
	}
	raw, rc := hostspeechReadFile(marker)
	if rc < 0 {
		// A transient read error leaves the marker for the next tick —
		// the engine's unreadable-file posture.
		eng.RefuseLane(hostint.TagSpeak, hostint.RefuseIO)
		return
	}
	if len(raw) > speech.TextMax {
		refuse(hostint.RefuseTooLarge)
		return
	}
	if len(raw) == 0 || !utf8.Valid(raw) {
		refuse(hostint.RefuseNotText)
		return
	}
	seq, refusal := eng.Emit(hostint.TagSpeak, 0, raw)
	switch refusal {
	case "":
		_ = hostspeechDeleteFile(marker)
		sum := shlib.SHA256Sum(raw)
		hostintLog(MarkerHostintSpeak + "seq=" + vi.Itoa64(int64(seq)) +
			" len=" + vi.Itoa64(int64(len(raw))) + " sha=" + hostint.HexDigest8(sum[:]))
	case hostint.RefuseBackpressure:
		// The runner holds MaxOutstanding of ours — the marker waits, a
		// retry next tick rather than a dropped utterance.
	default:
		refuse(refusal)
	}
}
