"""Independent byte, timing, stack, receipt and kernel cleanup assertions."""
import json
import os
from pathlib import Path
import re
import shutil
import sys

from receipt import parse, output, results, fields

ROOT = Path(__file__).resolve().parents[2]

def last_scheduler_reap(text):
    matches = list(re.finditer(r"(?m)^tasks [^\r\n]+ reaped\r?$", text))
    return matches[-1].start() if matches else -1


def check():
    rd = Path(os.environ["RUN_DIR"])
    tag = os.environ["VG_TAG"]
    ser = Path(os.environ["VG_SER"]).read_bytes()
    text = ser.decode(errors="replace")
    share = Path(os.environ["VG_SHARE"])
    evidence = ROOT / "artifacts/m88-acceptance"
    evidence.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(os.environ["VG_SER"], evidence / (tag + ".serial"))
    if tag == "repl" and (share / "REPL.R").is_file():
        shutil.copyfile(share / "REPL.R", evidence / "REPL.R")
    assert "[EXC]" not in text and "EngineInvariant" not in text, tag
    assert "fault: QJS.BIN" not in text, (tag, "native runtime fault, not a named refusal")
    assert "zig-guest: panic:" not in text, tag
    expected_status = 64 if tag == "usage" else 70 if tag in (
        "over", "invalid", "dense", "conversion", "missing", "denied", "exists", "evals", "diagnostics", "receipt", "idle") else 0
    if tag == "string":
        assert any(f"procs QJS.BIN exited status={status}" in text for status in (0, 70))
    elif tag not in ("bounds", "cold"):
        assert f"procs QJS.BIN exited status={expected_status}" in text, (tag, "wrong native exit status")
    if tag not in ("bounds", "cold", "usage", "exists"):
        final = re.search(r"qjs: final stack=(\d+) arena_peak=(\d+) files=0", text)
        assert final and 0 < int(final[1]) <= 131_072 and int(final[2]) <= 4_194_304, (tag, "full teardown measurement")
    pages = re.findall(r"^pages: armed=1 total=(0x[0-9a-f]+) free=(0x[0-9a-f]+)", text, re.M)
    assert len(pages) == 2, (tag, pages)
    reap = last_scheduler_reap(text)
    assert 0 <= reap < text.rfind("pages: armed=1"), tag
    receipt_name = {"file": "FILE.R", "refused": "REFUSED.R", "exact": "EXACT.R", "over": "OVER.R",
                    "invalid": "INVALID.R", "dense": "DENSE.R", "missing": "MISSING.R", "denied": "DENIED.R",
                    "string": "STRING.R", "conversion": "CONVERT.R",
                    "repl": "REPL.R", "line": "LINE.R", "line-over": "LINEOVER.R", "session": "SESSION.R",
                    "evals": "EVALS.R", "diagnostics": "DIAG.R", "receipt": "RECEIPT.R",
                    "eof": "EOF.R", "idle": "IDLE.R"}.get(tag)
    frames = []
    if receipt_name:
        data = (share / receipt_name).read_bytes()
        assert len(data) <= 1_114_112, (tag, "ReceiptLimit", len(data))
        shutil.copyfile(share / receipt_name, evidence / receipt_name)
        frames = parse(data, require_complete=tag != "receipt")
        diagnostic = b"".join(data for kind, data in frames if kind == "D")
        assert len(diagnostic) <= 16_384, (tag, "DiagnosticLimit", len(diagnostic))
        timings = {row["eval"]: row for kind, data in frames if kind == "T" for row in [fields(data)]}
        for result in results(frames):
            frequency = int(result["frequency"])
            assert frequency > 0
            assert 0 < int(result["stack"]) <= 131_072, result
            assert int(result["c_stack"]) <= 98_304 and int(result["arena_peak"]) <= 4_194_304, result
            assert int(result["files"]) <= 2 and int(result["resources"]) <= 7, result
            assert int(result["output"]) <= 65_536, result
            assert int(result["end"]) >= int(result["start"]), result
            elapsed = (int(result["end"]) - int(result["start"])) * 1000 / frequency
            assert elapsed <= 5000, (tag, "interruption/eval ceiling", elapsed)
            timing = timings.get(result["eval"])
            if tag == "receipt" and timing is None:
                continue  # A failed new receipt may end before the completion frame.
            assert timing is not None, result
            completed_ms = (int(timing["completion"]) - int(result["start"])) * 1000 / frequency
            assert completed_ms <= 5000, (tag, "visible completion ceiling", completed_ms)
            if result["failure"] == "EvaluationDeadline":
                assert 4000 <= completed_ms <= 5000, (tag, completed_ms)
            if result["failure"] == "Cancelled":
                assert int(timing["cancel"]) > 0
                assert (int(timing["completion"]) - int(timing["cancel"])) * 1000 / frequency <= 5000
            assert int(result["gap"]) * 1000 / frequency <= 100, (tag, "unchecked region", result)
        if results(frames):
            match = re.search(r"runtime-receipt: pid=\d+ name=QJS.BIN peak_pages=(\d+) page_cap=(\d+) peak_regions=(\d+) region_cap=(\d+) static_pages=(\d+)", text)
            assert match, "missing independent procs receipt QJS.BIN"
            dynamic, cap, regions, region_cap, static = map(int, match.groups())
            image = json.loads((rd / "share/QJS.BIN.json").read_text())["artifact"]
            expected_static = sum((p["memsz"] + 4095) // 4096 for p in image["segments"])
            assert static == expected_static and dynamic < cap and regions == 1 and region_cap == 16, match.groups()
    if tag in ("file", "refused"):
        golden = ROOT / "tests/fixtures/js" / (tag + ".stdout")
        actual = output(frames)
        assert actual == golden.read_bytes(), (tag, "golden mismatch")
        assert actual in ser, (tag, "stdout differs from receipt")
        assert len(results(frames)) == 1 and results(frames)[0]["failure"] == "none"
        print("QJS-FILE-EVAL" if tag == "file" else "QJS-REFUSALS: all 12 features, absent globals and failed authority calls")
    if tag == "cold":
        host_samples = json.loads((rd / "cold-host.json").read_text())
        assert len(host_samples) == 5
        shutil.copyfile(rd / "cold-host.json", evidence / "cold-host.json")
        samples = []
        for index in range(1, 6):
            path = share / f"COLD{index}"
            shutil.copyfile(path, evidence / path.name)
            frames = parse(path.read_bytes())
            result, = results(frames)
            assert output(frames) == b"7\n" and result["failure"] == "none"
            match = re.search(rf"qjs-cold: sample={index} child=\d+ frequency=(\d+) launch=(\d+)", text)
            assert match, index
            frequency, launch = map(int, match.groups())
            assert frequency == int(result["frequency"])
            assert int(result["first"]) >= launch
            sample = (int(result["first"]) - launch) * 1000 / frequency
            samples.append(sample)
        assert len(samples) == 5 and max(samples) <= 5000, samples
        (evidence / "cold.json").write_text(json.dumps({"samples_ms": samples, "maximum_ms": max(samples)}, indent=2) + "\n")
        print("QJS-COLD: five measured samples (cleanup asserted separately)", samples)
    if tag in ("repl", "session", "evals", "diagnostics", "receipt", "eof"):
        record = json.loads((rd / ("driver-" + tag + ".json")).read_text())
        shutil.copyfile(rd / ("driver-" + tag + ".json"), evidence / ("driver-" + tag + ".json"))
        assert record["success"], record
    if tag == "repl":
        rs = results(frames)
        assert [r["failure"] for r in rs[:7]] == ["none", "none", "JSException", "none", "none", "OutputLimit", "none"]
        assert rs[2]["reset"] == "0" and rs[5]["reset"] == "1"
        assert b"42\n40\nundefined\n" in output(frames), output(frames)[:150]
        for failure in ("StackLimit", "OutOfMemory", "UnsupportedFeature", "EvaluationDeadline", "Cancelled"):
            assert any(r["failure"] == failure and r["reset"] == "1" for r in rs), failure
        for index, result in enumerate(rs[:-1]):
            if result["failure"] != "none" and result["reset"] == "1":
                assert rs[index + 1]["failure"] == "none", (index, rs[index + 1])
        assert "qjs: LineCancelled" in text and "partial" not in text
        assert output(frames).endswith(b"42\n")
        # Decode per-eval output independently, rather than trusting result counts.
        eval_outputs = []
        pending = bytearray()
        for kind, data in frames:
            if kind == "O":
                pending.extend(data)
            elif kind == "R":
                eval_outputs.append(bytes(pending))
                pending.clear()
        # The last sixteen evals are eight hostile C paths and their 6*7 recovery.
        expected = (None, b"false\n", b"301030\n", b"20000\n", b"262145\n", b"32768\n", b"32768\n", None)
        for offset, golden in enumerate(expected):
            index = len(rs) - 16 + 2 * offset
            result = rs[index]
            if result["failure"] == "none":
                assert golden is not None and eval_outputs[index] == golden, (offset, result, eval_outputs[index][:80])
            else:
                assert result["failure"] in ("StackLimit", "OutOfMemory", "EvaluationDeadline", "WorkLimit"), result
                assert result["reset"] == "1"
            assert rs[index + 1]["failure"] == "none" and eval_outputs[index + 1] == b"42\n"
        print("QJS-INTERACTIVE-EVAL: real serial state, exceptions, reset, cancellation and next usable eval")
    if tag == "session":
        rs = results(frames)
        assert len(rs) == 17 and all(r["failure"] == "none" and r["output"] == "65536" for r in rs[:16])
        assert rs[16]["failure"] == "SessionLimit" and len(output(frames)) == 1_048_576
    if tag == "receipt":
        assert len(output(frames)) == 1_048_576 and frames[-1][0] != "Z"
        assert "qjs: ReceiptLimit" in text
    if tag == "eof":
        assert not results(frames) and output(frames) == b"" and "qjs: quit" in text
    if tag == "line":
        assert "qjs: LineLimit" not in text and output(frames) == b"42\n"
    if tag == "line-over":
        assert "qjs: LineLimit" in text and output(frames) == b"42\n"
    if tag == "exists":
        assert (share / "EXISTS").read_bytes() == b"previous receipt is never replaced\n"
    if tag == "bounds":
        assert text.count("stubs=5") == 3 and text.count("\n42\n") == 3
        print("QJS-BOUNDS: native policy checks, five stub latches and repeated complete teardown")
    if tag == "exact":
        assert output(frames) == b"7\n" and results(frames)[0]["failure"] == "none"
    if tag == "string":
        result, = results(frames)
        if result["failure"] == "none":
            assert output(frames) == b"32768\n" and "procs QJS.BIN exited status=0" in text
        else:
            assert result["failure"] in ("OutOfMemory", "WorkLimit", "EvaluationDeadline")
            assert result["reset"] == "1" and "procs QJS.BIN exited status=70" in text
    if tag == "conversion":
        result, = results(frames)
        assert result["failure"] in ("WorkLimit", "EvaluationDeadline") and result["reset"] == "1"
        assert output(frames) == b""
    if tag in ("over", "invalid", "dense"):
        assert "PREFIX-MUST-NOT-RUN" not in text
    assert pages[0] == pages[1], f"{tag}: M90f #1945 cleanup blocker: {pages}"
    print(tag + ": independent output/receipt and exact post-reap page recovery")
