# Checked GOSH monitor escape, plus M87c quiet periodic reports while serial
# is owned. Run 02 leaves a partial command unsubmitted for >10 guest seconds
# (two 5-second heartbeat periods), measured by a guest CNTPCT receipt probe.
# The command then writes diagnostic-looking bytes unchanged to the share;
# monitor handback restores version and a newly generated heartbeat.
# Prerequisite: the GOOS=virelai fork and .build/go/GOSH.ELF.

vgate_name live-sh-monitor "#1128 SD1 + #1859 M87c: checked monitor escape and owned-serial routine-report sinks"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script.txt <<'EOF'
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
print("staged GOSH.ELF (%d bytes) into share" % os.path.getsize(src))
PY

vgate_setup_python <<'PY'
import os
run = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(run, "share")
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    # M66b (#1444): the `#v<N>` schema header is REQUIRED — a headerless file
    # is refused WHOLE and the compiled defaults (shell=monitor) stay in
    # force. This spec seeded a bare `shell=sh\n` and had been silently red
    # since M66b (class-B gates are not enforced in CI); M68b repaired it.
    f.write("#v2\nwm=none\nshell=sh\n")
with open(os.path.join(run, "edit.bin"), "wb") as f:
    f.write(b"monitor\r")
with open(os.path.join(run, "edit2.bin"), "wb") as f:
    f.write(b"version\r")
PY

vgate_run 01 -- --screen '$RUN_DIR/screen' --script '$RUN_DIR/script.txt' --script2 '$RUN_DIR/edit.bin' --script2-after 'gosh: attached' --script3 '$RUN_DIR/edit2.bin' --script3-after 'gosh: monitor' --script-expect 'virelai-kernel' --timeout 120

vgate_assert 01 serial-contains 'VirelaiOS kernel has seized control.'
# The seed was ACCEPTED: a refused SETTINGS.TXT would keep shell=monitor and
# the login handoff could not run at all.
vgate_assert 01 serial-absent 'SETTINGS.TXT refused'
vgate_assert 01 serial-contains 'login: shell=sh -> GOSH.ELF serial'
vgate_assert 01 serial-contains 'gosh: ready'
vgate_assert 01 serial-contains 'gosh: attached'
vgate_assert 01 serial-contains 'gosh: monitor'
vgate_assert 01 serial-contains 'virelai-kernel'
# The detach is checked: the failure marker would mean the handover did not
# happen and `version` met a still-attached shell.
vgate_assert 01 serial-absent 'gosh: monitor failed'
vgate_assert 01 serial-absent '[EXC]'
vgate_assert 01 serial-absent '[EXC] parking:'

# The `version` type is sequenced on `gosh: monitor`, which GOSH prints only
# AFTER the checked detach returned: waiting on it waits for a COMPLETED
# handover. The python assert pins the other half — `version` must never reach
# GOSH, so the race names itself instead of only failing on a missing string.
vgate_assert 01 python <<'PY'
import os
ser = open(os.environ["VG_SER"], errors="replace").read()
i_ready = ser.find("gosh: ready")
i_mon = ser.find("gosh: monitor")
i_ver = ser.find("virelai-kernel")
assert i_ready >= 0 and i_mon >= 0 and i_ver >= 0, "missing marker(s)"
assert i_ready < i_mon < i_ver, f"wrong order ready={i_ready} monitor={i_mon} version={i_ver}"
assert "login: shell=sh -> GOSH.ELF serial" in ser, "the login handoff did not run"
# The race the positive assert alone would only report as a missing string:
# after the handover GOSH must stop running lines, so `version` was read by
# the kernel monitor and not typed into a shell that still held the console.
assert "gosh: line version" not in ser, "version went to GOSH, not the monitor"
assert "gosh: monitor failed" not in ser, "the detach did not complete"
print("monitor escape ordering OK: gosh: ready < gosh: monitor < virelai-kernel")
PY

# The probe has no tty and emits only explicit gate receipts. It waits using
# the guest counter, not a host settle delay, while GOSH keeps editing.
vgate_file quiet.go <<'EOF'
package main

import "virelai/vi"

func receipt(tag string, ns int64) {
	vi.ConsoleLine("quiet: " + tag + " ns=" + vi.Itoa64(ns))
}

func main() {
	// Let GOSH finish painting the rest of the input burst before timing
	// the quiet interval. The serial assertion pins that order.
	warmup := vi.Nanos()
	for vi.Nanos()-warmup < 2_000_000_000 {
		vi.Sleep(1)
	}
	start := vi.Nanos()
	receipt("begin", start)
	for vi.Nanos()-start < 11_000_000_000 {
		vi.Sleep(1)
	}
	end := vi.Nanos()
	receipt("end", end)
	// This file is made only when the previously partial line is submitted.
	deadline := end + 30_000_000_000
	for {
		if b, rc := vi.ReadFileAll("/host/QUIET.TXT", 256); rc >= 0 && len(b) > 0 {
			break
		}
		if vi.Nanos() >= deadline {
			vi.ConsoleLine("quiet: receipt timeout")
			vi.Exit(1)
		}
		vi.Sleep(1)
	}
	submitted := vi.Nanos()
	receipt("submitted", submitted)
	// Leave room for a fresh normal heartbeat AFTER monitor handback. The
	// assertion below requires it, not just the passage of this interval.
	for vi.Nanos()-submitted < 7_000_000_000 {
		vi.Sleep(1)
	}
	receipt("finish", vi.Nanos())
	vi.Exit(0)
}
EOF

vgate_file quiet.expected <<'EOF'
timer heartbeat ticks=literal
tasks worker advances=literal
userspace: literal
fx: site=shot literal
EOF

vgate_setup_python <<'PY'
import os, subprocess, sys
run = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(run, "share")
repo = os.getcwd()
fork = os.environ.get("GO_FORK_DIR") or os.path.join(os.path.dirname(repo), "go-virelai")
go = os.path.join(fork, "bin", "go")
if not os.path.isfile(go):
    sys.exit("quiet probe requires the provisioned GOOS=virelai fork: " + go)
gopath = os.path.join(run, "gopath")
os.makedirs(os.path.join(gopath, "src"))
os.symlink(os.path.join(repo, "user", "go"), os.path.join(gopath, "src", "virelai"))
env = dict(os.environ, GOROOT=fork, GOPATH=gopath, GO111MODULE="off",
           GOTOOLCHAIN="local", GOFLAGS="", CGO_ENABLED="0", GOOS="virelai", GOARCH="arm64")
subprocess.run([go, "build", "-ldflags", "-s -w", "-o", os.path.join(share, "QUIET.ELF"),
                os.path.join(run, "quiet.go")], env=env, check=True)
command = (b"printf 'timer heartbeat ticks=literal\\ntasks worker advances=literal\\n"
           b"userspace: literal\\nfx: site=shot literal\\n' > /host/QUIET.TXT")
with open(os.path.join(run, "partial.bin"), "wb") as f:
    f.write(b"QUIET.ELF &\r" + command)  # deliberately no Enter on the last line
with open(os.path.join(run, "submit.bin"), "wb") as f:
    f.write(b"\rmonitor\r")
with open(os.path.join(run, "handback.bin"), "wb") as f:
    f.write(b"version\rtimer\r")
PY

vgate_run 02 -- --screen '$RUN_DIR/quiet-screen' --script '$RUN_DIR/partial.bin' --script-after 'gosh: prompt' --script2 '$RUN_DIR/submit.bin' --script2-after 'quiet: end ns=' --script3 '$RUN_DIR/handback.bin' --script3-after 'gosh: monitor' --script-expect 'quiet: finish ns=' --timeout 120

vgate_assert 02 serial-absent 'SETTINGS.TXT refused'
vgate_assert 02 serial-contains 'login: shell=sh -> GOSH.ELF serial'
vgate_assert 02 serial-contains 'gosh: attached'
vgate_assert 02 serial-contains 'gosh: monitor'
vgate_assert 02 serial-contains 'virelai-kernel'
vgate_assert 02 serial-contains 'timer: armed=1'
vgate_assert 02 serial-absent 'gosh: monitor failed'
vgate_assert 02 serial-absent 'quiet: receipt timeout'
vgate_assert 02 serial-absent '[EXC]'
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 share-equals QUIET.TXT quiet.expected

vgate_assert 02 python <<'PY'
import os, re
ser = open(os.environ["VG_SER"], "rb").read()
receipts = {}
for tag in (b"begin", b"end", b"submitted", b"finish"):
    m = re.search(rb"quiet: " + tag + rb" ns=(\d+)", ser)
    assert m, "missing guest time receipt: " + tag.decode()
    receipts[tag] = (m.start(), int(m[1]))
begin, start_ns = receipts[b"begin"]
end, end_ns = receipts[b"end"]
assert end_ns - start_ns >= 11_000_000_000, "less than two real guest heartbeat periods"
attach = ser.index(b"gosh: attached")
submit = ser.index(b"gosh: line printf ")
monitor = ser.index(b"gosh: monitor")
version = ser.index(b"virelai-kernel")
finish, finish_ns = receipts[b"finish"]
assert attach < begin < end < submit < monitor < version < finish, "ownership/submit/handback order"
# Anchor the quiet window AFTER the complete partial line was painted.
partial = ser.find(b"> /host/QUIET.TXT", attach, begin)
assert partial >= 0, "partial command was not present across the measured interval"
owned = ser[attach:submit]
for prefix in (b"timer heartbeat ticks=", b"timer irq delivered ", b"tasks worker advances=",
               b"smp: secondary runs=", b"smp: steal runs=", b"userspace: el0=",
               b"fx: site=shot"):
    # User input contains identical diagnostic strings by design. Check
    # complete report shapes, not a client-side deletion/filter of bytes.
    if prefix == b"timer heartbeat ticks=":
        assert not re.search(rb"timer heartbeat ticks=\d+", owned), "owned heartbeat leaked"
    elif prefix == b"tasks worker advances=":
        assert not re.search(rb"tasks worker advances=\d+", owned), "owned worker report leaked"
    elif prefix == b"fx: site=shot":
        assert b"fx: site=shot core=" not in owned, "owned periodic sample leaked"
    else:
        assert prefix not in owned, "owned routine report leaked: " + prefix.decode()
assert not re.search(rb"tasks [^\r\n]+ sleeping \d+ ticks", owned), "owned sleep report leaked"
assert b"gosh: line version" not in ser, "version reached GOSH"
assert b"gosh: line timer" not in ser, "explicit diagnostic reached GOSH"
# The explicit timer snapshot is taken after handback. A heartbeat whose
# tick number is greater cannot be a report queued during ownership.
snapshot = re.search(rb"timer: armed=1 [^\r\n]+ ticks=(\d+) irq=(\d+) poll=(\d+)", ser[version:finish])
assert snapshot, "missing monitor timer snapshot after version"
beats = list(re.finditer(rb"timer heartbeat ticks=(\d+) irq=(\d+) poll=(\d+)",
                        ser[version + snapshot.end():finish]))
assert beats, "no fresh heartbeat after monitor handback"
assert any(int(m[1]) > int(snapshot[1]) and int(m[2]) > int(snapshot[2]) and int(m[3]) == 0
           for m in beats), "no newly generated IRQ heartbeat after the handback snapshot"
assert finish_ns - receipts[b"submitted"][1] >= 7_000_000_000, "handback receipt interval too short"
print("owned partial command: guest interval %.3fs; share bytes exact; checked handback + IRQ heartbeat" %
      ((end_ns - start_ns) / 1e9))
PY
