"""C1 live-oliver setup and byte comparisons. No downloads or normalization."""
import json
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools/zig"))
import oliver
import sdk


def setup(rd):
    cache = Path(os.environ.get("ZIG_GUEST_CACHE", sdk.DEFAULT_CACHE)).resolve()
    share = Path(os.environ.get("VG_SHARE") or rd / "share")
    oliver.build(cache, oliver.DEFAULT_SOURCE, rd / "build", share / "OLIVER.BIN", probe=True)
    oliver.probes(cache, oliver.DEFAULT_SOURCE, rd / "probes", share / "OPROBE.BIN")
    host = rd / "host-oliver"
    oliver.host(cache, oliver.DEFAULT_SOURCE, rd / "host-build", host)
    expected = {}
    cases = []

    def case(tag, data, args, status=0, diagnostic=None, library_output=None):
        input_path = share / (tag + ".IN")
        input_path.write_bytes(data)
        cases.append("\t".join([tag, "/host/" + input_path.name, str(status), *args]))
        if status < 0:
            out, err = b"", b""
        elif library_output:
            result = subprocess.run([str(host), "render", "--from", "markdown"],
                                    input=data, capture_output=True, check=True)
            out, err = result.stdout, b""
        elif diagnostic:
            out, err = b"", ("oliver: " + diagnostic + "\n").encode()
        else:
            result = subprocess.run([str(host), *args], input=data, capture_output=True, check=True)
            out, err = result.stdout, result.stderr
        expected[tag] = dict(status=status, out=out.hex(), err=err.hex(), library=library_output)

    md = (ROOT / "tests/oliver-spike/md-fixture.txt").read_bytes()
    # Refresh the historical golden independently, then compare, not eyeball.
    fresh = subprocess.run([str(host), "render", "--from", "markdown"],
                           input=md, capture_output=True, check=True).stdout
    assert fresh == (ROOT / "tests/oliver-spike/expect.html").read_bytes()
    (rd / "fresh-native-golden.html").write_bytes(fresh)
    (share / "MD.TXT").write_bytes(md)
    case("library-rel", md, ["library", "MD.TXT", "LIB.REL"], library_output="LIB.REL")
    case("library-abs", md, ["library", "/host/MD.TXT", "/host/LIB.ABS"], library_output="LIB.ABS")
    long_name = "a" * 31 + "/" + "b" * 26  # 58 relative / 64 absolute bytes
    (share / ("a" * 31)).mkdir()
    (share / long_name).write_bytes(md)
    case("library-long", md, ["library", long_name, "LIB.LONG"], library_output="LIB.LONG")
    case("path-over", md, ["library", long_name + "c", "UNWRITTEN"], 70, "PathLimit")
    case("component-over", md, ["library", "x" * 32, "UNWRITTEN"], 70, "PathLimit")
    case("arg255", md, ["library", "x" * 255, "UNWRITTEN"], 70, "PathLimit")
    case("arg256", md, ["library", "x" * 256, "UNWRITTEN"], -1)
    case("args8", md, ["render", "--from", "markdown", "--to", "html", "--raw-html", "allowed", "--footnotes"], -1)
    for dialect, data in (
        ("markdown", b"# Native\n\nA **bold** paragraph.\n\n- first\n- second\n"),
        ("textile", b"h1. Textile\n\nA *bold* paragraph.\n"),
        ("cooklang", b"= Sauce =\nMix @salt{2%g} with @water{100%ml}.\n"),
    ):
        for profile in ("html", "xhtml", "html4-strict"):
            case(dialect + "-" + profile, data, ["render", "--from", dialect, "--to", profile])
        case("meta-" + dialect, b'---\ntitle: "Native"\nauthor: Guest\ndescription: |\n  one\n  two\n---\n# Body\n',
             ["meta", "--from", dialect, "--format", "json"])
    # Seven user arguments, plus the gap loader's own program-name slot.
    case("diag", b"---\nlist: [1, 2]\n---\n# Body\n",
         ["render", "--from", "markdown", "--frontmatter", "yaml", "--diagnostics", "json"])
    for flag, data in (
        ("footnotes", b"A note[^n].\n\n[^n]: Note.\n"),
        ("heading-ids", b"# A *heading*\n"), ("heading-attributes", b"# Title {#id}\n"),
        ("definition-lists", b"Term\n: Definition\n"), ("strikethrough", b"~~gone~~\n"),
        ("wikilinks", b"[[Page|label]]\n"), ("callouts", b"> [!NOTE]\n> Body\n"),
        ("smartypants", b'"hello" -- friend...\n'), ("task-lists", b"- [x] done\n"),
    ):
        case(flag, data, ["render", "--from", "markdown", "--" + flag])
    case("rewrite", b"[Next](next.md) ![image](photo.textile)\n", ["render", "--from", "markdown"])
    for policy in ("allowed", "escaped"):
        case("raw-" + policy, b"<b>raw</b>\n", ["render", "--from", "markdown", "--raw-html", policy])
    case("raw-refused", b"<b>raw</b>\n", ["render", "--from", "markdown", "--raw-html", "rejected"], 70, "RawHtmlRejected")
    case("xhtml-refused", b"<b>raw</b>\n", ["render", "--from", "markdown", "--to", "xhtml"], 70, "RawHtmlNotXmlWellFormed")
    case("input-exact", b"a" * (128 * 1024), ["render", "--from", "markdown"])
    case("input-over", b"a" * (128 * 1024 + 1), ["render", "--from", "markdown"], 70, "InputLimit")
    case("output-exact", b"&" * 104856, ["render", "--from", "markdown"])
    case("output-over", b"&" * 104856 + b"a", ["render", "--from", "markdown"], 70, "OutputLimit")
    case("depth-exact", b"[" * 32, ["render", "--from", "markdown"])
    case("depth-over", b"[" * 33, ["render", "--from", "markdown"], 70, "DepthLimit")
    case("yaml-depth", b"---\na:\n" + b" " * 33 + b"b: c\n---\nbody\n",
         ["render", "--from", "markdown", "--frontmatter", "yaml"], 70, "DepthLimit")
    case("toml-depth", b"+++\n[" + b"a." * 33 + b"b]\nx=1\n+++\nbody\n",
         ["render", "--from", "markdown", "--frontmatter", "toml"], 70, "DepthLimit")
    case("oom", b"a\n\n" * 43690, ["render", "--from", "markdown"], 70, "OutOfMemory")
    case("usage", b"ignored", ["render", "--from", "invalid"], 70, "Usage")
    case("panic", b"ignored", ["probe-panic"], 71, "Panic")
    case("stack", b"ignored", ["probe-stack"], 70, "StackBudget")
    for cmd in ("wrap", "plan", "manifest", "serialize", "scale", "menu"):
        case("deferred-" + cmd, b"ignored", [cmd], 70, "UnsupportedCommand")
    for index, start in enumerate(range(0, len(cases), 12), 1):
        tags = list(expected)[start:start + 12]
        for tag in tags:
            expected[tag]["group"] = "cli" + str(index)
        (share / ("OLIVER.CASES." + str(index))).write_text("\n".join(cases[start:start + 12]) + "\n")
    (rd / "expected.json").write_text(json.dumps(expected))
    (rd / "case-count.txt").write_text(str(len(cases)))
    # Preserve a destination on a semantic/refusal failure, not a fake write.
    (share / "UNWRITTEN").write_bytes(b"prior-good-content")
    base = (share / "FLAT.BIN").read_bytes()
    magic, flags, entry, size = struct.unpack_from("<IIQQ", base)
    assert magic == 0x314B5344 and len(base) == size
    content = size - 24
    page = (content + 4095) & ~4095
    # Exact 2048-byte slack, first alignment step over, and next-page fit.
    for name, length in (("FIT.BIN", page + 2048), ("NEAR.BIN", page + 2049), ("FAR.BIN", page + 4097)):
        data = bytearray(base) + bytearray(length - content)
        struct.pack_into("<Q", data, 16, length + 24)
        (share / name).write_bytes(data)
        off, end = (length + 7) & ~7, (length + 4095) & ~4095
        fits = off + 2048 <= end
        assert fits == (name != "NEAR.BIN")
        print(name, "content", length, "argv2048 slack", end - off - 2048)
    print("C1 cases:", len(cases))


def verify(rd):
    share = Path(os.environ.get("VG_SHARE") or rd / "share")
    expected = {tag: case for tag, case in json.loads((rd / "expected.json").read_text()).items()
                if case["group"] == os.environ["VG_TAG"]}
    serial = Path(os.environ["VG_SER"]).read_text()
    for tag, want in expected.items():
        out = (share / (tag + ".OUT")).read_bytes()
        err = (share / (tag + ".ERR")).read_bytes()
        normal = b"" if want["library"] else bytes.fromhex(want["out"])
        assert out == normal, (tag, "stdout")
        assert err == bytes.fromhex(want["err"]), (tag, "stderr", err[:256])
        if want["library"]:
            assert (share / want["library"]).read_bytes() == bytes.fromhex(want["out"]), (tag, "library golden")
        assert "oliver-probe: ok " + tag + "\n" in serial, (tag, "missing guest completion")
        shutil.copyfile(share / (tag + ".OUT"), ROOT / "artifacts" / ("live-oliver-" + tag + ".out"))
        shutil.copyfile(share / (tag + ".ERR"), ROOT / "artifacts" / ("live-oliver-" + tag + ".err"))
    assert (share / "UNWRITTEN").read_bytes() == b"prior-good-content"
    receipts = re.findall(r"arena_peak=(\d+) stack_high_water=(\d+) resources_peak=(\d+)", serial)
    assert len(receipts) == sum(x["status"] == 0 for x in expected.values())
    assert all(int(a) <= 8 * 1024 * 1024 and int(s) <= 128 * 1024 and int(r) <= 4 for a, s, r in receipts)
    files = re.findall(r"files_peak=(\d+)", serial)
    assert len(files) == len(receipts) and all(int(n) <= 2 for n in files)
    assert serial.count("tasks user-exec reaped\n") == 1 + sum(x["status"] >= 0 for x in expected.values())
    assert f"oliver-probe: done cases={len(expected)}\n" in serial
    pages = re.findall(r"^pages: armed=1 total=(0x[0-9a-f]+) free=(0x[0-9a-f]+)", serial, re.M)
    assert len(pages) >= 2 and pages[0] == pages[-1], ("post-exit page recovery", pages)
    print(f"byte-exact: {len(expected)} stdout/stderr pairs; {len(receipts)} budget receipts; pages recovered")


if __name__ == "__main__":
    globals()[sys.argv[1]](Path(sys.argv[2]).resolve())
