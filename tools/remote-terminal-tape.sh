#!/usr/bin/env bash
# Class C only. Run on the OTHER machine; record only this consented fixture.
set -euo pipefail
set +x
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec python3 - "$ROOT" "$@" <<'PY'
import argparse
import datetime
import json
import os
from pathlib import Path
import platform
import select
import shlex
import subprocess
import sys
import time

parser = argparse.ArgumentParser(description="Real two-machine SSH fixture acceptance (never a local fallback)")
parser.add_argument("--ssh-target", help="authorized operator@vm-host with a verified known_hosts entry")
parser.add_argument("--host-repo", help="absolute repository on the VM host")
parser.add_argument("--share", help="absolute dedicated share on the VM host")
parser.add_argument("--fixture-only", action="store_true", help="consent to record the scripted fixture, not an operator session")
args = parser.parse_args(sys.argv[2:])


def blocked(message):
    print("remote-terminal-tape: BLOCKED class C: " + message, file=sys.stderr)
    sys.exit(2)


if not args.ssh_target:
    blocked("no second-machine SSH target supplied; run this driver on a different machine with --ssh-target")
if not args.host_repo or not args.share or not args.fixture_only:
    blocked("require --host-repo, --share, and explicit --fixture-only recording consent")
if not Path(args.host_repo).is_absolute() or not Path(args.share).is_absolute() or args.ssh_target.startswith("-"):
    blocked("require absolute host paths and an explicit SSH target")
ssh = ["ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=10"]
# A stable hardware/installation identity, never a random UUID or hostname.
# Send only its digest to evidence; a same-host SSH alias is still refused.
identity_code = r'''
import hashlib, pathlib, platform, re, subprocess
def machine_identity():
    if platform.system() == "Darwin":
        data = subprocess.check_output(["/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice"], timeout=5)
        match = re.search(rb'"IOPlatformUUID"\s*=\s*"([^"]+)"', data)
        if not match:
            raise RuntimeError("stable hardware identity unavailable")
        raw = match[1]
    else:
        raw = pathlib.Path("/etc/machine-id").read_bytes().strip()
        if not raw:
            raise RuntimeError("stable connecting-machine identity unavailable")
    return hashlib.sha256(raw).hexdigest()
'''
exec(identity_code)
try:
    identity = machine_identity()
except (OSError, RuntimeError, subprocess.SubprocessError):
    blocked("stable connecting-machine identity unavailable; no random/local substitute")
probe = identity_code + r'''
import fcntl, json, os
import ipaddress
connection = os.environ.get("SSH_CONNECTION", "").split()
if len(connection) != 4:
    raise SystemExit("host probe must run through OpenSSH")
peer = ipaddress.ip_address(connection[0])
host = ipaddress.ip_address(connection[2])
relationship = dict(transport="OpenSSH", peer_family=peer.version, host_family=host.version,
                    peer_loopback=peer.is_loopback, host_loopback=host.is_loopback,
                    same_address=peer == host)
share = pathlib.Path(__import__("sys").argv[1]).resolve()
base = pathlib.Path.home() / ".virelai/remote-terminal"
key = hashlib.sha256(os.fsencode(share)).hexdigest()
with (base / (key + ".lock")).open() as lock:
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        pass
    else:
        raise SystemExit("remote service is not running")
meta = json.loads((base / key / "session.json").read_text())
os.kill(meta["service"], 0)
os.kill(meta["runner"], 0)
serial = (base / key / "serial.log").read_bytes()
assert serial.rfind(b"gosh: attached") > max(serial.rfind(b"gosh: monitor"), serial.rfind(b"gosh: close"))
receipt = share / "REMOTE.RECEIPT"
verify_receipt = __import__("sys").argv[2] == "receipt"
print(json.dumps(dict(machine=machine_identity(),
                     arch=platform.machine(), macos=platform.mac_ver()[0],
                     address_relationship=relationship,
                     service=meta["service"], runner=meta["runner"],
                     receipt=receipt.read_bytes().hex() if verify_receipt and receipt.exists()
                     and receipt.stat().st_size <= 4096 else None)))
'''


def preflight(receipt=False):
    result = subprocess.run(ssh + [args.ssh_target, shlex.join(
        ["python3", "-c", probe, args.share, "receipt" if receipt else "identity"])],
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=20)
    if result.returncode != 0:
        blocked("verified-key SSH or the host's running GOSH service is unavailable; no localhost substitute")
    try:
        meta = json.loads(result.stdout)
        if not isinstance(meta, dict) or not isinstance(meta.get("machine"), str):
            raise ValueError()
    except ValueError:
        blocked("host preflight did not return a service identity")
    if meta["machine"] == identity:
        blocked("connecting machine and VM host are the same machine")
    try:
        major = int(meta.get("macos", "0").split(".")[0])
    except (ValueError, AttributeError):
        blocked("invalid host capability report")
    if meta.get("arch") != "arm64" or major < 27:
        blocked("VM host is not Apple silicon/macOS 27+")
    return meta


try:
    before = preflight()
except (OSError, subprocess.SubprocessError):
    blocked("SSH executable/verified-key host connection unavailable")

os.umask(0o077)
stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S.%fZ")
out = Path(sys.argv[1]) / "artifacts/remote-terminal" / stamp
out.mkdir(parents=True)
(out / "machines.json").write_text(json.dumps(dict(connecting=identity, host=before,
                                                  connecting_arch=platform.machine()), indent=2) + "\n")
command = shlex.join(["bash", str(Path(args.host_repo) / "tools/remote-terminal.sh"),
                      "attach", "--share", args.share])
transcript = (out / "fixture.transcript").open("wb")
clients = []


class Session:
    def __init__(self):
        self.p = subprocess.Popen(ssh + ["-tt", args.ssh_target, command],
                                  stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        clients.append(self.p)
        self.data = bytearray()
        self.until(b"gosh> ")

    def drain(self, seconds=0.1):
        ready, _, _ = select.select([self.p.stdout], [], [], seconds)
        if ready:
            data = os.read(self.p.stdout.fileno(), 65536)
            if not data:
                raise RuntimeError("SSH/client EOF before fixture completed")
            self.data.extend(data)
            transcript.write(data)
            transcript.flush()

    def until(self, needle, start=0):
        deadline = time.monotonic() + 20
        while needle not in self.data[start:]:
            if time.monotonic() > deadline:
                raise RuntimeError("fixture output deadline")
            self.drain()
        return self.data.index(needle, start)

    def send(self, data):
        for byte in data:
            self.p.stdin.write(bytes([byte]))
            self.p.stdin.flush()
            self.drain(0.003)

    def line(self, command, expected):
        start = len(self.data)
        self.send(command + b"\r")
        marker = self.until(b"gosh: line ", start)
        body = self.until(b"\n", marker)
        result = self.until(expected, body)
        if expected != b"gosh> ":
            self.until(b"gosh> ", result + len(expected))

    def disconnect(self):
        self.p.stdin.write(b"\x1d")
        self.p.stdin.flush()
        rc = self.p.wait(timeout=15)
        rest = self.p.stdout.read()
        transcript.write(rest)
        transcript.flush()
        if rc != 0:
            raise RuntimeError("SSH/client disconnect failed")


try:
    a = Session()
    a.line(b"whoami", b"\nuid=")
    a.line(b"id", b" caps=")
    a.line(b"cd /host", b"gosh> ")
    a.line(b"echo remote-owner-ok > REMOTE.RECEIPT", b"gosh> ")
    a.line(b"cat REMOTE.RECEIPT", b"\nremote-owner-ok")
    a.line(b"echo edit-oX\x7fk", b"\nedit-ok")
    a.line(b"\x1b[A", b"\nedit-ok")
    a.send(b"echo MUST-NOT-EXECUTE > CANCEL.RECEIPT")
    start = len(a.data)
    a.send(b"\x03")
    a.until(b"^C", start)
    a.line("printf '\\e[32mUTF-8: café λ\\e[0m\\n'".encode(), "\x1b[32mUTF-8: café λ\x1b[0m".encode())
    a.line(b"echo remote-pipe | cat", b"\nremote-pipe")
    a.line(b"HELLO.ELF", b"elf: hello from HELLO.ELF")
    start = len(a.data)
    partial = b"echo MUST-NOT-EXECUTE > PARTIAL.RECEIPT"
    a.send(partial)
    a.until(b"gosh> " + partial, start)
    a.disconnect()
    middle = preflight()
    assert (middle["service"], middle["runner"]) == (before["service"], before["runner"]), "VM restarted"
    b = Session()
    b.line(b"cat /host/REMOTE.RECEIPT", b"\nremote-owner-ok")
    b.line(b"echo same-live-vm", b"\nsame-live-vm")
    b.disconnect()
    after = preflight(receipt=True)
    assert (after["service"], after["runner"]) == (before["service"], before["runner"]), "VM restarted"
    assert after["receipt"] == b"remote-owner-ok\n".hex(), "host-share receipt bytes differ"
    # Read only the fixture negatives, not any operator documents/history.
    check = "from pathlib import Path; import sys; p=Path(sys.argv[1]); assert not (p/'CANCEL.RECEIPT').exists(); assert not (p/'PARTIAL.RECEIPT').exists()"
    result = subprocess.run(ssh + [args.ssh_target, shlex.join(["python3", "-c", check, args.share])], timeout=20)
    assert result.returncode == 0, "cancelled/partial command executed"
    (out / "result.txt").write_text("PASS class C: distinct machines, verified-key SSH, fixture bytes, same live VM\n")
    print("remote-terminal-tape: PASS class C; fixture evidence: " + str(out))
except (AssertionError, RuntimeError, OSError, subprocess.SubprocessError) as error:
    (out / "result.txt").write_text("FAIL class C: " + str(error) + "\n")
    print("remote-terminal-tape: FAIL class C; inspect fixture evidence: " + str(out), file=sys.stderr)
    sys.exit(1)
finally:
    for p in clients:
        if p.poll() is None:
            p.terminate()
            try:
                p.wait(timeout=5)
            except subprocess.TimeoutExpired:
                p.kill()
                p.wait()
    transcript.close()
PY
