# M91a — Land the supported macOS host front door and VM lifecycle

- **Parent index:** M91, [#1914](https://github.com/drawmeanelephant/VirelaiOS/issues/1914).
- **Owner:** Sol 6.1, host implementer; owner review before merge.
- **Depends on:** none. The host launch contract (drafted separately as M91-R1, now folded in) is this card's first deliverable.
- **Starting material:** the coach slice for this card's six files: `host/vm-runner/Package.swift`, `host/vm-runner/Sources/VMRunner/main.swift`, new `host/vm-runner/Sources/VMAppKit/NativeInput.swift`, new `host/vm-runner/Tests/VMRunnerTests/NativeInputTests.swift`, `tools/session.sh`, `tools/tests/test_session_apps.py`. It comes from the uncommitted `droid/coach-through-ios-app` work on `fc21e531`, preserved by the owner as a local patch snapshot (`artifacts/m91-coach-handoff/`, sha256 `767ca34384a9ca4a3842362d764cf48b3c121cbbb2cf814f47eeba70f3446c32`), and applies cleanly to `7ead82ee`. It is an unreviewed draft (#1914): reproduce each failure, review, and land it with its own regression. #1914 reports that typing needed both this slice's host event-loop change and M91b's USB completion change; test each on its own seam.
- **Size / scheduling:** 16–20 agent hours (12–16 plus the folded host-contract choice), deferred beyond week one.

## Deliverable

First, choose and record one supported host packaging/launch and identity contract: activation/first responder, startup/error display, close/quit/relaunch and cleanup. Preserve the CLI gate entrypoint. No installer, updater, notarization service or system-permission change. Record the choice in the launch instructions this card owns; if it proves ABI/security/cross-cutting enough for an ADR, declare that file before claiming.

Then land the entire selected macOS host front door: distinct app/window identity, native event dispatch, activation/first responder, clear startup/failure state and predictable close/quit/relaunch with VM cleanup. Retain the already-landed source-fresh manifest staging, preserving fixture documents/settings. Do not reimplement M87's launcher/staging or declare inherited Factory/Terminal identity fixed from API return values. The complete host lifecycle is this card's acceptance; the physical guest journey is #1914's human milestone acceptance.

## Exclusive ownership

- `host/vm-runner/*`, excluding generated `.build/`: host modules, package, app resources and Swift tests. `Sources/VMAppKit/` is new in the coach slice, not present on main.
- `tools/session.sh`, `tools/tests/test_session_apps.py`.
- `README.md`, `docs/testing.md`: supported local launch instructions only; preserve accepted remote material.
- No kernel, guest seat/app, hardware/status or gate-spec edits.

## Verification

- Run `swift test --package-path host/vm-runner` and the SPIKE release build used by session: `swift build --package-path host/vm-runner --configuration release -Xswiftc -DSPIKE`.
- Extend staging/lifecycle/identity tests, then run `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools/tests -p test_session_apps.py` and `bash -n tools/session.sh`. Preserve existing missing-builder, source-freshness and no-document/settings-overwrite behavior.
- In a consented isolated fixture, observe supported clean launch, key/first-responder state, window identity, close/quit and two launch/relaunch cycles. Record owned VM PIDs before/after, stop/cleanup outcomes and failures. A reported activation success or hidden window is insufficient.
- Run existing `live-input` and `go-wm-default` gates read-only for compatibility; an input-layer failure routes to M91b and cannot be narrated as a regression pass. No new verification shell gate.
- Evidence: Swift/staging/build output and fixture host lifecycle observations under `artifacts/m91-host/`, standard gate reports, no arbitrary host desktop capture.
- Run inventory/coordination checks before the future PR and `git diff --check`.

## Closes

The PR merging the complete supported host behavior and passing host/lifecycle verification closes this issue. A packaging-only or event-loop-only slice does not qualify. It leaves #1914 open; no automated host test substitutes for the human session.

## Sources read

#1914, M91a and host acceptance; `host/vm-runner/Sources/VMRunner/main.swift:2763-2824,4384-4391`; `host/vm-runner/Package.swift:29-65`; `tools/session.sh:57-65,108-175,196-232`; `tools/tests/test_session_apps.py:19-99`; the coach snapshot's file list and hunk headers.
