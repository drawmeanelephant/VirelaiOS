# live-remote-auth2.spec -- M50 TS4 class-B gate (issue #1138, ADR 0024 D6),
# retargeted to GOSH by M68b (#1450).
#
# The delegated challenge-response guest net front-end. A host-seeded
# SECRETS.TXT holds the `net-hmac` credential; `GOSH.ELF net 2323` reads it
# from the TS5 store (never argv) and the kernel pump frames
# `VIRELAIOS-AUTH/1 hmac-sha256 <hex-challenge>` on accept. Run 01 answers
# with the WRONG key -> `auth failed` + reset, no byte reaches the shell.
# Run 02 answers with the RIGHT key -> the shell drives the host client.
# Run 03 REPLAYS a captured handshake: a MAC that was valid for a previous
# challenge is rejected by the fresh one. Run 04 proves the explicit
# `open` mode still accepts (the documented insecure posture). Boot default
# unchanged: nothing listens unless a process asks.
#
# HOST PREREQ: bash tools/go/build-gosh.sh -> .build/go/GOSH.ELF

vgate_name live-remote-auth2 "#1138 TS4: HMAC challenge-response — wrong MAC rejected, right MAC drives the shell, captured handshake replayed, open still accepts"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script.txt <<'EOF'
net ip 10.0.0.1
exec GOSH.ELF net 2323
EOF

vgate_file script-open.txt <<'EOF'
net ip 10.0.0.1
exec GOSH.ELF net 2323 open
EOF

vgate_file help.txt <<'EOF'
help
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
src = os.path.join(".build", "go", "GOSH.ELF")
if not os.path.exists(src):
    sys.exit("GOSH.ELF missing (expected " + src + ") - build it first: "
             "bash tools/go/build-gosh.sh")
shutil.copy(src, os.path.join(share, "GOSH.ELF"))
print("staged GOSH.ELF (%d bytes) into share" % os.path.getsize(src))
# The TS5 store: net-hmac = the HMAC-SHA256 pre-shared key (byte-for-byte).
# M97g (#2083): the key is app-bound to GOSH.ELF, and OWNERS.TXT pins the
# image uid_system — the binding is operator-provisioned, not name-forgeable.
open(os.path.join(share, "SECRETS.TXT"), "w").write(
    "#v1\n"
    "net-hmac\t1000\ts3cret\tGOSH.ELF\n"
)
open(os.path.join(share, "OWNERS.TXT"), "w").write(
    "#v1\n"
    "GOSH.ELF\t644\t0\t-\n"
)
PY

# Run 01: a client with the wrong key. The kernel rejects on the verdict.
vgate_run 01 -- --net '$RUN_DIR/cap1.bin' \
    --script '$RUN_DIR/script.txt' \
    --net-tcp-connect '10.0.0.1:2323:$RUN_DIR/help.txt' \
    --net-tcp-connect-secret wrong \
    --net-tcp-connect-after 'gosh: remote auth=hmac-sha256' \
    --net-tcp-connect-close-after 'auth failed' \
    --script-expect 'tty net: auth failed' \
    --timeout 120

# Run 02: the right key drives the shell (`help` -> `builtins:`).
vgate_run 02 -- --net '$RUN_DIR/cap2.bin' \
    --script '$RUN_DIR/script.txt' \
    --net-tcp-connect '10.0.0.1:2323:$RUN_DIR/help.txt' \
    --net-tcp-connect-secret s3cret \
    --net-tcp-connect-after 'gosh: remote auth=hmac-sha256' \
    --net-tcp-connect-close-after 'builtins:' \
    --script-expect 'tty net: detached' \
    --timeout 120

# Run 03: a stale fixed MAC, NOT a capture-then-replay within the boot. The
# MAC below was valid for the fixed challenge 000102...1f under `s3cret`;
# the fresh challenge makes it wrong. The replay property proper (two
# accepts mint different challenges, so a captured handshake cannot answer
# the fresh one) is the class-A freshness test; this run proves the
# class-B framing rejects a MAC not valid for the current challenge.
vgate_run 03 -- --net '$RUN_DIR/cap3.bin' \
    --script '$RUN_DIR/script.txt' \
    --net-tcp-connect '10.0.0.1:2323:$RUN_DIR/help.txt' \
    --net-tcp-connect-mac '65bcb791094a86de218b38905cb939c6554ae9eb98238c48cd6835a8883f3ecb' \
    --net-tcp-connect-after 'gosh: remote auth=hmac-sha256' \
    --net-tcp-connect-close-after 'auth failed' \
    --script-expect 'tty net: auth failed' \
    --timeout 120

# Run 04: explicit insecure mode still accepts (with a credential present).
vgate_run 04 -- --net '$RUN_DIR/cap4.bin' \
    --script '$RUN_DIR/script-open.txt' \
    --net-tcp-connect '10.0.0.1:2323:$RUN_DIR/help.txt' \
    --net-tcp-connect-after 'gosh: remote auth=open' \
    --net-tcp-connect-close-after 'builtins:' \
    --script-expect 'tty net: detached' \
    --timeout 120

vgate_assert 01 serial-contains 'gosh: remote auth=hmac-sha256'
vgate_assert 01 serial-contains 'tty net: auth failed'
vgate_assert 01 output-contains 'auth failed'
vgate_assert 01 output-contains 'answered the challenge'
vgate_assert 01 serial-absent 'builtins:'
vgate_assert 01 serial-absent '[EXC] parking:'

vgate_assert 02 serial-contains 'gosh: remote auth=hmac-sha256'
vgate_assert 02 output-contains 'answered the challenge'
vgate_assert 02 output-contains 'builtins:'
vgate_assert 02 serial-contains 'tty net: detached'
vgate_assert 02 serial-absent 'auth failed'
vgate_assert 02 serial-absent '[EXC] parking:'

vgate_assert 03 serial-contains 'tty net: auth failed'
vgate_assert 03 output-contains 'recorded replay MAC'
vgate_assert 03 output-contains 'auth failed'
vgate_assert 03 serial-absent 'builtins:'
vgate_assert 03 serial-absent '[EXC] parking:'

vgate_assert 04 serial-contains 'gosh: remote auth=open'
vgate_assert 04 output-contains 'builtins:'
vgate_assert 04 serial-contains 'tty net: detached'
vgate_assert 04 serial-absent '[EXC] parking:'
