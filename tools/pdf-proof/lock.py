"""Additional proof input pin, checked alongside the read-only engine lock."""
import hashlib
import json
import sys
from pathlib import Path


def snapshot(root):
    paths = []
    for directory in ("user/go/pdfproof", "tools/pdf-proof", "tests/fixtures/pdf/acceptance"):
        for p in (root/directory).rglob("*"):
            if p.is_file() and p.suffix in (".go", ".py", ".sh", ".json", ".pdf") and p.name != "proof-lock.json":
                paths.append(p)
    return {p.relative_to(root).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted(paths)}


if __name__ == "__main__":
    root = Path(sys.argv[2])
    lock = root/"tools/pdf-proof/proof-lock.json"
    actual = {"version": 1, "engine_lock_sha256": hashlib.sha256((root/"tools/pdf-engine/engine-lock.json").read_bytes()).hexdigest(),
              "inputs_sha256": snapshot(root)}
    if sys.argv[1] == "record":
        lock.write_text(json.dumps(actual, indent=2)+"\n")
    elif actual != json.loads(lock.read_text()):
        raise ValueError("SourceDrift: proof lock")
    else:
        print("pdf-proof: lock verified")
