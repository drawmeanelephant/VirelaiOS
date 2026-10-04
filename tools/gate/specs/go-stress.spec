# go-stress.spec -- M70a2 (#1469, parent #1453): seeded randomized
# GOOS=virelai runtime stress on the compiled default GOMAXPROCS (no env
# pin — numCPUStartup = 2, ADR 0027 D6). Supersedes the #1227 fixed
# four-phase shape.
#
# The fixture (tools/go/gostress.go) runs 4 fixed seeds × 8 iterations;
# each iteration draws shape (gc / chan / timer / futex / mem / churn)
# and parameters from an in-tree splitmix64 seeded by the seed — no
# wall-clock, no host entropy (M70a D1); bounded guest time (D2). Run 02
# replays the first roster seed via argv. pins.txt is the pinned corpus:
# the exact deterministic line sequence per seed. A generator, PRNG or
# roster change regenerates it deliberately in the same PR; a crash on
# any seed fails the run.
#
# Matching is ordered-substring with an advancing cursor, NOT whole-line:
# the runtime writes a line's text and newline as separate writes, and
# the shell idle-loop reporters (smp/timer/userspace) interleave between
# them, so kernel fragments can sit inside a serial line — but never
# inside the program's write itself.
#
# Kernel-effect ties (python over the `syscalls` report): sys_mmap grew
# (the mem phase extends the sbrk break through slot 63); its ceiling is
# the kernel's loud-refusal budget (max_mmap_regions=16) — observed 8 on
# the landed roster, with the region count depending on allocator
# chunking the fixture does not control, so the ceiling enforces the
# budget rather than an allocator invariant. sys_futex ≥ floor (slot-74
# contention); sys_thread ≥ 2 (multi-M runtime, M65c).
#
# HOST PREREQUISITE (not hermetic — see tools/go/README.md):
# `just go-toolchain` must have produced .build/go/GOSTRESS.ELF.
#
# exec-order: assert-proven -- runs 01/02 are a single exec whose script2
# waits on the kernel's final process-exit report to read its retained
# peak page/region receipt; run 03 (bad argv) ends on
# the program's own FAIL marker. All asserts read the program's lines, so
# a green run always proves it ran.

vgate_name go-stress "M70a2 #1469: seeded GOOS=virelai runtime stress (gc/chan/timer/futex/mem/churn) on VZ"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script.txt <<'EOF'
pages
exec GOSTRESS.ELF
EOF

vgate_file script-seed.txt <<'EOF'
pages
exec GOSTRESS.ELF 0x9e3779b97f4a7c15
EOF

vgate_file script-bad.txt <<'EOF'
exec GOSTRESS.ELF bogus
EOF

vgate_file script2.txt <<'EOF'
pages
procs receipt GOSTRESS.ELF
syscalls
echo gostress-held-window
EOF

# Pinned corpus, 4 seeds × 17 lines (8 iteration markers + 8 phase-ok
# detail lines + 1 seed-ok line), in exact program order. The `procs=`
# line and the closing `done` marker are deliberately NOT in the pin —
# they are asserted separately below. Regenerate with the fixture, never
# by loosening an assert.
vgate_file pins.txt <<'EOF'
go-stress seed=0x9e3779b97f4a7c15 iter=1 phase=timer
go-stress seed=0x9e3779b97f4a7c15 iter=1 phase=timer n=6 maxms=14 ok
go-stress seed=0x9e3779b97f4a7c15 iter=2 phase=futex
go-stress seed=0x9e3779b97f4a7c15 iter=2 phase=futex g=17 iters=34 counter=578 ok
go-stress seed=0x9e3779b97f4a7c15 iter=3 phase=chan
go-stress seed=0x9e3779b97f4a7c15 iter=3 phase=chan fan workers=8 jobs=38 ok
go-stress seed=0x9e3779b97f4a7c15 iter=4 phase=churn
go-stress seed=0x9e3779b97f4a7c15 iter=4 phase=churn g=51 depth=37 ok
go-stress seed=0x9e3779b97f4a7c15 iter=5 phase=mem
go-stress seed=0x9e3779b97f4a7c15 iter=5 phase=mem steps=2 top=512KiB ok
go-stress seed=0x9e3779b97f4a7c15 iter=6 phase=gc
go-stress seed=0x9e3779b97f4a7c15 iter=6 phase=gc allocs=49 live=4 cycles=1 ok
go-stress seed=0x9e3779b97f4a7c15 iter=7 phase=gc
go-stress seed=0x9e3779b97f4a7c15 iter=7 phase=gc allocs=56 live=4 cycles=1 ok
go-stress seed=0x9e3779b97f4a7c15 iter=8 phase=chan
go-stress seed=0x9e3779b97f4a7c15 iter=8 phase=chan chain stages=4 vals=5 ok
go-stress seed=0x9e3779b97f4a7c15 ok
go-stress seed=0xa0761d6478bd642f iter=1 phase=mem
go-stress seed=0xa0761d6478bd642f iter=1 phase=mem steps=3 top=1152KiB ok
go-stress seed=0xa0761d6478bd642f iter=2 phase=gc
go-stress seed=0xa0761d6478bd642f iter=2 phase=gc allocs=58 live=4 cycles=2 ok
go-stress seed=0xa0761d6478bd642f iter=3 phase=churn
go-stress seed=0xa0761d6478bd642f iter=3 phase=churn g=33 depth=19 ok
go-stress seed=0xa0761d6478bd642f iter=4 phase=chan
go-stress seed=0xa0761d6478bd642f iter=4 phase=chan fan workers=8 jobs=29 ok
go-stress seed=0xa0761d6478bd642f iter=5 phase=timer
go-stress seed=0xa0761d6478bd642f iter=5 phase=timer n=5 maxms=14 ok
go-stress seed=0xa0761d6478bd642f iter=6 phase=futex
go-stress seed=0xa0761d6478bd642f iter=6 phase=futex g=24 iters=15 counter=360 ok
go-stress seed=0xa0761d6478bd642f iter=7 phase=futex
go-stress seed=0xa0761d6478bd642f iter=7 phase=futex g=24 iters=40 counter=960 parkwake=1 ok
go-stress seed=0xa0761d6478bd642f iter=8 phase=chan
go-stress seed=0xa0761d6478bd642f iter=8 phase=chan fan workers=6 jobs=18 ok
go-stress seed=0xa0761d6478bd642f ok
go-stress seed=0xe7037ed1a0b428db iter=1 phase=mem
go-stress seed=0xe7037ed1a0b428db iter=1 phase=mem steps=3 top=1152KiB ok
go-stress seed=0xe7037ed1a0b428db iter=2 phase=futex
go-stress seed=0xe7037ed1a0b428db iter=2 phase=futex g=6 iters=31 counter=186 parkwake=1 ok
go-stress seed=0xe7037ed1a0b428db iter=3 phase=chan
go-stress seed=0xe7037ed1a0b428db iter=3 phase=chan fan workers=5 jobs=38 ok
go-stress seed=0xe7037ed1a0b428db iter=4 phase=gc
go-stress seed=0xe7037ed1a0b428db iter=4 phase=gc allocs=48 live=4 cycles=2 ok
go-stress seed=0xe7037ed1a0b428db iter=5 phase=churn
go-stress seed=0xe7037ed1a0b428db iter=5 phase=churn g=43 depth=16 ok
go-stress seed=0xe7037ed1a0b428db iter=6 phase=timer
go-stress seed=0xe7037ed1a0b428db iter=6 phase=timer n=3 maxms=8 ok
go-stress seed=0xe7037ed1a0b428db iter=7 phase=gc
go-stress seed=0xe7037ed1a0b428db iter=7 phase=gc allocs=52 live=3 cycles=2 ok
go-stress seed=0xe7037ed1a0b428db iter=8 phase=mem
go-stress seed=0xe7037ed1a0b428db iter=8 phase=mem steps=2 top=512KiB ok
go-stress seed=0xe7037ed1a0b428db ok
go-stress seed=0xc0ffee iter=1 phase=churn
go-stress seed=0xc0ffee iter=1 phase=churn g=48 depth=7 ok
go-stress seed=0xc0ffee iter=2 phase=gc
go-stress seed=0xc0ffee iter=2 phase=gc allocs=41 live=3 cycles=1 ok
go-stress seed=0xc0ffee iter=3 phase=chan
go-stress seed=0xc0ffee iter=3 phase=chan chain stages=3 vals=5 ok
go-stress seed=0xc0ffee iter=4 phase=futex
go-stress seed=0xc0ffee iter=4 phase=futex g=23 iters=37 counter=851 ok
go-stress seed=0xc0ffee iter=5 phase=timer
go-stress seed=0xc0ffee iter=5 phase=timer n=3 maxms=14 ok
go-stress seed=0xc0ffee iter=6 phase=mem
go-stress seed=0xc0ffee iter=6 phase=mem steps=2 top=768KiB ok
go-stress seed=0xc0ffee iter=7 phase=mem
go-stress seed=0xc0ffee iter=7 phase=mem steps=2 top=1024KiB ok
go-stress seed=0xc0ffee iter=8 phase=futex
go-stress seed=0xc0ffee iter=8 phase=futex g=21 iters=23 counter=483 parkwake=1 ok
go-stress seed=0xc0ffee ok
EOF

vgate_setup_python <<'PY'
import os, shutil, struct, sys
share = os.path.join(os.environ["RUN_DIR"], "share")
src = os.path.join(".build", "go", "GOSTRESS.ELF")
if not os.path.exists(src):
    sys.exit("GOSTRESS.ELF missing (expected " + src + ") - "
             "build the fork binaries first: just go-toolchain "
             "(fork prerequisites in tools/go/README.md)")
shutil.copy(src, os.path.join(share, "GOSTRESS.ELF"))
elf = open(src, "rb").read()
phoff = struct.unpack_from("<Q", elf, 32)[0]
phsize, phnum = struct.unpack_from("<HH", elf, 54)
pages = 0
for i in range(phnum):
    kind, flags, off, va, pa, filesz, memsz, align = struct.unpack_from(
        "<IIQQQQQQ", elf, phoff + i * phsize)
    if kind == 1:
        pages += (memsz + 4095) // 4096
with open(os.path.join(os.environ["RUN_DIR"], "static-pages.txt"), "w") as f:
    f.write(str(pages))
print("staged GOSTRESS.ELF into share (%d bytes)" %
      os.path.getsize(os.path.join(share, "GOSTRESS.ELF")))
PY

# Process exit can precede the last task's reap. Give the idle reaper a turn,
# then require reaped=1 AND exact restoration, never treating exit as reap.
vgate_run 01 -- --script '$RUN_DIR/script.txt' --script2 '$RUN_DIR/script2.txt' --script2-after 'procs GOSTRESS.ELF exited status=0' --script2-delay 1 --script-expect 'gostress-held-window' --timeout 120

vgate_assert 01 serial-contains 'exec: loaded GOSTRESS.ELF'
vgate_assert 01 serial-contains 'go-stress procs=2'
vgate_assert 01 serial-contains 'go-stress done'
vgate_assert 01 serial-absent 'go-stress FAIL'
vgate_assert 01 serial-absent 'fatal error:'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'exited status=139'

vgate_assert 01 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
pins = [l for l in open(os.path.join(os.environ["RUN_DIR"], "pins.txt"))
        .read().splitlines() if l]
pos = 0
for i, want in enumerate(pins):
    idx = ser.find(want, pos)
    if idx < 0:
        sys.exit("FAIL: pin %d/%d missing or out of order:\n  %s"
                 % (i + 1, len(pins), want))
    pos = idx + len(want)
print("go-stress run 01: pinned corpus matched in order (%d lines)" % len(pins))
PY

vgate_file check-receipt.py <<'PY'
import os, re, sys
lines = open(os.environ["VG_SER"], errors="replace").read().splitlines()
def calls(slot):
    for l in lines:
        m = re.search(r"%d (sys_\w+) calls=(\d+)" % slot, l)
        if m:
            return int(m.group(2))
    return None
n_thread = calls(73)
if n_thread is None:
    sys.exit("FAIL: no sys_thread row in the syscalls report")
if n_thread < 2:
    sys.exit("FAIL: sys_thread calls >= 2 required, got %d" % n_thread)
n_futex = calls(74)
if n_futex is None or n_futex < 50:
    sys.exit("FAIL: sys_futex contention expected (>= 50 calls), got %s" % n_futex)
n_mmap = calls(63)
if n_mmap is None:
    sys.exit("FAIL: no sys_mmap row in the syscalls report")
if n_mmap < 2:
    sys.exit("FAIL: sys_mmap growth expected (>= 2 calls), got %s" % n_mmap)
if n_mmap > 16:
    sys.exit("FAIL: sys_mmap calls %d exceeds the per-process "
             "max_mmap_regions budget (16)" % n_mmap)
print("go-stress python asserts OK: sys_thread=%d sys_futex=%d sys_mmap=%d"
      % (n_thread, n_futex, n_mmap))
receipts = re.findall(
    r"runtime-receipt: pid=(\d+) name=GOSTRESS\.ELF peak_pages=(\d+) "
    r"page_cap=(\d+) peak_regions=(\d+) region_cap=(\d+) static_pages=(\d+) "
    r"page_tracking=extensible page_saturated=(\d+) total_pages=(\d+) "
    r"record_failures=(\d+) unrecorded_pages=(\d+) reaped=(\d+)",
    "\n".join(lines))
if len(receipts) != 1:
    sys.exit("FAIL: expected one final GOSTRESS.ELF runtime receipt")
pid, pages, page_cap, regions, region_cap, static_pages, saturated, total, failures, unrecorded, reaped = map(int, receipts[0])
if not (0 < pages <= total and page_cap == 4096):
    sys.exit("FAIL: peak demand pages %d / total %d / inline cap %d" % (pages, total, page_cap))
# #1958: three patched full-roster boots peaked at 1534/1618/1638 pages;
# replay at 1295/1308/1327, versus 15370/14784 before the padding fix.
# Keep scheduling headroom while refusing a restored alignment-gap sweep.
# This fixture regression bound is not a kernel cap or an engine budget.
if pages > 2048:
    sys.exit("FAIL: GOSTRESS peak demand pages %d exceeds fresh-padding "
             "regression bound 2048 (total allocations %d)" % (pages, total))
if saturated != int(pages >= page_cap) or failures or unrecorded or reaped != 1:
    sys.exit("FAIL: saturation=%d failures=%d unrecorded=%d reaped=%d"
             % (saturated, failures, unrecorded, reaped))
if not (0 < regions <= 16 and region_cap == 16 and regions <= n_mmap):
    sys.exit("FAIL: peak regions %d / cap %d inconsistent with sys_mmap=%d"
             % (regions, region_cap, n_mmap))
expected_static = int(open(os.path.join(os.environ["RUN_DIR"], "static-pages.txt")).read())
if static_pages != expected_static:
    sys.exit("FAIL: static segment pages %d != ELF page-rounded PT_LOAD %d"
             % (static_pages, expected_static))
serial = "\n".join(lines)
if serial.index("procs GOSTRESS.ELF exited status=0") >= serial.index("runtime-receipt:"):
    sys.exit("FAIL: receipt did not follow the final process exit")
counts = [int(n, 16) for n in re.findall(
    r"pages: armed=1 total=0x[0-9a-f]+ free=0x([0-9a-f]+)", serial)]
if len(counts) != 2 or counts[0] != counts[1]:
    sys.exit("FAIL: free pages before exec/after reap must match: %s" % counts)
if serial.index("pages: armed=1") >= serial.index("exec: loaded GOSTRESS.ELF"):
    sys.exit("FAIL: initial page sample did not precede exec")
if serial.rindex("pages: armed=1") <= serial.index("procs GOSTRESS.ELF exited status=0"):
    sys.exit("FAIL: final page sample did not follow final process exit")
print("go-stress free pages restored: %d -> %d" % tuple(counts))
print("go-stress runtime receipt OK: pid=%d peak_pages=%d (inline 4096) "
      "peak_regions=%d/16 static_pages=%d total_pages=%d"
      % (pid, pages, regions, static_pages, total))
PY

vgate_assert 01 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check-receipt.py"))
PY

vgate_run 02 -- --script '$RUN_DIR/script-seed.txt' --script2 '$RUN_DIR/script2.txt' --script2-after 'procs GOSTRESS.ELF exited status=0' --script2-delay 1 --script-expect 'gostress-held-window' --timeout 120

vgate_assert 02 serial-contains 'exec: loaded GOSTRESS.ELF'
vgate_assert 02 serial-contains 'go-stress procs=2'
vgate_assert 02 serial-contains 'go-stress done'
vgate_assert 02 serial-absent 'go-stress FAIL'
vgate_assert 02 serial-absent 'fatal error:'
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 serial-absent 'exited status=139'

# Same-seed-twice determinism, second boot: the argv replay of seed
# 0x9e3779b97f4a7c15 must reproduce run 01's first 17 pinned lines in
# order (the seed's full sequence).
vgate_assert 02 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
pins = [l for l in open(os.path.join(os.environ["RUN_DIR"], "pins.txt"))
        .read().splitlines() if l]
pos = 0
for i, want in enumerate(pins[:17]):
    idx = ser.find(want, pos)
    if idx < 0:
        sys.exit("FAIL: replay pin %d/17 missing or out of order:\n  %s"
                 % (i + 1, want))
    pos = idx + len(want)
print("go-stress run 02: argv replay reproduced the pinned seed sequence")
PY

vgate_assert 02 python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check-receipt.py"))
PY

# Bad-argv path (card-explicit contract): an unparseable seed prints the
# FAIL line and withholds the `done` marker, so a failed seeded run can
# never be mistaken for a clean one — the run's red comes from the
# missing anchor, and here the FAIL line itself ends the boot. The
# fixture's exit status stays 0; the gate reads the lines, not the
# status. No syscalls report needed for this run.
vgate_run 03 -- --script '$RUN_DIR/script-bad.txt' --script-expect 'go-stress FAIL argv=bogus' --timeout 120

vgate_assert 03 serial-contains 'exec: loaded GOSTRESS.ELF'
vgate_assert 03 serial-contains 'go-stress FAIL argv=bogus'
vgate_assert 03 serial-absent 'go-stress done'
vgate_assert 03 serial-absent 'fatal error:'
vgate_assert 03 serial-absent '[EXC] parking:'
vgate_assert 03 serial-absent 'exited status=139'
