<!-- iface-review:v1 app=GOCLOCK.ELF issue=3733 -->
# App audit: Clock (GOCLOCK.ELF)

> Retired from polish audit issue #3733 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

GOCLOCK.ELF is the clock app (M144a #2794, M152b): four tabs — World, Stopwatch, Timer, Alarm — multiplexed by one scheduler onto the process's single kernel one-shot app timer. Manifest row: `dock=false | v=2 | caps=file,window,timer | opens=text/calendar | icon=ICONCLK` — palette/exec launch, and the `.ics` hand-off target. Headless `import`/`export` verbs merge/write VCALENDAR through WriteFileSafe.

## Interface today

Observed (code, `user/go/goclock/`):
- **Window** (640x400): a tab strip (World | Stopwatch | Timer | Alarm) switchable by Left/Right/Tab or clicks. World is a row list of every `clockfmt.Offsets` zone with the settings-local zone first (`YYYY-MM-DD HH:MM:SS UTC±HH:MM` rows). Stopwatch has Start/Pause + Reset buttons and Space/'s'/'r' keys, counting monotonic `vsys.Nanotime` (wall steps never move it). Timer has a preset row arming 1/3/5/10/25 min on digit keys 1-5 plus Arm/Cancel buttons and an `'e'` custom-seconds entry. Alarm is up to 7 rows (daily or once, on/off toggle), Daily/Once arm buttons, four-digits+Return adds a daily ('o' a once), Up/Down pick a row, Space toggles, 'e' edits in place (M193b), 'x' deletes.
- **Fired deadlines** raise a seat toast via `vi.Notify` and print `goclock: fired <id>`; daily alarms re-arm 86400s out; once alarms flip their row off.
- **Persistence**: alarm rows ride both the seat save_state blob and a crash-safe share file; the wall-vs-monotonic split is deliberate.

## Coverage

`tools/gate/specs/go-clock.spec` — PASS 14/14 (`BOOTS=1 just gate go-clock`, 2026-10-25, worktree t3-40900d1c). All legs green: World-tab pixels, countdown→toast→click-through, stopwatch+alarm surface, save/restore halves, reboot-with-stale-record, M152b arm/edit legs, `.ics` import/export legs, and the snapshot probes. Rough reading: none. — ~12 legs: World tab pixels, countdown→toast→click-through, stopwatch+alarm surface, save/restore halves, reboot-with-stale-record, arm/edit legs, `.ics` import/export legs.

## Observed defects

None observed — 14/14 legs green.

## Improvement candidates

- World tab lists EVERY zone — with a small zone catalog that's long; a picker/add-remove model (like Settings' timezone choice) would beat a fixed exhaustive list.
- No analog face anywhere — the World tab is text rows; even a small analog local clock would give the app a visual identity matching its icon.
- Timer presets are hardwired to 1/3/5/10/25 — no way to save a custom preset; 'e' custom entry isn't remembered.
- No lap/split support on the stopwatch — Start/Pause/Reset only.
- Alarms can't be labelled — a row is bare HH:MM + daily/once; naming ("wake", " meds") is the classic ask.
- No snooze on a fired alarm — the toast fires and the alarm re-arms or switches off.
- Tab strip + buttons is mouse/keys only; there's no menu bar, so import/export and per-tab hints are invisible — a one-line key hint per tab would help discovery.
- caps include `timer` but there is no hourly chime / bell setting; `audio` isn't declared so no alarm sound choice (chime via toast only).

## Evidence

- `gh issue list --search "Clock GOCLOCK" --state open` → no existing audit issue.
- `bash tools/go/build-goclock.sh` (+ gotabwm/gosh prerequisites) → staged.
- `BOOTS=1 just gate go-clock` → `vgate go-clock: PASS (14/14 runs)` (spec honors `vgate_repeat 3 BOOTS`; audited at K=1 — CI runs K=3).
- Interface observed from `user/go/goclock/{main,model,paint,sched,alarms,import}.go` — tab names, preset row, alarm-row verbs, and the wall-vs-monotonic split are from code; World-tab pixel asserts (`goclock: painted epoch=`, zone rows) confirm the face reaches the framebuffer.
