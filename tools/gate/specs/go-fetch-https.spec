# go-fetch-https.spec -- M67c #1448: pin in-process HTTPS (M67b) and
# demote FETCHS.BIN to an unreferenced fallback (deleted outright in M71k #1570).
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

vgate_name go-fetch-https "M67c #1448: GOFETCH.ELF HTTPS in-process vs runner TLS responder; negatives fail closed; no FETCHS.BIN"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

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

for port in (24550, 24551, 24552, 24553):
    subprocess.run(["sh", "-c", "lsof -ti tcp:%d | xargs kill -9" % port],
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
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

start(24550, chain, "go-fetch-https-ok\n", 2, "tls-ok.log")
start(24552, expired_chain, "should-not-see-expired\n", 1, "tls-expired.log")
start(24553, leaf, "should-not-see-chain\n", 1, "tls-chain.log")
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
