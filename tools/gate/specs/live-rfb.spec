# live-rfb.spec -- M84c/M84d: explicit in-seat RFB 3.8/None over hermetic --net.
# Run 01: an independent host probe reads Calc's real button pixels, drives
# Ctrl+Space and a launcher-dismiss click, and a second viewer's SYN is
# refused honestly with RST (M84f, #1836). Runs 02-04 are the negatives:
# a refused security type, an oversized message with a press held, and a
# wedged viewer. Runs 05-06 are
# the M52 death sweep: the viewer dies mid-drag+mid-update, then mid-focus.
# Every refusal must name its drop reason, and the seat must then take
# LOCAL input: a fresh press edge is the proof that no capture is stuck.
# None authenticates nobody; no LAN/real-iron claim.
# Runs 07-08 are the M213c VeNCrypt lane (#3878): the seat offers security
# type 19 only and terminates VeNCrypt 0.2 subtype X509Plain (262) on the
# M135 TLS server half. The verifier is the probe's TLS mode — no
# third-party VeNCrypt viewer is reachable (Screen Sharing speaks type 2
# only; TigerVNC-class viewers are GPL, barred as a test reference). X509Vnc
# is refused: guest-side DES is rejected (ADR 0037 D6).
# Prereqs: .build/go/{GOTABWM.ELF,GOCALC.ELF,rfbprobe}.
# exec-order: assert-proven -- script2 waits for the seat's own window focus,
# and every run ends on a seat marker only the RFB session or post-session
# local input can print.

vgate_name live-rfb "M84c/M84d/M213c: RFB 3.8/None pixels, input, fail-closed negatives, viewer-death sweep, and the VeNCrypt X509Plain lane on the Go seat"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 3 BOOTS

vgate_file script.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
set GOMAXPROCS=1
exec GOTABWM.ELF --rfb-hermetic
EOF

vgate_file script2.txt <<'EOF'
dui focus 0
exec GOCALC.ELF
EOF

vgate_file script-venc.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
set GOMAXPROCS=1
exec GOTABWM.ELF --rfb-vencrypt /host/TLS.CRT /host/TLS.SEED /host/RFB.CRED
EOF

vgate_file rfborder.py <<'EOF'
import os, sys

def texts():
    run = os.path.join(os.environ["RUN_DIR"], "run-" + os.environ["VG_TAG"] + ".out")
    return (open(os.environ["VG_SER"], errors="replace").read(),
            open(run, errors="replace").read())

def ordered(ser, markers):
    at = -1
    for marker in markers:
        at = ser.find(marker, at + 1)
        if at < 0:
            sys.exit("missing/out-of-order RFB receipt: " + marker)

def probe_ok(out):
    if "RFBPROBE: FAIL" in out:
        sys.exit("probe rejected the live wire: " + out.split("RFBPROBE: FAIL", 1)[1].splitlines()[0])
EOF

vgate_setup_python <<'PY'
import os, shutil, subprocess, sys
share = os.environ.get("VG_SHARE") or os.path.join(os.environ["RUN_DIR"], "share")
subprocess.run(["bash", "tools/go/rfbprobe/build.sh"], check=True)
for name, cmd in (("GOTABWM.ELF", "bash tools/go/build-gotabwm.sh"),
                  ("GOCALC.ELF", "bash tools/go/build-gocalc.sh")):
    src = os.path.join(".build", "go", name)
    if not os.path.isfile(src):
        sys.exit(name + " missing: " + cmd)
    shutil.copy(src, os.path.join(share, name))
# Disable autostart, then explicitly exec the opt-in seat from the monitor.
# Do not seed GOTABWM.DEMO: the real live seat must outlast the exchange.
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    f.write("#v2\nwm=none\n")

# M213c legs 07-08: the VeNCrypt lane's credential set — the same env-gated
# host fixture generator live-httpd uses (DER self-signed Ed25519, CA:TRUE
# + serverAuth + the 10.0.0.1 IP SAN the probe verifies), plus the staged
# X509Plain credential. The probe reads its trust anchor from a RUN_DIR copy.
env = dict(os.environ)
env["HTTPD_TLS_GEN_FIXTURE"] = "1"
env["HTTPD_TLS_FIXTURE_DIR"] = share
r = subprocess.run(["go", "-C", "user/go", "test", "-run", "TestGenTLSFixture",
                    "./httpd/"], cwd=os.getcwd(), env=env,
                   capture_output=True, text=True)
sys.stderr.write(r.stdout[-800:] + r.stderr[-800:])
if r.returncode != 0:
    sys.exit("TLS fixture generator failed")
for name in ("TLS.CRT", "TLS.SEED"):
    if not os.path.exists(os.path.join(share, name)):
        sys.exit("fixture missing " + name)
with open(os.path.join(share, "RFB.CRED"), "w") as f:
    f.write("vncuser\nRFB-CRED-01\n")
shutil.copy(os.path.join(share, "TLS.CRT"),
            os.path.join(os.environ["RUN_DIR"], "TLS.CRT"))
print("live-rfb: staged TLS.CRT + TLS.SEED + RFB.CRED for the VeNCrypt legs")
PY

# ---- 01: pixels + input, a clean close, and one viewer only ----
vgate_run 01 -- \
    --screen '$RUN_DIR/screen' \
    --net '$RUN_DIR/cap.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect 10.0.0.1:5900 \
    --net-tcp-connect-stream '.build/go/rfbprobe' \
    --net-tcp-connect-intruder \
    --net-tcp-connect-after 'gocalc: present' \
    --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/script2.txt' \
    --script2-after 'gotabwm: win focus' \
    --script-expect 'gotabwm: rfb done' --script-expect-tail 4 --timeout 160

vgate_assert 01 serial-contains 'gotabwm: mode live'
vgate_assert 01 serial-contains 'gotabwm: rfb listen 5900 (None; hermetic only)'
vgate_assert 01 serial-contains 'gotabwm: rfb ready'
vgate_assert 01 serial-contains 'gocalc: declare accepted'
vgate_assert 01 serial-contains 'gocalc: present'
vgate_assert 01 serial-contains 'gotabwm: rfb frame'
vgate_assert 01 output-contains 'RFBPROBE: RFB 3.8 None 1280x720 VirelaiOS'
vgate_assert 01 output-contains 'RFBPROBE: Calc BtnIdle 16x16 pixels = #2d3748'
vgate_assert 01 output-contains 'RFBPROBE: pixel and input exchange complete'
vgate_assert 01 output-contains 'NET-TCP-STREAM: probe exited status=0'
vgate_assert 01 output-contains 'NET-TCP-STREAM: the viewer closed; sent FIN'
vgate_assert 01 serial-contains 'gotabwm: launcher open n='
vgate_assert 01 serial-contains 'gotabwm: rfb key usage=44'
vgate_assert 01 serial-contains 'gotabwm: rfb pointer x=800 y=600 buttons=1'
vgate_assert 01 serial-contains 'gotabwm: launcher dismiss'
vgate_assert 01 serial-contains 'gotabwm: rfb drop peer'
# Under M99f's per-listener two-child table (#2184) the kernel ADMITS the
# second SYN (child 2 of 2); the seat then drops the extra peer at the
# RFB layer (`rfb drop peer`), so the session still serves one viewer.
# The kernel's RST refusal now lives at the third SYN.
vgate_assert 01 output-contains 'NET-TCP-INTRUDER: sent a second SYN from port'
vgate_assert 01 output-contains 'NET-TCP-INTRUDER: the guest ADMITTED (SYN-ACK)'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'exited status=139'
vgate_assert 01 python <<'PY'
import os, sys
sys.path.insert(0, os.environ["RUN_DIR"])
from rfborder import texts, ordered, probe_ok
ser, out = texts()
ordered(ser, ("gocalc: present", "gotabwm: rfb ready", "gotabwm: rfb frame",
              "gotabwm: launcher open n=", "gotabwm: rfb key usage=44",
              "gotabwm: launcher dismiss", "gotabwm: rfb pointer x=800 y=600 buttons=1"))
ordered(ser, ("gotabwm: rfb ready", "gotabwm: rfb drop peer", "gotabwm: rfb done"))
probe_ok(out)
if "NET-TCP-INTRUDER: the guest ADMITTED (SYN-ACK)" not in out:
    sys.exit("the second SYN saw no SYN-ACK — the M99f two-child listener should admit it")
if "rfb drop peer" not in ser:
    sys.exit("the seat did not drop the admitted second peer at the RFB layer")
print("RFB pixels + seat key/pointer receipts ordered; FIN closed it; the second SYN was admitted (M99f child 2) and the seat dropped the extra peer")
PY

# ---- 02: a viewer that asks for VNC password auth (never offered) ----
# It keeps talking after its choice: a fake DES answer, ClientInit, and a
# press. The seat must answer SecurityResult=failed and hang up.
vgate_run 02 -- \
    --screen '$RUN_DIR/screen-02' \
    --via-virtio \
    --net '$RUN_DIR/cap-02.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect 10.0.0.1:5900 \
    --net-tcp-connect-stream '.build/go/rfbprobe' \
    --net-tcp-connect-stream-arg -mode=security \
    --net-tcp-connect-after 'gocalc: present' \
    --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/script2.txt' \
    --script2-after 'gotabwm: win focus' \
    --input-chords 'ctrl-space' --input-chords-after 'gotabwm: rfb done' \
    --script-expect 'gotabwm: launcher open n=' --script-expect-tail 3 --timeout 160

vgate_assert 02 serial-contains 'gotabwm: rfb handshake refused'
vgate_assert 02 serial-contains 'gocalc: present'
vgate_assert 02 serial-contains 'gotabwm: rfb drop handshake malformed'
vgate_assert 02 serial-contains 'gotabwm: rfb done'
vgate_assert 02 serial-absent 'gotabwm: rfb ready'
vgate_assert 02 serial-absent 'gotabwm: rfb pointer'
vgate_assert 02 output-contains 'RFBPROBE: security type 2 refused: rfb: security type refused'
vgate_assert 02 output-contains 'RFBPROBE: seat closed the connection after the refusal'
vgate_assert 02 output-contains 'NET-TCP-STREAM: probe exited status=0'
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 python <<'PY'
import os, sys
sys.path.insert(0, os.environ["RUN_DIR"])
from rfborder import texts, ordered, probe_ok
ser, out = texts()
# `gocalc: present` is unordered (serial-contains above): the probe SYN
# lands at `rfb listen` while Calc's launch is still in flight, so under
# shared-host noise the refused handshake legitimately precedes it.
ordered(ser, ("gotabwm: rfb handshake refused",
              "gotabwm: rfb drop handshake malformed", "gotabwm: rfb done",
              "gotabwm: launcher open n="))
probe_ok(out)
print("security type 2 refused on the wire; the seat took local input afterwards")
PY

# ---- 03: an oversized message while the viewer holds a content press ----
vgate_run 03 -- \
    --screen '$RUN_DIR/screen-03' \
    --net '$RUN_DIR/cap-03.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect 10.0.0.1:5900 \
    --net-tcp-connect-stream '.build/go/rfbprobe' \
    --net-tcp-connect-stream-arg -mode=malformed \
    --net-tcp-connect-after 'gotabwm: rfb listen' \
    --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/script2.txt' \
    --script2-after 'gotabwm: win focus' \
    --input-chords 'ctrl-space' --input-chords-after 'gotabwm: rfb release x=' \
    --pointer-virtio '800,600,d;800,600,u' --pointer-virtio-after 'gotabwm: launcher open n=' \
    --script-expect 'gotabwm: launcher dismiss' --script-expect-tail 3 --timeout 160

vgate_assert 03 serial-contains 'gotabwm: rfb ready'
vgate_assert 03 serial-contains 'gotabwm: rfb pointer x=88 y=158 buttons=1'
vgate_assert 03 serial-contains 'gotabwm: rfb drop malformed'
vgate_assert 03 serial-contains 'gotabwm: rfb release x=88 y=158'
vgate_assert 03 output-contains 'RFBPROBE: sent SetEncodings count=65535 with a held press'
vgate_assert 03 output-contains 'RFBPROBE: seat closed the connection after the oversized message'
vgate_assert 03 serial-absent '[EXC] parking:'
vgate_assert 03 python <<'PY'
import os, sys
sys.path.insert(0, os.environ["RUN_DIR"])
from rfborder import texts, ordered, probe_ok
ser, out = texts()
ordered(ser, ("gotabwm: rfb ready", "gotabwm: rfb drop malformed", "gotabwm: rfb done",
              "gotabwm: rfb release x=88 y=158",
              "gotabwm: launcher open n=", "gotabwm: launcher dismiss"))
ordered(ser, ("gotabwm: rfb pointer x=88 y=158 buttons=1", "gotabwm: rfb release x=88 y=158"))
probe_ok(out)
print("oversized SetEncodings dropped the session; the held press was released; local click routed")
PY

# ---- 04: a viewer wedged mid-message; the seat keeps presenting ----
# Local Ctrl+Space and a dismiss click must land while the session waits,
# then the 30 s read budget drops it.
vgate_run 04 -- \
    --screen '$RUN_DIR/screen-04' \
    --net '$RUN_DIR/cap-04.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect 10.0.0.1:5900 \
    --net-tcp-connect-stream '.build/go/rfbprobe' \
    --net-tcp-connect-stream-arg -mode=stall \
    --net-tcp-connect-after 'gotabwm: rfb listen' \
    --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/script2.txt' \
    --script2-after 'gotabwm: win focus' \
    --input-chords 'ctrl-space' --input-chords-after 'gotabwm: rfb ready' \
    --pointer-virtio '800,600,d;800,600,u' --pointer-virtio-after 'gotabwm: launcher open n=' \
    --script-expect 'gotabwm: rfb done' --script-expect-tail 4 --timeout 160

vgate_assert 04 serial-contains 'gotabwm: rfb ready'
vgate_assert 04 serial-contains 'gotabwm: rfb drop timeout'
vgate_assert 04 serial-contains 'gotabwm: launcher dismiss'
vgate_assert 04 output-contains 'RFBPROBE: stalled 7 bytes short of a FramebufferUpdateRequest'
vgate_assert 04 output-contains 'RFBPROBE: seat closed the connection while stalled mid-message'
vgate_assert 04 serial-absent '[EXC] parking:'
vgate_assert 04 python <<'PY'
import os, sys
sys.path.insert(0, os.environ["RUN_DIR"])
from rfborder import texts, ordered, probe_ok
ser, out = texts()
ordered(ser, ("gotabwm: rfb ready", "gotabwm: launcher open n=",
              "gotabwm: launcher dismiss", "gotabwm: rfb drop timeout", "gotabwm: rfb done"))
probe_ok(out)
print("wedged viewer timed out; local input composited while it waited")
PY

# ---- 05: the viewer dies mid-drag and mid-update (RST) ----
vgate_run 05 -- \
    --screen '$RUN_DIR/screen-05' \
    --net '$RUN_DIR/cap-05.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect 10.0.0.1:5900 \
    --net-tcp-connect-stream '.build/go/rfbprobe' \
    --net-tcp-connect-stream-arg -mode=die-drag \
    --net-tcp-connect-after 'gotabwm: rfb listen' \
    --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/script2.txt' \
    --script2-after 'gotabwm: win focus' \
    --input-chords 'ctrl-space' --input-chords-after 'gotabwm: rfb release x=' \
    --pointer-virtio '800,600,d;800,600,u' --pointer-virtio-after 'gotabwm: launcher open n=' \
    --script-expect 'gotabwm: launcher dismiss' --script-expect-tail 3 --timeout 160

vgate_assert 05 serial-contains 'gotabwm: rfb pointer x=88 y=158 buttons=1'
vgate_assert 05 serial-contains 'gotabwm: rfb pointer x=128 y=188 buttons=1'
vgate_assert 05 serial-contains 'gotabwm: rfb drop peer'
vgate_assert 05 serial-contains 'gotabwm: rfb release x=128 y=188'
vgate_assert 05 serial-absent 'gotabwm: rfb frame'
vgate_assert 05 output-contains 'RFBPROBE: viewer dies mid-drag and mid-update (2048 of 3686400 raw bytes read)'
vgate_assert 05 output-contains 'NET-TCP-STREAM: probe exited status=3'
vgate_assert 05 output-contains 'NET-TCP-STREAM: the viewer died; sent RST'
vgate_assert 05 serial-absent '[EXC] parking:'
vgate_assert 05 python <<'PY'
import os, sys
sys.path.insert(0, os.environ["RUN_DIR"])
from rfborder import texts, ordered, probe_ok
ser, out = texts()
ordered(ser, ("gotabwm: rfb ready", "gotabwm: rfb drop peer", "gotabwm: rfb done",
              "gotabwm: rfb release x=128 y=188",
              "gotabwm: launcher open n=", "gotabwm: launcher dismiss"))
ordered(ser, ("gotabwm: rfb pointer x=128 y=188 buttons=1", "gotabwm: rfb release x=128 y=188"))
probe_ok(out)
print("viewer died mid-drag and mid-update; drag released at its last point; local click routed")
PY

# ---- 06: the viewer dies with the launcher it summoned still open ----
vgate_run 06 -- \
    --screen '$RUN_DIR/screen-06' \
    --net '$RUN_DIR/cap-06.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect 10.0.0.1:5900 \
    --net-tcp-connect-stream '.build/go/rfbprobe' \
    --net-tcp-connect-stream-arg -mode=die-focus \
    --net-tcp-connect-after 'gocalc: present' \
    --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/script2.txt' \
    --script2-after 'gotabwm: win focus' \
    --pointer-virtio '800,600,d;800,600,u' --pointer-virtio-after 'gotabwm: rfb done' \
    --script-expect 'gotabwm: launcher dismiss' --script-expect-tail 3 --timeout 160

vgate_assert 06 serial-contains 'gotabwm: launcher open n='
vgate_assert 06 serial-contains 'gotabwm: rfb key usage=44'
vgate_assert 06 serial-contains 'gotabwm: rfb key usage=6'
vgate_assert 06 serial-contains 'gotabwm: launcher filter q=c n='
vgate_assert 06 serial-contains 'gotabwm: rfb drop peer'
vgate_assert 06 serial-absent 'gotabwm: rfb release x='
vgate_assert 06 output-contains 'RFBPROBE: viewer dies mid-focus with the launcher open'
vgate_assert 06 output-contains 'NET-TCP-STREAM: the viewer died; sent RST'
vgate_assert 06 serial-absent '[EXC] parking:'
vgate_assert 06 python <<'PY'
import os, sys
sys.path.insert(0, os.environ["RUN_DIR"])
from rfborder import texts, ordered, probe_ok
ser, out = texts()
ordered(ser, ("gotabwm: rfb ready", "gotabwm: launcher open n=",
              "gotabwm: launcher filter q=c ", "gotabwm: launcher dismiss"))
ordered(ser, ("gotabwm: rfb ready", "gotabwm: rfb drop peer", "gotabwm: rfb done",
              "gotabwm: launcher dismiss"))
probe_ok(out)
print("viewer died holding the launcher; the local pointer dismissed it")
PY

# ---- 07: the VeNCrypt lane — type 19, 0.2, X509Plain over TLS 1.3 ----
# rfbprobe -mode=vencrypt is the reachable verifier: it pins the fixture
# leaf as its trust anchor and requires the real chain + Ed25519
# CertificateVerify from the in-tree server half. Auth, ClientInit, a
# verified Calc patch, and one routed press — then a clean FIN.
vgate_run 07 -- \
    --screen '$RUN_DIR/screen-07' \
    --net '$RUN_DIR/cap-07.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect 10.0.0.1:5900 \
    --net-tcp-connect-stream '.build/go/rfbprobe' \
    --net-tcp-connect-stream-arg '-mode=vencrypt' \
    --net-tcp-connect-stream-arg '-vencrypt-cert=$RUN_DIR/TLS.CRT' \
    --net-tcp-connect-stream-arg '-vencrypt-user=vncuser' \
    --net-tcp-connect-stream-arg '-vencrypt-pass=RFB-CRED-01' \
    --net-tcp-connect-after 'gocalc: present' \
    --script '$RUN_DIR/script-venc.txt' \
    --script2 '$RUN_DIR/script2.txt' \
    --script2-after 'gotabwm: win focus' \
    --script-expect 'gotabwm: rfb done' --script-expect-tail 4 --timeout 160

vgate_assert 07 serial-contains 'gotabwm: rfb listen 5900 (VeNCrypt X509Plain; hermetic only)'
vgate_assert 07 serial-contains 'gocalc: present'
vgate_assert 07 serial-contains 'gotabwm: rfb ready'
vgate_assert 07 serial-contains 'gotabwm: rfb frame'
vgate_assert 07 serial-contains 'gotabwm: rfb pointer x=800 y=600 buttons=1'
vgate_assert 07 serial-contains 'gotabwm: rfb drop peer'
vgate_assert 07 serial-contains 'gotabwm: rfb done'
vgate_assert 07 serial-absent 'gotabwm: rfb handshake refused'
vgate_assert 07 output-contains 'RFBPROBE: seat offers VeNCrypt (19) only'
vgate_assert 07 output-contains 'RFBPROBE: VeNCrypt 0.2, picking subtype X509Plain (262)'
vgate_assert 07 output-contains 'RFBPROBE: TLS 1.3 TLS_AES_128_GCM_SHA256 x25519'
vgate_assert 07 output-contains 'RFBPROBE: X509Plain auth accepted'
vgate_assert 07 output-contains 'RFBPROBE: Calc BtnIdle 16x16 pixels = #2d3748'
vgate_assert 07 output-contains 'RFBPROBE: VeNCrypt auth + frame + input complete'
vgate_assert 07 output-contains 'NET-TCP-STREAM: probe exited status=0'
vgate_assert 07 output-contains 'NET-TCP-STREAM: the viewer closed; sent FIN'
vgate_assert 07 serial-absent '[EXC] parking:'
vgate_assert 07 python <<'PY'
import os, sys
sys.path.insert(0, os.environ["RUN_DIR"])
from rfborder import texts, ordered, probe_ok
ser, out = texts()
ordered(ser, ("gotabwm: rfb listen 5900 (VeNCrypt X509Plain; hermetic only)",
              "gotabwm: rfb ready", "gotabwm: rfb frame",
              "gotabwm: rfb pointer x=800 y=600 buttons=1",
              "gotabwm: rfb drop peer", "gotabwm: rfb done"))
probe_ok(out)
print("VeNCrypt 0.2 X509Plain over the in-tree TLS 1.3 server: auth + frame + routed input")
PY

# ---- 08: a wrong X509Plain password is refused inside the channel ----
# The seat emits the refusal SecurityResult in TLS records, hangs up, and
# still takes a local Ctrl+Space afterwards — authentication actually
# gates the lane, it is not a vacuous accept.
vgate_run 08 -- \
    --screen '$RUN_DIR/screen-08' \
    --net '$RUN_DIR/cap-08.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect 10.0.0.1:5900 \
    --net-tcp-connect-stream '.build/go/rfbprobe' \
    --net-tcp-connect-stream-arg '-mode=vencrypt' \
    --net-tcp-connect-stream-arg '-vencrypt-cert=$RUN_DIR/TLS.CRT' \
    --net-tcp-connect-stream-arg '-vencrypt-user=vncuser' \
    --net-tcp-connect-stream-arg '-vencrypt-pass=RFB-CRED-WRONG' \
    --net-tcp-connect-after 'gocalc: present' \
    --script '$RUN_DIR/script-venc.txt' \
    --script2 '$RUN_DIR/script2.txt' \
    --script2-after 'gotabwm: win focus' \
    --input-chords 'ctrl-space' --input-chords-after 'gotabwm: rfb done' \
    --script-expect 'gotabwm: launcher open n=' --script-expect-tail 3 --timeout 160

vgate_assert 08 serial-contains 'gotabwm: rfb handshake refused'
vgate_assert 08 serial-contains 'gotabwm: rfb drop handshake malformed'
vgate_assert 08 serial-contains 'gotabwm: rfb done'
vgate_assert 08 serial-contains 'gotabwm: launcher open n='
vgate_assert 08 serial-absent 'gotabwm: rfb ready'
vgate_assert 08 serial-absent 'gotabwm: rfb frame'
vgate_assert 08 output-contains 'RFBPROBE: X509Plain refused: rfb: X509Plain credential refused'
vgate_assert 08 output-contains 'RFBPROBE: seat closed the connection after the credential refusal'
vgate_assert 08 output-contains 'NET-TCP-STREAM: probe exited status=0'
vgate_assert 08 serial-absent '[EXC] parking:'
vgate_assert 08 python <<'PY'
import os, sys
sys.path.insert(0, os.environ["RUN_DIR"])
from rfborder import texts, ordered, probe_ok
ser, out = texts()
ordered(ser, ("gotabwm: rfb handshake refused",
              "gotabwm: rfb drop handshake malformed", "gotabwm: rfb done",
              "gotabwm: launcher open n="))
probe_ok(out)
print("bad X509Plain credential refused inside TLS; the seat took local input afterwards")
PY
