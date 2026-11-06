# go-fetch-https.spec -- M67c #1448: pin in-process HTTPS (M67b) and
# demote FETCHS.BIN to an unreferenced fallback (deleted outright in M71k #1570).
# M212a #3872 adds the --http2 legs: an ALPN-h2 responder proves the
# negotiated codec, a PUSH_PROMISE leg names its refusal, an ALPN-less
# peer falls back to HTTP/1.1, and a cleartext --http2 names h2c.
#
# GOFETCH.ELF dials the runner TLS responder (tlsresponder.py +
# --net-tcp-respond :relay) with the vendored AutoClaw fixture CA.
# Public-internet endpoints are out of scope. No Zig TLS helper is built any
# more (M71k #1570 deleted FETCHS.BIN); nothing is staged or exec'd for the
# Go apps. Negatives
# (wrong name, expired, bad chain) each fail closed; none hang.
#
# HOST PREREQUISITE: bash tools/go/build-web.sh fetch GOFETCH
# -> .build/go/GOFETCH.ELF. OpenSSL 3 for the expired-leaf fixture.

vgate_name go-fetch-https "M67c #1448 + M212a #3872: GOFETCH.ELF HTTPS in-process vs runner TLS responder; negatives fail closed; --http2 legs (h2, push refusal, ALPN-less fallback, h2c)"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 3 BOOTS

vgate_file script-ok.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOFETCH.ELF https://10.0.0.2:24550/
EOF

vgate_file script-name.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOFETCH.ELF https://10.0.0.2:24550/ wrong.example.com name
EOF

vgate_file script-expired.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOFETCH.ELF https://10.0.0.2:24552/ leaf.example.com expired
EOF

vgate_file script-chain.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOFETCH.ELF https://10.0.0.2:24553/ leaf.example.com chain
EOF

vgate_file script-h2.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOFETCH.ELF --http2 https://10.0.0.2:24554/
EOF

vgate_file script-alpnless.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOFETCH.ELF --http2 https://10.0.0.2:24555/
EOF

vgate_file script-push.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOFETCH.ELF --http2 https://10.0.0.2:24556/
EOF

vgate_file script-h2c.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOFETCH.ELF --http2 http://10.0.0.2/
EOF

# h2responder.py -- the M212a hermetic h2 peer: TLS 1.3 with ALPN h2, then
# a fixed SETTINGS/HEADERS/DATA transcript. The codec's whole test surface
# is the wire shape, so the responder hand-rolls frames: no h2 library.
vgate_file h2responder.py <<'EOF'
#!/usr/bin/env python3
"""h2 responder for the M212a #3872 GOFETCH --http2 gate legs.

TLS 1.3 + ALPN h2 (ssl.SSLContext set_alpn_protocols), then a fixed
transcript: read the client preface and its SETTINGS, answer server
SETTINGS and the ACK, wait for the request HEADERS on stream 1, serve
:status 200 plus the body. --mode push injects a PUSH_PROMISE before the
response head -- the client advertises ENABLE_PUSH=0, so it must refuse by
name. The wire is hand-rolled on purpose: the codec's whole test surface
is the frame grammar, and an h2 library would be a second implementation
of the thing under test.
"""
import argparse
import socket
import ssl
import sys
import time


def frame(typ, flags, sid, payload):
    return len(payload).to_bytes(3, "big") + bytes([typ, flags]) + \
        sid.to_bytes(4, "big") + payload


def read_exact(s, n):
    b = b""
    while len(b) < n:
        chunk = s.recv(n - len(b))
        if not chunk:
            raise EOFError("short read at %d/%d" % (len(b), n))
        b += chunk
    return b


def read_frame(s):
    hdr = read_exact(s, 9)
    ln = int.from_bytes(hdr[:3], "big")
    return hdr[3], hdr[4], \
        int.from_bytes(hdr[5:9], "big") & 0x7FFFFFFF, read_exact(s, ln)


def serve(s, body, push):
    preface = read_exact(s, 24)
    if preface != b"PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n":
        raise ValueError("bad client preface: %r" % preface)
    typ, flags, sid, payload = read_frame(s)
    if typ != 0x4 or sid != 0:
        raise ValueError("first client frame not SETTINGS: %d" % typ)
    s.sendall(frame(0x4, 0, 0, b""))   # server SETTINGS (defaults)
    s.sendall(frame(0x4, 0x1, 0, b""))  # ACK the client's SETTINGS
    while True:
        typ, flags, sid, payload = read_frame(s)
        if typ == 0x1 and sid == 1:
            break  # request HEADERS complete (END_STREAM or not)
    if push:
        # Promised stream 2 with a tiny literal block; the refusal lands
        # on the frame type alone, but a well-formed payload keeps the
        # leg honest.
        s.sendall(frame(0x5, 0x4, 1, (2).to_bytes(4, "big") +
                        b"\x82\x87\x01\x01a\x04\x01/"))
    s.sendall(frame(0x1, 0x4, 1, b"\x88"))        # HEADERS END_HEADERS
    s.sendall(frame(0x0, 0x1, 1, body.encode()))  # DATA END_STREAM


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--port", type=int, required=True)
    ap.add_argument("--cert", required=True, help="PEM chain, leaf first")
    ap.add_argument("--key", required=True)
    ap.add_argument("--body", default="go-fetch-h2-ok\n")
    ap.add_argument("--mode", choices=("ok", "push"), default="ok")
    ap.add_argument("--accept", type=int, default=1)
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--timeout", type=float, default=30.0,
                    help="hard deadline, so a lost peer cannot hang a runner")
    a = ap.parse_args()

    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.minimum_version = ssl.TLSVersion.TLSv1_3
    ctx.maximum_version = ssl.TLSVersion.TLSv1_3
    ctx.set_alpn_protocols(["h2"])
    try:
        ctx.load_cert_chain(a.cert, a.key)
    except Exception as e:
        sys.stderr.write("h2responder: cannot load %s: %s\n" % (a.cert, e))
        return 2

    ls = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    ls.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    ls.bind((a.host, a.port))
    ls.listen(8)
    sys.stdout.write("h2responder: listening on %s:%d mode=%s\n"
                     % (a.host, a.port, a.mode))
    sys.stdout.flush()

    attempts = 0
    deadline = time.monotonic() + a.timeout
    while attempts < a.accept:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            break
        ls.settimeout(remaining)
        attempts += 1
        try:
            raw, _ = ls.accept()
        except socket.timeout:
            break
        try:
            with ctx.wrap_socket(raw, server_side=True) as s:
                sys.stdout.write("h2responder: alpn=%s\n" % s.selected_alpn_protocol())
                sys.stdout.flush()
                serve(s, a.body, a.mode == "push")
                sys.stdout.write("h2responder: served\n")
                sys.stdout.flush()
        except (ssl.SSLError, OSError, EOFError, ValueError) as e:
            sys.stdout.write("h2responder: refused: %s\n" % e)
            sys.stdout.flush()
        finally:
            try:
                raw.close()
            except OSError:
                pass
    ls.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
EOF

vgate_setup_python <<'PY'
import os, shutil, subprocess, sys, time

run = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(run, "share")
os.makedirs(share, exist_ok=True)

gofetch = os.path.join(".build", "go", "GOFETCH-GATE.ELF")
if not os.path.exists(gofetch):
    sys.exit("GOFETCH.ELF missing (expected " + gofetch + ") - build it first: "
             "bash tools/go/build-web.sh fetch GOFETCH --gate")
shutil.copy(gofetch, os.path.join(share, "GOFETCH.ELF"))
# M71k (#1570): the Zig TLS helper is deleted outright, so there is no
# FETCHS.BIN left to un-stage. Nothing was ever staged for the Go apps.
print("go-fetch-https: staged GOFETCH.ELF (%d); no Zig TLS helper exists" %
      os.path.getsize(os.path.join(share, "GOFETCH.ELF")))

for port in (24550, 24551, 24552, 24553, 24554, 24555, 24556):
    subprocess.run([sys.executable,
                    os.path.join("tools", "lib", "port_guard.py"),
                    str(port)],
                   check=True)
time.sleep(0.2)

cfx = os.path.join("user", "src", "lib", "tls", "vectors", "fx")
chain = os.path.join(cfx, "chain-ec.pem")
key = os.path.join(cfx, "leaf-ec.key")
leaf = os.path.join(cfx, "leaf-ec.pem")
if not os.path.exists(chain):
    sys.exit("pinned fixture chain missing at %s" % chain)

fx = os.path.join(run, "fx")
os.makedirs(fx, exist_ok=True)
csr = os.path.join(fx, "expired.csr")
expired = os.path.join(fx, "expired.pem")
expired_chain = os.path.join(fx, "expired-chain.pem")
inter_pem = os.path.join(fx, "inter.pem")
inter_key = os.path.join(fx, "inter.key")
shutil.copy(os.path.join(cfx, "inter.pem"), inter_pem)
shutil.copy(os.path.join(cfx, "inter.key"), inter_key)
subprocess.check_call([
    "openssl", "req", "-new", "-key", key,
    "-subj", "/CN=leaf.example.com",
    "-addext", "subjectAltName=DNS:leaf.example.com",
    "-out", csr,
])
subprocess.check_call([
    "openssl", "x509", "-req", "-in", csr,
    "-CA", inter_pem, "-CAkey", inter_key,
    "-CAserial", os.path.join(fx, "inter.srl"),
    "-CAcreateserial", "-out", expired,
    "-not_before", "20200101000000Z", "-not_after", "20200102000000Z",
    "-copy_extensions", "copy",
])
with open(expired_chain, "wb") as out:
    out.write(open(expired, "rb").read())
    out.write(open(inter_pem, "rb").read())

responder = os.path.join("user", "src", "lib", "tls", "vectors", "tlsresponder.py")

def start(port, cert, body, accept, logname):
    log = open(os.path.join(run, logname), "wb")
    cmd = [
        sys.executable, responder,
        "--host", "127.0.0.1", "--port", str(port),
        "--cert", cert, "--key", key,
        "--body", body, "--accept", str(accept), "--timeout", "1800",
    ]
    proc = subprocess.Popen(cmd, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    print("go-fetch-https: responder pid=%d on 127.0.0.1:%d cert=%s" % (proc.pid, port, cert))

# Accept counts cover vgate_repeat (K=3) plus slack: a responder that
# exhausts its accept count mid-gate refuses the next boot's dial.
start(24550, chain, "go-fetch-https-ok\n", 8, "tls-ok.log")
start(24552, expired_chain, "should-not-see-expired\n", 4, "tls-expired.log")
start(24553, leaf, "should-not-see-chain\n", 4, "tls-chain.log")

# M212a legs. 24554/24556 are the h2 responders (ok / push); 24555 is a
# plain tlsresponder with no ALPN configured -- the --http2 fetch must
# negotiate nothing and fall back to HTTP/1.1 on the wire it already had.
h2resp = os.path.join(run, "h2responder.py")

def start_h2(port, mode, body, logname):
    log = open(os.path.join(run, logname), "wb")
    cmd = [
        sys.executable, h2resp,
        "--host", "127.0.0.1", "--port", str(port),
        "--cert", chain, "--key", key,
        "--mode", mode, "--body", body, "--accept", "6", "--timeout", "1800",
    ]
    proc = subprocess.Popen(cmd, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    print("go-fetch-https: h2 responder pid=%d on 127.0.0.1:%d mode=%s" % (proc.pid, port, mode))

start_h2(24554, "ok", "go-fetch-h2-ok\n", "h2-ok.log")
start_h2(24556, "push", "should-not-see-push\n", "h2-push.log")
start(24555, chain, "h2-alpn-none-h1-ok\n", 6, "tls-alpnless.log")
time.sleep(0.3)
PY

# Run 01 -- happy path: in-process GET, fixture body, no FETCHS.BIN.
vgate_run 01 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap01.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:24550:relay --net-tcp-respond-relay 127.0.0.1:24550 \
    --script '$RUN_DIR/script-ok.txt' --script-expect 'gofetch: ready' --timeout 180

vgate_assert 01 serial-contains 'gofetch: dial 10.0.0.2 24550 leaf.example.com'
vgate_assert 01 serial-contains 'gofetch: handshake ok'
vgate_assert 01 serial-contains 'gofetch: request sent'
vgate_assert 01 serial-contains 'go-fetch-https-ok'
vgate_assert 01 serial-contains 'gofetch: body complete'
vgate_assert 01 serial-contains 'gofetch: ready'
vgate_assert 01 serial-absent 'FETCHS.BIN'
vgate_assert 01 serial-absent 'gofetch: helper'
vgate_assert 01 serial-absent 'gofetch: error'
vgate_assert 01 serial-absent 'GET / HTTP'
vgate_assert 01 serial-absent 'ETIMEDOUT'
vgate_assert 01 serial-absent '[EXC] parking:'

# Run 02 -- wrong name: SNI/verify wrong.example.com against the fixture leaf.
vgate_run 02 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap02.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:24550:relay --net-tcp-respond-relay 127.0.0.1:24550 \
    --script '$RUN_DIR/script-name.txt' --script-expect 'gofetch: fail-closed name' --timeout 180

vgate_assert 02 serial-contains 'gofetch: dial 10.0.0.2 24550 wrong.example.com'
vgate_assert 02 serial-contains 'gofetch: handshake error '
vgate_assert 02 serial-contains 'hostname_mismatch'
vgate_assert 02 serial-contains 'gofetch: fail-closed name'
vgate_assert 02 serial-contains 'gofetch: ready'
vgate_assert 02 serial-absent 'gofetch: handshake ok'
vgate_assert 02 serial-absent 'gofetch: body complete'
vgate_assert 02 serial-absent 'ETIMEDOUT'
vgate_assert 02 serial-absent 'FETCHS.BIN'
vgate_assert 02 serial-absent '[EXC] parking:'

# Run 03 -- expired leaf (valid path to the vendored root, dates in 2020).
vgate_run 03 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap03.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:24552:relay --net-tcp-respond-relay 127.0.0.1:24552 \
    --script '$RUN_DIR/script-expired.txt' --script-expect 'gofetch: fail-closed expired' --timeout 180

vgate_assert 03 serial-contains 'gofetch: handshake error '
vgate_assert 03 serial-contains 'certificate chain validation failed: expired'
vgate_assert 03 serial-contains 'gofetch: fail-closed expired'
vgate_assert 03 serial-contains 'gofetch: ready'
vgate_assert 03 serial-absent 'gofetch: handshake ok'
vgate_assert 03 serial-absent 'should-not-see-expired'
vgate_assert 03 serial-absent 'ETIMEDOUT'
vgate_assert 03 serial-absent 'FETCHS.BIN'
vgate_assert 03 serial-absent '[EXC] parking:'

# Run 04 -- bad chain: leaf only, no intermediate, no path to the vendored root.
vgate_run 04 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap04.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:24553:relay --net-tcp-respond-relay 127.0.0.1:24553 \
    --script '$RUN_DIR/script-chain.txt' --script-expect 'gofetch: fail-closed chain' --timeout 180

vgate_assert 04 serial-contains 'gofetch: handshake error '
vgate_assert 04 serial-contains 'no_path_to_root'
vgate_assert 04 serial-contains 'gofetch: fail-closed chain'
vgate_assert 04 serial-contains 'gofetch: ready'
vgate_assert 04 serial-absent 'gofetch: handshake ok'
vgate_assert 04 serial-absent 'should-not-see-chain'
vgate_assert 04 serial-absent 'ETIMEDOUT'
vgate_assert 04 serial-absent 'FETCHS.BIN'
vgate_assert 04 serial-absent '[EXC] parking:'

# Run 05 -- M212a: --http2 negotiates ALPN h2 and the page bytes come back
# over frames, not the h1 engine.
vgate_run 05 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap05.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:24554:relay --net-tcp-respond-relay 127.0.0.1:24554 \
    --script '$RUN_DIR/script-h2.txt' --script-expect 'gofetch: ready' --timeout 180

vgate_assert 05 serial-contains 'gofetch: dial 10.0.0.2 24554 leaf.example.com'
vgate_assert 05 serial-contains 'gofetch: handshake ok'
vgate_assert 05 serial-contains 'gofetch: alpn h2'
vgate_assert 05 serial-contains 'gofetch: request sent'
vgate_assert 05 serial-contains 'go-fetch-h2-ok'
vgate_assert 05 serial-contains 'gofetch: body complete'
vgate_assert 05 serial-contains 'gofetch: ready'
vgate_assert 05 serial-absent 'gofetch: error'
vgate_assert 05 serial-absent 'GET / HTTP'
vgate_assert 05 serial-absent 'ETIMEDOUT'
vgate_assert 05 serial-absent '[EXC] parking:'

# Run 06 -- M212a: the peer answers h2 but pushes anyway; ENABLE_PUSH=0 was
# advertised, so PUSH_PROMISE names its refusal and no body is consumed.
vgate_run 06 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap06.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:24556:relay --net-tcp-respond-relay 127.0.0.1:24556 \
    --script '$RUN_DIR/script-push.txt' --script-expect 'gofetch: ready' --timeout 180

vgate_assert 06 serial-contains 'gofetch: handshake ok'
vgate_assert 06 serial-contains 'gofetch: alpn h2'
vgate_assert 06 serial-contains 'gofetch: error h2 h2: push_promise refused'
vgate_assert 06 serial-contains 'gofetch: ready'
vgate_assert 06 serial-absent 'gofetch: body complete'
vgate_assert 06 serial-absent 'should-not-see-push'
vgate_assert 06 serial-absent 'ETIMEDOUT'
vgate_assert 06 serial-absent '[EXC] parking:'

# Run 07 -- M212a: an ALPN-less peer (plain tlsresponder) negotiates
# nothing; the --http2 fetch falls back to the HTTP/1.1 engine and still
# returns page bytes.
vgate_run 07 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap07.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:24555:relay --net-tcp-respond-relay 127.0.0.1:24555 \
    --script '$RUN_DIR/script-alpnless.txt' --script-expect 'gofetch: ready' --timeout 180

vgate_assert 07 serial-contains 'gofetch: handshake ok'
vgate_assert 07 serial-contains 'gofetch: alpn none'
vgate_assert 07 serial-contains 'h2-alpn-none-h1-ok'
vgate_assert 07 serial-contains 'gofetch: body complete'
vgate_assert 07 serial-contains 'gofetch: ready'
vgate_assert 07 serial-absent 'gofetch: error'
vgate_assert 07 serial-absent 'ETIMEDOUT'
vgate_assert 07 serial-absent '[EXC] parking:'

# Run 08 -- M212a: --http2 on a cleartext URL never reaches a socket --
# h2c (the HTTP/1.1 Upgrade dance) is refused by name before any dial.
vgate_run 08 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap08.bin' --net-arp-respond 10.0.0.2 \
    --script '$RUN_DIR/script-h2c.txt' --script-expect 'gofetch: ready' --timeout 180

vgate_assert 08 serial-contains 'gofetch: url http://10.0.0.2/'
vgate_assert 08 serial-contains 'gofetch: refuse h2c'
vgate_assert 08 serial-contains 'gofetch: ready'
vgate_assert 08 serial-absent 'gofetch: dial'
vgate_assert 08 serial-absent 'gofetch: body complete'
vgate_assert 08 serial-absent '[EXC] parking:'
