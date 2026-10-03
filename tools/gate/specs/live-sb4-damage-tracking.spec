# live-sb4-damage-tracking.spec -- M33 SB4 (claim 2382) class-B gate:
#
# rect-granular damage, live on real VZ hardware.
#
# SB4DAM.BIN opens a 128x96 user window and fills TWO rects (8,8,48,48 and
# 100,60,16,16) back-to-back via the kernel-visible fill path (slot 13), with
# NO yield between them, so they coalesce into ONE union damage rect
# {8,8,108,68}. The compositor then repaints EXACTLY that union — not the whole
# window — which the gate observes on serial via `dui`'s new `last=x,y,w,h`

vgate_name live-sb4-damage-tracking "M33 SB4: rect-granular damage tracking on VZ"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_setup_python <<'PY'
import os
share = os.path.join(os.environ["RUN_DIR"], "share")
with open(os.path.join(share, "M91-PRESENTATION.TRACE"), "w") as f:
    f.write("v1\n")
PY

vgate_file script.txt <<'EOF'
exec SB4DAM.BIN
EOF

vgate_file script2.txt <<'EOF'
dui
EOF

vgate_run 01 -- --screen '$RUN_DIR/screen' --script '$RUN_DIR/script.txt' --script2 '$RUN_DIR/script2.txt' --script2-after 'timer heartbeat ticks=20' --script-expect 'last=8,8,108,68' --timeout 120

vgate_assert 01 serial-contains 'sb4: filled'
vgate_assert 01 serial-contains 'last=8,8,108,68'
vgate_assert 01 serial-absent 'sb4: open-fail'
vgate_assert 01 serial-absent 'sb4: fill-fail'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 serial-contains 'm91: trace armed guest-only'
vgate_assert 01 serial-contains 'owner=shim seat=0 phase=transfer captured=1'
vgate_assert 01 serial-absent 'owner=shim seat=1'
vgate_assert 01 serial-absent 'captured=0'
vgate_assert 01 serial-absent 'm91: trace ERROR'
vgate_assert 01 serial-absent 'result=timeout'
vgate_assert 01 serial-absent 'result=not_ready'
