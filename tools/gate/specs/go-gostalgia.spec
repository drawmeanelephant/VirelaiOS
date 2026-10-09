# go-gostalgia.spec -- M95b (#2010) + M95c (#2011): the pinned Gostalgia
# tree built as a GOOS=virelai guest through the overlay mechanism. Run 01
# is the M95b adapter gate (mem:// IPC, file primitives, sys_exec'd child,
# in-band shutdown) and is unchanged. Runs 02-04 are the M95c terminal
# backend gate: GOSTALGIA.ELF's Bubble Tea v1 shell on a GOTABWM-hosted
# window tty, driven by real HID keystrokes — never synthetic app input.
#
#   run 02: render + echo + typed `exit` -> status 0. `dui focus` is the
#           marker-gated refocus: the seat's own chrome windows (the Apps
#           button, blur probe) grab kernel focus at open — typing waits
#           for `dui focus: focused=3` so every key lands on the bound tty.
#   run 03: seat relayout resize + WIN_CLOSE. ctrl-shift-v cycles the
#           live seat's split (needs the staged CHARMHELLO tab), both
#           panes re-rect through wm_apply_rect -> WIN_RESIZE -> 80x21.
#   run 04: text-grid readback — kernel selection -> ctrl-shift-c copy ->
#           monitor `clip` — proves typed commands reached the shell and
#           its replies painted the grid; ctrl-c exits via the sig seam.
#   run 05: font-zoom resize — `font large` is the kernel's own WIN_RESIZE
#           delivery (M80i): the bound grid re-flows to the 10x21 rung and
#           the app re-reads TerminalCell — 80x44 -> 80x33.
#
# HOST PREREQUISITE: `.build/go/GSSMOKE.ELF` and `.build/go/GOSTALGIA.ELF`
# via `bash tools/go/build-gostalgia.sh` (overlay leg; --no-overlay is the
# fail-before leg — the staged pin has no cmd/gssmoke and refuses).
#
# exec-order: assert-proven -- every run ends on markers only the exec'd
# program emits (`gostalgia: done`, `procs GOSTALGIA.ELF exited status=0`,
# `gssmoke: done`); no script-supplied echo gates a run's end.

vgate_name go-gostalgia "M95b/c: pinned Gostalgia on Virelai — gsport adapters + Bubble Tea v1 on a bound GOTABWM window tty"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 1 BOOTS

vgate_file script.txt <<'EOF'
strace exec GSSMOKE.ELF
EOF

vgate_file script-02.txt <<'EOF'
exec GOSTALGIA.ELF
EOF

vgate_file script-02b.txt <<'EOF'
dui focus 3
EOF

# script-02c's bare `dui` registry dump doubles as the post-echo repaint
# barrier: it runs 2 s after the echo reply marker, so the guest-side
# snapshot (on `dui[`) reads the repainted grid.
vgate_file script-02c.txt <<'EOF'
dui
EOF

vgate_file script-05.txt <<'EOF'
exec GOSTALGIA.ELF
EOF

# focus before zoom: the font leg needs no input, but the exit chord does.
vgate_file script-05b.txt <<'EOF'
dui focus 3
font large
EOF

# `dui` runs 2 s after the 80x33 marker — the post-zoom repaint barrier,
# same shape as script-02c.
vgate_file script-05c.txt <<'EOF'
dui
EOF

vgate_file script-03.txt <<'EOF'
exec GOSTALGIA.ELF
EOF

vgate_file script-03b.txt <<'EOF'
exec CHARMHELLO.ELF
EOF

# `dui` first (geometry evidence + the screenshot marker), then close the
# GOSTALGIA window by id (the seat itself holds id 2) — the kernel release pushes WIN_CLOSE to its
# owner, which is the run's exit leg.
vgate_file script-03c.txt <<'EOF'
dui
dui close 3
EOF

vgate_file script-04.txt <<'EOF'
exec GOSTALGIA.ELF
EOF

# Same seat-chrome focus steal as run 02: the clock window (seat-owned,
# opened right after the declare) grabs kernel focus, so the typed echo
# must wait for `dui focus: focused=3` before any key is injected.
vgate_file script-04b.txt <<'EOF'
dui focus 3
EOF

# `clip` prints the clipboard lines on the console (the readback half of
# the echo proof); `dui close 3` is only a failsafe so a stalled ctrl-c
# still ends the boot.
vgate_file script-04c.txt <<'EOF'
clip
dui close 3
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
rd = os.environ["RUN_DIR"]
share = os.path.join(rd, "share")
src = os.path.join(".build", "go", "GSSMOKE.ELF")
if not os.path.exists(src):
    sys.exit("GSSMOKE.ELF missing (build it first: bash tools/go/build-gostalgia.sh)")
shutil.copy(src, os.path.join(share, "GSSMOKE.ELF"))
print("staged GSSMOKE.ELF into share (%d bytes)" % os.path.getsize(src))
gsrc = os.path.join(".build", "go", "GOSTALGIA.ELF")
if not os.path.exists(gsrc):
    sys.exit("GOSTALGIA.ELF missing (build it first: bash tools/go/build-gostalgia.sh gostalgia)")
shutil.copy(gsrc, os.path.join(share, "GOSTALGIA.ELF"))
print("staged GOSTALGIA.ELF into share (%d bytes)" % os.path.getsize(gsrc))
csrc = os.path.join(".build", "go", "CHARMHELLO.ELF")
if not os.path.exists(csrc):
    sys.exit("CHARMHELLO.ELF missing (build it first: bash tools/go/build-charmhello.sh)")
shutil.copy(csrc, os.path.join(share, "CHARMHELLO.ELF"))
print("staged CHARMHELLO.ELF into share (%d bytes)" % os.path.getsize(csrc))
# The seat itself is a Go ELF: the seed bundle only carries zig-out bins,
# so without this the boot logs `GOTABWM.ELF not on the share (shim
# compositing)` and no hosted-window grant ever arrives.
wsrc = os.path.join(".build", "go", "GOTABWM.ELF")
if not os.path.exists(wsrc):
    sys.exit("GOTABWM.ELF missing (build it first: bash tools/go/build-gotabwm.sh)")
shutil.copy(wsrc, os.path.join(share, "GOTABWM.ELF"))
print("staged GOTABWM.ELF into share (%d bytes)" % os.path.getsize(wsrc))
# Isolate headless fixture boots from the default Go seat (same trick as
# live-strace): no WM seating, so the smoke runs on a quiet desktop. Run
# 01's tag assert restores the default seat for runs 02-04.
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    f.write("#v2\nwm=none\n")
PY

vgate_run 01 -- \
    --script '$RUN_DIR/script.txt' \
    --script-expect 'gssmoke: done' --timeout 180

vgate_assert 01 serial-contains 'VirelaiOS kernel has seized control.'
vgate_assert 01 serial-contains 'strace: armed'
# `strace exec` bypasses the monitor's "exec: loaded" print (it calls
# esp_exec.exec_file directly); "gssmoke: ready" is the load evidence.
vgate_assert 01 serial-contains 'gssmoke: ready endpoint=mem://gostalgia-'
vgate_assert 01 serial-contains 'gssmoke: file ok'
vgate_assert 01 serial-contains 'gssmoke: spawn pid='
vgate_assert 01 serial-contains 'status=42'
vgate_assert 01 serial-contains 'gssmoke: clock now='
vgate_assert 01 serial-contains 'gssmoke: echo msg="m95b smoke"'
vgate_assert 01 serial-contains 'gssmoke: shutdown'
vgate_assert 01 serial-contains 'gssmoke: done'
vgate_assert 01 serial-absent 'gssmoke: FAIL'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'exited status=139'

# Slot evidence: decode the strace records, map sys_* names to ADR 0007
# slot numbers, and require the adapter set. This deliberately does NOT
# assert argument layout — only that the program burned the named slots
# and that no adapted path came back ENOSYS.
vgate_assert 01 python <<'PY'
import os, re, sys

serial = open(os.environ["VG_SER"], errors="replace").read()
records = re.findall(r"\[strace (\d+)\] (sys_[a-z_0-9]+)\(([^\n]*)", serial)
if not records:
    raise SystemExit("no decoded strace records")

# ADR 0007 name -> slot. Asserting the name set IS asserting the slot set:
# the decoder renders slots from the kernel's own table.
name_to_slot = {
    "sys_write": 1, "sys_exit": 3, "sys_sleep": 4,
    "sys_procs": 7,
    "sys_file_open": 23, "sys_file_read": 24, "sys_file_write": 25,
    "sys_file_close": 26, "sys_dir_list": 27,
    "sys_exec": 28, "sys_kill": 29,
    "sys_file_delete": 34, "sys_file_rename": 35,
    "sys_mmap": 63, "sys_time": 66, "sys_getrandom": 72,
    "sys_file_sync": 77,
}
observed = {name_to_slot[name] for _, name, _ in records if name in name_to_slot}
required = {4, 7, 23, 24, 25, 26, 27, 28, 34, 35, 77}
missing = sorted(required - observed)
if missing:
    raise SystemExit("adapter slots missing from trace: %s" % missing)
for pid, name, args in records:
    # ENOSYS renders decoded as "= -ENOSYS", raw as the two's-complement
    # of -4: 0xfffffffffffffffc.
    if args.endswith("= -ENOSYS") or args.endswith("= 0xfffffffffffffffc"):
        raise SystemExit("ENOSYS on %s from pid %s — an adapted path refused" % (name, pid))
print("slot evidence %s; no ENOSYS on %d records" % (sorted(observed & required), len(records)))
PY

# Between-run restore for boots 02-05 (evaluated after run 01, before run
# 02 — vgate.sh's tag-filtered assert loop): drop the wm=none override so
# the default GOTABWM seat autostarts and the window-tty legs can bind a
# hosted window. The same write on tag 02 guarantees the medium rung for
# 03-05's cell math.
vgate_assert 01 python <<'PY'
import os, sys
share = os.environ.get("VG_SHARE")
if not share:
    sys.exit("no armed share exported to the assert (vgate_share seed?)")
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    f.write("#v2\n")
print("between-run restore: SETTINGS.TXT back to defaults (seat + medium font)")
PY

# --- boot 02: render + echo + `exit` ---------------------------------------
# The chain, each step gated on a marker only the guest produces:
#   exec GOSTALGIA.ELF  -> the app opens its .user window, the seat hosts
#                        it (full-viewport grant -> WIN_RESIZE -> 80x44),
#                        gsport/tty binds /dev/tty to the window and the
#                        Bubble Tea shell starts. `gostalgia:` markers ride
#                        the kernel console (fd 1), never the tty.
#   ready endpoint=     -> script-02b `dui focus 3`: the seat's own chrome
#                        windows (the 1x1 blur probe, the Apps button)
#                        grabbed kernel focus at open; the restore is the
#                        seat's syncSeatChrome path, but the `dui focus:
#                        focused=3` marker is what proves it held before a
#                        single key is typed.
#   focused=3           -> --input-string types `echo m95c tty echo ok`:
#                        real HID key reports -> bound-tty bytes (the
#                        adapter maps the kernel's LF Enter to the CR the
#                        Charm key table names `enter`) -> the shell's IPC
#                        round trip to the echo app.
#   call ...echo ok     -> --screenshot-after captures the repainted frame.
#   call ...echo ok +2s -> script-02c `dui`: registry dump (geometry
#                        evidence) and the `dui[` marker that the
#                        guest-side snapshot and the exit chords key on —
#                        2 s guarantees the app's own repaint is what the
#                        snapshot sees.
#   dui[                -> --input-chords types `exit` + Return ->
#                        sys/shutdown -> the in-band seam -> closedMsg ->
#                        tea.Quit -> teardown -> status 0.
vgate_run 02 -- \
    --screen '$RUN_DIR/gos-02' \
    --input --via-virtio --cvc-snap \
    --snapshot-out '$RUN_DIR/gos-02-snap' \
    --script '$RUN_DIR/script-02.txt' \
    --script2 '$RUN_DIR/script-02b.txt' \
    --script2-after 'gostalgia: ready endpoint=' --script2-delay 1 \
    --input-string 'echo m95c tty echo ok\n' \
    --input-string-after 'dui focus: focused=3' \
    --screenshot-after 'gostalgia: call app/com.gostalgia.echo/echo ok' \
    --script3 '$RUN_DIR/script-02c.txt' \
    --script3-after 'gostalgia: call app/com.gostalgia.echo/echo ok' --script3-delay 2 \
    --snapshot-after 'dui[' \
    --input-chords 'e,x,i,t,return' \
    --input-chords-after 'dui[' \
    --script-expect 'procs GOSTALGIA.ELF exited status=0' --timeout 240

vgate_assert 02 serial-contains 'gostalgia: tty attached window=3'
# The two size markers pin the declared rect's cells and the seat's
# full-viewport grant (the font-zoom rung change lives on run 05).
vgate_assert 02 serial-contains 'gostalgia: size 80x24'
vgate_assert 02 serial-contains 'gostalgia: size 80x44'
vgate_assert 02 serial-contains 'gostalgia: call app/list ok'
vgate_assert 02 serial-contains 'gostalgia: call app/com.gostalgia.echo/echo ok'
vgate_assert 02 serial-contains 'gostalgia: shutdown reason='
vgate_assert 02 serial-contains 'gostalgia: done'
vgate_assert 02 serial-contains 'procs GOSTALGIA.ELF exited status=0'
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 serial-absent 'exited status=139'
vgate_assert 02 serial-absent 'panic:'

# Between-run restore for boots 03-05: guarantee the medium rung — the
# cell math below (80x21 splits, the readback drag, the 80x33 zoom)
# assumes it, and a persisted font_size survives across these boots.
# Same block deletes SESSION.TABS: the seat restores it on the NEXT boot
# and the restored tab is a placeholder row — a real strip cell whose
# window id is dead. Run 03's applySplit then rects tabs.At(0) (the
# ghost) through WmctlSetWindowRect, fails honestly, and the split never
# happens (observed r3: `session load n=1 mode=restore`, `rail n=2`, two
# chord WM_KEYs, no `split` marker). go-dogfood boots 03->04 hit the same
# class and use this same between-run-assert slot for the delete (the
# setup hooks all run before boot 01, so there is nowhere earlier to put
# it); vgate.sh does the harness-level equivalent for WINDOWS.SAV.
vgate_assert 02 python <<'PY'
import os, sys
share = os.environ.get("VG_SHARE")
if not share:
    sys.exit("no armed share exported to the assert")
ser = open(os.environ["VG_SER"], "rb").read()
if b"gostalgia: done" not in ser:
    sys.exit("boot 02 did not reach the shell teardown marker")
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    f.write("#v2\n")
st = os.path.join(share, "SESSION.TABS")
if os.path.exists(st):
    os.remove(st)
    print("between-run restore: font_size reset + SESSION.TABS removed "
          "(boot 03's strip starts empty)")
else:
    print("between-run restore: font_size reset (no SESSION.TABS to remove)")
PY

# The render frame at the medium rung: lipgloss' rounded border in ANSI 62
# (0x5f5fd7 = the kernel's xterm256 palette, terminal.zig xterm256Rgb)
# around the whole grid plus the accent line in 117 (0x87d7ff). The
# scanout is 2x the 1280x720 logical geometry; the 80-col grid occupies
# the left 640 logical px at 8x16 cells.
vgate_assert 02 snapshot 'gos-02-after' <<'PY'
import struct, sys, zlib

d = open(sys.argv[1], "rb").read()
assert d[:8] == b"\x89PNG\r\n\x1a\n", "not a PNG scanout"
pos = 8
idat = b""
w = h = ct = 0
while pos < len(d):
    n, typ = struct.unpack(">I4s", d[pos:pos + 8])
    chunk = d[pos + 8:pos + 8 + n]
    if typ == b"IHDR":
        w, h, depth, ct = struct.unpack(">IIBB", chunk[:10])
        assert depth == 8, "unexpected PNG depth"
    elif typ == b"IDAT":
        idat += chunk
    pos += 12 + n
assert (w, h) == (2560, 1440), "wanted 2560x1440 scanout, got %dx%d" % (w, h)
bpp = 4 if ct == 6 else 3
raw = zlib.decompress(idat)
stride = w * bpp
out = bytearray()
prev = bytearray(stride)
i = 0
for _ in range(h):
    filt = raw[i]
    i += 1
    row = bytearray(raw[i:i + stride])
    i += stride
    if filt == 1:
        for x in range(bpp, stride):
            row[x] = (row[x] + row[x - bpp]) & 0xff
    elif filt == 2:
        for x in range(stride):
            row[x] = (row[x] + prev[x]) & 0xff
    elif filt == 3:
        for x in range(stride):
            a = row[x-bpp] if x >= bpp else 0
            row[x] = (row[x] + ((a + prev[x]) >> 1)) & 0xff
    elif filt == 4:
        for x in range(stride):
            a = row[x-bpp] if x >= bpp else 0
            b = prev[x]; c = prev[x-bpp]
            p = a + b - c
            pa, pb, pc = abs(p-a), abs(p-b), abs(p-c)
            pr = a if (pa <= pb and pa <= pc) else (b if pb <= pc else c)
            row[x] = (row[x] + pr) & 0xff
    out += row
    prev = row

def px(x, y):
    k = (y * w + x) * bpp
    return out[k], out[k+1], out[k+2]

border = accent = 0
for y in range(30, 1440):
    for x in range(0, 1300):
        r, g, b = px(x, y)
        if abs(r - 95) < 40 and abs(g - 95) < 40 and b > 175:
            border += 1
        if r > 100 and r < 170 and g > 180 and b > 220:
            accent += 1
print("shell frame: border-62 px=%d accent-117 px=%d" % (border, accent))
assert border >= 400, ("no lipgloss border on the scanout (%d px): the shell "
                       "never painted its frame" % border)
assert accent >= 20, ("no accent-colour text (%d px): the welcome/prompt "
                      "line did not paint" % accent)
PY

# The post-zoom frame, from the guest's own composed scanout 2 s after the
# resize marker: same border and accent at the 10x21 rung (grid now
# 80x33 -> 800x709 px inside the window). A resize the app never saw would
# leave it painting the 80x44 layout into the re-flowed 33-row grid —
# wrapped borders and a drowned prompt, not this.
vgate_assert 02 snapshot 'gos-02-snap-*.raw' <<'PY'
import sys

W, H = 1280, 720
raw = open(sys.argv[1], "rb").read()
need = W * H * 4
if len(raw) < need:
    sys.exit("snapshot is %d bytes, want at least %d (%dx%d BGRX)" % (len(raw), need, W, H))

def px(x, y):
    off = (y * W + x) * 4
    return (raw[off + 2], raw[off + 1], raw[off])

border = accent = 0
for y in range(16, 710):
    for x in range(0, 820):
        r, g, b = px(x, y)
        if abs(r - 95) < 40 and abs(g - 95) < 40 and b > 175:
            border += 1
        if r > 100 and r < 170 and g > 180 and b > 220:
            accent += 1
print("post-zoom frame: border-62 px=%d accent-117 px=%d" % (border, accent))
assert border >= 200, ("post-zoom frame lost the shell border (%d px): the "
                       "app repainted nothing at the new cell rung" % border)
assert accent >= 15, ("post-zoom frame lost the accent text (%d px)" % accent)
PY

# --- boot 03: seat relayout resize + WIN_CLOSE ----------------------------
#   exec GOSTALGIA.ELF            -> hosted, grants 80x44.
#   gostalgia: ready endpoint=    -> script-03b execs CHARMHELLO: the
#                                  strip's second tab — ctrl-shift-v only
#                                  splits with >= 2 tabs (hid.go).
#   charmhello: ready             -> --input-chords ctrl-shift-v x2: the
#                                  seat's live split cycle none -> V ->
#                                  H. Each press also reaches the
#                                  kernel's focused-terminal paste chord —
#                                  `tty: paste 0 bytes` with the empty
#                                  clipboard, harmless.
#   gotabwm: split h              -> both panes re-rect through
#                                  wm_apply_rect -> WIN_RESIZE to the
#                                  owners -> `gostalgia: size 80x21`
#                                  (1280x360 pane, (360-16)/16).
#   split h + 3 s                 -> script-03c `dui` (dump + the `dui[`
#                                  marker the screenshot keys on, after
#                                  the repaint) then `dui close 3`:
#                                  kernel release -> WIN_CLOSE -> the
#                                  event goroutine's NotifyClose -> the
#                                  same in-band seam -> status 0.
vgate_run 03 -- \
    --screen '$RUN_DIR/gos-03' \
    --input --via-virtio \
    --script '$RUN_DIR/script-03.txt' \
    --script2 '$RUN_DIR/script-03b.txt' \
    --script2-after 'gostalgia: ready endpoint=' \
    --input-chords 'ctrl-shift-v,ctrl-shift-v' \
    --input-chords-after 'charmhello: ready' \
    --screenshot-after 'dui[' \
    --script3 '$RUN_DIR/script-03c.txt' \
    --script3-after 'gotabwm: split h' --script3-delay 3 \
    --script-expect 'procs GOSTALGIA.ELF exited status=0' --timeout 240

vgate_assert 03 serial-contains 'gostalgia: tty attached window=3'
vgate_assert 03 serial-contains 'gostalgia: size 80x44'
vgate_assert 03 serial-contains 'gotabwm: split v'
vgate_assert 03 serial-contains 'gotabwm: split h'
vgate_assert 03 serial-contains 'gostalgia: size 80x21'
vgate_assert 03 serial-contains 'gostalgia: shutdown reason='
vgate_assert 03 serial-contains 'gostalgia: done'
vgate_assert 03 serial-contains 'procs GOSTALGIA.ELF exited status=0'
vgate_assert 03 serial-absent '[EXC] parking:'
vgate_assert 03 serial-absent 'exited status=139'
vgate_assert 03 serial-absent 'panic:'

# Same between-run delete as tag 02: run 03's declares (gostalgia +
# charmhello) wrote two SESSION.TABS rows; boot 04 would otherwise
# restore two dead-id placeholder tabs.
vgate_assert 03 python <<'PY'
import os
share = os.environ.get("VG_SHARE")
st = os.path.join(share, "SESSION.TABS") if share else None
if st and os.path.exists(st):
    os.remove(st)
    print("between-run cleanup: SESSION.TABS removed before boot 04")
else:
    print("between-run cleanup: no SESSION.TABS (nothing to remove)")
PY

# The post-split frame (3 s after the relayout, before the close): the
# shell border survives in whichever half-pane the seat assigned it, and
# the OTHER half carries no border ink — a repaint the resize never
# triggered would leave the old layout clipped or spilled, not a clean
# pane-bounded frame.
vgate_assert 03 snapshot 'gos-03-after' <<'PY'
import struct, sys, zlib

d = open(sys.argv[1], "rb").read()
assert d[:8] == b"\x89PNG\r\n\x1a\n", "not a PNG scanout"
pos = 8
idat = b""
w = h = ct = 0
while pos < len(d):
    n, typ = struct.unpack(">I4s", d[pos:pos + 8])
    chunk = d[pos + 8:pos + 8 + n]
    if typ == b"IHDR":
        w, h, depth, ct = struct.unpack(">IIBB", chunk[:10])
        assert depth == 8, "unexpected PNG depth"
    elif typ == b"IDAT":
        idat += chunk
    pos += 12 + n
assert (w, h) == (2560, 1440), "wanted 2560x1440 scanout, got %dx%d" % (w, h)
bpp = 4 if ct == 6 else 3
raw = zlib.decompress(idat)
stride = w * bpp
out = bytearray()
prev = bytearray(stride)
i = 0
for _ in range(h):
    filt = raw[i]
    i += 1
    row = bytearray(raw[i:i + stride])
    i += stride
    if filt == 1:
        for x in range(bpp, stride):
            row[x] = (row[x] + row[x - bpp]) & 0xff
    elif filt == 2:
        for x in range(stride):
            row[x] = (row[x] + prev[x]) & 0xff
    elif filt == 3:
        for x in range(stride):
            a = row[x-bpp] if x >= bpp else 0
            row[x] = (row[x] + ((a + prev[x]) >> 1)) & 0xff
    elif filt == 4:
        for x in range(stride):
            a = row[x-bpp] if x >= bpp else 0
            b = prev[x]; c = prev[x-bpp]
            p = a + b - c
            pa, pb, pc = abs(p-a), abs(p-b), abs(p-c)
            pr = a if (pa <= pb and pa <= pc) else (b if pb <= pc else c)
            row[x] = (row[x] + pr) & 0xff
    out += row
    prev = row

def px(x, y):
    k = (y * w + x) * bpp
    return out[k], out[k+1], out[k+2]

def border_in(y0, y1):
    n = 0
    for y in range(y0, y1):
        for x in range(0, 2560):
            r, g, b = px(x, y)
            if abs(r - 95) < 40 and abs(g - 95) < 40 and b > 175:
                n += 1
    return n

top = border_in(0, 700)
bot = border_in(760, 1440)
print("post-split frame: border-62 top=%d bottom=%d" % (top, bot))
ok = (top >= 200 and bot * 3 < top) or (bot >= 200 and top * 3 < bot)
assert ok, ("border ink top=%d bottom=%d: no clean pane-bounded repaint — "
            "either nothing repainted after the split or the old layout "
            "spilled into the other pane" % (top, bot))
PY

# --- boot 04: text-grid readback + ctrl-c ---------------------------------
#   exec GOSTALGIA.ELF -> hosted shell; `call app/list ok` means the model
#   is interactive.
#   ready endpoint=    -> script-04b `dui focus 3` (the same clock-window
#   focus steal as run 02) -> `dui focus: focused=3`.
#   focused=3          -> --input-string types `echo m95c readback echo`.
#   call ...echo ok    -> --pointer-virtio drags over the transcript rows
#   (content top = the kernel's 16 px title band; the drag spans rows ~1-19
#   at the 8x16 rung) -> `dui: term sel begin/end`.
#   sel end            -> --input-chords ctrl-shift-c copies the focused
#   window's selection (`tty: copy N bytes`), then ctrl-c lands 0x03 in
#   the bound tty -> gsport/tty's seam + the model's ctrl+c key -> clean
#   quit.
#   tty: copy          -> script-04c `clip` prints the clipboard lines on
#   the console — the text-grid readback half of the echo proof (the reply
#   text the app painted is read back cell-for-cell) — then `dui close 3`
#   as the failsafe so a stalled ctrl-c still ends the boot.
vgate_run 04 -- \
    --screen '$RUN_DIR/gos-04' \
    --input --via-virtio \
    --script '$RUN_DIR/script-04.txt' \
    --script2 '$RUN_DIR/script-04b.txt' \
    --script2-after 'gostalgia: ready endpoint=' --script2-delay 1 \
    --input-string 'echo m95c readback echo\n' \
    --input-string-after 'dui focus: focused=3' \
    --pointer-virtio '24,40,d;1100,320;1100,320,u' \
    --pointer-virtio-after 'gostalgia: call app/com.gostalgia.echo/echo ok' \
    --input-chords 'ctrl-shift-c,ctrl-c' \
    --input-chords-after 'dui: term sel end' \
    --screenshot-after 'tty: copy ' \
    --script3 '$RUN_DIR/script-04c.txt' \
    --script3-after 'tty: copy ' \
    --script-expect 'procs GOSTALGIA.ELF exited status=0' --timeout 240

vgate_assert 04 serial-contains 'gostalgia: tty attached window=3'
vgate_assert 04 serial-contains 'gostalgia: call app/com.gostalgia.echo/echo ok'
vgate_assert 04 serial-contains 'dui: term sel begin'
vgate_assert 04 serial-contains 'dui: term sel end'
vgate_assert 04 serial-contains 'gostalgia: shutdown reason='
vgate_assert 04 serial-contains 'gostalgia: done'
vgate_assert 04 serial-contains 'procs GOSTALGIA.ELF exited status=0'
vgate_assert 04 serial-absent '[EXC] parking:'
vgate_assert 04 serial-absent 'exited status=139'
vgate_assert 04 serial-absent 'panic:'

# The readback: `clip` echoes the clipboard to the console, one `clip: `
# line per copied row. The typed command's reply — `m95c readback echo
# [echo #1]` — must appear inside a selected row, which proves the full
# round trip in grid cells: HID bytes -> shell -> IPC -> paint -> cells ->
# selection -> clipboard. `tty: copy` reports a nonzero byte count.
vgate_assert 04 python <<'PY'
import os, re, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
m = re.search(r"tty: copy (\d+) bytes", ser)
if not m:
    sys.exit("no `tty: copy` marker — the selection never reached the clipboard")
if int(m.group(1)) < 40:
    sys.exit("tty: copy %s bytes — the drag selected almost nothing" % m.group(1))
clip_lines = [ln for ln in ser.splitlines() if ln.startswith("clip:")]
if not clip_lines:
    sys.exit("the `clip` command printed nothing — clipboard readback failed")
hit = [ln for ln in clip_lines if "m95c readback echo" in ln]
if not hit:
    sys.exit("no clip line carries the echo reply; lines=%r" % clip_lines[:8])
print("readback: %d clip lines, %d bytes copied, reply text present: %r"
      % (len(clip_lines), int(m.group(1)), hit[0].strip()))
PY

# Same between-run delete as tags 02/03 before boot 05.
vgate_assert 04 python <<'PY'
import os
share = os.environ.get("VG_SHARE")
st = os.path.join(share, "SESSION.TABS") if share else None
if st and os.path.exists(st):
    os.remove(st)
    print("between-run cleanup: SESSION.TABS removed before boot 05")
else:
    print("between-run cleanup: no SESSION.TABS (nothing to remove)")
PY

# --- boot 05: font-zoom resize + `exit` -------------------------------------
# The font-zoom half of the resize contract, on its own boot so every step
# keeps a marker gate:
#   exec GOSTALGIA.ELF -> hosted shell, 80x44 on the seat's grant (medium
#                        rung restored on tag 02).
#   ready endpoint=    -> script-05b: `dui focus 3` (the exit chord needs
#                        the same keyboard focus as run 02's typing), then
#                        `font large`: the kernel's own WIN_RESIZE fan-out
#                        (apply_grid_font_size) -> NoteResize re-reads the
#                        now-10x21 rung -> `gostalgia: size 80x33`.
#   size 80x33 + 2 s   -> script-05c `dui`: registry dump + the `dui[`
#                        barrier for the large-font screenshot and exit.
#   dui[               -> --screenshot-after captures the repainted frame;
#                        --input-chords types `exit` -> status 0.
vgate_run 05 -- \
    --screen '$RUN_DIR/gos-05' \
    --input --via-virtio \
    --script '$RUN_DIR/script-05.txt' \
    --script2 '$RUN_DIR/script-05b.txt' \
    --script2-after 'gostalgia: ready endpoint=' --script2-delay 1 \
    --script3 '$RUN_DIR/script-05c.txt' \
    --script3-after 'gostalgia: size 80x33' --script3-delay 2 \
    --screenshot-after 'dui[' \
    --input-chords 'e,x,i,t,return' \
    --input-chords-after 'dui[' \
    --script-expect 'procs GOSTALGIA.ELF exited status=0' --timeout 240

vgate_assert 05 serial-contains 'gostalgia: tty attached window=3'
vgate_assert 05 serial-contains 'gostalgia: size 80x44'
# The font-zoom leg: NoteResize re-read TerminalCell and reported the
# 10x21 rung's cells — the resize path the seat's pixel payload cannot
# express on its own.
vgate_assert 05 serial-contains 'gostalgia: size 80x33'
vgate_assert 05 serial-contains 'gostalgia: shutdown reason='
vgate_assert 05 serial-contains 'gostalgia: done'
vgate_assert 05 serial-contains 'procs GOSTALGIA.ELF exited status=0'
vgate_assert 05 serial-absent '[EXC] parking:'
vgate_assert 05 serial-absent 'exited status=139'
vgate_assert 05 serial-absent 'panic:'

# The large-font frame still carries the lipgloss border (ANSI 62) and the
# accent line (117): a repaint that never ran would leave the border at the
# old rung's glyph rows; the counts stay well above the medium-rung floor.
vgate_assert 05 snapshot 'gos-05-after' <<'PY'
import struct, sys, zlib

d = open(sys.argv[1], "rb").read()
assert d[:8] == b"\x89PNG\r\n\x1a\n", "not a PNG scanout"
pos = 8
idat = b""
w = h = ct = 0
while pos < len(d):
    n, typ = struct.unpack(">I4s", d[pos:pos + 8])
    chunk = d[pos + 8:pos + 8 + n]
    if typ == b"IHDR":
        w, h, depth, ct = struct.unpack(">IIBB", chunk[:10])
        assert depth == 8, "unexpected PNG depth"
    elif typ == b"IDAT":
        idat += chunk
    pos += 12 + n
assert (w, h) == (2560, 1440), "wanted 2560x1440 scanout, got %dx%d" % (w, h)
bpp = 4 if ct == 6 else 3
raw = zlib.decompress(idat)
stride = w * bpp
out = bytearray()
prev = bytearray(stride)
i = 0
for _ in range(h):
    filt = raw[i]
    i += 1
    row = bytearray(raw[i:i + stride])
    i += stride
    if filt == 1:
        for x in range(bpp, stride):
            row[x] = (row[x] + row[x - bpp]) & 0xff
    elif filt == 2:
        for x in range(stride):
            row[x] = (row[x] + prev[x]) & 0xff
    elif filt == 3:
        for x in range(stride):
            a = row[x-bpp] if x >= bpp else 0
            row[x] = (row[x] + ((a + prev[x]) >> 1)) & 0xff
    elif filt == 4:
        for x in range(stride):
            a = row[x-bpp] if x >= bpp else 0
            b = prev[x]; c = prev[x-bpp]
            p = a + b - c
            pa, pb, pc = abs(p-a), abs(p-b), abs(p-c)
            pr = a if (pa <= pb and pa <= pc) else (b if pb <= pc else c)
            row[x] = (row[x] + pr) & 0xff
    out += row
    prev = row

def px(x, y):
    k = (y * w + x) * bpp
    return out[k], out[k+1], out[k+2]

border = accent = 0
for y in range(30, 1440):
    for x in range(0, 1700):
        r, g, b = px(x, y)
        if abs(r - 95) < 40 and abs(g - 95) < 40 and b > 175:
            border += 1
        if r > 100 and r < 170 and g > 180 and b > 220:
            accent += 1
print("zoomed shell frame: border-62 px=%d accent-117 px=%d" % (border, accent))
assert border >= 300, ("no lipgloss border on the post-zoom scanout (%d px): "
                       "the shell never repainted at the new rung" % border)
assert accent >= 10, ("no accent-colour text on the post-zoom scanout "
                      "(%d px)" % accent)
PY

# Repeat-cycle hygiene: on BOOTS>1 the next cycle's boot 02 would restore
# this boot's Gostalgia tab as a dead-id placeholder otherwise.
vgate_assert 05 python <<'PY'
import os
share = os.environ.get("VG_SHARE")
st = os.path.join(share, "SESSION.TABS") if share else None
if st and os.path.exists(st):
    os.remove(st)
    print("between-run cleanup: SESSION.TABS removed (repeat hygiene)")
else:
    print("between-run cleanup: no SESSION.TABS (nothing to remove)")
PY
