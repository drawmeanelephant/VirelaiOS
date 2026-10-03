#!/usr/bin/env python3
"""Build B5's explicit-context native fixture with the pinned compiler."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import sys

sys.dont_write_bytecode = True
import sdk


def build(cache, work, output):
    compiler, provenance = sdk.prepare(cache)
    work.mkdir(parents=True, exist_ok=True)
    output.parent.mkdir(parents=True, exist_ok=True)
    sources = ["tests/zig-thread-runtime.zig", "user/zig/threads.zig",
               "user/zig/native.zig", "kernel/src/thread_tls.zig",
               "tools/zig/guest.ld", "tools/zig/threads.py"]
    hashes = {p: sdk.digest((sdk.ROOT / p).read_bytes()) for p in sources}
    command = [
        str(compiler / "zig"), "build-exe", "--zig-lib-dir", str(compiler / "lib"),
        "--cache-dir", str(work / "local"), "--global-cache-dir", str(work / "global"),
        "-target", sdk.LOCK["target"], "-mcpu", sdk.LOCK["cpu"] + "+reserve_x18",
        "-O", sdk.LOCK["optimize"], "-fno-single-threaded", "-fno-PIE",
        "-fno-stack-protector", "-fno-unwind-tables", "-fentry=_start",
        "-T", str(sdk.ROOT / "tools/zig/guest.ld"),
        f"-femit-bin={output}", f"-femit-asm={work / 'fixture.s'}",
        "--dep", "threads", "--dep", "native", f"-Mroot={sdk.ROOT / sources[0]}",
        "--dep", "thread_tls", "--dep", "native", f"-Mthreads={sdk.ROOT / sources[1]}",
        f"-Mnative={sdk.ROOT / sources[2]}", f"-Mthread_tls={sdk.ROOT / sources[3]}",
    ]
    result = subprocess.run(command, cwd=sdk.ROOT, capture_output=True, text=True)
    (work / "compiler.log").write_text(result.stdout + result.stderr)
    if result.returncode:
        raise ValueError(result.stdout + result.stderr)
    if hashes != {p: sdk.digest((sdk.ROOT / p).read_bytes()) for p in sources}:
        raise ValueError("BuildInputChanged")
    assembly = (work / "fixture.s").read_text()
    if re.search(r"\b[wx]18\b", assembly):
        raise ValueError("PlatformRegisterUsed")
    frames = sdk.stack_frames(assembly)
    subprocess.run([sys.executable, str(sdk.ROOT / "tools/check-zc-host-contract.py"),
                    "--profile", "sdk", str(output)], check=True)
    provenance.update(source_sha256=hashes, artifact_sha256=sdk.digest(output.read_bytes()),
                      command=command, maximum_compiler_frame_bytes=max(frames.values()),
                      tls_model="explicit-static-local-exec-context")
    (work / "receipt.json").write_text(json.dumps(provenance, indent=2, sort_keys=True) + "\n")
    return output


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cache", type=Path, default=sdk.DEFAULT_CACHE)
    parser.add_argument("--work", type=Path, default=sdk.DEFAULT_CACHE / "threads")
    parser.add_argument("--output", type=Path, default=sdk.DEFAULT_CACHE / "ZTHREAD.BIN")
    args = parser.parse_args()
    try:
        print(build(args.cache.resolve(), args.work.resolve(), args.output.resolve()))
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"zig-threads: {error}\n")
