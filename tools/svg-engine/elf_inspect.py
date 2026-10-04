"""Inspect both complete ELFs, never substitute object/source sizes."""
import json
import struct
import sys
from pathlib import Path


def inspect(data):
    if len(data) < 64 or data[:7] != b"\x7fELF\x02\x01\x01":
        raise ValueError("expected ELF64 little-endian")
    kind, machine = struct.unpack_from("<HH", data, 16)
    if (kind, machine) != (2, 183):
        raise ValueError("expected static AArch64 ET_EXEC")
    phoff, shoff = struct.unpack_from("<QQ", data, 32)
    phsize, phnum, shsize, shnum = struct.unpack_from("<HHHH", data, 54)
    if phsize != 56 or phoff + phsize * phnum > len(data):
        raise ValueError("invalid program headers")
    loads = []
    for i in range(phnum):
        tag, flags, offset, va, _, filesz, memsz, align = struct.unpack_from("<IIQQQQQQ", data, phoff + i * phsize)
        if tag in (2, 3):
            raise ValueError("dynamic loader/interpreter refused")
        if tag == 1:
            if filesz > memsz or offset + filesz > len(data) or flags & 3 == 3:
                raise ValueError("invalid/W+X PT_LOAD")
            if align < 4096 or align & (align - 1) or va % align != offset % align:
                raise ValueError("invalid load alignment")
            if va % 4096 or va + memsz >= 0x10000000:
                raise ValueError("outside kernel gap aperture")
            loads.append(dict(vaddr=va, filesz=filesz, memsz=memsz, flags=flags, align=align))
    if not loads or not any(p["flags"] & 1 for p in loads):
        raise ValueError("missing executable segment")
    if sum(p["filesz"] for p in loads) > 32*1024*1024 or sum(p["memsz"] for p in loads) > 64*1024*1024:
        raise ValueError("exceeds kernel loader budget")
    for a, b in zip(loads, loads[1:]):
        if a["vaddr"] + a["memsz"] > b["vaddr"]:
            raise ValueError("overlapping load segments")
    if shnum:
        if shsize != 64 or shoff + shnum * shsize > len(data):
            raise ValueError("invalid section headers")
        for i in range(shnum):
            tag = struct.unpack_from("<I", data, shoff + i * shsize + 4)[0]
            if tag in (2, 6, 11):
                raise ValueError("symbol table or dynamic linking section")
    return dict(size=len(data), loads=loads,
                load_filesz=sum(p["filesz"] for p in loads),
                load_memsz=sum(p["memsz"] for p in loads))


def compare(empty, full, deps, empty_deps="runtime\n"):
    e, f = inspect(empty), inspect(full)
    delta = f["size"] - e["size"]
    if not 0 <= delta <= 1_048_576:
        raise ValueError("renderer-added file bytes exceed ceiling")
    packages = deps.splitlines()
    baseline = empty_deps.splitlines()
    if any(p in ("virelai/svg", "virelai/vector") for p in baseline):
        raise ValueError("empty baseline contains engine")
    if not {"virelai/svg", "virelai/vector"}.issubset(packages):
        raise ValueError("missing engine dependency")
    if any(p == "runtime/cgo" or p == "C" or p.startswith("github.com/") for p in packages + baseline):
        raise ValueError("FFI or third-party engine dependency")
    return dict(empty=e, full=f, renderer_added_bytes=delta,
                compiler="go1.27.1", flags=["CGO_ENABLED=0", "-trimpath", '-ldflags=-s -w'],
                static_no_ffi=True)


if __name__ == "__main__":
    print(json.dumps(compare(Path(sys.argv[1]).read_bytes(), Path(sys.argv[2]).read_bytes(),
                             Path(sys.argv[3]).read_text(), Path(sys.argv[4]).read_text()), indent=2))
