#!/usr/bin/env python3
"""C2 pinned k4o inputs. Only the explicit `fetch` command uses the network."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.request

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
LOCK = json.loads((HERE / "lock.json").read_text())
CACHE = ROOT / ".build/k4o"
SDK_CACHE = ROOT / ".build/.k4o-sdk"
spec = importlib.util.spec_from_file_location("sdk", ROOT / "tools/zig/sdk.py")
sdk = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sdk)


def fetch():
    CACHE.mkdir(parents=True, exist_ok=True)
    archive = CACHE / (LOCK["commit"] + ".tar.gz")
    if not archive.exists():
        with urllib.request.urlopen(LOCK["archive_url"], timeout=60) as response:
            data = response.read()
        if sdk.digest(data) != LOCK["archive_sha256"]:
            raise ValueError("k4o archive checksum mismatch")
        with archive.open("xb") as output:
            output.write(data)
    with tempfile.TemporaryDirectory(prefix="k4o-fetch-") as temp:
        source(Path(temp))
    sdk.fetch(SDK_CACHE)


def source(materializations=CACHE):
    archive = CACHE / (LOCK["commit"] + ".tar.gz")
    if not archive.is_file():
        raise ValueError("missing pinned k4o archive; run python3 tools/zig/k4o/build.py fetch")
    if sdk.digest(archive.read_bytes()) != LOCK["archive_sha256"]:
        raise ValueError("k4o archive checksum mismatch")
    destination = materializations / ("source-" + LOCK["archive_sha256"])
    exists = destination.exists()
    stage = destination if exists else Path(tempfile.mkdtemp(prefix="k4o-", dir=materializations))
    manifest = {}
    try:
        with tarfile.open(archive) as tar:
            for member in tar:
                path = Path(*Path(member.name).parts[1:])
                if not path.parts:
                    continue
                if path.is_absolute() or ".." in path.parts or not (member.isdir() or member.isfile()):
                    raise ValueError("unsupported k4o archive member")
                if member.isdir():
                    continue
                data = tar.extractfile(member).read()
                manifest[path.as_posix()] = sdk.digest(data)
                out = stage / path
                if exists:
                    if out.is_symlink() or not out.is_file() or sdk.digest(out.read_bytes()) != manifest[path.as_posix()]:
                        raise ValueError("pinned k4o source drift: " + str(path))
                else:
                    out.parent.mkdir(parents=True, exist_ok=True)
                    out.write_bytes(data)
        actual = {p.relative_to(stage).as_posix() for p in stage.rglob("*") if p.is_file() or p.is_symlink()}
        if actual != set(manifest):
            raise ValueError("additional k4o source files")
        if not exists:
            stage.rename(destination)
    finally:
        if not exists and stage.exists():
            shutil.rmtree(stage)  # Only this call's unpublished temporary tree.
    return destination, manifest


def clean_env():
    return {k: v for k, v in os.environ.items()
            if not k.startswith(("ZIG_", "NIX_")) and
            k not in ("LIBRARY_PATH", "C_INCLUDE_PATH", "CPLUS_INCLUDE_PATH")}


def modules(root, upstream, options):
    return ["--dep", "k4o", "--dep", "build_options", "-Mroot=" + str(root),
            "--dep", "build_options", "-Mk4o=" + str(upstream / "src/root.zig"),
            "-Mbuild_options=" + str(options)]


def compile(mode, work, instrument=False):
    # Keep each immutable materialization outside the desktop's watched tree.
    # prepare still validates every byte and rejects every additional file.
    with tempfile.TemporaryDirectory(prefix="virelai-k4o-") as temp:
        private = Path(temp)
        archive = sdk.archive_path(SDK_CACHE)
        if not archive.is_file():
            raise ValueError("missing pinned compiler; run python3 tools/zig/k4o/build.py fetch")
        os.link(archive, private / archive.name)
        compiler, provenance = sdk.prepare(private)
        upstream, manifest = source(private)
        return compile_prepared(mode, work, instrument, compiler, provenance, upstream, manifest)


def compile_prepared(mode, work, instrument, compiler, provenance, upstream, manifest):
    work.mkdir(parents=True, exist_ok=True)
    inputs = [*sorted((ROOT / "user/zig").rglob("*.zig")),
              *sorted(HERE.glob("*.py")), HERE / "lock.json",
              ROOT / "tools/zig/sdk.py", ROOT / "tools/zig/guest.ld",
              ROOT / "tools/zig/lock.json", ROOT / "tools/zig/overlay.json",
              ROOT / "tools/check-zc-host-contract.py"]
    input_hashes = {str(p.relative_to(ROOT)): sdk.digest(p.read_bytes()) for p in inputs}
    version = (upstream / "build.zig.zon").read_text().split('.version = "')[1].split('"')[0]
    options = work / "options.zig"
    options.write_text('pub const engine_mode = "normal";\npub const version = "' + version +
                       '";\npub const instrument = ' + str(instrument).lower() + ";\n")
    command = [str(compiler / "zig"), "test" if mode in ("test", "upstream-test") else "build-exe"]
    command += ["--cache-dir", str(work / "local"), "--global-cache-dir", str(work / "global"),
                "-O", "ReleaseSafe"]
    if mode in ("guest", "gate"):
        output = work / ("K4OGATE.BIN" if mode == "gate" else "K4OTEST.BIN" if instrument else "K4O.BIN")
        command += ["--zig-lib-dir", str(compiler / "lib"), "-target", sdk.LOCK["target"],
                    "-mcpu", "baseline", "-fsingle-threaded", "-fstrip", "-fno-PIE",
                    "-fno-stack-check", "-fno-stack-protector", "-fno-unwind-tables",
                    "-fentry=_start", "-T", str(ROOT / "tools/zig/guest.ld"),
                    "-femit-asm=" + str(work / "k4o.s"), "-femit-bin=" + str(output)]
        root = ROOT / "user/zig/k4o.zig"
        if mode == "gate":
            command += ["--dep", "native", "--dep", "startup"]
            root = ROOT / "user/zig/k4o/gate.zig"
    elif mode == "host":
        output = work / "k4o-host"
        command += ["-femit-bin=" + str(output)]
        root = upstream / "src/main.zig"  # Independent, unchanged hosted CLI.
    elif mode == "upstream-test":
        output = None
        root = upstream / "tests.zig"
    else:
        output = None
        root = ROOT / "user/zig/k4o/tests.zig"
        command += ["--dep", "memory", "--dep", "startup"]
    command += modules(root, upstream, options)
    if mode == "gate":
        command += ["-Mnative=" + str(ROOT / "user/zig/native.zig"),
                    "-Mstartup=" + str(ROOT / "user/zig/startup.zig")]
    if mode == "test":
        command += ["-Mmemory=" + str(ROOT / "user/zig/arena.zig"),
                    "-Mstartup=" + str(ROOT / "user/zig/startup.zig")]
    result = subprocess.run(command, cwd=ROOT, env=clean_env(), text=True, capture_output=True)
    (work / "compiler.log").write_text(result.stdout + result.stderr)
    if result.returncode:
        raise ValueError(result.stdout + result.stderr)
    if any(sdk.digest(p.read_bytes()) != input_hashes[str(p.relative_to(ROOT))] for p in inputs):
        raise ValueError("BuildInputChanged: refuse evidence from a concurrent source edit")
    if mode in ("test", "upstream-test"):
        print(result.stdout + result.stderr, end="")
    if mode in ("guest", "gate"):
        subprocess.run([sys.executable, str(ROOT / "tools/check-zc-host-contract.py"),
                        "--profile", "sdk", str(output)], check=True)
        assembly = (work / "k4o.s").read_text()
        frames = sdk.stack_frames(assembly)
        (work / "stack-frames.json").write_text(json.dumps(frames, indent=2, sort_keys=True) + "\n")
        provenance.update({
            "workload_commit": LOCK["commit"], "workload_tree": LOCK["tree"],
            "workload_archive_sha256": LOCK["archive_sha256"],
            "workload_files": manifest, "artifact_sha256": sdk.digest(output.read_bytes()),
            "flags": command[3:command.index("--dep")],
            "source_sha256": input_hashes, "build_options": options.read_text(),
            "maximum_compiler_frame_bytes": max(frames.values()),
        })
        if mode == "guest" and not instrument:
            stack_spec = importlib.util.spec_from_file_location("stack", HERE / "stack.py")
            stack = importlib.util.module_from_spec(stack_spec)
            stack_spec.loader.exec_module(stack)
            provenance["stack"] = stack.bound(assembly, frames)
        output.with_suffix(".BIN.json").write_text(json.dumps(provenance, indent=2, sort_keys=True) + "\n")
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("fetch", "guest", "host", "test", "gate"))
    parser.add_argument("--work", type=Path)
    parser.add_argument("--instrument", action="store_true")
    args = parser.parse_args()
    try:
        if args.command == "fetch":
            fetch()
        else:
            work = (args.work or CACHE / args.command).resolve()
            print(compile(args.command, work, args.instrument) or "k4o guest policy tests passed")
    except (ValueError, OSError, tarfile.TarError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"k4o-build: {error}\n")


if __name__ == "__main__":
    main()
