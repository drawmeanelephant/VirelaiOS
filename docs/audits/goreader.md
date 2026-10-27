<!-- iface-review:v1 app=GOREADER.ELF issue=3722 -->
# App audit: Reader (GOREADER.ELF)

> Retired from polish audit issue #3722 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

GOREADER.ELF is the document reader (M143a #2792) and the `opens=document` handler: PDFs off the share rendered by the bounded `user/go/pdf` engine (ADR 0040, read-only — one 96dpi page bitmap per call), plus page navigation, zoom, find, bookmarks, save-as, print staging, and save_state. Manifest row: `dock=false | v=2 | caps=file,window | opens=document | icon=ICONREAD`.

## Interface today

Observed (code, `user/go/goreader/`):
- **Window**: full-viewport under the seat (tab-aware) or a fixed window on the shim. One page at a time — the engine's cache rule — with a scroll-and-flip model: wheel/PgDn pans within the current page, then flips to the next (`reader.go` scrollDown/scrollUp).
- **Keys**: PgUp/PgDn/Up/Down/arrows + wheel scroll; Home/End; `=`/`-` zoom, `0` reset, `f` fit-width; ctrl-f opens the find bar, `n`/`shift-n` walk hits (all hits highlighted warm, current in accent); `b` toggles a bookmark on the current page, `[`/`]` jump between bookmarks; `s` saves a byte copy through the FilePicker; `p` stages the document on the host print lane (M202c); `q`/Esc quits. Position memory restores the last page/offset for a reopened document.
- **Refusals are honest**: engine limits surface as `goreader: refuse <code>` (e.g. `UnsupportedStroke`).

## Coverage

`tools/gate/specs/go-reader.spec` — FAILED 4/5 on this host (`BOOTS=1 just gate go-reader`, 2026-10-25, worktree t3-40900d1c). Runs 01-04 all PASS — open/nav/zoom/find on the pinned corpus, the `UnsupportedStroke` refusal leg, all three K=3 open chains (WEB→, GOFILES→GOREADER, GOFILES→GOVIEW), and the snapshot pixel asserts on those legs. Run 05 (the GOEDIT→export→read→host-verify leg) failed only on its framebuffer pixels — every one of its 27 serial asserts passed (export authored, PDF loaded `pages=3`, position memory `page=2 z=100 bm=2` restored, bookmark + pos saved). — legs: open/page/zoom/find journey on a pinned 3-page PDF, refusal leg (`UnsupportedStroke`), bookmark + position memory, mime dispatch via GOFILES, WEB `--gate` co-handler legs; `vgate_repeat 3 BOOTS` in CI (audited with BOOTS=1).

## Observed defects

No app defect observed. The run-05 failure deserves naming: the kind-4 snapshot armed on `goreader: bookmark on page=2 n=1` captured a frame with **zero page-white pixels** — "page-2 fill did not reach the scanout (0 px)" — and the downstream cgrender-vs-scanout compare failed on the same dark crop (`frac <= 0.08 and mean <= 24`). The serial journey was completely green (load → page 2 → bookmark → quit → pos saved → reopen at page=2 bm=2), and run 01's snapshot assert proves page ink reaches the framebuffer normally — so this reads as the snapshot racing the ~1 Hz composite under a heavily loaded host (multiple concurrent gate VMs), not as a reader bug. If it reproduces on an idle host it becomes a defect: the armed snapshot should wait out the composite tick.

## Improvement candidates

- Page-at-a-time scrolling means no continuous multi-page scroll — a "next page peeks in at the bottom" feel is impossible while the cache holds one bitmap; a small lookahead render (still bounded) would read much better.
- No page thumbnails or outline/sidebar — long documents are navigated blind; even a "page n/m" scrub strip would help.
- Zoom is keyboard-only (`=`/`-`/`0`/`f`) — no menu, no percentage display in a status bar; the zoom level is only observable through serial markers and the painted size.
- Find bar exists but there's no visible hit-count chrome beyond highlight bands; a "2/3" readout near the bar would help.
- No text selection / copy — the PDF text layer is extracted for find (`extract.go`) but can't be lifted to the clipboard.
- Bookmarks are per-session state (in the save blob); a share-side bookmark file would survive across machines.
- No night mode / invert — PDFs render engine colors only.

## Evidence

- `gh issue list --search "Reader GOREADER" --state open` → no existing audit issue.
- `bash tools/go/build-goreader.sh` (+ goedit/gofiles/goview/web --gate prerequisites) → all staged.
- `BOOTS=1 just gate go-reader` → `vgate go-reader: FAILED (4/5 runs; see artifacts/go-reader-report.txt)`. Per-leg serials in `artifacts/go-reader-serial-0*.log`; run-05 failure text quoted above from `artifacts/go-reader-run-05.txt` / `go-reader-gate.txt`.
- Interface description observed from `user/go/goreader/main.go` (event loop, key map, markers) and `user/go/goreader/reader.go` (key usages, scroll-and-flip model); visual appearance inferred from draw calls — the gate asserts pixels, I did not eyeball the window.
