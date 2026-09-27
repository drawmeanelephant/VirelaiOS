# go-panic.spec -- issue #1228: GOOS=virelai phase 0c fault delivery.
#
# The fixture (tools/go/gopanic.go) forces REAL data aborts (loads from
# unmapped 0x8 through an opaque pointer) and prints one completion line
# per phase so this run is assert-proven by program output:
#   1. main goroutine faults, recovers, reports the runtime.Error text
#   2. a worker goroutine (a different M / kernel task) does the same
#   3. a faulting probe walks runtime.CallersFrames mid-panic and finds
#      runtime.sigpanic + the faulting function (the traceback proof)
#
# The kernel side: slot 75 sys_exnotify registers sigtramp (initsig);
# deliverable EL0 faults redirect to it with the fault record in x0-x6;
# virfaulthandler arms sigpanic on the faulting stack (recover() works).
#
# HOST PREREQUISITE (not hermetic — see tools/go/README.md):
# `just go-toolchain` must have produced .build/go/GOPANIC.ELF, and
# `bash tools/go/build-goself.sh` must have produced .build/go/GOSELF.ELF.
#
# exec-order: assert-proven -- --script2-after waits on the program's
# `go-panic done` line, then script2 launches GOSELF in its deliberate panic
# fixture mode. Its recovery publishes a receipt before printing the marker;
# the host byte-compares that share file before the final echo ends the run.

vgate_name go-panic "issue #1228 + #1772: GOOS=virelai panic recovery and per-app crash receipt on VZ"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script.txt <<'EOF'
exec GOPANIC.ELF
EOF

vgate_file script2.txt <<'EOF'
syscalls
exec GOSELF.ELF --panic-receipt-fixture
EOF

vgate_file script3.txt <<'EOF'
echo gopanic-held-window
EOF

vgate_file crash-receipt.expected <<'EOF'
app=GOSELF.ELF
outcome=panic: M82e fixture panic
last-log:
started
fixture: before panic
panic: M82e fixture panic
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
share = os.path.join(os.environ["RUN_DIR"], "share")
src = os.path.join(".build", "go", "GOPANIC.ELF")
if not os.path.exists(src):
    sys.exit("GOPANIC.ELF missing (expected " + src + ") - "
             "build the fork binaries first: just go-toolchain "
             "(fork prerequisites in tools/go/README.md)")
shutil.copy(src, os.path.join(share, "GOPANIC.ELF"))
print("staged GOPANIC.ELF into share (%d bytes)" %
      os.path.getsize(os.path.join(share, "GOPANIC.ELF")))
src = os.path.join(".build", "go", "GOSELF.ELF")
if not os.path.exists(src):
    sys.exit("GOSELF.ELF missing (expected " + src + ") - "
             "build it first: bash tools/go/build-goself.sh")
shutil.copy(src, os.path.join(share, "GOSELF.ELF"))
print("staged GOSELF.ELF into share (%d bytes)" %
      os.path.getsize(os.path.join(share, "GOSELF.ELF")))
PY

vgate_run 01 -- --script '$RUN_DIR/script.txt' --script2 '$RUN_DIR/script2.txt' --script2-after 'go-panic done' --script3 '$RUN_DIR/script3.txt' --script3-after 'goself: crash receipt written' --script-expect 'gopanic-held-window' --timeout 120

vgate_assert 01 serial-contains 'exec: loaded GOPANIC.ELF'
vgate_assert 01 serial-contains 'exec: loaded GOSELF.ELF'
vgate_assert 01 serial-contains 'go-panic procs=2'
vgate_assert 01 serial-contains 'go-panic main recovered=runtime error: invalid memory address'
vgate_assert 01 serial-contains 'go-panic worker recovered=runtime error: invalid memory address'
vgate_assert 01 serial-contains 'go-panic frames sigpanic=1 probe=1'
vgate_assert 01 serial-contains 'go-panic done'
vgate_assert 01 serial-contains 'goself: crash receipt written'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'exited status=139'
vgate_assert 01 share-equals CRASH/GOSELF.ELF.TXT crash-receipt.expected

vgate_assert 01 python <<'PY'
import os, re, sys
lines = open(os.environ["VG_SER"], errors="replace").read().splitlines()
def calls(slot):
    for l in lines:
        m = re.search(r"%d (sys_\w+) calls=(\d+)" % slot, l)
        if m:
            return int(m.group(2))
    return None
n_fault = calls(75)
if n_fault is None:
    sys.exit("FAIL: no sys_exnotify row in the syscalls report")
if n_fault < 1:
    sys.exit("FAIL: sys_exnotify register expected (>= 1 call), got %d" % n_fault)
print("go-panic python asserts OK: sys_exnotify=%d" % n_fault)
PY
