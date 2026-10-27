<!-- iface-review:v1 app=GOREC.ELF issue=3695 -->
# App audit: Recorder (GOREC.ELF)

> Retired from polish audit issue #3695 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

`GOREC.ELF` ("Recorder", `user/go/gorec`) — captures the kernel's one
capture stream (v1 tuple S16/48 kHz/mono, slot 92) into a RIFF/WAVE
published crash-safe via `vi.WriteFileSafe`. Manifest row:
`GOREC.ELF | Recorder | r | dock=false | v=2 | caps=file,audio | icon=ICONREC`
— undocked; the launcher's "Recorder" row is a bare launch → the windowed
view; `exec GOREC.ELF <seconds> <out.wav>` is the headless form every
gate drives.

## Interface today

Observed (user/go/gorec/{main,view}.go):

- Windowed view (bare launch, M122c): a 480×320-native tabapp window —
  title "Recorder", one capture-capability line (`capture ready=… fmt=…
  rate=… ch=… ring=…` — the kernel's own answer verbatim), a "Record"
  button / `r` key that records a FIXED 5 s to a FIXED
  `/host/GOREC/CLIP.WAV` (overwritten each press), and a bounded
  10-line log teeing every `gorec:` line serial also gets. Esc/WIN_CLOSE
  exits; a second gesture while recording is refused on the window.
- Headless form: `gorec: cap` tuple line → bounded slot-92 ring loop →
  `gorec: wrote <path> seconds=… frames=… bytes=… sha256=…`; every
  refusal named (ENXIO/ETIMEDOUT/ENOSPC/EIO/EINVAL/ENAMETOOLONG) and a
  refused record leaves NO file. `gorec: done` terminates every path.
- During a record the window's event loop blocks on the synchronous
  capture — no live meter, no elapsed readout, no abort. (GOSOUND's
  `record` verb already proves the chunked-pump + live-meter pattern on
  the same capture seam.)

## Coverage

`live-sound-record` — **FAIL 1/2** (2026-10-25, `just gate
live-sound-record`):

- Run 01 (no `--sound` device): PASS — `cap ready=0`, `refused ENXIO
  at=0 of=192000`, `done`, no file left on the share.
- Assert-time SoundInProbe leg (`--sound-in --share`, host mic denied):
  PASS in its honest shape — `record leg: host mic denied — bounded
  ETIMEDOUT, no file (pass shape)`.
- Run 02 (bare launch → windowed view): FAIL — **spec defect, not an app
  defect**: the run boots with no `--screen` and never stages
  GOTABWM.ELF, so the app honestly prints `gorec: window refused` into a
  shim-composited boot. Filed as #3688.

## Observed defects

- #3688 — `live-sound-record` run 02 boots with no seat; the windowed
  leg can never pass as written.
- #3690 — the `GOREC.ELF` manifest row declares `caps=file,audio` but the
  windowed view needs `window`: every exec receipts
  `audit:window_denied` (would-be denial under ADR 0054 audit mode).
- #3691 — `gotabwm`'s `guessBin` has no `Recorder` row: ctrl-shift-t
  reopen / duplicate / session-restore would try `exec Recorder` →
  ENOENT (same class as observed for GOINFER on its gate).

## Improvement candidates

- **Fixed 5 s → fixed path, silently overwritten.** `viewSeconds=5` and
  `/host/GOREC/CLIP.WAV` are constants (view.go:30-37). A duration picker
  (1/5/10/30 s) and an auto-incremented `CLIP-N.WAV` (or a name field)
  would make it a real recorder rather than a proof rig.
- **No live meter / no abort while recording.** The capture blocks the
  event loop for the whole clip; GOSOUND's rec.go already has the
  chunked pump + live level + `s` stop on this same seam — porting that
  shape costs little.
- **No playback loop.** The view hints `clip ready — play it back with
  Sound` but offers no hand-off; an `opens=audio` exec of GOSOUND on the
  clip path closes the record→play loop in one line of code.
- **Log-only outcome.** Success/failure appears only as log lines — the
  window could show a persisted "last clip" card (name, seconds, sha,
  state) — the data already exists in the receipt string.
- **Windowed view has no mic-less teaching state.** With `cap ready=0`
  the Record button still invites a press that refuses; disabling the
  button with the named reason upfront (ENXIO) is one flag on the
  existing `capLine`.

## Evidence

```
$ bash tools/go/build-gorec.sh          # .build/go/GOREC.ELF (+ .stamp)
$ just gate live-sound-record           # FAIL 1/2 — run 02 is spec bug #3688
$ cat user/go/gorec/main.go user/go/gorec/view.go
$ grep GOREC image/apps.txt             # caps=file,audio (missing window)
$ cat site/apps/gorec.md site/includes/apps/gorec.story.md
```
