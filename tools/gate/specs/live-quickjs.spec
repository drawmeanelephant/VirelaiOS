# Standalone ADR 0039 QJS.BIN: file/tty eval, explicit refusals and budgets.
# All evals run on VZ. Node goldens are pinned inputs, never guest substitutes.
# Policy-only unreachable arithmetic limits are labeled separately.
# Cleanup requires procs receipt QJS.BIN and exact independent pages recovery.
# exec-order: assert-proven -- every snapshot follows guest reap or launcher
# exit; output, receipt and page assertions fail if any guest work is absent.

vgate_name live-quickjs "ADR 0039 file, interactive, refusal, bounds, cold/cleanup and desk-open journey acceptance"
vgate_share arm-virtiofs
vgate_runner_flags -Xswiftc -DSPIKE
vgate_fmt user/src/quickjs.zig tools/js-client/*.zig
vgate_note "QJS-FILE-EVAL QJS-INTERACTIVE-EVAL QJS-REFUSALS QJS-BOUNDS QJS-COLD-AND-CLEANUP QJS-STAGED QJS-VIRELAI-NS QJS-SHIPPED-WORKFLOW QJS-JOURNEY"

vgate_setup_python <<'PY'
import os, pathlib, sys
sys.dont_write_bytecode = True
sys.path.insert(0, str(pathlib.Path("tools/js-client").resolve()))
from prepare import prepare
prepare(pathlib.Path(os.environ["RUN_DIR"]))
PY

# M200a (issue #3352): the engine must also arrive through the session's own
# staging, not only the gate's prepare(). prepare() already dropped its copy
# — remove it so reappearance proves tools/session.sh put QJS.BIN on the
# share. VIRELAI_SESSION_SHARE rides the armed share, so the legs that
# follow read /host/QJS.BIN over the same virtio-fs a --save-capable
# session uses. SKIP_BUILD is the gate's reuse path: the product link runs
# first to .build/js-client/QJS.BIN, then the session stages it. The
# provisioned .virelairc is removed afterward — session.sh writes a seat
# line into it and the kernel monitor runs the rc on EVERY boot, which
# would change the serial-monitor shape of all 23 legs.
vgate_setup_python <<'PY'
import filecmp, os, pathlib, subprocess, sys
rd = os.environ["RUN_DIR"]
share = pathlib.Path(rd) / "share"
(share / "QJS.BIN").unlink(missing_ok=True)
# --work rides RUN_DIR, not the persistent .build/js-client/product: a
# Finder .DS_Store in either tree makes math_library's pinned file-set
# answer SourceDrift/EEXIST on a warm rerun (observed on this host).
subprocess.run([sys.executable, "tools/js-client/build.py", "build",
                "--work", os.path.join(rd, "build-product"),
                "--output", ".build/js-client/QJS.BIN"], check=True)
env = dict(os.environ)
env["VIRELAI_SESSION_PROFILE"] = os.path.join(rd, "session.virelaivm")
env["VIRELAI_SESSION_SHARE"] = str(share)
env["VIRELAI_SESSION_SKIP_BUILD"] = "1"
env["VIRELAI_SESSION_NO_GOTABWM"] = "1"
r = subprocess.run(["bash", "tools/session.sh", "--provision-only"],
                   capture_output=True, text=True, env=env)
sys.stdout.write(r.stdout)
if r.returncode != 0:
    sys.exit("session.sh --provision-only failed rc=%d: %s" % (r.returncode, r.stderr))
if "session: provisioning complete" not in r.stdout:
    sys.exit("no provisioning-complete receipt")
staged = share / "QJS.BIN"
assert staged.is_file(), "session.sh did not stage QJS.BIN"
assert filecmp.cmp(str(staged), ".build/js-client/QJS.BIN", shallow=False), \
    "staged QJS.BIN differs from the .build/js-client product"
(share / ".virelairc").unlink(missing_ok=True)
(share / "QJSESS.js").write_text("print('qjs-session-staged');\n")
(pathlib.Path(rd) / "staged.txt").write_text(
    "pages\nexec QJS.BIN /host/QJSESS.js --receipt=/host/QJSESS.R\n")
# M200d (issue #3441): the shipped script must also arrive through the
# session's own staging — image/scripts/report.js lands as
# SCRIPTS/REPORT.JS, beside the APPS.TXT input the workflow reads.
script = share / "SCRIPTS" / "REPORT.JS"
assert script.is_file(), "session.sh did not stage SCRIPTS/REPORT.JS"
assert filecmp.cmp(str(script), "image/scripts/report.js", shallow=False), \
    "staged SCRIPTS/REPORT.JS differs from image/scripts/report.js"
assert (share / "APPS.TXT").is_file(), "session.sh did not stage APPS.TXT"
(pathlib.Path(rd) / "workflow.txt").write_text(
    "pages\nexec QJS.BIN /host/SCRIPTS/REPORT.JS --receipt=/host/REPORT.R\n")
print("setup: session.sh staged QJS.BIN into the session share "
      "(byte-equal to the .build/js-client product)")
PY

# M200e (issue #3446): the acceptance journey fixture. The NO_GOTABWM
# provision above skips manifest apps, so the consumer itself —
# GOFILES.ELF — is staged through the same ensure-guest-elf builder
# session.sh would call (hash-cached). The .js a user wrote lives in its
# own directory so Enter's target is unambiguous; the readFile input
# sits at the share root, outside that listing. JOURNEY.JS reads JSEED,
# writes OUT.TXT through the virelai namespace, reads it back, then
# names the out-of-policy refusals — the whole arc in one script.
vgate_setup_python <<'PY'
import os, pathlib, shutil, subprocess, sys
rd = os.environ["RUN_DIR"]
share = pathlib.Path(rd) / "share"
subprocess.run(["bash", "tools/go/ensure-guest-elf.sh", "ensure", "GOFILES"],
               check=True)
shutil.copy2(".build/go/GOFILES.ELF", share / "GOFILES.ELF")
(share / "JSEED.TXT").write_text("jny-seed")
jdir = share / "JNEY"
jdir.mkdir(exist_ok=True)
(jdir / "JOURNEY.JS").write_text(
    "print('jny-args:' + virelai.args.join('|'));\n"
    "var s = virelai.readFile('/host/JSEED.TXT');\n"
    "print('jny-read:' + s);\n"
    "virelai.writeFile('/host/JNEY/OUT.TXT', s + '+js');\n"
    "print('jny-roundtrip:' + virelai.readFile('/host/JNEY/OUT.TXT'));\n"
    "for (const f of [\n"
    "  () => virelai.readFile('/dev/tty'),\n"
    "  () => virelai.readFile('/host/JNEY/GONE.TXT'),\n"
    "  () => virelai.writeFile('/etc/nope', 'x'),\n"
    "  () => virelai.writeFile('/host/JSEED.TXT', 'x'),\n"
    "]) { try { f(); print('jny-refusal-miss'); } catch (e) { print('jny-refusal:' + e.name); } }\n"
    "print('jny-done');\n")
(pathlib.Path(rd) / "journey-open.txt").write_text(
    "pages\nexec GOFILES.ELF /host/JNEY\n")
(pathlib.Path(rd) / "journey-close.txt").write_text("dui close 2\n")
(pathlib.Path(rd) / "journey-after.txt").write_text(
    "pages\nprocs receipt QJS.BIN\ntty\nsyscalls\necho jny-teardown-ok\n")
print("setup: staged GOFILES.ELF + JNEY/JOURNEY.JS journey fixture")
PY

vgate_file check.py <<'PY'
import pathlib, sys
sys.dont_write_bytecode = True
sys.path.insert(0, str(pathlib.Path("tools/js-client").resolve()))
from assertions import check
check()
PY

# ADR 0059 (M131c #2640): one dedicated base boot checkpoints the armed
# arm-virtiofs guest at the default boot-settle marker `tasks user-el0 reaped`,
# before any leg's workload (D4). The 20 script-only legs below restore it via
# --from-checkpoint qjs; their first --script-after is re-based onto the
# post-restore handshake (D5), while their later markers gate on the leg's own
# exec and re-occur inside the leg's epoch. The eight cold legs keep a
# `warm-cold:` comment naming their ADR D7 class.
vgate_checkpoint qjs --

vgate_run file --from-checkpoint qjs -- --script '$RUN_DIR/file.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run refused --from-checkpoint qjs -- --script '$RUN_DIR/refused.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run exact --from-checkpoint qjs -- --script '$RUN_DIR/exact.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run over --from-checkpoint qjs -- --script '$RUN_DIR/over.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run invalid --from-checkpoint qjs -- --script '$RUN_DIR/invalid.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run dense --from-checkpoint qjs -- --script '$RUN_DIR/dense.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run string --from-checkpoint qjs -- --script '$RUN_DIR/string.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run conversion --from-checkpoint qjs -- --script '$RUN_DIR/conversion.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run missing --from-checkpoint qjs -- --script '$RUN_DIR/missing.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run denied --from-checkpoint qjs -- --script '$RUN_DIR/denied.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run exists --from-checkpoint qjs -- --script '$RUN_DIR/exists.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run bounds --from-checkpoint qjs -- --script '$RUN_DIR/bounds.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
# warm-cold: cold (d) cold-start sampling — qjs-cold: sample= x5 + --script2-delay measure launch latency; a timing budget formed across the freeze is uninterpretable post-restore (ADR 0059 D7(d))
vgate_run cold -- --script '$RUN_DIR/cold.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'procs QLAUNCH.BIN exited status=0' --script2-delay 2 --script-expect 'qjs-gate-done' --timeout 90
# warm-cold: repl,session,evals,diagnostics,receipt,eof (c) --console-tcp bridges a live host socket into serial input — listener/bridge state lives outside the checkpoint bundle (ADR 0059 D7(c))
vgate_run repl -- --console-tcp '127.0.0.1:24786' --timeout 180
vgate_run line --from-checkpoint qjs -- --script '$RUN_DIR/line.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/line-input.txt' --script2-after 'qjs: ready eval=0' --script3 '$RUN_DIR/after.txt' --script3-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run line-over --from-checkpoint qjs -- --script '$RUN_DIR/line-over.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/line-over-input.txt' --script2-after 'qjs: ready eval=0' --script3 '$RUN_DIR/after.txt' --script3-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run session -- --console-tcp '127.0.0.1:24787' --timeout 180
vgate_run evals -- --console-tcp '127.0.0.1:24788' --timeout 600
vgate_run diagnostics -- --console-tcp '127.0.0.1:24789' --timeout 180
vgate_run receipt -- --console-tcp '127.0.0.1:24790' --timeout 600
vgate_run eof -- --console-tcp '127.0.0.1:24791' --timeout 90
vgate_run idle --from-checkpoint qjs -- --script '$RUN_DIR/idle.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 100
vgate_run usage --from-checkpoint qjs -- --script '$RUN_DIR/usage.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
# M200a: a warm boot on the session share — exec the binary session.sh
# staged on a script that lives beside it, output + receipt on /host.
vgate_run staged --from-checkpoint qjs -- --script '$RUN_DIR/staged.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
# M200c (issue #3434, ADR 0039 amendment): the virelai namespace on a real
# share — readFile/writeFile round-trip, args, and every named refusal.
vgate_run virelai --from-checkpoint qjs -- --script '$RUN_DIR/virelai.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
vgate_run virelai-refused --from-checkpoint qjs -- --script '$RUN_DIR/virelai-refused.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
# M200d (issue #3441): the shipped scripted workflow — the script session.sh
# staged under SCRIPTS/ runs end to end on the virelai namespace: readFile
# APPS.TXT, writeFile APPSUM.TXT, print the summary line.
vgate_run workflow --from-checkpoint qjs -- --script '$RUN_DIR/workflow.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'qjs-gate-done' --timeout 90
# M200e (issue #3446): the acceptance journey — ONE boot walks the whole
# consumer path: GOFILES' Script mime dispatch opens JNEY/JOURNEY.JS
# (the go-fileman run-23 shape), QJS.BIN execs it over the armed
# virtio-fs share, and the script itself round-trips /host/ through the
# virelai namespace and refuses outside it by name. --screen attaches
# the virtio-gpu scanout the window composites into.
# warm-cold: journey (D1) — --via-virtio arms a VZCustomVirtioDevice,
# unsaveable; statically cold, never attempted.
# Teardown ordering is load-bearing: check.py wants the last
# `tasks … reaped` BEFORE the last `pages:` and wants both pages samples
# equal — so GOFILES must be gone, not just QJS. `dui close 2` fires on
# QJS's reap (the first user-exec reap in this leg); the closing pages
# gate on `procs GOFILES.ELF exited` plus a 3 s settle, the same
# drain-the-reap-report margin the cold leg's --script2-delay uses —
# contains() cannot count a second `tasks user-exec reaped`, so the exit
# report + delay is the honest gate on the manager's reap landing first.
vgate_run journey -- --screen '$RUN_DIR/journey-screen' --input --via-virtio --script '$RUN_DIR/journey-open.txt' --script-after 'tasks user-el0 reaped' --input-chords 'return' --input-chords-after 'gofiles: ready' --script2 '$RUN_DIR/journey-close.txt' --script2-after 'tasks user-exec reaped' --script3 '$RUN_DIR/journey-after.txt' --script3-after 'procs GOFILES.ELF exited status=0' --script3-delay 3 --script-expect 'jny-teardown-ok' --timeout 240

vgate_assert file serial-contains 'procs QJS.BIN exited status=0'
vgate_assert refused serial-contains 'procs QJS.BIN exited status=0'
vgate_assert exact serial-contains 'procs QJS.BIN exited status=0'
vgate_assert over serial-contains 'qjs: InputLimit'
vgate_assert invalid serial-contains 'qjs: InvalidUtf8'
vgate_assert dense serial-contains 'qjs: OutOfMemory'
vgate_assert string serial-contains 'qjs: clean'
vgate_assert conversion serial-contains 'qjs: ContextReset'
vgate_assert missing serial-contains 'qjs: FileNotFound'
vgate_assert denied serial-contains 'qjs: AccessDenied'
vgate_assert exists serial-exact 'ReceiptExists' 1
vgate_assert bounds serial-contains 'qjs-bounds: done'
vgate_assert cold serial-count 'qjs-cold: sample=' 5
vgate_assert repl serial-contains 'qjs: ContextReset'
vgate_assert line serial-contains 'qjs: quit'
vgate_assert line-over serial-contains 'qjs: LineLimit'
vgate_assert session serial-contains 'qjs: SessionLimit'
vgate_assert evals serial-contains 'qjs: SessionLimit'
vgate_assert diagnostics serial-contains 'qjs: DiagnosticLimit'
vgate_assert receipt serial-contains 'qjs: ReceiptLimit'
vgate_assert eof serial-contains 'qjs: quit'
vgate_assert idle serial-contains 'qjs: IdleLimit'
vgate_assert usage serial-exact 'Usage' 1
vgate_assert staged serial-contains 'qjs-session-staged'
vgate_assert staged serial-contains 'procs QJS.BIN exited status=0'
vgate_assert virelai serial-contains 'ns-args:QJS.BIN|/host/VIRELAI.js'
vgate_assert virelai serial-contains 'ns-roundtrip:seed-42+wrote'
vgate_assert virelai serial-contains 'procs QJS.BIN exited status=0'
vgate_assert virelai share-equals NSOUT 'seed-42+wrote'
vgate_assert virelai-refused serial-contains 'ns-refusal:AccessDenied'
vgate_assert virelai-refused serial-contains 'ns-refusal:InvalidPath'
vgate_assert virelai-refused serial-contains 'ns-refusal:FileNotFound'
vgate_assert virelai-refused serial-contains 'ns-refusal:InputLimit'
vgate_assert virelai-refused serial-contains 'ns-refusal:PathLimit'
vgate_assert virelai-refused serial-contains 'ns-refusal:PathAlreadyExists'
vgate_assert virelai-refused serial-absent 'ns-refusal-miss'
vgate_assert virelai-refused serial-contains 'ns-refusals-done'
vgate_assert virelai-refused serial-contains 'procs QJS.BIN exited status=0'
vgate_assert workflow serial-contains 'report: apps='
vgate_assert workflow serial-contains 'wrote /host/APPSUM.TXT'
vgate_assert workflow serial-contains 'procs QJS.BIN exited status=0'
# The written file is compared against a report recomputed from the share's
# own APPS.TXT — the assertion follows the manifest, not a pinned golden.
# VG_SHARE, not RUN_DIR/share: a --from-checkpoint leg mounts the warm
# bundle's share clone (ADR 0059 D3), which is where the guest's write
# lands — warm or D8 cold-fallback alike.
vgate_assert workflow python <<'PY'
import os, pathlib
share = pathlib.Path(os.environ.get("VG_SHARE")
                     or os.path.join(os.environ["RUN_DIR"], "share"))
rows = docked = 0
names = []
for raw in (share / "APPS.TXT").read_text().split("\n"):
    line = raw.strip()
    if not line or line.startswith("#"):
        continue
    f = [s.strip() for s in line.split("|")]
    if len(f) < 4:
        continue
    rows += 1
    if f[3] == "dock=true":
        docked += 1
    names.append(f[1])
assert rows > 0, "APPS.TXT carried no app rows — the report is meaningless"
expected = ("APPS.TXT summary\napps: %d (%d docked)\n%s\n"
            % (rows, docked, "\n".join(names)))
got = (share / "APPSUM.TXT").read_text()
assert got == expected, "APPSUM.TXT mismatch:\n%r\nwant\n%r" % (got, expected)
PY

# M200e: the journey, in order — the dispatch decision pairs with the
# named console sink, the launch outcome names the handler + pid, the
# script's own markers prove the virelai namespace round-trip and the
# named refusals, and the file it wrote is byte-compared on the share.
vgate_assert journey serial-contains 'exec: loaded GOFILES.ELF'
vgate_assert journey serial-contains 'gofiles: list /host/JNEY n=1'
vgate_assert journey serial-contains 'gofiles: entry JOURNEY.JS file'
vgate_assert journey serial-contains 'gofiles: open file JOURNEY.JS type=script handler=QJS.BIN'
vgate_assert journey serial-contains 'gofiles: open console JOURNEY.JS'
vgate_assert journey serial-contains 'gofiles: open launched JOURNEY.JS handler=QJS.BIN pid='
vgate_assert journey serial-contains 'jny-args:QJS.BIN|/host/JNEY/JOURNEY.JS'
vgate_assert journey serial-contains 'jny-read:jny-seed'
vgate_assert journey serial-contains 'jny-roundtrip:jny-seed+js'
vgate_assert journey serial-contains 'jny-refusal:AccessDenied'
vgate_assert journey serial-contains 'jny-refusal:FileNotFound'
vgate_assert journey serial-contains 'jny-refusal:PathAlreadyExists'
vgate_assert journey serial-absent 'jny-refusal-miss'
vgate_assert journey serial-contains 'jny-done'
vgate_assert journey serial-contains 'qjs: clean'
vgate_assert journey serial-contains 'procs QJS.BIN exited status=0'
vgate_assert journey serial-contains 'dui close: closed=2'
vgate_assert journey serial-contains 'procs GOFILES.ELF exited status=0'
vgate_assert journey serial-contains 'jny-teardown-ok'
vgate_assert journey serial-absent 'gofiles: open refused JOURNEY.JS'
vgate_assert journey serial-absent 'gofiles: open launch refused'
vgate_assert journey serial-absent 'exited status=139'
vgate_assert journey share-equals JNEY/OUT.TXT 'jny-seed+js'
vgate_assert journey python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY

vgate_assert file python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert refused python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert exact python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert over python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert invalid python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert dense python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert string python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert conversion python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert missing python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert denied python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert exists python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert bounds python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert cold python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert repl python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert line python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert line-over python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert session python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert evals python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert diagnostics python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert receipt python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert eof python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert idle python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert usage python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert staged python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert virelai python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
vgate_assert virelai-refused python <<'PY'
import os, runpy
runpy.run_path(os.path.join(os.environ["RUN_DIR"], "check.py"))
PY
