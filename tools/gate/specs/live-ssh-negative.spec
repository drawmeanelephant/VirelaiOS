# live-ssh-negative.spec -- M51 SSH5 (#1172, ADR 0025 D8) class-B negatives
# (three runs, one shared fixture share), retargeted onto GOSSH.ELF by
# M71j (#1569).
#
# 01 unknown host-key pin: the responder presents a DIFFERENT pinned host
#    key (RFC 8032 TEST 3) than the client's KNOWN_HOSTS pin (TEST 1); the
#    guest must refuse after host-key verify and send NO userauth.
# 02 wrong user key: the responder pins TEST 3 as the client key while the
#    TS5 seed is TEST 2; the server rejects the signature (AuthRejected).
# 03 tampered MAC: the responder flips one bit of the first encrypted tag;
#    the guest's AEAD open must fail closed (protocol rc=8).
# All runs: serial-absent '[EXC] parking:'.
#
# HOST PREREQ:
#   bash tools/go/build-ssh.sh -> .build/go/GOSSH.ELF

vgate_name live-ssh-negative "M71j: GOSSH.ELF unknown host pin refused, wrong user key rejected, tampered MAC disconnected"
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
echo ssh-negative-done
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
# The honest client fixtures: TEST 2 seed, TEST 1 host pin. Each run varies
# only the RESPONDER's pinned keys (or the tamper knob), not the share.
open(os.path.join(share, "SECRETS.TXT"), "w").write(
    "#v1\n"
    "ssh-user-ed25519\t1000\t4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb\tGOSSH.ELF\n"
)
# M97g (#2083): the app binding needs GOSSH.ELF pinned uid_system.
open(os.path.join(share, "OWNERS.TXT"), "w").write(
    "#v1\n"
    "GOSSH.ELF\t644\t0\t-\n"
)
open(os.path.join(share, "SSH", "KNOWN_HOSTS"), "w").write(
    "#v1\n"
    "10.0.0.2\t2222\tssh-ed25519\td75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a\n"
)
print("staged GOSSH.ELF (%d bytes)" % os.path.getsize(os.path.join(share, "GOSSH.ELF")))
PY

# 01: the responder's host key is TEST 3 (pin mismatch -> exit 4).
vgate_run 01 -- --net '$RUN_DIR/cap1.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:2222:ssh \
    --net-tcp-respond-ssh-hostkey c5aa8df43f9f837bedb7442f31dcb7b166d38535076f094b85ce3a2e0b4458f7 \
    --script '$RUN_DIR/script-1.txt' \
    --script2 '$RUN_DIR/script-2.txt' \
    --script2-after 'ssh: fail stage=auth rc=4' \
    --script-expect 'tasks user-exec reaped' \
    --timeout 120

# 02: the responder pins TEST 3 as the accepted client key (rejected -> 5).
vgate_run 02 -- --net '$RUN_DIR/cap2.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:2222:ssh \
    --net-tcp-respond-ssh-userkey fc51cd8e6218a1a38da47ed00230f0580816ed13ba3303ac5deb911548908025 \
    --script '$RUN_DIR/script-1.txt' \
    --script2 '$RUN_DIR/script-2.txt' \
    --script2-after 'ssh: fail stage=auth rc=5' \
    --script-expect 'tasks user-exec reaped' \
    --timeout 120

# 03: the responder corrupts the first encrypted tag (AEAD fails -> 8).
vgate_run 03 -- --net '$RUN_DIR/cap3.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:2222:ssh \
    --net-tcp-respond-ssh-tamper-mac \
    --script '$RUN_DIR/script-1.txt' \
    --script2 '$RUN_DIR/script-2.txt' \
    --script2-after 'ssh: fail stage=protocol rc=8' \
    --script-expect 'tasks user-exec reaped' \
    --timeout 120

vgate_assert 01 serial-contains 'ssh: kex-ok'
vgate_assert 01 serial-contains 'ssh: auth-error HostKeyMismatch'
vgate_assert 01 serial-contains 'ssh: fail stage=auth rc=4'
vgate_assert 01 output-contains 'SSH-SRV: ecdh reply sent'
vgate_assert 01 serial-absent 'ssh: pin-ok'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 python <<'PY'
import os, sys
out = open(os.path.join(os.environ["RUN_DIR"], "run-01.out"), errors="replace").read()
if "SSH-SRV: service-accept" in out:
    sys.exit("FAIL: the responder saw a userauth service request from a client with an unknown host pin")
print("unknown pin: no userauth was attempted")
PY

vgate_assert 02 serial-contains 'ssh: kex-ok'
vgate_assert 02 serial-contains 'ssh: auth-error AuthRejected'
vgate_assert 02 serial-contains 'ssh: fail stage=auth rc=5'
vgate_assert 02 output-contains 'SSH-SRV: publickey rejected: key does not match the pinned client key'
vgate_assert 02 serial-absent '[EXC] parking:'

vgate_assert 03 serial-contains 'ssh: kex-ok'
vgate_assert 03 serial-contains 'ssh: fail stage=protocol rc=8'
vgate_assert 03 output-contains 'SSH-SRV: tamper: flipped the first encrypted packet tag (negative mode)'
vgate_assert 03 serial-absent 'ssh: auth-ok'
vgate_assert 03 serial-absent '[EXC] parking:'
