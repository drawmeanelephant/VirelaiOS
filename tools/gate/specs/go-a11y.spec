# go-a11y.spec — GOA11Y.ELF (M197b #3382)
# class-B gate: the reader's FIRST owning spec. Until now GOA11Y was
# exercised only incidentally (go-wm-journey run 03 audits its outline
# blocks as a sidecar of the seat journey); this leg pins the card's
# whole claim in one run under the live Go seat:
#
#   boot (wm=none) -> exec GOTABWM -> hostint arms lanes=speech ->
#   exec GOA11Y -announce -say -> exec GODEMO -> the M177c key drive ->
#   every focus move narrates in order AND rides the speech lane.
#
# The narration verbs (M197b): -announce prints `goa11y: announce <text>`
# for each new utterance — the focused node's name — deduped on
# (win, utterance) so a live-region rename re-announces and a repeated
# utterance does not; -say stages the same utterance as a SAY intent
# marker the seat signs into a SPEAK record (M186d, ADR 0051 V2-D8) and
# --host-speech hands it to NSSpeechSynthesizer. The receipts chain:
#   goa11y: announce  ->  goa11y: say queued  ->  gotabwm: hostint speak
#   ->  host-speech: speak (runner stdout, the decode -> synth hand-off)
#
# Exec order inside script2 is load-bearing (go-wm-journey run 03's
# rule): GOA11Y execs first so GODEMO's declare + self-raise is the last
# focus event the seat sees — a late GOA11Y declare would steal the
# keyboard and the chords would land on a window that ignores input by
# contract.
#
# The drive is run 03's key sequence verbatim through Return: escape,
# F10, down/right/right (File open -> Edit -> View), e (&Expand All
# mnemonic), tab (the ring hands the table the keys), down/down/right
# (cell r=2 c=1), return. The end marker rides script3's echo, gated on
# `goa11y: announce row 3 of 64` — the final outline's utterance — so
# every announce and every speak receipt the drive produced is already
# in serial when the run stops.
#
# HOST PREREQUISITES (fail the gate honestly when missing):
#   bash tools/go/build-gotabwm.sh     ->  .build/go/GOTABWM.ELF
#   bash tools/go/build-goa11y.sh      ->  .build/go/GOA11Y.ELF
#   bash tools/go/build-appkitdemo.sh  ->  .build/go/GODEMO.ELF

vgate_name go-a11y "M197b #3382: GOA11Y narration verbs — announce order + speak path over VHI1, first owning gate"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script-01.txt <<'EOF'
exec GOTABWM.ELF
EOF

vgate_file script2-01.txt <<'EOF'
dui focus 0
exec GOA11Y.ELF -announce -say
exec GODEMO.ELF
EOF

vgate_file script3-01.txt <<'EOF'
echo rx-go-a11y-done
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
share = os.environ.get("VG_SHARE") or os.path.join(os.environ["RUN_DIR"], "share")
for name, tool in (("GOTABWM.ELF", "build-gotabwm.sh"),
                   ("GOA11Y.ELF", "build-goa11y.sh"),
                   ("GODEMO.ELF", "build-appkitdemo.sh")):
    src = os.path.join(".build", "go", name)
    if not os.path.exists(src):
        sys.exit("%s missing (expected %s) - build it first: "
                 "bash tools/go/%s" % (name, src, tool))
    shutil.copy(src, os.path.join(share, name))
    print("staged %s (%d bytes)" % (name, os.path.getsize(src)))
# GODEMO exits 3 without its faces; the seat degrades cleanly but the
# demo does not.
for src, dst in (("Inter-Regular.ttf", "INTER.TTF"),
                 ("VirelaiChrome-Regular.ttf", "CHROME.TTF")):
    p = os.path.join("image", "fonts", src)
    if not os.path.isfile(p):
        sys.exit("font %s missing" % p)
    shutil.copy(p, os.path.join(share, dst))
print("staged INTER.TTF + CHROME.TTF")
# wm=none: the seat is exec'd on script, like the per-lane specs.
open(os.path.join(share, "SETTINGS.TXT"), "w").write("#v2\nwm=none\n")
# The lane credential --host-speech requires (the row the seat reads via
# sys_secret_get; the image binding needs the uid_system OWNERS row).
KEY = "m197bkey" * 8  # 64 bytes, ASCII
open(os.path.join(share, "SECRETS.TXT"), "w").write(
    "#v1\n" + "host-int\t1000\t" + KEY + "\tGOTABWM.ELF\n")
open(os.path.join(share, "OWNERS.TXT"), "w").write(
    "#v1\nGOTABWM.ELF\t644\t0\t-\n")
print("staged SETTINGS.TXT (wm=none) + host-int credential for GOTABWM.ELF")
PY

vgate_run 01 -- \
    --screen '$RUN_DIR/screen-01' \
    --via-virtio \
    --host-speech \
    --script '$RUN_DIR/script-01.txt' \
    --script2 '$RUN_DIR/script2-01.txt' \
    --script2-after 'gotabwm: hostint armed' \
    --input-chords 'escape,f10,down,right,right,e,tab,down,down,right,return' \
    --input-chords-after 'godialog: painted' \
    --script3 '$RUN_DIR/script3-01.txt' \
    --script3-after 'goa11y: announce row 3 of 64' \
    --script-expect 'rx-go-a11y-done' --timeout 300

# --- the seat + the armed speech lane --------------------------------------
vgate_assert 01 serial-contains 'exec: loaded GOTABWM.ELF'
vgate_assert 01 serial-contains 'gotabwm: registered'
vgate_assert 01 serial-contains 'gotabwm: mode live'
vgate_assert 01 serial-contains 'gotabwm: hostint armed epoch='
vgate_assert 01 serial-contains 'lanes=speech'

# --- the reader + the hosted demo -------------------------------------------
vgate_assert 01 serial-contains 'exec: loaded GOA11Y.ELF'
vgate_assert 01 serial-contains 'goa11y: open id='
vgate_assert 01 serial-contains 'goa11y: declare accepted'
vgate_assert 01 serial-contains 'goa11y: present'
vgate_assert 01 serial-contains 'exec: loaded GODEMO.ELF'
vgate_assert 01 serial-contains 'godialog: painted'
vgate_assert 01 serial-contains 'goa11y: focus win='

# --- the speak path, both receipts -------------------------------------------
# The reader staged at least one utterance; the seat signed it; the runner
# handed the decode to the synth — and no staging was ever refused.
vgate_assert 01 serial-contains 'goa11y: say queued len='
vgate_assert 01 serial-contains 'gotabwm: hostint speak seq=1'
vgate_assert 01 output-contains 'host-speech: speak seq=1'
vgate_assert 01 serial-absent 'goa11y: say refused'
vgate_assert 01 serial-absent 'goa11y: query '
vgate_assert 01 serial-absent 'goa11y: bad outline win='
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'exited status=139'

# --- the narration audit: focus moves announce in order ----------------------
# GOA11Y dedups on (win, outline bytes), so serial carries one announce per
# DISTINCT utterance the 500 ms poll sampled — a subsequence of the states
# the key drive published, not necessarily all of them. GODEMO always
# publishes exactly one focused node, so every announce is a focused
# node's name.
#
# Canonical utterance chain, in key order:
#   Tree            the initial ring focus (the CONTROL holds it)
#   File            F10 activates the bar on title 0
#   Open            down drops File; first enabled item hovered
#   Edit            right lands the all-disabled menu's title
#   Expand All      right drops View on its first item
#   Tree            e fires &Expand All; the ring refocuses
#   row 1 of 64     tab hands the table the keys (Row=0)
#   row 2 of 64     down
#   row 3 of 64     down,right land cell r=2 c=1 — the run's own stage
#                   gate, guaranteed present
# Any sample the poll caught must sit on this chain, in order.
vgate_assert 01 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
i0 = ser.find("godialog: painted")
if i0 < 0:
    sys.exit("godialog: painted never printed")
anns = [ln[len("goa11y: announce "):]
        for ln in ser[i0:].splitlines()
        if ln.startswith("goa11y: announce ")]
canonical = ["Tree", "File", "Open", "Edit", "Expand All",
             "Tree", "row 1 of 64", "row 2 of 64", "row 3 of 64"]
ci = 0
for text in anns:
    while ci < len(canonical) and canonical[ci] not in text:
        ci += 1
    if ci == len(canonical):
        sys.exit("announce %r breaks key order: observed %r" % (text, anns))
    ci += 1
if not anns or anns[-1] != "row 3 of 64":
    sys.exit("last announce %r, want row 3 of 64 (the run's stage gate)"
             % (anns[-1] if anns else None))
if not any(t in ("File", "Open", "Edit", "Expand All") for t in anns):
    sys.exit("no menu-mode announce observed: %r" % anns)
print("a11y narrate: %d announces in key order, chain %r" % (len(anns), anns))
PY
