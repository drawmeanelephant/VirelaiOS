#!/usr/bin/env python3
"""Scope one original hermetic peer process to its owning gate invocation."""
import os
import subprocess
import sys
import time


def main():
    parent = int(sys.argv[1])
    peer = subprocess.Popen(sys.argv[2:])
    try:
        while peer.poll() is None:
            try:
                os.kill(parent, 0)
            except ProcessLookupError:
                peer.terminate()
                try:
                    peer.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    peer.kill()
                    peer.wait()
                return
            time.sleep(0.1)
    finally:
        if peer.poll() is None:
            peer.terminate()
            peer.wait(timeout=5)
    raise SystemExit(peer.returncode)


if __name__ == "__main__":
    main()
