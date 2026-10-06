// GOTABWM.ELF — M62e (issue #1403): persist the in-process strip as `.tabs`
// v2 on /host/SESSION.TABS. Encode/Decode is a guest-safe copy of the
// host tabcodec layout (tabsv2.go); do not exec the host tool in-guest.
// Corrupt bytes fail closed: empty strip, no trust.
//
// M79g (#1718): the file now records what the USER did, not what the demo
// choreography staged. Every mutation that changes the strip writes it
// through (noteSessionMutation at each call site) instead of waiting for the
// one pin-stay snapshot — reorder a tab, pin it, freeze it, open or close
// one, and the next boot restores that. The choreography's own write keeps
// its marker.
package main

import "virelai/vi"

const (
	sessionPath   = "/host/SESSION.TABS"
	sessionIDBase = uint32(0x100) // placeholder ids; records have no window id
)

type sessionLoadState uint8

const (
	sessionMissing sessionLoadState = iota
	sessionRestored
	sessionCorrupt
)

var (
	sessionSeq uint16 = 1
	// sessionDirty is set by noteSessionMutation and cleared by a
	// successful writeSession. It is retry/observability state, not a
	// write gate: the no-op guard lives at the call sites (a same-cell
	// reorder, a pin of an already-pinned tab returns before the hook,
	// so a burst of no-ops costs no file write). A failed publish or an
	// empty-strip refusal leaves it set — there IS an unpersisted change;
	// publishing it would be a lie, dropping it would be amnesia.
	// The guard is on CHANGE, not on time — the seat has no timer to
	// debounce with, and at a human interaction rate a handful of 2 KB
	// crash-safe writes per boot is not a throughput problem.
	sessionDirty bool
)

func sessionTitlesLine(s *TabStrip) string {
	titles := ""
	pins := "pin="
	for i := 0; i < s.count; i++ {
		if i > 0 {
			titles += ","
			pins += ","
		}
		titles += s.tabs[i].Title
		if s.tabs[i].Pinned {
			pins += "1"
		} else {
			pins += "0"
		}
	}
	a := 0
	if s.count > 0 {
		a = s.focus
	}
	return titles + " " + pins + " active=" + dec(uint32(a))
}

// writeHostFile replaces path with data through the M66b crash-safe
// write: temp + fsync + delete/rename publish (vi.WriteFileSafe). It is a
// var so a host test can capture the bytes: vi.WriteFileSafe degrades to
// -ENOSYS off the guest, which would otherwise make the persist path
// unobservable (the execApp / closeWin pattern in hid.go and interop.go).
// M62e
// wrote in place — the write-open truncated the live file to zero before
// the first chunk landed — so a crash mid-write left a partial
// SESSION.TABS or LAYOUT.txt behind for the next boot to trust or trip
// over; now the live path only ever appears atomically, and the crash
// window leaves the previous bytes or none, which fail-closed readers
// treat as defaults.
var writeHostFile = func(path string, data []byte) bool {
	return vi.WriteFileSafe(path, data) == 0
}

// sessionRows is the strip SESSION.TABS may honestly record: the tabs THIS
// boot hosts. A row restored from a previous boot (a placeholder id, >=
// sessionIDBase) was never re-exec'd — the card keeps re-exec out of scope —
// so writing it back would grow the file with a row no app ever ran, and
// across boots (restore, open one, reboot) that grows without bound. Focus
// follows by id; a focus that sat on a dropped row records no active tab.
func (s *TabStrip) sessionRows() TabStrip {
	var out TabStrip
	fid, has := s.Focused()
	for i := 0; i < s.count; i++ {
		t0 := s.tabs[i]
		if t0.ID >= sessionIDBase {
			continue
		}
		if !out.OpenTab(t0.ID, t0.Title) {
			return TabStrip{}
		}
		j := out.count - 1
		out.tabs[j].Bin = t0.Bin
		out.tabs[j].Pinned = t0.Pinned
		out.tabs[j].Frozen = t0.Frozen
	}
	out.focus = -1 // set AFTER the loop: OpenTab focuses the first row
	if has {
		if i := out.index(fid); i >= 0 {
			out.focus = i
		}
	}
	return out
}

// noteSessionMutation is the write-through hook: every mutation that changed
// the strip calls it, and the file is correct the moment the mutation lands
// (no debounce, no save-on-exit, so a killed seat still leaves the truth
// behind). It always attempts the write — the write count stays bounded by
// CHANGES because no-op mutations return before reaching this hook, never
// by a timer. (The choreography's pin-stay snapshot in seat.go case 2 calls
// writeSession directly; equivalent — the declares already left dirty set.)
func noteSessionMutation() bool {
	sessionDirty = true
	return writeSession()
}

// writeSession encodes the live strip and writes SESSION.TABS. It refuses an
// EMPTY strip: a boot that closed everything must not publish a file that
// claims a session, and every reader already treats absent as defaults.
// M79g: this is also the choreography's pin-stay snapshot (seat.go case 2)
// and the write-through target of every user mutation.
func writeSession() bool {
	live := tabs.sessionRows()
	if live.count == 0 {
		return false
	}
	raw, ok := live.encodeTabsV2(sessionSeq)
	if !ok {
		vi.ConsoleLine("gotabwm: session write fail")
		return false
	}
	if !writeHostFile(sessionPath, raw) {
		vi.ConsoleLine("gotabwm: session write fail")
		return false
	}
	sessionDirty = false
	sessionSeq++
	vi.ConsoleLine(MarkerSessionWrite + vi.Itoa64(int64(live.count)))
	return true
}

// restoreSessionBytes keeps the missing/corrupt/present decisions testable on
// the host. A missing file is distinct from an invalid or valid empty session:
// only missing can request the first-boot workspace.
func restoreSessionBytes(strip *TabStrip, b []byte, readResult int64) (uint16, sessionLoadState) {
	if readResult < 0 || b == nil {
		return 0, sessionMissing
	}
	seq, ok := strip.applyTabsV2(b)
	if !ok {
		return 0, sessionCorrupt
	}
	return seq, sessionRestored
}

// loadSession reads SESSION.TABS. Missing is the first-boot branch; corrupt
// or truncated bytes fail closed, while a present valid file is restored.
func loadSession() sessionLoadState {
	b, r := vi.ReadFileAll(sessionPath, tabsV2MaxBytes)
	seq, state := restoreSessionBytes(&tabs, b, r)
	if state == sessionMissing {
		return state
	}
	if state == sessionCorrupt {
		vi.ConsoleLine(MarkerSessionBad)
		return state
	}
	sessionSeq = seq + 1
	if tabs.Count() == 0 {
		return state
	}
	// Placeholder ids (sessionIDBase+) are not kernel windows. Leave
	// hostedApp=0 so a later Wmctl* cannot aim at 0x100+i. stripDone
	// skips the live choreography that would close/split them.
	hostedApp = 0
	stripDone = true
	// M79g (#1718): `mode=restore` says what those ids ARE — placeholder
	// rows (>= sessionIDBase) read back from the file, NOT apps this boot
	// re-executed. Re-exec of restored tabs is a policy card of its own
	// (explicitly out of scope here); until then the marker is the honest
	// difference between a strip that came from disk and one that came
	// from a declare. Suffixed, so every existing grep for the load line
	// keeps matching.
	vi.ConsoleLine(MarkerSessionLoad + vi.Itoa64(int64(tabs.Count())) + " mode=restore")
	vi.ConsoleLine(MarkerSessionTitles + sessionTitlesLine(&tabs))
	// M71e (#1564): how many restored tabs came back carrying the frozen
	// badge — the observable that the flag survived the round-trip. A
	// separate line, not a field on the titles line above: that line's exact
	// shape is asserted by go-wm-tabs run 02.
	vi.ConsoleLine(MarkerSessionFreeze + vi.Itoa64(int64(tabs.FrozenCount())))
	dumpOrder()
	_ = writeLayoutFile()
	return state
}

// firstBootWorkspace is only for the default Go seat. A deliberately selected
// shim-only seat (wm=none) may exec GOTABWM for a gate, and the Zig fallback
// (wm=tabwm) owns its own startup path.
func firstBootWorkspace(state sessionLoadState, wm string) bool {
	return state == sessionMissing && wm == "gotabwm"
}

func hasLiveGuestELF(rows []vi.ProcRow) bool {
	for _, row := range rows {
		if row.State != vi.ProcCreated && row.State != vi.ProcRunning {
			continue
		}
		name := row.Name()
		// Init supervises the seat; it is not a competing workspace client.
		if name != "GOTABWM.ELF" && name != "INIT.ELF" && len(name) > 4 && name[len(name)-4:] == ".ELF" {
			return true
		}
	}
	return false
}

// anotherGuestProgram prevents an explicitly started app (for example,
// GOSH in a gate script) from being duplicated by the first-boot default.
func anotherGuestProgram() bool {
	var rows [64]vi.ProcRow
	n, r := vi.Procs(rows[:])
	return r >= 0 && hasLiveGuestELF(rows[:n])
}
