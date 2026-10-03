# M91d — Land default-desktop workflow readiness and the acceptance fixture

- **Parent index:** M91, [#1914](https://github.com/drawmeanelephant/VirelaiOS/issues/1914).
- **Owner:** Sol 6.1, Go desktop/workflow implementer; owner review before merge.
- **Depends on:** M91a/b/c must be ready before integrated acceptance. Go-only tests can run earlier against existing contracts.
- **Starting material:** the coach slice for `tools/gate/specs/go-wm-hid.spec`. It comes from the uncommitted `droid/coach-through-ios-app` work on `fc21e531`, preserved by the owner as a local patch snapshot (`artifacts/m91-coach-handoff/`, sha256 `767ca34384a9ca4a3842362d764cf48b3c121cbbb2cf814f47eeba70f3446c32`), and applies cleanly to `7ead82ee`. It is an unreviewed draft (#1914). #1914 reports the full current gate at 10/13 **on that draft**, with runs 01, 02 and 09 failing; that is not a main baseline.
- **Size / scheduling:** 8–12 agent hours, deferred beyond week one.

## Deliverable

Land complete **automated readiness** for the default first workspace and Apps → Terminal → representative editor workflow, including source-fresh binaries, focus/modal isolation, honest unavailable-app states, one-gesture-one-launch and receipt persistence. Reuse the M87 implementation already merged in PR #1861; fix only reproduced Go-seat/representative-app defects and deliver a fixture the human can use without hidden workarounds. Representative app changes require an observed defect, not an app catalog rewrite. Separate this complete automation/fixture deliverable from class-C acceptance: #1914's human milestone acceptance, not an agent or a new card, performs the physical journey after all readiness gates pass.

## Exclusive ownership

- `user/go/gotabwm/*`.
- `user/go/term/*`, `user/go/edit/*`, `user/go/note/*`: reserve representative apps; edit only the app(s) with an observed workflow defect.
- `tools/gate/specs/go-wm-hid.spec`, `tools/gate/specs/go-wm-default.spec`.
- Small pinned workflow fixtures under proposed `tests/fixtures/desktop/m91/*`.
- No session/host, input, compositor, app-manifest, shared SDK or README/testing/hardware/status edits. No card declares `docs/status.md`; #1914's final status pass edits the M87 and M91 rows.

## Verification

- From `user/go`, run `go test ./gotabwm ./vi ./term ./edit ./note`; focused no-leak, menu paint/hit, unavailable/missing binary, gesture, focus and persistence tests; race/vet checks for changed Go packages as applicable.
- Extend `go-wm-hid.spec` without deleting failing cases; run its **full current set** and `just gate go-wm-default`. Check `go-wm-seat` and `go-dogfood` read-only; reproduce and attribute failures rather than treating an older count as a current pass. This includes the `go-dogfood` wide-rune blank-cell snapshot failure that #1857 recorded as reproducing on its unchanged baseline.
- Include a default-boot, source-fresh fixture. `go-wm-hid` historically sets `wm=none` and execs the Go seat explicitly, so its pass alone cannot prove the normal boot default.
- With an existing tab: pointer-only Apps/Terminal launch; actual editor use/focus switch; guest Terminal write/read of a uniquely named `/host` receipt; independent host byte comparison after stop and again after relaunch.
- Prove readable label pixels and available/unavailable states; filter/Escape/outside dismissal leaves old-app bytes/input unchanged; header/padding never execs; one gesture cannot double-launch or click through.
- Evidence: standard `artifacts/go-wm-hid-*`, `go-wm-default-*` reports/scanouts and independent guest/host fixture bytes under `artifacts/m91-workflow/`. These are class A/B, **not** the human acceptance.
- Run inventory/coordination checks and `git diff --check`. No verification shell gate or catalog-wide app rewrite.

## Closes

The PR merging the **whole readiness fixture and passing default/workflow automation** closes this issue. It leaves #1914 open until the real human session. Do not put human acceptance into this agent card and then leave it open as a “prerequisite slice.”

## Sources read

#1914, M91d and milestone acceptance; #1857, landed implementation and its human checklist; `tools/session.sh:108-175`; `tools/tests/test_session_apps.py:51-99`; `tools/gate/specs/go-wm-hid.spec:5-15,1116-1325`; `docs/status.md:144`; the coach snapshot's file list and hunk headers.
