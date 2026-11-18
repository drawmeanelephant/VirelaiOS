<!-- iface-review:v1 app=GOFILES.ELF issue=3729 -->
# App audit: Files (GOFILES.ELF)

> Retired from polish audit issue #3729 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The file manager — a real Bubble Tea `tea.Model` over the kernel's bound `/dev/tty` (no POSIX tty underneath; ANSI frames are chunked through `vi.FileWriteAll`). Docked via `image/apps.txt` (`GOFILES.ELF | Files | f | dock=true | v=2 | caps=file,window | opens=text/vcard,archive | icon=ICONFILE`); the only docked app that declares `caps=`, `opens=text/vcard` routes `.vcf` files to it, and `opens=archive` receives a dragged directory's tar into the current directory. Implementation: `user/go/files` (~16.6k LOC), 512x384 tabapp TUI.

## Interface today

Observed from `user/go/files/{main,input,render,quick,grid,tar,drag,open}.go` + `go-fileman.spec`:

- Two-pane TUI; full hint row painted in-app (`render.go:429`): `f10 menu · j/k move · enter open · / search · space look · i props · o open with · e export · t grid · v volumes · f pins · b pin · tab view · s sort · backspace up · r rename · m mark · M unmark · d trash · u restore · c/x clip · p paste · q quit`. F10 engages the appkit menubar; `?` the hint overlay (`hintsKey`).
- Verbs: navigate/open, rename via a text entry, mark-set batch copy/move/delete, trash + undoable restore, clipboard c/x/p, recents, folder pins (`FILES.FAV`, gone pins named `missing`), volumes list (kernel mount table), file properties card (ctime/birth/on-disk bytes/volume-free), `.tar`/`.tgz` browsed in place as a read-only virtual directory, spacebar QuickLook (image/text/PDF page-one; `.vcf` paints a parsed contact card whose email row hands off to `GOMAIL.ELF send --to`), `t` toggles a thumbnail grid (lazy cell-mosaic tiles, focused image as a real sixel), `/` bounded content search over the subtree with n/N walking hits, Open With right-click menu through the shared mime registry, `.js` dispatch to QJS.BIN.
- Drag source: a held move off a pressed row arms the seat-mediated drag with named refusals — archive rows and volume/pseudo rows do not carry (`drag.go`, `carry.go`), an over-`DragMax` file is refused by name, and a directory carries one complete bounded tar under the `archive` MIME (`1024 + 512·members + padded bodies`, capped at the literal 65536 bytes); an incomplete walk or an encoded archive past that cap refuses without a partial carry.
- Drag target: a delivered `archive` tar is parsed and shape-checked before publication, saved in the receiver's current real directory under a root-derived `.TAR` name without replacing an existing file, then reopened in the existing read-only archive browser (no extraction). The bytes land through a stage the publisher alone owns — `~` plus the kernel pid (`vi.SelfPid`) and this process's sequence, so two live windows cannot share a stage even at the same nonce and sequence — written directly with the file primitives (no hidden `stage~` helper), then published by the kernel's atomic preserve-existing rename. A writer that takes the probed name in between keeps its exact bytes and the drop refuses by name instead of deleting it; a stage directory with no room for the full owner refuses by path rather than truncating it. `text/vcard` keeps its contact-card drop.
- Mouse: SGR reports split off the tty stream (`main.go:4-11`) — row press, right-click menu, drag. Model tests cover the pure layer; the spec drives rename over real HID.

## Coverage

`tools/gate/specs/go-fileman.spec` — run required two staging fixes before it would boot — see Observed defects.

The spec's own run list (from `vgate_name`): navigate/preview/rename/trash/restore/open, path picker, session resume, Open With menu, mount table, QuickLook on image+text+PDF page one, properties card, batch move+delete, bounded content search (run three times), thumbnail grid with focused sixel, favorites pins with missing-pin naming, in-place .tar browse, `.vcf` contact card + GOMAIL hand-off with real SMTP+TLS submit, `.js` → QJS.BIN dispatch, and a real HID folder drag between two GOFILES windows that saves the tar without overwrite and browses its members.

## Observed defects

- **Spec hygiene, not app: `GOMAIL-GATE.ELF` is a hard staging requirement absent from the spec's `HOST PREREQUISITE` comment** — the spec `sys.exit`s at staging with the honest message (`go-fileman.spec:2389-2393`), but the header prereq list doesn't mention it, so a cold run fails 10 s in. Cheap fix the next spec PR could fold in.



## Improvement candidates

- **The list grid is text-only where staged art exists** — the registry notes 10 filetype icons are staged (`image/art/ART.TXT`) but paint only in `appkit.FilePicker` rows; GOFILES' own rows never show them (`docs/component-registry.md:235`). Wiring the icons into the list/grid rows is the single most visible refresh this app could take.
- **QuickLook is a mode, not a pane** — `space` swaps the whole frame to the preview; a persistent split (list + preview) is the standard Finder muscle the two-pane layout almost has.
- **Search hits are n/N cycled but there's no results list** — a hit drawer would make the bounded search feel less blind.
- **Hint row is one long line** — `render.go:429`'s string is comprehensive but dense; a two-column `?`-overlay grid (it exists — `hintsKey`) grouped by verb family would read better.

## Evidence

- `just gate go-fileman` first run → staging FAIL on `GOMAIL-GATE.ELF missing`; built via `bash tools/go/build-web.sh gomail GOMAIL --gate`; rerun — see result note.
- `grep`/`sed` reads of `user/go/files/*.go`, `image/apps.txt`, `site/apps/gofiles.md`, `site/includes/apps/gofiles.story.md`, `docs/component-registry.md:235`.
- Dedupe: `gh issue list --search "files" --state open` → no existing audit card.
