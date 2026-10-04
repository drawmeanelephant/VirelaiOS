"""Fail-closed bitmap/work/timing verification shared by every VM boot."""
import json
import os
import shutil
from pathlib import Path

from artifacts import OUT, FIXTURES, verify
from compare import compare
from corpus import ICONS, MAXIMUM, CONSUMERS, materialize


def check(mode, share, serial):
    share = Path(share)
    report = verify()
    evidence = OUT / "vm" / mode
    evidence.mkdir(parents=True, exist_ok=True)
    lines = (share / (mode+".tsv")).read_text().splitlines()
    if lines[:2] != ["schema\t1", "arena\t8388608\t2048\t1"]:
        raise ValueError("missing schema/arena receipt")
    shutil.copyfile(share / (mode+".tsv"), evidence / (mode+".tsv"))
    renders, negatives, cycles, collectors = {}, [], [], []
    allocations, batches = {}, []
    for line in lines[2:]:
        row = line.split("\t")
        if row[0] == "render":
            if len(row) != 11:
                raise ValueError("render receipt shape")
            name = row[1]
            sample, nanos, work, raster_work, edges, scratch, allocs, width, height = map(int, row[2:])
            limit = 100_000_000 if mode == "icons" else 5_000_000_000
            if not (0 <= nanos <= limit and 0 <= raster_work <= work <= 64_000_000 and
                    0 <= edges <= 8192 and 0 <= scratch <= 524288 and allocs == 0 and width > 0 and height > 0):
                raise ValueError("render budget violation")
            renders.setdefault(name, []).append((sample, nanos, width, height))
        elif row[0] == "negative":
            if len(row) != 5:
                raise ValueError("negative receipt shape")
            negatives.append(row)
        elif row[0] == "cycle":
            i, nanos, work, allocs = map(int, row[1:])
            if not (0 <= nanos <= 100_000_000 and 0 <= work <= 64_000_000 and allocs == 0):
                raise ValueError("reuse budget violation")
            cycles.append(i)
        elif row[0] == "negative-count":
            if int(row[1]) != len(negatives):
                raise ValueError("negative count mismatch")
        elif row[0] == "gc":
            collectors.append(int(row[1]))
        elif row[0] == "allocations":
            if len(row) != 4 or int(row[2]) != 0 or row[1] in allocations:
                raise ValueError("missing/nonzero allocation receipt")
            allocations[row[1]] = int(row[3])
        elif row[0] == "allocation-batch":
            if len(row) != 3 or int(row[2]) != 0:
                raise ValueError("reuse batch allocated")
            batches.append(int(row[1]))
        elif row != ["direct-errors", "1", "0"]:
            raise ValueError("unrecognized receipt row")
    names = {"icons": ICONS+["viewport-meet"], "maximum": MAXIMUM,
             "consumer": ["consumer-"+name for name in CONSUMERS], "reuse": []}[mode]
    if set(renders) != set(names):
        raise ValueError("missing/extra render receipt")
    if mode in ("icons", "maximum") and allocations != {name: 21 if mode == "icons" else 1 for name in names}:
        raise ValueError("missing call-sequence allocation receipt")
    comparisons = {}
    manifest = json.loads((FIXTURES / "manifest.json").read_text())
    for name, rows in renders.items():
        count = 21 if mode == "icons" else 1
        if [r[0] for r in rows] != list(range(count)):
            raise ValueError("missing cold/repeated timing sample")
        bitmap = (share / (name+".bgra")).read_bytes()
        host = (OUT / "host" / (name+".bgra")).read_bytes()
        if bitmap != host:
            raise ValueError("same-algorithm host/guest pixels differ: "+name)
        shutil.copyfile(share / (name+".bgra"), evidence / (name+".bgra"))
        width, height = rows[0][2:]
        stride = 80 if name == "consumer-padding" else width
        if len(bitmap) != stride*height*4:
            raise ValueError("bitmap dimensions/padded stride")
        if name in manifest:
            entry = manifest[name]
            if (width, height) != (entry["width"], entry["height"]):
                raise ValueError("oracle dimension mismatch")
            comparisons[name] = compare(bitmap, (OUT / "references" / (name+".bgra")).read_bytes(), width, height)
        comparisons.setdefault(name, {})["maximum_nanoseconds"] = max(row[1] for row in rows)
    if mode == "reuse":
        expected = (OUT / "negatives/negative.tsv").read_text().splitlines()
        if (len(negatives) != len(expected) or cycles != list(range(100)) or
                collectors != list(range(9, 100, 10)) or batches != list(range(9, 100, 10))):
            raise ValueError("missing exclusion/boundary/reuse receipt")
        mismatches = []
        for row, expected_row in zip(negatives, expected):
            name, code, _ = expected_row.split("\t")
            if row[1:4] != [name, code, code]:
                mismatches.append({"expected": expected_row, "actual": row})
        if mismatches:
            (evidence / "engine-defects.json").write_text(json.dumps(mismatches, indent=2)+"\n")
            raise ValueError("engine exclusion classifications differ: "+str(len(mismatches)))
        # Refusals must not leave a published bitmap or a temporary output.
        if any(share.glob("n[0-9][0-9][0-9].bgra")) or any(share.glob("*.tmp")):
            raise ValueError("outstanding negative output")
    if mode == "consumer" and ["direct-errors", "1", "0"] not in [line.split("\t") for line in lines]:
        raise ValueError("missing direct errors receipt")
    (evidence / "comparison.json").write_text(json.dumps(comparisons, indent=2)+"\n")
    from ledger import pair
    pair(mode, serial, evidence)
    return comparisons


def gate(mode):
    check(mode, os.environ["VG_SHARE"], Path(os.environ["VG_SER"]).read_text(errors="replace"))
    print("SVG pixels, fixed thresholds, receipts, reuse and runtime ledger verified: "+mode)
