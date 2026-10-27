<!-- iface-review:v1 app=GOEMOJI.ELF issue=3700 -->
# App audit: Emoji (GOEMOJI.ELF)

> Retired from polish audit issue #3700 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The emoji picker — a fuzzy-searchable grid of every glyph the kernel's emoji font covers, plus compose pairs and the kaomoji dozen; Enter copies a pick as `text/plain` to the typed clipboard lane. Launches from the seat's launcher / `exec GOEMOJI.ELF`; manifest row `GOEMOJI.ELF | Emoji | : | dock=false | v=2 | caps=file,window | icon=ICONEMOJ` (image/apps.txt:64). Also summonable inside other apps via ctrl+shift+E (M146b chord).

## Interface today

Observed from `user/go/goemoji/main.go` + gate serial/snapshot evidence:

- One window, native 512x384 at (32,32), scaled on resize (`tabapp.Layout` — draw and hit-test share one geometry).
- Top strip: three tabs — Emoji / Compose / Kaomoji — plus a "stay" checkbox; a search field ("search names" placeholder, 64-byte bound) under it; content region; a status line naming the selection ("U+1F525 fire — Enter copies  F10 pins") and a `n/total` match count.
- Emoji tab: 6-column grid of 80x58 cells, each painting the kernel's 16x16 monochrome bitmap art at 2x with the glyph name beneath; empty query shows pinned favorites then recents rows (M152c shelf). Compose and Kaomoji tabs are list rows.
- Keyboard: text goes to the search field; arrows navigate (grid-aware ±gridCols on Emoji, ±1 on lists); Home/End; Enter picks; Escape clears a non-empty query (does not quit); Tab cycles tabs; F10 pins/unpins the focused cell.
- Pointer: click selects a cell, clicking the selected cell picks; click toggles stay / switches tabs.
- A pick writes `text/plain` to the typed clipboard (`vsys.ClipboardSetTyped`); close-on-pick unless stay is on. A hosted uid_user app is refused slots 38/39 (`clip refuse rc=-7`) — only `exec -u0` or a capable seat path lands the pick (policy boundary verified by run 01).
- Shelf state (favs + recents + tab/stay/query) rides the save_state v2 envelope; recents accumulate only in stay mode or across a seat restore — a named limitation.

## Coverage

`tools/gate/specs/go-emoji.spec` — ran `BOOTS=1 just gate go-emoji` on this worktree: **6/7 runs PASS**. Runs 02–07 green (bare-shim picks incl. pixel probes of the real 16x16 art, the GOEDIT paste leg, the ctrl+shift+E chord journey, the shelf snapshot/restore pair, and the M158b enforce leg). Run 01 FAILs on `launcher filter q=goemoji n=1` — the seat now prints `n=2` because the M191b search hand-off row counts in the filter total. Filed as #3698.

## Observed defects

- No app-side defects observed in gate output or code.
- Gate-side: the stale `n=1` launcher assert above (#3698) — a spec drift bug, not an app bug.

## Improvement candidates

- The grid paints the kernel's monochrome 16x16 bitmap emoji at 2x — the design loop (#3341) could ship color/raster emoji art; cells have room for a larger preview.
- Recents evaporate on close-on-pick (named limitation): persist the shelf to a small file under `/host/GOEMOJI/` so recents survive ordinary use, not just seat snapshots.
- Single-click could pick directly; today a first click selects and only a second click on the same cell picks — two gestures for the common case.
- No visible hint that Tab cycles tabs or that arrows navigate; the status line is the only affordance text. A one-line hints footer (like GOVAULT's two-line verb hints) would help discovery.
- Search matches names/aliases but the UI never shows which alias matched — surfacing the matched alias in the status line would teach the vocabulary.
- The "stay" checkbox is pointer-only; give it a key (e.g. ctrl-s free? F9?) for parity with the rest of the keyboard model.

## Evidence

- `BOOTS=1 just gate go-emoji` → `artifacts/go-emoji-report.txt`: 6/7 PASS; run 01 single-assert FAIL (`launcher filter q=goemoji n=1` → printed `n=2`); serial logs `go-emoji-serial-0{1..7}.log`.
- Snapshot leg run 02 verified grid art on scanout: "emoji grid: title band ok, selection ring Accent, smile art + carved eyes ink, fire cell ink"; kaomoji clipboard read-back verified host-side (the shrug's exact bytes).
- Code reads: `user/go/goemoji/main.go` (715 lines), `table.go`, `shelf.go`, `search.go`; manifest row `image/apps.txt:64`; showcase `site/apps/goemoji.md`.
