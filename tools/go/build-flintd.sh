#!/usr/bin/env bash
#
# build-flintd.sh -- build FLINTD.ELF, the in-guest FLINT-SYNC/1 read daemon
# (user/go/flintd, M214a issue #3918) with the GOOS=virelai fork toolchain.
# Output: .build/go/FLINTD.ELF
#
# Usage: bash tools/go/build-flintd.sh

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FORK_DIR="${GO_FORK_DIR:-$(dirname "$REPO")/go-virelai}"
DIR="flintd"
NAME="${GO_BUILD_NAME:-FLINTD}"
# Same gap-layout ELF budget as FLINT.ELF: the daemon imports virelai/vi,
# virelai/flintstore (encoding/json + Ed25519) and is well under 8 MiB.
MAX_BYTES=8388608

log() { printf 'build-flintd: %s\n' "$*"; }

if [ ! -x "$FORK_DIR/bin/go" ]; then
    log "missing fork toolchain at $FORK_DIR/bin/go"
    log "provision it once with: bash tools/go/apply.sh && just go-toolchain"
    exit 1
fi

# ADR 0049 Grounding: FLINT uses sshlib.SHA256Sum / EdSign / EdVerify in-tree
# and must not import crypto/* in non-test files. The extracted shared core
# moved that code out of user/go/flint, so the guard covers both the CLI and
# flintstore/flintd (VAL-READ-001).
for pkg in flint flintstore flintd; do
    if (cd "$REPO/user/go" && grep -rEn '"crypto/' "$pkg" --include='*.go' | grep -v '_test\.go'); then
        log "FAIL: user/go/$pkg imports a banned crypto/* package"
        exit 1
    fi
done

GOPATH_DIR="$(mktemp -d "${TMPDIR:-/tmp}/virelai-gopath.XXXXXX")"
trap 'rm -rf "$GOPATH_DIR"' EXIT
mkdir -p "$GOPATH_DIR/src"
ln -sfn "$REPO/user/go" "$GOPATH_DIR/src/virelai"

export GOROOT="$FORK_DIR"
export PATH="$FORK_DIR/bin:$PATH"
export GOTOOLCHAIN=local
export GO111MODULE=off
export GOFLAGS=
export GOPATH="$GOPATH_DIR"
export CGO_ENABLED=0

OUT_DIR="$REPO/.build/go"
mkdir -p "$OUT_DIR"
OUT="$OUT_DIR/$NAME.ELF"

log "building virelai/$DIR -> $OUT"
( cd "$REPO" && GOOS=virelai GOARCH=arm64 go build -o "$OUT" -ldflags "-s -w" "virelai/$DIR" )

SIZE="$(stat -f%z "$OUT" 2>/dev/null || stat -c%s "$OUT")"
log "wrote $OUT ($SIZE bytes)"
if [ "$SIZE" -gt "$MAX_BYTES" ]; then
    log "FAIL: $SIZE bytes exceeds FLINTD 8 MiB budget ($MAX_BYTES)"
    exit 1
fi
log "size ok (< $MAX_BYTES bytes)"
