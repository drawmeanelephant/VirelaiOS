#!/usr/bin/env bash
#
# lint-workflows.sh -- lint the GitHub Actions workflows (class A).
#
# Runs actionlint over every workflow in .github/workflows/ so trigger and
# expression typos (event names, `if:` conditions, job keys, runner
# labels, ...) fail in CI instead of surfacing only when a workflow
# actually runs in production. The actionlint revision is pinned (below);
# the runner-label allowance lives in .github/actionlint.yaml.
#
# Also asserts class-A coverage between the two places the portable set is
# written down: the justfile lane recipes (`verify-portable-core` +
# `verify-portable-swift`, which `verify-portable` purely composes) and the
# two jobs in .github/workflows/ci.yml that delegate to them. A hand-copied
# step list drifted twice before (three class-A gates silently local-only,
# issue #1186), so the check below asserts BOTH the delegation and that the
# expanded recipe commands appear in the workflow — a lane recipe that
# grows a step no CI lane runs fails here, not in a missed gate.
#
# Deterministic across hosts: shellcheck/pyflakes integrations are
# disabled (-shellcheck= -pyflakes=) so the result does not depend on
# which linters happen to be preinstalled on a runner — this check is
# about workflow structure and expressions.
#
# Uses `actionlint` from PATH when present (brew install actionlint),
# otherwise self-bootstraps the pinned release into .build/ (gitignored),
# so `just verify-portable` works on a clean checkout with no global
# install. Network is required only for the first bootstrap download.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="1.7.12"
BIN=""

# os_arch -- actionlint release asset suffix for this host. Kept as a
# function so the `case` never sits inside a command substitution (old
# bash 3.2 mis-parses `$(case ... esac)` and the repo runs on old bash
# until tools/env-check.sh is sourced).
os_arch() {
    case "$(uname -s)-$(uname -m)" in
        Darwin-arm64) echo darwin_arm64 ;;
        Darwin-x86_64) echo darwin_amd64 ;;
        Linux-x86_64) echo linux_x86_64 ;;
        Linux-aarch64) echo linux_arm64 ;;
        *)
            echo "lint-workflows.sh: unsupported platform: $(uname -s)-$(uname -m) (install actionlint on PATH)" >&2
            return 1 ;;
    esac
}

if command -v actionlint >/dev/null 2>&1; then
    BIN="$(command -v actionlint)"
else
    BIN="$ROOT/.build/actionlint-${VERSION}"
    if [ ! -x "$BIN" ]; then
        mkdir -p "$ROOT/.build"
        ARCH="$(os_arch)" || exit 1
        curl -fsSL -o "$BIN.tgz" \
            "https://github.com/rhysd/actionlint/releases/download/v${VERSION}/actionlint_${VERSION}_${ARCH}.tar.gz"
        tar -xzf "$BIN.tgz" -C "$ROOT/.build" actionlint
        mv "$ROOT/.build/actionlint" "$BIN"
        rm -f "$BIN.tgz"
    fi
fi

# No file arguments: actionlint auto-discovers .github/workflows/ and reads
# .github/actionlint.yaml from the repository root. It runs first so both
# checks report in one pass, and its exit status is preserved.
set +e
"$BIN" -shellcheck= -pyflakes= -no-color
LINT_RC=$?
set -e

# --- justfile sanity: recipe names must be unique ---------------------------
#
# `just` refuses to load a file that defines two recipes with the same name,
# which silently breaks EVERY `just` command while this text-only linter still
# passes. A duplicate `claim` recipe landed in #1216 and went unnoticed until a
# manual `just --list`. Extract recipe-definition names textually and fail on
# dupes, so CI catches it without needing `just` installed on the runner.
JUSTFILE="${JUSTFILE:-$ROOT/justfile}"
DUPES="$(awk '
    /^[[:space:]]*#/ { next }                          # comment
    /^[[:space:]]*$/ { next }                          # blank
    /:=/ { next }                                      # assignment / alias / set :=
    /^[A-Za-z_][A-Za-z0-9_-]*([[:space:]]+[^:]*)?:/ {  # recipe definition
        name = $1
        sub(/:.*$/, "", name)
        if (name != "" && seen[name]++) print name
    }
' "$JUSTFILE" | sort -u)"
if [ -n "$DUPES" ]; then
    echo "lint-workflows: justfile defines duplicate recipe name(s): $DUPES" >&2
    echo "lint-workflows: 'just' cannot load the file, so every 'just' command is broken." >&2
    exit 1
fi

# --- class-A coverage: the two CI lanes must cover the verify-portable set --
#
# `just verify-portable` is a pure composition of two lane recipes:
#   verify-portable-core   (CI ubuntu-latest job)
#   verify-portable-swift  (CI macos-latest job)
# The class-A set is written down ONCE in the justfile and each lane
# delegates to its recipe: a hand-copied step list drifted twice before
# (three class-A gates silently local-only, issue #1186). The coverage
# assertion is the guard that replaced the old subset check: every `run:`
# step in ci.yml is expanded (`just <recipe>` resolves to the recipe body)
# and every command of the two lane recipes must appear. Composition is
# asserted too — a command added to verify-portable itself would run
# locally but never reach CI, the exact #1186 drift shape.
#
# RECIPE= / RECIPE_CORE= / RECIPE_SWIFT= / JUSTFILE= / CI_YML= override the
# inputs so drift fixtures can be tested without touching the real files.

RECIPE="${RECIPE:-verify-portable}"
RECIPE_CORE="${RECIPE_CORE:-verify-portable-core}"
RECIPE_SWIFT="${RECIPE_SWIFT:-verify-portable-swift}"
JUSTFILE="${JUSTFILE:-$ROOT/justfile}"
CI_YML="${CI_YML:-$ROOT/.github/workflows/ci.yml}"

# parity_key LINE -- the comparable identity of one command line.
# Kept `case`-based with BRE sed: this file must run under old bash too
# (tools/env-check.sh is what makes the modern one win).
parity_key() {
    local l="$1"
    l="$(printf '%s' "$l" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
    case "$l" in
        "run: "*) l="$(printf '%s' "$l" | sed -e 's/^run:[[:space:]]*//')" ;;
    esac
    case "$l" in
        "zig fmt"*) printf 'zig fmt --check' ; return ;;
        "zig build"*) printf '%s' "$(printf '%s' "$l" | sed 's/^\(zig build\( [^-][^ ]*\)\?\).*/\1/')" ; return ;;
    esac
    printf '%s' "$l"
}

# recipe_cmds NAME -- the command lines of the named recipe's body
# (indented lines up to the next top-level recipe). Handles dep-style
# headers (`name: dep1 dep2`) as well as plain `name:` headers.
recipe_cmds() {
    awk -v r="$1" '
        $0 ~ "^" r "([[:space:]][^:]*)?:" { insec = 1; next }
        insec && /^[[:space:]]/ {
            line = $0
            sub(/^[[:space:]]+/, "", line)
            if (line != "" && line !~ /^#/) print line
            next
        }
        insec { insec = 0 }
    ' "$JUSTFILE"
}

# Composition: verify-portable must be exactly the two lanes, no own commands.
COMPOSE_HDR="$(grep -E "^${RECIPE}:" "$JUSTFILE" | head -1)"
EXPECT_HDR="${RECIPE}: ${RECIPE_CORE} ${RECIPE_SWIFT}"
if [ "$COMPOSE_HDR" != "$EXPECT_HDR" ]; then
    echo "lint-workflows: coverage FAILED -- '$JUSTFILE' has '$COMPOSE_HDR', expected '$EXPECT_HDR'" >&2
    exit 1
fi
PORTABLE_BODY="$(recipe_cmds "$RECIPE")"
if [ -n "$PORTABLE_BODY" ]; then
    echo "lint-workflows: coverage FAILED -- 'just $RECIPE' carries own commands (invisible to CI):" >&2
    printf '%s\n' "$PORTABLE_BODY" >&2
    exit 1
fi

CORE_BODY="$(recipe_cmds "$RECIPE_CORE")"
SWIFT_BODY="$(recipe_cmds "$RECIPE_SWIFT")"
if [ -z "$CORE_BODY" ] || [ -z "$SWIFT_BODY" ]; then
    echo "lint-workflows: coverage FAILED -- both lane recipes must exist and be non-empty" >&2
    exit 1
fi

CORE_KEYS="$(printf '%s\n' "$CORE_BODY"  | while IFS= read -r l; do parity_key "$l"; done | LC_ALL=C sort)"
SWIFT_KEYS="$(printf '%s\n' "$SWIFT_BODY" | while IFS= read -r l; do parity_key "$l"; done | LC_ALL=C sort)"
OVERLAP="$(LC_ALL=C comm -12 <(printf '%s\n' "$CORE_KEYS") <(printf '%s\n' "$SWIFT_KEYS"))"
if [ -n "$OVERLAP" ]; then
    echo "lint-workflows: coverage FAILED -- both lane recipes run the same command(s):" >&2
    printf '%s\n' "$OVERLAP" >&2
    exit 1
fi

# Delegation: each recipe invoked under the right runner label.
LANES="$(awk '
    /^[[:space:]]*runs-on:[[:space:]]/ {
        r = $0; sub(/^[[:space:]]*runs-on:[[:space:]]*/, "", r); sub(/[[:space:]]*$/, "", r); runner = r }
    /^[[:space:]]*run:[[:space:]]*just[[:space:]]+verify-portable(-[a-z]+)?([[:space:]]|$)/ {
        s = $0; sub(/^[[:space:]]*run:[[:space:]]*just[[:space:]]+/, "", s); sub(/[[:space:]].*$/, "", s)
        print s "\t" runner }
' "$CI_YML")"
delegated() { printf '%s\n' "$LANES" | awk -F'\t' -v r="$1" -v p="$2" '$1 == r && index($2, p) == 1 { f = 1 } END { exit(f ? 0 : 1) }'; }
if delegated "$RECIPE_CORE" ubuntu && delegated "$RECIPE_SWIFT" macos; then
    echo "lint-workflows: class-A delegation OK -- $CI_YML runs 'just $RECIPE_CORE' on ubuntu + 'just $RECIPE_SWIFT' on macos"
else
    echo "lint-workflows: coverage FAILED -- $CI_YML must run 'just $RECIPE_CORE' on an ubuntu* runner and 'just $RECIPE_SWIFT' on a macos* runner:" >&2
    printf '%s\n' "$LANES" >&2
    exit 1
fi

# Coverage: expand `just <recipe>` run-steps to recipe bodies; every lane
# command must appear. (Recipe lines also compare literally if a lane ever
# stops delegating -- the old subset check's fallback role.)
ci_cmds() {
    awk '
        {
            line = $0
            sub(/^[[:space:]]+/, "", line)
            sub(/^run:[[:space:]]*/, "", line)
            if (line ~ /^(bash|zig|swift|go|python3|just)[[:space:]]/) print line
        }
    ' "$CI_YML" | while IFS= read -r line; do
        case "$line" in
            just\ *) r="${line#just }"; recipe_cmds "${r%% *}" ;;
            *) printf '%s\n' "$line" ;;
        esac
    done
}

ci_hay=$'\n'
while IFS= read -r c; do
    [ -n "$c" ] || continue
    ci_hay="${ci_hay}$(parity_key "$c")"$'\n'
done < <(ci_cmds)

PARITY_TOTAL=0
PARITY_MISSING=0
PARITY_LIST=""
while IFS= read -r line; do
    [ -n "$line" ] || continue
    key="$(parity_key "$line")"
    PARITY_TOTAL=$((PARITY_TOTAL + 1))
    # Newline-delimited exact match -- a plain substring test would let
    # `zig build` pass on the strength of `zig build image`.
    case "$ci_hay" in
        *$'\n'"$key"$'\n'*) ;;
        *)
            PARITY_MISSING=$((PARITY_MISSING + 1))
            PARITY_LIST="${PARITY_LIST}    - ${key}"$'\n'
            ;;
    esac
done < <(printf '%s\n%s\n' "$CORE_BODY" "$SWIFT_BODY")

if [ "$PARITY_MISSING" -eq 0 ]; then
    echo "lint-workflows: class-A coverage OK -- all $PARITY_TOTAL command(s) of the two lane recipes are run by $CI_YML"
else
    echo "lint-workflows: class-A coverage FAILED -- $PARITY_MISSING of $PARITY_TOTAL command(s) of the two lane recipes are NOT run by $CI_YML:" >&2
    printf '%s' "$PARITY_LIST" >&2
    echo "  A gate that only runs locally cannot fail a merge." >&2
    exit 1
fi

exit "$LINT_RC"
