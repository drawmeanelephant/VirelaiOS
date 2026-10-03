#!/usr/bin/env python3
"""Class-A compiler semantics, native traversal, no-libc and stack bounds."""
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


def check(work, cache=build.sdk.DEFAULT_CACHE):
    work.mkdir(parents=True, exist_ok=True)
    binary = build.build("host", work / "host", cache)
    first = subprocess.check_output([str(binary)])
    second = subprocess.check_output([str(binary)])
    if first != second:
        raise ValueError("host compiler is not byte-stable")
    records = unpack(first)
    patched = build.build("patched-host", work / "patched-host", cache)
    if subprocess.check_output([str(patched)]) != first:
        raise ValueError("workspace changes altered pinned compiler artifact bytes")
    if golden(records) != json.loads((build.HERE / "golden.json").read_text()):
        raise ValueError("pinned compiler golden mismatch")
    if records["index.html"][1] != (build.HERE / "golden/index.html").read_bytes():
        raise ValueError("HTML golden byte mismatch")
    (work / "first.bundle").write_bytes(first)
    (work / "second.bundle").write_bytes(second)
    long_names = [p for p in records if p.startswith("guides/nested/") and p.endswith(".html")]
    if len(long_names) != 18 or len({p.split("/")[-1][:31] for p in long_names}) != 1:
        raise ValueError("long-name corpus lost its >16 colliding-prefix entries")
    build.build("patched-test", work / "tests", cache)
    candidates = [build.build("guest", work / f"release-{n}", cache) for n in range(2)]
    if candidates[0].read_bytes() != candidates[1].read_bytes():
        raise ValueError("offline compiler candidates are not byte-identical")
    receipt = json.loads(candidates[0].with_suffix(".BIN.json").read_text())
    if not receipt["no_libc"] or not receipt["release_ready"] or not receipt["stack_budget_verified"]:
        raise ValueError("guest closure must prove stack safety and publication readiness")
    if receipt["reserved_platform_register"] != "x18" or receipt["arena_bytes"] != 12582912:
        raise ValueError("native register/arena contract mismatch")
    bounds = receipt["stack_proof"]
    if not (bounds["maximum_frame_bytes"] <= bounds["guarded_stack_bytes"] < bounds["stack_budget_bytes"] == 131072):
        raise ValueError("invalid universal stack bound")
    (work / "result.json").write_text(json.dumps({
        "golden_artifacts": len(records), "long_names": len(long_names),
        "compiler_artifact_sha256": receipt["artifact_sha256"],
        "stack_proof": bounds, "native_acceptance": False,
        "blockers": receipt["blockers"],
    }, indent=2, sort_keys=True) + "\n")
    print(f"Class A: {len(records)} golden artifacts byte-stable, {len(long_names)} long nested names; "
          f"patched/untouched oracle bytes equal; two identical guarded no-libc serial offline artifacts; "
          f"worst-case stack <= {bounds['guarded_stack_bytes']} B; native publication verified separately by live-boris")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--work", type=Path, default=build.ROOT / "artifacts/boris-closure")
    parser.add_argument("--cache", type=Path, default=build.sdk.DEFAULT_CACHE)
    args = parser.parse_args()
    try:
        check(args.work.resolve(), args.cache.resolve())
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"boris-check: {error}\n")


if __name__ == "__main__":
    main()
