"""Inspect and pin local artifacts; gate reads never rebuild or repin them."""
import hashlib
import importlib.util
import json
import shutil
import subprocess
import sys
from pathlib import Path

import corpus
from compare import compare

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / "artifacts/m90-acceptance"
FIXTURES = ROOT / "tests/fixtures/svg/acceptance"


def sha(data):
    return hashlib.sha256(data).hexdigest()


def source_pins():
    paths = []
    for pattern in ("user/go/svgproof/*", "user/go/vectorconsumer/*", "tools/svg-proof/*",
                    "user/go/svg/*.go", "user/go/vector/*.go", "user/go/vi/*.go",
                    "user/go/vsys/*", "tools/go/overlay/runtime/*", "tests/fixtures/svg/acceptance/*"):
        paths.extend(p for p in ROOT.glob(pattern) if p.is_file() and p.suffix != ".pyc")
    return {str(p.relative_to(ROOT)): sha(p.read_bytes()) for p in sorted(set(paths))}


def prepare(fork):
    spec = importlib.util.spec_from_file_location("elf_inspect", ROOT / "tools/svg-engine/elf_inspect.py")
    elf = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(elf)
    build = OUT / "build"
    report = elf.compare((build / "empty.ELF").read_bytes(), (build / "svg.ELF").read_bytes(),
                         (build / "svg-deps.txt").read_text(), (build / "empty-deps.txt").read_text())
    report["consumer"] = elf.inspect((build / "consumer.ELF").read_bytes())
    deps = (build / "consumer-deps.txt").read_text().splitlines()
    if "virelai/svg" in deps or "virelai/svgproof" in deps or "virelai/vectorconsumer" not in deps:
        raise ValueError("consumer imports SVG or is missing")
    report["source_sha256"] = source_pins()
    report["fork_runtime_sha256"] = {
        str(p.relative_to(fork)): sha(p.read_bytes())
        for p in (fork / "src/runtime").glob("*") if p.is_file()
    }
    report["compiler_sha256"] = sha((fork / "pkg/tool/darwin_arm64/compile").read_bytes())
    report["linker_sha256"] = sha((fork / "pkg/tool/darwin_arm64/link").read_bytes())
    audit_spec = importlib.util.spec_from_file_location("engine_source_audit", ROOT / "tools/svg-engine/ledger.py")
    source_audit = importlib.util.module_from_spec(audit_spec)
    audit_spec.loader.exec_module(source_audit)
    report["mapping_touch_audit"] = source_audit.audit(ROOT, fork)
    report["artifact_sha256"] = {p.name: sha(p.read_bytes()) for p in build.glob("*") if p.suffix != ".json"}
    corpus.materialize(OUT / "negatives")
    report["negative_sha256"] = {p.name: sha(p.read_bytes()) for p in (OUT / "negatives").glob("*") if p.is_file()}
    comparisons = {}
    for name, entry in json.loads((FIXTURES / "manifest.json").read_text()).items():
        comparisons[name] = compare((OUT / "host" / (name+".bgra")).read_bytes(),
                                    (OUT / "references" / (name+".bgra")).read_bytes(),
                                    entry["width"], entry["height"])
    report["host_comparisons"] = comparisons
    report["host_sha256"] = {p.name: sha(p.read_bytes()) for p in (OUT / "host").glob("*.bgra")}
    (build / "artifacts.json").write_text(json.dumps(report, indent=2, sort_keys=True)+"\n")
    print("offline SVG/consumer artifacts and host comparisons verified")


def verify():
    report = json.loads((OUT / "build/artifacts.json").read_text())
    if report["source_sha256"] != source_pins():
        raise ValueError("stale build: source pins differ")
    for folder, key in (("build", "artifact_sha256"), ("host", "host_sha256"), ("negatives", "negative_sha256")):
        for name, digest in report[key].items():
            if sha((OUT / folder / name).read_bytes()) != digest:
                raise ValueError("artifact pin mismatch: "+name)
    for name, entry in json.loads((FIXTURES / "manifest.json").read_text()).items():
        if sha((OUT / "references" / (name+".bgra")).read_bytes()) != entry["bgra_sha256"]:
            raise ValueError("oracle reference missing/mismatch: "+name)
    return report


def stage(destination):
    verify()
    # Verify the installed oracle as well as the already-prepared reference
    # bytes. No reference generation or package installation occurs in gates.
    subprocess.run([str(OUT / "oracle/venv/bin/python"), "-c",
                    "import sys,json; sys.path.insert(0,'tools/svg-proof'); "
                    "import oracle; "
                    "assert oracle.environment()==json.loads((oracle.FIXTURES/'oracle-lock.json').read_text()), 'oracle drift'"],
                   cwd=ROOT, check=True)
    destination = Path(destination)
    for name in ("svg", "consumer"):
        shutil.copyfile(OUT / "build" / (name+".ELF"), destination / (name.upper()+".ELF"))
    for p in FIXTURES.glob("*.svg"):
        shutil.copyfile(p, destination / p.name)
    for p in (OUT / "negatives").glob("*"):
        shutil.copyfile(p, destination / p.name)
    print("verified prebuilt SVG and independent consumer, corpus and reference pins")


if __name__ == "__main__":
    if sys.argv[1] == "prepare":
        prepare(Path(sys.argv[2]))
    else:
        stage(sys.argv[2])
