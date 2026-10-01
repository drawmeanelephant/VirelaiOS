#!/usr/bin/env python3
"""Interactive host client for VMRunner's authenticated loopback serial bridge.

    python3 tools/console-client/client.py --addr 127.0.0.1:24681 \
        --secret-file /absolute/private/console.secret

Ctrl-] disconnects locally. Ctrl-C and Ctrl-D go to the guest. This is a
byte relay, not a terminal emulator, SSH client, or guest PTY resize protocol.
"""

import argparse
from contextlib import contextmanager
import errno
import fcntl
import hashlib
import hmac
import ipaddress
import math
import os
import re
import select
import signal
import socket
import stat
import sys
import termios
import time
import tty


SCHEME = b"VIRELAIOS-AUTH/1 hmac-sha256"
CHALLENGE_MAX = 160
SECRET_MAX = 4096
BUFFER_MAX = 64 * 1024  # Per direction; stop reading when the queue is full.
CHUNK_MAX = 4096
ESCAPE = b"\x1d"
REFUSALS = {
    b"console-tcp: busy\n": "bridge busy (one client at a time)",
    b"console-tcp: auth failed\n": "authentication failed",
    b"console-tcp: auth timeout\n": "bridge authentication timeout",
}
EXIT_SIGNALS = (signal.SIGINT, signal.SIGTERM, signal.SIGHUP,
                signal.SIGQUIT, signal.SIGTSTP)


class ClientError(Exception):
    """A local diagnostic, never peer bytes, a secret, or session content."""


class LocalSignal(BaseException):
    def __init__(self, number):
        self.number = number


def parse_addr(value):
    host, separator, port = value.rpartition(":")
    if (not separator or not port.isascii() or not port.isdecimal()
            or len(port) > 5):
        raise argparse.ArgumentTypeError("--addr wants a loopback host:port")
    if host == "localhost":
        host = "127.0.0.1"
    if host.startswith("[") and host.endswith("]"):
        host = host[1:-1]
    try:
        address = ipaddress.ip_address(host)
    except ValueError:
        raise argparse.ArgumentTypeError("--addr requires a literal loopback address")
    if not address.is_loopback or not 1 <= int(port) <= 65535:
        raise argparse.ArgumentTypeError("--addr requires loopback and port 1..65535")
    family = socket.AF_INET if address.version == 4 else socket.AF_INET6
    return family, (str(address), int(port))


def positive_timeout(value):
    try:
        seconds = float(value)
    except ValueError:
        raise argparse.ArgumentTypeError("--timeout must be a positive number")
    if not math.isfinite(seconds) or seconds <= 0:
        raise argparse.ArgumentTypeError("--timeout must be a positive finite number")
    return seconds


def read_secret(path):
    # Check the opened inode, not a path checked before open. Nonblocking open
    # also prevents a FIFO substituted for the private file from hanging us.
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    except FileNotFoundError:
        raise ClientError("secret file missing") from None
    except OSError as error:
        if error.errno == errno.ELOOP:
            raise ClientError("secret file insecure (symlink)") from None
        raise ClientError("secret file unreadable") from None
    with os.fdopen(fd, "rb") as stream:
        info = os.fstat(stream.fileno())
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid()
                or info.st_mode & 0o077):
            raise ClientError("secret file insecure (require owner-only regular file)")
        first = stream.readline(SECRET_MAX + 2)
    if len(first) > SECRET_MAX:
        raise ClientError("secret file first line too long")
    # Match VMRunner: first line only, with one LF and then one CR removed.
    secret = first.removesuffix(b"\n").removesuffix(b"\r")
    if not secret:
        raise ClientError("secret file empty")
    try:
        secret.decode("utf-8")
    except UnicodeDecodeError:
        raise ClientError("secret file must contain UTF-8") from None
    return secret


def remaining(sock, deadline, stage):
    seconds = deadline - time.monotonic()
    if seconds <= 0:
        raise ClientError(f"{stage} timeout")
    sock.settimeout(seconds)


def authenticate(sock, secret, deadline):
    line = bytearray()
    while True:
        remaining(sock, deadline, "challenge")
        chunk = sock.recv(1)
        if not chunk:
            raise ClientError("bridge EOF before challenge")
        if chunk == b"\n":
            break
        line.extend(chunk)
        if len(line) > CHALLENGE_MAX:
            raise ClientError("challenge too long")
    wire_line = bytes(line) + b"\n"
    if wire_line in REFUSALS:
        raise ClientError(REFUSALS[wire_line])
    match = re.fullmatch(re.escape(SCHEME) + rb" ([0-9a-fA-F]{64})", line)
    if match is None:
        raise ClientError("invalid challenge (require exact scheme and 32-byte hex)")
    challenge = bytes.fromhex(match[1].decode("ascii"))
    mac = hmac.new(secret, SCHEME + b"\x00" + challenge, hashlib.sha256)
    remaining(sock, deadline, "authentication")
    # The bridge reads through the MAC's LF before publishing/forwarding bytes.
    # Ctrl-L is the ONLY automatic guest input, even for a late attach.
    sock.sendall(mac.hexdigest().encode("ascii") + b"\n\x0c")

    first = bytearray()
    while True:
        remaining(sock, deadline, "first guest output")
        chunk = sock.recv(CHUNK_MAX)
        if not chunk:
            if first:
                return bytes(first)
            raise ClientError("bridge EOF before guest output")
        first.extend(chunk)
        for refusal, message in REFUSALS.items():
            if first.startswith(refusal):
                raise ClientError(message)
        # Buffer only an ambiguous refusal prefix across split reads. No
        # positive-auth reply exists; all other first bytes are guest output.
        if not any(refusal.startswith(first) for refusal in REFUSALS):
            return bytes(first)


@contextmanager
def local_terminal(stdin_fd, stdout_fd):
    """Restore exact termios, descriptor flags, and signal handlers on all exits."""
    terminals = []
    flags = []
    handlers = {}

    def interrupted(number, _frame):
        raise LocalSignal(number)

    try:
        for number in EXIT_SIGNALS:
            handlers[number] = signal.signal(number, interrupted)
        # Save BOTH endpoints before changing either; they often share a tty.
        for fd in dict.fromkeys((stdin_fd, stdout_fd)):
            if os.isatty(fd):
                terminals.append((fd, termios.tcgetattr(fd)))
            flags.append((fd, fcntl.fcntl(fd, fcntl.F_GETFL)))
        for fd, _attributes in terminals:
            tty.setraw(fd, termios.TCSANOW)
        for fd, original in flags:
            fcntl.fcntl(fd, fcntl.F_SETFL, original | os.O_NONBLOCK)
        yield
    finally:
        # A second signal during cleanup must not strand the caller's tty.
        mask = signal.pthread_sigmask(signal.SIG_BLOCK, EXIT_SIGNALS)
        try:
            for fd, original in reversed(flags):
                fcntl.fcntl(fd, fcntl.F_SETFL, original)
            for fd, attributes in reversed(terminals):
                # Flush unread local keystrokes on exit. On macOS, restoring
                # ICANON without this also leaves a new PENDIN flag behind.
                termios.tcsetattr(fd, termios.TCSAFLUSH, attributes)
        finally:
            for number, handler in handlers.items():
                signal.signal(number, handler)
            signal.pthread_sigmask(signal.SIG_SETMASK, mask)


def pump(sock, stdin_fd, stdout_fd, first):
    to_guest = bytearray()
    to_stdout = bytearray(first)
    stdin_open = True
    peer_open = True
    write_open = True
    sock.setblocking(False)
    with local_terminal(stdin_fd, stdout_fd):
        while True:
            if not peer_open and not to_stdout:
                return "bridge EOF (disconnected)"
            if not stdin_open and not to_guest and write_open and peer_open:
                sock.shutdown(socket.SHUT_WR)
                write_open = False
            readers = []
            writers = []
            if stdin_open and peer_open and len(to_guest) < BUFFER_MAX:
                readers.append(stdin_fd)
            if peer_open and len(to_stdout) < BUFFER_MAX:
                readers.append(sock)
            if to_guest and peer_open:
                writers.append(sock)
            if to_stdout:
                writers.append(stdout_fd)
            readable, writable, _ = select.select(readers, writers, [])
            if stdin_fd in readable:
                try:
                    chunk = os.read(stdin_fd, min(CHUNK_MAX, BUFFER_MAX - len(to_guest)))
                except (BlockingIOError, InterruptedError):
                    chunk = None
                if chunk == b"":
                    stdin_open = False
                elif chunk:
                    # Local escape drops pending writes instead of draining
                    # them. No synthetic exit command or automatic reconnect.
                    if ESCAPE in chunk:
                        return "local disconnect (Ctrl-])"
                    to_guest.extend(chunk)
            if sock in readable:
                try:
                    chunk = sock.recv(min(CHUNK_MAX, BUFFER_MAX - len(to_stdout)))
                except (BlockingIOError, InterruptedError):
                    chunk = None
                if chunk == b"":
                    peer_open = False
                    to_guest.clear()
                elif chunk:
                    to_stdout.extend(chunk)
            if sock in writable and peer_open:
                try:
                    written = sock.send(to_guest)
                except (BlockingIOError, InterruptedError):
                    written = None
                if written == 0:
                    raise ClientError("socket write closed")
                if written is not None:
                    del to_guest[:written]
            if stdout_fd in writable:
                try:
                    written = os.write(stdout_fd, to_stdout)
                except (BlockingIOError, InterruptedError):
                    written = None
                if written == 0:
                    raise ClientError("stdout write closed")
                if written is not None:
                    del to_stdout[:written]


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--addr", required=True, type=parse_addr)
    parser.add_argument("--secret-file", required=True)
    parser.add_argument("--timeout", type=positive_timeout, default=10.0,
                        help="connect/auth/first-output deadline in seconds (default: 10)")
    args = parser.parse_args(argv)
    try:
        secret = read_secret(args.secret_file)
        family, address = args.addr
        deadline = time.monotonic() + args.timeout
        with socket.socket(family, socket.SOCK_STREAM) as sock:
            remaining(sock, deadline, "connect")
            sock.connect(address)
            first = authenticate(sock, secret, deadline)
            del secret
            message = pump(sock, sys.stdin.fileno(), sys.stdout.fileno(), first)
        print(f"console-client: {message}", file=sys.stderr)
        return 0
    except LocalSignal as stopped:
        print(f"console-client: local signal {signal.Signals(stopped.number).name}",
              file=sys.stderr)
        return 128 + stopped.number
    except KeyboardInterrupt:
        # Before raw mode, Python's normal SIGINT behavior still applies.
        print("console-client: local signal SIGINT", file=sys.stderr)
        return 128 + signal.SIGINT
    except TimeoutError:
        print("console-client: connect/auth/first-output timeout", file=sys.stderr)
    except ClientError as error:
        print(f"console-client: {error}", file=sys.stderr)
    except OSError as error:
        print(f"console-client: socket/I/O error ({errno.errorcode.get(error.errno, 'unknown')})",
              file=sys.stderr)
    except Exception:
        # A traceback could expose auth/session locals through debug wrappers.
        print("console-client: internal error", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
