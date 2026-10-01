#!/usr/bin/env python3
"""Check an ELF against an explicit VirelaiOS artifact profile.

`zc` (default) preserves the legacy <=512 KiB contiguous, <=2-load shape.
It is NOT the current kernel's general loader limit. `sdk` is ADR 0038's
static ELF64 two-segment gap shape, <=8 MiB file / <=16 MiB mapped with
the rounded argument/environment tail. tools/zig tests cross-check this
narrower profile against the real kernel elf.parse_head implementation.
"""
import argparse
import struct
import sys
from pathlib import Path

TEXT_BASE = 0x00400000
LOAD_MAX = 512 * 1024
SDK_FILE_MAX = 8 * 1024 * 1024
SDK_MAP_MAX = 16 * 1024 * 1024
GAP_BASE_MAX = 0x10000000
ARG_ENV_BYTES = 4096
HEADER_WINDOW = 16 * 1024  # exec.header_window, including streamed SDK files.
EM_AARCH64 = 0xB7
PT_LOAD = 1


class ContractError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise ContractError(message)


def aligned(value, alignment=4096):
    return (value + alignment - 1) & -alignment


def check(buf, profile="zc"):
    require(len(buf) >= 16 and buf[:4] == b"\x7fELF", "not an ELF")
    cls = buf[4]
    require(cls in (1, 2), "unsupported class")
    require(buf[5] == 1, "unsupported endian (need little)")
    header_size = 52 if cls == 1 else 64
    require(len(buf) >= header_size, "truncated ELF header")
    require(struct.unpack_from("<H", buf, 18)[0] == EM_AARCH64, "unsupported machine")
    if cls == 1:
        entry, phoff = struct.unpack_from("<II", buf, 24)
        phentsize, phnum = struct.unpack_from("<HH", buf, 42)
    else:
        entry, phoff = struct.unpack_from("<QQ", buf, 24)
        phentsize, phnum = struct.unpack_from("<HH", buf, 54)
    require(phentsize == (32 if cls == 1 else 56), "bad phentsize")
    require(phoff <= len(buf) and phnum * phentsize <= len(buf) - phoff, "truncated program headers")
    segs = []
    for i in range(phnum):
        rec = phoff + i * phentsize
        kind = struct.unpack_from("<I", buf, rec)[0]
        if profile == "sdk":
            require(kind not in (2, 3, 7), "dynamic interpreter, dynamic segment or TLS")
            if kind == 0x6474E551:
                require(struct.unpack_from("<I", buf, rec + 4)[0] & 1 == 0, "executable stack")
        if kind != PT_LOAD:
            continue
        if cls == 1:
            off, va, _pa, fs, ms, fl, alignment = struct.unpack_from("<7I", buf, rec + 4)
        else:
            fl, off, va, _pa, fs, ms, alignment = struct.unpack_from("<I6Q", buf, rec + 4)
        segs.append(dict(flags=fl, offset=off, vaddr=va, filesz=fs, memsz=ms, alignment=alignment))
    require(0 < len(segs) <= 2, "need one or two PT_LOAD segments")
    s0 = segs[0]
    require(not s0["flags"] & 2, "segment 0 is writable (W^X)")
    prev_end = 0
    for s in segs:
        require(s["filesz"] <= s["memsz"], "p_memsz < p_filesz")
        require(s["offset"] <= len(buf) and s["filesz"] <= len(buf) - s["offset"], "segment file range escapes file")
        require(s["offset"] >= prev_end, "overlapping or unordered file ranges")
        prev_end = s["offset"] + s["filesz"]
        require(s["vaddr"] + s["memsz"] < 1 << 64, "virtual range overflow")
    require(s0["vaddr"] == TEXT_BASE, "bad text base")
    require(s0["vaddr"] <= entry < s0["vaddr"] + s0["filesz"], "entry outside initialized text")
    if profile == "zc":
        require(sum(s["memsz"] for s in segs) <= LOAD_MAX, "legacy zc load budget")
        if len(segs) == 2:
            require(segs[1]["flags"] & 2, "segment 1 is not writable")
            require(segs[1]["vaddr"] == TEXT_BASE + s0["memsz"], "zc requires contiguous data")
        return dict(segments=segs, entry=entry, file_bytes=len(buf))

    require(profile == "sdk", "unknown profile")
    require(phoff + phnum * phentsize <= HEADER_WINDOW, "program headers escape loader window")
    require(cls == 2 and buf[6] == 1, "SDK requires ELF64 version 1")
    require(struct.unpack_from("<HHI", buf, 16) == (2, EM_AARCH64, 1), "SDK requires static ET_EXEC")
    require(struct.unpack_from("<H", buf, 52)[0] == 64, "bad ELF64 header size")
    require(len(segs) == 2, "SDK requires exactly two loads")
    s1 = segs[1]
    require(s0["flags"] == 5 and s1["flags"] == 6, "SDK requires R+X / R+W (no W+X)")
    require(s1["memsz"] > 0, "SDK requires writable startup state")
    require(s1["vaddr"] == aligned(TEXT_BASE + s0["memsz"]) + 4096, "SDK requires one-page gap after rounded text")
    for s in segs:
        alignment = s["alignment"]
        require(alignment >= 4096 and alignment & (alignment - 1) == 0, "bad load alignment")
        require(s["offset"] % alignment == s["vaddr"] % alignment, "load offset/address incongruence")
    tail_bytes = aligned(s1["memsz"], 8) + ARG_ENV_BYTES
    mapped = aligned(s0["memsz"]) + aligned(tail_bytes)
    require(len(buf) <= SDK_FILE_MAX and mapped <= SDK_MAP_MAX, "ImageBudget")
    require(s1["vaddr"] + aligned(tail_bytes) <= GAP_BASE_MAX, "argument tail escapes loader aperture")
    _check_sections(buf)
    return dict(segments=segs, entry=entry, file_bytes=len(buf), mapped_bytes=mapped, argument_tail_bytes=ARG_ENV_BYTES)


def _check_sections(buf):
    shoff = struct.unpack_from("<Q", buf, 40)[0]
    shentsize, shnum = struct.unpack_from("<HH", buf, 58)
    require(shnum > 0 and shentsize == 64 and shoff <= len(buf) and shnum * 64 <= len(buf) - shoff,
            "SDK requires inspectable section headers")
    for i in range(shnum):
        _name, kind, flags, _addr, off, size, _link, _info, _align, entsize = struct.unpack_from("<II4QII2Q", buf, shoff + i * 64)
        require(kind == 8 or (off <= len(buf) and size <= len(buf) - off), "section escapes file")
        require(not flags & 0x400, "TLS section")
        require(kind not in (4, 9, 19) or size == 0, "relocations require runtime support")
        require(kind not in (6, 11), "dynamic section/symbol table")
        if kind == 2:
            require(entsize == 24 and size % 24 == 0, "bad symbol table")
            for pos in range(off + 24, off + size, 24):
                _name, info, _other, section = struct.unpack_from("<IBBH", buf, pos)
                require(not (section == 0 and info >> 4 in (1, 2)), "unresolved symbol")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", choices=("zc", "sdk"), default="zc")
    parser.add_argument("elf", nargs="?", default="a.out")
    args = parser.parse_args()
    try:
        result = check(Path(args.elf).read_bytes(), args.profile)
        for s in result["segments"]:
            print(f"PT_LOAD off={s['offset']:#x} va={s['vaddr']:#x} filesz={s['filesz']:#x} memsz={s['memsz']:#x} flags={s['flags']:#x}")
        print(f"entry={result['entry']:#x} file={result['file_bytes']} B" +
              (f" mapped+args={result['mapped_bytes']} B" if args.profile == "sdk" else ""))
        print("CONTRACT OK")
    except (OSError, ContractError, struct.error) as error:
        parser.exit(1, f"CONTRACT FAIL: {error}\n")


if __name__ == "__main__":
    main()
