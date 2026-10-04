#!/usr/bin/env python3
"""Record host load and competing VM activity alongside every cold sample."""
import json
import os
from pathlib import Path
import subprocess
import sys
import time
from receipt import parse, results


def capture(directory):
    records = []
    seen = set()
    deadline = time.monotonic() + 1200
    while directory.is_dir() and time.monotonic() < deadline:
        for index in range(1, 6):
            if index in seen:
                continue
            try:
                rows = results(parse((directory / "share" / f"COLD{index}").read_bytes(), require_complete=False))
            except (OSError, ValueError, IndexError):
                continue
            if len(rows) == 1 and rows[0]["failure"] == "none":
                seen.add(index)
                processes = subprocess.check_output(["ps", "-axo", "pid,ppid,comm"], text=True)
                records.append({"sample": index, "guest_receipt": rows[0], "observed_host_monotonic_ns": time.monotonic_ns(),
                                "load": os.getloadavg(),
                                "vm_processes": [row for row in processes.splitlines() if "VMRunner" in row]})
                (directory / "cold-host.json").write_text(json.dumps(records, indent=2) + "\n")
        if len(records) == 5:
            return
        time.sleep(0.05)
    raise RuntimeError("five cold samples not observed")


if __name__ == "__main__":
    capture(Path(sys.argv[1]))
