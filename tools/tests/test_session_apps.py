"""Human-session staging with fake per-app builders and no VM."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
APPS = {
    "GOTABWM": "gotabwm", "GOSH": "gosh", "GOCALC": "gocalc",
    "GOTERM": "goterm", "NOTE": "note", "GOEDIT": "goedit",
    "GOFILES": "files", "WEB": "web", "PULSE": "pulse", "GOHELP": "help",
    "GOTOP": "gotop", "GOSET": "goset", "GOVIEW": "goview",
}


class SessionAppsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for d in ("tools/go", "artifacts", "image", ".build/go", "zig-out/bin",
                  "host/vm-runner/.build/release"):
            (self.root / d).mkdir(parents=True, exist_ok=True)
        shutil.copy(ROOT / "tools/session.sh", self.root / "tools/session.sh")
        shutil.copy(ROOT / "image/apps.txt", self.root / "image/apps.txt")
        (self.root / "artifacts/disk.img").write_bytes(b"fake image")
        runner = self.root / "host/vm-runner/.build/release/VMRunner"
        runner.write_text("#!/bin/sh\nprintf 'fake runner: staging test only\\n'\n")
        runner.chmod(0o755)
        for name, builder in APPS.items():
            (self.root / f"tools/go/build-{builder}.sh").write_text(
                "#!/bin/sh\n"
                f"printf '{name} built\\n' > .build/go/{name}.ELF\n")
        self.share = self.root / "isolated-share"
        self.env = dict(os.environ, VIRELAI_SESSION_SHARE=str(self.share),
                        VIRELAI_SESSION_SKIP_BUILD="1")
        self.env.pop("VIRELAI_SESSION_NO_GOTABWM", None)
        self.env.pop("VIRELAI_SESSION_NO_TABWM", None)

    def run_session(self):
        result = subprocess.run(
            ["bash", str(self.root / "tools/session.sh")], env=self.env,
            capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn("command not found", result.stderr)
        return result.stdout

    def test_manifest_go_apps_and_available_zig_bundle_are_staged(self):
        (self.root / "zig-out/bin/TABWM.BIN").write_bytes(b"fallback")
        (self.root / "zig-out/bin/ZC.BIN").write_bytes(b"compiler")
        (self.root / "zig-out/bin/LD.SO").write_bytes(b"loader")
        output = self.run_session()
        for name in APPS:
            self.assertEqual((self.share / f"{name}.ELF").read_text(), f"{name} built\n")
            self.assertIn(f"session: staged {name}.ELF", output)
        self.assertEqual((self.share / "ZC.BIN").read_bytes(), b"compiler")
        self.assertEqual((self.share / "TABWM.BIN").read_bytes(), b"fallback")
        self.assertEqual((self.share / "APPS.TXT").read_bytes(),
                         (ROOT / "image/apps.txt").read_bytes())
        self.assertNotIn("tabwm start", (self.share / ".virelairc").read_text())

    def test_missing_failed_and_empty_builders_do_not_fake_success(self):
        (self.root / "tools/go/build-goterm.sh").unlink()
        (self.root / "tools/go/build-gocalc.sh").write_text("#!/bin/sh\nexit 1\n")
        (self.root / ".build/go/GOCALC.ELF").write_bytes(b"stale output")
        (self.root / "tools/go/build-note.sh").write_text("#!/bin/sh\nexit 0\n")
        output = self.run_session()
        for name in ("GOTERM", "GOCALC", "NOTE"):
            self.assertIn(f"{name}.ELF unavailable:", output)
            self.assertNotIn(f"session: staged {name}.ELF", output)
            self.assertFalse((self.share / f"{name}.ELF").exists())
        self.assertTrue((self.share / "GOFILES.ELF").exists())

    def test_no_documents_settings_or_startup_overwrite(self):
        self.share.mkdir()
        documents = {"SETTINGS.TXT": b"#v2\nwm=gotabwm\ntheme=light\n",
                     ".virelairc": b"# my startup\n", "NOTES.TXT": b"my notes\n",
                     "EDIT/DRAFT.TXT": b"my draft\n", "SESSION.TABS": b"my tabs\n"}
        for name, content in documents.items():
            target = self.share / name
            target.parent.mkdir(exist_ok=True)
            target.write_bytes(content)
            bundle = self.root / "zig-out/bin" / name
            bundle.parent.mkdir(exist_ok=True)
            bundle.write_bytes(b"must not overwrite")
        self.run_session()
        self.run_session()
        for name, content in documents.items():
            self.assertEqual((self.share / name).read_bytes(), content, name)

    def test_missing_seat_toolchain_uses_existing_fallback_contract(self):
        (self.root / "tools/go/build-gotabwm.sh").write_text(
            "#!/bin/sh\necho 'missing fork toolchain'\nexit 1\n")
        output = self.run_session()
        self.assertIn("GOTABWM.ELF unavailable: builder failed", output)
        self.assertIn("tabwm start", (self.share / ".virelairc").read_text())
        self.assertFalse((self.share / "GOTABWM.ELF").exists())


if __name__ == "__main__":
    unittest.main()
