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
#     `exec GOHTTPD.ELF --advertise v206b`. 'httpd: advertise sent' is both
#     A's announce receipt AND the client's --after marker (the listener is
#     already bound when it prints — the fetch can connect immediately).
#   * client on run a (the vgate_client pattern, vgate.sh :189/:580): waits
#     for 'httpd: advertise sent' in A's serial, then drives VM B's console
#     over the TCP bridge: dhcp pump (B binds 10.0.0.17 — the second lease),
#     arp + ping VM A, `exec GONETSTAT.ELF discover _http._tcp` (browses the
#     flooded mDNS announcements), `exec GOFETCH.ELF
#     http://10.0.0.16:8080/healthz`. The capture lands in client-a.out;
#     every line also lands in B's serial.
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
# proves the peer's channel is usable end to end: a second client waits
# on the 'flint: push ' receipt on A's serial and types `exec -u0
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
# The stream's signed Changes are the trust plane (clone/verify — FLINT
# sync between guests is the index's explicit non-goal, so the canonical
# remote stays the host flintd both runners reach through their own
# relays); every payload byte that matters — release.go and the ELF —
# arrives guest-to-guest. Run a ends on 'httpd: request
# /DROP/main/HUBAPP.ELF' — a marker only B's install can produce — plus
# a hold tail so B's consent+run finishes while both serials still
# stream; the python leg polls B's serial for JB-DONE and pins
# GRANTS.TXT / APPS.TXT / STORE.TXT host-side.
#
# HOST PREREQS (the setup hook fails honestly when absent):
#   bash tools/go/build-gohttpd.sh -> .build/go/GOHTTPD.ELF
#   bash tools/go/build-netdiag.sh -> .build/go/GONETSTAT.ELF
#   bash tools/go/build-gofetch.sh -> .build/go/GOFETCH.ELF
#   bash tools/go/build-store.sh   -> .build/go/STORE.ELF
#   bash tools/go/build-flint.sh   -> .build/go/FLINT.ELF
#   bash tools/go/build-gosh.sh    -> .build/go/GOSH.ELF
#   ../flint-server checkout (or FLINT_SERVER_DIR=...): hubd serves the
#   release stream on 127.0.0.1:4853 (this spec's own lane) — the M181
#   trust shape: stream history from the canonical remote, payload bytes
#   from the untrusted channel.

vgate_name live-net-hub "M206b #3814 + M206c #3815: two concurrent VMs on one --net-hub — ARP+ping both ways; vm-b browses vm-a's GOHTTPD _http._tcp over mDNS and fetches /healthz; then vm-a publishes+serves a signed release and vm-b installs it over the hub (tamper refuses elf_mismatch), consents, and runs it; K=3"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 3 BOOTS
vgate_note "K=3 consecutive green runs is the M206b (#3814) acceptance record (BOOTS overrides the default 3)"
vgate_note "vm-a (run a): --mac 02:00:00:00:00:0a, hub CLIENT, dhcp-binds 10.0.0.16, serves GOHTTPD --advertise v206b on :8080"
vgate_note "vm-b (peer_watch-spawned runner, --console-tcp 127.0.0.1:24720): --mac 02:00:00:00:00:0b, hub OWNER, dhcp-binds 10.0.0.17, browses + fetches vm-a"
vgate_note "a->b arp/ping on vm-a serial; b->a arp/ping + mDNS browse + /healthz fetch on the client capture and vm-b serial"

# vm-a stage 1: bind the hub lease, then advertise. The bare `net` after the
# pump pins the --mac override + bound counter line on this VM's serial.
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
# answered from B, pong= replies A got back).
vgate_file vma-s2.txt <<'EOF'
net arp 10.0.0.17
net
net arp 10.0.0.17
net arp 10.0.0.17
net
net ping 10.0.0.17
net
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
vgate_file vma-s3.txt <<'EOF'
net arp 10.0.0.2
net
net arp 10.0.0.2
net
exec GOSH.ELF -c "source /host/JA.SH"
EOF

# The payload app vm-a publishes and vm-b installs + runs. Deliberately
# minimal — the marker is the whole contract (the consent row, the
# elf_mismatch refusal, and the APPS.TXT/STORE.TXT records are the
# evidence, all host-pinnable).
vgate_file hubapp.go <<'EOF'
package main

import "virelai/vsys"

func main() {
	vsys.Println("hubapp: m206c-ok")
	vsys.Exit(0)
}
EOF

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
import os, shutil, subprocess, sys, time
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

# M206c — vm-a's publish lane: the store/shell ELFs, the payload app
# (built here from this spec's vgate_file'd hubapp.go), an empty FLINT
# workspace, the journey script, and the signer credentials (agent-sign
# is the same RFC 8032 TEST 1 seed live-dev-loop pins as GUEST_KEY; the
# flint-remote key matches the setup's hubd --auth-key). wm=none: the
# gosh->store->flint chain is the same task-pool shape live-dev-loop
# measured, so A's seat stays off too.
AUTH_KEY_HEX = "ab" * 32
GUEST_KEY = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
for name, how in (("STORE.ELF", "build-store.sh"),
                  ("FLINT.ELF", "build-flint.sh"),
                  ("GOSH.ELF", "build-gosh.sh")):
    src = os.path.join(".build", "go", name)
    if not os.path.isfile(src):
        sys.exit(name + " missing (expected " + src + ") - build it first: "
                 "bash tools/go/" + how)
    shutil.copy(src, os.path.join(share, name))
hub_build = os.path.join(rd, "hubapp-build")
os.makedirs(hub_build, exist_ok=True)
subprocess.run(
    ["bash", "tools/go/build-go.sh", os.path.join(rd, "hubapp.go")],
    env={**os.environ, "TMPDIR": hub_build, "GO_BUILD_OUT": hub_build,
         "GO_BUILD_NAME": "HUBAPP", "GO_LDFLAGS_VALUE": "-s -w"},
    check=True)
shutil.copy(os.path.join(hub_build, "HUBAPP.ELF"),
            os.path.join(share, "HUBAPP.ELF"))
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
# JA.SH: the M138d retry idiom ('X || sleep 1 ; X' — gosh sleep is
# mis-scaled ~100:1, so it only pays off as failure-path backoff against
# the virtiofs exec-lookup race; on the happy path each verb reruns
# idempotently). Iteration 1 lands FRESH + 'flint: push ok changes=1';
# iterations 2+ take EXISTS + 'push up to date' — client-2 waits only on
# the 'flint: push ' prefix both print.
open(os.path.join(share, "JA.SH"), "w").write(
    "FLINT.ELF init --name=hubws --src=/host/WS && echo JA-INIT-FRESH || echo JA-INIT-EXISTS\n"
    "STORE.ELF publish HUBAPP /host/HUBAPP.ELF --src=/host/WS --version=1.0.0 --stream=main --caps=file,memory,process,debug,net || sleep 1 ; STORE.ELF publish HUBAPP /host/HUBAPP.ELF --src=/host/WS --version=1.0.0 --stream=main --caps=file,memory,process,debug,net\n"
    "FLINT.ELF push --remote=10.0.0.2:4853 --stream=main || sleep 1 ; FLINT.ELF push --remote=10.0.0.2:4853 --stream=main\n"
    "echo JA-DONE\n")

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

# M206c — vm-b's consumer side: STORE/FLINT/GOSH for the install chain,
# OWNERS.TXT making FLINT.ELF/STORE.ELF uid_system-owned (app-bound
# secrets are invisible without it), a SECRETS.TXT carrying ONLY
# flint-remote at both uids (NO agent-sign: vm-b verifies and installs
# but cannot sign — the live-dev-loop trust split), a keyring pinning
# only vm-a's public key, an enforce-mode GRANTS.TXT the consent row
# lands in, and JB.SH — the install script the -u0 gosh sources:
# tampered drop refuses elf_mismatch first (bare line, pinned by count),
# the honest drop verifies + installs + stands the signed ask, consent
# writes the row, the app runs.
for name in ("STORE.ELF", "FLINT.ELF", "GOSH.ELF"):
    shutil.copy(os.path.join(".build", "go", name), os.path.join(bshare, name))
open(os.path.join(bshare, "OWNERS.TXT"), "w").write(
    "#v1\nFLINT.ELF\t644\t0\t-\nSTORE.ELF\t644\t0\t-\n")
open(os.path.join(bshare, "SECRETS.TXT"), "w").write(
    "#v1\nflint-remote\t1000\t" + AUTH_KEY_HEX + "\tFLINT.ELF,STORE.ELF\n"
    "flint-remote\t0\t" + AUTH_KEY_HEX + "\tFLINT.ELF,STORE.ELF\n")
os.makedirs(os.path.join(bshare, "FLINT"), exist_ok=True)
open(os.path.join(bshare, "FLINT", "TRUSTED.TXT"), "w").write(
    "#v1\nkey=" + GUEST_KEY + " name=storelan\n")
open(os.path.join(bshare, "GRANTS.TXT"), "w").write("#v1 mode=enforce\n")
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

# vm-b's leg file (see the note above the run section): a long repeated
# ARP+ping cycle with RX drains between, ending on the fetch + trailing
# evidence. The repetition makes the cold first iteration reliable — each
# `net arp` reopens the ~0.5 s learn window and the drains pull whatever
# has landed; once .16 resolves the entry carries ping and the fetch.
legs = ["net dhcp"] * 5
legs += ["exec GONETSTAT.ELF discover _http._tcp", "net recv"]
for _ in range(12):
    legs += ["net arp 10.0.0.16", "net", "net ping 10.0.0.16",
             "net", "net recv"]
legs += ["net dhcp", "net dhcp", "net arp"]
legs += ["exec GOFETCH.ELF http://10.0.0.16:8080/healthz"]
legs += ["net ping 10.0.0.16", "net", "net ping 10.0.0.16", "net",
         "net arp", "net recv"]
open(os.path.join(rd, "vmb-legs.txt"), "w").write("\n".join(legs) + "\n")

# vm-b's M206c leg file — typed by the SECOND run-a client once 'flint:
# push ' lands on A's serial. One monitor line: the plain (uid_user)
# gosh sources JB.SH, whose children run sequentially — the live-
# dev-loop-proven shape for the install legs (the flint-remote secret
# serves the uid-1000 lane).
open(os.path.join(rd, "vmb-store-legs.txt"), "w").write(
    "net arp 10.0.0.2\n"
    "net\n"
    "net arp 10.0.0.16\n"
    "net\n"
    "net arp 10.0.0.2\n"
    'exec GOSH.ELF -c "source /host/JB.SH"\n'
    "net recv\n")
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
subprocess.run(["sh", "-c", "lsof -ti tcp:4853 | xargs kill -9"],
               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
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
vgate_run a -- --net '$RUN_DIR/a.cap' --net-hub '$RUN_DIR/nethub.sock' --mac 02:00:00:00:00:0a \
    --net-arp-respond 10.0.0.2 --net-tcp-respond 10.0.0.2:4853:relay --net-tcp-respond-relay 127.0.0.1:4853 \
    --virtio-fs '$RUN_DIR/share' \
    --script '$RUN_DIR/vma-s1.txt' \
    --script2 '$RUN_DIR/vma-s2.txt' --script2-after 'httpd: request /healthz' --script2-delay 3 \
    --script3 '$RUN_DIR/vma-s3.txt' --script3-after 'vma-legs-done' \
    --script-expect 'httpd: request /DROP/main/HUBAPP.ELF' --script-expect-tail 45 --timeout 600

# The run-a client: waits for A's DHCP bind — deliberately ~2-4 s BEFORE
# 'httpd: advertise sent', not after it. GOHTTPD's advertiser is
# announce-only: 3 burst emits ~1 s apart, then one per TTL/2 (60 s —
# longer than a run lives). The only announces B's ~6 s browse window can
# catch are the burst emits, so the browse must open before/while the
# burst fires: --after on A's bind puts B's discover exec inside it.
# 'fetch: done' is the capture's last needed line; the discover tail
# continues into B's serial.
vgate_client a -- --addr '127.0.0.1:24720' --after 'net: dhcp bound ip=10.0.0.16' --after-timeout 120 \
    --connect-timeout 30 --send-file '$RUN_DIR/vmb-legs.txt' --expect 'fetch: done' --timeout 90

# The M206c client: waits for A's 'flint: push ' receipt — the marker
# that guarantees BOTH halves B needs are armed: hubd holds stream
# `main` (the trust plane) and /host/DROP/ is served by A's GOHTTPD (the
# untrusted payload, with dropwatch's DROPBAD beside it). Its one typed
# line starts the -u0 gosh on JB.SH; 'JB-DONE' is the capture's last
# needed line (the asserts judge each leg, so the expect just bounds
# the chain). Second client => own --out and --retry-busy against
# client-1's tail; last-declared, so its rc is the enforced one.
vgate_client a -- --addr '127.0.0.1:24720' --after 'flint: push ' --after-timeout 480 \
    --connect-timeout 60 --retry-busy --send-file '$RUN_DIR/vmb-store-legs.txt' \
    --expect 'JB-DONE' --timeout 480 --out '$RUN_DIR/client-a-store.out'

# The consent client: the good install's ELF fetch is the A-serial
# marker that proves install is materially through (the remaining work
# is sha + local state writes); --retry-busy then queues this client
# behind client-2's session so the -u0 consent line only reaches the
# console after JB.SH's install legs have fully run. JB2-DONE is the
# capture's last needed line. Last-declared => its rc is enforced.
vgate_client a -- --addr '127.0.0.1:24720' --after 'httpd: request /DROP/main/HUBAPP.ELF' --after-timeout 480 \
    --connect-timeout 120 --retry-busy --send-file '$RUN_DIR/vmb-consent-legs.txt' \
    --expect 'JB2-DONE' --timeout 240 --out '$RUN_DIR/client-a-consent.out'

# --- vm-a asserts (the foreground run's serial + runner stdout) --------------
vgate_assert a output-contains 'NET-HUB: attached to hub'
vgate_assert a serial-contains 'net: mac=02:00:00:00:00:0a'
vgate_assert a serial-contains 'net: dhcp bound ip=10.0.0.16'
vgate_assert a serial-contains 'httpd: listening on port 8080'
vgate_assert a serial-contains 'httpd: advertise sent'
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
vgate_assert a serial-absent '[EXC]'

# --- vm-b asserts on the client capture (the B->A half) ----------------------
vgate_assert a client-contains 'net: dhcp bound ip=10.0.0.17'
vgate_assert a client-contains 'net ping: echo request to 10.0.0.16 sent'
vgate_assert a client-contains 'fetch: connected'
vgate_assert a client-contains 'fetch: done'
vgate_assert a client-contains '"status":"ok"'

# --- vm-b's serial, the counters, and the artifacts ---------------------------
# The browse window can outlive the client capture (it closes at 'fetch:
# done'), so the discover tail is judged on vm-b's serial — polled, because
# it can also outlive the run end. Both VMs' serials + B's runner stdout are
# lifted into artifacts/ here (the run dir is deleted at gate_end).
#
# mDNS truth, observed + documented: the card asks for 'discover service
# v206b._http._tcp.local ... addr=10.0.0.16' on B. That line is
# ARCHITECTURALLY UNREACHABLE between two VirelaiOS guests today —
# kernel/src/ipv4.zig's dst gate admits only own_ip and 255.255.255.255
# ("multicast are NOT admitted — no consumer — the honest bound",
# M97e-F2), so A's 224.0.0.251-dst announces are dropped before any
# socket sees them, and the discover's own design (user/go/netstat/
# discover.go:12-16) says a unicast QU reply is "the only reply shape
# this stack delivers". GOHTTPD's advertiser is announce-only and never
# answers queries, so between two guests no unicast reply exists. What
# IS honest evidence here: B's browse runs the real QU query out onto
# the segment, A's announce frames cross the hub and are admitted at
# B's MAC layer (the rx `mdns=` counter grows) — asserted below — and
# the drop happens at the IPv4 gate, a deliberate stack bound the card
# cannot lift without touching kernel or GOHTTPD (both out of scope).
vgate_assert a python <<'PY'
import os, re, shutil, sys, time
rd = os.environ["RUN_DIR"]
suffix = os.environ.get("VIRELAI_GATE_SUFFIX", "")
ser_b_path = os.path.join(rd, "vm-serial-vmb.log")
deadline = time.time() + 45
ser_b = ""
while time.time() < deadline:
    try:
        ser_b = open(ser_b_path, errors="replace").read()
    except OSError:
        ser_b = ""
    if "netstat: discover done" in ser_b and "fetch: done" in ser_b:
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
need = [
    "virelai> ",
    "net: mac=02:00:00:00:00:0b",
    "net: dhcp bound ip=10.0.0.17",
    "net ping: echo request to 10.0.0.16 sent",
    "netstat: discover _http._tcp.local",
    "netstat: discover query _http._tcp.local",
    "netstat: discover done",
    "fetch: connected",
    "fetch: done",
    '{"status":"ok","port":8080}',
]
missing = [n for n in need if n not in ser_b]
if missing:
    sys.exit("FAIL: vm-b serial missing legs: " + " | ".join(missing))
# The strongest honest mDNS evidence available (see the comment above):
# A's announce frames were admitted at B's MAC layer — rx_mdns counts any
# frame whose dst is an mDNS group MAC (01:00:5e:00:00:fb/33:33::fb),
# and the only mDNS sender on the segment is A's advertiser.
mdns_hits = [int(m) for m in re.findall(r"mdns=(\d+)", ser_b)]
if not any(m >= 1 for m in mdns_hits):
    sys.exit("FAIL: vm-b's rx_mdns never counted an announce frame from vm-a")
print("vm-b mDNS evidence: rx_mdns grew to %d — announce frames crossed "
      "the hub into vm-b's stack (the IPv4 dst gate then drops them; "
      "see the spec comment)" % max(mdns_hits))
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
if "agent-sign" in secrets:
    sys.exit("FAIL: vm-b SECRETS.TXT must not carry agent-sign")
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
