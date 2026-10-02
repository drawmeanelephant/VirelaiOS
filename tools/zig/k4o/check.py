#!/usr/bin/env python3
"""Offline C2 acceptance, corpus staging and independent share comparison."""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
from unittest.mock import patch

sys.dont_write_bytecode = True
HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("build", HERE / "build.py")
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)
ROOT = build.ROOT
CACHE = build.CACHE


def oracle(args):
    result = subprocess.run([str(CACHE / "host/k4o-host"), *args], capture_output=True)
    if result.returncode:
        raise ValueError("host oracle failed: " + result.stderr.decode(errors="replace"))
    return result.stdout


def stage(share, expected):
    share.mkdir(parents=True, exist_ok=True)
    expected.mkdir(parents=True, exist_ok=True)
    for source, name in (("guest/K4O.BIN", "K4O.BIN"), ("gate/K4OGATE.BIN", "K4OGATE.BIN"),
                         ("instrumented/K4OTEST.BIN", "K4OTEST.BIN")):
        path = CACHE / source
        if not path.is_file():
            raise ValueError("missing " + str(path) + "; build explicitly before running the gate")
        shutil.copyfile(path, share / name)
    cases = []

    def add(id, args, output=b"", error=None, status=0, binary="K4O.BIN", metrics=False):
        for arg in args:
            if len(arg.encode()) > 255 or any(c in arg for c in "\n\t\0"):
                raise ValueError("invalid owned launch argument")
        if len(args) > 7:
            raise ValueError("owned launcher ArgumentLimit")
        if error is None and status != 0:
            raise ValueError("refusal case requires a diagnostic")
        (expected / (id + ".out")).write_bytes(output)
        cases.append(dict(id=id, args=args, status=status, error=error, binary=binary, metrics=metrics))

    with tempfile.TemporaryDirectory(prefix="k4o-corpus-") as temp:
        upstream, _ = build.source(Path(temp))
        corpus = sorted((upstream / "fixtures").glob("*.knap")) + sorted((upstream / "examples").glob("*.knap"))
        for i, template in enumerate(corpus):
            data = template.with_suffix(".json")
            shutil.copyfile(template, share / f"t{i}")
            shutil.copyfile(data, share / f"d{i}")
            for format in ("textile", "markdown", "gfm"):
                output = oracle(["render", str(template), "--data", str(data), "--format", format])
                # Existing pinned golden is a second oracle when present.
                golden = template.with_suffix("." + format)
                if golden.is_file() and output != golden.read_bytes():
                    raise ValueError("host engine differs from pinned golden " + str(golden))
                add(f"g{i}-{format}", ["render", f"t{i}", f"--data=d{i}", "--format=" + format], output)
        for i, template in enumerate(sorted((upstream / "fixtures/errors").glob("*.knap"))):
            data = template.with_suffix(".json")
            shutil.copyfile(template, share / f"e{i}")
            args = ["render", f"e{i}"]
            if data.is_file():
                shutil.copyfile(data, share / f"j{i}")
                args += [f"-d=j{i}"]
            # Pinned engine diagnostics are retained, with a named CLI error.
            diagnostic = "TemplateDepthLimit" if template.stem == "err-nesting-too-deep" else template.with_suffix(".error").read_text().strip()
            add(f"error{i}", args, error=diagnostic, status=70)

    (share / "plain").write_bytes(b"hello")
    (share / "basic").write_bytes(b"{{title|bold}}")
    (share / "data").write_bytes(b'{"title":"hello"}')
    for id, args in (
        ("seven", ["render", "basic", "--data", "data", "--format", "gfm", "-m=1M"]),
        ("short", ["render", "basic", "-d", "data", "--format=markdown", "-m", "1k"]),
        ("equals", ["render", "basic", "-d=data", "--format=textile", "--max-output=1024"]),
        ("repeat-limit", ["render", "basic", "--data=data", "-m=1", "-m=1k"]),
    ):
        host_args = [str(share / arg) if arg in ("basic", "data") else
                     arg.replace("=data", "=" + str(share / "data")) for arg in args]
        add(id, args, oracle(host_args))
    add("default-data", ["render", "plain"], b"hello")
    add("version", ["--version"], oracle(["--version"]))
    add("help", ["-h"])
    # Guest help deliberately differs from host defaults; verify it exactly via
    # the shared policy's pinned literal rather than comparing the host help.
    text = (ROOT / "user/zig/k4o/cli.zig").read_text().split("pub const help =\n")[1].split("\n;")[0]
    (expected / "help.out").write_text("\n".join(line.strip()[2:] for line in text.splitlines()))

    (share / "bad-json").write_bytes(b"{broken")
    (share / "not-object").write_bytes(b"[]")
    for id, args, name, status in (
        ("bad-json", ["render", "plain", "--data=bad-json"], "InvalidJson", 70),
        ("not-object", ["render", "plain", "-d=not-object"], "JsonObjectRequired", 70),
        ("missing-file", ["render", "missing"], "FileNotFound", 70),
        ("invalid-format", ["render", "plain", "--format=html"], "InvalidFormat", 64),
        ("unknown-option", ["render", "plain", "--threads=2"], "UnknownOption", 64),
        ("duplicate-data", ["render", "plain", "-d=data", "--data=data"], "DuplicateData", 64),
        ("duplicate-format", ["render", "plain", "--format=gfm", "--format=gfm"], "DuplicateFormat", 64),
        ("missing-template", ["render"], "MissingTemplate", 64),
        ("zero-limit", ["render", "plain", "-m=0"], "OutputLimit", 64),
        ("above-limit", ["render", "plain", "-m=1048577"], "OutputLimit", 64),
        ("host-limit", ["render", "plain", "-m=256m"], "OutputLimit", 64),
        ("negative-limit", ["render", "plain", "-m=-1"], "NegativeOutputLimit", 64),
        ("bad-limit", ["render", "plain", "-m=xyz"], "InvalidOutputLimit", 64),
        ("output-small", ["render", "plain", "-m=4"], "OutputLimit", 70),
        ("path-component", ["render", "x" * 32], "PathLimit", 70),
        ("path-full", ["render", "x" * 31 + "/" + "y" * 27], "PathLimit", 70),
        ("path-escape", ["render", "../plain"], "PathLimit", 70),
        ("path-255", ["render", "x" * 255], "PathLimit", 70),
    ):
        add(id, args, error=name, status=status)

    for id, data in (("input-exact", b"x" * 131072), ("input-over", b"x" * 131073)):
        (share / id).write_bytes(data)
        add(id, ["render", id], data if id.endswith("exact") else b"",
            error=None if id.endswith("exact") else "InputLimit", status=0 if id.endswith("exact") else 70)
    for id, n in (("json-exact", 131072), ("json-over", 131073)):
        (share / id).write_bytes(b'{"title":"hello"}' + b" " * (n - 17))
        # The object above is 17 bytes; the read proof probes one byte past cap.
        assert (share / id).stat().st_size == n
        add(id, ["render", "basic", "-d=" + id], b"*hello*" if id.endswith("exact") else b"",
            error=None if id.endswith("exact") else "InputLimit", status=0 if id.endswith("exact") else 70)

    (share / "repeat-data").write_text('{"items":[' + ",".join("0" for _ in range(16)) + "]}")
    (share / "output-exact").write_bytes(b"{% for x in items %}" + b"x" * 65536 + b"{% endfor %}")
    (share / "output-over").write_bytes((share / "output-exact").read_bytes() + b"!")
    add("output-exact", ["render", "output-exact", "-d=repeat-data"], b"x" * 1048576)
    add("output-over", ["render", "output-over", "-d=repeat-data"], error="OutputLimit", status=70)
    (share / "oom").write_bytes(b"{{x}}" * (131072 // 5))
    add("oom", ["render", "oom"], error="OutOfMemory", status=70, binary="K4OTEST.BIN", metrics=True)
    (share / "diagnostic-limit").write_bytes(b"prefix{{x|" + b"u" * 20000 + b"}}")
    add("diagnostic-limit", ["render", "diagnostic-limit"], error="DiagnosticLimit", status=70)

    (share / "stack-ok").write_text("{% if " + "not " * 8 + "true %}" +
                                    "{% if true %}" * 7 + "{{deep}}" + "{% endif %}" * 8)
    (share / "stack-data").write_text('{"deep":' + "[" * 7 + "0" + "]" * 7 + "}")
    stack_output = oracle(["render", str(share / "stack-ok"), "--data", str(share / "stack-data")])
    add("stack-ok", ["render", "stack-ok", "-d=stack-data"], stack_output, binary="K4OTEST.BIN", metrics=True)
    (share / "depth-over").write_text("{% if true %}" * 9)
    (share / "cond-over").write_text("{% if " + "not " * 9 + "true %}")
    (share / "json-deep").write_text('{"deep":' + "[" * 8 + "0" + "]" * 8 + "}")
    for id, args, error in (
        ("depth-over", ["render", "depth-over"], "TemplateDepthLimit"),
        ("cond-over", ["render", "cond-over"], "ConditionLimit"),
        ("json-deep", ["render", "basic", "-d=json-deep"], "JsonDepthLimit"),
    ):
        add(id, args, error=error, status=70)
    (expected / "cases.json").write_text(json.dumps(cases, indent=2) + "\n")
    assert len(cases) == 212, "update the declarative batch plan when the corpus changes"
    (share / "K4O.CASES").write_text("".join("\t".join([c["id"], c["binary"], str(c["status"]), *c["args"]]) + "\n" for c in cases))
    print(f"k4o: staged {len(cases)} cases, {3 * len(corpus)} independent host format goldens")


def compare(share, expected, evidence, batch=None):
    cases = json.loads((expected / "cases.json").read_text())
    if batch is not None:
        cases = cases[batch * 12:(batch + 1) * 12]
    evidence.mkdir(parents=True, exist_ok=True)
    for c in cases:
        id = c["id"]
        out = (share / (id + ".out")).read_bytes()
        err = (share / (id + ".err")).read_bytes()
        shutil.copyfile(share / (id + ".out"), evidence / (id + ".out"))
        shutil.copyfile(share / (id + ".err"), evidence / (id + ".err"))
        if len(err) > 16 * 1024:
            raise ValueError(id + ": DiagnosticLimit exceeded")
        if out != (expected / (id + ".out")).read_bytes():
            raise ValueError(id + ": golden output mismatch")
        if c["error"] is not None:
            if c["error"].encode() not in err or not err.startswith(b"k4o: "):
                raise ValueError(id + ": missing explicit stderr diagnostic " + c["error"])
            if out:
                raise ValueError(id + ": error contaminated stdout")
        elif not c["metrics"] and err:
            raise ValueError(id + ": successful render contaminated stderr")
        if c["metrics"]:
            match = re.search(rb"k4o-budget: arena_peak=(\d+) stack_high_water=(\d+) files_peak=(\d+)\n", err)
            if not match:
                raise ValueError(id + ": missing budget measurement")
            arena, stack, files = map(int, match.groups())
            if not (0 < arena <= 8388608 and 0 < stack <= 131072 and 0 < files <= 2):
                raise ValueError(id + ": budget exceeded")
    print(f"k4o: byte-compared {len(cases)} stdout files; checked {len(cases)} independent stderr destinations")


def check():
    subprocess.run([sys.executable, str(HERE / "test_build.py")], check=True)
    # Any accidental download in any compile/materialization is a hard failure.
    with patch.object(build.urllib.request, "urlopen", side_effect=AssertionError("OfflineBuildNetworkAccess")):
        build.compile("test", CACHE / "test")
        with tempfile.TemporaryDirectory(prefix="k4o-repro-", dir=CACHE) as temp:
            work = Path(temp)
            first = build.compile("guest", work / "first")
            second = build.compile("guest", work / "second")
            if first.read_bytes() != second.read_bytes():
                raise ValueError("non-reproducible guest bytes")
            (CACHE / "guest").mkdir(exist_ok=True)
            shutil.copyfile(first, CACHE / "guest/K4O.BIN")
            shutil.copyfile(first.with_suffix(".BIN.json"), CACHE / "guest/K4O.BIN.json")
        build.compile("guest", CACHE / "instrumented", instrument=True)
        build.compile("gate", CACHE / "gate")
        build.compile("host", CACHE / "host")
        build.compile("upstream-test", CACHE / "upstream-test")
    print("k4o: two byte-identical offline guest builds; CLI, gate, host and policy checks passed")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("check", "stage", "compare"))
    parser.add_argument("--share", type=Path)
    parser.add_argument("--expected", type=Path)
    parser.add_argument("--evidence", type=Path)
    parser.add_argument("--batch", type=int)
    args = parser.parse_args()
    try:
        if args.command == "check":
            check()
        elif args.command == "stage":
            stage(args.share.resolve(), args.expected.resolve())
        else:
            compare(args.share.resolve(), args.expected.resolve(), args.evidence.resolve(), args.batch)
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"k4o-check: {error}\n")


if __name__ == "__main__":
    main()
