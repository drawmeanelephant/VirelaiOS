# ADR 0022: Remote console — the host bridge and the authenticated guest session

Status: **ACCEPTED** · Date: 2026-09-11 · Milestone: **M46** (remote access,
Stage 0/1) · Goal **#1066** · Umbrella **#1108** · Cards **#1109** (this ADR),
**#1110** (host bridge), **#1069** (vgate client hook), **#1111** (guest auth),
**#1104**/**#1105** (net-front-end follow-ups), **#1112** (gate)

> Fixes the two remote-access tranches the M45 terminal seam left open: the
> **Stage 0 host console bridge** (already landed as `--console-tcp`, PR
> #1070) and the **Stage 1 authenticated guest remote** over the net front-end
> (ADR 0020 Amendment B). Extends, and does not revise, ADR 0020 (D1–D5),
> Amendment A, and Amendment B (B1–B6). No code lands here.

## Context

Goal #1066 ("get in and out") staged remote access. M45 landed the machinery
that makes it real:

- **ADR 0020 + Amendment B** fix the **net front-end** (selector `3`): a
  process enters LISTEN through the single bounded `kernel/src/tcp.zig`
  connection and pumps raw bytes between its `/dev/tty` and that connection.
  `SH.BIN net [port]` hosts it; the class-B `live-remote` gate proves a host→
  guest inbound connection drives a shell (PR #1103).
- **PR #1070** landed the **Stage 0 host bridge**: runner `--console-tcp
  [host:]port` serves the guest serial console on a TCP socket (single client,
  raw, host-side only).

Two gaps remain that the B5 posture explicitly deferred:

1. **No automation for network-interactive gates.** `vgate_setup_python` runs
   before the run and assert hooks run after it; nothing can act as a
   concurrent TCP client *during* a boot. So `--console-tcp` and the net
   front-end have no declarative coverage in the frozen spec format (#1069).
2. **No guest-side authentication.** Amendment B B5 is plaintext, open to
   anyone who can reach the port. #1111 asks for a bounded v1 gate before
   M47/M50 crypto (#1113/#1138) exists.

## Decision

### D1. Scope: Stage 0/1 only; plaintext is a decision, not an accident
This ADR fixes the host bridge transport, the v1 guest auth primitive, the
trust posture, and the gate topology. It deliberately does **not** add
encryption, TLS, SSH, a crypto library, multi-connection TCP, or session
resumption — those are M47 (crypto, #1113) and M50 (authenticated remote,
#1138). Until then every remote byte is **plaintext** and the secret below is
a barrier against accidental exposure, **not** a cryptographic control.

### D2. Host bridge transport (Stage 0, `--console-tcp`)
- **Transport:** raw TCP, no telnet IAC negotiation. The socket is byte-for-
  byte the same stream the `--console` plumbing bridges (client bytes → the
  guest serial input attachment; guest output → every connected client).
  `nc`/`telnet`/`socat` all work; a terminal client may need `--crlf` (the
  guest RX accepts CR or LF).
- **Single client at a time.** ~~A second connection waits in the listen
  backlog; the bridge re-accepts after the client disconnects.~~ **Amended
  by M70g G2 (2026-09-18, #1459):** the accept loop keeps accepting while a
  client is served and answers every extra connection `console-tcp: busy\n`
  then closes it — no backlog limbo, the refusal is observable. The seat is
  freed the moment the served socket closes.
- **Bounded open ends (M70g G2, #1459).** Pre-auth: a client that does not
  answer the challenge within **10 s** is dropped with `console-tcp: auth
  timeout\n` (the connected-but-mute shape of a half-open peer); the MAC
  compare is CryptoKit's constant-time check, never a string equality.
  Post-auth input: more than **1024 bytes without CR/LF** (4× the guest line
  editor's 256-byte `max_line`) is refused with `console-tcp: line too long\n`
  and the client is dropped; nothing from the offending read reaches the
  guest. Output: the client socket carries `SO_SNDTIMEO` = 2 s, and a client
  that stops draining is dropped rather than allowed to stall the guest-output
  tee; `SO_KEEPALIVE` (15 s idle, 3 × 5 s probes) detects a peer that vanished
  without a FIN. Keepalive is configured, not gate-observed (no gate can drop
  packets); the auth deadline is the gated proxy.
- **Teardown leaves no zombie seat state (M52's lesson).** When a client
  disconnects — for any reason — with a partial line outstanding (bytes since
  its last CR/LF), the bridge writes one `0x03` (Ctrl-C) into the guest
  serial input; `lineedit.zig` echoes `^C\r\n` and clears the line, so the
  next client starts at a clean prompt. Serial evidence: `<partial>^C`.
- **Bind address:** `[host:]port`, default `127.0.0.1`; `0.0.0.0` exposes it
  to the LAN (the operator's explicit choice).
- **Optional bridge secret:** `[host:]port:secret`. When set, the bridge
  requires the client's **first line** to equal `secret`; on mismatch it
  writes `console-tcp: auth failed\n` and closes the client (relistening). On
  match the line is consumed and the rest of the stream is bridged.
  (M50 TS4 replaced the equality with the HMAC challenge-response, ADR 0024
  D7.) **M70g G2 (#1459):** `--console-tcp-secret-file <path>` supplies the
  secret from a file's first line so it never appears in the process list;
  it is mutually exclusive with the inline form, which now warns on stderr.
  The bridge never echoes the spec or the secret into any log line (a bad
  spec fails without quoting it); `live-remote-console` run 03 pins that the
  secret text is absent from runner output.
- **Boot default unchanged:** `--console-tcp` is a runner flag; without it the
  VM and every existing gate are byte-identical.

### D3. Guest session auth (Stage 1): an in-band shared secret
**Superseded in part by M50 TS4 (2026-09-11, issue #1138, ADR 0024 D6).**
The v1 shared secret documented below is deleted; selector 3 now takes an
auth scheme (0 open / 1 hmac-sha256 / 2 ed25519), the pump frames a fresh
`VIRELAIOS-AUTH/1` challenge, and the attached process votes the verdict
through slot 71. The explicit `open` mode is the only way to reproduce the
v1 accept-immediately posture. The historical text is kept for the v1
record; see ADR 0024 D6 + the ADR 0007 slot-71 amendment for the binding
shape.

The v1 guest primitive is a **shared secret sent in-band as the first line**,
checked by the kernel net-front-end pump:

- CLI: `SH.BIN net <port> [secret]` (the default port stays `2323`; no secret
  keeps today's SH7 behavior exactly — accept immediately).
- ABI: `sys_tty_attach(3, port, secret_ptr, secret_len)` (slot 67 gains two
  arguments; `secret_ptr = 0` / `len = 0` means "no secret"). The kernel copies
  the bounded secret (≤ `terminal.net_secret_max` = 63 bytes) into the
  terminal's net binding through `uaccess`. No cross-process capability is
  added (Amendment B B2 reasoning still holds).
- Enforcement is in the pump, asymmetric with the shell: **before** a session
  is authenticated the net pump does **not** forward received bytes to the
  terminal input queue. It accumulates them in a bounded challenge buffer
  until a newline; byte-equality with the secret (trailing `\r` trimmed)
  authenticates the session and consumes the line (the shell never sees it);
  any mismatch sends `auth failed\n` over the connection and ends it
  (`tcp.reset()` + detach, the B4 path). A pre-auth byte flood is bounded by
  the challenge buffer and the stack's 64-byte `payload_max`.
- The secret is compared with `std.crypto.utils.timingSafeEql` where available
  (bounded constant-time intent); this is documented as best-effort, not a
  side-channel guarantee.

### D4. Optional source-IP allowlist (the "and/or" of #1111)
**TS4 note (2026-09-11):** the kernel-side opt-in documented below remains
(a4 of `sys_tty_attach` selector 3), but the M50 TS4 userland CLI is
`net <port> [open]` and no longer exposes an allow-ip argument. No gate used
it; the kernel field stays available for a future front-end.

`sys_tty_attach(3, port, secret_ptr, secret_len, allow_ip)` gains a fifth
argument: a **source-IP allowlist** entry as a big-endian `u32` (`0` = any).
CLI: `SH.BIN net <port> [secret] [allow-ip]`. When set, the TCP passive-open
path refuses a SYN from any other source IP with an RST (`rst_sent`,
`auth_rejected` counters) and **stays listening** — the allowed host can still
connect. The secret and the allowlist compose: either one failing rejects the
session. The allowlist is opt-in and costs nothing when unset (the SH7 path).

### D5. Gate topology: the declarative during-run TCP client hook
A network-interactive class-B gate needs a client that lives **while the VM
runs**. The vgate format gains one declaration-order directive (its own issue,
per `tools/gate/SPEC.md`; pilot below):

```
vgate_client TAG --addr HOST:PORT [--after MARKER] [--send-file FILE]
    [--send-text TEXT] [--expect TEXT] [--timeout SECONDS]
```

The harness launches a background `python3 tools/lib/vgate-client.py` for the
run tagged `TAG`, then runs VMRunner in the foreground, then waits for the
client and fails the run if it exits non-zero. The client waits for `MARKER`
in the run's serial log (empty/absent ⇒ connect immediately), connects with
retry until `--timeout`, sends the fixture, reads until `--expect` appears (or
EOF/timeout), and writes its capture to `$RUN_DIR/client-TAG.out`. Assert
kinds `client-contains TAG STR` read that capture. `$RUN_DIR`, `$VG_SER`,
`$VG_TAG`, `$VG_CLIENT_ADDR` are exported to the hook, so a spec that needs
bespoke logic can still use `vgate_setup_python`'s sibling `--script`.

This is the single mechanism for host↔guest interactive gates; the console
bridge, remote shell, HTTPD, and DNS all use it.

### D6. What Stage 1 guarantees (and what it does not)
Guaranteed: a host can reach the guest shell over TCP; a wrong secret (or a
non-allowlisted source IP) is rejected; a correct secret yields an
interactive shell; disconnects auto-detach; boot default is unchanged.
Not guaranteed: confidentiality, integrity, replay/freshness, mutual
authentication, or availability. "Trusted network / behind the VZ NAT
boundary" remains the deployment assumption (Amendment B B5), now with a
door.

## Consequences

- `--console-tcp` gains the optional secret and a class-B gate; the runner is
  the only Stage 0 surface and stays host-only.
- The terminal net binding grows a bounded secret + auth state; the net pump
  gains the half-open accept timeout (#1105) and an injectable TX/RX seam so
  the byte movement is host-testable.
- `TERM.BIN` accepts the same `net [port] [secret] [allow-ip]` front-end
  argument as `SH.BIN` (#1104), sharing the argv helper.
- The vgate format is extended once, with a pilot, as the frozen-format rules
  require.
- The kernel is untouched on the boot path: nothing attaches until a process
  asks, and the monitor keeps the raw console.

## Rejected alternatives

- **A challenge/response (HMAC) handshake now.** Would need SHA-256/HMAC and
  a nonce, i.e. M47 crypto (#1113). The M50 card #1138 already owns the
  authenticated-remote upgrade over this same seam; v1 stays a shared secret.
- **Checking the secret in userland (`SH.BIN`).** The shell core is a
  byte-driven line editor that must stay front-end-agnostic (ADR 0020 A5/B3);
  a userland check would either leak the pre-auth bytes to the editor or fork
  the shell core. The kernel pump is the one place every front-end shares.
- **Source-IP allowlist only, no secret.** Rejected as the *default* because
  the gate's host client has a fixed source IP (hard to prove reject/accept in
  one boot); it is kept as an opt-in (D4) that composes with the secret.
- **A separate `REMOTED.BIN` / cross-process front-end.** Already rejected by
  Amendment B; the remote session is the shell through a fourth front-end.
- **Telnet IAC negotiation in the host bridge.** Adds a state machine and
  terminal-type negotiation for no Stage 0 benefit; raw bytes are enough for
  `nc`/`socat` and for a gate.

## Open issues left by this ADR

- Replace the shared secret with an HMAC/Ed25519 challenge over the same seam
  (M50 #1138; requires M47 crypto #1113).
- Multiple concurrent remote sessions (needs a multi-connection TCP stack).
  The host bridge stays single-client by decision (D2, M70g amendment).
- A `settings` key for a persistent listen port / allowlist (surfaced as a
  follow-up; the CLI is the v1 mechanism).
- Rate-limiting / lockout after repeated failed secrets.

## Amendment: M87 SSH-to-host operator deployment (#1858–#1860)

The supported remote product boundary is **host OpenSSH → host interactive
client → loopback HMAC serial bridge → guest GOSH**. This does not repair or
expose GOSSHD, guest `pty-req`, or guest TCP. Host authorization is
guest-console operator authority. Remote Login, authorized SSH keys,
verified host keys and SSH reachability are operator prerequisites; the
tools never enable services, elevate privileges, alter network policy or
disable host-key checks. SSH supplies confidentiality; HMAC does not encrypt.

`tools/remote-terminal.sh serve --share /absolute/remote-share` is a
foreground persistent-VM service. `attach --share /absolute/remote-share`
uses M87b's exact `--addr`/`--secret-file` client contract. The wrapper only
binds `127.0.0.1`, defaults to port 24681, validates the port, and stores a
random mode-0600 secret inside mode-0700 private session state **outside the
guest share**. The secret never enters argv, environment, logs or tapes and
is reused for the service's whole lifetime. A per-share host lock and port
preflight refuse a competing serve. Unknown stale state is refused, not
deleted/resecreted; investigate it before manually removing it.
An empty, private per-share lock inode remains for race-free restarts; it
contains no credential.

The dedicated share is seeded with `#v2\nwm=none\nshell=sh\n`, source-fresh
GOSH and serial support files. Existing documents/history and comment-only
`.virelairc` are preserved. Another settings mode, active `.virelairc`, or
nonempty unconfigured share is refused, never rewritten. This is an explicit
serial-only mode, not a boot-default flip or a dependency on the Go seat.
Executable/support names are distribution-owned and refresh at restart.

The VM runs with `--timeout 0`, private vars and a throwaway overlay. Client
Ctrl-C cancels the guest line, Ctrl-] only disconnects, and mid-line
disconnect injects the existing cancel byte. Reattach uses the same live
VM. Service SIGINT/SIGTERM stops only its own runner and removes only its
own temporary state/vars/overlay, never the persistent share. Ctrl-D on an
empty GOSH line, `exit`, or `monitor` hands back the raw kernel monitor;
attach refuses that as a shell session until GOSH is resumed or restarted.

M87b owns the client (#1858), M87c quiet attached serial (#1859), and M87d
the wrapper, five class-A contracts, existing `live-remote-console` extension
and real two-machine SSH fixture (#1860). Class B enforces every child exit
and compares guest-written receipt/history bytes; a bare monitor or command
echo is insufficient. Class C's consented tape checks distinct machine and
same-live-VM identities, verified-key SSH, GOSH identity, receipt, editing,
history/cancel, UTF-8/ANSI and external command. It never falls back to a
local peer. Missing prerequisites leave product acceptance open.

This is serial byte rendering by the host terminal. No kernel Screen,
PTY-size negotiation, graphics/resize, concurrent desktop/serial tty, LAN
binding, RFB or expanded TUI compatibility is promised. M87 completes only
with A/B/C and both desktop and remote product-journey evidence;
`docs/status.md` remains its single compact status row.
