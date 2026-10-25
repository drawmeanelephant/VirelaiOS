# Component registry (M189a, #3225)

One table of every shipped component: name, kind, binary, the one version
surface that answers "what version is it", the owning gate spec or contract,
and its state. Checked by `tools/tests/test_component_registry.py`, which
runs in `just verify-portable` — a row that disagrees with `image/apps.txt`,
with a spec pin it cites, or with the release grammar fails the build.
The registry is also what the public site renders: `site/components.md`
carries a generated region (M189c, #3227) produced by
`tools/render-component-site.py`, re-rendered at publish time, and the
docs gate fails a PR that lets `image/apps.txt`, this file, or the
rendered page drift apart.

## The version rule

"Version" names exactly one contract surface per kind, and that surface
only. A shipped **app**'s version is its seat-stored state envelope
`appver` — the `StateVersion` the app registers with the tabapp harness
in its package under `user/go/`; an app that never saves state has no
version surface and shows `—`. A **store release**'s version is the
signed `ReleaseVersion` const in the head release manifest of its FLINT
stream (ADR 0055 D2) — that versions the release, never the app binary
itself. A **kernel service** or shared-format **contract**'s version is
the ADR number that pins it. The **kernel image**'s version is the
`osver` build stamp (`-Dos-version`, `kernel/src/osver.zig`). A
**shared library** has no version of its own unless it implements a
contract — then the contract's ADR is the surface. A pinned external
**host tool**'s version is its pin file.

Beside that contract surface, every component carries a product version
`ver M.m.p` — a semver-shaped const in the component's own source that
the drift check resolves the same way it resolves `StateVersion` today
(#3463): a `user/go` package carries `const AppVersion`, a Zig source
carries `pub const version`, a producer script carries `VER=`, a Go
library dir carries `const Version`, a host tool its own const. The
`ver` token sits comma-separated beside the kind's contract surface —
`appver`/`ADR`/`osver`/`pin` stay what they are; renaming them would
lie. Two exemptions: kernel-service rows, whose version IS the owning
ADR (the kernel image is the one exception — the product image itself
carries `ver` beside `osver`), and boris, whose pin file is the
surface.

Bump rule: everything seeds `ver 1.0.0` — no fake history; the refresh
ledger already carries touch dates. A PR that substantively touches a
component bumps patch; a feature-adding card bumps minor; a major bump
is an explicit owner decision.

Scope: every `image/apps.txt` row, the seat and the tools the session
stages beside it, the generated ELF fixtures, the shared libraries under
`user/go/` and `user/src/lib/`, the kernel services that own a contract,
the host tools the system cannot boot or publish without, and the gate
fleet — every `tools/go/build-*.sh` target that builds and is staged by
an owning class-B spec but never reaches the session share (the fleet
audit, issue #3397). What stays unlisted is what no `build-*.sh` target
produces: a gate-only Zig .BIN that `build.zig` emits for one spec's
share belongs to that spec, not to this table.

## Shipped apps (image/apps.txt)

Every row below is a manifest row — `app` means "has an `image/apps.txt`
entry", which is also what the drift check enforces in both directions.
All of them additionally answer to the schema-v2 manifest grammar
(`image/apps.txt` header).

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
| 64-bit Calc | app | GOCALC.ELF | appver 1, ver 1.0.0 | tools/gate/specs/go-calc.spec | shipped |
| Text Editor | app | NOTE.ELF | appver 1, ver 1.0.0 | tools/gate/specs/go-note.spec | shipped |
| Code Editor | app | GOEDIT.ELF | appver 1, ver 1.0.0 | tools/gate/specs/go-edit.spec | shipped |
| Files | app | GOFILES.ELF | appver 1, ver 1.0.0 | tools/gate/specs/go-fileman.spec | shipped |
| Terminal | app | GOTERM.ELF | appver 2, ver 1.0.0 | tools/gate/specs/go-term.spec | shipped |
| Web | app | WEB.ELF | appver 1, ver 1.0.0 | tools/gate/specs/live-web.spec | shipped |
| Pulse | app | PULSE.ELF | ver 1.0.0 | tools/gate/specs/go-top.spec | shipped |
| Shell | app | GOSH.ELF | appver 1, ver 1.0.0 | tools/gate/specs/go-sh.spec | shipped |
| Help | app | GOHELP.ELF | ver 1.0.0 | tools/gate/specs/go-help.spec | shipped |
| Welcome | app | GOWELCOME.ELF | appver 1, ver 1.0.0 | tools/gate/specs/go-welcome.spec | shipped |
| Top | app | GOTOP.ELF | appver 1, ver 1.0.0 | tools/gate/specs/go-top.spec | shipped |
| Settings | app | GOSET.ELF | ver 1.0.0 | tools/gate/specs/go-settings.spec | shipped |
| Image Viewer | app | GOVIEW.ELF | ver 1.0.0 | tools/gate/specs/live-image-viewer.spec | shipped |
| Sound | app | GOSOUND.ELF | ver 1.0.0 | tools/gate/specs/live-sound-playback.spec | shipped |
| Tabbed Desktop | app | TABWM.BIN | ver 1.0.0 | tools/gate/specs/live-tabwm.spec, ADR 0034 | retained-fallback |
| Gostalgia | app | GOSTALGIA.ELF | ver 1.0.0 | tools/gate/specs/go-gostalgia.spec | shipped |
| Store | app | GOSTORE.ELF | ver 1.0.0 | tools/gate/specs/go-gostore.spec | shipped |
| LLM Infer | app | GOINFER.ELF | appver 1, ver 1.0.0 | tools/gate/specs/go-infer.spec | shipped |
| Recorder | app | GOREC.ELF | ver 1.0.0 | tools/gate/specs/live-sound-record.spec | shipped |
| Snapshots | app | GOSNAP.ELF | appver 1, ver 1.0.0 | tools/gate/specs/go-snap.spec | shipped |
| Assistant | app | GOASSIST.ELF | appver 2, ver 1.0.0 | tools/gate/specs/go-assist.spec | shipped |
| Sheet | app | GOSHEET.ELF | appver 2, ver 1.0.0 | tools/gate/specs/go-sheet.spec | shipped |
| Reader | app | GOREADER.ELF | appver 2, ver 1.0.0 | tools/gate/specs/go-reader.spec | shipped |
| Clock | app | GOCLOCK.ELF | appver 1, ver 1.0.0 | tools/gate/specs/go-clock.spec | shipped |
| Mines | app | GOMINE.ELF | appver 2, ver 1.0.0 | tools/gate/specs/go-games.spec | shipped |
| Solitaire | app | GOSOL.ELF | appver 2, ver 1.0.0 | tools/gate/specs/go-games.spec | shipped |
| Emoji | app | GOEMOJI.ELF | appver 2, ver 1.0.0 | tools/gate/specs/go-emoji.spec | shipped |
| IRC | app | GOIRC.ELF | appver 3, ver 1.0.0 | tools/gate/specs/go-irc.spec | shipped |
| Vault | app | GOVAULT.ELF | appver 1, ver 1.0.0 | tools/gate/specs/go-vault.spec | shipped |
| Mail | app | GOMAIL.ELF | ver 1.0.0 | tools/gate/specs/go-mail.spec | shipped |
| Accessibility | app | GOA11Y.ELF | ver 1.0.0 | tools/gate/specs/go-a11y.spec | shipped |
| Paint | app | GOPAINT.ELF | appver 1, ver 1.0.0 | tools/gate/specs/go-paint.spec | shipped |

## Seat and staged tools

The session share (`tools/session.sh`) stages these beside the manifest:
the Go seat that is the boot default, the persistence tools the app-bound
credentials name, the shell set `help` advertises plus the audit's other
named tools (issue #3397's table; staged by #3417), and the generated
fixtures the exec/dyn-link gates run. None is a catalog app, so none
carries an `appver` column entry — the seat owns the envelope, it does
not ride in one.

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
| Tabbed desktop seat | seat | GOTABWM.ELF | ADR 0033, ver 1.0.0 | tools/gate/specs/go-wm-seat.spec, ADR 0033 | shipped |
| OS updater | tool | GOUPD.ELF | ver 1.0.0 | tools/gate/specs/live-os-update.spec | staged |
| Agent daemon | tool | AGENTD.ELF | ver 1.0.0 | tools/gate/specs/go-agentd.spec, ADR 0044 | staged |
| Diff tool | tool | GODIFF.ELF | ver 1.0.0 | tools/gate/specs/go-godiff.spec | staged |
| Ping | tool | GOPING.ELF | ver 1.0.0 | tools/gate/specs/live-n1-ping.spec | staged |
| Web fetch | tool | GOFETCH.ELF | ver 1.0.0 | tools/gate/specs/go-fetch-https.spec | staged |
| Netstat | tool | GONETSTAT.ELF | ver 1.0.0 | tools/gate/specs/live-netstat.spec | staged |
| DNS lookup | tool | GODNS.ELF | ver 1.0.0 | tools/gate/specs/live-n5-dns.spec | staged |
| Traceroute | tool | GOTRACEROUTE.ELF | ver 1.0.0 | tools/gate/specs/live-n7-traceroute.spec | staged |
| Git client | tool | GOTGIT.ELF | ver 1.0.0 | tools/gate/specs/go-git.spec | staged |
| SSH client | tool | GOSSH.ELF | ver 1.0.0 | tools/gate/specs/live-ssh-endpoint.spec | staged |
| SSH server | tool | GOSSHD.ELF | ver 1.0.0 | tools/gate/specs/live-ssh-server.spec | staged |
| HTTP server | tool | GOHTTPD.ELF | ver 1.0.0 | tools/gate/specs/live-httpd.spec | staged |
| WASM runtime | tool | WASM.BIN | ver 1.0.0 | docs/wasm-import-contract.md, tools/gate/specs/live-wasm.spec | staged |
| QuickJS engine (now session-staged, was gate-only) | tool | QJS.BIN | ADR 0039, ver 1.0.0 | ADR 0039, tools/gate/specs/live-quickjs.spec | staged |
| App-manifest report script (card d / index #3334) | script | SCRIPTS/REPORT.JS | ADR 0039, ver 1.0.0 | tools/gate/specs/live-quickjs.spec | staged |
| Zig userland root | tool | USER.BIN | ver 1.0.0 | ADR 0034 | retained-fallback |
| Sexiburger desktop | tool | SEXIBURG.BIN | ver 1.0.0 | tools/gate/specs/live-sexiburger.spec, ADR 0034 | retained-fallback |
| Hello fixture | fixture | HELLO.ELF | ver 1.0.0 | tools/mkhello-elf.py, tools/gate/specs/go-hello.spec | fixture |
| Crash fixture | fixture | CRASH.ELF | ver 1.0.0 | tools/mkhello-elf.py, tools/gate/specs/live-crash-viewer.spec | fixture |
| Dyn-link fixture set | fixture | DYNAPP.ELF | ver 1.0.0 | tools/mkdyn-elf.py, tools/gate/specs/live-dynamic-linking.spec | fixture |

## Gate fleet — built and gate-staged, not on the session share

Every `tools/go/build-*.sh` target that is neither an `image/apps.txt`
row nor staged by `tools/session.sh`, sorted by the fleet audit (issue
#3397 — the classification table lives there). `staged` on these rows
means the owning class-B spec copies the binary onto its gate share;
none of them rides the session share a daily boot sees. `tool` rows are
real programs driven by `exec`; `fixture` rows exist to serve their
spec. The build-go.sh family is one row — eighteen generated fixtures
sharing one builder and one naming map.

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
| Init service supervisor | tool | INIT.ELF | ver 1.0.0 | tools/gate/specs/live-boot-order.spec, ADR 0042 | staged |
| R3D renderer | tool | GOR3D.ELF | ver 1.0.0 | tools/gate/specs/go-r3d.spec | staged |
| RSS reader | tool | RSS.ELF | ver 1.0.0 | tools/gate/specs/go-rss.spec | staged |
| Git UI | tool | GOGITUI.ELF | ver 1.0.0 | tools/gate/specs/go-gitui.spec | staged |
| Crash viewer | tool | CRASHVIEW.ELF | ver 1.0.0 | tools/gate/specs/live-crash-viewer.spec | staged |
| Log viewer | tool | LOGVIEW.ELF | ver 1.0.0 | tools/gate/specs/go-selftest.spec | staged |
| Selftest runner | tool | GOSELF.ELF | ver 1.0.0 | tools/gate/specs/go-selftest.spec | staged |
| Observe CLI | tool | OBSERVE.ELF | ver 1.0.0 | tools/gate/specs/live-observe.spec | staged |
| Profiler | tool | PROF.ELF | ver 1.0.0 | tools/gate/specs/live-observe.spec, tools/gate/specs/live-n12-netprof.spec | staged |
| Syscall tracer | tool | STRACE.ELF | ver 1.0.0 | tools/gate/specs/live-strace.spec | staged |
| Heap inspector | tool | HEAP.ELF | ver 1.0.0 | tools/gate/specs/go-stress.spec | staged |
| FLINT release CLI | tool | FLINT.ELF | ADR 0055, ver 1.0.0 | tools/gate/specs/go-flint.spec, ADR 0055 | staged |
| Guest go-build driver | tool | GOBUILD.ELF | ver 1.0.0 | tools/gate/specs/live-gobuild.spec | staged |
| Guest compiler | tool | GOCMDCOMPILE.ELF | ver 1.0.0 | tools/go/build-gotool.sh, tools/gate/specs/live-gobuild.spec | staged |
| Guest linker | tool | GOCMDLINK.ELF | ver 1.0.0 | tools/go/build-gotool.sh, tools/gate/specs/live-gobuild.spec | staged |
| Release pipeline | tool | GORELEASE.ELF | ADR 0055, ver 1.0.0 | tools/gate/specs/live-release.spec, ADR 0055 | staged |
| Store install CLI | tool | STORE.ELF | ver 1.0.0 | tools/gate/specs/live-install-consent.spec | staged |
| Audio-seam proof | fixture | FART.ELF | ver 1.0.0 | tools/gate/specs/go-fart.spec | fixture |
| Term-graphics fixture | fixture | IMGCAT.ELF | ver 1.0.0 | tools/gate/specs/live-term-graphics.spec | fixture |
| Panic fixture | fixture | CRASHFIX.ELF | ver 1.0.0 | tools/gate/specs/live-crash-viewer.spec | fixture |
| Log fixture | fixture | LOGFIX.ELF | ver 1.0.0 | tools/gate/specs/go-selftest.spec | fixture |
| Charm/Bubble Tea seam proof | fixture | CHARMHELLO.ELF | ver 1.0.0 | tools/gate/specs/go-charmhello.spec | fixture |
| Symbol-donor fixture | fixture | GOEDITSYM.ELF | ver 1.0.0 | tools/gate/specs/live-observe.spec | fixture |
| Profile workload fixture | fixture | PROFFIX.ELF | ver 1.0.0 | tools/gate/specs/live-observe.spec | fixture |
| Instrumented compiler fixture | fixture | GOCMDPROFILE.ELF | ver 1.0.0 | tools/go/build-prof.sh, tools/gate/specs/live-observe.spec | fixture |
| Trace workload fixture | fixture | TRACEFIX.ELF | ver 1.0.0 | tools/gate/specs/live-strace.spec | fixture |
| Heap workload fixture | fixture | HEAPFIX.ELF | ver 1.0.0 | tools/gate/specs/go-stress.spec | fixture |
| Heap budget fixture | fixture | HEAPBUDG.ELF | ver 1.0.0 | tools/gate/specs/go-stress.spec | fixture |
| Gostalgia child fixture | fixture | GSCHILD.ELF | ver 1.0.0 | tools/gate/specs/go-gostalgia-proc.spec | fixture |
| Gostalgia proc fixture | fixture | GSPROC.ELF | ver 1.0.0 | tools/gate/specs/go-gostalgia-proc.spec | fixture |
| Stress storm fixture | fixture | GOSTORM.ELF | ver 1.0.0 | tools/gate/specs/go-stress.spec | fixture |
| Supervise fixture | fixture | SVFIX.ELF | ver 1.0.0 | tools/gate/specs/live-supervise.spec | fixture |
| Tabapp harness fixture | fixture | GOTABAPP.ELF | ver 1.0.0 | tools/gate/specs/go-tabapp.spec | fixture |
| Appkit demo fixture | fixture | GODEMO.ELF | ver 1.0.0 | tools/gate/specs/go-wm-journey.spec | fixture |
| Big-math fixture | fixture | BTMATH.ELF | ver 1.0.0 | tools/gate/specs/go-btmath.spec | fixture |
| GPU triangle proof | fixture | GPUTRIANGLE.ELF | ver 1.0.0 | tools/gate/specs/live-gpu3d.spec | fixture |
| Composition probe | fixture | GOCOMP.ELF | ver 1.0.0 | tools/gate/specs/live-m14-composition.spec | fixture |
| Progress-wire driver | fixture | GOPROGRESS.ELF | ver 1.0.0 | tools/gate/specs/go-wm-seat.spec | fixture |
| WM-RPC probe | fixture | WMRPCPROBE.ELF | ver 1.0.0 | tools/gate/specs/go-agentd.spec, tools/gate/specs/go-wm-tabs.spec | fixture |
| build-go.sh fixture set (GOHELLO/GOARGS/GOROUT/GOSCALE/GOSTRESS/GOPANIC/GOWIN/GONET/GOVINET/GOVIDNS/GOBIG/GOLOADBSS/GOREAD/GONETRATE/GOMBURST/GOSYSCALL/GOBIGREAD/GOSELFHOST) | fixture | — | ver 1.0.0 | tools/go/build-go.sh, tools/gate/specs/go-hello.spec | fixture |

### The windowed-candidates verdict (issue #3455)

The audit's four `*`-marked candidates were judged on their landed,
gate-proven usage — one promotion, three named refusals:

- **GOPAINT.ELF — promoted.** Its v2 row is not new config: the spec's
  drop leg ran `GOPAINT.ELF | Paint | P | dock=false | v=2 | opens=image`
  gate-locally for every drag-admission leg, and the signed
  ReleaseManifestRow the store journey installs spells the same row. It
  is a real interactive windowed app (brush/line/rect/fill/erase tools,
  undo, QOI save + PNG export, drop-open, `appver 1` save_state), so the
  row moved to `## Shipped apps` and `image/apps.txt`; the spec's share
  setup now asserts the upstream row instead of appending it.
- **GOR3D.ELF — refused.** The window exists to own the golden-match
  viewport the spec snapshots (`tools/gate/specs/go-r3d.spec`): the
  program draws one fixed scene, then its event loop answers only close
  and the first resize (`user/go/r3d/main.go`). No argv, no `opens=`
  surface, no state. A desk tile would launch a static picture of the
  fill-batcher proof — the proof is already its honest row.
- **RSS.ELF — refused.** A bound-tty tea program (the bound-window tty
  shape): every proven launch is `exec RSS.ELF` on a share seeded with
  RSS.OPML/RSS.CACHE and a configured net (`tools/gate/specs/go-rss.spec`).
  A manifest row would promise a no-arg, unseeded desk launch no gate has
  ever run, and a TUI's honest desk face is `exec` inside a terminal,
  not a launcher tile.
- **GOGITUI.ELF — refused.** Same bound-tty shape, and its proven
  invocation is argv-bound — `exec GOGITUI.ELF /host/R`
  (`tools/gate/specs/go-gitui.spec`). A session share carries no
  guaranteed repository path, so the tile would open on a legible error
  rather than a desk face.

## Staged art — the f-artcraft conveyor

Signed f-artcraft drops adopted by `tools/art/adopt.py` into `image/art/`
and staged on the session share under `/host/ART/`. The manifest
(`image/art/ART.TXT`) is the canonical row set — it pins each asset's
source slug, share name, ship size, and both SHA-256s; rows here group
the manifest by family and say which seams consume them. Assets are not
binaries, so the binary column stays `—`.

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
| Cursor set (10 masks + ship hotspots — arrow paints; context selection is #3348 b) | asset | — | — | image/art/cursors/MASKS.TXT, image/art/ART.TXT, tools/art/mkcursormask.py | staged |
| Mascot poses (wave + locked paint via GOWELCOME/lock curtain; slep/thnk/celb/oops unwired) | asset | — | — | image/art/ART.TXT | staged |
| Spot icons (doc + nres paint via EmptyState.Spot; six unwired — see #3348) | asset | — | — | image/art/ART.TXT | staged |
| Spinner sheets (arc + mascot — unwired; no spinner widget yet) | asset | — | — | image/art/ART.TXT | staged |
| Boot plate (plt + emb — unwired; kernel render_splash cannot read /host) | asset | — | — | image/art/ART.TXT | staged |
| Filetype icons (10 — paint in appkit.FilePicker rows; GOFILES list grid is text) | asset | — | — | image/art/ART.TXT | staged |
| App icons (30 — pending the f-artcraft icon worker's apps.txt `icon=` seam) | asset | — | — | image/art/ART.TXT | staged |

## Shared libraries — user/go

Importable packages under `user/go/` (every directory whose `.go` files
are not `package main`). No version surface of their own unless one is
listed; the contract column names the ADR or spec their behaviour answers
to where one exists.

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
| appkit | lib | — | ver 1.0.0 | ADR 0011 | shipped |
| art | lib | — | ver 1.0.0 | — | shipped |
| chords | lib | — | ver 1.0.0 | — | shipped |
| clockfmt | lib | — | ver 1.0.0 | — | shipped |
| css | lib | — | ver 1.0.0 | ADR 0028 | shipped |
| draw | lib | — | ver 1.0.0 | ADR 0011 | shipped |
| ed25519 | lib | — | ver 1.0.0 | ADR 0023 | shipped |
| fatimg | lib | — | ver 1.0.0 | ADR 0055 | shipped |
| find | lib | — | ver 1.0.0 | — | shipped |
| flate | lib | — | ver 1.0.0 | — | shipped |
| gpu3d | lib | — | ver 1.0.0 | ADR 0053 | shipped |
| heap | lib | — | ver 1.0.0 | — | shipped |
| hostint | lib | — | ver 1.0.0 | ADR 0051 | shipped |
| httpc | lib | — | ver 1.0.0 | — | shipped |
| ical | lib | — | ver 1.0.0 | — | shipped |
| icons | lib | — | ver 1.0.0 | — | shipped |
| ime | lib | — | ver 1.0.0 | — | shipped |
| infer | lib | — | ver 1.0.0 | — | shipped |
| layout | lib | — | ver 1.0.0 | — | shipped |
| linebuf | lib | — | ver 1.0.0 | — | shipped |
| mdns | lib | — | ver 1.0.0 | — | shipped |
| mime | lib | — | ver 1.0.0 | — | shipped |
| observe | lib | — | ver 1.0.0 | ADR 0043 | shipped |
| outbox | lib | — | ver 1.0.0 | ADR 0051 | shipped |
| pcm | lib | — | ver 1.0.0 | ADR 0046 | shipped |
| pdf | lib | — | ver 1.0.0 | ADR 0040 | shipped |
| pdfproof | lib | — | ver 1.0.0 | ADR 0040 | fixture |
| pdfwrite | lib | — | ver 1.0.0 | ADR 0040 | shipped |
| printq | lib | — | ver 1.0.0 | ADR 0051 | shipped |
| prof | lib | — | ver 1.0.0 | — | shipped |
| rfb | lib | — | ver 1.0.0 | ADR 0045 | shipped |
| settings | lib | — | ver 1.0.0 | tools/gate/specs/live-settings.spec | shipped |
| shlib | lib | — | ver 1.0.0 | ADR 0044 | shipped |
| snapshot | lib | — | ver 1.0.0 | — | shipped |
| speech | lib | — | ver 1.0.0 | ADR 0051 | shipped |
| sshlib | lib | — | ver 1.0.0 | ADR 0025 | shipped |
| store | lib | — | ReleaseVersion, ver 1.0.0 | ADR 0055 | shipped |
| strace | lib | — | ver 1.0.0 | — | shipped |
| supervise | lib | — | ver 1.0.0 | ADR 0042 | shipped |
| svcgraph | lib | — | ver 1.0.0 | ADR 0042 | shipped |
| svcmanifest | lib | — | ver 1.0.0 | ADR 0042 | shipped |
| svg | lib | — | ver 1.0.0 | ADR 0041 | shipped |
| svgproof | lib | — | ver 1.0.0 | ADR 0041 | fixture |
| tabapp | lib | — | ver 1.0.0 | ADR 0033 | shipped |
| teatty | lib | — | ver 1.0.0 | ADR 0020 | shipped |
| theme | lib | — | ver 1.0.0 | — | shipped |
| timesync | lib | — | ver 1.0.0 | ADR 0060 | shipped |
| tls | lib | — | ver 1.0.0 | ADR 0029 | shipped |
| ttf | lib | — | ver 1.0.0 | — | shipped |
| vcard | lib | — | ver 1.0.0 | — | shipped |
| vector | lib | — | ver 1.0.0 | ADR 0041 | shipped |
| vectorconsumer | lib | — | ver 1.0.0 | ADR 0041 | fixture |
| vaultlib | lib | — | ver 1.0.0 | — | shipped |
| vi | lib | — | ver 1.0.0 | — | shipped |
| vsockctl | lib | — | ver 1.0.0 | ADR 0056 | shipped |
| vsys | lib | — | ver 1.0.0 | — | shipped |
| webrender | lib | — | ver 1.0.0 | ADR 0028 | shipped |
| webstyle | lib | — | ver 1.0.0 | ADR 0028 | shipped |
| widgets | lib | — | ver 1.0.0 | ADR 0011 | shipped |

## Shared libraries — user/src/lib

Zig library units under `user/src/lib/` (one row per top-level file or
subdirectory package).

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
| action_registry.zig | lib | — | ver 1.0.0 | — | shipped |
| asmenc.zig | lib | — | ver 1.0.0 | — | shipped |
| crc32.zig | lib | — | ver 1.0.0 | — | shipped |
| crypto.zig + crypto/ | lib | — | ver 1.0.0 | ADR 0023 | shipped |
| fixtures/ | lib | — | ver 1.0.0 | — | fixture |
| flate.zig | lib | — | ver 1.0.0 | — | shipped |
| font8x8.zig | lib | — | ver 1.0.0 | — | shipped |
| font_atlas_gen.zig | lib | — | ver 1.0.0 | — | shipped |
| font_ttf.zig | lib | — | ver 1.0.0 | — | shipped |
| image.zig | lib | — | ver 1.0.0 | — | shipped |
| netargs.zig | lib | — | ver 1.0.0 | — | shipped |
| netauth.zig | lib | — | ver 1.0.0 | — | shipped |
| netstats.zig | lib | — | ver 1.0.0 | — | shipped |
| netstatus.zig | lib | — | ver 1.0.0 | — | shipped |
| pipe.zig | lib | — | ver 1.0.0 | — | shipped |
| png.zig | lib | — | ver 1.0.0 | — | shipped |
| qoi.zig | lib | — | ver 1.0.0 | — | shipped |
| rng.zig | lib | — | ver 1.0.0 | — | shipped |
| script.zig | lib | — | ver 1.0.0 | ADR 0021 | shipped |
| sexiburger.zig | lib | — | ver 1.0.0 | ADR 0034 | retained-fallback |
| shell.zig | lib | — | ver 1.0.0 | ADR 0021 | shipped |
| smpst.zig | lib | — | ver 1.0.0 | — | fixture |
| ssh/ | lib | — | ver 1.0.0 | ADR 0025 | shipped |
| startup.zig | lib | — | ver 1.0.0 | — | shipped |
| stdz/ | lib | — | ver 1.0.0 | — | shipped |
| tabapp.zig | lib | — | ver 1.0.0 | ADR 0033 | shipped |
| tls/ | lib | — | ver 1.0.0 | ADR 0029 | shipped |
| toolbox.zig | lib | — | ver 1.0.0 | — | shipped |
| tty.zig | lib | — | ver 1.0.0 | ADR 0020 | shipped |
| ui.zig + ui/ | lib | — | ver 1.0.0 | ADR 0011 | shipped |
| zc.zig | lib | — | ver 1.0.0 | tools/check-zc-host-contract.py | shipped |

## Kernel services owning a contract

Kernel-resident services whose cross-component behaviour is pinned by an
ADR or a contract doc. The version surface is the contract itself: when
the ADR is amended, the number in this table is what changed.

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
| Kernel image | kernel-service | KERNEL.BIN | osver, ver 1.0.0 | ADR 0004 | shipped |
| Boot loader | kernel-service | BOOTAA64.EFI | — | ADR 0001 | shipped |
| Syscall ABI | kernel-service | — | ADR 0007 | ADR 0007, ADR 0013 | shipped |
| exec / ELF loader | kernel-service | — | ADR 0035 | ADR 0035, ADR 0038 | shipped |
| Process model (sys-procs) | kernel-service | — | ADR 0047 | ADR 0047, ADR 0048 | shipped |
| Event delivery | kernel-service | — | ADR 0009 | ADR 0009 | shipped |
| Window server | kernel-service | — | ADR 0015 | ADR 0015, ADR 0018 | shipped |
| Terminal seam | kernel-service | — | ADR 0020 | ADR 0020 | shipped |
| File table / userland FS ABI | kernel-service | — | ADR 0010 | ADR 0010 | shipped |
| Network stack | kernel-service | — | ADR 0057 | ADR 0012, ADR 0057 | shipped |
| GPU / compute broker | kernel-service | — | ADR 0053 | ADR 0053 | shipped |
| Audio / PCM | kernel-service | — | ADR 0046 | ADR 0046 | shipped |
| Measurement + grants + manifest caps | kernel-service | — | ADR 0054 | ADR 0054, ADR 0024 | shipped |
| VLFS filesystem | kernel-service | — | ADR 0050 | ADR 0050, ADR 0061 | shipped |
| vsock-ctl channel | kernel-service | — | ADR 0056 | ADR 0056 | shipped |
| Host-integration lane | kernel-service | — | ADR 0051 | ADR 0051 | shipped |
| Warm checkpoint cooperation | kernel-service | — | ADR 0059 | ADR 0059 | shipped |
| Time and timers | kernel-service | — | ADR 0060 | ADR 0060 | shipped |

## Host tools

The host-side pieces the system cannot boot, image, or publish without.
`binary` names the in-repo source root.

| name | kind | binary | version | owning spec / contract | state |
|------|------|--------|---------|------------------------|-------|
| VMRunner | host-tool | host/vm-runner | — | docs/vz-runner.md | host |
| mkfs.vlfs | host-tool | tools/go/vlfs | ADR 0050 | ADR 0050, ADR 0061 | host |
| boris (docs site toolchain) | host-tool | tools/zig/boris | pin .github/boris-pin.json | .github/workflows/docs-gate.yml | host |

## Row grammar (what the drift check reads)

Every `|`-delimited line with six cells is a registry row; the check skips
the header and separator lines. Cells, in order:

- **name** — free text.
- **kind** — `app` (an `image/apps.txt` row), `seat`, `tool` (a staged
  guest binary), `fixture`, `lib`, `kernel-service`, `host-tool`.
- **binary** — the staged name (`NAME.ELF`/`NAME.BIN`/`NAME.SO`/`NAME.EFI`)
  or `—`. For `app` rows it must appear in `image/apps.txt`; for `seat`,
  `tool`, `fixture` and `kernel-service` rows it must be produced by the
  build or staging path (build.zig, `image/`, `tools/`); `lib` rows carry
  `—`; `host-tool` rows name their in-repo source root, which must exist.
- **version** — comma-separated tokens: `ver M.m.p` (the product
  version — the drift check resolves it against a const in the
  component's own source per "The version rule"; at most one per row,
  required on every row outside the named exemptions), `appver N` (the
  app's registered `StateVersion` under `user/go/`), a
  `Release`-prefixed const (one of the ADR 0055 grammar names in
  `user/go/store/release.go`), `ADR NNNN`, `osver`, `pin <path>`, or
  `—`.
- **owning spec / contract** — comma-separated refs: a
  `tools/gate/specs/*.spec` path, `ADR NNNN` (a file under
  `docs/decisions/` with that number must exist), another repo path that
  must exist, or `—`.
- **state** — `shipped`, `staged`, `fixture`, `retained-fallback`, `host`.

The check (`tools/tests/test_component_registry.py`, in
`just verify-portable`) fails when: an `image/apps.txt` row lacks a
registry `app` row or vice versa; a cited spec, ADR, const, or path does
not resolve; a binary is not in the produced set; an `appver` value
disagrees with the `StateVersion` the app's package registers (in either
direction — a new registration with a `—` here is drift too); a `ver`
value disagrees with the product-version const in the component's
source, or the source carries one the row does not show; or a cell
uses a token outside this grammar. `--missing-ver` prints the rows
still unseeded and exits 0 — advisory until the seeding cards land.
