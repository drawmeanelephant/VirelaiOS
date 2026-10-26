#!/usr/bin/env python3
"""Port claims for the hermetic TLS peers the M93/live-web gates spawn.

A responder orphaned by a dead peer_watch keeps its loopback port for its
full timeout and silently answers for every later gate run: the app fetches
through it happily (the same pinned inter CA signs every run's leaf, so the
stale chain still verifies), while the run's own peer log stays empty and
the peer-side asserts fail three legs later (issue #3731).

claim() reaps an ORPHANED responder holding the port (peer_watch dead ->
reparented to pid 1) and refuses to start while a live-parent process
holds it — two concurrent runs cannot share these ports, so the second one
names the squatter instead of being impersonated by it. await_ready()
bounds the spawn-to-listening gap so a dead-on-bind peer fails at setup,
not inside a leg.
"""
import os
import signal
import subprocess
import sys
import time


def _holders(port):
    try:
        out = subprocess.check_output(
            ["lsof", "-nP", "-sTCP:LISTEN", "-iTCP:%d" % port, "-t"],
            text=True, stderr=subprocess.DEVNULL)
    except (OSError, subprocess.CalledProcessError):
        return []
    return [int(p) for p in out.split() if p.strip().isdigit()]


def _ps(pid):
    try:
        return subprocess.check_output(
            ["ps", "-o", "ppid=,command=", "-p", str(pid)],
            text=True, stderr=subprocess.DEVNULL).strip()
    except (OSError, subprocess.CalledProcessError):
        return ""


def claim(port, marker):
    """Free 127.0.0.1:port for this run's peer, or die naming the squatter."""
    for _ in range(250):
        pids = _holders(port)
        if not pids:
            return
        for pid in pids:
            ps = _ps(pid)
            try:
                ppid = int(ps.split(None, 1)[0])
            except (IndexError, ValueError):
                ppid = -1
            if marker in ps and ppid == 1:
                print("peer_guard: reaping orphaned %s pid=%d on :%d"
                      % (marker, pid, port))
                try:
                    os.kill(pid, signal.SIGTERM)
                except OSError:
                    pass
                continue
            sys.exit("peer_guard: 127.0.0.1:%d is held by pid %d (%s) — "
                     "a live gate run or a foreign service; refusing to let "
                     "it impersonate this run's peer (issue #3731)"
                     % (port, pid, ps or "unknown"))
        time.sleep(0.2)
    sys.exit("peer_guard: 127.0.0.1:%d stayed bound after 50s of reaping"
             % port)


def await_ready(log_path, needle, proc):
    """Bound the spawn-to-listening gap; a dead-on-bind peer fails HERE."""
    for _ in range(150):
        try:
            with open(log_path) as f:
                if needle in f.read():
                    return
        except OSError:
            pass
        if proc.poll() is not None:
            break
        time.sleep(0.1)
    try:
        body = open(log_path).read()
    except OSError:
        body = "(no log)"
    sys.exit("peer_guard: peer never printed %r — its log follows:\n%s"
             % (needle, body))
