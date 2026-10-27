# design/ — the VirelaiOS design lane

VirelaiOS is the **design truth** (issue #3783). This directory vendors the
interface-design masters that used to live in
[drawmeanelephant/f-artcraft](https://github.com/drawmeanelephant/f-artcraft);
f-artcraft is now a renderer/preview sandbox that consumes these masters —
the conveyor only runs `tools/art/sync.py --export` (shipped-truth templates
for preview), never a fetch into this repo.

## Layout

`design/` mirrors two upstream subtrees **verbatim**, so the vendored
READMEs' relative links still resolve and `diff -r` against an f-artcraft
checkout is the provenance check:

| here | was (f-artcraft) | holds |
|---|---|---|
| `virelai/design/<sheet>/` | `virelai/design/<sheet>/` | design sheets — `.figcraft` / `.vectorcraft` natives + `.svg` + `@2x.png` renders (`tokens`, `apps`, `seat-shell`, `app-screens`) |
| `virelai/notes/` | `virelai/notes/` | the style bible — the taste the family answers to |
| `virelai/README.md` | `virelai/README.md` | family conventions of record (upstream text; its `work/` + `prompts/` references point at f-artcraft, which keeps the driving scripts) |
| `art/virelai/<slug>/` | `art/virelai/<slug>/` | delivered piece masters: `.vectorcraft` + `.svg` + `@2x.png` per slug, spinner `-fNN` frames, `HOTSPOTS.md` |

Naming and revision rules (`<slug>-V<V>R<RR>`, never overwrite a revision)
are upstream's; see `virelai/README.md` and `art/virelai/README.md`.

## Provenance

Imported 2026-10-26 from `drawmeanelephant/f-artcraft@1b0b03a8` (main).
All files are first-party: f-artcraft is the same owner's repo and the art
is authored for VirelaiOS — no third-party license terms ride with them
(`image/art/THIRD-PARTY.txt`).

## The contract

- **Masters here are truth.** `tools/art/sync.py` (no flags) adopts the
  highest `<slug>-V*R*@2x.png` per slug through `tools/art/adopt.py` into
  `image/art/` and regenerates `image/art/ART.TXT`; `--check` fails on
  drift. A design edit lands as a new `VxRy` master + a sync run + the
  staged-raster diff in one PR.
- **`--export` is the only outbound leg.** It regenerates the shipped-truth
  figcraft templates (`tokens/apps/seat-shell-SHIPPED`) under
  `artifacts/figcraft-out/` and diffs shipped code values against the
  local design sheets — f-artcraft picks the templates up for preview.
- The upstream driving scripts (`work/virelai/draw_*.py`, `vkit.py`) stay
  in f-artcraft; this repo carries their committed outputs.
