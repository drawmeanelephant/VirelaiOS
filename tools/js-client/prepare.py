"""Fixture/build setup for the declarative live-quickjs gate.

QJS_COMPILER_CACHE optionally selects an isolated, already-provisioned SDK
cache. The builder still verifies every compiler/stdlib byte and extra file.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
FIXTURES = ROOT / "tests/fixtures/js"


def prepare(rd):
    share = rd / "share"
    evidence = ROOT / "artifacts/m88-acceptance"
    evidence.mkdir(parents=True, exist_ok=True)
    cache = os.environ.get("QJS_COMPILER_CACHE")
    cache_args = ["--cache", str(Path(cache).resolve())] if cache else []
    for command, name in (("build", "QJS.BIN"), ("launcher", "QLAUNCH.BIN"), ("bounds", "QBOUNDS.BIN")):
        subprocess.run([sys.executable, "-B", str(HERE / "build.py"), command,
                        "--work", str(rd / ("build-" + command)), "--output", str(share / name),
                        *cache_args], check=True)
        shutil.copyfile(share / (name + ".json"), evidence / (name + ".json"))
    lock = json.loads((FIXTURES / "goldens.json").read_text())
    for filename, digest in lock["sha256"].items():
        if hashlib.sha256((FIXTURES / filename).read_bytes()).hexdigest() != digest:
            raise ValueError("FixtureDrift: " + filename)
    # Deliberately cross the native 2048-byte read boundary without committing padding.
    (share / "FILE.js").write_bytes(b"/*" + b" " * 2500 + b"*/\n" + (FIXTURES / "file.js").read_bytes())
    shutil.copyfile(FIXTURES / "refused.js", share / "REFUSED.js")
    (share / "COLD.js").write_text("print(1+2*3);\n")
    (share / "EXACT.js").write_bytes(b"print(7);" + b" " * (1_048_576 - 9))
    (share / "OVER.js").write_bytes(b"print('PREFIX-MUST-NOT-RUN');" + b" " * 1_048_576)
    (share / "INVALID.js").write_bytes(b"print('PREFIX-MUST-NOT-RUN');\xff")
    (share / "DENSE.js").write_bytes(b"let a=[" + b"0," * 524_283 + b"0];")
    (share / "STRING.js").write_text("print('x'.repeat(32768).split('').length);\n")
    (share / "CONVERT.js").write_text("print({toString(){for(;;){}}});\n")
    (share / "EXISTS").write_bytes(b"previous receipt is never replaced\n")
    # No-follow access refusal is independent of guest/host uid or chmod interpretation.
    (share / "DENIED.js").symlink_to("FILE.js")
    (rd / "line-input.txt").write_bytes(b" " * 4093 + b"7*6\n:quit\n")
    (rd / "line-over-input.txt").write_bytes(b" " * 4097 + b"\n6*7\n:quit\n")
    commands = {
        "file": "/host/FILE.js --receipt=/host/FILE.R",
        "refused": "/host/REFUSED.js --receipt=/host/REFUSED.R",
        "exact": "/host/EXACT.js --receipt=/host/EXACT.R",
        "over": "/host/OVER.js --receipt=/host/OVER.R",
        "invalid": "/host/INVALID.js --receipt=/host/INVALID.R",
        "dense": "/host/DENSE.js --receipt=/host/DENSE.R",
        "string": "/host/STRING.js --receipt=/host/STRING.R",
        "conversion": "/host/CONVERT.js --receipt=/host/CONVERT.R",
        "missing": "/host/MISSING.js --receipt=/host/MISSING.R",
        "denied": "/host/DENIED.js --receipt=/host/DENIED.R",
        "exists": "/host/FILE.js --receipt=/host/EXISTS",
        "repl": "--repl --receipt=/host/REPL.R",
        "line": "--repl --receipt=/host/LINE.R",
        "line-over": "--repl --receipt=/host/LINEOVER.R",
        "session": "--repl --receipt=/host/SESSION.R",
        "evals": "--repl --receipt=/host/EVALS.R",
        "diagnostics": "--repl --receipt=/host/DIAG.R",
        "idle": "--repl --receipt=/host/IDLE.R",
        "usage": "",
    }
    for tag, args in commands.items():
        (rd / (tag + ".txt")).write_text("pages\nexec QJS.BIN " + args + "\n")
    (rd / "bounds.txt").write_text("pages\nexec QBOUNDS.BIN\n")
    (rd / "cold.txt").write_text("pages\nexec QLAUNCH.BIN\n")
    (rd / "after.txt").write_text("pages\nprocs receipt QJS.BIN\ntty\nsyscalls\necho qjs-gate-done\n")
    (rd / "host.json").write_text(json.dumps({
        "macos": subprocess.check_output(["sw_vers", "-productVersion"], text=True).strip(),
        "machine": subprocess.check_output(["sysctl", "-n", "hw.model"], text=True).strip(),
        "ram": subprocess.check_output(["sysctl", "-n", "hw.memsize"], text=True).strip(),
        "load": os.getloadavg(),
        "processes": subprocess.check_output(["ps", "-axo", "pid,comm"], text=True),
    }, indent=2) + "\n")
    shutil.copyfile(rd / "host.json", evidence / "host.json")
    log = (rd / "cold-host.log").open("wb")
    subprocess.Popen([sys.executable, "-B", str(HERE / "cold_host.py"), str(rd)],
                     stdout=log, stderr=subprocess.STDOUT)
    log.close()
    for tag, mode, port in (("repl", "repl", 24786), ("session", "session", 24787),
                            ("evals", "evals", 24788), ("diagnostics", "diagnostics", 24789),
                            ("receipt", "receipt", 24790), ("eof", "eof", 24791)):
        log = (rd / ("driver-" + tag + ".log")).open("wb")
        subprocess.Popen([sys.executable, "-B", str(HERE / "interactive.py"), str(rd), tag, str(port), mode],
                         stdout=log, stderr=subprocess.STDOUT)
        log.close()
