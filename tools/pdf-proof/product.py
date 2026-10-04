"""Inspect the equivalent proof pair and reject reachable hosted transport."""
import json
import hashlib
import sys
from pathlib import Path


def inspect_product(root, out):
    sys.path.insert(0, str(root/"tools/pdf-engine"))
    from elf import inspect, reachable
    empty, full = (inspect((out/(name+".ELF")).read_bytes()) for name in ("empty", "full"))
    delta, initialized = full["file_bytes"]-empty["file_bytes"], full["load_filesz"]-empty["load_filesz"]
    if not (0 <= delta <= 2097152 and 0 <= initialized <= 2097152):
        raise ValueError("EngineSizeLimit")
    reachable((out/"reachable.txt").read_text())
    for name in ("empty", "full"):
        deps = (out/(name+"-deps.txt")).read_text().splitlines()
        for p in deps:
            if p in ("C", "runtime/cgo", "plugin") or p.startswith("net") or "." in p.split("/")[0]:
                raise ValueError("SourceDrift: forbidden dependency "+p)
            if p.startswith("virelai/") and p not in {"virelai/pdfproof", "virelai/pdf", "virelai/vector", "virelai/vi", "virelai/vsys"}:
                raise ValueError("SourceDrift: unapproved guest package "+p)
    result = {"empty": empty, "full": full, "engine_added_bytes": delta, "initialized_added_bytes": initialized,
              "full_sha256": hashlib.sha256((out/"full.ELF").read_bytes()).hexdigest(),
              "empty_sha256": hashlib.sha256((out/"empty.ELF").read_bytes()).hexdigest(),
              "proof_lock_sha256": hashlib.sha256((root/"tools/pdf-proof/proof-lock.json").read_bytes()).hexdigest(),
              "static_pages": full["page_rounded_load_bytes"]//4096, "guest_run": False}
    (out/"elf.json").write_text(json.dumps(result, indent=2)+"\n")
    return result


if __name__ == "__main__":
    inspect_product(Path(sys.argv[1]), Path(sys.argv[2]))
