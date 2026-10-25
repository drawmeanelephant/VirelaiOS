// act.go — the act operations' model half: the rename prompt, the M123d
// (#2578) error banner and its Replace/Cancel arms, the delete confirm
// and the trash restore, and the copy/cut/paste clipboard queue. ops.go
// owns the syscall seams these flows call; state.go owns the listing
// they mutate; the keys that arm them route through input.go.
package main

import (
	"strings"

	"virelai/appkit"
	"virelai/rss/keys"
	"virelai/vi"
	"virelai/vsys"
)

// startRename opens the prompt with an EMPTY buffer: the typeable name is
// the new name (the gate types `newname.txt` one chord at a time).
func (m *model) startRename() {
	if m.tar != nil {
		m.tarReadOnly("rename")
		return
	}
	if m.atPseudo() {
		return
	}
	if _, ok := m.selEntry(); !ok {
		m.status = "rename: nothing selected"
		return
	}
	m.mode = modeRename
	m.input = ""
	m.status = "rename"
}

// commitRename validates, runs the syscall, and on success emits the gate's
// `gofiles: renamed <from> -> <to>` marker before the re-list markers.
func (m *model) commitRename() {
	from, ok := m.selEntry()
	m.mode = modeNormal
	if !ok {
		m.status = "rename: nothing selected"
		return
	}
	fromName := from.NameString()
	to := m.input
	switch {
	case to == "":
		m.status = "rename cancelled"
		return
	case !validName(to):
		m.emit(markerRenameNo + fromName + " -> " + to)
		m.status = "rename: invalid name"
		return
	case to == fromName:
		m.status = "rename unchanged"
		return
	case containsName(m.entries[:], m.n, to):
		m.emit(markerRenameNo + fromName + " -> " + to + " exists")
		m.raiseRenameConflict(fromName, to)
		return
	}
	rc := renameEntry(m.path, fromName, to)
	if rc < 0 {
		m.emit(markerRenameNo + fromName + " -> " + to + " rc=" + vi.Itoa64(rc))
		m.status = refusalStatus("rename", rc)
		return
	}
	m.emit(markerRenamed + fromName + " -> " + to)
	m.status = "renamed " + to
	m.renamedBatch = true
	m.refresh()
	m.status = "renamed " + to // refresh's "listed" must not mask it
}

// --- M123d (#2578): the shared error banner ---------------------------------
//
// The banner is the status row while it is open: raiseBanner shows it and
// latches bannerFresh when the raise happened after the batch's paint
// (drainLaunch's exec outcome), so main knows a second paint owes the
// frame. Keys route through bannerKey — the appkit vocabulary on the tty
// keyspace (digits pick actions, c is Copy Error, Enter takes the focused
// safe-last button, Esc/Backspace dismiss) — and the resolution runs
// through bannerResolve.

// raiseBanner opens the banner over the status row and emits the raise
// marker. The marker rides pending like every other model marker, so the
// serial line still lands only after a frame that shows the strip.
func (m *model) raiseBanner(text string, actions ...appkit.BannerAction) {
	m.banner.Show(text, actions...)
	m.bannerFresh = true
	m.emit(markerBanner + text)
}

// raiseExecRefusal is drainLaunch's failure arm, kept on the model so the
// raise itself is host-pinnable: the exec outcome lands after the batch's
// own paint (bannerFresh asks main for the strip's frame), carries no
// contextual actions — Copy Error and dismiss are the honest answers.
func (m *model) raiseExecRefusal(bin string, err error) {
	m.bannerOp = ""
	m.raiseBanner("exec " + bin + " refused: " + err.Error())
	m.status = "open: " + bin + " refused"
}

// raiseRenameConflict arms the Replace/Cancel banner for a name-exists
// rename. The refused pair is remembered on the model — Replace cannot
// re-ask the prompt, it runs delete-the-incumbent + rename directly.
// Cancel is listed LAST: it is the focused default, so Enter never arms
// the destructive arm by accident.
func (m *model) raiseRenameConflict(from, to string) {
	m.bannerOp = "rename"
	m.bannerFrom, m.bannerTo = from, to
	m.status = "rename: name exists"
	m.raiseBanner("rename "+to+" exists",
		appkit.BannerAction{ID: "replace", Label: "Replace"},
		appkit.BannerAction{ID: "cancel", Label: "Cancel"})
}

// bannerKey feeds one key to the open banner. A resolved banner (Choice
// set, Open cleared) hands straight to bannerResolve so the outcome — the
// replace, the copy, or the dismissal — is applied in the same batch.
func (m *model) bannerKey(ev keys.Event) {
	switch ev.Key {
	case keys.KeyEsc, keys.KeyBackspace:
		m.banner.Dismiss()
	case keys.KeyEnter:
		m.banner.ActivateFocused()
	case keys.KeyTab:
		m.banner.Cycle(1)
	case keys.KeyRune:
		switch {
		case ev.Rune >= '1' && ev.Rune <= '9':
			m.banner.Activate(int(ev.Rune - '1'))
		case ev.Rune == 'c' || ev.Rune == 'C':
			m.banner.CopyChoice()
		}
	case keys.KeyCtrlC:
		m.quit = true
	}
	if !m.banner.Open {
		m.bannerResolve()
	}
}

// bannerResolve acts on the banner's Choice once it closes. `replace`
// runs the held rename's overwrite arm; `copy` publishes the error text
// on the machine clipboard (ClipText — the same slot-38 seam a file copy
// uses, different payload type); cancel/dismiss settles the prompt. Every
// path names itself on `gofiles: banner act …` — a banner that resolved
// silently would be a failure nobody saw answered.
func (m *model) bannerResolve() {
	choice := m.banner.Choice
	m.banner.Choice = ""
	op := m.bannerOp
	m.bannerOp = ""
	switch {
	case choice == appkit.BannerCopy:
		n, rc := vsys.ClipboardSetTyped(vsys.ClipText, []byte(m.banner.Text))
		if rc < 0 {
			m.emit(markerClipSys + "rc=" + vi.Itoa64(rc))
		} else {
			m.emit(markerClipSys + "n=" + vi.Itoa64(int64(n)))
		}
		m.emit(markerBannerAct + "copy")
		m.status = "error copied"
	case op == "rename" && choice == "replace":
		m.replaceRename()
	case op == "rename":
		m.emit(markerBannerAct + choice)
		m.status = "rename cancelled"
	default:
		m.emit(markerBannerAct + choice)
	}
}

// replaceRename is the conflict banner's Replace arm: the incumbent entry
// goes to TRASH (deleteEntry — lease-honest, and directories are refused
// at the model like startDelete's) and the held rename runs against the
// freed name. A refused delete leaves the rename untried; a `u` restore
// can still pull the incumbent back out of TRASH.
func (m *model) replaceRename() {
	from, to := m.bannerFrom, m.bannerTo
	m.bannerFrom, m.bannerTo = "", ""
	for i := 0; i < m.n; i++ {
		if m.entries[i].NameString() == to && m.entries[i].Dir() {
			m.emit(markerBannerAct + "replace refused dir")
			m.status = "replace: directories stay"
			return
		}
	}
	if _, rc := deleteEntry(m.path, to); rc < 0 {
		m.emit(markerBannerAct + "replace refused rc=" + vi.Itoa64(rc))
		m.status = refusalStatus("replace", rc)
		return
	}
	rc := renameEntry(m.path, from, to)
	if rc < 0 {
		m.emit(markerRenameNo + from + " -> " + to + " rc=" + vi.Itoa64(rc))
		m.status = refusalStatus("rename", rc)
		return
	}
	m.emit(markerRenamed + from + " -> " + to)
	m.emit(markerBannerAct + "replace " + to)
	m.status = "renamed " + to
	m.renamedBatch = true
	m.refresh()
	m.status = "renamed " + to
}

// startDelete arms the confirm step for a file — or, with marks present
// (M155b #2930), ONE confirm for the whole set: the prompt asks once and
// names the count, and 'y' queues the delete batch. Directories are
// refused honestly (ADR 0007 has no rmdir slot, so EASY delete would fail
// anyway) — the set cannot contain one because marks refuse them first.
func (m *model) startDelete() {
	if m.tar != nil {
		m.tarReadOnly("delete")
		return
	}
	if m.atPseudo() {
		return
	}
	if set := m.markedSet(); len(set) > 0 {
		m.mode = modeConfirmDelete
		m.delName = ""
		m.delSet = set
		m.emit(markerDeleteArm + "n=" + vi.Itoa64(int64(len(set))))
		m.status = "delete " + vi.Itoa64(int64(len(set))) + " marked? y/n"
		return
	}
	e, ok := m.selEntry()
	if !ok {
		m.status = "delete: nothing selected"
		return
	}
	if e.Dir() {
		m.emit(markerDeleteNo + e.NameString() + " (dir)")
		m.status = "delete: directories not supported"
		return
	}
	m.mode = modeConfirmDelete
	m.delName = e.NameString()
	m.emit(markerDeleteArm + m.delName)
	m.status = "delete " + m.delName + "? y/n"
}

// cancelDelete sheds the armed confirm without deleting: n/Esc, the
// first key that is not an answer, or a pointer press. The marker names
// the file the prompt held and what shed it — the arm/cancel pair is
// the serial receipt an armed-but-silent prompt never left (#2894).
func (m *model) cancelDelete(cause string) {
	if m.mode != modeConfirmDelete {
		return
	}
	m.mode = modeNormal
	if len(m.delSet) > 0 {
		m.emit(markerDeleteCancel + "n=" + vi.Itoa64(int64(len(m.delSet))) + " " + cause)
		m.delSet = nil
	} else {
		m.emit(markerDeleteCancel + m.delName + " " + cause)
	}
	m.delName = ""
	m.status = "delete cancelled"
}

func (m *model) doDelete() {
	if len(m.delSet) > 0 {
		// M155b (#2930): the 'y' answered the set's one confirm — queue
		// the delete batch, stepped like every other batch. A batch still
		// running is named rather than queued behind itself.
		items := m.delSet
		m.delSet, m.delName = nil, ""
		m.mode = modeNormal
		if m.batchOn {
			m.status = "delete: a batch is running"
			return
		}
		m.startBatch(batchDelete, items, m.path)
		return
	}
	name := m.delName
	m.delName = ""
	m.mode = modeNormal
	e, ok := m.selEntry()
	if !ok {
		m.status = "delete: nothing selected"
		return
	}
	if e.NameString() != name {
		// The listing shifted under the armed prompt — the 'y' answered
		// a different file's name, so the answer expired with it.
		m.emit(markerDeleteCancel + name + " key=stale")
		m.status = "delete cancelled"
		return
	}
	id, rc := deleteEntry(m.path, name)
	if rc < 0 {
		m.emit(markerDeleteNo + name + " rc=" + vi.Itoa64(rc))
		m.status = refusalStatus("delete", rc)
		return
	}
	m.emit(markerDeleted + name + " trash=" + id)
	m.status = "deleted " + name
	m.refresh()
	m.status = "deleted " + name
}

// restoreLatest restores the newest item in the shared trash. The vi helper
// refuses an occupied original path, so restore never overwrites user data.
func (m *model) restoreLatest() {
	path, id, rc := vi.TrashRestoreLatest()
	if rc < 0 {
		m.emit(markerRestoreNo + "rc=" + vi.Itoa64(rc))
		m.status = "restore refused"
		return
	}
	name := baseName(path)
	m.status = "restored " + name
	if parentPath(path) == m.path {
		m.refresh()
		m.status = "restored " + name
	}
	// Emit after refresh so a gate waiting on this marker also observes the
	// restored item in the immediately preceding listing rows.
	m.emit(markerRestored + name + " trash=" + id)
}

// yankClip holds the selected file — or the whole marked set (M155b
// #2930) — for a later paste (c = copy, x = move). The set is queued in
// listing order so the batch's progress sequence reads the way the list
// did. A copy also publishes the sources' URIs on the machine clipboard
// (one file:// line each); a cut publishes nothing.
func (m *model) yankClip(cut bool) {
	if m.tar != nil {
		op := "copy"
		if cut {
			op = "cut"
		}
		m.tarReadOnly(op)
		return
	}
	if m.atPseudo() {
		return
	}
	verb := "copy"
	if cut {
		verb = "cut"
	}
	if names := m.markedSet(); len(names) > 0 {
		var set []string
		for _, name := range names {
			src, ok := joinPath(m.path, name)
			if !ok {
				m.emit(markerClip + verb + " refused " + name + " path")
				m.status = "clip: path too long"
				return
			}
			set = append(set, src)
		}
		m.clipSet = set
		m.clipCut = cut
		m.emit(markerClip + verb + " set n=" + vi.Itoa64(int64(len(set))))
		m.status = verb + " " + vi.Itoa64(int64(len(set))) + " marked"
		if !cut {
			m.publishClipURIs(set)
		}
		return
	}
	e, ok := m.selEntry()
	if !ok {
		m.status = "clip: nothing selected"
		return
	}
	if e.Dir() {
		m.emit(markerPasteNo + e.NameString() + " (dir)")
		m.status = "clip: files only"
		return
	}
	src, ok := joinPath(m.path, e.NameString())
	if !ok {
		m.status = "clip: path too long"
		return
	}
	m.clipSet = []string{src}
	m.clipCut = cut
	m.emit(markerClip + verb + " " + e.NameString())
	m.status = verb + " " + e.NameString()
	if !cut {
		// M124c (#2582): a copy also publishes the file's URI on the
		// machine clipboard as text/uri-list — "copy file → paste path"
		// — while the in-app clip keeps its own paste semantics. A cut
		// publishes nothing: the URI would name a path about to go
		// stale. EACCES under the seat lands on the marker like every
		// other refused op; the local clip already succeeded.
		m.publishClipURIs([]string{src})
	}
}

// clipSysRead is the machine-clipboard read seam — vsys.ClipboardGetTyped
// in the guest, swapped by the host tests (the goedit clipRead precedent).
var clipSysRead = func() (string, []byte, int64) {
	var buf [vsys.ClipKernelMax]byte
	mime, n, r := vsys.ClipboardGetTyped(buf[:])
	return mime, buf[:n], r
}

// adoptSysClip is `p` with an empty in-app queue (M202d #3443): the machine
// clipboard's typed read classifies the body and the CLASS rules what paste
// means — a text/uri-list becomes a copy set (publishClipURIs' own spelling
// read back, so a copy in one window pastes in another), an image payload
// lands as a new PASTE.<ext> file in the listing, and every other class is
// refused BY NAME. The in-app queue keeps precedence while it holds a
// staged copy/cut — a machine-clipboard body does not redirect `p` away
// from a set the user already armed.
func (m *model) adoptSysClip() {
	if m.atPseudo() {
		return
	}
	mime, body, rc := clipSysRead()
	if rc < 0 {
		m.emit(markerPasteNo + "rc=" + vi.Itoa64(rc))
		m.status = refusalStatus("paste", rc)
		return
	}
	if len(body) == 0 {
		m.emit(markerPasteNo + "empty clip")
		m.status = "paste: nothing held"
		return
	}
	switch {
	case mime == vsys.ClipURIList:
		m.adoptURIList(body)
	case clipImageExt(mime) != "":
		m.adoptBlob(mime, body)
	default:
		m.emit(markerPasteNo + mime)
		m.status = "paste: " + mime + " is not a file"
	}
}

// adoptURIList turns a text/uri-list body into the queue the single/batch
// paste paths already run: file:// lines become absolute source paths with
// COPY semantics — a published list cannot express a move. A line that is
// not a file URI refuses the whole adopt by name: a partial import would
// pretend the list said less than it did.
func (m *model) adoptURIList(body []byte) {
	var set []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		p := strings.TrimPrefix(line, "file://")
		if p == line || p == "" || p[0] != '/' {
			m.emit(markerPasteNo + line + " not a file URI")
			m.status = "paste: not a file URI"
			return
		}
		set = append(set, p)
	}
	if len(set) == 0 {
		m.emit(markerPasteNo + "empty uri-list")
		m.status = "paste: nothing held"
		return
	}
	m.emit(markerClipSys + "adopt text/uri-list n=" + vi.Itoa64(int64(len(set))))
	m.clipSet, m.clipCut = set, false
	m.pasteClip()
}

// writePasteFile is the blob-landing write seam — vi.WriteFileSafe in the
// guest, stood up by the host tests (the clipSysRead precedent) so the
// PASTE.<ext> naming pins without a share.
var writePasteFile = vi.WriteFileSafe

// clipBlobName derives the landing name for a pasted payload: PASTE.<ext>,
// then PASTE1..9.<ext> when an earlier paste already holds the slot — a
// paste never overwrites, so "" is the no-free-name refusal.
func clipBlobName(entries []vi.DirEntry, n int, ext string) string {
	for i := 0; i < 10; i++ {
		name := "PASTE"
		if i > 0 {
			name += vi.Itoa64(int64(i))
		}
		name += "." + ext
		if !containsName(entries, n, name) {
			return name
		}
	}
	return ""
}

// adoptBlob writes an image payload as a new file in the listing under the
// derived PASTE.<ext> name. The write is the copy path's own safe-write
// plus lease check, and the receipt is the same pasted marker.
func (m *model) adoptBlob(mime string, body []byte) {
	ext := clipImageExt(mime)
	name := clipBlobName(m.entries[:], m.n, ext)
	if name == "" {
		m.emit(markerPasteNo + "PASTE." + ext + " no free name")
		m.status = "paste: no free name"
		return
	}
	dst, ok := joinPath(m.path, name)
	if !ok {
		m.emit(markerPasteNo + name + " path")
		m.status = "paste: path too long"
		return
	}
	if leaseBlocks(dst) {
		m.emit(markerPasteNo + name + " rc=" + vi.Itoa64(-vi.ErrEAGAIN))
		m.status = refusalStatus("paste", -vi.ErrEAGAIN)
		return
	}
	if rc := writePasteFile(dst, body); rc < 0 {
		m.emit(markerPasteNo + name + " rc=" + vi.Itoa64(rc))
		m.status = refusalStatus("paste", rc)
		return
	}
	m.emit(markerClipSys + "adopt " + mime)
	m.emit(markerPasted + name)
	m.status = "pasted " + name
	m.notifyToast("pasted " + name)
	m.refresh()
	m.status = "pasted " + name
}

// clipImageExt maps a classified image mime to the file extension the
// pasted blob lands under; "" names a class adoptBlob does not take.
func clipImageExt(mime string) string {
	switch mime {
	case vsys.ClipPNG:
		return "PNG"
	case vsys.ClipQOI:
		return "QOI"
	case vsys.ClipGIF:
		return "GIF"
	case vsys.ClipJPEG:
		return "JPG"
	case vsys.ClipBMP:
		return "BMP"
	}
	return ""
}

// pasteClip materializes the held file(s) in the current directory: copy
// reads the bytes and safe-writes them, move renames. An existing target
// is refused — a paste never overwrites. A queued SET (M155b #2930)
// hands off to the batch planner, which steps the items one per frame.
func (m *model) pasteClip() {
	if m.tar != nil {
		m.tarReadOnly("paste")
		return
	}
	if len(m.clipSet) == 0 {
		m.adoptSysClip()
		return
	}
	if m.atPseudo() {
		return
	}
	if len(m.clipSet) > 1 {
		m.startPasteBatch()
		return
	}
	src := m.clipSet[0]
	name := baseName(src)
	if m.clipCut && volumeOf(src) != volumeOf(m.path) {
		// A rename is same-volume only by ABI (ADR 0050 D6: pin_rename
		// refuses a cross-partition pair), so a held move cannot land off
		// its own volume — copy (c) crosses volumes; move (x) does not.
		m.emit(markerPasteNo + name + " cross-volume")
		m.status = "paste: a move stays on one volume"
		return
	}
	if containsName(m.entries[:], m.n, name) {
		m.emit(markerPasteNo + name + " exists")
		m.status = "paste: name exists"
		return
	}
	if m.clipCut {
		rc := pasteMove(m.path, src)
		if rc < 0 {
			m.emit(markerPasteNo + name + " rc=" + vi.Itoa64(rc))
			m.status = refusalStatus("paste", rc)
			return
		}
		m.emit(markerPasted + name)
		m.status = "moved " + name
		m.notifyToast("moved " + name)
		m.clipSet = nil
		m.refresh()
		m.status = "moved " + name
		return
	}
	data, rc := readCapped(src, maxPasteBytes)
	if rc < 0 {
		m.emit(markerPasteNo + name + " rc=" + vi.Itoa64(rc))
		m.status = "paste refused"
		return
	}
	if len(data) >= maxPasteBytes {
		m.emit(markerPasteNo + name + " at read cap")
		m.status = "paste: file too large"
		return
	}
	rc = pasteCopy(m.path, src, data)
	if rc < 0 {
		m.emit(markerPasteNo + name + " rc=" + vi.Itoa64(rc))
		m.status = refusalStatus("paste", rc)
		return
	}
	m.emit(markerPasted + name)
	m.status = "copied " + name
	m.notifyToast("copied " + name)
	m.refresh()
	m.status = "copied " + name
}
