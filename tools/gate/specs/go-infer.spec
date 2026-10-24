# go-infer.spec — GOINFER.ELF (M195e #3310)
# class-B gate: the inference front-end's FIRST owning spec. Until now
# GOINFER ran only incidentally (live-gpu3d's console journeys); this leg
# pins the card's whole claim in one run under the live Go seat on the
# real Metal path:
#
#   boot -> gotabwm autostart -> exec GOINFER.ELF (bare -> the windowed
#   face) -> 's' arms the stub journey -> the K=5 transcript on serial is
#   the pinned golden (user/go/goinfer/testdata/golden/stub-console.txt —
#   the ids live-gpu3d leg 10 verified byte-identical on Metal) ->
#   ctrl-shift-w closes the tab (the seat's kind-20 ask collects the
#   appver-1 envelope — armed journey + log tail — before the close
#   lands) -> ctrl-shift-t re-execs the recorded bin -> restore_state
#   hands the blob back -> the face reports the armed journey and the
#   restored tail, and runs NOTHING: generation stays a click.

vgate_name go-infer "M195e #3310: GOINFER.ELF windowed stub journey — golden transcript + appver-1 envelope round-trip, first owning gate"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script-01.txt <<'EOF'
echo rx-go-infer-boot
EOF

# GOMAXPROCS=1 is the go-wm-default run-22 headroom setting, kept for
# this app's exec (the go-welcome convention).
vgate_file script2-01.txt <<'EOF'
set GOMAXPROCS=1
exec GOINFER.ELF
EOF

vgate_file script3-01.txt <<'EOF'
echo rx-go-infer-restored
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
share = os.environ.get("VG_SHARE") or os.path.join(os.environ["RUN_DIR"], "share")
rd = os.environ["RUN_DIR"]
for name, how in (("GOINFER", "build-goinfer.sh"),
                  ("GOTABWM", "build-gotabwm.sh")):
    src = os.path.join(".build", "go", name + ".ELF")
    if not os.path.exists(src):
        sys.exit(name + ".ELF missing (expected " + src + ") - build it first: "
                 "bash tools/go/" + how)
    shutil.copy(src, os.path.join(share, name + ".ELF"))
    print("staged %s.ELF into share (%d bytes)" %
          (name, os.path.getsize(os.path.join(share, name + ".ELF"))))
# The pinned stub transcript the run's transcript assert reuses — the
# same file the host-fake golden test pins, so gate and class-A agree.
shutil.copy(os.path.join("user", "go", "goinfer", "testdata", "golden",
                         "stub-console.txt"),
            os.path.join(rd, "stub-console.txt"))
PY

# The seat autostarts (wm=gotabwm default); exec GOINFER bare once the
# seat is live — a person clicking the "LLM Infer" launcher row. chords1
# 's' arms the stub journey once the view marker says the window opened;
# chords2 closes then reopens once the journey's `goinfer: done` proves
# the run finished; script3 rides the restore marker.
vgate_run 01 -- \
    --screen '$RUN_DIR/screen-01' \
    --gpu3d-metal \
    --via-virtio \
    --script '$RUN_DIR/script-01.txt' \
    --script2 '$RUN_DIR/script2-01.txt' \
    --script2-after 'gotabwm: mode live' \
    --input-chords 's' \
    --input-chords-after 'goinfer: view open id=' \
    --input-chords2 'ctrl-shift-w,ctrl-shift-t' \
    --input-chords2-after 'goinfer: done' \
    --script3 '$RUN_DIR/script3-01.txt' \
    --script3-after 'goinfer: state restored' \
    --script-expect 'rx-go-infer-restored' --timeout 300

vgate_assert 01 serial-contains 'wm: autostart gotabwm (settings wm=gotabwm)'
vgate_assert 01 serial-contains 'gotabwm: mode live'
vgate_assert 01 serial-contains 'gpu3d: armed did=0x1083'
vgate_assert 01 serial-contains 'exec: loaded GOINFER.ELF'
vgate_assert 01 serial-contains 'goinfer: view open id='

# The stub journey on the real Metal pipelines: the pinned ids sequence
# and the consistent answer, then the receipt rows on the share.
vgate_assert 01 serial-count 'goinfer: stub run k=' 5
vgate_assert 01 serial-count 'ids=196,99,196,196,196,196,196,196,99,133,99,99,99,99,99,133' 5
vgate_assert 01 serial-contains 'goinfer: stub consistent K=5'
vgate_assert 01 serial-contains 'goinfer: stub answer'
vgate_assert 01 serial-contains 'goinfer OK'
vgate_assert 01 serial-contains 'goinfer: done'

# The whole golden transcript — the file the class-A golden test pins —
# is an ordered subsequence of the serial log (the seat's own lines may
# interleave, so this is not a contiguity demand the seat can flake).
vgate_assert 01 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
golden = open(os.path.join(os.environ["RUN_DIR"], "stub-console.txt")).read()
pos = 0
for line in golden.splitlines():
    i = ser.find(line, pos)
    if i < 0:
        sys.exit("golden line missing or out of order: %r" % line)
    pos = i + len(line)
print("golden stub transcript is an ordered subsequence of the serial log")
# Restore never re-runs: the reopened face shows the closed session's
# tail and armed journey, so the run lines stay the first session's five.
if ser.count("goinfer: stub run k=") != 5:
    sys.exit("a journey auto-ran on restore — the person's click was skipped")
if ser.count("goinfer OK") != 1:
    sys.exit("more than one journey completed — the reopen ran a journey")
share = os.environ["VG_SHARE"]
body = open(os.path.join(share, "GOINFER", "RECEIPT.TSV")).read()
if body.count("stub\trun\t") != 5 or "stub\tconsistent\tK=5" not in body:
    sys.exit("receipt wants 5 run rows and the consistent row:\n%s" % body)
print("receipt rows verified")
PY

# Close: the seat's ask is answered (the appver-1 envelope — armed
# journey + log tail — is staged and stored) before the tab close lands.
vgate_assert 01 serial-contains 'gotabwm: state ask id='
vgate_assert 01 serial-contains 'gotabwm: state save id='
vgate_assert 01 serial-contains ' ver=1'
vgate_assert 01 serial-contains 'gotabwm: tab close id='

# Reopen: the seat re-execs the recorded bin, restore_state hands the
# blob back, and the face reports the armed journey plus the bounded
# tail (viewLogMax lines — the intro's first line has already scrolled
# off by save time). NOTHING re-runs: the python assert pins the run
# count at the closed session's five and 'goinfer OK' at one.
vgate_assert 01 serial-contains 'gotabwm: reopen GOINFER.ELF'
vgate_assert 01 serial-contains 'gotabwm: state restore id='
vgate_assert 01 serial-contains 'goinfer: state restored journey=stub tail=12'
vgate_assert 01 serial-contains 'rx-go-infer-restored'

vgate_assert 01 serial-absent 'goinfer: state refused'
vgate_assert 01 serial-absent 'gotabwm: state drop'
vgate_assert 01 serial-absent 'goinfer: error'
vgate_assert 01 serial-absent 'goinfer: diverged'
vgate_assert 01 serial-absent 'goinfer: unsupported'
vgate_assert 01 serial-absent 'gpu3d: timeout'
vgate_assert 01 serial-absent 'gpu3d: badresp'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'exited status=139'
vgate_assert 01 output-contains 'compute-refusals=0'
vgate_assert 01 output-contains 'refusals=0'
