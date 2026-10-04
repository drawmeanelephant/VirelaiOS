import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("fresh", Path(__file__).with_name("fresh.py"))
fresh = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fresh)


class FreshTests(unittest.TestCase):
    def test_rebuilds_instead_of_trusting_existing_elf(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td).resolve()
            for name in ("user/go", "tools/go/overlay/cmd/golang.org",
                         ".build/go", "go-wm-default/share"):
                (root / name).mkdir(parents=True)
            (root / "user/go/go.mod").write_text("module virelai\n")
            (root / "user/go/go.sum").write_text("")
            (root / "tools/go/build-app.sh").write_text("current recipe\n")
            src = root / ".build/go/APP.ELF"
            src.write_bytes(b"stale")
            built = b"\x7fELFcurrent"
            def build(command, check):
                self.assertTrue(check)
                self.assertEqual(command, ["bash", str(root / "tools/go/build-app.sh")])
                src.write_bytes(built)
            cwd = Path.cwd()
            try:
                os.chdir(root)
                with patch.dict(os.environ, {"RUN_DIR": str(root / "go-wm-default"),
                                             "VG_SHARE": str(root / "go-wm-default/share"),
                                             "VIRELAI_GATE_SUFFIX": ""}), \
                     patch.object(fresh.subprocess, "run", side_effect=build), \
                     patch.object(fresh.subprocess, "check_output", return_value="go fixture"):
                    fresh.stage({"APP": "build-app.sh"})
                    self.assertEqual((root / "go-wm-default/share/APP.ELF").read_bytes(), built)
                    receipt = json.loads((root / "artifacts/m91-workflow/fresh-default.json").read_text())
                    self.assertEqual(receipt["binaries"]["APP.ELF"], hashlib.sha256(built).hexdigest())
                    # A failed builder cannot silently stage yesterday's binary.
                    src.write_bytes(b"stale again")
                    with patch.object(fresh.subprocess, "run",
                                      side_effect=subprocess.CalledProcessError(1, "builder")):
                        with self.assertRaises(subprocess.CalledProcessError):
                            fresh.stage({"APP": "build-app.sh"})
                    self.assertEqual((root / "go-wm-default/share/APP.ELF").read_bytes(), built)
            finally:
                os.chdir(cwd)


if __name__ == "__main__":
    unittest.main()
