# live-secrets.spec -- M50 TS5 class-B gate (issue #1139, ADR 0024 D8/D10),
# retargeted to GOSH by M68b (#1450).
#
# The secret store live, on one share with a host-seeded `SECRETS.TXT`
# (`#v1`, `key<TAB>uid<TAB>value` plus the M97g #2083 optional
# `<TAB>apps` allowlist): (1) the EL1h monitor's `secrets` command lists
# the key NAMEs only; (2) the EL0 shell's `secrets` builtin (through
# `sys_secret_get`, slot 70) lists only the names the caller may read —
# the unbound `netkey`, its own `goshkey`, but NOT `hostkey` bound to
# GOSSHD.ELF; (3) `SECRETS.TXT` is denied at BOTH seams — the monitor's
# `vf cat` and the EL0 file ABI `cat < SECRETS.TXT` — because TS5 registers
# it secret-class BY CONSTRUCTION, not only via a hand-seeded OWNERS.TXT;
# (4) the known secret VALUEs never appear anywhere in the serial capture
# while the NAMEs do. Boot default unchanged: no secret is read at boot on
# a default share.

vgate_name live-secrets "#1139 TS5: secrets store — names listable, SECRETS.TXT denied at both seams, value never logged"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

# The EL1h monitor session: prove the direct-consumer seam first (`vf cat`
# is denied even for uid_system + CAP_FS_ANY), then the monitor `secrets`
# name listing, then hand the console to GOSH (serial front-end) for the EL0
# file-ABI + sys_secret_get session.
vgate_file script.txt <<'EOF'
secrets
vf cat SECRETS.TXT
exec GOSH.ELF serial
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
PY

vgate_setup_python <<'PY'
import os
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
# Host-seeded secret store. A 64-char value (a 32-byte Ed25519 seed in hex)
# is the canonical max; a short marker value proves serial-absence loudly.
# M97g (#2083): `goshkey` is app-bound to GOSH.ELF (visible to it because
# OWNERS.TXT pins the image uid_system); `hostkey` is bound to GOSSHD.ELF —
# a name GOSH.ELF does not carry, so a plain app's slot-70 read must not
# return it (the audit's cross-app disclosure).
open(os.path.join(share, "SECRETS.TXT"), "w").write(
    "#v1\n"
    "netkey\t1000\tTS5-TOPSECRET-VALUE\n"
    "goshkey\t1000\tGOSH-BOUND-SECRET\tGOSH.ELF\n"
    "hostkey\t1000\tHOST-BOUND-SECRET\tGOSSHD.ELF\n"
)
# The app binding is only honest while the metadata is operator-owned:
# OWNERS.TXT pins GOSH.ELF to uid_system (EL0 cannot author that row, and
# OWNERS.TXT itself is self-protected by construction).
open(os.path.join(share, "OWNERS.TXT"), "w").write(
    "#v1\n"
    "GOSH.ELF\t644\t0\t-\n"
)
# The EL0 shell session; CR (0x0d) submits on this seam.
open(os.path.join(rd, "edit.bin"), "wb").write(
    b"secrets\r"
    b"cat < SECRETS.TXT\r"
    b"echo ts5-done\r"
)
PY

vgate_run 01 -- --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/edit.bin' \
    --script2-after 'gosh: attached' \
    --script-expect 'ts5-done' \
    --script-expect-tail 16 \
    --timeout 90

# The key NAME is listable at BOTH seams: the EL1h monitor `secrets` command
# and the EL0 shell `secrets` builtin (via sys_secret_get).
vgate_assert 01 serial-contains 'secrets:'
vgate_assert 01 serial-count '  netkey' 2
# M97g (#2083) app binding: `goshkey` (bound to GOSH.ELF, image pinned
# uid_system) lists at BOTH seams; `hostkey` (bound to GOSSHD.ELF) lists at
# the monitor but is refused to GOSH — a plain app gets only its own keys.
vgate_assert 01 serial-count '  goshkey' 2
vgate_assert 01 serial-count '  hostkey' 1
# Direct-consumer seam: the monitor's own secret read is denied (the class
# holds by construction, so even with no OWNERS.TXT entry the file is denied).
vgate_assert 01 serial-contains 'vf cat: SECRETS.TXT: permission denied'
# EL0 file ABI: the only in-guest reader is sys_secret_get; cat is denied.
vgate_assert 01 serial-contains 'gosh: cannot open SECRETS.TXT: EACCES'
vgate_assert 01 serial-contains 'ts5-done'
# Never-logged contract: the known VALUE never reaches the serial transcript,
# while its NAME (above) does. Names travel; values do not.
vgate_assert 01 serial-absent 'TS5-TOPSECRET-VALUE'
vgate_assert 01 serial-absent 'GOSH-BOUND-SECRET'
vgate_assert 01 serial-absent 'HOST-BOUND-SECRET'
vgate_assert 01 serial-absent '\[EXC\]'
vgate_assert 01 serial-absent '[EXC] parking:'

# The host share still holds the seeded value untouched (no sys_secret_set
# exists; the guest never writes SECRETS.TXT).
vgate_assert 01 python <<'PY'
import os, sys
p = os.path.join(os.environ["VG_SHARE"], "SECRETS.TXT")
try:
    body = open(p, errors="replace").read()
except OSError:
    sys.exit("FAIL: SECRETS.TXT missing from the host share")
if "netkey\t1000\tTS5-TOPSECRET-VALUE" not in body:
    sys.exit("FAIL: seeded SECRETS.TXT entry was modified: " + repr(body))
print("secrets host inspection ok")
PY