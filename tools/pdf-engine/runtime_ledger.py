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
        "runtime/mstats.go": fork/"src/runtime/mstats.go",
        "kernel/src/process.zig": root/"kernel/src/process.zig",
        "user/go/vi/vi_guest.go": root/"user/go/vi/vi_guest.go",
    }
    required = {
        "runtime/mem_sbrk.go": ("const isSbrkPlatform = true", "memclrNoHeapPointers(ap, n)", "func sysUnusedOS(v unsafe.Pointer, n uintptr) {\n}"),
        "runtime/malloc.go": ("n = alignUp(n, heapArenaBytes)", "sysAllocOS(unsafe.Sizeof(*l2)"),
        "runtime/os_virelai.go": ("virMmap(unsafe.Pointer(blocMax), bl+n-blocMax)",),
        "kernel/src/process.zig": ("pub const max_dynamic_pages: usize = 4096;", "pub const max_mmap_regions: usize = 16;"),
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
        "kernel_hard_cap": {"bytes": 16_777_216, "pages": 4096, "regions": 16},
        "static_load_pages": "report separately; never subtract them from dynamic runtime backing",
        "source_paths": [
            "sysAllocOS/sysReserveOS -> memAlloc/sbrk -> virMmap",
            "sysReserveAlignedSbrk touches alignment padding with memclrNoHeapPointers",
            "sysUnusedOS is a no-op, retained heap spans are not reclaimed backing",
            "heap L2 metadata bypasses ordinary MemStats accounting",
            "runtime stacks/collector/sysmon and each retained mmap growth region count",
        ],
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
