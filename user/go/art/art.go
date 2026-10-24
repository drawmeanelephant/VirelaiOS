// Package art decodes the M203 (#3338) staged interface rasters — the
// f-artcraft drop tools/session.sh and gate-run.sh copy from image/art/
// to /host/ART/<NAME>.QOI (8.3 share names; image/art/ART.TXT is the
// slug/provenance manifest).
//
// Every consumer honours the same degrade contract the launcher emblem
// (#2054) set: a share that does not carry the asset, or a decode that
// refuses, returns nil and the caller keeps its typographic/glyph form —
// art is an upgrade on a working surface, never a dependency.
package art

import (
	"virelai/draw"
	"virelai/vi"
	"virelai/webrender/qoi"
	"virelai/widgets"
)

const (
	// dir/ext name the staged share layout; names are the 8.3 bases in
	// image/art/ART.TXT.
	dir = "/host/ART/"
	ext = ".QOI"

	// The decode budgets bound the whole fleet: BRD_PLT.QOI (800x450) is
	// the largest staged piece by far.
	maxBytes = 1 << 20
	maxDim   = 1024
	maxPix   = 1024 * 1024
)

// Staged base names (image/art/ART.TXT), exported so call sites never
// spell the 8.3 strings by hand.
const (
	// #1249 cursor masters (kernel mask card consumes MASKS.TXT).
	CursorPtr = "CURS_PTR"
	// #1250 sexiburger poses.
	MascotWave      = "MSC_WAVE"
	MascotSleep     = "MSC_SLEP"
	MascotThink     = "MSC_THNK"
	MascotCelebrate = "MSC_CELB"
	MascotOops      = "MSC_OOPS"
	MascotLocked    = "MSC_LOCK"
	// #1253 empty-state spots.
	SpotEmptyFolder   = "SPT_FOLD"
	SpotNoResults     = "SPT_NRES"
	SpotOffline       = "SPT_OFFL"
	SpotError         = "SPT_ERR"
	SpotNotifications = "SPT_BELL"
	SpotDocument      = "SPT_DOC"
	SpotNoImage       = "SPT_IMG"
	SpotTrash         = "SPT_TRSH"
	// #1254 brand.
	BrandBootPlate   = "BRD_PLT"
	BrandAboutEmblem = "BRD_EMB"
	// #1255 spinner sheets (uniform 1-row frame grid, frame = H x H).
	SpinnerArc    = "SPN_ARC"
	SpinnerMascot = "SPN_MASC"
	// #1257 filetype family.
	FiletypeFolder     = "FT_FOLD"
	FiletypeFolderOpen = "FT_FOPE"
	FiletypeText       = "FT_TEXT"
	FiletypeImage      = "FT_IMG"
	FiletypeAudio      = "FT_AUD"
	FiletypeDocument   = "FT_DOC"
	FiletypeArchive    = "FT_ARC"
	FiletypeBinary     = "FT_BIN"
	FiletypeContact    = "FT_VCF"
	FiletypeCalendar   = "FT_ICS"
)

// readFile is the share-read seam — vi.ReadFileAll in the guest, a stub in
// host tests (the readAppsFile/openFile pattern every seam keeps).
var readFile = vi.ReadFileAll

// cache holds the decoded raster per name; tried[n] latches both outcomes
// so a missing asset costs one share read, not one per paint.
var (
	cache = map[string]*draw.BufferCanvas{}
	tried = map[string]bool{}
)

// Load returns the decoded raster for a staged base name, or nil. The
// decode happens once, lazily, on the first caller.
func Load(name string) *draw.BufferCanvas {
	if tried[name] {
		return cache[name]
	}
	tried[name] = true
	body, r := readFile(dir+name+ext, maxBytes)
	if r >= 0 && body != nil {
		w, h, pix, err := qoi.Decode(body, maxDim, maxPix)
		if err == nil {
			cache[name] = &draw.BufferCanvas{Pix: pix, W: w, H: h}
		}
	}
	return cache[name]
}

// Blit paints the named raster scaled into r. It is a no-op on any canvas
// that is not a real pixel buffer and when the asset is absent — the
// degrade path keeps its byte-identical FillRect stream.
func Blit(c widgets.RectCanvas, name string, r widgets.Rect) {
	bc, ok := c.(*draw.BufferCanvas)
	if !ok {
		return
	}
	src := Load(name)
	if src == nil {
		return
	}
	draw.BlitScaled(bc, src, r)
}

// Spot returns the EmptyState.Spot callback for a staged name, honouring
// the widget's probe-or-paint contract: an empty band reports whether the
// raster decoded; a real band gets it painted centred, aspect preserved.
// Bands too small for a readable spot (< 24 px tall) report false — the
// Icon row the widget falls back to is the small-space answer.
func Spot(name string) func(c widgets.RectCanvas, r widgets.Rect) bool {
	return func(c widgets.RectCanvas, r widgets.Rect) bool {
		src := Load(name)
		bc, ok := c.(*draw.BufferCanvas)
		if !ok || src == nil {
			return false
		}
		if r.Empty() {
			return true // the probe: pixels exist for a real band
		}
		if r.H < 24 {
			return false
		}
		w := r.H * src.W / src.H
		if w > r.W {
			w = r.W
		}
		h := w * src.H / src.W
		dst := widgets.Rect{
			X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: w, H: h,
		}
		draw.BlitScaled(bc, src, dst)
		return true
	}
}

// Reset drops the decode cache — host tests only.
func Reset() {
	cache = map[string]*draw.BufferCanvas{}
	tried = map[string]bool{}
}
