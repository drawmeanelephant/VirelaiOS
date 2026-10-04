#!/usr/bin/env bash
# Offline proof product. Provision separately; ordinary builds only check pins.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$ROOT/tools/env-check.sh"
FORK="${GO_FORK_DIR:-$(dirname "$ROOT")/go-virelai}"
OUT="${PDF_PROOF_OUT:-$ROOT/artifacts/m89-acceptance/build}"
[[ -x "$FORK/bin/go" ]] || { echo MissingToolchain >&2; exit 1; }
python3 "$ROOT/tools/pdf-engine/lock.py" check "$ROOT" "$FORK" "$ROOT/tools/pdf-engine/engine-lock.json"
python3 "$ROOT/tools/pdf-proof/lock.py" check "$ROOT"
mkdir -p "$OUT"
export GOROOT="$FORK" GOOS=virelai GOARCH=arm64 CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS= GOWORK=off
FLAGS=(-trimpath -ldflags="-s -w")
COMMON=("$ROOT/tools/pdf-proof/main.go")
for variant in full empty; do
    "$FORK/bin/go" -C "$ROOT/user/go" build "${FLAGS[@]}" -o "$OUT/$variant.ELF" "${COMMON[@]}" "$ROOT/tools/pdf-proof/$variant.go"
    "$FORK/bin/go" -C "$ROOT/user/go" list -deps "${COMMON[@]}" "$ROOT/tools/pdf-proof/$variant.go" > "$OUT/$variant-deps.txt"
done
GOCACHE="$OUT/reachability-cache" "$FORK/bin/go" -C "$ROOT/user/go" build -trimpath -ldflags="-s -w -dumpdep" -o "$OUT/reachability.ELF" "${COMMON[@]}" "$ROOT/tools/pdf-proof/full.go" > "$OUT/reachable.txt" 2>&1
python3 "$ROOT/tools/pdf-proof/product.py" "$ROOT" "$OUT"
python3 "$ROOT/tools/pdf-engine/runtime_ledger.py" "$ROOT" "$FORK" > "$OUT/runtime-ledger.json"
