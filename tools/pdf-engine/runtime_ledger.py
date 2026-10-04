"""Conservative runtime proof recipe. No host value is a guest receipt."""
import hashlib
import json
import sys
from pathlib import Path


def recipe(root, fork):
    paths = {
        "runtime/mem_sbrk.go": fork/"src/runtime/mem_sbrk.go",
        "runtime/malloc.go": fork/"src/runtime/malloc.go",
        "runtime/os_virelai.go": fork/"src/runtime/os_virelai.go",
        "runtime/sys_virelai_arm64.s": fork/"src/runtime/sys_virelai_arm64.s",
        "runtime/mstats.go": fork/"src/runtime/mstats.go",
        "kernel/src/process.zig": root/"kernel/src/process.zig",
        "kernel/src/exceptions.zig": root/"kernel/src/exceptions.zig",
        "kernel/src/syscall.zig": root/"kernel/src/syscall.zig",
        "user/go/vi/vi_guest.go": root/"user/go/vi/vi_guest.go",
    }
    required = {
        "runtime/mem_sbrk.go": (
            "const isSbrkPlatform = true",
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
        ),
        "runtime/malloc.go": ("n = alignUp(n, heapArenaBytes)", "sysAllocOS(unsafe.Sizeof(*l2)"),
        "runtime/os_virelai.go": (
            "initBloc()\n\tinitBlocFloor()",
            "virArgBlockBytes = 8 * 256", "virEnvBlockBytes = 16 * 128",
            "memRound(virArgvBlockBase + virArgBlockBytes + virEnvBlockBytes)",
            "bloc = f\n\t\tblocMax = f",
            "n = memRound(n)", "if bl+n > blocMax", "blocMax = bl + n",
            "virMmap(unsafe.Pointer(blocMax), bl+n-blocMax)",
        ),
        "runtime/sys_virelai_arm64.s": (
            "#define VIR_MAP_ANON      0x20",
            "MOVD\t$VIR_MAP_ANON, R3\n\tMOVD\t$VIR_SYS_MMAP, R8\n\tSVC",
        ),
        "kernel/src/process.zig": ("pub const max_dynamic_pages: usize = 4096;", "pub const max_mmap_regions: usize = 16;"),
        "kernel/src/exceptions.zig": (
            "@memset(pa_ptr[0..4096], 0);",
            "zero_phys_page(pa);\n    if (!process.record_dynamic_page(pid, pa))",
        ),
        "kernel/src/syscall.zig": (
            "if (process.mmap_collides(pid, va, aligned_len)) return error_result(.einval);",
        ),
        "user/go/vi/vi_guest.go": ("MapAnonymous|MapPrivate|MapPopulate",),
    }
    for name, terms in required.items():
        text = paths[name].read_text()
        if not all(term in text for term in terms):
            raise ValueError("SourceDrift: runtime ledger: "+name)
    return {
        "status": "guest_receipts_required", "observed_guest_run": False,
        "source_sha256": {name: hashlib.sha256(p.read_bytes()).hexdigest() for name, p in paths.items()},
        "arena_partitions": [6_291_456, 1_048_576, 524_288, 262_144, 262_144, 524_288, 524_288],
        "arena_bytes": 9_437_184, "arena_populated_pages": 2304, "arena_regions": 1,
        "runtime_upper_bound_required": {"bytes": 3_145_728, "pages": 768, "regions": 11},
        "total_upper_bound_required": {"bytes": 12_582_912, "pages": 3072, "regions": 12},
        "kernel_tracking": {"inline_page_capacity": 4096, "region_cap": 16,
                            "page_limit": "available physical memory, not the inline capacity"},
        "static_load_pages": "report separately; never subtract them from dynamic runtime backing",
        "source_paths": [
            "sysAllocOS/sysReserveOS -> memAlloc/sbrk -> virMmap",
            "sysReserveAlignedSbrk links fresh zero padding with memHdr writes only on virelai; ordinary frees and free-list trims still clear",
            "sysUnusedOS is a no-op, retained heap spans are not reclaimed backing",
            "heap L2 metadata bypasses ordinary MemStats accounting",
            "runtime stacks/collector/sysmon and each retained mmap growth region count",
        ],
        "alignment_padding": {
            "heap_arena_alignment_bytes": 67_108_864,
            "fresh_padding_is_cleared": False, "padding_is_touched": True,
            "touch": "memHdr records only; not a byte sweep or a measured page bound",
            "zero_invariant": "demand-zero growth; sysFreeOS-cleared shrink; page-rounded ELF/argv floor excludes tail slack",
        },
        "receipt_recipe": [
            "Use independently owned M90d kernel high-water dynamic-page/occupied-region receipts.",
            "Capture startup and one arena setup, cold render, 20 timed repeats, forced GC and 100 success/refusal/recovery cycles.",
            "Arena remains one populated 2304-page mapping. For every sample subtract only that proven mapping to bound runtime backing by 768 pages.",
            "Require high-water total <=3072 pages and <=12 regions, including samples during collector/stack growth, not just post-GC HeapAlloc.",
            "Preserve kernel high-water values across refusal/recovery. Assert all file handles closed and dynamic backing reclaimed after process exit.",
            "Account source recheck in the same time/work ledger before publication. Never use os.File/ReadAt shadow transport.",
            "If counters cannot include transient peaks or retained mapping growth, acceptance remains blocked; request a separately owned diagnostic.",
        ],
        "not_evidence": ["HeapAlloc alone", "ELF datapages", "virtual reservations", "256 MiB guest RAM", "host zero-allocation tests"],
    }


if __name__ == "__main__":
    print(json.dumps(recipe(Path(sys.argv[1]), Path(sys.argv[2])), indent=2))
