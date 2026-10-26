#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
source tools/lib/gate-run.sh
trap gate_end EXIT   # drops the private runner copy (issue #3689)

gate_build_lock
zig build -Dbad-handoff=true bad-handoff
gate_build_runner
gate_build_unlock
set +e
"$VZ_RUNNER_BIN" artifacts/bad-handoff.img artifacts/bad-handoff-serial.log --timeout 30 --expect "firmware has agreed to cooperate" > artifacts/m2-bad-handoff-runner.txt 2>&1
RUNNER_RC=$?
set -e
RC="$(python3 image/mkfat32.py --cat-file /RC.TXT artifacts/bad-handoff.img 2>/dev/null || true)"
printf '%s\n' "$RC"
if [ "$RUNNER_RC" -eq 0 ]; then
    echo "bad handoff unexpectedly satisfied the normal runner evidence gate" >&2
    exit 1
fi
printf '%s' "$RC" | grep -Eq 'kernel_rc=0x0*[1-9a-f]' || {
    echo "bad handoff did not produce a non-zero RC.TXT" >&2
    exit 1
}
if grep -q 'VirelaiOS kernel has seized control.' artifacts/bad-handoff-serial.log 2>/dev/null; then
    echo "bad handoff unexpectedly produced the takeover banner" >&2
    exit 1
fi
