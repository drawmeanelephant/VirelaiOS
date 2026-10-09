# Gostalgia overlays

The Gostalgia source tree stays pinned and unmodified
(`GOSTALGIA_PIN`, built via `tools/go/build-gostalgia.sh` which stages
`git archive` and applies these files). Every `.go` file here maps onto
the staged tree at the same relative path:

- a file at an **existing** path replaces it and must carry a
  `// pinned-sha256: <hex>` marker for the pinned original (checked
  before staging continues);
- a file at a **new** path is added and must carry `//go:build virelai`;
- `README.md` and non-`.go` files are never applied.

The adapter packages live in `user/go/gsport/`; these files are the thin
wiring from Gostalgia's platform hooks to them. `gsport/guard` walks both
trees and refuses POSIX routes (os/exec, os/signal, x/sys/unix, unix/tcp
net.Listen/Dial, syscall/vi outside `gsport/abi`).

## Adapter surface

One row per host assumption the pin makes that has no honest meaning on
VirelaiOS. Slots are ADR 0007 numbers.

| Gostalgia source site | Overlay file | gsport package | Slots | Refuses by name |
|---|---|---|---|---|
| `platform/ipc_unix.go` / `ipc_windows.go` — `ListenIPC` opens a unix socket or loopback TCP listener | `platform/ipc_virelai.go` | `gsport/ipc` | none (`net.Pipe`) | duplicate name; non-`mem://` endpoint |
| `platform/platform.go` — `DialIPC` switches `unix://`/`tcp://` | `platform/platform.go` (replace) | `gsport/ipc` | none | `unix://` and `tcp://` endpoints; unknown schemes |
| `platform/signals_unix.go` / `signals_windows.go` — `ShutdownSignals` returns SIGINT/SIGTERM | `platform/signals_virelai.go` | `gsport/sig` | none | the empty set itself — no async signal delivery exists |
| `internal/runtime` boot path + guest shutdown | — (callers wire `service.Context.Shutdown`) | `gsport/sig` | none | N/A — `Controller.RequestShutdown/Done/NotifyContext` seam for M95c input |
| `os.Open`/`WriteFile`/`Remove`/`Rename`/readdir call sites (services, runtime.json) | — (direct use, primitives only) | `gsport/fsys` | 23, 24, 25, 26, 27, 34, 35, 36, 77 | paths over kernel bounds (512/255/8); rename-over-live-target; mode bits as permission |
| `internal/process` child kind (`os/exec`) | — (primitives only; the child model is M95e's) | `gsport/proc` | 28, 7, 29 (+4 pacing) | argv/name over the exec block; pid ≤ 0; signalling other than kill |
| `time.Now`/`time.Since` call sites | — | none — fork runtime reads CNTPCT_EL0 (`nanotime1`) and slot 66 `sys_time`; no adapter needed (verified 2026-02: `src/runtime/os_virelai.go`) | — | — |

Later cards append their own rows (M95c terminal/termenv, M95d host VFS,
M95e child processes, M95f packaging).
