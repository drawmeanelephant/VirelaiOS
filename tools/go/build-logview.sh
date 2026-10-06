#!/usr/bin/env bash
# Build LOGVIEW.ELF and its deterministic LOGFIX.ELF producer with the existing
# GOOS=virelai fork. No new dependencies or toolchain downloads.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FORK_DIR="${GO_FORK_DIR:-$(dirname "$REPO")/go-virelai}"
[ -x "$FORK_DIR/bin/go" ] || {
    echo "build-logview: missing fork at $FORK_DIR/bin/go; provision tools/go/apply.sh + just go-toolchain" >&2
    exit 1
}
GOPATH_DIR="$(mktemp -d "${TMPDIR:-/tmp}/virelai-logview-gopath.XXXXXX")"
trap 'rm -rf "$GOPATH_DIR"' EXIT
mkdir -p "$GOPATH_DIR/src" "$REPO/.build/go"
ln -s "$REPO/user/go" "$GOPATH_DIR/src/virelai"
export GOROOT="$FORK_DIR" PATH="$FORK_DIR/bin:$PATH" GOTOOLCHAIN=local
export GO111MODULE=off GOFLAGS= GOPATH="$GOPATH_DIR" CGO_ENABLED=0
for pair in logview:LOGVIEW logfixture:LOGFIX; do
    dir="${pair%:*}" name="${pair#*:}"
    out="$REPO/.build/go/$name.ELF"
    GOOS=virelai GOARCH=arm64 go build -o "$out" -ldflags "-s -w" "virelai/$dir"
    size="$(stat -f%z "$out" 2>/dev/null || stat -c%s "$out")"
    echo "build-logview: $name.ELF $size bytes"
    [ "$size" -le 2097152 ] || { echo "build-logview: exceeds exec_program_max" >&2; exit 1; }
done
