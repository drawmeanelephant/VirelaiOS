#!/usr/bin/env bash
#
# Build the independent host RFB wire probe used by live-rfb.spec.
# Usage: bash tools/go/rfbprobe/build.sh

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
OUT="$REPO/.build/go/rfbprobe"
mkdir -p "$(dirname "$OUT")"

GOTOOLCHAIN=local go build -C "$REPO/tools/go/rfbprobe" -o "$OUT" .
echo "build-rfbprobe: wrote $OUT"
