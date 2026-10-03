#!/usr/bin/env python3
"""Static-TLS preparation; --integration checks the pinned compiler offline."""
import argparse
import importlib.util
import json
from pathlib import Path
import struct
import subprocess
import sys
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[2]


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


sdk = module("tls_sdk", ROOT / "tools/zig/sdk.py")
checker = module("tls_checker", ROOT / "tools/check-zc-host-contract.py")
LOCAL_EXEC = {549, 551}  # TLSLE_ADD_TPREL_HI12 / TLSLE_ADD_TPREL_LO12_NC.


def require_local_exec(relocations):
    if set(relocations) != LOCAL_EXEC:
        raise ValueError(f"not the pinned static local-exec model: {sorted(set(relocations))}")


class Elf:
    """Inspect compiler-emitted ELF64 fixtures, not a production ELF loader."""

    def __init__(self, data):
        self.data = data
        if len(data) < 64 or data[:7] != b"\x7fELF\x02\x01\x01":
            raise ValueError("expected little-endian ELF64")
        header = struct.unpack_from("<16sHHIQQQIHHHHHH", data)
        self.kind = header[1]
        if header[2] != 183 or header[11] != 64 or (header[10] and header[9] != 56):
            raise ValueError("expected AArch64 ELF64 tables")
        self.sections = [struct.unpack_from("<IIQQQQIIQQ", data, header[6] + i * 64)
                         for i in range(header[12])]
        self.phdrs = [struct.unpack_from("<IIQQQQQQ", data, header[5] + i * 56)
                      for i in range(header[10])]
        self.symbols = []
        self.symbol_tables = {}
        for index, section in enumerate(self.sections):
            if section[1] != 2:
                continue
            strings = self.section_bytes(self.sections[section[6]])
            symbols = []
            for offset in range(0, section[5], section[9]):
                name, info, _other, shndx, value, size = struct.unpack_from("<IBBHQQ", data, section[4] + offset)
                end = strings.index(b"\0", name)
                symbol = dict(name=strings[name:end].decode(), kind=info & 15,
                              section=shndx, value=value, size=size)
                symbols.append(symbol)
                self.symbols.append(symbol)
            self.symbol_tables[index] = symbols

    def section_bytes(self, section):
        return self.data[section[4]:section[4] + section[5]]

    def tls_symbols(self):
        return [symbol for symbol in self.symbols if symbol["kind"] == 6]

    def tls_relocations(self):
        result = []
        for section in self.sections:
            if section[1] != 4 or not self.sections[section[7]][2] & 4:
                continue
            symbols = self.symbol_tables[section[6]]
            for offset in range(0, section[5], section[9]):
                _site, info, _addend = struct.unpack_from("<QQq", self.data, section[4] + offset)
                if symbols[info >> 32]["kind"] == 6:
                    result.append(info & 0xffffffff)
        return result

    def instructions(self, name):
        symbol = next(s for s in self.symbols if s["name"] == name and s["kind"] == 2)
        section = self.sections[symbol["section"]]
        offset = section[4] + symbol["value"] - section[3]
        return [struct.unpack_from("<I", self.data, i)[0]
                for i in range(offset, offset + symbol["size"], 4)]

    def tp_offset(self, name):
        registers = {}
        for word in self.instructions(name):
            if word & 0xffffffe0 == 0xd53bd040:  # mrs xN, TPIDR_EL0.
                registers[word & 31] = 0
            elif word & 0xff000000 == 0x91000000:  # 64-bit add immediate.
                source = (word >> 5) & 31
                if source in registers:
                    immediate = ((word >> 10) & 0xfff) << (12 if word & (1 << 22) else 0)
                    registers[word & 31] = registers[source] + immediate
        if 0 not in registers:
            raise ValueError(f"{name} did not return a TP-relative address")
        return registers[0]


def sdk_fixture():
    data = bytearray(0x3100)
    data[:16] = b"\x7fELF\x02\x01\x01" + bytes(9)
    struct.pack_into("<HHIQQQIHHHHHH", data, 16, 2, 183, 1, 0x400000, 64, 0x3000, 0, 64, 56, 2, 64, 2, 0)
    struct.pack_into("<II6Q", data, 64, 1, 5, 0x1000, 0x400000, 0x400000, 16, 16, 4096)
    struct.pack_into("<II6Q", data, 120, 1, 6, 0x2000, 0x402000, 0x402000, 16, 16, 4096)
    struct.pack_into("<II4QII2Q", data, 0x3040, 0, 1, 3, 0x402000, 0x2000, 16, 0, 0, 8, 0)
    return data


class ProfileTests(unittest.TestCase):
    def test_local_exec_classification_excludes_pic_and_other_models(self):
        require_local_exec([549, 551, 549, 551])
        for relocations in ([], [549], [551], [562, 563, 564, 569], [549, 551, 541]):
            with self.subTest(relocations=relocations), self.assertRaises(ValueError):
                require_local_exec(relocations)

    def test_production_sdk_still_refuses_tls_headers_and_sections(self):
        checker.check(sdk_fixture(), "sdk")
        data = sdk_fixture()
        struct.pack_into("<H", data, 56, 3)
        struct.pack_into("<I", data, 176, 7)
        with self.assertRaisesRegex(checker.ContractError, "TLS"):
            checker.check(data, "sdk")
        data = sdk_fixture()
        struct.pack_into("<Q", data, 0x3040 + 8, 0x403)
        with self.assertRaisesRegex(checker.ContractError, "TLS"):
            checker.check(data, "sdk")

    def test_elf_inspection_rejects_non_aarch64_headers(self):
        for data in (b"", bytes(64), bytes(sdk_fixture()[:63])):
            with self.subTest(size=len(data)), self.assertRaises(ValueError):
                Elf(data)
        data = sdk_fixture()
        struct.pack_into("<H", data, 18, 62)
        with self.assertRaises(ValueError):
            Elf(data)


def integration(cache, work):
    work.mkdir(parents=True, exist_ok=True)
    with patch.object(sdk.urllib.request, "urlopen", side_effect=AssertionError("TLS probes must be offline")):
        compiler, provenance = sdk.prepare(cache)
    records = []

    def compile_probe(name, root, flags=(), dependencies=(), mode="build-obj", accepted=True, diagnostic=""):
        output = work / name
        command = [
            str(compiler / "zig"), mode, "--zig-lib-dir", str(compiler / "lib"),
            "--cache-dir", str(work / name / "local"), "--global-cache-dir", str(work / name / "global"),
            "-target", sdk.LOCK["target"], "-mcpu", sdk.LOCK["cpu"], "-O", sdk.LOCK["optimize"],
            "-fno-PIE", "-fno-stack-protector", f"-femit-bin={output}.bin",
            *flags,
        ]
        for dependency, _path in dependencies:
            command += ["--dep", dependency]
        command += [f"-Mroot={root}"]
        command += [f"-M{dependency}={path}" for dependency, path in dependencies]
        result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True)
        (work / f"{name}.log").write_text(result.stdout + result.stderr)
        records.append(dict(name=name, command=command, returncode=result.returncode))
        if (result.returncode == 0) != accepted or (not accepted and diagnostic not in result.stderr):
            raise ValueError(f"unexpected {name} result:\n{result.stderr}")
        return Elf(Path(f"{output}.bin").read_bytes()) if accepted else None

    root = ROOT / "tests/zig-thread-tls-probe.zig"
    options = work / "options.zig"
    options.write_text("pub const alignment = 64;\n")
    dependencies = [("tls_options", options)]
    multi = compile_probe("local-exec", root, ["-fno-single-threaded"], dependencies)
    require_local_exec(multi.tls_relocations())
    for name in ("tls_address", "tls_zero_address", "tls_read", "tls_write"):
        if not any(word & 0xffffffe0 == 0xd53bd040 for word in multi.instructions(name)):
            raise ValueError(f"{name}: missing TPIDR_EL0 read")
    pic = compile_probe("pic-negative", root, ["-fno-single-threaded", "-fPIC"], dependencies)
    try:
        require_local_exec(pic.tls_relocations())
    except ValueError:
        pass
    else:
        raise ValueError("PIC unexpectedly has the supported local-exec model")
    if not {562, 563, 564, 569} <= set(pic.tls_relocations()):
        raise ValueError("pinned PIC probe did not emit TLSDESC")

    layout_cases = []
    for alignment in (1, 8, 16, 32, 64, 4096):
        options.write_text(f"pub const alignment = {alignment};\n")
        elf = compile_probe(f"alignment-{alignment}", root,
                            ["-fno-single-threaded", "-fentry=_start"], dependencies, mode="build-exe")
        tls = [p for p in elf.phdrs if p[0] == 7]
        expected_alignment = max(8, alignment)
        zero_offset = (8 + alignment - 1) & -alignment
        if elf.kind != 2 or len(tls) != 1 or tls[0][5:] != (8, zero_offset + 19, expected_alignment):
            raise ValueError(f"unexpected TLS segment for alignment {alignment}: {tls}")
        if any(p[0] in (2, 3) for p in elf.phdrs):
            raise ValueError("static TLS probe unexpectedly needs dynamic linking")
        prefix = (16 + expected_alignment - 1) & -expected_alignment
        if elf.tp_offset("tls_address") != prefix or elf.tp_offset("tls_zero_address") != prefix + zero_offset:
            raise ValueError(f"variant-I offset drift for alignment {alignment}")
        template = elf.data[tls[0][2]:tls[0][2] + tls[0][5]]
        if template != struct.pack("<Q", 0x123456789abcdef0):
            raise ValueError("TLS initialized template differs")
        records[-1]["tls"] = dict(filesz=tls[0][5], memsz=tls[0][6], alignment=tls[0][7],
                                  prefix=prefix, zero_offset=zero_offset)
        layout_cases.append(f".{{ .alignment={expected_alignment}, .memory_size={tls[0][6]}, "
                            f".offset={elf.tp_offset('tls_address')}, .zero_offset={elf.tp_offset('tls_zero_address')} }},")

    core_probe = work / "core.zig"
    core_probe.write_text('const tls=@import("thread_tls"); '
                          'export fn prepare(source:[*]const u8, initialized:usize, memory_size:usize, alignment:usize, '
                          'target:[*]u8, capacity:usize) usize { '
                          'const layout=tls.initialize(source[0..initialized],memory_size,alignment,target[0..capacity]) '
                          'catch return 0; return layout.size; }\n')
    core_dependency = [("thread_tls", ROOT / "kernel/src/thread_tls.zig")]
    compile_probe("core-freestanding", core_probe, ["-fsingle-threaded"], core_dependency)
    contract = work / "layout-contract.zig"
    contract.write_text('''
const std=@import("std");
const tls=@import("thread_tls");
const Case=struct { alignment:usize, memory_size:usize, offset:usize, zero_offset:usize };
const cases=[_]Case{
''' + "\n".join(layout_cases) + '''
};
test "pure TLS core matches the pinned linker's emitted offsets" {
    var buffer:[4 * 4096]u8 align(4096)=undefined;
    const template=[_]u8{0xf0,0xde,0xbc,0x9a,0x78,0x56,0x34,0x12};
    for (cases) |case| {
        @memset(&buffer,0xa5);
        const layout=try tls.initialize(&template,case.memory_size,case.alignment,&buffer);
        try std.testing.expectEqual(case.offset,layout.tls_offset);
        try std.testing.expectEqualSlices(u8,&template,buffer[case.offset..][0..template.len]);
        for (buffer[case.zero_offset..][0..19]) |byte| try std.testing.expectEqual(@as(u8,0),byte);
        for (buffer[layout.size..]) |byte| try std.testing.expectEqual(@as(u8,0xa5),byte);
    }
}
''')
    result = subprocess.run([
        str(compiler / "zig"), "test", "--zig-lib-dir", str(compiler / "lib"), "-O", "ReleaseSafe",
        "--cache-dir", str(work / "contract-local"), "--global-cache-dir", str(work / "contract-global"),
        "--dep", "thread_tls", f"-Mroot={contract}", f"-Mthread_tls={ROOT / 'kernel/src/thread_tls.zig'}",
    ], cwd=ROOT, text=True, capture_output=True)
    (work / "layout-contract.log").write_text(result.stdout + result.stderr)
    if result.returncode:
        raise ValueError(f"TLS core disagrees with pinned link offsets:\n{result.stderr}")

    options.write_text("pub const alignment = 64;\n")
    linker = ["-fentry=_start", "-T", str(ROOT / "tools/zig/guest.ld")]
    single = compile_probe("sdk-single", root, ["-fsingle-threaded", *linker], dependencies, mode="build-exe")
    if single.tls_symbols() or any(s[2] & 0x400 for s in single.sections) or any(p[0] == 7 for p in single.phdrs):
        raise ValueError("single-threaded probe unexpectedly retains TLS")
    checker.check(single.data, "sdk")
    compile_probe("sdk-linker-refusal", root, ["-fno-single-threaded", *linker], dependencies,
                  mode="build-exe", accepted=False, diagnostic="PT_TLS")

    platform = work / "platform.zig"
    platform.write_text('const p=@import("platform"); export fn probe() usize { return @sizeOf(p.posix.fd_t); }\n')
    platform_dependency = [("platform", ROOT / "user/zig/platform.zig")]
    compile_probe("platform-single", platform, ["-fsingle-threaded"], platform_dependency)
    compile_probe("platform-multi-refusal", platform, ["-fno-single-threaded"], platform_dependency,
                  accepted=False, diagnostic="VirelaiSingleThreadedRequired")
    thread = work / "thread.zig"
    thread.write_text('const std=@import("std"); fn child() void {} export fn probe() void { '
                      '_=std.Thread.spawn(.{},child,.{}) catch return; }\n')
    compile_probe("thread-single-refusal", thread, ["-fsingle-threaded"], accepted=False, diagnostic="single-threaded")
    compile_probe("thread-multi-refusal", thread, ["-fno-single-threaded"], accepted=False,
                  diagnostic="Unsupported operating system freestanding")
    threaded = work / "threaded.zig"
    threaded.write_text('const std=@import("std"); pub const virelai=@import("platform"); '
                        'export fn probe() void { var t:std.Io.Threaded=undefined; _=&t; }\n')
    compile_probe("io-threaded-refusal", threaded, ["-fsingle-threaded"], platform_dependency,
                  accepted=False, diagnostic="VirelaiUnsupportedThreaded")
    provenance["probes"] = records
    (work / "receipt.json").write_text(json.dumps(provenance, indent=2, sort_keys=True) + "\n")
    print(f"static-TLS preparation: {len(records)} pinned compile probes passed; no guest execution")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--integration", action="store_true")
    parser.add_argument("--cache", type=Path, default=sdk.DEFAULT_CACHE)
    parser.add_argument("--work", type=Path, default=ROOT / "artifacts/thread-tls")
    args = parser.parse_args()
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ProfileTests))
    if not result.wasSuccessful():
        sys.exit(1)
    if args.integration:
        integration(args.cache.resolve(), args.work.resolve())
