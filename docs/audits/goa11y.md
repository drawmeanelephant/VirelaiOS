<!-- iface-review:v1 app=GOA11Y.ELF issue=3727 -->
# App audit: Accessibility (GOA11Y.ELF)

> Retired from polish audit issue #3727 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The accessibility outline reader — every 500 ms it asks the seat's WINDOWS_QUERY (kind 16) which window holds focus, reads that window's published outline at `/host/A11Y.<win>`, and paints it as an indented role/state/name list. `-announce` prints each new utterance; `-say` stages it on the M186d speech lane (SAY intent → seat-signed SPEAK record → host NSSpeechSynthesizer). Manifest row `GOA11Y.ELF | Accessibility | A | dock=true | v=2 | caps=file,window,timer | icon=ICONA11Y` (image/apps.txt:68) — the only audited app that IS docked.

## Interface today

Observed from `user/go/goa11y/main.go` (365 lines — the smallest audited app):

- One window, native 512x384 at (96,72). Header `goa11y — focused window outline` in accent; below, either the outline as an indented list (`<role> <state> <name>`, 14 px rows, 14 px per depth level, clipped at the window height) or a named status line: `waiting for the first poll` / `windows_query refused — no Go seat answers` / `no focused window` / `focused window publishes no outline` / `outline file is malformed`.
- Input model: **none by contract** — `handle` answers only `EvTimer`; all input events are ignored ("the reader is a mirror, not a driver"). No keyboard, no pointer, no scroll.
- Behavior: dedup on (win, body bytes) — the poll stays silent until the outline moves; utterance = the focused node's name, else `nodes[0].Name + ", N nodes"`, clamped to the speech lane's 512-byte grammar; announce/say dedup on (win, utterance) so a rename re-announces and a repeat does not.
- argv: `-announce`, `-say` only — anything else is ignored silently.

## Coverage

`tools/gate/specs/go-a11y.spec` — ran `BOOTS=1 just gate go-a11y` on this worktree: **PASS, 2/2 runs** — the announce ordering + speak-lane chain (`goa11y: announce` → `say queued` → `gotabwm: hostint speak` → host `host-speech: speak`) under a live GOTABWM seat with the scripted GODEMO focus drive, and the M197e four-leg journey (seat outline naming live tabs, in-order announces, live `a11y.reduce_motion` flip reaching GOVIEW, GOEDIT's published outline decoded off the share).

## Observed defects

- None observed — both runs green; every focus move narrated in order and the speech-lane receipts chained end to end.

## Improvement candidates

- The list clips silently at window height — deep outlines (GODEMO's 64-node table) overflow invisibly; PgUp/PgDn scroll or a scroll bar would make the mirror complete (input is ignored today, so even scroll needs a contract change).
- All output is one flat `role state name` line per node — visual hierarchy is only the 14 px indent; role icons or color-coded states (checked/focused) would read faster.
- No way to pin/inspect a window that is NOT focused — a `win=<id>` argv or a "follow this window" mode would aid debugging outlines without stealing focus.
- The status lines are helpful but transient — an event log tail (last N transitions) would show what the reader saw before the current state.
- dock=true means it auto-docks — but it has no dock affordance story (no unread/attention state, no quick mute of -say). A seat-level "mute announcements" toggle would be the accessibility-respectful counterpart.
- Announce covers the focused node's name only — expand utterance templates per role (e.g. "button Save, enabled", "text field, 3 of 64 rows").

## Evidence

- `BOOTS=1 just gate go-a11y` → `artifacts/go-a11y-report.txt`: `vgate go-a11y: PASS (2/2 runs)` — markers incl. `goa11y: announce row 3 of 64`, `goa11y: say queued len=`, `gotabwm: hostint speak`, `host-speech: speak`.
- Code read: `user/go/goa11y/main.go`; manifest `image/apps.txt:68`; showcase `site/apps/goa11y.md`.
