# ADR 0030: The language split (Zig owns the kernel, Go owns EL0)

- Status: ACCEPTED
- Date: 2026-09-15
- Amended: 2026-09-16 (M60 / #1297 — no new Zig EL0 apps; first leftover `EDIT.BIN` deleted)
- Amended: 2026-09-21 (M71k / #1570 — the EL0 HTTPS consumer is Go; `FETCHS.BIN` deleted, `lib/tls` is host-side interop)
- Amended: 2026-09-21 (M71j / #1569 — the EL0 SSH-2 client is Go; `SSH.BIN` deleted, `lib/ssh` wire/packet/stream stay for SSHPACKET.BIN)
- Amended: 2026-10-01 (A1 / #1865 — bounded native Zig portfolio exception)
- Amendment proposed: 2026-10-03 (A2 / #1921 — bounded standalone QuickJS; owner approval pending)
- Zig Guest Target/Backend Owner (A1 / #1865): **drawmeanelephant**,
  explicitly appointed by the project owner on 2026-10-01
- Issue: #1293 (this document), umbrella #1292, M60 #1297
- Related: ADR 0001 (Zig as guest language — narrowed here), ADR 0007
  (syscall ABI; kernel changes still ride amendments of that file only),
  ADR 0015 (userland WM seat / slot 65), ADR 0020 (terminal seam),
  ADR 0021 (userland shell; the interactive shell moves to Go later, the
  kernel monitor stays), ADR 0026 (GOOS=virelai), ADR 0028 (HTML — already
  a Go consumer in M54), ADR 0029 (TLS 1.3 client — Go owns the EL0
  consumer since M71k)

> Product split, not a vibe: **VZ is the hypervisor. Zig is the guest
> kernel. Go is EL0.** This ADR is docs-only. It carries the M56–M60 card
> split; there is no separate scoping doc. Boot default does not move
> until M59.

## Context

M54 proved a Go EL0 program can own a raw ADR 0007 window (`GOWIN.ELF`)
and that an independent Go renderer (`WEB.ELF` over `user/go/webrender`)
can paint in-guest. The GOOS=virelai runtime (ADR 0026 / 0027) is the
substrate. What is still hostile is **Zig userland**: `tabwm.zig`,
`lib/ui`, every `.BIN`. New desktop work there is a second product in the
wrong language.

Zig userland cannot become a library Go calls. There is no cgo and no
FFI. `lib/tls`, `lib/html`, and `LIBUI.SO` are dead to Go. The shared
contract is the one that already exists: ADR 0007 syscalls, WM_RPC over
slots 5/6, and on-disk file formats. Zig EL0 binaries become things you
`exec`, then you delete them.

ADR 0001 D3 said Zig is the guest implementation language. That remains
true for the kernel. It is no longer true for the desktop.

## Decision

**D1. Zig is the guest kernel, forever.** Zig owns `boot/`, `kernel/`,
virtio, GIC, the scheduler, the syscall table, the Swift runner, and the
serial monitor (the ADR 0021 boot/recovery console). `WASM.BIN` / `ZC.BIN`
may remain Zig tools. No new Zig GUI. Kernel changes in this arc ride
ADR 0007 amendments only; this file adds no slots.

**D2. Go is the EL0 product.** Go owns the WM, chrome, apps, and
eventually the interactive shell. Programs are statically linked
`GOOS=virelai` ELFs. The SDK is `user/go/vi`; it is never `LIBUI.SO`.
New UI is written once, in Go.

**D3. The contract is ABI and formats, not libraries.** A Go program
speaks ADR 0007 and WM_RPC the same way a Zig program does. It does not
link Zig objects, wrap `lib/ui`, or import a C ABI. Leftover Zig apps
keep working because they already speak that contract, not because Go
embeds them.

**D4. Forbidden moves** (every card in this arc; the load-bearing list):

- No Go rewrite of the kernel.
- No dual widget toolkits: small Go widgets actually needed (text,
  button, list) live under `user/go/tabapp` or a tiny `user/go/ui`.
  Never a `LIBUI` clone, never a second competing toolkit.
- No new `user/src/*.zig` apps (GUI or otherwise). In force as of M60
  (#1297); see the amendment below. Zig kernel stays.
- No porting crypto "because pivot". TLS was a Zig *helper*
  (`FETCHS.BIN` / `lib/tls` over TCP, ADR 0029) until M71k (#1570) deleted
  the EL0 binary: HTTPS is Go in-process and `lib/tls` survives only as
  host-side interop. SSH was `SSH.BIN` until M71j (#1569) deleted it:
  the EL0 client is `GOSSH.ELF` over `user/go/sshlib`; `lib/ssh`
  wire/packet/stream stay for `SSHPACKET.BIN`.
- No `settings set wm` / boot-default flip before M59. AGENTS.md's
  "do not change the boot default" rule yields only on that card. **#1298 is
  that card, and it has landed**: the compiled `wm` default is `gotabwm`, so
  a boot with no persisted setting seats the Go desktop; `settings set wm
  tabwm` keeps the Zig seat, and `settings set wm none` is the explicit
  shim-only VM the pre-M59 fleet assumed.
- No claiming a milestone parent (`#1296`, `#1295`, `#1294`) — those are
  indexes. Claim the leaf.

**D5. Seat discovery stays userland.** `is_wm_name` / `wm_peers` in
`user/src/lib/ui/abi.zig` currently accept `WND.BIN` or `TABWM.BIN`
(M42 SX3). `GOTABWM.ELF` must eventually match that list so leftover Zig
clients find the Go seat. That is a name-table change, not a kernel ABI
change; it lands with M57/M59, not with this file. Slot 65 remains
one-seat (`REGISTER` → `EACCES` if taken).

**D6. The first code of the pivot is a Go tab inside Zig TABWM, not a
Go WM.** If `user/go/tabapp` cannot speak WM_RPC as a *client*, a Go WM
is a second monolith. M56 finishes `vi` and lands one full-viewport Go
app in the existing seat. M57 is an opt-in second seat. M59 is the only
card allowed to make it the default.

**D7. This unblocks desktop work in Go. It does not absorb unrelated
debt.** The pivot neither blocks on nor fixes the #1252 nudge / VZ abort
(#1287), TLS consumers beyond the helper already on main, or DNS. Those
stay their own cards.

## Card split (M56–M60)

Parents `#1296` / `#1295` / `#1294` are indexes. Claim the leaf with
`just claim-card <n>`. No card declares `docs/status.md` in `Touches`
(status rows merge at landing). New gates are declarative specs under
`tools/gate/specs/`; extend an existing spec when it already covers the
change.

| # | Card | Claim | What "done" means |
|---|------|-------|-------------------|
| **M55** | This ADR | #1293 | This file reads ACCEPTED. No code. |
| **M56** | Finish the Go SDK | index #1296 | A Go tab speaks WM_RPC inside Zig TABWM on VZ; `vi` is enough to be a client. Kernel untouched. |
| M56a | `vi`: IPC 5/6 + `sys_procs` peer discovery | #1311 | Host tests for the WM_RPC wire / discovery. |
| M56b | `vi`: tab-client WM_RPC kinds + event dispatch | #1316 | Host tests. Depends on M56a. |
| M56c | `vi`: addr-hinted mmap (the GOWIN `addr=0` hole) | #1314 | Host tests; guest path is not `addr=0`. |
| M56d | `user/go/tabapp` + one Go app full-viewport in Zig TABWM | #1315 | **First commit of the pivot.** The milestone's only VZ card (`go-tabapp`): open / declare-fullscreen / resize / close, no `[EXC]`. Filled rect + title is enough — no widgets yet. |
| M56e | Go widgets: text, button, list | #1319 | Host-tested. Depends on M56d. Not a LIBUI clone. |
| **M57** | `GOTABWM.ELF` as a second seat | index #1295 | Unmodified Zig binaries run on the Go seat on VZ. Opt-in; default stays TABWM. One spec (`go-wm-seat`) grows. No Zig TABWM changes. |
| M57a | GOTABWM registers slot 65, composites a blank desktop | #1313 | Starts `go-wm-seat`: register, compose, clean unregister; default boot still TABWM. |
| M57b | GOTABWM manages its own Go windows | #1317 | Extends `go-wm-seat` (rect / chrome / focus / close). |
| M57c | GOTABWM hosts leftover Zig CALC/NOTEPAD | #1318 | Completes `go-wm-seat`. Parity on the exercised path only. |
| **M58** | Move the apps you touch | index #1294 | One app per card, full-viewport via `tabapp` in **Zig TABWM**. Does **not** wait on M57. Browser is already Go (M54). Leave CALC until it is in the way. Leave Zig `FILE.BIN` until a later leftover card. `EDIT.BIN` deleted in M60; `TERM.BIN` superseded by GOTERM. |
| M58a | Go file manager | #1305 | `go-files` on VZ: open, list a known share file, close. |
| M58b | Go editor | #1306 | `go-edit` on VZ: open fixture, dirty, save, close. Usable buffer + save, not EDIT's feature list. |
| M58c | Go terminal front-end (ADR 0020) | #1307 | `go-term` on VZ: attach, a typed line / shell marker, close. No new tty syscall. |
| M58d | Go fetch over the Zig TLS helper | #1308 | HTTPS via the Zig helper; never a cleartext GET. No Go crypto. DNS is not this card. |
| **M59** | Explicit default flip | #1298 | The **only** card allowed to move the boot default. `settings set wm gotabwm` persists; Zig TABWM remains the fallback. Touches include `kernel/src/shell.zig` (that is where the WM boot default lives), not only a settings panel. Gate: flip → reboot → Go WM hosting a leftover Zig app. Depends on M57 (second seat proven) and at least one leftover Zig app hosted under it. **Landed:** `wm` is a schema-v2 settings key whose compiled default is `gotabwm`; the shell-idle autostart resolves the seat through it (`gotabwm` → `GOTABWM.ELF`, `tabwm` → `TABWM.BIN`, `none` → shim), `tools/session.sh` stages the Go seat and lets the default apply, and `go-wm-default` proves the default boot on VZ (boot 01: no `wm` key → the Go seat hosts CALC; boot 02: the persisted `wm=tabwm` → the Zig seat). A boot whose share carries no seat binary says so and stays shim-only instead of faking a desktop. |
| **M60** | Starve Zig EL0 | #1297 | Policy in force (amendment below): no new `user/src/*.zig` apps. First leftover gone: `EDIT.BIN` → `GOEDIT.ELF` (gate `go-edit`). Further deletions are later cards. TLS stays `FETCHS.BIN` / `lib/tls`; SSH stays `SSH.BIN` (both superseded later: TLS deleted in M71k #1570; SSH is M71j #1569). No flag day. |

Superseded drafts (closed, do not claim): original M56/M57 leaves
#1300–#1304; both-seats M58 drafts #1309, #1310, #1312, #1320.

## Consequences

- Agents write desktop and apps in Go. Zig userland stops growing.
- The kernel stays the small hostile core. A Go WM is still an EL0
  process on slot 65; it does not move policy back into EL1.
- Zig leftover apps keep working across the seat change because WM_RPC
  is the contract, provided `is_wm_name` learns `GOTABWM.ELF` (D5).
- Two seats exist from M57 onward, and since M59 the Go one is the
  default. The compiled default idles the pre-M59 way only when it is
  asked to (`settings set wm none`) or when the seat binary is not on the
  share — in which case the boot reports the miss and stays shim-only.
- `LIBUI.SO` / `user/src/lib/ui` become the toolkit of the dying Zig
  desktop, not a thing to port.

## Amendment (M60 / #1297, 2026-09-16) — no new Zig EL0 apps

The no-new-Zig-apps rule is now policy, not just a pivot-era restraint:

- **No new `user/src/*.zig` apps.** Zig owns `boot/`, `kernel/`, virtio,
  GIC, the scheduler, the syscall table, the Swift runner, and the serial
  monitor. New userland is Go (`GOOS=virelai` ELF). Leftover Zig EL0 is
  something you `exec`, then delete.
- TLS stays a Zig helper: `FETCHS.BIN` / `lib/tls` (ADR 0029). Do not
  port crypto "because pivot." **Superseded by the M71k amendment below:**
  the EL0 binary is gone and `lib/tls` is host-side interop only.
- SSH stays `SSH.BIN` (ADR 0025) until its own Go card.
- Deletions are one binary at a time, each independently revertible. No
  flag day. Boot default does not move here (M59 already flipped it).

**First leftover deletion (this card):** Zig `EDIT.BIN` (`user/src/edit.zig`)
is gone. The editor is `GOEDIT.ELF` (gate `go-edit`). CALC, NOTEPAD, and
TABWM remain hosted leftovers; TLS/SSH remain helpers.

## Not decided here

- Syscall rows, `tabapp` shape, GOTABWM internals, widget metrics — M56
  and later.
- Whether the interactive shell (`SH.BIN`) flips in this arc or after
  M60. D2 says "eventually"; no card above moves it.
- Go ports of TLS or SSH. (TLS landed in M67b and retired the helper in
  M71k; the SSH client landed in M71j #1569.)
- Deleting Zig TABWM (M60, after the default has already flipped).

## Amendment (M71k / #1570, 2026-09-21) — the TLS helper retires

M67 demoted `FETCHS.BIN`: `go-fetch-https`, `live-web` boot 12 and
`go-git` all prove HTTPS in-process over `user/go/tls`. M71k deletes the
EL0 binary. The last thing keeping it in the tree was its own gate.

- **The EL0 HTTPS consumer is Go.** `GOFETCH.ELF` and `WEB.ELF` dial
  through `user/go/tls`; the name / expired / chain negatives are pinned
  by `go-fetch-https`. `FETCHS.BIN` and `user/src/fetchs.zig` are gone,
  with the `fetchs` build step.
- **`lib/tls` stays, and it is not a library Go links.** Its `vectors/`
  (`tlsresponder.py`, `fx/`) are consumed by the `live-web` and
  `go-fetch-https` class-B gates, and `interop_driver.zig` +
  `vectors/run_interop.sh` still exercise the Zig client against real
  peers on the host. Nothing in the guest links it, so it is not a second
  shipped TLS stack — it is test apparatus.
- **The gate retired with the binary.** `live-tls13.spec` is gone; its
  probe is `live-web` boot 12 (same responder script, port and fixture
  identity). The negotiated-suite assertion that spec uniquely carried
  is enforced in code on the Go path rather than echoed: `user/go/tls`
  offers exactly one suite (`client.go` `suiteOffered = 0x1301`) and
  rejects any other ServerHello choice (`client.go:574`), pinned by
  `client_test.go`. A green handshake cannot hide a different suite, so
  no marker is owed.
- **The "no porting crypto" rule is unchanged.** The Go TLS client was
  written for its own card (M67b), not derived from `lib/tls`.

SSH remained `SSH.BIN` until M71j (#1569); see the amendment below.

## Amendment (M71j / #1569, 2026-09-21) — the SSH client retires

M51 shipped `SSH.BIN`. M70g G1 already moved the **server** to
`GOSSHD.ELF`. M71j deletes the last M51 product binary.

- **The EL0 SSH-2 client is Go.** `GOSSH.ELF` (`user/go/ssh/`) speaks
  ADR 0025 D2 over `vi.Dial`. The three client gates
  (`live-ssh-endpoint`, `live-ssh-negative`, `live-ssh-nocred`) stage
  `.build/go/GOSSH.ELF`. `SSH.BIN` and `user/src/ssh.zig` are gone,
  with the Zig client modules nothing else linked.
- **`lib/ssh` wire/packet/stream stay.** `SSHPACKET.BIN` still proves
  the stream adapter (`live-ssh-packet`). Same shape as `lib/tls`
  after M71k: host-side / packet-layer proof, not a second shipped
  client.
- **The "no porting crypto" rule is unchanged.** The Go client reuses
  `user/go/sshlib/` (split out of `user/go/sshd/` so GOSSHD and GOSSH
  share one crate). ADR 0023 D2 stands.
- **GOSSHD is not this card.** The in-guest server (#1491) stays.

## Amendment (A1 / #1865) — bounded native Zig portfolio exception

This amendment creates a closed exception to D4 and the M60
no-new-`user/src/*.zig`-apps rule. It qualifies D2's app-language rule
only for the four workloads below. It is not a reversal of the Go
desktop decision.

### A1.1 — Allowlist and first stage

Permit native AArch64 EL0 adapters, non-UI supporting libraries and
guest artifacts for these workloads only:

- Oliver: refresh the real-library native proof, then render/meta CLI
  operation with redirected input to EOF, HTML output and separate JSON
  diagnostics. Recursive planning/walk and mutating commands are deferred.
- k4o: the existing template/JSON engine and bounded render CLI, preserving
  its supported output formats and diagnostic contract.
- Boris: an explicitly single-job, serial, offline compile-and-publish
  artifact retaining the existing compiler and publication contracts.
  Require complete nested discovery, lossless names, stable identity,
  no-follow behavior and safe repeated publication. Resolve and verify
  the artifact's no-libc dependency closure; the audit did not prove it.
- fart-app: the actual deterministic synthesizer using caller-owned
  allocation, with bounded mono 16-bit 44.1 kHz PCM/WAV generation and
  deterministic WAV export. Optional finite native playback requires
  verified rate/channel/format conversion and raw PCM submission;
  WAV headers are never PCM. Go FART is not this workload.

Wrappers for this allowlist may be new Zig EL0 source, including under
`user/src/`. Changing a path or product name does not enlarge the exception.
Libraries exist to support these native guest artifacts, not to expose a
Zig library ABI to Go or create another app platform.

### A1.2 — Meaning of bounded

First-stage workloads are single-threaded, offline, finite operations
over explicitly supplied inputs and permitted guest/share paths.
They inherit existing principal/capability, path-containment and access
checks, and acquire no ambient host-command or service authority.

The approved target design must state numeric per-workload limits for
input/output bytes, aggregate heap and stack use, open handles, argv/env,
path/name lengths, and applicable tree depth/entry count and PCM duration.
It must map each limit to enforcement and a named error/refusal. Unknown
footprints are measurements owed, not an unlimited resource allowance.
No workload implementation starts before those budgets are approved.

The initial recipe consumes the existing eight 256-byte argv slots
(255 bytes plus NUL each) and sixteen 128-byte env slots on the
env-capable gap-ELF path. The program name occupies one argv slot.
It must satisfy the selected image shape's actual loader/link contract.
No limit increase, silent truncation or fabricated metadata is authorized
by this amendment. k4o's host 16 MiB input and 256 MiB output defaults
are not guest budgets by adoption.

Unsupported operations must be compile-time excluded or fail explicitly
at the supported boundary. Host std/libc fallback and success-shaped
stubs are forbidden. Failure must not contaminate normal output,
escape the share, or destroy the previous published artifact.

### A1.3 — Ownership and design gate

The project owner appoints one named accountable maintainer to the role
**Zig Guest Target/Backend Owner**. The appointment is recorded with
this decision; no unowned exception starts implementation.

That role owns the pinned Zig/dependency recipe, target/build/stdlib
integration, startup and image validation, native allocation and I/O
backend, honest unsupported behavior, workload budgets and regression
coverage. It owns upgrade checks and the decision to revalidate, hold
the pin or suspend a failing workload. It does not own the Go desktop
or obtain authority to change the kernel ABI without review.
The initial toolchain pin is Zig 0.16.0; R1 records dependency pins
and the integration recipe. Version changes require revalidation.

R1 supplies one approved cross-cutting target design before A2/A3 or
workload implementation. Astra is the requested R1 design role and Sol
the requested implementation role, not automatic assignments and not
a substitute for the accountable maintainer.

The separately owner-approved R1 design and workload budgets are recorded
in [ADR 0038](0038-zig-guest-target.md) (#1879), accepted on 2026-10-01.
Implementation remains gated on landing that accepted design together
with the maintainer appointment above.

The design maps each std/platform requirement to an existing native
facility, an adaptation/replacement, an honest refusal or a listed
native-facility card. It must not assume an upstream out-of-tree OS
plugin exists. A custom Io vtable is only part of the integration.
Supported hooks may include `root.os.heap.page_allocator`,
`std_options_debug_io`, `std_options_FilePermissions` and
`std_options_cwd`; missing POSIX-shaped std definitions do not mandate
corresponding kernel operations.

Any native ABI extension uses a separately accepted ADR 0007 amendment
and preserves existing consumers and security checks. A1 adds no slots.

### A1.4 — Non-goals and unchanged decisions

Go still owns the desktop, WM, chrome, general apps and interactive
shell. No new Zig GUI, widget toolkit, LIBUI revival, Go-to-Zig FFI,
Go kernel rewrite or crypto port merely because of this exception.
The Swift host launcher and Apple Virtualization.framework boot path
stay unchanged. No libc/POSIX guest dependency or Linux-ABI disguise.

No full Zig compiler self-hosting or expansion of `zc` into Zig/std
conformance is authorized. Host-built native Zig artifacts, the
in-guest `zc` dialect and a full in-guest Zig toolchain are distinct.

Boris watch/preview, parallel compile, online publication/authentication,
editor hosting and child-process capture are outside the first stage.
fart-app host UI, host speech/audio commands, NINJAM, capture and
continuous low-latency playback are outside it. Oliver walk/mutation
requires a later explicit scope approval after B1-B4.

B5 threading/TLS, B6 preview networking and B7 continuous audio remain
conditional later cards, not first-stage prerequisites or delivery
promises. Activating them requires owner-approved scope and target-design
updates with their own limits and evidence.

The `gotabwm` boot default, frozen `tabwm` fallback and `none` setting
are unchanged. TLS/SSH retirements and other leftover-retirement rules
remain in force. Refresh obsolete support claims only within this arc.

### A1.5 — Acceptance and maintenance

Permission is not a claim that a workload runs. Require reproducible
no-libc builds and artifact checks, then native VZ execution with
independent output comparisons and negative tests at the approved bounds.
Verify stream separation/EOF, cleanup and each workload's actual behavior;
a library compile or self-reported success marker is not CLI acceptance.

Refresh and extend `live-oliver` rather than duplicate its gate.
Verification follows repository gate policy: existing specs where
applicable, new declarative specs only where needed, and no new
`tools/verify-*.sh` scripts or committed run logs.

Record observed versus inferred behavior and the still-unsupported
features. An unmaintained or failing workload can be suspended without
changing Go ownership, the boot default or existing ABI consumers.
Adding another program requires another owner-approved ADR amendment.

## Proposed amendment (A2 / #1921) — standalone bounded QuickJS

**Not accepted or implementation permission.** This amendment and
[ADR 0039](0039-quickjs-runtime.md) require explicit approval from
**drawmeanelephant**, the appointed Zig Guest Target/Backend Owner,
before merge. The approval date/link must be recorded when received.
The R1 card is not complete without that approval; A1's four-program
list remains closed while A2 is proposed.

### A2.1 — One additional program, not another app platform

Permit **QJS.BIN only**, a standalone host-built native AArch64 EL0
interpreter for explicitly supplied JS files and a bounded serial
line-eval loop. Its adapter lives in `user/src/quickjs.zig`, uses the
existing `user/zig/` SDK, and links the exact Bellard QuickJS C tree
`04be246001599f5995fa2f2d8c91a0f198d3f34c` (MIT, version 2026-06-04).
Private pinned C inputs/headers/bridges and supporting runtime fixtures
live under `user/zig/quickjs/`; isolated build tooling lives under
`tools/quickjs-runtime/` and `tools/js-client/`.

This narrowly qualifies D2/D4/M60 for that program and its verification
fixtures. D3 is unchanged: **Go never links C/Zig, imports a C ABI or
gains cgo/FFI**. No C-to-Go transpile carrying an emulated libc, general
libc/POSIX compatibility layer, new Zig GUI/toolkit, kernel expansion
or boot-default change. Go owns the desktop, WM, apps and shell.

### A2.2 — A1.2-shaped, finite limits

QJS is single-threaded, offline and capability-limited, with one live
runtime/context. JS sees only `print` and `console.log`; explicit script
loading, tty commands and optional receipt writing are adapter authority,
not JS file handles. No DOM/web APIs, fetch/sockets, timers, npm, JIT,
upstream std/os library, workers, native modules or ambient host commands.
ADR 0028 D2 permanently excludes JS from WEB.ELF, and its negative
JS-in-WASM decision is not reopened.

ADR 0039 defines enforcement, named failures and exact measurements owed:

- Source/eval ≤1,048,576 B; logical output/eval ≤65,536 B; diagnostics/
  session ≤16,384 B; receipt/session ≤1,114,112 B.
- One aggregate **4,194,304 B** arena includes engine, SDK, source,
  buffers and allocation overhead; no second allocator or mapping.
- Native stack use ≤131,072 B inside the existing 196,608 B mapping,
  QuickJS's own C-stack limit 98,304 B; no worker threads.
- At most two open native file handles: script or tty plus receipt.
  `/host` paths ≤64 B full/31 B component; only the literal adapter-owned
  `/dev/tty` is a device exception. Preserve existing trust/containment.
  At most seven SDK resources including reserved stream/cwd identities
  and one transient directory pin. Receipts use contained exclusive
  creation; existing files are refused, never truncated or replaced.
- Seven user argv entries ≤255 B each; sixteen env entries ≤127 B each,
  no JS env API, clipping or ABI increase.
- File/mapped image ≤4,194,304 B each; data/BSS ≤1,048,576 B; engine+
  bridge binary delta ≤1,572,864 B against the identical empty adapter.
- Cold first successful eval ≤5,000 ms (maximum of five fresh launches);
  interruption completion ≤5,000 ms with a 4,000 ms internal deadline,
  monotonic-counter polling and bounded C slow paths, not coarse sleep.
- Interactive input ≤4,096 B/line, ≤256 evals, ≤60,000 ms idle,
  ≤4,194,304 B aggregate source and ≤1,048,576 B aggregate output/session.
- Exact canonical platform inventory/caps: **51 provided, 5 bounded-error
  stubs, 134 refused symbols plus 12 refused features (146 refusal entries)**.
  Compiler/header primitives and normalized aliases follow ADR 0039's
  explicit counting rule, not extra hidden allowance.

These are chosen ceilings, not claimed native measurements. A within-size
script may still refuse OutOfMemory. Unsupported operations are excluded
or explicitly fail; success-shaped stubs, silent truncation, host fallback
and continuing a poisoned context are forbidden. Resource interruption
resets the context; ordinary JS exceptions do not roll back prior state.
Host scheduling is not a hard-real-time guarantee, and harness timeouts
never establish a guest refusal.

### A2.3 — Native C recipe, ownership and acceptance

ADR 0038 still owns the pinned Zig 0.16.0 target, private std overlay,
entry, single arena and two-segment static gap ELF. QJS adds an isolated
recipe compiling C11 with that distribution's `zig cc` for
`aarch64-freestanding-none`, baseline CPU, `-Os`, freestanding/private
headers, x18 reserved, no libc/sysroot/fast-math/hosted startup or dynamic
linking. Link C objects and private native Zig bridges into the SDK's
ReleaseSafe single-threaded artifact; retain its ELF/parser checks.
The five core units are quickjs/dtoa/regexp/Unicode/cutils, never
quickjs-libc or the upstream shell/compiler. ADR 0039 carries complete
flags, source hashes, patches, platform dispositions and update rules.
No shared SDK/root-build change is implied.

Subject to explicit approval, **drawmeanelephant** owns this program's
pin/security monitoring, private C boundary, numeric limits, upgrades
and suspension on regression, alongside the existing target-owner role.
Owner-approved landing precedes M88b's complete offline runtime and
M88c's independently closable product/guest acceptance. Their disjoint
Touches and named file-eval, interactive-eval, refusal, bound, cold-start
and cleanup proofs are in ADR 0039; no implementation card is filed here.

Require no-libc reproducible builds and complete symbol closure, then
real VZ file/interactive execution, independent output comparisons,
all resource/refusal tests and reclamation evidence. A hosted object,
library compile or marker is not guest acceptance. Suspend a failing or
unmaintained QJS without changing the Go desktop or existing consumers.
Any additional program or authority requires another owner-approved
amendment; this is not an open-ended C/Zig userland exception.
