# live-ssh-server.spec -- M70g G1 (#1491, ADR 0025) class-B positive.
#
# The in-guest GOSSHD.ELF listens on 2222 (slot 30 ip-0). The runner's
# `--net-tcp-connect …:ssh` client speaks the ADR 0025 D2 profile against
# it: curve25519-sha256 KEX, ssh-ed25519 host key from TS5
# `ssh-host-ed25519` (RFC 8032 TEST 1), publickey vs SSH/AUTHORIZED_KEYS
# (TEST 2), one session exec piped to GOSH.ELF -c. TX is paced one
# encrypted <=192-byte segment per guest ACK. Hermetic via --net.
# Class B (Apple silicon VZ).
#
# HOST PREREQ:
#   bash tools/go/build-sshd.sh -> .build/go/GOSSHD.ELF
#   bash tools/go/build-gosh.sh -> .build/go/GOSH.ELF

vgate_name live-ssh-server "M70g G1: GOSSHD.ELF KEX+publickey+GOSH exec against the runner's SSH-2 client"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script-1.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOSSHD.ELF
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
run = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(run, "share")
for name, cmd in (("GOSSHD.ELF", "bash tools/go/build-sshd.sh"),
                  ("GOSH.ELF", "bash tools/go/build-gosh.sh")):
    src = os.path.join(".build", "go", name)
    if not os.path.exists(src):
        sys.exit(name + " missing (expected " + src + ") - build it first: " + cmd)
    shutil.copy(src, os.path.join(share, name))
os.makedirs(os.path.join(share, "SSH"), exist_ok=True)
# RFC 8032 §7.1 TEST 1 seed: the guest host key (uid_user), app-bound to
# GOSSHD.ELF (M97g #2083); OWNERS.TXT pins the image uid_system.
open(os.path.join(share, "SECRETS.TXT"), "w").write(
    "#v1\n"
    "ssh-host-ed25519\t1000\t9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60\tGOSSHD.ELF\n"
)
open(os.path.join(share, "OWNERS.TXT"), "w").write(
    "#v1\n"
    "GOSSHD.ELF\t644\t0\t-\n"
    "GOSH.ELF\t644\t0\t-\n"
)
# RFC 8032 §7.1 TEST 2 public key: the runner client's identity.
open(os.path.join(share, "SSH", "AUTHORIZED_KEYS"), "w").write(
    "#v1\n"
    "alice\tssh-ed25519\t3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c\n"
)
print("staged GOSSHD.ELF (%d bytes) GOSH.ELF (%d bytes)"
      % (os.path.getsize(os.path.join(share, "GOSSHD.ELF")),
         os.path.getsize(os.path.join(share, "GOSH.ELF"))))
PY

vgate_run 01 -- --net '$RUN_DIR/cap.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-connect 10.0.0.1:2222:ssh \
    --net-tcp-connect-after 'sshd: listen' \
    --script '$RUN_DIR/script-1.txt' \
    --script-expect 'sshd: bye' \
    --timeout 120

vgate_assert 01 output-contains 'NET-TCP-CONNECT-SSH: cipher self-check ok'
vgate_assert 01 output-contains 'NET-TCP-CONNECT-SSH: ENABLED'
vgate_assert 01 output-contains 'SSH-CLI: version received: SSH-2.0-VirelaiOS_1.0'
vgate_assert 01 output-contains 'SSH-CLI: kexinit received (suite negotiated)'
vgate_assert 01 output-contains 'SSH-CLI: ecdh reply accepted; host key pinned'
vgate_assert 01 output-contains 'SSH-CLI: publickey accepted user=alice key=ssh-ed25519'
vgate_assert 01 output-contains 'SSH-CLI: exec echo VIRELAI-SSH-SERVER-OK-ARGV2'
vgate_assert 01 output-contains 'SSH-CLI: stdout VIRELAI-SSH-SERVER-OK-ARGV2'
vgate_assert 01 output-contains 'SSH-CLI: exit-status=0'
vgate_assert 01 output-contains 'NET-TCP-CONNECT-SSH: sent'
vgate_assert 01 serial-contains 'sshd: listen 2222'
vgate_assert 01 serial-contains 'sshd: accepted'
vgate_assert 01 serial-contains 'sshd: kex-ok'
vgate_assert 01 serial-contains 'sshd: auth-ok user=alice key=ssh-ed25519'
vgate_assert 01 serial-contains 'sshd: exec echo VIRELAI-SSH-SERVER-OK-ARGV2'
vgate_assert 01 serial-contains 'sshd: gosh rc=0'
vgate_assert 01 serial-contains 'sshd: bye'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent '9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60'
vgate_assert 01 serial-absent '4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb'
vgate_assert 01 python <<'PY'
import os, sys
run = os.environ["RUN_DIR"]
out = open(os.path.join(run, "run-01.out"), errors="replace").read()
for seed in (
    "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60",
    "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb",
):
    if seed in out:
        sys.exit("FAIL: a private seed appears in the runner stdout")
if out.count("NET-TCP-CONNECT-SSH: sent ") < 3:
    sys.exit("FAIL: pacing produced fewer than 3 segments")
print("ssh server runner log ok")
PY
