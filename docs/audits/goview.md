<!-- iface-review:v1 app=GOVIEW.ELF issue=3716 -->
# App audit: Image Viewer (GOVIEW.ELF)

> Retired from polish audit issue #3716 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

The image viewer — the `opens=image` default handler: QOI/PNG/JPEG/GIF/SVG through the shared `webrender` decoder (deliberately not a second decoder), MOV/Motion-JPEG through its bounded box demux (with its soun audio track since M202b). Manifest row (image/apps.txt:50): `GOVIEW.ELF | Image Viewer | v | dock=false | v=2 | opens=image | icon=ICONVIEW`. Launches via drop/open of an image (the opens= family + URL schemes per the manifest schema), `exec GOVIEW.ELF /host/PIC.QOI [-play]`, and the catalogue.

## Interface today

Observed from `user/go/view` and the live-image-viewer run output:

- The window sizes itself to the image before it opens (header read first — the gate asserts `gview: open id=N 328x264` for a 160x120 fixture); the title carries `name WxH fmt zoom%`.
- Keys (view.go:26-47): `=`/`-` zoom (table 25/33/50/66/100/150/200/300/400/600/800%), `0` reset, arrows pan, `,`/`.` frame step, space play/pause, `c` copy file bytes to the typed clipboard (mime from magic, ≤512 B kernel bound), `s` save-as picker (appkit FilePicker, clamped into the window), `q`/Esc quit; left-drag pans.
- Animated GIF honours per-frame delays; `a11y.reduce_motion` (startup read + live settings-bus flip, M177a/M197c) parks playback on frame 1 — space still plays on explicit request; `-play` arms playback at open.
- Checkerboard alpha backdrop; drop target takes image/* payloads (`gview: drop` receipts for the refused shapes too); empty/error states paint the staged spot art (SPT_IMG/SPT_ERR, wired under #3348) and keep the window open — VIEW.BIN's don't-panic rule.
- Subscribes theme + a11y.contrast + a11y.reduce_motion over the seat's settings bus; MOV playback reports `video frames=… elapsed_ms=…` + audio drift markers.

## Coverage

- `live-image-viewer.spec` — 12 legs (4 runs × warm-checkpoint repeats), **8/12 PASS** on this host (artifacts/live-image-viewer-gate.txt): the whole format spread green — QOI/PNG/JPEG/GIF/SVG decodes, zoom/pan/step markers, the MOV step+play legs with pinned per-frame sums, the codec refusal (`unsupported MOV codec=avc1`), Adam7 IPNG on legs 03-01/02.
  - Leg 03-03 failed the IPNG pixel assert with 0.000 sample match: the saved scanout shows the window's client area uniformly dark — `gview: present`/`ready` printed but the pixels never reached the framebuffer before capture. Same stale-present signature as the go-help flake; legs 01/02 passed the identical assert, so reads as capture-vs-present lag under concurrent VM load on this host, not a decode regression.
  - Legs 04-01/02/03 (the MOV+soun drift bound) all failed: `drift_ms` grew 1340→6425 ms across loop receipts with underruns 31→115 — the guest could not feed the negotiated 48 kHz stream at realtime on this loaded host (video loops ran 1–2.5× long too). Consistent across all three boots, so a real pacing regression cannot be excluded; needs a quiet-machine rerun to disambiguate.

## Observed defects

None confirmed in the app. Two gate-level observations (observed failures, inferred cause): the IPNG 0%-match leg (stale present at capture — app markers all honest) and the MOV+audio drift blowout (load-starved feed, or a real pacing issue — the one failure class here that a rerun must confirm). No named app refusals appeared; every refusal shape printed honestly.

## Improvement candidates

- No next/prev file navigation: opening one image leaves its directory's siblings unbrowsable — `n`/p over the share directory's image rows is the most-missed viewer verb (the manifest's opens=image makes GOVIEW the natural home for it).
- No fit-to-window: the zoom table steps fixed percentages and `0` resets to 100% — a `fit` mode (letterbox to viewport) is the conventional third state.
- No rotate/flip — modest code (the demux/decode path is already shared) for a real usability gain on camera-derived JPEGs.
- Title-only status: format/dims/zoom live in the window title, which a narrow tab truncates — a one-line footer (GOTOP's hint-line shape) would survive.
- Frame/playback state is serial-marker only — a small on-canvas "frame i/n" badge while stepping would surface it in the UI.
- `s` save-as exists; a matching "reveal in GOFILES" verb would connect the two apps (GOFILES already opens GOVIEW via opens=).
- Slideshow mode for multi-image directories once next/prev lands.

## Evidence

- `just gate live-image-viewer` — FAIL 8/12 (artifacts/live-image-viewer-gate.txt, report + serials; 03-03 snapshot and 04 drift logs kept under the vgate run dir).
- `image/apps.txt:50`; `user/go/view/{main,view,mov}.go`; `site/apps/goview.md` + `site/includes/apps/goview.story.md`; `docs/component-registry.md:79`; `docs/app-refresh.md:70`; issue #3348 (staged-art seams now wired for SPT_IMG/SPT_ERR).
