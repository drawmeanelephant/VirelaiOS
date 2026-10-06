# Declarative cold boot and the direct-seat escape path. The negative boots
# must relinquish init's tasks before fallback; an exec pid is not a live seat.
# exec-order: assert-proven -- observation scripts run only after the hosted shell prompt.
vgate_name live-boot-order "M92e: init ordering, readiness, named refusal and single-seat fallback"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_setup_python <<'PY'
import os, pathlib, shutil, subprocess
share = pathlib.Path(os.environ.get("VG_SHARE", str(pathlib.Path(os.environ["RUN_DIR"]) / "share")))
for script in ("build-init.sh", "build-gotabwm.sh", "build-gosh.sh"):
    subprocess.run(["bash", "tools/go/" + script], check=True)
for name in ("INIT", "GOTABWM", "GOSH"):
    shutil.copyfile(".build/go/" + name + ".ELF", share / (name + ".ELF"))
subprocess.run(["python3", "tests/fixtures/init/boot/native.py", str(share)], check=True)
(share / "INIT").mkdir(exist_ok=True)
shutil.copyfile("tests/fixtures/init/boot/cold.json", share / "INIT/SERVICES.JSON")
assert not (share / "SETTINGS.TXT").exists()
assert not (share / "SESSION.TABS").exists()
PY
vgate_file observe.txt <<'EOF'
procs
tasks
wm
echo m92e-boot-observed
EOF
vgate_run cold -- --screen '$RUN_DIR/screen-cold' --script '$RUN_DIR/observe.txt' --script-after 'gosh: prompt' --script-expect 'm92e-boot-observed' --script-expect-tail 4 --timeout 90
vgate_assert cold serial-contains 'init: manifest ok n=3'
vgate_assert cold serial-contains 'init: seated'
vgate_assert cold serial-contains 'gotabwm: registered'
vgate_assert cold serial-contains 'gosh: prompt'
vgate_assert cold serial-absent 'wm: autostart gotabwm'
vgate_assert cold serial-absent '[EXC] parking:'
vgate_assert cold python <<'PY'
import json, os, pathlib, re
serial = pathlib.Path(os.environ["VG_SER"]).read_text(errors="replace")
manifest = json.loads(pathlib.Path(os.environ["VG_SHARE"], "INIT/SERVICES.JSON").read_text())
nodes = {s["name"]: s for s in manifest["services"] if s.get("enabled", True)}
order, pending = [], set(nodes)
while pending:
    available = sorted(n for n in pending if all(p in order for p in nodes[n].get("requires", [])))
    assert available, "required cycle in test manifest"
    order.append(available[0])
    pending.remove(available[0])
starts = re.findall(r"init: start name=(\S+) order=(\d+)", serial)
assert starts == [(n, str(i+1)) for i, n in enumerate(order)], (starts, order)
own = {"z-pre": "initpre: ready", "a-dep": "initdep: ready", "seat": "gotabwm: registered"}
for name in order:
    start = serial.index("init: start name=" + name + " ")
    ready = serial.index("init: ready name=" + name)
    assert start < ready
    for prerequisite in nodes[name].get("requires", []):
        assert serial.index("init: ready name=" + prerequisite) < start
        assert serial.index(own[prerequisite]) < serial.index(own[name])
assert serial.index("gotabwm: registered") < serial.index("init: seated")
assert serial.count("gotabwm: registered\n") == 1
assert serial.count("gotabwm: first-boot workspace") == 1
assert serial.count("gosh: prompt") == 1
assert re.search(r"tasks: [^\n]*pool=16/16 zombies=0", serial), "cold-boot task budget not observed"
for binary in ("INIT.ELF", "INITPRE.BIN", "INITDEP.BIN", "GOTABWM.ELF", "GOSH.ELF"):
    assert re.search(r"procs: [^\n]*name=" + re.escape(binary) + r" [^\n]*state=running", serial), binary
print("svcgraph lexical order, subsequent-poll readiness, one seat and one shell observed")
PY
vgate_assert cold python <<'PY'
import os, pathlib
share = pathlib.Path(os.environ["VG_SHARE"])
(share / "SETTINGS.TXT").write_text("#v2\ninit=off\n")
for name in ("SESSION.TABS", "WINDOWS.SAV"):
    (share / name).unlink(missing_ok=True)
PY
vgate_run off -- --screen '$RUN_DIR/screen-off' --script '$RUN_DIR/observe.txt' --script-after 'gosh: prompt' --script-expect 'm92e-boot-observed' --script-expect-tail 2 --timeout 90
vgate_assert off serial-contains 'wm: autostart gotabwm (settings wm=gotabwm)'
vgate_assert off serial-contains 'gosh: prompt'
vgate_assert off serial-absent 'init:'
vgate_assert off python <<'PY'
import os, pathlib
share = pathlib.Path(os.environ["VG_SHARE"])
for name in ("SETTINGS.TXT", "SESSION.TABS", "WINDOWS.SAV", "INIT/SERVICES.JSON"):
    (share / name).unlink(missing_ok=True)
PY
vgate_run missing -- --screen '$RUN_DIR/screen-missing' --script '$RUN_DIR/observe.txt' --script-after 'gosh: prompt' --script-expect 'm92e-boot-observed' --script-expect-tail 2 --timeout 90
vgate_assert missing serial-contains 'init: refuse missing-manifest'
vgate_assert missing serial-contains 'init: fallback direct (init exited)'
vgate_assert missing serial-contains 'wm: autostart gotabwm (settings wm=gotabwm)'
vgate_assert missing serial-contains 'gosh: prompt'
vgate_assert missing python <<'PY'
import json, os, pathlib
share = pathlib.Path(os.environ["VG_SHARE"])
for name in ("SESSION.TABS", "WINDOWS.SAV"):
    (share / name).unlink(missing_ok=True)
m = json.loads(pathlib.Path("tests/fixtures/init/boot/cold.json").read_text())
m["services"][2]["requires"] = ["seat"]
(share / "INIT/SERVICES.JSON").write_text(json.dumps(m))
PY
vgate_run cycle -- --screen '$RUN_DIR/screen-cycle' --script '$RUN_DIR/observe.txt' --script-after 'gosh: prompt' --script-expect 'm92e-boot-observed' --script-expect-tail 2 --timeout 90
vgate_assert cycle serial-contains 'init: refuse cycle: a-dep -> z-pre -> seat -> a-dep'
vgate_assert cycle serial-contains 'init: fallback direct (init exited)'
vgate_assert cycle serial-contains 'wm: autostart gotabwm (settings wm=gotabwm)'
vgate_assert cycle serial-contains 'gosh: prompt'
vgate_assert cycle serial-absent 'init: start'
vgate_assert cycle serial-absent '[EXC] parking:'
vgate_assert cycle python <<'PY'
import os, pathlib
share = pathlib.Path(os.environ["VG_SHARE"])
for name in ("SESSION.TABS", "WINDOWS.SAV", "INIT.ELF"):
    (share / name).unlink(missing_ok=True)
PY
vgate_run noinit -- --screen '$RUN_DIR/screen-noinit' --script '$RUN_DIR/observe.txt' --script-after 'gosh: prompt' --script-expect 'm92e-boot-observed' --script-expect-tail 2 --timeout 90
vgate_assert noinit serial-contains 'init: refuse missing-init'
vgate_assert noinit serial-contains 'wm: autostart gotabwm (settings wm=gotabwm)'
vgate_assert noinit serial-contains 'gosh: prompt'
vgate_assert noinit python <<'PY'
import os, pathlib, shutil
share = pathlib.Path(os.environ["VG_SHARE"])
for name in ("SESSION.TABS", "WINDOWS.SAV"):
    (share / name).unlink(missing_ok=True)
shutil.copyfile(".build/go/INIT.ELF", share / "INIT.ELF")
(share / "INIT/SERVICES.JSON").write_text('{"version":1,"services":null}')
PY
vgate_run invalid -- --screen '$RUN_DIR/screen-invalid' --script '$RUN_DIR/observe.txt' --script-after 'gosh: prompt' --script-expect 'm92e-boot-observed' --script-expect-tail 2 --timeout 90
vgate_assert invalid serial-contains 'init: refuse null-field'
vgate_assert invalid serial-contains 'init: fallback direct (init exited)'
vgate_assert invalid serial-contains 'gosh: prompt'
vgate_assert invalid serial-absent 'init: start'
vgate_assert invalid python <<'PY'
import os, pathlib
share = pathlib.Path(os.environ["VG_SHARE"])
for name in ("SESSION.TABS", "WINDOWS.SAV"):
    (share / name).unlink(missing_ok=True)
(share / "INIT/SERVICES.JSON").write_bytes(b" " * 8193)
PY
vgate_run oversize -- --screen '$RUN_DIR/screen-oversize' --script '$RUN_DIR/observe.txt' --script-after 'gosh: prompt' --script-expect 'm92e-boot-observed' --script-expect-tail 2 --timeout 90
vgate_assert oversize serial-contains 'init: refuse oversize'
vgate_assert oversize serial-contains 'init: fallback direct (init exited)'
vgate_assert oversize serial-contains 'gosh: prompt'
vgate_assert oversize python <<'PY'
import json, os, pathlib
share = pathlib.Path(os.environ["VG_SHARE"])
for name in ("SESSION.TABS", "WINDOWS.SAV"):
    (share / name).unlink(missing_ok=True)
m = json.loads(pathlib.Path("tests/fixtures/init/boot/cold.json").read_text())
m["services"][1]["argv"] = ["GOSH.ELF"]
(share / "INIT/SERVICES.JSON").write_text(json.dumps(m))
PY
vgate_run budget -- --screen '$RUN_DIR/screen-budget' --script '$RUN_DIR/observe.txt' --script-after 'gosh: prompt' --script-expect 'm92e-boot-observed' --script-expect-tail 2 --timeout 90
vgate_assert budget serial-contains 'init: refuse task-budget'
vgate_assert budget serial-contains 'init: fallback direct (init exited)'
vgate_assert budget serial-contains 'gosh: prompt'
vgate_assert budget serial-absent 'init: start'
vgate_assert budget python <<'PY'
import os, pathlib, shutil
share = pathlib.Path(os.environ["VG_SHARE"])
for name in ("SESSION.TABS", "WINDOWS.SAV", "INITDEP.BIN"):
    (share / name).unlink(missing_ok=True)
shutil.copyfile("tests/fixtures/init/boot/cold.json", share / "INIT/SERVICES.JSON")
PY
vgate_run spawn -- --screen '$RUN_DIR/screen-spawn' --script '$RUN_DIR/observe.txt' --script-after 'gosh: prompt' --script-expect 'm92e-boot-observed' --script-expect-tail 2 --timeout 90
vgate_assert spawn serial-contains 'init: refuse unstartable'
vgate_assert spawn serial-contains 'init: fallback direct (init exited)'
vgate_assert spawn serial-contains 'gosh: prompt'
vgate_assert spawn serial-absent 'init: start name=seat'
vgate_assert spawn python <<'PY'
import os, pathlib, re, subprocess
serial = pathlib.Path(os.environ["VG_SER"]).read_text(errors="replace")
assert re.search(r"procs: [^\n]*name=INIT.ELF [^\n]*state=exited", serial), "init still owns tasks"
assert not re.search(r"procs: [^\n]*name=INITPRE.BIN [^\n]*state=running", serial), "partial-start child survived fallback"
share = pathlib.Path(os.environ["VG_SHARE"])
for name in ("SESSION.TABS", "WINDOWS.SAV"):
    (share / name).unlink(missing_ok=True)
subprocess.run(["python3", "tests/fixtures/init/boot/native.py", str(share)], check=True)
PY
vgate_file kill-init.txt <<'EOF'
kill INIT.ELF
EOF
vgate_run died -- --screen '$RUN_DIR/screen-died' --script '$RUN_DIR/kill-init.txt' --script-after 'init: start name=z-pre ' --script2 '$RUN_DIR/observe.txt' --script2-after 'gosh: prompt' --script-expect 'm92e-boot-observed' --script-expect-tail 2 --timeout 90
vgate_assert died serial-contains 'kill: INIT.ELF armed'
vgate_assert died serial-contains 'init: fallback direct (init exited)'
vgate_assert died serial-contains 'wm: autostart gotabwm (settings wm=gotabwm)'
vgate_assert died serial-contains 'gosh: prompt'
vgate_assert died serial-absent 'init: seated'
vgate_assert died python <<'PY'
import os, pathlib, re
serial = pathlib.Path(os.environ["VG_SER"]).read_text(errors="replace")
assert re.search(r"procs: [^\n]*name=INIT.ELF [^\n]*state=exited", serial), "init still owns tasks"
assert not re.search(r"procs: [^\n]*name=INIT(?:PRE|DEP).BIN [^\n]*state=running", serial), "orphaned init child"
assert serial.count("gotabwm: registered\n") == 1, "fallback double-launched seat"
assert serial.count("gotabwm: first-boot workspace") == 1, "fallback double-launched shell"
PY
