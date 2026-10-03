# M91d — Land default-desktop workflow readiness and the acceptance fixture

> **Held, not filed (grounding 2026-10-03, `7ead82ee`).** `tools/gate/specs/go-wm-hid.spec` overlaps the active #1857 claim (`droid/coach-through-ios-app`), and #1914 reports uncommitted corrections to it on that branch. The scope waits on held M91-R1. #1914 assigns `docs/status.md` to M91d; this draft moves it to #1857, which needs the owner's agreement. File after #1857's claim is narrowed.

- **Parent index:** M91, [#1914](https://github.com/drawmeanelephant/VirelaiOS/issues/1914).
- **Owner:** Sol 6.1, Go desktop/workflow implementer; owner review before merge.
- **Depends on:** M91-R1; M91a/b/c must be ready before integrated acceptance. Go-only tests can run earlier against existing contracts.
- **Scope finalized by R1:** final scope set by R1 output. Representative app changes require an observed defect, not an app catalog rewrite.
- **Size / scheduling:** 8–12 agent hours, deferred beyond week one.

## Deliverable

Land complete **automated readiness** for the default first workspace and Apps → Terminal → representative editor workflow, including source-fresh binaries, focus/modal isolation, honest unavailable-app states, one-gesture-one-launch and receipt persistence. Reuse the M87 implementation already merged in PR #1861; fix only reproduced Go-seat/representative-app defects and deliver a fixture the human can use without hidden workarounds. Separate this complete automation/fixture deliverable from class-C acceptance: existing #1857, not an agent or new acceptance card, performs the physical journey after all readiness gates pass.

## Exclusive ownership

- `user/go/gotabwm/*`.
- `user/go/term/*`, `user/go/edit/*`, `user/go/note/*`: reserve representative apps; edit only the app(s) with an observed workflow defect.
- `tools/gate/specs/go-wm-hid.spec`, `tools/gate/specs/go-wm-default.spec`.
- Small pinned workflow fixtures under proposed `tests/fixtures/desktop/m91/*`.
- No session/host, input, compositor, app-manifest, shared SDK, README/testing/hardware/status or R1 ADR edits. In particular, **only human #1857 owns `docs/status.md`**.

## Verification

- From `user/go`, run `go test ./gotabwm ./vi ./term ./edit ./note`; focused no-leak, menu paint/hit, unavailable/missing binary, gesture, focus and persistence tests; race/vet checks for changed Go packages as applicable.
- Extend `go-wm-hid.spec` without deleting failing cases; run its **full current set** and `just gate go-wm-default`. Check `go-wm-seat` and `go-dogfood` read-only; reproduce and attribute failures rather than treating an older count as a current pass.
- Include a default-boot, source-fresh fixture. `go-wm-hid` historically sets `wm=none` and execs the Go seat explicitly, so its pass alone cannot prove the normal boot default.
- With an existing tab: pointer-only Apps/Terminal launch; actual editor use/focus switch; guest Terminal write/read of a uniquely named `/host` receipt; independent host byte comparison after stop and again after relaunch.
- Prove readable label pixels and available/unavailable states; filter/Escape/outside dismissal leaves old-app bytes/input unchanged; header/padding never execs; one gesture cannot double-launch or click through.
- Evidence: standard `artifacts/go-wm-hid-*`, `go-wm-default-*` reports/scanouts and independent guest/host fixture bytes under `artifacts/m91-workflow/`. These are class A/B, **not** #1857 human acceptance.
- Run inventory/coordination checks and `git diff --check`. No verification shell gate or catalog-wide app rewrite.

## Closes

The PR merging the **whole readiness fixture and passing default/workflow automation** closes this issue. It leaves #1857, #1914 and #1856 open until the real human session. Do not put human acceptance into this agent card and then leave it open as a “prerequisite slice.”

## Sources read

#1914, M91d and milestone acceptance; #1857, landed implementation and remaining human checklist; `tools/session.sh:108-175`; `tools/tests/test_session_apps.py:51-99`; `tools/gate/specs/go-wm-hid.spec:5-15,1116-1325`; `docs/status.md:144`.
