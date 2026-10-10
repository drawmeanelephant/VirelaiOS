# live-ssh-endpoint.spec -- M51 SSH5 (#1172, ADR 0025 D8) class-B positive,
# retargeted onto GOSSH.ELF by M71j (#1569).
#
# The guest client GOSSH.ELF dials the runner-hosted minimal SSH-2 responder
# (`--net-tcp-respond 10.0.0.2:2222:ssh`): curve25519-sha256 KEX against a
# pinned ssh-ed25519 host key (RFC 8032 TEST 1), publickey auth with the
# TS5 `ssh-user-ed25519` seed (RFC 8032 TEST 2), one session channel, one
# fixed exec. Asserts the guest serial markers AND the responder's own
# KEX/auth/exec log. TX is paced one encrypted <=192-byte segment per guest
# ACK (the guest kernel RX is a single 192-byte slot). Hermetic via --net.
# Class B (Apple silicon VZ).
#
# HOST PREREQ:
#   bash tools/go/build-ssh.sh -> .build/go/GOSSH.ELF

vgate_name live-ssh-endpoint "M71j: GOSSH.ELF KEX+publickey+exec against the runner's pinned SSH-2 responder"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script-1.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOSSH.ELF alice@10.0.0.2:2222 VIRELAI-GATE-EXEC
echo ssh-launched
EOF

vgate_file script-2.txt <<'EOF'
procs
echo ssh-endpoint-done
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
run = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(run, "share")
src = os.path.join(".build", "go", "GOSSH.ELF")
if not os.path.exists(src):
    sys.exit("GOSSH.ELF missing (expected " + src + ") - build it first: bash tools/go/build-ssh.sh")
shutil.copy(src, os.path.join(share, "GOSSH.ELF"))
os.makedirs(os.path.join(share, "SSH"), exist_ok=True)
# RFC 8032 §7.1 TEST 2 seed: the client identity (the responder pins TEST 2).
open(os.path.join(share, "SECRETS.TXT"), "w").write(
    "#v1\n"
    "ssh-user-ed25519\t1000\t4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb\tGOSSH.ELF\n"
)
# M97g (#2083): the app binding needs GOSSH.ELF pinned uid_system.
open(os.path.join(share, "OWNERS.TXT"), "w").write(
    "#v1\n"
    "GOSSH.ELF\t644\t0\t-\n"
)
# RFC 8032 §7.1 TEST 1 public key: the responder's pinned host key.
open(os.path.join(share, "SSH", "KNOWN_HOSTS"), "w").write(
    "#v1\n"
    "10.0.0.2\t2222\tssh-ed25519\td75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a\n"
)
print("staged GOSSH.ELF (%d bytes)" % os.path.getsize(os.path.join(share, "GOSSH.ELF")))
PY

vgate_run 01 -- --net '$RUN_DIR/cap.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:2222:ssh \
    --script '$RUN_DIR/script-1.txt' \
    --script2 '$RUN_DIR/script-2.txt' \
    --script2-after 'ssh: bye rc=0' \
    --script-expect 'tasks user-exec reaped' \
    --timeout 120

vgate_assert 01 output-contains 'NET-TCP-SSH: cipher self-check ok'
vgate_assert 01 output-contains 'NET-TCP-SSH: ENABLED'
vgate_assert 01 output-contains 'SSH-SRV: version received: SSH-2.0-VirelaiOS_1.0'
vgate_assert 01 output-contains 'SSH-SRV: kexinit received (suite negotiated)'
vgate_assert 01 output-contains 'SSH-SRV: newkeys received; cipher installed'
vgate_assert 01 output-contains 'SSH-SRV: publickey accepted user=alice key=ssh-ed25519'
vgate_assert 01 output-contains 'SSH-SRV: exec command=VIRELAI-GATE-EXEC'
vgate_assert 01 output-contains 'NET-TCP-SSH: sent'
vgate_assert 01 serial-contains 'ssh: target user=alice host=10.0.0.2 port=2222 mode=exec'
vgate_assert 01 serial-contains 'ssh: connected'
vgate_assert 01 serial-contains 'ssh: kex-ok'
vgate_assert 01 serial-contains 'ssh: pin-ok'
vgate_assert 01 serial-contains 'ssh: auth-ok method=publickey'
vgate_assert 01 serial-contains 'ssh: channel-open remote=42'
vgate_assert 01 serial-contains 'VIRELAI-SSH5-OK'
vgate_assert 01 serial-contains 'ssh: exit-status=0'
vgate_assert 01 serial-contains 'ssh: eof'
vgate_assert 01 serial-contains 'ssh: bye rc=0'
vgate_assert 01 serial-contains 'ssh-endpoint-done'
vgate_assert 01 serial-absent '[EXC] parking:'
# Never-logged: the client seed is read through sys_secret_get and never
# reaches the serial transcript or the runner's stdout.
vgate_assert 01 serial-absent '4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb'
vgate_assert 01 python <<'PY'
import os, sys
run = os.environ["RUN_DIR"]
out = open(os.path.join(run, "run-01.out"), errors="replace").read()
seed = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
if seed in out:
    sys.exit("FAIL: the client seed appears in the runner stdout")
# The responder actually paced (more than one segment) and the full
# encrypted handshake+auth+exec happened.
for marker in ("SSH-SRV: ecdh reply sent", "SSH-SRV: publickey accepted",
               "SSH-SRV: exec command=VIRELAI-GATE-EXEC"):
    if marker not in out:
        sys.exit("FAIL: responder marker missing: " + marker)
if out.count("NET-TCP-SSH: sent ") < 3:
    sys.exit("FAIL: pacing produced fewer than 3 segments")
print("ssh endpoint runner log ok")
PY
