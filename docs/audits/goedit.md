<!-- iface-review:v1 app=GOEDIT.ELF issue=3717 -->
# App audit: Code Editor (GOEDIT.ELF)

> Retired from polish audit issue #3717 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The code editor — the registry's named owner of "the editor arms race". Docked via `image/apps.txt` (`GOEDIT.ELF | Code Editor | e | dock=true | v=2 | opens=text,binary | selection=primary | icon=ICONEDIT`); `opens=text,binary` takes file hand-offs through the M124d argv convention (`vi.Args()` at `main.go:688`, `args[1]` is the path) and `selection=primary` opts into the seat's primary selection. Implementation: `user/go/edit` (~11k LOC), 512x384 at 32,32, title "Edit".

## Interface today

Observed from `user/go/edit/main.go` + `go-edit.spec`:

- Chords: Ctrl-S save / Ctrl-Shift-S save-as picker (a `.pdf` name exports a paginated fixed-pitch PDF via `virelai/pdfwrite`), Ctrl-F find bar, Ctrl-G goto-line bar, Ctrl-R replace bar (Tab to the replacement field, Ctrl-A replace-all), Ctrl-Z / Ctrl-Shift-Z / Ctrl-Y undo/redo over a bounded ring (`histCap`), Ctrl-E diagnostics panel (`.BUILD/ERRORS.TXT`), Ctrl-N/Ctrl-P next/prev diagnostic, Ctrl-B bracket-match jump, Ctrl-K toggle fold / Ctrl-U unfold-all, Ctrl-O open (replaces buffer) / Ctrl-Shift-O open into a NEW buffer / Ctrl-W close buffer / Ctrl-PageUp/PageDown cycle the ring, Ctrl-P quick-open palette, `?` chord-hint overlay, and an F10/Alt appkit menubar (`buildMenus`, `main.go:2694`) whose rows call the same Actions.
- Selection: Shift+arrows or Ctrl-Shift-M mark, Ctrl-A select-all; a find's match lands selected. `selection=primary` publishes it.
- Clipboard: Ctrl-C/X/V through slots 38/39 — CAP_CLIPBOARD-gated, so an unprivileged exec prints the `-EACCES` receipt instead of a shadow clipboard.
- Status line right-aligns `line=L/T col=C`, `sel=N`, `*` dirty, `err=I/N` while a diag list is loaded.
- The kernel posts WIN_UNSAVED on the seat's unsaved-changes dialog; Save publishes the buffer, the other paths arrive as WIN_CLOSE.
- Optional `-minimap` argv flag reserves a right-edge minimap column (one pixel row per line, click jumps the caret); `-heap`, `--diags=`, `--prof=` are fixture flags.
- Light/dark themes from `virelai/theme`; subscribes to the settings `theme` key and repaints live; the caret amber and bracket teal are fixed accents.
- Mouse: the minimap column takes presses; **the document body takes no mouse input** (header comment `main.go:141-142`) — no click-to-caret, no drag-select.
- Named non-features (header `main.go:118-119`): no syntax highlighting, no tab strip/split view over the buffer ring, no LSP, no auto-indent. Format-on-save ships a named refusal — `edit: format unavailable (go/format not guest-safe)` — because `go/format` pushes the image over `exec_program_max`; `-tags editfmt` is the opt-in seam.
- Publishes the M197d a11y outline (`a11y.go`).

## Coverage

`tools/gate/specs/go-edit.spec` — ran locally: **PASS (19/19 runs, 661 s)** after building the undocumented `GOSET.ELF` prereq (see defects). Legs cover load-edit-save-close, find/goto/replace bars, undo ring, selection, clipboard rc, diagnostics panel, brackets, folds, the buffer ring, quick-open, themes, the `-minimap` column leg with pixel probes, and the unsaved-changes dialog client.

## Observed defects

None observed in the app — all 19 runs passed with clean serial logs. One gate-spec nit: `GOSET.ELF` (needed by the run-15 seat leg, staged at `go-edit.spec:1134`) is missing from the spec's `HOST PREREQUISITE` header, so a cold run fails at staging with only the honest `sys.exit` message.

## Improvement candidates

- **Syntax highlighting is the headline gap** — explicitly out of scope today (`main.go:118`); the `.go` scanner already exists for bracket matching (skips string/rune/comment spans, `brackets.go`), so a first pass could reuse that lexer for keyword/comment/string tinting without a full grammar.
- **Format-on-save is a shipped refusal.** Every `.go` Ctrl-S prints `format unavailable (go/format not guest-safe)` — the cap bump the seam (`format.go`, `-tags editfmt`) anticipates is the real fix.
- **Document body is keyboard-only.** No click-to-caret or pointer selection (`main.go:141-142`); the minimap is the only pointer surface. Click-to-position + drag-select is the daily-driver gap.
- **No case-sensitivity toggle or match highlight in find** (deliberate deltas from the Zig app, `main.go:47-58`) — worth revisiting in a refresh card; a find hit is selected but unmarked elsewhere in the viewport.
- **No word-wrap option** — `exportWrap` exists only for PDF export; the document view clips like NOTE's.
- The `?` overlay exists but the menubar's Shortcut column is the only persistent discoverability; the newer chords (Ctrl-P palette, Ctrl-Shift-O ring) could earn a row or a footer hint.

## Evidence

- `grep`/`sed` reads of `user/go/edit/main.go`, `buffer.go`, `brackets.go`, `export.go`, `quickopen.go`, `image/apps.txt`, `site/apps/goedit.md`, `site/includes/apps/goedit.story.md`.
- Dedupe: `gh issue list --search "goedit" --state open` → no existing audit card.
