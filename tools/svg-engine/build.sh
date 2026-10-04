#!/usr/bin/env bash
# Offline build recipe, not a verification gate. Provision the fork first.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$ROOT/tools/env-check.sh"
FORK="${GO_FORK_DIR:-$(dirname "$ROOT")/go-virelai}"
OUT="${SVG_ENGINE_OUT:-$ROOT/artifacts/m90-engine}"
[[ -x "$FORK/bin/go" ]] || { echo "svg-engine: provision/build the local Go fork first" >&2; exit 1; }
[[ "$("$FORK/bin/go" version)" == "go version go1.27.1 darwin/arm64" ]] || { echo "svg-engine: expected pinned Go 1.27.1 host compiler" >&2; exit 1; }
mkdir -p "$OUT"
export GOROOT="$FORK" GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off CGO_ENABLED=0 GOOS=virelai GOARCH=arm64 GOFLAGS=
COMMON=(-trimpath -ldflags="-s -w")
"$FORK/bin/go" -C "$ROOT/user/go" build "${COMMON[@]}" -o "$OUT/EMPTY.ELF" "$ROOT/tools/svg-engine/adapter.go" "$ROOT/tools/svg-engine/empty.go"
"$FORK/bin/go" -C "$ROOT/user/go" build "${COMMON[@]}" -o "$OUT/FULL.ELF" "$ROOT/tools/svg-engine/adapter.go" "$ROOT/tools/svg-engine/engine.go"
"$FORK/bin/go" -C "$ROOT/user/go" list -deps "$ROOT/tools/svg-engine/adapter.go" "$ROOT/tools/svg-engine/engine.go" > "$OUT/dependencies.txt"
"$FORK/bin/go" -C "$ROOT/user/go" list -deps "$ROOT/tools/svg-engine/adapter.go" "$ROOT/tools/svg-engine/empty.go" > "$OUT/empty-dependencies.txt"
python3 "$ROOT/tools/svg-engine/elf_inspect.py" "$OUT/EMPTY.ELF" "$OUT/FULL.ELF" "$OUT/dependencies.txt" "$OUT/empty-dependencies.txt" > "$OUT/elf.json"
python3 "$ROOT/tools/svg-engine/ledger.py" "$ROOT" "$FORK" > "$OUT/runtime-ledger.json"
