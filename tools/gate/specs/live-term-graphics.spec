# M85c: the pinned 16x32 QOI becomes four sixel-colour tiles on a REAL
# window-bound tty. The shim gives IMGCAT a stable 64,48,640,400 rect.
# Run 01 checks the 2x2 cells at row 2, col 4. Run 02 scrolls one row,
# checks both rows at row 1/2, then clears and checks the empty scanout.
# Serial markers only order captures; none is image or placement evidence.
# HOST PREREQUISITE: bash tools/go/build-imgcat.sh -> .build/go/IMGCAT.ELF
# exec-order: assert-proven -- image/scrolled/cleared come only from
# IMGCAT after the tty write, and the second run ends on its clear marker.

vgate_name live-term-graphics "M85c: imgcat sixel image, scroll and clear are real scanout pixels"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_setup_python <<'PY'
import hashlib, os, shutil, sys
share = os.path.join(os.environ["RUN_DIR"], "share")
src = ".build/go/IMGCAT.ELF"
fixture = "user/go/imgcat/testdata/quadrants.qoi"
if not os.path.isfile(src):
    sys.exit("IMGCAT.ELF missing; run bash tools/go/build-imgcat.sh")
digest = hashlib.sha256(open(fixture, "rb").read()).hexdigest()
assert digest == "526e75a76e0315686ba9ae02252f49627cc24db6ea2ba2728957687cfa38cfed", digest
shutil.copy(src, os.path.join(share, "IMGCAT.ELF"))
shutil.copy(fixture, os.path.join(share, "QUADS.QOI"))
print("staged IMGCAT.ELF + pinned %d-byte QOI into host share" % os.path.getsize(fixture))
PY

vgate_file boot.txt <<'EOF'
exec IMGCAT.ELF /host/QUADS.QOI
EOF

vgate_file scroll-settled.txt <<'EOF'
echo scroll-settled
EOF

vgate_file image-settled.txt <<'EOF'
echo image-settled
EOF

vgate_file clear-settled.txt <<'EOF'
echo clear-settled
EOF

vgate_run 01 -- --screen '$RUN_DIR/screen-01' --via-virtio \
    --cvc-snap --snapshot-out '$RUN_DIR/image' \
    --snapshot-after 'image-settled' \
    --script '$RUN_DIR/boot.txt' \
    --script2 '$RUN_DIR/image-settled.txt' \
    --script2-after 'imgcat: image 16x32' --script2-delay 4 \
    --script-expect 'image-settled' --script-expect-tail 3 \
    --timeout 90

vgate_assert 01 serial-contains 'exec: loaded IMGCAT.ELF'
vgate_assert 01 serial-contains 'imgcat: image 16x32'
vgate_assert 01 serial-contains 'image-settled'
vgate_assert 01 serial-absent 'imgcat: tty write refused'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 snapshot 'image-0.raw' <<'PY'
import sys
raw = open(sys.argv[1], "rb").read()
assert len(raw) == 1280 * 720 * 4, "not a 1280x720 BGRA guest scanout"
def rgb(x, y):
    i = (y * 1280 + x) * 4
    return (raw[i+2], raw[i+1], raw[i])
# Window client starts at (64,64), image origin CSI 3;5 H = (96,96).
# Four full-colour quadrant tiles, each exactly 8x16 pixels.
want = {(99, 100): (255, 0, 0), (107, 100): (0, 255, 0),
        (99, 116): (0, 0, 255), (107, 116): (255, 255, 0)}
for xy, c in want.items():
    assert rgb(*xy) == c, f"image tile {xy} = {rgb(*xy)}, want {c}"
bg = rgb(99, 84)
assert bg not in want.values(), f"control above image is an image colour: {bg}"
print("image 2x2 tiles RGB", [rgb(*xy) for xy in want], "control", bg)
PY

# One kind-4 capture per boot: the snapshot channel coalesces closely spaced
# requests. Run 02's scroll frame is a raw capture after a monitor settle
# marker; the clear frame is a separate host screenshot PNG after a later
# image-owning marker. The 5 s second phase gives the scroll frame time to
# complete before the clear key arrives.
vgate_run 02 -- --screen '$RUN_DIR/screen-02' --via-virtio \
    --cvc-snap --snapshot-out '$RUN_DIR/scroll' \
    --snapshot-after 'scroll-settled' \
    --screenshot-after 'clear-settled' \
    --script '$RUN_DIR/boot.txt' \
    --input-chords 's' --input-chords-after 'imgcat: image 16x32' \
    --script2 '$RUN_DIR/scroll-settled.txt' \
    --script2-after 'imgcat: scrolled' --script2-delay 4 \
    --input-string 'c' --input-string-after 'scroll-settled' \
    --script3 '$RUN_DIR/clear-settled.txt' \
    --script3-after 'imgcat: cleared' --script3-delay 4 \
    --script-expect 'clear-settled' --script-expect-tail 3 \
    --timeout 120

vgate_assert 02 serial-contains 'imgcat: image 16x32'
vgate_assert 02 serial-contains 'imgcat: scrolled'
vgate_assert 02 serial-contains 'scroll-settled'
vgate_assert 02 serial-contains 'imgcat: cleared'
vgate_assert 02 serial-contains 'clear-settled'
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 snapshot 'scroll-0.raw' <<'PY'
import sys
raw = open(sys.argv[1], "rb").read()
assert len(raw) == 1280 * 720 * 4, "not a 1280x720 BGRA guest scanout"
def rgb(x, y):
    i = (y * 1280 + x) * 4
    return (raw[i+2], raw[i+1], raw[i])
# CSI S moves the image cells UP a row, not merely a serial marker.
want = {(99, 84): (255, 0, 0), (107, 84): (0, 255, 0),
        (99, 100): (0, 0, 255), (107, 100): (255, 255, 0)}
for xy, c in want.items():
    assert rgb(*xy) == c, f"scrolled tile {xy} = {rgb(*xy)}, want {c}"
assert rgb(99, 116) not in want.values(), "the old bottom row did not move"
print("scrolled 2x2 tiles RGB", [rgb(*xy) for xy in want])
PY

vgate_assert 02 snapshot 'screen-02-after' <<'PY'
import sys, struct, zlib
d = open(sys.argv[1], "rb").read()
assert d[:8] == b"\x89PNG\r\n\x1a\n", "not a host scanout PNG"
pos, chunks, width, height, ct = 8, [], 0, 0, 0
while pos < len(d):
    n, typ = struct.unpack(">I4s", d[pos:pos+8])
    body = d[pos+8:pos+8+n]
    if typ == b"IHDR":
        width, height, depth, ct = struct.unpack(">IIBB", body[:10])
        assert depth == 8 and ct in (2, 6), "unsupported scanout PNG"
    if typ == b"IDAT":
        chunks.append(body)
    pos += n + 12
assert width in (1280, 2560) and height == width * 720 // 1280, "scanout dimensions drifted"
bpp = 4 if ct == 6 else 3
stride = width * bpp
raw = zlib.decompress(b"".join(chunks))
prev = bytearray(stride)
scale = width // 1280
targets = {(x*scale, y*scale) for x in (99, 107) for y in (84, 100, 116)}
colours = {}
i = 0
for y in range(height):
    filt = raw[i]; i += 1
    row = bytearray(raw[i:i+stride]); i += stride
    for x in range(stride):
        a = row[x-bpp] if x >= bpp else 0
        b = prev[x]
        c = prev[x-bpp] if x >= bpp else 0
        if filt == 1: add = a
        elif filt == 2: add = b
        elif filt == 3: add = (a+b)//2
        elif filt == 4:
            p = a+b-c
            add = min((a,b,c), key=lambda v: (abs(p-v), (a,b,c).index(v)))
        else: add = 0
        row[x] = (row[x] + add) & 255
    for x, yy in targets:
        if y == yy:
            colours[(x, y)] = tuple(row[x*bpp:x*bpp+3])
    prev = row
assert len(colours) == len(targets), "missing sampled scanout pixels"
assert len(set(colours.values())) == 1, f"clear left image pixels: {colours}"
assert next(iter(colours.values())) not in ((255, 0, 0), (0, 255, 0),
                                             (0, 0, 255), (255, 255, 0)), \
    f"clear is still an image colour: {colours}"
print("clear scanout cells all background:", colours)
PY
