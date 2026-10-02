# live-k4o — pinned CLI, not a library-only or host-only proof.
# B1 gives each child separate stdout/stderr; host engine oracles format bytes.
# Twelve children/boot bound the harness after an uncharacterised late refusal.
# exec-order: assert-proven -- done follows all waits; delayed pages follow reap.
vgate_name live-k4o "k4o CLI host goldens, explicit refusals and resource budgets"
vgate_share arm
vgate_runner_flags -Xswiftc -DSPIKE
vgate_fmt user/zig/k4o.zig user/zig/k4o/*.zig
vgate_repeat 1
vgate_setup_python <<'PY'
import os, pathlib, subprocess
rd = pathlib.Path(os.environ["RUN_DIR"])
subprocess.run(["python3", "tools/zig/k4o/check.py", "stage", "--share", str(rd / "share"),
                "--expected", str(rd / "expected")], check=True)
for batch in range(18):
    (rd / f"start-{batch}.txt").write_text(f"pages\nexec K4OGATE.BIN {batch}\n")
PY
vgate_file after.txt <<'EOF'
pages
procs
syscalls
echo done-k4o
EOF
vgate_file check.py <<'PY'
import json, os, pathlib, re, subprocess
rd = pathlib.Path(os.environ["RUN_DIR"])
serial = pathlib.Path(os.environ["VG_SER"]).read_text()
batch = int(os.environ["VG_TAG"].split("-")[0][1:])
cases = json.loads((rd / "expected/cases.json").read_text())[batch * 12:(batch + 1) * 12]
assert cases
assert serial.splitlines().count(f"k4o-gate: done cases={len(cases)}") == 1
assert all(serial.splitlines().count("k4o-gate: case " + c["id"]) == 1 for c in cases)
assert "k4o-gate: ArgumentLimit native length/count\n" in serial
assert "k4o-gate: FAIL" not in serial and "[EXC]" not in serial
for call in ("72 sys_getrandom", "73 sys_thread", "74 sys_futex"):
    assert call + " calls=0" in serial
pages = re.findall(r"^pages: armed=1 total=(0x[0-9a-f]+) free=(0x[0-9a-f]+)", serial, re.M)
assert len(pages) == 2 and pages[0] == pages[1], pages
assert serial.rfind("tasks user-exec reaped") < serial.rfind("pages: armed=1")
subprocess.run(["python3", "tools/zig/k4o/check.py", "compare", "--share", os.environ["VG_SHARE"],
                "--expected", str(rd / "expected"), "--evidence", "artifacts/live-k4o-bytes",
                "--batch", str(batch)], check=True)
PY
vgate_run b0 -- --script '$RUN_DIR/start-0.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b0 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b1 -- --script '$RUN_DIR/start-1.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b1 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b2 -- --script '$RUN_DIR/start-2.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b2 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b3 -- --script '$RUN_DIR/start-3.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b3 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b4 -- --script '$RUN_DIR/start-4.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b4 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b5 -- --script '$RUN_DIR/start-5.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b5 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b6 -- --script '$RUN_DIR/start-6.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b6 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b7 -- --script '$RUN_DIR/start-7.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b7 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b8 -- --script '$RUN_DIR/start-8.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b8 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b9 -- --script '$RUN_DIR/start-9.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b9 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b10 -- --script '$RUN_DIR/start-10.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b10 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b11 -- --script '$RUN_DIR/start-11.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b11 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b12 -- --script '$RUN_DIR/start-12.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b12 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b13 -- --script '$RUN_DIR/start-13.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b13 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b14 -- --script '$RUN_DIR/start-14.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b14 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b15 -- --script '$RUN_DIR/start-15.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b15 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b16 -- --script '$RUN_DIR/start-16.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b16 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_run b17 -- --script '$RUN_DIR/start-17.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'k4o-gate: done cases=' --script2-delay 1 --script-expect 'done-k4o' --timeout 90
vgate_assert b17 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
