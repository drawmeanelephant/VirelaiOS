"""Private, hash-checked compiler-rt reduction polls; never edit SDK cache."""
import os
from pathlib import Path
import re
import shutil

from materialize import LOCK, sha


def prepare(compiler, work):
    name = "lib/compiler_rt/rem_pio2_large.zig"
    original = (compiler / name).read_bytes()
    if sha(original) != LOCK["math_poll"]["original_sha256"]:
        raise ValueError("SourceDrift: compiler-rt reduction")
    destination = work / "math-library"
    if not destination.exists():
        # Hard links only save space for immutable inputs. The one changed
        # file is replaced through a new inode below, never written in place.
        shutil.copytree(compiler / "lib", destination / "lib", copy_function=os.link)
    base_files = {path.relative_to(compiler).as_posix(): path for path in (compiler / "lib").rglob("*") if path.is_file()}
    private_files = {path.relative_to(destination).as_posix(): path for path in (destination / "lib").rglob("*") if path.is_file() or path.is_symlink()}
    if set(base_files) != set(private_files):
        raise ValueError("SourceDrift: private math library file set")
    for relative, source in base_files.items():
        if relative == name:
            continue
        target = private_files[relative]
        if target.is_symlink() or not os.path.samestat(source.stat(), target.stat()):
            raise ValueError("SourceDrift: private math library " + relative)
    text = original.decode()
    # Every while in this pinned file has a braced body. The continuation
    # expression can itself contain braces, so skip its balanced parentheses.
    clean = re.sub(r"//[^\n]*", lambda m: " " * len(m.group()), text)
    offsets = []
    for match in re.finditer(r"\bwhile\s*\(", clean):
        index = clean.index("(", match.start())
        depth = 1
        index += 1
        while depth:
            depth += (clean[index] == "(") - (clean[index] == ")")
            index += 1
        while clean[index].isspace():
            index += 1
        if clean[index] == ":":
            index += 1
            while clean[index].isspace():
                index += 1
            if clean[index] != "(":
                raise ValueError("MathPollAnchorDrift")
            depth = 1
            index += 1
            while depth:
                depth += (clean[index] == "(") - (clean[index] == ")")
                index += 1
            while clean[index].isspace():
                index += 1
        if clean[index] != "{":
            raise ValueError("MathPollAnchorDrift")
        offsets.append(index + 1)
    if len(offsets) != LOCK["math_poll"]["while_sites"]:
        raise ValueError("MathPollAnchorDrift")
    for offset in reversed(offsets):
        text = text[:offset] + "\n        if (!qjs_poll_reduction(y)) return 0;\n" + text[offset:]
    text += """
extern fn qjs_native_checkpoint() callconv(.c) c_int;
fn qjs_poll_reduction(y: []f64) bool {
    if (qjs_native_checkpoint() == 0) return true;
    for (y) |*value| value.* = math.nan(f64);
    return false;
}
"""
    target = destination / name
    if target.is_symlink() or sha(target.read_bytes()) not in {sha(original), sha(text.encode())}:
        raise ValueError("SourceDrift: private math reduction")
    temporary = target.with_suffix(".qjs-new")
    temporary.write_text(text)
    temporary.replace(target)
    return destination / "lib", {"original_sha256": sha(original), "patched_sha256": sha(text.encode()),
                                "while_sites": len(offsets), "path": name}
