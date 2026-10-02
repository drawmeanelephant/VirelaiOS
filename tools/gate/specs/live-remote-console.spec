# HMAC reject/accept/deadline/file-secret plus M87d's real-client GOSH journey.
# Runs 01–03 keep the monitor auth fixtures. Run 04 is explicitly serial-only:
# no seat, source-fresh GOSH, late attach, same-boot reconnect and byte receipts.
# A finite during-run coordinator checks EVERY subprocess exit, including both
# historical run-03 clients; no reliance on the harness's last-client-only wait.
# exec-order: timeout-ordered -- one external fixture via GOSH; byte/output asserts

vgate_name live-remote-console "M46 RC4: vgate client hook + --console-tcp secret bridge — reject/accept (#1112, ADR 0022)"
vgate_share arm
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file wrong.txt <<'EOF'
wrong
EOF

vgate_file accept.txt <<'EOF'
echo remote-console-ok
EOF

vgate_run 01 -- --cvc-file '$RUN_DIR/monitor-share' --console-tcp '127.0.0.1:24681:s3cret' --timeout 25
vgate_client 01 -- --addr '127.0.0.1:24681' --after 'virelai>' \
    --hmac-secret wrong --send-file '$RUN_DIR/wrong.txt' --expect 'auth failed' --timeout 15

vgate_run 02 -- --cvc-file '$RUN_DIR/monitor-share' --console-tcp '127.0.0.1:24681:s3cret' --timeout 30
vgate_client 02 -- --addr '127.0.0.1:24681' --after 'virelai>' \
    --hmac-secret s3cret --send-file '$RUN_DIR/accept.txt' --expect 'remote-console-ok' --timeout 20

# Run 03 (M70g G2, #1459 — ADR 0022 D2 amendment): the secret comes from a
# FILE (`--console-tcp-secret-file`, so it is never on argv) and the two
# pre-auth negatives are pinned in one boot:
#   silent  connects at once (before the guest prompt — the bridge listens
#           from process start) and never answers the challenge: the
#           connected-but-mute shape of a half-open peer. It must be dropped
#           with `console-tcp: auth timeout` after the 10 s deadline;
#   real    waits for the prompt, is told `busy` while silent holds the seat,
#           retries, then authenticates with the file's secret and drives
#           the monitor.
# The coordinator below retains both fixtures and enforces both exit codes.
vgate_file secret.txt <<'EOF'
f1l3-s3cret
EOF

vgate_run 03 -- --cvc-file '$RUN_DIR/monitor-share' --console-tcp '127.0.0.1:24681' --console-tcp-secret-file '$RUN_DIR/secret.txt' --timeout 45

vgate_file receipt.expected <<'EOF'
remote-owner-ok
EOF

vgate_file history.expected <<'EOF'
whoami
id
echo edit-ok
cd /host
echo remote-owner-ok > REMOTE.RECEIPT
cat REMOTE.RECEIPT
printf '\e[32mUTF-8: café λ\e[0m\n'
echo pipe-ok | cat
HELLO.ELF
echo same-boot-ok
EOF

vgate_file remote-driver.py <<'PY'
import json, os, pathlib, re, select, subprocess, sys, time
rd = pathlib.Path(os.environ["RUN_DIR"])
repo = pathlib.Path.cwd()
children = []
codes = {}
deadline = time.monotonic() + 300

def wait_file(path, needle=None):
    while time.monotonic() < deadline:
        if path.exists() and (needle is None or needle in path.read_bytes()):
            return
        time.sleep(.1)
    raise RuntimeError("gate boot marker deadline")

def checked(p, name, expected=0):
    rc = p.wait(timeout=30)
    codes[name] = rc
    assert rc == expected, "%s exit %d, expected %d" % (name, rc, expected)

class Session:
    def __init__(self, secret="gosh.secret"):
        self.p = subprocess.Popen(
            [sys.executable, str(repo / "tools/console-client/client.py"),
             "--addr", "127.0.0.1:24681", "--secret-file", str(rd / secret)],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        children.append(self.p)
        self.data = bytearray()

    def drain(self, seconds=.1):
        ready, _, _ = select.select([self.p.stdout], [], [], seconds)
        if ready:
            chunk = os.read(self.p.stdout.fileno(), 65536)
            if not chunk:
                raise RuntimeError("client EOF before expected guest output")
            self.data.extend(chunk)
            with (rd / "client-04.out").open("ab") as f:
                f.write(chunk)

    def until(self, needle, start=0):
        end = time.monotonic() + 20
        while needle not in self.data[start:]:
            if time.monotonic() > end:
                raise RuntimeError("guest output deadline: " + repr(needle))
            self.drain()
        return self.data.index(needle, start)

    def send(self, data):
        for byte in data:
            self.p.stdin.write(bytes([byte]))
            self.p.stdin.flush()
            self.drain(.004)

    def line(self, data, output):
        start = len(self.data)
        self.send(data + b"\r")
        # Repainted prompts while TYPING are not command completion. Anchor
        # on the submitted guest line, then its result and subsequent prompt.
        marker = self.until(b"gosh: line ", start)
        body = self.until(b"\n", marker)
        result = self.until(output, body)
        if output != b"gosh> ":
            self.until(b"gosh> ", result + len(output))
        self.drain(.15)

    def disconnect(self, name):
        self.p.stdin.write(b"\x1d")
        self.p.stdin.flush()
        checked(self.p, name)
        with (rd / "client-04.out").open("ab") as f:
            f.write(self.p.stdout.read())

try:
    # Preserve the existing silent deadline and retry-busy auth fixtures,
    # but explicitly wait on BOTH clients rather than losing the first rc.
    wait_file(rd / "vm-serial-03.log")
    env = dict(os.environ, VG_TAG="03", VG_SER=str(rd / "vm-serial-03.log"))
    common = [sys.executable, str(repo / "tools/lib/vgate-client.py"),
              "--addr", "127.0.0.1:24681", "--connect-timeout", "30"]
    with (rd / "client-03.log").open("wb") as log:
        silent = subprocess.Popen(common + ["--expect", "auth timeout", "--timeout", "20",
                                            "--out", str(rd / "client-03-silent.out")], env=env, stdout=log, stderr=log)
        children.append(silent)
        # Ensure silent has acquired the seat before starting its contender.
        wait_file(rd / "run-03.out", b"listening on")
        time.sleep(1)  # silent retries every .25 s; its challenge/deadline is asserted
        real = subprocess.Popen(common + ["--after", "virelai>", "--retry-busy",
                                          "--hmac-secret", "f1l3-s3cret", "--send-file", str(rd / "accept.txt"),
                                          "--expect", "remote-console-ok", "--timeout", "20",
                                          "--out", str(rd / "client-03.out")], env=env, stdout=log, stderr=log)
        children.append(real)
        checked(silent, "silent")
        checked(real, "monitor-auth")
    (rd / "codes-03.json").write_text(json.dumps(codes))

    wait_file(rd / "vm-serial-04.log", b"gosh: prompt")
    time.sleep(1)  # late attach, not boot-time script injection
    wrong = subprocess.Popen([sys.executable, str(repo / "tools/console-client/client.py"),
                               "--addr", "127.0.0.1:24681", "--secret-file", str(rd / "wrong.secret")],
                              stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    children.append(wrong)
    output, _ = wrong.communicate(b"echo forbidden > /host/WRONG.RECEIPT\r", timeout=15)
    codes["wrong-auth"] = wrong.returncode
    assert wrong.returncode == 1 and b"authentication failed" in output, output
    (rd / "wrong-04.out").write_bytes(output)
    time.sleep(.2)
    a = Session()
    a.until(b"gosh> ")
    busy = Session()
    output, _ = busy.p.communicate(timeout=15)
    codes["busy"] = busy.p.returncode
    assert busy.p.returncode == 1 and b"bridge busy" in output, output
    (rd / "busy-04.out").write_bytes(output)
    a.line(b"whoami", b"\nuid=")
    a.line(b"id", b" caps=")
    a.line(b"echo edit-oX\x7fk", b"\nedit-ok")
    a.line(b"\x1b[A", b"\nedit-ok")
    a.send(b"echo cancelled > /host/CANCEL.RECEIPT")
    start = len(a.data)
    a.send(b"\x03")
    a.until(b"^C", start)
    a.line(b"cd /host", b"gosh> ")
    a.line(b"echo remote-owner-ok > REMOTE.RECEIPT", b"gosh> ")
    a.line(b"cat REMOTE.RECEIPT", b"\nremote-owner-ok")
    a.line("printf '\\e[32mUTF-8: café λ\\e[0m\\n'".encode(),
           "\x1b[32mUTF-8: café λ\x1b[0m".encode())
    a.line(b"echo pipe-ok | cat", b"\npipe-ok")
    external_start = len(a.data)
    a.line(b"HELLO.ELF", b"elf: hello from HELLO.ELF")
    # C deliberately preserves exit/reap diagnostics. Drain the external
    # fixture's final lifecycle event BEFORE measuring idle editor ownership.
    a.until(b"tasks user-exec reaped", external_start)
    start = len(a.data)
    partial = b"echo partial > /host/PARTIAL.RECEIPT"
    a.send(partial)
    a.until(b"gosh> " + partial, start)
    a.drain(.5)
    idle_start = len(a.data)
    start = time.monotonic()
    while time.monotonic() - start < 11:
        a.drain(.1)
    idle = bytes(a.data[idle_start:])
    assert idle == b"", "idle partial line contaminated: " + repr(idle)
    (rd / "idle-04.bin").write_bytes(idle)
    a.disconnect("first-attach")
    time.sleep(.5)
    b = Session()
    b.until(b"gosh> ")
    b.line(b"echo same-boot-ok", b"\nsame-boot-ok")
    b.disconnect("reattach")
    (rd / "codes-04.json").write_text(json.dumps(codes))
except Exception as error:
    (rd / "driver-error.txt").write_text(str(error))
    sys.exit(1)
finally:
    for p in children:
        if p.poll() is None:
            p.terminate()
            try:
                p.wait(timeout=5)
            except subprocess.TimeoutExpired:
                p.kill()
                p.wait()
PY

vgate_setup_python <<'PY'
import os, pathlib, shutil, subprocess, sys
rd = pathlib.Path(os.environ["RUN_DIR"])
share = rd / "share"
(rd / "monitor-share").mkdir()
shutil.copyfile(".build/go/GOSH.ELF", share / "GOSH.ELF")
subprocess.run([sys.executable, "tools/mkhello-elf.py", str(share / "HELLO.ELF")], check=True)
(share / "SETTINGS.TXT").write_text("#v2\nwm=none\nshell=sh\n")
for name, text in (("gosh.secret", "gosh-gate-private"), ("wrong.secret", "wrong")):
    p = rd / name
    p.write_text(text + "\n")
    p.chmod(0o600)
with (rd / "remote-driver.log").open("wb") as log:
    subprocess.Popen([sys.executable, str(rd / "remote-driver.py")], stdout=log, stderr=log)
PY

vgate_run 04 -- --console-tcp '127.0.0.1:24681' --console-tcp-secret-file '$RUN_DIR/gosh.secret' --timeout 85

vgate_assert 01 client-contains 'auth failed'
vgate_assert 01 output-contains 'console-tcp: auth failed'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 02 client-contains 'remote-console-ok'
vgate_assert 02 serial-contains 'remote-console-ok'
vgate_assert 02 output-contains 'console-tcp: client authenticated'
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 03 output-contains 'secret from file'
vgate_assert 03 output-contains 'console-tcp: auth timeout'
vgate_assert 03 output-contains 'console-tcp: client authenticated'
vgate_assert 03 client-contains 'remote-console-ok'
vgate_assert 03 serial-contains 'remote-console-ok'
vgate_assert 03 serial-absent '[EXC] parking:'
vgate_assert 03 python <<'PY'
import os, shutil
run = os.environ['RUN_DIR']
silent = open(os.path.join(run, 'client-03-silent.out'), 'rb').read()
assert b'VIRELAIOS-AUTH/1 hmac-sha256' in silent, silent
assert b'console-tcp: auth timeout' in silent, silent
out = open(os.path.join(run, 'run-%s.out' % os.environ['VG_TAG']), 'rb').read()
assert b'f1l3-s3cret' not in out, 'the bridge secret leaked into runner output'
import json
assert json.load(open(os.path.join(run, "codes-03.json"))) == {"silent": 0, "monitor-auth": 0}
for name in ("client-03-silent.out", "client-03.log", "codes-03.json"):
    shutil.copyfile(os.path.join(run, name),
                    "artifacts/live-remote-console-" + name + os.environ.get("VIRELAI_GATE_SUFFIX", ""))
PY

vgate_assert 04 serial-contains 'login: shell=sh -> GOSH.ELF serial'
vgate_assert 04 serial-contains 'gosh: attached'
vgate_assert 04 serial-contains 'gosh: prompt'
vgate_assert 04 serial-absent 'SETTINGS.TXT refused'
vgate_assert 04 serial-absent 'gosh: attach failed'
vgate_assert 04 serial-absent 'gosh: monitor'
vgate_assert 04 serial-absent 'gotabwm: ready'
vgate_assert 04 serial-absent '[EXC]'
vgate_assert 04 client-contains 'elf: hello from HELLO.ELF'
vgate_assert 04 client-contains 'same-boot-ok'
vgate_assert 04 share-equals REMOTE.RECEIPT receipt.expected
vgate_assert 04 share-equals GOSH-HISTORY.TXT history.expected
vgate_assert 04 capture-empty driver-error.txt
vgate_assert 04 capture-empty idle-04.bin
vgate_assert 04 python <<'PY'
import json, os, pathlib, shutil
rd = pathlib.Path(os.environ["RUN_DIR"])
codes = json.loads((rd / "codes-04.json").read_text())
assert codes == {"silent": 0, "monitor-auth": 0, "wrong-auth": 1, "busy": 1,
                 "first-attach": 0, "reattach": 0}, codes
ser = pathlib.Path(os.environ["VG_SER"]).read_bytes()
assert ser.count(b"login: shell=sh -> GOSH.ELF serial") == 1, "reattach booted a new guest"
assert ser.count(b"gosh: attached") == 1, "reattach launched a new GOSH"
assert b"^C" in ser, "cancel/disconnect did not clear partial input"
assert b"gosh: line echo partial" not in ser
for name in ("WRONG.RECEIPT", "CANCEL.RECEIPT", "PARTIAL.RECEIPT"):
    assert not (rd / "share" / name).exists(), name + " executed"
assert b"gosh-gate-private" not in (rd / "run-04.out").read_bytes()
assert b"gosh-gate-private" not in (rd / "client-04.out").read_bytes()
assert (rd / "idle-04.bin").read_bytes() == b""
for name in ("codes-04.json", "wrong-04.out", "busy-04.out", "remote-driver.log"):
    shutil.copyfile(rd / name, "artifacts/live-remote-console-" + name + os.environ.get("VIRELAI_GATE_SUFFIX", ""))
print("every client exit enforced; exact receipt/history; idle partial line quiet; same-boot reconnect")
PY
