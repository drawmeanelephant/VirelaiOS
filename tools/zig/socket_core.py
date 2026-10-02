#!/usr/bin/env python3
"""Offline injected socket-core fixture, not native network/SDK integration."""
import argparse
import json
import os
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
    sources = [
        "kernel/src/socket_core.zig", "kernel/src/tcp.zig", "kernel/src/arp.zig",
        "kernel/tests/net/socket_core_fixture.zig", "user/zig/native.zig",
        "tools/zig/socket_core.py", "tools/zig/sdk.py", "tools/zig/lock.json",
        "tools/zig/overlay.json", "tools/zig/guest.ld", "tools/check-zc-host-contract.py",
    ]
    hashes = {p: sdk.digest((sdk.ROOT / p).read_bytes()) for p in sources}
    env = {k: v for k, v in os.environ.items() if not k.startswith(("ZIG_", "NIX_"))
           and k not in ("LIBRARY_PATH", "C_INCLUDE_PATH", "CPLUS_INCLUDE_PATH")}
    cpu = sdk.LOCK["cpu"] + "+reserve_x18"
    command = [
        str(compiler / "zig"), "build-exe",
        "--zig-lib-dir", str(compiler / "lib"),
        "--cache-dir", str(work / "local"), "--global-cache-dir", str(work / "global"),
        "-target", sdk.LOCK["target"], "-mcpu", cpu, "-O", sdk.LOCK["optimize"],
        "-fsingle-threaded", "-fstrip", "-fno-PIE", "-fno-unwind-tables",
        "-fno-stack-check", "-fno-stack-protector", "-fentry=_start",
        "-T", str(sdk.ROOT / "tools/zig/guest.ld"),
        f"-femit-asm={work / 'fixture.s'}", f"-femit-bin={output}",
        "--dep", "socket_core", "--dep", "native",
        f"-Mroot={sdk.ROOT / sources[3]}",
        f"-Msocket_core={sdk.ROOT / sources[0]}",
        f"-Mnative={sdk.ROOT / 'user/zig/native.zig'}",
    ]
    result = subprocess.run(command, cwd=sdk.ROOT, env=env, capture_output=True, text=True)
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
    provenance.update(
        source_sha256=hashes, artifact_sha256=sdk.digest(output.read_bytes()),
        maximum_compiler_frame_bytes=max(frames.values()), target=sdk.LOCK["target"],
        optimize=sdk.LOCK["optimize"], cpu=cpu, evidence="injected-core-only",
    )
    output.with_suffix(".BIN.json").write_text(json.dumps(provenance, sort_keys=True, indent=2) + "\n")
    (work / "stack-frames.json").write_text(json.dumps(frames, sort_keys=True, indent=2) + "\n")
    return output


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cache", type=Path, default=sdk.DEFAULT_CACHE)
    parser.add_argument("--work", type=Path, default=sdk.DEFAULT_CACHE / "socket-core")
    parser.add_argument("--output", type=Path, default=sdk.DEFAULT_CACHE / "SOCKCORE.BIN")
    args = parser.parse_args()
    try:
        print(build(args.cache.resolve(), args.work.resolve(), args.output.resolve()))
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"socket-core: {error}\n")
