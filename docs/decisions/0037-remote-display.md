# ADR 0037: Remote display — the seams, the serving shape, and the auth posture

- Status: **ACCEPTED** · Date: 2026-09-27 · Milestone: **M84** (remote
  framebuffer) · Index **#1807** · Card **#1809** (M84a, this ADR)
- Related: ADR 0007 (syscall ABI), ADR 0009 (event wire), ADR 0015
  (userland WM seat), ADR 0016 (shared-anon), ADR 0020 (terminal seam /
  net front-end), ADR 0022 (remote console), ADR 0024 (trust baseline),
  ADR 0025 (SSH client-first), ADR 0031 (guest self-test), ADR 0033
  (GOTABWM is the tabbed desktop — no second compositor)
- No code lands here. No kernel change is required by anything decided
  below; had a seam been missing, this card's exit was to stop and
  report, not to detour (none was).

> Measures, before any RFB byte is written, what an RFB server can see
> and what remote input may touch: the frame source, the input injection
> point, the TCP listen seam, and host→guest reachability. Decides the
> serving shape (in-seat vs snapshot-fed daemon) and the auth/exposure
> posture — including the one the milestone headline depends on: what a
> Mac's Screen Sharing can actually speak to.

## Context — the seam survey (measured at `c4152178`)

Every fact below was read from the tree, not inferred. File:line refs
are the receipts; the VZ-boot receipts this card cannot capture on a
Linux VM are named in §"Blocked steps" and stay CI-verified.

### S1. The frame: seat-exclusive, 1280×720 BGRX

- The composited scanout is `virtio_gpu.gpu_fb`: 1280×720, B8G8R8X8_UNORM,
  3,686,400 bytes (`docs/hardware-contract.md` framebuffer-snapshot
  section; `user/go/vi/wmseat.go:43-47` `ScanoutWidth/Height/BPP/FbBytes`).
- The seat maps it writable via `vi.MmapScanout()` → `MmapHint(M33ScanoutTag,
  …)` (`user/go/vi/wmseat.go:92-97`) — the M33/ADR 0016 shared-anon seam.
- The bind is **seat-exclusive**: `wm_server.scanout_bind` returns 0
  unless `pid == wm_pid`, the registered WM seat
  (`kernel/src/wm_server.zig:438-444`; "the WM seat is the privilege").
  No other EL0 process can map the scanout. The frame lives in the seat
  process's address space — exactly as the card stated.
- Present is `vi.WmctlRequestPresent()` = slot 65 cmd 3
  (`user/go/vi/wmseat.go:76-79`); the seat's composite loop is paced by
  the kind-18 tick (`user/go/gotabwm/seat.go:265-303`,
  `EvCompositeTick = 18` at `user/go/vi/wmseat.go:51-54`).
- **No damage tracking exists**: `compositeTick` repaints whole frames
  (`user/go/gotabwm/seat.go:382-420`). An RFB server must diff frames
  itself (a 3.7 MB memcmp per frame is cheap; hextile/RRE are
  diff-friendly encodings — M84b's problem, not a seam problem).
- Host-side frame reads have one precedent: queue-4 kind-4 snapshots
  stream `gpu_fb` to the host (`docs/hardware-contract.md:320+`). It is
  host-side only — not usable by in-guest code.

### S2. Input: the seat drains kinds 19/21; synthesis is in-process

- `EvWmPointer = 19`: `arg0 = x|(y<<16)`, `flags` low byte = buttons
  (`user/go/vi/wmseat.go:60-62`).
- `EvWmKey = 21`: `arg0` = HID usage, `flags` = ADR 0009 key flags
  (`user/go/vi/wmseat.go:65-67`).
- The seat drains them with `vi.PollEvent()` (slot 21) through
  `drainSeatEvents` → `consumeSeatEvent` → `handleWmPointer` /
  `handleWmKey` (`user/go/gotabwm/seat.go:298`, `:512-533`,
  `user/go/gotabwm/hid.go:501+`).
- Queue-3 kinds 1/2 carry **host→guest** HID injection (raw 8-byte
  keyboard / 5-byte pointer reports); the contract states injected
  reports go through `input.decode_keyboard_report` verbatim and land in
  the per-process event FIFO "when an app window owns focus"
  (`docs/hardware-contract.md:199-230`). That direction is host-operator
  → guest. There is **no EL0→EL0 input-injection syscall** — a separate
  RFB daemon process cannot inject into the seat's queue.
- Consequence: the only kernel-change-free injection point is
  **in-process synthesis inside the seat**: build a `vi.Event` with kind
  19/21 and call `consumeSeatEvent` — the exact function the hardware
  path uses. RFB PointerEvent maps to absolute x/y directly (1280×720);
  RFB KeyEvent needs the keysym→HID-usage table (M84b's deliverable —
  keysyms are X11 keysym *numbers*, a data table, not X11).

### S3. TCP: one system-wide connection, no loopback, listen exists

- `vi.Listen(port)` — slot 30 passive-open (`ip==0`) — is the inbound
  seam (`user/go/vi/conn.go:131-148`); proven by GOSSHD.ELF (M70g G1,
  default 2222, `user/go/sshd/main.go`) and GOHTTPD.ELF (M71l).
- The kernel TCP is a **single system-wide connection**: `owner_pid`
  (`kernel/src/tcp.zig:348-367`, `:497`). While the RFB server holds it,
  no other process (GOSSHD, fetch, DNS) can use TCP. This is a
  deployment constraint, not a blocker — recorded in D5.
- **No TCP loopback**: "an own-IP connect is refused `.no_peer`"
  (`kernel/src/tcp.zig:29`). A guest process cannot TCP-connect to the
  guest's own listen port. TCP-in-TCP tunneling inside the guest is
  impossible — this decides the tunnel shape in D6.
- Bounds: `payload_max = 192`, one RX segment, no reassembly
  (`kernel/src/tcp.zig:59-67`). RFB's client messages are small; fine.

### S4. Reachability: gates yes, real iron no (recorded negative)

- **Gates**: the runner's `--net-tcp-connect <ip>:<port>:<mode>`
  emulation is the host→guest TCP path; `live-ssh-server.spec` proves a
  guest listen (GOSSHD) against the runner's SSH-2 client, hermetic via
  `--net`. M84c's `live-rfb` gate inherits this topology.
- **Real Apple-VZ iron**: `--net-nat` gives the guest outbound NAT only;
  there is **no NAT port-forward, no runner host-listen bridge mode, and
  no guest SSH `-R`** (`user/go/ssh` has no remote-forward). A Mac cannot
  open a TCP connection to the guest today. This is a recorded negative,
  not a gap in the card: M84's gates stay hermetic; the real-device
  headline needs a named follow-up (§"Open issues").
- ADR 0025's Context 1 ("the only host→guest path today is the
  custom-virtio console") is therefore still true outside `--net`
  emulation; GOSSHD moved the *guest* side, not the reachability.

### S5. Trust toolkit (ADR 0024, unchanged)

- Principals `uid_system`/`uid_user`, caps; remote input is a privileged
  capability, not a courtesy (this card's brief).
- M50 TS4 replaced the M46 in-band secret with HMAC-SHA256/Ed25519
  challenge-response (`VIRELAIOS-AUTH/1`); the proven strong remote auth
  in the tree is **SSH-2 publickey via GOSSHD** (Ed25519 host key from
  `ssh-host-ed25519`, `SSH/AUTHORIZED_KEYS` — M70g G1, gated).
- M47 crypto is userland-Zig-only; there is **no DES** anywhere in the
  tree (matters for D6).

### S6. The fallback that was not needed (ADR 0016)

Had the frame seam been missing, the fallback was a snapshot-fed daemon:
the seat copies each presented frame into an ADR 0016 shared-anon region
(`handle_mmap_shared`, `kernel/src/shared_region.zig` /
`shared_mmap.zig` — owner-RW/peer-RO wiring exists, max 8 regions) and a
daemon serves it. Measured cost: a 3.7 MB copy per present, region
management, half-frame tearing sync — and input would *still* need the
seat's cooperation (a new WM_RPC message type), because cross-process
injection does not exist (§S2). Every seam the in-seat shape needs
exists; the fallback is recorded and **not taken**. No kernel change is
required by this milestone.

## Decision

### D1. Serving shape: in-seat. The server reads its own scanout and
### synthesizes its own events. It serves the composite; it never composes.

The RFB server runs in the GOTABWM.ELF seat process (as a module or
goroutine of the seat — M84c's placement call, not this ADR's):

- **Frame**: the seat's existing writable scanout mapping (§S1). Zero
  copy, zero new seam. Against ADR 0033: this is not a second
  compositor — the server *serves the composite the seat already
  produced* and never composites. The "no second compositor" line holds
  because no new present path, surface, or composition policy is added.
- **Input**: in-process synthesis of kind-19/21 `vi.Event` into
  `consumeSeatEvent` (§S2) — the same routing hardware input takes, with
  no kernel change. `drainSeatEvents` already takes the poll function as
  a parameter, so synthetic events can be interleaved with the hardware
  queue without forking the loop.
- **TCP**: `vi.Listen` in the seat process (§S3).
- **Rejected: snapshot-fed daemon** — §S6. More moving parts, a
  per-present 3.7 MB copy, and no benefit: the frame seam it works
  around is not missing.

### D2. Present/damage: the server diffs; the seat is the clock

No damage regions exist in the seat and none are added. The server keeps
the last-sent frame, memcmps per present (it *is* the presenter, so
present cadence is observed directly, not polled), and encodes the diff
with hextile/RRE (M84b). Full-frame updates on demand stay legal.

### D3. Remote input is a privileged capability — gated at the session,
### the seat, and the boot path

- **Session**: only events from an authenticated viewer session are
  synthesized. The authentication *is* the control (D6) — there is no
  weaker "courtesy" tier.
- **Seat**: the seat is the trust boundary. Its scanout mapping and
  event queue are already `wm_pid`-gated (§S1); remote input inherits
  exactly the local seat's authority — no more, no less. A viewer can do
  what the local user can do, because it *is* the local user's input
  path. This is stated plainly so no later card "discovers" extra
  privilege: remote input === local input, and the viewer is
  authenticated as the operator.
- **Boot path**: the RFB server is never on it. Explicit operator start
  only (GOSSHD precedent: "Not on the boot path",
  `user/go/sshd/main.go`). A boot that never starts RFB is byte-identical
  to today.
- **Teardown**: M84d owns client-death honesty (M52's bar) — a viewer
  dying mid-drag must not wedge the seat's input routing. The in-seat
  shape makes this *easier* (no cross-process state), not free.

### D4. TCP ownership: while serving, the seat owns the one connection

§S3's single system-wide connection means an RFB-serving seat and
GOSSHD/fetch/DNS cannot use TCP concurrently. Operational consequence:
the RFB server and the SSH server are never co-tenants of one boot in
M84 — the operator picks the remote-access mode per session. A later
card may multiplex (SSH-channel-plumbed RFB, §D6), but M84 does not pay
for it.

### D5. Reachability posture: hermetic gates now, named follow-ups for
### real iron

- M84b/c/d run against the `--net-tcp-connect` emulation (the
  `live-ssh-server` topology). The runner is the trusted peer; the link
  is hermetic.
- Real-device inbound ("a Mac opens a socket to the guest") is a
  recorded negative (§S4). Enabling follow-ups, neither in M84:
  1. **Runner `--net-tcp-bridge`**: host listens on a Mac port and
     bridges into the `--net` emulation toward the guest's listen.
     Host-Swift-only work, no guest or kernel change. This is what the
     M84d Screen Sharing tape needs on real iron. **Trusted-local test
     path only**: the bridge binds 127.0.0.1, same user, same machine —
     it authenticates nobody and is not a remote-access story (see D6).
  2. **Guest SSH `-R`**: the guest dials out (NAT allows it) and
     reverse-forwards; needs `-R` in `user/go/ssh` (does not exist).
- M84 claims nothing about real-device inbound. The milestone is
  complete when the hermetic gates are green and the tape exists via
  follow-up (1).

### D6. Auth/exposure posture — the critical call

**On the RFB wire the server offers exactly one security type: None
(RFB 3.8 §7.2.1). "No unauthenticated direct RFB" is enforced by
exposure, not by the wire.**

Why None is the only honest offering:

- macOS Screen Sharing speaks None and VNC-password-auth on a standard
  connection. The milestone's headline ("a Mac can Screen-Share into
  it") and M84d's tape require one of the two.
- **VNC password auth is rejected.** It is DES-based (8-character
  password limit, cryptographically broken challenge-response), DES
  exists nowhere in the M47 tree, and minting broken crypto for a
  compatibility checkbox would be a deliberate downgrade from the
  project's established remote-auth bar (SSH-2 publickey, M70g G1).
  The project's modern-primitives culture does not mint DES.
- **A custom challenge security type is rejected.** Screen Sharing
  would not speak it; the headline dies.
- **VeNCrypt/TLS is deferred** to a later card: it needs a Go TLS
  *server*, and M53 shipped a TLS 1.3 *client* only.

Why exposure-enforcement satisfies "no unauthenticated *direct* RFB":

- The server is never on the boot path (D3) and never bound for LAN
  exposure by default; the operator starts it explicitly per session.
- There is **no authenticated remote path in M84**, and this document
  does not claim one. D4 rules out guest-side sshd co-tenancy in a
  serving boot (one system-wide TCP connection), and D5(1)'s bridge
  authenticates nobody — so "authenticated SSH tunnel" cannot describe
  anything M84 ships. Remote use of the RFB server is **unsupported**
  until an authenticated transport exists (the SSH-channel-plumbed
  card named below, or a TLS-server card).
- What M84 does have: the hermetic `--net` gates (the runner is the
  trusted peer) and the trusted-local bridge (D5(1): 127.0.0.1,
  same-user) for the M84d tape. Both are same-trust-domain paths;
  neither crosses a network an adversary can reach.
- Binding the RFB listen to a routable network without the tunnel is an
  operator error the docs forbid — it is not a mode the server
  advertises, defaults to, or documents.

The strong path (later card, not M84): **SSH-channel-plumbed RFB**.
Measured constraint (§S3): the kernel TCP is a single system-wide
connection with no loopback, so TCP-in-TCP tunneling inside the guest is
impossible — "GOSSHD port-forward" can only mean the SSH server
accepting a tcpip-forward channel and piping it **in-process** to the
RFB codec. Therefore **M84b must write `user/go/rfb` transport-agnostic**
(byte-stream in/out, no socket assumptions), so the later card can plug
it onto an SSH channel without touching the codec. This is a
hard requirement on M84b, recorded here so the codec's shape is right
the first time.

### D7. What M84b/c/d inherit (binding)

- M84b: codec over an abstract byte stream (D6); raw + hextile + RRE;
  keysym→HID-usage table (X11 keysym numbers as data); fixtures pinned.
- M84c: in-seat server (D1); `live-rfb` gate on the `--net` emulation
  (D5); rfbprobe speaks plain RFB 3.8/None to the emulated peer — the
  gate proves the wire and the pixels, the ADR proves the posture.
- M84d: negatives + client-death (D3); the Screen Sharing tape is
  class-C and needs follow-up D5(1) on real iron — the tape is not
  blocked on M84's gates.

## Consequences

- M84 needs no kernel change, no new syscall, no ADR 0007 amendment.
  The card's stop-and-report exit was not taken.
- GOTABWM grows a network server. The seat is already the trust hub
  (WM_RPC, hosted apps); the RFB module is still explicit-start-only and
  single-connection, and the seat's demo/live loop is untouched when RFB
  is not started.
- The auth story is honest about what it is: wire-None for Screen
  Sharing compatibility, with authentication living at the SSH layer and
  exposure controlled operationally. Any future reader who wants
  wire-level auth gets the VeNCrypt/TLS card, not a DES revival.

## Rejected alternatives

- **Snapshot-fed daemon** (§S6): all cost, no benefit; the seam it
  routes around exists.
- **VNC password (DES) auth** (D6): broken crypto, absent from the
  tree, 8-char passwords — a downgrade from the SSH-publickey bar.
- **Custom Ed25519 challenge as an RFB security type** (D6): kills the
  macOS headline; Screen Sharing would not speak it.
- **TLS-wrapped direct RFB in M84** (D6): needs a Go TLS server; M53
  was client-only. Later card.
- **Cross-process input injection via a new syscall** (§S2): a kernel
  change this card forbids; in-seat synthesis covers it.
- **Offering `None` *and* documenting direct LAN use**: refused — the
  exposure posture (D6) is load-bearing, not decorative.

## Open issues (not M84)

1. Runner `--net-tcp-bridge` host-listen mode (enables the M84d Screen
   Sharing tape on real iron). Host-Swift only.
2. Guest SSH `-R` remote-forward (the pure-SSH real-device path).
3. Go TLS server (the VeNCrypt/TLS direct-RFB card).
4. SSH-channel-plumbed RFB (the D6 strong path; needs M84b's
   transport-agnostic codec + a tcpip-forward-capable SSH server).
5. A loopback-bind seam for slot 30, if the threat model ever needs
   bind-address enforcement instead of operational posture. Kernel
   change — deliberately not proposed here.

## Blocked steps / observed vs inferred (AGENTS.md)

- **Observed** (read from the tree at `c4152178`, refs in §Context):
  scanout geometry/format and seat-exclusive bind; event kinds 19/21 and
  their arg layouts; `consumeSeatEvent` routing; `vi.Listen` semantics;
  single system-wide TCP connection; no TCP loopback; `--net-tcp-connect`
  emulation; GOSSHD/GOHTTPD as listen precedents; no NAT port-forward,
  no runner bridge, no guest `-R`; no DES in the tree; no damage
  tracking; shared-anon fallback shape.
- **Inferred** (reasonable, flagged): macOS Screen Sharing's supported
  security types on a standard connection (None, VNC-password) — from
  the RFB 3.8 spec and Screen Sharing's documented behavior, not from a
  packet capture; the judgment that DES-minting is the wrong trade
  (D6) — a values call, stated as such.
- **Blocked**: VZ-boot probe receipts (frame address, present-cadence
  observation, `--net` reachability) — no Apple Virtualization on this
  Linux VM. The seam *shapes* are code-measured above; the boot receipts
  stay open for the reference-host runs (per AGENTS.md, 0 class-B gates
  run in CI — the reference host runs them out-of-band). Nothing in this
  ADR claims a receipt that was not captured.
- **Not blocked**: `go test ./vi/...` host suite — green on this VM
  (Go 1.24.7 toolchain, repo-pinned go1.27.0 downloaded for the module).
  No Go code was changed by this card (docs-only diff).
