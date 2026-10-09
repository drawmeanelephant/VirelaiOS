# go-gostalgia-proc.spec -- M95e (#2013): the pinned Gostalgia child-process
# model over real kernel EL0 processes — gsport/child + supervise under the
# overlaid internal/process, driven by cmd/gsproc (GSPROC.ELF) inside the
# guest. Proven in one boot: spawn + bounded APPLOG capture (the only
# output channel an EL0 child has), clean exit 0, an EXTERNAL monitor kill
# -> status 137 + latest-only receipt, a BRK fault -> status 139 +
# receipt, and an on-failure crash loop that spends its 5-retry budget
# (6 starts, give-up, refused restart) with the registry's live rows back
# at baseline — no slot leak, no ENOSPC.
#
# wm=none keeps supervisor + fixture children inside the task budget.
# The fixture ELF is staged under four names (GSCHK/GSKILL/GSFAULT/GSLOOP)
# because the monitor's `kill <name>` reaches only the lowest-id row of a
# name — the kill child needs a name no other phase uses. The kill is a
# scripted second phase: --script2 fires once `gsproc: running name=GSKILL`
# appears in serial, the same claim-4613 sequencing live-kill uses.
#
# exec-order: assert-proven -- the run ends on `gsproc: complete`, a marker
# emitted only after every phase's state/code/restart accounting checked.

vgate_name go-gostalgia-proc "M95e: Gostalgia children as EL0 processes — bounded capture, receipts, M92c supervision"
vgate_share arm
vgate_runner_flags -Xswiftc -DSPIKE

vgate_setup_python <<'PY'
import os, shutil, subprocess
from pathlib import Path
subprocess.run(["bash", "tools/go/build-gschild.sh"], check=True)
share = Path(os.environ["RUN_DIR"]) / "share"
share.joinpath("SETTINGS.TXT").write_text("#v2\nwm=none\n")
for name in ("GSCHK.ELF", "GSKILL.ELF", "GSFAULT.ELF", "GSLOOP.ELF"):
    shutil.copyfile(".build/go/GSCHILD.ELF", share / name)
shutil.copyfile(".build/go/GSPROC.ELF", share / "GSPROC.ELF")
print("staged GSPROC + four childfix personalities")
PY

vgate_file launch.txt <<'EOF'
exec GSPROC.ELF
EOF
vgate_file kill.txt <<'EOF'
kill GSKILL.ELF
EOF

vgate_file kill-receipt.expected <<'EOF'
app=GSKILL
outcome=restart=0/0 status=137 backoff_s=0
last-log:
EOF
vgate_file fault-receipt.expected <<'EOF'
app=GSFAULT
outcome=restart=0/0 status=139 backoff_s=0
last-log:
childfix GSFAULT line 0
childfix GSFAULT line 1
EOF
vgate_file loop-receipt.expected <<'EOF'
app=GSLOOP
outcome=restart=5/5 status=3 backoff_s=0
last-log:
EOF

vgate_run 01 -- \
    --script '$RUN_DIR/launch.txt' \
    --script2 '$RUN_DIR/kill.txt' --script2-after 'gsproc: running name=GSKILL' \
    --script-expect 'gsproc: complete' --timeout 300

vgate_assert 01 serial-contains 'gsproc: ready baseline='
vgate_assert 01 serial-contains 'gsproc: exit name=GSCHK state=stopped code=0'
vgate_assert 01 serial-contains 'gsproc: applog GSCHK childfix GSCHK line 0'
vgate_assert 01 serial-contains 'gsproc: applog GSCHK childfix GSCHK line 3'
vgate_assert 01 serial-contains 'gsproc: capture name=GSCHK lines=4 truncated=false'
vgate_assert 01 serial-contains 'gsproc: running name=GSKILL pid='
vgate_assert 01 serial-contains 'kill: GSKILL.ELF armed'
vgate_assert 01 serial-contains 'gsproc: exit name=GSKILL state=failed code=137'
vgate_assert 01 serial-contains 'gsproc: exit name=GSFAULT state=failed code=139'
vgate_assert 01 serial-contains 'gsproc: loop name=GSLOOP state=failed starts=6 restarts=5 code=3'
vgate_assert 01 serial-contains 'gsproc: refused name=GSLOOP'
vgate_assert 01 serial-contains 'gsproc: procs baseline='
vgate_assert 01 serial-contains 'gsproc: complete'
vgate_assert 01 serial-absent 'gsproc: FAIL'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'ENOSPC'
vgate_assert 01 serial-absent 'fatal error:'
vgate_assert 01 share-equals CRASH/GSKILL.TXT kill-receipt.expected
vgate_assert 01 share-equals CRASH/GSFAULT.TXT fault-receipt.expected
vgate_assert 01 share-equals CRASH/GSLOOP.TXT loop-receipt.expected

vgate_assert 01 python <<'PY'
import os, re
from pathlib import Path
serial = Path(os.environ["VG_SER"]).read_text(errors="replace")

def lines(pattern):
    return list(re.finditer(pattern + r"\r?$", serial, re.M))

# Restart accounting: exactly six starts, six exits at status 3, five
# jittered backoffs in [1,3), one give-up — never a seventh spawn.
starts = lines(r"svc: start name=GSLOOP pid=(\d+) t_ns=\d+")
exits = lines(r"svc: exit name=GSLOOP status=3 t_ns=\d+")
backoffs = lines(r"svc: backoff name=GSLOOP k=(\d+) delay_s=(\d+) t_ns=\d+")
failed = lines(r"svc: failed name=GSLOOP reason=restart-limit t_ns=\d+")
assert (len(starts), len(exits), len(backoffs), len(failed)) == (6, 6, 5, 1), \
    ("loop counts", len(starts), len(exits), len(backoffs), len(failed))
assert len({m[1] for m in starts}) == 6, "pid reused instead of a new child"
for i, b in enumerate(backoffs):
    k, delay = int(b[1]), int(b[2])
    assert k == i + 1 and 1 <= delay < 3, ("jitter", k, delay)
assert "svc: start name=GSLOOP" not in serial[failed[0].end():], "spawn after give-up"

# External kill ordering: the monitor armed the kill while the child was
# running, before the supervisor observed status 137.
armed = serial.index("kill: GSKILL.ELF armed")
exit137 = lines(r"svc: exit name=GSKILL status=137 t_ns=\d+")
assert len(exit137) == 1 and armed < exit137[0].start(), "kill ordering"
assert "svc: backoff name=GSKILL" not in serial and \
       "svc: backoff name=GSFAULT" not in serial, "never policy retried"

# Clean exit wrote no receipt; receipts are failure records only.
share = Path(os.environ["VG_SHARE"])
assert not (share / "CRASH" / "GSCHK.TXT").exists(), "clean exit wrote a receipt"

# Live-slot accounting: no pid live at the end was absent at baseline.
m = re.search(r"gsproc: procs baseline=(\d+) leaked=(\d+)", serial)
assert m and m[2] == "0", ("slot leak", m.groups() if m else "missing")
print("loop 6x exit=3, five jittered backoffs, give-up, refused; kill armed before 137; no leaks")
PY
