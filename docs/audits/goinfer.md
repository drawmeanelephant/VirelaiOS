<!-- iface-review:v1 app=GOINFER.ELF issue=3696 -->
# App audit: LLM Infer (GOINFER.ELF)

> Retired from polish audit issue #3696 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

`GOINFER.ELF` ("LLM Infer", `user/go/goinfer` over `virelai/infer`) —
guest LLM inference on the GPU-compute path (ADR 0053): matmuls + vocab
softmax ride `sys_gpucomp`, norms/RoPE/residuals/sampling stay on CPU.
Manifest row:
`GOINFER.ELF | LLM Infer | l | dock=false | v=2 | caps=file | icon=ICONINFR`
— undocked; bare launch (the launcher row) is the windowed face;
`exec GOINFER.ELF stub` and `model [path]` are the console journeys.

## Interface today

Observed (user/go/goinfer/{main,view,host}.go):

- Windowed view (bare launch, M122c): 560×340-native tabapp window —
  "LLM Infer", two buttons (`Run stub` / model run), 's'/'m' keys, Esc
  closes, a bounded 12-line log teeing every `goinfer:` line. One
  compute session per process is a hard bound: after a journey the
  buttons report `session spent — close and relaunch GOINFER.ELF to run
  again`.
- Journeys: `stub` = deterministic PRNG-weights model, K=5 complete
  load→generate→free lifecycles on one session, fixed prompt, pinned
  answer; `model [path]` = owner-approved SmolLM2 VIW from the share,
  one stream-and-seal load (sha256 receipt), K=5 greedy decodes of a
  fixed ChatML prompt, licence receipt per run. Receipts land in
  `/host/GOINFER/RECEIPT.TSV`; no device → `goinfer: unsupported`, NO
  receipt.
- The windowed face is an appver-1 carrier: the seat's close-time state
  ask round-trips armed journey + weights path + 12-line log tail;
  restore re-paints them and runs nothing.

## Coverage

`go-infer` — **FAIL 0/1** (2026-10-25, `just gate go-infer`), but the
failure is downstream of the app: every GOINFER-owned assert passed —
5/5 pinned `ids=` runs, `stub consistent K=5`, the byte-exact golden
transcript as an ordered subsequence, RECEIPT.TSV verified, `goinfer
OK`, `goinfer: done`, plus the seat's `state ask/save ver=1` and `tab
close`. The run then died on the seat's reopen leg:
`gotabwm: reopen missing LLM_Infer` — filed as #3691. `go-infer` is the
app's first owning spec; the stub journey previously ran only
incidentally under `live-gpu3d`.

## Observed defects

- #3691 — `gotabwm` `guessBin` has no `LLM Infer` row: reopen execs the
  window title (`reopen missing LLM_Infer`) — kills this spec's
  close→reopen→restore leg.
- #3690 — the manifest row declares `caps=file` but the windowed face
  needs `window`: the run emitted `audit:window_denied` ×35 under ADR
  0054 audit mode (allowed today, `caps_denied` on `enforce`).

## Improvement candidates

- **Fixed prompts only.** Both journeys run one card-pinned prompt
  (`stubPrompt` / `modelPrompt` constants); there is no input field —
  the app is a proof rig, not a tool. A prompt box + streaming decode
  into the log is the difference between demo and app.
- **Session-spent = relaunch by hand.** The window knows the session is
  spent and says so — a `Relaunch` affordance (re-exec self) or a seat
  verb would close the loop the message only describes.
- **No generation progress.** A model run blocks: load_ms and the answer
  appear only at the end; the VIW staging loop and greedy decode could
  each drive a progress line (bytes staged, tokens emitted).
- **Model path is console-only in practice** — `model [path]` argv
  works, but the windowed face has no path field/chooser; drag-and-drop
  of a VIW (it declares no `opens=`) or a path line would help.
- **Receipts invisible in-app.** RECEIPT.TSV rows (ids, submits,
  licence, sha, resident) never surface in the window — a third log
  column or a receipt card would show them where the click happened.
- **Esc closes with no guard** mid-run — 's' during a journey is locked
  out by `busy` but Esc isn't; a confirm-over-close for an in-flight
  journey would prevent losing a long model run.

## Evidence

```
$ bash tools/go/build-goinfer.sh        # .build/go/GOINFER.ELF
$ just gate go-infer                    # FAIL 0/1 — reopen leg, defect #3691
$ cat user/go/goinfer/main.go user/go/goinfer/view.go
$ grep GOINFER image/apps.txt           # caps=file (missing window → #3690)
$ cat site/apps/goinfer.md site/includes/apps/goinfer.story.md
```
