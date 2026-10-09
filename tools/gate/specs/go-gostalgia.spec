# go-gostalgia.spec -- M95b (#2010): the pinned Gostalgia tree built as a
# GOOS=virelai guest through the overlay mechanism, exercising the gsport
# adapters end to end: mem:// IPC (authenticated client round trip to the
# echo app), file primitives over the /host share, one sys_exec'd child,
# in-band shutdown, clean exit. The python assert checks SLOT NUMBERS via
# the decoded sys_* names, not strace's line layout (M94b moved it).
#
# HOST PREREQUISITE: `.build/go/GSSMOKE.ELF` via `bash
# tools/go/build-gostalgia.sh` (overlay leg; --no-overlay is the
# fail-before leg — the staged pin has no cmd/gssmoke and refuses).
#
# exec-order: assert-proven -- the run ends on `gssmoke: done`, a marker
# only the program emits after echo+spawn+shutdown complete.

vgate_name go-gostalgia "M95b: pinned Gostalgia on Virelai — gsport adapters (ipc/sig/fsys/proc) over ADR 0007"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 1 BOOTS

vgate_file script.txt <<'EOF'
strace exec GSSMOKE.ELF
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
# Isolate headless fixture boots from the default Go seat (same trick as
# live-strace): no WM seating, so the smoke runs on a quiet desktop.
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
