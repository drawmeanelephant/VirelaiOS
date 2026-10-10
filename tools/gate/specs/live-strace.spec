# live-strace.spec -- legacy monitor tracing and ADR 0043 ring observation.
# STRACE's importable arm-before-exec path traces a real GOSH file/pipe script.
# TRACEFIX proves exact wrap, redaction, pid exclusion and foreign-uid refusal.
# Overhead is an explicit opt-in on a quiet host, never a fabricated number.

vgate_name live-strace "M22 D5: per-syscall tracing"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 1 BOOTS

vgate_file script.txt <<'EOF'
strace exec HELLO.ELF
echo strace-mid
EOF

vgate_file script2.txt <<'EOF'
crash
sym
strace off
echo rx-strace-ok
EOF

vgate_run 01 -- --script '$RUN_DIR/script.txt' --script-after 'tasks user-el0 exited status=7' --script2 '$RUN_DIR/script2.txt' --script2-after 'tasks user-exec exited status=42' --script-expect 'rx-strace-ok' --timeout 90

vgate_assert 01 serial-exact 'VirelaiOS kernel has seized control.' 1
vgate_assert 01 serial-exact 'strace: armed' 1
vgate_assert 01 serial-count '] sys_write(' 1
vgate_assert 01 serial-count '] sys_exit(' 1
vgate_assert 01 serial-exact 'elf: hello from HELLO.ELF' 1
vgate_assert 01 serial-exact 'tasks user-exec exited status=42' 1
vgate_assert 01 serial-exact 'strace: off' 1
vgate_assert 01 serial-exact 'rx-strace-ok' 1
vgate_assert 01 serial-absent '[EXC] parking:'

vgate_setup_python <<'PY'
import os, shutil
rd = os.environ["RUN_DIR"]
share = os.path.join(rd, "share")
for name in ("STRACE", "TRACEFIX", "GOSH"):
    src = os.path.join(".build", "go", name + ".ELF")
    if not os.path.exists(src):
        raise SystemExit(src + " missing; provision the Go fork and build-strace/build-gosh")
    shutil.copy(src, os.path.join(share, name + ".ELF"))
with open(os.path.join(share, "TRACE.IN"), "wb") as f:
    f.write(b"trace-input\n")
# Isolate headless fixture boots from the default Go seat without changing
# the boot default. This settings file belongs only to this temporary share.
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    f.write("#v2\nwm=none\n")
with open(os.path.join(rd, "overhead.txt"), "w") as f:
    if os.environ.get("TRACE_MEASURE") == "1":
        f.write("exec TRACEFIX.ELF overhead\n")
    else:
        f.write("echo trace-overhead-not-requested\necho trace-overhead-done\n")
PY

vgate_file gosh.txt <<'EOF'
exec STRACE.ELF -e 23,24,25,26,56,57,3 exec GOSH.ELF -c 'cat /host/TRACE.IN; echo trace-output > /host/TRACE.OUT; echo alpha-beta | grep alpha'
EOF

vgate_file self.txt <<'EOF'
exec TRACEFIX.ELF self
EOF

vgate_file deny.txt <<'EOF'
set GOMAXPROCS=1
exec -u0 GOSH.ELF -c "sleep 30"
EOF

vgate_file deny2.txt <<'EOF'
exec TRACEFIX.ELF deny
EOF

vgate_run 02 -- --script '$RUN_DIR/gosh.txt' --script-expect 'strace: done' --timeout 120
vgate_assert 02 serial-contains 'sys_file_open(path="/host/TRACE.IN", len=14, flags=READ) = 0'
vgate_assert 02 serial-contains 'sys_file_read(fd='
vgate_assert 02 serial-contains 'sys_file_write(fd='
vgate_assert 02 serial-contains 'alpha-beta'
vgate_assert 02 serial-contains 'strace: done dropped=0'
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 share-equals TRACE.OUT $'trace-output\n'
vgate_assert 02 python <<'PY'
import os, re
serial = open(os.environ["VG_SER"], errors="replace").read()
lines = re.findall(r"\[strace (\d+)\] (sys_[^(]+)\(([^\n]*)", serial)
if not lines:
    raise SystemExit("no decoded records")
allowed = {"sys_file_open", "sys_file_read", "sys_file_write", "sys_file_close",
           "sys_pipe_read", "sys_pipe_write", "sys_exit"}
if any(name not in allowed for _, name, _ in lines):
    raise SystemExit("slot filter leaked an excluded slot")
if len({pid for pid, _, _ in lines}) != 1:
    raise SystemExit("pid filter leaked another process")
opens = [(pid, args) for pid, name, args in lines if name == "sys_file_open"
         and 'path="/host/TRACE.IN", len=14, flags=READ)' in args]
if len(opens) != 1:
    raise SystemExit("expected exactly one TRACE.IN open")
m = re.fullmatch(r'path="/host/TRACE.IN", len=14, flags=READ\) = (0)', opens[0][1])
if not m:
    raise SystemExit("TRACE.IN open is not a decoded successful fd")
fd = m.group(1)
if not any(name == "sys_file_read" and re.fullmatch(
        r"fd=" + fd + r", buf=0x[0-9a-f]+, len=4096\) = 12", args)
        for _, name, args in lines):
    raise SystemExit("expected exact 12-byte read from the opened fd")
if not any(name == "sys_file_write" and re.fullmatch(
        r"fd=0, buf=0x[0-9a-f]+, len=13\) = 13", args)
        for _, name, args in lines):
    raise SystemExit("expected exact 13-byte TRACE.OUT write")
for name in ("sys_pipe_read", "sys_pipe_write"):
    if not any(record_name == name and args.endswith(") = 11")
               for _, record_name, args in lines):
        raise SystemExit("missing exact 11-byte pipe " + name)
print("decoded GOSH input open + 12-byte read, output write, pid/slot exclusion: PASS")
PY

vgate_run 03 -- --script '$RUN_DIR/self.txt' --script-expect 'trace: peer excluded' --timeout 120
vgate_assert 03 serial-contains 'trace: wrap records=256 dropped=44'
vgate_assert 03 serial-contains 'sys_secret_get(<redacted>)'
vgate_assert 03 serial-contains 'sys_tty_net_auth(<redacted>)'
vgate_assert 03 serial-contains 'trace: decoded/redacted done dropped=0'
vgate_assert 03 serial-contains 'trace: untraced peer done'
vgate_assert 03 serial-contains 'trace: peer excluded records=1 dropped=0'
vgate_assert 03 serial-absent 'trace: fixture failed'
vgate_assert 03 serial-absent '[EXC] parking:'
vgate_assert 03 python <<'PY'
import os, re
serial = open(os.environ["VG_SER"], errors="replace").read()
records = re.findall(r"\[strace (\d+)\] (sys_[^(]+)\(([^\n]*)", serial)
if len(records) != 260:
    raise SystemExit("expected exactly 256 wrap + 3 decoded/redacted + 1 pid-filter records")
if len({pid for pid, _, _ in records}) != 1:
    raise SystemExit("untraced process produced a ring record")
if sum(name == "sys_ping_poll" for _, name, _ in records) != 257:
    raise SystemExit("wrong wrapped/pid-filter record count")
for _, name, args in records:
    if name in {"sys_secret_get", "sys_tty_net_auth"} and args != "<redacted>)":
        raise SystemExit("redacted slot exposed args or result")
print("exact wrap, uid-independent pid exclusion and whole-record redaction: PASS")
PY

vgate_run 04 -- --script '$RUN_DIR/deny.txt' --script2 '$RUN_DIR/deny2.txt' --script2-after 'exec: loaded GOSH.ELF' --script-expect 'trace: cross-uid ARM' --timeout 120
vgate_assert 04 serial-contains 'trace: cross-uid ARM = -EACCES'
vgate_assert 04 serial-absent 'trace: fixture failed'
vgate_assert 04 serial-absent '[EXC] parking:'

# M97g #2086: the session binds to the arming PID, not the uid. Two uid_user
# processes: while TRACEFIX `hold` keeps its session live, TRACEFIX `grab`'s
# ARM must refuse EACCES — the legacy same-uid takeover is gone. (The token
# itself is a CSPRNG mint, so `grab` cannot guess it either.)
vgate_file hold.txt <<'EOF'
exec TRACEFIX.ELF hold
EOF

vgate_file grab.txt <<'EOF'
exec TRACEFIX.ELF grab
EOF

vgate_run 07 -- --script '$RUN_DIR/hold.txt' --script2 '$RUN_DIR/grab.txt' --script2-after 'trace: holding session' --script-expect 'trace: same-uid arm refused' --timeout 120
vgate_assert 07 serial-contains 'trace: holding session'
vgate_assert 07 serial-exact 'trace: same-uid arm refused' 1
vgate_assert 07 serial-absent 'trace: fixture failed'
vgate_assert 07 serial-absent '[EXC] parking:'

vgate_run 05 -- --script '$RUN_DIR/overhead.txt' --script-after 'tasks user-el0 exited status=7' --script-expect 'trace-overhead-done' --timeout 120
vgate_assert 05 python <<'PY'
import os, re
serial = open(os.environ["VG_SER"], errors="replace").read()
if os.environ.get("TRACE_MEASURE") != "1":
    if "trace-overhead-not-requested" not in serial:
        raise SystemExit("explicit overhead opt-out marker missing")
    print("Overhead NOT MEASURED: rerun TRACE_MEASURE=1 on a quiet host")
else:
    m = re.search(r"trace: overhead calls=10000 pairs=7 untraced=(\d+) filtered=(\d+) traced_untraced=(\d+) traced=(\d+) ns/call freq=(\d+)", serial)
    if not m or any(int(v) <= 0 for v in m.groups()):
        raise SystemExit("missing/non-positive counter-timed overhead medians/frequency")
    if not re.search(r"trace: filtered-overhead calls=100000 pairs=7 untraced=\d+ filtered=\d+ ns/call", serial):
        raise SystemExit("100,000-call filtered measurement missing")
    for path, calls in (("filtered", 10000), ("filtered100k", 100000), ("traced", 10000)):
        pairs = re.findall(r"trace: pair path=" + path + r" index=(\d+) calls=" + str(calls) + r" off_ns=(\d+) on_ns=(\d+)", serial)
        if [int(p[0]) for p in pairs] != list(range(1, 8)) or any(int(v) <= 0 for p in pairs for v in p[1:]):
            raise SystemExit("missing/non-positive interleaved pairs for " + path)
    print("Counter-timed overhead medians present and sane; no threshold asserted")
PY

vgate_setup_python <<'PY'
import os
rd = os.environ["RUN_DIR"]
with open(os.path.join(rd, "capture-overhead.txt"), "w") as f:
    if os.environ.get("TRACE_CAPTURE_MEASURE") == "1":
        f.write("exec TRACEFIX.ELF capture-overhead\n")
    else:
        f.write("echo trace-capture-not-requested\necho trace-capture-overhead-done\n")
PY

vgate_run 06 -- --script '$RUN_DIR/capture-overhead.txt' --script-after 'tasks user-el0 exited status=7' --script-expect 'trace-capture-overhead-done' --timeout 120
vgate_assert 06 serial-absent 'trace: fixture failed'
vgate_assert 06 serial-absent '[EXC] parking:'
vgate_assert 06 python <<'PY'
import json, math, os, re, shutil, struct
from pathlib import Path
serial = Path(os.environ["VG_SER"]).read_text(errors="replace")
if os.environ.get("TRACE_CAPTURE_MEASURE") != "1":
    assert "trace-capture-not-requested" in serial, "explicit capture-measure opt-out missing"
    print("128-byte capture mean/p95 NOT MEASURED in this invocation")
else:
    summary = re.search(r"trace: capture-overhead pairs=7 calls=10000 bytes=128 freq=(\d+) off_ticks=(\d+) on_ticks=(\d+) added_p95_ticks=(-?\d+)", serial)
    assert summary, "per-call capture summary missing"
    frequency, reported_off, reported_on, reported_added_p95 = map(int, summary.groups())
    assert frequency > 0
    pairs = re.findall(r"trace: capture-pair index=(\d+) calls=10000 off_ticks=(\d+) on_ticks=(\d+) off_p95_ticks=(\d+) on_p95_ticks=(\d+) added_p95_ticks=(-?\d+)", serial)
    assert [int(p[0]) for p in pairs] == list(range(1, 8)), "seven off/on capture pairs missing"
    raw = Path(os.environ["VG_SHARE"], "TRACE-CAPTURE.TICKS")
    data = raw.read_bytes()
    assert len(data) == 7 * 2 * 10000 * 8, "raw counter sample count mismatch"
    values = struct.unpack("<140000Q", data)
    assert min(values) > 0, "zero counter sample"
    def p95(samples):
        return sorted(samples)[math.ceil(len(samples)*.95)-1]
    records, all_off, all_on, all_added = [], [], [], []
    for i, row in enumerate(pairs):
        off = values[2*i*10000:(2*i+1)*10000]
        on = values[(2*i+1)*10000:(2*i+2)*10000]
        added = [b-a for a, b in zip(off, on)]
        assert tuple(map(int, row[1:])) == (sum(off), sum(on), p95(off), p95(on), p95(added)), "serial/raw counter mismatch"
        records.append({"pair": i+1, "off_mean_ns": sum(off)*1e9/frequency/10000,
                        "on_mean_ns": sum(on)*1e9/frequency/10000,
                        "added_mean_ns": (sum(on)-sum(off))*1e9/frequency/10000,
                        "off_p95_ns": p95(off)*1e9/frequency,
                        "on_p95_ns": p95(on)*1e9/frequency,
                        "paired_added_p95_ns": p95(added)*1e9/frequency})
        all_off.extend(off); all_on.extend(on); all_added.extend(added)
    assert (sum(all_off), sum(all_on), p95(all_added)) == (reported_off, reported_on, reported_added_p95)
    result = {"frequency": frequency, "pairs": records,
              "added_mean_ns": (sum(all_on)-sum(all_off))*1e9/frequency/70000,
              "on_p95_ns": p95(all_on)*1e9/frequency,
              "paired_added_p95_ns": p95(all_added)*1e9/frequency}
    suffix = os.environ.get("VIRELAI_GATE_SUFFIX", "")
    shutil.copyfile(raw, "artifacts/live-strace-capture-ticks.bin"+suffix)
    Path("artifacts/live-strace-capture-results.json"+suffix).write_text(json.dumps(result, indent=2)+"\n")
    print(json.dumps(result, indent=2))
    # Baseline durations are nonnegative, so absolute traced p95 is a
    # conservative upper bound on added p95, not a difference of quantiles.
    assert result["added_mean_ns"] <= 10000, "128-byte capture added mean exceeds 10 us"
    assert result["on_p95_ns"] <= 25000, "conservative 128-byte added p95 bound exceeds 25 us"
    print("128-byte capture added mean and conservative p95 bound: PASS")
PY
