#!/usr/bin/env bash
# M93f: build production GOTGIT or a separately named hermetic root artifact.
# The existing build-gogit.sh remains unchanged for its other callers.
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
case "${1:-}" in
    "")     bash "$REPO/tools/go/build-web.sh" git GOTGIT ;;
    --gate) bash "$REPO/tools/go/build-web.sh" git GOTGIT --gate ;;
    *)      echo "usage: build-git.sh [--gate]" >&2; exit 2 ;;
esac
