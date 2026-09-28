# live-rfb.spec -- M84c: explicit in-seat RFB 3.8/None over hermetic --net.
# An independent host probe reads Calc's actual button pixels and drives
# Ctrl+Space plus a launcher-dismiss pointer click over the runner's bounded
# byte-stream adapter. None authenticates nobody; no LAN/real-iron claim.
# Prereqs: .build/go/{GOTABWM.ELF,GOCALC.ELF,rfbprobe}.
# exec-order: assert-proven -- script2 waits for the seat's own window focus,
# and the run ends on the seat's RFB-driven launcher dismiss marker.

vgate_name live-rfb "M84c: RFB 3.8/None pixels and kind-19/21 input on the explicitly started Go seat"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

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

vgate_setup_python <<'PY'
import os, shutil, sys
share = os.environ.get("VG_SHARE") or os.path.join(os.environ["RUN_DIR"], "share")
for name, cmd in (("GOTABWM.ELF", "bash tools/go/build-gotabwm.sh"),
                  ("GOCALC.ELF", "bash tools/go/build-gocalc.sh")):
    src = os.path.join(".build", "go", name)
    if not os.path.isfile(src):
        sys.exit(name + " missing: " + cmd)
    shutil.copy(src, os.path.join(share, name))
if not os.path.isfile(".build/go/rfbprobe"):
    sys.exit("rfbprobe missing: go build -C tools/go/rfbprobe -o ../../../.build/go/rfbprobe .")
# Disable autostart, then explicitly exec the opt-in seat from the monitor.
# Do not seed GOTABWM.DEMO: the real live seat must outlast the exchange.
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    f.write("#v2\nwm=none\n")
PY

vgate_run 01 -- \
    --screen '$RUN_DIR/screen' \
    --net '$RUN_DIR/cap.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect 10.0.0.1:5900 \
    --net-tcp-connect-stream '.build/go/rfbprobe' \
    --net-tcp-connect-after 'gocalc: present' \
    --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/script2.txt' \
    --script2-after 'gotabwm: win focus' \
    --script-expect 'gotabwm: launcher dismiss' --script-expect-tail 6 --timeout 160

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
vgate_assert 01 serial-contains 'gotabwm: launcher open n='
vgate_assert 01 serial-contains 'gotabwm: rfb key usage=44'
vgate_assert 01 serial-contains 'gotabwm: rfb pointer x=100 y=600 buttons=1'
vgate_assert 01 serial-contains 'gotabwm: launcher dismiss'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'exited status=139'
vgate_assert 01 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
out = open(os.path.join(os.environ["RUN_DIR"], "run-01.out"), errors="replace").read()
ordered = ("gocalc: present", "gotabwm: rfb ready", "gotabwm: rfb frame",
           "gotabwm: launcher open n=", "gotabwm: rfb key usage=44",
           "gotabwm: launcher dismiss", "gotabwm: rfb pointer x=100 y=600 buttons=1")
at = -1
for marker in ordered:
    at = ser.find(marker, at + 1)
    if at < 0:
        sys.exit("missing/out-of-order RFB receipt: " + marker)
if "RFBPROBE: FAIL" in out:
    sys.exit("probe rejected the live wire or pixels")
print("RFB pixels + seat key/pointer receipts ordered")
PY
