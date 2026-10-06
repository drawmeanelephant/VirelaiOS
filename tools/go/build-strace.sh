#!/usr/bin/env bash
# Build the importable tracer's CLI and bounded native fixtures. Go 1.27.1's
# BSD-3-Clause distribution and the in-tree overlay are the only inputs.
set -euo pipefail
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FORK_DIR="${GO_FORK_DIR:-$(dirname "$REPO")/go-virelai}"
[ -x "$FORK_DIR/bin/go" ] || {
    echo "build-strace: provision tools/go/apply.sh + just go-toolchain first" >&2
    exit 1
}
GOPATH_DIR="$(mktemp -d "${TMPDIR:-/tmp}/virelai-strace-gopath.XXXXXX")"
trap 'rm -rf "$GOPATH_DIR"' EXIT
mkdir -p "$GOPATH_DIR/src" "$REPO/.build/go"
ln -s "$REPO/user/go" "$GOPATH_DIR/src/virelai"
export GOROOT="$FORK_DIR" PATH="$FORK_DIR/bin:$PATH" GOTOOLCHAIN=local
export GO111MODULE=off GOFLAGS= GOPATH="$GOPATH_DIR" CGO_ENABLED=0
for pair in cmd:STRACE fixture:TRACEFIX; do
    dir="${pair%:*}" name="${pair#*:}"
    GOOS=virelai GOARCH=arm64 go build -o "$REPO/.build/go/$name.ELF" \
        -ldflags "-s -w" "virelai/strace/$dir"
    echo "build-strace: wrote $name.ELF"
done
