#!/usr/bin/env bash
# Persistent serial-only GOSH, reached locally or through host OpenSSH.
# No system configuration changes. The share is never the secret store.
set -euo pipefail
set +x
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck disable=SC1091
source "$ROOT/tools/env-check.sh"
exec 3<&0
exec python3 - "$ROOT" "$@" <<'PY'
import argparse
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import socket
import stat
import subprocess
import sys
import time

# The heredoc carries code, not operator input. Restore the caller's input
# before exec'ing B's client (including an SSH-allocated terminal).
os.dup2(3, 0)
os.close(3)


class Refused(Exception):
    pass


def private(path, directory=False):
    info = path.lstat()
    kind = stat.S_ISDIR if directory else stat.S_ISREG
    mode = 0o700 if directory else 0o600
    if not kind(info.st_mode) or info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != mode:
        raise Refused("insecure session state (require owner-only directories/files)")


def private_dir(path):
    try:
        path.mkdir(mode=0o700)
    except FileExistsError:
        pass
    private(path, True)


def port_number(value):
    if not value.isascii() or not value.isdecimal() or len(value) > 5 or not 1 <= int(value) <= 65535:
        raise argparse.ArgumentTypeError("port must be an integer in 1..65535")
    return int(value)


def share_mode(share, new=False):
    if share.exists():
        if not share.is_dir() or share.is_symlink():
            raise Refused("share must be a real directory")
        settings = share / "SETTINGS.TXT"
        if settings.is_symlink():
            raise Refused("incompatible share: SETTINGS.TXT must not be a symlink")
        if not settings.exists():
            if not new or any(share.iterdir()):
                raise Refused("incompatible share: no remote settings; use a dedicated empty share")
        else:
            if not settings.is_file() or settings.is_symlink():
                raise Refused("incompatible share: SETTINGS.TXT must be a regular file")
            lines = settings.read_text().splitlines()
            modes = {}
            for line in lines[1:]:
                key, sep, value = line.partition("=")
                if sep and key in ("wm", "shell"):
                    if key in modes:
                        raise Refused("incompatible share: duplicate mode settings")
                    modes[key] = value
            if not lines or lines[0] != "#v2" or modes != {"wm": "none", "shell": "sh"}:
                raise Refused("incompatible share: require #v2, wm=none, shell=sh; settings are not rewritten")
        rc = share / ".virelairc"
        if rc.exists() or rc.is_symlink():
            if rc.is_symlink() or not rc.is_file() or any(
                    line.strip() and not line.lstrip().startswith("#") for line in rc.read_text().splitlines()):
                raise Refused("incompatible share: active .virelairc; use a dedicated remote share (not rewritten)")
    elif not new:
        raise Refused("share missing; start serve with a dedicated absolute share")


def seed(root, share):
    share_mode(share, new=True)
    share.mkdir(parents=True, exist_ok=True)
    settings = share / "SETTINGS.TXT"
    if not settings.exists():
        with settings.open("x") as stream:
            stream.write("#v2\nwm=none\nshell=sh\n")
    # Only distribution-owned executable/support names are refreshed.
    sources = [(root / ".build/go/GOSH.ELF", share / "GOSH.ELF")]
    for name in ("LD.SO", "LIBUI.SO", "LIBFONT.SO", "SSH.BIN"):
        src = root / "zig-out/bin" / name
        if src.is_file():
            sources.append((src, share / name))
    for src, dst in sources:
        if dst.is_symlink() or (dst.exists() and not dst.is_file()):
            raise Refused("incompatible share: executable/support destination is not a regular file")
        shutil.copyfile(src, dst)
    hello = share / "HELLO.ELF"
    if hello.is_symlink() or (hello.exists() and not hello.is_file()):
        raise Refused("incompatible share: HELLO.ELF is not a regular file")
    subprocess.run(["python3", str(root / "tools/mkhello-elf.py"), str(hello)], check=True)


def live(pid):
    if not isinstance(pid, int) or pid <= 1:
        return False
    try:
        os.kill(pid, 0)
        return True
    except ProcessLookupError:
        return False


def gosh_owned(serial):
    data = serial.read_bytes() if serial.exists() else b""
    attached = data.rfind(b"gosh: attached")
    return attached >= 0 and max(data.rfind(b"gosh: monitor"), data.rfind(b"gosh: close")) < attached


def main():
    root = Path(sys.argv[1])
    parser = argparse.ArgumentParser(description="SSH-to-host remote-only GOSH service")
    parser.add_argument("action", choices=("serve", "attach"))
    parser.add_argument("--share", required=True)
    parser.add_argument("--port", type=port_number, default=None, help="serve only; default 24681")
    args = parser.parse_args(sys.argv[2:])
    if not Path(args.share).is_absolute():
        raise Refused("--share must be absolute")
    # Refuse a symlink at the supplied share, before canonicalizing aliases.
    if Path(args.share).is_symlink():
        raise Refused("share must not be a symlink")
    share = Path(args.share).resolve()
    share_mode(share, new=args.action == "serve")
    base = Path.home() / ".virelai"
    private_dir(base)
    base = base / "remote-terminal"
    private_dir(base)
    key = hashlib.sha256(os.fsencode(share)).hexdigest()
    state = base / key
    lock_path = base / (key + ".lock")
    fd = os.open(lock_path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    private(lock_path)
    with os.fdopen(fd, "r+") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            occupied = False
        except BlockingIOError:
            occupied = True
        if args.action == "attach":
            if args.port is not None:
                raise Refused("--port belongs to serve; attach uses the running session's port")
            if not occupied or not state.exists():
                raise Refused("bridge missing or stale session; start serve on this host")
            private(state, True)
            if not (state / "session.json").exists():
                raise Refused("bridge service still starting; wait for GOSH ready")
            private(state / "session.json")
            private(state / "console.secret")
            meta = json.loads((state / "session.json").read_text())
            if meta.get("share") != str(share) or not live(meta.get("service")) or not live(meta.get("runner")):
                raise Refused("bridge service unavailable (stale session)")
            port = port_number(str(meta["port"]))
            if not gosh_owned(state / "serial.log"):
                raise Refused("GOSH not attached (booting or monitor handback); a bare monitor is not remote-shell acceptance")
            # Do not probe-connect: a probe itself would occupy the console.
            # B reports busy/auth/connect failures and restores the local tty.
            os.execvp("python3", ["python3", str(root / "tools/console-client/client.py"),
                                 "--addr", f"127.0.0.1:{port}", "--secret-file", str(state / "console.secret")])
        if occupied:
            raise Refused("session occupied: serve is already running for this share")
        if state.exists():
            raise Refused("stale session state exists; inspect it before removing it (never replaced automatically)")
        port = args.port or 24681
        with socket.socket() as probe:
            try:
                probe.bind(("127.0.0.1", port))
            except OSError as error:
                if error.errno == errno.EADDRINUSE:
                    raise Refused("port occupied: choose another --port or stop its owner") from None
                raise
        if subprocess.check_output(["uname", "-m"], text=True).strip() != "arm64":
            raise Refused("serve requires an Apple-silicon macOS 27+ host")
        if int(subprocess.check_output(["sw_vers", "-productVersion"], text=True).split(".")[0]) < 27:
            raise Refused("serve requires macOS 27+")
        state.mkdir(mode=0o700)
        runner = None
        stopped = False

        def stop(_number, _frame):
            nonlocal stopped
            stopped = True
            if runner is not None and runner.poll() is None:
                runner.terminate()

        old_handlers = {sig: signal.signal(sig, stop) for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)}
        try:
            # The secret is generated in-process and written directly, never
            # passed to a child via argv, environment, or a printed command.
            with (state / "console.secret").open("x") as stream:
                stream.write(secrets.token_hex(32) + "\n")
            for command in (["zig", "build"], ["zig", "build", "image"], ["zig", "build", "inspect"],
                            ["bash", str(root / "tools/go/ensure-guest-elf.sh"), "ensure", "GOSH"],
                            ["bash", str(root / "tools/go/ensure-guest-elf.sh"), "check", "GOSH"],
                            ["swift", "build", "--package-path", str(root / "host/vm-runner"),
                             "--configuration", "release", "-Xswiftc", "-DSPIKE"],
                            ["codesign", "--force", "--sign", "-", "--entitlements",
                             str(root / "host/vm-runner/entitlements.plist"),
                             str(root / "host/vm-runner/.build/release/VMRunner")]):
                if stopped:
                    return 130
                subprocess.run(command, cwd=root, check=True)
            seed(root, share)
            if stopped:
                return 130
            # Foundation's throwaway overlay is confined to our own tmp dir.
            (state / "tmp").mkdir(mode=0o700)
            env = dict(os.environ, TMPDIR=str(state / "tmp") + "/")
            env.pop("VIRELAI_KEEP_OVERLAY", None)
            command = [str(root / "host/vm-runner/.build/release/VMRunner"),
                       "--overlay-base", str(root / "artifacts/disk.img"),
                       "--vars", str(state / "efi-vars.bin"), "--serial", str(state / "serial.log"),
                       "--cvc-file", str(share), "--console-tcp", f"127.0.0.1:{port}",
                       "--console-tcp-secret-file", str(state / "console.secret"), "--timeout", "0"]
            with (state / "runner.log").open("x") as output:
                runner = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=output,
                                          stderr=subprocess.STDOUT, cwd=root, env=env, start_new_session=True)
                with (state / "session.json").open("x") as stream:
                    json.dump(dict(share=str(share), port=port, service=os.getpid(), runner=runner.pid), stream)
                deadline = time.monotonic() + 120
                while not gosh_owned(state / "serial.log"):
                    if runner.poll() is not None:
                        raise Refused("VMRunner exited before GOSH attached; inspect the host build/platform prerequisites")
                    if stopped:
                        return 130
                    if time.monotonic() > deadline:
                        raise Refused("GOSH boot deadline: no attached shell (no monitor fallback)")
                    time.sleep(0.1)
                print(f"remote-terminal: GOSH ready on 127.0.0.1:{port}; files persist in {share}", flush=True)
                print("remote-terminal: Ctrl-C here stops the VM; Ctrl-] in attach only disconnects.", flush=True)
                # A stuck child must not trap the service inside wait() after
                # SIGTERM. Teardown below has a bounded terminate/kill wait.
                while runner.poll() is None and not stopped:
                    time.sleep(0.1)
                return 130 if stopped else runner.returncode
        finally:
            if runner is not None and runner.poll() is None:
                runner.terminate()
                try:
                    runner.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    runner.kill()
                    runner.wait()
            # This exact directory was created above by this invocation.
            shutil.rmtree(state)
            for sig, handler in old_handlers.items():
                signal.signal(sig, handler)


os.umask(0o077)
try:
    sys.exit(main())
except Refused as error:
    print(f"remote-terminal: {error}", file=sys.stderr)
    sys.exit(1)
except (OSError, ValueError, KeyError, TypeError, argparse.ArgumentTypeError, subprocess.SubprocessError):
    print("remote-terminal: operation failed; check build, private state, and host prerequisites", file=sys.stderr)
    sys.exit(1)
PY
