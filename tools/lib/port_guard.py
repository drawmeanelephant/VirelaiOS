#!/usr/bin/env python3
"""One lane-lease contract for the loopback ports class-B setup hooks bind.

Every class-B spec's setup hook spawns a host-side server (flintd, a TLS
responder, a fixture relay) on a fixed 127.0.0.1 port. Until now each hook
cleared its port with

    lsof -ti tcp:<port> | xargs kill -9

which kills *any* holder — including a live sibling gate's mid-run server.
On a multi-worktree host that is not a race: worktree A's `just gate
live-dev-loop` and worktree B's `just gate live-flint-loop` collide
deterministically, the later setup SIGKILLs the earlier run's flintd, and
the earlier gate fails legs that have nothing wrong with the code under
test (issue #3909). So does a second run of the *same* spec, which the
one-worktree-per-agent model makes routine. Re-running the failed gate
alone passes, which is why this kept being dismissed as load flake.

WHY NOT THE ppid HEURISTIC
These specs spawn their server with `start_new_session=True`, so the moment
the setup hook's python exits (immediately) the server is reparented to pid
1 — verified: a detached child of a live python is `ppid 1`. Every holder
on these ports therefore *looks* orphaned, including a sibling gate's live
one, so "ppid == 1 means leftover" would reap exactly the server this card
exists to protect. Ownership has to be recorded, not inferred.

THE LEASE
A claim takes an exclusive `flock` on `$TMPDIR/virelai-gate-lanes/lane-<port>.lock`.
The lock is handed to a small detached KEEPER process (via `pass_fds`, so
the inherited open file description keeps the lock). The keeper lives while
BOTH of these hold:

  * the gate that claimed it is still alive (the claiming setup hook's
    parent — under `vgate.sh` that is the gate process itself), and
  * the lane still has a listener, after a short grace period for the
    server to come up.

Either ending releases the lease, and the kernel releases it anyway if the
keeper dies. That gives exactly the two behaviours the card asks for:

  * a LIVE sibling run holds the lease -> REFUSE (exit 3), naming the
    holder's pid, command and run dir; the holder is never signaled;
  * a DEAD run's leftover is reaped, because its keeper is gone (guardian
    died) so the lease is free and the stale listener is a leftover by
    definition. The #3731 self-heal is preserved.

A claim from the SAME run dir (a spec that deliberately restarts its server,
e.g. go-git's second 24541 block) is a RECLAIM, not a conflict: its own
leftover is reaped and the lease is retaken. Anything else refuses.

CLI, for a setup hook that wants no import plumbing:

    python3 tools/lib/port_guard.py <port> [MARKER]

    exit 0  lane is yours (possibly after reaping your own/dead leftover)
    exit 3  refused: a live sibling holds it; the message names the squatter

Import, for a setup hook that wants the helper in-process:

    sys.path.insert(0, os.path.join("tools", "lib"))
    import port_guard
    port_guard.reap_or_refuse(4749)

LANES. A port is only meaningful as one spec's own lane. Three were shared
and are now split, because a shared lane means the two gates refuse each
other instead of running in parallel (the multiagent model in AGENTS.md
expects parallel worktrees):

    4748  live-dev-loop    (kept — first claimant)
    4751  live-flint-loop  (was 4748; also 4749 before that)
    4752  live-godiff-loop (was 4748)
    4749  go-flint         (kept — its own lane, documented in go-flint.spec)
    4763  live-doom        (was 4749 — it used to kill go-flint's flintd)
    24788 live-quickjs     (kept — its own lane)
    24792 live-supervise   (was 24788 — it used to kill live-quickjs's seat)

Anything not in this table is single-claimant; if you add a hook, claim a
fresh port and note it here rather than reusing a neighbour's lane.

This module is NOT a gate (AGENTS.md: no new tools/verify-*.sh) — it is the
shared library the existing setup hooks call.
"""
import errno
import fcntl
import json
import os
import signal
import subprocess
import sys
import time

# Exit code for "a live sibling holds the lane, I did not touch it".
REFUSED = 3

_POLL_S = 0.1
_REAP_TIMEOUT_S = 10.0
_GRACE_S = 30.0          # how long a fresh lease waits for its server to bind
_KEEPER_POLL_S = 0.25


def _lane_dir():
    return os.environ.get("VIRELAI_LANE_DIR") or os.path.join(
        os.environ.get("TMPDIR", "/tmp"), "virelai-gate-lanes")


def _lock_path(port):
    return os.path.join(_lane_dir(), "lane-%d.lock" % port)


def _lease_path(port):
    return os.path.join(_lane_dir(), "lane-%d.json" % port)


def _holders(port):
    """PIDs with a TCP listener on 127.0.0.1:port (macOS lsof)."""
    try:
        out = subprocess.check_output(
            ["lsof", "-nP", "-sTCP:LISTEN", "-iTCP:%d" % port, "-t"],
            text=True, stderr=subprocess.DEVNULL)
    except (OSError, subprocess.CalledProcessError):
        return []
    return [int(p) for p in out.split() if p.strip().isdigit()]


def _ps(pid):
    """'ppid command...' for pid, or '' if it is already gone."""
    try:
        return subprocess.check_output(
            ["ps", "-o", "ppid=,command=", "-p", str(pid)],
            text=True, stderr=subprocess.DEVNULL).strip()
    except (OSError, subprocess.CalledProcessError):
        return ""


def _alive(pid):
    try:
        os.kill(pid, 0)
        return True
    except OSError as e:
        return e.errno == errno.EPERM


def _read_lease(port):
    try:
        with open(_lease_path(port)) as f:
            return json.load(f)
    except (OSError, ValueError):
        return {}


def _write_lease(port, **fields):
    path = _lease_path(port)
    data = _read_lease(port)
    data.update(fields)
    tmp = path + ".tmp"
    try:
        with open(tmp, "w") as f:
            json.dump(data, f)
        os.replace(tmp, path)
    except OSError:
        pass


def _write_lease_fresh(lane, **fields):
    """Replace the lease outright — a new owner, not an update."""
    path = _lease_path(lane)
    tmp = path + ".tmp"
    try:
        with open(tmp, "w") as f:
            json.dump(fields, f)
        os.replace(tmp, path)
    except OSError:
        pass


def _gate_pid():
    """The gate process whose lifetime bounds the lease.

    vgate.sh exports VIRELAI_GATE_PID=$$; without it (a hand run) walk up to
    the outermost ancestor, which is the shell/session that started the run.
    The setup hook itself is NOT a valid guardian: it exits the moment it
    spawns the server, so a lease keyed on it would release immediately.
    """
    env = os.environ.get("VIRELAI_GATE_PID")
    if env and env.isdigit():
        return int(env)
    pid, seen = os.getpid(), 0
    while seen < 32:
        ps = _ps(pid)
        try:
            ppid = int(ps.split(None, 1)[0])
        except (IndexError, ValueError):
            break
        if ppid <= 1:
            break
        pid = ppid
        seen += 1
    return pid


def _signal(pid, sig):
    try:
        os.kill(pid, sig)
    except OSError:
        pass


def _reap(port, why):
    """Kill every listener on the lane and wait for the port to go quiet."""
    pids = _holders(port)
    if not pids:
        return True
    for pid in pids:
        print("port_guard: reaping pid=%d on 127.0.0.1:%d (%s): %s"
              % (pid, port, why, _ps(pid) or "gone"), file=sys.stderr)
        _signal(pid, signal.SIGTERM)
    for _ in range(int(3.0 / _POLL_S)):
        if not _holders(port):
            return True
        time.sleep(_POLL_S)
    for pid in _holders(port):
        _signal(pid, signal.SIGKILL)
    for _ in range(int((_REAP_TIMEOUT_S - 3.0) / _POLL_S)):
        if not _holders(port):
            return True
        time.sleep(_POLL_S)
    return not _holders(port)


def _spawn_keeper(port, lock_fd, guardian_pid):
    """Detach a keeper that inherits lock_fd and holds the lease."""
    subprocess.Popen(
        [sys.executable, os.path.abspath(__file__), "--keeper", str(port),
         str(lock_fd), str(guardian_pid)],
        pass_fds=(lock_fd,), start_new_session=True,
        stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL)


def _try_lock(lock_path):
    """Exclusive non-blocking flock; returns an open fd, or None if busy."""
    os.makedirs(os.path.dirname(lock_path), exist_ok=True)
    fd = os.open(lock_path, os.O_RDWR | os.O_CREAT, 0o644)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        return fd
    except OSError:
        os.close(fd)
        return None


def reap_or_refuse(port, marker=None, run_id=None):
    """Take the lane lease for this run, or refuse naming the live holder.

    marker is an optional substring identifying this lane's own server; it is
    only used in messages. run_id identifies the claiming run (defaults to
    $RUN_DIR, which vgate sets, else the cwd): a claim from the same run_id is
    a reclaim of our own lane, not a conflict.
    """
    run_id = run_id or os.environ.get("RUN_DIR") or os.getcwd()
    guardian = _gate_pid()
    lock_path = _lock_path(port)
    deadline = time.monotonic() + _REAP_TIMEOUT_S

    while True:
        fd = _try_lock(lock_path)
        if fd is not None:
            # The lane is ours. Any listener still there predates us.
            if _holders(port):
                _reap(port, "leftover on a lane whose lease was free")
            if _holders(port):
                os.close(fd)
                sys.stderr.write(
                    "port_guard: REFUSED — 127.0.0.1:%d stayed bound after the "
                    "lease was taken; holder: %s\n"
                    % (port, ", ".join("%d %s" % (p, _ps(p))
                                       for p in _holders(port))))
                sys.exit(REFUSED)
            os.makedirs(_lane_dir(), exist_ok=True)
            _write_lease_fresh(port, run_id=run_id, port=port,
                               cmd=marker or "", guardian_pid=guardian,
                               claimed_at=time.time(), server_pid=None)
            _spawn_keeper(port, fd, guardian)
            os.close(fd)   # the keeper's inherited fd keeps the lock
            return

        lease = _read_lease(port)
        if lease.get("run_id") == run_id:
            # Same run: this spec restarts its own server. Clear our leftover
            # and wait for our keeper to notice and release the lease.
            _reap(port, "this run's own previous server (reclaim)")
            if time.monotonic() > deadline:
                sys.stderr.write(
                    "port_guard: REFUSED — 127.0.0.1:%d: this run's own lease "
                    "did not release within %.0fs\n"
                    % (port, _REAP_TIMEOUT_S))
                sys.exit(REFUSED)
            time.sleep(_POLL_S)
            continue

        pid = lease.get("server_pid")
        cmd = lease.get("cmd") or "unknown server"
        holder = ("pid %d (%s)" % (pid, _ps(pid) or cmd) if pid
                  else "pid unknown (%s)" % cmd)
        sys.stderr.write(
            "port_guard: REFUSED — 127.0.0.1:%d is held by a LIVE sibling gate "
            "run (%s, run dir %s).\n"
            "port_guard: it is not touched. Give this spec its own lane port "
            "instead of sharing one (see the LANES table in "
            "tools/lib/port_guard.py); re-run once that gate finishes.\n"
            % (port, holder, lease.get("run_id") or "unknown"))
        sys.exit(REFUSED)


def keeper_main(port, lock_fd, guardian_pid):
    """Hold the lease while the gate and its server are both alive."""
    # Grace: the setup hook spawns the server right after claiming.
    deadline = time.monotonic() + _GRACE_S
    while time.monotonic() < deadline:
        if not _alive(guardian_pid):
            return 0
        pids = _holders(port)
        if pids:
            _write_lease(port, server_pid=pids[0], server_ps=_ps(pids[0]))
            break
        time.sleep(_KEEPER_POLL_S)
    while _alive(guardian_pid):
        pids = _holders(port)
        if not pids:
            break
        if _read_lease(port).get("server_pid") != pids[0]:
            _write_lease(port, server_pid=pids[0], server_ps=_ps(pids[0]))
        time.sleep(_KEEPER_POLL_S)
    _write_lease(port, released_at=time.time())
    os.close(lock_fd)
    return 0


def await_release(port, timeout_s=5.0):
    """True if the lane has no listener within timeout_s."""
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        if not _holders(port):
            return True
        time.sleep(_POLL_S)
    return not _holders(port)


def main(argv):
    if len(argv) >= 2 and argv[1] == "--keeper":
        return keeper_main(int(argv[2]), int(argv[3]), int(argv[4]))
    if len(argv) < 2 or not argv[1].isdigit():
        sys.stderr.write("usage: port_guard.py <port> [MARKER]\n"
                         "  Leases 127.0.0.1:<port> for this run's server: "
                         "reaps a dead run's leftover, refuses a live one.\n")
        return 2
    reap_or_refuse(int(argv[1]), argv[2] if len(argv) > 2 else None)
    print("port_guard: 127.0.0.1:%s leased for this run" % argv[1],
          file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
