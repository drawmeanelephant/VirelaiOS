"""Host socket + PTY acceptance tests. These are not guest PTYs or VZ evidence."""

from contextlib import contextmanager
import hashlib
import hmac
import os
from pathlib import Path
import pty
import select
import signal
import socket
import subprocess
import sys
import tempfile
import termios
import threading
import time
import unittest

import client


CLIENT = Path(__file__).with_name("client.py").resolve()
SECRET = b"host-test-private-key"
CHALLENGE = bytes(range(32))
SCHEME = b"VIRELAIOS-AUTH/1 hmac-sha256"  # Independent wire oracle, not client.SCHEME.
CHALLENGE_LINE = SCHEME + b" " + CHALLENGE.hex().encode() + b"\n"
MAC = hmac.new(SECRET, SCHEME + b"\x00" + CHALLENGE,
               hashlib.sha256).hexdigest().encode() + b"\n"
PROMPT = b"\x1b[32mvirelai>\x1b[0m "


def read_exact(sock, length):
    data = bytearray()
    while len(data) < length:
        chunk = sock.recv(length - len(data))
        if not chunk:
            raise AssertionError("unexpected fake-bridge EOF")
        data.extend(chunk)
    return bytes(data)


def read_to_eof(sock):
    data = bytearray()
    while True:
        chunk = sock.recv(4096)
        if not chunk:
            return bytes(data)
        data.extend(chunk)


def authenticated(sock, first=PROMPT, split=False):
    if split:
        for chunk in (CHALLENGE_LINE[:13], CHALLENGE_LINE[13:49],
                      CHALLENGE_LINE[49:]):
            sock.sendall(chunk)
    else:
        sock.sendall(CHALLENGE_LINE)
    answer = read_exact(sock, len(MAC))
    repaint = read_exact(sock, 1)
    if answer != MAC or repaint != b"\x0c":
        raise AssertionError("client did not send exact HMAC line + Ctrl-L")
    if first:
        sock.sendall(first)


class FakeBridge:
    """One actual loopback listener; report thread assertions to the test."""

    def __init__(self, serve):
        self.serve = serve
        self.listener = socket.socket()
        self.listener.bind(("127.0.0.1", 0))
        self.listener.listen(1)
        self.listener.settimeout(5)
        self.addr = "127.0.0.1:" + str(self.listener.getsockname()[1])
        self.error = None
        self.thread = threading.Thread(target=self.run, daemon=True)

    def run(self):
        try:
            with self.listener.accept()[0] as peer:
                peer.settimeout(5)
                self.serve(peer)
        except BaseException as error:
            self.error = error

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, kind, value, traceback):
        self.thread.join(6)
        self.listener.close()
        if kind is None:
            if self.thread.is_alive():
                raise AssertionError("fake bridge did not finish")
            if self.error:
                raise self.error


class TerminalClient:
    def __init__(self, command):
        self.master, self.slave = pty.openpty()
        # Use nondefault state so cleanup must restore the exact saved state,
        # not a guessed cooked-mode configuration.
        original = termios.tcgetattr(self.slave)
        original[3] &= ~termios.ECHO
        original[6][termios.VEOF] = b"\x07"
        termios.tcsetattr(self.slave, termios.TCSANOW, original)
        self.original = termios.tcgetattr(self.slave)
        self.output = bytearray()
        self.process = subprocess.Popen(command, stdin=self.slave, stdout=self.slave,
                                        stderr=subprocess.PIPE)

    def read_until(self, needle, timeout=4):
        deadline = time.monotonic() + timeout
        while needle not in self.output:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise AssertionError("client PTY output deadline")
            if select.select([self.master], [], [], remaining)[0]:
                chunk = os.read(self.master, 65536)
                if not chunk:
                    raise AssertionError("client PTY EOF")
                self.output.extend(chunk)
        return bytes(self.output)

    def wait_raw(self):
        deadline = time.monotonic() + 4
        while time.monotonic() < deadline:
            attributes = termios.tcgetattr(self.slave)
            if not attributes[3] & (termios.ICANON | termios.ISIG | termios.ECHO):
                return
            if self.process.poll() is not None:
                raise AssertionError("client exited before raw mode")
            time.sleep(0.005)
        raise AssertionError("client never entered raw mode")

    def finish(self):
        code = self.process.wait(timeout=5)
        errors = self.process.stderr.read()
        if termios.tcgetattr(self.slave) != self.original:
            raise AssertionError("client failed to restore exact terminal attributes")
        return code, errors

    def close(self):
        if self.process.poll() is None:
            self.process.terminate()
            self.process.wait(timeout=5)
        self.process.stderr.close()
        os.close(self.master)
        os.close(self.slave)


class ClientTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.secret = Path(self.directory.name) / "console.secret"
        self.secret.write_bytes(SECRET + b"\r\nignored second line\n")
        self.secret.chmod(0o600)

    def command(self, addr, timeout=2, shim=None):
        arguments = ["--addr", addr, "--secret-file", str(self.secret),
                     "--timeout", str(timeout)]
        if shim:
            return [sys.executable, "-u", "-c", shim, str(CLIENT), *arguments]
        return [sys.executable, "-u", str(CLIENT), *arguments]

    @contextmanager
    def terminal(self, bridge, **kwargs):
        terminal = TerminalClient(self.command(bridge.addr, **kwargs))
        try:
            yield terminal
        finally:
            terminal.close()

    def pipe(self, bridge, payload=b"", **kwargs):
        return subprocess.run(self.command(bridge.addr, **kwargs), input=payload,
                              capture_output=True, timeout=8)

    def assert_private(self, result):
        self.assertNotIn(SECRET, result.stdout + result.stderr)
        self.assertNotIn(MAC.strip(), result.stdout + result.stderr)

    def test_hmac_challenge_and_secret_file(self):
        def serve(peer):
            authenticated(peer, split=True)
            self.assertEqual(read_to_eof(peer), b"")

        with FakeBridge(serve) as bridge:
            command = self.command(bridge.addr)
            self.assertNotIn(SECRET.decode(), " ".join(command))
            result = self.pipe(bridge)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, PROMPT)
        self.assert_private(result)

        for mode in (0o644, 0o620, 0o601):
            with self.subTest(mode=oct(mode)):
                self.secret.chmod(mode)
                result = subprocess.run(self.command("127.0.0.1:1"), input=b"",
                                        capture_output=True, timeout=3)
                self.assertEqual(result.returncode, 1)
                self.assertIn(b"secret file insecure", result.stderr)
                self.assert_private(result)
        self.secret.chmod(0o600)
        for data, diagnostic in ((b"\xff\n", b"must contain UTF-8"),
                                 (b"x" * 4097, b"first line too long")):
            with self.subTest(secret_error=diagnostic):
                self.secret.write_bytes(data)
                result = subprocess.run(self.command("127.0.0.1:1"), input=b"",
                                        capture_output=True, timeout=3)
                self.assertEqual(result.returncode, 1)
                self.assertIn(diagnostic, result.stderr)
        for data in (b"", b"\n", b"\r\n"):
            with self.subTest(empty=data):
                self.secret.write_bytes(data)
                result = subprocess.run(self.command("127.0.0.1:1"), input=b"",
                                        capture_output=True, timeout=3)
                self.assertIn(b"secret file empty", result.stderr)
        self.secret.unlink()
        result = subprocess.run(self.command("127.0.0.1:1"), input=b"",
                                capture_output=True, timeout=3)
        self.assertIn(b"secret file missing", result.stderr)
        os.mkfifo(self.secret, 0o600)
        result = subprocess.run(self.command("127.0.0.1:1"), input=b"",
                                capture_output=True, timeout=3)
        self.assertIn(b"secret file insecure", result.stderr)
        self.secret.unlink()
        target = Path(self.directory.name) / "target"
        target.write_bytes(SECRET)
        target.chmod(0o600)
        self.secret.symlink_to(target)
        result = subprocess.run(self.command("127.0.0.1:1"), input=b"",
                                capture_output=True, timeout=3)
        self.assertIn(b"secret file insecure", result.stderr)

    def test_bad_scheme_length_and_auth_failure(self):
        malformed = (
            b"VIRELAIOS-AUTH/2 hmac-sha256 " + b"ab" * 32 + b"\n",
            b"VIRELAIOS-AUTH/1 none " + b"ab" * 32 + b"\n",
            SCHEME + b" " + b"ab" * 31 + b"\n",
            SCHEME + b" " + b"ab" * 33 + b"\n",
            SCHEME + b" " + b"zz" * 32 + b"\n",
            SCHEME + b"  " + b"ab" * 32 + b"\n",
            SCHEME + b" " + b"ab" * 32 + b"\r\n",
            b"x" * 161,
        )
        for line in malformed:
            with self.subTest(challenge=line[:30]):
                def serve(peer):
                    peer.sendall(line)
                    self.assertEqual(read_to_eof(peer), b"")
                with FakeBridge(serve) as bridge, self.terminal(bridge) as terminal:
                    code, errors = terminal.finish()
                self.assertEqual(code, 1)
                self.assertIn(b"challenge", errors)
                self.assertNotIn(line.rstrip(b"\n"), errors)

        for refusal in (b"console-tcp: auth failed\n", b"console-tcp: auth timeout\n"):
            with self.subTest(refusal=refusal):
                def serve(peer):
                    authenticated(peer, first=None)
                    for byte in refusal:
                        peer.sendall(bytes([byte]))
                        time.sleep(0.001)
                    self.assertEqual(read_to_eof(peer), b"")
                with FakeBridge(serve) as bridge, self.terminal(bridge) as terminal:
                    code, errors = terminal.finish()
                self.assertEqual(code, 1)
                self.assertIn(b"auth", errors)

        for stage in ("challenge", "first output", "slow challenge"):
            with self.subTest(timeout_stage=stage):
                def serve(peer):
                    if stage == "first output":
                        authenticated(peer, first=None)
                    elif stage == "slow challenge":
                        for byte in CHALLENGE_LINE[:3]:
                            peer.sendall(bytes([byte]))
                            time.sleep(0.06)
                    self.assertEqual(read_to_eof(peer), b"")
                with FakeBridge(serve) as bridge, self.terminal(bridge, timeout=0.15) as terminal:
                    code, errors = terminal.finish()
                self.assertEqual(code, 1)
                self.assertIn(b"timeout", errors)

    def test_busy_and_eof_restore_terminal(self):
        for response, diagnostic in ((b"console-tcp: busy\n", b"busy"), (b"", b"EOF")):
            with self.subTest(response=response):
                def serve(peer):
                    if response:
                        for byte in response:
                            peer.sendall(bytes([byte]))
                    peer.shutdown(socket.SHUT_WR)
                    self.assertEqual(read_to_eof(peer), b"")
                with FakeBridge(serve) as bridge, self.terminal(bridge) as terminal:
                    code, errors = terminal.finish()
                self.assertEqual(code, 1)
                self.assertIn(diagnostic, errors)

        def no_guest(peer):
            authenticated(peer, first=None)
        with FakeBridge(no_guest) as bridge, self.terminal(bridge) as terminal:
            code, errors = terminal.finish()
        self.assertEqual(code, 1)
        self.assertIn(b"EOF before guest output", errors)

        close_peer = threading.Event()
        def disconnect(peer):
            authenticated(peer)
            self.assertTrue(close_peer.wait(4))
            peer.sendall(b"last guest bytes\r\n")
        with FakeBridge(disconnect) as bridge, self.terminal(bridge) as terminal:
            terminal.read_until(PROMPT)
            terminal.wait_raw()
            close_peer.set()
            terminal.read_until(b"last guest bytes\r\n")
            code, errors = terminal.finish()
        self.assertEqual(code, 0, errors)
        self.assertIn(b"EOF", errors)

    def test_raw_ctrl_c_arrow_utf8_and_partial_writes(self):
        payload = b"\x03\x04\x1b[A\x1b[B\x1b[C\x1b[D\x7f\x08\r\n" + "héllo 界".encode()
        def echo(peer):
            authenticated(peer)
            data = read_exact(peer, len(payload))
            self.assertEqual(data, payload)
            for byte in data:
                peer.sendall(bytes([byte]))
            self.assertEqual(read_to_eof(peer), b"")
        with FakeBridge(echo) as bridge, self.terminal(bridge) as terminal:
            terminal.read_until(PROMPT)
            terminal.wait_raw()
            os.write(terminal.master, payload)
            terminal.read_until(PROMPT + payload)
            self.assertIsNone(terminal.process.poll())  # Ctrl-C did not kill Python.
            self.assertEqual(bytes(terminal.output), PROMPT + payload)
            os.write(terminal.master, client.ESCAPE)
            code, errors = terminal.finish()
        self.assertEqual(code, 0, errors)

        # Force short writes and occasional EAGAIN in the actual subprocess;
        # all bytes still travel through real sockets and pipes.
        shim = """
import os, runpy, socket, sys
class ShortSocket(socket.socket):
    calls = 0
    def send(self, data, flags=0):
        self.calls += 1
        if self.calls % 11 == 0:
            raise BlockingIOError()
        return super().send(data[:113], flags)
socket.socket = ShortSocket
write = os.write
calls = 0
def short_write(fd, data):
    global calls
    calls += 1
    if calls % 13 == 0:
        raise BlockingIOError()
    return write(fd, data[:79])
os.write = short_write
sys.argv = sys.argv[1:]
runpy.run_path(sys.argv[0], run_name="__main__")
"""
        large = (payload + b"\x1b[31mred\x1b[0m\n") * 15000
        def pressure(peer):
            peer.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 4096)
            authenticated(peer)
            time.sleep(0.1)
            data = read_to_eof(peer)
            self.assertEqual(data, large)
            for start in range(0, len(data), 997):
                peer.sendall(data[start:start + 997])
        with FakeBridge(pressure) as bridge:
            result = self.pipe(bridge, large, shim=shim)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, PROMPT + large)
        self.assert_private(result)

    def test_late_attach_requests_repaint_not_submit(self):
        answered = threading.Event()
        repaint = threading.Event()
        release = threading.Event()
        def late_attach(peer):
            peer.sendall(CHALLENGE_LINE)
            self.assertEqual(read_exact(peer, len(MAC) + 1), MAC + b"\x0c")
            answered.set()
            self.assertTrue(repaint.wait(4))
            peer.sendall(b"console-tcp: auth ")
            self.assertTrue(release.wait(4))
            peer.sendall(b"diagnostic from guest\r\n" + PROMPT)
            self.assertEqual(read_to_eof(peer), b"")
        with FakeBridge(late_attach) as bridge, self.terminal(bridge) as terminal:
            self.assertTrue(answered.wait(4))
            # Even after sending the answer + repaint request, no raw mode
            # until actual guest bytes distinguish success from refusal.
            self.assertEqual(termios.tcgetattr(terminal.slave), terminal.original)
            repaint.set()
            time.sleep(0.02)
            self.assertEqual(termios.tcgetattr(terminal.slave), terminal.original)
            release.set()
            expected = b"console-tcp: auth diagnostic from guest\r\n" + PROMPT
            terminal.read_until(expected)
            self.assertEqual(bytes(terminal.output), expected)
            os.write(terminal.master, client.ESCAPE)
            code, errors = terminal.finish()
        self.assertEqual(code, 0, errors)

        def serve(peer):
            authenticated(peer, first=None, split=True)
            # No replay/positive-auth response. A deliberately ambiguous first
            # fragment must survive classification and be displayed byte-exact.
            peer.sendall(b"console-tcp: auth ")
            time.sleep(0.02)
            peer.sendall(b"diagnostic from guest\r\n" + PROMPT)
            self.assertEqual(read_to_eof(peer), b"")
        with FakeBridge(serve) as bridge:
            result = self.pipe(bridge)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, b"console-tcp: auth diagnostic from guest\r\n" + PROMPT)

    def test_escape_disconnects_without_guest_exit(self):
        def serve(peer):
            authenticated(peer)
            self.assertEqual(read_exact(peer, 7), b"partial")
            peer.sendall(b"partial")
            self.assertEqual(read_to_eof(peer), b"")
        with FakeBridge(serve) as bridge, self.terminal(bridge) as terminal:
            terminal.read_until(PROMPT)
            terminal.wait_raw()
            os.write(terminal.master, b"partial")
            terminal.read_until(PROMPT + b"partial")
            os.write(terminal.master, b"\x1dexit\r\n")
            code, errors = terminal.finish()
        self.assertEqual(code, 0, errors)
        self.assertIn(b"local disconnect", errors)

    def test_non_tty_and_signal_cleanup(self):
        payload = "pipe UTF-8 →".encode() + b"\x03\x04\x1b[A\r\n"
        def echo(peer):
            authenticated(peer)
            data = read_to_eof(peer)
            self.assertEqual(data, payload)
            peer.sendall(data)
        # If any termios function is touched in pipe mode, fail the subprocess.
        shim = """
import runpy, sys, termios
def forbidden(*args, **kwargs):
    raise AssertionError("termios touched in non-TTY mode")
termios.tcgetattr = forbidden
termios.tcsetattr = forbidden
sys.argv = sys.argv[1:]
runpy.run_path(sys.argv[0], run_name="__main__")
"""
        with FakeBridge(echo) as bridge:
            result = self.pipe(bridge, payload, shim=shim)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, PROMPT + payload)

        for number in client.EXIT_SIGNALS:
            with self.subTest(signal=number):
                def serve(peer):
                    authenticated(peer)
                    self.assertEqual(read_to_eof(peer), b"")
                with FakeBridge(serve) as bridge, self.terminal(bridge) as terminal:
                    terminal.read_until(PROMPT)
                    terminal.wait_raw()
                    terminal.process.send_signal(number)
                    code, errors = terminal.finish()
                self.assertEqual(code, 128 + number, errors)
                self.assertIn(signal.Signals(number).name.encode(), errors)

    def test_exception_restores_terminal(self):
        for exception in ("OSError(5, 'do not log session contents')",
                          "RuntimeError('do not log session contents')"):
            with self.subTest(exception=exception):
                def serve(peer):
                    authenticated(peer)
                    self.assertEqual(read_to_eof(peer), b"")
                shim = f"""
import os, runpy, sys
def fail_write(*args):
    raise {exception}
os.write = fail_write
sys.argv = sys.argv[1:]
runpy.run_path(sys.argv[0], run_name="__main__")
"""
                with FakeBridge(serve) as bridge, self.terminal(bridge, shim=shim) as terminal:
                    code, errors = terminal.finish()
                self.assertEqual(code, 1)
                self.assertNotIn(b"do not log session contents", errors)
                self.assertNotIn(b"Traceback", errors)

        for endpoint in ("socket", "stdout"):
            with self.subTest(zero_write=endpoint):
                def serve(peer):
                    authenticated(peer)
                    self.assertEqual(read_to_eof(peer), b"")
                shim = f"""
import os, runpy, socket, sys
if {endpoint!r} == "socket":
    class ClosedWrite(socket.socket):
        def send(self, *args):
            return 0
    socket.socket = ClosedWrite
else:
    os.write = lambda *args: 0
sys.argv = sys.argv[1:]
runpy.run_path(sys.argv[0], run_name="__main__")
"""
                with FakeBridge(serve) as bridge, self.terminal(bridge, shim=shim) as terminal:
                    if endpoint == "socket":
                        terminal.read_until(PROMPT)
                        os.write(terminal.master, b"trigger write")
                    code, errors = terminal.finish()
                self.assertEqual(code, 1)
                self.assertIn(b"write closed", errors)

    def test_loopback_only_and_socket_errors(self):
        for address in ("192.0.2.1:24681", "0.0.0.0:24681", "example.com:24681",
                        "127.0.0.1:0", "127.0.0.1:65536", "[::]:24681",
                        "127.0.0.1:" + "9" * 5000):
            with self.subTest(address=address):
                result = subprocess.run(self.command(address), input=b"",
                                        capture_output=True, timeout=3)
                self.assertEqual(result.returncode, 2)
                self.assertIn(b"loopback", result.stderr)
        self.assertEqual(client.parse_addr("localhost:24681")[1], ("127.0.0.1", 24681))
        self.assertEqual(client.parse_addr("[::1]:24681")[0], socket.AF_INET6)
        # Reserve a port, then close it, rather than assuming port 1 is free.
        with socket.socket() as unused:
            unused.bind(("127.0.0.1", 0))
            address = "127.0.0.1:" + str(unused.getsockname()[1])
        result = subprocess.run(self.command(address), input=b"",
                                capture_output=True, timeout=3)
        self.assertEqual(result.returncode, 1)
        self.assertIn(b"socket/I/O error", result.stderr)


if __name__ == "__main__":
    unittest.main()
