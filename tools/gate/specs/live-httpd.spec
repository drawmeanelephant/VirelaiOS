# live-httpd.spec -- GOHTTPD.ELF (exec'd) serves HTTP/1.1 on port 8080.
#
# M71l (#1571) proved the passive open only -- the monitor showed
# tcp=listen while the one-shot responder lived. Second pass (#2200):
# GOHTTPD is a persistent server on the slot-80 handle listener
# (vi.SocketListen), so the gate now drives real exchanges with the
# runner's --net-tcp-connect host clients:
#
#   01 passive listen (kept leg)
#   02 two clients on one listener without a restart
#   03 keep-alive: three requests on ONE connection (GET, GET 404, HEAD)
#   04 refusal surface: traversal 404, POST 405, garbage 400
#   05 HTTPS: --tls on 8443, host TLS 1.3 client over the
#      --net-tcp-connect-stream relay, cert DER + body checked (M135a)
#   06 negative: a TLS 1.2-only client gets the protocol_version alert
#      in the serial log
#   07 M201c: the shipped manifest's httpd row enabled — INIT.ELF owns
#      the lifecycle (order-1 start, kill 137 -> backoff -> supervisor
#      relisten -> a second client served)
#   08 opt-out: the shipped manifest as-is (httpd enabled=false) boots
#      to the seat with no svc: start for it
#   09 M209a: --admin over TLS with NO httpd-admin store row -> the
#      section does not exist (404); the run then stages SECRETS.TXT
#   10 M209a: --admin over TLS with the row: one connection carries the
#      whole suite — procs 200 (GOHTTPD.ELF in the table), grants 200
#      (rows or the named unknown), wrong creds 401, GET /SECRETS.TXT
#      404 while the file is armed (the card's first check, pinned),
#      unknown admin path 404, index 200
#   11 M209a: --admin over plaintext -> 403 'admin requires tls'
#
# The kernel's child_limit=2 bounds simultaneous children and every
# --net-tcp-connect client fires on its own dispatch queue at the same
# marker, so a literal third SEQUENTIAL connect in one boot is not
# expressible: run 02 instead proves the accept loop iterates -- one
# client's session completes and the pending child is served after it.
# Runs 02/03/04 re-accept five sessions total on one listener, and the
# payload clients FIN on the response so client-initiated close is
# covered too. GOHTTPD.ELF is staged into the share by the setup hook
# along with the HTTPD.TXT fixture the file route serves.
#
# HOST PREREQUISITE (fails honestly when missing):
#   bash tools/go/build-gohttpd.sh  ->  .build/go/GOHTTPD.ELF

vgate_name live-httpd "GOHTTPD.ELF persistent HTTP/1.1 on 8080 (sequential, keep-alive, refusals)"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_fmt kernel/src/tcp.zig kernel/src/syscall.zig kernel/src/monitor.zig user/src/lib/ui.zig build.zig

vgate_file script-1.txt <<'EOF'
net ip 10.0.0.1
exec GOHTTPD.ELF
echo httpd-launched
EOF

vgate_file script-2.txt <<'EOF'
procs
net
echo httpd-ok
EOF

vgate_file script-3.txt <<'EOF'
procs
kill GOHTTPD.ELF
echo httpd-killed
EOF

# run 02 payloads: one request each, explicit Connection: close.
vgate_file req-index.txt <<'EOF'
GET / HTTP/1.1
Connection: close

EOF

vgate_file req-file.txt <<'EOF'
GET /HTTPD.TXT HTTP/1.1
Connection: close

EOF

# run 03 payload: three pipelined requests on one keep-alive connection.
vgate_file req-keepalive.txt <<'EOF'
GET / HTTP/1.1

GET /NOPE.TXT HTTP/1.1

HEAD /healthz HTTP/1.1
Connection: close

EOF

# run 04 payloads: a traversal probe (404, keep-alive survives it) then
# POST (405, closes), and a second client whose bytes are not HTTP (400).
vgate_file req-errors.txt <<'EOF'
GET /../SECRETS.TXT HTTP/1.1

POST / HTTP/1.1

EOF

vgate_file req-garbage.txt <<'EOF'
HELLO WORLD

EOF

# run 05/06: the TLS-over-stdio client. ssl.MemoryBIO drives TLS 1.3 over the
# runner's byte relay; stderr is the evidence channel (into the run log).
# argv: CERT_DER_PATH [tls12]
vgate_file tlscli.py <<'EOF'
#!/usr/bin/env python3
import os, ssl, sys

cert_path = sys.argv[1]
tls12 = len(sys.argv) > 2 and sys.argv[2] == "tls12"

ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE
if tls12:
    ctx.minimum_version = ssl.TLSVersion.TLSv1_2
    ctx.maximum_version = ssl.TLSVersion.TLSv1_2
else:
    ctx.minimum_version = ssl.TLSVersion.TLSv1_3
    ctx.maximum_version = ssl.TLSVersion.TLSv1_3

inbio = ssl.MemoryBIO()
outbio = ssl.MemoryBIO()
so = ctx.wrap_bio(inbio, outbio, server_side=False)

def flush():
    while True:
        b = outbio.read()
        if not b:
            return
        os.write(1, b)

# Handshake loop: flush outgoing BIO, feed incoming stdin. An SSLError means
# the peer's alert arrived — print it for the log and stop.
ok = False
while True:
    try:
        so.do_handshake()
        ok = True
        break
    except ssl.SSLWantReadError:
        flush()
        block = os.read(0, 65536)
        if not block:
            break
        inbio.write(block)
    except ssl.SSLWantWriteError:
        flush()
    except ssl.SSLError as e:
        sys.stderr.write("TLSCLI: handshake ssl error %r\n" % (e,))
        break
    flush()

if not ok:
    sys.exit(0 if tls12 else 2)

peer = so.getpeercert(binary_form=True)
want = open(cert_path, "rb").read()
sys.stderr.write("TLSCLI: handshake ok, peer cert %d bytes, fixture match=%s\n"
                 % (len(peer or b""), peer == want))
so.write(b"GET / HTTP/1.1\r\nConnection: close\r\n\r\n")
flush()
body = b""
while True:
    try:
        d = so.read(4096)
        if not d:
            break
        body += d
    except ssl.SSLWantReadError:
        pass
    except ssl.SSLError as e:
        sys.stderr.write("TLSCLI: read err %r\n" % (e,))
        break
    block = os.read(0, 65536)
    if not block:
        break
    inbio.write(block)
sys.stderr.write("TLSCLI: body=%r\n" % body)
if b"VirelaiOS" not in body:
    sys.exit(3)
sys.exit(0)
EOF

# script-1.txt's `net ip` matters here too: SocketListen refuses ENXIO
# until the guest carries an IP (leg 07's comment names the rule), and
# without this line the legs raced autonomous DHCP — observed failing as
# 'httpd: listen failed' under a loaded host (2026-10-27).
vgate_file script-tls.txt <<'EOF'
net ip 10.0.0.1
exec GOHTTPD.ELF --tls /host/TLS.CRT /host/TLS.SEED 8443
echo tls-launched
EOF

vgate_file script-tls-kill.txt <<'EOF'
procs
kill GOHTTPD.ELF
echo tls-killed
EOF

vgate_setup_python <<'PY'
import os, shutil, sys

run = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(run, "share")
os.makedirs(share, exist_ok=True)

src = os.path.join(".build", "go", "GOHTTPD.ELF")
if not os.path.exists(src):
    sys.exit("GOHTTPD.ELF missing (expected " + src + ") - build it first: "
             "bash tools/go/build-gohttpd.sh")
dst = os.path.join(share, "GOHTTPD.ELF")
shutil.copy(src, dst)
print("live-httpd: staged GOHTTPD.ELF (%d bytes)" % os.path.getsize(dst))
with open(os.path.join(share, "HTTPD.TXT"), "w") as f:
    f.write("VIRELAI-HTTPD-FIXTURE-01\n")
print("live-httpd: staged HTTPD.TXT fixture")

# The TLS legs' credential: the env-gated host generator writes TLS.CRT
# (DER, self-signed Ed25519, CA:TRUE + serverAuth + SANs for 10.0.0.1 /
# virelai-https.local) and TLS.SEED (raw 32-byte Ed25519 seed) into the
# share.
env = dict(os.environ)
env["HTTPD_TLS_GEN_FIXTURE"] = "1"
env["HTTPD_TLS_FIXTURE_DIR"] = share
import subprocess
# Setup hooks run with the repo root as cwd (vgate.sh cd's there); __file__
# is $RUN_DIR/setup-N.py and says nothing about the checkout.
repo = os.getcwd()
r = subprocess.run(["go", "-C", "user/go", "test", "-run", "TestGenTLSFixture",
                    "-v", "./httpd/"], cwd=repo, env=env,
                   capture_output=True, text=True)
sys.stderr.write(r.stdout[-800:] + r.stderr[-800:])
if r.returncode != 0:
    sys.exit("fixture generator failed")
for name in ("TLS.CRT", "TLS.SEED"):
    p = os.path.join(share, name)
    if not os.path.exists(p):
        sys.exit("fixture missing " + p)
print("live-httpd: generated TLS.CRT + TLS.SEED in share")

# The stream client verifies the peer's DER against the same fixture; the
# share path varies ($VG_SHARE), so it reads a RUN_DIR copy.
shutil.copy(os.path.join(share, "TLS.CRT"), os.path.join(run, "TLS.CRT"))

import stat
os.chmod(os.path.join(run, "tlscli.py"), 0o755)
os.chmod(os.path.join(run, "admincli.py"), 0o755)
PY

# ---- 01: passive open (the kept M71l leg) --------------------------------
vgate_run 01 -- --net '$RUN_DIR/cap.bin' --script '$RUN_DIR/script-1.txt' --script2 '$RUN_DIR/script-2.txt' --script2-after 'httpd: listening on port 8080' --script-expect 'httpd-ok' --timeout 60

vgate_assert 01 serial-contains 'net ip: ip=10.0.0.1'
vgate_assert 01 serial-contains 'httpd: starting'
vgate_assert 01 serial-contains 'httpd: listening on port 8080'
vgate_assert 01 serial-contains 'echo httpd-launched'
vgate_assert 01 serial-contains 'GOHTTPD.ELF'
vgate_assert 01 serial-contains 'echo httpd-ok'

# ---- 02: two sequential clients, then kill mid-listen --------------------
vgate_run 02 -- --net '$RUN_DIR/cap2.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect '10.0.0.1:8080:$RUN_DIR/req-index.txt' \
    --net-tcp-connect '10.0.0.1:8080:$RUN_DIR/req-file.txt' \
    --net-tcp-connect-after 'httpd: listening on port 8080' \
    --script '$RUN_DIR/script-1.txt' \
    --script2 '$RUN_DIR/script-3.txt' --script2-after 'httpd: closed' --script2-delay 4 \
    --script-expect 'httpd-killed' --timeout 90

vgate_assert 02 serial-contains 'httpd: listening on port 8080'
vgate_assert 02 serial-contains 'kill: GOHTTPD.ELF armed'
vgate_assert 02 serial-contains 'httpd: request /'
vgate_assert 02 serial-contains 'httpd: request /HTTPD.TXT'
vgate_assert 02 output-contains 'HTTP/1.1 200 OK'
vgate_assert 02 output-contains 'VIRELAI-HTTPD-FIXTURE-01'
vgate_assert 02 output-contains 'Content-Type: text/plain; charset=utf-8'
vgate_assert 02 output-contains 'Connection: close'
vgate_assert 02 serial-absent 'httpd: error'
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
for marker, want in (("httpd: accepted", 2), ("httpd: served", 2), ("httpd: closed", 2)):
    n = ser.count(marker)
    if n != want:
        sys.exit("FAIL: %r appeared %d times, want %d" % (marker, n, want))
# Exact lines: 'httpd: request /' is a substring of the HTTPD.TXT line,
# so count only full-line matches.
lines = [l.strip() for l in ser.splitlines()]
for marker, want in (("httpd: request /", 1), ("httpd: request /HTTPD.TXT", 1)):
    n = lines.count(marker)
    if n != want:
        sys.exit("FAIL: %r appeared %d times, want %d" % (marker, n, want))
print("two sequential clients served on one listener without restart")
PY

# ---- 03: keep-alive -- three requests on ONE connection -------------------
vgate_run 03 -- --net '$RUN_DIR/cap3.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect '10.0.0.1:8080:$RUN_DIR/req-keepalive.txt' \
    --net-tcp-connect-after 'httpd: listening on port 8080' \
    --net-tcp-connect-close-after 'Connection: close' \
    --script '$RUN_DIR/script-1.txt' \
    --script2 '$RUN_DIR/script-3.txt' --script2-after 'httpd: closed' --script2-delay 2 \
    --script-expect 'httpd-killed' --timeout 90

vgate_assert 03 serial-contains 'kill: GOHTTPD.ELF armed'
vgate_assert 03 serial-contains 'httpd: request /'
vgate_assert 03 serial-contains 'httpd: request /NOPE.TXT'
vgate_assert 03 serial-contains 'httpd: request /healthz'
vgate_assert 03 output-contains 'HTTP/1.1 200 OK'
vgate_assert 03 output-contains 'HTTP/1.1 404 Not Found'
vgate_assert 03 output-contains 'Connection: keep-alive'
vgate_assert 03 output-contains 'Connection: close'
vgate_assert 03 serial-absent 'httpd: error'
vgate_assert 03 serial-absent '[EXC] parking:'
vgate_assert 03 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
if ser.count("httpd: accepted") != 1:
    sys.exit("keep-alive leg took %d connections, want exactly 1" % ser.count("httpd: accepted"))
if ser.count("httpd: served") != 3:
    sys.exit("keep-alive leg served %d responses, want 3" % ser.count("httpd: served"))
out = open(os.path.join(os.environ["RUN_DIR"], "run-03.out"), errors="replace").read()
# The HEAD answer carries the entity length but no JSON body.
if 'Content-Length' not in out:
    sys.exit("HEAD answer missing Content-Length")
if '"status":"ok"' in out:
    sys.exit("HEAD answer carried the healthz body")
print("keep-alive: 3 requests on one connection; HEAD had headers only")
PY

# ---- 04: refusal surface -- traversal 404, POST 405, garbage 400 ----------
vgate_run 04 -- --net '$RUN_DIR/cap4.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect '10.0.0.1:8080:$RUN_DIR/req-errors.txt' \
    --net-tcp-connect '10.0.0.1:8080:$RUN_DIR/req-garbage.txt' \
    --net-tcp-connect-after 'httpd: listening on port 8080' \
    --net-tcp-connect-close-after 'Connection: close' \
    --script '$RUN_DIR/script-1.txt' \
    --script2 '$RUN_DIR/script-3.txt' --script2-after 'httpd: closed' --script2-delay 4 \
    --script-expect 'httpd-killed' --timeout 90

vgate_assert 04 serial-contains 'kill: GOHTTPD.ELF armed'
vgate_assert 04 serial-contains 'httpd: request /../SECRETS.TXT'
vgate_assert 04 output-contains 'HTTP/1.1 404 Not Found'
vgate_assert 04 output-contains 'HTTP/1.1 405 Method Not Allowed'
vgate_assert 04 output-contains 'HTTP/1.1 400 Bad Request'
vgate_assert 04 serial-absent 'httpd: error'
vgate_assert 04 serial-absent '[EXC] parking:'
vgate_assert 04 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
# No path outside the served root is ever logged or opened: the traversal
# target is logged as the client's own string, and SECRETS.TXT's staged
# name never appears as a served path.
if "/host/" in ser:
    sys.exit("a share-root path leaked into the serial log")
print("traversal refused 404, POST 405, garbage 400")
PY

# ---- 05: HTTPS -- GOHTTPD --tls on 8443, host TLS 1.3 client (M135a) ------
vgate_run 05 -- --net '$RUN_DIR/cap5.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect '10.0.0.1:8443' \
    --net-tcp-connect-stream '$RUN_DIR/tlscli.py' \
    --net-tcp-connect-stream-arg '$RUN_DIR/TLS.CRT' \
    --net-tcp-connect-after 'httpd: listening on port 8443' \
    --script '$RUN_DIR/script-tls.txt' \
    --script2 '$RUN_DIR/script-tls-kill.txt' --script2-after 'httpd: closed' --script2-delay 3 \
    --script-expect 'tls-killed' --timeout 90

vgate_assert 05 serial-contains 'httpd: listening on port 8443'
vgate_assert 05 serial-contains 'httpd: accepted'
vgate_assert 05 serial-contains 'httpd: request /'
vgate_assert 05 serial-contains 'httpd: served'
vgate_assert 05 serial-absent 'httpd: tls alert'
vgate_assert 05 serial-absent 'httpd: tls error'
vgate_assert 05 serial-absent '[EXC] parking:'
vgate_assert 05 python <<'PY'
import os, sys
out = open(os.path.join(os.environ["RUN_DIR"], "run-05.out"), errors="replace").read()
if "fixture match=True" not in out:
    sys.exit("peer cert did not equal the staged TLS.CRT")
if "TLSCLI: handshake ok" not in out:
    sys.exit("host TLS 1.3 client did not complete the handshake")
if "VirelaiOS" not in out:
    sys.exit("HTTPS body missing the index page")
ser = open(os.environ["VG_SER"], errors="replace").read()
if "httpd: tls alert" in ser or "httpd: tls error" in ser:
    sys.exit("a TLS alert/error reached serial")
print("HTTPS leg: host TLS 1.3 client over the relay, cert + body verified")
PY

# ---- 06: TLS 1.2-only client -> protocol_version alert --------------------
vgate_run 06 -- --net '$RUN_DIR/cap6.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect '10.0.0.1:8443' \
    --net-tcp-connect-stream '$RUN_DIR/tlscli.py' \
    --net-tcp-connect-stream-arg '$RUN_DIR/TLS.CRT' \
    --net-tcp-connect-stream-arg 'tls12' \
    --net-tcp-connect-after 'httpd: listening on port 8443' \
    --script '$RUN_DIR/script-tls.txt' \
    --script2 '$RUN_DIR/script-tls-kill.txt' --script2-after 'httpd: closed' --script2-delay 3 \
    --script-expect 'tls-killed' --timeout 90

vgate_assert 06 serial-contains 'httpd: listening on port 8443'
vgate_assert 06 serial-contains 'httpd: accepted'
vgate_assert 06 serial-contains 'httpd: tls alert protocol_version'
vgate_assert 06 serial-contains 'httpd: closed'
vgate_assert 06 serial-absent 'httpd: served'
vgate_assert 06 serial-absent '[EXC] parking:'

vgate_assert 06 python <<'PY'
import json, os, pathlib, shutil, subprocess
# Stage the M201c opt-in boot for leg 07: INIT.ELF + the seat binaries, and
# the SHIPPED manifest with the httpd row enabled — the opt-in boot the
# card names, not a hand-built service shape.
share = pathlib.Path(os.environ["VG_SHARE"])
for script in ("build-init.sh", "build-gotabwm.sh", "build-gosh.sh"):
    subprocess.run(["bash", "tools/go/" + script], check=True)
for name in ("INIT", "GOTABWM", "GOSH"):
    shutil.copyfile(".build/go/" + name + ".ELF", share / (name + ".ELF"))
(share / "INIT").mkdir(exist_ok=True)
m = json.loads(pathlib.Path("user/go/init/SERVICES.JSON").read_text())
row = [s for s in m["services"] if s["name"] == "httpd"]
assert len(row) == 1 and row[0].get("enabled") is False, "shipped httpd row is not opt-in"
row[0]["enabled"] = True
(share / "INIT/SERVICES.JSON").write_text(json.dumps(m))
print("live-httpd: staged INIT/GOTABWM/GOSH; shipped httpd row enabled for leg 07")
PY

# ---- 07: M201c -- init owns the server lifecycle on an opt-in boot -------
# The enabled httpd row makes init start GOHTTPD.ELF in plan order (httpd
# sorts before seat). The kernel's SocketListen refuses ENXIO until the
# guest carries an IP (syscall.zig op 0), so the pre-`net ip` start fails
# by name -- 'httpd: listen failed' / 'svc: exit status=1' -- and the
# manifest's on-failure policy retries in backoff until the scripted IP
# lands. That churn IS the lifecycle evidence: the manifest, not a manual
# exec, owns every start, backoff and receipt. (An external `kill NAME`
# would only ever reach the exited first incarnation -- live-supervise
# documents the lowest-id name rule -- so the natural ENXIO failure is
# what exercises the restart policy.)
vgate_file script-7.txt <<'EOF'
net ip 10.0.0.1
procs
echo init-seated-observed
EOF

vgate_run 07 -- --screen '$RUN_DIR/screen-init' --net '$RUN_DIR/cap7.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect '10.0.0.1:8080:$RUN_DIR/req-index.txt' \
    --net-tcp-connect-after 'httpd: listening on port 8080' \
    --net-tcp-connect-close-after 'Connection: close' \
    --script '$RUN_DIR/script-7.txt' --script-after 'init: seated' \
    --script-expect 'httpd: served' --script-expect-tail 4 --timeout 150

vgate_assert 07 serial-contains 'init: autostart INIT.ELF (settings init=on)'
vgate_assert 07 serial-contains 'init: manifest ok n=2'
vgate_assert 07 serial-contains 'init: start name=httpd order=1'
vgate_assert 07 serial-contains 'init: start name=seat order=2'
vgate_assert 07 serial-contains 'init: seated'
vgate_assert 07 serial-contains 'httpd: starting'
vgate_assert 07 serial-contains 'httpd: listen failed'
vgate_assert 07 serial-contains 'svc: backoff name=httpd k=1'
vgate_assert 07 serial-contains 'httpd: listening on port 8080'
vgate_assert 07 serial-contains 'init: ready name=httpd'
vgate_assert 07 serial-contains 'httpd: accepted'
vgate_assert 07 serial-contains 'httpd: request /'
vgate_assert 07 serial-contains 'httpd: served'
vgate_assert 07 output-contains 'HTTP/1.1 200 OK'
vgate_assert 07 output-contains 'VirelaiOS'
vgate_assert 07 serial-absent 'init: refuse'
vgate_assert 07 serial-absent 'init: fallback'
vgate_assert 07 serial-absent 'svc: failed name=httpd'
vgate_assert 07 serial-absent '[EXC] parking:'
vgate_assert 07 share-contains CRASH/GOHTTPD.TXT 'status=1'
vgate_assert 07 python <<'PY'
import os, re, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
assert ser.index("net ip: ip=10.0.0.1") < ser.index("httpd: listening on port 8080"),     "listener bound before the guest had an IP"
exits1 = len(re.findall(r"svc: exit name=httpd status=1 ", ser))
starts = re.findall(r"svc: start name=httpd pid=(\d+) t_ns=\d+", ser)
assert len(starts) >= 2 and len(starts) == len(set(starts)), ("restarts", starts)
backoffs = re.findall(r"svc: backoff name=httpd k=(\d+) delay_s=(\d+)", ser)
assert [int(k) for k, _ in backoffs] == list(range(1, len(backoffs) + 1)), backoffs
for k, delay in ((int(k), int(d)) for k, d in backoffs):
    base = min(16, 2 << (k - 1))
    assert base <= delay < base + 2, ("backoff bound", k, delay)
assert ser.index("init: seated") < ser.index("httpd: served")
print("init-owned lifecycle: %d supervised starts, %d named no-IP exits, backoff %s -> listen -> served" % (
    len(starts), exits1, [d for _, d in backoffs]))
PY
vgate_assert 07 python <<'PY'
import os, pathlib
# Restore the shipped opt-out manifest for leg 08.
share = pathlib.Path(os.environ["VG_SHARE"])
(share / "INIT/SERVICES.JSON").write_bytes(
    pathlib.Path("user/go/init/SERVICES.JSON").read_bytes())
for name in ("CRASH/GOHTTPD.TXT", "SESSION.TABS", "WINDOWS.SAV"):
    (share / name).unlink(missing_ok=True)
print("live-httpd: shipped manifest restored (httpd row disabled) for leg 08")
PY
# ---- 08: opt-out is honest -- the shipped manifest boots without it ------
vgate_file script-8.txt <<'EOF'
procs
echo optout-observed
EOF

vgate_run 08 -- --screen '$RUN_DIR/screen-optout' \
    --script '$RUN_DIR/script-8.txt' --script-after 'init: seated' \
    --script-expect 'optout-observed' --script-expect-tail 2 --timeout 90

vgate_assert 08 serial-contains 'init: manifest ok n=1'
vgate_assert 08 serial-contains 'init: start name=seat order=1'
vgate_assert 08 serial-contains 'init: seated'
vgate_assert 08 serial-absent 'svc: start name=httpd'
vgate_assert 08 serial-absent 'httpd: starting'
vgate_assert 08 serial-absent 'httpd: listening'
vgate_assert 08 serial-absent 'init: refuse'
vgate_assert 08 serial-absent '[EXC] parking:'
vgate_assert 08 python <<'PY'
import json, os, pathlib, re
ser = pathlib.Path(os.environ["VG_SER"]).read_text(errors="replace")
share = pathlib.Path(os.environ["VG_SHARE"])
m = json.loads((share / "INIT/SERVICES.JSON").read_text())
row = [s for s in m["services"] if s["name"] == "httpd"]
assert len(row) == 1 and row[0].get("enabled") is False, \
    "the shipped row is not the opt-out shape the leg booted"
assert not re.search(r"procs: [^\n]*name=GOHTTPD.ELF", ser), \
    "a disabled service is on the process table"
print("opt-out boot: the shipped disabled httpd row never leaves the manifest")
PY

# ---- M209a (#3825): the opt-in /admin/ section -------------------------
# Legs 09-11 cover the card's auth lattice end to end. Leg 09 boots with
# NO SECRETS.TXT on the share (leg 08's tail cleanup enforces it, which
# also keeps the shape under vgate_repeat), so StoreGet finds no
# httpd-admin row and even correct credentials answer 404 — locked by
# default means the section does not exist. That run's tail assert then
# stages the uid_user row legs 10/11 authenticate against; the token is
# asserted serial-absent everywhere — creds never reach a marker or the
# log (only request targets are logged, never headers).

vgate_file script-admin-tls.txt <<'EOF'
net ip 10.0.0.1
exec GOHTTPD.ELF --tls /host/TLS.CRT /host/TLS.SEED --admin 8443
echo admin-tls-launched
EOF

vgate_file script-admin-plain.txt <<'EOF'
net ip 10.0.0.1
exec GOHTTPD.ELF --admin
echo admin-plain-launched
EOF

# Leg 11's payload: correct credentials on a PLAINTEXT request — the 403
# named refusal fires before the credential is ever looked at.
vgate_file req-admin-plain.txt <<'EOF'
GET /admin/procs HTTP/1.1
Authorization: Basic YWRtaW46bTIwOWEtZ2F0ZS10b2tlbg==
Connection: close

EOF

# Legs 09/10's client: TLS 1.3 over the runner's byte relay (tlscli.py's
# shape), then the /admin/ suite on ONE keep-alive connection.
# argv: CERT_DER GOOD_B64 BAD_B64 [norow] — the *_B64 args are base64'd
# Basic user:pass pairs; "norow" runs only the procs request (leg 09).
# stderr carries "ADMINCLI <tag> <status>" plus ADMINCLI-BODY blocks.
vgate_file admincli.py <<'EOF'
#!/usr/bin/env python3
import os, ssl, sys

cert_path, good, bad = sys.argv[1], sys.argv[2], sys.argv[3]
mode = sys.argv[4] if len(sys.argv) > 4 else "full"

ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE
ctx.minimum_version = ssl.TLSVersion.TLSv1_3
ctx.maximum_version = ssl.TLSVersion.TLSv1_3

inbio = ssl.MemoryBIO()
outbio = ssl.MemoryBIO()
so = ctx.wrap_bio(inbio, outbio, server_side=False)

def flush():
    while True:
        b = outbio.read()
        if not b:
            return
        os.write(1, b)

ok = False
while True:
    try:
        so.do_handshake()
        ok = True
        break
    except ssl.SSLWantReadError:
        flush()
        block = os.read(0, 65536)
        if not block:
            break
        inbio.write(block)
    except ssl.SSLWantWriteError:
        flush()
    except ssl.SSLError as e:
        sys.stderr.write("ADMINCLI: handshake ssl error %r\n" % (e,))
        break
    flush()

if not ok:
    sys.exit(2)

def pump_read():
    # Feed stdin ciphertext until the TLS layer takes it or the relay ends.
    block = os.read(0, 65536)
    if not block:
        return False
    inbio.write(block)
    return True

def send_all(data):
    # A post-handshake write can want a read (KeyUpdate and friends): feed
    # stdin on WantRead, flush pending records on WantWrite, else send.
    while data:
        try:
            so.write(data)
            data = b""
        except ssl.SSLWantWriteError:
            flush()
        except ssl.SSLWantReadError:
            if not pump_read():
                raise
    flush()

def complete(buf):
    head, sep, body = buf.partition(b"\r\n\r\n")
    if not sep:
        return False
    for line in head.split(b"\r\n")[1:]:
        if line.lower().startswith(b"content-length:"):
            want = int(line.split(b":", 1)[1].strip())
            return len(body) >= want
    return True  # every response here carries Content-Length

def http_get(path, cred, last):
    req = "GET %s HTTP/1.1\r\n" % path
    if cred:
        req += "Authorization: Basic %s\r\n" % cred
    req += "Connection: %s\r\n\r\n" % ("close" if last else "keep-alive")
    send_all(req.encode())
    buf = b""
    while not complete(buf):
        try:
            d = so.read(4096)
            if not d:
                break
            buf += d
        except ssl.SSLWantReadError:
            # Read stdin ONLY when the TLS layer wants ciphertext —
            # blocking on stdin with a complete buffered response was the
            # run-10 deadlock.
            if not pump_read():
                break
        except ssl.SSLWantWriteError:
            flush()
        except ssl.SSLError as e:
            sys.stderr.write("ADMINCLI: read err %r\n" % (e,))
            break
    return buf

seq = [("procs", "/admin/procs", good)]
if mode != "norow":
    seq += [
        ("grants", "/admin/grants", good),
        ("badcred", "/admin/procs", bad),
        # The card's first check, observed: SECRETS.TXT is armed on the
        # share this boot and the share fallback still cannot serve it
        # (secret-class by construction, ADR 0024 D8 -> FileOpen EACCES).
        ("secrets", "/SECRETS.TXT", good),
        ("noadmin", "/admin/nope", good),
        ("index", "/admin/", good),
    ]
for i, (tag, path, cred) in enumerate(seq):
    try:
        r = http_get(path, cred, i == len(seq) - 1)
    except Exception as e:
        sys.stderr.write("ADMINCLI %s client error %r\n" % (tag, e))
        r = b""
    status = r.split(b"\r\n", 1)[0].decode(errors="replace") if r else "NO-RESPONSE"
    sys.stderr.write("ADMINCLI %s %s\n" % (tag, status))
    sys.stderr.write("ADMINCLI-BODY %s\n%s\nADMINCLI-END %s\n"
                     % (tag, r.decode(errors="replace"), tag))
sys.exit(0)
EOF

# Leg 09's no-row boot precondition, owned by the run before it so the
# order is honest under vgate_repeat too.
vgate_assert 08 python <<'PY'
import os, pathlib
p = pathlib.Path(os.environ["VG_SHARE"]) / "SECRETS.TXT"
p.unlink(missing_ok=True)
print("live-httpd: SECRETS.TXT absent for leg 09's no-row boot")
PY

# ---- 09: --admin over TLS, no httpd-admin row -> the section is 404 ----
vgate_run 09 -- --net '$RUN_DIR/cap9.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect '10.0.0.1:8443' \
    --net-tcp-connect-stream '$RUN_DIR/admincli.py' \
    --net-tcp-connect-stream-arg '$RUN_DIR/TLS.CRT' \
    --net-tcp-connect-stream-arg 'YWRtaW46bTIwOWEtZ2F0ZS10b2tlbg==' \
    --net-tcp-connect-stream-arg 'YWRtaW46d3JvbmctdG9rZW4=' \
    --net-tcp-connect-stream-arg 'norow' \
    --net-tcp-connect-after 'httpd: listening on port 8443' \
    --script '$RUN_DIR/script-admin-tls.txt' \
    --script2 '$RUN_DIR/script-tls-kill.txt' --script2-after 'httpd: closed' --script2-delay 3 \
    --script-expect 'tls-killed' --timeout 90

vgate_assert 09 serial-contains 'httpd: listening on port 8443'
vgate_assert 09 serial-contains 'httpd: request /admin/procs'
vgate_assert 09 output-contains 'ADMINCLI procs HTTP/1.1 404'
vgate_assert 09 serial-absent 'm209a-gate-token'
vgate_assert 09 serial-absent 'YWRtaW46bTIwOWEtZ2F0ZS10b2tlbg=='
vgate_assert 09 serial-absent 'httpd: tls alert'
vgate_assert 09 serial-absent '[EXC] parking:'
vgate_assert 09 python <<'PY'
import os, pathlib
# Stage the credential legs 10/11 authenticate with: a uid_user row,
# `key<TAB>uid<TAB>value` — uid-scope only, no app binding (the gate's
# GOHTTPD is an ordinary EL0 process).
pathlib.Path(os.environ["VG_SHARE"], "SECRETS.TXT").write_text(
    "#v1\nhttpd-admin\t1000\tm209a-gate-token\n")
print("live-httpd: staged SECRETS.TXT httpd-admin row for legs 10/11")
PY

# ---- 10: --admin over TLS with the row -> the whole suite --------------
vgate_run 10 -- --net '$RUN_DIR/cap10.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect '10.0.0.1:8443' \
    --net-tcp-connect-stream '$RUN_DIR/admincli.py' \
    --net-tcp-connect-stream-arg '$RUN_DIR/TLS.CRT' \
    --net-tcp-connect-stream-arg 'YWRtaW46bTIwOWEtZ2F0ZS10b2tlbg==' \
    --net-tcp-connect-stream-arg 'YWRtaW46d3JvbmctdG9rZW4=' \
    --net-tcp-connect-after 'httpd: listening on port 8443' \
    --script '$RUN_DIR/script-admin-tls.txt' \
    --script2 '$RUN_DIR/script-tls-kill.txt' --script2-after 'httpd: closed' --script2-delay 3 \
    --script-expect 'tls-killed' --timeout 90

vgate_assert 10 serial-contains 'httpd: listening on port 8443'
vgate_assert 10 serial-contains 'httpd: request /admin/procs'
vgate_assert 10 serial-contains 'httpd: request /admin/grants'
vgate_assert 10 serial-contains 'httpd: request /SECRETS.TXT'
vgate_assert 10 output-contains 'ADMINCLI procs HTTP/1.1 200'
vgate_assert 10 output-contains 'ADMINCLI grants HTTP/1.1 200'
vgate_assert 10 output-contains 'ADMINCLI badcred HTTP/1.1 401'
vgate_assert 10 output-contains 'ADMINCLI secrets HTTP/1.1 404'
vgate_assert 10 output-contains 'ADMINCLI noadmin HTTP/1.1 404'
vgate_assert 10 output-contains 'ADMINCLI index HTTP/1.1 200'
vgate_assert 10 output-contains 'WWW-Authenticate: Basic'
# The first-check pin: the armed share carries SECRETS.TXT this boot AND
# the fallback still answered 404 for it — it can never be served.
vgate_assert 10 share-contains SECRETS.TXT 'httpd-admin'
vgate_assert 10 serial-absent 'm209a-gate-token'
vgate_assert 10 serial-absent 'YWRtaW46bTIwOWEtZ2F0ZS10b2tlbg=='
vgate_assert 10 serial-absent 'httpd: tls alert'
vgate_assert 10 serial-absent 'httpd: tls error'
vgate_assert 10 serial-absent '[EXC] parking:'
vgate_assert 10 python <<'PY'
import os, re, sys
out = open(os.path.join(os.environ["RUN_DIR"], "run-10.out"),
           errors="replace").read()
def body(tag):
    m = re.search(r"ADMINCLI-BODY %s\n(.*?)ADMINCLI-END %s" % (tag, tag),
                  out, re.S)
    return m.group(1) if m else ""
procs = body("procs")
if "GOHTTPD.ELF" not in procs:
    sys.exit("procs page did not list GOHTTPD.ELF")
if "sampled t=" not in procs:
    sys.exit("procs page missing the timestamp")
grants = body("grants")
if "unknown (" not in grants and "<td>" not in grants:
    sys.exit("grants page shows neither rows nor the named unknown")
if "WWW-Authenticate: Basic" not in body("badcred"):
    sys.exit("401 response carried no Basic challenge")
if "m209a-gate-token" in body("secrets"):
    sys.exit("SECRETS.TXT content reached the wire")
print("admin suite: procs 200 + GOHTTPD.ELF + timestamp, grants rows-or-"
      "unknown, 401+challenge, SECRETS.TXT 404 while armed")
PY

# ---- 11: --admin over plaintext -> 403 'admin requires tls' ------------
vgate_run 11 -- --net '$RUN_DIR/cap11.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect '10.0.0.1:8080:$RUN_DIR/req-admin-plain.txt' \
    --net-tcp-connect-after 'httpd: listening on port 8080' \
    --net-tcp-connect-close-after 'Connection: close' \
    --script '$RUN_DIR/script-admin-plain.txt' \
    --script2 '$RUN_DIR/script-3.txt' --script2-after 'httpd: closed' --script2-delay 3 \
    --script-expect 'httpd-killed' --timeout 90

vgate_assert 11 serial-contains 'httpd: listening on port 8080'
vgate_assert 11 serial-contains 'httpd: request /admin/procs'
vgate_assert 11 output-contains 'HTTP/1.1 403'
vgate_assert 11 output-contains 'admin requires tls'
vgate_assert 11 serial-absent 'm209a-gate-token'
vgate_assert 11 serial-absent 'YWRtaW46bTIwOWEtZ2F0ZS10b2tlbg=='
vgate_assert 11 serial-absent '[EXC] parking:'
