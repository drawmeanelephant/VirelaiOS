# Flat Go supervision: externally kill a child six times, admit five retries,
# pin jittered intervals and latest-only receipts, then prove never stays down.
# wm=none keeps supervisor + one multi-M child inside the existing task budget.
# The spec builds/stages its own fixture, without shared staging-tool changes.
# exec-order: assert-proven -- external monitor clients wait on supervisor
# readiness markers; program completion and receipts, not echoes, grade the run.
vgate_name live-supervise "M92c: external kills, five jittered retries, receipts and terminal policies"
vgate_share arm
vgate_runner_flags -Xswiftc -DSPIKE

vgate_setup_python <<'PY'
import os, shutil, subprocess
from pathlib import Path
subprocess.run(["bash", "tools/go/build-svfixture.sh"], check=True)
share = Path(os.environ["RUN_DIR"]) / "share"
share.joinpath("SETTINGS.TXT").write_text("#v2\nwm=none\n")
for name in ("SVFIX.ELF", "SVFIXCH.ELF", "SVFIXNV.ELF"):
    shutil.copyfile(".build/go/SVFIX.ELF", share / name)
print("staged fresh pinned-fork supervisor and two argv-selected child names")
PY

vgate_file final-receipt.expected <<'EOF'
app=SVFIX-RESTART
outcome=restart=5/5 status=137 backoff_s=0
last-log:
EOF
vgate_file never-receipt.expected <<'EOF'
app=SVFIX-NEVER
outcome=restart=0/0 status=137 backoff_s=0
last-log:
EOF

vgate_run 01 -- --console-tcp '127.0.0.1:24788' --timeout 180
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'virelai>' \
    --send-text 'exec SVFIX.ELF' --expect 'svfixture: ready mode=restart n=1' \
    --timeout 30 --out '$RUN_DIR/client-launch.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=restart n=1' --retry-busy \
    --send-text 'kill SVFIXCH.ELF' --expect 'svc: backoff name=SVFIX-RESTART k=1 ' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-kill1.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=restart n=2' --retry-busy \
    --send-text 'kill SVFIXCH.ELF' --expect 'svc: backoff name=SVFIX-RESTART k=2 ' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-kill2.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=restart n=3' --retry-busy \
    --send-text 'kill SVFIXCH.ELF' --expect 'svc: backoff name=SVFIX-RESTART k=3 ' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-kill3.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=restart n=4' --retry-busy \
    --send-text 'kill SVFIXCH.ELF' --expect 'svc: backoff name=SVFIX-RESTART k=4 ' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-kill4.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=restart n=5' --retry-busy \
    --send-text 'kill SVFIXCH.ELF' --expect 'svc: backoff name=SVFIX-RESTART k=5 ' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-kill5.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=restart n=6' --retry-busy \
    --send-text 'kill SVFIXCH.ELF' --expect 'svc: failed name=SVFIX-RESTART reason=restart-limit ' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-kill6.out'
vgate_client 01 -- --addr '127.0.0.1:24788' --after 'svfixture: ready mode=never n=1' --retry-busy \
    --send-text 'kill SVFIXNV.ELF' --expect 'svfixture: complete' \
    --after-timeout 170 --timeout 30 --out '$RUN_DIR/client-never.out'

vgate_assert 01 serial-exact 'svfixture: complete' 1
vgate_assert 01 serial-contains 'svfixture: fifth receipt saved'
vgate_assert 01 serial-absent 'svfixture: FAIL'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'fatal error:'
vgate_assert 01 share-equals CRASH/SVFIX-RESTART.TXT final-receipt.expected
vgate_assert 01 share-equals CRASH/SVFIX-NEVER.TXT never-receipt.expected

vgate_assert 01 python <<'PY'
import os, re, shutil
from pathlib import Path
run = Path(os.environ["RUN_DIR"])
serial = Path(os.environ["VG_SER"]).read_text(errors="replace")
def matches(pattern):
    return list(re.finditer("^" + pattern + r"\r?$", serial, re.M))
starts = matches(r"svc: start name=SVFIX-RESTART pid=(\d+) t_ns=(\d+)")
exits = matches(r"svc: exit name=SVFIX-RESTART status=137 t_ns=(\d+)")
backoffs = matches(r"svc: backoff name=SVFIX-RESTART k=(\d+) delay_s=(\d+) t_ns=(\d+)")
failed = matches(r"svc: failed name=SVFIX-RESTART reason=restart-limit t_ns=(\d+)")
assert len(starts) == 6 and len(exits) == 6 and len(backoffs) == 5 and len(failed) == 1, \
    ("restart counts", len(starts), len(exits), len(backoffs), len(failed))
assert len({m[1] for m in starts}) == 6, "pid reused instead of a new child"
assert len(matches(r"svc: start name=SVFIX-NEVER pid=\d+ t_ns=\d+")) == 1, "never respawned"
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
    kill = "kill: SVFIXCH.ELF armed"
    ready = serial.index(marker)
    armed = serial.index(kill, ready)
    assert starts[i-1].start() < ready < armed < exits[i-1].start(), ("external kill", i)
assert serial.count("kill: SVFIXCH.ELF armed") == 6
assert serial.count("kill: SVFIXNV.ELF armed") == 1
for name, expected in [
    ("launch", "svfixture: ready mode=restart n=1"),
    *[("kill%d" % i, "svc: backoff name=SVFIX-RESTART k=%d " % i) for i in range(1, 6)],
    ("kill6", "svc: failed name=SVFIX-RESTART reason=restart-limit "),
    ("never", "svfixture: complete"),
]:
    assert expected in (run / ("client-%s.out" % name)).read_text(errors="replace"), \
        "external client failed: " + name
receipt5 = Path(os.environ["VG_SHARE"]) / "SVFIX5.TXT"
expected = ("app=SVFIX-RESTART\noutcome=restart=5/5 status=137 backoff_s=%d\nlast-log:\n" %
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
