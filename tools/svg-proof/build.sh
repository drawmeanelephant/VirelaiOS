#!/usr/bin/env bash
# Offline artifact preparation. Neither a gate nor a dependency installer.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$ROOT/tools/env-check.sh"
FORK="${GO_FORK_DIR:-$ROOT/artifacts/m90-acceptance/go-virelai}"
OUT="$ROOT/artifacts/m90-acceptance"
[[ -x "$FORK/bin/go" ]] || { echo "provision Go fork first" >&2; exit 1; }
[[ "$("$FORK/bin/go" version)" == "go version go1.27.1 darwin/arm64" ]] || exit 1
mkdir -p "$OUT/build" "$OUT/host"
export GOROOT="$FORK" GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off CGO_ENABLED=0 GOOS=virelai GOARCH=arm64 GOFLAGS=
for variant in svg consumer empty; do
    ENGINE="$ROOT/tools/svg-proof/${variant}_guest.go"
    [[ "$variant" != empty ]] || ENGINE="$ROOT/tools/svg-proof/empty.go"
    "$FORK/bin/go" -C "$ROOT/user/go" build -trimpath -ldflags="-s -w" \
        -o "$OUT/build/${variant}.ELF" "$ROOT/tools/svg-proof/guest.go" "$ENGINE"
done
for variant in svg consumer; do
    "$FORK/bin/go" -C "$ROOT/user/go" list -deps "$ROOT/tools/svg-proof/guest.go" \
        "$ROOT/tools/svg-proof/${variant}_guest.go" > "$OUT/build/${variant}-deps.txt"
done
"$FORK/bin/go" -C "$ROOT/user/go" list -deps "$ROOT/tools/svg-proof/guest.go" \
    "$ROOT/tools/svg-proof/empty.go" > "$OUT/build/empty-deps.txt"
env GOOS=darwin GOARCH=arm64 "$FORK/bin/go" -C "$ROOT/user/go" run "$ROOT/tools/svg-proof/host.go" \
    "$ROOT/tests/fixtures/svg/acceptance" "$OUT/host"
python3 "$ROOT/tools/svg-proof/artifacts.py" prepare "$FORK"
