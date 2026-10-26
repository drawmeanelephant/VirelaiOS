#!/usr/bin/env bash
#
# test-ensure-guest-elf.sh -- class-A self-test for tools/go/ensure-guest-elf.sh
# (issue #1503). A checker whose negative path is never exercised can be
# wired backwards and still report green, so the stale/missing/foreign-ELF
# refusals are asserted here with no VM and no Go toolchain.
#
# Hermetic: ENSURE_GUEST_ELF_ROOT points at a temp tree; builders are stubs
# that write a byte string. The real live-sh / live-ssh-server specs are
# read only to pin needed-by detection.
#
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
SH="$ROOT/tools/go/ensure-guest-elf.sh"

PASS=0 FAIL=0
ok()  { PASS=$((PASS + 1)); echo "  ok    $1"; }
bad() { FAIL=$((FAIL + 1)); echo "  FAIL  $1"; }

TMP="$(mktemp -d "${TMPDIR:-/tmp}/virelai-test-ensure-guest-elf.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

FIXTURE="$TMP/tree"
mkdir -p "$FIXTURE/user/go/sh" "$FIXTURE/user/go/sshd" "$FIXTURE/tools/go"
printf 'package sh\n' > "$FIXTURE/user/go/sh/main.go"
printf 'package sshd\n' > "$FIXTURE/user/go/sshd/main.go"
printf '# fake build-gosh\n' > "$FIXTURE/tools/go/build-gosh.sh"
printf '# fake build-sshd\n' > "$FIXTURE/tools/go/build-sshd.sh"

export ENSURE_GUEST_ELF_ROOT="$FIXTURE"
export ENSURE_GUEST_ELF_TOOLCHAIN=test
unset ENSURE_GUEST_ELF_OUTDIR || true

cat > "$TMP/build-gosh.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
mkdir -p "${ENSURE_GUEST_ELF_ROOT}/.build/go"
printf '%s' "${FAKE_GOSH_PAYLOAD:-gosh-v1}" > "${ENSURE_GUEST_ELF_ROOT}/.build/go/GOSH.ELF"
printf 'gosh\n' >> "${ENSURE_GUEST_ELF_ROOT}/build-log"
SH
cat > "$TMP/build-sshd.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
mkdir -p "${ENSURE_GUEST_ELF_ROOT}/.build/go"
printf '%s' "${FAKE_SSHD_PAYLOAD:-sshd-v1}" > "${ENSURE_GUEST_ELF_ROOT}/.build/go/GOSSHD.ELF"
printf 'sshd\n' >> "${ENSURE_GUEST_ELF_ROOT}/build-log"
SH
chmod +x "$TMP/build-gosh.sh" "$TMP/build-sshd.sh"
export ENSURE_GUEST_ELF_BUILD_GOSH="$TMP/build-gosh.sh"
export ENSURE_GUEST_ELF_BUILD_GOSSHD="$TMP/build-sshd.sh"

run() {
    # Capture stdout+stderr and rc. Always returns 0 so the caller can
    # inspect CASE_RC / CASE_OUT (set -e stays off for the test body).
    CASE_OUT="$(bash "$SH" "$@" 2>&1)"
    CASE_RC=$?
    return 0
}

echo "=== test-ensure-guest-elf: a stale/foreign ELF is a named build error ==="

# --- missing / stale / check ---
run check GOSH
[ "$CASE_RC" != 0 ] && ok "check GOSH on a missing ELF is non-zero (rc=$CASE_RC)" \
    || bad "check GOSH must fail when the ELF is missing, got rc=$CASE_RC"
case "$CASE_OUT" in
    *"ensure-guest-elf: GOSH.ELF missing"*) ok "missing GOSH names ensure-guest-elf: GOSH.ELF missing" ;;
    *) bad "missing GOSH should name the binary: $CASE_OUT" ;;
esac
case "$CASE_OUT" in
    *"this gate refuses to boot a binary the current tree did not produce"*) \
        ok "the missing/stale refusal says it will not boot" ;;
    *) bad "refusal must say it will not boot: $CASE_OUT" ;;
esac
case "$CASE_OUT" in
    *"rebuild: bash tools/go/build-gosh.sh"*) ok "missing GOSH names the builder" ;;
    *) bad "missing GOSH should name tools/go/build-gosh.sh: $CASE_OUT" ;;
esac

mkdir -p "$FIXTURE/.build/go"
printf 'foreign-from-another-branch' > "$FIXTURE/.build/go/GOSH.ELF"
run check GOSH
[ "$CASE_RC" != 0 ] && ok "check GOSH on a stamp-less hand-built ELF is non-zero" \
    || bad "a stamp-less ELF must fail check, got rc=$CASE_RC"
case "$CASE_OUT" in
    *"ensure-guest-elf: GOSH.ELF stale"*) ok "stamp-less GOSH names ensure-guest-elf: GOSH.ELF stale" ;;
    *) bad "stamp-less GOSH should be named stale: $CASE_OUT" ;;
esac

printf 'foreign-sshd' > "$FIXTURE/.build/go/GOSSHD.ELF"
run check GOSSHD
[ "$CASE_RC" != 0 ] && ok "check GOSSHD on a stamp-less ELF is non-zero" \
    || bad "stamp-less GOSSHD must fail check"
case "$CASE_OUT" in
    *"ensure-guest-elf: GOSSHD.ELF stale"*) ok "stamp-less GOSSHD names ensure-guest-elf: GOSSHD.ELF stale" ;;
    *) bad "stamp-less GOSSHD should be named stale: $CASE_OUT" ;;
esac
case "$CASE_OUT" in
    *"rebuild: bash tools/go/build-sshd.sh"*) ok "stale GOSSHD names the builder" ;;
    *) bad "stale GOSSHD should name tools/go/build-sshd.sh: $CASE_OUT" ;;
esac

# --- ensure rebuilds once, then cache-hits ---
: > "$FIXTURE/build-log"
run ensure GOSH
[ "$CASE_RC" = 0 ] && ok "ensure GOSH rebuilds a stale ELF (rc=0)" \
    || bad "ensure GOSH should rebuild, got rc=$CASE_RC out=$CASE_OUT"
case "$CASE_OUT" in
    *"ensure-guest-elf: GOSH.ELF stale"*) ok "ensure prints the named stale error before rebuilding" ;;
    *) bad "ensure should print the named stale error: $CASE_OUT" ;;
esac
case "$CASE_OUT" in
    *"ensure-guest-elf: rebuilding GOSH.ELF"*) ok "ensure says it is rebuilding GOSH.ELF" ;;
    *) bad "ensure should announce the rebuild: $CASE_OUT" ;;
esac
builds="$(grep -c '^gosh$' "$FIXTURE/build-log" || true)"
[ "$builds" = 1 ] && ok "the GOSH builder ran once" \
    || bad "GOSH builder should run once, ran $builds"

run ensure GOSH
[ "$CASE_RC" = 0 ] && ok "second ensure GOSH is a cache hit (rc=0)" \
    || bad "fresh ensure should pass: $CASE_OUT"
case "$CASE_OUT" in
    *"ensure-guest-elf: GOSH.ELF fresh"*) ok "cache hit names GOSH.ELF fresh" ;;
    *) bad "cache hit should say fresh: $CASE_OUT" ;;
esac
builds="$(grep -c '^gosh$' "$FIXTURE/build-log" || true)"
[ "$builds" = 1 ] && ok "the GOSH builder did not run again on a matching stamp" \
    || bad "cache hit rebuilt ($builds times)"

run check GOSH
[ "$CASE_RC" = 0 ] && ok "check GOSH passes after ensure" \
    || bad "check after ensure should pass: $CASE_OUT"

run ensure GOSSHD
[ "$CASE_RC" = 0 ] && ok "ensure GOSSHD rebuilds a stale ELF" \
    || bad "ensure GOSSHD failed: $CASE_OUT"
run check GOSSHD
[ "$CASE_RC" = 0 ] && ok "check GOSSHD passes after ensure" \
    || bad "check GOSSHD after ensure should pass: $CASE_OUT"

# --- source change invalidates ---
printf 'package sh\n// touched\n' > "$FIXTURE/user/go/sh/main.go"
run check GOSH
[ "$CASE_RC" != 0 ] && ok "a source edit makes GOSH.ELF stale" \
    || bad "source edit should invalidate the stamp: $CASE_OUT"
case "$CASE_OUT" in
    *"ensure-guest-elf: GOSH.ELF stale"*) ok "source-edit refusal is still named GOSH.ELF stale" ;;
    *) bad "source edit should be named stale: $CASE_OUT" ;;
esac

# Restore sources, keep the ELF+stamp from before the edit: still stale
# because the stamp recorded the previous source hash... wait, we edited
# sources so want_src changed; stamp still has old source hash. That's
# the case we just asserted. Now restore sources: want_src matches stamp
# again, AND elf bytes still match stamp elf hash → fresh. Good check
# that we hash sources, not mtime.
printf 'package sh\n' > "$FIXTURE/user/go/sh/main.go"
run check GOSH
[ "$CASE_RC" = 0 ] && ok "restoring sources makes the same ELF fresh again" \
    || bad "restored sources should match the stamp: $CASE_OUT"

# --- foreign ELF with a forged matching source hash still fails (elf sha) ---
cp "$FIXTURE/.build/go/GOSH.ELF.stamp" "$TMP/saved.stamp"
printf 'totally-different-bytes' > "$FIXTURE/.build/go/GOSH.ELF"
cp "$TMP/saved.stamp" "$FIXTURE/.build/go/GOSH.ELF.stamp"
run check GOSH
[ "$CASE_RC" != 0 ] && ok "replacing the ELF bytes under an old stamp is stale" \
    || bad "a swapped ELF with the old stamp must not pass: $CASE_OUT"
case "$CASE_OUT" in
    *"ensure-guest-elf: GOSH.ELF stale"*) ok "swapped ELF is named GOSH.ELF stale" ;;
    *) bad "swapped ELF should be named stale: $CASE_OUT" ;;
esac

# --- failing builder keeps the named error ---
export ENSURE_GUEST_ELF_BUILD_GOSH="exit 7"
run ensure GOSH
[ "$CASE_RC" != 0 ] && ok "ensure GOSH is non-zero when the builder fails (rc=$CASE_RC)" \
    || bad "a failed rebuild must not pass"
case "$CASE_OUT" in
    *"ensure-guest-elf: GOSH.ELF stale"*) ok "a failed rebuild still names GOSH.ELF stale" ;;
    *) bad "failed rebuild should keep the named stale error: $CASE_OUT" ;;
esac
export ENSURE_GUEST_ELF_BUILD_GOSH="$TMP/build-gosh.sh"

# --- needed-by detection against real specs ---
run needed-by "$ROOT/tools/gate/specs/live-sh-complete.spec"
[ "$CASE_RC" = 0 ] && [ "$CASE_OUT" = "GOSH" ] && ok "live-sh-complete.spec needs GOSH" \
    || bad "live-sh-complete.spec needed-by want GOSH, got rc=$CASE_RC out=$(printf %s "$CASE_OUT" | tr '\n' '|')"

run needed-by "$ROOT/tools/gate/specs/live-ssh-server.spec"
got="$(printf '%s' "$CASE_OUT" | LC_ALL=C sort | tr '\n' ' ')"
[ "$CASE_RC" = 0 ] && [ "$got" = "GOSH GOSSHD " ] && ok "live-ssh-server.spec needs GOSH and GOSSHD" \
    || bad "live-ssh-server.spec needed-by want GOSH+GOSSHD, got rc=$CASE_RC out=$(printf %s "$CASE_OUT" | tr '\n' '|')"

run needed-by "$ROOT/tools/gate/specs/live-ssh-endpoint.spec"
[ "$CASE_RC" = 0 ] && [ "$CASE_OUT" = "GOSSH" ] && ok "live-ssh-endpoint.spec needs GOSSH" \
    || bad "live-ssh-endpoint.spec needed-by want GOSSH, got rc=$CASE_RC out=$(printf %s "$CASE_OUT" | tr '\n' '|')"

run needed-by "$ROOT/tools/gate/specs/live-secrets.spec"
[ "$CASE_RC" = 0 ] && [ "$CASE_OUT" = "GOSH" ] && ok "live-secrets.spec (os.path.join form) needs GOSH" \
    || bad "live-secrets.spec needed-by want GOSH, got rc=$CASE_RC out=$(printf %s "$CASE_OUT" | tr '\n' '|')"

run needed-by "$ROOT/tools/gate/specs/live-trust-caps.spec"
[ "$CASE_RC" = 0 ] && [ -z "$CASE_OUT" ] && ok "live-trust-caps.spec needs neither ELF" \
    || bad "live-trust-caps.spec should need nothing, got rc=$CASE_RC out=$(printf %s "$CASE_OUT" | tr '\n' '|')"

run needed-by "$ROOT/tools/gate/specs/live-args.spec"
[ "$CASE_RC" = 0 ] && [ -z "$CASE_OUT" ] && ok "live-args.spec needs neither ELF" \
    || bad "live-args.spec should need nothing, got $(printf %s "$CASE_OUT" | tr '\n' '|')"

# --- locale-invariant source hash ---
h_c="$(LC_ALL=C bash "$SH" hash GOSH)"
h_utf="$(LC_ALL=en_US.UTF-8 bash "$SH" hash GOSH)"
[ -n "$h_c" ] && [ "$h_c" = "$h_utf" ] && ok "source hash is locale-invariant ($h_c)" \
    || bad "hash C=[$h_c] UTF-8=[$h_utf]"

# --- harness wiring: vgate checks, fleet ensures, before zig build ---
if grep -q 'ensure-guest-elf.sh" check' "$ROOT/tools/gate/vgate.sh" \
   || grep -q 'ensure-guest-elf.sh check' "$ROOT/tools/gate/vgate.sh"; then
    ok "vgate.sh calls ensure-guest-elf.sh check"
else
    bad "vgate.sh must call ensure-guest-elf.sh check (fail closed before boot)"
fi
check_line="$(grep -n 'ensure-guest-elf.sh' "$ROOT/tools/gate/vgate.sh" | head -1 | cut -d: -f1)"
zig_line="$(grep -n '^    zig build' "$ROOT/tools/gate/vgate.sh" | head -1 | cut -d: -f1)"
if [ -n "$check_line" ] && [ -n "$zig_line" ] && [ "$check_line" -lt "$zig_line" ]; then
    ok "vgate.sh checks guest ELF freshness before zig build (line $check_line < $zig_line)"
else
    bad "the freshness check must run before zig build (check=$check_line zig=$zig_line)"
fi
if grep -q 'ensure-guest-elf.sh" ensure' "$ROOT/tools/gate/fleet.sh" \
   || grep -q 'ensure-guest-elf.sh ensure' "$ROOT/tools/gate/fleet.sh"; then
    ok "fleet.sh calls ensure-guest-elf.sh ensure"
else
    bad "fleet.sh must call ensure-guest-elf.sh ensure (one rebuild per invocation)"
fi
if grep -q 'ensure-guest-elf.sh" stamp' "$ROOT/tools/go/build-gosh.sh" \
   || grep -q 'ensure-guest-elf.sh stamp' "$ROOT/tools/go/build-gosh.sh"; then
    ok "build-gosh.sh writes the stamp after a successful link"
else
    bad "build-gosh.sh must stamp GOSH.ELF so a hand-build is considered fresh"
fi
if grep -q 'ensure-guest-elf.sh" stamp' "$ROOT/tools/go/build-sshd.sh" \
   || grep -q 'ensure-guest-elf.sh stamp' "$ROOT/tools/go/build-sshd.sh"; then
    ok "build-sshd.sh writes the stamp after a successful link"
else
    bad "build-sshd.sh must stamp GOSSHD.ELF so a hand-build is considered fresh"
fi
if grep -q 'ensure-guest-elf.sh" stamp' "$ROOT/tools/go/build-ssh.sh" \
   || grep -q 'ensure-guest-elf.sh stamp' "$ROOT/tools/go/build-ssh.sh"; then
    ok "build-ssh.sh writes the stamp after a successful link"
else
    bad "build-ssh.sh must stamp GOSSH.ELF so a hand-build is considered fresh"
fi

echo
if [ "$FAIL" = 0 ]; then
    echo "test-ensure-guest-elf: PASS — $PASS case(s): stale/missing/foreign GOSH.ELF and GOSSHD.ELF fail as named ensure-guest-elf errors, cache hits skip the builder, needed-by matches the GOSH/GOSSHD/GOSSH specs."
    exit 0
fi
echo "test-ensure-guest-elf: FAIL — $FAIL of $((PASS + FAIL)) case(s) failed."
exit 1
