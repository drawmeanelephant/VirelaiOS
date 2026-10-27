<!-- iface-review:v1 app=GOSTALGIA.ELF issue=3697 -->
# App audit: Gostalgia (GOSTALGIA.ELF)

> Retired from polish audit issue #3697 into `docs/audits/` by #3793
> (2026-10-27). The interface-review lane reads this file; its
> last-reviewed rotation state lives as `iface-review` comments on
> the lane's state-tracker issue — see `tools/status/interface-review.py`.

## What it is

`GOSTALGIA.ELF` ("Gostalgia", built from the pinned `../gostalgia` tree
through `user/go/gsport` overlay adapters) — an entire foreign Go
userland shipped as one static ELF: apps, services, and a Bubble Tea v1
shell over `mem://` IPC. Manifest row:
`GOSTALGIA.ELF | Gostalgia | o | dock=false | v=2 | argv=shell --root /host/GS | caps=file,window,term,process | icon=ICONSTLG`
— undocked; launched via the launcher row (ctrl-space → filter →
Return), which execs `shell --root /host/GS` onto a GOTABWM-hosted
window tty.

## Interface today

Observed (`../gostalgia/internal/experience/shell/` + go-gostalgia
serial): a full-screen Bubble Tea TUI inside the hosted window.

- Header `G O S T A L G I A / PERSONAL COMPUTING, REIMAGINED` + a gold
  `● ONLINE` badge ("guest · C: environment drive"); a transcript body;
  a DOS-style prompt `C:\users\guest>` (cwd shown, truncated to
  `C:…` when wide); footer `F2 APPS · TAB COMPLETE · ↑↓ HISTORY ·
  PgUp SCROLL · Ctrl-C EXIT`.
- Verbs: `help`/`?`, `exit`/`quit`, `cls`/`clear`, `apps`,
  `launch`/`run`/`stop APP-ID`, `echo`, `ls`/`dir`/`cd`, `cat`/`type`,
  `ps`, `status`, `shutdown`, and raw `call METHOD [JSON]` RPC.
- F2 toggles an app shelf — rows `NAME ID vVERSION [READY|LIVE/PID]` +
  description + `GRANT` line; Enter launches, F3 stops, Esc returns.
- Bounded by design: transcript capped at 400 entries, history at 100,
  input cell-width-managed. On the gate: window tty 80×44 → 80×33 on
  font zoom; real HID drives it end to end.
- Home is durable: `/users/guest` mounts `/host/GS/home` via gsport/vfs
  HostFS (run 09 wrote `keep.txt`, run 10 read it back on a fresh boot).

## Coverage

`go-gostalgia` — **FAIL 6/10** (2026-10-25, `just gate go-gostalgia`):
runs 01–04 + 09 + 10 PASS (adapter gate, tty render/echo/`exit`, relayout
resize, text-grid readback via kernel selection→clipboard, durable-home
write+readback). Runs 06/07/08 FAIL on ONE stale assert — `launcher
filter q=gostalgia n=1` where the launcher honestly reports `n=2` since
the file-search row was added (#3692). Run 05 FAILs separately on
environment-fragile window-id/geometry pins + a `win leak` marker
(#3699). Also covered by `go-gostalgia-files` and `go-gostalgia-proc`
(not run here).

## Observed defects

- #3692 — go-gostalgia runs 06/07/08 pin `n=1`; the launcher's
  search-row makes it `n=2`. Spec staleness, not app breakage — the
  sessions themselves ran to `status=0`.
- #3699 — go-gostalgia run 05: chrome took window id=3 mid-run, the tty
  attached as id=2 at 80x24, `font large` re-rung to 64x18, and a
  `gotabwm: win leak id=3` marker appeared.
- #3691 — `gotabwm` `guessBin` has no `Gostalgia` row (tty.go:269
  declares title `Gostalgia`): ctrl-shift-t reopen would exec
  `Gostalgia` → ENOENT.

## Improvement candidates

- **No mouse/pointer at all** — the TUI is keyboard-only (Bubble Tea v1
  could take the window's mouse events; app-shelf selection by click is
  the natural fit).
- **Transcript scrollback is PgUp-only in the footer hint** and capped
  at 400 entries; no search-in-transcript; a copy-friendly select mode
  would help on the already-working clipboard seam (ctrl-shift-c is
  proven by run 04).
- **`help` is a text dump** — the app's own app-shelf UI (F2) is the
  better model: a `help` overlay as a second shelf page (verbs +
  grants) would be more discoverable.
- **Prompt truncation `C:…<tail>` loses the path head** — swap to
  fish-style `/u/g/something` abbreviation or a two-line prompt at
  narrow widths.
- **No completion for fs paths** — footer says `TAB COMPLETE` but it
  completes commands/app ids; `type <path>`-style args benefit most and
  aren't covered (worth verifying before speccing).
- **Window title is static `Gostalgia`** (gsport/tty.go:269) — echoing
  cwd or the running app there would match how tabs actually look
  elsewhere on the seat.
- **Shelf shows grants but not state changes** — `apps` output is
  LIVE/READY text; a subtle badge for "updated since last shelf open"
  would surface the mem:// IPC app's own lifecycle.

## Evidence

```
$ bash tools/go/build-gostalgia.sh gssmoke
$ bash tools/go/build-gostalgia.sh gostalgia
$ just gate go-gostalgia       # 01-05,09 PASS; 06/07/08 FAIL on #3692
$ sed -n '1,400p' ../gostalgia/internal/experience/shell/shell.go
$ grep GOSTALGIA image/apps.txt  # argv=shell --root /host/GS caps=…
$ cat site/apps/gostalgia.md site/includes/apps/gostalgia.story.md
```
