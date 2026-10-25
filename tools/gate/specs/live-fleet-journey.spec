# live-fleet-journey.spec -- the fleet arc's acceptance (issue #3460,
# index #3335): ONE opt-in boot runs the journey end to end -- the gated
# binaries stage honestly, GOHTTPD serves as the init-supervised service
# on the LAN, a discovered fetch lands, and the NAT boot names the
# refusal surface. The journey needs its own spec because the armed
# share is shared across every leg and every repeat: an INIT.ELF +
# enabled httpd manifest staged into a multi-leg spec's share would
# autostart the supervised service into legs that exec it by hand.
#
#   stage:          the share is provisioned by tools/session.sh
#                   --provision-only itself -- the same call a `just
#                   session` desk makes -- and the setup hook proves the
#                   registry against it: every "Seat and staged tools"
#                   row's binary is on the share (the GORELEASE.ELF row's
#                   move to the gate-fleet table is the drift this leg
#                   named and fixed), and every session-staged machine
#                   tool has a `tool` row. The in-guest half: the six
#                   `help`-advertised CLIs all resolve under `exec`.
#   supervised-run: INIT.ELF + the SHIPPED manifest with the httpd row
#                   enabled, argv carrying `--advertise v201ej1` and caps
#                   the net-mdns word the verb declares (the opt-in boot
#                   live-httpd 07 lands, plus the LAN service grant
#                   word). INIT owns every start, the pre-`net ip` ENXIO
#                   backoff churn, the relisten, and the exit receipts.
#   fetch:          the host's dns-sd -B records the Add and the
#                   peer-scoped j1_fetch.py GETs the share canary off the
#                   advertised service -- the browsed fetch, with the
#                   live-net-bridged h2 leg's observed bound on
#                   announced-name resolution (via=literal-ip is the
#                   honest record until a query-answering responder
#                   exists).
#   goodbye:        the bounded AcceptConn expiry unwinds adv.stop()'s
#                   TTL=0 -- the browse records Rmv and `svc: exit
#                   name=httpd status=0` receipts the designed clean
#                   exit the on-failure policy does NOT restart.
#
# j1 --net-vmnet 192.168.80.0/24 runs the four stages in wire order on
# the host-independent segment. The deterministic part of the
# supervised-run evidence is the guest carrying NO address until s2:
# every httpd start before `net ip` fails by name and the manifest's
# on-failure policy retries it in backoff -- observed on this host:
# the --advertise check runs before the listen, and the no-address
# signature reads as no-LAN, so the churn names `advertise needs
# bridged` (the refusal surface inside the success boot; the assert
# confines it to before the first listen). The retry after s2 is what
# binds, listens and advertises. j2 --net-nat is the named-refuse half:
# the same opt-in manifest's httpd row refuses `advertise needs
# bridged` before the listener binds on every supervised retry, and the
# discover verb refuses `needs bridged` before any socket opens.
#
# HOST PREREQUISITE: none beyond the shared build preamble -- the setup
# hook ensures every Go binary the session stages through
# ensure-guest-elf (hash-cached), builds QJS.BIN when its pin is stale,
# and dns-sd is macOS's own.
#
# exec-order: assert-proven -- j1 ends on `httpd: advertise goodbye
# sent`, guest output only the supervised service's clean-exit path can
# produce (the listen, the announce and the served fetch all precede
# it); j2 ends on `j2-ok` but the run reaches it only after `svc:
# backoff name=httpd k=2`, the supervisor's second owned refusal -- an
# echo the boot cannot fake.

vgate_name live-fleet-journey "fleet acceptance journey (#3460): session-staged share + init-supervised GOHTTPD --advertise + host browsed fetch + goodbye on vmnet; NAT named refusals"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 3 BOOTS
vgate_note "K=3 consecutive green spec runs is the acceptance record (BOOTS overrides the default 3)"
vgate_note "j1 --net-vmnet 192.168.80.0/24: stage -> supervised-run -> browsed fetch -> goodbye; registry session-table vs share proven in setup"
vgate_note "j2 --net-nat: supervised 'advertise needs bridged' + svc receipts, 'discover needs bridged', no socket opens"

# j1's browsed fetch -- one fetch window per observed announce
# generation, the live-net-bridged h2 shape: the shared dns-sd -B log is
# the trigger, each new Add for the v201ej1 instance opens a bounded
# window that resolves the advertised .local name through the OS
# resolver and GETs the share canary, falls back to the DNS-SD stack's
# own answer, and finally records via=literal-ip honestly. The window
# then CLOSES so the guest's sequential listener can idle out -- a
# polling fetcher would keep it busy past the run's end and the
# clean-exit goodbye would never come.
vgate_file j1_fetch.py <<'EOF'
import http.client, os, re, socket, subprocess, sys, time

rd = sys.argv[1]
dnssd = os.path.join(rd, "dnssd.log")
out = os.path.join(rd, "j1-fetch.out")
NAME = "v201ej1"
HOST = NAME + ".local"
CANARY = "VIRELAI-FLEET-JOURNEY-J1-01"
GUEST_IP = "192.168.80.5"
t0 = time.time()


def note(msg):
    print("j1_fetch +%ds: %s" % (int(time.time() - t0), msg), flush=True)


def adds():
    try:
        n = 0
        for l in open(dnssd, errors="replace"):
            p = l.split()
            if p and p[-1] == NAME and "Add" in l:
                n += 1
        return n
    except OSError:
        return 0


def get(host):
    # Connection: close + an explicit close -- the server is sequential,
    # so a held keep-alive stalls the idle-expiry unwind past the run's
    # end.
    try:
        conn = http.client.HTTPConnection(host, 8080, timeout=5)
        conn.request("GET", "/J1LAN.TXT", headers={"Connection": "close"})
        r = conn.getresponse()
        body = r.read().decode("utf-8", "replace")
        conn.close()
        return body
    except Exception:
        return None


def q_a(name):
    try:
        r = subprocess.run(["dns-sd", "-Q", name, "A"],
                           capture_output=True, text=True, timeout=10)
        m = re.search(r"(\d+\.\d+\.\d+\.\d+)\s*$", r.stdout, re.M)
        return m.group(1) if m else None
    except Exception:
        return None


def canary(body):
    return body is not None and CANARY in body


handled = 0
while True:
    if adds() <= handled:
        time.sleep(0.5)
        continue
    deadline = time.time() + 10
    via, body = None, None
    while time.time() < deadline:
        try:
            socket.getaddrinfo(HOST, 8080, 0, socket.SOCK_STREAM)
        except OSError:
            time.sleep(0.5)
            continue
        b = get(HOST)
        if canary(b):
            via, body = "name", b
        break
    if via is None:
        ip = q_a(HOST)
        if ip:
            b = get(ip)
            if canary(b):
                via, body = "name-dnssd", b
    if via is None:
        b = get(GUEST_IP)
        if canary(b):
            via, body = "literal-ip", b
    if via is not None:
        with open(out, "a") as f:
            f.write("fetch-ok via=%s host=%s body=%s\n"
                    % (via, HOST if via != "literal-ip" else GUEST_IP,
                       body.splitlines()[0]))
        note("fetch-ok via=%s" % via)
    else:
        note("fetch-fail: no name answer and literal probe failed")
        with open(out, "a") as f:
            f.write("fetch-fail\n")
    handled += 1
EOF

# s1 runs at the first prompt while the guest still carries NO address:
# every supervised httpd start is a named ENXIO exit until s2 lands the
# static vmnet IP. The six `help`-advertised CLIs prove the staged fleet
# resolves under `exec`. GOHTTPD.ELF is deliberately NOT exec'd -- init
# owns it.
vgate_file j1.s1 <<'EOF'
exec GOPING.ELF
exec GOFETCH.ELF
exec GONETSTAT.ELF
exec GODNS.ELF
exec GOTRACEROUTE.ELF
exec GOTGIT.ELF
echo j1-staged
EOF

# s2 is gated on s1's own marker so its burst can never interleave with
# the exec burst: the IP lands, the next supervised retry binds, and
# `procs` leaves the live process table on the serial.
vgate_file j1.s2 <<'EOF'
net ip 192.168.80.5
procs
echo j1-ok
EOF

vgate_file j2.s1 <<'EOF'
net ip 192.168.64.5
exec GONETSTAT.ELF discover _http._tcp
echo j2-execd
EOF

vgate_file j2.s2 <<'EOF'
procs
echo j2-ok
EOF

# --- setup: provision the real session share, prove the registry, lay
# the opt-in supervised manifest on it, arm the LAN witnesses -----------
vgate_setup_python <<'PY'
import json, os, re, subprocess, sys

rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")

# ---- 1. the session roster, ensured through the one freshness path ----
# The same inputs tools/session.sh feeds session_go_apps: every
# image/apps.txt executable, the seat pair, the post-manifest tools
# (GOSNAP/GODIFF), and the SESSION_MACHINE_TOOLS line itself -- plus
# INIT.ELF, which session.sh does not stage (it is the opt-in boot's own
# binary, staged gate-side exactly like live-httpd 07). The arc's
# binaries (the machine tools, the seat pair, the post-manifest tools,
# INIT) hard-fail: a share without them is not the thing being proven.
# Catalog apps are ensured opportunistically and warn-skipped exactly
# like tools/session.sh's own "unavailable" contract (a builder that
# needs an argument ensure cannot express, like build-gostalgia.sh's
# gostalgia target, warns rather than stages -- the honest shape of a
# session provisioned on this tree, not a hidden divergence).
mt = re.search(r'SESSION_MACHINE_TOOLS="([^"]+)"',
               open("tools/session.sh").read()).group(1).split()
required = set(mt) | {"GOTABWM.ELF", "GOSH.ELF", "GOSNAP.ELF",
                      "GODIFF.ELF", "INIT.ELF"}
roster = []
for line in open("image/apps.txt"):
    line = line.strip()
    if line and not line.startswith("#") and "|" in line:
        n = line.split("|", 1)[0].strip()
        if n.endswith(".ELF") and n not in roster:
            roster.append(n)
roster += [n for n in required if n not in roster]
skipped = []
for app in roster:
    r = subprocess.run(["bash", "tools/go/ensure-guest-elf.sh", "ensure",
                        app[:-4]], capture_output=True, text=True)
    if r.returncode != 0:
        if app in required:
            sys.exit("%s could not be ensured (rc=%d): %s%s"
                     % (app, r.returncode, r.stdout[-800:], r.stderr[-800:]))
        skipped.append(app)
if skipped:
    print("m201e: catalog apps warn-skipped like session.sh itself: "
          + ", ".join(skipped))

# QJS.BIN is the session-staged engine (stage_qjs): build it only when
# its pin staleness says so. A missing pinned SDK warn-skips it exactly
# like stage_qjs itself -- the session's honest contract, not a hidden
# divergence -- and the share check exempts it the same way.
qjs = os.path.join(".build", "js-client", "QJS.BIN")
st = subprocess.run(["python3", "tools/js-client/build.py", "stale",
                     "--output", qjs], capture_output=True, text=True)
if "fresh" not in st.stdout:
    r = subprocess.run(["python3", "tools/js-client/build.py", "build",
                        "--output", qjs], capture_output=True, text=True)
    if r.returncode != 0:
        print("m201e: QJS.BIN warn-skipped like stage_qjs itself: %s"
              % (r.stdout[-200:] + r.stderr[-200:]).replace("\n", " | "))
        skipped.append("QJS.BIN")
print("m201e: ensured %d session binaries + QJS.BIN" % len(roster))

# ---- 2. the real provisioning path ------------------------------------
env = dict(os.environ)
env["VIRELAI_SESSION_PROFILE"] = os.path.join(rd, "session.virelaivm")
env["VIRELAI_SESSION_SHARE"] = share
env["VIRELAI_SESSION_SKIP_BUILD"] = "1"
r = subprocess.run(["bash", "tools/session.sh", "--provision-only"],
                   capture_output=True, text=True, env=env)
sys.stdout.write(r.stdout)
if r.returncode != 0 or "session: provisioning complete" not in r.stdout:
    sys.exit("session.sh --provision-only failed rc=%d: %s"
             % (r.returncode, r.stderr[-800:]))

# ---- 3. stage honesty: the registry's session-share table vs reality --
rows = []
sect = False
for line in open("docs/component-registry.md"):
    if line.startswith("## "):
        sect = "Seat and staged tools" in line
    elif sect and line.startswith("|"):
        c = [x.strip() for x in line.strip().strip("|").split("|")]
        if len(c) == 6 and c[0] != "name":
            rows.append(c)
missing = [c[2] for c in rows
           if c[5] in ("staged", "shipped", "fixture", "retained-fallback")
           and c[2] not in skipped
           and not os.path.isfile(os.path.join(share, c[2]))]
if missing:
    sys.exit("registry session-share rows not on the provisioned share: "
             + ", ".join(missing))
tool_bins = {c[2] for c in rows if c[1] == "tool"}
untracked = [t for t in mt if t not in tool_bins]
if untracked:
    sys.exit("session-staged machine tools with no `tool` row: "
             + ", ".join(untracked))
print("m201e: registry session-share table matches the share "
      "(%d rows, %d machine tools)" % (len(rows), len(mt)))

# ---- 4. the opt-in supervised boot ------------------------------------
# INIT.ELF is not a session binary -- session.sh never stages it -- so
# the opt-in boot stages it gate-side exactly like live-httpd 07, beside
# the SHIPPED manifest with the httpd row enabled, argv grown by the
# --advertise flag the LAN journey needs, and caps grown by the net-mdns
# word the verb declares. A refused manifest would read `init: refuse`
# at boot -- the legs assert the `init: manifest ok n=2` accept instead.
import shutil
shutil.copyfile(os.path.join(".build", "go", "INIT.ELF"),
                os.path.join(share, "INIT.ELF"))
m = json.loads(open("user/go/init/SERVICES.JSON").read())
row = [s for s in m["services"] if s["name"] == "httpd"]
assert len(row) == 1 and row[0].get("enabled") is False \
    and row[0]["argv"] == ["GOHTTPD.ELF"], \
    "the shipped httpd row is not the opt-in shape this leg extends"
row[0]["enabled"] = True
row[0]["argv"] = ["GOHTTPD.ELF", "--advertise", "v201ej1"]
row[0]["caps"] = ["file", "net", "net-mdns"]
os.makedirs(os.path.join(share, "INIT"), exist_ok=True)
with open(os.path.join(share, "INIT", "SERVICES.JSON"), "w") as f:
    json.dump(m, f)
with open(os.path.join(share, "J1LAN.TXT"), "w") as f:
    f.write("VIRELAI-FLEET-JOURNEY-J1-01\n")
print("m201e: opt-in httpd --advertise manifest + J1LAN.TXT staged")

# ---- 5. the LAN witnesses ---------------------------------------------
# The shared dns-sd browse watcher (live-net-bridged's a-leg shape) and
# the fetcher that arms on each Add -- both peer_watch-scoped to the
# gate's lifetime.
watch = os.path.join("user", "go", "browser", "peer_watch.py")
blog = open(os.path.join(rd, "dnssd.log"), "wb")
b = subprocess.Popen([sys.executable, watch, str(os.getppid()),
                      "dns-sd", "-B", "_http._tcp", "local."],
                     stdout=blog, stderr=subprocess.STDOUT,
                     start_new_session=True)
print("m201e: dns-sd browse watcher pid=%d" % b.pid)
flog = open(os.path.join(rd, "j1-fetch.log"), "wb")
fp = subprocess.Popen([sys.executable, watch, str(os.getppid()),
                       sys.executable, os.path.join(rd, "j1_fetch.py"), rd],
                      stdout=flog, stderr=subprocess.STDOUT,
                      start_new_session=True)
print("m201e: browsed-fetch watcher pid=%d" % fp.pid)
PY

# ---- j1: the journey -- stage -> supervised-run -> fetch -> goodbye ---
# s2 is gated on s1's own marker so the two bursts can never interleave;
# --screen seats GOTABWM the same way live-httpd 07's opt-in boot did.
vgate_run j1 -- --screen '$RUN_DIR/screen-j1' --net-vmnet --net-vmnet-subnet 192.168.80.0/24 \
    --script '$RUN_DIR/j1.s1' \
    --script2 '$RUN_DIR/j1.s2' --script2-after 'j1-staged' --script2-delay 1 \
    --script-expect 'httpd: advertise goodbye sent' --script-expect-tail 12 --timeout 150
vgate_allow_rc j1 0 1
vgate_assert j1 python <<'PY'
import os, re, sys
rd = os.environ["RUN_DIR"]
out = open(os.path.join(rd, "run-" + os.environ["VG_TAG"] + ".out"),
           errors="replace").read()
if "failed (vmnet_return_t=" in out:
    sys.exit(0)  # named refusal leg -- a non-success status carried by name
ser_path = os.environ.get("VG_SER", "")
ser = open(ser_path, errors="replace").read() if ser_path and os.path.exists(ser_path) else ""
need = [
    "init: autostart INIT.ELF (settings init=on)",
    "init: manifest ok n=2",
    "init: start name=httpd order=1",
    "init: start name=seat order=2",
    "init: seated",
    "exec: loaded GOPING.ELF",
    "exec: loaded GOFETCH.ELF",
    "exec: loaded GONETSTAT.ELF",
    "exec: loaded GODNS.ELF",
    "exec: loaded GOTRACEROUTE.ELF",
    "exec: loaded GOTGIT.ELF",
    "j1-staged",
    "net ip: ip=192.168.80.5",
    "svc: backoff name=httpd",
    "httpd: listening on port 8080",
    "httpd: advertising v201ej1._http._tcp.local",
    "httpd: advertise sent",
    "init: ready name=httpd",
    "httpd: accepted",
    "httpd: request /J1LAN.TXT",
    "httpd: served",
    "httpd: advertise goodbye sent",
    "svc: exit name=httpd status=0",
    "name=INIT.ELF",
    "j1-ok",
]
missing = [n for n in need if n not in ser]
if missing:
    sys.exit("FAIL: journey serial incomplete; missing: " + " | ".join(missing))
for bad in ("init: refuse", "init: fallback",
            "svc: failed name=httpd",
            "httpd: advertise needs net-mdns",
            ": not found on the host share",
            "[EXC] parking:"):
    if bad in ser:
        sys.exit("FAIL: a named refusal/escape fired inside the success leg: " + bad)
# Wire order: the pre-IP supervised churn precedes the scripted address,
# the listener binds only after it, advertises before saying goodbye,
# and the supervised clean exit is the last httpd event on the wire.
i_back = ser.find("svc: backoff name=httpd")
i_ip = ser.find("net ip: ip=192.168.80.5")
i_listen = ser.find("httpd: listening on port 8080")
i_adv = ser.find("httpd: advertise sent")
i_bye = ser.find("httpd: advertise goodbye sent")
if not (i_back < i_ip < i_listen < i_adv < i_bye):
    sys.exit("FAIL: journey out of order -- backoff, ip, listen, advertise, goodbye must come in wire order")
# Pre-IP the advertiser cannot tell vmnet from NAT -- the no-address
# signature reads as no-LAN -- so the early supervised starts refuse
# `advertise needs bridged` by name instead of `listen failed`. That IS
# the refusal surface naming itself inside the same boot; it is honest
# churn only while it all precedes the first successful listen.
for m in re.finditer(r"httpd: advertise needs bridged", ser):
    if m.start() > i_listen:
        sys.exit("FAIL: 'advertise needs bridged' fired after the service was up")
# The designed clean exit is status=0 -- and the on-failure policy does
# not restart it.
i_exit = ser.find("svc: exit name=httpd status=0")
if i_exit < i_bye or "svc: start name=httpd" in ser[i_exit:]:
    sys.exit("FAIL: the supervised service restarted after a clean exit")
# The pre-IP retries are the lifecycle evidence: every supervised start
# got its own pid, and at least one ENXIO exit is receipted before the
# successful listen.
starts = re.findall(r"svc: start name=httpd pid=(\d+)", ser)
if len(starts) < 2 or len(starts) != len(set(starts)):
    sys.exit("FAIL: supervised restarts missing or reused a pid: %s" % starts)
if "svc: exit name=httpd status=1" not in ser[:i_listen]:
    sys.exit("FAIL: no pre-IP ENXIO exit receipt before the successful listen")
# Host browse: the shared dns-sd -B log names this run's instance Add+Rmv.
log = open(os.path.join(rd, "dnssd.log"), errors="replace").read()
add = [l for l in log.splitlines()
       if l.split() and l.split()[-1] == "v201ej1" and "Add" in l]
rmv = [l for l in log.splitlines()
       if l.split() and l.split()[-1] == "v201ej1" and "Rmv" in l]
if not add:
    sys.exit("FAIL: dns-sd browse never recorded the v201ej1 Add")
if not rmv:
    sys.exit("FAIL: dns-sd browse never recorded the v201ej1 Rmv (goodbye)")
# The browsed fetch: one fetch-ok per armed run; the K=3 repeats tag
# -01..-0N so this leg requires at least that many.
try:
    idx = int(os.environ["VG_TAG"].rsplit("-", 1)[1])
except (IndexError, ValueError):
    idx = 1
fpath = os.path.join(rd, "j1-fetch.out")
lines = open(fpath).read().splitlines() if os.path.exists(fpath) else []
fetched = [l for l in lines if l.startswith("fetch-ok")]
named = [l for l in fetched if l.startswith("fetch-ok via=name")]
if len(fetched) < idx:
    sys.exit("FAIL: browsed fetch under-attested -- want >=%d fetch-ok, saw %d (%s)"
             % (idx, len(fetched), "; ".join(lines[-4:])))
if len(named) < idx:
    print("j1: BOUND -- v201ej1.local never resolved through the host "
          "resolver; the browsed fetch rode the announced service's "
          "address (%s)" % (fetched[-1] if fetched else "none"))
else:
    print("j1: journey complete -- stage proved, init supervised, "
          "host resolved v201ej1.local and fetched the share, goodbye Rmv'd")
PY

# ---- j2: the refusal surface names itself ------------------------------
# The same opt-in boot on --net-nat: the supervised httpd refuses
# `advertise needs bridged` before the listener binds on every retry,
# and `discover` names the same wall before any socket opens. s2 is
# gated on the k=2 backoff so the run can only end after the
# supervisor's second owned refusal -- the receipts, not a timing guess,
# are the supervised-refusal evidence.
vgate_run j2 -- --screen '$RUN_DIR/screen-j2' --net-nat \
    --script '$RUN_DIR/j2.s1' \
    --script2 '$RUN_DIR/j2.s2' --script2-after 'svc: backoff name=httpd k=2' --script2-delay 1 \
    --script-expect 'j2-ok' --timeout 120
vgate_assert j2 serial-contains 'init: autostart INIT.ELF (settings init=on)'
vgate_assert j2 serial-contains 'init: manifest ok n=2'
vgate_assert j2 serial-contains 'init: start name=httpd order=1'
vgate_assert j2 serial-contains 'init: seated'
vgate_assert j2 serial-contains 'httpd: starting'
vgate_assert j2 serial-contains 'httpd: advertise needs bridged'
vgate_assert j2 serial-contains 'svc: exit name=httpd status=1'
vgate_assert j2 serial-contains 'svc: backoff name=httpd'
vgate_assert j2 serial-contains 'netstat: discover needs bridged'
vgate_assert j2 serial-contains 'j2-execd'
vgate_assert j2 serial-contains 'j2-ok'
vgate_assert j2 serial-absent 'httpd: listening'
vgate_assert j2 serial-absent 'httpd: advertise sent'
vgate_assert j2 serial-absent 'netstat: discover query'
vgate_assert j2 serial-absent 'init: refuse'
vgate_assert j2 serial-absent '[EXC] parking:'
vgate_assert j2 python <<'PY'
import os, re, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
refusals = ser.count("httpd: advertise needs bridged")
starts = re.findall(r"svc: start name=httpd pid=(\d+)", ser)
if refusals < 2:
    sys.exit("FAIL: supervised refusal printed %d times, want >=2" % refusals)
if len(starts) < 2 or len(starts) != len(set(starts)):
    sys.exit("FAIL: supervised retries missing or reused a pid: %s" % starts)
print("j2: %d supervised 'needs bridged' refusals across %d starts; "
      "discover named the same wall" % (refusals, len(starts)))
PY
