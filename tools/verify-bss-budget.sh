#!/usr/bin/env bash
#
# verify-bss-budget.sh -- ADR 0013 D3.1 CI gate: enforce a hard .bss ceiling
# for the kernel ELF.
#
# Deterministic, no VM, no VZ -- pure host-side class A. Runs in CI and as
# `bash tools/verify-bss-budget.sh` (also called from `just verify-portable`).
#
# What this guards against
# -----------------------
# The kernel `.bss` is a hard architectural constraint (no allocator, fixed
# 1 MiB MMU page-table carve-out + the rest is data BSS). A silent global
# `var foo: [1 << 20]u8` would push `.bss` past the carve-out and break the
# build or runtime layout. This gate makes that impossible to merge silently:
# it inspects the linked ELF, reports the measured `.bss` size + remaining
# headroom, and fails the build if the budget is exceeded.
#
# Budget source of truth
# ----------------------
# ADR 0013 D3.1 (docs/decisions/0013-post-m14-abi-amendment.md), observed
# 2026-08-20 via `zig build kernel` + `llvm-readelf -SW`:
#
#   baseline .bss = 9,787,576 B (9.33 MiB, as of main post-M16)
#   post-M14 reservations = +1,737 B (ADR 0013 D3 -- planned)
#   headroom = ~1.7 MiB for future claims
#
# The budget is the pinned constant below (15,728,640 B = 15.0 MiB after the
# #1163 and #3156 raises; ADR 0013 D3.1 carries each raise's observed
# measurement + justification). To raise the budget, amend ADR 0013 D3.1 with
# the observed post-change measurement and a justification, then bump the
# constant below.
#
# What this does NOT do
# ---------------------
# - Does NOT reproduce the experimental canary (ADR 0013 D3.1's LTO lesson).
# - Does NOT artificially retain unused reservation stubs (production code
#   keeps its own allocations alive; this gate enforces the ceiling only).
# - Does NOT enumerate ELF symbols (the kernel ELF is stripped in ReleaseSmall
#   -- see kernel/linker.ld); the "largest contributors" section is sourced
#   from static-array declarations in the kernel source.
#
# Usage: bash tools/verify-bss-budget.sh
# Evidence saved under artifacts/bss-budget-gate.txt.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GATE_LOG="artifacts/bss-budget-gate.txt"
mkdir -p "$(dirname "$GATE_LOG")"

# Budget ceiling. Override at run time for ad-hoc checks:
#   BSS_BUDGET_BYTES=8000000 bash tools/verify-bss-budget.sh
BSS_BUDGET_BYTES="${BSS_BUDGET_BYTES:-15728640}" # 15.0 MiB (ADR 0013 D3.1, amended for issue #3156)
BSS_BUDGET_MIB="$(awk -v b="$BSS_BUDGET_BYTES" 'BEGIN { printf "%.1f", b / 1048576 }')"
# Discover llvm-readelf: env override > PATH > Homebrew > Xcode > fail.
if [ -z "${LLVM_READELF:-}" ]; then
    LLVM_READELF="$(command -v llvm-readelf 2>/dev/null || true)"
fi
if [ -z "${LLVM_READELF:-}" ]; then
    # Homebrew — find the first llvm-readelf under Cellar/opt (symlinks ok)
    LLVM_READELF="$(find /opt/homebrew/Cellar /opt/homebrew/opt /usr/local/opt \
        -name llvm-readelf -not -type d 2>/dev/null | head -1 || true)"
fi
if [ -z "${LLVM_READELF:-}" ]; then
    for p in /Library/Developer/CommandLineTools/usr/bin/llvm-readelf \
             /Applications/Xcode*.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/bin/llvm-readelf; do
        [ -e "$p" ] && LLVM_READELF="$p" && break
    done
fi
if [ -z "${LLVM_READELF:-}" ]; then
    echo "verify-bss-budget: FAIL — llvm-readelf not found. Set LLVM_READELF=path."
    exit 1
fi

echo "=== verify-bss-budget: ADR 0013 D3.1 CI gate — kernel .bss ceiling (class A, deterministic, no VM) ==="
echo "budget source: docs/decisions/0013-post-m14-abi-amendment.md D3.1"
echo "budget:        $BSS_BUDGET_BYTES B ($BSS_BUDGET_MIB MiB)"
echo

# Build the kernel. `zig build kernel` runs elf2bin.py on the linked ELF;
# the linked ELF (the input to elf2bin) is what we inspect, and it lives
# at .zig-cache/o/*/virelai-kernel.
echo "-- building kernel --"
rm -rf .zig-cache
zig build kernel 2>&1 | tail -1

KERNEL_ELF="$(find .zig-cache -name 'virelai-kernel' -type f 2>/dev/null | head -1 || true)"
if [ -z "$KERNEL_ELF" ]; then
    echo
    echo "verify-bss-budget: FAIL — kernel ELF not found in .zig-cache after build."
    sleep 0.5
    exit 1
fi

# Extract `.bss` size. llvm-readelf -SW prints a fixed-width table; on
# this host the `[Nr]` bracket is whitespace-split so `.bss` is field 3
# and the size is field 7. Use grep + awk for clarity over field-index
# arithmetic, and verify each section name explicitly so a future
# llvm-readelf change doesn't silently shift the columns.
section_hex() { # section_name -> hex_size (empty on miss)
    local name="$1"
    "$LLVM_READELF" -SW "$KERNEL_ELF" \
        | awk -v want="$name" '$3 == want { print $7; exit }' || true
}
BSS_SIZE_HEX="$(section_hex .bss)"
if [ -z "$BSS_SIZE_HEX" ]; then
    echo
    echo "verify-bss-budget: FAIL — llvm-readelf produced no .bss row for $KERNEL_ELF."
    sleep 0.5
    exit 1
fi
BSS_SIZE_DEC=$((16#$BSS_SIZE_HEX))
REMAINING=$((BSS_BUDGET_BYTES - BSS_SIZE_DEC))
OVER=$((BSS_SIZE_DEC - BSS_BUDGET_BYTES))

# .data and .userbss sections for the architectural-context line below.
DATA_SIZE_HEX="$(section_hex .data)"
USERBSS_SIZE_HEX="$(section_hex .userbss)"
DATA_SIZE_DEC=$((16#$DATA_SIZE_HEX))
USERBSS_SIZE_DEC=$((16#$USERBSS_SIZE_HEX))

echo
echo "-- kernel ELF section sizes --"
printf '  .bss:           %d B  (0x%s)\n' "$BSS_SIZE_DEC" "$BSS_SIZE_HEX"
printf '  .data:          %d B  (0x%s)\n' "$DATA_SIZE_DEC" "$DATA_SIZE_HEX"
printf '  .userbss:       %d B  (0x%s)\n' "$USERBSS_SIZE_DEC" "$USERBSS_SIZE_HEX"

echo
echo "-- budget check --"
printf '  measured:       %d B\n' "$BSS_SIZE_DEC"
printf '  budget:         %d B\n' "$BSS_BUDGET_BYTES"
printf '  remaining:      %d B\n' "$REMAINING"

STATUS=PASS
if [ "$BSS_SIZE_DEC" -gt "$BSS_BUDGET_BYTES" ]; then
    STATUS=FAIL
    printf '  over budget:    %d B\n' "$OVER"
else
    printf '  over budget:    0 B\n'
fi
printf '  status:         %s\n' "$STATUS"

# Largest known contributors (informational; sourced from source, not the
# stripped ELF). Update this table whenever a new large static array is
# introduced in the kernel -- keeping it honest is the only way this section
# remains useful.
echo
echo "-- known large .bss contributors (sourced from kernel/src; informational) --"
echo "  mmu.table_storage (kernel/src/mmu.zig):     2,097,152 B (2.0 MiB; page-table carve-out, ADR 0006, doubled in M16 C4)"
echo "  virtio_gpu.gpu_fb (kernel/src/virtio_gpu.zig): 3,686,400 B (3.52 MiB; 1280x720x4 scanout, align 4096)"
echo "  socket_core.rx_pool (kernel/src/socket_core.zig):     73,728 B (72 KiB; 4 x 16 KiB rx window + 2 KiB bitmap, ADR 0007 D11)"
echo "  post-M14 reservations (ADR 0013 D3):           1,737 B (~1.7 KiB; planned, not yet landed)"
echo "  other (font, console, scheduler, virtio rings, process table, mailbox, evidence, etc.): the remainder"

if [ "$STATUS" = "FAIL" ]; then
    echo
    echo "verify-bss-budget: FAILED — kernel .bss exceeded the $BSS_BUDGET_MIB MiB ceiling."
    echo "Either:"
    echo "  1. Remove the offending allocation (preferred -- amend the design)."
    echo "  2. Amend ADR 0013 D3.1 with the post-change measurement + justification,"
    echo "     then bump BSS_BUDGET_BYTES in this script."
    exit 1
fi

echo
echo "verify-bss-budget: PASS — kernel .bss = $BSS_SIZE_DEC B / $BSS_BUDGET_BYTES B ($REMAINING B headroom)."
exit 0
