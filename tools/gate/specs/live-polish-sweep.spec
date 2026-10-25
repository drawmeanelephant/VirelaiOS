# live-polish-sweep.spec -- the polish rotation's runnable half
# (#3668, goal #3337): every leg boots the session posture -- the Go
# seat autostarting on a share with no SETTINGS.TXT (the compiled
# wm=gotabwm default), NAT net, sound -- and walks image/apps.txt rows
# the way a dogfood session does: exec by registry name, the app's
# cheapest verifiable marker (cited from its owning spec, never
# re-implemented), then a clean `dui close`. Each leg sweeps the
# first-boot pair too (GOSH workspace + GOWELCOME tour open themselves
# when SESSION.TABS is missing), drops the session files so the next
# leg/repeat boots the same desk, and dumps procs+dui+ls so the python
# asserts can prove no leaked tasks, windows, or session state.
#
# exec-order: assert-proven -- legs end on console echoes, but every
# per-app assert reads guest-produced markers a program that never ran
# cannot print.

vgate_name live-polish-sweep "polish rotation dogfood sweep (#3668, goal #3337): default seat on NAT+sound, every shipped app opened by registry name -> cheapest verifiable action -> clean close, no leaked state"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 3 BOOTS
vgate_note "K=3: every leg repeats on the shared armed share; SESSION.TABS/SESSION.PANES are rm'd at each leg's end so every boot is the same first-boot desk"
vgate_note "the sweep drives the guest console over --console-tcp: one vgate_client per step, each --after-gated on the previous step's unique marker (every exec ends in a per-name 'procs <BIN> exited status=' line)"
vgate_note "window ids: the seat is id 2, the first-boot pair ids 3 (GOSH) and 4 (GOWELCOME) by boot order; the sweep closes them first, so every swept app lands on the lowest free slot -- id 3 -- and closes with 'dui close 3'"
vgate_note "only the LAST client's exit is harness-enforced (SPEC.md); every hop is double-pinned by serial asserts and the tail python ordering check"

# The marker-scan anchor script -- identical for every leg.
vgate_file boot.txt <<'EOF'
set GOMAXPROCS=1
echo sweep-console-armed
EOF

# First-boot pair closes (GOWELCOME id=4 then GOSH id=3) -- shared shape,
# per-leg file only so the chain reads top to bottom in one place.
vgate_file s1-close-pair.txt <<'EOF'
dui close 4
dui close 3
EOF
vgate_file s2-close-pair.txt <<'EOF'
dui close 4
dui close 3
EOF
vgate_file s3-close-pair.txt <<'EOF'
dui close 4
dui close 3
EOF
vgate_file s4-close-pair.txt <<'EOF'
dui close 4
dui close 3
EOF

# Each leg's tail: drop the seat's session files so the next boot
# re-takes the first-boot branch, then dump the registries for the
# leak asserts. The echo is the run's end marker.
vgate_file s1-tail.txt <<'EOF'
rm /host/SESSION.TABS
rm /host/SESSION.PANES
procs
dui
ls /host
echo s1-sweep-done
EOF
vgate_file s2-tail.txt <<'EOF'
rm /host/SESSION.TABS
rm /host/SESSION.PANES
procs
dui
ls /host
echo s2-sweep-done
EOF
vgate_file s3-tail.txt <<'EOF'
rm /host/SESSION.TABS
rm /host/SESSION.PANES
procs
dui
ls /host
echo s3-sweep-done
EOF
vgate_file s4-tail.txt <<'EOF'
rm /host/SESSION.TABS
rm /host/SESSION.PANES
procs
dui
ls /host
echo s4-sweep-done
EOF

# --- setup: stage the whole shipped fleet -----------------------------------
# Every image/apps.txt row's binary plus the Go seat itself, mirrored
# into the share -- the roster is DERIVED from the manifest so a new row
# fails the gate with its builder named, and the sweep can never drift
# from what the launcher would offer. TABWM.BIN comes from zig-out (the
# Zig fallback seat, ADR 0034). /host/GS is the manifest argv's
# Gostalgia root (--root /host/GS); the environment tree may be empty
# for this leg -- the open marker is the assertion.
vgate_setup_python <<'PY'
import os, re, shutil, sys
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")

def stage(src, dst, builder_hint):
    if not os.path.exists(src):
        sys.exit("%s missing (expected %s) -- build it first: %s"
                 % (dst, src, builder_hint))
    shutil.copy(src, os.path.join(share, dst))
    print("staged %s (%d bytes)" % (dst, os.path.getsize(src)))

manifest = []
for line in open("image/apps.txt"):
    line = line.strip()
    if not line or line.startswith("#"):
        continue
    manifest.append(line.split("|")[0].strip())

for binname in manifest:
    if binname == "TABWM.BIN":
        stage(os.path.join("zig-out", "bin", "TABWM.BIN"), "TABWM.BIN",
              "zig build tabwm")
        continue
    stage(os.path.join(".build", "go", binname), binname,
          "bash tools/go/build-%s.sh (ensure-guest-elf resolves it)"
          % binname[:-4].lower())
stage(os.path.join(".build", "go", "GOTABWM.ELF"), "GOTABWM.ELF",
      "bash tools/go/build-gotabwm.sh")
os.makedirs(os.path.join(share, "GS"), exist_ok=True)
# A first boot needs NO session files; the legs rm them again at close.
for f in ("SESSION.TABS", "SESSION.PANES", "WINDOWS.SAV"):
    p = os.path.join(share, f)
    if os.path.exists(p):
        os.unlink(p)
print("polish-sweep share: %d manifest rows staged + GOTABWM.ELF + GS/"
      % len(manifest))
PY

# =============================================================================
# leg s1 -- the docked daily set (manifest order, GOTERM last so its typed
# headline action lands on the focused tab). The seat's own first-boot
# open of GOSH+GOWELCOME (go-wm-default.spec) sweeps them too.
# =============================================================================
vgate_run s1 -- \
    --screen '$RUN_DIR/screen-s1' --via-virtio --net-nat --sound \
    --console-tcp '127.0.0.1:24711' \
    --script '$RUN_DIR/boot.txt' \
    --input-string $'echo sweep-daily\n' --input-string-after 'goterm: prompt' \
    --script-expect 's1-sweep-done' --script-expect-tail 12 --timeout 480

vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'gotabwm: mode live' \
    --send-text 'dui' --expect 'dui: windows=' --timeout 120 --out '$RUN_DIR/s1-base.out'

# The first-boot pair closes first: both ids are pinned by boot order
# (GOSH=3, GOWELCOME=4) so the sweep never guesses a swept app's slot.
vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'gosh: prompt' --retry-busy --after-timeout 420 \
    --send-text 'dui close 4' --expect 'procs GOWELCOME.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-welcome-close.out'
vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'procs GOWELCOME.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOSH.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-gosh-close.out'

# open->close pairs: each open waits for the previous task's per-name
# exit line; each close waits for the app's own ready marker and waits
# for the task exit, which frees window slot 3 for the next app.
vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'procs GOSH.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOCALC.ELF' --expect 'gocalc: present' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-gocalc-open.out'
vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'gocalc: present' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOCALC.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-gocalc-close.out'

vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'procs GOCALC.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec NOTE.ELF' --expect 'note: ready' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-note-open.out'
vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'note: ready' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs NOTE.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-note-close.out'

vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'procs NOTE.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOEDIT.ELF' --expect 'goedit: present' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-goedit-open.out'
vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'goedit: present' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOEDIT.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-goedit-close.out'

vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'procs GOEDIT.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOFILES.ELF' --expect 'gofiles: ready' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-gofiles-open.out'
vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'gofiles: ready' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOFILES.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-gofiles-close.out'

vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'procs GOFILES.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec WEB.ELF' --expect 'web: ready' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-web-open.out'
vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'web: ready' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs WEB.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-web-close.out'

vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'procs WEB.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec PULSE.ELF' --expect 'pulse: ready' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-pulse-open.out'
vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'pulse: ready' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs PULSE.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-pulse-close.out'

vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'procs PULSE.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOA11Y.ELF' --expect 'goa11y: present' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-goa11y-open.out'
vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'goa11y: present' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOA11Y.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-goa11y-close.out'

vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'procs GOA11Y.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOTOP.ELF' --expect 'top: ready' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-gotop-open.out'
vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'top: ready' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOTOP.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-gotop-close.out'

# GOTERM last: it holds focus for the typed headline action -- `echo
# sweep-daily` through its tty (go-term.spec / go-dogfood.spec's
# `goterm: line` + `goterm: done` idiom) -- and the close chains on the
# command's own completion marker, not the prompt.
vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'procs GOTOP.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOTERM.ELF' --expect 'goterm: prompt' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-goterm-open.out'
vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'goterm: done status=0' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOTERM.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s1-goterm-close.out'

vgate_client s1 -- --addr '127.0.0.1:24711' --after-timeout 420 --after 'procs GOTERM.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-file '$RUN_DIR/s1-tail.txt' --expect 's1-sweep-done' --timeout 120 --connect-timeout 30

vgate_assert s1 serial-contains 'wm: autostart gotabwm (settings wm=gotabwm)'
vgate_assert s1 serial-contains 'gotabwm: mode live'
vgate_assert s1 serial-contains 'gotabwm: present'
vgate_assert s1 serial-contains 'gotabwm: first-boot workspace'
vgate_assert s1 serial-contains 'gotabwm: welcome tour'
vgate_assert s1 serial-contains 'gosh: open id='
vgate_assert s1 serial-contains 'gowelcome: open id='

vgate_assert s1 serial-contains 'exec: loaded GOCALC.ELF'
vgate_assert s1 serial-contains 'gocalc: open id='
vgate_assert s1 serial-contains 'gocalc: declare accepted'
vgate_assert s1 serial-contains 'gocalc: present'
vgate_assert s1 serial-contains 'exec: loaded NOTE.ELF'
vgate_assert s1 serial-contains 'note: open id='
vgate_assert s1 serial-contains 'note: ready'
vgate_assert s1 serial-contains 'exec: loaded GOEDIT.ELF'
vgate_assert s1 serial-contains 'goedit: open id='
vgate_assert s1 serial-contains 'goedit: present'
vgate_assert s1 serial-contains 'exec: loaded GOFILES.ELF'
vgate_assert s1 serial-contains 'gofiles: open id='
vgate_assert s1 serial-contains 'gofiles: ready'
vgate_assert s1 serial-contains 'exec: loaded WEB.ELF'
vgate_assert s1 serial-contains 'web: ready'
vgate_assert s1 serial-contains 'exec: loaded PULSE.ELF'
vgate_assert s1 serial-contains 'pulse: ready'
vgate_assert s1 serial-contains 'exec: loaded GOA11Y.ELF'
vgate_assert s1 serial-contains 'goa11y: open id='
vgate_assert s1 serial-contains 'goa11y: present'
vgate_assert s1 serial-contains 'exec: loaded GOTOP.ELF'
vgate_assert s1 serial-contains 'top: open id='
vgate_assert s1 serial-contains 'top: ready'
vgate_assert s1 serial-contains 'exec: loaded GOTERM.ELF'
vgate_assert s1 serial-contains 'goterm: ready'
vgate_assert s1 serial-contains 'goterm: prompt'
vgate_assert s1 serial-contains 'goterm: line echo sweep-daily'
vgate_assert s1 serial-contains 'goterm: done status=0'

vgate_assert s1 serial-count 'dui close: closed=' 11
vgate_assert s1 serial-absent 'no such user window'
vgate_assert s1 serial-absent '[EXC] parking:'
vgate_assert s1 serial-absent 'exited status=139'
vgate_assert s1 serial-absent 'gocalc: declare refused'
vgate_assert s1 client-contains 's1-sweep-done'
vgate_assert s1 python <<'PY'
import os, re, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
# Every swept app: exec, then its own task exit, in exec order.
order = ["GOCALC.ELF", "NOTE.ELF", "GOEDIT.ELF", "GOFILES.ELF",
         "WEB.ELF", "PULSE.ELF", "GOA11Y.ELF", "GOTOP.ELF", "GOTERM.ELF"]
pos = 0
for name in order:
    for want in ("exec: loaded %s" % name,
                 "procs %s exited status=" % name):
        i = ser.find(want, pos)
        if i < 0:
            sys.exit("missing %r after offset %d" % (want, pos))
        pos = i + len(want)
for name in ("GOWELCOME.ELF", "GOSH.ELF"):
    if "procs %s exited status=" % name not in ser:
        sys.exit("first-boot app %s never exited" % name)
# Every tab the seat opened got closed: the open/close id sets are equal.
opens = re.findall(r"gotabwm: tab open id=(\d+)", ser)
closes = re.findall(r"dui close: closed=(\d+)", ser)
if sorted(opens) != sorted(closes):
    sys.exit("opened ids %s != closed ids %s" % (opens, closes))
# No swept task left running: the tail procs dump names no manifest binary.
i = ser.rfind("procs:", 0, ser.rfind("s1-sweep-done"))
dump = ser[i:ser.rfind("s1-sweep-done")] if i >= 0 else ""
running = re.findall(r"procs: id=\d+ name=(\S+).* state=running", dump)
leftover = [n for n in running if n.endswith((".ELF", ".BIN"))]
if leftover:
    sys.exit("tasks still running at sweep end: %s" % leftover)
share = os.environ["VG_SHARE"]
for f in ("SESSION.TABS", "SESSION.PANES", "WINDOWS.SAV"):
    if os.path.exists(os.path.join(share, f)):
        sys.exit("leaked session file on share: %s" % f)
print("s1: 9 swept apps + first-boot pair opened and closed in order; tab ids balanced; no leaked tasks or session files")
PY

# =============================================================================
# leg s2 -- the staged tools (GOHELP/GOSET/GOSNAP/GOSTORE/GOASSIST/GOREC/
# GOSTALGIA with its manifest argv / GOINFER).
# =============================================================================
vgate_run s2 -- \
    --screen '$RUN_DIR/screen-s2' --via-virtio --net-nat --sound \
    --console-tcp '127.0.0.1:24712' \
    --script '$RUN_DIR/boot.txt' \
    --script-expect 's2-sweep-done' --script-expect-tail 12 --timeout 480

vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'gotabwm: mode live' \
    --send-text 'dui' --expect 'dui: windows=' --timeout 120 --out '$RUN_DIR/s2-base.out'
vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'gosh: prompt' --retry-busy --after-timeout 420 \
    --send-file '$RUN_DIR/s2-close-pair.txt' --expect 'procs GOSH.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-pair-close.out'

vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'procs GOSH.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOHELP.ELF' --expect 'gohelp: ready' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-gohelp-open.out'
vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'gohelp: ready' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOHELP.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-gohelp-close.out'

vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'procs GOHELP.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOSET.ELF' --expect 'goset: ready' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-goset-open.out'
vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'goset: ready' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOSET.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-goset-close.out'

vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'procs GOSET.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOSNAP.ELF' --expect 'gosnap: ready' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-gosnap-open.out'
vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'gosnap: ready' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOSNAP.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-gosnap-close.out'

vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'procs GOSNAP.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOSTORE.ELF' --expect 'gostore: ready' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-gostore-open.out'
vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'gostore: ready' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOSTORE.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-gostore-close.out'

vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'procs GOSTORE.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOASSIST.ELF' --expect 'goassist: present' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-goassist-open.out'
vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'goassist: present' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOASSIST.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-goassist-close.out'

vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'procs GOASSIST.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOREC.ELF' --expect 'gorec: view open id=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-gorec-open.out'
vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'gorec: view open id=' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOREC.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-gorec-close.out'

# GOSTALGIA is exec'd with its manifest argv (image/apps.txt: `argv=shell
# --root /host/GS`) -- the same spelling the launcher prints in
# 'gotabwm: launcher exec GOSTALGIA.ELF argv=shell --root /host/GS'
# (go-gostalgia.spec run 06).
vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'procs GOREC.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOSTALGIA.ELF shell --root /host/GS' --expect 'gostalgia: tty attached window' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-gostalgia-open.out'
vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'gostalgia: tty attached window' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOSTALGIA.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-gostalgia-close.out'

vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'procs GOSTALGIA.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOINFER.ELF' --expect 'goinfer: view open id=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-goinfer-open.out'
vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'goinfer: view open id=' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOINFER.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s2-goinfer-close.out'

vgate_client s2 -- --addr '127.0.0.1:24712' --after-timeout 420 --after 'procs GOINFER.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-file '$RUN_DIR/s2-tail.txt' --expect 's2-sweep-done' --timeout 120 --connect-timeout 30

vgate_assert s2 serial-contains 'gotabwm: mode live'
vgate_assert s2 serial-contains 'gotabwm: first-boot workspace'
vgate_assert s2 serial-contains 'gotabwm: welcome tour'

vgate_assert s2 serial-contains 'exec: loaded GOHELP.ELF'
vgate_assert s2 serial-contains 'gohelp: open id='
vgate_assert s2 serial-contains 'gohelp: ready'
vgate_assert s2 serial-contains 'exec: loaded GOSET.ELF'
vgate_assert s2 serial-contains 'goset: declare accepted'
vgate_assert s2 serial-contains 'goset: ready'
vgate_assert s2 serial-contains 'exec: loaded GOSNAP.ELF'
vgate_assert s2 serial-contains 'gosnap: open id='
vgate_assert s2 serial-contains 'gosnap: ready n='
vgate_assert s2 serial-contains 'exec: loaded GOSTORE.ELF'
vgate_assert s2 serial-contains 'gostore: open id='
vgate_assert s2 serial-contains 'gostore: ready'
vgate_assert s2 serial-contains 'exec: loaded GOASSIST.ELF'
vgate_assert s2 serial-contains 'goassist: declare accepted'
vgate_assert s2 serial-contains 'goassist: present'
vgate_assert s2 serial-contains 'exec: loaded GOREC.ELF'
vgate_assert s2 serial-contains 'gorec: view open id='
vgate_assert s2 serial-contains 'exec: loaded GOSTALGIA.ELF'
vgate_assert s2 serial-contains 'gostalgia: tty attached window'
vgate_assert s2 serial-contains 'exec: loaded GOINFER.ELF'
vgate_assert s2 serial-contains 'goinfer: view open id='

vgate_assert s2 serial-count 'dui close: closed=' 10
vgate_assert s2 serial-absent 'no such user window'
vgate_assert s2 serial-absent '[EXC] parking:'
vgate_assert s2 serial-absent 'exited status=139'
vgate_assert s2 client-contains 's2-sweep-done'
vgate_assert s2 python <<'PY'
import os, re, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
order = ["GOHELP.ELF", "GOSET.ELF", "GOSNAP.ELF", "GOSTORE.ELF",
         "GOASSIST.ELF", "GOREC.ELF", "GOSTALGIA.ELF", "GOINFER.ELF"]
pos = 0
for name in order:
    for want in ("exec: loaded %s" % name,
                 "procs %s exited status=" % name):
        i = ser.find(want, pos)
        if i < 0:
            sys.exit("missing %r after offset %d" % (want, pos))
        pos = i + len(want)
opens = re.findall(r"gotabwm: tab open id=(\d+)", ser)
closes = re.findall(r"dui close: closed=(\d+)", ser)
if sorted(opens) != sorted(closes):
    sys.exit("opened ids %s != closed ids %s" % (opens, closes))
i = ser.rfind("procs:", 0, ser.rfind("s2-sweep-done"))
dump = ser[i:ser.rfind("s2-sweep-done")] if i >= 0 else ""
running = re.findall(r"procs: id=\d+ name=(\S+).* state=running", dump)
leftover = [n for n in running if n.endswith((".ELF", ".BIN"))]
if leftover:
    sys.exit("tasks still running at sweep end: %s" % leftover)
share = os.environ["VG_SHARE"]
for f in ("SESSION.TABS", "SESSION.PANES", "WINDOWS.SAV"):
    if os.path.exists(os.path.join(share, f)):
        sys.exit("leaked session file on share: %s" % f)
print("s2: 8 swept apps + first-boot pair opened and closed in order; tab ids balanced; no leaked tasks or session files")
PY

# =============================================================================
# leg s3 -- the undocked catalog (GOVIEW/GOSOUND/GOPAINT/GOSHEET/GOREADER/
# GOCLOCK/GOMINE/GOSOL/GOEMOJI). GOEMOJI is last and gets the leg's typed
# action: `fire` into its name filter (go-emoji.spec's own drive).
# =============================================================================
vgate_run s3 -- \
    --screen '$RUN_DIR/screen-s3' --via-virtio --net-nat --sound \
    --console-tcp '127.0.0.1:24713' \
    --script '$RUN_DIR/boot.txt' \
    --input-string $'fire\n' --input-string-after 'goemoji: present' \
    --script-expect 's3-sweep-done' --script-expect-tail 12 --timeout 480

vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'gotabwm: mode live' \
    --send-text 'dui' --expect 'dui: windows=' --timeout 120 --out '$RUN_DIR/s3-base.out'
vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'gosh: prompt' --retry-busy --after-timeout 420 \
    --send-file '$RUN_DIR/s3-close-pair.txt' --expect 'procs GOSH.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-pair-close.out'

vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'procs GOSH.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOVIEW.ELF' --expect 'gview: open id=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-goview-open.out'
vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'gview: open id=' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOVIEW.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-goview-close.out'

vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'procs GOVIEW.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOSOUND.ELF' --expect 'gosound: open id=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-gosound-open.out'
vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'gosound: open id=' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOSOUND.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-gosound-close.out'

vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'procs GOSOUND.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOPAINT.ELF' --expect 'gopaint: ready' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-gopaint-open.out'
vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'gopaint: ready' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOPAINT.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-gopaint-close.out'

vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'procs GOPAINT.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOSHEET.ELF' --expect 'gosheet: present' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-gosheet-open.out'
vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'gosheet: present' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOSHEET.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-gosheet-close.out'

# Bare exec: no document argv -- the honest empty state IS the action
# (go-reader.spec's `goreader: empty (no document)` marker).
vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'procs GOSHEET.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOREADER.ELF' --expect 'goreader: empty' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-goreader-open.out'
vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'goreader: empty' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOREADER.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-goreader-close.out'

vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'procs GOREADER.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOCLOCK.ELF' --expect 'goclock: painted epoch=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-goclock-open.out'
vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'goclock: painted epoch=' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOCLOCK.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-goclock-close.out'

vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'procs GOCLOCK.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOMINE.ELF' --expect 'gomine: present' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-gomine-open.out'
vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'gomine: present' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOMINE.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-gomine-close.out'

vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'procs GOMINE.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOSOL.ELF' --expect 'gosol: ready' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-gosol-open.out'
vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'gosol: ready' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOSOL.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-gosol-close.out'

vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'procs GOSOL.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOEMOJI.ELF' --expect 'goemoji: present' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-goemoji-open.out'
vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'goemoji: filter q=' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOEMOJI.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s3-goemoji-close.out'

vgate_client s3 -- --addr '127.0.0.1:24713' --after-timeout 420 --after 'procs GOEMOJI.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-file '$RUN_DIR/s3-tail.txt' --expect 's3-sweep-done' --timeout 120 --connect-timeout 30

vgate_assert s3 serial-contains 'gotabwm: mode live'
vgate_assert s3 serial-contains 'gotabwm: first-boot workspace'

vgate_assert s3 serial-contains 'exec: loaded GOVIEW.ELF'
vgate_assert s3 serial-contains 'gview: open id='
vgate_assert s3 serial-contains 'exec: loaded GOSOUND.ELF'
vgate_assert s3 serial-contains 'gosound: open id='
vgate_assert s3 serial-contains 'exec: loaded GOPAINT.ELF'
vgate_assert s3 serial-contains 'gopaint: open id='
vgate_assert s3 serial-contains 'gopaint: ready'
vgate_assert s3 serial-contains 'exec: loaded GOSHEET.ELF'
vgate_assert s3 serial-contains 'gosheet: open id='
vgate_assert s3 serial-contains 'gosheet: declare accepted'
vgate_assert s3 serial-contains 'gosheet: present'
vgate_assert s3 serial-contains 'exec: loaded GOREADER.ELF'
vgate_assert s3 serial-contains 'goreader: open id='
vgate_assert s3 serial-contains 'goreader: empty (no document)'
vgate_assert s3 serial-contains 'exec: loaded GOCLOCK.ELF'
vgate_assert s3 serial-contains 'goclock: declare accepted'
vgate_assert s3 serial-contains 'goclock: painted epoch='
vgate_assert s3 serial-contains 'exec: loaded GOMINE.ELF'
vgate_assert s3 serial-contains 'gomine: present'
vgate_assert s3 serial-contains 'exec: loaded GOSOL.ELF'
vgate_assert s3 serial-contains 'gosol: ready'
vgate_assert s3 serial-contains 'gosol: deal seed'
vgate_assert s3 serial-contains 'exec: loaded GOEMOJI.ELF'
vgate_assert s3 serial-contains 'goemoji: open id='
vgate_assert s3 serial-contains 'goemoji: present'
vgate_assert s3 serial-contains 'goemoji: filter q=fire'

vgate_assert s3 serial-count 'dui close: closed=' 11
vgate_assert s3 serial-absent 'no such user window'
vgate_assert s3 serial-absent '[EXC] parking:'
vgate_assert s3 serial-absent 'exited status=139'
vgate_assert s3 client-contains 's3-sweep-done'
vgate_assert s3 python <<'PY'
import os, re, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
order = ["GOVIEW.ELF", "GOSOUND.ELF", "GOPAINT.ELF", "GOSHEET.ELF",
         "GOREADER.ELF", "GOCLOCK.ELF", "GOMINE.ELF", "GOSOL.ELF",
         "GOEMOJI.ELF"]
pos = 0
for name in order:
    for want in ("exec: loaded %s" % name,
                 "procs %s exited status=" % name):
        i = ser.find(want, pos)
        if i < 0:
            sys.exit("missing %r after offset %d" % (want, pos))
        pos = i + len(want)
opens = re.findall(r"gotabwm: tab open id=(\d+)", ser)
closes = re.findall(r"dui close: closed=(\d+)", ser)
if sorted(opens) != sorted(closes):
    sys.exit("opened ids %s != closed ids %s" % (opens, closes))
i = ser.rfind("procs:", 0, ser.rfind("s3-sweep-done"))
dump = ser[i:ser.rfind("s3-sweep-done")] if i >= 0 else ""
running = re.findall(r"procs: id=\d+ name=(\S+).* state=running", dump)
leftover = [n for n in running if n.endswith((".ELF", ".BIN"))]
if leftover:
    sys.exit("tasks still running at sweep end: %s" % leftover)
share = os.environ["VG_SHARE"]
for f in ("SESSION.TABS", "SESSION.PANES", "WINDOWS.SAV"):
    if os.path.exists(os.path.join(share, f)):
        sys.exit("leaked session file on share: %s" % f)
print("s3: 9 swept apps + first-boot pair opened and closed in order; tab ids balanced; no leaked tasks or session files")
PY

# =============================================================================
# leg s4 -- the edges: two argv-required CLIs whose honest open IS the
# usage line and a self-exit (go-irc.spec / go-mail.spec), the vault,
# and the Zig fallback seat TABWM.BIN -- whose register is refused under
# a live seat (kernel/src/wm_server.zig: one seat; EACCES) and which
# then parks, so the sweep's clean close for it is `kill` by name.
# =============================================================================
vgate_run s4 -- \
    --screen '$RUN_DIR/screen-s4' --via-virtio --net-nat --sound \
    --console-tcp '127.0.0.1:24714' \
    --script '$RUN_DIR/boot.txt' \
    --script-expect 's4-sweep-done' --script-expect-tail 12 --timeout 360

vgate_client s4 -- --addr '127.0.0.1:24714' --after-timeout 420 --after 'gotabwm: mode live' \
    --send-text 'dui' --expect 'dui: windows=' --timeout 120 --out '$RUN_DIR/s4-base.out'
vgate_client s4 -- --addr '127.0.0.1:24714' --after-timeout 420 --after 'gosh: prompt' --retry-busy --after-timeout 420 \
    --send-file '$RUN_DIR/s4-close-pair.txt' --expect 'procs GOSH.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s4-pair-close.out'

vgate_client s4 -- --addr '127.0.0.1:24714' --after-timeout 420 --after 'procs GOSH.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOIRC.ELF' --expect 'usage: GOIRC.ELF' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s4-goirc-open.out'

vgate_client s4 -- --addr '127.0.0.1:24714' --after-timeout 420 --after 'procs GOIRC.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOMAIL.ELF' --expect 'usage: GOMAIL.ELF' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s4-gomail-open.out'

vgate_client s4 -- --addr '127.0.0.1:24714' --after-timeout 420 --after 'procs GOMAIL.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec GOVAULT.ELF' --expect 'govault: present' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s4-govault-open.out'
vgate_client s4 -- --addr '127.0.0.1:24714' --after-timeout 420 --after 'govault: present' --retry-busy --after-timeout 420 \
    --send-text 'dui close 3' --expect 'procs GOVAULT.ELF exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s4-govault-close.out'

vgate_client s4 -- --addr '127.0.0.1:24714' --after-timeout 420 --after 'procs GOVAULT.ELF exited status=' --retry-busy --after-timeout 420 \
    --send-text 'exec TABWM.BIN' --expect 'exec: loaded TABWM.BIN' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s4-tabwm-open.out'
vgate_client s4 -- --addr '127.0.0.1:24714' --after-timeout 420 --after 'exec: loaded TABWM.BIN' --retry-busy --after-timeout 420 \
    --send-text 'kill TABWM.BIN' --expect 'procs TABWM.BIN exited status=' --timeout 120 --connect-timeout 30 --out '$RUN_DIR/s4-tabwm-kill.out'

vgate_client s4 -- --addr '127.0.0.1:24714' --after-timeout 420 --after 'procs TABWM.BIN exited status=' --retry-busy --after-timeout 420 \
    --send-file '$RUN_DIR/s4-tail.txt' --expect 's4-sweep-done' --timeout 120 --connect-timeout 30

vgate_assert s4 serial-contains 'gotabwm: mode live'
vgate_assert s4 serial-contains 'gotabwm: first-boot workspace'

vgate_assert s4 serial-contains 'exec: loaded GOIRC.ELF'
vgate_assert s4 serial-contains 'usage: GOIRC.ELF'
vgate_assert s4 serial-contains 'procs GOIRC.ELF exited status='
vgate_assert s4 serial-contains 'exec: loaded GOMAIL.ELF'
vgate_assert s4 serial-contains 'usage: GOMAIL.ELF'
vgate_assert s4 serial-contains 'procs GOMAIL.ELF exited status='
vgate_assert s4 serial-contains 'exec: loaded GOVAULT.ELF'
vgate_assert s4 serial-contains 'govault: open id='
vgate_assert s4 serial-contains 'govault: present'
vgate_assert s4 serial-contains 'exec: loaded TABWM.BIN'
# The refused second registration: the Zig seat never reports
# `tabwm: registered` under the Go seat; the sweep ends its parked task.
vgate_assert s4 serial-absent 'tabwm: registered'
vgate_assert s4 serial-contains 'kill: TABWM.BIN'
vgate_assert s4 serial-contains 'procs TABWM.BIN exited status='

vgate_assert s4 serial-count 'dui close: closed=' 3
vgate_assert s4 serial-absent 'no such user window'
vgate_assert s4 serial-absent '[EXC] parking:'
vgate_assert s4 serial-absent 'exited status=139'
vgate_assert s4 client-contains 's4-sweep-done'
vgate_assert s4 python <<'PY'
import os, re, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
order = ["GOIRC.ELF", "GOMAIL.ELF", "GOVAULT.ELF", "TABWM.BIN"]
pos = 0
for name in order:
    for want in ("exec: loaded %s" % name,
                 "procs %s exited status=" % name):
        i = ser.find(want, pos)
        if i < 0:
            sys.exit("missing %r after offset %d" % (want, pos))
        pos = i + len(want)
opens = re.findall(r"gotabwm: tab open id=(\d+)", ser)
closes = re.findall(r"dui close: closed=(\d+)", ser)
if sorted(opens) != sorted(closes):
    sys.exit("opened ids %s != closed ids %s" % (opens, closes))
i = ser.rfind("procs:", 0, ser.rfind("s4-sweep-done"))
dump = ser[i:ser.rfind("s4-sweep-done")] if i >= 0 else ""
running = re.findall(r"procs: id=\d+ name=(\S+).* state=running", dump)
leftover = [n for n in running if n.endswith((".ELF", ".BIN"))]
if leftover:
    sys.exit("tasks still running at sweep end: %s" % leftover)
share = os.environ["VG_SHARE"]
for f in ("SESSION.TABS", "SESSION.PANES", "WINDOWS.SAV"):
    if os.path.exists(os.path.join(share, f)):
        sys.exit("leaked session file on share: %s" % f)
print("s4: GOIRC/GOMAIL usage-exits + GOVAULT close + parked TABWM.BIN killed; tab ids balanced; no leaked tasks or session files")
PY
