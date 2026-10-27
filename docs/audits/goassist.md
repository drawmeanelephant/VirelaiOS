<!-- iface-review:v1 app=GOASSIST.ELF issue=3735 -->
# App audit: Assistant (GOASSIST.ELF)

> Retired from polish audit issue #3735 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

GOASSIST.ELF is the chat app over the `virelai/infer` engine (M140b #2785): a windowed conversation that streams tokens from the staged model (or a deterministic PRNG stub for gates). Manifest row: `dock=false | v=2 | caps=file,window | icon=ICONASST` — launched via the seat palette or `exec`. Conversations are crash-safe files under `ASSIST/`; `save_state` carries the open thread's name and scroll row across snapshots.

## Interface today

Observed (code, `user/go/goassist/`):
- **Window** (`view.go`, 640x420): a 148px left rail listing conversations by display title with New / Rename (F2) / Delete (confirm) buttons; a new conversation opens a modal preset picker seeded from the store's `PRESETS.TXT` (up to 8 named system prompts). The transcript wraps, renders code fences in the mono face (TTF ui/mono chain when fonts are staged, bitmap degrade otherwise), bounded scrollback. A prompt field sends on Enter; Esc cancels an in-flight stream. IME pre-edit is supported (`goassist: ime *` markers). Pending events during a generation are stashed (128-event bound) so typing mid-answer is not lost.
- **Console journey**: `probe` runs one generate against the compute device and prints `goassist: probe ok toks=...`; `stub` opens the same window on synthetic weights; `model` (or bare) uses staged weights and honestly prints `goassist: weights absent` when the model isn't staged.

## Coverage

`tools/gate/specs/go-assist.spec` — PASS 10/10 (`BOOTS=1 just gate go-assist`, 2026-10-25, worktree t3-40900d1c). All legs green: real VIR1/Metal compute path (`gpu3d: armed`, `probe ok toks=8`), the windowed send/stream/save/commit journey on SynthStub, crash-restart with `state restored`, save/restore mid-thread, and the FONTS.TXT/NOTOCJK.TTF kana pre-edit leg (`fonts: notdef=0`). Rough reading: none. — legs: real VIR1/Metal compute path (probe + windowed send/answer), crash-restart with state restore, save/restore mid-thread; `vgate_repeat 3` in CI (audited here with BOOTS=1).

## Observed defects

None observed — 10/10 legs green; `goassist: unsupported`/`weights absent`/`error`/`cancelled` all correctly absent on the run.

## Improvement candidates

- The 148px sidebar is fixed; titles truncate silently — a wider/adjustable rail or tooltips would help once conversations accumulate.
- No transcript search or conversation filter — the rail is a plain list; a filter field (the palette pattern already exists in the seat) would scale past `convCap` 32.
- Code fences get the mono face but no other markdown structure (headings, lists, emphasis) — the renderer could grow a small subset since answers are model prose.
- No "regenerate" / "edit last message" affordance; correcting a prompt means a new turn in the same file.
- Preset choice is create-time only — no way to change a conversation's system prompt later.
- Streaming gives tokens but no visible typing indicator/thinking state beyond the growing answer; a status affordance (tokens/s, elapsed) would read better.
- `convCap` 32 rows is honest but low; the store could page or the rail could scroll-search.

## Evidence

- `gh issue list --search "Assistant GOASSIST" --state open` → no existing audit issue.
- `bash tools/go/build-goassist.sh` (+ gotabwm) → staged.
- `BOOTS=1 just gate go-assist` → `vgate go-assist: PASS (10/10 runs)` (spec honors `vgate_repeat 3 BOOTS`; audited at K=1 — CI runs K=3).
- Interface observed from `user/go/goassist/{main,view,conv,engine,presets,transcript}.go` — rail width, preset picker, F2 rename, bounded scrollback, IME markers; visual appearance inferred from layout code plus the spec's kana pixel leg.
