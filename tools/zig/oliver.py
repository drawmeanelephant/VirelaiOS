#!/usr/bin/env python3
"""C1 pinned Oliver. Only `fetch` may access the network; builds fail offline."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
import sdk

ROOT = sdk.ROOT
LOCK_PATH = Path(__file__).with_name("oliver.lock.json")
LOCK = json.loads(LOCK_PATH.read_text())
DEFAULT_SOURCE = ROOT / ".build/oliver-source"


def verify(source):
    for name, sha in LOCK["sources"].items():
        path = source / name
        if path.is_symlink() or not path.is_file() or sdk.digest(path.read_bytes()) != sha:
            raise ValueError(f"Oliver source checksum mismatch: {name}; run explicit fetch")
    actual = {p.relative_to(source).as_posix() for p in (source / "src").glob("*.zig")}
    if actual != set(LOCK["sources"]):
        raise ValueError("Oliver source inventory drift")


def fetch(source):
    if source.exists():
        verify(source)
        return
    source.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="oliver-fetch-", dir=source.parent) as tmp:
        stage = Path(tmp) / "source"
        subprocess.run(["git", "clone", "--no-checkout", LOCK["repository"], str(stage)], check=True)
        subprocess.run(["git", "-C", str(stage), "checkout", "--detach", LOCK["revision"]], check=True)
        got = subprocess.check_output(["git", "-C", str(stage), "rev-parse", "HEAD"], text=True).strip()
        if got != LOCK["revision"]:
            raise ValueError("Oliver revision mismatch")
        verify(stage)
        stage.rename(source)


def materialize(source, work):
    verify(source)
    target = work / "source"
    target.mkdir(parents=True, exist_ok=True)
    for name in LOCK["sources"]:
        text = (source / name).read_text()
        for edit in LOCK["edits"].get(name, []):
            if text.count(edit["before"]) != 1:
                raise ValueError(f"Oliver adapter anchor drift: {name}")
            text = text.replace(edit["before"], edit["after"], 1)
        out = target / name
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(text)
    (work / "options.zig").write_text(
        f'pub const commit = "{LOCK["revision"]}";\npub const version = "1.1.0";\n')
    return target


def command(compiler, source, work, root, guest=True, probe=False):
    target = materialize(source, work)
    with (work / "options.zig").open("a") as options:
        options.write(f"pub const probe = {'true' if probe else 'false'};\n")
    args = [str(compiler / "zig"), "build-exe",
            "--zig-lib-dir", str(compiler / "lib"), "--cache-dir", str(work / "local"),
            "--global-cache-dir", str(work / "global"), "-O", "ReleaseSafe"]
    if guest:
        args += ["-target", sdk.LOCK["target"], "-mcpu", sdk.LOCK["cpu"], "-fsingle-threaded",
                 "-fstrip", "-fno-PIE", "-fno-unwind-tables", "-fno-stack-check",
                 "-fno-stack-protector", "-fentry=_start", "-T", str(ROOT / "tools/zig/guest.ld"),
                 f"-femit-asm={work / 'oliver.s'}", "--dep", "sdk"]
    args += ["--dep", "oliver", "--dep", "oliver_cli",
             "--dep", "build_options", f"-Mroot={root}"]
    if guest:
        args += [f"-Msdk={ROOT / 'user/zig/runtime.zig'}"]
    args += [f"-Moliver={target / 'src/oliver.zig'}",
             "--dep", "oliver", "--dep", "build_options", f"-Moliver_cli={target / 'src/main.zig'}",
             f"-Mbuild_options={work / 'options.zig'}"]
    return args


def link_command(compiler, obj, output):
    # Nothing outside the guarded object may add an unchecked stack frame.
    return [str(compiler / "zig"), "build-exe", str(obj),
            "--zig-lib-dir", str(compiler / "lib"), "-target", sdk.LOCK["target"],
            "-mcpu", sdk.LOCK["cpu"], "-O", "ReleaseSafe", "-fstrip", "-fno-PIE", "-fno-compiler-rt",
            "-fentry=_start", "-T", str(ROOT / "tools/zig/guest.ld"),
            f"-femit-bin={output}"]


def build(cache, source, work, output, probe=False):
    compiler, provenance = sdk.prepare(cache)
    work.mkdir(parents=True, exist_ok=True)
    output.parent.mkdir(parents=True, exist_ok=True)
    args = command(compiler, source, work, ROOT / "user/zig/oliver.zig", probe=probe)
    subprocess.run(args + [f"-femit-bin={work / 'unguarded.elf'}"], cwd=ROOT, check=True)
    frames = sdk.stack_frames((work / "oliver.s").read_text())
    if max(frames.values()) > 8192:
        raise ValueError("StackBudget: compiler frame exceeds the guard's reserved 8 KiB")
    guarded = guard_assembly((work / "oliver.s").read_text())
    (work / "guarded.s").write_text(guarded)
    subprocess.run([str(compiler / "zig"), "cc", "-target", sdk.LOCK["target"],
                    "-c", str(work / "guarded.s"), "-o", str(work / "guarded.o")], check=True)
    subprocess.run(link_command(compiler, work / "guarded.o", output), check=True)
    (work / "frames.json").write_text(json.dumps(frames, indent=2, sort_keys=True) + "\n")
    subprocess.run(["python3", str(ROOT / "tools/check-zc-host-contract.py"),
                    "--profile", "sdk", str(output)], check=True)
    provenance.update(revision=LOCK["revision"], source_sha256=LOCK["sources"], probe=probe,
                      adapter_lock_sha256=sdk.digest(LOCK_PATH.read_bytes()),
                      artifact_sha256=sdk.digest(output.read_bytes()),
                      maximum_frame=max(frames.values()), stack_entry_floor_bytes=120 * 1024,
                      guarded_assembly_sha256=sdk.digest(guarded.encode()),
                      adapter_sha256={str(p.relative_to(ROOT)): sdk.digest(p.read_bytes())
                                      for p in [Path(__file__), ROOT / "tools/zig/sdk.py",
                                                ROOT / "tools/zig/lock.json", ROOT / "tools/zig/guest.ld",
                                                ROOT / "tools/check-zc-host-contract.py",
                                                *sorted((ROOT / "user/zig").glob("*.zig"))]})
    output.with_suffix(output.suffix + ".json").write_text(json.dumps(provenance, indent=2, sort_keys=True) + "\n")
    print(f"Oliver {LOCK['revision']}: {output.stat().st_size} file bytes, maximum frame {max(frames.values())}")


def guard_assembly(text):
    """Entry-SP guard, before any frame, also on non-tail recursion.

    The only exemptions are the naked entry (sets the floor before branching)
    and the allocation/frame-free refusal. x16/x17 are AAPCS64 scratch, never
    argument registers. Guarded SP >= initial SP - 120 KiB; with every frame
    <= 8 KiB, no function can descend below the 128 KiB workload budget.
    """
    aliases = dict(re.findall(r"(?m)^(\S+) = (\S+)$", text))
    if "_start" not in aliases or "oliver_stack_refused" not in aliases:
        raise ValueError("StackBudget: missing native entry/refusal aliases")
    exempt = {aliases["_start"], aliases["oliver_stack_refused"]}
    functions = [n for n in re.findall(r"(?m)^\s*\.type\s+(.+),@function$", text) if n not in aliases]
    output = []
    count = 0
    pending = None
    for line in text.splitlines():
        match = re.fullmatch(r"\s*\.type\s+(.+),@function", line)
        if match:
            pending = match[1] if match[1] not in aliases else None
        output.append(line)
        if pending is not None and line.startswith(pending + ":"):
            if pending not in exempt:
                output += ["\tadrp x16, oliver_stack_floor",
                           "\tldr x16, [x16, :lo12:oliver_stack_floor]",
                           "\tmov x17, sp", "\tcmp x17, x16",
                           f"\tb.hs .Lc1_stack_ok_{count}", "\tb oliver_stack_refused",
                           f".Lc1_stack_ok_{count}:"]
            count += 1
            pending = None
    if count != len(functions) or not exempt <= set(functions):
        raise ValueError("StackBudget: unknown assembly function-label format")
    return "\n".join(output) + "\n"


def host(cache, source, work, output):
    # The independent ground truth is the pinned upstream hosted main, not
    # the guest adapter. It uses no libc and needs no third-party packages.
    compiler, _ = sdk.prepare(cache)
    work.mkdir(parents=True, exist_ok=True)
    (work / "options.zig").write_text(
        f'pub const commit = "{LOCK["revision"]}";\npub const version = "1.1.0";\n')
    args = [str(compiler / "zig"), "build-exe", "-O", "ReleaseSafe",
            "--cache-dir", str(work / "local"), "--global-cache-dir", str(work / "global"),
            "--dep", "oliver", "--dep", "build_options", f"-Mroot={source / 'src/main.zig'}",
            f"-Moliver={source / 'src/oliver.zig'}", f"-Mbuild_options={work / 'options.zig'}",
            f"-femit-bin={output}"]
    verify(source)
    subprocess.run(args, cwd=ROOT, check=True)


def tests(cache, source, work):
    compiler, _ = sdk.prepare(cache)
    work.mkdir(parents=True, exist_ok=True)
    args = command(compiler, source, work, ROOT / "user/zig/oliver_core.zig", guest=False)
    args[1] = "test"
    subprocess.run(args, cwd=ROOT, check=True)
    subprocess.run([str(compiler / "zig"), "test", str(ROOT / "user/zig/oliver_streams.zig")], check=True)
    subprocess.run([str(compiler / "zig"), "test", str(ROOT / "user/zig/oliver_builtins.zig")], check=True)


def probes(cache, source, work, output):
    compiler, _ = sdk.prepare(cache)
    work.mkdir(parents=True, exist_ok=True)
    output.parent.mkdir(parents=True, exist_ok=True)
    args = command(compiler, source, work, ROOT / "user/zig/oliver_probe.zig")
    subprocess.run(args + [f"-femit-bin={output}"], cwd=ROOT, check=True)
    subprocess.run(["python3", str(ROOT / "tools/check-zc-host-contract.py"),
                    "--profile", "sdk", str(output)], check=True)
    flat = output.parent / "FLAT.ELF"
    subprocess.run([str(compiler / "zig"), "build-exe",
                    str(ROOT / "tests/oliver-spike/flat-boundary.zig"),
                    "-target", sdk.LOCK["target"], "-O", "ReleaseSafe", "-fstrip",
                    "-fsingle-threaded", "-fno-PIE", "-fentry=_start",
                    "-T", str(ROOT / "tools/zc-host-link.ld"),
                    f"-femit-bin={flat}"], check=True)
    subprocess.run(["python3", str(ROOT / "tools/elf2bin.py"),
                    str(flat), str(output.parent / "FLAT.BIN")], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("fetch", "build", "host", "test", "probes"))
    parser.add_argument("--cache", type=Path, default=sdk.DEFAULT_CACHE)
    parser.add_argument("--source", type=Path, default=DEFAULT_SOURCE)
    parser.add_argument("--work", type=Path, default=ROOT / ".build/oliver-build")
    parser.add_argument("--output", type=Path, default=ROOT / ".build/oliver-build/OLIVER.BIN")
    parser.add_argument("--probe", action="store_true", help="include gate-only receipts and refusal probes")
    args = parser.parse_args()
    try:
        if args.probe and args.command != "build":
            raise ValueError("--probe applies only to the guest build")
        if args.command == "fetch":
            sdk.fetch(args.cache.resolve())
            fetch(args.source.resolve())
        else:
            globals()[{"host": "host", "test": "tests", "build": "build", "probes": "probes"}[args.command]](
                args.cache.resolve(), args.source.resolve(), args.work.resolve(),
                *([] if args.command == "test" else [args.output.resolve()]),
                **({"probe": args.probe} if args.command == "build" else {}))
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"oliver: {error}\n")


if __name__ == "__main__":
    main()
