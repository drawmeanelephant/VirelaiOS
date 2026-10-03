# M91c — Make the cursor recognizable and enforce completed-frame presentation ownership

- **Parent index:** M91, [#1914](https://github.com/drawmeanelephant/VirelaiOS/issues/1914).
- **Owner:** Sol 6.1, compositor implementer; owner review before merge.
- **Depends on:** none. Existing canonical pointer fixtures permit implementation independent of M91b.
- **Starting material:** the coach slice for `kernel/src/driving_award.zig` and `kernel/tests/driving_award_test.zig`. It comes from the uncommitted `droid/coach-through-ios-app` work on `fc21e531`, preserved by the owner as the local, unpushed branch `droid/m91-coach-handoff` (commit `1f083d23`; patch sha256 `767ca34384a9ca4a3842362d764cf48b3c121cbbb2cf814f47eeba70f3446c32`), which every worktree of the repository can read. It applies cleanly to `7ead82ee`. Take this card's files with `git diff fc21e531 droid/m91-coach-handoff -- <paths> | git apply`; checking the paths out from the branch would revert later main changes. It is an unreviewed draft (#1914): adopt only ownership/cursor corrections whose regressions you reproduce.
- **Size / scheduling:** 12–16 agent hours, deferred beyond week one.

## Deliverable

Land a visible recognizable guest cursor with coherent hotspot/motion/old-position restoration and a single completed-frame presenter while the seat owns scanout. Prevent competing kernel flushes and intermediate blue/chrome frames, retaining shim/fallback and seat teardown behavior. The fix must not disable native input, switch seats, slow everything with arbitrary sleeps, replace the compositor or hide failures. This card owns presentation correctness and recorded frame evidence; human ordinary-use acceptance is #1914's human session.

## Exclusive ownership

- `kernel/src/driving_award.zig`, `kernel/src/wm_server.zig`, including `wm_server.zig`'s embedded tests.
- `kernel/tests/driving_award_test.zig`.
- `tools/gate/specs/go-wm-console-ink.spec`, `tools/gate/specs/live-sb4-damage-tracking.spec`, `tools/gate/specs/live-wm-pacing.spec`.
- No input/host/Go, hardware/status docs or `go-wm-hid.spec` edits.

## Verification

- `zig build test`: cursor mask/hotspot/clipping and old/new damage tests; ownership, completed-frame sequence, clean-scene no-flush, bind/unbind, seat death and shim recovery regressions.
- Extend named existing presentation specs and run `just gate go-wm-console-ink`, `just gate live-sb4-damage-tracking`, `just gate live-wm-pacing`. Preserve the pacing instrument/established limits; measure before proposing cadence changes.
- Before coding, choose a capture/trace method that can detect intermediate presents; two still screenshots cannot prove absence of flash. Then save frame/ownership traces and guest scanout sequences during a 60-second pointer/typing/menu/focus fixture interval. Compare before/after and reject intermediate blue/chrome frames or competing presenters (allowed: 0).
- Exercise fallback/shim and seat death read-only through `go-wm-default`/`go-wm-seat`; do not treat custom-virtio automation as the later physical journey.
- Evidence: failing-before/passing-after tests, standard named gate reports, presented-frame sequence and consented fixture scanouts under `artifacts/m91-presentation/`. No host desktop/session recordings or committed logs.
- Run `just verify-portable`, inventory/coordination checks and `git diff --check`.

## Closes

The PR merging **cursor and exclusive completed-frame ownership with all regressions/gates passing** closes this issue. A prettier cursor alone or a suppressed-blue-frame workaround does not qualify. #1914 remains open for the integrated physical journey.

## Sources read

#1914, cursor/flash reports and M91c; `kernel/src/driving_award.zig:99-101,3497-3525,4636-4649,4754-4777,5278-5289` (the cursor is an 8×8 block with no hotspot field); `kernel/src/wm_server.zig:438-469,482-500,600-620,867-913,1089-1129`; `tools/gate/specs/go-wm-console-ink.spec:30-44,101-114,175-207`; `tools/gate/specs/live-wm-pacing.spec:10-17,95-120`; the coach snapshot's file list and hunk headers.
