# WEB TrueType/depth/image regressions at the frozen M93 presentation.
# Owner authorized the physical-pixel re-pin and approved all ten host-generated
# expectations. Exact RGB SHA-256 replaces impossible old near-native counts.
# Native font metrics, font-removal falsification and every behavior boot remain.
# No guest capture is a golden. Gate-only root artifacts retain guest WEB.ELF identity.
# Prerequisite: bash tools/go/build-web.sh browser WEB --gate

vgate_name live-web-ttf "WEB: native font metrics and exact M93 typography/table/image/fallback presentation"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script-oliver.txt <<'EOF'
exec WEB.ELF /host/OLIVER.HTML
EOF
vgate_file script-depth.txt <<'EOF'
exec WEB.ELF /host/DEPTH.HTML
EOF
vgate_file script-nofont.txt <<'EOF'
vf rm INTER.TTF
vf rm INTERB.TTF
vf rm INTERI.TTF
vf rm FIRACODE.TTF
vf ls
exec WEB.ELF /host/OLIVER.HTML
EOF
vgate_file script-lists.txt <<'EOF'
exec WEB.ELF /host/LISTS.HTML
EOF
vgate_file script-png.txt <<'EOF'
exec WEB.ELF /host/PNG.HTML
EOF

vgate_setup_python <<'PY'
import binascii, os, shutil, struct, sys, zlib
share = os.environ.get("VG_SHARE") or os.path.join(os.environ["RUN_DIR"], "share")
src = os.path.join(".build", "go", "WEB-GATE.ELF")
if not os.path.exists(src):
    sys.exit("build first: bash tools/go/build-web.sh browser WEB --gate")
shutil.copy(src, os.path.join(share, "WEB.ELF"))
shutil.copy(os.path.join("tests", "oliver-spike", "expect.html"), os.path.join(share, "OLIVER.HTML"))
for face in ("INTER.TTF", "INTERB.TTF", "INTERI.TTF", "FIRACODE.TTF"):
    if not os.path.exists(os.path.join(share, face)):
        sys.exit("missing seeded face: " + face)
colors = [(0x11, 0x7f, 0x33), (0xd0, 0x33, 0x99),
          (0x22, 0x66, 0xdd), (0xee, 0xcc, 0x00)]
qoi = bytearray(b"qoif") + struct.pack(">II", 32, 32) + bytes([4, 0])
rows = bytearray()
for y in range(32):
    rows.append(0)
    for x in range(32):
        c = colors[(y // 16) * 2 + x // 16]
        qoi.extend((0xfe, *c))
        rows.extend((*c, 255))
qoi.extend((0, 0, 0, 0, 0, 0, 0, 1))
def chunk(kind, body):
    return (struct.pack(">I", len(body)) + kind + body +
            struct.pack(">I", binascii.crc32(kind + body) & 0xffffffff))
png = (b"\x89PNG\r\n\x1a\n" +
       chunk(b"IHDR", struct.pack(">IIBBBBB", 32, 32, 8, 6, 0, 0, 0)) +
       chunk(b"IDAT", zlib.compress(bytes(rows))) + chunk(b"IEND", b""))
open(os.path.join(share, "SWATCH.QOI"), "wb").write(qoi)
open(os.path.join(share, "SWATCH.PNG"), "wb").write(png)
depth = (b"<p>MMMMMMMM</p><p><strong>MMMMMMMM</strong></p><h4>Depth</h4>"
         b"<table><thead><tr><th>Left</th><th>Right</th></tr></thead>"
         b"<tbody><tr><td>one</td><td>two</td></tr></tbody></table>"
         b'<p>An image:</p><img src="SWATCH.QOI" alt="swatch">'
         b'<p><a href="NEXT.HTML">a link</a></p>')
lists = (b"<h4>Lists</h4><dl><dt>term</dt><dd>definition sits indented</dd></dl>"
         b'<p><img src="NOPE.QOI" alt="missing"></p>')
open(os.path.join(share, "DEPTH.HTML"), "wb").write(depth)
open(os.path.join(share, "LISTS.HTML"), "wb").write(lists)
open(os.path.join(share, "PNG.HTML"), "wb").write(
    b'<img src="SWATCH.PNG" width="32" height="32" alt="guest PNG">')
print("M93f staged separately tagged WEB and original QOI/PNG/HTML fixtures")
PY

# 01: real faces. The metric markers remain native CSS/legacy font facts;
# the exact presentation hash proves actual text, not a chrome rectangle.
vgate_run 01 -- \
    --screen '$RUN_DIR/screen' --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-01' \
    --script '$RUN_DIR/script-oliver.txt' \
    --snapshot-after 'web: settled' --snapshot-after 'web: repaint' \
    --script-expect 'web: ready' --timeout 120

vgate_assert 01 serial-contains 'web: open id='
vgate_assert 01 serial-contains 'web: parse nodes='
vgate_assert 01 serial-contains 'web: layout blocks='
vgate_assert 01 serial-contains 'web: url /host/OLIVER.HTML'
vgate_assert 01 serial-contains 'web: paint items='
vgate_assert 01 serial-contains 'web: settled'
vgate_assert 01 serial-contains 'web: repaint items='
vgate_assert 01 serial-contains 'web: ready'
vgate_assert 01 serial-contains 'web: budget startup-ms='
vgate_assert 01 serial-absent 'web: budget over'
vgate_assert 01 serial-absent 'web: error'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-contains 'tls: roots virelai-gate-roots-2026-09-14'
vgate_assert 01 serial-contains 'web: fonts truetype(inter+firacode) ui=truetype mono=truetype'
vgate_assert 01 serial-absent 'web: fonts bitmap8x8'
vgate_assert 01 serial-contains 'web: text face=truetype(inter+firacode) proportional=yes'
vgate_assert 01 serial-contains ' body-lineh=18'
vgate_assert 01 serial-contains ' h1-lineh=33'
vgate_assert 01 serial-contains ' mono-lineh=17'
vgate_assert 01 serial-contains ' adv-i=3'
vgate_assert 01 serial-contains ' adv-W=13'
vgate_assert 01 serial-contains ' adv-space=4'
vgate_assert 01 serial-contains ' bold-face=yes'
vgate_assert 01 serial-contains ' bold-heavier=yes'
vgate_assert 01 serial-contains ' italic-face=yes'
vgate_assert 01 serial-absent 'proportional=no'
vgate_assert 01 serial-absent 'adv-i=8'
vgate_assert 01 snapshot 'snap-01-*.raw' <<'PY'
import subprocess, sys
subprocess.check_call([sys.executable, 'user/go/browser/pixel_probe.py', sys.argv[1], 'oliver'])
PY

# 02: Regular/Bold text, table, decoded QOI quadrants, and link.
vgate_run 02 -- \
    --screen '$RUN_DIR/screen' --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-02' --script '$RUN_DIR/script-depth.txt' \
    --snapshot-after 'web: settled' --snapshot-after 'web: repaint' \
    --script-expect 'web: ready' --timeout 120

vgate_assert 02 serial-absent 'web: budget over'
vgate_assert 02 serial-contains 'web: url /host/DEPTH.HTML'
vgate_assert 02 serial-contains 'web: parse nodes='
vgate_assert 02 serial-contains 'web: layout blocks='
vgate_assert 02 serial-contains 'web: settled'
vgate_assert 02 serial-contains 'web: repaint items='
vgate_assert 02 serial-contains 'web: ready'
vgate_assert 02 serial-contains 'web: fonts truetype(inter+firacode)'
vgate_assert 02 serial-contains ' bold-face=yes'
vgate_assert 02 serial-contains ' bold-heavier=yes'
vgate_assert 02 serial-absent 'web: error'
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 snapshot 'snap-02-*.raw' <<'PY'
import subprocess, sys
subprocess.check_call([sys.executable, 'user/go/browser/pixel_probe.py', sys.argv[1], 'depth'])
PY

# 03: remove every face through the actual host file channel. Distinct grid
# hash is the falsification: a grid cannot pass the real-face expectation.
vgate_run 03 -- \
    --screen '$RUN_DIR/screen' --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-03' --script '$RUN_DIR/script-nofont.txt' \
    --snapshot-after 'web: settled' --snapshot-after 'web: repaint' \
    --script-expect 'web: ready' --timeout 120

vgate_assert 03 serial-absent 'web: budget over'
vgate_assert 03 serial-contains 'web: fonts bitmap8x8 ui=missing mono=missing'
vgate_assert 03 serial-contains 'web: text face=bitmap8x8 proportional=no'
vgate_assert 03 serial-contains ' bold-face=no'
vgate_assert 03 serial-contains ' italic-face=no'
vgate_assert 03 serial-contains ' adv-i=8'
vgate_assert 03 serial-contains ' adv-W=8'
vgate_assert 03 serial-contains ' h1-lineh=18'
vgate_assert 03 serial-absent 'proportional=yes'
vgate_assert 03 serial-contains 'web: parse nodes='
vgate_assert 03 serial-contains 'web: settled'
vgate_assert 03 serial-contains 'web: repaint items='
vgate_assert 03 serial-contains 'web: ready'
vgate_assert 03 serial-absent 'web: error'
vgate_assert 03 serial-absent '[EXC] parking:'
vgate_assert 03 snapshot 'snap-03-*.raw' <<'PY'
import subprocess, sys
subprocess.check_call([sys.executable, 'user/go/browser/pixel_probe.py', sys.argv[1], 'oliver-grid'])
PY

# 04: definition-list indentation and visible missing-image alt box.
vgate_run 04 -- \
    --screen '$RUN_DIR/screen' --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-04' --script '$RUN_DIR/script-lists.txt' \
    --snapshot-after 'web: repaint' \
    --script-expect 'web: ready' --timeout 120

vgate_assert 04 serial-absent 'web: budget over'
vgate_assert 04 serial-contains 'web: url /host/LISTS.HTML'
vgate_assert 04 serial-contains 'web: parse nodes='
vgate_assert 04 serial-contains 'web: layout blocks='
vgate_assert 04 serial-contains 'web: settled'
vgate_assert 04 serial-absent 'web: error'
vgate_assert 04 serial-absent '[EXC] parking:'
vgate_assert 04 snapshot 'snap-04-0.raw' <<'PY'
import subprocess, sys
subprocess.check_call([sys.executable, 'user/go/browser/pixel_probe.py', sys.argv[1], 'lists-grid'])
PY

# 05: real guest PNG decode; no face is needed. A placeholder cannot satisfy
# the exact host swatch hash or its four unique decoded colors.
vgate_run 05 -- \
    --screen '$RUN_DIR/screen' --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-05' --script '$RUN_DIR/script-png.txt' \
    --snapshot-after 'web: repaint' \
    --script-expect 'web: ready' --timeout 120

vgate_assert 05 serial-contains 'web: url /host/PNG.HTML'
vgate_assert 05 serial-contains 'web: parse nodes='
vgate_assert 05 serial-contains 'web: layout blocks='
vgate_assert 05 serial-contains 'web: paint items='
vgate_assert 05 serial-contains 'web: repaint items='
vgate_assert 05 serial-contains 'web: ready'
vgate_assert 05 serial-absent 'web: error'
vgate_assert 05 serial-absent 'web: budget over'
vgate_assert 05 serial-absent '[EXC] parking:'
vgate_assert 05 snapshot 'snap-05-*.raw' <<'PY'
import subprocess, sys
subprocess.check_call([sys.executable, 'user/go/browser/pixel_probe.py', sys.argv[1], 'png-grid'])
PY
