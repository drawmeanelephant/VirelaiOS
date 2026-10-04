#!/usr/bin/env python3
"""Marker-driven input to the real guest, not a host JS evaluator."""
import argparse
import json
from pathlib import Path
import socket
import time


def drive(directory, tag, port, mode):
    serial = directory / ("vm-serial-" + tag + ".log")
    deadline = time.monotonic() + 1200
    sock = None
    capture = bytearray()
    observations = []

    def text():
        try:
            return serial.read_bytes()
        except FileNotFoundError:
            return b""

    def wait(marker, count=1):
        while time.monotonic() < deadline:
            if not directory.is_dir():
                raise RuntimeError("gate ended before guest marker: " + marker)
            if sock:
                try:
                    chunk = sock.recv(65536)
                    if chunk:
                        capture.extend(chunk)
                    if len(capture) > 2_000_000:
                        raise ValueError("CaptureLimit")
                except socket.timeout:
                    pass
            observed = text()
            if b"fault: QJS.BIN" in observed:
                raise RuntimeError("guest QJS.BIN fault before marker: " + marker)
            if observed.count(marker.encode()) >= count:
                return
            time.sleep(0.005)
        raise TimeoutError("guest marker missing: " + marker)

    wait("tasks user-el0 reaped")
    while time.monotonic() < deadline:
        try:
            sock = socket.create_connection(("127.0.0.1", port), timeout=1)
            sock.settimeout(0.02)
            break
        except OSError:
            time.sleep(0.1)
    if sock is None:
        raise TimeoutError("guest console connection failed")
    deadline = time.monotonic() + (580 if mode in ("evals", "receipt") else 170)
    receipt = {"repl": "REPL.R", "session": "SESSION.R", "evals": "EVALS.R",
               "diagnostics": "DIAG.R", "receipt": "RECEIPT.R", "eof": "EOF.R"}[mode]
    sock.sendall(f"pages\nexec QJS.BIN --repl --receipt=/host/{receipt}\n".encode())
    wait("qjs: ready eval=0")
    evals = 0

    def send(source):
        nonlocal evals
        # Every command is gated on a preceding guest readiness observation.
        start = time.monotonic_ns()
        sock.sendall(source.encode() + b"\n")
        evals += 1
        wait(f"qjs: ready eval={evals}\n")
        observations.append({"source": source, "eval": evals,
                             "host_elapsed_ms": (time.monotonic_ns() - start) / 1_000_000})

    if mode == "repl":
        for source in ("let n=40", "n+2", "throw new Error('ordinary')", "n"):
            send(source)
        sock.sendall(b":reset\n")
        wait(f"qjs: ready eval={evals}\n", 2)
        send("typeof n")
        send("try { print('x'.repeat(65536)); } catch(e) {} 42")
        send("6*7")
        send("function f(){return f()} try {f()}catch(e){} 42")
        send("6*7")
        send("try { 'x'.repeat(4194304) }catch(e){} 42")
        send("6*7")
        for source in ("import x from 'std'", "import('os')", "async function f(){}",
                       "(0,eval)('async function f(){}')", "Function('return async function(){}')()"):
            send(source)
            send("6*7")
        # Partial-line cancellation must not evaluate the prefix.
        sock.sendall(b"throw new Error('partial')\x03")
        wait("qjs: LineCancelled")
        send("6*7")
        # Deadline and caught-error loop use actual CNTVCT_EL0, no fake clock.
        send("for(;;){}")
        send("6*7")
        send("try {for(;;){}}catch(e){} 42")
        send("6*7")
        # Trigger only after the guest confirms that execution has entered.
        sock.sendall(b"print('CANCEL-ARM');for(;;){}\n")
        wait("CANCEL-ARM")
        sock.sendall(b"\x03" + b"6*7\n")
        evals += 2
        wait(f"qjs: ready eval={evals}\n")
        observations.append({"source": "active Ctrl-C + preserved next line", "eval": evals})
        # Native slow paths, with independent expected results or named refusal.
        for source in (
            "eval('('.repeat(4000)+'1'+')'.repeat(4000))",
            "let a='a'.repeat(10000); /^(a+)+b$/.test(a)",
            "String(1n<<1000000n).length",
            "'e\\u0301'.repeat(20000).normalize('NFC').length",
            "JSON.stringify(new Array(32768).fill([1,2,3])).length",
            "new Array(32768).fill(1).sort((a,b)=>a-b).length",
            "'x'.repeat(32768).split('').length",
            "print({toString(){for(;;){}}})",
        ):
            sock.sendall(b":reset\n")
            wait(f"qjs: ready eval={evals}\n", 2)
            send(source)
            send("6*7")
    elif mode == "session":
        # Sixteen exact eval-output budgets fill the real session budget.
        for _ in range(16):
            send("'x'.repeat(65535)")
        send("1")
    elif mode == "evals":
        # One bounded burst, delivered only after ready=0. The real tty loop
        # must evaluate all 256 lines and refuse the 257th, not a host loop.
        sock.sendall(b"0\n" * 257)
        wait("qjs: SessionLimit")
    elif mode == "receipt":
        for _ in range(16):
            send("'x'.repeat(65535)")
        # The prior guest ready=16 anchors this bounded 2400-byte burst.
        sock.sendall(b"undefined\n" * 240)
        wait("qjs: ReceiptLimit")
    elif mode == "eof":
        sock.sendall(b"\x04")
    elif mode == "diagnostics":
        for _ in range(256):
            sock.sendall(b"throw new Error('d'.repeat(250))\n")
            evals += 1
            # Either the next ready or the terminal budget refusal is owed.
            while time.monotonic() < deadline:
                if b"qjs: DiagnosticLimit" in text():
                    break
                if f"qjs: ready eval={evals}\n".encode() in text():
                    break
                try:
                    capture.extend(sock.recv(65536))
                except socket.timeout:
                    pass
            if b"qjs: DiagnosticLimit" in text():
                break
        wait("qjs: DiagnosticLimit")
    else:
        raise ValueError("UnknownMode")
    if mode not in ("evals", "diagnostics", "receipt", "eof"):
        sock.sendall(b":quit\n")
    wait("qjs: clean")
    wait("tasks user-exec reaped")
    sock.sendall(b"pages\nprocs receipt QJS.BIN\ntty\nsyscalls\necho qjs-gate-done\n")
    wait("qjs-gate-done\n")
    sock.sendall(b"shutdown\n")
    sock.close()
    (directory / ("driver-" + tag + ".out")).write_bytes(capture)
    return observations


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    parser.add_argument("tag")
    parser.add_argument("port", type=int)
    parser.add_argument("mode")
    args = parser.parse_args()
    record = args.directory / ("driver-" + args.tag + ".json")
    try:
        observed = drive(args.directory, args.tag, args.port, args.mode)
        record.write_text(json.dumps({"success": True, "observations": observed}, indent=2) + "\n")
    except Exception as error:
        if args.directory.is_dir():
            record.write_text(json.dumps({"success": False, "error": str(error)}, indent=2) + "\n")
        raise


if __name__ == "__main__":
    main()
