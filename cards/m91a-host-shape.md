# M91a — Land the supported macOS host front door and VM lifecycle

> **Held, not filed (grounding 2026-10-03, `7ead82ee`).** Every code path here overlaps the active #1857 claim (`droid/coach-through-ios-app`): `host/vm-runner/Package.swift`, `Sources/VMRunner/main.swift`, `Sources/VMAppKit/NativeInput.swift`, `Tests/VMRunnerTests/NativeInputTests.swift`, `tools/session.sh` and `tools/tests/test_session_apps.py`. #1914 reports uncommitted corrections to them on that branch. The scope also waits on held M91-R1. File after #1857's claim is narrowed, with the host-contract choice restored as this card's first deliverable, as #1914 specifies.

- **Parent index:** M91, [#1914](https://github.com/drawmeanelephant/VirelaiOS/issues/1914).
- **Owner:** Sol 6.1, host implementer; owner review before merge.
- **Depends on:** M91-R1 and approved #1857 claim-path handoff.
- **Scope finalized by R1:** final scope set by R1 output. Packaging and launch behavior are chosen in R1, not by this draft.
- **Size / scheduling:** 12–16 agent hours, deferred beyond week one.

## Deliverable

Land the entire selected macOS host front door: distinct app/window identity, native event dispatch, activation/first responder, clear startup/failure state and predictable close/quit/relaunch with VM cleanup. Retain the CLI gate entrypoint and the already-landed source-fresh manifest staging, preserving fixture documents/settings. Do not reimplement M87's launcher/staging or declare inherited Factory/Terminal identity fixed from API return values. The complete host lifecycle is this card's acceptance; the physical guest journey remains exclusively human #1857.

## Exclusive ownership

- `host/vm-runner/*`, excluding generated `.build/`: host modules, package, app resources and Swift tests. Any new `Sources/VMAppKit/` is proposed, not present at baseline.
- `tools/session.sh`, `tools/tests/test_session_apps.py`.
- `README.md`, `docs/testing.md`: supported local launch instructions only; preserve accepted remote material.
- No kernel, guest seat/app, hardware/status, R1 ADR or gate-spec edits.

## Verification

- Run `swift test --package-path host/vm-runner` and the SPIKE release build used by session: `swift build --package-path host/vm-runner --configuration release -Xswiftc -DSPIKE`.
- Extend staging/lifecycle/identity tests, then run `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools/tests -p test_session_apps.py` and `bash -n tools/session.sh`. Preserve existing missing-builder, source-freshness and no-document/settings-overwrite behavior.
- In a consented isolated fixture, observe supported clean launch, key/first-responder state, window identity, close/quit and two launch/relaunch cycles. Record owned VM PIDs before/after, stop/cleanup outcomes and failures. A reported activation success or hidden window is insufficient.
- Run existing `live-input` and `go-wm-default` gates read-only for compatibility; an input-layer failure routes to M91b and cannot be narrated as a regression pass. No new verification shell gate.
- Evidence: Swift/staging/build output and fixture host lifecycle observations under `artifacts/m91-host/`, standard gate reports, no arbitrary host desktop capture.
- Run inventory/coordination checks before the future PR and `git diff --check`.

## Closes

The PR merging the complete supported host behavior and passing host/lifecycle verification closes this issue. A packaging-only or event-loop-only slice does not qualify. It leaves human #1857 and M91 open; no automated host test substitutes for physical guest interaction.

## Sources read

#1914, M91a and host acceptance; `host/vm-runner/Sources/VMRunner/main.swift:2763-2824,4384-4391`; `host/vm-runner/Package.swift:29-65`; `tools/session.sh:57-65,108-175,196-232`; `tools/tests/test_session_apps.py:19-99`.
