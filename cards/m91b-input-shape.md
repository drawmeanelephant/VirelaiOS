# M91b — Preserve HID completions and decode native keyboard/pointer reports correctly

- **Parent index:** M91, [#1914](https://github.com/drawmeanelephant/VirelaiOS/issues/1914).
- **Owner:** Sol 6.1, input implementer; owner review before merge.
- **Depends on:** M91-R1 and approved #1857 claim-path handoff. Can land independently of M91a/c/d once its tests can exercise the selected input seam.
- **Scope finalized by R1:** final scope set by R1 output. This includes reviewed wire captures and dirty-tree corrections.
- **Size / scheduling:** 8–12 agent hours. Candidate for week one's final 8-hour slot **only if the entire card fits**; otherwise defer it whole.

## Deliverable

Land complete shared HID-completion preservation/rearming/order correctness and native report normalization, keeping the canonical five-byte custom-virtio pointer payload unchanged. Use #1914's captured native report-ID-1/buttons/LE16-X/Y, 0–32767 format only after reviewing its genuine descriptor/captures. Cover keyboard keys, modifiers, releases/repeat, pointer movement and button edges. A pointer decode patch without cross-device completion correctness does not finish this card; native physical acceptance is still the human's #1857 work.

## Exclusive ownership

- `kernel/src/xhci.zig`, `kernel/src/input.zig`, including their embedded unit tests.
- `tools/gate/specs/live-input.spec`, `tools/gate/specs/live-xhci.spec`, `tools/gate/specs/live-usb-lifecycle.spec`.
- `docs/hardware-contract.md`: input rows/wire distinction only; sole file editor in this breakdown.
- Small pinned native/canonical vectors under proposed `tests/fixtures/input/m91/**`.
- No host, compositor, WM/Go, `go-wm-hid.spec`, status or R1 ADR edits.

## Verification

- `zig build test` plus focused regressions for interleaved keyboard/pointer completions, report ordering, ring wrap/rearm, releases/modifiers/repeat policy and short/malformed reports.
- Vector tests cover native report-ID boundaries and X/Y endpoints, button down/held/up, and unchanged canonical custom-virtio bytes. Test each transport explicitly rather than heuristically reinterpreting one shared payload.
- Extend the named input/XHCI specs only; run `just gate live-input`, `just gate live-xhci`, `just gate live-usb-lifecycle`, and existing `just gate live-pointer-virtio` read-only. Demonstrate native USB completion consumption separately from the custom-virtio leg.
- Run the full current `go-wm-hid` read-only and report all results. Its injected-input proof is not physical keyboard/trackpad proof; failures stop readiness at the responsible layer.
- Evidence: small captured-wire vectors committed with provenance; actual failing-before/passing-after regression output and standard `artifacts/live-input-*`, `live-xhci-*`, `live-usb-lifecycle-*` reports/captures. Keep descriptor/full capture logs under `artifacts/m91-input/`.
- Run `just verify-portable`, inventory/coordination checks and `git diff --check`. Never update hardware rows to “observed” from inference.

## Closes

The PR merging **all input corrections and passing the input contract/gates** closes this issue. Decoder-only or completion-only prerequisite PRs do not qualify. Physical human acceptance stays open on #1857; gate failure cannot be renamed “done.”

## Sources read

#1914, native wire observation and M91b; `kernel/src/xhci.zig:1409-1448,1852-1924`; `kernel/src/input.zig:1064-1117,1444-1527`; `tools/gate/specs/live-input.spec:14-37`; `docs/hardware-contract.md:21-23,204-218`.
