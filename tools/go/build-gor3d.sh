#!/usr/bin/env bash
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FORK_DIR="${GO_FORK_DIR:-$(dirname "$REPO")/go-virelai}"
if [ ! -x "$FORK_DIR/bin/go" ]; then
    printf 'Missing GOOS=virelai toolchain: %s/bin/go\n' "$FORK_DIR" >&2
    exit 1
fi
GOPATH_DIR="$(mktemp -d "${TMPDIR:-/tmp}/virelai-r3d.XXXXXX")"
trap 'rm -rf "$GOPATH_DIR"' EXIT
mkdir -p "$GOPATH_DIR/src" "$REPO/.build/go"
ln -s "$REPO/user/go" "$GOPATH_DIR/src/virelai"
export GOROOT="$FORK_DIR" GOPATH="$GOPATH_DIR" GOTOOLCHAIN=local GO111MODULE=off GOFLAGS= CGO_ENABLED=0
OUT="$REPO/.build/go/GOR3D.ELF"
GOOS=virelai GOARCH=arm64 "$FORK_DIR/bin/go" build -o "$OUT" -ldflags '-s -w' virelai/r3d
SIZE="$(stat -f%z "$OUT" 2>/dev/null || stat -c%s "$OUT")"
[ "$SIZE" -le $((32 * 1024 * 1024)) ] || { printf 'GOR3D exceeds M72a load_max: %s\n' "$SIZE" >&2; exit 1; }
printf 'GOR3D.ELF: %s bytes\n' "$SIZE"
