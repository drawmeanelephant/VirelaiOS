#!/usr/bin/env python3
"""Offline shared-SDK filesystem acceptance artifact, not a workload port."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

sys.dont_write_bytecode = True
import sdk


def build(cache, work):
    compiler, provenance = sdk.prepare(cache)
    work.mkdir(parents=True, exist_ok=True)
    output = work / "ZFS.BIN"
    sources = [
        "user/zig/fs_fixture.zig", "user/zig/fs.zig", "user/zig/io.zig",
        "user/zig/platform.zig", "user/zig/runtime.zig", "user/zig/entry.zig",
        "user/zig/native.zig", "user/zig/arena.zig", "user/zig/console.zig",
        "user/zig/startup.zig", "user/zig/operations.zig", "user/zig/refusals.zig",
        "tools/zig/fs.py", "tools/zig/sdk.py", "tools/zig/lock.json",
        "tools/zig/overlay.json", "tools/zig/guest.ld", "tools/check-zc-host-contract.py",
    ]
    hashes = {p: sdk.digest((sdk.ROOT / p).read_bytes()) for p in sources}
    env = {k: v for k, v in os.environ.items() if not k.startswith(("ZIG_", "NIX_"))
           and k not in ("LIBRARY_PATH", "C_INCLUDE_PATH", "CPLUS_INCLUDE_PATH")}
    command = [
        str(compiler / "zig"), "build-exe", str(sdk.ROOT / sources[0]),
        "--zig-lib-dir", str(compiler / "lib"),
        "--cache-dir", str(work / "local"), "--global-cache-dir", str(work / "global"),
        "-target", sdk.LOCK["target"], "-mcpu", sdk.LOCK["cpu"], "-O", sdk.LOCK["optimize"],
        "-fsingle-threaded", "-fstrip", "-fno-PIE", "-fno-unwind-tables",
        "-fno-stack-check", "-fno-stack-protector", "-fentry=_start",
        "-T", str(sdk.ROOT / "tools/zig/guest.ld"),
        f"-femit-asm={work / 'fixture.s'}", f"-femit-bin={output}",
    ]
    result = subprocess.run(command, cwd=sdk.ROOT, env=env, capture_output=True, text=True)
    (work / "compiler.log").write_text(result.stdout + result.stderr)
    if result.returncode:
        raise ValueError(result.stdout + result.stderr)
    if hashes != {p: sdk.digest((sdk.ROOT / p).read_bytes()) for p in sources}:
        raise ValueError("BuildInputChanged")
    frames = sdk.stack_frames((work / "fixture.s").read_text())
    subprocess.run([sys.executable, str(sdk.ROOT / "tools/check-zc-host-contract.py"),
                    "--profile", "sdk", str(output)], check=True)
    provenance.update(source_sha256=hashes, artifact_sha256=sdk.digest(output.read_bytes()),
                      maximum_compiler_frame_bytes=max(frames.values()),
                      target=sdk.LOCK["target"], optimize=sdk.LOCK["optimize"],
                      cpu=sdk.LOCK["cpu"])
    output.with_suffix(".BIN.json").write_text(json.dumps(provenance, sort_keys=True, indent=2) + "\n")
    (work / "stack-frames.json").write_text(json.dumps(frames, sort_keys=True, indent=2) + "\n")
    return output


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cache", type=Path, default=sdk.DEFAULT_CACHE)
    parser.add_argument("--work", type=Path, default=sdk.DEFAULT_CACHE / "filesystem")
    args = parser.parse_args()
    print(build(args.cache.resolve(), args.work.resolve()))
