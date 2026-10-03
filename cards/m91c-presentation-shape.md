# M91c — Make the cursor recognizable and enforce completed-frame presentation ownership

- **Parent index:** M91, [#1914](https://github.com/drawmeanelephant/VirelaiOS/issues/1914).
- **Owner:** Sol 6.1, compositor implementer; owner review before merge.
- **Depends on:** M91-R1 and approved #1857 claim-path handoff. Existing canonical pointer fixtures permit independent implementation from M91b.
- **Scope finalized by R1:** final scope set by R1 output. Adopt only reviewed ownership/cursor corrections with reproduced regressions.
- **Size / scheduling:** 12–16 agent hours, deferred beyond week one.

## Deliverable

Land a visible recognizable guest cursor with coherent hotspot/motion/old-position restoration and a single completed-frame presenter while the seat owns scanout. Prevent competing kernel flushes and intermediate blue/chrome frames, retaining shim/fallback and seat teardown behavior. The fix must not disable native input, switch seats, slow everything with arbitrary sleeps, replace the compositor or hide failures. This card owns presentation correctness and recorded frame evidence; human ordinary-use acceptance remains #1857.

## Exclusive ownership

- `kernel/src/driving_award.zig`, `kernel/src/wm_server.zig`, including `wm_server.zig`'s embedded tests.
- `kernel/tests/driving_award_test.zig`.
- `tools/gate/specs/go-wm-console-ink.spec`, `tools/gate/specs/live-sb4-damage-tracking.spec`, `tools/gate/specs/live-wm-pacing.spec`.
- No input/host/Go, hardware/status docs, `go-wm-hid.spec` or R1 ADR edits.

## Verification

- `zig build test`: cursor mask/hotspot/clipping and old/new damage tests; ownership, completed-frame sequence, clean-scene no-flush, bind/unbind, seat death and shim recovery regressions.
- Extend named existing presentation specs and run `just gate go-wm-console-ink`, `just gate live-sb4-damage-tracking`, `just gate live-wm-pacing`. Preserve the pacing instrument/established limits; measure before proposing cadence changes.
- Save frame/ownership traces and guest scanout sequences during a 60-second pointer/typing/menu/focus fixture interval. Compare before/after and reject intermediate blue/chrome frames or competing presenters. R1 must choose a capture/trace method capable of detecting intermediate presents; two still screenshots cannot prove absence of flash.
- Exercise fallback/shim and seat death read-only through `go-wm-default`/`go-wm-seat`; do not treat custom-virtio automation as the later physical journey.
- Evidence: failing-before/passing-after tests, standard named gate reports, presented-frame sequence and consented fixture scanouts under `artifacts/m91-presentation/`. No host desktop/session recordings or committed logs.
- Run `just verify-portable`, inventory/coordination checks and `git diff --check`.

## Closes

The PR merging **cursor and exclusive completed-frame ownership with all regressions/gates passing** closes this issue. A prettier cursor alone or a suppressed-blue-frame workaround does not qualify. M91 and #1857 remain open for the integrated physical journey.

## Sources read

#1914, cursor/flash reports and M91c; `kernel/src/driving_award.zig:99-101,3497-3525,4636-4649,4754-4777`; `kernel/src/wm_server.zig:867-913,1089-1129`; `tools/gate/specs/go-wm-console-ink.spec:30-44`; `tools/gate/specs/live-wm-pacing.spec:10-17,95-120`.
