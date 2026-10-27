<!-- iface-review:v1 app=GOPAINT.ELF issue=3738 -->
# App audit: Paint (GOPAINT.ELF)

> Retired from polish audit issue #3738 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The paint app — brush, line, rectangle, flood fill and eraser over a fixed 16-colour palette at two brush sizes, on a draw-v2 document canvas with an undo ring; QOI save/open through the shared FilePicker, `x` for PNG export; a drag-drop replaces the canvas behind a confirm. Manifest row `GOPAINT.ELF | Paint | P | dock=false | v=2 | opens=image` (image/apps.txt:69) — **no `icon=` and no `caps=`**: the only audited row without a raster icon (tile is the `P` rune) and without declared caps.

## Interface today

Observed from `user/go/gopaint/{main,doc,state}.go` + spec geometry:

- One window, native 832x600 at (24,24). 24 px title band (`GOPAINT` or `<file> *` for dirty), a 56 px left toolbar (B/L/R/F/E glyph buttons, two size swatches, the 4x4 palette grid of 11 px cells), the document viewport at (64,32) over an alpha checkerboard, and a 16 px status bar (`brush  size=4  color=8  unsaved`).
- Declared title is deliberately `GOPAINT.ELF` (the seat resolves drop-admission and session re-exec through the recorded bin); `SetTabTitle` repaints the rail label "Paint".
- Keyboard: `b/l/r/f/e` tools, `1-8` palette, `[/]` color step, `-`/`=` size step, `x` PNG export, ctrl-o open (dirty canvas first asks discard-confirm), ctrl-s save picker, ctrl-z undo (routed via tabapp's undo action), `q`/Esc quit.
- Pointer: press-drag-release gestures; line/rect rubber-band preview (display-only — the undo ring sees committed ops alone); fill commits on press. NOTE: pointer input only reaches the canvas under the bare shim — under GOTABWM the seat's content forward is terminal-only, so the app is effectively keyboard-only when docked in a tab.
- File flow: QOI save/open + PNG export sibling-path (`PAINT1.QOI`→`PAINT1.PNG`); reads are bounded (4 MiB); big QOI retries through the app's own canvas cap; drop offers decode QOI/PNG/SVG and confirm before replacing.

## Coverage

`tools/gate/specs/go-paint.spec` — ran `BOOTS=1 just gate go-paint` on this worktree: **FAILED, 0/5 runs** (host under concurrent-VM load from sibling worktrees). Run 01: every tool + undo digest green but `ctrl-s` never raised the picker (chord non-delivery → timeout). Run 02: `gofiles: drag arm` never fired. Run 03: reopen died mid-init, watchdog state-ask clobbered the blob with defaults → `tool=brush` not `tool=rect`. Runs 04/05: `store: refuse manifest_malformed` on remove+install (STORE.ELF exit 1). Defect card: #3737.

## Observed defects

- **Watchdog state-ask clobbers the session blob** (run 03): a tab dying during init lets the watchdog collect a default-state blob over the saved one — the restore loses tool/file selection (`tool=brush` vs saved `tool=rect`). Filed as #3737.
- **Store legs refuse `manifest_malformed`** (runs 04/05): `store: remove`/`install` both refused after `store: verified gopaint`; STORE.ELF exits status=1 while the .SH's own echo markers (`m141b: installed`) still print — so journeys can't self-detect the refusal. On #3737.
- **Likely-load failures** (runs 01/02): `ctrl-s` chord never reached the app (gestures+undo all green); drag arm never fired under the seat. Needs an idle-host re-run — flagged on #3737.

## Improvement candidates

- No `icon=` raster — the only audited app still on a rune tile; a paint-bucket/palette tile is an easy f-artcraft win (#3341).
- No `caps=` either — declare `file,window` like its peers for the enforce-mode ceiling.
- Pointer-dead under the seat: drawing inside a tabbed session is impossible (the seat's content forward is terminal-only). Until the seat forwards pointer events to canvas apps, Paint is a shim-mode citizen — that's the single biggest UX gap; either fix the forward or document the limitation on the showcase page.
- Tool buttons show letter glyphs (B/L/R/F/E), not tool icons — 8x8 pictograms would read better; the status bar already names the tool.
- Two brush sizes only (`brushSizes`); a slider or a third size is cheap since size swatches are already drawn as glyph-is-size squares.
- No redo — the undo ring is one-directional.
- Palette is fixed 16; a click-to-edit swatch (RGB picker) would fit the picker-modal pattern already in the file.
- Canvas fixed at doc size with no zoom — a 2x/4x zoom toggle for detail work.

## Evidence

- `BOOTS=1 just gate go-paint` → `artifacts/go-paint-report.txt`: `vgate go-paint: FAILED (0/5 runs)`; serial logs `go-paint-serial-0{1..5}.log` (gesture digests, `state restored ... tool=brush`, `store: refuse manifest_malformed`).
- Code reads: `user/go/gopaint/{main,doc,state}.go`; manifest `image/apps.txt:69`; showcase `site/apps/gopaint.md`.
