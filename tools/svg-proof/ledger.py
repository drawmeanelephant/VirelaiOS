"""ADR 0041 §5.1: source-backed accounting paired with M90d, never MemStats."""
import json
import re
from pathlib import Path

from artifacts import OUT, ROOT, sha, verify


def pair(mode, serial, evidence):
    report = verify()
    name = "CONSUMER.ELF" if mode == "consumer" else "SVG.ELF"
    matches = list(re.finditer(
        rf"runtime-receipt: pid=(\d+) name={re.escape(name)} peak_pages=(\d+) "
        r"page_cap=(\d+) peak_regions=(\d+) region_cap=(\d+) static_pages=(\d+) "
        r"page_tracking=extensible page_saturated=(\d+) total_pages=(\d+) "
        r"record_failures=(\d+) unrecorded_pages=(\d+) reaped=(\d+)(?=\r?\n|$)",
        serial))
    if len(matches) != 1:
        raise ValueError("M90d final per-process runtime receipt missing/unverifiable")
    receipt = matches[0]
    pid, pages, page_cap, regions, region_cap, static, saturated, total, failures, unrecorded, reaped = map(int, receipt.groups())
    violations = []
    if page_cap != 4096 or region_cap != 16 or not (0 < pages <= total):
        violations.append("inconsistent kernel page/region accounting")
    if saturated != int(pages >= page_cap) or failures != 0 or unrecorded != 0 or reaped != 1:
        violations.append("incomplete page tracking or final reap")
    if not (2048 <= pages <= 3584 and 1 <= regions <= 12):
        violations.append("arena+runtime peak exceeds §5.1 bounds")
    elf = report["consumer"] if mode == "consumer" else report["full"]
    expected_static = sum((p["memsz"]+4095)//4096 for p in elf["loads"])
    if static != expected_static:
        violations.append("static ELF segment receipt does not match page-rounded PT_LOAD")
    # The fork's sbrk mapping/touch audit identifies all runtime allocation
    # paths (including L2 metadata absent from MemStats). The kernel receipt
    # supplies the actual high-water, not virtual heap or ELF reservations.
    sources = {}
    required = {
        "tools/go/overlay/runtime/os_virelai.go": ["virMmap(unsafe.Pointer(blocMax), bl+n-blocMax)", "blocMax = bl + n"],
        "user/go/vi/vi_guest.go": ["MapAnonymous|MapPrivate|MapPopulate"],
    }
    for source_name, terms in required.items():
        p = ROOT / source_name
        text = p.read_text()
        if not all(term in text for term in terms):
            raise ValueError("runtime/arena source audit drift")
        sources[source_name] = sha(p.read_bytes())
    if "munmap" in (ROOT / "tools/go/overlay/runtime/os_virelai.go").read_text().lower():
        raise ValueError("runtime mapping reclamation semantics changed; subtraction unproved")
    runtime_pins = report["fork_runtime_sha256"]
    if not all("src/runtime/"+source_name in runtime_pins for source_name in ("mem_sbrk.go", "malloc.go", "mstats.go", "os_virelai.go")):
        raise ValueError("missing pinned runtime mapping/touch audit")
    audit = report["mapping_touch_audit"]
    if audit["renderer_arena"]["populated_pages"] != 2048 or not audit["alignment_hazard"]["padding_is_touched"]:
        raise ValueError("missing conservative mapping/touch source audit")
    # Populated arena is retained until process exit. sbrk mappings/free
    # spans stay recorded (sysUnusedOS no-op; no runtime munmap), so startup
    # pages/regions cannot disappear before arena admission. Subtraction
    # therefore bounds *all* runtime, collector, stack and wrapper use.
    # Static ELF pages are separate and never subtracted from peak_pages.
    runtime_pages, runtime_regions = pages-2048, regions-1
    if runtime_pages > 1536 or runtime_regions > 11:
        violations.append("runtime margin exceeded")
    allocations = list(re.finditer(r"pages: armed=1 total=(0x[0-9a-f]+) free=(0x[0-9a-f]+)", serial))
    counts = [tuple(int(n, 16) for n in sample.groups()) for sample in allocations]
    if len(counts) != 2 or counts[0] != counts[1]:
        violations.append("post-exit physical allocation not reclaimed exactly")
    exec_pos = serial.find("exec: loaded "+name)
    reap_pos = serial.rfind("tasks user-exec reaped")
    if (len(allocations) != 2 or exec_pos < 0 or allocations[0].start() >= exec_pos or
            reap_pos < exec_pos or allocations[-1].start() <= reap_pos or receipt.start() <= reap_pos):
        violations.append("allocation samples/receipt not paired with pre-exec and final reap")
    exit_marker = "procs "+name+" exited status=0"
    exit_pos = serial.find(exit_marker)
    if exit_pos < exec_pos or exit_pos < 0 or exit_pos >= receipt.start():
        violations.append("receipt not paired with successful final process exit")
    freq = re.search(r"timer: armed=1 .*? freq=(0x[0-9a-f]+)", serial)
    if not freq or int(freq[1], 16) == 0:
        raise ValueError("counter frequency missing")
    result = {"observed": True, "status": "failed" if violations else "passed",
              "violations": violations, "pid": pid, "name": name, "mode": mode,
              "arena": {"bytes": 8388608, "pages": 2048, "regions": 1, "populated": True},
              "peak_total": {"pages": pages, "bytes": pages*4096, "regions": regions},
              "runtime_upper_bound": {"pages": runtime_pages, "bytes": runtime_pages*4096, "regions": runtime_regions},
              "static_elf_pages": static, "expected_static_elf_pages": expected_static,
              "page_tracking": {"mode": "extensible", "inline_page_capacity": page_cap,
                                "saturated": saturated, "total_pages": total,
                                "record_failures": failures, "unrecorded_pages": unrecorded, "reaped": reaped},
              "kernel_region_cap": region_cap,
              "counter": {"frequency_hz": int(freq[1], 16), "resolution_ns_numerator": 1000000000,
                          "resolution_ns_denominator": int(freq[1], 16), "timing": "vi.Nanos integer ns"},
              "source_sha256": sources, "fork_runtime_sha256": runtime_pins,
              "mapping_touch_audit": audit,
              "physical_pages_before_after": [{"total": count[0], "free": count[1]} for count in counts],
              "audit": ["sysAllocOS/sysReserveOS -> memAlloc/sbrk -> virMmap slot 63",
                        "sysReserveAlignedSbrk alignment padding is touched by byte zeroing",
                        "heap L2 metadata sysAllocOS is included by kernel, not HeapAlloc",
                        "runtime sysUnusedOS retains backing; occupied sbrk regions retained",
                        "one retained MAP_POPULATE arena; all remaining dynamic backing charged to runtime"]}
    Path(evidence, "runtime-ledger.json").write_text(json.dumps(result, indent=2)+"\n")
    if violations:
        raise ValueError("; ".join(violations))
    return result
