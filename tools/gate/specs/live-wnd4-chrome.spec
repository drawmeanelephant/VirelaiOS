# live-wnd4-chrome.spec -- WMS4 Chrome parity (issue #624)
#
# M66c (#1485): the Zig notepad is retired; the client is NOTE.ELF (Go), which
# declares the same 56,56 512x384 rect. Chrome pixels are kernel-painted, so
# only the app-painted client colour and the `note:` prefix moved.
#
# HOST PREREQUISITE: bash tools/go/build-note.sh -> .build/go/NOTE.ELF

vgate_name live-wnd4-chrome "WMS4 Chrome parity: focused ring/title/close & unfocused metrics"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script-A.txt <<'EOF'
wnd start
exec NOTE.ELF
EOF

vgate_file s2-A.txt <<'EOF'
wm
echo chrome-a
EOF

vgate_file s3-A.txt <<'EOF'
echo done-a
EOF

vgate_file script-B.txt <<'EOF'
wnd start
exec NOTE.ELF
EOF

vgate_file s2-B.txt <<'EOF'
dui focus 0
echo chrome-b
EOF

vgate_file s3-B.txt <<'EOF'
echo done-b
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
src = os.path.join(".build", "go", "NOTE.ELF")
if not os.path.exists(src):
    sys.exit("NOTE.ELF missing (expected " + src + ") - build it first: "
             "bash tools/go/build-note.sh")
shutil.copy(src, os.path.join(share, "NOTE.ELF"))
print("staged NOTE.ELF into share (%d bytes)" % os.path.getsize(os.path.join(share, "NOTE.ELF")))
PY

# --- boot A: focused chrome parity ---
# The snapshot requests on chrome-a/chrome-b themselves. A later 30s wait
# before the final echo let the kernel's SMP worker trace flood serial input
# (observed on this host); the runner's snapshot-drain timeout still holds
# the VM for the kind-4 response after the shorter 3s terminal settle.
vgate_run A -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-A' \
    --script '$RUN_DIR/script-A.txt' \
    --script2 '$RUN_DIR/s2-A.txt' --script2-after "note: ready" --script2-delay 20 \
    --script3 '$RUN_DIR/s3-A.txt' --script3-after "chrome-a" --script3-delay 3 \
    --snapshot-after "chrome-a" \
    --script-expect "done-a" --timeout 180

vgate_assert A serial-contains "wm: chrome window id="
vgate_assert A serial-contains "kind=0x7f"
vgate_assert A serial-absent "[EXC] parking:"
vgate_assert A python <<'PY'
import os, re
ser = open(os.environ["VG_SER"]).read()
m_subs = re.search(r'wm: chrome submissions=([0-9]+)', ser)
assert m_subs and int(m_subs.group(1)) >= 1, "submissions check failed"
m_pol = re.search(r'policy_kind=(0x[0-9a-f]+)', ser)
assert m_pol and m_pol.group(1) == "0x7f", "policy check failed"
PY

vgate_assert A snapshot 'snap-A-*.raw' <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
w = 1280
def px(x, y):
    k = (y * w + x) * 4
    return (data[k + 2], data[k + 1], data[k])
X, Y, W, H = 56, 56, 512, 384
ok = True
for dy in (0, 1, 2):
    n = sum(1 for dx in range(8, W - 8, 16) if px(X + dx, Y + dy) == (59, 130, 246))
    ok &= n >= (W - 16) // 16 - 2
assert ok, "ring_top missing"
ink = sum(1 for x in range(X + 20, X + W - 40, 2) for y in range(Y + 3, Y + 16)
          if px(x, y)[0] > 200 and px(x, y)[1] > 200 and px(x, y)[2] > 200)
assert ink >= 30, f"label ink too low: {ink}"
red = sum(1 for x in range(X + W - 15, X + W - 5) for y in range(Y + 3, Y + 13)
          if px(x, y)[0] > 170 and px(x, y)[1] < 120 and px(x, y)[2] < 120)
assert red >= 6, f"close red too low: {red}"
PY

# --- boot B: unfocused chrome parity ---
vgate_run B -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-B' \
    --script '$RUN_DIR/script-B.txt' \
    --script2 '$RUN_DIR/s2-B.txt' --script2-after "note: ready" --script2-delay 20 \
    --script3 '$RUN_DIR/s3-B.txt' --script3-after "chrome-b" --script3-delay 3 \
    --snapshot-after "chrome-b" \
    --script-expect "done-b" --timeout 180

vgate_assert B serial-absent "[EXC] parking:"
vgate_assert B snapshot 'snap-B-*.raw' <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
w = 1280
def px(x, y):
    k = (y * w + x) * 4
    return (data[k + 2], data[k + 1], data[k])
BORDER = (0x47, 0x55, 0x69)
TITLE  = (0x1a, 0x2b, 0x3c)
# NOTE.ELF paints theme.Current.Bg, now 0x182026 in the default dark
# palette (note/main.go). The unfocused rest blend measures 23,31,37;
# ±6 absorbs that blend without accepting the chrome plate as client.
CLIENT = (0x18, 0x20, 0x26)
X, Y, W, H = 56, 56, 512, 384
fails = []
def near(c, want, tol=6):
    return all(abs(a - b) <= tol for a, b in zip(c, want))
for xx in (X, X + 1, X + W - 2, X + W - 1):
    good = sum(1 for yy in range(Y + 20, Y + H - 6, 24) if near(px(xx, yy), BORDER))
    need = len(range(Y + 20, Y + H - 6, 24))
    if good < need - 1: fails.append(f"border col {xx-X}: {good}/{need}")
third_bad = sum(1 for yy in range(Y + 20, Y + H - 6, 24) if near(px(X + 2, yy), BORDER))
if third_bad > 2: fails.append(f"border thicker than 2px ({third_bad} hits at col+2)")
for yy in (Y + H - 2, Y + H - 1):
    good = sum(1 for xx in range(X + 8, X + W - 8, 32) if near(px(xx, yy), BORDER))
    need = len(range(X + 8, X + W - 8, 32))
    if good < need - 1: fails.append(f"border row {yy-Y}: {good}/{need}")
band = tot = 0
for yy in range(Y + 2, Y + 16):
    for xx in range(X + 4, X + W - 40, 3):
        tot += 1
        if near(px(xx, yy), TITLE): band += 1
if band < int(tot * 0.55): fails.append(f"title band bg {band}/{tot}")
ink = sum(1 for xx in range(X + 20, X + W - 40, 2) for yy in range(Y + 2, Y + 16)
          if px(xx, yy) == (255, 255, 255))
if ink < 30: fails.append(f"title label ink {ink}")
cl = sum(1 for xx in range(X + 420, X + 504, 4) for yy in range(Y + 300, Y + 372, 4)
         if near(px(xx, yy), CLIENT))
if cl < 100: fails.append(f"client bg {cl}")
red = sum(1 for xx in range(X + W - 15, X + W - 5) for yy in range(Y + 3, Y + 13)
          if px(xx, yy)[0] > 170 and px(xx, yy)[1] < 120 and px(xx, yy)[2] < 120)
if red < 6: fails.append(f"close glyph red {red}")
assert not fails, "CHROME-FAILS: " + "; ".join(fails)
PY

# M86d: one app-facing chrome surface, the appkit prompt dialog. This third
# boot leaves A/B's window-manager chrome assertions untouched. GODIALOG owns
# a Seam-B window buffer and calls appkit.Dialog.Draw on draw.BufferCanvas;
# the shell echo is delayed from its post-present marker to let the compositor
# pick up the buffer before the kind-4 raw snapshot.
#
# HOST PREREQUISITE:
#   GO_FORK_DIR=<fork> GO_BUILD_NAME=GODIALOG GO_LDFLAGS_VALUE='-s -w' \
#     bash tools/go/build-go.sh user/go/appkit/demo/main.go
vgate_file script-C.txt <<'EOF'
wnd start
exec GODIALOG.ELF
EOF

vgate_file s2-C.txt <<'EOF'
echo chrome-dialog
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
share = os.environ.get("VG_SHARE") or os.path.join(os.environ["RUN_DIR"], "share")
src = os.path.join(".build", "go", "GODIALOG.ELF")
if not os.path.exists(src):
    sys.exit("GODIALOG.ELF missing: build user/go/appkit/demo/main.go first")
shutil.copy(src, os.path.join(share, "GODIALOG.ELF"))
shutil.copy(os.path.join("image", "fonts", "VirelaiChrome-Regular.ttf"),
            os.path.join(share, "CHROME.TTF"))
PY

vgate_run C -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-C' \
    --script '$RUN_DIR/script-C.txt' \
    --script2 '$RUN_DIR/s2-C.txt' --script2-after 'godialog: painted' --script2-delay 4 \
    --snapshot-after 'chrome-dialog' \
    --script-expect 'chrome-dialog' --timeout 180

vgate_assert C serial-contains 'godialog: painted'
vgate_assert C serial-absent 'godialog: surface failed'
vgate_assert C serial-absent 'godialog: fonts missing'
vgate_assert C serial-absent '[EXC] parking:'
vgate_assert C snapshot 'snap-C-*.raw' <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
W, H = 1280, 720
assert len(data) == W * H * 4, "wrong scanout size"
def px(x, y):
    k = (y * W + x) * 4
    return data[k + 2], data[k + 1], data[k]

# Window origin 56,56; the appkit dialog rect 96,72 320x156 is therefore
# 152,128..472,284. Inspect the one drawn surface at distinct semantic
# anchors, not the serial marker alone.
bg = (24, 32, 38)
plate = (34, 45, 53)
border = (51, 65, 85)
recess = (17, 23, 28)
accent = (59, 130, 246)
assert px(150, 126) == bg, "window background changed"
assert px(156, 164) == plate, "rounded plate absent"
assert px(320, 128) == border, "dialog top stroke absent"
assert px(350, 225) == recess, "input well absent"
# The rounded corner must not be a square fill: the very first pixel is
# background, and the inside has a partially covered antialiased edge.
assert px(152, 128) == bg, "dialog corner is square"
edge = [px(x, y) for y in range(129, 136) for x in range(153, 160)]
assert any(p != bg and p != plate and p != border for p in edge), "AA edge missing"
assert px(340, 245) == accent, "focused/default button ring absent"
icon = sum(1 for y in range(140, 164) for x in range(164, 180)
           if px(x, y)[2] > px(x, y)[0] + 50)
assert icon >= 8, f"tinted Virelai Chrome glyph missing: {icon}"
ink = sum(1 for y in range(144, 164) for x in range(184, 360)
          if all(v > 180 for v in px(x, y)))
assert ink >= 20, f"13px title ink missing: {ink}"
print(f"dialog chrome: AA edge, plate, stroke, input, focus, icon={icon}, title={ink}")
PY
