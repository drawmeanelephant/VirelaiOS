// The compose-N handoff tests (issue #2236, defect #13): the kind-18 seam
// registers an owner's RO-mirrored surface, the tick's composeSurfaces
// blits it into the scanout at the mirrored rect, and the record dies with
// the window's binding — never blitting a dead window's pages.
package main

import (
	"errors"
	"testing"

	"virelai/vi"
)

// stubSurface replaces the attach/liveness seams and resets the handoff
// tables, the stubGpu3d pattern. attach answers the seat's peer-attach;
// alive answers the WmctlWindowName probe (r >= 0 = live).
func stubSurface(t *testing.T, attach func(uint32, int) ([]byte, error),
	alive func(uint32, []byte) (int, int64)) {
	t.Helper()
	prevA, prevN := surfaceAttach, surfaceName
	prevRecs, prevXY := surfaceRecs, surfaceXY
	prevRects, prevTabs := gpu3dRects, tabs
	surfaceAttach = attach
	surfaceName = alive
	// A non-nil buffer keeps surfaceAlive off the guest mmap so host tests
	// never touch a VA.
	surfaceNameBuf = []byte{0}
	surfaceRecs = [surfaceMaxRecs]surfaceRec{}
	surfaceXY = [256]uint32{}
	gpu3dRects = [256]uint32{}
	tabs = TabStrip{}
	t.Cleanup(func() {
		surfaceAttach, surfaceName = prevA, prevN
		surfaceRecs, surfaceXY = prevRecs, prevXY
		gpu3dRects, tabs = prevRects, prevTabs
		surfaceNameBuf = nil
	})
}

// shareReq builds one authenticated kind-18 request frame.
func shareReq(id, pid, seq uint8, handle, stride, rows uint32, token uint64) vi.WmRpc {
	req := vi.WmRpc{Kind: vi.WmRpcKindShareSurface, ID: id, Seq: seq,
		ReplyTo: pid, Pad: vi.WmRpcPadBound}
	putU32LE(req.Title[0:], handle)
	putU32LE(req.Title[4:], stride)
	putU32LE(req.Title[8:], rows)
	vi.SetWmAuth(&req, token)
	return req
}

func aliveYes(uint32, []byte) (int, int64) { return 1, 0 }
func aliveNo(uint32, []byte) (int, int64)  { return 0, -vi.ErrEINVAL }

// A bound owner's request registers the surface and the tick blits it into
// the scanout at the mirrored rect.
func TestSurfaceShareComposesAtMirrorRect(t *testing.T) {
	const w, h = 4, 3
	surf := make([]byte, w*h*4)
	for i := range surf {
		surf[i] = 0xAA
	}
	var gotHandle uint32
	var gotSize int
	stubSurface(t, func(handle uint32, size int) ([]byte, error) {
		gotHandle, gotSize = handle, size
		return surf, nil
	}, aliveYes)

	req := shareReq(5, 7, 1, 3, w, h, 0)
	if !serveSurfaceShare(req) {
		t.Fatal("share refused")
	}
	if gotHandle != 3 || gotSize != w*h*4 {
		t.Fatalf("attach = handle %d size %d want 3/%d", gotHandle, gotSize, w*h*4)
	}
	if !surfaceRecs[0].live || surfaceRecs[0].win != 5 {
		t.Fatalf("record = %+v", surfaceRecs[0])
	}
	// The mirror names the rect; the blit lands at (x,y).
	surfaceXY[5] = 10 | 20<<16
	gpu3dRects[5] = w | h<<16
	scan := make([]byte, vi.ScanoutWidth*vi.ScanoutHeight*4)
	composeSurfaces(scan)
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			at := ((20+row)*vi.ScanoutWidth + 10 + col) * 4
			if scan[at] != 0xAA {
				t.Fatalf("scan(%d,%d) = %#x want 0xAA", col, row, scan[at])
			}
		}
	}
}

// The refused shapes: empty geometry, an over-scanout extent, a failed
// attach, and a full record pool — none register.
func TestSurfaceShareRefusals(t *testing.T) {
	stubSurface(t, func(uint32, int) ([]byte, error) {
		return nil, nil
	}, aliveYes)
	if serveSurfaceShare(shareReq(5, 7, 1, 3, 0, 4, 0)) {
		t.Fatal("zero stride applied")
	}
	if serveSurfaceShare(shareReq(5, 7, 1, 3, 4, 0, 0)) {
		t.Fatal("zero rows applied")
	}
	if serveSurfaceShare(shareReq(5, 7, 1, 3, vi.ScanoutWidth+1, vi.ScanoutHeight+1, 0)) {
		t.Fatal("over-scanout extent applied")
	}
	surfaceAttach = func(uint32, int) ([]byte, error) { return nil, errors.New("attach") }
	if serveSurfaceShare(shareReq(5, 7, 1, 3, 4, 3, 0)) {
		t.Fatal("failed attach applied")
	}
	for i := range surfaceRecs {
		surfaceRecs[i].live = true
		surfaceRecs[i].win = uint32(100 + i)
	}
	surfaceAttach = func(uint32, int) ([]byte, error) { return make([]byte, 48), nil }
	if serveSurfaceShare(shareReq(5, 7, 1, 3, 4, 3, 0)) {
		t.Fatal("full pool applied")
	}
}

// A zero handle drops the window's handoff — the release path the app uses
// before its window dies.
func TestSurfaceShareZeroHandleDrops(t *testing.T) {
	stubSurface(t, func(uint32, int) ([]byte, error) {
		return make([]byte, 48), nil
	}, aliveYes)
	if !serveSurfaceShare(shareReq(5, 7, 1, 3, 4, 3, 0)) {
		t.Fatal("share refused")
	}
	if !serveSurfaceShare(shareReq(5, 7, 2, 0, 0, 0, 0)) {
		t.Fatal("drop refused")
	}
	for i := range surfaceRecs {
		if surfaceRecs[i].live {
			t.Fatalf("record survived the drop: %+v", surfaceRecs[i])
		}
	}
}

// The compose never touches a dead window's pages: the name probe answers
// EINVAL and the record is dropped instead of blitted.
func TestSurfaceComposeDropsDeadWindow(t *testing.T) {
	surf := make([]byte, 48)
	stubSurface(t, func(uint32, int) ([]byte, error) {
		return surf, nil
	}, aliveNo)
	if !serveSurfaceShare(shareReq(5, 7, 1, 3, 4, 3, 0)) {
		t.Fatal("share refused")
	}
	surfaceXY[5] = 10 | 20<<16
	gpu3dRects[5] = 4 | 3<<16
	scan := make([]byte, vi.ScanoutWidth*vi.ScanoutHeight*4)
	composeSurfaces(scan)
	if surfaceRecs[0].live {
		t.Fatal("dead window's record survived compose")
	}
	for _, b := range scan {
		if b != 0 {
			t.Fatal("dead window's pixels reached the scanout")
		}
	}
}

// The binding funnel owns the record's lifetime: dropRpcBinding (tab close,
// released mirror) retires the handoff exactly like the token and the
// gpu3d context.
func TestSurfaceDropRidesBinding(t *testing.T) {
	stubSurface(t, func(uint32, int) ([]byte, error) {
		return make([]byte, 48), nil
	}, aliveYes)
	if !serveSurfaceShare(shareReq(5, 7, 1, 3, 4, 3, 0)) {
		t.Fatal("share refused")
	}
	surfaceXY[5] = 10 | 20<<16
	dropRpcBinding(5)
	if surfaceRecs[0].live {
		t.Fatal("record survived its binding's death")
	}
	if surfaceXY[5] != 0 {
		t.Fatal("stale position survived the drop")
	}
}

// The mirror feeds the compose rect: x|y lands in arg0, the released bit
// clears it.
func TestSurfaceMirrorTracksPosition(t *testing.T) {
	stubSurface(t, func(uint32, int) ([]byte, error) { return nil, nil }, aliveYes)
	surfaceNoteMirror(vi.Event{Kind: vi.EvWmWindow, Flags: 5, Arg0: 30 | 40<<16})
	if surfaceXY[5] != 30|40<<16 {
		t.Fatalf("surfaceXY[5] = %#x", surfaceXY[5])
	}
	surfaceNoteMirror(vi.Event{Kind: vi.EvWmWindow, Flags: 5 | 1<<13, Arg0: 99 | 99<<16})
	if surfaceXY[5] != 0 {
		t.Fatal("released mirror kept the position")
	}
}

// A bound kind-18 frame reaches the seat through the full authenticated
// path: gateRPC admits it, serveSurfaceShare attaches, and the ack reports
// applied.
func TestSurfaceShareEndToEnd(t *testing.T) {
	stubSurface(t, func(uint32, int) ([]byte, error) {
		return make([]byte, 48), nil
	}, aliveYes)
	stubAuthSeams(t, func(uint8) (string, bool) { return "", false })
	const tok = 0x5afe1234
	rpcBindings[5] = rpcBinding{pid: 7, token: tok}
	t.Cleanup(func() { rpcBindings = [256]rpcBinding{} })
	acks := decodeAcks(t, serveWmRpcFrames(t, [][]byte{
		shareReq(5, 7, 1, 3, 4, 3, tok).Encode(),
	}))
	if len(acks) != 1 {
		t.Fatalf("acks = %d want 1", len(acks))
	}
	if acks[0].Applied != 1 || acks[0].Pad != vi.WmRpcPadBound {
		t.Fatalf("share ack = %+v want applied+Bound", acks[0])
	}
	if !surfaceRecs[0].live || surfaceRecs[0].win != 5 {
		t.Fatalf("record = %+v", surfaceRecs[0])
	}
}

// seedComposeRec plants one live handoff record directly: the compose-order
// tests need record slots in an order that disagrees with tab focus, which
// serveSurfaceShare's first-free-slot allocation cannot be asked to produce.
func seedComposeRec(slot int, win uint32, w, h uint32, fill byte) {
	pix := make([]byte, int(w)*int(h)*4)
	for i := range pix {
		pix[i] = fill
	}
	surfaceRecs[slot] = surfaceRec{live: true, win: win, w: w, h: h, pixels: pix}
	surfaceXY[win] = 0
	gpu3dRects[win] = w | h<<16
}

func scanByte(scan []byte, x, y int) byte {
	return scan[(y*vi.ScanoutWidth+x)*4]
}

// Two full-viewport surfaces overlap everywhere; the focused tab's pixels
// must win the scanout the way regionWindowAt gives it the pointer (#3940).
func TestSurfaceComposeFocusBlitsLast(t *testing.T) {
	stubSurface(t, func(uint32, int) ([]byte, error) { return nil, nil }, aliveYes)
	seedComposeRec(0, 5, vi.ScanoutWidth, vi.ScanoutHeight, 0x11)
	seedComposeRec(1, 6, vi.ScanoutWidth, vi.ScanoutHeight, 0x22)
	tabs.OpenTab(5, "Mine")
	tabs.OpenTab(6, "Sol") // first tab keeps focus; slot 1 still blits last today

	scan := make([]byte, vi.ScanoutWidth*vi.ScanoutHeight*4)
	composeSurfaces(scan)
	if got := scanByte(scan, vi.ScanoutWidth/2, vi.ScanoutHeight/2); got != 0x11 {
		t.Fatalf("focused-first scanout = %#x want 0x11", got)
	}

	tabs.FocusTab(6)
	for i := range scan {
		scan[i] = 0
	}
	composeSurfaces(scan)
	if got := scanByte(scan, vi.ScanoutWidth/2, vi.ScanoutHeight/2); got != 0x22 {
		t.Fatalf("focused-second scanout = %#x want 0x22", got)
	}
}

// With no focused tab the compose keeps today's array order: the later
// record still wins the overlap.
func TestSurfaceComposeNoFocusKeepsArrayOrder(t *testing.T) {
	stubSurface(t, func(uint32, int) ([]byte, error) { return nil, nil }, aliveYes)
	seedComposeRec(0, 5, vi.ScanoutWidth, vi.ScanoutHeight, 0x11)
	seedComposeRec(1, 6, vi.ScanoutWidth, vi.ScanoutHeight, 0x22)

	scan := make([]byte, vi.ScanoutWidth*vi.ScanoutHeight*4)
	composeSurfaces(scan)
	if got := scanByte(scan, vi.ScanoutWidth/2, vi.ScanoutHeight/2); got != 0x22 {
		t.Fatalf("unfocused scanout = %#x want 0x22", got)
	}
}

// Disjoint pane rects never overlap, so the focused-last pass changes
// nothing: both surfaces land where their mirrors put them.
func TestSurfaceComposeDisjointPanesUnaffected(t *testing.T) {
	stubSurface(t, func(uint32, int) ([]byte, error) { return nil, nil }, aliveYes)
	seedComposeRec(0, 5, 4, 4, 0x11)
	seedComposeRec(1, 6, 4, 4, 0x22)
	surfaceXY[5] = 0
	surfaceXY[6] = 8 | 8<<16
	tabs.OpenTab(5, "Mine")
	tabs.OpenTab(6, "Sol")
	tabs.FocusTab(6)

	scan := make([]byte, vi.ScanoutWidth*vi.ScanoutHeight*4)
	composeSurfaces(scan)
	if got := scanByte(scan, 1, 1); got != 0x11 {
		t.Fatalf("left pane = %#x want 0x11", got)
	}
	if got := scanByte(scan, 9, 9); got != 0x22 {
		t.Fatalf("right pane = %#x want 0x22", got)
	}
}
