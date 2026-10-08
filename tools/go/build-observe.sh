#!/usr/bin/env bash
# Build the M94g combined observer (user/go/observe) with the GOOS=virelai
# fork toolchain, plus the GOEDIT symbol donor the profiler leg reads:
# GOEDIT.ELF itself ships stripped (-s -w) via build-goedit.sh, so this
# script also links the identical edit source with -ldflags "-w" only —
# keeping .symtab without DWARF. -s/-w toggle stripping, not layout, so
# the twin's symbols resolve the stripped runtime image's PCs.
#
# Usage: bash tools/go/build-observe.sh
# Outputs: .build/go/OBSERVE.ELF, .build/go/GOEDITSYM.ELF

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FORK_DIR="${GO_FORK_DIR:-$(dirname "$REPO")/go-virelai}"
MAX_BYTES=2097152 # kernel/src/exec.zig exec_program_max

log() { printf 'build-observe: %s\n' "$*"; }

[ -x "$FORK_DIR/bin/go" ] || {
    log "missing fork toolchain at $FORK_DIR/bin/go"
    log "provision it once with: bash tools/go/apply.sh && just go-toolchain"
    exit 1
}

GOPATH_DIR="$(mktemp -d "${TMPDIR:-/tmp}/virelai-observe-gopath.XXXXXX")"
trap 'rm -rf "$GOPATH_DIR"' EXIT
mkdir -p "$GOPATH_DIR/src" "$REPO/.build/go"
ln -sfn "$REPO/user/go" "$GOPATH_DIR/src/virelai"

export GOROOT="$FORK_DIR" PATH="$FORK_DIR/bin:$PATH" GOTOOLCHAIN=local
export GO111MODULE=off GOFLAGS= GOPATH="$GOPATH_DIR" CGO_ENABLED=0

# OBSERVE strips fully like STRACE/HEAP: it symbolizes the *target*, not
# itself, and an unstripped image exceeds the kernel's 2 MiB exec bound.
GOOS=virelai GOARCH=arm64 go build -o "$REPO/.build/go/OBSERVE.ELF" \
    -ldflags "-s -w" "virelai/observe/cmd"
log "wrote OBSERVE.ELF"

# The symbol donor for the stripped GOEDIT the session profiles.
GOOS=virelai GOARCH=arm64 go build -o "$REPO/.build/go/GOEDITSYM.ELF" \
    -ldflags "-w" "virelai/edit"
log "wrote GOEDITSYM.ELF"

python3 - "$REPO" <<'PY'
import os, sys
root = sys.argv[1]
sys.path.insert(0, os.path.join(root, "tools/lib"))
import elf_rules
# OBSERVE is exec'd with arguments, so the full rule set (including the
# argv/envp page-slack rule) applies to it.
path = os.path.join(root, ".build/go", "OBSERVE.ELF")
receipt, errors = elf_rules.check_file(path, "OBSERVE.ELF")
print("build-observe: " + receipt)
if errors:
    sys.exit("\n".join(errors))
if os.path.getsize(path) > 2097152:
    sys.exit("OBSERVE.ELF exceeds exec_program_max")
# GOEDITSYM is never exec'd — it is a symbol donor read over HF — so its
# own page slack is irrelevant (stripped GOEDIT.ELF fails that rule too,
# by the same padding arithmetic). What matters is that .symtab exists.
donor = os.path.join(root, ".build/go", "GOEDITSYM.ELF")
receipt, _ = elf_rules.check_file(donor, "GOEDITSYM.ELF")
print("build-observe: " + receipt)
PY
if [ "$(go tool nm "$REPO/.build/go/GOEDITSYM.ELF" | wc -l)" -lt 100 ]; then
    log "GOEDITSYM.ELF has no usable symbol table"
    exit 1
fi
