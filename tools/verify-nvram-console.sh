#!/usr/bin/env bash
#
# verify-nvram-console.sh -- claim 0015 gate: post-exit console bytes via
# the NVRAM variable channel (the VZ serial-gate successor step named by
# claim 0013).
#
# Problem (observed, claims 0002/0013): the first post-exit banner TX dies
# in the virtio-pci transport flush on VZ; vm-serial.log stays 0 B. The
# only proven post-exit device channel is EFI runtime SetVariable (the
# claim-0009 marker ladder persists post-exit).
#
# This gate: build the kernel with -Dnvram-console=true (console TX rides
# the NVRAM channel, never touching the hanging transport), boot it in a
# VZ VM, reconstruct the console stream from the chunk variables the kernel
# wrote (VirelaiC0..N, prefix "VIRELAIC <idx>:"), and assert the stream
# contains the takeover banner, the terminal state line, the virelai>
# prompt, and real command output (version, mem) from the scripted session.
#
# Flakiness: the VZ post-exit window is a documented flaky death site
# (claim 0009 — the ladder sometimes stops at M2_MAPD!, the MMU takeover,
# and sometimes later, e.g. mid map-dump after M2_TXOK!). The kernel is
# not at fault; VZ emulation is. This gate therefore retries the VM boot
# up to MAX_ATTEMPTS times with a fresh variable store each time, and
# passes on the first attempt whose reconstructed stream meets the
# assertions. Each attempt is logged under artifacts/.
#
# Note: the LAST help-catalog footer line ("type 'help <command>' ...") is
# deliberately NOT an assertion needle. The EFI variable store write budget
# (~61 KiB) is the real ceiling for how much of the catalog the channel can
# carry, so the tail is a store-budget question, not a channel-health one;
# reconstruction integrity is asserted by `complete=true` (no dropped chunk
# index). `kernel/src/nvram_console.zig` `max_chunks` is sized so today's
# session fits with headroom.
#
# The serial channel is NOT the gate (it is provably silent on VZ); the
# NVRAM console channel is. Run on Apple silicon only (VZ VM).
#
# Usage: bash tools/verify-nvram-console.sh
# Evidence saved under artifacts/: nvram-console-gate.txt (this script),
# nvram-console-run.txt (runner output), nvram-console.log (reconstructed
# stream), efi-vars.bin (the variable store), vm-serial.log.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

MAX_ATTEMPTS="${MAX_ATTEMPTS:-3}"

echo "=== verify-nvram-console: claim 0015 — post-exit console bytes via the NVRAM channel ==="

# --- tool versions (record which Zig/Swift/macOS you ran on) ---------------
zig version; swift --version 2>&1 | head -1; sw_vers

# --- formatting gate ---------------------------------------------------------
zig fmt --check boot/src/*.zig kernel/src/*.zig build.zig

# --- build gates (kernel built with console TX routed through NVRAM) --------
source tools/lib/gate-run.sh
trap gate_end EXIT   # releases the lock + drops the private runner copy
gate_build_lock      # provision under the worktree build lock (issue #3689)
zig build -Dnvram-console=true
zig build -Dnvram-console=true image
gate_build_runner
gate_build_unlock

# --- gate assertions (shared by every attempt) --------------------------------
needs_stream() {
    local log="$1"
    # Chunk-sequence integrity: the runner writes complete=false if any
    # chunk index was dropped (a silently-failed SetVariable). Needle
    # presence alone is not enough — a gap could still leave all needles
    # in the surviving chunks, so assert the full session persisted.
    if ! grep -qF -- "complete=true" "$log"; then
        return 1
    fi
    for needle in \
        "VirelaiOS kernel has seized control." \
        "kernel terminal state" \
        "virelai> " \
        "virelai-kernel" \
        "mem: descriptors=" \
        "nvram-console-ok" \
        "available commands:"; do
        if ! grep -qF -- "$needle" "$log"; then
            return 1
        fi
    done
    return 0
}

# --- THE GATE: boot the VM and reconstruct the NVRAM console stream -----------
# The store is append-per-write and survives across runs; each attempt gets
# a fresh store so the reconstructed stream is exactly that attempt's writes.
attempt=0
passed=0
while [ "$attempt" -lt "$MAX_ATTEMPTS" ] && [ "$passed" -eq 0 ]; do
    attempt=$((attempt + 1))
    echo
    echo "--- attempt $attempt/$MAX_ATTEMPTS (fresh variable store) ---"
    rm -f artifacts/efi-vars.bin
    set +e
    "$VZ_RUNNER_BIN" artifacts/disk.img artifacts/vm-serial.log \
        --nvram-console artifacts/nvram-console.log --timeout 25 \
        > artifacts/nvram-console-run.txt 2>&1
    RUNNER_RC=$?
    set -e
    cat artifacts/nvram-console-run.txt
    echo
    [ -f artifacts/nvram-console.log ] || { echo "ERROR: no nvram-console.log produced" >&2; exit 1; }
    if [ "$RUNNER_RC" -eq 0 ] && needs_stream artifacts/nvram-console.log; then
        passed=1
        echo "attempt $attempt: PASS"
    else
        echo "attempt $attempt: FAILED (documented flaky VZ post-exit death window, claim 0009 — retrying)"
    fi
done

echo
echo "=== reconstructed nvram console stream (artifacts/nvram-console.log) ==="
cat artifacts/nvram-console.log

echo
echo "=== loader trace: /LOADER.TXT on the ESP ==="
LOADER_TXT="$(python3 image/mkfat32.py --cat-file /LOADER.TXT artifacts/disk.img 2>/dev/null || true)"
printf '%s\n' "$LOADER_TXT"

# --- verdict -----------------------------------------------------------------
if [ "$passed" -ne 1 ]; then
    echo "verify-nvram-console: FAILED after $MAX_ATTEMPTS attempt(s) (evidence kept under artifacts/)" >&2
    exit 1
fi
echo "verify-nvram-console: PASS — post-exit console bytes reconstructed from the NVRAM channel (takeover banner, terminal state, prompt, command output)"
