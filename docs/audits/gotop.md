<!-- iface-review:v1 app=GOTOP.ELF issue=3704 -->
# App audit: Top (GOTOP.ELF)

> Retired from polish audit issue #3704 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The process monitor — SYSTEM MONITOR's remit folded into one app: a live process list with click-to-kill plus five more kernel-snapshot tabs. Manifest row (image/apps.txt:48): `GOTOP.ELF | Top | p | dock=true | v=2 | icon=ICONTOP` — the manifest's only docked audit-row, though `dock=` currently parses without painting on the Go seat (user/go/gotabwm/apps.go:22, M190c #3238). Launches via the catalogue, `exec GOTOP.ELF`, or the session-share tooling.

## Interface today

Observed from `user/go/top` and the go-top run output:

- 512x384 window at TOP.BIN's presentation rect (40,40); one compressed toolbar row carrying nine buttons (Refresh, Kill, Auto, Procs, Net, Agent, GPU, Trust, Mounts) + the filter field + a machine summary.
- Six tabs: procs (versioned `sys_procs` rows — real scheduler tick-share CPU% and resident MEM, ADR 0047), network counters, agent audit tail, GPU compute counters (handle-free `sys_gpucomp` STATS), Trust grant decisions + denial ring, and the mounts table. Each tab is a one-shot snapshot; nothing append-only lives here (docs/diagnostics.md:17).
- Keys: `1-6` sort columns, `k` click-to-kill the selected pid (slot-29 armed → kernel converts to status 137; refusals printed, never hidden), `/` or `f` opens the text filter (live name-substring narrow, Enter commits, Esc clears — M71g brought it back on widgets.TextInput), `r` refresh, `a` auto (~1 s), `p/n/c/g/t/m` tab jumps, `u` agent principal filter; the header line shows `^COL`/`vCOL` sort markers; a profile line samples machine identity on startup/refresh (never on the 1 Hz tick).
- appver-1 envelope persists sort column + direction + active tab across the seat's save/restore (M192d #3249).

## Coverage

- `go-top.spec` — 11 runs, **10/11 PASS** on this host (artifacts/go-top-gate.txt): kill-proof (listed COUNTER.BIN pid → armed → status 137, zero post-exit markers), `/`-filter leg (narrowed rows all COUNTER.BIN, full table restored), real CPU/MEM figures against GOSCALE (`busy cpu_ticks=16, gotop mem=6721536B, machine cpux10=500`), GPUTRIANGLE compute counters, trust tab cross-proof against kernel denial receipts, mounts free-space, and the sort+tab appver state round-trip all green. Run 08 (the profile leg) failed: the guest connected vsock port 1209 but the ctl channel answered `bad_greeting` — `top: profile refused bad_greeting` printed honestly, so the named-refusal path itself worked; the expected `vsock-ctl: authed` / `top: profile name=prof…` never arrived within the 150 s window. Reads as a vsock handshake hiccup under concurrent VM load on this host, not a GOTOP surface defect — a rerun on a quiet machine would confirm.

## Observed defects

None in GOTOP itself. Gate-level observation (inferred, not diagnosed): run 08's `vsock-ctl` greeting failed `bad_greeting` once — the app printed its named refusal correctly; likely harness/timing, worth a rerun note rather than a bug card.

## Improvement candidates

- The toolbar row has been compressed three times to fit new tabs (main.go:172-178 comments) — a second row or a real tab-strip widget (GOSET's tabStrip shape) would scale better than narrower buttons.
- Machine summary is one `cpux10`/mem line — per-core bars would suit the SMP scheduler the figures come from; no history/sparkline exists (point-in-time by design, but a short CPU% trail per row is the classic monitor affordance).
- Kill is instant — no confirm step; either a two-click arm or a status hint naming what `k` will do (the footer says it, the row doesn't) would make the destructive verb safer to discover.
- Sort indicator is an ASCII `^`/`v` on the header text — a drawn glyph on the sorted column header would read better (design-loop #3341 material).
- Filter is name-substring only — pid/state predicates (`state=running`, `mem>…`) would suit a monitor that already sorts on those columns.
- Mounts/network/agent tabs are tables only; a per-row action (unmount? open the audit dir in GOFILES?) is unexplored.

## Evidence

- `just gate go-top` — PASS 11/11 (artifacts/go-top-gate.txt, go-top-report.txt).
- `image/apps.txt:48`; `user/go/top/{main,table,netview,gpuview,trustview,mountview,auditview}.go`; `site/apps/gotop.md` + `site/includes/apps/gotop.story.md`; `docs/component-registry.md:77`; `docs/diagnostics.md:17`; `user/go/gotabwm/apps.go:22-25`.
