# Profiler-only boots: 7 interleaved fixed-work off/on pairs, CNTV on both
# cores, and the guest's cmd/compile profiled by a monitor-launched PROF.
# Existing fork/selfhost inputs only; tools/go/build-prof.sh keeps .symtab.
# Compile the same pinned hello with SSA/GC invariant checks enabled, so this
# tiny source supplies enough actual compiler CPU work for 100 Hz sampling.
# GOCMDPROFILE is the named compiler variant, SSA built with -N -l only.
# Its two existing read-only SSA check sites run 256 checks each, explicitly
# amplifying this pinned tiny build; never presented as normal build timing.
# Owner must release M94b's window first. No competing VM/build/test work;
# hold /tmp/virelai-vz.lock and record uptime before/after (no load floor).
# exec-order: assert-proven -- all end markers are emitted by the guest tool.
vgate_name live-observe "M94c: 100 Hz IRQ sampler, fixed-work overhead <2%, symbolized Go compile"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 1 BOOTS

vgate_file overhead.txt <<'EOF'
set GOMAXPROCS=1
exec -c1 PROFFIX.ELF
EOF

vgate_file samples.txt <<'EOF'
set GOMAXPROCS=1
exec -c1 PROFFIX.ELF --samples
EOF

vgate_file compile.txt <<'EOF'
set GOMAXPROCS=1
exec PROF.ELF exec GOCMDPROFILE.ELF -d=ssa/check/on -o=/host/HELLO.o -importcfg=/host/GOIMPORT.CFG /host/HELLO.GO
EOF

vgate_setup_python <<'PY'
import os, shutil
share = os.path.join(os.environ["RUN_DIR"], "share")
for name in ("PROF", "PROFFIX", "GOCMDPROFILE"):
    source = ".build/go/" + name + ".ELF"
    assert os.path.isfile(source), source + " missing: run tools/go/build-prof.sh"
    shutil.copyfile(source, os.path.join(share, name + ".ELF"))
stage = ".build/go/selfhost"
assert os.path.isdir(stage), "run tools/go/stage-selfhost.sh"
for name in sorted(os.listdir(stage)):
    if not name.startswith("."):
        shutil.copyfile(os.path.join(stage, name), os.path.join(share, name))
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    f.write("#v2\nwm=none\n")
PY

vgate_run samples -- --script '$RUN_DIR/samples.txt' \
    --script-expect 'proffixture: done' --timeout 60
vgate_assert samples serial-absent 'proffixture: FAIL'
vgate_assert samples serial-absent '[EXC] parking:'
vgate_assert samples python <<'PY'
import os, re
serial = open(os.environ["VG_SER"], errors="replace").read()
row = re.search(r"prof: sample_probe samples=(\d+) dropped=(\d+) work_ns=(\d+)", serial)
assert row, "no sample-count probe"
samples, dropped, elapsed = map(int, row.groups())
assert 0 < elapsed < 900_000_000, "probe crossed a physical quantum"
assert dropped == 0
expected = elapsed / 1e9 * 100
assert samples >= max(10, expected * 0.75), (samples, expected)
print("OBSERVED sample-count probe: samples=%d dropped=%d work_ns=%d" % (samples, dropped, elapsed))
PY

vgate_run overhead -- --script '$RUN_DIR/overhead.txt' \
    --script-expect 'proffixture: done' --timeout 180
vgate_assert overhead serial-contains 'prof: samples='
vgate_assert overhead serial-absent 'proffixture: FAIL'
vgate_assert overhead serial-absent '[EXC] parking:'
vgate_assert overhead python <<'PY'
import os, re, statistics
serial = open(os.environ["VG_SER"], errors="replace").read()
runs = re.findall(r"prof: work mode=(off|on) run=(\d+) ns=(\d+) checksum=([0-9a-f]+)", serial)
assert len(runs) == 14, "need exactly 7 interleaved off/on pairs"
assert [(mode, int(n)) for mode, n, _, _ in runs] == [
    (mode, pair) for pair in range(1, 8) for mode in ("off", "on")
], "off/on runs are not paired and interleaved"
durations = {}
for mode in ("off", "on"):
    rows = [(int(n), int(ns)) for m, n, ns, _ in runs if m == mode]
    assert [n for n, _ in rows] == list(range(1, 8))
    durations[mode] = [ns for _, ns in rows]
    assert all(ns > 0 for ns in durations[mode]), "nonpositive timing"
assert len({checksum for _, _, _, checksum in runs}) == 1, "work differs"
off = statistics.median(durations["off"])
on = statistics.median(durations["on"])
overhead = (on - off) / off
off_min, off_max = min(durations["off"]), max(durations["off"])
on_min, on_max = min(durations["on"]), max(durations["on"])
paired = [(enabled - baseline) / baseline for baseline, enabled in
          zip(durations["off"], durations["on"])]
# Conservative observed range, not a statistical confidence interval.
# If it straddles the 2% bar, the set is unresolved even with a low median.
low = (on_min - off_max) / off_max
high = (on_max - off_min) / off_min
print("OBSERVED fixed-work ns:", durations, "medians:", off, on,
      "off_min/max:", off_min, off_max, "on_min/max:", on_min, on_max,
      "overhead_pct:", 100*overhead, "paired median/min/max pct:",
      100*statistics.median(paired), 100*min(paired), 100*max(paired),
      "range_envelope_pct:", 100*low, 100*high)
assert not low <= 0.02 <= high, "UNRESOLVED: observed spread cannot resolve the 2% budget"
assert overhead < 0.02 and high < 0.02, "profiler overhead >= 2%"
summary = re.search(r"prof: samples=(\d+) dropped=(\d+) off_median_ns=(\d+) on_median_ns=(\d+)", serial)
assert summary, "no guest summary"
samples, dropped, guest_off, guest_on = map(int, summary.groups())
assert (guest_off, guest_on) == (off, on), "median arithmetic differs"
spread = re.search(r"prof: spread off_min_ns=(\d+) off_max_ns=(\d+) on_min_ns=(\d+) on_max_ns=(\d+) pairs=(\d+)", serial)
assert spread and tuple(map(int, spread.groups())) == (off_min, off_max, on_min, on_max, 7), "guest spread arithmetic differs"
assert dropped == 0, "normal session lost records"
# The worker runs immediately after Sleep wakes and finishes within one
# physical quantum. At 100 Hz it has many samples; a 1 Hz mutation cannot
# satisfy this bound. Timer IRQ counts are checked separately below.
assert max(durations["on"]) < 900_000_000, "work crossed a physical quantum"
expected = sum(durations["on"]) / 1e9 * 100
assert samples >= max(10, expected * 0.75), (samples, expected)
rates = re.findall(r"prof: timer core=(\d+) irq=(\d+) poll=(\d+) elapsed_cntpct=(\d+) freq=(\d+) physical_ticks=(\d+)", serial)
for core in (0, 1):
    measured = [(int(irq), int(poll), int(elapsed), int(freq), int(ticks))
                for c, irq, poll, elapsed, freq, ticks in rates if int(c) == core]
    assert measured, "no counter-timed PPI 27 window on core %d" % core
    irq, poll, elapsed, freq, ticks = measured[-1]
    assert freq > 0 and elapsed >= freq * 10
    hz = irq * freq / elapsed
    print("OBSERVED CNTV PPI 27 core=%d irq=%d poll=%d elapsed=%d freq=%d hz=%.6f physical_ticks=%d"
          % (core, irq, poll, elapsed, freq, hz, ticks))
    assert poll == 0 and 950 <= irq <= 1050 and 95 <= hz <= 105
    assert 9 <= ticks <= 11, "physical preemption clock changed"
PY

vgate_run compile -- --script '$RUN_DIR/compile.txt' \
    --script-expect 'prof: done' --timeout 180
vgate_assert compile serial-contains 'prof: samples='
vgate_assert compile serial-absent 'prof: FAIL'
vgate_assert compile serial-absent '[EXC] parking:'
vgate_assert compile serial-absent 'exited status=139'
vgate_assert compile share-contains PROF/GOCMDPROFILE.folded 'cmd/compile/'
vgate_assert compile share-contains PROF/GOCMDPROFILE.top 'runtime.'
vgate_assert compile python <<'PY'
import os, re, shutil
serial = open(os.environ["VG_SER"], errors="replace").read()
summary = re.search(r"prof: samples=(\d+) symbolized_pct=([0-9.]+) dropped=(\d+)", serial)
assert summary, "no compile profile summary"
assert int(summary[1]) > 0 and float(summary[2]) >= 90 and int(summary[3]) == 0
top = re.findall(r"prof: top \d+ samples=\d+ (.+)", serial)
assert top and len(top) <= 10
assert any("cmd/compile/" in name for name in top), top
assert any("runtime." in name for name in top), top
print("OBSERVED Go-build top 10:", top)
share = os.environ["VG_SHARE"]
obj = os.path.join(share, "HELLO.o")
assert os.path.isfile(obj), "guest compile produced no object"
raw = open(obj, "rb").read()
assert raw.startswith(b"!<arch>") and b"go object virelai arm64 " in raw
# A dependency-free rendered view, not a downloaded flame renderer.
import html
rows = re.findall(r"prof: top (\d+) samples=(\d+) (.+)", serial)
page = "<!doctype html><meta charset=utf-8><title>GOCMDPROFILE CPU samples</title><h1>GOCMDPROFILE top samples (SSA -N -l)</h1>"
largest = max(int(count) for _, count, _ in rows)
for rank, count, name in rows:
    page += '<p>%s. %s (%s)<br><meter min="0" max="%d" value="%s"></meter></p>' % (
        rank, html.escape(name), count, largest, count)
out = os.path.join("artifacts", "live-observe-compile-top.html")
open(out, "w").write(page)
PY
