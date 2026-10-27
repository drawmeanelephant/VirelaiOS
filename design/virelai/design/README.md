# virelai/design — figcraft sheets

Design-iteration documents for the VirelaiOS interface — what the shipped UI
looks like and where it's going. These are **docs, not delivered assets**:
they live here rather than under `art/` so the public gallery stays artwork.
The sheets themselves are #1262 (seat shell), #1263 (key app screens), #1264
(tokens + primitives).

## Conventions

- One directory per sheet: `virelai/design/<sheet>/<sheet>-V*R*.*` — the same
  revision rule as art (`AGENTS.md`): never overwrite, refinements bump `R`,
  new directions bump `V`.
- Committed per revision: the **native** + `.svg` + `@2x.png`; the `.pdf`
  proof stays local in `work/out/`. The native is `.figcraft` when drawn in
  FigCraft, `.vectorcraft` when drawn here — either is whitelisted in
  `.gitignore`. Exported PNGs ride along for review.
- Driving scripts follow the art convention: `work/virelai/draw_<sheet>.py`.
- Frame sizes are **real window sizes** — seat-shell frames at 1280×720, app
  frames at the app's declared size (`user/go/<app>/main.go` declare block).
- Sheets cite [`../notes/style-bible.md`](../notes/style-bible.md) and
  `docs/ui-primitives.md` for tokens instead of restating them — the token
  sheet (#1264) is the design-system-of-record later sheets reference.
- Sheets may embed delivered assets by reference (`art/virelai/<slug>/`)
  rather than redrawing them.
