# go-welcome.spec — GOWELCOME.ELF (M195b #3307, #3769)
# class-B gate: the welcome tour's owning spec. Two boots under the live
# Go seat, one per flow the app now ships:
#
#   run 01  READ: `exec GOWELCOME.ELF --read` keeps the read-only walk —
#           one Right press walks the tour to gesture step 1 ->
#           ctrl-shift-w closes the tab (the seat's kind-20 ask collects
#           the appver-1 blob before the close lands) -> ctrl-shift-t
#           re-execs the recorded bin -> restore_state hands the envelope
#           back -> the tour reports the same step, applied after its
#           first paint.
#
#   run 02  TRY (the #3769 interactive tour): the seat's LIVE first-boot
#           autostart opens the bare exec — no monitor exec, so exactly
#           ONE instance exists and every chord lands on it. ctrl-space
#           opens the launcher (the sink takes kernel focus: a WIN_BLUR
#           the armed row counts) and Esc hands focus back; ctrl-shift-p
#           does the same for the palette. Right presses skip the rows
#           whose evidence cannot be driven in this boot (ctrl+tab needs
#           a second tab; the rail drag is unobservable by declaration),
#           and a pointer-virtio click on a launcher button is the last
#           row's press evidence. The done screen stands MSC_CELB and
#           prints its decode receipt.
#
# The done-flag half (a finished tour never re-runs) is pinned by the
# host tests in user/go/welcome/tour_test.go — the leg needs the app's
# own serial markers, not a second close/reopen cycle.

vgate_name go-welcome "M195b #3307 + #3769: GOWELCOME.ELF — read walk + try-it steps, done screen (MSC_CELB), envelope round-trip"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script-01.txt <<'EOF'
echo rx-go-welcome-boot
EOF

# GOMAXPROCS=1 is the run-22 (go-wm-default) Go-runtime headroom setting,
# kept for this app's exec.
vgate_file script2-01.txt <<'EOF'
set GOMAXPROCS=1
exec GOWELCOME.ELF --read
EOF

vgate_file script3-01.txt <<'EOF'
echo rx-go-welcome-restored
EOF

vgate_file script-02.txt <<'EOF'
echo rx-go-welcome-try
EOF

vgate_file script3-02.txt <<'EOF'
echo rx-go-welcome-done
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

# The seat autostarts (wm=gotabwm default); exec GOWELCOME --read once the
# seat is live. chords1 walks the tour one step once its ready marker says
# the frame settled; chords2 closes then reopens once the step marker
# proves the advance landed; script3 rides the restore marker.
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
# The --read argv names the flow — the autostart instance (try) prints
# its own 'mode try', and this assert names the exec'd one.
vgate_assert 01 serial-contains 'gowelcome: mode read'
# M203 (#3338): the delivered wave pose decoded off the share.
vgate_assert 01 serial-contains 'gowelcome: mascot qoi 64x64'

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

# Boot two: the autostart tour is the only instance — every chord lands
# on the same window. The choreography avoids Esc entirely (an Esc that
# races a modal's focus grab lands on the app, where it is the quit
# verb): the launcher toggles shut on a second ctrl-space, and the 'a'
# verb skips everything still pending once focus is home.
#
#   ready             -> pointer-virtio click on the Terminal button
#                        (press evidence -> try 0 done press, try 1
#                        armed; the exec refuses — GOTERM is not staged)
#   try 1 armed       -> chords1: ctrl-space,ctrl-space (the sink takes
#                        focus -> try 1 done blur; the toggle dismisses
#                        and restores it)
#   launcher dismiss  -> chords2: a (skip-all — each remaining row
#                        named; the unobservable drag can only ever
#                        take this path)
#   done mascot qoi   -> script3's echo ends the run
#
# Button geometry: the seat proposes the full 1280x720 viewport, so the
# 512x384 natural canvas scales x2.5/y1.875 — the Terminal button's
# natural (384,308,116,48) lands at screen (960,577,290,90), centre
# (1105,622).
vgate_run 02 -- \
    --screen '$RUN_DIR/welcome-try-screen' \
    --via-virtio \
    --script '$RUN_DIR/script-02.txt' \
    --pointer-virtio '1105,622,d;1105,622,u' \
    --pointer-virtio-after 'gowelcome: ready' \
    --input-chords 'ctrl-space,ctrl-space' \
    --input-chords-after 'gowelcome: try 1 armed' \
    --input-chords2 'a' \
    --input-chords2-after 'gotabwm: launcher dismiss' \
    --script3 '$RUN_DIR/script3-02.txt' \
    --script3-after 'gowelcome: done mascot qoi' \
    --script-expect 'rx-go-welcome-done' --timeout 300

vgate_assert 02 serial-contains 'wm: autostart gotabwm (settings wm=gotabwm)'
vgate_assert 02 serial-contains 'gotabwm: mode live'
# M122b: the LIVE first-boot autostart opened the tour itself — no
# monitor exec, so this run has exactly one instance.
vgate_assert 02 serial-contains 'gotabwm: welcome tour'
vgate_assert 02 serial-contains 'gowelcome: open id='
vgate_assert 02 serial-contains 'gowelcome: declare accepted'
vgate_assert 02 serial-contains 'gowelcome: present'
vgate_assert 02 serial-contains 'gowelcome: mode try'
vgate_assert 02 serial-contains 'gowelcome: try 0 armed'
vgate_assert 02 serial-contains 'gowelcome: ready'

# The press: GOTERM.ELF is not staged on this share, so the exec's own
# refusal line reports it — the button push itself is the step's
# evidence, and it counts.
vgate_assert 02 serial-contains 'gowelcome: launch fail GOTERM.ELF'
vgate_assert 02 serial-contains 'gowelcome: try 0 done press'
vgate_assert 02 serial-contains 'gowelcome: try 1 armed'

# The blur: the launcher's focus sink took the keys — the armed row's
# own report names the observed event, and the toggle's dismiss hands
# focus home.
vgate_assert 02 serial-contains 'gotabwm: launcher focus sink='
vgate_assert 02 serial-contains 'gowelcome: try 1 done blur'
vgate_assert 02 serial-contains 'gotabwm: launcher dismiss'

# The honest skip: the rail-drag row is unobservable by declaration —
# the skip-all verb names it, and every other pending row, on the wire.
vgate_assert 02 serial-contains 'gowelcome: try 3 skip'

# Completion: the observed counts (the seat's blur delivery decides how
# many rows tried vs skipped, so the pin is the reported line, not a
# guessed number), then the done screen's MSC_CELB decode receipt
# (#3687's mascot convention).
vgate_assert 02 serial-contains 'gowelcome: done tried='
vgate_assert 02 serial-contains 'gowelcome: done mascot qoi 64x64'
vgate_assert 02 serial-contains 'rx-go-welcome-done'

vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 serial-absent 'exited status=139'
