#!/usr/bin/env python3
"""Offline ADR 0039 runtime checks, not guest/product acceptance."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

sys.dont_write_bytecode = True
import build
import inspection
from materialize import INPUT, LOCK, ROOT, sha


def run(command, work, env):
    result = subprocess.run(command, cwd=work, env=env, text=True, capture_output=True)
    if result.returncode:
        raise ValueError("ContractCheckFailed: " + result.stdout + result.stderr)
    return result.stdout + result.stderr


def units(work, cache):
    compiler, _ = build.prepare(cache)
    work.mkdir(parents=True, exist_ok=True)
    library, _ = build.math_library.prepare(compiler, work)
    env = build.environment()
    env["ZIG_LOCAL_CACHE_DIR"] = str(work / "cache/local")
    env["ZIG_GLOBAL_CACHE_DIR"] = str(work / "cache/global")
    zig = str(compiler / "zig")
    for name in ("control", "math_test", "allocator"):
        command = [zig, "test", "-O", "ReleaseSafe", "--zig-lib-dir", str(library)]
        if name == "allocator":
            command += ["--dep", "arena", "-Mroot=" + str(INPUT / "allocator.zig"),
                        "-Marena=" + str(ROOT / "user/zig/arena.zig")]
        else:
            command += [str(INPUT / (name + ".zig"))]
        (work / (name + ".log")).write_text(run(command, work, env))
    command = [zig, "test", str(INPUT / "bytes_test.zig"), "-O", "ReleaseSafe",
               "--zig-lib-dir", str(compiler / "lib"), "-cflags", "-std=c11", "-ffreestanding",
               "-fno-builtin", "-funsigned-char", "-nostdinc", "-I", str(INPUT / "headers"),
               "-isystem", str(compiler / "lib/include"), "--",
               *(str(INPUT / name) for name in ("bytes.c", "format.c", "bytes_fixture.c", "format_fixture.c"))]
    (work / "bytes.log").write_text(run(command, work, env))
    (work / "python.log").write_text(run([sys.executable, "-B", "-m", "unittest", "test_materialize", "test_safety"],
                                       ROOT / "tools/quickjs-runtime", env))
    return work


def refusals(work, cache, native_work):
    compiler, _ = build.prepare(cache)
    work.mkdir(parents=True, exist_ok=True)
    env = build.environment()
    env["ZIG_LOCAL_CACHE_DIR"] = str(work / "cache/local")
    env["ZIG_GLOBAL_CACHE_DIR"] = str(work / "cache/global")
    names = inspection.INVENTORY["refused"]
    if len(names) != 134 or len(set(names)) != 134:
        raise ValueError("InventoryDrift")
    receipts = {}
    for name in names:
        source = work / (name + ".c")
        source.write_text("extern void " + name + "(void);\n"
                          "void _start(void) { " + name + "(); for (;;) {} }\n")
        obj = work / (name + ".o")
        compile_command = [str(compiler / "zig"), "cc", *build.C_FLAGS, "-c", str(source), "-o", str(obj)]
        run(compile_command, work, env)
        inspection.symbols(obj.read_bytes())
        # Reject the injected reference against the real complete C closure.
        try:
            inspection.imports([*sorted((native_work / "c/objects").glob("*.o")), obj])
        except ValueError as error:
            if "UnexpectedImport: " + name not in str(error):
                raise
        else:
            raise ValueError("RefusedReferenceAccepted: " + name)
        # Independently verify that the freestanding linker has no hidden
        # platform shim. No libc/sysroot is available to satisfy the reference.
        command = [str(compiler / "zig"), "build-exe", str(obj), "-target", build.sdk.LOCK["target"],
                   "-mcpu", build.sdk.LOCK["cpu"], "-O", "ReleaseSafe", "-fentry=_start",
                   "-T", str(ROOT / "tools/zig/guest.ld"), "-femit-bin=" + str(work / (name + ".BIN"))]
        result = subprocess.run(command, cwd=work, env=env, text=True, capture_output=True)
        if result.returncode == 0 or ("undefined symbol: " + name) not in result.stderr:
            raise ValueError("RefusalLinkNegativeFailed: " + name + "\n" + result.stderr)
        (work / (name + ".link.log")).write_text(result.stderr)
        receipts[name] = {"injected_object_sha256": sha(obj.read_bytes()), "closure": "UnexpectedImport", "link": "undefined symbol"}
    (work / "refusals.json").write_text(json.dumps(receipts, sort_keys=True, indent=2) + "\n")
    return work / "refusals.json"


def fatal(work, cache, host_work):
    compiler, _ = build.prepare(cache)
    work.mkdir(parents=True, exist_ok=True)
    env = build.environment()
    env["ZIG_LOCAL_CACHE_DIR"] = str(work / "cache/local")
    env["ZIG_GLOBAL_CACHE_DIR"] = str(work / "cache/global")
    probe = work / "assert.o"
    flags = [flag for flag in build.C_FLAGS if flag not in ("-fno-pic", "-fno-pie")]
    flags[flags.index("aarch64-freestanding-none")] = "aarch64-macos"
    run([str(compiler / "zig"), "cc", *flags, "-I", str(INPUT / "headers"),
         "-isystem", str(compiler / "lib/include"), "-c", str(INPUT / "fatal_probe.c"), "-o", str(probe)], work, env)
    for assertion in (False, True):
        options = work / "options.zig"
        options.write_text("pub const assertion = " + str(assertion).lower() + ";\n")
        artifact = work / ("assert" if assertion else "abort")
        command = [str(compiler / "zig"), "build-exe", "-lc", "-O", "ReleaseSafe",
                   "-target", "aarch64-macos", "-femit-bin=" + str(artifact), str(probe),
                   *(str(p) for p in sorted((host_work / "c/objects").glob("*.o"))),
                   "--dep", "qjs", "--dep", "fatal_options", "-Mroot=" + str(INPUT / "fatal_fixture.zig"),
                   "--dep", "arena", "--dep", "hooks", "-Mqjs=" + str(INPUT / "runtime.zig"),
                   "-Marena=" + str(ROOT / "user/zig/arena.zig"),
                   "-Mhooks=" + str(INPUT / "host_hooks.zig"), "-Mfatal_options=" + str(options)]
        run(command, work, env)
        result = subprocess.run([str(artifact)], cwd=work, env=env, capture_output=True)
        if result.returncode != 71 or result.stderr != b"EngineInvariant\n":
            raise ValueError("FatalInvariantContract")
    return work


def kernel(work, cache, native_work):
    compiler, _ = build.prepare(cache)
    work.mkdir(parents=True, exist_ok=True)
    data = (native_work / "QJS-RUNTIME.BIN").read_bytes()
    (work / "runtime.elf").write_bytes(data)
    (work / "cases.zig").write_text(
        'pub const fixtures = [_]struct {name: []const u8, bytes: []const u8, accepted: bool}{\n'
        '.{.name="quickjs-runtime", .bytes=@embedFile("runtime.elf"), .accepted=true},\n};\n')
    env = build.environment()
    env["ZIG_LOCAL_CACHE_DIR"] = str(work / "cache/local")
    env["ZIG_GLOBAL_CACHE_DIR"] = str(work / "cache/global")
    command = [str(compiler / "zig"), "test", "--dep", "elf", "--dep", "cases",
               "-Mroot=" + str(ROOT / "tools/zig/kernel_contract.zig"),
               "-Melf=" + str(ROOT / "kernel/src/elf.zig"), "-Mcases=" + str(work / "cases.zig")]
    (work / "kernel.log").write_text(run(command, work, env))
    return work


def all_checks(work, cache):
    if (work / "native-a").exists() or (work / "native-b").exists():
        raise ValueError("FreshWorkRequired: use a new --work directory for clean reproducibility checks")
    work.mkdir(parents=True, exist_ok=True)
    units(work / "units", cache)
    build.linked(work / "host", cache, hosted=True)
    fatal(work / "fatal", cache, work / "host")
    first = build.linked(work / "native-a", cache)
    second = build.linked(work / "native-b", cache)
    baseline = build.linked(work / "baseline", cache, baseline=True)
    if first.read_bytes() != second.read_bytes():
        raise ValueError("NativeArtifactReproducibility")
    if first.with_suffix(".BIN.json").read_bytes() != second.with_suffix(".BIN.json").read_bytes():
        raise ValueError("NativeInputReceiptReproducibility")
    for receipt in ("c/objects.receipt.json", "stack-frames.json"):
        if (first.parent / receipt).read_bytes() != (second.parent / receipt).read_bytes():
            raise ValueError("NativeObjectReceiptReproducibility: " + receipt)
    full, empty = inspection.elf(first), inspection.elf(baseline)
    file_delta = full["file_bytes"] - empty["file_bytes"]
    initialized_delta = sum(segment["filesz"] for segment in full["segments"]) - sum(segment["filesz"] for segment in empty["segments"])
    if max(file_delta, initialized_delta) > 1_572_864:
        raise ValueError("EngineSizeLimit")
    refusals(work / "refusals", cache, first.parent)
    kernel(work / "kernel", cache, first.parent)
    report = {
        "native_artifact_sha256": sha(first.read_bytes()), "baseline_sha256": sha(baseline.read_bytes()),
        "file_delta_bytes": file_delta, "initialized_load_delta_bytes": initialized_delta,
        "mapped_image_and_args_bytes": full["mapped_bytes"],
        "conservative_arena_image_stack_pages": (4_194_304 + full["mapped_bytes"] + 196_608 + 4095) // 4096,
        "inventory": inspection.imports(sorted((first.parent / "c/objects").glob("*.o"))),
        "reproducible": True, "fatal_invariant_status": 71,
        "scope": "offline runtime and linked fixture; no guest/product timing or acceptance claim",
    }
    (work / "verification.json").write_text(json.dumps(report, sort_keys=True, indent=2) + "\n")
    return work / "verification.json"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("units", "refusals", "fatal", "kernel", "all"))
    parser.add_argument("--work", type=Path, default=ROOT / ".build/quickjs/check")
    parser.add_argument("--cache", type=Path, default=build.sdk.DEFAULT_CACHE)
    parser.add_argument("--native-work", type=Path, default=ROOT / ".build/quickjs/native")
    parser.add_argument("--host-work", type=Path, default=ROOT / ".build/quickjs/host")
    args = parser.parse_args()
    try:
        function = {"units": units, "refusals": refusals, "fatal": fatal, "kernel": kernel, "all": all_checks}[args.command]
        arguments = [args.work.resolve(), args.cache.resolve()]
        if args.command in ("refusals", "kernel"):
            arguments.append(args.native_work.resolve())
        if args.command == "fatal":
            arguments.append(args.host_work.resolve())
        print(function(*arguments))
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"quickjs-runtime: {error}\n")


if __name__ == "__main__":
    main()
