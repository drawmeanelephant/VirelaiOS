# go-welcome.spec — GOWELCOME.ELF (M195b #3307)
# class-B gate: the welcome tour's FIRST owning spec. Until now the app was
# exercised only incidentally (go-wm-default's first-boot and thin owning
# legs); this run pins the card's whole claim in one boot under the live
# Go seat:
#
#   boot -> gotabwm autostart -> exec GOWELCOME.ELF -> one Right press walks
#   the tour to gesture step 1 -> ctrl-shift-w closes the tab (the seat's
#   kind-20 ask collects the appver-1 blob before the close lands) ->
#   ctrl-shift-t re-execs the recorded bin -> restore_state hands the
#   envelope back -> the tour reports the same step, applied after its
#   first paint.
#
# The done-flag half (a finished tour never re-runs) is pinned by the
# host tests in user/go/welcome/tour_test.go — the leg needs the app's
# own serial markers, not a second close/reopen cycle.

vgate_name go-welcome "M195b #3307: GOWELCOME.ELF appver 1 envelope — advance/save/reopen step round-trip, first owning gate"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script-01.txt <<'EOF'
echo rx-go-welcome-boot
EOF

# GOMAXPROCS=1 is the run-22 (go-wm-default) Go-runtime headroom setting,
# kept for this app's exec.
vgate_file script2-01.txt <<'EOF'
set GOMAXPROCS=1
exec GOWELCOME.ELF
EOF

vgate_file script3-01.txt <<'EOF'
echo rx-go-welcome-restored
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
share = os.environ.get("VG_SHARE") or os.path.join(os.environ["RUN_DIR"], "share")
for name, how in (("GOWELCOME", "build-gowelcome.sh"),
                  ("GOTABWM", "build-gotabwm.sh")):
    src = os.path.join(".build", "go", name + ".ELF")
    if not os.path.exists(src):
        sys.exit(name + ".ELF missing (expected " + src + ") - build it first: "
                 "bash tools/go/" + how)
    shutil.copy(src, os.path.join(share, name + ".ELF"))
    print("staged %s.ELF into share (%d bytes)" %
          (name, os.path.getsize(os.path.join(share, name + ".ELF"))))
PY

# The seat autostarts (wm=gotabwm default); exec GOWELCOME once the seat is
# live. chords1 walks the tour one step once its ready marker says the
# frame settled; chords2 closes then reopens once the step marker proves
# the advance landed; script3 rides the restore marker.
vgate_run 01 -- \
    --screen '$RUN_DIR/welcome-screen' \
    --via-virtio \
    --script '$RUN_DIR/script-01.txt' \
    --script2 '$RUN_DIR/script2-01.txt' \
    --script2-after 'gotabwm: mode live' \
    --input-chords 'right' \
    --input-chords-after 'gowelcome: ready' \
    --input-chords2 'ctrl-shift-w,ctrl-shift-t' \
    --input-chords2-after 'gowelcome: step 1' \
    --script3 '$RUN_DIR/script3-01.txt' \
    --script3-after 'gowelcome: state restored' \
    --script-expect 'rx-go-welcome-restored' --timeout 300

vgate_assert 01 serial-contains 'wm: autostart gotabwm (settings wm=gotabwm)'
vgate_assert 01 serial-contains 'gotabwm: mode live'
vgate_assert 01 serial-contains 'exec: loaded GOWELCOME.ELF'
vgate_assert 01 serial-contains 'gowelcome: open id='
vgate_assert 01 serial-contains 'gowelcome: declare accepted'
vgate_assert 01 serial-contains 'gowelcome: present'
vgate_assert 01 serial-contains 'gowelcome: ready'

# The advance: one arrow press walks the tour off its first row.
vgate_assert 01 serial-contains 'gowelcome: step 1'

# Close: the seat's ask is answered (appver-1 envelope staged and stored),
# then the tab close lands.
vgate_assert 01 serial-contains 'gotabwm: state ask id='
vgate_assert 01 serial-contains 'gowelcome: state saved step=1'
vgate_assert 01 serial-contains 'gotabwm: state save id='
vgate_assert 01 serial-contains ' ver=1'
vgate_assert 01 serial-contains 'gotabwm: tab close id='
vgate_assert 01 serial-contains 'gowelcome: close'
vgate_assert 01 serial-contains 'gowelcome OK'

# Reopen: the seat re-execs the recorded bin, restore_state hands the blob
# back (its own copy is consumed on staging), and the app reports the
# round-trip — the tour resumes on the step the close saved.
vgate_assert 01 serial-contains 'gotabwm: reopen GOWELCOME.ELF'
vgate_assert 01 serial-contains 'gotabwm: state restore id='
vgate_assert 01 serial-contains 'gowelcome: state restored step=1'
vgate_assert 01 serial-contains 'rx-go-welcome-restored'

vgate_assert 01 serial-absent 'gowelcome: state refused'
vgate_assert 01 serial-absent 'gotabwm: state drop'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'exited status=139'
