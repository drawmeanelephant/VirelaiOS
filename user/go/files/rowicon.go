// #3348 (#1257): the VT list rows' filetype icons — the last unwired
// surface of the staged family. The list is a truecolour text grid and
// stays one: there is no pixel canvas under a row and the kernel font has
// no half-block rune, so the honest seam is the cell path the grid mosaic
// already owns — each staged raster downsampled to the row's two-cell
// strip by the same rounded-mean rule (mosaicInto), transparent pixels
// taking the blank stand-in. Two cells on the 8x16 grid is the row's
// 16x16 square footprint — the picker's icon cell, at cell resolution.
//
// Resolution is name-only (mime.Sniff with no bytes — the picker's own
// mapping, appkit.FiletypeArt) and eager at refresh: no per-row file
// reads, and art.Load's cache bounds the share cost to once per family.
// An absent or refused asset leaves its row typographic; a listing where
// nothing resolved reserves no column at all.
package main

import (
	"strings"

	"virelai/appkit"
	"virelai/art"
	"virelai/mime"
	"virelai/vi"
	"virelai/webrender"
)

// listIconW is the strip's width in cells — two 8x16 cells make the
// 16x16 square footprint.
const listIconW = 2

// rowIcon is one resolved strip: the per-cell background colours.
type rowIcon [listIconW]uint32

// syncListIcons rebuilds the listing's per-row icon table — the flat
// list's gridSync shape: each row's art name is the picker's own
// FiletypeArt mapping (the folder card for dirs, the family by sniffed
// mime otherwise), resolved strips cached per art name for the run's
// life. The `list icon qoi n=` receipt counts the rows that will paint a
// strip — emitted per listing like the `list`/`entry` markers it rides,
// and silent when the share carries nothing this listing maps to.
func (m *model) syncListIcons() {
	m.rowIconRef = make([]*rowIcon, m.n)
	m.rowIconN = 0
	for i := 0; i < m.n; i++ {
		e := m.entries[i]
		name := e.NameString()
		an := appkit.FiletypeArt(e.Dir(), name, mime.Sniff(name, nil))
		if an == "" {
			continue
		}
		if t := m.rowIconTile(an); t != nil {
			m.rowIconRef[i] = t
			m.rowIconN++
		}
	}
	if m.rowIconN > 0 {
		m.emit(markerListIcons + vi.Itoa64(int64(m.rowIconN)))
	}
}

// rowIconTile resolves one staged family name into its strip — the
// gridIconTile shape at row height: an absent or refused asset answers
// nil and the caller keeps the text row (the same degrade every other
// art seam holds).
func (m *model) rowIconTile(name string) *rowIcon {
	if t := m.rowIcons[name]; t != nil {
		return t
	}
	src := art.Load(name)
	if src == nil || src.W < 1 || src.H < 1 || len(src.Pix) != src.W*src.H {
		return nil
	}
	t := &rowIcon{}
	mosaicInto(&webrender.Image{Width: src.W, Height: src.H, Pix: src.Pix},
		t[:], listIconW, 1)
	if m.rowIcons == nil {
		m.rowIcons = make(map[string]*rowIcon)
	}
	m.rowIcons[name] = t
	return t
}

// rowIconCells paints row idx's strip into b: each cell's background
// colour then a reset and the row's own style again, so the label that
// follows keeps its ink. A row with no resolved strip paints the blank
// column in the row's style — the listing's alignment never shifts
// per row.
func (m model) rowIconCells(b *strings.Builder, idx int, style string) {
	var t *rowIcon
	if idx < len(m.rowIconRef) {
		t = m.rowIconRef[idx]
	}
	if t == nil {
		b.WriteString(strings.Repeat(" ", listIconW))
		return
	}
	for x := 0; x < listIconW; x++ {
		sgrBG(b, t[x])
		b.WriteByte(' ')
	}
	b.WriteString(colReset)
	b.WriteString(style)
}
