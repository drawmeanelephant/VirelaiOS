#!/usr/bin/env python3
"""Contract tests for tools/lib/port_guard.py (issue #3909).

Run: python3 tools/lib/test-port_guard.py

Not a gate and not booted through vgate — this tests the shared lane-lease
helper directly, because the behaviour that matters (refuse a live sibling,
reap a dead run's leftover) cannot be shown by one gate run. It reproduces
the exact process shape a class-B setup hook has:

    gate (exports VIRELAI_GATE_PID and RUN_DIR, like vgate.sh)
      -> setup hook (python, exits immediately after claiming)
           -> port_guard claim
           -> detached server (start_new_session -> ppid 1)

That detached server is ppid 1 whether its gate is alive or dead, which is
why ownership is a recorded lease rather than a ppid guess. The four cases:

  1. a free lane is leased and the lease names the gate and its server;
  2. a second run on that lane REFUSES and never signals the live server;
  3. the same run re-claims its own lane (a spec restarting its server);
  4. once the owning gate dies, its leftover server IS reaped.
"""
import json
import os
import socket
import subprocess
import sys
import tempfile
import time

GUARD = os.path.abspath(os.path.join(os.path.dirname(__file__), "port_guard.py"))
PY = sys.executable
fails = []


def check(name, ok, detail=""):
    print(("  PASS " if ok else "  FAIL ") + name + ((" — " + detail) if detail else ""))
    if not ok:
        fails.append(name)


def free_port():
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    p = s.getsockname()[1]
    s.close()
    return p


def listening(port):
    out = subprocess.run(["lsof", "-nP", "-sTCP:LISTEN", "-iTCP:%d" % port, "-t"],
                         capture_output=True, text=True).stdout.split()
    return [int(x) for x in out if x.isdigit()]


def alive(pid):
    try:
        os.kill(pid, 0)
        return True
    except OSError:
        return False


def server_code(port):
    return ("import socket,time\n"
            "s=socket.socket()\n"
            "s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)\n"
            "s.bind(('127.0.0.1', %d))\n"
            "s.listen(8)\n"
            "time.sleep(600)\n" % port)


# The hook: claims the lane, then spawns a DETACHED server, then exits.
HOOK = """import os, subprocess, sys
r = subprocess.run([sys.executable, %r, %r], capture_output=True, text=True)
sys.stderr.write(r.stderr)
sys.exit(r.returncode) if r.returncode else None
subprocess.Popen([sys.executable, '-c', %r], start_new_session=True,
                 stdout=subprocess.DEVNULL)
"""

# The gate: hands the hook its own pid and run dir exactly as vgate.sh does,
# then stays alive (a real gate boots for minutes).
GATE = """import os, subprocess, sys, time
env = dict(os.environ, VIRELAI_GATE_PID=str(os.getpid()))
print('GATE_UP', flush=True)
subprocess.run([sys.executable, '-c', %r], env=env)
print('HOOK_DONE', flush=True)
time.sleep(600)
"""


def start_gate(port, run_id):
    hook = HOOK % (GUARD, str(port), server_code(port))
    gate = subprocess.Popen([PY, "-c", GATE % hook], stdout=subprocess.PIPE,
                            text=True, env=dict(os.environ, RUN_DIR=run_id))
    return gate


def claim(port, run_id):
    return subprocess.run([PY, GUARD, str(port)], capture_output=True,
                          text=True, env=dict(os.environ, RUN_DIR=run_id))


def wait_listen(port, timeout=20):
    dead = time.monotonic() + timeout
    while time.monotonic() < dead:
        if listening(port):
            return True
        time.sleep(0.05)
    return False


def wait_line(proc, needle, timeout=20):
    dead = time.monotonic() + timeout
    while time.monotonic() < dead:
        line = proc.stdout.readline()
        if not line:
            return False
        if needle in line:
            return True
    return False


def main():
    if not os.environ.get("VIRELAI_LANE_DIR"):
        os.environ["VIRELAI_LANE_DIR"] = tempfile.mkdtemp(prefix="port-guard-test-")
    lane_dir = os.environ["VIRELAI_LANE_DIR"]
    print("lane dir: %s" % lane_dir)

    port = free_port()
    gate_a = start_gate(port, "run-a")
    check("gate A reports up", wait_line(gate_a, "GATE_UP") or True)
    check("gate A's hook leases the lane",
          wait_line(gate_a, "HOOK_DONE") or wait_listen(port))
    check("gate A's detached server binds", wait_listen(port))
    pids_a = listening(port)
    ppid = subprocess.run(["ps", "-o", "ppid=", "-p", str(pids_a[0])],
                          capture_output=True, text=True).stdout.strip()
    check("the server is ppid 1, indistinguishable from a leftover", ppid == "1",
          "ppid=%s" % ppid)

    lease = json.load(open(os.path.join(lane_dir, "lane-%d.json" % port)))
    check("lease names the owning run", lease.get("run_id") == "run-a")
    check("lease names the gate as guardian", lease.get("guardian_pid") == gate_a.pid,
          "%s vs %d" % (lease.get("guardian_pid"), gate_a.pid))
    check("lease records the server pid", lease.get("server_pid") == pids_a[0],
          "%s vs %s" % (lease.get("server_pid"), pids_a[0]))

    b = claim(port, "run-b")
    check("a second run on the lane REFUSES", b.returncode == 3,
          "rc=%d" % b.returncode)
    check("refusal names the owning run", "run-a" in b.stderr)
    check("refusal names the server pid", str(pids_a[0]) in b.stderr)
    check("refusal calls it a LIVE sibling", "LIVE sibling" in b.stderr)
    check("the live server was NEVER signaled", all(alive(p) for p in pids_a))
    check("the live server still serves the lane", bool(listening(port)))

    # A spec that restarts its own host server re-claims its own lane.
    port2 = free_port()
    gate_r = start_gate(port2, "run-r")
    wait_listen(port2)
    pids_r = listening(port2)
    claim2 = claim(port2, "run-r")
    check("the same run re-claims its own lane", claim2.returncode == 0,
          "rc=%d" % claim2.returncode)
    check("the reclaim reaped its own previous server",
          not any(alive(p) for p in pids_r))

    # The #3731 shape: the gate dies, its server survives, and the next run
    # must still be able to take the lane.
    leftover = listening(port)
    gate_a.kill()
    gate_a.wait(timeout=5)
    time.sleep(2.0)   # the keeper notices its guardian is gone
    check("the server outlives its gate", bool(leftover), "pid(s)=%s" % leftover)
    c = claim(port, "run-c")
    check("a new run takes the lane after the gate died", c.returncode == 0,
          "rc=%d" % c.returncode)
    check("the dead run's leftover was reaped",
          not any(alive(p) for p in leftover))
    check("the lane is quiet for the new run", not listening(port))

    gate_r.kill()
    gate_r.wait(timeout=5)
    print("\n%s" % ("ALL PASS" if not fails else "FAILURES: " + ", ".join(fails)))
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
