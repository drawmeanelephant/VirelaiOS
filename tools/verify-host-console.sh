#!/usr/bin/env bash
# M1.5 agent A gate: host-side interactive serial plumbing (host-only).
#
# Verifies on the real Apple silicon host:
#   1. The Swift runner builds.
#   2. A scripted console run (piped stdin, no TTY) forwards host input
#      bytes into the serial attachment (--debug-input), tees guest output
#      to the terminal + log, and ends cleanly by timeout.
#   3. A PTY console run engages character mode, and ^C / SIGINT restores
#      the terminal (canonical+echo) and exits 130.
#
# Run from the repo root. Requires an existing artifacts/disk.img.
set -euo pipefail

echo "=== m15 host plumbing gate: swift build runner ==="
source tools/lib/gate-run.sh
trap gate_end EXIT   # drops the private runner copy (issue #3689)
gate_build_runner

echo "=== gate 2: scripted console run (piped stdin, no TTY) ==="
set +e
printf 'hello\r' | "$VZ_RUNNER_BIN" artifacts/disk.img artifacts/m15-host-console.log --console --timeout 6 --debug-input > artifacts/m15-host-console-scripted.txt 2>&1
RC=$?
set -e
echo "scripted run exit code: $RC (expected 0 = session ended by timeout)"
grep -q 'interactive input: enabled' artifacts/m15-host-console-scripted.txt || { echo "FAIL: missing interactive-input diagnostic"; exit 1; }
grep -q 'input → serial attachment: 6 bytes' artifacts/m15-host-console-scripted.txt || { echo "FAIL: host input bytes were not shown as forwarded to the serial attachment"; exit 1; }
grep -q 'session timed out after 6s' artifacts/m15-host-console-scripted.txt || { echo "FAIL: session did not end by timeout"; exit 1; }
[ -f artifacts/m15-host-console.log ] || { echo "FAIL: serial log not created"; exit 1; }
echo "gate 2 PASS"

echo "=== gate 3: PTY console run — character mode, SIGINT, terminal restore ==="
VZ_RUNNER_BIN="$VZ_RUNNER_BIN" python3 - <<'PY'
import fcntl
import os
import pty
import select
import signal
import sys
import termios
import time

runner = os.environ["VZ_RUNNER_BIN"]
disk = "artifacts/disk.img"
log = "artifacts/m15-host-console-pty.log"

master, slave = pty.openpty()

# Ensure the pty starts canonical + echo (a normal terminal).
attrs = termios.tcgetattr(slave)
attrs[3] |= termios.ICANON | termios.ECHO
termios.tcsetattr(slave, termios.TCSANOW, attrs)

pid = os.fork()
if pid == 0:
    # Child B: becomes the session leader with the pty as controlling tty.
    os.setsid()
    fcntl.ioctl(slave, termios.TIOCSCTTY, 0)
    os.dup2(slave, 0)
    os.dup2(slave, 1)
    os.dup2(slave, 2)
    os.close(master)
    os.close(slave)

    rp = os.fork()
    if rp == 0:
        # Grandchild: the actual runner, in B's session/pgrp.
        os.execv(runner, [runner, disk, log, "--console", "--timeout", "30", "--debug-input"])

    time.sleep(5)  # runner startup, raw mode, and input forwarding
    os.kill(rp, signal.SIGINT)  # the documented Ctrl-C path (ISIG is on)

    # Wait for the runner with a safety SIGTERM fallback.
    deadline2 = time.time() + 15
    st = None
    while True:
        done, st = os.waitpid(rp, os.WNOHANG)
        if done:
            break
        if time.time() > deadline2:
            os.kill(rp, signal.SIGTERM)
            _, st = os.waitpid(rp, 0)
            break
        time.sleep(0.2)

    if os.WIFEXITED(st):
        code = os.WEXITSTATUS(st)
    elif os.WIFSIGNALED(st):
        code = -os.WTERMSIG(st)
    else:
        code = -1

    # We are still the live session leader holding the controlling tty,
    # so the pty's termios is queryable and must have been restored by the
    # runner's SIGINT handler (canonical + echo, as saved at startup).
    try:
        lflag = termios.tcgetattr(0)[3]
        restored = (lflag & termios.ICANON) != 0 and (lflag & termios.ECHO) != 0
    except Exception:
        restored = False
    print("pty child exit code:", code)
    print("terminal restored (ICANON+ECHO set):", restored)
    ok = code == 130 and restored
    print("gate 3:", "PASS" if ok else "FAIL")
    sys.stdout.flush()
    os._exit(0 if ok else 1)

# Parent A: type into the pty and capture the full transcript.
out = b""
written_hi = False
st = None
deadline = time.time() + 30
while time.time() < deadline:
    r, _, _ = select.select([master], [], [], 0.5)
    if r:
        try:
            chunk = os.read(master, 8192)
        except OSError:
            break
        if not chunk:
            break
        out += chunk
    if not written_hi:
        os.write(master, b"hi\r")
        written_hi = True
    done, st = os.waitpid(pid, os.WNOHANG)
    if done:
        break
if st is None:
    _, st = os.waitpid(pid, 0)

try:
    os.close(master)
except OSError:
    pass

text = out.decode("utf-8", "replace")
print("----- pty transcript (captured) -----")
print(text)
print("--------------------------------------")
ok = (
    os.WIFEXITED(st)
    and os.WEXITSTATUS(st) == 0
    and "input → serial attachment:" in text
    and "caught signal 2" in text
)
print("gate 3 (parent):", "PASS" if ok else "FAIL")
sys.exit(0 if ok else 1)
PY
echo "gate 3 PASS"
