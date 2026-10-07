# live-web-ttf.spec -- issue #1346 (class B): Go EL0 paints REAL TrueType.
#
# WEB.ELF is a Go EL0 program over virelai/webrender. Before this card its
# renderer drew the 8x8 integer-scaled bitmap: every advance was 8px, so an h1
# band was a grid rather than a typeface. This gate is what says otherwise, and
# it is written so a silent regression back to the grid FAILS rather than
# merely looking wrong in a screenshot.
#
# Three boots, each proving one thing the other two cannot:
#
#   01  the oliver fixture, faces present. The engine reports measurable facts
#       (per-glyph advances, line heights) and the page's FIRST INKED BAND is
#       measured in pixels: Inter at 13/26px gives an h1 band 20 rows tall
#       spanning 294px, the 8x8 bitmap gives 14 rows and 347px. The probe
#       requires the Inter numbers.
#
#   02  layout depth (ADR 0028 S2-S4) on a page whose table, image and link are
#       all ABOVE the fold: a table header rule, a link underline, and a 32x32
#       QOI image written by this spec's setup hook whose four quadrant colours
#       appear nowhere else on the page. Finding those exact colours is
#       evidence that the image DECODED and painted, not that a box appeared.
#
#   03  the SAME page as 01 with both faces removed from the share over the host
#       file channel (`vf <verb>`, see the script below). The app must fall
#       back, say so on the wire, still paint the page - and produce THE GRID
#       numbers. Boot 03 is the falsification of boot 01: without it, a probe
#       that always passed would look identical. It runs LAST because it
#       mutates the share: the deletion would otherwise be inherited by every
#       later boot (learned by running it second, when boot 03 came up with
#       `web: fonts bitmap8x8 ui=missing mono=missing`).
#
# Why 01 does not probe the table: the oliver fixture lays out 545px tall in a
# 322px content box, so its <table> and <hr> rules sit below the fold (measured
# on the host - the only in-view rules are the two link underlines). Boot 03 is
# where table depth is visible.
#
# M71i (#1568): boot 04 INHERITS two rungs from the retired Zig specs --
# live-doc-tables' `dl/dt/dd` (the S2 half DEPTH.HTML does not carry) and
# live-doc-web boot 01's missing-`<img>` placeholder. Both are pinned HERE
# rather than in a new spec: this file already owns S2/S3/S4 on the Go path,
# and the card's rule is extend-don't-add. The S2 table rule, the S3 image
# decode and the S4 link accent stay where boot 02 already pins them.
#
# HOST PREREQUISITE (fails the gate honestly when missing):
#   .build/go/WEB.ELF  --  bash tools/go/build-web.sh browser WEB
#
# The faces are seeded by `vgate_share seed` (tools/lib/gate-run.sh). Share
# names frozen by M69d #1531 D1: /host/INTER.TTF, /host/INTERB.TTF,
# /host/INTERI.TTF, /host/FIRACODE.TTF. Boot 01/02 require Bold so <strong>
# cannot be the old Regular+1px strike; boot 03 deletes every face.

vgate_name live-web-ttf "issue #1346: WEB.ELF paints real TrueType (Inter) on the oliver fixture, falls back to the 8x8 grid when the face is gone, and renders tables/images/links"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script-oliver.txt <<'EOF'
exec WEB.ELF /host/OLIVER.HTML
EOF

# Boot 02 takes the faces away first, through the HOST FILE CHANNEL, not a bare
# verb: the guest shell's `vf` command owns these file ops
# (kernel/src/monitor.zig cmd_vf_rm; the bare verbs only appear in the shell's
# completion table). The first version of this spec typed a bare verb, the
# shell ignored it, the faces were still loaded and the fallback never ran -
# the `vf ls` line below is there so the transcript shows the deletion.
vgate_file script-nofont.txt <<'EOF'
vf rm INTER.TTF
vf rm INTERB.TTF
vf rm INTERI.TTF
vf rm FIRACODE.TTF
vf ls
exec WEB.ELF /host/OLIVER.HTML
EOF

vgate_file script-depth.txt <<'EOF'
exec WEB.ELF /host/DEPTH.HTML
EOF

vgate_file script-lists.txt <<'EOF'
exec WEB.ELF /host/LISTS.HTML
EOF

vgate_file script-png.txt <<'EOF'
exec WEB.ELF /host/PNG.HTML
EOF

# M93e: original PNG bytes, built by the host fixture generator but decoded
# and painted by WEB in the guest. No host-decoded raster is staged.
vgate_setup_python <<'PY'
import binascii, os, struct, zlib
share = os.environ.get("VG_SHARE") or os.path.join(os.environ["RUN_DIR"], "share")
W = H = 32
colors = [(0x11, 0x7f, 0x33), (0xd0, 0x33, 0x99),
          (0x22, 0x66, 0xdd), (0xee, 0xcc, 0x00)]
def chunk(kind, body):
    return (struct.pack(">I", len(body)) + kind + body +
            struct.pack(">I", binascii.crc32(kind + body) & 0xffffffff))
rows = bytearray()
for y in range(H):
    rows.append(0)
    for x in range(W):
        rows.extend(colors[(y * 2 // H) * 2 + x * 2 // W])
        rows.append(255)
png = (b"\x89PNG\r\n\x1a\n" +
       chunk(b"IHDR", struct.pack(">IIBBBBB", W, H, 8, 6, 0, 0, 0)) +
       chunk(b"IDAT", zlib.compress(bytes(rows))) + chunk(b"IEND", b""))
with open(os.path.join(share, "SWATCH.PNG"), "wb") as f:
    f.write(png)
with open(os.path.join(share, "PNG.HTML"), "wb") as f:
    f.write(b'<img src="SWATCH.PNG" width="32" height="32" alt="guest PNG">')
print("M93e PNG fixture: %d source bytes, 32x32, RGBA8 non-interlaced" % len(png))
PY

vgate_setup_python <<'PY'
# Boot 02's page and its image. The swatch carries four colours that appear
# nowhere else on the page, so the pixel probe can assert the DECODE rather
# than merely "something was painted". QOI_OP_RGB (0xfe) per pixel is the
# simplest legal encoding and keeps this fixture readable by eye.
import os, struct, sys
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
W = H = 32
QUADS = [(0x11, 0x7f, 0x33), (0xd0, 0x33, 0x99), (0x22, 0x66, 0xdd), (0xee, 0xcc, 0x00)]
out = bytearray(b"qoif") + struct.pack(">II", W, H) + bytes([4, 0])
for y in range(H):
    for x in range(W):
        c = QUADS[(y * 2 // H) * 2 + (x * 2 // W)]
        out += bytes([0xfe, c[0], c[1], c[2]])
out += bytes([0, 0, 0, 0, 0, 0, 0, 1])
with open(os.path.join(share, "SWATCH.QOI"), "wb") as fh:
    fh.write(bytes(out))
page = (b"<p>MMMMMMMM</p>"
        b"<p><strong>MMMMMMMM</strong></p>"
        b"<h4>Depth</h4>"
        b"<table><thead><tr><th>Left</th><th>Right</th></tr></thead>"
        b"<tbody><tr><td>one</td><td>two</td></tr></tbody></table>"
        b"<p>An image:</p><img src=\"SWATCH.QOI\" alt=\"swatch\">"
        b"<p><a href=\"NEXT.HTML\">a link</a></p>")
with open(os.path.join(share, "DEPTH.HTML"), "wb") as fh:
    fh.write(page)
# M71i (#1568): boot 04's fixture -- the two structures the retired live-doc
# specs pinned alone. `<dl>` is ADR 0028 S2 (tables AND definition lists) and
# DEPTH.HTML does not carry it; the second <img> names a file that is not on
# the share, which is the migration's other inherited probe: a missing source
# is a VISIBLE placeholder box, never a blank (renderer rule D5).
lists = (b"<h4>Lists</h4>"
         b"<dl><dt>term</dt><dd>definition sits indented</dd></dl>"
         b"<p><img src=\"NOPE.QOI\" alt=\"missing\"></p>")
with open(os.path.join(share, "LISTS.HTML"), "wb") as fh:
    fh.write(lists)
print("boot 02 fixture: SWATCH.QOI %d bytes (%dx%d), DEPTH.HTML %d bytes, "
      "LISTS.HTML %d bytes" % (len(out), W, H, len(page), len(lists)))
PY

vgate_setup_python <<'PY'
import os, shutil, sys
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
src = os.path.join(".build", "go", "WEB.ELF")
if not os.path.exists(src):
    sys.exit("WEB.ELF missing (expected " + src + ") - build it first: "
             "bash tools/go/build-web.sh browser WEB")
shutil.copy(src, os.path.join(share, "WEB.ELF"))
# The oliver fixture: the pinned HTML this project's own Zig CLI emits, and the
# page ADR 0028 exists to display.
fixture = os.path.join("tests", "oliver-spike", "expect.html")
if not os.path.exists(fixture):
    sys.exit("oliver fixture missing at " + fixture)
shutil.copy(fixture, os.path.join(share, "OLIVER.HTML"))
for face in ("INTER.TTF", "INTERB.TTF", "INTERI.TTF", "FIRACODE.TTF"):
    p = os.path.join(share, face)
    if not os.path.exists(p):
        sys.exit("gate 01 needs the seeded face " + p)
print("staged WEB.ELF (%d bytes) + OLIVER.HTML + DEPTH.HTML + LISTS.HTML; "
      "faces present: %s"
      % (os.path.getsize(os.path.join(share, "WEB.ELF")),
         ", ".join(sorted(f for f in os.listdir(share) if f.endswith(".TTF")))))
PY

# --- boot 01: the oliver fixture, in Inter -------------------------------
vgate_run 01 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-01' \
    --script '$RUN_DIR/script-oliver.txt' \
    --snapshot-after "web: settled" \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 120

# The pre-existing WEB.ELF markers must all still be there: this card adds
# typography, it does not change the browser's contract.
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

# The face actually loaded, and the engine is the proportional one.
vgate_assert 01 serial-contains 'web: fonts truetype(inter+firacode) ui=truetype mono=truetype'
vgate_assert 01 serial-absent 'web: fonts bitmap8x8'
# The measured facts. On the 8x8 fallback these are 8/8/8 and 10/18; on Inter
# they are 3/13/4 and 18/33. Asserted numerically, not by adjective.
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

# The pixel probe: measure the page's FIRST INKED BAND inside the content box.
# Inter: 20 rows tall, 294px wide. The 8x8 grid: 14 rows, 347px. Both directions
# are asserted, so neither "rendered something" nor "rendered the grid" passes.
vgate_assert 01 snapshot 'snap-01-*.raw' <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
w = 1280
X, Y = 40, 28            # the browser window origin on the scanout
CX, CY, CW, CH = 8, 50, 496, 322   # the content box, window-local
def px(x, y):
    k = (y * w + x) * 4
    return (data[k + 2], data[k + 1], data[k])
def near(c, want, tol=6):
    return all(abs(a - b) <= tol for a, b in zip(c, want))
PAGE_BG = (0x18, 0x20, 0x26)
INK     = (0xe6, 0xed, 0xf3)
ACCENT  = (0x3b, 0x82, 0xf6)

rows = []
for yy in range(CY, CY + CH):
    n = 0
    for xx in range(CX, CX + CW):
        if not near(px(X + xx, Y + yy), PAGE_BG, 12):
            n += 1
    rows.append(n)
first = -1
for i, n in enumerate(rows):
    if n > 0:
        first = i
        break
assert first >= 0, "the content box painted nothing at all"
last = first
while last < len(rows) and rows[last] > 0:
    last += 1
band_h = last - first
minx, maxx = None, None
for yy in range(CY + first, CY + last):
    for xx in range(CX, CX + CW):
        if not near(px(X + xx, Y + yy), PAGE_BG, 12):
            if minx is None or xx < minx:
                minx = xx
            if maxx is None or xx > maxx:
                maxx = xx
span = maxx - minx + 1

# The band has to be TEXT, which is the part a shape-only test cannot say: its
# pixels must be the body ink colour, and its densest row must break into many
# runs. Window chrome is a different colour, and a solid fill or a chrome strip
# is one long run -- so both checks fail for anything that is not text.
band_ink = 0
for yy in range(CY + first, CY + last):
    for xx in range(CX + minx, CX + maxx + 1):
        if near(px(X + xx, Y + yy), INK, 2):
            band_ink += 1
densest, runs = 0, 0
for yy in range(CY + first, CY + last):
    n, row_runs, prev = 0, 0, False
    for xx in range(CX + minx, CX + maxx + 1):
        on = not near(px(X + xx, Y + yy), PAGE_BG, 12)
        if on:
            n += 1
            if not prev:
                row_runs += 1
        prev = on
    if n > densest:
        densest, runs = n, row_runs
print("live-web-ttf 01: h1 band rows=[%d,%d) height=%d span=%d ink-px=%d densest-row=%d runs=%d"
      % (first, last, band_h, span, band_ink, densest, runs))

# The fixture's two links: their text plus the underlines, in the accent colour.
accent = 0
for yy in range(CY, CY + CH):
    for xx in range(CX, CX + CW):
        if near(px(X + xx, Y + yy), ACCENT, 8):
            accent += 1
print("live-web-ttf 01: link-accent px=%d" % accent)

fails = []
# An 8x8-grid h1 is 14 rows tall and 347px wide; Inter's is 20 rows and 294px.
if band_h < 17:
    fails.append("h1 band height %d (the 8x8 grid gives 14, Inter gives 20)" % band_h)
if span > 320:
    fails.append("h1 band span %d (the 8x8 grid gives 347, Inter gives 294)" % span)
if span < 200:
    fails.append("h1 band span %d is implausibly narrow" % span)
if band_ink < 40:
    fails.append("the band carries only %d body-ink px: that band is not text" % band_ink)
if runs < 6:
    fails.append("the band's densest row breaks into %d run(s): a fill or a chrome strip, not text" % runs)
if accent < 20:
    fails.append("link accent pixels %d (two links plus their underlines)" % accent)
assert not fails, "WEB-TTF-FAILS: " + "; ".join(fails)
print("live-web-ttf 01 pixels ok: Inter h1 band (proved text, not just shape), links present")
PY

# --- boot 02: layout depth (table + decoded QOI image + link) -------------
vgate_run 02 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-02' \
    --script '$RUN_DIR/script-depth.txt' \
    --snapshot-after "web: settled" \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 120

# #1586: EVERY boot asserts the absence, not just boot 01. The
# guest printed `web: budget over startup ...` in all fourteen boots for months
# while only these three looked, which is why the red set looked load-driven.
# Measured then: startup=10048ms (7013ms of it vi.WmPeers retrying a WM seat
# that does not exist on the shim path, 3022ms the four faces). After the fix:
# startup=3025ms, so the absence is now true everywhere and a real regression
# trips it in whichever boot it happens.
vgate_assert 02 serial-absent 'web: budget over'
vgate_assert 02 serial-contains 'web: url /host/DEPTH.HTML'
vgate_assert 02 serial-contains 'web: parse nodes='
vgate_assert 02 serial-contains 'web: settled'
vgate_assert 02 serial-contains 'web: repaint items='
vgate_assert 02 serial-contains 'web: ready'
vgate_assert 02 serial-contains 'web: fonts truetype(inter+firacode)'
vgate_assert 02 serial-absent 'web: error'
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 snapshot 'snap-02-*.raw' <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
w = 1280
X, Y = 40, 28
CX, CY, CW, CH = 8, 50, 496, 322
def px(x, y):
    k = (y * w + x) * 4
    return (data[k + 2], data[k + 1], data[k])
def near(c, want, tol=6):
    return all(abs(a - b) <= tol for a, b in zip(c, want))
PAGE_BG = (0x18, 0x20, 0x26)
RULE = (0x2e, 0x3a, 0x44)
ACCENT = (0x3b, 0x82, 0xf6)
SWATCH = [(0x11, 0x7f, 0x33), (0xd0, 0x33, 0x99), (0x22, 0x66, 0xdd), (0xee, 0xcc, 0x00)]
INK = (0xe6, 0xed, 0xf3)
rule = accent = ink = text_ink = 0
sw = [0, 0, 0, 0]
for yy in range(CY, CY + CH):
    for xx in range(CX, CX + CW):
        c = px(X + xx, Y + yy)
        if not near(c, PAGE_BG, 12):
            ink += 1
        if near(c, INK, 2):
            text_ink += 1
        if near(c, RULE, 6):
            rule += 1
        if near(c, ACCENT, 8):
            accent += 1
        for i, s in enumerate(SWATCH):
            if near(c, s, 2):
                sw[i] += 1
print("live-web-ttf 02: ink=%d text-ink=%d table-rule=%d link-accent=%d swatch-quadrants=%s"
      % (ink, text_ink, rule, accent, sw))
fails = []
# Thresholds are set against what the page actually draws. The first version
# accepted 3 rule px and 5 accent px, which any grey border or any accent pixel
# could have satisfied without the table rule or the link existing; the header
# rule here is a 496px line and the link is a word plus an underline.
if ink < 1200:
    fails.append("the page painted only %d px" % ink)
if text_ink < 150:
    fails.append("only %d px of body ink: the table and paragraphs did not render as text" % text_ink)
if rule < 200:
    fails.append("table header rule px=%d (a 496px rule is expected; tables are ADR 0028 S2)" % rule)
if accent < 20:
    fails.append("link accent px=%d (a word plus an underline is expected; links are S4)" % accent)
for i, n in enumerate(sw):
    if n < 100:
        fails.append("swatch quadrant %d had %d px: the QOI image did not decode" % (i, n))

# M69d #1531: the first two inked bands are Regular M×8 then <strong> M×8.
# A Regular+1px strike widens the whole run by 1px; Inter Bold is designed
# wider and heavier, so span-delta > 1 and bold ink exceeds regular ink.
rows = []
for yy in range(CY, CY + CH):
    n = 0
    for xx in range(CX, CX + CW):
        if not near(px(X + xx, Y + yy), PAGE_BG, 12):
            n += 1
    rows.append(n)
bands = []
i = 0
while i < len(rows):
    if rows[i] == 0:
        i += 1
        continue
    start = i
    while i < len(rows) and rows[i] > 0:
        i += 1
    minx = maxx = None
    band_ink = 0
    for yy in range(CY + start, CY + i):
        for xx in range(CX, CX + CW):
            if not near(px(X + xx, Y + yy), PAGE_BG, 12):
                band_ink += 1
                if minx is None or xx < minx:
                    minx = xx
                if maxx is None or xx > maxx:
                    maxx = xx
    span = 0 if minx is None else (maxx - minx + 1)
    bands.append((start, i, span, band_ink))
print("live-web-ttf 02 strong-probe bands[:4]=%s" % (bands[:4],))
if len(bands) < 2:
    fails.append("need Regular then Strong M-bands, got %d" % len(bands))
else:
    rspan, bspan = bands[0][2], bands[1][2]
    rink, bink = bands[0][3], bands[1][3]
    print("live-web-ttf 02 strong: regular-span=%d bold-span=%d regular-ink=%d bold-ink=%d"
          % (rspan, bspan, rink, bink))
    # Inter matches advances across weights, so span may be equal. The
    # Regular+1px strike is the one that is *exactly* 1px wider. Real Bold
    # is heavier (more ink) without that 1px shift.
    if bspan - rspan == 1:
        fails.append("strong span %d vs regular %d is exactly +1px: the synthetic strike, not Inter Bold" % (bspan, rspan))
    if bink <= rink:
        fails.append("strong ink %d is not heavier than regular %d" % (bink, rink))
assert not fails, "WEB-TTF-DEPTH-FAILS: " + "; ".join(fails)
print("live-web-ttf 02 ok: table rule, link accent, decoded image, and Inter Bold != Regular+1px")
PY
# --- boot 03: the faces removed over the host file channel ----------------
vgate_run 03 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-03' \
    --script '$RUN_DIR/script-nofont.txt' \
    --snapshot-after "web: settled" \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 120

# The removal is proved by the app itself: it opened both share paths and found
# nothing, so its own report is `ui=missing mono=missing`. The transcript also
# echoes the removal commands, which is why the evidence is the engine report
# rather than a substring check on the listing.
# The fallback is announced, not hidden.
vgate_assert 03 serial-absent 'web: budget over'
vgate_assert 03 serial-contains 'web: fonts bitmap8x8 ui=missing mono=missing'
vgate_assert 03 serial-contains 'web: text face=bitmap8x8 proportional=no'
vgate_assert 03 serial-contains ' bold-face=no'
vgate_assert 03 serial-contains ' italic-face=no'
vgate_assert 03 serial-contains ' adv-i=8'
vgate_assert 03 serial-contains ' adv-W=8'
vgate_assert 03 serial-contains ' h1-lineh=18'
vgate_assert 03 serial-absent 'proportional=yes'
# And the page still renders end to end: a missing font costs typography, never
# the page (this is the "never a silent blank page" rule, live).
vgate_assert 03 serial-contains 'web: parse nodes='
vgate_assert 03 serial-contains 'web: settled'
vgate_assert 03 serial-contains 'web: repaint items='
vgate_assert 03 serial-contains 'web: ready'
vgate_assert 03 serial-absent 'web: error'
vgate_assert 03 serial-absent '[EXC] parking:'

# The falsification: the SAME probe must now report the grid's numbers. If this
# boot passed boot 01's thresholds, boot 01's probe would be vacuous.
vgate_assert 03 snapshot 'snap-03-*.raw' <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
w = 1280
X, Y = 40, 28
CX, CY, CW, CH = 8, 50, 496, 322
def px(x, y):
    k = (y * w + x) * 4
    return (data[k + 2], data[k + 1], data[k])
PAGE_BG = (0x18, 0x20, 0x26)
def near(c, want, tol=6):
    return all(abs(a - b) <= tol for a, b in zip(c, want))
rows = []
for yy in range(CY, CY + CH):
    n = 0
    for xx in range(CX, CX + CW):
        if not near(px(X + xx, Y + yy), PAGE_BG, 12):
            n += 1
    rows.append(n)
first = -1
for i, n in enumerate(rows):
    if n > 0:
        first = i
        break
assert first >= 0, "the fallback painted nothing: a missing font blanked the page"
last = first
while last < len(rows) and rows[last] > 0:
    last += 1
band_h = last - first
minx, maxx = None, None
for yy in range(CY + first, CY + last):
    for xx in range(CX, CX + CW):
        if not near(px(X + xx, Y + yy), PAGE_BG, 12):
            if minx is None or xx < minx:
                minx = xx
            if maxx is None or xx > maxx:
                maxx = xx
span = maxx - minx + 1
print("live-web-ttf 03: fallback h1 band height=%d span=%d" % (band_h, span))
fails = []
if band_h > 16:
    fails.append("fallback band height %d is not the 8x8 grid's 14" % band_h)
if span < 335:
    fails.append("fallback band span %d is not the 8x8 grid's 347 (the probe would be vacuous)" % span)
if span > 360:
    fails.append("fallback band span %d is wider than the grid can produce" % span)
assert not fails, "WEB-TTF-FALLBACK-FAILS: " + "; ".join(fails)
print("live-web-ttf 03 ok: faces gone -> grid metrics, page still painted")
PY

# --- boot 04: the two rungs inherited from the retired live-doc specs ----
# M71i (#1568): `<dl>/<dt>/<dd>` (ADR 0028 S2, which live-doc-tables pinned
# and DEPTH.HTML does not carry) and the missing-<img> placeholder (S3, from
# live-doc-web boot 01). Both are asserted as RELATIONS the page cannot
# satisfy by accident: the dd's ink starts further right than the dt's, and a
# surface-filled box exists where the named file does not -- a box, not a
# blank. Thresholds come from the measured run (the probe prints them).
#
# It runs LAST on purpose, after boot 03 has removed every face from the
# share, so it renders in the 8x8 grid -- and neither claim depends on
# typography: the indent is a layout property and the placeholder is a box.
# That also makes this boot a second, incidental fallback render, which cost
# nothing to get.
#
# Issue #1586: ONE trigger, and it is the PACED present. WEB paints twice on
# purpose: `web: settled` is the first paint, issued straight after the load,
# and `web: repaint` is the same frame rendered again after settleRepaint's
# `vi.Sleep(30)` -- whose comment records the reason ("a single present
# immediately after a long blocking fetch could leave the scanout on the
# pre-paint buffer"). The sibling boots assert `snap-0X-*.raw`, a glob, so
# they read whichever stream landed last; this boot pinned the exact `-0`
# file, i.e. the unpaced one. That frame is only ever final when something
# else delayed the app: it passed while WEB still paid its WM-attach retry,
# and failed the moment that retry was fixed. With a single trigger the file
# name is deterministic again -- `snap-04-0.raw` is the paced frame -- so the
# probe below is unchanged.
vgate_run 04 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-04' \
    --script '$RUN_DIR/script-lists.txt' \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 120

vgate_assert 04 serial-absent 'web: budget over'
vgate_assert 04 serial-contains 'web: url /host/LISTS.HTML'
vgate_assert 04 serial-contains 'web: parse nodes='
vgate_assert 04 serial-contains 'web: layout blocks='
vgate_assert 04 serial-contains 'web: settled'
vgate_assert 04 serial-absent 'web: error'
vgate_assert 04 serial-absent '[EXC] parking:'
vgate_assert 04 snapshot 'snap-04-0.raw' <<'PY'
import sys
data = open(sys.argv[1], "rb").read()
w = 1280
X, Y, W, H = 40, 28, 512, 384
# The window's own chrome sits above the page: a title band (y 28-43), the
# title text (48-54) and a toolbar band (62-75) -- measured on this boot. The
# page's client region is y 84 .. Y+H-40, with the status bar below it, so the
# scan below starts under the chrome and ends above the bar. (The first draft
# scanned the whole window and found the chrome instead of the page: the bands
# it reported were the title/toolbar fills.)
Y0, Y1 = Y + 56, Y + H - 40
CL, CR = X + 6, X + W - 6
def px(x, y):
    k = (y * w + x) * 4
    return (data[k + 2], data[k + 1], data[k])
def near(c, want, tol=8):
    return all(abs(a - b) <= tol for a, b in zip(c, want))
PAGE_BG = (0x18, 0x20, 0x26)
SURFACE = (0x22, 0x2d, 0x35)
def ink(c):
    return not near(c, PAGE_BG, 12)
prof = []
for yy in range(Y0, Y1):
    prof.append(sum(1 for xx in range(CL, CR) if ink(px(xx, yy))))
bands = []
i = 0
while i < len(prof):
    if prof[i] == 0:
        i += 1
        continue
    s = i
    while i < len(prof) and prof[i] > 0:
        i += 1
    ys, ye = Y0 + s, Y0 + i - 1
    lo = min(xx for xx in range(CL, CR)
             if any(ink(px(xx, yy)) for yy in range(ys, ye + 1))) - X
    hi = max(xx for xx in range(CL, CR)
             if any(ink(px(xx, yy)) for yy in range(ys, ye + 1))) - X
    surf = sum(1 for yy in range(ys, ye + 1) for xx in range(CL, CR)
               if near(px(xx, yy), SURFACE))
    bands.append((ys, ye, sum(prof[s:i]), lo, hi, surf))
print("live-web-ttf 04 bands (y0,y1,ink,x0,x1,surface abs):")
for b in bands:
    print("   %s" % (b,))
fails = []
text_bands = [b for b in bands if b[5] < 100 and b[2] >= 50 and b[4] - b[3] < 220]
if len(text_bands) < 3:
    fails.append("expected h4 + dt + dd text bands, found %d" % len(text_bands))
else:
    dt, dd = text_bands[1], text_bands[2]
    print("live-web-ttf 04 dl: dt y=%d x0=%d ink=%d, dd y=%d x0=%d ink=%d, indent=%d"
          % (dt[0], dt[3], dt[2], dd[0], dd[3], dd[2], dd[3] - dt[3]))
    if dt[1] >= dd[0]:
        fails.append("dt (y%d-%d) does not precede dd (y%d-%d)" % (dt[0], dt[1], dd[0], dd[1]))
    if dd[3] - dt[3] < 8:
        fails.append("dd x0=%d is not indented past dt x0=%d (a definition list indents its term)"
                     % (dd[3], dt[3]))
boxes = [b for b in bands if b[5] >= 400]
if not boxes:
    fails.append("no surface-filled placeholder box: the missing <img> src must be VISIBLE, never blank")
else:
    box = boxes[0]
    print("live-web-ttf 04 placeholder: %dx%d px surface=%d at y=%d"
          % (box[4] - box[3] + 1, box[1] - box[0] + 1, box[5], box[0]))
    if box[1] - box[0] < 8 or box[4] - box[3] < 16:
        fails.append("placeholder box %dx%d is too small to be the undecoded image's box"
                     % (box[4] - box[3] + 1, box[1] - box[0] + 1))
assert not fails, "WEB-TTF-LISTS-FAILS: " + "; ".join(fails)
print("live-web-ttf 04 ok: dl indents its definition, missing img still paints a box")
PY

# --- boot 05: guest PNG decode, not a placeholder -----------------------
# Boot 03 removed the fonts. This image-only probe intentionally requires no
# typography: its four colors can only come from the decoded PNG pixels.
vgate_run 05 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/snap-05' \
    --script '$RUN_DIR/script-png.txt' \
    --snapshot-after "web: repaint" \
    --script-expect "web: ready" --timeout 120

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
import sys
data = open(sys.argv[1], "rb").read()
W = 1280
X, Y, CW, CH = 48, 78, 496, 322
colors = [(0x11, 0x7f, 0x33), (0xd0, 0x33, 0x99),
          (0x22, 0x66, 0xdd), (0xee, 0xcc, 0x00)]
counts = [0] * 4
for y in range(Y, Y + CH):
    for x in range(X, X + CW):
        k = (y * W + x) * 4
        rgb = (data[k+2], data[k+1], data[k])
        for i, color in enumerate(colors):
            if rgb == color:
                counts[i] += 1
print("live-web-ttf 05: decoded PNG exact quadrant pixels=%s" % counts)
assert counts == [256, 256, 256, 256], \
    "PNG did not decode to the four exact 16x16 quadrants: %s" % counts
print("live-web-ttf 05 ok: guest PNG decoded and painted, not an alt box")
PY
