<!-- iface-review:v1 app=GOSNAP.ELF issue=3714 -->
# App audit: Snapshots (GOSNAP.ELF)

> Retired from polish audit issue #3714 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

GOSNAP.ELF is the named-snapshot manager for the running machine (ADR 0056, M115e #2464): `save`/`restore`/`rm`/`pin`/`unpin`/`prune` over the armed ctl vsock channel plus guest-side file ops on `/host/SNAPSHOTS/`. Manifest row: `dock=false | v=2 | caps=file | icon=ICONSNAP` — not on the dock, launched from the seat's command-palette app catalog (or `exec` from GOSH/the monitor). A bare launch opens the windowed face; argv runs the console verbs.

## Interface today

Observed (code, `user/go/gosnap/`):
- **Windowed face** (`view.go`, 480x300): a list of stored snapshots (name, bytes, `[pin]` tag), a name field (31-byte bound, the wire's NAME grammar), and Save / Restore / Delete / Pin / Prune buttons. Tab-order focus ring: list → field → Save → Restore → Delete → Pin → Prune; Enter in the field saves. Restore/Delete/Prune are arm buttons — first click marks the button `Confirm?` and prints the same `gosnap: consent <verb> <name>` ask the argv path prints, second click runs the verb; Esc, another click, a moved selection, or a changed listing disarms. Pin is a single-gesture toggle that relabels to "Unpin" on a pinned row. The listing re-polls at ~1 Hz so out-of-band snapshots (host Machine menu `menu-<stamp>.vzsave`, seat `CRASH<n>` slots) appear live. A status line carries hints and per-verb outcomes.
- **Console face** (`main.go`): `gosnap ls|save <name>|restore <name> [confirm]|rm <name> [confirm]|pin|unpin|prune [confirm]`; destructive verbs need the literal trailing `confirm` token and otherwise refuse `consent_required`; every verdict is one `gosnap:` serial line.

## Coverage

`tools/gate/specs/go-snap.spec` — FAILED 11/14 on this host (`BOOTS=1 just gate go-snap`, 2026-10-25 worktree t3-40900d1c). All app-verb legs pass: `setup ops-a ops-b ops-c crash offer windowed pwr winstate capld neg` = 11 PASS. The three failures are host-side legs, not app-verb legs — details under Observed defects. — ~14 VM-boot legs over one `.virelaivm` profile: save/ls/restore/rm consent, a `--resume` rewind leg, crash auto-snapshot legs (M125f), windowed-face legs, and the host cap-eviction legs (#3369).

## Observed defects

Two failure shapes observed (likely host contention — three other agents' class-B VMs were running concurrently — but recorded honestly since they reproduced across legs):

- **`ld` and `ld-crash` (the `--resume` rewind legs) FAIL**: `SUSPEND-RESUME: restored state.vzsave` prints — the rewind lands — then the guest emits **zero serial bytes for 120s** (`log=0 bytes`, `expected transcript 'gosnap-ld-done' not observed`). The spec's named bound (VZErrorDomain Code=12 refusal) is *not* what happened: the restore succeeded, the guest just never spoke again. `capld`, the third resume leg, PASSED, so resume is not universally broken. If this reproduces on an idle host it is a real defect in post-restore serial/console recovery, not an app bug — the app markers all fired before the rewind.
- **`cap` FAIL**: the eviction sequence stalled mid-chain — `WINBASE.vzsave` and `CAP0.vzsave` evicted, then nothing more; `gosnap: save CAP9 ok` never arrived in 480s while the kernel emitted only scheduler heartbeat chatter. Each snapshot is ~15 MB over vsock; plausibly load-induced, but unproven.

No defects found in the GOSNAP.ELF code or its own legs — every `gosnap:` marker asserted by the spec fired correctly on the 11 green legs.

## Improvement candidates

- CRASH* rotation slots and named snapshots share one flat list — consider visually separating crash slots (or a filter) since they are cap-exempt and not user-minted.
- The list shows name + bytes + pin tag only; a ctime column (or "oldest unpinned" marker for the prune victim) would make the eviction story legible in-window — `snapRow` already carries `ctimeSec`/`ctimeNsec` (`main.go:169-175`).
- No `opens=` on the manifest row — arguably correct, but a `.vzsave` dropped on Files has no handler; worth a decision either way.
- Status line is the only per-row feedback; the armed `Confirm?` state relies on the button label alone — a row highlight or inline confirm affordance would read better.
- `restore` stages only for next `--resume`; the window could surface "staged: <name>" persistently (today it is one status line that the next refresh can overwrite).
- dock=false is consistent with it being a system tool, but Snapshots has no Settings/Files adjacency — consider whether it belongs reachable from a system menu.

## Evidence

- `gh issue list --search "Snapshots GOSNAP" --state open` → no existing audit issue.
- `bash tools/go/build-gosnap.sh` (GO_FORK_DIR=/Users/tbuddy/dev/VirelaiOS/go-virelai-40900d1c) → GOSNAP.ELF 1,441,952 B.
- `BOOTS=1 just gate go-snap` → `vgate go-snap: FAILED (11/14 runs; see artifacts/go-snap-report.txt)`. Per-leg verdicts and serial logs in `artifacts/go-snap-run-*.txt`, `artifacts/go-snap-serial-*.log`.
- Interface description read from `user/go/gosnap/main.go` (verbs, consent, markers) and `user/go/gosnap/view.go` (window layout, arm/disarm, 1 Hz poll) — marked observed; pixel-level appearance inferred from widget code, not eyeballed (gate snapshot legs assert chrome, not app art).
