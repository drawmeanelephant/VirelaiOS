<!-- iface-review:v1 app=NOTE.ELF issue=3707 -->
# App audit: Text Editor (NOTE.ELF)

> Retired from polish audit issue #3707 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The fleet's plain text editor — deliberately not an editor arms race (GOEDIT owns that). Docked via `image/apps.txt` (`NOTE.ELF | Text Editor | n | dock=true | v=2 | opens=text | icon=ICONNOTE`); `opens=text` makes it the text hand-off target. Implementation: `user/go/note` (tabapp client, 512x384 at 56,56 — the retired Zig notepad's exact rect, kept on purpose; title "Notepad", retitled to the document name once open).

## Interface today

Observed from `user/go/note/main.go` + `go-note.spec` output:

- Single document: loads `/host/notes.txt` at startup (`defaultPath`, `main.go:82`); the appver-1 session envelope can restore a different path + caret (`main.go:576`, `:614`). No `vi.Args()` call anywhere — see defects.
- Keyboard model: printable runes insert (UTF-8 kept whole even when the 8x8 face degrades to `?`), Return splits the line, Backspace/Delete edit, arrows/Home/End/PageUp/PageDown move; Ctrl-S saves (`main.go:760-769`); Ctrl-F docks the shared appkit find overlay (`main.go:771-777`). No selection, no undo, no replace — all deliberate (`main.go:8-11`).
- Dirty buffer auto-publishes on WIN_CLOSE (`main.go` close path) so closing a tab can't silently drop typed text; publishes crash-safe via `vi.WriteFileSafe` (delete-then-rename — a mid-publish crash leaves the file *absent*, not half-written; documented at `main.go:30-32`).
- Long lines are clipped at the right edge, never wrapped (`Buffer.View`, header comment `main.go:35-38`). Buffer bound is 32 KiB (`MaxBytes`, `buffer.go:23`) with an honest truncated report over it.
- No mouse/pointer handling of any kind — no `EvMouse*` case in the event switch (`main.go` `handle()`); click-to-position-caret does not exist.
- Publishes the M197d a11y outline on every change (`main.go:276-287`), deduped on encoded bytes.

## Coverage

`tools/gate/specs/go-note.spec` — ran locally: **PASS (1/1 runs, 33 s)**. The single run drives the open → edit → Ctrl-S → close → reopen journey, asserting path + caret come back inside the appver-1 envelope.

## Observed defects

- **`opens=text` is unimplemented in the app** — the manifest advertises NOTE as a text drop/open target, but the app reads neither argv (the M124d exec hand-off) nor `PollDrag` (the seat's in-window drag frames). A `.txt` handed to NOTE opens `notes.txt` instead. Filed as #3706.

## Improvement candidates

- **No mouse support at all.** No click-to-caret, no drag-select, no scroll-by-wheel — the only Go editor-family app with zero pointer input (`main.go` event switch). Even a click→caret landing would help.
- **No undo** — deliberate per the header comment, but even a one-deep undo (last edit op) is the gap users hit first in a notepad.
- **Fixed single buffer.** `path` is notes.txt-or-envelope only; there is no File→Open, no save-as, no second document. The `opens=text` fix (#3706) effectively requires the argv intake anyway.
- **No in-window chrome**: no status row (line/col, dirty marker, path) — GOEDIT's status line (`line=L/T col=C`, `*` dirty) is the obvious muscle to borrow.
- **Clipped long lines** with no horizontal scroll indicator — a `>`/`<` affordance on clipped rows, or a wrap toggle, would make the clip choice visible.

## Evidence

- `just gate go-note` → `vgate go-note: PASS (1/1 runs)` (local run; `artifacts/go-note-gate.txt` in worktree `t3-b1dbc866`).
- `grep`/`sed` reads of `user/go/note/main.go`, `buffer.go`, `a11y.go`, `image/apps.txt`, `site/apps/note.md`, `site/includes/apps/note.story.md`, plus `user/go/tabapp` and `user/go/edit/main.go` for the argv/drag comparison.
- Dedupe: `gh issue list --search "note" --state open` → no existing audit card.
