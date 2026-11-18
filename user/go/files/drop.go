// #3915: the DropTarget doors. The seat admits a staged drag against this
// app's opens= row and hands the payload over by MIME; this file picks the
// door and keeps the repaint latch. text/vcard keeps its contact face (M193c
// #3255); archive validates, saves without overwrite and browses (#3915).
// The parsing, naming and publication helpers live in carry.go.
package main

import (
	"virelai/tabapp"
	"virelai/vi"
)

// dropFresh latches "a drop landed this pass" — the loop repaints and flushes
// once after PollDrag rather than per emitted row.
var dropFresh bool

// dropContact is the text/vcard door: the seat's opens= admission already
// filtered the mime, so the payload paints where it lands. The face's markers
// ride pending so they flush behind the repaint dropFresh earns.
func (m *model) dropContact(offer tabapp.DragOffer) {
	if offer.Blob == nil {
		m.emit(markerContact + "refused no-payload")
		m.status = "drop: no payload"
		return
	}
	m.openContactBlob(offer.Blob)
}

// dropOffer is the DropTarget hook: the payload's own admitted family picks
// the door. Anything else refuses by name rather than guessing.
func (m *model) dropOffer(offer tabapp.DragOffer) {
	dropFresh = true
	switch offer.Mime {
	case "text/vcard":
		m.dropContact(offer)
	case "archive":
		m.dropArchive(offer)
	default:
		m.emit(markerDropNo + "mime")
		m.status = "drop: unsupported mime"
	}
}

// dropArchive validates the offered tar and publishes it in the current real
// directory without replacing an existing file, then refreshes the listing
// and opens the saved archive with the existing reader. Archive and
// pseudo-listings are not writable destinations.
func (m *model) dropArchive(offer tabapp.DragOffer) {
	refuse := func(why string) {
		m.emit(markerDropNo + why)
		m.status = "drop: refused " + why
	}
	switch {
	case m.tar != nil:
		refuse("tar")
		return
	case m.path == volsPath:
		refuse("volume")
		return
	case m.path == favsView:
		refuse("favs")
		return
	case volumeOf(m.path) != volHost:
		refuse("dest")
		return
	}
	if offer.Blob == nil {
		refuse("no-payload")
		return
	}
	root, entries, why := inspectCarry(offer.Blob)
	if why != "" {
		refuse(why)
		return
	}
	canon := canonSharePath(m.path)
	if canon == "" {
		refuse("dest")
		return
	}
	list, err := carryListDir(canon)
	if err != nil {
		refuse("dest")
		return
	}
	name, why := archiveName(root, m.path, list)
	if why != "" {
		refuse(why)
		return
	}
	dest, ok := joinPath(m.path, name)
	if !ok {
		refuse("path")
		return
	}
	// Publication is an independently owned stage plus the kernel's atomic
	// preserve-existing rename. A writer that lands the probed name in
	// between wins; the rename's EEXIST refuses this drop and only our own
	// stage is removed.
	switch rc := carryPublish(dest, offer.Blob); {
	case rc == vi.ErrFileExists:
		refuse("collision")
		return
	case rc == -vi.ErrENAMETOOLONG:
		refuse("path")
		return
	case rc < 0:
		refuse("write rc=" + vi.Itoa64(rc))
		return
	}
	m.emit(markerDropSaved + name + " bytes=" + vi.Itoa64(int64(len(offer.Blob))) +
		" entries=" + vi.Itoa64(int64(entries)) + " root=" + root)
	m.status = "drop: saved " + name
	m.refresh()
	var e vi.DirEntry
	copy(e.Name[:], name)
	e.Size = uint32(len(offer.Blob))
	m.enterTar(e)
}
