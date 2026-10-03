# M87a — Complete human desktop launcher acceptance (implementation landed)

- **Parent index:** M91, [#1914](https://github.com/drawmeanelephant/VirelaiOS/issues/1914), as its existing final human acceptance dependency; original parent M87, [#1856](https://github.com/drawmeanelephant/VirelaiOS/issues/1856).
- **Existing issue:** [#1857](https://github.com/drawmeanelephant/VirelaiOS/issues/1857). **Do not file a duplicate or renumber it.** This file is a placement/closure proposal, not a replacement for its live human requirements.
- **Owner:** real human acceptance operator, currently unassigned. **Not agent work.**
- **Depends on:** M91-R1 and completed M91a/b/c/d readiness, current relevant gates green.
- **Size / scheduling:** 2–3 human hours including checks and the evidence/landing PR; excluded from the single-agent week and dependent on human availability.

## Deliverable

Observe and document the original #1857 journey on a real Apple-silicon/macOS 27+ host with a visible activated VZ VM, source-fresh apps, a fresh isolated fixture share and genuine keyboard/trackpad/pointer interaction. Retain every live #1857 requirement, including the exact guest-written receipt and independent host byte comparison. In that same consented human session, cover M91's additional launch/close/relaunch, normal input/cursor, flash-free ordinary interaction, representative editor/focus and receipt-survival requirements. Update the compact canonical status row only after these observations and accepted remote evidence satisfy both umbrellas. No automation, injected input, localhost result or hidden-window screenshot passes this card.

## Exclusive ownership

- Proposed narrowed #1857 repository ownership: `docs/status.md` only, for the final compact reconciliation.
- Real fixture evidence stays under gitignored `artifacts/`; it is not a repository ownership claim.
- The **live** issue still declares host/kernel/spec files and is labeled `claim`. Obtain an approved future handoff to M91 owners before narrowing that claim; this drafting task changes nothing on GitHub.
- No code-defect ownership is silently transferred to the human. A failed step returns to its scoped M91 owner; acceptance remains unpassed until corrected and reverified.

## Verification

Use the **unchanged live #1857 fixture procedure**, including:

```bash
source tools/env-check.sh
set -o pipefail
ROOT="$(git rev-parse --show-toplevel)"
fixture="$(mktemp -d "$ROOT/artifacts/m87-desktop-acceptance.XXXXXX")"
mkdir -p "$fixture/share"
VIRELAI_SESSION_SHARE="$fixture/share" bash tools/session.sh 2>&1 | tee "$fixture/host.log"
```

No build-skip, alternate-seat flag, personal share, security/permission change or unrelated host capture. The human, with the initial GOSH tab already open, clicks Apps, reads the named rows and clicks Terminal by pointer alone. In the **guest Terminal tab**, run exactly:

```text
cd /host
echo m87-desktop-owner-ok > M87-DESKTOP.RECEIPT
cat M87-DESKTOP.RECEIPT
```

After stopping only that session, independently check:

```bash
printf 'm87-desktop-owner-ok\n' | cmp - "$fixture/share/M87-DESKTOP.RECEIPT"
```

- Preserve #1857's full filter/Escape/outside-dismissal, header/padding, unavailable ELF, no-leak/no-double-launch/no-click-through and real guest-scanout PNG requirements. Copy that session's serial log into its fixture evidence.
- Additional M91 coverage, **without replacing the original receipt**: supported launch/close/relaunch cycles; slow movement/normal click edges; typing/Backspace/navigation/modifiers; 60 seconds of ordinary menu/focus/typing without intermediate flashing; representative editor; a separate uniquely named guest receipt independently compared after stop and relaunch.
- Run #1857's current checks: `go test ./gotabwm ./vi` from `user/go`, session-staging unittest, `bash -n tools/session.sh`, `just gate go-wm-hid`, inventory and coordination checks. Include the M91 readiness gate reports; a relevant failure blocks acceptance.
- Evidence: real guest scanouts, fixture-only before/after files, both byte comparisons, logs, revision/host capability and the human's observed step results under the retained fixture path. The future concise evidence comment is part of that acceptance, **not authorized in this draft task**.

## Closes

The **human acceptance landing PR**, after the whole observed journey and compact status reconciliation, closes existing human card #1857. If M91's child issues are already closed and all its outcomes are observed, that **same PR** also closes **M91's index (#1914)**; accepted remote #1860 plus the now-observed original desktop journey lets that same PR complete **M87's index (#1856)** last. Its future PR body names each intended closure once. No separate orphan-closing action and no index or implementation PR passes the human card. If any prerequisite or step is blocked, acceptance and the umbrellas stay open.

## Sources read

#1857, “Remaining acceptance work,” all five human steps and verification/completion; #1914, milestone acceptance and M87 reconciliation; #1856, both original journeys; `docs/status.md:144`. No human acceptance was performed by this drafting session.
