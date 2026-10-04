"""Source-fresh staging shared by the two existing desktop gate specs."""

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess


def stage(names):
    root = Path.cwd()
    share = Path(os.environ.get("VG_SHARE") or Path(os.environ["RUN_DIR"]) / "share")
    evidence = root / "artifacts/m91-workflow"
    evidence.mkdir(parents=True, exist_ok=True)
    sources = sorted((root / "user/go").rglob("*.go"))
    sources += [path for path in (root / "user/go/go.mod", root / "user/go/go.sum")
                if path.exists()]
    sources += sorted(path for path in (root / "tools/go/overlay").rglob("*")
                      if path.is_file())
    sources += [root / "tools/go" / builder for builder in names.values()]
    digest = hashlib.sha256()
    for path in sources:
        digest.update(str(path.relative_to(root)).encode() + b"\0" + path.read_bytes())
    fork = Path(os.environ.get("GO_FORK_DIR") or root.parent / "go-virelai")
    version = subprocess.check_output([str(fork / "bin/go"), "version"], text=True).strip()
    receipt = {"source_sha256": digest.hexdigest(), "toolchain": version, "binaries": {}}
    for name, builder in names.items():
        # Rebuild even an existing ELF. A file's presence/mtime is not freshness.
        command = ["bash", str(root / "tools/go" / builder)]
        subprocess.run(command, check=True)
        src = root / ".build/go" / (name + ".ELF")
        dst = share / src.name
        shutil.copyfile(src, dst)
        built = src.read_bytes()
        if not built.startswith(b"\x7fELF") or dst.read_bytes() != built:
            raise SystemExit("not a source-built, byte-identical ELF: " + name)
        receipt["binaries"][src.name] = hashlib.sha256(built).hexdigest()
    suffix = os.environ.get("VIRELAI_GATE_SUFFIX", "")
    tag = "default" if "go-wm-default" in os.environ["RUN_DIR"] else "hid"
    (evidence / ("fresh-" + tag + suffix + ".json")).write_text(
        json.dumps(receipt, indent=2) + "\n")
    print("M91 source-fresh staging: " + ", ".join(receipt["binaries"]))
