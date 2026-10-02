# live-oliver: C1 refresh of #1177/#1188, not another near-identical gate.
# The frozen Oliver pin is built offline as A2's two-segment gap ELF.
# Library-only file proofs are separate from render/meta stdin/stdout/stderr.
# A sequential test launcher gives each CLI real B1 redirection and waits;
# the host compares every output byte to the independent pinned host CLI.
# Flat-image fixtures still prove the real 8x256 (2048-byte) argv boundary.
# exec-order: intentional -- flat refusal is synchronous; the CLI launcher
# waits for every child and the recovery stage is anchored on its reap.
vgate_name live-oliver "pinned Oliver: native library + bounded render/meta, EOF, isolated streams and argv boundaries"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 1
vgate_fmt user/zig/oliver*.zig tests/oliver-spike/flat-boundary.zig

vgate_setup_python <<'PY'
import os, pathlib, sys
sys.path.insert(0, str(pathlib.Path("tests/oliver-spike").resolve()))
import gate
gate.setup(pathlib.Path(os.environ["RUN_DIR"]))
PY

vgate_file cli1-start.txt <<'EOF'
pages
exec OPROBE.BIN 1
EOF
vgate_file cli2-start.txt <<'EOF'
pages
exec OPROBE.BIN 2
EOF
vgate_file cli3-start.txt <<'EOF'
pages
exec OPROBE.BIN 3
EOF
vgate_file cli4-start.txt <<'EOF'
pages
exec OPROBE.BIN 4
EOF
vgate_file cli5-start.txt <<'EOF'
pages
exec OPROBE.BIN 5
EOF
vgate_file cli-after.txt <<'EOF'
pages
echo oliver-recovered
EOF
vgate_run cli1 -- --script '$RUN_DIR/cli1-start.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/cli-after.txt' --script2-after 'procs OPROBE.BIN exited status=0' --script2-delay 1 --script-expect 'oliver-recovered' --timeout 180
vgate_run cli2 -- --script '$RUN_DIR/cli2-start.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/cli-after.txt' --script2-after 'procs OPROBE.BIN exited status=0' --script2-delay 1 --script-expect 'oliver-recovered' --timeout 180
vgate_run cli3 -- --script '$RUN_DIR/cli3-start.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/cli-after.txt' --script2-after 'procs OPROBE.BIN exited status=0' --script2-delay 1 --script-expect 'oliver-recovered' --timeout 180
vgate_run cli4 -- --script '$RUN_DIR/cli4-start.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/cli-after.txt' --script2-after 'procs OPROBE.BIN exited status=0' --script2-delay 1 --script-expect 'oliver-recovered' --timeout 180
vgate_run cli5 -- --script '$RUN_DIR/cli5-start.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/cli-after.txt' --script2-after 'procs OPROBE.BIN exited status=0' --script2-delay 1 --script-expect 'oliver-recovered' --timeout 180
vgate_assert cli1 serial-absent 'oliver-probe: FAIL'
vgate_assert cli1 serial-absent '[EXC]'
vgate_assert cli1 python <<'PY'
import os, pathlib, sys
sys.path.insert(0, str(pathlib.Path("tests/oliver-spike").resolve()))
import gate
gate.verify(pathlib.Path(os.environ["RUN_DIR"]))
PY
vgate_assert cli2 serial-absent 'oliver-probe: FAIL'
vgate_assert cli2 serial-absent '[EXC]'
vgate_assert cli2 python <<'PY'
import os, pathlib, sys
sys.path.insert(0, str(pathlib.Path("tests/oliver-spike").resolve()))
import gate
gate.verify(pathlib.Path(os.environ["RUN_DIR"]))
PY
vgate_assert cli3 serial-absent 'oliver-probe: FAIL'
vgate_assert cli3 serial-absent '[EXC]'
vgate_assert cli3 python <<'PY'
import os, pathlib, sys
sys.path.insert(0, str(pathlib.Path("tests/oliver-spike").resolve()))
import gate
gate.verify(pathlib.Path(os.environ["RUN_DIR"]))
PY
vgate_assert cli4 serial-absent 'oliver-probe: FAIL'
vgate_assert cli4 serial-absent '[EXC]'
vgate_assert cli4 python <<'PY'
import os, pathlib, sys
sys.path.insert(0, str(pathlib.Path("tests/oliver-spike").resolve()))
import gate
gate.verify(pathlib.Path(os.environ["RUN_DIR"]))
PY
vgate_assert cli5 serial-absent 'oliver-probe: FAIL'
vgate_assert cli5 serial-absent '[EXC]'
vgate_assert cli5 python <<'PY'
import os, pathlib, sys
sys.path.insert(0, str(pathlib.Path("tests/oliver-spike").resolve()))
import gate
gate.verify(pathlib.Path(os.environ["RUN_DIR"]))
PY

vgate_file flat-fit.txt <<'EOF'
exec FIT.BIN alpha beta
EOF
vgate_file flat-near.txt <<'EOF'
exec NEAR.BIN alpha beta
echo flat-refused
EOF
vgate_file flat-far.txt <<'EOF'
exec FAR.BIN alpha beta
EOF
vgate_file flat-default.txt <<'EOF'
exec NEAR.BIN
EOF
vgate_run fit -- --script '$RUN_DIR/flat-fit.txt' --script-after 'tasks user-el0 reaped' --script-expect 'oliver-flat: argv256 ok' --timeout 90
vgate_run near -- --script '$RUN_DIR/flat-near.txt' --script-after 'tasks user-el0 reaped' --script-expect 'flat-refused' --timeout 90
vgate_run far -- --script '$RUN_DIR/flat-far.txt' --script-after 'tasks user-el0 reaped' --script-expect 'oliver-flat: argv256 ok' --timeout 90
vgate_run defaults -- --script '$RUN_DIR/flat-default.txt' --script-after 'tasks user-el0 reaped' --script-expect 'oliver-flat: argv256 ok' --timeout 90
vgate_assert fit serial-exact 'oliver-flat: argv256 ok' 1
vgate_assert fit serial-absent 'error: '
vgate_assert fit serial-absent '[EXC]'
vgate_assert near serial-contains 'error: image leaves no room for the argv block'
vgate_assert near serial-absent 'exec: loaded NEAR.BIN'
vgate_assert near serial-absent 'oliver-flat: argv256 ok'
vgate_assert far serial-exact 'oliver-flat: argv256 ok' 1
vgate_assert far serial-absent 'error: '
vgate_assert far serial-absent '[EXC]'
vgate_assert defaults serial-exact 'oliver-flat: argv256 ok' 1
vgate_assert defaults serial-absent 'error: '
vgate_assert defaults serial-absent '[EXC]'
