<!-- iface-review:v1 app=GOSOUND.ELF issue=3693 -->
# App audit: Sound (GOSOUND.ELF)

> Retired from polish audit issue #3693 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

`GOSOUND.ELF` ("Sound", `user/go/sound`) — the `audio` mime's registered
handler: a streaming RIFF/WAVE player (PCM, float, IMA/DVI ADPCM) that
resamples through a 16-tap windowed sinc into the mixer's negotiated tuple
and plays via the slot-43 audio-stream seam, plus record/mic-meter faces
over the one capture stream. Manifest row:
`GOSOUND.ELF | Sound | a | dock=false | v=2 | caps=file,window,audio | opens=audio | icon=ICONSND`
— undocked; launched by the launcher's "Sound" row, by GOFILES' `type=audio`
open dispatch, or by `exec GOSOUND.ELF <file.wav>` /
`record <s> <out.wav>` / `meter [s]`.

## Interface today

Observed (code, user/go/sound/main.go + rec.go):

- Window: tabapp full-viewport tab, 16 px title band ("SOUND PLAYER:
  <file>"), then text rows: source tuple → output tuple, a `m:ss / m:ss
  state` position readout, a non-interactive Slider as the progress bar,
  a hint line `p/space play  s stop  q quit`, a `resample:` line, and a
  `VOL` row with a draggable gain slider (slot-44 stream gain).
- Keyboard: `space`/`enter`/`p` re-arm from stopped/done, `s` stops (also
  stops a live record/meter), `q`/`Esc` quits, `←`/`→` adjust volume ±5 —
  the arrows are implemented (main.go:632-640) but **not named in the
  hint line**.
- Pointer: volume slider drag only.
- Record face (`record`/`meter` verbs): mode line, live level rendered as
  the same read-only Slider, captured/target progress, state line —
  reached **only via argv**, never from the player's window.
- States: `stEmpty` shows `no file` + `exec GOSOUND.ELF /host/FILE.WAV`;
  refused files show `cannot play <name>` + the named reason; `no audio
  device` on a soundless boot (one line, clean exit).

## Coverage

`live-sound-playback` — **PASS 7/7** (2026-10-25, `just gate
live-sound-playback`, worktree t3-a6ca59fc @ f3f60003): beep/PCM stream
legs, GOFILES→GOSOUND dispatch of TONE.WAV + ADPCM.WAV + LONG.WAV with
peer-overlap `peak=2`, the no-device leg, and the notify-toast overlap.
Record mode is separately exercised by `go-dogfood` boot 07 (the ENXIO
refusal shape). Green and deep.

## Observed defects

None observed in gate output or code. (Two spec-level defects were found
elsewhere in this audit — #3688, #3692 — and a manifest caps gap affecting
GOSOUND's siblings, #3690; GOSOUND's own row declares `window` correctly.)

## Improvement candidates

- **Bare launch is a dead end.** A launcher click (no argv) parks on
  `no file / exec GOSOUND.ELF /host/FILE.WAV` — an instruction the person
  can't act on from a window. GOREC's bare launch opens a working face;
  GOSOUND's should open its recorder/meter face or an "open a file"
  affordance (or the row should not exist — the M122c rule).
- **No pause.** `s` stops and re-`p` restarts from frame 0; there is no
  pause/resume even though the stream seat could hold position. Seek is
  absent too — the progress bar is deliberately read-only, but the
  bounded parser could skip to a data offset.
- **Record/meter reachable only by argv.** The window has no key or
  button into modeRecord — a person can't record from the player at all.
  A `r` toggle into the existing drawRec face is nearly free (the pump
  already exists).
- **Volume keys unhinted.** `←`/`→` ±5 work but the hint line says only
  `p/space play  s stop  q quit` (main.go: line under the position bar).
- **No "play the file I just recorded" loop.** Symmetric with GOREC's
  `clip ready — play it back with Sound` hint — neither side hands the
  WAV across even though `opens=audio` is the dispatch seam.
- Container list is WAV-only; `ogg`/`flac`/`mp3` get a clean
  `<container>: no decoder` refusal, but the window doesn't say what IS
  supported.

## Evidence

```
$ bash tools/go/build-gosound.sh        # .build/go/GOSOUND.ELF
$ just gate live-sound-playback         # PASS 7/7 (228s)
$ sed -n '1,1006p' user/go/sound/main.go user/go/sound/rec.go
$ cat image/apps.txt | grep GOSOUND     # caps=file,window,audio opens=audio
$ cat site/apps/gosound.md site/includes/apps/gosound.story.md
```
