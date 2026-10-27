<!-- iface-review:v1 app=WEB.ELF issue=3732 -->
# App audit: Web (WEB.ELF)

> Retired from polish audit issue #3732 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The browser — Go chrome (window, navigation, history, fetch) over the project's own `webrender` engine: HTML parse → bounded CSS cascade → box layout → native paint. Deliberately Dillo-tier: HTTP/1.1 keep-alive with chunked + gzip bodies, in-process TLS 1.3, real DNS, no JavaScript and no POST (ADR 0028's permanent refusal). Docked via `image/apps.txt` (`WEB.ELF | Web | w | dock=true | v=2 | caps=file,window,net | opens=http,https | icon=ICONWEB`) — the only app declaring `net`. Implementation: `user/go/browser` (~11k LOC), 512x384 window at 40,28, title "Web".

## Interface today

Observed from `user/go/browser/main.go` + siblings (`history.go`, `bookmarks.go`, `findbar.go`, `downloads.go`, `forms.go`, `zoom.go`, `saveas.go`, `savepdf.go`, `editor.go`, `histmenu.go`, `stores.go`) + `live-web.spec`:

- Chrome is app-drawn inside the window, below the kernel's 16px title band (`main.go:44-59`): title row, URL row with back/fwd/reload chips (`backX/fwdX/reloadX` hit boxes) and an editable URL field (`web: url-focus`); a status row at the bottom. Content viewport is declared by marker (`web: viewport css=1280x720 x=0 y=64 w=512 h=288 s=0`).
- Navigation: URL entry (256-byte argv slots; `@/host/LINK` reads the URL's first line from a file), back/forward chips, a history dropdown (`histmenu.go`), bookmarks (`bookmarks.go`), reload; `web: nav`/`nav-ready` markers gate each transition. PDFs dispatch to GOREADER rather than navigating.
- Page surface: find-in-page (`findbar.go`), per-tab zoom (`zoom.go`), forms (`forms.go` — get forms submit; the M93f `[x]` checkbox overlay is an owner-authorized golden exception), cancellable downloads (`downloads.go`), Save As HTML + Save PDF (`saveas.go`, `savepdf.go` — the M202c print lane), stores: history/bookmarks/cookies/cache/downloads persisted (`stores.go`).
- Keyboard/mouse: clicks hit-test chips, links, menus, and the modal picker (`click()` `main.go:1433+`); `?` opens the M121c hint overlay (40 rows observed); the F10/Alt appkit menubar exists (`web: menu open` / `menu Chord hints` markers in run 61).
- Fonts: truetype faces (inter + firacode + serif + Noto Naskh/Hebrew for bidi), `font-size medium base=13`, `text_scale=100` honoured from a11y settings.
- Networking: real DNS (`vi.ResolveDNS`, `network.go:72`), TLS 1.3 in-process with the gate-roots trust store, bounded budgets (`budget.go`; `web: budget startup-ms=` markers, `budget over` refusal).

## Coverage

`tools/gate/specs/live-web.spec` — local run: **FAILED (30/60 runs, 1414 s)**. A sibling worktree's independent run on branch `agent/webf9/head-resume` failed the same evening with **29/60 and the identical signature** — so this is a systematic gate break on current main, not a worktree flake. Legs that passed include boot/exec, file navigation, the corpus golden (M93f composited hash matched byte-exact), shim console-ink, menu/hints, downloads, history navigation.

## Observed defects

Two clusters, reproduced identically across two worktrees — filed as #3731:

- **The entire M93g/M93e reference family rendered a blank viewport.** Runs 22–37 (`exec WEB.ELF /host/<REF>.HTML`) all passed their serial asserts (`parse nodes=`, `layout blocks=`, `paint items=14`, `repaint items=`, `ready`, correct `web: viewport` marker) yet every golden-crop compare failed: the saved actual PNG is a uniform `#1a1a2e` — the `WallpaperTop` token (`user/go/theme/theme.go:68`), i.e. the crop captured desktop wallpaper, not the window's content. Runs 01 and 11 failed their snapshot asserts the same way; run 14's corpus golden PASSED in the same session, so the pipeline works when the window presents. The kernel's own serial records `open: id=2 owner=1 rect=40,28 512x384 ws=0` — the window is exactly where the crop expects — so the surface never composites in these boots. Roughly half the snapshot legs fail, with slight run-to-run variance in which, which reads timing-sensitive. (Serial log: `artifacts/live-web-serial-27.log`; diff PNGs in `artifacts/m93g/diffs/`; the sibling's `*-actual.png` files are the same uniform `#1a1a2e`.)
- **The https peer legs saw no requests.** Runs 15–17's python checks failed on `assert "request GET /wiki/Harbor HTTP/1.0" in peer` — the standin TLS responder (`standin_responder.py` on ports 24560–24562, per-run minted certs) logged nothing even though the app's serial markers show `web: tls en.wikipedia.org virelai-gate-roots-2026-09-14` and `web: nav https://…` succeeding from its side. Plausible causes: handshake completing but the responder's log format diverged, a port collision with another agent's concurrent live run, or the fetch silently falling back.

## Improvement candidates

- **Chrome is geometry constants in code** (`main.go:44-60` hit boxes) — the back/fwd/reload chips are painted cells, not the staged toolbar art; the design loop (#3341) could give Web real chrome art the same way dock tiles got icons.
- **URL bar is single-line text** — no autocomplete/dropdown suggestions from history+bookmarks (the stores exist; `histmenu` is a separate dropdown).
- **No POST** is a standing ADR refusal, but forms that need POST get no named refusal UI — a "POST not supported" banner would beat a silent dead submit (`forms.go`).
- **Tabs don't exist in-app** — one window = one page; the seat's tabs are the multiplex answer (same as GOTERM).
- **No reader mode / text-only toggle** despite the Dillo-tier render already being close.
- **Zoom is per-tab but there's no persisted default** — a settings key would help.

## Evidence

- `just gate live-web` → `vgate live-web: FAILED (30/60 runs)`; `artifacts/live-web-{gate,report}.txt`, `artifacts/m93g/diffs/*.png` (all `*-actual.png` uniform `#1a1a2e`), `artifacts/live-web-serial-*.log`; rerun queued.
- `grep`/`sed` reads of `user/go/browser/*.go`, `image/apps.txt`, `site/apps/web.md`, `site/includes/apps/web.story.md`, `tools/gate/specs/live-web.spec` (runs 22–38 at `:1131+`).
- Dedupe: `gh issue list --search "browser" --state open` → no existing audit card (#3684 is a fetch-perf card, unrelated).
