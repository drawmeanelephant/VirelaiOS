"""Class A: wrapper contracts with isolated build/VM/client stand-ins, no VM."""
import ast
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import tempfile
import time
import unittest


ROOT = Path(__file__).resolve().parents[2]


class RemoteTerminalTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "repo"
        self.home = Path(self.tmp.name) / "home"
        self.home.mkdir()
        self.share = (Path(self.tmp.name) / "remote share").resolve()
        self.root.mkdir()
        for name in ("tools/remote-terminal.sh", "tools/remote-terminal-tape.sh"):
            dst = self.root / name
            dst.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / name, dst)
        (self.root / "tools/env-check.sh").write_text(":\n")
        (self.root / "tools/go").mkdir()
        (self.root / "tools/go/ensure-guest-elf.sh").write_text(
            'mkdir -p .build/go; printf fresh-gosh > .build/go/GOSH.ELF\n')
        (self.root / "tools/mkhello-elf.py").write_text(
            'import pathlib,sys; pathlib.Path(sys.argv[1]).write_bytes(b"hello-elf")\n')
        bundle = self.root / "zig-out/bin"
        bundle.mkdir(parents=True)
        for name in ("LD.SO", "LIBUI.SO", "LIBFONT.SO", "SSH.BIN", "GOTABWM.ELF"):
            (bundle / name).write_bytes(name.encode())
        (self.root / "tools/console-client").mkdir()
        (self.root / "tools/console-client/client.py").write_text(
            'import json,os,sys; print(json.dumps(sys.argv[1:])); sys.exit(int(os.environ.get("CLIENT_RC","0")))\n')
        (self.root / "host/vm-runner/.build/release").mkdir(parents=True)
        self.runner = self.root / "host/vm-runner/.build/release/VMRunner"
        self.runner.write_text("""#!/usr/bin/env python3
import json,os,pathlib,signal,socket,sys,time
a=sys.argv[1:]
def val(flag): return a[a.index(flag)+1]
pathlib.Path("args.json").write_text(json.dumps(a))
secret=pathlib.Path(val("--console-tcp-secret-file")).read_text().strip()
pathlib.Path("env.json").write_text(json.dumps(
    {"secret_in_environment": any(secret in value for value in os.environ.values())}))
s=socket.socket(); s.bind(("127.0.0.1",int(val("--console-tcp").split(":")[1]))); s.listen()
pathlib.Path(val("--serial")).write_bytes(b"gosh: attached\\ngosh: prompt\\ngosh> ")
signal.signal(signal.SIGTERM,lambda *_:sys.exit(0))
while True: time.sleep(.05)
""")
        self.runner.chmod(0o755)
        self.bin = self.root / "fake-bin"
        self.bin.mkdir()
        for name, body in {"zig": "exit 0", "swift": "exit 0", "codesign": "exit 0",
                           "uname": "echo arm64", "sw_vers": "echo 27.2"}.items():
            p = self.bin / name
            p.write_text("#!/bin/sh\n" + body + "\n")
            p.chmod(0o755)
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            self.port = s.getsockname()[1]
        self.env = dict(os.environ, HOME=str(self.home), PATH=str(self.bin) + ":" + os.environ["PATH"])
        self.state = self.home / ".virelai/remote-terminal" / hashlib.sha256(os.fsencode(self.share)).hexdigest()

    def run_script(self, action, *extra, env=None):
        return subprocess.run(["bash", str(self.root / "tools/remote-terminal.sh"),
                               action, "--share", str(self.share), *extra],
                              cwd=self.root, env=env or self.env, capture_output=True, timeout=10)

    def serve(self):
        p = subprocess.Popen(["bash", str(self.root / "tools/remote-terminal.sh"),
                              "serve", "--share", str(self.share), "--port", str(self.port)],
                             cwd=self.root, env=self.env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.addCleanup(self.stop, p)
        deadline = time.monotonic() + 8
        while not (self.state / "serial.log").exists():
            if p.poll() is not None:
                self.fail("serve failed: " + repr(p.communicate()))
            if time.monotonic() > deadline:
                self.fail("serve startup deadline")
            time.sleep(.02)
        return p

    @staticmethod
    def stop(p):
        if p.poll() is None:
            p.send_signal(signal.SIGTERM)
        p.communicate(timeout=15)

    def test_seed_remote_share_without_clobber(self):
        p = self.serve()
        self.assertEqual((self.share / "SETTINGS.TXT").read_bytes(), b"#v2\nwm=none\nshell=sh\n")
        self.assertEqual((self.share / "GOSH.ELF").read_bytes(), b"fresh-gosh")
        self.assertEqual((self.share / "HELLO.ELF").read_bytes(), b"hello-elf")
        for name in ("LD.SO", "LIBUI.SO", "LIBFONT.SO", "SSH.BIN"):
            self.assertEqual((self.share / name).read_bytes(), name.encode())
        self.assertFalse((self.share / "GOTABWM.ELF").exists())
        self.stop(p)
        for name in ("DOCUMENT.TXT", "GOSH-HISTORY.TXT", ".virelairc"):
            (self.share / name).write_bytes(b"# owner bytes\n")
        p = self.serve()
        self.stop(p)
        for name in ("DOCUMENT.TXT", "GOSH-HISTORY.TXT", ".virelairc"):
            self.assertEqual((self.share / name).read_bytes(), b"# owner bytes\n")
        settings = b"#v2\nwm=gotabwm\nshell=sh\n"
        (self.share / "SETTINGS.TXT").write_bytes(settings)
        r = self.run_script("serve")
        self.assertNotEqual(r.returncode, 0)
        self.assertIn(b"incompatible share", r.stderr)
        self.assertEqual((self.share / "SETTINGS.TXT").read_bytes(), settings)
        (self.share / "SETTINGS.TXT").write_bytes(b"#v2\nwm=none\nshell=sh\n")
        (self.share / ".virelairc").write_text("tabwm start\n")
        self.assertIn(b".virelairc", self.run_script("serve").stderr)
        self.assertEqual((self.share / ".virelairc").read_text(), "tabwm start\n")

    def test_loopback_private_secret_and_no_argv_leak(self):
        for port in ("0", "65536", "x", "127.0.0.1:23", "1:secret"):
            self.assertNotEqual(self.run_script("serve", "--port", port).returncode, 0)
        p = self.serve()
        secret = (self.state / "console.secret").read_bytes().strip()
        self.assertEqual((self.state.stat().st_mode & 0o777), 0o700)
        self.assertEqual(((self.state / "console.secret").stat().st_mode & 0o777), 0o600)
        args = json.loads((self.root / "args.json").read_text())
        self.assertEqual(args[args.index("--console-tcp") + 1], f"127.0.0.1:{self.port}")
        self.assertEqual(args[args.index("--timeout") + 1], "0")
        self.assertNotIn("--display", args)
        self.assertNotIn("--input", args)
        self.assertFalse(secret in (self.root / "args.json").read_bytes(), "secret leaked in argv")
        self.assertFalse(json.loads((self.root / "env.json").read_text())["secret_in_environment"])
        for _ in range(2):
            r = self.run_script("attach")
            self.assertEqual(r.returncode, 0, r.stderr)
            self.assertFalse(secret in r.stdout + r.stderr, "secret leaked in client output")
        self.assertEqual((self.state / "console.secret").read_bytes().strip(), secret)
        p.send_signal(signal.SIGINT)
        output = p.communicate(timeout=10)
        self.assertFalse(secret in b"".join(output), "secret leaked in service output")
        self.assertFalse(self.state.exists())

    def test_attach_uses_console_client_contract(self):
        self.share.mkdir()
        (self.share / "SETTINGS.TXT").write_text("#v2\nwm=none\nshell=sh\n")
        self.assertIn(b"bridge missing", self.run_script("attach").stderr)
        p = self.serve()
        r = self.run_script("attach")
        self.assertEqual(json.loads(r.stdout), ["--addr", f"127.0.0.1:{self.port}",
                                               "--secret-file", str(self.state / "console.secret")])
        self.assertEqual(self.run_script("attach", env=dict(self.env, CLIENT_RC="1")).returncode, 1)
        # The shell's code heredoc must not replace the client's caller input.
        (self.root / "tools/console-client/client.py").write_text(
            "import sys; sys.stdout.buffer.write(sys.stdin.buffer.read())\n")
        r = subprocess.run(["bash", str(self.root / "tools/remote-terminal.sh"), "attach",
                            "--share", str(self.share)], env=self.env, input=b"operator-input",
                           capture_output=True, timeout=5)
        self.assertEqual(r.stdout, b"operator-input")
        self.assertEqual(r.returncode, 0)
        (self.state / "serial.log").write_text("gosh: attached\ngosh: close\nvirelai> ")
        self.assertIn(b"bare monitor", self.run_script("attach").stderr)
        self.stop(p)
        self.assertIn(b"bridge missing", self.run_script("attach").stderr)

    def test_disconnect_does_not_stop_server(self):
        sentinel = self.home / "keep-me"
        sentinel.write_text("owner")
        p = self.serve()
        runner = json.loads((self.state / "session.json").read_text())["runner"]
        for _ in range(2):
            self.assertEqual(self.run_script("attach").returncode, 0)
            self.assertIsNone(p.poll())
            os.kill(runner, 0)
        self.assertIn(b"session occupied", self.run_script("serve", "--port", str(self.port)).stderr)
        other = Path(self.tmp.name) / "other-share"
        r = subprocess.run(["bash", str(self.root / "tools/remote-terminal.sh"), "serve", "--share",
                            str(other), "--port", str(self.port)], env=self.env, capture_output=True, timeout=10)
        self.assertIn(b"port occupied", r.stderr)
        self.stop(p)
        self.assertFalse(self.state.exists())
        self.assertTrue((self.share / "GOSH.ELF").exists())
        self.assertEqual(sentinel.read_text(), "owner")
        with self.assertRaises(ProcessLookupError):
            os.kill(runner, 0)
        # Unknown stale state must never be deleted or silently resecreted.
        self.state.mkdir(mode=0o700)
        (self.state / "owner-file").write_text("preserve")
        self.assertIn(b"stale session", self.run_script("serve", "--port", str(self.port)).stderr)
        self.assertEqual((self.state / "owner-file").read_text(), "preserve")
        # Even an unresponsive child must not strand the service in wait().
        stale = self.state
        self.share = (Path(self.tmp.name) / "stubborn-share").resolve()
        self.state = self.home / ".virelai/remote-terminal" / hashlib.sha256(os.fsencode(self.share)).hexdigest()
        self.runner.write_text(self.runner.read_text().replace(
            "signal.signal(signal.SIGTERM,lambda *_:sys.exit(0))",
            "signal.signal(signal.SIGTERM,signal.SIG_IGN)"))
        p = self.serve()
        started = time.monotonic()
        self.stop(p)
        self.assertLess(time.monotonic() - started, 13)
        self.assertFalse(self.state.exists())
        self.assertEqual((stale / "owner-file").read_text(), "preserve")

    def test_remote_acceptance_has_no_local_success_fallback(self):
        tape = self.root / "tools/remote-terminal-tape.sh"
        r = subprocess.run(["bash", str(tape)], env=self.env, capture_output=True, timeout=5)
        self.assertEqual(r.returncode, 2)
        self.assertIn(b"BLOCKED class C", r.stderr)
        self.assertIn(b"no second-machine SSH target", r.stderr)
        self.assertFalse((self.root / "artifacts/remote-terminal").exists())
        fake_ssh = self.bin / "ssh"
        fake_ssh.write_text("#!/usr/bin/env python3\nimport json,shlex,sys\n"
                            "code=shlex.split(sys.argv[-1])[2].split('import fcntl, json, os')[0]\n"
                            "exec(code)\nprint(json.dumps(dict(machine=machine_identity())))\n")
        fake_ssh.chmod(0o755)
        r = subprocess.run(["bash", str(tape), "--ssh-target", "operator@alias", "--host-repo",
                            str(self.root), "--share", str(self.share), "--fixture-only"],
                           env=self.env, capture_output=True, timeout=5)
        self.assertEqual(r.returncode, 2)
        self.assertIn(b"same machine", r.stderr)
        text = tape.read_text()
        self.assertIn('"StrictHostKeyChecking=yes"', text)
        self.assertIn('"BatchMode=yes"', text)
        self.assertNotIn("StrictHostKeyChecking=no", text)
        # Pin the tape's command-completion boundary without a VM/SSH peer.
        # A redraw while typing is not completion; results may begin directly
        # after the submitted-line LF (there need not be a second newline).
        source = text.split("<<'PY'\n", 1)[1].rsplit("\nPY", 1)[0]
        node = next(n for n in ast.parse(source).body if isinstance(n, ast.ClassDef) and n.name == "Session")
        namespace = {"time": time}
        exec(compile(ast.Module(body=[node], type_ignores=[]), str(tape), "exec"), namespace)
        session = namespace["Session"].__new__(namespace["Session"])
        session.data = bytearray()
        session.send = lambda _: session.data.extend(
            b"\rgosh> whoami\r\ngosh: line whoami\nuid=1000 user\ngosh> ")
        session.drain = lambda *_: self.fail("completion matcher missed already-buffered guest result")
        session.line(b"whoami", b"\nuid=")


if __name__ == "__main__":
    unittest.main()
