// GOTABWM.ELF — #3348's region declare (WM_RPC kind 31): the
// app-declared-context half of the cursor seam. An app names window-local
// rects and the CursorShape each wants (vi.WmRpcKindRegionDeclare); the
// seat stores the bound slots on the tab, and pickCursorShape consults
// them whenever a pointer sample lands inside the window's live rect and
// no seat gesture or chrome owns the point — the seam txt/lnk/xhr/bsy
// were staged for and the seat alone could never see.
package main

import "virelai/vi"

// regionSlotsMax mirrors vi.RegionSlotMax — the wire's slot bound IS the
// per-tab table's. Fixed array on the Tab value (the navHist discipline:
// a reorder, a pin, or a space move carries the declaration for free,
// and a close frees it with the row).
const regionSlotsMax = 8

// appRegion is one declared zone: a window-local rect and the staged
// CursorShape index the seat picks while the pointer is inside it. A
// zero w or h is the empty slot — the wire's clear form lands the same
// shape a never-declared slot holds.
type appRegion struct {
	shape      uint8
	x, y, w, h uint16
}

// MarkerRegionDeclare is the region-declare receipt — printed when a
// bound app's frame lands a slot, so the gate sees the declaration apply
// before the cursor pick it feeds.
const MarkerRegionDeclare = "gotabwm: region id="

// serveRegionDeclare applies one kind-31 request: the request id is the
// bound window (gateRPC already proved ownership), the title the binary
// slot/shape/rect record. A window not on the strip or a malformed
// record — a slot or shape past the wire bound, a half-empty rect — is
// refused (applied=0) rather than installed.
func serveRegionDeclare(req vi.WmRpc) bool {
	id := uint32(req.ID)
	i := tabs.index(id)
	if i < 0 {
		return false
	}
	slot, shape, x, y, w, h, ok := vi.DecodeRegionTitle(req.Title)
	if !ok || int(slot) >= regionSlotsMax {
		return false
	}
	tabs.tabs[i].regions[slot] = appRegion{shape: shape, x: x, y: y, w: w, h: h}
	vi.ConsoleLine(MarkerRegionDeclare + vi.Itoa64(int64(id)) +
		" slot=" + vi.Itoa64(int64(slot)) +
		" shape=" + cursorShapeNames[shape])
	return true
}

// regionShapeAt is the app-declared half of the pick: the window under
// the sample names the zones; the seat translates by the window's live
// origin and the lowest set slot containing the point wins. A modal seat
// surface or a chrome overlay covering the point keeps the arrow rather
// than picking a region it occludes.
func regionShapeAt(px, py uint32) (uint64, bool) {
	if seatChromeModal() || notifyCenterOpen {
		return 0, false
	}
	if godMenuHit(px, py, vi.ScanoutWidth, vi.ScanoutHeight) {
		return 0, false
	}
	if _, onToast := notifyHit(px, py, vi.ScanoutWidth, vi.ScanoutHeight); onToast {
		return 0, false
	}
	id, lx, ly, ok := regionWindowAt(px, py)
	if !ok {
		return 0, false
	}
	i := tabs.index(id)
	if i < 0 {
		return 0, false
	}
	for s := range tabs.tabs[i].regions {
		r := &tabs.tabs[i].regions[s]
		if r.w == 0 || r.h == 0 {
			continue
		}
		if lx >= uint32(r.x) && lx < uint32(r.x)+uint32(r.w) &&
			ly >= uint32(r.y) && ly < uint32(r.y)+uint32(r.h) {
			return uint64(r.shape), true
		}
	}
	return 0, false
}

// regionWindowAt resolves the hosted window under the point and the
// point's window-local coords — the kind-20 mirror pair (surfaceXY /
// gpu3dRects), the same live-rect source dragTargetAt reads, so splits,
// zoom and the unsplit viewport all come through the one path. The rail
// band and the bottom chrome strip are seat paint, never content; the
// focused tab wins an overlap — abutting pane edges land where the user
// is looking.
func regionWindowAt(px, py uint32) (id, lx, ly uint32, ok bool) {
	if py < uint32(RailHeight) || int(py)+22 > int(vi.ScanoutHeight) ||
		px >= uint32(vi.ScanoutWidth) {
		return 0, 0, 0, false
	}
	fid, hasFocus := tabs.Focused()
	var hit, hx, hy uint32
	found := false
	for i := 0; i < tabs.Count(); i++ {
		wid := tabs.At(i).ID
		if wid >= sessionIDBase {
			continue // a restored placeholder has no window
		}
		xy, wh := surfaceXY[wid], gpu3dRects[wid]
		w, h := wh&0xffff, wh>>16
		if w == 0 || h == 0 {
			continue
		}
		x, y := xy&0xffff, xy>>16
		if px < x || px >= x+w || py < y || py >= y+h {
			continue
		}
		if hasFocus && wid == fid {
			return wid, px - x, py - y, true
		}
		hit, hx, hy, found = wid, x, y, true
	}
	if !found {
		return 0, 0, 0, false
	}
	return hit, px - hx, py - hy, true
}
