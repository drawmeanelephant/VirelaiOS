# live-m14-composition.spec -- claim 3289 (Milestone 14, Card S3)
#
# class-B gate: the composition capstone. ONE EL0 process uses BOTH shared user
# services in ONE session:
#
#   S1 (claim 0169): the shared kernel clipboard. The gate pre-loads it with the
#   terminal's `clip` command, then the client pastes it (`sys_clipboard_get`,
#   slot 39) and copies the result back out (`sys_clipboard_set`, slot 38) - the
#   paste AND copy directions, live.
#   S2 (claim 7323): the per-process app timer. The client arms it, then BLOCKS
#   in poll/sleep and re-arms on every TIMER event (kind 9) - slot 40 is called
#   once per toggle plus the initial arm, which is the `calls=7` asserted below.
#
# M66c follow-on (#1485): the client moved from the retired Zig notepad to
# user/go/compose (GOCOMP.ELF), a purpose-built probe. The composition's subject
# is the two kernel services, not any one app's editing surface, and the probe is
# where they fit: GOEDIT.ELF - which took this card's other rehomes - is 240 bytes
# under the argv/sbrk data-budget wall documented in its own header, and 240 bytes
# is ~15 string literals, not a probe's worth of code. The probe also takes NO
# args on purpose: with argc == 0 the kernel packs no argv/envp block, so this
# run cannot be pushed over that wall by its own markers.
#
# HOST PREREQUISITE (fails the gate honestly when missing):
#   GO_BUILD_NAME=GOCOMP bash tools/go/build-go.sh user/go/compose/main.go

vgate_name live-m14-composition "claim 3289 (Milestone 14, Card S3)"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script.txt <<'EOF'
clip hello world
exec GOCOMP.ELF
exec -u0 GOCOMP.ELF
EOF

vgate_file script2.txt <<'EOF'
syscalls
echo composition-live-ok
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
src = os.path.join(".build", "go", "GOCOMP.ELF")
if not os.path.exists(src):
    sys.exit("GOCOMP.ELF missing (expected " + src + ") - build it first: "
             "GO_BUILD_NAME=GOCOMP bash tools/go/build-go.sh user/go/compose/main.go")
shutil.copy(src, os.path.join(share, "GOCOMP.ELF"))
print("staged GOCOMP.ELF into share (%d bytes)" % os.path.getsize(os.path.join(share, "GOCOMP.ELF")))
PY

vgate_run 01 -- --display --script '$RUN_DIR/script.txt' --script-after "tasks user-el0 exited status=7" --script2 '$RUN_DIR/script2.txt' --script2-after "compose: done" --script-expect "procs GOCOMP.ELF exited status=43" --timeout 90

# M97g (#2082): the first exec runs uid_user + no caps, so slot 39 refuses
# with EACCES (-7) even though `clip` loaded the buffer — the unprivileged
# app cannot read the clipboard. The `-u0` re-exec then proves the capable
# path still performs the full paste/copy/blink composition.
vgate_assert 01 serial-contains 'compose: paste refused rc=-7'
vgate_assert 01 serial-contains 'tasks user-exec exited status=2'
vgate_assert 01 serial-contains 'compose: pasted n=11'
vgate_assert 01 serial-contains 'compose: copied n=11'
vgate_assert 01 serial-contains 'compose: armed blink'
vgate_assert 01 serial-contains 'compose: blink'
vgate_assert 01 serial-contains 'compose: done'
vgate_assert 01 serial-contains 'compose: exiting 43'
vgate_assert 01 serial-contains 'tasks user-exec exited status=43'
vgate_assert 01 serial-contains 'syscalls: slots=64 implemented='
# M97g (#2082): the slot-39 counter counts both probes — the refused
# unprivileged call and the admin one — while slot 38 fires only in the
# capable run (the refused run exits before copy).
vgate_assert 01 serial-contains '38 sys_clipboard_set calls=1'
vgate_assert 01 serial-contains '39 sys_clipboard_get calls=2'
vgate_assert 01 serial-contains '40 sys_timer_set calls=7'
vgate_assert 01 serial-contains 'composition-live-ok'
vgate_assert 01 serial-absent '[EXC] parking:'

# The cadence, not just its presence: six toggles is six TIMER events serviced
# between the arm and the completion, which is what makes the re-arm loop (the
# whole point of S2 over a spin) observable rather than incidental.
vgate_assert 01 python <<'PY'
import os
ser = open(os.environ["VG_SER"]).read()
n = ser.count("compose: blink")
assert n == 6, "compose: blink count = %d want 6 (one per TIMER event)" % n
i_arm = ser.index("compose: armed blink")
i_done = ser.index("compose: done")
assert i_arm < ser.index("compose: blink") < i_done, "blinks must fall between the arm and the completion"
print("blink cadence: 6 toggles between arm and done")
PY
