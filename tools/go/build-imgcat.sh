#!/usr/bin/env bash
# Build the M85c image terminal client using the existing GOOS=virelai fork.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FORK_DIR="${GO_FORK_DIR:-$(dirname "$REPO")/go-virelai}"
if [ ! -x "$FORK_DIR/bin/go" ]; then
    echo "build-imgcat: missing fork at $FORK_DIR/bin/go; run just go-toolchain" >&2
    exit 1
fi
GOPATH_DIR="$(mktemp -d "${TMPDIR:-/tmp}/virelai-gopath.XXXXXX")"
trap 'rm -rf "$GOPATH_DIR"' EXIT
mkdir -p "$GOPATH_DIR/src" "$REPO/.build/go"
ln -s "$REPO/user/go" "$GOPATH_DIR/src/virelai"
export GOROOT="$FORK_DIR" PATH="$FORK_DIR/bin:$PATH" GOTOOLCHAIN=local
export GO111MODULE=off GOFLAGS= GOPATH="$GOPATH_DIR" CGO_ENABLED=0
OUT="$REPO/.build/go/IMGCAT.ELF"
( cd "$REPO" && GOOS=virelai GOARCH=arm64 go build -ldflags '-s -w' -o "$OUT" virelai/imgcat )
SIZE="$(stat -f%z "$OUT" 2>/dev/null || stat -c%s "$OUT")"
echo "build-imgcat: wrote $OUT ($SIZE bytes)"
if [ "$SIZE" -gt $((32 * 1024 * 1024)) ]; then
    echo "build-imgcat: exceeds kernel exec_image_max (32 MiB)" >&2
    exit 1
fi
