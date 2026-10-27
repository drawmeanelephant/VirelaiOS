# VirelaiOS cursor hotspots — issue #1249

Intended hotspot for each `cursor-*` piece, in **master pixels** (32×48
canvas, y down, origin top-left) and **ship pixels** (16×24 — halve the
master value; the kernel's mask table wants ship coordinates). Fractional
ship values round to the nearer pixel center.

| piece | anchor | master (x,y) | ship 16×24 (x,y) |
|---|---|---|---|
| `cursor-arrow` | tip of the point | 2, 2 | 1, 1 |
| `cursor-ibeam` | bar midpoint | 16, 24 | 8, 12 |
| `cursor-hand` | index fingertip | 12, 4 | 6, 2 |
| `cursor-grab` | palm center | 16, 28 | 8, 14 |
| `cursor-grabbing` | fist center | 16, 28 | 8, 14 |
| `cursor-resize-x` | midpoint | 16, 24 | 8, 12 |
| `cursor-resize-y` | midpoint | 16, 24 | 8, 12 |
| `cursor-crosshair` | center gap | 16, 24 | 8, 12 |
| `cursor-busy` | glass waist | 16, 24 | 8, 12 |
| `cursor-not-allowed` | ring center | 16, 24 | 8, 12 |

Notes for the mask rasterizer:

- `cursor-arrow` matches the shipping hotspot convention (1,1 = tip).
- `cursor-grab`/`cursor-grabbing` share a hotspot so the hand doesn't
  jump on mouse-down.
- All fills are white on transparent; every outline closes. Reduction to
  a 2-color mask keeps the silhouette + interior seams as drawn.
- Proposed 8.3 names (adoption side): `CURS_PTR` arrow, `CURS_TXT` ibeam,
  `CURS_LNK` hand, `CURS_GRB` grab, `CURS_GRG` grabbing, `CURS_RSX`
  resize-x, `CURS_RSY` resize-y, `CURS_XHR` crosshair, `CURS_BSY` busy,
  `CURS_NO` not-allowed.
