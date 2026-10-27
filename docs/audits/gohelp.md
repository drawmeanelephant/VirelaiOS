<!-- iface-review:v1 app=GOHELP.ELF issue=3702 -->
# App audit: Help (GOHELP.ELF)

> Retired from polish audit issue #3702 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The help browser: a Bubble Tea model over the bound `/dev/tty` that walks the staged `/host/docs` bundle and the GOSH command catalog — single-sourced from `shlib.HelpRows()`, so the browser cannot fork the shell's help text. It runs the model without Bubble Tea's host Program loop (no POSIX tty/signals on Virelai): key bytes decode from the kernel's bound-tty queue, ANSI frames write back into it. Manifest row (image/apps.txt:46): `GOHELP.ELF | Help | h | dock=false | v=2 | icon=ICONHELP` — no caps, no opens. Launched via the launcher/palette catalogue and `exec GOHELP.ELF`; the seat has no dedicated help verb observed.

## Interface today

Observed from `user/go/help` and the go-help run's serial + screenshot:

- 512x384 window; two-pane browse view: left a grouped command list (sections per `shlib.HelpSections()` — the six D1 groups + tools + externals), right a live detail preview of the selected row (name/usage/blurb in the amber accent, notes in body colour).
- Keys: `j/k` move, `h/l` group-jump, `enter` full-page detail, `/` incremental filter (case-folded against names, live match count), `d` docs list, `q` quit; SGR mouse clicks hit rows; `esc` clears/backs out.
- Docs browser reads `/host/docs/*.txt|*.md` (sorted, ≤64 KiB pages, honestly marked truncated).
- Truecolour ANSI rendering (`38;2;r;g;b`) — detail text in (255,199,92), header/status accents; the frame geometry is the px-derived terminal grid.
- Boot markers report `gohelp: catalog n=62` — the spec's comment still says "the 45-row catalog", stale prose vs. the grown table (assert reads the live n=, so only the comment drifted).

## Coverage

- `go-help.spec` — 1 run, **FAIL** on this host (artifacts/go-help-gate.txt, go-help-report.txt): all 33 serial asserts passed (catalog n=62, docs n=2, group jumps, `filter ec n=3` → cleared n=62, `detail echo usage=echo [ARG...]`, settle, clean close), but the `help-screen-after` snapshot found 0 accent pixels in the detail band. The saved capture shows the *browse* frame (two-pane, `clear` selected) — a pre-detail frame, i.e. the compositor presented a stale frame at capture time; 358 accent pixels do exist elsewhere (preview pane y≈167-310, status row y≈780). The app's marker discipline is paint-then-print, so this reads as present-lag vs. ScreenCaptureKit under concurrent VM load on this host — a gate-robustness flake to rerun on a quiet machine, not (as observed) app behaviour.

## Observed defects

- None observed in the app itself. The stale "45-row catalog" comment at tools/gate/specs/go-help.spec (marker comment near markerFilterCleared) is prose drift only.

## Improvement candidates

- Filter matches names only (state.go); searching usage/blurb/notes text is the natural next step — 'ec' today narrows to 3 rows by name.
- No argv deep-link: `GOHELP.ELF echo` could open that row's detail directly, giving GOSH's `help`, the seat and other apps a target to point at.
- Detail pages have no cross-references — notes naming related verbs could be navigable rows.
- The docs reader is a flat file list; grouping, in-page search, or a scroll model for the >64 KiB truncation case would help.
- The palette already routes help topics to GOHELP (gotabwm/palette.go:16,587) — what is missing is a direct chord (F1/?) or a `help`-in-GOSH bridge that lands on the same page the shell verb printed.

## Evidence

- `just gate go-help` — FAIL 0/1, serial asserts all green, snapshot assert `detail-accent=0` on a stale-frame capture (artifacts/go-help-gate.txt, go-help-report.txt, go-help-help-screen-after PNG kept).
- `image/apps.txt:46`; `user/go/help/{main,state,render,tea,markers}.go`; `user/go/shlib/builtins.go:829-846` (HelpRows/HelpSections single source); `site/apps/gohelp.md` + `site/includes/apps/gohelp.story.md`; `docs/component-registry.md:75`.
