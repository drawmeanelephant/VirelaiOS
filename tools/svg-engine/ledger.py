"""Fail-closed source audit for ADR 0041 §5.1. Not a guest measurement.

No runtime edit, linkname to private state, or guessed MemStats-to-pages
conversion. A missing upper bound is a blocker, not an acceptance receipt.
"""
import hashlib
import json
import sys
from pathlib import Path


def audit(root, fork):
    inputs = {
        "runtime/mem_sbrk.go": fork / "src/runtime/mem_sbrk.go",
        "runtime/malloc.go": fork / "src/runtime/malloc.go",
        "runtime/os_virelai.go": fork / "src/runtime/os_virelai.go",
        "runtime/sys_virelai_arm64.s": fork / "src/runtime/sys_virelai_arm64.s",
        "runtime/mstats.go": fork / "src/runtime/mstats.go",
        "kernel/src/process.zig": root / "kernel/src/process.zig",
        "kernel/src/exceptions.zig": root / "kernel/src/exceptions.zig",
        "kernel/src/syscall.zig": root / "kernel/src/syscall.zig",
        "user/go/vi/vi_guest.go": root / "user/go/vi/vi_guest.go",
    }
    src = {name: path.read_text() for name, path in inputs.items()}
    required = {
        "runtime/mem_sbrk.go": [
            "const isSbrkPlatform = true", "func sysReserveAlignedSbrk(",
            "p = alignUp(bloc, align)", "memFree(r, l)",
            "func memFree(ap unsafe.Pointer, n uintptr) {\n\tmemFreeWithClear(ap, n, true)\n}",
            "func memFreeWithClear(ap unsafe.Pointer, n uintptr, clear bool) {\n"
            "\tn = memRound(n)\n\tif clear {\n\t\tmemclrNoHeapPointers(ap, n)\n\t}",
            'if GOOS == "virelai" {\n'
            "\t\t\t\t// sbrk returns zero bytes, including a cleared shrink.\n"
            "\t\t\t\tmemFreeWithClear(r, l, false)\n"
            "\t\t\t} else {\n\t\t\t\tmemFree(r, l)\n\t\t\t}",
            "r := sbrk(p + size - bloc)",
            "memFree(base, startLen)", "memFree(unsafe.Pointer(end), endLen)",
            "memclrNoHeapPointers(v, n)\n\t\t\tbloc -= n",
            "bloc = memRound(firstmoduledata.end)\n\tblocMax = bloc",
            "*p = memHdr{}\n\t\t\treturn unsafe.Pointer(p)",
            "func sysUnusedOS(v unsafe.Pointer, n uintptr) {\n}",
        ],
        "runtime/malloc.go": [
            "n = alignUp(n, heapArenaBytes)",
            "v, size = sysReserveAligned(nil, n, heapArenaBytes",
            "logHeapArenaBytes = (6+20)*",
            "sysAllocOS(unsafe.Sizeof(*l2)",
        ],
        "runtime/os_virelai.go": [
            "initBloc()\n\tinitBlocFloor()",
            "virArgBlockBytes = 8 * 256", "virEnvBlockBytes = 16 * 128",
            "memRound(virArgvBlockBase + virArgBlockBytes + virEnvBlockBytes)",
            "bloc = f\n\t\tblocMax = f",
            "n = memRound(n)", "if bl+n > blocMax", "blocMax = bl + n",
            "virMmap(unsafe.Pointer(blocMax), bl+n-blocMax)",
        ],
        "runtime/sys_virelai_arm64.s": [
            "#define VIR_MAP_ANON      0x20",
            "MOVD\t$VIR_MAP_ANON, R3\n\tMOVD\t$VIR_SYS_MMAP, R8\n\tSVC",
        ],
        "kernel/src/process.zig": [
            "pub const max_dynamic_pages: usize = 4096;",
            "pub const max_mmap_regions: usize = 16;",
            "space.mmap_region_count += 1;",
        ],
        "kernel/src/exceptions.zig": [
            "@memset(pa_ptr[0..4096], 0);",
            "zero_phys_page(pa);\n    if (!process.record_dynamic_page(pid, pa))",
        ],
        "kernel/src/syscall.zig": [
            "if (process.mmap_collides(pid, va, aligned_len)) return error_result(.einval);",
        ],
        "user/go/vi/vi_guest.go": ["MapAnonymous|MapPrivate|MapPopulate"],
    }
    for name, terms in required.items():
        for term in terms:
            if term not in src[name]:
                raise ValueError("audit source drift: " + name + ": " + term)
    partitions = [1_048_576, 4_194_304, 1_048_576, 524_288, 524_288, 1_048_576]
    return {
        "status": "blocked", "observed_guest_run": False,
        "source_sha256": {name: hashlib.sha256(path.read_bytes()).hexdigest()
                          for name, path in inputs.items()},
        "renderer_arena": {"partitions": partitions, "bytes": sum(partitions),
                           "populated_pages": sum(partitions)//4096, "regions": 1},
        "required_runtime_upper_bound": {"pages": 1536, "bytes": 6_291_456, "regions": 11},
        "required_total_upper_bound": {"pages": 3584, "bytes": 14_680_064, "regions": 12},
        "kernel_tracking": {"inline_page_capacity": 4096, "region_cap": 16,
                            "page_limit": "available physical memory, not the inline capacity"},
        "audited_runtime_paths": [
            "sysAllocOS and sysReserveOS -> memAlloc/sbrk -> virMmap(slot 63)",
            "sysReserveAlignedSbrk -> memFreeWithClear(fresh zero padding, false) on virelai; memFree(r, l) still clears on plan9/wasm",
            "sysUnusedOS is a no-op; free spans/mapping extents remain retained",
            "each successful growth occupies a new process mmap region",
            "heap L2 index uses sysAllocOS without MemStats accounting",
        ],
        "alignment_hazard": {
            "heap_arena_alignment_bytes": 67_108_864,
            "padding_is_touched": True,
            "fresh_padding_is_cleared": False,
            "touch": "memHdr records only; not a byte sweep or a measured page bound",
            "zero_invariant": "demand-zero growth; sysFreeOS-cleared shrink; page-rounded ELF/argv floor excludes tail slack",
            "warning": "Header writes still fault pages. Ordinary frees/free-list trims still clear; retained runtime backing needs guest receipts.",
        },
        "blocker": "No touched-page and maximum occupied-region upper bound for runtime startup, collector and stack growth is exposed by the permitted SDK. MemStats omits the heap L2 mapping and cannot count retained sbrk growth regions. Separately owned runtime/kernel mapping/touch diagnostics are required before §5.1 acceptance.",
    }


if __name__ == "__main__":
    result = audit(Path(sys.argv[1]), Path(sys.argv[2]))
    print(json.dumps(result, indent=2))
    if result["status"] == "blocked":
        print("svg-engine: runtime demand-page/region proof blocked; separately owned mapping/touch diagnostics required", file=sys.stderr)
    sys.exit(2 if result["status"] == "blocked" else 0)
