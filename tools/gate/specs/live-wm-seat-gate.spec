# live-wm-seat-gate.spec -- M97g #2079: the WM seat is provenance-gated on VZ
#
# The audit finding: any EL0 process could take the WM seat with three
# syscalls (find the seat pid, same-uid kill it, sys_wmctl REGISTER) and
# receive the raw key/pointer fan-out plus the writable scanout -- a
# keylogger primitive plus pixel injection. REGISTER is now gated on
# kernel-recorded spawn provenance: kernel-context spawns (monitor exec,
# boot autostart), launcher children bearing the configured seat program
# name (INIT -> GOTABWM.ELF/TABWM.BIN), or cap_proc_admin. Everything an
# ordinary EL0 app can spawn is refused EACCES.
#
# This spec proves both directions in ONE boot with a GPU armed:
#   1. monitor `exec WNDSTUB.BIN` (kernel-spawned) still registers: the
#      seat lifecycle is byte-identical (registered, tick, present ok,
#      `wm: unregistered, shim resumed` on exit).
#   2. `exec WNDSTUB.BIN` typed into `GOSH.ELF serial` -- the SAME binary
#      as a launcher-class child of a kernel-spawned shell, carrying a
#      non-seat name -- is refused EACCES and reports `wndstub: register
#      refused`. `wndstub: registered` must appear EXACTLY ONCE: under an
#      unfixed kernel the GOSH child would register (the seat is free by
#      then) and print it a second time.
#
# HOST PREREQUISITE (fails honestly when missing):
#   bash tools/go/build-gosh.sh  ->  .build/go/GOSH.ELF
# WNDSTUB.BIN is part of the seeded zig-out bundle.

vgate_name live-wm-seat-gate "M97g #2079: WM seat REGISTER is provenance-gated on VZ"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

# Phase 1: the monitor registers WNDSTUB itself (kernel-spawned seat);
# the stub ticks, presents, exits, and the shim resumes -- all before the
# shell takes the console.
vgate_file script.txt <<'EOF'
wm
exec WNDSTUB.BIN
EOF

# Phase 2: after the seat released, hand the console to GOSH serial.
vgate_file script2.txt <<'EOF'
exec GOSH.ELF serial
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
src = os.path.join(".build", "go", "GOSH.ELF")
if not os.path.exists(src):
    sys.exit("GOSH.ELF missing (expected " + src + ") - build it first: "
             "bash tools/go/build-gosh.sh")
shutil.copy(src, os.path.join(share, "GOSH.ELF"))
print("staged %s (%d bytes)" % (src, os.path.getsize(src)))
# The typed line is raw console bytes (CR submits the line in GOSH's editor).
with open(os.path.join(rd, "typed-wm.bin"), "wb") as f:
    f.write(b"exec WNDSTUB.BIN\r")
PY

vgate_run 01 -- \
    --screen '$RUN_DIR/screen' \
    --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/script2.txt' --script2-after 'wm: unregistered, shim resumed' \
    --script3 '$RUN_DIR/typed-wm.bin' --script3-after 'gosh: attached' \
    --script-expect 'wndstub: register refused' --timeout 120

# The kernel-spawned registration (monitor exec) is unchanged end to end.
vgate_assert 01 serial-contains 'VirelaiOS kernel has seized control.'
vgate_assert 01 serial-contains 'wndstub: registered'
vgate_assert 01 serial-contains 'wndstub: tick'
vgate_assert 01 serial-contains 'wndstub: present ok'
vgate_assert 01 serial-contains 'wm: unregistered, shim resumed'
# The GOSH child tried: its submit record is the proof the exec ran in EL0
# (an EL0 `sys_exec` never prints the monitor's `exec: loaded` line).
vgate_assert 01 serial-contains 'gosh: attached'
vgate_assert 01 serial-contains 'gosh: line exec WNDSTUB.BIN'
# ...and was refused: the refusal marker landed and the seat stayed free
# (one registered marker total -- the kernel-spawned run only).
vgate_assert 01 serial-contains 'wndstub: register refused'
vgate_assert 01 serial-count 'wndstub: registered' 1
vgate_assert 01 serial-absent '[EXC] parking:'
