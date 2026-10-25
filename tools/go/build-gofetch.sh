#!/usr/bin/env bash
#
# build-gofetch.sh -- convention-named builder for GOFETCH.ELF (issue #3417).
# The product lives behind build-web.sh's target argument (one script builds
# the browser, the fetch CLI, and the git client), so ensure-guest-elf.sh's
# tools/go/build-<lower>.sh map resolves here.
#
# Usage: bash tools/go/build-gofetch.sh [--gate]
#   default -> .build/go/GOFETCH.ELF; --gate -> the AutoClaw-rooted
#   GOFETCH-GATE.ELF the TLS specs stage under the GOFETCH.ELF guest name.

set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
case "${1:-}" in
    "")     bash "$REPO/tools/go/build-web.sh" fetch GOFETCH ;;
    --gate) bash "$REPO/tools/go/build-web.sh" fetch GOFETCH --gate ;;
    *)      echo "usage: build-gofetch.sh [--gate]" >&2; exit 2 ;;
esac
