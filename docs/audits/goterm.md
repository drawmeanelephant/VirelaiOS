<!-- iface-review:v1 app=GOTERM.ELF issue=3718 -->
# App audit: Terminal (GOTERM.ELF)

> Retired from polish audit issue #3718 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The terminal — a tabapp window bound to the kernel's `/dev/tty` seam (ADR 0020 selector 2) hosting a real gosh session; the binary draws no pixels (the kernel paints the tty grid into the bound window). Docked via `image/apps.txt` (`GOTERM.ELF | Terminal | t | dock=true | v=2 | selection=primary | icon=ICONTERM`); `selection=primary` opts into the seat's primary selection (publish on select, deliver on middle-click / shift+insert). Implementation: `user/go/term`, 640x400 at 64,48 — the classic 80-column rect, deliberately (`main.go:48-55`), title "Term".

## Interface today

Observed from `user/go/term/main.go`, `menu.go`, `links.go`, `profiles.go`, `find.go` + `go-term.spec`:

- Hosts a real gosh session: `virelai/shlib`'s editor + engine own the prompt, editing, line execution, and history (same files/caps as GOSH — `/host/STARTUP.SH`, `/host/PROFILE.SH`, `SETTINGS.TXT` prompt key). Shell exit restarts by default; `exit --close` or a second Ctrl-D closes the window; `term_restart=exit` flips the default.
- Keyboard model is the shell's line editor plus front-end verbs: `?` opens the M121c chord-hint overlay (`hints n=53` rows observed), Ctrl-F the M191a scrollback find bar (`find n=` live count, hit navigation with below/above-fold markers), F10 the M190d appkit menubar (`menu.go`: Edit line / EOF / Interrupt / Paste / Chord hints / Close rows).
- OSC-8 hyperlinks: `linkTracker` mirrors the screen model just enough to know which cells carry a link, wraps them in underline, and resolves clicks (`links.go:1-49`); scrollback drops link cells rather than resolve wrong.
- Named profiles (`profiles.go`); the `term-profile` line command cycles them — the run-d3 spec leg watched the seat's rail re-theme live.
- Primary selection + OSC 52 clipboard bridging + bracketed paste; middle-click/shift+insert delivers the seat's primary selection into the line editor (`main.go:97-99`).
- Notify-on-finish (issue #3251): a long-running foreground child that exits while the tab is blurred raises a seat toast (`goterm: notify sent sleep exited st=0`), click-through refocuses the sender — the threshold is 5 s and watching earns silence.
- Type-ahead: the M98f interrupter's stash parks lines typed during a foreground child; they submit in order as each finishes.

## Coverage

`tools/gate/specs/go-term.spec` — ran locally: **FAILED (9/10 runs, 511 s)**. Run 05 (notify-on-finish: `sleep 6` focused → `sleep 7` blurred → `cd /data`) missed `serial-contains 'goterm: line cd /data'` — the third type-ahead line's submit marker never reached the serial log before the run ended on `gotabwm: notify dismiss`. The other nine legs passed: session/exec/attach, restart + `exit --close`, the WEB exec leg, seat legs 03/04, profiles/theme, the '?'/menubar leg, and the scrollback find chain (`find chain OK`).

## Observed defects

- **Run 05 flake-or-bug: stashed third command's submit marker never printed.** `sleep 6` and `sleep 7` submitted fine (their markers landed); the type-ahead's third line `cd /data` never printed `goterm: line cd /data` even though the run continued through `sleep 7`'s exit, notify paint, toast click-dismiss, and refocus. Either (a) the type-ahead drain raced the run's `--script-expect` (the run ends the moment `notify dismiss` lands — if `cd`'s submit comes after, the assert can never see it), or (b) a real type-ahead drain bug. One observation; needs a rerun to discriminate. Not filed as a bug card yet — see Evidence.

## Improvement candidates

- **The window paints no pixels of its own** — every affordance is a serial marker or a tty-cell overlay. That's the design, but the hint overlay / menubar / find bar could get the staged-art treatment (the design loop #3341) since they already have defined geometry.
- **Profile cycling is a line command (`term-profile`)** — a menubar row or a rail-level affordance would make it discoverable; today only `?`-overlay readers know it exists.
- **OSC-8 links drop on scroll** (deliberate — "never resolve to a wrong URI"), so a long link block scrolled back becomes dead text; a re-parse-on-paint could keep links live through scrollback.
- **No split-pane / second session**: one window = one gosh session; the seat's tab model is the multiplex answer, but an in-app split is the standard terminal ask.
- **`term_restart=exit` is settings-file only** — no in-app toggle verb.

## Evidence

- `just gate go-term` → `vgate go-term: FAILED (9/10 runs; see artifacts/go-term-report.txt)` — run 05 `goterm: line cd /data` absent; all other runs PASS (local run, worktree `t3-b1dbc866`, `artifacts/go-term-gate.txt`).
- `grep`/`sed` reads of `user/go/term/main.go`, `menu.go`, `links.go`, `profiles.go`, `image/apps.txt`, `site/apps/goterm.md`, `site/includes/apps/goterm.story.md`, `tools/gate/specs/go-term.spec` (run 05 choreography at `:905-975`).
- Dedupe: `gh issue list --search "terminal" --state open` → no existing audit card.
