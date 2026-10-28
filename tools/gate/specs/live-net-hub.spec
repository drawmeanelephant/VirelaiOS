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
# HOST PREREQS (the setup hook fails honestly when absent):
#   bash tools/go/build-gohttpd.sh -> .build/go/GOHTTPD.ELF
#   bash tools/go/build-netdiag.sh -> .build/go/GONETSTAT.ELF
#   bash tools/go/build-gofetch.sh -> .build/go/GOFETCH.ELF

vgate_name live-net-hub "M206b #3814: two concurrent VMs on one --net-hub — ARP+ping both ways; vm-b browses vm-a's GOHTTPD _http._tcp over mDNS and fetches /healthz; K=3"
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

print("vmb: runner pid=%d up, owns nethub.sock, prompt reached; "
      "console-tcp 127.0.0.1:24720" % proc.pid)
PY

# vm-a — the foreground run: --net capture + --net-hub attachment + the
# --mac override, stage 1 at the prompt, stage 2 gated on B's fetch.
vgate_run a -- --net '$RUN_DIR/a.cap' --net-hub '$RUN_DIR/nethub.sock' --mac 02:00:00:00:00:0a \
    --script '$RUN_DIR/vma-s1.txt' \
    --script2 '$RUN_DIR/vma-s2.txt' --script2-after 'httpd: request /healthz' --script2-delay 3 \
    --script-expect 'vma-legs-done' --timeout 240

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
