#!/usr/bin/env bash
# M1.5 march step 19 gate: the automated `virelai>` transcript test.
#
# Proves the prompt + command loop without manual typing:
#   1. `zig test kernel/src/shell.zig` must pass (the mock-fed end-to-end
#      test asserts the exact transcript in-test).
#   2. That e2e test writes the captured mock output to
#      artifacts/m15-mock-transcript.txt; this gate diffs it byte-for-byte
#      against the canonical, checked-in fixture tests/transcript-console.txt.
#
# The live half of march row 19 (asserting the same bytes in vm-serial.log
# on a real VZ run) stays gated on claim 0002: until the VZ serial gate
# proves a device, the kernel adapter's readByte is an [inferred] no-RX stub
# and keystrokes cannot reach a live VM. The mock transcript exercises the
# same prompt/read/tokenize/exec path the kernel will drive.
#
# Run from the repo root (`zig build test-console`, `just test-console`, CI).
# Note: the e2e test writes artifacts/m15-mock-transcript.txt, so the
# artifacts/ directory must exist on checkout — keep artifacts/.gitkeep
# tracked (it is); a future .gitignore cleanup would break this gate.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

echo "=== transcript gate 1: shell module tests (zig test kernel/src/shell.zig) ==="
zig test kernel/src/shell.zig

echo "=== transcript gate 2: emitted transcript vs canonical fixture ==="
[ -f artifacts/m15-mock-transcript.txt ] || { echo "FAIL: artifacts/m15-mock-transcript.txt missing (did the e2e test run?)"; exit 1; }
[ -f tests/transcript-console.txt ] || { echo "FAIL: tests/transcript-console.txt missing"; exit 1; }

# The remedy is named on failure (issue #3912). This diff is the recurring
# "golden fixture drift" red: a shell change that intentionally moves the
# transcript lands the same way as a real regression, and the raw diff used
# to leave the reader to re-derive the copy from this file's header.
if ! diff -u tests/transcript-console.txt artifacts/m15-mock-transcript.txt; then
    cat >&2 <<'MSG'

verify-transcript: FAIL — the emitted transcript differs from the canonical fixture.

If the transcript change is INTENTIONAL (a shell/help/motd edit moved it), refresh
the golden with the recipe that names this step:

    just regolden-transcript

which is exactly:

    cp artifacts/m15-mock-transcript.txt tests/transcript-console.txt

Then re-run this gate (`just test-console`) and review the refreshed
tests/transcript-console.txt in the diff before committing it.

If the change is NOT intentional, it is a product regression: fix the shell
code rather than the fixture. Re-goldening a regression hides it.
MSG
    exit 1
fi

echo "verify-transcript: PASS — mock transcript is byte-identical to the canonical fixture"
