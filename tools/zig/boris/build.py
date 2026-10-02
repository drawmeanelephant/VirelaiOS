#!/usr/bin/env python3
"""Pinned C3 compiler closure probe. Only `fetch` may use the network."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
LOCK = json.loads((HERE / "lock.json").read_text())
INPUTS = ROOT / ".build/boris-inputs"
spec = importlib.util.spec_from_file_location("sdk", ROOT / "tools/zig/sdk.py")
sdk = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sdk)
from stack import frames, guard, proof


def verify(name, source):
    pin = LOCK[name]
    for path, sha in pin["files"].items():
        file = source / path
        if file.is_symlink() or not file.is_file() or sdk.digest(file.read_bytes()) != sha:
            raise ValueError(f"{name} checksum mismatch: {path}")
    actual = {p.relative_to(source).as_posix() for p in (source / "src").glob("*.zig")}
    if actual != {p for p in pin["files"] if p.startswith("src/")}:
        raise ValueError(f"{name} source inventory drift")


def fetch():
    INPUTS.mkdir(parents=True, exist_ok=True)
    for name in ("boris", "oliver"):
        source = INPUTS / name
        if source.exists():
            verify(name, source)
            continue
        with tempfile.TemporaryDirectory(prefix=name + "-fetch-", dir=INPUTS) as tmp:
            stage = Path(tmp) / "source"
            pin = LOCK[name]
            subprocess.run(["git", "clone", "--no-checkout", pin["repository"], str(stage)], check=True)
            subprocess.run(["git", "-C", str(stage), "checkout", "--detach", pin["revision"]], check=True)
            revision = subprocess.check_output(["git", "-C", str(stage), "rev-parse", "HEAD"], text=True).strip()
            if revision != pin["revision"]:
                raise ValueError(name + " revision mismatch")
            verify(name, stage)
            stage.rename(source)
    sdk.fetch(sdk.DEFAULT_CACHE)


def clean_env():
    return {k: v for k, v in os.environ.items()
            if not k.startswith(("ZIG_", "NIX_")) and
            k not in ("LIBRARY_PATH", "C_INCLUDE_PATH", "CPLUS_INCLUDE_PATH")}


def run(command, log, cwd=ROOT):
    result = subprocess.run(command, cwd=cwd, env=clean_env(), capture_output=True, text=True)
    log.write_text(result.stdout + result.stderr)
    if result.returncode:
        raise ValueError(result.stdout + result.stderr)
    return result.stdout + result.stderr


def materialize(work):
    """Boris-only exclusions, never patch the SDK or the dependency cache."""
    target = work / "source"
    patches = json.loads((HERE / "patches.json").read_text())
    stack_patches = json.loads((HERE / "workspace-patches.json").read_text())
    for path in LOCK["boris"]["files"]:
        data = (INPUTS / "boris" / path).read_bytes()
        if path in patches or path in stack_patches:
            text = data.decode()
            for edit in patches.get(path, []):
                if text.count(edit["before"]) != 1:
                    raise ValueError("Boris guest exclusion anchor drift: " + path)
                text = text.replace(edit["before"], edit["after"], 1)
            rules = stack_patches.get(path, {})
            if rules.get("parse_into"):
                # Keep the public value-returning API and every diagnostic,
                # but let failure sites write the caller's result directly.
                old = "fn fail(category: Category, line: u32, column: u32, message: []const u8) ParseResult {\n    return .{"
                new = "fn fail(out: *ParseResult, category: Category, line: u32, column: u32, message: []const u8) void {\n    out.* = .{"
                if text.count(old) != 1:
                    raise ValueError("Boris parser failure anchor drift")
                text = text.replace(old, new, 1)
                old = "pub fn parse(source: []const u8) ParseResult {"
                begin = text.index(old)
                end = text.index("\n// ---------------------------------------------------------------------------\n// Unit tests", begin)
                body = text[begin:end]
                if body.count("fail(") != 47 or body.count("return .{ .doc = doc };") != 3:
                    raise ValueError("Boris parser result-site inventory drift")
                body = body.replace(old, "noinline fn parseInto(source: []const u8, out: *ParseResult) void {")
                body = body.replace("fail(", "fail(out, ")
                body = body.replace("return .{ .doc = doc };", "out.* = .{ .doc = doc };\n        return;")
                body = body.replace("return .{ .diagnostic = .{", "out.* = .{ .diagnostic = .{")
                body = body.replace("        } };\n    }\n    if (unicode.warning)", "        } };\n        return;\n    }\n    if (unicode.warning)")
                wrapper = "pub fn parse(source: []const u8) ParseResult {\n    var result: ParseResult = undefined;\n    parseInto(source, &result);\n    return result;\n}\n\n"
                text = text[:begin] + wrapper + body + text[end:]
            for token, count in rules.get("stable_sorts", {}).items():
                if text.count(token) != count:
                    raise ValueError("Boris stable-sort anchor drift: " + path)
                text = text.replace(token, '@import("guest_workspace.zig").sort(')
            data = text.encode()
        out = target / path
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_bytes(data)
    (target / "src/guest_workspace.zig").write_bytes((ROOT / "user/zig/boris/workspace.zig").read_bytes())
    return target


def build(mode, work):
    work.mkdir(parents=True, exist_ok=True)
    compiler, receipt = sdk.prepare(sdk.DEFAULT_CACHE)
    for name in ("boris", "oliver"):
        verify(name, INPUTS / name)
    inputs = [*sorted((ROOT / "user/zig").glob("*.zig")),
              *sorted((ROOT / "user/zig/boris").glob("*.zig")),
              *sorted(HERE.glob("*.py")), HERE / "lock.json", HERE / "patches.json",
              HERE / "workspace-patches.json",
              ROOT / "tools/zig/sdk.py", ROOT / "tools/zig/lock.json",
              ROOT / "tools/zig/overlay.json", ROOT / "tools/zig/guest.ld",
              ROOT / "tools/check-zc-host-contract.py"]
    hashes = {str(p.relative_to(ROOT)): sdk.digest(p.read_bytes()) for p in inputs}
    guest = mode in ("guest", "audit")
    patched = mode in ("guest", "audit", "patched-host", "patched-test", "patched-upstream-test")
    source = materialize(work) if patched else INPUTS / "boris"
    tests = mode in ("test", "upstream-test", "patched-test", "patched-upstream-test")
    output = work / ("BORIS.BIN" if guest else "boris-oracle")
    root = ROOT / ("user/zig/boris.zig" if guest else "user/zig/boris/tests.zig" if tests else "user/zig/boris/host.zig")
    if mode in ("upstream-test", "patched-upstream-test"):
        root = source / "src/embed.zig"
    command = [str(compiler / "zig"), "test" if tests else "build-exe",
               "--cache-dir", str(work / "local"), "--global-cache-dir", str(work / "global"),
               "--zig-lib-dir", str(compiler / "lib"), "-O", "ReleaseSafe", "-freference-trace=40"]
    if guest:
        command += ["-target", sdk.LOCK["target"], "-mcpu", sdk.LOCK["cpu"],
                    "-fsingle-threaded", "-fstrip", "-fno-PIE", "-fno-unwind-tables",
                    "-fno-stack-check", "-fno-stack-protector", "-fentry=_start",
                    "-fno-compiler-rt",
                    "-T", str(ROOT / "tools/zig/guest.ld"),
                    "-femit-asm=" + str(work / "boris.s")]
    if not tests:
        command += ["-femit-bin=" + str(work / "unguarded.elf" if guest else output)]
    if mode in ("upstream-test", "patched-upstream-test"):
        command += ["--dep", "oliver", "-Mroot=" + str(root)]
    else:
        command += ["--dep", "boris", "-Mroot=" + str(root),
                    "--dep", "oliver", "-Mboris=" + str(source / "src/embed.zig")]
    command += ["-Moliver=" + str(INPUTS / "oliver/src/oliver.zig")]
    log = run(command, work / "compiler.log", INPUTS / "boris" if mode in ("upstream-test", "patched-upstream-test") else ROOT)
    if guest:
        assembly = (work / "boris.s").read_text()
        frame_sizes = frames(assembly)
        candidate = work / "unguarded.elf"
        run([sys.executable, str(ROOT / "tools/check-zc-host-contract.py"),
             "--profile", "sdk", str(candidate)], work / "candidate-check.log")
        # Linking from Zig alone, no build.zig/C/linkLibrary/-lc, then checking
        # the static ELF proves this compiler path, NOT native publication.
        forbidden = ("secp256k1", "nostr_keys", "nostr_sign", "nostr_publish",
                     "preview_server", "Thread.spawn", "Io.Threaded", "atproto_transport")
        if any(token in name for name in frame_sizes for token in forbidden):
            raise ValueError("UnsupportedHostedClosure")
        receipt.update(
            flags=command[2:command.index("--dep")], sources=hashes, dependencies=LOCK,
            candidate_sha256=sdk.digest(candidate.read_bytes()), no_libc=True,
            maximum_frame_bytes=max(frame_sizes.values()),
            oversized_frames={name: n for name, n in frame_sizes.items() if n > 8192},
            stack_budget_bytes=128 * 1024, entry_floor_bytes=120 * 1024,
            release_ready=False,
            blockers=["Shared SDK filesystem bridge pending"],
            scope="compiler memory closure only; no native discovery or publication",
        )
        (work / "closure.json").write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")
        (work / "frames.json").write_text(json.dumps(frame_sizes, indent=2, sort_keys=True) + "\n")
        print((work / "candidate-check.log").read_text(), end="")
        print(f"Compiler closure: {len(frame_sizes)} functions; largest frame {max(frame_sizes.values())} B")
        if mode == "audit":
            output = candidate
        else:
            release(compiler, work, assembly, frame_sizes, receipt)
    for name in ("boris", "oliver"):
        verify(name, INPUTS / name)
    if any(sdk.digest(p.read_bytes()) != hashes[str(p.relative_to(ROOT))] for p in inputs):
        raise ValueError("BuildInputChanged")
    if mode == "guest":
        (work / "guarded.elf").replace(output)
        output.with_suffix(".BIN.json").write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")
    if tests:
        print(log, end="")
    return None if tests else output


def release(compiler, work, assembly, frame_sizes, receipt):
    stack_proof = proof(assembly, frame_sizes)
    guarded = guard(assembly, frame_sizes)
    (work / "guarded.s").write_text(guarded)
    run([str(compiler / "zig"), "cc", "-target", sdk.LOCK["target"], "-c",
         str(work / "guarded.s"), "-o", str(work / "guarded.o")], work / "assembler.log")
    run([str(compiler / "zig"), "build-exe", str(work / "guarded.o"),
         "--zig-lib-dir", str(compiler / "lib"), "-target", sdk.LOCK["target"],
         "-mcpu", sdk.LOCK["cpu"], "-O", "ReleaseSafe", "-fstrip", "-fno-PIE",
         "-fno-compiler-rt", "-fentry=_start", "-T", str(ROOT / "tools/zig/guest.ld"),
         "-femit-bin=" + str(work / "guarded.elf")], work / "link.log")
    run([sys.executable, str(ROOT / "tools/check-zc-host-contract.py"),
         "--profile", "sdk", str(work / "guarded.elf")], work / "elf-check.log")
    receipt.update(
        artifact_sha256=sdk.digest((work / "guarded.elf").read_bytes()),
        guarded_functions=len(frame_sizes) - 2,
        guarded_assembly_sha256=sdk.digest(guarded.encode()),
        stack_proof={k: v for k, v in stack_proof.items() if k != "calls"},
        stack_budget_verified=True,
        scope="compiler memory closure probe; native discovery/publication awaits shared SDK bridge",
    )
    (work / "stack-proof.json").write_text(json.dumps(stack_proof, indent=2, sort_keys=True) + "\n")
    print((work / "elf-check.log").read_text(), end="")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("fetch", "audit", "guest", "host", "test", "upstream-test",
                                          "patched-host", "patched-test", "patched-upstream-test"))
    parser.add_argument("--work", type=Path)
    args = parser.parse_args()
    try:
        if args.command == "fetch":
            fetch()
        else:
            print(build(args.command, (args.work or ROOT / ".build/boris" / args.command).resolve()) or "tests passed")
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"boris-build: {error}\n")


if __name__ == "__main__":
    main()
