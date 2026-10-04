#!/usr/bin/env python3
"""Explicit implementation maintenance, never invoked by an offline build."""
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BASE = ROOT / "user/zig/quickjs"


def hashes(paths):
    return {p.relative_to(ROOT).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted(paths)}


def main():
    path = BASE / "lock.json"
    lock = json.loads(path.read_text())
    # Implementation pinning cannot bless changed upstream source.
    for name, digest in lock["upstream"]["files"].items():
        if hashlib.sha256((BASE / "upstream" / name).read_bytes()).hexdigest() != digest:
            raise ValueError("SourceDrift: " + name)
    lock["bridge_units"] = ["bytes.c", "format.c", "guard.c", "binding.c", "guard.S"]
    lock["patches"] = hashes([ROOT / "tools/quickjs-runtime/materialize.py", ROOT / "tools/quickjs-runtime/safety.py"])
    lock["private_inputs"] = hashes([
        *sorted((ROOT / "tools/quickjs-runtime").glob("*.py")), BASE / "inventory.json",
        *sorted((BASE / "headers").glob("*.h")),
        *sorted(BASE.glob("*.zig")), *sorted(BASE.glob("*.c")), *sorted(BASE.glob("*.S")),
    ])
    path.write_text(json.dumps(lock, indent=2, sort_keys=True) + "\n")


if __name__ == "__main__":
    main()
