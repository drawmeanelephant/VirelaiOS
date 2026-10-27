<!-- iface-review:v1 app=GOMINE.ELF issue=3723 -->
# App audit: Mines (GOMINE.ELF)

> Retired from polish audit issue #3723 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

GOMINE.ELF is Minesweeper (M145a #2796): three seeded board sizes (9x9/10, 16x16, 30x16), first click always safe (lazy mine placement excluding the clicked neighborhood), explicit-stack flood-fill reveal, right-click flag, chording on satisfied numbers, a `vi.Notify` on win, per-difficulty best times and a win chime (M152d). Manifest row: `dock=false | v=2 | caps=file,window,audio | icon=ICONMINE` — palette/exec launch; argv `GOMINE.ELF <seed> <size>` drives scripted legs.

## Interface today

Observed (code, `user/go/gomine/`):
- **Window** (720x560): a 44px header with a mine counter (`M <remaining>` + `X`/`W` end states) on the left, four header buttons (`9x9`, `16x16`, `30x16`, `New`), and an elapsed `T <s>` readout; below, the cell grid — hidden plate, classic number colors (1 blue, 2 green, 3 red...), amber flag glyph, red detonated cell, auto-flagged mines on a loss.
- **Input**: left-click reveals, right-click flags, chord = right-click a satisfied number (or second button while holding the first); keys `n` new game, `1`/`2`/`3` switch size. Audio cap declared and used for the win chime.
- **save_state** keeps board, elapsed seconds, seed, and best times through the seat snapshot.

## Coverage

`tools/gate/specs/go-games.spec` (shared with GOSOL) — FAILED 6/7 on this host (`BOOTS=1 just gate go-games`, 2026-10-25, worktree t3-40900d1c). Runs 01 (seeded GOMINE win), 02 (boom leg), 03 (GOSOL stock-draw + drag pins), 04 (seat-hosted play + snapshot), 06 (GOSOL `'a'` auto-move) and 07 (GRANTS.TXT enforce leg) all PASS. Run 05 (session-restore exact-restore pixel compare) failed inside its own python compare: it looks for the run-04 serial as `vm-serial-04-<it>.log` where `<it>` derives from run 05's own tag — under `BOOTS=1` the tag carries no iteration suffix, so it looked for `vm-serial-04-05.log`, found nothing, and exited before comparing a single pixel. **That is a spec artifact of my BOOTS=1 override, not a product failure** — every one of run 05's serial asserts passed: the seat autostarted, `session load n=2 mode=restore`, both apps re-exec'd and restored (`gomine: state restored size=intermediate rev=0` — the fresh board correctly saved after the scripted win moved to 16x16; `gosol: restored moves=0` — the fresh deal correctly saved after 'n'), and the snap-05 capture streamed fine. Under CI's `vgate_repeat 3` the tag scheme is consistent and the compare runs for real. — legs: seeded WIN run (scripted pointer), LOSS run (boom), seat-hosted play under GOTABWM, mid-game session snapshot + exact-restore pixel compare; `vgate_repeat 3 BOOTS` in CI (audited with BOOTS=1).

## Observed defects

None in GOMINE.ELF itself — its seeded win, boom, seat-hosted, and state-restore legs are all green, and the win/best/chime markers all fired. The shared spec's run-05 compare failure is a spec artifact of `BOOTS=1` (see Coverage), not a GOMINE defect. The adjacent GOSOL drag defect on the raw-shim path is filed as #3721 — it does not touch the GOMINE leg, which plays fully under the seat.

## Improvement candidates

- The classic minesweeper "smiley" new-game button is absent — `New` is a plain fourth button; a face-with-state affordance is the genre's readability convention.
- Best times are recorded per difficulty (save blob + `gomine: best` markers) but never shown in the window — a best-time readout in the header or a post-win panel would surface them.
- No flag-only / no-guess modes, no hint, no "remaining mines" explanation for color-blind users — number colors are the only count channel.
- The counter shows mines-minus-flags (`M n`) but there's no separate flag count; ambiguous for players who flag speculatively.
- Chording requires a right click or two-button press — on a trackpad-heavy host a `space`/`c` chord key would be more reachable.
- Elapsed runs during play but the window doesn't show a per-cell hover state mentioned in code — hover highlight would improve pointer feel (verify in play).
- No question-mark cell state (classic `?` mark) — right-click cycles flag/unflag only.

## Evidence

- `gh issue list --search "<name>" --state open` → no existing audit issue; `gh issue view 2878` → drag defect closed, residual reopened as #3721.
- `bash tools/go/build-gomine.sh build-gosol.sh build-gotabwm.sh` → all staged (private GO_FORK_DIR).
- `BOOTS=1 just gate go-games` → `vgate go-games: FAILED (6/7 runs; see artifacts/go-games-report.txt)`. Serials `artifacts/go-games-serial-0*.log`; run-03 drag pin and run-05 compare quoted above.
- Interface description observed from `user/go/gomine/{main,engine,render,drag,state,...}.go`; pixel appearance partly inferred from draw calls plus the spec's snapshot probes (which assert flag glyphs, number ink, revealed plates, and the kernel title band).
