<!-- iface-review:v1 app=GOWELCOME.ELF issue=3703 -->
# App audit: Welcome (GOWELCOME.ELF)

> Retired from polish audit issue #3703 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The first-boot tour: five core gestures and one-click launchers on a tabapp canvas. The seat execs it beside GOSH.ELF on a live first boot (when SESSION.TABS is absent); after first boot the launcher catalogue keeps it reachable. Manifest row (image/apps.txt:47): `GOWELCOME.ELF | Welcome | ? | dock=false | v=2 | icon=ICONWELC` — `?` is the literal v1 rune fallback; the raster ICONWELC is staged.

## Interface today

Observed from `user/go/welcome` and the go-welcome run:

- 512x384 window: a title/subtitle header, five gesture rows each with a key chip + one-line description, four launcher buttons, a footer line, and the mascot wave art.
- The five taught moves: CLICK (focus a tab), ctrl+space (launcher), ctrl+tab (cycle focus, +shift back), DRAG (reorder rail tabs), ctrl+shift+p (command palette). The three chord spellings are pulled live from `chords.Global` (tour.go seatChord) — a re-homed chord updates the tour for free, and a missing registry row fails the host tests rather than emptying a chip.
- Launcher buttons exec GOSET.ELF, GOFILES.ELF, GOSTORE.ELF, GOTERM.ELF (tour.go launchers); the status line echoes the last launch outcome (`gowelcome: launch <bin>` / `launch fail`).
- Arrow keys step the tour; Esc/q quits; appver-1 envelope persists `step` + `done` so a mid-tour close resumes on the same gesture instead of restarting (M195b #3307 — the owning leg pins exactly this round-trip).

## Coverage

- `go-welcome.spec` — 1 run, **PASS** (artifacts/go-welcome-gate.txt): the save/restore envelope round-trips under the live Go seat. Also exercised as the first-boot pair in `live-polish-sweep.spec` and `go-wm-default.spec`.

## Observed defects

None observed — the single owning leg is green and no roughness surfaced in the run output.

## Improvement candidates

- The tour teaches spellings but never verifies the gesture was performed — an interactive "try it" step that listens for the real chord (the seat dispatches it; a last-action receipt seam would be needed) would make it a tutor instead of a slide deck.
- No progress affordance: `step` exists internally but the frame doesn't say "2 of 5"; the staged `MSC_CELB` pose (#3348's remaining-pose list) is a natural done-screen celebration.
- Five gestures is a thin syllabus for a tabbed desktop: tab close/reopen, the notification center, and window resize edges are untaught — chapters or a second page could cover them.
- The four launcher buttons exec apps with no follow-up ("you opened Settings — this row is where the tour lives") and no way to reopen the tour from inside Help; a GOHELP cross-link (once GOHELP has an argv deep-link) would close the loop.
- `?` rune is an honest v1 fallback but the only manifest rune that is punctuation — a dedicated glyph could read better on a share without the raster (minor).

## Evidence

- `just gate go-welcome` — PASS 1/1 (artifacts/go-welcome-gate.txt, go-welcome-report.txt).
- `image/apps.txt:47`; `user/go/welcome/{main,tour}.go`; `user/go/chords` registry consumption; `site/apps/gowelcome.md` + `site/includes/apps/gowelcome.story.md`; `docs/component-registry.md:76`; first-boot pairing per `tools/gate/specs/live-polish-sweep.spec` header comment.
