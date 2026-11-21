# live-net-hub.spec -- M206b (issue #3814, index #3812): the first gate that
# boots TWO concurrent VirelaiOS VMs on one --net-hub segment and proves they
# share a LAN — ARP + ICMP echo both directions, then VM B browsing VM A's
# GOHTTPD --advertise over mDNS and fetching its /healthz. No entitlements:
# both NICs ride --net socketpairs relayed by card a's learning hub.
#
# Layout — two runners on one hub path (first-runner-binds, ADR 0057):
#   * VM B (background): the setup hook spawns a second VMRunner under
#     peer_watch (the h1/m1 background-process shape, scoped to this gate
#     invocation) — --mac 02:00:00:00:00:0b on --net-hub $RUN_DIR/nethub.sock.
#     Started first, it OWNS the segment switch + the hub-side DHCP responder
#     (one lease per MAC, 10.0.0.16-31). Console-mode --console-tcp
#     127.0.0.1:24720: it sits at `virelai> ` until the run-a client types its
#     legs; peer_watch kills it at gate end. Serial: vm-serial-vmb.log, lifted
#     into artifacts/ by the python leg (the run dir is deleted at gate_end).
#   * VM A (foreground vgate_run a): --mac 02:00:00:00:00:0a, attaches to the
#     same hub path as a client (`NET-HUB: attached to hub ... as client` on
#     stdout), DHCP-binds 10.0.0.16 — the pool's first lease, guaranteed
#     because B's guest does not DISCOVER until the client types it.
#
# Legs (each names the VM it asserts):
#   * s1 on vm-a: dhcp pump -> `net: dhcp bound ip=10.0.0.16` ->
#     `exec GOHTTPD.ELF --advertise v206b`. 'httpd: advertise sent' is A's
#     announce receipt; 'httpd: advertise burst done' (after the third emit)
#     is the client's --after marker — the post-burst discovery boundary.
#   * client on run a (the vgate_client pattern, vgate.sh :189/:580): waits
#     for A's 'httpd: advertise burst done' — the post-burst boundary — then
#     drives VM B's console over the TCP bridge: dhcp pump (B binds 10.0.0.17
#     — the second lease), a paced ARP settle and the B->A ARP/ping train,
#     then ONE sequential GOSH whose children are `GONETSTAT.ELF discover
#     _http._tcp` (a real query: A's responder answers after the burst has
#     finished; B's browse folds the reply into the exact service/IP/port
#     line) and `GOFETCH.ELF http://10.0.0.16:8080/healthz` — the fetch
#     cannot start before the browse prints its resolution. The capture
#     lands in client-a.out, stopped at GOSH's own post-chain marker so the
#     resolved line is present; every line also lands in B's serial.
#   * s2 on vm-a is gated on 'httpd: request /healthz' — a marker only B's
#     fetch can produce, so B is provably bound — then A arps + pings B and
#     echoes the run's end marker. The 3 s forward delay lets B's fetch
#     print its tail before A's run ends.
#
# Asserts: A's serial pins bound/advertise/request/served + the A->B arp and
# ping counters; the client capture pins B's bound/arp/ping/fetch lines; the
# python leg polls B's serial for the discover + fetch records (the browse
# window can outlive the client capture) and copies BOTH VMs' serials + B's
# runner stdout into artifacts/. vgate_repeat 3 = K=3.
#
# M206c (#3815): the store legs card b's LAN makes reachable. VM A also
# carries STORE/FLINT/GOSH + a publisher workspace: stage 3 (chained on
# 'vma-legs-done') runs JA.SH — flint init, `store publish` (the new
# --caps flag signing an install-time ask), `flint push` to a HOST flintd
# through run-a's own --net-tcp-respond relay (10.0.0.2:4853). VM B then
# proves the peer's channel is usable end to end: a later client waits
# on the 'VMA-FA-DONE' marker on A's serial (after the FLINTD phases and
# A's verify) and types `exec -u0
# GOSH.ELF -c "source /host/JB.SH"` — gosh's sequential children, run as
# the admin principal so `store consent` can land the row:
#   * `store install main --remote=10.0.0.2:4853 --channel=http://
#     10.0.0.16:8080/DROPBAD` — the tampered drop the setup's dropwatch
#     builds (same signed release.go, one ELF byte flipped): the clone
#     crosses B's own relay to the host flintd, the artifacts cross the
#     HUB from vm-a's GOHTTPD, and the sha gate refuses elf_mismatch by
#     name (the live-dev-loop pin).
#   * the same install against /DROP — verified, installed, `store:
#     grant propose` stands the signed ask — then `store consent` writes
#     the hash-bound row through slot 89 and HUBAPP.ELF prints its
#     marker.
# M214c (#3920): the direct guest-to-guest FLINT legs, staged so the
# existing journey survives. vm-a's stage 3 now runs a lifecycle script:
# a1/a2 + the signed drop and the host push (unchanged), then four
# bounded FLINTD.ELF phases on :5353 — vm-b genuinely discovers the
# advertised `_flint._tcp` instance after its burst, clones/pulls, and
# pushes signed history straight back to vm-a (no host daemon, no file
# copy). Each phase is gated by its own B client on an A-serial marker,
# so no sleep races the store. The refusals are the wire's own words:
# stale_head (vm-a advanced the same fork after B's push), author_not_allowed
# (a fresh fork pushed while vm-a's allowlist is vm-a only), auth_required
# (the same FLINT.ELF client run as uid_agent, where no flint-remote row
# binds, is challenged and refuses client-side). FLINTD exits on the same
# 30 s idle budget GOHTTPD has, so
# only one :5353 owner exists at a time; when the last phase is done the
# GOHTTPD restart loop retakes the port and the M206c install/consent/run
# lane proceeds unchanged. The host flintd stays the STORE remote: vm-a
# pushes `main` to it after every publish, so vm-b's local `main` is a
# prefix of the host's and the existing install fast-forwards.
#
# M217 (#3921): run b is the supervised boot the card names. vm-a boots
# the SHIPPED manifest (flintd disabled: `init: manifest ok n=1`, the
# seat is the only started row), then a marker-driven between-runs switch
# writes the SAME shipped manifest with the flintd row enabled — an
# ordinary reload, not a schema/restart-policy/admission change and not a
# test-only fault hook. INIT starts FLINTD.ELF, publishes
# `flint._flint._tcp.local`, and supervises it. vm-b (its launcher now
# arms the same `--cvc-file` share the harness gives vm-a, so OWNERS.TXT
# loads and the app-bound `flint-remote`/`agent-sign` rows bind — the
# uid_agent 2000 negative is preserved) discovers after the burst, clones,
# pulls, commits, pushes and verifies; vm-a advances the stream and
# verifies; then vm-a kills the supervisor-recorded pid during vm-b's
# two-pass push train, INIT restarts it (status=137 -> named backoff ->
# new pid), vm-b records the interrupted attempt, and the fault stream is
# either a verified complete suffix or an explicit `stream_torn` — never a
# silent repair. The daemon is persistent by default: the 40 s idle gap in
# vm-a's stage 2 exercises the accept timeout while the same pid serves
# vm-b's later pull, and only the staged run-a phases pass `--idle-exit`.
# Cold K=3 is three BOOTS=1 invocations (services.yaml gate_hub): vm-b's
# store outlives vgate_repeat's iterations, so the in-spec loop is not
# isolated enough for the card's K3 on its own.
#
# HOST PREREQS (the setup hook fails honestly when absent):
#   bash tools/go/build-gohttpd.sh -> .build/go/GOHTTPD.ELF
#   bash tools/go/build-netdiag.sh -> .build/go/GONETSTAT.ELF
#   bash tools/go/build-gofetch.sh -> .build/go/GOFETCH.ELF
#   bash tools/go/build-store.sh   -> .build/go/STORE.ELF
#   bash tools/go/build-flint.sh   -> .build/go/FLINT.ELF
#   bash tools/go/build-flintd.sh  -> .build/go/FLINTD.ELF
#   bash tools/go/build-gosh.sh    -> .build/go/GOSH.ELF
#   bash tools/go/build-init.sh    -> .build/go/INIT.ELF
#   bash tools/go/build-gotabwm.sh -> .build/go/GOTABWM.ELF
#   ../flint-server checkout (or FLINT_SERVER_DIR=...): hubd serves the
#   release stream on 127.0.0.1:4853 (this spec's own lane) — the M181
#   trust shape: stream history from the canonical remote, payload bytes
#   from the untrusted channel.

vgate_name live-net-hub "M206b #3814 + M206c #3815 + M214c #3920: two concurrent VMs on one --net-hub — ARP+ping both ways; vm-b browses vm-a's GOHTTPD _http._tcp over mDNS and fetches /healthz; vm-a advertises _flint._tcp and vm-b discovers, clones/pulls/pushes signed history directly guest-to-guest (literal stale_head/author_not_allowed/auth_required); then vm-a publishes+serves a signed release and vm-b installs it over the hub (tamper refuses elf_mismatch), consents, and runs it; M217 #3921: run b boots the shipped manifest, opts the FLINTD row in by reload, and INIT restarts a mid-sync kill on the two-desk journey; K=3"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 3 BOOTS
vgate_note "K=3 consecutive green runs is the M206b (#3814) acceptance record (BOOTS overrides the default 3)"
vgate_note "vm-a (run a): --mac 02:00:00:00:00:0a, hub CLIENT, dhcp-binds 10.0.0.16, serves GOHTTPD --advertise v206b on :8080"
vgate_note "vm-b (peer_watch-spawned runner, --console-tcp 127.0.0.1:24720): --mac 02:00:00:00:00:0b, hub OWNER, dhcp-binds 10.0.0.17, browses + fetches vm-a"
vgate_note "a->b arp/ping on vm-a serial; b->a arp/ping + mDNS browse + /healthz fetch on the client capture and vm-b serial"
vgate_note "run b: shipped manifest boots seat-only; a marker switch enables the flintd row by ordinary reload; the recorded supervised pid is killed mid-push and INIT restarts it"

# vm-a stage 1: bind the hub lease, then advertise. The bare `net` after the
# pump pins the --mac override + bound counter line on this VM's serial.
# `net arp 10.0.0.99` is the watchdog arm, NOT a resolution the gate needs:
# the rxq stall-reset (kernel virtio_net #3863) only fires while an ARP
# solicitation is outstanding (`owed`), and a permanently-unanswered target
# keeps a pending slot valid for the whole run — the learn window counts
# heartbeat ticks, of which a run sees a handful — so ANY later pair->ring
# wedge is recoverable instead of fatal. Observed live: a run where the
# attachment stalled at sil=pk=25654 with owed=0 and delivered nothing for
# the whole 720 s; with this arm the reset fires inside 512 silent polls.
# A learned target cannot arm it (`net arp` prints 'is at' and re-sends
# nothing), and .17 resolves mid-run — .99 never does, by construction.
vgate_file vma-s1.txt <<'EOF'
net dhcp
net dhcp
net dhcp
net dhcp
net dhcp
net dhcp
net dhcp
net dhcp
net dhcp
net dhcp
net arp 10.0.0.99
net
exec GOHTTPD.ELF --advertise v206b
echo vma-s1-done
EOF

# vm-a stage 2 — forwarded only after 'httpd: request /healthz' (a marker
# only vm-b's fetch can produce, so vm-b is provably bound): the A->B half
# of the shared-LAN proof. Same pacing rule as vmb-legs.txt: bare `net`
# drains between requests give the hub round trip time to land replies —
# the ARP reply names B's --mac in the table dump, and the icmp counters
# after the pings show both directions (req= A's echoes, repl= echoes A
# answered from B, pong= replies A got back). The `net arp`/`net ping
# 10.0.0.2` alternation paces the async learn race: a reply only learns
# inside a live pending window (arp.pending_wait_ticks, 2 ticks), and
# the reply lands whenever the rx drain pulls it — observed late by
# minutes under gate load. Each arp re-opens the window; each .2 ping
# (forged-responded instantly, no ICMP answer — the bounded wait is the
# pacer) buys real seconds so SOMETHING's window is open whenever the
# late reply arrives.
vgate_file vma-s2.txt <<'EOF'
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net arp
net
echo vma-legs-done
EOF

# vm-a stage 3 (M206c): the publish lane — forwarded only after
# 'vma-legs-done' so card b's legs serialize first. JA.SH does the real
# work inside gosh (sequential children, like every J-script in
# live-dev-loop): init the publisher workspace, `store publish` drops
# release.go + HUBAPP.ELF under /host/DROP/main/ (which GOHTTPD already
# serves), then `flint push` ships the signed stream to the host flintd
# through run-a's own --net-tcp-respond relay. 'JA-DONE' is the receipt
# the script3 stage ends on; the client-2 --after marker is the earlier
# 'flint: push ' line, which proves the stream is on flintd AND the drop
# is served.
#
# The .17 verify legs live HERE, not in stage 2: a solicited ARP reply
# rides the same pair->ring delivery lag everything else does, and on
# the live gate it lands ~a minute after stage 2's paced loop ends —
# observed twice: entries=0 at the s2 dump, learn=2 by the first s3
# counter. The pending slot never lapses inside a run (the learn window
# counts heartbeat ticks, of which a run sees a handful), so the reply
# ALWAYS learns eventually — the only question is whether a `net ping`
# + `net arp` still run afterwards. Stage 2 stays the solicitation
# train that keeps re-asking; this block paces on .2 (forged-instant,
# the bounded-wait pacer) while re-trying .17, so the first reply that
# lands is followed by more .17 pings, the arp dump that pins the entry,
# and a late `net` for the icmp counters — all before JA.SH starts.
#
# Two passes, not one, for the cold-boot case: observed 2026-11-06, a
# first-iteration run ended with vm-a `icmp=req=10,repl=42,pong=0` and no
# .17 entry — the reply had not landed inside one pass of pacer waits,
# while iterations 2 and 3 (B already warm) resolved early. The original
# single pass is ~11 pacer waits, and the lag documented above is "~a
# minute". The second pass repeats the same solicitation train with the
# `net` drains, which is what actually pulls the late reply off the ring.
vgate_file vma-s3.txt <<'EOF'
net arp 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net
net arp 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net ping 10.0.0.2
net ping 10.0.0.17
net
net arp
net
exec GOSH.ELF -c "source /host/JA.SH"
EOF

# The payload app vm-a publishes and vm-b installs + runs — GENERATED in
# the setup hook (below) as a minimal statically-linked AArch64 ELF the
# same shape tools/mkhello-elf.py emits (one PT_LOAD, direct syscalls —
# write(1, "hubapp: m206c-ok") + exit(0)). Deliberately TINY, and the
# size is load-bearing, not cosmetic: the store channel bounds one
# request/response exchange to a 5 s absolute recv deadline
# (user/go/store/channel.go httpTimeoutNs), and the guest TCP stack is
# stop-and-wait — one 1460-byte segment in flight per connection. A
# ~1.1 MiB Go ELF is ~780 segments; over the hub's measured per-segment
# latency under gate load that misses the bound EVERY time and installs
# refuse 'store: refuse channel_malformed body' instead of proving the
# card's elf_mismatch pin. ~150 bytes is ONE segment — the fetch lands
# whole inside any reachable window, so the tamper refusal names the
# sha gate, not the transport. The marker is still the whole contract
# (consent row, elf_mismatch, APPS.TXT/STORE.TXT all host-pinned).

# vm-b's leg file — typed over --console-tcp by the run-a client. The file
# is GENERATED in the setup hook (below) because its shape is load-bearing
# in two ways and needs loops:
#   * the browse runs FIRST: GOHTTPD's advertiser is announce-only (3
#     burst emits ~1 s apart then one per TTL/2=60 s), so the discover's
#     ~6 s window must open inside the burst — the client fires on A's
#     first 'dhcp bound ip=10.0.0.16', ~2-4 s before the burst.
#   * the ARP+ping legs REPEAT with drains between: on the first
#     iteration B's inbound pipeline warms up slowly (observed: hub
#     selfPort writes land in B's socketpair immediately but VZ-to-ring
#     delivery trickles for ~30-60 s; the ARP learn window is only ~0.5 s
#     per request), so arp/ping attempts cycle until one lands — once
#     resolved the table entry carries the ping and fetch legs. The bare
#     `net`/`net recv` lines are RX drains that give each request's reply
#     time to arrive.

# Setup: stage both shares, then boot VM B in the background — the
# first-runner-binds half of the card. B's runner owns the segment hub +
# the hub-side DHCP responder for the whole gate; it must be provably at
# the monitor prompt before run a is allowed to race it.
vgate_setup_python <<'PY'
import os, shutil, struct, subprocess, sys, time
rd = os.environ["RUN_DIR"]
repo = os.getcwd()
share = os.path.join(rd, "share")       # the armed gate share (vm-a)
bshare = os.path.join(rd, "share-vmb")  # vm-b's virtio-fs share

# vm-a's share: GOHTTPD.ELF beside the seeded app bundle.
src = os.path.join(".build", "go", "GOHTTPD.ELF")
if not os.path.isfile(src):
    sys.exit("GOHTTPD.ELF missing (expected " + src + ") - build it first: "
             "bash tools/go/build-gohttpd.sh")
shutil.copy(src, os.path.join(share, "GOHTTPD.ELF"))

# M217 (#3921) — the supervised boot's own binaries: INIT (the ordinary
# supervisor) and the GO seat its manifest starts. The manifest itself
# is staged by the between-runs step below (the SHIPPED one first, so
# the default boot proves the flintd row disabled; the opt-in copy is
# switched in only after the run-a evidence is in).
for name, how in (("INIT.ELF", "build-init.sh"),
                  ("GOTABWM.ELF", "build-gotabwm.sh")):
    src = os.path.join(".build", "go", name)
    if not os.path.isfile(src):
        sys.exit(name + " missing (expected " + src + ") - build it first: "
                 "bash tools/go/" + how)
    shutil.copy(src, os.path.join(share, name))

# M206c — vm-a's publish lane: the store/shell ELFs, the payload app
# (built here from this spec's vgate_file'd hubapp.go), an empty FLINT
# workspace, the journey script, and the signer credentials (agent-sign
# is the same RFC 8032 TEST 1 seed live-dev-loop pins as GUEST_KEY; the
# flint-remote key matches the setup's hubd --auth-key). wm=none: the
# gosh->store->flint chain is the same task-pool shape live-dev-loop
# measured, so A's seat stays off too.
AUTH_KEY_HEX = "ab" * 32
GUEST_KEY = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
# M214c (#3920): vm-b's own signer is RFC 8032 TEST 2. It can author its
# own Changes (that is what a direct guest-to-guest push means) while
# still being unable to forge vm-a's release: vm-a's TRUSTED.TXT pins
# both keys, the wire secret stays vm-a's, and each side's verify still
# walks the raw publisher signature.
BGUEST_SEED = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
BGUEST_KEY = "3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c"
for name, how in (("STORE.ELF", "build-store.sh"),
                  ("FLINT.ELF", "build-flint.sh"),
                  ("FLINTD.ELF", "build-flintd.sh"),
                  ("GOSH.ELF", "build-gosh.sh")):
    src = os.path.join(".build", "go", name)
    if not os.path.isfile(src):
        sys.exit(name + " missing (expected " + src + ") - build it first: "
                 "bash tools/go/" + how)
    shutil.copy(src, os.path.join(share, name))
# HUBAPP.ELF — a one-segment payload (see the sizing note above): the
# tools/mkhello-elf.py image shape, a 52-byte ELF32 header + one 32-byte
# PT_LOAD phdr + eight instructions + the marker, write(1)+exit(0) on
# the kernel's direct-syscall contract (elf.zig text_base 0x400000).
def movz(rd, imm16):
    return 0xD2800000 | (imm16 << 5) | rd

def adr(rd, rel):
    return 0x10000000 | ((rel & 0x3) << 29) | (((rel >> 2) & 0x7FFFF) << 5) | rd

MSG = b"hubapp: m206c-ok\n"
code = b"".join(struct.pack("<I", w) for w in [
    movz(8, 1),            # x8 = 1 (sys_write)
    movz(0, 1),            # x0 = fd 1
    adr(1, 8 * 4 - 8),     # x1 = &msg (follows the 8-instruction block)
    movz(2, len(MSG)),     # x2 = len
    0xD4000001,            # svc #0
    movz(0, 0),            # x0 = exit status 0
    movz(8, 3),            # x8 = 3 (sys_exit)
    0xD4000001,            # svc #0
]) + MSG
ehsize, phentsize, phnum = 52, 32, 1
code_off = ehsize + phnum * phentsize
ehdr = struct.pack(
    "<4s5B7xHHIIIIIHHHHHH",
    b"\x7fELF", 1, 1, 1, 0, 0,
    2, 0xB7, 1,            # ET_EXEC, EM_AARCH64, version
    0x400000, ehsize, 0, 0,
    ehsize, phentsize, phnum, 0, 0, 0)
phdr = struct.pack(
    "<8I",
    1,                     # PT_LOAD
    code_off, 0x400000, 0x400000,
    len(code), len(code),
    5,                     # PF_R | PF_X (W^X)
    0x1000)
open(os.path.join(share, "HUBAPP.ELF"), "wb").write(ehdr + phdr + code)
sig_dir = os.path.join("user", "go", "flint", "testdata", "sig")
SEED_HEX = open(os.path.join(sig_dir, "seed.hex")).read().strip()
open(os.path.join(share, "SECRETS.TXT"), "w").write(
    "#v1\n"
    "agent-sign\t1000\t" + SEED_HEX + "\tFLINT.ELF\n"
    "flint-remote\t1000\t" + AUTH_KEY_HEX + "\tFLINT.ELF\n")
open(os.path.join(share, "OWNERS.TXT"), "w").write(
    "#v1\nFLINT.ELF\t644\t0\t-\n")
open(os.path.join(share, "SETTINGS.TXT"), "w").write("#v2\nwm=none\n")
os.makedirs(os.path.join(share, "WS"), exist_ok=True)
open(os.path.join(share, "WS", "NOTE.TXT"), "w").write("m206c publisher workspace\n")
# M214c (#3920) — the source atoms the sync history carries. a1/a2 are
# ordinary workspace files; WS2/x.go is the change vm-a authors on the
# fork stream vm-b pushes (the stale_head leg). One Go decl per file: the
# FLINT parser keys Atoms by (file, kind, name).
for rel, body in (
        ("WS/a1.go", "package demo\n\nfunc A1() string { return \"a1\" }\n"),
        ("WS/a2.go", "package demo\n\nfunc A2() string { return \"a2\" }\n"),
        ("WS2/x.go", "package demo\n\nfunc LineA() string { return \"line-a\" }\n"),
        # M217 (#3921): the supervised journey's own atoms. WSV opens the
        # stream vm-b clones; WSVA is the advance it pulls while the
        # daemon is live; WSF opens the stream the kill interrupts.
        ("WSV/x.go", "package demo\n\nfunc SupA() string { return \"sup-a\" }\n"),
        ("WSV/x2.go", "package demo\n\nfunc SupA2() string { return \"sup-a2\" }\n"),
        ("WSF/f.go", "package demo\n\nfunc FaultA() string { return \"fault-a\" }\n")):
    p = os.path.join(share, rel)
    os.makedirs(os.path.dirname(p), exist_ok=True)
    open(p, "w").write(body)
# The FLINT store trust anchor + wire secrets and author allowlists.
os.makedirs(os.path.join(share, "FLINT"), exist_ok=True)
open(os.path.join(share, "FLINT", "TRUSTED.TXT"), "w").write(
    "#v1\nkey=" + GUEST_KEY + " name=hub-a\nkey=" + BGUEST_KEY + " name=hub-b\n")
open(os.path.join(share, "AUTH.key"), "w").write(AUTH_KEY_HEX + "\n")
open(os.path.join(share, "ALLOW-AB.TXT"), "w").write(
    GUEST_KEY + "\n" + BGUEST_KEY + "\n")
open(os.path.join(share, "ALLOW-A.TXT"), "w").write(GUEST_KEY + "\n")
# Per-iteration token. The shares (and vm-b's store) outlive vgate_repeat's
# iterations, but fork/workspace names must be fresh each boot. Both
# guests start at "a" and rotate once per iteration, so the names below
# cannot collide with the previous iteration's streams. `main` itself is
# never rewritten after the first publish: it stays byte-identical to the
# host flintd's copy, which is what lets the existing install lane
# fast-forward instead of colliding on a persistent local stream.
open(os.path.join(share, "SEQ"), "w").write("a\n")
# FA.SH: the M214c staged lifecycle. One GOSH, sequential children (the
# live-dev-loop-proven shape): a1/a2 + the signed drop and host push on
# stream `main`, then FOUR bounded FLINTD phases. A daemon started by
# INIT is a long-lived service: its accept loop treats the bounded
# AcceptConn idle timeout as a routine interval and keeps listening (the
# staged phases below therefore pass the optional `--idle-exit` so a phase
# still ends deterministically after vm-b's last connection and the next
# command runs — one :5353 owner at a time, no second responder, no added
# admission). Phase 1 gets 120 s: its window must outlive vm-b's discovery
# train plus the clone before the first session resets it, and a 30 s
# window is measurably too tight under gate load (observed
# `flint: refuse stream_missing la` when phase 1 ended before F1's push).
# Phases 2-4 only wait for a push that starts within ~15 s, so 60 s is
# ample. Each daemon is started as a background job and waited with `fg`:
# the shell's foreground wait has a 120 s budget, so a longer phase would
# return `gosh: wait failed` and leave the daemon owning :4748 for every
# later phase (observed `flintd: listen failed port=4748` cascades), while
# the job table reaps by polling and has no such budget. The shipped row
# does NOT pass the flag: the supervised daemon is persistent by default.
#   phase 1  auth + allowlist A,B — vm-b discovers/clones/pulls and pushes
#            its `l<N>` fork (accepted: the signed change lands in A).
#   after 1  A advances `l<N>` with its own signed Change.
#   phase 2  auth + allowlist A,B — vm-b's stale push on `l<N>` refuses
#            stale_head, then its `e<N>` push is accepted.
#   phase 3  auth + allowlist A only — vm-b's `d<N>` push refuses
#            author_not_allowed.
#   phase 4  auth + allowlist A,B — the same client as uid_agent (no
#            flint-remote row binds) refuses auth_required.
# Only then does the GOHTTPD restart loop take :5353 for the store lane.
# JA-DONE stays the first-phase receipt the older asserts name.
open(os.path.join(share, "JA.SH"), "w").write(
    "set N=$(cat /host/SEQ)\n"
    "if [ \"$N\" = \"a\" ]; then echo b > /host/SEQ; fi\n"
    "if [ \"$N\" = \"b\" ]; then echo c > /host/SEQ; fi\n"
    "if [ \"$N\" = \"c\" ]; then echo d > /host/SEQ; fi\n"
    "FLINT.ELF init --name=hubws --src=/host/WS || echo A-INIT-EXISTS\n"
    "FLINT.ELF add /host/WS/a1.go || true\n"
    "FLINT.ELF commit --ws=hubws -m \"a1-$N\" || echo A-A1-UP-TO-DATE\n"
    "FLINT.ELF add /host/WS/a2.go || true\n"
    "FLINT.ELF commit --ws=hubws -m \"a2-$N\" || echo A-A2-UP-TO-DATE\n"
    "STORE.ELF publish HUBAPP /host/HUBAPP.ELF --src=/host/WS --version=1.0.0 --stream=main --caps=file,memory,process,debug,net || sleep 1 ; STORE.ELF publish HUBAPP /host/HUBAPP.ELF --src=/host/WS --version=1.0.0 --stream=main --caps=file,memory,process,debug,net\n"
    "FLINT.ELF push --remote=10.0.0.2:4853 --stream=main || sleep 1 ; FLINT.ELF push --remote=10.0.0.2:4853 --stream=main\n"
    "echo JA-DONE\n"
    "echo VMA-FLINTD1\n"
    "FLINTD.ELF --advertise hubflint --idle-exit=30 --auth-key=/host/AUTH.key --allow-author=/host/ALLOW-AB.TXT\n"
    "echo VMA-FLINTD1-DONE\n"
    "FLINT.ELF init --name=lws$N --src=/host/WS2 --stream=l$N || echo A-LWS-EXISTS\n"
    "FLINT.ELF add /host/WS2/x.go || true\n"
    "FLINT.ELF commit --ws=lws$N -m \"line-$N\" || echo A-LINE-UP-TO-DATE\n"
    "echo VMA-FLINTD2\n"
    "sleep 2\n"
    "FLINTD.ELF --advertise hubflint --idle-exit=30 --auth-key=/host/AUTH.key --allow-author=/host/ALLOW-AB.TXT\n"
    "echo VMA-FLINTD2-DONE\n"
    "echo VMA-FLINTD3\n"
    "sleep 2\n"
    "FLINTD.ELF --advertise hubflint --idle-exit=30 --auth-key=/host/AUTH.key --allow-author=/host/ALLOW-A.TXT\n"
    "echo VMA-FLINTD3-DONE\n"
    "echo VMA-FLINTD4\n"
    "sleep 2\n"
    "FLINTD.ELF --advertise hubflint --idle-exit=30 --auth-key=/host/AUTH.key --allow-author=/host/ALLOW-AB.TXT\n"
    "echo VMA-FLINTD4-DONE\n"
    "FLINT.ELF verify --all\n"
    "echo VMA-FA-DONE\n"
    "while true; do GOHTTPD.ELF ; done\n")

# M217 (#3921) — vm-a's supervised journey scripts, sourced by the run-b
# monitor stages. Nothing here edits INIT's schema, restart policy or
# admission: the daemon is started by the SHIPPED manifest row once the
# between-runs switch enables it, and the supervisor's own receipts
# (`svc: start`, `svc: exit status=137`, `svc: backoff`, the next start)
# are the lifecycle evidence. `CURA` pins the iteration token once, so an
# INIT reload or a later stage cannot re-read a rotated `/host/SEQB`.
open(os.path.join(share, "SEQB"), "w").write("a\n")
# Stage 1: open the stream vm-b clones, then print the opt-in marker. The
# marker is the LAST line so the stream exists before any client can be
# gated on the daemon's post-burst boundary.
open(os.path.join(share, "SUPSTART.SH"), "w").write(
    "set N=$(cat /host/SEQB)\n"
    "echo $N > /host/CURA\n"
    "FLINT.ELF fork main sv$N || echo A-SUP-FORK-EXISTS\n"
    "FLINT.ELF init --name=wsv$N --src=/host/WSV --stream=sv$N || echo A-SUP-INIT-EXISTS\n"
    "FLINT.ELF add /host/WSV/x.go || true\n"
    "FLINT.ELF commit --ws=wsv$N -m \"a-sup-$N\" || echo A-SUP-UP-TO-DATE\n"
    "echo VMA-SUP-STREAM-READY\n"
    "echo VMA-SUP-OPTOUT\n")
# Stage 2 (after the supervisor restarted the daemon): advance the cloned
# stream while it is live, open the stream the kill interrupts, pull
# `main` and verify every signature the store holds — the same
# discover/clone/commit/push/pull/verify journey the M214c phases carry,
# now through a supervised daemon. The 40 s pause is a DELIBERATE idle
# window, not readiness: the daemon's bounded accept wait always expires
# at 30 s, and vm-b's next pull (gated on VMA-SUP-ADVANCED, a marker)
# must be served by the SAME supervised pid — the proof that the timeout
# is a routine interval and not a reason to exit.
open(os.path.join(share, "SUPADV.SH"), "w").write(
    "set N=$(cat /host/CURA)\n"
    "FLINT.ELF init --name=wsv2$N --src=/host/WSV --stream=sv$N || echo A-SUPA-INIT-EXISTS\n"
    "FLINT.ELF add /host/WSV/x2.go || true\n"
    "FLINT.ELF commit --ws=wsv2$N -m \"a-sup-advance-$N\" || echo A-SUPA-UP-TO-DATE\n"
    "FLINT.ELF fork main fv$N || echo A-SUPF-FORK-EXISTS\n"
    "FLINT.ELF init --name=wsvf$N --src=/host/WSF --stream=fv$N || echo A-SUPF-INIT-EXISTS\n"
    "FLINT.ELF add /host/WSF/f.go || true\n"
    "FLINT.ELF commit --ws=wsvf$N -m \"a-fault-$N\" || echo A-SUPF-UP-TO-DATE\n"
    "sleep 40\n"
    "FLINT.ELF pull --stream=main --remote=10.0.0.2:4853 || sleep 1 ; FLINT.ELF pull --stream=main --remote=10.0.0.2:4853\n"
    "FLINT.ELF verify --all\n"
    "echo VMA-SUP-ADVANCED\n"
    "echo VMA-SUP-JOURNEY-DONE\n"
    "echo VMA-SUP-FAULT-READY\n"
    'exec GOSH.ELF -c "source /host/SUPKILL.SH"\n')
# The killer runs on the supervised guest: it signals the pid INIT
# recorded for the daemon (`svc: start name=flintd pid=`, captured by the
# marker-driven switch into /host/SUPPID). 70 s is measured from
# VMA-SUP-FAULT-READY, which is before sup2's pull+verify and sup3's
# clone/fork/init, so it lands inside sup3's ~100 s push train on every
# observed run instead of racing the train's start.
open(os.path.join(share, "SUPKILL.SH"), "w").write(
    "sleep 40\n"
    "sleep 30\n"
    "set P=$(cat /host/SUPPID)\n"
    "echo VMA-SUP-KILL-PID=$P\n"
    "kill $P\n"
    "echo VMA-SUP-KILLED\n")
# Stage 2's tail is the fault hand-off. It prints VMA-SUP-FAULT-READY
# ("advance and pull are done") and then launches SUPKILL.SH with `exec`,
# which returns at once: the killer runs as its own task in THIS guest —
# the guest whose INIT recorded the daemon pid — so its `kill` reaches the
# supervised process, and the two desks plus the killing desk all overlap
# without racing for the console bridge. The killer's lead is sized
# against the fault leg's sequence (sup2's pull+verify, then sup3's clone,
# fork and init) so it lands mid-train; because every attempt carries a
# real Change and only 1 s separates them, a lead that lands between two
# pushes still meets the daemon inside its 2 s on-failure backoff window.
# A push that succeeds before the kill is real progress; one that meets
# the down window prints VMB-SUP-INTERRUPTED, and the leg then reports its
# real recovery outcome — VMB-SUP-RECOVERED only when the restarted daemon
# served the push again, otherwise VMB-SUP-RETRY-FAILED with the store
# left as an unrepaired verifiable prefix. It never claims a success it
# did not get.
# vm-b's share: the browse + fetch verbs; wm=none keeps the seat's EL0
# slots for the discover/fetch children.
os.makedirs(bshare, exist_ok=True)
for name, how in (("GONETSTAT.ELF", "build-netdiag.sh"),
                  ("GOFETCH.ELF", "build-gofetch.sh")):
    src = os.path.join(".build", "go", name)
    if not os.path.isfile(src):
        sys.exit(name + " missing (expected " + src + ") - build it first: "
                 "bash tools/go/" + how)
    shutil.copy(src, os.path.join(bshare, name))
open(os.path.join(bshare, "SETTINGS.TXT"), "w").write("#v2\nwm=none\n")
# The browse leg lives in a script, not one -c chain: gosh caps a chain at
# four commands. One line per bounded step keeps each chain inside the cap
# and still orders discover -> fetch -> marker. A retry around discover is
# NOT safe here: each bounded discover window is seconds long, and vm-a's
# HTTPD exits 30 s after it starts accepting, so a second discover pushed
# the fetch past that window (observed: `fetch: connect failed EINVAL`
# after the HTTPD's `bye`). The committed single-discover timing is the
# one inside the window.
open(os.path.join(bshare, "BROWSE.SH"), "w").write(
    "GONETSTAT.ELF discover _http._tcp\n"
    "GOFETCH.ELF http://10.0.0.16:8080/healthz\n"
    "echo vmb-browse-fetch-done\n")

# M206c — vm-b's consumer side: STORE/FLINT/GOSH for the install chain,
# OWNERS.TXT naming the two system images, a SECRETS.TXT carrying
# flint-remote plus vm-b's own agent-sign seed at uid_user (uid-scoped:
# see the note at the write below for why app-binding cannot bind on this
# launcher), a keyring pinning only vm-a's public key, an enforce-mode
# GRANTS.TXT the consent row lands in, and JB.SH — the install script the
# -u0 gosh sources:
# tampered drop refuses elf_mismatch first (bare line, pinned by count),
# the honest drop verifies + installs + stands the signed ask, consent
# writes the row, the app runs.
for name in ("STORE.ELF", "FLINT.ELF", "GOSH.ELF"):
    shutil.copy(os.path.join(".build", "go", name), os.path.join(bshare, name))
# M214c (#3920): vm-b authors its own signed Changes now, so it carries
# its OWN RFC 8032 TEST 2 agent-sign seed (never vm-a's), still bound to
# FLINT.ELF only. The auth phase runs the SAME binary under uid_agent
# (2000), where no `flint-remote` row binds: it reads no wire secret and
# the client refuses auth_required when the daemon challenges. A renamed
# copy of FLINT.ELF (FLINTU) cannot stand in for that — an unknown image
# name lands on the ambient floor, which denies `file` at exec, so the
# client never runs (observed: `refused:file_denied name=FLINTU.ELF`).
open(os.path.join(bshare, "OWNERS.TXT"), "w").write(
    "#v1\nFLINT.ELF\t644\t0\t-\nSTORE.ELF\t644\t0\t-\n")
# M214c (#3920) review fix (#3921): vm-b's rows are app-bound to
# FLINT.ELF, exactly like vm-a's, and vm-b's launcher now arms the
# queue-5 virtio-fs probe (`--cvc-file bshare`, above) so OWNERS.TXT is
# loaded and `trust.system_owned_key` can see the uid_system row an
# app-bound secret needs. The previous UID-scoped shape was a
# consequence of the missing probe, not a deliberate trust decision —
# with OWNERS loaded the app-bound row, not the uid, is the binding this
# boot enforces. The uid_agent (2000) leg still reads no secret and
# refuses auth_required: that negative is preserved unchanged.
open(os.path.join(bshare, "SECRETS.TXT"), "w").write(
    "#v1\nflint-remote\t1000\t" + AUTH_KEY_HEX + "\tFLINT.ELF\n"
    "agent-sign\t1000\t" + BGUEST_SEED + "\tFLINT.ELF\n")
# vm-b's source atoms for the streams it forks/pushes. One file per
# phase so an add stages exactly the intended Change.
for rel, body in (
        ("BWSL/a.go", "package demo\n\nfunc LineB1() string { return \"line-b1\" }\n"),
        ("BWSL/b.go", "package demo\n\nfunc LineB2() string { return \"line-b2\" }\n"),
        ("BWSE/a.go", "package demo\n\nfunc EdgeB() string { return \"edge-b\" }\n"),
        ("BWSD/a.go", "package demo\n\nfunc DenyB() string { return \"deny-b\" }\n")):
    p = os.path.join(bshare, rel)
    os.makedirs(os.path.dirname(p), exist_ok=True)
    open(p, "w").write(body)
# M217 (#3921): vm-b's fault-leg atoms — eight distinct files, one
# add/commit/push each, so every pre-kill attempt carries a REAL Change
# and the interruption always has accepted history to protect.
os.makedirs(os.path.join(bshare, "BWSF"), exist_ok=True)
for _i in range(1, 9):
    open(os.path.join(bshare, "BWSF", "f%d.go" % _i), "w").write(
        "package demo\n\nfunc FaultB%d() string { return \"fault-b%d\" }\n"
        % (_i, _i))
# The alternate corpus: same paths, different bytes. The fault leg adds one
# pass of each corpus in turn, so every attempt carries a real Change
# (`new` on the first pass, `modified` after) instead of an up-to-date
# no-op — a train of no-op pushes has nothing in flight for the kill to
# split, which is how an honest fault leg degrades into a vacuous pass.
os.makedirs(os.path.join(bshare, "FWS"), exist_ok=True)
for _i in range(1, 9):
    open(os.path.join(bshare, "FWS", "f%d.go" % _i), "w").write(
        "package demo\n\nfunc FaultW%d() string { return \"fault-w%d\" }\n"
        % (_i, _i))
open(os.path.join(bshare, "SEQB"), "w").write("a\n")
open(os.path.join(bshare, "SEQ"), "w").write("a\n")
os.makedirs(os.path.join(bshare, "FLINT"), exist_ok=True)
open(os.path.join(bshare, "FLINT", "TRUSTED.TXT"), "w").write(
    "#v1\nkey=" + GUEST_KEY + " name=storelan\n"
    "key=" + BGUEST_KEY + " name=m214c-b\n")
open(os.path.join(bshare, "GRANTS.TXT"), "w").write("#v1 mode=enforce\n")
# The tampered leg is a BARE line: its expected outcome IS the refusal, so
# the `X || sleep 1 ; X` retry idiom (correct on every happy-path verb
# below) would re-run it after every guaranteed failure. The judge pins
# `store: refuse elf_mismatch HUBAPP.ELF` at exactly one per JB2-DONE
# completion, and the retry made that structurally unreachable — observed
# 2 refusals for 1 pass on every iteration before this line was bare.
open(os.path.join(bshare, "JB.SH"), "w").write(
    "STORE.ELF install main --remote=10.0.0.2:4853 --channel=http://10.0.0.16:8080/DROPBAD\n"
    "STORE.ELF install main --remote=10.0.0.2:4853 --channel=http://10.0.0.16:8080/DROP || sleep 1 ; STORE.ELF install main --remote=10.0.0.2:4853 --channel=http://10.0.0.16:8080/DROP\n"
    "echo JB-DONE\n")
# JB2.SH is the ADMIN half — the -u0 gosh the third client types:
# consent needs CAP_PROC_ADMIN, which only the monitor's -u0 spawn
# carries. The app then runs inside the same source'd script (the
# install->consent->run ordering stays sequential inside one gosh each).
open(os.path.join(bshare, "JB2.SH"), "w").write(
    "STORE.ELF consent HUBAPP || STORE.ELF consent HUBAPP\n"
    "HUBAPP.ELF || sleep 1 ; HUBAPP.ELF\n"
    "echo JB2-DONE\n")

# M214c (#3920) — vm-b's direct-sync legs, one bounded script per FLINTD
# phase. Each is sourced by a monitor `GOSH.ELF -c` (foreground, so the
# console bridge stays busy until the script's marker; the next phase's
# client queues behind it with --retry-busy). N is the per-iteration
# token both shares rotate; stream names `l$N`/`e$N`/`d$N` are therefore
# fresh even though vm-b's store survives vgate_repeat's iterations.
#
# F1: discover AFTER vm-a's announce burst (the client is gated on
# 'flintd: advertise burst done'), then clone/pull `main` and push the
# `l<N>` fork the stale leg advances later. `flint log` is the raw-history
# receipt on B; verify --all re-walks every signature against TRUSTED.
open(os.path.join(bshare, "F1.SH"), "w").write(
    "set N=$(cat /host/SEQ)\n"
    "echo $N > /host/CUR\n"
    "if [ \"$N\" = \"a\" ]; then echo b > /host/SEQ; fi\n"
    "if [ \"$N\" = \"b\" ]; then echo c > /host/SEQ; fi\n"
    "if [ \"$N\" = \"c\" ]; then echo d > /host/SEQ; fi\n"
    "FLINT.ELF clone 10.0.0.16:4748 main || sleep 1 ; FLINT.ELF clone 10.0.0.16:4748 main\n"
    "FLINT.ELF pull --stream=main || sleep 1 ; FLINT.ELF pull --stream=main\n"
    "FLINT.ELF log --stream=main\n"
    "FLINT.ELF fork main l$N\n"
    "FLINT.ELF init --name=bl$N --src=/host/BWSL --stream=l$N\n"
    "FLINT.ELF add /host/BWSL/a.go\n"
    "FLINT.ELF commit --ws=bl$N -m \"b-line-1-$N\"\n"
    "FLINT.ELF push --remote=10.0.0.16:4748 --stream=l$N || sleep 1 ; FLINT.ELF push --remote=10.0.0.16:4748 --stream=l$N\n"
    "FLINT.ELF verify --all\n"
    "echo VMB-F1-DONE\n")
# F2: the stale push (vm-a advanced l<N> after phase 1), the pull that
# catches main up, and the accepted `e<N>` push. The local l<N> log keeps
# the refused sibling; that is the proof of a real head check, not a
# client-side skip.
open(os.path.join(bshare, "F2.SH"), "w").write(
    "set N=$(cat /host/CUR)\n"
    "GONETSTAT.ELF discover _flint._tcp\n"
    "sleep 5\n"
    "FLINT.ELF add /host/BWSL/b.go || true\n"
    "FLINT.ELF commit --ws=bl$N -m \"b-line-2-$N\" || true\n"
    "FLINT.ELF push --remote=10.0.0.16:4748 --stream=l$N\n"
    "FLINT.ELF pull --stream=main || sleep 1 ; FLINT.ELF pull --stream=main\n"
    "FLINT.ELF fork main e$N\n"
    "FLINT.ELF init --name=be$N --src=/host/BWSE --stream=e$N\n"
    "FLINT.ELF add /host/BWSE/a.go\n"
    "FLINT.ELF commit --ws=be$N -m \"b-edge-$N\"\n"
    "FLINT.ELF push --remote=10.0.0.16:4748 --stream=e$N || sleep 1 ; FLINT.ELF push --remote=10.0.0.16:4748 --stream=e$N\n"
    "FLINT.ELF ls-remote 10.0.0.16:4748\n"
    "echo VMB-F2-DONE\n")
# F3: main catches up to 1.<N>.2, then a fresh fork `d<N>` carries a
# signed Change to a daemon whose allowlist is vm-a only.
open(os.path.join(bshare, "F3.SH"), "w").write(
    "set N=$(cat /host/CUR)\n"
    "sleep 5\n"
    "FLINT.ELF pull --stream=main || sleep 1 ; FLINT.ELF pull --stream=main\n"
    "FLINT.ELF fork main d$N\n"
    "FLINT.ELF init --name=bd$N --src=/host/BWSD --stream=d$N\n"
    "FLINT.ELF add /host/BWSD/a.go\n"
    "FLINT.ELF commit --ws=bd$N -m \"b-denied-$N\"\n"
    "FLINT.ELF push --remote=10.0.0.16:4748 --stream=d$N\n"
    "echo VMB-F3-DONE\n")
# F4: the same push as uid_agent, where no flint-remote row binds. The
# client reads no wire secret, the daemon answers hello with a nonce, and
# the client refuses auth_required itself — the literal line the card
# asks for. Bare, not the retry idiom: the refusal IS the expected
# outcome, and a retry would print it twice.
open(os.path.join(bshare, "F4.SH"), "w").write(
    "set N=$(cat /host/CUR)\n"
    "sleep 5\n"
    "FLINT.ELF push --remote=10.0.0.16:4748 --stream=d$N\n"
    "FLINT.ELF verify --all\n"
    "FLINT.ELF ls-remote 10.0.0.16:4748\n"
    "echo VMB-F4-DONE\n")

# A private runner copy for vm-b — the same #3689 defense the harness gives
# its own boots: a concurrent build + re-sign must not move the shared
# binary out from under an in-flight VM.
runner = os.path.join("host", "vm-runner", ".build", "release", "VMRunner")
if not os.path.isfile(runner):
    sys.exit("VMRunner missing (expected " + runner + ") - the build preamble should have produced it")
bina = os.path.join(rd, "VMRunner-vmb")
shutil.copy(runner, bina)
os.chmod(bina, 0o755)

# vm-b's own EFI vars: point --vars at a path that does not exist so the
# runner mints a fresh VZEFIVariableStore (a 0-byte file is an unopenable
# store — VZErrorDomain 2 "Could not open variableStore", observed).

# vm-b boots NOW: binding the hub path first makes this runner the segment
# owner. peer_watch scopes it to this gate invocation (the setup hook's
# parent IS the gate process); --timeout 0 holds it until that kill.
watch = os.path.join("user", "go", "browser", "peer_watch.py")
ser_b = os.path.join(rd, "vm-serial-vmb.log")
log = open(os.path.join(rd, "vmb-runner.log"), "wb")
proc = subprocess.Popen(
    [sys.executable, watch, str(os.getppid()),
     bina,
     "--overlay-base", os.path.join(repo, "artifacts", "disk.img"),
     "--vars", os.path.join(rd, "efi-vars-vmb.bin"),
     "--serial", ser_b,
     "--virtio-fs", bshare,
     # M217 (#3921): arm the queue-5 probe on vm-b's OWN share too. This
     # is the launcher configuration vm-a has always had (the harness
     # appends --cvc-file for its run); without it OWNERS.TXT never
     # loads and no app-bound secret can bind on vm-b.
     "--cvc-file", bshare,
     "--net", os.path.join(rd, "vmb.cap"),
     "--net-hub", os.path.join(rd, "nethub.sock"),
     "--mac", "02:00:00:00:00:0b",
     "--net-arp-respond", "10.0.0.2",
     "--net-tcp-respond", "10.0.0.2:4853:relay",
     "--net-tcp-respond-relay", "127.0.0.1:4853",
     "--console-tcp", "127.0.0.1:24720",
     "--timeout", "0"],
    stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
log.close()
open(os.path.join(rd, "vmb.pid"), "w").write(str(proc.pid) + "\n")

# Honest bring-up bound: run a must not start while vm-b's boot state is
# unknown — the prompt line proves the guest reached the monitor (the
# client's typed legs need it), the owner line proves THIS runner hosts
# the segment switch + its DHCP responder.
deadline = time.time() + 180
ok = False
while time.time() < deadline:
    if proc.poll() is not None:
        break
    try:
        ser = open(ser_b, errors="replace").read()
    except OSError:
        ser = ""
    if "virelai> " in ser:
        ok = True
        break
    time.sleep(0.5)
if not ok:
    tail = ""
    try:
        tail = open(os.path.join(rd, "vmb-runner.log"), errors="replace").read()[-2000:]
    except OSError:
        pass
    sys.exit("vm-b never reached the monitor prompt — two-runner bring-up "
             "failed; runner rc=%s; tail: %s" % (proc.poll(), tail))
log_text = open(os.path.join(rd, "vmb-runner.log"), errors="replace").read()
if "this runner owns the segment hub" not in log_text:
    sys.exit("vm-b is not the hub owner — first-runner-binds violated; "
             "runner log tail: " + log_text[-2000:])

# vm-b's leg file: dhcp, the paced ARP settle + B->A ping train, then ONE
# GOSH whose children browse and fetch sequentially.
#
# The settle is load-bearing and is why the train cannot simply be typed
# after the browse. The kernel learns a peer's MAC only from an ARP reply
# for an outstanding solicitation, the pending slot never lapses inside a
# run, but the FIRST reply can trickle in tens of seconds after the guest
# binds (observed live: 42 `net ping` attempts errored 'peer not in ARP
# table' while the same guest resolved the entry a minute later, and
# another run learned .16 only after ~20 s of pacer waits). GOFETCH refuses
# an unresolved peer outright ('no route ... resolve first'), so a fetch
# immediately after the browse fails on a cold B. Each cycle therefore
# sends `net ping 10.0.0.2` first — the runner's --net-arp-respond target
# gets no ICMP answer, so the monitor's bounded wait is a real ~1 s
# delay — then re-opens the .16 solicitation and pings it. The train is a
# settle, not the acceptance: five cycles plus the paced browse is enough
# on the observed runs, and the B->A echo itself is typed by its own
# client only after A logs the fetch (vmb-ping-legs.txt), so a reply that
# trails the train can no longer fail the run. Only then does the browse
# run — after A's burst, as the client's --after boundary requires.
#
# GOSH's children run sequentially, so the fetch cannot start until
# GONETSTAT's bounded window has closed and its resolved service line has
# printed. That ordering is the card's acceptance (M206b residual, #3814):
# a monitor `exec` browse is asynchronous, so a fetch typed after it raced
# the browse and could reach /healthz before B had folded the answer. Each
# child runs with its OWN manifest row's words (GONETSTAT: net + net-mdns;
# GOFETCH: net), the same shape JB.SH's STORE/FLINT children rely on.
#
# The watchdog arm leads the legs, before even dhcp: an unanswered .99
# solicitation keeps a pending slot valid for B's whole session (B's
# runner spans all runs) — observed live: a stall during the dhcp legs
# killed every pump unanswered with owed=0, reset never armed. Armed
# first, the rxq stall-reset stays live through dhcp, the browse AND
# the install legs — where a mid-transfer wedge would otherwise tear.
legs = ["net arp 10.0.0.99", "net dhcp"] * 5
legs += ["net arp 10.0.0.16", "net arp 10.0.0.2"]
for _ in range(5):
    legs += ["net ping 10.0.0.2", "net arp 10.0.0.16",
             "net ping 10.0.0.16", "net", "net recv"]
# The browse script's marker prints only when its last line runs; the typed
# `source` line carries no marker, so no console-echo trick is needed.
legs += ['exec GOSH.ELF -c "source /host/BROWSE.SH"']
open(os.path.join(rd, "vmb-legs.txt"), "w").write("\n".join(legs) + "\n")

# vm-b's B->A echo legs, typed by a SECOND run-a client gated on A's
# 'httpd: request /healthz' — that line means B already resolved and used
# `10.0.0.16` for the health fetch. A monitor `exec` is asynchronous, so a
# ping typed right after the browse raced GOFETCH's ARP resolution: on cold
# boots A's solicited reply can trail the whole monitor-paced train, and the
# ping ran before `.16` was in B's table (`error: peer not in ARP table`).
# Waiting for A's request line makes the order deterministic: discovery
# folded the answer, the fetch resolved and used `10.0.0.16`, and only then
# does this leg solicit, resolve (the `is at` line) and echo. The split
# marker string keeps the console echo of the typed line from matching.
open(os.path.join(rd, "vmb-ping-legs.txt"), "w").write(
    "net arp 10.0.0.16\n"
    "net ping 10.0.0.16\n"
    'exec GOSH.ELF -c "echo vmb-ping""-done"\n')

# vm-b's M206c leg file — typed by the M206c run-a client once 'flint:
# push ' lands on A's serial. TWO monitor execs (both async spawns): a
# detached gosh loops `GOFETCH ... /keepalive` — the keep-alive traffic
# that keeps whichever GOHTTPD instance is bound from hitting its 30 s
# AcceptConn expiry mid-install (JA.SH's restart loop only covers the
# publish gap; a served conn resets the window, so a live instance stays
# bound for the whole leg) — and the plain (uid_user) gosh that sources
# JB.SH, whose children run sequentially — the live-dev-loop-proven
# shape for the install legs (the flint-remote secret serves the
# uid-1000 lane).
#
# The keep-alive is PACED and BOUNDED, and both are load-bearing. vm-b is
# one long-lived peer_watch runner across all of vgate_repeat's
# iterations, so this line is typed once per iteration into the SAME
# shell and the loops stack: an unpaced `while true; do GOFETCH ...; done`
# spins a new TCP connection with no gap, and by iterations 2-3 two or
# three of them were churning the guest's bounded client-slot table
# against STORE's own two connections. Observed on the 2026-11-06
# reference run: 200 keep-alive fetches connected and only 6 EOFs, while
# the four installs in iterations 2-3 were refused `store: refuse
# channel_missing` and A never saw their GETs at all (`httpd: request
# /DROP*` = 0). `sleep 20` holds the 30 s window open with one fetch per
# loop per 20 s, i.e. the same coverage at ~100x less socket churn. The
# 10-iteration bound (~200 s, longer than any one M206c leg) then ends
# the loop instead of leaving a `while true` running to the end of the
# whole K=3 run — observed 2026-11-19 a-03: with three unbounded loops
# the guest logged `net: rxq reset n=89`, A's ARP repl froze at 0 and
# the install refused channel_missing.
#
# The keep-alive hits /keepalive, NOT /healthz: A's stage-2 (the A->B
# ARP/ping proof) and the B->A echo client both gate on 'httpd: request
# /healthz', which must mean "vm-b's browse fetch just landed". A stacked
# keep-alive from an earlier iteration printing /healthz made that marker
# fire mid-train in iterations 2-3; A's 21 pings then arrived while B's
# rxq was stalled, B's icmp repl froze, and A's pong stayed 0 (observed
# 2026-11-19, a-02: A req=40/repl=11/pong=0, B repl frozen at 42). Any
# served path keeps the connection window open, so /keepalive is enough.
open(os.path.join(rd, "vmb-store-legs.txt"), "w").write(
    "net arp 10.0.0.2\n"
    "net\n"
    "net arp 10.0.0.16\n"
    "net\n"
    "net arp 10.0.0.2\n"
    'exec GOSH.ELF -c "for i in 1 2 3 4 5 6 7 8 9 10; do GOFETCH.ELF http://10.0.0.16:8080/keepalive ; sleep 20 ; done"\n'
    'exec GOSH.ELF -c "source /host/JB.SH"\n'
    "net recv\n")
# M214c (#3920): one GOSH per FLINTD phase, typed with a busy-retry
# client so no two sync scripts race the store. Phase 4 is the auth leg:
# uid_agent (2000) has no `flint-remote` row, so the same FLINT.ELF
# client is challenged and refuses auth_required. `exec -u2000` only
# spells the principal; the secret split, not a renamed binary, is what
# makes the client keyless (a renamed copy lands on the ambient floor and
# is refused at exec before it can run — observed `refused:file_denied`).
for _ph in (1, 2, 3, 4):
    open(os.path.join(rd, "vmb-f%d.txt" % _ph), "w").write(
        'exec%s GOSH.ELF -c "source /host/F%d.SH"\n'
        % (" -u2000" if _ph == 4 else "", _ph))

# M217 (#3921) — vm-b's supervised journey. Two GOSH legs, one console
# session each, both gated on markers the DAEMON's guest prints to its
# serial (a desk's own markers arrive on the console bridge's client
# capture, not in a serial file, so they cannot gate the next desk):
#   SUP1  discover the supervised daemon's instance after its burst,
#         clone the stream vm-a opened, pull it, author a signed fork and
#         push it back — the ordinary journey.
#   SUP2  one session for the rest: pull + verify --all after vm-a
#         advanced the cloned stream, then the fault leg — clone vm-a's
#         `fv<N>` and push a REAL multi-Change suffix in a dense push
#         train of four passes alternating the /host/BWSF and /host/FWS
#         byte variants of the same paths, so every add is new-or-modified
#         and every push carries bytes. A's stage 2 has already launched
#         SUPKILL.SH in its own guest, and its lead lands inside this
#         train; the failed push prints VMB-SUP-INTERRUPTED and the
#         retries after the restart finish the stream. The pull after the
#         restart is honest either way: it either recovers the verified
#         complete suffix or refuses stream_torn (recorded as
#         VMB-SUP-TORN) — no silent repair, no fabricated success.
open(os.path.join(bshare, "SUP1.SH"), "w").write(
    "set N=$(cat /host/SEQB)\n"
    "echo $N > /host/CURB\n"
    "GONETSTAT.ELF discover _flint._tcp\n"
    "FLINT.ELF clone 10.0.0.16:4748 sv$N || sleep 1 ; FLINT.ELF clone 10.0.0.16:4748 sv$N\n"
    "FLINT.ELF pull --stream=sv$N || sleep 1 ; FLINT.ELF pull --stream=sv$N\n"
    "FLINT.ELF fork sv$N bsv$N || echo B-SUP-FORK-EXISTS\n"
    "FLINT.ELF init --name=bwsv$N --src=/host/BWSL --stream=bsv$N || echo B-SUP-INIT-EXISTS\n"
    "FLINT.ELF add /host/BWSL/a.go || true\n"
    "FLINT.ELF commit --ws=bwsv$N -m \"b-sup-$N\" || echo B-SUP-COMMIT-UP-TO-DATE\n"
    "FLINT.ELF push --remote=10.0.0.16:4748 --stream=bsv$N || sleep 1 ; FLINT.ELF push --remote=10.0.0.16:4748 --stream=bsv$N\n"
    "FLINT.ELF verify --all\n"
    "echo VMB-SUP-JOURNEY-DONE\n")
open(os.path.join(bshare, "SUP2.SH"), "w").write(
    "set N=$(cat /host/CURB)\n"
    "FLINT.ELF pull --stream=sv$N || sleep 1 ; FLINT.ELF pull --stream=sv$N\n"
    "FLINT.ELF verify --all\n"
    "echo VMB-SUP-VERIFIED\n"
    "FLINT.ELF clone 10.0.0.16:4748 fv$N || sleep 1 ; FLINT.ELF clone 10.0.0.16:4748 fv$N\n"
    "FLINT.ELF fork fv$N bf$N || echo B-SUPF-FORK-EXISTS\n"
    "FLINT.ELF init --name=bfw$N --src=/host/BWSF --stream=bf$N || echo B-SUPF-INIT-EXISTS\n"
    "for i in 1 2 3 4 5 6 7 8; do FLINT.ELF add --ws=bfw$N /host/BWSF/f$i.go || true ; FLINT.ELF commit --ws=bfw$N -m \"fault-b1-$i-$N\" || true ; FLINT.ELF push --remote=10.0.0.16:4748 --stream=bf$N || echo VMB-SUP-INTERRUPTED ; sleep 1 ; done\n"
    "for i in 1 2 3 4 5 6 7 8; do FLINT.ELF add --ws=bfw$N /host/FWS/f$i.go || true ; FLINT.ELF commit --ws=bfw$N -m \"fault-w1-$i-$N\" || true ; FLINT.ELF push --remote=10.0.0.16:4748 --stream=bf$N || echo VMB-SUP-INTERRUPTED ; sleep 1 ; done\n"
    "for i in 1 2 3 4 5 6 7 8; do FLINT.ELF add --ws=bfw$N /host/BWSF/f$i.go || true ; FLINT.ELF commit --ws=bfw$N -m \"fault-b2-$i-$N\" || true ; FLINT.ELF push --remote=10.0.0.16:4748 --stream=bf$N || echo VMB-SUP-INTERRUPTED ; sleep 1 ; done\n"
    "for i in 1 2 3 4 5 6 7 8; do FLINT.ELF add --ws=bfw$N /host/FWS/f$i.go || true ; FLINT.ELF commit --ws=bfw$N -m \"fault-w2-$i-$N\" || true ; FLINT.ELF push --remote=10.0.0.16:4748 --stream=bf$N || echo VMB-SUP-INTERRUPTED ; sleep 1 ; done\n"
    "FLINT.ELF push --remote=10.0.0.16:4748 --stream=bf$N && echo VMB-SUP-RECOVERED || echo VMB-SUP-RETRY-FAILED\n"
    "FLINT.ELF pull --remote=10.0.0.16:4748 --stream=bf$N || echo VMB-SUP-TORN\n"
    "FLINT.ELF verify --all\n"
    "echo VMB-SUP-FAULT-DONE\n")
for _n in (1, 2):
    open(os.path.join(rd, "vmb-sup%d.txt" % _n), "w").write(
        'exec GOSH.ELF -c "source /host/SUP%d.SH"\n' % _n)
# Third leg file — the admin half, typed by the THIRD client once B's
# good-install fetch prints on A's serial ('httpd: request
# /DROP/main/HUBAPP.ELF'). --retry-busy holds it behind client-2's
# session, so consent can only land after install's STORE.TXT write.
open(os.path.join(rd, "vmb-consent-legs.txt"), "w").write(
    'exec -u0 GOSH.ELF -c "source /host/JB2.SH"\n'
    "net recv\n")

# Host flintd on this spec's own lane (127.0.0.1:4853 <-> 10.0.0.2:4853
# through EACH runner's --net-tcp-respond relay): the live-dev-loop
# pattern, an EMPTY store so vm-a's push `create`s stream main — and
# every later iteration answers 'push up to date'. The signed Changes
# are the trust plane; no guest FLINT server exists (the M206 index's
# explicit non-goal), so the canonical remote is the host daemon both
# guests reach through their own runner.
def find_sibling(env_name, dirname, need):
    d = os.environ.get(env_name) or os.path.join(repo, "..", dirname)
    d = os.path.abspath(d)
    if not os.path.isfile(os.path.join(d, "build.zig")):
        sys.exit("%s checkout missing at %s - clone it once (outside the "
                 "repo): git clone https://github.com/drawmeanelephant/%s %s"
                 % (dirname, d, dirname, d))
    if not os.path.isfile(os.path.join(d, need)):
        subprocess.check_call(["zig", "build"], cwd=d)
    return os.path.join(d, need)

flintd = find_sibling("FLINT_SERVER_DIR", "flint-server",
                      os.path.join("zig-out", "bin", "flintd"))
flintd_store = os.path.join(rd, "hubd-store")
subprocess.check_call([flintd, "init", "--store=" + flintd_store])
auth_key_file = os.path.join(rd, "flint-auth.key")
open(auth_key_file, "w").write(AUTH_KEY_HEX + "\n")
# Lane claim (issue #3909): this hook used to sweep 4853 with
# `lsof -ti tcp:4853 | xargs kill -9`, which kills ANY holder. 4853 is this
# spec's own lane, but a second worktree running this same spec is normal in
# the multiagent model, and the sweep would SIGKILL that run's hubd mid-gate.
# tools/lib/port_guard.py leases the lane instead: a LIVE sibling is refused
# by name (exit 3, nothing signaled), while a dead run's leftover hubd is
# reaped — the #3731 self-heal this sweep was reaching for, without the
# collateral. check=True because a refusal must fail setup, not be swallowed.
subprocess.run([sys.executable,
                os.path.join("tools", "lib", "port_guard.py"),
                "4853"],
               check=True)
time.sleep(0.2)
flintd_log = open(os.path.join(rd, "hubd.log"), "wb")
# No --auth-key: the daemon serves without an auth challenge. The
# wire-secret lane (auth_required) is a live-dev-loop concern, not this
# card's verification bar — the trust evidence here is the clone's
# Ed25519 verify + the signed-release sha gate.
flintd_proc = subprocess.Popen(
    [flintd, "serve", "--store=" + flintd_store,
     "--listen=127.0.0.1:4853"],
    stdout=flintd_log, stderr=subprocess.STDOUT, start_new_session=True)
import socket as _socket
flintd_up = False
for _ in range(100):
    try:
        s = _socket.create_connection(("127.0.0.1", 4853), 0.2)
        s.close()
        flintd_up = True
        break
    except OSError:
        if flintd_proc.poll() is not None:
            break
        time.sleep(0.1)
if not flintd_up:
    sys.exit("hubd did not come up on 127.0.0.1:4853 (see hubd.log)")

# dropwatch: the tampered channel B refuses by name. A's publish writes
# /host/DROP/main/{release.go,HUBAPP.ELF} mid-run; this watcher waits for
# both members (with a settle poll, the same read-after-write discipline
# the run-04 python used), then drops DROPBAD — the same release.go and
# the ELF with one byte flipped, so release.go still verifies and the
# sha gate alone produces 'store: refuse elf_mismatch HUBAPP.ELF'.
dropwatch = '''
import os, shutil, sys, time
share, logp = sys.argv[1], sys.argv[2]
src = os.path.join(share, "DROP", "main")
dst = os.path.join(share, "DROPBAD", "main")
deadline = time.time() + 900
def note(msg):
    open(logp, "a").write(msg + "\\n")
while time.time() < deadline:
    try:
        rel = os.path.join(src, "release.go")
        elf = os.path.join(src, "HUBAPP.ELF")
        if os.path.isfile(rel) and os.path.isfile(elf):
            s1 = (os.path.getsize(rel), os.path.getsize(elf))
            time.sleep(0.5)
            if s1 != (os.path.getsize(rel), os.path.getsize(elf)):
                continue
            os.makedirs(dst, exist_ok=True)
            shutil.copy(rel, os.path.join(dst, "release.go"))
            b = bytearray(open(elf, "rb").read())
            b[len(b) // 2] ^= 0xFF
            open(os.path.join(dst, "HUBAPP.ELF"), "wb").write(bytes(b))
            note("dropwatch: DROPBAD armed (" + str(len(b)) + "B ELF, 1 byte flipped)")
            sys.exit(0)
    except OSError:
        pass
    time.sleep(0.5)
note("dropwatch: DROP/main never materialized — tamper leg will refuse "
     "channel_missing instead of elf_mismatch")
'''
dw_path = os.path.join(rd, "dropwatch.py")
open(dw_path, "w").write(dropwatch)
dw_log = open(os.path.join(rd, "dropwatch-runner.log"), "wb")
subprocess.Popen(
    [sys.executable, dw_path, share, os.path.join(rd, "dropwatch.log")],
    stdout=dw_log, stderr=subprocess.STDOUT, start_new_session=True)
dw_log.close()

print("vmb: runner pid=%d up, owns nethub.sock, prompt reached; "
      "console-tcp 127.0.0.1:24720; hubd pid=%d on 127.0.0.1:4853; "
      "dropwatch armed" % (proc.pid, flintd_proc.pid))
PY

# vm-a — the foreground run: --net capture + --net-hub attachment + the
# --mac override, stage 1 at the prompt, stage 2 gated on B's fetch,
# stage 3 (M206c) chained on 'vma-legs-done' doing init->publish->push.
# The --net-arp/--net-tcp responders give A's guest a path to the host
# flintd through THIS runner's own relay (same shape B's runner carries
# for its clone); --virtio-fs joins the armed cvc share because FLINT's
# streams/ listing is virtiofs-only (the v1 DirList truncation). The run
# CANNOT end on A's own last echo — GOHTTPD dies with the VM — so the
# expect is 'httpd: request /DROP/main/HUBAPP.ELF', a serial line only
# vm-b's install fetch can produce, and the 45 s tail holds A up through
# B's consent + app spawn.
#
# Budget: 720 s was measured too tight for this chain. Observed on the
# 2026-11-06 reference-host run — iterations 2 and 3 reached JA-DONE and
# had B's installs still in flight when the deadline fired (`FAILURE:
# expected transcript 'httpd: request /DROP/main/HUBAPP.ELF' not
# observed within 720s`), while iteration 1, whose only difference is a
# colder B, landed the same fetch inside the budget. The deadline is a
# chosen cap, not a property of the chain: every substantive leg before
# it printed green in all three iterations. 1200 s keeps the same
# ordering (it still bounds the run) with room for the clone + one
# 150-byte fetch under gate load.
vgate_run a -- --net '$RUN_DIR/a.cap' --net-hub '$RUN_DIR/nethub.sock' --mac 02:00:00:00:00:0a \
    --net-arp-respond 10.0.0.2 --net-tcp-respond 10.0.0.2:4853:relay --net-tcp-respond-relay 127.0.0.1:4853 \
    --virtio-fs '$RUN_DIR/share' \
    --script '$RUN_DIR/vma-s1.txt' \
    --script2 '$RUN_DIR/vma-s2.txt' --script2-after 'httpd: request /healthz' --script2-delay 10 \
    --script3 '$RUN_DIR/vma-s3.txt' --script3-after 'vma-legs-done' \
    --script-expect 'httpd: request /DROP/main/HUBAPP.ELF' --script-expect-tail 60 --timeout 1800

# The run-a client: waits for A's 'httpd: advertise burst done' — the
# post-burst boundary (M206b residual, #3814). Earlier cards browsed DURING
# the burst because GOHTTPD was announce-only; after the shared responder
# landed, a browse after the burst must be answered by a real query. The
# ordering is load-bearing: the browse and its resolution cannot be credited
# to the startup announcements. The legs type the ARP settle + train and
# then the GOSH; GOSH's own 'vmb-browse-fetch-done' echo is the only safe
# stop marker — 'fetch: done' also prints from the M206c keep-alive client,
# and a monitor-typed marker executes before the async GOSH finishes.
vgate_client a -- --addr '127.0.0.1:24720' --after 'httpd: advertise burst done' --after-timeout 240 \
    --connect-timeout 30 --send-file '$RUN_DIR/vmb-legs.txt' --expect 'vmb-browse-fetch-done' --timeout 600

# The B->A echo client: gated on A's 'httpd: request /healthz' — the fetch
# only prints that line after B resolved 10.0.0.16 and connected, so the
# echo cannot race the browse's own ARP resolution (the client's --after
# watches A's serial, not B's). --retry-busy queues it behind the browse
# client's session, which holds the console until the GOSH marker. Its
# capture is its own file (the browse capture is the one the client-contains
# asserts read), so the echo evidence is judged on B's serial in the python
# block below.
vgate_client a -- --addr '127.0.0.1:24720' --after 'httpd: request /healthz' --after-timeout 400 \
    --connect-timeout 60 --retry-busy --send-file '$RUN_DIR/vmb-ping-legs.txt' \
    --expect 'vmb-ping-done' --timeout 150 --out '$RUN_DIR/client-a-ping.out'

# M214c (#3920) — the direct guest-to-guest FLINT clients. Each waits on
# the FLINTD phase marker that only stage 3 (FA.SH) prints, connects, and
# types ONE foreground GOSH. --retry-busy queues a phase's client behind
# the previous phase's session, so vm-b's console runs one sync script at
# a time. F1 is gated on 'flintd: advertise burst done' — the shared
# responder's post-burst boundary — so discovery can never be credited to
# the startup announcements.
vgate_client a -- --addr '127.0.0.1:24720' --after 'flintd: advertise burst done' --after-timeout 1000 \
    --connect-timeout 180 --retry-busy --send-file '$RUN_DIR/vmb-f1.txt' \
    --expect 'VMB-F1-DONE' --timeout 400 --out '$RUN_DIR/client-a-flint1.out'
vgate_client a -- --addr '127.0.0.1:24720' --after 'VMA-FLINTD2' --after-timeout 1300 \
    --connect-timeout 180 --retry-busy --send-file '$RUN_DIR/vmb-f2.txt' \
    --expect 'VMB-F2-DONE' --timeout 400 --out '$RUN_DIR/client-a-flint2.out'
vgate_client a -- --addr '127.0.0.1:24720' --after 'VMA-FLINTD3' --after-timeout 1500 \
    --connect-timeout 180 --retry-busy --send-file '$RUN_DIR/vmb-f3.txt' \
    --expect 'VMB-F3-DONE' --timeout 400 --out '$RUN_DIR/client-a-flint3.out'
vgate_client a -- --addr '127.0.0.1:24720' --after 'VMA-FLINTD4' --after-timeout 1700 \
    --connect-timeout 180 --retry-busy --send-file '$RUN_DIR/vmb-f4.txt' \
    --expect 'VMB-F4-DONE' --timeout 400 --out '$RUN_DIR/client-a-flint4.out'

# The M206c client: waits for A's VMA-FA-DONE — the marker stage 3 prints
# after the LAST FLINTD phase has exited, its own verify passed, and the
# GOHTTPD restart loop is about to bind :8080. That guarantees BOTH
# halves B needs are armed: hubd holds stream `main` (the trust plane,
# pushed after every publish) and /host/DROP/ is served by A's GOHTTPD
# (the untrusted payload, with dropwatch's DROPBAD beside it). It must
# NOT gate on 'flint: push ' any more: stage 3 pushes several times while
# the FLINTD phases still own :5353. Its one typed line starts the -u0
# gosh on JB.SH; 'JB-DONE' is the capture's last needed line.
vgate_client a -- --addr '127.0.0.1:24720' --after 'VMA-FA-DONE' --after-timeout 1750 \
    --connect-timeout 120 --retry-busy --send-file '$RUN_DIR/vmb-store-legs.txt' \
    --expect 'JB-DONE' --timeout 600 --out '$RUN_DIR/client-a-store.out'

# The consent client: the good install's ELF fetch is the A-serial
# marker that proves install is materially through (the remaining work
# is sha + local state writes); --retry-busy then queues this client
# behind client-2's session so the -u0 consent line only reaches the
# console after JB.SH's install legs have fully run. JB2-DONE is the
# capture's last needed line. Last-declared => its rc is enforced.
#
# Sizing: every client's --after clock starts at run launch, so the
# marker wait must cover the whole chain to the ELF GET — boot, card
# b's legs, A's paced arp stage, the s3 verify pace, JA.SH publish+push,
# then B's DROPBAD refusals + DROP clone before the fetch. That marker
# is also run-a's own --script-expect under --timeout 1200, so
# --after-timeout 1000 can never lap it inside a run that could still
# pass. The post-marker busy window ends when client-2's session does —
# bounded by ITS 600 s read deadline — so --connect-timeout 540 covers
# the worst queue.
vgate_client a -- --addr '127.0.0.1:24720' --after 'httpd: request /DROP/main/HUBAPP.ELF' --after-timeout 1780 \
    --connect-timeout 540 --retry-busy --send-file '$RUN_DIR/vmb-consent-legs.txt' \
    --expect 'JB2-DONE' --timeout 240 --out '$RUN_DIR/client-a-consent.out'

# --- vm-a asserts (the foreground run's serial + runner stdout) --------------
vgate_assert a output-contains 'NET-HUB: attached to hub'
vgate_assert a serial-contains 'net: mac=02:00:00:00:00:0a'
vgate_assert a serial-contains 'net: dhcp bound ip=10.0.0.16'
vgate_assert a serial-contains 'httpd: listening on port 8080'
vgate_assert a serial-contains 'httpd: advertise sent'
vgate_assert a serial-contains 'httpd: advertise burst done'
vgate_assert a serial-contains 'httpd: mdns query _http._tcp.local'
vgate_assert a serial-contains 'httpd: mdns answer v206b._http._tcp.local addr=10.0.0.16 port=8080'
vgate_assert a serial-contains 'httpd: request /healthz'
vgate_assert a serial-contains 'httpd: served'
vgate_assert a serial-contains 'net arp: 10.0.0.17 -> 02:00:00:00:00:0b'
vgate_assert a serial-contains 'net ping: echo request to 10.0.0.17 sent'
vgate_assert a serial-contains 'vma-legs-done'
# --- M206c vm-a asserts: the publish lane + the guest-to-guest fetches --
# 'store: published' pins the signed drop's sha; 'flint: push ' covers
# iteration 1's 'push ok changes=1' and later iterations' 'up to date'.
# The 'httpd: request /DROP*' lines are the strongest hub evidence
# available: only vm-b's installs can produce them, so each one is a
# frame that crossed the segment into A's GOHTTPD.
vgate_assert a serial-contains 'store: published HUBAPP 1.0.0 sha256='
vgate_assert a serial-contains 'flint: push '
vgate_assert a serial-contains 'httpd: request /DROPBAD/main/HUBAPP.ELF'
vgate_assert a serial-contains 'httpd: request /DROP/main/HUBAPP.ELF'
vgate_assert a serial-contains 'JA-DONE'
# M214c (#3920): the staged FLINTD lifecycle — four bounded :5353
# owners, each advertising the SAME configured instance and answering
# vm-b's post-burst query; the server side of the two refusals; and the
# final verify before GOHTTPD retakes the port.
vgate_assert a serial-contains 'flintd: listening port=4748'
vgate_assert a serial-contains 'flintd: advertising hubflint._flint._tcp.local port=4748'
vgate_assert a serial-contains 'flintd: advertise sent'
vgate_assert a serial-contains 'flintd: advertise burst done'
vgate_assert a serial-contains 'flintd: mdns query _flint._tcp.local'
vgate_assert a serial-contains 'flintd: mdns answer hubflint._flint._tcp.local addr=10.0.0.16 port=4748'
vgate_assert a serial-contains 'flintd: refuse seq='
vgate_assert a serial-contains 'name=stale_head'
vgate_assert a serial-contains 'name=author_not_allowed'
vgate_assert a serial-contains 'flint: verify '
vgate_assert a serial-contains 'VMA-FA-DONE'
vgate_assert a serial-absent 'flintd: advertise bind failed'
vgate_assert a serial-absent '9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60'
vgate_assert a serial-absent '4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb'
vgate_assert a serial-absent '[EXC]'

# --- vm-b asserts on the client capture (the B->A half) ----------------------
# One capture: the ARP settle/train precedes the browse, and the GOSH's
# post-chain marker stops it after the resolution and the fetch response.
# The B->A echo is not in this capture (the ping client has its own --out by
# construction); the python block below judges it on vm-b's serial.
vgate_assert a client-contains 'net: dhcp bound ip=10.0.0.17'
vgate_assert a client-contains 'fetch: connected'
vgate_assert a client-contains 'fetch: done'
vgate_assert a client-contains '"status":"ok"'
vgate_assert a client-contains 'netstat: discover service v206b._http._tcp.local host=v206b.local port=8080 addr=10.0.0.16'

# --- vm-b's serial, the counters, and the artifacts ---------------------------
# The browse window can outlive the client capture (it closes at 'fetch:
# done'), so the discover tail is judged on vm-b's serial — polled, because
# it can also outlive the run end. Both VMs' serials + B's runner stdout are
# lifted into artifacts/ here (the run dir is deleted at gate_end).
#
# mDNS truth after the M206b residual (#3814): the IPv4 dst gate now admits
# 224.0.0.251 for UDP :5353, and GOHTTPD's :5353 owner runs the shared
# bounded responder. B's browse therefore starts ONLY after A's burst has
# completed (the client above is gated on 'httpd: advertise burst done'),
# sends a real QU query, and must fold A's matching reply into the exact
# DNS-SD chain: service v206b._http._tcp.local, host v206b.local, port
# 8080, addr 10.0.0.16. A's serial pins its side of that exchange (query
# receipt + answer line). The ordering below is the point: a browse that
# overlapped the burst, or a resolved-looking fetch with no prior answer,
# cannot pass.
vgate_assert a python <<'PY'
import os, re, shutil, sys, time
rd = os.environ["RUN_DIR"]
suffix = os.environ.get("VIRELAI_GATE_SUFFIX", "")
ser_b_path = os.path.join(rd, "vm-serial-vmb.log")
deadline = time.time() + 120
ser_b = ""
while time.time() < deadline:
    try:
        ser_b = open(ser_b_path, errors="replace").read()
    except OSError:
        ser_b = ""
    # The B->A echo legs are typed by a client gated on the browse marker,
    # so they land AFTER the discover/fetch lines: wait for all three or a
    # fast discover/fetch read would race the echo client and fail `need`.
    if ("netstat: discover done" in ser_b and "fetch: done" in ser_b
            and "net ping: echo request to 10.0.0.16 sent" in ser_b):
        break
    time.sleep(0.5)
# Artifacts FIRST: the run dir is deleted at gate_end, so both VMs' serials
# + vm-b's runner stdout are lifted now — a failing leg below still leaves
# the evidence on disk.
os.makedirs("artifacts", exist_ok=True)
if os.path.isfile(ser_b_path):
    shutil.copy(ser_b_path, "artifacts/live-net-hub-serial-vmb%s.log" % suffix)
rlog_path = os.path.join(rd, "vmb-runner.log")
if os.path.isfile(rlog_path):
    shutil.copy(rlog_path, "artifacts/live-net-hub-run-vmb%s.txt" % suffix)
ser_a = open(os.environ["VG_SER"], errors="replace").read()
resolved = ("netstat: discover service v206b._http._tcp.local "
            "host=v206b.local port=8080 addr=10.0.0.16")
need = [
    "virelai> ",
    "net: mac=02:00:00:00:00:0b",
    "net: dhcp bound ip=10.0.0.17",
    "net ping: echo request to 10.0.0.16 sent",
    "netstat: discover _http._tcp.local",
    "netstat: discover query _http._tcp.local",
    resolved,
    "netstat: discover done",
    "fetch: connected",
    "fetch: done",
    '{"status":"ok","port":8080}',
]
missing = [n for n in need if n not in ser_b]
if missing:
    sys.exit("FAIL: vm-b serial missing legs: " + " | ".join(missing))

def ordered(text, first, second, where):
    a = text.find(first)
    b = text.find(second)
    if a < 0 or b < 0 or a >= b:
        sys.exit("FAIL: %s ordering: %r (at %d) must precede %r (at %d)"
                 % (where, first, a, second, b))

# A announced first, then answered the later query.
ordered(ser_a, "httpd: advertise burst done", "httpd: mdns query _http._tcp.local", "vm-a burst/query")
ordered(ser_a, "httpd: mdns query _http._tcp.local", "httpd: mdns answer v206b._http._tcp.local", "vm-a query/answer")
if "httpd: mdns answer v206b._http._tcp.local addr=10.0.0.16 port=8080" not in ser_a:
    sys.exit("FAIL: vm-a's answer line does not name the resolved identity")
# B bound, browsed (after the burst by client construction), resolved the
# service, and only then fetched.
ordered(ser_b, "net: dhcp bound ip=10.0.0.17", "netstat: discover query _http._tcp.local", "vm-b bind/query")
ordered(ser_b, "netstat: discover query _http._tcp.local", resolved, "vm-b query/resolved")
ordered(ser_b, resolved, "fetch: connected", "vm-b resolved/fetch")
ordered(ser_b, "netstat: discover done", "fetch: connected", "vm-b done/fetch")
mdns_hits = [int(m) for m in re.findall(r"mdns=(\d+)", ser_b)]
if not any(m >= 1 for m in mdns_hits):
    sys.exit("FAIL: vm-b's rx_mdns never counted an mDNS frame from vm-a")
print("vm-b mDNS evidence: query -> %s -> fetch; rx_mdns grew to %d"
      % (resolved, max(mdns_hits)))
# vm-b resolved vm-a's MAC — either print form counts (the direct `is at`
# reply line or the table dump).
if ("net arp: 10.0.0.16 is at 02:00:00:00:00:0a" not in ser_b
        and "net arp: 10.0.0.16 -> 02:00:00:00:00:0a" not in ser_b):
    sys.exit("FAIL: vm-b never resolved vm-a's MAC")
# vm-b's own icmp counters after its pings: req>=1 + pong>=1 prove the
# B->A echo round-trip on B's side.
ms = re.findall(r"icmp=req=(\d+),repl=(\d+),pong=(\d+)", ser_b)
if not ms or not any(int(r) >= 1 and int(p) >= 1 for r, _e, p in ms):
    sys.exit("FAIL: vm-b icmp counters show no completed echo to vm-a: %r" % ms)
# vm-a's counters must show BOTH directions: req>=1 (its own echoes) and
# repl>=1 (it answered B's echoes) plus pong>=1 (B's replies observed).
ser_a = open(os.environ["VG_SER"], errors="replace").read()
ms = re.findall(r"icmp=req=(\d+),repl=(\d+),pong=(\d+)", ser_a)
if not ms or not any(int(r) >= 1 and int(e) >= 1 and int(p) >= 1
                   for r, e, p in ms):
    sys.exit("FAIL: vm-a icmp counters do not show both directions: %r" % ms)
rlog = open(os.path.join(rd, "vmb-runner.log"), errors="replace").read()
if "this runner owns the segment hub" not in rlog:
    sys.exit("FAIL: vm-b runner log lost the hub-owner line")
print("vm-b serial + runner log kept in artifacts/; both directions observed")
PY

# --- M214c (#3920): the direct guest-to-guest FLINT evidence --------------
# What must hold, read off the two guests' own logs and stores (never a
# host daemon or a file copy):
#   * B discovered `_flint._tcp` AFTER the announce burst and the answer
#     names the configured instance, vm-a's address and the ACTUAL port;
#   * B cloned/pulled vm-a's `main` and both sides verified the signed
#     raw history (verify --all prints sig: verified, never sig_invalid);
#   * B's accepted push (`e<N>`) has the SAME head and the SAME raw JSONL
#     bytes in vm-a's store and vm-b's;
#   * the stale leg left two sibling heads on `l<N>` — vm-a's log still
#     contains the raw Change B pushed, but its head moved on;
#   * the denied leg's signed Change is in vm-b's store and NOT in
#     vm-a's, while vm-a's serial names the wire refusals.
# Each of the four phases is judged by its own client capture as well.
vgate_assert a python <<'PY'
import glob, json, os, shutil, sys
rd = os.environ["RUN_DIR"]
suffix = os.environ.get("VIRELAI_GATE_SUFFIX", "")
share_a = os.environ["VG_SHARE"]
share_b = os.path.join(rd, "share-vmb")
ser_a = open(os.environ["VG_SER"], errors="replace").read()
ser_b_path = os.path.join(rd, "vm-serial-vmb.log")
ser_b = open(ser_b_path, errors="replace").read() if os.path.isfile(ser_b_path) else ""
os.makedirs("artifacts", exist_ok=True)
for n in (1, 2, 3, 4):
    cap = os.path.join(rd, "client-a-flint%d.out" % n)
    if os.path.isfile(cap):
        shutil.copy(cap, "artifacts/live-net-hub-flint%d%s.out" % (n, suffix))

discovered = ("netstat: discover service hubflint._flint._tcp.local "
              "host=hubflint.local port=4748 addr=10.0.0.16")
if discovered not in ser_b:
    sys.exit("FAIL: vm-b never resolved the FLINT advertisement: %r" % discovered)
if "flint: clone ok" not in ser_b:
    sys.exit("FAIL: vm-b serial has no successful clone")
if "flint: pull ok" not in ser_b and "flint: pull up to date" not in ser_b:
    sys.exit("FAIL: vm-b serial has no completed pull")
if "flint: push ok" not in ser_b:
    sys.exit("FAIL: vm-b serial has no accepted push")
for line in ("flint: refuse stale_head", "flint: refuse author_not_allowed",
             "flint: refuse auth_required"):
    if line not in ser_b:
        sys.exit("FAIL: vm-b serial lacks the literal client refusal %r" % line)
for marker in ("VMB-F1-DONE", "VMB-F2-DONE", "VMB-F3-DONE", "VMB-F4-DONE"):
    if marker not in ser_b:
        sys.exit("FAIL: vm-b serial lacks phase marker " + marker)
if "sig_invalid" in ser_b:
    sys.exit("FAIL: vm-b verify --all found an invalid signature")
# The server side is judged on THIS iteration's vm-a serial.
need_a = [
    "flintd: listening port=4748",
    "flintd: advertising hubflint._flint._tcp.local port=4748",
    "flintd: advertise burst done",
    "flintd: mdns query _flint._tcp.local",
    "flintd: mdns answer hubflint._flint._tcp.local addr=10.0.0.16 port=4748",
    "flintd: refuse seq=",
    "name=stale_head",
    "name=author_not_allowed",
    "flint: verify ",
    "VMA-FA-DONE",
]
missing = [x for x in need_a if x not in ser_a]
if missing:
    sys.exit("FAIL: vm-a serial missing FLINTD/sync lines: " + " | ".join(missing))
if "flintd: advertise bind failed" in ser_a:
    sys.exit("FAIL: a second :5353 owner was attempted in vm-a")
if ser_a.count("flintd: advertising hubflint._flint._tcp.local") < 4:
    sys.exit("FAIL: fewer than four staged FLINTD lifecycles on vm-a")
if "sig_invalid" in ser_a:
    sys.exit("FAIL: vm-a verify --all found an invalid signature")

def load_streams(root):
    out = {}
    for p in glob.glob(os.path.join(root, "streams", "*.json")):
        try:
            rec = json.load(open(p))
        except (OSError, ValueError):
            continue
        name, sid, head = rec.get("name"), rec.get("id"), rec.get("head")
        logp = os.path.join(root, "streams", sid + ".jsonl")
        log = open(logp, "rb").read() if os.path.isfile(logp) else b""
        out[name] = (head, log)
    return out

a = load_streams(os.path.join(share_a, "FLINT"))
b = load_streams(os.path.join(share_b, "FLINT"))
if "main" not in a or "main" not in b:
    sys.exit("FAIL: main stream missing from one side (a=%s b=%s)"
             % (sorted(a), sorted(b)))
if a["main"] != b["main"]:
    sys.exit("FAIL: main head/raw history differs between the guests")
if not a["main"][1]:
    sys.exit("FAIL: main stream carries no raw signed history")
# B's accepted push: same stream name, same head, byte-identical JSONL.
edge = [n for n in b if n.startswith("e")]
if not edge:
    sys.exit("FAIL: vm-b has no accepted fork stream (e*)")
if not any(n in a and a[n] == b[n] for n in edge):
    sys.exit("FAIL: no accepted push has byte-identical history on both sides "
             "(a=%s b=%s)" % (sorted(a), sorted(b)))
# The stale leg: vm-a advanced the same stream, so heads differ while the
# raw Change B pushed is still verbatim in vm-a's log.
stale = [n for n in b if n.startswith("l") and n in a]
if not stale:
    sys.exit("FAIL: no stale-leg stream exists on both sides")
healed = False
for n in stale:
    bhead, blog = b[n]
    ahead, alog = a[n]
    first = blog.split(b"\n", 1)[0] + b"\n"
    if ahead != bhead and first in alog:
        healed = True
if not healed:
    sys.exit("FAIL: stale leg did not leave an advanced head with B's raw Change")
# The denied leg: B's signed Change exists only on B.
denied = [n for n in b if n.startswith("d")]
if not denied:
    sys.exit("FAIL: vm-b has no denied fork stream (d*)")
ok_denied = False
for n in denied:
    bhead, blog = b[n]
    if not blog:
        continue
    first = blog.split(b"\n", 1)[0] + b"\n"
    ahead, alog = a.get(n, (None, b""))
    if first not in alog:
        ok_denied = True
if not ok_denied:
    sys.exit("FAIL: the author-refused Change reached vm-a's store")
print("m214c: discovered %s; main bytes equal (%d B); accepted push bytes "
      "equal; stale advanced with B's raw Change; denied Change stayed on B"
      % (discovered, len(a["main"][1])))
PY

# --- M206c: vm-b's install chain, judged on its serial + host files ----
# B's serial accumulates ALL iterations (one peer_watch runner for the
# gate), so counts are pinned against JB-DONE completions: every pass
# must refuse the tampered drop exactly once by the live-dev-loop name
# and land exactly one successful install. The trust split is read off
# B's share: TRUSTED.TXT pins only vm-a's key, SECRETS.TXT has no
# agent-sign, GRANTS.TXT carries the hash-bound consent row, APPS.TXT
# the installed row, and the installed ELF hashes to A's published
# sha256= line.
vgate_assert a python <<'PY'
import hashlib, os, re, sys, time
rd = os.environ["RUN_DIR"]
ser_b_path = os.path.join(rd, "vm-serial-vmb.log")
bshare = os.path.join(rd, "share-vmb")
deadline = time.time() + 120
ser_b = ""
while time.time() < deadline:
    try:
        ser_b = open(ser_b_path, errors="replace").read()
    except OSError:
        ser_b = ""
    n = ser_b.count("JB2-DONE")
    if n >= 1 and ser_b.count("store: refuse elf_mismatch HUBAPP.ELF") >= n:
        break
    time.sleep(0.5)
n_jb = ser_b.count("JB2-DONE")
if n_jb < 1:
    sys.exit("FAIL: vm-b serial shows no completed JB.SH pass")
need = [
    "store: install main",
    "store: remote 10.0.0.2:4853",
    "flint: clone ok",
    "store: verified main",
    "store: grant propose HUBAPP.ELF words=file,memory,process,debug,net",
    "store: consent HUBAPP.ELF",
    "store: grant written name=HUBAPP.ELF words=file,memory,process,debug,net",
    "hubapp: m206c-ok",
]
missing = [x for x in need if x not in ser_b]
if missing:
    sys.exit("FAIL: vm-b serial missing store legs: " + " | ".join(missing))
n_ref = ser_b.count("store: refuse elf_mismatch HUBAPP.ELF")
if n_ref != n_jb:
    sys.exit("FAIL: %d elf_mismatch refusals for %d JB passes" % (n_ref, n_jb))
n_ok = ser_b.count("store: install ok HUBAPP.ELF") + \
    ser_b.count("store: already installed HUBAPP.ELF")
if n_ok < n_jb:
    sys.exit("FAIL: only %d successful installs for %d JB passes" % (n_ok, n_jb))
for bad in ("store: refuse key_unknown", "flint: refuse key_unknown",
            "store: refuse remote_missing", "store: refuse caps_",
            "store: refuse release_mismatch", "store: refuse eacces",
            "store: refuse channel_missing", "store: refuse sync_failed"):
    if bad in ser_b:
        sys.exit("FAIL: unexpected refusal on vm-b serial: " + bad)

# Host-side receipts on vm-b's share.
apps = open(os.path.join(bshare, "APPS.TXT"), errors="replace").read() \
    if os.path.isfile(os.path.join(bshare, "APPS.TXT")) else ""
if "HUBAPP.ELF" not in apps:
    sys.exit("FAIL: vm-b APPS.TXT has no HUBAPP.ELF row")
state = open(os.path.join(bshare, "STORE.TXT"), errors="replace").read() \
    if os.path.isfile(os.path.join(bshare, "STORE.TXT")) else ""
if "name=HUBAPP.ELF" not in state \
        or "caps=file,memory,process,debug,net" not in state:
    sys.exit("FAIL: vm-b STORE.TXT lost the install or the ask: %r" % state)
secrets = open(os.path.join(bshare, "SECRETS.TXT")).read()
# M214c (#3920): vm-b now signs its own sync Changes, so it MUST carry an
# agent-sign seed — its own RFC 8032 TEST 2 seed, never vm-a's. That is
# the trust split this card preserves: B can author its own history, but
# it cannot forge vm-a's release, and B's TRUSTED pins both publishers.
if "agent-sign" not in secrets:
    sys.exit("FAIL: vm-b SECRETS.TXT lost its own agent-sign seed")
if "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb" not in secrets:
    sys.exit("FAIL: vm-b agent-sign is not the distinct TEST 2 seed")
if "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60" in secrets:
    sys.exit("FAIL: vm-b holds vm-a's signing seed")
trusted_b = open(os.path.join(bshare, "FLINT", "TRUSTED.TXT")).read()
for k in ("d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a",
          "3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c"):
    if k not in trusted_b:
        sys.exit("FAIL: vm-b TRUSTED.TXT is missing a publisher key " + k[:16])
m = re.search(r"store: published HUBAPP 1\.0\.0 sha256=([0-9a-f]{64})",
              open(os.environ["VG_SER"], errors="replace").read())
if not m:
    sys.exit("FAIL: no published sha256 on vm-a's serial")
have = hashlib.sha256(
    open(os.path.join(bshare, "HUBAPP.ELF"), "rb").read()).hexdigest()
if have != m.group(1):
    sys.exit("FAIL: installed HUBAPP.ELF sha %s != published %s"
             % (have, m.group(1)))
grants = open(os.path.join(bshare, "GRANTS.TXT")).read()
rows = [l for l in grants.splitlines() if l and not l.startswith("#")]
want_row = "HUBAPP.ELF\t" + have + "\tfile,memory,process,debug,net"
if want_row not in rows:
    sys.exit("FAIL: consent wrote no hash-bound row: %r" % rows)
print("m206c: %d JB pass(es) — elf_mismatch x%d, install ok, consent row "
      "landed, app ran; sha256=%s" % (n_jb, n_ref, have[:16]))
PY

# --- M217 (#3921): the opt-in supervised boot, staged between the runs ------
# Run a left the shares in their M206c/M214c shape. Run b is the ordinary
# supervised boot: INIT autostarts from the SHIPPED manifest (seat only),
# then a marker-driven switch writes the SAME shipped manifest with the
# flintd row enabled — an ordinary reload of an ordinary row. No schema,
# restart-policy, admission or device change, and no test-only fault
# switch: the default boot itself is the disabled-by-default evidence.
#
# The switch also records the supervisor's own `svc: start name=flintd
# pid=` line, so the kill stage can only ever signal a pid INIT reported
# (never a name lookup, never another gate's process).
vgate_assert a python <<'PY'
import json, os, pathlib, subprocess, sys
rd = os.environ["RUN_DIR"]
share = pathlib.Path(os.environ["VG_SHARE"])
bshare = pathlib.Path(rd) / "share-vmb"

shipped = pathlib.Path("user/go/init/SERVICES.JSON").read_bytes()
m = json.loads(shipped)
row = [s for s in m["services"] if s["name"] == "flintd"]
if len(row) != 1:
    sys.exit("FAIL: the shipped manifest has no single flintd row")
if row[0].get("enabled") is not False:
    sys.exit("FAIL: the shipped flintd row is not disabled by default")
if row[0].get("class") != "on-demand":
    sys.exit("FAIL: the shipped flintd row is not on-demand")
(share / "INIT").mkdir(exist_ok=True)
(share / "INIT/SERVICES.JSON").write_bytes(shipped)
enabled = json.loads(shipped)
for s in enabled["services"]:
    if s["name"] == "flintd":
        s["enabled"] = True
(share.parent / "INIT-ENABLED.JSON").write_text(json.dumps(enabled))
(share / "SETTINGS.TXT").write_text("#v2\nwm=gotabwm\n")

def rotate(p):
    v = p.read_text().strip() if p.is_file() else "a"
    p.write_text({"a": "b", "b": "c", "c": "d"}.get(v, v) + "\n")

rotate(share / "SEQB")
rotate(bshare / "SEQB")

watch = '''import os, re, shutil, sys, time
ser, share, enabled, pidpath = sys.argv[1:5]
def note(msg):
    # The pid file must live in the share (the guest reads it); the host
    # log belongs beside the run dir, never inside the guest's filesystem.
    open(os.path.join(os.path.dirname(os.path.dirname(pidpath)),
                      "supswitch.log"), "a").write(msg + "\\n")
seen_opt = False
deadline = time.time() + 900
while time.time() < deadline:
    try:
        s = open(ser, errors="replace").read()
    except OSError:
        s = ""
    m = re.search(r"svc: start name=flintd pid=(\\d+)", s)
    if m and not os.path.exists(pidpath):
        open(pidpath, "w").write(m.group(1) + "\\n")
        note("switch: supervisor start recorded pid=" + m.group(1))
    if (not seen_opt) and "VMA-SUP-OPTOUT" in s:
        seen_opt = True
        shutil.copyfile(enabled, os.path.join(share, "INIT", "SERVICES.JSON"))
        note("switch: enabled copy of the shipped row written (ordinary reload)")
        # Keep watching for the pid: the reload needs a tick to start it.
        grace = time.time() + 120
        while time.time() < grace:
            try:
                s2 = open(ser, errors="replace").read()
            except OSError:
                s2 = ""
            m2 = re.search(r"svc: start name=flintd pid=(\\d+)", s2)
            if m2:
                open(pidpath, "w").write(m2.group(1) + "\\n")
                note("switch: supervised flintd pid=" + m2.group(1))
                sys.exit(0)
            time.sleep(0.25)
        note("switch: no supervised flintd start after opt-in")
        sys.exit(1)
    time.sleep(0.25)
note("switch: VMA-SUP-OPTOUT never observed")
sys.exit(1)
'''
(share.parent / "supswitch.py").write_text(watch)
log = open(os.path.join(rd, "supswitch.out"), "wb")
subprocess.Popen([sys.executable, str(share.parent / "supswitch.py"),
                  os.path.join(rd, "vm-serial-b.log"), str(share),
                  str(share.parent / "INIT-ENABLED.JSON"),
                  str(share / "SUPPID")],
                 stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
log.close()
print("m217: staged the shipped manifest (flintd disabled) + the marker-driven "
      "opt-in switch for run b; token rotated")
PY

# --- M217 run b: one ordinary supervised boot, one two-desk journey ---------
# Stage 1 pumps DHCP/ARP and prints the default-boot process table BEFORE
# any opt-in: `procs` shows the seat and no FLINTD. It then sources
# SUPSTART.SH (open the stream) and prints VMA-SUP-OPTOUT, which is the
# ONLY thing that enables the shipped row — the between-runs watcher writes
# it. Stage 2 runs after vm-b's first push created its stream (the daemon's
# own `op=create` receipt) and advances the journey through the supervised
# daemon, then prints the fault marker, waits its lead and kills the pid
# the supervisor reported (stage 2's tail — no third script).
vgate_file vma-sup-s1.txt <<'EOF'
net dhcp
net dhcp
net dhcp
net dhcp
net dhcp
net arp 10.0.0.99
net
procs
exec GOSH.ELF -c "source /host/SUPSTART.SH"
EOF
vgate_file vma-sup-s2.txt <<'EOF'
exec GOSH.ELF -c "source /host/SUPADV.SH"
EOF

# The host-flintd relay (10.0.0.2:4853 through THIS runner) is the same
# lane run a carried: A's `main` pull in stage 2 needs it, and the host
# daemon holds the stream run a pushed. The arp responder gives A the
# forged .2 peer the relay answers for.
vgate_run b -- --screen '$RUN_DIR/screen-sup' --net '$RUN_DIR/b.cap' --net-hub '$RUN_DIR/nethub.sock' \
    --mac 02:00:00:00:00:0a \
    --net-arp-respond 10.0.0.2 --net-tcp-respond 10.0.0.2:4853:relay --net-tcp-respond-relay 127.0.0.1:4853 \
    --virtio-fs '$RUN_DIR/share' \
    --script '$RUN_DIR/vma-sup-s1.txt' --script-after 'init: seated' \
    --script2 '$RUN_DIR/vma-sup-s2.txt' --script2-after 'op=create' \
    --script-expect 'VMA-SUP-KILLED' --script-expect-tail 300 --timeout 660

# The two desks are gated only on markers the daemon's own guest prints to
# its serial, and each desk is one console session: a desk's own markers
# arrive on the console bridge's client capture, never in a serial file,
# so a marker printed by one desk cannot gate the next. (Two desks gated
# on markers that became true within a millisecond raced for the one
# bridge; the loser spent its whole connect budget on `console-tcp: busy`
# and wrote `<connect failed>`.)
vgate_client b -- --addr '127.0.0.1:24720' --after 'flintd: advertise burst done' --after-timeout 240 \
    --connect-timeout 120 --retry-busy --send-file '$RUN_DIR/vmb-sup1.txt' \
    --expect 'VMB-SUP-JOURNEY-DONE' --timeout 300 --out '$RUN_DIR/client-b-sup1.out'
vgate_client b -- --addr '127.0.0.1:24720' --after 'VMA-SUP-ADVANCED' --after-timeout 400 \
    --connect-timeout 120 --retry-busy --send-file '$RUN_DIR/vmb-sup2.txt' \
    --expect 'VMB-SUP-FAULT-DONE' --timeout 600 --out '$RUN_DIR/client-b-sup2.out'

# The default boot is the disabled-by-default evidence: INIT reports one
# enabled row (the seat), starts it, and the process table before the
# opt-in carries no FLINTD. The reload is the ordinary manifest wait.
vgate_assert b serial-contains 'init: autostart INIT.ELF (settings init=on)'
vgate_assert b serial-contains 'init: manifest ok n=1'
vgate_assert b serial-contains 'init: start name=seat order=1'
vgate_assert b serial-contains 'init: seated'
vgate_assert b serial-contains 'VMA-SUP-OPTOUT'
vgate_assert b serial-contains 'init: reload applied'
vgate_assert b serial-contains 'init: start name=flintd'
vgate_assert b serial-contains 'svc: start name=flintd pid='
vgate_assert b serial-contains 'flintd: listening port=4748'
vgate_assert b serial-contains 'flintd: advertising flint._flint._tcp.local port=4748'
vgate_assert b serial-contains 'flintd: advertise burst done'
vgate_assert b serial-contains 'flintd: mdns query _flint._tcp.local'
vgate_assert b serial-contains 'flintd: mdns answer flint._flint._tcp.local addr=10.0.0.16 port=4748'
vgate_assert b serial-contains 'flintd: request seq='
vgate_assert b serial-contains 'op=create'
vgate_assert b serial-contains 'VMA-SUP-STREAM-READY'
vgate_assert b serial-contains 'VMA-SUP-ADVANCED'
vgate_assert b serial-contains 'VMA-SUP-JOURNEY-DONE'
vgate_assert b serial-contains 'VMA-SUP-FAULT-READY'
vgate_assert b serial-contains 'VMA-SUP-KILL-PID='
vgate_assert b serial-contains 'VMA-SUP-KILLED'
vgate_assert b serial-contains 'svc: exit name=flintd status=137'
vgate_assert b serial-contains 'svc: backoff name=flintd k='
# Persistent by default: the supervised daemon never takes the optional
# idle exit (only the staged run-a phases pass --idle-exit), never dies on
# the bounded AcceptConn timeout, and never gives up the port.
vgate_assert b serial-absent 'flintd: idle exit'
vgate_assert b serial-absent 'flintd: accept failed'
vgate_assert b serial-absent 'flintd: advertise bind failed'
vgate_assert b serial-absent 'httpd:'
vgate_assert b serial-absent '[EXC]'

vgate_assert b python <<'PY'
import json, os, pathlib, shutil, sys
rd = os.environ["RUN_DIR"]
share = pathlib.Path(os.environ["VG_SHARE"])
bshare = pathlib.Path(rd) / "share-vmb"
suffix = os.environ.get("VIRELAI_GATE_SUFFIX", "")
ser = open(os.environ["VG_SER"], errors="replace").read()
ser_b_path = os.path.join(rd, "vm-serial-vmb.log")
ser_b = open(ser_b_path, errors="replace").read() if os.path.isfile(ser_b_path) else ""
os.makedirs("artifacts", exist_ok=True)
for n in (1, 2):
    p = os.path.join(rd, "client-b-sup%d.out" % n)
    if os.path.isfile(p):
        shutil.copyfile(p, "artifacts/live-net-hub-sup%d%s.out" % (n, suffix))

fail = []
# --- the default boot owns no FLINTD ---------------------------------------
if "init: seated" not in ser or "VMA-SUP-OPTOUT" not in ser:
    sys.exit("FAIL: run b serial has no seated boot or opt-in marker")
seat = ser.index("init: seated")
opt = ser.index("VMA-SUP-OPTOUT")
if seat > opt:
    fail.append("the opt-in marker preceded the seated boot")
boot_seg = ser[:opt]
if "FLINTD" in boot_seg:
    fail.append("the default boot named FLINTD before the opt-in")
for line in boot_seg.splitlines():
    if line.startswith("svc: start") and "name=seat" not in line:
        fail.append("a non-seat service started before the opt-in: " + line)
first_start = ser.find("svc: start name=flintd")
reload_at = ser.find("init: reload applied")
if first_start < 0 or reload_at < 0 or reload_at > first_start:
    fail.append("flintd did not start from the post-reload manifest")

# --- the supervisor's own lifecycle: start -> 137 -> backoff -> start ------
import re
starts = re.findall(r"svc: start name=flintd pid=(\d+) t_ns=(\d+)", ser)
if len(starts) < 2:
    fail.append("only %d supervised flintd start(s) - the kill was not "
                "followed by a restart" % len(starts))
else:
    pids = [p for p, _ in starts]
    if len(set(pids)) != len(pids):
        fail.append("the supervised restart reused a pid: %r" % pids)
    kill_pid = re.search(r"VMA-SUP-KILL-PID=(\d+)", ser)
    if not kill_pid:
        fail.append("no recorded kill pid")
    elif kill_pid.group(1) != pids[0]:
        fail.append("kill pid %s is not the supervisor's first pid %s"
                    % (kill_pid.group(1), pids[0]))
    seg = ser[ser.index("VMA-SUP-KILL-PID="):]
    for s in ("svc: exit name=flintd status=137",
              "svc: backoff name=flintd k=",
              "svc: start name=flintd pid="):
        if s not in seg:
            fail.append("post-kill supervisor receipt missing: " + s)
    if "flintd: idle exit" in seg:
        fail.append("the supervised daemon used the optional idle exit")

# --- one pid served every pre-kill connection ------------------------------
# The supervised daemon must survive the bounded accept wait expiring
# (the deliberate 40 s idle window in stage 2) and then serve vm-b's
# later pull on the SAME pid. Any exit before the kill means the timeout
# still ended the service.
if "VMA-SUP-KILL-PID=" in ser:
    pre = ser[:ser.index("VMA-SUP-KILL-PID=")]
    if "svc: exit name=flintd" in pre:
        fail.append("the daemon exited before the kill (the idle accept "
                    "timeout was not treated as routine)")
    if pre.count("flintd: accepted") < 3:
        fail.append("the supervised daemon served fewer than three "
                    "connections before the kill")

# --- the desk: a real discovery, the journey, an interrupted push ----------
# The second desk's console is the harness console bridge, and the bridge
# hands the guest's output to the attached client: on this leg the bytes
# arrive in the client captures, not in the spawned runner's serial file.
# Both are guest output, so the judging text is the desk's own captured
# console (plus the serial when the runner also mirrored it there, which it
# does on other legs).
desk = ser_b
for _n in (1, 2):
    _p = os.path.join(rd, "client-b-sup%d.out" % _n)
    if os.path.isfile(_p):
        desk += open(_p, errors="replace").read()
if not re.search(r"netstat: discover service flint\._flint\._tcp\.local "
                 r"host=flint\.local port=4748 addr=10\.0\.0\.16", desk):
    fail.append("the desk never resolved the supervised instance")
for s in ("flint: clone ok", "flint: push ", "flint: pull ",
          "flint: verify ", "VMB-SUP-JOURNEY-DONE", "VMB-SUP-VERIFIED",
          "VMB-SUP-INTERRUPTED", "VMB-SUP-FAULT-DONE"):
    if s not in desk:
        fail.append("the desk's console never showed: " + s)
if "sig_invalid" in desk:
    fail.append("a signature failed to verify on the desk")
i_fail = desk.find("VMB-SUP-INTERRUPTED")
i_done = desk.find("VMB-SUP-FAULT-DONE")
if i_fail < 0 or i_done < 0 or i_fail > i_done:
    fail.append("the interrupted push is not recorded before the fault leg "
                "completed")
if "VMB-SUP-TORN" in desk and "flint: refuse stream_torn" not in desk:
    fail.append("VMB-SUP-TORN appeared without a stream_torn refusal")
# The leg states its real recovery outcome: a push the restarted daemon
# served, or an honest failure. It may not report both, and it may not
# stay silent.
recovered = "VMB-SUP-RECOVERED" in desk
retry_failed = "VMB-SUP-RETRY-FAILED" in desk
if recovered and retry_failed:
    fail.append("the fault leg reported both recovery and failure")
if not recovered and not retry_failed:
    fail.append("the fault leg never reported its recovery outcome")
# No premature success: inside the desk's own stream the outcome and the
# completion marker follow the interruption that the kill caused.
for _m, _txt in (("VMB-SUP-RECOVERED", "recovery"),
                 ("VMB-SUP-RETRY-FAILED", "failed recovery")):
    _i = desk.find(_m)
    if _i >= 0 and i_fail >= 0 and _i < i_fail:
        fail.append("the leg reported " + _txt + " before the interruption")
# The kill must split a REAL sync: the fault train stages new bytes on its
# first pass and modified bytes on the next two (the /host/BWSF and
# /host/FWS variants of the same paths), and each of those pushes carries a
# Change to bf<N>. A train of up-to-date no-ops would have nothing in
# flight, so this is the assertion that keeps the fault leg from passing
# vacuously.
real_adds = (desk.count("flint: add files=1 new=1")
             + desk.count("flint: add files=1 new=0 modified=1"))
fault_pushes = len(re.findall(r"flint: push ok stream=bf", desk))
if real_adds < 4:
    fail.append("the fault leg staged no real byte changes")
if fault_pushes < 4:
    fail.append("the fault leg pushed no real Change before the kill")

# --- the store outcome is honest: complete-and-verified, or stream_torn ----
n_token = (share / "SEQB").read_text().strip()
def record(root, name):
    streams = root / "FLINT" / "streams"
    for p in streams.glob("*.json"):
        try:
            rec = json.loads(p.read_text())
        except (OSError, ValueError):
            continue
        if rec.get("name") == name:
            logp = streams / (rec.get("id", "") + ".jsonl")
            body = logp.read_bytes() if logp.is_file() else b""
            return rec, body
    return None, b""

rec, body = record(share, "bf" + n_token)
complete = False
if rec is None:
    fail.append("vm-a's supervised store has no fault stream bf%s" % n_token)
else:
    lines = body.split(b"\n")
    complete = bool(lines) and lines[-1] == b""
    if complete:
        lines = lines[:-1]
    parsed = []
    for ln in lines:
        try:
            parsed.append(json.loads(ln))
        except ValueError:
            complete = False
            break
    if complete and parsed:
        if not all(c.get("id") for c in parsed):
            fail.append("a recovered Change has no id")
        elif rec.get("head") != parsed[-1].get("id"):
            fail.append("the recovered fault log's last Change is not the "
                        "recorded head")
    elif complete:
        fail.append("the fault stream holds no Change at all")
    elif "flint: refuse stream_torn" not in ser_b and not retry_failed:
        fail.append("the fault log is torn but vm-b reported neither "
                    "stream_torn nor a failed recovery (no silent repair)")
    if recovered and not complete:
        fail.append("the leg claimed recovery but the fault log is torn")
if not any("flint: verify " in l for l in ser.splitlines()):
    fail.append("vm-a never verified the supervised store")

if fail:
    sys.exit("FAIL: m217 supervised journey:\n  " + "\n  ".join(fail))
print("m217: default boot n=1 (seat only, no FLINTD); opt-in reload started "
      "flintd; kill of pid %s -> status=137 -> backoff k=1 -> restart; "
      "vm-b cloned/pulled/pushed/verified; fault stream bf%s is %s and the "
      "leg reported %s"
      % (starts[0][0] if starts else "?", n_token,
         "complete and verified" if complete else "honestly torn",
         "recovery" if recovered else "an honest failed recovery"))
PY
