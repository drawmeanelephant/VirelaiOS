<!-- iface-review:v1 app=GOCALC.ELF issue=3701 -->
# App audit: 64-bit Calc (GOCALC.ELF)

> Retired from polish audit issue #3701 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The dock calculator — fixed-point decimal arithmetic plus a programmer mode (HEX/DEC/OCT/BIN, bitwise ops, shifts). Launches from the dock via `image/apps.txt` (`GOCALC.ELF | 64-bit Calc | c | dock=true | v=2 | icon=ICONCALC`); no `caps=` so it runs unenforced, and it declares no `opens=` types. Implementation: `user/go/calc` (tabapp client, 512x384 at 32,32, title "Calc").

## Interface today

Observed from `user/go/calc/main.go` + `go-calc.spec` run output:

- Two on-screen pads driven by the same engine calls the keyboard uses (`main.go:248` `key()`, `main.go:304` `press()`): decimal pad is a 5x4 grid (`C +/- % /`, digits, `P` toggles programmer mode); programmer pad is a 6x6 grid (base row `HEX DEC OCT BIN STD CLR`, hex digits, `AND OR XOR NOT << >>`, digits + ops).
- Keyboard model: digits, `.`, `%`, `n`/`N` (negate), `p`/`P` (mode toggle), `t`/`T` (copy tape), `=`/Enter (eval), `c`/`C`/Escape (clear), Backspace (edit entry); in programmer mode `a`-`f`/`A`-`F`, `& | ^ ~ < >` (`main.go:248-302`).
- A history strip above the entry shows the live line plus one previous tape row (`main.go:13-15`, `histRowA/histRowB`); the tape itself holds up to 16 rendered lines (`tapeMax`, `main.go:82`) and rides the seat's appver-1 save/restore envelope (verified by spec run 06).
- Pointer clicks hit-test pad buttons with hover/pressed states (`main.go:221-245`).
- Publishes the M197d a11y outline (`a11y.go`): window + focused display textfield + one button node per live pad cell.
- `t` publishes the tape as text/plain on the kernel clipboard. When launched by the seat (uid_user, no CAP_CLIPBOARD) the copy is refused; the refusal is a serial marker only (`main.go` `doCopyTape`), nothing appears in-window.

## Coverage

`tools/gate/specs/go-calc.spec` — ran locally: **PASS (7/7 runs, 453 s)**. Legs: integer regression (12-3=9), decimal (1.5/4=0.375, 2+3.5=5.5), programmer keyboard (FF-1=FE), pointer pad clicks under the bare shim (FE AND 0F), M104h pixel probes (rounded pad plates, border stroke, digit ink), appver-1 state save/restore across tab close+re-exec, and the capable-`u0` tape copy (`clip: HEX FF-1=FE` read back by the monitor).

## Observed defects

None. All 7 spec runs passed; serial logs show no `[EXC] parking:` or crash statuses.

## Improvement candidates

- **Radix row has no keyboard path.** `press()` maps `HEX`/`DEC`/`OCT`/`BIN`/`STD`/`CLR` pad clicks (`main.go:328-336`), but `key()` (`main.go:248-302`) binds nothing to `doBase` — `a`-`f` are hex digits, so a keyboard-only user cannot switch base without reaching for the pointer. A chord set (e.g. a Ctrl spelling or dedicated runes) would close the gap.
- **Tape-copy refusal is invisible in-window.** Under the seat, `t` prints `gocalc: tape copy refused rc=` to serial only (`main.go` `doCopyTape`); the user sees nothing change. A status line / toast in the display strip would make the capability boundary legible. (Same posture GOFILES' clip-sys keeps — consistent, but still silent.)
- **Tape is 16 deep but only 1 row is visible.** The history strip shows the live line + previous entry; the other 14 tape rows exist only for copy/restore. A scrollback or taller tape plate is polish the design loop (#3341) could pick up.
- **`n` for negate and `t` for tape are undocumented in-window.** No hint row; the pad shows only `+/-` and `P` labels. The a11y outline names cells, but a sighted keyboard user has no in-app discoverability — a one-line hint strip (like GOFILES' `render.go:429` hint line) would help.

## Evidence

- `just gate go-calc` → `vgate go-calc: PASS (7/7 runs)` (local run; `artifacts/go-calc-gate.txt` in worktree `t3-b1dbc866`).
- `grep`/`sed` reads of `user/go/calc/main.go`, `user/go/calc/a11y.go`, `image/apps.txt`, `site/apps/gocalc.md`, `site/includes/apps/gocalc.story.md`.
- Dedupe: `gh issue list --search "calc" --state open` → no existing audit card.
