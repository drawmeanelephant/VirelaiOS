# live-ipc-wake.spec -- issue #3901: IPC delivery wakes a
# sys_wait_event-blocked consumer. IPCWAKE.BIN arms a 12-tick app timer
# (the only OTHER event a headless process can receive), then blocks in
# sys_wait_event; COUNTER.BIN bursts sends into its mailbox. Without the
# kind-23 IPC_MAIL push the only possible wake is the TIMER event ~4 s
# in -- the leg reads `ipcwake: tick-waited` and fails. With it, the
# first delivery wakes the waiter on the next scheduler pass:
# `ipcwake: mail` + `ipcwake: ok` land while the timer is still armed --
# the armed deadline is the sub-tick bound, no wall-clock needed.
# --script2-after holds the mbox snapshot until the first drain, so the
# row shows a live recv count.

vgate_name live-ipc-wake "mailbox delivery wakes a wait_event-blocked consumer sub-tick"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 3 BOOTS

vgate_file script1.txt <<'EOF'
exec IPCWAKE.BIN
exec COUNTER.BIN 1
echo ipcwake-phase1
EOF

vgate_file script2.txt <<'EOF'
mbox
echo ipcwake-run-done
EOF

vgate_run 01 -- --cpus 1 --script '$RUN_DIR/script1.txt' --script2 '$RUN_DIR/script2.txt' --script2-after 'ipcwake: ok' --script-expect 'ipcwake-run-done' --script-expect-tail 20 --timeout 60

vgate_assert 01 serial-exact 'VirelaiOS kernel has seized control.' 1
vgate_assert 01 serial-exact 'ipcwake: armed' 1
vgate_assert 01 serial-exact 'ipcwake: ok' 1
vgate_assert 01 serial-absent 'ipcwake: tick-waited'
vgate_assert 01 serial-absent 'ipcwake: waitfail'
vgate_assert 01 serial-absent 'ipcwake: no-mail'
vgate_assert 01 python <<'PY'
import os, re, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
lines = ser.splitlines()
def first(sub):
    return next((i for i, l in enumerate(lines) if sub in l), None)
armed, ping, mail, ok = (first("ipcwake: armed"), first("ipc: ping "),
                         first("ipcwake: mail "), first("ipcwake: ok"))
if armed is None or ping is None or mail is None or ok is None:
    sys.exit("FAIL: missing markers armed=%s ping=%s mail=%s ok=%s" % (armed, ping, mail, ok))
if not (armed < ping < mail < ok):
    sys.exit("FAIL: order armed=%s ping=%s mail=%s ok=%s" % (armed, ping, mail, ok))
# The mail echo carries COUNTER's bytes verbatim ("ping <d>").
m = re.search(r"ipcwake: mail <(\d+)> (ping \d+)", lines[mail])
if m is None:
    sys.exit("FAIL: mail echo shape: %r" % lines[mail])
seq = int(m.group(2).split()[1])
if not any(("ipc: ping %d" % seq) in l for l in lines):
    sys.exit("FAIL: echoed seq=%d not among sends" % seq)
# The mailbox row proves the drain (sent-recv == pending, recv >= 1 —
# script2's mbox snapshot waits on the first `ipcwake: ok`).
rows = [l for l in lines if re.search(r"mbox: id=1 name=IPCWAKE", l)]
if not rows:
    sys.exit("FAIL: no IPCWAKE mbox row")
row = rows[-1]
ps = int(re.search(r"sent=([0-9]+)", row).group(1))
pr = int(re.search(r"recv=([0-9]+)", row).group(1))
pp = int(re.search(r"pending=([0-9]+)", row).group(1))
if not (pr >= 1 and ps - pr == pp):
    sys.exit("FAIL: mbox invariant off: %r" % row)
print("ipc wake ok: seq=%d mbox=%r" % (seq, row.strip()))
PY
