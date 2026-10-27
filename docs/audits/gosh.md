<!-- iface-review:v1 app=GOSH.ELF issue=3715 -->
# App audit: Shell (GOSH.ELF)

> Retired from polish audit issue #3715 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The system shell: exec, line editing, history, jobs/fg, and the pipes / redirection / env / variables scripting subset. One `virelai/shlib` engine backs three front-ends — the full-viewport windowed tab on `/dev/tty` (default), `GOSH.ELF serial` on the console the kernel monitor hands over, and `GOSH.ELF net` with its HMAC/ed25519 challenge-response handshake; `GOSH.ELF -c LINE` is the headless composable form. Manifest row (image/apps.txt:45): `GOSH.ELF | Shell | s | dock=false | v=2 | caps=file,process,term | icon=ICONGOSH`. Note: `dock=` is parsed but paints nowhere on the Go seat (user/go/gotabwm/apps.go:22 — the rail is the running set, M190c #3238), so "undocked" costs GOSH nothing today; it launches via the launcher/palette catalogue, `exec GOSH.ELF`, and the first-boot autostart (the seat opens GOSH beside GOWELCOME when SESSION.TABS is absent).

## Interface today

Observed from `user/go/sh`, `user/go/shlib`, and the go-sh/go-shell-polish spec output:

- Full-viewport terminal tab (512x384 natural); the tab title follows the cwd (`Sh — <cwd>`, main.go:303-331, M98h).
- `gosh> ` prompt; `/host/STARTUP.SH` then `/host/PROFILE.SH` startup contract (≤2048 B each, silent when missing).
- Line editor (shlib/edit.go): history ring + `history` verb, reverse-i-search, session aliases, dir stack (pushd/popd/dirs, `cd -`), DECSET-2004 bracketed paste (4 KiB bound, M154a).
- Builtins (shlib/builtins.go:52-77): echo cat pwd cd pushd popd dirs open env printenv set unset export read jobs fg kill history help exit monitor suspend say sleep clear log date time whoami id chmod secrets true false source . break continue alias unalias — plus the tools group (tee, xargs, tr, basename, dirname, …) and the externals catalog rows (exec GOFETCH.ELF, GOPING.ELF, GONETSTAT.ELF, GODNS.ELF, GOTRACEROUTE.ELF, GOTGIT.ELF, QJS.BIN).
- `help` prints the same catalog GOHELP browses (single-sourced from `shlib.HelpRows()` — 62 rows observed in the go-help run).
- appver-1 state envelope: cwd round-trips the seat's save/restore (go-sh run 08 asserts `restored cwd=` plus title re-sync).
- The manifest row carries no `selection=primary` — unlike GOTERM/GOEDIT, GOSH does not publish the seat's primary selection.

## Coverage

- `go-sh.spec` — 9 runs, **7/9 PASS** on this host (artifacts/go-sh-report.txt): front-ends, startup contract, editor, jobs/fg, the M201b session-share toolset staging (`session.sh --provision-only` + monitor-side exec of all nine tools), serial/net faces, byte-exact share writes. Two losses, both the input/marker-timing class: run 01 missed only the final `gosh OK` exit marker (every share-equals and the log/help/job fixtures verified byte-exact) and run 08's typed `cd /host/GOSHCWD` never reached the focused tab (`state ask/save` then fired on the un-changed cwd) — the same focus-race class the go-settings run-02 comment documents.
- `go-shell-polish.spec` — 2 runs, **1/2 PASS** (artifacts/go-shell-polish-report.txt): run 01 green (line echo/history, `COUNTER.BIN &` job→kill 137, `\?` prompt repaint, tee share-equals); run 02 lost its injected `cd ./M98DIR` edit-resubmit line — 7 leg markers missing (GOEDIT release, `gosh: line`, boot end marker), the same input-routing flake as go-sh run 08 / go-settings run 02 on this host.
- Also exercised by `live-polish-sweep.spec` (first-boot pair + close) and `go-dogfood.spec`.

## Observed defects

None in the app. Both failed legs are the input-routing/marker-timing flake class observed across this host's sweep (three other specs lost legs the same way under concurrent VM load); every GOSH-side assert that did fire passed, including the byte-exact file checks.

## Improvement candidates

- `selection=primary` opt-in (image/apps.txt:45): GOTERM and GOEDIT already publish to the seat's primary selection; GOSH doesn't — middle-click/shift+insert paste into the daily shell is the conspicuous gap for a terminal app.
- `help` verb ↔ GOHELP bridge: `help` prints the flat text catalog; `help echo` could offer to open GOHELP's detail page (or GOSH could exec `GOHELP.ELF <cmd>` if an argv deep-link lands — see the GOHELP audit).
- Prompt customization is only "edit PROFILE.SH": a documented `prompt=` settings row or expanded `\w`/`\u` tokens would polish the daily surface.
- Drag-drop a file onto the shell tab inserts its quoted path — natural fit with the M124a drop-target machinery GOVIEW already rides.
- A pinned dock surface (M190c #3238) would give the undocked shell a permanent slot.

## Evidence

- `just gate go-sh` — FAIL 7/9 (artifacts/go-sh-gate.txt, go-sh-report.txt)
- `just gate go-shell-polish` — FAIL 1/2 (artifacts/go-shell-polish-gate.txt, go-shell-polish-report.txt)
- `image/apps.txt:45`; `user/go/sh/main.go`, `user/go/sh/host.go`, `user/go/shlib/builtins.go`, `user/go/shlib/edit.go`; `site/apps/gosh.md` + `site/includes/apps/gosh.story.md`; `docs/component-registry.md:74`; `user/go/gotabwm/apps.go:22-25`.
