#!/usr/bin/env python3
"""Offline native acceptance setup/comparison. Not a publication helper."""
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
    for mode in ("guest", "guest-oom", "guest-resources"):
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
    case("publication-blocked", ["build", "/host/content", "/host/site"], 70, "NativePublicationUnavailable:")
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
    (share / "BORIS.CASES").write_text("".join(
        "\t".join([c["id"], c["binary"], str(c["status"]), *c["args"]]) + "\n" for c in cases))
    (expected / "cases.json").write_text(json.dumps(cases, indent=2) + "\n")
    # Input/publication preservation assertions exclude only harness captures.
    (expected / "share.json").write_text(json.dumps(share_state(share), indent=2, sort_keys=True) + "\n")
    print(f"Staged {len(cases)} native cases; publication/entropy acceptance remains blocked")


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
    for c in cases:
        assert text.splitlines().count("boris-gate: case " + c["id"]) == 1, c["id"]
        stdout = (share / (c["id"] + ".out")).read_bytes()
        stderr = (share / (c["id"] + ".err")).read_text()
        if c["error"]:
            assert stdout == b"", c["id"]
            assert "boris-guest: " + c["error"] in stderr, (c["id"], stderr)
            if c["id"] == "resources":
                assert "boris-test: resources_peak=8\n" in stderr
        elif c["output"] == "oracle":
            assert check.unpack(stdout) == check.unpack((expected / "oracle.bundle").read_bytes()), c["id"]
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
    reaches_identity = any(c["args"][0] in ("compile", "inspect") and
                           c["id"] not in ("missing", "bad-root", "symlink-root", "parallel")
                           for c in cases)
    assert charge == (2 if reaches_identity else 0), pages
    pools = re.findall(r"^tasks: enabled=1 current=\d+ switches=\d+ pool=(\d+)/(\d+) zombies=(\d+)$", text, re.M)
    assert len(pools) == 2 and pools[0] == pools[1] and pools[0][2] == "0", pools
    assert "72 sys_getrandom calls=0" in text  # No fake publication/entropy proof.
    baseline = json.loads((expected / "share.json").read_text())
    current = share_state(share)
    for path, record in baseline.items():
        assert current.get(path) == record, path
    added = current.keys() - baseline.keys()
    captures = {c["id"] + ext for c in json.loads((expected / "cases.json").read_text()) for ext in (".out", ".err")}
    # The monitor independently persists command history after script input.
    assert added <= captures | {"HISTORY.TXT"}, added - captures
    assert all(current[name][0] == "file" for name in added), "unexpected mutation kind"
    (evidence / f"batch-{batch}.json").write_text(json.dumps(receipts, indent=2) + "\n")
    print(f"Native batch {batch}: {len(cases)} cases, oracle bytes/limits/cleanup/preservation pass; not publication acceptance")


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
