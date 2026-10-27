# VirelaiOS — the virelai area

Interface art and design docs for [VirelaiOS](https://github.com/drawmeanelephant/VirelaiOS)
— the OS this repo's mascot and commemorative work already decorates. This file is the
convention of record for the family (established by #1261);
[notes/style-bible.md](notes/style-bible.md) is the taste.

Sibling work: asset requests #1248–#1251, #1253–#1255, #1257 · figcraft sheets
#1262–#1264.

## Layout

| Path | Holds | Committed |
|---|---|---|
| `art/virelai/<slug>/` | delivered art, one dir per piece | `.vectorcraft` + `.svg` + `@2x.png` |
| `work/virelai/` | driving scripts `draw_<slug>.py` + shared `vkit.py` | scripts |
| `virelai/design/<sheet>/` | figcraft design sheets — UI iteration docs | native + `.svg` + `@2x.png` |
| `virelai/notes/` | style bible + working notes | markdown |
| `work/out/` | scratch renders, PDF proofs | never |

The issue's proposed `virelai/assets/` maps onto the house layout: delivered art
lives under `art/<series>/` so the site scanner picks it up, so there is no
`virelai/assets/` — the series is **`virelai`** and pieces are
`art/virelai/<slug>/<slug>-V*R*.*`. Downstream issues written as
`art/virelaios/<family>/<slug>/` land here as `art/virelai/<family>-<slug>/`.

## Naming

- Slugs are **`<family>-<name>`**, flat under `art/virelai/` — the site scan
  reads two levels (`art/<series>/<slug>/`), so no nested `icons/` subdirs.
  Alphabetical order already groups the families.
- The revision rule is unchanged: `<slug>-V<V>R<RR>` on every output
  (`AGENTS.md`). First pass `V1R00`; never overwrite a prior revision.
- File names = slug + revision, e.g.
  `art/virelai/icon-gofiles/icon-gofiles-V1R00.{vectorcraft,svg,@2x.png}`.
- Scripts: `work/virelai/draw_<slug>.py` (e.g. `draw_icon_gofiles.py`),
  bootstrapped like every series — see `work/virelai/README.md`.

### Families

| prefix | issue | master size | must also read at | notes |
|---|---|---|---|---|
| `icon-` | #1248 | 64×64 | 24, 16 | 31 app icons; silhouette-first, ≥3 px strokes at master; replaces the rune field |
| `cursor-` | #1249 | 32×48 | 16×24 mono mask | closed outlines, chunky silhouette; record hotspots (below) |
| `mascot-` | #1250 | 64×64 | 24 | same character, 6 poses — not a redesign |
| `wallpaper-` | #1251 | 2560×1440 | — | center-weighted; keep subject clear of seat chrome zones |
| `spot-` | #1253 | 160×120 | 80×60 | reads on dark AND light surfaces; one subject, no scenes |
| `brand-` | #1254 | 800×450 / 256×256 | 64 (emblem) | boot plate + about emblem; wordmark in Virelai Sans |
| `spinner-` | #1255 | 32×32 per frame | 16 | per-frame PNGs + one uniform-grid sheet, seamless loop |
| `filetype-` | #1257 | 32×32 | 16–20 | flatter than app icons; one accent hue per family |
| `sheet-` | review aid | — | — | per-wave contact sheets as their own piece (below) |
| figcraft | #1262–64 | 1280×720 frames | — | design sheets live in `virelai/design/`, not `art/` |

## Working rules

- **Transparent backgrounds** for anything that paints on chrome or app
  surfaces — icons, cursors, mascot poses, spots, filetypes, spinner frames.
  Wallpapers and the boot plate declare their field (navy or transparent).
- **Read-at-size gate:** before calling a revision done, inspect the proof at
  the smallest target size (16 px for filetypes, 24 px for icons/mascot,
  80×60 for spots). Detail that doesn't survive gets dropped, not shrunk.
- **8.3 names are adoption-side.** The OS build converts PNG masters to QOI
  under 8.3 names (`ICONCALC.QOI`, `CURS_PTR.QOI`) — keep full names in the
  repo; record the proposed 8.3 name in the piece's inbox note when the
  adoption card needs it.
- **Hotspots:** each cursor records its intended hotspot (arrow tip, ibeam
  mid, fingertip, crosshair center) in `art/virelai/HOTSPOTS.md` when the set
  lands, or in its inbox note.
- **Contact sheets:** a wave's review sheet is its own piece
  (`sheet-icon-wave1-V1R00`, on a uniform grid in animation/order) so it's
  viewable on the site and diffable like any revision.
- **Digits:** Virelai Sans has no digit glyphs — set digits in Source Sans 3.
  `add_text` takes no `opacity`/`style` kwargs; create the text object, then
  `object.setProps` / `text.setStyle` / `object.rotate`.
- **Deliverables per revision** under `art/virelai/<slug>/`: `.vectorcraft` +
  `.svg` committed on main; the `@2x.png` preview ships on the **media**
  branch to keep clones small. The `.pdf` proof stays local in `work/out/`.
  `virelai/design/` sheets ship the same set (natives are whitelisted in
  `.gitignore`).
- **Every task lands one `prompts/inbox/` note** (see `prompts/README.md`) —
  critique plus whatever family lesson the next wave needs (e.g. which
  strokes survived 16 px).

## Ground truth

VirelaiOS is public — design against the shipped pixels, not vibes:
`docs/ui-primitives.md` (chrome contract + tokens), `user/go/theme/theme.go`
(token hexes), `site/desktop.md` (the seat), `image/apps.txt` (the 31-app
fleet), `image/WALLPAPER.QOI` + `user/src/lib/fixtures/qoi/mascot_64x64.qoi`
(the two existing art fixtures). The sampled palette lives in
[notes/style-bible.md](notes/style-bible.md) and `work/virelai/vkit.py`.
