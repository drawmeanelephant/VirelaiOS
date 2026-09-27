// GOTABWM.ELF — M63b–e (issues #1420–#1423): chords, rail click/drag, type-in.
//
// Frozen table on #1418, plus M71d (#1563) BT1 reopen/duplicate and
// M79f (#1717) keyboard parity:
//
//	ctrl-shift-p  -> Pin() the focused tab
//	ctrl-shift-t  -> reopen the most recently closed tab (re-exec its bin)
//	ctrl-shift-d  -> duplicate the focused tab (re-exec its bin)
//	ctrl-shift-f  -> toggle the focused tab's frozen BADGE (M71e / #1564)
//	alt-tab       -> FocusTab + WmctlTaskbarClick (cmd 12), wrapping
//	ctrl-tab      -> the same wrapping cycle; shift reverses it (M79f)
//	ctrl-1..9     -> focus the Nth rail cell; out-of-range is a no-op (M79f)
//	rail click    -> top strip, equal-width cells, same TASKBAR+FocusTab
//	rail drag     -> press/release over different cells → existing Reorder()
//	rail close-x  -> the cell's rightmost railCloseW px: close that tab (M79b)
//	rail hover    -> the cell under the pointer tints; entry marker (M79b)
//	sash drag     -> press-drag-release on the split divider re-proportions
//	                 the panes through applySashDrag (M79c); motion while
//	                 armed is chrome, never content
//	ctrl-shift-v  -> split cycle none -> V -> H -> none (M79c: live mode has
//	                no other split entry; the choreography is demo-only)
//	ctrl-shift-s  -> take a SNAPSHOT BUNDLE (M81g: settings + SESSION.TABS +
//	                a docs selection, published crash-safe to
//	                /host/SNAPSHOT.BUNDLE; snapshot.go)
//	ordinary keys -> ignored here (ADR 0009: KEY_DOWN still reaches the app)
//
// Markers print only after the mutation/syscall that made them true.
// Ctrl+W is not bound (it collides with the editor).
// M79b (#1705): the rail's close-x and hover highlight live here. Hover-
// preview stays a later card. Client-area is ignored.
//
// M79c (#1706): the sash drag and the split-cycle chord live here too. No
// keyboard equivalent for the drag itself: a divider is a pointer target,
// and the cycle chord is the keyboard story, stated here rather than
// discovered later.
//
// M71d pick: Ctrl+Shift+T / Ctrl+Shift+D are Zig TABWM's own BT1 bindings
// (user/src/tabwm.zig:151/:427). They are free on GOTABWM — the only
// ctrl-shift chord bound here is ctrl-shift-p, and Ctrl+T (Zig's "+ New
// tab") is not bound at all on this seat, whose launcher is Ctrl+Space.
//
// M71e pick: Ctrl+Shift+F is Zig TABWM's own freeze binding (user/src/
// tabwm.zig:1448). It is a BADGE there too — Zig checks `frozen` in no close
// path — so the port keeps that shape rather than inventing a lock.
package main

import "virelai/vi"

const (
	MarkerAltTab    = "gotabwm: alt-tab id="
	MarkerRailClick = "gotabwm: rail-click id="
	// M79b (#1705): hover entry on a rail cell (transitions only, markRail's
	// discipline), naming the cell's tab id like rail-click does.
	MarkerRailHover = "gotabwm: rail-hover id="
	// M71d (#1563): the BT1 chord outcomes. <bin> is the re-exec'd executable,
	// matching Zig's `tabwm: reopen <bin>` / `tabwm: duplicate <bin>`.
	MarkerReopen           = "gotabwm: reopen "
	MarkerDuplicate        = "gotabwm: duplicate "
	MarkerReopenMissing    = "gotabwm: reopen missing "
	MarkerDuplicateMissing = "gotabwm: duplicate missing "

	// USB HID keyboard usages (the kernel's WM_KEY arg0).
	hidUsageD uint8 = 0x07 // 'd'
	hidUsageF uint8 = 0x09 // 'f'; M71e (#1564) freeze-badge toggle
	hidUsageP uint8 = 0x13
	hidUsageS uint8 = 0x16 // 's'; M81g (#1767) snapshot bundle
	hidUsageT uint8 = 0x17 // 't'
	hidUsageV uint8 = 0x19 // 'v'; M79c (#1706) split cycle
	// M79e (#1708): M48/BT5's per-tab history chords, the SAME two Zig
	// binds in the same ctrl+shift block (Zig tabwm's usage_left_bracket /
	// usage_right_bracket). Additive: nothing existing is rebound.
	hidUsageLeftBracket  uint8 = 0x2F // '['
	hidUsageRightBracket uint8 = 0x30 // ']'
	hidUsageTab          uint8 = 0x2B
	hidBtnLeft           uint8 = 0x01
)

// execApp is the exec seam for the BT1 chords. It is vi.Exec in the guest;
// the indirection exists because vi.Exec calls the raw syscall4 gateway and so
// bypasses vi's host-test syscall hook, which would leave the chord -> re-exec
// path unobservable off the guest.
var execApp = vi.Exec

// hidChordHold is how many composite ticks the two-tab choreography waits
// after first seeing n>=2, so a rail click (3×2.5 s), a press/release drag
// (pointerDragHold), `--input-string` into GOEDIT, and `--input-chords`
// land before auto reorder/pin. The type-in and drag boots are separate.
// M73z (#1638): raised 20 -> 32 so go-dogfood boot 04's acceptance
// chain (6-step pointer phase incl. the refocus rail click, chords,
// script2 tail, expect) finishes before the choreography's first
// close (n==2 + hold + 7 steps ~ tick 68).
// Claim 1747: raised 32 -> 64 after the pump fix. The seat drains WM_KEY
// ONE event per composite tick and Sleep(1)s after each while the
// launcher is closed, so a virtio chord burst drained inside one outer
// iteration costs N extra scheduler ticks of wall time. Observed on
// go-dogfood boot 04: the demo seat's first auto-close beat script2's
// `dui lower 4`, so the run's expect marker never arrived. The wider
// hold keeps the acceptance tail bounded but unable to lose that step.
const hidChordHold = 64

// pointerDragHold is one `--pointer-virtio 'x,y,d;x,y,u'` (4 messages × 2.5 s).
const pointerDragHold = 12

// prevPtrButtons is the last kind-19 flags low byte; rail click is a left
// down edge and drag-reorder commits on the matching up edge, matching
// Zig TABWM begin_tab_drag / end_tab_drag.
var (
	prevPtrButtons uint8
	railDragFrom   = -1 // source cell, or -1 when no drag is armed
	// contentDown tracks a pointer-down that went to the kernel as content
	// (#1688): the matching release is content too, wherever it lands.
	// Chrome downs (start surface, rail) never set it.
	contentDown bool
	// railHover is the M79b (#1705) hovered rail cell (index), or -1 while
	// the pointer rests off the rail. Paint state; updateRailHover owns it
	// and the entry marker.
	railHover = -1
	// sashDragging is the M79c (#1706) sash latch: a pointer-down on the
	// split divider arms it, the matching release commits the drag at the
	// release x/y, and motion while armed is swallowed (chrome, never
	// content — same discipline as contentDown/rail, inverted). It is
	// disjoint from railDragFrom: a sash down returns before
	// beginRailDrag, so endRailDrag can never also reorder.
	sashDragging bool
)

func handleWmKey(e vi.Event) {
	if handleLauncherKey(e) {
		return
	}
	usage := uint8(e.Arg0)
	ctrl := e.Flags&vi.ModCtrl != 0
	shift := e.Flags&vi.ModShift != 0
	alt := e.Flags&vi.ModAlt != 0
	if alt && !ctrl && usage == hidUsageTab {
		_ = applyAltTab(shift)
		return
	}
	// M79f (#1717): the ctrl branch sits before ctrl-shift so tab and
	// digits have one dispatch point while the launcher above keeps
	// modal precedence. Ctrl+Shift+Tab reverses the same cycle; a
	// digit outside the current strip is an honest no-op.
	if ctrl && !alt {
		if usage == hidUsageTab {
			_ = applyAltTab(shift)
			return
		}
		if i, ok := ctrlIndex(usage); ok {
			_ = applyCtrlIndex(i)
			return
		}
	}
	if ctrl && shift && !alt {
		switch usage {
		case hidUsageP:
			_ = applyHidPin()
		case hidUsageT:
			_ = applyReopen()
		case hidUsageD:
			_ = applyDuplicate()
		case hidUsageF:
			_ = applyFreezeToggle()
		case hidUsageV:
			_ = applySplitCycle()
		// M81g (#1767): the snapshot arm. It is a chord rather than a
		// timer on purpose — the card rules out scheduled snapshots, and a
		// bundle taken because time passed is a bundle nobody chose. Free
		// on this seat (Zig TABWM binds no ctrl-shift-s) and additive:
		// nothing existing is rebound.
		case hidUsageS:
			_ = saveSnapshot()
		// M79e (#1708): back/forward for the FOCUSED tab, queued for its
		// app to poll. The step is refused (and silent) at either end of
		// the history, so the chord is a no-op rather than a marker for
		// a step that did not happen.
		case hidUsageLeftBracket:
			_ = applyNavStep(true)
		case hidUsageRightBracket:
			_ = applyNavStep(false)
		}
		return
	}
	// M71e (#1564): Enter on an empty strip is the start surface's keyboard
	// affordance — it summons the same launcher the panel points at.
	if !ctrl && !shift && !alt && usage == hidUsageEnter && tabs.Count() == 0 {
		openLauncher()
	}
}

// applyNavStep is Zig TABWM nav_back_active / nav_forward_active: step the
// FOCUSED tab's history one entry and queue the target so the app picks it up
// on its next nav-poll. `back` selects the direction. False when nothing is
// focused, or the cursor is already at that end — an honest no-op that
// leaves the queued slot alone, so a refused step cannot swallow the target
// an earlier accepted step is still waiting to deliver.
func applyNavStep(back bool) bool {
	id, ok := tabs.Focused()
	if !ok {
		return false
	}
	var (
		path  string
		moved bool
	)
	if back {
		path, moved = tabs.NavBack(id)
	} else {
		path, moved = tabs.NavForward(id)
	}
	if !moved {
		return false
	}
	setPendingNav(id, path)
	marker := MarkerNavForward
	if back {
		marker = MarkerNavBack
	}
	vi.ConsoleLine(marker + vi.Itoa64(int64(id)) + " path=" + path)
	return true
}

// applyFreezeToggle is Zig TABWM freeze_toggle: flip the frozen badge on the
// focused tab and report which way it went. M71e (#1564) is deliberate about
// the shape — this is a BADGE, not a lock: Zig checks `frozen` nowhere in its
// close path, and neither does GOTABWM, so a frozen tab still closes.
func applyFreezeToggle() bool {
	id, ok := tabs.Focused()
	if !ok {
		return false
	}
	if tabs.Freeze(id) {
		vi.ConsoleLine(MarkerFreeze + vi.Itoa64(int64(id)) + " on")
		dumpOrder()
		// M79g (#1718): the badge is part of the session, so both
		// directions persist — a freeze the file does not carry was
		// never saved, only displayed.
		noteSessionMutation()
		return true
	}
	if tabs.Thaw(id) {
		vi.ConsoleLine(MarkerThaw + vi.Itoa64(int64(id)))
		dumpOrder()
		noteSessionMutation()
		return true
	}
	return false
}

// applyReopen is Zig TABWM reopen_last_closed(): pop the bounded LIFO, skip
// entries with no recorded bin, and re-exec the executable as a new hosted
// tab. A tab the seat never spawned has no bin and is an honest no-op.
func applyReopen() bool {
	c, ok := tabs.ReopenLastClosed()
	if !ok {
		return false
	}
	if _, err := execApp(c.Bin); err != nil {
		vi.ConsoleLine(MarkerReopenMissing + c.Bin)
		return false
	}
	vi.ConsoleLine(MarkerReopen + c.Bin)
	return true
}

// applyDuplicate is Zig TABWM duplicate_active_tab(): re-exec the focused
// tab's executable so it joins as a new tab. Honest no-op when nothing is
// focused or the focused tab has no recorded bin.
func applyDuplicate() bool {
	bin, ok := tabs.DuplicateFocused()
	if !ok {
		return false
	}
	if _, err := execApp(bin); err != nil {
		vi.ConsoleLine(MarkerDuplicateMissing + bin)
		return false
	}
	vi.ConsoleLine(MarkerDuplicate + bin)
	return true
}

// ctrlIndex maps the USB HID number-row usages for 1..9 to rail indexes.
// Ctrl+0 is not a rail shortcut.
func ctrlIndex(usage uint8) (int, bool) {
	if usage < 0x1e || usage > 0x26 {
		return 0, false
	}
	return int(usage - 0x1d), true
}

// altTabNext is Zig TABWM's alt_tab_next: the tab after `focus`, wrapping.
// Fewer than two tabs is a no-op. shift inverts. focus out of range starts at 0.
func altTabNext(count, focus int, shift bool) (int, bool) {
	if count < 2 {
		return 0, false
	}
	cur := focus
	if cur < 0 || cur >= count {
		cur = 0
	}
	if shift {
		if cur == 0 {
			return count - 1, true
		}
		return cur - 1, true
	}
	return (cur + 1) % count, true
}

func applyHidPin() bool {
	id, ok := tabs.Focused()
	if !ok {
		return false
	}
	if !tabs.Pin(id) {
		return false
	}
	vi.ConsoleLine(MarkerPin + "id=" + vi.Itoa64(int64(id)) + " on")
	dumpOrder()
	// M79g (#1718): Pin() re-partitions the strip (pinned left), so the
	// file has to be rewritten — an already-pinned tab returns false above
	// and costs nothing.
	noteSessionMutation()
	return true
}

// focusByIndex is the shared success path for Alt+Tab, Ctrl+Tab, and
// Ctrl+1..9. Tests replace it to pin dispatch without the guest-only
// slot-65 syscall.
var focusByIndex = func(i int) bool {
	if i < 0 || i >= tabs.Count() {
		return false
	}
	id := tabs.At(i).ID
	if !focusHosted(id) {
		return false
	}
	vi.ConsoleLine(MarkerAltTab + vi.Itoa64(int64(id)))
	vi.ConsoleLine(MarkerTabFocus + vi.Itoa64(int64(id)))
	vi.ConsoleLine(MarkerHostFocus + vi.Itoa64(int64(id)))
	dumpOrder()
	return true
}

func applyAltTab(shift bool) bool {
	i, ok := altTabNext(tabs.Count(), tabs.focus, shift)
	if !ok {
		return false
	}
	return focusByIndex(i)
}

func applyCtrlIndex(index int) bool {
	return index >= 1 && index <= tabs.Count() && focusByIndex(index-1)
}

func focusHosted(id uint32) bool {
	if id == 0 {
		return false
	}
	if vi.WmctlTaskbarClick(id) != 0 {
		return false
	}
	if !tabs.FocusTab(id) {
		return false
	}
	hostedApp = id
	return true
}

func handleWmPointer(e vi.Event) {
	px := e.Arg0 & 0xffff
	py := e.Arg0 >> 16
	btn := uint8(e.Flags & 0xff)
	down := pointerDownEdge(btn, prevPtrButtons)
	up := pointerUpEdge(btn, prevPtrButtons)
	prevPtrButtons = btn
	// M79k (#1720): the notify strip is chrome, so a press on a toast is
	// consumed HERE — it dismisses the toast and focuses its SENDER, and
	// it never reaches the content forward below (a stray content drag
	// inside a hosted pane would outlive the toast). The HIT decides
	// consumption, not whether the focus leg then succeeded: a press is
	// either on the toast or it is not.
	//
	// It comes FIRST, ahead of the launcher's own block below, because the
	// strip is painted LAST (compositeTick) and the pointer path has to
	// agree with the paint order: what is on top has to be what the click
	// finds. The launcher's block returns early for every button, so a
	// press on a toast that fired while the launcher was open used to be
	// swallowed there — it neither clicked the toast nor reached content,
	// it just closed the launcher under a toast the user could see. The
	// two rects are disjoint at 1280x720 anyway (the launcher panel starts
	// at launchX=240, the toast column ends at 216), so hoisting changes
	// no press outside a toast; TestToastHitPrecedesTheLauncher pins the
	// order rather than the coincidence.
	if down {
		if _, onToast := notifyHit(px, py, vi.ScanoutWidth, vi.ScanoutHeight); onToast {
			notifyClicked(px, py)
			return
		}
	}
	if launch.open {
		if down {
			i, ok := launchRowAt(px, py)
			if !ok {
				dismissLauncher()
				return
			}
			launch.sel = i
			execSelected()
		}
		return
	}
	if down {
		// M71e (#1564): on an empty strip the start surface is the click
		// target. Checked before the rail so it cannot be shadowed by a
		// rail cell that happens to span the point (the rail has no cells
		// when the strip is empty, so this is belt-and-braces).
		if tabs.Count() == 0 && startSurfaceHit(px, py) {
			openLauncher()
			return
		}
		// M79b (#1705): the close-x zone is its own hit target — it closes
		// the cell's tab and never focuses, never starts a drag, and never
		// reaches content (hit-test honesty). The rest of the cell keeps
		// the click/drag behaviour below, byte for byte.
		updateRailHover(px, py, false)
		if i, ok := railCloseZoneAt(px, py, vi.ScanoutWidth, tabs.Count(), RailHeight); ok {
			railDragFrom = -1
			applyRailClose(i)
			return
		}
		// M79c (#1706): the sash is chrome between the rail and the
		// content forward. A down on the divider arms the drag and
		// consumes the press: no rail drag, no click/focus change, no
		// content forward (a stray selection drag inside a pane would
		// otherwise shadow the resize). The rail (y < RailHeight) and
		// the divider zone are disjoint by construction (sashZoneAt),
		// so the order against beginRailDrag below cannot matter — but
		// the armed latch must be set before any of those run.
		if sashZoneAt(px, py, tabs.Split(),
			tabs.sashCenter(uint32(vi.ScanoutWidth), uint32(vi.ScanoutHeight)),
			vi.ScanoutWidth, vi.ScanoutHeight) {
			railDragFrom = -1
			sashDragging = true
			return
		}
		beginRailDrag(px, py)
		_, onRail := railCellAt(px, py, vi.ScanoutWidth, tabs.Count(), RailHeight)
		_ = applyRailClick(px, py)
		if !onRail {
			// #1688: not chrome — content for the kernel's local path
			// (terminal text selection, mouse-tracking reports). The
			// kernel derives press/release edges from this serialized
			// stream itself; consumed chrome is never forwarded.
			contentDown = true
			vi.WmctlContentPtr(px, py, btn)
		}
		return
	}
	if up {
		updateRailHover(px, py, false)
		// M79c (#1706): the sash release edge. The divider position is
		// the release point (an x for a vertical split, a y for a
		// horizontal one); applySashDrag clamps it into the pane minima
		// and refuses a release on the armed position. A sash press
		// never armed the rail drag and never set contentDown, so
		// neither fires here — return with the drag consumed.
		if sashDragging {
			sashDragging = false
			pos := int(px)
			if tabs.Split() == SplitHoriz {
				pos = int(py)
			}
			_ = applySashDrag(pos)
			return
		}
		_ = endRailDrag(px, py)
		if contentDown {
			vi.WmctlContentPtr(px, py, btn)
			contentDown = false
		}
		return
	}
	// #1688: motion is content too, unless it rides the rail chrome —
	// the only pointer consumer here besides the launcher above.
	// M79b (#1705): motion over the rail is hover — the tint follows the
	// pointer and the entry marker prints on cell transitions only.
	updateRailHover(px, py, true)
	// M79c (#1706): motion with a sash armed is an in-progress resize, not
	// content. Swallow it: forwarding held-button motion to the kernel's
	// local path would start a selection drag inside the pane under the
	// pointer. No live preview either (the seat paints no content-area
	// chrome); the rects move once, on the release edge above.
	if sashDragging {
		return
	}
	if _, onToast := notifyHit(px, py, vi.ScanoutWidth, vi.ScanoutHeight); onToast {
		// Chrome under the pointer, like the rail: not content. A drag
		// that starts on a toast is the toast's, not a content forward.
		return
	}
	if _, onRail := railCellAt(px, py, vi.ScanoutWidth, tabs.Count(), RailHeight); !onRail {
		vi.WmctlContentPtr(px, py, btn)
	}
}

func pointerDownEdge(btn, prev uint8) bool {
	return btn&hidBtnLeft != 0 && prev&hidBtnLeft == 0
}

func pointerUpEdge(btn, prev uint8) bool {
	return btn&hidBtnLeft == 0 && prev&hidBtnLeft != 0
}

// railCloseZoneAt is the M79b (#1705) close-x hit test: the cell's rightmost
// railCloseW px (tabs.go's railClose geometry), full strip height. It
// resolves the cell exactly like railCellAt and the width exactly like
// paintRail, so the hit zone can never disagree with the painted glyph.
// False everywhere the close-x is not a target.
func railCloseZoneAt(px, py uint32, width, n, stripH int) (int, bool) {
	i, ok := railCellAt(px, py, width, n, stripH)
	if !ok {
		return 0, false
	}
	cellW := width / n
	if cellW < 48 {
		cellW = 48
	}
	x := i * cellW
	w := cellW
	if x+w > width {
		w = width - x
	}
	// The zone is the PAINTED cell's rightmost railCloseW px. The upper
	// bound matters for railCellAt's remainder rule: pixels past n*cellW
	// resolve to the last cell but sit outside its paint, so they stay
	// click/drag territory and are never a close-x target.
	if w < railCloseW || int(px) < x+w-railCloseW || int(px) >= x+w {
		return 0, false
	}
	return i, true
}

// updateRailHover tracks M79b (#1705) hover state: railHover is the rail
// cell under the pointer (paintRail tints it), -1 anywhere else. The marker
// is motion-only and prints on ENTRY (a transition, markRail's discipline) —
// a press or release also rests the pointer on a cell, so state follows, but
// `rail-hover` names a hover, never a click.
func updateRailHover(px, py uint32, motion bool) {
	prev := railHover
	i, ok := railCellAt(px, py, vi.ScanoutWidth, tabs.Count(), RailHeight)
	if !ok {
		railHover = -1
		return
	}
	railHover = i
	if motion && i != prev {
		vi.ConsoleLine(MarkerRailHover + vi.Itoa64(int64(tabs.At(i).ID)))
	}
}

// applyRailClose is the M79b (#1705) close-x action: close the cell's tab by
// id through the same closeTabByID seam every other close uses, so the
// `host close id=` / `tab close id=` pair is exactly one shape. No focus
// change first — the click targets a button, not a cell.
func applyRailClose(i int) bool {
	if i < 0 || i >= tabs.Count() {
		return false
	}
	return closeTabByID(tabs.At(i).ID)
}

// railCellAt is the top-strip hit-test (M63c). Equal-width cells matching
// paintRail (`cellW = width/n`, min 48). py must be in the rail; the rail
// wins over any pane whose rect includes y=0. Close-x is not a target —
// the cell body is the click/drag target (railCloseZoneAt takes the zone).
func railCellAt(px, py uint32, width, n, stripH int) (int, bool) {
	if n <= 0 || width <= 0 || stripH <= 0 || py >= uint32(stripH) {
		return 0, false
	}
	cellW := width / n
	if cellW < 48 {
		cellW = 48
	}
	i := int(px) / cellW
	if i < 0 {
		return 0, false
	}
	if i >= n {
		i = n - 1
	}
	return i, true
}

func applyRailClick(px, py uint32) bool {
	n := tabs.Count()
	i, ok := railCellAt(px, py, vi.ScanoutWidth, n, RailHeight)
	if !ok {
		return false
	}
	id := tabs.At(i).ID
	if id == 0 {
		return false
	}
	if fid, focused := tabs.Focused(); focused && fid == id {
		return false
	}
	if !focusHosted(id) {
		return false
	}
	vi.ConsoleLine(MarkerRailClick + vi.Itoa64(int64(id)))
	vi.ConsoleLine(MarkerTabFocus + vi.Itoa64(int64(id)))
	vi.ConsoleLine(MarkerHostFocus + vi.Itoa64(int64(id)))
	dumpOrder()
	return true
}

func beginRailDrag(px, py uint32) {
	railDragFrom = -1
	i, ok := railCellAt(px, py, vi.ScanoutWidth, tabs.Count(), RailHeight)
	if ok {
		railDragFrom = i
	}
}

func endRailDrag(px, py uint32) bool {
	from := railDragFrom
	railDragFrom = -1
	if from < 0 {
		return false
	}
	to, ok := railCellAt(px, py, vi.ScanoutWidth, tabs.Count(), RailHeight)
	if !ok {
		return false
	}
	return applyRailReorder(from, to)
}

// applyRailReorder is Zig TABWM reorder_tab: Reorder() then the existing
// `gotabwm: reorder from->to` marker. Same-cell release is a click no-op.
// M79g (#1718): a drag that moved a tab persists SESSION.TABS right here,
// so the new order is what the next boot restores.
//
// M79c (#1706): a reorder while split re-proposes both pane rects. The
// rects are positional (index 0 = first pane), so without this the panes
// would silently swap which app is on the left — worse than the relayout.
// The dumps are the record; no extra marker.
func applyRailReorder(from, to int) bool {
	if !tabs.Reorder(from, to) {
		return false
	}
	vi.ConsoleLine(MarkerReorder + vi.Itoa64(int64(from)) + "->" + vi.Itoa64(int64(to)))
	dumpOrder()
	noteSessionMutation()
	if tabs.Split() == SplitNone || tabs.Count() != 2 {
		return true
	}
	ra, rb, ok := tabs.PaneRects(uint32(vi.ScanoutWidth), uint32(vi.ScanoutHeight))
	if !ok {
		return false
	}
	a, b := tabs.At(0), tabs.At(1)
	if !applyRect(a.ID, ra) || !applyRect(b.ID, rb) {
		return false
	}
	kind := tabs.Split()
	dumpTab(a, ra, kind)
	dumpTab(b, rb, kind)
	_ = writeLayoutFile()
	return true
}
