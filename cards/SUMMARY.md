# M88–M91 card drafts and week-one work order

**Drafts, 2026-10-03; grounded the same day against `7ead82ee`.** Three R1 cards passed after corrections and are filed as sub-issues of their indexes: M90a #1919, M89a #1920, M88a #1921. Those issues are now the canonical cards. The other twelve drafts are held, not filed; each starts with a one-line hold reason. M91-R1 and M91b are held, so week one's committed scope is the three filed R1s (24 hours); the M91 hours wait on the #1857 path handoff. No implementation was written. A followup shape is not a claim or an approved implementation spec.

## Grounding and evidence boundaries

- Read the complete live bodies of [#1915](https://github.com/drawmeanelephant/VirelaiOS/issues/1915), [#1916](https://github.com/drawmeanelephant/VirelaiOS/issues/1916), [#1917](https://github.com/drawmeanelephant/VirelaiOS/issues/1917), [#1914](https://github.com/drawmeanelephant/VirelaiOS/issues/1914), [#1856](https://github.com/drawmeanelephant/VirelaiOS/issues/1856) and [#1857](https://github.com/drawmeanelephant/VirelaiOS/issues/1857), including #1857's comments. Also read its live labels/assignees and the open-claim list.
- Worktree HEAD, local `main`, `origin/main` and live origin `main` all resolve to **`02ac6ea1e687726d8a8a83b350101c83854675bc`**. The checkout was clean before these drafts.
- Read canonical `docs/status.md`, the tree, relevant host/session/input/compositor/SDK/image contracts and existing gate specs. Source citations are in each card.
- Observed host identity for proposed measurement budgets: **Apple M4, 16 GiB RAM, macOS 27.2 (26B5091g)**. The current runner defaults to **2 vCPUs / 256 MiB** (`host/vm-runner/Sources/VMRunner/main.swift:440,1543-1544`). No engine performance or live VM result was measured here.
- #1914's uncommitted corrections and gate results belong to the coach checkout. Its local branch ref remains `fc21e5315d498a999974bc11da0ed2cd1dd39cbb`; that ref does **not** expose dirty work. This pass did not inspect, move, reset or adopt it. M91-R1 requires its consented exact diff/evidence handoff.
- The indexes' “PNG only/no vector path” prose is too broad for this tree. `user/go/webrender/image.go:8-19,43-56` handles QOI and selects PNG; `user/go/webrender/image_png_guest.go:19` explicitly refuses PNG in this guest path; `docs/ui-primitives.md:20-58,137-160` and `user/go/draw/` provide bounded Go shape primitives. That is not SVG or a general PDF-facing vector engine. R1 must survey actual capabilities rather than repeat the shorthand.
- ADR 0030 forbids cgo/Go FFI and new ordinary Zig apps; ADR 0038's native target is a **bounded named portfolio**, not blanket authorization. A1 is a closed four-workload exception, and "adding another program requires another owner-approved ADR amendment" (`0030:366`). QuickJS is C and Go cannot link it, so M88a must land that amendment. M89a/M90a need one only if they choose a native engine; a Go engine under `user/go/` needs none. ADR 0028 D2 also keeps JS out of WEB.ELF, and Amendment A closed JS-in-`WASM.BIN` (it measured QuickJS `04be246` failing a freestanding compile).

## Cards by milestone

Hours are planning estimates including the card's narrow validation, not observed durations. Followup paths, engines and detailed scope are finalized by the corresponding R1 before filing.

| Milestone | Card / local draft | Complete deliverable | Agent hours | Scheduling |
|---|---|---|---:|---|
| M88 #1915 | [M88a R1](m88a-r1.md), #1921 | Pin, actual symbol inventory/dispositions, API, all numeric budgets, final split | 8 | Week one |
| | [M88b](m88b-runtime-shape.md) | Complete bounded offline QuickJS runtime interface/port | 16–24 | Deferred |
| | [M88c](m88c-eval-shape.md) | Guest file + interactive eval and full budget/refusal acceptance | 12–16 | Deferred; final M88 closure PR |
| M89 #1916 | [M89a R1](m89a-r1.md), #1920 | Exact subset, engine survey/rejection, all budgets/hostile limits, M90 dependency, split | 8 | Week one, after M90a |
| | [M89b](m89b-renderer-shape.md) | Complete bounded declared PDF bitmap engine | 24–32 | Deferred; resplit if R1 requires |
| | [M89c](m89c-acceptance-shape.md) | Actual guest bitmap corpus, independent comparisons and bounded negatives | 12–16 | Deferred; final M89 closure PR |
| M90 #1917 | [M90a R1](m90a-r1.md), #1919 | Renderer survey, exact element/text policy, all budgets, vector contract, split | 8 | Week one, before M89a |
| | [M90b](m90b-renderer-shape.md) | Complete SVG and separately callable vector renderer | 16–24 | Deferred; resplit if R1 requires |
| | [M90c](m90c-acceptance-shape.md) | Guest pixel corpus and independent M89-facing consumer proof | 12–16 | Deferred; final M90 closure PR |
| M91 #1914 | [M91-R1](m91-r1.md) | Selected host launch contract, reviewed dirty-tree handoff, exact readiness split | 4 | Week one |
| | [M91a](m91a-host-shape.md) | Supported host identity, dispatch/focus, launch/close/relaunch/cleanup | 12–16 | Deferred |
| | [M91b](m91b-input-shape.md) | Complete native HID completion/order and report normalization | 8–12 | Conditional week-one slot |
| | [M91c](m91c-presentation-shape.md) | Recognizable cursor and exclusive completed-frame ownership | 12–16 | Deferred |
| | [M91d](m91d-workflow-shape.md) | Complete default-workflow automation and human-ready fixture | 8–12 | Deferred |
| M91 final human dependency; originally M87 | [Existing #1857](m91-human-existing-1857.md) | Original human journey + M91 physical readiness and umbrella reconciliation | **0 agent hours** | 2–3 human hours after readiness; do not file |

## A realistic single-agent week

**Completion of all four milestones does not fit a week.** Do not weaken their acceptance or pretend design work ships JS/PDF/SVG.

| Week-one sequence | Hours |
|---|---:|
| M91-R1, establish the host/handoff and claim boundaries | 4 |
| M90a, publish the vector contract before the PDF design | 8 |
| M89a, assess PDF against that interface | 8 |
| M88a, actual QuickJS pin/platform inventory and budgets | 8 |
| Entire M91b, only if its reviewed corrections and full verification fit | 8 |
| Review/grounding, design corrections and contingency | 4 |
| **Total planned capacity** | **40** |

Only the first **28 hours** of R1 deliverables are unconditional scope. If any R1 needs more investigation, owner review/handoff is unavailable, or M91b cannot close within the remaining slot, spend its 8 hours on completing/reviewing the designs and **defer all of M91b**. Do not merge a prerequisite slice and leave its card open. Claude review/owner approval may add elapsed waiting time.

Full initial shapes total **160–212 agent hours**, plus the 4-hour review/contingency allocation and **2–3 human hours**: roughly **4.1–5.4 single-agent weeks**, before unexpected porting or additional R1-selected cards. Week-one delivery consumes 36 card-hours only if M91b fits; roughly **124–172 card-hours** remain (M91b's whole 8–12-hour range leaves with it). These ranges are inference, not an assurance that the surveys will find feasible implementations.

### Explicit deferrals and why

- **M88b/c:** actual QuickJS surface, supported engine configuration, guest packaging and memory/cancellation design must come from R1; a speculative port plus product acceptance is not an 8-hour remainder.
- **M89b/c:** engine/subset/license and hostile-input choices are unresolved. Parser/raster correctness and an independent hostile corpus cannot honestly be compressed into week one. If selected, M90's vector contract implementation must land first.
- **M90b/c:** the surveyed subset/text decision, vector contract and pixel tolerance are not set yet. Complete rendering and independent consumer acceptance need their own implementation time.
- **M91a/c/d:** host contract, uncommitted fixes, frame observability and the 01/02/09 failures need reviewed layer ownership. No reimplementation of already-landed Apps/staging, no speculative timing tweaks.
- **Existing #1857:** human availability and A/B/C/D readiness are mandatory. It is not put into agent capacity, assigned to Sol, or passed with automation.
- No DOM/web APIs, PDF viewer/editor/forms/generation, SVG animation/filter/scripting/external loading, host installer/updater/notarization, remote expansion, boot-default flip or catalog rewrite is added.

## M87 → M91 resolution proposal

1. **Keep accepted remote work under M87.** #1858/#1859/#1860 are historical closed deliverables, with #1860's accepted physical-machine remote journey retained. This is reported in #1857 and canonical `docs/status.md:144`; this pass did not rerun remote acceptance. No remote replacement card or repeated remote session.
2. **Move new desktop implementation responsibility to M91.** Its a/b/c/d boundaries own host lifecycle, native HID, presentation and default workflow readiness. M87 does not remain an expandable desktop bug backlog. PR #1861's launcher/staging stays landed.
3. **Keep the original remaining acceptance issue #1857, human-only.** Place it as M91's final physical dependency and the final original M87 desktop receipt. Preserve its full live checklist, exact receipt commands, real-human/VZ/window/hardware bar and genuine evidence. No duplicate human issue, agent owner or automation substitution. M91's extra physical outcomes are additional observations in the same session, not replacements.
4. **Resolve the claim overlap before future implementation.** Observed live labels currently include `claim` and the body declares coach host/kernel/spec/status paths, although its earlier comment says the label was removed. With owner/human coordination, hand off those code/spec paths to the exact M91 cards and narrow #1857 to `docs/status.md`. Do not create overlapping claims or alter the dirty coach checkout. These are proposed future actions, not actions taken here.
5. **M91d closes on complete automated readiness, not human acceptance.** The existing human card owns the later physical closure bar. A/B/C/D implementation PRs each close their own complete issue, with no unfinished slice.
6. **M87 closes last, after a real landing PR.** The final human #1857 PR merges its compact status update and evidence-backed reconciliation. It says `Closes #1857`, plus `leaves #1914 open` and `leaves #1856 open`. AGENTS.md reserves `Closes #N` for the claimed card's own landing PR, and every recent index (#1807, #1808, #1816, #1880) was closed by hand with no closing PR. After that merge, the owner closes **M91's index (#1914)** once its children are closed and its outcomes observed. The owner then closes **M87's index (#1856)** on accepted remote #1860 plus the observed desktop journey. M88c/M89c/M90c follow the same rule for #1915/#1916/#1917. No reconciliation micro-card or umbrella-only PR.

This reconciles #1914's “leaves #1857 and #1856 open” instruction with eventual completion: **drafting, design and readiness do not close them; the observed human acceptance does, followed by the owner's index closures.**

## Ownership and verification discipline

- Each file has one prospective editor across this whole breakdown. Contract ADRs are design-owned and read-only to implementations. One doc per arc; no new `march-*`, parallel scoping prose or status changelog.
- New engine/client/proof directories are explicitly **proposed**, not existing state. R1 ratifies or revises reservations before claims; it must not introduce overlap. Root `build.zig`, manifests and shared SDK/draw/font files have **no editor in this proposal**; isolated offline builds must prove reuse or trigger a reviewed split/ownership revision.
- Exclusive spec editors: M88c `live-el0-exec`; M89c `live-image-viewer`; M90c `live-web` (these three are provisional: their R1s confirm coverage, and `live-web` does not cover SVG raster, `live-web.spec:49`); M91b `live-input`/`live-xhci`/`live-usb-lifecycle`; M91c `go-wm-console-ink`/`live-sb4-damage-tracking`/`live-wm-pacing`; M91d `go-wm-hid`/`go-wm-default`. Other gates are read-only regression checks.
- Write reservations in coordination-gate syntax: exact paths or `dir/*` prefix globs. The gate strips one trailing `*` and prefix-matches the rest literally (`tools/status/verify-issue-coordination.sh:89-101`). Observed: `host/vm-runner/**` does **not** overlap `host/vm-runner/Package.swift`, so a `dir/**` claim would hide a real collision with #1857.
- `docs/status.md` has no M88–M91 row yet. Under ADR 0030's convention, status rows merge at landing and are not declared in Touches (`0030:106-107`). Each milestone's final landing adds or edits its one compact row without declaring the file, so #1857's declared claim on it does not block M88–M90 rows.
- Specs are extended, never replaced by new verification shell gates. No gate-inventory snapshot is edited. R1 can reject a proposed adjacent spec choice, but must reassign a **named existing** spec without overlap before finalizing.
- Proposed numeric ceilings are labeled proposals throughout. R1 must approve concrete numbers, exact pins, actual inventories and scope. In particular, QuickJS shim caps are not fabricated source counts; R1's closure requires exact audited counts.
- Every card states its complete closing deliverable and artifacts. Standard gate evidence is `artifacts/NAME-{report.txt,serial-TAG.log,run-TAG.txt}` plus copied receipts/scanouts (`tools/gate/SPEC.md:62-101`). Commit only small pinned vectors/goldens; never logs, arbitrary host content or an evidence narrative.
- A missing dependency/capability remains precisely blocked. A source read, old gate count or issue report is not a new pass. #1857 retains genuine class C; agents only prepare and validate its prerequisites.

## This drafting pass

Observed: all six live issues read, main revision matched, host/toolchain identity checked and cited repository contracts inspected. Validation of the local markdown/ownership/budget/index coverage is recorded in the session response, not as product acceptance. No JS/PDF/SVG engine source audit, implementation test, class-B VM gate or human class-C acceptance was performed.
