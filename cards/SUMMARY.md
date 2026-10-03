# M88–M91 card drafts and week-one work order

**Drafts, 2026-10-03; grounded the same day against `7ead82ee`.** Three R1 cards passed after corrections and are filed as sub-issues of their indexes: M90a #1919, M89a #1920, M88a #1921. The owner then closed #1857 as superseded by M91 (#1914) and closed M87's index #1856 with a pointer to #1914. M91a–d were filed under #1914 as #1922–#1925. Filed issues are now the canonical cards. M91-R1 was folded into those cards, and the human-acceptance draft is superseded by #1914's milestone acceptance. The six M88–M90 implementation shapes stay held until their R1s land; each starts with a one-line hold reason. Week one's committed scope is the three filed R1s (24 hours). No implementation was written. A followup shape is not a claim or an approved implementation spec.

## Grounding and evidence boundaries

- Read the complete live bodies of [#1915](https://github.com/drawmeanelephant/VirelaiOS/issues/1915), [#1916](https://github.com/drawmeanelephant/VirelaiOS/issues/1916), [#1917](https://github.com/drawmeanelephant/VirelaiOS/issues/1917), [#1914](https://github.com/drawmeanelephant/VirelaiOS/issues/1914), [#1856](https://github.com/drawmeanelephant/VirelaiOS/issues/1856) and [#1857](https://github.com/drawmeanelephant/VirelaiOS/issues/1857), including #1857's comments. Also read its live labels/assignees and the open-claim list.
- Worktree HEAD, local `main`, `origin/main` and live origin `main` all resolve to **`02ac6ea1e687726d8a8a83b350101c83854675bc`**. The checkout was clean before these drafts.
- Read canonical `docs/status.md`, the tree, relevant host/session/input/compositor/SDK/image contracts and existing gate specs. Source citations are in each card.
- Observed host identity for proposed measurement budgets: **Apple M4, 16 GiB RAM, macOS 27.2 (26B5091g)**. The current runner defaults to **2 vCPUs / 256 MiB** (`host/vm-runner/Sources/VMRunner/main.swift:440,1543-1544`). No engine performance or live VM result was measured here.
- #1914's uncommitted corrections and gate results belong to the coach checkout on `fc21e5315d498a999974bc11da0ed2cd1dd39cbb`; that ref does **not** expose dirty work. With the owner's consent, the 14 dirty files were snapshotted read-only as a patch (sha256 `767ca34384a9ca4a3842362d764cf48b3c121cbbb2cf814f47eeba70f3446c32`) and preserved as the local, unpushed branch `droid/m91-coach-handoff` (commit `1f083d23`, parent `fc21e531`). It round-trips byte for byte on `fc21e531`; its 13 code/spec files apply cleanly to `7ead82ee`, and its stale `docs/status.md` hunk does not. Each M91 card names its file slice. The coach worktree was not modified, and nothing was adopted.
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
| M91 #1914 | [M91-R1](m91-r1.md), folded | Host contract moved to M91a; handoff done by snapshot and slice map | 0 | Not filed |
| | [M91a](m91a-host-shape.md), #1922 | Host launch contract, then supported host identity, dispatch/focus, launch/close/relaunch/cleanup | 16–20 | Deferred |
| | [M91b](m91b-input-shape.md), #1923 | Complete native HID completion/order and report normalization | 8–12 | Conditional week-one slot |
| | [M91c](m91c-presentation-shape.md), #1924 | Recognizable cursor and exclusive completed-frame ownership | 12–16 | Deferred |
| | [M91d](m91d-workflow-shape.md), #1925 | Complete default-workflow automation and human-ready fixture | 8–12 | Deferred |
| M91 human acceptance, on #1914 | [Superseded #1857](m91-human-existing-1857.md) | One human session after M91a–d land; discharges M87's desktop acceptance | **0 agent hours** | 2–3 human hours after readiness; do not file |

## A realistic single-agent week

**Completion of all four milestones does not fit a week.** Do not weaken their acceptance or pretend design work ships JS/PDF/SVG.

| Week-one sequence | Hours |
|---|---:|
| M90a, publish the vector contract before the PDF design | 8 |
| M89a, assess PDF against that interface | 8 |
| M88a, actual QuickJS pin/platform inventory and budgets | 8 |
| Entire M91b, only if its reviewed corrections and full verification fit | 8 |
| Review/grounding, design corrections and contingency | 8 |
| **Total planned capacity** | **40** |

Only the first **24 hours** of R1 deliverables are unconditional scope. If any R1 needs more investigation, owner review is unavailable, or M91b cannot close within the remaining slot, spend its 8 hours on completing/reviewing the designs and **defer all of M91b**. Do not merge a prerequisite slice and leave its card open. Claude review/owner approval may add elapsed waiting time.

Full initial shapes total **160–212 agent hours** (M91-R1's 4 hours moved into M91a), plus the 8-hour review/contingency allocation and **2–3 human hours**: roughly **4.2–5.5 single-agent weeks**, before unexpected porting or additional R1-selected cards. Week-one delivery consumes 32 card-hours only if M91b fits; roughly **128–176 card-hours** remain (M91b's whole 8–12-hour range leaves with it). These ranges are inference, not an assurance that the surveys will find feasible implementations.

### Explicit deferrals and why

- **M88b/c:** actual QuickJS surface, supported engine configuration, guest packaging and memory/cancellation design must come from R1; a speculative port plus product acceptance is not an 8-hour remainder.
- **M89b/c:** engine/subset/license and hostile-input choices are unresolved. Parser/raster correctness and an independent hostile corpus cannot honestly be compressed into week one. If selected, M90's vector contract implementation must land first.
- **M90b/c:** the surveyed subset/text decision, vector contract and pixel tolerance are not set yet. Complete rendering and independent consumer acceptance need their own implementation time.
- **M91a/c/d:** filed, but the host contract, coach-slice review, frame observability and the 01/02/09 failures need more than week one. No reimplementation of already-landed Apps/staging, no speculative timing tweaks.
- **#1914's human session:** human availability and A/B/C/D readiness are mandatory. It is not put into agent capacity, assigned to Sol, or passed with automation.
- No DOM/web APIs, PDF viewer/editor/forms/generation, SVG animation/filter/scripting/external loading, host installer/updater/notarization, remote expansion, boot-default flip or catalog rewrite is added.

## M87 → M91 resolution (owner decision, 2026-10-03)

1. **Accepted remote work stays under M87.** #1858/#1859/#1860 are closed deliverables, with #1860's accepted physical-machine remote journey retained. No remote replacement card or repeated remote session.
2. **M91 owns new desktop work.** M91a–d (#1922–#1925) own host lifecycle, native HID, presentation and default workflow readiness. PR #1861's launcher/staging stays landed.
3. **#1857 is closed as superseded by #1914; #1856 is closed with a pointer to #1914.** #1914's milestone acceptance already lists every #1857 step (Apps click, pointer-only Terminal, dismissal and header/padding negatives, missing binary, guest-written `/host` receipt with byte comparison, real scanouts) and adds relaunch survival. #1857's baseline `go-dogfood` wide-rune note moves to M91d's read-only `go-dogfood` check. #1914's body was updated to match.
4. **The claim overlap is resolved by that closure.** The coach diff is preserved as a local snapshot and split exactly once across M91a–d; its `docs/status.md` hunk goes to the final status pass.
5. **M91d closes on complete automated readiness, not human acceptance.** One human session runs on #1914 after M91a–d land. A failed step returns to its owning card with evidence. Each implementation PR closes its own complete issue, with no unfinished slice.
6. **Closures.** Each card's landing PR says `Closes #<its issue>` exactly once. Indexes close by hand, as every recent index did (#1807, #1808, #1816, #1880). Before closing an index, the owner lands one small status PR for its row (precedent #1913). For M91, that PR follows the human session and also rewrites the M87 row. M88c/M89c/M90c and #1915/#1916/#1917 follow the same rule.

## Ownership and verification discipline

- Each file has one prospective editor across this whole breakdown. Contract ADRs are design-owned and read-only to implementations. One doc per arc; no new `march-*`, parallel scoping prose or status changelog.
- New engine/client/proof directories are explicitly **proposed**, not existing state. R1 ratifies or revises reservations before claims; it must not introduce overlap. Root `build.zig`, manifests and shared SDK/draw/font files have **no editor in this proposal**; isolated offline builds must prove reuse or trigger a reviewed split/ownership revision.
- Exclusive spec editors: M88c `live-el0-exec`; M89c `live-image-viewer`; M90c `live-web` (these three are provisional: their R1s confirm coverage, and `live-web` does not cover SVG raster, `live-web.spec:49`); M91b `live-input`/`live-xhci`/`live-usb-lifecycle`; M91c `go-wm-console-ink`/`live-sb4-damage-tracking`/`live-wm-pacing`; M91d `go-wm-hid`/`go-wm-default`. Other gates are read-only regression checks.
- Write reservations in coordination-gate syntax: exact paths or `dir/*` prefix globs. The gate strips one trailing `*` and prefix-matches the rest literally (`tools/status/verify-issue-coordination.sh:89-101`). Observed: `host/vm-runner/**` does **not** overlap `host/vm-runner/Package.swift`, so a `dir/**` claim hides a real collision. #1914's proposed table still uses `**` globs and gives `docs/status.md` to M91d; the filed child cards govern.
- No card declares `docs/status.md`. Under ADR 0030's convention, status rows merge at landing and are not declared in Touches (`0030:106-107`). `docs/status.md` has no M88–M91 row yet, and its M87 row is stale after the closures. Each index's final status PR edits its one compact row.
- Specs are extended, never replaced by new verification shell gates. No gate-inventory snapshot is edited. R1 can reject a proposed adjacent spec choice, but must reassign a **named existing** spec without overlap before finalizing.
- Proposed numeric ceilings are labeled proposals throughout. R1 must approve concrete numbers, exact pins, actual inventories and scope. In particular, QuickJS shim caps are not fabricated source counts; R1's closure requires exact audited counts.
- Every card states its complete closing deliverable and artifacts. Standard gate evidence is `artifacts/NAME-{report.txt,serial-TAG.log,run-TAG.txt}` plus copied receipts/scanouts (`tools/gate/SPEC.md:62-101`). Commit only small pinned vectors/goldens; never logs, arbitrary host content or an evidence narrative.
- A missing dependency/capability remains precisely blocked. A source read, old gate count or issue report is not a new pass. #1914's human session retains genuine class C; agents only prepare and validate its prerequisites.

## This drafting pass

Observed: all six live issues read, main revision matched, host/toolchain identity checked and cited repository contracts inspected. Validation of the local markdown/ownership/budget/index coverage is recorded in the session response, not as product acceptance. No JS/PDF/SVG engine source audit, implementation test, class-B VM gate or human class-C acceptance was performed.
