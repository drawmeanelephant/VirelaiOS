# WEB.ELF bounded HTML/CSS/GET browser, plus GOFETCH TLS regression.
# One exec per boot, ended by program markers or an anchored kernel receipt.
# 01-11 retain render/link/fetch/error/cancel/store/offline/hostile behaviors.
# 12 proves in-process GOFETCH TLS; 13 retains shim console-ink evidence.
# 14 retains the pinned corpus on the surviving consumer.
# 15-17 type real hostnames against original hermetic site stand-ins.
# 18-20 prove wrong-host/expired/production-unknown-root refusal before GET.
# 21 measures the largest reference with actual kernel ownership counters.
# No public-internet fleet access, scripts or POST.
# Native font markers and approved exact presentation expectations are separate.
# Owner authorized the fixed three-pixel shim-border scanout composition.
# Prerequisites: build-web.sh browser WEB, browser WEB --gate,
# fetch GOFETCH --gate. Gate artifacts retain WEB.ELF/GOFETCH.ELF guest names.

vgate_name live-web "WEB: bounded CSS browser, exact M93 presentation, DNS/SNI/GET navigation and fail-closed TLS"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script-local.txt <<'EOF'
exec WEB.ELF /host/PAGE.HTML
EOF

vgate_file script-nav.txt <<'EOF'
exec WEB.ELF /host/PAGE.HTML
EOF

vgate_file script-fetch.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec WEB.ELF http://10.0.0.2/
EOF

vgate_file script-missing.txt <<'EOF'
exec WEB.ELF /host/NOPE.HTML
EOF

vgate_file script-https.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec WEB.ELF https://10.0.0.2:80/
EOF

vgate_file script-dns.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec WEB.ELF http://example.com/
EOF

vgate_file script-badurl.txt <<'EOF'
exec WEB.ELF http://
EOF

vgate_file script-hostile.txt <<'EOF'
exec WEB.ELF /host/HOSTILE.HTML
EOF

vgate_file script-store.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec WEB.ELF http://10.0.0.2/
EOF

vgate_file script-offline.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec WEB.ELF http://10.0.0.2/
EOF

vgate_file script-slow.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec WEB.ELF http://10.0.0.2/
EOF

vgate_file script-gofetch.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOFETCH.ELF https://10.0.0.2:24533/
EOF

vgate_file script-corpus.txt <<'EOF'
exec WEB.ELF /host/CORPUS.HTML
EOF

vgate_file script-m93-start.txt <<'EOF'
net ip 10.0.0.1
net arp 93.184.216.34
exec WEB.ELF --dns=93.184.216.34
EOF
vgate_file script-m93-name.txt <<'EOF'
net ip 10.0.0.1
net arp 93.184.216.34
exec WEB.ELF --dns=93.184.216.34 https://wrong.example.com:24561/
EOF
vgate_file script-m93-expired.txt <<'EOF'
net ip 10.0.0.1
net arp 93.184.216.34
exec WEB.ELF --dns=93.184.216.34 https://en.wikipedia.org:24562/
EOF
vgate_file script-m93-production.txt <<'EOF'
net ip 10.0.0.1
net arp 93.184.216.34
exec WEB-PROD.ELF --dns=93.184.216.34 https://en.wikipedia.org:24560/
EOF
vgate_file script-m93-largest.txt <<'EOF'
exec WEB.ELF /host/M93-LARGEST.HTML
EOF
vgate_file script-m93-receipt.txt <<'EOF'
procs receipt WEB.ELF
EOF

vgate_setup_python <<'PY'
# Boot 08 needs a peer that ACCEPTS the connection and never answers: the
# browser must sit in its waiting state so the injected cancel key has a load
# to cancel. A plain silent listener on the host, reached through the runner's
# relay responder, is exactly that peer.
import os, socket, sys, time
srv = socket.socket()
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
try:
    srv.bind(("127.0.0.1", 45871))
except OSError as exc:
    sys.exit("boot 08: cannot bind the silent peer on 127.0.0.1:45871: %s" % exc)
srv.listen(4)
srv.settimeout(1.0)
owner = os.getppid()
pid = os.fork()
if pid == 0:
    # The peer must not inherit the gate's stdio pipes: holding them open
    # keeps the runner's session alive after the gate finishes.
    devnull = os.open(os.devnull, os.O_RDWR)
    os.dup2(devnull, 0)
    os.dup2(devnull, 1)
    os.dup2(devnull, 2)
    conns = []
    deadline = time.time() + 300
    while time.time() < deadline:
        try:
            os.kill(owner, 0)
        except ProcessLookupError:
            break
        try:
            conn, _ = srv.accept()
            conns.append(conn)   # held open, never written to
        except OSError:
            pass
    os._exit(0)
print("boot 08: silent peer listening on 127.0.0.1:45871 (pid %d)" % pid)
PY

vgate_setup_python <<'PY'
import os, shutil, sys
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
src = os.path.join(".build", "go", "WEB-GATE.ELF")
if not os.path.exists(src):
    sys.exit("WEB.ELF missing (expected " + src + ") - build it first: "
             "bash tools/go/build-web.sh browser WEB --gate")
shutil.copy(src, os.path.join(share, "WEB.ELF"))
for src_name, dst_name in (("gate-page.html", "PAGE.HTML"), ("gate-next.html", "NEXT.HTML"),
                           ("hostile.html", "HOSTILE.HTML")):
    shutil.copy(os.path.join("user", "go", "browser", "testdata", src_name),
                os.path.join(share, dst_name))
# M71i (#1568): the M70d (#1456) fidelity-corpus page, inherited from the
# retired live-doc-web boot 05 ("one UA-table corpus page the guest can
# open"). It is a real page off the pinned corpus and the only thing that
# keeps that rung asserted IN the guest now that its Zig consumer is gone;
# the host-side golden tests (user/go/webrender/golden_test.go) pin all nine
# corpus pages' layout, but they never boot the VM.
corpus = os.path.join("user", "go", "webrender", "testdata", "corpus",
                      "cern-home.html")
if not os.path.exists(corpus):
    sys.exit("corpus fixture missing at " + corpus)
shutil.copy(corpus, os.path.join(share, "CORPUS.HTML"))
print("staged WEB.ELF (%d bytes) + PAGE.HTML/NEXT.HTML" %
      os.path.getsize(os.path.join(share, "WEB.ELF")))
gofetch = os.path.join(".build", "go", "GOFETCH-GATE.ELF")
if not os.path.exists(gofetch):
    sys.exit("GOFETCH.ELF missing (expected " + gofetch + ") - build it first: "
             "bash tools/go/build-web.sh fetch GOFETCH --gate")
shutil.copy(gofetch, os.path.join(share, "GOFETCH.ELF"))
print("staged GOFETCH.ELF (%d bytes)" % os.path.getsize(os.path.join(share, "GOFETCH.ELF")))
PY

vgate_setup_python <<'PY'
# Boot 12: TLS 1.3 responder on the same fixture identity the Go shelf
# vendors (leaf.example.com, AutoClaw test CA). Bound to loopback:24533 and
# reached through the runner's :relay -- the probe retired from live-tls13
# in M71k (#1570); this boot is its coverage. Long deadline because this
# spec has many boots before 12.
import os, subprocess, sys
rd = os.environ["RUN_DIR"]
cfx = os.path.join("user", "src", "lib", "tls", "vectors", "fx")
chain = os.path.join(cfx, "chain-ec.pem")
if not os.path.exists(chain):
    sys.exit("pinned fixture chain missing at %s" % chain)
cmd = [
    sys.executable,
    os.path.join("user", "src", "lib", "tls", "vectors", "tlsresponder.py"),
    "--host", "127.0.0.1", "--port", "24533",
    "--cert", chain, "--key", os.path.join(cfx, "leaf-ec.key"),
    "--body", "live-web-https-ok\n", "--accept", "2", "--timeout", "3600",
]
log = open(os.path.join(rd, "tlsresponder.log"), "wb")
watch = os.path.join("user", "go", "browser", "peer_watch.py")
proc = subprocess.Popen([sys.executable, watch, str(os.getppid())] + cmd,
                        stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
print("live-web 12: tlsresponder pid=%d on 127.0.0.1:24533" % proc.pid)
PY

vgate_setup_python <<'PY'
# Original stand-ins, real DNS names, separate loopback TLS peers. The runner's
# fixed A answer is 93.184.216.34; using it as the DNS peer too needs one ARP
# identity, not a host/SDK change. No public internet is reached.
import os, shutil, subprocess, sys
run = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(run, "share")
production = os.path.join(".build", "go", "WEB.ELF")
if not os.path.exists(production):
    sys.exit("build production first: bash tools/go/build-web.sh browser WEB")
shutil.copy(production, os.path.join(share, "WEB-PROD.ELF"))
reference = os.path.join("tests", "fixtures", "web", "reference")
references = [os.path.join(reference, name) for name in os.listdir(reference) if name.endswith(".html")]
largest_source = max(references, key=lambda path: os.path.getsize(path))
# reference_work_test.go inventories retained nodes + boxes + paint items.
# Wikipedia is the largest render-work reference, not the slightly larger
# source-only flex fixture whose layout emits a single paint item.
largest = os.path.join(reference, "wikipedia.html")
shutil.copy(largest, os.path.join(share, "M93-LARGEST.HTML"))
print("M93 largest retained-work reference:", largest, os.path.getsize(largest))
print("M93 largest source-only reference:", largest_source, os.path.getsize(largest_source))
fx = os.path.join(run, "m93-fx")
os.makedirs(fx, exist_ok=True)
base = os.path.join("user", "src", "lib", "tls", "vectors", "fx")
key = os.path.join(base, "leaf-ec.key")
csr, leaf, expired = (os.path.join(fx, n) for n in ("stand.csr", "stand.pem", "expired.pem"))
ext = os.path.join(fx, "stand.ext")
open(ext, "w").write(
    "basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\n"
    "extendedKeyUsage=serverAuth\n"
    "subjectAltName=DNS:en.wikipedia.org,DNS:github.com,DNS:developer.mozilla.org\n")
subprocess.check_call(["openssl", "req", "-new", "-key", key,
                      "-subj", "/CN=en.wikipedia.org", "-out", csr])
common = ["openssl", "x509", "-req", "-in", csr, "-CA", os.path.join(base, "inter.pem"),
          "-CAkey", os.path.join(base, "inter.key"), "-CAserial", os.path.join(fx, "serial"),
          "-CAcreateserial", "-extfile", ext]
subprocess.check_call(common + ["-days", "30", "-out", leaf])
subprocess.check_call(common + ["-not_before", "20200101000000Z",
                                "-not_after", "20200102000000Z", "-out", expired])
def chain(source, name):
    path = os.path.join(fx, name)
    open(path, "wb").write(open(source, "rb").read() +
                          open(os.path.join(base, "inter.pem"), "rb").read())
    return path
cert = chain(leaf, "stand-chain.pem")
expired_cert = chain(expired, "expired-chain.pem")
peer = os.path.join("user", "go", "browser", "standin_responder.py")
watch = os.path.join("user", "go", "browser", "peer_watch.py")
for port, certificate, negative in (
        (24560, cert, False), (24561, os.path.join(base, "chain-ec.pem"), True),
        (24562, expired_cert, True)):
    log = open(os.path.join(run, "m93-peer-%d.log" % port), "wb")
    command = [sys.executable, peer, "--port", str(port), "--cert", certificate,
               "--key", key, "--timeout", "3600"]
    if negative:
        command.append("--negative")
    proc = subprocess.Popen([sys.executable, watch, str(os.getppid())] + command,
                            stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    print("M93 original peer pid=%d port=%d" % (proc.pid, port))
PY

# --- boot 01: a local page renders (markers + pixels) --------------------
vgate_run 01 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-01' \
    --script '$RUN_DIR/script-local.txt' \
    --snapshot-after "web: settled" \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 120

vgate_assert 01 serial-contains 'web: open id='
vgate_assert 01 serial-contains 'web: parse nodes='
vgate_assert 01 serial-contains 'web: layout blocks='
vgate_assert 01 serial-contains 'web: url /host/PAGE.HTML'
vgate_assert 01 serial-contains 'web: paint items='
vgate_assert 01 serial-contains 'web: budget startup-ms='
vgate_assert 01 serial-contains ' wait-ms='
vgate_assert 01 serial-contains ' render-ms='
vgate_assert 01 serial-contains ' settle-ms='
vgate_assert 01 serial-absent 'web: budget over'
vgate_assert 01 serial-contains 'web: settled'
vgate_assert 01 serial-contains 'web: repaint items='
vgate_assert 01 serial-contains 'web: ready'
vgate_assert 01 serial-absent 'web: error'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 snapshot 'snap-01-*.raw' <<'PY'
import subprocess, sys
subprocess.check_call([sys.executable, 'user/go/browser/pixel_probe.py', sys.argv[1], 'page'])
PY

# --- boot 02: a pointer click on the in-page link replaces the page ------
vgate_run 02 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-02' \
    --script '$RUN_DIR/script-nav.txt' \
    --pointer-virtio "42,95,c" --pointer-virtio-after "web: settled" \
    --snapshot-after "web: settled" \
    --snapshot-after "web: navigated" \
    --script-expect "web: nav-ready" --timeout 120

# #1586: EVERY boot that launches WEB asserts the absence, not just the old
# 01/03/11. The guest printed `web: budget over startup ...` in all thirteen
# WEB boots for months while only three looked, which is why the red set
# looked load-driven. Measured then: startup=10048ms (7013ms of it vi.WmPeers
# retrying for a WM seat that does not exist on the shim path, 3022ms the four
# faces). After the fix: startup=3029-3054ms, so the absence is true everywhere
# and a real regression trips it in whichever boot it happens. Boot 12 is
# deliberately NOT asserted: it launches GOFETCH, not WEB, and prints no
# `web:` line at all (an absence there could only ever be vacuous).
vgate_assert 02 serial-absent 'web: budget over'
vgate_assert 02 serial-contains 'web: settled'
vgate_assert 02 serial-contains 'web: ev kind=3'
vgate_assert 02 serial-contains 'web: nav /host/NEXT.HTML'
vgate_assert 02 serial-contains 'web: navigated'
vgate_assert 02 serial-contains 'web: nav-ready'
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 snapshot 'snap-02-*.raw' <<'PY'
import subprocess, sys
subprocess.check_call([sys.executable, 'user/go/browser/pixel_probe.py', sys.argv[1], 'next'])
PY

# --- boot 03: HTTP fetch over the host TCP responder --------------------
vgate_run 03 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-03' \
    --net '$RUN_DIR/cap.bin' --net-arp-respond 10.0.0.2 --net-tcp-respond 10.0.0.2:80 \
    --script '$RUN_DIR/script-fetch.txt' \
    --snapshot-after "web: settled" \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 120

vgate_assert 03 serial-contains 'web: url http://10.0.0.2/'
vgate_assert 03 serial-contains 'web: parse nodes='
vgate_assert 03 serial-contains 'web: paint items='
vgate_assert 03 serial-contains 'web: settled'
vgate_assert 03 serial-contains 'web: repaint items='
vgate_assert 03 serial-contains 'web: ready'
vgate_assert 03 serial-contains 'web: budget startup-ms='
vgate_assert 03 serial-absent 'web: budget over'
vgate_assert 03 serial-absent 'web: error'
vgate_assert 03 serial-absent 'web: poll err='
vgate_assert 03 serial-absent '[EXC] parking:'
vgate_assert 03 output-contains "NET-TCP: answered the guest's HTTP request with 200 OK"
vgate_assert 03 snapshot 'snap-03-*.raw' <<'PY'
import subprocess, sys
subprocess.check_call([sys.executable, 'user/go/browser/pixel_probe.py', sys.argv[1], 'fetch'])
PY

# --- boot 04: a missing target renders an error page, still settles -----
vgate_run 04 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-04' \
    --script '$RUN_DIR/script-missing.txt' \
    --snapshot-after "web: settled" \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 120

vgate_assert 04 serial-absent 'web: budget over'
vgate_assert 04 serial-contains 'web: open id='
vgate_assert 04 serial-contains 'web: error file'
vgate_assert 04 serial-contains 'web: settled'
vgate_assert 04 serial-contains 'web: repaint items='
vgate_assert 04 serial-contains 'web: ready'
vgate_assert 04 serial-absent '[EXC] parking:'
vgate_assert 04 snapshot 'snap-04-*.raw' <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
w = 1280
X, Y = 40, 28
def px(x, y):
    k = (y * w + x) * 4
    return (data[k + 2], data[k + 1], data[k])
def ink(c):
    return c[0] > 200 and c[1] > 200 and c[2] > 200
n = sum(1 for yy in range(Y + 52, Y + 130)
        for xx in range(X + 10, X + 380)
        if ink(px(xx, yy)))
assert n >= 20, f"error page ink {n}"
print("live-web 04 error pixels ok")
PY

# --- boot 05: https is TLS, never a cleartext GET -------------------------
# The host TCP responder IS armed for 10.0.0.2:80, so a silent downgrade to
# plain http would print web: fetch and a NET-TCP 200. WEB.ELF instead
# tls.Dials :80; the handshake fails closed on the HTTP peer (not a hang),
# and the GET is never armed.
vgate_run 05 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-05' \
    --net '$RUN_DIR/cap5.bin' --net-arp-respond 10.0.0.2 --net-tcp-respond 10.0.0.2:80 \
    --script '$RUN_DIR/script-https.txt' \
    --snapshot-after "web: settled" \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 120

vgate_assert 05 serial-absent 'web: budget over'
vgate_assert 05 serial-contains 'web: url https://10.0.0.2:80/'
vgate_assert 05 serial-contains 'web: error tls'
vgate_assert 05 serial-contains 'web: ready'
vgate_assert 05 serial-absent 'web: fetch '
vgate_assert 05 serial-absent 'FETCHS.BIN'
vgate_assert 05 serial-absent '[EXC] parking:'
vgate_assert 05 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
assert "web: error tls" in ser, "https did not fail closed on TLS"
assert "web: fetch " not in ser, "a request was armed for an https URL"
assert "web: parse nodes=" not in ser, "an https URL produced a rendered page"
assert "GET / HTTP" not in ser, "a cleartext GET was logged"
assert "FETCHS.BIN" not in ser, "FETCHS.BIN was spawned"
print("live-web 05 https fail-closed ok (TLS handshake, no GET, no FETCHS)")
PY

# --- boot 06: a hostname is refused (no resolver), not attempted ---------
vgate_run 06 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-06' \
    --script '$RUN_DIR/script-dns.txt' \
    --snapshot-after "web: settled" \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 120

vgate_assert 06 serial-absent 'web: budget over'
vgate_assert 06 serial-contains 'web: error dns'
vgate_assert 06 serial-contains 'web: ready'
vgate_assert 06 serial-absent 'web: fetch '
vgate_assert 06 serial-absent '[EXC] parking:'

# --- boot 07: a malformed URL is a defined error, not a hang -------------
vgate_run 07 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-07' \
    --script '$RUN_DIR/script-badurl.txt' \
    --snapshot-after "web: settled" \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 120

vgate_assert 07 serial-absent 'web: budget over'
vgate_assert 07 serial-contains 'web: error url'
vgate_assert 07 serial-contains 'web: ready'
vgate_assert 07 serial-absent 'web: fetch '
vgate_assert 07 serial-absent '[EXC] parking:'

# --- boot 08: a load in flight can be stopped (cancel) -------------------
# The guest's request is relayed to the silent host peer (see the first setup
# hook), so the browser is genuinely waiting when the injected `x` arrives:
# the assert requires the cancel, not a fast connection failure.
vgate_run 08 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-08' \
    --net '$RUN_DIR/cap8.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:80:relay --net-tcp-respond-relay 127.0.0.1:45871 \
    --script '$RUN_DIR/script-slow.txt' \
    --input-chords x --input-chords-after "web: fetch " \
    --snapshot-after "web: settled" \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 180

vgate_assert 08 serial-absent 'web: budget over'
vgate_assert 08 serial-absent '[EXC] parking:'
vgate_assert 08 serial-contains 'web: fetch 10.0.0.2/'
vgate_assert 08 python <<'PY'
import os
ser = open(os.environ["VG_SER"], errors="replace").read()
assert "web: fetch 10.0.0.2/" in ser, "the load was never armed"
assert "web: error cancelled" in ser, "the injected cancel key did not stop the load"
assert "web: settled" in ser, "no frame was published after the cancel"
print("live-web 08 cancel ok (a load in flight was stopped by the injected cancel key)")
PY

# --- boot 09: the stores are written, and they land on the host share ----
# Fetch with the responder armed, then save the page. The python assert reads
# the share itself, so this is evidence of a persistent store, not of a
# marker the app printed about itself.
vgate_run 09 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-09' \
    --net '$RUN_DIR/cap9.bin' --net-arp-respond 10.0.0.2 --net-tcp-respond 10.0.0.2:80 \
    --script '$RUN_DIR/script-store.txt' \
    --input-chords s --input-chords-after "web: ready" \
    --snapshot-after "web: repaint" \
    --script-expect "web: download " --timeout 180

vgate_assert 09 serial-absent 'web: budget over'
vgate_assert 09 serial-contains 'web: stores '
vgate_assert 09 serial-contains 'web: cache store '
vgate_assert 09 serial-contains 'web: download '
vgate_assert 09 serial-absent '[EXC] parking:'
vgate_assert 09 python <<'PY'
import glob, os
share = os.environ["VG_SHARE"]
def rows(path):
    with open(path, errors="replace") as fh:
        return [l for l in fh.read().splitlines() if l and not l.startswith("#")]
hist = rows(os.path.join(share, "WEB-HISTORY.TXT"))
assert hist, "WEB-HISTORY.TXT has no data rows"
assert any("http://10.0.0.2/" in r for r in hist), "the visit was not recorded"
idx = rows(os.path.join(share, "WEB-CACHE.TXT"))
assert idx, "WEB-CACHE.TXT has no data rows"
bodies = glob.glob(os.path.join(share, "WEB-C-*.BIN"))
assert bodies, "no cached body file"
size = os.path.getsize(bodies[0])
dl = glob.glob(os.path.join(share, "WEB-DL-*"))
assert dl, "no download file"
dlsize = os.path.getsize(dl[0])
print("live-web 09 stores ok (history rows=%d, cache rows=%d, body=%dB, download=%s %dB)"
      % (len(hist), len(idx), size, os.path.basename(dl[0]), dlsize))
PY

# --- boot 10: the store survives a restart, and serves an offline copy ---
# A second, independent boot with NO responder: the connection cannot be
# answered, so the browser must fall back to what it stored in boot 09 and
# say so. The boot-time inventory is what proves the rows were read back
# from disk.
vgate_run 10 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-10' \
    --net '$RUN_DIR/cap10.bin' --net-arp-respond 10.0.0.2 \
    --script '$RUN_DIR/script-offline.txt' \
    --snapshot-after "web: settled" \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 180

vgate_assert 10 serial-absent 'web: budget over'
vgate_assert 10 serial-contains 'web: offline http://10.0.0.2/'
vgate_assert 10 serial-contains 'web: settled'
vgate_assert 10 serial-absent 'web: error tcp'
vgate_assert 10 serial-absent '[EXC] parking:'
vgate_assert 10 python <<'PY'
import os
ser = open(os.environ["VG_SER"], errors="replace").read()
line = ""
for ln in ser.splitlines():
    if ln.startswith("web: stores "):
        line = ln
        break
assert line, "no store inventory at boot"
fields = dict(f.split("=", 1) for f in line.split()[2:] if "=" in f)
assert int(fields.get("cache", 0)) >= 1, "the cache did not survive the restart: " + line
assert int(fields.get("history", 0)) >= 1, "history did not survive the restart: " + line
assert int(fields.get("downloads", 0)) >= 1, "downloads did not survive the restart: " + line
print("live-web 10 restart-persistence ok (%s)" % line.strip())
PY
vgate_assert 10 snapshot 'snap-10-*.raw' <<'PY'
import subprocess, sys
subprocess.check_call([sys.executable, 'user/go/browser/pixel_probe.py', sys.argv[1], 'fetch'])
PY

# --- boot 11: a hostile page is inert ------------------------------------
# The fixture carries a script whose body would fetch a URL and read a local
# file, an iframe/object/img pointing at the browser's own store files, and
# javascript:/file: links. This browser has no JavaScript and no way for an
# element to act on an attribute, so the page must render as text with no
# request armed, no file read, and no extra syscall path.
vgate_run 11 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-11' \
    --script '$RUN_DIR/script-hostile.txt' \
    --snapshot-after "web: settled" \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 120

vgate_assert 11 serial-contains 'web: parse nodes='
vgate_assert 11 serial-contains 'web: settled'
vgate_assert 11 serial-contains 'web: budget startup-ms='
vgate_assert 11 serial-contains ' wait-ms='
vgate_assert 11 serial-contains ' render-ms='
vgate_assert 11 serial-contains ' settle-ms='
vgate_assert 11 serial-absent 'web: fetch'
vgate_assert 11 serial-absent 'web: error'
vgate_assert 11 serial-absent 'web: download'
vgate_assert 11 serial-absent 'web: budget over'
vgate_assert 11 serial-absent '[EXC] parking:'
vgate_assert 11 snapshot 'snap-11-*.raw' <<'PY'
import subprocess, sys
subprocess.check_call([sys.executable, 'user/go/browser/pixel_probe.py', sys.argv[1], 'hostile'])
PY

# --- boot 12: GOFETCH.ELF https in-process (issue #1447) ------------------
# The host TLS 1.3 fixture peer (setup hook) is relayed at 10.0.0.2:24533.
# GOFETCH dials in-process via tls.Dial; FETCHS.BIN is not exec'd. The
# handshake and GET complete in this process (single goroutine).
# M71k (#1570): FETCHS.BIN no longer exists, so the serial-absent probe
# below can no longer be proven "by a binary that is not built" -- it is
# the identity the retired live-tls13 spec named.
vgate_run 12 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-12' \
    --net '$RUN_DIR/cap12.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:24533:relay --net-tcp-respond-relay 127.0.0.1:24533 \
    --script '$RUN_DIR/script-gofetch.txt' \
    --script-expect "gofetch: ready" --timeout 180

vgate_assert 12 serial-contains 'gofetch: open id='
vgate_assert 12 serial-contains 'gofetch: url https://10.0.0.2:24533/'
vgate_assert 12 serial-contains 'gofetch: dial 10.0.0.2 24533 leaf.example.com'
vgate_assert 12 serial-contains 'gofetch: handshake ok'
vgate_assert 12 serial-contains 'gofetch: request sent'
vgate_assert 12 serial-contains 'live-web-https-ok'
vgate_assert 12 serial-contains 'gofetch: body complete'
vgate_assert 12 serial-contains 'gofetch: ready'
vgate_assert 12 serial-absent 'FETCHS.BIN'
vgate_assert 12 serial-absent 'gofetch: helper'
vgate_assert 12 serial-absent 'gofetch: tcp'
vgate_assert 12 serial-absent 'gofetch: error'
vgate_assert 12 serial-absent 'web: fetch '
vgate_assert 12 serial-absent 'GET / HTTP'
vgate_assert 12 serial-absent 'fatal error: runtime: cannot allocate memory'
vgate_assert 12 serial-absent '[EXC] parking:'
vgate_assert 12 python <<'PY'
import os
ser = open(os.environ["VG_SER"], errors="replace").read()
assert "gofetch: handshake ok" in ser, "in-process TLS handshake did not complete"
assert "live-web-https-ok" in ser, "the fixture body was not read"
assert "FETCHS.BIN" not in ser, "FETCHS.BIN spawn marker present"
assert "gofetch: helper" not in ser, "the Zig TLS helper was exec'd"
assert "gofetch: tcp" not in ser, "Go opened a cleartext TCP socket"
assert "GET / HTTP" not in ser, "a cleartext GET was logged"
print("live-web 12 https in-process ok (GOFETCH tls.Dial, body, no FETCHS)")
PY

# --- boot 13: M69b console-ink re-measure in the web boot (#1592) ----------
# live-web 01-12 are shim-compositing (GOTABWM.ELF is not on the share;
# serial: "wm: autostart gotabwm: GOTABWM.ELF not on the share"). The
# M69b numbers came from this shape. Observed 2026-09-21 on this spec's
# first boot-13 run (macOS 27.2 / arm64, VZ): after web: settled, the
# band below WEB.ELF's 40,28 512x384 window held console-green
# 4747/82792 (5.734%) at +3 s and 4646/82792 (5.612%) at +20 s -- the
# M69b ~5.8% 20 s figure, already at plateau. Kernel fg_rgb = 0x00ff00.
# There is no seat, so paint_scene still blits the full-screen terminal
# and this probe pins that ink. A seated boot is go-wm-console-ink
# (#1561): paint_scene skips the terminal blit while the seat owns the
# layer (M71c) but the present must still flush. Skipping the drain
# outright leaves the pre-seat console frame (measured 1789 green pixels).
vgate_file script-ink-3s.txt <<'EOF'
echo web-ink-3s
EOF

vgate_file script-ink-20s.txt <<'EOF'
echo web-ink-20s
echo web-ink-done
EOF

vgate_run 13 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/ink' \
    --script '$RUN_DIR/script-local.txt' \
    --script2 '$RUN_DIR/script-ink-3s.txt' --script2-after "web: settled" --script2-delay 3 \
    --snapshot-after "web-ink-3s" \
    --script3 '$RUN_DIR/script-ink-20s.txt' --script3-after "web-ink-3s" --script3-delay 17 \
    --snapshot-after "web-ink-20s" \
    --script-expect "web-ink-done" --timeout 180

vgate_assert 13 serial-absent 'web: budget over'
vgate_assert 13 serial-contains 'web: settled'
vgate_assert 13 serial-contains 'web-ink-3s'
vgate_assert 13 serial-contains 'web-ink-20s'
vgate_assert 13 serial-contains 'web-ink-done'
vgate_assert 13 serial-absent '[EXC] parking:'
vgate_assert 13 snapshot 'ink-0.raw' <<'PY'
import os, sys
# First frame (3 s after settled). Shared sampler is below in the python
# assert so both frames are reported together; this check only proves the
# kind-4 stream wrote a 1280x720 BGRX scanout.
data = open(sys.argv[1], "rb").read()
assert len(data) == 1280 * 720 * 4, "3s scanout size %d" % len(data)
print("live-web 13 3s snapshot %d bytes" % len(data))
PY
vgate_assert 13 snapshot 'ink-1.raw' <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
assert len(data) == 1280 * 720 * 4, "20s scanout size %d" % len(data)
print("live-web 13 20s snapshot %d bytes" % len(data))
PY
vgate_assert 13 python <<'PY'
import os, sys

W, H = 1280, 720
# WEB.ELF window is 40,28 512x384; sample the band the window cannot cover.
Y0, STEP = 450, 2
PAGE_BG = (0x18, 0x20, 0x26)

def load(name):
    path = os.path.join(os.environ["RUN_DIR"], name)
    data = open(path, "rb").read()
    if len(data) != W * H * 4:
        sys.exit("%s size %d, want %d" % (name, len(data), W * H * 4))
    return data

def px(data, x, y):
    k = (y * W + x) * 4
    return data[k + 2], data[k + 1], data[k]

def near(c, want, tol=6):
    return all(abs(a - b) <= tol for a, b in zip(c, want))

def ink(rgb):
    return max(rgb) >= 100

def console_ink(rgb):
    r, g, b = rgb
    return g > 150 and r < 160 and b < 160

def measure(data, label):
    # Live-capture check: the WEB page fill at the declared window, or any
    # brighter ink there, so a blank/failed stream cannot pass.
    win = 0
    for yy in range(28 + 52, 28 + 200, STEP):
        for xx in range(40 + 10, 40 + 400, STEP):
            rgb = px(data, xx, yy)
            if near(rgb, PAGE_BG) or ink(rgb):
                win += 1
    tot = interior = green = 0
    for yy in range(Y0, H - 8, STEP):
        for xx in range(8, W - 8, STEP):
            rgb = px(data, xx, yy)
            tot += 1
            if ink(rgb):
                interior += 1
                if console_ink(rgb):
                    green += 1
    print("live-web 13 %s: window-page-bg=%d uncovered sampled=%d ink=%d (%.3f%%) console-green=%d (%.3f%%)"
          % (label, win, tot, interior, 100.0 * interior / tot if tot else 0.0,
             green, 100.0 * green / tot if tot else 0.0))
    if win < 200:
        sys.exit("FAIL: %s capture has no WEB window paint -- a blank scanout cannot prove the uncovered region" % label)
    return tot, interior, green

a = measure(load("ink-0.raw"), "3s")
b = measure(load("ink-1.raw"), "20s")
# Shim web boot: the full-screen kernel terminal is the desktop behind WEB.
# Pin the M69b ~5.8% console-green so a blank capture cannot pass and a
# seated paint cannot silently replace this measurement. The seated
# console-green=0 probe is go-wm-console-ink (#1561).
for label, m in (("3s", a), ("20s", b)):
    tot, interior, green = m
    pct = 100.0 * green / tot if tot else 0.0
    if green == 0:
        sys.exit("FAIL: %s uncovered band has 0 console-green -- this shim web boot previously measured ~5.8%% (M69b / #1592)" % label)
    if pct < 2.0 or pct > 12.0:
        sys.exit("FAIL: %s console-green %.3f%% outside the pinned shim band 2-12%% (M69b ~5.8%%)" % (label, pct))
print("live-web 13 console-ink: shim web boot still carries kernel console green in the uncovered band (M69b reproduced; seated probe is go-wm-console-ink)")
PY

# --- boot 14: M70d shard 1 corpus page, on the surviving consumer --------
# M71i (#1568): inherited from the retired live-doc-web boot 05. The fixture
# (CERN's first-website hub) is the corpus's own pinned bytes, and the page is
# readable from the compiled-in UA table alone -- no cascade, no script (ADR
# 0028 D2). The probe asserts a multi-paragraph real page, not a marker: the
# retired boot pinned 16 px of h1 ink, this one pins ink bands and text ink
# inside the client box so a page that failed to lay out cannot pass on its
# own `settled`.
vgate_run 14 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-14' \
    --script '$RUN_DIR/script-corpus.txt' \
    --snapshot-after "web: settled" \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 180

vgate_assert 14 serial-absent 'web: budget over'
vgate_assert 14 serial-contains 'web: url /host/CORPUS.HTML'
vgate_assert 14 serial-contains 'web: parse nodes='
vgate_assert 14 serial-contains 'web: layout blocks='
vgate_assert 14 serial-contains 'web: paint items='
vgate_assert 14 serial-contains 'web: settled'
vgate_assert 14 serial-contains 'web: ready'
vgate_assert 14 serial-absent 'web: error'
vgate_assert 14 serial-absent '[EXC] parking:'
vgate_assert 14 snapshot 'snap-14-*.raw' <<'PY'
import subprocess, sys
subprocess.check_call([sys.executable, 'user/go/browser/pixel_probe.py', sys.argv[1], 'corpus'])
PY

# 15: type Wikipedia, click its internal link, back/forward/back, then GET.
# The fixed 2.5 s pointer transport pacing includes inert toolbar moves while
# the translated URL strokes arrive; every transition is independently pinned
# by program nav markers, real peer GET lines and final pixels.
vgate_run 15 -- \
    --screen '$RUN_DIR/screen' --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-15' \
    --net '$RUN_DIR/cap15.bin' --net-arp-respond 93.184.216.34 \
    --net-dns-respond 93.184.216.34 \
    --net-tcp-respond 93.184.216.34:24560:relay --net-tcp-respond-relay 127.0.0.1:24560 \
    --script '$RUN_DIR/script-m93-start.txt' \
    --input-string $'https://en.wikipedia.org:24560/wiki/Harbor\n' --input-string-after 'web: url-focus' \
    --pointer-virtio '100,68,c;100,68;100,68;100,68;100,68;100,68;100,68;100,68;100,68;100,68;100,68;100,68;100,68;100,68;100,68;100,68;100,68;100,68;100,68;100,68;42,95,c;50,67,c;66,67,c;50,67,c;515,123,c' \
    --pointer-virtio-after 'web: ready' \
    --snapshot-after 'web: nav https://en.wikipedia.org:24560/wiki/Harbor' \
    --snapshot-after 'web: nav https://en.wikipedia.org:24560/w/index.php?search=Zig' \
    --script-expect 'web: nav-ready n=6' --timeout 240

vgate_assert 15 serial-contains 'web: url-focus'
vgate_assert 15 serial-contains 'web: dns en.wikipedia.org 93.184.216.34'
vgate_assert 15 serial-contains 'web: tls en.wikipedia.org virelai-gate-roots-2026-09-14'
vgate_assert 15 serial-contains 'web: nav https://en.wikipedia.org:24560/m93-next'
vgate_assert 15 serial-contains 'web: history back'
vgate_assert 15 serial-contains 'web: history forward'
vgate_assert 15 serial-contains 'web: form-get'
vgate_assert 15 serial-contains 'web: nav https://en.wikipedia.org:24560/w/index.php?search=Zig'
vgate_assert 15 serial-contains 'web: nav-ready n=6'
vgate_assert 15 serial-absent 'web: error'
vgate_assert 15 serial-absent 'web: budget over'
vgate_assert 15 serial-absent '[EXC] parking:'
vgate_assert 15 python <<'PY'
import os, shutil
path = os.path.join(os.environ["RUN_DIR"], "m93-peer-24560.log")
peer = open(path).read()
assert "SNI en.wikipedia.org" in peer
assert "request GET /wiki/Harbor HTTP/1.0" in peer
assert "request GET /m93.css HTTP/1.0" in peer
assert peer.count("request GET /m93-next HTTP/1.0") >= 2, "forward did not refetch the internal link"
assert peer.count("request GET /wiki/Harbor HTTP/1.0") >= 3, "back navigation did not restore the article"
assert "request GET /w/index.php?search=Zig HTTP/1.0" in peer, "successful controls were not sent exactly"
assert "request POST " not in peer
shutil.copyfile(path, os.path.join("artifacts", "m93f", "runs", "wiki-peer-evidence.log"))
print("M93f Wikipedia: DNS/SNI, typed URL, click, back/forward/back and exact GET proved")
PY
vgate_assert 15 snapshot 'snap-15-*.raw' <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
assert len(data) == 1280*720*4
pixels = [tuple(data[(y*1280+x)*4+i] for i in (2,1,0))
          for y in range(92,380) for x in range(40,552)]
assert sum(c == (0x18,0x20,0x26) for c in pixels) > 10000
assert sum(min(c) > 150 for c in pixels) > 20, "GET result page has no text"
print("M93f GET result scanout has page fill and text")
PY

# 16/17: independently type GitHub/MDN and follow one original internal link.
vgate_run 16 -- \
    --screen '$RUN_DIR/screen' --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-16' \
    --net '$RUN_DIR/cap16.bin' --net-arp-respond 93.184.216.34 --net-dns-respond 93.184.216.34 \
    --net-tcp-respond 93.184.216.34:24560:relay --net-tcp-respond-relay 127.0.0.1:24560 \
    --script '$RUN_DIR/script-m93-start.txt' \
    --input-chords ctrl-l --input-chords-after 'web: ready' \
    --input-string $'https://github.com:24560/mattn/go-runewidth\n' --input-string-after 'web: url-focus' \
    --pointer-virtio '42,95,c' --pointer-virtio-after 'web: nav https://github.com:24560/mattn/go-runewidth' \
    --snapshot-after 'web: nav https://github.com:24560/m93-next' \
    --script-expect 'web: nav-ready n=2' --timeout 240

vgate_assert 16 serial-contains 'web: dns github.com 93.184.216.34'
vgate_assert 16 serial-contains 'web: tls github.com virelai-gate-roots-2026-09-14'
vgate_assert 16 serial-contains 'web: nav https://github.com:24560/m93-next'
vgate_assert 16 serial-contains 'web: nav-ready n=2'
vgate_assert 16 serial-absent 'web: error'
vgate_assert 16 serial-absent 'web: budget over'
vgate_assert 16 serial-absent '[EXC] parking:'
vgate_assert 16 python <<'PY'
import os
peer = open(os.path.join(os.environ["RUN_DIR"], "m93-peer-24560.log")).read()
assert "SNI github.com" in peer
assert "request GET /mattn/go-runewidth HTTP/1.0" in peer
assert "request GET /m93-next HTTP/1.0" in peer
print("M93f GitHub stand-in GET/SNI and internal link proved")
PY
vgate_assert 16 snapshot 'snap-16-*.raw' <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
pixels = [tuple(data[(y*1280+x)*4+i] for i in (2,1,0))
          for y in range(92,160) for x in range(40,552)]
assert sum(min(c) > 150 for c in pixels) > 20, "internal page has no text"
print("M93f GitHub internal page scanout has text")
PY

vgate_run 17 -- \
    --screen '$RUN_DIR/screen' --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-17' \
    --net '$RUN_DIR/cap17.bin' --net-arp-respond 93.184.216.34 --net-dns-respond 93.184.216.34 \
    --net-tcp-respond 93.184.216.34:24560:relay --net-tcp-respond-relay 127.0.0.1:24560 \
    --script '$RUN_DIR/script-m93-start.txt' \
    --input-chords ctrl-l --input-chords-after 'web: ready' \
    --input-string $'https://developer.mozilla.org:24560/en-US/docs/Web/CSS/display\n' --input-string-after 'web: url-focus' \
    --pointer-virtio '42,95,c' --pointer-virtio-after 'web: nav https://developer.mozilla.org:24560/en-US/docs/Web/CSS/display' \
    --snapshot-after 'web: nav https://developer.mozilla.org:24560/m93-next' \
    --script-expect 'web: nav-ready n=2' --timeout 240

vgate_assert 17 serial-contains 'web: dns developer.mozilla.org 93.184.216.34'
vgate_assert 17 serial-contains 'web: tls developer.mozilla.org virelai-gate-roots-2026-09-14'
vgate_assert 17 serial-contains 'web: nav https://developer.mozilla.org:24560/m93-next'
vgate_assert 17 serial-contains 'web: nav-ready n=2'
vgate_assert 17 serial-absent 'web: error'
vgate_assert 17 serial-absent 'web: budget over'
vgate_assert 17 serial-absent '[EXC] parking:'
vgate_assert 17 python <<'PY'
import os
peer = open(os.path.join(os.environ["RUN_DIR"], "m93-peer-24560.log")).read()
assert "SNI developer.mozilla.org" in peer
assert "request GET /en-US/docs/Web/CSS/display HTTP/1.0" in peer
assert "request GET /m93-next HTTP/1.0" in peer
print("M93f MDN stand-in GET/SNI and internal link proved")
PY
vgate_assert 17 snapshot 'snap-17-*.raw' <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
pixels = [tuple(data[(y*1280+x)*4+i] for i in (2,1,0))
          for y in range(92,160) for x in range(40,552)]
assert sum(min(c) > 150 for c in pixels) > 20, "internal page has no text"
print("M93f MDN internal page scanout has text")
PY

# 18: name mismatch. Reverting the browser verification/SNI name to the old
# leaf.example.com incorrectly passes this peer and makes this negative red.
vgate_run 18 -- \
    --screen '$RUN_DIR/screen' --via-virtio --cvc-snap \
    --net '$RUN_DIR/cap18.bin' --net-arp-respond 93.184.216.34 --net-dns-respond 93.184.216.34 \
    --net-tcp-respond 93.184.216.34:24561:relay --net-tcp-respond-relay 127.0.0.1:24561 \
    --script '$RUN_DIR/script-m93-name.txt' --script-expect 'web: ready' --timeout 180

vgate_assert 18 serial-contains 'web: error tls-hostname-mismatch'
vgate_assert 18 serial-contains 'web: ready'
vgate_assert 18 serial-absent 'web: fetch '
vgate_assert 18 serial-absent 'web: tls '
vgate_assert 18 serial-absent 'web: budget over'
vgate_assert 18 serial-absent '[EXC] parking:'
vgate_assert 18 python <<'PY'
import os
peer = open(os.path.join(os.environ["RUN_DIR"], "m93-peer-24561.log")).read()
assert "SNI wrong.example.com" in peer
assert "m93f-peer: request " not in peer, "wrong-host request escaped validation"
print("M93f wrong-host browser refuses before GET")
PY

vgate_run 19 -- \
    --screen '$RUN_DIR/screen' --via-virtio --cvc-snap \
    --net '$RUN_DIR/cap19.bin' --net-arp-respond 93.184.216.34 --net-dns-respond 93.184.216.34 \
    --net-tcp-respond 93.184.216.34:24562:relay --net-tcp-respond-relay 127.0.0.1:24562 \
    --script '$RUN_DIR/script-m93-expired.txt' --script-expect 'web: ready' --timeout 180

vgate_assert 19 serial-contains 'web: error tls-expired'
vgate_assert 19 serial-absent 'web: fetch '
vgate_assert 19 serial-absent 'web: tls '
vgate_assert 19 serial-absent 'web: budget over'
vgate_assert 19 serial-absent '[EXC] parking:'

# 20: the real production build must reject the very CA that gate builds trust.
vgate_run 20 -- \
    --screen '$RUN_DIR/screen' --via-virtio --cvc-snap \
    --net '$RUN_DIR/cap20.bin' --net-arp-respond 93.184.216.34 --net-dns-respond 93.184.216.34 \
    --net-tcp-respond 93.184.216.34:24560:relay --net-tcp-respond-relay 127.0.0.1:24560 \
    --script '$RUN_DIR/script-m93-production.txt' --script-expect 'web: ready' --timeout 180

vgate_assert 20 serial-contains 'tls: roots virelai-nss-15-940706e6f856'
vgate_assert 20 serial-contains 'web: error tls-unknown-root'
vgate_assert 20 serial-absent 'web: fetch '
vgate_assert 20 serial-absent 'web: tls '
vgate_assert 20 serial-absent 'web: budget over'
vgate_assert 20 serial-absent '[EXC] parking:'

# 21: cold largest retained-work reference and actual kernel ownership receipt.
# Receipt sampling is anchored after the program's published frame, not an
# estimated heap counter. The script's receipt text is independently asserted.
vgate_run 21 -- \
    --screen '$RUN_DIR/screen' --via-virtio --cvc-snap \
    --script '$RUN_DIR/script-m93-largest.txt' \
    --script2 '$RUN_DIR/script-m93-receipt.txt' --script2-after 'web: ready' \
    --script-expect 'record_failures=0 unrecorded_pages=0 reaped=0' --timeout 180

vgate_assert 21 serial-contains 'web: url /host/M93-LARGEST.HTML'
vgate_assert 21 serial-contains 'web: settled'
vgate_assert 21 serial-contains 'web: ready'
vgate_assert 21 serial-contains 'runtime-receipt: pid='
vgate_assert 21 serial-absent 'web: budget over'
vgate_assert 21 serial-absent 'web: error'
vgate_assert 21 serial-absent '[EXC] parking:'
vgate_assert 21 python <<'PY'
import os, re
serial = open(os.environ["VG_SER"], errors="replace").read()
receipts = [line for line in serial.splitlines()
            if line.startswith("runtime-receipt: pid=") and " name=WEB.ELF " in line]
assert receipts, "no intact WEB ownership receipt"
fields = dict(re.findall(r"(\w+)=([^\s]+)", receipts[-1]))
assert int(fields["peak_pages"]) <= 3072, receipts[-1]
assert int(fields["peak_regions"]) <= 12, receipts[-1]
assert int(fields["record_failures"]) == 0, receipts[-1]
budgets = [line for line in serial.splitlines() if line.startswith("web: budget ")]
assert budgets
times = dict(re.findall(r"([\w-]+)=(\d+)", budgets[-1]))
assert int(times["layout-ms"])+int(times["paint-ms"]) <= 500, budgets[-1]
print("M93 largest reference actual demand/region/render ceilings proved:", receipts[-1])
PY
