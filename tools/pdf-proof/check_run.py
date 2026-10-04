"""Independent VZ receipts/pixel checks; no guest claims substitute for peaks."""
import json
import os
import re
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from compare import compare_files
from corpus import ROOT


def check(run, serial_path, tag, share):
    context = json.loads((run/"pdf-context.json").read_text())
    if context.get("mode") != "final-acceptance" or not context.get("M90f_merge"):
        raise ValueError("nonfinal diagnostic is not acceptance evidence")
    rows = context["plans"][tag]
    evidence = ROOT/"artifacts/m89-acceptance"/tag
    evidence.mkdir(parents=True, exist_ok=True)
    serial = serial_path.read_text(errors="replace")
    shutil.copyfile(serial_path, evidence/"serial.log")
    frequency = re.findall(r"interrupts: [^\r\n]* freq=(0x[0-9a-f]+)", serial)
    if len(frequency) != 1 or int(frequency[0], 16) == 0:
        raise ValueError("missing guest counter frequency/resolution")
    frequency_hz = int(frequency[0], 16)
    for marker in ("[EXC] parking:", "fatal error:", "panic:", "pdf-proof: FAIL"):
        if marker in serial:
            raise ValueError("unexpected exception/failure: "+marker)
    if serial.count("pdf-proof: complete\n") != 1 or serial.count("procs PDFPROOF.ELF exited status=0") != 1:
        raise ValueError("missing completion/exit or unexpected exits")
    if re.search(r"procs PDFPROOF\.ELF exited status=(?!0\b)\d+", serial):
        raise ValueError("unexpected exit")
    raw = (share/"PDF"/(tag+".receipt")).read_bytes()
    shutil.copyfile(share/"PDF"/(tag+".receipt"), evidence/"receipt.tsv")
    if len(raw) > 16384:
        raise ValueError("ReceiptLimit")
    lines = raw.decode("ascii").splitlines()
    cycles = 100 if tag == "runtime" else 1
    if lines[-1] != f"cycles\t{cycles}" or len(lines) != len(rows)+1:
        raise ValueError("receipt count/cycles")
    receipts = re.findall(r"runtime-receipt: pid=(\d+) name=PDFPROOF\.ELF ([^\r\n]+)", serial)
    if len(receipts) != (2 if tag == "runtime" else 1):
        raise ValueError("missing high-water receipts")
    peaks = []
    for pid, fields in receipts:
        values = {k: int(v) for k, v in re.findall(r"(\w+)=(\d+)", fields)}
        for key in ("peak_pages", "peak_regions", "static_pages", "page_cap", "region_cap"):
            if key not in values:
                raise ValueError("missing kernel counter: "+key)
        if not (2304 <= values["peak_pages"] <= 3072 and 1 <= values["peak_regions"] <= 12):
            raise ValueError("MemoryLimit: kernel high-water pages/regions")
        if values["peak_pages"]-2304 > 768 or values["peak_regions"]-1 > 11:
            raise ValueError("MemoryLimit: runtime partition")
        if values["static_pages"] != context["elf"]["static_pages"]:
            raise ValueError("static ELF pages mismatch (never subtracted)")
        if values.get("record_failures", 0) or values.get("unrecorded_pages", 0):
            raise ValueError("unprovable saturated recording")
        peaks.append(values)
    if tag == "runtime":
        if any(peaks[0][key] != peaks[-1][key] for key in ("peak_pages", "peak_regions")):
            raise ValueError("100-cycle retained backing/region growth")
    free = re.findall(r"pages: armed=1 total=0x[0-9a-f]+ free=(0x[0-9a-f]+)", serial)
    if len(free) != 2 or free[0] != free[1]:
        raise ValueError("post-exit free pool did not return to pre-run level")
    checked = []
    for line, row in zip(lines, rows):
        parts = line.split("\t")
        if len(parts) != 13 or parts[0] != row["id"]:
            raise ValueError("missing/misordered per-page receipt")
        code, repeats, ns, work, reads, rewinds, expansion, vector, arena_pages, arena_regions, handles, decoders = map(int, parts[1:])
        if code != context["codes"][row["expected"]] or repeats != row["repeats"]:
            raise ValueError("incorrect named refusal/repeats: "+row["id"])
        if not (0 < ns <= 5000000000 and 0 < work <= 256000000 and reads <= 134217728 and rewinds <= 32 and expansion <= 16777216 and vector <= 64000000):
            raise ValueError("missing/exceeded time/work/read/expansion maximum")
        if (arena_pages, arena_regions, handles, decoders) != (2304, 1, 0, 0):
            raise ValueError("arena/handle/decoder reuse")
        measured = {"id": row["id"], "code": row["expected"], "max_ns": ns, "max_work": work,
                    "max_reads": reads, "max_rewinds": rewinds, "max_expansion": expansion,
                    "max_vector_work": vector, "memory_upper_pages": peaks[-1]["peak_pages"],
                    "runtime_upper_pages": peaks[-1]["peak_pages"]-2304,
                    "region_upper": peaks[-1]["peak_regions"], "static_pages": peaks[-1]["static_pages"]}
        if row["output"] != "-":
            src = share/row["output"]
            dest = evidence/(row["id"]+".bgra")
            shutil.copyfile(src, dest)
            reference = ROOT/"artifacts/m89-acceptance/reference"/(row["reference"]["id"]+".bgra")
            measured["comparison"] = compare_files(reference, dest, row["reference"])
        checked.append(measured)
    (evidence/"checked.json").write_text(json.dumps({"pages": checked, "peaks": peaks,
                                                   "free_before": free[0], "free_after": free[1],
                                                   "counter_frequency_hz": frequency_hz,
                                                   "counter_resolution_ns": {"numerator": 1000000000, "denominator": frequency_hz}}, indent=2)+"\n")
    print(tag+": pixels, named refusals, maxima, runtime bounds and reclamation verified")


if __name__ == "__main__":
    check(Path(os.environ["RUN_DIR"]), Path(os.environ["VG_SER"]), os.environ["VG_TAG"], Path(os.environ["VG_SHARE"]))
