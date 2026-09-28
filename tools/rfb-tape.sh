#!/usr/bin/env bash
#
# rfb-tape.sh -- M84d class-C demo: a real macOS Screen Sharing viewer
# against the guest seat, through rfbprobe's loopback-only one-viewer bridge
# (ADR 0037 D5(1)) on the hermetic --net stream. Records the guest's own
# scanout as PNGs under gitignored artifacts/. Never gate evidence: the gate
# is live-rfb.spec. Exits 3 with the viewer's transcript when the viewer
# never completes the RFB handshake, instead of producing an empty tape.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${RFB_TAPE_OUT:-$ROOT/artifacts/rfb-tape/$(date -u +%Y%m%dT%H%M%SZ)}"
PORT="${RFB_TAPE_PORT:-5901}"
VIEWER=1

usage() {
    cat <<'EOF'
usage: bash tools/rfb-tape.sh [--dry-run] [--no-viewer]

Boots the seat with --rfb-hermetic, bridges one viewer from 127.0.0.1:$PORT
(RFB_TAPE_PORT, default 5901) into the guest, and opens vnc://127.0.0.1:$PORT
in macOS Screen Sharing. --no-viewer waits up to 120 s for a viewer started
by hand instead. RFB_TAPE_OUT selects the artifact directory. The bridge
accepts exactly one loopback connection and authenticates nobody.
EOF
}

DRY_RUN=0
for arg in "$@"; do
    case "$arg" in
        --dry-run) DRY_RUN=1 ;;
        --no-viewer) VIEWER=0 ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
done

if [ "$DRY_RUN" -eq 1 ]; then
    cat <<EOF
Would build disk, VMRunner (-DSPIKE), rfbprobe, GOTABWM.ELF, and GOCALC.ELF.
Would bridge one viewer on 127.0.0.1:$PORT and $([ "$VIEWER" -eq 1 ] && echo "open vnc://127.0.0.1:$PORT" || echo "wait for a manual viewer").
Would record $OUT/rfb-{5s,10s,15s,after}.png (after = the first frame served to the viewer).
EOF
    exit 0
fi

[ "$(uname -s)" = Darwin ] || { echo "rfb-tape: requires macOS Virtualization.framework" >&2; exit 1; }
cd "$ROOT"
mkdir -p "$OUT/share"
zig build image
swift build --package-path host/vm-runner --configuration release -Xswiftc -DSPIKE
codesign --force --sign - --entitlements host/vm-runner/entitlements.plist \
    host/vm-runner/.build/release/VMRunner
bash tools/go/rfbprobe/build.sh
bash tools/go/build-gotabwm.sh
bash tools/go/build-gocalc.sh

cp -R zig-out/bin/. "$OUT/share/"
cp .build/go/GOTABWM.ELF .build/go/GOCALC.ELF "$OUT/share/"
cp image/apps.txt "$OUT/share/APPS.TXT"
cp image/WALLPAPER.QOI "$OUT/share/"
cp image/fonts/Inter-Regular.ttf "$OUT/share/INTER.TTF"
cp image/fonts/Inter-Bold.ttf "$OUT/share/INTERB.TTF"
cp image/fonts/Inter-Italic.ttf "$OUT/share/INTERI.TTF"
cp image/fonts/FiraCode-Regular.ttf "$OUT/share/FIRACODE.TTF"
printf '#v2\nwm=none\n' >"$OUT/share/SETTINGS.TXT"
cat >"$OUT/boot.txt" <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
set GOMAXPROCS=1
exec GOTABWM.ELF --rfb-hermetic
EOF
cat >"$OUT/calc.txt" <<'EOF'
dui focus 0
exec GOCALC.ELF
EOF

host/vm-runner/.build/release/VMRunner \
    --overlay-base artifacts/disk.img --vars "$OUT/vars.bin" \
    --cvc-file "$OUT/share" --serial "$OUT/serial.log" \
    --screen "$OUT/rfb.png" --screenshot-after 'gotabwm: rfb frame' \
    --net "$OUT/cap.bin" --net-arp-respond 10.0.0.2 \
    --net-tcp-connect 10.0.0.1:5900 \
    --net-tcp-connect-stream .build/go/rfbprobe \
    --net-tcp-connect-stream-arg "-bridge=127.0.0.1:$PORT" \
    --net-tcp-connect-after 'gocalc: present' \
    --script "$OUT/boot.txt" \
    --script2 "$OUT/calc.txt" --script2-after 'gotabwm: win focus' \
    --script-expect 'gotabwm: rfb done' --script-expect-tail 4 --timeout 300 \
    >"$OUT/runner.log" 2>&1 &
RUNNER=$!

for _ in $(seq 1 300); do
    grep -q 'RFBPROBE: bridge listening' "$OUT/runner.log" 2>/dev/null && break
    kill -0 "$RUNNER" 2>/dev/null || break
    sleep 0.5
done
if grep -q 'RFBPROBE: bridge listening' "$OUT/runner.log"; then
    if [ "$VIEWER" -eq 1 ]; then
        open "vnc://127.0.0.1:$PORT"
    else
        echo "rfb-tape: connect a viewer to vnc://127.0.0.1:$PORT within 120 s"
    fi
fi
rc=0
wait "$RUNNER" || rc=$?

grep -a 'RFBPROBE: bridge' "$OUT/runner.log" || true
grep -a 'gotabwm: rfb' "$OUT/serial.log" || true
if ! grep -aq 'gotabwm: rfb frame' "$OUT/serial.log"; then
    echo "rfb-tape: BLOCKED: the viewer was never served a frame (runner rc=$rc); transcript above, logs in $OUT" >&2
    exit 3
fi
python3 - "$OUT" <<'PY'
import os, struct, sys
out = sys.argv[1]
for name in ("rfb-after.png",):
    p = os.path.join(out, name)
    b = open(p, "rb").read()
    if b[:8] != b"\x89PNG\r\n\x1a\n":
        raise SystemExit("not a real scanout PNG: " + p)
    w, h = struct.unpack(">II", b[16:24])
    print("scanout", name, w, h, len(b), "bytes")
PY
if command -v magick >/dev/null 2>&1; then
    magick -delay 100 -loop 0 "$OUT"/rfb-{5s,10s,15s,after}.png "$OUT/rfb.gif" 2>/dev/null || true
fi
echo "rfb-tape: $OUT (demonstration, not gate evidence)"
