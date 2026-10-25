# go-vault.spec -- M148a (issue #2804) + M148b (issue #2805) + M156c (#2933):
# GOVAULT.ELF class-B gate, run K=3. A PBKDF2+AES-256-GCM vault file under
# the Go seat: create through the masked pair, add an entry, reveal it, copy
# the secret onto the seat's primary selection, watch the 30 s lease expire,
# lock, unlock, quit. Two tamper legs and a wrong-pass leg pin the named
# refusals. The M148b acceptance leg (run 06) stores, locks, unlocks,
# reveals, copies, expires, then opens the Receipts face and pins its rows
# in order — on the serial listing AND on the vault's own receipts file in
# the share. M156c (run 07) is the bundle arc end to end — picker save,
# GVLX under its own passphrase, a fresh vault, picker open, the no-clobber
# merge — plus the M125a save_state hook and the TOTP probe (hmac/sha1 in
# class-A; guest clock within ±30 s of host, and the displayed six-digit
# code glyph-matched against a host-computed TOTP). M196d (run 08) is the
# provisioning leg: the vault's 'p' verb stages a mail-<acct> row through
# the slot-96 ask seam, the seat's modal consents, and the consumer —
# GOMAIL, reading the app-bound row — is the sys_secret_get read-back in
# the same boot. Iteration 1's ask is mode-new; iterations 2/3 exercise
# the #3407 replace path (SECRETS.TXT persists and the file ABI cannot
# delete it, so the row is already there). M196e (run 09) is the wave's
# acceptance journey — one boot where the whole arc holds at once: the
# vault provisions irc-gate through the seat's modal (the OTHER bind arm,
# bound to GOIRC.ELF — the modal owns the keyboard while it stands and
# focus falls to the terminal when it closes, so one boot consents one
# ask), GOIRC resolves the just-granted row into its dial boundary,
# GOMAIL reads the mail-gate row run 08 persisted earlier in the same
# iteration, and an unprovisioned mail-ghost names secret_not_found —
# all in the same boot, K=3.
#
# REPEAT SEMANTICS: vgate_repeat 3 re-runs EVERY leg, and the armed share
# persists across iterations — so each leg must be iteration-safe. Run 01
# deletes VAULT.BIN so the create face returns; run 02 re-clones it to
# VAULT.REF so the tamper legs (04/05) can restore a valid vault before
# mangling it; run 06 deletes VAULT.BIN and recreates it self-contained.
# RECEIPTS.TXT is never reset — the accumulating audit file IS the K=3
# evidence (the receipts view's n= grows 11, 22, 33).
#
# The clipboard story, for whoever audits the asserts: the KERNEL clipboard
# (slots 38/39) is seat/CAP_CLIPBOARD only, so the app's copy surface is the
# seat's primary selection (M124e kind-27 publish / kind-28 pull). The buffer
# contents are proven by the SEAT's own `gotabwm: sel pub` len markers — the
# buffer is seat memory, not a file a python assert can read — and the
# `clip: empty` read proves the secret never reached the kernel buffer.
# Clearing is an overwrite (`[cleared]`, 9 bytes) because kind-27 refuses an
# empty publish.
#
# exec-order: assert-proven -- every stage gate is anchored on output only
# the seat or the app produces (`gotabwm: registered`, `govault: *`); a
# program that never ran cannot pass.
#
# HOST PREREQUISITE (fails the gate honestly when missing):
#   bash tools/go/build-govault.sh  ->  .build/go/GOVAULT.ELF
#   bash tools/go/build-gotabwm.sh  ->  .build/go/GOTABWM.ELF
#   bash tools/go/build-gomail.sh   ->  .build/go/GOMAIL.ELF
#   bash tools/go/build-goirc.sh    ->  .build/go/GOIRC.ELF

vgate_name go-vault "M148a #2804 + M148b #2805 + M196d #3433 + M196e #3445: PBKDF2/AES-GCM vault — masked unlock, entry store, per-entry reveal, copy-with-expiry on the seat selection, idle lock, named refusals, receipts file + Receipts view; K=3, + the slot-96 provision leg: 'p' asks, the seat consents, GOMAIL reads the bound row back, + the acceptance journey: two consent-provisioned rows (GOMAIL+GOIRC consume, ghost names secret_not_found)"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 3

# Every iteration must find no vault on the share, so the app boots into the
# create face again — 'govault: new vault' is the chords gate.
vgate_file script-01.txt <<'EOF'
set GOMAXPROCS=1
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm GOVAULT/VAULT.BIN
wm
exec GOTABWM.ELF
EOF

vgate_file script2-01.txt <<'EOF'
dui focus 0
exec GOVAULT.ELF
EOF

# Create the vault (two masked fields), add wifi/s3cr3t/home, reveal, copy,
# ctrl-l lock, unlock, esc lock, esc quit. The pristine clone is seeded here
# (run 02's script is the earliest leg where VAULT.BIN exists on the share)
# so the tamper legs can restore it — `vf clone` refuses a live target, so
# REF is removed and re-taken every iteration.
vgate_file script-02.txt <<'EOF'
set GOMAXPROCS=1
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm GOVAULT/VAULT.REF
vf clone GOVAULT/VAULT.BIN GOVAULT/VAULT.REF
wm
exec GOTABWM.ELF
EOF

vgate_file script2-02.txt <<'EOF'
dui focus 0
exec GOVAULT.ELF
EOF

vgate_file script3-02.txt <<'EOF'
clip
echo rx-vault-done
EOF

vgate_file script-03.txt <<'EOF'
set GOMAXPROCS=1
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
wm
exec GOTABWM.ELF
EOF

vgate_file script2-03.txt <<'EOF'
dui focus 0
exec GOVAULT.ELF
EOF

# Tamper legs need no seat: the refusals live in the load/unlock path. Each
# first restores a VALID vault — iteration n's VAULT.BIN was left mangled by
# iteration n-1's run 05 — then mangles it the leg's own way.
vgate_file script-04.txt <<'EOF'
vf rm GOVAULT/VAULT.BIN
vf clone GOVAULT/VAULT.REF GOVAULT/VAULT.BIN
vf open GOVAULT/VAULT.BIN
vf truncate 0 61
vf close 0
exec GOVAULT.ELF
EOF

vgate_file script-05.txt <<'EOF'
vf rm GOVAULT/VAULT.BIN
vf clone GOVAULT/VAULT.REF GOVAULT/VAULT.BIN
vf open GOVAULT/VAULT.BIN
vf write 0 4
vf close 0
exec GOVAULT.ELF
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
run = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(run, "share")
for elf, build in (("GOVAULT.ELF", "tools/go/build-govault.sh"),
                   ("GOTABWM.ELF", "tools/go/build-gotabwm.sh")):
    src = os.path.join(".build", "go", elf)
    if not os.path.exists(src):
        sys.exit("%s missing (expected %s) - build it first: bash %s"
                 % (elf, src, build))
    shutil.copy(src, os.path.join(share, elf))
    print("staged %s into share (%d bytes)" % (elf, os.path.getsize(src)))
# The compiled default is already the Go seat (M59); seed wm=none so the
# seat under test is the executed GOTABWM.ELF, not the autostart path.
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    f.write("#v2\nwm=none\n")
# The copy path is the seat's primary selection, which the seat admits only
# for apps whose manifest row carries `selection=primary` (M124e). GOVAULT's
# row ships it — assert it here so a manifest that dropped the opt-in fails
# the gate where it would silently refuse the copy.
apps = os.path.join(share, "APPS.TXT")
if not os.path.exists(apps):
    shutil.copy("image/apps.txt", apps)
row = [ln for ln in open(apps).read().splitlines()
       if ln.startswith("GOVAULT.ELF")]
if not row or "selection=primary" not in row[0]:
    sys.exit("GOVAULT's APPS.TXT row must carry selection=primary: %r"
             % (row,))
print("share APPS.TXT GOVAULT row carries selection=primary")

# M196d (run 08): the provision leg's consumer read-back. GOMAIL resolves
# its password as the store row mail-<user>, app-bound to GOMAIL.ELF — an
# apps-allowlist entry is visible only when the named image is
# operator-owned on /host (kernel secret.zig visible() ->
# trust.system_owned_key), so OWNERS.TXT pins GOMAIL.ELF uid_system 0644.
# GOMAIL.TXT is the bare account the leg execs (no pass= — the store is
# the only credential source since M196b). SECRETS.TXT is deliberately NOT
# seeded: the vault's 'p' verb provisions the row through the consent
# path, and the kernel persists it itself.
gomail = os.path.join(".build", "go", "GOMAIL.ELF")
if not os.path.exists(gomail):
    sys.exit("GOMAIL.ELF missing (expected .build/go/GOMAIL.ELF) - build "
             "it first: bash tools/go/build-gomail.sh")
shutil.copy(gomail, os.path.join(share, "GOMAIL.ELF"))
with open(os.path.join(share, "GOMAIL.TXT"), "w") as f:
    f.write("# go-vault M196d consumer fixture — the store holds the pass\n"
            "host=10.0.0.2\n"
            "user=gate\n"
            "plain=on\n")
# M196e (run 09): GOIRC is the journey's second consumer — the vault
# provisions irc-gate bound to it through the same consent path, so its
# OWNERS row rides the same uid_system pinning GOMAIL's does. All argv —
# no config file exists to carry a credential.
goirc = os.path.join(".build", "go", "GOIRC.ELF")
if not os.path.exists(goirc):
    sys.exit("GOIRC.ELF missing (expected .build/go/GOIRC.ELF) - build "
             "it first: bash tools/go/build-goirc.sh")
shutil.copy(goirc, os.path.join(share, "GOIRC.ELF"))
with open(os.path.join(share, "OWNERS.TXT"), "w") as f:
    f.write("#v1\nGOMAIL.ELF\t644\t0\t-\nGOIRC.ELF\t644\t0\t-\n")
print("go-vault: staged GOMAIL.ELF+GOIRC.ELF + GOMAIL.TXT/OWNERS.TXT "
      "for runs 08/09")
PY

# --- run 01: create, add, reveal, copy, lock, unlock, quit ------------------
vgate_run 01 -- \
    --screen '$RUN_DIR/vault-screen-01' \
    --via-virtio \
    --script '$RUN_DIR/script-01.txt' \
    --script2 '$RUN_DIR/script2-01.txt' \
    --script2-after 'gotabwm: registered' \
    --input-chords 'h,u,n,t,e,r,2,return,h,u,n,t,e,r,2,return,a,w,i,f,i,tab,s,3,c,r,3,t,tab,h,o,m,e,return,return,r,c,ctrl-l,h,u,n,t,e,r,2,return,escape,escape' \
    --input-chords-after 'govault: new vault' \
    --script-expect 'govault: exiting 0' --timeout 240

vgate_assert 01 serial-contains 'VirelaiOS kernel has seized control.'
vgate_assert 01 serial-contains 'gotabwm: registered'
vgate_assert 01 serial-contains 'exec: loaded GOVAULT.ELF'
vgate_assert 01 serial-contains 'govault: open id='
vgate_assert 01 serial-contains 'govault: declare accepted'
vgate_assert 01 serial-contains 'govault: new vault'
vgate_assert 01 serial-contains 'govault: iterations '
vgate_assert 01 serial-contains 'govault: saved'
vgate_assert 01 serial-contains 'govault: unlocked n=0'
vgate_assert 01 serial-contains 'govault: added'
vgate_assert 01 serial-contains 'govault: revealed'
vgate_assert 01 serial-contains 'govault: copied'
vgate_assert 01 serial-contains 'govault: locked'
vgate_assert 01 serial-contains 'govault: unlocked n=1'
vgate_assert 01 serial-contains 'govault: close'
vgate_assert 01 serial-contains 'govault OK'
vgate_assert 01 serial-contains 'govault: exiting 0'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'exited status=139'
# The seat's own receipt that the copy landed in the buffer: a 6-byte
# publish ("s3cr3t") from GOVAULT's window.
vgate_assert 01 serial-contains 'gotabwm: sel pub id='
vgate_assert 01 python <<'PY'
import os, re
ser = open(os.environ["VG_SER"], "rb").read().decode("utf-8", "replace")
for m in re.finditer(r"gotabwm: sel pub id=\d+ len=(\d+)", ser):
    if int(m.group(1)) == 6:
        print("selection publish len=6 confirmed by the seat")
        break
else:
    raise SystemExit("no len=6 selection publish in serial")
PY
# The vault file itself on the host share: GVLT framing, sealed body.
vgate_assert 01 python <<'PY'
import os
share = os.environ["VG_SHARE"]
path = os.path.join(share, "GOVAULT", "VAULT.BIN")
b = open(path, "rb").read()
if b[:4] != b"GVLT":
    raise SystemExit("vault magic = %r, want GVLT" % b[:4])
if int.from_bytes(b[4:8], "little") != 2:
    raise SystemExit("vault version = %d, want 2 (the field-added frame)"
                     % int.from_bytes(b[4:8], "little"))
iters = int.from_bytes(b[8:12], "little")
if not (100_000 <= iters <= 4_000_000):
    raise SystemExit("recorded iters = %d, outside calibration clamps" % iters)
if b"s3cr3t" in b or b"wifi" in b or b"hunter2" in b:
    raise SystemExit("PLAINTEXT secret material in the sealed file")
print("VAULT.BIN: GVLT v2, iters=%d recorded, body sealed (%d bytes)"
      % (iters, len(b)))
PY

# --- run 02: copy expiry — the 30 s lease overwrites the selection ---------
vgate_run 02 -- \
    --screen '$RUN_DIR/vault-screen-02' \
    --via-virtio \
    --script '$RUN_DIR/script-02.txt' \
    --script2 '$RUN_DIR/script2-02.txt' \
    --script2-after 'gotabwm: registered' \
    --input-chords 'h,u,n,t,e,r,2,return,c' \
    --input-chords-after 'govault: locked' \
    --script3 '$RUN_DIR/script3-02.txt' \
    --script3-after 'govault: clip expired' \
    --script-expect 'rx-vault-done' --timeout 240

vgate_assert 02 serial-contains 'gotabwm: registered'
vgate_assert 02 serial-contains 'exec: loaded GOVAULT.ELF'
vgate_assert 02 serial-contains 'govault: locked'
vgate_assert 02 serial-contains 'govault: unlocked n=1'
vgate_assert 02 serial-contains 'govault: copied'
vgate_assert 02 serial-contains 'govault: clip expired'
# The compare rule's pull is the seat's own `sel pull` receipt; the expiry
# publish is the 9-byte [cleared] sentinel, both ordered after the copy.
vgate_assert 02 python <<'PY'
import os, re
ser = open(os.environ["VG_SER"], "rb").read().decode("utf-8", "replace")
lens = [int(m.group(1))
        for m in re.finditer(r"gotabwm: sel pub id=\d+ len=(\d+)", ser)]
if len(lens) < 2 or lens[0] != 6 or 9 not in lens[1:]:
    raise SystemExit("selection publishes len=%s, want 6 then 9 (the "
                     "[cleared] overwrite)" % lens)
if "gotabwm: sel pull id=" not in ser:
    raise SystemExit("the expiry's compare pull is missing its seat receipt")
print("selection: copy len=6, expiry overwrite len=9, compare pull seen")
PY
# The KERNEL clipboard was never touched — our lane is the selection.
vgate_assert 02 serial-contains 'clip: empty'
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 serial-absent 'exited status=139'

# --- run 03: wrong passphrase is `unlock failed`, nothing more specific ----
vgate_run 03 -- \
    --screen '$RUN_DIR/vault-screen-03' \
    --via-virtio \
    --script '$RUN_DIR/script-03.txt' \
    --script2 '$RUN_DIR/script2-03.txt' \
    --script2-after 'gotabwm: registered' \
    --input-chords 'b,a,d,p,a,s,s,return,h,u,n,t,e,r,2,return,escape,escape' \
    --input-chords-after 'govault: locked' \
    --script-expect 'govault: exiting 0' --timeout 240

vgate_assert 03 serial-contains 'exec: loaded GOVAULT.ELF'
vgate_assert 03 serial-contains 'govault: locked'
vgate_assert 03 serial-contains 'govault: unlock failed'
vgate_assert 03 serial-contains 'govault: unlocked n=1'
vgate_assert 03 serial-contains 'govault OK'
vgate_assert 03 serial-absent '[EXC] parking:'
vgate_assert 03 serial-absent 'exited status=139'

# --- run 04: ciphertext tamper refuses as `unlock failed` -------------------
# `vf truncate 0 61` keeps the 44-byte header and cuts inside the sealed
# region: the frame still parses, the GCM tag still refuses — the named
# refusal, not a crash and not a partial unlock.
vgate_run 04 -- \
    --screen '$RUN_DIR/vault-screen-04' \
    --via-virtio \
    --script '$RUN_DIR/script-04.txt' \
    --input-chords 'h,u,n,t,e,r,2,return,escape' \
    --input-chords-after 'govault: locked' \
    --script-expect 'govault: exiting 0' --timeout 240

vgate_assert 04 serial-contains 'vf: open GOVAULT/VAULT.BIN'
vgate_assert 04 serial-contains 'vf: truncate 0 size=61 ok'
vgate_assert 04 serial-contains 'exec: loaded GOVAULT.ELF'
vgate_assert 04 serial-contains 'govault: locked'
vgate_assert 04 serial-contains 'govault: unlock failed'
vgate_assert 04 serial-absent 'govault: bad file'
vgate_assert 04 serial-absent 'govault: unlocked'
vgate_assert 04 serial-absent '[EXC] parking:'
vgate_assert 04 serial-absent 'exited status=139'

# --- run 05: magic tamper refuses as `bad file` -----------------------------
# `vf write 0 4` overwrites the GVLT magic through the host channel: the
# refusal is structural, named before a passphrase is spent.
vgate_run 05 -- \
    --screen '$RUN_DIR/vault-screen-05' \
    --via-virtio \
    --script '$RUN_DIR/script-05.txt' \
    --input-chords 'escape' \
    --input-chords-after 'govault: bad file' \
    --script-expect 'govault: exiting 0' --timeout 240

vgate_assert 05 serial-contains 'vf: write 0 n=4 wrote=4'
vgate_assert 05 serial-contains 'exec: loaded GOVAULT.ELF'
vgate_assert 05 serial-contains 'govault: bad file'
vgate_assert 05 serial-absent 'govault: unlock failed'
vgate_assert 05 serial-absent 'govault: unlocked'
vgate_assert 05 serial-contains 'govault OK'
vgate_assert 05 serial-absent '[EXC] parking:'
vgate_assert 05 serial-absent 'exited status=139'

# --- run 06: acceptance — the receipts face lists the journey in order ------
# Self-contained (M148b): the vault is deleted and recreated inside this leg,
# so it runs identically on every iteration. The journey is store, lock,
# unlock, reveal, copy, expire — then the Receipts face. The 'v' chord rides
# --input-string (printable-only) gated on 'govault: clip expired', so the
# receipts listing lands after the lease fire exactly as the card orders it;
# the run ends on 'govault: receipts end', which the app prints only after
# every row marker, so the assert sees the whole listing.
vgate_file script-06.txt <<'EOF'
set GOMAXPROCS=1
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm GOVAULT/VAULT.BIN
wm
exec GOTABWM.ELF
EOF

vgate_file script2-06.txt <<'EOF'
dui focus 0
exec GOVAULT.ELF
EOF

vgate_run 06 -- \
    --screen '$RUN_DIR/vault-screen-06' \
    --via-virtio \
    --script '$RUN_DIR/script-06.txt' \
    --script2 '$RUN_DIR/script2-06.txt' \
    --script2-after 'gotabwm: registered' \
    --input-chords 'h,u,n,t,e,r,2,return,h,u,n,t,e,r,2,return,a,b,a,n,k,tab,h,u,n,t,e,r,3,tab,s,a,v,i,n,g,s,return,return,ctrl-l,h,u,n,t,e,r,2,return,r,c' \
    --input-chords-after 'govault: new vault' \
    --input-string 'v' \
    --input-string-after 'govault: clip expired' \
    --script-expect 'govault: receipts end' --timeout 240

vgate_assert 06 serial-contains 'gotabwm: registered'
vgate_assert 06 serial-contains 'exec: loaded GOVAULT.ELF'
vgate_assert 06 serial-contains 'govault: new vault'
vgate_assert 06 serial-contains 'govault: iterations '
vgate_assert 06 serial-contains 'govault: saved'
vgate_assert 06 serial-contains 'govault: unlocked n=0'
vgate_assert 06 serial-contains 'govault: added'
vgate_assert 06 serial-contains 'govault: locked'
vgate_assert 06 serial-contains 'govault: unlocked n=1'
vgate_assert 06 serial-contains 'govault: revealed'
vgate_assert 06 serial-contains 'govault: copied'
vgate_assert 06 serial-contains 'govault: clip expired'
vgate_assert 06 serial-contains 'govault: receipts end'
vgate_assert 06 serial-absent '[EXC] parking:'
vgate_assert 06 serial-absent 'exited status=139'
# The Receipts face on serial: a contiguous i=0..n-1 listing matching the
# count line, ending on the journey's own rows — unlock, reveal, copy — in
# append order.
vgate_assert 06 python <<'PY'
import os, re
ser = open(os.environ["VG_SER"], "rb").read().decode("utf-8", "replace")
rows = re.findall(r"govault: rcpt i=(\d+) (\S+)", ser)
m = re.search(r"govault: receipts n=(\d+)", ser)
if not m or not rows:
    raise SystemExit("no receipts listing on serial")
n = int(m.group(1))
if len(rows) != n or [int(i) for i, _ in rows] != list(range(n)):
    raise SystemExit("receipt listing incomplete/disordered: %d rows "
                     "vs n=%d: %s" % (len(rows), n, rows))
tail = [a for _, a in rows[-3:]]
if tail != ["unlock", "reveal", "copy"]:
    raise SystemExit("receipt tail %s — want the journey's "
                     "unlock,reveal,copy in order" % tail)
print("Receipts face listed n=%d rows, contiguous, tail=unlock,reveal,copy" % n)
PY
# The vault's own receipts file on the share (share-contains also lifts the
# file into the evidence artifacts): the stored entry's name lands on its
# reveal and copy rows.
vgate_assert 06 share-contains GOVAULT/RECEIPTS.TXT 'reveal bank'
vgate_assert 06 share-contains GOVAULT/RECEIPTS.TXT 'copy bank'
# Every row is "<epoch> <action> <name>", the tail is the leg's journey in
# order, the names are the stored entry's — and no secret material ever
# lands in it.
vgate_assert 06 python <<'PY'
import os, re
share = os.environ["VG_SHARE"]
data = open(os.path.join(share, "GOVAULT", "RECEIPTS.TXT"), "rb").read().decode("utf-8", "replace")
rows = []
for ln in data.split("\n"):
    if not ln:
        continue
    m = re.match(r"(\d+) (\S+) (.*)$", ln)
    if not m:
        raise SystemExit("corrupt receipt row: %r" % ln)
    rows.append((int(m.group(1)), m.group(2), m.group(3)))
acts = [a for _, a, _ in rows]
if acts[-3:] != ["unlock", "reveal", "copy"]:
    raise SystemExit("RECEIPTS.TXT tail = %s, want unlock,reveal,copy" % acts[-3:])
for action in ("unlock", "reveal", "copy", "unlock-failed"):
    if action not in acts:
        raise SystemExit("no %s receipt in the file" % action)
if rows[-2][2] != "bank" or rows[-1][2] != "bank":
    raise SystemExit("tail names = %r/%r, want the stored entry 'bank'"
                     % (rows[-2][2], rows[-1][2]))
for secret in ("hunter2", "hunter3", "s3cr3t"):
    if secret in data:
        raise SystemExit("SECRET material in the receipts file: %r" % secret)
print("RECEIPTS.TXT: %d rows, ordered tail unlock,reveal,copy(bank), no secrets" % len(rows))
PY
# The copy landed on the seat's selection (7-byte "hunter3" publish), and the
# expiry overwrite wrote the 9-byte [cleared] sentinel after the compare pull.
vgate_assert 06 python <<'PY'
import os, re
ser = open(os.environ["VG_SER"], "rb").read().decode("utf-8", "replace")
lens = [int(m.group(1))
        for m in re.finditer(r"gotabwm: sel pub id=\d+ len=(\d+)", ser)]
if len(lens) < 2 or lens[0] != 7 or 9 not in lens[1:]:
    raise SystemExit("selection publishes len=%s, want 7 then 9 "
                     "(the [cleared] overwrite)" % lens)
if "gotabwm: sel pull id=" not in ser:
    raise SystemExit("the expiry's compare pull is missing its seat receipt")
print("selection: copy len=7, expiry overwrite len=9, compare pull seen")
PY

# --- run 07 (M156c / #2933): export -> fresh vault -> import, one boot ------
# Two app sessions share the leg. Session A opens the vault run 06 left
# (bank/hunter3, passphrase hunter2 — 'govault: locked' is its gate),
# adds 'mail' (with the RFC 6238 seed GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ,
# the base32 text of the ASCII key "12345678901234567890") and 'pad', and
# exports the three entries to GOVAULT/EXPORT.GVLX under its own passphrase
# 'xport1'. script3 then deletes the vault and re-execs the app: session B's
# 'govault: new vault' is the unique marker the second input phase gates on
# (A never prints it — it unlocked). B creates a FRESH vault (pass fresh9),
# picks the bundle, types its passphrase, and the merge lands added=3.
# B then reveals row 0 (bank -> 'hunter3' painted), copies it (seat
# 'sel pub len=7'), answers the seat's ctrl-shift-s state ask
# ('govault: state saved' + the blob in SESSION.TABS), and locks.
# Iteration-safe: script-07 re-primes the share the same way every pass —
# EXPORT.GVLX is removed so the write target is fresh, and VAULT.BIN is
# always run 06's one-entry vault of THIS iteration.
vgate_file script-07.txt <<'EOF'
set GOMAXPROCS=1
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm GOVAULT/EXPORT.GVLX
wm
exec GOTABWM.ELF
EOF

vgate_file script2-07.txt <<'EOF'
dui focus 0
exec GOVAULT.ELF
EOF

vgate_file script3-07.txt <<'EOF'
vf rm GOVAULT/VAULT.BIN
dui focus 0
exec GOVAULT.ELF
EOF

vgate_run 07 -- \
    --screen '$RUN_DIR/vault-screen-07' \
    --via-virtio --cvc-snap --snapshot-out '$RUN_DIR/snap-07' \
    --snapshot-after 'govault: import added=3 skipped=0' \
    --snapshot-after 'govault: revealed' \
    --snapshot-after 'govault: copied' \
    --snapshot-after 'govault: state saved' \
    --snapshot-after 'gotabwm: snapshot saved' \
    --script '$RUN_DIR/script-07.txt' \
    --script2 '$RUN_DIR/script2-07.txt' \
    --script2-after 'gotabwm: registered' \
    --input-string $'hunter2\namail\nletmein\nwork\nGEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ\napad\nabcd\nside\n\neEXPORT.GVLX\nxport1\nxport1\n' \
    --input-string-after 'govault: locked' \
    --script3 '$RUN_DIR/script3-07.txt' \
    --script3-after 'govault: export n=3' \
    --input-chords 'f,r,e,s,h,9,return,f,r,e,s,h,9,return,i,E,X,P,O,R,T,.,G,V,L,X,return,x,p,o,r,t,1,return,up,up,r,c,ctrl-shift-s' \
    --input-chords-after 'govault: new vault' \
    --script-expect 'gotabwm: snapshot saved' --timeout 300

vgate_assert 07 serial-contains 'gotabwm: registered'
vgate_assert 07 serial-contains 'exec: loaded GOVAULT.ELF'
vgate_assert 07 serial-contains 'govault: locked'
vgate_assert 07 serial-contains 'govault: unlocked n=1'
vgate_assert 07 serial-contains 'govault: added'
vgate_assert 07 serial-contains 'govault: pick save /host/GOVAULT/EXPORT.GVLX'
vgate_assert 07 serial-contains 'govault: export n=3 bytes='
# Session B: a fresh vault face, the import merge, the republish, reveal.
vgate_assert 07 serial-contains 'govault: new vault'
vgate_assert 07 serial-contains 'govault: iterations '
vgate_assert 07 serial-contains 'govault: unlocked n=0'
vgate_assert 07 serial-contains 'govault: pick open /host/GOVAULT/EXPORT.GVLX'
vgate_assert 07 serial-contains 'govault: import added=3 skipped=0'
vgate_assert 07 serial-contains 'govault: revealed'
vgate_assert 07 serial-contains 'govault: copied'
# The save_state hook answered the seat's snapshot chord — the blob is the
# vault's path, nothing secret.
vgate_assert 07 serial-contains 'gotabwm: state ask id='
vgate_assert 07 serial-contains 'govault: state saved'
vgate_assert 07 serial-contains 'gotabwm: state save id='
vgate_assert 07 serial-contains 'len=23 ver=1'
vgate_assert 07 serial-contains 'gotabwm: snapshot saved'
# No secret material ever reaches serial — secrets, the seed and both
# passphrases stay off the wire.
vgate_assert 07 serial-absent 'hunter3'
vgate_assert 07 serial-absent 'letmein'
vgate_assert 07 serial-absent 'GEZDGNBVGY3TQOJQ'
vgate_assert 07 serial-absent 'xport1'
vgate_assert 07 serial-absent '[EXC] parking:'
vgate_assert 07 serial-absent 'exited status=139'
# The TOTP probe's guest leg: the wall clock the codes are computed
# against, within ±30 s of the host clock at verdict time.
vgate_assert 07 python <<'PY'
import os, re, sys, time
ser = open(os.environ["VG_SER"], "rb").read().decode("utf-8", "replace")
marks = re.findall(r"govault: clock epoch=(\d+)", ser)
if not marks:
    sys.exit("no govault clock marker — the probe's guest leg is missing")
guest = int(marks[-1])
skew = abs(guest - int(time.time()))
if skew > 30:
    sys.exit("guest clock epoch=%d vs host %d — skew %ds exceeds ±30s"
             % (guest, int(time.time()), skew))
print("clock probe: guest epoch=%d host=%d skew=%ds" % (guest, int(time.time()), skew))
PY
# The bundle on the share: GVLX framing, v2, sealed body — no plaintext.
vgate_assert 07 python <<'PY'
import os
share = os.environ["VG_SHARE"]
b = open(os.path.join(share, "GOVAULT", "EXPORT.GVLX"), "rb").read()
if b[:4] != b"GVLX":
    raise SystemExit("bundle magic = %r, want GVLX" % b[:4])
if int.from_bytes(b[4:8], "little") != 2:
    raise SystemExit("bundle version = %d, want 2" % int.from_bytes(b[4:8], "little"))
iters = int.from_bytes(b[8:12], "little")
if not (100_000 <= iters <= 4_000_000):
    raise SystemExit("bundle iters = %d, outside calibration clamps" % iters)
for pt in (b"mail", b"bank", b"pad", b"letmein", b"hunter3", b"abcd",
           b"GEZDGNBVGY3TQOJQ"):
    if pt in b:
        raise SystemExit("PLAINTEXT entry material in the bundle: %r" % pt)
print("EXPORT.GVLX: GVLX v2, iters=%d, body sealed (%d bytes)" % (iters, len(b)))
PY
# The merge's audit rows and the state blob on the share.
vgate_assert 07 share-contains GOVAULT/RECEIPTS.TXT 'import added=3 skipped=0'
vgate_assert 07 share-contains SESSION.TABS '/host/GOVAULT/VAULT.BIN'
# The displayed half: the revealed row paints 'hunter3' (the secret that
# round-tripped the bundle), and the mail row's six-digit code equals the
# host's own stdlib-hmac TOTP for the run's timestamp — the card's probe.
vgate_assert 07 snapshot 'snap-07-*.raw' <<'PY'
import os, re, sys, glob as _glob
import hmac as _hmac, hashlib as _hl, struct as _st

W, H = 1280, 720
need = W * H * 4
snaps = sorted(_glob.glob(os.path.join(os.path.dirname(sys.argv[1]),
                                       "snap-07-*.raw")),
               key=os.path.getmtime)
frames = [(os.path.basename(p), open(p, "rb").read())
          for p in snaps if os.path.getsize(p) >= need]
if not frames:
    sys.exit("no complete snapshot in %s" % snaps)

# The 8x8 face — same table the widget kit links (go-clock's assert lift):
# 95 glyphs, 8 rows of 1 byte, bit 0 the leftmost pixel.
src = open("user/go/webrender/font/font8x8.go").read()
rows = re.findall(r"\{(0x[0-9a-fA-F]{2}(?:,\s*0x[0-9a-fA-F]{2}){7})\}", src)
if len(rows) != 95:
    sys.exit("font8x8.go: %d glyph rows, want 95" % len(rows))
GB = {}
for i, r in enumerate(rows):
    vals = [int(v, 16) for v in r.split(",") if v.strip()]
    mask = 0
    for y in range(8):
        for x in range(8):
            mask = (mask << 1) | ((vals[y] >> x) & 1)
    GB[0x20 + i] = mask

def px(raw, x, y):
    off = (y * W + x) * 4
    return raw[off + 2], raw[off + 1], raw[off]

def decode_cell(raw, x0, y0):
    lums = [0.30 * px(raw, x0 + k % 8, y0 + k // 8)[0] +
            0.55 * px(raw, x0 + k % 8, y0 + k // 8)[1] +
            0.15 * px(raw, x0 + k % 8, y0 + k // 8)[2] for k in range(64)]
    lo, hi = min(lums), max(lums)
    if hi - lo < 20:
        return " "
    mid = (lo + hi) / 2
    mask = 0
    for v in lums:
        mask = (mask << 1) | (v >= mid)
    best, bs = 32, 999
    for cp, gm in GB.items():
        d = bin(mask ^ gm).count("1")
        if d < bs:
            bs, best = d, cp
    return chr(best)

def decode_at(raw, x0, y0, n):
    return "".join(decode_cell(raw, x0 + j * 8, y0) for j in range(n))

# Host-side TOTP for the bundle seed (python stdlib hmac — the card's own
# cross-check): every epoch inside the run's plausible paint window is an
# acceptable displayed code.
ser_hits = sorted(_glob.glob(os.path.join(os.path.dirname(sys.argv[1]),
                                          "vm-serial-07-*.log")),
                  key=os.path.getmtime)
if not ser_hits:
    sys.exit("no serial log for leg 07 in %s" % os.path.dirname(sys.argv[1]))
ser = open(ser_hits[-1], errors="replace").read()
cm = re.findall(r"govault: clock epoch=(\d+)", ser)
if not cm:
    sys.exit("no clock marker beside the snapshot")
centre = int(cm[-1])
seed = b"12345678901234567890"
codes = set()
for e in range(centre - 45, centre + 125):
    c = _st.pack(">Q", e // 30)
    d = _hmac.new(seed, c, _hl.sha1).digest()
    o = d[-1] & 0x0F
    v = (_st.unpack(">I", d[o:o + 4])[0] & 0x7FFFFFFF) % 1000000
    codes.add("%06d" % v)

# Rows live at canvas (12, 35+14i) for names, (188, +) secrets, (300, +)
# the code — but the pane origin is the seat's, so find it: anchor on the
# entry names over a bounded origin grid, then read the row's fields at
# the same origin. A frame that carries neither face fails closed.
def find_origin(raw):
    for oy in range(0, 160, 1):
        for ox in range(0, 128, 4):
            if decode_at(raw, ox + 12, oy + 35, 4) != "bank":
                continue
            if decode_at(raw, ox + 12, oy + 49, 4) == "mail":
                return ox, oy
    return None

verdict = None
for name, raw in frames:
    org = find_origin(raw)
    if org is None:
        continue
    ox, oy = org
    revealed = decode_at(raw, ox + 188, oy + 35, 7)
    if revealed != "hunter3":
        verdict = "revealed cell %r, want 'hunter3'" % revealed
        continue
    shown = decode_at(raw, ox + 300, oy + 49, 6)
    if shown not in codes:
        verdict = "totp cell %r not in %d host-computed codes" % (shown, len(codes))
        continue
    left = decode_at(raw, ox + 300 + 7 * 8, oy + 49, 3)
    m = re.match(r"(\d+)s\s*$", left)
    if not m or not (0 < int(m.group(1)) <= 30):
        verdict = "countdown cell %r is not Ns" % left
        continue
    print("snapshot %s: revealed 'hunter3', totp %s in host window, "
          "countdown %s (origin %d,%d)" % (name, shown, left, ox, oy))
    sys.exit(0)
sys.exit(verdict or "no capture carried the list face (revealed row + totp)")
PY

# --- run 08 (M196d / #3433): 'p' provisions through the slot-96 ask seam --
# The vault entry IS the store row: name mail-gate (the M196b convention,
# bound to GOMAIL.ELF by provisionApps), secret pw-gate-7. 'p' stages the
# ask; the seat's modal stands on 'gotabwm: secret-ask ... key=mail-gate';
# the 'g' chord is the consent gesture — the grant lands the entry AND
# persists SECRETS.TXT. GOVAULT's once-per-pass status read reports the
# verdict ('govault: provision-grant'), and the consumer is the read-back:
# GOMAIL's `boxes` resolves mail-gate out of slot 70 ('gomail: secret
# mail-gate') before its dial — script-expect ends the run there, so the
# dead 10.0.0.2 transport is never the verdict. Iteration-safe: iteration
# 1 stages mode-new; iterations 2/3 meet the persisted row and exercise
# the #3407 replace path — the markers are mode-agnostic.
vgate_file script-08.txt <<'EOF'
set GOMAXPROCS=1
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm GOVAULT/VAULT.BIN
wm
exec GOTABWM.ELF
EOF

vgate_file script2-08.txt <<'EOF'
dui focus 0
exec GOVAULT.ELF
EOF

vgate_file script3-08.txt <<'EOF'
exec GOMAIL.ELF boxes
EOF

vgate_run 08 -- \
    --screen '$RUN_DIR/vault-screen-08' \
    --via-virtio \
    --script '$RUN_DIR/script-08.txt' \
    --script2 '$RUN_DIR/script2-08.txt' \
    --script2-after 'gotabwm: registered' \
    --input-string $'hunter2\nhunter2\namail-gate\npw-gate-7\n\n\np' \
    --input-string-after 'govault: new vault' \
    --input-chords 'g' \
    --input-chords-after 'gotabwm: secret-ask name=GOVAULT.ELF key=mail-gate' \
    --script3 '$RUN_DIR/script3-08.txt' \
    --script3-after 'govault: provision-grant mail-gate' \
    --script-expect 'gomail: secret mail-gate' --timeout 240

vgate_assert 08 serial-contains 'gotabwm: registered'
vgate_assert 08 serial-contains 'exec: loaded GOVAULT.ELF'
vgate_assert 08 serial-contains 'govault: new vault'
vgate_assert 08 serial-contains 'govault: added'
# The ask staged under the entry's own name; the seat modal named the
# asker, the key and the app-binding it would grant.
vgate_assert 08 serial-contains 'govault: provision mail-gate'
vgate_assert 08 serial-contains 'gotabwm: secret-ask name=GOVAULT.ELF key=mail-gate'
vgate_assert 08 serial-contains 'apps=GOMAIL.ELF'
vgate_assert 08 serial-contains 'gotabwm: secret grant name=GOVAULT.ELF key=mail-gate'
# The asker's own verdict read, then the consumer's slot-70 read-back in
# the same boot.
vgate_assert 08 serial-contains 'govault: provision-grant mail-gate'
vgate_assert 08 serial-contains 'exec: loaded GOMAIL.ELF'
vgate_assert 08 serial-contains 'gomail: secret mail-gate'
# The secret never reaches serial — not in the ask marshal (names only),
# not in the modal, not in a marker.
vgate_assert 08 serial-absent 'pw-gate-7'
vgate_assert 08 serial-absent '[EXC] parking:'
vgate_assert 08 serial-absent 'exited status=139'
# The receipts file carries the ask AND the verdict — the audit surface
# the card asks for.
vgate_assert 08 share-contains GOVAULT/RECEIPTS.TXT 'provision mail-gate'
vgate_assert 08 share-contains GOVAULT/RECEIPTS.TXT 'provision-grant mail-gate'
# Commit persisted: the share's SECRETS.TXT holds the app-bound row the
# kernel wrote (the host reads what the guest file ABI cannot — the
# class denial is in-guest by construction).
vgate_assert 08 python <<'PY'
import os
share = os.environ["VG_SHARE"]
data = open(os.path.join(share, "SECRETS.TXT"), "rb").read().decode("utf-8", "replace")
want = "mail-gate\t1000\tpw-gate-7\tGOMAIL.ELF"
if want not in data:
    raise SystemExit("SECRETS.TXT lacks the provisioned row %r: %r" % (want, data))
print("SECRETS.TXT persisted: mail-gate bound to GOMAIL.ELF")
PY

# --- run 09 (M196e / #3445): the acceptance journey — one boot, all legs --
# One boot where the M196 arc holds at once, built on one hard fact about
# the modal: its input-sink OWNS the keyboard while an ask stands, and the
# kernel hands focus to the terminal (focused_id=0) when a focused window
# closes — so a second 'p' typed after a grant lands on the console, not
# the vault (observed 09-01: the strokes injected fine, the app's list
# face never saw them). The journey therefore exercises ONE consent per
# boot and picks the bind arm run 08 never touched: 'p' stages the entry
# irc-gate, the seat's modal names GOVAULT.ELF + the GOIRC.ELF binding it
# would grant, 'g' commits it, and the kernel persists SECRETS.TXT.
# mail-gate needs no second consent — run 08's own provision earlier in
# the SAME iteration already persists it (the file ABI cannot delete
# SECRETS.TXT), so the mail side of the arc rides the store's
# cross-boot persistence, itself a load-bearing part of the design.
#
# The consumers read their bound rows in the same boot: GOMAIL `boxes`
# resolves mail-gate out of slot 70 ('gomail: secret mail-gate' — the run
# ends there, the dead 10.0.0.2 transport is never the verdict, run 08's
# rule); GOIRC --sasl=gate resolves the just-granted irc-gate into its own
# dial boundary — 'goirc: dial' can only print AFTER resolveSASL, an
# unprovisioned row exits at 'goirc: sasl secret_not_found' before any
# dial — bounded by --no-reconnect + --max-ms so the dead transport costs
# one refused connection. The ghost leg names the unprovisioned path:
# --user ghost steers the lookup at mail-ghost, which nothing provisioned,
# so 'gomail: refuse secret_not_found mail-ghost' lands before any socket.
#
# Iteration-safe: irc-gate is mode-new on iteration 1 and mode-replace on
# 2/3 (it persists like mail-gate), mail-gate is replace in every leg
# after run 08 — every stage gate is a mode-agnostic substring. The
# secrets stay off the wire: 'pw-gate-7' and 'sasl-gate-9' never print —
# the asks marshal names only, argv carries the row name, and SECRETS.TXT
# is the share's only copy of either value.
vgate_file script-09.txt <<'EOF'
set GOMAXPROCS=1
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm GOVAULT/VAULT.BIN
wm
exec GOTABWM.ELF
EOF

vgate_file script2-09.txt <<'EOF'
dui focus 0
exec GOVAULT.ELF
EOF

vgate_file script3-09.txt <<'EOF'
exec GOIRC.ELF --host=10.0.0.2 --nick=gate --plain --sasl=gate --no-reconnect --max-ms 15000
exec GOMAIL.ELF --tls --user ghost fetch INBOX
exec GOMAIL.ELF boxes
EOF

vgate_run 09 -- \
    --screen '$RUN_DIR/vault-screen-09' \
    --via-virtio \
    --script '$RUN_DIR/script-09.txt' \
    --script2 '$RUN_DIR/script2-09.txt' \
    --script2-after 'gotabwm: registered' \
    --input-string $'hunter2\nhunter2\nairc-gate\nsasl-gate-9\n\n\np' \
    --input-string-after 'govault: new vault' \
    --input-chords 'g' \
    --input-chords-after 'gotabwm: secret-ask name=GOVAULT.ELF key=irc-gate' \
    --script3 '$RUN_DIR/script3-09.txt' \
    --script3-after 'govault: provision-grant irc-gate' \
    --script-expect 'goirc: closed' --timeout 300

vgate_assert 09 serial-contains 'gotabwm: registered'
vgate_assert 09 serial-contains 'exec: loaded GOVAULT.ELF'
vgate_assert 09 serial-contains 'govault: new vault'
vgate_assert 09 serial-contains 'govault: added'
# The journey's own provision: the ask staged under the entry's name, the
# seat naming asker + key + the GOIRC.ELF app-binding, the 'g' grant, the
# asker's verdict read.
vgate_assert 09 serial-contains 'govault: provision irc-gate'
vgate_assert 09 serial-contains 'gotabwm: secret-ask name=GOVAULT.ELF key=irc-gate'
vgate_assert 09 serial-contains 'apps=GOIRC.ELF'
vgate_assert 09 serial-contains 'gotabwm: secret grant name=GOVAULT.ELF key=irc-gate'
vgate_assert 09 serial-contains 'govault: provision-grant irc-gate'
# mail-gate is NOT re-staged this boot — its row persists from run 08's
# consent, and the consume below reads the store, not a second ask.
vgate_assert 09 serial-absent 'govault: provision mail-gate'
# The consumers' slot-70 read-backs in the same boot: GOIRC's dial
# boundary only prints after its row resolved; GOMAIL's marker is the
# run's own end gate. The ghost names the unprovisioned path before any
# socket opens.
vgate_assert 09 serial-contains 'exec: loaded GOMAIL.ELF'
vgate_assert 09 serial-contains 'exec: loaded GOIRC.ELF'
vgate_assert 09 serial-contains 'goirc: dial 10.0.0.2 6667 plain'
vgate_assert 09 serial-contains 'goirc: closed'
vgate_assert 09 serial-contains 'gomail: refuse secret_not_found mail-ghost'
vgate_assert 09 serial-contains 'gomail: secret mail-gate'
# Neither secret value ever reaches the wire — the ask marshals names
# only, argv carries the row name, no marker echoes a value.
vgate_assert 09 serial-absent 'pw-gate-7'
vgate_assert 09 serial-absent 'sasl-gate-9'
vgate_assert 09 serial-absent 'goirc: sasl secret_not_found irc-gate'
vgate_assert 09 serial-absent 'goirc: refuse sasl-argv'
vgate_assert 09 serial-absent '[EXC] parking:'
vgate_assert 09 serial-absent 'exited status=139'
# The provision arc is ordered, not just present — stage, ask, grant,
# verdict. The consumers are NOT ordered among themselves: script3's
# execs run concurrently, so their markers interleave (observed 09-02:
# the ghost's refuse landed before GOIRC's own dial marker) — each only
# has to land after the grant, which is the gate script3 fired on.
vgate_assert 09 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], "rb").read().decode("utf-8", "replace")
prev = -1
for m in ("govault: provision irc-gate",
          "gotabwm: secret-ask name=GOVAULT.ELF key=irc-gate",
          "gotabwm: secret grant name=GOVAULT.ELF key=irc-gate",
          "govault: provision-grant irc-gate"):
    i = ser.find(m, prev + 1)
    if i < 0:
        sys.exit("journey arc broke — %r missing after position %d"
                 % (m, prev))
    prev = i
for m in ("goirc: dial 10.0.0.2 6667 plain",
          "gomail: refuse secret_not_found mail-ghost",
          "gomail: secret mail-gate"):
    if ser.find(m, prev + 1) < 0:
        sys.exit("consumer read %r missing after the grant" % m)
print("journey arc ordered: provision->consent->verdict, then 3 "
      "consumer reads after the grant")
PY
# The receipts file carries this boot's ask AND its verdict — the audit
# surface the arc demands.
vgate_assert 09 share-contains GOVAULT/RECEIPTS.TXT 'provision irc-gate'
vgate_assert 09 share-contains GOVAULT/RECEIPTS.TXT 'provision-grant irc-gate'
# Both rows persisted: irc-gate was written by THIS boot's consent,
# mail-gate by run 08's earlier in the iteration — the host reads what
# the guest file ABI cannot (the class denial is in-guest by
# construction).
vgate_assert 09 python <<'PY'
import os
share = os.environ["VG_SHARE"]
data = open(os.path.join(share, "SECRETS.TXT"), "rb").read().decode("utf-8", "replace")
for want in ("mail-gate\t1000\tpw-gate-7\tGOMAIL.ELF",
             "irc-gate\t1000\tsasl-gate-9\tGOIRC.ELF"):
    if want not in data:
        raise SystemExit("SECRETS.TXT lacks the provisioned row %r: %r"
                         % (want, data))
print("SECRETS.TXT persisted: mail-gate->GOMAIL.ELF, irc-gate->GOIRC.ELF")
PY
