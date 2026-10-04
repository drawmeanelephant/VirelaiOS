#!/usr/bin/env bash
# Offline product build, not a gate. Provision and build the fork separately.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$ROOT/tools/env-check.sh"
FORK="${GO_FORK_DIR:-$(dirname "$ROOT")/go-virelai}"
OUT="${PDF_ENGINE_OUT:-$ROOT/artifacts/m89-engine}"
[[ -x "$FORK/bin/go" ]] || { echo MissingToolchain >&2; exit 1; }
python3 "$ROOT/tools/pdf-engine/lock.py" check "$ROOT" "$FORK" "$ROOT/tools/pdf-engine/engine-lock.json"
mkdir -p "$OUT"
export GOROOT="$FORK" GOOS=virelai GOARCH=arm64 CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS= GOWORK=off
FLAGS=(-trimpath -ldflags="-s -w")
"$FORK/bin/go" -C "$ROOT/user/go" build "${FLAGS[@]}" -o "$OUT/EMPTY.ELF" "$ROOT/tools/pdf-engine/adapter.go" "$ROOT/tools/pdf-engine/empty.go"
"$FORK/bin/go" -C "$ROOT/user/go" build "${FLAGS[@]}" -o "$OUT/FULL.ELF" "$ROOT/tools/pdf-engine/adapter.go" "$ROOT/tools/pdf-engine/full.go"
"$FORK/bin/go" -C "$ROOT/user/go" list -deps "$ROOT/tools/pdf-engine/adapter.go" "$ROOT/tools/pdf-engine/empty.go" > "$OUT/empty-deps.txt"
"$FORK/bin/go" -C "$ROOT/user/go" list -deps "$ROOT/tools/pdf-engine/adapter.go" "$ROOT/tools/pdf-engine/full.go" > "$OUT/full-deps.txt"
"$FORK/bin/go" -C "$ROOT/user/go" build -trimpath -ldflags="-s -w -dumpdep" -o "$OUT/REACHABILITY.ELF" "$ROOT/tools/pdf-engine/adapter.go" "$ROOT/tools/pdf-engine/full.go" > "$OUT/reachable.txt" 2>&1
python3 - "$ROOT/tools/pdf-engine" "$OUT/reachable.txt" <<'PY'
import sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from elf import reachable
reachable(Path(sys.argv[2]).read_text())
PY
python3 "$ROOT/tools/pdf-engine/elf.py" "$OUT/EMPTY.ELF" "$OUT/FULL.ELF" "$OUT/empty-deps.txt" "$OUT/full-deps.txt" > "$OUT/elf.json"
python3 "$ROOT/tools/pdf-engine/runtime_ledger.py" "$ROOT" "$FORK" > "$OUT/runtime-ledger.json"
