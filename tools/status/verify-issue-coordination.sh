#!/usr/bin/env bash
#
# verify-issue-coordination.sh -- coordination gate for claims tracked as
# GitHub issues (the docs/claims/ + docs/logs/ file system was deleted
# 2026-09-03; old claim numbers cited in prose are git-history references).
#
# Claims are GitHub issues labeled `claim` — one issue per piece of work.
#   * an OPEN issue is an ACTIVE (in progress) claim
#   * progress notes are issue comments; done/blocked = close with a comment
#   * the issue body carries the machine-read fields, one bullet per line:
#       - **Owner:** <agent id> (`<branch>`)          (branch required)
#       - **Touches:** path, or/and dir/prefix* globs   (ONE line)
#       - **Status:** optional; a value starting ⛔ marks the claim blocked
#         and excludes it from the overlap check (a closed issue is done)
#
# Checks:
#   1. Every open claim has an Owner line with a backticked branch.
#   2. Two open claims from different branches declaring overlapping Touches
#      fail the gate (one editor per file). ⛔ blocked claims are skipped.
#      GENERATED artifacts (machine-rendered; see `is_generated`) are exempt:
#      they are re-rendered mechanically and merge sequentially, so
#      declaring one never holds the file and never conflicts (the recurring
#      `docs/gate-fleet-inventory.md` false failure). Extend the list when a
#      new generated file is tracked.
#   3. Claims with no comment/edit for STALE_DAYS (default 14) draw a
#      warning — comment on the issue (or close it) to show life.
#
# Claims are fetched from the live GitHub tracker via `gh` (which resolves
# the repo from the current directory's remote), or from a JSON fixture —
# the shape of `gh issue list --json number,title,url,body,updatedAt` — via
# `--issues-json <file>` for offline tests (tools/status/test-coordination.sh).
#
# Run before opening a PR (`just verify-coordination`; also CI, where it
# runs with the workflow token). Local runs need `gh` authenticated.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

ISSUES_JSON=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        --issues-json)
            ISSUES_JSON="${2:-}"
            [ -n "$ISSUES_JSON" ] || { echo "usage: --issues-json <file>" >&2; exit 2; }
            shift 2 ;;
        *)
            echo "usage: verify-issue-coordination.sh [--issues-json <file>]" >&2
            exit 2 ;;
    esac
done

fail=0
bad() { printf 'error: %s\n' "$*" >&2; fail=1; }

# --- fetch open claim issues -----------------------------------------------

if [ -n "$ISSUES_JSON" ]; then
    json="$(cat "$ISSUES_JSON")"
else
    if ! command -v gh >/dev/null 2>&1; then
        echo "error: 'gh' not found — install the GitHub CLI and authenticate (or pass --issues-json <file> for offline checks)" >&2
        exit 1
    fi
    json="$(gh issue list --label claim --state open --limit 200 \
        --json number,title,url,body,updatedAt 2>/dev/null)" \
        || { echo "error: 'gh issue list' failed — are you authenticated and inside the repository? (GH_TOKEN in CI)" >&2; exit 1; }
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

now="$(date -u +%s)"
STALE_DAYS="${STALE_DAYS:-14}"

# is_generated TOKEN -- is a Touches token a machine-generated artifact?
# Generated files are re-rendered (not hand-edited) and merge sequentially,
# so a claim declaring one must never hold the file against another claim.
# Matched by EXACT token so a broad `docs/*` glob still collides on real docs.
# Add a line per newly tracked generated file.
is_generated() {
    case "$1" in
        docs/gate-fleet-inventory.md) return 0 ;;
        docs/component-registry.md) return 0 ;; # compiled from docs/registry/*.md (issue #3676)
        *) return 1 ;;
    esac
}

# tokens_overlap A B -- do two Touches tokens collide?
# Exact match, or either side is a `prefix*` glob matching the other.
tokens_overlap() {
    x="$1"; y="$2"
    [ "$x" = "$y" ] && return 0
    case "$x" in
        *\*) p="${x%\*}"; case "$y" in "$p"*) return 0 ;; esac ;;
    esac
    case "$y" in
        *\*) p="${y%\*}"; case "$x" in "$p"*) return 0 ;; esac ;;
    esac
    return 1
}

# iso_epoch ISO8601 -- gh's updatedAt ("2026-09-02T18:12:34Z", possibly with
# fractional seconds) -> unix epoch. python3 is already a project toolchain
# dependency (disk-image + gate tooling).
iso_epoch() {
    python3 -c '
import sys, datetime
s = sys.argv[1].split(".")[0].rstrip("Z")
print(int(datetime.datetime.strptime(s, "%Y-%m-%dT%H:%M:%S").timestamp()))
' "$1"
}

# --- per-claim validation + collection --------------------------------------

# Records: "branch touches" lines for ACTIVE (non-blocked) claims.
active_declared=""
checked=0
generated_exempt=0

while IFS= read -r rec; do
    [ -n "$rec" ] || continue
    num="$(jq -r '.number' <<<"$rec")"
    title="$(jq -r '.title' <<<"$rec")"
    url="$(jq -r '.url' <<<"$rec")"
    upd="$(jq -r '.updatedAt' <<<"$rec")"
    jq -r '.body' <<<"$rec" > "$tmp/body-$num.md"

    checked=$((checked + 1))

    branch="$(sed -n 's/^- \*\*Owner:\*\* [^`]*`\([^`]*\)`.*/\1/p' "$tmp/body-$num.md" | head -1)"
    if [ -z "$branch" ]; then
        bad "claim #$num ($title): Owner must include a backticked branch (e.g. '- **Owner:** buffy (\`agent/buffy/foo\`)')"
        continue
    fi

    # Staleness: an open claim with no comment/edit for STALE_DAYS+ days.
    if [ -n "$upd" ] && [ "$upd" != "null" ]; then
        ts="$(iso_epoch "$upd")" || ts=""
        if [ -n "$ts" ]; then
            age_days=$(( (now - ts) / 86400 ))
            if [ "$age_days" -ge "$STALE_DAYS" ]; then
                printf 'warn: claim #%s (%s): no update for %d days — comment on the issue or close it (past ~21 days anyone may close it: %s)\n' \
                    "$num" "$title" "$age_days" "$url" >&2
            fi
        fi
    fi

    # Blocked (⛔ in the optional Status field) claims do not hold files.
    status_line="$(sed -n 's/^- \*\*Status:\*\* //p' "$tmp/body-$num.md" | head -1)"
    case "$status_line" in
        ⛔*) continue ;;
    esac

    touches="$(sed -n 's/^- \*\*Touches:\*\* //p' "$tmp/body-$num.md" | head -1 | tr ',' ' ')"
    for t in $touches; do
        if is_generated "$t"; then
            generated_exempt=$((generated_exempt + 1))
            continue
        fi
        active_declared="${active_declared}${num} ${branch} ${t}
"
    done
done < <(printf '%s\n' "$json" | jq -c '.[]' 2>/dev/null)

# --- pairwise overlap between ACTIVE claims from different branches ---------
# Reported once per unordered pair.

if [ -n "$active_declared" ]; then
    while read -r n1 b1 t1; do
        [ -n "$t1" ] || continue
        while read -r n2 b2 t2; do
            [ -n "$t2" ] || continue
            [ "$b1" != "$b2" ] || continue
            if [[ "$b1$t1" < "$b2$t2" ]]; then continue; fi
            if tokens_overlap "$t1" "$t2"; then
                bad "open claims #$n1 ('$b1') and #$n2 ('$b2') both declare '$t1' / '$t2' — one editor per file: coordinate, or close/stall one claim"
            fi
        done <<EOF2
$active_declared
EOF2
    done <<EOF1
$active_declared
EOF1
fi

# --- verdict ----------------------------------------------------------------

if [ "$generated_exempt" -gt 0 ]; then
    printf 'note: %d generated-artifact Touches declaration(s) exempt from the one-editor rule\n' "$generated_exempt"
fi

if [ "$fail" -ne 0 ]; then
    printf 'verify-issue-coordination: FAILED\n' >&2
    exit 1
fi
printf 'verify-issue-coordination: ok — %d open claim issue(s) checked, no file overlaps\n' "$checked"
