The chat app over `virelai/infer` — a windowed conversation
that streams the engine's tokens, backed by the staged model or by
the deterministic stub the gate can pin without weights.
Conversations are files under `ASSIST/` published crash-safe, and
save_state carries the open thread's name and scroll row, so a
snapshot/restore re-opens the transcript mid-thread — the gate
boots fresh, restores, and lands a follow-up in the same
conversation. `go-assist.spec` owns that whole journey on the real
compute path. The tile is
[ICONASST](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconasst.qoi).
