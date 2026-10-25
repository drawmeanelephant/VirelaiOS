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
# Run 02 (M197e / #3420) is the wave's acceptance journey — ONE boot where
# all four M197 legs hold at once: the seat's own A11Y.SEAT outline names
# the rail's live tabs (a/#3410), focus moves announce in order over the
# same drive (b/#3392), a typed goset-style a11y.reduce_motion flip
# reaches a running GOVIEW -play live -> `gview: motion reduced`
# (c/#3412), and the adopted GOEDIT's published outline decodes off the
# share (d/#3418). vgate_repeat makes the whole
# spec K=3; the trailing python asserts on each run are the iteration
# hygiene (the armed share persists across boots — go-wm-journey run 03's
# rule).
#
# HOST PREREQUISITES (fail the gate honestly when missing):
#   bash tools/go/build-gotabwm.sh     ->  .build/go/GOTABWM.ELF
#   bash tools/go/build-goa11y.sh      ->  .build/go/GOA11Y.ELF
#   bash tools/go/build-appkitdemo.sh  ->  .build/go/GODEMO.ELF
#   bash tools/go/build-goedit.sh      ->  .build/go/GOEDIT.ELF
#   bash tools/go/build-goview.sh      ->  .build/go/GOVIEW.ELF
#   bash tools/go/build-goset.sh       ->  .build/go/GOSET.ELF

vgate_name go-a11y "M197b #3382: GOA11Y narration verbs — announce order + speak path over VHI1, first owning gate; M197e #3420: the four-leg a11y journey, K=3"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 3 BOOTS

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

# --- iteration hygiene (M197e: the spec is K=3 now) -------------------------
# The armed share persists across the vgate_repeat boots and the VM stops
# mid-run, so UnpublishOutline never ran. A leftover outline is dead data
# only until a window re-binds the same id — the next boot's GODEMO would
# then announce `row 3 of 64` BEFORE its drive ran (script3's gate fires
# early) or read a dedup-silent repeat (the utterance never re-announces).
# Drop every staged A11Y.* so each iteration reads only what it published.
vgate_assert 01 python <<'PY'
import glob, os, sys
share = os.environ.get("VG_SHARE")
if not share:
    sys.exit("no armed share exported to the assert (vgate_share seed?)")
stale = [p for p in glob.glob(os.path.join(share, "A11Y.*"))
         if os.path.isfile(p)]
for p in stale:
    os.remove(p)
print("run-01 hygiene: dropped %d stale A11Y.* outline(s)" % len(stale))
PY

# ===========================================================================
# Run 02 (M197e / #3420): the acceptance journey — one boot, all four legs.
#
#   script-02    exec GOTABWM.ELF — the same wm=none opt-in posture as
#                run 01 (the vf rm sweep drops any session/snapshot state a
#                prior iteration left before the seat can restore it).
#   script2-02   on `gotabwm: hostint armed`: dui focus 0 parks the console,
#                then GOA11Y -announce -say, GOEDIT, GOVIEW -play, GODEMO —
#                GODEMO execs last so its declare + self-raise is the last
#                focus event and the chords land on the demo (the run-01
#                exec-order rule). GOEDIT's own declare may hold focus long
#                enough to announce `document` mid-startup: harmless — the
#                audits below scope to windows, never to order across them.
#   chords       on `godialog: painted`: run 01's menu drive verbatim,
#                ending on return — announces Tree -> File -> Open -> Edit
#                -> Expand All -> row 1..3 of 64 on GODEMO.
#   script3-02   on `goa11y: announce row 3 of 64` — the drive's own end
#                utterance, so every announce and every speak receipt the
#                drive produced is already in serial: exec GOSET.ELF — the
#                last declare, so the panel holds the keyboard when the
#                typed lines arrive (the go-settings run-05 posture without
#                GOSH).
#   input-string on `goset: ready `: `access` opens the a11y pane, then
#                `a11y.reduce_motion=on` is apply+save+notify on Enter —
#                the run-05 typed pair verbatim.
#   expect       `gview: motion reduced` — the flip's live receipt; every
#                leg's evidence precedes it in serial.
#
# The four legs, asserted below:
#   (a) the seat describes itself  — `gotabwm: a11y seat nodes=` prints and
#       A11Y.SEAT decodes to the five live rail cells (A11y / Edit /
#       PLAY.GIF 160x120 GIF 100 / Dialog / Settings — the cell name is the
#       24-byte wire title, so GOVIEW's composed title loses its last
#       char), focused=Settings.
#   (b) the reader narrates        — GODEMO's announces replay the key-order
#       chain ending at `row 3 of 64`, plus the -say speak receipts.
#   (c) the live flip              — goset set -> saved -> notified ->
#       seat broadcast -> `gview: motion reduced`, and playback parks (no
#       `gview: play ` receipt may follow).
#   (d) the adopted app            — GOEDIT's published A11Y.<win> outline
#       decodes with its window/menubar/textfield structure and the focused
#       document field. Read off the share, not a rail-chord detour: rail
#       position is declare order, and a five-app exec burst can race a
#       mid-window declare onto the wrong cell (observed on the first
#       iteration — ctrl-2 landed on GOVIEW, which publishes nothing).
#
# HOST PREREQUISITE (fails honestly when missing):
#   bash tools/go/build-goedit.sh   -> .build/go/GOEDIT.ELF
#   bash tools/go/build-goview.sh   -> .build/go/GOVIEW.ELF
#   bash tools/go/build-goset.sh    -> .build/go/GOSET.ELF

vgate_file script-02.txt <<'EOF'
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm SESSION.PANES
vf rm SNAPSHOT.BUNDLE
vf rm SNAPSHOT.RESTORE
exec GOTABWM.ELF
EOF

vgate_file script2-02.txt <<'EOF'
dui focus 0
exec GOA11Y.ELF -announce -say
exec GOEDIT.ELF
exec GOVIEW.ELF -play /host/PLAY.GIF
exec GODEMO.ELF
EOF

vgate_file script3-02.txt <<'EOF'
exec GOSET.ELF
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
share = os.environ.get("VG_SHARE") or os.path.join(os.environ["RUN_DIR"], "share")
for name, tool in (("GOEDIT.ELF", "build-goedit.sh"),
                   ("GOVIEW.ELF", "build-goview.sh"),
                   ("GOSET.ELF", "build-goset.sh")):
    src = os.path.join(".build", "go", name)
    if not os.path.exists(src):
        sys.exit("%s missing (expected %s) - build it first: "
                 "bash tools/go/%s" % (name, src, tool))
    shutil.copy(src, os.path.join(share, name))
    print("staged %s (%d bytes)" % (name, os.path.getsize(src)))
# GOVIEW -play's ten-frame loop (the same fixture go-settings run 03/05 use).
src = "user/go/view/testdata/play10_160x120.gif"
if not os.path.isfile(src):
    sys.exit("fixture %s missing" % src)
shutil.copy(src, os.path.join(share, "PLAY.GIF"))
print("staged PLAY.GIF (ten frames at 50 ms)")
PY

vgate_run 02 -- \
    --screen '$RUN_DIR/screen-02' \
    --via-virtio \
    --host-speech \
    --script '$RUN_DIR/script-02.txt' \
    --script2 '$RUN_DIR/script2-02.txt' \
    --script2-after 'gotabwm: hostint armed' \
    --input-chords 'escape,f10,down,right,right,e,tab,down,down,right,return' \
    --input-chords-after 'godialog: painted' \
    --script3 '$RUN_DIR/script3-02.txt' \
    --script3-after 'goa11y: announce row 3 of 64' \
    --input-string $'access\na11y.reduce_motion=on\n' \
    --input-string-after 'goset: ready ' \
    --script-expect 'gview: motion reduced' --timeout 420

# --- the opt-in seat + the armed speech lane --------------------------------
vgate_assert 02 serial-contains 'exec: loaded GOTABWM.ELF'
vgate_assert 02 serial-contains 'gotabwm: registered'
vgate_assert 02 serial-contains 'gotabwm: mode live'
vgate_assert 02 serial-contains 'gotabwm: hostint armed epoch='
vgate_assert 02 serial-contains 'lanes=speech'
# --- the five runtimes of the journey ----------------------------------------
vgate_assert 02 serial-contains 'exec: loaded GOA11Y.ELF'
vgate_assert 02 serial-contains 'goa11y: open id='
vgate_assert 02 serial-contains 'goa11y: declare accepted'
vgate_assert 02 serial-contains 'goa11y: present'
vgate_assert 02 serial-contains 'exec: loaded GOEDIT.ELF'
vgate_assert 02 serial-contains 'exec: loaded GOVIEW.ELF'
vgate_assert 02 serial-contains 'exec: loaded GODEMO.ELF'
vgate_assert 02 serial-contains 'exec: loaded GOSET.ELF'
vgate_assert 02 serial-contains 'godialog: painted'
# --- the key drive landed (leg b's substrate) --------------------------------
vgate_assert 02 serial-contains 'godemo: menu expand-all'
vgate_assert 02 serial-contains 'godemo: table activate r=2 c=1'
# --- leg (a): the seat describes itself --------------------------------------
# The per-publish line, then the file itself decoded below.
vgate_assert 02 serial-contains 'gotabwm: a11y seat nodes='
# --- leg (c): the live flip, marker by marker --------------------------------
vgate_assert 02 serial-contains 'gview: loaded PLAY.GIF 160x120 GIF bytes=24121 frames=10'
vgate_assert 02 serial-contains 'gview: settings subscribed key=a11y.reduce_motion'
# Really playing before the flip — at least one loop-wrap receipt.
vgate_assert 02 serial-contains 'gview: play '
vgate_assert 02 serial-contains 'goset: declare accepted'
vgate_assert 02 serial-contains 'goset: ready keys='
vgate_assert 02 serial-contains 'goset: tab access'
vgate_assert 02 serial-contains 'goset: set a11y.reduce_motion=on'
vgate_assert 02 serial-contains 'goset: saved keys='
vgate_assert 02 serial-contains 'goset: settings notified key=a11y.reduce_motion'
vgate_assert 02 serial-contains 'gotabwm: settings broadcast key=a11y.reduce_motion listeners=1'
vgate_assert 02 serial-contains 'gview: motion reduced'
vgate_assert 02 share-contains 'SETTINGS.TXT' 'a11y.reduce_motion=on'
# --- leg (b): the speak path's receipts, both ends ---------------------------
vgate_assert 02 serial-contains 'goa11y: say queued len='
vgate_assert 02 serial-contains 'gotabwm: hostint speak seq=1'
vgate_assert 02 output-contains 'host-speech: speak seq=1'
vgate_assert 02 serial-absent 'goa11y: say refused'
vgate_assert 02 serial-absent 'goa11y: query '
vgate_assert 02 serial-absent 'goa11y: bad outline win='
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 serial-absent 'exited status=139'

# --- leg (b) the audit: focus moves announce in key order --------------------
# The reader's announces are grouped under their `focus win=` context — a
# startup `document` announce off GOEDIT's declare window sits in its own
# stream and can never break GODEMO's chain. GODEMO's win is the one whose
# stream ends on `row 3 of 64` — the drive's own last utterance, and the
# run's stage gate, so it is guaranteed present.
vgate_assert 02 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
i0 = ser.find("godialog: painted")
if i0 < 0:
    sys.exit("godialog: painted never printed")
# Attribute each announce to its window: a `focus win=` block (or a bare
# `no outline win=` state) sets the context announce lines land under —
# the lines themselves never repeat the id.
anns = {}
cur = None
for ln in ser[i0:].splitlines():
    if ln.startswith("goa11y: focus win=") or \
            ln.startswith("goa11y: no outline win="):
        cur = ln.split("win=", 1)[1].split(" ", 1)[0]
    elif ln.startswith("goa11y: announce ") and cur is not None:
        anns.setdefault(cur, []).append(ln[len("goa11y: announce "):])

demo = None
for w, ts in anns.items():
    if "row 3 of 64" in ts:
        demo = w
if demo is None:
    sys.exit("no window announced 'row 3 of 64': %r" % anns)

# Run 01's audit, scoped to the demo's window: any utterance the 500 ms poll
# caught must sit on the canonical chain, in key order.
canonical = ["Tree", "File", "Open", "Edit", "Expand All",
             "Tree", "row 1 of 64", "row 2 of 64", "row 3 of 64"]
ci = 0
for text in anns[demo]:
    while ci < len(canonical) and canonical[ci] not in text:
        ci += 1
    if ci == len(canonical):
        sys.exit("announce %r breaks key order: observed %r"
                 % (text, anns[demo]))
    ci += 1
if anns[demo][-1] != "row 3 of 64":
    sys.exit("last demo announce %r, want row 3 of 64" % anns[demo][-1])
if not any(t in ("File", "Open", "Edit", "Expand All") for t in anns[demo]):
    sys.exit("no menu-mode announce observed: %r" % anns[demo])
print("journey leg b: demo win=%s announces %r in key order"
      % (demo, anns[demo]))
PY

# --- leg (a) the file: the seat's outline names the rail's live tabs --------
# A11Y.SEAT is the fixed rendezvous (the seat is not a kind-16 row — the
# reader never sees it as a window). Read at gate time it holds the final
# chrome: five tab cells — the five exec'd apps' declared titles — with the
# still-focused GOSET cell carrying 'f'.
vgate_assert 02 python <<'PY'
import os, sys
share = os.environ["VG_SHARE"]
path = os.path.join(share, "A11Y.SEAT")
if not os.path.isfile(path):
    sys.exit("A11Y.SEAT missing — the seat never staged its outline")
body = open(path, "rb").read()
if not body.startswith(b"#a11y v1\n"):
    sys.exit("A11Y.SEAT header %r" % body[:16])
nodes = []
for ln in body[len(b"#a11y v1\n"):].decode("utf-8", "replace").splitlines():
    if not ln:
        continue
    f = ln.split(" ", 3)
    if len(f) < 3:
        sys.exit("bad seat-outline line %r" % ln)
    nodes.append((int(f[0]), f[1], f[2], f[3] if len(f) > 3 else ""))
if not nodes or nodes[0][:3] != (0, "seat", "-"):
    sys.exit("seat root %r" % (nodes[0] if nodes else None))
rails = [n for n in nodes if n[1] == "rail"]
if len(rails) != 1 or rails[0][3] != "space 1":
    sys.exit("rail container %r" % rails)
cells = [(n[2], n[3]) for n in nodes if n[1] == "tab"]
# Cell names are the declared titles as the wire carries them — truncated
# to wnd_core.wm_rpc_title_max (24), which clips the '%' off GOVIEW's
# composed title.
want = ["A11y", "Dialog", "Edit", "PLAY.GIF 160x120 GIF 100", "Settings"]
if sorted(name for _, name in cells) != want:
    sys.exit("rail cells %r want %r" % (cells, want))
focused = [name for st, name in cells if "f" in st]
if focused != ["Settings"]:
    sys.exit("focused cell %r, want only Settings" % focused)
print("journey leg a: %d nodes; rail cells %r; focused %s"
      % (len(nodes), cells, focused[0]))
PY

# --- leg (d) the adopted app's outline, off the share -------------------------
# GOEDIT's publishOutline runs on every loop pass, deduped on bytes — the
# file lands shortly after its first frame regardless of which window the
# seat focuses. Find the adopted outline among the staged A11Y.<win> files:
# it is the one carrying a focused `document` textfield under a
# window+menubar tree — the structure only GOEDIT publishes in this boot
# (GODEMO's tree/table outline has no textfield, and A11Y.SEAT is the
# seat's own chrome).
vgate_assert 02 python <<'PY'
import glob, os, sys
share = os.environ["VG_SHARE"]
hit = None
for path in glob.glob(os.path.join(share, "A11Y.*")):
    if os.path.basename(path) == "A11Y.SEAT" or not os.path.isfile(path):
        continue
    body = open(path, "rb").read()
    if not body.startswith(b"#a11y v1\n"):
        continue
    lines = [ln for ln in
             body[len(b"#a11y v1\n"):].decode("utf-8", "replace").splitlines()
             if ln]
    fields = [ln.split(" ", 3) for ln in lines]
    roles = {f[1] for f in fields if len(f) > 1}
    if not ({"window", "menubar", "textfield"} <= roles):
        continue
    if any(len(f) > 3 and f[1] == "textfield" and "f" in f[2]
           and f[3] == "document" for f in fields):
        hit = (os.path.basename(path), len(lines))
        break
if hit is None:
    sys.exit("no adopted outline with a focused document field on the share")
print("journey leg d: %s — %d nodes, window+menubar+focused document field"
      % (hit[0], hit[1]))
PY

# --- leg (c) the chain as an ordering pin, and the park as liveness ---------
# go-settings run 05's spine verbatim: only cross-process causality is safe
# to order on serial (the broadcast and the receipt can flush before GOSET's
# own `settings notified` line), so subscribe < play < set < broadcast <
# receipt; and once the notice applied, no `gview: play ` loop receipt may
# follow `motion reduced` — the armed timer's next event found playing off.
vgate_assert 02 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
patterns = [
    "gview: settings subscribed key=a11y.reduce_motion",
    "gview: play ",
    "goset: set a11y.reduce_motion=on",
    "gotabwm: settings broadcast key=a11y.reduce_motion listeners=1",
    "gview: motion reduced",
]
pos = [ser.find(p) for p in patterns]
if any(p < 0 for p in pos) or pos != sorted(pos):
    sys.exit("live-flip chain out of order: " + repr(list(zip(patterns, pos))))
if ser.find("gview: play ", pos[-1]) >= 0:
    sys.exit("a play receipt printed AFTER `gview: motion reduced` — the "
             "flip did not park playback")
print("live a11y.reduce_motion flip reached the running viewer and parked playback")
PY

# --- iteration hygiene -------------------------------------------------------
# The next boot's GOVIEW must open with reduce_motion OFF or the flip is no
# longer a live off->on transition: GOSET's crash-safe publish persisted the
# row, so restage the seed byte-for-byte. And drop every staged A11Y.* — the
# VM stopped mid-run, UnpublishOutline never ran, and a stale body would
# fire (or dedup-silence) a later iteration's announces.
vgate_assert 02 python <<'PY'
import glob, os, sys
share = os.environ.get("VG_SHARE")
if not share:
    sys.exit("no armed share exported to the assert (vgate_share seed?)")
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    f.write("#v2\nwm=none\n")
stale = [p for p in glob.glob(os.path.join(share, "A11Y.*"))
         if os.path.isfile(p)]
for p in stale:
    os.remove(p)
for name in ("SESSION.TABS", "SESSION.PANES",
             "SNAPSHOT.BUNDLE", "SNAPSHOT.RESTORE", "GOTABWM.DEMO"):
    p = os.path.join(share, name)
    if os.path.isfile(p):
        os.remove(p)
print("run-02 hygiene: SETTINGS.TXT reseeded (wm=none); dropped %d stale "
      "A11Y.* outline(s)" % len(stale))
PY
