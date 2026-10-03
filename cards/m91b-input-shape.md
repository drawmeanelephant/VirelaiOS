# M91b — Preserve HID completions and decode native keyboard/pointer reports correctly

- **Parent index:** M91, [#1914](https://github.com/drawmeanelephant/VirelaiOS/issues/1914).
- **Owner:** Sol 6.1, input implementer; owner review before merge.
- **Depends on:** none. Can land independently of M91a/c/d once its tests can exercise the selected input seam.
- **Starting material:** the coach slice for `kernel/src/xhci.zig`, `kernel/src/input.zig`, `tools/gate/specs/live-input.spec` and `docs/hardware-contract.md`. It comes from the uncommitted `droid/coach-through-ios-app` work on `fc21e531`, preserved by the owner as the local, unpushed branch `droid/m91-coach-handoff` (commit `1f083d23`; patch sha256 `767ca34384a9ca4a3842362d764cf48b3c121cbbb2cf814f47eeba70f3446c32`), which every worktree of the repository can read. It applies cleanly to `7ead82ee`. Take this card's files with `git diff fc21e531 droid/m91-coach-handoff -- <paths> | git apply`; checking the paths out from the branch would revert later main changes. It is an unreviewed draft (#1914): reproduce each failure, review, and land it with its own regression. #1914 reports `live-input` 2/2 **on that draft**, not on main.
- **Size / scheduling:** 8–12 agent hours. Candidate for week one's final 8-hour slot **only if the entire card fits**; otherwise defer it whole.

## Deliverable

Land complete shared HID-completion preservation/rearming/order correctness and native report normalization, keeping the canonical five-byte custom-virtio pointer payload unchanged. Use #1914's captured native report-ID-1/buttons/LE16-X/Y, 0–32767 format only after reviewing its genuine descriptor/captures. Cover keyboard keys, modifiers, releases/repeat, pointer movement and button edges. A pointer decode patch without cross-device completion correctness does not finish this card; native physical acceptance belongs to #1914's human session.

## Exclusive ownership

- `kernel/src/xhci.zig`, `kernel/src/input.zig`, including their embedded unit tests.
- `tools/gate/specs/live-input.spec`, `tools/gate/specs/live-xhci.spec`, `tools/gate/specs/live-usb-lifecycle.spec`.
- `docs/hardware-contract.md`: input rows/wire distinction only; sole file editor in this breakdown.
- Small pinned native/canonical vectors under proposed `tests/fixtures/input/m91/*`.
- No host, compositor, WM/Go, `go-wm-hid.spec` or status edits.

## Verification

- `zig build test` plus focused regressions for interleaved keyboard/pointer completions, report ordering, ring wrap/rearm, releases/modifiers/repeat policy and short/malformed reports.
- Vector tests cover native report-ID boundaries and X/Y endpoints, button down/held/up, and unchanged canonical custom-virtio bytes. Test each transport explicitly rather than heuristically reinterpreting one shared payload.
- Extend the named input/XHCI specs only; run `just gate live-input`, `just gate live-xhci`, `just gate live-usb-lifecycle`, and existing `just gate live-pointer-virtio` read-only. Demonstrate native USB completion consumption separately from the custom-virtio leg.
- Run the full current `go-wm-hid` read-only and report all results. Its injected-input proof is not physical keyboard/trackpad proof; failures stop readiness at the responsible layer.
- Evidence: small captured-wire vectors committed with provenance; actual failing-before/passing-after regression output and standard `artifacts/live-input-*`, `live-xhci-*`, `live-usb-lifecycle-*` reports/captures. Keep descriptor/full capture logs under `artifacts/m91-input/`.
- Run `just verify-portable`, inventory/coordination checks and `git diff --check`. Never update hardware rows to “observed” from inference.

## Closes

The PR merging **all input corrections and passing the input contract/gates** closes this issue. Decoder-only or completion-only prerequisite PRs do not qualify. Physical human acceptance stays open on #1914; gate failure cannot be renamed “done.”

## Sources read

#1914, native wire observation and M91b; `kernel/src/xhci.zig:1042-1070,1409-1448,1852-1924` (the in-order test at `:1875-1921` models one ring; cross-device order is untested at baseline); `kernel/src/input.zig:1064-1117,1444-1527` (the decoder accepts reports of three or more bytes; the five-byte canonical payload is specified in `docs/hardware-contract.md:204-218`); `tools/gate/specs/live-input.spec:14-37`; `docs/hardware-contract.md:21-23,204-218`; the coach snapshot's file list and hunk headers.
