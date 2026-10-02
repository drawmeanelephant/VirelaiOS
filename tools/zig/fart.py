#!/usr/bin/env python3
"""C4 offline synth build/check, sharing A2's pinned compiler materializer."""
import argparse
import json
from pathlib import Path
import subprocess
import sys

import sdk

ROOT = sdk.ROOT
PIN = Path(__file__).with_name("fart-lock.json")
DEFAULT = ROOT / ".build/zig-fart"


def verify_sources():
    lock = json.loads(PIN.read_text())
    if lock["revision"] != "bd453937b30ae6eb28c69f6a497ba202efe4cd05":
        raise ValueError("unapproved fart-app revision")
    for path, pin in lock["sources"].items():
        if sdk.digest((ROOT / path).read_bytes()) != pin["sha256"]:
            raise ValueError(f"pinned source drift: {path}")
    return lock


def run(command, log=None):
    result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True, timeout=180)
    if log:
        log.write_text(result.stdout + result.stderr)
    if result.returncode:
        raise ValueError(f"command failed: {' '.join(map(str, command))}\n{result.stdout}{result.stderr}")
    return result.stdout + result.stderr


def build(compiler, work, output, source="user/zig/fart.zig"):
    verify_sources()
    work.mkdir(parents=True, exist_ok=True)
    output.parent.mkdir(parents=True, exist_ok=True)
    asm = work / "fart.s"
    command = [
        str(compiler / "zig"), "build-exe", source,
        "--zig-lib-dir", str(compiler / "lib"),
        "--cache-dir", str(work / "local"), "--global-cache-dir", str(work / "global"),
        "-target", sdk.LOCK["target"], "-mcpu", sdk.LOCK["cpu"], "-O", sdk.LOCK["optimize"],
        "-fsingle-threaded", "-fstrip", "-fno-PIE", "-fno-unwind-tables", "-fno-stack-check",
        "-fno-stack-protector", "-fentry=_start", f"-femit-asm={asm}",
        "-T", "tools/zig/guest.ld", f"-femit-bin={output}",
    ]
    run(command, work / "compiler.log")
    frames = sdk.stack_frames(asm.read_text())
    # No input-dependent recursion in this closed graph: the scanner, planner,
    # renderer, converters, allocator and formatter all iterate bounded inputs.
    # Counting EVERY emitted frame is stricter than any nonrecursive call path.
    total_frames = sum(frames.values())
    if total_frames > sdk.STACK_BUDGET:
        raise ValueError(f"StackBudget: conservative frame sum {total_frames}")
    check = run([sys.executable, str(ROOT / "tools/check-zc-host-contract.py"), "--profile", "sdk", str(output)])
    inputs = [PIN, Path(__file__), ROOT / "user/zig/fart.zig",
              *sorted((ROOT / "user/zig/fart").rglob("*.zig")),
              *sorted((ROOT / "user/zig").glob("*.zig")), ROOT / "tools/zig/guest.ld"]
    receipt = {
        "revision": verify_sources()["revision"],
        "artifact_sha256": sdk.digest(output.read_bytes()),
        "file_bytes": output.stat().st_size,
        "maximum_frame_bytes": max(frames.values()),
        "nonrecursive_stack_ceiling_bytes": total_frames,
        "arena_budget": 4194304,
        "output_budget": 441044,
        "source_sha256": {str(p.relative_to(ROOT)): sdk.digest(p.read_bytes()) for p in inputs},
        "elf_check": check.strip(),
    }
    output.with_suffix(".json").write_text(json.dumps(receipt, sort_keys=True, indent=2) + "\n")
    print(check.strip())
    print(f"fart-synth: built {output} bytes={receipt['file_bytes']} stack_ceiling={total_frames}")


def oracle(compiler, work):
    work.mkdir(parents=True, exist_ok=True)
    output = work / "oracle"
    # Darwin otherwise resolves @sin/@exp through libSystem. Explicitly link
    # the pinned compiler-rt object, using its required no-builtin recipe to
    # avoid LLVM rewriting memory implementations into self-calls. This is
    # host-only oracle glue; guest flags/source/math stay entirely unchanged.
    runtime = work / "compiler-rt.o"
    caches = ["--cache-dir", str(work / "local"), "--global-cache-dir", str(work / "global")]
    run([str(compiler / "zig"), "build-obj", str(compiler / "lib/compiler_rt.zig"),
         *caches,
         "-O", "ReleaseSafe", "-mcpu", "baseline", "-fno-builtin",
         "-femit-bin=" + str(runtime)], work / "math-compiler.log")
    run([str(compiler / "zig"), "build-exe", "user/zig/fart/oracle.zig", str(runtime),
         *caches,
         "-O", "ReleaseSafe", "-mcpu", "baseline", "-femit-bin=" + str(output)],
        work / "oracle-compiler.log")
    return output


def goldens(compiler, work):
    binary = oracle(compiler, work)
    cases = [
        ("phrase", "a", "a.wav"), ("phrase", "kujamba karibu", "phrase.wav"),
        ("phrase", "m", "breath.wav"), ("phrase", "Ng'OMA", "cluster.wav"),
        ("seed", "0", "seed0.wav"), ("seed", "42", "seed42.wav"),
        ("seed", "18446744073709551615", "seedmax.wav"),
        ("phrase", "bbbbb" + "a" * 16, "limit.wav"), ("phrase", "a" + " " * 254, "input.wav"),
    ]
    for kind, text, name in cases:
        run([str(binary), kind, text, str(work / name)])
        run([str(binary), kind, text, str(work / ("repeat-" + name))])
        if (work / name).read_bytes() != (work / ("repeat-" + name)).read_bytes():
            raise ValueError("host oracle is nondeterministic")
        expected = verify_sources().get("golden_sha256", {}).get(name)
        if expected is not None and sdk.digest((work / name).read_bytes()) != expected:
            raise ValueError(f"pinned WAV golden drift: {name}")
    (work / "goldens.json").write_text(json.dumps(
        {name: sdk.digest((work / name).read_bytes()) for _, _, name in cases}, indent=2) + "\n")
    return cases


def check(compiler, work):
    first, second = work / "first/FARTSYN.BIN", work / "second/FARTSYN.BIN"
    build(compiler, work / "first", first)
    build(compiler, work / "second", second)
    if first.read_bytes() != second.read_bytes():
        raise ValueError("nonreproducible guest build")
    unit = run([str(compiler / "zig"), "test", "-O", "ReleaseSafe",
                "--cache-dir", str(work / "unit-local"), "--global-cache-dir", str(work / "unit-global"),
                "--dep", "arena", "-Mroot=user/zig/fart/tests.zig", "-Marena=user/zig/arena.zig"],
               work / "unit.log")
    print(unit.strip().splitlines()[-1])
    goldens(compiler, work / "goldens")
    tests = run([sys.executable, str(ROOT / "tools/zig/test_fart.py")], work / "python-tests.log")
    print(tests.strip())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("build", "check", "goldens", "probe"))
    parser.add_argument("--cache", type=Path, default=sdk.DEFAULT_CACHE)
    parser.add_argument("--work", type=Path, default=DEFAULT)
    parser.add_argument("--output", type=Path, default=DEFAULT / "FARTSYN.BIN")
    args = parser.parse_args()
    try:
        verify_sources()
        compiler, provenance = sdk.prepare(args.cache.resolve())
        # Retain archive + private stdlib hashes without changing the SDK lock.
        work = args.work.resolve()
        work.mkdir(parents=True, exist_ok=True)
        (work / "toolchain.json").write_text(json.dumps(provenance, indent=2) + "\n")
        if args.command == "probe":
            build(compiler, work / "probe", work / "FARTPRO.BIN", "user/zig/fart_probe.zig")
        elif args.command == "build":
            build(compiler, work, args.output.resolve())
        elif args.command == "goldens":
            goldens(compiler, work)
        else:
            check(compiler, work)
    except (ValueError, OSError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
        parser.exit(1, f"fart-synth: {error}\n")


if __name__ == "__main__":
    main()
