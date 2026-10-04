"""Source-boundary tests, not runtime or guest acceptance."""
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import struct
import sys
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import build
import materialize


def symbols(data):
    if data[:7] != b"\x7fELF\x02\x01\x01":
        raise ValueError("UnexpectedObjectFormat")
    if struct.unpack_from("<HH", data, 16) != (1, 183):
        raise ValueError("UnexpectedObjectTarget")
    offset = struct.unpack_from("<Q", data, 40)[0]
    width, count = struct.unpack_from("<HH", data, 58)
    sections = [struct.unpack_from("<II4QII2Q", data, offset + i * width) for i in range(count)]
    defined, undefined = set(), set()
    for section in sections:
        if section[1] != 2:
            continue
        strings = sections[section[6]]
        table = data[strings[4]:strings[4] + strings[5]]
        for pos in range(section[4], section[4] + section[5], section[9]):
            name, info, _other, index, _value, _size = struct.unpack_from("<IBBHQQ", data, pos)
            if name and info >> 4 in (1, 2):
                name = table[name:table.index(b"\0", name)].decode()
                (defined if index else undefined).add(name)
    return defined, undefined


class SourceTests(unittest.TestCase):
    def test_original_bytes_and_full_manifest(self):
        manifest = (materialize.INPUT / "upstream.sha256").read_bytes()
        self.assertEqual(materialize.sha(manifest), materialize.LOCK["upstream"]["manifest_sha256"])
        self.assertEqual(len(manifest.splitlines()), 77)
        names = []
        for line in manifest.splitlines():
            digest, name = line.split(b"  ", 1)
            self.assertEqual(len(digest), 64)
            names.append(name)
        self.assertEqual(sorted(names), names)
        for name, digest in materialize.LOCK["upstream"]["files"].items():
            self.assertEqual(materialize.sha((materialize.INPUT / "upstream" / name).read_bytes()), digest)

    def test_function_anchor_ignores_braces_in_strings_and_comments(self):
        text = 'static void fixture(int x)\n{ /* } */ const char *s = "}"; if (x) {} }\nvoid next(void) {}\n'
        result = materialize.function_body(text, "fixture", "    return;")
        self.assertEqual(result, "static void fixture(int x)\n{\n    return;\n}\nvoid next(void) {}\n")
        with self.assertRaisesRegex(ValueError, "PatchAnchorDrift"):
            materialize.function_body(text + text, "fixture", "    return;")
        with self.assertRaisesRegex(ValueError, "PatchAnchorDrift"):
            materialize.function_body(text, "absent", "")

    def test_offline_source_materialization_and_core_exclusions(self):
        with tempfile.TemporaryDirectory() as temp:
            source = materialize.materialize(Path(temp))
            self.assertEqual({p.name for p in source.iterdir()}, set(materialize.LOCK["upstream"]["files"]))
            text = (source / "quickjs.c").read_text()
            self.assertNotRegex(text, r"(?m)^#define CONFIG_ATOMICS")
            self.assertIn("#define CONFIG_STACK_CHECK", text)
            self.assertIn("#define JS_INTERRUPT_COUNTER_INIT 1000\n", text)
            self.assertIn("ctx->random_state = 1;", text)
            for token in ("gettimeofday(", "localtime_r(", "mktime("):
                self.assertNotIn(token, text)
            for token in ("sys/time.h", "time.h", "fenv.h"):
                self.assertNotIn("#include <" + token + ">", text)
            self.assertNotIn("JS_DumpMemoryUsage", (source / "quickjs.h").read_text())
            self.assertIn("#define INTERRUPT_COUNTER_INIT 1000\n", (source / "libregexp.c").read_text())

    def test_source_drift_and_missing_source_fail_offline(self):
        with tempfile.TemporaryDirectory() as temp:
            input_dir = Path(temp) / "inputs"
            input_dir.mkdir()
            (input_dir / "upstream").mkdir()
            (input_dir / "upstream.sha256").write_bytes((materialize.INPUT / "upstream.sha256").read_bytes())
            with patch.object(materialize, "INPUT", input_dir):
                with self.assertRaisesRegex(ValueError, "MissingPinnedSource"):
                    materialize.materialize(Path(temp) / "build")
                for name in materialize.LOCK["upstream"]["files"]:
                    (input_dir / "upstream" / name).write_bytes(b"drift")
                with self.assertRaisesRegex(ValueError, "SourceDrift"):
                    materialize.materialize(Path(temp) / "build")
                (input_dir / "upstream.sha256").write_bytes(b"drift")
                with self.assertRaisesRegex(ValueError, "SourceDrift: upstream manifest"):
                    materialize.materialize(Path(temp) / "build")

    def test_native_recipe_is_isolated_and_x18_reserved(self):
        required = ("-ffreestanding", "-fno-builtin", "-fwrapv", "-funsigned-char",
                    "-fno-stack-protector", "-fno-pic", "-fno-pie", "-fno-unwind-tables",
                    "-ffixed-x18", "-nostdinc", "-fno-lto")
        for flag in required:
            self.assertIn(flag, build.C_FLAGS)
        for flag in ("-lc", "-lm", "-ffast-math", "-D_GNU_SOURCE", "-D__EMSCRIPTEN__"):
            self.assertNotIn(flag, build.C_FLAGS)
        env = dict(ZIG_LIB_DIR="untrusted", NIX_PATH="untrusted", SDKROOT="untrusted",
                   CPATH="untrusted", C_INCLUDE_PATH="untrusted", PATH="/compiler")
        with patch.dict(build.os.environ, env, clear=True):
            self.assertEqual(build.environment(), {"PATH": "/compiler"})


if __name__ == "__main__":
    unittest.main()
