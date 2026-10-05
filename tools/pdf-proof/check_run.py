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


def parse_receipts(serial, expected_static, runtime=False):
    """M90f page_cap is inline storage, not a ceiling on true backing."""
    rows = re.findall(r"runtime-receipt: pid=(\d+) name=PDFPROOF\.ELF ([^\r\n]+)", serial)
    if runtime and len(rows) == 1 and "runtime-receipt: none" in serial:
        raise ValueError("RuntimeBaselineUnavailable: monitor exposes only final exit receipts; "
                         "100-cycle no-growth bracket remains unproved")
    if len(rows) != (3 if runtime else 1):
        raise ValueError("missing high-water receipts")
    peaks = []
    required = ("peak_pages", "peak_regions", "static_pages", "page_cap", "region_cap",
                "page_saturated", "total_pages", "record_failures", "unrecorded_pages", "reaped")
    for i, (pid, fields) in enumerate(rows):
        values = {k: int(v) for k, v in re.findall(r"(\w+)=(\d+)", fields)}
        if any(key not in values for key in required) or "page_tracking=extensible" not in fields:
            raise ValueError("missing extensible kernel counters")
        if values["page_cap"] != 4096 or values["region_cap"] != 16:
            raise ValueError("kernel receipt contract drift")
        if not 0 < values["peak_pages"] <= values["total_pages"]:
            raise ValueError("inconsistent true backing counters")
        if values["page_saturated"] != int(values["peak_pages"] >= values["page_cap"]):
            raise ValueError("inconsistent saturation counter")
        if values["record_failures"] or values["unrecorded_pages"]:
            raise ValueError("unprovable backing records")
        if values["static_pages"] != expected_static:
            raise ValueError("static ELF pages mismatch (never subtracted)")
        if values["reaped"] != int(i == len(rows)-1):
            raise ValueError("receipt did not bracket final reap")
        values["pid"] = int(pid)
        peaks.append(values)
    if runtime and any(row["pid"] != peaks[0]["pid"] for row in peaks):
        raise ValueError("reuse receipts describe different processes")
    return peaks


def check_pause_order(serial):
    """Both complete live receipt lines must land inside their matching pause."""
    phases = (
        r"^pdf-proof: baseline ns=\d+\r?$",
        r"^runtime-receipt: pid=\d+ name=PDFPROOF\.ELF [^\r\n]+ reaped=0\r?$",
        r"^pdf-proof: resumed\r?$",
        r"^pdf-proof: cycles=50 ns=\d+\r?$",
        r"^runtime-receipt: pid=\d+ name=PDFPROOF\.ELF [^\r\n]+ reaped=0\r?$",
        r"^pdf-proof: resumed cycle=50\r?$",
        r"^pdf-proof: complete\r?$",
        r"^procs PDFPROOF\.ELF exited status=0\r?$",
        r"^runtime-receipt: pid=\d+ name=PDFPROOF\.ELF [^\r\n]+ reaped=1\r?$",
    )
    # Validate exact marker multiplicity separately from the two live rows.
    for pattern in (phases[i] for i in (0, 2, 3, 5, 6, 7, 8)):
        if len(re.findall(pattern, serial, re.M)) != 1:
            raise ValueError("ReceiptPauseOrder: missing/duplicate pause or exit marker")
    position = 0
    for pattern in phases:
        match = re.search(pattern, serial[position:], re.M)
        if match is None:
            raise ValueError("ReceiptPauseOrder: live receipt did not precede resume")
        position += match.end()


def check_memory(peaks):
    if len(peaks) not in (1, 3):
        raise ValueError("missing high-water receipts")
    for values in peaks:
        pages, regions = values["peak_pages"], values["peak_regions"]
        if not (2304 <= pages <= 3072 and 1 <= regions <= 12):
            raise ValueError(f"MemoryLimit: kernel peak_pages={pages}/3072 peak_regions={regions}/12")
        if pages-2304 > 768 or regions-1 > 11:
            raise ValueError("MemoryLimit: runtime partition")
    keys = ("peak_pages", "peak_regions", "total_pages")
    if len(peaks) == 3:
        baseline, warmed, final = peaks
        if any(baseline[key] > warmed[key] for key in keys):
            raise ValueError("baseline exceeds cycle-50 high-water counters")
        if any(warmed[key] != final[key] for key in keys):
            raise ValueError("cycle-50-to-final retained backing/region growth")


def warmup_delta(peaks):
    if len(peaks) != 3:
        return None
    return {key: peaks[1][key]-peaks[0][key]
            for key in ("peak_pages", "total_pages", "peak_regions")}


def check(run, serial_path, tag, share):
    context = json.loads((run/"pdf-context.json").read_text())
    if context.get("mode") != "final-acceptance" or not context.get("M90f_merge"):
        raise ValueError("nonfinal diagnostic is not acceptance evidence")
    rows = context["plans"][tag]
    evidence = ROOT/"artifacts/m89-acceptance/runs"/run.name/tag
    evidence.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(run/"pdf-context.json", evidence.parent/"context.json")
    serial = serial_path.read_text(errors="replace")
    shutil.copyfile(serial_path, evidence/"serial.log")
    if tag == "runtime":
        for name in ("receipt-pause-result.json", "receipt-pause-observer.log"):
            if (run/name).is_file():
                shutil.copyfile(run/name, evidence/name)
    # Preserve observed counters and products even when completion, baseline,
    # memory or comparison validation fails. This is diagnostic, not a pass.
    observed = re.findall(r"runtime-receipt: pid=(\d+) name=PDFPROOF\.ELF ([^\r\n]+)", serial)
    (evidence/"observed-runtime-receipts.json").write_text(json.dumps(
        [{"pid": int(pid), "fields": fields, "validated": False} for pid, fields in observed], indent=2)+"\n")
    for row in rows:
        if row["output"] != "-" and (share/row["output"]).is_file():
            shutil.copyfile(share/row["output"], evidence/(row["id"]+".bgra"))
    receipt_path = share/"PDF"/(tag+".receipt")
    if receipt_path.is_file():
        shutil.copyfile(receipt_path, evidence/"receipt.tsv")
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
    if len(raw) > 16384:
        raise ValueError("ReceiptLimit")
    lines = raw.decode("ascii").splitlines()
    cycles = 100 if tag == "runtime" else 1
    if lines[-1] != f"cycles\t{cycles}" or len(lines) != len(rows)+1:
        raise ValueError("receipt count/cycles")
    peaks = parse_receipts(serial, context["elf"]["static_pages"], tag == "runtime")
    (evidence/"runtime-receipts.json").write_text(json.dumps(peaks, indent=2)+"\n")
    (evidence/"warmup-delta.json").write_text(json.dumps(warmup_delta(peaks), indent=2)+"\n")
    if tag == "runtime":
        check_pause_order(serial)
    check_memory(peaks)
    free = re.findall(r"pages: armed=1 total=0x[0-9a-f]+ free=(0x[0-9a-f]+)", serial)
    if len(free) != 2 or free[0] != free[1]:
        raise ValueError("post-exit free pool did not return to pre-run level")
    checked, failures = [], []
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
            try:
                measured["comparison"] = compare_files(reference, dest, row["reference"])
            except (OSError, ValueError) as exc:
                measured["comparison_failure"] = str(exc)
                failures.append(row["id"]+": "+str(exc))
        checked.append(measured)
    (evidence/"checked.json").write_text(json.dumps({"pages": checked, "peaks": peaks,
                                                   "warmup_delta": warmup_delta(peaks),
                                                   "free_before": free[0], "free_after": free[1],
                                                   "counter_frequency_hz": frequency_hz,
                                                   "counter_resolution_ns": {"numerator": 1000000000, "denominator": frequency_hz}}, indent=2)+"\n")
    if failures:
        raise ValueError("independent comparison failed: "+"; ".join(failures))
    if tag == "runtime":
        print("runtime: baseline/cycle-50/final "+
              " ".join(f"{key}="+"/".join(str(row[key]) for row in peaks)
                       for key in ("peak_pages", "total_pages", "peak_regions"))+
              " warmup_delta="+json.dumps(warmup_delta(peaks), sort_keys=True))
    print(tag+": pixels, named refusals, maxima, runtime bounds and reclamation verified")


if __name__ == "__main__":
    check(Path(os.environ["RUN_DIR"]), Path(os.environ["VG_SER"]), os.environ["VG_TAG"], Path(os.environ["VG_SHARE"]))
