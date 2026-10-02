#!/usr/bin/env python3
"""Class-A compiler evidence only. Never boot the over-stack-budget candidate."""
import argparse
import json
from pathlib import Path
import subprocess
import sys

sys.dont_write_bytecode = True
import build


def unpack(data):
    line, rest = data.split(b"\n", 1)
    fields = line.split()
    if fields[:2] != [b"boris-closure-probe", b"1"] or len(fields) != 3:
        raise ValueError("invalid probe header")
    count = int(fields[2])
    if not 0 <= count <= 128:
        raise ValueError("invalid artifact count")
    result = {}
    for _ in range(count):
        line, rest = rest.split(b"\n", 1)
        a, m, n = map(int, line.split())
        if min(a, m) <= 0 or n < 0 or a + m + n > len(rest):
            raise ValueError("invalid artifact frame")
        path = rest[:a].decode()
        if path in result:
            raise ValueError("duplicate artifact")
        result[path] = (rest[a:a + m].decode(), rest[a + m:a + m + n])
        rest = rest[a + m + n:]
    if rest:
        raise ValueError("trailing probe bytes")
    return result


def golden(records):
    return {path: {"media_type": media, "bytes": len(data), "sha256": build.sdk.digest(data)}
            for path, (media, data) in sorted(records.items())}


def check(work):
    work.mkdir(parents=True, exist_ok=True)
    binary = build.build("host", work / "host")
    first = subprocess.check_output([str(binary)])
    second = subprocess.check_output([str(binary)])
    if first != second:
        raise ValueError("host compiler is not byte-stable")
    records = unpack(first)
    if golden(records) != json.loads((build.HERE / "golden.json").read_text()):
        raise ValueError("pinned compiler golden mismatch")
    if records["index.html"][1] != (build.HERE / "golden/index.html").read_bytes():
        raise ValueError("HTML golden byte mismatch")
    (work / "first.bundle").write_bytes(first)
    (work / "second.bundle").write_bytes(second)
    long_names = [p for p in records if p.startswith("guides/nested/") and p.endswith(".html")]
    if len(long_names) != 18 or len({p.split("/")[-1][:31] for p in long_names}) != 1:
        raise ValueError("long-name corpus lost its >16 colliding-prefix entries")
    build.build("test", work / "tests")
    candidates = [build.build("audit", work / f"candidate-{n}") for n in range(2)]
    if candidates[0].read_bytes() != candidates[1].read_bytes():
        raise ValueError("offline compiler candidates are not byte-identical")
    receipt = json.loads((work / "candidate-0/closure.json").read_text())
    if not receipt["no_libc"] or receipt["release_ready"] or receipt["maximum_frame_bytes"] <= 131072:
        raise ValueError("expected blockers must not be reported as release success")
    result = subprocess.run([sys.executable, str(build.HERE / "build.py"), "guest",
                             "--work", str(work / "refused-release")], capture_output=True, text=True)
    (work / "release-refusal.log").write_text(result.stdout + result.stderr)
    if result.returncode == 0 or "StackBudget" not in result.stderr or (work / "refused-release/BORIS.BIN").exists():
        raise ValueError("unsafe release was not refused")
    print(f"Compiler-only: {len(records)} golden artifacts byte-stable, {len(long_names)} long nested names; "
          f"two identical no-libc candidates; release correctly blocked")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--work", type=Path, default=build.ROOT / "artifacts/boris-closure")
    args = parser.parse_args()
    try:
        check(args.work.resolve())
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"boris-check: {error}\n")


if __name__ == "__main__":
    main()
