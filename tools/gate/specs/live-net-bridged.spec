# live-net-bridged.spec — M117d (issue #2476, ADR 0057 D11): the opt-in
# real-LAN attachments --net-bridged <iface> / --net-vmnet. r1-r5 are the
# named refusals, asserted as loudly as the success path and
# host-independent: a nonexistent interface names itself, the
# one-network-device wall names both flags, and --net-vmnet-subnet's
# validation names itself. b1/v1 are dual-branch on entitlement: on a
# development-signed runner carrying the restricted keys
# (host/vm-runner/entitlements-net.plist — an ad-hoc signature cannot
# carry them; observed amfid "adhoc signed but contains restricted
# entitlements" on macOS 27.2) the VM boots and the guest's existing
# dhcp.zig binds a real lease plus a LAN-peer ping; on an ad-hoc-signed
# runner the runner refuses by name and the same asserts pass on the
# refusal line.
#
# Observed on this reference host (macOS 27.2, 2026-10-17): --net-bridged
# en0 refuses (com.apple.vm.networking absent from the ad-hoc signature —
# the SecTask self-check names it before VZ's config.validate() can).
# --net-vmnet SUCCEEDS here: the InternetSharing daemon's
# client_entitlement_check accepts com.apple.security.virtualization
# (which this signature carries) alongside com.apple.developer.
# networking.vmnet/root, so v1 exercises the full bound+ping branch.
# --net-vmnet-subnet 192.168.77.0/24 pins the daemon's rotating /24 so
# the branch is byte-assertable: the daemon's DHCP serves host=.1,
# guest=.2, and the lease records mask=0.0.0.0 gw=0.0.0.0 (the observed
# wire contents — the OFFER/ACK carries no option 1 or 3).
#
# M117f (#2478): h1 is the LAN-httpd leg — setup binds a real HTTP/1.0
# responder on THIS host's en0 IPv4:8077 (the host's own stack is a LAN
# peer from the bridged guest) and the guest ARPs, TCP-connects, and
# reads the canned 200 byte-patterned on the wire. The leg is
# conditional on en0 carrying an IPv4; the named entitlements/iface
# refusals short-circuit it exactly like b1. vgate_repeat makes the
# acceptance K: 3 consecutive green spec runs.
#
# M198a (#3349, index #3313): m1 is the multicast-delivery probe — the
# measurement the M198 arc hinges on. Setup spawns a host emitter that
# sends a fixed mDNS PTR query once a second to 224.0.0.251:5353 (en0,
# TTL 255) and to ff02::fb%en0 when a v6 scope exists: the
# "_m198a-probe._tcp.local" canary (nobody else queries it — a frame
# carrying it is provably host-originated) and a plain-IN "_http._tcp"
# query whose replies a real LAN's responders multicast. The guest's
# M198a MAC filter admits the two mDNS group MACs; the `net` report's
# mcast=/mdns=/mcast-filt= counters and `net recv` dumps record what
# arrived, and `net: offered=` records whether the device offers
# CTRL_VQ/CTRL_MAC_ADDR programming at all. The leg asserts the probe
# RAN end to end (markers + an offered= line + a parsed rx= counter
# line) and prints the verdict; arrived-v4/arrived-v6/arrived-both/
# absent are all legitimate findings — the gate pins the mechanics,
# the comment records the observation. The same named refusals as b1
# short-circuit it.

vgate_name live-net-bridged "M117d #2476 ADR 0057 D11 — --net-bridged/--net-vmnet named refusals + dual-branch lease/LAN-ping; M117f h1 LAN-httpd; M198a m1 multicast-delivery probe"
vgate_repeat 3 BOOTS
vgate_note "K=3 consecutive green spec runs is the M117f (#2478) acceptance record (BOOTS overrides the default 3)"
vgate_note "r1-r5 named refusals (host-independent): bogus iface, exclusivity x2, subnet requires+base"
vgate_note "b1 --net-bridged en0: dev-signed -> dhcp bound + gw ping; ad-hoc -> com.apple.vm.networking refusal"
vgate_note "v1 --net-vmnet --net-vmnet-subnet: bound 192.168.77.2 + ping host 192.168.77.1, or named vmnet_return_t refusal"
vgate_note "h1 --net-bridged en0: dev-signed -> dhcp bound + tcp connect/GET to the host's LAN httpd on en0:8077, or the same named refusals"
vgate_note "m1 --net-bridged en0: host mDNS emitter (canary + _http._tcp) vs guest mcast/mdns/mcast-filt counters + offered-bits record; verdict printed, every named outcome passes"

# r1-r5: the named refusals — the runner exits rc=1 before any VM is
# configured, so the output file carries the ERROR line and nothing else
# boots.
vgate_run r1 -- --net-bridged vz-bogus-iface-0 --timeout 20
vgate_allow_rc r1 1
vgate_assert r1 output-contains "--net-bridged: interface 'vz-bogus-iface-0' is not in VZBridgedNetworkInterface.networkInterfaces"
vgate_assert r1 output-contains "bridgeable here:"

vgate_run r2 -- --net-nat --net-bridged en0 --timeout 20
vgate_allow_rc r2 1
vgate_assert r2 output-contains "--net-bridged is mutually exclusive with --net-nat"

vgate_run r3 -- --net '$RUN_DIR/r3.cap' --net-vmnet --timeout 20
vgate_allow_rc r3 1
vgate_assert r3 output-contains "--net-vmnet is mutually exclusive with --net"

vgate_run r4 -- --net-vmnet-subnet 192.168.77.0/24 --timeout 20
vgate_allow_rc r4 1
vgate_assert r4 output-contains "--net-vmnet-subnet requires --net-vmnet"

vgate_run r5 -- --net-vmnet --net-vmnet-subnet 192.168.77.1/24 --timeout 20
vgate_allow_rc r5 1
vgate_assert r5 output-contains "--net-vmnet-subnet: '192.168.77.1/24' is not the network base"

# b1: --net-bridged en0. The bounded DHCP client never retransmits a lost
# DISCOVER, so s1 must not waste it — it stays neutral while the bridged
# attachment settles. s2 paces by the guest's own heartbeat
# (`--script2-after 'timer heartbeat ticks=45'`, past the observed VZ
# attach latency) then pumps the DHCP client; the LAN's always-listening
# DHCP server answers whenever the wire is up. s2 is generated by
# setup-python: it pings the host's default gateway (a real LAN peer)
# only when the default route rides the SAME interface the spec bridges
# (en0); otherwise the lease alone is the LAN proof.
vgate_file b1.s1 <<'EOF'
net
echo b1-pumped
EOF

vgate_setup_python <<'PY'
import os, re, subprocess
rd = os.environ["RUN_DIR"]
gw, iface = "", ""
try:
    out = subprocess.run(["route", "-n", "get", "default"],
                         capture_output=True, text=True, timeout=10).stdout
    m = re.search(r"gateway:\s+(\d+\.\d+\.\d+\.\d+)", out)
    i = re.search(r"interface:\s+(\S+)", out)
    if m: gw = m.group(1)
    if i: iface = i.group(1)
except Exception:
    pass
pingable = bool(gw) and iface == "en0"
open(os.path.join(rd, "b1.gw"), "w").write(gw if pingable else "")
lines = ["net dhcp"] * 10
if pingable:
    lines += ["net arp " + gw, "net arp " + gw, "net ping " + gw, "net arp", "net"]
lines += ["echo b1-ok"]
open(os.path.join(rd, "b1.s2"), "w").write("\n".join(lines) + "\n")
PY

vgate_run b1 -- --net-bridged en0 --script '$RUN_DIR/b1.s1' --script2 '$RUN_DIR/b1.s2' --script2-after 'timer heartbeat ticks=45' --script-expect 'b1-ok' --timeout 90
vgate_allow_rc b1 0 1
vgate_assert b1 python <<'PY'
import os, sys
rd = os.environ["RUN_DIR"]
out = open(os.path.join(rd, "run-" + os.environ["VG_TAG"] + ".out"),
           errors="replace").read()
if "requires the com.apple.vm.networking entitlement" in out:
    sys.exit(0)  # named refusal leg — the absent entitlement named itself
if "is not in VZBridgedNetworkInterface.networkInterfaces" in out:
    sys.exit(0)  # named refusal leg — en0 is not bridgeable on this host
ser_path = os.environ.get("VG_SER", "")
ser = open(ser_path, errors="replace").read() if ser_path and os.path.exists(ser_path) else ""
need = ["net: dhcp bound ip=", "b1-ok"]
gw_path = os.path.join(rd, "b1.gw")
gw = open(gw_path).read().strip() if os.path.exists(gw_path) else ""
if gw:
    need += ["net arp: request for " + gw + " sent",
             "net ping: echo request to " + gw + " sent",
             "pong=1"]
missing = [n for n in need if n not in ser]
if missing:
    sys.exit("FAIL: neither the com.apple.vm.networking refusal nor the bridged lease/ping completed; missing: " + " | ".join(missing))
PY

# v1: --net-vmnet on the pinned 192.168.77.0/24 network. The guest's
# bounded DHCP client never retransmits a lost DISCOVER (SELECTING waits
# forever — no guest-side retry exists), and the daemon's vmnet bootpd is
# intermittent (observed ~3 binds in ~10 runs here, and Apple's own
# Containerization calls vmnet_network_configuration_disable_dhcp and
# assigns statically — consistent with the service being best-effort),
# so the assert treats the DISCOVER as the attempt and the OFFER as the
# conditional: when the daemon answers, bound must follow (a lost ACK
# would be a real regression); when it doesn't, `selecting` is the
# honest observed state. The deterministic proof of the real LAN path is
# the ARP-learned + ping-pong to the daemon's host end, which is why s2
# is heartbeat-gated — `--script2-after 'timer heartbeat ticks=60'`
# lands every transmit after the observed worst-case attach latency
# (~48 s here). s1 also fires an early pump at prompt (the highest-yield
# DISCOVER window observed). On an unentitled host the named refusal
# replaces all of it (the python reads it off run-v1.out).
vgate_file v1.s1 <<'EOF'
net ip 192.168.77.5
net arp 192.168.77.1
net arp 192.168.77.1
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
net ip6
echo v1-s1
EOF
vgate_file v1.s2 <<'EOF'
net arp 192.168.77.1
net arp 192.168.77.1
net ping 192.168.77.1
net dhcp
net dhcp
net arp
net arp
net
echo v1-ok
EOF

vgate_run v1 -- --net-vmnet --net-vmnet-subnet 192.168.77.0/24 --script '$RUN_DIR/v1.s1' --script2 '$RUN_DIR/v1.s2' --script2-after 'timer heartbeat ticks=60' --script-expect 'v1-ok' --timeout 120
vgate_allow_rc v1 0 1
vgate_assert v1 python <<'PY'
import os, sys
rd = os.environ["RUN_DIR"]
out = open(os.path.join(rd, "run-" + os.environ["VG_TAG"] + ".out"),
           errors="replace").read()
if "failed (vmnet_return_t=" in out:
    sys.exit(0)  # named refusal leg — a non-success status carried by name
ser_path = os.environ.get("VG_SER", "")
ser = open(ser_path, errors="replace").read() if ser_path and os.path.exists(ser_path) else ""
if "net-vmnet: ENABLED (M117d, issue #2476, ADR 0057 D11)" not in out:
    sys.exit("FAIL: success branch missing the net-vmnet ENABLED line")
need = [
    "net ip: ip=192.168.77.5",
    "net dhcp: discover sent xid=",
    "net arp: 192.168.77.1 -> ",
    "net arp: entries=1",
    "net ping: echo request to 192.168.77.1 sent (46 bytes)",
    " icmp=req=1,repl=0,pong=1",
    "v1-ok",
]
missing = [n for n in need if n not in ser]
if missing:
    sys.exit("FAIL: neither a named vmnet refusal nor the pinned-subnet wire/ping completed; missing: " + " | ".join(missing))
# The daemon's DHCP is intermittent (observed): an OFFER must produce a
# REQUEST (the guest-side handshake step — a stall there is a real
# regression), and a bound transition line must come with the bound
# counter; a DISCOVER the daemon ignores stays SELECTING, the documented
# best-effort leg.
if "offer=1" in ser and "net dhcp: request sent" not in ser:
    sys.exit("FAIL: DHCP OFFER arrived but the REQUEST never went out")
if "net: dhcp bound ip=192.168.77.2" in ser and " dhcp=bound,ip=192.168.77.2" not in ser:
    sys.exit("FAIL: bound transition without the bound counter line")
PY

# h1: the M117f (#2478) LAN-httpd leg — a bridged boot reaches a real
# httpd on the LAN. The httpd is THIS host's own en0 stack: setup binds
# an HTTP/1.0 responder on the en0 IPv4, port 8077, and generates h1.s2
# with the concrete target. The guest's bounded TCP client ARPs the
# peer, completes the three-way handshake, sends a data segment, and
# reads the canned 200 back byte-patterned on the wire — 'net tcp recv:
# 48 54 54 50 2f 31 2e 30' is "HTTP/1.0" in hex, which only a real
# HTTP-speaking peer produces. The responder holds the connection open
# (the boot-08 shape) so the reply is read while the guest is still
# ESTABLISHED — a data+FIN burst could close the bounded client before
# `net tcp recv` ran.
#
# The leg inherits b1's host truth: on an ad-hoc-signed runner the
# com.apple.vm.networking refusal fires before any VM is configured and
# the python assert passes on the refusal line; on a development-signed
# runner carrying the restricted keys the full chain must land. When
# en0 carries no IPv4 there is no LAN peer to reach — s2 degrades to
# the DHCP pump and the assert requires the lease and markers only.
vgate_file h1_httpd.py <<'EOF'
# h1's LAN httpd: one canned HTTP/1.0 200 per connection, replied on any
# received bytes and then held open (the guest's `net tcp recv` must find
# the peer ESTABLISHED when it reads). peer_watch scopes this process to
# the gate's own lifetime.
import socket, sys, time
host, port = sys.argv[1], int(sys.argv[2])
srv = socket.socket()
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind((host, port))
srv.listen(8)
srv.settimeout(1.0)
RESP = b"HTTP/1.0 200 OK\r\nContent-Length: 13\r\n\r\nlan-httpd-ok\n"
conns = []
deadline = time.time() + 3600
while time.time() < deadline:
    try:
        conn, _ = srv.accept()
    except OSError:
        continue
    conn.settimeout(5.0)
    try:
        conn.recv(65536)
        conn.sendall(RESP)
    except OSError:
        pass
    conns.append(conn)
EOF

vgate_file h1.s1 <<'EOF'
net
echo h1-pumped
EOF

vgate_file h1.s3 <<'EOF'
net tcp
net tcp recv
net tcp close
net tcp
net tcp
echo h1-ok
EOF

vgate_setup_python <<'PY'
# h1: discover the en0 IPv4 the bridged guest can reach, start the LAN
# httpd scoped to this gate invocation, and only THEN generate h1.s2 —
# the connect chain is written only when the responder provably accepted
# a probe connection (a dead httpd cannot false-red the leg; the bound
# named in the assert is a missing en0 IPv4 or a refused bind, and in
# both cases the DHCP pump + markers still run).
import os, socket, subprocess, sys, time
rd = os.environ["RUN_DIR"]
ip = ""
try:
    ip = subprocess.run(["ipconfig", "getifaddr", "en0"],
                        capture_output=True, text=True, timeout=10).stdout.strip()
except Exception:
    pass
httpd_up = False
if ip:
    watch = os.path.join("user", "go", "browser", "peer_watch.py")
    log = open(os.path.join(rd, "h1-httpd.log"), "wb")
    proc = subprocess.Popen(
        [sys.executable, watch, str(os.getppid()),
         sys.executable, os.path.join(rd, "h1_httpd.py"), ip, "8077"],
        stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    for _ in range(20):
        try:
            probe = socket.create_connection((ip, 8077), timeout=1)
            probe.close()
            httpd_up = True
            break
        except OSError:
            time.sleep(0.25)
    if httpd_up:
        print("h1: LAN httpd pid=%d listening on %s:8077" % (proc.pid, ip))
    else:
        print("h1: httpd never accepted on %s:8077 — degrading to lease+markers (see h1-httpd.log)" % ip)
open(os.path.join(rd, "h1.ip"), "w").write(ip + "\n" if httpd_up else "")
# The bounded DHCP client never retransmits a lost DISCOVER, so s2 pumps
# it the way b1 does before spending the ARP/connect chain.
lines = ["net dhcp"] * 10
if httpd_up:
    lines += ["net arp " + ip, "net arp " + ip,
              "net tcp connect " + ip + " 8077",
              "net tcp", "net tcp", "net tcp send 8", "net tcp"]
lines += ["echo h1-sent"]
open(os.path.join(rd, "h1.s2"), "w").write("\n".join(lines) + "\n")
PY

# The run mirrors b1: s1 stays neutral while the bridged attachment
# settles, s2 is heartbeat-gated past the observed VZ attach latency,
# and s3 runs 2 s after h1-sent so the LAN round-trip (connect already
# done in s2; the 200 lands well inside the delay) is drained before
# `net tcp recv` prints it.
vgate_run h1 -- --net-bridged en0 --script '$RUN_DIR/h1.s1' --script2 '$RUN_DIR/h1.s2' --script2-after 'timer heartbeat ticks=45' --script3 '$RUN_DIR/h1.s3' --script3-after 'h1-sent' --script3-delay 2 --script-expect 'h1-ok' --timeout 120
vgate_allow_rc h1 0 1
vgate_assert h1 python <<'PY'
import os, sys
rd = os.environ["RUN_DIR"]
out = open(os.path.join(rd, "run-" + os.environ["VG_TAG"] + ".out"),
           errors="replace").read()
if "requires the com.apple.vm.networking entitlement" in out:
    sys.exit(0)  # named refusal leg — the absent entitlement named itself
if "is not in VZBridgedNetworkInterface.networkInterfaces" in out:
    sys.exit(0)  # named refusal leg — en0 is not bridgeable on this host
ser_path = os.environ.get("VG_SER", "")
ser = open(ser_path, errors="replace").read() if ser_path and os.path.exists(ser_path) else ""
need = ["net: dhcp bound ip=", "h1-ok"]
ip_path = os.path.join(rd, "h1.ip")
ip = open(ip_path).read().strip() if os.path.exists(ip_path) else ""
if ip:
    need += ["net tcp: established (peer=" + ip + ":8077)",
             "net tcp recv: 48 54 54 50 2f 31 2e 30",
             "net tcp: connection closed"]
missing = [n for n in need if n not in ser]
if missing:
    sys.exit("FAIL: neither the com.apple.vm.networking refusal nor the LAN-httpd leg completed; missing: " + " | ".join(missing))
PY

# m1/m2: the M198a (#3349) multicast-delivery probes — see the header
# comment for what they measure. The emitter is this host's own stack:
# once a second it multicasts a PTR query for the "_m198a-probe._tcp"
# canary (a frame carrying that qname is provably ours — it settles the
# "does host-originated multicast loop to the guest" question) and one
# for "_http._tcp" class-IN (its replies arrive multicast when real LAN
# responders exist). peer_watch scopes it to the gate's lifetime,
# exactly like h1's httpd.
#
# argv: <v4-bind-addr-or-empty> <v6-ifname-or-empty> <wait-for-addr>.
# m1 passes (en0's IPv4, en0, "") — emit immediately. m2 passes
# ("", "", 192.168.78.1) — the vmnet bridge interface only exists once
# the leg's VM creates the network, so the emitter polls until some
# interface carries the host end, then binds both families on it.
vgate_file mcast_emit.py <<'EOF'
import re, socket, subprocess, sys, time

bind4 = sys.argv[1] if len(sys.argv) > 1 else ""
ifname6 = sys.argv[2] if len(sys.argv) > 2 else ""
waitaddr = sys.argv[3] if len(sys.argv) > 3 else ""

def qname(name):
    out = b""
    for label in name.split("."):
        out += bytes([len(label)]) + label.encode()
    return out + b"\x00"

# Two plain-IN PTR queries (id=0, flags=0, qd=1): the canary only this
# emitter sends, and _http._tcp whose replies real responders multicast.
QUERIES = [
    b"\x00\x00\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00" + qname("_m198a-probe._tcp.local") + b"\x00\x0c\x00\x01",
    b"\x00\x00\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00" + qname("_http._tcp.local") + b"\x00\x0c\x00\x01",
]

def iface_holding(addr):
    # The interface whose configured IPv4 IS addr — for the vmnet leg
    # that is the bridge the daemon lights when the VM's network exists.
    for _, name in socket.if_nameindex():
        try:
            got = subprocess.run(["ipconfig", "getifaddr", name],
                                 capture_output=True, text=True,
                                 timeout=5).stdout.strip()
        except Exception:
            got = ""
        if got == addr:
            return name
    return None

def open4(ip):
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.setsockopt(socket.IPPROTO_IP, socket.IP_MULTICAST_IF,
                     socket.inet_aton(ip))
        s.setsockopt(socket.IPPROTO_IP, socket.IP_MULTICAST_TTL, 255)
        return s
    except OSError:
        return None

def open6(ifname):
    try:
        ifi = socket.if_nametoindex(ifname)
        s = socket.socket(socket.AF_INET6, socket.SOCK_DGRAM)
        s.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_MULTICAST_IF, ifi)
        s.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_MULTICAST_HOPS, 255)
        return s, ifi
    except OSError:
        return None, 0

deadline = time.time() + 3600
if waitaddr:
    # Poll until an iface carrying waitaddr appears (the VM's network is
    # created mid-run), then bind on it.
    while time.time() < deadline:
        name = iface_holding(waitaddr)
        if name:
            bind4 = waitaddr
            ifname6 = name
            sys.stderr.write("mcast_emit: %s resolved on %s\n" % (waitaddr, name))
            sys.stderr.flush()
            break
        time.sleep(1.0)

socks = []
if bind4:
    s4 = open4(bind4)
    if s4:
        socks.append((s4, ("224.0.0.251", 5353)))
if ifname6:
    s6, ifi = open6(ifname6)
    if s6:
        socks.append((s6, ("ff02::fb", 5353, 0, ifi)))

sys.stderr.write("mcast_emit: %d socket(s) live (v4=%s v6if=%s)\n"
                 % (len(socks), bind4 or "none", ifname6 or "none"))
sys.stderr.flush()
while time.time() < deadline and socks:
    for s, dst in socks:
        for q in QUERIES:
            try:
                s.sendto(q, dst)
            except OSError:
                pass
    time.sleep(1.0)
EOF

vgate_file m1.s1 <<'EOF'
net
echo m1-pumped
EOF

vgate_file m1.s2 <<'EOF'
net
net recv
echo m1-listen
EOF

vgate_file m1.s3 <<'EOF'
net
net recv
net
net recv
net
echo m1-ok
EOF

vgate_file m2.s1 <<'EOF'
net
echo m2-pumped
EOF

vgate_file m2.s2 <<'EOF'
net
net recv
echo m2-listen
EOF

vgate_file m2.s3 <<'EOF'
net
net recv
net
net recv
net
echo m2-ok
EOF

vgate_setup_python <<'PY'
# m1: spawn the en0 emitter (en0's IPv4 binds IP_MULTICAST_IF; empty =
# no v4 leg, v6 still tries en0's ifindex). m2: spawn the vmnet emitter
# in wait-mode — it polls for the iface carrying 192.168.78.1, which
# exists only while the m2 run's vmnet is up. Both are peer_watch-scoped
# to this gate's lifetime.
import os, subprocess, sys
rd = os.environ["RUN_DIR"]
ip = ""
try:
    ip = subprocess.run(["ipconfig", "getifaddr", "en0"],
                        capture_output=True, text=True, timeout=10).stdout.strip()
except Exception:
    pass
watch = os.path.join("user", "go", "browser", "peer_watch.py")
emit = os.path.join(rd, "mcast_emit.py")
for tag, args in (("m1", [emit, ip, "en0", ""]),
                  ("m2", [emit, "", "", "192.168.78.1"])):
    log = open(os.path.join(rd, tag + "-mcast.log"), "wb")
    proc = subprocess.Popen(
        [sys.executable, watch, str(os.getppid()), sys.executable] + args,
        stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    print("%s: emitter pid=%d" % (tag, proc.pid))
open(os.path.join(rd, "m1.ip"), "w").write(ip + "\n" if ip else "")
PY

# The verdict classifier is shared by m1/m2: parse the recv dumps and
# the cumulative counters, print the verdict record, write it to
# $RUN_DIR/<tag>-verdict.txt.
vgate_file mcast_verdict.py <<'EOF'
import os, re, sys
rd = os.environ["RUN_DIR"]
tag = os.environ["VG_TAG"]
ser = open(os.environ["VG_SER"], errors="replace").read()

# net: offered= — the device's raw feature bits (the "is CTRL_RX
# programming even possible" half: CTRL_VQ bit 17, CTRL_MAC_ADDR bit 23,
# CTRL_RX bit 16).
offered = re.search(r"net: offered=0x([0-9a-f]+)/0x([0-9a-f]+)", ser)
feats = (int(offered.group(1), 16) | (int(offered.group(2), 16) << 32)) if offered else 0

# The cumulative counters from the LAST `net` report — they survive the
# FIFO drains, so they are the durable evidence.
rxlines = re.findall(
    r"net: rx=frames=(\d+),bytes=(\d+),filtered=(\d+),overflow=(\d+),fifo=(\d+),mcast=(\d+),mdns=(\d+),mcast-filt=(\d+)",
    ser)
if not rxlines:
    sys.exit("FAIL: no M198a rx counter line — the report extension is missing")
frames, _, filtered, _, _, mcast, mdns, mcast_filt = (int(v) for v in rxlines[-1])

v4_hit = v6_hit = canary = False
canary_hex = "5f 6d 31 39 38 61 2d 70 72 6f 62 65"  # "_m198a-probe"
for line in ser.splitlines():
    if not line.startswith("net recv: "):
        continue
    toks = line[len("net recv: "):].split()
    if len(toks) < 18 or not all(re.fullmatch(r"[0-9a-f]{2}", t) for t in toks[:18]):
        continue
    if canary_hex in line:
        canary = True
    dst = toks[12:18]
    if dst == ["01", "00", "5e", "00", "00", "fb"]:
        v4_hit = True
    elif dst == ["33", "33", "00", "00", "00", "fb"]:
        v6_hit = True

# Strongest evidence first: a dumped canary frame proves host-originated
# multicast reached the guest; a group-MAC frame or a nonzero mdns=
# counter proves mDNS-group delivery; other multicast still answers the
# general delivery question; nothing at all is the honest "absent" the
# arc narrows on.
if v4_hit and v6_hit:
    verdict = "arrived-both"
elif v4_hit:
    verdict = "arrived-v4"
elif v6_hit:
    verdict = "arrived-v6"
elif mdns > 0:
    verdict = "arrived-mdns-counter"  # a group frame landed between dumps
elif mcast > 0 or mcast_filt > 0:
    verdict = "arrived-other-mcast"   # multicast delivered, mDNS group unseen
else:
    verdict = "absent"
report = ("%s mcast-probe: verdict=%s canary=%s frames=%d filtered=%d "
          "mcast=%d mdns=%d mcast-filt=%d ctrlvq=%d ctrlmac=%d ctrlrx=%d"
          % (tag, verdict, canary, frames, filtered, mcast, mdns,
             mcast_filt, int(bool(feats & (1 << 17))),
             int(bool(feats & (1 << 23))), int(bool(feats & (1 << 16)))))
print(report)
open(os.path.join(rd, tag + "-verdict.txt"), "w").write(report + "\n")
EOF

# m1 mirrors b1: s1 stays neutral while the bridged attachment settles
# (one `net` so the offered-bits record is in the log early), s2 waits
# out the observed VZ attach latency then takes a first recv snapshot,
# and s3 runs 8 s after m1-listen — the emitter's 1 s cadence lands
# several queries inside the window either way.
vgate_run m1 -- --net-bridged en0 --script '$RUN_DIR/m1.s1' --script2 '$RUN_DIR/m1.s2' --script2-after 'timer heartbeat ticks=45' --script3 '$RUN_DIR/m1.s3' --script3-after 'm1-listen' --script3-delay 8 --script-expect 'm1-ok' --timeout 120
vgate_allow_rc m1 0 1
vgate_assert m1 python <<'PY'
import os, subprocess, sys
rd = os.environ["RUN_DIR"]
out = open(os.path.join(rd, "run-" + os.environ["VG_TAG"] + ".out"),
           errors="replace").read()
if "requires the com.apple.vm.networking entitlement" in out:
    sys.exit(0)  # named refusal leg — the absent entitlement named itself
if "is not in VZBridgedNetworkInterface.networkInterfaces" in out:
    sys.exit(0)  # named refusal leg — en0 is not bridgeable on this host
ser_path = os.environ.get("VG_SER", "")
ser = open(ser_path, errors="replace").read() if ser_path and os.path.exists(ser_path) else ""
missing = [n for n in ("m1-listen", "m1-ok", "net: offered=") if n not in ser]
if missing:
    sys.exit("FAIL: probe mechanics did not run; missing: " + " | ".join(missing))
sys.exit(subprocess.run(
    [sys.executable, os.path.join(rd, "mcast_verdict.py")],
    env=dict(os.environ, RUN_DIR=rd, VG_TAG=os.environ["VG_TAG"],
             VG_SER=ser_path)).returncode)
PY

# m2: the vmnet twin — the locally-observable half of the bound on hosts
# whose ad-hoc signature makes en0 unbridgeable. The emitter waits for
# the 192.168.78.1 bridge iface the daemon lights once this run's vmnet
# exists, then emits the same canary + _http._tcp pair onto it. If VZ's
# virtio-net delivers group frames on a shared vmnet attachment, the
# guest's mdns=/canary evidence moves on THIS host — decomposing the
# arc's bound into "the device delivers" (answerable here) vs "the
# physical bridge forwards" (the dev-signed m1 half). v1's refusal
# branch (vmnet_return_t) and intermittency shape apply identically.
vgate_run m2 -- --net-vmnet --net-vmnet-subnet 192.168.78.0/24 --script '$RUN_DIR/m2.s1' --script2 '$RUN_DIR/m2.s2' --script2-after 'timer heartbeat ticks=60' --script3 '$RUN_DIR/m2.s3' --script3-after 'm2-listen' --script3-delay 8 --script-expect 'm2-ok' --timeout 120
vgate_allow_rc m2 0 1
vgate_assert m2 python <<'PY'
import os, subprocess, sys
rd = os.environ["RUN_DIR"]
out = open(os.path.join(rd, "run-" + os.environ["VG_TAG"] + ".out"),
           errors="replace").read()
if "failed (vmnet_return_t=" in out:
    sys.exit(0)  # named refusal leg — a non-success status carried by name
if "net-vmnet: ENABLED (M117d, issue #2476, ADR 0057 D11)" not in out:
    sys.exit("FAIL: success branch missing the net-vmnet ENABLED line")
ser_path = os.environ.get("VG_SER", "")
ser = open(ser_path, errors="replace").read() if ser_path and os.path.exists(ser_path) else ""
missing = [n for n in ("m2-listen", "m2-ok", "net: offered=") if n not in ser]
if missing:
    sys.exit("FAIL: probe mechanics did not run; missing: " + " | ".join(missing))
sys.exit(subprocess.run(
    [sys.executable, os.path.join(rd, "mcast_verdict.py")],
    env=dict(os.environ, RUN_DIR=rd, VG_TAG=os.environ["VG_TAG"],
             VG_SER=ser_path)).returncode)
PY
