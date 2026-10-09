# Gostalgia module overlays (gostalgia-mods)

M95c (#2011): the `tools/go/overlay/gostalgia/` tree maps onto the
*Gostalgia source* stage; this tree maps onto the **Go modules in its
dependency closure** — the only place Bubble Tea v1's and its deps'
platform files can be adapted without editing the host module cache.

Layout: `gostalgia-mods/<module-path>@<version>/<file>` — each directory
names a module version present in the staged pin's `go.sum`. For every
directory, `tools/go/build-gostalgia.sh`:

1. resolves the pristine module through `go mod download -json`
   (versions the pin's go.sum already names — nothing new is fetched);
2. copies it to `.build/gostalgia-mods/<module>@<version>/`;
3. applies each `.go` file here onto that copy at the same relative
   path, under the same rules as the source overlay:
   - a file at an **existing** path replaces it and must carry a
     `// pinned-sha256: <hex>` marker matching the module-cache original;
   - a file at a **new** path is added and must carry `//go:build virelai`;
   - `README.md` and non-`.go` files are never applied.
4. re-verifies the staged copy against the cache snapshot (every file
   the overlay did not name must match byte-for-byte);
5. adds `replace <module> => <staged copy>` to the transient
   `.build/gostalgia.mod` — the module cache itself is never written.

Guard coverage: `gsport/guard` walks `tools/go/overlay/gostalgia*` —
that glob includes this tree — so no file here may import `virelai/vi`,
`os/exec`, `os/signal`, `syscall` or `x/sys/unix`. Kernel access in an
overlay file goes through `virelai/gsport/abi` and `virelai/gsport/tty`
like everywhere else.

## Surface

| Module | Files | What it adapts |
|---|---|---|
| `github.com/charmbracelet/bubbletea@v1.3.10` | `tty_virelai.go`, `signals_virelai.go` (added) | `initInput`/`openInputTTY` (no termios — `term.IsTerminal` is the bound-fd answer, `MakeRaw` an honest no-op), `suspendSupported=false`, `listenForResize` polling the ADR 0009 queue: WIN_RESIZE → `tea.WindowSizeMsg` in cells, WIN_CLOSE → the `gsport/sig` seam + `QuitMsg` |
| `github.com/charmbracelet/x/term@v0.2.1` | `term_other.go` (replace, retag `!virelai`), `term_virelai.go` (add) | The catch-all fallback would compile but return "not implemented" errors; the virelai file answers `IsTerminal`/`GetSize` through `gsport/tty` and makes the state calls honest no-ops |
| `github.com/muesli/termenv@v0.16.0` | `termenv_virelai.go` (add) | The js/plan9/aix fallbacks retagged: ANSI256 pinned, no terminal probing (mirrors the v2 `tools/go/overlay/charm` shim) |
| `github.com/mattn/go-isatty@v0.0.20` | `isatty_virelai.go` (add) | `IsTerminal`/`IsCygwinTerminal` → false (mirrors the v2 shim) |

Not needed and deliberately absent: `muesli/cancelreader` (its
`cancelreader_default.go` fallback already selects for virelai), and
bubbletea's `key_other.go`/`inputreader_other.go` (`!windows` files —
they select and run unchanged).
