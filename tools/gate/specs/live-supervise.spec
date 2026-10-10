# Flat Go supervision: externally kill a child six times, admit five retries,
# pin jittered intervals and latest-only receipts, then prove never stays down.
# wm=none keeps supervisor + one multi-M child inside the existing task budget.
# The spec builds/stages its own fixture, without shared staging-tool changes.
# Kills go by pid: the monitor's `kill <name>` takes the LOWEST-id process of
# that name even when exited, so a name only ever reaches the first incarnation
# (second kill answers "already exited"). The registry hands out the first free
# slot and keeps exited rows, so the pids are boot-deterministic (0 demo, 1
# supervisor, 2-7 restart child, 8 never child); the python assert pins that.
# exec-order: assert-proven -- external monitor clients wait on supervisor
# readiness markers; program completion and receipts, not echoes, grade the run.
vgate_name live-supervise "M92c/d: retry policies and receipts; GOSET live, deferred and corrupt reload"
vgate_share arm
vgate_runner_flags -Xswiftc -DSPIKE

vgate_setup_python <<'PY'
import os, shutil, subprocess
from pathlib import Path
subprocess.run(["bash", "tools/go/build-svfixture.sh"], check=True)
for script in ("build-init.sh", "build-gotabwm.sh", "build-goset.sh"):
    subprocess.run(["bash", "tools/go/" + script], check=True)
share = Path(os.environ["RUN_DIR"]) / "share"
share.joinpath("SETTINGS.TXT").write_text("#v2\nwm=none\n")
for name in ("SVFIX.ELF", "SVFIXCH.ELF", "SVFIXNV.ELF"):
    shutil.copyfile(".build/go/SVFIX.ELF", share / name)
print("staged fresh pinned-fork supervisor and two argv-selected child names")
for name in ("INIT", "GOTABWM", "GOSET"):
    shutil.copyfile(".build/go/" + name + ".ELF", share / (name + ".ELF"))
subprocess.run(["python3", "tests/fixtures/init/boot/native.py", str(share)], check=True)
PY

vgate_file final-receipt.expected <<'EOF'
app=SVFIXCH
outcome=restart=5/5 status=137 backoff_s=0
last-log:
EOF
vgate_file never-receipt.expected <<'EOF'
app=SVFIXNV
outcome=restart=0/0 status=137 backoff_s=0
last-log:
EOF

vgate_run 01 -- --console-tcp '127.0.0.1:24788' --timeout 180
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'virelai>' \
    --send-text 'exec SVFIX.ELF' --expect 'svfixture: ready mode=restart n=1' \
    --timeout 30 --out '$RUN_DIR/client-launch.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=restart n=1' --retry-busy \
    --send-text 'kill 2' --expect 'svc: backoff name=SVFIX-RESTART k=1 ' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-kill1.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=restart n=2' --retry-busy \
    --send-text 'kill 3' --expect 'svc: backoff name=SVFIX-RESTART k=2 ' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-kill2.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=restart n=3' --retry-busy \
    --send-text 'kill 4' --expect 'svc: backoff name=SVFIX-RESTART k=3 ' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-kill3.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=restart n=4' --retry-busy \
    --send-text 'kill 5' --expect 'svc: backoff name=SVFIX-RESTART k=4 ' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-kill4.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=restart n=5' --retry-busy \
    --send-text 'kill 6' --expect 'svc: backoff name=SVFIX-RESTART k=5 ' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-kill5.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=restart n=6' --retry-busy \
    --send-text 'kill 7' --expect 'svc: failed name=SVFIX-RESTART reason=restart-limit ' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-kill6.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=never n=1' --retry-busy \
    --send-text 'kill 8' --expect 'svfixture: complete' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-never.out'

vgate_assert 01 serial-contains 'svfixture: complete'
vgate_assert 01 serial-contains 'svfixture: fifth receipt saved'
vgate_assert 01 serial-absent 'svfixture: FAIL'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'fatal error:'
vgate_assert 01 share-equals CRASH/SVFIXCH.TXT final-receipt.expected
vgate_assert 01 share-equals CRASH/SVFIXNV.TXT never-receipt.expected

vgate_assert 01 python <<'PY'
import os, re, shutil
from pathlib import Path
run = Path(os.environ["RUN_DIR"])
serial = Path(os.environ["VG_SER"]).read_text(errors="replace")
# A kernel line printed in several parts can be split by an EL0 write, which
# glues its tail ("10", "irq_ticks=") in front of our one-write marker. Match
# the marker and its line end, not the line start.
def matches(pattern):
    return list(re.finditer(pattern + r"\r?$", serial, re.M))
assert serial.count("svfixture: complete") == 1, "complete marker count"
starts = matches(r"svc: start name=SVFIX-RESTART pid=(\d+) t_ns=(\d+)")
exits = matches(r"svc: exit name=SVFIX-RESTART status=137 t_ns=(\d+)")
backoffs = matches(r"svc: backoff name=SVFIX-RESTART k=(\d+) delay_s=(\d+) t_ns=(\d+)")
failed = matches(r"svc: failed name=SVFIX-RESTART reason=restart-limit t_ns=(\d+)")
assert len(starts) == 6 and len(exits) == 6 and len(backoffs) == 5 and len(failed) == 1, \
    ("restart counts", len(starts), len(exits), len(backoffs), len(failed))
assert len({m[1] for m in starts}) == 6, "pid reused instead of a new child"
# The client commands above kill these pids; a moved sequence must fail closed.
assert [int(m[1]) for m in starts] == [2, 3, 4, 5, 6, 7], \
    ("pid sequence moved: update the kill targets", [m[1] for m in starts])
never_starts = matches(r"svc: start name=SVFIX-NEVER pid=(\d+) t_ns=\d+")
assert len(never_starts) == 1, "never respawned"
assert int(never_starts[0][1]) == 8, ("never pid moved", never_starts[0][1])
assert len(matches(r"svc: exit name=SVFIX-NEVER status=137 t_ns=\d+")) == 1, "never not killed"
assert "svc: backoff name=SVFIX-NEVER" not in serial
intervals = []
for i, (backoff, base_delay) in enumerate(zip(backoffs, (2, 4, 8, 8, 8))):
    k, delay, stamp = map(int, backoff.groups())
    assert k == i + 1 and base_delay <= delay < base_delay + 2, ("jitter", k, delay)
    assert starts[i].start() < exits[i].start() < backoff.start() < starts[i+1].start()
    interval = (int(starts[i+1][2]) - stamp) / 1e9
    assert delay <= interval <= base_delay + 2 + 1, ("interval", k, interval, delay)
    intervals.append((k, delay, interval))
assert starts[5].start() < exits[5].start() < failed[0].start(), "give-up ordering"
for i in range(1, 7):
    marker = "svfixture: ready mode=restart n=%d" % i
    # The monitor prints "kill: <name> armed" in parts, so match its prefix; a
    # refused kill prints "error: <name> already exited" and never this prefix.
    kill = "kill: SVFIXCH.ELF"
    ready = serial.index(marker)
    armed = serial.index(kill, ready)
    assert starts[i-1].start() < ready < armed < exits[i-1].start(), ("external kill", i)
assert serial.count("kill: SVFIXCH.ELF") == 6
assert serial.count("kill: SVFIXNV.ELF") == 1
for name, expected in [
    ("launch", "svfixture: ready mode=restart n=1"),
    *[("kill%d" % i, "svc: backoff name=SVFIX-RESTART k=%d " % i) for i in range(1, 6)],
    ("kill6", "svc: failed name=SVFIX-RESTART reason=restart-limit "),
    ("never", "svfixture: complete"),
]:
    assert expected in (run / ("client-%s.out" % name)).read_text(errors="replace"), \
        "external client failed: " + name
receipt5 = Path(os.environ["VG_SHARE"]) / "SVFIX5.TXT"
expected = ("app=SVFIXCH\noutcome=restart=5/5 status=137 backoff_s=%d\nlast-log:\n" %
            int(backoffs[4][2])).encode()
assert receipt5.read_bytes() == expected, "fifth receipt bytes"
# Preserve the checkpoint and measured intervals before the private share dies.
artifacts = Path("artifacts")
suffix = os.environ.get("VIRELAI_GATE_SUFFIX", "")
shutil.copyfile(receipt5, artifacts / ("live-supervise" + suffix + "-receipt5.txt"))
report = "\n".join("restart=%d delay_s=%d observed_interval_s=%.9f" % row for row in intervals) + "\n"
(artifacts / ("live-supervise" + suffix + "-intervals.txt")).write_text(report)
print(report, end="")
PY

# GOSET's HID edits and corrupt refusal share one init-owned boot. A valid
# empty session leaves the reserved shell's four tasks available to GOSET.
vgate_assert 01 python <<'PY'
import json, os
from pathlib import Path
share = Path(os.environ["VG_SHARE"])
(share / "SETTINGS.TXT").write_text("#v2\nwm=gotabwm\ninit=on\n")
(share / "SESSION.TABS").write_bytes(bytes([2, 0, 0, 1, 0, 0]))
(share / "INIT").mkdir(exist_ok=True)
m = {"version": 1, "services": [
    {"name": "seat", "argv": ["GOTABWM.ELF"], "restart": {"restart": "never"}, "class": "boot"},
    {"name": "boot", "argv": ["INITPRE.BIN"], "restart": {"restart": "never"}, "class": "boot"},
    {"name": "probe", "argv": ["INITDEP.BIN"], "restart": {"restart": "always",
        "backoff_base_s": 1, "backoff_cap_s": 1, "max_restarts": 3, "window": 32},
        "class": "on-demand", "enabled": False},
]}
(share / "INIT/SERVICES.JSON").write_text(json.dumps(m))
PY
vgate_file services-launch.txt <<'EOF'
exec GOSET.ELF
EOF
vgate_file services-corrupt.txt <<'EOF'
vf clone INIT/SERVICES.JSON INIT/SAVED.JSON
vf open INIT/SERVICES.JSON
vf truncate 0 0
vf write 0 64
vf fsync 0
vf close 0
procs
echo m92d-corrupt-staged
EOF
vgate_run toggle -- --screen '$RUN_DIR/screen-toggle' --via-virtio \
    --script '$RUN_DIR/services-launch.txt' --script-after 'init: seated' \
    --input-string $'services\nprobe=on\nprobe=off\nboot=off\n' --input-string-after 'goset: ready ' \
    --script2 '$RUN_DIR/services-corrupt.txt' --script2-after 'init: next boot name=boot' \
    --script-expect 'goset: services refuse invalid-json' --script-expect-tail 4 --timeout 90
vgate_assert toggle serial-contains 'goset: services n=3'
vgate_assert toggle serial-contains 'svc: start name=probe '
vgate_assert toggle serial-contains 'init: stop name=probe reason=config'
vgate_assert toggle serial-contains 'init: next boot name=boot'
vgate_assert toggle serial-contains 'init: reload refuse invalid-json'
vgate_assert toggle serial-contains 'goset: services refuse invalid-json'
vgate_assert toggle serial-absent '[EXC] parking:'
vgate_assert toggle serial-absent 'fatal error:'
vgate_assert toggle python <<'PY'
import json, os, re, shutil
from pathlib import Path
serial = Path(os.environ["VG_SER"]).read_text(errors="replace")
share = Path(os.environ["VG_SHARE"])
saved = share / "INIT/SAVED.JSON"
m = json.loads(saved.read_text())
states = {s["name"]: s.get("enabled", True) for s in m["services"]}
assert states == {"seat": True, "boot": False, "probe": False}, states
assert len(re.findall(r"svc: start name=probe pid=\d+ t_ns=\d+", serial)) == 1
assert len(re.findall(r"svc: exit name=probe status=137 t_ns=\d+", serial)) == 1
assert serial.index("goset: service saved name=probe enabled=on") < serial.index("svc: start name=probe ")
assert serial.index("svc: start name=probe ") < serial.index("init: stop name=probe reason=config")
assert serial.index("init: stop name=probe reason=config") < serial.index("svc: exit name=probe ")
assert "svc: backoff name=probe" not in serial
assert not (share / "CRASH/INITDEP.BIN.TXT").exists(), "explicit stop made a crash receipt"
assert "init: stop name=boot" not in serial, "boot toggle stopped live service"
assert len(re.findall(r"svc: start name=(?:boot|seat) ", serial)) == 2
assert re.search(r"procs: [^\n]*name=INITPRE.BIN [^\n]*state=running", serial)
assert re.search(r"procs: [^\n]*name=INITDEP.BIN [^\n]*state=exited", serial)
refusal = serial.index("init: reload refuse invalid-json")
assert not re.search(r"svc: (?:start|exit|backoff) ", serial[refusal:]), "corruption changed running set"
suffix = os.environ.get("VIRELAI_GATE_SUFFIX", "")
shutil.copyfile(saved, Path("artifacts") / ("live-supervise-services-saved.json" + suffix))
print("one boot: HID on/off, no restart or receipt, deferred boot, whole corrupt refusal")
PY
# Restore only the exact GOSET-published checkpoint, not a host-rebuilt config.
vgate_assert toggle python <<'PY'
import os, shutil
from pathlib import Path
share = Path(os.environ["VG_SHARE"])
shutil.copyfile(share / "INIT/SAVED.JSON", share / "INIT/SERVICES.JSON")
(share / "SESSION.TABS").write_bytes(bytes([2, 0, 0, 1, 0, 0]))
PY
vgate_file services-reboot.txt <<'EOF'
procs
echo m92d-persisted-observed
EOF
vgate_run persisted -- --screen '$RUN_DIR/screen-persisted' \
    --script '$RUN_DIR/services-reboot.txt' --script-after 'init: seated' \
    --script-expect 'm92d-persisted-observed' --script-expect-tail 3 --timeout 45
vgate_assert persisted serial-contains 'init: manifest ok n=1'
vgate_assert persisted serial-contains 'init: seated'
vgate_assert persisted serial-absent 'svc: start name=boot '
vgate_assert persisted serial-absent 'svc: start name=probe '
vgate_assert persisted serial-absent 'initpre: ready'
vgate_assert persisted serial-absent 'initdep: ready'
vgate_assert persisted serial-absent '[EXC] parking:'
vgate_assert persisted python <<'PY'
import os, re
from pathlib import Path
serial = Path(os.environ["VG_SER"]).read_text(errors="replace")
assert len(re.findall(r"svc: start name=seat ", serial)) == 1
assert re.search(r"procs: [^\n]*name=GOTABWM.ELF [^\n]*state=running", serial)
assert not re.search(r"procs: [^\n]*name=INIT(?:PRE|DEP).BIN", serial)
print("second boot: exact persisted boot/off and on-demand/off states observed")
PY
