<!-- iface-review:v1 app=GOSHEET.ELF issue=3730 -->
# App audit: Sheet (GOSHEET.ELF)

> Retired from polish audit issue #3730 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

GOSHEET.ELF is the spreadsheet (M142a/b, M152a): a virtualized grid behind a `TableController`, an edit line with sheet-convention replace semantics, live formula recalc that names cycles (`#CYCLE`), a menu bar, fills, a 32-step undo ring, and per-column display formats. Manifest row: `dock=false | v=2 | caps=file,window | opens=text | icon=ICONSHT` — palette/exec launch; `opens=text` makes it a text-handler candidate.

## Interface today

Observed (code, `user/go/gosheet/`):
- **Layout** (672x420): M137b menu bar (`Edit` — Fill down/right/series; `Format` — General/Integer/Fixed 2/Percent) at y20..44, engaged by F10/Alt/mnemonics; a name box (`A1`) plus edit line showing the focused cell's raw text; the virtualized grid with column-letter header; a 20px status line (focus, selection, display, fill count).
- **Keyboard model**: arrows/Home/End/PgUp/PgDn move the cell cursor; shift-move anchors a mark, ctrl-shift-m latches it, ctrl-a selects the used box, Esc drops it. Typing seeds the edit line (replace semantics); Enter/Tab opens the edit on existing text; Delete/Backspace clears. ctrl-c/ctrl-v move ranges as RFC 4180 CSV on the `text/csv` clipboard lane (over-kernel-bound refuses `clip-too-large`); ctrl-o/ctrl-s raise the shared FilePicker for crash-safe CSV import/export; ctrl-z pops undo (intercepted ahead of the appkit loop's own ActionUndo).
- **File format**: CSV only — formulas are not persisted; export writes display values.

## Coverage

`tools/gate/specs/go-sheet.spec` — PASS 27/27 (`just gate go-sheet`, 2026-10-25, worktree t3-40900d1c — this spec pins `vgate_repeat 3` with no env override, so all nine legs ran three full iterations). Recalc/`#CYCLE`/paste/picker-import/clipboard-bound refusal+export/seat-state save+restore/fill+undo+format/session-restore legs all green, including the K=3 acceptance that reopens the exported CSV in GOEDIT and compares cells. Rough reading: none — the only app gate in this audit batch to run its full K=3 and stay green. — 9 legs: recalc, `#CYCLE`, paste, FilePicker import, bound-refusal+export, seat state save/restore, fill/undo/format, second-boot session restore; `vgate_repeat 3` in CI (audited with BOOTS=1).

## Observed defects

None observed — 27/27 legs green, zero `gosheet: *refused*` anomalies beyond the deliberately asserted `clip-too-large` bound refusal.

## Improvement candidates

- `opens=text` is broad — `text/csv` (an M124a subtype spelling) would be the accurate claim and would stop Sheet appearing as a handler for arbitrary prose.
- Formulas are lost on save (export writes display values only): a formula-preserving file format is the obvious next milestone; today a `.csv` round-trip flattens `=sum[a1:a2]` to `13`.
- Only two menus and seven items total — no File menu (open/save live only on ctrl-o/ctrl-s chords); discoverability for the picker verbs is zero without the cheat sheet.
- No cell styling (bold, alignment, colors) and no column resize; `colW=64` is fixed for every column.
- No formula entry help — no function list, no autocomplete, no argument hints; `=sum[` syntax is folklore.
- The status line is one string; a dirty/modified indicator would help since there is no close-confirm either.
- Row/column insert-delete is absent — the grid is a fixed-address sheet.

## Evidence

- `gh issue list --search "Sheet GOSHEET" --state open` → no existing audit issue.
- `bash tools/go/build-gosheet.sh` (+ gotabwm/goedit prerequisites) → staged.
- `just gate go-sheet` → `vgate go-sheet: PASS (27/27 runs)` (the spec forces `vgate_repeat 3` — BOOTS does not override it). Serials `artifacts/go-sheet-serial-*.log`.
- Interface observed from `user/go/gosheet/main.go` (layout constants, menu table, key chords, marker vocabulary) and `render.go`/`model.go`; cell-level appearance confirmed by the gate's snapshot probes (e.g. `cell r0 c1 = '50%'` on the format leg).
