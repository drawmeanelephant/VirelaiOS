#!/usr/bin/env python3
"""Offline native publication acceptance setup and independent comparison."""
import argparse
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys

sys.dont_write_bytecode = True
import build
import check


def share_state(share):
    state = {}
    for p in share.rglob("*"):
        name = p.relative_to(share).as_posix()
        if p.is_symlink():
            state[name] = ["symlink", str(p.readlink())]
        elif p.is_file():
            state[name] = ["file", build.sdk.digest(p.read_bytes())]
        elif p.is_dir():
            state[name] = ["directory"]
        else:
            raise ValueError("unexpected fixture object: " + name)
    return state


def stage(share, expected, cache=build.sdk.DEFAULT_CACHE):
    share.mkdir(parents=True, exist_ok=True)
    expected.mkdir(parents=True, exist_ok=True)
    work = build.ROOT / "artifacts/boris-native/stage"
    oracle = build.build("host", work / "oracle", cache)
    sources = check.unpack(subprocess.check_output([str(oracle), "sources"]))
    bundle = subprocess.check_output([str(oracle)])
    (expected / "oracle.bundle").write_bytes(bundle)
    kernel_bytes = (build.ROOT / "zig-out/bin/KERNEL.BIN").stat().st_size
    assert kernel_bytes <= 16 * 1024 * 1024, f"KRN2 exceeds ceiling by {kernel_bytes - 16 * 1024 * 1024} bytes"
    (expected / "image.json").write_text(json.dumps({
        "kernel_bytes": kernel_bytes, "ceiling_bytes": 16 * 1024 * 1024,
        "headroom_bytes": 16 * 1024 * 1024 - kernel_bytes,
    }, indent=2) + "\n")
    print(f"KRN2: {kernel_bytes} bytes; headroom {16 * 1024 * 1024 - kernel_bytes} bytes")
    for mode in ("guest", "guest-oom", "guest-resources", "guest-no-entropy", "guest-short-entropy"):
        binary = build.build(mode, work / mode, cache)
        shutil.copy2(binary, share / binary.name)
    binary = build.gate(work / "gate", cache)
    shutil.copy2(binary, share / binary.name)

    def fixture(name):
        root = share / name
        root.mkdir()
        for path, (_, data) in sources.items():
            file = root / path
            file.parent.mkdir(parents=True, exist_ok=True)
            file.write_bytes(data)
        return root

    fixture("content")
    cases = []

    def case(name, args, status=0, error=None, output=None, binary="BORIS.BIN"):
        cases.append(dict(id=name, args=args, status=status, error=error, output=output, binary=binary))

    case("native-first", ["compile", "/host/content"], output="oracle")
    case("native-again", ["compile", "/host/content"], output="oracle")
    case("inventory", ["inspect", "/host/content"], output="inventory:29")
    site = share / "site"
    site.mkdir()
    (site / "prior.html").write_bytes(b"prior-good-site\n")
    case("publication-first", ["build", "/host/content", "/host/site"], output="published:site")
    case("oom", ["compile", "/host/content"], 70, "OutOfMemory", binary="BORISOOM.BIN")

    for count in (256, 257):
        root = share / f"entries-{count}"
        (root / "ignored").mkdir(parents=True)
        (root / "empty").mkdir()
        for n in range(count - 2):
            (root / "ignored" / f"ignored-{n:03}.txt").write_bytes(b"")
        case(f"entries-{count}", ["inspect", f"/host/entries-{count}"],
             0 if count == 256 else 70, None if count == 256 else "TreeLimit",
             "inventory:256" if count == 256 else None)
    for depth in (8, 9):
        root = share / f"depth-{depth}"
        root.mkdir()
        node = root
        for _ in range(depth):
            node = node / "a"
            node.mkdir()
        case(f"depth-{depth}", ["inspect", f"/host/depth-{depth}"],
             0 if depth == 8 else 70, None if depth == 8 else "ResourceLimit",
             f"inventory:{depth}" if depth == 8 else None)
    root = share / "name-exact"
    root.mkdir()
    (root / ("x" * 255)).write_bytes(b"ignored")
    case("name-exact", ["inspect", "/host/name-exact"], output="inventory:1")
    # /host/p + / + a 255-byte directory + / + a 248-byte leaf = 512.
    root = share / "p"
    (root / ("x" * 255)).mkdir(parents=True)
    (root / ("x" * 255) / ("y" * 248)).write_bytes(b"")
    case("path-exact", ["inspect", "/host/p"], output="inventory:2")
    root = share / "q"
    (root / ("x" * 255)).mkdir(parents=True)
    (root / ("x" * 255) / ("y" * 249)).write_bytes(b"")
    case("path-over", ["inspect", "/host/q"], 70, "NameTooLong")

    for length in (131072, 131073):
        root = fixture(f"file-{length}")
        (root / "padding.txt").write_bytes(b"x" * length)
        case(f"file-{length}", ["compile", f"/host/file-{length}"],
             0 if length == 131072 else 70, None if length == 131072 else "InputLimit",
             "oracle" if length == 131072 else None)
    source_bytes = sum(len(data) for _, data in sources.values())
    for length in (1048576, 1048577):
        root = fixture(f"total-{length}")
        remaining = length - source_bytes
        index = 0
        while remaining:
            n = min(131072, remaining)
            (root / f"padding-{index}.txt").write_bytes(b"x" * n)
            remaining -= n
            index += 1
        case(f"total-{length}", ["compile", f"/host/total-{length}"],
             0 if length == 1048576 else 70, None if length == 1048576 else "InputLimit",
             "oracle" if length == 1048576 else None)

    (share / "root-link").symlink_to("content", target_is_directory=True)
    case("symlink-root", ["compile", "/host/root-link"], 70, "AccessDenied")
    for name, target in (("directory-link", "../content"), ("file-link", "../content/index.md"),
                         ("dangling-link", "../absent")):
        root = share / name
        root.mkdir()
        (root / "link").symlink_to(target)
        case(name, ["compile", f"/host/{name}"], 70, "AccessDenied")
    case("missing", ["compile", "/host/absent"], 70, "FileNotFound")
    case("bad-root", ["compile", "/usb/content"], 70, "AccessDenied")
    for name in ("watch", "preview", "serve", "publish", "auth", "login", "keys", "editor",
                 "--watch", "--preview", "--online", "--capture"):
        case("unsupported-" + name.lstrip("-"), [name], 64, "UnsupportedFeature")
    case("parallel", ["compile", "/host/content", "--jobs=1"], 64, "UnsupportedParallelism")
    case("usage", ["build"], 64, "Usage")
    # B3 deliberately pins <=512 identities for the mount lifetime, not per
    # process. Keep the two ~256-object boundary trees in different boots.
    cases.insert(10, cases.pop(6))
    case("resources", ["inspect", "/host/content"], 70, "ResourceLimit", binary="BORISFULL.BIN")
    case("publication-again", ["build", "/host/content", "/host/site"], output="published:site")
    for name in ("no-entropy", "short-entropy", "failure-site", "invalid-site"):
        root = share / name
        root.mkdir()
        (root / "prior.html").write_bytes(b"prior-good-site\n")
    case("entropy-unavailable", ["build", "/host/content", "/host/no-entropy"], 70,
         "EntropyUnavailable", binary="BORISNO.BIN")
    case("entropy-short", ["build", "/host/content", "/host/short-entropy"],
         output="published:short-entropy", binary="BORISSHORT.BIN")
    # Real native type conflict after the three IR replacements: stage all
    # artifacts, report partial publication, never delete/copy the destination.
    (share / "failure-site" / "build-report.json").mkdir()
    (share / "failure-site" / "build-report.json" / "keep").write_bytes(b"prior-good-entry\n")
    case("publication-failure", ["build", "/host/content", "/host/failure-site"], 70, "InvalidArgument")
    invalid = fixture("invalid-content")
    (invalid / "index.md").write_bytes(b"---\ntitle: Bad\nparent: missing\n---\n# Bad\n")
    case("compiler-failure", ["build", "/host/invalid-content", "/host/invalid-site"], 70, "CompilationFailed")
    case("root-collision", ["build", "/host/content", "/host/content"], 70, "TargetOutputCollision")
    case("nested-output", ["build", "/host/content", "/host/content/site"], 70, "TargetOutputCollision")
    case("output-symlink", ["build", "/host/content", "/host/root-link"], 70, "AccessDenied")
    (share / "BORIS.CASES").write_text("".join(
        "\t".join([c["id"], c["binary"], str(c["status"]), *c["args"]]) + "\n" for c in cases))
    (expected / "cases.json").write_text(json.dumps(cases, indent=2) + "\n")
    # Input/publication preservation assertions exclude only harness captures.
    (expected / "share.json").write_text(json.dumps(share_state(share), indent=2, sort_keys=True) + "\n")
    print(f"Staged {len(cases)} native cases including real publication, republish, short entropy and failure preservation")


def published_tree(share, name, oracle):
    site = share / name
    for path, (_, data) in oracle.items():
        assert (site / path).read_bytes() == data, (name, path)
    files = {p.relative_to(site).as_posix() for p in site.rglob("*") if p.is_file()}
    assert files == oracle.keys() | {"prior.html"}, (name, files - oracle.keys())
    assert not any(p.name.startswith(".boris-stage-") for p in site.rglob("*")), name
    return {path: build.sdk.digest((site / path).read_bytes()) for path in sorted(files)}


def compare(share, expected, serial, batch):
    cases = json.loads((expected / "cases.json").read_text())[batch * 10:(batch + 1) * 10]
    evidence = build.ROOT / "artifacts/boris-native/vz"
    evidence.mkdir(parents=True, exist_ok=True)
    for c in cases:
        for ext in (".out", ".err"):
            capture = share / (c["id"] + ext)
            if capture.is_file():
                shutil.copy2(capture, evidence / (c["id"] + ext))
    text = serial.read_text()
    assert text.splitlines().count(f"boris-gate: done cases={len(cases)}") == 1
    assert "boris-gate: FAIL" not in text and "[EXC]" not in text
    receipts = []
    oracle = check.unpack((expected / "oracle.bundle").read_bytes())
    for c in cases:
        assert text.splitlines().count("boris-gate: case " + c["id"]) == 1, c["id"]
        stdout = (share / (c["id"] + ".out")).read_bytes()
        stderr = (share / (c["id"] + ".err")).read_text()
        if c["error"]:
            assert stdout == b"", c["id"]
            assert "boris-guest: " + c["error"] in stderr, (c["id"], stderr)
            if c["id"] == "resources":
                assert "boris-test: resources_peak=8\n" in stderr
        elif c["output"] == "oracle" or c["output"].startswith("published:"):
            publishing = c["output"].startswith("published:")
            if publishing:
                assert stdout == f"boris-published: artifacts={len(oracle)} jobs=1 offline=1\n".encode(), c["id"]
                tree = published_tree(share, c["output"].split(":")[1], oracle)
                if c["id"] == "publication-first":
                    (expected / "first-publication.json").write_text(json.dumps(tree, sort_keys=True) + "\n")
                if c["id"] == "publication-again":
                    assert tree == json.loads((expected / "first-publication.json").read_text()), "nondeterministic republish"
            else:
                assert check.unpack(stdout) == oracle, c["id"]
            if c["id"] == "native-again":
                assert stdout == (share / "native-first.out").read_bytes(), "nondeterministic repetition"
            m = re.search(r"visited=(\d+) input_bytes=(\d+) arena_peak=(\d+) stack_high_water=(\d+) resources_peak=(\d+)", stderr)
            assert m, (c["id"], stderr)
            visited, input_bytes, arena, stack, resources = map(int, m.groups())
            assert visited <= 256 and input_bytes <= 1048576 and arena <= 12582912 and stack <= 131072 and resources <= 8
            receipts.append(dict(id=c["id"], visited=visited, input_bytes=input_bytes,
                                 arena_peak=arena, stack_high_water=stack, resources_peak=resources))
        elif c["output"].startswith("inventory:"):
            n = int(c["output"].split(":")[1])
            assert stdout == f"boris-inventory: visited={n} resources_peak=5\n".encode(), (c["id"], stdout)
    pages = re.findall(r"^pages: armed=1 total=(0x[0-9a-f]+) free=(0x[0-9a-f]+)", text, re.M)
    # B3's mount-lifetime filesystem identity ledger allocates two pages on
    # first use. Existing live-user-fs pins this exact retained native charge.
    assert len(pages) == 2 and pages[0][0] == pages[1][0], pages
    charge = int(pages[0][1], 16) - int(pages[1][1], 16)
    reaches_identity = any(c["args"][0] in ("compile", "inspect", "build") and
                           c["id"] not in ("missing", "bad-root", "symlink-root", "parallel", "root-collision", "nested-output")
                           for c in cases)
    assert charge == (2 if reaches_identity else 0), pages
    pools = re.findall(r"^tasks: enabled=1 current=\d+ switches=\d+ pool=(\d+)/(\d+) zombies=(\d+)$", text, re.M)
    assert len(pools) == 2 and pools[0] == pools[1] and pools[0][2] == "0", pools
    entropy = re.search(r"72 sys_getrandom calls=(\d+)", text)
    assert entropy
    publications = sum(bool(c["output"] and c["output"].startswith("published:")) or c["id"] == "publication-failure" for c in cases)
    # Three-byte short fills require six actual slot-72 calls for 16 bytes.
    assert int(entropy[1]) == publications + 5 * sum(c["id"] == "entropy-short" for c in cases), entropy[0]
    assert "80 sys_socket calls=0" in text  # Offline artifact cannot use B6.
    baseline = json.loads((expected / "share.json").read_text())
    current = share_state(share)
    for path, record in baseline.items():
        assert current.get(path) == record, path
    added = current.keys() - baseline.keys()
    captures = {c["id"] + ext for c in json.loads((expected / "cases.json").read_text()) for ext in (".out", ".err")}
    publication_paths = set()
    for name in ("site", "short-entropy"):
        publication_paths.update(name + "/" + path for path in oracle)
        publication_paths.update(name + "/" + p for path in oracle for p in parents(path))
    failed_stages = [p for p in (share / "failure-site").iterdir() if p.name.startswith(".boris-stage-")]
    if any(c["id"] == "publication-failure" for c in cases):
        assert len(failed_stages) == 1
        stage = failed_stages[0]
        moved = set(list(oracle)[:3])
        assert {p.relative_to(stage).as_posix() for p in stage.rglob("*") if p.is_file()} == oracle.keys() - moved
        for path, (_, data) in oracle.items():
            location = share / "failure-site" if path in moved else stage
            assert (location / path).read_bytes() == data
            if path in moved:
                publication_paths.add("failure-site/" + path)
        error = (share / "publication-failure.err").read_text()
        assert f"retained_stage={stage.name} published=3 outcome=partial_or_unknown" in error
    for stage in failed_stages:
        publication_paths.add(stage.relative_to(share).as_posix())
        publication_paths.update(p.relative_to(share).as_posix() for p in stage.rglob("*"))
    # The monitor independently persists command history after script input.
    assert added <= captures | publication_paths | {"HISTORY.TXT"}, added - captures - publication_paths
    (evidence / f"batch-{batch}.json").write_text(json.dumps(receipts, indent=2) + "\n")
    print(f"Native batch {batch}: {len(cases)} cases, oracle bytes/publication/limits/cleanup/preservation pass")


def parents(path):
    result = []
    while "/" in path:
        path = path.rsplit("/", 1)[0]
        result.append(path)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("stage", "compare"))
    parser.add_argument("--share", type=Path, required=True)
    parser.add_argument("--expected", type=Path, required=True)
    parser.add_argument("--serial", type=Path)
    parser.add_argument("--batch", type=int)
    parser.add_argument("--cache", type=Path, default=build.sdk.DEFAULT_CACHE)
    args = parser.parse_args()
    if args.command == "stage":
        stage(args.share.resolve(), args.expected.resolve(), args.cache.resolve())
    else:
        compare(args.share.resolve(), args.expected.resolve(), args.serial.resolve(), args.batch)


if __name__ == "__main__":
    main()
