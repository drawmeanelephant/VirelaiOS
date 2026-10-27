<!-- iface-review:v1 app=TABWM.BIN issue=3694 -->
# App audit: Tabbed Desktop (TABWM.BIN)

> Retired from polish audit issue #3694 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

`TABWM.BIN` ("Tabbed Desktop", `user/src/tabwm.zig`, 6.4 kLOC Zig) — the
browser-style WM server that retired floating windows for a tab strip +
sidebar, and the fleet's deliberate **fallback seat**: `settings set wm
tabwm` selects it while GOTABWM.ELF owns the daily default (ADR 0034 —
retained permanently, engine frozen, bug-fix only). Manifest row:
`TABWM.BIN | Tabbed Desktop | m | dock=false | v=2 | icon=ICONTABM` —
not launchable like an app; it starts via the `wm` setting or the monitor's
`tabwm start` (console verb observed in the gate script).

## Interface today

Observed (tabwm.zig doc header + live-tabwm serial):

- Chrome: 180 px left sidebar — Sexiburger "God Menu" button (mascot
  emblem + shortcut hint) on top, a vertical pill-shaped tab list with
  accent bar + close-x per row in the middle, and a status tray (clock,
  `[D]/[L]` theme toggle, clipboard badge) at the bottom. Content
  viewport: the full 1100×720 to the right; inactive tabs are hidden via
  `wmctl set_state`.
- Mouse: click pill = activate, click x = kernel WIN_CLOSE (real app
  exit), middle-click = close, drag = reorder, `+ New tab` = launcher
  palette; close flashes an accent band ~18 ticks; dirty tabs wear an
  accent dot.
- Keyboard: Ctrl+Tab / Ctrl+Shift+Tab cycle, Ctrl+1..9 jump, Ctrl+W close,
  Ctrl+T new-tab palette, Ctrl+Space God Menu, Alt+Tab / Alt+Shift+Tab
  with WND-parity semantics.
- Lifecycle: kernel WIN_CLOSE seam; unsaved tabs route every close
  through the self-painted 200×100 dialog (same geometry/palette as
  WND's, kernel primitives 3-6); recently-closed ring, duplicate, drag
  reorder, scrollable overflow with a `+N` label.
- Shim mode: boots without a seat print `tabwm: none (shim compositing)`;
  `tabwm start` brings the seat up live (observed in gate serial).

## Coverage

`live-tabwm` — **PASS 1/1** (2026-10-25): cold boot, `tabwm` shim
announcement, `tabwm start` → registered → sidebar-rendered → present →
WINLOOP tab switch. The surrounding family exists and is broad:
`live-tabwm-{alttab,bt,close,fullscreen,unsaved}` plus `live-tabs`,
`live-tabstrip`, `live-tabclick`, `go-wm-tabs`, `go-wm-default` (the
fallback boot), `go-tabapp` — only live-tabwm was run for this audit;
the family is listed, not re-verified.

## Observed defects

None observed in gate output or code. (The gate run also showed a related
roughness upstream in the seat layer — `gotabwm`'s reopen path, filed as
#3691 — but that is the Go seat's table, not TABWM's.)

## Improvement candidates

Constraint honesty first: ADR 0034 Amendment A froze the Zig seat's
engine — candidates below are cosmetic/surface-level; deep UX work
belongs on GOTABWM or is out of scope by decision.

- **Tab pills have no app icons.** APPS.TXT rows already carry
  `icon=` rasters and the launcher paints them — the sidebar row could
  wear the same ICON* tile next to the title.
- **No pinned-tab affordance.** GOTABWM grew FlagPinned; the Zig seat's
  strip has reorder + overflow but no pin/sticky — a parity gap for
  fallback-seat dogfooding (if engine-frozen, note it as a deliberate
  divergence in the ADR rather than a bug).
- **Status tray is text-only.** Clock + [D]/[L] + clipboard badge; the
  net/audio/notifications state the Go seat shows has no fallback
  analogue — even the M82e per-app log badge would help debugging on the
  fallback.
- **Overflow scroll is invisible until needed.** `+N` label exists; a
  persistent scroll affordance (arrow caps on the list) would advertise
  it.
- **`tabwm` console UX:** `tabwm` prints `none (shim compositing)` —
  accurate, but `tabwm start`/`stop` discoverability is nil; a one-line
  `tabwm — the fallback tabbed seat: start|stop` usage response costs
  nothing.
- **Sidebar width is hardcoded 180 px** — narrow titles truncate; a
  settings-keyed width (or ellipsis with tooltip-ish selection echo)
  would help long app titles.

## Evidence

```
$ just gate live-tabwm                    # PASS 1/1
$ sed -n '1,140p' user/src/tabwm.zig      # interface doc header
$ cat docs/decisions/0034-tabwm-retained-fallback.md
$ grep TABWM image/apps.txt               # dock=false v=2 icon=ICONTABM
$ cat site/apps/tabwm.md site/includes/apps/tabwm.story.md
```
