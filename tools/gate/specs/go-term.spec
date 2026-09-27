# go-term.spec -- M58c (issue #1307) class-B gate: a Go terminal window opens
# over the existing /dev/tty seam, attaches selector 2, types a line, and
# closes, full-viewport inside Zig TABWM. M80j (#1726) also drives `exit`:
# the default `term_restart=stay` keeps the same window/fd, announces the
# shell status, and starts a second prompt before the harness closes it.
#
# user/go/term is a tabapp client: init -> declare (kind-8 WM_RPC) -> open
# /dev/tty -> sys_tty_attach(2, window_id) -> startup block through the tty
# (M73c #1627: startup contract banner, SETTINGS prompt load, history load,
# `goterm: ready`, first prompt painted by shlib's editor) -> injected
# keystrokes (ADR 0020 A5) feed editor.Feed, and each EvSubmit runs the line
# through shlib's Shell.RunLine -> a shell exit restarts the session (or
# closes it under term_restart=exit), and WIN_CLOSE detaches and exits.
# Zig TERM.BIN / SH.BIN are untouched; kernel untouched; no second renderer.
#
# M73c (#1627) execution proof: observed on the first reshaped run --
# WINDOWED tty bytes never reach serial (ADR 0020 selector 2 paints the
# window grid; only ConsoleLine lines are serial-observable), so output
# text (live-sh's `PWD=/data` shape) can never vouch for execution here.
# The proof is instead `goterm: done status=N`, printed by the app AFTER
# Shell.RunLine returned, carrying the engine's OWN status: the typed line
# `cd /data` yields status=0 only after host.Chdir verified the real
# directory (term's cd checks sys_dir_list, same as GOSH; `data/` is
# seeded in setup), and the negative control `cd /nosuchdir` yields a
# nonzero status -- so neither marker can come from a constant, an echo
# stub, or an unexecuted line. Chords are limited to characters both key
# tables map (`$`, `%`, `>` are unmapped in macKey/hidUsage -- observed in
# host/vm-runner main.swift), which is why the lines are cd forms typed
# over --input-chords.
#
# HOST PREREQUISITE (fails the gate honestly when missing):
#   bash tools/go/build-goterm.sh   ->  .build/go/GOTERM.ELF
#
# exec-order: assert-proven -- the run ends on `rx-goterm-ok`, which only the
# script prints, and the stage gate that forwards the close waits on the app's
# own `goterm: restart` marker; a program that never ran, or one that echoed
# without executing, cannot pass (the two `goterm: done status=` markers
# carry engine-computed statuses 0 then nonzero, and the ordering python
# asserts the whole chain).

vgate_name go-term "issue #1307 M58c: a Go terminal attaches /dev/tty selector 2 in Zig TABWM on VZ"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script.txt <<'EOF'
tabwm
tabwm start
EOF

vgate_file script2.txt <<'EOF'
exec GOTERM.ELF
EOF

# The close is driven from the harness after the app's own `goterm: restart`
# marker, so the replacement session reached its prompt before teardown.
vgate_file script3.txt <<'EOF'
dui close 2
echo rx-goterm-ok
EOF

vgate_setup_python <<'PY'
import os, shutil, sys
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
src = os.path.join(".build", "go", "GOTERM.ELF")
if not os.path.exists(src):
    sys.exit("GOTERM.ELF missing (expected " + src + ") - build it first: "
             "bash tools/go/build-goterm.sh")
shutil.copy(src, os.path.join(share, "GOTERM.ELF"))
print("staged GOTERM.ELF into share (%d bytes)" %
      os.path.getsize(os.path.join(share, "GOTERM.ELF")))
# The `cd /data` target must EXIST: term's cd verifies through sys_dir_list
# (same as GOSH's), so the assert below would otherwise never fire.
os.makedirs(os.path.join(share, "data"), exist_ok=True)
print("seeded data/ for cd")
# M81e2 (#1787): seed GOSH-HISTORY.TXT with a body LONGER than anything this
# run appends, so the history save is a write over existing bytes. The two
# lines the harness types are APPENDED (one per submit), so the file grows
# rather than shrinks here; what this run proves is that the append adds to
# the seeded body instead of replacing it, and leaves no orphan temp.
with open(os.path.join(share, "GOSH-HISTORY.TXT"), "w") as fh:
    fh.write("seeded-history-line\n" * 3)
print("seeded GOSH-HISTORY.TXT (3 lines) for the append half")
PY

vgate_run 01 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/script2.txt' \
    --script2-after 'tabwm: sidebar-rendered' \
    --input-chords 'c,d,space,/,d,a,t,a,return,c,d,space,/,n,o,s,u,c,h,d,i,r,return,e,x,i,t,return' \
    --input-chords-after 'goterm: prompt' \
    --script3 '$RUN_DIR/script3.txt' \
    --script3-after 'goterm: restart' \
    --script-expect 'rx-goterm-ok' --timeout 240

vgate_assert 01 serial-contains 'VirelaiOS kernel has seized control.'
vgate_assert 01 serial-contains 'tabwm: starting TABWM.BIN'
vgate_assert 01 serial-contains 'tabwm: registered'
vgate_assert 01 serial-contains 'tabwm: sidebar-rendered'
vgate_assert 01 serial-contains 'exec: loaded GOTERM.ELF'
vgate_assert 01 serial-contains 'goterm: open id='
vgate_assert 01 serial-contains 'goterm: declare accepted'
vgate_assert 01 serial-contains 'goterm: tty'
vgate_assert 01 serial-contains 'goterm: attached'
# M73c: the shlib startup block announced itself before the first prompt.
vgate_assert 01 serial-contains 'goterm: ready'
# Prompt bytes went through /dev/tty so the kernel has something to paint.
vgate_assert 01 serial-contains 'goterm: prompt'
# The injected keystrokes reached the tty input queue and submitted two
# lines, each announced by the app AFTER the editor cut it, each finished
# with the engine-computed status AFTER RunLine returned.
vgate_assert 01 serial-contains 'goterm: line cd /data'
vgate_assert 01 serial-contains 'goterm: done status=0'
vgate_assert 01 serial-contains 'goterm: line cd /nosuchdir'
vgate_assert 01 serial-contains 'goterm: restart'
vgate_assert 01 serial-contains 'goterm: close'
vgate_assert 01 serial-contains 'goterm OK'
vgate_assert 01 serial-contains 'rx-goterm-ok'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'exited status=139'
# M81e2 (#1787): the history save, on the share. GOTERM calls
# shlib.SaveHistory on every submit through the SAME termHost.WriteFile the
# shell hook uses, so this is the second front-end's half of the card. The
# file must still carry the three seeded lines AND the typed ones (an append
# that replaced would drop the seeded body), and the sacrificial temp must
# not be beside it.
vgate_assert 01 python <<'PY'
import os, sys
share = os.environ.get("VG_SHARE") or os.path.join(os.environ["RUN_DIR"], "share")
hist = os.path.join(share, "GOSH-HISTORY.TXT")
if not os.path.exists(hist):
    sys.exit("GOSH-HISTORY.TXT is missing - the history save never landed")
body = open(hist, errors="replace").read()
if body.count("seeded-history-line") != 3:
    sys.exit("the seeded history body is %d lines, want 3 - the save " +
             "replaced instead of appending:\n%s" %
             (body.count("seeded-history-line"), body))
for want in ("cd /data", "cd /nosuchdir"):
    if want not in body:
        sys.exit("the typed line %r is not in the history file:\n%s" % (want, body))
orphan = hist + "~"
if os.path.exists(orphan):
    sys.exit("GOSH-HISTORY.TXT~ survived the history save")
PY
# THE execution proof + the whole chain in order: prompt, submit 1, its
# done marker with status=0 (host.Chdir verified /data -- `data/` is
# seeded), submit 2, its done marker with a NONZERO status (the negative
# control: the status is the engine's, not a constant), then the
# WIN_CLOSE teardown the harness only sends after the restart marker.
vgate_assert 01 python <<'PY'
import os, re, sys
ser = open(os.environ["VG_SER"], errors="replace").read().splitlines()
done_re = re.compile(r"^goterm: done status=(-?\d+)$")

def first_after(sub, start=0):
    for i in range(start, len(ser)):
        if sub in ser[i]:
            return i
    sys.exit("missing %r (after %d)" % (sub, start))

def first_done_after(start):
    for i in range(start, len(ser)):
        m = done_re.match(ser[i].rstrip("\r"))
        if m:
            return i, int(m.group(1))
    sys.exit("no done marker after %d" % start)

ready_i = first_after("goterm: ready")
prompt_i = first_after("goterm: prompt", ready_i)
line1_i = first_after("goterm: line cd /data", prompt_i)
done1_i, st1 = first_done_after(line1_i)
if st1 != 0:
    sys.exit("cd /data status=%d want 0 (dir not verified?)" % st1)
line2_i = first_after("goterm: line cd /nosuchdir", done1_i)
done2_i, st2 = first_done_after(line2_i)
if st2 == 0:
    sys.exit("cd /nosuchdir status=0: the status is not the engine's")
# The replacement prompt is painted before restart is announced, and the
# harness only types script3 after restart. rx can race close on the
# console task, so it is chained to the restart rather than to close.
prompt2_i = first_after("goterm: prompt", done2_i)
restart_i = first_after("goterm: restart", prompt2_i)
close_i = first_after("goterm: close", restart_i)
rx_i = first_after("rx-goterm-ok", restart_i)
print("order ok: ready@%d prompt@%d cd/data@%d status=%d "
      "cd/nosuchdir@%d status=%d prompt2@%d restart@%d close@%d rx@%d" %
      (ready_i, prompt_i, line1_i, st1, line2_i, st2, prompt2_i,
       restart_i, close_i, rx_i))
PY
