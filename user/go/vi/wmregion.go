// M203 (#3348): the app-declared context-region half of the cursor seam.
//
// The seat owns the cursor pick (wmctl cmd 24 selects which staged mask
// the kernel stamps), but the text/link/busy shapes have no seat-visible
// context — only the APP knows which of its pixels are editable, a link,
// or a working zone. Kind 31 is the declaration: the client names one
// window-local rectangle and the CursorShape it wants over it, and the
// seat's pick consults the bound window's declared slots whenever a
// pointer sample lands inside its pane and no seat gesture or chrome
// owns the point.
//
// The wire is the frozen 38-byte frame, so the record rides the 24-byte
// title as BINARY — the kind-17/18/22 raw-title precedent, not a
// SetTitle string (the frame's x/y/w/h fields are the auth union under
// the Go seat):
//
//	title[0]   slot:  the window's region index, 0..RegionSlotMax-1. A
//	                  re-declare on the same slot replaces the region; a
//	                  frame with w=h=0 clears it, so an app can drop a
//	                  zone without renumbering the rest. Overlapping
//	                  zones resolve LOWEST SLOT FIRST — declare a narrow
//	                  zone in a lower slot than the broad zone covering
//	                  it (a link inside an editable band).
//	title[1]   shape: the CursorShape index (0..9) — the staged
//	                  MASKS.TXT order (ptr/txt/lnk/grb/grg/rsx/rsy/xhr/
//	                  bsy/no).
//	title[2:]  x,y,w,h: u16 little-endian, window-local px in the app's
//	                  own canvas coordinates. The seat translates by the
//	                  window's pane rect before the hit-test, so the app
//	                  re-declares on WIN_RESIZE — the seat stores the
//	                  rect, it does not rescale it.
//	title[10:] reserved zero.
//
// The request id is the bound window, like every client -> seat kind —
// the gateRPC bind is what lets the seat trust the declaration. Regions
// are a Go-seat surface, so the request refuses before the mailbox when
// no Go seat is registered — a Zig seat would only refuse the unknown
// kind anyway.
package vi

const (
	// WmRpcKindRegionDeclare publishes one window-local cursor region to
	// the seat (31). Additive — the frozen kinds are not renumbered.
	WmRpcKindRegionDeclare uint8 = 31
	// RegionSlotMax bounds one window's declared regions — the seat's
	// per-tab table is fixed, so this IS the wire's slot bound.
	RegionSlotMax = 8
)

// EncodeRegionTitle packs one declaration into the kind-31 title field.
// ok=false for a slot or shape past the wire bound, or a half-empty rect
// (w and h are either both zero — the clear form — or both nonzero): a
// frame the seat would refuse is refused before the mailbox.
func EncodeRegionTitle(slot uint8, shape uint64, x, y, w, h uint16) ([WmRpcTitleMax]byte, bool) {
	if slot >= RegionSlotMax || shape > 9 || (w == 0) != (h == 0) {
		return [WmRpcTitleMax]byte{}, false
	}
	var t [WmRpcTitleMax]byte
	t[0] = slot
	t[1] = uint8(shape)
	putU16(t[2:], x)
	putU16(t[4:], y)
	putU16(t[6:], w)
	putU16(t[8:], h)
	return t, true
}

// DecodeRegionTitle is the seat-side inverse: slot, shape and the
// window-local rect, or ok=false for a malformed frame — a slot or shape
// past the wire bound, or a half-empty rect — so the seat refuses rather
// than installs a plausible-looking region.
func DecodeRegionTitle(t [WmRpcTitleMax]byte) (slot, shape uint8, x, y, w, h uint16, ok bool) {
	slot, shape = t[0], t[1]
	x, y, w, h = getU16(t[2:]), getU16(t[4:]), getU16(t[6:]), getU16(t[8:])
	if slot >= RegionSlotMax || shape > 9 || (w == 0) != (h == 0) {
		return 0, 0, 0, 0, 0, 0, false
	}
	return slot, shape, x, y, w, h, true
}

// DeclareRegion publishes (or clears, w=h=0) one window-local cursor
// region on winID (kind 31). shape is a Cursor* index — txt over an
// editable field, lnk over a link, bsy while the zone is working, xhr
// over a precision canvas. The rect is in the app's own canvas
// coordinates; the app re-declares on WIN_RESIZE because the seat holds
// the rect verbatim. title is the window's intended kernel title — the
// bind handshake's restore — as with every wmRequestGoTitle caller.
// False on a malformed record, no Go seat, or a refused ack; the caller
// degrades to the arrow rather than assume a pick landed.
func DeclareRegion(winID uint32, slot uint8, shape uint64, x, y, w, h uint16, title, selfName string) bool {
	t, ok := EncodeRegionTitle(slot, shape, x, y, w, h)
	if !ok || !fitsWire8(winID) {
		return false
	}
	peers := WmPeers(selfName)
	if peers.WM == 0 || peers.Self == 0 || !fitsWire8(peers.Self) || !peers.Go {
		return false
	}
	rep, ok := wmRequestGoTitle(WmRpcKindRegionDeclare, uint8(winID), t, title, peers)
	return ok && rep.Applied != 0
}
