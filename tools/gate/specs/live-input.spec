# live-input.spec -- USB XHCI keyboard input on VZ (classic synthesized + custom-virtio)

vgate_name live-input "USB XHCI keyboard input on VZ (claim 6050 + 9588)"
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script.txt <<'EOF'
echo i3-serial-ok
EOF

vgate_file script-virtio.txt <<'EOF'
echo i3-virtio-pre
EOF

# --- Phase 1: synthesized NSEvent input ---
# Real pointer input is a human acceptance check: the untrusted NSEvent
# pointer route is not a USB delivery proof (see hardware-contract.md).
vgate_run A -- \
    --input --display \
    --script '$RUN_DIR/script.txt' \
    --input-string "input"$'\n' --input-string-after "userspace: el0=1" \
    --script-expect "input: armed=" \
    --timeout 70

vgate_assert A serial-contains "input: armed"
vgate_assert A serial-contains "events=6"
vgate_assert A serial-absent "[EXC] parking:"
vgate_assert A serial-count "input: usb consumed slot=0x0000000000000001 len=0x0000000000000008" 6
vgate_assert A serial-absent "cvspike: q3 armed"
vgate_assert A python <<'PY'
import os, re
from pathlib import Path

serial = Path(os.environ["VG_SER"]).read_text(errors="replace")
reports = re.findall(r"input: armed=1 fifo=\d+/\d+ dropped=(\d+) events=(\d+)\b", serial)
if ("0", "6") not in reports:
    raise SystemExit("native USB keyboard sequence incomplete or FIFO dropped input")
print("native USB keyboard sequence completed without FIFO drops")
PY

# --- Phase 2: custom-virtio INPUT queue ---
vgate_run B -- \
    --via-virtio \
    --script '$RUN_DIR/script-virtio.txt' \
    --input-string "input"$'\n' --input-string-after "i3-virtio-pre" \
    --script-expect "input: armed=" \
    --timeout 90

vgate_assert B serial-contains "input: armed"
vgate_assert B serial-contains "events=6"
vgate_assert B serial-absent "[EXC] parking:"
vgate_assert B output-contains "KEY-SEQ"
vgate_assert B serial-absent "input: usb consumed"

# More than 15 native reports crosses the transfer-ring Link TRB boundary.
vgate_run C -- \
    --input --display \
    --script '$RUN_DIR/script.txt' \
    --input-string "echo m91-usb-wrap"$'\n'"input"$'\n' \
    --input-string-after "userspace: el0=1" \
    --script-expect "input: armed=" \
    --timeout 160

vgate_assert C serial-contains "m91-usb-wrap"
vgate_assert C serial-contains "events=24"
vgate_assert C serial-count "input: usb consumed slot=0x0000000000000001 len=0x0000000000000008" 24
vgate_assert C serial-absent "cvspike: q3 armed"
vgate_assert C serial-absent "[EXC] parking:"
vgate_assert C python <<'PY'
import os, re
from pathlib import Path
serial = Path(os.environ["VG_SER"]).read_text(errors="replace")
if not re.search(r"input: armed=1 fifo=\d+/\d+ dropped=0 events=24\b", serial):
    raise SystemExit("native USB wrap sequence incomplete or FIFO dropped input")
print("native USB completion consumption and typed wrap sequence observed")
PY
