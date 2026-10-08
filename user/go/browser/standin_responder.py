#!/usr/bin/env python3
"""Original hermetic M93 fixtures over TLS 1.3; no public network access."""
import argparse
import socket
import ssl
import time
from pathlib import Path

HOSTS = {
    "en.wikipedia.org": "wikipedia.html",
    "github.com": "github.html",
    "developer.mozilla.org": "mdn.html",
}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--cert", required=True)
    parser.add_argument("--key", required=True)
    parser.add_argument("--timeout", type=int, default=3600)
    parser.add_argument("--negative", action="store_true")
    args = parser.parse_args()
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.minimum_version = ctx.maximum_version = ssl.TLSVersion.TLSv1_3
    ctx.load_cert_chain(args.cert, args.key)

    def sni(_socket, name, _context):
        print("m93f-peer: SNI " + str(name), flush=True)

    ctx.set_servername_callback(sni)
    pages = Path(__file__).resolve().parents[3] / "tests/fixtures/web/reference"
    listener = socket.socket()
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind(("127.0.0.1", args.port))
    listener.listen(8)
    end = time.monotonic() + args.timeout
    print("m93f-peer: listening " + str(args.port), flush=True)
    while time.monotonic() < end:
        listener.settimeout(min(2, max(0.01, end-time.monotonic())))
        try:
            raw, _ = listener.accept()
        except socket.timeout:
            continue
        try:
            raw.settimeout(30)
            with ctx.wrap_socket(raw, server_side=True) as conn:
                request = bytearray()
                while b"\r\n\r\n" not in request and len(request) <= 16384:
                    chunk = conn.recv(1024)
                    if not chunk:
                        break
                    request.extend(chunk)
                lines = bytes(request).split(b"\r\n")
                request_line = lines[0].decode("ascii", errors="replace")
                print("m93f-peer: request " + request_line, flush=True)
                if args.negative:
                    body = b"NEGATIVE MUST NOT LOAD"
                else:
                    host = ""
                    for line in lines[1:]:
                        if line.lower().startswith(b"host:"):
                            host = line.split(b":", 1)[1].strip().decode("ascii").split(":")[0]
                    parts = request_line.split()
                    target = parts[1] if len(parts) == 3 else ""
                    if host not in HOSTS or len(parts) != 3 or parts[0] != "GET":
                        body = b"invalid request"
                    elif "?" in target and target.startswith("/w/index.php?"):
                        body = b"<h1>Search results</h1><p>Original hermetic GET result.</p>"
                    elif target.endswith("/m93-next"):
                        body = b"<h1>Internal page</h1><p>Original hermetic navigation destination.</p>"
                    elif target.endswith(".css"):
                        body = b"p { color:#202122; }"
                    else:
                        source = (pages / HOSTS[host]).read_bytes()
                        # Add one original internal destination and linked CSS
                        # while preserving the authored stand-in's content.
                        body = source.replace(b"</head>", b'<link rel="stylesheet" href="/m93.css"></head>')
                        body = body.replace(b"<body>", b'<body><p><a href="/m93-next">Internal page</a></p>')
                response = (b"HTTP/1.0 200 OK\r\nContent-Length: " + str(len(body)).encode() +
                            b"\r\nConnection: close\r\nContent-Type: text/html\r\n\r\n" + body)
                conn.sendall(response)
                print("m93f-peer: served " + str(len(body)), flush=True)
        except (ssl.SSLError, OSError) as exc:
            print("m93f-peer: refused " + str(exc), flush=True)
        finally:
            raw.close()
    listener.close()


if __name__ == "__main__":
    main()
