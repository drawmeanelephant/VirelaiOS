"""Bounded host observation of paused live receipts; no guest counters invented."""
import json
import re
import subprocess
import sys
import time
from pathlib import Path

PAUSES = (
    ("baseline", "pdf-proof: baseline ns=", "pdf-proof: resumed"),
    ("cycle-50", "pdf-proof: cycles=50 ns=", "pdf-proof: resumed cycle=50"),
)
LIVE_ROW = re.compile(r"^runtime-receipt: pid=\d+ name=PDFPROOF\.ELF [^\r\n]* reaped=0\r?\n", re.M)


def acknowledge(serial, share, acknowledged):
    """Release only after a full live row, never a command echo or partial row."""
    for name, marker, resumed in PAUSES:
        if name in acknowledged:
            continue
        start = re.search(r"^"+re.escape(marker)+r"\d+\r?\n", serial, re.M)
        if start is None:
            break
        following = serial[start.end():]
        if re.search(r"^"+re.escape(resumed)+r"\r?\n", following, re.M):
            raise ValueError("ReceiptPauseOrder: resumed before host acknowledgment")
        if LIVE_ROW.search(following) is None:
            break
        path = share/"PDF"/(name+".resume")
        if path.exists():
            raise ValueError("ReceiptPauseDrift: acknowledgment already exists")
        pending = path.with_suffix(".pending")
        with pending.open("xb") as output:
            output.write(b"1")
        pending.rename(path)
        acknowledged.append(name)


def observe(run):
    acknowledged = []
    serial = run/"vm-serial-runtime.log"
    # Existing boot timeouts: 180 + 180 + 360 before runtime; runtime 360.
    # This observer exits after the second live row, or within their sum.
    deadline = time.monotonic() + 180 + 180 + 360 + 360
    result = run/"receipt-pause-result.json"
    try:
        while time.monotonic() < deadline:
            if serial.exists():
                before = len(acknowledged)
                acknowledge(serial.read_text(errors="replace"), run/"share", acknowledged)
                if len(acknowledged) != before:
                    processes = subprocess.check_output(["ps", "-axo", "pid,etime,command"], text=True)
                    (run/("ps-"+acknowledged[-1]+".txt")).write_text(
                        "\n".join(line for line in processes.splitlines() if "VMRunner" in line)+"\n")
            if len(acknowledged) == len(PAUSES):
                result.write_text(json.dumps({"acknowledged": acknowledged})+"\n")
                return
            time.sleep(0.02)
        raise ValueError("ReceiptPauseTimeout: missing paused live receipt")
    except Exception as exc:
        result.write_text(json.dumps({"acknowledged": acknowledged, "failure": str(exc)})+"\n")
        raise


def start_observer(run):
    with (run/"receipt-pause-observer.log").open("w") as log:
        child = subprocess.Popen([sys.executable, "-B", str(Path(__file__).resolve()), str(run)],
                                 stdout=log, stderr=subprocess.STDOUT)
    (run/"receipt-pause-observer.pid").write_text(str(child.pid)+"\n")


if __name__ == "__main__":
    observe(Path(sys.argv[1]).resolve())
