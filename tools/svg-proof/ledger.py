"""ADR 0041 §5.1: source-backed accounting paired with M90d, never MemStats."""
import json
import re
from pathlib import Path

from artifacts import OUT, ROOT, sha, verify


def pair(mode, serial, evidence):
    report = verify()
    name = "CONSUMER.ELF" if mode == "consumer" else "SVG.ELF"
    matches = re.findall(
        rf"runtime-receipt: pid=(\d+) name={re.escape(name)} peak_pages=(\d+) "
        r"page_cap=(\d+) peak_regions=(\d+) region_cap=(\d+) static_pages=(\d+)",
        serial)
    if len(matches) != 1:
        raise ValueError("M90d final per-process runtime receipt missing/unverifiable")
    pid, pages, page_cap, regions, region_cap, static = map(int, matches[0])
    if page_cap != 4096 or region_cap != 16 or not (2048 <= pages <= 3584 and 1 <= regions <= 12):
        raise ValueError("arena+runtime peak exceeds §5.1 bounds")
    elf = report["consumer"] if mode == "consumer" else report["full"]
    expected_static = sum((p["memsz"]+4095)//4096 for p in elf["loads"])
    if static != expected_static:
        raise ValueError("static ELF segment receipt does not match page-rounded PT_LOAD")
    # The fork's sbrk mapping/touch audit identifies all runtime allocation
    # paths (including L2 metadata absent from MemStats). The kernel receipt
    # supplies the actual high-water, not virtual heap or ELF reservations.
    sources = {}
    required = {
        "tools/go/overlay/runtime/os_virelai.go": ["virMmap(unsafe.Pointer(blocMax), bl+n-blocMax)", "blocMax = bl + n"],
        "user/go/vi/vi_guest.go": ["MapAnonymous|MapPrivate|MapPopulate"],
    }
    for name, terms in required.items():
        p = ROOT / name
        text = p.read_text()
        if not all(term in text for term in terms):
            raise ValueError("runtime/arena source audit drift")
        sources[name] = sha(p.read_bytes())
    if "munmap" in (ROOT / "tools/go/overlay/runtime/os_virelai.go").read_text().lower():
        raise ValueError("runtime mapping reclamation semantics changed; subtraction unproved")
    runtime_pins = report["fork_runtime_sha256"]
    if not all("src/runtime/"+name in runtime_pins for name in ("mem_sbrk.go", "malloc.go", "mstats.go", "os_virelai.go")):
        raise ValueError("missing pinned runtime mapping/touch audit")
    audit = report["mapping_touch_audit"]
    if audit["renderer_arena"]["populated_pages"] != 2048 or not audit["alignment_hazard"]["padding_is_touched"]:
        raise ValueError("missing conservative mapping/touch source audit")
    # Populated arena is retained until process exit. sbrk mappings/free
    # spans stay recorded (sysUnusedOS no-op; no runtime munmap), so startup
    # pages/regions cannot disappear before arena admission. Subtraction
    # therefore bounds *all* runtime, collector, stack and wrapper use.
    runtime_pages, runtime_regions = pages-2048, regions-1
    if runtime_pages > 1536 or runtime_regions > 11:
        raise ValueError("runtime margin exceeded")
    allocations = re.findall(r"pages: armed=1 total=(0x[0-9a-f]+) free=(0x[0-9a-f]+)", serial)
    if len(allocations) != 2 or allocations[0] != allocations[1]:
        raise ValueError("post-exit physical allocation not reclaimed exactly")
    if serial.rfind("tasks user-exec reaped") >= serial.rfind("pages: armed=1"):
        raise ValueError("reclamation sample preceded final reap")
    exit_marker = "procs "+name+" exited status=0"
    if exit_marker not in serial or serial.index(exit_marker) >= serial.index("runtime-receipt: pid="):
        raise ValueError("receipt not paired with successful final process exit")
    freq = re.search(r"timer: armed=1 .*? freq=(0x[0-9a-f]+)", serial)
    if not freq or int(freq[1], 16) == 0:
        raise ValueError("counter frequency missing")
    result = {"observed": True, "pid": pid, "name": name, "mode": mode,
              "arena": {"bytes": 8388608, "pages": 2048, "regions": 1, "populated": True},
              "peak_total": {"pages": pages, "bytes": pages*4096, "regions": regions},
              "runtime_upper_bound": {"pages": runtime_pages, "bytes": runtime_pages*4096, "regions": runtime_regions},
              "static_elf_pages": static, "kernel_caps": {"pages": page_cap, "regions": region_cap},
              "counter": {"frequency_hz": int(freq[1], 16), "resolution_ns_numerator": 1000000000,
                          "resolution_ns_denominator": int(freq[1], 16), "timing": "vi.Nanos integer ns"},
              "source_sha256": sources, "fork_runtime_sha256": runtime_pins,
              "mapping_touch_audit": audit,
              "physical_pages_before_after": allocations,
              "audit": ["sysAllocOS/sysReserveOS -> memAlloc/sbrk -> virMmap slot 63",
                        "sysReserveAlignedSbrk alignment padding is touched by byte zeroing",
                        "heap L2 metadata sysAllocOS is included by kernel, not HeapAlloc",
                        "runtime sysUnusedOS retains backing; occupied sbrk regions retained",
                        "one retained MAP_POPULATE arena; all remaining dynamic backing charged to runtime"]}
    Path(evidence, "runtime-ledger.json").write_text(json.dumps(result, indent=2)+"\n")
    return result
