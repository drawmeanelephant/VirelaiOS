# App refresh ledger (#3246, index #3234)

One row per shipped app — every `image/apps.txt` row — carrying the card
that last touched it and the ISO-8601 date. The component registry
answers "what exists"; this ledger answers "what is stale". Checked by
`tools/tests/test_app_refresh.py`, which runs in `just verify-portable`:
a manifest row with no ledger row, a row out of manifest order, or a
date that is not ISO-8601 fails the build. Bump your row: a PR that
touches an app's package re-dates that row in the same diff.

## The rotation rule

Each wave files one app-refresh index over the ledger's oldest tranche:
run `python3 tools/tests/test_app_refresh.py --stale`, take the top
rows, and refresh those apps. A refresh PR bumps its row — set
`last refresh` to the landing card and `date` to the merge date — so
the ledger, not memory, decides which apps the next wave sees.

Staleness is advisory, never a gate: `--stale` prints the table
oldest-first and exits 0 regardless. Time is not a build input.

The polish rotation's runnable half is the `live-polish-sweep` gate spec
(`tools/gate/specs/live-polish-sweep.spec`, #3668): it boots the
default seat on the session's NAT+sound posture and runs every shipped
app through open -> its cheapest verifiable marker -> clean close.

## Seeding convention

A row's `last refresh`/`date` is the newest non-merge commit on the
app's package directory (`user/go/<pkg>`; `user/src/tabwm.zig` for the
Zig fallback desktop) — spelled exactly, `git log --no-merges -1
--format=%ad --date=short -- <dir>`. Non-merge means single-parent: a
squash-landed PR is an ordinary commit and counts; a two-parent merge
(a `Merge main: …` landing) does not — seed from the newest non-merge
commit beneath it, never the merge's own date. That counts any
substantive touch — a focused app card, a shared-library change the
app consumes, a tree-wide migration that landed in the package alike.
`—` in `last refresh` marks a touch whose commit carries no card or
issue token (predates tracking); `—` in `date` marks no recorded touch
at all, and `--stale` sorts those rows first.

## Row grammar (what the checker reads)

- `app` — the manifest display name (apps.txt field 2).
- `binary` — the manifest executable (field 1). Rows appear in manifest
  order; the checker enforces exactly one ledger row per apps.txt row,
  in order.
- `last refresh` — the card that did it: whitespace-separated tokens,
  each either `Mnnnx` (a card) or `#issue`, or the whole cell `—`.
- `date` — ISO-8601 `YYYY-MM-DD`, or `—` only when `last refresh` is
  `—` too.
- `notes` — free text: what the touch was.

## The ledger

| app | binary | last refresh | date | notes |
|-----|--------|--------------|------|-------|
| 64-bit Calc | GOCALC.ELF | #3248 | 2026-10-23 | appver 1 envelope — mode + bounded tape + copy-tape verb |
| Text Editor | NOTE.ELF |  #3247 | 2026-10-23 | appver-1 envelope + status bar |
| Code Editor | GOEDIT.ELF |  #2991 | 2026-10-22 | per-function heat gutter from `--prof=` .folded |
| Files | GOFILES.ELF |  #3255 | 2026-10-23 | .vcf contact-card face — QuickLook pane + GOMAIL hand-off |
| Terminal | GOTERM.ELF |  #3251 | 2026-10-23 | notify-on-finish toast for >5s unfocused commands |
| Web | WEB.ELF |  #3142 | 2026-10-22 | a11y keys honoured — WEB text_scale |
| Pulse | PULSE.ELF |  #2959 | 2026-10-21 | lipgloss v2 |
| Shell | GOSH.ELF |  #3309 | 2026-10-24 | appver 1 envelope — cwd round-trips the seat's save/restore |
| Help | GOHELP.ELF |  #3129 | 2026-10-22 | free-file sleep sites → ns durations (tree-wide) |
| Welcome | GOWELCOME.ELF |  #3307 | 2026-10-24 | appver-1 envelope — tour step + done flag + first owning spec |
| Top | GOTOP.ELF |  #3249 | 2026-10-23 | appver-1 envelope — sort + active tab persist |
| Settings | GOSET.ELF |  #3171 | 2026-10-22 | settings table grows past 24 via bounded second bank |
| Image Viewer | GOVIEW.ELF |  #3155 | 2026-10-22 | MOV demux + Motion-JPEG playback |
| Sound | GOSOUND.ELF |  #3178 | 2026-10-22 | extract user/go/pcm (with GOREC) |
| Tabbed Desktop | TABWM.BIN |  #3226 | 2026-10-23 | docs litterbox touched the fallback source |
| Gostalgia | GOSTALGIA.ELF | #3306 | 2026-10-23 | durable /users/guest home over gsport/vfs HostFS |
| Store | GOSTORE.ELF |  #3188 | 2026-10-23 | store pin — signed-head rollback + ReleaseDepends |
| LLM Infer | GOINFER.ELF |  #3310 | 2026-10-24 | appver-1 envelope — armed journey + weights path + log tail; first owning spec |
| Recorder | GOREC.ELF |  #3178 | 2026-10-22 | extract user/go/pcm (with GOSOUND) |
| Snapshots | GOSNAP.ELF |  #3308 | 2026-10-24 | appver 1 envelope — selection + field round-trip on the windowed face |
| Assistant | GOASSIST.ELF |  #3145 | 2026-10-22 | romaji→kana composition in a text field |
| Sheet | GOSHEET.ELF |  #2915 | 2026-10-21 | v2 — fill series, 32-step undo, column formats, blob v2 |
| Reader | GOREADER.ELF |  #3244 | 2026-10-23 | shared appkit find overlay — GOREADER migrates |
| Clock | GOCLOCK.ELF |  #3254 | 2026-10-23 | editable events — ical write-back |
| Mines | GOMINE.ELF |  #2918 | 2026-10-21 | GOMINE best times (with GOSOL undo + stats) |
| Solitaire | GOSOL.ELF |  #2918 | 2026-10-21 | undo-in-blob + stats + chime |
| Emoji | GOEMOJI.ELF |  #2917 | 2026-10-21 | v2 — recents + favorites shelf, save_state v2 |
| IRC | GOIRC.ELF |  #2919 | 2026-10-21 | v2 — /hl highlight rules, bounded per-channel logs |
| Vault | GOVAULT.ELF |  #3240 | 2026-10-23 | PWR Lock row + ctrl+shift-l screen lock |
| Mail | GOMAIL.ELF |  #3253 | 2026-10-23 | folder browse over real LIST + bounded /host/MAIL cache, offline search |
| Accessibility | GOA11Y.ELF |  #3143 | 2026-10-22 | appkit a11y outlines + GOA11Y reader |
| Paint | GOPAINT.ELF |  #3455 | 2026-10-25 | manifest promotion — the gate-proven `opens=image` row ships |

## Non-app staleness

The registry's other components get the same ledger treatment: one row
per `docs/component-registry.md` row whose kind is `seat`, `tool`,
`script`, `lib`, `kernel-service` or `host-tool` and whose state is not
`fixture`, in registry order. `fixture`-state rows exist to serve their
spec — the rotation never refreshes a proof — and the `asset` rows
(staged art) sit outside the families the goal named; both skips are
recorded on the card (issue #3665). The wave-cadence question for
non-app rows is the owner's; until then this table is read-only
evidence, not a rotation.

Same seeding convention: `last refresh`/`date` is the newest non-merge
commit on the row's `seed path` — a package dir, or the pinning file a
kernel-service or host row names (`git log --no-merges -1
--format=%ad --date=short -- <path>`). `—`/`—` still means no recorded
touch. The checker enforces row↔registry coverage and order exactly as
it does for the app table, and `--stale` prints both tables.

### Row grammar (non-app)

- `component` — the registry `name`, spelled as there.
- `kind` — the registry `kind` token; the checker enforces it.
- `seed path` — space-separated repo paths the row seeds from; each
  must exist.
- `last refresh`, `date`, `notes` — as above.

### The ledger — non-app rows

| component | kind | seed path | last refresh | date | notes |
|-----------|------|-----------|--------------|------|-------|
| Tabbed desktop seat | seat | user/go/gotabwm | #3455 | 2026-10-25 | the windowed-candidates verdict — GOPAINT ships, three named refusals |
| OS updater | tool | user/go/goupd | #2651 | 2026-10-18 | argv-block residuals: dead pads/asserts, stale 32-byte bound, census re-pin |
| Agent daemon | tool | user/go/agentd | #3391 | 2026-10-25 | audit-lane integrity — fail-closed writes, refused-session |
| Diff tool | tool | user/go/godiff | #3129 | 2026-10-22 | migrate free-file sleep sites to ns durations / event wait |
| Ping | tool | user/go/ping | #2651 | 2026-10-18 | argv-block residuals: dead pads/asserts, stale 32-byte bound, census re-pin |
| Web fetch | tool | user/go/fetch | #2764 | 2026-10-20 | HTTP/1.1 keep-alive + chunked + gzip in GOFETCH and WEB |
| Netstat | tool | user/go/netstat | #3444 | 2026-10-25 | net-mdns — the LAN discovery verbs bind a real grant-table word |
| DNS lookup | tool | user/go/dns | #2651 | 2026-10-18 | argv-block residuals: dead pads/asserts, stale 32-byte bound, census re-pin |
| Traceroute | tool | user/go/traceroute | #2651 | 2026-10-18 | argv-block residuals: dead pads/asserts, stale 32-byte bound, census re-pin |
| Git client | tool | user/go/git | #2767 | 2026-10-20 | one inflater — promote hardened reader to user/go/flate |
| SSH client | tool | user/go/ssh | #2651 | 2026-10-18 | argv-block residuals: dead pads/asserts, stale 32-byte bound, census re-pin |
| SSH server | tool | user/go/sshd | #2961 | 2026-10-21 | GOSSHD serves a Bubble Tea v2 Program over the sshlib session |
| HTTP server | tool | user/go/httpd | #3444 | 2026-10-25 | net-mdns — the LAN discovery verbs bind a real grant-table word |
| WASM runtime | tool | user/src/wasm.zig | #3190 | 2026-10-23 | proof-binary census — dead dyn-link sources retired |
| QuickJS engine (now session-staged, was gate-only) | tool | tools/js-client | #3447 | 2026-10-25 | js-client: build the max env entry byte-wise — pinned Zig 0.17 comptime ++ chains on sentinel arrays emit interior NULs, tripping EnvironmentLimit on a clean 16-entry env in both the policy test and the QBOUNDS.BIN bounds leg |
| App-manifest report script (card d / index #3334) | script | image/scripts/report.js | #3441 | 2026-10-25 | the first shipped scripted workflow — SCRIPTS/REPORT.JS |
| Zig userland root | tool | user/src/main.zig | #3190 | 2026-10-23 | proof-binary census — dead dyn-link sources retired |
| Sexiburger desktop | tool | user/src/sexiburger.zig | #3190 | 2026-10-23 | proof-binary census — dead dyn-link sources retired |
| Init service supervisor | tool | user/go/init | #3440 | 2026-10-25 | GOHTTPD as an INIT-managed service on opt-in boots |
| R3D renderer | tool | user/go/r3d | #1367 | 2026-09-17 | Go EL0 software triangle rasterizer: GOR3D.ELF + go-r3d gate |
| RSS reader | tool | user/go/rss | #3239 | 2026-10-23 | menubar adoption sweep — GOEDIT/GOFILES/WEB/GOTERM |
| Git UI | tool | user/go/gitui | #3129 | 2026-10-22 | migrate free-file sleep sites to ns durations / event wait |
| Crash viewer | tool | user/go/crashview | #3189 | 2026-10-22 | diagnostics census — documented split + shared drain |
| Log viewer | tool | user/go/logview | #3189 | 2026-10-22 | diagnostics census — documented split + shared drain |
| Selftest runner | tool | user/go/selftest | #3351 | 2026-10-24 | the consent-gated sys_secret_set seam — slot 96 |
| Observe CLI | tool | user/go/observe | #3189 | 2026-10-22 | diagnostics census — documented split + shared drain |
| Profiler | tool | user/go/prof | — | 2026-10-06 | use paired median budget and guard profile control |
| Syscall tracer | tool | user/go/strace | #3189 | 2026-10-22 | diagnostics census — documented split + shared drain |
| Heap inspector | tool | user/go/heap | — | 2026-10-07 | wait for samples and reopen transient publication views |
| FLINT release CLI | tool | user/go/flint | #2777 | 2026-10-20 | flint blame, log --graph, tags; GODIFF -i side-by-side |
| Guest go-build driver | tool | user/go/gobuild | #2767 | 2026-10-20 | one inflater — promote hardened reader to user/go/flate |
| Guest compiler | tool | user/go/gobuild | #2767 | 2026-10-20 | one inflater — promote hardened reader to user/go/flate |
| Guest linker | tool | user/go/gobuild | #2767 | 2026-10-20 | one inflater — promote hardened reader to user/go/flate |
| Release pipeline | tool | user/go/release | #2651 | 2026-10-18 | argv-block residuals: dead pads/asserts, stale 32-byte bound, census re-pin |
| appkit | lib | user/go/appkit | #3348 | 2026-10-25 | wire the staged art's seated and raster surfaces |
| chords | lib | user/go/chords | #3252 | 2026-10-23 | GOEDIT quick-open — ctrl+p palette over recents + subtree |
| clockfmt | lib | user/go/clockfmt | #3262 | 2026-10-23 | GOREADER.ELF over its build bound — strip ianaZones via Names reachability, gate on exec_image_max |
| css | lib | user/go/css | #2596 | 2026-10-18 | per-tab zoom Ctrl+=/-/0 with layout reflow + font_size setting |
| draw | lib | user/go/draw | #2269 | 2026-10-14 | contract v2 depth primitives in user/go/draw + goldens |
| ed25519 | lib | user/go/ed25519 | #2398 | 2026-10-17 | agentd: virelai-agent/2 — Ed25519 identity, declared caps, bounded verbs |
| fatimg | lib | user/go/fatimg | #2465 | 2026-10-18 | guest-staged signed OS self-update — GOUPD + --apply-update/.prev rollback |
| find | lib | user/go/find | #3243 | 2026-10-23 | the launcher learns verbs — filename hits, open/reveal/open-with |
| flate | lib | user/go/flate | #3057 | 2026-10-22 | REVIEW-LOW sweep (#3057, disjoint subset): socket ABI variants, flate hardening, tabsv2 drops, term hints CSI |
| gpu3d | lib | user/go/gpu3d | #2494 | 2026-10-17 | gpu3d.ComputeSession: unmap the fixed-VA window on Close |
| heap | lib | user/go/heap | — | 2026-10-07 | wait for samples and reopen transient publication views |
| hostint | lib | user/go/hostint | #3387 | 2026-10-25 | lane-dir write policy + resilient seat listings |
| httpc | lib | user/go/httpc | #2764 | 2026-10-20 | HTTP/1.1 keep-alive + chunked + gzip in GOFETCH and WEB |
| ical | lib | user/go/ical | #3254 | 2026-10-23 | GOCLOCK editable events — ical write-back |
| icons | lib | user/go/icons | — | 2026-10-14 | icons: glyph batch 2 — ten new PUA shapes via Virelai Sans pipeline |
| ime | lib | user/go/ime | #3180 | 2026-10-22 | ime,layout,go-assist: kana->kanji candidates from a curated dict |
| infer | lib | user/go/infer | #3054 | 2026-10-21 | goinfer: hostile .VIW hardening — shape-pinned tensors, bounded fields |
| layout | lib | user/go/layout | #3180 | 2026-10-22 | ime,layout,go-assist: kana->kanji candidates from a curated dict |
| linebuf | lib | user/go/linebuf | #2581 | 2026-10-19 | shared appkit.FilePicker adopted by GOEDIT/GOFILES/WEB |
| mime | lib | user/go/mime | #3384 | 2026-10-24 | application/javascript opens — Script dispatch to QJS.BIN |
| observe | lib | user/go/observe | #3189 | 2026-10-22 | diagnostics census — documented split + shared drain |
| outbox | lib | user/go/outbox | #2602 | 2026-10-18 | OUTBOX export lane — REQ intent → signed TagOutbox → host Downloads |
| pcm | lib | user/go/pcm | #3178 | 2026-10-22 | extract user/go/pcm — the PCM library GOSOUND+GOREC consume |
| pdf | lib | user/go/pdf | #2988 | 2026-10-22 | ADR 0040 A4 — baseline DCTDecode via decoder hook |
| pdfwrite | lib | user/go/pdfwrite | #2976 | 2026-10-21 | shared zlib encoder, PNG encoder, GOPAINT PNG export |
| prof | lib | user/go/prof | — | 2026-10-06 | use paired median budget and guard profile control |
| rfb | lib | user/go/rfb | #1810 | 2026-09-27 | add transport-agnostic RFB wire codec |
| settings | lib | user/go/settings | #3402 | 2026-10-24 | a11y.* live family — running GOVIEW answers a reduce_motion flip |
| shlib | lib | user/go/shlib | #3441 | 2026-10-25 | the first shipped scripted workflow — SCRIPTS/REPORT.JS |
| snapshot | lib | user/go/snapshot | #2846 | 2026-10-20 | gotabwm: split panes as a tree + agent_input |
| sshlib | lib | user/go/sshlib | #2960 | 2026-10-21 | sshlib server interactive session — pty/shell/windowed IO |
| store | lib | user/go/store | #3188 | 2026-10-23 | store pin — signed-head rollback + ReleaseDepends |
| strace | lib | user/go/strace | #3189 | 2026-10-22 | diagnostics census — documented split + shared drain |
| supervise | lib | user/go/supervise | — | 2026-10-09 | land the eight owner-approved security follow-up fixes |
| svcgraph | lib | user/go/svcgraph | — | 2026-10-05 | Svcgraph: resolve bounded service dependencies deterministically |
| svcmanifest | lib | user/go/svcmanifest | #3444 | 2026-10-25 | net-mdns — the LAN discovery verbs bind a real grant-table word |
| svg | lib | user/go/svg | #1947 | 2026-10-04 | fix SVG exclusion classification |
| tabapp | lib | user/go/tabapp | #2568 | 2026-10-19 | gotabwm: (#2568) cross-app ctrl+z — seat-owned undo chord |
| teatty | lib | user/go/teatty | #2957 | 2026-10-21 | Bubble Tea v2 runs its own Program on a bound tty |
| theme | lib | user/go/theme | #2983 | 2026-10-21 | accessibility v1 — a11y.* settings rows, cap 20->24, GOSET access tab, high-contrast themes, seat fade=off |
| timesync | lib | user/go/timesync | #1775 | 2026-09-28 | SNTP sets the clock — `time sync`, slot 78, runner responder |
| tls | lib | user/go/tls | #3395 | 2026-10-24 | tls: bound record length to plainBuf before the slice |
| ttf | lib | user/go/ttf | #3179 | 2026-10-22 | Arabic presentation-form shaper + Noto Naskh face in WEB |
| vcard | lib | user/go/vcard | #3255 | 2026-10-23 | a real contact-card face for .vcf — QuickLook pane + GOMAIL hand-off |
| vector | lib | user/go/vector | — | 2026-10-18 | vector: re-measure on nonzero AllocsPerRun — the stray alloc is runtime-internal |
| vi | lib | user/go/vi | #3444 | 2026-10-25 | net-mdns — the LAN discovery verbs bind a real grant-table word |
| vsockctl | lib | user/go/vsockctl | — | 2026-10-18 | guest-visible profile summary in GOSET/GOTOP via vsock-ctl |
| vsys | lib | user/go/vsys | #3036 | 2026-10-22 | ns duration syscalls on the deadline overlay |
| webrender | lib | user/go/webrender | #3356 | 2026-10-24 | Adam7 interlace decode inside the existing PNG budget |
| webstyle | lib | user/go/webstyle | #2193 | 2026-10-13 | web typography honesty — family, size and weight resolve end to end |
| widgets | lib | user/go/widgets | #3338 | 2026-10-24 | adopt the f-artcraft drop — stage the six sets, wire the open seams |
| action_registry.zig | lib | user/src/lib/action_registry.zig | — | 2026-10-10 | zig 0.17.0 flag day: lock 0.17.0, netSend/netRead/netWrite -> Io.Operation, error_set.error_names, fmt canonical builtins, pin-respecting ttf gate |
| asmenc.zig | lib | user/src/lib/asmenc.zig | — | 2026-10-10 | zig 0.17.0 flag day: lock 0.17.0, netSend/netRead/netWrite -> Io.Operation, error_set.error_names, fmt canonical builtins, pin-respecting ttf gate |
| crc32.zig | lib | user/src/lib/crc32.zig | #821 | 2026-09-02 | user/lib: implement raster graphics engine and UI toolkit integration |
| crypto.zig + crypto/ | lib | user/src/lib/crypto.zig user/src/lib/crypto | — | 2026-10-10 | zig 0.17 dual-clean prep: ** -> @splat/repeat_lit, std.meta.Int -> @Int, @typeInfo fields -> std.meta.tags/fieldNames |
| flate.zig | lib | user/src/lib/flate.zig | #3057 | 2026-10-22 | REVIEW-LOW sweep (#3057, disjoint subset): socket ABI variants, flate hardening, tabsv2 drops, term hints CSI |
| font8x8.zig | lib | user/src/lib/font8x8.zig | — | 2026-08-15 | feat(m11): Milestone 11 complete — desktop platform & GUI apps |
| font_atlas_gen.zig | lib | user/src/lib/font_atlas_gen.zig | #2191 | 2026-10-13 | freeze the Zig font engine — ADR 0034 amendment A + cross-engine parity pin |
| font_ttf.zig | lib | user/src/lib/font_ttf.zig | #2191 | 2026-10-13 | freeze the Zig font engine — ADR 0034 amendment A + cross-engine parity pin |
| image.zig | lib | user/src/lib/image.zig | — | 2026-10-10 | zig 0.17 dual-clean prep: ** -> @splat/repeat_lit, std.meta.Int -> @Int, @typeInfo fields -> std.meta.tags/fieldNames |
| netargs.zig | lib | user/src/lib/netargs.zig | — | 2026-10-10 | zig 0.17 dual-clean prep: ** -> @splat/repeat_lit, std.meta.Int -> @Int, @typeInfo fields -> std.meta.tags/fieldNames |
| netauth.zig | lib | user/src/lib/netauth.zig | — | 2026-10-10 | zig 0.17 dual-clean prep: ** -> @splat/repeat_lit, std.meta.Int -> @Int, @typeInfo fields -> std.meta.tags/fieldNames |
| netstats.zig | lib | user/src/lib/netstats.zig | #400 | 2026-08-24 | feat(m26): N2 NETSTAT.BIN + N3 fetch display — sys_net_stats (slot 62) with live-verified gates |
| netstatus.zig | lib | user/src/lib/netstatus.zig | #1683 | 2026-09-24 | make GOFETCH the sole fetch and download client |
| pipe.zig | lib | user/src/lib/pipe.zig | #1080 | 2026-09-11 | SH4: pipes, redirection, globs |
| png.zig | lib | user/src/lib/png.zig | — | 2026-09-03 | feat(view): stream-chunked PNG decode for VIEW.BIN |
| qoi.zig | lib | user/src/lib/qoi.zig | — | 2026-10-10 | zig 0.17 dual-clean prep: ** -> @splat/repeat_lit, std.meta.Int -> @Int, @typeInfo fields -> std.meta.tags/fieldNames |
| rng.zig | lib | user/src/lib/rng.zig | #1171 | 2026-09-12 | SSH4: encrypted transport + session channel + SSH.BIN exec/shell |
| script.zig | lib | user/src/lib/script.zig | — | 2026-10-10 | zig 0.17 dual-clean prep: ** -> @splat/repeat_lit, std.meta.Int -> @Int, @typeInfo fields -> std.meta.tags/fieldNames |
| sexiburger.zig | lib | user/src/lib/sexiburger.zig | #3455 | 2026-10-25 | the windowed-candidates verdict — GOPAINT ships, three named refusals |
| shell.zig | lib | user/src/lib/shell.zig | — | 2026-10-10 | zig 0.17 dual-clean prep: ** -> @splat/repeat_lit, std.meta.Int -> @Int, @typeInfo fields -> std.meta.tags/fieldNames |
| ssh/ | lib | user/src/lib/ssh | — | 2026-10-10 | zig 0.17 dual-clean prep: ** -> @splat/repeat_lit, std.meta.Int -> @Int, @typeInfo fields -> std.meta.tags/fieldNames |
| startup.zig | lib | user/src/lib/startup.zig | — | 2026-10-10 | zig 0.17 dual-clean prep: ** -> @splat/repeat_lit, std.meta.Int -> @Int, @typeInfo fields -> std.meta.tags/fieldNames |
| stdz/ | lib | user/src/lib/stdz | — | 2026-10-10 | zig 0.17.0 flag day: lock 0.17.0, netSend/netRead/netWrite -> Io.Operation, error_set.error_names, fmt canonical builtins, pin-respecting ttf gate |
| tabapp.zig | lib | user/src/lib/tabapp.zig | — | 2026-10-10 | zig 0.17 dual-clean prep: ** -> @splat/repeat_lit, std.meta.Int -> @Int, @typeInfo fields -> std.meta.tags/fieldNames |
| tls/ | lib | user/src/lib/tls | — | 2026-10-10 | zig 0.17.0 flag day: lock 0.17.0, netSend/netRead/netWrite -> Io.Operation, error_set.error_names, fmt canonical builtins, pin-respecting ttf gate |
| toolbox.zig | lib | user/src/lib/toolbox.zig | #1631 | 2026-09-22 | paint rune cells to the scanout |
| tty.zig | lib | user/src/lib/tty.zig | — | 2026-10-10 | zig 0.17.0 flag day: lock 0.17.0, netSend/netRead/netWrite -> Io.Operation, error_set.error_names, fmt canonical builtins, pin-respecting ttf gate |
| ui.zig + ui/ | lib | user/src/lib/ui.zig user/src/lib/ui | #3036 | 2026-10-22 | ns duration syscalls on the deadline overlay |
| zc.zig | lib | user/src/lib/zc.zig | #3036 | 2026-10-22 | ns duration syscalls on the deadline overlay |
| Kernel image | kernel-service | kernel/src | #3463 | 2026-10-25 | ver M.m.p grammar + const-resolution check + kernel slice |
| Boot loader | kernel-service | boot/src | #2611 | 2026-10-19 | task pool 16->64, registry 16->64, threads 6->16, spawn-storm leg |
| Syscall ABI | kernel-service | kernel/src/syscall_abi.zig | #3351 | 2026-10-24 | the consent-gated sys_secret_set seam — slot 96 |
| exec / ELF loader | kernel-service | kernel/src/exec.zig | #3390 | 2026-10-24 | exec refuses an unprovisioned AGENTD.ELF — the recorded-name bind is no longer squat-table |
| Process model (sys-procs) | kernel-service | kernel/src/process.zig | #3444 | 2026-10-25 | net-mdns — the LAN discovery verbs bind a real grant-table word |
| Event delivery | kernel-service | kernel/src/events.zig | #2896 | 2026-10-21 | seat-forwarded pointer sample judged on capture-time modifiers |
| Window server | kernel-service | kernel/src/wm_server.zig | #3107 | 2026-10-22 | damage-driven present — unpin the WM seat from ring 0 |
| Terminal seam | kernel-service | kernel/src/terminal.zig | #3150 | 2026-10-22 | CSI private-parameter markers — <,=,>,? swallowed unless armed |
| File table / userland FS ABI | kernel-service | kernel/src/file_table.zig | #3387 | 2026-10-25 | lane-dir write policy + resilient seat listings |
| Network stack | kernel-service | kernel/src/socket_native.zig | #3401 | 2026-10-24 | broadcast-dst IPv4 — UDP delivers only to opted-in listeners, TCP a counted drop |
| GPU / compute broker | kernel-service | kernel/src/gpu3d_broker.zig | #2166 | 2026-10-12 | 1: reviewed WM-only GPU3D bridge — slot-65 cmd 22 + lifetime revocation |
| Audio / PCM | kernel-service | kernel/src/pcm_playback.zig | #2180 | 2026-10-13 | kernel mixer over the B7 stream + vi.AudioStream |
| Measurement + grants + manifest caps | kernel-service | kernel/src/measure.zig | #3044 | 2026-10-22 | receipts carry the principal |
| VLFS filesystem | kernel-service | kernel/src/vlfs | #3405 | 2026-10-24 | vlfs+vaultlib: bound stored kdf_iters — hostile counts refused at parse |
| vsock-ctl channel | kernel-service | kernel/src/vsock_ctl.zig | — | 2026-10-18 | guest-visible profile summary in GOSET/GOTOP via vsock-ctl |
| Host-integration lane | kernel-service | kernel/src/shared_region.zig | #2273 | 2026-10-14 | follow-up: pid-0 peer-seat free, surface-VA probe, crash-viewer -u0 |
| Warm checkpoint cooperation | kernel-service | kernel/src/seal.zig | #3128 | 2026-10-24 | shifted-tag tear is host-authored below the served-read path — phase-2 evidence + T>>(8k) scanner |
| Time and timers | kernel-service | kernel/src/timer.zig | #3033 | 2026-10-22 | kernel deadline overlay — ns deadlines beside the 1 Hz heartbeat |
| VMRunner | host-tool | host/vm-runner | #3391 | 2026-10-25 | audit-lane integrity — fail-closed writes, refused-session |
| mkfs.vlfs | host-tool | tools/go/vlfs | #3405 | 2026-10-24 | vlfs+vaultlib: bound stored kdf_iters — hostile counts refused at parse |
| boris (docs site toolchain) | host-tool | tools/zig/boris | — | 2026-10-18 | boris: zig-0.17 syntax port for upstream sources |
