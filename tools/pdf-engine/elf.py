"""Static guest ELF and complete engine-added-bytes checks, ADR 0040 §2/§5."""
import json
import struct
import sys
from pathlib import Path

LIMIT = 2_097_152


def inspect(data):
    if len(data) < 64 or data[:7] != b"\x7fELF\x02\x01\x01":
        raise ValueError("InvalidBuffer: not ELF64 LE")
    if struct.unpack_from("<HH", data, 16) != (2, 183):
        raise ValueError("InvalidBuffer: not AArch64 ET_EXEC")
    phoff, shoff = struct.unpack_from("<QQ", data, 32)
    phsize, phnum, shsize, shnum = struct.unpack_from("<HHHH", data, 54)
    if phsize != 56 or phoff + phsize * phnum > len(data):
        raise ValueError("InvalidBuffer: headers")
    loads = []
    for i in range(phnum):
        tag, flags, off, va, _, fs, ms, align = struct.unpack_from("<IIQQQQQQ", data, phoff+i*56)
        if tag in (2, 3):
            raise ValueError("InvalidBuffer: dynamic ELF")
        if tag != 1:
            continue
        if fs > ms or off+fs > len(data) or flags & 3 == 3:
            raise ValueError("InvalidBuffer: load permissions/size")
        if align < 4096 or align & (align-1) or va % align != off % align:
            raise ValueError("InvalidBuffer: alignment")
        if va % 4096 or va+ms >= 0x10000000:
            raise ValueError("InvalidBuffer: loader aperture")
        loads.append({"vaddr": va, "filesz": fs, "memsz": ms, "flags": flags})
    if not loads or not any(p["flags"] & 1 for p in loads):
        raise ValueError("InvalidBuffer: no executable load")
    for a, b in zip(loads, loads[1:]):
        if (a["vaddr"]+a["memsz"]+4095)//4096*4096 > b["vaddr"]:
            raise ValueError("InvalidBuffer: overlapping mapped loads")
    if sum(p["filesz"] for p in loads) > 32*1024*1024 or sum(p["memsz"] for p in loads) > 64*1024*1024:
        raise ValueError("InvalidBuffer: loader size")
    if shnum:
        if shsize != 64 or shoff+shsize*shnum > len(data):
            raise ValueError("InvalidBuffer: sections")
        for i in range(shnum):
            if struct.unpack_from("<I", data, shoff+i*64+4)[0] in (2, 6, 11):
                raise ValueError("InvalidBuffer: symbols/dynamic sections")
    return {"file_bytes": len(data), "loads": loads,
            "load_filesz": sum(p["filesz"] for p in loads),
            "load_memsz": sum(p["memsz"] for p in loads),
            "page_rounded_load_bytes": sum((p["memsz"]+4095)//4096*4096 for p in loads)}


def compare(empty, full, edeps, fdeps):
    e, f = inspect(empty), inspect(full)
    file_delta = f["file_bytes"]-e["file_bytes"]
    initialized_delta = f["load_filesz"]-e["load_filesz"]
    if not 0 <= file_delta <= LIMIT or not 0 <= initialized_delta <= LIMIT:
        raise ValueError("EngineSizeLimit")
    if {"virelai/pdf", "virelai/vector"} & set(edeps.splitlines()):
        raise ValueError("SourceDrift: engine in empty adapter")
    if not {"virelai/pdf", "virelai/vector"}.issubset(fdeps.splitlines()):
        raise ValueError("SourceDrift: incomplete full adapter")
    baseline = set(edeps.splitlines())
    for p in (edeps+fdeps).splitlines():
        if p in ("C", "runtime/cgo", "plugin") or p.startswith("net") or "." in p.split("/")[0]:
            raise ValueError("SourceDrift: forbidden dependency: "+p)
        if p.startswith("virelai/") and p not in ("virelai/pdf", "virelai/vector", "virelai/vi", "virelai/vsys"):
            raise ValueError("SourceDrift: unapproved guest package")
        # SDK encoding/hex brings fmt/os/syscall into go-list's package graph
        # even though the native writer calls only vi. No engine-added OS
        # package or reachable file-shadow symbol is permitted.
        if p in ("os", "syscall") and p not in baseline:
            raise ValueError("SourceDrift: engine-added hosted I/O")
    return {"empty": e, "full": f, "engine_added_bytes": file_delta,
            "initialized_added_bytes": initialized_delta,
            "engine_includes_vector": True, "guest_run": False}


def reachable(symbols):
    for line in symbols.splitlines():
        if any(name in line for name in ("os.(*File).", "syscall.virKeep", "syscall.virPull",
                                        "syscall.Pread", "syscall.Pwrite", "syscall.Seek",
                                        "runtime/cgo", "net/http.", "net.Dial")):
            raise ValueError("SourceDrift: reachable hosted I/O/FFI")


if __name__ == "__main__":
    args = [Path(a) for a in sys.argv[1:]]
    print(json.dumps(compare(args[0].read_bytes(), args[1].read_bytes(),
                             args[2].read_text(), args[3].read_text()), indent=2))
