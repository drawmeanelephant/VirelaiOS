#!/usr/bin/env bash
# Host recording of the M85c window's real scanout, never gate evidence.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${TERM_GRAPHICS_TAPE_OUT:-$ROOT/artifacts/term-graphics-tape/$(date -u +%Y%m%dT%H%M%SZ)}"
case "${1:-}" in
    --dry-run)
        echo "Would build image, VMRunner, IMGCAT.ELF, then record $OUT/image-{5s,10s,15s,after}.png from VM scanout."
        exit 0 ;;
    -h|--help)
        echo "usage: bash tools/term-graphics-tape.sh [--dry-run] (TERM_GRAPHICS_TAPE_OUT selects artifact directory)"
        exit 0 ;;
    "") ;;
    *) echo "usage: bash tools/term-graphics-tape.sh [--dry-run]" >&2; exit 2 ;;
esac
[ "$(uname -s)" = Darwin ] || { echo "term-graphics-tape: requires macOS VZ" >&2; exit 1; }
cd "$ROOT"
mkdir -p "$OUT/share"
zig build image
swift build --package-path host/vm-runner --configuration release -Xswiftc -DSPIKE
codesign --force --sign - --entitlements host/vm-runner/entitlements.plist \
    host/vm-runner/.build/release/VMRunner
bash tools/go/build-imgcat.sh
cp -R zig-out/bin/. "$OUT/share/"
cp .build/go/IMGCAT.ELF "$OUT/share/"
cp user/go/imgcat/testdata/quadrants.qoi "$OUT/share/QUADS.QOI"
cp image/apps.txt "$OUT/share/APPS.TXT"
cp image/WALLPAPER.QOI "$OUT/share/"
cp image/fonts/Inter-Regular.ttf "$OUT/share/INTER.TTF"
cp image/fonts/Inter-Bold.ttf "$OUT/share/INTERB.TTF"
cp image/fonts/Inter-Italic.ttf "$OUT/share/INTERI.TTF"
cp image/fonts/FiraCode-Regular.ttf "$OUT/share/FIRACODE.TTF"
cat >"$OUT/boot.txt" <<'EOF'
exec IMGCAT.ELF /host/QUADS.QOI
EOF
cat >"$OUT/scroll.txt" <<'EOF'
echo tape-scroll
EOF
cat >"$OUT/image.txt" <<'EOF'
echo tape-image
EOF
# The typed s/c are actual bound-tty input. The host frame captures use the
# Virtualization.framework display, not a serial or ANSI reconstruction.
host/vm-runner/.build/release/VMRunner \
    --overlay-base artifacts/disk.img --vars "$OUT/vars.bin" \
    --cvc-file "$OUT/share" --serial "$OUT/serial.log" \
    --screen "$OUT/image.png" --screenshot-after 'imgcat: cleared' \
    --input --via-virtio \
    --script "$OUT/boot.txt" \
    --script2 "$OUT/image.txt" --script2-after 'imgcat: image 16x32' --script2-delay 5 \
    --input-chords 's' --input-chords-after tape-image \
    --script3 "$OUT/scroll.txt" --script3-after 'imgcat: scrolled' --script3-delay 5 \
    --input-string 'c' --input-string-after tape-scroll \
    --script-expect 'imgcat: cleared' --script-expect-tail 8 --timeout 90
python3 - "$OUT" <<'PY'
import os, struct, sys
out = sys.argv[1]
for name in ("image-5s.png", "image-10s.png", "image-15s.png", "image-after.png"):
    p = os.path.join(out, name)
    b = open(p, "rb").read()
    if b[:8] != b"\x89PNG\r\n\x1a\n":
        raise SystemExit("not a real scanout PNG: " + p)
    w, h = struct.unpack(">II", b[16:24])
    if (w, h) != (2560, 1440):
        raise SystemExit("unexpected scanout dimensions: " + p)
    print("scanout", name, w, h, len(b), "bytes")
PY
if command -v magick >/dev/null 2>&1; then
    magick -delay 50 -loop 0 "$OUT"/image-{5s,10s,15s,after}.png "$OUT/image.gif"
fi
echo "term-graphics-tape: $OUT (demonstration, not gate evidence)"
