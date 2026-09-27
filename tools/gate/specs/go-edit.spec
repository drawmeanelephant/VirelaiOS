# go-edit.spec -- M58b (issue #1306) class-B gate: a Go editor opens a share
# file, edits the buffer, saves it, and closes, full-viewport inside Zig TABWM.
#
# user/go/edit is a tabapp client: init -> declare (kind-8 WM_RPC) -> read the
# seeded /host/EDIT/SEED.TXT -> accept injected keystrokes into the buffer
# (Win_DOWN arg1 is the Unicode codepoint) -> Ctrl-S writes the buffer back ->
# WIN_CLOSE exits. Zig EDIT.BIN is gone (M60 / #1297) and so is the Zig notepad
# (M66c / #1485); this gate pins the Go editor that replaced EDIT.BIN. Kernel untouched.
#
# The load-bearing evidence is NOT only the app's own markers: the run's last
# assert reads the share file back ON THE HOST and requires it to equal the
# seed plus the typed characters, so a save that reported success without
# writing the bytes cannot pass.
#
# M81d (#1764) adds the advisory write lease. Run 01 takes the lease, saves
# under it, and releases it at close. Run 02 is the SECOND WRITER: a foreign
# live record (stamped so no run outlives it) is on the path, the open-time
# acquire refuses with `lease held rc=-11`, Ctrl-S refuses with the
# save-error line carrying -11, and the host assert proves the bytes were
# NOT written. Run 03 is the takeover: a foreign STALE record (ts=1) is
# taken over at the open-time acquire, the save lands, and the record reads
# back as GOEDIT's own (pid= present, the foreign token gone). Runs 02/03
# drive /host/EDIT/LEASE.TXT (its own seed) so run 01's saved bytes are
# never on their paths; `--lease-fixture=` is the app's named fixture verb
# (the GOSELF --panic-receipt-fixture precedent).
#
# HOST PREREQUISITE (fails the gate honestly when missing):
#   bash tools/go/build-goedit.sh   ->  .build/go/GOEDIT.ELF
#
# exec-order: assert-proven -- the run ends on `rx-goedit-ok`, which only the
# script prints, and the stage gate that forwards the close waits on the app's
# own `goedit: saved `; a program that never ran cannot pass.

vgate_name go-edit "issue #1306 M58b: a Go editor opens, edits and saves a share file in Zig TABWM on VZ"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script.txt <<'EOF'
tabwm
tabwm start
EOF

vgate_file script2.txt <<'EOF'
exec GOEDIT.ELF /host/EDIT/SEED.TXT
EOF

# The close is driven from the harness after the app's own `goedit: saved `
# (the stage gate), so the bytes are on the share before the window closes.
vgate_file script3.txt <<'EOF'
dui close 2
echo rx-goedit-ok
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
src = os.path.join(".build", "go", "GOEDIT.ELF")
if not os.path.exists(src):
    sys.exit("GOEDIT.ELF missing (expected " + src + ") - build it first: "
             "bash tools/go/build-goedit.sh")
shutil.copy(src, os.path.join(share, "GOEDIT.ELF"))
ed = os.path.join(share, "EDIT")
os.makedirs(ed, exist_ok=True)
seed = os.path.join(ed, "SEED.TXT")
with open(seed, "wb") as f:
    f.write(b"seed-line\n")
# M81d runs 02/03: the lease drill's own target, seeded separately so run
# 01's saved SEED.TXT bytes are never on its path.
lease_seed = os.path.join(ed, "LEASE.TXT")
with open(lease_seed, "wb") as f:
    f.write(b"lease-seed\n")
print("staged GOEDIT.ELF into share (%d bytes) and %s (%d bytes)" %
      (os.path.getsize(os.path.join(share, "GOEDIT.ELF")),
       seed, os.path.getsize(seed)))
PY

vgate_run 01 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/script2.txt' \
    --script2-after 'tabwm: sidebar-rendered' \
    --input-chords 'X,Y,Z,ctrl-s' \
    --input-chords-after 'goedit: present' \
    --script3 '$RUN_DIR/script3.txt' \
    --script3-after 'goedit: saved ' \
    --script-expect 'rx-goedit-ok' --timeout 240

vgate_assert 01 serial-contains 'VirelaiOS kernel has seized control.'
vgate_assert 01 serial-contains 'tabwm: starting TABWM.BIN'
vgate_assert 01 serial-contains 'tabwm: registered'
vgate_assert 01 serial-contains 'tabwm: sidebar-rendered'
vgate_assert 01 serial-contains 'exec: loaded GOEDIT.ELF'
vgate_assert 01 serial-contains 'goedit: open id='
vgate_assert 01 serial-contains 'goedit: declare accepted'
# The seeded fixture was read (10 bytes: "seed-line\n").
vgate_assert 01 serial-contains 'goedit: read /host/EDIT/SEED.TXT n=10'
# The first frame is on the scanout (this is also the chord-release stage).
vgate_assert 01 serial-contains 'goedit: present'
# The injected keystrokes reached the buffer.
vgate_assert 01 serial-contains 'goedit: dirty'
# Ctrl-S wrote the buffer back (13 bytes: the seed plus the three typed chars).
vgate_assert 01 serial-contains 'goedit: saved /host/EDIT/SEED.TXT n=13'
# M81d: the session lease was taken at open (the marker names the holder pid)
# and released at close — the release line with no rc suffix.
vgate_assert 01 serial-contains 'goedit: lease acquired pid='
vgate_assert 01 serial-contains 'goedit: lease released'
vgate_assert 01 serial-contains 'goedit: close'
vgate_assert 01 serial-contains 'goedit OK'
vgate_assert 01 serial-contains 'rx-goedit-ok'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'exited status=139'

# The load-bearing assert: the share file on the HOST must now hold exactly the
# seed plus the typed characters. A save that reported success without writing
# the bytes cannot pass this.
vgate_assert 01 python <<'PY'
import os
share = os.environ["VG_SHARE"]
path = os.path.join(share, "EDIT", "SEED.TXT")
want = b"seed-line\nXYZ"
got = open(path, "rb").read()
if got != want:
    print("SAVED CONTENT MISMATCH: got %r want %r" % (got, want))
    raise SystemExit(1)
print("saved content verified on the host: %r (%d bytes)" % (got, len(got)))
PY

# --- M81d (#1764) run 02: the second writer is refused ----------------------
# The fixture verb stamps a foreign LIVE record (ts=9999999999: no boot outlives
# it, so the run has no timing edge) on /host/EDIT/LEASE.TXT before the editor's
# own acquire. The open-time acquire refuses (`lease held rc=-11`), the typed
# Ctrl-S refuses with the save-error line carrying -11, and the host assert
# proves the typed bytes never reached the share — the silent-loss shape the
# lease exists to make visible.
vgate_file script2-02.txt <<'EOF'
exec GOEDIT.ELF /host/EDIT/LEASE.TXT --lease-fixture=live
EOF

vgate_file script3-02.txt <<'EOF'
dui close 2
echo rx-goedit-lease-ok
EOF

vgate_run 02 -- \
    --screen '$RUN_DIR/screen-02' \
    --via-virtio \
    --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/script2-02.txt' \
    --script2-after 'tabwm: sidebar-rendered' \
    --input-chords 'A,B,ctrl-s' \
    --input-chords-after 'goedit: present' \
    --script3 '$RUN_DIR/script3-02.txt' \
    --script3-after 'goedit: save error ' \
    --script-expect 'rx-goedit-lease-ok' --timeout 240

vgate_assert 02 serial-contains 'exec: loaded GOEDIT.ELF'
vgate_assert 02 serial-contains 'goedit: lease fixture live'
# The open-time acquire read the foreign record and refused it.
vgate_assert 02 serial-contains 'goedit: read /host/EDIT/LEASE.TXT n=11'
vgate_assert 02 serial-contains 'goedit: lease held rc=-11'
# The typed keystrokes still reach the buffer — only the PUBLISH is refused.
vgate_assert 02 serial-contains 'goedit: dirty'
vgate_assert 02 serial-contains 'goedit: save error /host/EDIT/LEASE.TXT -11'
# The refused save must NOT have produced a save marker anywhere.
vgate_assert 02 serial-absent 'goedit: saved '
vgate_assert 02 serial-contains 'goedit: close'
vgate_assert 02 serial-contains 'rx-goedit-lease-ok'
vgate_assert 02 serial-absent '[EXC] parking:'

# The refused save must leave the foreign record standing — and its place
# and bytes are the host↔guest cross-check of the record NAME contract: the
# python re-implements fnv1a64 (big-endian 16 hex digits, the leaseHash rule)
# and requires the exact fixture text at that path. If the guest hashed to a
# different name or wrote a different shape, this fails.
vgate_assert 02 python <<'PY'
import os
share = os.environ["VG_SHARE"]
path = os.path.join(share, "EDIT", "LEASE.TXT")
want = b"lease-seed\n"
got = open(path, "rb").read()
if got != want:
    print("REFUSED SAVE WROTE THE FILE: got %r want %r" % (got, want))
    raise SystemExit(1)
print("refused save verified on the host: file untouched (%d bytes)" % len(got))

def fnv1a64(data: bytes) -> int:
    h = 0xcbf29ce484222325
    for b in data:
        h ^= b
        h = (h * 0x100000001b3) & 0xFFFFFFFFFFFFFFFF
    return h

target = "/host/EDIT/LEASE.TXT"
lp = os.path.join(share, "LEASES", "%016x" % fnv1a64(target.encode()))
want_rec = ("VLEASE1\npid=0\nts=9999999999\ntoken=fedcba9876543210\npath=%s\n" % target).encode()
got_rec = open(lp, "rb").read()
if got_rec != want_rec:
    print("LEASE RECORD MISMATCH at %s: got %r want %r" % (lp, got_rec, want_rec))
    raise SystemExit(1)
print("foreign live record verified at the contract path: %s" % lp)
PY

# --- M81d (#1764) run 03: a stale lease is taken over -----------------------
# The fixture stamps a foreign STALE record (ts=1: expired by any clock). The
# open-time acquire takes the lease over (`lease acquired`), the save lands,
# and the record reads back as GOEDIT's own: pid=<n> present, the foreign
# token gone — proof the takeover rewrote it, not merely ignored it.
vgate_file script2-03.txt <<'EOF'
exec GOEDIT.ELF /host/EDIT/LEASE.TXT --lease-fixture=stale
EOF

vgate_run 03 -- \
    --screen '$RUN_DIR/screen-03' \
    --via-virtio \
    --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/script2-03.txt' \
    --script2-after 'tabwm: sidebar-rendered' \
    --input-chords 'X,Y,Z,ctrl-s' \
    --input-chords-after 'goedit: present' \
    --script3 '$RUN_DIR/script3.txt' \
    --script3-after 'goedit: saved ' \
    --script-expect 'rx-goedit-ok' --timeout 240

vgate_assert 03 serial-contains 'goedit: lease fixture stale'
# The stale record was taken over at the open-time acquire, and the marker
# names the holder pid the record now carries (run 01 pins the same shape).
vgate_assert 03 serial-contains 'goedit: read /host/EDIT/LEASE.TXT n=11'
vgate_assert 03 serial-contains 'goedit: lease acquired pid='
vgate_assert 03 serial-contains 'goedit: saved /host/EDIT/LEASE.TXT n=14'
# The close released the lease: the success line has NO rc suffix (a release
# that refused to delete a foreign record would carry one).
vgate_assert 03 serial-contains 'goedit: lease released'
vgate_assert 03 serial-absent 'goedit: lease released rc='
vgate_assert 03 serial-contains 'rx-goedit-ok'
vgate_assert 03 serial-absent '[EXC] parking:'

# The load-bearing asserts: the save landed on the share, and the release is
# itself the ownership receipt — Release refuses to delete a record whose
# token is not ours, so a deleted record is one the takeover made ours. The
# python proves the record is GONE at the contract path (fnv1a64 recomputed
# host-side, the same rule run 02 cross-checks against the fixture text).
vgate_assert 03 python <<'PY'
import os
share = os.environ["VG_SHARE"]
path = os.path.join(share, "EDIT", "LEASE.TXT")
want = b"lease-seed\nXYZ"
got = open(path, "rb").read()
if got != want:
    print("TAKEN-OVER SAVE MISMATCH: got %r want %r" % (got, want))
    raise SystemExit(1)
print("taken-over save verified on the host: %r (%d bytes)" % (got, len(got)))

def fnv1a64(data: bytes) -> int:
    h = 0xcbf29ce484222325
    for b in data:
        h ^= b
        h = (h * 0x100000001b3) & 0xFFFFFFFFFFFFFFFF
    return h

target = "/host/EDIT/LEASE.TXT"
lp = os.path.join(share, "LEASES", "%016x" % fnv1a64(target.encode()))
if os.path.exists(lp):
    print("LEASE RECORD SURVIVED THE RELEASE: %r" % open(lp, "rb").read())
    raise SystemExit(1)
print("lease record released at the contract path: %s" % lp)
PY
