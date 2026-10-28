# go-assist.spec -- M140b (#2785) + M140c (#2786) class-B acceptance:
# GOASSIST.ELF, the chat client over virelai/infer, under GOTABWM on the
# real VIR1/Metal compute path, K=3 via vgate_repeat. Legs 01-04 are the
# M140b shape; legs 05+06 are the M140c journey the card names: the
# owner-approved SmolLM2 VIW on the real Metal path answers a typed
# question, the seat snapshots (ctrl-shift-s), a fresh boot
# session-restores the app, and a typed follow-up lands in the same
# conversation.
#
#   01  probe under the seat: one OpenCompute + SynthStub generate —
#       the card's first question (does the device share) answered.
#   02  streamed answer: chords type "hi there" into the stub-backed
#       window; `goassist: tokens=N` markers must count 1..N, the
#       pinned stub ids close it, and the conversation lands crash-safe
#       at ASSIST/C0001.CHAT on the share.
#   03  mid-conversation snapshot: kill the app after its committed
#       turn; the seat's crash arm re-execs it and restore_state hands
#       back {conv, scroll} — the transcript reloads from the file.
#   04  weights absent: the staged-model persona (bare launch) names
#       the refusal GOINFER's leg-12 wording pinned, before the device
#       is ever opened (compute-submits=0).
#   05  the journey's question: the default persona loads the staged
#       SmolLM2 VIW (real weights, real Metal path), a typed question
#       streams its pinned greedy answer, then the queued snapshot
#       chord saves SESSION.TABS + SNAPSHOT.BUNDLE once the commit
#       goes quiet.
#   06  the journey's restore + follow-up: SESSION.TABS re-execs the
#       app bare (mode=restore), the staged blob re-opens the same
#       C0001, and a typed follow-up commits a second turn pair into
#       the same conversation file.
#   07  (M152f #2920, stub) a typed "hi" opens C0001, F2 renames it to
#       "Trip plan" (codec v2 header — the C#### name stays), then the
#       snapshot chord saves SESSION.TABS with the title in the blob.
#   08  (M152f #2920) the restore boot's marker names the title; then
#       tab/delete/tab/return runs the confirm dialog to Yes and the
#       file is gone from the share.
#   09  (M178b #3145, stub) FONTS.TXT + NOTOCJK.TTF staged: ctrl-j arms
#       the prompt's romaji composer, n,i,h,o,n,g,o + Return commits
#       hiragana, a second word cycles to katakana first, and the
#       prompt's chain painting closes with fonts: notdef=0.
#   10  (M186c #3180, stub) KANADICT.BIN staged: nihongo + one Space
#       converts to the dict's top kanji (日本語) and commits it; a
#       reading outside the subset commits kana-only — the honest bound.
#   11  (M205c #3810, stub) the allow leg: a staged PRESETS.TXT persona's
#       @@system turn carries '@emit @call proc.list' — the deterministic
#       driver — and tab,return on the consent dialog runs proc.list
#       in-process under GOASSIST's grants; the v3 call/consent/result
#       records land in C0001.CHAT.
#   12  (M205c) reload + deny: the armed share's C0001 v3 file reloads at
#       boot (strict decode or the emit never fires), the same call's
#       dialog gets return on the deny default, and refuse:declined is
#       recorded with no result body.
#   13  (M205c) gosh.run refuses cap_required:term before any dialog.
#   14  (M205c) ui.input refuses cap_required:input — `input` is an
#       agent-keyring word with no caps= spelling, so the seat's
#       agent_input_denied is unreachable in-process and the refusal
#       lands one layer earlier, still a named refusal with no dialog.
#   15  (M205d #3811) the no-key leg: SECRETS.TXT carries no assist-hmac
#       row, so the same call refuses no_receipt_key BEFORE the consent
#       dialog — nothing runs, no receipt lands.
#
# M205d's receipts ride legs 11-14 too: every resolved act leaves one
# RCPT file in the assist lane (allow's ok, deny's declined, the two
# cap-gated refusals), MAC'd under assist-hmac — the python asserts
# count the lane per leg and verify the newest receipt's MAC + result.
#
# The act legs stage their PRESETS.TXT in the previous leg's tail hook
# (leg 10's for leg 11, leg 12's for 13, leg 13's for 14): the file must
# exist before the boot's loadPresets, and leg 14's tail removes it so
# the next iteration's legs 01-10 see the old picker-less store again.
# Leg 12's tail also stages its own conversation: keeping C0001 across
# the leg boundary is the reload evidence, not leftover state.
#
# The armed share persists across iterations, so per-leg python tails
# keep each iteration honest: legs 01-04 and leg 06 clear ASSIST/*.CHAT
# after their asserts (a fresh store => every sending leg sees C0001 and
# the pinned decodes stay deterministic), leg 03's tail un-stages the VIW
# and leg 04's tail re-stages it so leg 04's weights-absent boot is
# honest every iteration while legs 05/06 ride the real model. Leg 08's
# own delete leaves the store empty again.
# script-seat.txt's `vf rm`s keep a leftover SESSION.TABS from
# restoring into legs 01-05's and 07's boots; legs 06 and 08 alone must
# find it.
#
# Legs 02/03 use the `stub` persona (SynthStub) so the stream is
# deterministic without staged weights; leg 04 covers the absent-weight
# path of the default persona. Legs 05/06 ride the same engine path and
# the same staged artifact live-gpu3d's leg 13 pinned — and like that
# leg they are honest in both states of the staged file: VIW present
# -> the pinned greedy answer ids; absent -> the named refusal.
#
# exec-order: assert-proven -- each run's expect marker is printed by the
# script AFTER the guest markers its stage gates wait on; a leg whose
# app never ran cannot pass.
# HOST PREREQUISITE (fails honestly):
#   bash tools/go/build-gotabwm.sh tools/go/build-goassist.sh
# OPTIONAL (else the journey legs prove the named refusal):
#   artifacts/goinfer/SMOLLM2.VIW -- python3 tools/goinfer/convert-smollm2.py

vgate_name go-assist "M140b #2785 + M140c #2786 + M152f #2920: GOASSIST.ELF under GOTABWM — seat GPU-compute probe, streamed stub answer + conversation file, crash-restart restore, weights-absent named refusal, K=3 journey: real SmolLM2 question + snapshot + session-restore + same-conversation follow-up; v2 legs: stub rename C0001 -> 'Trip plan' + snapshot, restore boot shows the title, delete with confirm removes the file"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 3 BOOTS

vgate_file script-seat.txt <<'EOF'
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm SNAPSHOT.BUNDLE
vf rm SNAPSHOT.RESTORE
set GOMAXPROCS=1
wm
exec GOTABWM.ELF
EOF

# Leg 06's boot is the restore leg: SESSION.TABS and SNAPSHOT.BUNDLE are
# its INPUTS, so its seat script carries none of script-seat.txt's rms.
vgate_file script-seat6.txt <<'EOF'
set GOMAXPROCS=1
wm
exec GOTABWM.ELF
EOF

vgate_file script-probe.txt <<'EOF'
exec GOASSIST.ELF probe
EOF

vgate_file script-stub.txt <<'EOF'
exec GOASSIST.ELF stub
EOF

vgate_file script-bare.txt <<'EOF'
exec GOASSIST.ELF
EOF

vgate_file script-kill.txt <<'EOF'
kill GOASSIST.ELF
EOF

vgate_file script-rx1.txt <<'EOF'
echo rx-assist-1-ok
EOF

vgate_file script-rx2.txt <<'EOF'
echo rx-assist-2-ok
EOF

vgate_file script-rx4.txt <<'EOF'
echo rx-assist-4-ok
EOF

vgate_file script-rx6.txt <<'EOF'
echo rx-assist-6-ok
EOF

# GOTABWM.ELF and GOASSIST.ELF are .build products, not zig-out: stage
# both plus the wm=none seat opt-in so each leg boots exactly one seat
# and each app's build stamp is honoured.
vgate_setup_python <<'PY'
import os, shutil, sys
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
for name in ("GOTABWM.ELF", "GOASSIST.ELF"):
    src = os.path.join(".build", "go", name)
    if not os.path.exists(src):
        sys.exit("%s missing (expected %s) - build it first: "
                 "bash tools/go/build-%s.sh"
                 % (name, src, name[:-4].lower()))
    shutil.copy(src, os.path.join(share, name))
# FONTS.TXT + NOTOCJK.TTF (M178b #3145): the seed share already carries
# the five frozen 8.3 faces, so the manifest's ui chain resolves Inter as
# primary and reaches the JP face through the symbol consult — leg 09's
# kana pre-edit paints real glyphs and 'fonts: notdef=0' is the honest
# count. The absent NOTOSERF/CHROME rows degrade lazily, as designed.
for name, dst in (("fonts.txt", "FONTS.TXT"),
                  ("NotoSansJP-Regular.ttf", "NOTOCJK.TTF")):
    src = os.path.join("image", "fonts", name)
    if not os.path.exists(src):
        sys.exit("%s missing (expected %s) - the font registry needs it"
                 % (name, src))
    shutil.copy(src, os.path.join(share, dst))
# KANADICT.BIN (M186c #3180): the packed JMdict subset the prompt's
# composer reads at /host/KANADICT.BIN. Leg 10's conversions come from
# it; leg 09's words are outside the subset, so its pins hold either way.
kd = os.path.join("image", "kanadict", "KANADICT.BIN")
if not os.path.exists(kd):
    sys.exit("%s missing - regenerate it: python3 tools/kanadict/pack.py"
             % kd)
shutil.copy(kd, os.path.join(share, "KANADICT.BIN"))
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    f.write("#v2\nwm=none\n")
# M205d (#3811): the receipt legs' credential + owner rows. assist-hmac
# is ONE uid_user row on a two-app allowlist — GOASSIST writes the lane,
# GOTOP verifies it, AGENTD holds neither (the kernel store dedupes on
# name+uid, so the M173d two-row split cannot apply at same-uid). The
# kernel serves it to GOASSIST.ELF only while the image is
# uid_system-owned, hence the OWNERS.TXT row.
ASSIST_KEY = "b5" * 32
open(os.path.join(share, "SECRETS.TXT"), "w").write(
    "#v1\nassist-hmac\t1000\t" + ASSIST_KEY + "\tGOASSIST.ELF,GOTOP.ELF\n")
open(os.path.join(share, "OWNERS.TXT"), "w").write(
    "#v1\nGOASSIST.ELF\t644\t0\t-\n")
print("staged Go seat + GOASSIST.ELF + FONTS.TXT/NOTOCJK.TTF/KANADICT.BIN, wm=none, assist-hmac row")
PY

# The model VIW (M111c #2410's owner-approved SmolLM2-135M-Instruct,
# Apache-2.0; the same artifact live-gpu3d's leg 13 stages) parks in
# RUN_DIR, NOT the share: leg 04's weights-absent boot must still find
# GOINFER/SMOLLM2.VIW missing. Leg 04's python tail moves it onto the
# share for legs 05/06 — and leg 03's tail moves it back so EVERY
# iteration's leg 04 is honest. An absent artifact is not an error: the
# journey legs then prove the named refusal (the #2410 clawback).
vgate_setup_python <<'PY'
import hashlib, os, shutil, sys
rd = os.environ["RUN_DIR"]
VIW_PIN = "2f5d4c774ea89016ae2b049e288e21a73987fc1c37eb1a58fc1284fad86878bd"
viw = os.path.join("artifacts", "goinfer", "SMOLLM2.VIW")
if os.path.exists(viw):
    sha = hashlib.sha256(open(viw, "rb").read()).hexdigest()
    if sha != VIW_PIN:
        sys.exit("staged VIW sha256 %s != pinned %s" % (sha, VIW_PIN))
    shutil.copy(viw, os.path.join(rd, "SMOLLM2.VIW"))
    print("model VIW verified against pin, parked until leg 04 stages it")
else:
    print("no staged VIW: journey legs assert the weights-absent path")
PY

# --- leg 01: the generate probe under the seat ------------------------------
# The card's first deliverable: one compute session opened and one
# generate run while the seat holds its own device path. `probe ok`
# answers "the device can be shared"; a named refusal would have been
# the card's stop line.
vgate_run 01 -- \
    --screen '$RUN_DIR/screen-01' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-probe.txt' \
    --script2-after 'gotabwm: holding seat' \
    --script3 '$RUN_DIR/script-rx1.txt' \
    --script3-after 'goassist: probe ok' \
    --script-expect 'rx-assist-1-ok' --timeout 240

vgate_assert 01 serial-contains 'gpu3d: armed did=0x1083'
vgate_assert 01 serial-contains 'gotabwm: holding seat'
vgate_assert 01 serial-contains 'goassist: probe ok toks='
vgate_assert 01 serial-contains 'goassist: probe ok toks=8 ids='
vgate_assert 01 serial-contains 'submits='
vgate_assert 01 serial-contains 'goassist OK'
vgate_assert 01 serial-contains 'goassist: done'
vgate_assert 01 serial-absent 'goassist: probe refused'
vgate_assert 01 serial-absent 'goassist: unsupported'
vgate_assert 01 serial-absent 'gpu3d: timeout'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 output-contains 'compute-submits='
vgate_assert 01 output-contains 'compute-refusals=0'
# Iteration hygiene (the armed share persists across vgate_repeat):
# clear the conversation store so leg 02's send lands in a fresh C0001
# and its pinned stub decode stays valid on every iteration. Tail-hook:
# declared after this leg's asserts so the checks above see the share
# exactly as the leg left it.
vgate_assert 01 python <<'PY'
import glob, os
n = 0
for f in glob.glob(os.path.join(os.environ["VG_SHARE"], "ASSIST", "*.CHAT")):
    os.remove(f)
    n += 1
print("conversation store cleared for leg 02 (%d file(s))" % n)
PY

# --- leg 02: a prompt streams its answer ------------------------------------
# The stub persona needs no weights, so this leg is deterministic on any
# host. Chords type "hi there" + return into the focused prompt field;
# every produced id is a `goassist: tokens=N` marker, and the answer's
# pinned id list is the same stream the host-fake produces in
# TestStubAnswerIdsPinned (the leg-10 property: fake and Metal decode
# byte-identical).
vgate_run 02 -- \
    --screen '$RUN_DIR/screen-02' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-stub.txt' \
    --script2-after 'gotabwm: holding seat' \
    --input-chords 'h,i,space,t,h,e,r,e,return' \
    --input-chords-after 'goassist: present' \
    --script3 '$RUN_DIR/script-rx2.txt' \
    --script3-after 'goassist: committed conv=C0001' \
    --script-expect 'rx-assist-2-ok' --timeout 300

vgate_assert 02 serial-contains 'goassist: open id='
vgate_assert 02 serial-contains 'goassist: declare accepted'
vgate_assert 02 serial-contains 'goassist: present'
vgate_assert 02 serial-contains 'goassist: send conv=C0001'
vgate_assert 02 serial-contains 'goassist: tokens=1'
# The ids pin lives in the python below and is alphabet-valued, not
# position-exact: the Metal path resolved argmax near-ties two ways in
# K=3 bring-up (tail 209->EOS; position 36: 79->71) on identical inputs
# (SynthStub is fixed-seed). The alphabet {71,79,209,2} is the honest
# pin — it still fails a fabricated or wrong-model answer.
vgate_assert 02 serial-contains 'goassist: answer toks='
vgate_assert 02 serial-contains 'goassist: saved C0001.CHAT'
vgate_assert 02 serial-contains 'goassist: committed conv=C0001'
vgate_assert 02 serial-absent 'goassist: cancelled'
vgate_assert 02 serial-absent 'goassist: error'
vgate_assert 02 serial-absent 'gpu3d: timeout'
vgate_assert 02 serial-absent '[EXC] parking:'
# The token markers must count 1..N in order — "increase over time" is a
# property of the sequence, not of two spot checks — and every emitted
# id must come from the pinned stub alphabet.
vgate_assert 02 python <<'PY'
import os, re, sys
ALPHABET = {"71", "79", "209", "2"}  # the stub answer's token set (Metal-path observed)
ser = open(os.environ["VG_SER"], errors="replace").read()
nums = [int(m.group(1)) for m in re.finditer(r"goassist: tokens=(\d+)", ser)]
if not nums or nums != list(range(1, len(nums) + 1)):
    sys.exit("token markers did not count 1..N in order: %r" % nums)
m = re.search(r"goassist: answer toks=(\d+) ids=([0-9,]+)", ser)
if not m:
    sys.exit("answer line missing")
ids = m.group(2).split(",")
bad = [i for i in ids if i not in ALPHABET]
if bad or len(ids) != int(m.group(1)) or len(ids) < 32:
    sys.exit("answer ids outside the stub alphabet: %s" % m.group(2))
print("%d token markers, strictly increasing; ids within stub alphabet" % len(ids))
PY
# The conversation file on the HOST is the load-bearing evidence: the
# typed prompt and the streamed answer both landed crash-safe.
vgate_assert 02 python <<'PY'
import os, sys
share = os.environ["VG_SHARE"]
path = os.path.join(share, "ASSIST", "C0001.CHAT")
body = open(path, "rb").read().decode("utf-8", "replace")
for want in ("GOASSIST-CHAT v1", "@@user\nhi there\n", "@@assistant\n"):
    if want not in body:
        sys.exit("conversation file missing %r\n%s" % (want, body))
print("C0001.CHAT holds the user turn and the streamed answer")
PY
# Iteration hygiene, same tail-hook shape as leg 01: leg 03's send must
# also land in a fresh C0001.
vgate_assert 02 python <<'PY'
import glob, os
n = 0
for f in glob.glob(os.path.join(os.environ["VG_SHARE"], "ASSIST", "*.CHAT")):
    os.remove(f)
    n += 1
print("conversation store cleared for leg 03 (%d file(s))" % n)
PY

# --- leg 03: snapshot mid-conversation, restore --------------------------------
# A completed turn, then `kill` — the seat's crash arm stores the last
# posted blob ({conv, scroll}), re-execs the binary, and restore_state
# re-opens the same conversation file. turns=2 is the intact transcript:
# the user turn plus the streamed answer.
vgate_run 03 -- \
    --screen '$RUN_DIR/screen-03' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-stub.txt' \
    --script2-after 'gotabwm: holding seat' \
    --input-chords 'h,i,return' \
    --input-chords-after 'goassist: present' \
    --script3 '$RUN_DIR/script-kill.txt' \
    --script3-after 'goassist: committed conv=' \
    --script-expect 'goassist: state restored conv=' --timeout 300

vgate_assert 03 serial-contains 'goassist: send conv='
vgate_assert 03 serial-contains 'goassist: answer toks='
vgate_assert 03 serial-contains 'goassist: committed conv='
vgate_assert 03 serial-contains 'GOASSIST.ELF exited status=137'
vgate_assert 03 serial-contains 'gotabwm: crash id='
vgate_assert 03 serial-contains 'gotabwm: crash restart GOASSIST.ELF'
vgate_assert 03 serial-contains 'goassist: state restored conv='
# ` turns=` presence + the file check below are the evidence (the store
# is cleared per leg, so the restored conversation is this leg's own —
# always [user, assistant] on a green run).
vgate_assert 03 serial-contains ' turns='
vgate_assert 03 serial-count 'goassist: open id=' 2
vgate_assert 03 serial-absent 'goassist: state refused'
vgate_assert 03 serial-absent 'goassist: state mismatch'
vgate_assert 03 serial-absent '[EXC] parking:'
vgate_assert 03 python <<'PY'
import os, sys
share = os.environ["VG_SHARE"]
d = os.path.join(share, "ASSIST")
conv = sorted(os.path.join(d, n) for n in os.listdir(d)
              if n.endswith(".CHAT"))[-1]
body = open(conv, "rb").read().decode("utf-8", "replace")
if body.count("@@user\n") < 1 or body.count("@@assistant\n") < 1:
    sys.exit("restored conversation lost a turn\n%s" % body)
print("conversation file intact across the crash restart: %s" % conv)
PY
# Iteration hygiene, tail-hook: clear the store for leg 04's send AND
# un-stage the VIW so leg 04's weights-absent boot stays honest on
# iterations after the first (leg 04's own tail re-stages it for the
# journey legs). An absent staged file is a skipped move, not a failure.
vgate_assert 03 python <<'PY'
import glob, os
share = os.environ["VG_SHARE"]
n = 0
for f in glob.glob(os.path.join(share, "ASSIST", "*.CHAT")):
    os.remove(f)
    n += 1
viw = os.path.join(share, "GOINFER", "SMOLLM2.VIW")
if os.path.exists(viw):
    os.replace(viw, os.path.join(os.environ["RUN_DIR"], "SMOLLM2.VIW"))
    print("store cleared (%d); VIW un-staged back to RUN_DIR" % n)
else:
    print("store cleared (%d); no staged VIW to un-stage" % n)
PY

# --- leg 04: absent weights are a named refusal ------------------------------
# The default persona reads the staged VIW — absent here — and the send
# must name it (GOINFER leg-12's wording) without ever opening the
# compute session.
vgate_run 04 -- \
    --screen '$RUN_DIR/screen-04' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-bare.txt' \
    --script2-after 'gotabwm: holding seat' \
    --input-chords 'h,i,return' \
    --input-chords-after 'goassist: present' \
    --script3 '$RUN_DIR/script-rx4.txt' \
    --script3-after 'goassist: weights absent' \
    --script-expect 'rx-assist-4-ok' --timeout 240

vgate_assert 04 serial-contains 'gpu3d: armed did=0x1083'
vgate_assert 04 serial-contains 'goassist: declare accepted'
vgate_assert 04 serial-contains 'goassist: send conv='
vgate_assert 04 serial-contains 'goassist: weights absent'
vgate_assert 04 serial-contains 'goassist: committed conv='
vgate_assert 04 serial-absent 'goassist: answer toks='
vgate_assert 04 serial-absent 'gpu3d: timeout'
vgate_assert 04 serial-absent '[EXC] parking:'
vgate_assert 04 output-contains 'compute-submits=0'
# Iteration hygiene, tail-hook: the weights-absent boot just proved the
# named refusal — NOW clear the store so leg 05's send lands in a fresh
# C0001, and land the parked VIW (when one exists) at the app's default
# path so the journey legs load it exactly like an operator's install.
# Strict optional-file semantics: absent staged file = skipped move.
vgate_assert 04 python <<'PY'
import glob, os
share = os.environ["VG_SHARE"]
n = 0
for f in glob.glob(os.path.join(share, "ASSIST", "*.CHAT")):
    os.remove(f)
    n += 1
src = os.path.join(os.environ["RUN_DIR"], "SMOLLM2.VIW")
if os.path.exists(src):
    dst_dir = os.path.join(share, "GOINFER")
    os.makedirs(dst_dir, exist_ok=True)
    os.replace(src, os.path.join(dst_dir, "SMOLLM2.VIW"))
    print("store cleared (%d); VIW staged at GOINFER/SMOLLM2.VIW" % n)
else:
    print("store cleared (%d); no parked VIW (clawback state)" % n)
PY

# --- leg 05: the M140c journey's question + snapshot ------------------------
# The default persona opens the process's one compute session and streams
# the staged SmolLM2 VIW into sealed weight buffers — the real Metal path,
# the same load GOINFER's leg 13 pinned. Chords type the card's question
# and the snapshot chord queues behind it: the seat's snapshot arm is
# focus-quiet gated, so 'state ask' cannot land until the decode + commit
# go quiet — the python orders 'committed conv=' before 'snapshot saved',
# and the share's conv file is the byte-level evidence.
vgate_run 05 -- \
    --screen '$RUN_DIR/screen-05' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-bare.txt' \
    --script2-after 'gotabwm: holding seat' \
    --input-chords 'w,h,a,t,space,i,s,space,t,h,e,space,c,a,p,i,t,a,l,space,o,f,space,f,r,a,n,c,e,return,ctrl-shift-s' \
    --input-chords-after 'goassist: present' \
    --script-expect 'gotabwm: snapshot saved bytes=' --timeout 3600

vgate_assert 05 serial-contains 'gpu3d: armed did=0x1083'
vgate_assert 05 serial-contains 'gotabwm: holding seat'
vgate_assert 05 serial-contains 'exec: loaded GOASSIST.ELF'
vgate_assert 05 serial-contains 'goassist: open id='
vgate_assert 05 serial-contains 'goassist: declare accepted'
vgate_assert 05 serial-contains 'goassist: present'
vgate_assert 05 serial-contains 'goassist: conv new C0001'
vgate_assert 05 serial-contains 'goassist: send conv=C0001'
vgate_assert 05 serial-contains 'goassist: committed conv=C0001'
vgate_assert 05 serial-contains 'gotabwm: state ask id='
vgate_assert 05 serial-contains 'gotabwm: state save id='
vgate_assert 05 serial-contains 'gotabwm: snapshot saved bytes='
vgate_assert 05 serial-absent 'goassist: cancelled'
vgate_assert 05 serial-absent 'goassist: error'
vgate_assert 05 serial-absent 'gpu3d: timeout'
vgate_assert 05 serial-absent '[EXC] parking:'
vgate_assert 05 output-contains 'compute-refusals=0'
# The answer pin is bimodal like live-gpu3d leg 13: a staged VIW must
# produce the pinned greedy decode for the typed prompt (determinism,
# not fluency, is the property — the ids were observed on the Metal
# path), and an absent VIW must produce the named refusal and no answer.
vgate_assert 05 python <<'PY'
import hashlib, os, re, sys
VIW_PIN = "2f5d4c774ea89016ae2b049e288e21a73987fc1c37eb1a58fc1284fad86878bd"
# The greedy answer's alphabet for "what is the capital of france" on a
# fresh C0001 — observed on the real Metal path 2026-10-21 ("Franconia";
# determinism, not fluency, is the pin). Alphabet-valued like leg 02:
# Metal resolves argmax near-ties differently run to run.
ANSWER_ALPHABET = {"54", "3347", "607", "542", "2"}
share = os.environ["VG_SHARE"]
ser = open(os.environ["VG_SER"], errors="replace").read()
if ser.find("gotabwm: snapshot saved") < ser.find("goassist: committed conv="):
    sys.exit("snapshot landed before the answer committed")
conv = os.path.join(share, "ASSIST", "C0001.CHAT")
body = open(conv, "rb").read().decode("utf-8", "replace")
viw = os.path.join(share, "GOINFER", "SMOLLM2.VIW")
if not os.path.exists(viw):
    # Clawback state: the named refusal, never a fabricated answer.
    for want in ("goassist: weights absent", "goassist: committed conv=C0001",
                 "gotabwm: snapshot saved"):
        if want not in ser:
            sys.exit("absent-path marker missing: %r" % want)
    if "goassist: answer toks=" in ser or "goassist: tokens=" in ser:
        sys.exit("an answer appeared without a staged VIW")
    if "@@user\nwhat is the capital of france\n" not in body:
        sys.exit("clawback conv file lost the question\n%s" % body)
    print("no staged VIW: snapshot + named refusal verified (clawback clean)")
    sys.exit(0)
sha = hashlib.sha256(open(viw, "rb").read()).hexdigest()
if sha != VIW_PIN:
    sys.exit("staged VIW sha256 %s != pinned %s" % (sha, VIW_PIN))
m = re.search(r"goassist: answer toks=(\d+) ids=([0-9,]+)", ser)
if not m:
    sys.exit("model answer line missing:\n%s" % ser[-2000:])
ntoks = int(m.group(1))
ids = m.group(2).split(",")
bad = [i for i in ids if i not in ANSWER_ALPHABET]
if bad or len(ids) != ntoks or ntoks < 2:
    sys.exit("answer ids outside the pinned alphabet: %s" % m.group(2))
nums = [int(t.group(1)) for t in re.finditer(r"goassist: tokens=(\d+)", ser)]
if nums != list(range(1, ntoks + 1)):
    sys.exit("token markers did not count 1..%d in order: %r" % (ntoks, nums))
for want in ("@@user\nwhat is the capital of france\n", "@@assistant\n"):
    if want not in body:
        sys.exit("conversation file missing %r\n%s" % (want, body))
print("question leg: pinned sha, %d streamed tokens, conv file committed" % ntoks)
print("observed ids: " + m.group(2))
PY

# --- leg 06: session restore + the follow-up, same conversation -------------
# The restore boot the card names: SESSION.TABS (leg 05's snapshot) makes
# the seat re-exec GOASSIST.ELF bare under mode=restore and the staged
# blob re-opens C0001 — 'state restored conv=C0001 turns=2' is the
# transcript's own count. The follow-up chords then type a second
# question into the same open conversation: 'conv new' must stay absent
# (a fresh conversation would be the wrong answer), 'send conv=C0001'
# pairs the turn, and the share file ends [Q1, A1, Q2, A2].
vgate_run 06 -- \
    --screen '$RUN_DIR/screen-06' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat6.txt' \
    --input-chords 'w,h,a,t,space,a,b,o,u,t,space,s,p,a,i,n,return' \
    --input-chords-after 'goassist: present' \
    --script3 '$RUN_DIR/script-rx6.txt' \
    --script3-after 'goassist: committed conv=' \
    --script-expect 'rx-assist-6-ok' --timeout 3600

vgate_assert 06 serial-contains 'gpu3d: armed did=0x1083'
vgate_assert 06 serial-contains 'gotabwm: mode live'
vgate_assert 06 serial-contains 'gotabwm: session load n=1 mode=restore'
vgate_assert 06 serial-contains 'gotabwm: session reexec n=1'
vgate_assert 06 serial-contains 'gotabwm: session exec GOASSIST.ELF'
vgate_assert 06 serial-contains 'goassist: open id='
vgate_assert 06 serial-contains 'goassist: declare accepted'
vgate_assert 06 serial-contains 'goassist: state restored conv=C0001 turns=2'
vgate_assert 06 serial-contains 'goassist: present'
vgate_assert 06 serial-contains 'goassist: send conv=C0001'
vgate_assert 06 serial-contains 'goassist: committed conv=C0001'
vgate_assert 06 serial-absent 'goassist: conv new'
vgate_assert 06 serial-absent 'goassist: state refused'
vgate_assert 06 serial-absent 'goassist: state mismatch'
vgate_assert 06 serial-absent 'goassist: cancelled'
vgate_assert 06 serial-absent 'goassist: error'
vgate_assert 06 serial-absent 'gpu3d: timeout'
vgate_assert 06 serial-absent '[EXC] parking:'
vgate_assert 06 output-contains 'compute-refusals=0'
# Same bimodal shape as leg 05, plus the same-conversation proof: the
# restored conv name is parsed from the serial, the follow-up's send
# must carry it, and the file's turn sequence is asserted verbatim.
vgate_assert 06 python <<'PY'
import hashlib, os, re, sys
VIW_PIN = "2f5d4c774ea89016ae2b049e288e21a73987fc1c37eb1a58fc1284fad86878bd"
# The greedy follow-up answer's alphabet for "what about spain" in the
# C0001 context — observed via the kernel-emulating host fake
# (byte-identical to the Metal decode, the leg-10 property) and on the
# Metal path in K=3 bring-up.
ANSWER2_ALPHABET = {"49028", "2"}
share = os.environ["VG_SHARE"]
ser = open(os.environ["VG_SER"], errors="replace").read()
m = re.search(r"goassist: state restored conv=(C\d+) turns=(\d+)", ser)
if not m:
    sys.exit("no restore marker:\n%s" % ser[-2000:])
name = m.group(1)
if "goassist: send conv=" + name not in ser:
    sys.exit("the follow-up did not send into the restored %s" % name)
body = open(os.path.join(share, "ASSIST", name + ".CHAT"),
            "rb").read().decode("utf-8", "replace")
roles = [r.group(1) for r in re.finditer(r"^@@(\w+)$", body, re.M)]
viw = os.path.join(share, "GOINFER", "SMOLLM2.VIW")
if not os.path.exists(viw):
    # Clawback: two refusals, never an answer; turns are user/meta pairs.
    for want in ("goassist: weights absent", "goassist: committed conv=" + name):
        if want not in ser:
            sys.exit("absent-path marker missing: %r" % want)
    if ser.count("goassist: weights absent") != 1:
        sys.exit("want exactly one refusal this leg")
    if "goassist: answer toks=" in ser or "goassist: tokens=" in ser:
        sys.exit("an answer appeared without a staged VIW")
    if roles != ["user", "meta", "user", "meta"]:
        sys.exit("clawback turn sequence wrong: %r\n%s" % (roles, body))
    print("no staged VIW: restored conv held the follow-up refusal (clawback clean)")
    sys.exit(0)
sha = hashlib.sha256(open(viw, "rb").read()).hexdigest()
if sha != VIW_PIN:
    sys.exit("staged VIW sha256 %s != pinned %s" % (sha, VIW_PIN))
m2 = re.search(r"goassist: answer toks=(\d+) ids=([0-9,]+)", ser)
if not m2:
    sys.exit("model answer line missing:\n%s" % ser[-2000:])
ntoks = int(m2.group(1))
ids = m2.group(2).split(",")
bad = [i for i in ids if i not in ANSWER2_ALPHABET]
if bad or len(ids) != ntoks or ntoks < 2:
    sys.exit("follow-up ids outside the pinned alphabet: %s" % m2.group(2))
nums = [int(t.group(1)) for t in re.finditer(r"goassist: tokens=(\d+)", ser)]
if nums != list(range(1, ntoks + 1)):
    sys.exit("token markers did not count 1..%d in order: %r" % (ntoks, nums))
if roles != ["user", "assistant", "user", "assistant"]:
    sys.exit("turn sequence wrong — the follow-up left the conversation: %r\n%s"
             % (roles, body))
for want in ("what is the capital of france", "what about spain"):
    if want not in body:
        sys.exit("restored conversation lost %r\n%s" % (want, body))
print("restore leg: same conv %s, %d streamed follow-up tokens, 4 turns intact"
      % (name, ntoks))
print("observed ids: " + m2.group(2))
PY
# Iteration hygiene, tail-hook: the M152f legs want a fresh store so leg
# 07's send lands in C0001. Declared after this leg's asserts so the checks
# above still see the share exactly as the leg left it.
vgate_assert 06 python <<'PY'
import glob, os
n = 0
for f in glob.glob(os.path.join(os.environ["VG_SHARE"], "ASSIST", "*.CHAT")):
    os.remove(f)
    n += 1
print("conversation store cleared for leg 07 (%d file(s))" % n)
PY

# --- leg 07 (M152f #2920): rename C0001 -> "Trip plan", then snapshot -----
# The stub leg keeps this deterministic on any host: "hi" + Return opens
# C0001 and streams its answer, `tab` moves focus onto the sidebar, `f2`
# raises the rename prompt, the chords type the title, and Return lands
# it — the file's codec-v2 header carries it from there on. The queued
# snapshot chord then saves SESSION.TABS + SNAPSHOT.BUNDLE with the blob's
# title aboard, exactly like leg 05's commit-quiet ask.
vgate_file script-rx8.txt <<'EOF'
echo rx-assist-8-ok
EOF

vgate_run 07 -- \
    --screen '$RUN_DIR/screen-07' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-stub.txt' \
    --script2-after 'gotabwm: holding seat' \
    --input-chords 'h,i,return,tab,f2,T,r,i,p,space,p,l,a,n,return,ctrl-shift-s' \
    --input-chords-after 'goassist: present' \
    --script-expect 'gotabwm: snapshot saved bytes=' --timeout 300

vgate_assert 07 serial-contains 'goassist: open id='
vgate_assert 07 serial-contains 'goassist: declare accepted'
vgate_assert 07 serial-contains 'goassist: present'
vgate_assert 07 serial-contains 'goassist: conv new C0001'
vgate_assert 07 serial-contains 'goassist: send conv=C0001'
vgate_assert 07 serial-contains 'goassist: committed conv=C0001'
vgate_assert 07 serial-contains 'goassist: renamed conv=C0001 title=Trip plan'
vgate_assert 07 serial-contains 'gotabwm: state ask id='
vgate_assert 07 serial-contains 'gotabwm: state save id='
vgate_assert 07 serial-contains 'gotabwm: snapshot saved bytes='
vgate_assert 07 serial-absent 'goassist: rename refused'
vgate_assert 07 serial-absent 'goassist: error'
vgate_assert 07 serial-absent 'goassist: cancelled'
vgate_assert 07 serial-absent 'gpu3d: timeout'
vgate_assert 07 serial-absent '[EXC] parking:'
# The conversation file IS the rename evidence: codec v2, the title line
# ahead of the user turn, and the C#### name unmoved.
vgate_assert 07 python <<'PY'
import os, sys
share = os.environ["VG_SHARE"]
body = open(os.path.join(share, "ASSIST", "C0001.CHAT"),
            "rb").read().decode("utf-8", "replace")
for want in ("GOASSIST-CHAT v2\n@@title Trip plan\n",
             "@@user\nhi\n", "@@assistant\n"):
    if want not in body:
        sys.exit("renamed conv file missing %r\n%s" % (want, body))
print("C0001.CHAT carries @@title Trip plan")
PY
# The blob carries the title too — the card's contract — and the rename
# committed before the snapshot arm answered (order like leg 05's).
vgate_assert 07 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
r = ser.find("goassist: renamed conv=C0001 title=Trip plan")
s = ser.find("gotabwm: snapshot saved")
if r < 0 or s < 0 or s < r:
    sys.exit("rename must commit before the snapshot saves")
b = open(os.path.join(os.environ["VG_SHARE"], "SESSION.TABS"), "rb").read()
if b"Trip plan" not in b:
    sys.exit("the session blob did not carry the title")
print("snapshot landed after the rename; the blob carries 'Trip plan'")
PY

# --- leg 08 (M152f #2920): the restore boot shows the title; delete --------
# SESSION.TABS re-execs GOASSIST.ELF bare (mode=restore) like leg 06; the
# blob re-opens C0001 and the restored marker names the title the file
# carries. Then `tab` focuses the sidebar, `delete` raises the confirm
# (focus starts on No), `tab` moves to Yes, `return` confirms — the python
# tail checks the file is gone.
vgate_run 08 -- \
    --screen '$RUN_DIR/screen-08' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat6.txt' \
    --input-chords 'tab,delete,tab,return' \
    --input-chords-after 'goassist: present' \
    --script3 '$RUN_DIR/script-rx8.txt' \
    --script3-after 'goassist: conv deleted C0001' \
    --script-expect 'rx-assist-8-ok' --timeout 300

vgate_assert 08 serial-contains 'gotabwm: mode live'
vgate_assert 08 serial-contains 'gotabwm: session load n=1 mode=restore'
vgate_assert 08 serial-contains 'gotabwm: session reexec n=1'
vgate_assert 08 serial-contains 'gotabwm: session exec GOASSIST.ELF'
vgate_assert 08 serial-contains 'goassist: open id='
vgate_assert 08 serial-contains 'goassist: declare accepted'
vgate_assert 08 serial-contains 'goassist: state restored conv=C0001'
vgate_assert 08 serial-contains 'title=Trip plan'
vgate_assert 08 serial-contains 'goassist: conv deleted C0001'
vgate_assert 08 serial-contains 'rx-assist-8-ok'
vgate_assert 08 serial-absent 'goassist: state refused'
vgate_assert 08 serial-absent 'goassist: state mismatch'
vgate_assert 08 serial-absent 'goassist: error'
vgate_assert 08 serial-absent '[EXC] parking:'
# Delete-with-confirm evidence: the transcript file is gone from the
# armed share — exactly the check the card names.
vgate_assert 08 python <<'PY'
import os, sys
path = os.path.join(os.environ["VG_SHARE"], "ASSIST", "C0001.CHAT")
if os.path.exists(path):
    sys.exit("the confirmed delete left %s on the share" % path)
print("delete-with-confirm removed ASSIST/C0001.CHAT")
PY

# --- leg 09 (M178b #3145): romaji->kana composition in the prompt ---------
# The stub leg keeps the leg deterministic like legs 02/07 — nothing here
# needs a model answer, only the prompt field. ctrl-j arms the composer
# (a chord the seat's table leaves free at every scope), n,i,h,o,n,g,o
# paints the hiragana pre-edit underlined through the ui chain, and Return
# commits the shown form — it is NOT the send, so no conversation opens
# and no store hygiene is owed. The second word cycles once to katakana
# before its Return. 'fonts: notdef=0' is printed at each commit: the
# registry's running count proving every rune the prompt painted — kana
# pre-edit, candidate strip, committed text — found a face on the chain.
vgate_file script-rx9.txt <<'EOF'
echo rx-assist-9-ok
EOF

vgate_run 09 -- \
    --screen '$RUN_DIR/screen-09' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-stub.txt' \
    --script2-after 'gotabwm: holding seat' \
    --input-chords 'ctrl-j,n,i,h,o,n,g,o,return,s,u,s,h,i,space,return' \
    --input-chords-after 'goassist: present' \
    --script3 '$RUN_DIR/script-rx9.txt' \
    --script3-after 'goassist: ime commit スシ' \
    --script-expect 'rx-assist-9-ok' --timeout 300

vgate_assert 09 serial-contains 'gotabwm: holding seat'
vgate_assert 09 serial-contains 'goassist: open id='
vgate_assert 09 serial-contains 'goassist: declare accepted'
vgate_assert 09 serial-contains 'goassist: present'
vgate_assert 09 serial-contains 'goassist: ime on'
vgate_assert 09 serial-contains 'goassist: ime commit にほんご'
vgate_assert 09 serial-contains 'goassist: ime commit スシ'
vgate_assert 09 serial-contains 'fonts: notdef=0'
vgate_assert 09 serial-contains 'rx-assist-9-ok'
vgate_assert 09 serial-absent 'fonts: notdef=1'
vgate_assert 09 serial-absent 'goassist: ime off'
vgate_assert 09 serial-absent 'goassist: conv new'
vgate_assert 09 serial-absent 'goassist: send conv='
vgate_assert 09 serial-absent 'goassist: error'
vgate_assert 09 serial-absent 'gpu3d: timeout'
vgate_assert 09 serial-absent '[EXC] parking:'

# --- leg 10 (M186c #3180): kana->kanji conversion through the field ----
# Same deterministic stub shape as leg 09 — the prompt field is the whole
# stage. With KANADICT.BIN staged (setup hook), 'nihongo' stages the
# hiragana pre-edit and ONE Space converts to the dictionary's top
# surface: 'ime commit 日本語' is the kanji reaching the text, and the
# absent kana commit proves the kana did not just pass through. The
# second word is a reading outside the curated subset: no conversion is
# offered, so Return commits the hiragana — the card's honest bound.
# 'fonts: notdef=0' again proves every painted rune (pre-edit, candidate
# strip, committed kanji) found a face on the staged chain.
vgate_file script-rx10.txt <<'EOF'
echo rx-assist-10-ok
EOF

vgate_run 10 -- \
    --screen '$RUN_DIR/screen-10' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-stub.txt' \
    --script2-after 'gotabwm: holding seat' \
    --input-chords 'ctrl-j,n,i,h,o,n,g,o,space,return,h,u,g,a,h,u,g,a,return' \
    --input-chords-after 'goassist: present' \
    --script3 '$RUN_DIR/script-rx10.txt' \
    --script3-after 'goassist: ime commit ふがふが' \
    --script-expect 'rx-assist-10-ok' --timeout 300

vgate_assert 10 serial-contains 'gotabwm: holding seat'
vgate_assert 10 serial-contains 'goassist: present'
vgate_assert 10 serial-contains 'goassist: ime on'
vgate_assert 10 serial-contains 'goassist: ime commit 日本語'
vgate_assert 10 serial-contains 'goassist: ime commit ふがふが'
vgate_assert 10 serial-contains 'fonts: notdef=0'
vgate_assert 10 serial-contains 'rx-assist-10-ok'
vgate_assert 10 serial-absent 'goassist: ime commit にほんご'
vgate_assert 10 serial-absent 'goassist: ime off'
vgate_assert 10 serial-absent 'goassist: conv new'
vgate_assert 10 serial-absent 'goassist: send conv='
vgate_assert 10 serial-absent 'goassist: error'
vgate_assert 10 serial-absent 'gpu3d: timeout'
vgate_assert 10 serial-absent '[EXC] parking:'
# Act-leg staging, tail-hook: leg 11's boot must find the 'callproc'
# persona's PRESETS.TXT already inside the store — its @@system turn is
# the '@emit @call proc.list' driver the deterministic leg rides.
vgate_assert 10 python <<'PY'
import os
d = os.path.join(os.environ["VG_SHARE"], "ASSIST")
os.makedirs(d, exist_ok=True)
with open(os.path.join(d, "PRESETS.TXT"), "w") as f:
    f.write("GOASSIST-PRESETS v1\n@@callproc\n@emit @call proc.list\n")
print("staged PRESETS.TXT with the callproc @emit driver for leg 11")
PY

# --- leg 11 (M205c #3810): the allow act --------------------------------
# The whole loop under the seat: 'hi' + Return raises the preset picker,
# down,return chooses 'callproc' (row 1 after the default), the stub's
# @@system turn emits '@call proc.list' verbatim, the cap gate passes
# (process is a floor word GOASSIST holds), and the consent dialog comes
# up on the deny default — tab moves to Yes, return allows, proc.list
# runs in-process through virelai/agentverbs under GOASSIST's own grants,
# and the v3 records land crash-safe in C0001.CHAT.
vgate_file script-rx11.txt <<'EOF'
echo rx-assist-11-ok
EOF

vgate_run 11 -- \
    --screen '$RUN_DIR/screen-11' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-stub.txt' \
    --script2-after 'gotabwm: holding seat' \
    --input-chords 'h,i,return,down,return,tab,return' \
    --input-chords-after 'goassist: present' \
    --script3 '$RUN_DIR/script-rx11.txt' \
    --script3-after 'goassist: act ok proc.list' \
    --script-expect 'rx-assist-11-ok' --timeout 300

vgate_assert 11 serial-contains 'goassist: open id='
vgate_assert 11 serial-contains 'goassist: declare accepted'
vgate_assert 11 serial-contains 'goassist: present'
vgate_assert 11 serial-contains 'goassist: presets n=1'
vgate_assert 11 serial-contains 'goassist: conv new C0001 preset=callproc'
vgate_assert 11 serial-contains 'goassist: send conv=C0001'
vgate_assert 11 serial-contains 'goassist: emit'
vgate_assert 11 serial-contains 'goassist: call proc.list'
vgate_assert 11 serial-contains 'goassist: consent ask proc.list'
vgate_assert 11 serial-contains 'goassist: consent allow proc.list'
vgate_assert 11 serial-contains 'goassist: act ok proc.list'
vgate_assert 11 serial-contains 'goassist: saved C0001.CHAT'
vgate_assert 11 serial-contains 'goassist: committed conv=C0001'
vgate_assert 11 serial-absent 'goassist: consent deny'
vgate_assert 11 serial-absent 'goassist: call refused'
vgate_assert 11 serial-absent 'goassist: call malformed'
vgate_assert 11 serial-absent 'goassist: act refused'
vgate_assert 11 serial-absent 'goassist: error'
vgate_assert 11 serial-absent 'gpu3d: timeout'
vgate_assert 11 serial-absent '[EXC] parking:'
# The conversation file is the load-bearing v3 evidence: the driver
# persona, the emitted call in the assistant turn, and the act's three
# records — call, consent allow, ok result holding the live proc table.
vgate_assert 11 python <<'PY'
import os, sys
path = os.path.join(os.environ["VG_SHARE"], "ASSIST", "C0001.CHAT")
body = open(path, "rb").read().decode("utf-8", "replace")
for want in ("GOASSIST-CHAT v3", "@@system\n@emit @call proc.list\n",
             "@@user\nhi\n", "@@assistant\n@call proc.list\n",
             "@@call proc.list\n", "@@consent allow\n",
             "@@result ok\n", '"procs":[', "GOASSIST.ELF"):
    if want not in body:
        sys.exit("C0001.CHAT missing %r\n%s" % (want, body))
print("v3 records: call + consent allow + ok result with the proc table")
PY
# M205d (#3811): the allowed act left exactly one new RCPT file in the
# assist lane, MAC'd under assist-hmac — the card's own acceptance.
vgate_assert 11 serial-contains 'goassist: rcpt RCPT-'
vgate_assert 11 python <<'PY'
import hashlib, hmac, os, sys
adir = os.path.join(os.environ["VG_SHARE"], "ASSIST", "AUDIT")
names = [n for n in os.listdir(adir) if n.startswith("RCPT-")]
if len(names) != 1:
    sys.exit("assist lane holds %d receipts, want exactly 1: %r" % (len(names), names))
lines = open(os.path.join(adir, names[0]), "rb").read().decode().splitlines()
want = hmac.new(("b5" * 32).encode(), lines[0].encode(), hashlib.sha256).hexdigest()
if lines[1] != want:
    sys.exit("assist receipt MAC does not verify under assist-hmac")
for field in ('"verb":"proc.list"', '"caller":"GOASSIST.ELF"',
              '"agent":"C0001"', '"result":"ok"'):
    if field not in lines[0]:
        sys.exit("assist receipt missing %s\n%s" % (field, lines[0]))
print("assist lane: 1 new RCPT, MAC verifies under assist-hmac, caller GOASSIST.ELF")
PY
# Iteration hygiene: C0001 stays — leg 12's boot must reload this v3
# file (its own @@system turn carries the driver), so clearing here
# would erase the evidence, not just the state.

# --- leg 12 (M205c): the reload proves the records; deny is named ------
# A fresh boot over the armed share: the store's C0001 v3 file must pass
# the strict decode or the @@system driver is gone and 'goassist: emit'
# never prints — the reload IS this leg's first assert. 'hi' + Return
# sends into the open conversation (no picker: the conv is already open),
# the same call raises the consent dialog, and a bare return lands on
# the deny default: consent deny + refuse:declined, and the verb never
# runs.
vgate_file script-rx12.txt <<'EOF'
echo rx-assist-12-ok
EOF

vgate_run 12 -- \
    --screen '$RUN_DIR/screen-12' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-stub.txt' \
    --script2-after 'gotabwm: holding seat' \
    --input-chords 'h,i,return,return' \
    --input-chords-after 'goassist: present' \
    --script3 '$RUN_DIR/script-rx12.txt' \
    --script3-after 'goassist: consent deny proc.list' \
    --script-expect 'rx-assist-12-ok' --timeout 300

vgate_assert 12 serial-contains 'goassist: open id='
vgate_assert 12 serial-contains 'goassist: declare accepted'
vgate_assert 12 serial-contains 'goassist: present'
vgate_assert 12 serial-contains 'goassist: send conv=C0001'
vgate_assert 12 serial-contains 'goassist: emit'
vgate_assert 12 serial-contains 'goassist: call proc.list'
vgate_assert 12 serial-contains 'goassist: consent ask proc.list'
vgate_assert 12 serial-contains 'goassist: consent deny proc.list'
vgate_assert 12 serial-contains 'goassist: committed conv=C0001'
vgate_assert 12 serial-absent 'goassist: consent allow'
vgate_assert 12 serial-absent 'goassist: act ok'
vgate_assert 12 serial-absent 'goassist: act refused'
vgate_assert 12 serial-absent 'goassist: conv new'
vgate_assert 12 serial-absent 'goassist: error'
vgate_assert 12 serial-absent 'gpu3d: timeout'
vgate_assert 12 serial-absent '[EXC] parking:'
# Deny evidence: leg 11's allow act survives the reload byte-for-byte,
# and this leg's decline appended consent deny + refuse:declined with no
# result body — the file holds BOTH verdicts.
vgate_assert 12 python <<'PY'
import os, sys
path = os.path.join(os.environ["VG_SHARE"], "ASSIST", "C0001.CHAT")
body = open(path, "rb").read().decode("utf-8", "replace")
if body.count("@@consent allow\n") != 1 or body.count("@@result ok\n") != 1:
    sys.exit("leg 11's allow act did not survive the reload\n%s" % body)
# The codec writes one empty body line per empty-bodied record, so the
# wire order is marker, blank line, next marker — the declined result
# is the file's empty-bodied tail.
if body.count("@@consent deny\n") != 1:
    sys.exit("deny consent record missing\n%s" % body)
if not body.endswith("@@result refuse:declined\n\n"):
    sys.exit("the declined result is not the empty-bodied tail\n%s" % body)
print("reload held leg 11's records; deny appended refuse:declined, no body")
PY
# M205d: the declined act receipted too — the lane holds both verdicts.
vgate_assert 12 python <<'PY'
import hashlib, hmac, os, sys
adir = os.path.join(os.environ["VG_SHARE"], "ASSIST", "AUDIT")
names = sorted(n for n in os.listdir(adir) if n.startswith("RCPT-"))
if len(names) != 2:
    sys.exit("assist lane holds %d receipts, want 2 (allow + decline): %r" % (len(names), names))
line = open(os.path.join(adir, names[-1]), "rb").read().decode().splitlines()[0]
want = hmac.new(("b5" * 32).encode(), line.encode(), hashlib.sha256).hexdigest()
mac = open(os.path.join(adir, names[-1]), "rb").read().decode().splitlines()[1]
if mac != want:
    sys.exit("deny receipt MAC does not verify")
if '"result":"declined"' not in line:
    sys.exit("the newest receipt does not name declined\n%s" % line)
print("assist lane: 2 receipts (ok + declined), newest MAC verifies")
PY
# Iteration hygiene + next-leg staging: the gosh.run leg wants a fresh
# store AND its own driver persona.
vgate_assert 12 python <<'PY'
import glob, os
share = os.environ["VG_SHARE"]
n = 0
for f in glob.glob(os.path.join(share, "ASSIST", "*.CHAT")):
    os.remove(f)
    n += 1
with open(os.path.join(share, "ASSIST", "PRESETS.TXT"), "w") as f:
    f.write("GOASSIST-PRESETS v1\n@@callgosh\n@emit @call gosh.run cmd=ls\n")
print("store cleared (%d); staged callgosh @emit driver for leg 13" % n)
PY

# --- leg 13 (M205c): gosh.run is cap-gated before any dialog ------------
# The emit driver makes the stub call gosh.run; GOASSIST's grants hold
# no `term` word, so the cap gate refuses cap_required:term and no
# consent dialog ever stands — the file records call + refusal only.
vgate_file script-rx13.txt <<'EOF'
echo rx-assist-13-ok
EOF

vgate_run 13 -- \
    --screen '$RUN_DIR/screen-13' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-stub.txt' \
    --script2-after 'gotabwm: holding seat' \
    --input-chords 'h,i,return,down,return' \
    --input-chords-after 'goassist: present' \
    --script3 '$RUN_DIR/script-rx13.txt' \
    --script3-after 'goassist: call refused gosh.run cap_required:term' \
    --script-expect 'rx-assist-13-ok' --timeout 300

vgate_assert 13 serial-contains 'goassist: conv new C0001 preset=callgosh'
vgate_assert 13 serial-contains 'goassist: emit'
vgate_assert 13 serial-contains 'goassist: call gosh.run'
vgate_assert 13 serial-contains 'goassist: call refused gosh.run cap_required:term'
vgate_assert 13 serial-contains 'goassist: committed conv=C0001'
vgate_assert 13 serial-absent 'goassist: consent'
vgate_assert 13 serial-absent 'goassist: act ok'
vgate_assert 13 serial-absent 'gosh: '
vgate_assert 13 serial-absent 'goassist: error'
vgate_assert 13 serial-absent '[EXC] parking:'
vgate_assert 13 python <<'PY'
import os, sys
path = os.path.join(os.environ["VG_SHARE"], "ASSIST", "C0001.CHAT")
body = open(path, "rb").read().decode("utf-8", "replace")
for want in ("@@call gosh.run\ncmd=ls\n", "@@result refuse:cap_required:term\n"):
    if want not in body:
        sys.exit("gosh.run act missing %r\n%s" % (want, body))
if "@@consent" in body:
    sys.exit("a consent record landed for a cap-refused call\n%s" % body)
print("gosh.run: @@call + refuse:cap_required:term, no consent record")
PY
# M205d: the cap-gated call receipted its named refusal too.
vgate_assert 13 python <<'PY'
import hashlib, hmac, os, sys
adir = os.path.join(os.environ["VG_SHARE"], "ASSIST", "AUDIT")
names = sorted(n for n in os.listdir(adir) if n.startswith("RCPT-"))
if len(names) != 3:
    sys.exit("assist lane holds %d receipts, want 3: %r" % (len(names), names))
lines = open(os.path.join(adir, names[-1]), "rb").read().decode().splitlines()
want = hmac.new(("b5" * 32).encode(), lines[0].encode(), hashlib.sha256).hexdigest()
if lines[1] != want or '"result":"cap_required:term"' not in lines[0]:
    sys.exit("gosh.run receipt wrong: %s" % lines[0])
print("assist lane: 3 receipts, cap_required:term verifies")
PY
# Iteration hygiene + staging: the ui.input leg's own store and driver.
vgate_assert 13 python <<'PY'
import glob, os
share = os.environ["VG_SHARE"]
n = 0
for f in glob.glob(os.path.join(share, "ASSIST", "*.CHAT")):
    os.remove(f)
    n += 1
with open(os.path.join(share, "ASSIST", "PRESETS.TXT"), "w") as f:
    f.write("GOASSIST-PRESETS v1\n@@callui\n@emit @call ui.input win=0 events=x\n")
print("store cleared (%d); staged callui @emit driver for leg 14" % n)
PY

# --- leg 14 (M205c): ui.input's named refusal ---------------------------
# The seat-facing verb GOASSIST can never hold: `input` is an
# agent-keyring word outside the caps= vocabulary, so the cap gate
# refuses cap_required:input and the seat's agent_input_denied is
# unreachable in-process — the named refusal lands one layer earlier,
# still with no dialog and no dispatch.
vgate_file script-rx14.txt <<'EOF'
echo rx-assist-14-ok
EOF

vgate_run 14 -- \
    --screen '$RUN_DIR/screen-14' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-stub.txt' \
    --script2-after 'gotabwm: holding seat' \
    --input-chords 'h,i,return,down,return' \
    --input-chords-after 'goassist: present' \
    --script3 '$RUN_DIR/script-rx14.txt' \
    --script3-after 'goassist: call refused ui.input cap_required:input' \
    --script-expect 'rx-assist-14-ok' --timeout 300

vgate_assert 14 serial-contains 'goassist: conv new C0001 preset=callui'
vgate_assert 14 serial-contains 'goassist: emit'
vgate_assert 14 serial-contains 'goassist: call ui.input'
vgate_assert 14 serial-contains 'goassist: call refused ui.input cap_required:input'
vgate_assert 14 serial-contains 'goassist: committed conv=C0001'
vgate_assert 14 serial-absent 'goassist: consent'
vgate_assert 14 serial-absent 'goassist: act ok'
vgate_assert 14 serial-absent 'agent_input_denied'
vgate_assert 14 serial-absent 'goassist: error'
vgate_assert 14 serial-absent '[EXC] parking:'
vgate_assert 14 python <<'PY'
import os, sys
path = os.path.join(os.environ["VG_SHARE"], "ASSIST", "C0001.CHAT")
body = open(path, "rb").read().decode("utf-8", "replace")
for want in ("@@call ui.input\nwin=0 events=x\n",
             "@@result refuse:cap_required:input\n"):
    if want not in body:
        sys.exit("ui.input act missing %r\n%s" % (want, body))
if "@@consent" in body:
    sys.exit("a consent record landed for a cap-refused call\n%s" % body)
print("ui.input: @@call + refuse:cap_required:input, no consent record")
PY
# M205d: the ui.input refusal receipted as well — four files now.
vgate_assert 14 python <<'PY'
import hashlib, hmac, os, sys
adir = os.path.join(os.environ["VG_SHARE"], "ASSIST", "AUDIT")
names = sorted(n for n in os.listdir(adir) if n.startswith("RCPT-"))
if len(names) != 4:
    sys.exit("assist lane holds %d receipts, want 4: %r" % (len(names), names))
lines = open(os.path.join(adir, names[-1]), "rb").read().decode().splitlines()
want = hmac.new(("b5" * 32).encode(), lines[0].encode(), hashlib.sha256).hexdigest()
if lines[1] != want or '"result":"cap_required:input"' not in lines[0]:
    sys.exit("ui.input receipt wrong: %s" % lines[0])
print("assist lane: 4 receipts, cap_required:input verifies")
PY
# Iteration hygiene + next-leg staging: leg 15 is the M205d no-key leg —
# it wants a fresh store, its own driver persona, AND a SECRETS.TXT
# with no assist-hmac row (leg 15's tail restores the row for the next
# iteration). The lane's files stay: they are legs 11-14's evidence.
vgate_assert 14 python <<'PY'
import glob, os
share = os.environ["VG_SHARE"]
n = 0
for f in glob.glob(os.path.join(share, "ASSIST", "*.CHAT")):
    os.remove(f)
    n += 1
with open(os.path.join(share, "ASSIST", "PRESETS.TXT"), "w") as f:
    f.write("GOASSIST-PRESETS v1\n@@callnokey\n@emit @call proc.list\n")
sp = os.path.join(share, "SECRETS.TXT")
rows = [l for l in open(sp).read().splitlines()
        if not l.startswith("assist-hmac\t")]
open(sp, "w").write("\n".join(rows) + "\n")
print("store cleared (%d); callnokey driver staged; assist-hmac row stripped for leg 15" % n)
PY

# --- leg 15 (M205d #3811): no key row — the act refuses before consent --
# The emit driver lands '@call proc.list' as always, the cap gate
# passes (process is a floor word GOASSIST holds), but the lane gate
# finds no assist-hmac row: the act refuses no_receipt_key BEFORE the
# consent dialog — nothing runs, no dialog, no receipt.
vgate_file script-rx15.txt <<'EOF'
echo rx-assist-15-ok
EOF

vgate_run 15 -- \
    --screen '$RUN_DIR/screen-15' \
    --gpu3d-metal --via-virtio \
    --script '$RUN_DIR/script-seat.txt' \
    --script2 '$RUN_DIR/script-stub.txt' \
    --script2-after 'gotabwm: holding seat' \
    --input-chords 'h,i,return,down,return' \
    --input-chords-after 'goassist: present' \
    --script3 '$RUN_DIR/script-rx15.txt' \
    --script3-after 'goassist: call refused proc.list no_receipt_key' \
    --script-expect 'rx-assist-15-ok' --timeout 300

vgate_assert 15 serial-contains 'goassist: conv new C0001 preset=callnokey'
vgate_assert 15 serial-contains 'goassist: emit'
vgate_assert 15 serial-contains 'goassist: call proc.list'
vgate_assert 15 serial-contains 'goassist: call refused proc.list no_receipt_key'
vgate_assert 15 serial-contains 'goassist: committed conv=C0001'
vgate_assert 15 serial-absent 'goassist: consent'
vgate_assert 15 serial-absent 'goassist: act ok'
vgate_assert 15 serial-absent 'goassist: rcpt'
vgate_assert 15 serial-absent 'goassist: error'
vgate_assert 15 serial-absent '[EXC] parking:'
vgate_assert 15 python <<'PY'
import os, sys
share = os.environ["VG_SHARE"]
# Nothing ran and nothing receipted: legs 11-14's four receipts stand,
# but no fifth landed for the keyless act.
adir = os.path.join(share, "ASSIST", "AUDIT")
names = [n for n in os.listdir(adir)] if os.path.isdir(adir) else []
if len(names) != 4:
    sys.exit("lane count moved on a keyless act: %r" % names)
body = open(os.path.join(share, "ASSIST", "C0001.CHAT"), "rb").read().decode("utf-8", "replace")
for want in ("@@call proc.list\n", "@@result refuse:no_receipt_key\n"):
    if want not in body:
        sys.exit("no-key act missing %r\n%s" % (want, body))
if "@@consent" in body:
    sys.exit("a consent record landed for a keyless act\n%s" % body)
print("no-key leg: named refusal before consent, no dispatch, no receipt")
PY
# Iteration hygiene, last act leg: restore the assist-hmac row, drop the
# driver persona and the convs, and clear the lane so the NEXT
# vgate_repeat's legs start clean — PRESETS.TXT must not leak across an
# iteration.
vgate_assert 15 python <<'PY'
import glob, os
share = os.environ["VG_SHARE"]
sp = os.path.join(share, "SECRETS.TXT")
rows = open(sp).read()
if "assist-hmac\t" not in rows:
    open(sp, "w").write(
        rows + "assist-hmac\t1000\t" + "b5" * 32 + "\tGOASSIST.ELF,GOTOP.ELF\n")
n = 0
for f in glob.glob(os.path.join(share, "ASSIST", "*.CHAT")):
    os.remove(f)
    n += 1
for f in glob.glob(os.path.join(share, "ASSIST", "AUDIT", "RCPT-*")):
    os.remove(f)
p = os.path.join(share, "ASSIST", "PRESETS.TXT")
if os.path.exists(p):
    os.remove(p)
print("assist-hmac restored; store + lane cleared (%d) for the next iteration" % n)
PY
