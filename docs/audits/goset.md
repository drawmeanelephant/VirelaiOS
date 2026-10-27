<!-- iface-review:v1 app=GOSET.ELF issue=3705 -->
# App audit: Settings (GOSET.ELF)

> Retired from polish audit issue #3705 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The settings panel: the Go editor for the schema-v2 `/host/SETTINGS.TXT` the seat and kernel already read — every save a crash-safe publish (temp + fsync + rename), a corrupt file refused whole. Manifest row (image/apps.txt:49): `GOSET.ELF | Settings | g | dock=false | v=2 | caps=file,window,audio | icon=ICONSET`. Launched via the catalogue, `exec GOSET.ELF`, and GOWELCOME's Settings launcher button.

## Interface today

Observed from `user/go/goset` and the go-settings run output:

- 560x360 window; a six-cell tab strip (Settings / Services / Grants / Shortcuts / Profile / Access) where the active cell IS the view — clicks activate, Left/Right steps when the strip owns focus; a bounded focus ring walks tabs → row list → input → save → quit.
- Settings tab: rows of the in-force table (file rows + kernel-table keys the file omits + accepted-unseeded rows like idle_minutes/notify_dnd/timezone/keyboard_layout); click a row or Left/Right cycles its vocabulary, `key=value` + Enter applies and saves, bare Enter saves, Esc backs out/quits; `theme=custom` reveals palette_fg/bg/accent hex rows + a live swatch band.
- Services tab: `name=on|off` lines publish a service's config safely. Grants tab: consent rows for signed ReleaseCaps (Right=grant writes, Left=declines). Shortcuts view: the compiled chord registry, read-only — toggled by the app's own registered chord ctrl+shift+h (M82c, re-bound off ctrl+shift+s when the seat's snapshot arm took it, M81g). Profile view: read-only machine identity via the ctl channel (slot 93). Access view: a11y.* toggles + text scale.
- Unknown keys a file carries survive a save byte-for-byte but are not offered for editing; every write path names its refusal rather than going silent.
- Typed verbs (`services`, `grants`, `profile`, `access` + Enter) switch tabs from the input line — a quirk: they're the same line that writes `key=value`, so the tab names are reserved words in practice.

## Coverage

- `go-settings.spec` — 5 runs, **4/5 PASS** on this host (artifacts/go-settings-gate.txt): the pointer-press row toggle persisted to the share, the access tab's `a11y.contrast=high` + `a11y.reduce_motion=on` sets and saves verified on SETTINGS.TXT, the DarkHC token probe on the composited workspace (run 03 snapshot), and the remaining legs green. Run 02 failed on a non-GOSET dependency: the leg's tail drives GOCLOCK's timer (`right,right,e,3,return` after `goclock: painted`) and `goclock: timer armed 3` / `gotabwm: notify` never arrived within the 300 s timeout — every GOSET assert in the leg passed. The spec's own comments document this input-routing/focus race class (see the run-02 header note, observed 2026-10-21).

## Observed defects

- Stale comments only: `user/go/goset/input.go:158` and `user/go/goset/main.go:74` still say "ctrl+shift+s" for the shortcuts chord — the registry row and code moved to ctrl+shift+h (chords/table.go:251); the status-line hints are correct. Comment drift, not behaviour.

## Improvement candidates

- The display table already needs a bounded second bank past 24 rows (app-refresh #3171) — a `/`-style key filter over row names (GOTOP/GOHELP have the pattern) scales better than banking.
- Palette editing is hex-text only; the swatch band previews but there is no visual picker — sliders or a hue field would fit the app's own vocabulary-cycling model.
- No unsaved-changes indicator: typed `key=value` applies to the display table and Enter saves; a "dirty" marker on the save affordance would make the two-step model visible.
- The Shortcuts view is read-only — a rebind UI (write back to the registry's settings-backed rows, if any become writable) is a bigger card but the view already names the gap.
- Services/Grants rows are text-only; the staged art conveyor (#3338) could give services enabled/disabled glyphs.
- Access tab could preview text scale live (a sample line at the chosen scale) rather than describing it.

## Evidence

- `just gate go-settings` — FAIL 4/5, all GOSET asserts green; run 02's tail lost its injected chords (GOCLOCK timer marker never fired; see report).
- `image/apps.txt:49`; `user/go/goset/{main,input,rows,tabs,layout,controls,grants,services}.go`; `user/go/chords/table.go:251`; `site/apps/goset.md` + `site/includes/apps/goset.story.md`; `docs/component-registry.md:78`; `docs/app-authoring.md:312-325`.
