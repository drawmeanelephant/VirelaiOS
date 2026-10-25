# live-clipboard.spec -- milestone-fourteen card S1 class-B gate (claim
#
# 0169): the bounded shared kernel clipboard on real VZ. Host scripted
# keystrokes drive the terminal half (`clip <text...>` sets it, `clip` pastes
# it) and assert the same buffer round-trips across multiple sets/gets, then
# `syscalls` proves the ADR 0007 slots 38/39 are wired into the live dispatch
# table (implemented=46).
#
# Mechanism: the production image is booted with the runner's scripted-input
#
# Issue #3443 (M202d) adds runs 02-04 — the typed seam's two new adopters on
# their REAL paths, one boot each (the exec-order rule: one app's serial
# receipt anchors the next app's exec, never two execs ungated):
#
#   02 image adopt -- GOVIEW.ELF ('c' copies the decoded PNG onto the kernel
#      clipboard as image/png through ClipboardSetTyped), then GOFILES.ELF's
#      `p` with an empty in-app queue adopts the payload and writes it back
#      to the share as INBOX/PASTE.PNG (host byte-compare: the file IS the
#      clipboard body, not a re-encode).
#   03 the editor's named refusal + text path -- with image/png still on
#      the clipboard GOEDIT.ELF's ctrl-v names its refusal (`goedit: paste
#      refused image/png`), then the editor's own copy/paste proves the
#      declared text/plain direction both ways (select-all, copy, paste,
#      save; the host compares the doubled buffer).
#   04 uri-list adopt + the text refusal -- `clip` seeds text/plain and
#      GOFILES refuses it by name; the app's own `c` then publishes
#      file:///host/CLIP/SRC/ONE.TXT, a SECOND GOFILES in DST pastes with
#      an empty in-app queue, adopts the uri-list, and ONE.TXT lands on
#      the share byte-identical — the "copy here, paste there" the typed
#      vocabulary promised.
#
# Runs 02/03 `wnd start` first (live-image-viewer: graphics windows need a
# compositing server — the bare shim refuses GOVIEW's open) and carry
# --display (no GPU, no windows: `gofiles: error open -1`, observed). Run
# 04 is the go-fileman shape: tty-grid windows on the kernel desktop need
# --screen + --input but no wnd.
#
# Every exec is `-u0` (CAP_CLIPBOARD — the dogfood boot-08 convention):
# slots 38/39 refuse a plain principal, so without it every leg would end
# on the rc=-7 receipt instead of the adoption.
#
# exec-order: assert-proven -- every exec's next stage is gated on that
# app's own serial receipt (loaded/copy/list/pasted/present), and each
# run ends on a marker only the last program prints (saved / echo after
# pasted); a program that never ran cannot reach its anchor.
#
# HOST PREREQUISITES (fail the gate honestly when missing):
#   bash tools/go/build-goview.sh -> .build/go/GOVIEW.ELF
#   bash tools/go/build-files.sh -> .build/go/GOFILES.ELF
#   bash tools/go/build-goedit.sh -> .build/go/GOEDIT.ELF

vgate_name live-clipboard "milestone-fourteen card S1 + M202d (#3443): the shared kernel clipboard and its typed adopters on VZ"
vgate_repeat 1 BOOTS
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script.txt <<'EOF'
clip hello world
clip
clip second
clip
syscalls
echo clip-live-ok
EOF

vgate_run 01 -- --script '$RUN_DIR/script.txt' --script-expect "clip-live-ok" --timeout 40

vgate_assert 01 serial-contains 'VirelaiOS kernel has seized control.'
vgate_assert 01 serial-contains 'clip: stored 11 bytes'
vgate_assert 01 serial-contains 'clip: hello world'
vgate_assert 01 serial-contains 'clip: stored 6 bytes'
vgate_assert 01 serial-contains 'clip: second'
vgate_assert 01 serial-contains 'syscalls: slots='
vgate_assert 01 serial-contains 'implemented='
vgate_assert 01 serial-contains '  38 sys_clipboard_set calls='
vgate_assert 01 serial-contains '  39 sys_clipboard_get calls='
vgate_assert 01 serial-contains 'clip-live-ok'
vgate_assert 01 serial-absent '[EXC] parking:'

# --- M202d (#3443): the adopters' staging -----------------------------------
# PIC.PNG is a pinned 2x2 truecolour PNG (90-ish bytes, far under the kernel
# clipboard's 512): synthesized byte-exact here so the host compares can be
# ==, not "looks like". CLIP/INBOX receives run 02's paste, CLIP/T.TXT is
# the editor's seeded buffer, CLIP/SRC+DST are run 03's uri-list endpoints.
vgate_setup_python <<'PY'
import os, struct, sys, zlib
share = os.environ.get("VG_SHARE") or os.path.join(os.environ["RUN_DIR"], "share")
for elf, builder in [("GOVIEW.ELF", "build-goview.sh"),
                     ("GOFILES.ELF", "build-files.sh"),
                     ("GOEDIT.ELF", "build-goedit.sh")]:
    src = os.path.join(".build", "go", elf)
    if not os.path.exists(src):
        sys.exit("%s missing (expected %s) - build it first: bash tools/go/%s"
                 % (elf, src, builder))
    import shutil
    shutil.copy(src, os.path.join(share, elf))

def chunk(tag, data):
    return (struct.pack(">I", len(data)) + tag + data +
            struct.pack(">I", zlib.crc32(tag + data) & 0xffffffff))
# 2x2 RGB, non-interlaced, 8-bit — a body ClipboardClassify spells
# image/png by magic, and webrender's DecodePNG accepts (type 2).
raw = b"\x00" + b"\xff\x00\x00\x00\xff\x00" + b"\x00" + b"\x00\x00\xff\xff\xff\x00"
png = (b"\x89PNG\r\n\x1a\n" +
       chunk(b"IHDR", struct.pack(">IIBBBBB", 2, 2, 8, 2, 0, 0, 0)) +
       chunk(b"IDAT", zlib.compress(raw)) +
       chunk(b"IEND", b""))
clip = os.path.join(share, "CLIP")
for d in ("", "INBOX", "SRC", "DST"):
    os.makedirs(os.path.join(clip, d), exist_ok=True)
open(os.path.join(clip, "PIC.PNG"), "wb").write(png)
open(os.path.join(clip, "T.TXT"), "wb").write(b"clipme!!")
open(os.path.join(clip, "SRC", "ONE.TXT"), "wb").write(b"one-body\n")
# wm=none -> shim desktop, deliberately (the go-fileman precedent): the
# Go seat's M108 host lane syncs the machine clipboard from the host side,
# and an in-flight sync overwriting the pinned PNG mid-leg would be a
# flake the leg cannot see. Under the shim the kernel buffer is exactly
# what these three apps put in it.
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    f.write("#v2\nwm=none\n")
print("staged 3 ELFs + CLIP tree (PIC.PNG %d bytes)" % len(png))
PY

# --- run 02: image/png adopt — GOVIEW's typed write -> GOFILES paste-as-file
# `wnd start` is load-bearing (the live-image-viewer shape): graphics windows
# need a compositing server — under the bare shim GOVIEW's open refuses
# (`gview: failed to open window`, observed run 1). Anchor chain:
# `wnd: registered` arms the GOVIEW exec, its loaded receipt arms the 'c'
# keystroke (the M124c typed WRITE), `gview: copy mime=image/png` arms the
# GOFILES exec, and the per-path nav-declare (post-paint: the window is
# live AND focused — `list` precedes the window and `ready` repeats per
# instance) arms `p`. The run ends on the pasted receipt; the host assert
# compares bytes.
vgate_file clip2a.txt <<'EOF'
wnd start
EOF

vgate_file clip2b.txt <<'EOF'
exec -u0 GOVIEW.ELF /host/CLIP/PIC.PNG
EOF

vgate_file clip2c.txt <<'EOF'
exec -u0 GOFILES.ELF /host/CLIP/INBOX
EOF

vgate_run 02 -- \
    --display --via-virtio \
    --script '$RUN_DIR/clip2a.txt' \
    --script2 '$RUN_DIR/clip2b.txt' --script2-after 'wnd: registered' \
    --input-key 8 --input-key-after 'gview: loaded PIC.PNG' \
    --script3 '$RUN_DIR/clip2c.txt' --script3-after 'gview: copy mime=image/png' \
    --input-string 'p' --input-string-after 'gofiles: nav declare /host/CLIP/INBOX' \
    --script-expect 'gofiles: pasted PASTE.PNG' --timeout 240

vgate_assert 02 serial-contains 'wnd: registered'
vgate_assert 02 serial-contains 'exec: loaded GOVIEW.ELF'
vgate_assert 02 serial-contains 'gview: loaded PIC.PNG'
vgate_assert 02 serial-contains 'gview: copy mime=image/png'
vgate_assert 02 serial-contains 'exec: loaded GOFILES.ELF'
vgate_assert 02 serial-contains 'gofiles: clip-sys adopt image/png'
vgate_assert 02 serial-contains 'gofiles: pasted PASTE.PNG'
vgate_assert 02 serial-absent '[EXC] parking:'

# The landing is the payload: INBOX/PASTE.PNG must hold exactly the bytes
# GOVIEW pushed — a paste that stored different bytes, or no file at all,
# fails here.
vgate_assert 02 python <<'PY'
import os, sys
share = os.environ["VG_SHARE"]
clip = os.path.join(share, "CLIP")
src = open(os.path.join(clip, "PIC.PNG"), "rb").read()
got = open(os.path.join(clip, "INBOX", "PASTE.PNG"), "rb").read()
if got != src:
    sys.exit("PASTE.PNG != clipboard payload: %d vs %d bytes" % (len(got), len(src)))
print("host compare: PASTE.PNG == clipboard payload (%d B)" % len(got))
PY

# --- run 03: the editor's named refusal + its text/plain round trip ---------
# Same wnd+GOVIEW lead-in (the clipboard still holds image/png when GOEDIT
# opens), then ctrl-v names the refusal and the copy/paste/save burst proves
# the declared text/plain direction both ways; the saved file is the seed
# doubled — a refusal that spliced anyway could never produce n=16.
vgate_file clip3a.txt <<'EOF'
wnd start
EOF

vgate_file clip3b.txt <<'EOF'
exec -u0 GOVIEW.ELF /host/CLIP/PIC.PNG
EOF

vgate_file clip3c.txt <<'EOF'
exec -u0 GOEDIT.ELF /host/CLIP/T.TXT
EOF

vgate_run 03 -- \
    --display --via-virtio \
    --script '$RUN_DIR/clip3a.txt' \
    --script2 '$RUN_DIR/clip3b.txt' --script2-after 'wnd: registered' \
    --input-key 8 --input-key-after 'gview: loaded PIC.PNG' \
    --script3 '$RUN_DIR/clip3c.txt' --script3-after 'gview: copy mime=image/png' \
    --input-chords 'ctrl-v' --input-chords-after 'goedit: present' \
    --input-chords2 'ctrl-a,ctrl-c,right,ctrl-v,ctrl-s' --input-chords2-after 'goedit: paste refused image/png' \
    --script-expect 'goedit: saved /host/CLIP/T.TXT' --timeout 240

vgate_assert 03 serial-contains 'gview: copy mime=image/png'
vgate_assert 03 serial-contains 'exec: loaded GOEDIT.ELF'
vgate_assert 03 serial-contains 'goedit: paste refused image/png'
vgate_assert 03 serial-contains 'goedit: copy n=8'
vgate_assert 03 serial-contains 'goedit: paste n=8'
vgate_assert 03 serial-contains 'goedit: saved /host/CLIP/T.TXT n=16'
vgate_assert 03 serial-absent '[EXC] parking:'

vgate_assert 03 python <<'PY'
import os, sys
share = os.environ["VG_SHARE"]
t = open(os.path.join(share, "CLIP", "T.TXT"), "rb").read()
if t != b"clipme!!clipme!!":
    sys.exit("T.TXT = %r, want the seed doubled" % t)
print("host compare: T.TXT doubled through copy+paste (%d B)" % len(t))
PY

# --- run 04: uri-list adopt + the text/plain refusal (GOFILES only) ---------
# The go-fileman shape verbatim: kernel desktop, no wnd (the file manager's
# window is the tty grid, not a graphics surface), --input + --via-virtio.
# `clip holdtext` seeds a text/plain body first so `p` names its refusal;
# the in-app `c` then overwrites the buffer with the file:// URI it
# publishes, and the SECOND GOFILES (empty in-app queue, different dir)
# adopts it — the adopted set crosses between two live apps.
vgate_file clip4a.txt <<'EOF'
clip holdtext
exec -u0 GOFILES.ELF /host/CLIP/SRC
EOF

vgate_file clip4b.txt <<'EOF'
exec -u0 GOFILES.ELF /host/CLIP/DST
EOF

vgate_file clip4c.txt <<'EOF'
echo rx-clip-adopt-ok
EOF

vgate_run 04 -- \
    --screen '$RUN_DIR/screen-04' --input --via-virtio \
    --script '$RUN_DIR/clip4a.txt' \
    --input-string 'p' --input-string-after 'gofiles: nav declare /host/CLIP/SRC' \
    --input-chords 'c' --input-chords-after 'gofiles: paste refused text/plain' \
    --script2 '$RUN_DIR/clip4b.txt' --script2-after 'gofiles: clip-sys n=' \
    --input-key 35 --input-key-after 'gofiles: nav declare /host/CLIP/DST' \
    --script3 '$RUN_DIR/clip4c.txt' --script3-after 'gofiles: pasted ONE.TXT' \
    --script-expect 'rx-clip-adopt-ok' --timeout 240

vgate_assert 04 serial-contains 'clip: stored 8 bytes'
vgate_assert 04 serial-contains 'exec: loaded GOFILES.ELF'
vgate_assert 04 serial-contains 'gofiles: paste refused text/plain'
vgate_assert 04 serial-contains 'gofiles: clip copy ONE.TXT'
vgate_assert 04 serial-contains 'gofiles: clip-sys n=29'
vgate_assert 04 serial-contains 'gofiles: list /host/CLIP/DST n=0'
vgate_assert 04 serial-contains 'gofiles: clip-sys adopt text/uri-list n=1'
vgate_assert 04 serial-contains 'gofiles: pasted ONE.TXT'
vgate_assert 04 serial-contains 'rx-clip-adopt-ok'
vgate_assert 04 serial-absent '[EXC] parking:'

vgate_assert 04 python <<'PY'
import os, sys
share = os.environ["VG_SHARE"]
src = open(os.path.join(share, "CLIP", "SRC", "ONE.TXT"), "rb").read()
got = open(os.path.join(share, "CLIP", "DST", "ONE.TXT"), "rb").read()
if got != src:
    sys.exit("DST/ONE.TXT = %r, want the src bytes %r" % (got, src))
print("host compare: DST/ONE.TXT == SRC/ONE.TXT (%d B, adopted via uri-list)" % len(got))
PY
