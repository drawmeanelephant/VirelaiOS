// Command edit is the M58b (issue #1306) Go editor: open a share file, type
// into a buffer, save it back, and close - full-viewport inside Zig TABWM via
// user/go/tabapp.
//
// M66c follow-on (#1485): the three contracts the Zig NOTEPAD.BIN was the last
// live home for landed here, so the leftover binary could be deleted without
// dropping coverage. GOEDIT is the app M66c's own non-goals named as the owner
// of the editor arms race, and this file is what moved into it:
//
//   - find + goto-line (M20 U3). Ctrl-F opens the find bar, Return searches
//     from the caret and prints `goedit: find '<pat>' hit=N/M`, Ctrl-G opens
//     the goto-line bar and Return prints `goedit: goto line=N offset=O` (or
//     `miss lines=L`) as it moves the caret. The pattern buffer, the match
//     total/ordinal and the line offsets are pure functions with host tests,
//     so the live gate asserts the rules the unit tests pin.
//   - the unsaved-changes dialog client (M42 UX r2, WMS8 Gate 4). The kernel
//     posts WIN_UNSAVED (kind 17) when the user picks Save on a dirty window:
//     this app publishes the buffer and exits. The dialog's other two choices
//     never reach the owner as kind 17 - the kernel closes the window itself,
//     so they arrive as WIN_CLOSE (kind 8), which is the ActionClosed arm. An
//     editor's buffer is published by Ctrl-S or by an explicit Save, never
//     silently by a close; that is the whole point of the dialog.
//
// What did NOT land here, and why: the M14 S3 composition selfdemo (clipboard
// slots 38/39 + an app-timer blink, slot 40). It belongs to the same rehome, but
// this app's data segment is the wrong place for it. kernel/src/exec.zig packs
// argv+envp (256 + 2048 bytes) into the data segment's tail while the Go
// runtime's sbrk heap starts at `memRound(firstmoduledata.end)`, and
// `mmap_collides` extends the data aperture through `argv_end_va`: an image whose
// `mem_size mod 4096 > 1792` has that block straddling the break start, so the
// runtime's FIRST sys_mmap is refused and the app dies with `fatal error:
// runtime: cannot allocate memory` inside mallocinit. Every referenced string
// literal costs 16 bytes of that segment, and this app's budget is 240 bytes
// (hand-measured on the 2026-09-19 tree, NOT gate-pinned: memsz 0x2f610 mod
// 4096 = 1552 against the 1792 wall — treat the exact figure as illustrative
// and re-measure before betting image bytes on it).
// The selfdemo needs ~192 of them, so it lives in a purpose-built probe,
// user/go/compose, whose own budget is ~1 KB. Take this file over the wall and
// the app stops booting, silently.
//
// Find/goto parity with the Zig app it replaced is CHORD-DEEP, deliberately and
// in writing (M66c review, #1495). The four deltas, so nobody re-litigates them
// as regressions:
//
//   - there is no case-sensitivity toggle, and the search is case-SENSITIVE
//     (matchAt is a byte compare). The Zig app was case-insensitive with a
//     compile-time constant and no live coverage for a toggle, so this is a
//     real behaviour delta, pinned by `TestFindIsCaseSensitive` in
//     edit_test.go rather than left to prose.
//   - the find/goto bars are not sticky: they close on a hit or a miss instead
//     of staying open for the next search.
//   - a miss keeps the caret where it was and prints `miss`; it does not
//     re-anchor or wrap.
//   - replace, match highlighting and the status-line "Line X of Y" are absent.
//     The Zig side had them; the live gate (`live-text-search`) never asserted
//     them, so no live coverage moved -- but they did not come across.
//
// What this is NOT: EDIT.BIN's feature list. The buffer is a byte slice with a
// caret, find is a forward substring search, and there is no selection, undo,
// replace or syntax highlighting.
//
// The palette stays local (colChromeBg/colPageBg) rather than importing
// webrender's theme: the full webrender package drags in layout + HTML parsing,
// and the Go runtime's init then exceeds the kernel's sbrk region budget
// (observed live as "runtime: cannot allocate memory" in mallocinit).
//
// Every marker below is printed only AFTER its syscall returned, so the
// go-edit VZ gate's asserts can only pass if the app actually ran.
package main

import (
	"strings"
	"unicode/utf8"

	"virelai/heap"
	"virelai/layout"
	"virelai/settings"
	"virelai/tabapp"
	"virelai/vi"
	"virelai/webrender/font"
)

const (
	appName  = "GOEDIT.ELF"
	appTitle = "Edit"
	natW     = 512
	natH     = 384

	// The marker lines the class-B gate greps.
	markerOpen    = "goedit: open id="
	markerDeclare = "goedit: declare accepted"
	markerRead    = "goedit: read "
	markerPresent = "goedit: present"
	markerDirty   = "goedit: dirty"
	markerSaved   = "goedit: saved "
	markerSaveErr = "goedit: save error "
	markerClose   = "goedit: close"
	markerOK      = "goedit OK"
	markerOpenErr = "goedit: error open "

	// M20 U3 rehomed (#1485): the find and goto-line bar results, in the Zig
	// app's exact shape (`find '<pat>' hit=N/M`, `goto line=N offset=O`,
	// `goto line=N miss lines=L`) so the gate that asserted them moved by
	// binary name and marker prefix alone.
	markerFind = "goedit: find '"
	markerGoto = "goedit: goto line="

	// M83e (#1778): the dead-key stage. `layout` names the keyboard layout
	// the editor composes under, printed once after the read. `stage` prints
	// when an accent becomes pending, AFTER the frame showing it was
	// presented, so the marker means the pending mark is on the scanout. The
	// composed or literal text is the saved file's business, not a marker.
	markerLayout = "goedit: layout "
	markerStage  = "goedit: stage "

	// M42 UX r2 / WMS8 Gate 4 rehomed (#1485): the unsaved-changes dialog.
	markerUnsaved = "goedit: win_unsaved"

	// M81d (#1764): the advisory write lease's console receipts. The open
	// marker names the holder pid the record now carries (the takeover's
	// receipt); the save-error line carries the -ErrEAGAIN refusal when
	// another writer holds it live.
	markerLeaseOK   = "goedit: lease acquired pid="
	markerLeaseHeld = "goedit: lease held rc="
	markerLeaseRel  = "goedit: lease released"
	// The gate fixture verb's receipt (see leaseFixture).
	markerLeaseFixture = "goedit: lease fixture "

	// modCtrl is the ADR 0009 Ctrl modifier bit; every chord below tests it.
	modCtrl = uint16(0x0002)
	// keyS/keyF/keyG are the LOWERCASE Unicode codepoints. kernel/src/input.zig
	// puts the decoded Unicode codepoint in arg1; Ctrl chord handling retains
	// the established ASCII control-code convention ('s' & 0x1f = 0x13), so
	// both spellings are accepted for the save chord.
	keyS     = 0x73
	keyCtrlS = 0x13
	keyF     = 0x66
	keyCtrlF = 0x06
	keyG     = 0x67
	keyCtrlG = 0x07
	// backspace/delete/return/escape codepoints (the kernel sends the
	// codepoint in arg1).
	codeBackspace = 0x08
	codeDelete    = 0x7f
	codeReturn    = 0x0d
	codeNewline   = 0x0a
	codeEscape    = 0x1b

	// barMax bounds the find pattern and the goto digits; gotoDigitsMax is the
	// Zig app's 5-digit bound on a goto line number.
	barMax        = 32
	gotoDigitsMax = 5

	// defaultPath is the share fixture the gate seeds.
	defaultPath = "/host/EDIT/SEED.TXT"
	// maxBuffer bounds the buffer to the SDK's file bound.
	maxBuffer = vi.MaxFileBytes
)

// Event kinds the SDK does not name. WIN_UNSAVED (kernel/src/events.zig kind
// 17) carries the unsaved-changes dialog's choice in arg0: 0 save, 1 don't
// save, 2 cancel. vi.EvTimer (9), vi.EvWinClose (8) and vi.EvWinResize (10)
// come from the SDK.
const evWinUnsaved uint16 = 17

// The editor's own palette (the browser's chrome/body tones).
const (
	colChromeBg = uint32(0x11171c)
	colPageBg   = uint32(0x182026)
	colInk      = uint32(0xe6edf3)
	colText     = uint32(0xe6edf3)
	// colCaret is the caret block and colDim the find/goto bar's label.
	colCaret = uint32(0xffd75f)
	colDim   = uint32(0x8b98a8)
)

// inputMode is which surface owns the keyboard: the document, the find bar, or
// the goto-line bar (M20 U3).
type inputMode uint8

const (
	modeEdit inputMode = iota
	modeFind
	modeGoto
)

// fill is one clamped background rectangle through the kernel fill batcher.
func (e *editor) fill(x, y, w, h int, rgb uint32) {
	if w <= 0 || h <= 0 || x < 0 || y < 0 {
		return
	}
	if e.rec != nil {
		e.rec(x, y, w, h, rgb)
		return
	}
	e.f.Rect(e.ta.Win, uint32(x), uint32(y), uint32(w), uint32(h), rgb)
}

// drawText paints text with the VirelaiOS 8x8 face (virelai/webrender/font),
// coalescing each row's lit run into ONE fill. It is deliberately local: the
// full webrender package drags in layout + HTML parsing, and the Go runtime's
// init then exceeds the kernel's 16-region sbrk budget (observed live as
// "runtime: cannot allocate memory" in mallocinit).
func (e *editor) drawText(x, y int, text string, rgb uint32) {
	cx := x
	for _, ch := range text {
		g := font.Glyph8(ch)
		for row := 0; row < 8; row++ {
			bits := g[row]
			col := 0
			for col < 8 {
				if bits&(1<<uint(col)) == 0 {
					col++
					continue
				}
				run := 1
				for col+run < 8 && bits&(1<<uint(col+run)) != 0 {
					run++
				}
				e.fill(cx+col, y+row, run, 1, rgb)
				col += run
			}
		}
		cx += font.Advance(1)
	}
}

// editor is the whole app state: the path, the byte buffer, the caret, and the
// dirty flag.
type editor struct {
	ta    *tabapp.TabApp
	path  string
	buf   []byte
	dirty bool
	f     vi.Filler
	// rec, when set, receives every fill instead of the kernel batcher. It is
	// nil in the guest; host tests set it because vi.Filler.Flush does not go
	// through the syscall-hook seam.
	rec func(x, y, w, h int, rgb uint32)

	// M20 U3: cur is the caret as a byte offset into buf, clamped on every use
	// so a buffer that shrank under it cannot index out of range; mode says
	// which bar owns the keyboard; bar holds that bar's typed text.
	cur  int
	mode inputMode
	bar  []byte

	// M83e (#1778): the dead-key stage. It sits in front of whichever surface
	// owns the keyboard (the document or a bar) and hands it the text a
	// sequence resolves to. marked is the accent whose `stage` marker was last
	// printed, so the marker is an edge like markDirty.
	stage  layout.Stage
	marked layout.Accent

	// M81d (#1764): the advisory write lease taken at open and held for the
	// session, so a file manager cannot delete or overwrite the file out
	// from under the editing session. nil when the open-time acquire was
	// refused (another writer holds it live) — every save then retries.
	lease *vi.FileLease
}

// leaseFixture stamps a FOREIGN lease record on the edited path before the
// editor's own acquire — the second writer's side of the M81d drill without
// needing a second process (the GOSELF --panic-receipt-fixture precedent:
// a named fixture verb, not a hidden side door). `live` carries a stamp no
// run outlives (the year 2286), `stale` one every rule reads as expired
// (ts=1); pid=0 keeps the record's liveness purely on the stamp, so the
// fixture is timing-proof either way. The record text is the on-share
// VLEASE1 convention, hand-rendered the way a foreign writer would.
func leaseFixture(path, mode string) bool {
	ts := "1"
	if mode == "live" {
		ts = "9999999999"
	} else if mode != "stale" {
		return false
	}
	lp := vi.LeasePathFor(path)
	if lp == "" {
		return false
	}
	// The record's directory may not exist yet (the fixture can run before
	// any acquire); the MODE_DIR create triple's EEXIST is fine.
	h, rc := vi.FileOpen(vi.LeaseDir, vi.ModeWrite|vi.ModeCreate|vi.ModeDir)
	if rc < 0 && rc != -9 {
		vi.ConsoleLine(markerLeaseFixture + mode + " rc=" + vi.Itoa64(rc))
		return false
	}
	if rc >= 0 {
		vi.FileClose(uint32(h))
	}
	rec := "VLEASE1\npid=0\nts=" + ts + "\ntoken=fedcba9876543210\npath=" + path + "\n"
	if rc := vi.WriteFileSafe(lp, []byte(rec)); rc < 0 {
		vi.ConsoleLine(markerLeaseFixture + mode + " rc=" + vi.Itoa64(rc))
		return false
	}
	vi.ConsoleLine(markerLeaseFixture + mode)
	return true
}

func main() {
	args := vi.Args()
	path := defaultPath
	if len(args) > 1 && len(args[1]) > 0 {
		path = args[1]
	}
	if len(args) > 2 {
		for _, a := range args[2:] {
			if a == "-heap" {
				heap.Publish(appName, 1)
			}
			if strings.HasPrefix(a, "--lease-fixture=") {
				leaseFixture(path, strings.TrimPrefix(a, "--lease-fixture="))
			}
		}
	}

	ta := tabapp.Init(tabapp.Config{
		Name:  appName,
		Title: appTitle,
		X:     32,
		Y:     32,
		W:     natW,
		H:     natH,
	})
	if ta == nil {
		vi.ConsoleLine(markerOpenErr + "-1")
		vi.Exit(1)
	}
	vi.ConsoleLine(markerOpen + vi.Itoa64(int64(ta.Win)))
	if ta.TabAware {
		vi.ConsoleLine(markerDeclare)
	} else {
		vi.ConsoleLine("goedit: declare refused")
	}

	e := &editor{ta: ta, path: path}
	e.load()
	// The layout is read once, at open: it decides which physical keys are
	// dead. A change made in GOSET while this editor is open applies to the
	// next one; until then an unmapped dead key is ignored, as it always was.
	e.stage.SetLayout(layoutFor(settings.Load()))
	vi.ConsoleLine(markerLayout + string(e.stage.Layout()))
	// M81d: the session's advisory write lease. A refusal is reported and
	// the editor stays usable — every save retries the acquire, so a
	// writer that lets go while we edit is honored on the next Ctrl-S.
	if rc := e.ensureLease(); rc < 0 {
		vi.ConsoleLine(markerLeaseHeld + vi.Itoa64(rc))
	}
	e.draw()
	ta.Present()
	// The first frame is on the scanout: the gate releases the injected
	// keystrokes on this marker, so the editor is already in its event loop.
	vi.ConsoleLine(markerPresent)

	for {
		ev, r, ok := vi.PollEventRaw()
		if !ok {
			if r < 0 {
				break
			}
			vi.Sleep(1)
			continue
		}
		// M42 UX r2: the dialog's Save choice is a request to publish the
		// buffer, not a close — handled before the tabapp dispatch, which has
		// no opinion about it.
		if ev.Kind == evWinUnsaved {
			e.unsavedExit(ev)
		}
		switch ta.Dispatch(ev) {
		case tabapp.ActionClosed:
			e.releaseLease()
			vi.ConsoleLine(markerClose)
			vi.ConsoleLine(markerOK)
			ta.CloseAndExit(0)
		case tabapp.ActionResized:
			e.draw()
			ta.Present()
		case tabapp.ActionNone:
			if e.handle(ev) {
				e.draw()
				ta.Present()
				e.markStage()
			}
		}
	}
	// The event loop broke on a poll error — the same exit discipline as the
	// clean paths, so a lease is not left standing on a dead session.
	e.releaseLease()
}

// load reads the fixture into the buffer. A missing file is not fatal: the
// editor starts on an empty buffer (the create path), which is what a real
// editor does, and the read marker reports the byte count it actually got.
// The caret starts at the END of the buffer, which is what keeps the go-edit
// gate's typed characters appending to the seed exactly as they did before the
// caret existed.
func (e *editor) load() {
	b, rc := vi.ReadFileAll(e.path, maxBuffer)
	if rc < 0 {
		e.buf = nil
		e.cur = 0
		vi.ConsoleLine(markerRead + e.path + " n=0 rc=" + vi.Itoa64(rc))
		return
	}
	e.buf = b
	e.cur = len(b)
	vi.ConsoleLine(markerRead + e.path + " n=" + vi.Itoa64(int64(len(b))))
}

// layoutFor is the keyboard layout the kernel is translating under: the
// settings file's keyboard_layout, else the compiled default. A value the
// layout package does not know (the kernel refuses it too) is US.
func layoutFor(f settings.File) layout.ID {
	v, _ := f.Effective("keyboard_layout")
	if id, ok := layout.Parse(v); ok {
		return id
	}
	return layout.US
}

// handle is the loop's entry for events that are not window lifecycle. Losing
// focus types a staged accent as its literal, so a sequence abandoned by
// switching tabs is never swallowed.
func (e *editor) handle(ev vi.Event) bool {
	if ev.Kind == vi.EvWinBlur {
		return e.commitAll(e.stage.Flush())
	}
	return e.key(ev)
}

// key runs one key-down through the dead-key stage and then, unless the stage
// consumed it, through handleKey. Whatever the stage resolved to is inserted
// first, so a key that ends a sequence without being text (Ctrl-S, Return)
// still finds the accent already in the buffer. It reports whether the frame
// needs a redraw, which a changed pending state also requires.
func (e *editor) key(ev vi.Event) bool {
	if ev.Kind != vi.EvKeyDown {
		return false
	}
	before, _ := e.stage.Pending()
	step := e.stage.Feed(layout.Input{
		Usage: ev.Arg0,
		Shift: ev.Flags&vi.ModShift != 0,
		Chord: ev.Flags&(vi.ModCtrl|vi.ModAlt) != 0,
		Sym:   rune(ev.Arg1),
	})
	changed := e.commitAll(step.Commit)
	if after, _ := e.stage.Pending(); after != before {
		changed = true
	}
	if !step.Pass {
		return changed
	}
	return e.handleKey(ev) || changed
}

// commitAll inserts resolved text into the surface that owns the keyboard.
func (e *editor) commitAll(rs []rune) bool {
	changed := false
	for _, r := range rs {
		if e.mode == modeEdit {
			changed = e.insert(r) || changed
		} else {
			changed = e.barAppend(r) || changed
		}
	}
	return changed
}

// markStage prints the `stage` marker when an accent becomes pending. Call it
// after the frame that shows the mark was presented.
func (e *editor) markStage() {
	if m := e.stageMarker(); m != "" {
		vi.ConsoleLine(m)
	}
}

// stageMarker is the line markStage prints now, or "" when the pending state
// has not changed since the last one. It is separate so a host test can pin
// the edge without a console (vi.ConsoleLine does not go through the
// syscall-hook seam).
func (e *editor) stageMarker() string {
	a, ok := e.stage.Pending()
	if !ok {
		e.marked = 0
		return ""
	}
	if a == e.marked {
		return ""
	}
	e.marked = a
	return markerStage + a.Name()
}

// handleKey feeds one event to the buffer or to the focused bar, and reports
// whether the frame needs a redraw. The bar chords own the keyboard from ANY
// mode, so a Ctrl-G straight after a find's Return opens the goto bar instead
// of being swallowed as text.
func (e *editor) handleKey(ev vi.Event) bool {
	if ev.Kind != vi.EvKeyDown {
		return false
	}
	if isChord(ev, keyF, keyCtrlF) {
		return e.openBar(modeFind)
	}
	if isChord(ev, keyG, keyCtrlG) {
		return e.openBar(modeGoto)
	}
	switch e.mode {
	case modeFind:
		return e.barKey(ev, e.runFind)
	case modeGoto:
		return e.barKey(ev, e.runGoto)
	}
	if isSave(ev) {
		// A failed Ctrl-S leaves the app alive and the buffer dirty: the
		// error marker is the receipt, and the next Ctrl-S tries again.
		// (Only the dialog's Save choice treats failure as terminal --
		// there the user asked to publish and close.)
		if !e.save() {
			return true
		}
		return true
	}
	if ev.Flags&modCtrl != 0 {
		return false
	}
	if ev.Arg1 == codeBackspace || ev.Arg1 == codeDelete {
		return e.backspace()
	}
	if ch, ok := insertionFor(ev); ok {
		return e.insert(ch)
	}
	return false
}

// isChord reports whether ev is the Ctrl chord for a key, accepting either the
// bare codepoint or its derived control code (the duality isSave has always
// handled).
func isChord(ev vi.Event, key, ctrlCode uint32) bool {
	if ev.Kind != vi.EvKeyDown || ev.Flags&modCtrl == 0 {
		return false
	}
	return ev.Arg1 == key || ev.Arg1 == ctrlCode
}

// isSave reports whether the event is the Ctrl-S save chord.
func isSave(ev vi.Event) bool { return isChord(ev, keyS, keyCtrlS) }

// openBar focuses a bar with an empty entry.
func (e *editor) openBar(m inputMode) bool {
	e.mode = m
	e.bar = e.bar[:0]
	return true
}

// barKey handles one key while a bar owns the keyboard: Return runs the bar's
// action and closes it, Escape closes it without one, backspace trims it, and
// printable codepoints append (bounded by barMax).
func (e *editor) barKey(ev vi.Event, run func()) bool {
	if ev.Flags&modCtrl != 0 {
		return false
	}
	if ev.Arg1 == codeEscape {
		e.mode, e.bar = modeEdit, e.bar[:0]
		return true
	}
	if ev.Arg1 == codeReturn || ev.Arg1 == codeNewline {
		run()
		return true
	}
	if ev.Arg1 == codeBackspace || ev.Arg1 == codeDelete {
		if len(e.bar) == 0 {
			return false
		}
		_, size := utf8.DecodeLastRune(e.bar)
		e.bar = e.bar[:len(e.bar)-size]
		return true
	}
	if ch, ok := insertionFor(ev); ok {
		return e.barAppend(ch)
	}
	return false
}

// barAppend adds one scalar to the focused bar's entry, bounded by barMax.
func (e *editor) barAppend(ch rune) bool {
	if len(e.bar)+utf8.RuneLen(ch) > barMax {
		return false
	}
	e.bar = append(e.bar, []byte(string(ch))...)
	return true
}

// runFind runs the find bar's Return: search from the caret, move it to the
// match, and report the result. An empty pattern is a no-op with no marker.
func (e *editor) runFind() {
	pat := e.bar
	e.mode, e.bar = modeEdit, e.bar[:0]
	if len(pat) == 0 {
		return
	}
	total, ordinal, off := findFrom(e.buf, pat, e.cur)
	if off < 0 {
		vi.ConsoleLine(findMissMarker(pat))
		return
	}
	e.cur = off
	vi.ConsoleLine(findMarker(pat, ordinal, total))
}

// runGoto runs the goto bar's Return: move the caret to 1-based line n and
// report where that is, or how many lines the buffer actually has. An empty,
// non-digit, zero or over-long entry reports nothing (the Zig app's rule).
func (e *editor) runGoto() {
	digits := e.bar
	e.mode, e.bar = modeEdit, e.bar[:0]
	n, ok := parseLine(digits)
	if !ok {
		return
	}
	off := lineOffset(e.buf, n)
	if off < 0 {
		vi.ConsoleLine(gotoMissMarker(n, countLines(e.buf)))
		return
	}
	e.cur = off
	vi.ConsoleLine(gotoMarker(n, off))
}

// unsavedExit answers the unsaved-changes dialog: arg0 == 0 is the Save choice
// (the only one the kernel posts today; the dialog's other two choices close
// the window in the kernel, so they arrive as WIN_CLOSE and take the
// ActionClosed arm with no write at all). The markers come in the Zig client's
// order - the save reports first, then the dialog response, then the clean
// close - so the gate's stage marker keeps meaning the bytes are on the share.
func (e *editor) unsavedExit(ev vi.Event) {
	published := true
	if ev.Arg0 == 0 {
		published = e.save()
	}
	e.releaseLease()
	vi.ConsoleLine(markerUnsaved)
	vi.ConsoleLine(markerClose)
	if !published {
		// The dialog promised to save, and the save failed. Exiting 0 here
		// would be data loss wearing a clean status (M66c review, #1495):
		// the error marker above is the receipt, and the non-zero exit is
		// what lets a caller tell the two apart.
		e.ta.CloseAndExit(1)
	}
	vi.ConsoleLine(markerOK)
	e.ta.CloseAndExit(0)
}

// insertionFor maps a key event to the Unicode scalar it inserts. The kernel
// puts the decoded codepoint in arg1 (ADR 0014); Return becomes a newline.
func insertionFor(ev vi.Event) (rune, bool) {
	if ev.Kind != vi.EvKeyDown || ev.Flags&modCtrl != 0 {
		return 0, false
	}
	if ev.Arg1 == codeReturn || ev.Arg1 == codeNewline {
		return '\n', true
	}
	if (ev.Arg1 >= 0x20 && ev.Arg1 < 0x7f || ev.Arg1 >= 0xa0) &&
		ev.Arg1 <= utf8.MaxRune &&
		(ev.Arg1 < 0xd800 || ev.Arg1 > 0xdfff) {
		return rune(ev.Arg1), true
	}
	return 0, false
}

// insert splices one UTF-8 encoded scalar at the byte caret and marks the
// buffer dirty (the dirty marker prints once, on the first edit).
func (e *editor) insert(ch rune) bool {
	encoded := []byte(string(ch))
	if len(e.buf)+len(encoded) > maxBuffer {
		return false
	}
	e.clampCaret()
	oldLen := len(e.buf)
	e.buf = append(e.buf, make([]byte, len(encoded))...)
	copy(e.buf[e.cur+len(encoded):], e.buf[e.cur:oldLen])
	copy(e.buf[e.cur:], encoded)
	e.cur += len(encoded)
	e.markDirty()
	return true
}

// backspace removes the Unicode scalar before the byte caret.
func (e *editor) backspace() bool {
	e.clampCaret()
	if e.cur == 0 {
		return false
	}
	_, size := utf8.DecodeLastRune(e.buf[:e.cur])
	copy(e.buf[e.cur-size:], e.buf[e.cur:])
	e.buf = e.buf[:len(e.buf)-size]
	e.cur -= size
	e.markDirty()
	return true
}

// clampCaret keeps the caret inside the buffer, so a buffer that shrank under
// it (a host test builds an editor by hand) cannot make an index panic.
func (e *editor) clampCaret() {
	if e.cur < 0 {
		e.cur = 0
	}
	if e.cur > len(e.buf) {
		e.cur = len(e.buf)
	}
}

func (e *editor) markDirty() {
	if e.dirty {
		return
	}
	e.dirty = true
	vi.ConsoleLine(markerDirty)
}

// save publishes the whole buffer to the path and reports whether the bytes
// are on the share.
//
// M66c review (#1495): the first version of this function was in-place
// (FileOpen + a FileWrite loop + FileTruncate), which is parity with the Zig
// app it replaced but the wrong side of the milestone: M66b's whole point is
// that a `/host` write is a PUBLISH, not an overwrite. M66a/M66b landed
// vi.WriteFileSafe (temp + fsync + delete + rename, fail-closed, no in-place
// truncation of the live path) exactly so the M66 apps inherit it, and the app
// carrying the unsaved-decline contract is the worst place to keep a
// half-write window: the dialog's Save choice is what a user reaches for when
// they are already afraid of losing the buffer.
//
// The publish is one call, so there is no partial-write branch to report:
// WriteFileSafe either puts the whole body at the path or removes its temp and
// returns the failing step's negative code. The marker keeps the Zig shape
// (`goedit: saved <path> n=<bytes>`) because the gates parse it.
//
// Failure is REPORTED and not swallowed: the error marker is printed here (the
// only place this app writes) and the false return is what the callers use to
// decide whether they may report a clean exit.
func (e *editor) save() bool {
	if rc := e.ensureLease(); rc < 0 {
		vi.ConsoleLine(markerSaveErr + e.path + " " + vi.Itoa64(rc))
		return false
	}
	if rc := vi.WriteFileSafe(e.path, e.buf); rc < 0 {
		vi.ConsoleLine(markerSaveErr + e.path + " " + vi.Itoa64(rc))
		return false
	}
	e.dirty = false
	vi.ConsoleLine(markerSaved + e.path + " n=" + vi.Itoa64(int64(len(e.buf))))
	return true
}

// ensureLease acquires or refreshes this editor's advisory write lease
// (M81d #1764). The open-time call takes the lease for the session; every
// save re-stamps it first, so a session longer than vi.LeaseExpirySeconds
// renews instead of fighting its own record. A refusal (-ErrEAGAIN: another
// writer holds the lease live) is the caller's sign to report and back off;
// a lease that was taken over meanwhile drops the stale handle so the next
// save retries from scratch.
func (e *editor) ensureLease() int64 {
	if e.lease == nil {
		l, rc := vi.AcquireFileLease(e.path, appName)
		if rc < 0 {
			return rc
		}
		e.lease = l
		vi.ConsoleLine(markerLeaseOK + vi.Itoa64(int64(l.PID)))
		return 0
	}
	if rc := e.lease.Refresh(); rc < 0 {
		e.lease = nil
		return rc
	}
	return 0
}

// releaseLease drops the session's lease at every exit path. A lease that
// was taken over meanwhile belongs to the new holder and Release refuses to
// delete it — the marker says which happened, because the gates read these
// lines.
func (e *editor) releaseLease() {
	if e.lease == nil {
		return
	}
	if rc := e.lease.Release(); rc >= 0 {
		vi.ConsoleLine(markerLeaseRel)
	} else {
		vi.ConsoleLine(markerLeaseRel + " rc=" + vi.Itoa64(rc))
	}
	e.lease = nil
}

// findMarker is the find bar's serial result, in the Zig app's exact shape:
// the ordinal is 1-based among all matches.
func findMarker(pat []byte, ordinal, total int) string {
	return markerFind + string(pat) + "' hit=" + vi.Itoa64(int64(ordinal)) + "/" + vi.Itoa64(int64(total))
}

// findMissMarker is the find bar's no-match shape.
func findMissMarker(pat []byte) string {
	return markerFind + string(pat) + "' no-match"
}

// gotoMarker is the goto bar's success shape.
func gotoMarker(line, offset int) string {
	return markerGoto + vi.Itoa64(int64(line)) + " offset=" + vi.Itoa64(int64(offset))
}

// gotoMissMarker is the goto bar's beyond-the-buffer shape.
func gotoMissMarker(line, lines int) string {
	return markerGoto + vi.Itoa64(int64(line)) + " miss lines=" + vi.Itoa64(int64(lines))
}

// findFrom searches buf for pat from byte offset `from`, and returns the
// number of non-overlapping matches, the 1-based ordinal of the match the
// caret lands on, and that match's offset. The first match at or after the
// caret wins; when there is none the search wraps to the FIRST match (the find
// bar's "search again from the top" rule, and what makes Return on a
// single-match document report `hit=1/1`). No match returns -1.
func findFrom(buf, pat []byte, from int) (total, ordinal, off int) {
	off, ordinal = -1, 0
	if len(pat) == 0 || len(pat) > len(buf) {
		return 0, 0, -1
	}
	for i := 0; i+len(pat) <= len(buf); {
		if matchAt(buf, i, pat) {
			total++
			if off < 0 && i >= from {
				off, ordinal = i, total
			}
			i += len(pat)
			continue
		}
		i++
	}
	if off < 0 {
		for i := 0; i+len(pat) <= len(buf); i++ {
			if matchAt(buf, i, pat) {
				return total, 1, i
			}
		}
	}
	return total, ordinal, off
}

// matchAt reports whether pat matches at offset off.
func matchAt(buf []byte, off int, pat []byte) bool {
	if off < 0 || len(pat) == 0 || off+len(pat) > len(buf) {
		return false
	}
	for i := 0; i < len(pat); i++ {
		if buf[off+i] != pat[i] {
			return false
		}
	}
	return true
}

// countLines is the number of '\n'-separated lines. An empty buffer is one
// empty line, and a trailing newline opens a final empty one - the Zig app's
// count_lines rule verbatim, because the goto bar reports it.
func countLines(buf []byte) int {
	n := 1
	for _, b := range buf {
		if b == '\n' {
			n++
		}
	}
	return n
}

// lineOffset is the byte offset where 1-based line n starts (0 for line 1), or
// -1 when the buffer has fewer than n lines.
func lineOffset(buf []byte, n int) int {
	if n < 1 {
		return -1
	}
	if n == 1 {
		return 0
	}
	line := 1
	for i, b := range buf {
		if b != '\n' {
			continue
		}
		line++
		if line == n {
			return i + 1
		}
	}
	return -1
}

// parseLine parses the goto bar's digits as a 1-based line number. Empty,
// non-digit, zero and beyond-the-5-digit-bound entries are refused (the Zig
// app's rule), and a refused entry prints no marker at all: the gate greps
// only the two result shapes.
func parseLine(digits []byte) (int, bool) {
	if len(digits) == 0 || len(digits) > gotoDigitsMax {
		return 0, false
	}
	v := 0
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int(c-'0')
		if v > 99999 {
			return 0, false
		}
	}
	if v == 0 {
		return 0, false
	}
	return v, true
}

// Layout constants for the frame.
const (
	chromeH    = 24
	lineH      = 10
	maxLines   = 64
	textOrigin = 6
	caretW     = 2
)

// draw repaints the frame: a chrome bar with the path, the buffer line by
// line through the M56e text surface, the caret, and the focused bar's label.
// The fill batcher is flushed once.
func (e *editor) draw() {
	w, h := int(e.ta.W), int(e.ta.H)
	if w <= 0 || h <= 0 {
		w, h = natW, natH
	}
	e.fill(0, 0, w, chromeH, colChromeBg)
	e.fill(0, chromeH, w, h-chromeH, colPageBg)
	e.drawText(textOrigin, 8, "Edit  "+e.path, colInk)

	adv := font.Advance(1)
	// M83e: a staged accent belongs to the surface that owns the keyboard, so
	// the document shows it only in edit mode; a bar shows it below.
	pre := e.stage.Preedit()
	inDoc := pre != "" && e.mode == modeEdit

	y := chromeH + 4
	line, start := 0, 0
	caretRow, caretCol := -1, 0
	for i := 0; i <= len(e.buf); i++ {
		if i != len(e.buf) && e.buf[i] != '\n' {
			continue
		}
		if line >= maxLines {
			break
		}
		onCaret := e.cur >= start && e.cur <= i
		if inDoc && onCaret {
			// The accent is drawn where it will land, and the text after the
			// caret moves right one cell, as it will when the accent commits.
			col := utf8.RuneCount(e.buf[start:e.cur])
			e.drawText(textOrigin, y, string(e.buf[start:e.cur]), colText)
			e.drawPreedit(textOrigin+col*adv, y, pre)
			e.drawText(textOrigin+(col+1)*adv, y, string(e.buf[e.cur:i]), colText)
		} else if i > start {
			e.drawText(textOrigin, y, string(e.buf[start:i]), colText)
		}
		// M20 U3: the caret's cell, so a find or a goto visibly MOVED it.
		if onCaret {
			caretRow, caretCol = line, utf8.RuneCount(e.buf[start:e.cur])
			if inDoc {
				caretCol++
			}
		}
		y += lineH
		line++
		start = i + 1
	}
	if caretRow >= 0 && caretRow < maxLines {
		e.fill(textOrigin+caretCol*adv, chromeH+4+caretRow*lineH, caretW, 8, colCaret)
	}
	if e.mode != modeEdit {
		label := "Find: "
		if e.mode == modeGoto {
			label = "Goto: "
		}
		barY := h - lineH - 2
		e.drawText(textOrigin, barY, label+string(e.bar), colDim)
		if pre != "" {
			col := utf8.RuneCountInString(label) + utf8.RuneCount(e.bar)
			e.drawPreedit(textOrigin+col*adv, barY, pre)
		}
	}
	_ = e.f.Flush()
}

// drawPreedit paints a pending dead-key accent in the caret colour with an
// underline in the spare pixel row under the glyph (lines are lineH apart and
// glyphs are 8 rows), so it reads as text that is not committed yet.
func (e *editor) drawPreedit(x, y int, mark string) {
	e.drawText(x, y, mark, colCaret)
	e.fill(x, y+8, font.Advance(1), 1, colCaret)
}
