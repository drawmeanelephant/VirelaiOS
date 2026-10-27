<!-- iface-review:v1 app=GOSTORE.ELF issue=3711 -->
# App audit: Store (GOSTORE.ELF)

> Retired from polish audit issue #3711 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

`GOSTORE.ELF` ("Store", `user/go/gostore` over `virelai/store`) — the
store's human path: browse/search/detail/install/update/remove/rollback/
pin as a tab on the default seat, driving the same signed legs STORE.ELF
runs (verify → fetch → sha → publish → `.prev`) linked in-process.
Manifest row:
`GOSTORE.ELF | Store | i | dock=false | v=2 | caps=file,window,net | icon=ICONSTOR`
— undocked; launched via the launcher row (bare → windowed face) or
`exec GOSTORE.ELF [--remote=… --channel=… --kill-at=… --tear=…]`.

## Interface today

Observed (user/go/gostore/{main,app,render}.go):

- Window: 640×480-native tabapp tab, deliberately text-first — a header
  line (title + live `/` search), a 9-row browse list (one fixed-width
  truncated line per catalog row: stream, name, version, category,
  publisher/state, `[installed]` badge), an optional detail card (the
  `store info` field set, capped at 11 lines), a 9-line tee'd log pane
  (every `store:`/`gostore:` leg line), and a footer with key hints +
  status.
- Keyboard-only: `j`/`k`/arrows move, Enter toggles the detail card, `/`
  opens a search input (Enter applies, Esc cancels, Backspace edits),
  `l` reloads the catalog, `i` install / `u` update / `r` rollback /
  `d`elete / `p`in (prior signed head), `q` quit. **No mouse handling at
  all** — `main.go` dispatches only `EvKeyDown`.
- Modal consent: an install that wants grants stands a prompt —
  `[g]rant` writes the grant row via the admin slot, `[d]`/Esc declines;
  every other key is swallowed while the ask stands.
- Verbs act on `firstActionable` — the row preferring a search match,
  else the first actionable row — so a verb after a no-hit search lands
  on a fallback row, not on "nothing" (observed live: `/gohello` +
  `r` → `gostore: rollback badkey`).

## Coverage

`go-gostore` — **FAILING** (2026-10-25): 14-run spec covering
browse/search/detail/install, replica advance + update→v2, rollback→v1,
mid-publish kill survival, 6 refusal legs (elf/release/prev/state
mismatch, key_unknown+revoked, dep_missing), remove, pin, and
ReleaseDepends. Run 01 currently dies on `store: refuse
manifest_malformed` mid-install — the app's own code path is broken by a
stale constant (#3710); an earlier same-day attempt also ate a
host-side `com.apple.security.virtualization` entitlement flake when a
parallel `just gate` rebuilt VMRunner mid-run (#3689). The app itself
did open, declare, list, and refuse-by-name correctly wherever it got
that far.

## Observed defects

- #3710 — `user/go/store/manifest.go:14` `appsMaxBytes = 4096` vs the
  documented 8192 B budget: `image/apps.txt` is 5000 B, so
  `loadManifest` fails and every install/remove refuses
  `manifest_malformed` — breaks this spec's install legs outright.
- #3691 — `gotabwm` `guessBin` has no `Store` row (declared title
  "Store"): reopen/duplicate/restore execs `Store` → ENOENT.
- #3689 — parallel `just gate` in one worktree races `host/vm-runner/
  .build` (the first attempt's runs 01+02 died this way).
- Rough edge worth a look (code-observed, not gate): a verb after a
  no-hit search acts on the FIRST actionable row — `/gohello` then `r`
  issued `rollback badkey` (app.go firstActionable fallback). Honest
  refusal-by-name saved it, but a no-match search should probably
  disarm verbs instead.

## Improvement candidates

- **No mouse at all** — rows can't be clicked, the detail card can't be
  dismissed by pointer, no scroll. Row hit-testing + click-select and a
  scrollable list are table stakes for a 9-row window.
- **Fixed-width truncation loses information** — stream 8 / name 10 /
  version 7 columns (`pad()` in render.go) cut real values; detail card
  capped at 11 lines. A wider layout, wrapped description, or column
  priorities would carry the signed fields honestly.
- **Catalog icons exist but are never shown** — `icon_sha256` is in the
  release entry (detailLines prints the hash) yet no row/detail paints
  the raster. The launcher already decodes `icon=` rasters — the store
  could show the package icon per row.
- **Leg progress is log-text only** — sync→verify→fetch→sha→publish→
  prev deserves a real stepper/progress row; today the person reads a
  muted log pane.
- **No category filter / sort** — search is substring-only on stream or
  app name; no category facet, no installed-only filter, no column sort.
- **Search is invisible in rows** — matched text isn't highlighted, and
  non-matching nameless rows (sync-failed, catalog-invalid) crowd the
  filtered list (observed: the `/gohello` list still led with `badkey`).
- **Detail card has no actions** — install/update/rollback are keys on
  the list row, not buttons on the card that describes what they'd do.

## Evidence

```
$ bash tools/go/build-gostore.sh tools/go/build-store.sh \
    tools/go/build-flint.sh tools/go/build-gosh.sh \
    tools/go/build-go.sh tools/go/hello.go tools/go/build-gotabwm.sh
$ just gate go-gostore         # FAIL — run 01: manifest_malformed (#3710)
$ cat user/go/gostore/{main,app,render}.go
$ grep GOSTORE image/apps.txt  # caps=file,window,net
$ cat site/apps/gostore.md site/includes/apps/gostore.story.md
```
