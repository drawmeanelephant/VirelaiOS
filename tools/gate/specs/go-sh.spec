# go-sh.spec -- M68a (issue #1449) class-B gate: GOSH, the Go shell, hosts
# the M49 daily-use bar as a tabapp window in Zig TABWM. The seeded
# STARTUP.SH drives the scripting subset through the same engine a typed
# line uses: env/variables, one pipe, and > redirects (each read back ON
# THE HOST, byte-equal), a foreground exec whose status arrives through $?,
# and a background job cycled through jobs/fg. Then the harness types at
# the prompt: a fresh line, a Backspace edit, and an Up-arrow recall edited
# by one character -- each visible as its own `gosh: line ` marker.
#
# HOST PREREQUISITE (fails the gate honestly when missing):
#   bash tools/go/build-gosh.sh   ->  .build/go/GOSH.ELF
#
# exec-order: assert-proven -- the run ends on `rx-gosh-ok`, which only the
# stage script prints, and the stage gate waits on `gosh: line echo ay`,
# which only the typed recall can produce; a shell that never ran cannot pass.

vgate_name go-sh "issue #1449 M68a: GOSH runs the M49 bar as a tab (exec, pipe, redirect, jobs/fg, editing) on VZ"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script.txt <<'EOF'
tabwm
tabwm start
EOF

vgate_file script2.txt <<'EOF'
exec GOSH.ELF
EOF

# The close is driven from the harness after the typed-history marker, so
# every interactive line reached the shell before the window closes.
vgate_file script3.txt <<'EOF'
dui close 2
echo rx-gosh-ok
EOF

# M81e2 (#1787) added two lines to the startup script below, and they are
# the redirect decision pinned on the share. `>` is now a crash-safe
# publish, so `echo shrunk-body > /host/GOOSHSHRINK.TXT` runs against a body
# the setup hook seeded LONG (800 B) and must leave EXACTLY the 13 B new body
# -- the share-equals assert is byte-equal, so a surviving tail fails rather
# than passes. `echo first-appended >> /host/GOSHVARS.TXT` then appends to
# the redirect above, so GOSHVARS.TXT reads back as body-plus-one-line; an
# append that replaced would show only the appended line. Neither may leave
# a `~` temp beside it (the last python assert).
#
# NOTE for the next editor: do NOT put comments inside this heredoc. The
# startup block is a GUEST script run by shlib, where a leading `#` is a
# parse-level no-op but is still submitted as a line -- so it lands in the
# history ring and shifts the Up-arrow recall the typed-chord choreography
# below depends on. (Observed 2026-09-26: six `#` lines here made run 01
# recall the wrong entries and the `echo az` / `echo ay` markers never
# printed. Comments belong in this prose, above the heredoc.)
#
# The startup contract: STARTUP.SH (then PROFILE.SH, absent here) runs
# before the first prompt. `exec` inside GOSH is the monitor vocabulary --
# it runs the named ELF; the -c child form is GOSH's own headless mode
# (no window, no tty, exits with the line's status). Two children, in this
# order: the foreground `exit 7` pins exec+wait status propagation through
# $?; the background `echo nested-child-ok` cycles the job table
# (jobs -> fg) AND proves a child's own console output reaches the serial
# log -- the old `sleep 2` there printed nothing, which is why the
# `nested-child-ok` assert below could never fire. A THIRD
# sequential exec from one EL0 parent currently dies in the Go runtime's
# own schedinit (observed twice: "refill of span with reusable pointers"
# -> fatal exit 2) -- surfaced by this card, documented on #1449, follow-up
# owed to the runtime/kernel owners; this scenario stays at two children
# until that lands.
vgate_file STARTUP.SH <<'EOF'
echo gosh-startup-ran
set GREET=hello-vars
echo V=$GREET > /host/GOSHVARS.TXT
echo shrunk-body > /host/GOOSHSHRINK.TXT
echo first-appended >> /host/GOSHVARS.TXT
echo alpha-beta | grep alpha > /host/GOSHPIPE.TXT
exec GOSH.ELF -c "exit 7"
echo rc=$? > /host/GOSHRC.TXT
exec GOSH.ELF -c "echo nested-child-ok" &
jobs
fg 1
echo jobrc=$? > /host/GOSHJOB.TXT
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
shutil.copy(os.path.join(rd, "STARTUP.SH"), os.path.join(share, "STARTUP.SH"))
# M81e2 (#1787): seed the shrink target LONG, so STARTUP.SH's
# `echo shrunk-body > /host/GOOSHSHRINK.TXT` is a replace that SHRINKS. An
# in-place writer that opens without truncating leaves the tail of this body
# behind, and share-equals below is a byte-EQUAL check, so it would fail.
with open(os.path.join(share, "GOOSHSHRINK.TXT"), "w") as fh:
    fh.write("PREEXISTING-LONG-BODY\n" * 40)
print("staged GOSH.ELF (%d bytes) + STARTUP.SH into share" %
      os.path.getsize(os.path.join(share, "GOSH.ELF")))
PY

# Typed at the prompt -- THREE submitted lines, because `gosh: line ` is
# emitted on submit only: `echo abc` is typed and returned; Up recalls it,
# two backspaces and a z turn it into `echo az`, returned; Up recalls THAT,
# a backspace removes the z and a y makes it `echo ay`, the marker the stage
# gate waits on. The third line is deliberately NOT `echo azx`: `echo az`
# would be a prefix of it, and no substring-tolerant assert can then tell the
# two recall lines apart (see the marker assert below).
vgate_run 01 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --script '$RUN_DIR/script.txt' \
    --script2 '$RUN_DIR/script2.txt' \
    --script2-after 'tabwm: sidebar-rendered' \
    --input-chords 'e,c,h,o,space,a,b,c,return,up,backspace,backspace,z,return,up,backspace,y,return' \
    --input-chords-after 'gosh: prompt' \
    --script3 '$RUN_DIR/script3.txt' \
    --script3-after 'gosh: line echo ay' \
    --script-expect 'rx-gosh-ok' --timeout 240

vgate_assert 01 serial-contains 'VirelaiOS kernel has seized control.'
vgate_assert 01 serial-contains 'tabwm: starting TABWM.BIN'
vgate_assert 01 serial-contains 'tabwm: registered'
vgate_assert 01 serial-contains 'tabwm: sidebar-rendered'
vgate_assert 01 serial-contains 'exec: loaded GOSH.ELF'
vgate_assert 01 serial-contains 'gosh: ready'
vgate_assert 01 serial-contains 'gosh: open id='
vgate_assert 01 serial-contains 'gosh: declare accepted'
vgate_assert 01 serial-contains 'gosh: tty'
vgate_assert 01 serial-contains 'gosh: attached'
# The startup contract ran before the first prompt; its lines are visible
# as the same `gosh: line ` markers a typed line gets.
vgate_assert 01 serial-contains 'gosh: line echo gosh-startup-ran'
vgate_assert 01 serial-contains 'gosh: prompt'
vgate_assert 01 serial-contains 'gosh: job 1 pid='
vgate_assert 01 serial-contains 'gosh: job 1 done exit=0'
# The four markers neither existing kind can carry honestly -- the child's own
# console output and the three typed lines -- matched at LINE START by a
# python hook:
#  * `serial-exact` (whole line) is unusable for a guest CONSOLE line. The
#    guest's console write races the kernel's own log writes, which land on
#    the tail of the same serial line. Observed 1 run in 8:
#    `gosh: line echo azsmp: steal runs=50 task=GOSH.ELF from=0` -- the marker
#    was printed and grep -x counted 0.
#  * `serial-contains` cannot carry them either: the staged line
#    `gosh: line exec GOSH.ELF -c "echo nested-child-ok" &` contains the
#    child's marker, and `gosh: line echo az` is a substring of
#    `gosh: line echo azx` -- both would pass with no work done.
# Anchoring at ^ excludes every line that merely CONTAINS the text (the
# parent's staged line starts with `gosh: line exec`), and tolerating a tail
# keeps the interleaved case passing. The typed markers are prefix-free, so no
# marker can be satisfied by another marker's line.
vgate_assert 01 python <<'PY'
import os, re, sys

ser = open(os.environ["VG_SER"], errors="replace").read()
wants = {
    "nested-child-ok": "the background child's own console output",
    "gosh: line echo abc": "typed line 1 (a fresh line, submitted)",
    "gosh: line echo az": "typed line 2 (Up recall, two Backspaces, z)",
    "gosh: line echo ay": "typed line 3 (Up recall of echo az, Backspace, y)",
}
missing = [w for w in wants if not re.search(r"(?m)^" + re.escape(w), ser)]
if missing:
    sys.exit("marker(s) never printed at line start: " + ", ".join(
        "%s (%s)" % (w, wants[w]) for w in missing))
PY
vgate_assert 01 serial-contains 'gosh: close'
vgate_assert 01 serial-contains 'gosh OK'
vgate_assert 01 serial-contains 'rx-gosh-ok'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-absent 'exited status=139'
# The scripting subset's byte proof ON THE HOST, byte-EQUAL rather than
# contains -- `rc=7` would also have matched `rc=70`. The share files carry
# the trailing newline the typed echo wrote, hence the $'...' literals.
# `>>` appended AFTER the `>` redirect above, so the file is the redirect's
# body plus one appended line -- an append that replaced instead would read
# back as just `first-appended`.
vgate_assert 01 share-equals GOSHVARS.TXT $'V=hello-vars\nfirst-appended\n'
vgate_assert 01 share-equals GOSHPIPE.TXT $'alpha-beta\n'
vgate_assert 01 share-equals GOSHRC.TXT $'rc=7\n'
vgate_assert 01 share-equals GOSHJOB.TXT $'jobrc=0\n'
# M81e2 (#1787): the redirect decision, pinned on the share. `>` published
# crash-safe, so over a 800 B pre-existing body the file must read back as
# EXACTLY the 13 B new body -- byte-equal, so a surviving tail fails rather
# than passes. The sacrificial temp must not be there either: a share holding
# an orphan is a share a human has to clean by hand.
vgate_assert 01 share-equals GOOSHSHRINK.TXT $'shrunk-body\n'
vgate_assert 01 python <<'PY'
import os, sys
share = os.environ.get("VG_SHARE") or os.path.join(os.environ["RUN_DIR"], "share")
# Scoped to the paths THIS script redirects to, not a blanket scan of the
# share: the safe publish writes path+"~", so an orphan beside any of these
# is a publish that did not finish. A share-wide scan would also assert other
# components' publish hygiene, which is not what this run is here to prove.
stray = [n for n in sorted(os.listdir(share))
         if n.endswith("~") and n[:-1] in
         ("GOOSHSHRINK.TXT", "GOSHVARS.TXT", "GOSHPIPE.TXT", "GOSHRC.TXT",
          "GOSHJOB.TXT")]
if stray:
    sys.exit("orphan publish temp(s) beside a redirect target: " +
             ", ".join(stray) + " - the publish did not complete")
PY
