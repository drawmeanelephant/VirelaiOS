# live-net-rxrecover.spec -- #3895: the VZ dead-RX attachment auto-recovery,
# exercised through the fault-injection seam (the real wedge is an Apple-side
# dead state we cannot induce on demand — see the card's evidence comment).
#
# --net-rx-dead replays the observed signature at the last hop: host->guest
# frames count sent AND pair-consumed while never reaching VZ, so the guest
# re-sends the identical solicitation and the host detector proves dead-RX
# (repeats each backed by a consumed delivery). It then writes `net rxreset`
# into the guest console — the bounded transport_init + re-arm.
#
# rxok:  'until-reset' — the fault clears at the dispatch; the post-reset
#        solicitation completes, the NET-RECOVER recovery marker fires.
# rxref: 'stall' — the fault never clears; two bounded attempts fail and the
#        named refusal stands (never a silent reset loop).

vgate_name live-net-rxrecover "#3895: dead-RX detect -> bounded re-init -> NET-RECOVER markers/refusal"

vgate_file legs.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
net arp
EOF
vgate_file legs2.txt <<'EOF'
net arp 10.0.0.4
net arp
echo rxrecover-ok
EOF

vgate_repeat 3

vgate_run rxok -- --net '$RUN_DIR/rxok.cap' --net-arp-respond 10.0.0.2 \
    --net-rx-dead until-reset \
    --script '$RUN_DIR/legs.txt' --script2 '$RUN_DIR/legs2.txt' \
    --script2-after 'net rxreset: ok' --script-expect rxrecover-ok \
    --script-expect-tail 12 --timeout 90

# No --script-expect: the refusal is a HOST-side marker, never a serial
# one — the run ends at --timeout (rc 0, the honest bound), and the
# asserts below are the verdict. 90 s covers boot + detect + 2 bounded
# attempts + the refusal window.
vgate_run rxref -- --net '$RUN_DIR/rxref.cap' --net-arp-respond 10.0.0.2 \
    --net-rx-dead stall \
    --script '$RUN_DIR/legs.txt' --timeout 90

# --- rxok: detection, bounded re-init, named recovery marker ---

# The detector fired (the dead-rx signature named with its evidence).
vgate_assert rxok output-contains 'NET-RECOVER: dead-rx'
# The recovery dispatch reached the guest: transport_init + re-arm ran.
vgate_assert rxok serial-contains 'net rxreset: ok'
# Delivery resumed: the wedged solicitation's reply completed after the
# re-init — its learn lands in the guest's ARP table.
vgate_assert rxok serial-contains 'net arp: 10.0.0.2 -> 02:00:00:00:00:02'
# The named recovery marker in host logs — delivery restored.
vgate_assert rxok output-contains 'NET-RECOVER: rx delivery restored'
vgate_assert rxok serial-contains 'rxrecover-ok'
# Bounded: at least one re-init dispatch, never an unbounded stream.
# Post-restore the detector re-arms by design — a still-dying segment may
# earn a second bounded episode; what must never happen is a silent loop.
vgate_assert rxok python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
n = ser.count("net rxreset: ok")
if not (1 <= n <= 4):
    sys.exit(f"FAIL: rxok saw {n} 'net rxreset: ok' lines, want 1..4 (bounded)")
print(f"rxok re-init bounded at {n} dispatch(es)")
PY

# --- rxref: the fault never clears -> bounded attempts -> named refusal ---

vgate_assert rxref output-contains 'NET-RECOVER: dead-rx'
# Two bounded attempts, then the named refusal — never a silent loop.
vgate_assert rxref output-contains 'NET-RECOVER: refusing further attachment re-inits'
vgate_assert rxref python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
n = ser.count("net rxreset: ok")
if n != 2:
    sys.exit(f"FAIL: rxref saw {n} 'net rxreset: ok' lines, want exactly 2 (the bounded attempt count)")
print("rxref re-init bounded at 2 dispatches")
PY
# The run ends at --timeout — nothing recovered; the guest must NOT
# print a learn line for the wedged solicitation target.
vgate_assert rxref serial-absent 'net arp: 10.0.0.2 -> 02:00:00:00:00:02'
