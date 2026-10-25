# live-formats.spec -- M202e (#3453): the formats acceptance journey, K=3.
#
# One boot where the whole arc holds at once: an Adam7 interlaced PNG
# opens in GOVIEW (the corpus's 153-byte vector doubles as the clipboard
# payload -- the only interlaced fixture under the kernel buffer's 512
# bound), a MOV plays its sowt track through the mixer (the receipt's
# audio_frames rate is the play evidence -- the strict drift bound stays
# run 04 of live-image-viewer under WND), GOREADER's `p` stages a PDF
# onto the VHI1 print lane and the bare runner's consent-free degrade
# lands it byte-equal,
# and the decoded image crosses two apps through the typed seam (GOVIEW
# `c` -> GOFILES `p` -> PASTE.PNG byte-identical on the share).
#
# The focus choreography is the design: new declares take focus under
# the seat, so each keyed app is provably the focused window when its
# input lands. GOREADER rides script2's tail -- its `p` cannot land on
# anyone else; the IPNG viewer is script3's last declare for `c`; the
# GOFILES paste is the single hop one alt-shift-tab takes from the
# newest tab (the strip is declare order: ..., GOFILES, IPNG-viewer).
# lane_clip is never armed, so the seat's host clipboard sync cannot
# clobber the pinned payload mid-leg (the flake M202d's wm=none shape
# kept off this boot too). The pixel-level interlace proof stays in
# live-image-viewer run 03; here `gview: loaded IPNG.PNG` IS the
# interlace-path-landed claim (pre-M202a it was the named refusal).
#
# HOST PREREQUISITES (fail the gate honestly when missing):
#   bash tools/go/build-gotabwm.sh  ->  .build/go/GOTABWM.ELF
#   bash tools/go/build-goview.sh   ->  .build/go/GOVIEW.ELF
#   bash tools/go/build-files.sh    ->  .build/go/GOFILES.ELF
#   bash tools/go/build-goreader.sh ->  .build/go/GOREADER.ELF
#
# exec-order: assert-proven -- every stage is gated on that app's own
# receipt (armed -> ready -> print seq=1 -> loaded -> copy -> pasted);
# a program that never ran cannot reach its anchor.

vgate_name live-formats "M202e (#3453): the formats acceptance journey -- Adam7 open + MOV audio + VHI1 print + cross-app typed clipboard, one boot, K=3"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 3 BOOTS

vgate_setup_python <<'PY'
import math, os, shutil, struct, sys
run = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(run, "share")
for name, build in (
    ("GOTABWM.ELF", "bash tools/go/build-gotabwm.sh"),
    ("GOVIEW.ELF", "bash tools/go/build-goview.sh"),
    ("GOFILES.ELF", "bash tools/go/build-files.sh"),
    ("GOREADER.ELF", "bash tools/go/build-goreader.sh"),
):
    src = os.path.join(".build", "go", name)
    if not os.path.isfile(src):
        sys.exit(name + " missing (expected " + src + ") - build it first: " + build)
    shutil.copy(src, os.path.join(share, name))
# wm=none: the seat is exec'd on script, like the per-lane specs.
# notify_sound=off: the seat's chime rides vi.AudioStream too — observed
# journey-01's `gview: audio no device` (ENXIO) with the seat's own stream
# seat in play (iterations 02/03 show the seat holding gen=1, GOVIEW
# gen=2). Off keeps the mixer single-tenant: GOVIEW is the only client.
open(os.path.join(share, "SETTINGS.TXT"), "w").write(
    "#v2\nwm=none\nnotify_sound=off\n")
# The lane credential, verbatim the live-hostint shape: the row the seat
# reads via sys_secret_get (the image binding needs the uid_system OWNERS
# row) and the runner reads as a host file -- the value bytes ARE the key.
KEY = "m108ekey" * 8  # 64 bytes, ASCII
open(os.path.join(share, "SECRETS.TXT"), "w").write(
    "#v1\n" + "host-int\t1000\t" + KEY + "\tGOTABWM.ELF\n")
open(os.path.join(share, "OWNERS.TXT"), "w").write(
    "#v1\nGOTABWM.ELF\t644\t0\t-\n")
# The journey's share tree: one directory carries every staged input.
jn = os.path.join(share, "JNEY")
os.makedirs(os.path.join(jn, "INBOX"), exist_ok=True)
# IPNG: the webrender corpus's pinned Adam7 vector -- 7x5 RGB interlaced,
# 153 bytes, the only interlaced fixture under the kernel clipboard's
# 512-byte bound, so the same file is the open leg and the typed payload.
# The interlace byte (IHDR body[12], offset 28) is asserted here so a
# non-interlaced fixture can never stage this leg vacuously.
ipng = open("user/go/webrender/testdata/png/adam7-type-2-filter-4.png", "rb").read()
assert ipng[:8] == b"\x89PNG\r\n\x1a\n" and ipng[12:16] == b"IHDR" \
    and ipng[28] == 1, "adam7 fixture lost its interlaced IHDR"
assert len(ipng) <= 512, "adam7 fixture grew past the clipboard bound"
open(os.path.join(jn, "IPNG.PNG"), "wb").write(ipng)
open(os.path.join(run, "ipng-ref.png"), "wb").write(ipng)
# PRINTSRC: a reader's open document IS the print job (GOREADER never
# re-renders) -- the same dct.pdf live-hostint run 08 stages.
shutil.copy("user/go/goreader/testdata/dct.pdf",
            os.path.join(jn, "PRINTSRC.PDF"))
# AUD.MOV: the live-image-viewer run-04 construction verbatim -- the
# committed ten-frame clip grown a soun track: the video trak travels
# verbatim, the audio samples land at the end of mdat, both stco tables
# are repointed for the grown moov. Ten 4410-byte sowt s16-mono samples
# = 22050 frames at 22050 Hz, exactly the video loop's one second, a
# 440 Hz sine at 0.3 amp.
mov = open("user/go/view/testdata/clip_160x120.mov", "rb").read()
moov_at = mov.find(b"moov") - 4
mdat_at = mov.find(b"mdat") - 4
assert 0 < moov_at < mdat_at, "clip fixture lost its box layout"
ftyp, moov, mdat = mov[:moov_at], mov[moov_at:mdat_at], mov[mdat_at:]
moov_body, mdat_body = moov[8:], mdat[8:]
box = lambda t, p: struct.pack(">I", 8 + len(p)) + t + p
u32 = lambda v: struct.pack(">I", v)
u16 = lambda v: struct.pack(">H", v)
unity = (u32(0x10000) + u32(0) + u32(0) + u32(0) + u32(0x10000) + u32(0)
         + u32(0) + u32(0) + u32(0x40000000))
rate, nsamp, frames = 22050, 10, 2205
tkhd = box(b"tkhd", u32(7) + u32(0) + u32(0) + u32(2) + u32(0)
           + u32(nsamp * frames) + b"\0" * 8 + u16(0) * 4
           + unity + u32(0) + u32(0))
mdhd = box(b"mdhd", u32(0) + u32(0) + u32(0) + u32(rate)
           + u32(nsamp * frames) + u16(0x55c4) + u16(0))
hdlr = box(b"hdlr", u32(0) + u32(0) + b"soun" + b"\0" * 12)
smhd = box(b"smhd", u32(0) + u16(0) + u16(0))
dinf = box(b"dinf", box(b"dref", u32(0) + u32(1) + box(b"url ", u32(1))))
entry = (b"\0" * 6 + u16(1) + u16(0) + u16(0) + u32(0)
         + u16(1) + u16(16) + u16(0) + u16(0) + u32(rate << 16))
stsd = box(b"stsd", u32(0) + u32(1) + box(b"sowt", entry))
stts = box(b"stts", u32(0) + u32(1) + u32(nsamp) + u32(frames))
stsc = box(b"stsc", u32(0) + u32(1) + u32(1) + u32(nsamp) + u32(1))
stsz = box(b"stsz", u32(0) + u32(2 * frames) + u32(nsamp))
stco = box(b"stco", u32(0) + u32(1) + u32(0))
stbl = box(b"stbl", stsd + stts + stsc + stsz + stco)
minf = box(b"minf", smhd + dinf + stbl)
mdia = box(b"mdia", mdhd + hdlr + minf)
trak = box(b"trak", tkhd + mdia)
audio = bytearray()
for i in range(nsamp * frames):
    audio += struct.pack("<h",
        int(0.3 * 32767 * math.sin(2 * math.pi * 440 * i / rate)))
new_moov = bytearray(box(b"moov", moov_body + trak))
new_mdat = box(b"mdat", mdat_body + bytes(audio))
voff = moov_at + len(new_moov) + 8
aoff = voff + len(mdat_body)
i = new_moov.find(b"stco"); new_moov[i + 12:i + 16] = u32(voff)
j = new_moov.rfind(b"stco"); new_moov[j + 12:j + 16] = u32(aoff)
aud = ftyp + bytes(new_moov) + new_mdat
open(os.path.join(jn, "AUD.MOV"), "wb").write(aud)
os.makedirs(os.path.join(run, "printdest"), exist_ok=True)
print("staged formats journey: 4 ELFs + credential + JNEY(IPNG %dB, "
      "AUD.MOV %dB, PRINTSRC.PDF, INBOX/) + printdest" % (len(ipng), len(aud)))
PY

# --- the journey's three console bursts ------------------------------------
# GOREADER rides script2 ALONE: focus goes to the newest declare and its
# `p` must not race a sibling window opening (observed iter-01 of the
# first shape: AUD.MOV still loading when `p` fired, the keystroke went
# to its viewer and the print never happened). script3 then execs AUD,
# GOFILES, IPNG in that order -- the strip is declare order, so the
# IPNG viewer holds focus for `c` and a single alt-shift-tab from it
# lands on GOFILES.
# The share persists across vgate_repeat iterations (the harness clears
# only WINDOWS.SAV): the seat's M62e session files must go before the
# exec or iteration 2+ restores last boot's strip -- extra declares,
# extra execs, a different strip order. `vf rm` on the absent iter-01
# files just prints a not-found receipt; the delete is what matters.
vgate_file script-seat.txt <<'EOF'
vf rm SESSION.TABS
vf rm SESSION.PANES
exec GOTABWM.ELF
EOF

vgate_file script-apps.txt <<'EOF'
exec GOREADER.ELF /host/JNEY/PRINTSRC.PDF
EOF

vgate_file script-fm.txt <<'EOF'
exec GOVIEW.ELF -play /host/JNEY/AUD.MOV
exec -u0 GOFILES.ELF /host/JNEY/INBOX
exec -u0 GOVIEW.ELF /host/JNEY/IPNG.PNG
EOF

# --- journey: the one boot, replayed vgate_repeat times --------------------
# --screen arms the virtio-gpu the seat's WmctlRegister requires (the
# live-hostint shape); --sound the mixer AUD plays through; --host-print
# arms lane_print (PRINT/ appears at arm) and names the degrade's landing
# dir. The alt-shift-tab hop is sized by the strip's declare order: with
# tabs AUD, GOREADER, GOFILES, IPNG the one backwards step from the
# focused IPNG viewer is GOFILES -- the `p` three seconds later is the
# paste. exec-order chain: armed -> apps -> ready -> print seq=1 ->
# loaded -> copy -> pasted; nothing unanchored.
vgate_run journey -- \
    --screen '$RUN_DIR/screen' --sound --via-virtio \
    --host-print --host-print-dir '$RUN_DIR/printdest' \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-apps.txt' --script2-after 'gotabwm: hostint armed' \
    --input-chords 'p' --input-chords-after 'goreader: ready' \
    --script3 '$RUN_DIR/script-fm.txt' --script3-after 'gotabwm: hostint print seq=1' \
    --input-key 8 --input-key-after 'gview: loaded IPNG.PNG' \
    --input-chords2 'alt-shift-tab,p' --input-chords2-after 'gview: copy mime=image/png' \
    --script-expect 'gofiles: pasted PASTE' --script-expect-tail 40 --timeout 300

vgate_assert journey output-contains 'SOUND: virtio-snd attached'
vgate_assert journey output-contains 'host-int: armed lanes=print epoch='
vgate_assert journey serial-contains 'VirelaiOS kernel has seized control.'
vgate_assert journey serial-contains 'exec: loaded GOTABWM.ELF'
vgate_assert journey serial-contains 'gotabwm: registered'
vgate_assert journey serial-contains 'gotabwm: hostint armed epoch='
vgate_assert journey serial-contains 'lanes=print'
# The mixer's single-tenant posture: with the chime off the seat never
# takes a stream seat, so gen=1 is GOVIEW's.
vgate_assert journey serial-contains 'gotabwm: notify-sound off'
vgate_assert journey serial-contains 'gview: audio streaming gen=1'
# The print leg (M202c): the verb, the seat's signed record, the bare
# runner's honest `saved` degrade (a bundled panel is never claimed here).
vgate_assert journey serial-contains 'exec: loaded GOREADER.ELF'
vgate_assert journey serial-contains 'goreader: ready'
vgate_assert journey serial-contains 'goreader: print PRINTSRC.PDF bytes=1184'
vgate_assert journey serial-contains 'gotabwm: hostint print seq=1 name=PRINTSRC.PDF'
# The host dedups the landing name across iterations (PRINTSRC.PDF,
# PRINTSRC-2.PDF, ...) -- the seq + saved shape is what pins.
vgate_assert journey output-contains 'host-print: saved seq=1'
# The MOV audio leg (M202b): the soun track demuxed beside the video,
# decoded through virelai/pcm onto a mixer seat, receipts carrying the
# mixer's own frame count and the drift meter.
vgate_assert journey serial-contains 'gview: loaded AUD.MOV 160x120 MOV bytes=95861 frames=10 audio=sowt ch=1 rate=22050'
vgate_assert journey serial-contains 'gview: audio sowt ch=1 rate=22050 out=f32 rate=48000 ch=2'
vgate_assert journey serial-contains 'gview: audio streaming gen='
vgate_assert journey serial-contains 'audio_frames='
vgate_assert journey serial-contains 'drift_ms='
# The Adam7 leg (M202a): the interlaced load itself is the claim -- an
# image-unsupported refusal before the card. The viewer is tab-aware: a
# registered WM hosts this boot, not the shim.
vgate_assert journey serial-contains 'gview: tab-aware (full-viewport)'
vgate_assert journey serial-contains 'gview: loaded IPNG.PNG 7x5 PNG bytes=153'
# The clipboard leg (M202d): the typed write in GOVIEW, the one-hop focus
# switch, the adopt + paste-as-file in GOFILES.
vgate_assert journey serial-contains 'gview: copy mime=image/png n=153'
vgate_assert journey serial-contains 'exec: loaded GOFILES.ELF'
vgate_assert journey serial-contains 'gofiles: nav declare /host/JNEY/INBOX'
vgate_assert journey serial-contains 'gotabwm: alt-tab id='
# PASTE.PNG dedups to PASTE1..9 across iterations (the share persists) --
# the honest name is a prefix assert, the bytes are the share-equals.
vgate_assert journey serial-contains 'gofiles: clip-sys adopt image/png'
vgate_assert journey serial-contains 'gofiles: pasted PASTE'
# The honest absences: no format refusal, no clipboard refusal, no print
# refusal, no audio refusal, no crash.
vgate_assert journey serial-absent '[EXC] parking:'
vgate_assert journey serial-absent 'gview: unsupported'
vgate_assert journey serial-absent 'gview: image-'
vgate_assert journey serial-absent 'gview: copy refused'
vgate_assert journey serial-absent 'gview: audio refused'
vgate_assert journey serial-absent 'gview: audio stream err'
vgate_assert journey serial-absent 'gofiles: paste refused'
vgate_assert journey serial-absent 'goreader: print refused'
# The landed files: the clipboard payload byte-identical on the share
# (the file IS the clipboard body, never a re-encode) and the printed PDF
# byte-identical host-side (the bytes that crossed the glass ARE the
# fixture -- live-hostint run 08's check).
vgate_assert journey share-equals 'JNEY/INBOX/PASTE.PNG' 'ipng-ref.png'
vgate_assert journey python <<'PY'
import glob, os, re, sys
run = os.environ["RUN_DIR"]
# vgate_repeat writes per-iteration outputs (run-journey-01.out ...) —
# like the snapshot probes' serial glob, the newest is THIS boot's.
outs = glob.glob(os.path.join(run, "run-journey-*.out"))
if not outs:
    sys.exit("no run-journey-*.out beside the asserts")
out = open(max(outs, key=os.path.getmtime), errors="replace").read()
ser = open(os.environ["VG_SER"], errors="replace").read()
# The print landing: bytes equal the fixture, and every bare-runner lane
# line says saved -- never a claimed printed/panel (run-08's honesty).
want = open("user/go/goreader/testdata/dct.pdf", "rb").read()
# The host dedups the name across iterations (PRINTSRC.PDF, -2, -3) --
# this boot's landing is the newest PRINTSRC*.PDF and it byte-equals.
landings = glob.glob(os.path.join(run, "printdest", "PRINTSRC*.PDF"))
if not landings:
    sys.exit("FAIL: the degrade landing is absent")
landed = max(landings, key=os.path.getmtime)
if open(landed, "rb").read() != want:
    sys.exit("FAIL: landed bytes differ from the guest-side source")
for line in out.splitlines():
    if line.startswith("host-print:") and "saved" not in line:
        sys.exit("FAIL: bare-runner lane line claims a print: " + line[:80])
if out.count("host-print: saved") != 1:
    sys.exit("FAIL: saved count = %d" % out.count("host-print: saved"))
# "Plays audio" under a hosted seat, measured the honest way. Run 04's
# 250 ms absolute drift bound pins the WND shape; under GOTABWM the
# event-loop pump feeds a busier desk, so drift_ms opens at a ~1-3.4 s
# startup offset and creeps ~24 ms/loop on real underruns (observed
# 2121->3408 across 53 receipts, underruns 131->183). What the journey
# proves is the stream is LIVE and the mixer completes real-time output:
# every loop receipt lands, audio_frames advances ~48000 per ~1 s loop
# (the negotiated f32/48 kHz rate), never stalls. A feed that stopped
# would show a zero per-loop delta, not a rate.
drifts = [int(d) for d in re.findall(r"drift_ms=(\d+)", ser)]
if not drifts:
    sys.exit("no drift receipts in serial")
frames = [int(f) for f in re.findall(r"audio_frames=(\d+)", ser)]
if len(frames) != len(drifts):
    sys.exit("audio_frames/drift_ms receipt count mismatch")
if frames != sorted(frames) or frames[-1] < 48000:
    sys.exit("audio_frames not advancing a full loop: %r" % (frames,))
deltas = [b - a for a, b in zip(frames, frames[1:])]
if any(d < 30000 for d in deltas):
    sys.exit("audio_frames per-loop delta under 30000 -- the mixer feed "
             "stalled: %r" % (deltas,))
# The exec ledger: two GOVIEWs, one of each other app -- and the
# journey's own ordering, every leg's receipt postdating its anchor.
for elf, n in (("GOTABWM.ELF", 1), ("GOREADER.ELF", 1),
               ("GOFILES.ELF", 1), ("GOVIEW.ELF", 2)):
    if ser.count("exec: loaded " + elf) != n:
        sys.exit("exec count for %s != %d" % (elf, n))
# Per-leg ordering: each receipt postdates its own anchor. App load
# times vary (observed: `goreader: ready` ahead of the AUD.MOV load, the
# GOFILES nav declare behind the IPNG one), so only the pairs the
# exec-order rule actually guarantees are pinned.
pairs = [("goreader: ready", "goreader: print PRINTSRC.PDF"),
         ("goreader: print PRINTSRC.PDF", "gotabwm: hostint print seq=1"),
         ("gview: loaded IPNG.PNG", "gview: copy mime=image/png"),
         ("gview: copy mime=image/png", "gotabwm: alt-tab id="),
         ("gotabwm: alt-tab id=", "gofiles: clip-sys adopt image/png"),
         ("gofiles: clip-sys adopt image/png", "gofiles: pasted PASTE")]
for a, b in pairs:
    ia, ib = ser.find(a), ser.find(b)
    if ia < 0 or ib < 0 or ia >= ib:
        sys.exit("ordering broken: %r -> %r at %d/%d" % (a, b, ia, ib))
# And exactly one clipboard write reached the buffer: a second copy
# would mean a leg double-fired.
if ser.count("gview: copy ") != 1 or ser.count("gofiles: pasted") != 1:
    sys.exit("copy/paste counts wrong: %d/%d"
             % (ser.count("gview: copy "), ser.count("gofiles: pasted")))
print("journey: print landed byte-equal + saved-only, %d audio receipts "
      "advancing ~%d frames/loop (real-time rate), ordering and counts "
      "pinned" % (len(drifts), deltas[-1] if deltas else 0))
PY
