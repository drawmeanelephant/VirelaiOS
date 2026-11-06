#!/usr/bin/env python3
"""vgate-client.py -- the vgate during-run TCP client hook (M46 RC2, #1069).

Launched in the BACKGROUND by tools/gate/vgate.sh for the run tagged
$VG_TAG (see tools/gate/SPEC.md `vgate_client`). It waits for an optional
serial marker, connects to --addr with retry, sends a scripted fixture,
reads until --expect (or EOF/--timeout), writes the full capture to
$RUN_DIR/client-$VG_TAG.out, and exits 0 on success.

Environment (set by the harness): RUN_DIR, VG_TAG, VG_SER.

M50 TS4 (#1138, ADR 0024 D7): `--hmac-secret S` answers the host bridge's
`VIRELAIOS-AUTH/1 hmac-sha256 <hex-challenge>` line with
hex(HMAC-SHA256(S, "VIRELAIOS-AUTH/1 hmac-sha256" || 0x00 || challenge))
before sending the payload (Python stdlib hmac/hashlib, no new dependency).

Exit codes: 0 success; 1 expectation/timeout/connect failure; 2 usage.
--expect-fail inverts the connect result (0 iff the connect is refused or
times out) for negative gates such as "no listener".

M70g G2 (#1459): the hardened bridge tells a second client
`console-tcp: busy` and closes. `--retry-busy` treats that as "not yet"
and reconnects until --connect-timeout (a client waiting its turn);
without it the busy line is just captured (a client asserting the
refusal). `--hold S` keeps the connection open S seconds after the payload
before the normal read loop, so a spec can pin "client A still connected
while B connects". A send that fails because the bridge closed first (an
oversized line is refused mid-stream by design) is logged, not fatal: the
capture still records what the bridge said.
"""

import argparse
import hashlib
import hmac
import os
import socket
import sys
import time


def log(msg):
    tag = os.environ.get("VG_TAG", "?")
    print(f"vgate-client[{tag}]: {msg}", flush=True)


def parse_addr(spec):
    host, sep, port = spec.rpartition(":")
    if not sep or not host or not port:
        log(f"--addr wants host:port, got {spec!r}")
        sys.exit(2)
    try:
        return host, int(port)
    except ValueError:
        log(f"--addr port is not an integer: {spec!r}")
        sys.exit(2)


def ser_text(path):
    try:
        with open(path, "rb") as f:
            return f.read()
    except OSError:
        return b""


def wait_marker(path, marker, timeout):
    """Poll the serial log until MARKER appears (or timeout). Empty marker
    means "connect now". Returns True when satisfied."""
    if not marker:
        return True
    needle = marker.encode("utf-8", "replace")
    deadline = time.time() + timeout
    while time.time() < deadline:
        if needle in ser_text(path):
            return True
        time.sleep(0.2)
    return False


BUSY_LINE = b"console-tcp: busy"


def bridge_busy(s):
    """M70g G2: peek (bounded, 0.5 s) for the bridge's `console-tcp: busy`
    line. Returns True when the bridge refused us; the socket is then
    closed. On an idle no-secret bridge nothing arrives, so a --retry-busy
    connect costs a fixed 0.5 s — fine for a gate, noted for the reader."""
    s.settimeout(0.5)
    try:
        head = s.recv(len(BUSY_LINE) + 1, socket.MSG_PEEK)
    except socket.timeout:
        return False
    except OSError:
        # A refused client can be RST before the 'busy' line it was sent
        # is readable (a loaded bridge closes fast): a dead socket right
        # after a successful connect is not-us, so queue like a busy.
        # Close here too: connect() retries a busy socket every 0.25 s and
        # never closes it itself (the BUSY_LINE path above does the same
        # close), so returning without this leaks ~4 fds/s for the whole
        # wait and can reach the default 256-fd limit mid-gate.
        s.close()
        return True
    finally:
        s.settimeout(None)
    if head.startswith(BUSY_LINE):
        s.close()
        return True
    return False


def connect(host, port, timeout, retry_busy=False):
    """Retry-connect for up to `timeout` seconds. Returns a socket or None.
    With retry_busy, a connection the bridge answers `console-tcp: busy`
    counts as not-yet and is retried."""
    deadline = time.time() + timeout
    last = None
    while time.time() < deadline:
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.settimeout(2.0)
        try:
            s.connect((host, port))
            s.settimeout(None)
        except OSError as e:
            last = e
            s.close()
            time.sleep(0.25)
            continue
        if retry_busy and bridge_busy(s):
            last = "busy"
            time.sleep(0.25)
            continue
        return s
    log(f"connect {host}:{port} failed after {timeout:.0f}s ({last})")
    return None


def load_payload(args):
    if args.send_file:
        path = args.send_file
        if not os.path.isabs(path):
            path = os.path.join(os.environ.get("RUN_DIR", "."), path)
        with open(path, "rb") as f:
            return f.read()
    if args.send_text is not None:
        data = args.send_text.encode("utf-8")
        if data and data[-1:] not in (b"\n", b"\r"):
            data += b"\r"  # submit the line (CR == Enter on the guest)
        return data
    return b""


def recv_line(s, timeout):
    """Read one CR-stripped line from `s` (bounded), or None on EOF/timeout."""
    line = bytearray()
    deadline = time.time() + timeout
    while time.time() < deadline and len(line) <= 160:
        s.settimeout(max(0.1, min(1.0, deadline - time.time())))
        try:
            b = s.recv(1)
        except socket.timeout:
            continue
        except OSError:
            return None
        if not b:
            return None
        if b == b"\n":
            return bytes(line)
        if b != b"\r":
            line += b
    return None


def answer_challenge(s, secret, timeout):
    """M50 TS4 (#1138, ADR 0024 D6): read the host bridge's
    `VIRELAIOS-AUTH/1 hmac-sha256 <hex-challenge>` line and answer with
    hex(HMAC-SHA256(secret, 'VIRELAIOS-AUTH/1 hmac-sha256' || 0x00 ||
    challenge)) + newline. Returns None when no challenge parses."""
    line = recv_line(s, timeout)
    if line is None:
        return None
    parts = line.decode("utf-8", "replace").split(" ")
    if len(parts) != 3 or parts[0] != "VIRELAIOS-AUTH/1" or parts[1] != "hmac-sha256":
        log(f"unexpected challenge line: {line!r}")
        return None
    try:
        challenge = bytes.fromhex(parts[2])
    except ValueError:
        log(f"challenge is not hex: {parts[2]!r}")
        return None
    msg = b"VIRELAIOS-AUTH/1 hmac-sha256\x00" + challenge
    mac = hmac.new(secret.encode("utf-8"), msg, hashlib.sha256).hexdigest()
    s.sendall(mac.encode("ascii") + b"\n")
    return mac


def main():
    ap = argparse.ArgumentParser(add_help=False)
    ap.add_argument("--addr", required=True)
    ap.add_argument("--after", default="")
    ap.add_argument("--after-timeout", type=float, default=45.0)
    ap.add_argument("--connect-timeout", type=float, default=20.0)
    ap.add_argument("--send-text", default=None)
    ap.add_argument("--send-file", default=None)
    ap.add_argument("--expect", default=None)
    ap.add_argument("--expect-fail", action="store_true")
    ap.add_argument("--hmac-secret", default=None)
    ap.add_argument("--retry-busy", action="store_true")
    ap.add_argument("--hold", type=float, default=0.0)
    ap.add_argument("--timeout", type=float, default=20.0)
    ap.add_argument("--out", default=None)
    args = ap.parse_args()

    run_dir = os.environ.get("RUN_DIR", ".")
    tag = os.environ.get("VG_TAG", "run")
    ser = os.environ.get("VG_SER", os.path.join(run_dir, "vm-serial.log"))
    out = args.out or os.path.join(run_dir, f"client-{tag}.out")
    host, port = parse_addr(args.addr)

    if args.expect_fail:
        # Negative gate: the port must NOT accept. A short single-shot
        # attempt; success == refusal/timeout.
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.settimeout(max(args.connect_timeout, 1.0))
        try:
            s.connect((host, port))
            s.close()
            log(f"UNEXPECTED listener on {host}:{port} (--expect-fail)")
            with open(out, "wb") as f:
                f.write(b"<unexpected connection>\n")
            sys.exit(1)
        except OSError as e:
            log(f"refused as expected on {host}:{port} ({e})")
            with open(out, "wb") as f:
                f.write(b"<refused as expected>\n")
            sys.exit(0)

    if not wait_marker(ser, args.after, args.after_timeout):
        log(f"marker {args.after!r} not seen in {ser} within {args.after_timeout:.0f}s")
        sys.exit(1)

    s = connect(host, port, args.connect_timeout, retry_busy=args.retry_busy)
    if s is None:
        with open(out, "wb") as f:
            f.write(b"<connect failed>\n")
        sys.exit(1)

    # M50 TS4 (#1138, ADR 0024 D7): answer the host bridge's HMAC-SHA256
    # challenge before sending the payload. No --hmac-secret => the bridge
    # has no secret either and is byte-identical to the pre-TS4 path.
    if args.hmac_secret:
        answer_challenge(s, args.hmac_secret, max(args.timeout, 5.0))

    payload = load_payload(args)
    if payload:
        try:
            s.sendall(payload)
        except OSError as e:
            # The bridge may refuse mid-stream by design (oversized line);
            # what it said is still in the receive buffer for the capture.
            log(f"send interrupted ({e}); reading what the peer said")

    if args.hold > 0:
        # Stay connected (and quiet) so a concurrent client meets a busy
        # bridge; anything the guest says meanwhile is still captured.
        time.sleep(args.hold)

    capture = bytearray()
    expect = args.expect.encode("utf-8", "replace") if args.expect else None
    deadline = time.time() + args.timeout
    try:
        while time.time() < deadline:
            s.settimeout(max(0.1, min(1.0, deadline - time.time())))
            try:
                chunk = s.recv(4096)
            except socket.timeout:
                if expect is None:
                    break  # no expectation: a quiet settle is success
                continue
            if not chunk:
                break
            capture.extend(chunk)
            if expect is not None and expect in capture:
                break
    finally:
        try:
            s.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass
        s.close()

    with open(out, "wb") as f:
        f.write(capture)

    if expect is not None and expect not in capture:
        log(f"expected {args.expect!r} not seen ({len(capture)} bytes captured)")
        sys.exit(1)
    log(f"ok ({len(capture)} bytes captured -> {out})")
    sys.exit(0)


if __name__ == "__main__":
    main()
