<!-- iface-review:v1 app=GOSOL.ELF issue=3724 -->
# App audit: Solitaire (GOSOL.ELF)

> Retired from polish audit issue #3724 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

GOSOL.ELF is Klondike solitaire (M145b #2797, M152d #2918): draw-1 or draw-3 seeded deals, in-surface pointer drag with drop validation and snap-back, double-click to foundation, a 32-step undo ring, `'a'` single-step auto-move, session stats (played/won/streak/best), win chime + `vi.Notify`, and a save_state blob keeping the whole table. Manifest row: `dock=false | v=2 | caps=file,window,audio | icon=ICONSOL`. Argv: `--seed=N`, `--draw=1|3`.

## Interface today

Observed (code, `user/go/gosol/`):
- **Window** (512x384): stock at top-left, waste fan beside it (3-card spread in draw-3), seven tableau piles, four foundations; cards render face (0xf6f3ec) with rounded corners, suit glyphs (bitmask-rendered hearts/diamonds/clubs/spades in `render.go`), accent/edge tokens from the theme — all paints go out `draw.Opaque` (the M145c fix: `0x00RRGGBB` tokens previously blended to nothing — a black table).
- **Input**: pointer drag (validated drop, snap-back on illegal), double-click sends to foundation; keys `n` reseed fresh deal, `d` toggle draw-1/draw-3, `u` and seat ctrl+z undo, `a` one greedy auto-move step (foundation first, then waste→tableau, then exposing moves). Stats print via `gosol: stats` markers.
- Colors went through the M145c opaque fix; the table previously rendered black.

## Coverage

`tools/gate/specs/go-games.spec` (shared with GOMINE) — FAILED 6/7 on this host (`BOOTS=1 just gate go-games`, 2026-10-25, worktree t3-40900d1c). Runs 01 (seeded GOMINE win), 02 (boom leg), 03 (GOSOL stock-draw + drag pins), 04 (seat-hosted play + snapshot), 06 (GOSOL `'a'` auto-move) and 07 (GRANTS.TXT enforce leg) all PASS. Run 05 (session-restore exact-restore pixel compare) failed inside its own python compare: it looks for the run-04 serial as `vm-serial-04-<it>.log` where `<it>` derives from run 05's own tag — under `BOOTS=1` the tag carries no iteration suffix, so it looked for `vm-serial-04-05.log`, found nothing, and exited before comparing a single pixel. **That is a spec artifact of my BOOTS=1 override, not a product failure** — every one of run 05's serial asserts passed: the seat autostarted, `session load n=2 mode=restore`, both apps re-exec'd and restored (`gomine: state restored size=intermediate rev=0` — the fresh board correctly saved after the scripted win moved to 16x16; `gosol: restored moves=0` — the fresh deal correctly saved after 'n'), and the snap-05 capture streamed fine. Under CI's `vgate_repeat 3` the tag scheme is consistent and the compare runs for real. — legs: boot + stock-draw run under the bare shim, seat-hosted keyboard play under GOTABWM, session snapshot/restore pixel compare; `vgate_repeat 3 BOOTS` in CI (audited with BOOTS=1).

## Observed defects

- **Drag unresolvable on the raw-shim path** — run 03's `serial-absent 'gosol: move tab0->tab5'` / `serial-absent 'gosol: drop bad'` pins held: press/move/release produces neither marker because `pointer()` drops `EvMouseUp` when `ev.Flags&vi.BtnLeft == 0` (`user/go/gosol/main.go:228-231`) and the raw kernel path still ships flags=0 on release. Filed as **#3721** (residual of closed #2878 — the seat path works; run 04's seat-hosted clicks reach hosted apps fine).
- The shared spec's run-05 compare failure is a `BOOTS=1` tag-naming artifact (see Coverage), not a GOSOL defect — its restore asserts passed.

## Improvement candidates

- The raw-shim drag defect (below) aside: no drag ghost/lift styling is described beyond the card flying — a subtle shadow or lift offset would sell the drag.
- No undo/redo affordance visible — `u`/ctrl+z is keys-only; a small toolbar (New / Draw toggle / Undo / Auto) would surface the verbs that today are folklore.
- Stats exist but are serial-marker only — no wins/streak/best display in the window; a post-win panel or status strip would surface them.
- `'a'` auto-move steps one move per press and can't run to completion — a "finish the game" autoplay once all cards are face-up (classic solitaire endgame) is missing.
- No card-back art selection or table felt color choice; the single theme Surface tone is the whole look.
- No scoring beyond win/streak stats — no moves/time display in-window (`gosol: win moves=` is serial-only).
- Double-click-to-foundation is invisible affordance — the `'a'` hint exists nowhere on screen.

## Evidence

- `gh issue list --search "<name>" --state open` → no existing audit issue; `gh issue view 2878` → drag defect closed, residual reopened as #3721.
- `bash tools/go/build-gomine.sh build-gosol.sh build-gotabwm.sh` → all staged (private GO_FORK_DIR).
- `BOOTS=1 just gate go-games` → `vgate go-games: FAILED (6/7 runs; see artifacts/go-games-report.txt)`. Serials `artifacts/go-games-serial-0*.log`; run-03 drag pin and run-05 compare quoted above.
- Interface description observed from `user/go/gosol/{main,engine,render,drag,state,...}.go`; pixel appearance partly inferred from draw calls plus the spec's snapshot probes (which assert flag glyphs, number ink, revealed plates, and the kernel title band).
