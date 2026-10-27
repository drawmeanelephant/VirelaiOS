<!-- iface-review:v1 app=PULSE.ELF issue=3725 -->
# App audit: Pulse (PULSE.ELF)

> Retired from polish audit issue #3725 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

A Charm TUI system monitor — a real `tea.Model` fed by the kernel's bound `/dev/tty` queue (Update gets tty-decoded keys, View emits ANSI back), repainted on every key and on a 1 Hz timer tick. No POSIX tty underneath — same shape GOFILES and GOHELP ride. Docked via `image/apps.txt` (`PULSE.ELF | Pulse | u | dock=true | v=2 | icon=ICONPULS`); no `caps=`, no `opens=`. Implementation: `user/go/pulse`, 640x400 tabapp TUI at 32,32.

## Interface today

Observed from `user/go/pulse/{main,model,update,view}.go` + `go-top.spec` run 04:

- Four tabs on number keys: `1` Overview, `2` Procs, `3` Net, `4` Storage (`model.go:29-41`); the spec drove `2` and got `pulse: repainted`.
- Procs tab: `j`/`k` move selection, `s` or Return cycles the sort column (`update.go:64-76`), `/` opens filter entry (printable ASCII, ≤24 chars, backspace edits, Return/Esc closes), `x` kills the selected proc with a `y`/`n` confirm dialog (`update.go:80-89`, `doKill` → slot 29, honest `kill: no process selected` refusal).
- `r` refresh ("refreshed" notice), `?` help overlay (eats all keys except `?`/Esc/`q`), `q` quit, Esc is a no-op at top level.
- Footer hint line painted in-app (`view.go:85`): `1-4 tabs · j/k move · / filter · s sort · x kill · r refresh · ? help · q quit`.
- Usage figures come from the negotiated usage seam — the go-top run printed real numbers: `pulse usage ok: cpux10=769 memused=10391552B memtotal=535556096B`.
- Declares its window title as `PULSE.ELF` (binary name, deliberately — `main.go:21-22`: session restore's `guessBin` falls back to the title), so the seat's tab rail shows "PULSE.ELF", not "Pulse".
- No mouse input — it is a pure key-driven TUI; the help overlay is the only modal.

## Coverage

`tools/gate/specs/go-top.spec` — ran locally: **PASS (11/11 runs, 228 s)**. PULSE's owning leg is run 04 (M194f): `exec: loaded PULSE.ELF` → `pulse: ready` → `pulse: procs` → `pulse: usage cpux10=` (real negotiated figures) → HID `2` tab switch → `repainted` → `tick` (the 1 Hz arm) → `close` → `pulse OK`. PULSE is also staged and key-driven in `go-charmhello.spec` (a secondary consumer leg, not its owner) — not run separately here.

## Observed defects

None. The owning leg passed with real usage figures and a clean close; no `[EXC] parking:` or crash statuses.

## Improvement candidates

- **Sort direction is a one-way cycle** — `s`/Return steps through the column (`sortCol = (sortCol+1) % 3`, `update.go:64-76`); there is no ascending/descending toggle, so a "least first" view is unreachable. `S` (shift) reversing direction is the cheap fix.
- **Tab switching is digit-only** — no `tab`/`l`/`h` cycling; users on layouts where digits are chorded can't move tabs. `h`/`l` are free outside Procs (where j/k navigate).
- **Window title is the binary name** — the rail reads "PULSE.ELF" while the manifest Display Name is "Pulse" (`main.go:21-22`). Deliberate for session restore, but a friendlier title with restore keyed off `Name` rather than `Title` is worth a design-loop look.
- **Pure text TUI where a surface exists** — CPU/mem figures render as ANSI text; the same data could paint the staged-art sparkline/gauge art the design loop (#3341) is for, without changing the model.
- **`r` refresh overlaps the 1 Hz tick** — the notice "refreshed" implies a manual refresh matters; a last-updated timestamp in the footer would make the cadence legible.
- **Kill confirm is the only modal** — no per-process detail view (Enter on a row currently sorts); a detail overlay (uptime, rss trend) is the standard monitor next step.

## Evidence

- `just gate go-top` → `vgate go-top: PASS (11/11 runs)`; run 04 = PULSE leg (`artifacts/go-top-gate.txt`, worktree `t3-b1dbc866`).
- `grep`/`sed` reads of `user/go/pulse/{main,model,update,view,types}.go`, `image/apps.txt`, `site/apps/pulse.md`, `site/includes/apps/pulse.story.md`, `tools/gate/specs/go-top.spec`.
- Dedupe: `gh issue list --search "pulse" --state open` → no existing audit card.
