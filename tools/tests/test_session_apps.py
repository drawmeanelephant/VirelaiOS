"""Human-session staging with fake per-app builders and no VM."""
import os
import plistlib
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
                  "host/vm-runner/.build/release", "host/vm-runner/Resources", "fake-bin",
                  "run-temp"):
            (self.root / d).mkdir(parents=True, exist_ok=True)
        shutil.copy(ROOT / "tools/session.sh", self.root / "tools/session.sh")
        shutil.copy(ROOT / "image/apps.txt", self.root / "image/apps.txt")
        shutil.copy(ROOT / "host/vm-runner/Resources/Session-Info.plist",
                    self.root / "host/vm-runner/Resources/Session-Info.plist")
        (self.root / "host/vm-runner/entitlements.plist").write_text("fixture entitlements")
        # No host signing/service changes during unit tests.
        signer = self.root / "fake-bin/codesign"
        signer.write_text("#!/bin/sh\nprintf 'fixture codesign\\n'\n")
        signer.chmod(0o755)
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
                        VIRELAI_SESSION_SKIP_BUILD="1",
                        TMPDIR=str(self.root / "run-temp"),
                        PATH=str(self.root / "fake-bin") + os.pathsep + os.environ["PATH"])
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

    def test_runner_does_not_inherit_parent_bundle_identity(self):
        self.env["__CFBundleIdentifier"] = "com.electron.factory"
        runner = self.root / "host/vm-runner/.build/release/VMRunner"
        runner.write_text(
            "#!/bin/sh\n"
            "printf 'runner bundle=%s\\n' \"${__CFBundleIdentifier-unset}\"\n"
            "printf 'runner share=%s\\n' \"$VIRELAI_SESSION_SHARE\"\n")
        output = self.run_session()
        self.assertIn("runner bundle=unset", output)
        self.assertIn(f"runner share={self.share}", output)
        self.assertEqual(self.env["__CFBundleIdentifier"], "com.electron.factory")

    def test_distinct_bundle_and_host_mode_keep_cli_entrypoint(self):
        runner = self.root / "host/vm-runner/.build/release/VMRunner"
        original = (
            "#!/bin/sh\n"
            "printf 'bundle exec=%s\\n' \"$0\"\n"
            "printf 'flags=%s\\n' \"$*\"\n"
            "cp \"$(dirname \"$0\")/../Info.plist\" \"$VIRELAI_SESSION_SHARE/fixture.plist\"\n")
        runner.write_text(original)
        output = self.run_session()
        self.assertIn("VirelaiOS.app/Contents/MacOS/VirelaiOS", output)
        self.assertIn("--display --input --host-session --timeout 0", output)
        info = plistlib.loads((self.share / "fixture.plist").read_bytes())
        self.assertEqual(info["CFBundleIdentifier"], "org.virelaios.host")
        self.assertEqual(info["CFBundleName"], "VirelaiOS")
        self.assertEqual(info["CFBundleExecutable"], "VirelaiOS")
        self.assertEqual(runner.read_text(), original)
        self.assertEqual(list((self.root / "run-temp").iterdir()), [])

    def test_runner_failure_is_reported_and_run_directory_removed(self):
        runner = self.root / "host/vm-runner/.build/release/VMRunner"
        runner.write_text("#!/bin/sh\necho 'fixture startup error' >&2\nexit 7\n")
        result = subprocess.run(
            ["bash", str(self.root / "tools/session.sh")], env=self.env,
            capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 7)
        self.assertIn("fixture startup error", result.stderr)
        self.assertIn("VM runner exited with status 7", result.stderr)
        self.assertEqual(list((self.root / "run-temp").iterdir()), [])

    def test_terminal_stop_waits_for_owned_runner_before_cleanup(self):
        import signal
        import time
        runner = self.root / "host/vm-runner/.build/release/VMRunner"
        runner.write_text(
            "#!/bin/bash\n"
            "trap 'test -f \"$0\" && echo stopped > \"$VIRELAI_SESSION_SHARE/stopped\"; exit 143' TERM\n"
            "echo $$ > \"$VIRELAI_SESSION_SHARE/owned-pid\"\n"
            "while :; do sleep 0.05; done\n")
        with tempfile.TemporaryFile(mode="w+") as log:
            process = subprocess.Popen(
                ["bash", str(self.root / "tools/session.sh")], env=self.env,
                stdout=log, stderr=log)
            try:
                deadline = time.monotonic() + 10
                while not (self.share / "owned-pid").exists() and time.monotonic() < deadline:
                    time.sleep(0.02)
                self.assertTrue((self.share / "owned-pid").exists())
                pid = int((self.share / "owned-pid").read_text())
                process.send_signal(signal.SIGTERM)
                self.assertEqual(process.wait(timeout=10), 143)
                self.assertEqual((self.share / "stopped").read_text(), "stopped\n")
                with self.assertRaises(ProcessLookupError):
                    os.kill(pid, 0)
                self.assertEqual(list((self.root / "run-temp").iterdir()), [])
            finally:
                if process.poll() is None:
                    process.terminate()
                    process.wait(timeout=10)


if __name__ == "__main__":
    unittest.main()
