#!/usr/bin/env bash
# Build the M95e child fixture (user/go/gsport/childfix) with the
# repository-pinned Go fork — the build-svfixture.sh precedent (#1988).
# Output .build/go/GSCHILD.ELF; the gate stages it under per-phase names
# (GSCHK/GSKILL/GSFAULT/GSLOOP) so a monitor `kill <name>` reaches the
# right incarnation. Both ELFs take the 32 MiB file-exec budget
# (exec_image_max); the tighter 2 MiB staged-path bound does not apply.
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FORK_DIR="${GO_FORK_DIR:-$(dirname "$REPO")/go-virelai}"
if [ ! -x "$FORK_DIR/bin/go" ]; then
    echo "build-gschild: missing pinned fork at $FORK_DIR/bin/go" >&2
    echo "provision with tools/go/apply.sh and tools/go/build-go.sh first" >&2
    exit 1
fi
GOPATH_DIR="$(mktemp -d "${TMPDIR:-/tmp}/virelai-gschild.XXXXXX")"
# The module cache can land under GOPATH when a nested module-mode build
# inherits it; go's cache files are read-only, so un-wedge before removal.
trap 'chmod -R u+w "$GOPATH_DIR" 2>/dev/null || true; rm -rf "$GOPATH_DIR"' EXIT
mkdir -p "$GOPATH_DIR/src"
ln -s "$REPO/user/go" "$GOPATH_DIR/src/virelai"
export GOROOT="$FORK_DIR" GOPATH="$GOPATH_DIR"
export PATH="$FORK_DIR/bin:$PATH"
export GOTOOLCHAIN=local GO111MODULE=off GOFLAGS= CGO_ENABLED=0
mkdir -p "$REPO/.build/go"
OUT="$REPO/.build/go/GSCHILD.ELF"
GOOS=virelai GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o "$OUT" virelai/gsport/childfix
SIZE="$(stat -f%z "$OUT" 2>/dev/null || stat -c%s "$OUT")"
if [ "$SIZE" -gt 33554432 ]; then
    echo "build-gschild: $SIZE bytes exceeds the 33554432-byte application budget" >&2
    exit 1
fi
echo "build-gschild: wrote $OUT ($SIZE bytes)"

# GSPROC.ELF is a Gostalgia-module program (cmd/gsproc in the overlay): it
# needs the pinned stage + transient modfile, which the M95b recipe already
# produces as a byproduct of any target build. Building gssmoke first also
# re-proves the overlay still applies cleanly to the pin.
# The stage build is module-mode: drop this script's GOPATH fixture env so
# the shared recipe resolves modules against the real cache, not mktemp.
export GO_FORK_DIR="$FORK_DIR"
env -u GOPATH -u GO111MODULE bash "$REPO/tools/go/build-gostalgia.sh" gssmoke
STAGE="$REPO/.build/gostalgia-src"
MODFILE="$REPO/.build/gostalgia.mod"
OUT="$REPO/.build/go/GSPROC.ELF"
(cd "$STAGE" && env -u GOPATH GOOS=virelai GOARCH=arm64 GO111MODULE=on \
    GOWORK=off GOPROXY=off \
    go build -mod=mod -modfile "$MODFILE" -p=2 -ldflags "-s -w" \
    -o "$OUT" ./cmd/gsproc)
SIZE="$(stat -f%z "$OUT" 2>/dev/null || stat -c%s "$OUT")"
if [ "$SIZE" -gt 33554432 ]; then
    echo "build-gschild: $SIZE bytes exceeds the 33554432-byte application budget" >&2
    exit 1
fi
echo "build-gschild: wrote $OUT ($SIZE bytes)"
